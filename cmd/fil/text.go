package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/library"
)

func (a *app) searchCommand() *cobra.Command {
	var opts library.SearchOptions
	cmd := &cobra.Command{
		Use:   "search <words>",
		Short: "Find passages in your PDFs by keyword",
		Long: "Find the passages in your library's PDFs that contain every word you give,\n" +
			"best first, each with its paper and page. Words match by stem, so \"citing\"\n" +
			"finds \"cited\"; put a phrase in quotes to find it as written, and end a word\n" +
			"with * to match its beginning.\n\n" +
			"Reference lists are left out, or a search for an author's name would find\n" +
			"every paper that cites them; --references puts them back.\n\n" +
			"Only PDFs fil has read are searched. fil fetch reads what it downloads;\n" +
			"fil extract reads PDFs you put in papers/ yourself.",
		Example: "  fil search open data citation advantage\n" +
			"  fil search '\"nine percent\" citations'\n" +
			"  fil search replicat* --paper 10.7717/peerj.4375\n" +
			"  fil search Piwowar --references",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.Limit < 0 {
				return usageError{cmd: cmd, err: errors.New("--limit must not be negative")}
			}
			err := a.search(cmd.Context(), strings.Join(args, " "), opts)
			if errors.Is(err, errs.ErrEmptyQuery) {
				return usageError{cmd: cmd, err: errors.New("give at least one word to search for")}
			}
			return err
		},
	}
	cmd.Flags().IntVar(&opts.Limit, "limit", 0, fmt.Sprintf("most passages to show (default %d)", library.DefaultSearchLimit))
	cmd.Flags().StringVar(&opts.Paper, "paper", "", "search only this paper (fil ID, DOI, OpenAlex ID or title)")
	cmd.Flags().BoolVar(&opts.References, "references", false, "search reference lists too")
	return cmd
}

func (a *app) search(ctx context.Context, words string, opts library.SearchOptions) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()
	res, err := lib.Search(ctx, words, opts)
	if err != nil {
		return a.explainFind(err)
	}

	w := a.stdout
	if len(res.Hits) == 0 {
		fmt.Fprintln(w, "No passage contains all of those words.")
	}
	for i, h := range res.Hits {
		if i > 0 {
			fmt.Fprintln(w)
		}
		where := fmt.Sprintf("page %d", h.Page)
		if h.Section != "" {
			where += ", reference list"
		}
		fmt.Fprintf(w, "%d. %s — %s\n", i+1, headline(h.Work), where)
		meta := workIDs(h.Work)
		if by := byline(h.Work); by != "" {
			meta += " · " + by
		}
		fmt.Fprintf(w, "   %s\n", meta)
		writeWrapped(w, "   ", snippet(h.Snippet), 78)
		fmt.Fprintf(w, "   %s\n", h.File)
	}
	limit := opts.Limit
	if limit == 0 {
		limit = library.DefaultSearchLimit
	}
	if len(res.Hits) == limit {
		fmt.Fprintf(w, "\nThe best %d passages, at most %d from one paper. --limit shows more.\n", limit, library.DefaultPerPaper)
	}
	if res.Unread > 0 {
		fmt.Fprintf(w, "\nnote: %s in papers/ not read yet, so not searched — fil extract reads %s.\n",
			plural(res.Unread, "PDF", "PDFs"), map[bool]string{true: "it", false: "them"}[res.Unread == 1])
	}
	if len(res.Hits) == 0 {
		return exitCode(exitNotFound)
	}
	return nil
}

// snippet puts a search snippet on one line, with the matched words in
// guillemets — a run of them together, as «green open access».
func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, library.MarkEnd+" "+library.MarkStart, " ")
	return strings.NewReplacer(library.MarkStart, "«", library.MarkEnd, "»").Replace(s)
}

// writeWrapped writes s in lines of at most width, each with indent.
func writeWrapped(w io.Writer, indent, s string, width int) {
	line := indent
	for _, word := range strings.Fields(s) {
		if len(line) > len(indent) && len([]rune(line))+1+len([]rune(word)) > width {
			fmt.Fprintln(w, line)
			line = indent
		}
		if len(line) > len(indent) {
			line += " "
		}
		line += word
	}
	if len(line) > len(indent) {
		fmt.Fprintln(w, line)
	}
}

func (a *app) extractCommand() *cobra.Command {
	var again bool
	cmd := &cobra.Command{
		Use:   "extract [paper]",
		Short: "Read the text of your PDFs so they can be searched",
		Long: "Read the text of every PDF in papers/ that fil has not read yet — new ones,\n" +
			"ones that changed, and PDFs you put there yourself — so fil search finds\n" +
			"them. fil fetch already does this for what it downloads.\n\n" +
			"A PDF with no usable text is reported with the reason: a scan (fil does no\n" +
			"OCR), a text layer that is gibberish, a damaged file. It is not tried again\n" +
			"until the file changes, or you ask with --again.",
		Example: "  fil extract\n  fil extract 10.7717/peerj.4375\n  fil extract --again",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				return a.extractOne(cmd.Context(), args[0], again)
			}
			return a.extract(cmd.Context(), again)
		},
	}
	cmd.Flags().BoolVar(&again, "again", false, "read PDFs again even if already read")
	return cmd
}

