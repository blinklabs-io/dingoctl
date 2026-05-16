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
	FormatText Format = "text"
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

// IsValid reports whether f is a recognised output format.
func (f Format) IsValid() bool {
	switch f {
	case FormatText, FormatJSON, FormatYAML:
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
}

// New creates a Printer that writes to w.
func New(w io.Writer, format Format, quiet bool) *Printer {
	return &Printer{
		w:      w,
		format: format,
		quiet:  quiet,
		color:  ColorEnabled(w),
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
		return enc.Encode(v)
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
