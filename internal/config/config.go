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

// Package config provides typed configuration with profiles, environment
// variable mapping, and XDG-compliant file paths.
package config

import (
	"sort"
	"time"
)

// Config is the top-level configuration structure that holds global settings
// and named profiles.
type Config struct {
	// CurrentProfile is the name of the active profile.  When empty,
	// "default" is used.
	CurrentProfile string `mapstructure:"current_profile" yaml:"current_profile,omitempty"`

	// Profiles is a map of profile name to profile configuration.
	Profiles map[string]Profile `mapstructure:"profiles" yaml:"profiles"`
}

// Profile holds all configuration settings for connecting to and interacting
// with a Dingo node.  Each field can be overridden by a corresponding
// environment variable (DINGOCTL_<FIELD>) or command-line flag.
type Profile struct {
	// Connect is the address of the Dingo node (host:port).
	Connect string `mapstructure:"connect" yaml:"connect,omitempty"`

	// TLS enables TLS for the connection.
	TLS bool `mapstructure:"tls" yaml:"tls,omitempty"`

	// Insecure skips TLS certificate verification (implies TLS).
	Insecure bool `mapstructure:"insecure" yaml:"insecure,omitempty"`

	// CACert is the path to a PEM CA certificate for server verification.
	CACert string `mapstructure:"ca_cert" yaml:"ca_cert,omitempty"`

	// ClientCert is the path to a PEM client certificate for mTLS.
	ClientCert string `mapstructure:"client_cert" yaml:"client_cert,omitempty"`

	// ClientKey is the path to a PEM client key for mTLS.
	ClientKey string `mapstructure:"client_key" yaml:"client_key,omitempty"`

	// Timeout is the default timeout for requests to the node.
	Timeout time.Duration `mapstructure:"timeout" yaml:"timeout,omitempty"`

	// Output is the default output format: text, json, or yaml.
	Output string `mapstructure:"output" yaml:"output,omitempty"`
}

// Default returns a Config with sensible defaults that work without a config file.
func Default() *Config {
	return &Config{
		CurrentProfile: "default",
		Profiles: map[string]Profile{
			"default": {
				Connect: "localhost:8080",
				Timeout: 30 * time.Second,
				Output:  "text",
			},
		},
	}
}

// GetProfile returns the named profile, or nil if it doesn't exist.
func (c *Config) GetProfile(name string) *Profile {
	if name == "" {
		name = c.CurrentProfile
	}
	if name == "" {
		name = "default"
	}
	if p, ok := c.Profiles[name]; ok {
		return &p
	}
	return nil
}

// GetCurrentProfile returns the active profile.
func (c *Config) GetCurrentProfile() *Profile {
	return c.GetProfile(c.CurrentProfile)
}

// SetProfile creates or updates a profile.
func (c *Config) SetProfile(name string, profile Profile) {
	if c.Profiles == nil {
		c.Profiles = make(map[string]Profile)
	}
	c.Profiles[name] = profile
}

// DeleteProfile removes a profile.  Returns false if the profile doesn't exist.
func (c *Config) DeleteProfile(name string) bool {
	if _, ok := c.Profiles[name]; ok {
		delete(c.Profiles, name)
		return true
	}
	return false
}

// ListProfiles returns a sorted list of profile names.
func (c *Config) ListProfiles() []string {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
