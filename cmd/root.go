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
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/blinklabs-io/dingoctl/internal/config"
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
	Profile    string
	Connect    string
	TLS        bool
	Insecure   bool
	CACert     string
	ClientCert string
	ClientKey  string
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
		"config file (default: ~/.config/dingoctl/config.yaml)",
	)

	// profile selection
	pf.StringVar(
		&globalFlags.Profile,
		"profile", "",
		"config profile to use (default: current profile)",
	)

	// connection flags
	pf.StringVar(
		&globalFlags.Connect,
		"connect", "",
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
	pf.StringVar(
		&globalFlags.CACert,
		"ca-cert", "",
		"path to a PEM CA certificate for server verification",
	)
	pf.StringVar(
		&globalFlags.ClientCert,
		"client-cert", "",
		"path to a PEM client certificate for mTLS",
	)
	pf.StringVar(
		&globalFlags.ClientKey,
		"client-key", "",
		"path to a PEM client key for mTLS",
	)
	pf.DurationVar(
		&globalFlags.Timeout,
		"timeout", 0,
		"timeout for requests to the node",
	)

	// output flags
	pf.StringVar(
		&globalFlags.Output,
		"output", "",
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
	_ = viper.BindPFlag("profile", pf.Lookup("profile"))
	_ = viper.BindPFlag("connect", pf.Lookup("connect"))
	_ = viper.BindPFlag("tls", pf.Lookup("tls"))
	_ = viper.BindPFlag("insecure", pf.Lookup("insecure"))
	_ = viper.BindPFlag("ca-cert", pf.Lookup("ca-cert"))
	_ = viper.BindPFlag("client-cert", pf.Lookup("client-cert"))
	_ = viper.BindPFlag("client-key", pf.Lookup("client-key"))
	_ = viper.BindPFlag("timeout", pf.Lookup("timeout"))
	_ = viper.BindPFlag("output", pf.Lookup("output"))
	_ = viper.BindPFlag("quiet", pf.Lookup("quiet"))
	_ = viper.BindPFlag("verbose", pf.Lookup("verbose"))

	// allow env-var overrides of the form DINGOCTL_<FLAG>.
	// The replacer normalises hyphens to underscores so that flags like
	// --ca-cert map to DINGOCTL_CA_CERT (hyphens are invalid in env var names).
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.SetEnvPrefix("DINGOCTL")
	viper.AutomaticEnv()

	// sub-commands
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newCompletionCmd())
	rootCmd.AddCommand(newConfigCmd())
}

// initConfig sets up the config file search paths via Viper.
// The actual read happens in persistentPreRun so errors can be returned.
func initConfig() {
	if globalFlags.ConfigFile != "" {
		viper.SetConfigFile(globalFlags.ConfigFile)
	} else {
		// Use XDG-compliant config path by default
		configPath := config.GetConfigPath()
		viper.SetConfigFile(configPath)
		viper.SetConfigType("yaml")
	}
}

// persistentPreRun validates flags that apply to every sub-command.
func persistentPreRun(cmd *cobra.Command, _ []string) error {
	// Load the configuration from the XDG-compliant path
	var cfg *config.Config
	var err error

	if globalFlags.ConfigFile != "" {
		cfg, err = config.LoadFrom(globalFlags.ConfigFile)
	} else {
		cfg, err = config.Load()
	}

	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	// Determine which profile to use
	profileName := globalFlags.Profile
	if profileName == "" {
		// Check environment variable
		profileName = os.Getenv("DINGOCTL_PROFILE")
	}
	if profileName == "" {
		// Use current profile from config
		profileName = cfg.CurrentProfile
	}

	// Get the profile settings
	profile := cfg.GetProfile(profileName)
	if profile == nil {
		return fmt.Errorf("profile %q does not exist", profileName)
	}

	// Apply profile settings to viper (these become the base layer)
	if profile.Connect != "" {
		viper.SetDefault("connect", profile.Connect)
	}
	viper.SetDefault("tls", profile.TLS)
	viper.SetDefault("insecure", profile.Insecure)
	if profile.CACert != "" {
		viper.SetDefault("ca-cert", profile.CACert)
	}
	if profile.ClientCert != "" {
		viper.SetDefault("client-cert", profile.ClientCert)
	}
	if profile.ClientKey != "" {
		viper.SetDefault("client-key", profile.ClientKey)
	}
	if profile.Timeout > 0 {
		viper.SetDefault("timeout", profile.Timeout)
	}
	if profile.Output != "" {
		viper.SetDefault("output", profile.Output)
	}

	// Hydrate all global options from the canonical Viper values.
	// Priority order: CLI flags > env vars > profile settings > defaults
	globalFlags.Profile = profileName
	globalFlags.Connect = viper.GetString("connect")
	globalFlags.TLS = viper.GetBool("tls")
	globalFlags.Insecure = viper.GetBool("insecure")
	globalFlags.CACert = viper.GetString("ca-cert")
	globalFlags.ClientCert = viper.GetString("client-cert")
	globalFlags.ClientKey = viper.GetString("client-key")

	// Get timeout; explicit zero is valid (no timeout)
	if viper.IsSet("timeout") {
		globalFlags.Timeout = viper.GetDuration("timeout")
	} else if profile.Timeout > 0 {
		globalFlags.Timeout = profile.Timeout
	} else {
		globalFlags.Timeout = 30 * time.Second
	}

	globalFlags.Verbose = viper.GetBool("verbose")
	globalFlags.Quiet = viper.GetBool("quiet")

	// --insecure implies --tls.  Apply to both globalFlags and Viper so that
	// any code reading from either source sees the canonical value.
	if globalFlags.Insecure {
		globalFlags.TLS = true
		viper.Set("tls", true)
	}

	// A custom CA cert or mTLS keypair only makes sense over TLS.
	if globalFlags.CACert != "" || globalFlags.ClientCert != "" || globalFlags.ClientKey != "" {
		globalFlags.TLS = true
		viper.Set("tls", true)
	}

	// --client-cert and --client-key must always be provided together.
	if (globalFlags.ClientCert == "") != (globalFlags.ClientKey == "") {
		return fmt.Errorf("--client-cert and --client-key must be provided together")
	}

	// Validate and set output format
	outputFormat := viper.GetString("output")
	if outputFormat == "" {
		outputFormat = "text"
	}
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
