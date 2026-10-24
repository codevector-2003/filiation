package openalex

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/model"
)

// locations_peerj_refs.json was recorded 24 Oct 2026: twenty of the M0 seed's
// references, with select=locationFields. They cover every case — closed works,
// green and diamond works with landing pages only, bronze with no licence, and
// works with several copies.
var peerjRefIDs = []string{
	"W1767272795", "W2016860460", "W2048185449", "W2140880926", "W2089123513",
	"W2115339903", "W2160597895", "W1989318653", "W1560783210", "W2003844967",
	"W2029057325", "W1957687230", "W2231201268", "W1724212071", "W1917633107",
	"W2078310052", "W2020807482", "W2031754690", "W1931854307", "W1944943415",
}

func TestGetLocations(t *testing.T) {
	t.Parallel()
	ids := append(append([]string(nil), peerjRefIDs...), "W99999999999")
	c, f := newTest(t, map[string]route{
		"/works?filter=openalex_id:" + strings.Join(ids, "|"): {status: 200, fixture: "locations_peerj_refs.json"},
		// Omitted by the filter, so confirmed on its own: not in OpenAlex.
		"/works/W99999999999": {status: http.StatusNotFound, fixture: "work_404.json"},
	})

	got, err := c.GetLocations(t.Context(), ids)
	if err != nil {
		t.Fatalf("GetLocations: %v", err)
	}
	if len(got.Links) != 20 || len(got.Missing) != 1 || got.Missing[0] != "W99999999999" {
		t.Fatalf("got %d with links, missing %v", len(got.Links), got.Missing)
	}
	if len(f.calls) != 2 {
		t.Errorf("requests = %v, want one batch and one confirmation", f.calls)
	}

	count := map[model.PDFAvailability]int{}
	for _, l := range got.Links {
		count[l.Availability]++
	}
	want := map[model.PDFAvailability]int{model.PDFLink: 10, model.PDFOpenNoLink: 4, model.PDFNoOpenCopy: 6}
	for k, v := range want {
		if count[k] != v {
			t.Errorf("%s: %d works, want %d (all: %v)", k, count[k], v, count)
		}
	}

	// Published before preprint, whatever order OpenAlex lists them in.
	two := got.Links["W2048185449"]
	if len(two.Links) != 2 || two.Links[0].Version != "published" || two.Links[1].Version != "preprint" {
		t.Errorf("W2048185449 links = %+v; want the published copy first", two.Links)
	}
	if l := two.Links[0]; l.Host != "journals.plos.org" || l.License != "cc-by" || l.LandingURL == "" {
		t.Errorf("first link = %+v", l)
	}
	// Three preprint copies, the best location (arXiv) first; duplicates of
	// the best location in the list are not repeated.
	three := got.Links["W1944943415"].Links
	if len(three) != 3 || three[0].Host != "arxiv.org" {
		t.Errorf("W1944943415 links = %+v", three)
	}
	// Bronze: a PDF link and no licence.
	if b := got.Links["W2078310052"]; b.Availability != model.PDFLink || b.Links[0].License != "" {
		t.Errorf("bronze work = %+v", b)
	}
}

func TestPDFLinksRules(t *testing.T) {
	t.Parallel()
	decode := func(js string) *model.PDFLinks {
		t.Helper()
		var w work
		if err := json.Unmarshal([]byte(js), &w); err != nil {
			t.Fatal(err)
		}
		return w.pdfLinks()
	}

	// No locations in the response: not known, never "no open copy".
	if got := decode(`{"id":"W1","open_access":{"is_oa":true}}`); got != nil {
		t.Errorf("absent locations = %+v, want nil", got)
	}
	// Present and empty: OpenAlex knows of no copy.
	if got := decode(`{"id":"W1","open_access":{"is_oa":false},"locations":[]}`); got == nil || got.Availability != model.PDFNoOpenCopy {
		t.Errorf("empty locations = %+v", got)
	}

	got := decode(`{"id":"W1","open_access":{"is_oa":true},"locations":[
		{"is_oa":false,"pdf_url":"https://publisher.example/paywalled.pdf","version":"publishedVersion"},
		{"is_oa":true,"pdf_url":"ftp://old.example/x.pdf"},
		{"is_oa":true,"pdf_url":"javascript:alert(1)"},
		{"is_oa":true,"pdf_url":"https://www.Repo.example/a.pdf","version":"acceptedVersion"},
		{"is_oa":true,"pdf_url":"https://www.Repo.example/a.pdf","version":"acceptedVersion"},
		{"is_oa":true,"pdf_url":"https://arxiv.org/pdf/1","version":"submittedVersion"},
		{"is_oa":true,"pdf_url":"https://other.example/b.pdf"}
	]}`)
	if got == nil || got.Availability != model.PDFLink {
		t.Fatalf("got %+v", got)
	}
	var hosts []string
	for _, l := range got.Links {
		hosts = append(hosts, l.Host+":"+l.Version)
	}
	// A closed location is never offered, whatever link it carries; only http
	// and https; one copy of each URL; accepted, then preprint, then unstated.
	if strings.Join(hosts, " ") != "repo.example:accepted arxiv.org:preprint other.example:" {
		t.Errorf("links = %v", hosts)
	}
}

// A work hydrated from a response cached before locations were asked for has
// no links, and says so by PDF being nil.
func TestResolveWithoutLocationsLeavesPDFUnknown(t *testing.T) {
	t.Parallel()
	c, _ := newTest(t, map[string]route{
		"/works/doi:10.7717/peerj.4375": {status: 200, fixture: "work_W2741809807.json"},
	})
	h, err := c.Resolve(t.Context(), parse(t, "10.7717/peerj.4375"))
	if err != nil {
		t.Fatal(err)
	}
	if h.PDF != nil {
		t.Errorf("PDF = %+v, want nil: the fixture has no locations", h.PDF)
	}
}
