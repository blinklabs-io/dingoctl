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
	"fmt"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	lifecyclev1alpha1 "github.com/blinklabs-io/bark/proto/v1alpha1/lifecycle"
	"github.com/blinklabs-io/dingoctl/internal/client"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/types/known/durationpb"
)

// ── stop ─────────────────────────────────────────────────────────────────

func newStopCmd() *cobra.Command {
	var (
		gracefulTimeout time.Duration
		noConfirm       bool
	)

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the Dingo node gracefully",
		Long: `Stop initiates a graceful shutdown of the node.

The shutdown coordinator ceases chain sync, drains in-flight connections,
flushes the database, and then exits the process. If the graceful sequence
does not finish within the effective timeout, the coordinator forces the
process to exit rather than hang.

This is a destructive operation that takes the node offline. It requires
confirmation unless --no-confirm is given.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ok, err := confirm(cmd, "This will stop the node. Continue?", noConfirm)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
				return nil
			}

			c, err := GetClient()
			if err != nil {
				return err
			}

			req := &lifecyclev1alpha1.StopRequest{}
			if gracefulTimeout != 0 {
				if gracefulTimeout < 0 {
					return fmt.Errorf("graceful-timeout must be non-negative, got %v", gracefulTimeout)
				}
				req.GracefulTimeout = durationpb.New(gracefulTimeout)
			}

			// Stop starts a server-side shutdown operation; see client.WithNoRetry's
			// doc comment for why this can't safely go through the shared retry policy.
			resp, err := c.LifecycleService().Stop(
				client.WithNoRetry(cmd.Context()),
				connect.NewRequest(req),
			)
			if err != nil {
				return err
			}

			result := stopResult{EffectiveTimeout: resp.Msg.GetEffectiveTimeout().AsDuration()}
			if resp.Msg.GetDeadline() != nil {
				deadline := resp.Msg.GetDeadline().AsTime()
				result.Deadline = &deadline
			}
			return GetOutputPrinter().Print(result)
		},
	}

	cmd.Flags().DurationVar(&gracefulTimeout, "graceful-timeout", 0, "maximum duration for graceful shutdown (0 = server default)")
	cmd.Flags().BoolVar(&noConfirm, "no-confirm", false, "skip confirmation prompt")

	return cmd
}

// ── restart ──────────────────────────────────────────────────────────────

func newRestartCmd() *cobra.Command {
	var (
		gracefulTimeout time.Duration
		noConfirm       bool
	)

	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the Dingo node gracefully",
		Long: `Restart performs the same graceful shutdown sequence as stop and then
re-executes the node process in place (same binary path and arguments).

This allows picking up a replaced binary (e.g. after an upgrade) or a
changed configuration. The shutdown coordinator ceases chain sync, drains
in-flight connections, flushes the database, and then re-executes. If the
graceful sequence does not finish within the effective timeout, the
coordinator forces re-execution rather than hang.

This is a disruptive operation that takes the node offline temporarily. It
requires confirmation unless --no-confirm is given.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ok, err := confirm(cmd, "This will restart the node. Continue?", noConfirm)
			if err != nil {
				return err
			}
			if !ok {
				fmt.Fprintln(cmd.ErrOrStderr(), "Aborted.")
				return nil
			}

			c, err := GetClient()
			if err != nil {
				return err
			}

			req := &lifecyclev1alpha1.RestartRequest{}
			if gracefulTimeout != 0 {
				if gracefulTimeout < 0 {
					return fmt.Errorf("graceful-timeout must be non-negative, got %v", gracefulTimeout)
				}
				req.GracefulTimeout = durationpb.New(gracefulTimeout)
			}

			// Restart starts a server-side shutdown+restart operation; see client.WithNoRetry's
			// doc comment for why this can't safely go through the shared retry policy.
			resp, err := c.LifecycleService().Restart(
				client.WithNoRetry(cmd.Context()),
				connect.NewRequest(req),
			)
			if err != nil {
				return err
			}

			result := restartResult{EffectiveTimeout: resp.Msg.GetEffectiveTimeout().AsDuration()}
			if resp.Msg.GetDeadline() != nil {
				deadline := resp.Msg.GetDeadline().AsTime()
				result.Deadline = &deadline
			}
			return GetOutputPrinter().Print(result)
		},
	}

	cmd.Flags().DurationVar(&gracefulTimeout, "graceful-timeout", 0, "maximum duration for graceful shutdown (0 = server default)")
	cmd.Flags().BoolVar(&noConfirm, "no-confirm", false, "skip confirmation prompt")

	return cmd
}

