package ui

import (
	"net/http"
	"strconv"
	"testing"
)

func TestSavedViewsAndSearchWithin(t *testing.T) {
	t.Parallel()
	h, _, _ := openTest(t)

	if w := post(t, h, "/api/views", `{"name":"Everything","filters":{}}`); w.Code != http.StatusBadRequest {
		t.Errorf("a view of every paper = %d", w.Code)
	}
	w := post(t, h, "/api/views", `{"name":"Recent","filters":{"yearFrom":2025}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("save view = %d %s", w.Code, w.Body)
	}
	id := decode[struct {
		ID int64 `json:"id"`
	}](t, w).ID
	lib := decode[libraryJSON](t, ask(t, h, "GET", "/api/library"))
	if len(lib.Views) != 1 || lib.Views[0].Name != "Recent" || lib.Views[0].Papers != 1 {
		t.Fatalf("views = %+v", lib.Views)
	}

	type searched struct {
		Hits     []hitJSON `json:"hits"`
		Within   bool      `json:"within"`
		Papers   int       `json:"papers"`
		Readable int       `json:"readable"`
	}
	in := decode[searched](t, ask(t, h, "GET", "/api/search?q=hedgerow&view="+strconv.FormatInt(id, 10)))
	if !in.Within || in.Papers != 1 || in.Readable != 1 || len(in.Hits) != 1 {
		t.Fatalf("searching the view = %+v", in)
	}
	// A view's years can be narrowed further from Search.
	if got := decode[searched](t, ask(t, h, "GET", "/api/search?q=hedgerow&view="+strconv.FormatInt(id, 10)+"&year_from=2030")); got.Papers != 0 || len(got.Hits) != 0 {
		t.Fatalf("narrowed past the view = %+v", got)
	}
	if got := decode[searched](t, ask(t, h, "GET", "/api/search?q=hedgerow")); got.Within || len(got.Hits) != 1 {
		t.Fatalf("the whole library = %+v", got)
	}
	if w := ask(t, h, "GET", "/api/search?q=hedgerow&view=999"); w.Code != http.StatusNotFound {
		t.Errorf("a view not saved = %d", w.Code)
	}

	if w := post(t, h, "/api/views/"+strconv.FormatInt(id, 10), `{"name":"Lately"}`); w.Code != http.StatusOK {
		t.Fatalf("rename = %d %s", w.Code, w.Body)
	}
	if w := post(t, h, "/api/views/"+strconv.FormatInt(id, 10)+"/delete", `{}`); w.Code != http.StatusOK {
		t.Fatalf("delete = %d %s", w.Code, w.Body)
	}
	if lib := decode[libraryJSON](t, ask(t, h, "GET", "/api/library")); len(lib.Views) != 0 {
		t.Fatalf("after delete = %+v", lib.Views)
	}
}
