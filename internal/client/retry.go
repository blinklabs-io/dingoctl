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

// transientCode reports whether code represents a transient condition that is
// safe to retry for any call:
//
//   - Unavailable: transport-level failure; the server was unreachable and
//     never received the request, so replaying it has no side effects.
//   - ResourceExhausted: quota / rate-limit rejection; the server received and
//     immediately rejected the request before executing any handler logic, so
//     retrying after backoff is safe.
func transientCode(code connect.Code) bool {
	return code == connect.CodeUnavailable || code == connect.CodeResourceExhausted
}

// retryPolicy encapsulates the attempt counter and exponential-backoff state.
// It is shared between retryInterceptor (unary) and RunServerStream so both
// use exactly the same logic and are maintained in one place.
type retryPolicy struct {
	cfg     Config
	attempt int
	delay   time.Duration
}

func newRetryPolicy(cfg Config) *retryPolicy {
	return &retryPolicy{cfg: cfg, delay: cfg.RetryBaseDelay}
}

// retriable returns true when err is transient and the attempt budget allows
// at least one more try.
func (rp *retryPolicy) retriable(err error) bool {
	return err != nil &&
		rp.cfg.MaxRetries > 0 &&
		rp.attempt < rp.cfg.MaxRetries &&
		transientCode(connect.CodeOf(err))
}

// advance waits the current backoff delay (honouring ctx cancellation),
// increments the attempt counter, and doubles the delay up to RetryMaxDelay.
// The doubling uses a pre-cap check to avoid int64 overflow on large delays.
func (rp *retryPolicy) advance(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(rp.delay):
	}
	rp.attempt++
	if rp.delay > rp.cfg.RetryMaxDelay/2 {
		rp.delay = rp.cfg.RetryMaxDelay
	} else {
		rp.delay *= 2
	}
	return nil
}

// retryInterceptor returns a ConnectRPC unary interceptor that re-attempts
// failed calls using retryPolicy.  Context cancellation (e.g. --timeout)
// stops the loop between attempts so the total budget is respected.
func retryInterceptor(cfg Config) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			rp := newRetryPolicy(cfg)
			for {
				resp, err := next(ctx, req)
				if err == nil {
					return resp, nil
				}
				if !rp.retriable(err) {
					return nil, err
				}
				if advErr := rp.advance(ctx); advErr != nil {
					return nil, advErr
				}
			}
		}
	}
}

// timeoutInterceptor returns a ConnectRPC unary interceptor that adds a
// per-call deadline equal to cfg.Timeout.  If the incoming context already
// carries a tighter deadline the existing deadline wins (Go's context
// semantics guarantee the shorter of the two deadlines takes effect).
func timeoutInterceptor(cfg Config) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			if cfg.Timeout <= 0 {
				return next(ctx, req)
			}
			ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
			defer cancel()
			return next(ctx, req)
		}
	}
}
