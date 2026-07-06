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
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
)

// ProgressBar represents a progress indicator for long-running operations.
type ProgressBar struct {
	w          io.Writer
	color      bool
	width      int
	done       bool
	mu         sync.Mutex
	lastRender time.Time
	message    string
	model      progress.Model
}

// NewProgressBar creates a progress bar that writes to w.
// width specifies the character width of the progress bar (default 40).
func NewProgressBar(w io.Writer, color bool, width int) *ProgressBar {
	if width <= 0 {
		width = 40
	}

	var model progress.Model
	if color {
		model = progress.New(
			progress.WithWidth(width),
			progress.WithDefaultGradient(),
		)
	} else {
		// Plain progress bar for no-color mode
		model = progress.New(
			progress.WithWidth(width),
			progress.WithoutPercentage(),
		)
	}

	return &ProgressBar{
		w:     w,
		color: color,
		width: width,
		model: model,
	}
}

// Update sets the progress to p (0.0 to 1.0) and updates the message.
// Progress is rendered immediately if enough time has passed since the last render.
func (pb *ProgressBar) Update(p float64, msg string) error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	if pb.done {
		return nil
	}

	pb.message = msg

	// Rate limit updates to avoid terminal flicker
	now := time.Now()
	if now.Sub(pb.lastRender) < 50*time.Millisecond {
		return nil
	}
	pb.lastRender = now

	return pb.render(p)
}

// Complete marks the progress bar as done and renders a final completion message.
func (pb *ProgressBar) Complete(msg string) error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	if pb.done {
		return nil
	}

	pb.done = true
	pb.message = msg

	return pb.render(1.0)
}

// render writes the current progress state to the writer.
// Must be called with pb.mu held.
func (pb *ProgressBar) render(p float64) error {
	// Clear the current line
	fmt.Fprintf(pb.w, "\r%s\r", strings.Repeat(" ", pb.width+50))

	if pb.color {
		// Styled progress bar
		bar := pb.model.ViewAs(p)
		_, err := fmt.Fprintf(pb.w, "\r%s %s", bar, pb.message)
		return err
	}

	// Plain text progress bar
	filled := int(p * float64(pb.width))
	empty := pb.width - filled
	bar := "[" + strings.Repeat("=", filled) + strings.Repeat(" ", empty) + "]"
	pct := int(p * 100)
	_, err := fmt.Fprintf(pb.w, "\r%s %3d%% %s", bar, pct, pb.message)
	return err
}

// Clear removes the progress bar from the terminal.
func (pb *ProgressBar) Clear() error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	fmt.Fprintf(pb.w, "\r%s\r", strings.Repeat(" ", pb.width+50))
	return nil
}

// Spinner represents a simple spinner for indeterminate operations.
type Spinner struct {
	w       io.Writer
	color   bool
	frames  []string
	current int
	done    bool
	mu      sync.Mutex
	message string
	ticker  *time.Ticker
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewSpinner creates a spinner that writes to w.
func NewSpinner(w io.Writer, color bool) *Spinner {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	if !color {
		frames = []string{"|", "/", "-", "\\"}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Spinner{
		w:      w,
		color:  color,
		frames: frames,
		ctx:    ctx,
		cancel: cancel,
	}
}

// Start begins the spinner animation in a background goroutine.
func (s *Spinner) Start(msg string) {
	s.mu.Lock()
	s.message = msg
	s.done = false
	s.mu.Unlock()

	s.ticker = time.NewTicker(100 * time.Millisecond)
	go s.run()
}

// Update changes the spinner message without stopping it.
func (s *Spinner) Update(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.message = msg
}

// Stop halts the spinner and clears the line.
func (s *Spinner) Stop() {
	s.mu.Lock()
	s.done = true

	if s.cancel != nil {
		s.cancel()
	}
	if s.ticker != nil {
		s.ticker.Stop()
	}

	// Clear the spinner line (hold lock to prevent race with render loop)
	fmt.Fprintf(s.w, "\r%s\r", strings.Repeat(" ", 80))
	s.mu.Unlock()
}

// run is the spinner animation loop.
func (s *Spinner) run() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.ticker.C:
			s.mu.Lock()
			if s.done {
				s.mu.Unlock()
				return
			}

			frame := s.frames[s.current]
			s.current = (s.current + 1) % len(s.frames)

			// Clear and render
			fmt.Fprintf(s.w, "\r%s\r", strings.Repeat(" ", 80))

			if s.color {
				styled := lipgloss.NewStyle().
					Foreground(lipgloss.Color("12")).
					Render(frame)
				fmt.Fprintf(s.w, "\r%s %s", styled, s.message)
			} else {
				fmt.Fprintf(s.w, "\r%s %s", frame, s.message)
			}

			s.mu.Unlock()
		}
	}
}
