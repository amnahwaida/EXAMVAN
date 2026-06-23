FROM cloudflare/cloudflared:2025.10.0 AS bin

FROM alpine:3.21

# Install ca-certificates (required for HTTPS connections to Cloudflare)
RUN apk add --no-cache ca-certificates

# Copy the cloudflared binary from the official image
COPY --from=bin /usr/local/bin/cloudflared /usr/local/bin/cloudflared

# Copy and set up the entrypoint script
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

# Switch to non-root user
USER nobody

ENTRYPOINT ["/entrypoint.sh"]
