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
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"connectrpc.com/connect"
	databasev1alpha1 "github.com/blinklabs-io/bark/proto/v1alpha1/database"
	databasev1alpha1connect "github.com/blinklabs-io/bark/proto/v1alpha1/database/databasev1alpha1connect"
	"github.com/blinklabs-io/dingoctl/internal/client"
	"github.com/blinklabs-io/dingoctl/internal/output"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// newDatabaseCmd creates the database command with all its subcommands,
// covering every RPC exposed by bark's DatabaseService (bark#16): the
// snapshot catalog (create/list/delete/verify), restore, truncate, their
// status/streaming RPCs, and GetDatabaseInfo/GetOperationHistory.
func newDatabaseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "database",
		Short: "Manage the Dingo node's database (snapshot, restore, truncate)",
		Long: `Manage the Dingo node's database lifecycle over the Bark
DatabaseService: capture snapshots, restore from one, or truncate the
chain to an earlier point (CIP-0135 disaster recovery).

Restore and truncate are destructive and irreversible on the target node;
both prompt for confirmation unless --no-confirm is given.`,
	}

	cmd.AddCommand(newDatabaseInfoCmd())
	cmd.AddCommand(newDatabaseStatusCmd())
	cmd.AddCommand(newDatabaseSnapshotCmd())
	cmd.AddCommand(newDatabaseRestoreCmd())
	cmd.AddCommand(newDatabaseTruncateCmd())
	cmd.AddCommand(newDatabaseCancelCmd())

	return cmd
}

// ── cancel ───────────────────────────────────────────────────────────────

func newDatabaseCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel <operation-id>",
		Short: "Cancel a running database operation",
		Long: `Cancel a running snapshot, restore, truncate, or verify operation.

The server stops it promptly at its next internal checkpoint rather than
instantly — see the operation's own status for whether it actually
reached CANCELLED. A --wait'd command also sends this automatically if
interrupted (Ctrl+C), so this is mainly for cancelling an operation
another dingoctl invocation (or bark itself) started.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := GetClient()
			if err != nil {
				return err
			}
			resp, err := c.DatabaseService().CancelOperation(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.CancelOperationRequest{
					OperationId: args[0],
				}),
			)
			if err != nil {
				return err
			}
			return GetOutputPrinter().Print(operationCancelledResult{
				OperationID: args[0],
				Status:      statusString(resp.Msg.GetStatus()),
			})
		},
	}
}

// ── info ─────────────────────────────────────────────────────────────────

func newDatabaseInfoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "info",
		Short: "Show the node's current database state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := GetClient()
			if err != nil {
				return err
			}
			svc := c.DatabaseService()

			resp, err := svc.GetDatabaseInfo(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.GetDatabaseInfoRequest{}),
			)
			if err != nil {
				return err
			}
			info := databaseInfoFromProto(resp.Msg)

			// Last operation is a nice-to-have on top of the core info RPC:
			// degrade quietly if the server hasn't implemented operation
			// history yet, but surface any other failure.
			historyResp, err := svc.GetOperationHistory(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.GetOperationHistoryRequest{PageSize: 1}),
			)
			switch {
			case err == nil && len(historyResp.Msg.GetRecords()) > 0:
				rec := operationRecordFromProto(historyResp.Msg.GetRecords()[0])
				info.LastOperation = &rec
			case err != nil && connect.CodeOf(err) != connect.CodeUnimplemented:
				return err
			}

			return GetOutputPrinter().Print(info)
		},
	}
}

// ── status ───────────────────────────────────────────────────────────────

// newDatabaseStatusCmd reports whatever operation is currently running (if
// any) and its live progress. Because GetDatabaseInfo only tells us an
// operation ID, not its type, this uses StreamOperationProgress rather than
// one of the type-specific GetXStatus RPCs — it's the one RPC that reports
// progress generically regardless of whether the running operation is a
// snapshot, restore, truncate, or verify.
func newDatabaseStatusCmd() *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the current database operation, if any, with live progress",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := GetClient()
			if err != nil {
				return err
			}
			svc := c.DatabaseService()

			infoResp, err := svc.GetDatabaseInfo(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.GetDatabaseInfoRequest{}),
			)
			if err != nil {
				return err
			}
			if !infoResp.Msg.GetOperationInProgress() {
				return GetOutputPrinter().Print(databaseStatusResult{})
			}

			opID := infoResp.Msg.GetCurrentOperationId()
			stream, err := svc.StreamOperationProgress(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.StreamOperationProgressRequest{OperationId: opID}),
			)
			if err != nil {
				return err
			}
			defer func() { _ = stream.Close() }()

			for stream.Receive() {
				progress := stream.Msg().GetProgress()
				if err := GetOutputPrinter().Print(operationStatusFromProto(progress, "", nil)); err != nil {
					return err
				}
				if !follow || isTerminalStatus(progress.GetStatus()) {
					break
				}
			}
			return stream.Err()
		},
	}

	cmd.Flags().BoolVar(&follow, "follow", false, "keep streaming progress until the operation finishes")
	return cmd
}

// ── snapshot create / list / delete / verify / status ────────────────────

func newDatabaseSnapshotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Create, list, delete, or verify database snapshots",
	}
	cmd.AddCommand(newSnapshotCreateCmd())
	cmd.AddCommand(newSnapshotListCmd())
	cmd.AddCommand(newSnapshotDeleteCmd())
	cmd.AddCommand(newSnapshotVerifyCmd())
	cmd.AddCommand(newSnapshotStatusCmd())
	return cmd
}

func newSnapshotCreateCmd() *cobra.Command {
	var (
		name        string
		description string
		wait        bool
		waitTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Start a new database snapshot",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := GetClient()
			if err != nil {
				return err
			}
			svc := c.DatabaseService()

			createResp, err := svc.CreateSnapshot(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.CreateSnapshotRequest{
					Name:        name,
					Description: description,
				}),
			)
			if err != nil {
				return err
			}
			opID := createResp.Msg.GetOperationId()
			snapshotID := createResp.Msg.GetSnapshotId()

			if !wait {
				return GetOutputPrinter().Print(snapshotStartedResult{
					OperationID: opID,
					SnapshotID:  snapshotID,
				})
			}

			ctx, cancel := withOptionalTimeout(cmd.Context(), waitTimeout)
			defer cancel()
			progress, err := streamUntilTerminal(ctx, cmd.Context(), c.Config(), svc, opID)
			if err != nil {
				return err
			}
			return printTerminalResult(progress, snapshotID, "snapshot")
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "human-readable label for the snapshot")
	cmd.Flags().StringVar(&description, "description", "", "optional free-form description")
	addWaitFlags(cmd, &wait, &waitTimeout)
	return cmd
}

// newSnapshotListCmd calls ListAvailableSnapshots rather than the more
// narrowly-named ListSnapshots: the former is a strict superset (falls
// back to exactly ListSnapshots's result when no cloud destination is
// configured server-side, and additionally surfaces a snapshot whose
// local copy is gone but whose cloud mirror survives otherwise) — there's
// no case where an operator would want the narrower view, so "list"
// always shows everything actually available to restore/verify/delete.
func newSnapshotListCmd() *cobra.Command {
	var limit uint32

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available snapshots (local and cloud)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := GetClient()
			if err != nil {
				return err
			}
			resp, err := c.DatabaseService().ListAvailableSnapshots(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.ListAvailableSnapshotsRequest{PageSize: limit}),
			)
			if err != nil {
				return err
			}
			return GetOutputPrinter().Print(snapshotListFromAvailableProto(resp.Msg))
		},
	}

	cmd.Flags().Uint32Var(&limit, "limit", 0, "maximum number of snapshots to return (0 = server default)")
	return cmd
}

func newSnapshotDeleteCmd() *cobra.Command {
	var noConfirm bool

	cmd := &cobra.Command{
		Use:   "delete <snapshot-id>",
		Short: "Permanently delete a stored snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			snapshotID := args[0]
			ok, err := confirm(
				cmd,
				fmt.Sprintf("This will permanently delete snapshot %q. Continue?", snapshotID),
				noConfirm,
			)
			if err != nil {
				return err
			}
			if !ok {
				GetOutputPrinter().Println("Aborted.")
				return nil
			}

			c, err := GetClient()
			if err != nil {
				return err
			}
			resp, err := c.DatabaseService().DeleteSnapshot(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.DeleteSnapshotRequest{SnapshotId: snapshotID}),
			)
			if err != nil {
				return err
			}
			return GetOutputPrinter().Print(snapshotDeletedResult{
				SnapshotID: snapshotID,
				DeletedAt:  resp.Msg.GetDeletedAt().AsTime(),
			})
		},
	}

	cmd.Flags().BoolVar(&noConfirm, "no-confirm", false, "skip the confirmation prompt")
	return cmd
}

func newSnapshotVerifyCmd() *cobra.Command {
	var (
		wait        bool
		waitTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "verify <snapshot-id>",
		Short: "Run an integrity check against a stored snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			snapshotID := args[0]
			c, err := GetClient()
			if err != nil {
				return err
			}
			svc := c.DatabaseService()

			resp, err := svc.VerifySnapshot(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.VerifySnapshotRequest{SnapshotId: snapshotID}),
			)
			if err != nil {
				return err
			}
			opID := resp.Msg.GetOperationId()

			if !wait {
				return GetOutputPrinter().Print(operationStartedResult{OperationID: opID})
			}

			ctx, cancel := withOptionalTimeout(cmd.Context(), waitTimeout)
			defer cancel()
			progress, err := streamUntilTerminal(ctx, cmd.Context(), c.Config(), svc, opID)
			if err != nil {
				return err
			}
			return printTerminalResult(progress, snapshotID, "verify")
		},
	}

	addWaitFlags(cmd, &wait, &waitTimeout)
	return cmd
}

func newSnapshotStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <operation-id>",
		Short: "Check the status of a snapshot operation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := GetClient()
			if err != nil {
				return err
			}
			resp, err := c.DatabaseService().GetSnapshotStatus(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.GetSnapshotStatusRequest{OperationId: args[0]}),
			)
			if err != nil {
				return err
			}
			return GetOutputPrinter().Print(operationStatusFromProto(
				resp.Msg.GetProgress(), resp.Msg.GetSnapshotId(), nil,
			))
		},
	}
}

// ── restore / restore status ─────────────────────────────────────────────

// newDatabaseRestoreCmd builds the "restore" command itself as a flat,
// argument-taking command (per dingoctl#5's "restore [--no-confirm]"), with
// "status" as its only subcommand — restore has just one action, so there
// is no separate verb needed the way "snapshot" needs create/list/delete/
// verify.
func newDatabaseRestoreCmd() *cobra.Command {
	var (
		noConfirm   bool
		wait        bool
		waitTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "restore <snapshot-id>",
		Short: "Restore the database from a snapshot",
		Long: `Restore the database from the given snapshot.

