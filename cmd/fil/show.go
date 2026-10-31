package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

func (a *app) listCommand() *cobra.Command {
	var opts library.ListOptions
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the papers in your library, with their PDF and text",
		Long: "List the papers in your library: the ones you added first, then the most cited\n" +
			"within it. Beside each: PDF if its PDF is in papers/, its text — searchable, or\n" +
			"why not — and how many of its references carry the sentence they are cited in.",
		Example: "  fil list --seeds\n  fil list --with-pdf --limit 50\n  fil list --title open access\n" +
			"  fil list --collection \"My reading\"\n  fil list --without-text\n  fil list --json",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if opts.Limit < 0 {
				return usageError{cmd: cmd, err: errors.New("--limit must not be negative")}
			}
			return a.list(cmd.Context(), opts, asJSON)
		},
	}
	cmd.Flags().BoolVar(&opts.Seeds, "seeds", false, "only the papers you added or imported")
	cmd.Flags().BoolVar(&opts.WithPDF, "with-pdf", false, "only papers with a PDF in papers/")
	cmd.Flags().BoolVar(&opts.WithoutText, "without-text", false, "only papers whose PDF gave no searchable text")
	cmd.Flags().StringVar(&opts.Collection, "collection", "", "only this collection's papers (a folder in papers/ is one)")
	cmd.Flags().StringVar(&opts.Title, "title", "", "only papers whose title has all these words")
	cmd.Flags().IntVar(&opts.Limit, "limit", 50, "most papers to list; 0 lists all")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON, one object per paper, for scripts")
	cmd.Flags().BoolVar(&a.pickMode, "pick", false, "move through the papers with the arrow keys; Enter for details, o to open a PDF")
	return cmd
}

func (a *app) list(ctx context.Context, opts library.ListOptions, asJSON bool) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()
	rows, err := lib.List(ctx, opts)
	if err != nil {
		return err
	}
	w := a.stdout
	if asJSON {
		enc := json.NewEncoder(a.raw())
		for _, r := range rows {
			if err := enc.Encode(listJSON(r)); err != nil {
				return err
			}
		}
		return nil
	}
	if a.pickMode && a.canPick() && len(rows) > 0 {
		picks := make([]pickRow, len(rows))
		for i, r := range rows {
			picks[i] = listRow(r)
		}
		return a.pick(ctx, lib, "Papers", picks)
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "No papers match.")
		return nil
	}
	for _, r := range rows {
		title := r.Title
		if title == "" {
			title = "[not fetched: " + r.OpenAlexID + "]"
		}
		year := "    "
		if r.Year > 0 {
			year = fmt.Sprint(r.Year)
		}
		mark := " "
		if r.Seed {
			mark = "*"
		}
		fmt.Fprintf(w, "%s %s  %s  %s\n", mark, r.FilID, year, titles.title(short(title)))
		var state []string
		if r.Author != "" {
			state = append(state, lastName(r.Author))
		}
		if r.CitedBy > 0 {
			state = append(state, "cited by "+plural(r.CitedBy, "paper", "papers")+" here")
		}
		if r.HasPDF {
			s := "PDF"
			if r.Mismatch {
				s += " (another paper or edition?)"
			}
			state = append(state, s)
		}
		switch r.TextStatus {
		case "":
		case "ok":
			state = append(state, "searchable")
		default:
			state = append(state, textReason(r.TextStatus, ""))
		}
		if r.Contexts > 0 {
			state = append(state, plural(r.Contexts, "citation sentence", "citation sentences"))
		}
		if len(state) > 0 {
			fmt.Fprintf(w, "             %s\n", strings.Join(state, " · "))
		}
	}
	if opts.Limit > 0 && len(rows) == opts.Limit {
		fmt.Fprintf(w, "\nThe first %d. --limit 0 lists all.\n", opts.Limit)
	}
	fmt.Fprintln(w, "\n* a paper you added or imported. fil show <fil ID> tells everything about one.")
	return nil
}

type listed struct {
	FilID      string `json:"fil_id"`
	OpenAlexID string `json:"openalex_id,omitempty"`
	Title      string `json:"title,omitempty"`
	Year       int    `json:"year,omitempty"`
	Author     string `json:"first_author,omitempty"`
	Seed       bool   `json:"seed"`
	Fetched    bool   `json:"fetched"`
	CitedBy    int    `json:"cited_by_here"`
	PDF        bool   `json:"pdf"`
	Mismatch   bool   `json:"pdf_mismatch,omitempty"`
	Text       string `json:"text,omitempty"`
	Contexts   int    `json:"citation_sentences"`
}

func listJSON(r library.Listed) listed {
	return listed{FilID: r.FilID, OpenAlexID: r.OpenAlexID, Title: r.Title, Year: r.Year, Author: r.Author,
		Seed: r.Seed, Fetched: r.Hydrated, CitedBy: r.CitedBy, PDF: r.HasPDF, Mismatch: r.Mismatch,
		Text: r.TextStatus, Contexts: r.Contexts}
}

func lastName(name string) string {
	f := strings.Fields(name)
	if len(f) == 0 {
		return name
	}
	return f[len(f)-1]
}

func (a *app) showCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <paper>",
		Short: "Tell everything the library knows about one paper",
		Long: "Tell everything the library knows about one paper: its details, its place in\n" +
			"the graph, its PDF or why there is none, its text or why there is none, and the\n" +
			"sentences it cites its references in.",
		Example: "  fil show 10.7717/peerj.4375\n  fil show F7K2M9QXA",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return usageError{cmd: cmd, err: errors.New("show needs a paper")}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.show(cmd.Context(), strings.Join(args, " "))
		},
	}
}

