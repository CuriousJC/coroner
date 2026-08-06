// Package examples prints worked usage. Carried over from hecato, where the
// lesson was that a tool nobody can remember the flags for is a tool nobody
// uses.
package examples

import "github.com/curiousjc/coroner/internal/corlog"

// Print writes the examples to the console and the log.
func Print() {
	corlog.Heading(true, "coroner: digest bodies of writing, then search them by idea")
	corlog.Info(true, "")

	section("Getting started", []example{
		{"coroner initconfig", "write a starter coroner.yaml beside the binary"},
		{"coroner initsource -source=source/substack -type=substack", "create a source directory and its manifest"},
		{"", "then drop the export into that directory and digest it"},
	})

	section("Digesting", []example{
		{"coroner digest", "digest every corpus under the source root"},
		{"coroner digest -source=source/substack", "digest one corpus"},
		{"coroner digest -source=source/substack -force", "re-embed even unchanged chunks"},
		{"coroner digest -verbose", "list the files that could not be parsed"},
	})

	section("Searching", []example{
		{"coroner search \"choice\"", "hybrid search: the word and the idea"},
		{"coroner search \"branching logic\" -hits=25", "more results"},
		{"coroner search \"Thatcher\" -mode=lexical", "names are what lexical search is best at"},
		{"coroner search \"the cost of certainty\" -mode=vector", "ideas are what vector search is best at"},
		{"coroner search \"choice\" -source=substack", "one corpus only"},
		{"coroner search \"choice\" -format=json", "machine-readable, for piping somewhere else"},
	})

	section("Looking around", []example{
		{"coroner sources", "what has been digested, and when"},
		{"coroner stats", "counts, date histogram, vocabulary"},
		{"coroner stats -sample=10", "read a few documents spread through a corpus"},
		{"coroner stats -format=json", "the same numbers, machine-readable"},
		{"coroner version", "build metadata"},
		{"", "stats is how you check a corpus parsed properly: a gap in the year"},
		{"", "histogram or a suspiciously tight word count is a parser problem"},
	})

	corlog.Detail(true, "Searching needs ollama running, because that is what turns a query into a")
	corlog.Detail(true, "vector. There is no fallback on purpose: results that silently changed")
	corlog.Detail(true, "depending on whether a daemon was up would be worse than an error.")
	corlog.Detail(true, "  -mode=lexical is the exception, and works without it.")
	corlog.Info(true, "")
}

type example struct {
	cmd  string
	what string
}

func section(title string, rows []example) {
	corlog.Heading(true, "  %s", title)
	for _, r := range rows {
		if r.cmd == "" {
			corlog.Detail(true, "      %s", r.what)
			continue
		}
		corlog.Row(true,
			corlog.Seg(corlog.StylePlain, "    %-46s", r.cmd),
			corlog.Seg(corlog.StyleDim, "  %s", r.what),
		)
	}
	corlog.Info(true, "")
}
