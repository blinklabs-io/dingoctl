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
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// testCfg returns a Config with minimal delays so retry tests run fast.
func testCfg(maxRetries int) Config {
	return Config{
		MaxRetries:     maxRetries,
		RetryBaseDelay: time.Millisecond,
		RetryMaxDelay:  10 * time.Millisecond,
		Timeout:        5 * time.Second,
	}
}

// newReq wraps an empty proto message to satisfy connect.AnyRequest.
func newReq() connect.AnyRequest { return connect.NewRequest(&emptypb.Empty{}) }

// sequence returns a UnaryFunc that runs through the given errors in order;
// nil means "return success".  callCount is incremented on each invocation.
func sequence(errs ...error) (connect.UnaryFunc, *int) {
	count := 0
	fn := func(_ context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		var err error
		if count < len(errs) {
			err = errs[count]
		}
		count++
		return nil, err
	}
	return fn, &count
}

func unavailable() error { return connect.NewError(connect.CodeUnavailable, errors.New("down")) }
func exhausted() error   { return connect.NewError(connect.CodeResourceExhausted, errors.New("rate")) }
func notFound() error    { return connect.NewError(connect.CodeNotFound, errors.New("gone")) }

// ── transientCode ────────────────────────────────────────────────────────────

func TestTransientCode(t *testing.T) {
	transient := []connect.Code{connect.CodeUnavailable, connect.CodeResourceExhausted}
	for _, c := range transient {
		if !transientCode(c) {
			t.Errorf("expected code %v to be transient", c)
		}
	}

	permanent := []connect.Code{
		connect.CodeNotFound,
		connect.CodeInvalidArgument,
		connect.CodePermissionDenied,
		connect.CodeUnauthenticated,
		connect.CodeAlreadyExists,
		connect.CodeInternal,
		connect.CodeUnimplemented,
		connect.CodeDeadlineExceeded,
		connect.CodeAborted,
		connect.CodeCanceled,
	}
	for _, c := range permanent {
		if transientCode(c) {
			t.Errorf("expected code %v to be permanent (non-retryable)", c)
		}
	}
}

// ── retryInterceptor ─────────────────────────────────────────────────────────

func TestRetryNoRetryOnSuccess(t *testing.T) {
	fn, calls := sequence(nil)
	wrapped := retryInterceptor(testCfg(3))(fn)

	_, err := wrapped(context.Background(), newReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *calls != 1 {
		t.Fatalf("expected 1 call, got %d", *calls)
	}
}

func TestRetrySucceedsOnSecondAttempt(t *testing.T) {
	fn, calls := sequence(unavailable(), nil)
	wrapped := retryInterceptor(testCfg(3))(fn)

	_, err := wrapped(context.Background(), newReq())
	if err != nil {
		t.Fatalf("expected success after one retry, got: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("expected 2 calls, got %d", *calls)
	}
}

func TestRetryResourceExhaustedIsRetried(t *testing.T) {
	fn, calls := sequence(exhausted(), nil)
	wrapped := retryInterceptor(testCfg(3))(fn)

	_, err := wrapped(context.Background(), newReq())
	if err != nil {
		t.Fatalf("expected success after retry on ResourceExhausted, got: %v", err)
	}
	if *calls != 2 {
		t.Fatalf("expected 2 calls, got %d", *calls)
	}
}

func TestRetryPermanentErrorNotRetried(t *testing.T) {
	fn, calls := sequence(notFound())
	wrapped := retryInterceptor(testCfg(3))(fn)

	_, err := wrapped(context.Background(), newReq())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("expected CodeNotFound, got %v", connect.CodeOf(err))
	}
	if *calls != 1 {
		t.Fatalf("expected exactly 1 call (no retries), got %d", *calls)
	}
}

func TestRetryExhausted(t *testing.T) {
	// MaxRetries=2 → 1 original + 2 retries = 3 total calls, then give up.
	fn, calls := sequence(unavailable(), unavailable(), unavailable(), nil)
	wrapped := retryInterceptor(testCfg(2))(fn)

	_, err := wrapped(context.Background(), newReq())
	if err == nil {
		t.Fatal("expected error after exhausting retries, got nil")
	}
	if connect.CodeOf(err) != connect.CodeUnavailable {
		t.Fatalf("expected CodeUnavailable, got %v", connect.CodeOf(err))
	}
	if *calls != 3 {
		t.Fatalf("expected 3 calls (1 + 2 retries), got %d", *calls)
	}
}

func TestRetryDisabledWithMaxRetriesZero(t *testing.T) {
	fn, calls := sequence(unavailable(), nil)
	wrapped := retryInterceptor(testCfg(0))(fn)

	_, err := wrapped(context.Background(), newReq())
	if err == nil {
		t.Fatal("expected error when retries disabled, got nil")
	}
	if *calls != 1 {
		t.Fatalf("expected exactly 1 call with MaxRetries=0, got %d", *calls)
	}
}

func TestRetryContextCancelledStopsLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	calls := 0
	fn := func(_ context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		calls++
		if calls == 1 {
			// Cancel the context before the retry sleep fires.
			cancel()
		}
		return nil, unavailable()
	}

	wrapped := retryInterceptor(testCfg(10))(fn)
	_, err := wrapped(ctx, newReq())

	if err == nil {
		t.Fatal("expected error after context cancellation")
	}
	// The error should be context.Canceled (from ctx.Err()).
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call before cancellation, got %d", calls)
	}
}

// ── timeoutInterceptor ───────────────────────────────────────────────────────

func TestTimeoutApplied(t *testing.T) {
	cfg := Config{Timeout: 10 * time.Millisecond}
	fn := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		// Block until context deadline fires.
		<-ctx.Done()
		return nil, connect.NewError(connect.CodeDeadlineExceeded, ctx.Err())
	}
	wrapped := timeoutInterceptor(cfg)(fn)

	start := time.Now()
	_, err := wrapped(context.Background(), newReq())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("timeout took too long: %v (expected ~10ms)", elapsed)
	}
}

func TestTimeoutZeroIsNoop(t *testing.T) {
	cfg := Config{Timeout: 0}
	called := false
	fn := func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		called = true
		if _, hasDeadline := ctx.Deadline(); hasDeadline {
			t.Error("expected no deadline when Timeout=0")
		}
		return nil, nil
	}
	wrapped := timeoutInterceptor(cfg)(fn)
	_, _ = wrapped(context.Background(), newReq())
	if !called {
		t.Fatal("underlying function was not called")
	}
}
