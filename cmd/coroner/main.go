/*
main executable for coroner

	coroner initconfig
	coroner initsource -source=source/substack -type=substack
	coroner digest
	coroner digest -source=source/substack -verbose
	coroner search "choice"
	coroner search "branching logic" -hits=25 -format=json
	coroner sources
	coroner stats -sample=10
	coroner links -source=source/facebook_posts

// For creating an executable:::

	go build -o coroner.exe cmd/coroner/main.go
	make all

//Stuff todo:::
Planned work lives in TODO.md at the repo root, not here. It was moved out
because a roadmap in a header comment cannot carry the reasoning behind an item,
and an item without its reasoning gets done wrong. In brief: the substack parser
(now unblocked), a third HTML export awaiting bytes, cross-corpus dedupe, and two
search refinements.
*/
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/curiousjc/coroner/internal/config"
	"github.com/curiousjc/coroner/internal/corlog"
	"github.com/curiousjc/coroner/internal/corpus"
	"github.com/curiousjc/coroner/internal/digest"
	"github.com/curiousjc/coroner/internal/doc"
	"github.com/curiousjc/coroner/internal/dupes"
	"github.com/curiousjc/coroner/internal/embed"
	"github.com/curiousjc/coroner/internal/examples"
	"github.com/curiousjc/coroner/internal/links"
	"github.com/curiousjc/coroner/internal/parse"
	"github.com/curiousjc/coroner/internal/search"
	"github.com/curiousjc/coroner/internal/stats"
	"github.com/curiousjc/coroner/internal/store"
	"github.com/curiousjc/coroner/internal/version"
)

var buildContext = "development"

// verbose is the app-level console verbosity, set once the flags are parsed.
// The log file gets everything regardless; this only governs what you are shown
// at the time.
var verbose bool

// Built-in defaults, overridden by the config, overridden by an explicit flag.
const (
	defaultSource   = "source"
	defaultDigested = "digested"
	defaultHits     = 10
)

func main() {
	// A tool that refuses to run because it cannot write a log file beside
	// itself is a tool that cannot be installed under Program Files. Warn and
	// carry on rather than exit, which is a deliberate departure from hecato.
	if logFile, err := corlog.LogSetup(buildContext); err != nil {
		corlog.Discard()
		fmt.Fprintf(os.Stderr, "coroner: continuing without a log file: %v\n", err)
	} else {
		defer logFile.Close()
	}

	if len(os.Args) < 2 {
		corlog.Heading(true, "coroner %s", version.Version)
		corlog.Info(true, "")
		corlog.Warn(true, "No command given, so there is nothing to do yet.")
		corlog.Info(true, "")
		examples.Print()
		os.Exit(1)
	}

	cmd, args := os.Args[1], os.Args[2:]

	switch cmd {
	case "digest":
		run(cmdDigest(args))
	case "search":
		run(cmdSearch(args))
	case "sources":
		run(cmdSources(args))
	case "links":
		run(cmdLinks(args))
	case "stats":
		run(cmdStats(args))
	case "dupes":
		run(cmdDupes(args))
	case "initsource":
		run(cmdInitSource(args))
	case "initconfig":
		run(cmdInitConfig(args))
	case "version":
		version.Print()
	case "examples", "help", "-h", "--help", "-help":
		examples.Print()
	default:
		corlog.Heading(true, "coroner %s", version.Version)
		corlog.Error(true, "Unknown command %q.", cmd)
		corlog.Detail(true, "Commands: digest, search, sources, stats, dupes, links, initsource, initconfig, version, examples")
		corlog.Detail(true, "Try `coroner examples` for worked usage.")
		os.Exit(1)
	}
}

// run is the single exit point for a failing command, so that every command
// reports the same way and none of them call os.Exit halfway through their own
// output.
func run(err error) {
	if err == nil {
		return
	}

	corlog.Info(true, "")
	for i, line := range strings.Split(err.Error(), "\n") {
		if i == 0 {
			corlog.Error(true, "%s", line)
			continue
		}
		corlog.Detail(true, "%s", line)
	}
	os.Exit(1)
}

// commonFlags are the flags every command shares.
type commonFlags struct {
	fs *flag.FlagSet

	config   *string
	digested *string
	verbose  *bool
	noColor  *bool
}

func newFlagSet(name string) *commonFlags {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	c := &commonFlags{fs: fs}

	c.config = fs.String("config", "", "Path to a config file. Defaults to coroner.yaml beside the executable.")
	c.digested = fs.String("digested", defaultDigested, "The searchable corpus directory.")
	c.verbose = fs.Bool("verbose", false, "Show more, including files that could not be parsed.")
	c.noColor = fs.Bool("no-color", false, "Disable coloured output. Colour is off automatically when piped or when NO_COLOR is set.")

	return c
}

// set reports which flags were actually typed, as opposed to sitting at their
// default. This is the only way to tell -hits=10 from the identical default, and
// therefore the only way "an explicit flag beats the config" can work.
func (c *commonFlags) set() map[string]bool {
	out := map[string]bool{}
	c.fs.Visit(func(f *flag.Flag) { out[f.Name] = true })
	return out
}

