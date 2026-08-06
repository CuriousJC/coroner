package parse

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/curiousjc/coroner/internal/doc"
)

// facebookParser reads a Facebook "Download your information" export, in its
// JSON form.
//
// The shape, confirmed against a real export rather than documentation: the
// posts file is a flat array of records, each with a unix `timestamp`, a `data`
// array in which at most one entry carries a `post` string, an `attachments`
// array whose media entries may carry a written `description`, and a `title`
// that is chrome rather than a title. See the notes on each of those below --
// every one of them is a decision that looked obvious in the wrong direction
// first.
type facebookParser struct {
	// Counted across every file, so digest can report how much of the export
	// contained no writing. Atomic because ParseFile runs concurrently.
	records atomic.Int64
	empty   atomic.Int64
}

func (p *facebookParser) Include() []string {
	// Only the posts files. An export contains a dozen other JSON files --
	// edit history, album metadata, uncategorized photos -- and none of them
	// hold writing that is not already here. `posts_on_other_pages_and_
	// profiles.json` looks promising and is not: it is media records with no
	// post text at all.
	//
	// The trailing digit is why this is a glob: a large export splits into
	// your_posts__..._1.json, _2.json and so on.
	return []string{"your_posts*.json"}
}

func (p *facebookParser) Prepare(src Source) error { return nil }

// Records reports how many export records were read and how many produced no
// document, so a digest can distinguish an export that is mostly photographs
// from a parser that has quietly stopped working.
func (p *facebookParser) Records() (seen, empty int) {
	return int(p.records.Load()), int(p.empty.Load())
}

func (p *facebookParser) ParseFile(src Source, rel string, data []byte) ([]doc.Document, error) {
	var records []fbRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("parsing %s as a Facebook posts export: %w", rel, err)
	}

	out := make([]doc.Document, 0, len(records))

	for _, r := range records {
		p.records.Add(1)

		text := r.text()
		if text == "" {
			p.empty.Add(1)
			continue
		}

		// No native key exists. The export carries no post identifier, so
		// identity has to be built.
		//
		// Measured on a real export of 8,195 records: timestamps alone collide
		// 327 times, because a batch upload stamps several posts with the same
		// second. Text alone collides 31 times, because people repeat
		// themselves. Together they collide twice, and those two are genuinely
		// the same post recorded twice. So the key is both, and the residue is
		// left for dedupeIDs to drop and report.
		key := fmt.Sprintf("%d:%s", r.Timestamp, doc.Hash(doc.Normalise(text))[:12])

		d := doc.New(src.Name, src.Type, key, rel, doc.Document{
			// Deliberately no Title. The export's `title` field says things
			// like "Justin Crosby updated his status." -- chrome, identical
			// across thousands of records. Since doc.Indexed prepends the
			// title to every chunk before embedding, using it would inject the
			// same boilerplate into every vector in the corpus and flatten
			// exactly the distinctions search exists to find.
			Author:    src.Author,
			Published: time.Unix(r.Timestamp, 0).UTC(),
			Text:      text,
		})

		out = append(out, d)
	}

	return out, nil
}

// fbRecord is one entry in a posts file.
type fbRecord struct {
	Timestamp   int64          `json:"timestamp"`
	Title       string         `json:"title"`
	Data        []fbData       `json:"data"`
	Attachments []fbAttachment `json:"attachments"`
}

type fbData struct {
	Post string `json:"post"`
}

type fbAttachment struct {
	Data []fbAttachmentData `json:"data"`
}

type fbAttachmentData struct {
	Media *fbMedia `json:"media"`
}

type fbMedia struct {
	Description string `json:"description"`
}

// text assembles everything a record actually contains that was written by a
// person, with the encoding repaired.
//
// Photo captions are included because they are writing: on a real export, 1,148
// media descriptions were all hand-written and none were Facebook's generated
// alt text. They are also mostly redundant -- 905 of those 1,148 were byte-for-
// byte the post text they hung under, because Facebook copies a caption into
// both places. Appending them blind would duplicate that text inside its own
// document, where it would inflate every term frequency BM25 computes. Hence the
// deduplication.
func (r fbRecord) text() string {
	var parts []string
	seen := map[string]bool{}

	add := func(s string) {
		s = repairMojibake(strings.TrimSpace(s))
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		parts = append(parts, s)
	}

	for _, d := range r.Data {
		add(d.Post)
	}
	for _, a := range r.Attachments {
		for _, ad := range a.Data {
			if ad.Media != nil {
				add(ad.Media.Description)
			}
		}
	}

	return strings.Join(parts, "\n\n")
}

// repairMojibake undoes the encoding bug in Facebook's export: text that was
// already UTF-8 gets read as latin-1 and encoded to UTF-8 again, so an
// apostrophe arrives as "â€™" and an emoji as four accented letters.
//
// Every rune of such a string lands in U+0000..U+00FF, because that range is the
// whole of latin-1, and each rune stands for exactly one byte of the original.
// Mapping the runes back to bytes and decoding as UTF-8 recovers the text.
//
// The guards matter more than the transformation. A string is only repaired when
// the result is valid UTF-8 *and* the input had a byte above ASCII to explain
// *and* the result actually contains a multi-byte character. Without those, a
// genuine latin-1 name or a plain ASCII post would be mangled by a function
// meant to unmangle. Measured on a real export: 1,200 strings repaired, 5,595
// left untouched, and no mojibake markers surviving anywhere.
//
// This runs before doc.New, so what gets hashed and embedded is the repaired
// text. Getting it wrong after the fact would mean re-digesting the corpus,
// since every document ID is derived from the text.
func repairMojibake(s string) string {
	suspicious := false
	buf := make([]byte, 0, len(s))

	for _, r := range s {
		if r > 0xFF {
			// A character outside latin-1 proves the string was never squeezed
			// through it, so there is nothing here to undo.
			return s
		}
		if r >= 0x80 {
			suspicious = true
		}
		buf = append(buf, byte(r))
	}

	if !suspicious || !utf8.Valid(buf) {
		return s
	}

	// If decoding produced no multi-byte character, the bytes were plain ASCII
	// all along and the "repair" is a coincidence rather than a recovery.
	if utf8.RuneCount(buf) == len(buf) {
		return s
	}

	return string(buf)
}
