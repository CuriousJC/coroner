package corlog

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/fatih/color"
)

// capture redirects the log stream and forces colour on, so that the two halves
// of every helper can be compared. Colour is normally off in tests because
// stdout is not a terminal, which would make this test pass trivially.
func capture(t *testing.T, f func()) string {
	t.Helper()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetFlags(0)

	was := color.NoColor
	color.NoColor = false
	defer func() {
		color.NoColor = was
		log.SetOutput(bytes.NewBuffer(nil))
	}()

	f()
	return buf.String()
}

// TestLogFileNeverGetsColorCodes is the invariant the whole package is shaped
// around. If this fails, app.log has become ungreppable: every line would carry
// escape sequences that a regular expression has to be taught to skip.
func TestLogFileNeverGetsColorCodes(t *testing.T) {
	got := capture(t, func() {
		Heading(false, "a heading")
		Info(false, "some info")
		Detail(false, "a detail")
		Success(false, "success")
		Warn(false, "a warning")
		Error(false, "an error")
		Field(false, "Label", "value")
		Row(false,
			Seg(StyleScore, "score "),
			Seg(StyleDim, "dim "),
			Seg(StyleBar, "bar "),
			Seg(StylePlain, "plain"),
		)
	})

	if strings.Contains(got, "\x1b[") {
		t.Errorf("an escape sequence reached the log file:\n%q", got)
	}

	// The text itself must still be there, or the invariant is being satisfied
	// by writing nothing.
	for _, want := range []string{"a heading", "some info", "an error", "Label", "value", "plain"} {
		if !strings.Contains(got, want) {
			t.Errorf("the log is missing %q:\n%s", want, got)
		}
	}
}

// TestRowLogsTheConcatenation pins the other half of Row's contract: what
// reaches the log is exactly the segments joined, with nothing inserted between
// them.
func TestRowLogsTheConcatenation(t *testing.T) {
	got := capture(t, func() {
		Row(false,
			Seg(StylePlain, "  %3d. ", 7),
			Seg(StyleScore, "%s", "score"),
			Seg(StyleDim, " %s", "tail"),
		)
	})

	want := "    7. score tail"
	if strings.TrimRight(got, "\n") != want {
		t.Errorf("Row logged %q, want %q", strings.TrimRight(got, "\n"), want)
	}
}

func TestFormattingIsApplied(t *testing.T) {
	got := capture(t, func() {
		Info(false, "%s has %d items", "list", 3)
	})

	if !strings.Contains(got, "list has 3 items") {
		t.Errorf("format arguments were not applied: %q", got)
	}
}

func TestDisableColor(t *testing.T) {
	was := color.NoColor
	defer func() { color.NoColor = was }()

	color.NoColor = false
	if !ColorEnabled() {
		t.Fatal("ColorEnabled disagrees with the colour package")
	}

	DisableColor()
	if ColorEnabled() {
		t.Error("DisableColor did not take effect")
	}
}
