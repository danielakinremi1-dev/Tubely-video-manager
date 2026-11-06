# CLAUDE.md: Tubely

Tubely is a video-hosting backend in Go. Users sign up, create video records, upload an MP4 and a thumbnail, and play videos back through a CDN. It started as the boot.dev "File Servers and CDNs with S3 and CloudFront" starter and is being developed in phases into a production-shaped service. The plan is in `docs/ROADMAP.md`. Work one phase at a time and don't jump ahead.

If a `notes/` directory exists locally, read it for maintainer context. It is git-ignored, so never copy its contents into committed files.

## Stack

- Go (version in `go.mod`), standard-library `net/http` with method-pattern routing (`mux.HandleFunc("POST /api/...")`)
- SQLite through `mattn/go-sqlite3`, which needs cgo. The schema is managed by goose migrations that run automatically on startup (`internal/database/migrate.go`).
- `aws-sdk-go-v2` for S3. Locally, `docker compose` runs `adobe/s3mock` on :9090 (path-style, no auth). On AWS, media is served through CloudFront. The DB stores object **keys**; public URLs are built at response time from `MEDIA_BASE_URL` (`withMediaURLs` in `media_url.go`).
- `ffmpeg` and `ffprobe` must be on PATH. They handle fast-start remuxing and aspect-ratio detection.
- Frontend: vanilla JS, HTML and CSS in `app/`, served at `/app/`. There is no build step.
- Auth: argon2id password hashes, 1h JWT access tokens, and refresh tokens stored in the DB (`internal/auth`). The UI refreshes access tokens through `authFetch` in `app/app.js`.

## Commands

```bash
cp .env.example .env        # local defaults work as-is with the emulator
make up                     # docker compose up -d (S3 emulator on :9090)
go run .                    # serves http://localhost:$PORT/app/  (or: make dev)
make check                  # go build ./... && go vet ./... && go test ./...
make test-integration       # also run S3 tests against the emulator (starts it)
make down                   # stop services; `docker compose down -v` wipes uploads
./samplesdownload.sh        # sample videos and images into samples/
```

`GET /` returns 404 by design. The UI is at `/app/`.

## Layout

- `main.go`: env/config loading, the `apiConfig` struct, routes
- `handler_*.go`: one file per resource. Handlers are methods on `*apiConfig`.
- `helper_functions_upload.go`: ffmpeg/ffprobe helpers, file extensions, object key generation
- `internal/storage`: the `Store` interface for media objects, the S3 implementation (endpoint, path-style, multipart via the transfer manager) and an in-memory `Memory` store for tests
- `objects.go`: `deleteObject` / `replaceObject`, which log failures and don't return them
- `media_url.go`: `MEDIA_BASE_URL` resolution and key → URL building
- `docker-compose.yml`, `Makefile`: local services and shortcuts
- `json.go`: `respondWithJSON` / `respondWithError`. Use these for every response.
- `internal/database`: SQL by hand, with no ORM. Lookups return `database.ErrNotFound` when a row is missing.
- `internal/database/migrations/`: numbered goose SQL migrations (`NNNNN_name.sql`), embedded in the binary. Never edit one that has shipped; add a new one.
- `internal/auth`: JWT, refresh tokens, password hashing, bearer header parsing
- `app/`: frontend
- `docs/`: public docs (`ROADMAP.md`)

## Conventions

- Every protected handler follows the same steps: parse the path ID, call `auth.GetBearerToken`, call `auth.ValidateJWT`, load the record (`ErrNotFound` → 404), check ownership (→ 403).
- Errors go through `respondWithError(w, status, msg, err)`. Don't write raw `http.Error` calls.
- New config comes from env vars read in `main.go`, with a matching entry in `.env.example`. Optional vars need a sensible default. Don't make them `log.Fatal` when unset.
- The module path is `github.com/danielakinremi1-dev/tubely`.
- Match the existing style: small files, early returns, no extra frameworks.

## AWS and cost rules (important)

- The real AWS stack (S3, CloudFront) was **torn down on purpose** to avoid charges. Existing `video_url` values that point at the old CloudFront domain are dead. That's expected.
- **Never** run `terraform apply`, create AWS resources, or call real AWS without the user's explicit OK in that conversation. Local dev and tests use the S3 emulator.
- Any AWS infrastructure must be Terraform under `infra/`, tagged `Project=tubely`, destroyable with one command, and must keep the budget alert.
- Never commit `.env*` files (except `.env.example`), AWS credentials, Terraform state or `*.tfvars`. `.gitignore` covers all of these.

## Workflow

- Before editing, read the relevant phase in `docs/ROADMAP.md`. Tick each item as it lands, in the same commit.
- One commit per roadmap checkbox, with a clear message. Stage files by path, never `git add -A`. Don't push unless asked.
- After each commit-sized change, run `go build ./... && go vet ./... && go test ./...` and `gofmt -l .`. Never commit a failing build.
- At the end of each phase, stop for the user's review before starting the next one.
- For frontend changes, list the manual browser checks the user should do, because JS can't be run here.
- Keep the README accurate for a newcomer: a quickstart that works locally, an architecture diagram, and an honest description of what runs locally and what runs on AWS.
