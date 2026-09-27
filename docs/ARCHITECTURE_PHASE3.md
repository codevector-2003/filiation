# Phase 3 architecture — papers on disk

**Covers:** milestone M3 · ships as **v0.3**
**Language:** Go — see D8 in `DECISIONS.md`
**Status:** Agreed 27 Sept 2026 — decisions D17, D18 and D19
**Date:** 27 September 2026

This document is the detailed design for M3, in the same shape as
[`ARCHITECTURE_PHASE1.md`](ARCHITECTURE_PHASE1.md). It does not repeat the system-wide view in
[`ARCHITECTURE.md`](ARCHITECTURE.md); where it changes something there, it says so.

---

## 1. What Phase 3 must do

| | |
| --- | --- |
| **In** | A library that is a folder the user can see. Our own work IDs (fil IDs). Legal open-access PDFs found, downloaded and filed. PDFs the user already has, imported and identified. Text extracted, chunked and searchable by keyword. The sentence around each citation, attached to the edge OpenAlex gave us. |
| **Out** | Embeddings, semantic search and answers (M4). The web interface (M5). Citation intent (M6). Zotero (D19). Building citation edges from a PDF's reference list — measured here, built later (D19). OCR. |
| **Done when** | `fil fetch` downloads the PDF of `10.7717/peerj.4375` into the library folder. `fil search` finds a passage in it and names the page. Some of its citations carry the sentence they were made in. `fil stats` says why each paper without text has none. Running `fil fetch` twice downloads nothing. A PDF dropped into `Inbox/` is identified, verified and filed. |

### Non-functional requirements carried from Phase 1

- **Every hard rule holds.** §2 says how, rule by rule.
- **Interruptible.** Ctrl-C during a download or an extraction loses only the paper in flight.
  Progress lives in the domain tables, never only in memory.
- **The user's files are the user's.** fil never deletes, moves or edits a file a person put in
  the library folder (ADR-012).
- **Windows, macOS and Linux behave the same.** Every path rule in this document is written for
  all three.

---

## 2. How M3 keeps the hard rules

| Rule | In M3 |
| --- | --- |
| 1. Open access only | PDF links come only from OpenAlex OA locations and Unpaywall. A response that is not a PDF (`%PDF-`) is rejected, and a landing page is never read for links — that would be scraping. A PDF the user already has is imported, never downloaded |
| 2. No reference parser | Citation edges still come only from OpenAlex. Citation context attaches a sentence to an edge that already exists (ADR-015). Parsing references for works OpenAlex does not know is the fallback rule 2 allows "only later": measured in M3 (spike 11), built after it (D19) |
| 3. No LLM-built graph | No LLM anywhere in M3 |
| 4. No cgo | The PDF extractor must be pure Go or run as WebAssembly in pure Go. The bake-off (spike 10) rejects anything else, and anything AGPL |
| 5. One writer | Downloads and extraction may run in parallel; one goroutine commits to the database |
| 6. Deduplicate | **Reworded by D17:** every work has a fil ID that never changes; no two works may share an `openalex_id`, enforced by a `UNIQUE` index; merged records and shared DOIs fold on the way in; a shared title is reported, never merged |

---

## 3. Component map

```
internal/
├── identity/     + fil IDs: generate, parse, normalise (ADR-010)
├── store/        schema v2+: fil_id is the key; work files; full-text state; chunks; mentions
├── config/       + the library folder (ADR-011)
├── blobs/        the papers/ folder: names, atomic writes, hashes, rescans (ADR-012)
├── sources/
│   ├── openalex/ + OA locations, fetched when PDFs are wanted (ADR-013)
│   └── unpaywall/  second opinion, only if spike 9 shows it adds links (ADR-013)
├── httpx/        + streaming download to a file, bypassing the response cache
├── text/         extract (ADR-009, decided by spike 10), clean, chunk, find markers
├── library/      + Fetch, Import, Search, List, Show
└── mcpsrv/       + search_library, fetch tool; every work carries its fil ID
```

