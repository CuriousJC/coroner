// Package store is the digested corpus on disk.
//
// The layout, all inside the digested directory:
//
//	manifest.yaml        what built this, and with which model
//	<corpus>.docs.jsonl  one Document per line, sorted by ID
//	<corpus>.chunks.jsonl one Chunk per line, sorted by document then index
//	<corpus>.vec         chunk vectors, in the same order as the chunks file
//
// One set of files per corpus, which is what makes digesting incremental: adding
// a Substack export must not re-embed a Facebook one, and embedding is the only
// slow step in the whole tool.
//
// Everything except manifest.yaml is byte-identical across re-digests of
// unchanged sources. The manifest is not, and is not meant to be: it records
// when a corpus was built, which is precisely the fact that differs between two
// otherwise identical runs. Keeping that in one file rather than sprinkling
// timestamps through the data is what lets you diff two digested directories and
// see only what actually changed.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/curiousjc/coroner/internal/doc"
)

// Store is a digested directory.
type Store struct {
	Dir string
}

// Open prepares a digested directory, creating it if necessary.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating digested directory %s: %w", dir, err)
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) docsPath(name string) string   { return filepath.Join(s.Dir, name+".docs.jsonl") }
func (s *Store) chunksPath(name string) string { return filepath.Join(s.Dir, name+".chunks.jsonl") }
func (s *Store) vecPath(name string) string    { return filepath.Join(s.Dir, name+".vec") }

// Corpus is one source's digested content, held in memory.
type Corpus struct {
	Name    string
	Docs    []doc.Document
	Chunks  []doc.Chunk
	Vectors [][]float32
}

// Sources lists the corpora present, sorted.
func (s *Store) Sources() ([]string, error) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var out []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".docs.jsonl"); ok {
			out = append(out, name)
		}
	}

	sort.Strings(out)
	return out, nil
}

// Write replaces one corpus's files.
//
// Documents and chunks are sorted here rather than trusted from the caller,
// because the sort is what makes the output independent of the order a
// concurrent digest happened to finish in. Vectors are permuted to match.
//
// Each file is written to a temporary name and renamed into place, so an
// interrupted digest leaves the previous corpus intact rather than a truncated
// one that would fail to load on the next search.
func (s *Store) Write(c *Corpus) error {
	if len(c.Vectors) != 0 && len(c.Vectors) != len(c.Chunks) {
		return fmt.Errorf("corpus %s has %d chunks but %d vectors", c.Name, len(c.Chunks), len(c.Vectors))
	}

	docs := make([]doc.Document, len(c.Docs))
	copy(docs, c.Docs)
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })

	order := make([]int, len(c.Chunks))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		x, y := c.Chunks[order[a]], c.Chunks[order[b]]
		if x.DocID != y.DocID {
			return x.DocID < y.DocID
		}
		return x.Index < y.Index
	})

	chunks := make([]doc.Chunk, len(order))
	var vecs [][]float32
	if len(c.Vectors) > 0 {
		vecs = make([][]float32, len(order))
	}
	for i, from := range order {
		chunks[i] = c.Chunks[from]
		if vecs != nil {
			vecs[i] = c.Vectors[from]
		}
	}

	if err := writeJSONL(s.docsPath(c.Name), len(docs), func(i int) any { return docs[i] }); err != nil {
		return err
	}
	if err := writeJSONL(s.chunksPath(c.Name), len(chunks), func(i int) any { return chunks[i] }); err != nil {
		return err
	}

	tmp := s.vecPath(c.Name) + ".tmp"
	if err := WriteVectors(tmp, vecs); err != nil {
		return err
	}
	return os.Rename(tmp, s.vecPath(c.Name))
}

// Read loads one corpus.
func (s *Store) Read(name string) (*Corpus, error) {
	c := &Corpus{Name: name}

	if err := readJSONL(s.docsPath(name), func(dec *json.Decoder) error {
		var d doc.Document
		if err := dec.Decode(&d); err != nil {
			return err
		}
		c.Docs = append(c.Docs, d)
		return nil
	}); err != nil {
		return nil, err
	}

	if err := readJSONL(s.chunksPath(name), func(dec *json.Decoder) error {
		var ch doc.Chunk
		if err := dec.Decode(&ch); err != nil {
			return err
		}
		c.Chunks = append(c.Chunks, ch)
		return nil
	}); err != nil {
		return nil, err
	}

	vecs, err := ReadVectors(s.vecPath(name))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	c.Vectors = vecs

	if len(c.Vectors) > 0 && len(c.Vectors) != len(c.Chunks) {
		return nil, fmt.Errorf("corpus %s is inconsistent: %d chunks but %d vectors; re-digest it", name, len(c.Chunks), len(c.Vectors))
	}

	return c, nil
}

// ReadAll loads every corpus, in sorted name order.
//
// Brute force, and appropriate here. At the scale this tool targets -- under ten
// thousand documents, so tens of thousands of chunks -- the whole corpus is tens
// of megabytes and loads in well under a second. An approximate-nearest-neighbour
// index would add a persisted structure that can drift out of step with the data
// it indexes, to solve a problem this corpus does not have.
func (s *Store) ReadAll() ([]*Corpus, error) {
	names, err := s.Sources()
	if err != nil {
		return nil, err
	}

	out := make([]*Corpus, 0, len(names))
	for _, n := range names {
		c, err := s.Read(n)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// Remove deletes one corpus's files.
func (s *Store) Remove(name string) error {
	for _, p := range []string{s.docsPath(name), s.chunksPath(name), s.vecPath(name)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// writeJSONL writes n records, one compact JSON object per line.
func writeJSONL(path string, n int, at func(int) any) error {
	tmp := path + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("creating %s: %w", tmp, err)
	}

	w := bufio.NewWriterSize(f, 1<<16)
	enc := json.NewEncoder(w)

	// No HTML escaping. The default turns < > & into < and friends, which
	// is meaningless outside a browser and makes the corpus far less greppable
	// -- the one property this format was chosen for.
	enc.SetEscapeHTML(false)

	for i := 0; i < n; i++ {
		if err := enc.Encode(at(i)); err != nil {
			f.Close()
			return fmt.Errorf("writing %s: %w", tmp, err)
		}
	}

	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

// readJSONL streams a JSONL file through decode.
func readJSONL(path string, decode func(*json.Decoder) error) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	defer f.Close()

	// A single essay can exceed bufio.Scanner's default token size, so this
	// decodes the stream rather than scanning lines.
	dec := json.NewDecoder(bufio.NewReaderSize(f, 1<<16))
	for dec.More() {
		if err := decode(dec); err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
	}
	return nil
}
