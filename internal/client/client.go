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
	"net/http"

	"connectrpc.com/connect"
)

// Client is the session-scoped handle to a Bark node.  It owns the HTTP
// transport (and therefore the underlying connection pool) and bundles the
// ConnectRPC interceptors.  All exported methods are safe for concurrent use.
//
// Once the Bark proto stubs are published as a Go module, each per-service
// factory method can be written as a one-liner, e.g.:
//
//	func (c *Client) DatabaseService() *databasev1connect.DatabaseServiceClient {
//	    return databasev1connect.NewDatabaseServiceClient(c.httpClient, c.baseURL, c.connectOpts...)
//	}
type Client struct {
	httpClient  *http.Client
	baseURL     string
	connectOpts []connect.ClientOption
	cfg         Config
}

// New constructs a Client from cfg.  The underlying HTTP transport (and its
// connection pool) is created once here and reused across all service calls.
func New(cfg Config) (*Client, error) {
	transport, err := buildTransport(cfg)
	if err != nil {
		return nil, err
	}

	// Order matters: timeoutInterceptor is outermost, so its context.WithTimeout
	// wraps the entire retry loop.  All retry attempts share the same deadline,
	// which means --timeout is a total-time-budget, not a per-attempt limit.
	// retryInterceptor sits inside so it can observe and re-enter that context.
	opts := []connect.ClientOption{
		connect.WithInterceptors(
			timeoutInterceptor(cfg),
			retryInterceptor(cfg),
		),
	}

	return &Client{
		httpClient:  &http.Client{Transport: transport},
		baseURL:     serverBaseURL(cfg),
		connectOpts: opts,
		cfg:         cfg,
	}, nil
}

// HTTPClient returns the shared http.Client.  Use this together with BaseURL
// and ConnectOptions to construct a typed service client from generated stubs:
//
//	dbClient := databasev1connect.NewDatabaseServiceClient(
//	    c.HTTPClient(), c.BaseURL(), c.ConnectOptions()...,
//	)
func (c *Client) HTTPClient() *http.Client { return c.httpClient }

// BaseURL returns the scheme + authority for the Bark server
// (e.g. "https://bark.example.com:8080").
func (c *Client) BaseURL() string { return c.baseURL }

// ConnectOptions returns a copy of the connect.ClientOption values (timeout +
// retry interceptors) to pass to every generated client constructor.  A copy
// is returned so callers cannot mutate the shared interceptor chain.
func (c *Client) ConnectOptions() []connect.ClientOption {
	opts := make([]connect.ClientOption, len(c.connectOpts))
	copy(opts, c.connectOpts)
	return opts
}

// Config returns the Config that was used to create this client.
func (c *Client) Config() Config { return c.cfg }

// ── Per-service factories ────────────────────────────────────────────────────
//
// These methods return the three arguments required by every ConnectRPC
// generated client constructor (NewXxxServiceClient(httpClient, baseURL,
// opts...)).  They exist now so subcommands have a stable call-site to depend
// on; the return type will be changed to the concrete generated type once the
// Bark proto module is available.

// DatabaseService returns the connection parameters for the Bark
// DatabaseService.
func (c *Client) DatabaseService() (*http.Client, string, []connect.ClientOption) {
	return c.httpClient, c.baseURL, c.ConnectOptions()
}

// LifecycleService returns the connection parameters for the Bark
// LifecycleService.
func (c *Client) LifecycleService() (*http.Client, string, []connect.ClientOption) {
	return c.httpClient, c.baseURL, c.ConnectOptions()
}

// EventService returns the connection parameters for the Bark EventService.
func (c *Client) EventService() (*http.Client, string, []connect.ClientOption) {
	return c.httpClient, c.baseURL, c.ConnectOptions()
}
