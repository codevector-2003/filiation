package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/library"
)

// Window serves the screens for one library at a time, and lets the person
// open, make, move and switch libraries while the app runs (D30). Each library
// gets a server of its own, so nothing the screens of one remembered, such as
// the Inbox's list, follows the person into the next.
type Window struct {
	base config.Options // where the settings are; empty outside tests
	opts Options        // everything but the library

	mu  sync.RWMutex // every request to the open library holds it for reading
	cur *server
	h   http.Handler
	cfg *config.Config // the open library's settings; nil when none is open

	change sync.Mutex // one change of library at a time; guards cfg for reading
	mux    *http.ServeMux
}

// NewWindow opens the library fil would open, the default or on a first run a
// new one under Documents, and serves it. Anything wrong is shown in the
// window, not lost on a console a double-clicked app does not have.
func NewWindow(ctx context.Context, opts Options) *Window {
	return newWindow(ctx, config.Options{}, opts)
}

func newWindow(ctx context.Context, base config.Options, opts Options) *Window {
	w := &Window{base: base, opts: opts, mux: http.NewServeMux()}
	w.mux.HandleFunc("GET /api/libraries", w.list)
	w.mux.HandleFunc("POST /api/libraries/pick", w.pick)
	w.mux.HandleFunc("POST /api/libraries/open", w.open)
	w.mux.HandleFunc("POST /api/libraries/new", w.create)
	w.mux.HandleFunc("POST /api/libraries/close", w.close)
	w.mux.HandleFunc("POST /api/libraries/forget", w.forget)
	w.mux.HandleFunc("POST /api/libraries/move", w.move)

	cfg, err := config.Load(base)
	if err != nil {
		w.show(nil, nil, err.Error())
		return w
	}
	// With no terminal to ask in, a first run takes the default place, as an
	// assistant's first fil mcp does.
	if cfg.FirstRun {
		if err := cfg.Save(); err != nil {
			w.show(nil, nil, err.Error())
			return w
		}
	}
	lib, err := openLibrary(ctx, cfg)
	if err != nil {
		w.show(cfg, nil, err.Error())
		return w
	}
	w.show(cfg, lib, "")
	return w
}

func openLibrary(ctx context.Context, cfg *config.Config) (*library.Library, error) {
	lib, err := library.Open(ctx, cfg, library.Options{})
	if err != nil {
		return nil, err
	}
	// People move and delete files in papers/ themselves. A scan that fails
	// costs only fresh file records, so the library still opens.
	if lib.Folder() != "" {
		lib.Scan(ctx)
	}
	return lib, nil
}

// show makes lib the library on screen. The caller holds mu, or nothing is
// serving yet.
func (w *Window) show(cfg *config.Config, lib *library.Library, problem string) {
	opts := w.opts
	opts.Library, opts.Problem, opts.Name = lib, problem, ""
	if cfg != nil && lib != nil {
		opts.Name = cfg.Name
	}
	w.cur = &server{Options: opts}
	w.h = w.cur.routes()
	w.cfg = cfg
	if lib == nil {
		w.cfg = nil
	}
}

func (w *Window) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/libraries" || strings.HasPrefix(r.URL.Path, "/api/libraries/") {
		w.mux.ServeHTTP(rw, r)
		return
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	w.h.ServeHTTP(rw, r)
}

// Close stops what is running and closes the open library.
func (w *Window) Close() {
	w.change.Lock()
	defer w.change.Unlock()
	w.swap(nil, nil, "")
}

// swap puts lib on screen once the requests to the last one have finished,
// stopping its job, and closes the last.
func (w *Window) swap(cfg *config.Config, lib *library.Library, problem string) {
	w.mu.Lock()
	old := w.cur
	old.stopJob()
	w.show(cfg, lib, problem)
	w.mu.Unlock()
	if old.Library != nil {
		old.Library.Close()
	}
}

type libraryEntryJSON struct {
	Name   string `json:"name"`
	Folder string `json:"folder"`
	Open   bool   `json:"open"`
	Found  bool   `json:"found"` // its folder is still there
}