This is destructive: it replaces the node's current database and cannot
be undone from this side. The node quiesces (stops forging/syncing/
serving) for the whole duration of the restore.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			snapshotID := args[0]
			ok, err := confirm(
				cmd,
				fmt.Sprintf(
					"This will replace the node's current database with snapshot %q. Continue?",
					snapshotID,
				),
				noConfirm,
			)
			if err != nil {
				return err
			}
			if !ok {
				GetOutputPrinter().Println("Aborted.")
				return nil
			}

			c, err := GetClient()
			if err != nil {
				return err
			}
			svc := c.DatabaseService()

			restoreResp, err := svc.Restore(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.RestoreRequest{SnapshotId: snapshotID}),
			)
			if err != nil {
				return err
			}
			opID := restoreResp.Msg.GetOperationId()

			if !wait {
				return GetOutputPrinter().Print(operationStartedResult{OperationID: opID})
			}

			ctx, cancel := withOptionalTimeout(cmd.Context(), waitTimeout)
			defer cancel()
			progress, err := streamUntilTerminal(ctx, cmd.Context(), c.Config(), svc, opID)
			if err != nil {
				return err
			}
			return printTerminalResult(progress, "", "restore")
		},
	}

	cmd.Flags().BoolVar(&noConfirm, "no-confirm", false, "skip the confirmation prompt")
	addWaitFlags(cmd, &wait, &waitTimeout)
	cmd.AddCommand(newRestoreStatusCmd())
	return cmd
}

func newRestoreStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <operation-id>",
		Short: "Check the status of a restore operation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := GetClient()
			if err != nil {
				return err
			}
			resp, err := c.DatabaseService().GetRestoreStatus(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.GetRestoreStatusRequest{OperationId: args[0]}),
			)
			if err != nil {
				return err
			}
			return GetOutputPrinter().Print(operationStatusFromProto(resp.Msg.GetProgress(), "", nil))
		},
	}
}

// ── truncate / truncate status ───────────────────────────────────────────

