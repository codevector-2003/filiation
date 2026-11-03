package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

// The Library menu: the library as a whole rather than one paper in it.

type statsJSON struct {
	Name       string `json:"name"`
	Folder     string `json:"folder,omitempty"`
	Works      int    `json:"works"`
	Fetched    int    `json:"fetched"`
	Stubs      int    `json:"stubs"`
	Unresolved int    `json:"unresolved"`
	Seeds      int    `json:"seeds"`
	Citations  int    `json:"citations"`
	DeadEnds   int    `json:"deadEnds"`
	PDFs       int    `json:"pdfs"`
	Searchable int    `json:"searchable"`
	MaxNodes   int    `json:"maxNodes"` // what Expand uses unless told otherwise
	MaxDepth   int    `json:"maxDepth"`
}

func (s *server) stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sum, err := s.Library.Summarise(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	out := statsJSON{Name: s.Name, Folder: s.Library.Folder(), Works: sum.Works, Fetched: sum.Fetched, Stubs: sum.Stubs,
		Unresolved: sum.Unresolved, Seeds: sum.Seeds, Citations: sum.Citations, DeadEnds: sum.DeadEnds}
	out.MaxNodes, out.MaxDepth = s.Library.Budget()
	if out.PDFs, err = s.Library.FetchedOnDisk(ctx); err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	counts, err := s.Library.TextCounts(ctx)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	out.Searchable = counts["ok"]
	send(w, out)
}

func (s *server) libraryFolder(w http.ResponseWriter, _ *http.Request) {
	s.open(w, s.Library.Folder())
}

type scanJSON struct {
	Folders  int `json:"folders"`
	Files    int `json:"files"`
	NewFiles int `json:"newFiles"`
	Moved    int `json:"moved"`
	Missing  int `json:"missing"`
	Loose    int `json:"loose"`
}

func (s *server) scan(w http.ResponseWriter, r *http.Request) {
	rep, err := s.Library.Scan(r.Context())
	if errors.Is(err, library.ErrNoFolder) {
		fail(w, http.StatusConflict, errors.New("this library has no folder"))
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	send(w, scanJSON{Folders: rep.Folders, Files: rep.Files, NewFiles: rep.NewFiles,
		Moved: len(rep.Moved), Missing: len(rep.Missing), Loose: len(rep.Loose)})
}

// expand grows the graph from the whole library, as fil expand does, within
// the budget asked for or the configured one.
func (s *server) expand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MaxNodes int `json:"maxNodes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		fail(w, http.StatusBadRequest, err)
		return
	}
	if req.MaxNodes < 0 || req.MaxNodes > 100000 {
		fail(w, http.StatusBadRequest, errors.New("choose between 1 and 100,000 papers"))
		return
	}
	s.start(w, "expand", func(ctx context.Context, set func(int, int, string)) (string, error) {
		total := req.MaxNodes
		if total == 0 {
			total, _ = s.Library.Budget()
		}
		set(0, total, "Following citations")
		res, err := s.Library.Expand(ctx, library.ExpandOptions{MaxNodes: req.MaxNodes,
			Progress: func(p model.ExpansionResult) { set(p.Hydrated, total, "Following citations") }})
		if err != nil {
			return "", err
		}
		done := fmt.Sprintf("%s fetched, %s citations", count(res.Hydrated, "paper"), count(res.Edges, "new"))
		switch res.StoppedBecause {
		case model.StopFrontierEmpty:
			return done + ". Every reference within reach is here.", nil
		case model.StopMaxDepth:
			return done + ". What is left is too many steps from your papers.", nil
		case model.StopCancelled:
			return "", context.Canceled
		}
		return done, nil
	})
}

func count(n int, what string) string {
	if what == "paper" && n != 1 {
		what = "papers"
	}
	return fmt.Sprintf("%d %s", n, what)
}
