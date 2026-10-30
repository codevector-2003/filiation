# spikes/

Throwaway programs that answer one question each, kept because they are the evidence behind
several ADRs. **Results are written up in [`../docs/SPIKES.md`](../docs/SPIKES.md)** — read that
first; this directory is for re-running a measurement when you doubt it.

| Command | Question |
| --- | --- |
| `go run ./spikes/1-batch-by-id` | Can works be fetched in batches by OpenAlex ID, and what is the ceiling? |
| `go run ./spikes/2-per-page` | What is the real `per_page` maximum? |
| `go run ./spikes/3-rate-limit` | What request rate does OpenAlex actually allow, and what does a call cost? |
| `go run ./spikes/4-reference-coverage` | What fraction of works have a usable reference list, by field? |
| `go run ./spikes/5-fts5` | Does the SQLite driver support FTS5, and does the real schema apply? |
| `go run ./spikes/6-vectors` | Does vector search work in the same file, and is it fast enough? |
| `go run ./spikes/8-validation` | The M1 validation run: 20 seeds, 5 fields, one library each |
| `go run ./spikes/9-pdf-availability -dir <dir>` | How many works have a PDF we can actually download, and does Unpaywall add any? Set `FIL_SPIKE_UNPAYWALL_EMAIL` to include Unpaywall; the address is never written down |
| `cd spikes/10-pdf-text && go run . -pdfs <dir>/pdfs -pdftotext <path> -out <dir>` | Which PDF text extractor? Its own module, so the candidates stay out of fil's `go.mod` |
| `go run ./spikes/11-reference-matching -text <dir>/text -cache <dir>/cache` | Can a PDF's reference list be split and matched to real works? |
| `go run ./spikes/12-validation-m3 -root <dir>` | The M3 validation run: the same 20 seeds, now with PDFs, text, search, citation sentences and import. `-recheck-imports` and `-recheck-search` repeat only the import check or the title search. `<dir>` should not be a temporary folder — the hand check reads it afterwards |

Spike 7 (cross-compilation) has no program — it is three `go build` invocations, recorded in
`docs/SPIKES.md`.

## Rules for anything added here

- **They hit the live API.** 1–4 make real requests to OpenAlex, a free service run by a
  non-profit. Keep them small, keep them sequential, and abort rather than retry on a 429. Spike 3
  is deliberately the only one that pushes, and it stops at the first sign of throttling.
- **Measure, do not just execute.** Spike 6 checks its results against a brute-force computation
  in Go. A query that runs and returns the wrong ranking is worse than one that fails.
- **A spike may fail.** That is a result, not a defect. Spike 6 is a partial pass and the write-up
  says so.
- These are not part of the product. Nothing under `internal/` may import them, and they are
  excluded from the release build by living outside `cmd/`.
