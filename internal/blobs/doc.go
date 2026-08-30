// Package blobs stores PDFs on the filesystem, named by the SHA-256 of their
// contents.
//
// Content addressing buys deduplication for free — the same paper fetched from
// two OA locations is written once — and makes backup a plain directory copy.
// The database stores the hash only; no binary ever enters SQLite.
//
// Layout is sharded to keep directory sizes sane:
//
//	blobs/ab/cd/abcd...pdf
//
// Writes are atomic: temp file, fsync, rename. A half-written PDF sitting under
// a hash that does not match its contents is the one failure this package must
// never produce.
package blobs
