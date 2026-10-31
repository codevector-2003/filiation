package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/blobs"
	"github.com/codevector-2003/filiation/internal/library"
)

func (a *app) importCommand() *cobra.Command {
	var as, local string
	var year int
	var acceptFirst bool
	var searches int
	cmd := &cobra.Command{
		Use:   "import [file…]",
		Short: "Identify PDFs you already have and file them in your library",
		Long: "Identify PDFs you already have and file them under the right paper.\n\n" +
			"With no files, fil imports everything in Inbox/ and any PDF in papers/ that is\n" +
			"not yet a paper's. A PDF from Inbox/ moves into its paper's folder; one already\n" +
			"in papers/ stays where you put it; one from anywhere else is copied, and the\n" +
			"original is never touched.\n\n" +
			"fil looks for an identifier in the PDF — the arXiv stamp, the file name, the\n" +
			"DOI in the page header, the PDF's metadata — and trusts it only if the paper it\n" +
			"names has its title and first author in the PDF. Failing that, it searches by\n" +
			"the title on page 1 (10 of OpenAlex's 1,000 free daily credits each, at most\n" +
			"--searches a run) and asks you to choose; it never picks on its own unless you\n" +
			"pass --accept-first, and then only a match it could check.\n\n" +
			"Name a paper yourself with --as, or keep a PDF OpenAlex does not know as a\n" +
			"local document with --local. fil fetch and fil extract also file what is in\n" +
			"Inbox/ whenever its identifier checks out, so dropping a PDF there is enough.",
		Example: "  fil import\n" +
			"  fil import ~/Downloads/paper.pdf\n" +
			"  fil import Inbox/scan.pdf --as 10.7717/peerj.4375\n" +
			"  fil import notes.pdf --local \"Field notes on solitary bees\" --year 2025",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case as != "" && local != "":
				return usageError{cmd: cmd, err: errors.New("give --as or --local, not both")}
			case (as != "" || local != "") && len(args) != 1:
				return usageError{cmd: cmd, err: errors.New("--as and --local name one file")}
			case searches < 0:
				return usageError{cmd: cmd, err: errors.New("--searches must not be negative")}
			}
			var y *int
			if cmd.Flags().Changed("year") {
				y = &year
			}
			return a.importFiles(cmd.Context(), args, as, local, y, acceptFirst, searches, cmd.Flags().Changed("searches"))
		},
	}
	cmd.Flags().StringVar(&as, "as", "", "file the PDF as this paper (DOI, arXiv ID, OpenAlex ID or PMID)")
	cmd.Flags().StringVar(&local, "local", "", "keep the PDF as a local document with this title")
	cmd.Flags().IntVar(&year, "year", 0, "the year of a --local document")
	cmd.Flags().BoolVar(&acceptFirst, "accept-first", false, "file a title search's best match without asking, if its title and author are in the PDF")
	cmd.Flags().IntVar(&searches, "searches", library.DefaultTitleSearches, "most title searches to spend (10 credits each)")
	return cmd
}

func (a *app) importFiles(ctx context.Context, files []string, as, local string, year *int, acceptFirst bool, searches int, searchesSet bool) error {
	var lib *library.Library
	var err error
	if len(files) == 0 {
		lib, err = a.openExisting(ctx)
	} else {
		lib, err = a.openForFiles(ctx, files)
	}
	if err != nil {
		return err
	}
	defer lib.Close()

	switch {
	case as != "":
		f, err := lib.ImportAs(ctx, files[0], as, false)
		if err != nil {
			return a.explainImport(err)
		}
		if f.Outcome == library.ImportUnconfirmed && a.interactive && a.confirm(f) {
			if f, err = lib.ImportAs(ctx, files[0], as, true); err != nil {
				return a.explainImport(err)
			}
		}
		a.printImported(lib, f)
		return outcomeCode(f)
	case local != "":
		f, err := lib.ImportLocal(ctx, files[0], local, year)
		if err != nil {
			return a.explainImport(err)
		}
		a.printImported(lib, f)
		return outcomeCode(f)
	}

	opts := library.ImportOptions{Files: files, Again: true, AcceptFirst: acceptFirst, TitleSearches: searches}
	if searchesSet && searches == 0 {
		opts.TitleSearches = -1
	}
	if a.interactive {
		opts.Progress = func(f library.ImportedFile) {
			a.setStatus(short(filepath.Base(f.File)))
		}
	}
	res, runErr := lib.Import(ctx, opts)
	if a.interactive && len(res.Files) > 0 {
		a.endStatus()
	}
	if errors.Is(runErr, library.ErrNoFolder) {
		return a.explainNoFolder(runErr)
	}

	w := a.stdout
	if len(res.Files) == 0 && runErr == nil {
		fmt.Fprintf(w, "Nothing to import: %s is empty, and every PDF in papers/ is a paper's.\n",
			filepath.Join(lib.Folder(), blobs.InboxDir))
		return nil
	}
	imported, waiting := 0, 0
	for _, f := range res.Files {
		// Choosing is the researcher's job (ADR-005): ask, when someone is
		// there to answer.
		if a.interactive && len(f.Candidates) > 0 &&
			(f.Outcome == library.ImportCandidates || f.Outcome == library.ImportUnconfirmed) {
			if chosen, ok := a.chooseImport(ctx, lib, f); ok {
				f = chosen
			}
		}
		a.printImported(lib, f)
		if f.Outcome == library.ImportImported {
			imported++
		} else if f.Outcome != library.ImportDuplicate {
			waiting++
		}
	}
	fmt.Fprintf(w, "\nImported: %s", plural(imported, "PDF", "PDFs"))
	if waiting > 0 {
		fmt.Fprintf(w, "; %s not filed yet", plural(waiting, "is", "are"))
	}
	if res.Searches > 0 {
		fmt.Fprintf(w, "; %s searched (%d credits)", plural(res.Searches, "title", "titles"), 10*res.Searches)
	}
	fmt.Fprintln(w, ".")
	if waiting > 0 {
		fmt.Fprintf(w, "Why each is waiting: %s\n", filepath.Join(lib.Folder(), blobs.InboxDir, blobs.ReportName))
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			fmt.Fprintln(w, "\nStopped. Every PDF filed so far stays filed; run fil import to continue.")
			return errCancelled
		}
		return runErr
	}
	return nil
}