// ── status ───────────────────────────────────────────────────────────────

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show node status, health, uptime, and synchronization state",
		Long: `Report the node's current lifecycle state, health, uptime, version,
and chain synchronization state.

This command remains available while a graceful stop or restart is draining,
so operators can poll it to observe the transition and the enforced deadline.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := GetClient()
			if err != nil {
				return err
			}

			resp, err := c.LifecycleService().GetStatus(cmd.Context(), connect.NewRequest(&lifecyclev1alpha1.GetStatusRequest{}))
			if err != nil {
				return err
			}

			result := statusResultFromProto(resp.Msg)
			return GetOutputPrinter().Print(result)
		},
	}

	return cmd
}

// ── Result types ─────────────────────────────────────────────────────────

type stopResult struct {
	EffectiveTimeout time.Duration `json:"effective_timeout" yaml:"effective_timeout"`
	Deadline         *time.Time    `json:"deadline,omitempty" yaml:"deadline,omitempty"`
}

func (r stopResult) String() string {
	if r.Deadline == nil {
		return fmt.Sprintf("Stop initiated. Effective timeout: %s", r.EffectiveTimeout)
	}
	return fmt.Sprintf("Stop initiated. Effective timeout: %s, deadline: %s",
		r.EffectiveTimeout, r.Deadline.Format(time.RFC3339))
}

type restartResult struct {
	EffectiveTimeout time.Duration `json:"effective_timeout" yaml:"effective_timeout"`
	Deadline         *time.Time    `json:"deadline,omitempty" yaml:"deadline,omitempty"`
}

func (r restartResult) String() string {
	if r.Deadline == nil {
		return fmt.Sprintf("Restart initiated. Effective timeout: %s", r.EffectiveTimeout)
	}
	return fmt.Sprintf("Restart initiated. Effective timeout: %s, deadline: %s",
		r.EffectiveTimeout, r.Deadline.Format(time.RFC3339))
}

type statusResult struct {
	State                   string        `json:"state" yaml:"state"`
	Health                  string        `json:"health" yaml:"health"`
	Uptime                  time.Duration `json:"uptime" yaml:"uptime"`
	Version                 string        `json:"version" yaml:"version"`
	Synced                  bool          `json:"synced" yaml:"synced"`
	TipSlot                 *uint64       `json:"tip_slot,omitempty" yaml:"tip_slot,omitempty"`
	TipHash                 *string       `json:"tip_hash,omitempty" yaml:"tip_hash,omitempty"`
	TipBlockNumber          *uint64       `json:"tip_block_number,omitempty" yaml:"tip_block_number,omitempty"`
	EstimatedNetworkTipSlot uint64        `json:"estimated_network_tip_slot,omitempty" yaml:"estimated_network_tip_slot,omitempty"`
	SlotsBehind             uint64        `json:"slots_behind,omitempty" yaml:"slots_behind,omitempty"`
	Deadline                *time.Time    `json:"deadline,omitempty" yaml:"deadline,omitempty"`
}

func (r statusResult) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "State: %s\n", r.State)
	fmt.Fprintf(&b, "Health: %s\n", r.Health)
	fmt.Fprintf(&b, "Uptime: %s\n", r.Uptime)
	fmt.Fprintf(&b, "Version: %s\n", r.Version)

	if r.Synced {
		fmt.Fprintf(&b, "Sync: synced")
	} else {
		fmt.Fprintf(&b, "Sync: not synced")
	}

	// Bark's BlockRef contract permits a tip with any combination of slot, hash, and block_number.
	// Render whichever fields are present.
	if r.TipSlot != nil || r.TipHash != nil || r.TipBlockNumber != nil {
		fmt.Fprintf(&b, " (tip")
		separator := " "
		if r.TipSlot != nil {
			fmt.Fprintf(&b, "%sslot: %d", separator, *r.TipSlot)
			separator = ", "
		}
		if r.TipBlockNumber != nil {
			fmt.Fprintf(&b, "%sblock: %d", separator, *r.TipBlockNumber)
			separator = ", "
		}
		if r.TipHash != nil {
			fmt.Fprintf(&b, "%shash: %s", separator, *r.TipHash)
		}
		fmt.Fprintf(&b, ")")
	}

	if r.SlotsBehind > 0 {
		fmt.Fprintf(&b, "\nSlots behind: %d", r.SlotsBehind)
	}
	if r.EstimatedNetworkTipSlot > 0 {
		fmt.Fprintf(&b, "\nEstimated network tip slot: %d", r.EstimatedNetworkTipSlot)
	}

	if r.Deadline != nil {
		fmt.Fprintf(&b, "\nDeadline: %s", r.Deadline.Format(time.RFC3339))
	}

	return b.String()
}

func (r statusResult) TableHeader() []string {
	return []string{"State", "Health", "Uptime", "Version", "Synced", "Tip", "Slots Behind"}
}

func (r statusResult) TableRows() [][]string {
	// Bark's BlockRef contract permits a tip with any combination of slot, hash, and block_number.
	// Show whichever fields are present.
	tipInfo := "N/A"
	if r.TipSlot != nil || r.TipHash != nil || r.TipBlockNumber != nil {
		var parts []string
		if r.TipSlot != nil {
			parts = append(parts, "slot:"+strconv.FormatUint(*r.TipSlot, 10))
		}
		if r.TipBlockNumber != nil {
			parts = append(parts, "block:"+strconv.FormatUint(*r.TipBlockNumber, 10))
		}
		if r.TipHash != nil {
			parts = append(parts, "hash:"+*r.TipHash)
		}
		tipInfo = strings.Join(parts, " ")
	}

	slotsBehind := ""
	if r.SlotsBehind > 0 {
		slotsBehind = strconv.FormatUint(r.SlotsBehind, 10)
	}

	return [][]string{{
		r.State,
		r.Health,
		r.Uptime.String(),
		r.Version,
		strconv.FormatBool(r.Synced),
		tipInfo,
		slotsBehind,
	}}
}

// statusResultFromProto converts the proto status response to our result type.
func statusResultFromProto(msg *lifecyclev1alpha1.GetStatusResponse) statusResult {
	result := statusResult{
		State:   lifecycleStateString(msg.GetState()),
		Health:  healthStatusString(msg.GetHealth()),
		Uptime:  msg.GetUptime().AsDuration(),
		Version: msg.GetVersion(),
	}

	if sync := msg.GetSync(); sync != nil {
		result.Synced = sync.GetSynced()
		result.EstimatedNetworkTipSlot = sync.GetEstimatedNetworkTipSlot()
		result.SlotsBehind = sync.GetSlotsBehind()

		if tip := sync.GetTip(); tip != nil {
			if tip.GetSlot() != nil {
				slot := *tip.GetSlot()
				result.TipSlot = &slot
			}
			if tip.GetHash() != nil {
				hash := *tip.GetHash()
				result.TipHash = &hash
			}
			if tip.GetBlockNumber() != nil {
				blockNum := *tip.GetBlockNumber()
				result.TipBlockNumber = &blockNum
			}
		}
	}

	if msg.GetDeadline() != nil {
		deadline := msg.GetDeadline().AsTime()
		result.Deadline = &deadline
	}

	return result
}

func lifecycleStateString(state lifecyclev1alpha1.LifecycleState) string {
	switch state {
	case lifecyclev1alpha1.LifecycleState_LIFECYCLE_STATE_RUNNING:
		return "running"
	case lifecyclev1alpha1.LifecycleState_LIFECYCLE_STATE_STOPPING:
		return "stopping"
	case lifecyclev1alpha1.LifecycleState_LIFECYCLE_STATE_RESTARTING:
		return "restarting"
	default:
		return "unknown"
	}
}

func healthStatusString(health lifecyclev1alpha1.HealthStatus) string {
	switch health {
	case lifecyclev1alpha1.HealthStatus_HEALTH_STATUS_HEALTHY:
		return "healthy"
	case lifecyclev1alpha1.HealthStatus_HEALTH_STATUS_DEGRADED:
		return "degraded"
	case lifecyclev1alpha1.HealthStatus_HEALTH_STATUS_UNHEALTHY:
		return "unhealthy"
	default:
		return "unknown"
	}
}
