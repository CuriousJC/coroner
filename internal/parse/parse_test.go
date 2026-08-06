package parse

import (
	"strings"
	"testing"
)

func parseOne(t *testing.T, p Parser, rel, body string) []struct {
	Title, Text string
} {
	t.Helper()

	src := Source{Dir: ".", Name: "test", Type: "html", Author: "Fallback Author"}
	docs, err := p.ParseFile(src, rel, []byte(body))
	if err != nil {
		t.Fatal(err)
	}

	out := make([]struct{ Title, Text string }, len(docs))
	for i, d := range docs {
		out[i] = struct{ Title, Text string }{d.Title, d.Text}
	}
	return out
}

func TestTypesAreRegistered(t *testing.T) {
	types := Types()

	want := map[string]bool{"text": true, "html": true, "facebook": true, "substack": true}
	for _, ty := range types {
		delete(want, ty)
	}
	if len(want) != 0 {
		t.Errorf("missing registered types: %v", want)
	}

	// Sorted, because this list appears in error messages and help text.
	for i := 1; i < len(types); i++ {
		if types[i-1] > types[i] {
			t.Errorf("Types() is not sorted: %v", types)
			break
		}
	}
}

func TestImplementedDistinguishesPendingParsers(t *testing.T) {
	if !Implemented("html") {
		t.Error("html should be implemented")
	}
	if !Implemented("text") {
		t.Error("text should be implemented")
	}
	if !Implemented("facebook") {
		t.Error("facebook should be implemented")
	}
	if !Implemented("htmlsite") {
		t.Error("htmlsite should be implemented")
	}
	if !Implemented("substack") {
		t.Error("substack should be implemented")
	}
	if Implemented("nonsense") {
		t.Error("an unregistered type reported as implemented")
	}
}

// TestPendingParserIsAnHonestError checks that a format coroner knows about but
// cannot read yet says so, rather than being told the type is unknown.
//
// Exercised directly rather than through the registry: every registered type now
// has a real parser behind it. The placeholder stays because the next format to
// be named before it is written should get this error and not a lie about being
// unrecognised, and a mechanism with no test is a mechanism that has quietly
// stopped working by the time it is needed.
func TestPendingParserIsAnHonestError(t *testing.T) {
	var p Parser = &pendingParser{format: "somethingnew", waitingFor: "a real export"}

	err := p.Prepare(Source{})
	if err == nil {
		t.Fatal("the pending parser reported success")
	}
	if !strings.Contains(err.Error(), "not written yet") {
		t.Errorf("the error does not explain the situation: %v", err)
	}
}

// Substack is no longer pending, which is the thing most likely to be forgotten
// when a placeholder is replaced.
func TestSubstackIsImplemented(t *testing.T) {
	if !Implemented("substack") {
		t.Error("substack still reports as pending")
	}

	p, err := New("substack")
	if err != nil {
		t.Fatalf("New(\"substack\"): %v", err)
	}
	if _, ok := p.(*substackParser); !ok {
		t.Errorf("New(\"substack\") = %T, want *substackParser", p)
	}
}

func TestTextParserSplitsLeadingHeading(t *testing.T) {
	p := &textParser{}

	got := parseOne(t, p, "posts/thing.md", "# The Title\n\nThe body of the post.\n")
	if len(got) != 1 {
		t.Fatalf("parsed %d documents", len(got))
	}
	if got[0].Title != "The Title" {
		t.Errorf("title = %q", got[0].Title)
	}
	if strings.Contains(got[0].Text, "The Title") {
		t.Errorf("the title was left in the body: %q", got[0].Text)
	}
}

func TestTextParserFallsBackToFilename(t *testing.T) {
	p := &textParser{}

	got := parseOne(t, p, "posts/2019-some-post.md", "Just a body, no heading.\n")
	if got[0].Title != "2019 some post" {
		t.Errorf("title = %q, want %q", got[0].Title, "2019 some post")
	}
}

func TestTextParserSkipsEmptyFiles(t *testing.T) {
	p := &textParser{}

	if got := parseOne(t, p, "empty.txt", "   \n\n  "); len(got) != 0 {
		t.Errorf("an empty file produced %d documents", len(got))
	}
}