The package rules do not change. `store` never imports `sources`; `sources` never imports
`store`; front doors never import `store`. `blobs` and `text` import neither `store` nor
`sources` — they work on files and bytes, and `library` joins them up.

---

## 4. Architecture decision records

### ADR-010: Every work has a fil ID

**Status:** Agreed · **Date:** 27 Sept 2026 · **Decision:** D17

**Context.** Until v0.2 the OpenAlex ID was the primary key. Three things M3 needs cannot be
built on that: a PDF the user owns that OpenAlex does not know (a thesis, a report, many
humanities books) has no row to live in; folder names and notes tied to an OpenAlex ID break when
OpenAlex merges records, which it does (M1 folds them); and the library should keep working if
OpenAlex changes its terms again, as it did once (D11).

**Decision.** Every work gets a **fil ID**: `F` followed by 8 characters of Crockford base32
(`0-9` and `A-Z` without `I`, `L`, `O`, `U`), for example `F7K2M9QXA`.

- Generated on the user's machine, from `crypto/rand`, once, and **never changed**. It carries no
  meaning — not the title, not the year — so there is never a reason to change it.
- **At least one of the eight characters is a digit.** A nine-letter word beginning with F is
  then never read as an ID (`FASTTRACK` is a title; `F7K2M9QXA` is an ID).
- Case-insensitive on input, upper case on output. No lookalike substitution (`O` for `0`):
  that would make more words parse as IDs.
- 40 bits of randomness. The single writer checks a new ID against the library before using it
  (work rows and aliases), so a clash is retried, never stored. Merging two libraries later
  matches works by OpenAlex ID or DOI first and replaces a clashing fil ID.
- It is the primary key of `work`. `openalex_id` becomes a nullable `UNIQUE` column: still the
  deduplication key for every work OpenAlex knows, and still enforced by the database.
- When two works fold into one, the survivor keeps its fil ID and the other's fil ID and OpenAlex
  ID are kept as **aliases**, so old folder names, notes and scripts still find the paper.
- Everywhere a person types a paper, a fil ID is accepted alongside DOI, arXiv ID, PMID, OpenAlex
  ID and title. Every result shows both IDs.

**Consequences**
- Easier: local documents, stable folder names, other sources later (Crossref, ISBN lookups).
- Harder: the first migration rebuilds every table that referred to `openalex_id`. Done now,
  while v0.2 has few users; it only gets more expensive.
- Stubs get fil IDs too. A stub is a row with an ID and little else; that has not changed.

### ADR-011: The library is a folder the user chooses

**Status:** Agreed · **Date:** 27 Sept 2026 · **Decision:** D18 · amends ADR-006

**Context.** ADR-006 put the library in a hidden per-user application directory, to stop
databases scattering into whatever directory the user was standing in. Researchers also need to
see, open, back up and organise their PDFs, which a hidden directory prevents.

**Decision.** A library is a folder the user chooses — by default `Documents/Filiation/<name>` —
and fil remembers it. What ADR-006 guarded against does not return: the folder is chosen once,
on purpose, and recorded; it is never the current directory.

```
Documents/Filiation/My Library/
├── filiation.toml          library settings: name, budgets, switches
├── filiation.db            graph, metadata, text, search index
├── papers/                 the user's papers — see ADR-012
├── Inbox/                  drop PDFs here to have fil identify and file them (ADR-014)
├── exports/                GraphML, BibTeX, CSV are written here by default
└── .filiation/tmp/         downloads in progress; same drive as papers/, so rename is atomic
```

- **One library, many projects.** A project is a collection inside the library, so the map keeps
  accumulating (PRODUCT_PLAN §1). A person who wants separate worlds can make a second library;
  the global config lists the libraries and which is the default.
- **Documents is found per OS.** Windows asks the shell for the Documents known folder
  (`golang.org/x/sys/windows`, already a dependency, no cgo) because it is often redirected into
  OneDrive. macOS uses `~/Documents`. Linux reads `XDG_DOCUMENTS_DIR` and falls back to
  `~/Documents`.
