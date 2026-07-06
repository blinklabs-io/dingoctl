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

package output

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestFormatIsValid(t *testing.T) {
	tests := []struct {
		name  string
		fmt   Format
		valid bool
	}{
		{"text is valid", FormatText, true},
		{"json is valid", FormatJSON, true},
		{"yaml is valid", FormatYAML, true},
		{"table is valid", FormatTable, true},
		{"invalid format", Format("invalid"), false},
		{"empty format", Format(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fmt.IsValid(); got != tt.valid {
				t.Errorf("Format.IsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

func TestColorEnabled(t *testing.T) {
	// Test NO_COLOR environment variable
	t.Run("NO_COLOR disables color", func(t *testing.T) {
		os.Setenv("NO_COLOR", "1")
		defer os.Unsetenv("NO_COLOR")

		if ColorEnabled(os.Stdout) {
			t.Error("ColorEnabled() should return false when NO_COLOR is set")
		}
	})

	// Test with a non-file writer
	t.Run("non-file writer disables color", func(t *testing.T) {
		os.Unsetenv("NO_COLOR")
		buf := &bytes.Buffer{}
		if ColorEnabled(buf) {
			t.Error("ColorEnabled() should return false for non-file writers")
		}
	})
}

func TestPrinterJSON(t *testing.T) {
	buf := &bytes.Buffer{}
	p := New(buf, FormatJSON, false)

	data := map[string]any{
		"name":  "test",
		"count": 42,
	}

	if err := p.Print(data); err != nil {
		t.Fatalf("Print() error = %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}

	if result["name"] != "test" || result["count"] != float64(42) {
		t.Errorf("JSON output mismatch: %v", result)
	}
}

func TestPrinterYAML(t *testing.T) {
	buf := &bytes.Buffer{}
	p := New(buf, FormatYAML, false)

	data := map[string]any{
		"name":  "test",
		"count": 42,
	}

	if err := p.Print(data); err != nil {
		t.Fatalf("Print() error = %v", err)
	}

	var result map[string]any
	if err := yaml.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse YAML output: %v", err)
	}

	if result["name"] != "test" || result["count"] != 42 {
		t.Errorf("YAML output mismatch: %v", result)
	}
}

func TestPrinterQuietMode(t *testing.T) {
	buf := &bytes.Buffer{}
	p := New(buf, FormatText, true)

	// All output should be suppressed in quiet mode
	p.Print("test")
	p.Println("test")
	p.Success("test")
	p.Error("test")
	p.Warning("test")
	p.Info("test")
	p.Header("test")
	p.KeyValue("key", "value")

	if buf.Len() > 0 {
		t.Errorf("Quiet mode should suppress all output, got: %s", buf.String())
	}
}

func TestPrinterText(t *testing.T) {
	buf := &bytes.Buffer{}
	p := New(buf, FormatText, false)

	tests := []struct {
		name string
		fn   func() error
		want string
	}{
		{"plain println", func() error { p.Println("test"); return nil }, "test"},
		{"success", func() error { return p.Success("done") }, "done"},
		{"error", func() error { return p.Error("failed") }, "failed"},
		{"warning", func() error { return p.Warning("caution") }, "caution"},
		{"info", func() error { return p.Info("note") }, "note"},
		{"header", func() error { return p.Header("Section") }, "Section"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			if err := tt.fn(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			output := strings.TrimSpace(buf.String())
			// Remove ANSI codes for comparison (they may be present)
			output = stripANSI(output)
			if !strings.Contains(output, tt.want) {
				t.Errorf("output %q should contain %q", output, tt.want)
			}
		})
	}
}

func TestTableRenderer(t *testing.T) {
	buf := &bytes.Buffer{}
	tr := NewTableRenderer(buf, false)

	table := &Table{
		Headers: []string{"Name", "Age", "City"},
		Rows: [][]string{
			{"Alice", "30", "NYC"},
			{"Bob", "25", "LA"},
			{"Charlie", "35", "SF"},
		},
	}

	if err := tr.Render(table); err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Alice") || !strings.Contains(output, "Bob") {
		t.Errorf("Table output missing data: %s", output)
	}
}

func TestTableRendererSimple(t *testing.T) {
	buf := &bytes.Buffer{}
	tr := NewTableRenderer(buf, false)

	table := &Table{
		Headers: []string{"Name", "Age"},
		Rows: [][]string{
			{"Alice", "30"},
			{"Bob", "25"},
		},
	}

	if err := tr.RenderSimple(table); err != nil {
		t.Fatalf("RenderSimple() error = %v", err)
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 3 {
		t.Errorf("Expected at least 3 lines (header, separator, rows), got %d", len(lines))
	}
}

func TestProgressBar(t *testing.T) {
	buf := &bytes.Buffer{}
	pb := NewProgressBar(buf, false, 20)

	if err := pb.Update(0.5, "Processing"); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "50%") {
		t.Errorf("Progress bar should show 50%%, got: %s", output)
	}

	buf.Reset()
	if err := pb.Complete("Done"); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	output = buf.String()
	if !strings.Contains(output, "100%") {
		t.Errorf("Completed progress bar should show 100%%, got: %s", output)
	}
}

func TestSpinner(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewSpinner(buf, false)

	s.Start("Loading")
	// Give it a moment to render
	s.Update("Processing")
	s.Stop()

	// Output should be cleared after stop
	// We can't easily test the intermediate states without actual timing
}

func TestPrinterProgressBarQuietMode(t *testing.T) {
	buf := &bytes.Buffer{}
	p := New(buf, FormatText, true)

	pb := p.NewProgressBar(40)
	if pb != nil {
		t.Error("NewProgressBar() should return nil in quiet mode")
	}

	sp := p.NewSpinner()
	if sp != nil {
		t.Error("NewSpinner() should return nil in quiet mode")
	}
}

// stripANSI removes ANSI escape codes from a string for testing.
func stripANSI(s string) string {
	// Simple ANSI code stripper for tests
	var result strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}
