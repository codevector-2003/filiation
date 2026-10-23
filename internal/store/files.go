package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/codevector-2003/filiation/internal/errs"
)

// Folder is a row of work_folder: a folder in papers/ named for one work.
type Folder struct {
	Path      string // relative to the library folder
	FilID     string
	GivenName string // the name fil gave it; empty when a person named it
	Missing   bool
}

// File is a row of work_file: a file that belongs to a work.
type File struct {
	Path    string // relative to the library folder
	FilID   string
	SHA256  string
	Size    int64
	ModTime int64  // Unix seconds when it was hashed
	Version string // published | accepted | preprint, or empty
	Origin  string // download | import | found
	Missing bool
}

// File origins.
const (
	OriginDownload = "download"
	OriginImport   = "import"
	OriginFound    = "found"
)

const folderColumns = `path, fil_id, ifnull(given_name, ''), missing`

const fileColumns = `path, fil_id, sha256, size, ifnull(mtime, 0), ifnull(version, ''), origin, missing`

// Folders returns every work folder the library knows, in path order.
func (db *DB) Folders(ctx context.Context) ([]Folder, error) {
	return queryFolders(ctx, db.read, `SELECT `+folderColumns+` FROM work_folder ORDER BY path;`)
}

// FoldersOf returns the folders of one work, found ones first.
func (db *DB) FoldersOf(ctx context.Context, filID string) ([]Folder, error) {
	return queryFolders(ctx, db.read,
		`SELECT `+folderColumns+` FROM work_folder WHERE fil_id = ? ORDER BY missing, path;`, filID)
}

// Files returns every file the library knows, in path order.
func (db *DB) Files(ctx context.Context) ([]File, error) {
	return queryFiles(ctx, db.read, `SELECT `+fileColumns+` FROM work_file ORDER BY path;`)
}

// FilesOf returns the files of one work, found ones first.
func (db *DB) FilesOf(ctx context.Context, filID string) ([]File, error) {
	return queryFiles(ctx, db.read,
		`SELECT `+fileColumns+` FROM work_file WHERE fil_id = ? ORDER BY missing, path;`, filID)
}

// FilesWithHash returns the files holding exactly these bytes, which is how a
// download or an import learns it already has a paper.
func (db *DB) FilesWithHash(ctx context.Context, sha256 string) ([]File, error) {
	return queryFiles(ctx, db.read,
		`SELECT `+fileColumns+` FROM work_file WHERE sha256 = ? ORDER BY missing, path;`, strings.ToLower(sha256))
}

// PutFolder records a work folder, replacing what was recorded at its path.
func (tx *Tx) PutFolder(ctx context.Context, f Folder) error {
	_, err := tx.tx.ExecContext(ctx, `
INSERT INTO work_folder (path, fil_id, given_name, missing) VALUES (?, ?, ?, ?)
ON CONFLICT (path) DO UPDATE SET
    fil_id = excluded.fil_id, given_name = excluded.given_name, missing = excluded.missing;`,
		f.Path, f.FilID, nullString(f.GivenName), boolToInt(f.Missing))
	if err != nil {
		return fmt.Errorf("store: record folder %s: %w", f.Path, err)
	}
	return nil
}

// MoveFolder records that the folder at from is now at to, and moves the
// recorded paths of every file inside it with it.
func (tx *Tx) MoveFolder(ctx context.Context, from string, to Folder) error {
	if from != to.Path {
		if _, err := tx.tx.ExecContext(ctx, `DELETE FROM work_folder WHERE path = ?;`, from); err != nil {
			return fmt.Errorf("store: move folder %s: %w", from, err)
		}
		// Files under the old path take the new prefix. substr is 1-based.
		if _, err := tx.tx.ExecContext(ctx, `
UPDATE work_file SET path = ?2 || substr(path, length(?1) + 1)
WHERE substr(path, 1, length(?1) + 1) = ?1 || '/';`, from, to.Path); err != nil {
			return fmt.Errorf("store: move files of %s: %w", from, err)
		}
	}
	return tx.PutFolder(ctx, to)
}

