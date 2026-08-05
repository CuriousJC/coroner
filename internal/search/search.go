// Package search retrieves documents from a digested corpus.
//
// Two retrievers run over the same chunks and their rankings are fused.
//
// That is not hedging, it is the design. The two things you want to search for
// pull in opposite directions. A name -- a person, a place, a book -- has almost
// no semantic neighbourhood: vector search will happily return everything about
// politics generally while missing the one post that names the person once.
// An idea does the reverse: nothing lexical connects "choice" to "branching
// logic", and only a vector notices they are about the same thing. A tool that
// picked one retriever would work on half its queries.
//
// Reciprocal rank fusion combines them without needing their scores to be
// comparable, which they are not: BM25 returns an unbounded relevance number and
// cosine similarity returns something between -1 and 1. RRF throws the magnitudes
// away and keeps only the ranks, which is what makes it robust here.
package search

import (
	"sort"

	"github.com/curiousjc/coroner/internal/doc"
	"github.com/curiousjc/coroner/internal/store"
)

// RRFK damps the contribution of top ranks so that one retriever being very
// confident cannot single-handedly decide the result. 60 is the value from the
// original paper and the de facto standard; it is a constant rather than a flag
// because tuning it without a labelled evaluation set is guessing.
const RRFK = 60

// DefaultDepth is how many chunks each retriever contributes to the fusion.
//
// Deep enough that a document ranked mediocrely by one retriever and highly by
// the other still surfaces -- which is the entire point of fusing -- and shallow
// enough that the fusion stays cheap.
const DefaultDepth = 200

// Mode selects which retrievers run.
type Mode int

const (
	// ModeHybrid runs both and fuses. The default, and what the tool is for.
	ModeHybrid Mode = iota

	// ModeLexical is BM25 alone. Useful when you know the exact word you want
	// and semantic neighbours are noise, and it is the only mode that works
	// without ollama.
	ModeLexical

	// ModeVector is embeddings alone, for seeing what the semantic half thinks
	// on its own.
	ModeVector
)

func (m Mode) String() string {
	switch m {
	case ModeLexical:
		return "lexical"
	case ModeVector:
		return "vector"
	default:
		return "hybrid"
	}
}

// Engine holds a digested corpus in memory, flattened across sources.
type Engine struct {
	Docs   []doc.Document
	Chunks []doc.Chunk

	vectors [][]float32
	docAt   map[string]int
	lex     *bm25Index
}

// NewEngine flattens corpora into one searchable body.
//
// Corpora arrive in sorted name order and are appended in that order, so a
// chunk's index is a function of the corpus rather than of load timing. Several
// things downstream break ties on that index.
func NewEngine(corpora []*store.Corpus) *Engine {
	e := &Engine{docAt: map[string]int{}}

	for _, c := range corpora {
		for _, d := range c.Docs {
			e.docAt[d.ID] = len(e.Docs)
			e.Docs = append(e.Docs, d)
		}

		// A corpus digested without vectors -- which cannot currently happen,
		// but would if a lexical-only digest were ever added -- still
		// contributes to lexical search. Padding keeps chunk and vector indices
		// aligned so the vector retriever can simply skip the empty ones.
		for i, ch := range c.Chunks {
			e.Chunks = append(e.Chunks, ch)
			if i < len(c.Vectors) {
				e.vectors = append(e.vectors, c.Vectors[i])
			} else {
				e.vectors = append(e.vectors, nil)
			}
		}
	}

	// Indexed rather than raw chunk text, so the lexical half sees exactly what
	// the embedder saw. See doc.Indexed.
	texts := make([]string, len(e.Chunks))
	for i, ch := range e.Chunks {
		var d doc.Document
		if at, ok := e.docAt[ch.DocID]; ok {
			d = e.Docs[at]
		}
		texts[i] = doc.Indexed(d, ch)
	}
	e.lex = buildBM25(texts)

	return e
}

// Query is one search.
type Query struct {
	// Text is what was typed, used by the lexical retriever.
	Text string

	// Vector is Text already embedded. Nil in lexical mode.
	Vector []float32

	// Hits is how many documents to return.
	Hits int

	Mode Mode

	// Depth is how many chunks each retriever contributes. Zero means
	// DefaultDepth.
	Depth int
}

// Result is one document that matched, with the passage that matched best.
type Result struct {
	Doc   doc.Document
	Chunk doc.Chunk

	// Score is the fused score. Comparable within one result set and
	// meaningless outside it, which is why it is displayed as a bar rather than
	// a number you might be tempted to threshold on.
	Score float64

	// LexicalRank and VectorRank are this chunk's 1-based rank from each
	// retriever, or 0 when it did not appear. Worth surfacing: they are what
	// tells you whether a result came up because of the words or because of the
	// idea, which is most of what you want to know when a search surprises you.
	LexicalRank int
	VectorRank  int
}

