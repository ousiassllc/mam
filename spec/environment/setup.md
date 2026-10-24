# 環境構築

## 開発ツール

Go（`go.mod` のバージョン）、Node.js（24。CI は `.github/workflows/ci.yml` で `node-version: 24` を固定している。`static/package.json` に `engines` は無い）、`templ`、`golangci-lint`、`linterly`、`govulncheck`、`lefthook`、`jq`、Chrome / Chromium。`make tools` が templ（`go.mod` の版に合わせる）・air・golangci-lint・linterly・govulncheck を版を固定して入れ、npm 依存（`static/node_modules`。Biome を含む）も用意する。`make assets` / `make test-static` / `make lint-static` / `make fmt` / `make fmt-check` / `make dev` は `static/node_modules` が無ければ（または `package-lock.json` が新しければ）`npm ci` を自動で実行する。フロントエンドのパッケージマネージャは npm（`static/package-lock.json`）。

- **`jq`**: pre-commit の `scripts/linterly-staged.sh`（コミット対象ファイルの行数チェック）と `scripts/linterly-baseline.sh`（行数の基準値チェック。`make lint-lines`・CI）、Windows 版のパッケージング `scripts/package-windows.sh`（`build/windows/info.json` へのバージョン書き込み）が使う。無いと「jq not found」で pre-commit が必ず失敗し、`scripts/package-windows.sh` もツール確認で失敗する（`make tools` では入らない。OS のパッケージマネージャで入れる。Windows の Git Bash には既定で入っていない）。
- **Chrome / Chromium**: Lit コンポーネントのテスト（`*.wtest.ts`）を実際のブラウザで動かす `@web/test-runner-chrome`（`static/web-test-runner.config.mjs`）が、インストール済みの Chrome を探して使う。`make test-static`（したがって `make test` と pre-push）が必要とし、見つからないときは `CHROME_PATH` で実行ファイルを指定する。CI（GitHub-hosted の `ubuntu-latest`）は、イメージに入っている Chrome の版がイメージの更新で動くため使わず、`Ensure Chrome` ステップが常に Chrome for Testing を取得して `CHROME_PATH` に渡す（web-test-runner は `CHROME_PATH` を優先する）。GitHub-hosted の runner は毎回空の状態で始まるため、取得（`RUNNER_TOOL_CACHE` 以下への展開）は実行のたびに行い、実行の間には残らない。取得は版を固定する: ダウンローダ `@puppeteer/browsers` は `static/package.json` の devDependency（版を固定）で `package-lock.json` が解決し、`make tools` の `npm ci` が入れた `static/node_modules/.bin/browsers` を使う（`npx --yes` で未検証の版を取得しない）。Chrome の版は `ci.yml` の `Ensure Chrome` ステップの `CHROME_VERSION`（`chrome@<ビルド番号>` の完全な版。`stable` などの浮動チャンネルは使わない）で固定し、同じコミットで `make test-static` の結果が変わらないようにする。Chrome の版上げは `CHROME_VERSION` を手で書き換える（`@puppeteer/browsers` は dependabot の `npm` が更新する）。`browsers install` は Chrome for Testing の zip を `unzip` で展開する（`unzip` も任意依存の `yauzl` も無いと `Extraction failed: no zip archiver is available` で失敗する。`yauzl` は `package-lock.json` に無い）ため、`Ensure Chrome` は取得の前に `unzip` が PATH に無ければ `sudo -n apt-get install -y --no-install-recommends unzip` で入れる（ffmpeg と同じ。`ubuntu-latest` のイメージには `unzip` が入っていて、パスワード無しの sudo が使える）。取得に失敗したらステップが失敗する（`shell: bash` で `-o pipefail` を有効にし、`browsers install` が失敗した終了コードを `| tail | cut` に握りつぶさせない。得た `CHROME_PATH` が空・実行できないパスのときも `::error::` を出して `exit 1` する）ので、空の `CHROME_PATH` を書いたまま `Test` まで進まない。
- **ffmpeg / ffprobe**: ビルドには要らず、実行時に任意（PATH に無ければ使わない）。無いと Judge（起動時に警告して無効になる）、Take のプレビュー（4K の 1080p プロキシ・サムネイル）、部分採用・区間指定の再生成の合成、Continue の最終フレーム取り出し、Rough Cut のプレビュー、音声制作（Audio Director: 音声の生成・ミックス・ラウドネス正規化・Lip Sync のクリップ切り出し）、最終 Export が使えない（Export は `ErrUnavailable`）。詳細は `spec/architecture/model-adapters.md` §10.2 と `spec/api/endpoints.md`・`spec/api/endpoints-api.md` の各 API。
- **Wails v3 CLI（`wails3`）と NSIS（`makensis`）**: Windows 版のパッケージング（`scripts/package-windows.sh`。上の `jq` も要る）だけが使う。`wails3` は `CGO_ENABLED=0 go install github.com/wailsapp/wails/v3/cmd/wails3@<版>` で入れ（cgo 有効だと Linux では webkitgtk-6.0 のヘッダを要求される）、版は `go.mod` の `github.com/wailsapp/wails/v3` と同じものに固定する（v3 はベータ）。NSIS は Linux では `apt install nsis`（リリースの `build` ジョブは、無ければ自動で入れる。下の「リリース」節）。通常の開発・`make build`・`make test` には要らない。

## タスク

