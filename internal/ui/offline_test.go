package ui

import (
	"io"
	"net/http"
	"strings"
)

// offline stands in for OpenAlex in these tests, answering every request
// with no results. Without it, importing from the Inbox searched the real
// OpenAlex by title: slow from a shared or busy address, and its answers
// change, so a test could wait a minute and then fail (no network in unit
// tests, CLAUDE.md).
type offline struct{}

func (offline) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"meta":{"count":0,"page":1,"per_page":25},"results":[]}`)),
		Request:    r,
	}, nil
}
