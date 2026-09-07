# Phase 1 architecture — the core graph

**Covers:** milestones M0 and M1 · 15 Sept – 14 Nov 2026 · ships as **v0.1**
**Language:** Go — see D8 in `DECISIONS.md`
**Status:** Proposed
**Date:** 29 August 2026

---

## 1. What Phase 1 must do

| | |
| --- | --- |
| **In** | Resolve an identifier to a paper. Fetch its metadata from OpenAlex. Walk its references. Expand outward under a budget. Deduplicate versions of the same work. Export the graph. |
| **Out** | PDFs, full text, embeddings, retrieval, MCP, web UI. All later phases. |
| **Done when** | 20 real papers from 5 fields expand into a clean graph — zero duplicate works, no crashes, and the Gephi export looks right. |

### Non-functional requirements

- **Setup:** download one binary and run it. No runtime, no server, no Docker, no API key.
- **Interruptible:** Ctrl-C at any point must leave a consistent database, and re-running must continue rather than restart.
- **Offline-tolerant:** losing the network mid-run is a pause, not a corruption.
- **Budget-bounded:** the user says how big the graph may get, and that limit is respected exactly.
- **Cheap:** a typical expansion should cost a small fraction of the daily API allowance.

---

## 2. Component map

```
cmd/fil/main.go         wires cobra to internal/library — no logic
internal/
├── library/            ★ the core. add, expand, path, neighbours, stats
├── config/             library path, contact email, budgets (flag > env > file > default)
├── identity/           normalise and classify: DOI, arXiv, OpenAlex ID, PMID, title
├── model/              Work, Edge, FrontierItem, ExpansionResult
├── store/              ★ ALL SQL. schema.sql embedded with //go:embed. The swap seam
├── httpx/              shared client, rate.Limiter token bucket, retry, response cache
├── sources/
│   ├── source.go       the Source interface — so Crossref/arXiv slot in later
│   └── openalex/       GetWork, GetWorksBatch, SearchByTitle
├── graph/              ★ the budgeted traversal. The heart of Phase 1
├── export/             GraphML, JSON, BibTeX
└── errs/               sentinel errors so the CLI can print something useful
```

**Dependency direction is strictly one way:**

```
cli → expand → { store, sources } → http
       ↑
    identity, config, models  (leaves — depend on nothing internal)
```

`internal/store` never imports `internal/sources`. `internal/sources` never imports `internal/store`. Only `internal/graph` knows about both. This is what keeps the storage backend swappable later without a rewrite. Go's `internal/` convention keeps packages private to the module but does **not** enforce this direction — a `golangci-lint` import rule can.

---

## 3. The cost model

**Measured 30 August 2026 — the original assumptions in this section were wrong in both
directions. Full evidence in [`SPIKES.md`](SPIKES.md).**

| Operation | Originally assumed | **Measured** | Works returned | Credits per work |
| --- | --- | --- | --- | --- |
| Single work by ID | 1 credit | **0 — free** | 1 | **0.000** |
| List request, any `per_page` 1–200 | 10 credits | **1 credit** | up to 200 | **0.005** |
| Daily allowance | 100,000 | **1,000** ($0.10) | | |

At most **100 IDs** may be piped into a filter (101 returns a hard 400), so hydration batches at
100 even though a page could hold 200.

`referenced_works` comes back **inside the work object**, complete and never truncated — verified
across 50 works and 5,389 edges. Fetching a paper gives you its outgoing edges with no extra call.
This single fact still shapes the whole algorithm.

### What batching is actually for

Credits are no longer the reason. Single fetches are free, and a 500-node graph costs 5 credits
out of 1,000 — half a percent of a day. The reason is **round trips**: the first 429 appears
around 11 req/s, so 500 single fetches is ~50 seconds of wall clock and 500 chances to be
throttled, against 5 requests and under a second batched.

So the ADR-002 conclusion holds and its stated rationale does not. Batch because it is fast and
polite, not because it is cheap.

### What expansion actually costs

Measured branching is **70–100 references per work in STEM**, not the ~40 originally assumed.

