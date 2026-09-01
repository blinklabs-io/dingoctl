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
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	lifecyclev1alpha1 "github.com/blinklabs-io/bark/proto/v1alpha1/lifecycle"
	lifecyclev1alpha1connect "github.com/blinklabs-io/bark/proto/v1alpha1/lifecycle/lifecyclev1alpha1connect"
	"github.com/blinklabs-io/dingoctl/internal/client"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ── statusResult.String() BlockRef rendering tests ──────────────────────

// TestStatusResultString_NoTipFields checks that when no tip fields are set,
// the String output does not include tip information.
func TestStatusResultString_NoTipFields(t *testing.T) {
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
	}
	out := r.String()
	if strings.Contains(out, "tip") {
		t.Errorf("expected no tip info when all fields are nil, got: %s", out)
	}
	if !strings.Contains(out, "synced") {
		t.Errorf("expected sync status, got: %s", out)
	}
}

// TestStatusResultString_SlotOnly checks that a tip with only slot set renders
// correctly, not dropping the tip data.
func TestStatusResultString_SlotOnly(t *testing.T) {
	slot := uint64(12345)
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
		TipSlot: &slot,
	}
	out := r.String()
	if !strings.Contains(out, "tip") {
		t.Errorf("expected tip info, got: %s", out)
	}
	if !strings.Contains(out, "slot: 12345") {
		t.Errorf("expected slot in output, got: %s", out)
	}
	if strings.Contains(out, "block:") || strings.Contains(out, "hash:") {
		t.Errorf("expected only slot in tip, got: %s", out)
	}
}

// TestStatusResultString_HashOnly checks that a tip with only hash set renders
// correctly. Bark's BlockRef contract permits hash-only tips.
func TestStatusResultString_HashOnly(t *testing.T) {
	hash := "deadbeef0123456789abcdef"
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
		TipHash: &hash,
	}
	out := r.String()
	if !strings.Contains(out, "tip") {
		t.Errorf("expected tip info for hash-only tip, got: %s", out)
	}
	if !strings.Contains(out, "hash: deadbeef") {
		t.Errorf("expected hash in output, got: %s", out)
	}
	if strings.Contains(out, "slot:") || strings.Contains(out, "block:") {
		t.Errorf("expected only hash in tip, got: %s", out)
	}
}

// TestStatusResultString_BlockNumberOnly checks that a tip with only block_number
// set renders correctly. This prevents dropping valid tip data when only block
// number is available.
func TestStatusResultString_BlockNumberOnly(t *testing.T) {
	blockNum := uint64(9876)
	r := statusResult{
		State:          "running",
		Health:         "healthy",
		Uptime:         10 * time.Minute,
		Version:        "1.0.0",
		Synced:         true,
		TipBlockNumber: &blockNum,
	}
	out := r.String()
	if !strings.Contains(out, "tip") {
		t.Errorf("expected tip info for block-number-only tip, got: %s", out)
	}
	if !strings.Contains(out, "block: 9876") {
		t.Errorf("expected block number in output, got: %s", out)
	}
	if strings.Contains(out, "slot:") || strings.Contains(out, "hash:") {
		t.Errorf("expected only block number in tip, got: %s", out)
	}
}

// TestStatusResultString_SlotAndHash checks that a tip with slot and hash renders
// both fields correctly with proper separators.
func TestStatusResultString_SlotAndHash(t *testing.T) {
	slot := uint64(12345)
	hash := "abcdef1234567890"
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
		TipSlot: &slot,
		TipHash: &hash,
	}
	out := r.String()
	if !strings.Contains(out, "slot: 12345") {
		t.Errorf("expected slot in output, got: %s", out)
	}
	if !strings.Contains(out, "hash: abcdef") {
		t.Errorf("expected hash in output, got: %s", out)
	}
	// Check that fields are separated by comma
	if !strings.Contains(out, ", ") {
		t.Errorf("expected comma separator between fields, got: %s", out)
	}
}

