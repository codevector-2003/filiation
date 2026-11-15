package store

import (
	"slices"
	"testing"
)

func TestSearchLeavesOutFillerWords(t *testing.T) {
	t.Parallel()
	db := textLibrary(t)
	ctx := t.Context()
	putText(t, db, Text{FilID: "W1", Path: "papers/a--F00000001/published.pdf", SHA256: "a1", Status: "ok", Extractor: "e"},
		Chunk{Page: 1, Ordinal: 0, Text: "The citation advantage of open access holds in physics."},
		Chunk{Page: 2, Ordinal: 1, Text: "Nothing to see on this page."},
	)
	// Asked as a question, the words "how", "is", "the" and "then" would
	// find no passage with every word; left out, the question finds the
	// passage that answers it, as having every word that matters.
	hits, err := db.Search(ctx, SearchQuery{Words: "How is the citation advantage in physics, then?", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Page != 1 || !hits[0].Every {
		t.Errorf("question = %+v; want page 1, with every word", hits)
	}
	// Only filler words: searched for as typed, rather than refused.
	if hits, err := db.Search(ctx, SearchQuery{Words: "on this", Limit: 10}); err != nil || len(hits) != 1 || hits[0].Page != 2 {
		t.Errorf("filler only = %+v, %v", hits, err)
	}
	// A quoted phrase keeps its filler words.
	if hits, _ := db.Search(ctx, SearchQuery{Words: `"of open access"`, Limit: 10}); len(hits) != 1 {
		t.Errorf("phrase = %+v", hits)
	}
}

func TestSearchRanksPapersByTheirPassages(t *testing.T) {
	t.Parallel()
	db := textLibrary(t)
	ctx := t.Context()
	// W1 has one passage dense with the words; W2 has three that each use
	// them, so it is about the subject. W2 comes first, and each paper's
	// passages stay together.
	putText(t, db, Text{FilID: "W1", Path: "papers/a--F00000001/published.pdf", SHA256: "a1", Status: "ok", Extractor: "e"},
		Chunk{Page: 1, Ordinal: 0, Text: "Preprint servers and preprint citation counts, preprint by preprint."},
		Chunk{Page: 2, Ordinal: 1, Text: "Something else entirely about libraries and budgets."},
	)
	putText(t, db, Text{FilID: "W2", Path: "papers/b--F00000002/Paper.PDF", SHA256: "b2", Status: "ok", Extractor: "e"},
		Chunk{Page: 1, Ordinal: 0, Text: "Preprint posting changes how physics papers are cited later on."},
		Chunk{Page: 2, Ordinal: 1, Text: "Each preprint on the server was matched to its journal version."},
		Chunk{Page: 3, Ordinal: 2, Text: "A preprint posted early is cited sooner and more."},
	)
	hits, err := db.Search(ctx, SearchQuery{Words: "preprint cited", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, h := range hits {
		if len(order) == 0 || order[len(order)-1] != h.FilID {
			order = append(order, h.FilID)
		}
	}
	if !slices.Equal(order, []string{"F00000002"}) {
		t.Errorf("papers in order %v; want only W2, the one whose passages have both words", order)
	}
	hits, _ = db.Search(ctx, SearchQuery{Words: "preprint", Limit: 10})
	order = nil
	for _, h := range hits {
		if len(order) == 0 || order[len(order)-1] != h.FilID {
			order = append(order, h.FilID)
		}
	}
	if !slices.Equal(order, []string{"F00000002", "F00000001"}) {
		t.Errorf("papers in order %v; want W2, with three passages on it, then W1, each together", order)
	}
}

func TestSearchTitleBreaksTies(t *testing.T) {
	t.Parallel()
	db := textLibrary(t)
	ctx := t.Context()
	// The same passage in both papers: the paper whose title is about the
	// words comes first, though its fil ID sorts second.
	mustExec(t, db.write, `UPDATE work SET title = 'Budgets of research libraries', abstract = NULL WHERE fil_id = 'F00000001';`)
	mustExec(t, db.write, `UPDATE work SET title = 'The reproducibility of psychology', abstract = 'Experiments run again.' WHERE fil_id = 'F00000002';`)
	same := "Only some experiments gave the same result when run again by others."
	putText(t, db, Text{FilID: "W1", Path: "papers/a--F00000001/published.pdf", SHA256: "a1", Status: "ok", Extractor: "e"},
		Chunk{Page: 1, Ordinal: 0, Text: same})
	putText(t, db, Text{FilID: "W2", Path: "papers/b--F00000002/Paper.PDF", SHA256: "b2", Status: "ok", Extractor: "e"},
		Chunk{Page: 1, Ordinal: 0, Text: same})
	hits, err := db.Search(ctx, SearchQuery{Words: "experiments run again", Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].FilID != "F00000002" {
		t.Errorf("hits = %+v; want W2, whose title and abstract match, first", hits)
	}
}

func TestWorkSearchFollowsWork(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	found := func(words string) []string {
		t.Helper()
		rows, err := db.read.QueryContext(ctx, `SELECT s.fil_id FROM work_fts JOIN work_search s ON s.id = work_fts.rowid
			WHERE work_fts MATCH ? ORDER BY s.fil_id;`, words)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id string
			rows.Scan(&id)
			out = append(out, id)
		}
		return out
	}
	mustExec(t, db.write, `INSERT INTO work (fil_id, openalex_id) VALUES ('F00000001', 'W1');`) // a stub
	if got := found("hedgerow"); len(got) != 0 {
		t.Errorf("a stub with no title is found: %v", got)
	}
	mustExec(t, db.write, `UPDATE work SET title = 'Hedgerow birds', abstract = 'Counted in winter.' WHERE fil_id = 'F00000001';`)
	if got := found("hedgerow"); !slices.Equal(got, []string{"F00000001"}) {
		t.Errorf("after the title arrived = %v", got)
	}
	if got := found("winter"); !slices.Equal(got, []string{"F00000001"}) {
		t.Errorf("abstract = %v", got)
	}
	mustExec(t, db.write, `UPDATE work SET title = 'Moorland birds' WHERE fil_id = 'F00000001';`)
	if got := found("hedgerow"); len(got) != 0 {
		t.Errorf("an old title is still found: %v", got)
	}
	mustExec(t, db.write, `UPDATE work SET year = 2001 WHERE fil_id = 'F00000001';`) // not a title: no change
	if got := found("moorland"); !slices.Equal(got, []string{"F00000001"}) {
		t.Errorf("after an unrelated change = %v", got)
	}
	mustExec(t, db.write, `DELETE FROM work WHERE fil_id = 'F00000001';`)
	if got := found("moorland"); len(got) != 0 {
		t.Errorf("a deleted work is still found: %v", got)
	}
	var n int
	db.read.QueryRow(`SELECT count(*) FROM work_search;`).Scan(&n)
	if n != 0 {
		t.Errorf("work_search keeps %d rows of deleted works", n)
	}
}

func TestUpgradeIndexesTitles(t *testing.T) {
	t.Parallel()
	db := migrated(t)
	ctx := t.Context()
	// A library at schema 14: no index, and works already there.
	for _, q := range []string{
		`DROP TRIGGER work_search_ai;`, `DROP TRIGGER work_search_au;`, `DROP TRIGGER work_search_ad;`,
		`DROP TABLE work_fts;`, `DROP TABLE work_search;`,
		`INSERT INTO work (fil_id, openalex_id, title, abstract) VALUES
			('F00000001', 'W1', 'Hedgerow birds', NULL), ('F00000002', 'W2', NULL, 'Counted in winter.'), ('F00000003', 'W3', NULL, NULL);`,
		`UPDATE meta SET value = '14' WHERE key = 'schema_version';`,
	} {
		mustExec(t, db.write, q)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	db.read.QueryRow(`SELECT count(*) FROM work_search;`).Scan(&n)
	if n != 3 {
		t.Errorf("work_search has %d rows, want 3", n)
	}
	for words, want := range map[string]string{"hedgerow": "F00000001", "winter": "F00000002"} {
		var got string
		if err := db.read.QueryRow(`SELECT s.fil_id FROM work_fts JOIN work_search s ON s.id = work_fts.rowid WHERE work_fts MATCH ?;`,
			words).Scan(&got); err != nil || got != want {
			t.Errorf("%s = %q, %v; want %s", words, got, err, want)
		}
	}
	// A work added since is indexed by the triggers the upgrade made.
	mustExec(t, db.write, `INSERT INTO work (fil_id, openalex_id, title) VALUES ('F00000004', 'W4', 'Moorland birds');`)
	var got string
	if err := db.read.QueryRow(`SELECT s.fil_id FROM work_fts JOIN work_search s ON s.id = work_fts.rowid WHERE work_fts MATCH 'moorland';`).
		Scan(&got); err != nil || got != "F00000004" {
		t.Errorf("new work = %q, %v", got, err)
	}
}
