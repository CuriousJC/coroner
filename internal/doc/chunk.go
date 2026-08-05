package doc

import (
	"fmt"
	"strings"
)

// Chunking constants. These are the retrieval unit's shape, and changing any of
// them changes every chunk ID in every digested corpus -- which is why they are
// constants recorded in the digest manifest rather than flags. A corpus digested
// under one set and searched under another would silently rank badly.
const (
	// TargetChars is the size a chunk aims for. Paragraphs accumulate until
	// adding the next one would exceed it.
	//
	// Roughly 250-300 words. Small enough that a single embedding still points
	// at something specific -- the failure mode being avoided is one vector for
	// a 2,000-word essay, which averages every idea in it into a vector that is
	// near everything and close to nothing.
	TargetChars = 1200

	// MaxChars is the hard ceiling. Only a single paragraph longer than this
	// gets split internally, which is rare in practice and ugly when it happens.
	MaxChars = 2000

	// MinChars is the floor below which a trailing chunk is folded back into
	// the previous one rather than left to stand alone. A two-line orphan chunk
	// retrieves badly and clutters results.
	MinChars = 200
)

// Split breaks a document's normalised text into chunks.
//
// The rules, in order:
//
//   - Split on blank lines, which Normalise has already made canonical.
//   - Accumulate paragraphs until the next would push past TargetChars.
//   - A single paragraph longer than MaxChars is split on sentence boundaries,
//     and if that is not enough, on whitespace.
//   - A final chunk shorter than MinChars is merged back into its predecessor.
//
// There is no overlap between chunks. Overlap would improve recall for an idea
// that straddles a boundary, at the cost of duplicating text in the results and
// embedding more of it; it can be added later as a constant, but starting
// without it keeps the mapping from text to chunks obvious.
//
// A document whose text is empty produces no chunks, and callers should expect
// that: an export full of image-only posts is mostly empty documents.
func Split(d Document) []Chunk {
	if strings.TrimSpace(d.Text) == "" {
		return nil
	}

	spans := paragraphSpans(d.Text)

	var out []Chunk
	var cur []span

	flush := func() {
		if len(cur) == 0 {
			return
		}
		start, end := cur[0].start, cur[len(cur)-1].end
		out = append(out, newChunk(d, len(out), start, end))
		cur = nil
	}

	for _, p := range spans {
		if p.len() > MaxChars {
			// An oversized paragraph cannot join anything; emit what is pending
			// and then break the paragraph up on its own.
			flush()
			for _, s := range splitLong(d.Text, p) {
				out = append(out, newChunk(d, len(out), s.start, s.end))
			}
			continue
		}

		// Flush before adding, not after: a chunk overshoots TargetChars only
		// by whatever the paragraph that filled it was worth, never by a whole
		// extra paragraph.
		if len(cur) > 0 && p.end-cur[0].start > TargetChars {
			flush()
		}
		cur = append(cur, p)
	}
	flush()

	// Fold a runt tail into its predecessor. Done after the fact rather than by
	// look-ahead because it only ever affects the last chunk, and merging is
	// cheaper to reason about than a chunker that peeks.
	if n := len(out); n > 1 && len(out[n-1].Text) < MinChars {
		merged := newChunk(d, n-2, out[n-2].Start, out[n-1].End)
		out = append(out[:n-2], merged)
	}

	return out
}

func newChunk(d Document, index, start, end int) Chunk {
	text := strings.Trim(d.Text[start:end], "\n ")
	return Chunk{
		ID:    fmt.Sprintf("%s#%d", d.ID, index),
		DocID: d.ID,
		Index: index,
		Start: start,
		End:   end,
		Text:  text,
		Hash:  Hash(text),
	}
}

// span is a half-open byte range within a document's text.
type span struct{ start, end int }

func (s span) len() int { return s.end - s.start }

// paragraphSpans locates paragraphs as offsets rather than substrings, so that
// Chunk.Start and Chunk.End stay meaningful against the original text.
func paragraphSpans(text string) []span {
	var out []span

	start := 0
	for i := 0; i+1 < len(text); i++ {
		if text[i] == '\n' && text[i+1] == '\n' {
			if i > start {
				out = append(out, span{start, i})
			}
			start = i + 2
			i++
		}
	}
	if start < len(text) {
		out = append(out, span{start, len(text)})
	}

	return out
}

// splitLong breaks an oversized paragraph into pieces no longer than MaxChars,
// preferring sentence ends and falling back to whitespace.
func splitLong(text string, p span) []span {
	var out []span

	start := p.start
	for start < p.end {
		end := start + MaxChars
		if end >= p.end {
			out = append(out, span{start, p.end})
			break
		}

		// Look back from the ceiling for the last sentence end, then for the
		// last space. The window is half a chunk: further back than that and
		// the split is worse than an arbitrary one.
		cut := lastSentenceEnd(text, start+MaxChars/2, end)
		if cut < 0 {
			cut = strings.LastIndex(text[start:end], " ")
			if cut < 0 {
				cut = end - start
			} else {
				cut++
			}
			cut += start
		}

		out = append(out, span{start, cut})
		start = cut
	}

	return out
}

// lastSentenceEnd returns the offset just past the final sentence terminator in
// text[from:to], or -1 when there is none.
func lastSentenceEnd(text string, from, to int) int {
	for i := to - 1; i > from; i-- {
		switch text[i] {
		case '.', '!', '?', '\n':
			if i+1 < len(text) && (text[i+1] == ' ' || text[i+1] == '\n') {
				return i + 2
			}
			if i+1 == len(text) {
				return i + 1
			}
		}
	}
	return -1
}
