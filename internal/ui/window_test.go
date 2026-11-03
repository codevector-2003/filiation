package ui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/codevector-2003/filiation/internal/config"
)

// windowTest opens a Window on a first run in a fresh place, as the desktop
// app does.
func windowTest(t *testing.T) (*Window, config.Options, string) {
	t.Helper()
	root := t.TempDir()
	base := config.Options{Dir: filepath.Join(root, "settings"), DocumentsDir: filepath.Join(root, "Documents")}
	w := newWindow(context.Background(), base, Options{OpenPath: func(string) error { return nil }})
	t.Cleanup(w.Close)
	return w, base, root
}

func libraryShown(t *testing.T, w *Window) libraryJSON {
	t.Helper()
	return decode[libraryJSON](t, ask(t, w, "GET", "/api/library"))
}

func TestWindowMakesAndSwitchesLibraries(t *testing.T) {
	w, base, root := windowTest(t)
	first := libraryShown(t, w)
	if !first.Ready || first.Name != config.DefaultLibraryName {
		t.Fatalf("first run opened %+v", first)
	}

	if got := post(t, w, "/api/libraries/new", `{"name":"Thesis"}`); got.Code != http.StatusOK {
		t.Fatalf("new library: %d %s", got.Code, got.Body)
	}
	thesis := libraryShown(t, w)
	want := filepath.Join(root, "Documents", "Filiation", "Thesis")
	if thesis.Name != "Thesis" || !sameFolder(thesis.Folder, want) {
		t.Fatalf("after New Library the window shows %q at %s, want Thesis at %s", thesis.Name, thesis.Folder, want)
	}

	listed := decode[struct {
		Libraries []libraryEntryJSON `json:"libraries"`
	}](t, ask(t, w, "GET", "/api/libraries"))
	if len(listed.Libraries) != 2 || listed.Libraries[0].Open || !listed.Libraries[1].Open || !listed.Libraries[1].Found {
		t.Fatalf("libraries listed as %+v", listed.Libraries)
	}

	body, _ := json.Marshal(map[string]string{"folder": first.Folder})
	if got := post(t, w, "/api/libraries/open", string(body)); got.Code != http.StatusOK {
		t.Fatalf("switching back: %d %s", got.Code, got.Body)
	}
	if back := libraryShown(t, w); back.Name != config.DefaultLibraryName {
		t.Fatalf("switched back to %q", back.Name)
	}
	// The library switched to is the one the app opens next time.
	cfg, err := config.Load(base)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Name != config.DefaultLibraryName {
		t.Fatalf("next time the app opens %q", cfg.Name)
	}
}

func TestWindowRefusesAFolderThatIsNotALibrary(t *testing.T) {
	w, _, root := windowTest(t)
	other := filepath.Join(root, "Holiday photos")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "beach.jpg"), []byte("not a library"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"folder": other})
	if got := post(t, w, "/api/libraries/open", string(body)); got.Code != http.StatusBadRequest {
		t.Fatalf("opening a folder of photos: %d %s", got.Code, got.Body)
	}
	if lib := libraryShown(t, w); lib.Name != config.DefaultLibraryName {
		t.Fatalf("a refused folder still changed the library, to %q", lib.Name)
	}
	for _, name := range []string{"", "  ", "a/b", `a\b`, "..", "what?"} {
		body, _ := json.Marshal(map[string]string{"name": name})
		if got := post(t, w, "/api/libraries/new", string(body)); got.Code != http.StatusBadRequest {
			t.Errorf("a library named %q: %d %s", name, got.Code, got.Body)
		}
	}
}

