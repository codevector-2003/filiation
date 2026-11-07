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

**Amended 31 Oct 2026 by D24:** this holds for `fil`, which stays one cgo-free binary. The desktop
app shell, a separate module in `desktop/`, may use cgo for a native window.

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

---

## D15 — Licence: Apache-2.0

**Decided:** 24 Sept 2026 · closes the licence question in `CLAUDE.md`'s "Still open"

Filiation is licensed under the **Apache License 2.0**, with the copyright line in `NOTICE`.

**Why.** The people most likely to adopt a research tool are researchers inside universities and
companies, and their legal teams approve Apache-2.0 readily: it is permissive like MIT, and adds an
explicit patent licence from every contributor, which MIT lacks. It is also what `spf13/cobra` uses.
Every dependency is already permissive (MIT, MIT-0, BSD-3, Apache-2.0), so nothing constrained the
choice.

**Rejected:**

- *MIT.* Equally permissive and shorter, but without the patent grant — the difference that
  matters to institutional users.
- *AGPL-3.0.* The only option that stops a company hosting a modified fil as a closed, paid
  service. The cost is adoption: many companies and some universities will not use AGPL code at
  all. For a local-first tool whose first goal is to be used, that is the wrong trade today.
- *MPL-2.0.* A reasonable middle ground, but less familiar, and it does not stop hosted services
  either.

**Accepted consequence.** Someone may host fil as a paid service without sharing their changes.
If that ever becomes a real problem, the lever is the optional server mode, not the licence of the
core. Loosening a licence later is easy for a sole author; tightening one after outside
contributions is not — which is why the permissive choice is made deliberately, now.


---

## D16 — MCP expansion runs inside the call; the job system waits for M3

**Decided:** 25 Sept 2026 · amends `ARCHITECTURE.md` §5 ("anything over ~2 s returns a `JobHandle`")

`expand_graph`, the one M2 tool that takes more than a moment, **runs inside the tool call** rather
than returning a job handle. It is bounded to **200 works by default and 500 at most** per call,
reports MCP progress notifications after every committed batch, and honours cancellation. To go
further, the assistant calls it again: expansion already resumes from the persisted frontier, so
repeated calls are the continuation mechanism, and nothing new had to be built for it.

**Why.** §5's job table and background worker are real work — a table, a worker goroutine that owns
the write path, polling, and recovery on restart — and in M2 they would serve exactly one tool whose
worst case is measured: 500 works took ~21 s live, and 52 took 7 s in the MCP check. That is inside
any MCP client's timeout, and progress notifications keep the call visibly alive. M3's PDF fetching
is the first operation that genuinely runs for minutes; the job system is built there, for it, and
`expand_graph` moves onto it then.

**Rejected:**

- *Build the job system now.* Right design, wrong time: a week of M3-shaped work to serve one tool
  that does not need it yet.
- *Unbounded expansion inside the call.* A client timeout would cut the call off mid-run. Nothing
  would be lost — every batch commits — but the assistant would see a failure, not a result.

**Accepted consequence.** An assistant that wants 2,000 works makes four calls. The tool
description and every result's `note` say so.

---

## D17 — Every work has a fil ID; hard rule 6 is reworded

**Decided:** 27 Sept 2026 · amends D2 · detail in `ARCHITECTURE_PHASE3.md` ADR-010

Every work gets a **fil ID** — `F` and 8 characters of Crockford base32, at least one a digit
(`F7K2M9QXA`) — generated once on the user's machine and never changed. It is the primary key of
`work`. `openalex_id` stays, as a nullable `UNIQUE` column, and stays the deduplication key for
every work OpenAlex knows.

**Why.** Three things the OpenAlex ID cannot carry: a PDF the user owns that OpenAlex does not
know has no row to live in; folder names and notes tied to an OpenAlex ID break when OpenAlex
merges records; and the library should survive OpenAlex changing its terms again (D11).

**Hard rule 6, reworded:** *Every work has a fil ID, created once and never changed. No two works
may share an `openalex_id` — the database enforces it. Merged records and shared DOIs are folded
on the way in (`graph.hydrate`). A shared title is reported, never merged (D14).*

