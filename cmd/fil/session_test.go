package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSessionArgs(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{`/search citation advantage`, []string{"search", "citation", "advantage"}},
		{`search "green open access" --limit 5`, []string{"search", "green open access", "--limit", "5"}},
		{`fil stats --why`, []string{"stats", "--why"}},
		{`  /show   F45ZSWF64  `, []string{"show", "F45ZSWF64"}},
		{`add 'Attention is all you need'`, []string{"add", "Attention is all you need"}},
	}
	for _, tt := range tests {
		got, err := sessionArgs(tt.line)
		if err != nil || !slices.Equal(got, tt.want) {
			t.Errorf("sessionArgs(%q) = %q, %v; want %q", tt.line, got, err, tt.want)
		}
	}
	for _, bad := range []string{`search "open access`, `/`, `   `} {
		if _, err := sessionArgs(bad); err == nil {
			t.Errorf("sessionArgs(%q) gave no error", bad)
		}
	}
}

func TestRemember(t *testing.T) {
	h := remember([]string{"stats", "list", "search x"}, "stats")
	if !slices.Equal(h, []string{"list", "search x", "stats"}) {
		t.Errorf("remember moved nothing: %q", h)
	}
	long := make([]string, historyMax)
	for i := range long {
		long[i] = strings.Repeat("x", i+1)
	}
	if got := remember(long, "new"); len(got) != historyMax || got[len(got)-1] != "new" {
		t.Errorf("history grew past %d or lost the newest", historyMax)
	}
}

// lines feeds a session, then leaves.
func lines(ls ...string) func([]string) (string, bool, error) {
	i := 0
	return func([]string) (string, bool, error) {
		if i == len(ls) {
			return "", false, nil
		}
		i++
		return ls[i-1], true, nil
	}
}

// A session runs each line as a command, survives a bad one, and keeps going
// until exit; what was typed is remembered for the next session.
func TestSessionRunsCommandsUntilExit(t *testing.T) {
	h := newHarness(t, nil)
	var out, errOut bytes.Buffer
	a := &app{stdin: strings.NewReader(""), stdout: &out, stderr: &errOut, configDir: h.configDir,
		cacheDir: h.cacheDir, documentsDir: h.documentsDir, transport: h.replay,
		prompter: lines("/version", "frobnicate", `search "unclosed`, "help", "fil where", "exit", "version")}
	if code := a.session(t.Context()); code != exitOK {
		t.Fatalf("session = %d", code)
	}
	got := out.String()
	for _, want := range []string{"No library yet", "fil " + Version, "Type any fil command", "Settings:"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Count(got, "fil "+Version) != 1 {
		t.Errorf("a line after exit was run:\n%s", got)
	}
	for _, want := range []string{"unknown command", "not closed"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("missing %q in stderr:\n%s", want, errOut.String())
		}
	}
	saved, err := os.ReadFile(filepath.Join(h.cacheDir, "session-history"))
	if err != nil || !strings.Contains(string(saved), "fil where") {
		t.Errorf("history = %q, %v", saved, err)
	}
	if _, err := os.Stat(h.documentsDir); err == nil {
		t.Error("a session with no add made a library")
	}
}

