# Build a static binary; the pure-Go SQLite driver needs no cgo.
FROM golang:1.27-alpine3.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/nexusplay ./cmd/nexusplay

# Runtime: the binary plus ffmpeg/ffprobe for probing and transcoding.
FROM alpine:3.24
RUN apk add --no-cache ffmpeg tzdata \
    && adduser -D -H -u 1000 nexusplay \
    && mkdir -p /data /media \
    && chown nexusplay:nexusplay /data
COPY --from=build /out/nexusplay /usr/local/bin/nexusplay

ENV NEXUSPLAY_ADDR=:8080 \
    NEXUSPLAY_MEDIA_DIR=/media \
    NEXUSPLAY_DATA_DIR=/data
USER nexusplay
EXPOSE 8080
VOLUME ["/data"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 CMD ["nexusplay", "healthcheck"]
ENTRYPOINT ["nexusplay"]
