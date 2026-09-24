# Architecture

Whole-system design for Filiation. Covers all six milestones.

**How to read this.** This document is the system-wide view: layers, contracts, and the decisions
that span more than one phase. [`ARCHITECTURE_PHASE1.md`](ARCHITECTURE_PHASE1.md) is the detailed
design for the core graph and does not repeat what is here. [`DECISIONS.md`](DECISIONS.md) is the
running log of what was chosen and rejected.

**Status:** Proposed · 29 August 2026 · nothing is built yet.

---

## 1. Requirements

### Functional

| # | Capability | Milestone |
| --- | --- | --- |
| F1 | Resolve a DOI, arXiv ID, OpenAlex ID, PMID, title or uploaded PDF to a work | M0 |
| F2 | Walk a work's references and build the citation graph, deduplicating versions | M1 |
| F3 | Answer graph questions: neighbours, shortest path, reachable set, co-citation | M1 |
| F4 | Export the whole library: GraphML, JSON, BibTeX | M1 |
| F5 | Expose every capability over MCP so an assistant can drive it | M2 |
| F6 | Fetch, store and extract text from open-access PDFs | M3 |
| F7 | Capture the sentence around each citation marker | M3 |
| F8 | Search by keyword and by meaning, expanded along weighted citation edges | M4 |
| F9 | Answer natural-language questions with citations into the user's own library | M4 |
| F10 | Browse and edit the library in a local web interface | M5 |
| F11 | Classify citation intent and trace a claim to its earliest source | M6 |

### Non-functional

| | Target | Why this number |
| --- | --- | --- |
| **Setup** | Download one binary and run it | No runtime, no package manager, no Docker. Ollama is an optional extra for semantic search |
| **Cost to run** | Zero, for every core feature | "Free for everyone" is a product constraint, not a nice-to-have |
| **Graph query latency** | < 50 ms at 100k works | Interactive browsing; anything slower feels broken |
| **Retrieval latency** | < 500 ms to ranked passages | Excludes answer generation, which is LLM-bound |
| **Answer latency** | 2–10 s | Dominated by the model, not by us |
| **Durability** | No operation may corrupt the library | Losing a two-year reading history is unforgivable |
| **Interruptibility** | Ctrl-C is always safe; re-running resumes | Long operations on a laptop that closes |
| **Offline** | Everything except fetching keeps working | Researchers work on trains |

### Constraints

One developer, roughly ten hours a week, alongside coursework. No budget. Open source. This is
why the design consistently prefers "one fewer moving part" over "more correct in the limit."

---

## 2. High-level design

Five layers. Each depends only on the layer below it.

```
┌──────────────────────────────────────────────────────────────┐
│  FRONT DOORS                                                 │
│                                                              │
│   cmd/fil          internal/mcpsrv       internal/web       │
│   cobra            MCP tools             net/http + embed   │
│   humans           assistants            researchers         │
└───────────────┬──────────────┬───────────────┬───────────────┘
                │              │               │
                └──────────────┼───────────────┘
                               ▼
┌──────────────────────────────────────────────────────────────┐
│  CORE  —  the only place logic lives                         │
│                                                              │
│   internal/library   add / expand / search / ask / path      │
│   internal/jobs      long-running work, resumable            │
│                                                              │
│   Every front door calls these. None of them contain logic.  │
└───────────────┬──────────────┬───────────────┬───────────────┘
                ▼              ▼               ▼
┌──────────────────────┐ ┌───────────┐ ┌─────────────────────┐
│  GRAPH               │ │ TEXT      │ │  RETRIEVAL          │
│  internal/graph      │ │ internal/ │ │  internal/retrieve  │
│  expand · traverse   │ │ text      │ │  search · rank      │
│  identity            │ │ pdf·chunk │ │  answer             │
└──────────┬───────────┘ └─────┬─────┘ └──────────┬──────────┘
           └───────────────────┼──────────────────┘
                               ▼
┌──────────────────────────────────────────────────────────────┐
│  STORAGE   internal/store — all SQL · internal/blobs — files │
└───────────────┬──────────────────────────────┬───────────────┘
                ▼                              ▼
        library.db (SQLite)              blobs/ (PDFs)
        graph · FTS5 · vectors           SHA-256 named

┌──────────────────────────────────────────────────────────────┐
│  ADAPTERS   internal/sources  (OpenAlex, Unpaywall, Zotero)  │
│             internal/embed    (Ollama HTTP)                  │
│             internal/llm      (Ollama, optional API key)     │
│             internal/httpx    (rate limit, retry, cache)     │
└──────────────────────────────────────────────────────────────┘
```

