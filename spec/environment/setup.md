# 環境構築

## 開発ツール

Go（`go.mod` のバージョン）、Node.js（24。CI は `.github/workflows/ci.yml` で `node-version: 24` を固定している。`static/package.json` に `engines` は無い）、`templ`、`golangci-lint`、`linterly`、`govulncheck`、`lefthook`、`jq`、Chrome / Chromium。`make tools` が templ（`go.mod` の版に合わせる）・air・golangci-lint・linterly・govulncheck を版を固定して入れ、npm 依存（`static/node_modules`。Biome を含む）も用意する。`make assets` / `make test-static` / `make lint-static` / `make fmt` / `make fmt-check` / `make dev` は `static/node_modules` が無ければ（または `package-lock.json` が新しければ）`npm ci` を自動で実行する。フロントエンドのパッケージマネージャは npm（`static/package-lock.json`）。

- **`jq`**: pre-commit の `scripts/linterly-staged.sh`（コミット対象ファイルの行数チェック）と `scripts/linterly-baseline.sh`（行数の基準値チェック。`make lint-lines`・CI）が使う。無いと「jq not found」で pre-commit が必ず失敗する（`make tools` では入らない。OS のパッケージマネージャで入れる）。
- **Chrome / Chromium**: Lit コンポーネントのテスト（`*.wtest.ts`）を実際のブラウザで動かす `@web/test-runner-chrome`（`static/web-test-runner.config.mjs`）が、インストール済みの Chrome を探して使う。`make test-static`（したがって `make test` と pre-push）が必要とし、見つからないときは `CHROME_PATH` で実行ファイルを指定する。CI（`ubuntu-latest`）は GitHub 同梱のものを使う。
- **ffmpeg / ffprobe**: ビルドには要らず、実行時に任意（PATH に無ければ使わない）。無いと Judge（起動時に警告して無効になる）、Take のプレビュー（4K の 1080p プロキシ・サムネイル）、部分採用・区間指定の再生成の合成、Continue の最終フレーム取り出し、Rough Cut のプレビュー、音声制作（Audio Director: 音声の生成・ミックス・ラウドネス正規化・Lip Sync のクリップ切り出し）、最終 Export が使えない（Export は `ErrUnavailable`）。詳細は `spec/architecture/model-adapters.md` §10.2 と `spec/api/endpoints.md` の各 API。
- **Wails v3 CLI（`wails3`）と NSIS（`makensis`）**: Windows 版のパッケージング（`scripts/package-windows.sh`）だけが使う。`wails3` は `go install github.com/wailsapp/wails/v3/cmd/wails3@<版>` で入れ、版は `go.mod` の `github.com/wailsapp/wails/v3` と同じものに固定する（v3 はベータ）。NSIS は `choco install nsis`（Windows）。通常の開発・`make build`・`make test` には要らない。

## タスク

| コマンド | 内容 |
|---|---|
| `make generate` | `templ generate ./backend/...` |
| `make fmt` | `templ fmt backend`、`golangci-lint fmt ./...`（`.golangci.yml` の formatter は gofmt と goimports）、Biome によるフロントエンド TypeScript の整形（`npm run format --prefix static`） |
| `make fmt-check` | `gofmt -l backend static` が空であることと `templ fmt -fail backend`（goimports は見ない）、`biome format`（`npm run format:check --prefix static`）。未整形で失敗 |
| `make lint` | `make lint-go` と `make lint-static` |
| `make lint-lines` | `scripts/linterly-baseline.sh check`。`.linterly-baseline` より悪化した行数超過があれば失敗（Linterly 節）。`linterly` と `jq` が要る |
| `make vuln` | `govulncheck ./...`。コードから到達できる既知の脆弱性（標準ライブラリ・依存モジュール）があれば失敗。`make tools` が入れる `govulncheck` が要る |
| `make linterly-baseline` | `scripts/linterly-baseline.sh update`。`.linterly-baseline` を現在の `linterly check` の結果で書き換える |
| `make lint-go` | `make generate` のあと `golangci-lint run ./...`（`.golangci.yml`。`gocyclo` 25 / `gocognit` 50 の複雑度上限を含む。「関数の複雑度」節） |
| `make lint-static` | `tsc --noEmit`（`npm run typecheck --prefix static`）と `biome lint`（`npm run lint --prefix static`） |
| `make test` | `make test-go` と `make test-static` と `make test-scripts` |
| `make test-scripts` | `bash scripts/linterly-baseline_test.sh`（行数基準値のスクリプトのテスト。`jq` が要る） |
| `make test-go` | `make generate` のあと `go test -race -timeout 30m ./...`（`-race` の逐次テストが長く、既定の 10 分では負荷時に足りない） |
| `make test-static` | `npm test --prefix static` |
| `make build` | `scripts/build.sh`（templ → esbuild/tailwind minify → go build）。`bin/mam` を作る |
| `make build-all` | `scripts/build.sh --all`。windows / darwin / linux × amd64 / arm64 の 6 構成を `bin/mam-<os>-<arch>[.exe]`（Windows は `.exe` 付き）に出力する（`spec/requirements/non-functional.md` §1） |
| `make clean` | `bin`・`tmp`・`static/src/dist/app.*` を削除する |
| `scripts/package-windows.sh <version>` | Windows 版（Wails ウィンドウ）のパッケージング。templ → esbuild/tailwind → `wails3 package GOOS=windows`（`GOARCH=amd64`）で `bin/mam-<version>-windows-amd64.exe`（ポータブル）と `bin/mam-<version>-windows-amd64-setup.exe`（NSIS インストーラー）を作る。`<version>` は `X.Y.Z` 形式でなければ失敗する。Windows 上（Git Bash）で動かす。`make` のターゲットは無い（Windows に `make` が無いため） |

