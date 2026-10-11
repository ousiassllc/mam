# 環境構築

## 開発ツール

Go（`go.mod` のバージョン）、Node.js（24。CI は `.github/workflows/ci.yml` で `node-version: 24` を固定している。`static/package.json` に `engines` は無い）、`templ`、`golangci-lint`、`linterly`、`lefthook`、`jq`、Chrome / Chromium。`make tools` が templ（`go.mod` の版に合わせる）・air・golangci-lint を版を固定して入れ、npm 依存（`static/node_modules`）も用意する。`make assets` / `make test-static` / `make lint-static` / `make dev` は `static/node_modules` が無ければ（または `package-lock.json` が新しければ）`npm ci` を自動で実行する。フロントエンドのパッケージマネージャは npm（`static/package-lock.json`）。

- **`jq`**: pre-commit の `scripts/linterly-staged.sh`（コミット対象ファイルの行数チェック）が使う。無いと「jq not found」で pre-commit が必ず失敗する（`make tools` では入らない。OS のパッケージマネージャで入れる）。
- **Chrome / Chromium**: Lit コンポーネントのテスト（`*.wtest.ts`）を実際のブラウザで動かす `@web/test-runner-chrome`（`static/web-test-runner.config.mjs`）が、インストール済みの Chrome を探して使う。`make test-static`（したがって `make test` と pre-push）が必要とし、見つからないときは `CHROME_PATH` で実行ファイルを指定する。CI（`ubuntu-latest`）は GitHub 同梱のものを使う。
- **ffmpeg / ffprobe**: ビルドには要らず、実行時に任意（PATH に無ければ使わない）。無いと Judge（起動時に警告して無効になる）、Take のプレビュー（4K の 1080p プロキシ・サムネイル）、部分採用・区間指定の再生成の合成、Continue の最終フレーム取り出し、Rough Cut のプレビュー、音声制作（Audio Director: 音声の生成・ミックス・ラウドネス正規化・Lip Sync のクリップ切り出し）、最終 Export が使えない（Export は `ErrUnavailable`）。詳細は `spec/architecture/model-adapters.md` §10.2 と `spec/api/endpoints.md` の各 API。
- **Wails v3 CLI（`wails3`）と NSIS（`makensis`）**: Windows 版のパッケージング（`scripts/package-windows.sh`）だけが使う。`wails3` は `go install github.com/wailsapp/wails/v3/cmd/wails3@<版>` で入れ、版は `go.mod` の `github.com/wailsapp/wails/v3` と同じものに固定する（v3 はベータ）。NSIS は `choco install nsis`（Windows）。通常の開発・`make build`・`make test` には要らない。

## タスク

| コマンド | 内容 |
|---|---|
| `make generate` | `templ generate ./backend/...` |
| `make fmt` | `templ fmt backend` と `golangci-lint fmt ./...`（`.golangci.yml` の formatter は gofmt と goimports） |
| `make fmt-check` | `gofmt -l backend static` が空であることと `templ fmt -fail backend`（goimports は見ない。未整形で失敗） |
| `make lint` | `make lint-go` と `make lint-static` |
| `make lint-go` | `make generate` のあと `golangci-lint run ./...`（`.golangci.yml`） |
| `make lint-static` | `tsc --noEmit`（`npm run typecheck --prefix static`） |
| `make test` | `make test-go` と `make test-static` |
| `make test-go` | `make generate` のあと `go test -race -timeout 30m ./...`（`-race` の逐次テストが長く、既定の 10 分では負荷時に足りない） |
| `make test-static` | `npm test --prefix static` |
| `make build` | `scripts/build.sh`（templ → esbuild/tailwind minify → go build）。`bin/mam` を作る |
| `make build-all` | `scripts/build.sh --all`。windows / darwin / linux × amd64 / arm64 の 6 構成を `bin/mam-<os>-<arch>[.exe]`（Windows は `.exe` 付き）に出力する（`spec/requirements/non-functional.md` §1） |
| `make clean` | `bin`・`tmp`・`static/src/dist/app.*` を削除する |
| `scripts/package-windows.sh <version>` | Windows 版（Wails ウィンドウ）のパッケージング。templ → esbuild/tailwind → `wails3 package GOOS=windows`（`GOARCH=amd64`）で `bin/mam-<version>-windows-amd64.exe`（ポータブル）と `bin/mam-<version>-windows-amd64-setup.exe`（NSIS インストーラー）を作る。`<version>` は `X.Y.Z` 形式でなければ失敗する。Windows 上（Git Bash）で動かす。`make` のターゲットは無い（Windows に `make` が無いため） |

