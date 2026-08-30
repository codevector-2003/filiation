// Command spike2 finds the real per_page maximum on the OpenAlex works endpoint.
//
// Spike 2 of docs/ARCHITECTURE_PHASE1.md §9: the documentation gives both 100
// and 200. It matters because a list request costs a flat 10 credits regardless
// of how many works come back, so the page size sets the credits-per-work floor
// once spike 1 has settled how many IDs may be filtered at once.
//
// Usage:
//
//	go run ./spikes/2-per-page
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	mailto    = "samprasharendaishika@gmail.com"
	userAgent = "filiation-spike/0.0 (https://github.com/codevector-2003/filiation; mailto:" + mailto + ")"
)

type response struct {
	Meta struct {
		Count   int `json:"count"`
		PerPage int `json:"per_page"`
	} `json:"meta"`
	Results []struct {
		ID string `json:"id"`
	} `json:"results"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

func main() {
	client := &http.Client{Timeout: 90 * time.Second}

	fmt.Println("| per_page requested | HTTP | meta.per_page | results returned | Note |")
	fmt.Println("| --- | --- | --- | --- | --- |")

	for _, n := range []int{50, 100, 150, 200, 201, 500} {
		q := url.Values{}
		q.Set("filter", "publication_year:2020,type:article,has_doi:true")
		q.Set("per_page", fmt.Sprint(n))
		q.Set("select", "id")
		q.Set("mailto", mailto)

		req, _ := http.NewRequest(http.MethodGet, "https://api.openalex.org/works?"+q.Encode(), nil)
		req.Header.Set("User-Agent", userAgent)

		resp, err := client.Do(req)
		if err != nil {
			fmt.Printf("| %d | — | — | — | transport error: %v |\n", n, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var out response
		_ = json.Unmarshal(body, &out)

		note := "ok"
		if resp.StatusCode != http.StatusOK {
			note = "**rejected**: " + firstLine(out.Message+out.Error+snippet(body))
		} else if len(out.Results) < n {
			note = fmt.Sprintf("**capped at %d**", len(out.Results))
		}

		fmt.Printf("| %d | %d | %d | %d | %s |\n",
			n, resp.StatusCode, out.Meta.PerPage, len(out.Results), note)

		time.Sleep(time.Second)
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	if len(s) > 160 {
		s = s[:160] + "..."
	}
	return s
}

func snippet(b []byte) string { return string(b) }
