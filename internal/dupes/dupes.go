// Package dupes finds the same piece of writing recorded in more than one
// corpus, and says which copy should be treated as the real one.
//
// It exists because the corpora overlap heavily and none of them knows it. The
// hand-built HTML site is almost entirely a curated copy of Facebook posts, and
// Substack revisits older material about a fifth of the time. Nothing in the
// exports records that relationship, so it has to be measured.
//
// # Why not ContentHash
//
// Document.ContentHash is exact, and exactness is useless here: the HTML posts
// were copied out of Facebook by hand. Measured on the real corpus, hash
// equality finds 3 of roughly 175 real duplicates in that pair and 0 of 49
// across the two Substack pairs. What actually separates them is date proximity
// plus shingle overlap.
//
// # Why the thresholds are constants
//
// Measured across all three corpus pairs, no document scores between 0.2 and
// 0.5 against its best match. That empty middle is the whole reason this needs
// no evaluation set: any threshold in 0.5-0.8 draws the same line on every
// pair, so there is no judgement call to get wrong and nothing to tune. The
// same measurement showed a +/-2 and a +/-3 day window producing identical
// results, so the narrower one is used.
//
// This package reads a digested corpus and nothing else. It needs no ollama, it
// writes nothing, and it cannot corrupt anything.
package dupes

import (
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/curiousjc/coroner/internal/doc"
	"github.com/curiousjc/coroner/internal/store"
)

// ShingleWords is the shingle length, in words. Five is long enough that an
// ordinary shared phrase does not match and short enough to survive light
// editing between two versions of a post.
const ShingleWords = 5

// DefaultWindow is how many days apart two documents may be dated and still be
// compared. See the package comment: +/-3 was measured and made no difference.
const DefaultWindow = 2

// DefaultThreshold is the Jaccard score at or above which two documents are
// called the same writing. Anywhere in 0.5-0.8 gives the same answer on the
// measured corpus.
const DefaultThreshold = 0.5

// Options tunes the pass. The zero value is not useful; see Defaults.
type Options struct {
	// Window is the date tolerance in days.
	Window int

	// Threshold is the minimum Jaccard score for a match.
	Threshold float64

	// Priority ranks corpora, higher winning. A corpus absent from the map
	// scores zero, which loses to anything ranked.
	Priority map[string]int
}

// Defaults returns the measured options, with no priorities set.
func Defaults() Options {
	return Options{Window: DefaultWindow, Threshold: DefaultThreshold}
}

// Member is one document's place in a group.
type Member struct {
	DocID  string    `json:"doc_id"`
	Source string    `json:"source"`
	Title  string    `json:"title,omitempty"`
	Date   time.Time `json:"date,omitempty"`
	Words  int       `json:"words"`

	// Priority is the corpus rank this member was ranked by, copied here so a
	// report can be read without also reading the manifest.
	Priority int `json:"priority"`

	// Score is this member's shingle overlap with the group's winner. The
	// winner itself scores 1.
	Score float64 `json:"score"`

	// Snippet is the head of the document's text, for a report to show.
	Snippet string `json:"snippet,omitempty"`
}

// Group is one piece of writing, found in more than one corpus.
type Group struct {
	// Winner is the DocID of the copy precedence selects.
	Winner string `json:"winner"`

	// Lowest is the weakest pairwise score anywhere in the group. A group whose
	// Lowest sits near the threshold is the one worth reading by eye, and
	// keeping it means the report can be sorted by how sure it is.
	Lowest float64 `json:"lowest"`

	Members []Member `json:"members"`
}

// Report is the whole pass.
type Report struct {
	// Groups is sorted by date, then by winning document ID, so two runs over
	// the same corpus produce the same file.
	Groups []Group `json:"groups"`

	// Compared counts the documents that were eligible to match: dated, and in
	// a corpus with at least one other corpus to compare against.
	Compared int `json:"compared"`

	// Duplicated counts documents that ended up in a group.
	Duplicated int `json:"duplicated"`

	Window    int     `json:"window_days"`
	Threshold float64 `json:"threshold"`
}

