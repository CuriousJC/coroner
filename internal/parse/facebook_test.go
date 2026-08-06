package parse

import (
	"strings"
	"testing"
	"time"
)

// Every fixture here is invented. The parser was written against a real export,
// but a real export is someone's personal writing and this repository is public,
// so the tests reproduce the export's *shape* and none of its content.
//
// The shape is what matters anyway: a flat array of records, at most one `post`
// per record's data array, captions hanging off attachment media, and a `title`
// that is chrome rather than a title.

func fbSource() Source {
	return Source{Dir: ".", Name: "fb", Type: "facebook", Author: "A Writer"}
}

func TestFacebookExtractsPostText(t *testing.T) {
	p := &facebookParser{}

	body := `[
	  {
	    "timestamp": 1600000000,
	    "title": "A Writer updated his status.",
	    "data": [
	      {"update_timestamp": 1600000000},
	      {"post": "A thought about choosing between two roads."},
	      {}
	    ]
	  }
	]`

	docs, err := p.ParseFile(fbSource(), "your_posts_1.json", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("parsed %d documents, want 1", len(docs))
	}

	d := docs[0]
	if d.Text != "A thought about choosing between two roads." {
		t.Errorf("text = %q", d.Text)
	}
	if want := time.Unix(1600000000, 0).UTC(); !d.Published.Equal(want) {
		t.Errorf("published = %v, want %v", d.Published, want)
	}
	if d.Author != "A Writer" {
		t.Errorf("author = %q", d.Author)
	}
	if d.File != "your_posts_1.json" {
		t.Errorf("file = %q", d.File)
	}
}