- **Cloud sync folders.** SQLite in WAL mode is corrupted by sync tools that copy `-wal` and `-shm`
  separately from the database. When the library folder is inside OneDrive, iCloud Drive, Dropbox
  or Google Drive, `filiation.db` is kept in the per-user application directory instead and the
  folder holds everything else. PDFs sync safely; the database does not.
- **Existing v0.2 libraries** keep working where they are. `fil library move <folder>` moves one
  into a library folder; nothing moves on its own.
- **The HTTP cache stays in the OS cache directory**, outside the library. It is disposable.

### ADR-012: `papers/` is the file store — readable names, one folder per work

**Status:** Agreed · **Date:** 27 Sept 2026 · **Decision:** D18 · amends D4

**Context.** D4 named each PDF by its SHA-256. That buys deduplication and integrity checks, and
gives the user a folder of names nobody can read.

**Decision.** PDFs live in `papers/`, **one folder per work**, with readable names. The SHA-256
moves from the file name into the database, where it still deduplicates (look the hash up before
saving) and still verifies (re-hash and compare).

```
papers/
├── Piwowar-2018-The-state-of-OA--F7K2M9QXA/
│   ├── published.pdf
│   └── preprint.pdf
├── Chapter 2 - Open access/                        ← made by the researcher
│   └── Harnad-2004-The-access-impact-problem--F3QD8WN2T/
│       ├── published.pdf
│       └── Harnad 2004 annotated.pdf               ← imported, original name kept
└── Methods/
    └── Vaswani-2017-Attention-is-all-you-need--F9M4K7B2R/
        └── preprint.pdf
```

**The work folder name:** `{FirstAuthorSurname}-{Year}-{ShortTitle}--{filID}`.
- Accents folded to ASCII (é → e); anything else outside letters and digits dropped; spaces become
  `-`. The characters Windows forbids (`< > : " / \ | ? *`) cannot survive, and a name ending in
  the fil ID can never be a reserved Windows name (`CON`, `NUL`).
- The title part stops at about 60 characters, so a full path stays well under Windows' 260 even
  in a deep folder. fil warns before a path passes about 240.
- No author gives `Unknown`; no year gives `nd`.
- **The fil ID at the end** is what makes each name unique on case-insensitive file systems, and
  what lets fil find a folder the user moved.

**Files inside** are named by version, from OpenAlex's vocabulary: `published.pdf`,
`accepted.pdf`, `preprint.pdf`. An imported file keeps its original name — the person recognises
it, and the work folder already makes it unique.

**The researcher owns the tree; fil owns only what it named.**

| The researcher | fil |
| --- | --- |
| makes subfolders, at any depth | has nothing to do: they are the researcher's |
| moves a work folder | finds it on the next scan by the fil ID in its name, and updates the path |
| renames a work folder | keeps the new name. fil renames a folder only while it still has the name fil gave it, which is recorded, so a corrected title never undoes a person's choice |
| removes the fil ID from a folder name | finds the folder by the hashes of the files in it |
| adds a file to a work folder | lists it with the work; never changes or deletes it |
| drops a PDF somewhere in `papers/` | identifies it (ADR-014) and **leaves it where it is** — a work does not need a folder of its own |
| deletes a work folder | marks the paper "file missing" and does not download it again unasked. The paper stays in the graph: the citation is still true |

- **New downloads go directly into `papers/`.** The researcher files them from there.
- **Folders become collections**, one way, **on by default** with a switch in `filiation.toml` to
  turn it off. `papers/Chapter 2/Key studies` appears in fil as that collection. Collections can
  do more than folders — one paper in many — so fil never moves files when a collection changes.
- **Scans run when a command needs them**, not continuously. Walking tens of thousands of folders
  takes well under a second. A live watcher (`fsnotify`, pure Go) waits for the web interface.
- Paths are stored relative to the library folder, with `/`, in Unicode NFC — macOS stores
  accented names decomposed, and the same name must compare equal on all three systems.
