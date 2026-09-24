package timeline

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// JSON writes the timeline as one indented JSON document.
func (t Timeline) JSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(t)
}

// HTML writes the timeline as one self-contained page: newest first, so the
// oldest writing is at the bottom and the page reads upward in the order it was
// written.
//
// The page loads nothing -- no script, no font, no stylesheet from anywhere --
// because it holds the whole private corpus and must not reach out when opened.
// The corpus, length and quote filters are pure CSS for the same reason.
func (t Timeline) HTML(w io.Writer) error {
	return page.Execute(w, t.view())
}

// previewRunes is how much of a long entry shows before it is expanded, and
// longRunes the length past which an entry is collapsed at all. A status update
// shorter than that is shown whole: hiding one line behind a click is noise.
const (
	previewRunes = 500
	longRunes    = 1000
)

type pageView struct {
	Sources []Source
	Lengths []lengthView
	Quotes  int
	Undated []entryView
	Years   []yearView
	Entries int
	Folded  int
	Newest  string
	Oldest  string
	CSS     template.CSS
}

type lengthView struct {
	Name  string
	Label string
	Count int
}

var lengthLabels = map[string]string{
	Short:  "under 250 words",
	Medium: "250 to 999",
	Long:   "1,000 or more",
}

type yearView struct {
	Year   int
	Count  int
	Months []monthView
}

type monthView struct {
	Label   string
	Entries []entryView
}

type entryView struct {
	Entry
	Date    string
	Long    bool
	Preview string
	Also    []copyView
}

type copyView struct {
	Source string
	Date   string
}

func (t Timeline) view() pageView {
	v := pageView{Sources: t.Sources, Entries: len(t.Entries), Folded: t.Folded}

	counts := map[string]int{}
	for _, e := range t.Entries {
		counts[e.Length]++
		if e.Quote {
			v.Quotes++
		}
	}
	for _, l := range Lengths {
		v.Lengths = append(v.Lengths, lengthView{Name: l, Label: lengthLabels[l], Count: counts[l]})
	}

	for _, e := range t.Undated() {
		v.Undated = append(v.Undated, entryViewOf(e))
	}

	dated := t.Dated()
	if len(dated) > 0 {
		v.Newest = day(dated[0].Published)
		v.Oldest = day(dated[len(dated)-1].Published)
	}

	for _, e := range dated {
		y, m := e.Published.Year(), e.Published.Month()
		if len(v.Years) == 0 || v.Years[len(v.Years)-1].Year != y {
			v.Years = append(v.Years, yearView{Year: y})
		}
		yv := &v.Years[len(v.Years)-1]
		label := fmt.Sprintf("%s %d", m, y)
		if len(yv.Months) == 0 || yv.Months[len(yv.Months)-1].Label != label {
			yv.Months = append(yv.Months, monthView{Label: label})
		}
		mv := &yv.Months[len(yv.Months)-1]
		mv.Entries = append(mv.Entries, entryViewOf(e))
		yv.Count++
	}

	v.CSS = filterCSS(t.Sources)
	return v
}

func entryViewOf(e Entry) entryView {
	ev := entryView{Entry: e, Date: day(e.Published)}
	if utf8.RuneCountInString(e.Text) > longRunes {
		ev.Long = true
		ev.Preview = preview(e.Text)
	}
	for _, c := range e.Copies {
		ev.Also = append(ev.Also, copyView{Source: c.Source, Date: day(c.Published)})
	}
	return ev
}

