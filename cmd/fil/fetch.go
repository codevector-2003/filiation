package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

func (a *app) fetchCommand() *cobra.Command {
	var dryRun bool
	var max int
	cmd := &cobra.Command{
		Use:   "fetch [paper]",
		Short: "Download the legal open-access PDFs of your papers",
		Long: "Download your papers' PDFs into papers/, from the open-access copies OpenAlex\n" +
			"knows of — the published version first, then the accepted manuscript, then a\n" +
			"preprint. fil never downloads from behind a paywall or a login, and a link\n" +
			"that fails is left alone for a month.\n\n" +
			"Your own papers come first, then the ones your library cites most. Each run\n" +
			"downloads at most --max PDFs; run it again to continue. A PDF you deleted is\n" +
			"not downloaded again — name the paper to ask for it.\n\n" +
			"Once the downloads are done, fil reads the text of every new PDF, so\n" +
			"fil search finds it.\n\n" +
			"With --dry-run, fil only looks up the links and reports what it would\n" +
			"download, and why each paper without a PDF has none.",
		Example: "  fil fetch\n  fil fetch --max 200\n  fil fetch 10.7717/peerj.4375\n  fil fetch --dry-run",
		RunE: func(cmd *cobra.Command, args []string) error {
			if max < 0 {
				return usageError{cmd: cmd, err: errors.New("--max must not be negative")}
			}
			switch {
			case dryRun && len(args) > 0:
				return a.pdfLinks(cmd.Context(), strings.Join(args, " "))
			case dryRun:
				return a.locate(cmd.Context(), max)
			case len(args) > 0:
				return a.fetchOne(cmd.Context(), strings.Join(args, " "))
			}
			return a.fetch(cmd.Context(), max)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "look up links and report, without downloading")
	cmd.Flags().IntVar(&max, "max", 0, fmt.Sprintf("most PDFs to download in this run (default %d); with --dry-run, papers to look up (default %s)",
		library.DefaultFetchMax, thousands(library.DefaultLocateMax)))
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

func (a *app) fetch(ctx context.Context, max int) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()

	opts := library.FetchOptions{Max: max}
	budget := max
	if budget == 0 {
		budget = library.DefaultFetchMax
	}
	if a.interactive {
		downloading := false // a download progress line is on screen
		opts.Progress = func(f library.Fetched) {
			downloading = true
			fmt.Fprintf(a.stderr, "\r  %s of up to %s papers tried · %s downloaded · %s ",
				thousands(f.Tried), thousands(budget), thousands(f.Downloaded), megabytes(f.Bytes))
		}
		opts.ReadProgress = func(e library.Extracted) {
			if downloading {
				downloading = false
				fmt.Fprintln(a.stderr) // reading gets a line of its own
			}
			a.readProgress(e)
		}
	}
	res, runErr := lib.Fetch(ctx, opts)
	if a.interactive && (res.Tried > 0 || res.Read.Done > 0) {
		fmt.Fprintln(a.stderr)
	}
	if errors.Is(runErr, library.ErrNoFolder) {
		return a.explainNoFolder(runErr)
	}

	w := a.stdout
	if res.Located.Checked > 0 {
		fmt.Fprintf(w, "Looked up:  links for %s.\n", plural(res.Located.Checked, "paper", "papers"))
	}
	switch {
	case res.Tried == 0 && runErr == nil:
		fmt.Fprintln(w, "Nothing to download: every paper with a legal PDF link has its PDF, or its\n"+
			"links failed within the last month. fil fetch --dry-run shows the rest.")
	default:
		fmt.Fprintf(w, "Downloaded: %s of %s tried, %s in %s.\n",
			plural(res.Downloaded, "PDF", "PDFs"), thousands(res.Tried), megabytes(res.Bytes), res.Duration.Round(100*time.Millisecond))
		if failed := res.Tried - res.Downloaded; failed > 0 {
			fmt.Fprintf(w, "No PDF:     %s. Links that failed: %s. fil tries them again in a month.\n",
				plural(failed, "paper", "papers"), reasons(res.Outcomes))
		}
	}
	if res.Read.Total > 0 {
		a.printRead(res.Read)
	}
	ctx = context.WithoutCancel(ctx)
	if n, err := lib.FetchedOnDisk(ctx); err == nil {
		fmt.Fprintf(w, "Library:    %s with a PDF in %s\n", plural(n, "paper", "papers"), filepath.Join(lib.Folder(), "papers"))
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			fmt.Fprintln(w, "\nStopped. Every finished download, and every PDF read, is saved; run fil fetch\nto continue.")
			return errCancelled
		}
		fmt.Fprintln(w, "\nEvery finished download is saved; run fil fetch to continue.")
		return runErr
	}
	return nil
}

func (a *app) fetchOne(ctx context.Context, input string) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()
	p, err := lib.FetchOne(ctx, input)
	if errors.Is(err, library.ErrNoFolder) {
		return a.explainNoFolder(err)
	}
	if err != nil {
		return a.explainFind(err)
	}

	w := a.stdout
	fmt.Fprintln(w, headline(p.Work))
	switch {
	case p.Already:
		fmt.Fprintf(w, "Already in your library:\n  %s\n", p.File)
		if p.Text != nil {
			a.printPaperText(*p.Text)
		}
	case p.File != "":
		fmt.Fprintf(w, "Downloaded:\n  %s\n", p.File)
		if p.Text != nil {
			a.printPaperText(*p.Text)
		}
	case len(p.Attempts) == 0:
		fmt.Fprintln(w, "No legal PDF link to try. fil fetch --dry-run says why.")
		return exitCode(exitFailure)
	default:
		fmt.Fprintln(w, "No PDF. What each link did:")
		for _, at := range p.Attempts {
			fmt.Fprintf(w, "  • %s — %s\n    %s\n", at.Host, describeOutcome(at.Outcome, at.Status), at.URL)
		}
		return exitCode(exitFailure)
	}
	return nil
}

// reasons lists failed attempts by outcome, commonest first, in words.
func reasons(outcomes map[string]int) string {
	type kv struct {
		k string
		n int
	}
	var all []kv
	for k, n := range outcomes {
		all = append(all, kv{k, n})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].n > all[j].n || all[i].n == all[j].n && all[i].k < all[j].k })
	var parts []string
	for _, x := range all {
		parts = append(parts, fmt.Sprintf("%s %d", describeOutcome(x.k, 0), x.n))
	}
	return strings.Join(parts, ", ")
}

// describeOutcome says what a failed download means.
func describeOutcome(outcome string, status int) string {
	s := map[string]string{
		"refused":       "the host refused",
		"not-found":     "the link is dead",
		"http-error":    "the host answered with an error",
		"not-a-pdf":     "a web page, not a PDF",
		"too-large":     "larger than 100 MB",
		"timeout":       "timed out",
		"network":       "network error",
		"redirect-loop": "redirected in circles",
		"duplicate":     "another paper's PDF",
		"ok":            "downloaded",
	}[outcome]
	if s == "" {
		s = outcome
	}
	if status != 0 && outcome != "not-a-pdf" {
		s += fmt.Sprintf(" (HTTP %d)", status)
	}
	return s
}

func megabytes(b int64) string { return fmt.Sprintf("%.1f MB", float64(b)/(1<<20)) }