- **Writes are atomic.** A download goes to `.filiation/tmp/`, is checked (`%PDF-`, size, hash)
  and only then renamed into place. A crash leaves at most a temporary file, never a bad PDF under
  a good name.

### ADR-013: Finding and downloading a legal PDF

**Status:** Agreed · **Date:** 27 Sept 2026

**Context.** Measured on our own acceptance seed: OpenAlex's `open_access.oa_url` for
`10.7717/peerj.4375`, a gold OA paper, is `https://doi.org/10.7717/peerj.4375` — a landing page —
and `primary_location.pdf_url` is null. What v0.2 stores is usually not a PDF.

**Decision.** An ordered chain of sources, each asked only when the one before found nothing — the
shape `quelle`'s resolver uses, reimplemented, never imported:

1. **OpenAlex locations** — `best_oa_location` and `locations[]`, reading `pdf_url`, `version`,
   `license`, `is_oa`. Fetched when PDFs are wanted, 100 works per request (1 credit), so
   expansion does not change and old libraries need no backfill.
2. **Unpaywall** — `url_for_pdf` by DOI. Unpaywall requires an email address; it is asked only
   when the contact email is set, and otherwise skipped with a one-line note. Built only if spike
   9 shows it finds PDFs OpenAlex does not: the two share data.

**Downloading.**
- Stream to `.filiation/tmp/`, never into memory; a size cap (100 MB by default).
- Reject anything that does not begin `%PDF-`. An HTML page is a landing page, a login page or a
  bot wall. It is recorded as "not a PDF" and **never read for links**.
- No cookies, no credentials, a plain `User-Agent` naming fil.
- A token bucket **per host**, so one repository is never hammered: arXiv, PMC and publishers are
  different people's servers.
- PDFs bypass the HTTP response cache. `papers/` is where they live.
- Every attempt is recorded — the URL, what happened and why — so a paper without text always says
  why: no open copy, open but no direct PDF, not a PDF, download failed, no text layer.

**Which papers.** Fetching is always explicit: `fil fetch`, with a budget. Seeds first, then by
in-graph in-degree — the same ranking expansion uses. Never a side effect of `add` or `expand`:
disk and bandwidth are the user's.

**Bronze** (free to read on the publisher's site, no licence) is fetched for private reading,
recorded as having no licence, and never included in any export or share.

**Known limit, documented rather than engineered around:** we trust OpenAlex and Unpaywall's OA
labels. On a campus network, a paywalled link mislabelled as open could still download. fil
never goes looking for PDFs itself, so that label is the only exposure.

### ADR-014: Importing PDFs the user already has

**Status:** Agreed · **Date:** 27 Sept 2026

**Context.** Researchers already hold folders of PDFs, often obtained legitimately through their
institution. Importing them adds full text without breaking rule 1: fil downloads nothing. But an
imported file can have any name, so the name cannot identify it.

**Decision.** Collect clues from cheapest to most expensive, then **verify before trusting any
of them**.

| Order | Clue | Catches |
| --- | --- | --- |
| 1 | The file name | arXiv downloads (`1706.03762v5.pdf`), names carrying a DOI or article number |
| 2 | Metadata inside the PDF (XMP `prism:doi`, `dc:identifier`) | Many publishers' files. Sometimes wrong: templates reuse old values |
| 3 | Text of pages 1–2 | The arXiv stamp in the margin, a DOI in the header or footer, PMID/PMCID |
| 4 | The title — the largest text on page 1 | Anything else. Candidates only |

Page 1 carries other papers' DOIs ("Cite this article", related articles, references on a short
paper), so a DOI with a `doi:` label or in the header or footer is preferred. DOIs broken across
lines, ligatures and soft hyphens are repaired before searching.

**Verification.** An ID is resolved with a free single OpenAlex lookup, and the returned title
must appear in the first pages (`identity.TitlesMatch`) with the first author's surname.

