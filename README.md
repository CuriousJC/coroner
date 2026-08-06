# coroner

Digest bodies of writing into one searchable corpus, then search it by idea rather than by keyword.

Point it at exports of things you have written — a Facebook archive, a Substack export, a folder of HTML — and it turns them into a single indexed corpus. Then ask it about a concept. Searching for `choice` should also surface the essay about branching logic that never uses the word.

Your writing never leaves your machine: embeddings are computed by a local [ollama](https://ollama.com).

## Status

Working end to end for `facebook`, `html` and `text` sources. The `substack` parser is registered but not yet written — it is waiting on a real export to be written against rather than guessed at.

The Facebook parser was built against a real 8,195-record export and handles the things that export actually does: no post identifiers, captions stored twice, and text that arrives double-encoded so `don't` reads as `donâ€™t`.

## Install

```bash
go build -o coroner cmd/coroner/main.go
```

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
coroner examples    # worked usage for everything

# Every link you shared, with what you said about it
coroner links -source=source/facebook
```

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