// fold makes two corpora comparable at the character level.
//
// This matters more than it sounds. The corpora disagree about apostrophes
// almost totally -- Substack is 81% typographic, the HTML site 99% straight --
// and since the tokeniser treats ' as a word character, every contraction
// yields a different token on each side. English prose is full of contractions,
// so a five-word shingle spanning one is destroyed. Measured: folding does not
// change which documents are detected, but it lifts 14 of the Substack matches
// from the 0.5-0.8 band into >=0.8, which is the difference between a report
// that reads as confident and one that invites second-guessing.
//
// This is deliberately a comparison-time transform. It must never be applied
// upstream of a document ID: the corpora legitimately differ here, and
// rewriting stored text would renumber every Substack document to fix what is
// only a reporting nicety.
var folded = strings.NewReplacer(
	"’", "'", // right single quote
	"‘", "'", // left single quote
	"“", `"`, // left double quote
	"”", `"`, // right double quote
	"—", "-", // em dash
	"–", "-", // en dash
	" ", " ", // non-breaking space
)

// shingle returns the set of overlapping word n-grams in a document.
//
// A set rather than a count: repetition within one document should not make it
// look more or less like another, and Jaccard over sets is what the thresholds
// above were measured with.
func shingle(text string) map[string]struct{} {
	return shingleOf(text, true)
}

// shingleOf is shingle with the fold made optional. Nothing in the pass passes
// false; the test does, to hold on to the measurement that justifies the fold
// existing at all rather than leaving it as an assertion in a comment.
func shingleOf(text string, foldQuotes bool) map[string]struct{} {
	if foldQuotes {
		text = folded.Replace(text)
	}

	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
	})

	out := make(map[string]struct{})
	if len(words) < ShingleWords {
		if len(words) > 0 {
			out[strings.Join(words, " ")] = struct{}{}
		}
		return out
	}

	for i := 0; i+ShingleWords <= len(words); i++ {
		out[strings.Join(words[i:i+ShingleWords], " ")] = struct{}{}
	}
	return out
}

// jaccard is intersection over union. Empty on either side scores zero rather
// than dividing by zero: a document with no words is not a duplicate of
// anything, including another empty one.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	// Iterate the smaller set; the result is the same either way.
	small, large := a, b
	if len(large) < len(small) {
		small, large = large, small
	}

	shared := 0
	for s := range small {
		if _, ok := large[s]; ok {
			shared++
		}
	}
	return float64(shared) / float64(len(a)+len(b)-shared)
}

// candidate is a document plus the derived things the pass needs, computed once.
type candidate struct {
	d        *doc.Document
	day      int64 // days since epoch, for the window comparison
	shingles map[string]struct{}
	priority int
}

// Find compares every pair of corpora and groups the documents that match.
//
// Only cross-corpus pairs are considered. Duplicates within one corpus mean the
// export listed something twice, which digest already drops; two posts in the
// same corpus that merely resemble each other are the author repeating himself,
// which is writing rather than duplication.
func Find(corpora []*store.Corpus, opts Options) Report {
	rep := Report{Window: opts.Window, Threshold: opts.Threshold, Groups: []Group{}}

	// Build per-corpus candidates, skipping undated documents. A document with
	// no date cannot be windowed, and comparing it against everything would
	// change the cost of the pass from linear-ish to quadratic to find the
	// handful of cases the exports do not actually produce.
	sets := make([][]*candidate, 0, len(corpora))
	for _, c := range corpora {
		var list []*candidate
		for i := range c.Docs {
			d := &c.Docs[i]
			if d.Published.IsZero() {
				continue
			}
			list = append(list, &candidate{
				d:        d,
				day:      d.Published.Unix() / 86400,
				shingles: shingle(d.Text),
				priority: opts.Priority[d.Source],
			})
		}
		// Sorted so the pairing below runs in a fixed order. Go gives no
		// ordering guarantee otherwise and this report must be diffable.
		sort.Slice(list, func(i, j int) bool { return list[i].d.ID < list[j].d.ID })
		sets = append(sets, list)
	}

	if len(sets) > 1 {
		for _, list := range sets {
			rep.Compared += len(list)
		}
	}

	uf := newUnionFind()
	scores := map[[2]string]float64{}

	for i := 0; i < len(sets); i++ {
		for j := i + 1; j < len(sets); j++ {
			byDay := map[int64][]*candidate{}
			for _, c := range sets[j] {
				byDay[c.day] = append(byDay[c.day], c)
			}

			for _, a := range sets[i] {
				for off := -opts.Window; off <= opts.Window; off++ {
					for _, b := range byDay[a.day+int64(off)] {
						s := jaccard(a.shingles, b.shingles)
						if s < opts.Threshold {
							continue
						}
						uf.union(a.d.ID, b.d.ID)
						scores[key(a.d.ID, b.d.ID)] = s
					}
				}
			}
		}
	}

	// Collect members by group root.
	byRoot := map[string][]*candidate{}
	for _, list := range sets {
		for _, c := range list {
			if root, ok := uf.find(c.d.ID); ok {
				byRoot[root] = append(byRoot[root], c)
			}
		}
	}

	for _, members := range byRoot {
		rep.Groups = append(rep.Groups, buildGroup(members, scores, opts))
		rep.Duplicated += len(members)
	}

	sort.Slice(rep.Groups, func(i, j int) bool {
		a, b := rep.Groups[i], rep.Groups[j]
		ad, bd := a.Members[0].Date, b.Members[0].Date
		if !ad.Equal(bd) {
			return ad.Before(bd)
		}
		return a.Winner < b.Winner
	})

	return rep
}