**Rejected:** a random UUID (36 characters — too long for folder names and for typing);
an ID derived from the OpenAlex ID (it would change when OpenAlex merges records, the case this
exists for); an auto-increment integer (not unique across two libraries, which lab sharing will
need).

**Accepted consequence.** The first migration rebuilds every table that referred to
`openalex_id`. Done at v0.2, while there are few libraries to upgrade.

---

## D18 — The library is a folder the user chooses, and its PDFs have readable names

**Decided:** 27 Sept 2026 · amends ADR-006 and D4 · detail in `ARCHITECTURE_PHASE3.md`
ADR-011 and ADR-012

A library is a folder, `Documents/Filiation/<name>` by default, holding `filiation.db`,
`filiation.toml`, `papers/`, `Inbox/` and `exports/`. PDFs live in `papers/`, one folder per work,
named `{Author}-{Year}-{ShortTitle}--{filID}`; the SHA-256 moves from the file name into the
database. The researcher owns the folder tree — subfolders, moves, renames — and fil only renames
what it named. New downloads land directly in `papers/`. Folders become collections, on by
default, with a switch to turn it off.

**Why.** Researchers need to see, open, back up and organise their PDFs. A folder of SHA-256
names serves the program and nobody else.

**Taken as defaults on 27 Sept, open to change:** a project is a collection inside one library,
and a second library is allowed for anyone who wants separate worlds; inside a cloud sync folder
the database is kept in the per-user application directory, because sync tools corrupt SQLite in
WAL mode; v0.2 libraries move only when the user runs `fil library move`; an imported file keeps
its original name.

**Rejected:** a hidden hash-named store plus a readable view made of copies (doubles the disk),
symbolic links (need admin or Developer Mode on Windows) or hard links (fail on FAT32 and exFAT,
and some sync tools turn them into copies). None behaves the same on all three systems.

---

## D19 — What M3 includes

**Decided:** 27 Sept 2026 · detail in `ARCHITECTURE_PHASE3.md`

- **In:** fil IDs and the library folder; legal PDFs found and downloaded (OpenAlex locations,
  Unpaywall if spike 9 shows it adds anything); importing PDFs the user already has, verified
  before trusting (ADR-014); local documents — works with a fil ID and no OpenAlex record,
  searchable, without citation edges; text, chunks and FTS5 search; citation context attached to
  existing edges (ADR-015).
- **Out, for now:** Zotero. M3 is already the largest milestone; the cheapest later route is a
  BibTeX or CSL-JSON export, not Zotero's database file. *Superseded 3 Nov 2026 by D29:* Zotero
  comes in through its local API, with a Zotero RDF export as the fallback, since BibTeX and
  CSL-JSON lose both the collection tree and the PDFs.
- **Measured, not built:** citation edges parsed from a local document's reference list. Rule 2
  allows parsing "only for works with no DOI and no index entry, and only later". Spike 11
  measures matching against OpenAlex's own reference lists; the feature follows M3 if the numbers
  are good, one hop only, with every parsed edge marked `source = pdf` and a confidence.
- **Decided by measurement:** whether the job system (D16) is built in M3 or with M4's embedding,
  once spike 9 has timed real downloads; the PDF extractor (ADR-009), by spike 10.
- **Checked in mid-October:** `PRODUCT_PLAN.md` gates Phase 3 on someone outside the maintainer
  using v0.2 twice within three weeks — a window closing around 16 Oct. M3 starts ahead of the
  planned 6 Dec, so that check still happens, and can still change course.

---

## D20 — PDF text comes from PDFium, run as WebAssembly (closes ADR-009)

**Decided:** 24 Oct 2026, by spike 10 · detail in `SPIKES.md`

`internal/text` extracts with `github.com/klippa-app/go-pdfium/webassembly`: PDFium compiled to
WebAssembly and run by wazero, so there is still no cgo (hard rule 4). It is handed bytes, never a
path, and runs under a time limit with a recover, one file failing one paper.

**Why.** Against `pdftotext` on 19 real PDFs from five fields: 0.985 median word F1, no failures,
no ligatures left, no words run together, every page count right, and within about twice `pdftotext`'s time. The
pure-Go `ledongthuc/pdf` scored 0.24 — its text loses the spaces between words — and failed on two
files.

