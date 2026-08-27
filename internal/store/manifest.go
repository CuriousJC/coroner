package store

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/curiousjc/coroner/internal/doc"
)

// ManifestName is the digested corpus's record of itself.
const ManifestName = "manifest.yaml"

// Manifest records what produced this digested corpus.
//
// It exists to make one specific failure loud. Embeddings are only comparable
// to other embeddings from the same model: vectors from nomic-embed-text and
// vectors from mxbai-embed-large occupy unrelated spaces, and searching a corpus
// that mixes them returns confident nonsense rather than an error. Nothing about
// the vectors themselves reveals which model made them, so the manifest is the
// only place that fact can live.
type Manifest struct {
	// CoronerVersion is the build that last wrote here.
	CoronerVersion string `yaml:"coroner_version"`

	Embed    EmbedInfo    `yaml:"embed"`
	Chunking ChunkingInfo `yaml:"chunking"`

	// Sources is a slice rather than a map so the file is written in a stable
	// order. Go randomises map iteration, and a manifest that reshuffles itself
	// on every digest is a manifest nobody can diff.
	Sources []SourceInfo `yaml:"sources"`

	// Path is where this was read from. Not part of the file.
	Path string `yaml:"-"`
}

// EmbedInfo pins the embedding model.
type EmbedInfo struct {
	Model string `yaml:"model"`

	// Digest is ollama's content hash for the model, when it could be read.
	// This is the part that catches a model being re-pulled and quietly
	// changing underneath a corpus -- the name stays the same, the digest does
	// not.
	Digest string `yaml:"digest,omitempty"`

	Dimensions int `yaml:"dimensions"`
}

// ChunkingInfo records the chunker's constants, since changing any of them
// changes every chunk in every corpus.
type ChunkingInfo struct {
	TargetChars int `yaml:"target_chars"`
	MaxChars    int `yaml:"max_chars"`
	MinChars    int `yaml:"min_chars"`
}

// CurrentChunking is the chunker as this build is compiled.
func CurrentChunking() ChunkingInfo {
	return ChunkingInfo{
		TargetChars: doc.TargetChars,
		MaxChars:    doc.MaxChars,
		MinChars:    doc.MinChars,
	}
}

// SourceInfo is one corpus's entry.
type SourceInfo struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`

	Documents int `yaml:"documents"`
	Chunks    int `yaml:"chunks"`

	// Priority is copied from the source manifest at digest time. It lives here
	// so `coroner dupes` can read the digested directory and nothing else: a
	// pass that also had to resolve and re-read every source manifest could
	// disagree with the corpus it is describing, and would fail on a digested
	// directory whose exports have since been moved.
	Priority int `yaml:"priority,omitempty"`

	// SourceDir is where the export was read from, for your reference. It is
	// provenance only; nothing resolves against it.
	SourceDir string `yaml:"source_dir,omitempty"`

	// DigestedAt is the one deliberately non-reproducible field in the whole
	// digested directory. See the package comment.
	DigestedAt time.Time `yaml:"digested_at"`

	CoronerVersion string `yaml:"coroner_version"`
}

// LoadManifest reads the manifest, returning a zero one when the digested
// directory is new. An empty digested directory is a normal starting state, not
// an error.
func (s *Store) LoadManifest() (*Manifest, error) {
	path := filepath.Join(s.Dir, ManifestName)

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &Manifest{}, nil
		}
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()

	var m Manifest
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)

	if err := dec.Decode(&m); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	m.Path = path
	return &m, nil
}

// SaveManifest writes the manifest, sorting sources so the file is stable.
func (s *Store) SaveManifest(m *Manifest) error {
	sort.Slice(m.Sources, func(i, j int) bool { return m.Sources[i].Name < m.Sources[j].Name })

	out, err := yaml.Marshal(m)
	if err != nil {
		return err
	}

	header := []byte("# coroner digested corpus\n" +
		"#\n" +
		"# Written by coroner. Editing it by hand will not change the data it\n" +
		"# describes, but it may well stop that data from loading.\n" +
		"#\n" +
		"# digested_at is the only field here that differs between two otherwise\n" +
		"# identical runs. The .jsonl and .vec files are byte-identical when the\n" +
		"# sources, the coroner version and the embedding model are unchanged.\n\n")

	path := filepath.Join(s.Dir, ManifestName)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(header, out...), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Source finds one corpus's entry.
func (m *Manifest) Source(name string) (SourceInfo, bool) {
	for _, s := range m.Sources {
		if s.Name == name {
			return s, true
		}
	}
	return SourceInfo{}, false
}

// SetSource inserts or replaces a corpus's entry.
func (m *Manifest) SetSource(info SourceInfo) {
	for i, s := range m.Sources {
		if s.Name == info.Name {
			m.Sources[i] = info
			return
		}
	}
	m.Sources = append(m.Sources, info)
}

// RemoveSource drops a corpus's entry.
func (m *Manifest) RemoveSource(name string) {
	for i, s := range m.Sources {
		if s.Name == name {
			m.Sources = append(m.Sources[:i], m.Sources[i+1:]...)
			return
		}
	}
}

// Empty reports whether anything has been digested here yet.
func (m *Manifest) Empty() bool { return len(m.Sources) == 0 }

// CheckEmbed verifies an incoming model against what this corpus was built
// with, and refuses rather than mixing vector spaces.
//
// This is a hard error by design, and the reason is the same one behind
// hecato's drive-relative target check: the failure it prevents is the kind that
// looks like success. Mixed vectors do not crash anything. They return a ranked
// list of plausible-looking results that is, for half the corpus, meaningless.
func (m *Manifest) CheckEmbed(model, digest string, dims int) error {
	if m.Empty() || m.Embed.Model == "" {
		return nil
	}

	if m.Embed.Model != model {
		return fmt.Errorf("this corpus was digested with %q but you asked for %q; embeddings from two models are not comparable, so re-digest every source or switch back", m.Embed.Model, model)
	}

	if dims != 0 && m.Embed.Dimensions != 0 && m.Embed.Dimensions != dims {
		return fmt.Errorf("this corpus holds %d-dimensional vectors but %q now returns %d; re-digest every source", m.Embed.Dimensions, model, dims)
	}

	if digest != "" && m.Embed.Digest != "" && m.Embed.Digest != digest {
		return fmt.Errorf("the model %q has changed since this corpus was digested (%s, now %s); its vectors are no longer comparable, so re-digest every source", model, short(m.Embed.Digest), short(digest))
	}

	return nil
}

// CheckChunking verifies the chunker has not changed shape under an existing
// corpus. A warning rather than an error is not enough here: chunk IDs encode
// the boundaries, so a corpus chunked under different constants cannot be added
// to incrementally.
func (m *Manifest) CheckChunking() error {
	if m.Empty() || m.Chunking == (ChunkingInfo{}) {
		return nil
	}

	if cur := CurrentChunking(); m.Chunking != cur {
		return fmt.Errorf("this corpus was chunked at target=%d max=%d min=%d but this build uses target=%d max=%d min=%d; re-digest every source",
			m.Chunking.TargetChars, m.Chunking.MaxChars, m.Chunking.MinChars,
			cur.TargetChars, cur.MaxChars, cur.MinChars)
	}
	return nil
}

func short(digest string) string {
	if len(digest) > 19 {
		return digest[:19]
	}
	return digest
}
