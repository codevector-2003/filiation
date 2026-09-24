package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/library"
)

func (a *app) exportCommand() *cobra.Command {
	export := &cobra.Command{
		Use:   "export",
		Short: "Write your library out for other tools",
		Long:  "Write your library in a format other tools can read. Nothing is locked in.",
	}

	var out string
	var includeStubs bool
	graphml := &cobra.Command{
		Use:   "graphml",
		Short: "Export the citation graph as GraphML, for Gephi, Cytoscape or NetworkX",
		Long: "Export the citation graph as GraphML. Each work is a node carrying its title,\n" +
			"year, type, venue, DOI, open-access status, global citation count, depth from\n" +
			"your papers, and whether it is a seed. Each arrow means \"cites\".\n\n" +
			"By default only fetched works are exported, with the citations between them.\n" +
			"After an expansion most works are known only by ID, and including them buries\n" +
			"the readable graph; --include-stubs exports everything.",
		Example: "  fil export graphml -o library.graphml\n" +
			"  fil export graphml --include-stubs > everything.graphml",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.exportGraphML(cmd.Context(), out, includeStubs)
		},
	}
	graphml.Flags().StringVarP(&out, "output", "o", "", "file to write (default: standard output)")
	graphml.Flags().BoolVar(&includeStubs, "include-stubs", false, "also export works known only by ID")
	export.AddCommand(graphml)
	return export
}

func (a *app) exportGraphML(ctx context.Context, out string, includeStubs bool) error {
	if out == "" && a.interactive {
		// Thousands of lines of XML scrolling past help nobody.
		return usageError{err: errors.New("give a file to write, e.g. fil export graphml -o library.graphml, or redirect the output")}
	}
	lib, err := a.openExisting(ctx)
	if err != nil {
		return err
	}
	defer lib.Close()

	opts := library.ExportOptions{IncludeStubs: includeStubs}
	var got library.Exported
	if out == "" {
		got, err = lib.ExportGraphML(ctx, a.stdout, opts)
	} else {
		got, err = writeFileAtomically(out, func(w io.Writer) (library.Exported, error) {
			return lib.ExportGraphML(ctx, w, opts)
		})
	}
	if err != nil {
		return err
	}

	where := "standard output"
	if out != "" {
		where = out
	}
	fmt.Fprintf(a.stderr, "Wrote %s and %s to %s.\n",
		plural(got.Nodes, "work", "works"), plural(got.Edges, "citation", "citations"), where)
	if !includeStubs {
		fmt.Fprintln(a.stderr, "Only fetched works are included; --include-stubs adds those known only by ID.")
	}
	return nil
}

// writeFileAtomically writes to a temporary file beside path and renames it
// into place, so a failed or interrupted export never leaves a truncated file
// where a good one used to be.
func writeFileAtomically(path string, write func(io.Writer) (library.Exported, error)) (library.Exported, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".fil-export-*")
	if err != nil {
		return library.Exported{}, fmt.Errorf("create %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // gone after a successful rename

	got, err := write(tmp)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return library.Exported{}, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return library.Exported{}, fmt.Errorf("write %s: %w", path, err)
	}
	return got, nil
}
