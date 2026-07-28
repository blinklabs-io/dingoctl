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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	databasev1alpha1 "github.com/blinklabs-io/bark/proto/v1alpha1/database"
	databasev1alpha1connect "github.com/blinklabs-io/bark/proto/v1alpha1/database/databasev1alpha1connect"
	"github.com/blinklabs-io/dingoctl/internal/client"
)

// fakeFullDatabaseService implements every DatabaseService RPC
// cmd/database.go calls, unlike database_test.go's narrower
// fakeDatabaseServiceHandler (which only backs the streamUntilTerminal unit
// tests). Each RPC returns whatever canned response/error a test sets on the
// corresponding field, and records the last request it received so tests can
// assert the right RPC was called with the right parameters (dingoctl#5's
// "each command issues the right Bark RPC").
type fakeFullDatabaseService struct {
	databasev1alpha1connect.UnimplementedDatabaseServiceHandler

	mu sync.Mutex

	createSnapshotResp *databasev1alpha1.CreateSnapshotResponse
	createSnapshotErr  error
	lastCreateSnapshot *databasev1alpha1.CreateSnapshotRequest

	getSnapshotStatusResp *databasev1alpha1.GetSnapshotStatusResponse
	getSnapshotStatusErr  error

	listAvailableSnapshotsResp *databasev1alpha1.ListAvailableSnapshotsResponse
	listAvailableSnapshotsErr  error
	lastListAvailableSnapshots *databasev1alpha1.ListAvailableSnapshotsRequest

	deleteSnapshotResp     *databasev1alpha1.DeleteSnapshotResponse
	deleteSnapshotErr      error
	lastDeleteSnapshot     *databasev1alpha1.DeleteSnapshotRequest
	deleteSnapshotAttempts int

	verifySnapshotResp *databasev1alpha1.VerifySnapshotResponse
	verifySnapshotErr  error
	lastVerifySnapshot *databasev1alpha1.VerifySnapshotRequest

	restoreResp *databasev1alpha1.RestoreResponse
	restoreErr  error
	lastRestore *databasev1alpha1.RestoreRequest

	getRestoreStatusResp *databasev1alpha1.GetRestoreStatusResponse
	getRestoreStatusErr  error

	truncateResp *databasev1alpha1.TruncateResponse
	truncateErr  error
	lastTruncate *databasev1alpha1.TruncateRequest

	getTruncateStatusResp *databasev1alpha1.GetTruncateStatusResponse
	getTruncateStatusErr  error

	getDatabaseInfoResp *databasev1alpha1.GetDatabaseInfoResponse
	getDatabaseInfoErr  error

	getOperationHistoryResp *databasev1alpha1.GetOperationHistoryResponse
	getOperationHistoryErr  error

	cancelOperationResp *databasev1alpha1.CancelOperationResponse
	cancelOperationErr  error
	lastCancelOperation *databasev1alpha1.CancelOperationRequest

	// streamProgressions is sent, in order, by StreamOperationProgress
	// regardless of which operation_id was requested — tests only ever
	// track one operation at a time, so no per-ID routing is needed.
	streamProgressions []*databasev1alpha1.OperationProgress

	// blockStreamUntilCancelled, when true, makes StreamOperationProgress
	// ignore streamProgressions and instead block until its request
	// context is cancelled — simulating a --wait'd stream that's still
	// open when Ctrl+C arrives, rather than one that finishes on its own.
	// streamStarted is closed the moment the RPC is actually invoked, so a
	// test knows it's safe to cancel the client context without racing
	// the call's own setup.
	blockStreamUntilCancelled bool
	streamStarted             chan struct{}
	streamStartedOnce         sync.Once
}

func (f *fakeFullDatabaseService) CreateSnapshot(
	_ context.Context,
	req *connect.Request[databasev1alpha1.CreateSnapshotRequest],
) (*connect.Response[databasev1alpha1.CreateSnapshotResponse], error) {
	f.mu.Lock()
	f.lastCreateSnapshot = req.Msg
	f.mu.Unlock()
	if f.createSnapshotErr != nil {
		return nil, f.createSnapshotErr
	}
	return connect.NewResponse(f.createSnapshotResp), nil
}

func (f *fakeFullDatabaseService) GetSnapshotStatus(
	_ context.Context,
	_ *connect.Request[databasev1alpha1.GetSnapshotStatusRequest],
) (*connect.Response[databasev1alpha1.GetSnapshotStatusResponse], error) {
	if f.getSnapshotStatusErr != nil {
		return nil, f.getSnapshotStatusErr
	}
	return connect.NewResponse(f.getSnapshotStatusResp), nil
}

