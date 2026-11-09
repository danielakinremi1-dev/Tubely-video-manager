#!/usr/bin/env bash
# End-to-end smoke test against a running Tubely and its S3 emulator.
#
#   make smoke          # against `make up-app` (the containerized server)
#   scripts/smoke.sh    # against any server; see the variables below
#
# It signs up two throwaway users and exercises every upload failure mode,
# a large multipart upload and a client disconnect. Then it checks that the
# only new objects in the bucket are the ones the test video references,
# that no multipart uploads are left open, and (if SMOKE_TMP_CMD is set)
# that the server left no temp files. Needs curl and python3; ffmpeg is
# optional (without it the large-file checks are skipped).
#
# SMOKE_AWS=1 (make smoke-aws) targets the AWS demo stack: the bucket is
# private, so it's listed with the AWS CLI and your credentials, and media
# comes from CloudFront, which answers 403 (not 404) for a missing key.

# SC2015: "check && ok || bad" is intended; ok only prints, so it can't fail.
# shellcheck disable=SC2015
set -uo pipefail

BASE=${BASE_URL:-http://localhost:8091}
S3=${S3_URL:-http://localhost:9090}
BUCKET=${S3_BUCKET:-tubely-local}
TMP_CMD=${SMOKE_TMP_CMD:-} # prints the server's temp dir contents
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

pass=0 fail=0
ok() { pass=$((pass + 1)); printf '  ok    %s\n' "$1"; }
bad() { fail=$((fail + 1)); printf '  FAIL  %s\n' "$1"; }
section() { printf '\n== %s\n' "$1"; }
json() { python3 -c "import sys, json; print(json.load(sys.stdin)$1)"; }

# expect NAME STATUS curl-args...: the status must match, and error
# responses must have a JSON {"error": ...} body.
expect() {
  local name=$1 want=$2 got
  shift 2
  got=$(curl -s -o "$WORK/body" -w '%{http_code}' "$@")
  if [[ $got == "$want" ]] && { [[ $want == 2* ]] || grep -q '"error"' "$WORK/body"; }; then
    ok "$name -> $got"
  else
    bad "$name: want $want, got $got $(head -c 200 "$WORK/body")"
  fi
}

# expect_media NAME STATUS-REGEX curl-args...: a status check for media
# URLs, which S3 or CloudFront answers itself (errors aren't Tubely's JSON).
expect_media() {
  local name=$1 want=$2 got
  shift 2
  got=$(curl -s -o /dev/null -w '%{http_code}' "$@")
  [[ $got =~ ^($want)$ ]] && ok "$name -> $got" || bad "$name: want $want, got $got"
}

AWS_MODE=${SMOKE_AWS:-0}
if [[ $AWS_MODE == 1 ]]; then
  # Pass the bucket's region: an org region guardrail may deny S3 calls
  # sent to the CLI's default region.
  bucket_keys() { aws s3api list-objects-v2 --region "${S3_REGION:-us-east-1}" --bucket "$BUCKET" --query 'Contents[].Key' --output text | tr '\t' '\n' | grep -v '^None$' | sort; }
  # shellcheck disable=SC2016 # backticks are JMESPath, not shell
  open_uploads() { aws s3api list-multipart-uploads --region "${S3_REGION:-us-east-1}" --bucket "$BUCKET" --query 'length(Uploads || `[]`)' --output text; }
else
  bucket_keys() { curl -s "$S3/$BUCKET?list-type=2&max-keys=1000" | grep -o '<Key>[^<]*' | sed 's/<Key>//' | sort; }
  open_uploads() { curl -s "$S3/$BUCKET?uploads" | grep -c '<UploadId>'; }
fi

# ---- preflight
if [[ $(curl -s -o /dev/null -w '%{http_code}' "$BASE/api/healthz") != 200 ]]; then
  echo "Tubely isn't healthy at $BASE (start it with 'make up-app' or 'make dev')" >&2
  exit 2
fi
if [[ $AWS_MODE != 1 ]] && ! curl -s -o /dev/null "$S3/$BUCKET?list-type=2"; then
  echo "No S3 emulator at $S3" >&2
  exit 2
fi
keys_before=$(bucket_keys)
uploads_before=$(open_uploads)

# ---- fixtures
M=$WORK/media
mkdir -p "$M"
cp "$ROOT/internal/media/testdata/landscape.mp4" "$M/small.mp4"
echo 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==' | base64 -d >"$M/thumb.png"
printf '%%PDF-1.4\nnot a video' >"$M/fake.pdf"
printf 'GIF89a\x01\x00\x01\x00\x00\x00\x00' >"$M/thumb.gif"
{ head -c 32 "$M/small.mp4"; head -c 4000 /dev/urandom; } >"$M/corrupt.mp4"
{ cat "$M/thumb.png"; head -c $((11 * 1024 * 1024)) /dev/zero; } >"$M/huge.png"
big=""
if command -v ffmpeg >/dev/null; then
  # Random noise doesn't compress: about 40 MB, over the 16 MiB multipart threshold.
  ffmpeg -v error -y -f lavfi -i testsrc=duration=1:size=1280x720:rate=30 \
    -vf noise=alls=100:allf=t -c:v mpeg4 -q:v 1 -pix_fmt yuv420p "$M/big.mp4" && big=$M/big.mp4
fi

# ---- users and a video
run=$(date +%s)-$RANDOM
for u in a b; do
  curl -s -o /dev/null -X POST "$BASE/api/users" -d "{\"email\":\"smoke-$u-$run@example.com\",\"password\":\"pw\"}"
done
TA=$(curl -s -X POST "$BASE/api/login" -d "{\"email\":\"smoke-a-$run@example.com\",\"password\":\"pw\"}" | json "['token']")
TB=$(curl -s -X POST "$BASE/api/login" -d "{\"email\":\"smoke-b-$run@example.com\",\"password\":\"pw\"}" | json "['token']")
A=(-H "Authorization: Bearer $TA")
B=(-H "Authorization: Bearer $TB")
V=$(curl -s -X POST "$BASE/api/videos" "${A[@]}" -d '{"title":"smoke test","description":"scripts/smoke.sh"}' | json "['id']")
VU=$BASE/api/video_upload/$V
TU=$BASE/api/thumbnail_upload/$V

section "request handling"
expect "malformed JSON" 400 -X POST "$BASE/api/videos" "${A[@]}" -d '{"title":'
expect "blank title" 400 -X POST "$BASE/api/videos" "${A[@]}" -d '{"title":"   "}'
expect "no token" 401 "$BASE/api/videos"
expect "other user reads video" 403 "$BASE/api/videos/$V" "${B[@]}"
expect "owner reads video" 200 "$BASE/api/videos/$V" "${A[@]}"

section "video upload failures"
expect "no token" 401 -X POST "$VU" -F "video=@$M/small.mp4;type=video/mp4"
expect "not the owner" 403 -X POST "$VU" "${B[@]}" -F "video=@$M/small.mp4;type=video/mp4"
expect "unknown video" 404 -X POST "$BASE/api/video_upload/$(python3 -c 'import uuid; print(uuid.uuid4())')" "${A[@]}" -F "video=@$M/small.mp4"
expect "malformed id" 400 -X POST "$BASE/api/video_upload/nope" "${A[@]}" -F "video=@$M/small.mp4"
expect "wrong form field" 400 -X POST "$VU" "${A[@]}" -F "file=@$M/small.mp4;type=video/mp4"
expect "PDF claiming to be MP4" 415 -X POST "$VU" "${A[@]}" -F "video=@$M/fake.pdf;type=video/mp4"
expect "unplayable MP4" 422 -X POST "$VU" "${A[@]}" -F "video=@$M/corrupt.mp4;type=video/mp4"

section "thumbnail upload failures"
expect "not the owner" 403 -X POST "$TU" "${B[@]}" -F "thumbnail=@$M/thumb.png;type=image/png"
expect "GIF claiming to be PNG" 415 -X POST "$TU" "${A[@]}" -F "thumbnail=@$M/thumb.gif;type=image/png"
expect "11 MB thumbnail" 413 -X POST "$TU" "${A[@]}" -F "thumbnail=@$M/huge.png;type=image/png"
expect "not multipart" 400 -X POST "$TU" "${A[@]}" -H "Content-Type: application/json" -d '{}'

if [[ -n $big ]]; then
  section "client disconnects mid-upload"
  curl -s -o /dev/null --limit-rate 10M --max-time 2 -X POST "$VU" "${A[@]}" -F "video=@$big;type=video/mp4"
  [[ $? == 28 ]] && ok "curl gave up after 2s, mid-body" || bad "curl finished before the disconnect"
fi

section "successful uploads"
expect "small MP4 (client says octet-stream)" 200 -X POST "$VU" "${A[@]}" -F "video=@$M/small.mp4;type=application/octet-stream"
first_url=$(json "['video_url']" <"$WORK/body")
if [[ -n $big ]]; then
  expect "$(($(stat -c %s "$big") / 1048576)) MB MP4 (multipart)" 200 -X POST "$VU" "${A[@]}" -F "video=@$big;type=video/mp4"
fi
video_url=$(json "['video_url']" <"$WORK/body")
expect_media "replaced object is gone" "403|404" "$first_url"
expect_media "stored video is served" 200 "$video_url"
curl -s -o "$WORK/video.dl" "$video_url"
python3 - "$WORK/video.dl" <<'PY' && ok "stored video is fast-start (moov before mdat)" || bad "stored video isn't fast-start"
import sys
d = open(sys.argv[1], "rb").read(1 << 20)
m, a = d.find(b"moov"), d.find(b"mdat")
sys.exit(0 if m >= 0 and (a < 0 or m < a) else 1)
PY
expect_media "range request (seeking)" 206 -H "Range: bytes=0-99" "$video_url"
expect "thumbnail" 200 -X POST "$TU" "${A[@]}" -F "thumbnail=@$M/thumb.png;type=image/png"
thumb_url=$(json "['thumbnail_url']" <"$WORK/body")

section "leftovers"
sleep 1
# The key is the URL path, after the bucket name for a path-style emulator URL.
key_of() { python3 -c "import sys, urllib.parse as p; u=sys.argv[1]; m='/$BUCKET/'; print(u.split(m, 1)[1] if m in u else p.urlparse(u).path.lstrip('/'))" "$1"; }
want_keys=$(printf '%s\n%s\n' "$(key_of "$video_url")" "$(key_of "$thumb_url")" | sort)
new_keys=$(comm -13 <(echo "$keys_before") <(bucket_keys))
[[ $new_keys == "$want_keys" ]] && ok "new objects are exactly the video and its thumbnail" ||
  bad "unexpected objects: $(comm -3 <(echo "$want_keys") <(echo "$new_keys") | tr '\n' ' ')"
[[ $(open_uploads) == "$uploads_before" ]] && ok "no multipart uploads left open" || bad "multipart uploads left open"
if [[ -n $TMP_CMD ]]; then
  left=$(eval "$TMP_CMD")
  [[ -z $left ]] && ok "no temp files on the server" || bad "temp files left: $left"
fi

section "delete"
expect "delete video" 204 -X DELETE "$BASE/api/videos/$V" "${A[@]}"
[[ -z $(comm -13 <(echo "$keys_before") <(bucket_keys)) ]] && ok "its objects are gone from the bucket" || bad "objects left after delete"

printf '\nPASS %d  FAIL %d\n' "$pass" "$fail"
((fail == 0))