// newDatabaseTruncateCmd builds the "truncate" command itself as a flat
// command taking its target via flags (per dingoctl#5's "truncate
// (--slot | --block-hash | --block-number) [--no-confirm]"), with "status"
// as its only subcommand — truncate has just one action, so there is no
// separate verb needed the way "snapshot" needs create/list/delete/verify.
func newDatabaseTruncateCmd() *cobra.Command {
	var (
		slot        uint64
		blockHash   string
		blockNumber uint64

		noConfirm   bool
		wait        bool
		waitTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "truncate",
		Short: "Truncate the chain to a target block",
		Long: `Truncate the database to a target point, identified by exactly one of
--slot, --block-hash, or --block-number. The target block becomes the new
chain tip; every block and metadata row added after it is removed.

Unlike a normal chain rollback, this does not reject a target beyond the
configured security parameter — it is intended for disaster recovery
scenarios (see CIP-0135) where the chain must be rewound further than
Ouroboros Praos's built-in rollback limit allows. This is destructive
and cannot be undone from this side; the node quiesces for the whole
duration of the operation.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			target, err := blockRefFromFlags(cmd, slot, blockHash, blockNumber)
			if err != nil {
				return err
			}

			ok, err := confirm(
				cmd,
				fmt.Sprintf("This will truncate the chain to %s and remove everything after it. Continue?", target),
				noConfirm,
			)
			if err != nil {
				return err
			}
			if !ok {
				GetOutputPrinter().Println("Aborted.")
				return nil
			}

			c, err := GetClient()
			if err != nil {
				return err
			}
			svc := c.DatabaseService()

			truncResp, err := svc.Truncate(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.TruncateRequest{Target: target}),
			)
			if err != nil {
				return err
			}
			opID := truncResp.Msg.GetOperationId()

			if !wait {
				return GetOutputPrinter().Print(operationStartedResult{OperationID: opID})
			}

			ctx, cancel := withOptionalTimeout(cmd.Context(), waitTimeout)
			defer cancel()
			progress, err := streamUntilTerminal(ctx, cmd.Context(), c.Config(), svc, opID)
			if err != nil {
				return err
			}
			// StreamOperationProgress reports generic progress only; fetch
			// the truncate-specific final tally once the stream ends.
			statusResp, err := svc.GetTruncateStatus(
				ctx,
				connect.NewRequest(&databasev1alpha1.GetTruncateStatusRequest{OperationId: opID}),
			)
			if err != nil {
				return err
			}
			return printTerminalResult(progress, "", "truncate", statusResp.Msg.GetBlocksRemoved())
		},
	}

	cmd.Flags().Uint64Var(&slot, "slot", 0, "truncate target slot")
	cmd.Flags().StringVar(&blockHash, "block-hash", "", "truncate target block hash (hex)")
	cmd.Flags().Uint64Var(&blockNumber, "block-number", 0, "truncate target block number")
	cmd.Flags().BoolVar(&noConfirm, "no-confirm", false, "skip the confirmation prompt")
	addWaitFlags(cmd, &wait, &waitTimeout)
	cmd.AddCommand(newTruncateStatusCmd())
	return cmd
}

func newTruncateStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <operation-id>",
		Short: "Check the status of a truncate operation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := GetClient()
			if err != nil {
				return err
			}
			resp, err := c.DatabaseService().GetTruncateStatus(
				cmd.Context(),
				connect.NewRequest(&databasev1alpha1.GetTruncateStatusRequest{OperationId: args[0]}),
			)
			if err != nil {
				return err
			}
			return GetOutputPrinter().Print(operationStatusFromProto(
				resp.Msg.GetProgress(), "", &resp.Msg.BlocksRemoved,
			))
		},
	}
}

// ── shared flags/helpers ─────────────────────────────────────────────────

// addWaitFlags attaches the --wait/--wait-timeout flags shared by every
// command that kicks off an async operation (snapshot create/verify,
// restore, truncate). With --wait, progress is streamed live from the
// server (see streamUntilTerminal) rather than polled.
func addWaitFlags(cmd *cobra.Command, wait *bool, waitTimeout *time.Duration) {
	cmd.Flags().BoolVar(wait, "wait", false, "block and stream progress until the operation finishes")
	cmd.Flags().DurationVar(waitTimeout, "wait-timeout", 0, "give up waiting after this long (0 = no limit); the operation keeps running on the node regardless")
}

// withOptionalTimeout wraps ctx with a timeout when d > 0, otherwise
// returns ctx unchanged (with a no-op cancel so callers can always defer
// cancel() unconditionally).
func withOptionalTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// blockRefFromFlags builds a BlockRef from exactly one of --slot,
// --block-hash, --block-number.
func blockRefFromFlags(
	cmd *cobra.Command,
	slot uint64,
	blockHash string,
	blockNumber uint64,
) (*databasev1alpha1.BlockRef, error) {
	set := 0
	ref := &databasev1alpha1.BlockRef{}
	if cmd.Flags().Changed("slot") {
		ref.Slot = &slot
		set++
	}
	if cmd.Flags().Changed("block-hash") {
		if _, err := hex.DecodeString(blockHash); err != nil {
			return nil, fmt.Errorf("invalid --block-hash: %w", err)
		}
		ref.Hash = &blockHash
		set++
	}
	if cmd.Flags().Changed("block-number") {
		ref.BlockNumber = &blockNumber
		set++
	}
	if set != 1 {
		return nil, errors.New("exactly one of --slot, --block-hash, or --block-number is required")
	}
	return ref, nil
}

// confirm prompts on cmd's stdin/stdout unless noConfirm is set. It refuses
// to prompt (rather than hang) when stdin isn't a terminal, so a script
// that forgets --no-confirm fails loudly instead of blocking forever.
func confirm(cmd *cobra.Command, prompt string, noConfirm bool) (bool, error) {
	if noConfirm {
		return true, nil
	}
	if f, ok := cmd.InOrStdin().(*os.File); !ok || !isatty.IsTerminal(f.Fd()) {
		return false, errors.New(
			"refusing to prompt for confirmation on a non-interactive input; pass --no-confirm to proceed",
		)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N]: ", prompt); err != nil {
		return false, err
	}
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes", nil
}

// streamUntilTerminal opens a StreamOperationProgress stream for opID,
// printing each update as it arrives (live progress from the Bark
// coordinator), and returns the last progress reported once the operation
// reaches a terminal status or ctx is done.
//
// parentCtx is the un-timeout-wrapped context (ultimately cmd.Context(),
// cancelled only by a real interrupt — see Execute's signal.NotifyContext
// wiring in root.go), used solely to tell "the user pressed Ctrl+C" apart
// from "--wait-timeout expired": ctx itself goes Done() for either reason,
// but parentCtx only goes Done() for a real interrupt. On a real
// interrupt this best-effort asks the server to cancel the operation
// (requestCancelOnInterrupt), since otherwise it would be left running
// with no way to stop it from here; on a --wait-timeout expiry, per that
// flag's own documented contract, the operation is deliberately left
// running untouched.
//
// The interrupt check runs both when the stream ends after delivering at
// least one message AND when the very first call to open it fails: for a
// server-streaming RPC, StreamOperationProgress's connect client blocks
// synchronously waiting for response headers before Receive() is ever
// callable, so a Ctrl+C that lands before the operation's first progress
// update (a real, easily-hit race, not just a theoretical one) surfaces as
// an error from that initial call rather than from the Receive() loop. Only
// checking the loop's exit would silently drop the cancel request in that
// case and leave the operation orphaned on the server.
// errStreamReachedTerminal is the StreamHandler sentinel used to stop
// client.RunServerStream as soon as a terminal status arrives, rather than
// waiting for the server to close the stream on its own. RunServerStream
// never retries a handler error, so this always surfaces back unchanged and
// is translated back to a nil error below.
var errStreamReachedTerminal = errors.New("operation reached a terminal status")

func streamUntilTerminal(
	ctx context.Context,
	parentCtx context.Context,
	cfg client.Config,
	svc databasev1alpha1connect.DatabaseServiceClient,
	opID string,
) (*databasev1alpha1.OperationProgress, error) {
	var last *databasev1alpha1.OperationProgress
	runErr := client.RunServerStream(
		ctx,
		cfg,
		func(ctx context.Context) (*connect.ServerStreamForClient[databasev1alpha1.StreamOperationProgressResponse], error) {
			return svc.StreamOperationProgress(
				ctx,
				connect.NewRequest(&databasev1alpha1.StreamOperationProgressRequest{OperationId: opID}),
			)
		},
		func(msg *databasev1alpha1.StreamOperationProgressResponse) error {
			last = msg.GetProgress()
			if err := GetOutputPrinter().Print(operationStatusFromProto(last, "", nil)); err != nil {
				return err
			}
			if isTerminalStatus(last.GetStatus()) {
				return errStreamReachedTerminal
			}
			return nil
		},
	)

	switch {
	case errors.Is(runErr, errStreamReachedTerminal):
		runErr = nil
	case runErr == nil && ctx.Err() != nil:
		// Guards a clean stream close (Receive() -> false, Err() -> nil)
		// racing a context cancellation/timeout that fired just as the
		// stream ended: without this, a --wait-timeout expiry or Ctrl+C
		// landing at that exact moment would be reported as success.
		runErr = ctx.Err()
	case runErr == nil && last == nil:
		// The server closed the stream without ever reporting progress.
		// Without this, the caller renders a misleading
		// status=unspecified progress=0% success instead of an error.
		runErr = fmt.Errorf(
			"operation stream for operation_id=%s closed with no progress reported",
			opID,
		)
	}
	requestCancelIfInterrupted(parentCtx, svc, opID)
	return last, runErr
}

// requestCancelIfInterrupted asks the server to cancel opID when parentCtx
// shows a real interrupt happened (see streamUntilTerminal's doc comment),
// regardless of which of its two exit points triggered the check.
func requestCancelIfInterrupted(
	parentCtx context.Context,
	svc databasev1alpha1connect.DatabaseServiceClient,
	opID string,
) {
	if !shouldRequestCancelOnInterrupt(parentCtx) {
		return
	}
	GetOutputPrinter().Println(fmt.Sprintf(
		"Interrupted — requesting the server cancel operation_id=%s", opID,
	))
	requestCancelOnInterrupt(svc, opID)
}

// shouldRequestCancelOnInterrupt reports whether the loop above ending
// represents a real interrupt (parentCtx cancelled) as opposed to
// --wait-timeout expiring (parentCtx unaffected — only the derived ctx
// passed to the stream call would be Done in that case). Split out as its
// own function so this decision is testable directly, without needing to
// simulate a timeout firing mid-stream against a real or fake server.
func shouldRequestCancelOnInterrupt(parentCtx context.Context) bool {
	return parentCtx.Err() != nil
}

// requestCancelOnInterrupt best-effort asks the server to cancel opID,
// using a short-lived context detached from the (already-cancelled)
// caller context so the cancel request itself isn't immediately aborted
// too. Errors are swallowed: this is a courtesy cleanup on a command the
// operator already interrupted, not something worth surfacing a second
// error for.
func requestCancelOnInterrupt(svc databasev1alpha1connect.DatabaseServiceClient, opID string) {
	//nolint:contextcheck // ctx is intentionally derived from Background, not the
	// caller's (already-cancelled) context, so this cleanup request can
	// actually reach the server instead of being aborted immediately too.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = svc.CancelOperation(
		ctx,
		connect.NewRequest(&databasev1alpha1.CancelOperationRequest{OperationId: opID}),
	)
}

func isTerminalStatus(s databasev1alpha1.OperationStatus) bool {
	switch s {
	case databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED,
		databasev1alpha1.OperationStatus_OPERATION_STATUS_FAILED,
		databasev1alpha1.OperationStatus_OPERATION_STATUS_CANCELLED:
		return true
	default:
		return false
	}
}

func statusString(s databasev1alpha1.OperationStatus) string {
	switch s {
	case databasev1alpha1.OperationStatus_OPERATION_STATUS_PENDING:
		return "pending"
	case databasev1alpha1.OperationStatus_OPERATION_STATUS_RUNNING:
		return "running"
	case databasev1alpha1.OperationStatus_OPERATION_STATUS_COMPLETED:
		return "completed"
	case databasev1alpha1.OperationStatus_OPERATION_STATUS_FAILED:
		return "failed"
	case databasev1alpha1.OperationStatus_OPERATION_STATUS_CANCELLED:
		return "cancelled"
	default:
		return "unspecified"
	}
}

func operationTypeString(t databasev1alpha1.OperationType) string {
	switch t {
	case databasev1alpha1.OperationType_OPERATION_TYPE_SNAPSHOT:
		return "snapshot"
	case databasev1alpha1.OperationType_OPERATION_TYPE_RESTORE:
		return "restore"
	case databasev1alpha1.OperationType_OPERATION_TYPE_TRUNCATE:
		return "truncate"
	case databasev1alpha1.OperationType_OPERATION_TYPE_VERIFY:
		return "verify"
	default:
		return "unspecified"
	}
}

// printTerminalResult prints the final status of a --wait'd operation and
// returns a non-nil error when it finished as FAILED, so the process exits
// non-zero even though the RPCs themselves all succeeded.
func printTerminalResult(
	progress *databasev1alpha1.OperationProgress,
	snapshotID string,
	kind string,
	blocksRemoved ...uint64,
) error {
	var removed *uint64
	if len(blocksRemoved) > 0 {
		removed = &blocksRemoved[0]
	}
	result := operationStatusFromProto(progress, snapshotID, removed)
	if err := GetOutputPrinter().Print(result); err != nil {
		return err
	}
	if progress.GetStatus() == databasev1alpha1.OperationStatus_OPERATION_STATUS_FAILED {
		return fmt.Errorf("%s failed: %s", kind, progress.GetMessage())
	}
	return nil
}

// ── result types ─────────────────────────────────────────────────────────
//
// These are deliberately hand-written rather than printing the raw proto
// response structs: proto enums (OperationStatus) serialize as bare
// integers under encoding/json with no custom marshaling, and go.yaml.in/
// yaml wouldn't understand the protoreflect message at all. Small, plainly
// tagged structs give clean text/json/yaml output either way.

type operationStartedResult struct {
	OperationID string `json:"operation_id" yaml:"operation_id"`
}

func (r operationStartedResult) String() string {
	return fmt.Sprintf("Started: operation_id=%s", r.OperationID)
}

type operationCancelledResult struct {
	OperationID string `json:"operation_id" yaml:"operation_id"`
	Status      string `json:"status" yaml:"status"`
}

func (r operationCancelledResult) String() string {
	return fmt.Sprintf(
		"Cancel requested: operation_id=%s status=%s",
		r.OperationID, r.Status,
	)
}

type snapshotStartedResult struct {
	OperationID string `json:"operation_id" yaml:"operation_id"`
	SnapshotID  string `json:"snapshot_id" yaml:"snapshot_id"`
}

func (r snapshotStartedResult) String() string {
	return fmt.Sprintf(
		"Snapshot started: operation_id=%s snapshot_id=%s",
		r.OperationID, r.SnapshotID,
	)
}

type operationStatusResult struct {
	OperationID   string     `json:"operation_id" yaml:"operation_id"`
	Status        string     `json:"status" yaml:"status"`
	ProgressPct   float32    `json:"progress_percent" yaml:"progress_percent"`
	Message       string     `json:"message,omitempty" yaml:"message,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty" yaml:"started_at,omitempty"`
	UpdatedAt     *time.Time `json:"updated_at,omitempty" yaml:"updated_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty" yaml:"completed_at,omitempty"`
	SnapshotID    string     `json:"snapshot_id,omitempty" yaml:"snapshot_id,omitempty"`
	BlocksRemoved *uint64    `json:"blocks_removed,omitempty" yaml:"blocks_removed,omitempty"`
}

func (r operationStatusResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "operation_id=%s status=%s progress=%.0f%%", r.OperationID, r.Status, r.ProgressPct)
	if r.SnapshotID != "" {
		fmt.Fprintf(&b, " snapshot_id=%s", r.SnapshotID)
	}
	if r.BlocksRemoved != nil {
		fmt.Fprintf(&b, " blocks_removed=%d", *r.BlocksRemoved)
	}
	if r.Message != "" {
		fmt.Fprintf(&b, " message=%q", r.Message)
	}
	return b.String()
}

func operationStatusFromProto(
	p *databasev1alpha1.OperationProgress,
	snapshotID string,
	blocksRemoved *uint64,
) operationStatusResult {
	r := operationStatusResult{
		OperationID: p.GetOperationId(),
		Status:      statusString(p.GetStatus()),
		ProgressPct: p.GetProgressPercent(),
		Message:     p.GetMessage(),
		SnapshotID:  snapshotID,
	}
	if ts := p.GetStartedAt(); ts != nil {
		t := ts.AsTime()
		r.StartedAt = &t
	}
	if ts := p.GetUpdatedAt(); ts != nil {
		t := ts.AsTime()
		r.UpdatedAt = &t
	}
	if ts := p.GetCompletedAt(); ts != nil {
		t := ts.AsTime()
		r.CompletedAt = &t
	}
	if blocksRemoved != nil && *blocksRemoved > 0 {
		r.BlocksRemoved = blocksRemoved
	}
	return r
}

type blockRefResult struct {
	Hash        string `json:"hash,omitempty" yaml:"hash,omitempty"`
	Slot        uint64 `json:"slot" yaml:"slot"`
	BlockNumber uint64 `json:"block_number" yaml:"block_number"`
}

func (r blockRefResult) String() string {
	if r.Hash != "" {
		return fmt.Sprintf("slot=%d block_number=%d hash=%s", r.Slot, r.BlockNumber, r.Hash)
	}
	return fmt.Sprintf("slot=%d block_number=%d", r.Slot, r.BlockNumber)
}

func blockRefFromProtoResult(b *databasev1alpha1.BlockRef) blockRefResult {
	return blockRefResult{
		Hash:        b.GetHash(),
		Slot:        b.GetSlot(),
		BlockNumber: b.GetBlockNumber(),
	}
}

type operationRecordResult struct {
	OperationID string     `json:"operation_id" yaml:"operation_id"`
	Type        string     `json:"type" yaml:"type"`
	Status      string     `json:"status" yaml:"status"`
	Message     string     `json:"message,omitempty" yaml:"message,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty" yaml:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty" yaml:"completed_at,omitempty"`
}

func (r operationRecordResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "operation_id=%s type=%s status=%s", r.OperationID, r.Type, r.Status)
	if r.Message != "" {
		fmt.Fprintf(&b, " message=%q", r.Message)
	}
	return b.String()
}

