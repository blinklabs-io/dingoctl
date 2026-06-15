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

package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
	"go.yaml.in/yaml/v3"
)

// Load reads the configuration from the XDG-compliant path.
// If the file doesn't exist, it returns a default configuration.
// Environment variables (DINGOCTL_*) are applied on top of the file config.
func Load() (*Config, error) {
	return LoadFrom(GetConfigPath())
}

// LoadFrom reads configuration from the specified path.
func LoadFrom(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	// Set up environment variable mapping
	v.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	v.SetEnvPrefix("DINGOCTL")
	v.AutomaticEnv()

	// Try to read the config file
	if err := v.ReadInConfig(); err != nil {
		// Check if the file doesn't exist
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			// No config file found; return defaults but still allow env vars
			cfg := Default()
			// Apply any environment variable overrides to the default profile
			if defaultProfile, ok := cfg.Profiles["default"]; ok {
				if connect := v.GetString("connect"); connect != "" {
					defaultProfile.Connect = connect
				}
				if v.IsSet("tls") {
					defaultProfile.TLS = v.GetBool("tls")
				}
				if v.IsSet("insecure") {
					defaultProfile.Insecure = v.GetBool("insecure")
				}
				if caCert := v.GetString("ca_cert"); caCert != "" {
					defaultProfile.CACert = caCert
				}
				if clientCert := v.GetString("client_cert"); clientCert != "" {
					defaultProfile.ClientCert = clientCert
				}
				if clientKey := v.GetString("client_key"); clientKey != "" {
					defaultProfile.ClientKey = clientKey
				}
				if v.IsSet("timeout") {
					defaultProfile.Timeout = v.GetDuration("timeout")
				}
				if output := v.GetString("output"); output != "" {
					defaultProfile.Output = output
				}
				cfg.Profiles["default"] = defaultProfile
			}
			// Validate config even when loaded from env vars only
			if err := cfg.Validate(); err != nil {
				return nil, err
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("error reading config file %q: %w", path, err)
	}

	// Unmarshal into our Config struct
	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("error parsing config file %q: %w", path, err)
	}

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save writes the configuration to the XDG-compliant path.
func (c *Config) Save() error {
	return c.SaveTo(GetConfigPath())
}

// SaveTo writes the configuration to the specified path.
func (c *Config) SaveTo(path string) error {
	// Ensure the directory for the given path exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Validate before saving
	if err := c.Validate(); err != nil {
		return err
	}

	// Marshal to YAML
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file %q: %w", path, err)
	}

	return nil
}

// Validate checks that the configuration is valid.
func (c *Config) Validate() error {
	if c.CurrentProfile == "" {
		c.CurrentProfile = "default"
	}

	if c.Profiles == nil {
		c.Profiles = make(map[string]Profile)
	}

	// Ensure the current profile exists
	if _, ok := c.Profiles[c.CurrentProfile]; !ok && len(c.Profiles) > 0 {
		return fmt.Errorf("current profile %q does not exist", c.CurrentProfile)
	}

	// Validate each profile
	for name, profile := range c.Profiles {
		if err := profile.Validate(); err != nil {
			return fmt.Errorf("profile %q: %w", name, err)
		}
	}

	return nil
}

// Validate checks that a profile has valid settings.
func (p *Profile) Validate() error {
	// Validate output format if specified
	if p.Output != "" {
		validFormats := []string{"text", "json", "yaml"}
		valid := false
		for _, format := range validFormats {
			if p.Output == format {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid output format %q: must be one of text, json, yaml", p.Output)
		}
	}

	// Validate that client cert and key are both present or both absent
	if (p.ClientCert == "") != (p.ClientKey == "") {
		return errors.New("client_cert and client_key must be provided together")
	}

	return nil
}
