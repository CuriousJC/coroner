package stats

import (
	"strings"
	"testing"
	"time"

	"github.com/curiousjc/coroner/internal/doc"
	"github.com/curiousjc/coroner/internal/store"
)

func corpusOf(name string, docs ...doc.Document) *store.Corpus {
	c := &store.Corpus{Name: name}
	for _, d := range docs {
		c.Docs = append(c.Docs, d)
		c.Chunks = append(c.Chunks, doc.Split(d)...)
	}
	return c
}

func mk(source, key, text string, published time.Time) doc.Document {
	return doc.New(source, "text", key, "f.txt", doc.Document{
		Text:      text,
		Published: published,
	})
}

func day(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
}

func TestComputeCountsDocumentsAndWords(t *testing.T) {
	c := corpusOf("a",
		mk("a", "1", "one two three", day(2020, 1, 1)),
		mk("a", "2", "four five", day(2021, 6, 1)),
	)

	rep := Compute([]*store.Corpus{c})
	if len(rep.Sources) != 1 {
		t.Fatalf("computed %d sources, want 1", len(rep.Sources))
	}

	s := rep.Sources[0]
	if s.Documents != 2 {
		t.Errorf("documents = %d, want 2", s.Documents)
	}
	if s.Words != 5 {
		t.Errorf("words = %d, want 5", s.Words)
	}
	if s.Name != "a" {
		t.Errorf("name = %q", s.Name)
	}
}

func TestComputeTracksDateSpanAndUndated(t *testing.T) {
	c := corpusOf("a",
		mk("a", "1", "dated early", day(2015, 3, 4)),
		mk("a", "2", "dated late", day(2022, 11, 30)),
		mk("a", "3", "no date at all", time.Time{}),
	)

	s := Compute([]*store.Corpus{c}).Sources[0]

	if got := s.Earliest.Format("2006-01-02"); got != "2015-03-04" {
		t.Errorf("earliest = %s", got)
	}
	if got := s.Latest.Format("2006-01-02"); got != "2022-11-30" {
		t.Errorf("latest = %s", got)
	}
	if s.Undated != 1 {
		t.Errorf("undated = %d, want 1", s.Undated)
	}
}

// TestComputeYearsAreSortedAndComplete guards the histogram, which is the whole
// reason this package exists: a year missing from it is either a year you did
// not write or a year the parser dropped.
func TestComputeYearsAreSortedAndComplete(t *testing.T) {
	c := corpusOf("a",
		mk("a", "1", "later post", day(2022, 1, 1)),
		mk("a", "2", "earlier post", day(2019, 1, 1)),
		mk("a", "3", "another later post", day(2022, 6, 1)),
	)

	s := Compute([]*store.Corpus{c}).Sources[0]

	if len(s.ByYear) != 2 {
		t.Fatalf("got %d years, want 2: %+v", len(s.ByYear), s.ByYear)
	}
	if s.ByYear[0].Year != 2019 || s.ByYear[1].Year != 2022 {
		t.Errorf("years are not sorted ascending: %+v", s.ByYear)
	}
	if s.ByYear[1].Count != 2 {
		t.Errorf("2022 count = %d, want 2", s.ByYear[1].Count)
	}
}

func TestComputeIsDeterministic(t *testing.T) {
	var docs []doc.Document
	for i := 0; i < 60; i++ {
		docs = append(docs, mk("a", string(rune('a'+i%26))+string(rune('0'+i/26)),
			"some writing about a thing number "+string(rune('a'+i%26)), day(2015+i%8, 1+i%12, 1)))
	}
	c := corpusOf("a", docs...)

	first := Compute([]*store.Corpus{c})
	for i := 0; i < 10; i++ {
		again := Compute([]*store.Corpus{c})

		if again.Total.Vocabulary != first.Total.Vocabulary || again.Total.Hapax != first.Total.Hapax {
			t.Fatalf("run %d disagreed on vocabulary", i)
		}
		if len(again.Total.ByYear) != len(first.Total.ByYear) {
			t.Fatalf("run %d produced a different number of years", i)
		}
		for j := range first.Total.ByYear {
			if again.Total.ByYear[j] != first.Total.ByYear[j] {
				t.Fatalf("run %d differs at year %d; a map was iterated without sorting", i, j)
			}
		}
	}
}

