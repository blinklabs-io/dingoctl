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
// Supported --output formats: text, json, yaml, table.
// Color is suppressed when NO_COLOR is set or stdout is not a TTY.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"text/tabwriter"

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

// TableWriter is implemented by result types that have a natural
// multi-row shape (a list of things, one row per element) — snapshot
// listings, operation histories, and the like. Print's table mode uses
// this when available; a result type that doesn't implement it (a single
// record, not a list) instead gets a generic one-row table built via
// reflection over its exported fields (see printTable).
type TableWriter interface {
	// TableHeader returns the column names, in display order.
	TableHeader() []string
	// TableRows returns one []string per row, each the same length as
	// TableHeader.
	TableRows() [][]string
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
		if err := enc.Encode(v); err != nil {
			return err
		}
		return enc.Close()
	case FormatTable:
		return printTable(p.w, v)
	default: // text
		_, err := fmt.Fprintln(p.w, v)
		return err
	}
}

// TableFooter is an optional TableWriter extension for a result type that
// carries one extra line of metadata not naturally a table column or row —
// e.g. a pagination token. printTable renders it as a plain line after the
// table body rather than a column, so it isn't tabwriter-aligned as if it
// were another record. Return "" to render nothing.
type TableFooter interface {
	TableFooter() string
}

// printTable renders v as a tab-aligned table. If v implements TableWriter
// its header/rows are used directly (one row per list element); otherwise
// v is treated as a single record and rendered as a one-row table whose
// columns are v's exported struct fields (name from its `json` tag, or the
// Go field name if untagged). If v also implements TableFooter, that line
// is appended after the table.
func printTable(w io.Writer, v any) error {
	var header []string
	var rows [][]string
	if tw, ok := v.(TableWriter); ok {
		header = tw.TableHeader()
		rows = tw.TableRows()
	} else {
		header, rows = genericSingleRowTable(v)
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, strings.Join(header, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		safeRow := make([]string, len(row))
		for i, cell := range row {
			safeRow[i] = tableCellSanitizer.Replace(cell)
		}
		if _, err := fmt.Fprintln(tw, strings.Join(safeRow, "\t")); err != nil {
			return err
		}
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if tf, ok := v.(TableFooter); ok {
		if footer := tf.TableFooter(); footer != "" {
			if _, err := fmt.Fprintln(w, footer); err != nil {
				return err
			}
		}
	}
	return nil
}

// tableCellSanitizer strips characters a cell value could contain (e.g. a
// snapshot description or operation message) that would otherwise be
// misread as tabwriter column delimiters or row breaks, corrupting the
// one-record-per-row table structure.
var tableCellSanitizer = strings.NewReplacer("\t", " ", "\r", " ", "\n", " ")

// genericSingleRowTable reflects over v's exported fields to build a
// one-row table: header from each field's `json` tag name (falling back to
// the Go field name), values formatted with tableCellValue. v must be a
// struct or a pointer to one.
func genericSingleRowTable(v any) ([]string, [][]string) {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, nil
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return []string{"value"}, [][]string{{fmt.Sprintf("%v", v)}}
	}

	rt := rv.Type()
	header := make([]string, 0, rt.NumField())
	row := make([]string, 0, rt.NumField())
	for i := range rt.NumField() {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}
		name := field.Name
		if tag, ok := field.Tag.Lookup("json"); ok {
			tagName, _, _ := strings.Cut(tag, ",")
			if tagName == "-" {
				continue
			}
			if tagName != "" {
				name = tagName
			}
		}
		header = append(header, name)
		row = append(row, tableCellValue(rv.Field(i)))
	}
	return header, [][]string{row}
}

// tableCellValue formats a single field for display: a nil pointer
// renders as an empty cell rather than "<nil>", and everything else uses
// its normal %v formatting (which already calls String() for types like
// blockRefResult/operationRecordResult that implement fmt.Stringer).
func tableCellValue(v reflect.Value) string {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	return fmt.Sprintf("%v", v.Interface())
}

// Println writes a plain text line to w, respecting quiet mode.
func (p *Printer) Println(msg string) {
	if p.quiet {
		return
	}
	_, _ = fmt.Fprintln(p.w, msg)
}

// ColorEnabled returns whether this printer will use ANSI colors.
func (p *Printer) ColorEnabled() bool {
	return p.color
}
