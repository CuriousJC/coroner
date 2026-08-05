package search

import (
	"container/heap"
	"sort"
)

// Hit is one scored chunk, identified by its position in the engine's flat
// chunk slice.
type Hit struct {
	Chunk int
	Score float64
}

// worse reports whether a should be discarded before b: lower score first, and
// on a tie the higher chunk index, so that ties resolve toward the earlier
// chunk. Ties are not hypothetical -- two chunks of boilerplate score
// identically -- and without a total order the result would depend on heap
// internals rather than on the corpus.
func worse(a, b Hit) bool {
	if a.Score != b.Score {
		return a.Score < b.Score
	}
	return a.Chunk > b.Chunk
}

// hitHeap is a min-heap on worse, so the element to evict is always at the root.
type hitHeap struct{ items []Hit }

func (h *hitHeap) Len() int           { return len(h.items) }
func (h *hitHeap) Less(i, j int) bool { return worse(h.items[i], h.items[j]) }
func (h *hitHeap) Swap(i, j int)      { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *hitHeap) Push(x any)         { h.items = append(h.items, x.(Hit)) }

func (h *hitHeap) Pop() any {
	old := h.items
	n := len(old)
	last := old[n-1]
	h.items = old[:n-1]
	return last
}

// topN keeps the best `limit` hits it is shown, in memory bounded by limit
// rather than by how many chunks exist.
//
// Carried over from hecato, where it kept the largest files out of a million
// without holding a million File structs. The job here is the same shape: a
// retriever scores every chunk in the corpus and only the leaders matter, and
// sorting the whole scored corpus to take two hundred of it is work nobody
// needs done.
type topN struct {
	limit int
	h     *hitHeap
}

func newTopN(limit int) *topN {
	if limit < 0 {
		limit = 0
	}
	return &topN{limit: limit, h: &hitHeap{items: make([]Hit, 0, min(limit, 1024))}}
}

// add offers a hit. It is kept only if there is room or it beats the weakest
// entry currently held.
func (t *topN) add(x Hit) {
	if t.limit == 0 {
		return
	}

	if t.h.Len() < t.limit {
		heap.Push(t.h, x)
		return
	}

	if worse(t.h.items[0], x) {
		t.h.items[0] = x
		heap.Fix(t.h, 0)
	}
}

// sorted returns the held hits best first.
func (t *topN) sorted() []Hit {
	out := make([]Hit, len(t.h.items))
	copy(out, t.h.items)

	sort.Slice(out, func(i, j int) bool { return worse(out[j], out[i]) })
	return out
}
