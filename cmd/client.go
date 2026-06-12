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

package cmd

import (
	"sync"

	"github.com/blinklabs-io/dingoctl/internal/client"
)

// sessionClient holds the single Client instance created during a dingoctl
// invocation.  All sub-commands that need a Bark connection call GetClient()
// and receive the same underlying HTTP transport, so TCP connections
// (including HTTP/2 streams) are pooled across the session.
var (
	sessionOnce   sync.Once
	sessionClient *client.Client
	sessionErr    error
)

// GetClient returns the session-scoped Bark client, constructing it on first
// call from the current globalFlags values.  Subsequent calls return the same
// instance without re-dialling.
//
// Call this from sub-command RunE functions (after persistentPreRun has
// hydrated globalFlags from flags/env/config).
func GetClient() (*client.Client, error) {
	sessionOnce.Do(func() {
		cfg := client.Config{
			Address:        globalFlags.Connect,
			TLS:            globalFlags.TLS,
			Insecure:       globalFlags.Insecure,
			CACert:         globalFlags.CACert,
			ClientCert:     globalFlags.ClientCert,
			ClientKey:      globalFlags.ClientKey,
			Timeout:        globalFlags.Timeout,
			MaxRetries:     client.DefaultConfig().MaxRetries,
			RetryBaseDelay: client.DefaultConfig().RetryBaseDelay,
			RetryMaxDelay:  client.DefaultConfig().RetryMaxDelay,
		}
		sessionClient, sessionErr = client.New(cfg)
	})
	return sessionClient, sessionErr
}
