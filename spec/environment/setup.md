# 環境構築

## 開発ツール

Go（`go.mod` のバージョン）、Node.js（24。CI は `.github/workflows/ci.yml` で `node-version: 24` を固定している。`static/package.json` に `engines` は無い）、`templ`、`golangci-lint`、`linterly`、`lefthook`、`jq`、Chrome / Chromium。`make tools` が templ（`go.mod` の版に合わせる）・air・golangci-lint を版を固定して入れ、npm 依存（`static/node_modules`）も用意する。`make assets` / `make test-static` / `make lint-static` / `make dev` は `static/node_modules` が無ければ（または `package-lock.json` が新しければ）`npm ci` を自動で実行する。フロントエンドのパッケージマネージャは npm（`static/package-lock.json`）。

- **`jq`**: pre-commit の `scripts/linterly-staged.sh`（コミット対象ファイルの行数チェック）が使う。無いと「jq not found」で pre-commit が必ず失敗する（`make tools` では入らない。OS のパッケージマネージャで入れる）。
- **Chrome / Chromium**: Lit コンポーネントのテスト（`*.wtest.ts`）を実際のブラウザで動かす `@web/test-runner-chrome`（`static/web-test-runner.config.mjs`）が、インストール済みの Chrome を探して使う。`make test-static`（したがって `make test` と pre-push）が必要とし、見つからないときは `CHROME_PATH` で実行ファイルを指定する。CI（`ubuntu-latest`）は GitHub 同梱のものを使う。
- **ffmpeg / ffprobe**: ビルドには要らず、実行時に任意（PATH に無ければ使わない）。無いと Judge（起動時に警告して無効になる）、Take のプレビュー（4K の 1080p プロキシ・サムネイル）、部分採用・区間指定の再生成の合成、Continue の最終フレーム取り出し、Rough Cut のプレビュー、音声制作（Audio Director: 音声の生成・ミックス・ラウドネス正規化・Lip Sync のクリップ切り出し）、最終 Export が使えない（Export は `ErrUnavailable`）。詳細は `spec/architecture/model-adapters.md` §10.2 と `spec/api/endpoints.md` の各 API。

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

## CI

`.github/workflows/ci.yml`。`main` への push と全 PR で実行する。ツールは `make tools`（Makefile の版指定）で入れる。`make generate` → コミット済み `*_templ.go` の一致確認 → `fmt-check` → `lint` → `test` → `build` の順。`make generate` の直後に `*_templ.go` が変更・未追跡になっていれば（`.templ` と食い違う古い・手編集の出力、再生成漏れ）失敗させる。`lint` / `test` / `build` は先に再生成するため、この確認がないと食い違いが緑になる（`_templ.go` は git 管理で、素の `go build` はそれをそのままコンパイルする）。

## Linterly

`.linterly.yml`。行数上限はデフォルト（1 ファイル 300 行 / 1 ディレクトリ 2000 行）のまま維持し、特別な理由がない限り変えない。除外は生成物（`*_templ.go`、`static/src/dist/**`、`package-lock.json`）のみで、手書きソースは除外しない。既存の超過は CI では止めず、pre-commit でコミット対象のファイルに限って止める。

## Git Hooks

`lefthook.yml`（`lefthook install` で有効化）。

- pre-commit: コミット対象ファイルの行数（`scripts/linterly-staged.sh`。`linterly check` は 1 パスしか受け取れないため全体結果をコミット対象に絞る）、`make fmt-check`、`make lint-go`、`make lint-static`（対象ファイルがある場合）
- pre-push: `go build ./...` と `go vet ./...`、それに `make test`
