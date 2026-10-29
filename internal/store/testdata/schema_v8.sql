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
--   * PDFs live on the filesystem, in the library folder's papers/, one folder per work
--     with a readable name (D18). Only paths and hashes are stored here.

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
    id         INTEGER PRIMARY KEY,
    work_id    TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    section    TEXT,      -- 'references' after the paper's references heading, else NULL
    page       INTEGER,   -- from 1
    ordinal    INTEGER,   -- position within the work, for stitching neighbours back together
    text       TEXT NOT NULL,
    char_start INTEGER,   -- schema v6: byte offsets of text in its page's cleaned text
    char_end   INTEGER
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

-- A collection that mirrors a folder in papers/ (ADR-012): papers/Chapter 2/Key
-- studies appears as the collection "Chapter 2/Key studies". Rescans keep its
-- members in step with the work folders directly inside it. A collection with no
-- row here is the user's own and no scan touches it.
CREATE TABLE IF NOT EXISTS folder_collection (
    collection_id INTEGER PRIMARY KEY REFERENCES collection(id) ON DELETE CASCADE,
    folder        TEXT NOT NULL UNIQUE  -- relative to the library folder
);

-- ---------------------------------------------------------------- papers on disk

-- Schema v3 (ARCHITECTURE_PHASE3.md §5). Paths are relative to the library
-- folder, with '/', in Unicode NFC, so one library reads the same on every OS.
-- The researcher owns the folder tree: these tables record where things are, and
-- a scan updates them when a person moves something (ADR-012).

