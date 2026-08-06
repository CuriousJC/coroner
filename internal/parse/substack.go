package parse

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/curiousjc/coroner/internal/doc"
)

// substackParser reads a Substack export: posts.csv beside a posts/ directory
// of HTML bodies.
//
// This is the format the two-phase parser interface was designed around. The
// HTML holds only the body of a post -- no title, no date, no <h1> -- and
// everything else lives in posts.csv, so something has to read that once before
// any file is parsed.
//
// Written against a real 139-post export. The numbers in these comments are
// measured; re-measure before simplifying anything they justify.
type substackParser struct {
	// posts maps a post_id to its row in posts.csv. Built in Prepare and only
	// read afterwards, which is what makes it safe for concurrent ParseFile.
	posts map[string]substackPost
}

type substackPost struct {
	title     string
	subtitle  string
	published time.Time
	isDraft   bool
}

// Include is deliberately a narrow allowlist rather than a broad glob with
// exclusions bolted on.
//
// posts/ holds 218 .delivers.csv and .opens.csv files alongside the 139 posts --
// per-subscriber email analytics carrying subscriber addresses, and there is
// another subscriber list at the export root. None of that is writing and none
// of it belongs in a searchable corpus. Naming what to read, rather than what to
// skip, means a future export adding another analytics file cannot quietly start
// indexing addresses.
func (p *substackParser) Include() []string {
	return []string{"posts/*.html"}
}

// Prepare reads posts.csv. Every post's title, subtitle and date comes from
// here; the HTML bodies carry none of it.
func (p *substackParser) Prepare(src Source) error {
	name := filepath.Join(src.Dir, "posts.csv")

	f, err := os.Open(name)
	if err != nil {
		return fmt.Errorf("substack: reading the export's posts.csv: %w", err)
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1

	rows, err := r.ReadAll()
	if err != nil {
		return fmt.Errorf("substack: parsing %s: %w", name, err)
	}
	if len(rows) < 1 {
		return fmt.Errorf("substack: %s is empty", name)
	}

	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.TrimSpace(h)] = i
	}
	for _, required := range []string{"post_id", "post_date", "is_published", "title", "subtitle"} {
		if _, ok := col[required]; !ok {
			return fmt.Errorf("substack: %s has no %q column", name, required)
		}
	}

	field := func(row []string, key string) string {
		i, ok := col[key]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	p.posts = make(map[string]substackPost, len(rows)-1)
	for _, row := range rows[1:] {
		id := field(row, "post_id")
		if id == "" {
			continue
		}
		p.posts[id] = substackPost{
			title:     field(row, "title"),
			subtitle:  field(row, "subtitle"),
			published: parseTime(field(row, "post_date")),
			isDraft:   field(row, "is_published") == "false",
		}
	}

	return nil
}

func (p *substackParser) ParseFile(src Source, rel string, data []byte) ([]doc.Document, error) {
	// The filename is the post_id with .html on the end -- checked across the
	// whole export: 139 rows, 139 files, no row without a file and no file
	// without a row.
	id := strings.TrimSuffix(path.Base(rel), path.Ext(rel))

	meta, ok := p.posts[id]
	if !ok {
		// A body with no row is a body with no title and no date. Better to say
		// so than to invent them.
		return nil, fmt.Errorf("substack: %s has no row in posts.csv", rel)
	}

	// Unpublished drafts are not part of the corpus. On the real export the five
	// drafts are exactly the five posts with no title and no date: two are
	// finished-looking essays and three are scaffolding full of placeholder
	// markers, and nothing but reading them separates the two. Rather than guess
	// which drafts were meant, the published flag is taken at its word -- it is
	// the author's own record of what counts as finished.
	if meta.isDraft {
		return nil, nil
	}

	root, err := html.Parse(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	body := contentRoot(root)
	if body == nil {
		return nil, nil
	}

	// ExtractText is exported for exactly this: the export wraps HTML inside
	// another container format, and the inner extraction is the same problem the
	// generic parser already solves. No unwrapping is needed here, unlike the
	// hand-built site -- these bodies are machine-generated and arrive on a
	// single line.
	text := ExtractText(body)

	// The subtitle is joined to the head of the text rather than dropped.
	// Measured: 117 of 139 posts have one, they are distinct per post, and only
	// one of them already appears in its own body. So it is real writing that
	// exists nowhere else in the corpus, and it reads as the piece's lede.
	//
	// It is not folded into Title, which doc.Indexed prepends to every chunk.
	// Title carries what the post is called; a summary sentence repeated across
	// every chunk of a long post is how the Facebook `title` field flattened
	// vectors, and there is no reason to reintroduce that shape here.
	if meta.subtitle != "" && text != "" {
		text = meta.subtitle + "\n\n" + text
	}

	title := meta.title
	if title == "" {
		// Every untitled post in the real export is a draft, so this is a safety
		// net rather than a path anything currently takes. A published post with
		// no title is still better served by its slug than by nothing.
		title = substackTitleFromID(id)
	}

	d := doc.New(src.Name, src.Type, id, rel, doc.Document{
		Title:     title,
		Author:    src.Author,
		Published: meta.published,
		Text:      text,
	})

	if d.Text == "" {
		return nil, nil
	}
	return []doc.Document{d}, nil
}

// substackIDPrefix matches the numeric post id and separator that begins every
// post_id, leaving the slug.
var substackIDPrefix = regexp.MustCompile(`^\d+\.`)

func substackTitleFromID(id string) string {
	slug := substackIDPrefix.ReplaceAllString(id, "")
	slug = strings.NewReplacer("_", " ", "-", " ").Replace(slug)
	return strings.TrimSpace(strings.Join(strings.Fields(slug), " "))
}
