---
title: 'Filiation: a citation graph and research library that runs on your own computer'
tags:
  - Go
  - citation analysis
  - literature review
  - bibliometrics
  - OpenAlex
  - open access
  - Model Context Protocol
authors:
  - given-names: Haren
    surname: Daishika
    orcid: 0000-0000-0000-0000 # TODO: replace with your ORCID iD before submitting
    affiliation: '1'
affiliations:
  - name: Department of Computer Science and Engineering, University of Moratuwa, Sri Lanka
    index: 1
date: 24 November 2026 # TODO: set to the submission date
bibliography: paper.bib
---

<!--
DRAFT. Not ready to submit before early March 2027: JOSS asks for more than six
months of public history, and the repository was created on 29 August 2026.
Before submitting: add your ORCID; fill in the Research impact statement with
real evidence of use; check the AI usage disclosure against what you used.
Word count, without front matter and references, should stay within 750 to 1750.
-->

# Summary

Filiation builds a research library from one paper. Given a DOI, an arXiv
identifier, a link or a title, it follows the paper's reference list through
OpenAlex [@priem2022openalex], most-cited first and within a budget the
researcher sets, and records every citation as an edge between two works. It
downloads the legal open-access PDFs of the papers it finds, extracts their
text, and finds passages by keyword with the page they are on. Where the text
allows, it records the sentence in which one paper cites another. The library
is served to AI assistants over the Model Context Protocol (MCP)
[@mcp2024spec], so that an assistant's answer can quote passages, with their
pages, from papers the researcher actually holds. Filiation runs on the
researcher's own computer as a desktop app and as a command-line tool, `fil`,
with no account and no server.

# Statement of need

A literature review starts from a few papers and works outward through their
references, the practice that citation indexes were invented to support
[@garfield1955citation]. Researchers do this today with several tools that do
not share data. Reference managers such as Zotero [@zotero] keep references
and PDFs but do not follow citations. Citation-map websites such as Connected
Papers [@connectedpapers], Litmaps [@litmaps] and ResearchRabbit
[@researchrabbit] draw the neighbourhood of a paper, but hold the map on their
servers and do not keep the papers or their text. General-purpose AI
assistants write fluent summaries, but invent or misattribute references
[@walters2023fabrication], and they cannot see the papers on the researcher's
disk.

Filiation joins these steps in one local tool. The citation graph decides what
enters the library; the PDFs and their text make it searchable to the page;
and the MCP server lets an assistant search that library instead of its
memory, in the manner of retrieval-augmented generation [@lewis2020rag], while
Filiation itself writes no prose. Its users are researchers and students
conducting literature reviews, including those who never use a terminal, and
developers who want a citation graph they can script or export.

# State of the field

Bibliometric software such as CiteSpace [@chen2006citespace], VOSviewer
[@vaneck2010vosviewer] and bibliometrix [@aria2017bibliometrix] analyses and
maps a corpus that the user has already exported from an index. Filiation
differs in building the corpus itself by following references from a seed,
and in keeping the documents, not only their metadata. Citation-map websites
build the corpus automatically but are hosted services without full text.
Filiation's graph exports as GraphML for Gephi [@bastian2009gephi] and
NetworkX, so it complements these tools rather than replacing them.

# Software design

Filiation is written in Go and distributed as one static binary per platform,
with no runtime to install. Two decisions follow from that. SQLite is used
through a WebAssembly build of the driver, so no C toolchain is needed, with
FTS5 for keyword search; and PDF text is extracted by PDFium compiled to
WebAssembly, which matched `pdftotext` at a word-level F1 of 0.985 in our
measurements.

Every work receives a permanent local identifier, and works are deduplicated
on their OpenAlex identifier, which a unique index enforces, because the same
paper often exists as preprint, conference paper and journal article. Graph
traversal is a breadth-first search with one set-based query per step: a
recursive SQL query produced 20.8 million intermediate rows and took 125
seconds to depth five on a library of 10,000 citations. Expansion always takes
a node budget, because papers in science and engineering cite 70 to 100 works
each, and a library grows to several thousand works within two steps.

Coverage depends on the field, and Filiation reports it rather than hiding it.
In our measurements, works with no reference list at the edge of the graph
ranged from 6% in medicine to 84% in the arts and humanities, and roughly one
work in eight had a legal open-access PDF, from 46% in physics to under 1% in
the humanities. Filiation never downloads a paywalled PDF.

Search ranks papers by their best passages, fused with the title and
abstract, using BM25 [@robertson2009bm25]. Communities in the citation graph
are found with the Louvain method [@blondel2008louvain] and offered as
collections. The command line, the MCP server and the desktop app are thin
layers over one library package, and all storage sits behind one package so
the backend can be replaced.

# Research impact statement

<!-- TODO: replace with evidence gathered before submission. JOSS asks for
demonstrated research impact: who uses Filiation and for what (theses, reviews,
courses), downloads, stars, issues and pull requests from people other than the
author, and any work that cites it. Keep only what you can show. -->

Filiation has been validated across five fields. Twenty seed papers, four each
in medicine, physics, computer science, the social sciences and the arts and
humanities, each built a library, fetched its open-access PDFs, extracted
their text and exported the graph without error, 20 of 20. Searching each
paper's own title found it among the first five results in 211 of 219 papers
with text, and searches over 38,730 passages took under a third of a second.
The software has had public releases since September 2026, each with
binaries for Windows, macOS and Linux, and since November 2026 a Windows
installer for the desktop app, which keeps itself up to date.

# AI usage disclosure

<!-- TODO: check every line against what you actually used, and add versions. -->

Generative AI was used in building this software and preparing this paper.
Claude Code, Anthropic's coding assistant, running Claude models
[TODO: list the models and versions used], was used to write and revise code,
tests and documentation, to run validation and measurement scripts, and to
draft this paper. The author set the project's scope and made its design
decisions, which are recorded with their reasons and measurements in the
repository's decision log; reviewed, edited and tested the changes; and ran
the validation reported above. Every claim in this paper was checked against
the repository's measurements.

# Acknowledgements

Filiation's bibliographic data comes from OpenAlex, which publishes it under
CC0.

# References