// buildGroup turns a set of matched documents into a reportable group, and
// picks the winner.
//
// Precedence is corpus priority first. Ties are broken by corpus name and then
// document ID rather than by anything clever like length: two corpora ranked
// equally have no stated reason to differ, and an arbitrary-but-fixed rule is
// honest about that where "longest wins" would look like a judgement nobody
// made.
func buildGroup(members []*candidate, scores map[[2]string]float64, opts Options) Group {
	sort.Slice(members, func(i, j int) bool {
		a, b := members[i], members[j]
		if a.priority != b.priority {
			return a.priority > b.priority
		}
		if a.d.Source != b.d.Source {
			return a.d.Source < b.d.Source
		}
		return a.d.ID < b.d.ID
	})

	winner := members[0]
	g := Group{Winner: winner.d.ID, Lowest: 1}

	for _, m := range members {
		score := 1.0
		if m != winner {
			// Prefer the measured pairwise score; recompute when this member
			// reached the group through a third document rather than directly.
			s, ok := scores[key(winner.d.ID, m.d.ID)]
			if !ok {
				s = jaccard(winner.shingles, m.shingles)
			}
			score = s
		}
		if score < g.Lowest {
			g.Lowest = score
		}

		g.Members = append(g.Members, Member{
			DocID:    m.d.ID,
			Source:   m.d.Source,
			Title:    m.d.Title,
			Date:     m.d.Published,
			Words:    m.d.Words,
			Priority: m.priority,
			Score:    score,
			Snippet:  doc.Snippet(m.d.Text, 160),
		})
	}

	return g
}

// key orders a pair of IDs so a score can be looked up from either side.
func key(a, b string) [2]string {
	if a > b {
		a, b = b, a
	}
	return [2]string{a, b}
}

// unionFind groups documents that match transitively. A Substack post can match
// an HTML post which matches a Facebook post, and the measured corpus says that
// three-way chain is the common case rather than the exception -- 23 of the 26
// overlapping Substack documents. Resolving pairs independently would report
// the same piece of writing two or three times.
type unionFind struct {
	parent map[string]string
}

func newUnionFind() *unionFind {
	return &unionFind{parent: map[string]string{}}
}

func (u *unionFind) find(id string) (string, bool) {
	root, ok := u.parent[id]
	if !ok {
		return "", false
	}
	for root != u.parent[root] {
		u.parent[root] = u.parent[u.parent[root]]
		root = u.parent[root]
	}
	return root, true
}

func (u *unionFind) union(a, b string) {
	for _, id := range []string{a, b} {
		if _, ok := u.parent[id]; !ok {
			u.parent[id] = id
		}
	}

	ra, _ := u.find(a)
	rb, _ := u.find(b)
	if ra == rb {
		return
	}
	// Attach the larger root under the smaller so the result does not depend on
	// which order the pairs arrived in.
	if ra > rb {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
}
