// Package links turns the outbound links an export recorded into something a
// person can read.
//
// This is deliberately not part of the corpus. Links are not writing: a URL
// tokenises into fragments that mean nothing to a reader and everything to a
// lexical index -- "https", "www", "com", a hex tracking parameter -- and there
// are thousands of them, so indexing them would degrade every search in order to
// make one kind of lookup work. The artefact stands on its own instead, and what
// makes it worth reading is not the URLs but what the author wrote when sharing
// them.
package links

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/curiousjc/coroner/internal/parse"
)

// Report is a corpus's links, summarised and grouped.
type Report struct {
	Source string       `json:"source"`
	Links  []parse.Link `json:"links"`

	// Dropped counts links the export recorded that pointed nowhere usable.
	Dropped int `json:"dropped"`

	// Unique is how many distinct URLs the list holds. The gap between this and
	// the total is how often something was shared more than once.
	Unique int `json:"unique"`

	Domains []DomainCount `json:"domains"`

	Earliest time.Time `json:"earliest,omitempty"`
	Latest   time.Time `json:"latest,omitempty"`
}

// DomainCount is one host and how often it appeared.
type DomainCount struct {
	Domain string `json:"domain"`
	Count  int    `json:"count"`
}

// Build assembles the report. The links arrive sorted from the parser and stay
// that way; everything derived here is sorted explicitly, because it came out of
// a map.
func Build(source string, list []parse.Link, dropped int) Report {
	rep := Report{Source: source, Links: list, Dropped: dropped}

	seen := map[string]bool{}
	hosts := map[string]int{}

	for _, l := range list {
		if !seen[l.URL] {
			seen[l.URL] = true
			rep.Unique++
		}
		if h := host(l.URL); h != "" {
			hosts[h]++
		}
		if !l.Published.IsZero() {
			if rep.Earliest.IsZero() || l.Published.Before(rep.Earliest) {
				rep.Earliest = l.Published
			}
			if l.Published.After(rep.Latest) {
				rep.Latest = l.Published
			}
		}
	}

	for h, n := range hosts {
		rep.Domains = append(rep.Domains, DomainCount{Domain: h, Count: n})
	}
	sort.Slice(rep.Domains, func(i, j int) bool {
		if rep.Domains[i].Count != rep.Domains[j].Count {
			return rep.Domains[i].Count > rep.Domains[j].Count
		}
		return rep.Domains[i].Domain < rep.Domains[j].Domain
	})

	return rep
}

// host strips the leading "www." so that one site does not appear as two.
func host(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Host), "www.")
}

// Markdown renders the report for reading, newest first.
//
// Newest first because the reason to open this file is usually "what was that
// thing I shared recently", not a chronological read from 2009. The year
// headings make it skimmable either way.
func (r Report) Markdown(topDomains int) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Links shared on %s\n\n", r.Source)

	if len(r.Links) == 1 {
		b.WriteString("1 link")
	} else {
		fmt.Fprintf(&b, "%s links", comma(len(r.Links)))
	}
	if r.Unique != len(r.Links) {
		fmt.Fprintf(&b, ", %s distinct", comma(r.Unique))
	}
	if !r.Earliest.IsZero() {
		fmt.Fprintf(&b, ", from %s to %s",
			r.Earliest.Format("2006-01-02"), r.Latest.Format("2006-01-02"))
	}
	b.WriteString(".\n")

	if r.Dropped > 0 {
		fmt.Fprintf(&b, "\n%s recorded links pointed nowhere usable and were left out.\n", comma(r.Dropped))
	}

	if len(r.Domains) > 0 {
		b.WriteString("\n## Most shared\n\n")
		for i, d := range r.Domains {
			if i >= topDomains {
				fmt.Fprintf(&b, "\n_and %s more domains._\n", comma(len(r.Domains)-topDomains))
				break
			}
			fmt.Fprintf(&b, "- %s — %s\n", d.Domain, comma(d.Count))
		}
	}

	// Newest first, without disturbing the report's own ordering.
	byDate := make([]parse.Link, len(r.Links))
	copy(byDate, r.Links)
	for i, j := 0, len(byDate)-1; i < j; i, j = i+1, j-1 {
		byDate[i], byDate[j] = byDate[j], byDate[i]
	}

	// Not zero: zero is the sentinel for an undated link, so starting there
	// would swallow the "Undated" heading when the first link has no date.
	year := -1

	for _, l := range byDate {
		y := l.Published.Year()
		if l.Published.IsZero() {
			y = 0
		}
		if y != year {
			year = y
			if y == 0 {
				b.WriteString("\n## Undated\n\n")
			} else {
				fmt.Fprintf(&b, "\n## %d\n\n", y)
			}
		}

		date := "?"
		if !l.Published.IsZero() {
			date = l.Published.Format("2006-01-02")
		}

		fmt.Fprintf(&b, "- **%s** — <%s>\n", date, l.URL)
		if l.Title != "" {
			fmt.Fprintf(&b, "  _%s_\n", oneLine(l.Title))
		}
		if l.Comment != "" {
			// Blockquoted so a multi-paragraph comment stays inside its bullet
			// rather than ending the list.
			for _, line := range strings.Split(l.Comment, "\n") {
				if strings.TrimSpace(line) == "" {
					b.WriteString("  >\n")
					continue
				}
				fmt.Fprintf(&b, "  > %s\n", line)
			}
		}
	}

	return b.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func comma(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 || len(s) <= 3 {
		return s
	}

	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