| コマンド | 内容 |
|---|---|
| `make generate` | `go run github.com/a-h/templ/cmd/templ@<go.mod の templ の版> generate ./backend/...`。PATH 上の `templ` の版に依らず go.mod と同じ版で生成する（古い `templ` が go.mod とずれた `*_templ.go` を書き、CI の鮮度確認が落ちるのを防ぐ。`scripts/makefile_test.sh` が検証）。`make build` / `make dev` も同じ版を使う |
| `make fmt` | `templ fmt backend`、`golangci-lint fmt ./...`（`.golangci.yml` の formatter は gofmt と goimports）、Biome によるフロントエンド TypeScript の整形（`npm run format --prefix static`） |
| `make fmt-check` | `gofmt -l backend static` が空であることと `templ fmt -fail backend`（goimports は見ない）、`biome format`（`npm run format:check --prefix static`）。未整形で失敗 |
| `make lint` | `make lint-go` と `make lint-static` |
| `make lint-lines` | `scripts/linterly-baseline.sh check`。`.linterly-baseline` より悪化した行数超過があれば失敗（Linterly 節）。`linterly` と `jq` が要る |
| `make vuln` | `govulncheck ./...` を Linux ホストと Windows ターゲット（`GOOS=windows CGO_ENABLED=0 govulncheck ./...`。配布する Windows 版の `backend/cmd/wails`（`//go:build windows`）・Wails v3・Windows 実装は Linux の解析に入らないため）の両方で実行する。コードから到達できる既知の脆弱性（標準ライブラリ・依存モジュール）がどちらかにあれば失敗。`make tools` が入れる `govulncheck` が要る |
| `make linterly-baseline` | `scripts/linterly-baseline.sh update`。`.linterly-baseline` を現在の `linterly check` の結果で書き換える |
| `make lint-go` | `make generate` のあと `golangci-lint run ./...`（`.golangci.yml`。`gocyclo` 25 / `gocognit` 50 の複雑度上限を含む。「関数の複雑度」節） |
| `make lint-static` | `tsc --noEmit`（`npm run typecheck --prefix static`）と `biome lint`（`npm run lint --prefix static`） |
| `make test` | `make test-go` と `make test-static` と `make test-scripts` |
| `make test-scripts` | `bash scripts/linterly-baseline_test.sh`（行数基準値のスクリプトのテスト。`jq` が要る）、`bash scripts/slow-tests_test.sh`（遅いテストの一覧を出すスクリプトのテスト。`jq` が要る）、`bash scripts/makefile_test.sh`（`Makefile` の全ターゲットが `.PHONY` と `make help` にあるか、`.gitattributes` が `scripts/*.sh` などを LF に固定し、バイナリを `binary` にしているか）、`bash scripts/complexity-ratchet_test.sh`（複雑度の許可リストの判定。`golangci-lint` が要る）、`bash scripts/third-party-notices_test.sh`（`scripts/third-party-notices.sh` が表記ファイルを作ること、ライセンスファイルの無い依存で失敗することの検証。偽の `go` と `node` を使い、`jq` も `golangci-lint` も要らない） |
| `make test-go` | `make generate` のあと `go test -race -timeout 30m -json ./...` を `scripts/slow-tests.sh` に流す（`-race` の逐次テストが長く、既定の 10 分では負荷時に足りない）。テストの出力は `go test` と同じに出し、最後に 3 秒（`SLOW_TEST_SECONDS`）を超えたテストを遅い順に最大 20 件 stderr に一覧する。警告だけで失敗にはしない（終了コードは `go test` のもの）。`jq` が要る |
| `make test-static` | `npm test --prefix static` |
| `make build` | `scripts/build.sh`（templ → esbuild/tailwind minify → go build）。`bin/mam` を作る |
| `make build-all` | `scripts/build.sh --all`。windows / darwin / linux × amd64 / arm64 の 6 構成を `bin/mam-<os>-<arch>[.exe]`（Windows は `.exe` 付き）に出力する（`spec/requirements/non-functional.md` §1） |
| `make clean` | `bin`・`tmp`・`static/src/dist/app.*` を削除する |
| `scripts/package-windows.sh <version>` | Windows 版（Wails ウィンドウ）のパッケージング。templ → esbuild/tailwind → `wails3 package GOOS=windows`（`GOARCH=amd64`）で `bin/mam-<version>-windows-amd64.exe`（ポータブル）と `bin/mam-<version>-windows-amd64-setup.exe`（NSIS インストーラー）を作る。`<version>` は `X.Y.Z` 形式でなければ失敗する。`go`・`npm`・`wails3`・`makensis`・`jq` が要る（無いとツール確認で失敗する）。`templ` は PATH に要らず、`make generate` と同じく `go run` で go.mod と同じ版を使って生成する（Windows に `make` が無くても、古い `templ` が `*_templ.go` を書き換えない）。Windows 上（Git Bash）で動かす。`make` のターゲットは無い（Windows に `make` が無いため） |
| `scripts/third-party-notices.sh [出力先]` | `THIRD_PARTY_NOTICES.md`（既定。リポジトリ直下）を再生成する。Windows 版 exe（`backend/cmd/wails`、`GOOS=windows`、`-tags production`）にリンクされる Go モジュールと Go の標準ライブラリ、本番フロントエンドバンドル（`static/src/main.ts` が import するもの）の npm パッケージと `tailwindcss` のライセンスファイルを、そのまま写す。`go` と `node` と、済ませた `npm ci --prefix static` が要る。ライセンスファイルが無い依存があると失敗する（表記から黙って漏れない）。`go.mod` や `static/package-lock.json` を変えたら実行し直す。`make` のターゲットは無く、CI でも実行しない |

## フロントエンドの整形・lint

`static/src` の TypeScript（`*.ts`・`*.wtest.ts`・`*.test.ts`）と `static/build.mjs`・`static/web-test-runner.config.mjs` は Biome（`@biomejs/biome`。`static/package.json` に版を固定）で整形・lint する。設定は `static/biome.json`（インデント 2 スペース・シングルクォート・行幅 120・末尾カンマ。`.gitignore` と `static/src/dist` は対象外）。

- 整形: `make fmt`（`npm run format --prefix static`）で書き換え、`make fmt-check`（`npm run format:check --prefix static`）で未整形なら失敗する。
- lint: `make lint-static` が `tsc --noEmit` のあとに `biome lint` を走らせる。recommended に加えて、未使用の import / 変数（`noUnusedImports`・`noUnusedVariables`）、`==`（`noDoubleEquals`）、`void` を付け忘れた Promise（`nursery/noFloatingPromises`）をエラーにする。`!`（非 null アサーション）は `noUncheckedIndexedAccess` の下で意図して使っているため `noNonNullAssertion` は無効にしている。
- import の並べ替え（organizeImports）は使わない。
- 初回の一括整形は 1 コミットに分けてある。以降の差分に整形のノイズを混ぜない。
- 整形で行数が増えた分は `.linterly-baseline` の該当エントリに反映した（機械的な増加であり、悪化の見逃しではない）。

