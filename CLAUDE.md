# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`coroner` is a single-binary CLI that digests bodies of personal writing into a searchable corpus, then searches that corpus by concept rather than by keyword. Two verbs: `digest` reads exports into a shared digested directory, `search` queries across everything digested.

Four direct dependencies: `gopkg.in/yaml.v3` (config and manifests), `github.com/fatih/color` (console output), `golang.org/x/net/html` (HTML extraction), `golang.org/x/text` (Unicode normalisation).

It is the sibling of `hecato` and follows its patterns deliberately. Where it departs, the departure is noted below.

## Commands

```bash
go build -o coroner.exe cmd/coroner/main.go   # quick local build, no version injection
make all                                       # cross-compile linux + windows, dev logging
make release                                   # same, but BUILD_CONTEXT=release (what CI runs)
make test
make vet

coroner initsource -source=source/substack -type=substack
coroner digest
coroner search "choice"
coroner search "branching logic" -hits=25 -format=json
coroner sources
coroner stats
coroner stats -sample=10
```

A single test: `go test ./internal/doc -run TestSplitIsDeterministic -v`.

Every package has tests except `internal/examples`, `internal/version` and `internal/embed`. `embed` is untested because everything in it is an HTTP conversation with ollama; the code that depends on it is tested through the `digest.Embedder` interface instead.

## The determinism contract

This is the constraint the whole design serves, and the reason to be careful before changing anything in `internal/doc` or `internal/store`.

**Guaranteed:** given the same source bytes, the same coroner version and the same embedding model, digesting twice produces byte-identical `.docs.jsonl`, `.chunks.jsonl` and `.vec` files. `TestDigestIsReproducible` asserts this by digesting the same tree twice at different worker counts and comparing bytes.

**Not guaranteed:** embeddings across an ollama upgrade or a model re-pull. A forward pass is not sampling, so it is stable in practice on one machine, but nothing promises bit-identical floats across versions. This is why `digested/manifest.yaml` records the model name, ollama's digest for it, and the vector width — and why `Manifest.CheckEmbed` is a hard error rather than a warning. Mixed vector spaces do not crash; they return a confident ranked list that is meaningless for half the corpus.

**Deliberately not reproducible:** `digested/manifest.yaml` carries `digested_at`. Keeping the one wall-clock fact in one file is what lets you diff two digested directories and see only what actually changed.

Four things break determinism if you are not deliberate, and all four have been hit at least once here:

- **Go randomises map iteration.** Anything accumulated in a map and then serialised, ranked or summed must be sorted first. See `ruleSet.all()`, `fuse()`, `buildBM25`'s per-chunk term sort, and `SaveManifest`.
- **Concurrency reorders results.** `parseAll` writes into per-file slots and merges in file order; `store.Write` sorts documents and chunks itself rather than trusting the caller.
- **Float summation order.** BM25 iterates sorted unique query terms over index-ordered posting lists. `embed.BatchSize` is a constant rather than something derived from the job size, so batches are a function of the corpus and not of how much work happened to be left.
- **Unicode.** `doc.Normalise` composes to NFC before anything is hashed. Without it the same essay exported by two tools is two documents.

## Architecture

Digest and search are two pipelines that meet at the digested directory.

**Digest:** `cmd/coroner` → `internal/digest` → `internal/corpus` (manifest) → `internal/parse` (per-format parser) → `internal/doc` (normalise, hash, chunk) → `internal/embed` (ollama) → `internal/store` (JSONL + vectors).

**Search:** `cmd/coroner` → `internal/store` (load all) → `internal/search` (BM25 + vectors + RRF) → results.

**Links:** `internal/links` renders the outbound links an export recorded, via the optional `parse.LinkLister` interface. Only `facebook` implements it.

Links are deliberately **not documents**. A URL tokenises into fragments that mean nothing to a reader and everything to a lexical index — `https`, `www`, `com`, a hex tracking parameter — and there are thousands of them, so indexing them would degrade every search in order to make one kind of lookup work. What makes the artefact worth reading is not the URLs but the commentary written alongside them.

`coroner links` runs through `digest.ParseOnly`, which shares the walk and worker pool with `Run` but discards the documents and embeds nothing — so it works with ollama absent. Sharing that path is deliberate: a second route through the same export could drift out of step with the first.

