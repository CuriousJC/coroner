package links

import (
	"strings"
	"testing"
	"time"

	"github.com/curiousjc/coroner/internal/parse"
)

func at(y, m, d int) time.Time {
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
}

func sample() []parse.Link {
	return []parse.Link{
		{URL: "https://example.com/a", Comment: "worth reading", Published: at(2019, 3, 1)},
		{URL: "https://www.example.com/b", Published: at(2020, 5, 2)},
		{URL: "https://other.org/c", Comment: "not sure about this", Published: at(2020, 6, 3)},
		{URL: "https://example.com/a", Comment: "shared it again", Published: at(2021, 1, 4)},
	}
}

func TestBuildCountsUniqueAndTotal(t *testing.T) {
	rep := Build("fb", sample(), 2)

	if len(rep.Links) != 4 {
		t.Errorf("links = %d, want 4", len(rep.Links))
	}
	if rep.Unique != 3 {
		t.Errorf("unique = %d, want 3; the same URL shared twice counts once", rep.Unique)
	}
	if rep.Dropped != 2 {
		t.Errorf("dropped = %d, want 2", rep.Dropped)
	}
}

func TestBuildSpansDates(t *testing.T) {
	rep := Build("fb", sample(), 0)

	if got := rep.Earliest.Format("2006-01-02"); got != "2019-03-01" {
		t.Errorf("earliest = %s", got)
	}
	if got := rep.Latest.Format("2006-01-02"); got != "2021-01-04" {
		t.Errorf("latest = %s", got)
	}
}

// TestBuildFoldsWWW keeps one site from appearing as two in the summary, which
// is the whole point of the summary.
func TestBuildFoldsWWW(t *testing.T) {
	rep := Build("fb", sample(), 0)

	for _, d := range rep.Domains {
		if strings.HasPrefix(d.Domain, "www.") {
			t.Errorf("domain %q kept its www prefix", d.Domain)
		}
	}

	if rep.Domains[0].Domain != "example.com" || rep.Domains[0].Count != 3 {
		t.Errorf("top domain = %+v, want example.com with 3", rep.Domains[0])
	}
}

func TestBuildSortsDomainsDeterministically(t *testing.T) {
	// Two domains with equal counts must not swap between runs.
	list := []parse.Link{
		{URL: "https://zebra.com/1", Published: at(2020, 1, 1)},
		{URL: "https://alpha.com/1", Published: at(2020, 1, 2)},
	}

	first := Build("fb", list, 0)
	for i := 0; i < 20; i++ {
		again := Build("fb", list, 0)
		for j := range first.Domains {
			if again.Domains[j] != first.Domains[j] {
				t.Fatalf("run %d ordered domains differently: %+v vs %+v", i, again.Domains, first.Domains)
			}
		}
	}

	// Equal counts break alphabetically.
	if first.Domains[0].Domain != "alpha.com" {
		t.Errorf("tie broken as %q, want alpha.com", first.Domains[0].Domain)
	}
}

func TestMarkdownIsNewestFirst(t *testing.T) {
	md := Build("fb", sample(), 0).Markdown(10)

	i2021 := strings.Index(md, "## 2021")
	i2019 := strings.Index(md, "## 2019")

	if i2021 < 0 || i2019 < 0 {
		t.Fatalf("year headings missing:\n%s", md)
	}
	if i2021 > i2019 {
		t.Error("years are oldest-first; the reason to open this file is usually the recent end")
	}
}

func TestMarkdownIncludesCommentary(t *testing.T) {
	md := Build("fb", sample(), 0).Markdown(10)

	for _, want := range []string{"worth reading", "not sure about this", "https://other.org/c"} {
		if !strings.Contains(md, want) {
			t.Errorf("output is missing %q", want)
		}
	}
}

// TestMarkdownQuotesMultilineComments guards the formatting: an unquoted blank
// line inside a bullet ends the list and the rest of the year renders as prose.
func TestMarkdownQuotesMultilineComments(t *testing.T) {
	list := []parse.Link{{
		URL:       "https://example.com/a",
		Comment:   "first paragraph\n\nsecond paragraph",
		Published: at(2020, 1, 1),
	}}

	md := Build("fb", list, 0).Markdown(10)

	for _, line := range strings.Split(md, "\n") {
		if strings.Contains(line, "paragraph") && !strings.HasPrefix(strings.TrimSpace(line), ">") {
			t.Errorf("comment line is not blockquoted: %q", line)
		}
	}
	if !strings.Contains(md, "  >\n") {
		t.Error("the blank line between paragraphs was not quoted, which ends the list")
	}
}

func TestMarkdownLimitsDomains(t *testing.T) {
	var list []parse.Link
	for i := 0; i < 40; i++ {
		list = append(list, parse.Link{
			URL:       "https://site" + string(rune('a'+i%26)) + string(rune('0'+i/26)) + ".com/x",
			Published: at(2020, 1, 1),
		})
	}

	md := Build("fb", list, 0).Markdown(5)
	if !strings.Contains(md, "more domains") {
		t.Error("the domain list was not truncated and did not say so")
	}
}

func TestMarkdownHandlesUndated(t *testing.T) {
	list := []parse.Link{{URL: "https://example.com/a"}}

	md := Build("fb", list, 0).Markdown(10)
	if !strings.Contains(md, "## Undated") {
		t.Errorf("an undated link did not get its own heading:\n%s", md)
	}
}

func TestMarkdownReportsDropped(t *testing.T) {
	md := Build("fb", sample(), 7).Markdown(10)
	if !strings.Contains(md, "7 recorded links pointed nowhere usable") {
		t.Error("the artefact does not account for links it left out")
	}
}

func TestBuildEmpty(t *testing.T) {
	rep := Build("fb", nil, 0)
	if rep.Unique != 0 || len(rep.Domains) != 0 {
		t.Errorf("empty input produced %+v", rep)
	}

	// Must still render rather than panic.
	if md := rep.Markdown(10); !strings.Contains(md, "0 links") {
		t.Errorf("empty report rendered as %q", md)
	}
}
