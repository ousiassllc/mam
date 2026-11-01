#!/usr/bin/env bash
# Tests for scripts/third-party-notices.sh with stubbed `go` and `node` (the go list output and the npm package list are files).
# Run: bash scripts/third-party-notices_test.sh
set -euo pipefail

script="$(cd "$(dirname "$0")" && pwd)/third-party-notices.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# A throw-away repository: scripts/ holds a copy of the script (it works from its own parent directory).
mkdir -p "$work/repo/scripts" "$work/repo/static/node_modules" "$work/bin" "$work/goroot" "$work/mods" "$work/npm"
cp "$script" "$(dirname "$script")/fetch-onnxruntime.sh" "$work/repo/scripts/"
cp "$(dirname "$script")/third-party-notices-musetalk.md" "$work/repo/scripts/"
cat >"$work/bin/go" <<'STUB'
#!/usr/bin/env bash
case "$1" in
  env) echo "$FAKE_GOROOT" ;;
  list) cat "$FAKE_GO_LIST" ;;
  *) exit 1 ;;
esac
STUB
cat >"$work/bin/node" <<'STUB'
#!/usr/bin/env bash
cat >/dev/null # the script text on stdin
cat "$FAKE_NPM_LIST"
STUB
chmod +x "$work/bin/go" "$work/bin/node"
export PATH="$work/bin:$PATH" FAKE_GOROOT="$work/goroot" FAKE_GO_LIST="$work/go-list" FAKE_NPM_LIST="$work/npm-list"

# Components. a: plain LICENSE (no final newline, CRLF). b: LICENSE, a vendored copy with its own LICENSE, files that are not licenses.
# c: a license text that contains a ~~~~ line.
printf 'Stdlib license text\n' >"$work/goroot/LICENSE"
mkdir -p "$work/mods/a" "$work/mods/b/internal/vendored/pkg" "$work/mods/c"
printf 'License of a\r\nsecond line' >"$work/mods/a/LICENSE"
printf 'License of b\n' >"$work/mods/b/LICENSE"
printf 'License of b/vendored\n' >"$work/mods/b/internal/vendored/LICENSE"
printf 'logo license\n' >"$work/mods/b/LICENSE-LOGO"
printf 'patent grant\n' >"$work/mods/b/PATENTS"
printf 'License of c\n~~~~\nafter\n' >"$work/mods/c/LICENSE.md"
mkdir -p "$work/npm/node_modules/lib"
# The copied Lucide icons: the release comes from icon.templ, the license text from atoms/icons/LICENSE (with a Feather MIT part).
mkdir -p "$work/repo/backend/internal/web/atoms/icons"
printf '// Icons: paths copied from Lucide v9.8.7 (https://lucide.dev, ISC).\npackage atoms\n' >"$work/repo/backend/internal/web/atoms/icon.templ"
printf 'ISC License\n\nLucide text\n\nThe MIT License (MIT) (for portions derived from Feather)\n' >"$work/repo/backend/internal/web/atoms/icons/LICENSE"
# The bundled ONNX Runtime DLL: the version comes from fetch-onnxruntime.sh, the texts from build/windows/onnxruntime/.
mkdir -p "$work/repo/build/windows/onnxruntime"
printf 'MIT License\n\nONNX Runtime license text\n' >"$work/repo/build/windows/onnxruntime/LICENSE"
printf 'THIRD PARTY SOFTWARE NOTICES\r\nline two\r\n' >"$work/repo/build/windows/onnxruntime/ThirdPartyNotices.txt"
printf 'License of lib\n' >"$work/npm/node_modules/lib/LICENSE"
write_lists() {
  {
    echo "example.com/a|v1.0.0|$work/mods/a|$work/mods/a"
    echo "example.com/b|v2.0.0|$work/mods/b|$work/mods/b/internal/vendored/pkg"
    echo "example.com/b|v2.0.0|$work/mods/b|$work/mods/b"
    echo "example.com/c|v3.0.0|$work/mods/c|$work/mods/c"
  } >"$FAKE_GO_LIST"
  echo "lib|1.2.3|$work/npm/node_modules/lib" >"$FAKE_NPM_LIST"
}
write_lists

