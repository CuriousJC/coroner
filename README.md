# coroner

Digest bodies of writing into one searchable corpus, then search it by idea rather than by keyword.

Point it at exports of things you have written — a Facebook archive, a Substack export, your Goodreads reviews, a folder of HTML — and it turns them into a single indexed corpus. Then ask it about a concept. Searching for `choice` should also surface the essay about branching logic that never uses the word.

Your writing never leaves your machine: embeddings are computed by a local [ollama](https://ollama.com).

## Status

Six source types, all working end to end:

| type | reads |
| --- | --- |
| `facebook` | a Facebook "Download your information" export — the `your_posts*.json` files |
| `substack` | a Substack export — `posts.csv` plus `posts/*.html`, published posts only |
| `goodreads` | a Goodreads library export — the reviews in `goodreads_library_export.csv` |
| `htmlsite` | a hand-built HTML site — `<h1>` titles, dates in the filenames |
| `html` | loose HTML files |
| `text` | loose `.txt` or `.md` files |

The format-specific parsers handle what those exports actually do. Facebook has no post identifiers, stores captions twice, and double-encodes its text so `don't` reads as `donâ€™t`. Substack keeps subscriber analytics beside the posts, so the parser reads a narrow allowlist and none of them can end up indexed. Goodreads records no date for a review, so a review is dated by when the book was read (or 2012-01-01, Goodreads' own placeholder for old reads, when there is no read date), and each one is titled with the book, its author and your star rating.

## Install

```bash
make all      # needs Go and node; builds the browser viewer into the binary
```

`go build -o coroner cmd/coroner/main.go` also works and needs only Go; `coroner serve` then shows the static page instead of the full viewer.

You will also need ollama, with an embedding model pulled:

```bash
ollama serve
ollama pull nomic-embed-text
```

## Use

```bash
# Describe an export
coroner initsource -source=source/blog -type=html

# ...drop the export into source/blog, then
coroner digest

# Ask it something
coroner search "choice"
coroner search "branching logic" -hits=25
coroner search "Thatcher" -mode=lexical
coroner search "the cost of certainty" -format=json

coroner sources     # what has been digested
coroner stats       # counts, date histogram, vocabulary
coroner dupes       # the same writing in more than one export
coroner examples    # worked usage for everything

# Everything you wrote, in one place
coroner export      # digested/writing.html, newest first
coroner serve       # the same, browsable at http://127.0.0.1:8484

# Every link you shared, with what you said about it
coroner links -source=source/facebook
```

## Finding the same writing twice

Writing gets copied between platforms: a review cross-posted to Facebook, a Facebook post tidied up for a blog, a blog post revised for Substack. `coroner dupes` finds those copies across corpora and names which one it treats as the real one.

It compares documents dated within two days of each other by how many five-word runs they share, not by exact hash, because hand-copied text almost never survives byte for byte. Which copy wins comes from a `priority` in each corpus's `corpus.yaml`, with the higher number winning. The pass only reports: search results are never filtered or hidden because of it.

## Reading everything

`coroner export` writes every piece of writing in the corpus to one HTML file, newest first, so the bottom of the page is the first thing you wrote and scrolling up reads forward in time. Long pieces show their opening and expand on a click. Writing that exists in more than one export is listed once, under the copy `dupes` picks, with a note of where else it appears. `-format=json` writes the same list as JSON.

Checkboxes filter by corpus, by length (under 250 words, 250 to 999, 1,000 or more), and whether to show quotes: short posts that are someone else's words with an attribution such as `~ Seneca`, flagged when the corpus is digested.

The page is a single file that loads nothing from anywhere, and it is written into `digested/` by default because it holds the whole corpus.

`coroner serve` shows the same list in the browser with the same checkboxes, filtering by words, and either order. It listens on loopback only and answers only requests addressed to it by a loopback name, so nothing else on the network, and no other website open in your browser, can read it.

## Links are not part of the corpus

`coroner links` writes a browsable Markdown file — links grouped by year, newest first, each with the commentary you wrote when sharing it, plus a summary of which sites you shared most.

They are kept out of the searchable corpus on purpose. A URL tokenises into fragments that mean nothing to a reader and everything to a keyword index — `https`, `www`, `com`, a tracking parameter — and there are thousands of them, so indexing them would degrade every search to make one kind of lookup possible. The artefact stands on its own instead, and needs no ollama since nothing is embedded.

## Checking a corpus parsed properly

A corpus is not something you can read, so `coroner stats` is how you tell a good parse from a bad one:

```
  facebook
  Documents    5,651 in 6,490 chunks, 354,093 words
  Span         2009-08-04 to 2025-12-24
  Words/doc    min 1  median 20  p90 131  p99 785  max 2,162
  Vocabulary   22,077 distinct terms, 10,920 used once (49%)

    2009  █                            13
    2010  ███                          54
    ...
    2024  ███████████████████████████  554
    2025  █████████████████            358
```

A gap in the year histogram is either a year you did not write or a year the parser dropped. A suspiciously tight word-count spread means text is being truncated. A low share of once-used words means you are indexing boilerplate rather than writing. None of those are visible in a total.

`coroner digest` reports the same instinct: it counts records that produced no text, and says so out loud only when that share passes 80% — high enough that a legitimately photo-heavy export does not train you to ignore the warning.

## How searching works

Two retrievers run over the same passages and their rankings are fused.

Keyword search (BM25) is what finds names, places and unusual coinages — the things a vector is worst at, because a proper noun has almost no semantic neighbourhood. Vector search is what connects `choice` to `branching logic`, which no amount of keyword matching will do. Fusing them by reciprocal rank means a passage both retrievers liked outranks one that either loved on its own.

Results are documents, with the best-matching passage shown as the snippet, and each result says which retriever found it:

```
  1. ██████████████  blog · 2021-03-14 · both (lexical #1, vector #1)
     The Shape of a Decision
     Every fork in the road is really a branching structure, and the branches...
```

The bar is relative to the top result. The underlying score is a sum of reciprocal ranks and has no absolute meaning, which is why it appears as a bar here and as a number only in `-format=json`.

## Reproducibility

Digesting the same sources twice produces byte-identical output, given the same coroner version and embedding model. Parsing, normalisation, chunking, identifiers and ranking are deterministic by construction — no wall-clock time, no unsorted map iteration, no dependence on which worker finished first.

The one thing outside that guarantee is the embedding model itself: a forward pass is stable in practice, but nothing promises identical floats across an ollama upgrade or a model re-pull. So the digested corpus records the model and its digest, and coroner refuses to search a corpus whose vectors came from a different model rather than silently comparing two unrelated vector spaces.

## Your corpora stay yours

`source/` and `digested/` are gitignored. This repository holds the methods, not the writing. Clone it, point it at your own exports, and it behaves the same way.

## License

See [LICENSE](LICENSE).