func operationRecordFromProto(r *databasev1alpha1.OperationRecord) operationRecordResult {
	rr := operationRecordResult{
		OperationID: r.GetOperationId(),
		Type:        operationTypeString(r.GetType()),
		Status:      statusString(r.GetStatus()),
		Message:     r.GetMessage(),
	}
	if ts := r.GetStartedAt(); ts != nil {
		t := ts.AsTime()
		rr.StartedAt = &t
	}
	if ts := r.GetCompletedAt(); ts != nil {
		t := ts.AsTime()
		rr.CompletedAt = &t
	}
	return rr
}

type databaseInfoResult struct {
	Tip                 blockRefResult         `json:"tip" yaml:"tip"`
	BlockCount          uint64                 `json:"block_count" yaml:"block_count"`
	SizeBytes           uint64                 `json:"size_bytes" yaml:"size_bytes"`
	OldestSlot          uint64                 `json:"oldest_slot" yaml:"oldest_slot"`
	OperationInProgress bool                   `json:"operation_in_progress" yaml:"operation_in_progress"`
	CurrentOperationID  string                 `json:"current_operation_id,omitempty" yaml:"current_operation_id,omitempty"`
	LastOperation       *operationRecordResult `json:"last_operation,omitempty" yaml:"last_operation,omitempty"`
}

