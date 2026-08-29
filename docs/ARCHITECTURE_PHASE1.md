# Phase 1 architecture — the core graph

**Covers:** milestones M0 and M1 · 15 Sept – 31 Oct 2026 · ships as **v0.1**
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

- **Setup:** `pip install filiation` then one command. No server, no Docker, no API key.
- **Interruptible:** Ctrl-C at any point must leave a consistent database, and re-running must continue rather than restart.
- **Offline-tolerant:** losing the network mid-run is a pause, not a corruption.
- **Budget-bounded:** the user says how big the graph may get, and that limit is respected exactly.
- **Cheap:** a typical expansion should cost a small fraction of the daily API allowance.

---

## 2. Component map

```
src/filiation/
├── cli.py            Typer commands: add, expand, show, path, neighbours, export, stats
├── config.py         Resolve library path, contact email, default budgets (flag > env > file > default)
├── identity.py       Normalise and classify user input: DOI, arXiv, OpenAlex ID, PMID, title
├── models.py         Plain dataclasses: Work, Edge, FrontierItem, ExpansionResult
├── store.py          ★ ALL SQL lives here. The seam for a future server backend
├── schema.sql        DDL
├── http.py           httpx client, token bucket, retry with backoff, on-disk response cache
├── sources/
│   ├── base.py       Source protocol — so Crossref/arXiv can be added without touching expand.py
│   └── openalex.py   get_work, get_works_batch, search_by_title
├── expand.py         ★ The budgeted traversal. The heart of Phase 1
├── export.py         GraphML, JSON, BibTeX
└── errors.py         Typed errors so the CLI can print something useful
```

**Dependency direction is strictly one way:**

```
cli → expand → { store, sources } → http
       ↑
    identity, config, models  (leaves — depend on nothing internal)
```

`store.py` never imports `sources/`. `sources/` never imports `store.py`. Only `expand.py` knows about both. This is what keeps the storage backend swappable later without a rewrite.

---

## 3. The cost model

Everything about the expansion design falls out of these numbers.

| Operation | Credits | Works returned | Credits per work |
| --- | --- | --- | --- |
| Single work by ID | 1 | 1 | **1.00** |
| List filtered by many IDs, `per_page` filled | 10 | up to 50 | **0.20** |

Batching is five times cheaper per work and vastly fewer round trips. The free tier gives 100,000 credits per day.

`referenced_works` comes back **inside the work object**, so fetching a paper gives you its outgoing edges with no extra call. This single fact shapes the whole algorithm.

### What expansion actually costs

Assume ~40 references per paper.

| Reach | Works | List calls | Credits | Verdict |
| --- | --- | --- | --- | --- |
| Depth 1 | ~40 | 1 | 10 | Trivial |
| Depth 2 | ~1,600 | 32 | 320 | Comfortable |
| Depth 3 | ~64,000 | 1,280 | 12,800 | Affordable, but nobody wants this graph |

**Depth is not the control. The node budget is.** Depth 3 is affordable and useless — the graph stops being a map of a field and becomes a map of science. The user asks for 500 useful nodes, not three hops.

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

