// Command spike1 tests whether OpenAlex works can be fetched in batches by ID.
//
// Spike 1 of docs/ARCHITECTURE_PHASE1.md §9. ADR-002's cost model rests on this
// entirely: a list request costs 10 credits and returns up to 50 works (0.20
// credits/work), against 1 credit for a single work (1.00 credits/work). If
// filtering by a pipe-separated list of OpenAlex IDs does not work, batching
// collapses, expansion costs 5x more, and the fallback is batching by DOI —
// which silently drops every work that has none.
//
// Measures three things:
//  1. Does filter=openalex_id:W1|W2|... work at all, and does ids.openalex: too?
//  2. What is the ceiling on the number of piped values — 50, 100, or higher?
//  3. Does referenced_works arrive complete inside a batched list response?
//     ("edges arrive free" is the premise of the whole expansion algorithm)
//
// Usage:
//
//	go run ./spikes/1-batch-by-id
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	mailto    = "samprasharendaishika@gmail.com"
	userAgent = "filiation-spike/0.0 (https://github.com/codevector-2003/filiation; mailto:" + mailto + ")"
)

var client = &http.Client{Timeout: 90 * time.Second}

type work struct {
	ID                   string   `json:"id"`
	ReferencedWorks      []string `json:"referenced_works"`
	ReferencedWorksCount int      `json:"referenced_works_count"`
}

type response struct {
	Meta struct {
		Count   int `json:"count"`
		PerPage int `json:"per_page"`
	} `json:"meta"`
	Results []work `json:"results"`
}

func main() {
	fmt.Fprintln(os.Stderr, "collecting real work IDs to batch...")
	ids, err := collectIDs(150)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not collect IDs: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "got %d IDs\n\n", len(ids))

	fmt.Println("## Filter syntax")
	fmt.Println()
	fmt.Println("| Filter key | IDs sent | HTTP | meta.count | results | Verdict |")
	fmt.Println("| --- | --- | --- | --- | --- | --- |")
	for _, key := range []string{"openalex_id", "ids.openalex"} {
		tryBatch(key, ids[:25])
	}

	fmt.Println()
	fmt.Println("## Ceiling on piped values")
	fmt.Println()
	fmt.Println("| Filter key | IDs sent | HTTP | meta.count | results | Verdict |")
	fmt.Println("| --- | --- | --- | --- | --- | --- |")
	for _, n := range []int{50, 100, 101, 150} {
		if n > len(ids) {
			continue
		}
		tryBatch("openalex_id", ids[:n])
	}

	fmt.Println()
	fmt.Println("## Do references arrive inside a batched response?")
	fmt.Println()
	checkRefsInBatch(ids[:50])
}

// collectIDs pulls real work IDs that are likely to have references, so the
// batch test is not measuring empty records. Simple page paging: 50 per page,
// as many pages as it takes to reach n.
func collectIDs(n int) ([]string, error) {
	var ids []string

	for page := 1; len(ids) < n && page <= 5; page++ {
		q := url.Values{}
		q.Set("filter", "referenced_works_count:>30,has_doi:true,publication_year:2018-2023")
		q.Set("per_page", "50")
		q.Set("page", fmt.Sprint(page))
		q.Set("select", "id")
		q.Set("mailto", mailto)

		var out response
		status, err := get("https://api.openalex.org/works?"+q.Encode(), &out)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d while collecting IDs on page %d", status, page)
		}
		if len(out.Results) == 0 {
			break
		}
		for _, w := range out.Results {
			ids = append(ids, shortID(w.ID))
		}
		time.Sleep(time.Second)
	}

	ids = dedupe(ids)
	if len(ids) > n {
		ids = ids[:n]
	}
	return ids, nil
}

func tryBatch(filterKey string, ids []string) {
	q := url.Values{}
	q.Set("filter", filterKey+":"+strings.Join(ids, "|"))
	q.Set("per_page", "100")
	q.Set("select", "id")
	q.Set("mailto", mailto)

	var out response
	status, err := get("https://api.openalex.org/works?"+q.Encode(), &out)
	time.Sleep(time.Second)

	if err != nil {
		fmt.Printf("| `%s` | %d | — | — | — | transport error: %v |\n", filterKey, len(ids), err)
		return
	}

	verdict := "works"
	switch {
	case status != http.StatusOK:
		verdict = "**REJECTED**"
	case out.Meta.Count == len(ids):
		verdict = "all returned"
	case out.Meta.Count < len(ids):
		verdict = fmt.Sprintf("**only %d of %d matched**", out.Meta.Count, len(ids))
	case out.Meta.Count > len(ids):
		verdict = "**over-matched — filter ignored?**"
	}

	fmt.Printf("| `%s` | %d | %d | %d | %d | %s |\n",
		filterKey, len(ids), status, out.Meta.Count, len(out.Results), verdict)
}

func checkRefsInBatch(ids []string) {
	q := url.Values{}
	q.Set("filter", "openalex_id:"+strings.Join(ids, "|"))
	q.Set("per_page", "50")
	q.Set("select", "id,referenced_works,referenced_works_count")
	q.Set("mailto", mailto)

	var out response
	status, err := get("https://api.openalex.org/works?"+q.Encode(), &out)
	if err != nil || status != http.StatusOK {
		fmt.Printf("failed: HTTP %d, err %v\n", status, err)
		return
	}

	truncated, empty, total := 0, 0, 0
	for _, w := range out.Results {
		if len(w.ReferencedWorks) != w.ReferencedWorksCount {
			truncated++
		}
		if len(w.ReferencedWorks) == 0 {
			empty++
		}
		total += len(w.ReferencedWorks)
	}

	fmt.Printf("%d works returned, %d edges total, mean %.1f refs/work.\n",
		len(out.Results), total, float64(total)/float64(max(len(out.Results), 1)))
	fmt.Printf("Truncated (len != referenced_works_count): %d — must be 0.\n", truncated)
	fmt.Printf("Empty reference lists: %d.\n", empty)
	fmt.Println()
	fmt.Printf("Cost: 1 list request = 10 credits for %d works and %d edges.\n", len(out.Results), total)
	if len(out.Results) > 0 {
		fmt.Printf("That is %.2f credits per work, against 1.00 for single fetches.\n",
			10.0/float64(len(out.Results)))
	}
}

func get(u string, into any) (int, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, nil
	}
	return resp.StatusCode, json.Unmarshal(body, into)
}

// shortID turns https://openalex.org/W123 into W123. The filter takes either,
// but the short form keeps URLs under any length limit.
func shortID(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

func dedupe(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
