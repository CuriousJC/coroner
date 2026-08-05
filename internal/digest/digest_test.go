package digest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/curiousjc/coroner/internal/store"
)

// stubEmbedder derives a vector from the text's hash.
//
// Deterministic by construction, which is what lets this file test the claim
// the whole tool rests on. A real model is deterministic in practice but cannot
// promise it across an ollama upgrade, so proving reproducibility with one would
// be proving the wrong thing: what is being tested here is that *coroner's* half
// of the pipeline contributes no variation.
type stubEmbedder struct {
	dims  int
	calls int
}

func (s *stubEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	s.calls += len(texts)

	out := make([][]float32, len(texts))
	for i, t := range texts {
		sum := sha256.Sum256([]byte(t))
		v := make([]float32, s.dims)
		for j := range v {
			// Two bytes of the hash per dimension, wrapped, mapped to [-1, 1).
			at := (j * 2) % (len(sum) - 1)
			v[j] = float32(int16(binary.LittleEndian.Uint16(sum[at:]))) / 32768
		}
		out[i] = v
	}
	return out, nil
}

// writeSource builds a source directory holding a manifest and some documents.
func writeSource(t *testing.T, name string, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()

	manifest := fmt.Sprintf("type: text\nname: %s\nauthor: A Writer\n", name)
	if err := os.WriteFile(filepath.Join(dir, "corpus.yaml"), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	for rel, body := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func sampleFiles() map[string]string {
	files := map[string]string{}
	for i := 0; i < 25; i++ {
		files[fmt.Sprintf("posts/post-%02d.md", i)] = fmt.Sprintf(
			"# Post number %d\n\nSome writing about choice, and about the weight of choosing.\n\n"+
				"A second paragraph, so that the chunker has something to divide.\n\n"+
				"A third, mentioning decisions and branching and the shape of a fork in the road.\n", i)
	}
	// A nested directory and a file the text parser should not be offered.
	files["notes/deep/thought.txt"] = "A loose note about tides.\n"
	files["assets/logo.png"] = "not really a png, but it must not be parsed"
	return files
}

func run(t *testing.T, sourceDir, digestedDir string, workers int) *Report {
	t.Helper()

	st, err := store.Open(digestedDir)
	if err != nil {
		t.Fatal(err)
	}

	rep, err := Run(context.Background(), Options{
		SourceDir: sourceDir,
		Store:     st,
		Embedder:  &stubEmbedder{dims: 16},
		ModelName: "stub",
		Workers:   workers,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func corpusFiles(t *testing.T, dir, name string) map[string][]byte {
	t.Helper()

	out := map[string][]byte{}
	for _, suffix := range []string{".docs.jsonl", ".chunks.jsonl", ".vec"} {
		b, err := os.ReadFile(filepath.Join(dir, name+suffix))
		if err != nil {
			t.Fatal(err)
		}
		out[suffix] = b
	}
	return out
}

// TestDigestIsReproducible is the test this project exists to be able to pass.
// Two runs over the same source, at different concurrency, must produce
// byte-identical corpus files.
func TestDigestIsReproducible(t *testing.T) {
	src := writeSource(t, "sample", sampleFiles())

	dirA := t.TempDir()
	dirB := t.TempDir()

	// Different worker counts on purpose: if anything downstream depended on
	// the order files finished parsing, this is where it would show.
	repA := run(t, src, dirA, 1)
	repB := run(t, src, dirB, 8)

	if repA.Documents != repB.Documents || repA.Chunks != repB.Chunks {
		t.Fatalf("run A produced %d docs / %d chunks, run B produced %d / %d",
			repA.Documents, repA.Chunks, repB.Documents, repB.Chunks)
	}

	a := corpusFiles(t, dirA, "sample")
	b := corpusFiles(t, dirB, "sample")

	for suffix := range a {
		if !bytes.Equal(a[suffix], b[suffix]) {
			t.Errorf("%s differs between two digests of identical sources", suffix)
		}
	}
}

// TestDigestReusesUnchangedVectors is what makes adding a second export cheap:
// re-digesting must not re-embed anything whose text has not moved.
func TestDigestReusesUnchangedVectors(t *testing.T) {
	src := writeSource(t, "sample", sampleFiles())
	digested := t.TempDir()

	first := run(t, src, digested, 4)
	if first.Embedded == 0 {
		t.Fatal("the first digest embedded nothing")
	}
	if first.Reused != 0 {
		t.Errorf("the first digest reused %d vectors, with nothing to reuse from", first.Reused)
	}

	second := run(t, src, digested, 4)
	if second.Embedded != 0 {
		t.Errorf("re-digesting an unchanged source embedded %d chunks", second.Embedded)
	}
	if second.Reused != second.Chunks {
		t.Errorf("reused %d of %d chunks", second.Reused, second.Chunks)
	}
}

// TestDigestReembedsOnlyWhatChanged checks the reuse is keyed on content and not
// on something coarser like the file or the corpus.
func TestDigestReembedsOnlyWhatChanged(t *testing.T) {
	files := sampleFiles()
	src := writeSource(t, "sample", files)
	digested := t.TempDir()

	first := run(t, src, digested, 4)

	edited := "# Post number 3\n\nCompletely different writing about tides and the moon.\n"
	if err := os.WriteFile(filepath.Join(src, "posts", "post-03.md"), []byte(edited), 0644); err != nil {
		t.Fatal(err)
	}

	second := run(t, src, digested, 4)

	if second.Embedded == 0 {
		t.Error("editing a document embedded nothing")
	}
	if second.Embedded >= first.Chunks {
		t.Errorf("editing one document re-embedded %d chunks of %d; reuse is not working", second.Embedded, first.Chunks)
	}
	if second.Reused == 0 {
		t.Error("editing one document reused nothing")
	}
}

func TestDigestForceReembedsEverything(t *testing.T) {
	src := writeSource(t, "sample", sampleFiles())
	digested := t.TempDir()

	first := run(t, src, digested, 4)

	st, _ := store.Open(digested)
	second, err := Run(context.Background(), Options{
		SourceDir: src,
		Store:     st,
		Embedder:  &stubEmbedder{dims: 16},
		ModelName: "stub",
		Workers:   4,
		Force:     true,
	})
	if err != nil {
		t.Fatal(err)
	}

	if second.Reused != 0 {
		t.Errorf("-force reused %d vectors", second.Reused)
	}
	if second.Embedded != first.Chunks {
		t.Errorf("-force embedded %d of %d chunks", second.Embedded, first.Chunks)
	}
}

// TestDigestSkipsExcludedFiles checks the built-in noise list prunes rather than
// merely failing to parse.
func TestDigestSkipsExcludedFiles(t *testing.T) {
	src := writeSource(t, "sample", sampleFiles())
	rep := run(t, src, t.TempDir(), 4)

	if len(rep.Errors) != 0 {
		t.Errorf("digest reported %d file errors; the image should have been excluded, not attempted: %v", len(rep.Errors), rep.Errors)
	}
	if rep.Documents != 26 {
		t.Errorf("digested %d documents, expected 26 (25 posts and one note)", rep.Documents)
	}
}

func TestDigestWritesManifest(t *testing.T) {
	src := writeSource(t, "sample", sampleFiles())
	digested := t.TempDir()

	run(t, src, digested, 4)

	st, _ := store.Open(digested)
	man, err := st.LoadManifest()
	if err != nil {
		t.Fatal(err)
	}

	info, ok := man.Source("sample")
	if !ok {
		t.Fatal("the manifest does not mention the corpus that was just digested")
	}
	if info.Documents != 26 {
		t.Errorf("manifest records %d documents, expected 26", info.Documents)
	}
	if man.Embed.Model != "stub" {
		t.Errorf("manifest records model %q, expected \"stub\"", man.Embed.Model)
	}
	if man.Embed.Dimensions != 16 {
		t.Errorf("manifest records %d dimensions, expected 16", man.Embed.Dimensions)
	}
	if man.Chunking != store.CurrentChunking() {
		t.Errorf("manifest recorded chunking %+v, expected %+v", man.Chunking, store.CurrentChunking())
	}
}

// TestDigestRejectsAMismatchedModel is the guard against the quietest failure
// available: comparing vectors from two unrelated spaces.
func TestDigestRejectsAMismatchedModel(t *testing.T) {
	src := writeSource(t, "sample", sampleFiles())
	digested := t.TempDir()

	run(t, src, digested, 4)

	st, _ := store.Open(digested)
	man, _ := st.LoadManifest()

	if err := man.CheckEmbed("stub", "", 16); err != nil {
		t.Errorf("the model it was digested with was rejected: %v", err)
	}
	if err := man.CheckEmbed("some-other-model", "", 16); err == nil {
		t.Error("a different model was accepted against an existing corpus")
	}
	if err := man.CheckEmbed("stub", "", 768); err == nil {
		t.Error("a different vector width was accepted against an existing corpus")
	}
}

func TestDigestUnknownTypeFails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "corpus.yaml"), []byte("type: nonsense\nname: x\n"), 0644); err != nil {
		t.Fatal(err)
	}

	st, _ := store.Open(t.TempDir())
	_, err := Run(context.Background(), Options{
		SourceDir: dir,
		Store:     st,
		Embedder:  &stubEmbedder{dims: 16},
	})
	if err == nil {
		t.Error("an unknown source type was accepted")
	}
}

func TestDigestPendingParserFailsEarly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "corpus.yaml"), []byte("type: facebook\nname: fb\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stub := &stubEmbedder{dims: 16}
	st, _ := store.Open(t.TempDir())
	_, err := Run(context.Background(), Options{
		SourceDir: dir,
		Store:     st,
		Embedder:  stub,
	})

	if err == nil {
		t.Fatal("a parser that is not written yet reported success")
	}
	if stub.calls != 0 {
		t.Errorf("the run embedded %d texts before discovering it could not parse anything", stub.calls)
	}
}