// reorder moves flags ahead of positional arguments.
//
// Go's flag package stops parsing at the first non-flag argument, which is fine
// for a tool whose arguments are all flags and wrong for this one: the natural
// way to type a search is `coroner search "choice" -hits=25`, and unreordered
// that parses as a query of `choice -hits=25` with the flag silently ignored.
// Silently is the problem -- the search still runs, against the wrong query,
// with the wrong settings.
//
// Whether a flag consumes the next argument depends on whether it is a bool, so
// this asks the FlagSet rather than guessing.
func (c *commonFlags) reorder(args []string) []string {
	var flags, positional []string

	for i := 0; i < len(args); i++ {
		a := args[i]

		// "--" ends flag parsing; everything after it is positional, which is
		// how you search for a phrase that begins with a dash.
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}

		if len(a) < 2 || a[0] != '-' {
			positional = append(positional, a)
			continue
		}

		name := strings.TrimLeft(a, "-")
		name, _, hasValue := strings.Cut(name, "=")

		f := c.fs.Lookup(name)
		if f == nil {
			// Not ours. Hand it to the FlagSet anyway so it produces the usual
			// "flag provided but not defined" error rather than searching for it.
			flags = append(flags, a)
			continue
		}

		flags = append(flags, a)

		// A non-bool flag written without "=" takes the next argument as its
		// value, and that argument must not be mistaken for part of the query.
		if !hasValue && !isBoolFlag(f) && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}

	if len(positional) == 0 {
		return flags
	}

	// The separator has to be re-emitted, not just consumed. Without it a query
	// that legitimately starts with a dash -- which is exactly what the caller
	// wrote "--" to express -- would be parsed as a flag on the way back in.
	return append(append(flags, "--"), positional...)
}

// isBoolFlag reports whether a flag may be written bare, without a value.
func isBoolFlag(f *flag.Flag) bool {
	bf, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && bf.IsBoolFlag()
}

// load parses the arguments and folds in the config file.
func (c *commonFlags) load(args []string) (*config.Config, map[string]bool, error) {
	if err := c.fs.Parse(c.reorder(args)); err != nil {
		return nil, nil, err
	}

	if *c.noColor {
		corlog.DisableColor()
	}

	cfg, err := config.Load(*c.config)
	if err != nil {
		return nil, nil, err
	}

	set := c.set()

	if cfg.Defaults.Digested != "" && !set["digested"] {
		*c.digested = cfg.Defaults.Digested
	}
	if cfg.Defaults.Verbose && !set["verbose"] {
		*c.verbose = true
	}

	verbose = *c.verbose
	return cfg, set, nil
}

// ---------------------------------------------------------------- digest

func cmdDigest(args []string) error {
	c := newFlagSet("digest")
	source := c.fs.String("source", "", "A source directory to digest. Default: every corpus under the source root.")
	root := c.fs.String("source-root", defaultSource, "Where source directories live, when -source is not given.")
	workers := c.fs.Int("workers", digest.DefaultWorkers, "How many files to parse concurrently.")
	force := c.fs.Bool("force", false, "Re-embed every chunk, even ones whose text has not changed.")

	cfg, set, err := c.load(args)
	if err != nil {
		return err
	}
	if cfg.Defaults.Source != "" && !set["source-root"] {
		*root = cfg.Defaults.Source
	}
	if cfg.Defaults.Workers > 0 && !set["workers"] {
		*workers = cfg.Defaults.Workers
	}

	corlog.Heading(true, "coroner %s", version.Version)

	// Which corpora, resolved before anything else so the summary below can
	// describe a run that is actually going to happen.
	dirs, err := resolveSources(*source, *root)
	if err != nil {
		return err
	}

	st, err := store.Open(*c.digested)
	if err != nil {
		return err
	}

	client := embed.New(cfg.Embed.BaseURL, cfg.Embed.Model)

	corlog.Info(true, "")
	corlog.Field(true, "Command", "digest")
	corlog.Field(true, "Sources", strings.Join(relAll(dirs), ", "))
	corlog.Field(true, "Digested", *c.digested)
	corlog.Field(true, "Model", client.Model)
	corlog.Field(true, "Config", configLabel(cfg))

	// Every check that can fail happens before the first byte is embedded.
	// Discovering after twenty minutes of walking that the model is wrong is
	// the sort of thing that makes a tool unusable.
	man, err := st.LoadManifest()
	if err != nil {
		return err
	}
	if err := man.CheckChunking(); err != nil {
		return fmt.Errorf("the chunker has changed since this corpus was digested: %w", err)
	}

	corlog.Info(true, "")
	corlog.Detail(true, "  checking ollama at %s...", client.BaseURL)

	ctx := context.Background()
	info, err := client.Probe(ctx)
	if err != nil {
		return err
	}
	if err := man.CheckEmbed(client.Model, info.Digest, man.Embed.Dimensions); err != nil {
		return err
	}
	corlog.Detail(true, "  ollama has %s (%s)", info.Name, shortDigest(info.Digest))

	started := time.Now()
	var totalDocs, totalChunks, totalEmbedded, totalErrors int

	for _, dir := range dirs {
		corlog.Info(true, "")
		corlog.Info(true, "Digesting %s", filepath.ToSlash(dir))

		rep, err := digest.Run(ctx, digest.Options{
			SourceDir: dir,
			Store:     st,
			Embedder:  client,
			ModelName: client.Model,
			Model:     info,
			Workers:   *workers,
			Verbose:   *c.verbose,
			Force:     *force,
		})
		if err != nil {
			return fmt.Errorf("digesting %s: %w", filepath.ToSlash(dir), err)
		}

		reportDigest(rep)

		totalDocs += rep.Documents
		totalChunks += rep.Chunks
		totalEmbedded += rep.Embedded
		totalErrors += len(rep.Errors)
	}

	corlog.Info(true, "")
	corlog.Success(true, "Digested %s and %s from %s in %s",
		plural(totalDocs, "document", "documents"),
		plural(totalChunks, "chunk", "chunks"),
		plural(len(dirs), "corpus", "corpora"),
		time.Since(started).Round(time.Millisecond))

	if totalEmbedded == 0 && totalChunks > 0 {
		corlog.Detail(true, "  nothing needed embedding: every chunk was already stored unchanged")
	}
	if totalErrors > 0 {
		corlog.Warn(true, "  %s could not be parsed", plural(totalErrors, "file", "files"))
		if !verbose {
			corlog.Detail(true, "    run again with -verbose to list them")
		}
	}

	corlog.Info(true, "")
	corlog.Detail(true, "  coroner search \"something you wrote about\"")

	return nil
}

