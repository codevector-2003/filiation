-- Filiation storage schema (SQLite)
--
-- Design notes:
--   * fil_id is the primary key: "F" and eight Crockford base32 characters, created
--     once and never changed (D17). Every table that refers to a work refers to it.
--   * openalex_id is the deduplication key. It kills duplicate preprint/conference/
--     journal versions of the same work, because OpenAlex already merges most of
--     them. It is UNIQUE, so the database refuses a second row for one OpenAlex
--     work, and NULL for a work OpenAlex does not know (a local document).
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

-- Schema history: v1 keyed work on openalex_id (v0.1, v0.2). v2 added fil_id as
-- the key and work_alias, and dropped the unused pdf_sha256 and pdf_license, whose
-- replacement arrives with the papers/ folder (ARCHITECTURE_PHASE3.md §5). The
-- upgrade is in migrate.go.
--
-- A work row exists in one of two states (see ADR-003):
--   stub     hydrated = 0 — the OpenAlex ID is known and nothing else. Created the
--                           moment another paper is seen to reference it, which is
--                           what makes deduplication and relevance scoring free.
--   hydrated hydrated = 1 — metadata fetched.
-- Because of this, title is NULLABLE. Every read path must tolerate a stub.
CREATE TABLE IF NOT EXISTS work (
    -- NOT NULL is not redundant: SQLite lets a non-INTEGER primary key hold NULL.
    fil_id        TEXT PRIMARY KEY NOT NULL,  -- F7K2M9QXA; never changes (D17)
    openalex_id   TEXT UNIQUE,       -- NULL for a work OpenAlex does not know
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

-- IDs a work used to have. When two records fold into one (a merged OpenAlex
-- record, a shared DOI), the survivor keeps its fil ID and the other's fil ID and
-- OpenAlex ID land here, so a folder name, a note or a script that used them
-- still finds the paper. An alias is never reused as a new fil ID.
CREATE TABLE IF NOT EXISTS work_alias (
    alias  TEXT PRIMARY KEY,
    fil_id TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_work_alias_fil ON work_alias(fil_id);

-- ---------------------------------------------------------------- people

CREATE TABLE IF NOT EXISTS author (
    openalex_id TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    orcid       TEXT
);

CREATE TABLE IF NOT EXISTS authorship (
    work_id   TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    author_id TEXT NOT NULL REFERENCES author(openalex_id) ON DELETE CASCADE,
    position  INTEGER,
    PRIMARY KEY (work_id, author_id)
);

-- ---------------------------------------------------------------- the graph

CREATE TABLE IF NOT EXISTS cites (
    from_work  TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    to_work    TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
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
    work_id  TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
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
    work_id    TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    body       TEXT NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS collection (
    id   INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS collection_work (
    collection_id INTEGER NOT NULL REFERENCES collection(id) ON DELETE CASCADE,
    work_id       TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    PRIMARY KEY (collection_id, work_id)
);

-- ---------------------------------------------------------------- bookkeeping

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);

INSERT OR IGNORE INTO meta(key, value) VALUES ('schema_version', '2');
