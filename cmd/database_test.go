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
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"connectrpc.com/connect"
	databasev1alpha1 "github.com/blinklabs-io/bark/proto/v1alpha1/database"
	databasev1alpha1connect "github.com/blinklabs-io/bark/proto/v1alpha1/database/databasev1alpha1connect"
	dingoclient "github.com/blinklabs-io/dingoctl/internal/client"
	"github.com/spf13/cobra"
)

// ── blockRefFromFlags ────────────────────────────────────────────────────

func newTestTruncateFlags() (*cobra.Command, *uint64, *string, *uint64) {
	cmd := &cobra.Command{Use: "start"}
	var (
		slot        uint64
		blockHash   string
		blockNumber uint64
	)
	cmd.Flags().Uint64Var(&slot, "slot", 0, "")
	cmd.Flags().StringVar(&blockHash, "block-hash", "", "")
	cmd.Flags().Uint64Var(&blockNumber, "block-number", 0, "")
	return cmd, &slot, &blockHash, &blockNumber
}

// TestBlockRefFromFlags_NoneSet checks that omitting --slot, --block-hash, and
// --block-number entirely is rejected as an error, not a zero-value BlockRef.
func TestBlockRefFromFlags_NoneSet(t *testing.T) {
	cmd, slot, hash, num := newTestTruncateFlags()
	if _, err := blockRefFromFlags(cmd, *slot, *hash, *num); err == nil {
		t.Fatal("expected error when no target flag is set")
	}
}

