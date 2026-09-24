# Decision log

Short records of what was decided, what was rejected, and why. Add to the bottom.

---

## D1 — Get reference data from OpenAlex, do not parse PDFs

**Decided:** 29 Aug 2026

OpenAlex returns already-resolved reference lists (`referenced_works`) as work IDs. Free, no API
key, 100k credits/day, 100 req/s. Send `mailto=` to join the polite pool.

**Rejected:** parsing reference lists out of PDFs with GROBID. Reference extraction has real,
documented error rates, and those errors compound across a graph. GROBID stays as a later
fallback for works with no DOI and no index entry.

---

## D2 — Deduplicate on the OpenAlex work ID

**Decided:** 29 Aug 2026

The same paper exists as an arXiv preprint, a conference paper and a journal article, with
different DOIs and drifting titles. OpenAlex already merges most of these. Using its ID as the
primary key borrows that entity resolution instead of rebuilding it.

**Fallback order** for works OpenAlex does not know: DOI, then normalised title + year + first
author surname.

---

## D3 — SQLite for everything, not Neo4j

**Decided:** 29 Aug 2026

The constraints "free, open source, one-command setup, local first" rule out anything needing a
server process before the user sees a result. SQLite gives graph edges, FTS5 keyword search and
`sqlite-vec` vectors in one file with no server.

**Rejected:** Neo4j. Good database, wrong for v1 — it needs a server running before anything
works, which breaks setup for exactly the non-technical researchers we want. Community Edition is
GPLv3. Revisit as an optional backend for shared-lab server mode.

**Consequence:** all storage sits behind one interface so Postgres + pgvector or Neo4j can be
swapped in later without rewriting the app.

---

## D4 — PDFs on the filesystem, not in a database or the git repo

**Decided:** 29 Aug 2026

Files are named by their SHA-256; the database stores the hash, source URL, OA status and licence.
Free deduplication, easy backup, easy to serve.

**Rejected:** a document database. Document databases store JSON records, not binary files.
MongoDB also ships under SSPL, which the OSI has not approved as an open-source licence.

**Rejected:** committing PDFs to git. Binary blobs make the repo unusable within months.

---

## D5 — Open-access full text only

**Decided:** 29 Aug 2026

Fetch through OpenAlex OA locations and Unpaywall. Never scrape publisher sites; never use a
user's institutional credentials. This is a permanent constraint on the product, not a v1 limit.

**Consequence:** a large share of papers will have metadata but no full text. The interface must
show OA status everywhere so this never surprises anyone.

---

## D6 — Name: Filiation

**Decided:** 29 Aug 2026

In textual criticism, *filiation* is the work of establishing which manuscript descended from
which. It names the process rather than the field, which makes it more specific than the
alternatives, and it is exactly what this tool does with papers.

Package `filiation`, CLI command `fil`. Confirmed unclaimed on PyPI, and no software product
uses the name.

**Superseded: Stemmatics.** Chosen first, then dropped — stemmatics.io is a live research data
provenance platform in early access. That is adjacent to this project's space, so the clash was
substantive rather than merely phonetic.

**Also considered:** Catenary (free on PyPI, sounds more like a product, less literal), Hypotext
(free, exact meaning, but one letter from "hypertext"), Collation (free, perfect meaning, but
collides with the SQL term in a project built on SQLite), Wellhead (free, strong oil and gas
association), Headwater (free on PyPI but headwatertech.io exists).

**Note on spelling:** the word has one L. `fillation` is also unclaimed on PyPI if the doubled
spelling is ever preferred, but it loses the meaning that made the name worth having.

**Still to check:** npm, and the `.dev` / `.org` domain.

---

## D7 — Phase 1 architecture

**Decided:** 29 Aug 2026

Six ADRs covering the core graph: one SQL module without an ORM, budgeted best-first
expansion with batched hydration, stub nodes recorded before their targets are fetched,
a polite-pool HTTP layer with a token bucket and response cache, no silent acceptance of
title search results, and one library per user rather than per directory.