**Costs, accepted:** about **10 MB** more per binary (fil is 16–18 MB today, so ~26–28 MB);
`go.mod` moves to **Go 1.26** and `x/text` v0.42 when `internal/text` lands. Licences are MIT,
Apache-2.0 and BSD-3; `go-plugin` (MPL-2.0) is not linked.

**Rejected:** `ledongthuc/pdf` — small and fast, but its output cannot be searched. Shelling out to
`pdftotext` — a second program for every user to install, which is what the single binary exists
to avoid. Native PDFium through cgo — breaks hard rule 4.

**Follow-on, for step 7:** a gibberish check. One file in 19 had a text layer that was nonsense (a
scanned page stored upside down); a text layer is not the same as text, and such a paper must be
recorded as "no usable text", not indexed. **Built in step 7** (`text.Judge`): a scan is `no-text`,
a layer with too few letters, vowels or word-like tokens is `gibberish`, and neither is indexed.

---

## D21 — What fil can promise about PDFs, and how fetching runs

**Decided:** 24 Oct 2026, by spike 9 · Unpaywall decided 28 Oct, when the spike asked it · detail
in `SPIKES.md`

- **The promise is "about one paper in eight", and it depends on the field.** OpenAlex offers a PDF
  link for 25% of fetched works and a download succeeds for about half of those: roughly 46% of
  physics, 14% of computer science, 9% of social sciences, 8% of medicine and under 1% of the
  humanities. The README and `fil stats` say so per field, and every paper without a PDF says why —
  as D12 already does for references.
- **The host decides, not the paper.** arXiv served 56 of 56; publishers answered 403 to almost
  every request, and PubMed Central sent HTML. fil does not work around either.
- **`fil fetch` runs inside the command**, with a budget and progress, like `fil expand`. A median
  download is 0.8 s (90th percentile 4 s) at one request per second per host, so a few hundred PDFs
  take minutes, not hours. **The job system (D16) is not needed for downloads; it moves to M4**,
  where embedding a library is the first truly long job. Open to change if `fil fetch` on a real
  library shows otherwise.
- **Unpaywall is not built** (28 Oct). Asked about 286 works, it offered no PDF link for any of
  the 198 OpenAlex had none for, and for the other 88 only the links OpenAlex had already given.
  For the M0 seed it has ten open locations and no PDF link — the same as OpenAlex. It would add
  a required email address, a second rate limit and a second source to keep in step, for nothing.
  Revisit only if a later sample shows otherwise. `internal/sources/unpaywall` stays empty.
- **The M0 seed's PDF therefore arrives by import (step 8):** the researcher downloads its CC-BY
  PDF from PeerJ and drops it into `Inbox/`; fil identifies, verifies and files it. That meets
  M3's "the seed's PDF is in `papers/`" within the hard rules — no landing page is ever read for
  links. A direct Zenodo source (the seed has Zenodo deposits) is noted for later, like Europe PMC.
- **Europe PMC** publishes PubMed Central's open-access articles through a documented API. It is
  the most promising next source for medicine, and is noted here rather than built.

---

## D22 — No reference parsing in M3; revisit with section-finding first

**Decided:** 24 Oct 2026, by spike 11 · detail in `SPIKES.md`

Matching a PDF's reference entries to real works is **precise** — 97% of matches confirmed by year
or surname — but **recall is 44%**, because the reference list is often not found (no heading in
physics layouts) or not split (author-year and some numbered styles). Parsed citation edges for
local documents stay out of M3, as D19 planned. If built later, the work is in finding and
splitting the list, not in matching; the edges stay one hop, marked `source = pdf`, with a
confidence. Hard rule 2 is unchanged: a work OpenAlex knows always uses OpenAlex's list.

---

## D23 — Citation context fetches details of cited works, not their references

**Decided:** 29 Oct 2026, M3 step 9 · detail in `STATUS.md`

Recognising a reference in a PDF needs its title, year and first author, and most references are
stubs. Hydrating them would record their own reference lists: about 50 new stubs each, 275,000 for
a library of a hundred read papers. Instead `graph.DetailIDs` fetches the same batch (one request,
one credit per 100) and writes only title, year and authors (`store.PutDetails`); the work stays a
stub (`hydrated = 0`) and on the expansion frontier, so `fil expand` treats it as not yet fetched.
A paper that has a PDF but is itself a stub is hydrated before it is read: without its reference
list there is nothing to find.

