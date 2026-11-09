# Design notes

Short notes on the decisions behind Tubely. Each one gives the problem, what the code does about it, what that costs, and where to look.

- [Fast start: the `moov` atom](#fast-start-the-moov-atom)
- [CloudFront in front of a private bucket](#cloudfront-in-front-of-a-private-bucket)
- [CDN URLs vs presigned URLs](#cdn-urls-vs-presigned-urls)
- [Aspect-ratio key prefixes](#aspect-ratio-key-prefixes)
- [Store keys, build URLs](#store-keys-build-urls)
- [Upload ordering that leaves no orphans](#upload-ordering-that-leaves-no-orphans)
- [Credentials split by job](#credentials-split-by-job)

## Fast start: the `moov` atom

**Problem.** An MP4 file is a sequence of boxes, called atoms. The two that matter here are `mdat`, which holds the audio and video data, and `moov`, the index that says where each frame is and how long the video lasts. Many encoders write `moov` last, because they only know the index once the media has been written. A browser can't play or seek without the index, so it has to download the entire file before playback starts. On a phone connection, a 500 MB upload is then unwatchable.

**Decision.** Every upload is remuxed with `ffmpeg -c copy -movflags faststart`, which rewrites the file with `moov` before `mdat` ([internal/media/ffmpeg.go#L69-L72](../internal/media/ffmpeg.go#L69-L72)). The player can then read the index from the first few kilobytes, start playing, and fetch any other position with an HTTP Range request.

**Trade-off.** `-c copy` copies the streams without re-encoding them. That makes the step I/O-bound and fast: seconds for a large file, compared with minutes for a transcode. It also leaves quality untouched. The cost is one extra pass over the file on disk. It doesn't change codecs or produce multiple bitrates; adaptive streaming (HLS) is a separate, later step.

**Verified by:**
- `scripts/smoke.sh` and [internal/api/integration_test.go](../internal/api/integration_test.go) download the stored video and check that `moov` comes before `mdat`;
- the test MP4s in `internal/media/testdata` are deliberately *not* fast-start.

## CloudFront in front of a private bucket

**Problem.** The simplest way to serve media is a public-read bucket. Every play then comes from a single region at S3's data-transfer price, nothing is cached, and anyone who learns or guesses a key can download the object, with no way to put a CDN, rate limit or signed access in front of it later.

**Decision.** The bucket is private: all four public-access blocks are on, and object ACLs are disabled ([infra/s3.tf#L17-L34](../infra/s3.tf#L17-L34)). CloudFront reads from it through **Origin Access Control**, which signs each request to S3 with SigV4 ([infra/cloudfront.tf#L5-L11](../infra/cloudfront.tf#L5-L11)). The bucket policy allows `s3:GetObject` only to the CloudFront service *and* only when `AWS:SourceArn` is this one distribution ([infra/s3.tf#L74-L95](../infra/s3.tf#L74-L94)). Without the second condition, any CloudFront distribution in any account could read the bucket.

Viewers hit the nearest edge location:
- `Managed-CachingOptimized` keeps copies there;
- Range requests make seeking work;
- `PriceClass_100` limits the edges to the cheapest regions, North America and Europe ([infra/cloudfront.tf#L20-L45](../infra/cloudfront.tf#L20-L58)).

**Trade-off.** A distribution takes minutes to create or delete, and a cached object can be served for a while after it's deleted from S3. That's acceptable for immutable, UUID-named media. Replacing a video always writes a new key, so there's nothing to invalidate.

**Verified by:** the Phase 3 demo on AWS.
- An object fetched through CloudFront returned `200`, and `206` for a byte range.
- The same object fetched directly from S3 returned `403`.
- A missing key returned `403` through CloudFront, so it isn't possible to probe which keys exist.

## CDN URLs vs presigned URLs

**Problem.** Videos are private in the API: only the owner can list, read or delete them. The media files, though, are served from stable CDN URLs. Anyone holding a URL can play the file. The URLs are unguessable (a random UUID per object), but they aren't secret once shared.

**Options considered:**
- **S3 presigned GET URLs.** Time-limited and per-object, so expired URLs stop working. But each signature produces a different URL, so every request misses the CDN cache, and the bucket would need to accept direct reads again, outside CloudFront.
- **CloudFront signed URLs or signed cookies.** Also time-limited, and signed by a key that CloudFront knows. Caching still works, because the cache key can ignore the signature, and the bucket stays private behind OAC.
- **Stable CDN URLs (current).** The best caching and the simplest code, but access control stops at the API.

**Decision.** Stable CDN URLs for now, because every video has a single owner and there's no sharing yet. CloudFront signed URLs are the planned path once videos can be private, unlisted or public (the roadmap's *Later* section). For uploads, presigned **PUT** URLs would let large files go straight from the browser to S3 instead of through the API. That's also listed under *Later*.

## Aspect-ratio key prefixes

**Problem.** Phones and desktops produce different shapes. It's useful to know a video's shape without probing it again, and to be able to treat portrait clips differently (lifecycle rules, metrics, a future vertical feed).

**Decision.** ffprobe reads the dimensions. [`ClassifyAspectRatio`](../internal/media/media.go#L43-L59) classifies them with tolerance bands:

| Shape | Ratio | Accepted range |
|---|---|---|
| 16:9 | 1.78 | 1.72 to 1.84 |
| 9:16 | 0.5625 | 0.50 to 0.62 |

The bands absorb encoder rounding such as 1920×1088. The key gets a `landscape/`, `portrait/` or `other/` prefix, followed by a random UUID ([internal/api/handler_upload_video.go#L92](../internal/api/handler_upload_video.go#L92), [internal/api/upload_helpers.go#L15-L20](../internal/api/upload_helpers.go#L15-L20)).

**Trade-off.** S3 has no real folders: a prefix is just the start of the key, and it doesn't change performance. The prefix is organisational, so a per-prefix lifecycle rule or a `ListObjects` call can target one shape. The UUID is what does the real work: keys never collide, and they can't be guessed or enumerated.

## Store keys, build URLs

**Problem.** The original starter stored full URLs in the database. Changing the bucket, moving from an emulator to S3, or putting a CDN in front would have meant rewriting every row.

**Decision.** The database stores only object keys (`video_key`, `thumbnail_key`). [`withMediaURLs`](../internal/api/media_url.go#L10-L26) builds the public URL from `MEDIA_BASE_URL` for every response. When the schema changed, the migration backfilled keys from the old stored URLs, so existing rows kept working ([internal/database/migrate.go#L95-L140](../internal/database/migrate.go#L95-L140)).

**Trade-off.** All media must live under a single base URL. That's true today, and a per-row host would be a deliberate schema change.

**Verified by:** moving from S3Mock on `localhost:9090` to CloudFront for the Phase 3 demo changed configuration only (`MEDIA_BASE_URL`, `S3_*`). No code or data changed.

## Upload ordering that leaves no orphans

**Problem.** An upload touches two systems that can't share a transaction: S3 and SQLite. A crash or failure between them can leave either an object that no row points to (paid storage forever) or a row that points to an object that doesn't exist (a broken player).

**Decision.** The handler orders its steps so each failure has a clean fallback ([internal/api/handler_upload_video.go#L92-L116](../internal/api/handler_upload_video.go#L92-L115)):

1. **Put the new object under a new key.** If this fails, nothing has changed. A failed or cancelled multipart upload is aborted, so no parts are left behind either ([internal/storage/s3.go#L81-L96](../internal/storage/s3.go#L81-L96)).
2. **Point the row at the new key.** If this fails, the handler deletes the object it just uploaded.
3. **Delete the old object.** This only happens once the row no longer references it ([internal/api/objects.go#L10-L30](../internal/api/objects.go#L7-L27)).

Cleanup deletes use a context detached from the request, so they still run if the client disconnects mid-response.

**Trade-off.** If step 3 fails, the old object is orphaned. That's logged as a warning, never a user-facing error, and on the AWS demo stack the bucket's lifecycle rules expire every object after 7 days anyway. The alternative order, deleting first, would risk a row that points at nothing, which is worse.

**Verified by:** the handler tests force each step to fail and check that no objects are left over, that the key hasn't changed, and that no temp files remain. `scripts/smoke.sh` also audits the bucket after a real run.

## Credentials split by job

**Problem.** If the app's credentials can read the bucket, a leaked `.env` file exposes every user's videos. If they're long-lived access keys, a leak stays dangerous until someone notices and rotates them.

**Decision.**
- **The app can't read or list.** Its role can only put, delete and abort multipart uploads on `bucket/*` ([infra/iam.tf#L39-L55](../infra/iam.tf#L39-L55)). Reading is CloudFront's job.
- **The credentials are temporary.** The app *assumes* the role and gets short-lived STS credentials, which the SDK refreshes on its own ([internal/storage/s3.go#L53-L66](../internal/storage/s3.go#L53-L70)). No access keys are stored anywhere.
- **Only one identity can assume the role:** the one that deployed the stack ([infra/iam.tf#L11-L25](../infra/iam.tf#L11-L25)).

**Trade-off.** Debugging is less convenient: the app can't fetch an object to check it, so the smoke test audits the bucket with the operator's own credentials instead.

**Verified by:** on AWS, the role's `GetObject` and `ListObjectsV2` both returned `AccessDenied`, while `PutObject` and `DeleteObject` succeeded.