## フロントエンドの整形・lint

`static/src` の TypeScript（`*.ts`・`*.wtest.ts`・`*.test.ts`）と `static/build.mjs`・`static/web-test-runner.config.mjs` は Biome（`@biomejs/biome`。`static/package.json` に版を固定）で整形・lint する。設定は `static/biome.json`（インデント 2 スペース・シングルクォート・行幅 120・末尾カンマ。`.gitignore` と `static/src/dist` は対象外）。

- 整形: `make fmt`（`npm run format --prefix static`）で書き換え、`make fmt-check`（`npm run format:check --prefix static`）で未整形なら失敗する。
- lint: `make lint-static` が `tsc --noEmit` のあとに `biome lint` を走らせる。recommended に加えて、未使用の import / 変数（`noUnusedImports`・`noUnusedVariables`）、`==`（`noDoubleEquals`）、`void` を付け忘れた Promise（`nursery/noFloatingPromises`）をエラーにする。`!`（非 null アサーション）は `noUncheckedIndexedAccess` の下で意図して使っているため `noNonNullAssertion` は無効にしている。
- import の並べ替え（organizeImports）は使わない。
- 初回の一括整形は 1 コミットに分けてある。以降の差分に整形のノイズを混ぜない。
- 整形で行数が増えた分は `.linterly-baseline` の該当エントリに反映した（機械的な増加であり、悪化の見逃しではない）。

CI は `make fmt-check` と `make lint` を実行するため、上の検査は追加のステップ無しで CI に入る（`static/node_modules` は `make tools` の `npm ci` で用意される）。

## CI

`.github/workflows/ci.yml`。`main` への push と全 PR で実行する。ツールは `make tools`（Makefile の版指定）で入れる。`make generate` → コミット済み `*_templ.go` の一致確認 → `fmt-check` → `lint` → 脆弱性の検査（`make vuln`。後述）→ 行数の悪化確認（`make lint-lines`。後述の Linterly 節）→ ffmpeg の導入（`sudo apt-get install -y ffmpeg`）→ `test` → `build` の順。`make generate` の直後に `*_templ.go` が変更・未追跡になっていれば（`.templ` と食い違う古い・手編集の出力、再生成漏れ）失敗させる。`lint` / `test` / `build` は先に再生成するため、この確認がないと食い違いが緑になる（`_templ.go` は git 管理で、素の `go build` はそれをそのままコンパイルする）。`cmd/wails`（`//go:build windows`）は Ubuntu の通常のステップに入らないため、`GOOS=windows CGO_ENABLED=0` で `go build ./backend/...`・`go vet ./backend/...`・`golangci-lint run ./backend/...` を別ステップで実行する（Windows 版の壊れを PR で検知する）。

標準ライブラリの脆弱性は Go の版で決まるため、`go.mod` の `toolchain` 行で修正版の Go（`actions/setup-go` の `go-version-file: go.mod` が CI と `release.yml` の両方で読む）を固定する。`lint` の次のステップで `make vuln`（`govulncheck ./...`）を実行し、コードから到達できる既知の脆弱性（標準ライブラリ・依存モジュール）があれば失敗させる。依存の更新 PR は `.github/dependabot.yml`（`gomod`・`npm`（`/static`）・`github-actions`）が週次で作る。

