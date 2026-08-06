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

package errs

import (
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"
)

// TestFormat_DecodesConnectError checks that a *connect.Error — the type
// every RPC failure from internal/client arrives as — maps to its friendly message.
func TestFormat_DecodesConnectError(t *testing.T) {
	err := connect.NewError(connect.CodeNotFound, errors.New(`unknown operation "abc"`))
	got := Format(err)
	want := `resource not found: unknown operation "abc"`
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

// TestFormat_WrappedConnectError verifies Format still decodes a *connect.Error
// found further down the chain via errors.As, not just at the top level.
func TestFormat_WrappedConnectError(t *testing.T) {
	inner := connect.NewError(connect.CodeFailedPrecondition, errors.New("another operation is already in progress"))
	wrapped := fmt.Errorf("truncate: %w", inner)
	got := Format(wrapped)
	want := "failed precondition: another operation is already in progress"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

// TestFormat_NonConnectError checks Format falls back to the raw error message
// when there's no ConnectRPC code to decode.
func TestFormat_NonConnectError(t *testing.T) {
	err := errors.New("boom")
	if got := Format(err); got != "boom" {
		t.Errorf("Format() = %q, want %q", got, "boom")
	}
}

// TestFormat_Nil checks that Format returns an empty string for a nil error,
// rather than panicking or printing a placeholder.
func TestFormat_Nil(t *testing.T) {
	if got := Format(nil); got != "" {
		t.Errorf("Format(nil) = %q, want empty string", got)
	}
}

// TestExitCode checks that each ConnectRPC code maps to its documented UNIX
// exit code, and that a non-connect error or nil falls back to 1 or 0.
func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"not a connect error", errors.New("boom"), 1},
		{"not found", connect.NewError(connect.CodeNotFound, errors.New("x")), 69},
		{"unavailable", connect.NewError(connect.CodeUnavailable, errors.New("x")), 69},
		{"unimplemented", connect.NewError(connect.CodeUnimplemented, errors.New("x")), 69},
		{"invalid argument", connect.NewError(connect.CodeInvalidArgument, errors.New("x")), 65},
		{"out of range", connect.NewError(connect.CodeOutOfRange, errors.New("x")), 65},
		{"unauthenticated", connect.NewError(connect.CodeUnauthenticated, errors.New("x")), 77},
		{"permission denied", connect.NewError(connect.CodePermissionDenied, errors.New("x")), 77},
		{"failed precondition falls through to default", connect.NewError(connect.CodeFailedPrecondition, errors.New("x")), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExitCode(tt.err); got != tt.want {
				t.Errorf("ExitCode() = %d, want %d", got, tt.want)
			}
		})
	}
}
