# Validation runs

Live checks against real data, run before each milestone is called done. Unit tests replay
recorded responses; these are the runs that do not.

- [M1 — 20 seeds, 5 fields](#m1--20-seeds-5-fields-24-september-2026) (24 Sept 2026)
- [M2 — a real MCP client](#m2--a-real-mcp-client-25-september-2026) (25 Sept 2026)
- [M3 step 10 — the PDF tools from a real client](#m3-step-10--the-pdf-tools-from-a-real-client-29-october-2026) (29 Oct 2026)
- [M3 — 20 seeds, 5 fields, the papers on disk](#m3--20-seeds-5-fields-the-papers-on-disk-30-october-2026) (30 Oct 2026)

## M1 — 20 seeds, 5 fields, 24 September 2026

ARCHITECTURE_PHASE1.md §10's "20-paper / 5-field validation run", done against live OpenAlex
before the first release. Code: [`spikes/8-validation`](../spikes/8-validation/main.go). Each seed
got a library of its own, one `fil expand` with the defaults (500 works, depth 3), a title-duplicate
report (D14) and a GraphML export read back by an independent parser.

### Findings

**Every seed completed every step — 20 of 20.** No errors, no failed batches, no export that did
not read back, no edge pointing outside its file. The budget held everywhere: every run stopped on
it except EPR (below). 500 works took 14 s to 73 s; the whole run took under 10 minutes and ~115
of the day's 1,000 credits.

**Reference coverage follows D12's ordering, less steeply.** Medicine 93%, physics 91%, computer
science 88%, social sciences 59%, arts and humanities 56%. Spike 4 measured 84% dead ends on the
humanities frontier; here it is 44%. The seeds explain most of the gap: they were chosen with more
than 20 references each, and three of the four sit where the humanities meet science (linguistics,
science studies, philosophy of biology). A humanities user starting from a monograph will see
something closer to spike 4. The low-coverage note `fil expand` prints below 70% fired for six of the
eight social-science and humanities seeds, and for no STEM seed — which is what it is for.

**Title duplicates rise with the same gradient**: 1.4 per 100 fetched works in medicine, 2.7 in
physics, 3.8 in computer science and the social sciences, 4.1 in the humanities.

**D14 was right, and the run produced its best example.** OpenAlex's reference list for
Einstein–Podolsky–Rosen (1935) holds one entry: Bohr's reply, which has *the same title* — filed as
a 1996 reprint in his collected works — and which cites EPR back. The title-duplicate report
flagged the pair, as it should; merging on title would have folded Einstein's paper into Bohr's
rebuttal. It is also an upstream data error (a 1935 paper citing a 1996 reprint), recorded
faithfully: citation edges come from OpenAlex, and fil does not second-guess them (hard rule 2).
That one wrong edge is also why EPR's expansion stopped after a single work.

**"Not found" is highest in computer science** — 124 cited works with no OpenAlex record, 53 of
them from ResNet's neighbourhood — consistent with CS citing preprints and software that OpenAlex
lists in reference lists but does not hold.

**Merged records are rare but real**: 8 folded across the run, 5 of them in computer science.

**Adding by arXiv ID and by OpenAlex ID for a book with no DOI both worked** — the arXiv landing-page
fallback (step 7) and the no-DOI path, on live data.

`go run ./spikes/8-validation`. 20 seeds, 5 fields, one library each; default budget 500, depth 3. Took 9m46s. OpenAlex allowance at the end: 859 of 1000 credits.

| Field | Seed | Refs | Fetched | Coverage | Not found | Merged | Title dups | Stopped | Export (nodes / edges) | Time |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Medicine | 10.1056/NEJMoa2034577 — BNT162b2 vaccine trial, 2020 | 8 | 495 | 96% | 5 | 0 | 3 | budget-exhausted | 496 / 4222 | 31s |
| Medicine | 10.1016/S0140-6736(20)30183-5 — COVID-19 clinical features, Wuhan, 2020 | 37 | 496 | 96% | 4 | 0 | 9 | budget-exhausted | 497 / 5203 | 51s |
| Medicine | 10.1001/jama.2020.1585 — 138 hospitalised COVID-19 patients, 2020 | 12 | 492 | 98% | 8 | 0 | 2 | budget-exhausted | 493 / 7039 | 22s |
| Medicine | 10.1371/journal.pmed.0020124 — Why most published research findings are false, 2005 | 40 | 494 | 83% | 6 | 0 | 13 | budget-exhausted | 495 / 3227 | 16s |
| Physics | 10.1103/PhysRevLett.116.061102 — LIGO GW150914, 2016 | 100 | 492 | 89% | 7 | 1 | 12 | budget-exhausted | 493 / 8466 | 21s |
| Physics | 10.1103/PhysRev.47.777 — Einstein-Podolsky-Rosen, 1935 | 1 | 1 | 100% | 0 | 0 | 1 | frontier-empty | 2 / 2 | 1s |
| Physics | 10.1038/s41586-019-1666-5 — Quantum supremacy, 2019 | 54 | 481 | 92% | 19 | 0 | 19 | budget-exhausted | 482 / 5361 | 29s |
| Physics | 10.1103/RevModPhys.81.109 — Electronic properties of graphene, 2009 | 480 | 493 | 92% | 7 | 0 | 8 | budget-exhausted | 494 / 5101 | 16s |
| Computer Science | arXiv:1706.03762 — Attention is all you need, 2017 (by arXiv ID) | 28 | 473 | 88% | 25 | 2 | 27 | budget-exhausted | 474 / 3745 | 47s |
| Computer Science | 10.1109/CVPR.2016.90 — Deep residual learning, 2016 | 82 | 446 | 93% | 53 | 1 | 19 | budget-exhausted | 447 / 4400 | 1m13s |
| Computer Science | 10.1038/nature14539 — Deep learning, 2015 | 53 | 478 | 86% | 21 | 1 | 12 | budget-exhausted | 479 / 3724 | 26s |
| Computer Science | 10.1145/3292500.3330701 — Optuna, 2019 | 28 | 474 | 86% | 25 | 1 | 13 | budget-exhausted | 475 / 3709 | 27s |
| Social Sciences | 10.7717/peerj.4375 — The state of OA, 2018 (the M0 seed) | 54 | 471 | 67% | 28 | 1 | 18 | budget-exhausted | 472 / 2593 | 35s |
| Social Sciences | 10.1086/225469 — The strength of weak ties, 1973 | 36 | 491 | 40% | 9 | 0 | 14 | budget-exhausted | 492 / 1059 | 18s |
| Social Sciences | 10.1126/science.aac4716 — Reproducibility of psychological science, 2015 | 39 | 498 | 74% | 2 | 0 | 19 | budget-exhausted | 499 / 4269 | 22s |
| Social Sciences | 10.1257/aer.91.5.1369 — Colonial origins of comparative development, 2001 | 13 | 458 | 57% | 42 | 0 | 21 | budget-exhausted | 459 / 1639 | 48s |
| Arts and Humanities | 10.2307/412243 — Turn-taking for conversation, 1974 | 37 | 491 | 40% | 9 | 0 | 16 | budget-exhausted | 492 / 802 | 20s |
| Arts and Humanities | 10.1177/030631289019003001 — Boundary objects, 1989 | 23 | 478 | 51% | 21 | 1 | 27 | budget-exhausted | 479 / 1742 | 29s |
| Arts and Humanities | 10.1098/rspb.1979.0086 — The spandrels of San Marco, 1979 | 28 | 494 | 58% | 6 | 0 | 19 | budget-exhausted | 495 / 1285 | 15s |
| Arts and Humanities | W2266294403 — Meeting the Universe Halfway, 2007 (no DOI) | 107 | 494 | 75% | 6 | 0 | 19 | budget-exhausted | 495 / 2805 | 14s |

### By field

| Field | Seeds OK | Fetched | Dead ends | Coverage | Not found | Title dups per 100 fetched |
| --- | --- | --- | --- | --- | --- | --- |
| Medicine | 4 / 4 | 1977 | 136 | 93% | 23 | 1.4 |
| Physics | 4 / 4 | 1467 | 128 | 91% | 33 | 2.7 |
| Computer Science | 4 / 4 | 1871 | 216 | 88% | 124 | 3.8 |
| Social Sciences | 4 / 4 | 1918 | 777 | 59% | 81 | 3.8 |
| Arts and Humanities | 4 / 4 | 1957 | 862 | 56% | 42 | 4.1 |

20 of 20 seeds completed every step.

## M2 — a real MCP client, 25 September 2026

Does an assistant actually use the MCP surface well, not just call it correctly? Unit tests prove
the tools work (`internal/mcpsrv`, a real client over an in-memory connection) and a scripted
stdio run proved the protocol live. This run put a real assistant in front of `fil mcp`.

**Set-up.** Claude Code 2.1.273, headless (`claude -p`), model Sonnet, `--strict-mcp-config` with
only the filiation server, and only its tools allowed. The server was the dev build with `--db` on
a throwaway library and an isolated settings profile — nothing in the user's own Claude or fil
configuration was touched. One prompt, written as a researcher would:

> I'm researching open-access publishing. Add the paper 10.7717/peerj.4375 to my Filiation
> library, grow the graph by about 150 works, then tell me: (1) which fetched references of that
> paper are most influential, (2) whether it descends from 'Anatomy of green open access' and
> through what chain, and (3) how complete the reference data is.

### What the assistant did

Seven turns, US$0.30. It found all five tools unprompted and called each once, in a sensible order:

| Call | Result |
| --- | --- |
| `add_paper` `10.7717/peerj.4375` | Added, 54 references recorded as stubs |
| `expand_graph` `max_nodes: 150` | 136 fetched, 1 OpenAlex duplicate merged, 13 not in OpenAlex; stopped on budget |
| `neighbours` `direction: cites, limit: 60` | All 54 references, fetched first, ranked by citations |
| `find_path` `to: "Anatomy of green open access"` | A title, resolved inside the library; one step — the seed cites it directly |
| `library_stats` | 2,036 works, 137 fetched, 1,899 stubs, reference coverage 0.72 |

### Findings

**The answer was faithful to the data.** It separated the 46 resolved references from the 8
stubs, reported 72% coverage and what it means, described stubs correctly ("known only by ID
because something cites them"), and advised expanding further before drawing wider conclusions.
Rules 1–3 of `ARCHITECTURE.md` §6 did what they were written to do.

**Two gaps, both in what the tools return rather than in how they work:**

- **Authors came from the model's memory, not the library.** The answer credited "Piwowar et al."
  and "Björk & Laakso" — correct here, but no tool returned an author. Ingestion reads
  `authorships` from OpenAlex and stores none of it yet, so the assistant filled the gap itself.
  An answer should rest on the library, so authors belong in the returned work.
- **The global citation count was presented as if local.** The assistant labelled
  `cited_by_count` "Cites" in its table without saying it is OpenAlex's worldwide count. The field
  description says so; a field name that says so would be harder to miss.

Both were fixed before v0.2 (M2 steps 4–5 in [`STATUS.md`](STATUS.md)): works now carry their first
three authors, and the field is `cited_by_count_global`.

## M3 step 10 — the PDF tools from a real client, 29 October 2026

Does an assistant use the library's PDFs well — quote them, rather than paraphrase from memory?

**Set-up.** As M2: Claude Code 2.1.273, headless, Sonnet, `--strict-mcp-config` with only the
filiation server and only its tools allowed; the dev build, in an isolated settings profile, on a
throwaway library folder outside the user's own (`AppData\Local\fil-check10`). The library: the M0
seed, `fil expand --max-nodes 100`, `fil fetch --max 30` — 1,491 works, 22 PDFs, 21 searchable,
332 of 534 citations from papers with text carrying their sentence. One prompt:

> I'm researching the open-access citation advantage, using my Filiation library. First fetch up
> to 10 more open-access PDFs. Then: (1) what do the papers' own texts say about how large the
> citation advantage is? Quote passages with their paper and page. (2) Pick the most relevant paper
> and tell me which earlier works it cites when making its main claim, with the sentence it cites
> them in. (3) Is there any paper in the library whose PDF looks wrong, or that has no searchable
> text, and why?

### What the assistant did

24 turns, US$0.82. It used every new tool unprompted:

| Calls | What for |
| --- | --- |
| `library_stats`, then `fetch_pdfs` `max_pdfs: 10` | 6 more PDFs (2 links were web pages, 2 refused) |
| `search_library` ×13 | Passages on the size of the advantage, then within single papers by `paper` |
| `neighbours` `cites`, `get_paper` ×6 | The chosen paper's references and their sentences; then each candidate for part 3 |

### Findings

**The answer quoted the papers, with pages.** Eysenbach 2006 p.1 ("twice as likely to be cited
(odds ratio = 2.1 …)"), Antelman 2004 p.5 (45% in philosophy to 86% in political science), Davis
2008 p.7 (17%, 11% without PNAS), Gargouri 2010 p.11, Hajjem 2006 p.5, Tennant 2016's table on p.8,
and Kurtz and Henneken 2007's dissent — each a passage `search_library` returned. It framed the
spread itself as the finding: whether a study controls for self-selection.

**Citation sentences did their job.** For Gargouri et al. 2010 it gave the main-claim sentence —
"This 'OA Impact Advantage' has been found in all fields analyzed so far … [3–12]" — and the six
works cited in it, from the library's mentions, marking the one stub as "title known, content not
fetched".

**The download check was used as meant.** It found Henneken et al. 2006 flagged as possibly another
paper, checked with `search_library` on that paper, and concluded the file is a different document
(a report by Brody et al.) sitting at that link — which it is. It explained missing PDFs by their
recorded reasons (`open_as_web_page_only`, `every_link_failed`).

**Two gaps, both fixed before the PR:**

- **It passed folder-style names** ("Eysenbach-2006") as `paper`, which fail, then recovered with fil
  IDs. The argument's description now says to pass the `fil_id` every result carries.
- **It missed the one paper with a gibberish text layer**: nothing listed the papers without text,
  so it could only find them one `get_paper` at a time. `library_stats` now reports `pdfs`, with
  each paper without text and why, and each download that may be another paper.

## M3 — 20 seeds, 5 fields, the papers on disk, 30 October 2026

ARCHITECTURE_PHASE3.md's step 11: M1's 20 seeds again, now for what M3 added — PDFs, their text,
search, citation sentences and import. Code: [`spikes/12-validation-m3`](../spikes/12-validation-m3/main.go).
Each seed got a library folder of its own, isolated from any real library: `fil add`, expansion to
200 works (M1 used 500; the PDFs are what is under test), `fil fetch` of at most 25 PDFs — open
access only, a refusal recorded and never worked around (hard rule 1) — which reads them. Then,
for every paper with text, a search for its own title's four longest words across the library.
After the seeds, an import check per field: 8 of the field's downloaded PDFs, renamed
`scan001.pdf`…, dropped into a fresh, empty library's `Inbox/`. Last, 50 citation sentences — 10
per field — checked by hand.

### The seed's own PDF

M3's last done-when item. PeerJ answers scripted downloads with a bot wall, so the PDF was saved
from PeerJ's own article page in a browser, then dropped into a scratch library's `Inbox/` after
`fil add 10.7717/peerj.4375`. `fil import` identified it by the DOI in its header, filed it in the
seed's folder in `papers/` and read it: 23 pages, 47 passages, searchable — "hybrid open access
citation advantage" finds it on pages 5, 1 and 17. **28 of its 47 references carry the sentence
they are cited in.** (OpenAlex now lists 47 references for the seed; M0 recorded 54.)

### Findings

**Every seed completed every step — 20 of 20.** 35m53s, and 487 of the day's 1,000 OpenAlex credits
— title searches cost 10 each, and the import check spends them.

**PDFs follow spike 9's gradient.** 226 downloaded from 476 papers tried: physics 72, computer
science 67, social sciences 55, medicine 18, humanities 14. Most failures — 187 of 288 — are publishers
refusing the request, the rest web pages served in place of a PDF and broken links — each recorded,
none retried around.

**Text: 219 of 226 PDFs searchable.** Five gave no text, four of them in one economics library, and
two a gibberish text layer — each with its reason in `fil stats --why`. Two layers
passed the gibberish check that should not have: an NBER scan with poor OCR, and a review whose
font drops "c" and "ff" ("Dislo ations", "e e tive ux"). Both are searchable only in part.

**Search finds a paper by its own title in 211 of 219 cases** — among the first five distinct
papers, with every word in one passage. Of the eight misses, five have no passage holding every
word: a wrong file (below), two OCR'd or front-matter PDFs, and a title whose words the PDF splits
or hyphenates. The other three are papers outranked in a library where many share the words —
three gravitational-wave and graphene titles. That is ranking, which M4's retrieval takes over.

**Citation sentences: 4,032 of 7,950 citations made by papers with text (51%).** Computer science
62%, physics 50%, social sciences 45%, medicine 32%, humanities 20%. Medicine is low because The
Lancet, JAMA and NEJM cite by superscript number — 0 of 32 for the vaccine trial's library — and
superscripts are not read (D23's follow-up). 36 of the 219 papers with text gave no citation at all.

**Hand check: 49 of 50 sentences tie to the right work**, at least 9 of 10 in every field. The one
wrong: "Sachs (1995b, 1995c)" was given to Sachs, Tornell and Velasco (1995) — the only Sachs of
1995 OpenAlex resolved, so the year looked unique. **Fixed:** a marker naming one author alone no
longer ties to a paper of three or more. Re-reading all 20 libraries with the fix dropped 8 of
4,032 sentences. Six sentences were usable but imperfect: three cut short — two at "p." before a
page number (**fixed**) and one at "10% [1]." — and three with page furniture glued in (a running
footer, a figure caption) or letters a font lost.

**The download check flagged 11 of 226 PDFs; 2 were right to.** An OpenAlex record of "Attention
Is All You Need" (2025, preprint) links a PDF of an unrelated NMR paper; a graphene paper's arXiv
copy carries its preprint title, "Quantum-limited shot noise in graphene". Of the 9 false alarms,
two were a bug — **fixed**: a surname of two words, "Aspuru‐Guzik" or "Martínez-Cantín", was
looked for as one. The other seven are not code: OpenAlex lists the wrong first author (1), an
affiliation letter set against a name ("Buonannoa,b") (1), OCR'd scans (2), and a table of
contents, an empty first page or a repository's cover sheet in front of the title (3). A flag only
marks a PDF to check; nothing is deleted.

**Import into an empty library: 15 of 40 PDFs filed on their own, all correctly; none filed
wrongly.** A title search never files on its own (ADR-005), so in a library that does not already
hold the papers most PDFs wait for a choice. For 15 more the right paper is among the candidates,
and `--accept-first` would have filed 13 of them — all correctly. For the last 10 it is not: NBER
working-paper covers read as the title ("NBER WORKING PAPER SERIES A MODEL"), scans with no text,
and garbled OCR. M3 step 8's 34 of 40 was measured in a library that held the papers. **Two bugs
found and fixed:** OpenAlex refuses `*` and `?` in a title search with a 400, and one garbled title
("Ill Structured P coblems*") stopped the whole import — wildcards are now stripped, and a refused
search leaves that one file waiting; and a work both a PDF's DOI and its title search named was
offered twice.

### Per seed

`go run ./spikes/12-validation-m3 -root <folder>`. Context and Silent are before the fixes above.
*Context* is citations from papers with text that carry their sentence; *Silent*, papers with text
in which no citation was found.

| Field | Seed | With a PDF link | Tried | PDFs | Failed (why) | Text: ok / no text / gibberish | Context | Silent | Mismatch | Fetch time |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Medicine | 10.1056/NEJMoa2034577 — BNT162b2 vaccine trial, 2020 | 824 | 25 | 2 | network 1, not-a-pdf 5, refused 18 | 2 / 0 / 0 | 0 / 32 (0%) | 2 | 0 | 53s |
| Medicine | 10.1016/S0140-6736(20)30183-5 — COVID-19 clinical features, Wuhan, 2020 | 947 | 25 | 7 | network 1, not-a-pdf 6, refused 11 | 7 / 0 / 0 | 62 / 161 (39%) | 3 | 0 | 1m5s |
| Medicine | 10.1001/jama.2020.1585 — 138 hospitalised COVID-19 patients, 2020 | 823 | 25 | 7 | network 3, not-a-pdf 9, refused 9 | 7 / 0 / 0 | 44 / 101 (44%) | 2 | 1 | 1m7s |
| Medicine | 10.1371/journal.pmed.0020124 — Why most published research findings are false, 2005 | 343 | 25 | 2 | refused 23 | 2 / 0 / 0 | 56 / 215 (26%) | 0 | 0 | 29s |
| Physics | 10.1103/PhysRevLett.116.061102 — LIGO GW150914, 2016 | 1441 | 25 | 23 | http-error 2, not-a-pdf 3, refused 4 | 23 / 0 / 0 | 569 / 1012 (56%) | 0 | 1 | 48s |
| Physics | 10.1103/PhysRev.47.777 — Einstein-Podolsky-Rosen, 1935 | 1 | 1 | 0 | refused 1 | 0 / 0 / 0 | — | 0 | 0 | 1s |
| Physics | 10.1038/s41586-019-1666-5 — Quantum supremacy, 2019 | 1215 | 25 | 24 | refused 1 | 24 / 0 / 0 | 443 / 870 (51%) | 5 | 1 | 1m4s |
| Physics | 10.1103/RevModPhys.81.109 — Electronic properties of graphene, 2009 | 793 | 25 | 25 | not-a-pdf 1, refused 4 | 25 / 0 / 0 | 479 / 1100 (44%) | 3 | 1 | 41s |
| Computer Science | arXiv:1706.03762 — Attention is all you need, 2017 | 699 | 25 | 19 | http-error 1, not-a-pdf 2, refused 6 | 19 / 0 / 0 | 383 / 638 (60%) | 2 | 1 | 2m4s |
| Computer Science | 10.1109/CVPR.2016.90 — Deep residual learning, 2016 | 357 | 25 | 17 | http-error 1, not-a-pdf 1, refused 4, timeout 2 | 17 / 0 / 0 | 496 / 651 (76%) | 0 | 0 | 3m2s |
| Computer Science | 10.1038/nature14539 — Deep learning, 2015 | 386 | 25 | 14 | http-error 3, not-a-pdf 3, refused 9 | 13 / 0 / 1 | 318 / 618 (51%) | 1 | 0 | 1m18s |
| Computer Science | 10.1145/3292500.3330701 — Optuna, 2019 | 607 | 25 | 17 | http-error 1, not-a-pdf 4, refused 3 | 17 / 0 / 0 | 370 / 628 (59%) | 1 | 1 | 1m57s |
| Social Sciences | 10.7717/peerj.4375 — The state of OA, 2018 | 606 | 25 | 18 | not-a-pdf 2, refused 6 | 18 / 0 / 0 | 205 / 332 (62%) | 4 | 0 | 1m0s |
| Social Sciences | 10.1086/225469 — The strength of weak ties, 1973 | 26 | 25 | 4 | not-a-pdf 8, not-found 1, refused 12, timeout 1 | 4 / 0 / 0 | 3 / 19 (16%) | 3 | 2 | 2m13s |
| Social Sciences | 10.1126/science.aac4716 — Reproducibility of psychological science, 2015 | 264 | 25 | 12 | not-a-pdf 8, refused 8 | 11 / 0 / 1 | 391 / 816 (48%) | 1 | 0 | 47s |
| Social Sciences | 10.1257/aer.91.5.1369 — Colonial origins of comparative development, 2001 | 251 | 25 | 21 | duplicate 1, http-error 1, refused 2 | 17 / 4 / 0 | 171 / 548 (31%) | 2 | 1 | 3m17s |
| Arts and Humanities | 10.2307/412243 — Turn-taking for conversation, 1974 | 53 | 25 | 3 | network 1, not-a-pdf 7, refused 16, timeout 2 | 3 / 0 / 0 | 1 / 3 (33%) | 2 | 0 | 2m21s |
| Arts and Humanities | 10.1177/030631289019003001 — Boundary objects, 1989 | 196 | 25 | 3 | http-error 1, not-a-pdf 5, refused 17 | 3 / 0 / 0 | 5 / 52 (10%) | 2 | 1 | 1m38s |
| Arts and Humanities | 10.1098/rspb.1979.0086 — The spandrels of San Marco, 1979 | 201 | 25 | 5 | http-error 1, not-a-pdf 6, refused 16 | 4 / 1 / 0 | 16 / 53 (30%) | 2 | 1 | 1m17s |
| Arts and Humanities | W2266294403 — Meeting the Universe Halfway, 2007 | 137 | 25 | 3 | http-error 7, refused 17 | 3 / 0 / 0 | 20 / 101 (20%) | 1 | 0 | 36s |

### By field

| Field | PDFs | Failed downloads | Text ok | No text / gibberish | Citations with a sentence | Title search in top 5 | Hand check | Import: filed (right) / offered / not offered |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Medicine | 18 | 86 | 18 | 0 | 162 / 509 (32%) | 18 / 18 | 10 / 10 | 4 (4) / 4 / 0 |
| Physics | 72 | 16 | 72 | 0 | 1,491 / 2,982 (50%) | 68 / 72 | 10 / 10 | 3 (3) / 4 / 1 |
| Computer Science | 67 | 40 | 66 | 1 | 1,567 / 2,535 (62%) | 65 / 66 | 10 / 10 | 4 (4) / 4 / 0 |
| Social Sciences | 55 | 50 | 50 | 5 | 770 / 1,715 (45%) | 49 / 50 | 9 / 10 | 2 (2) / 2 / 4 |
| Arts and Humanities | 14 | 96 | 13 | 1 | 42 / 209 (20%) | 11 / 13 | 10 / 10 | 2 (2) / 1 / 5 |

The title search column is from `-recheck-search`, and the import column from `-recheck-imports`,
both after the fixes and the review of this PR: the first run's humanities import stopped at the
wildcard bug with nothing filed.