```python
def expand(seed_id, max_nodes=500, max_depth=3, max_refs_per_work=200):
    hydrate(seed_id, depth=0)          # 1 credit
    record_edges_and_stubs(seed_id)    # free, from referenced_works

    spent = 1
    while spent < max_nodes:
        # Best-first, not breadth-first. Costs one indexed query.
        batch = store.next_frontier(
            limit=50,
            max_depth=max_depth,
            order_by="in_graph_indegree DESC, depth ASC, cited_by_count DESC",
        )
        if not batch:
            break

        with store.transaction():            # one transaction per batch
            works = openalex.get_works_batch(batch)   # 10 credits for up to 50
            for w in works:
                store.hydrate(w)
                if w.depth < max_depth:
                    store.record_edges_and_stubs(w, cap=max_refs_per_work)
            spent += len(works)
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

**Decision.** Put every SQL statement in `store.py` as plain module-level functions over `sqlite3`. No ORM, no repository class hierarchy, no interface declaration. The module boundary *is* the seam.

**Options considered**

| Option | Complexity | Swap cost later | Speed now | Verdict |
| --- | --- | --- | --- | --- |
| A. Direct `sqlite3` calls scattered through the codebase | Low | Very high | Fastest | Rejected |
| **B. One `store.py` module, plain functions** | **Low** | **Moderate** | **Fast** | **Chosen** |
| C. SQLAlchemy Core with dialect swap | Medium | Low | Slower | Rejected for now |
| D. Full repository pattern with protocols | High | Low | Slowest | Rejected |

**Trade-off.** C would make the Postgres swap nearly free, but adds a dependency and an indirection layer for a migration scheduled for *after* v1.0 and conditional on the product succeeding. B costs a day of rewriting `store.py` if that day ever comes. Paying one day later beats paying every day now.

**Consequences**
- Easier: writing exactly the SQL you want, including recursive CTEs for path finding, which ORMs make awkward.
- Harder: the eventual Postgres port is a rewrite of one file rather than a config change.
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

**Decision.** One `http.py` module owning: a shared `httpx` client that always sends `mailto`, a global token bucket set well below the measured limit, exponential backoff with jitter on 429 and 5xx that respects `Retry-After`, and an on-disk response cache keyed by full URL.

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

**Decision.** `identity.py` classifies and normalises input. Deterministic identifiers resolve silently. A **title search returns candidates and requires confirmation** — interactively in the CLI, and by an explicit `--accept-first` flag in scripts.

**Trade-off.** One extra keystroke against a wrong paper silently seeding an entire graph. A wrong seed is not a small error; it poisons everything expanded from it, and the user may not notice for weeks.

**Consequences**
- Easier: trust in what is in the library.
- Harder: scripted bulk import needs the explicit flag.

---

### ADR-006: One library per user, not one per directory

**Status:** Proposed · **Date:** 29 Aug 2026

**Context.** The whole product thesis is a library that accumulates over years. A database that lives in the current working directory produces a scattering of half-built graphs.

**Decision.** The library defaults to a per-user application data directory via `platformdirs`. Override order: `--db` flag, then `FILIATION_DB` environment variable, then config file, then the default.

**Consequences**
- Easier: `fil add` does the right thing from any directory. Accumulation happens by default.
- Harder: tests must always pass an explicit path, and the default location differs per OS — the CLI must print where the library is on first run, or users will not find it.

---

## 6. Schema changes this design requires

The current `schema.sql` assumes a work is fully known when inserted. ADR-003 breaks that. Apply before writing `store.py`:

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

**No network in any unit test.** Record real OpenAlex responses once into `tests/fixtures/` and replay them through a fake `httpx` transport.

| Test | Asserts |
| --- | --- |
| Golden expansion | A fixed seed produces an exact node and edge count |
| Budget respected | `max_nodes=100` never produces 101 hydrated works |
| Idempotent | Running `expand` twice adds nothing the second time |
| Dedup | The same paper reached by DOI, arXiv ID and OpenAlex ID yields one row |
| Resume | Kill after two batches, re-run, end state equals the uninterrupted run |
| Cycle safety | A hand-built cyclic fixture does not hang path finding |
| Stub handling | Export and display work on a graph that is 90% stubs |

The idempotency and resume tests are the two that will actually catch regressions. Write them early.

---

## 9. Run these spikes in week 1, before writing real code

The cost model rests on assumptions the documentation contradicts itself about. Each of these is an hour, and each can invalidate a design choice.

1. **Batch fetch by OpenAlex ID.** Does filtering works by a pipe-separated list of OpenAlex IDs work, and is the ceiling 50 or 100 values? Sources disagree. If batch-by-ID is unsupported, ADR-002's cost model collapses and the fallback is batching by DOI — which fails for works without one.
2. **Real `per_page` maximum.** Documented as both 100 and 200.
3. **Real sustained request rate with `mailto` set.** Documented as 10/second in one place and 100/second in another. Set the token bucket from what you measure, not what you read.
4. **Reference coverage across fields.** Take 20 papers from 5 fields and check what fraction have a complete `referenced_works` list. **This is the biggest unknown in the entire product**, not just this phase — OpenAlex reference coverage depends on what publishers deposit. If some field is sparse, the graph is thin there and you need to know that in September, not in March.

Spike 4 is a product question wearing an engineering costume. Do it first.

---

## 10. Week by week

| Week | Dates | Work |
| --- | --- | --- |
| 1 | 15–21 Sept | The four spikes. Then `config.py`, `schema.sql` with the §6 changes, `store.py` skeleton |
| 2 | 22–28 Sept | `identity.py`, `http.py` with token bucket and cache, `sources/openalex.py` |
| 3 | 29 Sept – 5 Oct | `fil add` working end to end. **M0 complete** |
| 4–5 | 6–19 Oct | `expand.py`: frontier query, batching, budget, resume. The core of the phase |
| 6 | 20–26 Oct | `export.py`, `fil path`, `fil neighbours`, `fil stats` |
| 6.5 | 27–31 Oct | The 20-paper / 5-field validation run. Fix what breaks. Tag **v0.1** |

Weeks 4 and 5 hold the only genuinely hard code in this phase. Everything before is plumbing and everything after is a thin layer over queries. If you slip, slip weeks 1–3 and protect 4–5.

---

## 11. Action items

1. [ ] Run spike 4 (reference coverage across fields) — it can change the product, not just the code
2. [ ] Run spikes 1–3 and write the measured numbers into this document
3. [ ] Apply the §6 schema changes to `schema.sql`
4. [ ] Decide the default `max_nodes`. Suggestion: 500 — large enough to be a map, small enough to finish in under a minute
5. [ ] Write the idempotency and resume tests before writing `expand.py`, not after
6. [ ] Print the library path on first run, per ADR-006
