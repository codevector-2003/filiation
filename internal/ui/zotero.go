package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/codevector-2003/filiation/internal/errs"
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
	// State is ready, notRunning or notAllowed; or, for an export file,
	// notExport.
	State string `json:"state"`
	// From is the export file's name, and Folder where it is, when the
	// library is read from one rather than from a running Zotero.
	From        string            `json:"from,omitempty"`
	Folder      string            `json:"folder,omitempty"`
	PDFsMissing int               `json:"pdfsMissing"`
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
	from := s.work.zoteroFrom
	s.work.mu.Unlock()
	if from != "" {
		out.From, out.Folder = filepath.Base(from), filepath.Dir(from)
	}
	p, err := s.Library.ZoteroPreview(r.Context(), from)
	switch {
	case errors.Is(err, library.ErrZoteroNotRunning):
		out.State = "notRunning"
	case errors.Is(err, library.ErrZoteroNotAllowed):
		out.State = "notAllowed"
	case errors.Is(err, library.ErrZoteroNotExport), from != "" && errors.Is(err, fs.ErrNotExist):
		out.State = "notExport"
	case err != nil:
		fail(w, http.StatusInternalServerError, err)
		return
	default:
		out.State = "ready"
		out.Items, out.WithDOI, out.WithOtherID, out.NoID = p.Items, p.WithDOI, p.WithOtherID, p.NoID
		out.PDFs, out.Tags, out.Already, out.PDFsMissing = p.PDFs, p.Tags, p.Already, p.PDFsMissing
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
	s.work.mu.Lock()
	from := s.work.zoteroFrom
	s.work.mu.Unlock()
	s.start(w, "zotero", func(ctx context.Context, set func(int, int, string)) (string, error) {
		rep, err := s.Library.ZoteroImport(ctx, library.ZoteroOptions{Skip: req.Skip, Refs: req.Refs, PDFs: req.PDFs, Tags: req.Tags,
			From: from, Progress: set})
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

// zoteroExport asks for a Zotero RDF export in the system's file dialog; the
// Zotero screen then reads it instead of a running Zotero.
func (s *server) zoteroExport(w http.ResponseWriter, r *http.Request) {
	if s.PickExport == nil {
		fail(w, http.StatusNotImplemented, errors.New("choosing a file needs the desktop app"))
		return
	}
	file, err := s.PickExport()
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if file == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	s.work.mu.Lock()
	s.work.zoteroFrom = file
	s.work.mu.Unlock()
	s.zotero(w, r)
}

// zoteroLive goes back to reading the running Zotero.
func (s *server) zoteroLive(w http.ResponseWriter, r *http.Request) {
	s.work.mu.Lock()
	s.work.zoteroFrom = ""
	s.work.mu.Unlock()
	s.zotero(w, r)
}

type titleCandidateJSON struct {
	OpenAlexID string `json:"openalexId"`
	Title      string `json:"title"`
	By         string `json:"by"`
	Year       *int   `json:"year"`
	Venue      string `json:"venue"`
	Type       string `json:"type"`    // OpenAlex's: book, article, review...
	CitedBy    int    `json:"citedBy"` // across the world, which tells a book from its reviews
	TitleMatch bool   `json:"titleMatch"`
	PDFMatches bool   `json:"pdfMatches"`
	InLibrary  string `json:"inLibrary,omitempty"`
}

// paperCandidates searches OpenAlex for a local document's title. It costs
// credits, so it is a POST the person asked for, never a page load.
func (s *server) paperCandidates(w http.ResponseWriter, r *http.Request) {
	found, err := s.Library.TitleCandidates(r.Context(), r.PathValue("id"))
	if err != nil {
		failFor(w, err)
		return
	}
	out := []titleCandidateJSON{}
	for _, c := range found {
		out = append(out, titleCandidateJSON{OpenAlexID: c.Work.OpenAlexID, Title: c.Work.DisplayTitle(), By: authors(c.Work),
			Year: c.Work.Year, Venue: c.Work.Venue, Type: string(c.Work.Type), CitedBy: c.Work.CitedByCount, TitleMatch: c.TitleMatch, PDFMatches: c.PDFMatches, InLibrary: c.InLibrary})
	}
	send(w, out)
}

// paperLink links a local document to the work an identifier names.
func (s *server) paperLink(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID   string `json:"id"`
		Refs bool   `json:"refs"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	got, err := s.Library.LinkLocal(r.Context(), r.PathValue("id"), strings.TrimSpace(req.ID), req.Refs)
	if err != nil {
		failFor(w, err)
		return
	}
	send(w, struct {
		ID     string `json:"id"`
		Folded bool   `json:"folded"`
		Refs   int    `json:"refs"`
	}{got.Work.FilID, got.Folded, got.Recorded.Edges})
}

// failFor answers a lookup in OpenAlex that went wrong.
func failFor(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errs.ErrUnresolved), errors.Is(err, errs.ErrNotFound):
		fail(w, http.StatusNotFound, errors.New("OpenAlex has no record of that"))
	case errors.Is(err, errs.ErrInvalidInput):
		fail(w, http.StatusBadRequest, errors.New(reason(err, errs.ErrInvalidInput)))
	default:
		fail(w, http.StatusBadGateway, err)
	}
}