**Superscript citation markers are not built in M3**: measured in step 9 at 23% of the references
in papers read; built in step 10 and hand-checked at 82% precision (33 of 40), under the 90% bar,
so reverted. Affiliation marks, formula indices and two-reference entries were the failures; each
needs its own fix and its own hand check before superscripts come back.

---

## D24 — `fil` stays cgo-free; the desktop app shell may use cgo (amends D9)

**Decided:** 31 Oct 2026, planning the desktop app (M5)

A desktop window is what non-coding researchers expect, and every mature way to give a Go program
a native window on macOS and Linux (Wails, the webview library) needs cgo. D9 forbade that
everywhere. Removing it everywhere would make `fil` itself need a per-OS build, and on Linux need
WebKitGTK installed, so it could no longer run on a server or in a container — the CLI, the MCP
server and the planned server mode would all pay for a window they do not use.

**So the rule is narrowed, not dropped:**

- **`fil` stays cgo-free.** The CLI, the MCP server, the library and the interface
  (`internal/ui`) remain one static binary per platform, cross-built from one machine. CI and
  GoReleaser already build it with `CGO_ENABLED=0`, so a dependency needing cgo fails the build.
- **The desktop app shell may use cgo.** It lives in `desktop/`, a Go module of its own, so its
  dependencies never enter `fil`'s `go.mod`. It is thin: a native window showing the same
  interface `fil` serves, so the interface is written once. Nothing in `fil` imports it.
- **The shell is built per OS** in CI, on each platform's own runner, and shipped beside `fil`.
- **Without the shell, the interface still works:** `fil` can open it in a browser's app-mode
  window, for Linux systems without WebKitGTK and anyone who only installs `fil`.

**Rejected:** removing the rule for everything (above); and keeping the rule with only an
app-mode browser window — no native menus, file dialogs or drop target, and macOS and Linux would
depend on a Chromium-based browser being installed.

**Costs accepted:** the release pipeline gains per-OS jobs for the shell; on Linux the shell needs
WebKitGTK at run time; contributors to `desktop/` need a C toolchain. Which webview library the
shell uses is the desktop app's first step to decide.

---

## D25 — The desktop app shell is Wails v2, showing fil's interface in the same process

**Decided:** 31 Oct 2026, M5 step 1 · measured in spike 13 (`SPIKES.md`)

The shell (D24) uses **Wails v2**. It is the only candidate that ran on every platform: Windows
(x86-64 and Arm64), macOS and Ubuntu 24.04 (x86-64 and Arm64). Its Linux start is the fastest
measured, about 0.2 s warm. Wails v3 does not run on a stock Ubuntu 24.04: WebKitGTK 6.0 always
sandboxes its web content with bubblewrap, which Ubuntu restricts, and even when allowed it took
17–26 s to start. Its tray and multi-window support are not needed for the designed screens.

**How it shows the interface:** in the same process. The window's page is served by the
`http.Handler` that `internal/ui` provides, so the interface is written
once and no local port is opened for the window. The `desktop/` module requires fil's module
through a `replace` to the repository root.

**What each platform needs:**

- **Windows:** no cgo, so it stays cross-buildable; WebView2, present on Windows 10 and 11.
- **macOS:** cgo, built on a macOS runner, with `CGO_LDFLAGS="-framework UniformTypeIdentifiers"`
  for the Xcode 26 SDK.
- **Linux:** cgo, built on a Linux runner of each architecture with the `webkit2_41` tag;
  `libwebkit2gtk-4.1` at run time.

**Revisit** Wails v3 when its Linux build starts quickly under the WebKitGTK 6.0 sandbox.

---

## D26 — The desktop app comes first; the browser version comes later (amends D24)

**Decided:** 2 Nov 2026, M5

D24 planned `fil` serving the interface from a local server, with a browser window as the fallback.
The user wants a local application first, so that is not built now. The app shows `internal/ui` in
its own window, in the same process, with no port open, so there is no local server to secure.

