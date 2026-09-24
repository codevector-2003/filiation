package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/library"
)

func (a *app) statsCommand() *cobra.Command {
	var listDuplicates bool
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Summarise your library",
		Long: "Count what your library holds: papers you added, works fetched and still to\n" +
			"fetch, citations, and how many works have no reference list.\n\n" +
			"It also reports works that share a title — usually one paper that OpenAlex\n" +
			"keeps as two records. fil lists them but never merges them on its own: a book\n" +
			"review, for one, carries the title of the book it reviews.",
		Example: "  fil stats\n  fil stats --duplicates",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.stats(cmd.Context(), listDuplicates)
		},
	}
	cmd.Flags().BoolVar(&listDuplicates, "duplicates", false, "list works that share a title")
	return cmd
}

func (a *app) stats(ctx context.Context, listDuplicates bool) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	if cfg.FirstRun {
		fmt.Fprintln(a.stdout, "Your library is empty. Add a paper first, for example:\n\n  fil add 10.7717/peerj.4375")
		return nil
	}
	lib, err := library.Open(ctx, cfg, library.Options{CacheDir: a.cacheDir, NoCache: a.noCache, Transport: a.transport})
	if err != nil {
		return err
	}
	defer lib.Close()

	s, err := lib.Summarise(ctx)
	if err != nil {
		return err
	}
	dups, err := lib.ProbableDuplicates(ctx)
	if err != nil {
		return err
	}

	w := a.stdout
	fmt.Fprintf(w, "Library:    %s\n", lib.Path())
	fmt.Fprintf(w, "Seeds:      %s you added\n", thousands(s.Seeds))
	fmt.Fprintf(w, "Works:      %s — %s fetched, %s still to fetch",
		thousands(s.Works), thousands(s.Fetched), thousands(s.Stubs-s.Unresolved))
	if s.Unresolved > 0 {
		fmt.Fprintf(w, ", %s not in OpenAlex", thousands(s.Unresolved))
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Citations:  %s\n", thousands(s.Citations))
	if s.Fetched > 0 {
		withRefs := s.Fetched - s.DeadEnds
		fmt.Fprintf(w, "Coverage:   %s of %s fetched works have a reference list (%.0f%%)\n",
			thousands(withRefs), thousands(s.Fetched), 100*float64(withRefs)/float64(s.Fetched))
	}

	switch {
	case len(dups) == 0:
		fmt.Fprintln(w, "Duplicates: none found by title")
	case !listDuplicates:
		fmt.Fprintf(w, "Duplicates: %s that more than one fetched work shares — probably the same\n"+
			"            paper recorded twice by OpenAlex. fil does not merge these on its own;\n"+
			"            run fil stats --duplicates to see them.\n",
			plural(len(dups), "title", "titles"))
	default:
		fmt.Fprintf(w, "Duplicates: %s that more than one fetched work shares. A shared title is\n"+
			"            not proof — compare the years and types — so fil lists them and\n"+
			"            merges nothing.\n\n", plural(len(dups), "title", "titles"))
		for _, g := range dups {
			fmt.Fprintf(w, "  %s\n", g.Title)
			for _, work := range g.Works {
				year := "—"
				if work.Year != nil {
					year = strconv.Itoa(*work.Year)
				}
				doi := ""
				if work.DOI != nil {
					doi = "doi:" + *work.DOI
				}
				fmt.Fprintf(w, "    %-12s %s  %-16s %s\n", work.OpenAlexID, year, work.Type, doi)
			}
			fmt.Fprintln(w)
		}
	}
	return nil
}