### The one decision that shapes everything above

**Three front doors, one core.** The CLI, the MCP server and the web app are thin translation
layers. Each takes a request, calls one function in `internal/library`, and formats the result. No
front door contains a rule, a query, or a policy.

This sounds obvious and is the thing most likely to erode. The pressure to write "just one small
query" directly in a web route is constant, and the first time you do it, MCP and CLI silently
diverge from the web app. The rule that keeps it honest: **if a front door imports `internal/store`,
that is a bug.**

The payoff is that F5 — the whole MCP differentiator — costs about a week instead of a rewrite,
because by M2 the core already does everything and MCP is a schema wrapper over it.

---

## 3. Data flows

### Flow A — adding and expanding (M0–M1)

```
identifier ──► identity.resolve ──► sources.openalex.get_work
                                         │
                                         ▼
                              store: hydrate seed
                              store: stub rows + edges     ◄── free, no API call
                                         │
                     ┌───────────────────┴────────────────────┐
                     ▼                                        │
          store.next_frontier(50)                             │
          ordered by in-graph in-degree                       │
                     │                                        │
                     ▼                                        │
          openalex.get_works_batch  ──► hydrate ──► new stubs ┘
                     │                                   loop until budget spent
                     ▼
              one transaction per batch  ──► Ctrl-C safe, resumable
```

Detail in [`ARCHITECTURE_PHASE1.md`](ARCHITECTURE_PHASE1.md).

### Flow B — acquiring full text (M3)

```
work (oa_url present) ──► http.get ──► blobs/tmp/xxxx
                                          │
                                    hash + verify
                                          │
                              rename ──► blobs/ab/abcd….pdf   (atomic)
                                          │
                                    pdf.extract_text
                                          │
                         ┌────────────────┴─────────────────┐
                         ▼                                  ▼
                  chunk.split                        context.extract
                  chunk rows + FTS5                  UPDATE cites SET context
                         │
                         ▼
                  embed (background job) ──► chunk_vec
```

`context.extract` is the step nobody else does, and it is nearly free here because the text is
already parsed. It is what M6 depends on.

### Flow C — asking a question (M4)

```
question
   │
   ├──► embed(question) ──► vector search ──┐
   │                                        ├──► seed set (~40 chunks)
   └──► FTS5 keyword search ────────────────┘
                                              │
                                              ▼
                         graph expansion: works citing / cited by the seed works,
                         weighted by co-citation and (after M6) citation intent
                                              │
                                              ▼
                              rerank ──► top ~12 passages
                                              │
                                              ▼
                              llm.answer(question, passages)
                                              │
                                              ▼
                     answer + citations, every one resolvable to a work in the library
```

**The graph expansion step is the whole thesis.** If ablating it does not improve results on the
test set, the graph is decoration — see the Phase 4 gate in
[`PRODUCT_PLAN.md`](PRODUCT_PLAN.md).

---

## 4. Storage

One SQLite file plus a directory of PDFs. Rationale in `DECISIONS.md` D3 and D4.

| What | Where | Notes |
| --- | --- | --- |
| Works, authors, edges | `library.db` | Edge table plus recursive CTEs for traversal |
| Chunk text | `library.db` | Small; must be searchable and joinable |
| Keyword index | `library.db` (FTS5) | Built in, no dependency |
| Embeddings | `library.db` (sqlite-vec) | Same file. Pre-1.0: pin the version |
| PDFs | `blobs/ab/<sha256>.pdf` | Content-addressed, sharded two chars deep |
| HTTP cache | `http/` under the OS cache directory | **Separate from the library** — deletable without touching user data. A directory of files, not `cache.db`; see ADR-004's implementation note |
| Config | `config.toml` | Alongside the library |
| Web UI assets | inside the binary | `//go:embed` — nothing to ship separately |

Everything derived — text, chunks, embeddings, citation contexts — is regenerable from the blobs
and the graph. Only `library.db` metadata and the blobs are irreplaceable. That distinction is
what makes the backup story a single sentence: copy those two, ignore the rest.

---

## 5. The core contract

Every front door calls this and nothing else. Signatures are indicative, not final.