// TestStatusResultString_AllTipFields checks that when all three tip fields are
// present, they all render correctly with proper separators.
func TestStatusResultString_AllTipFields(t *testing.T) {
	slot := uint64(12345)
	blockNum := uint64(9876)
	hash := "fedcba9876543210"
	r := statusResult{
		State:          "running",
		Health:         "healthy",
		Uptime:         10 * time.Minute,
		Version:        "1.0.0",
		Synced:         true,
		TipSlot:        &slot,
		TipBlockNumber: &blockNum,
		TipHash:        &hash,
	}
	out := r.String()
	if !strings.Contains(out, "slot: 12345") {
		t.Errorf("expected slot in output, got: %s", out)
	}
	if !strings.Contains(out, "block: 9876") {
		t.Errorf("expected block number in output, got: %s", out)
	}
	if !strings.Contains(out, "hash: fedcba") {
		t.Errorf("expected hash in output, got: %s", out)
	}
}

// ── statusResult.TableRows() BlockRef rendering tests ───────────────────

// TestStatusResultTableRows_NoTipFields checks that when no tip fields are set,
// the table shows "N/A" for tip info.
func TestStatusResultTableRows_NoTipFields(t *testing.T) {
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
	}
	rows := r.TableRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// Tip info is at index 5
	tipInfo := rows[0][5]
	if tipInfo != "N/A" {
		t.Errorf("expected N/A for tip when no fields set, got: %s", tipInfo)
	}
}

// TestStatusResultTableRows_SlotOnly checks that a slot-only tip is rendered
// in the table, not displayed as N/A.
func TestStatusResultTableRows_SlotOnly(t *testing.T) {
	slot := uint64(12345)
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
		TipSlot: &slot,
	}
	rows := r.TableRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	tipInfo := rows[0][5]
	if !strings.Contains(tipInfo, "slot:12345") {
		t.Errorf("expected slot in tip info, got: %s", tipInfo)
	}
	if strings.Contains(tipInfo, "N/A") {
		t.Errorf("expected slot-only tip to not be N/A, got: %s", tipInfo)
	}
}

// TestStatusResultTableRows_HashOnly checks that a hash-only tip is rendered
// in the table. This validates the fix for dropping valid tip data.
func TestStatusResultTableRows_HashOnly(t *testing.T) {
	hash := "deadbeef0123456789"
	r := statusResult{
		State:   "running",
		Health:  "healthy",
		Uptime:  10 * time.Minute,
		Version: "1.0.0",
		Synced:  true,
		TipHash: &hash,
	}
	rows := r.TableRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	tipInfo := rows[0][5]
	if !strings.Contains(tipInfo, "hash:deadbeef") {
		t.Errorf("expected hash in tip info, got: %s", tipInfo)
	}
	if tipInfo == "N/A" {
		t.Errorf("hash-only tip should not be N/A, got: %s", tipInfo)
	}
}

// TestStatusResultTableRows_BlockNumberOnly checks that a block-number-only tip
// is rendered in the table. Bark's BlockRef permits this combination.
func TestStatusResultTableRows_BlockNumberOnly(t *testing.T) {
	blockNum := uint64(9876)
	r := statusResult{
		State:          "running",
		Health:         "healthy",
		Uptime:         10 * time.Minute,
		Version:        "1.0.0",
		Synced:         true,
		TipBlockNumber: &blockNum,
	}
	rows := r.TableRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	tipInfo := rows[0][5]
	if !strings.Contains(tipInfo, "block:9876") {
		t.Errorf("expected block number in tip info, got: %s", tipInfo)
	}
	if tipInfo == "N/A" {
		t.Errorf("block-number-only tip should not be N/A, got: %s", tipInfo)
	}
}

// TestStatusResultTableRows_AllTipFields checks that when all three tip fields
// are present, they all appear in the table output.
func TestStatusResultTableRows_AllTipFields(t *testing.T) {
	slot := uint64(12345)
	blockNum := uint64(9876)
	hash := "fedcba9876543210"
	r := statusResult{
		State:          "running",
		Health:         "healthy",
		Uptime:         10 * time.Minute,
		Version:        "1.0.0",
		Synced:         true,
		TipSlot:        &slot,
		TipBlockNumber: &blockNum,
		TipHash:        &hash,
	}
	rows := r.TableRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	tipInfo := rows[0][5]
	if !strings.Contains(tipInfo, "slot:12345") {
		t.Errorf("expected slot in tip info, got: %s", tipInfo)
	}
	if !strings.Contains(tipInfo, "block:9876") {
		t.Errorf("expected block number in tip info, got: %s", tipInfo)
	}
	if !strings.Contains(tipInfo, "hash:fedcba") {
		t.Errorf("expected hash in tip info, got: %s", tipInfo)
	}
}

