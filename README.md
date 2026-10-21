# MAM

AI Video Production Agent。企画・脚本・演出・撮影・品質判定・撮り直し・編集・音声・最終 QC までを AI が自律的に進める、ローカル動作の AI Production Studio です。サーバーは自分の PC の `127.0.0.1` だけで待ち受け、画面は既定ブラウザ、または Windows ではネイティブウィンドウで開きます。

仕様は [`spec/`](spec/overview.md) にあります。

## Windows でインストールする

[GitHub Releases](https://github.com/ousiassllc/mam/releases) の最新版から、次のどちらかをダウンロードします（Windows 10 / 11、x64）。

| ファイル | 内容 |
|---|---|
| `mam-<version>-windows-amd64-setup.exe` | インストーラー。Start メニューとデスクトップにショートカットを作ります。アンインストールは「設定 > アプリ」から行います |
| `mam-<version>-windows-amd64.exe` | ポータブル版。インストール不要で、置いた場所からそのまま起動できます |

インストーラーはユーザー単位のインストールです。`%LocalAppData%\Programs\MAM` に入れるため、管理者権限の確認（UAC）は出ません。WebView2 ランタイムが無い Windows 10 では、インストール中にダウンロードします（インターネット接続が必要）。ポータブル版も WebView2 ランタイムを使います（Windows 11 と最近の Windows 10 には入っています）。

インストーラー版は自動で更新されます。起動時と 6 時間ごとに新しい版を確認し、見つかるとダウンロードして検証し（SHA-256 と、リリースに添えた ed25519 署名）、アプリを終了してインストーラーを静かに実行し、更新後に自動で起動し直します。新しい版の配布物は、アプリが確認する公開リポジトリ [`ousiassllc/mam-releases`](https://github.com/ousiassllc/mam-releases) にも載ります。ポータブル版は自動では更新しません。v0.1.x の管理者権限のインストール（`C:\Program Files\MAM`）は自動更新に移行しません。「設定 > アプリ」からアンインストールしてから、新しいインストーラーを入れてください。

起動するとウィンドウが開きます。ウィンドウを閉じるとアプリ全体が終了します。すでに起動中のときは、新しいウィンドウは開かず、先のウィンドウが前面に出ます。

プロジェクト・設定・生成物は `%AppData%\mam` に保存します。アンインストールしても消えません。外部モデルの API キーは、プロジェクト一覧の「設定（API キー・モデル）」から登録します（OS のセキュアストレージに保存され、`%AppData%\mam` には書きません）。

### SmartScreen の警告が出たとき

配布物にはコード署名をしていないため、初めて実行すると Windows SmartScreen が「Windows によって PC が保護されました」と警告することがあります。

1. 警告の画面で「詳細情報」をクリックします
2. 「実行」をクリックします

ダウンロードしたファイルが Releases のものか不安なときは、実行前にファイルを右クリックして「プロパティ」を開き、全般タブ下部の「許可する」にチェックを入れて「OK」を押しても、警告は出なくなります。

### ffmpeg を PATH に入れる

ffmpeg は同梱していません。無くても起動はできますが、Judge（品質判定）、動画のプレビュー、部分採用の合成、Export は ffmpeg と ffprobe を使うので、使うなら入れてください。

1. PowerShell で `winget install Gyan.FFmpeg` を実行します（または [ffmpeg.org](https://ffmpeg.org/download.html) から Windows ビルドを展開します）
2. `ffmpeg.exe` と `ffprobe.exe` があるフォルダを環境変数 `PATH` に追加します（winget で入れたときは自動で追加されます）
3. MAM を起動し直します（PATH はプロセスの起動時に読むため、起動中の MAM には反映されません）
4. 確認: PowerShell で `ffmpeg -version` と `ffprobe -version` が動けば使えます

## macOS / Linux、ブラウザで使う

Windows 以外、および Windows でもブラウザで使いたいときは、ソースからビルドします（Go と Node.js 24 が要ります）。

```sh
make tools    # templ・air・golangci-lint・linterly・govulncheck と npm 依存を入れる
make build    # bin/mam を作る
bin/mam       # サーバーを起動して既定ブラウザを開く
```

`bin/mam --help` で `--data-dir`・`--port`・`--no-browser` などの設定を表示します（それぞれ `MAM_DATA_DIR` などの環境変数でも指定できます）。`make build-all` は Windows / macOS / Linux × amd64 / arm64 の 6 構成を `bin/` に作ります。

## 開発

```sh
make dev          # air + templ --watch + esbuild/tailwind --watch
make test         # Go・front-end・スクリプトのテスト
make lint         # golangci-lint・tsc・Biome lint
make lint-lines   # 行数上限の超過が .linterly-baseline より悪化していないか
make vuln         # govulncheck: 到達できる既知の脆弱性（標準ライブラリ・依存）が無いか（Linux と Windows の両ターゲット）
make fmt          # 整形（Go・templ・TypeScript）
```

Windows 版（Wails ウィンドウ + ユーザー単位の NSIS インストーラー）は `scripts/package-windows.sh <version>`（`X.Y.Z`）で作ります。`wails3`（`go.mod` の `github.com/wailsapp/wails/v3` と同じ版）と NSIS（`makensis`）と `jq` が要ります。環境変数 `RELEASE_SIGNING_PUBLIC_KEY`（更新の署名検証用の公開鍵。base64）を渡すとバイナリに埋め込みます（空なら警告して埋め込まず、アプリは署名検証を省きます）。リリースは、`main` への push で CI が成功すると GitHub Actions（`.github/workflows/release.yml`）が最新タグの patch を 1 つ上げた版で自動で作ります。インストーラーの `checksums.txt` に署名（シークレット `RELEASE_SIGNING_KEY`）して、このリポジトリのリリースと、公開リポジトリ `ousiassllc/mam-releases`（シークレット `RELEASES_TOKEN`。アプリの自動更新が確認する）の両方に載せます。minor・major を上げるときだけ `v<version>` タグを手で push します。詳しくは [`spec/architecture/desktop.md`](spec/architecture/desktop.md) と [`spec/environment/setup.md`](spec/environment/setup.md) を参照してください。
