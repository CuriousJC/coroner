package parse

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/curiousjc/coroner/internal/doc"
)

// goodreadsParser reads a Goodreads library export: one CSV, one row per book.
//
// Only the reviews are writing. The rest of the file is a library -- shelves,
// ISBNs, page counts -- and a book shelved without a review is a catalogue
// entry rather than an empty post, so those rows produce nothing.
//
// It does not implement RecordCounter. Most of a library is unreviewed books,
// so rows-without-text would trip the quiet-corpus warning on every healthy
// digest; required columns catch the realistic silent failure instead.
type goodreadsParser struct{}

// Include names the file Goodreads writes. Globbed so that a browser's
// "goodreads_library_export (1).csv" on a second download is still found.
func (p *goodreadsParser) Include() []string {
	return []string{"goodreads_library_export*.csv"}
}

// Prepare has nothing to do: the export is one self-describing file, which
// ParseFile reads whole.
func (p *goodreadsParser) Prepare(src Source) error { return nil }

// goodreadsColumns are the columns a document is built from. A missing one is an
// error rather than an empty field, because a renamed "My Review" column would
// otherwise parse cleanly to zero documents.
var goodreadsColumns = []string{"Book Id", "Title", "Author", "My Rating", "My Review", "Date Read"}

func (p *goodreadsParser) ParseFile(src Source, rel string, data []byte) ([]doc.Document, error) {
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1

	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("goodreads: parsing %s: %w", rel, err)
	}
	if len(rows) < 1 {
		return nil, fmt.Errorf("goodreads: %s is empty", rel)
	}

	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.TrimSpace(h)] = i
	}
	for _, required := range goodreadsColumns {
		if _, ok := col[required]; !ok {
			return nil, fmt.Errorf("goodreads: %s has no %q column", rel, required)
		}
	}

	field := func(row []string, key string) string {
		i := col[key]
		if i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}

	var out []doc.Document
	for _, row := range rows[1:] {
		review := field(row, "My Review")
		if review == "" {
			continue
		}

		// Book Id is the native key: unique per row, one review per book, so an
		// edited review keeps its ID. Without one there is no stable identity,
		// and a content-hash fallback would renumber the review on every edit.
		id := field(row, "Book Id")
		if id == "" {
			return nil, fmt.Errorf("goodreads: %s has a review with no Book Id", rel)
		}

		text, err := goodreadsText(review)
		if err != nil {
			return nil, fmt.Errorf("goodreads: %s, book %s: %w", rel, id, err)
		}

		d := doc.New(src.Name, src.Type, id, rel, doc.Document{
			Title:  goodreadsTitle(field(row, "Title"), field(row, "Author"), field(row, "My Rating")),
			Author: src.Author,

			// The export records no date for the review itself. Date Read is the
			// nearest thing, and agrees with the review's Facebook cross-post
			// when there is one. Date Added is when the book was shelved, often
			// years earlier, so a review with no Date Read carries no date rather
			// than that one.
			Published: parseTime(field(row, "Date Read")),

			// No URL: the export has no review permalink, and the book's page is
			// not this document.
			Text: text,
		})

		if d.Text == "" {
			continue
		}
		out = append(out, d)
	}

	return out, nil
}

// goodreadsText renders a review's markup as plain text. Reviews are HTML
// fragments: <br/><br/> is the paragraph break, and the text inside the
// occasional <b>, <a>, <s> and <blockquote> is all kept -- struck-through text
// included, since it reads as part of the review.
func goodreadsText(review string) (string, error) {
	root, err := html.Parse(strings.NewReader(review))
	if err != nil {
		return "", err
	}
	return ExtractText(contentRoot(root)), nil
}

// goodreadsTitle names the review after the book, its author and the star
// rating, as Goodreads shows them.
//
// The author is in the title because doc.Indexed prepends the title to every
// chunk, so a search for an author's name finds reviews that never mention
// them. The rating is drawn in star glyphs rather than written as a number: the
// search tokeniser keeps only letters and digits, so glyphs cannot make every
// review match a query for "4" or "stars".
//
// Whitespace is collapsed because the export pads some names with a double
// space where a middle name would go.
func goodreadsTitle(title, author, rating string) string {
	title = strings.Join(strings.Fields(title), " ")
	author = strings.Join(strings.Fields(author), " ")

	var out string
	switch {
	case title == "":
		out = author
	case author == "":
		out = title
	default:
		out = title + " by " + author
	}

	if stars := goodreadsStars(rating); stars != "" {
		out += " " + stars
	}
	return strings.TrimSpace(out)
}

// goodreadsStars draws a 1-5 rating as five star slots. The export writes a
// rating as "4.0", and an unrated book as "0" or "0.0", which draws nothing.
func goodreadsStars(rating string) string {
	f, err := strconv.ParseFloat(rating, 64)
	if err != nil {
		return ""
	}
	n := int(math.Round(f))
	if n < 1 || n > 5 {
		return ""
	}
	return strings.Repeat("★", n) + strings.Repeat("☆", 5-n)
}