// quietThreshold is the share of input that has to produce nothing before the
// digest says so out loud.
//
// Deliberately high. Plenty of legitimate exports are mostly photographs, and a
// tool that warns about every one of them teaches you to ignore it. Below this
// the count is still reported, just without comment -- the number is the useful
// part, and what it means is a judgement the person who made the export can make
// and this program cannot.
const quietThreshold = 0.8

func reportDigest(rep *digest.Report) {
	corlog.Detail(true, "  %s, %s parsed from %s walked",
		rep.Type,
		plural(rep.Parsed, "file", "files"),
		plural(rep.Walked, "file", "files"))

	// Record counts, for formats where one file holds thousands of entries and
	// the file counts above say almost nothing.
	if rep.RecordsSeen > 0 {
		if rep.RecordsEmpty > 0 {
			corlog.Detail(true, "  %s read, %s held no text (%s)",
				plural(rep.RecordsSeen, "record", "records"),
				comma(rep.RecordsEmpty),
				percent(rep.RecordsEmpty, rep.RecordsSeen))
		} else {
			corlog.Detail(true, "  %s read", plural(rep.RecordsSeen, "record", "records"))
		}
	}

	line := fmt.Sprintf("  %s, %s",
		plural(rep.Documents, "document", "documents"),
		plural(rep.Chunks, "chunk", "chunks"))
	if rep.Reused > 0 {
		line += fmt.Sprintf(" (%s embedded, %s reused)", comma(rep.Embedded), comma(rep.Reused))
	}
	corlog.Detail(true, "%s", line)

	if rep.Duplicates > 0 {
		corlog.Detail(true, "  %s dropped as duplicates within this corpus", plural(rep.Duplicates, "document", "documents"))
	}

	if n := len(rep.EmptyFiles); n > 0 {
		corlog.Detail(true, "  %s parsed but held no text", plural(n, "file", "files"))
		if verbose {
			for _, f := range rep.EmptyFiles {
				corlog.Detail(true, "    %s", f)
			}
		} else {
			corlog.Detail(true, "    run again with -verbose to list them")
		}
	}

	// The observation, not a verdict. An export that is almost entirely
	// photographs and a parser that has quietly stopped working produce exactly
	// the same silence, and only one of them is a problem.
	if seen, empty := quietCounts(rep); seen > 0 && float64(empty)/float64(seen) >= quietThreshold {
		corlog.Warn(true, "  most of what was read produced no text (%s of %s)",
			comma(empty), comma(seen))
		corlog.Detail(true, "    normal for a photo-heavy export, and also what a broken parser looks like")
	}

	if n := len(rep.Errors); n > 0 {
		corlog.Warn(true, "  %s could not be parsed", plural(n, "file", "files"))
		if verbose {
			for _, e := range rep.Errors {
				corlog.Detail(true, "    %s: %v", e.File, e.Err)
			}
		} else {
			corlog.Detail(true, "    run again with -verbose to see why")
		}
	}
}

// quietCounts picks whichever granularity the parser actually reported at, so
// the observation is made against records for formats that have them and files
// for those that do not.
func quietCounts(rep *digest.Report) (seen, empty int) {
	if rep.RecordsSeen > 0 {
		return rep.RecordsSeen, rep.RecordsEmpty
	}
	return rep.Parsed + len(rep.EmptyFiles), len(rep.EmptyFiles)
}

func percent(n, of int) string {
	if of == 0 {
		return "0%"
	}
	return fmt.Sprintf("%.0f%%", float64(n)/float64(of)*100)
}

// resolveSources works out which corpora to digest, and explains itself when
// there are none. An empty source root is the normal first-run state and should
// read like an instruction, not a failure.
func resolveSources(source, root string) ([]string, error) {
	if source != "" {
		info, err := os.Stat(source)
		if err != nil {
			return nil, fmt.Errorf("cannot read source directory %s: %w", source, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s is a file, not a source directory", source)
		}
		return []string{source}, nil
	}

	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("no source root at %s.\n"+
			"  A source directory holds one export plus a corpus.yaml saying what it is.\n"+
			"    coroner initsource -source=%s/substack -type=substack\n"+
			"  Then drop the export into that directory and digest it", root, filepath.ToSlash(root))
	}

	dirs, err := corpus.Discover(root)
	if err != nil {
		return nil, err
	}
	if len(dirs) == 0 {
		return nil, fmt.Errorf("no corpora under %s.\n"+
			"  Coroner looks for directories containing a %s. There are none yet.\n"+
			"    coroner initsource -source=%s/substack -type=substack",
			filepath.ToSlash(root), corpus.FileName, filepath.ToSlash(root))
	}

	return dirs, nil
}

// ---------------------------------------------------------------- search

