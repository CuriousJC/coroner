package parse

import (
	"bytes"
	"encoding/csv"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/curiousjc/coroner/internal/doc"
)

// goodreadsHeader is the export's header row, all 23 columns, so the parser is
// tested against the column positions it will actually meet.
var goodreadsHeader = []string{
	"Book Id", "Title", "Author", "Author l-f", "Additional Authors", "ISBN", "ISBN13",
	"My Rating", "Publisher", "Binding", "Number of Pages", "Year Published",
	"Original Publication Year", "Date Read", "Date Added", "Bookshelves",
	"Bookshelves with positions", "Exclusive Shelf", "My Review", "Spoiler",
	"Private Notes", "Read Count", "Owned Copies",
}

// goodreadsCSV writes rows through encoding/csv rather than by hand, so the
// quoting matches the export's -- including its `="..."` ISBNs, which only parse
// strictly because Goodreads quotes them properly.
func goodreadsCSV(t *testing.T, header []string, rows ...map[string]string) []byte {
	t.Helper()

	var b bytes.Buffer
	w := csv.NewWriter(&b)
	if err := w.Write(header); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		rec := make([]string, len(header))
		for i, h := range header {
			rec[i] = row[h]
		}
		if err := w.Write(rec); err != nil {
			t.Fatal(err)
		}
	}
	w.Flush()
	return b.Bytes()
}

func goodreadsParse(t *testing.T, data []byte) []doc.Document {
	t.Helper()

	p := &goodreadsParser{}
	src := Source{Dir: ".", Name: "goodreads", Type: "goodreads", Author: "Justin"}
	if err := p.Prepare(src); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	docs, err := p.ParseFile(src, "goodreads_library_export.csv", data)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	return docs
}

func sampleRow() map[string]string {
	return map[string]string{
		"Book Id":    "1000001",
		"Title":      "The Lamplighter (Harbour Lights, #2)",
		"Author":     "Ann Example",
		"ISBN":       `="0000000000"`,
		"My Rating":  "5.0",
		"Date Read":  "2021/03/14",
		"Date Added": "2021/02/01",
		"My Review":  "A slow start.<br/><br/>A strong finish.",
		"Read Count": "1",
	}
}

// Only reviews are writing; a shelved book with no review is a catalogue entry.
func TestGoodreadsReadsOnlyReviewedRows(t *testing.T) {
	shelved := map[string]string{
		"Book Id":    "1000002",
		"Title":      "A Book Not Yet Reviewed",
		"Author":     "Ann Example",
		"My Rating":  "4.0",
		"Date Read":  "2019/05/01",
		"Date Added": "2018/01/01",
	}

	got := goodreadsParse(t, goodreadsCSV(t, goodreadsHeader, shelved, sampleRow()))
	if len(got) != 1 {
		t.Fatalf("want 1 document, got %d", len(got))
	}
	if got[0].NativeKey != "1000001" {
		t.Errorf("parsed the wrong row: native key %q", got[0].NativeKey)
	}
}

// The title carries the book, its author and the star rating, as Goodreads
// shows them. The export pads some names with a double space; that is collapsed
// rather than carried into every chunk.
func TestGoodreadsTitleNamesBookAuthorAndRating(t *testing.T) {
	row := sampleRow()
	row["Author"] = "Ben  Sample"
	row["Title"] = "A Quiet Field"
	row["My Rating"] = "4.0"

	got := goodreadsParse(t, goodreadsCSV(t, goodreadsHeader, row))[0]

	if want := "A Quiet Field by Ben Sample ★★★★☆"; got.Title != want {
		t.Errorf("title = %q, want %q", got.Title, want)
	}
	if got.Author != "Justin" {
		t.Errorf("author = %q, want the manifest's author, not the book's", got.Author)
	}
}

// An unrated book is exported as "0" or "0.0" and draws no stars at all, rather
// than five empty ones that would read as a zero-star review.
func TestGoodreadsUnratedTitleHasNoStars(t *testing.T) {
	for _, rating := range []string{"0", "0.0", ""} {
		row := sampleRow()
		row["My Rating"] = rating

		got := goodreadsParse(t, goodreadsCSV(t, goodreadsHeader, row))[0]
		if want := "The Lamplighter (Harbour Lights, #2) by Ann Example"; got.Title != want {
			t.Errorf("rating %q: title = %q, want %q", rating, got.Title, want)
		}
	}
}

// Nothing about the book outside the review -- rating, shelves, ISBN -- is
// writing, and none of it reaches the text.
func TestGoodreadsTextIsOnlyTheReview(t *testing.T) {
	got := goodreadsParse(t, goodreadsCSV(t, goodreadsHeader, sampleRow()))[0]

	for _, leaked := range []string{"5.0", "0000000000", "Ann Example", "Harbour Lights"} {
		if strings.Contains(got.Text, leaked) {
			t.Errorf("text carries %q from outside the review: %q", leaked, got.Text)
		}
	}
}