// TestBlockRefFromFlags_MultipleSet checks that setting more than one target
// flag at once is rejected, since exactly one target form is required.
func TestBlockRefFromFlags_MultipleSet(t *testing.T) {
	cmd, slot, hash, num := newTestTruncateFlags()
	if err := cmd.Flags().Set("slot", "100"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("block-number", "5"); err != nil {
		t.Fatal(err)
	}
	if _, err := blockRefFromFlags(cmd, *slot, *hash, *num); err == nil {
		t.Fatal("expected error when more than one target flag is set")
	}
}

// TestBlockRefFromFlags_SlotOnly checks that --slot alone produces a BlockRef
// with only Slot set, leaving Hash and BlockNumber nil.
func TestBlockRefFromFlags_SlotOnly(t *testing.T) {
	cmd, slot, hash, num := newTestTruncateFlags()
	if err := cmd.Flags().Set("slot", "42"); err != nil {
		t.Fatal(err)
	}
	ref, err := blockRefFromFlags(cmd, *slot, *hash, *num)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.GetSlot() != 42 {
		t.Errorf("Slot: got %d, want 42", ref.GetSlot())
	}
	if ref.Hash != nil || ref.BlockNumber != nil {
		t.Errorf("expected only Slot set, got %+v", ref)
	}
}

// TestBlockRefFromFlags_BlockHashOnly checks that --block-hash alone produces
// a BlockRef carrying that hex string as Hash.
func TestBlockRefFromFlags_BlockHashOnly(t *testing.T) {
	cmd, _, _, _ := newTestTruncateFlags()
	if err := cmd.Flags().Set("block-hash", "deadbeef"); err != nil {
		t.Fatal(err)
	}
	ref, err := blockRefFromFlags(cmd, 0, "deadbeef", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.GetHash() != "deadbeef" {
		t.Errorf("Hash: got %q, want %q", ref.GetHash(), "deadbeef")
	}
}

// TestBlockRefFromFlags_InvalidBlockHash checks that a non-hex --block-hash
// value is rejected before ever reaching the Truncate RPC.
func TestBlockRefFromFlags_InvalidBlockHash(t *testing.T) {
	cmd, _, _, _ := newTestTruncateFlags()
	if err := cmd.Flags().Set("block-hash", "not-hex"); err != nil {
		t.Fatal(err)
	}
	if _, err := blockRefFromFlags(cmd, 0, "not-hex", 0); err == nil {
		t.Fatal("expected error for non-hex --block-hash")
	}
}

// TestBlockRefFromFlags_EmptyBlockHash checks that --block-hash="" is
// rejected rather than deferred to the server as a valid (if odd) target:
// hex.DecodeString("") succeeds with a zero-length result, so this needs
// its own explicit check.
func TestBlockRefFromFlags_EmptyBlockHash(t *testing.T) {
	cmd, _, _, _ := newTestTruncateFlags()
	if err := cmd.Flags().Set("block-hash", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := blockRefFromFlags(cmd, 0, "", 0); err == nil {
		t.Fatal("expected error for empty --block-hash")
	}
}

// ── confirm ──────────────────────────────────────────────────────────────

// TestConfirm_NoConfirmSkipsPrompt checks that noConfirm=true short-circuits
// to (true, nil) without ever touching stdin.
func TestConfirm_NoConfirmSkipsPrompt(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	ok, err := confirm(cmd, "proceed?", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("expected confirm to return true when noConfirm is set")
	}
}

// TestConfirm_NonInteractiveRefusesToPrompt checks that confirm errors out on
// non-TTY stdin instead of blocking, so a script without --no-confirm fails loudly.
func TestConfirm_NonInteractiveRefusesToPrompt(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	cmd.SetIn(strings.NewReader("y\n"))
	if _, err := confirm(cmd, "proceed?", false); err == nil {
		t.Fatal("expected error refusing to prompt on non-interactive stdin")
	}
}

// ── status/type string mapping ───────────────────────────────────────────

// TestIsTerminalStatus checks that only COMPLETED/FAILED/CANCELLED count as
// terminal, so streamUntilTerminal knows exactly when to stop.
func TestIsTerminalStatus(t *testing.T) {
	cases := map[databasev1alpha1.OperationStatus]bool{
		databasev1alpha1.OperationStatus_OPERATION_STATUS_PENDING:   false,
		databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING:   false,
		databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED: true,
		databasev1alpha1.OperationStatus_OPERATION_STATUS_FAILED:    true,
		databasev1alpha1.OperationStatus_OPERATION_STATUS_CANCELLED: true,
	}
	for status, want := range cases {
		if got := isTerminalStatus(status); got != want {
			t.Errorf("isTerminalStatus(%v): got %v, want %v", status, got, want)
		}
	}
}

// TestOperationTypeString checks that every OperationType enum value maps to
// its lowercase display name, including the unspecified/zero value.
func TestOperationTypeString(t *testing.T) {
	cases := map[databasev1alpha1.OperationType]string{
		databasev1alpha1.OperationType_OPERATION_TYPE_SNAPSHOT:    "snapshot",
		databasev1alpha1.OperationType_OPERATION_TYPE_RESTORE:     "restore",
		databasev1alpha1.OperationType_OPERATION_TYPE_TRUNCATE:    "truncate",
		databasev1alpha1.OperationType_OPERATION_TYPE_VERIFY:      "verify",
		databasev1alpha1.OperationType_OPERATION_TYPE_UNSPECIFIED: "unspecified",
	}
	for typ, want := range cases {
		if got := operationTypeString(typ); got != want {
			t.Errorf("operationTypeString(%v): got %q, want %q", typ, got, want)
		}
	}
}

// ── printTerminalResult ──────────────────────────────────────────────────

// TestPrintTerminalResult_FailedReturnsError checks that a FAILED terminal
// progress produces a non-nil error, so a --wait'd command exits non-zero.
func TestPrintTerminalResult_FailedReturnsError(t *testing.T) {
	progress := &databasev1alpha1.OperationProgress{
		OperationId: "op1",
		Status:      databasev1alpha1.OperationStatus_OPERATION_STATUS_FAILED,
		Message:     "boom",
	}
	if err := printTerminalResult(progress, "", "truncate"); err == nil {
		t.Fatal("expected error for a FAILED terminal result")
	}
}

// TestPrintTerminalResult_CompletedReturnsNil checks that a COMPLETED terminal
// progress prints cleanly and returns no error.
func TestPrintTerminalResult_CompletedReturnsNil(t *testing.T) {
	progress := &databasev1alpha1.OperationProgress{
		OperationId: "op1",
		Status:      databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED,
	}
	if err := printTerminalResult(progress, "", "truncate"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestPrintTerminalResult_CancelledReturnsError checks that a CANCELLED
// terminal progress is treated as an error, not silently as success, so
// automation driving --wait can't mistake a cancellation for completion.
func TestPrintTerminalResult_CancelledReturnsError(t *testing.T) {
	progress := &databasev1alpha1.OperationProgress{
		OperationId: "op1",
		Status:      databasev1alpha1.OperationStatus_OPERATION_STATUS_CANCELLED,
	}
	if err := printTerminalResult(progress, "", "truncate"); err == nil {
		t.Fatal("expected error for a CANCELLED terminal result")
	}
}

// TestOperationStatusFromProto_ZeroBlocksRemovedIsPreserved checks that a
// truncate which legitimately removed zero blocks still reports
// blocks_removed=0 rather than dropping the field entirely.
func TestOperationStatusFromProto_ZeroBlocksRemovedIsPreserved(t *testing.T) {
	progress := &databasev1alpha1.OperationProgress{OperationId: "op1"}
	zero := uint64(0)
	r := operationStatusFromProto(progress, "", &zero)
	if r.BlocksRemoved == nil {
		t.Fatal("expected BlocksRemoved to be set to 0, got nil")
	}
	if *r.BlocksRemoved != 0 {
		t.Errorf("BlocksRemoved: got %d, want 0", *r.BlocksRemoved)
	}
}

// ── proto -> result mapping ───────────────────────────────────────────────

// TestSnapshotInfoFromProto checks that every SnapshotInfo field, including
// its nested BlockRef tip, maps onto snapshotInfoResult correctly.
func TestSnapshotInfoFromProto(t *testing.T) {
	hash := "abc123"
	slot := uint64(10)
	s := &databasev1alpha1.SnapshotInfo{
		SnapshotId:  "snap-1",
		Name:        "nightly",
		Description: "test snapshot",
		Tip:         &databasev1alpha1.BlockRef{Hash: &hash, Slot: &slot},
		SizeBytes:   1024,
		Checksum:    "sha256:deadbeef",
		Location:    "/var/lib/dingo/snapshots/snap-1",
	}
	got := snapshotInfoFromProto(s)
	if got.SnapshotID != "snap-1" || got.Name != "nightly" || got.SizeBytes != 1024 {
		t.Errorf("unexpected mapping: %+v", got)
	}
	if got.Tip.Hash != "abc123" || got.Tip.Slot != 10 {
		t.Errorf("unexpected tip mapping: %+v", got.Tip)
	}
}

// TestSnapshotListFromAvailableProto checks that a ListAvailableSnapshotsResponse
// maps to a snapshotListResult with all snapshots and the next-page token intact.
func TestSnapshotListFromAvailableProto(t *testing.T) {
	resp := &databasev1alpha1.ListAvailableSnapshotsResponse{
		Snapshots:     []*databasev1alpha1.SnapshotInfo{{SnapshotId: "a"}, {SnapshotId: "b"}},
		NextPageToken: "tok",
	}
	got := snapshotListFromAvailableProto(resp)
	if len(got.Snapshots) != 2 || got.NextPageToken != "tok" {
		t.Errorf("unexpected mapping: %+v", got)
	}
}

// TestDatabaseInfoFromProto checks that GetDatabaseInfoResponse's fields,
// including the optional current_operation_id pointer, map correctly.
func TestDatabaseInfoFromProto(t *testing.T) {
	opID := "op-9"
	resp := &databasev1alpha1.GetDatabaseInfoResponse{
		BlockCount:          5,
		SizeBytes:           2048,
		OldestSlot:          1,
		OperationInProgress: true,
		CurrentOperationId:  &opID,
	}
	got := databaseInfoFromProto(resp)
	if got.BlockCount != 5 || !got.OperationInProgress || got.CurrentOperationID != "op-9" {
		t.Errorf("unexpected mapping: %+v", got)
	}
}

// ── streamUntilTerminal (against a real Connect server-stream handler) ───

// fakeDatabaseServiceHandler implements only StreamOperationProgress;
// every other RPC falls back to UnimplementedDatabaseServiceHandler.
type fakeDatabaseServiceHandler struct {
	databasev1alpha1connect.UnimplementedDatabaseServiceHandler
	progressions []*databasev1alpha1.OperationProgress

	mu             sync.Mutex
	cancelledOpID  string
	cancelledCalls int
}

func (f *fakeDatabaseServiceHandler) StreamOperationProgress(
	_ context.Context,
	_ *connect.Request[databasev1alpha1.StreamOperationProgressRequest],
	stream *connect.ServerStream[databasev1alpha1.StreamOperationProgressResponse],
) error {
	for _, p := range f.progressions {
		if err := stream.Send(&databasev1alpha1.StreamOperationProgressResponse{Progress: p}); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeDatabaseServiceHandler) CancelOperation(
	_ context.Context,
	req *connect.Request[databasev1alpha1.CancelOperationRequest],
) (*connect.Response[databasev1alpha1.CancelOperationResponse], error) {
	f.mu.Lock()
	f.cancelledOpID = req.Msg.GetOperationId()
	f.cancelledCalls++
	f.mu.Unlock()
	return connect.NewResponse(&databasev1alpha1.CancelOperationResponse{
		Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_CANCELLED,
	}), nil
}

func newFakeDatabaseServiceClient(
	t *testing.T,
	progressions []*databasev1alpha1.OperationProgress,
) (databasev1alpha1connect.DatabaseServiceClient, *fakeDatabaseServiceHandler) {
	t.Helper()
	fake := &fakeDatabaseServiceHandler{progressions: progressions}
	path, handler := databasev1alpha1connect.NewDatabaseServiceHandler(fake)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return databasev1alpha1connect.NewDatabaseServiceClient(srv.Client(), srv.URL), fake
}

// TestStreamUntilTerminal_StopsAtTerminalStatus checks that the loop returns
// as soon as it sees COMPLETED, without draining messages sent after it.
func TestStreamUntilTerminal_StopsAtTerminalStatus(t *testing.T) {
	client, _ := newFakeDatabaseServiceClient(t, []*databasev1alpha1.OperationProgress{
		{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING, ProgressPercent: 10},
		{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING, ProgressPercent: 50},
		{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED, ProgressPercent: 100},
		// Should never be reached: streamUntilTerminal must stop at the
		// first terminal status rather than draining the whole stream.
		{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING, ProgressPercent: 0},
	})

	got, err := streamUntilTerminal(context.Background(), context.Background(), dingoclient.Config{}, client, "op1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GetStatus() != databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED {
		t.Errorf("status: got %v, want COMPLETED", got.GetStatus())
	}
	if got.GetProgressPercent() != 100 {
		t.Errorf("progress_percent: got %v, want 100", got.GetProgressPercent())
	}
}

// TestStreamUntilTerminal_StreamEndsWithoutTerminalStatus checks that a
// stream closing early (last status still RUNNING, not COMPLETED/FAILED/
// CANCELLED) surfaces an error instead of being reported as success --
// restore/truncate automation driven by --wait must not be told a
// destructive operation finished when it never reached a terminal status.
func TestStreamUntilTerminal_StreamEndsWithoutTerminalStatus(t *testing.T) {
	client, _ := newFakeDatabaseServiceClient(t, []*databasev1alpha1.OperationProgress{
		{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING, ProgressPercent: 30},
	})

	got, err := streamUntilTerminal(context.Background(), context.Background(), dingoclient.Config{}, client, "op1")
	if err == nil {
		t.Fatal("expected error for a stream that closed before reaching a terminal status")
	}
	if got.GetStatus() != databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING {
		t.Errorf("status: got %v, want RUNNING", got.GetStatus())
	}
}

// TestStreamUntilTerminal_EmptyStreamReturnsError checks that a stream
// closing cleanly without ever reporting a single progress update surfaces
// an error, rather than the caller rendering a misleading
// status=unspecified progress=0% success.
func TestStreamUntilTerminal_EmptyStreamReturnsError(t *testing.T) {
	client, _ := newFakeDatabaseServiceClient(t, nil)

	got, err := streamUntilTerminal(context.Background(), context.Background(), dingoclient.Config{}, client, "op1")
	if err == nil {
		t.Fatal("expected error for a stream that reported no progress")
	}
	if got != nil {
		t.Errorf("expected nil progress, got %+v", got)
	}
}

// TestStreamUntilTerminal_RealInterruptRequestsCancel simulates a Ctrl+C
// (parentCtx already cancelled) mid-operation and checks CancelOperation
// fires. The fake server here closes its stream immediately after its one
// canned RUNNING update regardless of context state (see the file-level
// comment on TestShouldRequestCancelOnInterrupt_RealInterrupt), so the
// stream context (ctx, deliberately left un-cancelled here to isolate the
// cancel-request side effect from ctx.Err() handling) never itself reports
// cancellation -- meaning this closes on a non-terminal status, same as any
// other stream that ends before COMPLETED/FAILED/CANCELLED, and now
// correctly surfaces that as an error too.
func TestStreamUntilTerminal_RealInterruptRequestsCancel(t *testing.T) {
	client, fake := newFakeDatabaseServiceClient(t, []*databasev1alpha1.OperationProgress{
		{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING, ProgressPercent: 10},
	})

	parentCtx, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	if _, err := streamUntilTerminal(context.Background(), parentCtx, dingoclient.Config{}, client, "op1"); err == nil {
		t.Fatal("expected an error: stream closed before reaching a terminal status")
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.cancelledCalls != 1 {
		t.Errorf("CancelOperation calls: got %d, want 1", fake.cancelledCalls)
	}
	if fake.cancelledOpID != "op1" {
		t.Errorf("cancelled operation_id: got %q, want %q", fake.cancelledOpID, "op1")
	}
}

// TestShouldRequestCancelOnInterrupt_RealInterrupt checks that a cancelled
// parentCtx (a real Ctrl+C) is reported as "should request cancel".
//
// This and the WaitTimeoutOnly test below exercise
// shouldRequestCancelOnInterrupt directly rather than through a full
// streamUntilTerminal call: faithfully simulating "--wait-timeout expires
// while the stream is genuinely still open" against a real or fake server
// would need its own synchronization machinery, since the fake handler here
// sends its canned messages and closes immediately. The decision itself is a
// two-line pure function, so testing it directly is simpler and just as faithful.
func TestShouldRequestCancelOnInterrupt_RealInterrupt(t *testing.T) {
	parentCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if !shouldRequestCancelOnInterrupt(parentCtx) {
		t.Error("expected true once parentCtx is cancelled (a real interrupt)")
	}
}

// TestShouldRequestCancelOnInterrupt_WaitTimeoutOnly checks that an untouched
// parentCtx (only --wait-timeout expiring) is reported as "do not cancel".
func TestShouldRequestCancelOnInterrupt_WaitTimeoutOnly(t *testing.T) {
	// parentCtx (cmd.Context()) is untouched by --wait-timeout expiring —
	// only the derived, timeout-wrapped context passed to the stream call
	// would be Done in that case.
	if shouldRequestCancelOnInterrupt(context.Background()) {
		t.Error("expected false when parentCtx was never cancelled")
	}
}
