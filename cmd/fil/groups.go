package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/library"
)

func (a *app) groupsCommand() *cobra.Command {
	var detail, collect string
	var all, fringe bool
	cmd := &cobra.Command{
		Use:   "groups [group]",
		Short: "Find groups of papers that cite one another",
		Long: "Find the topics in your library from its citations alone: papers that cite\n" +
			"one another more than they cite the rest. Each group is named after the paper\n" +
			"its members cite most. Nothing is filed until you ask.\n\n" +
			"Only papers linked to at least two others are grouped; a reference that one\n" +
			"paper cites says little about where it belongs. Where OpenAlex has few\n" +
			"reference lists, mostly in the humanities, the groups are fewer and smaller.\n\n" +
			"Name a group, by its number, its name or its paper's fil ID, to list its\n" +
			"papers; add --collect to file them in a new collection.",
		Example: "  fil groups\n  fil groups --detail fine\n  fil groups 2\n  fil groups 2 --collect \"Replication\"",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			which := ""
			if len(args) == 1 {
				which = args[0]
			}
			if collect != "" && which == "" {
				return usageError{cmd: cmd, err: errors.New("--collect needs the group to file, by its number or name")}
			}
			return a.groups(cmd.Context(), which, detail, collect, all, fringe)
		},
	}
	cmd.Flags().StringVar(&detail, "detail", "normal", "how finely to cut the groups: broad, normal or fine")
	cmd.Flags().StringVar(&collect, "collect", "", "file the group's papers in a new collection with this name")
	cmd.Flags().BoolVar(&fringe, "fringe", false, "with --collect, also file the papers linked to this group alone")
	cmd.Flags().BoolVar(&all, "all", false, "also list the groups marked not useful in the desktop app")
	return cmd
}

func (a *app) groups(ctx context.Context, which, detail, collect string, all, fringe bool) error {
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	if cfg.FirstRun {
		fmt.Fprintln(a.stdout, "Your library is empty. Add a paper first, for example:\n\n  fil add 10.7717/peerj.4375")
		return nil
	}
	lib, err := library.Open(ctx, cfg, a.libOptions())
	if err != nil {
		return err
	}
	defer lib.Close()
	w := a.stdout

	if which == "" {
		res, err := lib.CitationGroups(ctx, detail)
		if err != nil {
			return a.explainFind(err)
		}
		shown := 0
		for _, g := range res.Groups {
			if all || !g.Dismissed {
				shown++
			}
		}
		if len(res.Groups) == 0 {
			fmt.Fprintf(w, "No groups yet: %s of %s linked to at least two others.\n",
				thousands(res.Linked), plural(res.Total, "paper is", "papers are"))
			fmt.Fprintln(w, "Groups appear as the library grows: fil expand follows the references outward.\n"+
				"Where OpenAlex has few reference lists, mostly in the humanities, they stay few.")
			return nil
		}
		fmt.Fprintf(w, "%s at %s detail, among the %s linked to at least two others.\n",
			plural(len(res.Groups), "group", "groups"), res.Detail, plural(res.Linked, "paper", "papers"))
		if res.Fringe > 0 {
			fmt.Fprintf(w, "%s linked to only one other %s not grouped.\n", plural(res.Fringe, "paper", "papers"), isAre(res.Fringe))
		}
		fmt.Fprintln(w)
		n := 0
		for i, g := range res.Groups {
			if g.Dismissed && !all {
				continue
			}
			n++
			years := ""
			if g.YearFrom > 0 {
				years = fmt.Sprintf(", %d–%d", g.YearFrom, g.YearTo)
			}
			note := ""
			if g.Dismissed {
				note = "  (marked not useful)"
			}
			fmt.Fprintf(w, "%3d  %s%s\n     %s, %s fetched%s\n", i+1, g.Name, note,
				plural(len(g.Members), "paper", "papers"), thousands(g.Fetched), years)
			var titles []string
			for _, m := range g.Members[:min(3, len(g.Members))] {
				if m.Title != "" {
					titles = append(titles, m.Title)
				}
			}
			if len(titles) > 0 {
				fmt.Fprintf(w, "     %s\n", clip(strings.Join(titles, "; "), 100))
			}
		}
		if hidden := len(res.Groups) - shown; hidden > 0 {
			fmt.Fprintf(w, "\n%s marked not useful in the desktop app; --all lists them.\n", plural(hidden, "group", "groups"))
		}
		fmt.Fprintln(w, "\nfil groups <number> lists a group's papers; --collect files them in a collection.")
		return nil
	}

	g, err := lib.CitationGroup(ctx, which, detail)
	if errors.Is(err, library.ErrNoGroup) {
		fmt.Fprintf(a.stderr, "fil: no group %q at %s detail. fil groups lists them.\n", which, detail)
		return exitCode(exitNotFound)
	}
	if err != nil {
		return a.explainFind(err)
	}
	if collect != "" {
		id, err := lib.MakeGroupCollection(ctx, g.Anchor, detail, collect, 0, fringe)
		if err != nil {
			return a.explainFind(err)
		}
		n := len(g.Members)
		if fringe {
			n += len(g.Fringe)
		}
		fmt.Fprintf(w, "Filed %s in the new collection %q (number %d).\n", plural(n, "paper", "papers"), strings.TrimSpace(collect), id)
		return nil
	}
	fmt.Fprintf(w, "%s: %s, %s fetched\n\n", g.Name, plural(len(g.Members), "paper", "papers"), thousands(g.Fetched))
	for _, m := range g.Members {
		title := m.Title
		if title == "" {
			title = "(not fetched yet) " + m.OpenAlexID
		}
		year := "    "
		if m.Year > 0 {
			year = fmt.Sprint(m.Year)
		}
		fmt.Fprintf(w, "  %s  %s  %s  cited by %d here\n", m.FilID, year, clip(title, 70), g.CitedWithin[m.FilID])
	}
	if len(g.Fringe) > 0 {
		fmt.Fprintf(w, "\n%s linked to this group alone; --collect --fringe files them too.\n", plural(len(g.Fringe), "more paper is", "more papers are"))
	}
	return nil
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}