// openForFiles opens the library to import files into, making it on a first
// run as fil add does: importing the PDFs you have is a way to start. A
// file that is not there is refused first, so a typo creates nothing.
func (a *app) openForFiles(ctx context.Context, files []string) (*library.Library, error) {
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			return nil, fmt.Errorf("import %s: %w", f, err)
		}
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.FirstRun {
		if err := a.firstRun(cfg); err != nil {
			return nil, err
		}
	}
	return library.Open(ctx, cfg, a.libOptions())
}

// printImported is one file's outcome, in two or three lines.
func (a *app) printImported(lib *library.Library, f library.ImportedFile) {
	w := a.stdout
	name := filepath.Base(f.File)
	switch f.Outcome {
	case library.ImportImported:
		fmt.Fprintf(w, "✓ %s\n  %s  %s\n  filed at %s", name, headline(f.Work), f.Work.FilID, f.Saved)
		if f.Clue != "" {
			fmt.Fprintf(w, " — identified by %s", f.Clue)
		}
		fmt.Fprintln(w)
		if f.Text != nil {
			fmt.Fprint(w, "  ")
			a.printPaperText(*f.Text)
		}
	case library.ImportDuplicate:
		fmt.Fprintf(w, "= %s\n  %s\n", name, f.Reason)
	default:
		fmt.Fprintf(w, "· %s\n  not filed: %s\n", name, f.Reason)
		if !a.interactive {
			for i, c := range f.Candidates {
				if i == 3 {
					break
				}
				mark := ""
				if c.Verified {
					mark = " (title and author found in the PDF)"
				}
				fmt.Fprintf(w, "    %s%s\n      fil import %q --as %s\n", headline(c.Work), mark, f.File, c.Work.OpenAlexID)
			}
		}
	}
}

// chooseImport asks which candidate a PDF is, or whether to keep it as a
// local document. False leaves it where it is.
func (a *app) chooseImport(ctx context.Context, lib *library.Library, f library.ImportedFile) (library.ImportedFile, bool) {
	w := a.stdout
	fmt.Fprintf(w, "\n%s — %s. Which paper is it?\n\n", filepath.Base(f.File), f.Reason)
	for i, c := range f.Candidates {
		mark := " "
		if c.Verified {
			mark = "✓"
		}
		fmt.Fprintf(w, "  %2d. %s %s\n         %s\n", i+1, mark, headline(c.Work), identifiers(c.Work))
	}
	fmt.Fprintln(w, "\n  ✓ its title and first author are in the PDF")
	for {
		fmt.Fprintf(w, "Choose 1–%d, l to keep it as a local document, or Enter to leave it: ", len(f.Candidates))
		line, err := a.readLine()
		switch {
		case line == "":
			fmt.Fprintln(w)
			return f, false
		case strings.EqualFold(line, "l"):
			fmt.Fprint(w, "Its title: ")
			title, _ := a.readLine()
			if title == "" {
				return f, false
			}
			got, err := lib.ImportLocal(ctx, f.File, title, nil)
			if err != nil {
				fmt.Fprintf(a.stderr, "fil: %v\n", err)
				return f, false
			}
			return got, true
		}
		if n, convErr := strconv.Atoi(line); convErr == nil && n >= 1 && n <= len(f.Candidates) {
			got, err := lib.ImportChosen(ctx, f.File, f.Candidates[n-1])
			if err != nil {
				fmt.Fprintf(a.stderr, "fil: %v\n", err)
				return f, false
			}
			return got, true
		}
		if err != nil {
			return f, false
		}
		fmt.Fprintf(w, "%q is not one of the choices above.\n", line)
	}
}

// confirm asks whether to file a PDF as a paper whose title is not in it.
func (a *app) confirm(f library.ImportedFile) bool {
	if len(f.Candidates) == 0 {
		return false
	}
	fmt.Fprintf(a.stdout, "%s: the title of %s is not in the PDF.\nFile it as that paper anyway? [y/N] ",
		filepath.Base(f.File), headline(f.Candidates[0].Work))
	line, _ := a.readLine()
	return strings.EqualFold(line, "y") || strings.EqualFold(line, "yes")
}

func outcomeCode(f library.ImportedFile) error {
	if f.Outcome == library.ImportImported || f.Outcome == library.ImportDuplicate {
		return nil
	}
	return exitCode(exitFailure)
}

func (a *app) explainImport(err error) error {
	if errors.Is(err, library.ErrNoFolder) {
		return a.explainNoFolder(err)
	}
	return err
}