## CI

`.github/workflows/ci.yml`。`main` への push と全 PR で実行する。ツールは `make tools`（Makefile の版指定）で入れる。`make generate` → コミット済み `*_templ.go` の一致確認 → `fmt-check` → `lint` → `test` → `build` の順。`make generate` の直後に `*_templ.go` が変更・未追跡になっていれば（`.templ` と食い違う古い・手編集の出力、再生成漏れ）失敗させる。`lint` / `test` / `build` は先に再生成するため、この確認がないと食い違いが緑になる（`_templ.go` は git 管理で、素の `go build` はそれをそのままコンパイルする）。`cmd/wails`（`//go:build windows`）は Ubuntu の通常のステップに入らないため、`GOOS=windows CGO_ENABLED=0` で `go build ./backend/...`・`go vet ./backend/...`・`golangci-lint run ./backend/...` を別ステップで実行する（Windows 版の壊れを PR で検知する）。

## リリース

`.github/workflows/release.yml`。`v*` タグ（`vX.Y.Z`）の push で実行する。`permissions` は `contents: write` のみ。`windows-latest` の 1 ジョブ:

1. checkout → setup-go（`go.mod`）→ setup-node（24、npm キャッシュ）
2. ツール: `templ`（`go.mod` の版）、`wails3`（`go.mod` の版）、`choco install nsis`
3. `bash scripts/package-windows.sh "${GITHUB_REF_NAME#v}"`（タグが `vX.Y.Z` でなければ失敗）
4. スモーク（`bash scripts/smoke-windows.sh bin/mam-<version>-windows-amd64-setup.exe`）: インストーラーを `/S` で `RUNNER_TEMP` 配下へ静かにインストールし（`/D=`）、そのディレクトリの `mam.exe` を `--data-dir`（一時ディレクトリ）・`--port` 固定で起動して `GET /` が 200 を返すことを確認してから停止する。失敗したらリリースを作らない
5. `gh release create "$GITHUB_REF_NAME" --title "$GITHUB_REF_NAME" --generate-notes --verify-tag bin/mam-<version>-windows-amd64-setup.exe bin/mam-<version>-windows-amd64.exe`

成果物はこの 2 つだけ（`spec/requirements/non-functional.md` §1）。コード署名はしない（署名の手順を足す場合は別 Issue）。リリースノートは GitHub の自動生成に任せる。同じタグのリリースが既にあれば `gh release create` が失敗するため、作り直すときはタグとリリースを削除してから push し直す。

## Linterly

`.linterly.yml`。行数上限はデフォルト（1 ファイル 300 行 / 1 ディレクトリ 2000 行）のまま維持し、特別な理由がない限り変えない。除外は生成物（`*_templ.go`、`static/src/dist/**`、`package-lock.json`）のみで、手書きソースは除外しない。既存の超過は CI では止めず、pre-commit でコミット対象のファイルに限って止める。

## Git Hooks

`lefthook.yml`（`lefthook install` で有効化）。

- pre-commit: コミット対象ファイルの行数（`scripts/linterly-staged.sh`。`linterly check` は 1 パスしか受け取れないため全体結果をコミット対象に絞る）、`make fmt-check`、`make lint-go`、`make lint-static`（対象ファイルがある場合）
- pre-push: `go build ./...` と `go vet ./...`、それに `make test`

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.1 | 2026-10-03 | Wails v3 CLI・NSIS を開発ツールに、`scripts/package-windows.sh` をタスクに、Windows 向けの CI ステップと `release.yml`（リリース）の節を追加。この表を新設した（以前の版の履歴は無い） | Issue #480。Windows インストーラー exe を GitHub Releases で配布するため |
| 1.2 | 2026-10-11 | `scripts/smoke-windows.sh`（リリースのスモーク）を `release.yml` の手順 4 に明記 | Issue #480 の実装。スモークの手順をスクリプトに切り出し、手元でも shellcheck できるようにした |