func day(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// preview is the head of a text, cut back to a word boundary and flattened to
// one paragraph, since it sits inside a one-line summary.
func preview(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= previewRunes {
		return s
	}
	cut := []rune(s)[:previewRunes]
	head := string(cut)
	if i := strings.LastIndexByte(head, ' '); i > previewRunes/2 {
		head = head[:i]
	}
	return strings.TrimRight(head, " ,.;:-") + "…"
}

// badgeColours is the palette corpus badges cycle through, as CSS custom
// properties so light and dark mode each define their own.
const badgeColours = 6

// filterCSS is the generated part of the stylesheet: a badge colour per corpus,
// and the rules that hide a corpus, a length bucket or quotes when its checkbox
// is cleared.
//
// Built here rather than in the template because it is CSS generated from data.
// Corpus names are restricted to [a-z0-9_-] by the manifest validator, so they
// are safe as class names and IDs without escaping. Corpus checkboxes are
// f-<name>; the others are fl-<length> and fq, which no corpus ID can match.
func filterCSS(sources []Source) template.CSS {
	var b strings.Builder
	for i, s := range sources {
		fmt.Fprintf(&b, ".s-%s .src{background:var(--badge-%d)}\n", s.Name, i%badgeColours)
		fmt.Fprintf(&b, "body:has(#f-%s:not(:checked)) .s-%s{display:none}\n", s.Name, s.Name)
	}
	for _, l := range Lengths {
		fmt.Fprintf(&b, "body:has(#fl-%s:not(:checked)) .l-%s{display:none}\n", l, l)
	}
	b.WriteString("body:has(#fq:not(:checked)) .q{display:none}\n")
	return template.CSS(b.String())
}

var page = template.Must(template.New("page").Funcs(template.FuncMap{
	"comma": comma,
}).Parse(pageTemplate))

func comma(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

const pageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<meta name="referrer" content="no-referrer">
<title>Everything written</title>
<style>
:root {
  --bg: #fbfaf7; --fg: #1f1d1a; --muted: #6b665e; --rule: #e4e0d8; --card: #ffffff;
  --accent: #8a4b1f;
  --badge-0: #e8d9c6; --badge-1: #d6e3d3; --badge-2: #d8dcec; --badge-3: #eed6d6; --badge-4: #e6e1c4; --badge-5: #d9e6e6;
}
@media (prefers-color-scheme: dark) {
  :root {
    --bg: #171614; --fg: #e8e4dc; --muted: #9a948a; --rule: #2e2b27; --card: #1f1d1a;
    --accent: #e0a36f;
    --badge-0: #4a3b2a; --badge-1: #2f4230; --badge-2: #323852; --badge-3: #4d2f2f; --badge-4: #464126; --badge-5: #2c4545;
  }
}
* { box-sizing: border-box; }
html { scroll-behavior: auto; }
body { margin: 0; background: var(--bg); color: var(--fg);
  font: 17px/1.55 Georgia, "Iowan Old Style", "Palatino Linotype", serif; }
a { color: var(--accent); }
.wrap { max-width: 46rem; margin: 0 auto; padding: 0 16px; }
header.top { padding: 2.5rem 0 1rem; border-bottom: 1px solid var(--rule); }
h1 { font-size: 2rem; margin: 0 0 .25rem; font-weight: normal; }
.summary { color: var(--muted); margin: 0 0 1rem; }
.filters { border: 0; padding: 0; margin: 0 0 .75rem; display: flex; flex-wrap: wrap; gap: .4rem .9rem;
  font: 14px/1.4 system-ui, sans-serif; }
.filters legend { float: left; margin-right: .6rem; color: var(--muted); }
.filters label { white-space: nowrap; cursor: pointer; }
.filters .n { color: var(--muted); }
nav.years { display: flex; flex-wrap: wrap; gap: .2rem .7rem; font: 14px/1.6 system-ui, sans-serif; }
nav.years a { text-decoration: none; }
nav.years span { color: var(--muted); font-size: 12px; }
section.year > h2 { font-size: 1.6rem; font-weight: normal; margin: 2.5rem 0 .5rem;
  padding-bottom: .25rem; border-bottom: 1px solid var(--rule); }
section.month { content-visibility: auto; contain-intrinsic-size: auto 1200px; }
section.month > h3 { font: 600 13px/1 system-ui, sans-serif; letter-spacing: .06em; text-transform: uppercase;
  color: var(--muted); margin: 1.75rem 0 .5rem; }
article.e { background: var(--card); border: 1px solid var(--rule); border-radius: 6px;
  padding: .75rem 1rem; margin: 0 0 .6rem; }
.meta { font: 13px/1.4 system-ui, sans-serif; color: var(--muted); display: flex; gap: .6rem;
  flex-wrap: wrap; align-items: baseline; }
.src { color: var(--fg); padding: 0 .4rem; border-radius: 3px; }
article.e h4 { font-size: 1.05rem; margin: .3rem 0 0; }
.text { white-space: pre-wrap; overflow-wrap: anywhere; margin-top: .4rem; }
details > summary { cursor: pointer; list-style: none; margin-top: .4rem; }
details > summary::-webkit-details-marker { display: none; }
details > summary .more { display: block; font: 13px/1.4 system-ui, sans-serif; color: var(--accent); margin-top: .3rem; }
details[open] > summary .preview { display: none; }
details[open] > summary .more::before { content: "Collapse · "; }
.copies { font: 13px/1.4 system-ui, sans-serif; color: var(--muted); margin: .5rem 0 0; }
footer { color: var(--muted); padding: 2rem 0 4rem; border-top: 1px solid var(--rule); margin-top: 2.5rem; }
{{.CSS}}
</style>
</head>
<body>
<div class="wrap">
<header class="top" id="top">
<h1>Everything written</h1>
<p class="summary">{{comma .Entries}} pieces of writing{{if .Oldest}}, {{.Oldest}} to {{.Newest}}{{end}}. Newest first: the oldest is at the <a href="#beginning">bottom</a>.{{if .Folded}} {{comma .Folded}} more are copies of writing listed once, under its best copy.{{end}}</p>
<fieldset class="filters"><legend>Show</legend>
{{range .Sources}}<label><input type="checkbox" id="f-{{.Name}}" checked> {{.Name}} <span class="n">{{comma .Entries}}</span></label>
{{end}}</fieldset>
<fieldset class="filters"><legend>Length</legend>
{{range .Lengths}}<label><input type="checkbox" id="fl-{{.Name}}" checked> {{.Label}} <span class="n">{{comma .Count}}</span></label>
{{end}}{{if .Quotes}}<label><input type="checkbox" id="fq" checked> quotes <span class="n">{{comma .Quotes}}</span></label>
{{end}}</fieldset>
<nav class="years">{{if .Undated}}<a href="#undated">undated</a>{{end}}{{range .Years}}<a href="#y{{.Year}}">{{.Year}} <span>{{comma .Count}}</span></a>{{end}}</nav>
</header>
<main>
{{if .Undated}}<section class="year" id="undated"><h2>Undated</h2><section class="month">
{{range .Undated}}{{template "entry" .}}{{end}}</section></section>
{{end}}{{range .Years}}<section class="year" id="y{{.Year}}"><h2>{{.Year}}</h2>
{{range .Months}}<section class="month"><h3>{{.Label}}</h3>
{{range .Entries}}{{template "entry" .}}{{end}}</section>
{{end}}</section>
{{end}}</main>
<footer id="beginning"><p>{{if .Oldest}}The beginning: {{.Oldest}}. {{end}}<a href="#top">Back to the newest</a></p></footer>
</div>
</body>
</html>
{{define "entry"}}<article class="e s-{{.Source}} l-{{.Length}}{{if .Quote}} q{{end}}" id="d-{{.ID}}">
<div class="meta">{{if .Date}}<time datetime="{{.Date}}">{{.Date}}</time>{{end}}<span class="src">{{.Source}}</span><span>{{comma .Words}} words</span></div>
{{if .Title}}<h4>{{if .URL}}<a href="{{.URL}}" rel="noreferrer noopener">{{.Title}}</a>{{else}}{{.Title}}{{end}}</h4>{{end}}
{{if .Long}}<details><summary><span class="text preview">{{.Preview}}</span><span class="more">Read all {{comma .Words}} words</span></summary><div class="text">{{.Text}}</div></details>{{else}}<div class="text">{{.Text}}</div>{{end}}
{{if .Also}}<p class="copies">Also in {{range $i, $c := .Also}}{{if $i}}, {{end}}{{$c.Source}}{{if $c.Date}} ({{$c.Date}}){{end}}{{end}}</p>{{end}}
</article>
{{end}}`
