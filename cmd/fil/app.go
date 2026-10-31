package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

// app is everything a command touches outside its arguments, gathered so that
// a test can run the real CLI against buffers, a temporary config directory and
// recorded OpenAlex responses.
type app struct {
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	interactive bool

	// configDir overrides the per-user config directory; empty in production.
	configDir string
	// cacheDir overrides the per-user cache directory; empty in production.
	cacheDir string
	// documentsDir overrides the user's Documents folder, under which a first
	// library is proposed; empty in production.
	documentsDir string
	// transport replaces the network; nil in production.
	transport http.RoundTripper
	// pdfTransport replaces the network for PDF downloads; nil in production.
	pdfTransport http.RoundTripper

	lines *bufio.Reader

	// stdoutFile and stderrFile are the real files behind stdout and stderr,
	// which decide whether output is a terminal and gets colour; nil in tests.
	stdoutFile, stderrFile *os.File
	// outPaint and errPaint colour stdout and stderr; plain unless a terminal.
	outPaint, errPaint paint
	// rawStdout is stdout before styling, for output a program reads: --json,
	// GraphML.
	rawStdout io.Writer
	status    status
	// prompter reads a session's next line; nil means the terminal prompt.
	// Tests feed lines through it.
	prompter func(history []string) (string, bool, error)

	// flags shared by every command
	dbFlag      string
	libraryFlag string
	noCache     bool
	plain       bool

	// configPath is the file to name when the fix is to edit it. Set as soon
	// as config has been located, so an error after that point can point at it.
	configPath string
}

// run executes one command line and returns the exit code.
func (a *app) run(ctx context.Context, args []string) int {
	root := a.rootCommand()
	root.SetArgs(args)
	root.SetOut(a.stdout)
	root.SetErr(a.stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		var usage usageError
		if !errors.As(err, &usage) && strings.HasPrefix(err.Error(), "unknown command") {
			// cobra reports an unknown subcommand as a plain error.
			usage = usageError{cmd: root, err: err}
		}
		if usage.err != nil {
			fmt.Fprintf(a.stderr, "fil: %v\n\n", err)
			if usage.cmd != nil {
				fmt.Fprint(a.stderr, usage.cmd.UsageString())
			}
			return exitInvalidInput
		}
		if errors.Is(err, errCancelled) {
			return exitCancelled
		}
		var code exitCode
		if errors.As(err, &code) {
			return int(code)
		}
		return report(a.stderr, err, a.configPath)
	}
	return exitOK
}

// usageError marks a mistake in how a command was called, as opposed to a
// failure while running it, so only the former prints usage.
type usageError struct {
	cmd *cobra.Command
	err error
}

func (e usageError) Error() string { return e.err.Error() }

// noArgs is cobra.NoArgs reported as a usage mistake, so it prints usage and
// exits with exitInvalidInput like every other one.
func noArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return usageError{cmd: cmd, err: err}
	}
	return nil
}

func (a *app) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "fil",
		Short: "A citation graph and retrieval engine you run yourself",
		Long: "fil builds a map of what cites what, starting from papers you give it,\n" +
			"and keeps it in a library on your own machine.\n\n" +
			"Run fil on its own in a terminal to keep it open: a session that runs one\n" +
			"command after another, with / for the list of commands.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return usageError{cmd: cmd, err: err}
	})
	root.PersistentFlags().StringVar(&a.libraryFlag, "library", "", "use this library, by name or folder, instead of the default")
	root.PersistentFlags().StringVar(&a.dbFlag, "db", "", "use the database at this path, with no library folder")
	root.PersistentFlags().BoolVar(&a.noCache, "no-cache", false, "fetch from OpenAlex even if a response is cached")
	root.PersistentFlags().BoolVar(&a.plain, "plain", false, "no colour, even in a terminal")
	root.PersistentPreRun = func(*cobra.Command, []string) { a.setUpColour() }

	root.AddCommand(a.addCommand(), a.expandCommand(), a.neighboursCommand(), a.pathCommand(),
		a.statsCommand(), a.listCommand(), a.showCommand(), a.fetchCommand(), a.importCommand(), a.extractCommand(), a.searchCommand(), a.exportCommand(), a.libraryCommand(), a.mcpCommand(), a.whereCommand(),
		a.versionCommand(), a.cacheCommand(), a.themeCommand())
	return root
}

// raw is stdout without colour, for output a program reads.
func (a *app) raw() io.Writer {
	if a.rawStdout != nil {
		return a.rawStdout
	}
	return a.stdout
}

