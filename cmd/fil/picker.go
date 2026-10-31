package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

// The picker turns a list of papers — from list, search or neighbours — into
// one a person moves through with the arrow keys. Enter shows a paper's
// details, from which its PDF opens in the computer's own PDF reader, its
// folder in the file manager, its DOI on the clipboard, or its neighbours
// as the next list. It runs only in a terminal; everywhere else the command
// prints as it always has.

// pickRow is one line of a picker list.
type pickRow struct {
	id    string // fil ID: what Enter shows
	seed  bool
	year  string
	title string
	note  string // on the right: "PDF · searchable", "page 5", "it cites"
	good  bool   // the note is good news: a PDF, text
}

// card is one paper's details, laid out.
type card struct {
	id, title, byline, kind, doi, ids, graph, pdf, text, context string
	pdfOK, pdfWarn                                                 bool
	pdfPath, folder                                                string
	canFetch                                                       bool
}

// pickActions is what the picker asks of the library and the computer; a
// test stands in for it.
type pickActions interface {
	card(id string) (card, error)
	neighbours(id string) (string, []pickRow, error)
	fetch(id string) (string, error)
	openFile(path string) error
	openFolder(path string) error
	copyText(s string) error
}

// pickList is one list the picker shows; neighbours stack a new one.
type pickList struct {
	heading string
	rows    []pickRow
	cur     int
	filter  string
}

type picker struct {
	act       pickActions
	p         paint
	list      pickList
	stack     []pickList
	shown     []int // rows passing the filter
	filtering bool
	filterIn  textinput.Model
	card      *card
	status    string
	statusBad bool
	width     int
	height    int
}

func newPicker(act pickActions, p paint, heading string, rows []pickRow) picker {
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = "words in the title"
	m := picker{act: act, p: p, list: pickList{heading: heading, rows: rows}, filterIn: in, width: 100, height: 30}
	m.refilter()
	return m
}

func (m picker) Init() tea.Cmd { return nil }

// refilter keeps the rows whose title has every word of the filter.
func (m *picker) refilter() {
	words := strings.Fields(strings.ToLower(m.list.filter))
	m.shown = m.shown[:0]
	for i, r := range m.list.rows {
		t := strings.ToLower(r.title)
		keep := true
		for _, w := range words {
			if !strings.Contains(t, w) {
				keep = false
				break
			}
		}
		if keep {
			m.shown = append(m.shown, i)
		}
	}
	if m.list.cur >= len(m.shown) {
		m.list.cur = max(0, len(m.shown)-1)
	}
}

// selected is the row under the cursor, if any.
func (m picker) selected() (pickRow, bool) {
	if len(m.shown) == 0 {
		return pickRow{}, false
	}
	return m.list.rows[m.shown[m.list.cur]], true
}

func (m *picker) say(msg string, bad bool) { m.status, m.statusBad = msg, bad }

func (m picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch {
		case m.filtering:
			return m.updateFilter(msg)
		case m.card != nil:
			return m.updateCard(msg)
		default:
			return m.updateList(msg)
		}
	}
	return m, nil
}

func (m picker) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filtering = false
		return m, nil
	case "esc":
		m.filtering = false
		m.filterIn.SetValue("")
		m.list.filter = ""
		m.refilter()
		return m, nil
	}
	var cmd tea.Cmd
	m.filterIn, cmd = m.filterIn.Update(msg)
	m.list.filter = m.filterIn.Value()
	m.list.cur = 0
	m.refilter()
	return m, cmd
}

func (m picker) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.status = ""
	page := max(1, m.listRows()-1)
	switch msg.String() {
	case "up", "k":
		m.list.cur = max(0, m.list.cur-1)
	case "down", "j":
		m.list.cur = min(max(0, len(m.shown)-1), m.list.cur+1)
	case "pgup":
		m.list.cur = max(0, m.list.cur-page)
	case "pgdown":
		m.list.cur = min(max(0, len(m.shown)-1), m.list.cur+page)
	case "home":
		m.list.cur = 0
	case "end":
		m.list.cur = max(0, len(m.shown)-1)
	case "/":
		m.filtering = true
		m.filterIn.SetValue(m.list.filter)
		m.filterIn.Focus()
		m.filterIn.CursorEnd()
	case "enter", "right", "l":
		m.openCard()
	case "o", "f", "c", "n":
		// The same keys work on the row as on its card.
		m.openCard()
		if m.card != nil {
			return m.cardAction(msg.String(), true)
		}
	case "esc", "q", "backspace", "left", "h":
		if len(m.stack) > 0 {
			m.list = m.stack[len(m.stack)-1]
			m.stack = m.stack[:len(m.stack)-1]
			m.filterIn.SetValue(m.list.filter)
			m.refilter()
			return m, nil
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m *picker) openCard() {
	r, ok := m.selected()
	if !ok {
		return
	}
	c, err := m.act.card(r.id)
	if err != nil {
		m.say(err.Error(), true)
		return
	}
	m.card = &c
}

func (m picker) updateCard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k := msg.String(); k {
	case "esc", "backspace", "left", "h", "q":
		m.card, m.status = nil, ""
		return m, nil
	case "o", "f", "c", "n", "g":
		return m.cardAction(k, false)
	}
	return m, nil
}

