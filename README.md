<p align="center">
  <img src="docs/assets/banner.jpg" width="100%" alt="Filiation: follow the citations back, see where an idea came from">
</p>

<h1 align="center"><img src="docs/assets/logo.svg" width="36" alt=""> Filiation</h1>

<p align="center">
  <strong>A citation graph you build and keep on your own machine.</strong><br>
  Give it one paper. It follows the references, maps what cites what, and shows you where an idea came from.
</p>

<p align="center">
  <a href="https://github.com/codevector-2003/filiation/releases/tag/v0.2.0"><img src="https://img.shields.io/badge/release-v0.2.0-6366f1" alt="Release v0.2.0"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/licence-Apache--2.0-6366f1" alt="Licence: Apache-2.0"></a>
  <img src="https://img.shields.io/badge/go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26">
  <img src="https://img.shields.io/badge/platforms-Windows%20%C2%B7%20macOS%20%C2%B7%20Linux-64748b" alt="Windows, macOS, Linux">
  <img src="https://img.shields.io/badge/cgo-none-22c55e" alt="No cgo">
  <a href="https://openalex.org"><img src="https://img.shields.io/badge/data-OpenAlex-f97316" alt="Data: OpenAlex"></a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#commands">Commands</a> ·
  <a href="#use-it-from-an-ai-assistant">AI assistants</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="#know-this-before-you-start">Coverage</a> ·
  <a href="#roadmap">Roadmap</a> ·
  <a href="#documentation">Docs</a>
</p>

<p align="center">
  <img src="docs/assets/demo.svg" width="860" alt="A terminal session: fil add, fil expand, fil path, fil stats and fil export, with real output">
</p>

---

In textual criticism, **filiation** is the work of establishing which manuscript was copied from which, reconstructing the lines of descent between surviving texts. This tool does the same for research papers. Start from one paper, follow its citations back, and see the literature it grew out of.

Most citation-map tools are session-shaped: you run a query, get a picture, and close the tab. Filiation builds **your** map instead. It keeps growing as you add papers, lives in one file you own, and can be exported to any tool you like.

## Highlights

