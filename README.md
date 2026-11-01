# MAM

AI Video Production Agent。企画・脚本・演出・撮影・品質判定・撮り直し・編集・音声・最終 QC までを AI が自律的に進める、ローカル動作の AI Production Studio です。サーバーは自分の PC の `127.0.0.1` だけで待ち受け、画面は既定ブラウザ、または Windows・macOS（Apple Silicon）ではネイティブウィンドウで開きます。

仕様は [`spec/`](spec/overview.md) にあります。

## Windows でインストールする

[GitHub Releases](https://github.com/ousiassllc/mam/releases) の最新版から、次のどちらかをダウンロードします（Windows 10 / 11、x64）。

| ファイル | 内容 |
|---|---|
| `mam-<version>-windows-amd64-setup.exe` | インストーラー。Start メニューとデスクトップにショートカットを作ります。アンインストールは「設定 > アプリ」から行います |
| `mam-<version>-windows-amd64.exe` | ポータブル版。インストール不要で、置いた場所からそのまま起動できます |

インストーラーはユーザー単位のインストールです。`%LocalAppData%\Programs\MAM` に入れるため、管理者権限の確認（UAC）は出ません。WebView2 ランタイムが無い Windows 10 では、インストール中にダウンロードします（インターネット接続が必要）。ポータブル版も WebView2 ランタイムを使います（Windows 11 と最近の Windows 10 には入っています）。

インストーラー版は自動で更新されます。起動時と 6 時間ごとに、この [GitHub Releases](https://github.com/ousiassllc/mam/releases) に新しい版が無いか確認し、見つかるとダウンロードして検証し（SHA-256 と、リリースに添えた ed25519 署名 `checksums.txt.sig`）、アプリを終了してインストーラーを静かに実行し、更新後に自動で起動し直します。この ed25519 署名は更新の改ざんを検出するためのもので、Authenticode のコード署名ではありません（下の SmartScreen の警告は出ます）。ポータブル版も同じように更新を確認し、新しい版が見つかると、ユーザー単位のインストール版に切り替わります（ポータブルの exe 自体は書き換えません）。v0.1.0・v0.1.1 は自動更新を持たず、チェックサムも署名も付いていないので、自動では更新されません。署名付きの最初のリリースを手動でインストールしてください（以後は自動で更新されます）。v0.1.x の管理者権限のインストール（`C:\Program Files\MAM`）は自動更新に移行しません。「設定 > アプリ」からアンインストールしてから、新しいインストーラーを入れてください。

起動するとウィンドウが開きます。ウィンドウを閉じるとアプリ全体が終了します。すでに起動中のときは、新しいウィンドウは開かず、先のウィンドウが前面に出ます。

プロジェクト・設定・生成物は `%AppData%\mam` に保存します。アンインストールしても消えません。外部モデルの API キーは、プロジェクト一覧の「設定」カードから登録します（OS のセキュアストレージに保存され、`%AppData%\mam` には書きません）。

### SmartScreen の警告が出たとき

配布物には Authenticode のコード署名をしていないため、初めて実行すると Windows SmartScreen が「Windows によって PC が保護されました」と警告することがあります。

1. 警告の画面で「詳細情報」をクリックします
2. 「実行」をクリックします

ダウンロードしたファイルが Releases のものか不安なときは、実行前にファイルを右クリックして「プロパティ」を開き、全般タブ下部の「許可する」にチェックを入れて「OK」を押しても、警告は出なくなります。

### ChatGPT でログインすると「あなたの組織では、このアプリによるアクセスが許可されていません」と出るとき

ChatGPT Business / Enterprise / Edu のワークスペースアカウントでは、「Sign in with ChatGPT」で他のアプリからプランを使えません（OpenAI の仕様で、プランを使えるのは個人の Go / Plus / Pro だけです）。組織の Admin Console の「外部アクセス」にも、これを許可する設定はありません。

1. 設定画面の「ChatGPT でログイン」で、方式を「Codex 経由」に切り替えます
2. 初回は Codex の実行ファイルをダウンロードします（約 80〜90 MB。Windows 版は `mam.exe` と同じ場所の `codex` フォルダに、macOS 版（Apple Silicon）は `~/Library/Application Support/mam/codex` に置きます。`MAM.app` の中には置かないので、自動更新で消えません。Windows 版と macOS 版だけです。ほかの環境では自分で入れた `codex` のパスを指定します）
3. 「ブラウザでログイン」か「デバイスコードでログイン」で、組織のアカウントにログインします
4. 「LLM の割り当て」で、使う Agent のプロバイダーを `chatgpt-codex` にします

使うには、ワークスペースの所有者が「Codex をローカルで使う」権限（Codex Local）をメンバーに許可している必要があります。デバイスコードでログインするには、デバイスコードのログインも許可されている必要があります。この方式はワークスペースの Codex の利用上限を使います。また、ローカル・オープンソースでの利用のためのもので、MAM をサーバーで動かして他の人に提供する用途には使えません。テキスト生成だけで、画像・動画・音声の生成には使いません。

Codex のログイン情報は、MAM 専用の `CODEX_HOME`（Windows は `%AppData%\mam\codex-home`、macOS は `~/Library/Application Support/mam/codex-home`。あなたの `~/.codex` とは混ざりません）と、OS の資格情報の保管庫（Windows の資格情報マネージャー、macOS のキーチェーン）に Codex が保存します。MAM は読みません。アンインストールしても、`%AppData%\mam` と資格情報マネージャーの項目は残る場合があります（アンインストールの動作は Windows 実機では未確認です）。アンインストールの前に、設定画面で「ログアウト」（「取得した Codex 本体と mam 専用の CODEX_HOME も削除する」にチェック）をしてください。

### ffmpeg を入れる

ffmpeg は同梱していません。無くても起動はできますが、Judge（品質判定）、動画のプレビュー、部分採用の合成、Export は ffmpeg と ffprobe を使うので、使うなら入れてください。**MAM がボタン 1 つでダウンロードして入れられます**。

1. MAM の設定画面の「ffmpeg」（初回ガイドの「ffmpeg の確認」でも同じです）で「ffmpeg をダウンロードして入れる」を押します。約 110 MB（展開後は約 200 MB）を、配布元の [gyan.dev](https://www.gyan.dev/ffmpeg/builds/) のビルド（GitHub Releases の固定の版。mam が決めた SHA-256 と一致したものだけを使い、一致しなければ削除します）から取得し、`mam.exe` と同じ場所の `ffmpeg` フォルダに置きます（アンインストールで消えます）。ボタンを押したときだけ通信し、自動の更新確認はしません。ライセンスは GPL v3 です（ソースは配布元にあります。[`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) も見てください）
2. 「ffmpeg を入れました」と出たら、MAM を起動し直します（PATH などの探索は起動時に一度だけ行うため、起動中の MAM には反映されません）
3. 確認: 設定画面の「ffmpeg」に「使えます」と場所が出れば使えます

自分で入れたい人は、これまでどおりにできます。PowerShell で `winget install Gyan.FFmpeg` を実行する（または [ffmpeg.org](https://ffmpeg.org/download.html) から Windows ビルドを展開して、`ffmpeg.exe` と `ffprobe.exe` があるフォルダを環境変数 `PATH` に追加する）か、設定画面の「ffmpeg」でパスを指定します。**自分で入れた ffmpeg（設定したパス・`PATH`）は、MAM がダウンロードしたものより優先して使います。** MAM がダウンロードしたものは、ほかに見つからないときに使います。

## macOS（Apple Silicon）でインストールする

[GitHub Releases](https://github.com/ousiassllc/mam/releases) の最新版から `mam-<version>-darwin-arm64.dmg` をダウンロードします（macOS 12 以降、Apple Silicon（M1 以降）だけ。Intel Mac 用はありません）。

1. dmg を開き、`MAM.app` を `/Applications` にドラッグします。**`/Applications` に置いてください**（ディスクイメージの中や「ダウンロード」フォルダのままでは、自動更新が働きません）
2. 初回だけ、Gatekeeper が止めます。MAM は Apple の Developer ID による署名・公証をしていないためです。`/Applications` の `MAM.app` を**右クリック（control + クリック）→「開く」**を選び、確認のダイアログで「開く」を押します（2 回目以降は普通に開けます）。macOS 15 以降で「開く」が出ないときは、いったん開こうとしてから「システム設定 > プライバシーとセキュリティ」の「このまま開く」を押します
3. ターミナルで済ませるときは、隔離属性を外します: `xattr -dr com.apple.quarantine /Applications/MAM.app`

起動するとウィンドウが開き、ウィンドウを閉じるとアプリ全体が終了します。プロジェクト・設定・生成物は `~/Library/Application Support/mam` に保存します（アプリを消しても残ります）。

自動更新は Windows 版と同じです（起動時と 6 時間ごとに確認し、SHA-256 と ed25519 署名を検証し、作業中でないときにアプリを終了して `MAM.app` を入れ替え、新しい版で起動し直します）。`/Applications` など自分が書き込めるフォルダにある `MAM.app` だけが更新されます。

macOS 版でまだ使えないもの: 同梱の ComfyUI エンジンの導入（別に動かしている ComfyUI の URL を設定すれば使えます）。ffmpeg は同梱していません（次の節）。

### macOS で ffmpeg を入れる

ffmpeg は同梱していません。無くても起動はできますが、Judge（品質判定）、動画のプレビュー、部分採用の合成、Export は ffmpeg と ffprobe を使うので、使うなら入れてください。**MAM がボタン 1 つでダウンロードして入れられます**（Apple Silicon のみ）。

1. MAM の設定画面の「ffmpeg」（初回ガイドの「ffmpeg の確認」でも同じです）で「ffmpeg をダウンロードして入れる」を押します。約 54 MB（展開後は約 126 MB）を、配布元の [martin-riedl.de](https://ffmpeg.martin-riedl.de/) のビルド（Developer ID で署名された固定の版。mam が決めた SHA-256 と一致したものだけを使い、一致しなければ削除します）から取得し、`~/Library/Application Support/mam/ffmpeg` に置きます（`MAM.app` の中には置かないので、自動更新で消えません）。ボタンを押したときだけ通信し、自動の更新確認はしません。ライセンスは GPL v3 です（ソースは配布元にあります。[`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) も見てください）
2. 「ffmpeg を入れました」と出たら、MAM を起動し直します（ffmpeg は起動時に探すため、起動中の MAM には反映されません）
3. 確認: 設定画面の「ffmpeg」に「使えます」と場所が出れば使えます

自分で入れたい人は、ターミナルで `brew install ffmpeg` を実行します（[Homebrew](https://brew.sh) が無いときは先に入れます。ffprobe も一緒に入ります）。Finder や Dock から起動した MAM は、ターミナルの `PATH` を引き継ぎません。そのため MAM は `PATH` に加えて、Homebrew の既定の場所（Apple Silicon は `/opt/homebrew/bin`、Intel は `/usr/local/bin`）も探します。ほかの場所（MacPorts など）に入れたときは、設定画面の「ffmpeg」で ffmpeg と ffprobe のパスを指定してください。ログインシェルの `PATH` は読みません。**自分で入れた ffmpeg は、MAM がダウンロードしたものより優先して使います。**

## Linux、ブラウザで使う

Linux、および Windows・macOS でもブラウザで使いたいときは、ソースからビルドします（Go と Node.js 24 が要ります）。

```sh
make tools    # templ・air・golangci-lint・linterly・govulncheck と npm 依存を入れる
make build    # bin/mam を作る
bin/mam       # サーバーを起動して既定ブラウザを開く
```

`bin/mam --help` で `--data-dir`・`--port`・`--no-browser` などの設定を表示します（それぞれ `MAM_DATA_DIR` などの環境変数でも指定できます）。`make build-all` は Windows / macOS / Linux × amd64 / arm64 の 6 構成を `bin/` に作ります（これはブラウザ版のサーバーで、ネイティブウィンドウ版ではありません）。

## 開発

```sh
make dev          # air + templ --watch + esbuild/tailwind --watch
make test         # Go・front-end・スクリプトのテスト
make lint         # golangci-lint・tsc・Biome lint
make lint-lines   # 行数上限の超過が .linterly-baseline より悪化していないか
make vuln         # govulncheck: 到達できる既知の脆弱性（標準ライブラリ・依存）が無いか（Linux と Windows の両ターゲット）
make fmt          # 整形（Go・templ・TypeScript）
```

macOS 版（Apple Silicon・署名なし）は、Apple Silicon の Mac で `scripts/package-macos.sh <version>`（`X.Y.Z`）で作ります（Xcode コマンドラインツール・Go・Node.js が要り、`bin/mam-<version>-darwin-arm64.dmg` ができます。リリースでは GitHub-hosted の `macos-latest`（Apple Silicon の Mac）が作ります）。

Windows 版（Wails ウィンドウ + ユーザー単位の NSIS インストーラー）は `scripts/package-windows.sh <version>`（`X.Y.Z`）で作ります。`wails3`（`go.mod` の `github.com/wailsapp/wails/v3` と同じ版）と NSIS（`makensis`）と `jq` が要ります。環境変数 `RELEASE_SIGNING_PUBLIC_KEY`（更新の署名検証用の公開鍵。base64）を渡すとバイナリに埋め込みます（空なら警告して埋め込まず、アプリは署名検証を省きます）。リリースは、`main` への push で CI が成功すると GitHub Actions（`.github/workflows/release.yml`）が最新タグの patch を 1 つ上げた版で自動で作ります。Windows のインストーラーと macOS の dmg の `checksums.txt` に署名（シークレット `RELEASE_SIGNING_KEY`）して、このリポジトリの Releases に載せます（`GITHUB_TOKEN` だけを使います。アプリの自動更新もこの Releases を確認します）。Authenticode のコード署名と、Apple の Developer ID 署名・公証はしません。minor・major を上げるときだけ `v<version>` タグを手で push します。詳しくは [`spec/architecture/desktop.md`](spec/architecture/desktop.md) と [`spec/environment/setup.md`](spec/environment/setup.md) を参照してください。

## ライセンス

MAM は [GNU Affero General Public License v3.0](LICENSE)（AGPL-3.0）で公開しています。Copyright (C) 2026 Ousiass合同会社。改変した MAM をネットワーク越しに利用させるときも、その利用者にソースコードを提供する義務があります（AGPL-3.0 第 13 条）。

Windows 版が使うサードパーティのソフトウェアのライセンス表記は [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) にあります。インストーラーは `LICENSE` とこのファイルをインストール先に置きます。`go.mod` や `static/package-lock.json` の依存を変えたら、`scripts/third-party-notices.sh` で作り直します。画面のアイコンは [Lucide](https://lucide.dev)（ISC。Feather 由来のものは MIT）の SVG をソースにコピーしたもので、取り込んだ版のライセンス文は [`backend/internal/web/atoms/icons/LICENSE`](backend/internal/web/atoms/icons/LICENSE) にあり、`THIRD_PARTY_NOTICES.md` にも載ります。
