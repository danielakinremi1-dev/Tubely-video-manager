# CLAUDE.md: Tubely

Tubely is a video-hosting backend in Go. Users sign up, create video records, upload an MP4 and a thumbnail, and play videos back through a CDN. It started as the boot.dev "File Servers and CDNs with S3 and CloudFront" starter and is being developed in phases into a production-shaped service. The plan is in `docs/ROADMAP.md`. Work one phase at a time and don't jump ahead.

If a `notes/` directory exists locally, read it for maintainer context. It is git-ignored, so never copy its contents into committed files.

## Stack

- Go (version in `go.mod`), standard-library `net/http` with method-pattern routing (`mux.HandleFunc("POST /api/...")`)
- SQLite through `mattn/go-sqlite3`, which needs cgo. The schema is managed by goose migrations that run automatically on startup (`internal/database/migrate.go`).
- `aws-sdk-go-v2` for S3. Locally, `docker compose` runs `adobe/s3mock` on :9090 (path-style, no auth). On AWS, media is served through CloudFront. The DB stores object **keys**; public URLs are built at response time from `MEDIA_BASE_URL` (`withMediaURLs` in `internal/api/media_url.go`).
- `ffmpeg` and `ffprobe` must be on PATH. They handle fast-start remuxing and aspect-ratio detection.
- Frontend: vanilla JS, HTML and CSS in `app/`, served at `/app/`. There is no build step.
- Auth: argon2id password hashes, 1h JWT access tokens, and refresh tokens stored in the DB (`internal/auth`). The UI refreshes access tokens through `authFetch` in `app/app.js`.

## Commands

```bash
cp .env.example .env        # local defaults work as-is with the emulator
make up                     # docker compose up -d (S3 emulator on :9090)
go run ./cmd/tubely         # serves http://localhost:$PORT/app/  (or: make dev)
make check                  # go build ./... && go vet ./... && go test ./...
make lint                   # golangci-lint v2 (.golangci.yml); local binary, else the pinned Docker image
make tf-fmt tf-validate     # offline Terraform checks for infra/ (no AWS credentials needed)
make test-integration       # also run S3 tests against the emulator (starts it)
make up-app                 # build and run the API in Docker too (compose profile "app")
make smoke                  # scripts/smoke.sh end-to-end check against make up-app
make down                   # stop services; `docker compose down -v` wipes uploads
./samplesdownload.sh        # sample videos and images into samples/
```

`GET /` returns 404 by design. The UI is at `/app/`.

## Layout

- `cmd/tubely/main.go`: reads config from env (`config.go` resolves `MEDIA_BASE_URL`), builds the dependencies, starts the server
- `internal/api`: the HTTP layer. `server.go` has `Config`, `NewServer` and the routes; handlers are methods on `*Server`.
  - `handler_*.go`: one file per resource
  - `upload_helpers.go`: content-type sniffing, upload form errors, file extensions, object key generation
  - `objects.go`: `deleteObject` / `replaceObject`, which log failures and don't return them
  - `media_url.go`: key → public URL (`withMediaURLs`)
  - `json.go`: `respondWithJSON` / `respondWithError` / `decodeJSON`. Use these for every response.
  - `handlers_test.go`: the `httptest` harness (`newTestServer`, `serve`, `assertJSONError`), using `storage.Memory` and `media.Fake`
- `internal/storage`: the `Store` interface for media objects, the S3 implementation (endpoint, path-style, multipart via the transfer manager) and an in-memory `Memory` store for tests
- `internal/media`: the `Processor` interface (`Probe`, `FastStart`), the `FFmpeg` implementation (timeouts, stderr in errors), `ClassifyAspectRatio` and a `Fake` for tests
- `internal/database`: SQL by hand, with no ORM. Lookups return `database.ErrNotFound` when a row is missing.
- `internal/database/migrations/`: numbered goose SQL migrations (`NNNNN_name.sql`), embedded in the binary. Never edit one that has shipped; add a new one.
- `internal/auth`: JWT, refresh tokens, password hashing, bearer header parsing
- `docker-compose.yml`, `Makefile`, `Dockerfile`: local services (S3Mock, plus the app under the `app` profile), shortcuts, and the multi-stage image (cgo build, then bookworm-slim with ffmpeg, non-root)
- `scripts/smoke.sh`: end-to-end HTTP check of a running server; run it after changes to the upload path
- `app/`: frontend
- `infra/`: Terraform for the AWS demo stack (local state, git-ignored; `.terraform.lock.hcl` is committed)
- `docs/`: public docs (`ROADMAP.md`)

## Conventions

- Protected routes are wrapped in `s.requireAuth(...)` in `server.go` (401 without a valid token); handlers read the caller with `userIDFrom(r.Context())`. Handlers for one video call `s.loadOwnedVideo(w, r)`, which handles bad IDs (400), missing rows (404) and other owners (403). Videos are private to their owner.
- Errors go through `respondWithError(w, status, msg, err)`. Don't write raw `http.Error` calls. `msg` goes to the client; `err` is attached to the request's access-log line.
- Log with `log/slog`. Inside a request, use `logFrom(ctx)` so lines carry the request ID. Every request already gets one access-log line, so don't add per-handler "request done" logs.
- New config comes from env vars read in `cmd/tubely/main.go` and passed in through `api.Config`, with a matching entry in `.env.example`. Optional vars need a sensible default. Don't make them `log.Fatal` when unset.
- The module path is `github.com/danielakinremi1-dev/tubely`.
- Handlers reach S3 and ffmpeg only through `storage.Store` and `media.Processor`. Never import the AWS SDK or `os/exec` in `internal/api`.
- Match the existing style: small files, early returns, no extra frameworks.

## AWS and cost rules (important)

- The real AWS stack (S3, CloudFront) was **torn down on purpose** to avoid charges. Existing `video_url` values that point at the old CloudFront domain are dead. That's expected.
- **Never** run `terraform apply`, create AWS resources, or call real AWS without the user's explicit OK in that conversation. Local dev and tests use the S3 emulator.
- Any AWS infrastructure must be Terraform under `infra/`, tagged `Project=tubely`, destroyable with one command, and must keep the budget alert.
- Never commit `.env*` files (except `.env.example`), AWS credentials, Terraform state or `*.tfvars`. `.gitignore` covers all of these.

## Workflow

- Before editing, read the relevant phase in `docs/ROADMAP.md`. Tick each item as it lands, in the same commit.
- Each phase gets its own branch, `phase-N-<slug>` (for example `phase-3-aws-terraform`), cut from an up-to-date `main`. Never commit to `main` directly.
- One commit per roadmap checkbox, with a clear message. Stage files by path, never `git add -A`. Don't push unless asked, and then push the branch, never `main`.
- When the phase is done and the user has reviewed it, open a PR to `main` with `gh pr create` (the default repo is `danielakinremi1-dev/Tubely-video-manager`; never target the `upstream` bootdotdev repo). The body covers the summary, decisions, bugs found and verification. Merge with a merge commit (`gh pr merge --merge`), not a squash, so the per-checkbox history survives.
- After each commit-sized change, run `go build ./... && go vet ./... && go test ./...`, `gofmt -l .` and `make lint`. Never commit a failing build or new lint findings. Fix the cause rather than adding `//nolint`; when a finding is deliberate, use `//nolint:<linter> // reason`.
- At the end of each phase, stop for the user's review before starting the next one.
- For frontend changes, list the manual browser checks the user should do, because JS can't be run here.
- Keep the README accurate for a newcomer: a quickstart that works locally, an architecture diagram, and an honest description of what runs locally and what runs on AWS.