func TestSwitchingAsksBeforeStoppingAJob(t *testing.T) {
	w, _, _ := windowTest(t)
	stopped := make(chan struct{})
	w.cur.start(httptest.NewRecorder(), "fetch", func(ctx context.Context, set func(int, int, string)) (string, error) {
		<-ctx.Done()
		close(stopped)
		return "", ctx.Err()
	})
	if got := post(t, w, "/api/libraries/new", `{"name":"Side project"}`); got.Code != http.StatusConflict {
		t.Fatalf("switching while a fetch runs: %d %s", got.Code, got.Body)
	}
	if lib := libraryShown(t, w); lib.Name != config.DefaultLibraryName {
		t.Fatalf("the library changed under a running job, to %q", lib.Name)
	}
	if got := post(t, w, "/api/libraries/new", `{"name":"Side project","stop":true}`); got.Code != http.StatusOK {
		t.Fatalf("switching once agreed: %d %s", got.Code, got.Body)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("the job was still running after the switch")
	}
	if lib := libraryShown(t, w); lib.Name != "Side project" {
		t.Fatalf("after agreeing the window shows %q", lib.Name)
	}
}

func TestClosingAndForgettingALibrary(t *testing.T) {
	w, base, _ := windowTest(t)
	first := libraryShown(t, w)
	if got := post(t, w, "/api/libraries/new", `{"name":"Old"}`); got.Code != http.StatusOK {
		t.Fatalf("new library: %d %s", got.Code, got.Body)
	}
	old := libraryShown(t, w)

	body, _ := json.Marshal(map[string]string{"folder": old.Folder})
	if got := post(t, w, "/api/libraries/forget", string(body)); got.Code != http.StatusConflict {
		t.Fatalf("forgetting the open library: %d", got.Code)
	}
	if got := post(t, w, "/api/libraries/close", `{}`); got.Code != http.StatusNoContent {
		t.Fatalf("close: %d %s", got.Code, got.Body)
	}
	if lib := libraryShown(t, w); lib.Ready {
		t.Fatalf("a closed library is still shown: %+v", lib)
	}
	if got := ask(t, w, "GET", "/api/papers"); got.Code != http.StatusServiceUnavailable {
		t.Fatalf("papers with no library open: %d", got.Code)
	}

	if got := post(t, w, "/api/libraries/forget", string(body)); got.Code != http.StatusNoContent {
		t.Fatalf("forget: %d %s", got.Code, got.Body)
	}
	cfg, err := config.Load(base)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Libraries) != 1 || !sameFolder(cfg.Libraries[0].Folder, first.Folder) {
		t.Fatalf("after forgetting, the settings list %+v", cfg.Libraries)
	}
	// Old was the default; with it forgotten the app opens the one left.
	if !sameFolder(cfg.Folder, first.Folder) {
		t.Fatalf("next time the app opens %s", cfg.Folder)
	}
	if _, err := os.Stat(old.Folder); err != nil {
		t.Fatalf("forgetting touched the folder: %v", err)
	}
}

func TestMovingTheOpenLibrary(t *testing.T) {
	w, _, root := windowTest(t)
	before := libraryShown(t, w)
	to := filepath.Join(root, "Elsewhere")
	body, _ := json.Marshal(map[string]string{"parent": to})
	if got := post(t, w, "/api/libraries/move", string(body)); got.Code != http.StatusOK {
		t.Fatalf("move: %d %s", got.Code, got.Body)
	}
	after := libraryShown(t, w)
	want := filepath.Join(to, filepath.Base(before.Folder))
	if !after.Ready || !sameFolder(after.Folder, want) {
		t.Fatalf("after the move the window shows %+v, want it at %s", after, want)
	}
	if _, err := os.Stat(before.Folder); !os.IsNotExist(err) {
		t.Fatalf("the old folder is still there: %v", err)
	}
}

func TestMenusNameEveryCommandOnce(t *testing.T) {
	seen := map[string]string{}
	for _, m := range Menus {
		for _, it := range m.Items {
			if it.ID == "" {
				continue
			}
			// Copy Citation is in Edit and in Paper, as in VS Code a command
			// can be; it must say the same in both.
			if label, ok := seen[it.ID]; ok && label != it.Label {
				t.Errorf("%s is %q and %q", it.ID, label, it.Label)
			}
			seen[it.ID] = it.Label
		}
	}
	w, _, _ := windowTest(t)
	got := decode[struct {
		Menus []Menu `json:"menus"`
	}](t, ask(t, w, "GET", "/api/menus"))
	if len(got.Menus) != len(Menus) {
		t.Fatalf("the page gets %d menus", len(got.Menus))
	}
}
