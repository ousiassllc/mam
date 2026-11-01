#!/usr/bin/env bash
# Regenerates THIRD_PARTY_NOTICES.md: the licenses of the third-party software that goes into the
# Windows release (the portable exe and the installer), taken verbatim from each component's own license files.
#   - Go modules linked into backend/cmd/wails (GOOS=windows, -tags production, the same build as
#     build/windows/Taskfile.yml), plus the Go standard library
#   - npm packages in the production front-end bundle: what static/src/main.ts imports (found with an esbuild
#     metafile, like static/build.mjs bundles it) and tailwindcss (its CSS is part of the generated app.css)
#   - material copied into the source tree: the Lucide icons of backend/internal/web/atoms/icon.templ, with the
#     license text of the release they were copied from (atoms/icons/LICENSE; the release is named in icon.templ)
#   - the ONNX Runtime DLL the installer bundles (Face Identity Similarity): the LICENSE and ThirdPartyNotices.txt of the
#     pinned release, committed in build/windows/onnxruntime/ (scripts/fetch-onnxruntime.sh pins the version and refuses
#     a zip whose two files differ from the committed ones)
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

# 4. Material copied into the source tree (the Go and npm lists above do not see it): the Lucide icons. The release
# is the one icon.templ names ("Lucide v1.61.0"); the license file is the one of that release, whole.
icons_dir="backend/internal/web/atoms/icons"
lucide_version="$(sed -n 's/.*Lucide v\([0-9][0-9.]*\).*/\1/p' backend/internal/web/atoms/icon.templ 2>/dev/null | head -n1 || true)"
[[ -n "$lucide_version" ]] || die "backend/internal/web/atoms/icon.templ does not name the Lucide release (\"Lucide v<version>\")"
[[ -n "$(license_files_in "$icons_dir")" ]] || die "no license file in $icons_dir (the Lucide icons)"
while IFS= read -r f; do record 4 "Lucide（アイコン）" "$lucide_version" "$f" "$icons_dir"; done < <(license_files_in "$icons_dir")

# 5. The ONNX Runtime DLL of the installer (scripts/package-windows.sh -> scripts/fetch-onnxruntime.sh). The license files are the
# ones of the pinned release: LICENSE (MIT) and ThirdPartyNotices.txt (Microsoft's notices for what ONNX Runtime contains).
ort_dir="build/windows/onnxruntime"
ort_version="$(bash scripts/fetch-onnxruntime.sh --version)"
[[ -n "$ort_version" ]] || die "scripts/fetch-onnxruntime.sh does not print the ONNX Runtime version"
for f in LICENSE ThirdPartyNotices.txt; do
  [[ -s "$ort_dir/$f" ]] || die "no $f in $ort_dir (the ONNX Runtime license texts)"
  record 4 "ONNX Runtime（onnxruntime.dll）" "$ort_version" "$ort_dir/$f" "$ort_dir"
done

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
  case "$1" in 1) echo "Go 標準ライブラリ" ;; 2) echo "Go モジュール" ;; 4) echo "同梱した素材" ;; *) echo "npm パッケージ" ;; esac
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
- ソースにコピーして同梱した素材: アイコンの Lucide（ISC。Feather 由来のアイコンの MIT を含む。取り込んだ版の `LICENSE` の全文）
- インストーラーが `mam.exe` の隣に置く ONNX Runtime の DLL（`onnxruntime.dll`・`onnxruntime_providers_shared.dll`。MIT、Microsoft）: 顔の同一性の数値評価に使います。取り込んだ版の `LICENSE` と、Microsoft が付ける第三者の通知 `ThirdPartyNotices.txt` の全文を載せています（`build/windows/onnxruntime/` にある写しで、`scripts/fetch-onnxruntime.sh` が固定した版のリリースと同じ内容であることを確かめます）。ONNX Runtime を Go から呼ぶバインディング（`github.com/shota3506/onnxruntime-purego`、MIT）と、その下の `github.com/ebitengine/purego`（Apache-2.0）は、上の Go モジュールに入っています。バインディングは、Windows 対応の修正（DLL の読み込みとモデルのパスの UTF-16 化）の 1 コミットだけを足した `github.com/aubs3993/onnxruntime-purego` のコミットに `go.mod` の `replace` で差し替えています（上の「版」は元のモジュールのもので、`LICENSE` は同じ MIT です）
- インストーラーに同梱する Microsoft の WebView2 ブートストラッパー（下の節）

