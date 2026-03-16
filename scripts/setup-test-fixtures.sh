#!/bin/bash
#
# Creates test fixture media files for integration tests.
#
# Sources (Creative Commons):
#   - Short clip: Sintel trailer (~52s) from Blender Foundation
#   - Long clip: Big Buck Bunny 1080p (~10min) from https://media.xiph.org/video/derf/
#
# Usage:
#   ./scripts/setup-test-fixtures.sh
#
# The script checks these locations for the short source:
#   1. ../veo/assets/blender/sintel_trailer_2k_1080p24.y4m
#   2. ./sintel_trailer_2k_1080p24.y4m
#
# The long source is downloaded automatically if not already present.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
FIXTURE_DIR="${PROJECT_DIR}/fixtures/media"

mkdir -p "$FIXTURE_DIR"

# --- Short clip: Sintel trailer (~52s) ---

SINTEL_SOURCE=""
if [ -f "${PROJECT_DIR}/../veo/assets/blender/sintel_trailer_2k_1080p24.y4m" ]; then
    SINTEL_SOURCE="${PROJECT_DIR}/../veo/assets/blender/sintel_trailer_2k_1080p24.y4m"
elif [ -f "./sintel_trailer_2k_1080p24.y4m" ]; then
    SINTEL_SOURCE="./sintel_trailer_2k_1080p24.y4m"
fi

if [ -n "$SINTEL_SOURCE" ]; then
    LINK="${FIXTURE_DIR}/sintel_trailer_1080p.y4m"
    if [ ! -e "$LINK" ]; then
        echo "Symlinking sintel_trailer_1080p.y4m..."
        ABSOLUTE_SOURCE="$(cd "$(dirname "$SINTEL_SOURCE")" && pwd)/$(basename "$SINTEL_SOURCE")"
        ln -s "$ABSOLUTE_SOURCE" "$LINK"
    else
        echo "sintel_trailer_1080p.y4m already exists, skipping."
    fi
else
    echo "Warning: Sintel trailer y4m not found, skipping short fixture."
    echo "  Download from: https://media.xiph.org/video/derf/"
    echo "  Or place at: ../veo/assets/blender/sintel_trailer_2k_1080p24.y4m"
fi

# --- Long clip: Big Buck Bunny 1080p (~10min) ---

BBB_XZ="${FIXTURE_DIR}/big_buck_bunny_1080p24.y4m.xz"
BBB_Y4M="${FIXTURE_DIR}/big_buck_bunny_1080p24.y4m"
BBB_URL="https://media.xiph.org/video/derf/y4m/big_buck_bunny_1080p24.y4m.xz"

if [ -f "$BBB_Y4M" ]; then
    echo "big_buck_bunny_1080p24.y4m already exists, skipping download."
else
    if [ ! -f "$BBB_XZ" ]; then
        echo "Downloading big_buck_bunny_1080p24.y4m.xz (~10GB, this will take a while)..."
        curl -L -o "$BBB_XZ" "$BBB_URL"
    fi
    echo "Decompressing big_buck_bunny_1080p24.y4m.xz (~44GB uncompressed)..."
    xz -d "$BBB_XZ"
fi

# Create a 2-minute mp4 reference for integration tests that exercise scene sampling.
# The sampler needs a muxed container (mp4), not raw y4m.
BBB_MP4="${FIXTURE_DIR}/big_buck_bunny_120s_1080p.mp4"
if [ -f "$BBB_MP4" ]; then
    echo "big_buck_bunny_120s_1080p.mp4 already exists, skipping."
else
    echo "Creating big_buck_bunny_120s_1080p.mp4 (2min near-lossless mp4)..."
    ffmpeg -hide_banner -y \
        -i "$BBB_Y4M" \
        -t 120 \
        -c:v libx264 -crf 1 -preset fast \
        -an \
        "$BBB_MP4"
fi

echo ""
echo "Fixtures:"
ls -lh "${FIXTURE_DIR}/"