**A browser version is still planned, later.** It serves the same handler, so the screens are
written once. A listening port must then add what this does not need yet: `127.0.0.1` only, a launch
token, and Host and Origin checks.

**The screens have no build step:** plain HTML, CSS and JavaScript in `internal/ui/assets`, embedded
with `//go:embed`. Fonts are embedded too, so the app never needs the network to look right.

**Cost accepted:** until the browser version exists, Linux without WebKitGTK has the command line
only. The window uses the native title bar, not the one drawn in the design, so it behaves correctly
on all three systems.

## D27 — The whole-library map is drawn on a canvas, laid out by d3-force

The map around one paper uses page elements, which suits a dozen cards and not thousands of dots.
The whole-library view draws on a canvas, and d3-force arranges it in a Web Worker so the window
never waits. The library is 18 KB, ISC-licensed, and kept in `internal/ui/assets/d3force`, so
there is still no build step. Measured on a real library of 3,132 papers and 4,504 citations: 3.8 s
to arrange in the desktop window, about 3 ms to redraw. d3-force starts from fixed places with a
fixed random seed, so the same library gets the same map. It opens on papers with two or more
links, since a paper with one link is mostly a reference-list entry nothing else is known about.

## D28 — Collections nest, labels sit across them, and papers are dragged with the pointer

Schema v11. A collection can sit inside another, and its name only has to be unique among the
collections beside it, so two chapters can each hold "Methods". Choosing a collection shows the
papers in it and in every collection inside it, and its count is of distinct papers. Labels are
separate from collections: a paper can carry several, and choosing several shows the papers that
carry all of them. Favourites are a star on the paper. None of these moves or changes a PDF, and
deleting a collection or a label never deletes a paper.

A collection made from a folder in `papers/` stays the folder's (ADR-012): it cannot be renamed,
moved, deleted or filled here, and nothing can be made inside it. Folder collections nest by their
paths rather than by a stored parent, so a rescan never has to reparent anything.

Dragging papers onto a collection, a label or Favourites uses pointer events, not the browser's
drag and drop. The desktop window sets `DisableWebViewDrop` so files dropped from outside reach
the Inbox, and on macOS and Linux Wails then stops a drop inside the page from arriving at all.
Pointer events behave the same on all three. Every drag has a menu equivalent: the right-click
menu and the bar that appears when several papers are selected do everything a drag does.

## D29 — Zotero comes in through its local API, read-only (supersedes D19's route)

Zotero 7 and later serve the library on this computer at `http://127.0.0.1:23119/api/` once the
user ticks "Allow other applications on this computer to communicate with Zotero". It needs no
account or key, works offline, and gives the collection tree, every item, its attachments and the
path of each file on disk. `internal/sources/zotero` sends GET requests and nothing else, to this
computer only: Zotero 10 lets other programs write through the same API, and Filiation never will.
Reading Zotero's database file was ruled out: it is locked while Zotero runs, and its layout is
Zotero's to change. A Zotero RDF export (File, Export Library, with Export Files) is the fallback
for an older Zotero or one on another computer, and comes in a later change.

An item with a DOI, arXiv ID or PubMed ID is looked up in OpenAlex (free) and added as a seed with
what it cites, as `fil add` does; the user can turn the references off. One without, or that
OpenAlex does not know, is kept as a local document. A title is never searched on its own: each
search costs 10 of the day's 1,000 credits, so it is the user's to ask for. A PDF attached in Zotero
is copied into `papers/` without the identity check `fil import` makes (ADR-014): the user filed it
under that item, which is the evidence the check looks for. Zotero's own files are never moved.

Schema v12 records each item brought in (`zotero_item`), so bringing the library in again adds only
what is new, and a collection keeps its Zotero key, so it follows a rename in Zotero. Nothing is
ever deleted because it went from Zotero, and a collection the user made with the same name in the
same place is adopted rather than doubled. Only tags the user added become labels, and only when
asked: publishers' automatic tags often run to hundreds.

Not tested against a real Zotero yet when this was written: the client follows Zotero's documented
local API and is tested against a stand-in. A real read is to be run before release.

