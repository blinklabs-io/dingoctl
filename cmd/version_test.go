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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	lifecyclev1alpha1 "github.com/blinklabs-io/bark/proto/v1alpha1/lifecycle"
	"github.com/blinklabs-io/dingoctl/internal/version"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestVersionCommand_JSONOutputIncludesNodeVersion(t *testing.T) {
	fake, buf := setupLifecycleCommandTest(t)
	globalFlags.Output = "json"
	fake.statusResp = &lifecyclev1alpha1.GetStatusResponse{
		State:   lifecyclev1alpha1.LifecycleState_LIFECYCLE_STATE_RUNNING,
		Health:  lifecyclev1alpha1.HealthStatus_HEALTH_STATUS_HEALTHY,
		Uptime:  durationpb.New(30 * time.Second),
		Version: "2.1.0",
		Sync:    &lifecyclev1alpha1.SyncStatus{Synced: true},
	}

	cmd := newVersionCmd()
	cmd.SetArgs(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected version command error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("expected JSON output, unmarshal failed: %v\nraw: %s", err, buf.String())
	}
	if got["cli"] == "" {
		t.Fatalf("expected non-empty cli version in JSON output, got: %#v", got)
	}
	if got["node"] != "2.1.0" {
		t.Fatalf("expected node version 2.1.0, got: %#v", got["node"])
	}
	if _, hasErr := got["node_error"]; hasErr {
		t.Fatalf("did not expect node_error on successful status response, got: %#v", got["node_error"])
	}
	if fake.statusReqs != 1 {
		t.Fatalf("expected exactly one GetStatus request, got %d", fake.statusReqs)
	}
}

func TestVersionCommand_StatusErrorUsesNoRetryAndReturnsStructuredOutput(t *testing.T) {
	fake, buf := setupLifecycleCommandTest(t)
	globalFlags.Output = "json"
	fake.statusErr = connect.NewError(connect.CodeUnavailable, errors.New("try again"))

	cmd := newVersionCmd()
	cmd.SetArgs(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("version should degrade gracefully on status errors, got: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("expected JSON output, unmarshal failed: %v\nraw: %s", err, buf.String())
	}
	if got["cli"] == "" {
		t.Fatalf("expected non-empty cli version in JSON output, got: %#v", got)
	}
	nodeError, ok := got["node_error"].(string)
	if !ok || nodeError == "" {
		t.Fatalf("expected node_error in output, got: %#v", got["node_error"])
	}
	if got["node"] != nil {
		t.Fatalf("expected node to be absent/null on status error, got: %#v", got["node"])
	}
	if fake.statusReqs != 1 {
		t.Fatalf("expected exactly one GetStatus request under WithNoRetry, got %d", fake.statusReqs)
	}
}

func TestVersionResultString(t *testing.T) {
	cli := version.GetVersionString()
	node := "3.0.0"
	withNode := versionResult{CLI: cli, Node: &node}
	if got := withNode.String(); got == "" || !bytes.Contains([]byte(got), []byte("Node: 3.0.0")) {
		t.Fatalf("expected String() to include node version, got %q", got)
	}

	errText := "unable to get status: boom"
	withErr := versionResult{CLI: cli, NodeError: &errText}
	if got := withErr.String(); got == "" || !bytes.Contains([]byte(got), []byte("Node: <unable to get status: boom>")) {
		t.Fatalf("expected String() to include node error, got %q", got)
	}
}