| Reach | Works | List calls | Credits | Verdict |
| --- | --- | --- | --- | --- |
| Depth 1 | ~70 | 1 | 1 | Trivial |
| Depth 2 | ~5,000 | 50 | 50 | Comfortable |
| Depth 3 | ~350,000 | 3,500 | 3,500 | **Exceeds the daily allowance, and nobody wants this graph** |

**Depth is not the control. The node budget is.** With the corrected branching factor, depth 3 is
now the first thing in this design that would actually hit a hard limit — which only sharpens the
original point. The user asks for 500 useful nodes, not three hops.

### The constraint that replaced credits

The token bucket in ADR-004 is now the only thing between the tool and a 429. Set it to **5 req/s**
against the ~11 req/s where throttling was first observed.

---

## 4. The expansion algorithm

### The key idea: edges before nodes

When you fetch a work, you get the OpenAlex IDs of everything it references. You can write those edges into the database **before** you know anything else about the targets. So a work row exists in one of two states:

- **stub** — the ID is known, nothing else is (`hydrated = 0`)
- **hydrated** — title, year, venue, authors are filled in (`hydrated = 1`)

Two things fall out of this, and both are worth more than they cost:

1. **Deduplication becomes free.** "Connect to the existing node if the paper is already there" is just `INSERT ... ON CONFLICT DO NOTHING` against a primary key. There is no matching logic to get wrong.
2. **Relevance scoring becomes free.** A paper cited by 5 papers you already have is far more interesting than one cited by 1. That in-graph in-degree is computable from edges you already stored, with **zero API calls**. It is the best ranking signal available and it costs nothing.

### Pseudocode

```go
func (e *Expander) Expand(ctx context.Context, seedID string, o Opts) error {
    if err := e.hydrate(ctx, seedID, 0); err != nil {     // 1 credit
        return err
    }
    e.recordEdgesAndStubs(ctx, seedID, o.MaxRefsPerWork)  // free: from referenced_works

    spent := 1
    for spent < o.MaxNodes {
        if err := ctx.Err(); err != nil {                 // cancellation must actually stop
            return err
        }

        // Best-first, not breadth-first. One indexed query.
        batch, err := e.store.NextFrontier(ctx, 50, o.MaxDepth)
        if err != nil || len(batch) == 0 {
            return err
        }

        works, err := e.src.GetWorksBatch(ctx, batch)     // 10 credits for up to 50
        if err != nil {
            if errors.Is(err, errs.Transient) {
                continue                                   // batch failed; run continues
            }
            return err
        }

        // One transaction per batch: Ctrl-C loses at most 50 works.
        err = e.store.Tx(ctx, func(tx *store.Tx) error {
            for _, w := range works {
                if err := tx.Hydrate(w); err != nil {
                    return err
                }
                if w.Depth < o.MaxDepth {
                    if err := tx.RecordEdgesAndStubs(w, o.MaxRefsPerWork); err != nil {
                        return err
                    }
                }
            }
            return nil
        })
        if err != nil {
            return err
        }
        spent += len(works)
    }
    return nil
}
```

### Why each choice

- **Best-first, not breadth-first.** Breadth-first spends the budget on whatever happens to be near the seed. Best-first spends it on what the graph itself says is important. Same cost, better graph.
- **One transaction per batch.** Ctrl-C loses at most 50 works of progress and never leaves a half-written graph.
- **`hydrated` and `fetched_refs` are persisted per work.** Re-running `expand` continues from the frontier instead of starting over. Resume is free — it is just the same query returning the remaining stubs.
- **`max_refs_per_work`.** A review article can cite 800 papers and would eat the entire budget alone. Record *all* its edges — they arrive free in the response — but let it contribute at most N candidates to the frontier.

---

## 5. Architecture decision records

### ADR-001: One SQL module, no ORM

**Status:** Proposed · **Date:** 29 Aug 2026

**Context.** The product plan commits to "local first, optional server later," which needs a seam where the storage backend can be replaced. But wrapping SQLite in an abstraction on day one is a well-known way to pay an ongoing tax for a swap that may never happen.

