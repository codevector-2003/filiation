// Package zotero reads a user's Zotero library through the local API that
// Zotero 7 and later serve on this computer (D29).
//
// Read-only, always: Filiation must never write to somebody's reference
// manager. The client sends GET requests and nothing else, to 127.0.0.1 only,
// so nothing about the library leaves the computer through it.
package zotero