func cmdSearch(args []string) error {
	c := newFlagSet("search")
	hits := c.fs.Int("hits", defaultHits, "How many documents to return.")
	mode := c.fs.String("mode", "hybrid", "Which retrievers to use: hybrid, lexical or vector.")
	sourceFilter := c.fs.String("source", "", "Restrict to one corpus.")
	format := c.fs.String("format", "table", "Output format: table or json.")
	depth := c.fs.Int("depth", search.DefaultDepth, "How many chunks each retriever contributes to the fusion.")

	cfg, set, err := c.load(args)
	if err != nil {
		return err
	}
	if cfg.Defaults.Hits > 0 && !set["hits"] {
		*hits = cfg.Defaults.Hits
	}

	query := strings.TrimSpace(strings.Join(c.fs.Args(), " "))
	if query == "" {
		return fmt.Errorf("no query given, so there is nothing to search for.\n" +
			"    coroner search \"choice\"\n" +
			"  The query is a concept, not a filename: coroner looks for writing about it,\n" +
			"  including writing that never uses the word")
	}

	m, err := parseMode(*mode)
	if err != nil {
		return err
	}

	asJSON := *format == "json"
	if *format != "json" && *format != "table" {
		return fmt.Errorf("unknown format %q; use table or json", *format)
	}

	// JSON on stdout must be JSON and nothing else, or it cannot be piped
	// anywhere. Everything human-facing still reaches the log file.
	console := !asJSON

	if console {
		corlog.Heading(true, "coroner %s", version.Version)
	}

	st, err := store.Open(*c.digested)
	if err != nil {
		return err
	}

	man, err := st.LoadManifest()
	if err != nil {
		return err
	}
	if man.Empty() {
		return fmt.Errorf("nothing has been digested into %s yet.\n"+
			"    coroner initsource -source=source/substack -type=substack\n"+
			"    coroner digest", *c.digested)
	}
	if err := man.CheckChunking(); err != nil {
		return err
	}

	corpora, err := st.ReadAll()
	if err != nil {
		return err
	}
	if *sourceFilter != "" {
		corpora = filterCorpora(corpora, *sourceFilter)
		if len(corpora) == 0 {
			return fmt.Errorf("no digested corpus called %q; `coroner sources` lists what there is", *sourceFilter)
		}
	}

	engine := search.NewEngine(corpora)

	var vector []float32
	if m != search.ModeLexical {
		client := embed.New(cfg.Embed.BaseURL, cfg.Embed.Model)

		info, err := client.Probe(context.Background())
		if err != nil {
			return fmt.Errorf("%w\n  Or search without embeddings: coroner search %q -mode=lexical", err, query)
		}
		if err := man.CheckEmbed(client.Model, info.Digest, 0); err != nil {
			return err
		}

		vector, err = client.EmbedOne(context.Background(), query)
		if err != nil {
			return err
		}
	}

	results := engine.Search(search.Query{
		Text:   query,
		Vector: vector,
		Hits:   *hits,
		Mode:   m,
		Depth:  *depth,
	})

	if asJSON {
		return emitJSON(query, m, results)
	}

	corlog.Info(true, "")
	corlog.Field(true, "Query", query)
	corlog.Field(true, "Mode", m.String())
	corlog.Field(true, "Corpus", fmt.Sprintf("%s across %s",
		plural(len(engine.Docs), "document", "documents"),
		plural(len(corpora), "corpus", "corpora")))

	corlog.Info(true, "")
	if len(results) == 0 {
		corlog.Warn(true, "Nothing matched.")
		corlog.Detail(true, "  Hybrid search should find something for almost any query, so no results")
		corlog.Detail(true, "  usually means the corpus is smaller than you think.")
		corlog.Detail(true, "    coroner sources")
		return nil
	}

	printResults(results)
	return nil
}

func printResults(results []search.Result) {
	top := results[0].Score

	for i, r := range results {
		where := r.Doc.Source
		if !r.Doc.Published.IsZero() {
			where += " · " + r.Doc.Published.Format("2006-01-02")
		}
		where += " · " + retrievers(r)

		corlog.Row(true,
			corlog.Seg(corlog.StylePlain, "  %3d. ", i+1),
			corlog.Seg(corlog.StyleBar, "%s ", bar(r.Score, top, barWidth)),
			corlog.Seg(corlog.StyleDim, " %s", where),
		)

		// The title line is skipped rather than filled in when a source has no
		// titles. Facebook posts have none -- the export's "title" field is
		// chrome like "Justin Crosby updated his status." -- and falling back
		// to the filename would print the same JSON path under every result in
		// the corpus. The line above already says where a result came from, and
		// the snippet says what it is.
		if r.Doc.Title != "" {
			corlog.Row(true,
				corlog.Seg(corlog.StylePlain, "       "),
				corlog.Seg(corlog.StyleTitle, "%s", r.Doc.Title),
			)
		}

		corlog.Detail(true, "       %s", doc.Snippet(r.Chunk.Text, 150))
		corlog.Info(true, "")
	}
}

// retrievers says which half of the search found this, which is most of what
// you want to know when a result surprises you: whether it came up because of
// the words or because of the idea.
func retrievers(r search.Result) string {
	switch {
	case r.LexicalRank > 0 && r.VectorRank > 0:
		return fmt.Sprintf("both (lexical #%d, vector #%d)", r.LexicalRank, r.VectorRank)
	case r.LexicalRank > 0:
		return fmt.Sprintf("lexical #%d", r.LexicalRank)
	case r.VectorRank > 0:
		return fmt.Sprintf("vector #%d", r.VectorRank)
	default:
		return "unranked"
	}
}

