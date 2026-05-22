FROM python:3.10-slim

# Set environment variables
ENV PYTHONDONTWRITEBYTECODE=1
ENV PYTHONUNBUFFERED=1
ENV PORT=5000

# Set working directory
WORKDIR /app

# Install system dependencies (lightweight build utilities if needed)
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    && rm -rf /var/lib/apt/lists/*

# Install python requirements
COPY server/requirements.txt /app/
RUN pip install --no-cache-dir -r requirements.txt

# Copy backend files
COPY server/ /app/

# Ensure directories exist
RUN mkdir -p /app/storage

# Expose server port
EXPOSE 5000

# Run with Gunicorn in production mode
CMD ["gunicorn", "-w", "4", "-b", "0.0.0.0:5000", "app:app"]
