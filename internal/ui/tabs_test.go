package ui

import (
	"net/http"
	"strings"
	"testing"

	"github.com/codevector-2003/filiation/internal/library"
)

func TestReaderTabsKept(t *testing.T) {
	t.Parallel()
	h, id, _ := openTest(t)

	if got := decode[library.ReaderTabs](t, ask(t, h, "GET", "/api/tabs")); got.Tabs == nil || len(got.Tabs) != 0 || got.Active != "" {
		t.Fatalf("before any were kept = %+v; want an empty list", got)
	}
	body := `{"tabs":[{"id":"` + strings.ToLower(id) + `","page":3,"zoom":1.2,"side":"notes"}],"active":"` + id + `","screen":"papers"}`
	if w := post(t, h, "/api/tabs", body); w.Code != http.StatusNoContent {
		t.Fatalf("save = %d %s", w.Code, w.Body)
	}
	got := decode[library.ReaderTabs](t, ask(t, h, "GET", "/api/tabs"))
	if len(got.Tabs) != 1 || got.Tabs[0] != (library.ReaderTab{ID: id, Page: 3, Zoom: 1.2, Side: "notes"}) || got.Active != id || got.Screen != "papers" {
		t.Fatalf("kept = %+v", got)
	}

	if w := post(t, h, "/api/tabs", `{"tabs":[{"id":"not a paper"}]}`); w.Code != http.StatusBadRequest {
		t.Errorf("a tab naming no paper = %d", w.Code)
	}
	if w := post(t, h, "/api/tabs", `{"tabs":[`+strings.Repeat(`{"id":"`+id+`","find":"`+strings.Repeat("x", 200)+`"},`, 400)+`{}]}`); w.Code != http.StatusBadRequest {
		t.Errorf("a body far too big = %d", w.Code)
	}
}