func (a *app) show(ctx context.Context, input string) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()
	p, err := lib.Show(ctx, input)
	if err != nil {
		return a.explainFind(err)
	}
	w := a.stdout
	fmt.Fprintln(w, headline(p.Work))
	if by := byline(p.Work); by != "" {
		fmt.Fprintf(w, "       %s\n", by)
	}
	fmt.Fprintf(w, "       %s\n\n", identifiers(p.Work))

	origin := map[model.Source]string{model.SourceSeed: "added by you", model.SourceUpload: "imported by you",
		model.SourceExpansion: "reached by expansion"}[p.Work.Source]
	if origin != "" {
		fmt.Fprintf(w, "In the library:  %s", origin)
		if p.Work.Depth != nil && *p.Work.Depth > 0 {
			fmt.Fprintf(w, ", %s from a seed", plural(*p.Work.Depth, "step", "steps"))
		}
		fmt.Fprintln(w)
	}
	switch {
	case p.Work.IsStub():
		fmt.Fprintln(w, "Graph:           not fetched yet, so its references are unknown — fil expand fetches it")
	default:
		fmt.Fprintf(w, "Graph:           cites %s; cited by %s in your library — fil neighbours lists them\n",
			plural(p.Cites, "work", "works"), plural(p.CitedBy, "work", "works"))
	}
	if p.Folder != "" {
		fmt.Fprintf(w, "Folder:          %s\n", p.Folder)
	}

	// The PDF, or why there is none.
	present := 0
	for _, f := range p.Files {
		if f.Missing {
			continue
		}
		present++
		label := "PDF:            "
		if present > 1 {
			label = "                "
		}
		what := f.Version
		if what == "" {
			what = map[string]string{"import": "your copy", "found": "found in papers/", "download": "downloaded"}[f.Origin]
		}
		fmt.Fprintf(w, "%s %s (%s)\n", label, f.Path, what)
		if f.Mismatch {
			fmt.Fprintln(w, "                 its first pages do not carry this paper's title and author: another\n"+
				"                 paper, or another edition of it. Kept; check it.")
		}
	}
	if present == 0 {
		fmt.Fprintf(w, "PDF:             none — %s\n", pdfWhy(p))
	}

	// The text, or why there is none.
	switch {
	case p.Text == nil && present > 0:
		fmt.Fprintln(w, "Text:            not read yet — fil extract reads it")
	case p.Text == nil:
	case p.Text.Status == "ok":
		fmt.Fprintf(w, "Text:            searchable, %s — fil search --paper %s\n", plural(p.Text.Pages, "page", "pages"), p.Work.FilID)
	default:
		fmt.Fprintf(w, "Text:            none — %s\n", textReason(p.Text.Status, p.Text.Reason))
	}

	if p.Contexts > 0 {
		fmt.Fprintf(w, "Citations:       %s of %s carry the sentence they are cited in:\n",
			thousands(p.Contexts), plural(p.Cites, "reference", "references"))
		for _, c := range p.Sample {
			fmt.Fprintf(w, "  • %s\n", titles.title(short(c.Work.DisplayTitle())))
			writeWrapped(w, "      ", fmt.Sprintf("p. %d: “%s”", c.Page, c.Sentence), 78)
		}
		if p.Contexts > len(p.Sample) {
			fmt.Fprintf(w, "  fil neighbours %s --context shows them all.\n", p.Work.FilID)
		}
	}
	return nil
}

// pdfWhy says why a paper has no PDF on disk, from what the library knows.
func pdfWhy(p library.Paper) string {
	for _, f := range p.Files {
		if f.Missing {
			return "you deleted it; fil fetch " + p.Work.FilID + " downloads it again"
		}
	}
	if p.Work.OpenAlexID == "" {
		return "a local document's PDF has gone from papers/"
	}
	if len(p.Attempts) > 0 {
		return "every link failed: " + reasons(outcomesOf(p.Attempts)) + " — fil tries again in a month"
	}
	switch p.PDF.Availability {
	case model.PDFLink:
		return "a legal link is known — fil fetch downloads it"
	case model.PDFOpenNoLink:
		return "open access, but only as a web page"
	case model.PDFNoOpenCopy:
		return "no open-access copy that OpenAlex knows of"
	case model.PDFNotInOpenAlex:
		return "not in OpenAlex"
	}
	return "not looked up yet — fil fetch --dry-run looks"
}

func outcomesOf(attempts []library.Attempt) map[string]int {
	out := map[string]int{}
	for _, at := range attempts {
		if at.Outcome != "ok" {
			out[at.Outcome]++
		}
	}
	return out
}

// printWithoutText is fil stats --why: each paper whose PDF gave no text,
// grouped by why.
func (a *app) printWithoutText(ctx context.Context, lib *library.Library) error {
	none, err := lib.WithoutText(ctx)
	if err != nil {
		return err
	}
	w := a.stdout
	if len(none) == 0 {
		fmt.Fprintln(w, "\nEvery PDF fil has read gave searchable text.")
		return nil
	}
	fmt.Fprintln(w, "\nPapers whose PDF gave no searchable text, and why:")
	last := ""
	for _, n := range none {
		if n.Status != last {
			fmt.Fprintf(w, "\n%s\n", textReason(n.Status, ""))
			last = n.Status
		}
		fmt.Fprintf(w, "  • %s  %s\n", short(n.Work.DisplayTitle()), n.Work.FilID)
		if n.Reason != "" {
			fmt.Fprintf(w, "      %s\n", n.Reason)
		}
		fmt.Fprintf(w, "      %s\n", n.File)
	}
	return nil
}
