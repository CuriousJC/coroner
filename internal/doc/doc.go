// Package doc is coroner's document model and the normalisation that makes it
// reproducible.
//
// Everything here is deterministic by construction and deliberately free of
// wall-clock time, map iteration and randomness. That is the load-bearing
// property of the whole tool: given the same source bytes and the same coroner
// version, digesting twice must produce byte-identical documents, chunks and
// identifiers. Parsers may do whatever source-specific repair they need, but
// they hand their text to Normalise before anything is hashed, so the hash is
// of a canonical form rather than of whatever encoding the export happened to
// use that day.
package doc

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Document is one piece of writing, whatever it came from: a Facebook status, a
// Substack essay, a loose HTML file.
//
// The JSON tags matter. Documents are stored as JSONL so the digested corpus
// stays greppable, which means the field names are a user-facing surface and
// renaming one invalidates every stored corpus.
type Document struct {
	// ID is stable across re-digests of the same source. See MakeID.
	ID string `json:"id"`

	// Source is the corpus name from the manifest, and part of the ID. Two
	// corpora may legitimately contain the same essay; they get different IDs
	// and the later dedupe pass is what relates them.
	Source     string `json:"source"`
	SourceType string `json:"source_type"`

	// NativeKey is whatever the export calls this document: a Facebook post id,
	// a Substack slug, a relative path. Empty when the source has no such
	// notion, in which case the ID falls back to the content hash.
	NativeKey string `json:"native_key,omitempty"`

	// File is the path the document was read from, relative to the source
	// directory. Provenance only; never part of the ID, because moving an
	// export to a different folder must not renumber the corpus.
	File string `json:"file"`

	Title  string `json:"title,omitempty"`
	Author string `json:"author,omitempty"`
	URL    string `json:"url,omitempty"`

	// Published is the document's own timestamp, zero when the export does not
	// carry one. Always stored UTC so the digested corpus does not depend on
	// the timezone of the machine that built it.
	Published time.Time `json:"published,omitempty"`

	// Text is the normalised plain text. The full text lives in the digested
	// corpus on purpose: it means changing the embedding model later re-embeds
	// from here without needing the original exports at all.
	Text string `json:"text"`

	// ContentHash is sha256 of Text. This is what the dedupe pass compares, and
	// what lets a re-digest tell an unchanged document from an edited one and
	// skip re-embedding it.
	ContentHash string `json:"content_hash"`

	Words int `json:"words"`
}

// Chunk is a passage of a document, and the unit that actually gets embedded
// and retrieved. Search rolls chunk hits back up to documents; see the search
// package for why.
type Chunk struct {
	ID    string `json:"id"`
	DocID string `json:"doc_id"`

	// Index is the chunk's position within its document, 0-based.
	Index int `json:"index"`

	// Start and End are byte offsets into Document.Text, so a chunk can always
	// be located in its source document without re-running the chunker.
	Start int `json:"start"`
	End   int `json:"end"`

	Text string `json:"text"`

	// Hash is sha256 of Text, and the key under which this chunk's vector is
	// reused across digests. Keying on content rather than on position means a
	// document that gains a paragraph only re-embeds the chunks that changed.
	Hash string `json:"hash"`
}

// Normalise puts text into the canonical form everything downstream assumes.
//
// The steps are ordered and each one exists because some export violates it:
//
//   - NFC composition, so "é" written as e+U+0301 hashes the same as U+00E9.
//     Different exports of the same essay genuinely differ this way, and
//     without this they would be different documents with different IDs.
//   - CRLF and CR collapse to LF, since a Windows-produced export and a
//     Unix-produced one otherwise disagree about every line.
//   - Non-breaking spaces and friends become ordinary spaces. HTML exports are
//     full of U+00A0 and it is never meaningful.
//   - Zero-width characters are dropped outright.
//   - Trailing whitespace goes per line, and runs of blank lines collapse to
//     one, which is also what makes paragraph-boundary chunking stable.
func Normalise(s string) string {
	s = norm.NFC.String(s)

	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case isZeroWidth(r):
			// Dropped entirely. See isZeroWidth.
		case r == '\t':
			b.WriteRune(' ')
		case unicode.IsSpace(r):
			// Covers U+00A0, U+2007, U+202F and the rest of the exotic spaces.
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}

	lines := strings.Split(b.String(), "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, ln := range lines {
		ln = strings.TrimRight(ln, " ")
		if ln == "" {
			blank++
			// One blank line is a paragraph break; more than one carries no
			// extra meaning and would make chunk boundaries depend on how
			// generously the exporter spaced things out.
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, ln)
	}

	return strings.Trim(strings.Join(out, "\n"), "\n ")
}