func TestComputeTotalsAcrossCorpora(t *testing.T) {
	a := corpusOf("a", mk("a", "1", "alpha writing here", day(2020, 1, 1)))
	b := corpusOf("b", mk("b", "1", "beta writing here", day(2021, 1, 1)))

	rep := Compute([]*store.Corpus{a, b})

	if len(rep.Sources) != 2 {
		t.Fatalf("got %d sources, want 2", len(rep.Sources))
	}
	if rep.Total.Documents != 2 {
		t.Errorf("total documents = %d, want 2", rep.Total.Documents)
	}
	if rep.Total.Name != "all" {
		t.Errorf("total name = %q", rep.Total.Name)
	}
	if len(rep.Total.ByYear) != 2 {
		t.Errorf("total years = %+v, want two", rep.Total.ByYear)
	}

	// "writing" and "here" appear in both corpora and must be counted once in
	// the combined vocabulary, and must not be hapax there.
	if rep.Total.Vocabulary >= rep.Sources[0].Vocabulary+rep.Sources[1].Vocabulary {
		t.Errorf("combined vocabulary %d did not merge shared terms", rep.Total.Vocabulary)
	}
}

func TestSpreadHandlesSmallInput(t *testing.T) {
	if got := spread(nil); got != (Spread{}) {
		t.Errorf("spread(nil) = %+v, want zero", got)
	}

	got := spread([]int{5})
	if got.Min != 5 || got.Max != 5 || got.P50 != 5 || got.P99 != 5 || got.Mean != 5 {
		t.Errorf("spread of one element = %+v", got)
	}
}

func TestSpreadOrdersPercentiles(t *testing.T) {
	var v []int
	for i := 1; i <= 100; i++ {
		v = append(v, i)
	}

	s := spread(v)
	if !(s.Min <= s.P50 && s.P50 <= s.P90 && s.P90 <= s.P99 && s.P99 <= s.Max) {
		t.Errorf("percentiles are not ordered: %+v", s)
	}
	if s.Min != 1 || s.Max != 100 {
		t.Errorf("min/max = %d/%d, want 1/100", s.Min, s.Max)
	}
}

func TestEmptyDocumentsAreCounted(t *testing.T) {
	// A document that survived digestion with no text should never happen, which
	// is exactly why it is worth reporting if it does.
	c := &store.Corpus{Name: "a"}
	c.Docs = append(c.Docs, doc.Document{ID: "x", Text: ""})

	if got := Compute([]*store.Corpus{c}).Sources[0].Empty; got != 1 {
		t.Errorf("empty = %d, want 1", got)
	}
}

// TestSampleSpansTheCorpus checks the sampling is spread rather than taken from
// the front, since documents are stored sorted by a hash and the first few are a
// fixed arbitrary slice that would hide a parser failing on later records.
func TestSampleSpansTheCorpus(t *testing.T) {
	var docs []doc.Document
	for i := 0; i < 100; i++ {
		docs = append(docs, mk("a", strings.Repeat("k", i+1), "writing number here", day(2020, 1, 1)))
	}
	c := corpusOf("a", docs...)

	got := Sample(c, 5)
	if len(got) != 5 {
		t.Fatalf("sampled %d, want 5", len(got))
	}

	// The last sample must come from well past the beginning.
	firstIdx, lastIdx := -1, -1
	for i, d := range c.Docs {
		if d.ID == got[0].ID {
			firstIdx = i
		}
		if d.ID == got[len(got)-1].ID {
			lastIdx = i
		}
	}
	if lastIdx-firstIdx < len(c.Docs)/2 {
		t.Errorf("samples span indices %d..%d of %d; that is not spread through the corpus", firstIdx, lastIdx, len(c.Docs))
	}

	if n := len(Sample(c, 1000)); n != 100 {
		t.Errorf("asking for more samples than documents returned %d, want 100", n)
	}
	if got := Sample(c, 0); got != nil {
		t.Errorf("Sample(0) returned %d documents", len(got))
	}
}
