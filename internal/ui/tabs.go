package ui

import (
	"net/http"

	"github.com/codevector-2003/filiation/internal/library"
)

// The Reader's tabs: the papers open beside the Library tab, kept in the
// library so they come back when it is opened again.

func (s *server) readerTabs(w http.ResponseWriter, r *http.Request) {
	tabs, err := s.Library.ReaderTabs(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	send(w, tabs)
}

func (s *server) saveReaderTabs(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var tabs library.ReaderTabs
	if !readBody(w, r, &tabs) {
		return
	}
	change(w, 0, s.Library.SaveReaderTabs(r.Context(), tabs))
}
