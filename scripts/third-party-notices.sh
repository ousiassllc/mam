#!/usr/bin/env bash
# Regenerates THIRD_PARTY_NOTICES.md: the licenses of the third-party software that goes into the
# Windows release (the portable exe and the installer), taken verbatim from each component's own license files.
#   - Go modules linked into backend/cmd/wails (GOOS=windows, -tags production, the same build as
#     build/windows/Taskfile.yml), plus the Go standard library
#   - npm packages in the production front-end bundle: what static/src/main.ts imports (found with an esbuild
#     metafile, like static/build.mjs bundles it) and tailwindcss (its CSS is part of the generated app.css)
#   - material copied into the source tree: the Lucide icons of backend/internal/web/atoms/icon.templ, with the
#     license text of the release they were copied from (atoms/icons/LICENSE; the release is named in icon.templ)
#   - scripts/third-party-notices-musetalk.md: the MuseTalk section (text that is written by hand)
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
- インストーラーに同梱する Microsoft の WebView2 ブートストラッパー（下の節）

開発用のツール・テスト用のパッケージ（`devDependencies` など）は配布物に入らないので載せていません。ffmpeg・ComfyUI・GPT-SoVITS・MuseTalk・Codex は同梱しません（利用者が取得する別プログラムとして、下の節に載せています）。

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

## ComfyUI（利用者が取得する別プログラム）

MAM のモデルマネージャーは、利用者の操作で ComfyUI のポータブル版（v0.42.1）を公式のリリースから取得し、MAM の実行ファイルと同じ場所の `comfyui` ディレクトリに展開して、別のプロセスとして起動します。ComfyUI は GPL-3.0 のプログラムで、**MAM のインストーラー・ポータブル版・リリースには ComfyUI のバイトを一切含めません**。MAM は ComfyUI のコードをリンクせず、HTTP（`127.0.0.1`）で通信するだけです。

- ComfyUI のライセンス（GPL-3.0）と対応するソースコード: <https://github.com/Comfy-Org/ComfyUI/tree/v0.42.1>（取得するポータブル版は <https://github.com/Comfy-Org/ComfyUI/releases/tag/v0.42.1> のアセットで、SHA-256 を MAM のカタログに固定して検証します）。
- **ノード画面（ComfyUI のフロントエンド、GPL-3.0）**: MAM の「ノード」画面（`/nodes`）は、取得した ComfyUI が配信する画面（`Comfy-Org/ComfyUI_frontend`、GPL-3.0。ポータブル版に pip パッケージ `comfyui-frontend-package` として入っている）を iframe で表示するだけです。**MAM はそのコードを一切含めず、改変も再配布もしません**（ブラウザは ComfyUI のサーバーから直接読み込みます）。ソース: <https://github.com/Comfy-Org/ComfyUI_frontend>。
- ポータブル版に入っている Python・PyTorch・その他のパッケージのライセンスは、**一覧を調べていません（未確認）**。取得したものを再配布するときは、取得した `comfyui\engine` の中の各ライセンスを確認してください。
- 利用者が導入できるモデルの重み（Stable Diffusion XL・FLUX.1・Qwen-Image・Wan 2.2・LTX-Video・Stable Diffusion 3.5・HunyuanVideo など）は、Hugging Face から利用者が取得します。MAM は再配布せず、各モデルのライセンス（商用利用の可否・地域の制限を含む）は導入の画面に表示します。条件の一部（FLUX.1 [dev] の非商用、HunyuanVideo の EU・英国・韓国での不可など）は、導入の前に同意を求めます。

## GPT-SoVITS（利用者が取得する別プログラム）

MAM のローカルランタイム管理（Issue #918）は、利用者の操作で GPT-SoVITS（声のクローン・音声合成。公式の Windows 統合パッケージ `GPT-SoVITS-v2pro-20250604`、コードのタグ `20250606v2pro`）を公式の配布先（Hugging Face の `lj1995/GPT-SoVITS-windows-package`、コミット固定）から取得し、MAM の実行ファイルと同じ場所の `localai\gpt-sovits` ディレクトリに展開して、別のプロセス（公式の `api_v2.py`、`127.0.0.1` だけで待ち受け）として起動します。GPT-SoVITS のコードは MIT ライセンスで、**MAM のインストーラー・ポータブル版・リリースには GPT-SoVITS のバイトを一切含めません**。MAM は GPT-SoVITS のコードをリンクせず、HTTP（`127.0.0.1`）で通信するだけです。対応するのは Windows（x64）と NVIDIA の GPU だけで、それ以外の環境では導入せず、理由を表示します。

