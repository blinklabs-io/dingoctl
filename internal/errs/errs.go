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
// It maps gRPC status codes to human-readable messages and ensures
// subcommands exit with non-zero codes on failure.
package errs

import (
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// grpcMessages maps gRPC status codes to operator-friendly messages.
var grpcMessages = map[codes.Code]string{
	codes.Canceled:           "operation was cancelled",
	codes.Unknown:            "unknown error from server",
	codes.InvalidArgument:    "invalid argument",
	codes.DeadlineExceeded:   "operation timed out",
	codes.NotFound:           "resource not found",
	codes.AlreadyExists:      "resource already exists",
	codes.PermissionDenied:   "permission denied",
	codes.ResourceExhausted:  "resource exhausted",
	codes.FailedPrecondition: "failed precondition",
	codes.Aborted:            "operation aborted",
	codes.OutOfRange:         "value out of range",
	codes.Unimplemented:      "operation not implemented by server",
	codes.Internal:           "internal server error",
	codes.Unavailable:        "server unavailable — check --connect address",
	codes.DataLoss:           "data loss or corruption detected",
	codes.Unauthenticated:    "unauthenticated — check credentials",
}

// Format returns a single readable error line suitable for stderr output.
// If err wraps a gRPC status error the code is decoded; otherwise the
// raw message is returned unchanged.
func Format(err error) string {
	if err == nil {
		return ""
	}
	// Walk the error chain looking for a gRPC status error.
	for e := err; e != nil; e = errors.Unwrap(e) {
		if st, ok := status.FromError(e); ok && st.Code() != codes.OK {
			return formatStatus(st)
		}
	}
	return err.Error()
}

func formatStatus(st *status.Status) string {
	if msg, ok := grpcMessages[st.Code()]; ok {
		detail := st.Message()
		if detail != "" && detail != st.Code().String() {
			return fmt.Sprintf("%s: %s", msg, detail)
		}
		return msg
	}
	return st.Message()
}

// ExitCode returns a UNIX exit code for the given error.
// gRPC-specific codes get dedicated values; all other errors return 1.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	// Walk the error chain so wrapped gRPC errors are handled correctly.
	for e := err; e != nil; e = errors.Unwrap(e) {
		st, ok := status.FromError(e)
		if !ok || st.Code() == codes.OK {
			continue
		}
		switch st.Code() {
		case codes.Unauthenticated, codes.PermissionDenied:
			return 77 // EX_NOPERM
		case codes.NotFound, codes.Unavailable, codes.DeadlineExceeded, codes.Unimplemented:
			return 69 // EX_UNAVAILABLE
		case codes.InvalidArgument, codes.OutOfRange:
			return 65 // EX_DATAERR
		default:
			return 1
		}
	}
	return 1
}

// Die writes a formatted error to stderr and calls os.Exit with the
// appropriate exit code for err.
func Die(err error) {
	fmt.Fprintf(os.Stderr, "error: %s\n", Format(err))
	os.Exit(ExitCode(err))
}
