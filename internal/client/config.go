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

// Package client provides a connection layer for talking to a Bark node over
// ConnectRPC.  It wraps the generated service clients with TLS, mTLS, retry,
// per-call timeouts, and session-scoped connection pooling so subcommands
// don't each reinvent the wheel.
package client

import "time"

// Config holds all parameters needed to open a connection to the Bark server.
type Config struct {
	// Address is the host:port of the Bark server (e.g. "localhost:8080").
	Address string
	// TLS enables TLS; if false a plain-text HTTP/2 (h2c) connection is used.
	TLS bool
	// Insecure skips TLS certificate verification (implies TLS).
	Insecure bool
	// CACert is the optional path to a PEM CA certificate for server
	// verification.  When empty the system certificate pool is used.
	CACert string
	// ClientCert and ClientKey are the paths to a PEM certificate/key pair
	// used for mutual TLS (mTLS).  Both must be set together.
	ClientCert string
	ClientKey  string
	// Timeout is applied to every RPC call that has no tighter deadline set
	// on the incoming context.
	Timeout time.Duration
	// MaxRetries is how many additional attempts are made for idempotent calls
	// that fail with a transient error.  0 disables retries.
	MaxRetries int
	// RetryBaseDelay is the starting delay for exponential backoff.
	RetryBaseDelay time.Duration
	// RetryMaxDelay caps the per-attempt backoff ceiling.
	RetryMaxDelay time.Duration
}

// DefaultConfig returns a Config with production-safe defaults.
func DefaultConfig() Config {
	return Config{
		Address:        "localhost:8080",
		Timeout:        30 * time.Second,
		MaxRetries:     3,
		RetryBaseDelay: 100 * time.Millisecond,
		RetryMaxDelay:  5 * time.Second,
	}
}
