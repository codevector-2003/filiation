package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/library"
)

func (a *app) zoteroCommand() *cobra.Command {
	var dryRun, noRefs, noPDFs, tags bool
	var skip []string
	cmd := &cobra.Command{
		Use:   "zotero",
		Short: "Bring in your Zotero library",
		Long: "Bring in the Zotero library on this computer: its items as papers, its\n" +
			"collections, and its PDFs. Zotero is only read, never changed.\n\n" +
			"Zotero 7 or later must be running, with \"Allow other applications on this\n" +
			"computer to communicate with Zotero\" ticked in Settings, Advanced.\n\n" +
			"An item with a DOI, arXiv ID or PubMed ID is looked up in OpenAlex and added\n" +
			"like fil add, with what it cites. One without, or that OpenAlex does not know,\n" +
			"is kept as your own document: searchable, with no citation links. Titles are\n" +
			"never searched, since each search costs OpenAlex credits.\n\n" +
			"Running it again adds only what is new. Stopping keeps what is in so far.",
		Example: "  fil zotero --dry-run\n" +
			"  fil zotero\n" +
			"  fil zotero --skip ABCD2345 --tags",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.zotero(cmd.Context(), dryRun, library.ZoteroOptions{Skip: skip, Refs: !noRefs, PDFs: !noPDFs, Tags: tags})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "say what would come in, and write nothing")
	cmd.Flags().BoolVar(&noRefs, "no-refs", false, "do not record what each paper cites")
	cmd.Flags().BoolVar(&noPDFs, "no-pdfs", false, "do not copy the PDFs")
	cmd.Flags().BoolVar(&tags, "tags", false, "make labels from the tags you added in Zotero")
	cmd.Flags().StringSliceVar(&skip, "skip", nil, "leave out this Zotero collection, by key, and those inside it")
	return cmd
}

func (a *app) zotero(ctx context.Context, dryRun bool, opts library.ZoteroOptions) error {
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()
	w := a.stdout

	p, err := lib.ZoteroPreview(ctx)
	switch {
	case errors.Is(err, library.ErrZoteroNotRunning):
		return fmt.Errorf("Zotero is not running on this computer. Open Zotero 7 or later, and in Settings, Advanced, "+
			"tick \"Allow other applications on this computer to communicate with Zotero\": %w", err)
	case errors.Is(err, library.ErrZoteroNotAllowed):
		return fmt.Errorf("Zotero is running but not letting other apps read it. In Zotero's Settings, Advanced, "+
			"tick \"Allow other applications on this computer to communicate with Zotero\": %w", err)
	case err != nil:
		return err
	}
	fmt.Fprintf(w, "Zotero:     %s, %s\n", plural(p.Items, "item", "items"), plural(len(p.Collections), "collection", "collections"))
	fmt.Fprintf(w, "            %s with a DOI, %s with an arXiv or PubMed ID, %s with no ID\n",
		thousands(p.WithDOI), thousands(p.WithOtherID), thousands(p.NoID))
	fmt.Fprintf(w, "            %s with a PDF; %s brought in before\n", thousands(p.PDFs), thousands(p.Already))
	if dryRun {
		fmt.Fprintln(w, "\nCollections, with their keys for --skip:")
		depth := map[string]int{}
		for _, c := range p.Collections {
			if c.Parent != "" {
				depth[c.Key] = depth[c.Parent] + 1
			}
			fmt.Fprintf(w, "  %*s%s  %s (%d)\n", 2*depth[c.Key], "", c.Key, c.Name, c.Items)
		}
		fmt.Fprintln(w, "\nNothing was written. Run fil zotero to bring it in.")
		return nil
	}

	opts.Progress = func(done, total int, _ string) {
		if done%25 == 0 || done == total {
			fmt.Fprintf(a.stderr, "\r%d of %d items", done, total)
		}
	}
	r, err := lib.ZoteroImport(ctx, opts)
	fmt.Fprintln(a.stderr)
	fmt.Fprintf(w, "\nIn:         %s matched in OpenAlex, %s kept as your own documents, %s in already\n",
		thousands(r.Matched), thousands(r.Unmatched), thousands(r.Already))
	fmt.Fprintf(w, "PDFs:       %s copied", thousands(r.PDFs))
	if r.PDFsMissing > 0 {
		fmt.Fprintf(w, "; %s listed in Zotero but not on this computer", thousands(r.PDFsMissing))
	}
	fmt.Fprintf(w, "\nCollections: %s\n", thousands(r.Collections))
	for _, u := range r.NotMatched {
		fmt.Fprintf(w, "  %s  %s (%s)\n", u.FilID, u.Title, u.Reason)
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(w, "\nStopped. Everything brought in is kept; run it again to continue.")
		return nil
	}
	return err
}