// TestFacebookLeavesTitleEmpty pins a decision that is easy to reverse by
// accident. The export's title field is chrome, identical across thousands of
// records, and doc.Indexed prepends a title to every chunk before embedding --
// so using it would put the same boilerplate into every vector in the corpus.
func TestFacebookLeavesTitleEmpty(t *testing.T) {
	p := &facebookParser{}

	body := `[{"timestamp": 1600000000, "title": "A Writer shared a link.",
	  "data": [{"post": "Some commentary on the link."}]}]`

	docs, err := p.ParseFile(fbSource(), "your_posts_1.json", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if docs[0].Title != "" {
		t.Errorf("title = %q; the export's title field is chrome and must not become a document title", docs[0].Title)
	}
}

func TestFacebookSkipsRecordsWithNoText(t *testing.T) {
	p := &facebookParser{}

	body := `[
	  {"timestamp": 1, "title": "A Writer added a new photo.", "data": [{"update_timestamp": 1}, {}, {}]},
	  {"timestamp": 2, "title": "A Writer shared a link.", "data": [{}],
	   "attachments": [{"data": [{"external_context": {"url": "http://example.com"}}]}]},
	  {"timestamp": 3, "data": [{"post": "   "}]},
	  {"timestamp": 4, "data": [{"post": "Actual writing."}]}
	]`

	docs, err := p.ParseFile(fbSource(), "your_posts_1.json", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("parsed %d documents, want 1", len(docs))
	}

	seen, empty := p.Records()
	if seen != 4 {
		t.Errorf("records seen = %d, want 4", seen)
	}
	if empty != 3 {
		t.Errorf("records empty = %d, want 3", empty)
	}
}

// TestFacebookIncludesCaptions covers the case that recovers documents an
// earlier draft of this parser would have thrown away: a photo post whose only
// writing is the caption.
func TestFacebookIncludesCaptions(t *testing.T) {
	p := &facebookParser{}

	body := `[{"timestamp": 1600000000, "title": "A Writer added a new photo.",
	  "data": [{}],
	  "attachments": [{"data": [{"media": {"uri": "media/x.jpg", "description": "The caption is the only writing here."}}]}]}]`

	docs, err := p.ParseFile(fbSource(), "your_posts_1.json", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 {
		t.Fatalf("parsed %d documents, want 1", len(docs))
	}
	if !strings.Contains(docs[0].Text, "only writing here") {
		t.Errorf("caption was not indexed: %q", docs[0].Text)
	}
}

// TestFacebookDeduplicatesCaptions is the other half of that decision. Facebook
// copies a caption into both the post text and the media description, and on a
// real export 905 of 1,148 captions were byte-identical to their post. Appending
// blind would double every term frequency inside those documents.
func TestFacebookDeduplicatesCaptions(t *testing.T) {
	p := &facebookParser{}

	body := `[{"timestamp": 1600000000,
	  "data": [{"post": "The same words twice."}],
	  "attachments": [{"data": [{"media": {"description": "The same words twice."}}]}]}]`

	docs, err := p.ParseFile(fbSource(), "your_posts_1.json", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(docs[0].Text, "The same words twice."); got != 1 {
		t.Errorf("the caption appears %d times in the document text: %q", got, docs[0].Text)
	}
}

func TestFacebookKeepsCaptionThatAddsSomething(t *testing.T) {
	p := &facebookParser{}

	body := `[{"timestamp": 1600000000,
	  "data": [{"post": "The post says one thing."}],
	  "attachments": [{"data": [{"media": {"description": "The caption says another."}}]}]}]`

	docs, err := p.ParseFile(fbSource(), "your_posts_1.json", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"one thing", "another"} {
		if !strings.Contains(docs[0].Text, want) {
			t.Errorf("document text is missing %q: %q", want, docs[0].Text)
		}
	}
}

// TestFacebookIdentityUsesTimeAndText pins the measured decision. On a real
// export timestamps alone collided 327 times and text alone 31 times; together,
// twice.
func TestFacebookIdentityUsesTimeAndText(t *testing.T) {
	p := &facebookParser{}

	body := `[
	  {"timestamp": 1600000000, "data": [{"post": "First post at this second."}]},
	  {"timestamp": 1600000000, "data": [{"post": "Second post at the same second."}]},
	  {"timestamp": 1700000000, "data": [{"post": "First post at this second."}]}
	]`

	docs, err := p.ParseFile(fbSource(), "your_posts_1.json", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 3 {
		t.Fatalf("parsed %d documents, want 3", len(docs))
	}

	seen := map[string]bool{}
	for _, d := range docs {
		if seen[d.ID] {
			t.Errorf("duplicate ID %s; a shared timestamp or a repeated text collapsed two distinct posts", d.ID)
		}
		seen[d.ID] = true
	}
}

func TestFacebookIdentityIsStable(t *testing.T) {
	body := `[{"timestamp": 1600000000, "data": [{"post": "Something worth an identifier."}]}]`

	first, err := (&facebookParser{}).ParseFile(fbSource(), "your_posts_1.json", []byte(body))
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		again, err := (&facebookParser{}).ParseFile(fbSource(), "your_posts_1.json", []byte(body))
		if err != nil {
			t.Fatal(err)
		}
		if again[0].ID != first[0].ID {
			t.Fatalf("run %d produced a different ID from identical bytes", i)
		}
	}
}

func TestFacebookRejectsUnparseableJSON(t *testing.T) {
	p := &facebookParser{}

	if _, err := p.ParseFile(fbSource(), "your_posts_1.json", []byte("{not json")); err == nil {
		t.Error("malformed JSON was accepted")
	}
}

func TestFacebookImplementsRecordCounter(t *testing.T) {
	var p Parser = &facebookParser{}
	if _, ok := p.(RecordCounter); !ok {
		t.Error("the facebook parser should report record counts; without them a digest cannot tell a photo-heavy export from a broken parser")
	}
}

// --- encoding repair

// mangle reproduces Facebook's bug: it takes correct text, encodes it as UTF-8,
// then reads each of those bytes as a latin-1 character.
//
// Building the fixtures this way rather than pasting the mangled forms in keeps
// them readable and, more importantly, correct. Pasted mojibake is invisible to
// review and survives copying between editors only by luck.
func mangle(s string) string {
	var b strings.Builder
	for _, by := range []byte(s) {
		b.WriteRune(rune(by))
	}
	return b.String()
}

func TestRepairMojibake(t *testing.T) {
	want := []string{
		"didn’t",          // right single quote
		"a “repent” sign", // curly double quotes
		"exist—but",       // em dash
		"café",            // accented letter
		"nerd \U0001f913", // four-byte emoji
		"你好",              // outside latin-1 once decoded
	}

	for _, w := range want {
		in := mangle(w)
		if in == w {
			t.Fatalf("fixture %q did not mangle, so the test proves nothing", w)
		}
		if got := repairMojibake(in); got != w {
			t.Errorf("repairMojibake(%q) = %q, want %q", in, got, w)
		}
	}
}

// TestRepairMojibakeLeavesGoodTextAlone is the guard that matters most. A repair
// applied where nothing is broken is itself the bug, and the text it would
// corrupt is the text that was already correct.
func TestRepairMojibakeLeavesGoodTextAlone(t *testing.T) {
	unchanged := []string{
		"",
		"plain ascii text with punctuation, and numbers 123.",
		"already correct ’ curly quote",
		"already correct emoji \U0001f913",
		"café",         // a genuine latin-1 accent, not mojibake
		"naïve résumé", // two of them
		"你好",           // outside latin-1 entirely
	}

	for _, s := range unchanged {
		if got := repairMojibake(s); got != s {
			t.Errorf("repairMojibake(%q) = %q; correct text must pass through untouched", s, got)
		}
	}
}

func TestRepairMojibakeIsIdempotent(t *testing.T) {
	in := mangle("didn’t")

	once := repairMojibake(in)
	if twice := repairMojibake(once); twice != once {
		t.Errorf("repairing twice changed the text again: %q then %q", once, twice)
	}
}

// TestFacebookRepairsBeforeHashing matters because every document ID is derived
// from the text. Repairing after the fact would mean re-digesting the corpus.
func TestFacebookRepairsBeforeHashing(t *testing.T) {
	r := fbRecord{
		Timestamp: 1600000000,
		Data:      []fbData{{Post: mangle("didn’t")}},
	}

	if got := r.text(); got != "didn’t" {
		t.Errorf("record text = %q, want %q", got, "didn’t")
	}
}
