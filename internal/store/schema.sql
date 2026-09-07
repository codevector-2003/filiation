-- Filiation storage schema (SQLite)
--
-- Design notes:
--   * openalex_id is the canonical key. It kills duplicate preprint/conference/journal
--     versions of the same work, because OpenAlex already merges most of them.
--   * cites.context holds the sentence around the citation marker in the citing paper.
--     Nobody provides this for free. It is what makes retrieval better than plain search.
--   * PDFs live on the filesystem, named by their SHA-256. Only the hash is stored here.

-- NOTE: these two PRAGMAs are a statement of intent, not the mechanism.
-- internal/store strips every PRAGMA from this file before applying it, because
-- journal_mode cannot run inside the transaction that makes migration
-- all-or-nothing. Both are set per connection from the DSN in open.go instead --
-- which is also the only thing that works, since foreign_keys is per connection
-- and never stored in the file (spike 5). Adding a PRAGMA here will NOT run it.
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

-- ---------------------------------------------------------------- works

-- A work row exists in one of two states (see ADR-003):
--   stub     hydrated = 0 — the OpenAlex ID is known and nothing else. Created the
--                           moment another paper is seen to reference it, which is
--                           what makes deduplication and relevance scoring free.
--   hydrated hydrated = 1 — metadata fetched.
-- Because of this, title is NULLABLE. Every read path must tolerate a stub.
CREATE TABLE IF NOT EXISTS work (
    openalex_id   TEXT PRIMARY KEY,
    doi           TEXT UNIQUE,
    arxiv_id      TEXT,
    pmid          TEXT,
    title         TEXT,              -- NULL while the work is still a stub
    abstract      TEXT,
    year          INTEGER,
    venue         TEXT,
    type          TEXT,              -- article | preprint | book-chapter | ...
    cited_by_count INTEGER DEFAULT 0,
    oa_status     TEXT,              -- gold | green | hybrid | bronze | closed
    oa_url        TEXT,              -- legal full-text location, if any
    pdf_sha256    TEXT,              -- NULL until the PDF is fetched
    pdf_license   TEXT,              -- record it: needed to know what may be redistributed
    hydrated      INTEGER DEFAULT 0, -- 0 = stub, 1 = metadata fetched
    fetched_refs  INTEGER DEFAULT 0, -- 0 = its own references not yet recorded
    unresolved    INTEGER DEFAULT 0, -- OpenAlex has no record of it; stop retrying
    depth         INTEGER,           -- hops from the nearest seed
    is_seed       INTEGER DEFAULT 0, -- explicitly added by the user
    source        TEXT,              -- how it entered: seed | expansion | upload | zotero
    added_at      TEXT DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_work_year   ON work(year);
CREATE INDEX IF NOT EXISTS idx_work_doi    ON work(doi);
CREATE INDEX IF NOT EXISTS idx_work_refs   ON work(fetched_refs);

-- Frontier selection runs once per batch during expansion. Partial index keeps it
-- cheap as the graph grows past a few thousand nodes.
CREATE INDEX IF NOT EXISTS idx_work_frontier
    ON work(hydrated, depth) WHERE hydrated = 0;

-- ---------------------------------------------------------------- people

CREATE TABLE IF NOT EXISTS author (
    openalex_id TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    orcid       TEXT
);

CREATE TABLE IF NOT EXISTS authorship (
    work_id   TEXT NOT NULL REFERENCES work(openalex_id) ON DELETE CASCADE,
    author_id TEXT NOT NULL REFERENCES author(openalex_id) ON DELETE CASCADE,
    position  INTEGER,
    PRIMARY KEY (work_id, author_id)
);

-- ---------------------------------------------------------------- the graph

CREATE TABLE IF NOT EXISTS cites (
    from_work  TEXT NOT NULL REFERENCES work(openalex_id) ON DELETE CASCADE,
    to_work    TEXT NOT NULL REFERENCES work(openalex_id) ON DELETE CASCADE,
    intent     TEXT,     -- background | method | comparison | contradiction | uncategorised
    context    TEXT,     -- the sentence around the citation marker
    section    TEXT,     -- intro | methods | results | discussion | related-work
    confidence REAL,     -- confidence in intent + context extraction
    PRIMARY KEY (from_work, to_work)
);

-- Reverse lookups are as common as forward ones: "who cites this?"
CREATE INDEX IF NOT EXISTS idx_cites_to     ON cites(to_work);
CREATE INDEX IF NOT EXISTS idx_cites_intent ON cites(intent);

-- ---------------------------------------------------------------- text for retrieval

CREATE TABLE IF NOT EXISTS chunk (
    id       INTEGER PRIMARY KEY,
    work_id  TEXT NOT NULL REFERENCES work(openalex_id) ON DELETE CASCADE,
    section  TEXT,
    page     INTEGER,
    ordinal  INTEGER,   -- position within the work, for stitching neighbours back together
    text     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_chunk_work ON chunk(work_id);

-- Keyword search. Contentless FTS5 table mirroring chunk.text.
CREATE VIRTUAL TABLE IF NOT EXISTS chunk_fts USING fts5(
    text,
    content = 'chunk',
    content_rowid = 'id',
    tokenize = 'porter unicode61'
);

CREATE TRIGGER IF NOT EXISTS chunk_ai AFTER INSERT ON chunk BEGIN
    INSERT INTO chunk_fts(rowid, text) VALUES (new.id, new.text);
END;
CREATE TRIGGER IF NOT EXISTS chunk_ad AFTER DELETE ON chunk BEGIN
    INSERT INTO chunk_fts(chunk_fts, rowid, text) VALUES ('delete', old.id, old.text);
END;
CREATE TRIGGER IF NOT EXISTS chunk_au AFTER UPDATE ON chunk BEGIN
    INSERT INTO chunk_fts(chunk_fts, rowid, text) VALUES ('delete', old.id, old.text);
    INSERT INTO chunk_fts(rowid, text) VALUES (new.id, new.text);
END;

-- Vector search lives in sqlite-vec. Created at runtime because it needs the loaded
-- extension and the embedding dimension of whichever model is configured:
--
--   CREATE VIRTUAL TABLE chunk_vec USING vec0(
--       chunk_id INTEGER PRIMARY KEY,
--       embedding FLOAT[384]
--   );
--
-- sqlite-vec is pre-1.0. Pin the exact version and keep all calls behind one wrapper module.

-- ---------------------------------------------------------------- the user's own layer

CREATE TABLE IF NOT EXISTS note (
    id         INTEGER PRIMARY KEY,
    work_id    TEXT NOT NULL REFERENCES work(openalex_id) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS collection (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS collection_work (
    collection_id INTEGER NOT NULL REFERENCES collection(id) ON DELETE CASCADE,
    work_id       TEXT NOT NULL REFERENCES work(openalex_id) ON DELETE CASCADE,
    PRIMARY KEY (collection_id, work_id)
);

-- ---------------------------------------------------------------- bookkeeping

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);

INSERT OR IGNORE INTO meta(key, value) VALUES ('schema_version', '1');
