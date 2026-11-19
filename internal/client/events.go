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

package client

import (
	"context"
	"errors"
	"io"

	"connectrpc.com/connect"
	eventv1alpha1 "github.com/blinklabs-io/bark/proto/v1alpha1/event"
	eventv1alpha1connect "github.com/blinklabs-io/bark/proto/v1alpha1/event/eventv1alpha1connect"
)

// EventStreamOptions configures RunEventStream.
type EventStreamOptions struct {
	Filter *eventv1alpha1.EventFilter
	// Reliable selects SubscribeWithAck (at-least-once, resumable) instead of
	// Subscribe (at-most-once).
	Reliable bool
	// SubscriberID identifies a reliable subscription across reconnects. It is
	// required when Reliable is set.
	SubscriberID string
	// LastAckedSequence is where a reliable subscription starts; zero means
	// the stream's current position.
	LastAckedSequence uint64
}

// errStreamClosed marks a stream the server ended without an error. A
// subscription is meant to run until cancelled, so it is treated like a
// transient disconnect.
var errStreamClosed = connect.NewError(connect.CodeUnavailable, errors.New("event stream closed by server"))

// RunEventStream delivers events to handler until ctx is cancelled, handler
// fails, or a non-transient error occurs. Transient disconnects are retried
// under cfg's retry policy, whose budget is restored each time an event
// arrives so a long-lived subscription is not worn down by occasional
// reconnects. A nil return means ctx was cancelled.
//
// In reliable mode an event is acknowledged only after handler returns nil,
// reconnects resume from the last acknowledged sequence, and a redelivered
// event (at-least-once) is skipped because sequences are strictly increasing
// within a delivery stream, so the cursor never moves backwards. Handler
// errors are never retried. onReconnect, if non-nil, is called with the
// error that caused each retry.
func RunEventStream(
	ctx context.Context,
	cfg Config,
	svc eventv1alpha1connect.EventServiceClient,
	opts EventStreamOptions,
	handler func(*eventv1alpha1.Event) error,
	onReconnect func(error),
) error {
	rp := newRetryPolicy(cfg)
	lastAcked := opts.LastAckedSequence

	for {
		progressed := false
		onEvent := func(ev *eventv1alpha1.Event) error {
			progressed = true
			if opts.Reliable && ev.GetSequence() <= lastAcked {
				return nil
			}
			if err := handler(ev); err != nil {
				return &handlerError{cause: err}
			}
			if opts.Reliable {
				lastAcked = ev.GetSequence()
			}
			return nil
		}

		var err error
		if opts.Reliable {
			err = runReliable(ctx, svc, opts, &lastAcked, onEvent)
		} else {
			err = runPlain(ctx, svc, opts, onEvent)
		}

		var he *handlerError
		switch {
		case errors.As(err, &he):
			return he.cause
		case ctx.Err() != nil:
			return nil //nolint:nilerr // cancellation is the normal way to stop
		}
		if err == nil {
			err = errStreamClosed
		}
		if progressed {
			rp = newRetryPolicy(cfg)
		}
		if !rp.retriable(err) {
			return err
		}
		if onReconnect != nil {
			onReconnect(err)
		}
		if advErr := rp.advance(ctx); advErr != nil {
			return nil //nolint:nilerr // cancelled while backing off
		}
	}
}

type eventSink func(*eventv1alpha1.Event) error

func runPlain(
	ctx context.Context,
	svc eventv1alpha1connect.EventServiceClient,
	opts EventStreamOptions,
	sink eventSink,
) error {
	stream, err := svc.Subscribe(ctx, connect.NewRequest(&eventv1alpha1.SubscribeRequest{Filter: opts.Filter}))
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()
	for stream.Receive() {
		if err := sink(stream.Msg().GetEvent()); err != nil {
			return err
		}
	}
	return stream.Err()
}

func runReliable(
	ctx context.Context,
	svc eventv1alpha1connect.EventServiceClient,
	opts EventStreamOptions,
	lastAcked *uint64,
	sink eventSink,
) error {
	stream := svc.SubscribeWithAck(ctx)
	defer func() { _ = stream.CloseResponse() }()

	err := stream.Send(&eventv1alpha1.SubscribeWithAckRequest{
		Message: &eventv1alpha1.SubscribeWithAckRequest_Start{
			Start: &eventv1alpha1.SubscribeWithAckStart{
				Filter:            opts.Filter,
				SubscriberId:      opts.SubscriberID,
				LastAckedSequence: *lastAcked,
			},
		},
	})
	if err != nil {
		return sendErr(stream, err)
	}
	for {
		msg, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := sink(msg.GetEvent()); err != nil {
			return err
		}
		err = stream.Send(&eventv1alpha1.SubscribeWithAckRequest{
			Message: &eventv1alpha1.SubscribeWithAckRequest_Ack{
				Ack: &eventv1alpha1.SubscribeWithAckAck{Sequence: *lastAcked},
			},
		})
		if err != nil {
			return sendErr(stream, err)
		}
	}
}

// sendErr returns the stream's real failure after a Send error: Connect
// reports a Send on a dead stream as io.EOF and leaves the cause to Receive.
func sendErr(stream *connect.BidiStreamForClient[eventv1alpha1.SubscribeWithAckRequest, eventv1alpha1.SubscribeWithAckResponse], err error) error {
	if errors.Is(err, io.EOF) {
		if _, rerr := stream.Receive(); rerr != nil && !errors.Is(rerr, io.EOF) {
			return rerr
		}
		return nil
	}
	return err
}