-- A folder named for one work. Usually one per work; a person may make more.
CREATE TABLE IF NOT EXISTS work_folder (
    path       TEXT PRIMARY KEY NOT NULL,
    fil_id     TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    given_name TEXT,     -- the name fil gave it. fil renames the folder only while
                         -- it still has this name; NULL means a person named it
    missing    INTEGER NOT NULL DEFAULT 0,  -- gone from disk at the last scan
    added_at   TEXT DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_work_folder_work ON work_folder(fil_id);

-- A file that belongs to a work. The hash both deduplicates (look it up before
-- saving) and verifies (re-hash and compare); it replaced the hash-named files of
-- D4. A missing file keeps its row, so fil knows not to download it again unasked.
CREATE TABLE IF NOT EXISTS work_file (
    path     TEXT PRIMARY KEY NOT NULL,
    fil_id   TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    sha256   TEXT NOT NULL,
    size     INTEGER NOT NULL,
    mtime    INTEGER,           -- Unix seconds when hashed; a change means re-hash
    version  TEXT,              -- published | accepted | preprint; NULL if not known
    origin   TEXT NOT NULL,     -- download | import | found
    missing  INTEGER NOT NULL DEFAULT 0,
    added_at TEXT DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_work_file_work   ON work_file(fil_id);
CREATE INDEX IF NOT EXISTS idx_work_file_sha256 ON work_file(sha256);

-- ---------------------------------------------------------------- open-access links

-- Schema v4 (ADR-013). What fil knows about each work's legal PDF. A work with
-- no row has not been looked up; every other work has an answer, so a paper
-- without a PDF always says why — as reference coverage does (D12).
CREATE TABLE IF NOT EXISTS work_pdf (
    fil_id       TEXT PRIMARY KEY NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    availability TEXT NOT NULL,  -- pdf-link | open-no-pdf | no-open-copy | not-in-openalex
    checked_at   TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_work_pdf_availability ON work_pdf(availability);

-- A work's PDF links, in the order to try them: published, then accepted, then
-- preprint. Only open-access locations with a direct PDF link; a landing page
-- is never read for links (ADR-013).
CREATE TABLE IF NOT EXISTS work_location (
    fil_id      TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    rank        INTEGER NOT NULL,  -- 0 is tried first
    pdf_url     TEXT NOT NULL,
    landing_url TEXT,
    version     TEXT,              -- published | accepted | preprint; NULL if not stated
    license     TEXT,              -- NULL when none is stated, as for bronze
    host        TEXT NOT NULL,
    source      TEXT NOT NULL,     -- openalex; unpaywall if spike 9 justifies it
    PRIMARY KEY (fil_id, rank)
);

-- Schema v5. Every download attempt, so a paper without a PDF says exactly why
-- — "the publisher refused, the repository sent a web page" — and a link that
-- failed is not tried again for a month (ADR-013).
CREATE TABLE IF NOT EXISTS download_attempt (
    id         INTEGER PRIMARY KEY,
    fil_id     TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    host       TEXT NOT NULL,
    outcome    TEXT NOT NULL,  -- ok | refused | not-found | http-error | not-a-pdf | too-large
                               -- | timeout | network | redirect-loop | duplicate
    status     INTEGER,        -- the HTTP status, when there was one
    bytes      INTEGER,
    millis     INTEGER,
    at         TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_download_attempt_work ON download_attempt(fil_id);
CREATE INDEX IF NOT EXISTS idx_download_attempt_url  ON download_attempt(url);

-- Schema v6. What reading each paper's PDF gave: searchable text, or why there
-- is none — a scan, a text layer that is gibberish, a damaged file — so a paper
-- without text says why, as one without a PDF does (ADR-009). The chunks are the
-- text; this row is where they came from. A PDF whose hash or extractor changes
-- is read again.
CREATE TABLE IF NOT EXISTS work_text (
    fil_id     TEXT PRIMARY KEY NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    path       TEXT NOT NULL,   -- the PDF read, relative to the library folder
    sha256     TEXT NOT NULL,   -- its hash when read
    status     TEXT NOT NULL,   -- ok | no-text | gibberish | unreadable | timed-out
    reason     TEXT,            -- why, in words, when not ok
    pages      INTEGER,
    chars      INTEGER,
    extractor  TEXT NOT NULL,   -- e.g. pdfium/1; an older one means read again
    millis     INTEGER,
    read_at    TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Schema v7 (ADR-014). What importing each PDF the user brought gave, by its
-- hash: filed under a work, or why not — no identifier, one that did not match
-- the text, not in OpenAlex. A file that could not be identified is not tried
-- again, nor its title searched again (10 credits), until it changes or the
-- user names it.
CREATE TABLE IF NOT EXISTS import_attempt (
    sha256     TEXT PRIMARY KEY NOT NULL,
    path       TEXT NOT NULL,   -- where it was, relative to the library folder or absolute
    outcome    TEXT NOT NULL,   -- imported | duplicate | unconfirmed | candidates | unidentified
                                -- | not-in-openalex | unreadable
    reason     TEXT,
    fil_id     TEXT REFERENCES work(fil_id) ON DELETE SET NULL,
    searched   INTEGER NOT NULL DEFAULT 0, -- a title search was spent on it
    at         TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Schema v8 (ADR-015). Every place a paper cites another, found in its PDF: the
-- sentence, and where it is. Only for an edge OpenAlex already gave: nothing read
-- from a PDF creates, removes or redirects one. cites.context keeps the first
-- mention, for display; M6 needs them all.
CREATE TABLE IF NOT EXISTS mention (
    id         INTEGER PRIMARY KEY,
    from_work  TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    to_work    TEXT NOT NULL REFERENCES work(fil_id) ON DELETE CASCADE,
    page       INTEGER NOT NULL,   -- from 1
    char_start INTEGER NOT NULL,   -- byte offsets of the sentence in its page's cleaned text
    char_end   INTEGER NOT NULL,
    sentence   TEXT NOT NULL,
    section    TEXT,               -- introduction | methods | results | discussion | …, when a heading says
    marker     TEXT NOT NULL,      -- as printed: "[12]", "(Piwowar et al., 2018)"
    method     TEXT NOT NULL,      -- numeric | author-year
    confidence REAL NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mention_from ON mention(from_work);
CREATE INDEX IF NOT EXISTS idx_mention_to   ON mention(to_work);

-- ---------------------------------------------------------------- bookkeeping

CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT
);

INSERT OR IGNORE INTO meta(key, value) VALUES ('schema_version', '8');