// setUpColour decides, once flags are read, whether stdout and stderr get the
// theme's colours, and routes stdout through the styling. It does nothing
// for output that is not a terminal, which is every test.
func (a *app) setUpColour() {
	if a.rawStdout != nil {
		return // already set up: a session runs many commands
	}
	a.rawStdout = a.stdout
	if a.stdoutFile == nil && a.stderrFile == nil {
		return
	}
	cfg := a.themeSettings()
	a.outPaint = paintFor(a.stdoutFile, a.plain, cfg.Theme, cfg.ThemeBackground)
	a.errPaint = paintFor(a.stderrFile, a.plain, cfg.Theme, cfg.ThemeBackground)
	if a.outPaint.on {
		a.stdout = styled{w: a.stdout, p: a.outPaint}
		titles = a.outPaint
	}
	if a.errPaint.on {
		a.stderr = styled{w: a.stderr, p: a.errPaint}
	}
}

func (a *app) addCommand() *cobra.Command {
	var acceptFirst bool
	var limit int
	cmd := &cobra.Command{
		Use:   "add <DOI | arXiv ID | PMID | OpenAlex ID | link | title>",
		Short: "Add a paper and record everything it cites",
		Long: "Add a paper to your library as a seed. Its references are recorded as works\n" +
			"to fetch later, with an edge for each citation.\n\n" +
			"A title can match several papers, so fil lists them and asks which one you\n" +
			"mean. In a script, pass --accept-first to take the closest match.",
		Example: "  fil add 10.7717/peerj.4375\n" +
			"  fil add https://arxiv.org/abs/1706.03762\n" +
			"  fil add Attention is all you need",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{cmd: cmd, err: errors.New("add needs a paper to add")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// A title may be typed without quotes; the words are one input.
			return a.add(cmd.Context(), strings.Join(args, " "), acceptFirst, limit)
		},
	}
	cmd.Flags().BoolVar(&acceptFirst, "accept-first", false, "for a title, take the closest match without asking")
	cmd.Flags().IntVar(&limit, "limit", 0, "for a title, how many candidates to offer (default 10)")
	return cmd
}