CI は `make fmt-check` と `make lint` を実行するため、上の検査は追加のステップ無しで CI に入る（`static/node_modules` は `make tools` の `npm ci` で用意される）。

`static/build.mjs`（`npm run build` / `npm run dev`。`make build`・`scripts/package-windows.sh` が使う）は、esbuild と Tailwind を並べて動かす。Tailwind は `npx` を介さず、`process.execPath`（`node`）で、インストール済みの `@tailwindcss/cli`（`static/package.json` の devDependency）の `bin` が指すエントリを直接起動する（`shell` は使わない。Windows の `npx` は `npx.cmd` で、`shell` 無しの `spawn` では `ENOENT`（新しい Node では `EINVAL`）になるため。`--no-install` と同じく何も取得しない）。`static/src/build.test.ts` は環境変数 `MAM_TAILWIND_CLI`（Tailwind のエントリの上書き。テスト専用）で偽の Tailwind を差し込んで失敗経路を、PATH を空にして `npx` が無くても本物の Tailwind で本番ビルドが通ることを検査し、Windows でも動かす（SIGTERM のテストだけは Windows にシグナルが無いので Windows では飛ばす）。

## CI

`.github/workflows/ci.yml`。`main` への push と全 PR で、GitHub-hosted の `ubuntu-latest`（`runs-on: ubuntu-latest`）で実行する（公開リポジトリなら GitHub-hosted runner は無料で、外部の PR が自社のサーバーで動くこともない。トリガーは `push`（`main`）と `pull_request` だけで `pull_request_target` は使わず、secrets も使わない。`permissions` は `contents: read`）。ツールは `make tools`（Makefile の版指定）で入れる。`make generate` → コミット済み `*_templ.go` の一致確認 → `fmt-check` → `lint` → 脆弱性の検査（`make vuln`。後述）→ 行数の悪化確認（`make lint-lines`。後述の Linterly 節）→ ffmpeg の確認（無ければ `sudo -n apt-get install -y ffmpeg`）→ Chrome の確認（常に固定した版の Chrome for Testing を取得して `CHROME_PATH` に渡す。`unzip` が無ければ `sudo -n apt-get install -y unzip` で入れてから取得する。取得に失敗したらそのステップで失敗する。前提は開発ツールの「Chrome / Chromium」）→ `test` → `build` の順。`make generate` の直後に `*_templ.go` が変更・未追跡になっていれば（`.templ` と食い違う古い・手編集の出力、再生成漏れ）失敗させる。`lint` / `test` / `build` は先に再生成するため、この確認がないと食い違いが緑になる（`_templ.go` は git 管理で、素の `go build` はそれをそのままコンパイルする）。`cmd/wails`（`//go:build windows`）は Linux の通常のステップに入らないため、`GOOS=windows CGO_ENABLED=0` で `go build ./backend/...`・`go vet ./backend/...`・`golangci-lint run ./backend/...` を別ステップで実行する（Windows 版の壊れを PR で検知する）。

標準ライブラリの脆弱性は Go の版で決まるため、`go.mod` の `toolchain` 行で修正版の Go（`actions/setup-go` の `go-version-file: go.mod` が CI と `release.yml` の両方で読む）を固定する。`lint` の次のステップで `make vuln`（`govulncheck ./...` を Linux ホストと `GOOS=windows CGO_ENABLED=0` の Windows ターゲットの両方で実行）を実行し、コードから到達できる既知の脆弱性（標準ライブラリ・依存モジュール）があれば失敗させる。Windows ターゲットも検査するのは、配布する exe の元になる `backend/cmd/wails`（`//go:build windows`）と Wails v3・Windows 実装が Linux の govulncheck の解析に一度も入らないため（Windows 向けの `go build` / `go vet` / `golangci-lint` を別ステップで回しているのと同じ理由）。依存の更新 PR は `.github/dependabot.yml`（`gomod`・`npm`（`/static`）・`github-actions`）が週次で作る。

ffmpeg / ffprobe は `ubuntu-latest` のイメージに入っていないため、PATH に無いときに `test` の前に apt で導入する（runner のユーザーはパスワード無しの sudo が使える。導入できなければステップが失敗する）。ffmpeg を使う Go テスト（Judge のフレーム抽出、`media`、Audio Director のミックス、Take のプレビュー・部分採用・区間指定の再生成、Continue の最終フレーム）は、PATH に無いとローカルでは `t.Skip` されるが、`CI=true`（GitHub Actions が設定する）では `backend/internal/testenv` の `MissingFFmpeg` が失敗にする。CI の導入手順が外れても緑のまま飛ばされない。ffmpeg の無い開発機では該当テストが Skip され、lefthook の pre-push（同じ `make test`）でも同様なので、実ファイルの経路を確かめるには ffmpeg を入れてから `go test -v` で SKIP が出ていないことを見る。

`actions/setup-go` / `actions/setup-node` は actions のキャッシュを使う。GitHub-hosted runner は毎回空の状態で始まるため、`setup-go` は Go のモジュール・ビルドのキャッシュを `go.sum` をキーに（`cache: true`・`cache-dependency-path: go.sum`）、`setup-node` は npm のキャッシュを `static/package-lock.json` をキーに（`cache: npm`・`cache-dependency-path: static/package-lock.json`）保存・復元する。`release.yml` の `verify`・`build` ジョブも同じ指定にする。

`ci.yml` と `release.yml` の `uses:` はどちらもコミット SHA で固定する（行末のコメントがタグ。同じ action は両ファイルで同じ SHA・コメントにそろえる）。上流のタグが付け替えられても、書き込み権限を持つジョブや配布物を作るジョブで任意コードが走らないようにする。版は dependabot の `github-actions` が SHA とコメントごと更新する。