func (r databaseInfoResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "tip: %s\n", r.Tip)
	fmt.Fprintf(&b, "block_count: %d\n", r.BlockCount)
	fmt.Fprintf(&b, "size_bytes: %d\n", r.SizeBytes)
	fmt.Fprintf(&b, "oldest_slot: %d\n", r.OldestSlot)
	if r.OperationInProgress {
		fmt.Fprintf(&b, "operation_in_progress: true (operation_id=%s)\n", r.CurrentOperationID)
	} else {
		fmt.Fprint(&b, "operation_in_progress: false\n")
	}
	if r.LastOperation != nil {
		fmt.Fprintf(&b, "last_operation: %s", *r.LastOperation)
	} else {
		fmt.Fprint(&b, "last_operation: none")
	}
	return b.String()
}

func databaseInfoFromProto(resp *databasev1alpha1.GetDatabaseInfoResponse) databaseInfoResult {
	return databaseInfoResult{
		Tip:                 blockRefFromProtoResult(resp.GetTip()),
		BlockCount:          resp.GetBlockCount(),
		SizeBytes:           resp.GetSizeBytes(),
		OldestSlot:          resp.GetOldestSlot(),
		OperationInProgress: resp.GetOperationInProgress(),
		CurrentOperationID:  resp.GetCurrentOperationId(),
	}
}

