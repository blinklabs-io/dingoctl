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
	"testing"
	"time"

	"github.com/spf13/viper"
)

// resetForTest wipes the global Viper state and globalFlags so that each
// test starts from a clean slate.  It re-installs the env-prefix and
// AutomaticEnv so that env-var tests work correctly.
func resetForTest() {
	viper.Reset()
	viper.SetEnvPrefix("DINGOCTL")
	viper.AutomaticEnv()
	globalFlags = GlobalFlags{}
}

// TestPersistentPreRun_AllFieldsHydrated verifies that every field of
// globalFlags is populated from Viper (not left at its zero value when a
// value is present in Viper).
func TestPersistentPreRun_AllFieldsHydrated(t *testing.T) {
	resetForTest()

	wantConnect := "node.example.com:9090"
	wantTimeout := 10 * time.Second

	viper.Set("connect", wantConnect)
	viper.Set("tls", true)
	viper.Set("insecure", false)
	viper.Set("timeout", wantTimeout)
	viper.Set("verbose", true)
	viper.Set("quiet", false)
	viper.Set("output", "json")

	if err := persistentPreRun(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if globalFlags.Connect != wantConnect {
		t.Errorf("Connect: got %q, want %q", globalFlags.Connect, wantConnect)
	}
	if !globalFlags.TLS {
		t.Error("TLS: got false, want true")
	}
	if globalFlags.Insecure {
		t.Error("Insecure: got true, want false")
	}
	if globalFlags.Timeout != wantTimeout {
		t.Errorf("Timeout: got %v, want %v", globalFlags.Timeout, wantTimeout)
	}
	if !globalFlags.Verbose {
		t.Error("Verbose: got false, want true")
	}
	if globalFlags.Quiet {
		t.Error("Quiet: got true, want false")
	}
	if globalFlags.Output != "json" {
		t.Errorf("Output: got %q, want %q", globalFlags.Output, "json")
	}
}

// TestPersistentPreRun_InsecureSetsTLSInBothSources verifies that when
// insecure=true, TLS is forced to true in both globalFlags *and* Viper so
// that every consumer — regardless of which source it reads — sees the
// canonical value.
func TestPersistentPreRun_InsecureSetsTLSInBothSources(t *testing.T) {
	resetForTest()

	viper.Set("insecure", true)
	viper.Set("tls", false) // must be overridden by the insecure implication
	viper.Set("output", "text")

	if err := persistentPreRun(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !globalFlags.TLS {
		t.Error("globalFlags.TLS: got false, want true (insecure implies tls)")
	}
	if !viper.GetBool("tls") {
		t.Error("viper tls: got false, want true (insecure implies tls)")
	}
}

// TestPersistentPreRun_InsecureViaEnv verifies that DINGOCTL_INSECURE=true
// (the env-var path) also triggers the insecure => tls implication.
func TestPersistentPreRun_InsecureViaEnv(t *testing.T) {
	resetForTest()
	t.Setenv("DINGOCTL_INSECURE", "true")
	viper.Set("output", "text")

	if err := persistentPreRun(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !globalFlags.Insecure {
		t.Error("globalFlags.Insecure: got false, want true")
	}
	if !globalFlags.TLS {
		t.Error("globalFlags.TLS: got false, want true (insecure implies tls)")
	}
	if !viper.GetBool("tls") {
		t.Error("viper tls: got false, want true (insecure implies tls)")
	}
}

// TestPersistentPreRun_ConnectFromEnv verifies that DINGOCTL_CONNECT is
// reflected in globalFlags.Connect after persistentPreRun runs.
func TestPersistentPreRun_ConnectFromEnv(t *testing.T) {
	resetForTest()
	t.Setenv("DINGOCTL_CONNECT", "envnode:7070")
	viper.Set("output", "text")

	if err := persistentPreRun(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if globalFlags.Connect != "envnode:7070" {
		t.Errorf("Connect: got %q, want %q", globalFlags.Connect, "envnode:7070")
	}
}

// TestPersistentPreRun_TimeoutFromEnv verifies that DINGOCTL_TIMEOUT is
// reflected in globalFlags.Timeout after persistentPreRun runs.
func TestPersistentPreRun_TimeoutFromEnv(t *testing.T) {
	resetForTest()
	t.Setenv("DINGOCTL_TIMEOUT", "5s")
	viper.Set("output", "text")

	if err := persistentPreRun(nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if globalFlags.Timeout != 5*time.Second {
		t.Errorf("Timeout: got %v, want 5s", globalFlags.Timeout)
	}
}

// TestPersistentPreRun_InvalidOutput verifies that an unrecognised output
// format value causes persistentPreRun to return an error.
func TestPersistentPreRun_InvalidOutput(t *testing.T) {
	resetForTest()
	viper.Set("output", "table")

	err := persistentPreRun(nil, nil)
	if err == nil {
		t.Fatal("expected error for invalid output format, got nil")
	}
}
