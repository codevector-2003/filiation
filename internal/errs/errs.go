package errs

import "errors"

// The sentinels below are the vocabulary the CLI acts on. Each one exists
// because some caller has to make a different decision when it sees it — a
// sentinel nobody branches on is noise, so this list is deliberately short and
// should stay that way.
//
// Return them wrapped, never bare, so the message keeps the context the user
// needs:
//
//	return fmt.Errorf("hydrate %s: %w", id, errs.ErrUnresolved)
//
// and compare with errors.Is, never with ==, or the wrapping breaks the check.
var (
	// ErrNotFound means this library has no such row.
	//
	// It is about the local database only. "OpenAlex has never heard of this
	// work" is ErrUnresolved, and the two must not be conflated: this one is
	// answered by adding the work, that one can never be answered at all.
	ErrNotFound = errors.New("not found")

	// ErrTransient means the operation failed for a reason that may not recur:
	// HTTP 429, any 5xx, a dropped connection, a timeout.
	//
	// It is the expander's continue condition. §7 requires that one bad batch
	// must not kill a 500-node run, so the loop skips the batch and carries on
	// rather than returning — the works stay stubs and the next run picks them
	// up off the frontier, because the frontier is a query and not a queue.
	//
	// A 429 also carries a Retry-After that the backoff needs. That duration is
	// not threaded through this sentinel; internal/httpx owns retry timing and
	// exposes it separately, so that a package deciding *whether* to retry does
	// not have to understand *when*.
	ErrTransient = errors.New("transient failure")

	// ErrUnresolved means the upstream index has no record of an identifier we
	// know is real, because some other paper cited it.
	//
	// This is permanent, and that is the whole point of separating it from
	// ErrNotFound: the store sets unresolved = 1 and expansion stops spending
	// budget on it. The citation edge stays. It remains true that the paper was
	// cited, whatever OpenAlex knows, and deleting the edge would silently
	// falsify the graph (§7).
	ErrUnresolved = errors.New("unresolved identifier")

	// ErrAmbiguous means the input matched more than one work and picking one is
	// not ours to do.
	//
	// Only title search raises it: DOI, arXiv ID, PMID and OpenAlex ID all
	// resolve deterministically. Per ADR-005 the CLI must present the candidates
	// and wait, or be given --accept-first explicitly. The asymmetry justifies
	// the extra keystroke — a wrong seed is not a small error, it poisons every
	// node expanded from it and the user may not notice for weeks.
	ErrAmbiguous = errors.New("ambiguous identifier")

	// ErrInvalidConfig means the configuration was read but is not usable: a
	// malformed TOML file, a budget of zero, an unwritable library path.
	//
	// It is separate from every other failure because the CLI answers it
	// differently. This is the one error class the user can fix themselves, and
	// the only useful response is to name the file and stop — so the front door
	// prints the config path and exits, rather than reporting a failure the user
	// has no way to act on. Falling back to defaults instead would be worse: a
	// typo in max_nodes would silently expand a graph the user did not ask for.
	ErrInvalidConfig = errors.New("invalid configuration")

	// ErrSchemaTooNew means the library was written by a newer build of fil
	// than the one now running, and must not be touched.
	//
	// It is a separate error because it is the one failure where doing nothing
	// is the whole point. A newer schema may maintain state this build knows
	// nothing about — a denormalised counter, a changed trigger — and writes
	// that look perfectly valid here would leave it quietly inconsistent, with
	// no error for the user to notice.
	//
	// The check can only ever run in the older binary, which is why it ships
	// from the first release: a guard added later cannot reach a copy of fil
	// somebody already downloaded. It matters more here than in most tools
	// because ADR-006 points every build on the machine at the same library
	// file, so an old binary meeting a new library is ordinary, not exotic.
	ErrSchemaTooNew = errors.New("library schema is newer than this build")
)

// There is deliberately no ErrBudgetExhausted.
//
// An expansion that stops on its budget did exactly what it was asked to do, so
// returning an error would make success look like failure and force every
// caller to special-case it. The outcome is reported instead as
// model.StopBudgetExhausted on ExpansionResult, alongside the three other
// perfectly normal ways a run ends. See §7: "budget exhausted mid-expansion —
// normal, not an error."
