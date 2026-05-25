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

// Package cmd holds every Cobra command for dingoctl.
package cmd

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/blinklabs-io/dingoctl/internal/errs"
	"github.com/blinklabs-io/dingoctl/internal/output"
	"github.com/blinklabs-io/dingoctl/internal/version"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// GlobalFlags holds the values bound to persistent root-level flags.
// Sub-commands read these via the exported accessors or through viper.
type GlobalFlags struct {
	ConfigFile string
	Connect    string
	TLS        bool
	Insecure   bool
	Timeout    time.Duration
	Output     string
	Quiet      bool
	Verbose    bool
}

var globalFlags GlobalFlags

// rootCmd is the base command for dingoctl.
var rootCmd = &cobra.Command{
	Use:   "dingoctl",
	Short: "The only way to control a Dingo in the wild",
	Long: `dingoctl is the command-line interface for managing a running Dingo node.

It communicates with the node over the Bark gRPC API.  Point it at your
node with --connect (or $DINGOCTL_CONNECT) and then run any sub-command.`,
	Version: version.GetVersionString(),
	// Silence cobra's default error printing; we handle it ourselves.
	SilenceErrors: true,
	SilenceUsage:  true,
	// Validate --output before any sub-command runs.
	PersistentPreRunE: persistentPreRun,
}

// Execute is the single entry-point called by main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		errs.Die(err)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	pf := rootCmd.PersistentFlags()

	// config file (handled separately from viper auto-config)
	pf.StringVar(
		&globalFlags.ConfigFile,
		"config", "",
		"config file (default: $HOME/.dingoctl.yaml)",
	)

	// connection flags
	pf.StringVar(
		&globalFlags.Connect,
		"connect", "localhost:8080",
		"address of the Dingo node (host:port)",
	)
	pf.BoolVar(
		&globalFlags.TLS,
		"tls", false,
		"use TLS when connecting to the node",
	)
	pf.BoolVar(
		&globalFlags.Insecure,
		"insecure", false,
		"skip TLS certificate verification (implies --tls)",
	)
	pf.DurationVar(
		&globalFlags.Timeout,
		"timeout", 30*time.Second,
		"timeout for requests to the node",
	)

	// output flags
	pf.StringVar(
		&globalFlags.Output,
		"output", "text",
		"output format: text, json, yaml",
	)
	pf.BoolVar(
		&globalFlags.Quiet,
		"quiet", false,
		"suppress all non-error output",
	)
	pf.BoolVar(
		&globalFlags.Verbose,
		"verbose", false,
		"enable verbose/debug output",
	)

	// bind flags to viper so env vars and config files populate them too
	_ = viper.BindPFlag("connect", pf.Lookup("connect"))
	_ = viper.BindPFlag("tls", pf.Lookup("tls"))
	_ = viper.BindPFlag("insecure", pf.Lookup("insecure"))
	_ = viper.BindPFlag("timeout", pf.Lookup("timeout"))
	_ = viper.BindPFlag("output", pf.Lookup("output"))
	_ = viper.BindPFlag("quiet", pf.Lookup("quiet"))
	_ = viper.BindPFlag("verbose", pf.Lookup("verbose"))

	// allow env-var overrides of the form DINGOCTL_<FLAG>
	viper.SetEnvPrefix("DINGOCTL")
	viper.AutomaticEnv()

	// sub-commands
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newCompletionCmd())
}

// initConfig sets up the config file search paths via Viper.
// The actual read happens in persistentPreRun so errors can be returned.
func initConfig() {
	if globalFlags.ConfigFile != "" {
		viper.SetConfigFile(globalFlags.ConfigFile)
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			viper.AddConfigPath(home)
		}
		viper.AddConfigPath(".")
		viper.SetConfigName(".dingoctl")
		viper.SetConfigType("yaml")
	}
}

// persistentPreRun validates flags that apply to every sub-command.
func persistentPreRun(cmd *cobra.Command, _ []string) error {
	// Read the config file now so parse errors surface as command errors
	// (non-zero exit) rather than being silently ignored.
	if err := viper.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return fmt.Errorf("config file error: %w", err)
		}
	}

	// Hydrate all global options from the canonical Viper values so that
	// env-var overrides (DINGOCTL_*) and config-file values are reflected in
	// globalFlags, not just explicit CLI flags.
	globalFlags.Connect = viper.GetString("connect")
	globalFlags.TLS = viper.GetBool("tls")
	globalFlags.Insecure = viper.GetBool("insecure")
	globalFlags.Timeout = viper.GetDuration("timeout")
	globalFlags.Verbose = viper.GetBool("verbose")
	globalFlags.Quiet = viper.GetBool("quiet")

	// --insecure implies --tls.  Apply to both globalFlags and Viper so that
	// any code reading from either source sees the canonical value.
	if globalFlags.Insecure {
		globalFlags.TLS = true
		viper.Set("tls", true)
	}

	outputFormat := viper.GetString("output")
	if !output.Format(outputFormat).IsValid() {
		return fmt.Errorf(
			"invalid --output %q: must be one of text, json, yaml",
			outputFormat,
		)
	}
	globalFlags.Output = outputFormat
	return nil
}

// GetOutputPrinter constructs a Printer from the current global flags.
func GetOutputPrinter() *output.Printer {
	return output.New(
		os.Stdout,
		output.Format(globalFlags.Output),
		globalFlags.Quiet,
	)
}
