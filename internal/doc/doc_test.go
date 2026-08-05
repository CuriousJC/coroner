package doc

import (
	"strings"
	"testing"
)

// The characters this package exists to clean up are invisible, so they are
// built from code points here rather than pasted in. A test whose input you
// cannot see in the source is a test you cannot review.
var (
	nbsp       = string(rune(0x00A0)) // no-break space
	narrowNBSP = string(rune(0x202F)) // narrow no-break space
	zwsp       = string(rune(0x200B)) // zero-width space
	bom        = string(rune(0xFEFF)) // byte order mark
	acute      = string(rune(0x0301)) // combining acute accent
)

func TestNormaliseCanonicalises(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"crlf", "one\r\ntwo", "one\ntwo"},
		{"lone cr", "one\rtwo", "one\ntwo"},
		{"tab becomes space", "one\ttwo", "one two"},
		{"non-breaking space", "one" + nbsp + "two", "one two"},
		{"narrow no-break space", "one" + narrowNBSP + "two", "one two"},
		{"zero width space", "wo" + zwsp + "rd", "word"},
		{"byte order mark mid text", "wo" + bom + "rd", "word"},
		{"trailing whitespace per line", "one   \ntwo  ", "one\ntwo"},
		{"blank runs collapse", "one\n\n\n\n\ntwo", "one\n\ntwo"},
		{"leading and trailing", "\n\n  one\n\n", "one"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Normalise(tc.in); got != tc.want {
				t.Errorf("Normalise(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNormaliseComposesUnicode is the case that matters most for identity: the
// same visible text written two legal ways must hash to one document, or the
// same essay exported by two different tools becomes two documents.
func TestNormaliseComposesUnicode(t *testing.T) {
	composed := "caf" + string(rune(0x00E9)) // é as a single code point
	decomposed := "cafe" + acute             // e followed by a combining accent

	if composed == decomposed {
		t.Fatal("test inputs are already identical, which defeats the point")
	}

	if Normalise(composed) != Normalise(decomposed) {
		t.Errorf("normalisation left two spellings of the same word different: %q vs %q",
			Normalise(composed), Normalise(decomposed))
	}
	if Hash(Normalise(composed)) != Hash(Normalise(decomposed)) {
		t.Error("the same word hashed differently depending on how it was encoded")
	}
}

func TestNormaliseIsIdempotent(t *testing.T) {
	in := "\r\n  Some text\twith" + zwsp + " junk\r\n\n\n\nand more  \r\n"

	once := Normalise(in)
	if twice := Normalise(once); once != twice {
		t.Errorf("Normalise is not idempotent: %q then %q", once, twice)
	}
}

func TestMakeIDIsStableAndScopedToSource(t *testing.T) {
	hash := Hash("some text")

	a := MakeID("substack", "my-post", hash)
	if b := MakeID("substack", "my-post", hash); a != b {
		t.Errorf("the same inputs produced two IDs: %s and %s", a, b)
	}

	// The same native key in a different corpus is a different document.
	// Relating the two is the dedupe pass's job, not the identifier's.
	if c := MakeID("facebook", "my-post", hash); a == c {
		t.Error("two corpora with the same native key collided into one ID")
	}

	// With no native key, content decides.
	keyless := MakeID("html", "", hash)
	if same := MakeID("html", "", hash); keyless != same {
		t.Error("a keyless document's ID is not stable")
	}
	if different := MakeID("html", "", Hash("other text")); keyless == different {
		t.Error("two different texts produced the same keyless ID")
	}

	if len(a) != 16 {
		t.Errorf("ID is %d characters, expected 16", len(a))
	}
}

func TestNewNormalisesBeforeHashing(t *testing.T) {
	crlf := New("s", "text", "k", "f.txt", Document{Text: "one\r\n\r\ntwo"})
	lf := New("s", "text", "k", "f.txt", Document{Text: "one\n\ntwo"})

	if crlf.ContentHash != lf.ContentHash {
		t.Error("line endings changed a document's content hash")
	}
	if crlf.Words != 2 {
		t.Errorf("Words = %d, want 2", crlf.Words)
	}
}

func TestNewStoresPublishedAsUTC(t *testing.T) {
	d := New("s", "text", "k", "f.txt", Document{Text: "text"})
	if !d.Published.IsZero() {
		t.Error("a document with no date should keep a zero time, not invent one")
	}
}

func TestSnippetStaysUnderLimit(t *testing.T) {
	long := strings.Repeat("word ", 100)

	got := Snippet(long, 40)
	if len(got) > 43 { // 40 plus the ellipsis
		t.Errorf("snippet is %d characters: %q", len(got), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("a truncated snippet should say so: %q", got)
	}

	if got := Snippet("short", 40); got != "short" {
		t.Errorf("a short snippet was altered: %q", got)
	}

	if got := Snippet("one\n\ntwo   three", 40); got != "one two three" {
		t.Errorf("snippet did not flatten whitespace: %q", got)
	}
}