func (w *Window) list(rw http.ResponseWriter, _ *http.Request) {
	w.change.Lock()
	defer w.change.Unlock()
	cfg, err := config.Load(w.base)
	if err != nil {
		fail(rw, http.StatusInternalServerError, err)
		return
	}
	out := []libraryEntryJSON{}
	for _, l := range cfg.Libraries {
		_, err := os.Stat(l.Folder)
		out = append(out, libraryEntryJSON{Name: l.Name, Folder: l.Folder, Found: err == nil,
			Open: w.cfg != nil && sameFolder(w.cfg.Folder, l.Folder)})
	}
	home, _ := w.home()
	send(rw, map[string]any{"libraries": out, "home": home})
}

// home is where a new library goes unless the person picks somewhere else.
func (w *Window) home() (string, error) {
	docs := w.base.DocumentsDir
	if docs == "" {
		d, err := config.DocumentsDir()
		if err != nil {
			return "", err
		}
		docs = d
	}
	return filepath.Join(docs, "Filiation"), nil
}

func (w *Window) pick(rw http.ResponseWriter, r *http.Request) {
	var req struct {
		Title string `json:"title"`
	}
	if !readBody(rw, r, &req) {
		return
	}
	if w.opts.PickFolder == nil {
		fail(rw, http.StatusNotImplemented, errors.New("choosing a folder needs the desktop app"))
		return
	}
	dir, err := w.opts.PickFolder(req.Title)
	if err != nil {
		fail(rw, http.StatusInternalServerError, err)
		return
	}
	if dir == "" {
		rw.WriteHeader(http.StatusNoContent)
		return
	}
	send(rw, map[string]string{"folder": dir})
}

// changeRequest is a change of library. Stop says the person agreed to stop
// what is running; without it a running job refuses the change.
type changeRequest struct {
	Folder string `json:"folder"`
	Name   string `json:"name"`
	Parent string `json:"parent"`
	Stop   bool   `json:"stop"`
}

func readBody(rw http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil && !errors.Is(err, io.EOF) {
		fail(rw, http.StatusBadRequest, err)
		return false
	}
	return true
}

// busy answers 409 when a job is running and the person has not agreed to
// stop it.
func (w *Window) busy(rw http.ResponseWriter, req changeRequest) bool {
	w.mu.RLock()
	job, running := w.cur.running()
	w.mu.RUnlock()
	if running && !req.Stop {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusConflict)
		json.NewEncoder(rw).Encode(map[string]any{"error": "something is running in this library", "job": job})
		return true
	}
	return false
}

func (w *Window) open(rw http.ResponseWriter, r *http.Request) {
	var req changeRequest
	if !readBody(rw, r, &req) {
		return
	}
	if strings.TrimSpace(req.Folder) == "" {
		fail(rw, http.StatusBadRequest, errors.New("choose a folder"))
		return
	}
	w.change.Lock()
	defer w.change.Unlock()
	if w.cfg != nil && sameFolder(w.cfg.Folder, req.Folder) {
		rw.WriteHeader(http.StatusNoContent)
		return
	}
	if w.busy(rw, req) {
		return
	}
	base, err := config.Load(w.base)
	if err != nil {
		fail(rw, http.StatusInternalServerError, err)
		return
	}
	cfg, err := library.Adopt(r.Context(), base, req.Folder)
	if err != nil {
		failChange(rw, err)
		return
	}
	w.switchTo(rw, r, cfg)
}

// libraryName refuses a name that would not make one folder.
func libraryName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", fmt.Errorf("give the library a name: %w", errs.ErrInvalidInput)
	case name == "." || name == "..", strings.ContainsAny(name, `/\:*?"<>|`):
		return "", fmt.Errorf("a library's name becomes its folder's, so it cannot hold / \\ : * ? \" < > |: %w", errs.ErrInvalidInput)
	}
	return name, nil
}

func (w *Window) create(rw http.ResponseWriter, r *http.Request) {
	var req changeRequest
	if !readBody(rw, r, &req) {
		return
	}
	name, err := libraryName(req.Name)
	if err != nil {
		failChange(rw, err)
		return
	}
	w.change.Lock()
	defer w.change.Unlock()
	if w.busy(rw, req) {
		return
	}
	parent := strings.TrimSpace(req.Parent)
	if parent == "" {
		if parent, err = w.home(); err != nil {
			fail(rw, http.StatusInternalServerError, err)
			return
		}
	}
	base, err := config.Load(w.base)
	if err != nil {
		fail(rw, http.StatusInternalServerError, err)
		return
	}
	made, err := library.Create(r.Context(), base, filepath.Join(parent, name), false)
	if err != nil {
		failChange(rw, err)
		return
	}
	cfg, err := config.Load(withLibrary(w.base, made.Folder))
	if err != nil {
		fail(rw, http.StatusInternalServerError, err)
		return
	}
	w.switchTo(rw, r, cfg)
}