Go テストの所要時間（`backend/internal/router`）: ffmpeg で作る素材（`testsrc` の mp4、`sine` の wav）は `testenv.FFmpegFile` / `testenv.PatternVideo` がプロセスごとに引数ごと 1 回だけ作り、各テストはそのバイト列を使う（テストごとに ffmpeg を起動しない）。DB・アセット置き場・鍵の保管庫（`credentialtest.Memory`（`backend/internal/credential/credentialtest`）。go-keyring のモックはプロセス全体で 1 つなので使わない）をテストごとに持つ `newApp` / `newEngine` / `newGenApp` / `newTakeApp` / `newJudgeApp` は `testenv.Parallel` で並列に走る。並列数は `testenv` が 8 に絞る（背景ワーカーのタイムアウト待ちが、負荷時に不安定になるのを避ける）。`t.Setenv` を使うテストは並列にできないので `newSerialApp` / `newSerialEngine` を使う。

顔同一性（YuNet / SFace・ONNX Runtime）の実モデルのテスト（`backend/internal/facesim/onnx_test.go` の `TestONNX_SamePersonIsCloserThanAnotherPerson` と `TestONNX_ImageWithoutFaceHasNoFaces`）は、CI では動かさない。`MAM_FACESIM_MODEL_DIR`（モデルのディレクトリ。`scripts/fetch-face-models.sh <DIR>` で取得する）・`MAM_FACESIM_PHOTOS`（`a.jpg` / `a_small.jpg` / `b.jpg` / `none.jpg` を置いたディレクトリ）・任意の `MAM_FACESIM_RUNTIME_LIB`（`libonnxruntime` のパス、ONNX Runtime 1.23.x）が揃ったときだけ動き、未設定だと `t.Skip` するため、CI・`make test`・pre-push では常に Skip される（ffmpeg のような `CI=true` での失敗化はしない）。写真が権利上リポジトリに置けず、モデルとランタイムの取得も重いため。`align.go` / `detect.go` / `imaging.go`（顔検出・整列・埋め込みの前処理）を変えたときは、手元で `go test -v -run TestONNX ./backend/internal/facesim/` を実行し、Skip が出ないことと類似度の値（同一人物 > 0.7、別人物 < 0.3）を確かめる。詳細は `spec/architecture/model-adapters.md` §10.1。

## リリース

`.github/workflows/release.yml`。実行の契機は 2 つ。(1) `ci.yml`（`CI`）が `main` への push で成功して完了したとき（`workflow_run`）: `main` の先端を、最新の `v*` タグの patch を 1 つ上げた版（`vX.Y.Z` → `vX.Y.(Z+1)`、タグが 1 つも無ければ `v0.1.0`）でリリースする。誰もタグを打たない。(2) `v*` タグ（`vX.Y.Z`）の手動 push: minor・major を上げるときだけ使う。全ジョブは `ci.yml` と同じ GitHub-hosted の `ubuntu-latest`（`runs-on: ubuntu-latest`）で動く。Windows 版は Linux 上でクロスビルド（`GOOS=windows`、`CGO_ENABLED=0`）し、インストーラーは Linux の `makensis` で作る（Windows の runner は使わない）。ワークフロー全体の `permissions` は `contents: read` で、書き込み権限（`contents: write`）を持つのは `release` ジョブだけ（タグも `gh release create` が作る）。`workflow_run` は同じリポジトリの `main` への push で成功した CI の完了だけを `plan` ジョブの `if:` で通す（fork の CI の完了では動かない）。ビルドの工程（`go install`・NSIS の導入・`npm ci`）は書き込み用の `GITHUB_TOKEN` を持たない環境で動く。`uses:` はコミット SHA で固定し（行末のコメントがタグ）、版は dependabot の `github-actions` が SHA ごと更新する。`concurrency`（`group: release`、`cancel-in-progress: false`）で同時に 1 本だけ動かす（次の版を既存タグから決めるため、重ならないようにする）。待ち行列は 1 件しか持たないので、待っている間に更に新しい実行が来ると古い待機は取り消される（`main` の先端以外はどのみちリリースしない）。リリースするコミット `RELEASE_SHA` は、`workflow_run` では CI が通ったコミット（`workflow_run.head_sha`）、タグの push ではタグのコミット。4 ジョブ構成（`plan` → `verify` → `build` → `release`）:

### `plan`（`ubuntu-latest`、`contents: read`）

リリースするかどうかとタグを決める。`workflow_run` は CI の実行ごとに起動するため、ジョブの `if` で、成功（`conclusion == success`）・`push` 由来・このリポジトリのブランチ（フォークの `main` を除く）に絞る。`fetch-depth: 0` で全タグと `main` の先端を取り、次のとおり決める。`release=false` のときは後続のジョブを動かさない。

- タグの push: タグが `vX.Y.Z` でなければ失敗。そのタグをリリースする
- `workflow_run`: `RELEASE_SHA` が `origin/main` の先端でなければ `release=false`（CI が遅れて終わった古いコミットが、新しいコミットより新しい版を得ないようにする。その後の実行が先端をリリースする）。`RELEASE_SHA` に既に `v*` タグが付いていれば `release=false`（手動タグでリリース済みのコミットを二重に出さない）。それ以外は、`vX.Y.Z` 形式のタグを `--sort=-v:refname` で並べた先頭の patch に 1 を足す

### `verify`（`ubuntu-latest`、`needs: plan`、`contents: read` + `actions: read`）

リリースするコミットを検証する。どれかが失敗したらビルドもリリースも走らない。

1. `git merge-base --is-ancestor "$RELEASE_SHA" origin/main`: コミットが `main` の履歴にあること（`fetch-depth: 0`）。main 以外のコミットに打ったタグは公開しない
2. CI の成功確認: `gh run list --workflow ci.yml --commit "$RELEASE_SHA"` に `success` の実行があること。CI が赤い、または未完了のコミットは公開しない（手動タグは CI が通るのを待ってから打つ）
3. `make tools` → `make generate` → `make vuln`（Linux と Windows の両方のターゲットを検査する）: CI の後に出た新しい脆弱性勧告でも止める。`setup-go` / `setup-node` は `ci.yml` と同じキャッシュ指定（`go.sum`・`static/package-lock.json`）

