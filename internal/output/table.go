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
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// TableRenderer renders data as formatted tables.
type TableRenderer struct {
	w     io.Writer
	color bool
}

// NewTableRenderer creates a table renderer that writes to w.
func NewTableRenderer(w io.Writer, color bool) *TableRenderer {
	return &TableRenderer{w: w, color: color}
}

// Table represents a data table with headers and rows.
type Table struct {
	Headers []string
	Rows    [][]string
}

// Render writes the table to the renderer's writer.
func (r *TableRenderer) Render(t *Table) error {
	if len(t.Headers) == 0 || len(t.Rows) == 0 {
		return nil
	}

	// Build the table using lipgloss/table
	tb := table.New()

	if r.color {
		// Styled table with colors
		headerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Bold(true).
			Padding(0, 1)

		cellStyle := lipgloss.NewStyle().
			Padding(0, 1)

		evenRowStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("15"))

		oddRowStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("7"))

		tb = tb.
			Border(lipgloss.NormalBorder()).
			BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8"))).
			StyleFunc(func(row, col int) lipgloss.Style {
				if row == 0 {
					return headerStyle
				}
				if row%2 == 0 {
					return cellStyle.Inherit(evenRowStyle)
				}
				return cellStyle.Inherit(oddRowStyle)
			})
	} else {
		// Plain table without colors
		tb = tb.Border(lipgloss.NormalBorder())
	}

	// Set headers
	tb = tb.Headers(t.Headers...)

	// Set rows
	tb = tb.Rows(t.Rows...)

	_, err := fmt.Fprintln(r.w, tb.Render())
	return err
}

// RenderSimple renders a simple table without borders.
// Useful for compact output.
func (r *TableRenderer) RenderSimple(t *Table) error {
	if len(t.Headers) == 0 || len(t.Rows) == 0 {
		return nil
	}

	// Calculate column widths
	widths := make([]int, len(t.Headers))
	for i, h := range t.Headers {
		widths[i] = len(h)
	}
	for _, row := range t.Rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	// Render header
	headerParts := make([]string, len(t.Headers))
	for i, h := range t.Headers {
		if r.color {
			styled := lipgloss.NewStyle().
				Foreground(lipgloss.Color("14")).
				Bold(true).
				Render(h)
			headerParts[i] = styled + strings.Repeat(" ", widths[i]-len(h))
		} else {
			headerParts[i] = h + strings.Repeat(" ", widths[i]-len(h))
		}
	}
	fmt.Fprintln(r.w, strings.Join(headerParts, "  "))

	// Render separator
	if !r.color {
		sepParts := make([]string, len(widths))
		for i, w := range widths {
			sepParts[i] = strings.Repeat("-", w)
		}
		fmt.Fprintln(r.w, strings.Join(sepParts, "  "))
	}

	// Render rows
	for _, row := range t.Rows {
		rowParts := make([]string, len(row))
		for i, cell := range row {
			if i < len(widths) {
				rowParts[i] = cell + strings.Repeat(" ", widths[i]-len(cell))
			} else {
				rowParts[i] = cell
			}
		}
		fmt.Fprintln(r.w, strings.Join(rowParts, "  "))
	}

	return nil
}
