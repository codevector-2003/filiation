// Command spike4 measures OpenAlex reference coverage across research fields.
//
// Spike 4 of docs/ARCHITECTURE_PHASE1.md §9, and the one that document calls
// "the biggest unknown in the entire product". Filiation's whole premise is that
// referenced_works is complete enough to walk. Coverage depends on what
// publishers deposit with Crossref, which varies by field, so this is a product
// question wearing an engineering costume.
//
// Method: draw a random sample per field rather than hand-picking papers.
// Famous papers are exactly the ones most likely to be well indexed, so picking
// them by hand measures the best case and calls it the average.
//
// Usage:
//
//	go run ./spikes/4-reference-coverage
//
// Prints a markdown table for pasting into docs/SPIKES.md.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	mailto     = "samprasharendaishika@gmail.com"
	userAgent  = "filiation-spike/0.0 (https://github.com/codevector-2003/filiation; mailto:" + mailto + ")"
	sampleSize = 50
	seed       = 42
)

// OpenAlex field IDs come from the topics hierarchy (domain > field > subfield).
// Five picked to span the range: two where deposit practice is known to be good,
// one clinical, one physical, and humanities — the usual worst case.
var fields = []struct {
	id   string
	name string
}{
	{"fields/17", "Computer Science"},
	{"fields/27", "Medicine"},
	{"fields/31", "Physics and Astronomy"},
	{"fields/33", "Social Sciences"},
	{"fields/12", "Arts and Humanities"},
}

type work struct {
	ID                   string   `json:"id"`
	DOI                  string   `json:"doi"`
	Year                 int      `json:"publication_year"`
	ReferencedWorks      []string `json:"referenced_works"`
	ReferencedWorksCount int      `json:"referenced_works_count"`
	PrimaryTopic         *struct {
		Field struct {
			DisplayName string `json:"display_name"`
		} `json:"field"`
	} `json:"primary_topic"`
}

type response struct {
	Meta struct {
		Count int `json:"count"`
	} `json:"meta"`
	Results []work `json:"results"`
}

type result struct {
	field       string
	confirmedAs string
	n           int
	zero        int // referenced_works empty
	truncated   int // len(referenced_works) != referenced_works_count
	noDOI       int
	median      int
	mean        float64
}

func main() {
	client := &http.Client{Timeout: 60 * time.Second}
	results := make([]result, 0, len(fields))

	for _, f := range fields {
		fmt.Fprintf(os.Stderr, "sampling %s (%s)...\n", f.name, f.id)
		r, err := sampleField(client, f.id, f.name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  FAILED: %v\n", err)
			continue
		}
		results = append(results, r)
		time.Sleep(time.Second) // one request per second; well inside any published limit
	}

	report(results)
	frontierReport(client)
}

// frontierCoverage answers the question expansion actually cares about.
//
// A uniform random sample of OpenAlex is not what an expansion hydrates. It
// hydrates works that appear in someone's reference list, which is a
// citation-weighted sample and a completely different population — editorials,
// errata and thin records rarely get cited. This measures depth 1 from a real
// seed: of the papers a seed actually references, how many can be expanded
// further?
func frontierCoverage(c *http.Client, fieldID, fieldName string) (result, error) {
	// A paper a researcher would plausibly seed with: well cited, real reference
	// list, in the field.
	q := url.Values{}
	q.Set("filter", "primary_topic.field.id:"+fieldID+",referenced_works_count:>20,cited_by_count:>200,type:article")
	q.Set("sort", "cited_by_count:desc")
	q.Set("per_page", "1")
	q.Set("select", "id,referenced_works,referenced_works_count")
	q.Set("mailto", mailto)

	var seeds response
	if err := getJSON(c, "https://api.openalex.org/works?"+q.Encode(), &seeds); err != nil {
		return result{}, err
	}
	if len(seeds.Results) == 0 {
		return result{}, fmt.Errorf("no seed found for %s", fieldName)
	}
	seed := seeds.Results[0]
	time.Sleep(time.Second)

	refs := seed.ReferencedWorks
	if len(refs) > 100 { // spike 1: 100 is the hard ceiling on piped IDs
		refs = refs[:100]
	}
	for i, r := range refs {
		if j := strings.LastIndex(r, "/"); j >= 0 {
			refs[i] = r[j+1:]
		}
	}

	q = url.Values{}
	q.Set("filter", "openalex_id:"+strings.Join(refs, "|"))
	q.Set("per_page", "100")
	q.Set("select", "id,doi,referenced_works,referenced_works_count")
	q.Set("mailto", mailto)

	var out response
	if err := getJSON(c, "https://api.openalex.org/works?"+q.Encode(), &out); err != nil {
		return result{}, err
	}

	r := result{field: fieldName, confirmedAs: shortID(seed.ID), n: len(out.Results)}
	counts := make([]int, 0, len(out.Results))
	total := 0
	for _, w := range out.Results {
		if len(w.ReferencedWorks) == 0 {
			r.zero++
		}
		if w.DOI == "" {
			r.noDOI++
		}
		counts = append(counts, len(w.ReferencedWorks))
		total += len(w.ReferencedWorks)
	}
	sort.Ints(counts)
	if len(counts) > 0 {
		r.median = counts[len(counts)/2]
		r.mean = float64(total) / float64(len(counts))
	}
	return r, nil
}

