# S30 website

This repository contains the Shandalar 30th Anniversary Edition landing page and its Go bug-report service.

## Local development

Start the Go server:

```sh
go run .
```

The site runs at `http://localhost:8080` by default. Set `PORT` or `STATIC_DIR` to change the listener or frontend directory.

## Bug reports

The game sends reports to `POST /api/bug-reports`. The server creates an issue in the configured GitHub repository and returns the issue URL and number in the format that the game expects.

Copy `.env.example` to `.env`. Set `GITHUB_TOKEN` to a fine-grained GitHub token that has Issues read and write access for `benprew/s30`.

## Production deployment (Systemd + Caddy)

Deploy the binary and static frontend assets to the server:

```sh
bin/deploy.sh
```

This builds a standalone static Linux binary, uploads it to `/var/www/s30-web/`, syncs `public/` assets, and restarts the `s30-web` systemd service.

Make sure `/var/www/s30-web/.env` is configured with `GITHUB_TOKEN`.

### Docker deployment (Alternative)

Alternatively, run as a container:

```sh
cp .env.example .env
docker compose up -d --build
```

Set the game endpoint to:

```sh
S30_BUG_WORKER_URL=https://s30.throwingbones.com/api/bug-reports
```