Two measured details: the export repeats one `external_context` across a record's attachments, which accounted for 317 of 2,848 entries, so the same URL at the same second is deduplicated — but the same URL at a *different* time is a genuine re-share and is kept, because when you shared something again is what you would be reading the file to find out. And `t.co` is a third of all links: Twitter cross-posts, opaque and mostly dead, but they carry commentary so they are not dropped.

**Stats:** `internal/stats` describes a digested corpus — counts, date histogram, length spreads, vocabulary. It exists because a corpus is not something you can eyeball: five thousand documents you cannot read are indistinguishable from five thousand that parsed badly, and parser failures are quiet. Text truncated at the first newline shows up as a suspiciously tight word-count spread; a dropped year shows up as a gap in the histogram; boilerplate shows up as a low hapax share. `coroner stats -sample=N` prints documents spread evenly through the corpus rather than from the front, since documents are sorted by a hash and the first few are a fixed arbitrary slice that would hide a parser failing on later records.

### Subcommands, not `-method`

The one substantial departure from hecato. Hecato's `-method` switch works because every method takes the same flags; `digest` and `search` genuinely do not. Each subcommand builds its own `flag.FlagSet`, and `commonFlags` holds what they share.

`commonFlags.reorder` exists because Go's `flag` package stops parsing at the first non-flag argument, so `coroner search "choice" -hits=25` would parse as a query of `choice -hits=25` with the flag silently ignored. It moves flags ahead of positionals, asking the FlagSet whether each flag consumes the next argument rather than guessing, and re-emits `--` so a query starting with a dash survives. This was a live bug, not a hypothetical.

Flag precedence is **explicit flag > config > built-in default**, implemented with `fs.Visit` exactly as hecato does it — `Visit` walks only flags actually typed, which is the only way to tell `-hits=10` from the identical default.

### Sources and manifests

A source directory holds one export plus a `corpus.yaml` saying what it is. Coroner never sniffs formats: the guess would depend on which file the walk reached first, and a digest that depends on walk order is not reproducible.

`Manifest.Name` is load-bearing. It is part of every document ID and it names the files that corpus writes into the digested directory, so `validName` restricts it to `[a-z0-9_-]{1,64}` — restricting rather than escaping, because a name containing a separator would let a manifest write outside its own corpus.

`corpus.example.yaml` and `coroner.example.yaml` at the repo root are committed copies of what `initsource` and `initconfig` write. `TestExampleConfigMatchesGenerator` and `TestExampleManifestMatchesGenerator` are the only things keeping them in sync — change a starter and regenerate the example or those tests fail.

### The parser pattern