// cardAction does what a key asks of the paper on the card. From the list
// (fromList), the card closes again after a quick action.
func (m picker) cardAction(key string, fromList bool) (tea.Model, tea.Cmd) {
	c := m.card
	done := func() (tea.Model, tea.Cmd) {
		if fromList {
			m.card = nil
		}
		return m, nil
	}
	switch key {
	case "o":
		if c.pdfPath == "" {
			m.say("No PDF to open: "+c.pdf, true)
			return done()
		}
		if err := m.act.openFile(c.pdfPath); err != nil {
			m.say("Could not open the PDF: "+err.Error(), true)
		} else {
			m.say("Opened "+filepath.Base(c.pdfPath)+" in your PDF reader", false)
		}
	case "f":
		if c.folder == "" {
			m.say("This paper has no folder in papers/ yet.", true)
			return done()
		}
		if err := m.act.openFolder(c.folder); err != nil {
			m.say("Could not open the folder: "+err.Error(), true)
		} else {
			m.say("Opened its folder", false)
		}
	case "c":
		if c.doi == "" {
			m.say("This paper has no DOI.", true)
			return done()
		}
		if err := m.act.copyText(c.doi); err != nil {
			m.say("Could not copy: "+err.Error(), true)
		} else {
			m.say("Copied "+c.doi, false)
		}
	case "n":
		heading, rows, err := m.act.neighbours(c.id)
		if err != nil {
			m.say(err.Error(), true)
			return done()
		}
		if len(rows) == 0 {
			m.say("No neighbours recorded for this paper yet.", true)
			return done()
		}
		m.stack = append(m.stack, m.list)
		m.list = pickList{heading: heading, rows: rows}
		m.filterIn.SetValue("")
		m.refilter()
		m.card, m.status = nil, ""
		return m, nil
	case "g":
		if !c.canFetch {
			return m, nil
		}
		msg, err := m.act.fetch(c.id)
		if err != nil {
			m.say("Could not fetch it: "+err.Error(), true)
			return m, nil
		}
		if again, err := m.act.card(c.id); err == nil {
			m.card = &again
		}
		m.say(msg, !m.card.pdfOK)
	}
	return done()
}

// listRows is how many rows fit under the heading and above the footer.
func (m picker) listRows() int { return max(3, m.height-5) }

func (m picker) View() string {
	if m.card != nil {
		return m.viewCard()
	}
	return m.viewList()
}

func (m picker) viewList() string {
	p := m.p
	var b strings.Builder
	count := fmt.Sprintf("%d", len(m.shown))
	if len(m.shown) != len(m.list.rows) {
		count = fmt.Sprintf("%d of %d", len(m.shown), len(m.list.rows))
	}
	b.WriteString(" " + p.label(m.list.heading) + p.dim(" · "+count) + "\n")
	if m.filtering || m.list.filter != "" {
		b.WriteString(" " + m.filterIn.View() + "\n")
	} else {
		b.WriteString("\n")
	}

	rows := m.listRows()
	start := 0
	if m.list.cur >= rows {
		start = m.list.cur - rows + 1
	}
	end := min(len(m.shown), start+rows)
	titleWidth := max(16, m.width-40)
	for i := start; i < end; i++ {
		r := m.list.rows[m.shown[i]]
		star := "  "
		if r.seed {
			star = p.warn("★ ")
		}
		title := clip(r.title, titleWidth)
		note := p.dim(r.note)
		if r.good {
			note = p.ok(r.note)
		}
		year := fmt.Sprintf("%-4s", r.year)
		if i == m.list.cur {
			b.WriteString(p.label(" › ") + star + p.id(r.id) + "  " + year + "  " + p.title(title) + "  " + note + "\n")
		} else {
			b.WriteString("   " + star + p.dim(r.id) + "  " + p.dim(year) + "  " + title + "  " + note + "\n")
		}
	}
	if len(m.shown) == 0 {
		b.WriteString(p.dim("   Nothing matches. Esc clears the filter.") + "\n")
	}
	for i := end - start; i < rows; i++ {
		b.WriteString("\n")
	}
	b.WriteString(m.footer("↑↓ move · Enter details · o open PDF · f folder · c copy DOI · n neighbours · / filter · Esc back"))
	return b.String()
}

