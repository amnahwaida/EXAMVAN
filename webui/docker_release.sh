#!/usr/bin/env bash
# Safe wrapper to run release_apk against the production DB inside the compose
# network (host has no direct DB port). Run from webui/:  ! bash docker_release.sh
set -euo pipefail
cd "$(dirname "$0")"

ERROR() { echo "ERROR: $*" >&2; exit 1; }
[ -f .env ] || ERROR ".env not found in $(pwd)"
[ -f /tmp/release_apk_bin ] || ERROR "/tmp/release_apk_bin missing — build it first (CGO_ENABLED=0 go build -o /tmp/release_apk_bin cmd/release_apk/main.go)"

DBPASS="$(grep -E '^DB_PASSWORD=' .env | head -1 | cut -d= -f2- | tr -d '"')"
DBURL="postgresql://examvan:${DBPASS}@db:5432/examvan"

APK="/home/vannyezha/project/sekolah/EXAMVAN/android/app/build/outputs/apk/student/debug/app-student-debug.apk"
[ -f "$APK" ] || ERROR "APK not found: $APK"

echo ">> Release APK 2.4.1 -> R2 + system_apps id=4 (production)"
echo ">> APK: $APK ($(stat -c%s "$APK") bytes)"

docker run --rm --network webui_internal \
  -e "DATABASE_URL=$DBURL" \
  -v "$APK:/android/app/build/outputs/apk/student/debug/app-student-debug.apk:ro" \
  -v "$(pwd)/.env:/app/.env:ro" \
  -w /app \
  -v "/tmp/release_apk_bin:/release_apk:ro" \
  alpine:3.20 /release_apk