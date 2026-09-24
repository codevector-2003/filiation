# M1 validation run — 24 September 2026

ARCHITECTURE_PHASE1.md §10's "20-paper / 5-field validation run", done against live OpenAlex
before the first release. Code: [`spikes/8-validation`](../spikes/8-validation/main.go). Each seed
got a library of its own, one `fil expand` with the defaults (500 works, depth 3), a title-duplicate
report (D14) and a GraphML export read back by an independent parser.

## Findings

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

## By field

| Field | Seeds OK | Fetched | Dead ends | Coverage | Not found | Title dups per 100 fetched |
| --- | --- | --- | --- | --- | --- | --- |
| Medicine | 4 / 4 | 1977 | 136 | 93% | 23 | 1.4 |
| Physics | 4 / 4 | 1467 | 128 | 91% | 33 | 2.7 |
| Computer Science | 4 / 4 | 1871 | 216 | 88% | 124 | 3.8 |
| Social Sciences | 4 / 4 | 1918 | 777 | 59% | 81 | 3.8 |
| Arts and Humanities | 4 / 4 | 1957 | 862 | 56% | 42 | 4.1 |

20 of 20 seeds completed every step.