`parse.Parser` is two phases: `Prepare` runs once and is where a format reads its sidecar metadata (Substack's `posts.csv`); `ParseFile` is called concurrently and must be read-only afterwards. Adding a format means a new file in `internal/parse`, an entry in `constructors`, and a line in the manifest starter's type list.

`text`, `html` and `facebook` are implemented. `substack` is **registered but pending** — `pendingParser` returns a clear error saying it is not written yet. Registered rather than absent on purpose: a manifest saying `type: substack` is not a typo and should not be told it is one. It waits on a real export, because writing a parser against a documented format rather than real bytes is how you get one that is confidently wrong about encoding, and encoding errors quietly corrupt every document ID in a corpus.

`parse.RecordCounter` is an optional interface for parsers whose files hold many records. Only `facebook` implements it. It exists because "1 file parsed, 5,651 documents" gives no way to tell a photo-heavy export from a parser that has silently started dropping things.

### What the Facebook export actually looks like

Written against a real 8,195-record export, and these numbers are why the parser does what it does. Do not "simplify" any of them without re-measuring.

- **The `title` field is chrome**, not a title: "Justin Crosby updated his status." repeated across thousands of records. Documents are given **no title at all**. Since `doc.Indexed` prepends the title to every chunk before embedding, using it would inject identical boilerplate into every vector in the corpus and flatten exactly the distinctions search exists to find. `TestFacebookLeavesTitleEmpty` guards this.
- **There is no post identifier anywhere.** Identity is built from `timestamp` plus a hash of the text. Measured: timestamps alone collide 327 times (batch uploads share a second), text alone collides 31 times (people repeat themselves), the two together collide twice — and those two are genuinely the same post recorded twice, which `dedupeIDs` drops and reports.
- **Photo captions are real writing.** 1,148 media descriptions, all hand-written, none of them Facebook's generated alt text. But 905 of those were byte-identical to the post they hung under, because Facebook copies a caption into both places, so they are deduplicated before being appended. Skipping captions entirely would lose documents; appending blind would double term frequencies inside them.
- **The export is double-encoded**: UTF-8 bytes re-read as latin-1, so `don't` arrives as `donâ€™t` and an emoji as four accented letters. `repairMojibake` undoes it, and runs *before* `doc.New` because every document ID derives from the text. Measured on the real export: 1,200 strings repaired, 5,595 untouched, zero mojibake markers surviving. Its guards matter more than its transformation — it refuses unless the result is valid UTF-8, the input had a byte above ASCII, and the result contains a multi-byte character, so genuine latin-1 text and plain ASCII pass through unharmed.
- **Only the posts files are read.** `your_posts*.json`, globbed because a large export splits into `_1`, `_2`. `posts_on_other_pages_and_profiles.json` looks promising and holds no post text at all; `edits_you_made_to_posts.json` is edit history that would duplicate everything.
- **31% of records hold no text.** Photos and bare link shares. That is normal, and the reason the quiet-corpus warning threshold is 80% rather than something that would fire here.

### Document identity

`doc.MakeID` hashes `source + nativeKey`, falling back to `source + content hash` when a format has no native key. Sixteen hex characters, because these get read and grepped by hand.

The tradeoff: `html` and `text` use the relative path as the native key, so fixing a parser bug leaves IDs stable but moving a file changes them. For a keyless source it is the other way round. Every document also carries `ContentHash` regardless, which is what the future dedupe pass will compare.

Duplicates *across* corpora are deliberately left alone. The same essay in three exports gets three IDs, and relating them is a separate pass that has to measure how similar they are rather than assume. Only duplicate IDs *within* one corpus are dropped, which means the export listed something twice.

### Chunking

`doc.Split` splits on paragraph boundaries up to `TargetChars`, breaks an oversized paragraph on sentence ends, and folds a runt tail back into its predecessor. The constants are constants rather than flags because chunk IDs encode the boundaries: a corpus chunked under different constants cannot be added to incrementally, which is what `Manifest.CheckChunking` enforces.

Search returns **documents**, with the best-matching chunk as the snippet. A document's score is its best chunk's score, not the sum: summing rewards length, so a long essay mentioning the subject five times in passing would outrank a short post entirely about it.

### Hybrid retrieval

Two retrievers over the same chunks, fused by reciprocal rank. This is not hedging. A proper noun has almost no semantic neighbourhood, so vector search returns everything about politics generally while missing the post that names the person once; an idea is the reverse, since nothing lexical connects "choice" to "branching logic". A tool picking one retriever works on half its queries.

RRF is used because the two scores are not comparable — BM25 is unbounded, cosine is in [-1, 1] — and throwing the magnitudes away keeps only what both agree on.

**Both retrievers must index the same text.** `doc.Indexed` prepends the title to a chunk, and both `digest` (for embedding) and `search.NewEngine` (for BM25) call it. When only the embedder saw titles, a word appearing solely in a title was findable semantically and invisible to keyword search. Two retrievers disagreeing about what text exists is a bug that presents as bad ranking, which is the hardest kind to notice.

Tokenisation is deliberately unstemmed: the lexical half exists to be literal and catch exactly the queries the vector half is bad at. Morphology is the vector half's job.

### The store

```
digested/
  manifest.yaml           what built this, and with which model
  <corpus>.docs.jsonl     one Document per line, sorted by ID
  <corpus>.chunks.jsonl   one Chunk per line, sorted by document then index
  <corpus>.vec            chunk vectors, same order as the chunks file
```

One set per corpus, which is what makes digesting incremental — adding a Substack export must not re-embed a Facebook one. Reuse is keyed on `doc.Hash(doc.Indexed(...))`, so an edited title correctly invalidates the vectors of every chunk under it.

JSONL because the corpus should stay greppable; `SetEscapeHTML(false)` for the same reason. Vectors are a small binary format because 768 floats per chunk are not information anyone extracts by eye, and JSON would triple the size. The vector file carries no per-record framing, so `ReadVectors` length-checks against the header — a truncated file must be caught there or it returns garbage.

Everything is written to a temporary name and renamed, so an interrupted digest leaves the previous corpus intact.

Loading is brute force: under ten thousand documents the whole corpus is tens of megabytes. An ANN index would add a persisted structure that can drift out of step with the data it indexes, to solve a problem this corpus does not have.

### Embeddings

Local ollama only. There is no hosted option and no lexical fallback: a silent degrade would mean the same query returning different results depending on whether a daemon was up, which is the class of quiet wrongness everything else here is built to avoid. `-mode=lexical` is the explicit opt-in and the only mode that runs without ollama.

`Probe` runs before any real work, so a run that cannot embed fails before the export is walked rather than after.

`digest.Embedder` is an interface rather than `*embed.Client` specifically so the reproducibility claim can be tested with a deterministic stub. A real model would be testing the wrong thing.

### Logging

`internal/corlog` is hecato's `heclog`, with two fixes to things hecato's own roadmap lists as bugs: the log path resolves via `os.Executable()` rather than `os.Args[0]`, and a failure to open `app.log` warns instead of being fatal, so the tool can be installed somewhere unwritable.

The dual-output invariant is unchanged and must stay: the console gets colour, `app.log` gets identical text with no escape codes. `TestLogFileNeverGetsColorCodes` guards it. Use the semantic helpers rather than reaching for `color` directly.

`search -format=json` writes only JSON to stdout, so it can be piped; the human-facing output still goes to the log.

## Build metadata

Four values injected by the Makefile's `LDFLAGS`, all with working defaults under a plain `go build`:

- `main.buildContext` — `"development"` writes `app.log` to the working directory, anything else beside the executable. Unlike hecato this is not a hardcoded machine path, because this is a public repo.
- `internal/version.Version` / `.Commit` / `.BuildTime` — from `git describe --tags --always --dirty`, `git rev-parse --short HEAD`, and a UTC timestamp. The version is written into every digested manifest, which is why the unversioned default is `dev` rather than something plausible.

## CI

- `ci.yml` — gofmt, vet, test, `go test -race`, and `make all`. The race detector needs cgo and so cannot run on a stock Windows dev box; this is the only place it reliably executes.
- `build_release.yml` — on a `v*.*.*` tag, builds via `make release` and publishes a GitHub Release with both binaries. `workflow_dispatch` runs the same build and uploads artifacts without publishing, so it is a safe dry run. Needs `fetch-depth: 0` because the Makefile calls `git describe`.

`BINARY_NAME` in the Makefile must stay `coroner`: both workflows reference `coroner` and `coroner.exe` by name.

Unlike hecato, `.gitattributes` pins `*.go` to LF, so `gofmt -l .` is trustworthy locally on Windows rather than listing every file.

## Privacy posture

`source/` and `digested/` are gitignored and must stay that way. The methods are public; the writing is not. Anyone should be able to clone this, point it at their own exports and get the same behaviour, which is also why nothing resolves against a path baked in for one machine.

## Known rough edges

Present as written — don't treat them as bugs to fix unless asked:

- An HTML document whose `<h1>` repeats its title shows that title twice: once as the result heading, once at the head of the snippet. The `h1` is genuinely part of the article body, and stripping it heuristically risks removing real text.
- `Chunk.Start` and `Chunk.End` bracket the untrimmed span, while `Chunk.Text` is trimmed. Offsets locate the chunk; they are not byte-exact against `Text`.
- `-depth` is exposed on `search` but there is no evaluation set to tune it against, so the default is the only value anyone has a reason to use.
- Facebook posts carry no URL. The export has `external_context.url` for link shares, but that is the link that was shared rather than a permalink to the post, and putting it in `Document.URL` would imply the wrong thing.
- Search results show no title line for sources that have no titles. Deliberate — see the comment in `printResults` — but it does mean Facebook results look different from HTML ones.

## Roadmap

Planned work lives in the header comment block of `cmd/coroner/main.go`. Check there before proposing new subcommands. The near-term items are the Facebook and Substack parsers, and the cross-corpus dedupe pass.