// TestStatusResultTableRows_HashAndBlockNumber checks a combination without slot.
func TestStatusResultTableRows_HashAndBlockNumber(t *testing.T) {
	blockNum := uint64(5555)
	hash := "cafebabe"
	r := statusResult{
		State:          "running",
		Health:         "healthy",
		Uptime:         10 * time.Minute,
		Version:        "1.0.0",
		Synced:         true,
		TipBlockNumber: &blockNum,
		TipHash:        &hash,
	}
	rows := r.TableRows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	tipInfo := rows[0][5]
	if !strings.Contains(tipInfo, "block:5555") {
		t.Errorf("expected block number in tip info, got: %s", tipInfo)
	}
	if !strings.Contains(tipInfo, "hash:cafebabe") {
		t.Errorf("expected hash in tip info, got: %s", tipInfo)
	}
	if strings.Contains(tipInfo, "slot:") {
		t.Errorf("should not contain slot, got: %s", tipInfo)
	}
}

func TestStatusResultTableHeader_UsesTipColumnName(t *testing.T) {
	r := statusResult{}
	headers := r.TableHeader()
	if len(headers) < 6 {
		t.Fatalf("expected at least 6 headers, got %d", len(headers))
	}
	if headers[5] != "Tip" {
		t.Fatalf("expected sixth header to be Tip, got %q", headers[5])
	}
}

func TestStatusResultFromProto_HashOnlyTip(t *testing.T) {
	hash := "deadbeef"
	msg := &lifecyclev1alpha1.GetStatusResponse{
		State:   lifecyclev1alpha1.LifecycleState_LIFECYCLE_STATE_RUNNING,
		Health:  lifecyclev1alpha1.HealthStatus_HEALTH_STATUS_HEALTHY,
		Uptime:  durationpb.New(90 * time.Second),
		Version: "1.2.3",
		Sync: &lifecyclev1alpha1.SyncStatus{
			Synced: true,
			Tip: &lifecyclev1alpha1.BlockRef{
				Hash: &hash,
			},
		},
	}

	r := statusResultFromProto(msg)
	if r.TipHash == nil || *r.TipHash != hash {
		t.Fatalf("expected tip hash %q, got %#v", hash, r.TipHash)
	}
	if r.TipSlot != nil {
		t.Fatalf("expected nil tip slot for hash-only tip, got %v", *r.TipSlot)
	}
	if r.TipBlockNumber != nil {
		t.Fatalf("expected nil tip block number for hash-only tip, got %v", *r.TipBlockNumber)
	}
}

func TestStatusResultFromProto_BlockNumberOnlyTip(t *testing.T) {
	blockNum := uint64(77)
	msg := &lifecyclev1alpha1.GetStatusResponse{
		State:   lifecyclev1alpha1.LifecycleState_LIFECYCLE_STATE_RUNNING,
		Health:  lifecyclev1alpha1.HealthStatus_HEALTH_STATUS_HEALTHY,
		Uptime:  durationpb.New(90 * time.Second),
		Version: "1.2.3",
		Sync: &lifecyclev1alpha1.SyncStatus{
			Synced: true,
			Tip: &lifecyclev1alpha1.BlockRef{
				BlockNumber: &blockNum,
			},
		},
	}

	r := statusResultFromProto(msg)
	if r.TipBlockNumber == nil || *r.TipBlockNumber != blockNum {
		t.Fatalf("expected tip block number %d, got %#v", blockNum, r.TipBlockNumber)
	}
	if r.TipSlot != nil {
		t.Fatalf("expected nil tip slot for block-number-only tip, got %v", *r.TipSlot)
	}
	if r.TipHash != nil {
		t.Fatalf("expected nil tip hash for block-number-only tip, got %v", *r.TipHash)
	}
}

func TestLifecycleStateString_AllKnownAndUnknown(t *testing.T) {
	if got := lifecycleStateString(lifecyclev1alpha1.LifecycleState_LIFECYCLE_STATE_RUNNING); got != "running" {
		t.Fatalf("running mapping: got %q", got)
	}
	if got := lifecycleStateString(lifecyclev1alpha1.LifecycleState_LIFECYCLE_STATE_STOPPING); got != "stopping" {
		t.Fatalf("stopping mapping: got %q", got)
	}
	if got := lifecycleStateString(lifecyclev1alpha1.LifecycleState_LIFECYCLE_STATE_RESTARTING); got != "restarting" {
		t.Fatalf("restarting mapping: got %q", got)
	}
	if got := lifecycleStateString(lifecyclev1alpha1.LifecycleState(99)); got != "unknown" {
		t.Fatalf("unknown mapping: got %q", got)
	}
}

