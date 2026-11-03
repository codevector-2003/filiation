package ui

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/codevector-2003/filiation/internal/text/pdftest"
)

func TestALocalDocumentHasNothingToBringIn(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)
	got := decode[surroundingsJSON](t, ask(t, h, "GET", "/api/papers/"+id+"/around"))
	if !got.Local || got.Cites != 0 || got.CitedBy != 0 {
		t.Fatalf("around a local document: %+v", got)
	}
	p := decode[paperJSON](t, ask(t, h, "GET", "/api/papers/"+id))
	if p.Downloadable {
		t.Error("a local document with its PDF offers a download")
	}
	if w := ask(t, h, "GET", "/api/papers/F00000000/around"); w.Code != http.StatusNotFound {
		t.Errorf("around a paper not here = %d", w.Code)
	}
}

func TestPaperActionsCheckWhatTheyAreAsked(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)
	for _, c := range []struct{ path, body string }{
		{"/api/papers/" + id + "/around", `{}`},
		{"/api/papers/" + id + "/around", `{"citing":-1}`},
		{"/api/papers/" + id + "/around", `{"citing":5000}`},
		{"/api/pdfs", `{"ids":[]}`},
		{"/api/pdfs", `not json`},
	} {
		if w := post(t, h, c.path, c.body); w.Code != http.StatusBadRequest {
			t.Errorf("POST %s %s = %d", c.path, c.body, w.Code)
		}
	}
	// A local document is refused inside the job, which reports why.
	if w := post(t, h, "/api/papers/"+id+"/around", `{"references":true}`); w.Code != http.StatusAccepted {
		t.Fatalf("bring in around = %d %s", w.Code, w.Body)
	}
	if j := waitJob(t, h); j.Error == "" {
		t.Errorf("bringing in around a local document: %+v", j)
	}
}

func TestAddingYourPDFToAPaper(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)
	if w := post(t, h, "/api/papers/"+id+"/attach", `{}`); w.Code != http.StatusNotImplemented {
		t.Errorf("choosing a file outside the desktop app = %d", w.Code)
	}
	if w := post(t, h, "/api/papers/"+id+"/attach", `{"file":"notes.docx"}`); w.Code != http.StatusBadRequest {
		t.Errorf("a file that is not a PDF = %d", w.Code)
	}
	other := filepath.Join(t.TempDir(), "my copy.pdf")
	report := pdftest.Make("Quarterly budget report for the finance committee.\n" +
		"Spending for each department rose over the year, with travel and equipment the largest lines.\n" +
		"The committee asked for a revised forecast before the spring meeting, and for savings in printing.")
	if err := os.WriteFile(other, report, 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"file": other})
	got := decode[attachedJSON](t, post(t, h, "/api/papers/"+id+"/attach", string(body)))
	if got.State != "unconfirmed" || got.File == "" {
		t.Fatalf("a file without the paper's title = %+v; want it asked about", got)
	}
	body, _ = json.Marshal(map[string]any{"file": got.File, "force": true})
	if got := decode[attachedJSON](t, post(t, h, "/api/papers/"+id+"/attach", string(body))); got.State != "added" || got.Name != "my copy.pdf" {
		t.Fatalf("added anyway = %+v", got)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("the original was moved: %v", err)
	}
}
