# Single-stage build — Tailwind CSS sudah pre-built (output.css di-track git)
FROM python:3.10-slim

# Set environment variables
ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV PORT=5000

# Set working directory
WORKDIR /app

# Install system dependencies (curl for healthcheck)
RUN apt-get update && apt-get install -y --no-install-recommends curl && \
    apt-get clean && rm -rf /var/lib/apt/lists/*

# Install python requirements
COPY server/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

# Copy backend files (termasuk output.css yang sudah pre-built)
COPY server/ /app/

# Entrypoint: fix bind-mount permissions at runtime, then exec CMD
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]

# Expose server port
EXPOSE 5000

# Run with Gunicorn: 1 worker + 4 threads for safe SQLite concurrent access
CMD ["gunicorn", "-w", "1", "--threads", "4", "-b", "0.0.0.0:5000", "app:app"]
