package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/library"
)

func (a *app) libraryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "library",
		Short: "Show your library folder, or make, move and scan one",
		Long: "A library is a folder you can see: papers/ holds the PDFs, one folder per\n" +
			"paper with a readable name; Inbox/ and exports/ sit beside it. Organise\n" +
			"papers/ however you like — move, rename and group the folders. fil finds\n" +
			"them again by the fil ID at the end of each name, and each folder you make\n" +
			"becomes a collection.\n\n" +
			"Without a subcommand, shows where the library is and what is in papers/.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.showLibrary(cmd.Context())
		},
	}

	var makeDefault bool
	newCmd := &cobra.Command{
		Use:   "new <folder>",
		Short: "Make another library in a new folder",
		Long: "Make a new, empty library in a folder that does not exist yet or is empty.\n" +
			"Use it with --library <name>, or pass --default to open it from now on.",
		Example: "  fil library new \"~/Documents/Filiation/Side project\" --default",
		Args:    exactlyOne("new needs a folder"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.newLibrary(cmd.Context(), args[0], makeDefault)
		},
	}
	newCmd.Flags().BoolVar(&makeDefault, "default", false, "open this library when --library is not given")

	moveCmd := &cobra.Command{
		Use:   "move <folder>",
		Short: "Move your library to a new folder",
		Long: "Move the library to a new folder, which must not exist yet or be empty.\n\n" +
			"A library made by fil 0.1 or 0.2 is a single database file. move gives it a\n" +
			"library folder: the database is copied in and checked against the original,\n" +
			"which is left where it was for you to delete.\n\n" +
			"A library that already has a folder is moved as a whole. The new folder must\n" +
			"be on the same drive; to go further, use your file manager.\n\n" +
			"Close anything else using the library first, such as an MCP client.",
		Example: "  fil library move \"~/Documents/Filiation/My Library\"",
		Args:    exactlyOne("move needs a folder"),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.moveLibrary(cmd.Context(), args[0])
		},
	}

	scanCmd := &cobra.Command{
		Use:   "scan",
		Short: "Find papers you moved, renamed or deleted in papers/",
		Long: "Bring the library up to date with papers/: folders you moved or renamed are\n" +
			"found again, new files in a paper's folder are recorded, and folders you\n" +
			"deleted are marked missing. Commands that need it scan on their own; this\n" +
			"shows what changed. Nothing on disk is changed.",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.scanLibrary(cmd.Context())
		},
	}

	folderCmd := &cobra.Command{
		Use:   "folder <paper>",
		Short: "Print a paper's folder in papers/, making it if needed",
		Long: "Print the folder of a paper in your library, making it in papers/ if it has\n" +
			"none. Put the paper's PDF there, or anything else that belongs with it.",
		Example: "  fil library folder 10.7717/peerj.4375",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{cmd: cmd, err: errors.New("folder needs a paper")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.workFolder(cmd.Context(), strings.Join(args, " "))
		},
	}

	cmd.AddCommand(newCmd, moveCmd, scanCmd, folderCmd)
	return cmd
}

func exactlyOne(msg string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != 1 {
			return usageError{cmd: cmd, err: errors.New(msg + " — quote a path with spaces")}
		}
		return nil
	}
}

func (a *app) showLibrary(ctx context.Context) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	w := a.stdout
	if cfg.FirstRun {
		fmt.Fprintf(w, "No library yet. fil add makes one at:\n\n  %s\n", cfg.Folder)
		return nil
	}
	lib, err := library.Open(ctx, cfg, a.libOptions())
	if err != nil {
		return err
	}
	defer lib.Close()

	if cfg.Folder == "" {
		fmt.Fprintf(w, "Library:  %s\n\n", cfg.DBPath)
		fmt.Fprintln(w, "This library is a single database file, as fil 0.1 and 0.2 made them. Give it\n"+
			"a folder for your PDFs with:\n\n  fil library move \"<folder>\"")
		a.listOthers(cfg)
		return nil
	}
	fmt.Fprintf(w, "Library:  %s\n", cfg.Name)
	fmt.Fprintf(w, "Folder:   %s\n", cfg.Folder)
	fmt.Fprintf(w, "Database: %s\n", cfg.DBPath)
	if cfg.Cloud != "" {
		fmt.Fprintf(w, "          kept outside the folder, which %s syncs: sync tools corrupt live databases\n", cfg.Cloud)
	}
	r, err := lib.Scan(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\nPapers:   %s with a folder, %s\n",
		thousands(r.Folders), plural(r.Files, "file", "files"))
	if cs, err := lib.Collections(ctx); err == nil && len(cs) > 0 {
		var names []string
		for _, c := range cs {
			names = append(names, fmt.Sprintf("%s (%d)", c.Name, c.Works))
		}
		fmt.Fprintf(w, "Collections: %s\n", strings.Join(names, ", "))
	}
	a.printScanNotes(r)
	a.listOthers(cfg)
	return nil
}

