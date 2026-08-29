# Filiation

A citation graph and retrieval engine that researchers can run on their own machine.

Give it one paper. It follows the reference list, builds a map of what cites what, keeps the
open-access PDFs, and lets you ask questions across everything you have read.

**Filiation** is the term from textual criticism for establishing which manuscript descended
from which — working out the actual lines of copying between surviving texts. That is what this
tool does for papers: it reconstructs where a claim came from by following the citations back.

- Package name: `filiation`
- CLI command: `fil`
- Status: pre-M0. Nothing is built yet.

---

## The one sentence that matters

The graph is not the product. The answers are. The graph is what makes the answers better
than plain search.

---

## Locked decisions

| Decision | Consequence |
| --- | --- |
| Serve developers **and** non-coding researchers from day one | One package, one process, two front doors: MCP + CLI, and a local web page |
| v1 includes the full retrieval engine with Q&A | The graph alone does not count as done. Ships in stages, not one release |
| Local first, optional server later | All storage sits behind one interface so the backend can be swapped |
| Open source, free to run, one-command setup | Rules out anything needing a server before the user sees a result |

---

## Hard rules — do not violate these

1. **Never download a paywalled PDF.** No scraping publisher sites. No using anyone's
   institutional login. Open access only, via OpenAlex OA locations and Unpaywall.
   This is a permanent line, not a v1 limitation.
2. **Do not write a reference parser.** OpenAlex already returns resolved reference lists
   (`referenced_works`). PDF reference extraction has real error rates that compound across a
   graph. GROBID only as a fallback for works with no DOI and no index entry, and only later.
3. **Do not build an LLM-extracted knowledge graph.** Our citation edges are ground truth.
   Adding guessed entities and relations on top makes the graph worse and costs a lot.
4. **Do not add a service that must be running before the tool works.** No Docker requirement,
   no database server, no account. If a change breaks `pip install` then one command, reject it.
5. **Deduplicate on `openalex_id`.** The same work exists as preprint, conference paper and
   journal article with different DOIs. OpenAlex already merges most of these. Use its ID as the
   primary key or the graph will quietly rot.

---

## Stack and why

| Layer | Choice | Why |
| --- | --- | --- |
| Metadata + graph | SQLite, one file | No server. Ships with Python. Recursive CTEs handle 2–3 hop traversal at this scale |
| Keyword search | SQLite FTS5 | Built in, no extra dependency |
| Vector search | `sqlite-vec` | Same file, no server, MIT/Apache. **Pre-1.0 — pin the exact version** and keep it behind a thin wrapper |
| Graph algorithms | networkx / igraph | Load edges into memory for PageRank or communities. 100k nodes fits in RAM |
| PDF files | Filesystem, named by SHA-256 | Filesystem is the best store for binary files. Free dedup, easy backup |
| Reference data | OpenAlex API | Free, no key needed, resolved reference lists, merges duplicate versions |
| Open-access PDFs | OpenAlex OA links + Unpaywall | The only legal route |
| Embeddings | Local model on CPU | The free path must work with no API key |
| Answers | Bring your own key, or Ollama | Users who will not pay must still get working answers |
| Graph in browser | Sigma.js (WebGL) | D3 force layout stalls past ~2,000 nodes |
| Server mode (later) | Postgres + pgvector | Behind the same storage interface |

### Rejected, with reasons

- **Neo4j** — good database, wrong for v1. Needs a server running before anything works, which
  breaks one-command setup for non-technical users. Community Edition is GPLv3. Revisit only for
  the shared-lab server mode, as an optional backend.
- **MongoDB / any document database for file storage** — document databases store JSON records,
  not binary files. For PDFs the filesystem wins on every axis. MongoDB also ships under SSPL,
  which the OSI has not approved as an open-source licence.
- **Putting PDFs in the git repo** — binary blobs make the repo unusable within months.

---

## API notes

- **OpenAlex**: no API key required. Free tier is 100,000 credits/day and 100 requests/second.
  Always send `mailto=<contact>` to join the polite pool. List requests cost 10 credits, single
  record requests cost 1 — prefer batched filters over per-work lookups.
- **Expansion blows up fast**: ~40 references per paper means depth 2 ≈ 1,600 nodes and
  depth 3 ≈ 64,000. Expansion must always take a node budget and a priority order. Never
  breadth-first without a limit.

