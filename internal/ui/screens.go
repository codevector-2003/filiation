package ui

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/codevector-2003/filiation/internal/assistants"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

// The map shows this much: enough to read, however many a paper cites. A
// second step follows the first few only, or it would be a hairball.
const (
	mapFirstStep   = 12
	mapSecondFrom  = 6
	mapSecondStep  = 3
	recentCalls    = 30
	exportFileName = "citation-map.graphml"
)

type snippetPart struct {
	Text string `json:"text"`
	Mark bool   `json:"mark,omitempty"`
}

// snippetParts splits a search snippet into plain and matched stretches.
func snippetParts(s string) []snippetPart {
	s = strings.Join(strings.Fields(s), " ")
	var out []snippetPart
	for s != "" {
		before, rest, found := strings.Cut(s, library.MarkStart)
		if before != "" {
			out = append(out, snippetPart{Text: before})
		}
		if !found {
			break
		}
		marked, after, _ := strings.Cut(rest, library.MarkEnd)
		out = append(out, snippetPart{Text: marked, Mark: true})
		s = after
	}
	return out
}

type hitJSON struct {
	ID         string        `json:"id"`
	Title      string        `json:"title"`
	By         string        `json:"by"`
	Page       int           `json:"page"`
	References bool          `json:"references"`
	Parts      []snippetPart `json:"parts"`
}

func (s *server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	words := strings.TrimSpace(q.Get("q"))
	if words == "" {
		fail(w, http.StatusBadRequest, errors.New("type some words to search for"))
		return
	}
	res, err := s.Library.Search(r.Context(), words, library.SearchOptions{Paper: q.Get("paper"), References: q.Get("references") == "true"})
	if err != nil {
		failFind(w, err)
		return
	}
	hits := make([]hitJSON, len(res.Hits))
	for i, h := range res.Hits {
		hits[i] = hitJSON{ID: h.Work.FilID, Title: h.Work.DisplayTitle(), By: workBy(h.Work), Page: h.Page,
			References: h.Section != "", Parts: snippetParts(h.Snippet)}
	}
	send(w, map[string]any{"hits": hits, "partial": res.Partial, "unread": res.Unread,
		"capped": len(hits) == library.DefaultSearchLimit})
}

func (s *server) page(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		fail(w, http.StatusBadRequest, errors.New("no such page"))
		return
	}
	id := r.PathValue("id")
	p, err := s.Library.Show(r.Context(), id)
	if err != nil {
		failFind(w, err)
		return
	}
	text, err := s.Library.PageText(r.Context(), id, n)
	if err != nil {
		failFind(w, err)
		return
	}
	pages := 0
	if p.Text != nil {
		pages = p.Text.Pages
	}
	send(w, map[string]any{"page": n, "pages": pages, "text": text})
}

type nodeJSON struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	By     string `json:"by"`
	PDF    bool   `json:"pdf"`
	Stub   bool   `json:"stub"`
	Parent string `json:"parent"`
	Step   int    `json:"step"`
}

func (s *server) mapAround(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ctx := r.Context()
	id := q.Get("id")
	if id == "" {
		rows, err := s.Library.List(ctx, library.ListOptions{Limit: 1})
		if err != nil {
			fail(w, http.StatusInternalServerError, err)
			return
		}
		if len(rows) == 0 {
			send(w, map[string]any{"nodes": []nodeJSON{}})
			return
		}
		id = rows[0].FilID
	}
	citedBy := q.Get("direction") == "citedby"
	side := func(n library.Neighbourhood) []model.Work {
		if citedBy {
			return n.CitedBy
		}
		return n.Cites
	}
	n, err := s.Library.Neighbours(ctx, id)
	if err != nil {
		failFind(w, err)
		return
	}
	withPDF, err := s.Library.List(ctx, library.ListOptions{WithPDF: true})
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	hasPDF := make(map[string]bool, len(withPDF))
	for _, l := range withPDF {
		hasPDF[l.FilID] = true
	}
	node := func(wk model.Work, parent string, step int) nodeJSON {
		return nodeJSON{ID: wk.FilID, Title: wk.DisplayTitle(), By: workBy(wk), PDF: hasPDF[wk.FilID], Stub: wk.IsStub(), Parent: parent, Step: step}
	}
	first := side(n)
	nodes := []nodeJSON{}
	shown := map[string]bool{n.Work.FilID: true}
	for _, wk := range first[:min(len(first), mapFirstStep)] {
		nodes = append(nodes, node(wk, n.Work.FilID, 1))
		shown[wk.FilID] = true
	}
	if q.Get("steps") == "2" {
		from := 0
		for _, wk := range first[:min(len(first), mapFirstStep)] {
			if from == mapSecondFrom {
				break
			}
			if wk.IsStub() {
				continue
			}
			from++
			next, err := s.Library.Neighbours(ctx, wk.FilID)
			if err != nil {
				failFind(w, err)
				return
			}
			added := 0
			for _, o := range side(next) {
				if added == mapSecondStep {
					break
				}
				if !shown[o.FilID] {
					shown[o.FilID] = true
					nodes = append(nodes, node(o, wk.FilID, 2))
					added++
				}
			}
		}
	}
	send(w, map[string]any{"center": node(n.Work, "", 0), "nodes": nodes, "total": len(first), "cites": len(n.Cites), "citedBy": len(n.CitedBy)})
}

