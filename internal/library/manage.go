package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/codevector-2003/filiation/internal/blobs"
	"github.com/codevector-2003/filiation/internal/config"
	"github.com/codevector-2003/filiation/internal/errs"
	"github.com/codevector-2003/filiation/internal/store"
)

// Create makes a new library folder at dir, with its database, and lists it
// in the global settings — as the default when makeDefault is set. The library
// cfg names is left as it is.
func Create(ctx context.Context, cfg *config.Config, dir string, makeDefault bool) (*config.Config, error) {
	nc := *cfg
	nc.FirstRun = false
	if err := nc.UseFolder(dir); err != nil {
		return nil, err
	}
	if err := emptyOrAbsent(nc.Folder); err != nil {
		return nil, err
	}
	if _, err := os.Stat(nc.DBPath); err == nil {
		return nil, fmt.Errorf("a library database is already at %s: %w", nc.DBPath, errs.ErrInvalidInput)
	}
	if makeDefault {
		nc.MakeDefault()
	}
	if err := nc.Save(); err != nil {
		return nil, err
	}
	lib, err := Open(ctx, &nc, Options{NoCache: true})
	if err != nil {
		return nil, err
	}
	return &nc, lib.Close()
}

// Relocated is what Move did.
type Relocated struct {
	Folder   string // the library folder now
	Database string // the database now
	Cloud    string // the sync service the folder is in, if any

	// OldDatabase is the v0.2 database that was copied. It is left where it
	// was, for the person to delete once they are happy.
	OldDatabase string
}

// Move gives the library cfg names a new folder.
//
// A v0.2 library — a database with no folder — gets a library folder at dir:
// papers/, Inbox/ and exports/ are created, the database is copied into place
// and checked against the original, and the global settings are updated to
// open the new one. The old database is left where it was.
//
// A library that already has a folder has it moved to dir, which must be on
// the same drive: moving gigabytes of PDFs is a job for the file manager, and
// a rename either happens or does not. If the database belongs in a different
// place at the new folder — in or out of a cloud sync service — it is copied
// there and checked, and the old copy removed.
//
// Nothing else may have the library open while it moves.
func Move(ctx context.Context, cfg *config.Config, dir string) (Relocated, error) {
	nc := *cfg
	if err := nc.UseFolder(dir); err != nil {
		return Relocated{}, err
	}
	if cfg.Folder == "" {
		return moveDatabaseOnly(ctx, cfg, &nc)
	}

	if err := emptyOrAbsent(nc.Folder); err != nil {
		return Relocated{}, err
	}
	if err := os.Remove(nc.Folder); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Relocated{}, fmt.Errorf("move library: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(nc.Folder), 0o755); err != nil {
		return Relocated{}, fmt.Errorf("move library: %w", err)
	}
	if err := os.Rename(cfg.Folder, nc.Folder); err != nil {
		return Relocated{}, fmt.Errorf("move %s to %s: %v — move it with your file manager instead, "+
			"then run fil library new on the new folder: %w", cfg.Folder, nc.Folder, err, errs.ErrInvalidInput)
	}

	// The database moved with the folder if it was inside it.
	current := cfg.DBPath
	if rel, err := filepath.Rel(cfg.Folder, cfg.DBPath); err == nil && filepath.IsLocal(rel) {
		current = filepath.Join(nc.Folder, rel)
	}
	if current != nc.DBPath {
		if err := copyDatabase(ctx, current, nc.DBPath); err != nil {
			return Relocated{}, err
		}
		removeDatabase(current)
	}

	nc.Name = cfg.Name
	nc.Forget(cfg.Folder)
	if err := nc.Save(); err != nil {
		return Relocated{}, err
	}
	return Relocated{Folder: nc.Folder, Database: nc.DBPath, Cloud: nc.Cloud}, nil
}

func moveDatabaseOnly(ctx context.Context, cfg, nc *config.Config) (Relocated, error) {
	if err := emptyOrAbsent(nc.Folder); err != nil {
		return Relocated{}, err
	}
	if _, err := os.Stat(cfg.DBPath); err != nil {
		return Relocated{}, fmt.Errorf("no library at %s to move: %w", cfg.DBPath, errs.ErrNotFound)
	}
	if _, err := os.Stat(nc.DBPath); err == nil {
		return Relocated{}, fmt.Errorf("a library database is already at %s: %w", nc.DBPath, errs.ErrInvalidInput)
	}
	if _, err := blobs.Open(nc.Folder); err != nil {
		return Relocated{}, err
	}
	if err := copyDatabase(ctx, cfg.DBPath, nc.DBPath); err != nil {
		return Relocated{}, err
	}
	if cfg.IsRecordedDatabase(cfg.DBPath) {
		nc.DropDatabaseOnly()
		nc.MakeDefault()
	}
	nc.FirstRun = false
	if err := nc.Save(); err != nil {
		return Relocated{}, err
	}
	return Relocated{Folder: nc.Folder, Database: nc.DBPath, Cloud: nc.Cloud, OldDatabase: cfg.DBPath}, nil
}

// copyDatabase copies the library at from to to, upgrading it first, and
// checks the copy: an integrity check, and the same number of works,
// citations, files and notes. A copy that fails the check is deleted.
func copyDatabase(ctx context.Context, from, to string) error {
	src, err := store.Open(ctx, from)
	if err != nil {
		return err
	}
	defer src.Close()
	if err := src.Migrate(ctx); err != nil {
		return err
	}
	want, err := src.Check(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return fmt.Errorf("copy library: %w", err)
	}
	if err := src.CopyTo(ctx, to); err != nil {
		return err
	}
	dst, err := store.Open(ctx, to)
	if err != nil {
		removeDatabase(to)
		return err
	}
	got, err := dst.Check(ctx)
	dst.Close()
	if err == nil && got != want {
		err = fmt.Errorf("the copy at %s has %+v, the original %+v", to, got, want)
	}
	if err != nil {
		removeDatabase(to)
		return fmt.Errorf("copy library: %w", err)
	}
	return nil
}

// removeDatabase deletes a database fil placed, with its WAL files.
func removeDatabase(path string) {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(path + suffix)
	}
}

// emptyOrAbsent refuses a folder that already holds something, so that a new
// library never mixes itself into a person's files.
func emptyOrAbsent(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check %s: %w", dir, err)
	}
	if len(entries) > 0 {
		return fmt.Errorf("%s is not empty — choose a new folder, or an empty one: %w", dir, errs.ErrInvalidInput)
	}
	return nil
}