Full reasoning, cost model, failure modes, test strategy and week-by-week plan:
[`ARCHITECTURE_PHASE1.md`](ARCHITECTURE_PHASE1.md).

**Consequence:** `schema.sql` changed — `work.title` is now nullable and the table gained
`hydrated`, `unresolved`, `depth` and `is_seed`, plus a partial index for frontier selection.

**Unverified:** the cost model assumes batch-fetching works by OpenAlex ID with 50 IDs per
request. The documentation contradicts itself on this, on `per_page`, and on the real rate
limit. Four spikes in week 1 must confirm them before the design is trusted.

---

## D8 — Language: Go, not Python

**Decided:** 29 Aug 2026

Written in Go. The deciding constraint is distribution: "one command setup for researchers who do
not code" is better served by a cross-compiled binary the user double-clicks than by
`pip install`. Secondary reason, stated openly: the maintainer's existing projects are all Python
and this is a deliberate stack expansion.

**What Go gives us**

- One static binary per platform. No runtime for the user to install
- `ncruces/go-sqlite3` (WASM, no cgo) plus official sqlite-vec Go bindings — graph, keyword and
  vector search all in one file, still statically linked
- An official MCP Go SDK, maintained with Google
- `//go:embed` puts the M5 web UI inside the binary — Phase 5 shrinks by about a week
- `golang.org/x/time/rate` is exactly the token bucket ADR-004 needs

**What it costs, honestly**

- **PDF text extraction is markedly weaker than Python or Java.** Deferred to a Phase 3
  bake-off — see ADR-009
- **Embeddings move behind Ollama** (ADR-008), so semantic search needs a second install. This
  partly undercuts the one-command promise that motivated the choice
- **`quelle` is unusable.** That Python package covered much of M0 ingestion and M3 PDF
  fetching. The layer is now ours to write — roughly a week added to Phase 3
- **Learning while building.** Phase 1 carries two extra weeks for this

**Rejected:** Python (better libraries, worse distribution, and no stack growth for the
maintainer). Java (excellent PDFBox, but needs a JVM or a fiddly GraalVM build, and the verbosity
costs velocity). Rust (best binary story, worst solo velocity at ten hours a week).

**Consequence:** the architecture is unchanged in substance. Stub nodes, the batching cost model,
the three-front-doors rule, the degradation ladder and the schema all carry over. Only package
names and two adapter decisions changed.

---

## D9 — No cgo, anywhere

**Decided:** 29 Aug 2026

The single static binary is the entire reason for D8. A dependency requiring cgo breaks
cross-compilation and drags in a C toolchain, which loses the benefit that justified the language
choice.

**This is a standing constraint on every future dependency**, not a one-time choice. If a library
needs cgo, find another way or do without.

Detail in ADR-007.

---

## D10 — Ollama for embeddings and generation

**Decided:** 29 Aug 2026

Both `internal/embed` and `internal/llm` are thin HTTP clients for Ollama, each behind a
one-method interface.

This is the direct cost of D8 and it changes the product promise: semantic search and answers now
need Ollama installed. Acceptable only because the degradation ladder already covers the absence —
without Ollama, keyword search and the entire graph still work.

**The first-run experience must handle this explicitly:** detect Ollama, and when it is missing,
say what works without it and what installing it would add. Failing confusingly here would undo
the distribution advantage that motivated the whole decision.

**Revisit when:** users report the Ollama install as the reason they stopped. A pure-Go ONNX path
(`onnx-gomlx`) would restore true one-command setup at the cost of depending on a
community-maintained inference stack.

Detail in ADR-008.

---

## D11 — The OpenAlex cost model is measured, not documented

**Decided:** 30 Aug 2026

Spikes 1–3 were run against the live API before writing the client. Every headline number in the
published documentation — and therefore in D1, ADR-002 and ADR-004 — was wrong.

