# Build the Go API + seed binaries.
FROM golang:1.25 AS build
WORKDIR /src
COPY apps/api/go.mod apps/api/go.sum ./apps/api/
WORKDIR /src/apps/api
RUN go mod download
COPY apps/api/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOOS=linux go build -o /out/seed ./cmd/seed

# Runtime image includes headless Chromium for PDF export.
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates chromium tzdata \
 && rm -rf /var/lib/apt/lists/*
ENV PDF_BINARY=chromium PDF_RENDERER=chromium STORAGE_DIR=/data/storage
RUN mkdir -p /data/storage
COPY --from=build /out/api /usr/local/bin/api
COPY --from=build /out/seed /usr/local/bin/seed
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/api"]