- 🧭 **Best-first expansion under a budget.** The papers your library cites most are fetched first, so 500 works cover the literature that matters rather than whatever happened to be nearby. Stop at any time; the next run carries on where this one left off.
- 🌳 **Lineage, not just links.** `fil path` finds how one paper descends from another through its references. Citation cycles are real, and the search is safe on them.
- 🧹 **Duplicates handled carefully.** The same paper under two IDs, or a merged record, becomes one node. Papers that only *share a title* are reported for you to judge and never merged automatically: a book review carries the title of the book it reviews.
- 📊 **Honest about the data.** Every run reports how many works came with a reference list, and why that varies by field.
- 📦 **One binary, nothing to install.** A single file for Windows, macOS or Linux. No runtime, no database server, no Docker, no account, no API key.
- 🔒 **Local and legal.** Your library never leaves your machine. Reference data comes from [OpenAlex](https://openalex.org), which is open, and full text will only ever come from open-access sources.

## See it

<p align="center">
  <img src="docs/assets/graph.svg" width="860" alt="The citation graph fil built from one seed paper: 472 works, coloured by year, sized by how often the library cites them">
</p>

<p align="center"><sub>Real output: one run of <code>fil expand</code> from <em>The state of OA</em> (2018), laid out from <code>fil export graphml</code>.<br>The early open-access literature (blue) sits on the left; the recent work around the seed (pink) sits on the right.</sub></p>

## Quick start

### Download

Get the archive for your system from **[Releases](https://github.com/codevector-2003/filiation/releases)**, unpack it, and run `fil`. There is nothing else to install.

| System | Archive |
| --- | --- |
| Windows | `fil_<version>_windows_amd64.zip` (`arm64` also available) |
| macOS (Apple silicon) | `fil_<version>_darwin_arm64.tar.gz` |
| macOS (Intel) | `fil_<version>_darwin_amd64.tar.gz` |
| Linux | `fil_<version>_linux_amd64.tar.gz` (`arm64` also available) |

### Or build from source

```sh
git clone https://github.com/codevector-2003/filiation
cd filiation
go build ./cmd/fil        # Go 1.26 or later. Pure Go, no C compiler needed.
```

### First steps

```sh
fil add 10.7717/peerj.4375                  # a DOI, arXiv ID, PMID, OpenAlex ID, link, or title
fil expand                                  # follow the references, most-cited first
fil path 10.7717/peerj.4375 W4300313809     # how one paper descends from another
fil export graphml -o library.graphml       # open it in Gephi
```

On the first run fil asks where to keep your library: a folder you can see, `Documents/Filiation/My
Library` unless you choose another. Papers go in its `papers/` folder, one folder per paper with a
readable name such as `Piwowar-2018-The-state-of-OA--F7K2M9QXA`. Move, rename and group those
folders however you like; fil finds them again, and each folder you make becomes a collection.
`fil where` shows where everything is.

## Commands

| Command | What it does |
| --- | --- |
| `fil add <paper>` | Add a paper, plus a record for everything it cites. Takes a DOI, arXiv ID, PMID, OpenAlex ID, a link to any of those, or a title. A title can match several papers, so fil lists them and asks; `--accept-first` is for scripts. |
| `fil expand` | Fetch the works your library cites but doesn't have yet, most-cited first. `--max-nodes` (default 500) and `--max-depth` (default 3) bound the run. |
| `fil neighbours <paper>` | What a paper cites, and what in your library cites it. `--context` shows the sentence each reference is cited in, with its page, once fil has read the paper's PDF. |
| `fil path <a> <b>` | The shortest chain of references from one paper to another. `--any-direction` allows citations either way. |
| `fil stats` | The library at a glance, including reference coverage, PDFs, text and citation sentences. `--duplicates` lists works that share a title; `--why` lists each paper whose PDF gave no text, and why. |
| `fil export graphml` | The graph as GraphML for Gephi, Cytoscape, yEd, NetworkX or igraph. `--include-stubs` adds works known so far only by ID. |
| `fil mcp` | Serve your library to an AI assistant over MCP. Your MCP client runs it. See the [MCP guide](docs/MCP.md). |
| `fil fetch` | Download the legal open-access PDFs of your papers into `papers/` — published version first, then accepted manuscript, then preprint. Never from behind a paywall or a login. 50 a run (`--max`); name a paper to fetch just that one. `--dry-run` says what it would download and why the others have none. Once the downloads are done, it reads their text so `fil search` finds them. |
| `fil list` | Your papers, with their PDF, text and citation sentences beside each. `--seeds`, `--with-pdf`, `--without-text`, `--collection`, `--title`, `--json`. |
| `fil show <paper>` | Everything about one paper: its place in the graph, its PDF or why there is none, its text or why there is none, and the sentences it cites its references in. |
| `fil search <words>` | Find passages in your PDFs by keyword, best first, each with its paper and page. Words match by stem; `"quote"` a phrase; `word*` matches a prefix. Reference lists are left out unless you pass `--references`; `--paper` searches one paper. |
| `fil import [file…]` | File PDFs you already have. With no files, everything in `Inbox/` and any loose PDF in `papers/`. fil finds the identifier in the PDF — the arXiv stamp, the DOI in the header — and files it only if that paper's title and author are in the PDF; otherwise it searches by title and asks you to choose. `--as <DOI>` names it yourself; `--local "<title>"` keeps a paper OpenAlex doesn't know. Files from elsewhere are copied, never moved. `fil fetch` and `fil extract` also file what's in `Inbox/`, so dropping a PDF there is enough. |
| `fil extract` | Read the text of PDFs fil has not read yet — ones you put in `papers/` yourself, say. A scan with no text layer, a gibberish text layer or a damaged file is reported with the reason. `--again` reads everything again. |
| `fil library` | Your library folder and what is in `papers/`. `library folder <paper>` prints a paper's folder, making it if needed; `library scan` finds papers you moved, renamed or deleted; `library new` and `library move` make another library or move this one. `--library <name>` picks a library for any command. |
| `fil where` | Where your library, settings and cache live. |
| `fil cache clear` | Empty the cache of OpenAlex responses. Your library isn't touched. |

Every command has `--help`. Exit codes are distinct per outcome (not found, ambiguous, network, and so on), so scripts can branch on them.

## Use it from an AI assistant

`fil mcp` lets an assistant such as Claude, Cursor or any other MCP client use your library directly. It can add papers, grow the graph, and answer *"how does this paper descend from that one?"* against a map you own.

**Claude Code**

```sh
claude mcp add filiation -- fil mcp
```

**Claude Desktop, Cursor and most other clients:** add this to the client's MCP settings file.

```json
{
  "mcpServers": {
    "filiation": { "command": "fil", "args": ["mcp"] }
  }
}
```

Use the full path to `fil` if it isn't on your `PATH`. Set-up for Cursor, VS Code and others is in the [MCP guide](docs/MCP.md#set-up). The assistant gets five tools:

| Tool | What the assistant can do |
| --- | --- |
| `add_paper` | Add a paper by DOI, arXiv ID, PMID, link or title. A title that matches several papers comes back as a list to choose from, never as a guess. |
| `expand_graph` | Follow references outward, most-cited first, with live progress. |
| `neighbours` | What a paper cites and what cites it: ranked, capped, and saying what was left out. |
| `find_path` | The chain of citations between two papers. |
| `library_stats` | What the library holds, its reference coverage, and possible duplicates. |

Every paper the assistant sees carries its IDs — fil's own, which never changes, and OpenAlex's — and its open-access status. Papers known only by ID are marked as such, so the assistant can't talk about a paper as if it had read it.

📘 **[Full MCP guide](docs/MCP.md)**: set-up for Claude Desktop, Claude Code, Cursor and VS Code; every tool's arguments and results; limits; troubleshooting.

## How it works

<p align="center">
  <img src="docs/assets/how-it-works.svg" width="860" alt="How fil works: one paper, resolved references from OpenAlex, a library on your machine, then path, neighbours and export">
</p>

- **References come from OpenAlex, already resolved.** fil never parses reference lists out of PDFs, a process whose errors compound across a graph.
- **Edges before nodes.** A paper's references become records the moment it's fetched. Deduplication is then a primary-key match, and "which unfetched paper matters most?" becomes a cheap count of how often your library cites it.
- **Every batch is one transaction.** Interrupting a run loses at most the batch in flight, and the library is never half-written.
- **Polite to OpenAlex.** Requests are batched 100 works at a time, rate-limited below the measured limit, retried with backoff, and cached.

The design, and the measurements behind it, are written up in [`docs/`](docs).

## Know this before you start

**The graph is only as deep as the reference data behind it, and that depends on your field.** In the [validation run](docs/VALIDATION.md) (20 seed papers across 5 fields, 500 works each), the share of fetched works that came with a reference list was:

| Medicine | Physics | Computer science | Social sciences | Arts & humanities |
| :---: | :---: | :---: | :---: | :---: |
| 93% | 91% | 88% | 59% | 56% |

Where books and chapters dominate, publishers rarely deposit reference lists, and a random humanities sample fares worse still ([spike 4](docs/SPIKES.md)). fil tells you when a work has no references. That's a gap in the data, not a fault in the tool.

## Roadmap

| | Milestone | |
| --- | --- | --- |
| ✅ | **M0: Skeleton.** `fil add`: any identifier to a stored paper and its references | Done |
| ✅ | **M1: The graph.** Budgeted expansion, dedup, `path`, `neighbours`, `stats`, GraphML export, cross-platform release | Done: **v0.1** |
| ✅ | **M2: MCP server.** Let AI assistants query your library directly | Done: **v0.2** |
| ⏳ | **M3: Papers on disk.** Open-access PDFs, full text, and the sentence around each citation | Next |
| ⏳ | **M4: Retrieval and answers.** Search across everything you've read, answers with citations | |
| ⏳ | **M5: Web interface.** The same library for people who don't use a terminal | |
| ⏳ | **M6: Claim genealogy.** Why each paper cites another, and tracing a claim to its earliest source | |

## Documentation

- [`docs/MCP.md`](docs/MCP.md): using fil from an AI assistant: set-up, tools, limits, troubleshooting
- [`docs/STATUS.md`](docs/STATUS.md): where the project is, what's done, what's next
- [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md): whole-system design
- [`docs/DECISIONS.md`](docs/DECISIONS.md): decisions made, and options rejected, with reasons
- [`docs/SPIKES.md`](docs/SPIKES.md) and [`docs/VALIDATION.md`](docs/VALIDATION.md): measured answers from live data
- [`CLAUDE.md`](CLAUDE.md): working context, hard rules and build order

## Contributing

Issues and pull requests are welcome. Before a larger change, read [`docs/STATUS.md`](docs/STATUS.md) and the hard rules in [`CLAUDE.md`](CLAUDE.md). A few are non-negotiable:

- no cgo;
- no paywalled full text, ever;
- no reference parsing;
- no network access in unit tests.

`go test ./...` runs the whole suite against recorded OpenAlex responses.

## Acknowledgements

Reference data comes from [OpenAlex](https://openalex.org), an open index of the world's research. SQLite runs in pure Go through [`ncruces/go-sqlite3`](https://github.com/ncruces/go-sqlite3). PDF text comes from [PDFium](https://pdfium.googlesource.com/pdfium/), compiled to WebAssembly by [`klippa-app/go-pdfium`](https://github.com/klippa-app/go-pdfium) and run by [`wazero`](https://github.com/tetratelabs/wazero). The command line is built with [`spf13/cobra`](https://github.com/spf13/cobra).

## Licence

[Apache License 2.0](LICENSE). Use it, change it, and build on it, including commercially; keep the notices in [`NOTICE`](NOTICE). The licence includes an explicit patent grant from contributors.
