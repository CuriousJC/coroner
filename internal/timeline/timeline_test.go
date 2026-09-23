package timeline

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/curiousjc/coroner/internal/doc"
	"github.com/curiousjc/coroner/internal/store"
)

func on(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func document(source, key, title string, published time.Time, text string) doc.Document {
	return doc.New(source, source+"type", key, key+".txt", doc.Document{
		Title:     title,
		Published: published,
		Text:      text,
	})
}

const review = "This book took a long time to get going, but once the heist started " +
	"I could not put it down. The magic system is clever without being fussy, and " +
	"the ending earned every page that came before it. I will read the sequel."

// fixture is three corpora: a review cross-posted from goodreads to facebook on
// the same day, a standalone facebook post, an older html post, and an undated
// review.
func fixture() ([]*store.Corpus, *store.Manifest) {
	corpora := []*store.Corpus{
		{Name: "facebook", Docs: []doc.Document{
			document("facebook", "fb1", "", on(2021, 3, 14), review),
			document("facebook", "fb2", "", on(2022, 1, 2), "Snow day. Everyone is home and nobody is working."),
		}},
		{Name: "goodreads", Docs: []doc.Document{
			document("goodreads", "gr1", "The Heist by Someone ★★★★☆", on(2021, 3, 14), review),
			document("goodreads", "gr2", "Unfinished by Someone", time.Time{}, "Did not finish. Bored at page 150."),
		}},
		{Name: "site", Docs: []doc.Document{
			document("site", "old", "First Post", on(2016, 10, 25), "The first thing I ever put on this site."),
		}},
	}
	man := &store.Manifest{Sources: []store.SourceInfo{
		{Name: "facebook", Type: "facebook", Priority: 10},
		{Name: "goodreads", Type: "goodreads", Priority: 25},
		{Name: "site", Type: "htmlsite", Priority: 20},
	}}
	return corpora, man
}

// Duplicated writing is one entry: the copy that wins on priority, carrying the
// others as copies.
func TestBuildFoldsDuplicatesIntoTheWinner(t *testing.T) {
	tl := Build(fixture())

	if tl.Documents != 5 || tl.Folded != 1 || len(tl.Entries) != 4 {
		t.Fatalf("documents %d, folded %d, entries %d; want 5, 1, 4", tl.Documents, tl.Folded, len(tl.Entries))
	}

	var heist *Entry
	for i := range tl.Entries {
		if tl.Entries[i].Text == review {
			if heist != nil {
				t.Fatal("the cross-posted review appears twice")
			}
			heist = &tl.Entries[i]
		}
	}
	if heist == nil {
		t.Fatal("the cross-posted review is missing")
	}
	if heist.Source != "goodreads" {
		t.Errorf("the entry is the %s copy; goodreads outranks facebook", heist.Source)
	}
	if len(heist.Copies) != 1 || heist.Copies[0].Source != "facebook" {
		t.Errorf("copies = %+v, want the facebook copy", heist.Copies)
	}

	counts := map[string]int{}
	for _, s := range tl.Sources {
		counts[s.Name] = s.Entries
	}
	if counts["facebook"] != 1 || counts["goodreads"] != 2 || counts["site"] != 1 {
		t.Errorf("per-corpus entries = %v", counts)
	}
}

// Newest first, so the oldest writing is at the end; undated after that.
func TestBuildOrdersNewestFirstUndatedLast(t *testing.T) {
	tl := Build(fixture())

	var got []string
	for _, e := range tl.Entries {
		got = append(got, e.Source+":"+day(e.Published))
	}
	want := []string{"facebook:2022-01-02", "goodreads:2021-03-14", "site:2016-10-25", "goodreads:"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("order = %v\nwant    %v", got, want)
	}
}

// The page holds the whole private corpus, so it must load nothing when opened,
// and text is writing, never markup.
func TestHTMLIsSelfContainedAndEscaped(t *testing.T) {
	corpora, man := fixture()
	corpora[1].Docs[1].Text = `<script>alert("x")</script> and <img src="https://example.com/x.png">`

	var b bytes.Buffer
	if err := Build(corpora, man).HTML(&b); err != nil {
		t.Fatal(err)
	}
	page := b.String()

	// Walk the markup rather than grep the bytes: the text legitimately contains
	// "src=" and "https://", escaped, and that is harmless.
	z := html.NewTokenizer(strings.NewReader(page))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		tok := z.Token()
		switch tok.Data {
		case "script", "img", "link", "iframe", "object", "embed", "video", "audio", "source":
			t.Errorf("page has a <%s> element", tok.Data)
		}
		for _, a := range tok.Attr {
			if a.Key == "src" || a.Key == "srcset" {
				t.Errorf("<%s> has a %s attribute", tok.Data, a.Key)
			}
		}
	}
	if strings.Contains(page, "@import") || strings.Contains(page, "url(") {
		t.Error("the stylesheet loads something")
	}
	if !strings.Contains(page, "&lt;script&gt;") {
		t.Error("markup in the text was not escaped")
	}

	// Oldest at the bottom: the 2016 post comes after the 2022 one.
	if strings.Index(page, "The first thing I ever put") < strings.Index(page, "Snow day") {
		t.Error("the oldest writing is not below the newest")
	}
}

// The same corpus renders the same bytes, whatever order corpora arrive in.
func TestOutputIsDeterministic(t *testing.T) {
	render := func(reverse bool) (string, string) {
		corpora, man := fixture()
		if reverse {
			for i, j := 0, len(corpora)-1; i < j; i, j = i+1, j-1 {
				corpora[i], corpora[j] = corpora[j], corpora[i]
			}
		}
		tl := Build(corpora, man)
		var h, j bytes.Buffer
		if err := tl.HTML(&h); err != nil {
			t.Fatal(err)
		}
		if err := tl.JSON(&j); err != nil {
			t.Fatal(err)
		}
		return h.String(), j.String()
	}

	h1, j1 := render(false)
	h2, j2 := render(true)
	if h1 != h2 {
		t.Error("HTML differs with corpus order")
	}
	if j1 != j2 {
		t.Error("JSON differs with corpus order")
	}
}

// Undated entries omit the field rather than claiming the year 1.
func TestJSONOmitsZeroDates(t *testing.T) {
	var b bytes.Buffer
	if err := Build(fixture()).JSON(&b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "0001-01-01") {
		t.Error("an undated entry was written with a zero date")
	}

	var back Timeline
	if err := json.Unmarshal(b.Bytes(), &back); err != nil {
		t.Fatalf("the JSON does not read back: %v", err)
	}
	if len(back.Entries) != 4 {
		t.Errorf("read back %d entries", len(back.Entries))
	}
}

func TestPreviewCutsAtAWord(t *testing.T) {
	long := strings.Repeat("word ", 200)
	p := preview(long)
	if !strings.HasSuffix(p, "word…") {
		t.Errorf("preview does not end on a whole word: %q", p[len(p)-20:])
	}
	if n := len([]rune(p)); n > previewRunes+1 {
		t.Errorf("preview is %d runes, want at most %d", n, previewRunes+1)
	}
}
