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

	"github.com/charmbracelet/lipgloss"
)

// TextRenderer renders structured data as styled text using lipgloss.
type TextRenderer struct {
	w     io.Writer
	color bool
}

// NewTextRenderer creates a text renderer that writes to w.
// If color is false, all styling is stripped.
func NewTextRenderer(w io.Writer, color bool) *TextRenderer {
	return &TextRenderer{w: w, color: color}
}

// Style definitions for consistent theming.
var (
	// SuccessStyle for success messages.
	SuccessStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")).
			Bold(true)

	// ErrorStyle for error messages.
	ErrorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("9")).
			Bold(true)

	// WarningStyle for warnings.
	WarningStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("11")).
			Bold(true)

	// InfoStyle for informational messages.
	InfoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("12"))

	// HeaderStyle for section headers.
	HeaderStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("13")).
			Bold(true).
			Underline(true)

	// KeyStyle for key-value pair keys.
	KeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Bold(true)

	// ValueStyle for key-value pair values.
	ValueStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("15"))

	// DimStyle for secondary information.
	DimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("8"))
)

// Success renders a success message.
func (r *TextRenderer) Success(msg string) error {
	return r.render(SuccessStyle, "✓ "+msg)
}

// Error renders an error message.
func (r *TextRenderer) Error(msg string) error {
	return r.render(ErrorStyle, "✗ "+msg)
}

// Warning renders a warning message.
func (r *TextRenderer) Warning(msg string) error {
	return r.render(WarningStyle, "⚠ "+msg)
}

// Info renders an informational message.
func (r *TextRenderer) Info(msg string) error {
	return r.render(InfoStyle, msg)
}

// Header renders a section header.
func (r *TextRenderer) Header(text string) error {
	return r.render(HeaderStyle, text)
}

// KeyValue renders a key-value pair.
func (r *TextRenderer) KeyValue(key, value string) error {
	if !r.color {
		_, err := fmt.Fprintf(r.w, "%s: %s\n", key, value)
		return err
	}
	keyStr := KeyStyle.Render(key + ":")
	valStr := ValueStyle.Render(value)
	_, err := fmt.Fprintf(r.w, "%s %s\n", keyStr, valStr)
	return err
}

// Dim renders dimmed/secondary text.
func (r *TextRenderer) Dim(msg string) error {
	return r.render(DimStyle, msg)
}

// Plain renders plain unstyled text.
func (r *TextRenderer) Plain(msg string) error {
	_, err := fmt.Fprintln(r.w, msg)
	return err
}

// render applies the style and writes to w.
// If color is disabled, strips all styling.
func (r *TextRenderer) render(style lipgloss.Style, text string) error {
	if !r.color {
		_, err := fmt.Fprintln(r.w, text)
		return err
	}
	_, err := fmt.Fprintln(r.w, style.Render(text))
	return err
}
