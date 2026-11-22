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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/blinklabs-io/bark/eventtype"
	eventv1alpha1 "github.com/blinklabs-io/bark/proto/v1alpha1/event"
	eventv1alpha1connect "github.com/blinklabs-io/bark/proto/v1alpha1/event/eventv1alpha1connect"
	"github.com/blinklabs-io/dingoctl/internal/client"
	"github.com/blinklabs-io/dingoctl/internal/errs"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type fakeEventService struct {
	eventv1alpha1connect.UnimplementedEventServiceHandler

	mu         sync.Mutex
	lastFilter *eventv1alpha1.EventFilter
	reliable   bool
	events     []*eventv1alpha1.Event
}

func (f *fakeEventService) ListEventTypes(
	context.Context, *connect.Request[eventv1alpha1.ListEventTypesRequest],
) (*connect.Response[eventv1alpha1.ListEventTypesResponse], error) {
	return connect.NewResponse(&eventv1alpha1.ListEventTypesResponse{
		EventTypes: []*eventv1alpha1.EventTypeInfo{
			{Name: "chain.update", Category: "chain", Description: "block added", HasTypedPayload: true},
			{Name: "epoch.transition", Category: "epoch", Description: "epoch changed"},
		},
	}), nil
}

func (f *fakeEventService) GetEventSchema(
	_ context.Context, req *connect.Request[eventv1alpha1.GetEventSchemaRequest],
) (*connect.Response[eventv1alpha1.GetEventSchemaResponse], error) {
	if req.Msg.GetEventType() != "epoch.transition" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("unknown event type"))
	}
	// The same descriptor set the server sends: the payload's file and every
	// file it imports.
	set, err := eventtype.PayloadDescriptors("epoch.transition")
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&eventv1alpha1.GetEventSchemaResponse{
		Info:               &eventv1alpha1.EventTypeInfo{Name: "epoch.transition", Category: "epoch", Description: "epoch changed", HasTypedPayload: true},
		PayloadMessage:     "bark.v1alpha1.event.EpochTransitionEventData",
		PayloadDescriptors: set,
	}), nil
}

func (f *fakeEventService) Subscribe(
	ctx context.Context,
	req *connect.Request[eventv1alpha1.SubscribeRequest],
	stream *connect.ServerStream[eventv1alpha1.SubscribeResponse],
) error {
	f.mu.Lock()
	f.lastFilter = req.Msg.GetFilter()
	events := f.events
	f.mu.Unlock()
	for _, ev := range events {
		if err := stream.Send(&eventv1alpha1.SubscribeResponse{Event: ev}); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}

func (f *fakeEventService) SubscribeWithAck(
	ctx context.Context,
	stream *connect.BidiStream[eventv1alpha1.SubscribeWithAckRequest, eventv1alpha1.SubscribeWithAckResponse],
) error {
	first, err := stream.Receive()
	if err != nil {
		return err
	}
	f.mu.Lock()
	f.reliable = true
	f.lastFilter = first.GetStart().GetFilter()
	events := f.events
	f.mu.Unlock()
	for _, ev := range events {
		if err := stream.Send(&eventv1alpha1.SubscribeWithAckResponse{Event: ev}); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}

func setupEventsCommandTest(t *testing.T, format string) (*fakeEventService, *syncBuffer) {
	t.Helper()
	fake := &fakeEventService{}
	mux := http.NewServeMux()
	mux.Handle(eventv1alpha1connect.NewEventServiceHandler(fake))
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)

	c, err := client.New(client.Config{
		Address:        strings.TrimPrefix(srv.URL, "https://"),
		Insecure:       true,
		Timeout:        5 * time.Second,
		MaxRetries:     1,
		RetryBaseDelay: time.Millisecond,
		RetryMaxDelay:  time.Millisecond,
	})
	if err != nil {
		t.Fatalf("construct client: %v", err)
	}
	sessionOnce.Do(func() {})
	sessionClient = c
	sessionErr = nil
	t.Cleanup(func() {
		sessionOnce = sync.Once{}
		sessionClient = nil
		sessionErr = nil
	})

	prev := globalFlags
	globalFlags = GlobalFlags{Output: format}
	t.Cleanup(func() { globalFlags = prev })

	buf := &syncBuffer{}
	outputWriterForTest = buf
	t.Cleanup(func() { outputWriterForTest = nil })
	return fake, buf
}

func runEvents(ctx context.Context, args ...string) error {
	cmd := newEventsCmd()
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return cmd.ExecuteContext(ctx)
}

func chainEvent(t *testing.T, id string, seq uint64, era string) *eventv1alpha1.Event {
	t.Helper()
	data, err := anypb.New(&eventv1alpha1.EpochTransitionEventData{NewEpoch: seq, Era: era})
	if err != nil {
		t.Fatal(err)
	}
	return &eventv1alpha1.Event{Id: id, EventType: "epoch.transition", Sequence: seq, OriginNodeId: "n1", TypedData: data}
}

func TestEventsListCommand(t *testing.T) {
	_, buf := setupEventsCommandTest(t, "json")
	if err := runEvents(context.Background(), "list"); err != nil {
		t.Fatalf("events list: %v", err)
	}
	var got eventTypeList
	if err := json.Unmarshal([]byte(buf.String()), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, buf.String())
	}
	if len(got.EventTypes) != 2 || got.EventTypes[0].Name != "chain.update" || !got.EventTypes[0].HasTypedPayload {
		t.Fatalf("unexpected event types: %+v", got.EventTypes)
	}
}

