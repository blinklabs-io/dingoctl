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
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"connectrpc.com/connect"
	eventv1alpha1 "github.com/blinklabs-io/bark/proto/v1alpha1/event"
	eventv1alpha1connect "github.com/blinklabs-io/bark/proto/v1alpha1/event/eventv1alpha1connect"
)

type fakeEventService struct {
	eventv1alpha1connect.UnimplementedEventServiceHandler
	connects  atomic.Int32
	startSeqs chan uint64
}

// SubscribeWithAck sends events 1 and 2 on the first connection and then
// drops it, and on the second connection redelivers 2 before sending 3.
func (f *fakeEventService) SubscribeWithAck(
	ctx context.Context,
	stream *connect.BidiStream[eventv1alpha1.SubscribeWithAckRequest, eventv1alpha1.SubscribeWithAckResponse],
) error {
	first, err := stream.Receive()
	if err != nil {
		return err
	}
	f.startSeqs <- first.GetStart().GetLastAckedSequence()
	send := func(seq uint64, id string) error {
		return stream.Send(&eventv1alpha1.SubscribeWithAckResponse{
			Event: &eventv1alpha1.Event{Id: id, Sequence: seq},
		})
	}
	if f.connects.Add(1) == 1 {
		for _, e := range []struct {
			seq uint64
			id  string
		}{{1, "a"}, {2, "b"}} {
			if err := send(e.seq, e.id); err != nil {
				return err
			}
		}
		for i := 0; i < 2; i++ {
			if _, err := stream.Receive(); err != nil {
				return err
			}
		}
		return connect.NewError(connect.CodeUnavailable, nil)
	}
	for _, e := range []struct {
		seq uint64
		id  string
	}{{2, "b"}, {3, "c"}} {
		if err := send(e.seq, e.id); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}

func TestRunEventStreamReliableResumesAndDedupes(t *testing.T) {
	svc := &fakeEventService{startSeqs: make(chan uint64, 4)}
	mux := http.NewServeMux()
	mux.Handle(eventv1alpha1connect.NewEventServiceHandler(svc))
	srv := httptest.NewUnstartedServer(mux)
	srv.EnableHTTP2 = true
	srv.StartTLS()
	defer srv.Close()

	cfg := Config{MaxRetries: 3, RetryBaseDelay: time.Millisecond, RetryMaxDelay: time.Millisecond}
	c := eventv1alpha1connect.NewEventServiceClient(srv.Client(), srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var got []string
	err := RunEventStream(ctx, cfg, c,
		EventStreamOptions{Reliable: true, SubscriberID: "t"},
		func(ev *eventv1alpha1.Event) error {
			got = append(got, ev.GetId())
			if ev.GetId() == "c" {
				cancel()
			}
			return nil
		}, nil)
	if err != nil {
		t.Fatalf("RunEventStream: %v", err)
	}

	if len(got) != 3 || got[2] != "c" {
		t.Fatalf("handled %v, want three events ending in c with b not repeated", got)
	}
	if first, second := <-svc.startSeqs, <-svc.startSeqs; first != 0 || second != 2 {
		t.Fatalf("start sequences = %d then %d, want 0 then 2", first, second)
	}
}
