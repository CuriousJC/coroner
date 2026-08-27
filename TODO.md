# TODO

Working notes for coroner: what is planned, what was decided and why, and what is
needed from Justin. Written to be read cold — assume whoever picks this up has no
memory of the conversation that produced it.

**Division of labour with CLAUDE.md.** CLAUDE.md describes the system as it *is*:
architecture, the determinism contract, and the measured facts behind each
parser. This file is everything that is not yet true — planned work, open
questions, decisions and their reasoning, and things waiting on someone outside
the repo. Parser internals are not repeated here; two descriptions of the same
thing drift and then neither is trustworthy. Where a parser's behaviour matters
to a decision below, this file names the decision and points at CLAUDE.md for the
mechanism.

Convention: an item says what the work is *and why it is shaped that way*. An
item nobody can reconstruct the reasoning for in three months is one that gets
done wrong.

---

## What this is ultimately for

Justin, 2026-08-06, in his own framing: **a single corpus of all his writing, so
he can stop worrying about how it is scattered.**

Worth having at the top, because it is the thing individual decisions get judged
against and it is not derivable from the code. The scattering is the problem;
searching is the means. A change that makes search cleverer but leaves him still
tracking which export a piece lives in has not moved toward this.

It also sets a bar for the dedupe pass that is worth stating plainly: a report
that lists duplicates still leaves him holding the question of which copy is the
real one. See the note in that section — the settled decision is report-first,
and whether that is the end state or a stepping stone is genuinely open.

---

## Where things stand — 2026-08-06

**All five parsers are written**: `text`, `html`, `htmlsite`, `facebook`,
`substack`. Nothing in `parse.constructors` is pending. `pendingParser` survives
unregistered, exercised directly by its test, so the next format named before it
is written gets "not written yet" rather than a lie about being unrecognised.