func TestHealthStatusString_AllKnownAndUnknown(t *testing.T) {
	if got := healthStatusString(lifecyclev1alpha1.HealthStatus_HEALTH_STATUS_HEALTHY); got != "healthy" {
		t.Fatalf("healthy mapping: got %q", got)
	}
	if got := healthStatusString(lifecyclev1alpha1.HealthStatus_HEALTH_STATUS_DEGRADED); got != "degraded" {
		t.Fatalf("degraded mapping: got %q", got)
	}
	if got := healthStatusString(lifecyclev1alpha1.HealthStatus_HEALTH_STATUS_UNHEALTHY); got != "unhealthy" {
		t.Fatalf("unhealthy mapping: got %q", got)
	}
	if got := healthStatusString(lifecyclev1alpha1.HealthStatus(99)); got != "unknown" {
		t.Fatalf("unknown mapping: got %q", got)
	}
}

func TestStopResultString_DeadlineOptional(t *testing.T) {
	withoutDeadline := stopResult{EffectiveTimeout: 30 * time.Second}
	if strings.Contains(withoutDeadline.String(), "deadline:") {
		t.Fatalf("expected no deadline text when deadline is nil, got %q", withoutDeadline.String())
	}

	d := time.Unix(1700000000, 0).UTC()
	withDeadline := stopResult{EffectiveTimeout: 30 * time.Second, Deadline: &d}
	if !strings.Contains(withDeadline.String(), "deadline:") {
		t.Fatalf("expected deadline text when deadline is set, got %q", withDeadline.String())
	}
}

func TestRestartResultString_DeadlineOptional(t *testing.T) {
	withoutDeadline := restartResult{EffectiveTimeout: 30 * time.Second}
	if strings.Contains(withoutDeadline.String(), "deadline:") {
		t.Fatalf("expected no deadline text when deadline is nil, got %q", withoutDeadline.String())
	}

	d := time.Unix(1700000000, 0).UTC()
	withDeadline := restartResult{EffectiveTimeout: 30 * time.Second, Deadline: &d}
	if !strings.Contains(withDeadline.String(), "deadline:") {
		t.Fatalf("expected deadline text when deadline is set, got %q", withDeadline.String())
	}
}

type fakeLifecycleService struct {
	lifecyclev1alpha1connect.UnimplementedLifecycleServiceHandler

	mu sync.Mutex

	stopErr  error
	stopResp *lifecyclev1alpha1.StopResponse
	stopReqs int

	restartErr  error
	restartResp *lifecyclev1alpha1.RestartResponse
	restartReqs int

	statusErr  error
	statusResp *lifecyclev1alpha1.GetStatusResponse
	statusReqs int
}

func (f *fakeLifecycleService) Stop(
	_ context.Context,
	_ *connect.Request[lifecyclev1alpha1.StopRequest],
) (*connect.Response[lifecyclev1alpha1.StopResponse], error) {
	f.mu.Lock()
	f.stopReqs++
	f.mu.Unlock()
	if f.stopErr != nil {
		return nil, f.stopErr
	}
	return connect.NewResponse(f.stopResp), nil
}

func (f *fakeLifecycleService) Restart(
	_ context.Context,
	_ *connect.Request[lifecyclev1alpha1.RestartRequest],
) (*connect.Response[lifecyclev1alpha1.RestartResponse], error) {
	f.mu.Lock()
	f.restartReqs++
	f.mu.Unlock()
	if f.restartErr != nil {
		return nil, f.restartErr
	}
	return connect.NewResponse(f.restartResp), nil
}

func (f *fakeLifecycleService) GetStatus(
	_ context.Context,
	_ *connect.Request[lifecyclev1alpha1.GetStatusRequest],
) (*connect.Response[lifecyclev1alpha1.GetStatusResponse], error) {
	f.mu.Lock()
	f.statusReqs++
	f.mu.Unlock()
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	return connect.NewResponse(f.statusResp), nil
}

