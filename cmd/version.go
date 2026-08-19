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

	"connectrpc.com/connect"
	lifecyclev1alpha1 "github.com/blinklabs-io/bark/proto/v1alpha1/lifecycle"
	"github.com/blinklabs-io/dingoctl/internal/version"
	"github.com/spf13/cobra"
)

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

			// Print CLI version
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "dingoctl %s\n", version.GetVersionString()); err != nil {
				return err
			}

			// Try to get node version info
			c, err := GetClient()
			if err != nil {
				// If we can't connect, just show CLI version
				_, writeErr := fmt.Fprintf(cmd.OutOrStderr(), "\nNode: <unable to connect: %v>\n", err)
				return writeErr
			}

			resp, err := c.LifecycleService().GetStatus(cmd.Context(), connect.NewRequest(&lifecyclev1alpha1.GetStatusRequest{}))
			if err != nil {
				// If status fails, show CLI version but note the error
				_, writeErr := fmt.Fprintf(cmd.OutOrStderr(), "\nNode: <unable to get status: %v>\n", err)
				return writeErr
			}

			// Show node version from status response
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Node: %s\n", resp.Msg.Version); err != nil {
				return err
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&short, "short", false, "print only the CLI version number")
	return cmd
}
