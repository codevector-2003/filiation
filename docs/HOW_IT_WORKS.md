# How it works

<p align="center">
  <img src="assets/how-it-works.svg" width="860" alt="How fil works: one paper, resolved references from OpenAlex, a library on your machine, then path, neighbours and export">
</p>

- **References come from OpenAlex, already resolved.** fil never parses reference lists out of PDFs, a process whose errors compound across a graph.
- **Edges before nodes.** A paper's references become records the moment it's fetched. Deduplication is then a primary-key match, and "which unfetched paper matters most?" becomes a cheap count of how often your library cites it.
- **Every batch is one transaction.** Interrupting a run loses at most the batch in flight, and the library is never half-written.
- **Polite to OpenAlex.** Requests are batched 100 works at a time, rate-limited below the measured limit, retried with backoff, and cached.
- **Open access only.** PDFs come from the open-access copies OpenAlex lists: published version first, then accepted manuscript, then preprint. A publisher that refuses is recorded, never worked around.
- **Text is read locally.** PDFium, compiled to WebAssembly, reads each PDF inside fil itself. Search runs on SQLite's full-text index in the same library file.
- **Citations keep their sentence.** When fil reads a paper, it finds where each reference is cited and keeps that sentence and its page, so you can see *why* one paper cites another.

The full design, and the measurements behind it, are in [ARCHITECTURE.md](ARCHITECTURE.md),
[DECISIONS.md](DECISIONS.md) and [SPIKES.md](SPIKES.md).

## Know this before you start

**The graph is only as deep as the reference data behind it, and that depends on your field.** In the [validation run](VALIDATION.md) (20 seed papers across 5 fields, 500 works each), the share of fetched works that came with a reference list was:

| Medicine | Physics | Computer science | Social sciences | Arts & humanities |
| :---: | :---: | :---: | :---: | :---: |
| 93% | 91% | 88% | 59% | 56% |

Where books and chapters dominate, publishers rarely deposit reference lists, and a random humanities sample fares worse still ([spike 4](SPIKES.md)). fil tells you when a work has no references. That's a gap in the data, not a fault in the tool.

**Open-access PDFs follow the same gradient.** In the [M3 validation run](VALIDATION.md#m3--20-seeds-5-fields-the-papers-on-disk-30-october-2026), up to 25 PDFs were tried for each of four seeds per field: physics ended with 72 PDFs, the humanities with 14. Publishers refuse most scripted downloads, and fil does not get around that. PDFs you already have can always be imported.
