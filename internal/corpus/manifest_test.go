package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var knownTypes = []string{"facebook", "html", "substack", "text"}

func TestValidateAcceptsAGoodManifest(t *testing.T) {
	m := &Manifest{Type: "html", Name: "my-corpus_2"}
	if err := m.Validate(knownTypes); err != nil {
		t.Errorf("a valid manifest was rejected: %v", err)
	}
}

// TestValidateRejectsUnsafeNames matters more than it looks: the name becomes a
// filename in the digested directory, so a separator in it would let a manifest
// write outside the corpus it owns.
func TestValidateRejectsUnsafeNames(t *testing.T) {
	bad := []string{
		"",
		"Has Capitals",
		"has spaces",
		"has/slash",
		`has\backslash`,
		"..",
		"../escape",
		"has.dot",
		strings.Repeat("x", 65),
	}

	for _, name := range bad {
		m := &Manifest{Type: "html", Name: name}
		if err := m.Validate(knownTypes); err == nil {
			t.Errorf("the name %q was accepted", name)
		}
	}
}

func TestValidateRejectsUnknownType(t *testing.T) {
	m := &Manifest{Type: "nonsense", Name: "ok"}

	err := m.Validate(knownTypes)
	if err == nil {
		t.Fatal("an unknown type was accepted")
	}
	if !strings.Contains(err.Error(), "html") {
		t.Errorf("the error does not list what the known types are: %v", err)
	}
}

func TestLoadReadsAManifest(t *testing.T) {
	dir := t.TempDir()
	body := "type: html\nname: mine\nauthor: A Writer\nexclude:\n  - \"drafts/\"\n"

	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}

	if m.Type != "html" || m.Name != "mine" || m.Author != "A Writer" {
		t.Errorf("loaded %+v", m)
	}
	if m.Dir != dir {
		t.Errorf("Dir = %q, want %q", m.Dir, dir)
	}
	if !m.Matcher().MatchDir("drafts") {
		t.Error("the exclude pattern did not reach the matcher")
	}
}

// TestLoadRejectsUnknownFields guards the KnownFields decision: a typo in a
// manifest must not silently do nothing before a long embedding run.
func TestLoadRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	body := "type: html\nname: mine\nexcludes:\n  - \"drafts/\"\n" // "excludes", not "exclude"

	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(dir); err == nil {
		t.Error("a misspelled field was accepted")
	}
}

func TestLoadMissingManifestExplainsItself(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("a directory with no manifest loaded successfully")
	}
	if !strings.Contains(err.Error(), "initsource") {
		t.Errorf("the error does not say how to fix it: %v", err)
	}
}

func TestMatcherFoldsInTheNoiseList(t *testing.T) {
	m := &Manifest{Type: "html", Name: "mine"}
	ig := m.Matcher()

	if !ig.MatchFile("some/path/photo.jpg") {
		t.Error("images are not being excluded by default")
	}
	if ig.MatchFile("some/path/post.html") {
		t.Error("HTML is being excluded by default")
	}
}

func TestDiscoverFindsSourceDirectories(t *testing.T) {
	root := t.TempDir()

	for _, name := range []string{"zebra", "alpha"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte("type: html\nname: "+name+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// A directory with no manifest is not a corpus.
	if err := os.MkdirAll(filepath.Join(root, "not-a-corpus"), 0755); err != nil {
		t.Fatal(err)
	}

	// A manifest inside an export must not be mistaken for an outer one.
	inner := filepath.Join(root, "alpha", "export", "nested")
	if err := os.MkdirAll(inner, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, FileName), []byte("type: html\nname: nested\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(got) != 2 {
		t.Fatalf("found %d corpora, expected 2: %v", len(got), got)
	}
	if filepath.Base(got[0]) != "alpha" || filepath.Base(got[1]) != "zebra" {
		t.Errorf("Discover did not return sorted results: %v", got)
	}
}

func TestSuggestName(t *testing.T) {
	tests := map[string]string{
		"source/substack":    "substack",
		"source/My Facebook": "my-facebook",
		"source/dump.v2":     "dump-v2",
		"source/!!!":         "corpus",
	}

	for in, want := range tests {
		if got := SuggestName(in); got != want {
			t.Errorf("SuggestName(%q) = %q, want %q", in, got, want)
		}
		if !validName(SuggestName(in)) {
			t.Errorf("SuggestName(%q) produced an invalid name: %q", in, SuggestName(in))
		}
	}
}

func TestWriteStarterRefusesToOverwrite(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "corpus")

	if err := WriteStarter(dir, "mine", "html"); err != nil {
		t.Fatal(err)
	}
	if err := WriteStarter(dir, "other", "text"); err == nil {
		t.Error("initsource overwrote an existing manifest")
	}

	// What it wrote must load and validate.
	m, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(knownTypes); err != nil {
		t.Errorf("the generated manifest does not validate: %v", err)
	}
	if m.Name != "mine" || m.Type != "html" {
		t.Errorf("generated manifest has name %q type %q", m.Name, m.Type)
	}
}
