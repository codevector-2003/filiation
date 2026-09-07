# Filiation

A citation graph and retrieval engine that researchers can run on their own machine.

Give it one paper. It follows the reference list, builds a map of what cites what, keeps the
open-access PDFs, and lets you ask questions across everything you have read.

**Filiation** is the term from textual criticism for establishing which manuscript descended
from which — working out the actual lines of copying between surviving texts. That is what this
tool does for papers: it reconstructs where a claim came from by following the citations back.

- Language: **Go** (see D8 in `docs/DECISIONS.md`)
- Module: `github.com/codevector-2003/filiation`
- Binary / CLI command: `fil`
- Status: **M0 in progress** — 3 of 9 packages done (`model`, `errs`, `config`). See `docs/STATUS.md`.

---

## The one sentence that matters

The graph is not the product. The answers are. The graph is what makes the answers better
than plain search.

---

## Locked decisions

| Decision | Consequence |
| --- | --- |
| Serve developers **and** non-coding researchers from day one | One binary, two front doors: MCP + CLI, and a local web page |
| v1 includes the full retrieval engine with Q&A | The graph alone does not count as done. Ships in stages |
| Local first, optional server later | All storage sits behind one package so the backend can be swapped |
| Open source, free to run, one-command setup | Rules out anything needing a server before the user sees a result |
| Go, not Python | Single cross-compiled binary; embeddings move behind Ollama |

---

## Hard rules — do not violate these

1. **Never download a paywalled PDF.** No scraping publisher sites. No using anyone's
   institutional login. Open access only, via OpenAlex OA locations and Unpaywall.
   This is a permanent line, not a v1 limitation.
2. **Do not write a reference parser.** OpenAlex already returns resolved reference lists
   (`referenced_works`). PDF reference extraction has real error rates that compound across a
   graph. Fall back to parsing only for works with no DOI and no index entry, and only later.
3. **Do not build an LLM-extracted knowledge graph.** Our citation edges are ground truth.
   Adding guessed entities and relations on top makes the graph worse and costs a lot.
4. **No cgo.** The entire value of choosing Go is one static binary the user double-clicks.
   A dependency that needs cgo breaks cross-compilation and the whole distribution story.
   If a library requires it, find another way.
5. **One writer goroutine.** SQLite allows a single writer. Go makes it trivially easy to
   violate this by accident — see "Concurrency" below. This is the most likely source of
   `database is locked` bugs in this codebase.
6. **Deduplicate on `openalex_id`.** The same work exists as preprint, conference paper and
   journal article with different DOIs. OpenAlex already merges most of these. Use its ID as the
   primary key or the graph will quietly rot.

---

## Stack