---

## Data model

See `src/filiation/schema.sql` for the DDL. The one non-obvious column is `cites.context` —
the sentence around the citation marker in the citing paper. Nobody provides this for free and
it is what makes retrieval better than everyone else's.

---

## Build order

Current position: **pre-M0**.

- [ ] **M0 — Skeleton** (1–2 weeks). Package layout, config, SQLite schema, one command that
      takes a DOI, fetches from OpenAlex, stores one node.
      *Done when:* `fil add 10.1145/3292500` writes a row and prints the title.
- [ ] **M1 — The graph** (2–3 weeks). Reference expansion with depth and node budget. Dedup on
      OpenAlex ID. Commands: expand, neighbours, path, export GraphML. **First release.**
      *Done when:* one seed paper gives a clean 500-node graph with no duplicates, exportable to Gephi.
- [ ] **M2 — MCP server** (~1 week). Expose M1 commands as MCP tools. Cheap because the logic
      exists, and it is the part no existing citation-map tool has.
      *Done when:* an assistant can answer "what connects these two papers in my library".
- [ ] **M3 — Papers on disk** (2–3 weeks). OA PDF fetch, content-addressed storage, text
      extraction, chunking, FTS5 search. **Capture citation context sentences here** — the text is
      already parsed, so the extra cost is small and the payoff is large.
      *Done when:* keyword search returns a passage and you can open the page it came from.
- [ ] **M4 — Retrieval and answers** (3–4 weeks). Local embeddings, vector search, hybrid
      retrieval: seed by meaning and keyword, expand along weighted citation edges, rerank,
      answer with citations. Weight edges by co-citation before anything cleverer.
      *Done when:* an answer cites three papers you actually have, and the citations are correct.
- [ ] **M5 — Web interface** (3–4 weeks). One command starts a local server and opens a browser.
      Graph view, search, reader, notes.
      *Done when:* someone installs it, runs one command, and adds a paper without reading docs.
- [ ] **M6 — Claim genealogy** (3–4 weeks). Classify citation intent from the M3 context
      sentences, then trace a claim backwards to the paper that first made it. **The differentiator.**
      *Done when:* the tool shows a widely repeated number tracing back to one small old study.

Roughly four to five months part-time to M5, six to M6. If time runs short, ship M1–M4 as a
developer tool and move the web interface after M6 — the differentiator matters more than the
second audience.

---

## Known risks

- **Full-text coverage will disappoint people.** A large share of papers have no legal free PDF.
  Show OA status on every node from day one so nobody is left guessing.
- **Citation edges are topically noisy.** A paper often cites another for a dataset, not an idea.
  Blind expansion degrades results with distance. Edge weighting in M4 is not polish — it is what
  makes retrieval work at all.
- **`sqlite-vec` is pre-1.0.** Breaking changes are promised. Pin the version.
- **Dependency licences can force your hand.** Some popular PDF libraries are AGPL, which would
  push this whole project to AGPL. Check every dependency licence before adding it.

---

## Conventions

- Python 3.11+
- `ruff` for lint and format, `pytest` for tests
- Type hints on public functions
- No network calls in unit tests — record fixtures
- Every external API call goes through one module per source (`sources/openalex.py`, etc.)
  so rate limiting and caching live in one place

---

## Prior art to check before writing code

**`quelle` on PyPI** (MIT, actively maintained) is a Python CLI that takes a DOI, arXiv ID, ISBN
or title, fetches metadata from OpenAlex, Crossref, Semantic Scholar, arXiv, Unpaywall, Open
Library and Google Books, normalises to JSON, caches in SQLite, and optionally downloads
open-access PDFs.

That overlaps heavily with M0 ingestion and M3 PDF fetching. **Read it before week one.** Either
adopt it as a dependency and save weeks of the least interesting work, or learn the failure modes
of that layer for free. Do not rebuild it unexamined. It does none of the differentiated work —
no graph, no citation context, no claim genealogy — so it is a component, not a competitor.

---

## Still open

1. **Licence** — MIT/Apache-2.0 for widest adoption, or AGPL to stop a company hosting it as a
   paid service. Dependency choices may decide this.
2. **Zotero** — read from a user's existing library? Cheapest route to real users. Decide early.
3. **Who maintains it after the degree?** Changes how much to invest in docs and tests now.
