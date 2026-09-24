# Spike results

Measured answers to the questions `ARCHITECTURE_PHASE1.md` §9 says must be settled before the
design is trusted. Every number here came from running code against the live API, not from
reading documentation — the documentation is what disagreed with itself in the first place.

**Run:** 30 August 2026 · OpenAlex API · Go 1.27.0 · code under `spikes/`

> **Headline:** the cost model in `ARCHITECTURE_PHASE1.md` §3 was wrong in both directions.
> Single-work fetches are **free**, not 1 credit. List requests cost **1** credit, not 10. The
> daily allowance is **1,000**, not 100,000. Batching is still correct, but for a different
> reason than the one written down.

---

## Spike 1 — Batch fetch by OpenAlex ID · **PASS, better than assumed**

`go run ./spikes/1-batch-by-id`

Both filter spellings work and are equivalent:

| Filter key | IDs sent | HTTP | meta.count | Verdict |
| --- | --- | --- | --- | --- |
| `openalex_id` | 25 | 200 | 25 | all returned |
| `ids.openalex` | 25 | 200 | 25 | all returned |

The ceiling is exactly **100 piped values**, and it fails loudly rather than silently truncating:

| IDs sent | HTTP | meta.count | Verdict |
| --- | --- | --- | --- |
| 50 | 200 | 50 | all returned |
| **100** | **200** | **100** | **all returned — the ceiling** |
| 101 | 400 | — | rejected |
| 150 | 400 | — | rejected |

**References arrive complete inside a batched list response.** 50 works returned 5,389 edges,
mean 107.8 refs/work, and `len(referenced_works) == referenced_works_count` for every single one.
Zero truncation. The premise the whole expansion algorithm rests on — that edges are free once
you have the work — holds.

**Consequence for ADR-002:** batch size goes from the assumed 50 to **100**. Failing loudly at
101 means the batcher needs a hard chunk size, not a hopeful one.

---

## Spike 2 — Real `per_page` maximum · **200**

`go run ./spikes/2-per-page`

50, 100, 150 and 200 all return exactly what was asked for. 201 and 500 are rejected with
`{"error":"Pagination error.","message":"per-page parameter must be between 1 and 200."}`.

**But `per_page` is not the binding constraint** — the 100-ID filter ceiling from spike 1 is.
There is no way to ask for more than 100 specific works at once, so hydration batches at 100 and
the extra 100 slots of page capacity go unused. `per_page=200` is only useful for unfiltered
listing, which this tool does not do.

---

## Spike 3 — Rate limit and the real cost model · **the big one**

`go run ./spikes/3-rate-limit`

### Rate

| Target rate | Sent | 2xx | 429 | Achieved req/s | p50 latency |
| --- | --- | --- | --- | --- | --- |
| 5/s | 10 | 10 | 0 | 3.3 | 1.188s |
| 10/s | 20 | 20 | 0 | 7.5 | 353ms |
| 25/s | 18 | 16 | **2** | 11.1 | 385ms |
| 50/s | not attempted — the run aborts on the first 429 | | | | |

**First 429 at an achieved ~11 req/s.** Neither documented figure (10/s or 100/s) is right as
stated: 100/s is plainly wrong, and 10/s is about where trouble starts rather than a safe
ceiling.

### The cost model, measured

OpenAlex returns rate-limit headers that the design docs never accounted for:

```
X-RateLimit-Limit: 1000          X-RateLimit-Limit-USD: 0.1
X-RateLimit-Remaining: 983       X-RateLimit-Remaining-USD: 0.0983
X-RateLimit-Credits-Used: 1      X-RateLimit-Cost-USD: 0.0001
X-RateLimit-Reset: 73233         (seconds — a rolling ~24h window)
```

Credits are USD-denominated at $0.0001 each, and the free allowance is **$0.10/day = 1,000
credits/day**. Measured by watching `X-RateLimit-Remaining` across controlled runs:

| Operation | §3 assumed | **Measured** | Works returned | Credits/work |
| --- | --- | --- | --- | --- |
| Single work by ID | 1 credit | **0 — free** | 1 | **0.000** |
| List request, any `per_page` 1–200 | 10 credits | **1 credit** | up to 200 | **0.005** |
| Daily allowance | 100,000 | **1,000** | | |

Verified directly: 20 successful single-work fetches moved `Remaining` by **0**. Five
`per_page=200` list requests moved it by exactly **5** — the cost is per request, flat,
regardless of page size.

### What this changes

- **Credits are no longer the constraint.** A 500-node graph costs **5 credits** — half a percent
  of a day's allowance. The daily ceiling is 1,000 list requests, which at 100 IDs each is
  100,000 works/day. Nobody will hit it.
