#!/usr/bin/env bash
# Downloads the face models of the AI Video Judge's Face Identity Similarity (OpenCV Zoo:
# YuNet, MIT, and SFace, Apache-2.0) into a directory and verifies their SHA-256.
# Usage: scripts/fetch-face-models.sh [DIR]
#   DIR defaults to $MAM_FACE_MODEL_DIR, then $MAM_DATA_DIR/models, then <user config dir>/mam/models.
# The ONNX Runtime library (1.23.x) is a separate download: https://github.com/microsoft/onnxruntime/releases
# See spec/architecture/model-adapters.md §10.1.
set -euo pipefail

case "$(uname -s)" in
  Darwin) default_dir="$HOME/Library/Application Support/mam/models" ;;
  *) default_dir="${XDG_CONFIG_HOME:-$HOME/.config}/mam/models" ;;
esac
dir="${1:-${MAM_FACE_MODEL_DIR:-${MAM_DATA_DIR:+$MAM_DATA_DIR/models}}}"
dir="${dir:-$default_dir}"
# Pinned to a commit of opencv_zoo: a branch URL could change the files under the checksums.
base="https://github.com/opencv/opencv_zoo/raw/47534e27c9851bb1128ccc0102f1145e27f23f98/models"

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  echo "sha256sum or shasum is required to verify the downloads" >&2
  exit 1
fi

tmp=""
trap '[[ -z "$tmp" ]] || rm -f "$tmp"' EXIT

fetch() { # <url-path> <file> <sha256>
  local file="$dir/$2"
  if [[ -f "$file" && "$(sha256 "$file")" == "$3" ]]; then
    echo "ok      $file"
    return
  fi
  echo "fetch   $file"
  tmp="$file.tmp"
  curl -fsSL --proto '=https' --tlsv1.2 --retry 3 --connect-timeout 10 --max-time 300 -o "$tmp" "$base/$1"
  if [[ "$(sha256 "$tmp")" != "$3" ]]; then
    echo "checksum mismatch: $2" >&2
    exit 1
  fi
  mv "$tmp" "$file"
  tmp=""
}

mkdir -p "$dir"
fetch face_detection_yunet/face_detection_yunet_2023mar.onnx face_detection_yunet_2023mar.onnx 8f2383e4dd3cfbb4553ea8718107fc0423210dc964f9f4280604804ed2552fa4
fetch face_recognition_sface/face_recognition_sface_2021dec.onnx face_recognition_sface_2021dec.onnx 0ba9fbfa01b5270c96627c4ef784da859931e02f04419c829e83484087c34e79
echo "models are in $dir"