func withLibrary(base config.Options, folder string) config.Options {
	base.LibraryFlag = folder
	return base
}

// switchTo opens cfg's library and puts it on screen, leaving the last one
// open if it cannot. It becomes the library the app opens next time.
func (w *Window) switchTo(rw http.ResponseWriter, r *http.Request, cfg *config.Config) {
	lib, err := openLibrary(r.Context(), cfg)
	if err != nil {
		fail(rw, http.StatusInternalServerError, err)
		return
	}
	cfg.MakeDefault()
	if err := cfg.Save(); err != nil {
		lib.Close()
		fail(rw, http.StatusInternalServerError, err)
		return
	}
	w.swap(cfg, lib, "")
	send(rw, map[string]string{"name": cfg.Name, "folder": cfg.Folder})
}

func failChange(rw http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrNotLibrary), errors.Is(err, errs.ErrInvalidInput), errors.Is(err, errs.ErrInvalidConfig):
		fail(rw, http.StatusBadRequest, err)
	default:
		fail(rw, http.StatusInternalServerError, err)
	}
}

func (w *Window) close(rw http.ResponseWriter, r *http.Request) {
	var req changeRequest
	if !readBody(rw, r, &req) {
		return
	}
	w.change.Lock()
	defer w.change.Unlock()
	if w.busy(rw, req) {
		return
	}
	w.swap(nil, nil, "")
	rw.WriteHeader(http.StatusNoContent)
}

func (w *Window) forget(rw http.ResponseWriter, r *http.Request) {
	var req changeRequest
	if !readBody(rw, r, &req) {
		return
	}
	w.change.Lock()
	defer w.change.Unlock()
	if w.cfg != nil && sameFolder(w.cfg.Folder, req.Folder) {
		fail(rw, http.StatusConflict, errors.New("this library is open: switch to another first"))
		return
	}
	cfg, err := config.Load(w.base)
	if err != nil {
		fail(rw, http.StatusInternalServerError, err)
		return
	}
	if err := cfg.ForgetLibrary(req.Folder); err != nil {
		fail(rw, http.StatusInternalServerError, err)
		return
	}
	rw.WriteHeader(http.StatusNoContent)
}

// move moves the open library into the folder chosen, as fil library move
// does. The library has to be closed while it moves; if the move fails it
// opens again where it was.
func (w *Window) move(rw http.ResponseWriter, r *http.Request) {
	var req changeRequest
	if !readBody(rw, r, &req) {
		return
	}
	w.change.Lock()
	defer w.change.Unlock()
	if w.cfg == nil {
		fail(rw, http.StatusConflict, errors.New("no library is open"))
		return
	}
	if strings.TrimSpace(req.Parent) == "" {
		fail(rw, http.StatusBadRequest, errors.New("choose where to move it"))
		return
	}
	if w.busy(rw, req) {
		return
	}
	cfg := w.cfg
	name := filepath.Base(cfg.Folder)
	if cfg.Folder == "" {
		name = cfg.Name
	}
	w.swap(nil, nil, "")
	moved, err := library.Move(r.Context(), cfg, filepath.Join(req.Parent, name))
	if err != nil {
		w.reopen(r.Context(), cfg)
		failChange(rw, err)
		return
	}
	now, err := config.Load(withLibrary(w.base, moved.Folder))
	if err != nil {
		fail(rw, http.StatusInternalServerError, err)
		return
	}
	lib, err := openLibrary(r.Context(), now)
	if err != nil {
		w.swap(now, nil, err.Error())
		fail(rw, http.StatusInternalServerError, err)
		return
	}
	w.swap(now, lib, "")
	send(rw, map[string]string{"folder": moved.Folder, "oldDatabase": moved.OldDatabase})
}

func (w *Window) reopen(ctx context.Context, cfg *config.Config) {
	lib, err := openLibrary(ctx, cfg)
	if err != nil {
		w.swap(cfg, nil, err.Error())
		return
	}
	w.swap(cfg, lib, "")
}

// sameFolder compares folders as the system does: Windows ignores case.
func sameFolder(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	a, b = filepath.Clean(a), filepath.Clean(b)
	if filepath.Separator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}
