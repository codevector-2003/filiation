package store

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/codevector-2003/filiation/internal/errs"
)

// Collection is one collection, where it sits, and how many papers it holds.
type Collection struct {
	ID         int64
	Parent     int64  // 0 at the top
	Name       string // as stored: a folder collection's is its path in papers/
	Short      string // as a tree shows it, inside its parent
	Works      int    // distinct papers in it and in every collection inside it
	FromFolder bool   // mirrors a folder in papers/, so only moving folders changes it
	Zotero     string // the Zotero collection it was brought in from, if any
}

// Collections returns the library's collections, each after the one it sits
// in, siblings in name order.
//
// The user's collections nest by parent_id. Folder collections all sit at the
// top in the table, named by their path, and nest here instead: papers/A/B
// sits inside papers/A when that folder is a collection too.
func (db *DB) Collections(ctx context.Context) ([]Collection, error) {
	cs, err := collectionTree(ctx, db.read)
	if err != nil {
		return nil, err
	}
	rows, err := db.read.QueryContext(ctx, `SELECT collection_id, work_id FROM collection_work;`)
	if err != nil {
		return nil, fmt.Errorf("store: count collections: %w", err)
	}
	defer rows.Close()
	in := map[int64][]string{}
	for rows.Next() {
		var id int64
		var work string
		if err := rows.Scan(&id, &work); err != nil {
			return nil, fmt.Errorf("store: count collections: %w", err)
		}
		in[id] = append(in[id], work)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: count collections: %w", err)
	}
	for i := range cs {
		seen := map[string]bool{}
		for _, id := range within(cs, cs[i].ID) {
			for _, w := range in[id] {
				seen[w] = true
			}
		}
		cs[i].Works = len(seen)
	}
	return cs, nil
}

