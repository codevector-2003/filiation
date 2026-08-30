# Scope — v1

Draft 1 · 29 August 2026

## What we are building

A tool that turns a researcher's reading into a database they own. You point it at a paper. It
asks OpenAlex for that paper's reference list, adds each reference as a node, and draws the
citation links. If a paper is already there, it links to the existing node instead of adding a
copy. Over months this becomes a personal map of a field.

On top of that map sits a retrieval engine. Because the links are real citations and not guesses,
it can do things a normal search box cannot: trace a claim back to the paper that first made it,
or find the shortest chain of citations between two papers.

> The graph is not the product. The answers are. The graph is what makes the answers better than
> plain search.

## Constraints

- Open source
- Free to run
- Single binary, no runtime to install
- Local first

Written in Go — see D8 in [`DECISIONS.md`](DECISIONS.md).

## In scope for v1

- **Ingest** a paper by DOI, arXiv ID, title, or uploaded PDF
- **Expand** the graph along references, with a budget so it does not run away
- **Deduplicate** preprint, conference and journal versions into one node
- **Fetch** open-access PDFs only, and record where each came from
- **Extract** text, split into chunks, and store the sentence around each citation
- **Search** by keyword and by meaning, then expand along citation links
- **Answer** questions with citations pointing back to real papers in the graph
- **Show** the graph in a browser and let a person click through it
- **Export** everything: GraphML, JSON, BibTeX. No lock-in
- **Serve** the same functions over MCP so an AI assistant can use them

## Explicitly not v1

- **Any paywalled PDF.** No scraping publisher sites, no using someone's university login.
  A hard line, not a later feature.
- **Our own reference parser.** OpenAlex already resolved the references. GROBID only as a
  fallback, later.
- **An AI-extracted knowledge graph.** Our links are ground truth. Guessed entities on top make
  the graph worse.
- **Accounts, cloud hosting, sync.** Nothing leaves the machine unless the user asks.
- **A recommendation model.** Graph maths first; machine learning only if maths is not enough.
- **A writing assistant.** Different product.
- **Replacing Zotero.** Read from it, do not compete with it.
- **Mobile.**

## Risks

| Severity | Risk | Mitigation |
| --- | --- | --- |
| High | Full-text coverage disappoints people — many papers have no legal free PDF | Show OA status on every node from day one; never leave someone guessing why a paper has no text |
| Medium | **OpenAlex's real free allowance is 1,000 credits/day, not 100,000**, and its pricing is now USD-denominated (spike 3, D11) | Batching keeps a 500-node graph at 5 credits. Read `X-RateLimit-Remaining` from responses rather than trusting a compiled-in constant |
| High | Scope is large for one person — both audiences plus full Q&A is a real six months | Every milestone ships something usable, so an interruption leaves a working tool |
| **High** | **The graph dies after one hop in the humanities** — 84% of works on the frontier have no reference list (spike 4, D12) | Show reference coverage on every node, like OA status. Say so in the README rather than letting people discover it |
| Medium | **Vector search has no ANN index** — `vec1` is a brute-force scan, ~3 s over a 50k-chunk library (spike 6, D13) | Pre-filter by graph and FTS5 so the scan sees thousands, not everything. This is the hybrid design, not a workaround |
| Medium | Go's PDF text extraction is weaker than Python's | Deferred to a Phase 3 bake-off; bundling `pdftotext` is the fallback |
| Medium | Semantic search requires Ollama | Degrade to keyword plus graph, and say so plainly in the interface |
| Medium | Dependency licences force your hand | Check every licence before adding it. Also: no dependency may require cgo |
| Medium | Citation links are topically noisy — papers cite each other for datasets, not ideas | Edge weighting in M4 is required, not polish |

## Still open

1. **Licence.** MIT/Apache-2.0 for widest adoption, or AGPL to stop a company hosting it as a
   paid service. Dependency choices may decide this.
2. **Zotero.** Reading an existing Zotero library is the cheapest route to real users.
3. **Maintenance after the degree.** Changes how much to invest in docs and tests now.