func (m picker) viewCard() string {
	p, c := m.p, m.card
	width := min(max(40, m.width-4), 96)
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 2).Width(width)
	if p.on {
		box = box.BorderForeground(lipgloss.Color(p.p.accent.hexString()))
	}
	inner := width - 4
	var lines []string
	lines = append(lines, p.title(lipgloss.NewStyle().Width(inner).Render(c.title)))
	if c.byline != "" {
		lines = append(lines, lipgloss.NewStyle().Width(inner).Render(c.byline))
	}
	lines = append(lines, "")
	row := func(label, value string) {
		if value != "" {
			lines = append(lines, p.label(fmt.Sprintf("%-8s", label))+" "+value)
		}
	}
	row("About", c.kind)
	row("DOI", c.doi)
	row("IDs", p.id(c.id)+p.dim(strings.TrimPrefix(c.ids, c.id)))
	row("Graph", c.graph)
	switch {
	case c.pdfOK && c.pdfWarn:
		row("PDF", p.warn("! "+c.pdf))
	case c.pdfOK:
		row("PDF", p.ok("✓ "+c.pdf))
	default:
		row("PDF", p.dim(c.pdf))
	}
	row("Text", c.text)
	row("Context", c.context)
	lines = append(lines, "")

	key := func(k, what string, on bool) string {
		if !on {
			return p.dim("[" + k + "] " + what)
		}
		return p.accent("["+k+"]") + " " + what
	}
	actions := []string{key("o", "Open PDF", c.pdfPath != ""), key("f", "Open folder", c.folder != ""),
		key("c", "Copy DOI", c.doi != ""), key("n", "Neighbours", true)}
	if c.canFetch {
		actions = append(actions, key("g", "Get the PDF", true))
	}
	lines = append(lines, strings.Join(actions, "   "))
	lines = append(lines, key("Esc", "Back to the list", true))

	return box.Render(strings.Join(lines, "\n")) + "\n" + m.footer("")
}

func (m picker) footer(hint string) string {
	p := m.p
	switch {
	case m.status != "" && m.statusBad:
		return " " + p.bad(m.status)
	case m.status != "":
		return " " + p.ok(m.status)
	}
	return " " + p.dim(hint)
}

// clip shortens s to n runes, with an ellipsis.
func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// --- the library and the computer, for the real picker ---

type libActions struct {
	ctx context.Context
	lib *library.Library
}

func (a libActions) card(id string) (card, error) {
	p, err := a.lib.Show(a.ctx, id)
	if err != nil {
		return card{}, err
	}
	return cardFrom(p), nil
}

func (a libActions) neighbours(id string) (string, []pickRow, error) {
	n, err := a.lib.Neighbours(a.ctx, id)
	if err != nil {
		return "", nil, err
	}
	var rows []pickRow
	for _, w := range n.Cites {
		rows = append(rows, workRow(w, "it cites"))
	}
	for _, w := range n.CitedBy {
		rows = append(rows, workRow(w, "cites it"))
	}
	return "Neighbours of " + clip(n.Work.DisplayTitle(), 50), rows, nil
}

func (a libActions) fetch(id string) (string, error) {
	f, err := a.lib.FetchOne(a.ctx, id)
	if err != nil {
		return "", err
	}
	switch {
	case f.File != "" && f.Already:
		return "Its PDF was already in papers/", nil
	case f.File != "":
		return "Downloaded " + filepath.Base(f.File), nil
	case len(f.Attempts) > 0:
		return "No PDF: " + reasons(outcomesOf(f.Attempts)), nil
	}
	return "No legal PDF link is known for it", nil
}

func (a libActions) openFile(path string) error {
	if err := a.inLibrary(path); err != nil {
		return err
	}
	return openWithSystem(path)
}

func (a libActions) openFolder(path string) error {
	if err := a.inLibrary(path); err != nil {
		return err
	}
	return openWithSystem(path)
}

func (a libActions) copyText(s string) error { return clipboard.WriteAll(s) }