func (s *server) link(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	contexts, err := s.Library.CitationContexts(r.Context(), q.Get("from"))
	if err != nil {
		failFind(w, err)
		return
	}
	out := []sentenceJSON{}
	for _, m := range contexts[q.Get("to")] {
		out = append(out, sentenceJSON{Page: m.Page, Text: m.Sentence})
	}
	send(w, map[string]any{"sentences": out})
}

func (s *server) path(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ctx := r.Context()
	p, err := s.Library.PathBetween(ctx, q.Get("from"), q.Get("to"), library.PathOptions{})
	if err == nil && !p.Found {
		p, err = s.Library.PathBetween(ctx, q.Get("from"), q.Get("to"), library.PathOptions{AnyDirection: true})
	}
	var many *library.AmbiguousError
	if errors.As(err, &many) {
		fail(w, http.StatusBadRequest, fmt.Errorf("%d papers match that title: type more of it, or its DOI", len(many.Candidates)))
		return
	}
	if err != nil {
		failFind(w, err)
		return
	}
	type stepJSON struct {
		ID    string `json:"id"`
		Title string `json:"title"`
		By    string `json:"by"`
		Cites bool   `json:"cites"` // it cites the next step, not the reverse
	}
	steps := make([]stepJSON, len(p.Works))
	for i, wk := range p.Works {
		steps[i] = stepJSON{ID: wk.FilID, Title: wk.DisplayTitle(), By: workBy(wk)}
		if i < len(p.Cites) {
			steps[i].Cites = p.Cites[i]
		}
	}
	send(w, map[string]any{"found": p.Found, "steps": steps})
}

func (s *server) export(w http.ResponseWriter, r *http.Request) {
	root := s.Library.Folder()
	if root == "" {
		fail(w, http.StatusConflict, errors.New("this library has no folder"))
		return
	}
	dir := filepath.Join(root, "exports")
	tmp, err := os.CreateTemp(dir, exportFileName+".*")
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	defer os.Remove(tmp.Name())
	_, err = s.Library.ExportGraphML(r.Context(), tmp, library.ExportOptions{})
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), filepath.Join(dir, exportFileName))
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	s.open(w, dir)
}

func (s *server) assistantList(w http.ResponseWriter, _ *http.Request) {
	if s.Assistants == nil {
		fail(w, http.StatusNotImplemented, errors.New("connecting assistants needs the desktop app"))
		return
	}
	list := assistants.List(*s.Assistants)
	s.work.mu.Lock()
	for _, a := range list {
		if a.Connected && a.ReadOnly {
			s.readOnly = true
		}
	}
	grow := !s.readOnly
	s.work.mu.Unlock()
	calls := assistants.Recent(s.CallLog, recentCalls)
	if calls == nil {
		calls = []assistants.Call{}
	}
	send(w, map[string]any{"list": list, "grow": grow, "calls": calls,
		"settings": fmt.Sprintf(`"%s": { "command": %q, "args": ["mcp"] }`, assistants.ServerName, s.Assistants.Command)})
}

func (s *server) assistantConnect(w http.ResponseWriter, r *http.Request) {
	if s.Assistants == nil {
		fail(w, http.StatusNotImplemented, errors.New("connecting assistants needs the desktop app"))
		return
	}
	s.work.mu.Lock()
	readOnly := s.readOnly
	s.work.mu.Unlock()
	if err := assistants.Connect(*s.Assistants, r.PathValue("id"), readOnly); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) assistantDisconnect(w http.ResponseWriter, r *http.Request) {
	if s.Assistants == nil {
		fail(w, http.StatusNotImplemented, errors.New("connecting assistants needs the desktop app"))
		return
	}
	if err := assistants.Disconnect(*s.Assistants, r.PathValue("id")); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// assistantPermission sets whether assistants may grow the library, for every
// one already connected as well as those to come.
func (s *server) assistantPermission(w http.ResponseWriter, r *http.Request) {
	if s.Assistants == nil {
		fail(w, http.StatusNotImplemented, errors.New("connecting assistants needs the desktop app"))
		return
	}
	readOnly := r.URL.Query().Get("grow") != "true"
	s.work.mu.Lock()
	s.readOnly = readOnly
	s.work.mu.Unlock()
	for _, a := range assistants.List(*s.Assistants) {
		if !a.Connected || a.ReadOnly == readOnly {
			continue
		}
		if err := assistants.Connect(*s.Assistants, a.ID, readOnly); err != nil {
			fail(w, http.StatusConflict, fmt.Errorf("%s: %w", a.Name, err))
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