func setupLifecycleCommandTest(t *testing.T) (*fakeLifecycleService, *bytes.Buffer) {
	t.Helper()

	fake := &fakeLifecycleService{}
	path, handler := lifecyclev1alpha1connect.NewLifecycleServiceHandler(fake)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	addr := strings.TrimPrefix(srv.URL, "https://")
	c, err := client.New(client.Config{
		Address:  addr,
		Insecure: true,
		Timeout:  2 * time.Second,
	})
	if err != nil {
		t.Fatalf("construct fake session client: %v", err)
	}

	sessionOnce.Do(func() {})
	sessionClient = c
	sessionErr = nil
	t.Cleanup(func() {
		sessionOnce = sync.Once{}
		sessionClient = nil
		sessionErr = nil
	})

	prevGlobalFlags := globalFlags
	globalFlags = GlobalFlags{Output: "text"}
	t.Cleanup(func() { globalFlags = prevGlobalFlags })

	var buf bytes.Buffer
	outputWriterForTest = &buf
	t.Cleanup(func() { outputWriterForTest = nil })

	return fake, &buf
}

func TestStopCommand_UsesNoRetryContext(t *testing.T) {
	fake, _ := setupLifecycleCommandTest(t)
	fake.stopErr = connect.NewError(connect.CodeUnavailable, errors.New("try again"))

	cmd := newStopCmd()
	cmd.SetArgs([]string{"--no-confirm"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected stop error")
	}
	if fake.stopReqs != 1 {
		t.Fatalf("expected exactly 1 stop request with no-retry context, got %d", fake.stopReqs)
	}
}

func TestRestartCommand_UsesNoRetryContext(t *testing.T) {
	fake, _ := setupLifecycleCommandTest(t)
	fake.restartErr = connect.NewError(connect.CodeUnavailable, errors.New("try again"))

	cmd := newRestartCmd()
	cmd.SetArgs([]string{"--no-confirm"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected restart error")
	}
	if fake.restartReqs != 1 {
		t.Fatalf("expected exactly 1 restart request with no-retry context, got %d", fake.restartReqs)
	}
}

func TestStopCommand_RejectsNegativeGracefulTimeout(t *testing.T) {
	fake, _ := setupLifecycleCommandTest(t)
	fake.stopResp = &lifecyclev1alpha1.StopResponse{}

	cmd := newStopCmd()
	cmd.SetArgs([]string{"--no-confirm", "--graceful-timeout", "-1s"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "must be non-negative") {
		t.Fatalf("expected non-negative timeout error, got %v", err)
	}
	if fake.stopReqs != 0 {
		t.Fatalf("expected no stop RPC for invalid timeout, got %d", fake.stopReqs)
	}
}

func TestRestartCommand_RejectsNegativeGracefulTimeout(t *testing.T) {
	fake, _ := setupLifecycleCommandTest(t)
	fake.restartResp = &lifecyclev1alpha1.RestartResponse{}

	cmd := newRestartCmd()
	cmd.SetArgs([]string{"--no-confirm", "--graceful-timeout", "-1s"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.ExecuteContext(context.Background())
	if err == nil || !strings.Contains(err.Error(), "must be non-negative") {
		t.Fatalf("expected non-negative timeout error, got %v", err)
	}
	if fake.restartReqs != 0 {
		t.Fatalf("expected no restart RPC for invalid timeout, got %d", fake.restartReqs)
	}
}

func TestStatusCommand_MapsHashOnlyTipFromProto(t *testing.T) {
	fake, buf := setupLifecycleCommandTest(t)
	hash := "beadfeed"
	fake.statusResp = &lifecyclev1alpha1.GetStatusResponse{
		State:   lifecyclev1alpha1.LifecycleState_LIFECYCLE_STATE_RUNNING,
		Health:  lifecyclev1alpha1.HealthStatus_HEALTH_STATUS_HEALTHY,
		Uptime:  durationpb.New(5 * time.Second),
		Version: "2.0.0",
		Sync: &lifecyclev1alpha1.SyncStatus{
			Synced: true,
			Tip: &lifecyclev1alpha1.BlockRef{
				Hash: &hash,
			},
		},
		Deadline: timestamppb.New(time.Unix(1700000100, 0).UTC()),
	}

	cmd := newStatusCmd()
	cmd.SetArgs(nil)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected status command error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "hash: beadfeed") {
		t.Fatalf("expected hash-only tip in status output, got: %s", out)
	}
	if fake.statusReqs != 1 {
		t.Fatalf("expected exactly one get status request, got %d", fake.statusReqs)
	}
}
