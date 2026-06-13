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
	"os"
	"path/filepath"
)

// GetConfigPath returns the XDG-compliant configuration file path.
// It checks XDG_CONFIG_HOME first, falling back to ~/.config/dingoctl/config.yaml.
func GetConfigPath() string {
	// Check for explicit override first
	if path := os.Getenv("DINGOCTL_CONFIG"); path != "" {
		return path
	}

	// XDG_CONFIG_HOME takes precedence
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return filepath.Join(xdgConfig, "dingoctl", "config.yaml")
	}

	// Fall back to ~/.config/dingoctl/config.yaml
	home, err := os.UserHomeDir()
	if err != nil {
		// If we can't determine home, use a relative path
		return filepath.Join(".config", "dingoctl", "config.yaml")
	}
	return filepath.Join(home, ".config", "dingoctl", "config.yaml")
}

// GetConfigDir returns the directory containing the config file.
func GetConfigDir() string {
	return filepath.Dir(GetConfigPath())
}

// EnsureConfigDir creates the config directory if it doesn't exist.
func EnsureConfigDir() error {
	dir := GetConfigDir()
	return os.MkdirAll(dir, 0755)
}
