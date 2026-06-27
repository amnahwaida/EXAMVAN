# Stage 1: Build
# Docker context: EXAMVAN/ (parent of webui/)
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy ALL source first — needed for go mod tidy to resolve imports
COPY webui/ .

# Generate go.sum and download dependencies
RUN go mod tidy && go mod download

# Build statically linked binary
RUN CGO_ENABLED=0 go build -o server ./cmd/server

# Stage 2: Runtime
FROM alpine:3.19

RUN apk add --no-cache curl

COPY --from=builder /build/server /usr/local/bin/server
COPY --from=builder /build/static /app/static
COPY --from=builder /build/templates /app/templates

WORKDIR /app

EXPOSE 5000

CMD ["server"]