fails=0
check() { # <name> <condition result: 0 = pass>
  if [[ "$2" -eq 0 ]]; then echo "ok   $1"; else echo "FAIL $1"; fails=$((fails + 1)); fi
}
has() { grep -qxF -- "$1" "$out"; }
hasnt() { ! grep -qF -- "$1" "$out"; }

out="$work/notices.md"
(cd "$work/repo" && bash scripts/third-party-notices.sh "$out" 2>/dev/null)
check "success writes the output" $?
check "lists the Go standard library" "$(has '## Go standard library' && echo 0 || echo 1)"
check "lists a Go module with its version" "$(has '## example.com/a v1.0.0' && echo 0 || echo 1)"
check "lists an npm package" "$(has '## lib 1.2.3' && echo 0 || echo 1)"
check "lists the copied Lucide icons with the release icon.templ names" "$(has '## Lucide（アイコン） 9.8.7' && grep -qxF '| 同梱した素材 | Lucide（アイコン） | 9.8.7 |' "$out" && echo 0 || echo 1)"
check "the Lucide license is shown whole, the Feather MIT part included" "$(
  diff <(awk '/^## Lucide/ { b = 1 } /^### LICENSE$/ && b { f = 1; next } f && /^~~~$/ { if (++n == 2) exit; next } f && n == 1 { print }' "$out") "$work/repo/backend/internal/web/atoms/icons/LICENSE" >/dev/null && grep -qF 'Feather' "$out" && echo 0 || echo 1
)"
check "lists the bundled ONNX Runtime with the pinned version, its license and Microsoft's notices whole" "$(
  pinned="$(bash "$work/repo/scripts/fetch-onnxruntime.sh" --version)"
  has "## ONNX Runtime（onnxruntime.dll） $pinned" && grep -qxF "| 同梱した素材 | ONNX Runtime（onnxruntime.dll） | $pinned |" "$out" && has '### LICENSE' && has 'ONNX Runtime license text' &&
    has '### ThirdPartyNotices.txt' && has 'THIRD PARTY SOFTWARE NOTICES' && has 'line two' && echo 0 || echo 1
)"
check "lists the table row of every component" "$(
  [[ "$(grep -c '^| \(Go\|npm\)' "$out")" -eq 5 ]] && echo 0 || echo 1
)"
check "vendored license is shown by its path in the module" "$(has '### internal/vendored/LICENSE' && echo 0 || echo 1)"
check "module root license is kept next to the vendored one" "$(has 'License of b' && has 'License of b/vendored' && echo 0 || echo 1)"
check "a module used by two packages appears once" "$([[ "$(grep -c '^## example.com/b ' "$out")" -eq 1 ]] && echo 0 || echo 1)"
check "LICENSE-LOGO is left out" "$(hasnt 'logo license' && echo 0 || echo 1)"
check "PATENTS is left out" "$(hasnt 'patent grant' && echo 0 || echo 1)"
check "CR is removed and a missing final newline is added" "$(
  # the closing fence is on its own line right after the second line
  grep -A1 -xF 'second line' "$out" | grep -qx '~~~' && ! grep -q $'\r' "$out" && echo 0 || echo 1
)"
check "a longer fence is used when the text has ~~~~" "$(
  has '~~~~~' && [[ "$(grep -c -x '~~~~~' "$out")" -eq 2 ]] && echo 0 || echo 1
)"
check "texts are verbatim (b)" "$(
  diff <(awk '/^## example.com\/b / { b = 1 } /^### LICENSE$/ && b { f = 1; next } f && /^~~~$/ { if (++n == 2) exit; next } f && n == 1 { print }' "$out") "$work/mods/b/LICENSE" >/dev/null && echo 0 || echo 1
)"
check "ends with the WebView2 and face model sections" "$(
  has '## Microsoft Edge WebView2 Runtime（ブートストラッパー）' && has '## 同梱していないが実行時に取得するもの' && echo 0 || echo 1
)"
check "the ComfyUI section: a separate GPL-3.0 program, the source, no bytes, the unverified Python/PyTorch inventory" "$(
  has '## ComfyUI（利用者が取得する別プログラム）' && grep -qF 'https://github.com/Comfy-Org/ComfyUI/tree/v0.42.1' "$out" && grep -qF 'ComfyUI のバイトを一切含めません' "$out" && grep -qF '未確認' "$out" && echo 0 || echo 1
)"
check "the GPT-SoVITS section: a separate MIT program, the source, no bytes, Windows + NVIDIA only, the unverified package inventory" "$(
  has '## GPT-SoVITS（利用者が取得する別プログラム）' && grep -qF 'https://github.com/RVC-Boss/GPT-SoVITS/tree/20250606v2pro' "$out" && grep -qF 'GPT-SoVITS のバイトを一切含めません' "$out" && grep -qF 'NVIDIA' "$out" && grep -qF '未確認' "$out" && echo 0 || echo 1
)"
check "the MuseTalk section: a separate MIT program, the pinned commit, no bytes, the sources of the environment, the unverified licenses of the wheels" "$(
  has '## MuseTalk 1.5（利用者が取得する別プログラム）' && grep -qF 'https://github.com/TMElyralab/MuseTalk/tree/0a89dec45a0192b824e3cf4daf96c239440c5ed8' "$out" && grep -qF 'MuseTalk・Python・下の Python パッケージ・PyTorch のバイトを一切含めません' "$out" && grep -qF 'https://download.pytorch.org/whl/cu118/' "$out" && grep -qF '| torch | 2.0.1+cu118 |' "$out" && grep -qF 'NVIDIA の CUDA ランタイムと cuDNN の DLL' "$out" && grep -qF '未確認' "$out" && echo 0 || echo 1
)"
check "the weights section: not bundled, pinned, the unclear commercial-use ones are named, for GPT-SoVITS and MuseTalk" "$(
  has '## GPT-SoVITS と MuseTalk の重み・依存ファイル（利用者が取得するもの）' && grep -qF '同梱せず再配布しません' "$out" && grep -qF '### MuseTalk 1.5 の重みと依存ファイル' "$out" && grep -qF 'はっきりしない' "$out" && grep -qF 'face-parse-bisent' "$out" && echo 0 || echo 1
)"
check "the Codex section: a separate Apache-2.0 program, the source, no bytes, the unverified dependency inventory" "$(
  has '## Codex app-server（利用者が取得する別プログラム）' && grep -qF 'https://github.com/openai/codex/tree/rust-v0.164.0' "$out" && grep -qF 'Codex のバイトを一切含めません' "$out" && grep -qF 'Apache-2.0' "$out" && echo 0 || echo 1
)"
check "the ffmpeg section: a separate GPL v3 program, the source, no bytes, only on a click, the unverified dependency inventory" "$(
  has '## ffmpeg と ffprobe（利用者が取得する別プログラム）' && grep -qF 'https://github.com/FFmpeg/FFmpeg/commit/946fcce07b' "$out" && grep -qF 'https://ffmpeg.org/releases/ffmpeg-9.0.2.tar.xz' "$out" && grep -qF 'ffmpeg のバイトを一切含めません' "$out" && grep -qF 'GPL v3 以降' "$out" && grep -qF 'ボタンを押したときだけ' "$out" && echo 0 || echo 1
)"
check "the output is deterministic" "$(
  (cd "$work/repo" && bash scripts/third-party-notices.sh "$work/second.md" 2>/dev/null) && cmp -s "$out" "$work/second.md" && echo 0 || echo 1
)"
check "the default output is THIRD_PARTY_NOTICES.md in the repository root" "$(
  (cd "$work" && bash repo/scripts/third-party-notices.sh 2>/dev/null) && cmp -s "$out" "$work/repo/THIRD_PARTY_NOTICES.md" && echo 0 || echo 1
)"

