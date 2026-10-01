# 環境構築

## 開発ツール

Go（`go.mod` のバージョン）、Node.js、`templ`、`golangci-lint`、`linterly`、`lefthook`。`make tools` が templ（`go.mod` の版に合わせる）・air・golangci-lint を版を固定して入れ、npm 依存（`static/node_modules`）も用意する。`make assets` / `make test-static` / `make lint-static` / `make dev` は `static/node_modules` が無ければ（または `package-lock.json` が新しければ）`npm ci` を自動で実行する。フロントエンドのパッケージマネージャは npm（`static/package-lock.json`）。

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
| `make test-go` | `make generate` のあと `go test -race ./...` |
| `make test-static` | `npm test --prefix static` |
| `make build` | `scripts/build.sh`（templ → esbuild/tailwind minify → go build）。`bin/mam` を作る |
| `make build-all` | `scripts/build.sh --all`。windows / darwin / linux × amd64 / arm64 の 6 構成を `bin/mam-<os>-<arch>` に出力する（`spec/requirements/non-functional.md` §1） |
| `make clean` | `bin`・`tmp`・`static/src/dist/app.*` を削除する |

## CI

`.github/workflows/ci.yml`。`main` への push と全 PR で実行する。ツールは `make tools`（Makefile の版指定）で入れる。`make generate` → `fmt-check` → `lint` → `test` → `build` の順。

## Linterly

`.linterly.yml`。行数上限はデフォルト（1 ファイル 300 行 / 1 ディレクトリ 2000 行）のまま維持し、特別な理由がない限り変えない。除外は生成物（`*_templ.go`、`static/src/dist/**`、`package-lock.json`）のみで、手書きソースは除外しない。既存の超過は CI では止めず、pre-commit でコミット対象のファイルに限って止める。

## Git Hooks

`lefthook.yml`（`lefthook install` で有効化）。

- pre-commit: コミット対象ファイルの行数（`scripts/linterly-staged.sh`。`linterly check` は 1 パスしか受け取れないため全体結果をコミット対象に絞る）、`make fmt-check`、`make lint-go`、`make lint-static`（対象ファイルがある場合）
- pre-push: `go build ./...` と `go vet ./...`、それに `make test`
