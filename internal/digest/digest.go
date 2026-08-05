// Package digest turns a source directory into a digested corpus.
//
// The pipeline: enumerate, parse, chunk, embed, write. Only the embed step is
// slow, which is why the whole design bends toward doing less of it -- one
// corpus at a time, and within a corpus only the chunks whose text actually
// changed.
//
// Concurrency here is deliberately different from hecato's. Hecato walks an
// unknown tree with a worker pool that is also its own producer, because there
// is no file list until the walk has finished producing it. Coroner has two
// distinct phases: enumeration, which is cheap and must be ordered, and parsing,
// which is expensive and can be done in any order. So the walk is a plain
// ordered WalkDir, and the pool runs over the resulting slice with each worker
// writing into its own index. The output is a function of the file list, not of
// which worker finished first.
package digest

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/curiousjc/coroner/internal/corpus"
	"github.com/curiousjc/coroner/internal/doc"
	"github.com/curiousjc/coroner/internal/embed"
	"github.com/curiousjc/coroner/internal/ignore"
	"github.com/curiousjc/coroner/internal/parse"
	"github.com/curiousjc/coroner/internal/store"
	"github.com/curiousjc/coroner/internal/version"
)

// DefaultWorkers is how many files are parsed concurrently.
//
// Eight, matching hecato, though for a different reason: hecato's walk waits on
// the filesystem, while parsing HTML is CPU work. The number is capped against
// NumCPU below, which is what actually governs on a small machine.
const DefaultWorkers = 8

// MaxFileSize is the ceiling on a single source file.
//
// An export occasionally contains one enormous file -- a concatenated archive, a
// database dump someone renamed -- and reading it costs more than it could
// possibly be worth. Sixty-four megabytes is far above any real piece of writing.
const MaxFileSize = 64 << 20

// Embedder is the slice of the ollama client this package actually needs.
//
// An interface rather than the concrete type so that the reproducibility claim
// can be tested. Proving that two digests of the same source produce identical
// bytes requires an embedder that is itself deterministic, and a real model on a
// real machine is the one part of the pipeline that cannot promise that.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// Options configure one digest run.
type Options struct {
	// SourceDir is the directory holding the export and its corpus.yaml.
	SourceDir string

	Store    *store.Store
	Embedder Embedder

	// ModelName is what the config asked for, recorded in the manifest so the
	// next run can be checked against it.
	ModelName string

	// Model is what probing ollama reported, done once by the caller rather
	// than per corpus. Its digest is what catches a model being re-pulled and
	// quietly changing underneath a corpus.
	Model embed.ModelInfo

	Workers int
	Verbose bool

	// Force re-embeds every chunk even when an identical one is already stored.
	// The escape hatch for when you suspect the stored vectors rather than the
	// text.
	Force bool
}

// FileError is one file that could not be read or parsed.
//
// Collected rather than fatal, carrying over hecato's rule that walk errors are
// values: one malformed file in an export of forty thousand must not abort the
// digest. Unlike hecato, the count is always reported, because a file that
// failed to parse is writing that will not be searchable and you need to know.
type FileError struct {
	File string
	Err  error
}

// Report is what a digest did.
type Report struct {
	Corpus string
	Type   string

	Walked    int
	Parsed    int
	Documents int
	Chunks    int

	// Embedded and Reused split the chunk count by whether the vector had to be
	// computed. On a re-digest of an unchanged corpus, Embedded is zero and the
	// run takes seconds instead of minutes.
	Embedded int
	Reused   int

	// Duplicates counts documents dropped because another document in the same
	// corpus already had that ID. Within one corpus an ID collision means the
	// export genuinely lists the same item twice.
	Duplicates int

	Errors  []FileError
	Elapsed time.Duration

	// ModelInfo is what the embedder reported, recorded in the manifest.
	Model embed.ModelInfo
	Dims  int
}

// Run digests one source directory.
func Run(ctx context.Context, opts Options) (*Report, error) {
	started := time.Now()

	man, err := corpus.Load(opts.SourceDir)
	if err != nil {
		return nil, err
	}
	if err := man.Validate(parse.Types()); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(opts.SourceDir, corpus.FileName), err)
	}

	parser, err := parse.New(man.Type)
	if err != nil {
		return nil, err
	}

	src := parse.Source{
		Dir:    opts.SourceDir,
		Name:   man.Name,
		Type:   man.Type,
		Author: man.Author,
	}

	// Prepare before anything expensive. A parser that cannot read its sidecar
	// metadata, or one that is not written yet, should say so before the export
	// is walked rather than after.
	if err := parser.Prepare(src); err != nil {
		return nil, err
	}

	rep := &Report{Corpus: man.Name, Type: man.Type, Model: opts.Model}

	files, err := enumerate(opts.SourceDir, man, parser)
	if err != nil {
		return nil, err
	}
	rep.Walked = len(files)

	docs := parseAll(ctx, src, parser, opts, files, rep)

	// Sorted by ID before anything downstream sees them, so that chunk order,
	// embedding batches and the written files are all functions of the corpus
	// rather than of the order files finished parsing.
	sort.Slice(docs, func(i, j int) bool { return docs[i].ID < docs[j].ID })
	docs = dedupeIDs(docs, rep)
	rep.Documents = len(docs)

	var chunks []doc.Chunk
	for _, d := range docs {
		chunks = append(chunks, doc.Split(d)...)
	}
	rep.Chunks = len(chunks)

	vectors, err := embedChunks(ctx, opts, man.Name, docs, chunks, rep)
	if err != nil {
		return nil, err
	}

	if err := opts.Store.Write(&store.Corpus{
		Name:    man.Name,
		Docs:    docs,
		Chunks:  chunks,
		Vectors: vectors,
	}); err != nil {
		return nil, err
	}

	if err := updateManifest(opts, man, rep); err != nil {
		return nil, err
	}

	rep.Elapsed = time.Since(started)
	return rep, nil
}

