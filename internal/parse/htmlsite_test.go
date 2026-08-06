package parse

import (
	"strings"
	"testing"
	"time"
)

func parseSite(t *testing.T, rel, body string) []doc0 {
	t.Helper()

	p := &htmlsiteParser{}
	src := Source{Dir: ".", Name: "site", Type: "htmlsite", Author: "Justin"}

	docs, err := p.ParseFile(src, rel, []byte(body))
	if err != nil {
		t.Fatalf("ParseFile(%q): %v", rel, err)
	}

	out := make([]doc0, len(docs))
	for i, d := range docs {
		out[i] = doc0{Title: d.Title, Text: d.Text, Published: d.Published}
	}
	return out
}

// doc0 is the slice of a Document these tests care about, so a failure prints
// the three fields under test rather than every hash on the struct.
type doc0 struct {
	Title     string
	Text      string
	Published time.Time
}

const sitePage = `<!DOCTYPE html>
<html>
<head><title>xolsiion writing</title></head>
<body>
  <div class="container">
    <h1>The Default</h1>
    <h2>07.24.2019</h2>
    <p>Today my son made a friend.</p>
  </div>
</body>
</html>`

// The site banner in <title> is the whole reason this format is not the generic
// html parser. If it ever reaches a document, every document in the corpus gets
// the same title and every vector gets the same prefix.
func TestHTMLSiteTakesTitleFromH1NotTitleTag(t *testing.T) {
	got := parseSite(t, "2019-07-24-the-default.html", sitePage)
	if len(got) != 1 {
		t.Fatalf("want 1 document, got %d", len(got))
	}

	if got[0].Title != "The Default" {
		t.Errorf("title = %q, want %q", got[0].Title, "The Default")
	}
	if strings.Contains(got[0].Title, "xolsiion") {
		t.Errorf("site banner leaked into the title: %q", got[0].Title)
	}
	if strings.Contains(got[0].Text, "xolsiion") {
		t.Errorf("site banner leaked into the text: %q", got[0].Text)
	}
}

// The h1 and the h2 are chrome for indexing purposes: the title is carried on
// the document and prepended by doc.Indexed, and the date line is not writing.
// Leaving either in the body would double the title in every snippet and put a
// bare date at the head of every document.
func TestHTMLSiteRemovesTitleAndDateFromText(t *testing.T) {
	got := parseSite(t, "2019-07-24-the-default.html", sitePage)

	if strings.Contains(got[0].Text, "The Default") {
		t.Errorf("h1 left in the body text: %q", got[0].Text)
	}
	if strings.Contains(got[0].Text, "07.24.2019") {
		t.Errorf("date line left in the body text: %q", got[0].Text)
	}
	if !strings.Contains(got[0].Text, "Today my son made a friend.") {
		t.Errorf("body text missing: %q", got[0].Text)
	}
}

// Measured on the real export: the filename and the <h2> disagree on 14 of 183
// pages, and the <h2> is wrong every time. Four pages carry the first post's
// date because its page was used as a template, which is what this reproduces.
func TestHTMLSitePrefersFilenameDateOverBody(t *testing.T) {
	page := strings.Replace(sitePage, "<h2>07.24.2019</h2>", "<h2>10.25.2016</h2>", 1)

	got := parseSite(t, "2025-08-16-what-is-government.html", page)
	want := time.Date(2025, 8, 16, 0, 0, 0, 0, time.UTC)

	if !got[0].Published.Equal(want) {
		t.Errorf("published = %s, want %s (filename must win)", got[0].Published, want)
	}
}

// Both separators appear in filenames, and ten pages drop a leading zero.
func TestHTMLSiteFilenameDateFormats(t *testing.T) {
	cases := []struct {
		rel  string
		want time.Time
	}{
		{"2016.10.25-first-politics-post.html", time.Date(2016, 10, 25, 0, 0, 0, 0, time.UTC)},
		{"2019-07-24-the-default.html", time.Date(2019, 7, 24, 0, 0, 0, 0, time.UTC)},
		{"2020-7-10-time-well-spent.html", time.Date(2020, 7, 10, 0, 0, 0, 0, time.UTC)},
		{"2025-04-7-own-it.html", time.Date(2025, 4, 7, 0, 0, 0, 0, time.UTC)},
		{"2025-07-19--tank-disaster copy.html", time.Date(2025, 7, 19, 0, 0, 0, 0, time.UTC)},
	}

	for _, c := range cases {
		if got := dateFromFilename(c.rel); !got.Equal(c.want) {
			t.Errorf("dateFromFilename(%q) = %s, want %s", c.rel, got, c.want)
		}
	}
}

