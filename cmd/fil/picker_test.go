package main

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

// fakeActions stands in for the library and the computer, and records what
// the picker asked of them.
type fakeActions struct {
	cards     map[string]card
	opened    []string
	folders   []string
	copied    []string
	urls      []string
	fetched   []string
	neighbour []pickRow
}

func (f *fakeActions) card(id string) (card, error) {
	c, ok := f.cards[id]
	if !ok {
		return card{}, errors.New("no such paper")
	}
	return c, nil
}
func (f *fakeActions) neighbours(id string) (string, []pickRow, error) {
	return "Neighbours of " + id, f.neighbour, nil
}
func (f *fakeActions) fetch(id string) (string, error) {
	f.fetched = append(f.fetched, id)
	c := f.cards[id]
	c.pdfOK, c.pdfPath, c.pdf, c.canFetch = true, "/lib/papers/new.pdf", "new.pdf (downloaded)", false
	f.cards[id] = c
	return "Downloaded new.pdf", nil
}
func (f *fakeActions) openFile(p string) error   { f.opened = append(f.opened, p); return nil }
func (f *fakeActions) openFolder(p string) error { f.folders = append(f.folders, p); return nil }
func (f *fakeActions) copyText(s string) error   { f.copied = append(f.copied, s); return nil }
func (f *fakeActions) openURL(u string) error    { f.urls = append(f.urls, u); return nil }

func pickerFixture() (*fakeActions, picker) {
	act := &fakeActions{
		cards: map[string]card{
			"F45ZSWF64": {id: "F45ZSWF64", title: "The state of OA", doi: "10.7717/peerj.4375", pdfOK: true,
				pdfPath: "/lib/papers/Piwowar/oa.pdf", folder: "/lib/papers/Piwowar", pdf: "oa.pdf (your copy)",
				web: "https://doi.org/10.7717/peerj.4375"},
			"FW08M2BQ1": {id: "FW08M2BQ1", title: "Citation Advantage of Open Access Articles", pdf: "none: no open copy",
				canFetch: true},
		},
		neighbour: []pickRow{{id: "FN1111111", title: "Cited one", note: "it cites"}},
	}
	rows := []pickRow{
		{id: "F45ZSWF64", seed: true, year: "2018", title: "The state of OA", note: "PDF · searchable", good: true},
		{id: "FW08M2BQ1", year: "2006", title: "Citation Advantage of Open Access Articles"},
		{id: "F1PTH9JC1", year: "2012", title: "Anatomy of open access publishing"},
	}
	return act, newPicker(act, paint{}, "Papers", rows)
}

func press(m picker, keys ...string) picker {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "end":
			msg = tea.KeyMsg{Type: tea.KeyEnd}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(picker)
	}
	return m
}

func TestPickerMovesAndShowsACard(t *testing.T) {
	_, m := pickerFixture()
	m = press(m, "down", "down", "down", "up")
	if r, _ := m.selected(); r.id != "FW08M2BQ1" {
		t.Errorf("after down×3, up the cursor is on %s", r.id)
	}
	m = press(m, "enter")
	if m.card == nil || m.card.id != "FW08M2BQ1" {
		t.Fatalf("Enter did not show the paper's card: %+v", m.card)
	}
	view := m.View()
	for _, want := range []string{"Citation Advantage of Open Access Articles", "none: no open copy", "[g] Get the PDF", "Back to the list"} {
		if !strings.Contains(view, want) {
			t.Errorf("card is missing %q:\n%s", want, view)
		}
	}
	m = press(m, "esc")
	if m.card != nil {
		t.Error("Esc did not close the card")
	}
}

func TestPickerOpensFoldersCopiesAndFetches(t *testing.T) {
	act, m := pickerFixture()
	m = press(m, "enter", "o", "f", "c")
	if !slices.Equal(act.opened, []string{"/lib/papers/Piwowar/oa.pdf"}) || !slices.Equal(act.folders, []string{"/lib/papers/Piwowar"}) ||
		!slices.Equal(act.copied, []string{"10.7717/peerj.4375"}) {
		t.Errorf("opened %v, folders %v, copied %v", act.opened, act.folders, act.copied)
	}
	if !strings.Contains(m.View(), "Copied 10.7717/peerj.4375") {
		t.Error("the copy was not confirmed")
	}

	// A paper with no PDF says why, and g gets it.
	m = press(m, "esc", "down", "o")
	if len(act.opened) != 1 || !strings.Contains(m.status, "No PDF to open") {
		t.Errorf("o on a paper with no PDF: opened %v, status %q", act.opened, m.status)
	}
	m = press(m, "enter", "g")
	if !slices.Equal(act.fetched, []string{"FW08M2BQ1"}) || m.card == nil || !m.card.pdfOK {
		t.Fatalf("g did not fetch and refresh the card: %v %+v", act.fetched, m.card)
	}
	m = press(m, "o")
	if !slices.Contains(act.opened, "/lib/papers/new.pdf") {
		t.Error("the fetched PDF did not open")
	}
}

