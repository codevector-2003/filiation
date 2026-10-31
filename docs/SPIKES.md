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

## M1 validation run — 24 Sept 2026

20 seeds, 5 fields, live; every seed completed add, expand, duplicate report and export. Full
results in [`VALIDATION.md`](VALIDATION.md). Two measurements that belong with the spikes:

| Question | Answer |
| --- | --- |
| Frontier reference coverage, with seeds that have reference lists | Medicine 93%, physics 91%, CS 88%, social sciences 59%, humanities 56% |
| Title duplicates per 100 fetched works | 1.4 (medicine) → 4.1 (humanities) |
| Can OpenAlex's reference data be plainly wrong? | **Yes.** EPR (1935)'s only listed reference is Bohr's same-titled reply, as a 1996 reprint, which cites EPR back |

---

## Spike 9 — PDF availability · **a PDF for about one work in eight** · 24 Oct 2026

`go run ./spikes/9-pdf-availability`. The 20 validation libraries rebuilt live (8,385 fetched works,
unique within each field), every work's OA locations read from OpenAlex (85 requests, 1 credit
each), then 40 works per field with a PDF link downloaded the way ADR-013 says fil will: no cookies,
no credentials, a plain `User-Agent`, one request per second per host, `%PDF-` or it is not a PDF.
Run twice; the outcomes were identical.

| Field | Works | No open copy | Open, no PDF link | PDF link | Downloads that got a PDF | ≈ works fil can get |
| --- | --- | --- | --- | --- | --- | --- |
| Medicine | 1,646 | 44% | 26% | 30% | 11 of 40 (28%) | ~8% |
| Physics | 1,495 | 43% | 3% | 54% | 34 of 40 (85%) | ~46% |
| Computer Science | 1,348 | 66% | 9% | 25% | 23 of 40 (58%) | ~14% |
| Social Sciences | 1,936 | 75% | 8% | 16% | 22 of 40 (55%) | ~9% |
| Arts and Humanities | 1,960 | 86% | 6% | 8% | 3 of 40 (8%) | <1% |
| **All** | 8,385 | 64% | 10% | 25% | 93 of 200 (47%) | **~12%** |

The last column multiplies the two before it; with 40 downloads per field it is an estimate, not a
measurement. Gold and bronze works have a PDF link 89% and 96% of the time, green 58%.

**Why downloads fail — the host decides, not the paper.**

| Host | Attempts | PDFs | What came back |
| --- | --- | --- | --- |
| arxiv.org | 56 | **56** | the PDF, every time |
| journals.plos.org, nature.com, BioMed Central, doi.org, ACL | 14 | 14 | the PDF |
| Publishers: APS, ACM, Wiley, NEJM, OUP, Lancet, BMJ, JAMA, Cell, PNAS… | ~55 | 0 | **HTTP 403** — bot walls in front of open copies |
| iopscience.iop.org | 11 | 0 | HTTP 400, or an HTML page |
| pmc.ncbi.nlm.nih.gov | 7 | 0 | **an HTML page**, never the PDF |
| hdl.handle.net (repositories) | 10 | 0 | 403 or an HTML landing page |

84 of 200 attempts were 403s and 25 were HTML. fil does not work around either: a 403 is a refusal,
and an HTML page is recorded as "not a PDF" and never read for links (ADR-013). **PubMed Central** is
the loss that matters — it is where most open medicine lives — and Europe PMC publishes the same
open-access articles through a documented API; that is a candidate for a later source, not a
workaround.

**Speed:** median download 0.8 s, 90th percentile 4 s; median size 0.4–0.9 MB by field. 200
downloads with four at a time took under five minutes, most of it the per-host wait.