**The export fallback, built 3 Nov 2026.** `zotero.ReadExport` reads a Zotero RDF export into the
same items and collections, and the import runs unchanged. Read from Zotero's own "Zotero RDF"
translator, not from documentation, because three things in it matter:

- An item's subject is its ISBN, its URL or `#item_` and Zotero's local row number. None is the
  Zotero key, so an exported item is keyed by a hash of its type, title, year, first author and
  identifiers (`rdf:...`), and a collection by a hash of its path of names. A later export of the
  same library has the same keys.
- A journal article's DOI is written on its journal, the node `dcterms:isPartOf` points to, not on
  the article. Reading only the article would miss most DOIs.
- Tags a publisher or Zotero added are nodes typed `z:AutomaticTag`; the user's own are literals.

Reading the same library by both routes adds nothing twice: a matched paper is the same OpenAlex
work, a local document is found again by its title and year (`LocalWorkNamed`), and a collection
with the same name in the same place is adopted whichever route brought it.

**Matching later.** A local document, from Zotero or not, can be linked to OpenAlex afterwards: by
an identifier the user gives, which is free, or by choosing from a title search, which costs 10
credits and runs only when asked (`fil match`, or "Find it by title" on the paper). Each result
shows its type and how often it is cited, because a book's reviews carry its title (D14). The
document keeps its fil ID, PDF, collections, labels and notes; if the library has the work
already, the two become one through the same fold a merge uses, and the document's fil ID becomes
an alias. Building this found that a merge deleted a work's `zotero_item` row; it now moves it.

## D30 — The window has no frame, and the app switches libraries while it runs

Decided 3 Nov 2026. The Windows title bar, a strip in the system's accent colour with nothing of
the app in it, made Filiation look unfinished beside the tools researchers already use. On Windows
the window is frameless and the page draws its own title bar, as VS Code does: the logo, the menus
(File, Edit, View, Library, Go, Paper, Help), the library switcher in the middle, the running job,
and minimise, maximise and close. Wails moves the window from anything marked
`--wails-draggable: drag` and keeps the edges for resizing and snapping. macOS keeps its three
buttons over the page, and its menu bar at the top of the screen is built from the same list
(`ui.Menus`), each item sending its ID to the page; Edit there is the system's own. Linux keeps its
window manager's frame, because Wails cannot resize a frameless window there, and the page draws
the menus below it.

The menus are data in Go, so the page and the Mac's menu bar cannot drift apart, and every command
runs by its ID however it was asked for. A menu holds only what the app can do: items that need a
paper are greyed until one is chosen. The library card, the job card and Help left the sidebar for
the title bar.

The settings could already list several libraries (`fil library new`), but the app opened only
one. A `ui.Window` now serves one library at a time and swaps it while running: every request to
the open library holds a read lock, and a switch waits for them, stops the running job only once
the person has agreed, opens the new library before closing the old one (so a library that will not
open leaves the last one on screen), and gives it a fresh server, so nothing one library's screens
remembered follows the person into the next. The library switched to is the one the app opens next
time. Opening a folder lists it in the settings (`config.Adopt`); an empty or new folder becomes a
new library; any other folder must have its `filiation.toml`, so a folder of photos is refused
rather than turned into a library. A library whose folder has gone can be taken off the list, which
deletes nothing.

## D31 — Bringing in the papers around one paper, with their details only

Decided 4 Nov 2026. Expand grows outward from the whole library; a researcher often wants only the
neighbourhood of the paper in front of them. "Bring in papers around this" fetches, for one paper,
what it cites and what cites it, one citation step and no further (`graph.BringAround`).

What it cites is already recorded as stubs; their details cost one request per 100. What cites it
is a new query, `filter=cites:W…` sorted by `cited_by_count:desc`, one request per 200, so a
classic with ten thousand citers gives its most cited first and the person chooses how many
(up to 1,000). Measured on PeerJ 4375: 1,238 citers worldwide, 45 references and 30 citers brought
in by two requests and the batch for the references.

By default the papers come in as stubs with their details (title, year, venue, type, citation
count, authors) and the citations to and from this paper, not their own reference lists: 100 citing
papers recording theirs would add about 5,000 "Not fetched" rows to the list. A citing paper stays
off the expansion frontier until something cites it, so Expand does not wander into it later. A box
in the dialog records their references as well, hydrating them as Expand would.