// DeleteFolder forgets a folder. Its files' rows are left for the caller.
func (tx *Tx) DeleteFolder(ctx context.Context, path string) error {
	if _, err := tx.tx.ExecContext(ctx, `DELETE FROM work_folder WHERE path = ?;`, path); err != nil {
		return fmt.Errorf("store: forget folder %s: %w", path, err)
	}
	return nil
}

// PutFile records a file, replacing what was recorded at its path. The hash is
// stored in lower case so lookups compare exactly.
func (tx *Tx) PutFile(ctx context.Context, f File) error {
	_, err := tx.tx.ExecContext(ctx, `
INSERT INTO work_file (path, fil_id, sha256, size, mtime, version, origin, missing)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (path) DO UPDATE SET
    fil_id = excluded.fil_id, sha256 = excluded.sha256, size = excluded.size,
    mtime = excluded.mtime, version = excluded.version, origin = excluded.origin,
    missing = excluded.missing;`,
		f.Path, f.FilID, strings.ToLower(f.SHA256), f.Size, f.ModTime,
		nullString(f.Version), f.Origin, boolToInt(f.Missing))
	if err != nil {
		return fmt.Errorf("store: record file %s: %w", f.Path, err)
	}
	return nil
}

// MoveFile records that the file at from is now at to.Path.
func (tx *Tx) MoveFile(ctx context.Context, from string, to File) error {
	if from != to.Path {
		if _, err := tx.tx.ExecContext(ctx, `DELETE FROM work_file WHERE path = ?;`, from); err != nil {
			return fmt.Errorf("store: move file %s: %w", from, err)
		}
	}
	return tx.PutFile(ctx, to)
}

// SyncFolderCollections makes the collections that mirror folders match want,
// a map from folder path to the collection name and its members' fil IDs. A
// folder collection not in want is deleted; the user's own collections are
// never touched, except that one whose name a folder now claims is adopted.
func (tx *Tx) SyncFolderCollections(ctx context.Context, want map[string]FolderCollection) error {
	rows, err := tx.tx.QueryContext(ctx, `SELECT collection_id, folder FROM folder_collection;`)
	if err != nil {
		return fmt.Errorf("store: list folder collections: %w", err)
	}
	have := map[string]int64{}
	for rows.Next() {
		var id int64
		var folder string
		if err := rows.Scan(&id, &folder); err != nil {
			rows.Close()
			return fmt.Errorf("store: list folder collections: %w", err)
		}
		have[folder] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("store: list folder collections: %w", err)
	}

	for folder, id := range have {
		if _, ok := want[folder]; !ok {
			if _, err := tx.tx.ExecContext(ctx, `DELETE FROM collection WHERE id = ?;`, id); err != nil {
				return fmt.Errorf("store: remove collection for %s: %w", folder, err)
			}
		}
	}
	for folder, fc := range want {
		id, ok := have[folder]
		if ok {
			if _, err := tx.tx.ExecContext(ctx,
				`UPDATE collection SET name = ? WHERE id = ?;`, fc.Name, id); err != nil {
				return fmt.Errorf("store: rename collection for %s: %w", folder, err)
			}
		} else {
			err := tx.tx.QueryRowContext(ctx, `
INSERT INTO collection (name) VALUES (?)
ON CONFLICT (name) DO UPDATE SET name = excluded.name
RETURNING id;`, fc.Name).Scan(&id)
			if err != nil {
				return fmt.Errorf("store: create collection for %s: %w", folder, err)
			}
			if _, err := tx.tx.ExecContext(ctx,
				`INSERT INTO folder_collection (collection_id, folder) VALUES (?, ?);`, id, folder); err != nil {
				return fmt.Errorf("store: link collection to %s: %w", folder, err)
			}
		}
		if _, err := tx.tx.ExecContext(ctx,
			`DELETE FROM collection_work WHERE collection_id = ?;`, id); err != nil {
			return fmt.Errorf("store: refresh collection for %s: %w", folder, err)
		}
		for _, fil := range fc.Works {
			if _, err := tx.tx.ExecContext(ctx,
				`INSERT OR IGNORE INTO collection_work (collection_id, work_id) VALUES (?, ?);`, id, fil); err != nil {
				return fmt.Errorf("store: fill collection for %s: %w", folder, err)
			}
		}
	}
	return nil
}

