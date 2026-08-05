/*
main executable for coroner

	coroner initconfig
	coroner initsource -source=source/substack -type=substack
	coroner digest
	coroner digest -source=source/substack -verbose
	coroner search "choice"
	coroner search "branching logic" -hits=25 -format=json
	coroner sources

// For creating an executable:::

	go build -o coroner.exe cmd/coroner/main.go
	make all

//Stuff todo:::
TODO: parser: facebook, written against a real export rather than the documented shape
TODO: parser: substack, same
TODO: method: dedupe, the after-the-fact pass that relates near-identical documents across corpora
TODO: search: filter by date range, which is most of what "what was I writing about in 2019" needs
TODO: search: -explain, showing which query terms drove a lexical hit
TODO: digest: report which files parsed to nothing, since an export full of image-only posts looks identical to a broken parser
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
	"github.com/curiousjc/coroner/internal/embed"
	"github.com/curiousjc/coroner/internal/examples"
	"github.com/curiousjc/coroner/internal/parse"
	"github.com/curiousjc/coroner/internal/search"
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
		corlog.Detail(true, "Commands: digest, search, sources, initsource, initconfig, version, examples")
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

func reportDigest(rep *digest.Report) {
	corlog.Detail(true, "  %s, %s parsed from %s walked",
		rep.Type,
		plural(rep.Parsed, "file", "files"),
		plural(rep.Walked, "file", "files"))

	corlog.Detail(true, "  %s", digest.Describe(rep))

	if rep.Duplicates > 0 {
		corlog.Detail(true, "  %s dropped as duplicates within this corpus", plural(rep.Duplicates, "document", "documents"))
	}

	if n := len(rep.Errors); n > 0 {
		corlog.Warn(true, "  %s could not be parsed", plural(n, "file", "files"))
		if verbose {
			for _, e := range rep.Errors {
				corlog.Detail(true, "    %s: %v", e.File, e.Err)
			}
		}
	}
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

		title := r.Doc.Title
		if title == "" {
			title = r.Doc.File
		}

		corlog.Row(true,
			corlog.Seg(corlog.StylePlain, "  %3d. ", i+1),
			corlog.Seg(corlog.StyleBar, "%s ", bar(r.Score, top, barWidth)),
			corlog.Seg(corlog.StyleDim, " %s", where),
		)
		corlog.Row(true,
			corlog.Seg(corlog.StylePlain, "       "),
			corlog.Seg(corlog.StyleTitle, "%s", title),
		)
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
