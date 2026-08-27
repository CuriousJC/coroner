package dupes

import (
	"testing"
	"time"

	"github.com/curiousjc/coroner/internal/doc"
	"github.com/curiousjc/coroner/internal/store"
)

func day(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}

func corpus(name string, docs ...doc.Document) *store.Corpus {
	for i := range docs {
		docs[i].Source = name
	}
	return &store.Corpus{Name: name, Docs: docs}
}

func document(id, title, date, text string) doc.Document {
	return doc.Document{ID: id, Title: title, Published: day(date), Text: text}
}

// Two versions of the same paragraph, differing the way the real corpora do.
const (
	straight = "I don't think the argument holds up. It's the same claim dressed " +
		"differently, and we've heard it before. The trouble isn't that it's wrong, " +
		"it's that nobody has bothered to check whether it's right."
	curly = "I don’t think the argument holds up. It’s the same claim dressed " +
		"differently, and we’ve heard it before. The trouble isn’t that it’s wrong, " +
		"it’s that nobody has bothered to check whether it’s right."
	unrelated = "The bread proofed for nine hours on the counter and came out with a " +
		"crumb far more open than the last attempt, which I am putting down to the " +
		"warmer kitchen rather than anything I did deliberately."
)

func ranked() Options {
	o := Defaults()
	o.Priority = map[string]int{"substack": 30, "html": 20, "facebook": 10}
	return o
}

func TestFindMatchesAcrossCorpora(t *testing.T) {
	rep := Find([]*store.Corpus{
		corpus("facebook", document("f1", "", "2020-01-01", straight)),
		corpus("html", document("h1", "A Post", "2020-01-01", straight)),
	}, ranked())

	if len(rep.Groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(rep.Groups))
	}
	if got := len(rep.Groups[0].Members); got != 2 {
		t.Fatalf("want 2 members, got %d", got)
	}
	if rep.Duplicated != 2 {
		t.Errorf("Duplicated = %d, want 2", rep.Duplicated)
	}
}

// The whole point of the priority field: the highest-ranked corpus supplies the
// winner regardless of what order the corpora were loaded in.
func TestWinnerFollowsPriority(t *testing.T) {
	fb := corpus("facebook", document("f1", "", "2020-01-01", straight))
	hs := corpus("html", document("h1", "A Post", "2020-01-01", straight))
	sb := corpus("substack", document("s1", "A Post", "2020-01-01", straight))

	for _, order := range [][]*store.Corpus{
		{fb, hs, sb},
		{sb, hs, fb},
		{hs, sb, fb},
	} {
		rep := Find(order, ranked())
		if len(rep.Groups) != 1 {
			t.Fatalf("want 1 group, got %d", len(rep.Groups))
		}
		if rep.Groups[0].Winner != "s1" {
			t.Errorf("winner = %q, want s1", rep.Groups[0].Winner)
		}
	}
}

// An unranked corpus must not beat a ranked one. Zero is the default for a
// corpus.yaml nobody has thought about, and it should lose.
func TestUnrankedCorpusLoses(t *testing.T) {
	opts := Defaults()
	opts.Priority = map[string]int{"html": 20}

	rep := Find([]*store.Corpus{
		corpus("aaa_unranked", document("u1", "", "2020-01-01", straight)),
		corpus("html", document("h1", "A Post", "2020-01-01", straight)),
	}, opts)

	if len(rep.Groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(rep.Groups))
	}
	// "aaa_unranked" sorts first alphabetically, so this fails if priority is
	// ignored and the name tie-break is doing the work.
	if rep.Groups[0].Winner != "h1" {
		t.Errorf("winner = %q, want h1", rep.Groups[0].Winner)
	}
}

// The measured reason folding exists: Substack is overwhelmingly typographic and
// the HTML site overwhelmingly straight, and every contraction breaks a shingle.
func TestQuoteFoldingLiftsTheScore(t *testing.T) {
	folded := jaccard(shingle(straight), shingle(curly))
	if folded < 0.99 {
		t.Errorf("folded score = %.2f, want ~1.00", folded)
	}

	// Without the fold the same pair scores far lower. Computed here rather
	// than asserted as a constant, so this documents the gap rather than
	// pinning an exact number.
	raw := jaccard(shingleOf(straight, false), shingleOf(curly, false))
	if raw >= folded {
		t.Errorf("raw score %.2f should be below folded %.2f", raw, folded)
	}
	if raw > 0.5 {
		t.Errorf("raw score = %.2f; the fixture no longer demonstrates the problem", raw)
	}
}

func TestUnrelatedTextDoesNotMatch(t *testing.T) {
	rep := Find([]*store.Corpus{
		corpus("facebook", document("f1", "", "2020-01-01", straight)),
		corpus("html", document("h1", "Bread", "2020-01-01", unrelated)),
	}, ranked())

	if len(rep.Groups) != 0 {
		t.Errorf("want no groups, got %d", len(rep.Groups))
	}
}

// Duplicates within one corpus are digest's problem, not this pass's. Two posts
// in the same corpus that resemble each other are the author repeating himself.
func TestSameCorpusIsNotCompared(t *testing.T) {
	rep := Find([]*store.Corpus{
		corpus("facebook",
			document("f1", "", "2020-01-01", straight),
			document("f2", "", "2020-01-01", straight)),
	}, ranked())

	if len(rep.Groups) != 0 {
		t.Errorf("want no groups within one corpus, got %d", len(rep.Groups))
	}
}