**Decision.** Put every SQL statement in `internal/store` as plain functions over `database/sql`. No ORM, no repository interface. The package boundary *is* the seam.

**Options considered**

| Option | Complexity | Swap cost later | Speed now | Verdict |
| --- | --- | --- | --- | --- |
| A. `database/sql` calls scattered through the codebase | Low | Very high | Fastest | Rejected |
| **B. One `internal/store` package, plain functions** | **Low** | **Moderate** | **Fast** | **Chosen** |
| C. `sqlc` — generate typed Go from SQL | Low–medium | Moderate | Fast after setup | Escape hatch |
| D. GORM or another ORM | Medium | Low | Slower, and hides the SQL | Rejected |

**Trade-off.** Hand-written `database/sql` means writing `rows.Scan` boilerplate for every query, which is the least pleasant part of Go. `sqlc` removes exactly that while keeping SQL as the source of truth — adopt it the moment scan code starts causing bugs, not before. An ORM would hide the recursive CTEs that are the interesting part of this codebase.

**Consequences**
- Easier: writing exactly the SQL you want, including recursive CTEs for path finding, which ORMs make awkward.
- Harder: `rows.Scan` boilerplate. The eventual Postgres port is a rewrite of one package rather than a config change.
- Revisit when: server mode is actually scheduled, not before.

---

### ADR-002: Budgeted best-first expansion with batched hydration

**Status:** Proposed · **Date:** 29 Aug 2026

**Context.** One paper has ~40 references. Depth 2 is ~1,600 papers and depth 3 is ~64,000. Naive traversal either produces an unusable graph or burns the API allowance, and single-work fetches cost 5× what batched fetches cost.

**Decision.** Expansion takes a **node budget** as its primary control, with depth as a secondary guard. The frontier is a priority queue ordered by in-graph in-degree, and hydration happens in batches of up to 50 IDs per list request.

**Options considered**

| Option | Cost | Graph quality | Complexity |
| --- | --- | --- | --- |
| A. Depth-limited BFS, one fetch per work | 5× higher | Poor — spends budget on whatever is nearest | Low |
| B. Depth-limited BFS, batched fetches | Baseline | Poor for the same reason | Low |
| **C. Budgeted best-first, batched fetches** | **Baseline** | **Good — budget goes to what the graph says matters** | **Medium** |
| D. C plus a learned relevance model | Baseline + training | Unknown | High |

**Trade-off.** C costs one ordered query per batch over B, and the ranking signal it needs is already in the database for free. D is a Phase 6 conversation at the earliest — graph maths first, machine learning only if maths is not enough.

**Consequences**
- Easier: the user controls graph size directly and predictably.
- Harder: `next_frontier` needs a good index or it becomes the bottleneck as the graph grows.
- Revisit when: users say the selected papers feel wrong. That is a scoring change, not a structural one.

---

### ADR-003: Stub nodes — record edges before hydrating targets

**Status:** Proposed · **Date:** 29 Aug 2026

**Context.** `referenced_works` arrives inside the work object as a list of OpenAlex IDs. The obvious approach is to fetch each target before writing the edge, so that no edge points at a row that does not exist.

**Decision.** Insert an unhydrated stub row for every referenced ID immediately, then the edges, then hydrate the stubs later in batches under the budget.

**Options considered**

| Option | API cost | Dedup | Scoring signal |
| --- | --- | --- | --- |
| A. Fetch target, then write edge | High — forces per-work fetches | Same | Unavailable until fetched |
| B. Buffer edges in memory, write after hydration | Baseline | Same | Lost on crash |
| **C. Stub rows first, hydrate later** | **Baseline** | **Free, via primary key** | **Available immediately and free** |

**Trade-off.** C means the database contains rows that are mostly empty, which must be handled everywhere a work is displayed or exported. That is a real cost and it is worth paying, because it is what makes both deduplication and relevance scoring free.

**Consequences**
- Easier: dedup is a primary key constraint. In-graph in-degree needs no API calls. Resume works naturally.
- Harder: every read path must handle a work with no title. The CLI and exporter must filter or label stubs, never crash on them.
- **Requires a schema change** — see §6.