// collectionTree reads every collection and orders them as a tree.
func collectionTree(ctx context.Context, q queryer) ([]Collection, error) {
	rows, err := q.QueryContext(ctx, `
SELECT c.id, ifnull(c.parent_id, 0), c.name, fc.folder IS NOT NULL, ifnull(fc.folder, ''), ifnull(c.zotero_key, '')
FROM collection c LEFT JOIN folder_collection fc ON fc.collection_id = c.id;`)
	if err != nil {
		return nil, fmt.Errorf("store: list collections: %w", err)
	}
	defer rows.Close()
	var all []Collection
	folders := map[string]int{} // folder path to its index in all
	for rows.Next() {
		var c Collection
		var folder string
		if err := rows.Scan(&c.ID, &c.Parent, &c.Name, &c.FromFolder, &folder, &c.Zotero); err != nil {
			return nil, fmt.Errorf("store: list collections: %w", err)
		}
		c.Short = c.Name
		if folder != "" {
			folders[folder] = len(all)
		}
		all = append(all, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list collections: %w", err)
	}
	for folder, i := range folders {
		if all[i].Parent != 0 {
			continue
		}
		for up := folder; strings.Contains(up, "/"); {
			up = up[:strings.LastIndex(up, "/")]
			if j, ok := folders[up]; ok {
				all[i].Parent = all[j].ID
				all[i].Short = strings.TrimPrefix(all[i].Name, all[j].Name+"/")
				break
			}
		}
	}
	slices.SortFunc(all, func(a, b Collection) int {
		return cmp.Or(cmp.Compare(strings.ToLower(a.Short), strings.ToLower(b.Short)), cmp.Compare(a.ID, b.ID))
	})
	kids := map[int64][]Collection{}
	for _, c := range all {
		kids[c.Parent] = append(kids[c.Parent], c)
	}
	out := make([]Collection, 0, len(all))
	var walk func(parent int64)
	walk = func(parent int64) {
		for _, c := range kids[parent] {
			out = append(out, c)
			walk(c.ID)
		}
	}
	walk(0)
	return out, nil
}

// within is id and every collection inside it, however deep.
func within(cs []Collection, id int64) []int64 {
	out := []int64{id}
	for i := 0; i < len(out); i++ {
		for _, c := range cs {
			if c.Parent == out[i] {
				out = append(out, c.ID)
			}
		}
	}
	return out
}

// collectionIn finds one collection, as the tree places it.
func collectionIn(ctx context.Context, q queryer, id int64) (Collection, []Collection, error) {
	cs, err := collectionTree(ctx, q)
	if err != nil {
		return Collection{}, nil, err
	}
	for _, c := range cs {
		if c.ID == id {
			return c, cs, nil
		}
	}
	return Collection{}, nil, fmt.Errorf("no collection %d: %w", id, errs.ErrNotFound)
}

// errFromFolder is why a folder collection cannot be changed here.
func errFromFolder(c Collection) error {
	return fmt.Errorf("%q is a folder in papers/: move or rename the folder instead: %w", c.Name, errs.ErrInvalidInput)
}

// userCollection finds a collection the user may change: one of theirs, not
// a folder's.
func (tx *Tx) userCollection(ctx context.Context, id int64) (Collection, []Collection, error) {
	c, cs, err := collectionIn(ctx, tx.tx, id)
	if err != nil {
		return Collection{}, nil, err
	}
	if c.FromFolder {
		return Collection{}, nil, errFromFolder(c)
	}
	return c, cs, nil
}

// nameFree checks no collection beside the new place already has the name.
func (tx *Tx) nameFree(ctx context.Context, parent int64, name string, except int64) error {
	var n int
	if err := tx.tx.QueryRowContext(ctx,
		`SELECT count(*) FROM collection WHERE ifnull(parent_id, 0) = ? AND name = ? AND id <> ?;`,
		parent, name, except).Scan(&n); err != nil {
		return fmt.Errorf("store: look for collection %q: %w", name, err)
	}
	if n > 0 {
		return fmt.Errorf("there is already a collection called %q there: %w", name, errs.ErrInvalidInput)
	}
	return nil
}

// parentFor checks a collection may go inside parent: one of the user's, or
// the top.
func (tx *Tx) parentFor(ctx context.Context, parent int64) error {
	if parent == 0 {
		return nil
	}
	_, _, err := tx.userCollection(ctx, parent)
	return err
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// CreateCollection makes a collection inside parent (0: at the top) and
// returns its ID.
func (tx *Tx) CreateCollection(ctx context.Context, name string, parent int64) (int64, error) {
	if err := tx.parentFor(ctx, parent); err != nil {
		return 0, err
	}
	if err := tx.nameFree(ctx, parent, name, 0); err != nil {
		return 0, err
	}
	var id int64
	if err := tx.tx.QueryRowContext(ctx, `INSERT INTO collection (parent_id, name) VALUES (?, ?) RETURNING id;`,
		nullID(parent), name).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: make collection %q: %w", name, err)
	}
	return id, nil
}

// RenameCollection renames one of the user's collections.
func (tx *Tx) RenameCollection(ctx context.Context, id int64, name string) error {
	c, _, err := tx.userCollection(ctx, id)
	if err != nil {
		return err
	}
	if err := tx.nameFree(ctx, c.Parent, name, id); err != nil {
		return err
	}
	if _, err := tx.tx.ExecContext(ctx, `UPDATE collection SET name = ? WHERE id = ?;`, name, id); err != nil {
		return fmt.Errorf("store: rename collection: %w", err)
	}
	return nil
}

// MoveCollection puts one of the user's collections inside another (0: at
// the top). A collection cannot go inside itself or one of its own.
func (tx *Tx) MoveCollection(ctx context.Context, id, parent int64) error {
	c, cs, err := tx.userCollection(ctx, id)
	if err != nil {
		return err
	}
	if slices.Contains(within(cs, id), parent) {
		return fmt.Errorf("a collection cannot go inside itself: %w", errs.ErrInvalidInput)
	}
	if err := tx.parentFor(ctx, parent); err != nil {
		return err
	}
	if err := tx.nameFree(ctx, parent, c.Name, id); err != nil {
		return err
	}
	if _, err := tx.tx.ExecContext(ctx, `UPDATE collection SET parent_id = ? WHERE id = ?;`, nullID(parent), id); err != nil {
		return fmt.Errorf("store: move collection: %w", err)
	}
	return nil
}

// DeleteCollection deletes one of the user's collections and every collection
// inside it. The papers stay in the library.
func (tx *Tx) DeleteCollection(ctx context.Context, id int64) error {
	if _, _, err := tx.userCollection(ctx, id); err != nil {
		return err
	}
	if _, err := tx.tx.ExecContext(ctx, `DELETE FROM collection WHERE id = ?;`, id); err != nil {
		return fmt.Errorf("store: delete collection: %w", err)
	}
	return nil
}

// FileInCollection adds papers to one of the user's collections, or takes
// them out. A paper already there, or not there, is passed over.
func (tx *Tx) FileInCollection(ctx context.Context, id int64, works []string, in bool) error {
	if _, _, err := tx.userCollection(ctx, id); err != nil {
		return err
	}
	q := `INSERT OR IGNORE INTO collection_work (collection_id, work_id) VALUES (?, ?);`
	if !in {
		q = `DELETE FROM collection_work WHERE collection_id = ? AND work_id = ?;`
	}
	return tx.eachWork(ctx, works, q, id)
}

// eachWork runs q for each of works, after the leading args, once it is sure
// every one is in the library.
func (tx *Tx) eachWork(ctx context.Context, works []string, q string, args ...any) error {
	for _, w := range works {
		var n int
		if err := tx.tx.QueryRowContext(ctx, `SELECT count(*) FROM work WHERE fil_id = ?;`, w).Scan(&n); err != nil {
			return fmt.Errorf("store: find %s: %w", w, err)
		}
		if n == 0 {
			return fmt.Errorf("no paper %s: %w", w, errs.ErrNotFound)
		}
		if _, err := tx.tx.ExecContext(ctx, q, append(slices.Clone(args), w)...); err != nil {
			return fmt.Errorf("store: file %s: %w", w, err)
		}
	}
	return nil
}

// Label is a label the user puts on papers, and how many carry it.
type Label struct {
	ID     int64
	Name   string
	Colour string
	Works  int
}

// Labels returns every label, in name order.
func (db *DB) Labels(ctx context.Context) ([]Label, error) {
	rows, err := db.read.QueryContext(ctx, `
SELECT l.id, l.name, l.colour, count(wl.work_id)
FROM label l LEFT JOIN work_label wl ON wl.label_id = l.id
GROUP BY l.id ORDER BY l.name COLLATE NOCASE, l.id;`)
	if err != nil {
		return nil, fmt.Errorf("store: list labels: %w", err)
	}
	defer rows.Close()
	var out []Label
	for rows.Next() {
		var l Label
		if err := rows.Scan(&l.ID, &l.Name, &l.Colour, &l.Works); err != nil {
			return nil, fmt.Errorf("store: list labels: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (tx *Tx) labelFree(ctx context.Context, name string, except int64) error {
	var n int
	if err := tx.tx.QueryRowContext(ctx, `SELECT count(*) FROM label WHERE name = ? AND id <> ?;`, name, except).Scan(&n); err != nil {
		return fmt.Errorf("store: look for label %q: %w", name, err)
	}
	if n > 0 {
		return fmt.Errorf("there is already a label called %q: %w", name, errs.ErrInvalidInput)
	}
	return nil
}

// CreateLabel makes a label and returns its ID.
func (tx *Tx) CreateLabel(ctx context.Context, name, colour string) (int64, error) {
	if err := tx.labelFree(ctx, name, 0); err != nil {
		return 0, err
	}
	var id int64
	if err := tx.tx.QueryRowContext(ctx, `INSERT INTO label (name, colour) VALUES (?, ?) RETURNING id;`,
		name, colour).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: make label %q: %w", name, err)
	}
	return id, nil
}

// ChangeLabel renames a label and sets its colour.
func (tx *Tx) ChangeLabel(ctx context.Context, id int64, name, colour string) error {
	if err := tx.labelFree(ctx, name, id); err != nil {
		return err
	}
	res, err := tx.tx.ExecContext(ctx, `UPDATE label SET name = ?, colour = ? WHERE id = ?;`, name, colour, id)
	if err != nil {
		return fmt.Errorf("store: change label: %w", err)
	}
	return oneLabel(res, id)
}

// DeleteLabel deletes a label. The papers it was on stay as they were.
func (tx *Tx) DeleteLabel(ctx context.Context, id int64) error {
	res, err := tx.tx.ExecContext(ctx, `DELETE FROM label WHERE id = ?;`, id)
	if err != nil {
		return fmt.Errorf("store: delete label: %w", err)
	}
	return oneLabel(res, id)
}

func oneLabel(res sql.Result, id int64) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: change label: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("no label %d: %w", id, errs.ErrNotFound)
	}
	return nil
}

// SetLabel puts a label on papers, or takes it off.
func (tx *Tx) SetLabel(ctx context.Context, id int64, works []string, on bool) error {
	var n int
	if err := tx.tx.QueryRowContext(ctx, `SELECT count(*) FROM label WHERE id = ?;`, id).Scan(&n); err != nil {
		return fmt.Errorf("store: find label: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("no label %d: %w", id, errs.ErrNotFound)
	}
	q := `INSERT OR IGNORE INTO work_label (label_id, work_id) VALUES (?, ?);`
	if !on {
		q = `DELETE FROM work_label WHERE label_id = ? AND work_id = ?;`
	}
	return tx.eachWork(ctx, works, q, id)
}

// SetFavourite stars papers, or unstars them.
func (tx *Tx) SetFavourite(ctx context.Context, works []string, on bool) error {
	q := `INSERT OR IGNORE INTO favourite (work_id) VALUES (?);`
	if !on {
		q = `DELETE FROM favourite WHERE work_id = ?;`
	}
	return tx.eachWork(ctx, works, q)
}

// Favourites counts the starred papers.
func (db *DB) Favourites(ctx context.Context) (int, error) {
	var n int
	if err := db.read.QueryRowContext(ctx, `SELECT count(*) FROM favourite;`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count favourites: %w", err)
	}
	return n, nil
}

// Organised is how the user has filed one paper.
type Organised struct {
	Favourite   bool
	Collections []int64 // the collections it is in itself, not through one inside
	Labels      []int64
	Zotero      string // "" not from Zotero; matched, or unmatched
}

// OrganisedOf says how the user has filed a paper.
func (db *DB) OrganisedOf(ctx context.Context, filID string) (Organised, error) {
	var out Organised
	var cs, ls string
	err := db.read.QueryRowContext(ctx, `
SELECT EXISTS (SELECT 1 FROM favourite WHERE work_id = ?1),
       ifnull((SELECT group_concat(collection_id) FROM collection_work WHERE work_id = ?1), ''),
       ifnull((SELECT group_concat(label_id) FROM work_label WHERE work_id = ?1), ''),
       ifnull((SELECT CASE max(matched) WHEN 1 THEN 'matched' WHEN 0 THEN 'unmatched' END FROM zotero_item WHERE fil_id = ?1), '');`,
		filID).Scan(&out.Favourite, &cs, &ls, &out.Zotero)
	if err != nil {
		return Organised{}, fmt.Errorf("store: read how %s is filed: %w", filID, err)
	}
	if out.Collections, err = idList(cs); err != nil {
		return Organised{}, err
	}
	if out.Labels, err = idList(ls); err != nil {
		return Organised{}, err
	}
	return out, nil
}

// idList reads a list group_concat wrote.
func idList(s string) ([]int64, error) {
	out := []int64{}
	if s == "" {
		return out, nil
	}
	for _, f := range strings.Split(s, ",") {
		n, err := strconv.ParseInt(f, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("store: read IDs %q: %w", s, err)
		}
		out = append(out, n)
	}
	slices.Sort(out)
	return out, nil
}

// inCollection fills in which collections a query's CollectionID covers: it
// and every one inside it.
func (db *DB) inCollection(ctx context.Context, q ListQuery) (ListQuery, error) {
	if q.CollectionID == 0 {
		return q, nil
	}
	_, cs, err := collectionIn(ctx, db.read, q.CollectionID)
	if errors.Is(err, errs.ErrNotFound) {
		q.within = []int64{q.CollectionID} // finds nothing, as a deleted collection should
		return q, nil
	}
	if err != nil {
		return q, err
	}
	q.within = within(cs, q.CollectionID)
	return q, nil
}
