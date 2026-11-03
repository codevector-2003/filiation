package ui

import (
	"net/http"
	"testing"
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
