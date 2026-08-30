// Command spike3 measures the sustained request rate OpenAlex allows in the
// polite pool.
//
// Spike 3 of docs/ARCHITECTURE_PHASE1.md §9: the documentation says 10 requests
// per second in one place and 100 in another. ADR-004 sets a rate.Limiter token
// bucket "well below the measured limit", and it cannot be set from a number
// that is off by 10x in an unknown direction.
//
// This deliberately probes a free public service, so it is built to be polite:
//   - single-work requests rather than list requests
//   - a short burst at each target rate, not a sustained flood
//   - it aborts the entire run on the first 429 rather than confirming it twice
//   - rotating IDs, so a CDN cache does not make the test meaningless
//
// The single-work choice was made to be cheap on credits, and the run then
// discovered why it was cheaper than expected: single-work fetches cost nothing
// at all, and list requests cost 1 credit against a 1,000/day allowance — the
// reverse of what the design documents assumed. See docs/SPIKES.md.
//
// Usage:
//
//	go run ./spikes/3-rate-limit
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
	"sync"
	"sync/atomic"
	"time"
)

const (
	mailto      = "samprasharendaishika@gmail.com"
	userAgent   = "filiation-spike/0.0 (https://github.com/codevector-2003/filiation; mailto:" + mailto + ")"
	burstWindow = 2 * time.Second
)

var (
	client   = &http.Client{Timeout: 30 * time.Second}
	got429   atomic.Bool
	hdrsOnce sync.Once
)

func main() {
	fmt.Fprintln(os.Stderr, "collecting IDs to rotate through...")
	ids, err := collectIDs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not collect IDs: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "got %d IDs\n\n", len(ids))

	fmt.Println("| Target rate | Sent | 2xx | 429 | Other | Achieved req/s | p50 latency | p95 latency |")
	fmt.Println("| --- | --- | --- | --- | --- | --- | --- | --- |")

	for _, rate := range []int{5, 10, 25, 50, 100} {
		if got429.Load() {
			fmt.Printf("| %d/s | — | — | — | — | not attempted — stopped after first 429 | | |\n", rate)
			continue
		}
		burst(ids, rate)
		time.Sleep(3 * time.Second) // let any bucket refill before the next step
	}

	fmt.Println()
	if got429.Load() {
		fmt.Println("A 429 was observed. Set the token bucket well below the rate that produced it.")
	} else {
		fmt.Println("No 429 at any attempted rate. The limit is at or above 100 req/s, which means")
		fmt.Println("the bucket should be set by politeness rather than by the ceiling: this is a")
		fmt.Println("free service funded by a non-profit, and the tool has no reason to go fast.")
	}
}

func burst(ids []string, targetRate int) {
	var (
		wg                 sync.WaitGroup
		mu                 sync.Mutex
		latencies          []time.Duration
		ok, tooMany, other int
	)

	interval := time.Second / time.Duration(targetRate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	deadline := time.Now().Add(burstWindow)
	start := time.Now()
	sent := 0

	for time.Now().Before(deadline) {
		<-ticker.C
		if got429.Load() {
			break
		}
		id := ids[sent%len(ids)]
		sent++
		wg.Add(1)

		go func(id string) {
			defer wg.Done()
			t0 := time.Now()
			status := fetch(id)
			d := time.Since(t0)

			mu.Lock()
			defer mu.Unlock()
			latencies = append(latencies, d)
			switch {
			case status == http.StatusTooManyRequests:
				tooMany++
				got429.Store(true)
			case status >= 200 && status < 300:
				ok++
			default:
				other++
			}
		}(id)
	}

	wg.Wait()
	elapsed := time.Since(start)

	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	fmt.Printf("| %d/s | %d | %d | %d | %d | %.1f | %s | %s |\n",
		targetRate, sent, ok, tooMany, other,
		float64(sent)/elapsed.Seconds(),
		percentile(latencies, 0.50), percentile(latencies, 0.95))
}

func fetch(id string) int {
	u := "https://api.openalex.org/works/" + id + "?select=id&mailto=" + url.QueryEscape(mailto)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	// Print any rate-limit headers once — they are worth more than the timing.
	hdrsOnce.Do(func() {
		var found []string
		for k, v := range resp.Header {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "ratelimit") || strings.Contains(lk, "retry-after") ||
				strings.Contains(lk, "x-api") {
				found = append(found, fmt.Sprintf("%s: %s", k, strings.Join(v, ", ")))
			}
		}
		sort.Strings(found)
		if len(found) == 0 {
			fmt.Fprintln(os.Stderr, "(no rate-limit headers returned)")
		} else {
			fmt.Fprintln(os.Stderr, "rate-limit headers: "+strings.Join(found, " | "))
		}
	})

	return resp.StatusCode
}

func collectIDs() ([]string, error) {
	q := url.Values{}
	q.Set("filter", "publication_year:2020,type:article")
	q.Set("per_page", "200")
	q.Set("select", "id")
	q.Set("mailto", mailto)

	req, err := http.NewRequest(http.MethodGet, "https://api.openalex.org/works?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var out struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(out.Results))
	for _, r := range out.Results {
		if i := strings.LastIndex(r.ID, "/"); i >= 0 {
			ids = append(ids, r.ID[i+1:])
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no IDs returned")
	}
	return ids, nil
}

func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted)-1) * p)
	return sorted[i].Round(time.Millisecond)
}
