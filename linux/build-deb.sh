#!/bin/bash
# Build EXAMVAN .deb package
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BUILD_DIR="$SCRIPT_DIR/pkg-build"
OUTPUT_DIR="$SCRIPT_DIR/dist"

mkdir -p "$OUTPUT_DIR"

# Check dpkg-deb availability
if ! command -v dpkg-deb &>/dev/null; then
    echo "Error: dpkg-deb not found. Install dpkg-dev."
    exit 1
fi

# Extract version from control file
VERSION=$(grep "^Version:" "$BUILD_DIR/DEBIAN/control" | awk '{print $2}')

echo "📦 Building examvan-linux_${VERSION}.deb ..."
dpkg-deb --build "$BUILD_DIR" "$OUTPUT_DIR/examvan-linux_${VERSION}.deb"

echo "✅ Package: $OUTPUT_DIR/examvan-linux_${VERSION}.deb"
echo "   Install: sudo dpkg -i $OUTPUT_DIR/examvan-linux_${VERSION}.deb"
echo "   Run: examvan"
