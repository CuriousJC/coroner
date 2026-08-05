package search

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// BM25 parameters, at their conventional values. k1 controls how quickly repeated
// terms stop adding score, b how strongly a long chunk is penalised for being
// long. These are the defaults essentially every implementation uses, and there
// is no corpus-specific reason here to depart from them.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// Tokenise splits text into lowercase terms.
//
// Runs of letters and digits, everything else a separator. Deliberately not
// stemmed: the lexical half of this search exists to be literal, and to catch
// exactly the queries the vector half is bad at -- names, places, unusual
// coinages. Stemming would blur precisely the terms that need to stay sharp,
// and morphological variants are what the vector half is for.
//
// Apostrophes are dropped rather than treated as separators, so "don't" is one
// token and not two, which keeps it from matching every occurrence of "t".
func Tokenise(s string) []string {
	var out []string
	var b strings.Builder

	flush := func() {
		if b.Len() > 0 {
			out = append(out, b.String())
			b.Reset()
		}
	}

	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == '\'' || r == '’':
			// Swallowed, not a boundary.
		default:
			flush()
		}
	}
	flush()

	return out
}

// posting is one term's presence in one chunk.
type posting struct {
	chunk int
	tf    int
}

// bm25Index is an inverted index over chunk text.
//
// Built in memory at search time rather than persisted alongside the corpus.
// At this scale it costs a fraction of a second, and a lexical index that is
// not stored is a lexical index that cannot drift out of step with the
// documents it describes.
type bm25Index struct {
	postings map[string][]posting

	lengths []int
	avgLen  float64
	total   int
}

func buildBM25(texts []string) *bm25Index {
	ix := &bm25Index{
		postings: make(map[string][]posting, len(texts)*8),
		lengths:  make([]int, len(texts)),
		total:    len(texts),
	}

	var sum int
	for i, text := range texts {
		terms := Tokenise(text)
		ix.lengths[i] = len(terms)
		sum += len(terms)

		// Count within the chunk first, so each term contributes one posting
		// rather than one per occurrence.
		tf := make(map[string]int, len(terms))
		for _, t := range terms {
			tf[t]++
		}

		// Sorted, because the posting lists are accumulated from a map and
		// their order decides the order floats are summed at scoring time.
		// Unsorted, two runs over identical data could differ in the last bits
		// of a score, which is enough to swap two near-tied results.
		keys := make([]string, 0, len(tf))
		for t := range tf {
			keys = append(keys, t)
		}
		sort.Strings(keys)

		for _, t := range keys {
			ix.postings[t] = append(ix.postings[t], posting{chunk: i, tf: tf[t]})
		}
	}

	if len(texts) > 0 {
		ix.avgLen = float64(sum) / float64(len(texts))
	}
	return ix
}

// search scores the corpus against a query and returns the best `limit` chunks.
//
// Accumulating into a dense slice rather than a map is the deterministic choice
// as well as the fast one: a map would have to be iterated to find the leaders,
// and that iteration order is randomised.
func (ix *bm25Index) search(query string, limit int) []Hit {
	terms := Tokenise(query)
	if len(terms) == 0 || ix.total == 0 {
		return nil
	}

	// Unique terms in sorted order, so the summation order is a function of the
	// query and nothing else. A repeated query term does not count twice; BM25
	// saturates within a document, and repeating a word in the query is not a
	// request for it to matter more.
	seen := make(map[string]struct{}, len(terms))
	unique := make([]string, 0, len(terms))
	for _, t := range terms {
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		unique = append(unique, t)
	}
	sort.Strings(unique)

	scores := make([]float64, ix.total)
	touched := false

	for _, t := range unique {
		list := ix.postings[t]
		if len(list) == 0 {
			continue
		}
		touched = true

		df := float64(len(list))
		idf := math.Log(1 + (float64(ix.total)-df+0.5)/(df+0.5))

		for _, p := range list {
			tf := float64(p.tf)
			norm := 1 - bm25B + bm25B*float64(ix.lengths[p.chunk])/ix.avgLen
			scores[p.chunk] += idf * (tf * (bm25K1 + 1)) / (tf + bm25K1*norm)
		}
	}

	if !touched {
		return nil
	}

	top := newTopN(limit)
	for i, s := range scores {
		if s > 0 {
			top.add(Hit{Chunk: i, Score: s})
		}
	}
	return top.sorted()
}