| | Documented / assumed | Measured |
| --- | --- | --- |
| Daily allowance | 100,000 credits | **1,000 credits** ($0.10 equivalent) |
| Single work by ID | 1 credit | **0 — free** |
| List request | 10 credits, up to 50 works | **1 credit, up to 200 works** |
| IDs per filter | 50 or 100, sources disagree | **exactly 100; 101 is a hard 400** |
| Sustained rate | 10/s or 100/s, sources disagree | **first 429 at ~11/s** |
| Polite pool via `mailto` | "far faster" | **no measurable difference** |

**Decision.** Trust `docs/SPIKES.md` over OpenAlex's documentation, and re-measure rather than
re-read when something looks off. The client is built to the measured numbers: batches of 100, a
5 req/s token bucket, `mailto` sent as courtesy rather than optimisation.

**What survives.** Batching, and the whole of ADR-002. What changes is *why*: batching was
justified as 5× cheaper per work, and single fetches turn out to be free. The real reason is round
trips — at ~11 req/s, 500 single fetches is ~50 seconds and 500 chances to be throttled, against 5
requests batched.

**What gets worse.** Measured branching is 70–100 references per work in STEM, not the ~40
assumed, so depth 3 is now ~350,000 works and would exhaust a day's allowance. The node budget
moves from "good practice" to "the only thing keeping expansion inside the free tier."

**Consequence:** every one of these numbers is a moving target — OpenAlex has clearly introduced
USD-denominated credits since the documentation was written. The client must read
`X-RateLimit-Remaining` from responses and surface it, rather than trusting any constant compiled
into the binary.

---

## D12 — Show reference coverage per node, from day one

**Decided:** 30 Aug 2026

Spike 4 measured what fraction of works on the expansion frontier can themselves be expanded:

| Field | Dead ends |
| --- | --- |
| Medicine | 6% |
| Physics and Astronomy | 12% |
| Computer Science | 15% |
| Social Sciences | 54% |
| **Arts and Humanities** | **84%** |

**The tool works in STEM and does not work in the humanities.** Books and chapters dominate
citation practice there, and those publishers largely do not deposit reference lists with
Crossref. A humanities researcher's first expansion returns a star of unexpandable stubs.

**Decision.** Treat reference coverage as a first-class, always-visible property of a node,
exactly like OA status under D5. `fil add` and `fil expand` report how many works in the result
have no reference list, and the M5 graph view marks them.

**Rejected:** hiding it, or filtering unexpandable nodes out of the display. The stub is still
true — the paper really was cited — and a silently thin graph is indistinguishable from a broken
tool.

**Rejected for now:** falling back to reference parsing for humanities works, which would mean
breaking D1. Revisit only if humanities users turn up and stay, and price it as its own project.

**Consequence:** `SCOPE.md` lists "full-text coverage will disappoint people" as a high risk about
PDFs. It lands one layer earlier than expected, on the graph itself. Say so in the README before
someone discovers it by installing the tool.

---

## D13 — Vector search is `vec1`, and the ANN index is a problem we may never have

**Decided:** 30 Aug 2026 · supersedes the `sqlite-vec` line in the stack table

Spike 6 tested SQLite's own vector extension, `ext/vec1`, which `ncruces/go-sqlite3` already
ships as a loadable WASM module. It was tested first because it costs no new dependency and no
pre-1.0 risk.

**It is exactly correct** — top-10 identical to a brute-force check in Go, persistent across
reopen, coexists with FTS5 in one file. **And it has no ANN index:** version 0.7 accepts only
`none` or `flat`, and flat is a linear scan at ~66 µs/vector. That is ~3 seconds for a
50,000-chunk library, against ARCHITECTURE.md's 500 ms target.

**Decision.** Use `vec1`. Do not add `sqlite-vec` now.

