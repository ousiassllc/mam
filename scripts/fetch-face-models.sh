#!/usr/bin/env bash
# Downloads the face models of the AI Video Judge's Face Identity Similarity (OpenCV Zoo:
# YuNet, MIT, and SFace, Apache-2.0) into a directory and verifies their SHA-256.
# Usage: scripts/fetch-face-models.sh [DIR]   (default: <user config dir>/mam/models)
# The ONNX Runtime library (1.23.x) is a separate download: https://github.com/microsoft/onnxruntime/releases
# See spec/architecture/model-adapters.md §10.1.
set -euo pipefail

case "$(uname -s)" in
  Darwin) default_dir="$HOME/Library/Application Support/mam/models" ;;
  *) default_dir="${XDG_CONFIG_HOME:-$HOME/.config}/mam/models" ;;
esac
dir="${1:-$default_dir}"
base="https://github.com/opencv/opencv_zoo/raw/main/models"

fetch() { # <url-path> <file> <sha256>
  local file="$dir/$2"
  if [[ -f "$file" ]] && echo "$3  $file" | shasum -a 256 -c --status 2>/dev/null; then
    echo "ok      $file"
    return
  fi
  echo "fetch   $file"
  curl -fsSL -o "$file.tmp" "$base/$1"
  echo "$3  $file.tmp" | shasum -a 256 -c --status || { rm -f "$file.tmp"; echo "checksum mismatch: $2" >&2; exit 1; }
  mv "$file.tmp" "$file"
}

mkdir -p "$dir"
fetch face_detection_yunet/face_detection_yunet_2023mar.onnx face_detection_yunet_2023mar.onnx 8f2383e4dd3cfbb4553ea8718107fc0423210dc964f9f4280604804ed2552fa4
fetch face_recognition_sface/face_recognition_sface_2021dec.onnx face_recognition_sface_2021dec.onnx 0ba9fbfa01b5270c96627c4ef784da859931e02f04419c829e83484087c34e79
echo "models are in $dir"
