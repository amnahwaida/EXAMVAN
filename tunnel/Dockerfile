FROM cloudflare/cloudflared:latest AS bin

FROM alpine:latest

# Install ca-certificates (required for HTTPS connections to Cloudflare)
RUN apk add --no-cache ca-certificates

# Copy the cloudflared binary from the official image
COPY --from=bin /usr/local/bin/cloudflared /usr/local/bin/cloudflared

# Copy and set up the entrypoint script
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
