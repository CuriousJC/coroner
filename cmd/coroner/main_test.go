package main

import (
	"strings"
	"testing"
)

// TestReorderPutsFlagsFirst guards a bug that was live: Go's flag package stops
// parsing at the first non-flag argument, so `coroner search "choice" -hits=25`
// parsed as a query of `choice -hits=25` and silently ignored the flag. Silently
// is what made it worth fixing rather than documenting.
func TestReorderPutsFlagsFirst(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		flags []string
		query string
	}{
		{
			name:  "flag after query",
			args:  []string{"choice", "-hits=25"},
			flags: []string{"-hits=25"},
			query: "choice",
		},
		{
			name:  "flag with a separate value after query",
			args:  []string{"choice", "-hits", "25"},
			flags: []string{"-hits", "25"},
			query: "choice",
		},
		{
			name:  "bool flag does not swallow the next word",
			args:  []string{"choice", "-verbose", "of", "words"},
			flags: []string{"-verbose"},
			query: "choice of words",
		},
		{
			name:  "flags before query still work",
			args:  []string{"-hits=25", "choice"},
			flags: []string{"-hits=25"},
			query: "choice",
		},
		{
			name:  "multi-word query",
			args:  []string{"branching", "logic", "-mode=vector"},
			flags: []string{"-mode=vector"},
			query: "branching logic",
		},
		{
			name:  "double dash ends flag parsing",
			args:  []string{"--", "-not-a-flag"},
			flags: nil,
			query: "-not-a-flag",
		},
		{
			name:  "double dash form of a real flag",
			args:  []string{"choice", "--hits=5"},
			flags: []string{"--hits=5"},
			query: "choice",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newFlagSet("search")
			c.fs.Int("hits", 10, "")
			c.fs.String("mode", "hybrid", "")

			got := c.reorder(tc.args)

			if err := c.fs.Parse(got); err != nil {
				t.Fatalf("reordered args did not parse: %v (from %v)", err, got)
			}

			if query := strings.Join(c.fs.Args(), " "); query != tc.query {
				t.Errorf("query = %q, want %q (reordered to %v)", query, tc.query, got)
			}

			for _, f := range tc.flags {
				if !containsArg(got[:len(got)-len(c.fs.Args())], f) {
					t.Errorf("%q did not end up in the flag section of %v", f, got)
				}
			}
		})
	}
}

// TestReorderAppliesTheFlag is the end of the story: the value has to actually
// reach the variable, not merely be sorted into the right half of the slice.
func TestReorderAppliesTheFlag(t *testing.T) {
	c := newFlagSet("search")
	hits := c.fs.Int("hits", 10, "")

	if err := c.fs.Parse(c.reorder([]string{"choice", "-hits=25"})); err != nil {
		t.Fatal(err)
	}

	if *hits != 25 {
		t.Errorf("hits = %d, want 25; the flag was parsed but not applied", *hits)
	}
	if got := strings.Join(c.fs.Args(), " "); got != "choice" {
		t.Errorf("query = %q, want \"choice\"", got)
	}
}

func TestReorderLeavesUnknownFlagsForTheFlagSet(t *testing.T) {
	c := newFlagSet("search")

	got := c.reorder([]string{"choice", "-nonsense=1"})
	if !containsArg(got, "-nonsense=1") {
		t.Errorf("an unknown flag was dropped rather than passed on: %v", got)
	}
}

func TestBar(t *testing.T) {
	if got := bar(0, 0, 10); strings.TrimSpace(got) != "" {
		t.Errorf("a zero top score produced a bar: %q", got)
	}
	if got := bar(1, 1, 10); len([]rune(got)) != 10 {
		t.Errorf("bar is %d cells wide, want 10", len([]rune(got)))
	}
	if got := bar(2, 1, 10); len([]rune(strings.TrimSpace(got))) != 10 {
		t.Errorf("a score above the top overflowed the bar: %q", got)
	}
	// Anything nonzero gets at least one cell, so a result never renders as
	// nothing at all.
	if got := bar(0.0001, 1, 10); strings.TrimSpace(got) == "" {
		t.Errorf("a small but nonzero score rendered as an empty bar")
	}
}

func TestPluralAndComma(t *testing.T) {
	if got := plural(1, "document", "documents"); got != "1 document" {
		t.Errorf("plural(1) = %q", got)
	}
	if got := plural(0, "document", "documents"); got != "0 documents" {
		t.Errorf("plural(0) = %q", got)
	}
	if got := comma(1234567); got != "1,234,567" {
		t.Errorf("comma(1234567) = %q", got)
	}
	if got := comma(100); got != "100" {
		t.Errorf("comma(100) = %q", got)
	}
	if got := comma(-4321); got != "-4,321" {
		t.Errorf("comma(-4321) = %q", got)
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