// Search runs the query and returns documents, best first.
func (e *Engine) Search(q Query) []Result {
	depth := q.Depth
	if depth <= 0 {
		depth = DefaultDepth
	}

	var lexHits, vecHits []Hit
	if q.Mode == ModeHybrid || q.Mode == ModeLexical {
		lexHits = e.lex.search(q.Text, depth)
	}
	if (q.Mode == ModeHybrid || q.Mode == ModeVector) && len(q.Vector) > 0 {
		vecHits = e.vectorSearch(q.Vector, depth)
	}

	fused := fuse(lexHits, vecHits)
	return e.rollUp(fused, q.Hits)
}

// vectorSearch scores every chunk by cosine similarity.
//
// A dot product, because vectors are unit-normalised when they are stored.
// Brute force over the whole corpus: at tens of thousands of chunks this is a
// few million multiply-adds, which is microseconds, and it is exact rather than
// approximate.
func (e *Engine) vectorSearch(query []float32, limit int) []Hit {
	q := make([]float32, len(query))
	copy(q, query)
	store.Normalise(q)

	top := newTopN(limit)
	for i, v := range e.vectors {
		if len(v) != len(q) {
			continue
		}

		var sum float32
		for j, x := range v {
			sum += x * q[j]
		}
		top.add(Hit{Chunk: i, Score: float64(sum)})
	}

	return top.sorted()
}

// fusion is a chunk's combined standing.
type fusion struct {
	chunk   int
	score   float64
	lexRank int
	vecRank int
}

// fuse combines two ranked lists by reciprocal rank.
//
// Each list contributes 1/(RRFK + rank) to every chunk it ranked. A chunk that
// both retrievers liked outscores one that either loved, which is the behaviour
// wanted: agreement between a literal match and a semantic one is the strongest
// signal available without a labelled evaluation set to tune against.
func fuse(lexHits, vecHits []Hit) []fusion {
	byChunk := make(map[int]*fusion, len(lexHits)+len(vecHits))

	get := func(c int) *fusion {
		f, ok := byChunk[c]
		if !ok {
			f = &fusion{chunk: c}
			byChunk[c] = f
		}
		return f
	}

	// Lexical first, then vector, always in that order, so every score is a sum
	// of the same terms added the same way on every run.
	for i, h := range lexHits {
		f := get(h.Chunk)
		f.lexRank = i + 1
		f.score += 1 / float64(RRFK+i+1)
	}
	for i, h := range vecHits {
		f := get(h.Chunk)
		f.vecRank = i + 1
		f.score += 1 / float64(RRFK+i+1)
	}

	out := make([]fusion, 0, len(byChunk))
	for _, f := range byChunk {
		out = append(out, *f)
	}

	// Sorted by chunk index before ranking, because the slice above was built
	// by iterating a map. Without this the input order to the sort is
	// randomised, and any pair of exactly tied chunks would swap between runs.
	sort.Slice(out, func(i, j int) bool { return out[i].chunk < out[j].chunk })
	sort.SliceStable(out, func(i, j int) bool { return out[i].score > out[j].score })

	return out
}

// rollUp turns ranked chunks into ranked documents.
//
// A document's score is its best chunk's score, and that chunk becomes the
// passage shown. The alternative -- summing a document's chunks -- rewards
// length rather than relevance, so a long essay that mentions the subject in
// passing five times would outrank a short post that is entirely about it.
func (e *Engine) rollUp(fused []fusion, hits int) []Result {
	if hits <= 0 {
		hits = 10
	}

	seen := make(map[string]struct{}, hits)
	out := make([]Result, 0, hits)

	for _, f := range fused {
		if len(out) >= hits {
			break
		}

		ch := e.Chunks[f.chunk]
		if _, ok := seen[ch.DocID]; ok {
			continue
		}
		seen[ch.DocID] = struct{}{}

		at, ok := e.docAt[ch.DocID]
		if !ok {
			// A chunk whose document is missing means the corpus files are out
			// of step. Skipping is right: the alternative is failing a whole
			// search over one orphan.
			continue
		}

		out = append(out, Result{
			Doc:         e.Docs[at],
			Chunk:       ch,
			Score:       f.score,
			LexicalRank: f.lexRank,
			VectorRank:  f.vecRank,
		})
	}

	return out
}