func (f *fakeFullDatabaseService) ListAvailableSnapshots(
	_ context.Context,
	req *connect.Request[databasev1alpha1.ListAvailableSnapshotsRequest],
) (*connect.Response[databasev1alpha1.ListAvailableSnapshotsResponse], error) {
	f.mu.Lock()
	f.lastListAvailableSnapshots = req.Msg
	f.mu.Unlock()
	if f.listAvailableSnapshotsErr != nil {
		return nil, f.listAvailableSnapshotsErr
	}
	return connect.NewResponse(f.listAvailableSnapshotsResp), nil
}

func (f *fakeFullDatabaseService) DeleteSnapshot(
	_ context.Context,
	req *connect.Request[databasev1alpha1.DeleteSnapshotRequest],
) (*connect.Response[databasev1alpha1.DeleteSnapshotResponse], error) {
	f.mu.Lock()
	f.lastDeleteSnapshot = req.Msg
	f.deleteSnapshotAttempts++
	f.mu.Unlock()
	if f.deleteSnapshotErr != nil {
		return nil, f.deleteSnapshotErr
	}
	return connect.NewResponse(f.deleteSnapshotResp), nil
}

func (f *fakeFullDatabaseService) VerifySnapshot(
	_ context.Context,
	req *connect.Request[databasev1alpha1.VerifySnapshotRequest],
) (*connect.Response[databasev1alpha1.VerifySnapshotResponse], error) {
	f.mu.Lock()
	f.lastVerifySnapshot = req.Msg
	f.mu.Unlock()
	if f.verifySnapshotErr != nil {
		return nil, f.verifySnapshotErr
	}
	return connect.NewResponse(f.verifySnapshotResp), nil
}

func (f *fakeFullDatabaseService) Restore(
	_ context.Context,
	req *connect.Request[databasev1alpha1.RestoreRequest],
) (*connect.Response[databasev1alpha1.RestoreResponse], error) {
	f.mu.Lock()
	f.lastRestore = req.Msg
	f.mu.Unlock()
	if f.restoreErr != nil {
		return nil, f.restoreErr
	}
	return connect.NewResponse(f.restoreResp), nil
}

func (f *fakeFullDatabaseService) GetRestoreStatus(
	_ context.Context,
	_ *connect.Request[databasev1alpha1.GetRestoreStatusRequest],
) (*connect.Response[databasev1alpha1.GetRestoreStatusResponse], error) {
	if f.getRestoreStatusErr != nil {
		return nil, f.getRestoreStatusErr
	}
	return connect.NewResponse(f.getRestoreStatusResp), nil
}

func (f *fakeFullDatabaseService) Truncate(
	_ context.Context,
	req *connect.Request[databasev1alpha1.TruncateRequest],
) (*connect.Response[databasev1alpha1.TruncateResponse], error) {
	f.mu.Lock()
	f.lastTruncate = req.Msg
	f.mu.Unlock()
	if f.truncateErr != nil {
		return nil, f.truncateErr
	}
	return connect.NewResponse(f.truncateResp), nil
}

func (f *fakeFullDatabaseService) GetTruncateStatus(
	_ context.Context,
	_ *connect.Request[databasev1alpha1.GetTruncateStatusRequest],
) (*connect.Response[databasev1alpha1.GetTruncateStatusResponse], error) {
	if f.getTruncateStatusErr != nil {
		return nil, f.getTruncateStatusErr
	}
	return connect.NewResponse(f.getTruncateStatusResp), nil
}

func (f *fakeFullDatabaseService) GetDatabaseInfo(
	_ context.Context,
	_ *connect.Request[databasev1alpha1.GetDatabaseInfoRequest],
) (*connect.Response[databasev1alpha1.GetDatabaseInfoResponse], error) {
	if f.getDatabaseInfoErr != nil {
		return nil, f.getDatabaseInfoErr
	}
	return connect.NewResponse(f.getDatabaseInfoResp), nil
}

func (f *fakeFullDatabaseService) GetOperationHistory(
	_ context.Context,
	_ *connect.Request[databasev1alpha1.GetOperationHistoryRequest],
) (*connect.Response[databasev1alpha1.GetOperationHistoryResponse], error) {
	if f.getOperationHistoryErr != nil {
		return nil, f.getOperationHistoryErr
	}
	return connect.NewResponse(f.getOperationHistoryResp), nil
}

