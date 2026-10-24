// Command pdf-availability is spike 9 (ARCHITECTURE_PHASE3.md §6): of the works
// in the M1 validation libraries, how many have a PDF we can actually
// download — per field, per OA status — and how many more does Unpaywall add?
// How long does one download take?
//
// It runs in phases, each resumable from what the last run left in -dir:
//
//  1. Rebuild the 20 validation libraries (spike 8's seeds, add + one default
//     expansion each). The HTTP cache makes a re-run nearly free.
//  2. Read every fetched work's OA locations from OpenAlex, 100 per request
//     (1 credit each), and classify it: no open copy; open but no PDF link;
//     a PDF link.
//  3. Download a sample — -per-field works with a PDF link from each field —
//     the way ADR-013 says fil will: no cookies, no credentials, a plain
//     User-Agent, one request per second per host, %PDF- or it is not a PDF,
//     and an HTML answer is counted, never read for links. Successful PDFs are
//     kept in -dir/pdfs for spikes 10 and 11.
//  4. If FIL_SPIKE_UNPAYWALL_EMAIL is set, ask Unpaywall about a sample of
//     works OpenAlex gave no PDF link for, and about every download that
//     failed, and try the PDF links it offers. The address is read from the
//     environment only: it is never written to -dir, the cache or the report.
//
// Usage:
//
//	go run ./spikes/9-pdf-availability -dir <scratch> > spike9.md
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/httpx"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/sources/openalex"
	"github.com/codevector-2003/filiation/internal/store"
)

type seed struct{ field, id string }

// The seeds of spike 8, the M1 validation run.
var seeds = []seed{
	{"Medicine", "10.1056/NEJMoa2034577"}, {"Medicine", "10.1016/S0140-6736(20)30183-5"},
	{"Medicine", "10.1001/jama.2020.1585"}, {"Medicine", "10.1371/journal.pmed.0020124"},
	{"Physics", "10.1103/PhysRevLett.116.061102"}, {"Physics", "10.1103/PhysRev.47.777"},
	{"Physics", "10.1038/s41586-019-1666-5"}, {"Physics", "10.1103/RevModPhys.81.109"},
	{"Computer Science", "arXiv:1706.03762"}, {"Computer Science", "10.1109/CVPR.2016.90"},
	{"Computer Science", "10.1038/nature14539"}, {"Computer Science", "10.1145/3292500.3330701"},
	{"Social Sciences", "10.7717/peerj.4375"}, {"Social Sciences", "10.1086/225469"},
	{"Social Sciences", "10.1126/science.aac4716"}, {"Social Sciences", "10.1257/aer.91.5.1369"},
	{"Arts and Humanities", "10.2307/412243"}, {"Arts and Humanities", "10.1177/030631289019003001"},
	{"Arts and Humanities", "10.1098/rspb.1979.0086"}, {"Arts and Humanities", "W2266294403"},
}

var fields = []string{"Medicine", "Physics", "Computer Science", "Social Sciences", "Arts and Humanities"}

const (
	userAgent = "filiation-spike/9 (+https://github.com/codevector-2003/filiation)"
	maxPDF    = 100 << 20
)

// Work is one fetched work and what OpenAlex says about its open copies.
type Work struct {
	ID       string     `json:"id"`
	Field    string     `json:"field"`
	DOI      string     `json:"doi,omitempty"`
	Status   string     `json:"oa_status"`
	Class    string     `json:"class"` // closed | oa-no-pdf | pdf-link | omitted
	PDFURLs  []string   `json:"pdf_urls,omitempty"`
	Versions []string   `json:"versions,omitempty"` // version of each PDF URL
	Landing  int        `json:"landing_pages"`
	Tries    []Attempt  `json:"tries,omitempty"`
	UPW      *Unpaywall `json:"unpaywall,omitempty"`
}

// Attempt is one download.
type Attempt struct {
	URL     string `json:"url"`
	Host    string `json:"host"`
	Source  string `json:"source"` // openalex | unpaywall
	Outcome string `json:"outcome"`
	Status  int    `json:"status,omitempty"`
	Bytes   int64  `json:"bytes,omitempty"`
	Millis  int64  `json:"ms"`
}

