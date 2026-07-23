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
	"strings"
	"testing"
)

// TestFormatIsValid checks that text/json/yaml/table are accepted and any
// other value (including empty) is rejected.
func TestFormatIsValid(t *testing.T) {
	cases := map[Format]bool{
		FormatText:      true,
		FormatJSON:      true,
		FormatYAML:      true,
		FormatTable:     true,
		Format("bogus"): false,
		Format(""):      false,
	}
	for f, want := range cases {
		if got := f.IsValid(); got != want {
			t.Errorf("Format(%q).IsValid(): got %v, want %v", f, got, want)
		}
	}
}

// listResult implements TableWriter directly, the way a command's own
// "list" result type would (see cmd's snapshotListResult).
type listResult struct {
	Items []string
}

func (r listResult) TableHeader() []string { return []string{"item"} }

func (r listResult) TableRows() [][]string {
	rows := make([][]string, len(r.Items))
	for i, item := range r.Items {
		rows[i] = []string{item}
	}
	return rows
}

// TestPrintTable_UsesTableWriterWhenImplemented checks that table mode
// renders a TableWriter's own header/rows, one row per list element.
func TestPrintTable_UsesTableWriterWhenImplemented(t *testing.T) {
	var buf bytes.Buffer
	p := New(&buf, FormatTable, false)

	if err := p.Print(listResult{Items: []string{"a", "b"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header + 2 rows), got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "item") {
		t.Errorf("header line missing column name: %q", lines[0])
	}
	if !strings.Contains(lines[1], "a") || !strings.Contains(lines[2], "b") {
		t.Errorf("rows missing expected values: %q", out)
	}
}

// singleRecord has no TableHeader/TableRows methods, so table mode must
// fall back to a generic one-row table built from its exported fields —
// the shape every "info"/"status"-style result type in cmd/database.go
// has (databaseInfoResult, operationStatusResult, etc.).
type singleRecord struct {
	Name      string `json:"name"`
	Count     int    `json:"count"`
	Hidden    string `json:"-"`
	unexpored string //nolint:unused // deliberately unexported, must be skipped
}

// TestPrintTable_GenericFallbackForSingleStruct checks the reflection-based
// one-row fallback for a struct without TableWriter, including json tag handling.
func TestPrintTable_GenericFallbackForSingleStruct(t *testing.T) {
	var buf bytes.Buffer
	p := New(&buf, FormatTable, false)

	if err := p.Print(singleRecord{Name: "snap1", Count: 3, Hidden: "secret"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header + 1 row), got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "name") || !strings.Contains(lines[0], "count") {
		t.Errorf("header missing expected columns: %q", lines[0])
	}
	if strings.Contains(lines[0], "Hidden") || strings.Contains(out, "secret") {
		t.Errorf("json:\"-\" field must be excluded from the table: %q", out)
	}
}

// TestPrintTable_SanitizesTabsAndNewlinesInCells checks that a cell value
// containing a tab or newline (e.g. a snapshot description) is neutralized
// rather than corrupting the table's column/row structure.
func TestPrintTable_SanitizesTabsAndNewlinesInCells(t *testing.T) {
	var buf bytes.Buffer
	p := New(&buf, FormatTable, false)

	if err := p.Print(singleRecord{Name: "line1\tline2\nline3", Count: 1}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header + 1 row), got %d: %q", len(lines), out)
	}
	if strings.Contains(lines[1], "\n") {
		t.Errorf("row must not contain an embedded newline: %q", lines[1])
	}
}

type withNilPointer struct {
	Name string  `json:"name"`
	When *string `json:"when"`
}

// TestPrintTable_NilPointerRendersAsEmptyCell checks that a nil pointer field
// renders as an empty cell rather than the literal string "<nil>".
func TestPrintTable_NilPointerRendersAsEmptyCell(t *testing.T) {
	var buf bytes.Buffer
	p := New(&buf, FormatTable, false)

	if err := p.Print(withNilPointer{Name: "x", When: nil}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "<nil>") {
		t.Errorf("nil pointer field must render as an empty cell, not <nil>: %q", out)
	}
}

// TestPrintTable_QuietSuppressesOutput checks that quiet mode suppresses all
// output in table mode too, not just text/json/yaml.
func TestPrintTable_QuietSuppressesOutput(t *testing.T) {
	var buf bytes.Buffer
	p := New(&buf, FormatTable, true)

	if err := p.Print(listResult{Items: []string{"a"}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("quiet mode must suppress all output, got: %q", buf.String())
	}
}
