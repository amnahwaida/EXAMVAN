#!/bin/sh
# EXAMVAN entrypoint — fixes permissions for bind-mounted volumes at runtime

# Create data directories (safe even if bind mount doesn't exist yet)
mkdir -p /app/data /app/storage

# Fix permissions for bind-mounted directories:
# Bind mount mewarisi uid/gid dari host, yang mungkin berbeda dengan
# container user. chmod 777 memastikan appuser bisa menulis.
chmod 777 /app/data /app/storage

# Set HOME untuk gunicorn (mencegah "Control server error: /nonexistent")
export HOME=/app

# Execute the CMD (gunicorn)
exec "$@"
