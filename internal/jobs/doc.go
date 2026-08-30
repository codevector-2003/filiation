// Package jobs runs long work in the background and survives interruption.
//
// Expanding a graph or fetching a hundred PDFs takes minutes, and will be
// cancelled, crash, or lose the network. Job state is recorded in SQLite, so a
// resumed run continues from the last completed unit instead of starting over.
//
// This package owns the write path (see Concurrency in CLAUDE.md): one worker
// goroutine holds the write handle and everything else reads. context.Context
// is threaded through every unit of work — cancelling an expansion has to stop
// it, not merely stop reporting on it.
package jobs