### `build`（`ubuntu-latest`、`needs: [plan, verify]`、`contents: read`）

1. checkout（`persist-credentials: false`）→ setup-go（`go.mod`、`go.sum` のキャッシュ）→ setup-node（24、npm のキャッシュ）。行末はリポジトリ直下の `.gitattributes`（`* text=auto eol=lf`。バイナリ（`*.png`・`*.ico`・`*.webm`・`*.exe`・`*.onnx`）は `binary`）で LF に固定している（Windows の git は `core.autocrlf=true` が既定で `scripts/*.sh` を CRLF にし、bash が `set: pipefail\r: invalid option name` で止まるため。Linux の runner でも `//go:embed` する `migrations/*.sql` の行末が変わらないよう、この指定を保つ）
2. `wails3`（`go.mod` の版）: `CGO_ENABLED=0 go install`。`templ` は入れない（`scripts/package-windows.sh` が `go run` で `go.mod` の版を使う）
3. NSIS（`makensis`）: PATH に無ければ導入する。passwordless sudo があれば `apt-get install nsis`。無ければ root 不要の方法として、`apt-get download nsis nsis-common` を `dpkg -x` でユーザーのキャッシュ（`${XDG_CACHE_HOME:-$HOME/.cache}/mam-release/nsis`）へ展開し、`NSISDIR`（スタブは `nsis-common` にある）を設定する `makensis` ラッパーを PATH に足す。キャッシュは実行をまたいで残る。版はディストリビューションのもの（固定しない）。導入できなければ失敗
4. `bash scripts/package-windows.sh "${TAG#v}"`（`TAG` は `plan` が決めたタグ）
5. `actions/upload-artifact`（名前 `windows-release`）で 2 つの exe を `release` ジョブへ渡す

Windows 上で動かす検査（`fsutil` の `LockFileEx`・`GetDiskFreeSpaceEx` の実装や `GOOS=windows` 分岐のテスト、子プロセスのコンソール非表示のテスト、フロントエンドの Windows 上のビルド、インストーラーのスモーク `scripts/smoke-windows.sh`）は、Windows の runner が無いためリリースでは動かさない。`smoke-windows.sh` は Windows の機械で手動実行する用に残す（`bash scripts/smoke-windows.sh bin/mam-<version>-windows-amd64-setup.exe`）。

### `release`（`ubuntu-latest`、`needs: [plan, build]`、`contents: write`）

checkout はせず（`GH_REPO` を渡す）、`actions/download-artifact` で受け取った exe を使って `gh release create "$TAG" --title "$TAG" --generate-notes --target "$RELEASE_SHA" dist/mam-<version>-windows-amd64-setup.exe dist/mam-<version>-windows-amd64.exe` を実行するだけ。`--target` はタグが無ければそのコミットにタグを作り、手動タグの push ならそのタグをそのまま使う。

成果物はこの 2 つだけ（`spec/requirements/non-functional.md` §1）。コード署名はしない（署名の手順を足す場合は別 Issue）。リリースノートは GitHub の自動生成に任せる。タグはビルドまで通った後の `release` ジョブでだけ作るので、途中で失敗してもタグは残らず、`main` に次の push があれば同じ版で作り直される。同じタグのリリースが既にあれば `gh release create` が失敗するため、手動タグで作り直すときはタグとリリースを削除してから push し直す。`CI` が失敗・取り消しになった push は、`plan` ジョブごとスキップされる（Actions の一覧には Skipped の Release 実行が並ぶ）。

## Linterly

`.linterly.yml`。行数上限はデフォルト（1 ファイル 300 行 / 1 ディレクトリ 2000 行）のまま維持し、特別な理由がない限り変えない。除外は生成物（`*_templ.go`、`static/src/dist/**`、`package-lock.json`）とライセンス全文（`LICENSE`、`THIRD_PARTY_NOTICES.md`）のみで、手書きソースは除外しない。

既存の超過（linterly の `error`: 上限に `warning_threshold` を足した値を超えたもの。`linterly check` が exit 1 になる判定と同じ）は、超過ゼロを目標にせず、**増やさないことを CI で保証する**。超過しているファイルとディレクトリの行数を `.linterly-baseline`（`<行数><TAB><パス>`、ディレクトリは末尾 `/`）に基準値として記録し、`scripts/linterly-baseline.sh check`（`make lint-lines`、CI の「Line limit (no regression)」ステップ）が次のどちらかで失敗させる。

- 基準値に無いファイル・ディレクトリが上限を超えた（新規の超過）
- 基準値にあるファイル・ディレクトリの行数が基準値より増えた

基準値より減った・上限以下に戻ったエントリは失敗にせず、「基準値を下げられる」と stderr に出す。分割して減らしたら `make linterly-baseline`（`scripts/linterly-baseline.sh update`）で `.linterly-baseline` を書き換えて同じ PR でコミットする。差分は行の削除か数値の減少だけであること（増えている差分は悪化を基準値に紛れ込ませているのでレビューで止める）。基準値に残っているファイルの分割は別 Issue で段階的に進める。`scripts/linterly-baseline_test.sh`（`make test-scripts`）がこの判定をスタブの `linterly` で検査する。

ディレクトリ上限（2000 行）も同じ基準値方式で扱い、Go のフラットなパッケージ向けの別基準（ファイル数など）は設けない。超過しているパッケージは、その直下の合計行数を増やせない（ファイルの追加・既存ファイルの行増で CI が落ちる）。新しいコードは新しいパッケージへ置くか、パッケージを責務ごとに分割して満たす。分割できたら基準値から外す。

仕様書（Markdown）も計測対象で、同じ基準で扱う。改訂履歴の追記で本文が基準値や上限を超えそうなときは、基準値を上げず、改訂履歴の表を `<spec>.changelog.md`（例: `spec/architecture/overview.changelog.md`）へ移し、本文には参照だけを残す（`overview.md`・`endpoints.md`・`er.md`・`model-adapters.md` は分割済み。新しい版はそれぞれの `*.changelog.md` の末尾に足す）。

pre-commit（`scripts/linterly-staged.sh`）は従来どおり、コミット対象のファイルが超過していれば基準値の有無に関わらず止める。

