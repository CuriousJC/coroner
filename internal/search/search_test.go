package search

import (
	"testing"

	"github.com/curiousjc/coroner/internal/doc"
	"github.com/curiousjc/coroner/internal/store"
)

// build makes a small corpus with hand-chosen vectors, so the vector retriever's
// behaviour is a property of the test rather than of a model.
func build(t *testing.T, texts []string, vecs [][]float32) *Engine {
	t.Helper()

	c := &store.Corpus{Name: "test"}
	for i, text := range texts {
		d := doc.New("test", "text", string(rune('a'+i)), "f.txt", doc.Document{Text: text})
		c.Docs = append(c.Docs, d)

		chunks := doc.Split(d)
		if len(chunks) != 1 {
			t.Fatalf("test text %d produced %d chunks, expected 1", i, len(chunks))
		}
		c.Chunks = append(c.Chunks, chunks[0])

		v := append([]float32(nil), vecs[i]...)
		store.Normalise(v)
		c.Vectors = append(c.Vectors, v)
	}

	return NewEngine([]*store.Corpus{c})
}

func TestLexicalFindsTheLiteralWord(t *testing.T) {
	e := build(t,
		[]string{
			"A post about Thatcher and the miners.",
			"A post about pastry, entirely unrelated.",
			"A post about weather.",
		},
		[][]float32{{1, 0}, {0, 1}, {0, 1}},
	)

	got := e.Search(Query{Text: "Thatcher", Hits: 3, Mode: ModeLexical})
	if len(got) == 0 {
		t.Fatal("lexical search found nothing for a word that is present")
	}
	if !contains(got[0].Chunk.Text, "Thatcher") {
		t.Errorf("the top lexical hit does not contain the query term: %q", got[0].Chunk.Text)
	}
	if got[0].LexicalRank != 1 {
		t.Errorf("top result has lexical rank %d", got[0].LexicalRank)
	}
	if got[0].VectorRank != 0 {
		t.Errorf("lexical-only search reported a vector rank of %d", got[0].VectorRank)
	}
}

// TestVectorFindsWhatLexicalCannot is the whole reason embeddings are here: a
// query sharing no words with the document it should find.
func TestVectorFindsWhatLexicalCannot(t *testing.T) {
	e := build(t,
		[]string{
			"Every fork in the road is a branching decision tree.",
			"A recipe for shortcrust pastry, entirely unrelated.",
		},
		[][]float32{{1, 0}, {0, 1}},
	)

	// A query vector pointing at the first document, sharing no vocabulary.
	got := e.Search(Query{Text: "choice", Vector: []float32{0.99, 0.01}, Hits: 2, Mode: ModeVector})
	if len(got) == 0 {
		t.Fatal("vector search found nothing")
	}
	if !contains(got[0].Chunk.Text, "branching") {
		t.Errorf("vector search returned the wrong document: %q", got[0].Chunk.Text)
	}

	// The same query lexically should find nothing at all, which is the point.
	if lex := e.Search(Query{Text: "choice", Hits: 2, Mode: ModeLexical}); len(lex) != 0 {
		t.Errorf("lexical search unexpectedly matched %d documents for a word not present", len(lex))
	}
}

// TestHybridPrefersAgreement is the property RRF exists to give: a document both
// retrievers liked beats one that only a single retriever ranked first.
func TestHybridPrefersAgreement(t *testing.T) {
	e := build(t,
		[]string{
			"A discussion of choice and the weight of choosing.", // both should like this
			"A branching decision tree, described at length.",    // vector only
			"The word choice appears here once, in passing, amid unrelated filler about tides and pastry and weather.",
		},
		[][]float32{{0.9, 0.1}, {1, 0}, {0, 1}},
	)

	got := e.Search(Query{
		Text:   "choice",
		Vector: []float32{1, 0},
		Hits:   3,
		Mode:   ModeHybrid,
	})

	if len(got) < 2 {
		t.Fatalf("hybrid search returned %d results", len(got))
	}

	top := got[0]
	if top.LexicalRank == 0 || top.VectorRank == 0 {
		t.Errorf("the top hybrid result was ranked by only one retriever (lexical #%d, vector #%d): %q",
			top.LexicalRank, top.VectorRank, top.Chunk.Text)
	}
}