開発用のツール・テスト用のパッケージ（`devDependencies` など）は配布物に入らないので載せていません。ffmpeg・ComfyUI・Codex は同梱しません（利用者が取得する別プログラムとして、下の節に載せています）。

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

Judge の顔同一性（Face Identity Similarity）に使うモデル、YuNet（MIT）と SFace（Apache-2.0）は OpenCV Zoo のもので、MAM には同梱しません。利用者が `scripts/fetch-face-models.sh` で実行時にダウンロードします。そのため配布物には入らず、ここにライセンス文は載せません。取得したモデルを再配布するときは、それぞれのライセンスに従ってください。ONNX Runtime の DLL（Windows 版）は同梱しており、上の節に載せています（macOS 版は同梱せず、利用者が用意した `libonnxruntime.dylib` を使います）。

## ComfyUI（利用者が取得する別プログラム）

MAM のモデルマネージャーは、利用者の操作で ComfyUI のポータブル版（v0.42.1）を公式のリリースから取得し、MAM の実行ファイルと同じ場所の `comfyui` ディレクトリに展開して、別のプロセスとして起動します。ComfyUI は GPL-3.0 のプログラムで、**MAM のインストーラー・ポータブル版・リリースには ComfyUI のバイトを一切含めません**。MAM は ComfyUI のコードをリンクせず、HTTP（`127.0.0.1`）で通信するだけです。

- ComfyUI のライセンス（GPL-3.0）と対応するソースコード: <https://github.com/Comfy-Org/ComfyUI/tree/v0.42.1>（取得するポータブル版は <https://github.com/Comfy-Org/ComfyUI/releases/tag/v0.42.1> のアセットで、SHA-256 を MAM のカタログに固定して検証します）。
- **ノード画面（ComfyUI のフロントエンド、GPL-3.0）**: MAM の「ノード」画面（`/nodes`）は、取得した ComfyUI が配信する画面（`Comfy-Org/ComfyUI_frontend`、GPL-3.0。ポータブル版に pip パッケージ `comfyui-frontend-package` として入っている）を iframe で表示するだけです。**MAM はそのコードを一切含めず、改変も再配布もしません**（ブラウザは ComfyUI のサーバーから直接読み込みます）。ソース: <https://github.com/Comfy-Org/ComfyUI_frontend>。
- ポータブル版に入っている Python・PyTorch・その他のパッケージのライセンスは、**一覧を調べていません（未確認）**。取得したものを再配布するときは、取得した `comfyui\engine` の中の各ライセンスを確認してください。
- 利用者が導入できるモデルの重み（Stable Diffusion XL・FLUX.1・Qwen-Image・Wan 2.2・LTX-Video・Stable Diffusion 3.5・HunyuanVideo など）は、Hugging Face から利用者が取得します。MAM は再配布せず、各モデルのライセンス（商用利用の可否・地域の制限を含む）は導入の画面に表示します。条件の一部（FLUX.1 [dev] の非商用、HunyuanVideo の EU・英国・韓国での不可など）は、導入の前に同意を求めます。

## ffmpeg と ffprobe（利用者が取得する別プログラム）