func (f *fakeFullDatabaseService) CancelOperation(
	_ context.Context,
	req *connect.Request[databasev1alpha1.CancelOperationRequest],
) (*connect.Response[databasev1alpha1.CancelOperationResponse], error) {
	f.mu.Lock()
	f.lastCancelOperation = req.Msg
	f.mu.Unlock()
	if f.cancelOperationErr != nil {
		return nil, f.cancelOperationErr
	}
	return connect.NewResponse(f.cancelOperationResp), nil
}

func (f *fakeFullDatabaseService) StreamOperationProgress(
	ctx context.Context,
	_ *connect.Request[databasev1alpha1.StreamOperationProgressRequest],
	stream *connect.ServerStream[databasev1alpha1.StreamOperationProgressResponse],
) error {
	if f.blockStreamUntilCancelled {
		// Guarded rather than assumed-initialized: a future test that sets
		// blockStreamUntilCancelled without also setting streamStarted
		// would otherwise panic on close(nil).
		f.streamStartedOnce.Do(func() {
			if f.streamStarted != nil {
				close(f.streamStarted)
			}
		})
		<-ctx.Done()
		return ctx.Err()
	}
	for _, p := range f.streamProgressions {
		if err := stream.Send(&databasev1alpha1.StreamOperationProgressResponse{Progress: p}); err != nil {
			return err
		}
	}
	return nil
}

