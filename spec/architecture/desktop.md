# Windows デスクトップ版（Wails v3）

`spec/architecture/overview.md` §11 から参照される、Windows ネイティブウィンドウ版の設計。ブラウザ版（`cmd/server`）は残し、全 OS でこれまで通り使える。

Windows では Wails v3 のネイティブウィンドウ（WebView2）でアプリを開く。Wails v3 はベータ（調査時点の最新タグは v3.0.0-beta.27）のため、`go.mod` の `github.com/wailsapp/wails/v3` と CI の `wails3` CLI を同じ版に固定し、版を上げるときは別途動作確認する。

## 1. 方式: サーバーを維持し、ウィンドウはその URL を開く

- `cmd/wails` は `internal/app` のサーバーを `127.0.0.1` で起動し、起動完了の通知（`OnReady(url)`）でウィンドウに `http://127.0.0.1:<port>` を読み込む（`WebviewWindow.SetURL`）。Wails の AssetServer（`application.AssetOptions.Handler`）へ Gin の `http.Handler` を直接渡す方式は採らない
- 理由: ① Wails v3 の AssetServer は WebSocket のハンドシェイクを 501 で拒否する（`v3/internal/assetserver/assetserver.go`、v3.0.0-beta.27 のソースで確認）。チャット（`/ws/projects/:id`）と生成進捗（`/ws/shots/:id`）は WebSocket で、ポーリングは禁止（non-functional.md §2）。② AssetServer 経由の Host は `wails.localhost`（`assetserver_windows.go`）で、`LoopbackOnly`（overview.md §6）がループバック以外の Host を拒否するのと食い違う。サーバーを維持すれば WebSocket・CSRF・`LoopbackOnly`・大容量アップロード・動画の Range 配信・ダウンロードのいずれも変更せずに使える（AssetServer 上の Range とアップロード上限の挙動は未検証で、本方式では問題にならない）
- ウィンドウはサーバーの URL 以外へ移動しない。Wails のバインディング・イベントは使わない（Go 側の API を WebView に公開しない）。`LoopbackOnly` と CSRF は変更しない
- 起動順: ① `internal/app` が `127.0.0.1` のポートを確保して待ち受けを始める（`--port` の既定 8787、使用中ならOSが選ぶ空きポート）② `OnReady(url)` が確定したポートの URL をウィンドウに渡す。ウィンドウはそれまで背景色だけの空ページを出す。サーバーの起動はウィンドウの生成結果に依存しない

## 2. `cmd/wails` の挙動

- ビルドタグ `//go:build windows`（Wails v3 の Linux/macOS ビルドは CGO と GTK/WebKit を要するため、Ubuntu の CI で `go build ./...`・`go vet ./...`・golangci-lint が通るようにする）。Windows 向けは `CGO_ENABLED=0` でビルドできること
- DI ルートは `internal/app`（`cmd/server/main.go` の `buildServices` を移す）。`internal/app` は `service`・`repository`・`agent`・`router`・`credential` など全層を import してよい（`.golangci.yml` の depguard は `internal/{domain,repository,agent,service,web/handler}` の各層だけを制限し、`internal/app` と `cmd/*` は対象外。実装時に `golangci-lint` で確認する）
- ウィンドウ: タイトル `MAM`、既定 1440×900、最小 1024×700。背景色はアプリの背景（`bg-neutral-950`）に合わせ、サーバーが応答するまでの白いちらつきを避ける
- 構成: Wails に依存しない部分は `internal/desktop`（`Navigator`: サーバーの URL とウィンドウの初回ナビゲーション完了を、どちらが先でも待ち合わせて `SetURL` する。`Server`: `app.Run` のバックグラウンド実行・停止・予期しない終了の通知）に置き、OS 非依存でテストする。`cmd/wails` はそれを Wails のウィンドウ・単一インスタンス・`OnShutdown` に結ぶだけにする
- 単一インスタンス: `application.Options.SingleInstance`（`UniqueID` は固定の `com.ousiassllc.mam`）。2 つ目の起動は新しいウィンドウを作らず、先のウィンドウを復元して前面に出して終了する（データディレクトリのロック `.mam.lock` に到達しない）。2 つ目の起動の `--data-dir` などの引数は使わず捨てる。Windows 版は `--data-dir` が違っても同時に 1 つしか起動できない（並行起動はブラウザ版 `cmd/server` を使う）
- 終了: ウィンドウを閉じるとアプリ全体を終了する（トレイ常駐なし）。`app.Run` の `ctx` を取り消してグレースフルシャットダウン（`shutdownTimeout` 10 秒）を行い、サーバーが止まってから Wails アプリを終了する。生成ジョブ（外部 API 呼び出しで課金が発生する）の実行中に閉じたときの扱い: ジョブは中断され、次回起動時にクラッシュ復旧（non-functional.md §3）と同じ経路で再開または安全に失敗として扱う。実行中ジョブがあるときの確認ダイアログは出さない（ブラウザ版でタブやプロセスを閉じるのと同じ扱い）
- 起動エラー（設定の誤り・データディレクトリが別プロセスで使用中・リッスン失敗）はコンソールが無い（`-H windowsgui`）ためネイティブのエラーダイアログ（Win32 `MessageBox`。Wails のダイアログはイベントループが要るため使わない）に出して終了コード 1 で終わる。`--help` と引数の誤りもダイアログに出す（`app.ValidateArgs` が使い方の文字列を返す）。起動後にサーバーが落ちたときも同じ（ダイアログ → 終了コード 1）
- 設定は `cmd/server` と同じ `config.Load`（フラグ・`MAM_*`。`--data-dir`・`MAM_DATA_DIR` も同様に効く）。`--no-browser` は無視する。ショートカットは引数なしで起動する（データディレクトリの既定は `os.UserConfigDir()` 配下の `mam`、つまり `%AppData%\mam`）
- ブラウザ版と異なる点: Windows では ONNX Runtime のバインディングが無いため顔同一性の定量評価は使えない（non-functional.md §1、ブラウザ版も同じ）。ffmpeg は PATH から探す（同梱しない）

