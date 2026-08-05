# coroner

Digest bodies of writing into one searchable corpus, then search it by idea rather than by keyword.

Point it at exports of things you have written — a Facebook archive, a Substack export, a folder of HTML — and it turns them into a single indexed corpus. Then ask it about a concept. Searching for `choice` should also surface the essay about branching logic that never uses the word.

Your writing never leaves your machine: embeddings are computed by a local [ollama](https://ollama.com).

## Status

Working end to end for `html` and `text` sources. The `facebook` and `substack` parsers are registered but not yet written — they are waiting on real exports to be written against rather than guessed at.

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
coroner examples    # worked usage for everything
```

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
