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
	"fmt"
	"time"

	"connectrpc.com/connect"
	lifecyclev1alpha1 "github.com/blinklabs-io/bark/proto/v1alpha1/lifecycle"
	"github.com/blinklabs-io/dingoctl/internal/client"
	"github.com/blinklabs-io/dingoctl/internal/errs"
	"github.com/blinklabs-io/dingoctl/internal/version"
	"github.com/spf13/cobra"
)

type versionResult struct {
	CLI       string  `json:"cli" yaml:"cli"`
	Node      *string `json:"node,omitempty" yaml:"node,omitempty"`
	NodeError *string `json:"node_error,omitempty" yaml:"node_error,omitempty"`
}

func (r versionResult) String() string {
	if r.Node != nil {
		return fmt.Sprintf("dingoctl %s\nNode: %s", r.CLI, *r.Node)
	}
	if r.NodeError != nil {
		return fmt.Sprintf("dingoctl %s\nNode: <%s>", r.CLI, *r.NodeError)
	}
	return fmt.Sprintf("dingoctl %s", r.CLI)
}

func newVersionCmd() *cobra.Command {
	var short bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long: `Print the dingoctl CLI version and the connected node's version.

With --short, only the CLI version number is printed (useful for scripts).
Otherwise, the CLI version and the connected node's version are shown.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if short {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), version.Version)
				return err
			}

			result := versionResult{CLI: version.GetVersionString()}

			// Try to get node version info.
			c, err := GetClient()
			if err != nil {
				msg := fmt.Sprintf("unable to connect: %s", errs.Format(err))
				result.NodeError = &msg
				return GetOutputPrinter().Print(result)
			}

			// Version command should stay quick: this is best-effort metadata, not a required RPC.
			statusCtx, cancel := context.WithTimeout(client.WithNoRetry(cmd.Context()), 3*time.Second)
			defer cancel()
			resp, err := c.LifecycleService().GetStatus(statusCtx, connect.NewRequest(&lifecyclev1alpha1.GetStatusRequest{}))
			if err != nil {
				msg := fmt.Sprintf("unable to get status: %s", errs.Format(err))
				result.NodeError = &msg
				return GetOutputPrinter().Print(result)
			}

			nodeVersion := resp.Msg.Version
			result.Node = &nodeVersion
			return GetOutputPrinter().Print(result)
		},
	}

	cmd.Flags().BoolVar(&short, "short", false, "print only the CLI version number")
	return cmd
}