## 3. 実装時に検証する項目

Windows 実機が要る項目は、リリース前に実機で確かめる（実機での確認結果は Issue #480 に残す）。ソースで確かめられた分は結果を書く。

- ダウンロード（`Content-Disposition: attachment` を返す `handler/bundle.go`・`review.go`、`<a download>` の `editor_panel.templ`・`settings_body.templ`・`review_workspace.templ`・`review_list.templ`）: v3.0.0-beta.27 は WebView2 の `DownloadStarting` を公開していない（`pkg/application` に該当の API が無い）。実装は何も差し込まず WebView2 の既定（ダウンロード UI）に任せる。**実機で保存できるかは未確認**。保存できなければ、ダウンロードの URL を `application.BrowserManager.OpenURL` で既定ブラウザへ渡す
- `target="_blank"` のリンク（`world_state_view.templ`・`message_frame.templ`・`keyframe_card.templ`。ループバックのアセット画像）: v3.0.0-beta.27 は `NewWindowRequested` を公開していないため、WebView2 の既定（新しいポップアップウィンドウ）になる。**実機で目的を果たせるかは未確認**。足りなければ新しいウィンドウを作らず `application.BrowserManager.OpenURL` で既定ブラウザへ渡す
- 外部 URL を開くウィンドウに Wails のランタイム（`window._wails`）が露出するか: ナビゲーション完了のたびにランタイムが注入される（`webview_window_windows.go`）ため露出する。バインディングは登録しないので Go 側の API は呼べず、権限は増えない。`AllowSimpleEventEmit` は無効（既定）のままとする
- `windows-latest`（デスクトップセッションなし）で GUI の `mam.exe` のウィンドウ生成が失敗しないか: **未確認**（最初のタグのリリースジョブで分かる）。失敗するなら、リリースのスモーク（`spec/environment/setup.md`）の対象をブラウザ版 `cmd/server` の exe に切り替える

## 4. パッケージングと配布

- ビルド・パッケージングは `scripts/package-windows.sh`（`build.sh` と同じ templ → esbuild → tailwind の後、`wails3 package GOOS=windows`）。設定は `build/`（Wails v3 の既定配置。Taskfile のフロントエンド工程は本リポジトリの `static/` のビルドへ置き換える。Go のビルドは `./backend/cmd/wails`、`-tags production`、`-ldflags "-s -w -H windowsgui"`、`-trimpath`、`CGO_ENABLED=0`、`GOOS=windows GOARCH=amd64`）
- 成果物: ポータブル `mam.exe`（Wails ウィンドウ版）と、それを入れる NSIS インストーラー exe。インストーラーは WebView2 ブートストラッパーを含み（WebView2 が無い Windows 10 では取得するためインターネット接続が要る）、Start メニューとデスクトップにショートカットを作り、アンインストーラーを登録する。インストール先は `%ProgramFiles%\MAM`（Wails v3 の NSIS 既定の管理者権限）。アンインストールでデータディレクトリ（SQLite・生成物）は消さない。完了画面に ffmpeg の導入案内（PATH に `ffmpeg` / `ffprobe` を置くと Judge・プレビュー・Export などが使える。無くても起動する）を出す
- バージョンは Git タグ `vX.Y.Z` の `X.Y.Z`。`build/windows/info.json`（ファイルバージョン・ProductVersion）と NSIS の `INFO_PRODUCTVERSION`、ファイル名に反映する
- 配布は GitHub Actions の `release.yml`（`windows-latest`、`v*` タグの push）が `gh release create` でリリースを作り、2 つの成果物を添付する（`spec/environment/setup.md`）。コード署名はしない

## 5. 対象外

トレイ常駐・自動更新・コード署名・Windows arm64・macOS/Linux のネイティブ版・ffmpeg の同梱。必要になった時点で別 Issue とする。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-03 | 新規作成。`spec/architecture/overview.md` §11 に書いた Windows デスクトップ版（方式・`cmd/wails` の挙動・未検証項目・パッケージング）をこのファイルへ移した | Issue #480。`overview.md` が行数上限（300 行、`.linterly.yml`）を超えるため、`model-adapters.md` と同じく `spec/architecture/` 内の別ファイルに切り出した |
| 1.1 | 2026-10-11 | §2 に構成（`internal/desktop`）・背景色・ダイアログの実装方法・終了時の確認ダイアログなしを、§3 に v3.0.0-beta.27 のソースで確かめた結果（`DownloadStarting`・`NewWindowRequested` が非公開、ランタイムは露出）と実機で未確認の項目を追記 | Issue #480 の実装 |
| 1.2 | 2026-10-11 | §3 のダウンロードの実機確認の対象に、Review Package の ZIP のリンク（`organisms/review_list.templ`）を加えた | Issue #514。`<a download>` を持つテンプレートは 4 つあるのに列挙が 3 つで、リリース前の実機確認が Review Package のダウンロードを確認漏れにするため（応答は `handler/review.go` の `Content-Disposition: attachment`） |