// inLibrary refuses anything outside the library's folder: the picker opens
// the library's own files, never a path from anywhere else.
func (a libActions) inLibrary(path string) error {
	root := a.lib.Folder()
	if root == "" {
		return errors.New("this library has no folder")
	}
	if !within(root, path) {
		return fmt.Errorf("%s is outside the library folder", path)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%s is not there any more: fil library scan finds moved files", filepath.Base(path))
	}
	return nil
}

// within reports whether path is root or inside it.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// cardFrom lays out what Show knows about a paper.
func cardFrom(p library.Paper) card {
	w := p.Work
	c := card{id: w.FilID, title: w.DisplayTitle(), byline: byline(w), folder: p.Folder}
	var kind []string
	if w.Year != nil {
		kind = append(kind, fmt.Sprint(*w.Year))
	}
	if w.Type != model.TypeUnknown {
		kind = append(kind, string(w.Type))
	}
	if w.Venue != "" {
		kind = append(kind, w.Venue)
	}
	if w.OAStatus != model.OAUnknown {
		kind = append(kind, string(w.OAStatus)+" open access")
	}
	c.kind = strings.Join(kind, " · ")
	if w.DOI != nil {
		c.doi = *w.DOI
	}
	c.ids = w.FilID
	if w.OpenAlexID != "" {
		c.ids += " · " + w.OpenAlexID
	}
	if w.IsStub() {
		c.graph = "not fetched yet, so its references are unknown"
	} else {
		c.graph = fmt.Sprintf("cites %s · cited by %s in your library", plural(p.Cites, "work", "works"), plural(p.CitedBy, "work", "works"))
	}
	for _, f := range p.Files {
		if f.Missing {
			continue
		}
		what := f.Version
		if what == "" {
			what = map[string]string{"import": "your copy", "found": "found in papers/", "download": "downloaded"}[f.Origin]
		}
		c.pdf = filepath.Base(f.Path) + " (" + what + ")"
		c.pdfOK, c.pdfPath = true, f.Path
		if f.Mismatch {
			c.pdfWarn = true
			c.pdf += ": its first pages name another paper or edition; check it"
		}
		break
	}
	if !c.pdfOK {
		c.pdf = "none: " + pdfWhy(p)
		c.canFetch = w.OpenAlexID != ""
	}
	switch {
	case p.Text == nil && c.pdfOK:
		c.text = "not read yet: fil extract reads it"
	case p.Text == nil:
	case p.Text.Status == "ok":
		c.text = fmt.Sprintf("searchable, %s", plural(p.Text.Pages, "page", "pages"))
	default:
		c.text = "none: " + textReason(p.Text.Status, p.Text.Reason)
	}
	if p.Contexts > 0 {
		c.context = fmt.Sprintf("%s of %s carry the sentence they are cited in", thousands(p.Contexts), plural(p.Cites, "reference", "references"))
	}
	return c
}

// workRow is a work as a picker row, with a note.
func workRow(w model.Work, note string) pickRow {
	year := ""
	if w.Year != nil {
		year = fmt.Sprint(*w.Year)
	}
	return pickRow{id: w.FilID, seed: w.IsSeed, year: year, title: w.DisplayTitle(), note: note}
}

// listRow is a list row as a picker row.
func listRow(r library.Listed) pickRow {
	year := ""
	if r.Year > 0 {
		year = fmt.Sprint(r.Year)
	}
	title := r.Title
	if title == "" {
		title = "[not fetched: " + r.OpenAlexID + "]"
	}
	var note []string
	if r.HasPDF {
		note = append(note, "PDF")
	}
	switch r.TextStatus {
	case "":
	case "ok":
		note = append(note, "searchable")
	default:
		note = append(note, "no text")
	}
	if r.Contexts > 0 {
		note = append(note, plural(r.Contexts, "sentence", "sentences"))
	}
	return pickRow{id: r.FilID, seed: r.Seed, year: year, title: title, note: strings.Join(note, " · "), good: r.HasPDF}
}

// canPick reports whether the picker can run: a terminal on both ends.
func (a *app) canPick() bool {
	return a.interactive && a.stdoutFile != nil && isTerminal(a.stdoutFile)
}

// pick runs the picker over rows until the person leaves it.
func (a *app) pick(ctx context.Context, lib *library.Library, heading string, rows []pickRow) error {
	if len(rows) == 0 {
		fmt.Fprintln(a.stdout, "Nothing to show.")
		return nil
	}
	m := newPicker(libActions{ctx: ctx, lib: lib}, a.outPaint, heading, rows)
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithOutput(a.raw())).Run()
	return err
}
