# TODO

Working notes for coroner. This file is the canonical list of planned work and of
things Claude needs from Justin in order to continue; the roadmap comment in
`cmd/coroner/main.go` points here rather than duplicating it, because two lists
of planned work drift and then neither one is trustworthy.

Convention: an item says what the work is, and *why it is shaped that way*, in
the same register as CLAUDE.md. An item nobody can reconstruct the reasoning for
in three months is an item that gets done wrong.

---

## Milestone: one searchable corpus

The near-term goal. All three sources digest cleanly and `coroner search` spans
them. No dedupe, no precedence, no overlap handling — those are deliberately
deferred, because a corpus you cannot search is not a thing you can make
judgements about overlap from. Get it whole first, then argue with it.

Nothing here requires an architecture change. The digested directory already
holds one set of files per corpus and search already loads all of them, so
"one corpus" is a property of the store as it stands, not something to build.

**Definition of done:**

1. **Done** — `htmlsite` parser written, registered, `initsource` run against
   `source/html_posts`. Validated on all 189 real pages: 189 documents, none
   empty, none missing a date or title, no duplicate IDs, word counts spread
   53–2329 with a median of 551 rather than the tight cluster that would mean
   text truncated at the first newline.
2. **Done** — `substack` parser written, replacing the `pendingParser`
   placeholder. Validated on the real export: 139 files in, 134 documents out
   with the five unpublished drafts skipped, no duplicate IDs, and every
   document carrying both a title and a date. Word counts 239–3022, median 812.
3. **Done** — embedding model pulled (Justin, 2026-08-06).
4. All three digested together; `coroner stats` looks sane per corpus — no
   suspiciously tight word-count spread, no missing years, no collapsed titles.
5. `coroner search` returns results drawn from all three.

Nothing has been digested yet. The current priority is a mergeable PR #3, not a
built corpus; digesting is step 4 and comes after the parsers land.

Once this lands on `main`, revisit embeddings — model choice and dimensions were
picked before there was a corpus to judge them against. Deferred deliberately:
changing the model invalidates every vector, so it is a thing to do to a working
corpus, not on the way to building one.

Post-milestone, and only then: overlap, precedence, dedupe.

---

## Next up

### 1. Substack parser — done, kept here for the reasoning

**Status: written and validated.** `internal/parse/substack.go`, ten tests,
registered in place of the `pendingParser`. What follows is why it is shaped the
way it is.

Measured contents:

- **139 `.html` files** in `posts/`, named `<post_id>.<slug>.html`.
- **`posts.csv`** — the sidecar: `post_id, post_date, is_published,
  email_sent_at, inbox_sent_at, type, audience, title, subtitle, podcast_url`.
- **218 `.delivers.csv` / `.opens.csv`** files, also in `posts/`. Per-subscriber
  email analytics. Not writing, and they contain subscriber addresses — a
  privacy matter, not merely noise.
- **`email_list.ambivalentdad.csv`** — subscriber list. Same.

Shape of the work:

- `Prepare` reads `posts.csv` into a `post_id` → metadata map. This is the case
  the two-phase parser interface was designed for; CLAUDE.md names Substack as
  the example.
- `Include()` must be a **narrow allowlist** — `posts/*.html` only. Not a CSV
  exclusion bolted on afterwards: an allowlist means a future export adding
  another CSV cannot quietly start indexing subscriber addresses.
- **`post_id` is a genuine native key.** Unlike Facebook, which had to synthesise
  identity from timestamp plus text hash, Substack numbers its posts. IDs
  survive a slug edit, and slugs are in the filename.
- **Title comes from the sidecar, not the body.** The post HTML is a body
  fragment starting at `<p>` with no `<h1>`.
- **`subtitle` is real writing.** Decide deliberately whether it joins the
  indexed text and record why — a subtitle prepended to every chunk carries the
  same flattening risk that made Facebook's `title` field unusable.
- **Strip the UI chrome**: 374 `<button>`, 374 `<svg>` with `<polyline>`/`<line>`
  children, plus `<picture>`/`<source>`. Share buttons and audio players are not
  text, and left in they inject identical boilerplate into every document.
- **`<figcaption>` is real writing** — 94 of them. Check for duplication against
  surrounding body text before appending, the way Facebook's captions needed
  (905 of 1,148 were byte-identical to their post).