// In a session, --plain holds for its own command only, and a theme chosen
// with fil theme colours the commands after it (PR #18 review).
func TestSessionColourFollowsEachCommand(t *testing.T) {
	h := newHarness(t, nil)
	var out, errOut bytes.Buffer
	// Stand in a terminal: colour on in the saved theme unless --plain.
	term := func(_ *os.File, plain bool, name, _ string) paint {
		if plain {
			return paint{}
		}
		th, ok := themeNamed(name)
		if !ok {
			th = themes[0]
		}
		return paint{on: true, p: th.dark, depth: 24}
	}
	fake, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatal(err)
	}
	defer fake.Close()
	a := &app{stdin: strings.NewReader(""), stdout: &out, stderr: &errOut, configDir: h.configDir,
		cacheDir: h.cacheDir, documentsDir: h.documentsDir, transport: h.replay,
		stdoutFile: fake, stderrFile: fake, colourFor: term,
		prompter: lines("where --plain", "where", "theme graph", "where")}
	a.session(t.Context())

	chunks := strings.Split(out.String(), "Settings:")
	if len(chunks) != 4 {
		t.Fatalf("expected three where outputs:\n%q", out.String())
	}
	label := func(th theme) string {
		return paint{on: true, p: th.dark, depth: 24}.label("Settings:")
	}
	got := out.String()
	plainWhere := chunks[0][strings.LastIndex(chunks[0], "\n")+1:]
	if strings.Contains(plainWhere, "\x1b[") {
		t.Errorf("--plain was coloured: %q", plainWhere)
	}
	if !strings.Contains(got, label(themes[0])) {
		t.Error("the second where was not in the default theme")
	}
	graph, _ := themeNamed("graph")
	if !strings.Contains(got, label(graph)) {
		t.Error("the where after theme graph was not in graph's colours")
	}
	if a.plain || !a.outPaint.on || a.outPaint.p != graph.dark {
		t.Errorf("the session ended plain %v, paint %+v", a.plain, a.outPaint.on)
	}
}

func key(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }

func typeIn(m promptModel, s string) promptModel {
	for _, r := range s {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(promptModel)
	}
	return m
}

func send(m promptModel, msgs ...tea.Msg) promptModel {
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(promptModel)
	}
	return m
}

var testCommands = []sessionCommand{{"search", "Find passages"}, {"show", "Everything about one paper"}, {"stats", "Summarise"}, {"exit", "Leave"}}

func TestPromptMenuAndCompletion(t *testing.T) {
	m := typeIn(newPrompt(testCommands, nil, paint{}), "/s")
	if len(m.matches) != 3 {
		t.Fatalf("matches for /s = %v", m.matches)
	}
	m = send(m, key(tea.KeyDown), key(tea.KeyTab))
	if got := m.input.Value(); got != "/show " {
		t.Errorf("Down then Tab = %q, want /show ", got)
	}
	if len(m.matches) != 0 {
		t.Error("the menu stayed open after completing")
	}

	// Enter on a half-typed name completes it rather than running it.
	m = send(typeIn(newPrompt(testCommands, nil, paint{}), "/sea"), key(tea.KeyEnter))
	if m.done || m.input.Value() != "/search " {
		t.Errorf("Enter on /sea: done %v, value %q", m.done, m.input.Value())
	}
	m = send(typeIn(m, "open access"), key(tea.KeyEnter))
	if !m.done || m.value != "/search open access" {
		t.Errorf("sent %q, done %v", m.value, m.done)
	}
	if !strings.Contains(m.View(), "/search open access") {
		t.Errorf("the sent line is not left on screen: %q", m.View())
	}
}

func TestPromptHistoryAndLeaving(t *testing.T) {
	m := typeIn(newPrompt(testCommands, []string{"stats", "search hybrid"}, paint{}), "dra")
	m = send(m, key(tea.KeyUp))
	if m.input.Value() != "search hybrid" {
		t.Errorf("Up = %q", m.input.Value())
	}
	m = send(m, key(tea.KeyUp), key(tea.KeyDown), key(tea.KeyDown))
	if m.input.Value() != "dra" {
		t.Errorf("back down to the draft = %q", m.input.Value())
	}

	// Ctrl+C clears a line, and on an empty line leaves.
	m = send(m, key(tea.KeyCtrlC))
	if m.done || m.input.Value() != "" {
		t.Errorf("Ctrl+C on a line: done %v, value %q", m.done, m.input.Value())
	}
	m = send(m, key(tea.KeyCtrlC))
	if !m.done || !m.left {
		t.Error("Ctrl+C on an empty line did not leave")
	}
}

// The menu lists the CLI's own commands, the most used first, and not mcp,
// which is for assistants, not people at a prompt.
func TestSessionCommands(t *testing.T) {
	cmds := (&app{}).sessionCommands()
	names := make([]string, len(cmds))
	for i, c := range cmds {
		names[i] = c.name
	}
	if names[0] != "search" || !slices.Contains(names, "theme") || !slices.Contains(names, "exit") || slices.Contains(names, "mcp") {
		t.Errorf("session commands = %q", names)
	}
}