## 関数の複雑度

`.golangci.yml` の `gocyclo`（循環的複雑度 25 超）と `gocognit`（認知的複雑度 50 超）が、関数単位の複雑度の悪化を `make lint-go`（`make lint`・pre-commit・CI の `Lint` ステップ）で止める。テストコード（`_test.go`）は対象外。

既存の超過関数は `linters.exclusions.rules` で 1 関数ずつ許可している（`//nolint` は使わない）。各エントリは `path`（関数のあるファイル）と `text`（`cyclomatic complexity (<上限+1>..<現在値>) of func \`<関数名>\` ` の形で、複雑度は上限の次の値から現在値までを正規表現で列挙する）の AND で、`linters` も `gocyclo` / `gocognit` のどちらか 1 つに絞る。このため、許可済みの関数がさらに複雑になる（現在値を超える）、同名の関数が別のファイルにある、許可リストに無い関数が上限を超える、のいずれも lint エラーになり、新規の超過と悪化の両方が止まる。関数を分割して複雑度が下がる分には、エントリはそのまま通る。上限以内に戻ったときは golangci-lint が消し忘れを知らせないため、同じ PR で許可エントリを削除する（レビューで確認する）。`scripts/complexity-ratchet_test.sh`（`make test-scripts`）が、許可済みの関数の現在値は通り、現在値を超える・同名の別ファイルの関数・新しい関数は失敗することを、使い捨てのモジュールで検査する。許可リストが空になったら閾値を `gocyclo` 20 / `gocognit` 40 に下げる（別 Issue）。

## Git Hooks

`lefthook.yml`（`lefthook install` で有効化）。