---

### ADR-004: Polite pool, token bucket, and an on-disk response cache

**Status:** Proposed · **Date:** 29 Aug 2026

**Context.** OpenAlex needs no API key, but the anonymous pool is far slower than the polite pool, which only requires sending a contact email. Published rate limits are also inconsistent across the documentation — which means they must be measured, not assumed. Separately, during development the same expansion gets re-run dozens of times while debugging, and re-hitting the API each time is slow and rude.

> **Measured 30 Aug 2026 (spike 3, [`SPIKES.md`](SPIKES.md)).** Two corrections to the context above.
> **The bucket rate is 5 req/s** — the first 429 appeared at an achieved ~11 req/s, so neither
> published figure (10/s, 100/s) is usable as stated. And **the polite-pool claim did not
> reproduce**: limit headers were byte-identical with and without `mailto`. Keep sending it, because
> it is what OpenAlex asks for and it is how they reach you, but no part of the design may assume a
> throughput benefit from it.

**Decision.** One `internal/httpx` package owning: a shared `*http.Client` whose `RoundTripper`
always adds `mailto`, a `golang.org/x/time/rate.Limiter` token bucket set well below the measured
limit, exponential backoff with jitter on 429 and 5xx that respects `Retry-After`, and an on-disk
response cache keyed by full URL.

`rate.Limiter` is exactly this shape already — do not hand-roll one.

The cache lives in a **separate SQLite file** from the library, so it can be deleted at any time without touching the user's data.

**Options considered**

| Option | Dev speed | Correctness risk | Complexity |
| --- | --- | --- | --- |
| A. Plain httpx, `time.sleep` between calls | Slow — refetches everything | Rate-limit bans | Lowest |
| B. httpx plus an HTTP caching library | Fast | Cache semantics you do not control | Low |
| **C. httpx plus token bucket plus own URL cache** | **Fast** | **Explicit, inspectable** | **Low–medium** |

**Trade-off.** B is less code but couples cache correctness to another library's interpretation of HTTP headers, and OpenAlex's cache headers are not the thing being optimised for here — the goal is "do not refetch during a debugging loop." C is about thirty lines and fully under control.

**Consequences**
- Easier: fast iteration; deterministic replays; one place to change when the real rate limit is measured.
- Harder: a stale cache can hide a bug. Ship `--no-cache` and `fil cache clear` from day one.
- Revisit when: the measured rate limit differs from the assumed one, in the week-1 spike.

---

### ADR-005: Never silently accept a title search result

**Status:** Proposed · **Date:** 29 Aug 2026

**Context.** Input arrives as a DOI in five formats, an arXiv ID in two generations, a bare OpenAlex ID, a URL, a PMID, or a title typed from memory. The first five resolve deterministically. Title search does not.

**Decision.** `internal/identity` classifies and normalises input. Deterministic identifiers resolve silently. A **title search returns candidates and requires confirmation** — interactively in the CLI, and by an explicit `--accept-first` flag in scripts.

**Trade-off.** One extra keystroke against a wrong paper silently seeding an entire graph. A wrong seed is not a small error; it poisons everything expanded from it, and the user may not notice for weeks.

**Consequences**
- Easier: trust in what is in the library.
- Harder: scripted bulk import needs the explicit flag.

---

### ADR-006: One library per user, not one per directory

**Status:** Proposed · **Date:** 29 Aug 2026

**Context.** The whole product thesis is a library that accumulates over years. A database that lives in the current working directory produces a scattering of half-built graphs.

**Decision.** The library defaults to a per-user application data directory. Override order: `--db` flag, then `FILIATION_DB` environment variable, then config file, then the default.

> **Implemented, 7 Sept 2026.** `os.UserConfigDir` alone; **`adrg/xdg` was not taken** — stdlib is already correct on Windows, macOS and Linux, and xdg only earns its place if the full spec (separate data, cache and state directories) is ever needed. Precedence is applied per field, not per source. On first run the CLI **offers the user a choice of location** and records it with `config.Save`; the config package itself never prompts, because it must also work from an MCP server and a web handler.

