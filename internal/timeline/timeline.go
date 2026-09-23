// Package timeline is every piece of writing in the digested corpus, in one
// date-ordered list: the model behind `coroner export` and `coroner serve`.
//
// Writing that exists in more than one corpus appears once. The copy the dupes
// pass names as the winner is the entry, and the others are listed on it as
// copies, so a review cross-posted to Facebook is read once rather than twice
// in a row.
//
// Everything here is a function of the digested directory alone -- no wall
// clock, no map iteration order -- so the same corpus renders the same bytes.
package timeline

import (
	"sort"
	"time"

	"github.com/curiousjc/coroner/internal/doc"
	"github.com/curiousjc/coroner/internal/dupes"
	"github.com/curiousjc/coroner/internal/store"
)

// Timeline is the whole corpus as one list.
type Timeline struct {
	Sources []Source `json:"sources"`

	// Entries are newest first, with undated writing after the oldest. Ties
	// break on corpus then document ID, so the order is total.
	Entries []Entry `json:"entries"`

	// Documents counts every digested document, and Folded the ones that are
	// listed as a copy on another entry rather than as entries themselves.
	Documents int `json:"documents"`
	Folded    int `json:"folded"`
}

// Source is one corpus's share of the timeline.
type Source struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Priority int    `json:"priority"`

	// Entries counts the entries this corpus supplies, which excludes its
	// documents folded into another corpus's copy.
	Entries int `json:"entries"`
}

// Entry is one piece of writing.
type Entry struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Type      string    `json:"type"`
	Title     string    `json:"title,omitempty"`
	URL       string    `json:"url,omitempty"`
	Published time.Time `json:"published,omitzero"`
	Words     int       `json:"words"`
	Text      string    `json:"text"`

	// Copies are the same writing in other corpora, strongest match first.
	Copies []Copy `json:"copies,omitempty"`
}

// Copy is another corpus's version of an entry.
type Copy struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Published time.Time `json:"published,omitzero"`
	Score     float64   `json:"score"`
}

// Build assembles the timeline from every digested corpus. The manifest supplies
// each corpus's priority, which decides which copy of duplicated writing is the
// entry.
func Build(corpora []*store.Corpus, man *store.Manifest) Timeline {
	opts := dupes.Defaults()
	opts.Priority = map[string]int{}
	types := map[string]string{}
	if man != nil {
		for _, si := range man.Sources {
			opts.Priority[si.Name] = si.Priority
			types[si.Name] = si.Type
		}
	}

	rep := dupes.Find(corpora, opts)

	copies := map[string][]Copy{}
	folded := map[string]bool{}
	for _, g := range rep.Groups {
		for _, m := range g.Members {
			if m.DocID == g.Winner {
				continue
			}
			folded[m.DocID] = true
			copies[g.Winner] = append(copies[g.Winner], Copy{
				ID:        m.DocID,
				Source:    m.Source,
				Published: m.Date,
				Score:     m.Score,
			})
		}
	}

	var t Timeline
	perSource := map[string]int{}

	for _, c := range corpora {
		for i := range c.Docs {
			d := &c.Docs[i]
			t.Documents++
			if folded[d.ID] {
				t.Folded++
				continue
			}
			perSource[c.Name]++
			t.Entries = append(t.Entries, entryOf(d, copies[d.ID]))

			if types[c.Name] == "" {
				types[c.Name] = d.SourceType
			}
		}
	}

	sort.Slice(t.Entries, func(i, j int) bool { return newerFirst(&t.Entries[i], &t.Entries[j]) })

	for _, c := range corpora {
		t.Sources = append(t.Sources, Source{
			Name:     c.Name,
			Type:     types[c.Name],
			Priority: opts.Priority[c.Name],
			Entries:  perSource[c.Name],
		})
	}
	sort.Slice(t.Sources, func(i, j int) bool { return t.Sources[i].Name < t.Sources[j].Name })

	return t
}

func entryOf(d *doc.Document, cs []Copy) Entry {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].Score != cs[j].Score {
			return cs[i].Score > cs[j].Score
		}
		if cs[i].Source != cs[j].Source {
			return cs[i].Source < cs[j].Source
		}
		return cs[i].ID < cs[j].ID
	})

	return Entry{
		ID:        d.ID,
		Source:    d.Source,
		Type:      d.SourceType,
		Title:     d.Title,
		URL:       d.URL,
		Published: d.Published,
		Words:     d.Words,
		Text:      d.Text,
		Copies:    cs,
	}
}

// newerFirst orders entries newest first, undated last.
func newerFirst(a, b *Entry) bool {
	az, bz := a.Published.IsZero(), b.Published.IsZero()
	if az != bz {
		return bz
	}
	if !a.Published.Equal(b.Published) {
		return a.Published.After(b.Published)
	}
	if a.Source != b.Source {
		return a.Source < b.Source
	}
	return a.ID < b.ID
}

// Dated and Undated split the entries, keeping their order.
func (t Timeline) Dated() []Entry   { return t.split(false) }
func (t Timeline) Undated() []Entry { return t.split(true) }

func (t Timeline) split(undated bool) []Entry {
	var out []Entry
	for _, e := range t.Entries {
		if e.Published.IsZero() == undated {
			out = append(out, e)
		}
	}
	return out
}