```go
// package library

func Add(ctx context.Context, identifier string, opts AddOpts) (*Work, []Candidate, error)
func Expand(ctx context.Context, workID string, opts ExpandOpts) (JobHandle, error)
func Neighbours(ctx context.Context, workID string, dir Direction) ([]Work, error)
func Path(ctx context.Context, from, to string, maxHops int) ([]Work, error)
func Reach(ctx context.Context, workID string, depth int) ([]Work, error)
func CoCited(ctx context.Context, workID string, limit int) ([]WorkCount, error)
func Gaps(ctx context.Context, limit int) ([]Work, error)   // co-cited but absent
func Search(ctx context.Context, q string, mode SearchMode, limit int) ([]Passage, error)
func Ask(ctx context.Context, question string, limit int) (*Answer, error)
func Genealogy(ctx context.Context, claim string) ([]Work, error)   // M6
func FetchText(ctx context.Context, workID string) (JobHandle, error)
func Export(ctx context.Context, format Format, path string) error
func Stats(ctx context.Context) (*LibraryStats, error)
```

Two conventions that matter:

- **Anything that can take more than about two seconds returns a `JobHandle`, not a result.** The
  CLI may block on it and print a progress bar; the web app polls it; MCP returns the handle and
  lets the assistant check back. One rule, three behaviours, no duplicated logic.
- **Work identity is always the OpenAlex ID.** Never a title, never a database rowid. Front doors
  translate user input into an ID at the boundary and never handle anything else.

---

## 6. The MCP surface (M2)

The differentiator, so it deserves designing rather than auto-generating. Tools are named for
what a researcher wants, not for internal functions.

| Tool | Arguments | Returns |
| --- | --- | --- |
| `add_paper` | identifier | the work, or candidates to disambiguate |
| `expand_graph` | work_id, budget | job handle, then a summary |
| `find_path` | two identifiers | the citation chain, or "no path within N hops" |
| `neighbours` | work_id, direction | works citing or cited by it |
| `find_gaps` | — | heavily co-cited works missing from the library |
| `search_library` | query, mode | ranked passages with work IDs |
| `ask_library` | question | answer plus resolvable citations |
| `trace_claim` | claim text | descent chain, earliest source first *(M6)* |
| `library_stats` | — | counts, coverage, OA share |

Three design rules for this surface:

1. **Never return an unbounded list.** An assistant given 5,000 works will spend its context on
   them and lose the thread. Cap, rank, and say what was omitted.
2. **Every returned work carries its OpenAlex ID and OA status.** The ID makes follow-up calls
   possible; the OA status stops the assistant from claiming it can read a paper it cannot.
3. **Stubs are labelled as stubs.** A work with no title must never be presented as if the
   library knows something about it.

---

## 7. Jobs and concurrency

Long operations are unavoidable: expanding 500 nodes takes roughly a minute, fetching a hundred
PDFs several minutes, embedding a large library considerably longer. A CLI can block. A web UI
cannot. MCP has timeouts.

**Design.** A `job` table and a single background worker **goroutine** inside the same process.

```sql
job(id, kind, args_json, state, progress, total, error, started_at, finished_at)
```

- Jobs are **resumable**, not restartable — progress lives in the domain tables (`hydrated`,
  `fetched_refs`, `pdf_sha256`), not in the job row. Killing the process mid-job loses nothing
  but the current batch.
- **One writer.** SQLite permits one writer at a time, so the worker goroutine holds the write
  path and front doors read. Go makes this easy to violate by accident, so enforce it
  structurally: two `*sql.DB` handles, the write one pinned to `SetMaxOpenConns(1)`, WAL on, and
  a `busy_timeout` set. This is the most likely source of `database is locked` bugs here.
- **`context.Context` everywhere.** Cancelling an expansion must actually stop the goroutine, not
  just stop waiting for it.
- **No queue, no scheduler, no broker.** A goroutine and a table. Adding infrastructure would
  break the one-command setup constraint for a problem this does not have.

Revisit when: server mode arrives and several users write concurrently. That is the same trigger
as replacing SQLite, and it is the same migration.

---

## 8. Caching

Three caches, three different reasons, three different lifetimes.

| Cache | Keyed by | Lifetime | Why |
| --- | --- | --- | --- |
| HTTP responses | full URL | manual clear | Debugging loops re-run the same expansion dozens of times |
| Embeddings | chunk id | forever, in `chunk_vec` | Recomputing is the single most expensive operation |
| Answers | question + retrieved chunk ids | 24 h, optional | Cheap protection against a user re-asking |

The embedding "cache" is really storage — it is only a cache in that it is regenerable from the
text. Include the **model name and dimension** in the schema. Changing embedding model
invalidates every vector, and without recording which model produced them you will silently mix
two vector spaces and produce nonsense rankings. This is a classic, quiet, expensive bug.

---

## 9. Error handling and the degradation ladder