**Branch and PR.** `main` is at **`b200e11`**. Two PRs merged on 2026-08-06:
[#3](https://github.com/CuriousJC/coroner/pull/3) (the five parsers, links,
stats, TODO.md) and [#4](https://github.com/CuriousJC/coroner/pull/4)
(`workflow_dispatch` plus the first-digest findings). PR #2 was closed as
superseded.

**Branch `fix/ci-outage-note`**, off `b200e11`, corrects the CI diagnosis in
`TODO.md` and `.github/workflows/ci.yml` — the first version of both blamed the
workflow file and the repo's Actions settings, and the cause turned out to be a
GitHub outage. Opening that PR is also the cheapest way to find out whether the
`pull_request` trigger fires again, which the outage left untested.

The three merged branches — `updates-1`, `feature/digest-milestone` and
`feature/initial-scaffolding` — were deleted locally and on the remote. The last
of those was never an ancestor of `main`: its tip `31a3d14` was cherry-picked
onto `updates-1` as `31b6b46`, so the content is on `main` under a different
hash and the branch was superseded rather than abandoned.

**The corpus is digested.** All three sources, 5,974 documents in 7,977 chunks,
in 2m32s. Counts came out exactly as predicted:

| directory | manifest `type` | manifest `name` | documents | chunks |
| --- | --- | --- | --- | --- |
| `source/facebook_posts/` | `facebook` | `facebook_posts` | 5,651 | 6,490 |
| `source/html_posts/` | `htmlsite` | `html_posts` | 189 | 739 |
| `source/substack_posts/` | `substack` | `substack_posts` | 134 | 748 |

**Corpus naming settled** (Justin, 2026-08-06): every corpus takes its directory
name, so Facebook's was changed from `facebook` to `facebook_posts` before the
first digest. That was the whole reason to settle it then — the name is part of
every document ID and names the files in `digested/`, so it is free to change
before a digest and means re-digesting from scratch afterwards.

**Ollama** is running with the embedding model pulled (`nomic-embed-text`, 768
dimensions, what `embed.DefaultModel` names). `digest` has no lexical mode —
`-mode=lexical` is a `search` flag — so a corpus cannot be built without it.

**CI stopped triggering on 2026-08-06, and the cause was a GitHub Actions
outage.** Not the workflow file, and not this repo's configuration. Three pushes
in a row produced no run at all: `2b5f4ed` to a PR branch at 18:21Z, the merge
commit `e7eea6a` to `main` at 18:46Z, and `06f10da` to a fresh branch with a
fresh PR at 19:40Z. GitHub's status page showed **Actions in `major_outage`,
incident opened 15:22:49Z**, and every miss falls inside that window while the
last green run — `a703daf` at 14:02Z — falls before it. The repo was checked and
is healthy: `actions/permissions` reports `enabled: true` and
`allowed_actions: all`, and both workflows report `state: active`.

**The outage is over** — GitHub reported Actions `operational` with no
unresolved incidents later the same evening. **The missed runs did not replay.**
Nothing was queued and flushed; those three triggers are simply gone, which is
why `workflow_dispatch` earns its place.

**Check <https://www.githubstatus.com> first when CI does not fire.** It is the
cheapest diagnostic there is, and skipping it cost an afternoon of suspecting
the workflow file, the triggers and the repo settings in turn — all of which
were fine. Only escalate to repo configuration once the status page is clean.

**The consequence outlived the outage for three weeks, and is now closed.**
`2b5f4ed` added 963 lines of Go — `internal/parse/htmlsite.go`,
`internal/parse/substack.go` and their tests, plus smaller changes in
`parse.go`, `main.go` and `corpus/starter.go` — and for three weeks CI had seen
none of it. The race detector needs cgo and does not run on a stock Windows box,
so CI is the only place it executes.

**Run `33121447668` on 2026-08-27 cleared it**, and by a route worth recording,
because it was not the manual trigger this file had been telling people to
reach for. The PR that corrected these very notes touches only `TODO.md` and
`ci.yml` — **no Go file differs between that branch and `main`** — so the
`pull_request` run exercised `main`'s parser code exactly as it stands. All five
steps ran and passed, race detector included, `internal/parse` among them.

Two things fell out of that. The `pull_request` trigger fires again, so the
outage was the entire cause and nothing in the repo needed changing. And a
docs-only PR off an untested `main` is a way to get `main`'s code through CI
without inventing a dummy commit — cheaper than `workflow_dispatch` when a real
change is waiting to go out anyway.

`ci.yml` previously had **no `workflow_dispatch`**, unlike `build_release.yml`,
so a missed run left no way to kick CI short of pushing another commit. Added in
`06f10da`. The outage explains why the runs went missing; `workflow_dispatch` is
what lets the skipped checks be run afterwards without inventing a dummy commit,
which is exactly the recovery case.

---

## Immediate next steps

1. ~~**Dedupe, as a report.**~~ **Done, 2026-08-27.** `coroner dupes` ships with
   `-format=json`, `-window` and `-threshold`. On the real corpus it finds **177
   groups covering 378 documents**. Mechanism and the measurements behind it are
   in CLAUDE.md; this file keeps only what is still open.

   Priority is live: the three `source/*/corpus.yaml` files carry 30/20/10 and
   the corpus was re-digested to copy them into the digested manifest. That
   re-digest reused every one of the 7,977 chunks and took **1.1s against the
   original 2m32s**, which is the incremental design doing exactly what it was
   built for — worth knowing before anyone hesitates to re-digest for a metadata
   change.

   **The pass cross-checks against the standalone measurement exactly**: 23
   three-way chains, 2 `substack`+`facebook`, 1 `substack`+`html`, 26 Substack
   documents and 175 HTML documents involved — all matching numbers derived
   independently in Python before the Go was written. Winners land 151
   `html_posts` and 26 `substack_posts`, `facebook_posts` never, and no group
   contains a member outranking its own winner.
2. **The viewer** — see "A front end for the corpus" below. This is the next
   real piece of work.
3. **Show the document ID in human search output** — small, and argued below to
   matter more than its size. Worth doing alongside the viewer, since both are
   about referring to a piece of writing by one stable handle.
4. **Revisit embeddings** when there is something to judge them against, which
   means an evaluation set only Justin can supply.

The CI item that used to head this list is done — see above. If CI ever goes
quiet again, check <https://www.githubstatus.com> before suspecting the repo,
then `gh workflow run ci.yml --ref main`.

Deliberately not next: corpus balance, and any change to how search ranks. See
the decisions below.

---

## Revisit embeddings

The model and its dimensions were chosen before there was a corpus to judge them
against, and the milestone was the thing that had to land first. Changing the
model invalidates every vector, so this is something to do *to* a working corpus
rather than on the way to building one. That condition is now met.

**The most likely reason to change models has been retired.** Measured across
all 7,977 chunks: the largest is 2,000 chars — `doc.MaxChars`, as designed —
which is roughly 500 tokens against `nomic-embed-text`'s 2,048 context. Nothing
is being truncated at embed time, so no chunk is silently losing its tail.

What is left is an open evaluation with no evaluation set behind it, which is the
same gap that makes `-depth` untunable. Building one means writing down a set of
queries and what should come back for each — and only Justin can supply that,
because it is his writing and he is the one who knows which post he meant. Until
that exists, "is the model good enough" has no answer that is not a vibe.

One measured caveat for whoever does this: mean chunk length is **433 chars**,
far below the 1,200 target, because Facebook status updates dominate. Short-text
embedding is a different regime from paragraph embedding, so an evaluation drawn
only from Substack essays would not describe how the corpus actually behaves.

---

## After the milestone

### Cross-corpus dedupe, with a precedence order

Justin's rule, settled 2026-08-06 and now complete:

**`substack` > `htmlsite` > `facebook`.**

Substack wins over everything. The HTML site wins over Facebook because those
posts were copied out of Facebook *by hand*, so the HTML is the curated version —
correctly encoded, given a real title, given a deliberate date — and the Facebook
record is the raw original. The encoding measurement supports this independently:
the HTML export is clean UTF-8 throughout, while the Facebook export still
carries the double-encoding `repairMojibake` has to undo.

**The hard part, and why this was deferred rather than bundled into the
milestone: hand-copying means `ContentHash` will not match.** That was the
prediction. It is now measured against the real digested corpus, and it was
right by a wide margin:

**It is a three-way problem, not a two-way one.** The first measurement only
compared `html_posts` against `facebook_posts`, which was too narrow — a search
for "electoral college" returned the same piece from `html_posts` and
`substack_posts` on the same date, which the Facebook-only comparison could not
have seen. All three pairs, ≥0.5 Jaccard within ±3 days:

| from | against | overlapping | share |
| --- | --- | --- | --- |
| `html_posts` (189) | `facebook_posts` | 175 | 93% |
| `substack_posts` (134) | `facebook_posts` | 25 | 19% |
| `substack_posts` (134) | `html_posts` | 24 | 18% |

Read that as: the HTML site is almost entirely a curated copy of Facebook, while
Substack is mostly new writing that revisits older material about a fifth of the
time. This is why the precedence order needs all three tiers rather than just
the two the original framing implied — a Substack post can displace an HTML post
that is itself displacing a Facebook post, and the pass has to resolve the chain
rather than a pair.

The detailed measurements below are the `html_posts` → `facebook_posts` pair,
which was measured first and most closely. **The other two pairs have now been
broken down the same way — see "The two Substack pairs" below.** The headline
conclusion is that the method transfers unchanged, with one correction to make
before the pass is written.

- **171 of the 189 `html_posts` share a date with at least one Facebook post**
  (176 distinct dates in `html_posts`, 171 of them present in `facebook_posts`).
  So the overlap is close to total, as expected from posts copied out of
  Facebook by hand.
- **Exactly 3 documents match by `ContentHash`.** A hash-equality dedupe would
  catch 3 of roughly 175 real duplicates — under 2%. This confirms the reason
  for deferring rather than reusing the anticipated pass.
- **5-word-shingle Jaccard, against Facebook posts within ±2 days, separates
  them cleanly**: 169 of 189 score **≥0.8**, 6 score 0.5–0.8, **nothing at all
  lands between 0.2 and 0.5**, and 14 score 0.0. The empty middle is the useful
  part — there is no ambiguous band to agonise over a threshold in, so anything
  from about 0.5 to 0.8 draws the same line. Four of the top matches are exactly
  1.00 with identical word counts, which are copies that happen not to be
  byte-identical.
- **The 14 non-matches are genuinely HTML-only**, not detection failures. Two of
  them are titled `Small Government (NEVER POSTED)` and `Two Parents (NEVER
  POSTED)`, which is the author's own record of the fact.

#### The two Substack pairs — measured 2026-08-27

Measured against the same digested corpus, same method: 5-word-shingle Jaccard
against candidates within a date window. The `html_posts` → `facebook_posts`
pair was re-run first as a control and reproduced exactly — 175 overlapping at
93%, 169 at ≥0.8, 3 by `ContentHash`, empty 0.2–0.5 band — so the numbers below
come from the same measurement, not a different one that happens to agree.

| from | against | ≥0.8 | 0.5–0.8 | 0.2–0.5 | <0.2 | ≥0.5 total |
| --- | --- | --- | --- | --- | --- | --- |
| `substack_posts` (134) | `facebook_posts` | 15 | 10 | **0** | 109 | 25 (19%) |
| `substack_posts` (134) | `html_posts` | 14 | 10 | **0** | 110 | 24 (18%) |
| `html_posts` (189) | `facebook_posts` | 169 | 6 | **0** | 14 | 175 (93%) |

Four things follow, and they settle most of what was open.

**The empty middle is a property of the data, not of hand-copying.** That was
the open worry: the 0.2–0.5 gap might have been an artefact of the HTML site
being copied out of Facebook by hand, in which case Substack — genuinely
rewritten — would smear across the middle and force a threshold argument. It
does not. **All three pairs have exactly zero documents scoring 0.2–0.5.**
Anything from about 0.5 to 0.8 draws the same line on every pair, so the
threshold needs no tuning and no evaluation set.

**The date window is not sensitive.** ±2 and ±3 days produce byte-identical
results on all three pairs. Keep ±2; it is cheaper and nothing is lost.

**It is genuinely a chain, and the chain is the common case.** Of the 26
Substack documents matching anything, **23 match in both other corpora**, 1 only
in `html_posts`, 2 only in `facebook_posts`. So the pass cannot resolve pairs
and union the results — the dominant shape is one piece of writing present three
times, and precedence has to pick one winner from a group of three. This is what
the earlier two-way framing would have got wrong.

**`ContentHash` is useless across every pair, not just the measured one**: 0 for
`substack`→`facebook`, 0 for `substack`→`html`, 3 for `html`→`facebook`. The
deferral was right for all three.

#### One correction to make before writing the pass: fold quote characters

Every one of the 10 Substack documents in the 0.5–0.8 band was read. **All ten
are the same post, not partial reuse** — there is no ambiguous class here to
design around. The depressed score has three identifiable causes, and one of
them is a defect in the comparison rather than a fact about the writing.

The corpora disagree about apostrophes, and the disagreement is near-total:

| corpus | `U+0027` straight | `U+2019` curly |
| --- | --- | --- |
| `substack_posts` | 643 | **2,831** |
| `html_posts` | 4,377 | 47 |
| `facebook_posts` | 10,770 | 1,367 |

Substack is 81% typographic; the HTML site is 99% straight. Since the tokeniser
treats `'` as a word character, every contraction — and English prose is full of
them — produces a different token on each side, and a 5-word shingle spanning
one contraction is destroyed. This is why the Substack pairs top out at **0.99
with not a single exact 1.00**, while `html_posts` → `facebook_posts` has 86.

Folding `U+2019/2018/201C/201D/2014/2013/00A0` to their ASCII equivalents before
shingling was measured:

| pair | ≥0.5 | ≥0.8 | ==1.00 |
| --- | --- | --- | --- |
| `substack`→`facebook` | 25 → 25 | 15 → **22** | 0 → 0 |
| `substack`→`html` | 24 → 24 | 14 → **21** | 0 → 0 |
| `html`→`facebook` | 175 → 175 | 169 → 170 | 86 → **88** |

Read that carefully, because it decides how much this matters: **folding does
not change which documents are detected** — the ≥0.5 sets are identical — it
changes the score they are reported with. So the pass would work without it. But
the report exists to be read and trusted, and a real duplicate labelled 0.63
because of an apostrophe convention invites exactly the "is this threshold
right?" second-guessing the empty middle otherwise makes unnecessary. Fold, and
say in the output that scores are computed on folded text.

Note this is a **comparison-time** fold only. It must not touch `doc.Normalise`
or anything upstream of a document ID — the corpora legitimately differ here,
and rewriting the stored text would change every Substack ID and force a
re-digest to fix a reporting nicety.

The other two causes are real differences and should be left alone: Substack
prepends its subtitle and often a date line to the body (both deliberate, both
recorded in CLAUDE.md), and the writing is genuinely edited between versions —
one pair pseudonymises a name that appears in clear in the Facebook original.
Nothing scoring ≥0.5 was a false positive, so no exclusion rule is needed.

One incidental find worth keeping: `Post Debate Disaster: Biden v. Trump` opens
with the author's own note, "This was written elsewhere and migrated here."
Where such a marker exists it corroborates the detection, but only 1 of 26 has
one, so it is not a signal to build on.

Remaining open questions:

- **What signal?** **Settled.** Date proximity within ±2 days plus 5-shingle
  Jaccard on quote-folded text, threshold anywhere in 0.5–0.8. Now measured on
  all three pairs rather than one: the empty 0.2–0.5 band holds throughout, and
  ±2 and ±3 give identical results, so neither the threshold nor the window
  needs tuning and no embeddings are involved. Embedding cosine was the other
  candidate and would have meant a judgement call about a threshold; shingling
  turned out not to.
- **Does the result change what search returns, or is it a separate report?**
  **Settled (Justin, 2026-08-06): a report, and nothing else.** Search behaviour
  does not change — no filtering, no suppression, not even behind a flag until
  there is a reason to want one. Silently changing ranking is the class of quiet
  wrongness the rest of the design exists to avoid, and a dedupe pass that only
  ever prints is one you can be wrong about harmlessly. The precedence order
  still matters: it decides which member of a group the report names as the
  winner.

  **Flagged, not reopened:** this sits awkwardly against the stated goal at the
  top of this file. A report tells Justin where the duplicates are; it does not
  give him the single corpus that would let him stop tracking which export a
  piece lives in. Both can be true — report first, act later, once the groups
  have been read and trusted — and report-first is the right first step either
  way. But if the report is ever treated as the finished feature, the goal has
  not been met. Decide deliberately rather than by drift.
- **Where does precedence live?** **Settled (Justin, 2026-08-27): a `priority`
  field in `corpus.yaml`**, carrying `substack` > `htmlsite` > `facebook`. It
  generalises without hardcoding three corpus names and keeps the rule beside
  the data it describes. Lower stakes than it looks while the pass only reports
  — a wrong priority produces a misleading line, not a hidden document — so it
  does not need to be perfect before it is useful. Add it to the manifest
  starter and `corpus.example.yaml`, remembering that
  `TestExampleManifestMatchesGenerator` will fail until both move together.

**Built as described, 2026-08-27.** `internal/dupes` plus a `coroner dupes`
subcommand; mechanism and measurements now live in CLAUDE.md. What the real
corpus gives: **177 groups covering 378 documents**. One thing worth knowing
that only showed up on the real data — the quote-folding correction described
above turned out to matter for grouping as well as for scoring, and the pass
unions matches transitively rather than resolving pairs, because the three-way
chain is the common case.

### A front end for the corpus

**Decided 2026-08-27.** Justin's framing: a website that shows the breadth of
his writing by date, with snippets, serving both the combined-corpus view and
the dupes report. That instinct is right, and it is a better answer to the goal
at the top of this file than a terminal report is — the report tells you where
the duplicates are and still leaves you holding the question. A duplicate group
is an annotation on a date-ordered list, not a separate kind of thing, so one
data file serves both views.

**What the dupes report does not cover, and why that shapes the viewer.**
Measured 2026-08-27, and worth stating because "`facebook_posts` never wins a
group" is easy to misread as a claim about the whole corpus:

| corpus | total | in a group | alone |
| --- | --- | --- | --- |
| `facebook_posts` | 5,651 | 176 | **5,475 (97%)** |
| `html_posts` | 189 | 176 | 13 (7%) |
| `substack_posts` | 134 | 26 | 108 (81%) |

Facebook loses every group it is in, which is correct — where a post exists in
more than one corpus, Facebook is by construction the raw original. But that is
a statement about 176 documents. **97% of Facebook exists nowhere else and never
appears in the report at all.**

That remainder is not all status updates: **576 of it is 100+ words, 200 is
200+, and 59 is 400+**, up to a 1,209-word maximum. Those are substantial pieces
that never reached the site or Substack. The `html_posts` row is the mirror
image and confirms the curation story — only 13 of 189 HTML posts are not also
on Facebook, so the site really is a hand-picked selection.

**So the viewer should not be scoped as "dupes with a UI".** Dedupe answers
"which copy is real" for about 3% of the corpus. The larger and more interesting
thing to surface is the writing that exists in exactly one place and has been
lost track of — which is the scattering problem at the top of this file. A
duplicate group is an annotation on that view, not its organising principle.

**Three steps, each shippable, in this order:**

1. ~~`coroner dupes`~~ — done. Getting the pass right first means the front end
   is built against a real contract rather than one invented alongside it.
2. **`coroner export -o site.json`** — one file: documents, dates, titles,
   snippets, and duplicate groups with their precedence winner. **This is the
   seam, and the point of it is that the front end never reads `digested/`
   directly.**
3. **The SPA**, in `web/`, reading that file. End state is `coroner serve`:
   the Go binary serves it on localhost with the built assets baked in via
   `go:embed`, so the single-binary property survives and the corpus never
   leaves the machine.

**Two objections raised and answered, recorded so they are not rediscovered:**

- **Privacy is the one that matters.** `source/` and `digested/` are gitignored
  because the methods are public and the writing is not. A Vite build wants to
  bundle its data and the natural thing to do with a `dist/` is commit or deploy
  it — that is one careless `git add -f` from publishing the whole corpus. The
  design has to make that *structurally* hard, not discourage it in a comment.
  Same reason nothing here goes to a hosted preview service.
- **The single-binary contract.** Adding node, a lockfile and a second CI
  toolchain is a real departure from "one Go binary, four dependencies". Judged
  worth it, but as a decision rather than a side effect.

**Payload is not a constraint**, measured: snippets for the whole corpus are
**1.3 MB**, full text **5.3 MB**. So React is a choice about interaction comfort
— a virtualised list over ~6,000 items, date filtering — and not about scale.
Anyone revisiting this should know a plain page would also have worked.

**Cost to name for step 3:** `go:embed` needs the built assets present at
compile time, so either `web/dist` is committed or CI builds it. CI builds it.
Also: `internal/embed` already means ollama embeddings here, so the asset
package needs a different name.

### Corpus balance — observed, deliberately not acted on

**Facebook is 95% of the corpus by document count**: 5,651 of 5,974, with a
median of 20 words. The curated writing — `substack_posts` and `html_posts`, 323
documents between them — is outnumbered roughly 18:1. Measured on three test
queries after the first digest, Facebook took 4, 9 and 8 of the top 12.

Nothing is broken. The ranking is not favouring Facebook; there is simply far
more of it, and a short status update that genuinely matches a query deserves to
rank. But the writing most likely to be worth finding is the smallest part of
the corpus, and that is a property worth having written down.

**Decision (Justin, 2026-08-06): leave it and use the tool first.** Whether
Facebook crowding the results is actually annoying is something you find out by
searching, not by reasoning about ratios, and every available fix — a corpus
filter, a boost, a per-corpus quota — is a ranking judgement made before there
is evidence for it. Revisit after real use.

Note that **dedupe will not move this number**: it concerns roughly 175
documents, under 3% of the corpus, and under the report-only decision it removes
none of them.

If it does turn out to need addressing, a `-source` filter on `search` — the way
`stats -source` already works — is the option that involves no ranking judgement
at all, and so the one to reach for first.

### Search: show the document ID in human output

`search -format=json` carries `id` on every result; the human-readable output
does not. So a document can be found and read but not cited without re-running
the query as JSON, which makes the 16-character ID much less useful than it was
meant to be — it is short and greppable precisely so it can be written down.

Small change, and it matters more than its size given the goal at the top of
this file: referring to a piece of writing by one stable handle, rather than by
which export it happens to live in, is most of what "a single corpus" means in
practice.

Worth deciding whether it always shows or hides behind `-verbose`. Always is
probably right — the ID is 16 characters on a line that already carries a corpus
name, a date and a score.

### Search: filter by date range

Most of what "what was I writing about in 2019" needs. Every document already
carries a date, so this is a filter over loaded documents rather than anything
structural. Worth more once three corpora span 2016–2026.

### Search: `-explain`

Show which query terms drove a lexical hit. Diagnostic for the half of retrieval
that is legible — there is no equivalent for the vector half, and pretending
otherwise would be worse than omitting it.

---

## For Justin: errors in the HTML site source files

Found by cross-checking each page's filename date against its in-body `<h2>`
across all 189 pages. They agree on 169 and **disagree on 14, with the `<h2>`
wrong every time.** Coroner takes the filename unconditionally and ignores the
`<h2>`, so nothing is broken either way — these are listed only because they are
mistakes in the writing that Justin may want to correct at the source.

| page | filename says | `<h2>` says |
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

Two smaller things in the same export, both handled and neither needing a fix:
**five pages have the literal `<h1>title</h1>`** left over from a template, and
**six filenames end in ` copy`** — none of which has a surviving twin, so they are
ordinary posts rather than duplicates.

---

## Decisions, and why

Recorded so they are not silently reversed by someone who only sees the code.

- **`htmlsite` is a separate format from `html`, not a configuration of it.**
  188 of 189 pages carry the same site banner in `<title>`, and titles are
  prepended to every chunk before embedding. Reusing the generic parser would put
  one identical string into every vector in the corpus.
- **Filename dates beat in-body dates for `htmlsite`**, unconditionally — see the
  table above.
- **Substack drafts are skipped** (Justin, 2026-08-06): only `is_published: true`
  is corpus, leaving 134 documents from 139 files. The five drafts are exactly
  the five posts with no title and no date; two read as finished essays and three
  are scaffolding, and nothing but reading them separates the two. Taking the
  published flag at its word is the non-guessing option — it is the author's own
  record of what counts as finished. A heuristic keyed on placeholder text would
  be fragile in exactly the way this codebase avoids.
- **Substack subtitles live in the text, not the title.** They are real writing
  that exists nowhere else (117 of 139 posts, only one already appearing in its
  own body), but a title is repeated across every chunk and a summary sentence
  repeated through a long post is the Facebook `title` shape again.
- **`Include()` for Substack is a narrow allowlist and must stay one.** `posts/`
  holds 218 analytics CSVs carrying subscriber email addresses. Naming what to
  read rather than what to skip means a later export adding another analytics
  file cannot quietly start indexing them. This is a privacy constraint, not
  tidiness, and there is a test asserting it.
- **Every parser is written against a real export, never a documented format.**
  Facebook arrived double-encoded in a way nothing documented would have
  predicted, and encoding errors silently corrupt every document ID in a corpus.
  If a format is named before its bytes exist, register it with `pendingParser`
  and wait.

---

## Housekeeping

Small, low-risk, none urgent.

- **CLAUDE.md omits `internal/ignore`** from the architecture walkthrough. It is
  a real package with tests, carried from hecato, and it governs what the walk
  never sees — which makes it load-bearing for the Substack allowlist decision.
- **CLAUDE.md's Commands block omits `coroner links`**, though the subcommand
  ships and has its own architecture section further down.
- **`internal/version` has no tests**, which CLAUDE.md states deliberately. Noted
  only so it does not get "fixed" by someone reading a coverage report.
- **CLAUDE.md still says the dedupe pass compares `ContentHash`.** Measurement
  now says that catches 3 of ~175 real duplicates. Worth correcting when the
  pass is actually written, not before — CLAUDE.md describes what is, and right
  now what is, is nothing.

---

## Done

- **Milestone: one searchable corpus** (2026-08-06). All three sources digest
  cleanly and `coroner search` spans them. No architecture change was needed —
  `digested/` already held one set of files per corpus and search already loaded
  all of them, so "one corpus" was a property of the store as it stood.

  What was checked, and what it showed. `coroner stats` per corpus, read for the
  three failure shapes it exists to expose — a tight word-count spread meaning
  text truncated at the first newline, a gap in the date histogram meaning a
  dropped year, a low hapax share meaning boilerplate got in. None of them
  appeared:

  | corpus | span | words/doc (min/median/max) | vocabulary | hapax |
  | --- | --- | --- | --- | --- |
  | `facebook_posts` | 2009-08-04 – 2025-12-24 | 1 / 20 / 2,162 | 22,077 | 49% |
  | `html_posts` | 2016-10-25 – 2026-01-01 | 53 / 551 / 2,329 | 9,268 | 45% |
  | `substack_posts` | 2018-11-18 – 2026-08-03 | 239 / 812 / 3,022 | 9,388 | 44% |
  | all | 2009-08-04 – 2026-08-03 | 1 / 21 / 3,022 | 23,869 | 41% |

  Facebook's median of 20 words is a status update, not a truncation — its
  spread runs to 2,162 and its histogram has no missing year. `coroner stats
  -sample=6` on each of the two smaller corpora read as real writing throughout,
  which is what the evenly-spread sample is for: documents are sorted by hash,
  so the front of the file is a fixed arbitrary slice that would hide a parser
  failing on later records.

  One thing that looked wrong and was not: a Substack sample began
  `Capitalism Can Be a Bitch / Premium / I pay a yearly fee…`, which reads like
  paywall chrome. It is the post's subtitle followed by its first section
  heading — the title is `Consumption Tiers` and lives in `Title`, exactly the
  shape the subtitle decision below describes. `Premium` occurs 4 times in the
  whole corpus and leads no document. Recorded because the next person to read a
  sample will have the same suspicion.

  Search was run across several queries and returned hits from all three corpora
  every time.
- **Corpus naming settled** (Justin, 2026-08-06). Every corpus takes its
  directory name; `facebook` became `facebook_posts` before the first digest.
- **`substack` parser** (2026-08-06). `internal/parse/substack.go` plus tests,
  replacing `pendingParser` in the registry. Validated on the real export: 139
  files in, 134 documents out, no duplicate IDs, every document carrying both a
  title and a date, words 239–3022 with a median of 812.
- **`htmlsite` parser** (2026-08-06). `internal/parse/htmlsite.go` plus tests,
  registered in `parse.constructors` and listed in the manifest starter;
  `corpus.example.yaml` regenerated to match, which
  `TestExampleManifestMatchesGenerator` requires. Validated on all 189 real
  pages: 189 documents, none empty, none missing a date or title, no duplicate
  IDs, words 53–2329 with a median of 551.
- **TODO.md established** (2026-08-06) as the canonical home for planned work.
  The `cmd/coroner/main.go` header comment and CLAUDE.md's Roadmap section both
  point here instead of carrying their own lists.
- **Branch consolidation** (2026-08-06). `updates` was fully contained in
  `updates-1`; PR #2 closed as superseded by #3, branch deleted locally and on
  the remote. One feature branch.
