# CLAUDE.md: Tubely

Tubely is a video-hosting backend in Go. Users sign up, create video records, upload an MP4 and a thumbnail, and play videos back through a CDN. It started as the boot.dev "File Servers and CDNs with S3 and CloudFront" starter and is being developed in phases into a production-shaped service. The plan is in `docs/ROADMAP.md`. Work one phase at a time and don't jump ahead.

If a `notes/` directory exists locally, read it for maintainer context. It is git-ignored, so never copy its contents into committed files.

## Stack

- Go (version in `go.mod`), standard-library `net/http` with method-pattern routing (`mux.HandleFunc("POST /api/...")`)
- SQLite through `mattn/go-sqlite3`, which needs cgo. Until goose migrations land (Phase 1), the schema is auto-migrated in `internal/database/database.go`.
- `aws-sdk-go-v2` for S3. Videos are served through CloudFront on AWS, or through a local S3 emulator in dev (Phase 0.5).
- `ffmpeg` and `ffprobe` must be on PATH. They handle fast-start remuxing and aspect-ratio detection.
- Frontend: vanilla JS, HTML and CSS in `app/`, served at `/app/`. There is no build step.
- Auth: argon2id password hashes, 1h JWT access tokens, and refresh tokens stored in the DB (`internal/auth`). The UI refreshes access tokens through `authFetch` in `app/app.js`.

## Commands

```bash
cp .env.example .env        # then fill in the values
go run .                    # serves http://localhost:$PORT/app/
go build ./... && go vet ./... && go test ./...
./samplesdownload.sh        # sample videos and images into samples/
```

`GET /` returns 404 by design. The UI is at `/app/`.

## Layout

- `main.go`: env/config loading, the `apiConfig` struct, routes
- `handler_*.go`: one file per resource. Handlers are methods on `*apiConfig`.
- `helper_functions_upload.go`: ffmpeg/ffprobe helpers, file extensions, assets dir
- `json.go`: `respondWithJSON` / `respondWithError`. Use these for every response.
- `internal/database`: SQL by hand, with no ORM. Lookups return `database.ErrNotFound` when a row is missing.
- `internal/auth`: JWT, refresh tokens, password hashing, bearer header parsing
- `app/`: frontend
- `docs/`: public docs (`ROADMAP.md`)

## Conventions

- Every protected handler follows the same steps: parse the path ID, call `auth.GetBearerToken`, call `auth.ValidateJWT`, load the record (`ErrNotFound` → 404), check ownership (→ 403). The upload handlers still return 401 for an ownership failure until Phase 1 fixes it.
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
