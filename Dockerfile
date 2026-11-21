FROM golang:1.27.4-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/watplan ./cmd/watplan

FROM ubuntu:24.04

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl libnss3 nss-plugin-pem \
    && rm -rf /var/lib/apt/lists/*

ARG TARGETARCH=amd64

RUN case "$TARGETARCH" in \
        amd64) archive=curl-impersonate-v2.3.2.x86_64-linux-gnu.tar.gz ;; \
        arm64) archive=curl-impersonate-v2.3.2.aarch64-linux-gnu.tar.gz ;; \
        *) echo "unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac \
    && curl -fsSL "https://github.com/lexiforest/curl-impersonate/releases/download/v2.3.2/$archive" -o /tmp/curl-impersonate.tar.gz \
    && tar -xzf /tmp/curl-impersonate.tar.gz -C /tmp \
    && install -m 0755 /tmp/curl-impersonate /tmp/curl_chrome136 /usr/local/bin/ \
    && rm -rf /tmp/curl-impersonate.tar.gz /tmp/curl-impersonate /tmp/curl_chrome136

RUN groupadd --system app \
    && useradd --system --gid app --home /app app \
    && mkdir -p /app/data \
    && chown -R app:app /app

WORKDIR /app
COPY --from=build /out/watplan /usr/local/bin/watplan
COPY --chown=app:app plans.json sources.json /app/

ENV PLANWAT_DATA=/app/data

EXPOSE 8080

USER app

ENTRYPOINT ["/bin/sh", "-c", "exec env PLANWAT_ADDR=0.0.0.0:${PORT:-8080} /usr/local/bin/watplan"]