ffmpeg / ffprobe は `ubuntu-latest` に入っていないため、`test` の前に導入する。ffmpeg を使う Go テスト（Judge のフレーム抽出、`media`、Audio Director のミックス、Take のプレビュー・部分採用・区間指定の再生成、Continue の最終フレーム）は、PATH に無いとローカルでは `t.Skip` されるが、`CI=true`（GitHub Actions が設定する）では `backend/internal/testenv` の `MissingFFmpeg` が失敗にする。CI の導入手順が外れても緑のまま飛ばされない。ffmpeg の無い開発機では該当テストが Skip され、lefthook の pre-push（同じ `make test`）でも同様なので、実ファイルの経路を確かめるには ffmpeg を入れてから `go test -v` で SKIP が出ていないことを見る。

顔同一性（YuNet / SFace・ONNX Runtime）の実モデルのテスト（`backend/internal/facesim/onnx_test.go` の `TestONNX_SamePersonIsCloserThanAnotherPerson` と `TestONNX_ImageWithoutFaceHasNoFaces`）は、CI では動かさない。`MAM_FACESIM_MODEL_DIR`（モデルのディレクトリ。`scripts/fetch-face-models.sh <DIR>` で取得する）・`MAM_FACESIM_PHOTOS`（`a.jpg` / `a_small.jpg` / `b.jpg` / `none.jpg` を置いたディレクトリ）・任意の `MAM_FACESIM_RUNTIME_LIB`（`libonnxruntime` のパス、ONNX Runtime 1.23.x）が揃ったときだけ動き、未設定だと `t.Skip` するため、CI・`make test`・pre-push では常に Skip される（ffmpeg のような `CI=true` での失敗化はしない）。写真が権利上リポジトリに置けず、モデルとランタイムの取得も重いため。`align.go` / `detect.go` / `imaging.go`（顔検出・整列・埋め込みの前処理）を変えたときは、手元で `go test -v -run TestONNX ./backend/internal/facesim/` を実行し、Skip が出ないことと類似度の値（同一人物 > 0.7、別人物 < 0.3）を確かめる。詳細は `spec/architecture/model-adapters.md` §10.1。

## リリース

`.github/workflows/release.yml`。`v*` タグ（`vX.Y.Z`）の push で実行する。ワークフロー全体の `permissions` は `contents: read` で、書き込み権限（`contents: write`）を持つのは `release` ジョブだけ。ビルドの工程（`go install`・`choco install`・`npm ci`）は書き込み用の `GITHUB_TOKEN` を持たない環境で動く。`uses:` はコミット SHA で固定し（行末のコメントがタグ）、版は dependabot の `github-actions` が SHA ごと更新する。3 ジョブ構成（`verify` → `build` → `release`）:

### `verify`（`ubuntu-latest`、`contents: read` + `actions: read`）

タグのコミットを検証する。どれかが失敗したらビルドもリリースも走らない。

1. `git merge-base --is-ancestor "$GITHUB_SHA" origin/main`: タグのコミットが `main` の履歴にあること（`fetch-depth: 0`）。main 以外のコミットに打ったタグは公開しない
2. CI の成功確認: `gh run list --workflow ci.yml --commit "$GITHUB_SHA"` に `success` の実行があること。CI が赤い、または未完了のコミットは公開しない（CI が通るのを待ってからタグを打つ）
3. `make tools` → `make generate` → `make vuln`: CI の後に出た新しい脆弱性勧告でも止める

### `build`（`windows-latest`、`needs: verify`、`contents: read`）

1. checkout（`persist-credentials: false`）→ setup-go（`go.mod`）→ setup-node（24、npm キャッシュ）
2. ツール: `templ`（`go.mod` の版）、`wails3`（`go.mod` の版）、`choco install nsis --version <版>`（ワークフローの `NSIS_VERSION` で固定）
3. `bash scripts/package-windows.sh "${GITHUB_REF_NAME#v}"`（タグが `vX.Y.Z` でなければ失敗）
4. スモーク（`bash scripts/smoke-windows.sh bin/mam-<version>-windows-amd64-setup.exe`）: インストーラーを `/S` で `RUNNER_TEMP` 配下へ静かにインストールし（`/D=`）、そのディレクトリの `mam.exe` を `--data-dir`（一時ディレクトリ）・`--port` 固定で起動して `GET /` が 200 を返すことを確認してから停止する。失敗したらリリースを作らない
5. `actions/upload-artifact`（名前 `windows-release`）で 2 つの exe を `release` ジョブへ渡す