// TestSearchIsDeterministic runs the same query repeatedly. Anything that
// iterated a map without sorting afterwards shows up here.
func TestSearchIsDeterministic(t *testing.T) {
	var texts []string
	var vecs [][]float32
	for i := 0; i < 40; i++ {
		// Deliberately repetitive, so many chunks score identically and ties are
		// the common case rather than the rare one.
		texts = append(texts, "choice and decision and branching logic repeated for the sake of ties")
		vecs = append(vecs, []float32{1, 0})
	}

	e := build(t, texts, vecs)

	q := Query{Text: "choice decision", Vector: []float32{1, 0}, Hits: 10, Mode: ModeHybrid}
	first := e.Search(q)

	for run := 0; run < 20; run++ {
		got := e.Search(q)
		if len(got) != len(first) {
			t.Fatalf("run %d returned %d results, first run returned %d", run, len(got), len(first))
		}
		for i := range first {
			if got[i].Doc.ID != first[i].Doc.ID || got[i].Score != first[i].Score {
				t.Fatalf("run %d differs at position %d: %s (%v) vs %s (%v)",
					run, i, got[i].Doc.ID, got[i].Score, first[i].Doc.ID, first[i].Score)
			}
		}
	}
}

// TestRollUpReturnsEachDocumentOnce guards the rule that results are documents,
// not passages: a long essay must not fill the whole first page with its own
// chunks.
func TestRollUpReturnsEachDocumentOnce(t *testing.T) {
	long := ""
	for i := 0; i < 40; i++ {
		long += "A paragraph about choice, and choosing, and the weight of it all.\n\n"
	}

	c := &store.Corpus{Name: "test"}
	d := doc.New("test", "text", "long", "f.txt", doc.Document{Text: long})
	c.Docs = append(c.Docs, d)

	chunks := doc.Split(d)
	if len(chunks) < 3 {
		t.Fatalf("test document produced only %d chunks", len(chunks))
	}
	for range chunks {
		v := []float32{1, 0}
		store.Normalise(v)
		c.Vectors = append(c.Vectors, v)
	}
	c.Chunks = chunks

	e := NewEngine([]*store.Corpus{c})
	got := e.Search(Query{Text: "choice", Vector: []float32{1, 0}, Hits: 10, Mode: ModeHybrid})

	if len(got) != 1 {
		t.Errorf("one document produced %d results; chunks were not rolled up", len(got))
	}
}

func TestEmptyQueryReturnsNothing(t *testing.T) {
	e := build(t, []string{"some writing about a thing"}, [][]float32{{1, 0}})

	if got := e.Search(Query{Text: "   ", Hits: 5, Mode: ModeLexical}); len(got) != 0 {
		t.Errorf("an empty query returned %d results", len(got))
	}
}

func TestTokenise(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"Hello, World!", []string{"hello", "world"}},
		{"don't stop", []string{"dont", "stop"}},
		{"one-two_three", []string{"one", "two", "three"}},
		{"2019 was a year", []string{"2019", "was", "a", "year"}},
		{"   ", nil},
	}

	for _, tc := range tests {
		got := Tokenise(tc.in)
		if len(got) != len(tc.want) {
			t.Errorf("Tokenise(%q) = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("Tokenise(%q) = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

// TestFuseRewardsBothLists checks the arithmetic directly, since the ranking
// behaviour of the whole tool rests on it.
func TestFuseRewardsBothLists(t *testing.T) {
	lex := []Hit{{Chunk: 1, Score: 9}, {Chunk: 2, Score: 8}}
	vec := []Hit{{Chunk: 2, Score: 0.9}, {Chunk: 3, Score: 0.8}}

	got := fuse(lex, vec)
	if len(got) != 3 {
		t.Fatalf("fused %d chunks, expected 3", len(got))
	}

	// Chunk 2 is second in both lists; chunk 1 is first in one and absent from
	// the other. Agreement should win.
	if got[0].chunk != 2 {
		t.Errorf("top fused chunk is %d, expected 2 (the one both retrievers ranked)", got[0].chunk)
	}
	if got[0].lexRank != 2 || got[0].vecRank != 1 {
		t.Errorf("chunk 2 recorded ranks lexical #%d vector #%d, expected #2 and #1", got[0].lexRank, got[0].vecRank)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
