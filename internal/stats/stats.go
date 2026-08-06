// Package stats describes a digested corpus.
//
// This exists because a corpus is not something you can eyeball. Five thousand
// documents you cannot read are indistinguishable from five thousand documents
// that parsed badly, and the failure modes of a parser are quiet ones: text
// truncated at the first newline, every document dated 1970, a vocabulary
// consisting mostly of HTML fragments. Each of those looks fine in a search
// result and obvious in a histogram.
//
// Everything here is derived from the digested corpus rather than the export, so
// it describes what coroner will actually search rather than what it read.
package stats

import (
	"sort"
	"time"

	"github.com/curiousjc/coroner/internal/doc"
	"github.com/curiousjc/coroner/internal/search"
	"github.com/curiousjc/coroner/internal/store"
)

// Report is the whole picture: one entry per corpus plus a combined total.
type Report struct {
	Sources []Source `json:"sources"`
	Total   Source   `json:"total"`
}

// Source is one corpus, or the total across all of them.
type Source struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`

	Documents int `json:"documents"`
	Chunks    int `json:"chunks"`
	Words     int `json:"words"`

	// Undated counts documents whose export carried no timestamp. A number
	// close to the document count usually means the parser is not finding the
	// date field rather than that the writing is undated.
	Undated int `json:"undated"`

	Earliest time.Time `json:"earliest,omitempty"`
	Latest   time.Time `json:"latest,omitempty"`

	// ByYear is sorted ascending. A gap in it is either a year you did not
	// write or a year the parser dropped, and only you can tell which.
	ByYear []YearCount `json:"by_year,omitempty"`

	// WordsPerDocument is where truncation shows up: a parser that stops at the
	// first newline produces a suspiciously tight distribution.
	WordsPerDocument Spread `json:"words_per_document"`

	ChunksPerDocument Spread `json:"chunks_per_document"`

	// Vocabulary is the number of distinct lexical terms, and Hapax how many of
	// those appear exactly once. A healthy corpus of personal writing has a
	// large hapax share -- names, places, one-off words. A small one suggests
	// the text is mostly boilerplate.
	Vocabulary int `json:"vocabulary"`
	Hapax      int `json:"hapax"`

	// Empty counts documents that survived digestion with no text at all. Should
	// be zero; anything else is a parser emitting placeholders.
	Empty int `json:"empty"`
}

// YearCount is one bar of the histogram.
type YearCount struct {
	Year  int `json:"year"`
	Count int `json:"count"`
}

// Spread is a distribution summarised the way a distribution actually needs to
// be: not a mean, which one 12,000-word outlier ruins, but the shape.
type Spread struct {
	Min  int `json:"min"`
	P50  int `json:"p50"`
	P90  int `json:"p90"`
	P99  int `json:"p99"`
	Max  int `json:"max"`
	Mean int `json:"mean"`
}

// Compute builds the report. Corpora arrive in sorted order and are kept that
// way, so the output is stable between runs.
func Compute(corpora []*store.Corpus) Report {
	var rep Report

	totalTerms := map[string]int{}

	for _, c := range corpora {
		s := computeOne(c, totalTerms)
		rep.Sources = append(rep.Sources, s)

		rep.Total.Documents += s.Documents
		rep.Total.Chunks += s.Chunks
		rep.Total.Words += s.Words
		rep.Total.Undated += s.Undated
		rep.Total.Empty += s.Empty

		rep.Total.Earliest = earlier(rep.Total.Earliest, s.Earliest)
		rep.Total.Latest = later(rep.Total.Latest, s.Latest)
	}

	rep.Total.Name = "all"
	rep.Total.Vocabulary = len(totalTerms)
	for _, n := range totalTerms {
		if n == 1 {
			rep.Total.Hapax++
		}
	}

	// The combined spreads are recomputed across every document rather than
	// averaged from the per-corpus ones, because an average of percentiles is
	// not a percentile of anything.
	var words, chunks []int
	perDoc := map[string]int{}
	for _, c := range corpora {
		for _, ch := range c.Chunks {
			perDoc[ch.DocID]++
		}
	}
	for _, c := range corpora {
		for _, d := range c.Docs {
			words = append(words, d.Words)
			chunks = append(chunks, perDoc[d.ID])
		}
	}
	rep.Total.WordsPerDocument = spread(words)
	rep.Total.ChunksPerDocument = spread(chunks)
	rep.Total.ByYear = mergeYears(rep.Sources)

	return rep
}

func computeOne(c *store.Corpus, totalTerms map[string]int) Source {
	s := Source{Name: c.Name, Documents: len(c.Docs), Chunks: len(c.Chunks)}

	perDoc := map[string]int{}
	for _, ch := range c.Chunks {
		perDoc[ch.DocID]++
	}

	years := map[int]int{}
	terms := map[string]int{}

	var words, chunks []int

	for _, d := range c.Docs {
		if s.Type == "" {
			s.Type = d.SourceType
		}

		s.Words += d.Words
		words = append(words, d.Words)
		chunks = append(chunks, perDoc[d.ID])

		if d.Text == "" {
			s.Empty++
		}

		if d.Published.IsZero() {
			s.Undated++
		} else {
			years[d.Published.Year()]++
			s.Earliest = earlier(s.Earliest, d.Published)
			s.Latest = later(s.Latest, d.Published)
		}
	}

	// Vocabulary is counted over chunk text, which is what the lexical
	// retriever actually indexes.
	for _, ch := range c.Chunks {
		for _, t := range search.Tokenise(ch.Text) {
			terms[t]++
			totalTerms[t]++
		}
	}

	s.Vocabulary = len(terms)
	for _, n := range terms {
		if n == 1 {
			s.Hapax++
		}
	}

	s.WordsPerDocument = spread(words)
	s.ChunksPerDocument = spread(chunks)

	// Sorted, because this came out of a map.
	for y, n := range years {
		s.ByYear = append(s.ByYear, YearCount{Year: y, Count: n})
	}
	sort.Slice(s.ByYear, func(i, j int) bool { return s.ByYear[i].Year < s.ByYear[j].Year })

	return s
}

func mergeYears(sources []Source) []YearCount {
	years := map[int]int{}
	for _, s := range sources {
		for _, y := range s.ByYear {
			years[y.Year] += y.Count
		}
	}

	out := make([]YearCount, 0, len(years))
	for y, n := range years {
		out = append(out, YearCount{Year: y, Count: n})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Year < out[j].Year })
	return out
}

func spread(v []int) Spread {
	if len(v) == 0 {
		return Spread{}
	}

	s := make([]int, len(v))
	copy(s, v)
	sort.Ints(s)

	total := 0
	for _, x := range s {
		total += x
	}

	return Spread{
		Min:  s[0],
		P50:  s[len(s)*50/100],
		P90:  s[min(len(s)*90/100, len(s)-1)],
		P99:  s[min(len(s)*99/100, len(s)-1)],
		Max:  s[len(s)-1],
		Mean: total / len(s),
	}
}

func earlier(a, b time.Time) time.Time {
	if b.IsZero() {
		return a
	}
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}

func later(a, b time.Time) time.Time {
	if b.IsZero() {
		return a
	}
	if a.IsZero() || b.After(a) {
		return b
	}
	return a
}

// Sample returns a few documents spread evenly through a corpus, for eyeballing.
//
// Evenly spread rather than random, and rather than the first few: documents are
// stored sorted by ID, which is a hash, so the first few are already an
// arbitrary sample -- but a fixed one. Sampling across the whole range is what
// catches a parser that works on early records and fails on late ones.
func Sample(c *store.Corpus, n int) []doc.Document {
	if n <= 0 || len(c.Docs) == 0 {
		return nil
	}
	if n >= len(c.Docs) {
		return c.Docs
	}

	out := make([]doc.Document, 0, n)
	step := len(c.Docs) / n
	for i := 0; i < n; i++ {
		out = append(out, c.Docs[i*step])
	}
	return out
}
