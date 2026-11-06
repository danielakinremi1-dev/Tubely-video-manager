# syntax=docker/dockerfile:1

# ---- build: compile with cgo (the SQLite driver is C) ----
FROM golang:1.27.1-bookworm AS build
WORKDIR /src

# Dependencies first, so editing code doesn't re-download modules.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/tubely ./cmd/tubely

# ---- runtime: the binary, the web UI, ffmpeg, and nothing else ----
# Same Debian release as the builder, so the cgo binary's glibc matches.
FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*

# Run as an unprivileged user; /data holds the SQLite database (a volume).
RUN useradd --system --uid 10001 --home-dir /nonexistent --shell /usr/sbin/nologin tubely \
 && mkdir /data && chown tubely /data

COPY --from=build /out/tubely /usr/local/bin/tubely
COPY app /srv/tubely/app

ENV DB_PATH=/data/tubely.db \
    FILEPATH_ROOT=/srv/tubely/app \
    PORT=8091
USER tubely
EXPOSE 8091
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s \
  CMD curl -fsS "http://localhost:${PORT}/api/healthz" || exit 1
ENTRYPOINT ["/usr/local/bin/tubely"]