func (a *app) add(ctx context.Context, input string, acceptFirst bool, limit int) error {
	// Before anything else: a typo must not create a library on a first run.
	if err := library.Validate(input); err != nil {
		return err
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	if cfg.FirstRun {
		if err := a.firstRun(cfg); err != nil {
			return err
		}
	}

	lib, err := library.Open(ctx, cfg, a.libOptions())
	if err != nil {
		return err
	}
	defer lib.Close()

	res, err := lib.Add(ctx, input, library.AddOptions{AcceptFirst: acceptFirst, SearchLimit: limit})
	var amb *library.AmbiguousError
	if errors.As(err, &amb) {
		if !a.interactive {
			a.printCandidates(a.stderr, amb)
			return err
		}
		c, ok := a.choose(amb)
		if !ok {
			fmt.Fprintln(a.stdout, "Cancelled. Nothing was added.")
			return nil
		}
		res, err = lib.AddCandidate(ctx, c)
	}
	if err != nil {
		return err
	}

	a.printAdded(res)
	if s, err := lib.Stats(ctx); err == nil {
		fmt.Fprintf(a.stdout, "Library:    %s works (%s not fetched yet), %s citations.\n",
			thousands(s.Works), thousands(s.Stubs), thousands(s.Edges))
	}
	if q, ok := lib.Quota(); ok && q.Limit > 0 {
		fmt.Fprintf(a.stdout, "OpenAlex:   %s of %s credits left today.\n",
			thousands(q.Remaining), thousands(q.Limit))
	}
	return nil
}

func (a *app) printAdded(res library.Added) {
	w := res.Work
	if res.AlreadySeed {
		fmt.Fprintf(a.stdout, "Already in your library: %s\n", headline(w))
	} else {
		fmt.Fprintf(a.stdout, "Added: %s\n", headline(w))
	}
	if by := byline(w); by != "" {
		fmt.Fprintf(a.stdout, "       %s\n", by)
	}
	fmt.Fprintf(a.stdout, "       %s\n", identifiers(w))

	switch {
	case res.DeadEnd:
		// D12: say what this is before the user concludes the tool is broken.
		fmt.Fprint(a.stdout, "\nOpenAlex has no reference list for this work, so the graph cannot grow from\n"+
			"it. That is a gap in the data, not in fil: it is common for books, proceedings\n"+
			"volumes and editorials, and in arts and humanities it is most works.\n\n")
	case res.AlreadySeed:
		fmt.Fprintf(a.stdout, "References: %d, all already recorded.\n", res.Refs)
	default:
		fmt.Fprintf(a.stdout, "References: %d — %d new to your library, %d citations recorded.\n",
			res.Refs, res.NewStubs, res.NewEdges)
	}
}

// choose lists title candidates and reads the user's choice. Pressing Enter
// cancels; anything unreadable is asked again rather than guessed at.
func (a *app) choose(amb *library.AmbiguousError) (library.Candidate, bool) {
	a.printCandidates(a.stdout, amb)
	for {
		fmt.Fprintf(a.stdout, "Choose 1–%d, or press Enter to cancel: ", len(amb.Candidates))
		line, err := a.readLine()
		if line == "" {
			fmt.Fprintln(a.stdout)
			return library.Candidate{}, false
		}
		n, convErr := strconv.Atoi(line)
		if convErr == nil && n >= 1 && n <= len(amb.Candidates) {
			return amb.Candidates[n-1], true
		}
		if err != nil { // end of input with a bad answer: do not loop forever
			return library.Candidate{}, false
		}
		fmt.Fprintf(a.stdout, "%q is not one of the numbers above.\n", line)
	}
}

func (a *app) printCandidates(w io.Writer, amb *library.AmbiguousError) {
	fmt.Fprintf(w, "%q matches %d works:\n\n", amb.Title, len(amb.Candidates))
	anyMatch := false
	for i, c := range amb.Candidates {
		mark := " "
		if c.TitleMatch {
			mark, anyMatch = "*", true
		}
		fmt.Fprintf(w, "  %2d. %s %s\n", i+1, mark, headline(c.Work))
		fmt.Fprintf(w, "         %s\n", identifiers(c.Work))
	}
	if anyMatch {
		fmt.Fprintln(w, "\n  * the title matches what you typed")
	}
	fmt.Fprintln(w)
}

func (a *app) whereCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "where",
		Short: "Print where your library, settings and cache live",
		Long: "Print where fil keeps things. The default differs on every operating system,\n" +
			"so this is how to find your own data.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := a.loadConfig()
			if err != nil {
				return err
			}
			note := ""
			if cfg.FirstRun {
				note = "  (not created yet — fil add creates it)"
			}
			if cfg.Folder != "" {
				fmt.Fprintf(a.stdout, "Library:  %s%s\n", cfg.Folder, note)
				note = ""
			}
			fmt.Fprintf(a.stdout, "Database: %s%s\n", cfg.DBPath, note)
			fmt.Fprintf(a.stdout, "Settings: %s\n", cfg.ConfigPath)
			if dir, err := a.resolvedCacheDir(); err == nil {
				fmt.Fprintf(a.stdout, "Cache:    %s\n", dir)
			}
			return nil
		},
	}
}

func (a *app) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  noArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(a.stdout, "fil %s (%s/%s, %s)\n", Version, runtime.GOOS, runtime.GOARCH, runtime.Version())
		},
	}
}

func (a *app) cacheCommand() *cobra.Command {
	cache := &cobra.Command{
		Use:   "cache",
		Short: "Manage the cache of OpenAlex responses",
	}
	cache.AddCommand(&cobra.Command{
		Use:   "clear",
		Short: "Delete every cached response",
		Long: "Delete every cached OpenAlex response. Your library is not touched.\n" +
			"Use it when you suspect a cached answer is stale.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := library.ClearCache(a.cacheDir)
			if err != nil {
				return err
			}
			fmt.Fprintf(a.stdout, "Cleared the cache at %s. Your library was not touched.\n", dir)
			return nil
		},
	})
	return cache
}

func (a *app) loadConfig() (*config.Config, error) {
	cfg, err := config.Load(config.Options{
		DBFlag:       a.dbFlag,
		LibraryFlag:  a.libraryFlag,
		Dir:          a.configDir,
		DocumentsDir: a.documentsDir,
	})
	if cfg != nil {
		a.configPath = cfg.ConfigPath
	} else if errors.Is(err, errs.ErrInvalidConfig) {
		// Load failed on the file itself; name it so the user can fix it.
		if dir := a.configDir; dir != "" {
			a.configPath = filepath.Join(dir, config.FileName)
		} else if dir, derr := config.DefaultDir(); derr == nil {
			a.configPath = filepath.Join(dir, config.FileName)
		}
	}
	return cfg, err
}

// libOptions are the library options every command opens with: the cache,
// and in tests the replayed OpenAlex and the local PDF hosts.
func (a *app) libOptions() library.Options {
	return library.Options{CacheDir: a.cacheDir, NoCache: a.noCache, Transport: a.transport, PDFTransport: a.pdfTransport}
}

