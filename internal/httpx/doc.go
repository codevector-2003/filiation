// Package httpx is the shared HTTP layer for every outbound request.
//
// It provides the token bucket (golang.org/x/time/rate, per ADR-004), retry
// with backoff on 429 and 5xx, and an on-disk response cache so that re-running
// an expansion does not re-pay for work already done.
//
// Every package under sources/ dials through here, and nothing else makes a raw
// http.Client call. A limiter that only some callers respect is how you get
// rate limited on somebody else's free tier.
package httpx