// From the list, o opens the row's PDF without staying on its card.
func TestPickerOpensFromTheList(t *testing.T) {
	act, m := pickerFixture()
	m = press(m, "o")
	if len(act.opened) != 1 || m.card != nil {
		t.Errorf("o from the list: opened %v, card %v", act.opened, m.card != nil)
	}
}

func TestPickerNeighboursStackAndUnwind(t *testing.T) {
	_, m := pickerFixture()
	m = press(m, "n")
	if m.list.heading != "Neighbours of F45ZSWF64" || len(m.stack) != 1 {
		t.Fatalf("n did not open the neighbours: %q, stack %d", m.list.heading, len(m.stack))
	}
	m = press(m, "esc")
	if m.list.heading != "Papers" || len(m.stack) != 0 {
		t.Errorf("Esc did not go back: %q", m.list.heading)
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Error("Esc on the first list did not leave the picker")
	}
	_ = next
}

func TestPickerFilters(t *testing.T) {
	_, m := pickerFixture()
	m = press(m, "/", "o", "p", "e", "n", " ", "a", "c")
	if len(m.shown) != 2 {
		t.Errorf("filter 'open ac' kept %d rows, want 2", len(m.shown))
	}
	m = press(m, "enter", "end")
	if r, _ := m.selected(); r.id != "F1PTH9JC1" {
		t.Errorf("End within the filter is %s", r.id)
	}
	if !strings.Contains(m.View(), "2 of 3") {
		t.Errorf("the count does not say it is filtered:\n%s", m.View())
	}
	m = press(m, "/", "esc")
	if len(m.shown) != 3 {
		t.Error("Esc in the filter did not clear it")
	}
}

