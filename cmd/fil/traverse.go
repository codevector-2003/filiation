package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

// exitCode is an outcome already explained to the user: run() exits with it
// and prints nothing more.
type exitCode int

func (c exitCode) Error() string { return fmt.Sprintf("exit %d", int(c)) }

func (a *app) neighboursCommand() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:     "neighbours <paper>",
		Aliases: []string{"neighbors"},
		Short:   "Show what a paper cites and what in your library cites it",
		Long: "Show a paper's references and the papers in your library that cite it.\n\n" +
			"The paper must already be in your library; name it by DOI, arXiv ID, PMID,\n" +
			"OpenAlex ID, link or title. \"Cited by\" counts only papers in your library.",
		Example: "  fil neighbours 10.7717/peerj.4375\n  fil neighbours The state of OA --limit 0",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{cmd: cmd, err: errors.New("neighbours needs a paper")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.neighbours(cmd.Context(), strings.Join(args, " "), limit)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 15, "most works to list on each side; 0 lists all")
	return cmd
}

func (a *app) pathCommand() *cobra.Command {
	var maxHops int
	var anyDirection bool
	cmd := &cobra.Command{
		Use:   "path <paper> <paper>",
		Short: "Find the shortest chain of citations between two papers",
		Long: "Find the shortest chain of citations between two papers in your library.\n\n" +
			"By default fil looks for lineage: one paper reaching the other by following\n" +
			"references, so that one descends from the other. --any-direction also allows\n" +
			"steps where a paper is cited rather than citing.\n\n" +
			"Quote titles, since this command takes two papers.",
		Example: "  fil path 10.7717/peerj.4375 W1503178185\n" +
			"  fil path \"The state of OA\" \"Anatomy of green open access\" --any-direction",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return usageError{cmd: cmd, err: fmt.Errorf("path needs exactly two papers, got %d — quote titles", len(args))}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.path(cmd.Context(), args[0], args[1], maxHops, anyDirection)
		},
	}
	cmd.Flags().IntVar(&maxHops, "max-hops", library.DefaultMaxHops, "longest chain to look for, in citation steps")
	cmd.Flags().BoolVar(&anyDirection, "any-direction", false, "allow steps in either citation direction")
	return cmd
}

// openExisting opens the library for a read-only command, or explains that
// there is nothing to read yet without creating anything.
func (a *app) openExisting(ctx context.Context) (*library.Library, error) {
	cfg, err := a.loadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.FirstRun {
		fmt.Fprintln(a.stdout, "Your library is empty. Add a paper first, for example:\n\n  fil add 10.7717/peerj.4375")
		return nil, exitCode(exitOK)
	}
	return library.Open(ctx, cfg, library.Options{CacheDir: a.cacheDir, NoCache: a.noCache, Transport: a.transport})
}

func (a *app) neighbours(ctx context.Context, input string, limit int) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()

	n, err := lib.Neighbours(ctx, input)
	if err != nil {
		return a.explainFind(err)
	}

	w := a.stdout
	fmt.Fprintln(w, headline(n.Work))
	fmt.Fprintf(w, "       %s\n\n", identifiers(n.Work))

	fetched := 0
	for _, c := range n.Cites {
		if !c.IsStub() {
			fetched++
		}
	}
	switch {
	case n.Work.IsStub():
		fmt.Fprintln(w, "References: not known yet — this work has not been fetched. fil expand fetches it.")
	case len(n.Cites) == 0:
		fmt.Fprintln(w, "References: none in OpenAlex, so the graph cannot grow from here.")
	default:
		fmt.Fprintf(w, "Cites %s — %s fetched, %s known only by ID:\n",
			plural(len(n.Cites), "work", "works"), thousands(fetched), thousands(len(n.Cites)-fetched))
		listWorks(w, n.Cites, limit)
	}
	fmt.Fprintln(w)
	if len(n.CitedBy) == 0 {
		fmt.Fprintln(w, "Cited by: nothing in your library.")
	} else {
		fmt.Fprintf(w, "Cited by %s in your library:\n", plural(len(n.CitedBy), "work", "works"))
		listWorks(w, n.CitedBy, limit)
	}
	return nil
}

