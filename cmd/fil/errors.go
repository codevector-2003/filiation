package main

import (
	"errors"
	"fmt"
	"io"

	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/library"
)

// Exit codes. Each sentinel in internal/errs gets its own, because each asks the
// user — or the script calling fil — to do something different.
const (
	exitOK           = 0
	exitFailure      = 1 // anything not listed below
	exitInvalidInput = 2 // retype the argument
	exitAmbiguous    = 3 // choose a candidate, or pass --accept-first
	exitUnresolved   = 4 // OpenAlex has no such work; retrying will not help
	exitTransient    = 5 // network or rate limit; retrying later may
	exitConfig       = 6 // fix the config file
	exitSchemaTooNew = 7 // upgrade fil
)

// report prints err with the one sentence of advice its class calls for, and
// returns the exit code. configPath is named when the fix is to edit it.
func report(w io.Writer, err error, configPath string) int {
	fmt.Fprintf(w, "fil: %v\n", err)

	switch {
	case errors.Is(err, errs.ErrInvalidInput):
		fmt.Fprintln(w, "\nfil add takes a DOI (10.7717/peerj.4375), an arXiv ID (1706.03762), a PMID\n"+
			"(PMID:29051481), an OpenAlex ID (W2741809807), a link to any of those, or a title.")
		return exitInvalidInput

	case errors.Is(err, errs.ErrAmbiguous):
		fmt.Fprintln(w, "\nRun fil in a terminal to choose one, or pass --accept-first to take the first.")
		return exitAmbiguous

	case errors.Is(err, errs.ErrUnresolved):
		fmt.Fprintln(w, "\nOpenAlex has no record of it. Check for a typo, or try the paper's DOI.")
		return exitUnresolved

	case errors.Is(err, errs.ErrTransient):
		if d, ok := library.RetryAfter(err); ok {
			fmt.Fprintf(w, "\nOpenAlex asked us to wait %s, which usually means today's allowance is spent.\n"+
				"Nothing was written. Try again after that.\n", d)
		} else {
			fmt.Fprintln(w, "\nNothing was written. Check your connection and try again.")
		}
		return exitTransient

	case errors.Is(err, errs.ErrInvalidConfig):
		if configPath != "" {
			fmt.Fprintf(w, "\nFix or delete %s and run fil again.\n", configPath)
		}
		return exitConfig

	case errors.Is(err, errs.ErrSchemaTooNew):
		fmt.Fprintln(w, "\nThis library was written by a newer fil. Upgrade fil; nothing was changed.")
		return exitSchemaTooNew
	}
	return exitFailure
}
