package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/model"
)

func (a *app) matchCommand() *cobra.Command {
	var noRefs bool
	cmd := &cobra.Command{
		Use:   "match <fil ID> [DOI | arXiv ID | PMID | OpenAlex ID]",
		Short: "Find one of your own documents in OpenAlex",
		Long: "Link one of your own documents, one OpenAlex did not know when it came in, to\n" +
			"the paper OpenAlex knows it as. It keeps its fil ID, PDF, collections, labels\n" +
			"and notes, and gains its citation links.\n\n" +
			"With an identifier, it is looked up and linked; that is free. With only the\n" +
			"fil ID, its title is searched for and what OpenAlex finds is listed for you to\n" +
			"choose from. A title search uses 10 of OpenAlex's 1,000 free credits a day.\n\n" +
			"If your library has the paper already, as something your papers cite, say,\n" +
			"the two become one and the document's fil ID still finds it.",
		Example: "  fil match F7K2M9QXA\n" +
			"  fil match F7K2M9QXA W2122130843\n" +
			"  fil match F7K2M9QXA 10.7717/peerj.4375 --no-refs",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 || len(args) > 2 {
				return usageError{cmd: cmd, err: errors.New("give a fil ID, and the paper's identifier if you know it")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 2 {
				return a.link(cmd.Context(), args[0], args[1], !noRefs)
			}
			return a.match(cmd.Context(), args[0])
		},
	}
	cmd.Flags().BoolVar(&noRefs, "no-refs", false, "do not record what the paper cites")
	return cmd
}

func (a *app) match(ctx context.Context, input string) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()
	fmt.Fprintln(a.stderr, "Searching OpenAlex by title: 10 of the 1,000 free credits a day.")
	found, err := lib.TitleCandidates(ctx, input)
	if err != nil {
		return err
	}
	w := a.stdout
	if len(found) == 0 {
		fmt.Fprintln(w, "OpenAlex has nothing with that title. If you know its DOI, give it: fil match", input, "<DOI>")
		return nil
	}
	for _, c := range found {
		var why []string
		if c.TitleMatch {
			why = append(why, "same title")
		}
		if c.PDFMatches {
			why = append(why, "its PDF matches")
		}
		if c.InLibrary != "" {
			why = append(why, "in your library as "+c.InLibrary)
		}
		fmt.Fprintf(w, "%-12s %s\n", c.Work.OpenAlexID, titles.title(c.Work.DisplayTitle()))
		line := strings.Join(nonEmpty(byYear(c.Work), c.Work.Venue), " · ")
		if len(why) > 0 {
			line += "  (" + strings.Join(why, ", ") + ")"
		}
		fmt.Fprintf(w, "%-12s %s\n", "", line)
	}
	fmt.Fprintf(w, "\nLink the right one with fil match %s <OpenAlex ID>.\n", input)
	return nil
}

func (a *app) link(ctx context.Context, input, id string, refs bool) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()
	got, err := lib.LinkLocal(ctx, input, id, refs)
	if err != nil {
		return err
	}
	w := a.stdout
	fmt.Fprintf(w, "Linked:     %s is OpenAlex's %s\n", got.Work.FilID, got.Work.OpenAlexID)
	fmt.Fprintf(w, "            %s\n", titles.title(got.Work.DisplayTitle()))
	if got.Folded {
		fmt.Fprintf(w, "            It was in your library already; %s now finds it too.\n", input)
	}
	if refs {
		fmt.Fprintf(w, "References: %s recorded\n", thousands(got.Recorded.Refs))
	}
	return nil
}

// byYear is a work's first author and year.
func byYear(w model.Work) string {
	var parts []string
	if len(w.Authors) > 0 {
		parts = append(parts, w.Authors[0].Name)
	}
	if w.Year != nil {
		parts = append(parts, fmt.Sprint(*w.Year))
	}
	return strings.Join(parts, " · ")
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
