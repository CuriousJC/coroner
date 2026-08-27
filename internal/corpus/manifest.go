// Package corpus describes a body of source material: one export, sitting in
// one directory, with a manifest saying what it is.
//
// A source directory is the unit coroner digests. It holds whatever the export
// produced, untouched, plus a corpus.yaml naming the format and the corpus. The
// manifest exists so the digester never has to guess: sniffing file formats is
// where reproducibility goes to die, since the guess depends on which file the
// walk happened to reach first.
package corpus

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/curiousjc/coroner/internal/ignore"
)

// FileName is the manifest looked for in every source directory.
const FileName = "corpus.yaml"

// Manifest is a source directory's corpus.yaml. Only Type and Name are required.
type Manifest struct {
	// Type selects the parser. See the parse package for the registered ones.
	Type string `yaml:"type"`

	// Name is this corpus's stable identity. It is part of every document ID
	// and it names the files this corpus writes into the digested directory, so
	// renaming it is equivalent to re-digesting from scratch under a new corpus.
	//
	// Deliberately not derived from the directory name: moving or renaming a
	// folder is a thing people do casually, and it must not silently renumber
	// every document in the corpus.
	Name string `yaml:"name"`

	// Author and Title are provenance, copied onto documents when the export
	// itself does not carry them. A Facebook export knows who wrote it; a
	// directory of loose HTML usually does not.
	Author string `yaml:"author"`
	Title  string `yaml:"title"`

	// Notes is free text, for you. Coroner never reads it.
	Notes string `yaml:"notes"`

	// Priority ranks this corpus against the others when the same piece of
	// writing appears in several, higher winning. Only `coroner dupes` reads
	// it, and only to decide which copy of a group it names as the real one.
	//
	// A number rather than an ordered list of corpus names, because the rule
	// belongs beside the corpus it describes: a list would have to live
	// somewhere central and be kept in step with every source directory by
	// hand. Unset is zero, which loses to anything ranked -- so a corpus nobody
	// has thought about does not silently outrank one somebody has.
	Priority int `yaml:"priority"`

	// Include narrows which files the parser is offered, as globs matched
	// against the path relative to the source directory. Empty means the
	// parser's own default, which is usually right.
	Include []string `yaml:"include"`

	// Exclude prunes. Same syntax as hecato's ignore rules: a trailing "/"
	// skips a whole directory rather than filtering its contents one by one.
	Exclude []string `yaml:"exclude"`

	// Dir is where this manifest was read from. Not part of the file.
	Dir string `yaml:"-"`
}

// validName is the character set a corpus name may use.
//
// This is not fussiness. The name becomes a filename in the digested directory,
// so anything permitting a separator or a "." would let a manifest write outside
// the corpus it owns. Restricting rather than escaping keeps the digested
// listing readable, which is the other half of the point.
func validName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// Validate checks a manifest is usable, with knownTypes supplied by the caller
// so this package does not have to import the parser registry and create a
// cycle.
func (m *Manifest) Validate(knownTypes []string) error {
	if !validName(m.Name) {
		return fmt.Errorf("corpus name %q is not usable: use lowercase letters, digits, %q and %q, up to 64 characters", m.Name, "-", "_")
	}

	for _, t := range knownTypes {
		if m.Type == t {
			return nil
		}
	}

	sort.Strings(knownTypes)
	return fmt.Errorf("unknown source type %q; known types are %s", m.Type, strings.Join(knownTypes, ", "))
}

// Matcher builds the exclude matcher for this corpus, folding in the built-in
// noise list.
func (m *Manifest) Matcher() *ignore.Matcher {
	patterns := make([]string, 0, len(m.Exclude)+len(ignore.Noise))
	patterns = append(patterns, m.Exclude...)
	patterns = append(patterns, ignore.Noise...)
	return ignore.New(patterns)
}

// Load reads the manifest from a source directory.
func Load(dir string) (*Manifest, error) {
	path := filepath.Join(dir, FileName)

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("no %s in %s: every source directory needs one, run `coroner initsource -source=%s` to write a starter", FileName, dir, filepath.ToSlash(dir))
		}
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	var m Manifest
	dec := yaml.NewDecoder(f)

	// Reject unknown fields, same reasoning as hecato's config: a typo that
	// silently does nothing is worse than a startup failure. Here it is worse
	// still, because the typo would be in the description of a corpus you are
	// about to spend minutes embedding.
	dec.KnownFields(true)

	if err := dec.Decode(&m); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	m.Dir = dir
	return &m, nil
}

// Discover finds every source directory under root, meaning every directory
// that contains a corpus.yaml. Results are sorted, because the order corpora
// are digested in must not depend on the filesystem.
//
// It does not descend into a source directory once found: an export may well
// contain something called corpus.yaml of its own, and the outer manifest is
// the one that governs.
func Discover(root string) ([]string, error) {
	var out []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}

		if _, statErr := os.Stat(filepath.Join(path, FileName)); statErr == nil {
			out = append(out, path)
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Strings(out)
	return out, nil
}
