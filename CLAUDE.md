# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Keep the repo present-day

Docs and comments describe the system as it is now. No history, no "measured on" or "decided on" narratives, no tombstones for things that were removed or changed, no records of past incidents. Git history and PRs already hold all of that. When a rule needs a reason, give the reason briefly and in the present tense.

`TODO.md` lists only work that has not been done. Remove an item when it is done; do not move it to a "done" list.

## What this is

`coroner` is a single-binary CLI that digests bodies of personal writing into a searchable corpus, then searches it by concept rather than by keyword. `digest` reads exports into a shared digested directory; `search`, `dupes`, `stats`, `export` and `serve` read from it.

Go dependencies: `gopkg.in/yaml.v3` (config and manifests), `github.com/fatih/color` (console output), `golang.org/x/net/html` (HTML extraction), `golang.org/x/text` (Unicode normalisation). The front end in `web/` is React and Vite, built with node.

It is the sibling of `hecato` and follows its patterns.

## Commands

```bash
go build -o coroner.exe cmd/coroner/main.go   # quick local build: no version injection, no front end
make all                                       # front end + cross-compile linux and windows, dev logging
make release                                   # same, BUILD_CONTEXT=release (what CI runs)
make web                                       # just the front end, into internal/webui/dist
make test
make vet

coroner initsource -source=source/goodreads -type=goodreads
coroner digest
coroner search "choice"
coroner search "branching logic" -hits=25 -format=json
coroner sources
coroner stats -sample=10
coroner links -source=source/facebook_posts
coroner dupes
coroner export                  # digested/writing.html
coroner export -format=json     # digested/writing.json
coroner serve                   # http://127.0.0.1:8484
```

A single test: `go test ./internal/doc -run TestSplitIsDeterministic -v`. Front-end dev: `coroner serve` in one terminal, `npm run dev` in `web/` in another (Vite proxies `/api` to serve).

Every Go package has tests except `internal/examples`, `internal/version`, `internal/embed` and `internal/webui`. `embed` is an HTTP conversation with ollama, tested through the `digest.Embedder` interface instead.

## Determinism

Given the same source bytes, coroner version and embedding model, digesting twice produces byte-identical `.docs.jsonl`, `.chunks.jsonl` and `.vec` files (`TestDigestIsReproducible`). `export` output is likewise a function of the digested directory alone. The one wall-clock fact is `digested_at` in `digested/manifest.yaml`.

Embeddings are not guaranteed identical across an ollama upgrade or model re-pull, so the manifest records the model, its digest and the vector width, and `Manifest.CheckEmbed` is a hard error: mixed vector spaces return a confident ranking that is meaningless.

Four things break determinism if you are not deliberate:

- **Map iteration.** Anything accumulated in a map and then serialised, ranked or summed is sorted first (`ruleSet.all()`, `fuse()`, `buildBM25`, `SaveManifest`, `timeline.Build`).
- **Concurrency.** `parseAll` writes into per-file slots and merges in file order; `store.Write` sorts documents and chunks itself.
- **Float summation order.** BM25 iterates sorted unique query terms over index-ordered postings; `embed.BatchSize` is a constant so batches depend on the corpus, not on leftover work.
- **Unicode.** `doc.Normalise` composes to NFC before anything is hashed.

## Privacy

The methods are public; the writing is not.

- `source/` and `digested/` are gitignored and stay that way. Nothing resolves against a path baked in for one machine.
- `coroner export` writes into the digested directory by default, because the file is the whole corpus.
- The exported HTML page loads nothing: no script, font or remote asset.
- `coroner serve` listens on loopback only (`serve.CheckLoopback`, no override flag). It answers only requests whose `Host` is a loopback name on its own port, which stops DNS rebinding. It sends no CORS headers, and its CSP keeps the front end to its own origin.
- The front-end bundle is code only; the writing is fetched from `serve` at runtime, so building, embedding or releasing the binary never carries corpus text.
- Substack's `Include()` is a narrow allowlist (`posts/*.html`) because the export keeps per-subscriber analytics with email addresses beside the posts.

## Architecture

**Digest:** `cmd/coroner` → `internal/digest` → `internal/corpus` (manifest) → `internal/parse` (per-format parser) → `internal/doc` (normalise, hash, chunk) → `internal/embed` (ollama) → `internal/store` (JSONL + vectors).

**Search:** `cmd/coroner` → `internal/store` (load all) → `internal/search` (BM25 + vectors + RRF).

**Read-only passes over `digested/`**, none needing ollama:

- `internal/dupes`: the same writing across corpora, and which copy wins.
- `internal/timeline`: every piece of writing in one date-ordered list, duplicates folded into their winning copy. Rendered as JSON or a self-contained HTML page by `coroner export`, and served by `internal/serve` for `coroner serve`.
- `internal/stats`: counts, date histogram, length spreads, vocabulary.

