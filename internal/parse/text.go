package parse

import (
	"path"
	"strings"

	"github.com/curiousjc/coroner/internal/doc"
)

// textParser reads loose plain-text and Markdown files, one document per file.
//
// The simplest possible source, and useful as the reference implementation: it
// is what every other parser is doing underneath, once the format-specific
// unwrapping is done.
type textParser struct{}

func (p *textParser) Include() []string {
	return []string{"*.txt", "*.md", "*.markdown"}
}

func (p *textParser) Prepare(src Source) error { return nil }

func (p *textParser) ParseFile(src Source, rel string, data []byte) ([]doc.Document, error) {
	text := string(data)

	title, body := splitLeadingHeading(text)
	if title == "" {
		title = titleFromFilename(rel)
	}

	d := doc.New(src.Name, src.Type, rel, rel, doc.Document{
		Title:  title,
		Author: src.Author,
		Text:   body,
	})

	if d.Text == "" {
		return nil, nil
	}
	return []doc.Document{d}, nil
}

// splitLeadingHeading pulls a Markdown "# Title" off the front, so the title is
// not also the first line of the body. Anything else is left alone: a file that
// does not open with a heading has no title to recover, and inventing one from
// the first sentence would put a fragment of the body in the title of every
// result.
func splitLeadingHeading(text string) (title, body string) {
	trimmed := strings.TrimLeft(text, "\r\n \t")
	if !strings.HasPrefix(trimmed, "# ") {
		return "", text
	}

	line, rest, _ := strings.Cut(trimmed, "\n")
	return strings.TrimSpace(strings.TrimPrefix(line, "# ")), rest
}

// titleFromFilename is the fallback: "2019-my-post.md" reads better as
// "2019 my post" than as a filename, and it is all a bare text file gives us.
func titleFromFilename(rel string) string {
	base := path.Base(rel)
	if i := strings.LastIndex(base, "."); i > 0 {
		base = base[:i]
	}

	base = strings.NewReplacer("_", " ", "-", " ").Replace(base)
	return strings.TrimSpace(strings.Join(strings.Fields(base), " "))
}
