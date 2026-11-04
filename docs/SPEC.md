# Architecture & design decisions (SPEC)

## Shape

One Go binary (`cmd/familytree`) embedding everything. No CGO (pure-Go
SQLite driver), no Node build. Dependencies (pinned in go.mod, deliberately
few):

| dep | role |
|---|---|
| `modernc.org/sqlite` | SQLite driver (pure Go) |
| `golang.org/x/crypto` | bcrypt |
| `elliotchance/gedcom` | GEDCOM parser |
| `disintegration/imaging` | thumbnailing |
| `golang.org/x/text` | charset decoding (ANSEL helper tables hand-written) |

Vendored front-end (in `internal/web/static/vendor`, served from embed.FS):
`d3@7.9.0` UMD, `family-chart@0.9.0` dist + stylesheet.

## Packages

```
cmd/familytree        subcommands: serve (default), admin-create, import,
                      backup, healthcheck, version
internal/config       env-only config (DATA_DIR/PORT/APP_COOKIE_SECURE)
internal/db           sqlite open (WAL+busy_timeout+FK pragmas), migrations,
                      online backup (VACUUM INTO)
internal/models       structs + role weights + living heuristic
internal/store        all SQL; "Queries over DBTX" so importers can run
                      whole-tile transactions with the same functions
internal/auth         bcrypt, hashed-token sessions, Wrap(minRole) with CSRF
internal/gedcom       charset normalize (ANSEL tables), import engine
internal/web          handlers, embedded templates/static, chart data JSON
migrations (internal/db/migrations/*.sql) — sequential, embedded
```

## Schema highlights

- people/families carry denormalized birth/death/marriage dates+years;
  `gedcom_xref` keeps original `@I…@` references (normalized with @).
- `events` is a generic table for OCCU/RESI/EDUC/…; the importer also
  recognizes unknown tags WITH a DATE/PLAC child as events → nothing lost.
- `people.raw_facts` / `families.raw_facts` JSON keep underscore vendor tags
  (`_HEB`, `_DNA`, …).
- `media` keeps original + 256px JPEG thumb BLOBs in SQLite so the entire
  app state is a single file. GEDCOM OBJE imports create `import_note`
  placeholder rows (MyHeritage ships the real photos in a separate zip);
  uploading "attaches" (upgrades) them in place.
- `imports` records every import run (counts + warnings) for the admin UI.
- `audit_log` records every mutation with the acting user.

## Auth model

- bcrypt passwords; sessions store SHA-256 hashes of random cookie tokens
  (expiry filtered in SQL; sliding 30-day TTL refreshed at half-life).
- `auth.Wrap(minRole, handler)` guards every route: anonymous → 302
  /login?next=…; insufficient role → 403 page; POST without matching
  per-session CSRF token → 403. /login POST is the sole anonymous form.
- Roles: admin > editor > viewer; anonymous sees nothing.

## Import engine

`internal/gedcom.Import`: bytes → charset normalize (HEAD.CHAR; ANSEL table
with GEDCOM extensions) → elliotchance/gedcom decode → map (sources,
people+events+media+citations, families+child links, family statuses) →
single transaction `DELETE` of tree tables + inserts → commit; failures roll
back AND record a `failed` row in `imports`. Warnings (unknown source refs,
extra names, unmapped ANSEL bytes) go to the report, never break the import.
Years are best-effort parsed from the GEDCOM date string and stored in
separate `*_year` columns (raw string is authoritative).

## Tree data (family-chart)

`/tree.json?person=&gens=` builds nodes in the documented family-chart
format: gender mapped to M/F (unknown → M), avatar → /media/{id}/thumb,
`first name`/`last name`/`years` fields, and rels that are always
bidirectional (checked by tests): parents ↔ children, spouses both ways.
Included set: focus + `gens` ancestor levels + `gens` descendant levels +
all spouses of anyone included. The interactive view (default) uses the
vendored library; `?view=list` renders server-side indent lists.

## Conventions (handoff rules)

- stdlib `net/http` routing with method+path patterns; html/template with
  shared partials (`header`, `footer`, `personlink…`).
- Free-text dates everywhere; `gedcom.ParseYear` extracts the year.
- All writes audited; deletes are admin-only and confirm-checkbox gated.
- Tests live next to code; web tests exercise real flows over httptest
  (login → CSRF → form POST). `mise check` = gofmt + vet + tests must pass.
- Keep functions small (≤ ~80 lines), one concern per file, no new runtime
  dependencies without strong justification.