# A component without a license file fails, and the output is not written.
rm "$work/mods/a/LICENSE"
rm -f "$work/third.md"
err="$(cd "$work/repo" && bash scripts/third-party-notices.sh "$work/third.md" 2>&1)" && rc=0 || rc=$?
check "a Go module without a license file fails" "$([[ $rc -ne 0 && "$err" == *"no license file in example.com/a@v1.0.0"* && ! -e "$work/third.md" ]] && echo 0 || echo 1)"
printf 'License of a\n' >"$work/mods/a/LICENSE"
rm "$work/npm/node_modules/lib/LICENSE"
err="$(cd "$work/repo" && bash scripts/third-party-notices.sh "$work/third.md" 2>&1)" && rc=0 || rc=$?
check "an npm package without a license file fails" "$([[ $rc -ne 0 && "$err" == *"no license file in npm package lib@1.2.3"* ]] && echo 0 || echo 1)"

# The Lucide icons without their license text, or without a named release, fail (the npm license is back).
printf 'License of lib\n' >"$work/npm/node_modules/lib/LICENSE"
mv "$work/repo/backend/internal/web/atoms/icons/LICENSE" "$work/lucide-license"
err="$(cd "$work/repo" && bash scripts/third-party-notices.sh "$work/third.md" 2>&1)" && rc=0 || rc=$?
check "the Lucide icons without a license file fail" "$([[ $rc -ne 0 && "$err" == *"no license file in backend/internal/web/atoms/icons"* ]] && echo 0 || echo 1)"
mv "$work/lucide-license" "$work/repo/backend/internal/web/atoms/icons/LICENSE"
printf 'package atoms\n' >"$work/repo/backend/internal/web/atoms/icon.templ"
err="$(cd "$work/repo" && bash scripts/third-party-notices.sh "$work/third.md" 2>&1)" && rc=0 || rc=$?
check "icons without a named Lucide release fail" "$([[ $rc -ne 0 && "$err" == *"does not name the Lucide release"* ]] && echo 0 || echo 1)"
printf '// Lucide v9.8.7\n' >"$work/repo/backend/internal/web/atoms/icon.templ"