The product promise is "free and local." That means every layer must degrade rather than fail.

| What is missing | What still works |
| --- | --- |
| Network | Everything already in the library: graph, keyword search, vector search, browsing |
| Ollama present but no chat model | All retrieval including semantic. Ranked passages instead of prose |
| **Ollama not installed** | Keyword search plus graph expansion. This is now the common case, not an edge case — say so in the interface |
| PDF unavailable (paywalled) | Full metadata and the complete graph. The paper is a node without text |
| OpenAlex down | The entire local library. Only expansion and adding stop |

**Rule:** a missing optional capability produces a smaller result and a one-line explanation.
It never produces a stack trace, and it never produces silence.

Retry policy lives in one place (`internal/httpx`): honour `Retry-After`, exponential backoff with
jitter on 429 and 5xx, three attempts, then mark the batch failed and continue. A single bad
batch must never kill a 500-node run.

---

## 10. Load estimation

Orders of magnitude, to size decisions rather than to be precise.

| | Typical library | Heavy user |
| --- | --- | --- |
| Works in graph | 5,000 | 50,000 |
| Edges | ~200,000 | ~2,000,000 |
| With OA full text (~40%) | 2,000 | 20,000 |
| PDF storage | ~4 GB | ~40 GB |
| Text chunks | ~50,000 | ~500,000 |
| Embeddings (384-dim, float32) | ~75 MB | ~750 MB |
| `library.db` total | ~300 MB | ~3 GB |

Comfortable for SQLite in both columns. Two observations that follow from the numbers:

- **Blobs dominate storage by an order of magnitude.** Which is exactly why they are not in the
  database — otherwise every backup copies 40 GB.
- **Embedding is the only genuinely slow operation.** Half a million chunks on a laptop CPU is a
  long background job. It must be resumable, it must show progress, and it must be interruptible.
  Design for that in M4 rather than discovering it.

---

## 11. Extension points

Three adapter interfaces, each with exactly one implementation at v1.0. The point is not
speculative generality — it is that each has a *known* second implementation coming.

| Interface | v1.0 | Known next |
| --- | --- | --- |
| `internal/sources` | OpenAlex | Crossref, arXiv direct, Zotero import |
| `internal/embed` | Ollama HTTP | pure-Go ONNX, to restore one-command setup |
| `internal/llm` | Ollama or user API key | anything OpenAI-compatible |

Everything else is concrete. Resist adding a fourth.

---

## 12. Trade-offs, stated plainly

| Choice | Bought | Paid |
| --- | --- | --- |
| SQLite over a graph database | One-command setup; graph, keyword and vector search in one file | More SQL to write; no native traversal syntax; one writer |
| Filesystem blobs over database blobs | Small backups; users can open their PDFs | Two things to keep consistent instead of one |
| Three front doors, one core | MCP costs a week, not a rewrite | Constant discipline to keep logic out of routes |
| Thread-and-table jobs over a broker | No infrastructure | No distribution; one machine only |
| Stub nodes | Free deduplication and free ranking | Every read path must handle a work with no title |
| Open access only | Legal by construction | A large share of papers will never have full text |
| Go over Python | One binary users double-click; no runtime to install | Weak PDF extraction; embeddings pushed behind Ollama; `quelle` unusable |
| Embeddings via Ollama | Keeps the binary static and cgo-free | A second install for the full experience |
| Local models by default | Genuinely free to run | Slower, and weaker than the best hosted models |

---

## 13. What to revisit, and the trigger for each

Not "someday" — a specific signal for each.

| Revisit | Trigger |
| --- | --- |
| Postgres + pgvector instead of SQLite | Shared-lab mode is scheduled, **or** measured retrieval latency exceeds 500 ms |
| Neo4j as an optional backend | A user wants graph queries deeper than about 6 hops, repeatedly |
| Denormalise in-graph in-degree into a column | Frontier selection shows up in a profile |
| Approximate nearest neighbour instead of exact | Vector search exceeds 200 ms — roughly a million chunks |
| A real job queue | Server mode with concurrent users |
| A learned relevance model | Graph-based ranking plateaus on the Phase 4 test set |
| Pure-Go ONNX embeddings instead of Ollama | Users report the Ollama install as the reason they stopped |
| GROBID for reference parsing | A field turns out to have poor OpenAlex reference coverage — see spike 1 |

**The rule underneath all of these:** every one is a real improvement and every one costs setup
friction, complexity, or both. None of them get adopted before their trigger fires. A tool that
nobody can install is not made better by being architecturally superior.
