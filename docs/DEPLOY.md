# Deployment runbook (VPS, docker)

Target: any Linux VPS with Docker + docker compose plugin. The app is one
statically-linked binary inside a distroless container; state lives in a
single SQLite file under `/data`.

## 1. First installation

```sh
# on your machine
git clone <repo> genealogy-tree && cd genealogy-tree
mise docker                        # builds image "genealogy-tree" (or: docker compose build)

# on the VPS
docker compose up -d
```

Then open the site in a browser — with **no users yet, every page funnels to
the one-time `/setup` screen** where you create the first admin (username +
password of your choosing). After that the setup page closes automatically.

Alternative (CLI / headless):

```sh
docker compose exec familytree /familytree admin-create -u admin -p 'LongPasswordHere'
```

Or with the CLI import path:

```sh
docker compose cp family_tree.ged familytree:/tmp/tree.ged
docker compose exec familytree /familytree import /tmp/tree.ged
docker compose exec familytree rm /tmp/tree.ged
```

## 2. HTTPS

The app binds to `127.0.0.1:8080` on the VPS so Nginx can proxy to it without
exposing the app port publicly. For `family.timoteoponce.com`, start with this
HTTP server block; Certbot can then add the TLS certificate and HTTPS redirect:

```nginx
server {
    listen 80;
    listen [::]:80;
    server_name family.timoteoponce.com;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

`APP_COOKIE_SECURE` is enabled in Compose for HTTPS cookies. Complete Certbot's
TLS setup before using the site to sign in, since browsers only send those
cookies over HTTPS.

## 3. Backups

Everything (database, photos as blobs) is one SQLite file:

```sh
# ad-hoc
docker compose exec familytree /familytree backup /data/backup-$(date +%F).db

# nightly cron on the host (04:00) — the CLI works on the mounted volume:
echo '0 4 * * * docker compose -f /opt/genealogy-tree/docker-compose.yml exec -T familytree /familytree backup /data/backups/backup-$(date +\%F).db' \
  | crontab -
```

Keep the SQLite file container-internal flag `VOLUME /data` in mind: either
use the named volume (compose default) or bind-mount `./data:/data` if you
prefer file-level access for rsync/restic.

Optional advanced continuous replication: [litestream](https://litestream.io).

## 4. Restore

```sh
docker compose stop familytree
# replace the data volume's familytree.db with the backup copy
docker compose start familytree
```

## 5. Upgrades

```sh
git pull
mise docker
docker compose up -d
```

Migrations run automatically on boot (schema_version tracking).

### Automatic deploys from GitHub Actions

The `master` branch runs CI on pushes and pull requests. A push to `master`
that passes formatting, `go vet`, `go test`, and the Docker build deploys the
image and current Compose file to the VPS over SSH. The workflow transfers the
image archive directly; it does not require a container registry. It runs
`docker compose up` from the existing deployment directory, so keep that
directory and its Compose data volume in place between deploys.

Add these repository Actions secrets in GitHub:

| Secret | Value |
|---|---|
| `SSH_PRIVATE_KEY` | Private key for an account that can SSH to the VPS and run Docker / Docker Compose. |
| `SSH_HOST` | VPS hostname or IP address. |
| `SSH_USER` | SSH account name. |
| `DEPLOY_PATH` | Absolute path on the VPS containing this repository's `docker-compose.yml`, such as `/opt/genealogy-tree`. |

On the VPS, clone the repository at the deployment path and complete the first
`docker compose up -d` manually. This creates the persistent Compose volume and
starts the app. Future successful pushes to `master` replace the image and
recreate the container while retaining that volume. The SSH account must be
able to run Docker and Docker Compose without an interactive password prompt.

## 6. Health

The image defines `HEALTHCHECK` = `familytree healthcheck` which hits
`/healthz` on localhost. `docker ps` shows `(healthy)`. Reverse proxies can
also use `GET /healthz` ("ok").

## 7. Common operations

* create a user: `docker compose exec familytree /familytree admin-create -u name -p pw -role editor|viewer|admin`
* reset a password: admin UI → Users
* re-import a fresh GEDCOM: admin UI → Import (REPLACES the tree; people you
  edited in the web UI are lost — export/backup first if mixed workflows)
* logs: `docker compose logs -f familytree`
