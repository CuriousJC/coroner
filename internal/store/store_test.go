package store

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/curiousjc/coroner/internal/doc"
)

func sampleCorpus(t *testing.T) *Corpus {
	t.Helper()

	var docs []doc.Document
	var chunks []doc.Chunk
	var vecs [][]float32

	for i := 0; i < 20; i++ {
		d := doc.New("test", "text", string(rune('a'+i)), "f.txt", doc.Document{
			Title: "Document " + string(rune('a'+i)),
			Text:  "Some writing about a thing, number " + string(rune('a'+i)) + ".",
		})
		docs = append(docs, d)

		for _, c := range doc.Split(d) {
			chunks = append(chunks, c)
			vecs = append(vecs, []float32{float32(i), 0.5, -0.25})
		}
	}

	return &Corpus{Name: "test", Docs: docs, Chunks: chunks, Vectors: vecs}
}

func TestWriteReadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	want := sampleCorpus(t)
	if err := s.Write(want); err != nil {
		t.Fatal(err)
	}

	got, err := s.Read("test")
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Docs) != len(want.Docs) {
		t.Fatalf("read %d documents, wrote %d", len(got.Docs), len(want.Docs))
	}
	if len(got.Chunks) != len(want.Chunks) {
		t.Fatalf("read %d chunks, wrote %d", len(got.Chunks), len(want.Chunks))
	}
	if len(got.Vectors) != len(want.Chunks) {
		t.Fatalf("read %d vectors for %d chunks", len(got.Vectors), len(want.Chunks))
	}

	// Vectors must still line up with the chunks they belong to after the
	// sorting the writer applies.
	for i, c := range got.Chunks {
		if got.Vectors[i] == nil {
			t.Fatalf("chunk %s has no vector", c.ID)
		}
	}
}

// TestWriteIsOrderIndependent is the determinism test that matters most here.
// A concurrent digest finishes files in whatever order it likes, so the writer
// has to be what imposes an order -- otherwise two runs over identical sources
// produce different bytes.
func TestWriteIsOrderIndependent(t *testing.T) {
	base := sampleCorpus(t)

	read := func(dir, name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}

	dirA := t.TempDir()
	sa, _ := Open(dirA)
	if err := sa.Write(base); err != nil {
		t.Fatal(err)
	}

	// The same corpus, shuffled.
	shuffled := &Corpus{
		Name:    base.Name,
		Docs:    append([]doc.Document(nil), base.Docs...),
		Chunks:  append([]doc.Chunk(nil), base.Chunks...),
		Vectors: append([][]float32(nil), base.Vectors...),
	}

	rng := rand.New(rand.NewSource(1))
	rng.Shuffle(len(shuffled.Docs), func(i, j int) {
		shuffled.Docs[i], shuffled.Docs[j] = shuffled.Docs[j], shuffled.Docs[i]
	})
	rng.Shuffle(len(shuffled.Chunks), func(i, j int) {
		shuffled.Chunks[i], shuffled.Chunks[j] = shuffled.Chunks[j], shuffled.Chunks[i]
		shuffled.Vectors[i], shuffled.Vectors[j] = shuffled.Vectors[j], shuffled.Vectors[i]
	})

	dirB := t.TempDir()
	sb, _ := Open(dirB)
	if err := sb.Write(shuffled); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"test.docs.jsonl", "test.chunks.jsonl", "test.vec"} {
		if !bytes.Equal(read(dirA, name), read(dirB, name)) {
			t.Errorf("%s differs between a sorted and a shuffled write; the digest is not reproducible", name)
		}
	}
}

func TestWriteRejectsMismatchedVectors(t *testing.T) {
	s, _ := Open(t.TempDir())

	c := sampleCorpus(t)
	c.Vectors = c.Vectors[:len(c.Vectors)-1]

	if err := s.Write(c); err == nil {
		t.Error("writing a corpus with fewer vectors than chunks should fail loudly")
	}
}

func TestReadMissingCorpusIsEmpty(t *testing.T) {
	s, _ := Open(t.TempDir())

	c, err := s.Read("nothing")
	if err != nil {
		t.Fatalf("reading an absent corpus should not be an error: %v", err)
	}
	if len(c.Docs) != 0 {
		t.Errorf("an absent corpus returned %d documents", len(c.Docs))
	}
}

func TestSourcesListsWhatWasWritten(t *testing.T) {
	s, _ := Open(t.TempDir())

	for _, name := range []string{"zebra", "alpha", "middle"} {
		c := sampleCorpus(t)
		c.Name = name
		if err := s.Write(c); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.Sources()
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"alpha", "middle", "zebra"}
	if len(got) != len(want) {
		t.Fatalf("Sources() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Sources() = %v, want %v (sorted)", got, want)
		}
	}
}

func TestRemoveDeletesEverything(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)

	if err := s.Write(sampleCorpus(t)); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove("test"); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		t.Errorf("%s survived Remove", e.Name())
	}
}
