package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

// A session is fil kept open: run on its own in a terminal, fil shows the
// library's numbers and a prompt, runs each command typed there exactly as
// the command line would, and waits for the next. Ctrl+C stops the command
// that is running, not the session.
//
// The prompt is the only new front: every command still goes through run,
// so a session prints, colours and fails exactly as fil always does.

// historyMax is how many lines a session remembers between runs.
const historyMax = 500

// session runs until the person leaves, and returns the exit code.
func (a *app) session(ctx context.Context) int {
	a.setUpColour()
	a.printBanner(ctx)
	read := a.prompter
	if read == nil {
		read = a.promptLine
	}
	history := a.loadHistory()
	for {
		line, ok, err := read(history)
		if err != nil {
			fmt.Fprintf(a.stderr, "fil: %v\n", err)
			return exitFailure
		}
		if !ok {
			return exitOK
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		history = remember(history, line)
		a.saveHistory(history)

		args, err := sessionArgs(line)
		if err != nil {
			fmt.Fprintf(a.stderr, "fil: %v\n\n", err)
			continue
		}
		switch strings.ToLower(args[0]) {
		case "exit", "quit", "q":
			return exitOK
		case "help", "?":
			a.printSessionHelp()
			continue
		case "clear", "cls":
			fmt.Fprint(a.stdout, "\x1b[2J\x1b[H")
			continue
		}
		if a.canPick() {
			args = withPick(args)
		}
		a.runInSession(ctx, args)
		// A command's --plain was its own; a theme it chose is the session's.
		a.plain = false
		a.setUpColour()
		fmt.Fprintln(a.stdout)
	}
}

// runInSession runs one command, which Ctrl+C cancels without ending the
// session.
func (a *app) runInSession(ctx context.Context, args []string) int {
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)
	go func() {
		select {
		case <-sig:
			cancel()
		case <-cctx.Done():
		}
	}()
	return a.run(cctx, args)
}

// sessionArgs splits a typed line into arguments: words, or "quoted
// phrases" kept together. A leading "/" or "fil " is optional, so
// "/search open access", "search open access" and "fil search open access"
// are the same command.
func sessionArgs(line string) ([]string, error) {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "/")
	if rest, ok := strings.CutPrefix(line, "fil "); ok {
		line = rest
	}
	var args []string
	var cur strings.Builder
	inWord, quote := false, rune(0)
	for _, r := range line {
		switch {
		case quote != 0 && r == quote:
			quote = 0
		case quote != 0:
			cur.WriteRune(r)
		case r == '"' || r == '\'':
			quote, inWord = r, true
		case unicode.IsSpace(r):
			if inWord {
				args = append(args, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, errors.New("a quote was opened and not closed")
	}
	if inWord {
		args = append(args, cur.String())
	}
	if len(args) == 0 {
		return nil, errors.New("nothing to run")
	}
	return args, nil
}

// withPick asks list, search and neighbours to show their papers in the
// picker, unless the line wants something else: JSON, help, or already says.
func withPick(args []string) []string {
	switch args[0] {
	case "list", "search", "neighbours":
	default:
		return args
	}
	for _, a := range args[1:] {
		switch a {
		case "--json", "--help", "-h", "--context", "--pick", "--pick=false", "--pick=true":
			return args
		}
	}
	return append(args, "--pick")
}

// remember adds line to the end of history, moving it there if it was
// already in, and keeps the last historyMax.
func remember(history []string, line string) []string {
	out := history[:0:0]
	for _, h := range history {
		if h != line {
			out = append(out, h)
		}
	}
	out = append(out, line)
	if len(out) > historyMax {
		out = out[len(out)-historyMax:]
	}
	return out
}

func (a *app) historyPath() string {
	dir, err := a.resolvedCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "session-history")
}

func (a *app) loadHistory() []string {
	path := a.historyPath()
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// saveHistory writes the history; a failure only costs the next session its
// history, so it is not reported.
func (a *app) saveHistory(history []string) {
	path := a.historyPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	os.WriteFile(path, []byte(strings.Join(history, "\n")+"\n"), 0o600)
}

// sessionCommand is one command the prompt offers.
type sessionCommand struct {
	name, short string
}

// sessionCommands are the commands a session can run, from the command line's
// own definitions, plus the session's own.
func (a *app) sessionCommands() []sessionCommand {
	var out []sessionCommand
	for _, c := range a.rootCommand().Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" || c.Name() == "mcp" {
			continue
		}
		out = append(out, sessionCommand{c.Name(), c.Short})
	}
	out = append(out, sessionCommand{"help", "What a session can do"}, sessionCommand{"clear", "Clear the screen"},
		sessionCommand{"exit", "Leave fil"})
	sort.SliceStable(out, func(i, j int) bool { return order(out[i].name) < order(out[j].name) })
	return out
}

