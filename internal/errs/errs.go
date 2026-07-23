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

// Package errs provides consistent error handling for dingoctl.
//
// It maps ConnectRPC error codes to human-readable messages and ensures
// subcommands exit with non-zero codes on failure.
package errs

import (
	"errors"
	"fmt"
	"os"

	"connectrpc.com/connect"
)

// connectMessages maps ConnectRPC codes to operator-friendly messages.
// Every call in internal/client goes through a generated Connect client
// (the Connect protocol, not native gRPC), so RPC failures always arrive as
// *connect.Error with a connect.Code — never a google.golang.org/grpc/status
// error, even against a server that also speaks gRPC on the wire.
var connectMessages = map[connect.Code]string{
	connect.CodeCanceled:           "operation was cancelled",
	connect.CodeUnknown:            "unknown error from server",
	connect.CodeInvalidArgument:    "invalid argument",
	connect.CodeDeadlineExceeded:   "operation timed out",
	connect.CodeNotFound:           "resource not found",
	connect.CodeAlreadyExists:      "resource already exists",
	connect.CodePermissionDenied:   "permission denied",
	connect.CodeResourceExhausted:  "resource exhausted",
	connect.CodeFailedPrecondition: "failed precondition",
	connect.CodeAborted:            "operation aborted",
	connect.CodeOutOfRange:         "value out of range",
	connect.CodeUnimplemented:      "operation not implemented by server",
	connect.CodeInternal:           "internal server error",
	connect.CodeUnavailable:        "server unavailable — check --connect address",
	connect.CodeDataLoss:           "data loss or corruption detected",
	connect.CodeUnauthenticated:    "unauthenticated — check credentials",
}

// Format returns a single readable error line suitable for stderr output.
// If err wraps a ConnectRPC error the code is decoded; otherwise the raw
// message is returned unchanged.
func Format(err error) string {
	if err == nil {
		return ""
	}
	if connectErr, ok := asConnectError(err); ok {
		return formatConnectError(connectErr)
	}
	return err.Error()
}

func formatConnectError(connectErr *connect.Error) string {
	if msg, ok := connectMessages[connectErr.Code()]; ok {
		detail := connectErr.Message()
		if detail != "" && detail != connectErr.Code().String() {
			return fmt.Sprintf("%s: %s", msg, detail)
		}
		return msg
	}
	return connectErr.Message()
}

// asConnectError walks err's chain looking for a *connect.Error, the type
// every RPC failure from internal/client's generated service clients
// actually arrives as.
func asConnectError(err error) (*connect.Error, bool) {
	var connectErr *connect.Error
	ok := errors.As(err, &connectErr)
	return connectErr, ok
}

// ExitCode returns a UNIX exit code for the given error.
// ConnectRPC-specific codes get dedicated values; all other errors return 1.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	connectErr, ok := asConnectError(err)
	if !ok {
		return 1
	}
	switch connectErr.Code() {
	case connect.CodeUnauthenticated, connect.CodePermissionDenied:
		return 77 // EX_NOPERM
	case connect.CodeNotFound, connect.CodeUnavailable,
		connect.CodeDeadlineExceeded, connect.CodeUnimplemented:
		return 69 // EX_UNAVAILABLE
	case connect.CodeInvalidArgument, connect.CodeOutOfRange:
		return 65 // EX_DATAERR
	default:
		return 1
	}
}

// Die writes a formatted error to stderr and calls os.Exit with the
// appropriate exit code for err.
func Die(err error) {
	fmt.Fprintf(os.Stderr, "error: %s\n", Format(err))
	os.Exit(ExitCode(err))
}
