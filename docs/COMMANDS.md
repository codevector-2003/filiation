# Commands

Every command has `--help`. Exit codes are distinct per outcome (not found, ambiguous, network, and
so on), so scripts can branch on them. `--library <name>` picks a library for any command, and
`--plain` turns colour off.

## Building the graph

| Command | What it does |
| --- | --- |
| `fil add <paper>` | Add a paper, plus a record for everything it cites. Takes a DOI, arXiv ID, PMID, OpenAlex ID, a link to any of those, or a title. A title can match several papers, so fil lists them and asks; `--accept-first` is for scripts. |
| `fil expand` | Fetch the works your library cites but doesn't have yet, most-cited first. `--max-nodes` (default 500) and `--max-depth` (default 3) bound the run. |
| `fil neighbours <paper>` | What a paper cites, and what in your library cites it. `--context` shows the sentence each reference is cited in, with its page, once fil has read the paper's PDF. |
| `fil path <a> <b>` | The shortest chain of references from one paper to another. `--any-direction` allows citations either way. |
| `fil stats` | The library at a glance, including reference coverage, PDFs, text and citation sentences. `--duplicates` lists works that share a title; `--why` lists each paper whose PDF gave no text, and why. |
| `fil export graphml` | The graph as GraphML for Gephi, Cytoscape, yEd, NetworkX or igraph. `--include-stubs` adds works known so far only by ID. |

## Papers and their text

| Command | What it does |
| --- | --- |
| `fil fetch` | Download the legal open-access PDFs of your papers into `papers/`: published version first, then accepted manuscript, then preprint. Never from behind a paywall or a login. 50 a run (`--max`); name a paper to fetch just that one. `--dry-run` says what it would download and why the others have none. Once the downloads are done, it reads their text so `fil search` finds them. |
| `fil import [file…]` | File PDFs you already have. With no files, everything in `Inbox/` and any loose PDF in `papers/`. fil finds the identifier in the PDF (the arXiv stamp, the DOI in the header) and files it only if that paper's title and author are in the PDF; otherwise it searches by title and asks you to choose. `--as <DOI>` names it yourself; `--local "<title>"` keeps a paper OpenAlex doesn't know. Files from elsewhere are copied, never moved. `fil fetch` and `fil extract` also file what's in `Inbox/`, so dropping a PDF there is enough. |
| `fil extract` | Read the text of PDFs fil has not read yet, such as ones you put in `papers/` yourself. A scan with no text layer, a gibberish text layer or a damaged file is reported with the reason. `--again` reads everything again. |
| `fil search <words>` | Find passages in your PDFs by keyword, best first, each with its paper and page. Words match by stem; `"quote"` a phrase; `word*` matches a prefix. Reference lists are left out unless you pass `--references`; `--paper` searches one paper. |
| `fil list` | Your papers, with their PDF, text and citation sentences beside each. `--seeds`, `--with-pdf`, `--without-text`, `--collection`, `--title`, `--json`. |
| `fil show <paper>` | Everything about one paper: its place in the graph, its PDF or why there is none, its text or why there is none, and the sentences it cites its references in. |

## AI assistants

| Command | What it does |
| --- | --- |
| `fil mcp` | Serve your library to an AI assistant over MCP. Your MCP client runs it. See [MCP.md](MCP.md). |

## Your library, settings and cache

| Command | What it does |
| --- | --- |
| `fil library` | Your library folder and what is in `papers/`. `library folder <paper>` prints a paper's folder, making it if needed; `library scan` finds papers you moved, renamed or deleted; `library new` and `library move` make another library or move this one. |
| `fil where` | Where your library, settings and cache live. |
| `fil theme [name]` | Show the colour schemes, or choose one: `lineage` (the default), `manuscript`, `graph`, or `plain`. `--background dark` or `light` if fil guesses your terminal's background wrong. Colour only reaches a terminal: piped output, `--json` and files stay plain, as does everything when `NO_COLOR` is set. `FIL_THEME` overrides the choice for one run. |
| `fil cache clear` | Empty the cache of OpenAlex responses. Your library isn't touched. |
