#!/bin/sh

# Check if TUNNEL_TOKEN is set and not empty
if [ -z "$TUNNEL_TOKEN" ]; then
    echo "=========================================================="
    echo " WARNING: TUNNEL_TOKEN environment variable is not set."
    echo " EXAMVAN will run in OFFLINE mode (LAN only)."
    echo "=========================================================="
    # Keep the container running idle so it doesn't crash-loop
    exec tail -f /dev/null
else
    echo "=========================================================="
    echo " INFO: TUNNEL_TOKEN is set."
    echo " Starting Cloudflare Tunnel (ONLINE mode)..."
    echo "=========================================================="
    # Start the cloudflare tunnel
    exec cloudflared tunnel --no-autoupdate run
fi
