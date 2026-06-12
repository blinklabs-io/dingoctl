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

// transientCode returns true when the error code is a transient condition
// that is safe to retry on idempotent calls:
//
//   - Unavailable: server was temporarily unreachable; the request never
//     reached the handler so retrying is always safe.
//   - ResourceExhausted: server-side rate limit; safe to retry after backoff.
func transientCode(code connect.Code) bool {
	return code == connect.CodeUnavailable || code == connect.CodeResourceExhausted
}

// retryInterceptor returns a ConnectRPC unary interceptor that re-attempts
// failed calls with exponential backoff when the error is transient.
//
// Only unary calls are retried (streaming calls cannot be safely replayed at
// this layer).  The interceptor honours context cancellation between attempts
// so the global --timeout still applies.
func retryInterceptor(cfg Config) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			delay := cfg.RetryBaseDelay
			for attempt := 0; ; attempt++ {
				resp, err := next(ctx, req)
				if err == nil {
					return resp, nil
				}
				if cfg.MaxRetries == 0 || attempt >= cfg.MaxRetries || !transientCode(connect.CodeOf(err)) {
					return nil, err
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
				}
				delay *= 2
				if delay > cfg.RetryMaxDelay {
					delay = cfg.RetryMaxDelay
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