| Outcome | Then |
| --- | --- |
| ID found, title matches | **confirmed** — imported |
| ID found, title does not match | **unconfirmed** — the user is asked |
| Only a title | title search gives candidates; the user chooses (ADR-005) |
| Nothing, e.g. a scanned PDF | stays in `Inbox/` with the reason; `fil import file.pdf --as <id>` names it, and is still verified |
| Not in OpenAlex at all | becomes a **local document**: a fil ID, no `openalex_id`, searchable text, no citation edges |

**Title searches cost 10 credits each** against 1,000 a day, so they are capped per run (20 by
default) and fil asks before spending more. Steps 1–3 cost nothing.

**Where files go.** `Inbox/` means "file this for me": the PDF moves into a new work folder.
Anywhere in `papers/` means "I filed it myself": identified and left in place. A file outside the
library (`fil import ~/Downloads/x.pdf`) is **copied**; the original is never moved or deleted.
The same hash already in the library is reported, not stored twice; the same paper with a
different file (an annotated copy) keeps both. An imported paper is a seed with
`source = upload`, and its references still come from OpenAlex. A user's own copy is marked
"user's copy, licence unknown" and never exported.

### ADR-015: Citation context attaches sentences to edges that already exist

**Status:** Agreed · **Date:** 27 Sept 2026

**Context.** To know that `[12]` means a particular paper, fil has to look at entry 12 of the
paper's reference list. Hard rule 2 forbids a reference parser because its errors compound across
a graph.

**Decision.** Nothing read from a PDF creates, removes or redirects an edge. Context capture:

1. Finds the reference section and splits it into entries — splitting, not field extraction.
2. Matches entries to **references OpenAlex already gave us**, by looking for what we already
   know inside the entry text: a DOI, then a title we hold, then first author and year.
3. Finds the in-text markers — numeric (`[12]`, `[3–5]`, superscripts, which need font sizes
   from the extractor) and author–year (`(Piwowar et al., 2018)`) — and takes the sentence around
   each.
4. Stores **every mention**, with section, page, method and a confidence, in a `mention` table.
   M6 needs all of them — a paper cited once in the methods and twice in the discussion says more
   than one string would. `cites.context` keeps the first, for display.

No match, no context. Never a guess. Precision is checked by hand on a sample before release.

### ADR-009, still open: PDF text extraction

Decided by spike 10, before `internal/text` is written. Recorded as a decision when it is.

---

## 5. Schema changes

Each change is a numbered, all-or-nothing migration step, so a v0.2 library upgrades straight to
the latest schema in one run, and a newer library is still refused by an older binary.

**v2 — identity (ADR-010).** `work.fil_id` becomes the primary key; `openalex_id` becomes
nullable and `UNIQUE`. Every table that referred to a work — `cites`, `authorship`, `chunk`,
`note`, `collection_work` — refers to `fil_id`. `work_alias` records the fil IDs and OpenAlex IDs
of folded works. The unused `pdf_sha256` and `pdf_license` columns go; their replacement is `v3`'s
file table. SQLite cannot change a primary key in place, so this follows its twelve-step table
rebuild, with foreign keys off for the rebuild and `foreign_key_check` before commit. **The
rowid order of `cites` is preserved**, because the frontier ranks references by it.

**Later steps, each landing with the code that uses it:** work files and folders (ADR-012),
download attempts and full-text state (ADR-013), chunk offsets and extractor version (text),
mentions (ADR-015), import state (ADR-014).

---

## 6. Spikes

This environment's network policy blocks OpenAlex, Unpaywall and PDF hosts, so the spike programs
are written here and run on the maintainer's machine, like spikes 1–7.