// Date Read is the nearest thing the export has to a review date. Date Added is
// when the book was shelved, often years earlier, so a review with no Date Read
// joins Goodreads' own 2012-01-01 placeholder rather than taking that date.
func TestGoodreadsDatesFromDateReadNeverDateAdded(t *testing.T) {
	read := sampleRow()

	dnf := sampleRow()
	dnf["Book Id"] = "1000003"
	dnf["Date Read"] = ""
	dnf["Date Added"] = "2020/10/04"

	got := goodreadsParse(t, goodreadsCSV(t, goodreadsHeader, read, dnf))
	if len(got) != 2 {
		t.Fatalf("want 2 documents, got %d", len(got))
	}

	byKey := map[string]doc.Document{}
	for _, d := range got {
		byKey[d.NativeKey] = d
	}

	want := time.Date(2021, 3, 14, 0, 0, 0, 0, time.UTC)
	if p := byKey["1000001"].Published; !p.Equal(want) {
		t.Errorf("published = %s, want Date Read %s", p, want)
	}
	placeholder := time.Date(2012, 1, 1, 0, 0, 0, 0, time.UTC)
	if p := byKey["1000003"].Published; !p.Equal(placeholder) {
		t.Errorf("a review with no Date Read was dated %s, want %s; Date Added must not stand in", p, placeholder)
	}
}

// Book Id is a native key, so editing a review leaves its ID alone and changes
// only its content hash.
func TestGoodreadsUsesBookIDAsNativeKey(t *testing.T) {
	first := goodreadsParse(t, goodreadsCSV(t, goodreadsHeader, sampleRow()))[0]

	edited := sampleRow()
	edited["My Review"] = "A slow start, rewritten a week later."
	second := goodreadsParse(t, goodreadsCSV(t, goodreadsHeader, edited))[0]

	if first.ID != second.ID {
		t.Errorf("editing the review changed the ID: %s then %s", first.ID, second.ID)
	}
	if first.ContentHash == second.ContentHash {
		t.Error("editing the review left the content hash unchanged")
	}
}

// Reviews are HTML fragments with <br/><br/> as the paragraph break. The breaks
// survive as paragraph boundaries for the chunker, and the text inside every tag
// the export uses survives too.
func TestGoodreadsRendersReviewMarkup(t *testing.T) {
	row := sampleRow()
	row["My Review"] = "<s>First impression.</s><br/><br/>(Second thoughts.)<br/><br/>" +
		"<blockquote>Quoted from an earlier review.</blockquote><br/><br/>" +
		`The best part was <b>chapter one</b>, and see <a href="https://www.goodreads.com/review/show/1">my other review</a>.`

	got := goodreadsParse(t, goodreadsCSV(t, goodreadsHeader, row))[0]

	want := "First impression.\n\n(Second thoughts.)\n\n" +
		"Quoted from an earlier review.\n\n" +
		"The best part was chapter one, and see my other review."
	if got.Text != want {
		t.Errorf("text =\n%q\nwant\n%q", got.Text, want)
	}
}

// A renamed column would otherwise parse cleanly to zero documents.
func TestGoodreadsMissingColumnIsAnError(t *testing.T) {
	var header []string
	for _, h := range goodreadsHeader {
		if h != "My Review" {
			header = append(header, h)
		}
	}

	p := &goodreadsParser{}
	src := Source{Name: "goodreads", Type: "goodreads"}
	_, err := p.ParseFile(src, "goodreads_library_export.csv", goodreadsCSV(t, header, sampleRow()))
	if err == nil {
		t.Fatal("an export with no review column parsed without error")
	}
	if !strings.Contains(err.Error(), "My Review") {
		t.Errorf("the error does not name the missing column: %v", err)
	}
}

// Without a Book Id a review has no stable identity, and a content-hash fallback
// would renumber it on every edit. Refuse rather than guess.
func TestGoodreadsReviewWithoutBookIDIsAnError(t *testing.T) {
	row := sampleRow()
	row["Book Id"] = ""

	p := &goodreadsParser{}
	src := Source{Name: "goodreads", Type: "goodreads"}
	if _, err := p.ParseFile(src, "goodreads_library_export.csv", goodreadsCSV(t, goodreadsHeader, row)); err == nil {
		t.Fatal("a review with no Book Id parsed without error")
	}
}

// The export is one named file. A browser's numbered re-download should still
// be found; a stray spreadsheet dropped beside it should not be read as a
// library.
func TestGoodreadsIncludesOnlyTheExport(t *testing.T) {
	p := &goodreadsParser{}

	matches := func(name string) bool {
		for _, g := range p.Include() {
			if ok, _ := path.Match(g, name); ok {
				return true
			}
		}
		return false
	}

	for _, name := range []string{"goodreads_library_export.csv", "goodreads_library_export (1).csv"} {
		if !matches(name) {
			t.Errorf("%q is not included", name)
		}
	}
	for _, name := range []string{"reading_notes.csv", "posts.csv"} {
		if matches(name) {
			t.Errorf("%q is included", name)
		}
	}
}
