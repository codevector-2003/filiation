package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

func (a *app) fetchCommand() *cobra.Command {
	var dryRun bool
	var max int
	cmd := &cobra.Command{
		Use:   "fetch [paper]",
		Short: "Find legal open-access PDFs for your papers",
		Long: "Find where your papers can be downloaded legally: the open-access copies\n" +
			"OpenAlex knows of — the published version, the accepted manuscript or a\n" +
			"preprint. fil never downloads from behind a paywall or a login.\n\n" +
			"With --dry-run, fil looks up the links and reports what it would download,\n" +
			"and why each paper without a PDF has none. Your own papers come first, then\n" +
			"the ones your library cites most. Downloading itself is not built yet.\n\n" +
			"Name a paper to see its links.",
		Example: "  fil fetch --dry-run\n  fil fetch --dry-run 10.7717/peerj.4375",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !dryRun {
				return usageError{cmd: cmd, err: errors.New("downloading is not built yet — pass --dry-run to see what fil would download")}
			}
			if max < 0 {
				return usageError{cmd: cmd, err: errors.New("--max must not be negative")}
			}
			if len(args) > 0 {
				return a.pdfLinks(cmd.Context(), strings.Join(args, " "))
			}
			return a.locate(cmd.Context(), max)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "look up links and report, without downloading")
	cmd.Flags().IntVar(&max, "max", 0, fmt.Sprintf("most papers to look up in this run (default %s, 1 credit per 100)", thousands(library.DefaultLocateMax)))
	return cmd
}

func (a *app) locate(ctx context.Context, max int) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()

	opts := library.LocateOptions{Max: max}
	if a.interactive {
		opts.Progress = func(l library.Located) {
			fmt.Fprintf(a.stderr, "\r  looked up %s papers ", thousands(l.Checked))
		}
	}
	res, runErr := lib.LocatePDFs(ctx, opts)
	if a.interactive && res.Checked > 0 {
		fmt.Fprintln(a.stderr)
	}

	w := a.stdout
	switch {
	case res.Checked == 0 && runErr == nil:
		fmt.Fprintln(w, "Every paper's links were looked up within the last month; nothing to ask.")
	default:
		fmt.Fprintf(w, "Looked up: %s in %s.\n\n", plural(res.Checked, "paper", "papers"), plural(res.Requests, "request", "requests"))
	}

	ctx = context.WithoutCancel(ctx)
	if counts, err := lib.PDFCounts(ctx); err == nil {
		a.printPDFCounts(counts)
	}
	if q, ok := lib.Quota(); ok && q.Limit > 0 {
		fmt.Fprintf(w, "\nOpenAlex:  %s of %s credits left today.\n", thousands(q.Remaining), thousands(q.Limit))
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			fmt.Fprintln(w, "\nStopped. Everything looked up is saved; run it again to continue.")
			return errCancelled
		}
		fmt.Fprintln(w, "\nEverything looked up before the failure is saved; run it again to continue.")
		return runErr
	}
	return nil
}

// printPDFCounts says, for the whole library, what fil could download and why
// the rest cannot be — so a library where most papers have no PDF reads as the
// state of open access, not as a broken tool (D21).
func (a *app) printPDFCounts(c map[model.PDFAvailability]int) {
	w := a.stdout
	total := 0
	for _, n := range c {
		total += n
	}
	if total == 0 {
		return
	}
	rows := []struct {
		k    model.PDFAvailability
		what string
	}{
		{model.PDFLink, "have a legal PDF link fil can try"},
		{model.PDFOpenNoLink, "are open access, but only as a web page — no direct PDF"},
		{model.PDFNoOpenCopy, "have no open-access copy that OpenAlex knows of"},
		{model.PDFNotInOpenAlex, "are not in OpenAlex"},
		{model.PDFUnchecked, "not looked up yet — fil fetch --dry-run continues"},
	}
	label := "PDFs:"
	for _, r := range rows {
		if n := c[r.k]; n > 0 {
			fmt.Fprintf(w, "%-10s %s %s (%.0f%%)\n", label, plural(n, "paper", "papers"), r.what, 100*float64(n)/float64(total))
			label = ""
		}
	}
}

func (a *app) pdfLinks(ctx context.Context, input string) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()
	p, err := lib.PDFLinks(ctx, input)
	if err != nil {
		return a.explainFind(err)
	}

	w := a.stdout
	fmt.Fprintln(w, headline(p.Work))
	fmt.Fprintf(w, "       %s\n\n", identifiers(p.Work))
	switch p.Availability {
	case model.PDFLink:
		fmt.Fprintf(w, "fil would try %s, in this order:\n", plural(len(p.Links), "link", "links"))
		for i, l := range p.Links {
			fmt.Fprintf(w, "  %d. %s\n     %s\n", i+1, describeLocation(l), l.PDFURL)
		}
	case model.PDFOpenNoLink:
		fmt.Fprintln(w, "Open access, but only as a web page: no open copy offers a direct PDF.\n"+
			"Read it in a browser from the paper's DOI.")
	case model.PDFNoOpenCopy:
		fmt.Fprintln(w, "No open-access copy that OpenAlex knows of. fil never downloads from behind\n"+
			"a paywall; a preprint may still appear later, and fil asks again after a month.")
	case model.PDFNotInOpenAlex:
		fmt.Fprintln(w, "Not in OpenAlex, so fil has nowhere legal to look.")
	default:
		fmt.Fprintln(w, "Not looked up: this paper has no OpenAlex ID.")
	}
	return nil
}

// describeLocation is one link's version, host and licence in words.
func describeLocation(l model.Location) string {
	version := map[string]string{
		"published": "published version", "accepted": "accepted manuscript", "preprint": "preprint",
	}[l.Version]
	if version == "" {
		version = "version not stated"
	}
	licence := l.License
	if licence == "" {
		licence = "no licence stated"
	}
	return fmt.Sprintf("%s · %s · %s", version, l.Host, licence)
}
