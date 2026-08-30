// Package sources holds one subpackage per external API.
//
// One package per source means rate limiting, retry, caching and response-shape
// quirks live in exactly one place per upstream. All of them dial out through
// internal/httpx.
//
// Subpackages return their own types. Mapping those onto rows belongs to
// internal/graph — sources never imports store, and store never imports
// sources.
//
// No network in unit tests: record real responses as fixtures and replay them
// through an http.RoundTripper stub.
package sources
