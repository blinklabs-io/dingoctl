// Copyright 2025 Blink Labs Software
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

	"github.com/blinklabs-io/dingoctl/internal/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	var short bool

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the dingoctl version",
		Long:  `Print the full version, commit hash, and build date for dingoctl.`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			if short {
				fmt.Fprintln(cmd.OutOrStdout(), version.Version)
				return
			}
			fmt.Fprintf(
				cmd.OutOrStdout(),
				"dingoctl %s\n",
				version.GetVersionString(),
			)
		},
	}

	cmd.Flags().BoolVar(&short, "short", false, "print only the version number")
	return cmd
}