// setupDatabaseCommandTest wires a fakeFullDatabaseService behind a real
// (TLS, so the client's normal HTTP/2 transport works without needing h2c)
// httptest server, points the package-level session client at it, and
// captures GetOutputPrinter's output into the returned buffer. Every piece
// of global state it touches (globalFlags, the session client singleton,
// outputWriterForTest) is restored via t.Cleanup, and — because these are
// all package-level globals — tests using this helper must not run with
// t.Parallel().
func setupDatabaseCommandTest(t *testing.T) (*fakeFullDatabaseService, *bytes.Buffer) {
	t.Helper()

	fake := &fakeFullDatabaseService{}
	path, handler := databasev1alpha1connect.NewDatabaseServiceHandler(fake)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	addr := strings.TrimPrefix(srv.URL, "https://")
	c, err := client.New(client.Config{
		Address:  addr,
		Insecure: true,
		Timeout:  5 * time.Second,
	})
	if err != nil {
		t.Fatalf("construct fake session client: %v", err)
	}

	// sessionOnce/sessionClient/sessionErr are the same package-level
	// singleton GetClient() itself builds from lazily; consuming the Once
	// here means GetClient() returns this fake client instead of dialling
	// globalFlags.Connect for real.
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

func mustExecute(t *testing.T, args []string) error {
	t.Helper()
	return mustExecuteContext(t, context.Background(), args)
}

// mustExecuteContext is mustExecute with a caller-supplied context, letting
// a test drive cmd.Context() the same way root.go's Execute() does (via
// signal.NotifyContext) — needed to exercise Ctrl+C-during-a-stream
// end-to-end, through the real command dispatch, rather than only the
// extracted helper functions it relies on.
func mustExecuteContext(t *testing.T, ctx context.Context, args []string) error {
	t.Helper()
	cmd := newDatabaseCmd()
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return cmd.ExecuteContext(ctx)
}

// ── database info / status ───────────────────────────────────────────────

// TestDatabaseInfoCommand checks that "info" calls GetDatabaseInfo and
// GetOperationHistory, and renders both the core fields and last_operation.
func TestDatabaseInfoCommand(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	slot := uint64(100)
	blockNum := uint64(5)
	fake.getDatabaseInfoResp = &databasev1alpha1.GetDatabaseInfoResponse{
		Tip:        &databasev1alpha1.BlockRef{Slot: &slot, BlockNumber: &blockNum},
		BlockCount: 42,
		SizeBytes:  2048,
	}
	fake.getOperationHistoryResp = &databasev1alpha1.GetOperationHistoryResponse{
		Records: []*databasev1alpha1.OperationRecord{{
			OperationId: "op-prev",
			Type:        databasev1alpha1.OperationType_OPERATION_TYPE_SNAPSHOT,
			Status:      databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED,
		}},
	}

	if err := mustExecute(t, []string{"info"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "block_count: 42") {
		t.Errorf("output missing block_count: %s", out)
	}
	if !strings.Contains(out, "last_operation") {
		t.Errorf("output missing last_operation: %s", out)
	}
}

// TestDatabaseInfoCommand_JSONOutput checks that "info --output json" produces
// valid, parseable JSON with the expected field values.
func TestDatabaseInfoCommand_JSONOutput(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.getDatabaseInfoResp = &databasev1alpha1.GetDatabaseInfoResponse{BlockCount: 7}
	globalFlags.Output = "json"

	if err := mustExecute(t, []string{"info"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v (output: %s)", err, buf.String())
	}
	if decoded["block_count"] != float64(7) {
		t.Errorf("block_count: got %v, want 7", decoded["block_count"])
	}
}

// TestDatabaseStatusCommand_NoOperationInProgress checks that "status" reports
// operation_in_progress: false without ever opening a progress stream.
func TestDatabaseStatusCommand_NoOperationInProgress(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.getDatabaseInfoResp = &databasev1alpha1.GetDatabaseInfoResponse{OperationInProgress: false}

	if err := mustExecute(t, []string{"status"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "operation_in_progress: false") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestDatabaseStatusCommand_ByDefaultShowsOnlyCurrentSnapshot checks that
// "status" without --follow prints one progress snapshot and stops there.
func TestDatabaseStatusCommand_ByDefaultShowsOnlyCurrentSnapshot(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	opID := "op-x"
	fake.getDatabaseInfoResp = &databasev1alpha1.GetDatabaseInfoResponse{
		OperationInProgress: true,
		CurrentOperationId:  &opID,
	}
	fake.streamProgressions = []*databasev1alpha1.OperationProgress{
		{OperationId: opID, Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING, ProgressPercent: 50},
		{OperationId: opID, Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED, ProgressPercent: 100},
	}

	// Without --follow, the command must print exactly one snapshot of
	// progress (the first message) and stop, not drain the whole stream.
	if err := mustExecute(t, []string{"status"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "status=running") {
		t.Errorf("expected the first (running) status, got: %s", out)
	}
	if strings.Contains(out, "status=completed") {
		t.Errorf("must not stream past the first message without --follow, got: %s", out)
	}
}

// TestDatabaseStatusCommand_FollowStreamsUntilCompleted checks that "status
// --follow" keeps streaming progress through to the terminal status.
func TestDatabaseStatusCommand_FollowStreamsUntilCompleted(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	opID := "op-x"
	fake.getDatabaseInfoResp = &databasev1alpha1.GetDatabaseInfoResponse{
		OperationInProgress: true,
		CurrentOperationId:  &opID,
	}
	fake.streamProgressions = []*databasev1alpha1.OperationProgress{
		{OperationId: opID, Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING, ProgressPercent: 50},
		{OperationId: opID, Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED, ProgressPercent: 100},
	}

	if err := mustExecute(t, []string{"status", "--follow"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "status=completed") {
		t.Errorf("--follow must stream through to the terminal status, got: %s", buf.String())
	}
}

// TestDatabaseStatusCommand_FollowNonTextEOFBeforeTerminalErrorsButStillPrints
// checks that "status --follow --output json" still prints the last known
// update as a single JSON document when the server closes the stream
// cleanly without ever reporting a terminal status, but now also surfaces
// a non-zero error for that case — --follow promises to keep streaming
// until the operation finishes, so a clean EOF stuck on RUNNING/PENDING
// must not be reported as success (automation would otherwise be told the
// operation finished when it didn't). A naive "only print when the loop's
// own terminal/!follow check fires" gate would print nothing at all, so
// the last update must still be printed before returning that error.
func TestDatabaseStatusCommand_FollowNonTextEOFBeforeTerminalErrorsButStillPrints(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	globalFlags.Output = "json"
	opID := "op-x"
	fake.getDatabaseInfoResp = &databasev1alpha1.GetDatabaseInfoResponse{
		OperationInProgress: true,
		CurrentOperationId:  &opID,
	}
	fake.streamProgressions = []*databasev1alpha1.OperationProgress{
		{OperationId: opID, Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING, ProgressPercent: 30},
		{OperationId: opID, Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING, ProgressPercent: 60},
	}

	if err := mustExecute(t, []string{"status", "--follow"}); err == nil {
		t.Fatal("expected an error: --follow closed before reaching a terminal status")
	}

	dec := json.NewDecoder(buf)
	var result map[string]any
	if err := dec.Decode(&result); err != nil {
		t.Fatalf("expected one JSON document reporting the last update, got decode error: %v (output: %s)", err, buf.String())
	}
	if status, _ := result["status"].(string); status != "running" {
		t.Errorf("status: got %v, want running (the last update seen)", result["status"])
	}
	var second map[string]any
	if err := dec.Decode(&second); !errors.Is(err, io.EOF) {
		t.Errorf("expected exactly one JSON document, got a second value or unexpected error: %v", err)
	}
}

// ── snapshot create / list / delete / verify / status ────────────────────

// TestSnapshotCreateCommand checks that "snapshot create" sends name/description
// through to CreateSnapshot and renders the returned snapshot_id.
func TestSnapshotCreateCommand(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.createSnapshotResp = &databasev1alpha1.CreateSnapshotResponse{OperationId: "op1", SnapshotId: "snap1"}

	if err := mustExecute(t, []string{"snapshot", "create", "--name", "nightly", "--description", "desc"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.lastCreateSnapshot.GetName() != "nightly" || fake.lastCreateSnapshot.GetDescription() != "desc" {
		t.Errorf("unexpected request: %+v", fake.lastCreateSnapshot)
	}
	if !strings.Contains(buf.String(), "snap1") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestSnapshotCreateCommand_Wait checks that "snapshot create --wait" streams
// progress through to completion and renders the final completed status.
func TestSnapshotCreateCommand_Wait(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.createSnapshotResp = &databasev1alpha1.CreateSnapshotResponse{OperationId: "op1", SnapshotId: "snap1"}
	fake.streamProgressions = []*databasev1alpha1.OperationProgress{
		{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED, ProgressPercent: 100},
	}

	if err := mustExecute(t, []string{"snapshot", "create", "--wait"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "status=completed") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestSnapshotCreateCommand_WaitJSONOutputIsSingleDocument checks that a
// --wait'd stream with --output json prints exactly one JSON document, not
// one per progress update: multiple concatenated top-level values would
// not parse as a single document for a machine consumer piping into e.g. jq.
func TestSnapshotCreateCommand_WaitJSONOutputIsSingleDocument(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	globalFlags.Output = "json"
	fake.createSnapshotResp = &databasev1alpha1.CreateSnapshotResponse{OperationId: "op1", SnapshotId: "snap1"}
	fake.streamProgressions = []*databasev1alpha1.OperationProgress{
		{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING, ProgressPercent: 10},
		{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING, ProgressPercent: 50},
		{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED, ProgressPercent: 100},
	}

	if err := mustExecute(t, []string{"snapshot", "create", "--wait"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dec := json.NewDecoder(buf)
	var first map[string]any
	if err := dec.Decode(&first); err != nil {
		t.Fatalf("expected one valid JSON document, got decode error: %v (output: %s)", err, buf.String())
	}
	if status, _ := first["status"].(string); status != "completed" {
		t.Errorf("status: got %v, want completed (output: %s)", first["status"], buf.String())
	}
	var second map[string]any
	if err := dec.Decode(&second); !errors.Is(err, io.EOF) {
		t.Errorf(
			"expected exactly one JSON document (EOF after the first), got a second value or unexpected error: %v (output: %s)",
			err, buf.String(),
		)
	}
}

// TestSnapshotCreateCommand_WaitFailurePropagatesError checks that a FAILED
// snapshot operation surfaces as a command error containing the server's message.
func TestSnapshotCreateCommand_WaitFailurePropagatesError(t *testing.T) {
	fake, _ := setupDatabaseCommandTest(t)
	fake.createSnapshotResp = &databasev1alpha1.CreateSnapshotResponse{OperationId: "op1", SnapshotId: "snap1"}
	fake.streamProgressions = []*databasev1alpha1.OperationProgress{
		{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_FAILED, Message: "disk full"},
	}

	err := mustExecute(t, []string{"snapshot", "create", "--wait"})
	if err == nil {
		t.Fatal("expected an error for a FAILED snapshot")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error missing server message: %v", err)
	}
}

// TestSnapshotCreateCommand_WaitInterruptRequestsCancel drives a --wait'd
// command end-to-end and checks that cancelling cmd.Context() mid-stream reaches CancelOperation.
func TestSnapshotCreateCommand_WaitInterruptRequestsCancel(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.createSnapshotResp = &databasev1alpha1.CreateSnapshotResponse{OperationId: "op1", SnapshotId: "snap1"}
	fake.blockStreamUntilCancelled = true
	fake.streamStarted = make(chan struct{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- mustExecuteContext(t, ctx, []string{"snapshot", "create", "--wait"})
	}()

	select {
	case <-fake.streamStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("StreamOperationProgress was never called")
	}

	// Simulate Ctrl+C: cancel the exact context the command is running
	// under, the same one signal.NotifyContext would cancel in root.go.
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected an error when the --wait stream is interrupted")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command did not return after its context was cancelled")
	}
	if !strings.Contains(buf.String(), "Interrupted") {
		t.Errorf("expected an interrupt notice in output, got: %q", buf.String())
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastCancelOperation.GetOperationId() != "op1" {
		t.Errorf(
			"CancelOperation not requested for the interrupted operation: got %+v",
			fake.lastCancelOperation,
		)
	}
}

// TestSnapshotListCommand checks that "snapshot list --limit" sends page_size
// through to ListAvailableSnapshots and renders the returned snapshots.
func TestSnapshotListCommand(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.listAvailableSnapshotsResp = &databasev1alpha1.ListAvailableSnapshotsResponse{
		Snapshots: []*databasev1alpha1.SnapshotInfo{{SnapshotId: "a", Name: "nightly"}},
	}

	if err := mustExecute(t, []string{"snapshot", "list", "--limit", "5"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.lastListAvailableSnapshots.GetPageSize() != 5 {
		t.Errorf("page_size: got %d, want 5", fake.lastListAvailableSnapshots.GetPageSize())
	}
	if !strings.Contains(buf.String(), "snapshot_id=a") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestSnapshotDeleteCommand checks that "snapshot delete --no-confirm" calls
// DeleteSnapshot with the right snapshot_id and renders the deletion result.
func TestSnapshotDeleteCommand(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.deleteSnapshotResp = &databasev1alpha1.DeleteSnapshotResponse{}

	if err := mustExecute(t, []string{"snapshot", "delete", "snap1", "--no-confirm"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.lastDeleteSnapshot.GetSnapshotId() != "snap1" {
		t.Errorf("snapshot_id: got %q, want snap1", fake.lastDeleteSnapshot.GetSnapshotId())
	}
	if !strings.Contains(buf.String(), "Deleted snapshot snap1") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestSnapshotDeleteCommand_RequiresConfirmation checks that omitting
// --no-confirm on non-interactive stdin errors out without calling DeleteSnapshot.
func TestSnapshotDeleteCommand_RequiresConfirmation(t *testing.T) {
	fake, _ := setupDatabaseCommandTest(t)

	err := mustExecute(t, []string{"snapshot", "delete", "snap1"})
	if err == nil {
		t.Fatal("expected an error refusing to prompt on non-interactive stdin")
	}
	if fake.lastDeleteSnapshot != nil {
		t.Error("DeleteSnapshot RPC must not be called without confirmation")
	}
}

// TestSnapshotDeleteCommandDoesNotRetryOnTransientError guards against the
// destructive DeleteSnapshot RPC being retried by the shared retry policy:
// if the server actually deletes the snapshot but its response is lost to a
// transient error (e.g. CodeUnavailable), an automatic retry would then see
// NotFound and report a false failure even though the deletion succeeded.
// Unlike setupDatabaseCommandTest's harness (which leaves MaxRetries at its
// zero value, disabling retries entirely and so unable to distinguish
// "correctly opted out" from "retries were never possible here anyway"),
// this test builds its own client with retries genuinely enabled (short
// backoff so a regression doesn't slow the suite down) — a dropped
// client.WithNoRetry call would show up here as more than one attempt.
func TestSnapshotDeleteCommandDoesNotRetryOnTransientError(t *testing.T) {
	fake := &fakeFullDatabaseService{
		deleteSnapshotErr: connect.NewError(connect.CodeUnavailable, errors.New("transient")),
	}
	path, handler := databasev1alpha1connect.NewDatabaseServiceHandler(fake)
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	addr := strings.TrimPrefix(srv.URL, "https://")
	c, err := client.New(client.Config{
		Address:        addr,
		Insecure:       true,
		Timeout:        5 * time.Second,
		MaxRetries:     3,
		RetryBaseDelay: time.Millisecond,
		RetryMaxDelay:  5 * time.Millisecond,
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

	if err := mustExecute(t, []string{"snapshot", "delete", "snap1", "--no-confirm"}); err == nil {
		t.Fatal("expected the transient error to surface")
	}
	if fake.deleteSnapshotAttempts != 1 {
		t.Fatalf(
			"expected exactly 1 DeleteSnapshot attempt under client.WithNoRetry, got %d",
			fake.deleteSnapshotAttempts,
		)
	}
}

// TestSnapshotVerifyCommand checks that "snapshot verify" calls VerifySnapshot
// with the given snapshot_id and renders the returned operation_id.
func TestSnapshotVerifyCommand(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.verifySnapshotResp = &databasev1alpha1.VerifySnapshotResponse{OperationId: "op-verify"}

	if err := mustExecute(t, []string{"snapshot", "verify", "snap1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.lastVerifySnapshot.GetSnapshotId() != "snap1" {
		t.Errorf("snapshot_id: got %q, want snap1", fake.lastVerifySnapshot.GetSnapshotId())
	}
	if !strings.Contains(buf.String(), "op-verify") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestSnapshotStatusCommand checks that "snapshot status" calls
// GetSnapshotStatus and renders its progress plus the snapshot_id.
func TestSnapshotStatusCommand(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.getSnapshotStatusResp = &databasev1alpha1.GetSnapshotStatusResponse{
		Progress:   &databasev1alpha1.OperationProgress{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING},
		SnapshotId: "snap1",
	}

	if err := mustExecute(t, []string{"snapshot", "status", "op1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "snapshot_id=snap1") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// ── restore / restore status ─────────────────────────────────────────────

// TestRestoreCommand checks that "restore --no-confirm" calls Restore with
// the given snapshot_id.
func TestRestoreCommand(t *testing.T) {
	fake, _ := setupDatabaseCommandTest(t)
	fake.restoreResp = &databasev1alpha1.RestoreResponse{OperationId: "op-restore"}

	if err := mustExecute(t, []string{"restore", "snap1", "--no-confirm"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.lastRestore.GetSnapshotId() != "snap1" {
		t.Errorf("snapshot_id: got %q, want snap1", fake.lastRestore.GetSnapshotId())
	}
}

// TestRestoreCommand_RequiresConfirmation checks that omitting --no-confirm
// on non-interactive stdin errors out without calling Restore.
func TestRestoreCommand_RequiresConfirmation(t *testing.T) {
	fake, _ := setupDatabaseCommandTest(t)

	err := mustExecute(t, []string{"restore", "snap1"})
	if err == nil {
		t.Fatal("expected an error refusing to prompt on non-interactive stdin")
	}
	if fake.lastRestore != nil {
		t.Error("Restore RPC must not be called without confirmation")
	}
}

// TestRestoreStatusCommand checks that "restore status" calls GetRestoreStatus
// and renders its progress.
func TestRestoreStatusCommand(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.getRestoreStatusResp = &databasev1alpha1.GetRestoreStatusResponse{
		Progress: &databasev1alpha1.OperationProgress{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED},
	}

	if err := mustExecute(t, []string{"restore", "status", "op1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "status=completed") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// ── truncate / truncate status ────────────────────────────────────────────

// TestTruncateCommand checks that "truncate --slot --no-confirm" calls
// Truncate with a BlockRef target carrying that slot.
func TestTruncateCommand(t *testing.T) {
	fake, _ := setupDatabaseCommandTest(t)
	fake.truncateResp = &databasev1alpha1.TruncateResponse{OperationId: "op-trunc"}

	if err := mustExecute(t, []string{"truncate", "--slot", "100", "--no-confirm"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.lastTruncate.GetTarget().GetSlot() != 100 {
		t.Errorf("target slot: got %d, want 100", fake.lastTruncate.GetTarget().GetSlot())
	}
}

// TestTruncateCommand_RequiresExactlyOneTarget checks that omitting all of
// --slot/--block-hash/--block-number errors out without calling Truncate.
func TestTruncateCommand_RequiresExactlyOneTarget(t *testing.T) {
	fake, _ := setupDatabaseCommandTest(t)

	err := mustExecute(t, []string{"truncate", "--no-confirm"})
	if err == nil {
		t.Fatal("expected an error when no target flag is set")
	}
	if fake.lastTruncate != nil {
		t.Error("Truncate RPC must not be called without a valid target")
	}
}

// TestTruncateCommand_RequiresConfirmation checks that omitting --no-confirm
// on non-interactive stdin errors out without calling Truncate.
func TestTruncateCommand_RequiresConfirmation(t *testing.T) {
	fake, _ := setupDatabaseCommandTest(t)

	err := mustExecute(t, []string{"truncate", "--slot", "1"})
	if err == nil {
		t.Fatal("expected an error refusing to prompt on non-interactive stdin")
	}
	if fake.lastTruncate != nil {
		t.Error("Truncate RPC must not be called without confirmation")
	}
}

// TestTruncateCommand_Wait checks that "truncate --wait" streams progress to
// completion, then fetches and renders the final blocks_removed tally.
func TestTruncateCommand_Wait(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.truncateResp = &databasev1alpha1.TruncateResponse{OperationId: "op-trunc"}
	fake.streamProgressions = []*databasev1alpha1.OperationProgress{
		{OperationId: "op-trunc", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED, ProgressPercent: 100},
	}
	fake.getTruncateStatusResp = &databasev1alpha1.GetTruncateStatusResponse{
		Progress:      &databasev1alpha1.OperationProgress{OperationId: "op-trunc", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED},
		BlocksRemoved: 7,
	}

	if err := mustExecute(t, []string{"truncate", "--slot", "1", "--no-confirm", "--wait"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "blocks_removed=7") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestTruncateStatusCommand checks that "truncate status" calls
// GetTruncateStatus and renders its blocks_removed count.
func TestTruncateStatusCommand(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.getTruncateStatusResp = &databasev1alpha1.GetTruncateStatusResponse{
		Progress:      &databasev1alpha1.OperationProgress{OperationId: "op1", Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED},
		BlocksRemoved: 42,
	}

	if err := mustExecute(t, []string{"truncate", "status", "op1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "blocks_removed=42") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// ── --output table ────────────────────────────────────────────────────────

// TestSnapshotListCommand_TableOutput checks that "--output table" renders
// snapshot list's own TableWriter: one header line plus one row per snapshot.
func TestSnapshotListCommand_TableOutput(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.listAvailableSnapshotsResp = &databasev1alpha1.ListAvailableSnapshotsResponse{
		Snapshots: []*databasev1alpha1.SnapshotInfo{
			{SnapshotId: "a", Name: "nightly", SizeBytes: 100},
			{SnapshotId: "b", Name: "weekly", SizeBytes: 200},
		},
	}
	globalFlags.Output = "table"

	if err := mustExecute(t, []string{"snapshot", "list"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected a header line plus one row per snapshot, got %d lines: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "snapshot_id") {
		t.Errorf("header missing snapshot_id column: %q", lines[0])
	}
	if !strings.Contains(lines[1], "a") || !strings.Contains(lines[2], "b") {
		t.Errorf("rows missing expected snapshot IDs: %q", out)
	}
}

// TestDatabaseInfoCommand_TableOutput checks that "info --output table" falls
// back to the generic single-record table, since it has no TableWriter of its own.
func TestDatabaseInfoCommand_TableOutput(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.getDatabaseInfoResp = &databasev1alpha1.GetDatabaseInfoResponse{BlockCount: 7}
	globalFlags.Output = "table"

	// databaseInfoResult has no TableHeader/TableRows of its own, so this
	// exercises the generic single-record fallback rather than a
	// TableWriter implementation.
	if err := mustExecute(t, []string{"info"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected a header line plus one row, got %d lines: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "block_count") {
		t.Errorf("header missing block_count column: %q", lines[0])
	}
	if !strings.Contains(lines[1], "7") {
		t.Errorf("row missing block_count value: %q", lines[1])
	}
}

// ── database cancel ──────────────────────────────────────────────────────

// TestDatabaseCancelCommand checks that "cancel" calls CancelOperation with
// the given operation_id and renders the returned status.
func TestDatabaseCancelCommand(t *testing.T) {
	fake, buf := setupDatabaseCommandTest(t)
	fake.cancelOperationResp = &databasev1alpha1.CancelOperationResponse{
		Status: databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING,
	}

	if err := mustExecute(t, []string{"cancel", "op1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.lastCancelOperation.GetOperationId() != "op1" {
		t.Errorf(
			"operation_id: got %q, want op1",
			fake.lastCancelOperation.GetOperationId(),
		)
	}
	if !strings.Contains(buf.String(), "op1") || !strings.Contains(buf.String(), "running") {
		t.Errorf("unexpected output: %s", buf.String())
	}
}

// TestDatabaseCancelCommand_ServerError checks that a NotFound error from
// CancelOperation propagates as a command error with the same connect code.
func TestDatabaseCancelCommand_ServerError(t *testing.T) {
	fake, _ := setupDatabaseCommandTest(t)
	fake.cancelOperationErr = connect.NewError(
		connect.CodeNotFound, errors.New("unknown operation"),
	)

	err := mustExecute(t, []string{"cancel", "does-not-exist"})
	if err == nil {
		t.Fatal("expected an error for an unknown operation")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("error code: got %v, want NotFound", connect.CodeOf(err))
	}
}
