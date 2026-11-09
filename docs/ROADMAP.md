# Tubely roadmap

Tubely is being developed in phases from a single-binary course project into a production-shaped video service. Each checkbox is meant to be one reviewable commit. A phase is finished when every box is ticked and its **Done when** check passes.

**Guiding constraint:** everything runs locally with no cloud account. The same code deploys to AWS (S3 + CloudFront) through Terraform, and that environment is temporary: it's created for a demo and destroyed after.

## Architecture

**Today (after Phase 2):** one Go process, `cmd/tubely`, which wires concrete implementations into `internal/api`. The API layer handles auth (`requireAuth` middleware; videos are owner-only), validates uploads by their bytes, and reaches media storage and processing only through the `storage.Store` and `media.Processor` interfaces: S3 (multipart) and ffprobe/ffmpeg in production, an in-memory store and a fake in tests. SQLite stores object keys, not URLs, with a goose-versioned schema. The server has timeouts, graceful shutdown, structured request logs and a health check, and ships as a Docker image. ffmpeg still runs in the request path; moving it to a background worker is under Later.

**Target:**

```mermaid
flowchart LR
    UI[Web UI] -->|JWT| API[Go API]
    API --> DB[(SQLite / Postgres)]
    API --> Media[media: ffprobe / ffmpeg]
    API --> Storage[storage: S3 API]
    Storage --> Local[S3 emulator<br/>local and CI]
    Storage --> S3[AWS S3<br/>private bucket]
    S3 -->|OAC| CF[CloudFront]
    CF -->|playback| UI
    Local -->|playback| UI
```

Design rules the phases build toward:
- **Store object keys, not URLs.** Public URLs are built at response time from `MEDIA_BASE_URL`, so changing the storage backend or CDN needs no data migration.
- **Put storage and media behind interfaces**, so handlers can be tested with fakes and the S3 backend can be swapped through configuration.
- **Short-lived access tokens plus refresh tokens kept in the database**, so sessions can be revoked.

---

## Phase 0: Foundations and crash fixes ✅

- [x] Module renamed to `github.com/danielakinremi1-dev/tubely`; stray binary removed and ignored
- [x] `/api/refresh` no longer panics on an unknown token (`database.ErrNotFound` → 401)
- [x] Revoked and expired refresh tokens are rejected
- [x] Missing videos return 404 on get, delete and both upload endpoints
- [x] Access tokens last 1h; the web UI refreshes them transparently and revokes them on logout
- [x] Removed the unused `lib/pq` import; `golang-jwt/jwt/v5` moved from a pre-release to stable
- [x] Fixed the `DB_PATH` error message; removed a debug print

## Phase 0.5: S3-compatible storage that runs locally ✅

Uploads and playback currently depend on an AWS stack that no longer exists. This phase makes the storage backend configurable and runnable on a laptop.

- [x] Configurable S3 client: optional `S3_ENDPOINT` and `S3_FORCE_PATH_STYLE`, set through `s3.Options.BaseEndpoint` / `UsePathStyle`. Empty means real AWS.
- [x] `MEDIA_BASE_URL` for public playback URLs. It falls back to `https://$S3_CF_DISTRO`, and `S3_CF_DISTRO` becomes optional.
- [x] Store object keys instead of URLs: add a `video_key` column (a guarded `ALTER TABLE`, because the current migration only creates tables), backfill keys from existing URL paths, and build URLs at response time.
- [x] Thumbnails go into the same bucket under `thumbnails/`. Remove the local `assets/` storage and the hard-coded `http://localhost` URL.
- [x] Deleting a video removes its video and thumbnail objects, and a re-upload removes the previous object. Storage errors are logged, and the row is still deleted.
- [x] Upload-path fixes needed for local testing: the `other/` key prefix, `getVideoAspectRatio` error handling (the `cmd.Run()` error, no streams, zero height, `-select_streams v:0`), and closing and removing the `.processing` file.
- [x] `docker-compose.yml` with **`adobe/s3mock:5.2.3`** (Apache 2.0, no account, port 9090). It creates the bucket on startup with `COM_ADOBE_TESTING_S3MOCK_STORE_INITIAL_BUCKETS=tubely-local` and allows public reads, which is fine because it only runs locally. It is path-style only, so the local settings are `S3_FORCE_PATH_STYLE=true` and `MEDIA_BASE_URL=http://localhost:9090/tubely-local`.
- [x] Verify that the emulator serves unsigned browser GETs and HTTP Range requests (needed for video seeking). If either fails, switch to SeaweedFS.
- [x] `.env.example` with working local defaults, plus a `Makefile` with `up`, `down`, `dev` and `test`.

*Emulators not chosen:* LocalStack (new images require an auth token, which breaks "fresh clone just works" and would need a CI secret), MinIO (community edition archived, images removed), Garage (too much setup for local dev).

**Done when:** a fresh clone, then `cp .env.example .env && docker compose up && go run .`, gives a working upload and playback in the browser with no AWS credentials on the machine.

## Phase 1: Upload pipeline correctness ✅

**Video upload**
- [x] `exec.CommandContext` with a timeout for ffmpeg/ffprobe, with stderr captured in the returned error
- [x] Use `r.Context()` for S3 calls so a cancelled request stops the upload
- [x] `uuid.NewString()` for object keys instead of unchecked `rand.Read`
- [x] Ownership mismatch returns 403 with a JSON error, not 401 with `{}`
- [x] Fix the "Missing Content-Type for thumbnail" message in the video handler
- [x] Sniff the real content type (`http.DetectContentType`) instead of trusting the client header
- [x] S3 transfer manager (`feature/s3/transfermanager`, the successor to the deprecated `feature/s3/manager`) for multipart uploads of large files