| Layer | Choice | Why |
| --- | --- | --- |
| Language | Go 1.25 | One static binary per platform. No runtime for the user to install |
| SQLite driver | `ncruces/go-sqlite3` | WASM build, **no cgo**. Ships FTS5 and vec1 as loadable extensions |
| Vector search | `ext/vec1` (SQLite's own) | Same file, no new dependency. **Exact but brute-force — no ANN index.** See D13 |
| Keyword search | SQLite FTS5 | Confirmed working (spike 5) — but must be registered per connection |
| Graph traversal | Recursive CTEs in SQL | Standard SQL, so it survives a move to Postgres |
| Graph algorithms | `gonum/graph` | PageRank and communities in memory. 2M edges fits easily |
| CLI | `spf13/cobra` | Standard, and matches what users expect from a Go tool |
| HTTP client | stdlib `net/http` + `golang.org/x/time/rate` | `rate.Limiter` is exactly the token bucket ADR-004 needs |
| MCP | `modelcontextprotocol/go-sdk` | Official, maintained with Google |
| Web server | stdlib `net/http` + `//go:embed` | **The SPA compiles into the binary.** No separate assets to ship |
| Embeddings | Ollama HTTP | See ADR-008. The cost of choosing Go |
| Answers | Ollama, or user-supplied API key | Free path must exist |
| PDF text | See ADR-009 | The weakest part of the Go choice. Treat as a Phase 3 spike |
| Distribution | GoReleaser → GitHub Releases | Cross-compiled binaries for Windows, macOS, Linux |

### Rejected, with reasons

- **Python** — better for PDF extraction and embeddings, worse for distribution. Chosen against
  deliberately; see D8. The consequence is that `quelle` (a Python package covering much of M0
  ingestion and M3 PDF fetching) can no longer be reused. That layer is ours to build.
- **cgo-based SQLite (`mattn/go-sqlite3`)** — faster, but cross-compilation becomes painful and
  the single-binary promise weakens.
- **An ORM (GORM)** — hides the SQL, and this project's queries are the interesting part.
  `sqlc` is the escape hatch if hand-written scan code becomes tedious; it keeps SQL as the
  source of truth while generating types.
- **Neo4j** — needs a server running before anything works, which breaks setup for exactly the
  non-technical researchers we want. Revisit only for shared-lab server mode.
- **MongoDB or any document DB for file storage** — document databases store JSON, not binary
  files. For PDFs the filesystem wins. MongoDB is also SSPL, which the OSI has not approved.

---

## Layout

```
filiation/
├── go.mod
├── cmd/fil/main.go            entry point, wires cobra to internal/library
├── internal/
│   ├── library/               ★ THE CORE. add, expand, search, ask, path
│   ├── store/                 ★ ALL SQL. schema.sql embedded here. The swap seam
│   ├── blobs/                 content-addressed PDF storage
│   ├── graph/                 expand, traverse, identity resolution
│   ├── export/                GraphML, JSON, BibTeX — no lock-in, M1
│   ├── text/                  pdf extraction, chunking, citation context
│   ├── retrieve/              hybrid search, ranking, answer assembly
│   ├── jobs/                  background work, resumable
│   ├── sources/               openalex, unpaywall, zotero
│   ├── embed/                 ollama client (interface: one method)
│   ├── llm/                   ollama or user API key
│   ├── httpx/                 rate limiting, retry, response cache
│   ├── config/                library path, contact email, budgets    ┐ leaves —
│   ├── identity/              DOI, arXiv, OpenAlex ID, PMID, title    │ these import
│   ├── model/                 Work, Edge, FrontierItem                │ nothing else
│   ├── errs/                  sentinel errors, compared with errors.Is┘ in internal/
│   ├── mcpsrv/                MCP tool definitions, M2
│   └── web/                   handlers + //go:embed of the built SPA, M5
├── web/ui/                    SPA source, built into internal/web/dist
└── docs/
```

**The rule that matters most:** `cmd/`, `internal/mcpsrv/` and `internal/web/` are translation
layers. Each takes a request, calls one function in `internal/library`, and formats the result.
**If a front door imports `internal/store`, that is a bug.** Go's `internal/` convention helps
but does not enforce this — you have to.

---

## Concurrency

Go makes the single-writer constraint easy to break, and Python's GIL used to hide this class of
mistake. Be deliberate:

- **One goroutine owns all writes.** The job worker holds the write path; everything else reads.
- Open **two `*sql.DB` handles**: a read pool, and a write handle with `SetMaxOpenConns(1)`.
- Enable WAL (already in `schema.sql`) so readers never block the writer.
- Set a `busy_timeout`. Without it, contention surfaces as an immediate error instead of a wait.
- Pass `context.Context` through everything. Cancelling an expansion must actually stop it.

---

## API notes

- **OpenAlex**: no API key required. **Measured 30 Aug 2026 — see `docs/SPIKES.md`, and trust it
  over the published documentation, which contradicts itself:**
  - Free allowance is **1,000 credits/day** ($0.10 equivalent), not 100,000.
  - **Single-work fetches are free.** List requests cost **1 credit** flat, whatever the page size.
  - `per_page` max is **200**, but at most **100 IDs** may be piped into a filter — 101 is a hard
    400. So hydration batches at 100, and that is the binding constraint.
  - **Batch for round trips, not for credits.** The first 429 appears around **11 req/s**, so set
    the token bucket to **5 req/s**. Credits will never be the thing that runs out; the rate will.
  - Send `mailto=<contact>` because it is asked for and it is how they reach you — but it produced
    **no measurable throughput or limit difference**. Do not design around a polite-pool benefit.
- **Expansion blows up fast**: STEM papers cite 70–100 works, not the ~40 originally assumed, so
  depth 2 is ~5,000–10,000 nodes. Expansion must always take a node budget. Never breadth-first
  without a limit.
- **Reference coverage is a field problem, not a bug.** Dead ends on the expansion frontier:
  Medicine 6%, Physics 12%, CS 15%, Social Sciences 54%, **Arts and Humanities 84%**. The graph
  genuinely stops after one hop in the humanities. Show per-node reference coverage from day one,
  exactly like OA status, or users will think the tool is broken when it is the data.
- **Ollama**: embeddings and generation over local HTTP. Absent Ollama, the tool must still work —
  see the degradation ladder in `docs/ARCHITECTURE.md`.

---

## Architecture

- `docs/STATUS.md` — where the project actually is, what is done, what is next. **Start here.**
- `docs/ARCHITECTURE.md` — whole-system design.
- `docs/ARCHITECTURE_PHASE1.md` — detailed design for the core graph, with ADRs.
- `docs/SPIKES.md` — measured answers. **Trust these over any documentation, including this file.**

## Data model

See `internal/store/schema.sql` for the DDL, embedded with `//go:embed`. The one non-obvious
column is `cites.context` — the sentence around the citation marker in the citing paper. Nobody
provides this for free and it is what makes retrieval better than everyone else's.

---

## Build order

Current position: **M0, steps 1–3 of 9 complete** — `internal/model`, `internal/errs` and
`internal/config` are written and tested; `internal/store` is next. Dates assume learning Go alongside building;
Phase 1 carries two extra weeks for that.

- [ ] **M0 — Skeleton** (2–3 weeks). Module layout, config, embedded schema, `store` package,
      one command that takes a DOI, fetches from OpenAlex, stores one node.
      *Done when:* `fil add 10.1145/3292500` writes a row and prints the title.
- [ ] **M1 — The graph** (3–4 weeks). Budgeted expansion, dedup, `expand`, `neighbours`, `path`,
      GraphML export. **First release, with cross-compiled binaries.**
      *Done when:* one seed gives a clean 500-node graph with no duplicates, opens in Gephi.
- [ ] **M2 — MCP server** (~1 week). Expose M1 as MCP tools. The differentiator.
- [ ] **M3 — Papers on disk** (3–4 weeks). OA PDF fetch, content-addressed storage, text
      extraction, chunking, FTS5. **Capture citation context sentences here.** Longer than the
      Python plan because the ingestion layer is now ours to write.
- [ ] **M4 — Retrieval and answers** (3–4 weeks). Ollama embeddings, `vec1`, hybrid
      retrieval, answers with citations. **Pre-filtering by graph and FTS5 is load-bearing, not an
      optimisation** — the vector stage is a brute-force scan, so it must never see the whole
      library. Build candidate generation before semantic search (D13).
- [ ] **M5 — Web interface** (2–3 weeks). Shorter than planned: `//go:embed` puts the SPA inside
      the binary, so there is nothing to package separately.
- [ ] **M6 — Claim genealogy** (3–4 weeks). Citation intent, weighted edges, tracing a claim to
      its earliest source. **The differentiator.**

---

## Spikes — all run, 30 Aug 2026

**Results and evidence: `docs/SPIKES.md`. Code: `spikes/`.** Trust those numbers over any
documentation, including this file's own history.

| # | Question | Answer |
| --- | --- | --- |
| 1 | Batch fetch by OpenAlex ID? | Yes. **Ceiling exactly 100**; 101 is a hard 400 |
| 2 | Real `per_page` max? | **200**, but the 100-ID filter binds first |
| 3 | Real sustained rate? | **First 429 at ~11 req/s** → bucket at 5/s. Found the real cost model (D11) |
| 4 | Reference coverage by field? | STEM 6–15% dead ends, **humanities 84%** (D12) |
| 5 | Does the driver ship FTS5? | Yes, as a loadable extension — `sqlite3.AutoExtension(fts5.Register)` |
| 6 | Vector search end to end? | `vec1` is exact but **has no ANN index** (D13) |
| 7 | Cross-compile with no cgo? | Yes — 3 platforms, ~2.3 MB, D9 holds |

**Two gotchas the store package must handle**, both from spike 5:

- Register FTS5 (and vec1) with `AutoExtension` **before** opening the read pool and write handle.
  A connection opened without the extension cannot even read a table created with it.
- `PRAGMA foreign_keys` is **per connection**, not stored in the file. The one in `schema.sql`
  only affects the connection that applied it — set it on every connection.

---

## Conventions

- Go 1.25. `gofmt` and `golangci-lint`. Table-driven tests with the stdlib `testing` package.
- `context.Context` as the first parameter of anything that does I/O.
- Errors wrapped with `fmt.Errorf("...: %w", err)`. Sentinel errors in `internal/errs`.
- No network in unit tests — record OpenAlex responses as fixtures, replay with an
  `http.RoundTripper` stub.
- Every external API goes through one package under `sources/`, so rate limiting and caching
  live in one place.

---

## Prior art to check before writing code

**`quelle`** (Python, MIT) fetches metadata from OpenAlex, Crossref, Semantic Scholar, arXiv and
Unpaywall, caches in SQLite, and downloads open-access PDFs. It cannot be reused from Go, but
**read its source before building M0 and M3** — it has already solved the identifier-resolution
and fallback-chain problems you are about to hit.

---

## Still open

1. **Licence** — MIT or Apache-2.0 for widest adoption, AGPL to stop a company hosting it as a
   paid service. Go's ecosystem is overwhelmingly permissive; MIT fits convention.
2. **Zotero** — read from a user's existing library? Cheapest route to real users. Decide early.
3. **Embeddings without Ollama** — is a pure-Go ONNX path (`onnx-gomlx`) worth the risk later,
   to restore true one-command setup? Revisit after M4 ships.
4. **Who maintains it after the degree?**
