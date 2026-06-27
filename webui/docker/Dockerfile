# Stage 1: Build
FROM golang:1.22-alpine AS builder

WORKDIR /build

# Cache go module downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build statically linked binary
RUN CGO_ENABLED=0 go build -o server ./cmd/server

# Stage 2: Runtime
FROM alpine:3.19

# Install curl for healthcheck
RUN apk add --no-cache curl

# Copy binary from builder
COPY --from=builder /build/server /usr/local/bin/server

# Copy static assets and templates
COPY --from=builder /build/static /app/static
COPY --from=builder /build/templates /app/templates

EXPOSE 5000

CMD ["server"]