**Redirects get the per-host limit too.** The recorded run limited only the first host of each URL;
a link through `doi.org` reached the publisher unlimited (found in review, PR #3). The spike now
waits on each redirect hop's own host, and `fil fetch` must do the same (step 6). The outcomes
above are per host and would not change, but the run was not repeated after the fix.

**Re-run after the fix, 28 Oct 2026:** the 20 libraries rebuilt (8,381 works, 25% with a PDF
link, the same split by field) and a new sample of 200 downloads: **100 PDFs (50%)** — physics 95%,
computer science 70%, social sciences 45%, medicine 30%, humanities 10%. arXiv served 61 of 62;
publishers still answered 403 (81 attempts) or HTML (21). The outcomes stand. The median time rose
from 0.8 s to 6.4 s because each redirect hop now waits its own turn on its host — mostly arXiv,
queued at one request a second — not because hosts got slower.

**Unpaywall, 28 Oct 2026 — adds nothing OpenAlex does not already give.** Asked with a separate
address supplied by the maintainer, read from `FIL_SPIKE_UNPAYWALL_EMAIL` only; it appears in no
file the spike wrote (checked). 286 DOIs: 198 works OpenAlex gave no PDF link for, and 88 whose
OpenAlex download failed.

| Asked about | Unpaywall's answer |
| --- | --- |
| 198 works with no OpenAlex PDF link (closed, or open as a web page only) | **No PDF link for any of them** |
| 88 works whose OpenAlex link failed | A PDF link for all 88 — **every one the same link OpenAlex gave**, already refused; nothing new to try |
| The M0 seed, `10.7717/peerj.4375`, asked once by hand | Gold OA, 10 locations (PeerJ, PubMed Central, PubMed, Zenodo, institutional repositories), **no `url_for_pdf` on any** |
| 5 DOIs | 404, unknown to Unpaywall |

The two services share their data, and on this sample Unpaywall's PDF links are a subset of
OpenAlex's. Decision recorded in D21: not built.

---

## Spike 10 — PDF text extraction · **PDFium in WebAssembly** · 24 Oct 2026

`cd spikes/10-pdf-text && go run .` — its own module, so the candidates stay out of fil's `go.mod`.
19 PDFs from spike 9 (4 per field; the humanities had 3), scored against Poppler's `pdftotext`
26.09, which stands in for the truth and is not a candidate: it is a separate program.

| | `ledongthuc/pdf` (pure Go) | `go-pdfium` WebAssembly (PDFium on wazero) | `pdftotext` |
| --- | --- | --- | --- |
| Files that failed | 2 of 19 (`LZWDecode`, a zlib header) | **0** | 0 |
| Words F1 against baseline, median | 0.24 | **0.985** | 1 |
| 6-word sequences in baseline order, median | 0.03 | **0.85** | 1 |
| Files with order below 0.8 | 15 | 8 | — |
| Ligature characters left (ﬁ, ﬂ) | 884 | **0** | — |
| Words longer than 25 letters per 1,000 (missing spaces) | 179 | **0** | — |
| Page counts that match | 17 of 19 | **19 of 19** | — |
| Time, all 19 files (two runs) | 0.6–1.8 s | 6.0–6.4 s | 2.9–6.3 s |
| Added to a binary built without cgo | +0.6 MB | **+10 MB** (5.75 MB is the `.wasm`) | not shippable |
| Builds with `CGO_ENABLED=0` for Windows, Linux, macOS | yes | **yes** | — |
| Licences linked in | BSD-3 | MIT, Apache-2.0 (wazero, commons-pool), BSD-3 (uuid, x/sys, x/text) | — |

`ledongthuc/pdf`'s plain-text call drops the spaces between words in most files: it is fast and
small, and its output cannot be searched. PDFium's words are the baseline's words. Its lower order
scores are mostly two-column pages and formulas, which `pdftotext` also orders its own way; the
one file at 0.22 is a scanned NBER working paper stored **upside down**, with a garbled text layer
— both tools produce nonsense there. **Text extraction therefore needs a gibberish check** (step 7):
a text layer is not the same as text.

The 65 private-use characters in PDFium's output are the pieces of large brackets in formulas
(Symbol font). `pdftotext` produces the same 65.

**Broken files** — empty, cut off at 30%, random bytes behind a `%PDF-` header, HTML saved as
`.pdf`, a wrecked cross-reference table: every candidate refused the first four with an error and no
panic or hang. PDFium and `pdftotext` recovered all 45 pages of the wrecked one; `ledongthuc`
refused it.

**A time limit must stop the work, not just stop waiting for it** (found in review, PR #3). PDFium
runs in a small pool of instances, and an extraction abandoned at its time limit would keep its
instance. The spike now kills the instance, with wazero's `WithCloseOnContextDone` so the kill
interrupts code mid-run — measured: a 48-page extraction killed 20 ms in returns at once, and the
next file extracts normally. go-pdfium's default runtime config has to be repeated alongside it,
because the bundled module needs WebAssembly exception handling. `internal/text` (step 7) uses the
same set-up. Re-run after the fix: identical quality scores.

**What adopting PDFium costs:** `go-pdfium` v1.21.3 requires **Go 1.26** and `x/text` v0.42, so
fil's `go` line moves from 1.25 to 1.26 when `internal/text` lands (local toolchain 1.27, CI uses
`go.mod`). `hashicorp/go-plugin` (MPL-2.0) is in `go-pdfium`'s `go.mod` but **not linked** by the
WebAssembly package — checked with `go version -m`. On Windows, PDFium's sandbox mounts the current
drive; fil hands it bytes rather than paths, so it needs no view of the file system.

---

## Spike 11 — matching references from PDF text · **precise, but finding the list is the hard part** · 24 Oct 2026

`go run ./spikes/11-reference-matching`, on the texts spike 10 extracted. OpenAlex's
`referenced_works` for each paper is the answer key, so the measurement is free. Find the last
"References" heading, split the entries (numbered when the numbers run in sequence, otherwise at
author-list starts), and match each OpenAlex reference to the entry holding at least 70% of its
title's words.

| Text from | Section found | Entries | OpenAlex refs with a title | Matched (recall) | Confirmed by year or surname |
| --- | --- | --- | --- | --- | --- |
| `ledongthuc` | 2 of 19 | 9 | 548 | 1% | — |
| **PDFium** | 12 of 19 | 352 | 548 | **44%** | **97%** |
| `pdftotext` | 13 of 19 | 310 | 548 | 38% | 96% |

- **When a match is made it is right.** 97% of matches also carry the year or the first author's
  surname.
- **When the list is found and split, recall is high**: 62–100% on 9 of the 13 papers where
  `pdftotext`'s text had a section.
- **Finding and splitting the list is what fails.** All four physics papers have no "References"
  heading (APS and arXiv layouts end with numbered notes); one social-science paper's 129
  references became 5 entries, because its author-year style broke the splitter; one medicine
  paper's 33 references became 5 entries, the same way with numbers.
- PDFium's text does slightly better than `pdftotext`'s, which agrees with spike 10.

---

## Spike 13 — the desktop app shell · **Wails v2** · 31 Oct 2026

**Question.** Which native webview should the desktop app shell use (D24)? Candidates: Wails
v2.17.0 and Wails v3.0.0, each as the same tiny shell. It opens a native window whose page is
served by a plain Go `http.Handler` in the same process, which is how fil's interface would be
shown. The page times a call to the Go side, then reports that it is up. `webview_go` was not
tried: its last change was in August 2024. Code: `spikes/13-desktop-shell`. It ran on the
maintainer's Windows 11 machine and in CI (`.github/workflows/spike-desktop-shell.yml`) on Windows,
macOS (Apple silicon), and Ubuntu 24.04 x86-64 and Arm64.

*Start* is process start to the page's first script running: first run / the next two.

| Platform | Wails v2: size | v2: start | Wails v3: size | v3: start |
| --- | --- | --- | --- | --- |
| Windows 11, maintainer's machine | 11.8 MB | 1.57 s / 0.74 s | 12.6 MB | 1.27 s / 0.70–0.81 s |
| Windows, CI | 11.3 MB | 3.15 s / 0.51–0.55 s | 12.1 MB | 3.15 s / 0.51–0.55 s |
| macOS 26, Apple silicon, CI | 7.8 MB | 3.18 s / 1.73 s | 10.4 MB | 0.87 s / 1.32–1.42 s |
| Ubuntu 24.04 x86-64, CI | 8.7 MB | 1.93 s / 0.15 s | 11.6 MB | **crashes**; 17–25 s with the sandbox let through |
| Ubuntu 24.04 Arm64, CI | 8.1 MB | 1.63 s / 0.17–0.18 s | 10.9 MB | **crashes**; 18–26 s with the sandbox let through |

The page's round trip to the Go side was 11–30 ms everywhere. Sizes are stripped (`-s -w`).

- **Both build without cgo on Windows.** Wails drives WebView2 from pure Go there, so the Windows
  shell can still be cross-built, for arm64 too (checked).
- **Wails v3 does not run on a stock Ubuntu 24.04.** It uses GTK 4 and WebKitGTK 6.0, which always
  runs web content in a bubblewrap sandbox. The sandbox needs unprivileged user namespaces, which
  Ubuntu 24.04 restricts by default: `bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted`,
  then the app aborts. Allowing them (`kernel.apparmor_restrict_unprivileged_userns=0`) lets it
  start, but only after 17–26 s, a fixed wait that switching off the accessibility bus did not
  remove. Building v3 on GTK 3 (`-tags gtk3`) also took about 25 s. Not worth chasing: Wails v2
  starts in 0.15 s on the same machines.
- **Wails v2 on Linux uses GTK 3 and WebKitGTK 4.1**, where the sandbox is optional. It needs
  `libwebkit2gtk-4.1` at run time (Ubuntu 22.04+, Debian 12+, Fedora), and the `webkit2_41` build
  tag.
- **Wails v2 on macOS needs one linker flag with the Xcode 26 SDK:** it does not link
  `UniformTypeIdentifiers` itself, so `CGO_LDFLAGS="-framework UniformTypeIdentifiers"`.
- **macOS start-up is the slowest** (about 1.7 s warm for v2), which is WebKit's own start on the
  CI runner; v3 was faster there.

**Decision: Wails v2** (D25). It is the only one that ran on every platform, it is mature, and its
Linux start is the fastest of all. Wails v3's tray and multi-window support are not needed for the
designed screens. Revisit v3 when its Linux build starts quickly under the WebKitGTK 6.0 sandbox.

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
