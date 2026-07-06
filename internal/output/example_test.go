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

package output_test

import (
	"os"
	"time"

	"github.com/blinklabs-io/dingoctl/internal/output"
)

// Example_basicPrinter demonstrates basic printer usage with different formats.
func Example_basicPrinter() {
	// Create a printer for JSON output
	jsonPrinter := output.New(os.Stdout, output.FormatJSON, false)
	data := map[string]any{"status": "ok", "count": 42}
	jsonPrinter.Print(data)

	// Create a printer for YAML output
	yamlPrinter := output.New(os.Stdout, output.FormatYAML, false)
	yamlPrinter.Print(data)

	// Create a printer for text output
	textPrinter := output.New(os.Stdout, output.FormatText, false)
	textPrinter.Success("Operation completed successfully")
	textPrinter.Info("Processing 100 items")
	textPrinter.Warning("Low disk space")
	textPrinter.Error("Failed to connect")
}

// Example_styledText demonstrates styled text output.
func Example_styledText() {
	printer := output.New(os.Stdout, output.FormatText, false)

	// Headers and sections
	printer.Header("System Status")
	printer.KeyValue("Node", "dingo-01")
	printer.KeyValue("Status", "Running")
	printer.KeyValue("Uptime", "5 days")

	// Messages
	printer.Success("All systems operational")
	printer.Info("Current block: 1234567")
	printer.Warning("Memory usage at 80%")
}

// Example_table demonstrates table rendering.
func Example_table() {
	printer := output.New(os.Stdout, output.FormatText, false)

	table := &output.Table{
		Headers: []string{"Pool ID", "Pledge", "Margin", "Status"},
		Rows: [][]string{
			{"pool1abc...", "100K ADA", "2%", "Active"},
			{"pool2def...", "500K ADA", "1.5%", "Active"},
			{"pool3ghi...", "250K ADA", "3%", "Retiring"},
		},
	}

	// Render styled table with borders
	printer.Table().Render(table)

	// Or render simple table without borders
	printer.Table().RenderSimple(table)
}

// Example_progressBar demonstrates progress bar usage.
func Example_progressBar() {
	printer := output.New(os.Stdout, output.FormatText, false)

	// Create a progress bar
	pb := printer.NewProgressBar(50)
	if pb == nil {
		// Quiet mode is enabled, or format doesn't support progress bars
		return
	}

	// Simulate a long-running operation
	for i := 0; i <= 100; i++ {
		pb.Update(float64(i)/100, "Processing blocks...")
		time.Sleep(50 * time.Millisecond)
	}

	pb.Complete("Done processing 100 blocks")
}

// Example_spinner demonstrates spinner usage for indeterminate operations.
func Example_spinner() {
	printer := output.New(os.Stdout, output.FormatText, false)

	spinner := printer.NewSpinner()
	if spinner == nil {
		return
	}

	spinner.Start("Connecting to node...")
	time.Sleep(2 * time.Second)

	spinner.Update("Authenticating...")
	time.Sleep(1 * time.Second)

	spinner.Update("Loading blockchain state...")
	time.Sleep(2 * time.Second)

	spinner.Stop()

	printer.Success("Connected successfully")
}

// Example_quietMode demonstrates quiet mode suppressing all output.
func Example_quietMode() {
	printer := output.New(os.Stdout, output.FormatText, true)

	// All of these will be suppressed
	printer.Success("This won't be printed")
	printer.Error("Neither will this")
	printer.Print(map[string]string{"data": "suppressed"})

	// Progress bars and spinners return nil in quiet mode
	pb := printer.NewProgressBar(50)
	if pb == nil {
		// Expected in quiet mode
	}
}

// Example_structuredOutput demonstrates outputting structured data.
func Example_structuredOutput() {
	type NodeInfo struct {
		NodeID   string `json:"node_id" yaml:"node_id"`
		Version  string `json:"version" yaml:"version"`
		Network  string `json:"network" yaml:"network"`
		Synced   bool   `json:"synced" yaml:"synced"`
		BlockNum uint64 `json:"block_num" yaml:"block_num"`
	}

	info := NodeInfo{
		NodeID:   "dingo-01",
		Version:  "1.0.0",
		Network:  "mainnet",
		Synced:   true,
		BlockNum: 1234567,
	}

	// JSON format
	jsonPrinter := output.New(os.Stdout, output.FormatJSON, false)
	jsonPrinter.Print(info)

	// YAML format
	yamlPrinter := output.New(os.Stdout, output.FormatYAML, false)
	yamlPrinter.Print(info)

	// Text format with custom rendering
	textPrinter := output.New(os.Stdout, output.FormatText, false)
	textPrinter.Header("Node Information")
	textPrinter.KeyValue("Node ID", info.NodeID)
	textPrinter.KeyValue("Version", info.Version)
	textPrinter.KeyValue("Network", info.Network)
	if info.Synced {
		textPrinter.Success("Node is synced")
	} else {
		textPrinter.Warning("Node is syncing")
	}
	textPrinter.KeyValue("Current Block", string(rune(info.BlockNum)))
}