- **Unpublished drafts are skipped** (Justin's call, 2026-08-06): only
  `is_published: true` is corpus. That leaves 134 documents from 139 files. The
  five drafts are exactly the five posts with no title and no date — two of them
  (`slop`, 1265 words; `fears`, 861 words) read as finished essays, the other
  three are scaffolding full of `!!!! IMAGE HERE !!!!` markers and `xxxxxx`
  placeholder names, and nothing but reading them tells the groups apart. Taking
  the published flag at its word is the non-guessing option: it is the author's
  own record of what counts as finished. With drafts gone every remaining
  document has both a title and a date, and the shortest is 239 words rather
  than 58.

### 2. `htmlsite` parser — done, kept here for the reasoning

**Status: written and validated.** `internal/parse/htmlsite.go`, registered as
type `htmlsite`, ten tests. `source/html_posts/corpus.yaml` exists. What follows
is why it is shaped the way it is; the measurements are the reason not to
"simplify" any of it without re-measuring.

This is Justin's hand-built site: 189 posts he copied out of Facebook into HTML
by hand, spanning 2016–2026 (84 in 2024, 45 in 2025, single digits before 2020).

**It must not use the existing `html` parser.** That parser takes `<title>` as
the document title, and here:

- **188 of 189 `<title>` elements are the string `xolsiion writing`** — a
  site-wide banner, not a title. The one exception is a real title, which reads
  as an authoring slip rather than a pattern to rely on.
- Since `doc.Indexed` prepends the title to every chunk before embedding, using
  `<title>` would inject the same six characters into every vector in the corpus
  and flatten exactly the distinctions search exists to find. This is the
  Facebook `title` failure arriving by a different route, and it is the second
  time this export family has presented it.
- **`<h1>` is the real title** — 186 of 189 have one. The 3 without:
  `2016.11.01-politics-post.html`, `2016.11.01-politics-post2.html`,
  `2024-11-07-let-him-in.html`.

Dates are the other reason it needs its own parser — the generic one extracts
none, and here there are **two independent sources that disagree in format**:

- **Filename prefix**: 168 are `YYYY-MM-DD`, 11 are `YYYY.MM.DD`, and 10 use a
  single-digit month or day (`2020-7-10-time-well-spent.html`,
  `2025-04-7-own-it.html`). All 189 carry one.
- **`<h2>` in the body**: 175 `MM.DD.YYYY`, 5 `MM.D.YYYY`, 2 `M.DD.YYYY`, 1
  `MM-DD-YYYY`. Six files have no `<h2>` at all.

These were cross-checked across the whole export before the parser was written:
**they agree on 169 pages and disagree on 14, and the `<h2>` is wrong every
time.** So the filename wins unconditionally and the `<h2>` is not consulted as a
fallback. A source that is wrong 14 times out of 183 is not one to reach for when
the reliable one is already complete, and a wrong date is worse than no date
because it silently reorders everything sorted by recency.

The 14, listed because they are **errors in the source files** Justin may want to
fix — coroner now ignores them, so nothing is broken either way:

| page | filename | `<h2>` says |
| --- | --- | --- |
| `2016.11.11-why-vote-for-trump` | 2016-11-11 | 2016-10-25 |
| `2020-05-22-100k-dead` | 2020-05-22 | 2020-02-27 |
| `2020-10-13-electoral-college` | 2020-10-13 | 2020-10-15 |
| `2020-10-15-why-vote-biden` | 2020-10-15 | 2020-10-25 |
| `2021-05-24-captialism` | 2021-05-24 | 2021-05-23 |
| `2023-04-14-book-sale` | 2023-04-14 | 2023-04-13 |
| `2024-01-29-link-dump` | 2024-01-29 | **2014**-01-29 |
| `2024-04-24-people-dont-buy-books` | 2024-04-24 | 2024-04-23 |
| `2024-07-28-link-dump` | 2024-07-28 | 2024-07-29 |
| `2024-09-19-Evangelicals-For-Kamala` | 2024-09-19 | 2024-09-13 |
| `2025.07.28-oppenheimer` | 2025-07-28 | 2016-10-25 |
| `2025.08.15-newsome-ca-tx-gerrymander` | 2025-08-15 | 2016-10-25 |
| `2025.08.16-what-is-government` | 2025-08-16 | 2016-10-25 |
| `2025.08.18-8-years-after-charlottesville` | 2025-08-18 | 2016-10-25 |

The pattern is legible: five carry `10.25.2016`, the date of the very first post,
whose page was evidently used as a template and its heading never updated. One is
a year typo'd a decade out. The rest are off by a day or two, which reads like
writing on one day and dating it another.

Also:

- **File mtimes are not post dates.** They range across 2023–2025 independently
  of content, so they are edit times. Do not fall back to them.
- **Six filenames end in ` copy`** (`2020-06-30-trump-era copy.html` and five
  others). **None has a surviving twin** — checked. They are ordinary posts whose
  filename records an editing accident, and they must not be dropped as
  duplicates. `2025-07-19--tank-disaster copy.html` also has a doubled dash.
- **Encoding is clean**: all 189 valid UTF-8, zero mojibake markers, 32 files
  carrying non-ASCII. No `repairMojibake` equivalent is needed. This is itself
  evidence for the precedence rule below — the hand-copying passed through a
  rendering step that repaired what the Facebook export still carries raw.
- Native key is the relative path, as with the generic `html` parser, so IDs are
  stable under content edits and change if a file is renamed. Given the ` copy`
  names may get tidied, that tradeoff is worth stating out loud but not worth
  changing — the alternative makes every typo fix a new document.

Two more things found while writing it, both of which shaped the code:

- **Five pages have the literal `<h1>title</h1>`** — started from a template and
  never filled in. Letting the word through would give five unrelated posts the
  same title, so it falls back to the filename slug, as do the three pages with
  no `<h1>` at all.
- **Twenty-three pages have no `<p>` tags**: the writing sits directly in the
  container div, hand-wrapped at roughly eighty characters. HTML treats those
  newlines as ordinary whitespace, but `doc.Normalise` deliberately preserves
  newlines because for most formats they are authored structure — so passing the
  markup's wrapping through would put a hard break mid-sentence throughout the
  text and hand the chunker false boundaries. The parser unwraps them. Blank
  lines are kept, which a browser would not do: in about half of those pages a
  blank line is the author's only record of where a paragraph falls, and since
  the chunker splits on paragraph boundaries, honouring it recovers structure
  that would otherwise be lost. On pages that do use `<p>`, it changes nothing.

### 3. Initialise the two new sources

`coroner initsource -source=source/substack_posts -type=substack` and the
equivalent for `html_posts`. Neither has a `corpus.yaml`, so neither digests.

---

## After the milestone

### Cross-corpus dedupe, with a precedence order

Justin's rule, settled 2026-08-06 and now complete:

**`substack` > `htmlsite` > `facebook`.**

Substack wins over everything. The HTML site wins over Facebook, because those
posts were copied out of Facebook by hand — so the HTML is the curated version
(correctly encoded, given a real title, given a deliberate date) and the Facebook
record is the raw original. The encoding measurement bears this out
independently: the HTML export is clean UTF-8 throughout, while Facebook's still
carries the double-encoding `repairMojibake` has to undo.

The hard part, and the reason this is deferred rather than bundled into the
milestone: **hand-copying means `ContentHash` will not match.** The dedupe pass
CLAUDE.md anticipated compares content hashes, which catches byte-identical
duplicates and will catch approximately none of these. Editing while copying is
the normal case, not the exception.

So this needs near-duplicate detection, which means a similarity threshold, which
means a judgement call about what counts as the same post. Open questions to
settle before writing it:

- What signal? Date proximity plus text similarity is the obvious pair, and both
  corpora have reliable dates. Shingling or embedding cosine are both plausible;
  embeddings are already computed, which argues for reusing them.
- Does the result change what search returns, or is it a separate report? Silently
  changing ranking is the class of quiet wrongness the rest of the design avoids.
  A report Justin reads first is the safer default.
- Where does precedence live? A `priority` field in `corpus.yaml` generalises
  ("this corpus supersedes that one") without hardcoding two names, and keeps the
  rule next to the data it describes.
- A `priority` field in `corpus.yaml` is the obvious home for the order, since it
  generalises without hardcoding three names and keeps the rule beside the data
  it describes.

### Search: filter by date range

Most of what "what was I writing about in 2019" needs. Every document already
carries a date, so this is a filter over loaded documents rather than anything
structural. Worth more once three corpora span 2016–2026.

### Search: `-explain`

Show which query terms drove a lexical hit. Diagnostic for the half of retrieval
that is legible — there is no equivalent for the vector half, and pretending
otherwise would be worse than omitting it.

---

## Housekeeping

Small, low-risk, none of them urgent.

- **CLAUDE.md omits `internal/ignore`** from the architecture walkthrough. It is
  a real package with tests, carried from hecato, and it governs what the walk
  never sees — which makes it load-bearing for the Substack allowlist above.
- **CLAUDE.md's Commands block omits `coroner links`**, though the subcommand
  ships and has its own architecture section further down.
- **`internal/version` has no tests**, which CLAUDE.md states deliberately. Noted
  only so it does not get "fixed" by someone reading a coverage report.

## Done

- **`substack` parser** (2026-08-06). `internal/parse/substack.go` plus ten
  tests, replacing `pendingParser` in the registry. `pendingParser` itself stays
  and is now exercised directly by its test, since no registered format is
  pending — it is what the next unwritten format should get instead of being
  told its type is unknown.
- **`htmlsite` parser** (2026-08-06). `internal/parse/htmlsite.go` plus ten
  tests, registered in `parse.constructors` and listed in the manifest starter.
  `corpus.example.yaml` regenerated to match, which
  `TestExampleManifestMatchesGenerator` requires.
- **Branch consolidation** (2026-08-06). `updates` was fully contained in
  `updates-1`; PR #2 closed as superseded by #3, branch deleted locally and on
  the remote. One feature branch.