// Unpaywall is what Unpaywall said about one DOI.
type Unpaywall struct {
	Asked   bool     `json:"asked"`
	IsOA    bool     `json:"is_oa"`
	Status  string   `json:"oa_status,omitempty"`
	PDFURLs []string `json:"pdf_urls,omitempty"`
	Err     string   `json:"error,omitempty"`
}

func main() {
	dir := flag.String("dir", "", "scratch directory for libraries, cache, PDFs and results (required)")
	perField := flag.Int("per-field", 40, "works with a PDF link to try downloading, per field")
	upwPerField := flag.Int("unpaywall-per-field", 40, "works without an OpenAlex PDF link to ask Unpaywall about, per field")
	flag.Parse()
	if *dir == "" {
		fatal(errors.New("-dir is required"))
	}
	ctx := context.Background()
	cache := filepath.Join(*dir, "cache")

	// A previous run's works and attempts are kept, so a re-run resumes.
	var works []*Work
	if b, err := os.ReadFile(filepath.Join(*dir, "works.json")); err == nil {
		if err := json.Unmarshal(b, &works); err != nil {
			fatal(err)
		}
	} else {
		works = loadOrBuild(ctx, *dir, cache)
		locate(ctx, cache, works)
		save(*dir, works)
	}

	rng := rand.New(rand.NewSource(9))
	sample := sampleBy(works, rng, *perField, func(w *Work) bool { return w.Class == "pdf-link" })
	download(ctx, *dir, sample)
	save(*dir, works)

	if email := strings.TrimSpace(os.Getenv("FIL_SPIKE_UNPAYWALL_EMAIL")); email != "" {
		ask := sampleBy(works, rng, *upwPerField, func(w *Work) bool {
			return w.DOI != "" && (w.Class == "closed" || w.Class == "oa-no-pdf")
		})
		for _, w := range sample {
			if w.DOI != "" && !downloaded(w) {
				ask = append(ask, w)
			}
		}
		unpaywall(ctx, *dir, email, ask)
		save(*dir, works)
	}
	report(works, sample)
}

// loadOrBuild returns the fetched works of the 20 libraries, building any
// library that is not in dir yet.
func loadOrBuild(ctx context.Context, dir, cache string) []*Work {
	seen := map[string]map[string]bool{}
	var works []*Work
	for i, s := range seeds {
		db := filepath.Join(dir, "libs", fmt.Sprintf("lib%02d", i), "library.db")
		if _, err := os.Stat(db); err != nil {
			fmt.Fprintf(os.Stderr, "building library %d (%s) ... ", i+1, s.id)
			if err := build(ctx, db, cache, s.id); err != nil {
				fmt.Fprintf(os.Stderr, "failed: %v\n", err)
				continue
			}
			fmt.Fprintln(os.Stderr, "done")
		}
		st, err := store.Open(ctx, db)
		if err != nil {
			fatal(err)
		}
		titled, err := st.TitledWorks(ctx)
		st.Close()
		if err != nil {
			fatal(err)
		}
		if seen[s.field] == nil {
			seen[s.field] = map[string]bool{}
		}
		for _, w := range titled {
			if w.OpenAlexID == "" || seen[s.field][w.OpenAlexID] {
				continue
			}
			seen[s.field][w.OpenAlexID] = true
			doi := ""
			if w.DOI != nil {
				doi = *w.DOI
			}
			works = append(works, &Work{ID: w.OpenAlexID, Field: s.field, DOI: doi})
		}
	}
	return works
}

func build(ctx context.Context, db, cache, id string) error {
	cfg := &config.Config{DBPath: db, MaxNodes: config.DefaultMaxNodes, MaxDepth: config.DefaultMaxDepth,
		MaxRefsPerWork: config.DefaultMaxRefsPerWork}
	lib, err := library.Open(ctx, cfg, library.Options{CacheDir: cache})
	if err != nil {
		return err
	}
	defer lib.Close()
	if _, err := lib.Add(ctx, id, library.AddOptions{}); err != nil {
		return err
	}
	_, err = lib.Expand(ctx, library.ExpandOptions{})
	return err
}

type location struct {
	IsOA       bool    `json:"is_oa"`
	PDFURL     *string `json:"pdf_url"`
	LandingURL *string `json:"landing_page_url"`
	Version    *string `json:"version"`
}

