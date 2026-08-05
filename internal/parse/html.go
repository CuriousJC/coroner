package parse

import (
	"bytes"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/curiousjc/coroner/internal/doc"
)

// htmlParser reads loose HTML files, one document per file.
//
// A real tokenising parse rather than tag-stripping with regular expressions.
// That is not fastidiousness: stripping breaks differently on comments, on
// attributes containing ">", and on unclosed tags, which means the same essay
// saved by two different tools would extract to different text and hash to
// different documents. Correct parsing is what makes the output a function of
// the content rather than of the markup's tidiness.
type htmlParser struct{}

func (p *htmlParser) Include() []string {
	return []string{"*.html", "*.htm", "*.xhtml"}
}

func (p *htmlParser) Prepare(src Source) error { return nil }

func (p *htmlParser) ParseFile(src Source, rel string, data []byte) ([]doc.Document, error) {
	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		// html.Parse is extremely forgiving and essentially only fails on I/O,
		// but a file it cannot handle is a file we must not silently drop.
		return nil, err
	}

	meta := readMeta(root)

	body := contentRoot(root)
	if body == nil {
		return nil, nil
	}

	text := ExtractText(body)

	title := meta.title
	if title == "" {
		title = titleFromFilename(rel)
	}

	author := meta.author
	if author == "" {
		author = src.Author
	}

	d := doc.New(src.Name, src.Type, rel, rel, doc.Document{
		Title:     title,
		Author:    author,
		URL:       meta.url,
		Published: meta.published,
		Text:      text,
	})

	if d.Text == "" {
		return nil, nil
	}
	return []doc.Document{d}, nil
}

// skipped are elements whose text is markup rather than writing. Dropped
// wholesale, subtree and all.
var skipped = map[atom.Atom]bool{
	atom.Script:   true,
	atom.Style:    true,
	atom.Head:     true,
	atom.Noscript: true,
	atom.Template: true,
	atom.Svg:      true,
	atom.Nav:      true,
	atom.Form:     true,
	atom.Button:   true,
	atom.Select:   true,
	atom.Iframe:   true,
}

// blocks are elements that end a line of text. Without these every paragraph
// would run into the next, which would not only read badly but would destroy
// the paragraph boundaries the chunker splits on.
var blocks = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Li: true, atom.Ul: true, atom.Ol: true, atom.Blockquote: true, atom.Pre: true,
	atom.Tr: true, atom.Table: true, atom.Br: true, atom.Hr: true,
	atom.Header: true, atom.Footer: true, atom.Figcaption: true, atom.Dd: true, atom.Dt: true,
}

// ExtractText renders a node's subtree as plain text, preserving paragraph
// structure and nothing else.
//
// Exported because the Substack and Facebook parsers will need exactly this
// once their formats are written: both wrap HTML bodies inside another
// container format, and the inner extraction is the same problem.
func ExtractText(n *html.Node) string {
	var b strings.Builder
	walkText(n, &b)

	// The walk emits generous newlines and lets normalisation collapse them,
	// rather than trying to be precise about spacing during the walk. Getting
	// this wrong in the walk means paragraph boundaries that depend on nesting
	// depth; getting it wrong here is a cosmetic fix in one place.
	return doc.Normalise(b.String())
}

func walkText(n *html.Node, b *strings.Builder) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(n.Data)
		return
	case html.CommentNode, html.DoctypeNode:
		return
	case html.ElementNode:
		if skipped[n.DataAtom] {
			return
		}
	}

	block := n.Type == html.ElementNode && blocks[n.DataAtom]
	if block {
		b.WriteString("\n\n")
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walkText(c, b)
	}

	if block {
		b.WriteString("\n\n")
	}
}

// contentRoot picks the subtree worth reading: the first <article>, else the
// first <main>, else <body>.
//
// Preferring the semantic container is what keeps site navigation, sidebars and
// footers out of the text. On an export of a hundred pages from one site, the
// boilerplate is otherwise identical on every page, and identical boilerplate is
// exactly what makes every document look similar to every other one -- it would
// flatten the search results the whole tool exists to sharpen.
func contentRoot(root *html.Node) *html.Node {
	if n := firstElement(root, atom.Article); n != nil {
		return n
	}
	if n := firstElement(root, atom.Main); n != nil {
		return n
	}
	if n := firstElement(root, atom.Body); n != nil {
		return n
	}
	return root
}

func firstElement(n *html.Node, a atom.Atom) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := firstElement(c, a); found != nil {
			return found
		}
	}
	return nil
}

// pageMeta is what the document head is willing to tell us.
type pageMeta struct {
	title     string
	author    string
	url       string
	published time.Time
}

// readMeta harvests title, author, canonical URL and publication date.
//
// Open Graph and article: properties are preferred over the <title> element,
// because <title> usually carries the site name as well and that would end up in
// every document's title. The <title> is the fallback, not the first choice.
func readMeta(root *html.Node) pageMeta {
	var m pageMeta
	var titleTag string

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.DataAtom {
			case atom.Title:
				if titleTag == "" {
					titleTag = textOf(n)
				}
			case atom.Meta:
				key := attr(n, "property")
				if key == "" {
					key = attr(n, "name")
				}
				content := attr(n, "content")
				if content == "" {
					break
				}

				switch strings.ToLower(key) {
				case "og:title", "twitter:title":
					if m.title == "" {
						m.title = content
					}
				case "author", "article:author", "og:article:author":
					if m.author == "" {
						m.author = content
					}
				case "og:url":
					if m.url == "" {
						m.url = content
					}
				case "article:published_time", "og:article:published_time", "date", "pubdate", "publish_date":
					if m.published.IsZero() {
						m.published = parseTime(content)
					}
				}
			case atom.Link:
				if strings.EqualFold(attr(n, "rel"), "canonical") && m.url == "" {
					m.url = attr(n, "href")
				}
			case atom.Time:
				if m.published.IsZero() {
					if dt := attr(n, "datetime"); dt != "" {
						m.published = parseTime(dt)
					}
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)

	if m.title == "" {
		m.title = titleTag
	}
	m.title = strings.TrimSpace(m.title)
	m.author = strings.TrimSpace(m.author)

	return m
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, name) {
			return a.Val
		}
	}
	return ""
}

func textOf(n *html.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			b.WriteString(c.Data)
		}
	}
	return strings.TrimSpace(b.String())
}

// timeLayouts are tried in order. Listed longest-and-most-specific first so
// that a string carrying a timezone is not matched by a layout that would
// discard it.
var timeLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05Z0700",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
	"2006/01/02",
	time.RFC1123Z,
	time.RFC1123,
	"January 2, 2006",
	"Jan 2, 2006",
	"2 January 2006",
}

// parseTime is deliberately strict: an unrecognised date becomes a zero time
// rather than a guess. A wrong date is worse than no date, because it silently
// reorders every result sorted by recency.
func parseTime(s string) time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}
	}

	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