// isZeroWidth reports whether r is one of the invisible characters that turn up
// mid-word in text copied out of a web page: zero-width space, zero-width
// non-joiner, zero-width joiner, and a byte order mark that has drifted into the
// middle of a document.
//
// Written as hex code points rather than rune literals on purpose. As literals
// they are invisible in the source too, and nobody reviewing this file could see
// what the code was actually comparing against.
func isZeroWidth(r rune) bool {
	return r == 0x200B || r == 0x200C || r == 0x200D || r == 0xFEFF
}

// Hash is sha256 in lowercase hex, used for both document and chunk content.
func Hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// MakeID derives a document's stable identifier.
//
// The identity is (source, nativeKey) where the export supplies a key, and
// (source, content) where it does not. Both are hashed together with the source
// name so that the same essay appearing in two corpora gets two IDs -- relating
// those is the dedupe pass's job, not the identifier's.
//
// The tradeoff worth knowing: for a keyless source, fixing a parser bug changes
// the text, which changes the ID. That is accepted. The alternative -- keying on
// file path -- survives parser fixes but breaks when an export is re-downloaded
// into a differently named folder, which is the more common event.
//
// Sixteen hex characters, 64 bits. At the scale this tool is built for, under
// ten thousand documents, a collision is not a thing that happens; the short
// form is worth it because these IDs get read and grepped by hand.
func MakeID(source, nativeKey, contentHash string) string {
	key := nativeKey
	if key == "" {
		key = "sha256:" + contentHash
	}
	sum := sha256.Sum256([]byte(source + "\x00" + key))
	return hex.EncodeToString(sum[:])[:16]
}

// New builds a Document from parsed parts, normalising the text and filling in
// everything derived. Parsers call this rather than assembling a Document by
// hand, so that no parser can accidentally skip normalisation.
func New(source, sourceType, nativeKey, file string, d Document) Document {
	d.Source = source
	d.SourceType = sourceType
	d.NativeKey = nativeKey
	d.File = file

	d.Text = Normalise(d.Text)
	d.Title = strings.TrimSpace(Normalise(d.Title))
	d.Author = strings.TrimSpace(d.Author)

	d.ContentHash = Hash(d.Text)
	d.ID = MakeID(source, nativeKey, d.ContentHash)
	d.Words = len(strings.Fields(d.Text))

	if !d.Published.IsZero() {
		d.Published = d.Published.UTC()
	}

	return d
}

// Indexed is the text a chunk is searched by, as opposed to the text it
// displays. The title is prepended when there is one.
//
// A chunk from the middle of an essay has lost the context that says what the
// essay is about: a passage of pronouns and mid-argument reasoning embeds as
// something close to nothing without it. The title is the cheapest way to give
// it back.
//
// Both retrievers use this, and that matters more than it looks. When only the
// embedder saw titles, a word appearing solely in a title was findable
// semantically and invisible to keyword search -- so `coroner search "Pastry"`
// returned nothing while the same query in hybrid mode worked. Two retrievers
// disagreeing about what text exists is a bug that presents as bad ranking.
func Indexed(d Document, ch Chunk) string {
	if d.Title == "" {
		return ch.Text
	}
	return d.Title + "\n\n" + ch.Text
}

// Snippet is a short single-line rendering of text, for search results.
func Snippet(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}

	// Cut on a word boundary when one is close enough to the limit, so the
	// snippet does not end mid-word for the sake of three characters.
	cut := s[:max]
	if i := strings.LastIndex(cut, " "); i > max-20 {
		cut = cut[:i]
	}
	return cut + "..."
}