**Consequences**
- Easier: `fil add` does the right thing from any directory. Accumulation happens by default.
- Harder: tests must always pass an explicit path, and the default location differs per OS — the CLI must print where the library is on first run, or users will not find it.

---

### ADR-007: SQLite driver is `ncruces/go-sqlite3`, and no cgo anywhere

**Status:** Proposed · **Date:** 29 Aug 2026

**Context.** The entire reason for choosing Go is that a researcher downloads one file and runs
it. The standard Go SQLite driver (`mattn/go-sqlite3`) uses cgo, which makes cross-compilation
painful and drags in a C toolchain.

**Decision.** Use `ncruces/go-sqlite3`, a WASM build of SQLite translated to Go. No cgo. The
official sqlite-vec Go bindings target this driver specifically, so graph, keyword and vector
search all stay in one static binary.

**Options considered**

| Option | cgo | Cross-compile | sqlite-vec | Speed |
| --- | --- | --- | --- | --- |
| `mattn/go-sqlite3` | Yes | Painful | Manual extension load | Fastest |
| `modernc.org/sqlite` | No | Clean | Not the bindings' target | Good |
| **`ncruces/go-sqlite3`** | **No** | **Clean** | **Official bindings** | **Good** |

**Trade-off.** WASM SQLite is slower than the C build. At this workload — point lookups and
bounded traversals over a few million rows — the difference is not the bottleneck. Distribution
is worth more than the margin.

**Consequences**
- Easier: `GOOS=windows go build` produces a working `.exe` from a Linux machine.
- Harder: **FTS5 availability must be verified** (spike 5). M3 keyword search depends on it.
- **Rule:** no dependency may require cgo. This constrains every future library choice.

---

### ADR-008: Embeddings and generation both go through Ollama

**Status:** Proposed · **Date:** 29 Aug 2026

**Context.** Go has no mature in-process embedding stack. The available options are community
ONNX bindings, or calling out to a local service.

**Decision.** `internal/embed` and `internal/llm` are both thin HTTP clients for Ollama. The
interface is one method each, so a different backend can replace either later.

**Trade-off, stated honestly.** This is the price of choosing Go. Semantic search now requires
the user to install Ollama, which weakens the one-command promise that motivated the language
choice in the first place. It is survivable only because the degradation ladder already handles
the absence: without Ollama, keyword search and the whole graph still work.

**Consequences**
- Easier: the binary stays static; swapping models is a config line.
- Harder: **the first-run experience must explain this clearly**, not fail confusingly. Detect
  Ollama, and if it is missing say what works without it and what installing it would add.
- Revisit when: users report the Ollama install as the reason they stopped using the tool.
  A pure-Go ONNX path would restore true one-command setup.

---

### ADR-009: PDF text extraction is deferred to a Phase 3 spike

**Status:** Open · **Date:** 29 Aug 2026

**Context.** Scientific PDFs are two-column, full of ligatures, and their reference sections need
layout awareness. Python and Java own this problem; Go's libraries are markedly weaker. This is
the single biggest known weakness of the language choice.

**Decision.** Do not decide yet. Phase 1 does not touch PDFs. Before M3, run a bake-off on 20
real papers across 5 fields:

1. A pure-Go library — keeps the single binary, likely lowest quality
2. Bundling `pdftotext` from Poppler — good quality, but per-platform binaries to ship
3. An optional external service the user already has

**Why deferred rather than guessed.** Choosing now means choosing without evidence, and the
answer depends on quality thresholds that only become visible once retrieval exists. Recorded
here so it is a scheduled decision rather than a surprise.

---

## 6. Schema changes this design requires

The current `schema.sql` assumes a work is fully known when inserted. ADR-003 breaks that. Apply before writing the `store` package:

