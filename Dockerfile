# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /app

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o video-compressor ./cmd/server

# Debian's ffmpeg is built with NVENC support and loads the NVIDIA driver
# libraries injected by the NVIDIA Container Toolkit at runtime.
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        tzdata \
        ffmpeg \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/video-compressor .

ENV PORT=8080 \
    NO_BROWSER=1 \
    MAX_WORKERS=2 \
    DOCKER=true

EXPOSE 8080

CMD ["./video-compressor"]
