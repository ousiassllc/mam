#!/usr/bin/env bash
# Regenerates THIRD_PARTY_NOTICES.md: the licenses of the third-party software that goes into the
# Windows release (the portable exe and the installer), taken verbatim from each component's own license files.
#   - Go modules linked into backend/cmd/wails (GOOS=windows, -tags production, the same build as
#     build/windows/Taskfile.yml), plus the Go standard library
#   - npm packages in the production front-end bundle: what static/src/main.ts imports (found with an esbuild
#     metafile, like static/build.mjs bundles it) and tailwindcss (its CSS is part of the generated app.css)
# Usage: scripts/third-party-notices.sh [OUTPUT]      OUTPUT defaults to THIRD_PARTY_NOTICES.md in the repository root
# Needs: go, node, and `npm ci --prefix static` done. Run it again when go.mod or static/package-lock.json changes.
# Fails when a component has no license file, so a dependency can never be left out silently.
set -euo pipefail

cd "$(dirname "$0")/.."
out="${1:-THIRD_PARTY_NOTICES.md}"

die() { echo "$*" >&2; exit 1; }
for tool in go node; do
  command -v "$tool" >/dev/null || die "$tool not found in PATH"
done
[[ -d static/node_modules ]] || die "static/node_modules not found: run npm ci --prefix static"