// order puts the commands people use most first in the menu.
func order(name string) int {
	for i, n := range []string{"search", "list", "show", "add", "expand", "fetch", "neighbours", "path", "stats", "import", "extract"} {
		if n == name {
			return i
		}
	}
	return 100
}

// printBanner shows what fil is and what the library holds.
func (a *app) printBanner(ctx context.Context) {
	p := a.outPaint
	var lines []string
	lines = append(lines, p.title("Filiation")+" "+p.dim(Version))
	cfg, err := a.loadConfig()
	switch {
	case err != nil:
		lines = append(lines, p.warn(err.Error()))
	case cfg.FirstRun:
		lines = append(lines, "No library yet. Start one with "+p.accent("/add 10.7717/peerj.4375")+", or any DOI or title.")
	default:
		lines = append(lines, a.libraryLine(ctx, cfg.Name))
	}
	lines = append(lines, p.dim("Type / for the commands, help for more, exit to leave."))

	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	if p.on {
		box = box.BorderForeground(lipgloss.Color(p.p.accent.hexString()))
	}
	fmt.Fprintln(a.raw(), box.Render(strings.Join(lines, "\n")))
	fmt.Fprintln(a.raw())
}

// libraryLine is the library's name and numbers, or why they are missing.
func (a *app) libraryLine(ctx context.Context, name string) string {
	p := a.outPaint
	if name == "" {
		name = "Your library"
	}
	lib, err := a.openExisting(ctx)
	if err != nil {
		return name
	}
	defer lib.Close()
	parts := []string{p.title(name)}
	if s, err := lib.Summarise(ctx); err == nil {
		parts = append(parts, p.num(thousands(s.Works))+" works", p.num(thousands(s.Fetched))+" fetched")
	}
	if n, err := lib.FetchedOnDisk(ctx); err == nil {
		parts = append(parts, p.num(thousands(n))+" PDFs")
	}
	if c, err := lib.TextCounts(ctx); err == nil {
		parts = append(parts, p.num(thousands(c["ok"]))+" searchable")
	}
	return strings.Join(parts, p.dim(" · "))
}

func (a *app) printSessionHelp() {
	p := a.outPaint
	w := a.raw()
	fmt.Fprintln(w, "Type any fil command, with or without the leading /. For example:")
	fmt.Fprintln(w)
	for _, ex := range []string{"search citation advantage", "list --with-pdf", "show F45ZSWF64", "add 10.7717/peerj.4375",
		"expand --max-nodes 100", "fetch --max 20", "neighbours F45ZSWF64 --context"} {
		fmt.Fprintf(w, "  %s %s\n", p.accent("›"), ex)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Quote a phrase: search \"green open access\". Add --help to any command for its options.")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s  Tab completes · ↑ ↓ history and menu · Esc clears · Ctrl+C stops a command, or clears the line · exit leaves\n", p.label("Keys:"))
}

// hexString is the colour as lipgloss takes it.
func (c rgb) hexString() string { return fmt.Sprintf("#%02X%02X%02X", c.r, c.g, c.b) }