- GPT-SoVITS のライセンス（MIT）とソースコード: <https://github.com/RVC-Boss/GPT-SoVITS/tree/20250606v2pro>（ライセンス文は <https://github.com/RVC-Boss/GPT-SoVITS/blob/20250606v2pro/LICENSE>。取得するパッケージは <https://huggingface.co/lj1995/GPT-SoVITS-windows-package> のファイルで、SHA-256 と大きさを MAM のカタログに固定して検証します）。
- 統合パッケージには、GPT-SoVITS のコードのほかに、Python の実行環境・PyTorch・その他の多数のパッケージと、**学習済みモデルの重み**が入っています。**それぞれのライセンスと商用利用の可否は、一覧を調べていません（未確認）**。取得したものを再配布するときや商用に使うときは、取得した `localai\gpt-sovits\env` の中の各ライセンスと、重みの配布元（<https://huggingface.co/lj1995/GPT-SoVITS>）の記載を確認してください。重みごとの出典とライセンスは、次の節に載せ、モデルマネージャーの画面にも表示します。

EOF
  # The MuseTalk section is a file of its own: it lists the Python packages of the environment mam assembles (a long table).
  cat scripts/third-party-notices-musetalk.md
  cat <<'EOF'
## GPT-SoVITS と MuseTalk の重み・依存ファイル（利用者が取得するもの）

MAM のローカルランタイム管理（Issue #920）は、上の GPT-SoVITS の統合パッケージに入っている重みと依存ファイル、および MuseTalk 1.5 が使う重みと依存ファイルを、固定の URL（Hugging Face のコミット固定）と SHA-256 で検証します。**MAM はこれらを同梱せず再配布しません**。足りない・壊れたファイルだけを、利用者の操作で取得元から取得します。出典・ライセンス・商用利用の可否はモデルマネージャーの画面（「ローカル音声・口パク」）と MCP（`list_local_runtimes`）にも表示します。固定の一覧はソースの `backend/internal/modelmgr/catalog/data/assets/` にあり、意味は `spec/architecture/model-manager/local-runtime.md` の §12 です。**法的な助言ではありません**。再配布や商用利用の前に、配布元の条件を確認してください。

**商用利用がはっきりしないもの（`unclear`）は、GPT-SoVITS の g2pW のモデル、LatentSync の SyncNet、S3FD の顔検出の重み、face-parse-bisent の `79999_iter.pth`（CelebAMask-HQ で学習。データは非商用の研究目的と README に明記）、torchvision の `resnet18` です。** 画面では、これらを琥珀色の注記で目立つように表示します。

### GPT-SoVITS の重みと依存ファイル

| 名前 | 出典 | ライセンス | 商用利用 |
|---|---|---|---|
| GPT-SoVITS v2 の GPT モデル | <https://huggingface.co/lj1995/GPT-SoVITS> | [MIT License（GPT-SoVITS の学習済みモデル）](https://huggingface.co/lj1995/GPT-SoVITS) | 可 |
| GPT-SoVITS v2 の SoVITS モデル | <https://huggingface.co/lj1995/GPT-SoVITS> | [MIT License（GPT-SoVITS の学習済みモデル）](https://huggingface.co/lj1995/GPT-SoVITS) | 可 |
| chinese-hubert-base の設定 | <https://huggingface.co/lj1995/GPT-SoVITS> | [MIT License（chinese-hubert-base）](https://huggingface.co/TencentGameMate/chinese-hubert-base) | 可 |
| chinese-hubert-base の前処理の設定 | <https://huggingface.co/lj1995/GPT-SoVITS> | [MIT License（chinese-hubert-base）](https://huggingface.co/TencentGameMate/chinese-hubert-base) | 可 |
| chinese-hubert-base の重み | <https://huggingface.co/lj1995/GPT-SoVITS> | [MIT License（chinese-hubert-base）](https://huggingface.co/TencentGameMate/chinese-hubert-base) | 可 |
| chinese-roberta-wwm-ext-large の設定 | <https://huggingface.co/lj1995/GPT-SoVITS> | [Apache License 2.0（chinese-roberta-wwm-ext-large）](https://huggingface.co/hfl/chinese-roberta-wwm-ext-large) | 可 |
| chinese-roberta-wwm-ext-large のトークナイザー | <https://huggingface.co/lj1995/GPT-SoVITS> | [Apache License 2.0（chinese-roberta-wwm-ext-large）](https://huggingface.co/hfl/chinese-roberta-wwm-ext-large) | 可 |
| chinese-roberta-wwm-ext-large の重み | <https://huggingface.co/lj1995/GPT-SoVITS> | [Apache License 2.0（chinese-roberta-wwm-ext-large）](https://huggingface.co/hfl/chinese-roberta-wwm-ext-large) | 可 |
| fastText の言語判定モデル（lid.176） | <https://fasttext.cc/docs/en/language-identification.html> | [CC BY-SA 3.0（fastText の言語判定モデル）](https://fasttext.cc/docs/en/language-identification.html) | 可 |
| NLTK の cmudict（英語の発音辞書） | <https://github.com/nltk/nltk_data> | [CMU Pronouncing Dictionary の利用条件](https://raw.githubusercontent.com/nltk/nltk_data/gh-pages/index.xml) | 可 |
| NLTK の Averaged Perceptron Tagger（英語の品詞推定） | <https://github.com/nltk/nltk_data> | [MIT License（Averaged Perceptron Tagger）](https://github.com/sloria/textblob-aptagger) | 可 |
| Open JTalk の辞書（日本語の読みと音の並び） | <https://github.com/r9y9/pyopenjtalk> | [BSD 3-Clause（Open JTalk の辞書）](https://github.com/r9y9/pyopenjtalk/blob/master/LICENSE.md) | 可 |
| g2pW（中国語の読みの推定） | <https://github.com/GitYCC/g2pW> | [g2pW の学習済みモデル（配布条件を確認できていない）](https://github.com/GitYCC/g2pW) | **はっきりしない（未確認）** |