func TestDateWindowIsRespected(t *testing.T) {
	opts := ranked()
	opts.Window = 2

	inside := Find([]*store.Corpus{
		corpus("facebook", document("f1", "", "2020-01-01", straight)),
		corpus("html", document("h1", "A Post", "2020-01-03", straight)),
	}, opts)
	if len(inside.Groups) != 1 {
		t.Errorf("two days apart should match, got %d groups", len(inside.Groups))
	}

	outside := Find([]*store.Corpus{
		corpus("facebook", document("f1", "", "2020-01-01", straight)),
		corpus("html", document("h1", "A Post", "2020-01-05", straight)),
	}, opts)
	if len(outside.Groups) != 0 {
		t.Errorf("four days apart should not match, got %d groups", len(outside.Groups))
	}
}

// The measured shape of the real corpus: a Substack post displacing an HTML post
// that is itself displacing a Facebook post. 23 of the 26 overlapping Substack
// documents look like this, so the pass must resolve the chain into one group
// rather than reporting two or three overlapping pairs.
func TestThreeWayChainIsOneGroup(t *testing.T) {
	rep := Find([]*store.Corpus{
		corpus("facebook", document("f1", "", "2020-01-01", straight)),
		corpus("html", document("h1", "A Post", "2020-01-01", straight)),
		corpus("substack", document("s1", "A Post", "2020-01-02", curly)),
	}, ranked())

	if len(rep.Groups) != 1 {
		t.Fatalf("want 1 group for a three-way chain, got %d", len(rep.Groups))
	}
	g := rep.Groups[0]
	if len(g.Members) != 3 {
		t.Fatalf("want 3 members, got %d", len(g.Members))
	}
	if g.Winner != "s1" {
		t.Errorf("winner = %q, want s1", g.Winner)
	}
}

func TestUndatedDocumentsAreSkipped(t *testing.T) {
	undated := doc.Document{ID: "f1", Text: straight}
	rep := Find([]*store.Corpus{
		corpus("facebook", undated),
		corpus("html", document("h1", "A Post", "2020-01-01", straight)),
	}, ranked())

	if len(rep.Groups) != 0 {
		t.Errorf("an undated document cannot be windowed, so it should not group")
	}
	if rep.Compared != 1 {
		t.Errorf("Compared = %d, want 1", rep.Compared)
	}
}

// The report is written to be diffed, so the same corpus must produce the same
// groups in the same order however the corpora and documents are ordered going
// in. This is the determinism contract applied to this pass.
func TestReportIsDeterministic(t *testing.T) {
	build := func(reverse bool) []*store.Corpus {
		fb := corpus("facebook",
			document("f1", "", "2020-01-01", straight),
			document("f2", "", "2021-06-06", unrelated))
		hs := corpus("html",
			document("h1", "A Post", "2020-01-01", straight),
			document("h2", "Bread", "2021-06-06", unrelated))
		sb := corpus("substack", document("s1", "A Post", "2020-01-02", curly))
		if reverse {
			return []*store.Corpus{sb, hs, fb}
		}
		return []*store.Corpus{fb, hs, sb}
	}

	a := Find(build(false), ranked())
	b := Find(build(true), ranked())

	if len(a.Groups) != len(b.Groups) {
		t.Fatalf("group counts differ: %d vs %d", len(a.Groups), len(b.Groups))
	}
	for i := range a.Groups {
		ga, gb := a.Groups[i], b.Groups[i]
		if ga.Winner != gb.Winner {
			t.Errorf("group %d winner differs: %q vs %q", i, ga.Winner, gb.Winner)
		}
		if len(ga.Members) != len(gb.Members) {
			t.Fatalf("group %d member counts differ", i)
		}
		for j := range ga.Members {
			if ga.Members[j].DocID != gb.Members[j].DocID {
				t.Errorf("group %d member %d differs: %q vs %q",
					i, j, ga.Members[j].DocID, gb.Members[j].DocID)
			}
		}
	}
}

func TestGroupsAreSortedByDate(t *testing.T) {
	rep := Find([]*store.Corpus{
		corpus("facebook",
			document("f1", "", "2021-06-06", unrelated),
			document("f2", "", "2020-01-01", straight)),
		corpus("html",
			document("h1", "Bread", "2021-06-06", unrelated),
			document("h2", "A Post", "2020-01-01", straight)),
	}, ranked())

	if len(rep.Groups) != 2 {
		t.Fatalf("want 2 groups, got %d", len(rep.Groups))
	}
	first := rep.Groups[0].Members[0].Date
	second := rep.Groups[1].Members[0].Date
	if !first.Before(second) {
		t.Errorf("groups are not in date order: %s then %s", first, second)
	}
}

func TestLowestReportsTheWeakestMember(t *testing.T) {
	// Lightly edited rather than rewritten: a few words changed in the tail, the
	// way a post actually differs between two of these corpora. Far enough from
	// the original to score below 1, close enough to stay above the threshold.
	partial := "I don't think the argument holds up. It's the same claim dressed " +
		"differently, and we've heard it before. The trouble isn't that it's wrong, " +
		"it's that nobody has troubled to check whether it holds."

	rep := Find([]*store.Corpus{
		corpus("facebook", document("f1", "", "2020-01-01", straight)),
		corpus("html", document("h1", "A Post", "2020-01-01", partial)),
	}, ranked())

	if len(rep.Groups) != 1 {
		t.Fatalf("want 1 group, got %d", len(rep.Groups))
	}
	g := rep.Groups[0]
	if g.Lowest >= 1 {
		t.Errorf("Lowest = %.2f, want below 1 for an edited copy", g.Lowest)
	}
	if g.Lowest < DefaultThreshold {
		t.Errorf("Lowest = %.2f, below the threshold it was grouped by", g.Lowest)
	}
}
