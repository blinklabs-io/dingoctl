// Copyright 2026 Blink Labs Software
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"connectrpc.com/connect"
	eventv1alpha1 "github.com/blinklabs-io/bark/proto/v1alpha1/event"
	"github.com/blinklabs-io/dingoctl/internal/client"
	"github.com/blinklabs-io/dingoctl/internal/errs"
	"github.com/blinklabs-io/dingoctl/internal/output"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/anypb"
)

// newEventsCmd creates the events command, covering the read-only RPCs of
// bark's EventService: ListEventTypes, GetEventSchema, and the Subscribe and
// SubscribeWithAck streams. Publish is intentionally not exposed.
func newEventsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "events",
		Short: "List, inspect, and stream events from the Dingo node's EventBus",
	}
	cmd.AddCommand(newEventsListCmd())
	cmd.AddCommand(newEventsSubscribeCmd())
	cmd.AddCommand(newEventsSchemaCmd())
	return cmd
}

func newEventsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the event types the node accepts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := GetClient()
			if err != nil {
				return err
			}
			resp, err := c.EventService().ListEventTypes(
				cmd.Context(),
				connect.NewRequest(&eventv1alpha1.ListEventTypesRequest{}),
			)
			if err != nil {
				return err
			}
			res := eventTypeList{EventTypes: make([]eventTypeResult, 0, len(resp.Msg.GetEventTypes()))}
			for _, t := range resp.Msg.GetEventTypes() {
				res.EventTypes = append(res.EventTypes, eventTypeFromProto(t))
			}
			return GetOutputPrinter().Print(res)
		},
	}
}

func newEventsSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema <type>",
		Short: "Print the schema for an event type",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := GetClient()
			if err != nil {
				return err
			}
			resp, err := c.EventService().GetEventSchema(
				cmd.Context(),
				connect.NewRequest(&eventv1alpha1.GetEventSchemaRequest{EventType: args[0]}),
			)
			if err != nil {
				return err
			}
			res, err := eventSchemaFromProto(resp.Msg)
			if err != nil {
				return err
			}
			return GetOutputPrinter().Print(res)
		},
	}
}

