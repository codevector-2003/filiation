// Package blobs is the filesystem half of a library: its folder, and the
// papers/ folder inside it where PDFs live (ADR-011, ADR-012).
//
// D4 named each PDF by its SHA-256. D18 replaced that with readable names, one
// folder per work — Piwowar-2018-The-state-of-OA--F7K2M9QXA/published.pdf — and
// moved the hash into the database, where it still deduplicates and still
// verifies. This package knows the names, the layout and the bytes; it knows
// nothing about SQL. What a file belongs to is internal/store's business, and
// reconciling the two is internal/library's.
//
// Two rules hold everywhere here:
//
//   - The researcher owns the tree. fil writes new folders and files, and
//     renames only what it named; it never moves, edits or deletes anything a
//     person put there.
//   - Writes are atomic. A file is written under .filiation/tmp/, synced,
//     checked and only then renamed into place, so a crash leaves at most a
//     temporary file — never a bad PDF under a good name.
package blobs
