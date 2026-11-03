package zotero

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"slices"
	"testing"
)

// fake serves a small library the way Zotero's local API does.
func fake(t *testing.T) *Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/users/0/collections", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("%s %s: the client only reads", r.Method, r.URL)
		}
		w.Write([]byte(`[
 {"key":"THESIS01","data":{"key":"THESIS01","name":"Thesis","parentCollection":false}},
 {"key":"CHAPTER2","data":{"key":"CHAPTER2","name":"Chapter 2","parentCollection":"THESIS01"}}]`))
	})
	mux.HandleFunc("GET /api/users/0/items", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
 {"key":"ART00001","data":{"key":"ART00001","version":7,"itemType":"journalArticle","title":"The state of OA",
   "date":"2018-02-13","DOI":"https://doi.org/10.7717/peerj.4375","collections":["CHAPTER2"],
   "creators":[{"creatorType":"author","firstName":"Heather","lastName":"Piwowar"}],
   "tags":[{"tag":"key paper"},{"tag":"Open Access","type":1}]}},
 {"key":"PDF00001","data":{"key":"PDF00001","itemType":"attachment","parentItem":"ART00001","linkMode":"imported_file","contentType":"application/pdf","title":"Full Text PDF"}},
 {"key":"WEB00001","data":{"key":"WEB00001","itemType":"attachment","parentItem":"ART00001","linkMode":"imported_url","contentType":"text/html","title":"Snapshot"}},
 {"key":"NOTE0001","data":{"key":"NOTE0001","itemType":"note","parentItem":"ART00001","note":"<p>hi</p>"}},
 {"key":"PRE00001","data":{"key":"PRE00001","itemType":"preprint","title":"Attention","date":"June 2017","archiveID":"arXiv:1706.03762","collections":[]}},
 {"key":"BOOK0001","data":{"key":"BOOK0001","itemType":"book","title":"Structure","date":"1962","extra":"PMID: 12345\nOriginal Date: 1962","creators":[{"name":"Library services"}]}},
 {"key":"GONE0001","data":{"key":"GONE0001","itemType":"book","title":"Thrown away","deleted":1}}]`))
	})
	mux.HandleFunc("GET /api/users/0/items/PDF00001/file/view/url", func(w http.ResponseWriter, r *http.Request) {
		if runtime.GOOS == "windows" {
			w.Write([]byte("file:///C:/Users/me/Zotero/storage/PDF00001/Piwowar%202018.pdf\n"))
			return
		}
		w.Write([]byte("file:///home/me/Zotero/storage/PDF00001/Piwowar%202018.pdf\n"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return New(srv.URL+"/api", nil)
}

func TestReadsALibrary(t *testing.T) {
	t.Parallel()
	c := fake(t)
	ctx := t.Context()
	if err := c.Check(ctx); err != nil {
		t.Fatal(err)
	}
	cols, err := c.Collections(ctx)
	if err != nil || len(cols) != 2 || cols[0].Parent != "" || cols[1].Parent != "THESIS01" {
		t.Fatalf("Collections = %+v, %v", cols, err)
	}
	items, err := c.Items(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %+v; three, without the note, attachments and the trashed book", items)
	}
	a := items[0]
	if a.DOI != "10.7717/peerj.4375" || a.Year != 2018 || !slices.Equal(a.Creators, []string{"Heather Piwowar"}) ||
		!slices.Equal(a.Tags, []string{"key paper"}) || len(a.PDFs) != 1 || a.PDFs[0].Key != "PDF00001" {
		t.Errorf("article = %+v", a)
	}
	if items[1].ArXiv != "1706.03762" || items[1].Year != 2017 {
		t.Errorf("preprint = %+v", items[1])
	}
	if items[2].PMID != "12345" || items[2].Creators[0] != "Library services" || items[2].DOI != "" {
		t.Errorf("book = %+v", items[2])
	}
	path, err := c.FilePath(ctx, "PDF00001")
	want := "/home/me/Zotero/storage/PDF00001/Piwowar 2018.pdf"
	if runtime.GOOS == "windows" {
		want = `C:\Users\me\Zotero\storage\PDF00001\Piwowar 2018.pdf`
	}
	if err != nil || path != want {
		t.Errorf("FilePath = %q, %v; want %q", path, err, want)
	}
}

func TestSaysWhyItCannotRead(t *testing.T) {
	t.Parallel()
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer forbidden.Close()
	if err := New(forbidden.URL+"/api", nil).Check(t.Context()); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("Zotero refusing = %v", err)
	}
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	if err := New(gone.URL+"/api", nil).Check(t.Context()); !errors.Is(err, ErrNotRunning) {
		t.Errorf("no Zotero = %v", err)
	}
}

func TestIdentifiers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		doi, link, extra, archive string
		want                      [3]string
	}{
		{"10.1000/xyz123.", "", "", "", [3]string{"10.1000/xyz123", "", ""}},
		{"", "https://doi.org/10.1000/abc", "", "", [3]string{"10.1000/abc", "", ""}},
		{"", "https://arxiv.org/abs/2101.00001v2", "", "", [3]string{"", "2101.00001", ""}},
		{"", "", "DOI: 10.5555/extra\nPMID: 99", "", [3]string{"10.5555/extra", "", "99"}},
		{"", "https://example.org/paper", "", "", [3]string{"", "", ""}},
	}
	for _, c := range cases {
		d, a, p := identifiers(c.doi, c.link, c.extra, c.archive)
		if got := [3]string{d, a, p}; got != c.want {
			t.Errorf("identifiers(%q, %q, %q) = %v, want %v", c.doi, c.link, c.extra, got, c.want)
		}
	}
}
