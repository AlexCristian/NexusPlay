# Build a static binary; the pure-Go SQLite driver needs no cgo. The build
# stage runs on the build machine's own platform and cross-compiles for the
# target, so a multi-platform build only emulates the runtime stage.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine3.24 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/nexusplay ./cmd/nexusplay

# Runtime: the binary plus ffmpeg/ffprobe for probing and transcoding.
FROM alpine:3.24
LABEL org.opencontainers.image.source="https://github.com/AlexCristian/NexusPlay" \
      org.opencontainers.image.title="NexusPlay" \
      org.opencontainers.image.description="Self-hosted web media center" \
      org.opencontainers.image.licenses="AGPL-3.0-only"
RUN apk add --no-cache ffmpeg tzdata \
    && adduser -D -H -u 1000 nexusplay \
    && mkdir -p /data /media \
    && chown nexusplay:nexusplay /data
COPY --from=build /out/nexusplay /usr/local/bin/nexusplay

ENV NEXUSPLAY_ADDR=:8080 \
    NEXUSPLAY_MEDIA_DIR=/media \
    NEXUSPLAY_DATA_DIR=/data
USER 1000:1000
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 CMD ["nexusplay", "healthcheck"]
ENTRYPOINT ["nexusplay"]
