# Output Package

The `output` package provides consistent formatting for dingoctl commands with support for multiple output formats, styled text, tables, and progress indicators.

## Features

- **Multiple Output Formats**: text, json, yaml, table
- **Styled Text Output**: Success, error, warning, info messages with color
- **Table Rendering**: Formatted tables for list commands
- **Progress Indicators**: Progress bars and spinners for long operations
- **Quiet Mode**: Suppress all non-essential output
- **Color Awareness**: Respects NO_COLOR env var and TTY detection

## Usage

### Basic Printer

```go
import "github.com/blinklabs-io/dingoctl/internal/output"

// Create a printer (typically from cmd.GetOutputPrinter())
printer := output.New(os.Stdout, output.FormatText, false)

// Print structured data
printer.Print(data)

// Print plain text
printer.Println("Hello")
```

### Styled Text

```go
printer := output.New(os.Stdout, output.FormatText, false)

printer.Success("Operation completed")
printer.Error("Connection failed")
printer.Warning("Low disk space")
printer.Info("Processing 100 items")
printer.Header("System Status")
printer.KeyValue("Node", "dingo-01")
```

### Tables

```go
printer := output.New(os.Stdout, output.FormatText, false)

table := &output.Table{
    Headers: []string{"Name", "Status", "Uptime"},
    Rows: [][]string{
        {"node-1", "Active", "5 days"},
        {"node-2", "Active", "3 days"},
    },
}

// Styled table with borders
printer.Table().Render(table)

// Simple table without borders (compact)
printer.Table().RenderSimple(table)
```

### Progress Bars

```go
printer := output.New(os.Stdout, output.FormatText, false)

pb := printer.NewProgressBar(50) // width in characters
if pb != nil {
    for i := 0; i <= 100; i++ {
        pb.Update(float64(i)/100, "Processing...")
        time.Sleep(50 * time.Millisecond)
    }
    pb.Complete("Done!")
}
```

### Spinners

```go
printer := output.New(os.Stdout, output.FormatText, false)

spinner := printer.NewSpinner()
if spinner != nil {
    spinner.Start("Loading...")
    // ... do work ...
    spinner.Update("Still loading...")
    // ... more work ...
    spinner.Stop()
}

printer.Success("Complete")
```

## Output Formats

### Text Format

Human-readable output with colors and styling (when supported):
- Styled messages (success, error, warning, info)
- Headers and sections
- Key-value pairs
- Tables
- Progress bars

### JSON Format

Machine-readable JSON output:
```json
{
  "status": "ok",
  "count": 42
}
```

### YAML Format

Machine-readable YAML output:
```yaml
status: ok
count: 42
```

### Table Format

Table-formatted output without additional decoration. Useful for list commands.

## Quiet Mode

When quiet mode is enabled:
- All output is suppressed (except explicit errors to stderr)
- Progress bars return nil
- Spinners return nil
- All printer methods become no-ops

```go
printer := output.New(os.Stdout, output.FormatText, true) // quiet=true
printer.Success("This won't print")
```

## Color Control

Colors are automatically disabled when:
- `NO_COLOR` environment variable is set (any value)
- Output is not a TTY (e.g., piped or redirected)

Colors are always disabled for JSON and YAML formats.

## Integration with Commands

Commands should use the global printer from `cmd.GetOutputPrinter()`:

```go
import (
    "github.com/spf13/cobra"
    cmdpkg "github.com/blinklabs-io/dingoctl/cmd"
)

func runMyCommand(cmd *cobra.Command, args []string) error {
    printer := cmdpkg.GetOutputPrinter()
    
    // For structured data output
    if err := printer.Print(result); err != nil {
        return err
    }
    
    // For text-only messages
    printer.Success("Operation completed")
    
    // For tables
    if table := printer.Table(); table != nil {
        table.Render(myTableData)
    }
    
    return nil
}
```

## Best Practices

1. **Use structured output for data**: When outputting data, use `printer.Print()` to support all formats
2. **Use styled text for messages**: Success/error/warning/info for operator feedback
3. **Check for nil**: Progress bars and spinners return nil in quiet mode
4. **Respect format**: Only use text styling (Success, Header, etc.) for text format
5. **Tables for lists**: Use table renderer for any list/collection output
6. **Progress for long ops**: Show progress for operations > 2 seconds

## Testing

The package includes comprehensive tests covering:
- Format validation
- Color detection
- Output rendering (JSON, YAML, text)
- Quiet mode behavior
- Table rendering
- Progress indicators

Run tests with:
```bash
go test -v ./internal/output/
```

## Example Commands

### Showing Node Status

```go
type Status struct {
    NodeID  string `json:"node_id"`
    Synced  bool   `json:"synced"`
    Block   uint64 `json:"block"`
}

status := getNodeStatus()

// JSON/YAML: structured output
if err := printer.Print(status); err != nil {
    return err
}

// Text: styled output
if printer.Format() == output.FormatText {
    printer.Header("Node Status")
    printer.KeyValue("Node ID", status.NodeID)
    printer.KeyValue("Block", fmt.Sprintf("%d", status.Block))
    if status.Synced {
        printer.Success("Synced")
    } else {
        printer.Warning("Syncing")
    }
}
```

### Showing Pool List

```go
pools := getPoolList()

// JSON/YAML: array of objects
if printer.Format() != output.FormatText {
    return printer.Print(pools)
}

// Text/Table: formatted table
table := &output.Table{
    Headers: []string{"Pool ID", "Pledge", "Margin"},
    Rows: make([][]string, len(pools)),
}
for i, p := range pools {
    table.Rows[i] = []string{p.ID, p.Pledge, p.Margin}
}

return printer.Table().Render(table)
```

### Long-Running Operation

```go
pb := printer.NewProgressBar(50)
defer func() {
    if pb != nil {
        pb.Clear()
    }
}()

for i, item := range items {
    processItem(item)
    if pb != nil {
        progress := float64(i+1) / float64(len(items))
        pb.Update(progress, fmt.Sprintf("Processed %d/%d", i+1, len(items)))
    }
}

if pb != nil {
    pb.Complete("All items processed")
}
printer.Success("Operation complete")
```
