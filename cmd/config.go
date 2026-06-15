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
	"sort"
	"strings"
	"time"

	"github.com/blinklabs-io/dingoctl/internal/config"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// newConfigCmd creates the config command with all its subcommands.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage dingoctl configuration and profiles",
		Long: `Manage dingoctl configuration and profiles.

Configuration is stored at ~/.config/dingoctl/config.yaml (XDG-compliant).
You can override the location with DINGOCTL_CONFIG or XDG_CONFIG_HOME.

Profiles let you maintain multiple named configurations and switch between them.
Each profile can have its own connection settings, TLS configuration, and defaults.`,
	}

	cmd.AddCommand(newConfigGetCmd())
	cmd.AddCommand(newConfigSetCmd())
	cmd.AddCommand(newConfigListCmd())
	cmd.AddCommand(newConfigUseCmd())
	cmd.AddCommand(newConfigCurrentCmd())
	cmd.AddCommand(newConfigPathCmd())

	return cmd
}

// newConfigGetCmd creates the 'config get' subcommand.
func newConfigGetCmd() *cobra.Command {
	var profileName string

	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Long: `Get a configuration value from the current or specified profile.

Examples:
  dingoctl config get connect
  dingoctl config get --profile mainnet timeout
  dingoctl config get output`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if profileName == "" {
				profileName = cfg.CurrentProfile
			}

			profile := cfg.GetProfile(profileName)
			if profile == nil {
				return fmt.Errorf("profile %q does not exist", profileName)
			}

			key := args[0]
			value, err := getProfileField(profile, key)
			if err != nil {
				return err
			}

			fmt.Println(value)
			return nil
		},
	}

	cmd.Flags().StringVar(&profileName, "profile", "", "profile name (default: current profile)")

	return cmd
}

// newConfigSetCmd creates the 'config set' subcommand.
func newConfigSetCmd() *cobra.Command {
	var profileName string

	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a configuration value in the current or specified profile.

If the profile doesn't exist, it will be created.

Examples:
  dingoctl config set connect localhost:8080
  dingoctl config set --profile mainnet connect mainnet.example.com:443
  dingoctl config set --profile mainnet tls true
  dingoctl config set timeout 60s`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if profileName == "" {
				profileName = cfg.CurrentProfile
			}

			profile := cfg.GetProfile(profileName)
			if profile == nil {
				// Create new profile with defaults
				profile = &config.Profile{
					Connect: "localhost:8080",
					Timeout: 30 * time.Second,
					Output:  "text",
				}
			}

			key := args[0]
			value := args[1]

			if err := setProfileField(profile, key, value); err != nil {
				return err
			}

			cfg.SetProfile(profileName, *profile)

			if err := cfg.Save(); err != nil {
				return err
			}

			fmt.Printf("Set %s = %s in profile %q\n", key, value, profileName)
			return nil
		},
	}

	cmd.Flags().StringVar(&profileName, "profile", "", "profile name (default: current profile)")

	return cmd
}

// newConfigListCmd creates the 'config list' subcommand.
func newConfigListCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all profiles",
		Long: `List all configured profiles.

Use --verbose to show all settings for each profile.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			if len(cfg.Profiles) == 0 {
				fmt.Println("No profiles configured")
				return nil
			}

			names := cfg.ListProfiles()
			sort.Strings(names)

			for _, name := range names {
				profile := cfg.GetProfile(name)
				if profile == nil {
					continue
				}

				marker := " "
				if name == cfg.CurrentProfile {
					marker = "*"
				}

				if verbose {
					fmt.Printf("%s %s:\n", marker, name)
					data, _ := yaml.Marshal(profile)
					for _, line := range strings.Split(string(data), "\n") {
						if line != "" {
							fmt.Printf("    %s\n", line)
						}
					}
				} else {
					fmt.Printf("%s %s\n", marker, name)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "show all settings for each profile")

	return cmd
}

// newConfigUseCmd creates the 'config use' subcommand.
func newConfigUseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "use <profile>",
		Short: "Switch to a different profile",
		Long: `Switch the current profile to the specified profile.

The current profile is used by default when no --profile flag is provided.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			profileName := args[0]
			if cfg.GetProfile(profileName) == nil {
				return fmt.Errorf("profile %q does not exist", profileName)
			}

			cfg.CurrentProfile = profileName

			if err := cfg.Save(); err != nil {
				return err
			}

			fmt.Printf("Switched to profile %q\n", profileName)
			return nil
		},
	}

	return cmd
}

// newConfigCurrentCmd creates the 'config current' subcommand.
func newConfigCurrentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "current",
		Short: "Show the current profile",
		Long:  `Show the name of the current profile.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}

			fmt.Println(cfg.CurrentProfile)
			return nil
		},
	}

	return cmd
}

// newConfigPathCmd creates the 'config path' subcommand.
func newConfigPathCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "path",
		Short: "Show the configuration file path",
		Long:  `Show the path to the configuration file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(config.GetConfigPath())
			return nil
		},
	}

	return cmd
}

// getProfileField retrieves a field value from a profile.
func getProfileField(p *config.Profile, key string) (string, error) {
	switch key {
	case "connect":
		return p.Connect, nil
	case "tls":
		return fmt.Sprintf("%t", p.TLS), nil
	case "insecure":
		return fmt.Sprintf("%t", p.Insecure), nil
	case "ca_cert", "ca-cert":
		return p.CACert, nil
	case "client_cert", "client-cert":
		return p.ClientCert, nil
	case "client_key", "client-key":
		return p.ClientKey, nil
	case "timeout":
		return p.Timeout.String(), nil
	case "output":
		return p.Output, nil
	default:
		return "", fmt.Errorf("unknown config key %q", key)
	}
}

// setProfileField sets a field value in a profile.
func setProfileField(p *config.Profile, key, value string) error {
	switch key {
	case "connect":
		p.Connect = value
	case "tls":
		p.TLS = value == "true" || value == "1"
	case "insecure":
		p.Insecure = value == "true" || value == "1"
	case "ca_cert", "ca-cert":
		p.CACert = value
	case "client_cert", "client-cert":
		p.ClientCert = value
	case "client_key", "client-key":
		p.ClientKey = value
	case "timeout":
		d, err := time.ParseDuration(value)
		if err != nil {
			return fmt.Errorf("invalid timeout %q: %w", value, err)
		}
		p.Timeout = d
	case "output":
		if value != "text" && value != "json" && value != "yaml" {
			return fmt.Errorf("invalid output format %q: must be text, json, or yaml", value)
		}
		p.Output = value
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}