```sql
-- work.title must allow NULL: a stub has an ID and nothing else
title TEXT,                      -- was: TEXT NOT NULL

-- new columns
hydrated   INTEGER DEFAULT 0,    -- 0 = stub, 1 = metadata fetched
depth      INTEGER,              -- hops from the nearest seed
is_seed    INTEGER DEFAULT 0,    -- explicitly added by the user
unresolved INTEGER DEFAULT 0,    -- OpenAlex has no record; stop retrying
```

And the index that makes frontier selection cheap. Without it, `next_frontier` degrades badly once the graph passes a few thousand nodes:

```sql
CREATE INDEX IF NOT EXISTS idx_work_frontier
    ON work(hydrated, depth) WHERE hydrated = 0;
```

In-graph in-degree is computed from `cites` at query time in Phase 1. If it becomes slow, denormalise it into a counter column — but measure first.

---

## 7. Failure modes

| What happens | Response |
| --- | --- |
| HTTP 429 | Honour `Retry-After`, exponential backoff with jitter. The token bucket should make this rare; if it is not rare, the bucket is set wrong |
| HTTP 5xx | Retry three times, then mark the batch failed and continue. One bad batch must not kill a 500-node run |
| Network disappears mid-run | Fail with a clear message. State is already committed per batch, so `expand` resumes |
| Ctrl-C | Transaction per batch means the database is always consistent |
| Work not in OpenAlex | Set `unresolved = 1`. The edge stays — it is still true that the paper was cited — but stop retrying |
| Review article with 800 references | Record every edge; cap frontier contributions at `max_refs_per_work` |
| Citation cycle | Real, though rare (simultaneous publication, corrections). **The graph is not a DAG.** Path finding and traversal must carry a visited set. Never assume acyclic |
| Budget exhausted mid-expansion | Normal, not an error. Report what was reached and how to continue |

---

## 8. Test strategy

**No network in any unit test.** Record real OpenAlex responses once into `testdata/` — Go's conventional name, and the toolchain ignores it — and replay them through a stub `http.RoundTripper`. Table-driven tests throughout.

| Test | Asserts |
| --- | --- |
| Golden expansion | A fixed seed produces an exact node and edge count |
| Single writer | Concurrent reads during an expansion never produce `database is locked` |
| Budget respected | `MaxNodes=100` never produces 101 hydrated works |
| Idempotent | Running `Expand` twice adds nothing the second time |
| Dedup | The same paper reached by DOI, arXiv ID and OpenAlex ID yields one row |
| Resume | Cancel the context after two batches, re-run, end state equals the uninterrupted run |
| Cycle safety | A hand-built cyclic fixture does not hang path finding |
| Stub handling | Export and display work on a graph that is 90% stubs |

The idempotency and resume tests are the two that will actually catch regressions. Write them early.

---

## 9. Run these spikes in week 1, before writing real code

The cost model rests on assumptions the documentation contradicts itself about. Each of these is an hour, and each can invalidate a design choice.

**Status: 1, 2, 3, 4 and 7 were run on 30 August 2026. Results and evidence in
[`SPIKES.md`](SPIKES.md); §3 and ADR-004 above have been corrected from them.**

1. ✅ **Batch fetch by OpenAlex ID.** Does filtering works by a pipe-separated list of OpenAlex IDs work, and is the ceiling 50 or 100 values? Sources disagree. If batch-by-ID is unsupported, ADR-002's cost model collapses and the fallback is batching by DOI — which fails for works without one.
   → **Works. Ceiling is exactly 100; 101 is a hard 400. `referenced_works` arrives complete in
   batched responses, zero truncation across 5,389 edges.**
2. ✅ **Real `per_page` maximum.** Documented as both 100 and 200.
   → **200, with an explicit error above it. Not the binding constraint — the 100-ID filter is.**
3. ✅ **Real sustained request rate with `mailto` set.** Documented as 10/second in one place and 100/second in another. Set the token bucket from what you measure, not what you read.
   → **First 429 at ~11 req/s; bucket set to 5 req/s. Separately discovered the real cost model:
   single fetches free, list requests 1 credit, 1,000/day. `mailto` changed nothing measurable.**
