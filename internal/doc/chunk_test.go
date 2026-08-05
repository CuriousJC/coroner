package doc

import (
	"strings"
	"testing"
)

func makeDoc(text string) Document {
	return New("test", "text", "key", "file.txt", Document{Text: text})
}

func TestSplitIsDeterministic(t *testing.T) {
	d := makeDoc(strings.Repeat("A paragraph of some length that says a thing.\n\n", 60))

	first := Split(d)
	for i := 0; i < 5; i++ {
		again := Split(d)
		if len(again) != len(first) {
			t.Fatalf("run %d produced %d chunks, first run produced %d", i, len(again), len(first))
		}
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("run %d chunk %d differs from the first run", i, j)
			}
		}
	}
}

func TestSplitRespectsSizes(t *testing.T) {
	d := makeDoc(strings.Repeat("Sentences that add up to a reasonable paragraph length here.\n\n", 200))

	chunks := Split(d)
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		if len(c.Text) > MaxChars {
			t.Errorf("chunk %d is %d characters, over the %d ceiling", i, len(c.Text), MaxChars)
		}
	}
}

func TestSplitBreaksOversizedParagraph(t *testing.T) {
	// One paragraph, no blank lines, well over the ceiling.
	d := makeDoc(strings.Repeat("word ", 2000))

	chunks := Split(d)
	if len(chunks) < 2 {
		t.Fatalf("an oversized paragraph was not split: got %d chunks", len(chunks))
	}
	for i, c := range chunks {
		if len(c.Text) > MaxChars {
			t.Errorf("chunk %d is %d characters, over the %d ceiling", i, len(c.Text), MaxChars)
		}
	}
}

// TestSplitMergesRunt guards the rule that a two-line orphan at the end is
// folded back rather than left to retrieve badly on its own.
func TestSplitMergesRunt(t *testing.T) {
	body := strings.Repeat("A paragraph with enough words in it to matter for length.\n\n", 30)
	d := makeDoc(body + "Tiny.")

	chunks := Split(d)
	last := chunks[len(chunks)-1]

	if len(last.Text) < MinChars {
		t.Errorf("the final chunk is %d characters, under the %d floor: %q", len(last.Text), MinChars, last.Text)
	}
	if !strings.HasSuffix(last.Text, "Tiny.") {
		t.Errorf("the runt was dropped rather than merged: %q", last.Text)
	}
}

func TestSplitEmptyDocument(t *testing.T) {
	if chunks := Split(makeDoc("   \n\n  ")); chunks != nil {
		t.Errorf("an empty document produced %d chunks", len(chunks))
	}
}

func TestChunkIDsAreUniqueAndOrdered(t *testing.T) {
	d := makeDoc(strings.Repeat("Something worth a sentence or two of space.\n\n", 100))

	seen := map[string]bool{}
	for i, c := range Split(d) {
		if c.Index != i {
			t.Errorf("chunk at position %d has index %d", i, c.Index)
		}
		if c.DocID != d.ID {
			t.Errorf("chunk %d belongs to %s, expected %s", i, c.DocID, d.ID)
		}
		if seen[c.ID] {
			t.Errorf("duplicate chunk ID %s", c.ID)
		}
		seen[c.ID] = true

		if c.Hash != Hash(c.Text) {
			t.Errorf("chunk %d hash does not match its text", i)
		}
	}
}

// TestChunkOffsetsLocateText is what makes Start and End worth storing: a chunk
// must be findable in its document without re-running the chunker.
func TestChunkOffsetsLocateText(t *testing.T) {
	d := makeDoc(strings.Repeat("A sentence that is long enough to be worth indexing.\n\n", 80))

	for i, c := range Split(d) {
		if c.Start < 0 || c.End > len(d.Text) || c.Start > c.End {
			t.Fatalf("chunk %d has impossible offsets %d..%d in %d bytes", i, c.Start, c.End, len(d.Text))
		}
		if !strings.Contains(d.Text[c.Start:c.End], c.Text) {
			t.Errorf("chunk %d text is not at its recorded offsets", i)
		}
	}
}