func TestEventsSchemaCommand(t *testing.T) {
	_, buf := setupEventsCommandTest(t, "text")
	if err := runEvents(context.Background(), "schema", "epoch.transition"); err != nil {
		t.Fatalf("events schema: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"epoch.transition", "EpochTransitionEventData", "new_epoch", "uint64", "era"} {
		if !strings.Contains(out, want) {
			t.Errorf("schema output missing %q:\n%s", want, out)
		}
	}

	err := runEvents(context.Background(), "schema", "nope")
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("unknown type: got %v, want NotFound", err)
	}
	if errs.ExitCode(err) != 69 {
		t.Fatalf("unknown type exit code = %d, want 69", errs.ExitCode(err))
	}
}

func subscribeUntil(t *testing.T, buf *syncBuffer, lines int, args ...string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runEvents(ctx, append([]string{"subscribe"}, args...)...) }()
	for strings.Count(buf.String(), "\n") < lines {
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Millisecond):
		}
		if ctx.Err() != nil {
			t.Fatalf("timed out; output so far:\n%s", buf.String())
		}
	}
	cancel()
	return <-done
}

func TestEventsSubscribeStreamsAndFilters(t *testing.T) {
	for _, reliable := range []bool{false, true} {
		fake, buf := setupEventsCommandTest(t, "json")
		fake.events = []*eventv1alpha1.Event{
			chainEvent(t, "a", 1, "byron"),
			chainEvent(t, "b", 2, "conway"),
		}
		args := []string{"--type", "epoch.*", "--filter", "data.era=conway"}
		if reliable {
			args = append(args, "--reliable")
		}
		if err := subscribeUntil(t, buf, 1, args...); err != nil {
			t.Fatalf("reliable=%v: subscribe returned %v after cancel, want nil", reliable, err)
		}

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 1 {
			t.Fatalf("reliable=%v: got %d lines, want 1 (byron filtered out):\n%s", reliable, len(lines), buf.String())
		}
		var ev eventResult
		if err := json.Unmarshal([]byte(lines[0]), &ev); err != nil {
			t.Fatalf("line is not compact JSON: %v", err)
		}
		if ev.ID != "b" || ev.Data["era"] != "conway" || ev.Data["new_epoch"] != "2" {
			t.Fatalf("unexpected event: %+v", ev)
		}

		fake.mu.Lock()
		gotTypes, gotReliable := fake.lastFilter.GetEventTypes(), fake.reliable
		fake.mu.Unlock()
		if len(gotTypes) != 1 || gotTypes[0] != "epoch.*" {
			t.Fatalf("server filter types = %v", gotTypes)
		}
		if gotReliable != reliable {
			t.Fatalf("server saw reliable=%v, want %v", gotReliable, reliable)
		}
	}
}