### `release`（`ubuntu-latest`、`needs: build`、`contents: write`）

checkout はせず（`GH_REPO` を渡す）、`actions/download-artifact` で受け取った exe を使って `gh release create "$GITHUB_REF_NAME" --title "$GITHUB_REF_NAME" --generate-notes --verify-tag dist/mam-<version>-windows-amd64-setup.exe dist/mam-<version>-windows-amd64.exe` を実行するだけ。

成果物はこの 2 つだけ（`spec/requirements/non-functional.md` §1）。コード署名はしない（署名の手順を足す場合は別 Issue）。リリースノートは GitHub の自動生成に任せる。同じタグのリリースが既にあれば `gh release create` が失敗するため、作り直すときはタグとリリースを削除してから push し直す。

## Linterly

`.linterly.yml`。行数上限はデフォルト（1 ファイル 300 行 / 1 ディレクトリ 2000 行）のまま維持し、特別な理由がない限り変えない。除外は生成物（`*_templ.go`、`static/src/dist/**`、`package-lock.json`）のみで、手書きソースは除外しない。

既存の超過（linterly の `error`: 上限に `warning_threshold` を足した値を超えたもの。`linterly check` が exit 1 になる判定と同じ）は、超過ゼロを目標にせず、**増やさないことを CI で保証する**。超過しているファイルとディレクトリの行数を `.linterly-baseline`（`<行数><TAB><パス>`、ディレクトリは末尾 `/`）に基準値として記録し、`scripts/linterly-baseline.sh check`（`make lint-lines`、CI の「Line limit (no regression)」ステップ）が次のどちらかで失敗させる。

- 基準値に無いファイル・ディレクトリが上限を超えた（新規の超過）
- 基準値にあるファイル・ディレクトリの行数が基準値より増えた

基準値より減った・上限以下に戻ったエントリは失敗にせず、「基準値を下げられる」と stderr に出す。分割して減らしたら `make linterly-baseline`（`scripts/linterly-baseline.sh update`）で `.linterly-baseline` を書き換えて同じ PR でコミットする。差分は行の削除か数値の減少だけであること（増えている差分は悪化を基準値に紛れ込ませているのでレビューで止める）。基準値に残っているファイルの分割は別 Issue で段階的に進める。`scripts/linterly-baseline_test.sh`（`make test-scripts`）がこの判定をスタブの `linterly` で検査する。

ディレクトリ上限（2000 行）も同じ基準値方式で扱い、Go のフラットなパッケージ向けの別基準（ファイル数など）は設けない。超過しているパッケージは、その直下の合計行数を増やせない（ファイルの追加・既存ファイルの行増で CI が落ちる）。新しいコードは新しいパッケージへ置くか、パッケージを責務ごとに分割して満たす。分割できたら基準値から外す。

pre-commit（`scripts/linterly-staged.sh`）は従来どおり、コミット対象のファイルが超過していれば基準値の有無に関わらず止める。

## 関数の複雑度

`.golangci.yml` の `gocyclo`（循環的複雑度 25 超）と `gocognit`（認知的複雑度 50 超）が、関数単位の複雑度の悪化を `make lint-go`（`make lint`・pre-commit・CI の `Lint` ステップ）で止める。テストコード（`_test.go`）は対象外。

既存の超過関数は `linters.exclusions.rules` の `text`（`of func \`<関数名>\` `）で個別に許可している（`//nolint` は使わない）。許可リストに無い関数が上限を超えれば lint エラーになるので、新規の超過は止まる。一方、許可済みの関数自体がさらに複雑になっても検出できないため、レビューで止める。関数を分割して上限以内になったら、同じ PR で許可エントリを削除する。許可リストが空になったら閾値を `gocyclo` 20 / `gocognit` 40 に下げる（別 Issue）。

## Git Hooks

`lefthook.yml`（`lefthook install` で有効化）。

