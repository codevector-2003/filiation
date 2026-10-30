# Project status

**Date:** 30 October 2026 · **Position:** **M3 complete — v0.3** · next: **M4**
**Phase 1 target:** v0.1 by 14 November 2026

> **In one line:** M0 is done, and M1's expander works live — `fil expand` grew one seed to 525
> fetched works and 11,802 citations, with no duplicate IDs or DOIs. Title-level duplicates
> (~4%) are reported by `fil stats --duplicates` and never merged on a guess (D14).
> `fil path`, `fil neighbours` and GraphML export work, and the 20-seed validation run passed
> 20 of 20. Licensed Apache-2.0 (D15). **v0.1 released 25 Sept 2026** — M1 is complete.
> **M2 complete, v0.2 released 25 Sept 2026:** `fil mcp` serves the library to AI assistants —
> five tools, verified live over stdio and from a real client, with authors on every fetched work.
> **M3 complete, 30 Oct 2026:** the library is a folder of readable `papers/`; `fil fetch` downloads
> the open-access PDFs, reads them and makes them searchable; `fil import` files the PDFs a
> researcher already has; citations carry the sentence they are made in. The 20-seed run passed
> 20 of 20, and the seed's own PDF is in `papers/` and searchable (`VALIDATION.md`).

---

## Where this sits against the plan

`ARCHITECTURE_PHASE1.md` §10 puts spikes in week 1 (15–21 Sept) and the first real package in
week 2. Those spikes ran on 30 August. The schedule has roughly two weeks of slack going into
M0, which is worth keeping rather than spending — §10 also warns that weeks 5–6 (`internal/graph`,
the budgeted expander) hold the only genuinely hard code in the phase, and that slack should be
protected for them.

| Phase | Milestone | State |
| --- | --- | --- |
| 0 | Spikes, scaffolding, toolchain | **Complete** |
| 1 | M0 — skeleton, `fil add` writes one node | **Complete** — 24 Sept, verified live |
| 1 | M1 — budgeted expansion, export, **first release** | **Complete** — 25 Sept, released as v0.1 |
| 2 | M2 — MCP server | **Complete** — 25 Sept, released as v0.2 |
| 3 | M3 — fil IDs, library folder, PDFs, text, citation context, FTS5 | **Complete** — 30 Oct, released as v0.3 ([`ARCHITECTURE_PHASE3.md`](ARCHITECTURE_PHASE3.md)) |
| 4 | M4 — retrieval and answers | Not started |
| 5 | M5 — web interface | Not started |
| 6 | M6 — claim genealogy | Not started |

---

## What is complete

**Toolchain and distribution (D8, D9).** Go 1.27.0. `go build`, `go vet` and `gofmt` are clean
across the module. The binary cross-compiles to `windows/amd64`, `darwin/arm64` and `linux/amd64`
from Windows with `CGO_ENABLED=0` — **including with the WASM SQLite driver and its extensions
linked in**, which was the real test. The single-binary promise holds.

**Package layout.** Every package in the declared tree exists with a doc comment stating what
belongs in it and what must not. Four packages named in `ARCHITECTURE_PHASE1.md` §2 but missing
from the tree were added: `config`, `identity`, `model`, `export`.

**Storage stack proven end to end.** The real `internal/store/schema.sql` applies cleanly through
`ncruces/go-sqlite3` — FTS5 virtual table, all three sync triggers, the partial frontier index,
WAL. Keyword search, vector search and the graph all live in one file with no server.

**Seven spikes, all run.** Results and reproducible code in [`SPIKES.md`](SPIKES.md) and
`spikes/`. Decisions D11, D12 and D13 came out of them.

**Documentation reconciled.** Every measured number was written back into the documents that
carried the wrong one — §3's cost model, ADR-004's rate limit, §9's spike list, the stack table
and API notes in `CLAUDE.md`, and the risk register in `SCOPE.md`.

**M0 steps 1–2 — the two leaf packages.** `internal/model` carries `Work`, `Edge`,
`FrontierItem` and `ExpansionResult`. Optional fields are pointers so a stub cannot hand back a
zero value that reads like real data, and `DOI` in particular *must* be one: the column is
`UNIQUE`, and SQLite permits many NULLs in a unique index but only one empty string, so a
string-typed DOI would make the second stub without one fail to insert. `HydratedWork` is split
from `Work` so that `IsDeadEnd` cannot be asked of a work loaded from the store, where the
reference list is gone and every work would answer yes.

`internal/errs` began with four sentinels — `ErrNotFound`, `ErrTransient`, `ErrUnresolved`,
`ErrAmbiguous` — and no logic. It has since grown three, each added by the package that first
needed the CLI to answer differently: `ErrInvalidConfig` (step 3), `ErrSchemaTooNew` (step 4) and
`ErrInvalidInput` (step 5). `ErrNotFound` (this library has no such row) and `ErrUnresolved`
(OpenAlex has no record) are deliberately distinct: the first is answered by adding the work, the
second can never be answered, and merging them would have expansion retrying dead identifiers
forever. Keeping the vocabulary in a leaf package is also what lets `cmd/fil` branch on an error
without importing `store`.

**M0 step 3 — `internal/config`.** Four decisions were taken while writing it:

- **TOML, not JSON**, for the config file (`BurntSushi/toml`, MIT, no cgo). This file is meant to
  be hand-edited by researchers, and a format where a trailing comma is fatal is a support burden.
- **`os.UserConfigDir`, not `adrg/xdg`** — stdlib is already correct on all three targets, so the
  dependency ADR-006 suggested is **not taken**. Recorded in `go.mod`.
- **Contact email stays optional**, surfaced through `Warnings()` rather than refused. `mailto`
  produced byte-identical rate-limit headers (D11), so blocking on it would punish politeness that
  buys the user nothing.
- **`config` may import `errs`** — the one exception to its leaf status, so a bad config file
  reaches the CLI as `ErrInvalidConfig` rather than an opaque string. `errs` imports nothing, so no
  cycle is possible. `identity` will want the same exception at step 5.

Precedence is applied **per field, not per source**: a `--db` flag must not discard budgets set in
the file. Unknown keys are refused rather than ignored — `max_node = 5000` parses cleanly, changes
nothing, and would leave the user believing they had raised a budget they had not.

`config` never prompts. It reports `FirstRun` and the resolved paths; `cmd/fil` does the asking and
calls `Save`. That keeps the package usable from a test, an MCP server and a web handler, none of
which have a terminal.

**M0 step 4 — `internal/store`.** The package is in two halves. The connection half handles both
of spike 5's silent failures: FTS5 is registered with `sqlite3.AutoExtension` before either handle
is opened, and `foreign_keys` is set in the DSN of *every* connection rather than trusting the
`PRAGMA` in `schema.sql`, which only ever bound the connection that applied it. A third, found
while writing it: the driver applies its one-minute `busy_timeout` default only when the DSN
carries no `_pragma` at all — set any other pragma and the default silently becomes zero, which
turns every moment of contention into an immediate `database is locked`. It is now set explicitly.

The query half is four decisions:

- **`Tx` is the only way to write.** The write handle is unexported, so the single-writer rule is
  structural rather than remembered. The unit of work is the caller's, not the statement's:
  hydrating a work, writing its edges and creating the stubs they point at is one transaction,
  because the edges reference rows the same call creates.
- **Hydration updates, it does not replace.** A work is almost always already present as a stub
  when it is hydrated, so `depth` keeps the smaller of the two values, `is_seed` is sticky and
  `source` records how the work *first* entered and is never rewritten. Overwriting any of them
  would lose provenance that no later fetch can reconstruct.
- **`RecordEdgesAndStubs` records every reference, including a review's eight hundred.** They
  arrive free inside a response already paid for, and an edge is ground truth this layer is not
  entitled to discard. §4's cap on how many may compete for the budget moves to the frontier
  query in M1, where it can be applied per citing work without throwing measurements away.
- **A non-bare OpenAlex ID is refused, not normalised.** `openalex_id` is the deduplication key,
  so a work admitted as `https://openalex.org/W...` is not a formatting problem — it is a
  duplicate node the primary key can no longer catch. Normalising is `internal/identity`'s job.

Dead ends are reported rather than raised: `Recorded.DeadEnd` answers "this work cannot extend the
graph" as a fact about the data, which is what D12 requires of every layer that touches it.

Twenty-six tests, including the two §8 names as the ones that will actually catch regressions —
recording the same reference list twice writes nothing the second time, and a failure partway
through a reference list leaves the citing work without `fetched_refs`.

**M0 step 5 — `internal/identity`.** `Parse` turns whatever the user typed into a kind and one
normalised value: OpenAlex IDs in bare and URL form, DOIs in the five resolver formats and inside
publisher URLs, arXiv IDs in both schemes with the version dropped, PMIDs, and titles.
`NormaliseOpenAlexID` and `NormaliseDOI` are exported for `sources/openalex`, which must run every
ID in every response through them — OpenAlex returns the URL form, and `store` refuses it. Three
decisions:

- **A malformed identifier is refused, never searched as a title.** `W0123`, `10.12/abc` and
  `doi:hello` return `errs.ErrInvalidInput` with the input quoted and the reason given. Falling
  through would ask the user to pick a candidate for something that was never a title.
- **`ErrInvalidInput` is a new sentinel.** The CLI answers it differently from
  `ErrInvalidConfig` — echo the argument and list the accepted forms, rather than name the config
  file — and `store` now uses it too, where step 4 had borrowed `ErrInvalidConfig`.
- **Bare numbers are PMIDs only from five digits.** `2017` is refused with a hint to write
  `PMID:2017`; a year typed by mistake must never seed a graph from an unrelated paper.

DOIs are lower-cased (case-insensitive by specification), and stray trailing punctuation is
trimmed — a closing bracket only when unbalanced, because real DOIs contain balanced ones.
`ArXivDOI` maps an arXiv ID to its DataCite DOI (`10.48550/arxiv.…`). Step 7 checked whether
OpenAlex resolves it: **not reliably** — see below.

**Read against `quelle`** ([vcoeur/quelle](https://github.com/vcoeur/quelle), MIT) — as prior art
only; nothing is imported or copied. Its DOI and arXiv patterns match ours. Three of its ideas were
reimplemented here:

- **A DOI cut out of a URL loses the URL's wrapping.** Publishers append file extensions and view
  segments after the DOI (`…/10.1073/pnas.1719367115.full.pdf`, `…/asi.24301/full`), and
  bioRxiv and medRxiv append a version (`…002386v1`). Captured as-is, each is a DOI that resolves
  to nothing. Trimmed for URLs only — a DOI the user typed is taken as typed.
- **arXiv `/html/` links** are accepted alongside `/abs/` and `/pdf/`.
- **`TitleKey` and `TitlesMatch`** — case-folded, letters and digits only, then edit-distance
  similarity at 0.85. One deliberate difference: `quelle` also accepts one title *containing* the
  other, which would call "Attention" a match for "Attention Is All You Need". Ours does not. It
  ranks and flags title-search candidates; it never accepts one (ADR-005).

Two more are **deferred, not dropped**:

- **An ordered source fallback chain** — try each source in turn, treat not-found and network
  failure as "try the next", keep the last error, and merge enrichment only when an `accept`
  predicate agrees the record is the same work. This is the shape M3 needs for OpenAlex →
  Unpaywall PDF locations, and the shape Crossref will slot into later.
- **ISBNs.** `quelle` resolves books through Open Library, Google Books and BnF. That is directly
  relevant to D12: the humanities graph dies after one hop because its citations are books. Not
  in M0 scope, but it belongs in the conversation about what to do for those users.

**M0 step 6 — `internal/httpx`.** One `Client` per upstream service, each with its own
`rate.Limiter` — 5 req/s for OpenAlex against the measured first 429 at ~11 (spike 3). Retries
three times on 429, 5xx and network failure (§7), with exponential backoff and jitter, honouring
`Retry-After`. Four decisions:

- **Failure is classified, not just reported.** A 429 or 5xx that never clears wraps
  `errs.ErrTransient`, the expander's "skip this batch and carry on". Any other status fails at
  once as a `*StatusError` carrying the code and the start of the body — OpenAlex explains a 400
  only there, and `sources/openalex` needs a 404 to become `ErrUnresolved`, not a retry.
- **A `Retry-After` over 60 s fails immediately**, with the wait attached for the CLI to report.
  That is the daily allowance spent, and sleeping through it looks like a hang.
- **The cache is a directory, not the SQLite file ADR-004 proposed** — all SQL stays in
  `internal/store`, and no second WASM database handle. Files are written to a temporary name and
  renamed, so a reader never sees half a response; the key is stored in each file and checked on
  read. Only 2xx responses are cached, for a week by default, and a hit skips the rate limiter.
- **The contact goes in the `User-Agent` and in a per-source query parameter** (`mailto` for
  OpenAlex, `email` for Unpaywall), but **not in the cache key** — changing it does not empty the
  cache, and the address is never written to disk.

`Client.Quota` exposes the last `X-RateLimit-*` headers, closing the carry-forward below about
reading the allowance rather than trusting a constant. Where the cache lives is not decided here:
`config` has no cache path yet, and step 8 will choose one (likely `os.UserCacheDir`).

`golang.org/x/time` is pinned at **v0.15.0**: v0.16.0 requires Go 1.26 and `go get` silently raised
the module's `go` line to match, which would have broken the Go 1.25 convention. Tests use a stub
`RoundTripper` and a recorded sleep, so the suite never touches the network and never actually waits
out a backoff; one test runs the real limiter to prove it spaces requests.

**M0 step 7 — `internal/sources/openalex`.** `Resolve` fetches the one work a deterministic
identifier names; `GetWorksBatch` hydrates by OpenAlex ID, 100 to a request; `SearchByTitle`
returns candidates. Tested against **real recorded responses** (`testdata/`, 24 Sept 2026), and
recording them overturned two assumptions — see "Recorded while building step 7" in
[`SPIKES.md`](SPIKES.md). Five decisions:

- **arXiv resolves in two steps.** OpenAlex 404s "Attention Is All You Need" by its arXiv DOI, so
  the DOI is tried first (free) and on a 404 the work is found by its arXiv `/abs/` landing page
  (1 credit). The arXiv ID it was found by is kept on the work.
- **A batch's silence is confirmed, not trusted.** A filter omits IDs it cannot match, and the
  absence means either "gone" or "merged into another record". Each omitted ID gets one free single
  lookup: a 404 goes in `Missing` (to be marked unresolved), a different ID coming back goes in
  `Aliases` (the caller must fold the old node into the new, or hard rule 6 breaks).
- **A 404 is `ErrUnresolved`, never transient**, and its HTML body never reaches the user.
- **Title search flags, never accepts.** Candidates arrive hydrated, sorted so a close
  `identity.TitlesMatch` comes first. In the recorded search, only the exact title is flagged —
  not "Attention Is All You Need *In Speech Separation*". It costs 10 credits.
- **No `sources.Source` interface yet.** `ARCHITECTURE_PHASE1.md` §2 sketched one so Crossref could
  slot in later. With one implementation it would be a guess at a shape; the idiom is to declare it
  where it is consumed, in `internal/graph`, once there is a second source to fit.

`HTTPOptions` holds the facts about OpenAlex — 5 req/s, `mailto` — so whoever builds the client
cannot get them wrong. Identifiers from responses all pass through `identity`, because OpenAlex
returns every one as a URL. References that fail to normalise are skipped, so one bad entry cannot
cost a work its other edges. Recording the fixtures cost about 13 of the day's 1,000 credits and
did not send `mailto`.

**M0 step 8 — `internal/library`, and the first of `internal/graph`.** `library.Add` takes what the
user typed and makes it a seed: parse, resolve, then the work, a stub per reference and the edges
in one transaction. **It passes M0's definition of done end to end** — every package real except
the network, which replays step 7's recorded responses: `10.7717/peerj.4375` adds 55 works (54
stubs) and 54 edges, and adding it again writes nothing and makes no request.

- **The two package notes contradicted each other**, and the split resolves it. `library/doc.go`
  said library calls store and sources; `graph/doc.go` and `store` said only graph may know both.
  Now **`graph` moves data** (`AddSeed`, `AddSeedWork`) and **`library` is the composition root**:
  it builds the store and the OpenAlex client and hands both to graph, but never passes data
  between them. `go list` confirms `store` never reaches `sources` and `sources` never reaches
  `store`.
- **`graph.Source` is declared where it is consumed**, with only the two methods graph uses. It is
  what lets graph's tests use a map instead of OpenAlex — not a guess at Crossref's shape.
- **A title never writes before a choice** (ADR-005). `Add` returns an `*AmbiguousError` matching
  `errs.ErrAmbiguous` with the candidates, closest title first. `AddCandidate` adds the chosen one
  **without a second request** — candidates carry their reference lists. `AcceptFirst` is the
  scripted path.
- **Re-adding is reported, not silent.** `Added.AlreadySeed` is read inside the writing
  transaction, so the CLI can say "already in your library" rather than print zeros; `Added.DeadEnd`
  says a seed cannot grow the graph (D12) instead of leaving the user to wonder.
- **The cache lives at `config.DefaultCacheDir`** — `os.UserCacheDir()/filiation/http`, deliberately
  not beside the library, because one is disposable and the other is not. `Options.NoCache` and
  `ClearCache` are the `--no-cache` and `fil cache clear` ADR-004 requires from day one.

Front doors get only `model` types, `library` types and `errs` sentinels, so `cmd/fil` can
branch on every outcome without importing `store`. **Not handled yet:** a seed whose DOI already
belongs to a *different* OpenAlex ID in the library fails on the `UNIQUE` index rather than being
reconciled. That is `graph`'s identity-resolution job, and lands with M1's merged-record handling.

**M0 step 9 — `cmd/fil`, and M0 is done.** Run live on 24 Sept against OpenAlex, in a throwaway
profile: `fil add 10.7717/peerj.4375` printed the title, recorded 54 references as stubs with 54
edges, and a second run reported "Already in your library" and made no request. Commands: `add`,
`where`, `version`, `cache clear`, with `--db` and `--no-cache` on all of them.

- **Every error class has its own exit code and one sentence of advice** — retype it (2), choose a
  candidate (3), OpenAlex has no record (4), try later, with the wait if OpenAlex gave one (5), fix
  the named config file (6), upgrade fil (7). A script can branch on the code; a person reads the
  sentence. OpenAlex's HTML 404 page never reaches the terminal.
- **Input is checked before anything is created.** Found by running it: the first draft made the
  library and config file on `fil add 2017` and only then refused the input. `library.Validate`
  now runs first, and a test pins it.
- **fil asks only when someone can answer.** On a terminal, a first run offers a choice of library
  location and a title offers a numbered list; a bad answer is asked again, Enter cancels. Piped
  or scripted, it never prompts — it prints the candidates and exits 3 with the `--accept-first`
  hint. First-run notes (where the library went, no contact email) are printed once.
- **The front-door rule is enforced twice.** `.golangci.yml` carries it with depguard, alongside
  `store` ↛ `sources`, `sources` ↛ `store`, and a ban on the cgo SQLite driver. But `golangci-lint`
  is not installed here, and a lint rule binds only people who run the linter — so
  `cmd/fil/imports_test.go` enforces the front-door rule on every `go test ./...`, and proves it
  can fail.
- **Ctrl-C cancels the context**, so an add in flight rolls back rather than being killed mid-write.

Binaries are **14–16 MB** stripped, against spike 7's ~2.3 MB. The difference is the WASM SQLite
engine, which the spike binary did not yet link; nothing requires cgo and all three targets
cross-compile. The README now shows what works and states the field-coverage limitation (D12)
up front, closing that carry-forward item.

**Dependencies, all licence-checked before adding.** `ncruces/go-sqlite3` MIT ·
`go-sqlite3-wasm/v3` MIT-0 · `julianday` MIT · `golang.org/x/sys` BSD-3 · `BurntSushi/toml` MIT ·
`golang.org/x/time` BSD-3 ·
`spf13/cobra` Apache-2.0 · `spf13/pflag` BSD-3 · `inconshreveable/mousetrap` Apache-2.0.
Nothing copyleft, nothing
requiring cgo. The licence question in "Still open" remains genuinely open — no dependency has
forced it.

---

## Results

### The seven spikes

| # | Question | Result |
| --- | --- | --- |
| 1 | Batch fetch by OpenAlex ID? | **Pass.** Ceiling is exactly 100; 101 is a hard 400. References arrive complete in batched responses — 5,389 edges, zero truncation |
| 2 | Real `per_page` maximum? | **200**, with an explicit error above it. Not the binding constraint — the 100-ID filter is |
| 3 | Real sustained rate? | **First 429 at ~11 req/s.** Neither documented figure was usable. Bucket set to 5 req/s |
| 4 | Reference coverage by field? | **Answered, and it constrains the product.** See below |
| 5 | Does the driver ship FTS5? | **Pass**, as a loadable extension rather than compiled in |
| 6 | Vector search end to end? | **Partial.** Exactly correct, but no ANN index |
| 7 | Cross-compile with no cgo? | **Pass.** Three platforms, D9 holds |

### The cost model was wrong in both directions (D11)

Measured by watching `X-RateLimit-Remaining` across controlled runs:

| | Assumed | **Measured** |
| --- | --- | --- |
| Daily allowance | 100,000 credits | **1,000** ($0.10 equivalent) |
| Single work by ID | 1 credit | **0 — free** |
| List request | 10 credits / 50 works | **1 credit / up to 200 works** |

Batching survives; its stated justification does not. It was defended as five times cheaper per
work, and single fetches turn out to cost nothing. The real reason is round trips: at ~11 req/s,
500 single fetches is ~50 seconds and 500 chances to be throttled, against 5 requests batched.
**Credits will never be what runs out. The request rate will.**

A 500-node graph costs 5 credits — half a percent of a day.

Separately, `mailto` produced byte-identical limit headers with and without it. ADR-004's claim
that the polite pool is "far faster" did not reproduce. Keep sending it, because it is what
OpenAlex asks for and how they reach us; do not design around a benefit from it.

### The graph works in STEM and dies in the humanities (D12)

Two populations were measured, because they give very different answers and only one is the
question worth asking. A uniform random sample of OpenAlex is mostly editorials, errata and thin
records nobody would ever expand into. The **frontier** sample — works actually reached by
following a real seed's references — is the population expansion hydrates.

| Field | Frontier dead ends | Median refs |
| --- | --- | --- |
| Medicine | **6%** | 43 |
| Physics and Astronomy | **12%** | 24 |
| Computer Science | **15%** | 12 |
| Social Sciences | 54% | 0 |
| **Arts and Humanities** | **84%** | 0 |

In the humanities the graph stops after one hop: books and chapters dominate citation practice
and those publishers largely do not deposit reference lists. This is the "coverage will
disappoint people" risk from `SCOPE.md`, landing one layer earlier than expected — on the graph,
not the PDFs.

Also measured: STEM papers cite **70–100** works, not the ~40 the expansion arithmetic assumed.
Depth 2 is ~5,000 nodes rather than 1,600, which makes the node budget more necessary, not less.

### Vector search is correct but has no index (D13)

`ext/vec1` — SQLite's own extension, already in the dependency tree — returns results identical
to a brute-force check in Go, survives a reopen, and coexists with FTS5 in one file. But version
0.7 accepts only `none` or `flat`, and flat *is* a linear scan:

| Chunks | Projected query |
| --- | --- |
| 2,000 | 133 ms |
| 20,000 | 1.3 s |
| 100,000 | 6.7 s |

A 500-paper library is roughly 50,000 chunks — about 3 seconds, against a 500 ms target.

**This is not a blocker, but it reorders M4.** Hybrid retrieval was always specified as: seed by
keyword and meaning, expand along citation edges, rerank. A scan over a candidate set already
narrowed by FTS5 and the graph sees thousands of chunks, not the library, and lands near 100 ms.
Pre-filtering moves from optimisation to load-bearing, and must exist *before* semantic search is
demonstrated on a real library.

---

## Next steps

### Immediately: M0, built inward-out

Leaves first, each package tested before the next begins. The order is chosen so the riskiest
package is proven early rather than discovered late.

| Order | Package | What lands | State |
| --- | --- | --- | --- |
| 1 | `internal/model` | `Work`, `Edge`, `FrontierItem`. Must make the stub state impossible to forget — a `Work` may have an ID and nothing else (ADR-003) | **Done** |
| 2 | `internal/errs` | Sentinels the CLI can act on: not found, transient, unresolved, ambiguous. **Not budget exhausted** — a run that stops on its budget succeeded, and reports `model.StopBudgetExhausted` (§7) | **Done** |
| 3 | `internal/config` | `--db` > `FILIATION_DB` > config file > per-user default (ADR-006). Contact email. `MaxNodes` default **500** | **Done** |
| 4 | `internal/store` | Two handles — read pool, and a write handle at `SetMaxOpenConns(1)`. PRAGMAs, embedded schema, `Tx`, and the first queries | **Done** |
| 5 | `internal/identity` | DOI, arXiv, OpenAlex ID, PMID, URL, title. **Title search never auto-accepts** (ADR-005). It also owns normalising the OpenAlex URL form, which `store` refuses outright | **Done** |
| 6 | `internal/httpx` | 5 req/s token bucket, `mailto`, backoff honouring `Retry-After`, response cache in a separate file | **Done** |
| 7 | `internal/sources/openalex` | `GetWork`, `GetWorksBatch` (**chunks of 100**), `SearchByTitle` | **Done** |
| 8 | `internal/library` | `Add` — resolve, hydrate seed, record edges and stubs | **Done** |
| 9 | `cmd/fil` | cobra wiring, plus the lint rule forbidding front doors from importing `store` | **Done** |

**The two gotchas from spike 5 are handled**, both in `internal/store`, and both were silent
failures if missed. They stay written down because a future connection opened anywhere else has to
obey the same rules:

- FTS5 and vec1 are registered with `sqlite3.AutoExtension` **before** either handle is opened. A
  connection without the extension cannot even read a table created with it.
- `PRAGMA foreign_keys` is **per connection**, not stored in the file, so it is set in the DSN of
  every connection in both the read pool and the write handle. The one in `schema.sql` only ever
  bound the connection that applied it.

**M0 is done when** `fil add 10.7717/peerj.4375` writes a row and prints the title, running it
twice adds nothing the second time, and the seed's 54 references are present as stubs with edges.

> **Changed 24 Sept 2026.** The original acceptance DOI, `10.1145/3292500`, turned out in step 7
> to be the KDD 2019 *proceedings volume* — type `paratext`, zero references — so the last clause
> could never pass. `10.7717/peerj.4375` ("The state of OA") has 54 references and is gold OA, so
> it also exercises M3's PDF path.

### M1, complete — v0.1, 25 Sept 2026

*Done when:* one seed gives a clean 500-node graph with no duplicates, and it opens in Gephi.

| Order | Work | State |
| --- | --- | --- |
| 1 | `store`: best-first frontier query, merged-record folding | **Done** |
| 2 | `graph.Expand`: the budgeted loop, with §8's resume, idempotency, budget, cycle and single-writer tests | **Done** |
| 3 | `library.Expand` and `fil expand`, with progress and a coverage report | **Done** — verified live |
| 4 | Title-level duplicates: reported by `fil stats --duplicates`, never merged (D14) | **Done** |
| 5 | `fil neighbours`, `fil path` — breadth-first search, not a recursive CTE (measured). `fil stats` landed with step 4 | **Done** |
| 6 | GraphML export, the 20-seed validation run, GoReleaser, and the licence (Apache-2.0, D15) — **done**. Tagged **v0.1.0** | **Done** |

**Steps 1–3.** `fil expand` grows the graph from everything in the library, best first — in-graph
in-degree, then depth, then ID, so the order is deterministic and resume is exact. One transaction
per batch of up to 100; Ctrl-C loses only the batch in flight, and what was already fetched for it
is committed. Stopping on the budget, an empty frontier, the depth limit or Ctrl-C is a normal
outcome with its own message; the report always gives reference coverage (D12), and explains it
when it drops below 70%.

- **Every hydrated work records all its references**, including at the depth limit. §4's
  pseudocode skipped them there; that would leave hydrated works without their edges and force a
  refetch when a later run goes deeper. The depth limit governs only which stubs are fetched.
- **The per-work cap lives in the frontier query**, as a window function over the order OpenAlex
  sent references in — so a review's 800 edges are all kept, but only its first
  `max_refs_per_work` compete for the budget.
- **A batch's silence is confirmed**, and the live run proved it matters: 10 of the seed's 54
  references were omitted by the filter, and 2 of those exist. See `SPIKES.md`.
- **Duplicates by DOI are folded.** The first live run died on OpenAlex holding one preprint
  under two IDs with one DOI; the whole batch rolled back. `graph.hydrate`, the one path by which
  a fetched work enters the library, now folds a work into the record already holding its DOI —
  edges move, the references are kept as the holder's, and the report counts it as merged. Seeds
  go through the same path.
- **The report cannot count a rolled-back batch.** The failed run reported 137 works fetched when
  only 46 were saved; counts are now applied only after the commit succeeds, and a test pins it.
- **Three batches failing in a row stops the run** with `ErrTransient` — the network is gone —
  while a single failed batch is skipped for the rest of the run and left on the frontier (§7).

Live, the second run fetched 478 works in 21 s: 525 hydrated, 7,677 works, 11,802 edges, with no
duplicate IDs or DOIs, no self-loops and no dangling edges.

**Step 5 — `fil neighbours` and `fil path`.** Both read the library only, never OpenAlex: a
paper not yet added gets "add it first" (exit 8), not a silent fetch. They take any identifier, or
a title matched against the library's own titles — one close match is used and named in the
output; several are listed (exit 3). For a read-only question that is safe in a way it is not for
`fil add`: nothing is written.

- **`fil neighbours`** lists a paper's references, fetched first, and what *in your library*
  cites it — never a global count.
- **`fil path`** looks for **lineage** by default: one paper reaching the other by following
  references, tried in both orders and always printed in the order asked. `--any-direction`
  allows steps either way. No chain within `--max-hops` (default 6) is an answer, not an error:
  it prints the way forward and exits 1.
- **Breadth-first search, not a recursive CTE** — the stack table's plan. Measured on the live
  library, the recursive CTE produced 20.8 million rows in 125 s to reach depth 5, because it
  enumerates paths rather than works. The search is one set-based SQL query per step plus a
  visited set, so each work is touched once and cycles cannot blow it up. Ties go to the lower
  ID, so the same library always gives the same path. `CLAUDE.md`, `ARCHITECTURE*.md` and
  `graph/doc.go` are updated; the numbers are in `SPIKES.md`.

On the live library, `fil path` found the seed descending from *Invisible Colleges* (1973) through
a 2011 RCT in two steps, and the two *Invisible Colleges* records D14 reports turned out to be
cited by different papers — the split citations that make a duplicate costly.

**Step 6, first part — GraphML export.** `fil export graphml -o library.graphml` writes the
graph for Gephi, Cytoscape, yEd, NetworkX or igraph. Each work is a node carrying label, title,
year, type, venue, DOI, OA status, global citation count, depth, and seed / fetched / unresolved
flags; each arrow means "cites". Absent values are left out, not written as zero — "year 0" would
put every stub at the start of a timeline.

- **Fetched works only, by default**, with the citations between them; `--include-stubs` exports
  everything. After one expansion the library is ~93% stubs — no title, year or type — and they
  bury the readable graph. No edge ever refers to a node the file does not contain.
- **It streams** from the store, so a library of any size exports in constant memory, and writes
  to a temporary file renamed into place, so a failed export never truncates a good file.
- **It refuses to dump XML onto a terminal**: without `-o`, a person at a terminal is asked for a
  file; a pipe or redirect gets standard output as usual.

**Validated live with an independent reader.** The live library exported as 472 works / 2,593
citations by default and 6,555 / 10,049 with stubs. NetworkX, installed in a throwaway
environment, read both as directed graphs with typed attributes and one connected component. The
most-cited node is Lawrence 2001, *Free online availability substantially increases a paper's
impact* — the founding paper of this seed's field, which is what a correct graph should show.
The 93%-stub export is §8's "stub handling" test, passed on real data. **Opening it in Gephi is
still to be done by hand** — it is a GUI and was not available here.

**Step 6 — the validation run.** §10's 20 papers across 5 fields, live, one library each, with
the default budget: **20 of 20 completed every step** — add, expand, duplicate report, and a GraphML
export read back by an independent parser — with no errors and the budget exact throughout. Full
tables and findings: [`VALIDATION.md`](VALIDATION.md). In brief:

- Reference coverage on the frontier: medicine 93%, physics 91%, CS 88%, social sciences 59%,
  humanities 56% — D12's ordering, less steep than spike 4 because the seeds were chosen with
  reference lists. The low-coverage note fired for six of eight social-science and humanities
  seeds and no STEM seed.
- Title duplicates: 1.4 per 100 fetched works in medicine rising to 4.1 in the humanities.
- **D14's best evidence:** OpenAlex lists Bohr's same-titled reply as EPR's only reference. The
  duplicate report flagged the pair; a title merge would have folded Einstein into Bohr.
- 500 works in 14–73 s; the run cost ~115 credits.

**Step 6 — release tooling.** `.goreleaser.yaml` builds `fil` for Windows, macOS and Linux on
amd64 and arm64, with `CGO_ENABLED=0` (hard rule 4), the version stamped in, and a checksum file.
`.github/workflows/release.yml` runs the tests and publishes on a pushed `v*` tag, so released
binaries come from a clean runner and the tagged commit only.

**Both release decisions are made.** *Version:* **v0.1** — §10's name for it, and consistent with
the locked decision that v1 includes the full retrieval engine. *Licence:* **Apache-2.0** (D15),
in `LICENSE`, with the copyright line in `NOTICE`; both ship in every archive.

### M2, complete — v0.2, 25 Sept 2026

*Done when:* an assistant can add a paper, grow the graph and answer "what connects these two
papers" against the user's own library, from a real MCP client.

| Order | Work | State |
| --- | --- | --- |
| 1 | `internal/mcpsrv` on `modelcontextprotocol/go-sdk` v1.8.0: five tools — `add_paper`, `expand_graph`, `neighbours`, `find_path`, `library_stats` | **Done** |
| 2 | `fil mcp`: stdio transport; a first run takes the default library silently, notices on stderr | **Done** — verified live over stdio |
| 3 | A real client: Claude Code, headless, one researcher's question — all five tools used well; see [`VALIDATION.md`](VALIDATION.md#m2--a-real-mcp-client-25-september-2026) | **Done** |
| 4 | Authors on returned works: `authorships` stored with the work in one transaction, loaded for any number of works in one query; the MCP tools return the first three names and a count, and `fil add` / `fil neighbours` print a byline | **Done** — verified live |
| 5 | `cited_by_count` renamed `cited_by_count_global` in MCP output, so an assistant cannot present it as a count within the library | **Done** |
| 6 | Tag **v0.2** | **Done** — 25 Sept 2026 |

**The surface follows `ARCHITECTURE.md` §6.** Every list is capped and ranked — fetched works
first, then by global citations — and says how many it left out. Every work carries its OpenAlex
ID and OA status. Stubs say `stub: true` and carry no title. Ambiguous titles come back to
`add_paper` as candidates with `status: "choose"`, never as a guess (ADR-005). Errors name the next
step ("add it with add_paper first"). `find_gaps` waits for co-citation; `search_library` and
`ask_library` for M3–M4.

**`expand_graph` runs inside the call (D16)**, 200 works by default and 500 at most, with a progress
notification after each batch. The job system of §5 is built with M3, which needs it.

**The live check** drove the built binary over real stdio against OpenAlex, with an isolated
profile. stdout carried nothing but JSON-RPC; `add_paper` wrote the M0 seed; a title returned
candidates and wrote nothing; `expand_graph` fetched 52 works in 7 s with two progress
notifications; `find_path` returned the lineage chain; an unknown ID came back as a tool error
pointing at `add_paper`; the server exited 0 when the client closed stdin.

**Found on the way:** OpenAlex returns **`oa_status: "diamond"`** — open access with no author
fees. The model treated it as unknown, so `IsOpen` reported diamond papers closed. It is now
`model.OADiamond`, and open.

### M3, complete — v0.3, 30 Oct 2026

*Done when:* `fil fetch` downloads the PDF of `10.7717/peerj.4375` into the library folder,
`fil search` finds a passage in it with its page, some of its citations carry their sentence,
`fil stats` says why each paper without text has none, a second fetch downloads nothing, and a
PDF dropped into `Inbox/` is identified, verified and filed.

The plan was agreed in conversation on 27 Sept and recorded as D17 (fil IDs, hard rule 6
reworded), D18 (the library is a folder the user chooses; `papers/` with readable names) and D19
(what M3 includes). Full design and ADRs 010–015: [`ARCHITECTURE_PHASE3.md`](ARCHITECTURE_PHASE3.md).

| Step | Work | State |
| --- | --- | --- |
| 1 | `identity`: fil IDs | **Done** — 27 Sept |
| 2 | Schema v2 and the first migration; fil IDs through every layer | **Done** — 27 Sept |
| 3 | The library folder and `papers/`: config, names, atomic writes, rescan, `fil library move` | **Done** — 23 Oct |
| 4 | Spikes 9 (PDF availability), 10 (extractor bake-off), 11 (reference matching) | **Done** — 24 Oct, D20–D22; Unpaywall measured 28 Oct: adds nothing (D21) |
| 5 | Legal PDF links: OpenAlex locations, Unpaywall if spike 9 justifies it | **Done** — 24 Oct (OpenAlex); Unpaywall not built (D21) |
| 6 | Downloading, `fil fetch` | **Done** — 24 Oct; the seed's own PDF arrives by import (step 8): no source has a PDF link for it |
| 7 | `internal/text`, FTS5, `fil search` | **Done** — 28 Oct; the seed's own passage waits on its PDF (step 6) |
| 8 | Import, `Inbox/`, local documents | **Done** — 29 Oct; live: 34 of 40 renamed PDFs filed, all correct, the other 6 with the right paper among the candidates |
| 9 | Citation context | **Done** — 29 Oct; hand-checked: 50 of 50 mentions on a fresh sample tie to the right work |
| 10 | `fil list`, `fil show`, full text in `fil stats`; MCP `search_library` and a fetch tool | **Done** — 29 Oct; checked from a real MCP client |
| 11 | Validation run, docs, tag **v0.3** | **Done** — 30 Oct; 20 of 20 seeds, the seed's own PDF imported and searchable, 49 of 50 sentences right by hand; five bugs found and fixed |

**Steps 1–2 — fil IDs, and the first migration.** Every work now has a fil ID
(`F7K2M9QXA`): generated in `internal/identity` from `crypto/rand`, with at least one digit so
that a nine-letter word beginning with F is never read as an ID, and no lookalike substitution
for the same reason. `fil add` refuses a fil ID without a request — it names a work already in
the library.

`work.fil_id` is the primary key and every table that refers to a work refers to it;
`openalex_id` is `UNIQUE`. The write path still takes OpenAlex IDs, because that is what OpenAlex
sends, and looks up or creates the fil ID inside the single writer; reads take any ID a work has
or had. `fil neighbours`, `fil path` and the MCP tools accept fil IDs, and every result shows both.
GraphML nodes are now keyed by fil ID, with `openalex_id` as an attribute.

- **The first real migration.** A v1 library is upgraded in one transaction: the v1 tables are
  renamed aside, the v2 tables created, rows copied through a map from OpenAlex ID to new fil
  ID, row counts compared table by table, `foreign_key_check` run, and only then the v1 tables
  dropped. **The rowid order of `cites` is preserved** — the frontier ranks references by it.
  Foreign keys stay on throughout, which SQLite's generic twelve-step recipe does not manage:
  renaming a table rewrites the references to it, and dropping the v1 children before `v1_work`
  means the parent's drop cascades into nothing. Tested against a real v1 library built from the
  v0.2 schema, and against one that would lose a row, which is refused and left untouched.
- **Found on the way:** `fil_id TEXT PRIMARY KEY` accepted NULL. SQLite lets a non-INTEGER primary
  key hold NULL unless the column also says `NOT NULL` — a compatibility quirk. It now does, and a
  test pins it.
- **Folds keep names and user data.** When two records fold into one, the survivor keeps its fil
  ID; the other's fil ID and OpenAlex ID become aliases, and its notes, collections and text move
  to the survivor rather than being deleted by the cascade. An alias is never reused as a new
  fil ID.
- **Tie-breaking is unchanged.** Path finding and neighbour lists still break ties by OpenAlex
  ID, which orders the same way in every library; fil IDs are random and would not.
- A clashing fil ID is drawn again inside the single writer (tested with a forced clash), and a
  generator that only ever returns taken IDs fails after 16 draws rather than looping.
- **Measured, not assumed:** recording 200 works citing 80 overlapping references each (16,000
  edges) takes 0.95 s on v1 and 1.35 s on v2. The difference is mostly the second unique index
  `work` now keeps. A first draft that checked each new ID in a query of its own took 1.43 s, and
  one that tried the insert first took 2.1 s — a refused insert costs more than a lookup, and in a
  dense graph most references point at works already present. On a live 500-work expansion,
  which is network-bound at ~20 s, the difference is about a second.

**Step 3 — the library folder and `papers/` (ADR-011, ADR-012).** A library is now a folder:
`filiation.toml`, `filiation.db`, `papers/`, `Inbox/`, `exports/` and `.filiation/tmp/`. A first
run proposes `Documents/Filiation/My Library`; on Windows the Documents folder comes from the
shell's known-folder API, because it is often redirected into OneDrive. *Done when* — a work
folder can be created, moved by hand and found again — is met in tests and was run live.

- **Settings are in two places.** The global `filiation.toml` lists the libraries
  (`[[library]]` with name and folder) and `default_library`; each library folder has its own
  `filiation.toml` for its name, budgets and `folder_collections`, so they travel with the folder.
  Budgets apply defaults < global < library, per field. `--library <name or folder>` picks one
  for any command; `--db` still opens a bare database.
- **Cloud sync folders.** A folder inside OneDrive, Dropbox, Google Drive or iCloud Drive keeps
  its database in the per-user directory (`libraries/<name>-<hash>/filiation.db`), and where it
  went is recorded in the global settings rather than worked out again. Detection reads the path
  and OneDrive's environment variables. **This applies to the maintainer's own machine**:
  Documents is in OneDrive.
- **v0.2 libraries keep working** unchanged: a `db_path` setting opens a database with no folder.
  `fil library move <folder>` gives one a folder — copies the database with `VACUUM INTO`,
  checks the copy (integrity check, and the counts of works, citations, files and notes), switches
  the settings and leaves the original for the person to delete. A library with a folder is moved
  as a whole by a rename, same drive only. Run live against a library made by the v0.2.0 binary:
  it upgraded from schema 1 to 3 and moved with every row.
- **Schema v3:** `work_folder` (path, work, the name fil gave it, missing), `work_file` (path,
  work, SHA-256, size, mtime, version, origin, missing) and `folder_collection`. All three are new
  tables, so the upgrade is schema.sql's `CREATE … IF NOT EXISTS`. A fold moves folders and files
  to the survivor, like notes.
- **Names** are `{Surname}-{Year}-{ShortTitle}--{filID}`, folded to ASCII with `x/text`
  (BSD, pure Go — the one new dependency) plus a table for letters that do not decompose (ß, ø,
  Ł). The title stops at a word boundary within 60 characters. Stored paths are relative, `/`,
  NFC. fil warns about paths past 240 characters.
- **Scans** run when a command needs them. A folder is found by the fil ID in its name (any case;
  aliases left by folds resolve); one whose ID was removed is found by the hashes of its files,
  hashing only files of the same size; a deleted one is marked missing and the paper stays. New
  files in a work folder are hashed once and recorded; unchanged ones (same size and mtime) are
  not re-read. Hashing happens before the write transaction opens. A PDF dropped loose in
  `papers/` is reported and left alone for import (step 8).
- **fil renames only what it named.** `fil library folder <paper>` renames a folder when the name
  would now differ — a stub has since been fetched — but only while the folder still has the
  name fil gave it.
- **Folders become collections** (on by default): each folder holding work folders is a
  collection of them, by its path under `papers/`. Collections a person makes are never touched.
- **Writes are atomic** (`blobs.Write`, for step 6): a temporary file in `.filiation/tmp/`, synced,
  checked (`%PDF-` in the first 1024 bytes, size limit, optional hash), then renamed. It never
  overwrites. Temporary files older than a day are cleared on open; younger ones may be another
  process's download.
- **Commands:** `fil library` (where it is, what `papers/` holds, collections, other libraries),
  `library folder <paper>`, `library scan`, `library new <folder> [--default]`, `library move`.
  `fil where` names the folder and the database separately.
- **Found on the way:** the CLI test harness had no Documents override, so one test run created an
  empty library folder under the real Documents. It was removed at once; the harness now passes
  its own, and every test that loads settings does too.

**Step 11 — the validation run** ([`VALIDATION.md`](VALIDATION.md#m3--20-seeds-5-fields-the-papers-on-disk-30-october-2026)).
M1's 20 seeds again, each in a library folder of its own: expansion to 200 works, `fil fetch` of 25
PDFs, a title search for every paper with text, an import check per field into an empty library,
and 50 citation sentences checked by hand. Code: `spikes/12-validation-m3`.

- **M3's definition of done, met.** The seed's own PDF, saved from PeerJ's page in a browser (PeerJ
  refuses scripted downloads), was filed from `Inbox/` by the DOI in its header and read: 23 pages,
  searchable, 28 of its 47 references with their sentence.
- **20 of 20 seeds completed every step.** 226 PDFs from 476 papers tried — physics 72, humanities
  14 — 219 of them searchable. 51% of citations made by papers with text carry their sentence:
  62% in computer science, 32% in medicine, whose journals cite by superscript (D23's follow-up).
  A paper's own title finds it in the first five papers 213 times in 219.
- **The hand check: 49 of 50 right**, at least 9 of 10 per field. The wrong one was a one-name
  marker, "Sachs (1995b)", given to a three-author paper of that year; that is now refused.
- **Import into an empty library filed 15 of 40 PDFs, none wrongly**; for 15 more the right paper is
  among the candidates. Step 8's 34 of 40 was measured where the papers were already in the library.
- **The download check flagged 11 PDFs, 2 rightly**: a wrong file at an OpenAlex link, and an arXiv
  copy under its preprint title. Seven of the nine false alarms are scans, front matter or
  OpenAlex's own author order.
- **Five bugs found and fixed, each with a regression test.** A `*` or `?` in a title read off a
  PDF made OpenAlex refuse the search with a 400, and that stopped the whole import — wildcards are
  now stripped (this also covers `fil add` by a title ending in a question), and a refused search
  leaves only that file waiting. A work named by both a PDF's DOI and its title search was offered
  twice. A two-word surname ("Aspuru-Guzik") was looked for as one word, failing the download
  check. "Sachs (1995b)" was tied to Sachs, Tornell and Velasco. And "p." before a page number
  ended a sentence. The last two change what reading stores, so the extractor version is
  `pdfium/4` and every PDF is read again once.
- **Not fixed, recorded:** two text layers the gibberish check passed (poor OCR; a font that
  drops "c" and "ff"); NBER cover pages read as titles by import; an affiliation letter set
  against a surname ("Buonannoa,b").

**Step 10 — the front doors.** What steps 5–9 recorded, through every front door, each over one
library function.

- **`fil list`** — seeds first, then most cited here; beside each its first author, PDF, text, and
  citation sentences. `--seeds`, `--with-pdf`, `--without-text`, `--collection`, `--title`,
  `--limit`, `--json`. One store query.
- **`fil show <paper>`** — details, place in the graph, its folder, its PDF or **why there is none**
  (no open copy, only a web page, every link failed with how, deleted by you), its text or **why
  there is none**, and its first citation sentences.
- **`fil stats --why`** — every paper whose PDF gave no text, by reason: M3's "`fil stats` says why
  each paper without text has none".
- **MCP: eight tools.** `search_library` (passages with page and file), `get_paper` (`fil show` as
  data), `fetch_pdfs` (downloads inside the call, with progress, like `expand_graph`); `neighbours`
  gains citation sentences; `library_stats` gains the PDFs. Instructions and the stub description
  now say a stub may have a title (D23).
- **A download is checked against its paper** once read: its first pages must carry the paper's
  title and first author (step 8's check). A mismatch is **flagged, kept**: schema v9's
  `work_file.identity`, which survives the file moving and resets when it changes. Live, 2 of 22
  downloads were flagged. One was right — a report by Brody et al. sitting at the link of Henneken et
  al. 2006. The other was wrong: a PLoS ONE PDF prints "Björk" as "Bjo¨ rk", an accent set as a glyph
  of its own. `Clean` now joins such accents (`ExtractorVersion` `pdfium/3`), which also makes the
  name searchable, and the check ignores accents on both sides. After the fix: 1 flagged, correctly.
- **Superscript citations, built and dropped.** Built as planned — digits set against the word
  before, lists numbered "1 B. D. …" — and hand-checked on 40 random mentions: **33 right (82%)**,
  under the 90% the plan set. The failures: author affiliation marks on page 1 ("Nilsson,¹ … Peres³"),
  formula indices ("Fn1≥n2", "±qx1"), and entries holding two references. Reverted; each failure
  has a fix to try with its own hand check (D23).
- **From a real client** (Claude Code, headless; `VALIDATION.md`): it fetched, searched 13 times,
  quoted seven papers with their pages, named the works a paper cites for its main claim with the
  sentence, and used the download flag to find the misfiled PDF. Two gaps it hit are fixed:
  `paper` takes the `fil_id` (it tried "Eysenbach-2006"), and `library_stats` lists the papers
  without text (it could not see them).

**Step 9 — citation context (ADR-015).** Every place a paper cites one of its references, found
in its PDF while it is read, with the sentence: `mention` rows, and `cites.context` set to the
first. Nothing read from a PDF creates, removes or redirects an edge — a mention for a work the
paper does not cite in OpenAlex's list is refused by the store.

- **Two routes** (`text.Contexts`). *Author–year* markers — "(Piwowar et al., 2018)", "Hajjem and
  Harnad (2006)", "(Laakso & Björk, 2011; Sutton 2011)" — name the work, and are matched to the
  references directly by first author and year, with no need for the reference list; two
  references sharing both are never told apart. *Numeric* markers — `[12]`, `[3–5]`, `[1, 4, 7]`
  — go through the numbered list: found after the last references heading, or, in physics layouts
  with none, by entries numbered from 1 anywhere in the paper; split by their running numbers; each
  entry matched by DOI, or by 70% of its title's words confirmed by year or surname, or — physics
  styles print no titles — by the first author's surname at the entry's start plus the year, when
  exactly one reference has both. A bare `(12)` counts only in a paper that never uses brackets.
- **The sentence** runs back to the previous sentence end and on to its own, ignoring "et al.",
  "Fig.", "Ref." and initials, stopping at a heading; capped at 600 characters round the marker.
  The section is the heading above it.
- **Details, not hydration, for cited stubs** (D23): reading first fetches title, year and authors
  of the stubs the papers cite (`graph.DetailIDs`, one request per 100) without recording their
  own references, so the graph does not grow and they stay on the frontier. A paper with a PDF
  that is still a stub is hydrated first — it had no reference list at all.
- **Schema v8, `mention`**; `ExtractorVersion` is `pdfium/2`, so libraries read before are read
  once more. `fil neighbours <paper> --context` shows each reference's sentences with their page;
  `fil stats` counts how many citations carry one.
- **Live, on the 42 papers of the step 7 library still on disk** (the rest had been deleted from
  the scratch folder by the system's temp cleaner — not by fil; all marked missing). First pass:
  75 citations with a sentence. Three causes found and fixed: 34 papers downloaded as stubs had no
  reference list (now hydrated before reading); physics reference lists print no titles (now
  matched by surname and year); a Reviews of Modern Physics text layer had lost its parentheses,
  and a collaboration paper's list sat before 1,000 author names (citation context and the list
  search now cope). **1,208 mentions; 693 of 1,645 references (42%) carry a sentence — 63% in the 21
  papers where any marker was found.**
- **The hand check, the done-when.** 50 random mentions read against the cited work and, for
  numeric ones, the list entry: **46 of 50 right** on the first sample. The four wrong ones each
  had a cause, now fixed and kept as tests — a second author taken for the first ("Cornish and
  Littenberg 2015"), an initial for a surname ("C."), one of several Abbott 2016 papers chosen by
  luck, two columns interleaving entries ("9. 16. Peres"). **A fresh sample after the fixes: 50 of
  50 right.** The sentence itself is clean in about 43 of 50; the rest carry layout debris — a
  "PACS numbers:" line, a page number, formula fragments — around a correct citation.
- **Superscripts, measured, not built** (the plan's choice): 14 of the 41 papers with references
  cite only by superscript numbers ("tunnel junction,¹"), holding 377 of the 1,645 references
  (23%). Worth building next: the text layer shows them as digits glued to the word before, and
  their lists ("1 B. D. Josephson, …") need one more entry pattern. The sample was mostly physics,
  because the temp cleaner left mostly physics PDFs; a mixed-field check belongs in step 11.

**Step 8 — importing PDFs the researcher already has (ADR-014).** A PDF dropped into `Inbox/`,
left loose in `papers/`, or named with `fil import <file>` is identified, checked and filed.

- **Clues, free and local** (`text.Front`, `text.Clues`): the first two pages' text, the largest
  type on page 1 (its title, usually), and the PDF's metadata. Identifiers are tried in order of
  how telling they are: the arXiv stamp, an arXiv download's file name, a DOI labelled in page 1's
  header or footer, the metadata, a labelled DOI elsewhere on page 1, a PMID, any DOI. A DOI broken
  across lines is joined; Springer's invisible `(0123456789().,-volV)` after its DOIs and a bracket
  set around one are cut off, while a DOI's own parentheses — The Lancet's `(20)` — are kept.
- **Nothing is trusted until it checks out.** An identifier resolves with one free OpenAlex lookup,
  and the work's title and first author's surname must be in the PDF (`text.Mentions`). For an
  identifier found in the PDF itself a close match will do (`text.Resembles`): at least 60% of the
  title's words in page 1's top lines, and the surname as a word — a preprint's title is often not
  the published one ("2D" for "two-dimensional"). A title search's candidates get the exact check
  only, and never file anything on their own (ADR-005); `--accept-first` takes the best only if it
  checks out. Title searches cost 10 credits, at most 20 a run.
- **Where files go:** from `Inbox/` into the paper's folder, under its own name; one in `papers/`
  stays where the researcher put it; one from elsewhere is copied, the original untouched. The same
  bytes twice are reported, not stored; an annotated second copy is kept beside the first. An
  imported paper is a seed with `source = upload`; one OpenAlex does not know becomes a **local
  document** (`--local`): a fil ID, its text searchable, no citations.
- **Schema v7, `import_attempt`:** each file's outcome by hash, so `fil fetch` and `fil extract` —
  which file whatever in `Inbox/` identifies itself for free, before reading — do not ask about an
  unchanged file again. `Inbox/fil-report.txt` says why each PDF still there is waiting, for a
  researcher who only looks at the folder. `fil import` asks at a terminal (`1`–`n`, `l` for a
  local document); in a script it prints the `--as` command for each candidate. `fil stats` counts
  what is waiting.
- **Live, 40 PDFs from step 7's library renamed `paper-00.pdf`… into an empty library:** first
  run, 26 filed, all correct, 14 waiting. Two causes found and fixed — a surname read with its
  affiliation mark ("Alicea1"), and published titles that differ from the preprint's — then an
  author line set in the title's size running on into the title query, and kerning that breaks a
  word ("d evices") sinking a title search (now cut at the author list, and retried once on the
  first six words). **Final: 34 of 40 filed automatically, every one correct; the other 6 all have
  the right paper among their candidates, first for 5 of them.** 21 s; 9 title searches.
- **Found on the way: a downloaded PDF can be a different edition from its record.** The one
  "wrong" answer in the first run was right: the file was the 2020 edition of a Living Review,
  which step 6 had saved under the 2016 record because that record's link serves the newest
  edition. `fil fetch` never checked a download against its paper; the same title-and-author check
  could, cheaply — noted for step 10.
- **Found on the way:** dropping the seed's own PDF in needs the PDF, and PeerJ and Europe PMC
  both answer scripts with a bot wall (403), as spike 9 measured — so the researcher downloads it in
  a browser.
- **A bug the tests caught:** a surname looked for inside the text with spaces removed let "Ma" —
  first author of a different "Attention is all you need…" paper — match "parmar" and "models".
  Surnames are now matched as whole words.

**Step 7 — text and `fil search` (ADR-009, D20).** Every PDF in `papers/` is read with PDFium
in WebAssembly, cleaned, checked, cut into passages and indexed with FTS5.

- **`internal/text`**, no database: `PDFium` (spike 10's set-up — two instances, bytes not paths,
  a time limit that kills the instance, panics recovered, and now **no file-system mount at
  all**: go-pdfium mounts the whole drive unless told otherwise); `Clean`; `Judge`; `Chunks`.
  `Read` ties them together and turns a bad file into a result, never an error.
- **What PDFium hands back, measured on real papers:** `\r\n` line ends; a word hyphenated at a
  line end arrives already joined, with **U+0002** where the hyphen was; **U+00AD** standing in for
  a printed hyphen inside a line; other control and private-use characters for glyphs with no
  meaning. A hyphen that does end a line is a number range. `Clean` handles each.
- **The gibberish check** (spike 10 found a text layer is not text): too few characters per page
  is a scan (`no-text`); too many U+FFFD, too few letters, too few vowels among Latin letters, or
  too few word-like tokens is `gibberish`. Language-blind — German, Chinese and formula-heavy pages
  pass — and tuned to lean towards ok, since a paper wrongly dropped from search is worse than
  noise in it. On 10 real papers: 86–96% letters, 96–100% word-like, vowel share 0.39–0.41,
  against limits of 35%, 45% and 0.25.
- **Passages** of about 250 words end at a sentence and never cross a page, so every hit has one
  page; each stores its byte offsets in its page. Passages after the last references heading are
  marked `references` and left out of search by default (`--references` puts them back) — a
  heading missed costs some noise, nothing else (spike 11).
- **Schema v6:** `work_text` (the file read and its hash, the status and why, the extractor
  version) and chunk offsets — the first `ALTER TABLE` in the migrations. A v1 library's chunk
  table is rebuilt from the current schema on the way, so the step adds only missing columns.
  A changed file, or a newer `ExtractorVersion`, is read again; a scan is not retried until it
  changes. Folding two works keeps one paper's text, never two mixed.
- **Search** is FTS5 bm25 with Porter stemming, at most three passages a paper by default.
  **What a person types is never FTS5 syntax:** every word and quoted phrase becomes a quoted FTS5
  string, `*` a prefix, control characters separators — `C++`, `NEAR(`, `col:umn` or a stray quote
  cannot be a syntax error (tested). Matches are marked with control characters the front door
  renders; the CLI shows «green open access».
- **Commands:** `fil fetch` reads every new PDF once its downloads are done (the user's choice:
  after the downloads, not between them); `fil fetch <paper>` reads that one. `fil extract
  [paper] [--again]` reads PDFs put in `papers/` by hand. `fil search <words> [--limit]
  [--paper] [--references]` prints paper, page, snippet and file, and says how many PDFs are
  still unread. `fil stats` gains a Text line.
- **Start-up:** compiling PDFium's 5.75 MB module took **3.1 s on every run**. wazero's
  compilation cache, kept in the user cache directory beside the HTTP cache (`…/filiation/pdfium`,
  ~21 MB), brings it to **80 ms**; a cache that cannot be used is skipped, never fatal.
- **Cost:** Go 1.26 (`go-pdfium` requires it); binary 29.4 → 40.2 MB on Windows, +10.7 MB on each
  platform; `CGO_ENABLED=0` builds for all three.
- **Live, 6 seeds across physics, CS, medicine, economics and biology:** `fil fetch --max 120`
  downloaded 111 PDFs (121 MB, 4 min 38 s) and **read all 111 in 37 s** — 4,354 passages, a
  187-page review included. A phrase copied from page 11 of one paper was found on page 11; an
  author's name found their own paper and, with `--references`, the bibliographies citing them.
- **Found live: the first gibberish limit was wrong.** One physics review was called gibberish
  for 7% unmapped characters — every one a mathematical symbol in its equations, its prose intact
  (96% word-like). Exactly the failure the check must not make. The limit is now 25%: a font with
  no character map loses its letters, far more than that. Across the other 110: letters 0.67–0.93,
  word-like 0.87–0.99, vowels 0.37–0.42 — every limit has a wide margin. Kept as a test.
- **Seen live, and fixed after step 7 (29 Oct):** a paper downloaded while still a stub showed as
  "[not fetched: W…]" in search and got an `Unknown-nd-Untitled` folder. `fil fetch` now hydrates
  the stubs it is about to download — one request per 100 (`graph.HydrateIDs`) — so each has a
  title, a named folder, and its own references for citation context (step 9). And a search with
  no passage holding every word returned nothing: it now falls back to passages with any of the
  words, bm25 ranking those with more first, labelled as partial. Still as written: some PDFs space
  a ligature inside a word ("fi ndings"), which PDFium reports faithfully.
- **Tests** use real PDFs made in code (`text/pdftest`), so no binary sits in `testdata`. A
  3,000-page PDF that takes 17 s to read is stopped at a 50 ms limit, and both instances serve the
  next files. The fetch test's seed is now a real PDF: fetched, read, and found on page 2.

**Step 6 — `fil fetch` downloads (ADR-013).** PDFs land in `papers/`, in each paper's
readable folder, named by version: `published.pdf`, `accepted.pdf`, `preprint.pdf`.

- **`internal/sources/pdfs`**, the one place fil downloads a PDF: streamed, never cached; no
  cookies or credentials; a User-Agent that names fil; one request a second to each host **on
  every redirect hop**; the first 1 KB must look like a PDF, and a web page is refused unread.
  Outcomes are named — refused, not found, not a PDF, too large, timeout, network, redirect loop.
- **`blobs.Stage` → `Commit` / `Discard`:** a download is hashed in `.filiation/tmp/` before it
  lands, so bytes the library already holds for *another* paper — a publisher's notice served
  for many papers — are refused as a duplicate.
- **Schema v5, `download_attempt`:** every attempt, with outcome, status, size and time. A paper
  without a PDF says exactly why; a link that failed is left alone for a month.
- **Order and budget:** seeds first, then the most cited; links tried best first, at most three a
  paper, one PDF a paper; four downloads at once; 50 PDFs a run by default (`--max`). A PDF the
  researcher deleted is not downloaded again; `fil fetch <paper>` asks for it explicitly.
- **Live, on the M0 seed expanded by 100:** run 1 downloaded 32 of 50 (20.8 MB, 2 min 51 s,
  most of it one download running to the 2-minute timeout); run 2 the next 21 of 50 (36 s). 103
  attempts, no URL tried twice, all 53 files hash-checked as PDFs. arXiv 24 of 24, PLOS 8 of 8;
  BMJ, Wiley and handle.net repositories refused every time — as spike 9 measured. A deleted
  folder was not re-downloaded; naming the paper fetched it again.
- **Done when, half met:** a second run downloads nothing once the candidates are used up
  (tested). *"The seed's PDF is in `papers/`"* is not: the M0 seed has no OpenAlex PDF link
  (step 5). It waited on spike 9's Unpaywall measurement, by decision on 24 Oct. **Measured 28
  Oct: Unpaywall has no PDF link for it either** (D21), so it arrives by import in step 8.

**Step 5 — legal PDF links (ADR-013).** Every work gets an answer: a ranked list of legal PDF
links, or why there is none — open but only as a web page, no open copy, not in OpenAlex.

- **Free for new works.** Hydration now asks OpenAlex for `best_oa_location` and `locations`
  too; a list request costs one credit whatever it returns. Works fetched before, and stubs, are
  looked up 100 to a request, the IDs a filter drops confirmed one by one (spike 1). A response
  without locations — an older cached one — reads as "not known", never as "no open copy".
- **Order to try:** published, then accepted, then preprint; within a version, OpenAlex's best
  location first. Only open locations, only http(s) links, each URL once.
- **Schema v4:** `work_pdf` (the answer and when it was asked) and `work_location` (the ranked
  links). Answers older than a month are asked again — repositories catch up.
- **`fil fetch --dry-run`** looks the links up, seeds first then the most cited, and reports what
  fil would download and why the rest cannot be; `fil fetch --dry-run <paper>` shows one paper's
  links. `fil stats` gains a PDFs line. Plain `fil fetch` refuses until step 6 builds downloading.
- **Live, on the M0 seed expanded by 100:** 1,470 works answered in 14 requests — 32% with a PDF
  link, 16% open without one, 49% no open copy, 4% not in OpenAlex. The 96 hydrated works had
  their links free.
- **Measured: OpenAlex is slow to answer for locations** — about 1.25 s for 20 works against
  0.74 s without them. Three lookups now run at once, all still on the shared 5-per-second bucket,
  committed in order by the single writer: 48 s → 21 s for the run above.
- **Found: the M0 seed itself has no direct PDF link.** `10.7717/peerj.4375` is gold open
  access, but none of its twelve OpenAlex locations carries a `pdf_url` — only landing pages. M3's
  definition of done is to download exactly that PDF, so OpenAlex alone cannot meet it: whether
  Unpaywall offers `url_for_pdf` for it is now the question spike 9 must answer.

**Step 4 — spikes 9, 10 and 11** (results in `SPIKES.md`, decisions D20–D22), run live on 24 Oct:

- **Spike 9:** OpenAlex offers a PDF link for 25% of 8,385 fetched works, and about half of
  those downloads succeed — roughly **one work in eight** overall, from ~46% in physics to under 1%
  in the humanities. arXiv served 56 of 56; publishers answered 403 to nearly everything, and PubMed
  Central sent HTML. Median download 0.8 s. So `fil fetch` runs inside the command, and the job
  system moves to M4 (D21). Unpaywall, asked on 28 Oct with a separate address, added no PDF
  link OpenAlex did not already give, so it is not built (D21).
- **Spike 10:** **PDFium as WebAssembly** (D20) — 0.985 median word F1 against `pdftotext`, no
  failures; the pure-Go candidate ran words together (0.24). Costs ~10 MB and Go 1.26. A text layer
  can be gibberish, so step 7 needs a check for it.
- **Spike 11:** matching a PDF's references is 97% precise but 44% recall, because the list is
  often not found or not split. Not built in M3 (D22).

**Found while planning.** OpenAlex's `oa_url` for the M0 seed, a gold OA paper, is a DOI landing
page and its `pdf_url` is null — what v0.2 stores is usually not a PDF, so M3 reads OA locations
(ADR-013). Unpaywall requires an email, and ours is optional. The migration code has never
upgraded a library: v2 is its first real use. `httpx` reads whole bodies into memory and caches
them, which is right for JSON and wrong for PDFs.

**Network.** The cloud environment used for development blocks `api.openalex.org`,
`api.unpaywall.org` and PDF hosts. Unit tests need none of them (recorded fixtures), but spikes
9–11 and the live validation run on the maintainer's machine unless the environment's network
access is widened.

**Taken as defaults, open to change** (D18): a project is a collection inside one library; the
database stays out of cloud sync folders; v0.2 libraries move only on `fil library move`;
imported files keep their original names.

### Decided: title-level duplicates are reported, not merged (D14)

After ID and DOI dedup, ~4% of fetched works still share a title with another — OpenAlex records
under different DOIs or none. **Decided 24 Sept: report them, merge nothing on a title match.**
The live run proved the point: the shared-title groups include a book review titled like its
book, two different magazine pieces titled like Ioannidis's 2005 paper, and 1970s reviews of
*Invisible Colleges*. Full reasoning and the rejected options are in
[`DECISIONS.md`](DECISIONS.md#d14--title-level-duplicates-are-reported-never-merged-automatically).

`fil stats` now reports the library at a glance — seeds, fetched, still to fetch, not in
OpenAlex, citations, and reference coverage (D12) — and the number of shared titles;
`fil stats --duplicates` lists them with year, type and DOI so a person can judge. Titles under 20
letters and digits ("Editorial") are not reported. Automatic merging on title + year + a shared
author is the revisit, once the store persists authors.

M1's "no duplicates" is therefore read as: no duplicate IDs or DOIs, no merged records left
unfolded, and every probable title duplicate reported.

### Carry forward from the spikes

- **Surface reference coverage per node** (D12) — the same way OA status is surfaced. *`fil add`
  explains a dead-end seed and the README states the field limitation (M0).* Still to do:
  `fil expand` must report how many works in a run had no reference list.
- ~~**Read `X-RateLimit-Remaining` from responses** rather than trusting a constant compiled into
  the binary.~~ **Done in step 6** — `httpx.Client.Quota`.
- **Build M4's candidate generation before its semantic search** (D13).

---

## Open questions

| | Question | Blocking? |
| --- | --- | --- |
| 1 | ~~**Licence**~~ — **decided: Apache-2.0** (D15) | Closed |
| 2 | ~~**Zotero**~~ — **decided: not in M3** (D19); later via a BibTeX or CSL-JSON export | Closed for M3 |
| 3 | ~~**PDF text extraction**~~ (ADR-009) — **decided: PDFium as WebAssembly** (D20, spike 10); built in M3 step 7 | Closed |
| 4 | **`sqlite-vec` revisit** (D13) — only if a pre-filtered query misses target, and only after checking whether it is a real ANN index or just a faster scan | No. Not before M4 |
| 5 | **Who maintains this after the degree?** | No, but it changes how much to invest in docs and tests now |

---

## Risk register changes

Two risks were promoted or added on the strength of measurement:

- **New, High** — the humanities coverage gap (D12). Previously unknown; now quantified at 84%.
- **New, Medium** — vector search has no ANN index (D13). Mitigated by design rather than by a
  dependency change.
- **Revised** — `sqlite-vec` pre-1.0 risk is no longer carried, because the dependency is no
  longer planned.
- **Revised** — OpenAlex allowance is 100× smaller than documented, but batching keeps usage at
  well under 1% of it.

Unchanged and still the largest: **scope is large for one person at ten hours a week.** Every
milestone is built to ship something usable, so an interruption leaves a working tool rather than
a half-finished one.