### MuseTalk 1.5 の重みと依存ファイル

| 名前 | 出典 | ライセンス | 商用利用 |
|---|---|---|---|
| MuseTalk 1.5 の重み（unet.pth） | <https://huggingface.co/TMElyralab/MuseTalk> | [MIT License（MuseTalk の学習済みモデル）](https://huggingface.co/TMElyralab/MuseTalk/blob/2bcb936e2fddb4d86db4c62fd45b387d0c061571/LICENSE.txt) | 可 |
| MuseTalk 1.5 の設定（musetalk.json） | <https://huggingface.co/TMElyralab/MuseTalk> | [MIT License（MuseTalk の学習済みモデル）](https://huggingface.co/TMElyralab/MuseTalk/blob/2bcb936e2fddb4d86db4c62fd45b387d0c061571/LICENSE.txt) | 可 |
| sd-vae-ft-mse の重み | <https://huggingface.co/stabilityai/sd-vae-ft-mse> | [MIT License（sd-vae-ft-mse）](https://huggingface.co/stabilityai/sd-vae-ft-mse) | 可 |
| sd-vae-ft-mse の設定 | <https://huggingface.co/stabilityai/sd-vae-ft-mse> | [MIT License（sd-vae-ft-mse）](https://huggingface.co/stabilityai/sd-vae-ft-mse) | 可 |
| whisper-tiny の重み | <https://huggingface.co/openai/whisper-tiny> | [Apache License 2.0（whisper-tiny）](https://huggingface.co/openai/whisper-tiny) | 可 |
| whisper-tiny の設定 | <https://huggingface.co/openai/whisper-tiny> | [Apache License 2.0（whisper-tiny）](https://huggingface.co/openai/whisper-tiny) | 可 |
| whisper-tiny の前処理の設定 | <https://huggingface.co/openai/whisper-tiny> | [Apache License 2.0（whisper-tiny）](https://huggingface.co/openai/whisper-tiny) | 可 |
| DWPose の重み（dw-ll_ucoco_384.pth） | <https://huggingface.co/yzd-v/DWPose> | [Apache License 2.0（DWPose）](https://huggingface.co/yzd-v/DWPose) | 可 |
| LatentSync の SyncNet（latentsync_syncnet.pt） | <https://huggingface.co/ByteDance/LatentSync> | [CreativeML OpenRAIL++（LatentSync の配布物）](https://huggingface.co/ByteDance/LatentSync) | **はっきりしない（未確認）** |
| S3FD の顔検出の重み（s3fd-619a316812.pth） | <https://github.com/1adrianb/face-alignment> | [S3FD の学習済みモデル（ライセンスの表示を確認できていない）](https://github.com/1adrianb/face-alignment) | **はっきりしない（未確認）** |
| face-parse-bisent の重み（79999_iter.pth） | <https://github.com/zllrunning/face-parsing.PyTorch> | [face-parsing.PyTorch の学習済みモデル（学習データが研究用）](https://github.com/zllrunning/face-parsing.PyTorch) | **はっきりしない（未確認）** |
| face-parse-bisent の ResNet-18（resnet18-5c106cde.pth） | <https://github.com/pytorch/vision> | [torchvision の学習済みモデル（ResNet-18）](https://github.com/pytorch/vision#pre-trained-models) | **はっきりしない（未確認）** |

- 取得先が原本と違うもの（第三者のミラー）は、`source_note` に原本の場所と SHA-256 が一致することを確かめた日付（2026-11-01）を書いています。face-parse-bisent は原本が Google Drive だけの配布のため、Hugging Face のミラー（`ManyOtherFunctions/face-parse-bisent`）を、Drive の原本と SHA-256 が一致することを確かめた上で使います。
- 各ライセンスは、モデルカード・同梱の LICENSE・配布一覧の記載を読んで記録したもので、**原文との突き合わせや学習データのライセンスの確認までは行っていません（未確認）**。

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
