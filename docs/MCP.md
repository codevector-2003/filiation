# Using Filiation from an AI assistant (MCP)

`fil mcp` runs an [MCP](https://modelcontextprotocol.io) server over your library, so that an
assistant such as Claude, Cursor or GitHub Copilot can add papers, grow the citation graph and
answer questions like *"how does this paper descend from that one?"* against a map you own.

The server runs on your machine and reads your own library file. The only network traffic it
causes is the same as the CLI's: requests to OpenAlex when a paper is added or the graph is
expanded.

> `fil mcp` is in every release from **v0.2** on.

- [Set-up](#set-up)
- [What the assistant can do](#what-the-assistant-can-do)
- [Tool reference](#tool-reference)
- [Limits, and why they exist](#limits-and-why-they-exist)
- [Troubleshooting](#troubleshooting)

---

## Set-up

Every client needs the same thing: run the command `fil` with the argument `mcp`. Your client
starts it, talks to it over standard input and output, and stops it. You never run `fil mcp` in a
terminal yourself.

If `fil` is not on your `PATH`, use its full path, for example `C:\Tools\fil.exe` or
`/usr/local/bin/fil`. In JSON, a Windows path needs doubled backslashes: `"C:\\Tools\\fil.exe"`.

### Claude Code

```sh
claude mcp add filiation -- fil mcp
```

Add `--scope user` to make it available in every project, not just the current one. Check it with
`claude mcp list`.

### Claude Desktop

Open *Settings → Developer → Edit Config*, or edit the file directly:

- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`

```json
{
  "mcpServers": {
    "filiation": { "command": "fil", "args": ["mcp"] }
  }
}
```

Restart Claude Desktop afterwards.

### Cursor

Add the same block to `~/.cursor/mcp.json` (every project) or `.cursor/mcp.json` (one project):

```json
{
  "mcpServers": {
    "filiation": { "command": "fil", "args": ["mcp"] }
  }
}
```

### VS Code (GitHub Copilot)

In `.vscode/mcp.json`:

```json
{
  "servers": {
    "filiation": { "type": "stdio", "command": "fil", "args": ["mcp"] }
  }
}
```

### Any other client

Anything that can launch a stdio MCP server works: command `fil`, arguments `["mcp"]`.

### Which library it uses

The same one as the CLI: the location `fil where` prints. If you have never used fil, the first
start creates the library in the default location without asking, because there is no terminal to
ask in. To use a different library, add `--db`:

```json
{ "command": "fil", "args": ["mcp", "--db", "/path/to/library.db"] }
```

You can keep using the CLI while an assistant is connected. Both work on the same file safely.

---

## What the assistant can do

Five tools, named for what a researcher wants rather than for how fil works inside:

| Tool | In a sentence | Changes the library |
| --- | --- | --- |
| [`add_paper`](#add_paper) | Add a paper and a record of everything it cites | Yes |
| [`expand_graph`](#expand_graph) | Fetch the works the library cites but doesn't have yet, most-cited first | Yes |
| [`neighbours`](#neighbours) | What a paper cites, and what in the library cites it | No |
| [`find_path`](#find_path) | The chain of citations between two papers | No |
| [`library_stats`](#library_stats) | What the library holds, and how complete its reference data is | No |

Things to ask once it's connected:

- *"Add 10.7717/peerj.4375 to my library and grow the graph by 200 papers."*
- *"Which of its references are the most cited?"*
- *"Does it build on 'Anatomy of green open access'? Through what chain?"*
- *"How complete is my reference data? Are there duplicates?"*

A sample session, with the calls the assistant made and what came back, is in
[`VALIDATION.md`](VALIDATION.md#m2--a-real-mcp-client-25-september-2026).

### Three promises every answer keeps

1. **No unbounded lists.** Every list is capped and ranked, and says how many it left out. An
   assistant handed five thousand works spends its context on them and loses the thread.
2. **Every paper carries its OpenAlex ID and open-access status.** The ID lets the assistant ask a
   follow-up about any paper it was shown. The status stops it from claiming it can read a paper it
   can't.
3. **Stubs are labelled.** A *stub* is a work the library knows only by ID: something cites it, but
   it hasn't been fetched yet. Stubs come back with `"stub": true` and no title, so an assistant
   can't talk about one as if it knew what it says.

---

## Tool reference

Each tool returns structured JSON, which is also sent as text for clients that don't read
structured results. Arguments not given take the default shown.

### A paper, as every tool returns it

| Field | Always there | Meaning |
| --- | --- | --- |
| `id` | yes | OpenAlex ID, `W` followed by digits. Pass it to any other tool |
| `oa_status` | yes | `diamond`, `gold`, `green`, `hybrid` or `bronze` are free to read; `closed` is not; `unknown` until the paper is fetched |
| `stub` | yes | `true` when only the ID is known |
| `title`, `year`, `type`, `venue`, `doi` | when known | Bibliographic details |
| `authors` | when recorded | The first three authors, in byline order |
| `more_authors` | when there are more | How many further authors the byline has ("et al.") |
| `oa_url` | when known | A free-to-read copy |
| `cited_by_count_global` | when known | How often **the whole world** cites it, per OpenAlex. Not a count within your library |
| `seed` | when true | You added this paper yourself |
| `unresolved` | when true | OpenAlex has no record of this ID, so it will never be fetched |

### `add_paper`

Adds a paper as a seed, plus a stub for every work it cites. Adding the same paper twice changes
nothing.

| Argument | Default | |
| --- | --- | --- |
| `identifier` | required | A DOI, arXiv ID, PMID, OpenAlex ID, a link to any of those, or a title |
| `candidates` | 10 | For a title, how many possible matches to offer (at most 100) |

Returns a `status`:

- `added`: the `paper`, how many `references` it has, and how many were `new_works` to the
  library.
- `already_in_library`: the paper was a seed already. Nothing changed.
- `choose`: a **title** matched several papers, so nothing was added. The `candidates` come back,
  best first, with `title_match: true` on those whose title matches allowing for case, punctuation
  and a typo. The assistant should ask you which one you mean, then call `add_paper` again with its
  `id`. A title never adds a paper by guesswork.

If OpenAlex has no reference list for the paper, a `note` says so. That's common for books and
outside the sciences. It's a gap in the data, not a fault in fil.

### `expand_graph`

Fetches works the library cites but hasn't fetched yet, most-cited first (the papers your library
cites most often come first), and records what *they* cite in turn.

| Argument | Default | |
| --- | --- | --- |
| `max_nodes` | 200 | Most works to fetch in this call. At most 500 |
| `max_depth` | from your settings, usually 3 | How many citation steps from your own papers to go |

Returns `fetched`, `new_citations`, `reference_coverage` (the share of fetched works that came with
a reference list, 0 to 1), `not_in_openalex`, `merged` (records OpenAlex holds twice, folded into
one), `skipped`, `seconds`, the `library` totals afterwards, and `stopped_because`:

| `stopped_because` | Meaning |
| --- | --- |
| `budget-exhausted` | Reached `max_nodes`. Call again to continue: it picks up exactly where it stopped |
| `frontier-empty` | Everything the library cites has been fetched, or the library is empty |
| `max-depth` | Reached the depth limit |
| `cancelled` | The client cancelled the call. Everything fetched so far is kept |

The call reports progress after every batch of up to 100 works. Clients that show progress will
show it. If a request to OpenAlex fails partway through, what was already fetched is kept, and the
error says how many works that was.

### `neighbours`

| Argument | Default | |
| --- | --- | --- |
| `identifier` | required | The paper: an ID, DOI, arXiv ID, PMID or title. It must already be in the library |
| `direction` | `both` | `cites` (its references), `cited_by` (what in the library cites it), or `both` |
| `limit` | 20 | How many works to list each way. At most 100 |

Returns the `paper`, and `references` and/or `cited_by`, each a capped list:

| Field | Meaning |
| --- | --- |
| `total` | How many there are |
| `fetched` | How many of those have been fetched; the rest are stubs |
| `omitted` | How many were left out. Raise `limit` to see more |
| `works` | Fetched works first, then by `cited_by_count_global` |

`cited_by` counts only papers **in your library**. For the worldwide figure, see the paper's
`cited_by_count_global`.

### `find_path`

| Argument | Default | |
| --- | --- | --- |
| `from`, `to` | required | Two papers already in the library, by ID, DOI, arXiv ID, PMID or title |
| `max_hops` | 6 | Longest chain to look for. At most 10 |
| `any_direction` | `false` | Also allow chains that mix citing and being cited |

By default this looks for **lineage**: one paper reaching the other by following references, in
either order. That's how an idea descends. With `any_direction`, any connection counts.

Returns `found`, the number of `steps`, and the `chain` from the first paper to the second. Each
paper in the chain has a `next` field: `cites` means it cites the next paper, `cited_by` means the
next paper cites it.

Not finding a chain is an answer, not an error. `found` is `false`, the two papers come back as
`from` and `to`, and a `note` suggests what to try: `any_direction`, or `expand_graph` when one of
them hasn't been fetched yet.

### `library_stats`

| Argument | Default | |
| --- | --- | --- |
| `include_duplicates` | `false` | Also list fetched works that share a title |

Returns `works`, `fetched`, `stubs`, `not_in_openalex`, `seeds`, `citations` and
`reference_coverage`. With `include_duplicates`, it also returns up to 20 groups of works sharing a
title, plus `duplicates_omitted`. Works sharing a title are *probably* one paper held twice, but not
certainly (a book review carries its book's title), so fil never merges them on its own
([D14](DECISIONS.md#d14--title-level-duplicates-are-reported-never-merged-automatically)).

### Errors

A tool that fails returns an error the assistant can read and act on. Each one names the next
step:

| Situation | The message tells the assistant to |
| --- | --- |
| The paper isn't in the library | Add it with `add_paper` first |
| The identifier can't be read | Give a DOI, arXiv ID, PMID, OpenAlex ID, link or title |
| OpenAlex has no such work | Check the identifier |
| A title matches several papers in the library | Call again with one of the listed IDs |
| OpenAlex is unreachable or rate-limiting | Try again later, or after the wait OpenAlex asked for |

---

## Limits, and why they exist

| Limit | Value | Why |
| --- | --- | --- |
| Works per list | 20 by default, 100 at most | An assistant's context is finite. Ranked and capped lists keep answers on topic |
| Works per `expand_graph` call | 200 by default, 500 at most | The expansion runs inside the call ([D16](DECISIONS.md#d16--mcp-expansion-runs-inside-the-call-the-job-system-waits-for-m3)). 500 works take about 20 seconds, well inside any client's timeout. For more, the assistant calls again |
| Path length | 6 by default, 10 at most | A longer chain says little about how two papers are related |
| Duplicate groups shown | 20 | The rest are counted in `duplicates_omitted` |

OpenAlex allows about 1,000 list requests a day per user, and fetching a single paper is free.
One `expand_graph` of 500 works uses roughly 5–10 of them, so the allowance is very unlikely to be
what stops you.

---

## Troubleshooting

**The client says the server failed to start.** Run `fil version` in a terminal. If that fails,
the client can't find `fil` either: use the full path in its config.

**Where are the server's messages?** fil writes a start-up line and any notices to standard
error, which clients keep in their logs. Claude Desktop keeps them in its `logs` folder
(*Settings → Developer* shows where). In Claude Code, run `claude --debug`. Standard output is
reserved for the protocol, so fil never prints anything else there.

**The assistant can't find a paper I added from the CLI.** Both must use the same library. Compare
the path in the server's start-up line (`fil … serving <path> over MCP`) with `fil where`, and check
the client's config for a `--db` argument.

**`expand_graph` stops early.** Read `stopped_because`. `frontier-empty` with few works usually
means the papers have no reference lists, which is common in the humanities (see
[the coverage table](../README.md#know-this-before-you-start)). `max-depth` means raising
`max_depth` will go further.

**A paper has no `authors`.** It is a stub (not fetched yet), or it was fetched by fil v0.1, which
didn't store authors. Works fetched from v0.2 on carry them. If an answer names authors for such a
paper, the names came from the model's memory, not from your library.
