// Package corlog is coroner's logging, carried over from hecato's heclog with
// two deliberate changes noted below.
//
// Every message goes to app.log; whether it also reaches the console is the
// caller's choice, passed as the leading bool. The console and the log file are
// deliberately not the same stream: the console gets colour, app.log gets the
// identical text with no escape codes in it, so the file stays greppable and
// readable in an editor. Anything added here must preserve that split.
//
// Changed from heclog:
//
//   - The log path is resolved with os.Executable() rather than os.Args[0],
//     which is only the executable path when the caller supplies one. Invoked
//     by bare name via PATH, os.Args[0] resolves to the working directory.
//
//   - "development" writes to the working directory rather than a hardcoded
//     repo path. This is a public repo; a path baked in for one machine is
//     worse than useless to anyone who clones it.
package corlog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
)

// LogSetup opens app.log and points the log package at it. The returned file is
// nil when logging could not be set up, which is not fatal: a tool that refuses
// to run because it cannot write a log file beside itself is a tool that cannot
// be installed under Program Files.
func LogSetup(buildContext string) (*os.File, error) {
	dir := "."
	if buildContext != "development" {
		exe, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("locating the executable: %w", err)
		}
		dir = filepath.Dir(exe)
	}

	logFile, err := os.OpenFile(filepath.Join(dir, "app.log"), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening log file: %w", err)
	}

	// No Lshortfile: every message funnels through write() below, so the file
	// and line would be the same constant on every line.
	log.SetOutput(logFile)
	log.SetFlags(log.Ldate | log.Ltime)
	log.Println("coroner starting...")

	return logFile, nil
}

// Discard sends the log stream nowhere, for when app.log could not be opened
// and for tests. The console half of every helper still works.
func Discard() {
	log.SetOutput(io.Discard)
}

// DisableColor turns off colour unconditionally, for -no-color.
//
// It does not need to be called for a pipe or for NO_COLOR: the color package
// already disables itself when stdout is not a terminal and when NO_COLOR is
// set in the environment.
func DisableColor() {
	color.NoColor = true
}

// ColorEnabled reports whether console output will actually be coloured, after
// TTY detection, NO_COLOR and -no-color have all had their say.
func ColorEnabled() bool {
	return !color.NoColor
}

// write is the single path through which every coloured message goes. The log
// file always receives the plain string; only the console sees the escape codes.
func write(c *color.Color, logToConsole bool, format string, args ...interface{}) {
	plain := fmt.Sprintf(format, args...)

	log.Print(plain)

	if !logToConsole {
		return
	}

	// color.Output is the wrapped stdout that makes escape codes work on older
	// Windows consoles. Writing to it rather than os.Stdout is what keeps this
	// working outside Windows Terminal.
	if c == nil {
		fmt.Fprintln(color.Output, plain)
		return
	}
	c.Fprintln(color.Output, plain)
}

var (
	headingColor = color.New(color.Bold)
	labelColor   = color.New(color.FgCyan)
	successColor = color.New(color.FgGreen)
	warnColor    = color.New(color.FgYellow)
	errorColor   = color.New(color.FgRed, color.Bold)
	detailColor  = color.New(color.Faint)
)

// Heading is a section title.
func Heading(logToConsole bool, format string, args ...interface{}) {
	write(headingColor, logToConsole, format, args...)
}

// Info is ordinary output with no colour of its own.
func Info(logToConsole bool, format string, args ...interface{}) {
	write(nil, logToConsole, format, args...)
}

// Detail is secondary text: dimmed, for things worth showing but not reading.
func Detail(logToConsole bool, format string, args ...interface{}) {
	write(detailColor, logToConsole, format, args...)
}

// Success marks a completed run.
func Success(logToConsole bool, format string, args ...interface{}) {
	write(successColor, logToConsole, format, args...)
}

// Warn is for things that did not stop the run but that you should know about.
func Warn(logToConsole bool, format string, args ...interface{}) {
	write(warnColor, logToConsole, format, args...)
}

// Error is for the run failing. Callers decide whether to exit.
func Error(logToConsole bool, format string, args ...interface{}) {
	write(errorColor, logToConsole, format, args...)
}

// Style names the colours a Segment can take. Named rather than exposing
// *color.Color so the rest of the program does not have to import the colour
// library to print a line.
type Style int

const (
	StylePlain Style = iota
	StyleDim
	StyleScore
	StyleBar
	StyleWarn
	StyleTitle
)

func (s Style) color() *color.Color {
	switch s {
	case StyleDim:
		return detailColor
	case StyleScore:
		return scoreColor
	case StyleBar:
		return barColor
	case StyleWarn:
		return warnColor
	case StyleTitle:
		return titleColor
	default:
		return nil
	}
}

var (
	scoreColor = color.New(color.FgHiYellow)
	barColor   = color.New(color.FgBlue)
	titleColor = color.New(color.Bold)
)

// Segment is one styled run of text within a Row.
type Segment struct {
	Text  string
	Style Style
}

// Seg builds a Segment, formatting like Printf.
func Seg(style Style, format string, args ...interface{}) Segment {
	return Segment{Text: fmt.Sprintf(format, args...), Style: style}
}

// Row prints segments as a single line, each in its own colour, while the log
// file receives the plain concatenation.
//
// The invariant to preserve: what reaches the log is exactly the concatenated
// Text fields, no escape codes.
func Row(logToConsole bool, segs ...Segment) {
	var plain strings.Builder
	for _, s := range segs {
		plain.WriteString(s.Text)
	}

	log.Print(plain.String())

	if !logToConsole {
		return
	}

	for _, s := range segs {
		if c := s.Style.color(); c != nil {
			c.Fprint(color.Output, s.Text)
			continue
		}
		fmt.Fprint(color.Output, s.Text)
	}
	fmt.Fprintln(color.Output)
}

// Field prints an aligned "label  value" pair, used by the intent summary. The
// label is coloured and padded; the value is left plain so paths stay easy to
// copy out of a terminal.
func Field(logToConsole bool, label, value string) {
	plain := fmt.Sprintf("  %-12s %s", label, value)

	log.Print(plain)

	if !logToConsole {
		return
	}
	fmt.Fprintf(color.Output, "  %s %s\n", labelColor.Sprintf("%-12s", label), value)
}
