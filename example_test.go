// Package coroner_test holds the checks that keep the committed example files in
// step with the code that generates them.
//
// The examples exist so that someone reading this repo can see what a config and
// a manifest look like without running anything. They are only useful if they are
// accurate, and the only thing that can keep them accurate is a test that fails
// when they drift.
package coroner_test

import (
	"os"
	"testing"

	"github.com/curiousjc/coroner/internal/config"
	"github.com/curiousjc/coroner/internal/corpus"
)

const (
	exampleConfig   = "coroner.example.yaml"
	exampleManifest = "corpus.example.yaml"
)

func TestExampleConfigMatchesGenerator(t *testing.T) {
	want := config.Starter()

	got, err := os.ReadFile(exampleConfig)
	if err != nil {
		t.Fatalf("%v\n\nRegenerate it: see the note in this test.", err)
	}

	if string(got) != want {
		t.Errorf("%s is out of step with config.Starter().\n\n"+
			"Regenerate it with a one-off program that writes config.Starter() to that\n"+
			"path, or paste the generator's output over it. The example is committed so\n"+
			"that someone reading the repo can see what a config looks like; an example\n"+
			"that has drifted is worse than none.", exampleConfig)
	}
}

func TestExampleManifestMatchesGenerator(t *testing.T) {
	// The committed example is the "html" form, since that is the source type
	// someone can use without waiting on a parser to be written.
	want := corpus.Starter("example", "html")

	got, err := os.ReadFile(exampleManifest)
	if err != nil {
		t.Fatalf("%v\n\nRegenerate it: see the note in this test.", err)
	}

	if string(got) != want {
		t.Errorf("%s is out of step with corpus.Starter(\"example\", \"html\").\n\n"+
			"Regenerate it the same way as %s.", exampleManifest, exampleConfig)
	}
}

// TestExampleManifestIsValid checks the committed example would actually work,
// rather than merely matching a generator that is itself wrong.
func TestExampleManifestIsValid(t *testing.T) {
	dir := t.TempDir()

	body, err := os.ReadFile(exampleManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir+"/"+corpus.FileName, body, 0644); err != nil {
		t.Fatal(err)
	}

	m, err := corpus.Load(dir)
	if err != nil {
		t.Fatalf("the committed example manifest does not load: %v", err)
	}
	if err := m.Validate([]string{"facebook", "html", "substack", "text"}); err != nil {
		t.Errorf("the committed example manifest does not validate: %v", err)
	}
}
