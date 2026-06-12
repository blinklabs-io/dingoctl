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
	"time"

	"connectrpc.com/connect"
)

// StreamFactory is a function that opens a new server-streaming RPC and
// returns the stream.  The factory is called once per (re)connect attempt.
type StreamFactory[T any] func(ctx context.Context) (*connect.ServerStreamForClient[T], error)

// StreamHandler is called for every message received from the stream.
// Returning a non-nil error stops the stream (it is NOT retried).
type StreamHandler[T any] func(msg *T) error

// RunServerStream opens a server-streaming RPC via factory and drives it with
// handler.  On a transient disconnect (CodeUnavailable) the stream is
// re-established up to cfg.MaxRetries times with exponential backoff.
//
// The context should carry the caller's cancellation signal; cancellation
// always terminates the loop regardless of the retry budget.
func RunServerStream[T any](
	ctx context.Context,
	cfg Config,
	factory StreamFactory[T],
	handler StreamHandler[T],
) error {
	delay := cfg.RetryBaseDelay
	for attempt := 0; ; attempt++ {
		err := driveStream(ctx, factory, handler)
		if err == nil {
			return nil
		}
		if cfg.MaxRetries == 0 || attempt >= cfg.MaxRetries || !transientCode(connect.CodeOf(err)) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
		if delay > cfg.RetryMaxDelay {
			delay = cfg.RetryMaxDelay
		}
	}
}

// driveStream opens one stream and runs handler for each received message
// until the stream ends normally, the context is cancelled, or an error
// occurs.
func driveStream[T any](
	ctx context.Context,
	factory StreamFactory[T],
	handler StreamHandler[T],
) error {
	stream, err := factory(ctx)
	if err != nil {
		return err
	}
	defer stream.Close()

	for stream.Receive() {
		if err := handler(stream.Msg()); err != nil {
			return err
		}
	}
	return stream.Err()
}