MAM の設定画面の「ffmpeg」と初回ガイドには、利用者がボタンを押したときだけ ffmpeg と ffprobe（バージョン 9.0.2。Windows 版（x64）と macOS 版（Apple Silicon）のみ）を配布元から取得し、データの置き場（Windows 版は MAM の実行ファイルと同じ場所の `ffmpeg` ディレクトリ、macOS 版はデータディレクトリの中）に展開するボタンがあります。ffmpeg は **GPL v3 以降**のビルドで、**MAM のインストーラー・ポータブル版・リリースには ffmpeg のバイトを一切含めません**。MAM は ffmpeg のコードをリンクせず、別のプロセスとして起動するだけです。ボタンを押さない限り、通信も取得もしません（自動の更新確認はありません）。

- ffmpeg のライセンス（GPL v3 以降）と対応するソースコード: Windows 版は <https://github.com/FFmpeg/FFmpeg/commit/946fcce07b>（取得するのは gyan.dev のビルド <https://www.gyan.dev/ffmpeg/builds/> のうち、<https://github.com/GyanD/codexffmpeg/releases/tag/9.0.2> のアセット `ffmpeg-9.0.2-essentials_build.zip`）、macOS 版は <https://ffmpeg.org/releases/ffmpeg-9.0.2.tar.xz>（取得するのは Martin Riedl のビルドサーバー <https://ffmpeg.martin-riedl.de/> の release 9.0.2 の `ffmpeg.zip` と `ffprobe.zip`）。アーカイブと展開した 2 つの実行ファイルの SHA-256 とサイズを MAM のカタログに固定して検証し、一致しなければ使いません。
- 取得したビルドは `libx264`・`libx265`・`libvpx`・`libopus` ほか多数の GPL・LGPL・BSD などのライブラリを静的に含みます。**その一覧とライセンス文は調べていません（未確認）**。取得したものを再配布するときは、配布元の README・LICENSE（アーカイブの中にあります）に従い、対応するソースを提供してください。

## Codex app-server（利用者が取得する別プログラム）

MAM の「ChatGPT でログイン」の Codex 経由の方式（Business / Enterprise / Edu のワークスペースのアカウントでも使える、テキスト生成だけの方式）は、利用者の操作で OpenAI の Codex の app-server（バージョン 0.164.0。Windows 版と macOS 版（Apple Silicon）のみ）を公式のリリースから取得し、`codex` ディレクトリ（Windows 版は MAM の実行ファイルと同じ場所、macOS 版はデータディレクトリの中）に展開して、別のプロセスとして起動します。Codex は Apache-2.0 のプログラムで、**MAM のインストーラー・ポータブル版・リリースには Codex のバイトを一切含めません**。MAM は Codex のコードをリンクせず、標準入出力の JSON-RPC で通信するだけです。

- Codex のライセンス（Apache-2.0）と対応するソースコード: <https://github.com/openai/codex/tree/rust-v0.164.0>（取得するのは <https://github.com/openai/codex/releases/tag/rust-v0.164.0> のアセット `codex-app-server-x86_64-pc-windows-msvc.exe.zip`・`codex-app-server-aarch64-pc-windows-msvc.exe.zip`（Windows）または `codex-app-server-aarch64-apple-darwin.tar.gz`（macOS）で、SHA-256 とサイズを MAM のカタログに固定して検証します）。
- Codex が保存するログイン情報は、MAM 専用の `CODEX_HOME`（データディレクトリの `codex-home`）と OS の資格情報ストアにあり、MAM は読みません。
- 取得したものを再配布するときは、Apache-2.0 の LICENSE と NOTICE（上のソースにあります）を添えてください。取得した実行ファイルが静的に含む依存のライセンスは、**一覧を調べていません（未確認）**。
EOF
} >"$tmp_out"
chmod 644 "$tmp_out" # mktemp creates it 0600
mv "$tmp_out" "$out"
tmp_out=""

echo "wrote $out: $(cut -d'|' -f1-3 "$records" | LC_ALL=C sort -u | wc -l | tr -d ' ') components" >&2
