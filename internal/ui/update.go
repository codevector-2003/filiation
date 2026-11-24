package ui

import (
	"errors"
	"net/http"

	"github.com/codevector-2003/filiation/internal/sources/github"
	"github.com/codevector-2003/filiation/internal/update"
)

// Updating the app (D38): whether a newer version is out, and on Windows,
// downloading, checking and installing it. None of it needs a library open.

func (s *server) updateCheck(w http.ResponseWriter, r *http.Request) {
	if s.Updater == nil {
		send(w, map[string]any{"current": s.Version, "unavailable": true})
		return
	}
	info, err := s.Updater.Check(r.Context())
	switch {
	case errors.Is(err, github.ErrOffline):
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		send(w, map[string]any{"error": "No connection. The check needs the internet to reach GitHub. Try again when you are online.", "offline": true})
	case err != nil:
		fail(w, http.StatusBadGateway, errors.New("GitHub did not say which version is the latest. Try again later."))
	default:
		send(w, info)
	}
}

func (s *server) updateDownload(w http.ResponseWriter, _ *http.Request) {
	if s.Updater == nil {
		fail(w, http.StatusNotFound, errors.New("updates are not available here"))
		return
	}
	err := s.Updater.Download()
	switch {
	case errors.Is(err, update.ErrBusy):
		fail(w, http.StatusConflict, err)
	case err != nil:
		fail(w, http.StatusBadRequest, err)
	default:
		send(w, s.Updater.Status())
	}
}

func (s *server) updateStatus(w http.ResponseWriter, _ *http.Request) {
	if s.Updater == nil {
		send(w, update.State{Phase: update.Idle})
		return
	}
	send(w, s.Updater.Status())
}

func (s *server) updateCancel(w http.ResponseWriter, _ *http.Request) {
	if s.Updater != nil {
		s.Updater.Cancel()
	}
	w.WriteHeader(http.StatusNoContent)
}

// updateInstall starts Setup. The page has saved its tabs first, and closes
// the window as soon as this answers.
func (s *server) updateInstall(w http.ResponseWriter, _ *http.Request) {
	if s.Updater == nil {
		fail(w, http.StatusNotFound, errors.New("updates are not available here"))
		return
	}
	if err := s.Updater.Install(); err != nil {
		fail(w, http.StatusConflict, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
