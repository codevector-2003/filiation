<p align="center">
  <img src="docs/assets/banner.jpg" width="100%" alt="Filiation: follow the citations back, see where an idea came from">
</p>

<h1 align="center"><img src="docs/assets/logo.svg" width="36" alt=""> Filiation</h1>

<p align="center">
  <strong>A citation graph you build and keep on your own machine.</strong><br>
  Give it one paper. It follows the references, maps what cites what, and shows you where an idea came from.
</p>

<p align="center">
  <a href="https://github.com/codevector-2003/filiation/releases/tag/v0.3.0"><img src="https://img.shields.io/badge/release-v0.3.0-6366f1" alt="Release v0.3.0"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/licence-Apache--2.0-6366f1" alt="Licence: Apache-2.0"></a>
  <img src="https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26">
  <img src="https://img.shields.io/badge/platforms-Windows%20%C2%B7%20macOS%20%C2%B7%20Linux-64748b" alt="Windows, macOS, Linux">
  <img src="https://img.shields.io/badge/cgo-none-22c55e" alt="No cgo">
  <a href="https://openalex.org"><img src="https://img.shields.io/badge/data-OpenAlex-f97316" alt="Data: OpenAlex"></a>
</p>

<p align="center">
  <a href="docs/GETTING_STARTED.md">Getting started</a> ·
  <a href="docs/COMMANDS.md">Commands</a> ·
  <a href="docs/MCP.md">AI assistants</a> ·
  <a href="docs/HOW_IT_WORKS.md">How it works</a> ·
  <a href="#roadmap">Roadmap</a> ·
  <a href="#documentation">Docs</a>
</p>

<p align="center">
  <img src="docs/assets/demo.svg" width="860" alt="A terminal session: fil add, fil expand, fil path, fil stats and fil export, with real output">
</p>

---

In textual criticism, **filiation** is the work of establishing which manuscript was copied from which, reconstructing the lines of descent between surviving texts. This tool does the same for research papers. Start from one paper, follow its citations back, and see the literature it grew out of.

Most citation-map tools are session-shaped: you run a query, get a picture, and close the tab. Filiation builds **your** map instead. It keeps growing as you add papers, lives in a folder you own, and can be exported to any tool you like.

## Highlights