func TestHTMLParserExtractsProse(t *testing.T) {
	p := &htmlParser{}

	body := `<!doctype html>
<html><head>
<title>Page Title | Some Site</title>
<meta property="og:title" content="The Real Title">
<meta property="article:published_time" content="2021-03-14T09:00:00Z">
<link rel="canonical" href="https://example.com/post">
<style>body { color: red }</style>
</head>
<body>
<nav><a href="/">Home</a><a href="/about">About</a></nav>
<article>
<h1>The Real Title</h1>
<p>First paragraph of the actual writing.</p>
<p>Second paragraph, which says something else.</p>
<script>console.log("not prose")</script>
</article>
<footer>Copyright someone</footer>
</body></html>`

	src := Source{Dir: ".", Name: "test", Type: "html"}
	docs, err := p.ParseFile(src, "post.html", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("parsed %d documents", len(docs))
	}

	d := docs[0]

	if d.Title != "The Real Title" {
		t.Errorf("title = %q; og:title should beat the <title> tag, which carries the site name", d.Title)
	}
	if d.URL != "https://example.com/post" {
		t.Errorf("url = %q", d.URL)
	}
	if d.Published.Format("2006-01-02") != "2021-03-14" {
		t.Errorf("published = %v", d.Published)
	}

	for _, unwanted := range []string{"console.log", "color: red", "Copyright", "About"} {
		if strings.Contains(d.Text, unwanted) {
			t.Errorf("extracted text contains %q, which is markup or navigation, not writing:\n%s", unwanted, d.Text)
		}
	}
	for _, wanted := range []string{"First paragraph", "Second paragraph"} {
		if !strings.Contains(d.Text, wanted) {
			t.Errorf("extracted text is missing %q:\n%s", wanted, d.Text)
		}
	}

	// Paragraph boundaries have to survive, because the chunker splits on them.
	if !strings.Contains(d.Text, "writing.\n\nSecond") {
		t.Errorf("paragraphs ran together, which would destroy chunk boundaries:\n%q", d.Text)
	}
}

func TestHTMLParserPrefersArticleOverBody(t *testing.T) {
	p := &htmlParser{}

	body := `<html><body>
<header>Site wide banner that appears on every single page</header>
<article><p>The writing itself.</p></article>
<footer>Site wide footer that appears on every single page</footer>
</body></html>`

	got := parseOne(t, p, "post.html", body)
	if len(got) != 1 {
		t.Fatalf("parsed %d documents", len(got))
	}
	if strings.Contains(got[0].Text, "banner") || strings.Contains(got[0].Text, "footer") {
		t.Errorf("boilerplate leaked into the text: %q", got[0].Text)
	}
}

func TestHTMLParserDecodesEntities(t *testing.T) {
	p := &htmlParser{}

	got := parseOne(t, p, "post.html", `<html><body><article><p>Tom &amp; Jerry &lt;3 &quot;quotes&quot;</p></article></body></html>`)
	if !strings.Contains(got[0].Text, `Tom & Jerry <3 "quotes"`) {
		t.Errorf("entities were not decoded: %q", got[0].Text)
	}
}

// TestHTMLParserSurvivesMalformedMarkup is why this uses a real tokeniser rather
// than tag stripping: an attribute containing ">" breaks the naive approach.
func TestHTMLParserSurvivesMalformedMarkup(t *testing.T) {
	p := &htmlParser{}

	body := `<html><body><article>
<p title="a > b">Text after an awkward attribute.</p>
<p>Unclosed paragraph
<div>and a stray div</article></body></html>`

	got := parseOne(t, p, "post.html", body)
	if len(got) != 1 {
		t.Fatalf("parsed %d documents", len(got))
	}
	if !strings.Contains(got[0].Text, "Text after an awkward attribute.") {
		t.Errorf("lost text following an attribute containing '>': %q", got[0].Text)
	}
	if strings.Contains(got[0].Text, "a > b") {
		t.Errorf("an attribute value was extracted as prose: %q", got[0].Text)
	}
}

func TestHTMLParserIsDeterministic(t *testing.T) {
	p := &htmlParser{}
	body := `<html><head><title>T</title></head><body><article><p>One.</p><p>Two.</p></article></body></html>`

	src := Source{Dir: ".", Name: "test", Type: "html"}
	first, err := p.ParseFile(src, "post.html", []byte(body))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 10; i++ {
		again, err := p.ParseFile(src, "post.html", []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if again[0].ContentHash != first[0].ContentHash || again[0].ID != first[0].ID {
			t.Fatalf("run %d produced a different document from identical bytes", i)
		}
	}
}

func TestParseTimeIsStrict(t *testing.T) {
	if got := parseTime("2021-03-14"); got.Format("2006-01-02") != "2021-03-14" {
		t.Errorf("a plain date did not parse: %v", got)
	}
	if got := parseTime("sometime last spring"); !got.IsZero() {
		t.Errorf("an unparseable date produced %v; a wrong date is worse than none", got)
	}
	if got := parseTime(""); !got.IsZero() {
		t.Errorf("an empty date produced %v", got)
	}
}
