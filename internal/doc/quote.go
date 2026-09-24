package doc

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A quote is a short post that is someone else's words with an attribution at
// the end: `If you would judge, understand. ~ Seneca`, or `"…" - Henry Ford`.
//
// The limits are word counts. A post of quoteMaxWords or more is not a quote
// even when it ends in one, because it is then mostly commentary around an
// epigraph. An attribution runs to attributionMaxWords, which is long enough
// for `~ Robert Jordan, Lord of Chaos (Wheel of Time, #6)`.
//
// A trailing link changes what a marker means. `Finally! ~ DNR to build pistol
// range … http://…` and `comment -- Headline http://…` use the marker to set
// off an article title, not an author. After a link, a dash never introduces an
// attribution, and a tilde does only for linkedMaxWords or fewer, which keeps
// retweets like `~ Henry Ford http://…`.
const (
	quoteMaxWords       = 100
	attributionMaxWords = 10
	dashMaxWords        = 6
	linkedMaxWords      = 6
)

// trailer is a link or retweet credit at the very end of a post, after the
// attribution it follows.
var trailer = regexp.MustCompile(`\s*(?:https?://\S+|(?:RT|via)\s+@\w+)\s*$`)

// IsQuote reports whether a post of the given length is a quotation with a
// trailing attribution.
//
// The attribution must be on the text's last line, after the last tilde on that
// line, or after a dash (-, --, en or em dash) that follows closing punctuation.
// The punctuation requirement is what separates `profound truth. - Niels Bohr`
// from a hyphenated name or `Day one - Discovery Museum`. A tilde needs no such
// context: nobody writes one mid-sentence. Tilde attributions may start
// lowercase (`~e e cummings`); dash attributions must start with a capital.
func IsQuote(text string, words int) bool {
	if words >= quoteMaxWords {
		return false
	}

	body, linked := stripTrailers(strings.TrimSpace(text))
	start := strings.LastIndexByte(body, '\n') + 1
	line := body[start:]

	if i := strings.LastIndexByte(line, '~'); i >= 0 {
		return isAttribution(line[i+1:], true, linked)
	}
	if linked {
		return false
	}

	// Right to left, so a dash inside the attribution -- `Ferdinand Foch
	// (1851 - 1929)` -- is tried and rejected before the one that introduces it.
	for i := len(line); i > 0; {
		r, size := utf8.DecodeLastRuneInString(line[:i])
		i -= size
		if !isDash(r) {
			continue
		}
		at := i
		for at > 0 && line[at-1] == '-' {
			at--
		}
		before := strings.TrimRight(body[:start+at], " ")
		if endsInClosingPunct(before) && isAttribution(line[i+size:], false, false) {
			return true
		}
		i = at
	}
	return false
}

// stripTrailers removes trailing links and retweet credits, reporting whether a
// link was among them.
func stripTrailers(s string) (string, bool) {
	linked := false
	for {
		loc := trailer.FindStringIndex(s)
		if loc == nil {
			return s, linked
		}
		if strings.Contains(s[loc[0]:], "http") {
			linked = true
		}
		s = s[:loc[0]]
	}
}

func isAttribution(a string, tilde, linked bool) bool {
	a = strings.TrimSpace(a)
	if a == "" || strings.ContainsAny(a, "?!") {
		return false
	}

	first, _ := utf8.DecodeRuneInString(a)
	if tilde && !unicode.IsLetter(first) || !tilde && !unicode.IsUpper(first) {
		return false
	}

	n := len(strings.Fields(a))
	switch {
	case n > attributionMaxWords:
		return false
	case !tilde && n > dashMaxWords:
		return false
	case linked && n > linkedMaxWords:
		return false
	}
	return true
}

func isDash(r rune) bool {
	return r == '-' || r == '–' || r == '—'
}

// endsInClosingPunct reports whether s ends a sentence or a quotation: a full
// stop, question or exclamation mark, or a straight or curly closing quote.
func endsInClosingPunct(s string) bool {
	r, _ := utf8.DecodeLastRuneInString(s)
	return strings.ContainsRune(".!?\"'”’", r)
}