- **Best-first expansion under a budget.** The papers your library cites most are fetched first, so 500 works cover the literature that matters rather than whatever happened to be nearby. Stop at any time; the next run carries on where this one left off.
- **Lineage, not just links.** `fil path` finds how one paper descends from another through its references. Citation cycles are real, and the search is safe on them.
- **The papers themselves, searchable.** `fil fetch` downloads the legal open-access PDFs into readable folders and reads them, so `fil search` finds a passage and names its page. PDFs you already have are identified and filed for you.
- **The sentence behind each citation.** fil keeps the sentence each reference is cited in, so you can see why one paper cites another.
- **Duplicates handled carefully.** The same paper under two IDs, or a merged record, becomes one node. Papers that only *share a title* are reported for you to judge and never merged automatically: a book review carries the title of the book it reviews.
- **Honest about the data.** Every run reports how many works came with a reference list, and why that varies by field.
- **One binary, nothing to install.** A single file for Windows, macOS or Linux. No runtime, no database server, no Docker, no account, no API key.
- **Local and legal.** Your library never leaves your machine. Reference data comes from [OpenAlex](https://openalex.org), which is open, and full text only ever comes from open-access sources.

## See it

<p align="center">
  <img src="docs/assets/graph.svg" width="860" alt="The citation graph fil built from one seed paper: 472 works, coloured by year, sized by how often the library cites them">
</p>

<p align="center"><sub>Real output: one run of <code>fil expand</code> from <em>The state of OA</em> (2018), laid out from <code>fil export graphml</code>.<br>The early open-access literature (blue) sits on the left; the recent work around the seed (pink) sits on the right.</sub></p>

## Quick start

Download the archive for your system from **[Releases](https://github.com/codevector-2003/filiation/releases)**, unpack it, and run `fil`. Then:

```sh
fil add 10.7717/peerj.4375          # a DOI, arXiv ID, PMID, OpenAlex ID, link, or title
fil expand                          # follow the references, most-cited first
fil fetch                           # download the open-access PDFs, and read them
fil search "citation advantage"     # find passages, with their paper and page
```

Or run `fil` on its own to keep it open and type one command after another, with `/` for the list.

Building from source, where your library lives, and the next steps: **[Getting started](docs/GETTING_STARTED.md)**. Every command and option: **[Commands](docs/COMMANDS.md)**.

## Use it from an AI assistant

`fil mcp` lets Claude, GitHub Copilot, Cursor, Gemini CLI or any other MCP client use your library directly: add papers, grow the graph, search the PDFs and quote passages with their page, and answer *"how does this paper descend from that one?"* against a map you own. For Claude Code:

```sh
claude mcp add filiation -- fil mcp
```

Set-up for other clients, all eight tools, limits and troubleshooting: **[MCP guide](docs/MCP.md)**.

## Roadmap

| Milestone | | Status |
| --- | --- | --- |
| **M0: Skeleton** | `fil add`: any identifier to a stored paper and its references | Done |
| **M1: The graph** | Budgeted expansion, dedup, `path`, `neighbours`, `stats`, GraphML export, cross-platform release | Done, v0.1 |
| **M2: MCP server** | Let AI assistants query your library directly | Done, v0.2 |
| **M3: Papers on disk** | Open-access PDFs, full text, and the sentence around each citation | Done, v0.3 |
| **M4: Retrieval and answers** | Search across everything you've read, answers with citations | Planned |
| **M5: Desktop app** | The same library in a desktop window, for people who don't use a terminal | In progress |
| **M6: Claim genealogy** | Why each paper cites another, and tracing a claim to its earliest source | Planned |

## Documentation

**Using fil**

- [Getting started](docs/GETTING_STARTED.md): install, first steps, where your library lives
- [Commands](docs/COMMANDS.md): every command and its options
- [AI assistants (MCP)](docs/MCP.md): set-up, tools, limits, troubleshooting
- [How it works](docs/HOW_IT_WORKS.md): the design in brief, and how much data to expect in your field

**Building fil**

- [Status](docs/STATUS.md): where the project is, what's done, what's next
- [Architecture](docs/ARCHITECTURE.md): whole-system design
- [Decisions](docs/DECISIONS.md): decisions made, and options rejected, with reasons
- [Spikes](docs/SPIKES.md) and [Validation](docs/VALIDATION.md): measured answers from live data
- [CLAUDE.md](CLAUDE.md): working context, hard rules and build order

## Contributing

Issues and pull requests are welcome. Before a larger change, read [Status](docs/STATUS.md) and the hard rules in [CLAUDE.md](CLAUDE.md). A few are non-negotiable:

- no cgo in `fil` itself (the desktop app shell is the one exception, D24);
- no paywalled full text, ever;
- no reference parsing;
- no network access in unit tests.

`go test ./...` runs the whole suite against recorded OpenAlex responses.

## Acknowledgements

Reference data comes from [OpenAlex](https://openalex.org), an open index of the world's research. SQLite runs in pure Go through [`ncruces/go-sqlite3`](https://github.com/ncruces/go-sqlite3). PDF text comes from [PDFium](https://pdfium.googlesource.com/pdfium/), compiled to WebAssembly by [`klippa-app/go-pdfium`](https://github.com/klippa-app/go-pdfium) and run by [`wazero`](https://github.com/tetratelabs/wazero). The command line is built with [`spf13/cobra`](https://github.com/spf13/cobra).

## Licence

[Apache License 2.0](LICENSE). Use it, change it, and build on it, including commercially; keep the notices in [`NOTICE`](NOTICE). The licence includes an explicit patent grant from contributors.