# The ONNX Runtime license texts missing fail, one by one (the texts are back afterwards).
for f in LICENSE ThirdPartyNotices.txt; do
  mv "$work/repo/build/windows/onnxruntime/$f" "$work/ort-$f"
  err="$(cd "$work/repo" && bash scripts/third-party-notices.sh "$work/third.md" 2>&1)" && rc=0 || rc=$?
  check "the ONNX Runtime without $f fails" "$([[ $rc -ne 0 && "$err" == *"no $f in build/windows/onnxruntime"* ]] && echo 0 || echo 1)"
  mv "$work/ort-$f" "$work/repo/build/windows/onnxruntime/$f"
done

# No Go module / npm package at all is an error, not an empty file.
: >"$FAKE_GO_LIST"
err="$(cd "$work/repo" && bash scripts/third-party-notices.sh "$work/third.md" 2>&1)" && rc=0 || rc=$?
check "no Go module found fails" "$([[ $rc -ne 0 && "$err" == *"go list found no module"* ]] && echo 0 || echo 1)"
write_lists
: >"$FAKE_NPM_LIST"
err="$(cd "$work/repo" && bash scripts/third-party-notices.sh "$work/third.md" 2>&1)" && rc=0 || rc=$?
check "no npm package found fails" "$([[ $rc -ne 0 && "$err" == *"found no npm package"* ]] && echo 0 || echo 1)"

echo
if ((fails > 0)); then echo "$fails failed" >&2; exit 1; fi
echo "all passed"
