-- 001_init.sql — initial schema for the family tree application.
-- All tables are created here; later migrations are numbered sequentially
-- (002_..., 003_...) and applied in order by internal/db.

CREATE TABLE schema_version (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Users of the site, with one of three roles:
--   admin  — everything (users, settings, import, delete)
--   editor — edit people/families, upload photos
--   viewer — read-only
CREATE TABLE users (
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('admin','editor','viewer')),
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- Sessions: id is the SHA-256 hex of the random cookie token (the raw token
-- is never stored, so the DB cannot mint valid cookies). csrf is the token
-- every authenticated POST must echo back in its form data.
CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  csrf TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_sessions_user ON sessions (user_id);

-- People. gedcom_xref preserves the original @I…@ reference so that
-- re-imports could be matched later. raw_facts (JSON) keeps unknown or
-- proprietary GEDCOM tags (e.g. MyHeritage _HEB) so nothing is lost.
CREATE TABLE people (
  id INTEGER PRIMARY KEY,
  gedcom_xref TEXT UNIQUE,
  given_names TEXT NOT NULL DEFAULT '',
  surname TEXT NOT NULL DEFAULT '',
  married_name TEXT NOT NULL DEFAULT '',
  nickname TEXT NOT NULL DEFAULT '',
  sex TEXT NOT NULL DEFAULT 'U' CHECK (sex IN ('M','F','U','X')),
  birth_date TEXT NOT NULL DEFAULT '',
  birth_year INTEGER,
  birth_place TEXT NOT NULL DEFAULT '',
  death_date TEXT NOT NULL DEFAULT '',
  death_year INTEGER,
  death_place TEXT NOT NULL DEFAULT '',
  living INTEGER NOT NULL DEFAULT 0,
  notes TEXT NOT NULL DEFAULT '',
  raw_facts TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_people_name ON people (surname, given_names);
CREATE INDEX idx_people_birth_year ON people (birth_year);

-- Families: a couple (partners) that may have children via child_links.
CREATE TABLE families (
  id INTEGER PRIMARY KEY,
  gedcom_xref TEXT UNIQUE,
  partner1_id INTEGER REFERENCES people(id) ON DELETE SET NULL,
  partner2_id INTEGER REFERENCES people(id) ON DELETE SET NULL,
  status TEXT NOT NULL DEFAULT 'married'
    CHECK (status IN ('married','unmarried','divorced','separated','other')),
  marriage_date TEXT NOT NULL DEFAULT '',
  marriage_year INTEGER,
  marriage_place TEXT NOT NULL DEFAULT '',
  notes TEXT NOT NULL DEFAULT '',
  raw_facts TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_families_partner1 ON families (partner1_id);
CREATE INDEX idx_families_partner2 ON families (partner2_id);

-- A child's membership in a parent family (a person may have several,
-- e.g. birth family and adoptive family). ord preserves GEDCOM CHIL order.
CREATE TABLE child_links (
  id INTEGER PRIMARY KEY,
  family_id INTEGER NOT NULL REFERENCES families(id) ON DELETE CASCADE,
  person_id INTEGER NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  ord INTEGER NOT NULL DEFAULT 0,
  relationship TEXT NOT NULL DEFAULT 'birth'
    CHECK (relationship IN ('birth','adopted','foster'))
);
CREATE INDEX idx_child_links_family ON child_links (family_id);
CREATE INDEX idx_child_links_person ON child_links (person_id);

-- Generic events/facts. BIRT/DEAT are denormalized onto people, MARR onto
-- families; everything else (OCCU, RESI, EDU,…) lives here.
CREATE TABLE events (
  id INTEGER PRIMARY KEY,
  person_id INTEGER REFERENCES people(id) ON DELETE CASCADE,
  family_id INTEGER REFERENCES families(id) ON DELETE CASCADE,
  tag TEXT NOT NULL,
  date TEXT NOT NULL DEFAULT '',
  year INTEGER,
  place TEXT NOT NULL DEFAULT '',
  detail TEXT NOT NULL DEFAULT '',
  sort INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX idx_events_person ON events (person_id, sort);
CREATE INDEX idx_events_family ON events (family_id, sort);

-- Photos and other media. Blobs live in SQLite so the whole app is one
-- backup-able file. data = original upload, thumb = small generated JPEG.
-- import_note rows come from GEDCOM OBJE links (no blob yet): MyHeritage
-- ships media as a separate zip, so from-import rows keep the filename and
-- can be replaced by a real upload later.
CREATE TABLE media (
  id INTEGER PRIMARY KEY,
  person_id INTEGER REFERENCES people(id) ON DELETE CASCADE,
  family_id INTEGER REFERENCES families(id) ON DELETE CASCADE,
  filename TEXT NOT NULL DEFAULT '',
  mime TEXT NOT NULL DEFAULT '',
  size INTEGER NOT NULL DEFAULT 0,
  caption TEXT NOT NULL DEFAULT '',
  is_profile INTEGER NOT NULL DEFAULT 0,
  import_note INTEGER NOT NULL DEFAULT 0,
  data BLOB,
  thumb BLOB,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_media_person ON media (person_id);
CREATE INDEX idx_media_family ON media (family_id);

-- Sources and citations are import-only (display), no editing UI in v1.
CREATE TABLE sources (
  id INTEGER PRIMARY KEY,
  gedcom_xref TEXT UNIQUE,
  title TEXT NOT NULL DEFAULT '',
  author TEXT NOT NULL DEFAULT '',
  publication TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL DEFAULT ''
);

CREATE TABLE citations (
  id INTEGER PRIMARY KEY,
  source_id INTEGER NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
  person_id INTEGER REFERENCES people(id) ON DELETE CASCADE,
  family_id INTEGER REFERENCES families(id) ON DELETE CASCADE,
  detail TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_citations_person ON citations (person_id);
CREATE INDEX idx_citations_family ON citations (family_id);

-- Record of each GEDCOM import run (for the admin report page).
CREATE TABLE imports (
  id INTEGER PRIMARY KEY,
  filename TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL CHECK (status IN ('running','done','failed','rolled_back')),
  people_count INTEGER NOT NULL DEFAULT 0,
  families_count INTEGER NOT NULL DEFAULT 0,
  sources_count INTEGER NOT NULL DEFAULT 0,
  events_count INTEGER NOT NULL DEFAULT 0,
  media_count INTEGER NOT NULL DEFAULT 0,
  warnings TEXT NOT NULL DEFAULT '[]',
  started_at TEXT NOT NULL DEFAULT (datetime('now')),
  finished_at TEXT
);

-- Simple key/value settings (site name, chart defaults…).
CREATE TABLE settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

-- Who did what (visible to admins on the audit page).
CREATE TABLE audit_log (
  id INTEGER PRIMARY KEY,
  user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
  action TEXT NOT NULL,
  entity TEXT NOT NULL,
  entity_id INTEGER,
  detail TEXT NOT NULL DEFAULT '',
  at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_audit_at ON audit_log (at);
