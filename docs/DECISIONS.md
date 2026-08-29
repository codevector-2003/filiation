# Decision log

Short records of what was decided, what was rejected, and why. Add to the bottom.

---

## D1 — Get reference data from OpenAlex, do not parse PDFs

**Decided:** 29 Aug 2026

OpenAlex returns already-resolved reference lists (`referenced_works`) as work IDs. Free, no API
key, 100k credits/day, 100 req/s. Send `mailto=` to join the polite pool.

**Rejected:** parsing reference lists out of PDFs with GROBID. Reference extraction has real,
documented error rates, and those errors compound across a graph. GROBID stays as a later
fallback for works with no DOI and no index entry.

---

## D2 — Deduplicate on the OpenAlex work ID

**Decided:** 29 Aug 2026

The same paper exists as an arXiv preprint, a conference paper and a journal article, with
different DOIs and drifting titles. OpenAlex already merges most of these. Using its ID as the
primary key borrows that entity resolution instead of rebuilding it.

**Fallback order** for works OpenAlex does not know: DOI, then normalised title + year + first
author surname.

---

## D3 — SQLite for everything, not Neo4j

**Decided:** 29 Aug 2026

The constraints "free, open source, one-command setup, local first" rule out anything needing a
server process before the user sees a result. SQLite gives graph edges, FTS5 keyword search and
`sqlite-vec` vectors in one file with no server.

**Rejected:** Neo4j. Good database, wrong for v1 — it needs a server running before anything
works, which breaks setup for exactly the non-technical researchers we want. Community Edition is
GPLv3. Revisit as an optional backend for shared-lab server mode.

**Consequence:** all storage sits behind one interface so Postgres + pgvector or Neo4j can be
swapped in later without rewriting the app.

---

## D4 — PDFs on the filesystem, not in a database or the git repo

**Decided:** 29 Aug 2026

Files are named by their SHA-256; the database stores the hash, source URL, OA status and licence.
Free deduplication, easy backup, easy to serve.

**Rejected:** a document database. Document databases store JSON records, not binary files.
MongoDB also ships under SSPL, which the OSI has not approved as an open-source licence.

**Rejected:** committing PDFs to git. Binary blobs make the repo unusable within months.

---

## D5 — Open-access full text only

**Decided:** 29 Aug 2026

Fetch through OpenAlex OA locations and Unpaywall. Never scrape publisher sites; never use a
user's institutional credentials. This is a permanent constraint on the product, not a v1 limit.

**Consequence:** a large share of papers will have metadata but no full text. The interface must
show OA status everywhere so this never surprises anyone.

---

## D6 — Name: Filiation

**Decided:** 29 Aug 2026

In textual criticism, *filiation* is the work of establishing which manuscript descended from
which. It names the process rather than the field, which makes it more specific than the
alternatives, and it is exactly what this tool does with papers.

Package `filiation`, CLI command `fil`. Confirmed unclaimed on PyPI, and no software product
uses the name.

**Superseded: Stemmatics.** Chosen first, then dropped — stemmatics.io is a live research data
provenance platform in early access. That is adjacent to this project's space, so the clash was
substantive rather than merely phonetic.

**Also considered:** Catenary (free on PyPI, sounds more like a product, less literal), Hypotext
(free, exact meaning, but one letter from "hypertext"), Collation (free, perfect meaning, but
collides with the SQL term in a project built on SQLite), Wellhead (free, strong oil and gas
association), Headwater (free on PyPI but headwatertech.io exists).

**Note on spelling:** the word has one L. `fillation` is also unclaimed on PyPI if the doubled
spelling is ever preferred, but it loses the meaning that made the name worth having.

**Still to check:** npm, and the `.dev` / `.org` domain.

---

## D7 — Phase 1 architecture

**Decided:** 29 Aug 2026

Six ADRs covering the core graph: one SQL module without an ORM, budgeted best-first
expansion with batched hydration, stub nodes recorded before their targets are fetched,
a polite-pool HTTP layer with a token bucket and response cache, no silent acceptance of
title search results, and one library per user rather than per directory.

Full reasoning, cost model, failure modes, test strategy and week-by-week plan:
[`ARCHITECTURE_PHASE1.md`](ARCHITECTURE_PHASE1.md).

**Consequence:** `schema.sql` changed — `work.title` is now nullable and the table gained
`hydrated`, `unresolved`, `depth` and `is_seed`, plus a partial index for frontier selection.

**Unverified:** the cost model assumes batch-fetching works by OpenAlex ID with 50 IDs per
request. The documentation contradicts itself on this, on `per_page`, and on the real rate
limit. Four spikes in week 1 must confirm them before the design is trusted.