// firstRun offers a choice of library folder and records it (ADR-011). The
// config package never prompts, because it also serves an MCP server and a web
// handler; asking is the front door's job.
func (a *app) firstRun(cfg *config.Config) error {
	if a.interactive && a.dbFlag == "" {
		fmt.Fprintf(a.stdout, "Welcome to fil. Your library will be a folder at:\n\n  %s\n\n"+
			"Your papers go in its papers/ folder, where you can see and organise them.\n\n", cfg.Folder)
		fmt.Fprint(a.stdout, "Press Enter to accept, or type another folder: ")
		line, _ := a.readLine()
		if line != "" {
			if err := cfg.UseFolder(line); err != nil {
				return fmt.Errorf("library location %q: %w", line, err)
			}
		}
		fmt.Fprintln(a.stdout)
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	where := cfg.DBPath
	if cfg.Folder != "" {
		where = cfg.Folder
	}
	fmt.Fprintf(a.stderr, "Created your library at %s\nSettings are in %s\n", where, cfg.ConfigPath)
	if cfg.Cloud != "" {
		fmt.Fprintf(a.stderr, "%s syncs that folder, so the database is kept at %s instead:\n"+
			"sync tools corrupt live databases. Your papers sync as usual.\n", cfg.Cloud, cfg.DBPath)
	}
	for _, w := range cfg.Warnings() {
		fmt.Fprintf(a.stderr, "note: %s\n", w)
	}
	fmt.Fprintln(a.stderr)
	return nil
}

func (a *app) resolvedCacheDir() (string, error) {
	if a.cacheDir != "" {
		return a.cacheDir, nil
	}
	return config.DefaultCacheDir()
}

// readLine reads one trimmed line. At end of input it returns what it has and
// io.EOF, so a script piping nothing in cannot hang a prompt.
func (a *app) readLine() (string, error) {
	if a.lines == nil {
		a.lines = bufio.NewReader(a.stdin)
	}
	line, err := a.lines.ReadString('\n')
	return strings.TrimSpace(line), err
}

// titles makes paper titles stand out where fil prints them. setUpColour
// gives it stdout's paint; it is plain everywhere else, tests included.
var titles paint

// headline is a work as one line: title, year and type.
func headline(w model.Work) string {
	var extra []string
	if w.Year != nil {
		extra = append(extra, strconv.Itoa(*w.Year))
	}
	if w.Type != model.TypeUnknown {
		extra = append(extra, string(w.Type))
	}
	if len(extra) == 0 {
		return titles.title(w.DisplayTitle())
	}
	return fmt.Sprintf("%s (%s)", titles.title(w.DisplayTitle()), strings.Join(extra, ", "))
}

// identifiers is the line under a headline: how to find the work again, and
// whether a free copy exists.
// byline names the first three authors and counts the rest, or returns "" when
// no authors are recorded — a stub, or a work fetched before v0.2.
func byline(w model.Work) string {
	const shown = 3
	if len(w.Authors) == 0 {
		return ""
	}
	names := make([]string, 0, shown)
	for _, a := range w.Authors[:min(len(w.Authors), shown)] {
		names = append(names, a.Name)
	}
	s := "by " + strings.Join(names, ", ")
	switch more := len(w.Authors) - len(names); more {
	case 0:
	case 1:
		s += " and 1 other"
	default:
		s += fmt.Sprintf(" and %d others", more)
	}
	return s
}

// workIDs names a work by both its IDs, fil ID first: the one that never
// changes, then the one the rest of the world uses. A local document has only
// the first (D17).
func workIDs(w model.Work) string {
	if w.OpenAlexID == "" {
		return w.FilID
	}
	if w.FilID == "" {
		return w.OpenAlexID
	}
	return w.FilID + " " + w.OpenAlexID
}

func identifiers(w model.Work) string {
	parts := []string{workIDs(w)}
	if w.DOI != nil {
		parts = append(parts, "doi:"+*w.DOI)
	}
	if w.ArXivID != nil {
		parts = append(parts, "arXiv:"+*w.ArXivID)
	}
	if w.Venue != "" {
		parts = append(parts, w.Venue)
	}
	switch {
	case w.OAStatus.IsOpen():
		parts = append(parts, string(w.OAStatus)+" open access")
	case w.OAStatus == model.OAClosed:
		parts = append(parts, "closed access")
	}
	return strings.Join(parts, " · ")
}

// thousands formats n with separators, because "12345 works" is harder to read
// at a glance than "12,345 works".
func thousands(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + thousands(-n)
	}
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