# License files of a component: LICENSE*, COPYING*, NOTICE*... (PATENTS is a patent grant, not a license).
# LICENSE-LOGO of modernc.org/memory is the license of the project's logo, which is not in the binary.
is_license_file() {
  local name
  name="$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')"
  case "$name" in
    LICENSE-LOGO) return 1 ;;
    LICENSE | LICENSE[-._]* | LICENCE* | COPYING* | NOTICE* | UNLICENSE*) return 0 ;;
    *) return 1 ;;
  esac
}
license_files_in() { # <dir>: the license files directly in it
  local f
  for f in "$1"/*; do
    [[ -f "$f" ]] && is_license_file "$(basename "$f")" && echo "$f"
  done
  return 0
}

records="$(mktemp "${TMPDIR:-/tmp}/mam-notices-XXXXXX")"
trap 'rm -f "$records" "${tmp_out:-}"' EXIT
# record <sort group> <name> <version> <license file> <component dir>: one line per license file of a component;
# the file is shown by its path inside the component (a vendored copy has its own LICENSE in a subdirectory)
record() { printf '%s|%s|%s|%s|%s\n' "$1" "$2" "$3" "$4" "${4#"$5"/}" >>"$records"; }

# 1. Go standard library
goroot="$(go env GOROOT)"
[[ -f "$goroot/LICENSE" ]] || die "no LICENSE in GOROOT: $goroot"
record 1 "Go standard library" "" "$goroot/LICENSE" "$goroot"

# 2. Go modules: every package linked into the Windows exe. A vendored copy inside a module (wails/v3
# internal/webview2 and internal/go-common-file-dialog) has its own LICENSE next to the code, so the
# directories between the package and the module root count as well.
go_packages="$(GOOS=windows CGO_ENABLED=0 go list -deps -tags production \
  -f '{{if .Module}}{{if not .Module.Main}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}|{{.Dir}}{{end}}{{end}}' \
  ./backend/cmd/wails | LC_ALL=C sort -u)"
[[ -n "$go_packages" ]] || die "go list found no module"
while IFS='|' read -r path version root pkgdir; do
  [[ -n "$(license_files_in "$root")" ]] || die "no license file in $path@$version ($root)"
  dir="$pkgdir"
  while :; do
    while IFS= read -r f; do record 2 "$path" "$version" "$f" "$root"; done < <(license_files_in "$dir")
    [[ "$dir" == "$root" || "$dir" == "/" ]] && break
    dir="$(dirname "$dir")"
  done
done <<<"$go_packages"

# 3. npm packages of the production bundle (name|version|directory)
npm_packages="$(cd static && node --input-type=module - <<'JS' | LC_ALL=C sort
import { build } from "esbuild";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
const result = await build({ entryPoints: ["src/main.ts"], bundle: true, format: "esm", target: "es2022", write: false, metafile: true, logLevel: "silent" });
const dirs = new Set();
for (const input of Object.keys(result.metafile.inputs)) {
  const m = input.match(/^(.*node_modules\/(?:@[^/]+\/)?[^/]+)\//);
  if (m) dirs.add(join(process.cwd(), m[1]));
}
const tailwind = dirname(createRequire(join(process.cwd(), "package.json")).resolve("tailwindcss/package.json"));
dirs.add(tailwind);
for (const dir of dirs) {
  const pkg = JSON.parse(readFileSync(join(dir, "package.json"), "utf8"));
  console.log(`${pkg.name}|${pkg.version}|${dir}`);
}
JS
)"
[[ -n "$npm_packages" ]] || die "found no npm package in the front-end bundle"
while IFS='|' read -r name version dir; do
  files="$(license_files_in "$dir")"
  [[ -n "$files" ]] || die "no license file in npm package $name@$version ($dir)"
  while IFS= read -r f; do record 3 "$name" "$version" "$f" "$dir"; done <<<"$files"
done <<<"$npm_packages"

LC_ALL=C sort -t'|' -k1,1 -k2,2 -k4,4 -u "$records" -o "$records"

# A fence longer than any run of ~ in the text, so a license text can never close it early.
fence_for() { # <file>
  awk 'BEGIN { m = 2 } { s = $0; while (match(s, /~+/)) { if (RLENGTH > m) m = RLENGTH; s = substr(s, RSTART + RLENGTH) } }
       END { f = ""; for (i = 0; i <= m; i++) f = f "~"; print f }' "$1"
}
# The text as it is, except for CR (a checkout with core.autocrlf must not change the generated file).
emit_file() { # <file>
  local fence
  fence="$(fence_for "$1")"
  echo "$fence"
  tr -d '\r' <"$1"
  [[ -z "$(tail -c1 "$1")" ]] || echo
  echo "$fence"
}
kind_label() {
  case "$1" in 1) echo "Go 標準ライブラリ" ;; 2) echo "Go モジュール" ;; *) echo "npm パッケージ" ;; esac
}

tmp_out="$(mktemp "${TMPDIR:-/tmp}/mam-notices-out-XXXXXX")"
{
  cat <<'EOF'
# サードパーティのライセンス表記

MAM の Windows 版に含まれるサードパーティのソフトウェアと、そのライセンス文です。MAM 自身のライセンスは [AGPL-3.0](LICENSE) です。インストーラー（`mam-<version>-windows-amd64-setup.exe`）は、`LICENSE` とこのファイルをインストール先に置きます。

このファイルは [`scripts/third-party-notices.sh`](scripts/third-party-notices.sh) が生成します。手で編集せず、`go.mod` や `static/package-lock.json` を変えたらスクリプトを実行し直してください。ライセンス文は各コンポーネントの `LICENSE` などのファイルをそのまま写しています。

載せている範囲は次のとおりです。

- Go の標準ライブラリと、Windows 版 exe（`backend/cmd/wails`、`GOOS=windows`、`-tags production`）にリンクされる Go モジュール
- `static/` の本番バンドルに入る npm パッケージ（`static/src/main.ts` から import されるもの。`static/src/dist/app.css` に Tailwind CSS が含まれるので `tailwindcss` も）
- インストーラーに同梱する Microsoft の WebView2 ブートストラッパー（下の節）

開発用のツール・テスト用のパッケージ（`devDependencies` など）は配布物に入らないので載せていません。ffmpeg は同梱しません。

## 一覧

| 種別 | 名前 | 版 |
|---|---|---|
EOF
  while IFS='|' read -r group name version _; do
    echo "| $(kind_label "$group") | $name | ${version:--} |"
  done < <(cut -d'|' -f1-3 "$records" | LC_ALL=C sort -u -t'|' -k1,1 -k2,2)

  prev=""
  while IFS='|' read -r group name version file label; do
    if [[ "$group|$name|$version" != "$prev" ]]; then
      prev="$group|$name|$version"
      echo
      echo "## $name${version:+ $version}"
      echo
      kind_label "$group"
    fi
    echo
    echo "### $label"
    echo
    emit_file "$file"
  done <"$records"

  cat <<'EOF'

## Microsoft Edge WebView2 Runtime（ブートストラッパー）

インストーラーは、Microsoft の WebView2 ランタイム（Evergreen）のブートストラッパー `MicrosoftEdgeWebview2Setup.exe` を同梱します。WebView2 ランタイムが入っていない PC でだけ実行し、Microsoft のサーバーからランタイムをダウンロードしてインストールします。ポータブル版はブートストラッパーを含まず、PC に入っているランタイムを使います。

ブートストラッパーとランタイムは Microsoft の著作物で、AGPL-3.0 の対象ではありません。条件は Microsoft の WebView2 のライセンス条項に従います（<https://developer.microsoft.com/microsoft-edge/api/eula/webview2>）。配布の方法は <https://learn.microsoft.com/en-us/microsoft-edge/webview2/concepts/distribution> にあります。

## 同梱していないが実行時に取得するもの

Judge の顔同一性（Face Identity Similarity）に使うモデル、YuNet（MIT）と SFace（Apache-2.0）は OpenCV Zoo のもので、MAM には同梱しません。利用者が `scripts/fetch-face-models.sh` で実行時にダウンロードします（ONNX Runtime も別途ダウンロードです）。そのため配布物には入らず、ここにライセンス文は載せません。取得したモデルを再配布するときは、それぞれのライセンスに従ってください。
EOF
} >"$tmp_out"
chmod 644 "$tmp_out" # mktemp creates it 0600
mv "$tmp_out" "$out"
tmp_out=""

echo "wrote $out: $(cut -d'|' -f1-3 "$records" | LC_ALL=C sort -u | wc -l | tr -d ' ') components" >&2
