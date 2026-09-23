package corpus

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Starter renders the corpus.yaml that `coroner initsource` writes.
//
// corpus.example.yaml at the repo root is a committed copy of this output for
// the "html" type, and TestExampleManifestMatchesGenerator is the only thing
// keeping the two in sync. Change this and regenerate the example, or that test
// fails with the command to do it.
func Starter(name, sourceType string) string {
	var b strings.Builder

	b.WriteString("# coroner corpus manifest\n")
	b.WriteString("#\n")
	b.WriteString("# This file says what the export sitting beside it is. Coroner never guesses\n")
	b.WriteString("# a format: the guess would depend on which file the walk reached first, and\n")
	b.WriteString("# a digest that depends on walk order is not reproducible.\n")
	b.WriteString("\n")
	b.WriteString("# Which parser reads this export.\n")
	b.WriteString("#   facebook   a Facebook \"Download your information\" export\n")
	b.WriteString("#   substack   a Substack export: posts.csv plus posts/*.html\n")
	b.WriteString("#   goodreads  a Goodreads library export: the reviews in its CSV\n")
	b.WriteString("#   html       loose HTML files\n")
	b.WriteString("#   htmlsite   a hand-built HTML site: <h1> title, dated filenames\n")
	b.WriteString("#   text       loose .txt or .md files\n")
	fmt.Fprintf(&b, "type: %s\n", sourceType)
	b.WriteString("\n")
	b.WriteString("# This corpus's stable identity. It is part of every document ID and it names\n")
	b.WriteString("# the files this corpus writes into the digested directory, so changing it\n")
	b.WriteString("# means re-digesting from scratch. Lowercase letters, digits, - and _.\n")
	fmt.Fprintf(&b, "name: %s\n", name)
	b.WriteString("\n")
	b.WriteString("# Rank this corpus against the others when the same piece of writing turns\n")
	b.WriteString("# up in more than one of them. Higher wins. Only `coroner dupes` reads it,\n")
	b.WriteString("# and only to say which copy it treats as the real one -- nothing is hidden\n")
	b.WriteString("# from search either way. Unset is 0, which loses to anything ranked.\n")
	b.WriteString("# priority: 10\n")
	b.WriteString("\n")
	b.WriteString("# Provenance, used when the export itself does not carry it.\n")
	b.WriteString("# author: \"Your Name\"\n")
	b.WriteString("# title: \"What this body of work is\"\n")
	b.WriteString("# notes: \"Anything you want to remember about this export. Coroner ignores it.\"\n")
	b.WriteString("\n")
	b.WriteString("# Narrow which files the parser is offered, as globs relative to this\n")
	b.WriteString("# directory. Empty means the parser's own default, which is usually right.\n")
	b.WriteString("# include:\n")
	b.WriteString("#   - \"posts/**/*.html\"\n")
	b.WriteString("\n")
	b.WriteString("# Prune what you do not want walked at all. A trailing / skips the whole\n")
	b.WriteString("# directory rather than filtering its contents one file at a time; on an\n")
	b.WriteString("# export whose media folder holds 40,000 images, that is the difference\n")
	b.WriteString("# that matters. Images, video and web scaffolding are excluded already.\n")
	b.WriteString("# exclude:\n")
	b.WriteString("#   - \"comments/\"\n")
	b.WriteString("#   - \"*.json.bak\"\n")

	return b.String()
}

// WriteStarter creates a source directory and its manifest. It refuses to
// overwrite an existing manifest: pointing initsource at a corpus you have
// already described should not quietly discard how you described it.
func WriteStarter(dir, name, sourceType string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	path := filepath.Join(dir, FileName)
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; delete it first if you meant to start over", path)
	}

	return os.WriteFile(path, []byte(Starter(name, sourceType)), 0644)
}

// SuggestName turns a directory name into a usable corpus name, so initsource
// has something to default to.
func SuggestName(dir string) string {
	base := strings.ToLower(filepath.Base(filepath.Clean(dir)))

	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('-')
		}
	}

	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "corpus"
	}
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