// jsonResult is the machine-readable shape. Named fields rather than the
// internal structs, so that refactoring the search package does not silently
// change coroner's output contract.
type jsonResult struct {
	Rank        int     `json:"rank"`
	Score       float64 `json:"score"`
	ID          string  `json:"id"`
	Source      string  `json:"source"`
	Title       string  `json:"title,omitempty"`
	URL         string  `json:"url,omitempty"`
	File        string  `json:"file"`
	Published   string  `json:"published,omitempty"`
	Snippet     string  `json:"snippet"`
	ChunkID     string  `json:"chunk_id"`
	LexicalRank int     `json:"lexical_rank"`
	VectorRank  int     `json:"vector_rank"`
}

func emitJSON(query string, m search.Mode, results []search.Result) error {
	out := struct {
		Query   string       `json:"query"`
		Mode    string       `json:"mode"`
		Results []jsonResult `json:"results"`
	}{Query: query, Mode: m.String(), Results: make([]jsonResult, 0, len(results))}

	for i, r := range results {
		jr := jsonResult{
			Rank:        i + 1,
			Score:       r.Score,
			ID:          r.Doc.ID,
			Source:      r.Doc.Source,
			Title:       r.Doc.Title,
			URL:         r.Doc.URL,
			File:        r.Doc.File,
			Snippet:     doc.Snippet(r.Chunk.Text, 400),
			ChunkID:     r.Chunk.ID,
			LexicalRank: r.LexicalRank,
			VectorRank:  r.VectorRank,
		}
		if !r.Doc.Published.IsZero() {
			jr.Published = r.Doc.Published.Format(time.RFC3339)
		}
		out.Results = append(out.Results, jr)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

func parseMode(s string) (search.Mode, error) {
	switch strings.ToLower(s) {
	case "hybrid", "":
		return search.ModeHybrid, nil
	case "lexical", "bm25", "keyword":
		return search.ModeLexical, nil
	case "vector", "semantic", "embedding":
		return search.ModeVector, nil
	default:
		return 0, fmt.Errorf("unknown mode %q; use hybrid, lexical or vector", s)
	}
}

func filterCorpora(corpora []*store.Corpus, name string) []*store.Corpus {
	var out []*store.Corpus
	for _, c := range corpora {
		if c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

// ---------------------------------------------------------------- sources

func cmdSources(args []string) error {
	c := newFlagSet("sources")

	if _, _, err := c.load(args); err != nil {
		return err
	}

	corlog.Heading(true, "coroner %s", version.Version)

	st, err := store.Open(*c.digested)
	if err != nil {
		return err
	}

	man, err := st.LoadManifest()
	if err != nil {
		return err
	}

	corlog.Info(true, "")
	corlog.Field(true, "Digested", *c.digested)

	if man.Empty() {
		corlog.Info(true, "")
		corlog.Warn(true, "Nothing digested yet.")
		corlog.Detail(true, "  coroner initsource -source=source/substack -type=substack")
		corlog.Detail(true, "  coroner digest")
		return nil
	}

	corlog.Field(true, "Model", fmt.Sprintf("%s, %d dimensions", man.Embed.Model, man.Embed.Dimensions))
	if man.Embed.Digest != "" {
		corlog.Field(true, "Model digest", shortDigest(man.Embed.Digest))
	}
	corlog.Field(true, "Chunking", fmt.Sprintf("target %d, max %d, min %d",
		man.Chunking.TargetChars, man.Chunking.MaxChars, man.Chunking.MinChars))

	corlog.Info(true, "")

	var docs, chunks int
	for _, s := range man.Sources {
		corlog.Row(true,
			corlog.Seg(corlog.StyleTitle, "  %-20s", s.Name),
			corlog.Seg(corlog.StyleDim, "%-10s", s.Type),
			corlog.Seg(corlog.StylePlain, "%8s  ", comma(s.Documents)),
			corlog.Seg(corlog.StyleDim, "%9s chunks   ", comma(s.Chunks)),
			corlog.Seg(corlog.StyleDim, "digested %s", s.DigestedAt.Local().Format("2006-01-02 15:04")),
		)
		if verbose && s.SourceDir != "" {
			corlog.Detail(true, "  %-20s from %s", "", s.SourceDir)
		}
		docs += s.Documents
		chunks += s.Chunks
	}

	corlog.Info(true, "")
	corlog.Success(true, "%s and %s across %s",
		plural(docs, "document", "documents"),
		plural(chunks, "chunk", "chunks"),
		plural(len(man.Sources), "corpus", "corpora"))

	return nil
}

// ---------------------------------------------------------------- links

func cmdLinks(args []string) error {
	c := newFlagSet("links")
	source := c.fs.String("source", "", "REQUIRED: the source directory to read.")
	out := c.fs.String("out", "links.md", "Where to write. Use - for standard output.")
	format := c.fs.String("format", "md", "Output format: md or json.")
	topDomains := c.fs.Int("domains", 25, "How many domains to list in the summary.")

	if _, _, err := c.load(args); err != nil {
		return err
	}

	if *format != "md" && *format != "json" {
		return fmt.Errorf("unknown format %q; use md or json", *format)
	}
	if *source == "" {
		return fmt.Errorf("no -source given, so there is nothing to read.\n" +
			"    coroner links -source=source/facebook_posts")
	}

	// Reads and parses the export but embeds nothing, so this works without
	// ollama running. Links are not part of the corpus and never reach it.
	man, parser, err := loadParser(*source)
	if err != nil {
		return err
	}

	lister, ok := parser.(parse.LinkLister)
	if !ok {
		return fmt.Errorf("the %s parser does not record outbound links.\n"+
			"  Only formats that keep links as their own field can produce this;\n"+
			"  links inside a page's markup are part of the writing, not a list", man.Type)
	}

	if *format == "md" && *out != "-" {
		corlog.Heading(true, "coroner %s", version.Version)
		corlog.Info(true, "")
		corlog.Field(true, "Reading", filepath.ToSlash(*source))
	}

	if err := parseForLinks(*source, man, parser); err != nil {
		return err
	}

	dropped := 0
	if d, ok := parser.(interface{ DroppedLinks() int }); ok {
		dropped = d.DroppedLinks()
	}

	rep := links.Build(man.Name, lister.Links(), dropped)

	var rendered []byte
	if *format == "json" {
		rendered, err = json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		rendered = append(rendered, '\n')
	} else {
		rendered = []byte(rep.Markdown(*topDomains))
	}

	if *out == "-" {
		_, err := os.Stdout.Write(rendered)
		return err
	}

	if err := os.WriteFile(*out, rendered, 0644); err != nil {
		return err
	}

	corlog.Info(true, "")
	corlog.Success(true, "Wrote %s to %s", plural(len(rep.Links), "link", "links"), filepath.ToSlash(*out))
	if rep.Unique != len(rep.Links) {
		corlog.Detail(true, "  %s distinct URLs; the rest were shared more than once", comma(rep.Unique))
	}
	if rep.Dropped > 0 {
		corlog.Detail(true, "  %s recorded links pointed nowhere usable", comma(rep.Dropped))
	}
	if len(rep.Domains) > 0 {
		corlog.Detail(true, "  most shared: %s (%s)", rep.Domains[0].Domain, comma(rep.Domains[0].Count))
	}

	return nil
}

// loadParser resolves a source directory to its manifest and a fresh parser.
func loadParser(dir string) (*corpus.Manifest, parse.Parser, error) {
	man, err := corpus.Load(dir)
	if err != nil {
		return nil, nil, err
	}
	if err := man.Validate(parse.Types()); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", filepath.Join(dir, corpus.FileName), err)
	}

	parser, err := parse.New(man.Type)
	if err != nil {
		return nil, nil, err
	}
	return man, parser, nil
}

// parseForLinks runs the parser purely for its side effects, discarding the
// documents. Wasteful in principle and irrelevant in practice: parsing an
// 8,000-record export takes under a second, and the alternative is a second code
// path through the same JSON that could drift out of step with the first.
func parseForLinks(dir string, man *corpus.Manifest, parser parse.Parser) error {
	src := parse.Source{Dir: dir, Name: man.Name, Type: man.Type, Author: man.Author}

	if err := parser.Prepare(src); err != nil {
		return err
	}
	return digest.ParseOnly(context.Background(), dir, man, parser, src)
}

// ---------------------------------------------------------------- stats

func cmdStats(args []string) error {
	c := newFlagSet("stats")
	sourceFilter := c.fs.String("source", "", "Restrict to one corpus.")
	format := c.fs.String("format", "table", "Output format: table or json.")
	sampleN := c.fs.Int("sample", 0, "Also print this many documents, spread evenly through the corpus.")

	if _, _, err := c.load(args); err != nil {
		return err
	}

	asJSON := *format == "json"
	if *format != "json" && *format != "table" {
		return fmt.Errorf("unknown format %q; use table or json", *format)
	}

	st, err := store.Open(*c.digested)
	if err != nil {
		return err
	}

	corpora, err := st.ReadAll()
	if err != nil {
		return err
	}
	if *sourceFilter != "" {
		corpora = filterCorpora(corpora, *sourceFilter)
		if len(corpora) == 0 {
			return fmt.Errorf("no digested corpus called %q; `coroner sources` lists what there is", *sourceFilter)
		}
	}
	if len(corpora) == 0 {
		return fmt.Errorf("nothing has been digested into %s yet.\n    coroner digest", *c.digested)
	}

	rep := stats.Compute(corpora)

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(rep)
	}

	corlog.Heading(true, "coroner %s", version.Version)

	for _, s := range rep.Sources {
		printSourceStats(s)
	}
	if len(rep.Sources) > 1 {
		printSourceStats(rep.Total)
	}

	if *sampleN > 0 {
		for _, c := range corpora {
			corlog.Info(true, "")
			corlog.Heading(true, "  %s: %s spread through the corpus", c.Name, plural(*sampleN, "document", "documents"))
			for _, d := range stats.Sample(c, *sampleN) {
				when := "undated"
				if !d.Published.IsZero() {
					when = d.Published.Format("2006-01-02")
				}
				corlog.Row(true,
					corlog.Seg(corlog.StyleDim, "    %s  ", when),
					corlog.Seg(corlog.StylePlain, "%s", doc.Snippet(d.Text, 100)),
				)
			}
		}
	}

	return nil
}

func printSourceStats(s stats.Source) {
	corlog.Info(true, "")
	corlog.Heading(true, "  %s", s.Name)

	corlog.Field(true, "Documents", fmt.Sprintf("%s in %s, %s words",
		comma(s.Documents), plural(s.Chunks, "chunk", "chunks"), comma(s.Words)))

	if !s.Earliest.IsZero() {
		corlog.Field(true, "Span", fmt.Sprintf("%s to %s",
			s.Earliest.Format("2006-01-02"), s.Latest.Format("2006-01-02")))
	}
	if s.Undated > 0 {
		corlog.Field(true, "Undated", fmt.Sprintf("%s (%s)", comma(s.Undated), percent(s.Undated, s.Documents)))
	}
	if s.Empty > 0 {
		corlog.Warn(true, "  %-12s %s documents hold no text at all", "Empty", comma(s.Empty))
	}

	corlog.Field(true, "Words/doc", spreadLine(s.WordsPerDocument))
	corlog.Field(true, "Chunks/doc", spreadLine(s.ChunksPerDocument))

	// The hapax share is the quickest signal that a corpus is real writing
	// rather than repeated boilerplate.
	corlog.Field(true, "Vocabulary", fmt.Sprintf("%s distinct terms, %s used once (%s)",
		comma(s.Vocabulary), comma(s.Hapax), percent(s.Hapax, s.Vocabulary)))

	if len(s.ByYear) > 0 {
		corlog.Info(true, "")
		printYears(s.ByYear)
	}
}

func spreadLine(sp stats.Spread) string {
	return fmt.Sprintf("min %s  median %s  p90 %s  p99 %s  max %s",
		comma(sp.Min), comma(sp.P50), comma(sp.P90), comma(sp.P99), comma(sp.Max))
}

// printYears draws the histogram. A gap in it is either a year you did not write
// or a year the parser dropped, and that is exactly the kind of thing that is
// invisible in a total and obvious in a row of bars.
func printYears(years []stats.YearCount) {
	most := 0
	for _, y := range years {
		if y.Count > most {
			most = y.Count
		}
	}

	for _, y := range years {
		corlog.Row(true,
			corlog.Seg(corlog.StyleDim, "    %d  ", y.Year),
			corlog.Seg(corlog.StyleBar, "%s", bar(float64(y.Count), float64(most), 28)),
			corlog.Seg(corlog.StyleDim, " %s", comma(y.Count)),
		)
	}
}

// ---------------------------------------------------------------- dupes

func cmdDupes(args []string) error {
	c := newFlagSet("dupes")
	format := c.fs.String("format", "table", "Output format: table or json.")
	window := c.fs.Int("window", dupes.DefaultWindow, "Days apart two documents may be dated and still be compared.")
	threshold := c.fs.Float64("threshold", dupes.DefaultThreshold, "Minimum shingle overlap, 0 to 1, for two documents to be called the same writing.")

	if _, _, err := c.load(args); err != nil {
		return err
	}

	asJSON := *format == "json"
	if *format != "json" && *format != "table" {
		return fmt.Errorf("unknown format %q; use table or json", *format)
	}
	if *threshold <= 0 || *threshold > 1 {
		return fmt.Errorf("-threshold must be above 0 and at most 1, got %v", *threshold)
	}
	if *window < 0 {
		return fmt.Errorf("-window cannot be negative, got %d", *window)
	}

	st, err := store.Open(*c.digested)
	if err != nil {
		return err
	}

	corpora, err := st.ReadAll()
	if err != nil {
		return err
	}
	if len(corpora) == 0 {
		return fmt.Errorf("nothing has been digested into %s yet.\n    coroner digest", *c.digested)
	}
	if len(corpora) < 2 {
		return fmt.Errorf("only one corpus is digested, so there is nothing to compare it against.\n" +
			"    dupes looks for the same writing in different corpora; duplicates within one\n" +
			"    corpus are dropped at digest time. `coroner sources` lists what there is.")
	}

	man, err := st.LoadManifest()
	if err != nil {
		return err
	}

	opts := dupes.Defaults()
	opts.Window = *window
	opts.Threshold = *threshold
	opts.Priority = map[string]int{}
	for _, si := range man.Sources {
		opts.Priority[si.Name] = si.Priority
	}

	rep := dupes.Find(corpora, opts)

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return enc.Encode(rep)
	}

	printDupes(rep, opts)
	return nil
}

func printDupes(rep dupes.Report, opts dupes.Options) {
	corlog.Heading(true, "coroner %s", version.Version)
	corlog.Info(true, "")

	// Say what the pass did before what it found. A run that compared far fewer
	// documents than the corpus holds has hit undated documents, and that is
	// worth noticing before reading the groups.
	corlog.Field(true, "Compared", fmt.Sprintf("%s dated documents, +/-%s, overlap >= %.2f",
		comma(rep.Compared), plural(rep.Window, "day", "days"), rep.Threshold))

	if len(rep.Groups) == 0 {
		corlog.Info(true, "")
		corlog.Detail(true, "No document appears in more than one corpus.")
		return
	}

	corlog.Field(true, "Found", fmt.Sprintf("%s in %s",
		plural(rep.Duplicated, "document", "documents"),
		plural(len(rep.Groups), "group", "groups")))

	unranked := true
	for _, p := range opts.Priority {
		if p != 0 {
			unranked = false
		}
	}
	if unranked {
		corlog.Warn(true, "  %-12s no corpus sets `priority` in its corpus.yaml, so the winner in each",
			"Unranked")
		corlog.Detail(true, "               group below is only the first by corpus name. Set priority and re-digest.")
	}

	for _, g := range rep.Groups {
		corlog.Info(true, "")

		when := "undated"
		if !g.Members[0].Date.IsZero() {
			when = g.Members[0].Date.Format("2006-01-02")
		}
		corlog.Heading(true, "  %s  (weakest match %.2f)", when, g.Lowest)

		for _, m := range g.Members {
			mark := "  "
			style := corlog.StyleDim
			if m.DocID == g.Winner {
				// The winner is marked rather than reordered away: seeing what
				// it beat, and by how much, is most of what the report is for.
				mark = "->"
				style = corlog.StylePlain
			}
			corlog.Row(true,
				corlog.Seg(corlog.StyleDim, "    %s ", mark),
				corlog.Seg(style, "%-16s ", m.DocID),
				corlog.Seg(corlog.StyleDim, "%-16s ", m.Source),
				corlog.Seg(style, "%.2f  ", m.Score),
				corlog.Seg(style, "%s", firstNonEmpty(m.Title, m.Snippet)),
			)
		}
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// ---------------------------------------------------------------- init

func cmdInitSource(args []string) error {
	c := newFlagSet("initsource")
	source := c.fs.String("source", "", "REQUIRED: the source directory to create.")
	sourceType := c.fs.String("type", "", "REQUIRED: the export format. One of: "+strings.Join(parse.Types(), ", "))
	name := c.fs.String("name", "", "The corpus name. Defaults to the directory name.")

	if _, _, err := c.load(args); err != nil {
		return err
	}

	corlog.Heading(true, "coroner %s", version.Version)

	if *source == "" {
		return fmt.Errorf("no -source given, so there is nowhere to create.\n" +
			"    coroner initsource -source=source/substack -type=substack")
	}
	if *sourceType == "" {
		return fmt.Errorf("no -type given, so the manifest cannot say what the export is.\n"+
			"  Known types: %s\n"+
			"    coroner initsource -source=%s -type=html",
			strings.Join(parse.Types(), ", "), filepath.ToSlash(*source))
	}
	if _, err := parse.New(*sourceType); err != nil {
		return err
	}

	corpusName := *name
	if corpusName == "" {
		corpusName = corpus.SuggestName(*source)
	}

	if err := corpus.WriteStarter(*source, corpusName, *sourceType); err != nil {
		return err
	}

	corlog.Info(true, "")
	corlog.Success(true, "Created %s", filepath.ToSlash(filepath.Join(*source, corpus.FileName)))
	corlog.Info(true, "")
	corlog.Field(true, "Corpus", corpusName)
	corlog.Field(true, "Type", *sourceType)

	corlog.Info(true, "")
	if !parse.Implemented(*sourceType) {
		corlog.Warn(true, "The %s parser is not written yet.", *sourceType)
		corlog.Detail(true, "  The manifest is valid and the directory is ready; digesting it will say the")
		corlog.Detail(true, "  same thing until the parser exists.")
		corlog.Info(true, "")
	}

	corlog.Detail(true, "  Put the export into %s, then:", filepath.ToSlash(*source))
	corlog.Detail(true, "    coroner digest -source=%s", filepath.ToSlash(*source))

	return nil
}

func cmdInitConfig(args []string) error {
	c := newFlagSet("initconfig")

	// Parsed directly rather than through load(), because load() reads the
	// config we are about to write and an absent one is not an error here.
	if err := c.fs.Parse(args); err != nil {
		return err
	}
	if *c.noColor {
		corlog.DisableColor()
	}

	corlog.Heading(true, "coroner %s", version.Version)

	path := *c.config
	if path == "" {
		var err error
		if path, err = config.DefaultPath(); err != nil {
			return err
		}
	}

	if err := config.WriteStarter(path); err != nil {
		return err
	}

	corlog.Info(true, "")
	corlog.Success(true, "Wrote a starter config to %s", filepath.ToSlash(path))
	corlog.Detail(true, "  Everything in it is commented out, so it changes nothing until you edit it.")

	return nil
}

// ---------------------------------------------------------------- output helpers

const barWidth = 14

// barCell is U+2588 FULL BLOCK, and deliberately not the partial blocks
// U+2589-U+258F: the Windows console font ships this one without them, so they
// render as missing-glyph boxes. Carried over from hecato, where the same
// lesson was learned the hard way.
const barCell = "█"

// bar renders a score as a proportion of the best one in the result set.
//
// The bar is relative because a fused rank score has no absolute meaning: it is
// a sum of reciprocal ranks, so 0.03 is not "3% relevant", it is "roughly as
// good as the third result". Showing the number would invite thresholding on
// it, which is why the number is in the JSON output and the bar is in the
// human one.
func bar(score, top float64, width int) string {
	if top <= 0 || score <= 0 {
		return strings.Repeat(" ", width)
	}

	cells := int(score/top*float64(width) + 0.5)
	if cells > width {
		cells = width
	}
	if cells < 1 {
		cells = 1
	}

	return strings.Repeat(barCell, cells) + strings.Repeat(" ", width-cells)
}

func relAll(dirs []string) []string {
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = filepath.ToSlash(d)
	}
	sort.Strings(out)
	return out
}

func configLabel(cfg *config.Config) string {
	if cfg.Loaded() {
		return cfg.Path
	}
	return "none found, using built-in defaults"
}

func shortDigest(d string) string {
	if len(d) > 12 {
		return d[:12]
	}
	if d == "" {
		return "unknown"
	}
	return d
}

// plural pairs a grouped count with the right form of its noun, so a summary
// reads "1 document" rather than "1 documents".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return comma(n) + " " + many
}

// comma groups digits so five-figure document counts stay readable.
func comma(n int) string {
	if n < 0 {
		return "-" + comma(-n)
	}

	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}

	var out []byte
	for i, ch := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, ch)
	}
	return string(out)
}
