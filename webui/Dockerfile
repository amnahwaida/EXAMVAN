# Stage 1: Build
# Docker context: EXAMVAN/ (parent of webui/)
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Copy dependency files first for layer caching.
COPY webui/go.mod webui/go.sum ./
RUN go mod download

# Copy the rest of the source.
COPY webui/ .

# Build statically linked binary. The version is derived from a hash of the
# static assets so ?v= URLs change whenever CSS/JS change — this busts the
# long-lived (365d, immutable) browser/Cloudflare asset caches automatically.
RUN VERSION_SUFFIX=$(find static -type f 2>/dev/null | sort | xargs md5sum 2>/dev/null | md5sum | cut -c1-8); \
    CGO_ENABLED=0 go build -ldflags "-X github.com/examvan/webui/internal/config.DefaultVersion=2.7.3-${VERSION_SUFFIX}" -o server ./cmd/server

# Stage 2: Runtime
FROM alpine:3.19

RUN apk add --no-cache curl tzdata

RUN adduser -D -H -u 1000 appuser

COPY --from=builder /build/server /usr/local/bin/server
COPY --from=builder /build/static /app/static
COPY --from=builder /build/templates /app/templates

RUN chown -R appuser:appuser /app

WORKDIR /app

USER appuser

EXPOSE 5000

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:5000/api/health || exit 1

CMD ["server"]