- pre-commit: コミット対象ファイルの行数（`scripts/linterly-staged.sh`。`linterly check` は 1 パスしか受け取れないため全体結果をコミット対象に絞る）、`make fmt-check`（`*.go` / `*.templ` / `*.ts`）、`make lint-go`、`make lint-static`（`static/**/*.ts` がある場合。`tsc` と Biome lint）
- pre-push: `go build ./...` と `go vet ./...`、それに `make test`

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.1 | 2026-10-03 | Wails v3 CLI・NSIS を開発ツールに、`scripts/package-windows.sh` をタスクに、Windows 向けの CI ステップと `release.yml`（リリース）の節を追加。この表を新設した（以前の版の履歴は無い） | Issue #480。Windows インストーラー exe を GitHub Releases で配布するため |
| 1.2 | 2026-10-11 | `scripts/smoke-windows.sh`（リリースのスモーク）を `release.yml` の手順 4 に明記 | Issue #480 の実装。スモークの手順をスクリプトに切り出し、手元でも shellcheck できるようにした |
| 1.3 | 2026-10-11 | CI に ffmpeg の導入ステップを追加し、CI では ffmpeg 無しのテストが Skip でなく失敗になることを CI 節に明記 | Issue #485。`ubuntu-latest` に ffmpeg が無く、ffmpeg を使う Go テストが CI で全件 Skip されて緑になっていた |
| 1.4 | 2026-10-11 | CI 節に、顔同一性の実モデルのテストは環境変数が未設定で Skip し CI・`make test`・pre-push では動かさないこと、手元での実行方法を明記 | Issue #486。実モデルのテストが常に Skip されているのに、環境構築の仕様に存在も実行方法も書かれていなかった |
| 1.5 | 2026-10-11 | Linterly 節を、既存の超過を `.linterly-baseline` に記録して CI（`make lint-lines`）で悪化を止める運用に書き換え。ディレクトリ上限の方針を明記。`make tools` に linterly、`make test` に `test-scripts` を追加 | Issue #495。超過は CI で検出されず、増えても気づけず、減らす圧力もなかった |
| 1.6 | 2026-10-11 | フロントエンドの TypeScript に Biome（format + lint）を導入し、`make fmt` / `make fmt-check` / `make lint-static`・lefthook の pre-commit に組み込んだ。「フロントエンドの整形・lint」節を追加 | Issue #505。`static/src` の約 4,800 行に formatter / linter が無く、`tsc` だけで CI・pre-commit を通っていた（未使用 import・`no-floating-promises`・`==`・インデントの崩れがレビューまで検出されなかった） |
| 1.7 | 2026-10-11 | Linterly 節の基準値を、統合済みの各修正後の行数に合わせて更新（減った分は下げ、悪化した分は新しいパッケージへの移動・ファイル分割で戻した。基準値を上げたエントリは無い） | Issue #496・#500・#501。基準値が #482〜#484・#494 などの統合前に採られ、統合ブランチで `make lint-lines` が失敗していた |
| 1.8 | 2026-10-11 | `go.mod` に `toolchain go1.26.9` を追加し、`golang.org/x/net` を v0.60.0・`golang.org/x/crypto` を v0.57.0 へ更新。`make vuln`（`govulncheck ./...`）と、`make tools` への govulncheck の導入、CI の「Vulnerability check」ステップ、`.github/dependabot.yml`（gomod・npm・github-actions）を追加 | Issue #523。`go 1.26.0` のままでは `setup-go` が標準ライブラリの未修正版でビルドし（CI と配布する Windows バイナリ）、govulncheck が標準ライブラリ 16 件と x/net の脆弱性を検出していた。検知の仕組みも無かった |
| 1.9 | 2026-10-12 | 「開発ツール」に `govulncheck`（`make tools` が入れる）を加え、CI の実行順を `lint` → `make vuln` → `make lint-lines` に直した | Issue #525。1.8（#523）が `make vuln`・タスク表・脆弱性の段落・README には反映したのに、同じ内容を持つ「開発ツール」の列挙と CI の実行順の段落が更新されておらず、次の段落（`lint` の次に `make vuln`）・`.github/workflows/ci.yml`（`Lint` → `Vulnerability check` → `Line limit (no regression)`）・Makefile の `tools` ターゲットと食い違っていた |
| 1.10 | 2026-10-12 | `make lint-go` に `gocyclo`（25）・`gocognit`（50）の複雑度上限を追加し、既存の超過関数 5 個を除外ルールで許可する「関数の複雑度」節を追加 | Issue #534。行数は Linterly で縛っていたが関数単位の複雑度は未管理で、CI で悪化を検出できなかった |
| 1.11 | 2026-10-12 | リリース節を `verify`（main の履歴・CI 成功・`make vuln`）→ `build`（`contents: read`）→ `release`（`contents: write`、`gh release create` だけ）の 3 ジョブ構成に書き換え、NSIS の版固定と `uses:` の SHA 固定を明記 | Issue #541。リリースがタグの push だけで CI を通らず公開され、ビルドの全工程が書き込み権限付きの `GITHUB_TOKEN` を持っていた |