// Five pages were started from a template and never had the heading filled in.
// Letting the literal word through would give five unrelated posts one title.
func TestHTMLSitePlaceholderTitleFallsBackToSlug(t *testing.T) {
	page := strings.Replace(sitePage, "<h1>The Default</h1>", "<h1>title</h1>", 1)

	got := parseSite(t, "2019-11-04-impeachment.html", page)
	if got[0].Title != "impeachment" {
		t.Errorf("title = %q, want %q", got[0].Title, "impeachment")
	}
}

// Three pages have no <h1> at all.
func TestHTMLSiteMissingH1FallsBackToSlug(t *testing.T) {
	page := strings.Replace(sitePage, "<h1>The Default</h1>", "", 1)

	got := parseSite(t, "2024-11-07-let-him-in.html", page)
	if got[0].Title != "let him in" {
		t.Errorf("title = %q, want %q", got[0].Title, "let him in")
	}
	if strings.HasPrefix(got[0].Title, "2024") {
		t.Errorf("date prefix left in the slug title: %q", got[0].Title)
	}
}

// The six " copy" pages are ordinary posts -- none has a surviving twin -- so
// the suffix is a filename accident that must not become part of a title.
func TestHTMLSiteCopySuffixDroppedFromSlugTitle(t *testing.T) {
	page := strings.Replace(sitePage, "<h1>The Default</h1>", "", 1)

	got := parseSite(t, "2020-06-30-trump-era copy.html", page)
	if got[0].Title != "trump era" {
		t.Errorf("title = %q, want %q", got[0].Title, "trump era")
	}
}

// Twenty-three pages have no <p> tags: the writing sits directly in the
// container, hand-wrapped. The wrapping is markup whitespace and must not
// survive as hard breaks, but the blank line between paragraphs is authored
// structure the chunker splits on.
func TestHTMLSiteUnwrapsSoftLineBreaksButKeepsParagraphs(t *testing.T) {
	page := `<!DOCTYPE html><html><head><title>xolsiion writing</title></head><body>
  <div class="container">
    <h1>Impeachment</h1>
    <h2>11.04.2019</h2>
    We're all in this together.
    It's a trite phrase, perhaps, for me.

    Closely related to that is an implicit
    understanding.
  </div>
</body></html>`

	got := parseSite(t, "2019-11-04-impeachment.html", page)
	text := got[0].Text

	if !strings.Contains(text, "We're all in this together. It's a trite phrase, perhaps, for me.") {
		t.Errorf("soft line break not unwrapped:\n%q", text)
	}
	if !strings.Contains(text, "Closely related to that is an implicit understanding.") {
		t.Errorf("soft line break not unwrapped in second paragraph:\n%q", text)
	}
	if !strings.Contains(text, "\n\n") {
		t.Errorf("paragraph break not preserved:\n%q", text)
	}
}

// A page whose <h2> is not a date is a page whose <h2> is writing.
func TestHTMLSiteKeepsNonDateH2AsText(t *testing.T) {
	page := strings.Replace(sitePage, "<h2>07.24.2019</h2>", "<h2>A Real Subheading</h2>", 1)

	got := parseSite(t, "2019-07-24-the-default.html", page)
	if !strings.Contains(got[0].Text, "A Real Subheading") {
		t.Errorf("non-date h2 was dropped: %q", got[0].Text)
	}
}

func TestHTMLSiteIsRegistered(t *testing.T) {
	p, err := New("htmlsite")
	if err != nil {
		t.Fatalf("New(\"htmlsite\"): %v", err)
	}
	if _, ok := p.(*htmlsiteParser); !ok {
		t.Errorf("New(\"htmlsite\") = %T, want *htmlsiteParser", p)
	}
}
