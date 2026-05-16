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
	"os"

	"github.com/spf13/cobra"
)

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for dingoctl.

To load completions in your current shell session:

  Bash:
    source <(dingoctl completion bash)

  Zsh:
    # If shell completion is not already enabled in your environment,
    # enable it first by running:
    #   echo "autoload -U compinit; compinit" >> ~/.zshrc
    source <(dingoctl completion zsh)

  Fish:
    dingoctl completion fish | source

  PowerShell:
    dingoctl completion powershell | Out-String | Invoke-Expression

To make completions persist across sessions, add the source command
to your shell profile (e.g. ~/.bashrc, ~/.zshrc) or follow your
distribution's instructions for installing completion scripts.`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				// unreachable due to OnlyValidArgs, but keeps exhaustive check happy
				return nil
			}
		},
	}
	return cmd
}
