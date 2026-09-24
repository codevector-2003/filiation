// Package httpx is the shared HTTP layer for every outbound request.
//
// It provides the token bucket (golang.org/x/time/rate, per ADR-004), retry
// with backoff on 429 and 5xx, and an on-disk response cache so that re-running
// an expansion does not re-pay for work already done.
//
// Every package under sources/ dials through here, and nothing else makes a raw
// http.Client call. A limiter that only some callers respect is how you get
// rate limited on somebody else's free tier.
//
// Build one Client per upstream service, once, and share it: each service has
// its own rate limit, and a limiter is only a limit if every request to that
// service waits on the same one.
//
// The response cache is a directory of files, not the separate SQLite file
// ADR-004 proposed. See DirCache for why.
package httpx