- **Batching is still right, for a different reason.** The justification in ADR-002 is "5× cheaper
  per work." That is now wrong — single fetches are free. The real justification is **round trips**:
  at ~10 req/s, hydrating 500 works one at a time takes ~50 seconds of wall clock, against 5
  requests and well under a second batched. Batching is a latency and politeness decision now, not
  a cost one.
- **The token bucket matters more than the credit budget.** ADR-004's `rate.Limiter` is now the
  only thing standing between the tool and a 429. Set it to **5 req/s**, comfortably under the
  observed ~11 where the first 429 appeared.

### `mailto` made no measurable difference

Identical limit headers with and without it (`Limit: 1000`, `Remaining: 983` both ways). ADR-004's
claim that "the anonymous pool is far slower than the polite pool" was **not reproduced**. Keep
sending it — it is what OpenAlex asks for, it costs nothing, and it is how they reach you if the
tool misbehaves — but do not expect a throughput benefit from it, and do not build a design
around one.

---

## Spike 4 — Reference coverage across fields · **the product finding**

`go run ./spikes/4-reference-coverage`

Measured two different populations, because they give very different answers and only one of
them is the question worth asking.

### Uniform random sample (50 works/field, `sample` + `seed`, 2015–2023 articles)

| Field | n | No refs | Median refs | Mean refs | No DOI |
| --- | --- | --- | --- | --- | --- |
| Computer Science | 50 | 24 (48%) | 3 | 18.2 | 14 |
| Medicine | 50 | 23 (46%) | 1 | 13.9 | 10 |
| Physics and Astronomy | 50 | 13 (26%) | 21 | 39.6 | 9 |
| Social Sciences | 50 | 44 (88%) | 0 | 2.9 | 25 |
| Arts and Humanities | 50 | 37 (74%) | 0 | 5.8 | 21 |
| **Overall** | **250** | **141 (56%)** | | | 79 |

56% with no references at all looks catastrophic — but it is the wrong population. A uniform draw
from OpenAlex is mostly editorials, errata, conference abstracts, book reviews and thin records
from non-depositing publishers. Nobody seeds a graph with those, and nothing cites them either.

### Frontier sample — works actually reached by following a seed's references

This is the population expansion hydrates: for the most-cited paper in each field, fetch
everything it references and measure *those*.

| Field | Seed | Hydrated | No refs | Median refs | Mean refs |
| --- | --- | --- | --- | --- | --- |
| Medicine | W3128646645 | 100 | **6 (6%)** | 43 | 68.5 |
| Physics and Astronomy | W1981368803 | 34 | **4 (12%)** | 24 | 25.3 |
| Computer Science | W2064675550 | 33 | **5 (15%)** | 12 | 18.4 |
| Social Sciences | W2142225512 | 46 | 25 (54%) | 0 | 14.9 |
| Arts and Humanities | W2153190547 | 37 | **31 (84%)** | 0 | 2.4 |
| **Overall** | | **250** | **71 (28%)** | | |

**Verdict: the tool works in STEM and does not work in the humanities.**

- Medicine, Physics and CS: 6–15% dead ends. An expansion keeps going for several hops, which is
  exactly what the product needs.
- Social Sciences: 54% dead ends. Usable, thin, and the user should be told.
- Arts and Humanities: **84% dead ends.** The graph stops at depth 1. Books and chapters dominate
  the citation practice there and publishers largely do not deposit reference lists.

This is the risk `SCOPE.md` lists as "full-text coverage will disappoint people," except it lands
one layer earlier than expected — on the *graph*, not the PDFs. A humanities researcher's first
expansion will return a star of unexpandable stubs.

**Act on it:** show reference coverage per node from day one, the same way OA status is shown.
"This paper has no reference list in OpenAlex" is a fact the user needs at the moment of
expansion, not a mystery about why their graph stopped growing.

### Also: ~40 references per paper is low

The docs' expansion arithmetic assumes ~40 refs/work. Measured: 68.5 mean on the Medicine
frontier, 107.8 mean across the well-cited sample in spike 1. In STEM the branching factor is
closer to 70–100, so depth 2 is ~5,000–10,000 nodes rather than 1,600. This makes the node budget
*more* necessary, not less — the qualitative conclusion in ADR-002 survives, with worse constants.

---

## Spike 5 — FTS5 in `ncruces/go-sqlite3` · **PASS, with a catch worth knowing**

`go run ./spikes/5-fts5` · driver v0.35.3, SQLite 3.53.4

