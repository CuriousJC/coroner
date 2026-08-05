package parse

import (
	"fmt"

	"github.com/curiousjc/coroner/internal/doc"
)

// pendingParser stands in for a format whose parser has not been written yet.
//
// It is registered rather than absent on purpose. A manifest saying
// `type: facebook` is not a typo and should not be told it is one; the honest
// answer is that coroner knows what that is and cannot read it yet. Writing
// these against a documented format rather than against a real export is how
// you get a parser that is confidently wrong about encoding, which is the one
// class of bug that quietly corrupts every document ID in a corpus.
type pendingParser struct {
	format     string
	waitingFor string
}

func (p *pendingParser) Include() []string { return nil }

func (p *pendingParser) Prepare(src Source) error {
	return fmt.Errorf("the %s parser is not written yet: it is waiting on %s to be written against rather than guessed at", p.format, p.waitingFor)
}

func (p *pendingParser) ParseFile(src Source, rel string, data []byte) ([]doc.Document, error) {
	return nil, fmt.Errorf("the %s parser is not written yet", p.format)
}
