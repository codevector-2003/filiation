# Project status

**Date:** 24 September 2026 · **Position:** M0 in progress — 4 of 9 packages done
**Phase 1 target:** v0.1 by 14 November 2026

> **In one line:** Phase 0 is closed — the design survived contact with the API, but three of its
> numbers did not. M0 is building inward-out and the riskiest package is now behind it:
> `internal/store` holds both of spike 5's silent failures and both are handled, so what remains
> before `fil add` works is fetching rather than storing.

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
| 1 | M0 — skeleton, `fil add` writes one node | **In progress** — steps 1–4 of 9 |
| 1 | M1 — budgeted expansion, export, **first release** | Not started |
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

`internal/errs` holds four sentinels — `ErrNotFound`, `ErrTransient`, `ErrUnresolved`,
`ErrAmbiguous` — and no logic. `ErrNotFound` (this library has no such row) and `ErrUnresolved`
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

**Dependencies, all licence-checked before adding.** `ncruces/go-sqlite3` MIT ·
`go-sqlite3-wasm/v3` MIT-0 · `julianday` MIT · `golang.org/x/sys` BSD-3 · `BurntSushi/toml` MIT.
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
| 5 | **`internal/identity`** | DOI, arXiv, OpenAlex ID, PMID, URL, title. **Title search never auto-accepts** (ADR-005). It also owns normalising the OpenAlex URL form, which `store` refuses outright | **Next** |
| 6 | `internal/httpx` | 5 req/s token bucket, `mailto`, backoff honouring `Retry-After`, response cache in a separate file | |
| 7 | `internal/sources/openalex` | `GetWork`, `GetWorksBatch` (**chunks of 100**), `SearchByTitle` | |
| 8 | `internal/library` | `Add` — resolve, hydrate seed, record edges and stubs | |
| 9 | `cmd/fil` | cobra wiring, plus the lint rule forbidding front doors from importing `store` | |

**The two gotchas from spike 5 are handled**, both in `internal/store`, and both were silent
failures if missed. They stay written down because a future connection opened anywhere else has to
obey the same rules:

- FTS5 and vec1 are registered with `sqlite3.AutoExtension` **before** either handle is opened. A
  connection without the extension cannot even read a table created with it.
- `PRAGMA foreign_keys` is **per connection**, not stored in the file, so it is set in the DSN of
  every connection in both the read pool and the write handle. The one in `schema.sql` only ever
  bound the connection that applied it.

**M0 is done when** `fil add 10.1145/3292500` writes a row and prints the title, running it twice
adds nothing the second time, and the seed's ~40–100 references are present as stubs with edges.

### Then M1

Budgeted best-first expansion, dedup, `expand` / `neighbours` / `path`, GraphML export, and the
first cross-compiled release. Write the idempotency and resume tests *before* `internal/graph`,
not after — `ARCHITECTURE_PHASE1.md` §8 names them as the two that will actually catch
regressions.

### Carry forward from the spikes

- **Surface reference coverage per node** (D12) — the same way OA status is surfaced. `fil add`
  and `fil expand` should report how many works in a result have no reference list, and the README
  should state the field limitation before someone discovers it by installing the tool.
- **Read `X-RateLimit-Remaining` from responses** rather than trusting a constant compiled into
  the binary. OpenAlex has clearly changed its pricing model once already.
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
