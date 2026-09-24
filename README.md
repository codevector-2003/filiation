# Filiation

**A citation graph and retrieval engine you run yourself.**

Give it one paper. It follows the references, builds a map of what cites what, keeps the
open-access PDFs, and lets you ask questions across everything you have read.

In textual criticism, *filiation* is the work of establishing which manuscript was copied from
which — reconstructing the lines of descent between surviving texts. This tool does the same for
research papers, and uses those lines to trace a claim back to whoever made it first.

> Status: early development. The first milestone works — `fil add` puts a paper and everything it
> cites into your library. Following those references outward is next.

## What works today

Build it (Go 1.25 or later; there is no release yet):

```
go build ./cmd/fil
```

Then add a paper by DOI, arXiv ID, PubMed ID, OpenAlex ID, a link to any of those, or its title:

```
$ fil add 10.7717/peerj.4375
Added: The state of OA: a large-scale analysis of the prevalence and impact of Open Access articles (2018, article)
       W2741809807 · doi:10.7717/peerj.4375 · PeerJ · gold open access
References: 54 — 54 new to your library, 54 citations recorded.
Library:    55 works (54 not fetched yet), 54 citations.
```

A title can match several papers, so `fil` lists them and asks which one you mean; in a script,
pass `--accept-first`. `fil where` prints where your library lives — the default differs on every
operating system. `fil cache clear` empties the cache of OpenAlex responses without touching your
library.

### Know this before you start

**The graph is only as deep as the reference data behind it, and that depends on your field.**
Reference lists come from [OpenAlex](https://openalex.org), and coverage is very uneven. Among
works reached by following real references, the share with *no* reference list is about 6% in
medicine, 12% in physics and 15% in computer science — but 54% in the social sciences and **84% in
arts and humanities**, where books and chapters dominate and their publishers rarely deposit
reference lists. In those fields the graph often stops after one step. `fil` tells you when a work
has no references; that is a gap in the data, not a fault in the tool.

## Why

Existing citation-map tools are session-shaped: you run a query, you get a picture, you close the
tab. Nothing builds *your* map of everything you have read, growing over years, that an AI
assistant can query directly.

And a citation arrow on its own is nearly worthless. What matters is *why* one paper cites
another — for a method, a dataset, a comparison, or a disagreement. Filiation captures that, and
uses it to trace a claim back to the paper that first made it.

## Design goals

- **Free to run.** No API key required for the core. Local embeddings by default.
- **One binary.** Download and run. No runtime, no database server, no Docker, no account.
- **Local first.** Your library stays on your machine.
- **No lock-in.** Export everything to GraphML, JSON and BibTeX.
- **Legal by construction.** Open-access full text only. Never scrapes paywalled papers.

## Two front doors

- **MCP server + CLI** for developers and AI assistants
- **A local web page** for everyone else

## Planned stack

Written in **Go**, distributed as a single binary — no runtime to install. SQLite for the graph,
FTS5 for keyword search, SQLite's own vector extension for semantic search, plain files on disk
for PDFs, OpenAlex for reference data. One database file, no server.

Semantic search and generated answers use [Ollama](https://ollama.com) if you have it. Without
it, keyword search and the whole citation graph still work.

## Documentation

- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) — whole-system design: layers, contracts, trade-offs
- [`CLAUDE.md`](CLAUDE.md) — full working context, hard rules and build order
- [`docs/SCOPE.md`](docs/SCOPE.md) — what is in and out of v1
- [`docs/DECISIONS.md`](docs/DECISIONS.md) — decisions made and options rejected

## Licence

Not yet chosen. See the open questions in `CLAUDE.md`.