// enumerate lists the files the parser should be offered, in sorted order.
func enumerate(root string, man *corpus.Manifest, parser parse.Parser) ([]string, error) {
	exclude := man.Matcher()

	include := man.Include
	if len(include) == 0 {
		include = parser.Include()
	}
	// Include patterns reuse the exclude matcher's syntax rather than inventing
	// a second glob dialect: "*.html" matches a base name at any depth, and a
	// pattern with a slash matches the whole relative path.
	including := ignore.New(include)

	var out []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// One unreadable directory is not a reason to abandon the export.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}

		if d.IsDir() {
			if exclude.MatchDir(rel) {
				return fs.SkipDir
			}
			return nil
		}

		if rel == corpus.FileName {
			return nil
		}
		if exclude.MatchFile(rel) {
			return nil
		}
		if !including.Empty() && !including.MatchFile(rel) {
			return nil
		}

		out = append(out, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}

	// WalkDir already visits in lexical order, but sorting makes the guarantee
	// explicit rather than inherited from an implementation detail.
	sort.Strings(out)
	return out, nil
}

// parseAll runs the parser over every file, concurrently, into per-file slots.
func parseAll(ctx context.Context, src parse.Source, parser parse.Parser, opts Options, files []string, rep *Report) []doc.Document {
	workers := opts.Workers
	if workers < 1 {
		workers = DefaultWorkers
	}
	if workers > runtime.NumCPU()*2 {
		workers = runtime.NumCPU() * 2
	}
	if workers > len(files) {
		workers = len(files)
	}
	if workers < 1 {
		return nil
	}

	// One slot per file, written only by the worker that owns that index. No
	// locking on the hot path, and the merge below is a plain ordered append.
	results := make([][]doc.Document, len(files))
	errs := make([]*FileError, len(files))

	var next int64
	var mu sync.Mutex

	take := func() (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		if int(next) >= len(files) {
			return 0, false
		}
		i := int(next)
		next++
		return i, true
	}

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if ctx.Err() != nil {
					return
				}
				i, ok := take()
				if !ok {
					return
				}

				rel := files[i]
				docs, err := parseOne(src, parser, opts.SourceDir, rel)
				if err != nil {
					errs[i] = &FileError{File: rel, Err: err}
					continue
				}
				results[i] = docs
			}
		}()
	}
	wg.Wait()

	var out []doc.Document
	for i := range files {
		if errs[i] != nil {
			rep.Errors = append(rep.Errors, *errs[i])
			continue
		}
		if len(results[i]) > 0 {
			rep.Parsed++
			out = append(out, results[i]...)
		}
	}

	return out
}