**Thumbnail upload**
- [x] Check the `ParseMultipartForm` error and cap the body with `http.MaxBytesReader` (10 MB)

**Database**
- [x] Schema fixes: `videos.user_id` becomes `TEXT`, the `video_url TEXT TEXT` typo is fixed, and foreign keys get `ON DELETE CASCADE`
- [x] Turn on SQLite foreign-key enforcement (`_foreign_keys=on`)
- [x] `UpdateVideo` sets `updated_at` and stops passing pointer-to-pointer arguments
- [x] Versioned migrations with goose. The baseline matches the current schema, including `video_key`.

**Request handling**
- [x] JSON decode errors return 400
- [x] Validate video titles: required, with a length limit

**Done when:** every upload failure mode returns the correct 4xx/5xx with a JSON error, and no temp files or orphaned objects are left behind.

## Phase 2: Structure and operability ✅

**Structure**
- [x] `storage` interface (`Put`, `Delete`) with the S3 implementation and an in-memory fake
- [x] `media` interface (`Probe`, `FastStart`) with the ffmpeg implementation, a pure aspect-ratio classifier and a fake
- [x] Split into `cmd/tubely`, `internal/api`, `internal/storage` and `internal/media`, next to the existing `internal/auth` and `internal/database`
- [x] `requireAuth` middleware that puts the user ID into the request context (it replaces five copies of the JWT parsing), make `GET /api/videos/{id}` owner-only, and pin the JWT signing method to HS256

**Operations**
- [x] `http.Server` timeouts, with longer deadlines for the upload routes (`http.ResponseController`)
- [x] Graceful shutdown on SIGINT/SIGTERM
- [x] `log/slog` structured logging, request-ID middleware and panic recovery
- [x] Startup check that ffmpeg/ffprobe are on PATH, plus `GET /api/healthz`

**Tests and packaging**
- [x] Unit tests: `getExtension`, the aspect-ratio classifier and the auth helpers
- [x] Handler tests with `httptest` and the fake storage/media implementations
- [x] Small sample MP4s in `testdata/` for the ffprobe tests
- [x] Multi-stage `Dockerfile` with ffmpeg, an `app` service in compose, and `scripts/smoke.sh` (the end-to-end upload check)
- [x] `make lint` (golangci-lint)

**Done when:** handlers have no direct AWS or ffmpeg calls, and `go test ./...` covers the main upload path without network access.

## Phase 3: AWS infrastructure as code ✅

Everything here is written and validated offline (`terraform fmt` / `validate` need no credentials). The Done-when demo ran on 2026-11-09 in a sandbox member account (us-east-2):
- `make smoke-aws` passed 28/28.
- CloudFront served the media, and direct S3 reads were denied.
- The app role could upload and delete but not read or list.
- `make aws-down` left nothing tagged `Project=tubely`.

- [x] `infra/` skeleton: Terraform and providers pinned (lock file committed), `default_tags` with `Project=tubely`, a region variable, local state only (git-ignored), and `make tf-fmt` / `make tf-validate`
- [x] Private S3 bucket: all public access blocked, bucket-owner-enforced, SSE-S3, a TLS-only policy, `force_destroy`, and lifecycle rules (expire objects after 7 days, abort incomplete multipart uploads after 1 day)
- [x] CloudFront with Origin Access Control, `PriceClass_100` and HTTPS only; the bucket policy lets only that distribution read
- [x] Least-privilege app role: `s3:PutObject`, `s3:DeleteObject` and `s3:AbortMultipartUpload` on `bucket/*` only, assumed for temporary credentials (no long-lived keys). The app never reads objects; CloudFront does.
- [x] AWS Budgets alert ($1 actual, $5 forecast), included in the first apply
- [x] `docs/AWS_DEMO.md` runbook, an `ENV_FILE` setting, `make aws-up` (apply, then write `.env.aws`), `make dev-aws`, and `make aws-down` (destroy, then check that nothing tagged `Project=tubely` is left)

**Done when:** `make aws-up` → upload and play a video → `make aws-down` works end to end.

## Phase 4: Continuous integration

- [x] GitHub Actions: `gofmt`, `go vet`, golangci-lint, and `go test -race -cover` (with ffmpeg installed)
- [x] Integration tests against the S3 emulator as a service container: upload → key stored → URL built → delete removes the object
- [ ] `terraform fmt -check`, `terraform validate` and the Trivy scan (`make tf-scan`). No `apply` and no AWS credentials in CI.
- [ ] CI badge in the README
- [ ] Branch protection on `main`: changes arrive by pull request, and the CI checks must pass before merging

**Done when:** a pull request to `main` can't merge until CI is green, and `main` itself is green, with the README badge showing it. CI runs the same `make` targets as a local checkout, with the same pinned tool versions.

## Phase 5: Documentation

- [ ] README rewrite: overview, demo GIF, architecture diagram, local quickstart, API table, and credits for the course this project started from
- [ ] Design notes: fast-start (`moov` atom) for streaming, CloudFront vs. a public bucket, presigned vs. CDN URLs, aspect-ratio key prefixes, keys vs. URLs

## Later

- [ ] PostgreSQL + sqlc behind a `database` interface (deferred from Phase 1)
- [ ] Sharing and visibility (private / unlisted / public) with signed media URLs (CloudFront signed URLs or S3 presigned GETs), so private videos stay private at the CDN as well as in the API
- [ ] Asynchronous processing: the upload returns `202 Accepted`, and a worker pool runs ffprobe/ffmpeg and the upload with retries (`processing → ready | failed`). The UI polls for status.
- [ ] Direct-to-S3 uploads with presigned PUT URLs, so large files never pass through the API
- [ ] Automatic thumbnail generation with ffmpeg when none is uploaded
- [ ] HLS transcoding into multiple renditions for adaptive streaming