**FTS5 is not compiled into the default WASM build.** `PRAGMA compile_options` returns 58 entries
and `ENABLE_FTS5` is not among them — nor FTS3/FTS4, RTREE or JSON1. Applying `schema.sql`
straight away fails at the `chunk_fts` table with `no such module: fts5`.

**It ships as a loadable WASM extension instead**, alongside `parser`, `rtree`, `spellfix` and
`vec1`. One line fixes it:

```go
sqlite3.AutoExtension(fts5.Register)   // github.com/ncruces/go-sqlite3/ext/fts5
```

`AutoExtension` runs for every new connection, which is what `internal/store` needs — **a
connection opened without it cannot even read a table created with it**, so this must be wired
into `store.Open` before the read pool and the write handle are created, not bolted on later.

With that registered, against the real `internal/store/schema.sql`:

| Check | Result |
| --- | --- |
| Multi-statement `Exec` of the whole schema | **supported** — no statement splitter needed |
| `chunk_fts` contentless FTS5 table + 3 triggers | created cleanly |
| `journal_mode` after schema | `wal` |
| `foreign_keys` | `1` — but it is **per connection**, not stored in the file |
| `MATCH`, `NEAR(a b, 5)`, `snippet()`, `ORDER BY rank` | all work |
| `porter unicode61` stemming | `cite` / `cited` / `citation` fold together |
| `AFTER DELETE` trigger keeps the index in sync | yes |

**ADR-007's open risk is closed.** No external index, no custom SQLite build. Two notes for the
store package: register the extension on every connection, and re-apply `foreign_keys` per
connection since the PRAGMA in `schema.sql` only affects the connection that ran it.

---

## Spike 6 — Vector search · **PARTIAL, and it changes the M4 plan**

`go run ./spikes/6-vectors`

The plan was to test `asg017/sqlite-vec`. Before doing that: the driver already ships
**`ext/vec1`, SQLite's own vector extension** (<https://sqlite.org/vec1>), as a loadable WASM
module in the existing dependency tree. Testing that first costs nothing and could retire the
pre-1.0 risk CLAUDE.md flags.

**It is correct.** Against 2,000 random 384-dim unit vectors, the top-10 by
`vec1_l2_distance` matched a brute-force computation in Go **exactly**, in order, for both L2 and
cosine. Results survived closing and reopening the database, and vec1 coexists with FTS5 in one
file. (`vec1_l2_distance` returns **squared** L2 — 1.6664 where true L2 is 1.2909.)

**It has no ANN index.** `MATCH` is rejected in every form with `unable to use function MATCH in
the requested context`, and this is not a syntax problem:

```
vec1_info() -> version 0.7 (Scalar, single-threaded)
rebuild {index:"ivf"}  -> vec1: unrecognized index 'ivf', should be one of  none, or flat
```

`flat` *is* the brute-force scan. There is no index to find.

**The scan does not meet the latency target.** 133 ms for 2,000 vectors, ~66 µs per vector:

| Chunks | Projected query |
| --- | --- |
| 2,000 | 133 ms |
| 20,000 | 1.3 s |
| 100,000 | 6.7 s |

`ARCHITECTURE.md` targets < 500 ms to ranked passages. A 500-paper library is roughly 50,000
chunks, which lands around 3 seconds — out by an order of magnitude.

### What to do about it

**Do not treat this as a blocker, and do not rush to `sqlite-vec`.** M4 is specified as *hybrid*
retrieval: seed by keyword and meaning, expand along citation edges, then rerank. A vector scan
over a graph-and-FTS5-prefiltered candidate set of a few thousand chunks is ~100 ms, which is
inside target. **Scanning a pre-filtered set is the design, not a workaround** — the missing ANN
index may never sit on the critical path.

Before M4, in order:

1. **Build retrieval so the vector stage never sees the whole library.** Required anyway.
2. **Only if that is not enough, re-test `asg017/sqlite-vec`** — and check first whether it offers
   a real ANN index or merely a faster SIMD scan. If it is the latter, switching buys a constant
   factor and costs the pre-1.0 dependency risk. That trade is worth making only with a measured
   number in hand.

---

## Spike 7 — Cross-compilation · **PASS**

`windows/amd64`, `darwin/arm64` and `linux/amd64` all build from Windows with `CGO_ENABLED=0`,
~2.3 MB each, and the Windows binary runs. **D9 holds — including with the WASM SQLite driver and
its extensions linked in**, which was the real question.

---

## Recorded while building step 7 — 24 Sept 2026

Not a spike, but measured the same way: real responses, recorded as the fixtures in
`internal/sources/openalex/testdata/`. Ten requests, about 13 credits. `mailto` was not sent.

