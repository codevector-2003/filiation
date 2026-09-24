package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/codevector-2003/filiation/internal/library"
	"github.com/codevector-2003/filiation/internal/model"
)

// errCancelled marks a run the user stopped. It is not a failure — everything
// fetched is saved — but a script needs to know the run did not finish, so it
// gets the conventional exit code for an interrupt.
var errCancelled = errors.New("stopped")

// lowCoverage is the reference coverage below which fil explains itself. Under
// it the graph visibly thins out, and a user who is not told why will blame the
// tool (D12).
const lowCoverage = 0.7

func (a *app) expandCommand() *cobra.Command {
	var maxNodes, maxDepth int
	cmd := &cobra.Command{
		Use:   "expand",
		Short: "Follow references outward and grow the graph",
		Long: "Fetch the works your library cites but does not have yet, best first: a paper\n" +
			"cited by several of yours comes before one cited by a single paper.\n\n" +
			"Each run fetches at most --max-nodes works. Run it again to continue — it picks\n" +
			"up where the last run stopped, and stopping with Ctrl-C loses nothing already\n" +
			"fetched.",
		Example: "  fil expand\n  fil expand --max-nodes 2000 --max-depth 4",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.expand(cmd.Context(), maxNodes, maxDepth)
		},
	}
	cmd.Flags().IntVar(&maxNodes, "max-nodes", 0, "most works to fetch in this run (default from config, 500)")
	cmd.Flags().IntVar(&maxDepth, "max-depth", 0, "furthest to go from your own papers, in citation steps (default from config, 3)")
	return cmd
}

func (a *app) expand(ctx context.Context, maxNodes, maxDepth int) error {
	if maxNodes < 0 || maxDepth < 0 {
		return usageError{err: errors.New("--max-nodes and --max-depth must not be negative")}
	}
	cfg, err := a.loadConfig()
	if err != nil {
		return err
	}
	if cfg.FirstRun {
		// Nothing to grow, and no reason to create a library to find that out.
		fmt.Fprintln(a.stdout, "Your library is empty. Add a paper first, for example:\n\n  fil add 10.7717/peerj.4375")
		return nil
	}

	lib, err := library.Open(ctx, cfg, library.Options{CacheDir: a.cacheDir, NoCache: a.noCache, Transport: a.transport})
	if err != nil {
		return err
	}
	defer lib.Close()

	if s, err := lib.Stats(ctx); err == nil && s.Works == 0 {
		fmt.Fprintln(a.stdout, "Your library is empty. Add a paper first, for example:\n\n  fil add 10.7717/peerj.4375")
		return nil
	}

	budget, _ := lib.Budget()
	if maxNodes > 0 {
		budget = maxNodes
	}
	opts := library.ExpandOptions{MaxNodes: maxNodes, MaxDepth: maxDepth}
	if a.interactive {
		opts.Progress = func(r model.ExpansionResult) {
			fmt.Fprintf(a.stderr, "\r  fetched %s of up to %s · %s citations recorded ",
				thousands(r.Hydrated), thousands(budget), thousands(r.Edges))
		}
	}

	res, runErr := lib.Expand(ctx, opts)
	if a.interactive && res.Hydrated > 0 {
		fmt.Fprintln(a.stderr)
	}

	_, configuredDepth := lib.Budget()
	a.printExpansion(res, maxDepth, configuredDepth)
	if s, err := lib.Stats(context.WithoutCancel(ctx)); err == nil {
		fmt.Fprintf(a.stdout, "Library:   %s works (%s not fetched yet), %s citations.\n",
			thousands(s.Works), thousands(s.Stubs), thousands(s.Edges))
	}
	if q, ok := lib.Quota(); ok && q.Limit > 0 {
		fmt.Fprintf(a.stdout, "OpenAlex:  %s of %s credits left today.\n", thousands(q.Remaining), thousands(q.Limit))
	}

	if runErr != nil {
		fmt.Fprintln(a.stdout, "\nEverything fetched before the failure is saved; run fil expand to continue.")
		return runErr
	}
	if res.StoppedBecause == model.StopCancelled {
		return errCancelled
	}
	return nil
}

func (a *app) printExpansion(res model.ExpansionResult, maxDepth, configuredDepth int) {
	w := a.stdout
	fmt.Fprintf(w, "Fetched:   %s works in %s, recording %s new citations.\n",
		thousands(res.Hydrated), res.Duration.Round(100*time.Millisecond), thousands(res.Edges))

	if res.Hydrated > 0 {
		cov := res.ReferenceCoverage()
		fmt.Fprintf(w, "Coverage:  %s of %s had a reference list (%.0f%%).\n",
			thousands(res.Hydrated-res.DeadEnds), thousands(res.Hydrated), 100*cov)
		if cov < lowCoverage {
			fmt.Fprint(w, "           Low coverage is usually the field, not fil: OpenAlex rarely has\n"+
				"           reference lists for books and chapters, which is most of arts and\n"+
				"           humanities. The graph thins out where the data does.\n")
		}
	}
	if res.Unresolved > 0 {
		fmt.Fprintf(w, "Not found: %s no OpenAlex record. The citations stay; fil\n"+
			"           will not ask for them again.\n",
			plural(res.Unresolved, "cited work has", "cited works have"))
	}
	if res.Merged > 0 {
		fmt.Fprintf(w, "Merged:    %s twice by OpenAlex (same DOI, two records), kept\n"+
			"           as one node.\n", plural(res.Merged, "work listed", "works listed"))
	}
	if res.Skipped > 0 {
		fmt.Fprintf(w, "Skipped:   %s whose request failed; queued for the next run.\n",
			plural(res.Skipped, "work", "works"))
	}

	switch res.StoppedBecause {
	case model.StopBudgetExhausted:
		fmt.Fprintln(w, "Stopped:   budget reached. Run fil expand again to continue, or raise --max-nodes.")
	case model.StopFrontierEmpty:
		fmt.Fprintln(w, "Stopped:   nothing left to fetch — every reference within reach is in your library.")
	case model.StopMaxDepth:
		if maxDepth == 0 {
			maxDepth = configuredDepth
		}
		fmt.Fprintf(w, "Stopped:   what is left is more than %d citation steps from your papers.\n"+
			"           Pass --max-depth %d to go further.\n", maxDepth, maxDepth+1)
	case model.StopCancelled:
		fmt.Fprintln(w, "Stopped:   interrupted. Everything fetched is saved; run fil expand to continue.")
	}
}

// plural formats a count with the right noun: "1 work", "2 works".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return thousands(n) + " " + many
}
