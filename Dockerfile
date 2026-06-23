# Stage 1: Build Tailwind CSS
FROM node:20-alpine AS css-builder
WORKDIR /css
COPY server/static/css/tailwind/ /css/
COPY package.json /css/package.json
RUN npm install && npm run tailwind:build

# Stage 2: Python app
FROM python:3.10-slim

# Create non-root user
RUN addgroup --system --gid 1001 appgroup && \
    adduser --system --uid 1001 --gid 1001 --no-create-home appuser

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

# Copy backend files
COPY server/ /app/

# Copy pre-built Tailwind CSS from builder stage
COPY --from=css-builder /css/output.css /app/static/css/tailwind/output.css

# Ensure directories exist and set ownership for runtime
RUN mkdir -p /app/data /app/storage && chown -R appuser:appgroup /app

# Switch to non-root user
USER appuser

# Expose server port
EXPOSE 5000

# Run with Gunicorn: 1 worker + 4 threads for safe SQLite concurrent access
CMD ["gunicorn", "-w", "1", "--threads", "4", "-b", "0.0.0.0:5000", "app:app"]
