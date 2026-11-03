package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/codevector-2003/filiation/internal/library"
)

// Bringing in a Zotero library: the Inbox's "Zotero" entry. Zotero is only
// read; the library keeps every rule about what comes in.

type zoteroColJSON struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	Parent string `json:"parent"`
	Items  int    `json:"items"`
}

type zoteroUnmatchedJSON struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	By     string `json:"by"`
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

type zoteroReportJSON struct {
	Items       int                   `json:"items"`
	Matched     int                   `json:"matched"`
	Unmatched   int                   `json:"unmatched"`
	Already     int                   `json:"already"`
	PDFs        int                   `json:"pdfs"`
	PDFsMissing int                   `json:"pdfsMissing"`
	Collections int                   `json:"collections"`
	NotMatched  []zoteroUnmatchedJSON `json:"notMatched"`
}

type zoteroJSON struct {
	// State is ready, notRunning or notAllowed.
	State       string            `json:"state"`
	Items       int               `json:"items"`
	WithDOI     int               `json:"withDOI"`
	WithOtherID int               `json:"withOtherID"`
	NoID        int               `json:"noID"`
	PDFs        int               `json:"pdfs"`
	Tags        int               `json:"tags"`
	Already     int               `json:"already"`
	Collections []zoteroColJSON   `json:"collections"`
	Last        *zoteroReportJSON `json:"last,omitempty"` // what the last import in this window did
}

func (s *server) zotero(w http.ResponseWriter, r *http.Request) {
	s.work.mu.Lock()
	out := zoteroJSON{Collections: []zoteroColJSON{}, Last: s.work.zotero}
	s.work.mu.Unlock()
	p, err := s.Library.ZoteroPreview(r.Context())
	switch {
	case errors.Is(err, library.ErrZoteroNotRunning):
		out.State = "notRunning"
	case errors.Is(err, library.ErrZoteroNotAllowed):
		out.State = "notAllowed"
	case err != nil:
		fail(w, http.StatusInternalServerError, err)
		return
	default:
		out.State = "ready"
		out.Items, out.WithDOI, out.WithOtherID, out.NoID = p.Items, p.WithDOI, p.WithOtherID, p.NoID
		out.PDFs, out.Tags, out.Already = p.PDFs, p.Tags, p.Already
		for _, c := range p.Collections {
			out.Collections = append(out.Collections, zoteroColJSON{Key: c.Key, Name: c.Name, Parent: c.Parent, Items: c.Items})
		}
	}
	send(w, out)
}

func (s *server) zoteroImport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Skip []string `json:"skip"`
		Refs bool     `json:"refs"`
		PDFs bool     `json:"pdfs"`
		Tags bool     `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	s.start(w, "zotero", func(ctx context.Context, set func(int, int, string)) (string, error) {
		rep, err := s.Library.ZoteroImport(ctx, library.ZoteroOptions{Skip: req.Skip, Refs: req.Refs, PDFs: req.PDFs, Tags: req.Tags,
			Progress: set})
		out := &zoteroReportJSON{Items: rep.Items, Matched: rep.Matched, Unmatched: rep.Unmatched, Already: rep.Already,
			PDFs: rep.PDFs, PDFsMissing: rep.PDFsMissing, Collections: rep.Collections, NotMatched: []zoteroUnmatchedJSON{}}
		for _, u := range rep.NotMatched {
			out.NotMatched = append(out.NotMatched, zoteroUnmatchedJSON{ID: u.FilID, Title: u.Title, By: u.By, Type: u.Type, Reason: u.Reason})
		}
		s.work.mu.Lock()
		s.work.zotero = out
		s.work.mu.Unlock()
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d of %d items in", rep.Matched+rep.Unmatched+rep.Already, rep.Items), nil
	})
}