func (a *app) extract(ctx context.Context, again bool) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()

	opts := library.ExtractOptions{Again: again}
	if a.interactive {
		opts.Progress = a.readProgress
	}
	res, runErr := lib.ExtractText(ctx, opts)
	if a.interactive && res.Done > 0 {
		fmt.Fprintln(a.stderr)
	}
	if errors.Is(runErr, library.ErrNoFolder) {
		return a.explainNoFolder(runErr)
	}

	w := a.stdout
	if res.Total == 0 && runErr == nil {
		fmt.Fprintln(w, "Nothing to read: fil has read every PDF in papers/. fil extract --again reads\nthem all again.")
	} else {
		a.printRead(res)
	}
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) {
			fmt.Fprintln(w, "\nStopped. Every PDF read so far is saved; run fil extract to continue.")
			return errCancelled
		}
		return runErr
	}
	return nil
}

func (a *app) readProgress(e library.Extracted) {
	fmt.Fprintf(a.stderr, "\r  read %s of %s PDFs ", thousands(e.Done), thousands(e.Total))
}

// printRead says what reading the PDFs gave, and names every paper left
// without text, with the reason — a library where some PDFs are scans reads
// as the state of the files, not as a broken tool.
func (a *app) printRead(res library.Extracted) {
	w := a.stdout
	fmt.Fprintf(w, "Read:       %s in %s — %s searchable (%s)",
		plural(res.Done, "PDF", "PDFs"), res.Duration.Round(100*time.Millisecond),
		thousands(res.Counts["ok"]), plural(res.Passages, "passage", "passages"))
	if rest := withoutText(res.Counts); rest != "" {
		fmt.Fprintf(w, "; %s", rest)
	}
	fmt.Fprintln(w, ".")
	var none []library.ExtractedPaper
	for _, p := range res.Papers {
		if p.Status != "ok" {
			none = append(none, p)
		}
	}
	sort.Slice(none, func(i, j int) bool { return none[i].Status < none[j].Status })
	const shown = 10
	for i, p := range none {
		if i == shown {
			fmt.Fprintf(w, "  … and %d more — fil stats counts them.\n", len(none)-shown)
			break
		}
		fmt.Fprintf(w, "  • %s  %s\n    %s\n", short(p.Work.DisplayTitle()), p.Work.FilID, textReason(p.Status, p.Reason))
	}
}

// withoutText counts the papers whose PDF gave no text, by why, in words.
func withoutText(counts map[string]int) string {
	var parts []string
	for _, k := range []struct{ status, one, many string }{
		{"no-text", "scan with no text", "scans with no text"},
		{"gibberish", "with a gibberish text layer", "with a gibberish text layer"},
		{"unreadable", "unreadable", "unreadable"},
		{"timed-out", "too slow to read", "too slow to read"},
	} {
		if n := counts[k.status]; n > 0 {
			parts = append(parts, plural(n, k.one, k.many))
		}
	}
	return strings.Join(parts, ", ")
}

// printTextCounts is fil stats' line on text: how many papers can be
// searched, and why the rest of the PDFs read gave none.
func printTextCounts(w io.Writer, c map[string]int) {
	read := 0
	for _, n := range c {
		read += n
	}
	if read == 0 {
		return
	}
	fmt.Fprintf(w, "Text:       %s searchable", plural(c["ok"], "paper", "papers"))
	if rest := withoutText(c); rest != "" {
		fmt.Fprintf(w, "; no text from %s", rest)
	}
	fmt.Fprintln(w, " — fil search finds passages")
}

// textReason says why a paper's PDF gave no text.
func textReason(status, reason string) string {
	head := map[string]string{
		"ok":         "searchable",
		"no-text":    "no text layer",
		"gibberish":  "the text layer is gibberish",
		"unreadable": "could not be read",
		"timed-out":  "took too long to read",
	}[status]
	if head == "" {
		head = status
	}
	if reason == "" {
		return head
	}
	return head + ": " + reason
}

func (a *app) extractOne(ctx context.Context, input string, again bool) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()
	p, err := lib.ExtractOne(ctx, input, again)
	switch {
	case errors.Is(err, library.ErrNoFolder):
		return a.explainNoFolder(err)
	case errors.Is(err, library.ErrNoPDF):
		fmt.Fprintf(a.stderr, "fil: %s has no PDF in papers/. fil fetch %s downloads it, if there is a legal copy.\n",
			p.Work.FilID, p.Work.FilID)
		return exitCode(exitNotFound)
	case err != nil:
		return a.explainFind(err)
	}
	w := a.stdout
	fmt.Fprintln(w, headline(p.Work))
	fmt.Fprintf(w, "  %s\n", p.File)
	a.printPaperText(p)
	return nil
}

// printPaperText is one paper's text, in a line.
func (a *app) printPaperText(p library.ExtractedPaper) {
	w := a.stdout
	switch {
	case p.Status == "ok" && p.Already:
		fmt.Fprintf(w, "Text:  searchable, %s; read before. --again reads it again.\n", plural(p.Pages, "page", "pages"))
	case p.Status == "ok":
		fmt.Fprintf(w, "Text:  searchable, %s in %s.\n", plural(p.Pages, "page", "pages"), plural(p.Passages, "passage", "passages"))
	default:
		fmt.Fprintf(w, "Text:  none — %s.\n", textReason(p.Status, p.Reason))
	}
}
