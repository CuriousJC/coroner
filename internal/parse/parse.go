// Package parse turns exports into documents.
//
// One parser per export format, selected by the corpus manifest's type. A
// parser's whole job is to get from whatever the export produced to normalised
// text plus whatever metadata it can honestly recover, and to do it the same way
// every time.
//
// The two-phase shape -- Prepare, then ParseFile -- exists because formats
// differ in where they keep their metadata. A directory of loose HTML is
// self-describing file by file. A Substack export keeps titles and dates in a
// posts.csv beside the HTML, so something has to read that first. Prepare runs
// once; after it returns the parser must be read-only, because ParseFile is
// called from several goroutines at once.
package parse

import (
	"fmt"
	"sort"

	"github.com/curiousjc/coroner/internal/doc"
)

// Source is what a parser is told about the corpus it is reading.
type Source struct {
	// Dir is the source directory, an absolute or working-directory-relative
	// path. Parsers that need to read a sidecar file resolve against it.
	Dir string

	// Name is the corpus name, and part of every document ID.
	Name string

	// Type is the manifest type, carried onto documents.
	Type string

	// Author is the manifest's author, used when the export does not name one.
	Author string
}

// Parser reads one export format.
type Parser interface {
	// Include is the default set of globs this parser wants, relative to the
	// source directory, used when the manifest specifies none.
	Include() []string

	// Prepare runs once before any ParseFile call. Parsers with no sidecar
	// metadata return nil.
	Prepare(src Source) error

	// ParseFile turns one file's bytes into zero or more documents. Returning
	// none is normal and not an error: an export is full of files that contain
	// no writing.
	//
	// rel is the file's path relative to the source directory, always with
	// forward slashes, so that a document's provenance does not depend on which
	// platform digested it.
	ParseFile(src Source, rel string, data []byte) ([]doc.Document, error)
}

// constructors is the registry. Parsers are built fresh for each run rather
// than shared, so that whatever Prepare caches belongs to one digest and cannot
// leak between corpora.
var constructors = map[string]func() Parser{
	"text":     func() Parser { return &textParser{} },
	"html":     func() Parser { return &htmlParser{} },
	"facebook": func() Parser { return &facebookParser{} },
	"substack": func() Parser {
		return &pendingParser{format: "substack", waitingFor: "a Substack export zip, expanded"}
	},
}

// RecordCounter is implemented by parsers whose files hold many records, so a
// digest can say how much of an export contained no writing.
//
// Optional, because it only means something for those formats. A directory of
// HTML files maps one file to one document, and the file counts already say
// everything there is to say. A Facebook export is a single JSON array holding
// thousands of records, most of which may legitimately be photographs with no
// text -- and without this, "one file parsed, 5,653 documents" gives no way to
// tell that from a parser that has silently started dropping things.
type RecordCounter interface {
	// Records reports how many records were read and how many produced no
	// document.
	Records() (seen, empty int)
}

// New builds the parser for a manifest type.
func New(sourceType string) (Parser, error) {
	c, ok := constructors[sourceType]
	if !ok {
		return nil, fmt.Errorf("unknown source type %q; known types are %v", sourceType, Types())
	}
	return c(), nil
}

// Types lists the registered types, sorted so that error messages and help text
// do not shuffle between runs.
func Types() []string {
	out := make([]string, 0, len(constructors))
	for t := range constructors {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Implemented reports whether a type has a real parser behind it yet, so the
// digester can fail early and clearly rather than after walking the export.
func Implemented(sourceType string) bool {
	p, err := New(sourceType)
	if err != nil {
		return false
	}
	_, pending := p.(*pendingParser)
	return !pending
}
