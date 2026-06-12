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

	"connectrpc.com/connect"
)

// handlerError wraps an error returned by a StreamHandler so RunServerStream
// can distinguish application failures from transport failures and never retry
// them, honouring the StreamHandler contract.
type handlerError struct{ cause error }

func (e *handlerError) Error() string { return e.cause.Error() }
func (e *handlerError) Unwrap() error { return e.cause }

// StreamFactory is a function that opens a new server-streaming RPC.
// It is called once per (re)connect attempt.
type StreamFactory[T any] func(ctx context.Context) (*connect.ServerStreamForClient[T], error)

// StreamHandler is called for every message received from the stream.
// Returning a non-nil error terminates the stream and is NOT retried.
type StreamHandler[T any] func(msg *T) error

// RunServerStream opens a server-streaming RPC via factory and drives it with
// handler.  On a transient transport disconnect (CodeUnavailable) the stream
// is re-established using the same retryPolicy as retryInterceptor.
//
// Handler errors are never retried.  The context should carry the caller's
// cancellation signal; cancellation terminates the loop regardless of the
// retry budget.
func RunServerStream[T any](
	ctx context.Context,
	cfg Config,
	factory StreamFactory[T],
	handler StreamHandler[T],
) error {
	rp := newRetryPolicy(cfg)
	for {
		err := driveStream(ctx, factory, handler)
		if err == nil {
			return nil
		}
		// Never retry application-level handler errors.
		var he *handlerError
		if errors.As(err, &he) {
			return he.cause
		}
		if !rp.retriable(err) {
			return err
		}
		if advErr := rp.advance(ctx); advErr != nil {
			return advErr
		}
	}
}

// driveStream opens one stream instance and feeds each received message to
// handler until the stream ends, the context is cancelled, or an error
// occurs.
//
// Handler errors are wrapped in handlerError so RunServerStream can identify
// them.  The stream.Close error is captured via a named return and merged with
// the function's returned error so cleanup failures are never silently dropped.
func driveStream[T any](
	ctx context.Context,
	factory StreamFactory[T],
	handler StreamHandler[T],
) (retErr error) {
	stream, err := factory(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := stream.Close(); cerr != nil && retErr == nil {
			retErr = cerr
		}
	}()

	for stream.Receive() {
		if err := handler(stream.Msg()); err != nil {
			return &handlerError{cause: err}
		}
	}
	return stream.Err()
}