func newEventsSubscribeCmd() *cobra.Command {
	var (
		types        []string
		originNodes  []string
		reliable     bool
		subscriberID string
		fromSequence uint64
		filters      []string
	)

	cmd := &cobra.Command{
		Use:   "subscribe",
		Short: "Stream events to stdout until interrupted",
		Long: `Stream events from the node. Each event is written as it arrives:
one line in text mode, one compact JSON object per line with --output json
(the practical choice for scripting), and one document per event with
--output yaml. The table format is rendered as text.

By default delivery is at-most-once (Subscribe): events published while the
connection is down are not replayed. With --reliable (SubscribeWithAck) each
event is acknowledged after it is written, a reconnect resumes from the last
acknowledged event, and redelivered events are skipped by sequence. Either mode
reconnects on a transient disconnect within the configured retry budget.

--type accepts registered names ("chain.update") and category patterns
("peergov.*"). SIGINT and SIGTERM stop the stream once the event being written is
complete, and exit with the conventional 128+signal status (130 for
SIGINT, 143 for SIGTERM).

--filter narrows events on the client after they arrive, and is repeatable
with every filter required to match. A filter is key=value or key!=value,
where the value may use * and ? wildcards. Keys are type, origin_node_id,
id, sequence, and data.<field> for a field of the decoded payload (nested
fields as data.rollback.to_slot). A key an event lacks never matches =.
Filtered-out events are still acknowledged in --reliable mode.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !reliable && (subscriberID != "" || fromSequence != 0) {
				return errors.New("--subscriber-id and --from-sequence require --reliable")
			}
			match, err := parseEventFilters(filters)
			if err != nil {
				return err
			}
			if reliable && subscriberID == "" {
				id, err := newSubscriberID()
				if err != nil {
					return err
				}
				subscriberID = id
			}
			c, err := GetClient()
			if err != nil {
				return err
			}

			w := cmd.OutOrStdout()
			if outputWriterForTest != nil {
				w = outputWriterForTest
			}
			stderr := cmd.ErrOrStderr()
			format := output.Format(globalFlags.Output)

			handler := func(ev *eventv1alpha1.Event) error {
				res := eventFromProto(ev)
				if globalFlags.Quiet || !match(res) {
					return nil
				}
				return writeEvent(w, format, res)
			}
			onReconnect := func(err error) {
				fmt.Fprintf(stderr, "event stream interrupted (%v); reconnecting\n", err)
			}

			sigCh := make(chan os.Signal, 2)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			defer signal.Stop(sigCh)

			err = client.RunEventStream(
				cmd.Context(),
				c.Config(),
				c.EventService(),
				client.EventStreamOptions{
					Filter: &eventv1alpha1.EventFilter{
						EventTypes:    types,
						OriginNodeIds: originNodes,
					},
					Reliable:          reliable,
					SubscriberID:      subscriberID,
					LastAckedSequence: fromSequence,
				},
				handler,
				onReconnect,
			)
			if err != nil {
				return err
			}
			return signalExit(cmd.Context(), sigCh)
		},
	}

	cmd.Flags().StringSliceVar(&types, "type", nil, "event type or category pattern to receive (repeatable; default all)")
	cmd.Flags().StringSliceVar(&originNodes, "origin-node", nil, "only receive events from this origin node id (repeatable)")
	cmd.Flags().StringArrayVar(&filters, "filter", nil, "client-side filter such as type=chain.update or data.era!=byron (repeatable, all must match; see --help)")
	cmd.Flags().BoolVar(&reliable, "reliable", false, "use acknowledged at-least-once delivery (SubscribeWithAck)")
	cmd.Flags().StringVar(&subscriberID, "subscriber-id", "", "identity of a reliable subscription, to resume it across runs (default: random)")
	cmd.Flags().Uint64Var(&fromSequence, "from-sequence", 0, "with --reliable, resume after this sequence (0 = current position)")
	return cmd
}

func newSubscriberID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generating subscriber id: %w", err)
	}
	return "dingoctl-" + hex.EncodeToString(b[:]), nil
}

// writeEvent renders one event. JSON is compact so the stream is
// newline-delimited; the Printer's indented JSON would span many lines per
// event and defeat line-oriented consumers.
func writeEvent(w io.Writer, format output.Format, ev eventResult) error {
	switch format {
	case output.FormatJSON:
		b, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\n", b)
		return err
	case output.FormatYAML:
		if _, err := fmt.Fprintln(w, "---"); err != nil {
			return err
		}
		return output.New(w, format, false).Print(ev)
	default:
		_, err := fmt.Fprintln(w, ev)
		return err
	}
}

// ── result types ─────────────────────────────────────────────────────────

type eventResult struct {
	ID           string         `json:"id" yaml:"id"`
	Type         string         `json:"type" yaml:"type"`
	Timestamp    string         `json:"timestamp,omitempty" yaml:"timestamp,omitempty"`
	OriginNodeID string         `json:"origin_node_id,omitempty" yaml:"origin_node_id,omitempty"`
	Sequence     uint64         `json:"sequence" yaml:"sequence"`
	Data         map[string]any `json:"data,omitempty" yaml:"data,omitempty"`
	// DataType is the payload's type URL, set only when the payload could not
	// be decoded (a type this dingoctl build does not know).
	DataType string `json:"data_type,omitempty" yaml:"data_type,omitempty"`
}

func eventFromProto(ev *eventv1alpha1.Event) eventResult {
	r := eventResult{
		ID:           ev.GetId(),
		Type:         ev.GetEventType(),
		OriginNodeID: ev.GetOriginNodeId(),
		Sequence:     ev.GetSequence(),
	}
	if ts := ev.GetTimestamp(); ts != nil {
		r.Timestamp = ts.AsTime().UTC().Format(time.RFC3339Nano)
	}
	if td := ev.GetTypedData(); td != nil {
		r.Data, r.DataType = decodePayload(td)
	}
	return r
}

func decodePayload(td *anypb.Any) (map[string]any, string) {
	msg, err := anypb.UnmarshalNew(td, proto.UnmarshalOptions{})
	if err != nil {
		return nil, td.GetTypeUrl()
	}
	b, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(msg)
	if err != nil {
		return nil, td.GetTypeUrl()
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		return nil, td.GetTypeUrl()
	}
	return data, ""
}

func (r eventResult) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s seq=%d id=%s", r.Timestamp, r.Type, r.Sequence, r.ID)
	if r.OriginNodeID != "" {
		fmt.Fprintf(&sb, " origin=%s", r.OriginNodeID)
	}
	switch {
	case r.Data != nil:
		if b, err := json.Marshal(r.Data); err == nil {
			fmt.Fprintf(&sb, " data=%s", b)
		}
	case r.DataType != "":
		fmt.Fprintf(&sb, " data_type=%s", r.DataType)
	}
	return sb.String()
}

type eventTypeResult struct {
	Name            string `json:"name" yaml:"name"`
	Category        string `json:"category" yaml:"category"`
	Description     string `json:"description" yaml:"description"`
	HasTypedPayload bool   `json:"has_typed_payload" yaml:"has_typed_payload"`
}

func eventTypeFromProto(t *eventv1alpha1.EventTypeInfo) eventTypeResult {
	return eventTypeResult{
		Name:            t.GetName(),
		Category:        t.GetCategory(),
		Description:     t.GetDescription(),
		HasTypedPayload: t.GetHasTypedPayload(),
	}
}

type eventTypeList struct {
	EventTypes []eventTypeResult `json:"event_types" yaml:"event_types"`
}

func (l eventTypeList) String() string {
	var sb strings.Builder
	for i, t := range l.EventTypes {
		if i > 0 {
			sb.WriteByte('\n')
		}
		fmt.Fprintf(&sb, "%s\t%s", t.Name, t.Description)
	}
	return sb.String()
}

func (l eventTypeList) TableHeader() []string {
	return []string{"NAME", "CATEGORY", "TYPED PAYLOAD", "DESCRIPTION"}
}

func (l eventTypeList) TableRows() [][]string {
	rows := make([][]string, 0, len(l.EventTypes))
	for _, t := range l.EventTypes {
		rows = append(rows, []string{t.Name, t.Category, strconv.FormatBool(t.HasTypedPayload), t.Description})
	}
	return rows
}

type eventFieldResult struct {
	Name     string `json:"name" yaml:"name"`
	Number   int32  `json:"number" yaml:"number"`
	Type     string `json:"type" yaml:"type"`
	Repeated bool   `json:"repeated,omitempty" yaml:"repeated,omitempty"`
}

type eventSchemaResult struct {
	eventTypeResult `yaml:",inline"`
	PayloadMessage  string             `json:"payload_message,omitempty" yaml:"payload_message,omitempty"`
	Fields          []eventFieldResult `json:"fields,omitempty" yaml:"fields,omitempty"`
}

func (r eventSchemaResult) String() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s (%s)\n%s", r.Name, r.Category, r.Description)
	if r.PayloadMessage == "" {
		sb.WriteString("\nNo typed payload.")
		return sb.String()
	}
	fmt.Fprintf(&sb, "\nPayload: %s", r.PayloadMessage)
	for _, f := range r.Fields {
		rep := ""
		if f.Repeated {
			rep = "repeated "
		}
		fmt.Fprintf(&sb, "\n  %d  %s  %s%s", f.Number, f.Name, rep, f.Type)
	}
	return sb.String()
}

// eventSchemaFromProto builds the schema view from the descriptors the
// server returned rather than compiled-in types, so it also describes
// payloads newer than this dingoctl build.
func eventSchemaFromProto(msg *eventv1alpha1.GetEventSchemaResponse) (eventSchemaResult, error) {
	res := eventSchemaResult{
		eventTypeResult: eventTypeFromProto(msg.GetInfo()),
		PayloadMessage:  msg.GetPayloadMessage(),
	}
	if res.PayloadMessage == "" {
		return res, nil
	}
	files, err := protodesc.NewFiles(&descriptorpb.FileDescriptorSet{
		File: msg.GetPayloadDescriptors().GetFile(),
	})
	if err != nil {
		return res, fmt.Errorf("decoding payload descriptors: %w", err)
	}
	d, err := files.FindDescriptorByName(protoreflect.FullName(res.PayloadMessage))
	if err != nil {
		return res, fmt.Errorf("payload message %s not in descriptors: %w", res.PayloadMessage, err)
	}
	md, ok := d.(protoreflect.MessageDescriptor)
	if !ok {
		return res, fmt.Errorf("payload %s is not a message", res.PayloadMessage)
	}
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		typ := f.Kind().String()
		switch {
		case f.Message() != nil:
			typ = string(f.Message().FullName())
		case f.Enum() != nil:
			typ = string(f.Enum().FullName())
		}
		res.Fields = append(res.Fields, eventFieldResult{
			Name:     string(f.Name()),
			Number:   int32(f.Number()),
			Type:     typ,
			Repeated: f.Cardinality() == protoreflect.Repeated,
		})
	}
	return res, nil
}

// signalExit turns a stream ended by SIGINT/SIGTERM into the conventional
// 128+signal exit status. Execute's signal context and this command's own
// channel are notified independently, so when the context is already
// cancelled the signal may land a moment after the stream returned.
func signalExit(ctx context.Context, sigCh <-chan os.Signal) error {
	wait := time.Duration(0)
	if ctx.Err() != nil {
		wait = 200 * time.Millisecond
	}
	select {
	case sig := <-sigCh:
		if s, ok := sig.(syscall.Signal); ok {
			return &errs.ExitCodeError{Code: 128 + int(s)}
		}
		return &errs.ExitCodeError{Code: 130}
	case <-time.After(wait):
		return nil
	}
}
