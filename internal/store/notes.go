package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/codevector-2003/filiation/internal/errs"
)

// Note is what a reader marked on a paper: a highlight, a comment, or both.
// The PDF is never changed; a note lives here and is drawn over the page.
type Note struct {
	ID      int64
	FilID   string
	Page    int    // from 1; 0 for a note on the paper as a whole
	Colour  string // yellow | green | blue | pink, "" for a comment alone
	Quote   string // the text marked
	Rects   string // where on the page, as JSON fractions of its size
	File    string // the PDF marked, in the library folder: another edition has other pages
	Comment string
	Created string // as the database wrote it, UTC
}

// AddNote stores a note and returns its ID.
func (tx *Tx) AddNote(ctx context.Context, n Note) (int64, error) {
	res, err := tx.tx.ExecContext(ctx, `
INSERT INTO note (work_id, body, page, colour, quote, rects, file) VALUES (?, ?, ?, ?, ?, ?, ?);`,
		n.FilID, n.Comment, n.Page, n.Colour, n.Quote, n.Rects, n.File)
	if err != nil {
		return 0, fmt.Errorf("add note: %w", err)
	}
	return res.LastInsertId()
}

// SetNoteComment replaces a note's comment.
func (tx *Tx) SetNoteComment(ctx context.Context, id int64, comment string) error {
	res, err := tx.tx.ExecContext(ctx, `UPDATE note SET body = ? WHERE id = ?;`, comment, id)
	if err != nil {
		return fmt.Errorf("change note: %w", err)
	}
	return oneNote(res)
}

// DeleteNote removes a note.
func (tx *Tx) DeleteNote(ctx context.Context, id int64) error {
	res, err := tx.tx.ExecContext(ctx, `DELETE FROM note WHERE id = ?;`, id)
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	return oneNote(res)
}

func oneNote(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("note: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("no such note: %w", errs.ErrNotFound)
	}
	return nil
}

// Notes lists a work's notes in reading order.
func (db *DB) Notes(ctx context.Context, filID string) ([]Note, error) {
	rows, err := db.read.QueryContext(ctx, `
SELECT id, work_id, ifnull(page, 0), ifnull(colour, ''), ifnull(quote, ''), ifnull(rects, ''), ifnull(file, ''), body, ifnull(created_at, '')
FROM note WHERE work_id = ? ORDER BY ifnull(page, 0), id;`, filID)
	if err != nil {
		return nil, fmt.Errorf("read notes: %w", err)
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		var n Note
		if err := rows.Scan(&n.ID, &n.FilID, &n.Page, &n.Colour, &n.Quote, &n.Rects, &n.File, &n.Comment, &n.Created); err != nil {
			return nil, fmt.Errorf("read notes: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("read notes: %w", err)
	}
	return out, nil
}