func listWorks(w io.Writer, works []model.Work, limit int) {
	shown := works
	if limit > 0 && len(works) > limit {
		shown = works[:limit]
	}
	for _, work := range shown {
		fmt.Fprintf(w, "  • %s  %s\n", headline(work), work.OpenAlexID)
	}
	if len(shown) < len(works) {
		fmt.Fprintf(w, "  … and %s more — pass --limit 0 to list all.\n", thousands(len(works)-len(shown)))
	}
}

func (a *app) path(ctx context.Context, from, to string, maxHops int, anyDirection bool) error {
	if maxHops < 1 {
		return usageError{err: errors.New("--max-hops must be at least 1")}
	}
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()

	p, err := lib.PathBetween(ctx, from, to, library.PathOptions{MaxHops: maxHops, AnyDirection: anyDirection})
	if err != nil {
		return a.explainFind(err)
	}

	w := a.stdout
	if !p.Found {
		fmt.Fprintf(w, "No chain of %s links these within %s:\n  %s\n  %s\n\n",
			map[bool]string{false: "references", true: "citations"}[anyDirection],
			plural(maxHops, "step", "steps"), headline(p.Works[0]), headline(p.Works[1]))
		if !anyDirection {
			fmt.Fprintln(w, "Neither descends from the other. --any-direction also follows citations the\n"+
				"other way; --max-hops searches further.")
		} else {
			fmt.Fprintln(w, "--max-hops searches further, and fil expand may connect them.")
		}
		return exitCode(exitFailure)
	}

	steps := len(p.Works) - 1
	allCite, allCited := true, true
	for _, c := range p.Cites {
		allCite = allCite && c
		allCited = allCited && !c
	}
	first, last := p.Works[0].DisplayTitle(), p.Works[steps].DisplayTitle()
	switch {
	case steps == 0:
		fmt.Fprintln(w, "Both name the same work:")
	case allCite:
		fmt.Fprintf(w, "%q descends from %q in %s:\n", short(first), short(last), plural(steps, "step", "steps"))
	case allCited:
		fmt.Fprintf(w, "%q descends from %q in %s:\n", short(last), short(first), plural(steps, "step", "steps"))
	default:
		fmt.Fprintf(w, "Connected in %s, through citations in both directions:\n", plural(steps, "step", "steps"))
	}
	fmt.Fprintln(w)
	for i, work := range p.Works {
		fmt.Fprintf(w, "  %s  %s\n", headline(work), work.OpenAlexID)
		if i < steps {
			if p.Cites[i] {
				fmt.Fprintln(w, "      cites")
			} else {
				fmt.Fprintln(w, "      is cited by")
			}
		}
	}
	return nil
}

// explainFind turns a failure to find a paper in the library into something a
// user can act on. Anything else passes through to report().
func (a *app) explainFind(err error) error {
	var amb *library.AmbiguousError
	if errors.As(err, &amb) {
		fmt.Fprintf(a.stderr, "%q matches %d works in your library:\n\n", amb.Title, len(amb.Candidates))
		for _, c := range amb.Candidates {
			fmt.Fprintf(a.stderr, "  • %s  %s\n", headline(c.Work), c.Work.OpenAlexID)
		}
		fmt.Fprintln(a.stderr, "\nName it by its OpenAlex ID or DOI instead.")
		return exitCode(exitAmbiguous)
	}
	if errors.Is(err, errs.ErrNotFound) {
		fmt.Fprintf(a.stderr, "fil: %v\n\nAdd it first with fil add, or see what is there with fil stats.\n", err)
		return exitCode(exitNotFound)
	}
	return err
}

// short trims a title for a one-line summary.
func short(title string) string {
	const limit = 60
	r := []rune(title)
	if len(r) <= limit {
		return title
	}
	return strings.TrimSpace(string(r[:limit])) + "…"
}
