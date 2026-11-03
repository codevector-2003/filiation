package ui

import (
	"net/http"
	"slices"
	"strconv"
	"testing"
)

func TestOrganisingThePapers(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)
	made := func(path, body string) int64 {
		t.Helper()
		w := post(t, h, path, body)
		if w.Code != http.StatusOK {
			t.Fatalf("POST %s = %d %s", path, w.Code, w.Body)
		}
		return decode[map[string]int64](t, w)["id"]
	}
	thesis := made("/api/collections", `{"name":"Thesis"}`)
	ch2 := made("/api/collections", `{"name":" Chapter 2 ","parent":`+strconv.FormatInt(thesis, 10)+`}`)
	key := made("/api/labels", `{"name":"Key paper","colour":"blue"}`)
	papers := `{"papers":["` + id + `"],"on":true}`
	for _, path := range []string{"/api/collections/" + strconv.FormatInt(ch2, 10) + "/papers",
		"/api/labels/" + strconv.FormatInt(key, 10) + "/papers", "/api/favourites"} {
		if w := post(t, h, path, papers); w.Code != http.StatusNoContent {
			t.Fatalf("POST %s = %d %s", path, w.Code, w.Body)
		}
	}

	lib := decode[libraryJSON](t, ask(t, h, "GET", "/api/library"))
	if len(lib.Collections) != 2 || lib.Collections[1].Name != "Chapter 2" || lib.Collections[1].Parent != thesis ||
		lib.Collections[0].Works != 1 || lib.Favourites != 1 || len(lib.Labels) != 1 || lib.Labels[0].Works != 1 ||
		len(lib.Colours) == 0 {
		t.Errorf("library = %+v", lib)
	}
	type list struct {
		Papers []rowJSON `json:"papers"`
		Total  int       `json:"total"`
	}
	for _, q := range []string{"collection=" + strconv.FormatInt(thesis, 10), "favourites=1",
		"label=" + strconv.FormatInt(key, 10) + "&favourites=1"} {
		got := decode[list](t, ask(t, h, "GET", "/api/papers?"+q))
		if got.Total != 1 || len(got.Papers) != 1 || !got.Papers[0].Favourite || !slices.Equal(got.Papers[0].Labels, []int64{key}) {
			t.Errorf("papers?%s = %+v", q, got)
		}
	}
	p := decode[paperJSON](t, ask(t, h, "GET", "/api/papers/"+id))
	if !p.Favourite || !slices.Equal(p.Collections, []int64{ch2}) || !slices.Equal(p.Labels, []int64{key}) {
		t.Errorf("paper = %+v", p)
	}

	// Refusals come back in words, without the sentinel's name.
	w := post(t, h, "/api/collections/"+strconv.FormatInt(thesis, 10)+"/move", `{"parent":`+strconv.FormatInt(ch2, 10)+`}`)
	if w.Code != http.StatusBadRequest || decode[map[string]string](t, w)["error"] != "a collection cannot go inside itself" {
		t.Errorf("moving Thesis into Chapter 2 = %d %s", w.Code, w.Body)
	}
	if w := post(t, h, "/api/labels", `{"name":"Odd","colour":"tartan"}`); w.Code != http.StatusBadRequest {
		t.Errorf("a label in no colour we offer = %d", w.Code)
	}
	if w := post(t, h, "/api/collections/999/delete", `{}`); w.Code != http.StatusNotFound {
		t.Errorf("deleting a collection not there = %d", w.Code)
	}

	// Deleting Thesis takes Chapter 2, and the paper stays.
	if w := post(t, h, "/api/collections/"+strconv.FormatInt(thesis, 10)+"/delete", `{}`); w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	lib = decode[libraryJSON](t, ask(t, h, "GET", "/api/library"))
	if len(lib.Collections) != 0 || lib.Works != 1 {
		t.Errorf("after deleting Thesis = %+v", lib)
	}
}
