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
	"strings"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.CurrentProfile != "default" {
		t.Errorf("expected current profile to be 'default', got %q", cfg.CurrentProfile)
	}

	if len(cfg.Profiles) != 1 {
		t.Errorf("expected 1 profile, got %d", len(cfg.Profiles))
	}

	profile := cfg.GetProfile("default")
	if profile == nil {
		t.Fatal("expected default profile to exist")
	}

	if profile.Connect != "localhost:8080" {
		t.Errorf("expected connect to be 'localhost:8080', got %q", profile.Connect)
	}

	if profile.Timeout != 30*time.Second {
		t.Errorf("expected timeout to be 30s, got %v", profile.Timeout)
	}

	if profile.Output != "text" {
		t.Errorf("expected output to be 'text', got %q", profile.Output)
	}
}

func TestGetProfile(t *testing.T) {
	cfg := Default()

	// Test getting existing profile
	profile := cfg.GetProfile("default")
	if profile == nil {
		t.Fatal("expected default profile to exist")
	}

	// Test getting non-existent profile
	profile = cfg.GetProfile("nonexistent")
	if profile != nil {
		t.Error("expected nil for non-existent profile")
	}

	// Test getting profile with empty name (should use current)
	profile = cfg.GetProfile("")
	if profile == nil {
		t.Error("expected profile when using empty name")
	}
}

func TestSetProfile(t *testing.T) {
	cfg := Default()

	newProfile := Profile{
		Connect: "mainnet.example.com:443",
		TLS:     true,
		Timeout: 60 * time.Second,
		Output:  "json",
	}

	cfg.SetProfile("mainnet", newProfile)

	profile := cfg.GetProfile("mainnet")
	if profile == nil {
		t.Fatal("expected mainnet profile to exist")
	}

	if profile.Connect != "mainnet.example.com:443" {
		t.Errorf("expected connect to be 'mainnet.example.com:443', got %q", profile.Connect)
	}

	if !profile.TLS {
		t.Error("expected TLS to be true")
	}
}

func TestDeleteProfile(t *testing.T) {
	cfg := Default()

	cfg.SetProfile("temp", Profile{Connect: "temp:1234"})

	if !cfg.DeleteProfile("temp") {
		t.Error("expected DeleteProfile to return true for existing profile")
	}

	if cfg.GetProfile("temp") != nil {
		t.Error("expected profile to be deleted")
	}

	if cfg.DeleteProfile("nonexistent") {
		t.Error("expected DeleteProfile to return false for non-existent profile")
	}
}

func TestListProfiles(t *testing.T) {
	cfg := Default()
	cfg.SetProfile("mainnet", Profile{Connect: "mainnet:443"})
	cfg.SetProfile("preview", Profile{Connect: "preview:443"})

	profiles := cfg.ListProfiles()
	if len(profiles) != 3 {
		t.Errorf("expected 3 profiles, got %d", len(profiles))
	}

	// Check that all expected profiles are present
	expected := map[string]bool{
		"default": false,
		"mainnet": false,
		"preview": false,
	}

	for _, name := range profiles {
		if _, ok := expected[name]; ok {
			expected[name] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("expected profile %q to be in list", name)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name:    "valid default config",
			cfg:     Default(),
			wantErr: false,
		},
		{
			name: "valid config with multiple profiles",
			cfg: &Config{
				CurrentProfile: "mainnet",
				Profiles: map[string]Profile{
					"mainnet": {
						Connect: "mainnet:443",
						TLS:     true,
						Output:  "json",
					},
					"preview": {
						Connect: "preview:443",
						Output:  "yaml",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid output format",
			cfg: &Config{
				CurrentProfile: "default",
				Profiles: map[string]Profile{
					"default": {
						Connect: "localhost:8080",
						Output:  "invalid",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "mismatched client cert and key",
			cfg: &Config{
				CurrentProfile: "default",
				Profiles: map[string]Profile{
					"default": {
						Connect:    "localhost:8080",
						ClientCert: "/path/to/cert.pem",
						ClientKey:  "",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "current profile does not exist",
			cfg: &Config{
				CurrentProfile: "nonexistent",
				Profiles: map[string]Profile{
					"default": {
						Connect: "localhost:8080",
					},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSaveAndLoad(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Set the config path via environment variable
	t.Setenv("DINGOCTL_CONFIG", configPath)

	// Create a config
	cfg := Default()
	cfg.SetProfile("mainnet", Profile{
		Connect: "mainnet.example.com:443",
		TLS:     true,
		Timeout: 60 * time.Second,
		Output:  "json",
	})

	// Save the config
	if err := cfg.SaveTo(configPath); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Check that the file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file was not created")
	}

	// Load the config
	loadedCfg, err := LoadFrom(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Verify the loaded config matches
	if loadedCfg.CurrentProfile != cfg.CurrentProfile {
		t.Errorf("expected current profile %q, got %q", cfg.CurrentProfile, loadedCfg.CurrentProfile)
	}

	if len(loadedCfg.Profiles) != len(cfg.Profiles) {
		t.Errorf("expected %d profiles, got %d", len(cfg.Profiles), len(loadedCfg.Profiles))
	}

	mainnetProfile := loadedCfg.GetProfile("mainnet")
	if mainnetProfile == nil {
		t.Fatal("expected mainnet profile to exist")
	}

	if mainnetProfile.Connect != "mainnet.example.com:443" {
		t.Errorf("expected connect to be 'mainnet.example.com:443', got %q", mainnetProfile.Connect)
	}

	if !mainnetProfile.TLS {
		t.Error("expected TLS to be true")
	}
}

func TestLoadNonExistentFile(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()
	nonExistentPath := filepath.Join(tmpDir, "nonexistent.yaml")

	// Loading a non-existent file should return defaults
	cfg, err := LoadFrom(nonExistentPath)
	if err != nil {
		t.Fatalf("expected no error when loading non-existent file, got %v", err)
	}

	if cfg.CurrentProfile != "default" {
		t.Errorf("expected default profile, got %q", cfg.CurrentProfile)
	}

	if len(cfg.Profiles) != 1 {
		t.Errorf("expected 1 profile, got %d", len(cfg.Profiles))
	}
}

func TestLoadWithEnvVarsValidation(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()
	nonExistentPath := filepath.Join(tmpDir, "nonexistent.yaml")

	// Set invalid output format via env var
	t.Setenv("DINGOCTL_OUTPUT", "invalid")

	// Loading should fail validation even without a config file
	_, err := LoadFrom(nonExistentPath)
	if err == nil {
		t.Fatal("expected validation error for invalid output format from env var")
	}

	if !strings.Contains(err.Error(), "invalid output format") {
		t.Errorf("expected 'invalid output format' error, got: %v", err)
	}

	// Test mismatched client cert/key
	t.Setenv("DINGOCTL_OUTPUT", "json")
	t.Setenv("DINGOCTL_CLIENT_CERT", "/path/to/cert.pem")

	_, err = LoadFrom(nonExistentPath)
	if err == nil {
		t.Fatal("expected validation error for mismatched client cert/key")
	}

	if !strings.Contains(err.Error(), "client_cert and client_key must be provided together") {
		t.Errorf("expected client cert/key error, got: %v", err)
	}
}
