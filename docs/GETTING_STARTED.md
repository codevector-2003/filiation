# Getting started

From nothing to a citation graph of your own, in a few minutes.

## Install

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

## First steps

```sh
fil add 10.7717/peerj.4375                  # a DOI, arXiv ID, PMID, OpenAlex ID, link, or title
fil expand                                  # follow the references, most-cited first
fil path 10.7717/peerj.4375 W4300313809     # how one paper descends from another
fil export graphml -o library.graphml       # open it in Gephi
```

Then bring in the papers themselves:

```sh
fil fetch                                   # download the open-access PDFs, and read them
fil search "citation advantage"             # find passages, with their paper and page
fil show 10.7717/peerj.4375                 # everything about one paper
```

## Where your library lives

On the first run fil asks where to keep your library: a folder you can see, `Documents/Filiation/My
Library` unless you choose another. Papers go in its `papers/` folder, one folder per paper with a
readable name such as `Piwowar-2018-The-state-of-OA--F7K2M9QXA`. Move, rename and group those
folders however you like; fil finds them again, and each folder you make becomes a collection.

PDFs you already have go in the library's `Inbox/` folder, or `fil import <file>`: fil works out which
paper each one is, checks that its title and author are really in it, and files it.

`fil where` shows where everything is.

## Next

- Every command and its options: [COMMANDS.md](COMMANDS.md)
- Using your library from Claude, Copilot, Cursor or another AI assistant: [MCP.md](MCP.md)
- How it works, and how much reference data to expect in your field: [HOW_IT_WORKS.md](HOW_IT_WORKS.md)