func TestEventsSubscribeValidation(t *testing.T) {
	setupEventsCommandTest(t, "text")
	for _, args := range [][]string{
		{"subscribe", "--subscriber-id", "x"},
		{"subscribe", "--from-sequence", "3"},
		{"subscribe", "--filter", "nonsense"},
		{"subscribe", "--filter", "colour=red"},
	} {
		if err := runEvents(context.Background(), args...); err == nil {
			t.Errorf("%v: expected an error", args)
		}
	}
}

func TestEventFilters(t *testing.T) {
	ev := eventFromProto(chainEvent(t, "a", 7, "conway"))
	for expr, want := range map[string]bool{
		"type=epoch.transition": true,
		"type=epoch.*":          true,
		"type=chain.*":          false,
		"type!=chain.update":    true,
		"origin=n1":             true,
		"sequence=7":            true,
		"data.era=conway":       true,
		"data.era!=conway":      false,
		"data.missing=x":        false,
		"data.missing!=x":       true,
	} {
		match, err := parseEventFilters([]string{expr})
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		if got := match(ev); got != want {
			t.Errorf("%s: got %v, want %v", expr, got, want)
		}
	}
}

// TestEventNodeStatePayloadsDecode checks the sync state, protocol parameter
// and database operation payloads decode into fields a filter can match.
func TestEventNodeStatePayloadsDecode(t *testing.T) {
	for _, tc := range []struct {
		eventType string
		payload   proto.Message
		filter    string
	}{
		{
			"ledger.sync_state",
			&eventv1alpha1.SyncStateEventData{
				PreviousState: eventv1alpha1.SyncState_SYNC_STATE_SYNCING,
				CurrentState:  eventv1alpha1.SyncState_SYNC_STATE_CAUGHT_UP,
				TipSlot:       700,
			},
			"data.current_state=SYNC_STATE_CAUGHT_UP",
		},
		{
			"ledger.protocol_params",
			&eventv1alpha1.ProtocolParametersEventData{Epoch: 12, Era: "conway", ProtocolMajor: 10},
			"data.era=conway",
		},
		{
			"database.operation",
			&eventv1alpha1.DatabaseOperationEventData{OperationId: "op-1"},
			"data.operation_id=op-1",
		},
	} {
		data, err := anypb.New(tc.payload)
		if err != nil {
			t.Fatal(err)
		}
		ev := eventFromProto(&eventv1alpha1.Event{Id: "x", EventType: tc.eventType, TypedData: data})
		if ev.DataType != "" || ev.Data == nil {
			t.Fatalf("%s: payload not decoded (data_type %q)", tc.eventType, ev.DataType)
		}
		match, err := parseEventFilters([]string{tc.filter})
		if err != nil {
			t.Fatalf("%s: %v", tc.filter, err)
		}
		if !match(ev) {
			t.Errorf("%s: %s did not match %v", tc.eventType, tc.filter, ev.Data)
		}
	}
}

func TestSignalExit(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	for sig, want := range map[syscall.Signal]int{syscall.SIGINT: 130, syscall.SIGTERM: 143} {
		ch := make(chan os.Signal, 1)
		ch <- sig
		err := signalExit(cancelled, ch)
		if errs.ExitCode(err) != want {
			t.Errorf("%v: exit code %d, want %d", sig, errs.ExitCode(err), want)
		}
	}
	if err := signalExit(context.Background(), make(chan os.Signal)); err != nil {
		t.Errorf("no signal: got %v, want nil", err)
	}
}
