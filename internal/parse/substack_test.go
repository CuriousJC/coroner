package parse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/curiousjc/coroner/internal/doc"
)

// substackExport writes a miniature export with the same shape as the real one:
// posts.csv at the root, bodies under posts/, and the analytics files that must
// never be read sitting beside them.
func substackExport(t *testing.T, rows string, bodies map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "posts"), 0o755); err != nil {
		t.Fatal(err)
	}

	header := "post_id,post_date,is_published,email_sent_at,inbox_sent_at,type,audience,title,subtitle,podcast_url\n"
	if err := os.WriteFile(filepath.Join(dir, "posts.csv"), []byte(header+rows), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, body := range bodies {
		if err := os.WriteFile(filepath.Join(dir, "posts", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func substackParse(t *testing.T, dir, rel string) []doc.Document {
	t.Helper()

	p := &substackParser{}
	src := Source{Dir: dir, Name: "substack", Type: "substack", Author: "Justin"}

	if err := p.Prepare(src); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, rel))
	if err != nil {
		t.Fatal(err)
	}

	docs, err := p.ParseFile(src, rel, data)
	if err != nil {
		t.Fatalf("ParseFile(%q): %v", rel, err)
	}
	return docs
}

const substackRow = "173688001.oppenheimer,2023-08-15T18:41:00.000Z,true,,,newsletter,everyone,Oppenheimer,Great man theory,\n"

const substackBody = `<p>Finally watched Oppenheimer.</p><p>Next is Barbie I guess.</p>`

func TestSubstackTakesMetadataFromSidecar(t *testing.T) {
	dir := substackExport(t, substackRow, map[string]string{
		"173688001.oppenheimer.html": substackBody,
	})

	got := substackParse(t, dir, "posts/173688001.oppenheimer.html")
	if len(got) != 1 {
		t.Fatalf("want 1 document, got %d", len(got))
	}

	if got[0].Title != "Oppenheimer" {
		t.Errorf("title = %q, want %q", got[0].Title, "Oppenheimer")
	}
	want := time.Date(2023, 8, 15, 18, 41, 0, 0, time.UTC)
	if !got[0].Published.Equal(want) {
		t.Errorf("published = %s, want %s", got[0].Published, want)
	}
	if !strings.Contains(got[0].Text, "Finally watched Oppenheimer.") {
		t.Errorf("body missing: %q", got[0].Text)
	}
}

// post_id is a real native key, which is the whole difference from Facebook --
// identity there had to be synthesised from a timestamp and a text hash. An ID
// derived from post_id survives an edit to the post's text.
func TestSubstackUsesPostIDAsNativeKey(t *testing.T) {
	dir := substackExport(t, substackRow, map[string]string{
		"173688001.oppenheimer.html": substackBody,
	})
	first := substackParse(t, dir, "posts/173688001.oppenheimer.html")[0]

	edited := substackExport(t, substackRow, map[string]string{
		"173688001.oppenheimer.html": `<p>Finally watched Oppenheimer, and then rewrote this.</p>`,
	})
	second := substackParse(t, edited, "posts/173688001.oppenheimer.html")[0]

	if first.NativeKey != "173688001.oppenheimer" {
		t.Errorf("native key = %q", first.NativeKey)
	}
	if first.ID != second.ID {
		t.Errorf("editing the text changed the ID: %s then %s", first.ID, second.ID)
	}
	if first.ContentHash == second.ContentHash {
		t.Error("editing the text left the content hash unchanged")
	}
}

// Measured: 117 of 139 posts carry a subtitle, they are distinct per post, and
// only one already appears in its own body. It is writing that exists nowhere
// else, so it is kept -- but in the text, not folded into the title, which
// doc.Indexed repeats across every chunk.
func TestSubstackKeepsSubtitleInTextNotTitle(t *testing.T) {
	dir := substackExport(t, substackRow, map[string]string{
		"173688001.oppenheimer.html": substackBody,
	})

	got := substackParse(t, dir, "posts/173688001.oppenheimer.html")[0]

	if !strings.Contains(got.Text, "Great man theory") {
		t.Errorf("subtitle dropped from the text: %q", got.Text)
	}
	if strings.Contains(got.Title, "Great man theory") {
		t.Errorf("subtitle folded into the title: %q", got.Title)
	}
}

// Only published posts are corpus. The five drafts in the real export are
// exactly the five posts with no title and no date, and the published flag is
// the author's own record of what counts as finished -- so it is taken at its
// word rather than second-guessed by reading the bodies.
func TestSubstackSkipsUnpublishedDrafts(t *testing.T) {
	row := "203694737.slop,,false,,,newsletter,everyone,,,\n"
	dir := substackExport(t, row, map[string]string{
		"203694737.slop.html": `<p>Thinking about creativity in the age of AI.</p>`,
	})

	if got := substackParse(t, dir, "posts/203694737.slop.html"); len(got) != 0 {
		t.Errorf("a draft produced %d documents, want 0: %+v", len(got), got)
	}
}

// A published post with no title should still be findable by something. Nothing
// in the current export takes this path -- every untitled post is a draft -- but
// an empty title is worse than a slug if one ever appears.
func TestSubstackUntitledPublishedPostFallsBackToSlug(t *testing.T) {
	row := "203694737.slop,2025-01-02T00:00:00.000Z,true,,,newsletter,everyone,,,\n"
	dir := substackExport(t, row, map[string]string{
		"203694737.slop.html": `<p>Thinking about creativity in the age of AI.</p>`,
	})

	got := substackParse(t, dir, "posts/203694737.slop.html")
	if len(got) != 1 {
		t.Fatalf("want 1 document, got %d", len(got))
	}
	if got[0].Title != "slop" {
		t.Errorf("title = %q, want %q", got[0].Title, "slop")
	}
}

// posts/ holds 218 analytics files carrying subscriber addresses. The glob is
// what keeps them out, so it is worth asserting rather than assuming.
func TestSubstackIncludeIsANarrowAllowlist(t *testing.T) {
	got := (&substackParser{}).Include()

	if len(got) != 1 || got[0] != "posts/*.html" {
		t.Fatalf("Include() = %v, want [posts/*.html]", got)
	}
	for _, bad := range []string{"*.csv", "posts/*.csv", "*"} {
		for _, g := range got {
			if g == bad {
				t.Errorf("Include() would read %q", bad)
			}
		}
	}
}

// A body with no row has no title and no date, and inventing them would be worse
// than saying so. The real export matches 139 to 139, so this should never fire
// -- which is exactly why it should be loud if it ever does.
func TestSubstackUnknownPostIsAnError(t *testing.T) {
	dir := substackExport(t, substackRow, map[string]string{
		"999999999.mystery.html": substackBody,
	})

	p := &substackParser{}
	src := Source{Dir: dir, Name: "substack", Type: "substack"}
	if err := p.Prepare(src); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "posts", "999999999.mystery.html"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := p.ParseFile(src, "posts/999999999.mystery.html", data); err == nil {
		t.Error("a body with no posts.csv row parsed without complaint")
	}
}

func TestSubstackPrepareRejectsAMissingSidecar(t *testing.T) {
	err := (&substackParser{}).Prepare(Source{Dir: t.TempDir()})
	if err == nil {
		t.Fatal("Prepare succeeded with no posts.csv")
	}
	if !strings.Contains(err.Error(), "posts.csv") {
		t.Errorf("the error does not name the missing file: %v", err)
	}
}

// Substack's chrome is share buttons and inline SVG icons -- 374 of each across
// the export. Left in, they inject identical boilerplate into every document.
func TestSubstackStripsWidgetChrome(t *testing.T) {
	body := `<p>Real writing here.</p>` +
		`<button aria-label="Share"><svg><polyline points="1,2"></polyline></svg>Share</button>` +
		`<div class="subscribe-widget"><button>Subscribe now</button></div>`

	dir := substackExport(t, substackRow, map[string]string{
		"173688001.oppenheimer.html": body,
	})

	got := substackParse(t, dir, "posts/173688001.oppenheimer.html")[0]

	if strings.Contains(got.Text, "Share") {
		t.Errorf("button text survived: %q", got.Text)
	}
	if strings.Contains(got.Text, "Subscribe now") {
		t.Errorf("subscribe widget survived: %q", got.Text)
	}
	if !strings.Contains(got.Text, "Real writing here.") {
		t.Errorf("body text lost: %q", got.Text)
	}
}

// Unlike Facebook, where 905 of 1,148 captions duplicated their post, only 2 of
// 94 Substack captions repeat body text. They are kept whole.
func TestSubstackKeepsFigcaptions(t *testing.T) {
	body := `<p>Real writing here.</p><figure><figcaption>A caption worth reading.</figcaption></figure>`

	dir := substackExport(t, substackRow, map[string]string{
		"173688001.oppenheimer.html": body,
	})

	got := substackParse(t, dir, "posts/173688001.oppenheimer.html")[0]
	if !strings.Contains(got.Text, "A caption worth reading.") {
		t.Errorf("figcaption dropped: %q", got.Text)
	}
}