| Question | Answer |
| --- | --- |
| Does OpenAlex resolve an arXiv paper by its DataCite DOI? | **Not reliably.** `doi:10.48550/arxiv.1706.03762` is a 404, although other works do carry arXiv DOIs. The work *is* found by `filter=locations.landing_page_url:` on its arXiv `/abs/` page (http and https), for 1 credit |
| What does a 404 look like? | An **HTML page**, not JSON. Never show its body to the user |
| What does a batch do with an ID it cannot match? | **Omits it silently** — 3 asked, 1 returned, `meta.count` 1. The absence is the only signal |
| Does a missing ID redirect to a merged record? | Not the one tested: `W2963403868` is a plain 404. OpenAlex documents redirects for merged records, so absent IDs are confirmed by a free single lookup that would follow one |
| What does a title search cost? | **10 credits** (`cost_usd` 0.001), ten times a list request. `title.search` is rewritten internally to `display_name.search` |
| What is `10.1145/3292500`, the DOI in M0's definition of done? | The **KDD 2019 proceedings volume**: type `paratext`, **zero references**. It resolves, but it cannot seed a graph |
| Types seen that the model has no constant for | `conference-paper`, `paratext`. Stored as they arrive |

---

## Measured in M1's first live expansion — 24 Sept 2026

`fil expand` from `10.7717/peerj.4375` ("The state of OA"), default budget 500, max depth 3, in a
throwaway profile. About 7 credits. Three things no spike had measured:

| Question | Answer |
| --- | --- |
| How many of a real seed's references does a batch filter return? | **44 of 54.** Of the ten omitted, **8 are dangling** — listed in `referenced_works`, 404 by ID — and **2 exist under the same ID** but are not matched by the filter. Without a confirming single lookup, those two real papers would be marked unresolved |
| Does OpenAlex hold one paper under two IDs with one DOI? | **Yes.** `W2949614626` and `W4294576234` are one arXiv preprint (`10.48550/arxiv.astro-ph/0411275`). Writing the second tripped the `UNIQUE` index on `work.doi` and rolled back a whole batch — the first live run stopped there |
| After ID and DOI dedup, how many duplicates remain? | **20 same-title groups among 525 hydrated works (~4%)**: different DOIs or none. Some are one paper (two 2008 "articles"; a 2017 preprint and its 2018 journal version). At least one mixes in a **book review sharing the book's title**, so title alone cannot decide |

End state: 525 hydrated, 7,677 works, 11,802 edges; depth 0–3; no duplicate IDs or DOIs, no
self-loops, no dangling edges. Budget exact: 478 hydrated + 21 unresolved + 1 merged = 500.
Reference coverage 67% — this seed's neighbourhood is information science, which sits with the
social sciences in D12's table.

**Path finding: a recursive CTE does not scale on a real graph.** The stack table named recursive
CTEs for traversal. SQLite's recursive CTE has no shared visited set; guarding cycles with a path
string, it enumerates every *path* rather than every *work*. From the seed, undirected, on the
same library (6,555 works, 10,049 edges):

| Depth | Rows produced | Distinct works | Time |
| --- | --- | --- | --- |
| 2 | 1,982 | 1,043 | 0.03 s |
| 3 | 40,856 | 4,769 | 0.08 s |
| 4 | 937,387 | 6,555 | 1.54 s |
| 5 | **20,848,660** | 6,555 | **125 s** |

Growth is ~20× per step, and every work was already reached at depth 4. `store.ShortestPath` is
instead a breadth-first search: one set-based query per step over the whole frontier, and a
visited set in Go, so each work is touched once. Still plain SQL, so the Postgres move is
unaffected. Two things the live graph showed on the way: `fil path` found real lineage in two
steps (the seed → a 2011 RCT → *Invisible Colleges*, 1973), and the graph has real cycles — the
seed and the 2018 Sci-Hub paper each cite the other.

---

## Consequences for the build

- **`go.mod` moved to `go 1.25.0`.** `go get` bumped it: `go-sqlite3-wasm/v3` requires it. CLAUDE.md
  said Go 1.24; it now says 1.25.
- **Dependency licences, all checked before adding** (D9 and the licence rule): `ncruces/go-sqlite3`
  MIT · `ncruces/go-sqlite3-wasm/v3` MIT-0 · `ncruces/julianday` MIT · `golang.org/x/sys` BSD-3.
  Nothing copyleft, nothing that forces AGPL.
- **`github.com/ncruces/go-sqlite3/embed` is no longer needed.** Importing it prints
  *"If you're reading this, you're unnecessarily importing …"* at runtime. The WASM binary now comes
  from the separate `go-sqlite3-wasm/v3` module.