Download PDF on a paper is `fil fetch <paper>`: naming a paper is asking for it, so every link is
tried again. It is greyed when OpenAlex knows no legal copy, or only a web page, and says why.

## D32 — The citation map sizes and filters in the page, and your own PDF goes straight onto a paper

Decided 4 Nov 2026. The whole-library map sized a paper only by how often the library cites it, and
offered no way to thin it out. The map's data now carries each paper's year and the count OpenAlex
keeps of its citations worldwide, so a circle can say either, or nothing; papers can be kept to those
cited at least so often, a range of years, with or without a PDF, the person's own papers and what
they link to, and fetched or not. What is left out is faded, so the map keeps its shape, or hidden.
All of it runs in the page over data already loaded, so a slider answers at once; the choices are
kept per computer.

A paper with no legal copy can take a PDF the person has: "Add your PDF" files it under that paper
(`library.AttachPDF`). The Inbox already filed a PDF under the paper it names, matched by OpenAlex
ID (hard rule 6), but made the paper a seed and needed the file identified first. Here the person
has named the paper, so it is not made a seed, the file is copied rather than moved, and the title
is still checked: a file that does not carry it is asked about, never attached silently. Filing
fetches a paper known only by ID, as a download does, so its folder has a name and its citations
can be found in its text.

## D33 — Search takes the Papers list's filters, and a view is a saved question

Decided 6 Nov 2026. Search looked across the whole library and kept the best 20 passages; a filter
applied after that would have been left with whatever of those 20 fell inside it, often nothing.
Search now takes the Papers list's filters (a collection with those inside it, a label,
Favourites, years, authors, venues, a tab) and they are applied inside the full-text query: the
papers they find are passed to SQLite as one JSON list (`json_each`), so the best passages are
chosen among those papers only. The answer says how many papers were searched and how many of
them have text, because a filter can land on papers with no PDF.

A saved view is the Papers list's filters under a name (schema v13, `saved_view`). It keeps the
filters as JSON, not the papers, so a paper filed or fetched later that matches shows up in it,
and deleting a view touches no paper. A view that keeps every paper is refused, since it would only
be the Papers list again. Opening a view puts its filters on the list as chips that can still be
changed; the view itself changes only on "Update to these filters". A view whose collection is
deleted finds nothing rather than everything. `fil search --view` and search_library's `view`
search inside one.

## D34 — Citation groups are found from the citations, and suggested, never filed

Decided 7 Nov 2026. Papers that cite one another more than they cite the rest of the library
usually share a topic or a debate. Filiation finds these groups with the Louvain method
(`gonum/graph/community`, pure Go) over the citations taken as undirected links, and suggests each
as a collection. The groups come from the citation edges alone, never from a model reading the
papers, which keeps them on the right side of hard rule 3.

Only papers linked to at least two others are grouped. On a library of 4,613 works grown from one
seed, clustering every paper let the references that a single paper cites (3,470 of them) fill each
group with one paper's bibliography; keeping to the 1,143 linked twice or more gave 11 groups that
read as real debates (the open-access citation advantage, journal economics, replication, altmetrics)
in under 100 ms. The papers linked to one group and nothing else are its fringe, and making a
collection can file them too. A group needs five papers to be suggested. Broad, normal and fine are
resolutions 0.5, 1 and 2: 6, 11 and 21 groups on that library.

Groups are worked out each time they are asked for, so papers added later join them, and the same
citations always give the same groups: papers are numbered in fil ID order and the search is seeded
with a constant. A group is known by its anchor, the member the others cite most, and named after
its most cited member with a known title ("Around Ioannidis 2005"), since the anchor can be a
reference not fetched yet. What the user chooses about a group, not useful or made into a
collection, is kept under its anchor (schema v14, `group_choice`), so a choice survives the group
changing shape and holds at every detail. Making a collection files the papers in one transaction
and is the only thing that ever files them. The Papers list keeps to a group as a chip, the
citation map can colour its eight largest groups, and `fil groups` and the `citation_groups` tool
read them. Where reference lists are few, mostly in the humanities (D12), groups are few, and the
screens say that is the data.