func parseOne(src parse.Source, parser parse.Parser, root, rel string) ([]doc.Document, error) {
	path := filepath.Join(root, filepath.FromSlash(rel))

	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxFileSize {
		return nil, fmt.Errorf("file is %d bytes, over the %d byte ceiling", info.Size(), MaxFileSize)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return parser.ParseFile(src, rel, data)
}

// dedupeIDs drops documents whose ID a previous document already claimed.
//
// Within one corpus this means the export listed the same thing twice, which
// happens. Duplicates *across* corpora are a different matter entirely and are
// left alone here: those are the near-identical copies of one essay in three
// exports, and relating them is a separate pass that needs to measure how
// similar they are rather than assume.
func dedupeIDs(docs []doc.Document, rep *Report) []doc.Document {
	seen := make(map[string]struct{}, len(docs))
	out := docs[:0]

	for _, d := range docs {
		if _, ok := seen[d.ID]; ok {
			rep.Duplicates++
			continue
		}
		seen[d.ID] = struct{}{}
		out = append(out, d)
	}

	return out
}

// embedChunks computes vectors, reusing stored ones wherever the embedded text
// is unchanged.
func embedChunks(ctx context.Context, opts Options, name string, docs []doc.Document, chunks []doc.Chunk, rep *Report) ([][]float32, error) {
	if len(chunks) == 0 {
		return nil, nil
	}

	docAt := make(map[string]int, len(docs))
	for i, d := range docs {
		docAt[d.ID] = i
	}

	texts := make([]string, len(chunks))
	keys := make([]string, len(chunks))
	for i, ch := range chunks {
		var d doc.Document
		if at, ok := docAt[ch.DocID]; ok {
			d = docs[at]
		}
		texts[i] = doc.Indexed(d, ch)

		// Keyed on the embedded text rather than the chunk text, so that an
		// edited title correctly invalidates the vectors of every chunk under
		// it.
		keys[i] = doc.Hash(texts[i])
	}

	reusable := map[string][]float32{}
	if !opts.Force {
		var err error
		reusable, err = loadReusable(opts.Store, name, docs)
		if err != nil {
			return nil, err
		}
	}

	vectors := make([][]float32, len(chunks))

	var todo []int
	for i := range chunks {
		if v, ok := reusable[keys[i]]; ok {
			vectors[i] = v
			rep.Reused++
			continue
		}
		todo = append(todo, i)
	}

	if len(todo) == 0 {
		if len(vectors) > 0 {
			rep.Dims = len(vectors[0])
		}
		return vectors, nil
	}

	batch := make([]string, len(todo))
	for i, at := range todo {
		batch[i] = texts[at]
	}

	fresh, err := opts.Embedder.Embed(ctx, batch)
	if err != nil {
		return nil, err
	}
	if len(fresh) != len(todo) {
		return nil, fmt.Errorf("asked for %d embeddings and got %d", len(todo), len(fresh))
	}

	for i, at := range todo {
		v := fresh[i]
		store.Normalise(v)
		vectors[at] = v
	}
	rep.Embedded = len(todo)

	// Every vector must be the same width, or the vector file cannot be written
	// and the corpus would be half-usable. Checking here names the problem;
	// checking in the writer would only say the file was malformed.
	dims := len(vectors[0])
	for i, v := range vectors {
		if len(v) != dims {
			return nil, fmt.Errorf("chunk %s embedded to %d dimensions, expected %d", chunks[i].ID, len(v), dims)
		}
	}
	rep.Dims = dims

	return vectors, nil
}

// loadReusable maps embedded-text hash to the vector already stored for it.
func loadReusable(s *store.Store, name string, docs []doc.Document) (map[string][]float32, error) {
	prev, err := s.Read(name)
	if err != nil {
		// A corpus that cannot be read is a corpus that will be replaced. There
		// is nothing to reuse, and no reason to refuse to rebuild it.
		return map[string][]float32{}, nil
	}
	if len(prev.Vectors) == 0 {
		return map[string][]float32{}, nil
	}

	prevDocAt := make(map[string]int, len(prev.Docs))
	for i, d := range prev.Docs {
		prevDocAt[d.ID] = i
	}

	out := make(map[string][]float32, len(prev.Chunks))
	for i, ch := range prev.Chunks {
		var d doc.Document
		if at, ok := prevDocAt[ch.DocID]; ok {
			d = prev.Docs[at]
		}
		out[doc.Hash(doc.Indexed(d, ch))] = prev.Vectors[i]
	}

	return out, nil
}

// updateManifest records this corpus in the digested directory's manifest.
func updateManifest(opts Options, man *corpus.Manifest, rep *Report) error {
	s := opts.Store

	m, err := s.LoadManifest()
	if err != nil {
		return err
	}

	m.CoronerVersion = version.Version
	m.Chunking = store.CurrentChunking()

	// The model recorded is what was asked for, not what ollama normalised it
	// to, because that is what the next run's config will be compared against.
	// The digest is ollama's, and is the half that catches a re-pull.
	if opts.ModelName != "" {
		m.Embed.Model = opts.ModelName
	}
	if rep.Dims > 0 {
		m.Embed.Dimensions = rep.Dims
	}
	if rep.Model.Digest != "" {
		m.Embed.Digest = rep.Model.Digest
	}

	abs, absErr := filepath.Abs(opts.SourceDir)
	if absErr != nil {
		abs = opts.SourceDir
	}

	m.SetSource(store.SourceInfo{
		Name:           man.Name,
		Type:           man.Type,
		Documents:      rep.Documents,
		Chunks:         rep.Chunks,
		SourceDir:      filepath.ToSlash(abs),
		DigestedAt:     time.Now().UTC().Truncate(time.Second),
		CoronerVersion: version.Version,
	})

	return s.SaveManifest(m)
}

// Describe renders a one-line summary of what a report contains.
func Describe(rep *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d documents, %d chunks", rep.Documents, rep.Chunks)
	if rep.Reused > 0 {
		fmt.Fprintf(&b, " (%d embedded, %d reused)", rep.Embedded, rep.Reused)
	}
	return b.String()
}