// FolderCollection is a collection as a folder defines it.
type FolderCollection struct {
	Name  string
	Works []string // fil IDs
}

// Collections returns every collection's name and member count, in name order.
func (db *DB) Collections(ctx context.Context) ([]CollectionSize, error) {
	rows, err := db.read.QueryContext(ctx, `
SELECT c.name, count(cw.work_id), fc.folder IS NOT NULL
FROM collection c
LEFT JOIN collection_work cw ON cw.collection_id = c.id
LEFT JOIN folder_collection fc ON fc.collection_id = c.id
GROUP BY c.id ORDER BY c.name;`)
	if err != nil {
		return nil, fmt.Errorf("store: list collections: %w", err)
	}
	defer rows.Close()
	var out []CollectionSize
	for rows.Next() {
		var c CollectionSize
		if err := rows.Scan(&c.Name, &c.Works, &c.FromFolder); err != nil {
			return nil, fmt.Errorf("store: list collections: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CollectionSize is one line of a collection listing.
type CollectionSize struct {
	Name       string
	Works      int
	FromFolder bool
}

func queryFolders(ctx context.Context, q queryer, query string, args ...any) ([]Folder, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list folders: %w", err)
	}
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.Path, &f.FilID, &f.GivenName, &f.Missing); err != nil {
			return nil, fmt.Errorf("store: list folders: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func queryFiles(ctx context.Context, q queryer, query string, args ...any) ([]File, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list files: %w", err)
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.Path, &f.FilID, &f.SHA256, &f.Size, &f.ModTime,
			&f.Version, &f.Origin, &f.Missing); err != nil {
			return nil, fmt.Errorf("store: list files: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ResolveRefs maps each ID to the fil ID of the work it names — its own fil
// ID, OpenAlex ID or an alias — leaving out IDs that name nothing. A scan uses
// it to read the fil IDs in folder names, some of which a fold has turned into
// aliases.
func (db *DB) ResolveRefs(ctx context.Context, refs []string) (map[string]string, error) {
	out := make(map[string]string, len(refs))
	for _, ref := range refs {
		fil, err := resolveRef(ctx, db.read, ref)
		if err != nil {
			if errors.Is(err, errs.ErrNotFound) {
				continue
			}
			return nil, err
		}
		out[ref] = fil
	}
	return out, nil
}

// CopyTo writes a consistent copy of the whole library to path, which must not
// exist. VACUUM INTO reads one snapshot, so it includes what is still in the
// write-ahead log and is safe while readers are open.
func (db *DB) CopyTo(ctx context.Context, path string) error {
	if _, err := db.write.ExecContext(ctx, `VACUUM INTO ?;`, path); err != nil {
		return fmt.Errorf("store: copy library to %s: %w", path, err)
	}
	return nil
}

// Check runs SQLite's integrity check and reports the row counts that a copy
// must reproduce.
func (db *DB) Check(ctx context.Context) (Counts, error) {
	var res string
	if err := db.read.QueryRowContext(ctx, `PRAGMA integrity_check;`).Scan(&res); err != nil {
		return Counts{}, fmt.Errorf("store: integrity check: %w", err)
	}
	if res != "ok" {
		return Counts{}, fmt.Errorf("store: integrity check: %s", res)
	}
	var c Counts
	err := db.read.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM work), (SELECT count(*) FROM cites),
       (SELECT count(*) FROM work_file), (SELECT count(*) FROM note);`).Scan(&c.Works, &c.Cites, &c.Files, &c.Notes)
	if err != nil {
		return Counts{}, fmt.Errorf("store: count rows: %w", err)
	}
	return c, nil
}

// Counts are the row counts Check compares.
type Counts struct {
	Works, Cites, Files, Notes int
}