**Why this is not the disaster it looks like.** M4 was always specified as hybrid retrieval: seed
by keyword and by meaning, expand along citation edges, rerank. If the vector stage runs over a
candidate set already narrowed by FTS5 and the graph, it sees a few thousand chunks, not the
library — about 100 ms, inside target. **Scanning a pre-filtered set is the design.** The missing
index only becomes critical if retrieval ever needs to embed-search the whole library at once,
which the architecture does not call for.

**Rejected for now:** `asg017/sqlite-vec`. Revisit only if a measured pre-filtered query misses
target, and check first whether it provides a genuine ANN index or merely a faster SIMD scan. If
it is the latter, switching buys a constant factor and costs the pre-1.0 dependency risk that
CLAUDE.md warns about — a bad trade made on a guess, a fine one made on a measurement.

**Consequence:** the M4 build order changes. Pre-filtering is no longer an optimisation to add
after the vector search works — it is load-bearing, and must exist before semantic search is
demonstrated on a real library. Edge weighting and candidate generation come first.

**Also settled:** keyword search is safe. FTS5 works via `AutoExtension` (spike 5), so M3 needs no
external index. Both extensions must be registered on **every** connection in `store.Open`.

---

## D14 — Title-level duplicates are reported, never merged automatically

**Decided:** 24 Sept 2026 · extends D2

D2 deduplicates on the OpenAlex work ID, and M1 folds two further kinds of duplicate on the way
in, because in both the evidence is conclusive: a **merged record** (OpenAlex redirects the old ID
to the survivor) and a **shared DOI** (OpenAlex holds one paper under two IDs with the same DOI —
seen live as `W2949614626` and `W4294576234`). Neither involves a judgement.

What is left after that is not conclusive. M1's first live expansion (`10.7717/peerj.4375`,
~500 works fetched) found **18–20 groups of fetched works sharing a title, about 4%**, under
different DOIs or none. Some are one paper: two 1998 records of "Free Internet access to
traditional journals"; a 2017 preprint and its 2018 journal version. Others are not:

- *The access principle* (2006) appears as an article **and a book review** — the review carries
  the book's title.
- *Why Most Published Research Findings Are False* appears as Ioannidis's 2005 paper in PLoS
  Medicine **and two different pieces in the magazine *Chance***, from 2005 and 2019.
- *Invisible Colleges* appears in 1973 and 1974 under different DOIs — most likely reviews of the
  book, not the book.

**Decision.** A shared title is evidence, not proof. fil **reports** works that share a normalised
title — `fil stats` counts them, `fil stats --duplicates` lists them with year, type and DOI — and
**merges nothing on a title match**. Titles shorter than 20 letters and digits once normalised
("Editorial", "Introduction", "Preface") are not reported: they are shared by thousands of
unrelated works and would bury the real cases.

**Why.** A wrong merge is worse than a duplicate. A duplicate is visible — two nodes, one list
entry to check — and splits a paper's citations in two. A wrong merge stitches two works into one
node, gives each the other's citations and references, and is invisible afterwards. It is the
same asymmetry ADR-005 answers for title search: a title never decides on its own.

**Rejected:**

- *Merge on title and year.* Simplest, and wrong for every case listed above — the book review,
  the *Chance* pieces and the reviews of *Invisible Colleges* all share a year with, or sit a year
  from, the work they are not.
- *Merge on title, year and a shared author, excluding reviews, errata and front matter.* The
  right shape for automation, but it needs author IDs, which the store does not yet persist
  (OpenAlex sends them; `authorship` is empty). **Revisit when authors are stored** — and even
  then, measure its precision on real libraries before letting it write.

**Relation to D2's fallback.** D2's "normalised title + year + first author surname" is for works
OpenAlex does not know at all — identifying a paper that has no ID. It is not a licence to merge
two records OpenAlex does know. D14 governs that case.

**Consequence for M1.** Its definition of done — "a clean 500-node graph with no duplicates" —
is read as: no duplicate IDs, no duplicate DOIs, no merged records left unfolded, and every
probable title duplicate reported to the user. That is what can be guaranteed without guessing.
