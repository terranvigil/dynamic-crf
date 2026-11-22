#!/usr/bin/env bash
#
# build-vmaf.sh - Build Netflix's vmaf CLI from source with the VMAF v1 models.
#
# dynamic-crf scores with VMAF v1 (vmaf_v1.0.16), which needs libvmaf 3.2.1 or
# newer. Homebrew's libvmaf 3.2.0 ships the v1 models but can't load them.
#
# Usage:
#   ./scripts/build-vmaf.sh
#
# Environment:
#   VMAF_REF     git tag or commit to build (default v3.2.1)
#   PREFIX       install prefix (default ~/.local/opt/vmaf-v1)
#   MODEL_DIR    where the v1 model JSON files go (default ~/.local/share/vmaf/model)
#
# Output:
#   $PREFIX/bin/vmaf
#   $MODEL_DIR/vmaf_v1.0.16/*.json and $MODEL_DIR/vmaf_v1.0.16_hfr/*.json
#
# Then point dynamic-crf at it:
#   export VMAF_BIN=$PREFIX/bin/vmaf
#   export VMAF_MODEL_DIR=$MODEL_DIR   # optional, the models are also built in
#
# Requires git, meson, ninja and a C compiler (brew install meson ninja nasm).

set -euo pipefail

VMAF_REF="${VMAF_REF:-v3.2.1}"
PREFIX="${PREFIX:-$HOME/.local/opt/vmaf-v1}"
MODEL_DIR="${MODEL_DIR:-$HOME/.local/share/vmaf/model}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BUILD_DIR="$PROJECT_ROOT/.vmaf-build"

for tool in git meson ninja; do
    if ! command -v "$tool" >/dev/null; then
        echo "ERROR: $tool not found (brew install meson ninja nasm)"
        exit 1
    fi
done

echo "==> Fetching libvmaf $VMAF_REF..."
mkdir -p "$BUILD_DIR"
if [[ ! -d "$BUILD_DIR/vmaf" ]]; then
    git clone -q https://github.com/Netflix/vmaf.git "$BUILD_DIR/vmaf"
fi
git -C "$BUILD_DIR/vmaf" fetch -q --tags origin
git -C "$BUILD_DIR/vmaf" checkout -q "$VMAF_REF"

echo "==> Building..."
cd "$BUILD_DIR/vmaf/libvmaf"
rm -rf build
meson setup build \
    --prefix="$PREFIX" \
    -Dbuildtype=release \
    -Denable_float=true \
    -Dbuilt_in_models=true
ninja -C build
ninja -C build install

echo "==> Installing v1 models to $MODEL_DIR..."
mkdir -p "$MODEL_DIR"
for d in "$BUILD_DIR"/vmaf/model/vmaf_v1.0.16*; do
    [[ -d "$d" ]] && cp -R "$d" "$MODEL_DIR/"
done

echo ""
echo "==> Verifying..."
"$PREFIX/bin/vmaf" --version
ls "$MODEL_DIR"

echo ""
echo "==> Done. Point dynamic-crf at it:"
echo "    export VMAF_BIN=$PREFIX/bin/vmaf"
