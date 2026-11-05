# genealogy-tree

A self-hosted family tree web application: import a MyHeritage GEDCOM, browse
and edit your family, upload photos and share an interactive tree with
relatives behind simple login accounts.

**Single statically-linked Go binary** (pure-Go SQLite, everything embedded),
deployed with one Docker image. No external services.

## Requirements

- [mise](https://mise.jdx.dev) manages the toolchain: `mise install && mise trust`
- Go 1.27 (pinned in `mise.toml` — no CGO, no Node, no build steps)

## Development

```sh
mise install        # toolchain
mise dev            # run the server on :8080 (data dir .data via mise env)
mise build          # static binary -> bin/familytree
mise check          # gofmt + vet + all tests — must pass before every commit
mise docker         # build the deployment image
```

## Command overview

```text
familytree serve                          # web server (default)
familytree admin-create -u admin -p 'pw'  # optional CLI bootstrap (there is also a /setup page on first run)
familytree import family_tree.ged         # replace tree with GEDCOM (CLI)
familytree backup /path/backup.db         # online consistent SQLite copy
familytree healthcheck -port 8080         # used by the Docker HEALTHCHECK
familytree version
```

### First run, no users

The app ships with **no users**. On a fresh database every page redirects to
the one-time `/setup` screen to create the first admin (username + password).
Once one account exists, `/setup` closes permanently.

## Features

- **GEDCOM import** (MyHeritage-flavored 5.5.1): charsets UTF-8/UTF-16/
  Windows-1252/ISO-8859-1/ANSEL, `_MARNM`, `_HEB` and other `_` vendor tags
  preserved as raw facts, OBJE media as placeholders, atomic replace-tree
  transaction. Available via CLI and the admin UI (with a full report).
- **Roles**: admin (users/settings/import/deletes) > editor (edit people/
  families, photos) > viewer (read-only). Anonymous users get redirected to
  the login page.
- **Editing**: person & family forms with free-text GEDCOM-style dates
  ("ABT 1847", "BET 1900 AND 1905"), guided add-parents/add-spouse/add-child
  flows, everything audited in an admin-visible log.
- **Photos**: uploads become embedded 256px JPEG thumbnails (stored as
  SQLite blobs — the whole app is one backupable file); GEDCOM photo
  placeholders can be upgraded in place.
- **Tree**: interactive `family-chart` (click re-centers, double-click opens
  the profile, generations selector) plus a plain list view fallback; served
  fully offline from vendored dist files.

## Configuration (env vars)

| Variable | Default | Meaning |
|---|---|---|
| `DATA_DIR` | `./data` | Where `familytree.db` lives (mount a volume in production) |
| `PORT` | `8080` | HTTP listen port |
| `APP_COOKIE_SECURE` | `false` | Set `true` behind an HTTPS reverse proxy |

## Deployment

See **docs/DEPLOY.md** for the full VPS runbook (docker compose, TLS via
Caddy/nginx, backups, restore, healthcheck).

## Documentation

- `docs/SPEC.md` — architecture, schema, and design decisions
- `docs/DEPLOY.md` — production runbook
- `docs/HANDOFF.md` — conventions for continuing development (incl. with a
  less capable AI model)

## License note

Vendored runtime libraries (licenses in their upstream repos): d3.js (ISC),
family-chart (MIT/ISC). All application code lives in this repository.