type databaseStatusResult struct {
	OperationInProgress bool `json:"operation_in_progress" yaml:"operation_in_progress"`
}

func (r databaseStatusResult) String() string {
	return "operation_in_progress: false"
}

type snapshotInfoResult struct {
	SnapshotID  string         `json:"snapshot_id" yaml:"snapshot_id"`
	Name        string         `json:"name" yaml:"name"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	Tip         blockRefResult `json:"tip" yaml:"tip"`
	SizeBytes   uint64         `json:"size_bytes" yaml:"size_bytes"`
	Checksum    string         `json:"checksum,omitempty" yaml:"checksum,omitempty"`
	CreatedAt   *time.Time     `json:"created_at,omitempty" yaml:"created_at,omitempty"`
	Location    string         `json:"location,omitempty" yaml:"location,omitempty"`
}

func (r snapshotInfoResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "snapshot_id=%s name=%q size_bytes=%d tip=(%s)", r.SnapshotID, r.Name, r.SizeBytes, r.Tip)
	if r.Description != "" {
		fmt.Fprintf(&b, " description=%q", r.Description)
	}
	if r.Location != "" {
		fmt.Fprintf(&b, " location=%s", r.Location)
	}
	return b.String()
}

func snapshotInfoFromProto(s *databasev1alpha1.SnapshotInfo) snapshotInfoResult {
	r := snapshotInfoResult{
		SnapshotID:  s.GetSnapshotId(),
		Name:        s.GetName(),
		Description: s.GetDescription(),
		Tip:         blockRefFromProtoResult(s.GetTip()),
		SizeBytes:   s.GetSizeBytes(),
		Checksum:    s.GetChecksum(),
		Location:    s.GetLocation(),
	}
	if ts := s.GetCreatedAt(); ts != nil {
		t := ts.AsTime()
		r.CreatedAt = &t
	}
	return r
}

type snapshotListResult struct {
	Snapshots     []snapshotInfoResult `json:"snapshots" yaml:"snapshots"`
	NextPageToken string               `json:"next_page_token,omitempty" yaml:"next_page_token,omitempty"`
}

func (r snapshotListResult) String() string {
	if len(r.Snapshots) == 0 {
		return "no snapshots found"
	}
	lines := make([]string, len(r.Snapshots))
	for i, s := range r.Snapshots {
		lines[i] = s.String()
	}
	out := strings.Join(lines, "\n")
	if r.NextPageToken != "" {
		out += fmt.Sprintf("\nnext_page_token=%s", r.NextPageToken)
	}
	return out
}

// snapshotListFromAvailableProto is the only conversion dingoctl needs
// from the wire format to snapshotListResult: "snapshot list" always
// calls ListAvailableSnapshots (see newSnapshotListCmd), never the
// narrower ListSnapshots.
func snapshotListFromAvailableProto(
	resp *databasev1alpha1.ListAvailableSnapshotsResponse,
) snapshotListResult {
	snaps := make([]snapshotInfoResult, len(resp.GetSnapshots()))
	for i, s := range resp.GetSnapshots() {
		snaps[i] = snapshotInfoFromProto(s)
	}
	return snapshotListResult{Snapshots: snaps, NextPageToken: resp.GetNextPageToken()}
}

var _ output.TableWriter = snapshotListResult{}

// TableHeader/TableRows make snapshotListResult a output.TableWriter: a
// list of snapshots naturally renders as one row per snapshot, unlike the
// single-record result types elsewhere in this file that just fall back
// to output's generic one-row-per-struct table.
func (r snapshotListResult) TableHeader() []string {
	return []string{"snapshot_id", "name", "description", "tip", "size_bytes", "created_at", "location"}
}

func (r snapshotListResult) TableRows() [][]string {
	rows := make([][]string, len(r.Snapshots))
	for i, s := range r.Snapshots {
		var createdAt string
		if s.CreatedAt != nil {
			createdAt = s.CreatedAt.Format(time.RFC3339)
		}
		rows[i] = []string{
			s.SnapshotID,
			s.Name,
			s.Description,
			s.Tip.String(),
			fmt.Sprintf("%d", s.SizeBytes),
			createdAt,
			s.Location,
		}
	}
	return rows
}

type snapshotDeletedResult struct {
	SnapshotID string    `json:"snapshot_id" yaml:"snapshot_id"`
	DeletedAt  time.Time `json:"deleted_at" yaml:"deleted_at"`
}

func (r snapshotDeletedResult) String() string {
	return fmt.Sprintf("Deleted snapshot %s at %s", r.SnapshotID, r.DeletedAt.Format(time.RFC3339))
}
