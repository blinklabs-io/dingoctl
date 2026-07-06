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

// Package output handles result formatting and color policy for dingoctl.
//
// Supported --output formats: text, json, yaml.
// Color is suppressed when NO_COLOR is set or stdout is not a TTY.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/mattn/go-isatty"
	"go.yaml.in/yaml/v3"
)

// Format represents an output format requested by the operator.
type Format string

const (
	FormatText  Format = "text"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
	FormatTable Format = "table"
)

// IsValid reports whether f is a recognised output format.
func (f Format) IsValid() bool {
	switch f {
	case FormatText, FormatJSON, FormatYAML, FormatTable:
		return true
	default:
		return false
	}
}

// ColorEnabled reports whether ANSI color should be used on w.
// It returns false when the NO_COLOR environment variable is set (any value)
// or when w is not a TTY.
func ColorEnabled(w io.Writer) bool {
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return false
	}
	if f, ok := w.(*os.File); ok {
		return isatty.IsTerminal(f.Fd()) || isatty.IsCygwinTerminal(f.Fd())
	}
	return false
}

// Printer writes structured data to an output stream in the requested format.
type Printer struct {
	w      io.Writer
	format Format
	quiet  bool
	color  bool
	text   *TextRenderer
	table  *TableRenderer
}

// New creates a Printer that writes to w.
func New(w io.Writer, format Format, quiet bool) *Printer {
	color := ColorEnabled(w)
	return &Printer{
		w:      w,
		format: format,
		quiet:  quiet,
		color:  color,
		text:   NewTextRenderer(w, color),
		table:  NewTableRenderer(w, color),
	}
}

// Print encodes v according to the printer's format and writes it to w.
// In quiet mode nothing is written.
func (p *Printer) Print(v any) error {
	if p.quiet {
		return nil
	}
	switch p.format {
	case FormatJSON:
		enc := json.NewEncoder(p.w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case FormatYAML:
		enc := yaml.NewEncoder(p.w)
		enc.SetIndent(2)
		if err := enc.Encode(v); err != nil {
			return err
		}
		return enc.Close()
	default: // text
		_, err := fmt.Fprintln(p.w, v)
		return err
	}
}

// Println writes a plain text line to w, respecting quiet mode.
func (p *Printer) Println(msg string) {
	if p.quiet {
		return
	}
	fmt.Fprintln(p.w, msg)
}

// ColorEnabled returns whether this printer will use ANSI colors.
func (p *Printer) ColorEnabled() bool {
	return p.color
}

// Text returns the text renderer for styled output.
// Only applies to text format; returns nil for JSON/YAML.
func (p *Printer) Text() *TextRenderer {
	if p.format != FormatText {
		return nil
	}
	return p.text
}

// Table returns the table renderer for tabular output.
// Only applies to text format; returns nil for JSON/YAML.
func (p *Printer) Table() *TableRenderer {
	if p.format != FormatText {
		return nil
	}
	return p.table
}

// NewProgressBar creates a progress bar for this printer.
// Returns nil if quiet mode is enabled.
func (p *Printer) NewProgressBar(width int) *ProgressBar {
	if p.quiet {
		return nil
	}
	return NewProgressBar(p.w, p.color, width)
}

// NewSpinner creates a spinner for this printer.
// Returns nil if quiet mode is enabled.
func (p *Printer) NewSpinner() *Spinner {
	if p.quiet {
		return nil
	}
	return NewSpinner(p.w, p.color)
}

// Success writes a success message (text format only).
func (p *Printer) Success(msg string) error {
	if p.quiet || p.format != FormatText {
		return nil
	}
	return p.text.Success(msg)
}

// Error writes an error message (text format only).
func (p *Printer) Error(msg string) error {
	if p.quiet || p.format != FormatText {
		return nil
	}
	return p.text.Error(msg)
}

// Warning writes a warning message (text format only).
func (p *Printer) Warning(msg string) error {
	if p.quiet || p.format != FormatText {
		return nil
	}
	return p.text.Warning(msg)
}

// Info writes an informational message (text format only).
func (p *Printer) Info(msg string) error {
	if p.quiet || p.format != FormatText {
		return nil
	}
	return p.text.Info(msg)
}

// Header writes a section header (text format only).
func (p *Printer) Header(text string) error {
	if p.quiet || p.format != FormatText {
		return nil
	}
	return p.text.Header(text)
}

// KeyValue writes a key-value pair (text format only).
func (p *Printer) KeyValue(key, value string) error {
	if p.quiet || p.format != FormatText {
		return nil
	}
	return p.text.KeyValue(key, value)
}
