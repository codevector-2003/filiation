# Project status

**Date:** 24 September 2026 · **Position:** **M0 complete** · M1 in progress — steps 1–4 of 6
**Phase 1 target:** v0.1 by 14 November 2026

> **In one line:** M0 is done, and M1's expander works live — `fil expand` grew one seed to 525
> fetched works and 11,802 citations, with no duplicate IDs or DOIs. Title-level duplicates
> (~4%) are reported by `fil stats --duplicates` and never merged on a guess (D14).
> The schedule has slack: §10 planned the expander for 13–26 October.

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
| 1 | M1 — budgeted expansion, export, **first release** | **In progress** — expander and `fil expand` done, verified live |
| 2 | M2 — MCP server | Not started |
| 3 | M3 — PDFs, text, citation context, FTS5 | Not started |
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

### M1, in progress

*Done when:* one seed gives a clean 500-node graph with no duplicates, and it opens in Gephi.

| Order | Work | State |
| --- | --- | --- |
| 1 | `store`: best-first frontier query, merged-record folding | **Done** |
| 2 | `graph.Expand`: the budgeted loop, with §8's resume, idempotency, budget, cycle and single-writer tests | **Done** |
| 3 | `library.Expand` and `fil expand`, with progress and a coverage report | **Done** — verified live |
| 4 | Title-level duplicates: reported by `fil stats --duplicates`, never merged (D14) | **Done** |
| 5 | `fil neighbours`, `fil path` (recursive CTEs with a visited set — the graph is not a DAG). `fil stats` landed with step 4 | **Next** |
| 6 | GraphML export, then GoReleaser, the multi-field validation run, and **v0.1** | |

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
| 1 | **Licence** — MIT or Apache-2.0 for adoption, AGPL to stop paid rehosting | No. No dependency has forced it; all four are permissive |
| 2 | **Zotero** — read from an existing library? Cheapest route to real users | Not yet, but decide before M3 |
| 3 | **PDF text extraction** (ADR-009) — pure Go, bundled `pdftotext`, or external | No. Deliberately deferred to a Phase 3 bake-off |
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