| # | Question | Answers |
| --- | --- | --- |
| 9 | Of the works in the M1 validation libraries, how many have a PDF we can actually download — per field, per OA status — and how many more does Unpaywall add? How long does one download take? | Whether Unpaywall is built; what coverage the README promises; whether the job system is needed in M3 (D19) |
| 10 | ADR-009 bake-off, 20 PDFs from 5 fields: `ledongthuc/pdf` (pure Go), `go-pdfium` in WebAssembly mode (PDFium run by wazero, no cgo — to be verified), `pdftotext` as the quality baseline. Text fidelity, two-column reading order, ligatures and hyphens, superscript markers, font sizes, speed, crashes on bad files, binary size, licence | The extractor |
| 11 | Split and match the reference lists of 20 PDFs **that OpenAlex does know**, and compare with their `referenced_works` | Precision and recall of reference matching, for free. Decides whether parsed references for local documents get built after M3 |

---

## 7. Failure modes

| What happens | Response |
| --- | --- |
| The URL returns HTML | Recorded as "not a PDF". Never parsed for links |
| Download interrupted | The temporary file is deleted on the next run; the paper stays "not fetched" |
| PDF larger than the cap | Recorded as too large, with the size |
| Malformed or hostile PDF | Extraction runs with a time limit and recovers from panics; one bad file fails one paper, never the run |
| Scanned PDF, no text layer | Recorded as "no text". No OCR |
| The user deleted or moved a file | Found by rescan, or reported missing. Never downloaded again unasked |
| Two works fold into one | Files and folder follow the survivor; the old fil ID becomes an alias |
| Library inside a sync folder | Database kept outside it (ADR-011) |

---

## 8. Test strategy

No network in unit tests, as before: recorded responses replayed through a stub `RoundTripper`,
and small generated or openly licensed PDFs under `testdata/`. Table-driven throughout. Every
component lands with its tests, and `go vet`, `gofmt` and `go test ./...` are clean before each
commit.

| Test | Asserts |
| --- | --- |
| Migration | A real v1 library upgrades to the latest schema: same works, same edges, same edge order, every work with a unique fil ID; a failure part-way leaves it untouched |
| fil IDs | Format, at least one digit, case-insensitive parsing, words never parse as IDs, clashes retried |
| Names | Forbidden characters, reserved names, long titles, accents, missing author and year |
| Atomic write | A failed or interrupted write leaves no file under a final name |
| Rescan | A moved, renamed or ID-less folder is found; a deleted one is reported |
| Not a PDF | An HTML response is rejected and recorded |
| Idempotent fetch | Running fetch twice downloads nothing |
| Import | Each clue type; a wrong DOI on page 1 is caught by verification |
| Context | Markers map to known edges; an unmatched marker produces no context |

---

## 9. Build order

Each step is tested before the next begins, and each lands with its command where it has one.

| Step | Work | Done when |
| --- | --- | --- |
| 1 | `identity`: fil IDs | Generated, parsed and normalised, with tests |
| 2 | Schema v2 and the first migration; fil IDs through store, graph, library, MCP, CLI and export | A v1 library upgrades cleanly; every command shows fil IDs |
| 3 | The library folder (ADR-011) and `papers/` (ADR-012): config, names, atomic writes, rescan, `fil library move` | A work folder can be created, moved by hand and found again |
| 4 | Spikes 9, 10 and 11 — whenever the network allows, in parallel with 1–3 | D-records for the extractor, Unpaywall and the job system |
| 5 | Legal PDF links (ADR-013) | Every work gets a link or a reason |
| 6 | Downloading, `fil fetch` | The seed's PDF is in `papers/`; a second run downloads nothing |
| 7 | `internal/text`: extract, clean, chunk, FTS5; `fil search` | A passage in the seed is found with its page |
| 8 | Import, `Inbox/`, local documents (ADR-014) | A dropped PDF is identified, verified and filed |
| 9 | Citation context (ADR-015) | Precision checked by hand on a sample |
| 10 | `fil list`, `fil show`, full text in `fil stats`; MCP `search_library` and a fetch tool | One library function behind every front door |
| 11 | Validation run on 20 seeds across 5 fields; docs; tag **v0.3** | Like M1's run |

Steps 1–7 and 10 are a usable tool on their own: "find a passage by keyword" is Phase 3's success
test in `PRODUCT_PLAN.md`. If exams cut time short, that half ships first.