`internal/ignore` decides what the digest walk never sees: the built-in `Noise` list (version control, media, web scaffolding) plus a manifest's `exclude` globs.

`internal/links` renders outbound links an export recorded, via the optional `parse.LinkLister` (Facebook only). It runs through `digest.ParseOnly`, sharing the walk with `Run`, so it needs no ollama.

### CLI

Each subcommand builds its own `flag.FlagSet`; `commonFlags` holds what they share. `commonFlags.reorder` moves flags ahead of positionals, because Go's `flag` stops at the first positional and `coroner search "choice" -hits=25` would otherwise search for `choice -hits=25`. It asks the FlagSet whether a flag takes a value and re-emits `--`.

Flag precedence is explicit flag > config > built-in default, via `fs.Visit`, which walks only flags actually typed.

### Sources and manifests

A source directory holds one export plus a `corpus.yaml` naming its type. Coroner never sniffs formats, because a guess would depend on walk order.

`Manifest.Name` is part of every document ID and names the corpus's files in `digested/`, so `validName` restricts it to `[a-z0-9_-]{1,64}`. Every corpus takes its directory name.

`priority` in `corpus.yaml` ranks corpora for `dupes` and `timeline`, higher winning, and is copied into the digested manifest at digest time. Because `source/` is gitignored, priorities are local: `substack_posts: 30`, `goodreads: 25`, `html_posts: 20`, `facebook_posts: 10`.

`corpus.example.yaml` and `coroner.example.yaml` are committed copies of what `initsource` and `initconfig` write, kept in sync by `TestExampleManifestMatchesGenerator` and `TestExampleConfigMatchesGenerator`. Change a starter, regenerate the example.

### Parsers

`parse.Parser` is two phases: `Prepare` runs once and reads sidecar metadata; `ParseFile` is called concurrently and must be read-only. Adding a format means a file in `internal/parse`, an entry in `constructors`, and a line in the manifest starter's type list.

Formats: `text`, `html`, `htmlsite`, `facebook`, `substack`, `goodreads`. A parser is only ever written against a real export on disk, never a documented shape: encoding mistakes corrupt document IDs silently. A format named before its export exists gets `pendingParser`, which fails with "not written yet".

`parse.RecordCounter` is optional, for files holding many records, so a digest can say how much produced no text. Only `facebook` implements it.

`doc.Indexed` prepends the title to every chunk for both retrievers, so **a title must be distinct per document**. A title repeated across a corpus (boilerplate) flattens every vector. Several format rules below exist because of this.

**facebook** (`your_posts*.json`)
- The export's `title` field is boilerplate ("… updated his status."), so documents have no title.
- No post identifier exists; identity is `timestamp` plus a hash of the text.
- Photo captions are appended, deduplicated against the post text because Facebook often copies the caption into both.
- The export is double-encoded (UTF-8 read as latin-1). `repairMojibake` undoes it before `doc.New`, and refuses unless the result is valid UTF-8 containing a multi-byte character, so genuine latin-1 and ASCII pass through.
- Other JSON files in the export hold no post text or duplicate edit history, and are not read.

**htmlsite** (a hand-built site, one page per post)
- Separate from `html` because `<title>` is the same site banner on every page. The title is the `<h1>`; the first `<h2>` is a date line. Both are removed from the text. An `<h2>` that does not parse as a date is kept as writing.
- The filename date is used, never the `<h2>`, which is often stale from a template.
- `<h1>title</h1>` or no `<h1>` falls back to the filename slug. A ` copy` suffix is dropped from slugs.
- Pages without `<p>` tags are hand-wrapped; `unwrap` joins wrapped lines and keeps blank lines as paragraph breaks.

**substack** (`posts.csv` plus `posts/*.html`)
- Titles, subtitles and dates come from `posts.csv` in `Prepare`; the HTML is body only. `post_id` is the native key.
- The subtitle goes at the head of the text, not the title (a summary repeated on every chunk flattens vectors).
- Unpublished drafts (`is_published: false`) are skipped.

**goodreads** (`goodreads_library_export*.csv`)
- Only rows with `My Review` are documents; the rest are shelved books.
- `Book Id` is the native key.
- The title is `<book> by <author> <stars>`. The author makes a search for their name find reviews that never mention it; the rating is star glyphs, which the tokeniser drops, so it cannot pollute lexical search. Unrated books get no stars.
- The date is `Date Read`; a review without one is undated. `Date Added` is never used: it is when the book was shelved. `2012/01/01` on older rows is kept as given.
- No `RecordCounter`: most rows are unreviewed books, which would trip the quiet-corpus warning on every digest.

### Document identity

`doc.MakeID` hashes `source + nativeKey`, falling back to `source + content hash` when a format has no native key. Sixteen hex characters, because they are read and grepped by hand. For `html` and `text` the key is the relative path. Every document carries `ContentHash`, which lets a re-digest skip unchanged documents.