// locate reads OA locations for every work, 100 IDs per request.
func locate(ctx context.Context, cache string, works []*Work) {
	dc, err := httpx.NewDirCache(cache)
	if err != nil {
		fatal(err)
	}
	h, err := httpx.New(openalex.HTTPOptions("", dc))
	if err != nil {
		fatal(err)
	}
	byID := map[string][]*Work{}
	var ids []string
	for _, w := range works {
		if byID[w.ID] == nil {
			ids = append(ids, w.ID)
		}
		byID[w.ID] = append(byID[w.ID], w)
	}
	sort.Strings(ids)
	for start := 0; start < len(ids); start += 100 {
		batch := ids[start:min(start+100, len(ids))]
		q := url.Values{
			"filter":   {"openalex_id:" + strings.Join(batch, "|")},
			"per_page": {"100"},
			"select":   {"id,open_access,best_oa_location,locations"},
		}
		body, err := h.Get(ctx, "https://api.openalex.org/works?"+q.Encode())
		if err != nil {
			fatal(fmt.Errorf("locations batch at %d: %w", start, err))
		}
		var page struct {
			Results []struct {
				ID         string `json:"id"`
				OpenAccess struct {
					IsOA     bool   `json:"is_oa"`
					OAStatus string `json:"oa_status"`
				} `json:"open_access"`
				Best      *location  `json:"best_oa_location"`
				Locations []location `json:"locations"`
			} `json:"results"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			fatal(err)
		}
		for _, r := range page.Results {
			id := r.ID[strings.LastIndex(r.ID, "/")+1:]
			var pdfs, versions []string
			landing := 0
			add := func(l location) {
				if !l.IsOA {
					return
				}
				if l.PDFURL != nil && *l.PDFURL != "" {
					for _, p := range pdfs {
						if p == *l.PDFURL {
							return
						}
					}
					pdfs = append(pdfs, *l.PDFURL)
					versions = append(versions, deref(l.Version))
				} else if l.LandingURL != nil {
					landing++
				}
			}
			if r.Best != nil {
				add(*r.Best)
			}
			for _, l := range r.Locations {
				add(l)
			}
			class := "closed"
			switch {
			case len(pdfs) > 0:
				class = "pdf-link"
			case r.OpenAccess.IsOA:
				class = "oa-no-pdf"
			}
			for _, w := range byID[id] {
				w.Status, w.Class, w.PDFURLs, w.Versions, w.Landing = r.OpenAccess.OAStatus, class, pdfs, versions, landing
			}
		}
		fmt.Fprintf(os.Stderr, "\rlocations: %d of %d", min(start+100, len(ids)), len(ids))
	}
	fmt.Fprintln(os.Stderr)
	for _, w := range works {
		if w.Class == "" {
			w.Class = "omitted" // a batch filter silently drops some IDs (spike 1)
		}
	}
}

// sampleBy picks up to n works per field that match keep, at random but the
// same on every run.
func sampleBy(works []*Work, rng *rand.Rand, n int, keep func(*Work) bool) []*Work {
	var out []*Work
	for _, f := range fields {
		var pool []*Work
		for _, w := range works {
			if w.Field == f && keep(w) {
				pool = append(pool, w)
			}
		}
		rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
		out = append(out, pool[:min(n, len(pool))]...)
	}
	return out
}

// downloader fetches PDFs the way ADR-013 says fil will.
type downloader struct {
	client *http.Client
	dir    string
	mu     sync.Mutex
	hosts  map[string]*rate.Limiter
}

func newDownloader(dir string) *downloader {
	return &downloader{
		// No Jar: no cookies. Redirects are followed, as a browser would.
		client: &http.Client{Timeout: 90 * time.Second},
		dir:    dir,
		hosts:  map[string]*rate.Limiter{},
	}
}

func (d *downloader) limiter(host string) *rate.Limiter {
	d.mu.Lock()
	defer d.mu.Unlock()
	l, ok := d.hosts[host]
	if !ok {
		l = rate.NewLimiter(rate.Every(time.Second), 1)
		d.hosts[host] = l
	}
	return l
}

// get downloads one URL to path if it is a PDF.
func (d *downloader) get(ctx context.Context, raw, source, path string) (a Attempt) {
	a = Attempt{URL: raw, Source: source}
	u, err := url.Parse(raw)
	if err != nil {
		a.Outcome = "bad-url"
		return a
	}
	a.Host = strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if err := d.limiter(a.Host).Wait(ctx); err != nil {
		a.Outcome = "cancelled"
		return a
	}
	start := time.Now()
	defer func() { a.Millis = time.Since(start).Milliseconds() }()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		a.Outcome = "bad-url"
		return a
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/pdf,*/*;q=0.5")
	resp, err := d.client.Do(req)
	if err != nil {
		a.Outcome = "network: " + classifyNetErr(err)
		return a
	}
	defer resp.Body.Close()
	a.Status = resp.StatusCode
	if resp.StatusCode != http.StatusOK {
		a.Outcome = fmt.Sprintf("http %d", resp.StatusCode)
		return a
	}
	head := make([]byte, 1024)
	n, _ := io.ReadFull(resp.Body, head)
	head = head[:n]
	if !strings.Contains(string(head), "%PDF-") {
		ct := resp.Header.Get("Content-Type")
		switch {
		case strings.Contains(strings.ToLower(string(head)), "<html") || strings.Contains(ct, "html"):
			a.Outcome = "not a PDF: html"
		default:
			a.Outcome = "not a PDF: " + strings.SplitN(ct, ";", 2)[0]
		}
		return a
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fatal(err)
	}
	f, err := os.Create(path + ".part")
	if err != nil {
		fatal(err)
	}
	f.Write(head)
	rest, err := io.Copy(f, io.LimitReader(resp.Body, maxPDF))
	f.Close()
	a.Bytes = int64(len(head)) + rest
	switch {
	case err != nil:
		os.Remove(path + ".part")
		a.Outcome = "network: " + classifyNetErr(err)
	case a.Bytes > maxPDF:
		os.Remove(path + ".part")
		a.Outcome = "too large"
	default:
		os.Rename(path+".part", path)
		a.Outcome = "ok"
	}
	return a
}

func classifyNetErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "Client.Timeout") || strings.Contains(s, "deadline"):
		return "timeout"
	case strings.Contains(s, "certificate"), strings.Contains(s, "tls"):
		return "tls"
	case strings.Contains(s, "no such host"):
		return "dns"
	case strings.Contains(s, "stopped after 10 redirects"):
		return "redirect loop"
	}
	return "error"
}

// download tries each sampled work's PDF links in order until one works, at
// most three, four works at a time; hosts are rate limited separately.
func download(ctx context.Context, dir string, sample []*Work) {
	d := newDownloader(dir)
	jobs := make(chan *Work)
	var wg sync.WaitGroup
	var done int
	var mu sync.Mutex
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range jobs {
				if downloaded(w) || len(w.Tries) > 0 {
					continue
				}
				path := pdfPath(dir, w)
				for i, u := range w.PDFURLs {
					if i == 3 {
						break
					}
					a := d.get(ctx, u, "openalex", path)
					w.Tries = append(w.Tries, a)
					if a.Outcome == "ok" {
						break
					}
				}
				mu.Lock()
				done++
				fmt.Fprintf(os.Stderr, "\rdownloads: %d of %d", done, len(sample))
				mu.Unlock()
			}
		}()
	}
	for _, w := range sample {
		jobs <- w
	}
	close(jobs)
	wg.Wait()
	fmt.Fprintln(os.Stderr)
}

func pdfPath(dir string, w *Work) string {
	return filepath.Join(dir, "pdfs", strings.ReplaceAll(w.Field, " ", "-"), w.ID+".pdf")
}

func downloaded(w *Work) bool {
	for _, a := range w.Tries {
		if a.Outcome == "ok" {
			return true
		}
	}
	return false
}

// unpaywall asks Unpaywall about each work's DOI, uncached, and tries the PDF
// links it offers that OpenAlex did not.
func unpaywall(ctx context.Context, dir, email string, ask []*Work) {
	// No cache: the request URL carries the address, and nothing holding it
	// may be written to disk.
	h, err := httpx.New(httpx.Options{RatePerSecond: 2, Contact: email, ContactParam: "email", UserAgent: "filiation-spike/9"})
	if err != nil {
		fatal(err)
	}
	d := newDownloader(dir)
	for i, w := range ask {
		fmt.Fprintf(os.Stderr, "\runpaywall: %d of %d", i+1, len(ask))
		if w.UPW != nil {
			continue
		}
		u := &Unpaywall{Asked: true}
		w.UPW = u
		body, err := h.Get(ctx, "https://api.unpaywall.org/v2/"+url.PathEscape(w.DOI))
		if err != nil {
			u.Err = fmt.Sprintf("http %d", httpx.StatusCode(err))
			continue
		}
		var r struct {
			IsOA        bool   `json:"is_oa"`
			OAStatus    string `json:"oa_status"`
			OALocations []struct {
				URLForPDF *string `json:"url_for_pdf"`
			} `json:"oa_locations"`
		}
		if err := json.Unmarshal(body, &r); err != nil {
			u.Err = "bad json"
			continue
		}
		u.IsOA, u.Status = r.IsOA, r.OAStatus
		for _, l := range r.OALocations {
			if l.URLForPDF != nil && *l.URLForPDF != "" {
				u.PDFURLs = append(u.PDFURLs, *l.URLForPDF)
			}
		}
		if downloaded(w) {
			continue
		}
		tried := map[string]bool{}
		for _, a := range w.Tries {
			tried[a.URL] = true
		}
		n := 0
		for _, p := range u.PDFURLs {
			if tried[p] || n == 3 {
				continue
			}
			n++
			a := d.get(ctx, p, "unpaywall", pdfPath(dir, w))
			w.Tries = append(w.Tries, a)
			if a.Outcome == "ok" {
				break
			}
		}
	}
	fmt.Fprintln(os.Stderr)
}

func save(dir string, works []*Work) {
	b, err := json.MarshalIndent(works, "", " ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "works.json"), b, 0o644); err != nil {
		fatal(err)
	}
}

func report(works []*Work, sample []*Work) {
	fmt.Println("## Spike 9 — PDF availability")
	fmt.Println()
	fmt.Printf("`go run ./spikes/9-pdf-availability`. %d fetched works across the 20 validation libraries "+
		"(unique within each field).\n\n", len(works))

	fmt.Println("### What OpenAlex offers, by field")
	fmt.Println()
	fmt.Println("| Field | Works | No open copy | Open, no PDF link | PDF link | Omitted by batch |")
	fmt.Println("| --- | --- | --- | --- | --- | --- |")
	total := map[string]int{}
	for _, f := range fields {
		c := map[string]int{}
		n := 0
		for _, w := range works {
			if w.Field == f {
				c[w.Class]++
				total[w.Class]++
				n++
			}
		}
		fmt.Printf("| %s | %d | %s | %s | %s | %d |\n", f, n, pct(c["closed"], n), pct(c["oa-no-pdf"], n), pct(c["pdf-link"], n), c["omitted"])
	}
	fmt.Printf("| **All** | %d | %s | %s | %s | %d |\n\n", len(works),
		pct(total["closed"], len(works)), pct(total["oa-no-pdf"], len(works)), pct(total["pdf-link"], len(works)), total["omitted"])

	fmt.Println("### By OA status")
	fmt.Println()
	fmt.Println("| OA status | Works | PDF link |")
	fmt.Println("| --- | --- | --- |")
	byStatus := map[string][2]int{}
	for _, w := range works {
		s := byStatus[w.Status]
		s[0]++
		if w.Class == "pdf-link" {
			s[1]++
		}
		byStatus[w.Status] = s
	}
	for _, st := range []string{"diamond", "gold", "green", "hybrid", "bronze", "closed", ""} {
		if s, ok := byStatus[st]; ok {
			name := st
			if name == "" {
				name = "(none)"
			}
			fmt.Printf("| %s | %d | %s |\n", name, s[0], pct(s[1], s[0]))
		}
	}
	fmt.Println()

	fmt.Println("### Downloads from OpenAlex PDF links")
	fmt.Println()
	fmt.Println("| Field | Tried | Got a PDF | Median time | Median size |")
	fmt.Println("| --- | --- | --- | --- | --- |")
	outcomes := map[string]int{}
	hosts := map[string][2]int{}
	var allMS []int64
	for _, f := range fields {
		var tried, ok int
		var ms, sizes []int64
		for _, w := range sample {
			if w.Field != f {
				continue
			}
			tried++
			for _, a := range w.Tries {
				if a.Source != "openalex" {
					continue
				}
				outcomes[a.Outcome]++
				h := hosts[a.Host]
				h[0]++
				if a.Outcome == "ok" {
					h[1]++
					ok++
					ms = append(ms, a.Millis)
					sizes = append(sizes, a.Bytes)
				}
				hosts[a.Host] = h
			}
		}
		allMS = append(allMS, ms...)
		fmt.Printf("| %s | %d | %s | %s | %s |\n", f, tried, pct(ok, tried), dur(median(ms)), mb(median(sizes)))
	}
	fmt.Printf("\nAcross all downloads: median %s, 90th percentile %s.\n\n", dur(median(allMS)), dur(percentile(allMS, 90)))

	fmt.Println("Every attempt, by outcome:")
	fmt.Println()
	for _, kv := range sorted(outcomes) {
		fmt.Printf("- %s: %d\n", kv.k, kv.v)
	}
	fmt.Println()
	fmt.Println("Hosts with the most attempts (attempts, PDFs):")
	fmt.Println()
	type hostRow struct {
		host     string
		tried, n int
	}
	var hs []hostRow
	for h, v := range hosts {
		hs = append(hs, hostRow{h, v[0], v[1]})
	}
	sort.Slice(hs, func(i, j int) bool {
		return hs[i].tried > hs[j].tried || hs[i].tried == hs[j].tried && hs[i].host < hs[j].host
	})
	for _, h := range hs[:min(15, len(hs))] {
		fmt.Printf("- %s: %d, %d\n", h.host, h.tried, h.n)
	}
	fmt.Println()

	var asked, upwPDF, upwGot, failedBefore, rescued int
	for _, w := range works {
		if w.UPW == nil || !w.UPW.Asked || w.UPW.Err != "" {
			continue
		}
		asked++
		if len(w.UPW.PDFURLs) > 0 {
			upwPDF++
		}
		openalexOK, upwOK := false, false
		for _, a := range w.Tries {
			if a.Outcome == "ok" {
				if a.Source == "openalex" {
					openalexOK = true
				} else {
					upwOK = true
				}
			}
		}
		if w.Class == "pdf-link" && !openalexOK {
			failedBefore++
			if upwOK {
				rescued++
			}
		}
		if upwOK {
			upwGot++
		}
	}
	if asked > 0 {
		fmt.Println("### Unpaywall")
		fmt.Println()
		fmt.Printf("Asked about %d DOIs: works OpenAlex gave no PDF link for, and failed downloads.\n\n", asked)
		fmt.Printf("- Unpaywall offered a PDF link for %s.\n", pct(upwPDF, asked))
		fmt.Printf("- A PDF was actually downloaded from an Unpaywall link for %d of them.\n", upwGot)
		fmt.Printf("- Of %d failed OpenAlex downloads, Unpaywall rescued %d.\n\n", failedBefore, rescued)
	}
}

type kv struct {
	k string
	v int
}

func sorted(m map[string]int) []kv {
	var out []kv
	for k, v := range m {
		out = append(out, kv{k, v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].v > out[j].v || out[i].v == out[j].v && out[i].k < out[j].k })
	return out
}

func pct(n, of int) string {
	if of == 0 {
		return "—"
	}
	return fmt.Sprintf("%d (%.0f%%)", n, 100*float64(n)/float64(of))
}

func median(xs []int64) int64 { return percentile(xs, 50) }

func percentile(xs []int64, p int) int64 {
	if len(xs) == 0 {
		return 0
	}
	s := append([]int64(nil), xs...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[min(len(s)-1, len(s)*p/100)]
}

func dur(ms int64) string {
	return (time.Duration(ms) * time.Millisecond).Round(100 * time.Millisecond).String()
}

func mb(b int64) string { return fmt.Sprintf("%.1f MB", float64(b)/(1<<20)) }

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "spike 9:", err)
	os.Exit(1)
}
