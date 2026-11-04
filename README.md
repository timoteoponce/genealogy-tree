# genealogy-tree

A self-hosted family tree web application: import a MyHeritage GEDCOM, browse and
edit your family, and share it with relatives behind simple login accounts.

Single statically-linked Go binary (SQLite embedded, pure Go — no CGO), deployed
with one Docker image.

## Requirements

- [mise](https://mise.jdx.dev) manages the Go toolchain: `mise install && mise trust`
- Go 1.27 (pinned in `mise.toml`)

## Development

```sh
mise install        # install the pinned toolchain
mise dev            # run the server on :8080 (data dir /home/.data from mise.toml env)
mise build          # build the static binary -> bin/familytree
mise check          # gofmt + vet + tests (must pass before every commit)
```

## Deployment (VPS)

```sh
mise docker         # build image (or: docker compose build)
docker compose up -d
```

Reviews of configuration env vars (DATA_DIR, PORT, APP_COOKIE_SECURE) are in
`internal/config/config.go`; the full production runbook will land in
`docs/DEPLOY.md`.

## Command overview

```text
familytree serve       # web server (default)
familytree version     # build info
```

Status / roadmap is tracked per milestone in commit history; see `README.md`
updates as features land.
