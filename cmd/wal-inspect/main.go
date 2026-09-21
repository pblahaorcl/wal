// Command wal-inspect reports the contents and integrity of a WAL file.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"wal"
)

const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiCyan    = "\x1b[36m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiRed     = "\x1b[31m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
)

type palette struct {
	enabled bool
}

func (p palette) paint(code, value string) string {
	if !p.enabled {
		return value
	}
	return code + value + ansiReset
}

func main() {
	colorMode := flag.String("color", "auto", "color output: auto, always, or never")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: wal-inspect PATH\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	useColor, err := colorEnabled(*colorMode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	p := palette{enabled: useColor}
	path := flag.Arg(0)
	report, err := wal.Inspect(path)
	if err != nil && !errors.Is(err, wal.ErrCorrupt) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Println(p.paint(ansiBold+ansiCyan, "WAL INSPECT"))
	fmt.Printf("file: %s\n\n", p.paint(ansiDim, path))
	fmt.Printf("status: %s\n", statusText(p, report))
	fmt.Printf("records: %s\n", p.paint(ansiBold, fmt.Sprintf("%d", report.RecordCount)))
	if report.RecordCount == 0 {
		fmt.Println("sequence range: empty")
	} else {
		fmt.Printf("sequence range: %d..%d\n", report.FirstSequence, report.LastSequence)
	}

	fmt.Println("\nbytes")
	fmt.Printf("  %-8s %d B\n", "file", report.FileBytes)
	fmt.Printf("  %-8s %d B\n", "records", report.RecordBytes)
	printByteBar(p, "headers", report.HeaderBytes, report.FileBytes, ansiBlue)
	printByteBar(p, "payload", report.PayloadBytes, report.FileBytes, ansiMagenta)
	printByteBar(p, "unparsed", report.UnparsedBytes, report.FileBytes, ansiYellow)

	fmt.Println()
	if report.Issue == nil {
		fmt.Printf("issue: %s\n", p.paint(ansiGreen, "none"))
	} else {
		issue := fmt.Sprintf("%s at offset %d: %s", report.Issue.Kind, report.Issue.Offset, report.Issue.Reason)
		color := ansiYellow
		if report.Issue.Kind == wal.InspectionCorruption {
			color = ansiRed
		}
		fmt.Printf("issue: %s\n", p.paint(color, issue))
	}

	if errors.Is(err, wal.ErrCorrupt) {
		os.Exit(1)
	}
}

func colorEnabled(mode string) (bool, error) {
	switch mode {
	case "always":
		return true, nil
	case "never":
		return false, nil
	case "auto":
		info, err := os.Stdout.Stat()
		return err == nil && info.Mode()&os.ModeCharDevice != 0, nil
	default:
		return false, fmt.Errorf("invalid color mode %q: want auto, always, or never", mode)
	}
}

func statusText(p palette, report wal.Inspection) string {
	if report.Issue == nil {
		return p.paint(ansiGreen, "✓ clean")
	}
	if report.Issue.Kind == wal.InspectionCorruption {
		return p.paint(ansiRed, "✗ corruption")
	}
	return p.paint(ansiYellow, "⚠ truncated tail")
}

func printByteBar(p palette, name string, value, total uint64, color string) {
	bar := byteBar(value, total, 24)
	percent := 0.0
	if total != 0 {
		percent = 100 * float64(value) / float64(total)
	}
	fmt.Printf("  %-8s %s %d B (%4.1f%%)\n", name, p.paint(color, bar), value, percent)
}

func byteBar(value, total uint64, width int) string {
	if width <= 0 {
		return ""
	}
	filled := 0
	if total != 0 && value != 0 {
		filled = int(float64(value) / float64(total) * float64(width))
		if filled == 0 {
			filled = 1
		}
		if filled > width {
			filled = width
		}
	}
	return strings.Repeat("█", filled) + strings.Repeat("·", width-filled)
}