- pre-commit: コミット対象ファイルの行数（`scripts/linterly-staged.sh`。`linterly check` は 1 パスしか受け取れないため全体結果をコミット対象に絞る）、`make fmt-check`（`*.go` / `*.templ` / `*.ts` / `*.mjs`）、`make lint-go`、`make lint-static`（`static/**/*.ts` / `static/*.mjs` がある場合。`tsc` と Biome lint）
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
| 1.12 | 2026-10-12 | 「関数の複雑度」節を、許可エントリが `path` と複雑度の範囲まで固定し、悪化・同名の別関数も止める記述に改め、`make test-scripts` に `scripts/complexity-ratchet_test.sh` と `scripts/makefile_test.sh` を加えた。`make lint-go` / `lint-static` / `clean` の `##` 説明と `.PHONY` の `build-all` を足した | Issue #537・#540・#544。許可リストが関数名だけで一致し、許可済みの関数の悪化も同名の別関数も通していた。`make help` に出ないターゲットと `.PHONY` の抜けもあった |
| 1.13 | 2026-10-12 | 「Linterly」節に、仕様書（Markdown）も行数の計測対象であり、改訂履歴の追記で基準値・上限を超えそうなときは基準値を上げず改訂履歴の表を `<spec>.changelog.md` へ移すことを追記した | Issue #546。#535・#536・#545 の改訂履歴の追記で `spec/architecture/overview.md` が基準値に無いまま 337 行になり、`endpoints.md`・`er.md` も基準値を 1 行ずつ超えて、CI の「Line limit (no regression)」が落ちていた |
| 1.14 | 2026-10-12 | `make test-go` を `go test -json` + `scripts/slow-tests.sh` にし、3 秒を超えたテストを一覧に出すようにした。CI 節に `router` のテストの素材共有と並列化を追記 | Issue #558。`router` の 215 本が直列で 226〜350 秒かかり、ffmpeg で同じ素材をテストごとに作っていた |
| 1.15 | 2026-10-12 | 「Go テストの所要時間」の段落で、router テストの鍵の保管庫を `memStore` から `credentialtest.Memory`（`backend/internal/credential/credentialtest`）に直した | Issue #559。`backend/internal/router` に `memStore` は無く、実装は #558 で入れた `credentialtest.Memory` だった |
| 1.16 | 2026-10-12 | CI（`ci.yml`）の実行先を `ubuntu-latest` から組織の self-hosted runner（`[self-hosted, Linux, X64]`）に変え、ffmpeg の導入を「無いときだけ `sudo -n apt-get`」に改めた。Chrome・ffmpeg は runner のホストのものを使い、Linux 版 Chrome が無い runner では `Ensure Chrome` ステップが Chrome for Testing を取得し、`setup-go` / `setup-node` は actions のキャッシュを使わないと CI 節・開発ツールに明記 | GitHub-hosted runner の課金停止で CI ジョブが開始されず、PR #707 の CI が確認できなかった。組織には online の self-hosted runner が 2 台あり、うち `silent` は Windows の chrome.exe しか見えず `make test-static` が起動に失敗し、`setup-go` のキャッシュ保存が止まった |
| 1.17 | 2026-10-15 | `jq` の用途に `scripts/package-windows.sh`（`build/windows/info.json` へのバージョン書き込み）を加え、Windows 版パッケージングの前提（開発ツール・タスク表の `scripts/package-windows.sh`）に `jq` を明記した。README の前提も同様に直した | Issue #724。`package-windows.sh` は `jq` を必須としツール確認で失敗するのに、仕様書は `jq` の用途を pre-commit・行数基準値のスクリプトに限り、Windows 版パッケージングの前提を `wails3` と NSIS だけとしていた（リリースの CI は runner に `jq` があるため表に出ていなかった） |
| 1.18 | 2026-10-15 | CI 節に、`ci.yml` も `release.yml` と同じく `uses:` をコミット SHA で固定する方針を追記し、`ci.yml` の `actions/checkout` / `setup-go` / `setup-node` を `release.yml` と同じ SHA に固定した | Issue #722。`ci.yml` だけ浮動タグ（`@v4` / `@v5`）のままで、self-hosted runner（ホストにキャッシュが残る）上でタグの付け替えによる任意コード実行を許していた |
| 1.19 | 2026-10-15 | CI の `Ensure Chrome` ステップを、`npx --yes @puppeteer/browsers install chrome@stable` から、`static/package.json` の devDependency に固定した `@puppeteer/browsers`（`static/node_modules/.bin/browsers`）と `CHROME_VERSION` で固定した Chrome for Testing の取得に改め、「Chrome / Chromium」に版固定の方針を追記した | Issue #723。`@puppeteer/browsers` も `chrome@stable` も版が固定されておらず、`--yes` で未検証のパッケージを取得して self-hosted runner 上で実行していた。`stable` が進むと同じコミットで Lit コンポーネントテストの結果が変わりえた |
| 1.20 | 2026-10-15 | 開発ツールの「Chrome / Chromium」と CI 節に、`Ensure Chrome` が Chrome for Testing の zip 展開に `unzip` を必要とすることを明記し、`ci.yml` の `Ensure Chrome` に、`unzip` が無いときだけ `sudo -n apt-get install` で入れる確認を足した（ffmpeg と同じ扱い） | Issue #726。`browsers install` は `unzip`（か任意依存の `yauzl`）が無いと `Extraction failed: no zip archiver is available` で失敗するが、`ci.yml` も仕様書も `unzip` の前提を確認・記載していなかった。Linux 版 Chrome も `unzip` も無い最小構成の runner で、このステップだけが分かりにくく失敗した |
| 1.21 | 2026-10-16 | CI の `Ensure Chrome` ステップに `shell: bash`（`bash -eo pipefail`）を指定し、取得した Chrome のパスが空・実行不可なら `::error::` を出して失敗させるようにした。開発ツールの「Chrome / Chromium」と CI 節に「取得に失敗したらステップが失敗する」ことを明記した | Issue #728。`ci.yml` の `Ensure Chrome` は `shell:` 未指定（既定は `bash -e` で `pipefail` なし）のため、`browsers install \| tail \| cut` の終了コードが `cut` の 0 になり、取得に失敗してもステップが成功して空の `CHROME_PATH=` を `$GITHUB_ENV` に書いていた。失敗が後続の `Test` で別の症状として現れ、原因が読めなかった |
| 1.22 | 2026-10-16 | 「フロントエンドの整形・lint」節の後に、`static/build.mjs` が Tailwind を `npx` ではなく `node` でインストール済みの `@tailwindcss/cli` のエントリから直接起動することを追記し、`release.yml` の `build` ジョブの手順に、パッケージの前に Windows 上でフロントエンドのビルドと `build.test.ts` を動かす手順（手順 3）を足した（以降の手順は繰り下げ） | Issue #765。`build.mjs` は `spawn('npx', …)`（`shell` 無し）で Tailwind を起動しており、Windows の `npx.cmd` を起動できず `build failed: spawn npx ENOENT` で `scripts/package-windows.sh` が止まり、リリースの `build` ジョブ（`windows-latest`）が成功しなかった。CI は Linux だけで、`build.test.ts` の Tailwind 異常系も Windows では `skipIf` で飛ばしていたため、この経路は一度も検証されていなかった |
| 1.23 | 2026-10-16 | `make vuln` を Linux ホストに加えて Windows ターゲット（`GOOS=windows CGO_ENABLED=0 govulncheck ./...`）でも実行するようにし、タスク表・CI 節・リリース節 `verify` の手順 3 に「Linux と Windows の両方のターゲットを検査する」ことを明記した。`scripts/makefile_test.sh` に `make vuln` が両方を実行する検査を足した | Issue #767。`make vuln` は `GOOS` を指定せず Linux でだけ走り、配布する Windows 版の `backend/cmd/wails`（`//go:build windows`）と Wails v3・`fsutil` の Windows 実装・標準ライブラリの Windows 実装が検査の対象外だった（1.8 の「CI と配布する Windows バイナリ」を守るという記述と実装がずれていた）。Windows ターゲットでも現時点で脆弱性は無く、見逃しではなく網羅範囲のずれ |
| 1.24 | 2026-10-16 | `release.yml` の `build` ジョブの手順に、パッケージの前に Windows 上で `fsutil`・`desktop`・`app`・`config` の Go テストを動かす手順（手順 4）を足した（以降の手順は繰り下げ）。ffmpeg を要するパッケージは対象外とし、その理由を明記した | Issue #771。`fsutil` の Windows 実装（`LockFileEx`・`GetDiskFreeSpaceEx`）と `runtime.GOOS == "windows"` で分岐するテストは、CI（Linux だけ。Windows はビルド・vet・lint まで）でもリリースの `build` ジョブ（スモークは起動時のロック取得だけ）でも `go test` されず、`FreeBytes` の Windows 経路や 2 回目のロック失敗が検証されていなかった（#767 と同種の網羅範囲のずれ） |
| 1.25 | 2026-10-16 | `scripts/package-windows.sh` が `templ generate` を PATH 上の `templ` ではなく `go run github.com/a-h/templ/cmd/templ@<go.mod の版>` で実行するようにし、`templ` を同スクリプトの前提ツール（タスク表・リリース節の「ツール」・`release.yml` の `Install tools`）から外した。`scripts/makefile_test.sh` に同スクリプトの検査を足した | Issue #768。`Makefile` は PATH の `templ` を使わない方針（古い `templ` が go.mod とずれた `*_templ.go` を書き CI の鮮度確認が落ちる）なのに、`make` の無い Windows で使う `package-windows.sh` だけ版を検証せず PATH の `templ` を使い、`wails3` だけ版を検証していた |
| 1.26 | 2026-10-16 | Git Hooks 節の pre-commit の対象を `*.mjs`（`make fmt-check`）・`static/*.mjs`（`make lint-static`）まで広げ、`lefthook.yml` の glob も同じにした | Issue #773。Biome の対象（`static/biome.json`）と CI の `make fmt-check` は `static/build.mjs`・`static/web-test-runner.config.mjs` を見るのに、lefthook の glob が `*.ts` だけで、`.mjs` だけを変えたコミットでは整形・lint が走らず push 後の CI まで検出されなかった |
| 1.27 | 2026-10-16 | `release.yml` の `build` ジョブの手順に、パッケージの前に Windows 上で `internal/media` の `TestHideWindowSetsNoConsoleFlags` だけを `-run` で動かす手順（手順 5）を足した（以降の手順は繰り下げ）。手順 4 の「ffmpeg を要するパッケージは含めない」を「丸ごとは含めない」に直した | Issue #774。#770 の Windows 専用テスト `run_windows_test.go` は、`ci.yml`（Linux だけ）でも手順 4 の対象（`media` が入っていない）でもどこでも実行されず、`run` が `hideWindow` を呼ばなくなる退行を検出できなかった。このテストは ffmpeg を起動しないので、`testenv.MissingFFmpeg` に掛からないよう `-run` で絞れば `build` ジョブで動かせる |
| 1.28 | 2026-10-16 | リポジトリ直下に `.gitattributes`（`* text=auto eol=lf`、`*.png`・`*.ico`・`*.webm`・`*.exe`・`*.onnx` は `binary`）を追加し、リリース節 `build` ジョブの手順 1 に、行末を LF に固定している理由（Windows runner の `core.autocrlf=true` で bash スクリプトが壊れる）を明記した。`scripts/makefile_test.sh` に、`scripts/*.sh`・`migrations/*.sql`・`Makefile` が LF に固定され、リポジトリのバイナリファイルが `binary` として明示されている検査を足した | Issue #776。`.gitattributes` が無く、`windows-latest` の checkout が `core.autocrlf=true` で `scripts/package-windows.sh` / `scripts/smoke-windows.sh` を CRLF にし、タグを打つたびに「Package」ステップが `set: pipefail\r: invalid option name` で失敗してリリースが作れなかった（Linux / macOS・CI では再現しないため未検出だった） |
| 1.29 | 2026-10-16 | 手順 5 の Windows 上のテストに `TestRunHidesConsoleWindow` を足し、`internal/media` の `run` の `exec.Cmd` 生成を差し込める変数 `commandContext` にした。手順 5 の説明を、`hideWindow` 単体の検査と `run` 経由の検査に分けて書き直した | Issue #777。`TestHideWindowSetsNoConsoleFlags` は `hideWindow` を直接呼ぶだけで `run` を通らず、`run` から `hideWindow(cmd)` を消しても通るのに、手順 5 は「`run` の退行を検出する」と述べていた。`run` を実 exe で通して作られた `exec.Cmd` を検査するテストを足して記述を実態に合わせた |
| 1.30 | 2026-10-21 | リリース節を、`main` への push で `ci.yml` が成功した後に自動で次の patch 版をリリースする（`workflow_run`）構成に書き換え、リリースする版とコミットを決める `plan` ジョブを足した（3 → 4 ジョブ）。手動の `v*` タグは minor・major を上げる用に残した。`concurrency` で直列化し、`main` の先端以外・タグ済みのコミットはリリースしない | `release.yml` が一度も動いておらず、リリースのたびにタグを手で打つ運用だったため。`main` に入れるだけで配布物が出るようにする |
| 1.31 | 2026-10-21 | リリースの全ジョブを self-hosted の Linux runner へ移し、Windows 版は Linux 上のクロスビルド + `makensis` で作る構成にした。`build` ジョブから Windows 上のテスト・フロントエンドの Windows 上のビルド・インストーラーのスモークを外し（`smoke-windows.sh` は手動用に残す）、`wails3` を `CGO_ENABLED=0` で入れ、NSIS を無ければ自動で導入する（sudo があれば apt、無ければ root 不要で展開）手順を足した。依存ツールの節の NSIS の入れ方も改めた | 初回のリリース実行（PR #820 のマージ後）が、GitHub-hosted runner の課金の支払い失敗で `plan` ジョブから起動できず失敗したため。Linux 上で `scripts/package-windows.sh` がインストーラー・単体 exe まで通ることを確認済み |
| 1.32 | 2026-10-21 | ffmpeg の節の API の参照先に `spec/api/endpoints-api.md` を加えた | `endpoints.md`・`model-adapters.md` が行数上限（300 行）を超え、後半を別ファイルに分けたため（`spec/api/endpoints.changelog.md` 1.149、`spec/architecture/model-adapters.changelog.md` 1.46） |
| 1.33 | 2026-10-24 | CI（`ci.yml`）と `release.yml` の全ジョブの実行先を、組織の self-hosted runner から GitHub-hosted の `ubuntu-latest` へ戻した（Windows 版は引き続き Linux 上のクロスビルド）。`setup-go`（`go.sum`）・`setup-node`（`static/package-lock.json`）の actions キャッシュを有効にし、`Ensure Chrome` を「ホストの Chrome が無いときだけ取得」から「常に固定版の Chrome for Testing を取得」に改め、ffmpeg は `ubuntu-latest` に無いため apt で導入する旨に直した。CI 節に `pull_request_target`・secrets を使わないことと、`workflow_run` が同一リポジトリの `main` の成功時だけ動くことを明記した | Issue #825。リポジトリを公開する準備として、公開リポジトリなら無料の GitHub-hosted runner に移し、外部の PR が自社のサーバーで動かないようにするため（1.16・1.31 は GitHub-hosted の課金停止で self-hosted へ移していた） |
| 1.34 | 2026-10-24 | リポジトリ直下に `LICENSE`（AGPL-3.0）と `THIRD_PARTY_NOTICES.md` を足し、`scripts/third-party-notices.sh`（`THIRD_PARTY_NOTICES.md` の再生成）とそのテスト `scripts/third-party-notices_test.sh`（`make test-scripts` に入れた）をタスク表に足した。Linterly の除外に `LICENSE` と `THIRD_PARTY_NOTICES.md` を足した。「Chrome / Chromium」の記述を、GitHub-hosted の runner は毎回空の状態で始まるため Chrome for Testing を実行のたびに取得する（実行の間には残らない）と改めた | Issue #826。公開に向けてライセンスを AGPL-3.0 にし、サードパーティのライセンス表記を足したため。`LICENSE` と生成物の `THIRD_PARTY_NOTICES.md` は行数上限の対象にしない。Chrome の記述は「ツールキャッシュ」が実行間で残るように読めたため直した（Issue #825 の見直し） |
