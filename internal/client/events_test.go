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
	"net"
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
// drops it, and on the second connection redelivers 1 (under an id the client never saw) and 2 before sending 3.
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
	}{{1, "z"}, {2, "b"}, {3, "c"}} {
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

	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("handled %v, want a b c with redelivered events skipped", got)
	}
	if first, second := <-svc.startSeqs, <-svc.startSeqs; first != 0 || second != 2 {
		t.Fatalf("start sequences = %d then %d, want 0 then 2", first, second)
	}
}

type plainOnceService struct {
	eventv1alpha1connect.UnimplementedEventServiceHandler
}

func (plainOnceService) Subscribe(
	ctx context.Context,
	_ *connect.Request[eventv1alpha1.SubscribeRequest],
	stream *connect.ServerStream[eventv1alpha1.SubscribeResponse],
) error {
	if err := stream.Send(&eventv1alpha1.SubscribeResponse{
		Event: &eventv1alpha1.Event{Id: "a", Sequence: 1},
	}); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

// TestRunEventStreamRetriesConnectionReset checks a subscription survives the
// connection being reset, as it is while the node restarts: connect-go
// reports the reset as CodeCanceled, which must not end the stream.
func TestRunEventStreamRetriesConnectionReset(t *testing.T) {
	_, handler := eventv1alpha1connect.NewEventServiceHandler(plainOnceService{})
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			// Linger 0 makes Close send a TCP reset rather than a FIN.
			_ = conn.(*net.TCPConn).SetLinger(0)
			_ = conn.Close()
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	svc := eventv1alpha1connect.NewEventServiceClient(srv.Client(), srv.URL)
	cfg := Config{MaxRetries: 3, RetryBaseDelay: time.Millisecond, RetryMaxDelay: 10 * time.Millisecond}
	var got []string
	err := RunEventStream(ctx, cfg, svc, EventStreamOptions{}, func(ev *eventv1alpha1.Event) error {
		got = append(got, ev.GetId())
		cancel()
		return nil
	}, nil)
	if err != nil {
		t.Fatalf("RunEventStream: %v", err)
	}
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("events = %v, want [a]", got)
	}
	if n := requests.Load(); n != 2 {
		t.Fatalf("requests = %d, want 2 (one reset, one served)", n)
	}
}