// listOthers names the other libraries, so a person knows --library has
// somewhere to go.
func (a *app) listOthers(cfg *config.Config) {
	var others []string
	for _, l := range cfg.Libraries {
		if l.Folder != cfg.Folder {
			others = append(others, fmt.Sprintf("  %s  %s", l.Name, l.Folder))
		}
	}
	if len(others) > 0 {
		fmt.Fprintf(a.stdout, "\nOther libraries (open with --library <name>):\n%s\n", strings.Join(others, "\n"))
	}
}

func (a *app) newLibrary(ctx context.Context, dir string, makeDefault bool) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	made, err := library.Create(ctx, cfg, dir, makeDefault)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Made the library %q at %s\n", made.Name, made.Folder)
	if made.Cloud != "" {
		fmt.Fprintf(a.stdout, "Its database is kept at %s, outside the folder, because %s syncs it.\n", made.DBPath, made.Cloud)
	}
	if makeDefault {
		fmt.Fprintln(a.stdout, "fil opens it from now on.")
	} else {
		fmt.Fprintf(a.stdout, "Open it with --library %q. To open it by default, set default_library in\n%s\n",
			made.Name, made.ConfigPath)
	}
	return nil
}

func (a *app) moveLibrary(ctx context.Context, dir string) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	if cfg.FirstRun {
		fmt.Fprintln(a.stdout, "There is no library to move yet. fil add makes one.")
		return nil
	}
	r, err := library.Move(ctx, cfg, dir)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "Your library is now at %s\n", r.Folder)
	if r.Cloud != "" {
		fmt.Fprintf(a.stdout, "Its database is kept at %s, outside the folder, because %s syncs it.\n", r.Database, r.Cloud)
	}
	if r.OldDatabase != "" {
		fmt.Fprintf(a.stdout, "\nThe database was copied and checked. The original is still at\n  %s\n"+
			"Delete it, with its -wal and -shm files, once you are happy.\n", r.OldDatabase)
	}
	return nil
}

func (a *app) scanLibrary(ctx context.Context) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()
	r, err := lib.Scan(ctx)
	if err != nil {
		return a.explainNoFolder(err)
	}
	fmt.Fprintf(a.stdout, "Scanned %s: %s with a folder, %s, %s new.\n",
		filepath.Join(lib.Folder(), "papers"), thousands(r.Folders),
		plural(r.Files, "file", "files"), thousands(r.NewFiles))
	a.printScanNotes(r)
	return nil
}

func (a *app) printScanNotes(r library.ScanReport) {
	w := a.stdout
	for _, m := range r.Moved {
		how := "moved"
		if m.ByHash {
			how = "found by its files, without its fil ID"
		}
		fmt.Fprintf(w, "  %s: %s → %s\n", how, m.From, m.To)
	}
	for _, m := range r.Missing {
		fmt.Fprintf(w, "  missing: %s — the paper stays in your library, and fil will not download it again unasked\n", m)
	}
	if r.MissingFiles > 0 {
		fmt.Fprintf(w, "  %s missing in all.\n", plural(r.MissingFiles, "file is", "files are"))
	}
	for _, u := range r.Unknown {
		fmt.Fprintf(w, "  not in this library: %s — its fil ID names no paper here\n", u)
	}
	if n := len(r.Loose); n > 0 {
		fmt.Fprintf(w, "  %s in papers/ not yet matched to a paper; fil leaves them where they are.\n",
			plural(n, "PDF", "PDFs"))
	}
	for _, p := range r.LongPaths {
		fmt.Fprintf(w, "  long path, near Windows' limit of 260 characters: %s\n", p)
	}
}

func (a *app) workFolder(ctx context.Context, input string) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()
	f, err := lib.FolderFor(ctx, input)
	if err != nil {
		if errors.Is(err, library.ErrNoFolder) {
			return a.explainNoFolder(err)
		}
		return a.explainFind(err)
	}
	fmt.Fprintln(a.stdout, f.Path)
	switch {
	case f.Created:
		fmt.Fprintf(a.stderr, "Made a folder for %s\n", headline(f.Work))
	case f.Renamed != "":
		fmt.Fprintf(a.stderr, "Renamed from %s, now that fil knows more about the paper\n", f.Renamed)
	}
	for _, file := range f.Files {
		if file.Missing {
			fmt.Fprintf(a.stderr, "  missing: %s\n", filepath.Base(file.Path))
		}
	}
	if f.LongPath {
		fmt.Fprintln(a.stderr, "This path is near Windows' limit of 260 characters; a shorter folder above it would help.")
	}
	return nil
}

func (a *app) explainNoFolder(err error) error {
	if !errors.Is(err, library.ErrNoFolder) {
		return err
	}
	fmt.Fprintln(a.stderr, "fil: this library is a single database file, as fil 0.1 and 0.2 made them,\n"+
		"so it has no papers/ folder yet. Give it one with:\n\n  fil library move \"<folder>\"")
	return exitCode(exitFailure)
}