func frontierReport(c *http.Client) {
	fmt.Println()
	fmt.Println("## Frontier coverage — works reached by following one seed's references")
	fmt.Println()
	fmt.Println("| Field | Seed | Refs hydrated | No refs | Median refs | Mean refs | No DOI |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- |")

	var totalN, totalZero int
	for _, f := range fields {
		fmt.Fprintf(os.Stderr, "frontier for %s...\n", f.name)
		r, err := frontierCoverage(c, f.id, f.name)
		if err != nil {
			fmt.Printf("| %s | — | — | — | — | — | failed: %v |\n", f.name, err)
			continue
		}
		fmt.Printf("| %s | %s | %d | %d (%.0f%%) | %d | %.1f | %d |\n",
			r.field, r.confirmedAs, r.n, r.zero, pct(r.zero, r.n), r.median, r.mean, r.noDOI)
		totalN += r.n
		totalZero += r.zero
		time.Sleep(time.Second)
	}

	fmt.Println()
	fmt.Printf("Frontier overall: %d works, %d with no references (%.0f%%).\n",
		totalN, totalZero, pct(totalZero, totalN))
	fmt.Println()
	fmt.Println("Compare against the uniform sample above. If this number is much lower, the")
	fmt.Println("uniform sample was measuring a long tail of records nobody would ever expand")
	fmt.Println("into, and the graph is healthier than that table suggests.")
}

func getJSON(c *http.Client, u string, into any) error {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(body))
	}
	return json.Unmarshal(body, into)
}

func shortID(id string) string {
	if i := strings.LastIndex(id, "/"); i >= 0 {
		return id[i+1:]
	}
	return id
}

func sampleField(c *http.Client, fieldID, fieldName string) (result, error) {
	q := url.Values{}
	q.Set("filter", "primary_topic.field.id:"+fieldID+",publication_year:2015-2023,type:article")
	q.Set("sample", fmt.Sprint(sampleSize))
	q.Set("seed", fmt.Sprint(seed))
	q.Set("per_page", fmt.Sprint(sampleSize))
	q.Set("select", "id,doi,publication_year,referenced_works,referenced_works_count,primary_topic")
	q.Set("mailto", mailto)

	u := "https://api.openalex.org/works?" + q.Encode()

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return result{}, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.Do(req)
	if err != nil {
		return result{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return result{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return result{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(body))
	}

	var out response
	if err := json.Unmarshal(body, &out); err != nil {
		return result{}, fmt.Errorf("decode: %w (body: %s)", err, snippet(body))
	}

	r := result{field: fieldName, n: len(out.Results)}
	counts := make([]int, 0, len(out.Results))
	total := 0

	for _, w := range out.Results {
		if len(w.ReferencedWorks) == 0 {
			r.zero++
		}
		// If these disagree, the list response is truncating references and the
		// whole "edges arrive free inside the work object" premise is weaker
		// than ADR-002 assumes.
		if len(w.ReferencedWorks) != w.ReferencedWorksCount {
			r.truncated++
		}
		if w.DOI == "" {
			r.noDOI++
		}
		if w.PrimaryTopic != nil && r.confirmedAs == "" {
			r.confirmedAs = w.PrimaryTopic.Field.DisplayName
		}
		counts = append(counts, len(w.ReferencedWorks))
		total += len(w.ReferencedWorks)
	}

	sort.Ints(counts)
	if len(counts) > 0 {
		r.median = counts[len(counts)/2]
		r.mean = float64(total) / float64(len(counts))
	}
	return r, nil
}

func report(rs []result) {
	fmt.Println()
	fmt.Println("| Field | Confirmed as | n | No refs | Median refs | Mean refs | Truncated | No DOI |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- |")

	var totalN, totalZero, totalTrunc int
	for _, r := range rs {
		fmt.Printf("| %s | %s | %d | %d (%.0f%%) | %d | %.1f | %d | %d |\n",
			r.field, r.confirmedAs, r.n,
			r.zero, pct(r.zero, r.n), r.median, r.mean, r.truncated, r.noDOI)
		totalN += r.n
		totalZero += r.zero
		totalTrunc += r.truncated
	}

	fmt.Println()
	fmt.Printf("Overall: %d works sampled, %d with no references (%.0f%%), %d truncated.\n",
		totalN, totalZero, pct(totalZero, totalN), totalTrunc)
	fmt.Println()
	fmt.Println("Reading: 'No refs' is the number that matters — those works are leaves in the")
	fmt.Println("graph no matter how much budget is spent on them. 'Truncated' must be 0, or")
	fmt.Println("referenced_works does not arrive complete inside the list response and ADR-002")
	fmt.Println("needs a second call per work.")
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
