package doc

import (
	"strings"
	"testing"
)

var (
	enDash     = string(rune(0x2013))
	emDash     = string(rune(0x2014))
	leftQuote  = string(rune(0x201C))
	rightQuote = string(rune(0x201D))
)

func TestIsQuote(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"tilde", "If you would judge, understand. ~ Seneca", true},
		{"tilde no space", "Well begun is half done. ~Aristotle", true},
		{"tilde own line", "We are what we repeatedly do.\n~ Will Durant", true},
		{"tilde lowercase name", "Every answer asks a more beautiful question.\n~e e cummings", true},
		{"tilde name and work", "Kneel, or you will be knelt.\n~ Robert Jordan, Lord of Chaos (Wheel of Time, #6)", true},
		{"tilde hyphenated name", "Simplicity is the ultimate form of sophistication. ~ Saint-Exupery", true},
		{"tilde then retweet credit", "Courage is grace under pressure. ~ Hemingway RT @quotes", true},
		{"tilde then link", "Failure is a chance to begin again. ~ Henry Ford http://t.co/abc", true},
		{"dash after full stop", "Punctuality is the virtue of the bored. -- Evelyn Waugh", true},
		{"dash after quote mark", `"Chance favours those in motion." - James Austin`, true},
		{"dash touching quote mark", `"Persistence beats strength."- Buddha`, true},
		{"en dash", "Less is more. " + enDash + " Mies", true},
		{"em dash", "Less is more." + emDash + "Mies", true},
		{"curly quote", leftQuote + "Teach me to care." + rightQuote + " - T. S. Eliot", true},
		{"dash inside attribution", "None but a coward boasts he never knew fear. - Ferdinand Foch (1851 - 1929)", true},

		{"plain post", "Went to the lake today. Nice weather.", false},
		{"approximately", "The world is ~12,000 years into agriculture.", false},
		{"link separator headline", "Finally! ~ DNR to build pistol range at the Spartanburg gun range http://t.co/x", false},
		{"dash headline with link", "Worth a read. -- Science Isn't Broken http://t.co/x", false},
		{"dash with no punctuation", "Birthday extravaganza. Day one - Discovery Museum", false},
		{"dash lowercase", "Tried it. - not great", false},
		{"attribution asks a question", "Great review. ~~ Who Knew Ink Would Be Our Saviour? http://t.co/x", false},
		{"commentary after quote", "Only two things are infinite. ~ Einstein\n\nI understand nothing.", false},
		{"attribution too long", "Short. ~ Someone who said this at a very long meeting in the office last year", false},
		{"hyphenated word", "A well-known fact", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			text := Normalise(tc.in)
			if got := IsQuote(text, len(strings.Fields(text))); got != tc.want {
				t.Errorf("IsQuote(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// A long post that ends in a quote is commentary with an epigraph, not a quote,
// and hiding quotes must not hide it.
func TestIsQuoteNeedsAShortPost(t *testing.T) {
	text := strings.Repeat("word ", quoteMaxWords) + "\n~ Seneca"
	if IsQuote(text, len(strings.Fields(text))) {
		t.Errorf("a %d-word post was flagged as a quote", len(strings.Fields(text)))
	}
}

func TestNewFlagsQuotes(t *testing.T) {
	d := New("s", "text", "k", "f", Document{Text: "If you would judge, understand. ~ Seneca"})
	if !d.Quote {
		t.Error("New did not flag a quote")
	}
	d = New("s", "text", "k", "f", Document{Text: "Went to the lake today."})
	if d.Quote {
		t.Error("New flagged a plain post as a quote")
	}
}