Only duplicate IDs *within* a corpus are dropped (`dedupeIDs`). The same writing in two corpora keeps both IDs; `dupes` relates them.

### Chunking

`doc.Split` splits on paragraphs up to `TargetChars`, breaks oversized paragraphs on sentence ends, and folds a runt tail into its predecessor. The sizes are constants because chunk IDs encode the boundaries; `Manifest.CheckChunking` enforces them.

Search returns documents, scored by their best chunk rather than the sum, so length is not rewarded.

### Hybrid retrieval

BM25 and vector search over the same chunks, fused by reciprocal rank. Lexical finds proper nouns, which have no semantic neighbourhood; vectors find ideas with no shared words. RRF is used because the two scores are not comparable.

Both retrievers must index the same text, `doc.Indexed`, or a word only in a title is findable one way and not the other. Tokenisation is unstemmed; morphology is the vector half's job.

### Dupes

Compares documents across corpora (never within one) dated within ±2 days, by Jaccard over 5-word shingles, threshold 0.5. `ContentHash` is not used: hand-copied text rarely matches byte for byte. Quote characters are folded to ASCII at comparison time only, never upstream of a document ID. Matches are unioned into groups, since the same writing often exists in three corpora. The winner is the highest `priority`, ties broken by corpus name then document ID. The pass only reports; search is unaffected. The constants are fixed because scores fall almost entirely below 0.2 or above 0.5.

### Timeline, export and serve

`timeline.Build` runs dupes and keeps one entry per group (the winner, listing the other copies). Entries are newest first, undated last. The HTML page puts undated writing in its own section at the top, so the bottom of the page is the oldest writing. Long entries collapse behind `<details>`; the corpus filter is pure CSS.

`coroner serve` pre-renders the timeline JSON at `/api/writing.json` and serves the front end at `/`. The front end comes from `internal/webui`, which embeds `internal/webui/dist` only under the `webui` build tag. `make` builds `web/` and sets the tag; a plain `go build` compiles the stub, and `serve` falls back to the static HTML page. `internal/embed` means ollama embeddings, hence the name `webui`.

### The store

```
digested/
  manifest.yaml           what built this, and with which model
  <corpus>.docs.jsonl     one Document per line, sorted by ID
  <corpus>.chunks.jsonl   one Chunk per line, sorted by document then index
  <corpus>.vec            chunk vectors, same order as the chunks file
  writing.html / .json    coroner export output
```

One set per corpus, so adding one export never re-embeds another. Vector reuse is keyed on `doc.Hash(doc.Indexed(...))`, so an edited title re-embeds its chunks. JSONL with `SetEscapeHTML(false)` keeps the corpus greppable. `ReadVectors` length-checks against the header because the file has no per-record framing. Writes go to a temporary name and are renamed. Loading is brute force; the corpus is tens of megabytes.

### Embeddings

Local ollama only, with no fallback: a silent degrade would change results depending on whether a daemon was up. `search -mode=lexical` is the explicit opt-in that runs without it. `Probe` runs before any work. `digest.Embedder` is an interface so reproducibility can be tested with a deterministic stub.

### Logging

`internal/corlog` resolves `app.log` via `os.Executable()`, and a failure to open it warns rather than exits. The console gets colour; `app.log` gets identical text with no escape codes (`TestLogFileNeverGetsColorCodes`). Use the semantic helpers, not `color` directly. `search -format=json` and `export -out=-` write only data to stdout.

## Build and CI

`LDFLAGS` inject `main.buildContext` (`"development"` logs to the working directory, anything else beside the executable) and `internal/version.Version`/`.Commit`/`.BuildTime`. The version goes into every digested manifest; the unversioned default is `dev`.

- `ci.yml`: gofmt, vet, test, `go test -race` (needs cgo, so it only runs reliably here), and `make all`, which needs node (`web/.nvmrc`).
- `build_release.yml`: on a `v*.*.*` tag, `make release` and a GitHub Release with both binaries; `workflow_dispatch` builds without publishing. Needs `fetch-depth: 0` for `git describe`.

`BINARY_NAME` must stay `coroner`: both workflows name `coroner` and `coroner.exe`. `.gitattributes` pins `*.go` to LF, so `gofmt -l .` is trustworthy on Windows.

## Known rough edges

Present as written; not bugs to fix unless asked:

- An HTML document whose `<h1>` repeats its title shows the title twice in search results.
- `Chunk.Start`/`End` bracket the untrimmed span while `Chunk.Text` is trimmed.
- `search -depth` has no evaluation set to tune it against.
- Facebook posts carry no URL; the export's `external_context.url` is the shared link, not a permalink.
- Search results show no title line for untitled sources, so Facebook results look different from HTML ones.

## Planned work

`TODO.md`. Check it before proposing new subcommands. Anything Claude needs from Justin (an export not yet handed over, a decision) goes there too.