4. ✅ **Reference coverage across fields.** Take 20 papers from 5 fields and check what fraction have a complete `referenced_works` list. **This is the biggest unknown in the entire product**, not just this phase — OpenAlex reference coverage depends on what publishers deposit. If some field is sparse, the graph is thin there and you need to know that in September, not in March.
   → **Answered, and it is a product constraint. Frontier dead ends: Medicine 6%, Physics 12%,
   CS 15%, Social Sciences 54%, Arts and Humanities 84%. The tool works in STEM. In the
   humanities the graph stops after one hop, and the interface has to say so.**

5. ✅ **Does `ncruces/go-sqlite3` ship FTS5?** *(Go-specific)* M3 keyword search depends on it. If
   it does not, you need either a build that includes it or an external index — and that is worth
   knowing in September, not December.
   → **Yes, but not compiled in — it loads as a WASM extension via
   `sqlite3.AutoExtension(fts5.Register)`. This must be wired into `store.Open`: a connection
   without it cannot read a table created with it. The real schema then applies cleanly, and
   multi-statement `Exec` is supported.**
6. ⚠️ **sqlite-vec with that driver, end to end.** Create a `vec0` table, insert vectors, run a
   query. Prove the WASM pairing works before designing M4 around it.
   → **Tested SQLite's own `ext/vec1` instead, already in the dependency tree. Exactly correct
   results, but version 0.7 has no ANN index — only `none` or `flat`, and flat is a brute-force
   scan at ~66 µs/vector (~3 s for a 50,000-chunk library, against a 500 ms target). Not a
   blocker: M4's hybrid design pre-filters by graph and FTS5 first, and a scan over a few thousand
   candidates is inside budget. Revisit `sqlite-vec` only with a measured number, and check
   whether it is a real ANN index or just a faster scan.**
7. ✅ **Cross-compile from day one.** `GOOS=windows`, `GOOS=darwin`, `GOOS=linux`. If something
   drags in cgo, you want to find out in week one, not the week you plan to release.
   → **All three build from Windows with `CGO_ENABLED=0`, ~2.3 MB each. D9 holds.**

Spike 1 is a product question wearing an engineering costume. Do it first.

---

## 10. Week by week

| Week | Dates | Work |
| --- | --- | --- |
| 1 | 15–21 Sept | Spikes 1–4 (OpenAlex) and 5–7 (Go). Module layout, `go.mod`, cross-compile proof |
| 2 | 22–28 Sept | `config`, embedded `schema.sql`, `store` skeleton with the §6 changes |
| 3 | 29 Sept – 5 Oct | `identity`, `httpx` with `rate.Limiter` and cache |
| 4 | 6–12 Oct | `sources/openalex`, then `fil add` end to end. **M0 complete** |
| 5–6 | 13–26 Oct | `graph`: frontier query, batching, budget, resume. The core of the phase |
| 7 | 27 Oct – 2 Nov | `export`, `fil path`, `fil neighbours`, `fil stats` |
| 8 | 3–9 Nov | The 20-paper / 5-field validation run. Fix what breaks |
| 8.5 | 10–14 Nov | GoReleaser, cross-compiled binaries, tag **v0.1** |

Weeks 5 and 6 hold the only genuinely hard code in this phase. Everything before is plumbing and
everything after is a thin layer over queries. If you slip, slip weeks 1–4 and protect 5–6.

Two extra weeks over the original plan, for learning Go while building. That is a real cost and
pretending otherwise would only move the slip later.

---

## 11. Action items

1. [ ] Run spike 4 (reference coverage across fields) — it can change the product, not just the code
2. [ ] Run spikes 1–3 and write the measured numbers into this document
3. [ ] Apply the §6 schema changes to `schema.sql`
4. [ ] Decide the default `MaxNodes`. Suggestion: 500 — large enough to be a map, small enough to finish in under a minute
5. [ ] Write the idempotency and resume tests before writing `internal/graph`, not after
6. [ ] Print the library path on first run, per ADR-006
7. [ ] Add a `golangci-lint` import rule forbidding `cmd/` and front doors from importing `internal/store`
8. [ ] Set up GoReleaser in week 1, not week 8 — a broken cross-compile found late is expensive
