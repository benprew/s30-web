# S30 website

This repository contains the Shandalar 30th Anniversary Edition landing page and its Go bug-report service.

## Local development

Start the Go server:

```sh
go run .
```

The site runs at `http://localhost:8080` by default. Set `PORT` or `STATIC_DIR` to change the listener or frontend directory.

## Bug reports

The game sends reports to `POST /api/bug-reports`. The game creates compact JSON and gzip-compresses it locally. It uploads a multipart form with a `metadata` JSON field (`title`, `body`, `is_crash`) and a `report` file (`.json.gz`). The server stores the uploaded gzip bytes unchanged in SQLite, then creates a GitHub issue with a short summary and a download link. The response includes `issue_url`, `issue_number`, and `report_url`. Full game state and diagnostics stay in the download instead of the issue body. Reports remain stored if GitHub issue creation fails.

`GET /api/bug-reports/{id}` downloads the `.json.gz` file as an attachment (`application/gzip`). The API does not decompress the file or set HTTP `Content-Encoding`, so browsers save the compressed file. Legacy uncompressed JSON reports still download as `.json`. Each URL uses a random 128-bit ID. Anyone with the URL can download the report; the link is included in the GitHub issue.

Set `PUBLIC_BASE_URL` to the public origin of this server (default `https://s30.throwingbones.com`). This setting controls download links; local development can use `http://localhost:8080`. Store the SQLite database (`DB_PATH`, default `s30.db`) on persistent storage and include it in backups. Docker Compose uses a named volume for this database.

The API accepts requests up to 16 MiB, with metadata limited to 64 KiB. This limit applies to the compressed upload; the API never expands it. The supplied nginx configuration uses the same limit. Apply that configuration, or an equivalent limit in your reverse proxy, when deploying. Older clients that send `application/json` and embed full state in the issue body are supported; the server removes that section and replaces it with the download link.

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