func TestCardFromAPaper(t *testing.T) {
	year, doi := 2018, "10.7717/peerj.4375"
	title := "The state of OA"
	p := library.Paper{
		Work:  model.Work{FilID: "F45ZSWF64", OpenAlexID: "W2741809807", Title: &title, Year: &year, DOI: &doi, Hydrated: true, Type: "article"},
		Cites: 47, CitedBy: 3, Folder: "/lib/papers/Piwowar",
		Files:    []library.PaperFile{{Path: "/lib/papers/Piwowar/gone.pdf", Missing: true}, {Path: "/lib/papers/Piwowar/oa.pdf", Origin: "import"}},
		Text:     &library.PaperText{Status: "ok", Pages: 23},
		Contexts: 28,
	}
	c := cardFrom(p)
	if c.pdfPath != "/lib/papers/Piwowar/oa.pdf" || c.pdf != "oa.pdf (your copy)" || !c.pdfOK || c.canFetch {
		t.Errorf("pdf = %q %q %v %v", c.pdfPath, c.pdf, c.pdfOK, c.canFetch)
	}
	for got, want := range map[string]string{c.kind: "2018 · article", c.graph: "cites 47 works · cited by 3 works in your library",
		c.text: "searchable, 23 pages", c.context: "28 of 47 references carry the sentence they are cited in", c.ids: "F45ZSWF64 · W2741809807"} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestWithin(t *testing.T) {
	root := filepath.Join("lib", "My Library")
	for path, want := range map[string]bool{
		filepath.Join(root, "papers", "a.pdf"): true,
		root:                                   true,
		filepath.Join(root, "..", "elsewhere.pdf"):     false,
		filepath.Join("lib", "My Library2", "a.pdf"):   false,
		filepath.Join(root, "papers", "..", "..", "x"): false,
	} {
		if got := within(root, path); got != want {
			t.Errorf("within(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestWithPick(t *testing.T) {
	tests := map[string][]string{
		"list --with-pdf":      {"list", "--with-pdf", "--pick"},
		"search open access":   {"search", "open", "access", "--pick"},
		"neighbours F45ZSWF64": {"neighbours", "F45ZSWF64", "--pick"},
		"list --json":          {"list", "--json"},
		"search --help":        {"search", "--help"},
		"stats":                {"stats"},
	}
	for line, want := range tests {
		args, _ := sessionArgs(line)
		if got := withPick(args); !slices.Equal(got, want) {
			t.Errorf("withPick(%q) = %q, want %q", line, got, want)
		}
	}
}

// Off a terminal --pick changes nothing: scripts get the list as before.
func TestPickFlagOffATerminal(t *testing.T) {
	h := newHarness(t, peerj)
	h.run(false, "", "add", "10.7717/peerj.4375")
	_, plain, _ := h.run(false, "", "list")
	code, picked, _ := h.run(false, "", "list", "--pick")
	if code != exitOK || picked != plain {
		t.Errorf("list --pick off a terminal differs:\n%s\nvs\n%s", picked, plain)
	}
}

func TestCapped(t *testing.T) {
	ws := []model.Work{{FilID: "F1"}, {FilID: "F2"}, {FilID: "F3"}}
	if got := capped(ws, 2); len(got) != 2 {
		t.Errorf("capped to 2 = %d", len(got))
	}
	if got := capped(ws, 0); len(got) != 3 {
		t.Errorf("--limit 0 must keep all, got %d", len(got))
	}
}

// --context asks for the sentences, which only the printed list shows, so
// the session does not swap it for the picker (PR #19 review).
func TestWithPickLeavesContextPrinted(t *testing.T) {
	args, _ := sessionArgs("neighbours F45ZSWF64 --context")
	if got := withPick(args); slices.Contains(got, "--pick") {
		t.Errorf("withPick added --pick to %q", got)
	}
}

// [g] Get the PDF is offered only where a PDF can be kept: a library that is
// only a database has no folder for one (PR #19 review).
func TestCardOffersFetchOnlyWithAFolder(t *testing.T) {
	h := newHarness(t, peerj)
	db := filepath.Join(t.TempDir(), "only.db")
	if code, _, errOut := h.run(false, "", "--db", db, "add", "10.7717/peerj.4375"); code != exitOK {
		t.Fatalf("add with --db: %d %s", code, errOut)
	}
	if code, _, errOut := h.run(false, "", "add", "10.7717/peerj.4375"); code != exitOK {
		t.Fatalf("add to a folder library: %d %s", code, errOut)
	}
	for _, tt := range []struct {
		name  string
		db    string
		fetch bool
	}{{"database only", db, false}, {"library folder", "", true}} {
		a := &app{configDir: h.configDir, cacheDir: h.cacheDir, documentsDir: h.documentsDir, transport: h.replay, dbFlag: tt.db}
		cfg, err := a.loadConfig()
		if err != nil {
			t.Fatal(err)
		}
		lib, err := library.Open(t.Context(), cfg, a.libOptions())
		if err != nil {
			t.Fatal(err)
		}
		c, err := libActions{ctx: t.Context(), lib: lib}.card("10.7717/peerj.4375")
		lib.Close()
		if err != nil {
			t.Fatal(err)
		}
		if c.canFetch != tt.fetch {
			t.Errorf("%s: canFetch = %v, want %v", tt.name, c.canFetch, tt.fetch)
		}
	}
}

func TestPickerOpensTheWebsite(t *testing.T) {
	act, m := pickerFixture()
	m = press(m, "enter")
	if !strings.Contains(m.View(), "https://doi.org/10.7717/peerj.4375") || !strings.Contains(m.View(), "[w] Website") {
		t.Errorf("the card does not show the web page:\n%s", m.View())
	}
	m = press(m, "w")
	if !slices.Equal(act.urls, []string{"https://doi.org/10.7717/peerj.4375"}) || !strings.Contains(m.status, "in your browser") {
		t.Errorf("w opened %v, status %q", act.urls, m.status)
	}
	// From the list too; and a paper with no page says so.
	m = press(m, "esc", "down", "w")
	if len(act.urls) != 1 || !strings.Contains(m.status, "No web page") {
		t.Errorf("w with no page: opened %v, status %q", act.urls, m.status)
	}
}

func TestWebPage(t *testing.T) {
	doi, oa := "10.7717/peerj.4375", "https://example.org/oa"
	tests := []struct {
		name string
		p    library.Paper
		want string
	}{
		{"the DOI first", library.Paper{Work: model.Work{DOI: &doi, OpenAlexID: "W1", OAURL: &oa}}, "https://doi.org/10.7717/peerj.4375"},
		{"then an open copy's landing page", library.Paper{Work: model.Work{OpenAlexID: "W1", OAURL: &oa},
			PDF: model.PDFLinks{Links: []model.Location{{LandingURL: "javascript:alert(1)"}, {LandingURL: "https://arxiv.org/abs/1"}}}}, "https://arxiv.org/abs/1"},
		{"then OpenAlex's link", library.Paper{Work: model.Work{OpenAlexID: "W1", OAURL: &oa}}, oa},
		{"then OpenAlex's page", library.Paper{Work: model.Work{OpenAlexID: "W2741809807"}}, "https://openalex.org/W2741809807"},
		{"a local document has none", library.Paper{Work: model.Work{FilID: "F1"}}, ""},
	}
	for _, tt := range tests {
		if got := webPage(tt.p); got != tt.want {
			t.Errorf("%s: %q, want %q", tt.name, got, tt.want)
		}
	}
}

// Only web addresses open: never a file, a script or anything with a
// space a command line could split.
func TestWebAddress(t *testing.T) {
	for u, want := range map[string]bool{
		"https://doi.org/10.7717/peerj.4375": true,
		"http://arxiv.org/abs/1706.03762":    true,
		"file:///C:/Windows/System32/x.exe":  false,
		"javascript:alert(1)":                false,
		`C:\Users\me\a.pdf`:                  false,
		"https://":                           false,
		"https://a.org/x y":                  false,
		`https://a.org/"x`:                   false,
	} {
		if got := webAddress(u); got != want {
			t.Errorf("webAddress(%q) = %v, want %v", u, got, want)
		}
	}
	if err := (libActions{}).openURL("file:///etc/passwd"); err == nil {
		t.Error("openURL opened a file address")
	}
}
