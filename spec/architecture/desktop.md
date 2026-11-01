# デスクトップ版（Wails v3）: Windows と macOS（Apple Silicon）

`spec/architecture/overview.md` §11 から参照される、ネイティブウィンドウ版（Windows・macOS）の設計。本文は Windows を軸に書き、macOS の違いは §7 にまとめる（`cmd/wails` の挙動・自動更新の流れ・署名と検証は共通）。ブラウザ版（`cmd/server`）は残し、全 OS でこれまで通り使える。

Windows では Wails v3 のネイティブウィンドウ（WebView2）、macOS（Apple Silicon）では WKWebView でアプリを開く。Wails v3 はベータ（調査時点の最新タグは v3.0.0-beta.27。現在の固定版は `go.mod` の `github.com/wailsapp/wails/v3`）のため、`go.mod` の `github.com/wailsapp/wails/v3` と CI の `wails3` CLI を同じ版に固定し、版を上げるときは別途動作確認する。

## 1. 方式: サーバーを維持し、ウィンドウはその URL を開く

- `cmd/wails` は `internal/app` のサーバーを `127.0.0.1` で起動し、起動完了の通知（`OnReady(url)`）でウィンドウに `http://127.0.0.1:<port>` を読み込む（`WebviewWindow.SetURL`）。Wails の AssetServer（`application.AssetOptions.Handler`）へ Gin の `http.Handler` を直接渡す方式は採らない
- 理由: ① Wails v3 の AssetServer は WebSocket のハンドシェイクを 501 で拒否する（`v3/internal/assetserver/assetserver.go`、v3.0.0-beta.27 のソースで確認）。チャット（`/ws/projects/:id`）と生成進捗（`/ws/shots/:id`）は WebSocket で、ポーリングは禁止（non-functional.md §2）。② AssetServer 経由の Host は `wails.localhost`（`assetserver_windows.go`）で、`LoopbackOnly`（overview.md §6）がループバック以外の Host を拒否するのと食い違う。サーバーを維持すれば WebSocket・CSRF・`LoopbackOnly`・大容量アップロード・動画の Range 配信・ダウンロードのいずれも変更せずに使える（AssetServer 上の Range とアップロード上限の挙動は未検証で、本方式では問題にならない）
- ウィンドウはサーバーの URL 以外へ移動しない。Wails のバインディング・イベントは使わない（Go 側の API を WebView に公開しない）。`LoopbackOnly` と CSRF は変更しない
- 起動順: ① `internal/app` が `127.0.0.1` のポートを確保して待ち受けを始める（`--port` の既定 8787、使用中ならOSが選ぶ空きポート）② `OnReady(url)` が確定したポートの URL をウィンドウに渡す。ウィンドウはそれまで背景色だけの空ページを出す。サーバーの起動はウィンドウの生成結果に依存しない
- MCP サーバー（`spec/architecture/mcp.md`）は `internal/app` の中で起動するので、Wails 版でもブラウザ版と同じに使える（ウィンドウとは無関係に別のリスナーで、既定は `127.0.0.1:8788`。ウィンドウが閉じてアプリが終われば止まる）。インストーラー版・ポータブル版の違いもない

## 2. `cmd/wails` の挙動

- ビルドタグ `//go:build windows || darwin`（ファイルは `main.go` と、OS ごとの `platform_windows.go`・`platform_darwin.go`・`dialog.go`（Windows）・`dialog_darwin.go`）。Wails v3 の Linux ビルドは CGO と GTK/WebKit を要するため、Ubuntu の CI で `go build ./...`・`go vet ./...`・golangci-lint が通るようにする（Linux では何もビルドされない）。Windows 向けは `CGO_ENABLED=0` でビルドできること。macOS 向けは cgo が要る（Cocoa・WebKit）ため Linux の CI では `cmd/wails` をビルドできず、GitHub-hosted の `macos-latest` で動く `ci.yml` の `macos` ジョブと `release.yml` の `build-macos` ジョブがビルドする（§7）
- DI ルートは `internal/app`（`cmd/server/main.go` の `buildServices` を移す）。`internal/app` は `service`・`repository`・`agent`・`router`・`credential` など全層を import してよい（`.golangci.yml` の depguard は `internal/{domain,repository,agent,service,web/handler}` の各層だけを制限し、`internal/app` と `cmd/*` は対象外。実装時に `golangci-lint` で確認する）
- ウィンドウ: タイトル `MAM`、既定 1440×900、最小 1024×700。背景色はアプリの背景（`bg-neutral-950`）に合わせ、サーバーが応答するまでの白いちらつきを避ける
- 構成: Wails に依存しない部分は `internal/desktop`（`Navigator`: サーバーの URL とウィンドウの初回ナビゲーション完了を、どちらが先でも待ち合わせて `SetURL` する。`Server`: `app.Run` のバックグラウンド実行・停止・予期しない終了の通知）に置き、OS 非依存でテストする。`cmd/wails` はそれを Wails のウィンドウ・単一インスタンス・`OnShutdown` に結ぶだけにする
- 単一インスタンス: `application.Options.SingleInstance`（`UniqueID` は固定の `com.ousiassllc.mam`）。2 つ目の起動は新しいウィンドウを作らず、先のウィンドウを復元して前面に出して終了する（データディレクトリのロック `.mam.lock` に到達しない）。2 つ目の起動の `--data-dir` などの引数は使わず捨てる。Windows 版は `--data-dir` が違っても同時に 1 つしか起動できない（並行起動はブラウザ版 `cmd/server` を使う）
- 終了: ウィンドウを閉じるとアプリ全体を終了する（トレイ常駐なし）。`app.Run` の `ctx` を取り消してグレースフルシャットダウン（`shutdownTimeout` 10 秒）を行い、サーバーが止まってから Wails アプリを終了する。10 秒で終わらないリクエスト（動画の配信・長いアップロードなど）は中断し（コンテキストをキャンセルして接続を閉じる）、ハンドラが戻るまで最大 5 秒（`abortTimeout`）待ってからジョブキューや DB を閉じる（中断は要求された停止であり、失敗として扱わない）。ウィンドウを閉じてからアプリが終わるまでの最長は約 15 秒。生成ジョブ（外部 API 呼び出しで課金が発生する）の実行中に閉じたときの扱い: ジョブは中断され、次回起動時にクラッシュ復旧（non-functional.md §3）と同じ経路で再開または安全に失敗として扱う。実行中ジョブがあるときの確認ダイアログは出さない（ブラウザ版でタブやプロセスを閉じるのと同じ扱い）
- 起動エラー（設定の誤り・データディレクトリが別プロセスで使用中・リッスン失敗）はコンソールが無い（`-H windowsgui`）ためネイティブのエラーダイアログ（Win32 `MessageBox`。Wails のダイアログはイベントループが要るため使わない）に出して終了コード 1 で終わる。`--help` と引数の誤りもダイアログに出す（`app.ValidateArgs` が使い方の文字列を返す）。起動後にサーバーが落ちたときも同じ（ダイアログ → 終了コード 1）
- 設定は `cmd/server` と同じ `config.Load`（フラグ・`MAM_*`。`--data-dir`・`MAM_DATA_DIR` も同様に効く）。`--no-browser` は無視する。ショートカットは引数なしで起動する（データディレクトリの既定は `os.UserConfigDir()` 配下の `mam`、つまり `%AppData%\mam`）
- ブラウザ版と異なる点: Windows では ONNX Runtime のバインディングが無いため顔同一性の定量評価は使えない（non-functional.md §1、ブラウザ版も同じ）。ffmpeg は設定で指定したパス、無ければ `PATH`（macOS は Homebrew の場所も）から探す（同梱しない。[ffmpeg-lookup.md](ffmpeg-lookup.md)）
- 子プロセス: ffmpeg / ffprobe はコンソールウィンドウを出さずに起動する（GUI プロセスはコンソールを持たず、何もしないと起動のたびに黒いウィンドウが点滅するため。`internal/media/run_windows.go` が `HideWindow` と `CREATE_NO_WINDOW` を付ける）。ブラウザ版は親のコンソールを継承するので影響しない。付くことは Windows 専用テスト `internal/media/run_windows_test.go` が確かめる（`hideWindow` 単体と、ffmpeg / ffprobe を起動する唯一の経路 `run` を実 exe で通して `commandContext` が作った `exec.Cmd` を見る検査の 2 つ）。リリースの `build` ジョブ（`spec/environment/setup.md` のリリース手順 5）が Windows 上で動かす

## 3. 実装時に検証する項目

Windows 実機が要る項目は、リリース前に実機で確かめる（実機での確認結果は Issue #480 に残す）。ソースで確かめられた分は結果を書く。

- ダウンロード（`Content-Disposition: attachment` を返す `handler/bundle.go`・`review.go`・`editor.go` の `ExportVideo`、`<a download>` の `organisms/editorpanel/stages.templ`・`settings_body.templ`・`review_workspace.templ`・`reviewlist/list.templ`・`text_message.templ`（チャットの書き出し完了通知「書き出した動画をダウンロード」。`ref_type=export` のリンクを `molecules/format.go` の `refLinkOf` が作る。応答は `GET /projects/:id/exports/:exportId/video`））: v3.0.0-beta.27 は WebView2 の `DownloadStarting` を公開していない（`pkg/application` に該当の API が無い）。実装は何も差し込まず WebView2 の既定（ダウンロード UI）に任せる。**実機で保存できるかは未確認**。保存できなければ、ダウンロードの URL を `application.BrowserManager.OpenURL` で既定ブラウザへ渡す
- `target="_blank"` のリンク（`organisms/worldstate/view.templ`・`message_frame.templ`・`keyframe_card.templ`。ループバックのアセット画像）: v3.0.0-beta.27 は `NewWindowRequested` を公開していないため、WebView2 の既定（新しいポップアップウィンドウ）になる。**実機で目的を果たせるかは未確認**。足りなければ新しいウィンドウを作らず `application.BrowserManager.OpenURL` で既定ブラウザへ渡す
- 外部 URL を開くウィンドウに Wails のランタイム（`window._wails`）が露出するか: ナビゲーション完了のたびにランタイムが注入される（`webview_window_windows.go`）ため露出する。バインディングは登録しないので Go 側の API は呼べず、権限は増えない。`AllowSimpleEventEmit` は無効（既定）のままとする
- Windows 上の GUI の `mam.exe` のウィンドウ生成が失敗しないか: **未確認**（リリースのジョブは Linux でクロスビルドするため、Windows では動かさない。Windows の機械で `scripts/smoke-windows.sh` を手動で実行して確かめる）

## 4. パッケージングと配布

- ビルド・パッケージングは `scripts/package-windows.sh`（`build.sh` と同じ templ → esbuild → tailwind の後、`wails3 package GOOS=windows`）。設定は `build/`（Wails v3 の既定配置。Taskfile のフロントエンド工程は本リポジトリの `static/` のビルドへ置き換える。Go のビルドは `./backend/cmd/wails`、`-tags production`、`-ldflags "-s -w -H windowsgui"`、`-trimpath`、`CGO_ENABLED=0`、`GOOS=windows GOARCH=amd64`）。`wails3 package` が読むのは `build/Taskfile.yml`・`build/windows/` で、`build/config.yml` は読まない（`wails3 update build-assets` を実行する箇所も無い）ため、exe のリソースやインストーラーを変えるときは `config.yml` の `info`・`version` ではなく `build/windows/info.json` と `package-windows.sh` の引数を直す
- 成果物: ポータブル `mam.exe`（Wails ウィンドウ版）と、それを入れる NSIS インストーラー exe。インストーラーは WebView2 ブートストラッパーを含み（WebView2 が無い Windows 10 では取得するためインターネット接続が要る）、`LICENSE`（AGPL-3.0）とサードパーティのライセンス表記 `THIRD_PARTY_NOTICES.md` をインストール先に置き、Start メニューとデスクトップにショートカットを作り、アンインストーラーを登録する。インストールはユーザー単位（`WAILS_INSTALL_SCOPE=user`）で、インストール先は `%LOCALAPPDATA%\Programs\MAM`、管理者権限は要らない（自動更新が UAC の確認なしで動くため。§6）。アンインストールでデータディレクトリ（SQLite・生成物）は消さない。完了画面に ffmpeg の導入案内（PATH に `ffmpeg` / `ffprobe` を置くと Judge・プレビュー・Export などが使える。無くても起動する）を出す
- バージョンは Git タグ `vX.Y.Z` の `X.Y.Z`。`main` への push で CI が成功すると、最新の `v*` タグの patch を 1 つ上げた版（タグが無ければ `v0.1.0`）を自動で付ける。minor・major を上げるときは手でタグを打つ。`build/windows/info.json`（ファイルバージョン・ProductVersion）と NSIS の `INFO_PRODUCTVERSION`、ファイル名に反映する
- 配布は GitHub Actions の `release.yml`（`main` の CI 成功後の `workflow_run`、または `v*` タグの手動 push。`plan` → `verify` → `build-windows` → `build-macos` → `sign` → `release` の 6 ジョブ。Windows 版は GitHub-hosted の `ubuntu-latest` で Linux 上でクロスビルドして `makensis` でインストーラーを作り、macOS 版は GitHub-hosted の `macos-latest`（Apple Silicon。§7）でビルドする。`release` だけが書き込み権限を持つ）が `gh release create` でリリースを作り、5 つのファイルを添付する: インストーラー `mam-<version>-windows-amd64-setup.exe`、ポータブル版 `mam-<version>-windows-amd64.exe`、macOS 版のディスクイメージ `mam-<version>-darwin-arm64.dmg`、2 つのインストーラー（setup exe と dmg）を対象にしたチェックサム `checksums.txt`、その署名 `checksums.txt.sig`（§6.3。`spec/environment/setup.md`）。Authenticode のコード署名と、Apple の Developer ID 署名・公証はしない

- **ComfyUI は同梱しない**: インストーラー・ポータブル版・Release のどれにも ComfyUI のバイトは入らず、インストーラーのサイズは変わらない（サイズと自動更新の上限のため）。ComfyUI のポータブル版（GPL-3.0 の別プログラム）は、アプリ内の「エンジンの導入」操作が GitHub から取得し（SHA-256 を検証）、`<mam.exe のディレクトリ>\comfyui` に展開する。インストール版は `%LOCALAPPDATA%\Programs\MAM\comfyui`、ポータブル版は exe の隣。**アンインストールの扱い**: `build/windows/nsis/project.nsi` の `uninstall` セクションの `RMDir /r $INSTDIR` がインストール先をまるごと消すので、`comfyui`（エンジン・モデル・ComfyUI の出力）も一緒に消え、他の場所には何も残らない（データディレクトリ `%AppData%\mam` は従来どおり残す）。アンインストールの時点で ComfyUI の子プロセスが動いているとファイルが消せないため、子は MAM の終了で止まり（MAM が異常終了しても Job Object の `KILL_ON_JOB_CLOSE` で止まる）、残らない。`project.nsi` の変更は不要と見ているが、実機での確認は未了（`model-manager/model-manager.md` §11）。詳細は `model-manager/model-manager.md`。

## 5. 対象外

トレイ常駐・コード署名（Authenticode。§6 の ed25519 署名はインストーラーの真正性を確かめる別の仕組み）・Apple の Developer ID 署名と公証・Windows arm64・macOS の Intel 版とユニバーサル版・Linux のネイティブ版・ffmpeg の同梱。必要になった時点で別 Issue とする。

## 6. 自動更新

インストール済みの Windows 版と macOS 版（Apple Silicon）は、新しいリリースを自分で見つけて入れ替える。Wails 版（`cmd/wails`）だけが動かし、ヘッドレスの `cmd/server` は更新しない（インストーラーが無い）。実装は `internal/service/updater`（GitHub・検証・安全ゲート。プラットフォーム別のアセット名は `updater.Platform`）、`internal/service/appupdate`（状態・手動確認・スケジュール）、`internal/desktop`（終了とインストーラー・入れ替えの起動）、画面は §6.7。本節の流れは Windows（`mam-<X.Y.Z>-windows-amd64-setup.exe` を `/S` で実行）で書き、macOS（`mam-<X.Y.Z>-darwin-arm64.dmg` を入れ替える）の違いは §7.3 にある。

### 6.1 流れ

1. 起動の 1 分後に最初の確認をし、その後 6 時間ごとに確認する（起動を遅くしない。`appupdate.Schedule`）。失敗したときと、新しいリリースを安全ゲートが止めたときは、1 分から始めて倍にし最大 1 時間までの間隔で繰り返す。原因が変わらない失敗（`ErrorAccess`・`ErrorVerification`・`ErrorRelease`）は繰り返さず、次の 6 時間ごとの確認を待つ。開発版は最初の確認で打ち切る
2. この（`ousiassllc/mam`）リポジトリ自身の Releases（`version.GitHubOwner` / `version.GitHubRepo`）の `GET /repos/{owner}/{repo}/releases/latest` の `tag_name` を、埋め込んだ版（`version.Version`）と `golang.org/x/mod/semver` で比べる。新しくなければ何もしない。リリースが無い（404）のは失敗ではなく「リリースなし」。リポジトリが非公開の間は、認証無しの照会に GitHub が 404 を返すので、この「リリースなし」になる（公開すれば最新の Release が見える。追加の処理は要らない）
3. 安全ゲート（§6.2）が通れば、インストーラー（リリースの `tag_name` から先頭の `v` を除いた版の名前 `mam-<X.Y.Z>-windows-amd64-setup.exe`（macOS は `mam-<X.Y.Z>-darwin-arm64.dmg`。§7.3）に**完全に一致する**アセットだけ。別の版のインストーラーやポータブル版は選ばず、無ければ `ErrorRelease`）・`checksums.txt`・`checksums.txt.sig` を一時ディレクトリ（`mam-update-*`）へ取得し、署名と SHA-256 を検証する（§6.3）
4. ダウンロードには数分かかるので、**取得の直後に安全ゲートをもう一度**確かめる。閉じていれば結果は Blocked で、取り込んだファイルは消す（次の確認で取り直す）
5. 検証できたら `appupdate.Service` が `UpdateOptions.Install(path)` を呼ぶ。`desktop.Updater` はインストーラーのパスを覚え、Wails の `Quit()` を別 goroutine で呼ぶ（検証した確認 goroutine の中で終了を待たない）
6. `OnShutdown` で `app.Run` が止まって（サーバー・ジョブキュー・DB が閉じて）から、`desktop.InstallCommand(path)`（`<installer> /S`、コンソール無し・新しいプロセスグループの切り離したプロセス）を起動し、アプリは終わる。起動に失敗したときはネイティブのダイアログで知らせる（アプリは終わっているので、起動し直してもらう）
7. インストーラーは `/S` で画面なしに動く。`.onInit` が、入れ替え先の `mam.exe` を書き込みで開けるまで（旧プロセスが終わるまで。最大 60 秒）待ち、`wails.files` が上書きし、`.onInstSuccess` が**サイレントのときだけ**新しい `mam.exe` を `Exec` で起動する。サイレント実行は完了画面を含む全ページを飛ばすため、完了画面の「起動する」では再起動できない
8. 次の起動の `tempcleanup.CleanupStale` が、前回のダウンロードの一時ディレクトリを消す（実行中のインストーラー自身は自分を消せないため）。このプロセスの起動より古いものだけを消す

### 6.2 安全ゲート（再起動で失う・壊れる作業が走っていない）

`updater.SafeGate.SafeToUpdate` は、次のいずれかがあれば更新を止める（`BlockReason.Kind`）。確かめられなかったとき（DB の問い合わせの失敗）も止める（fail closed）。

| 種別 | 条件 | 権威のある情報源と理由 |
|------|------|------------------------|
| `jobs` | `job` テーブルに `running` または `queued` の行がある（generate・judge・repair・plan・model_install・gentest。モデルの導入と試し生成も、再起動で止まるため作業中として数える） | **DB**。ジョブは実行前に保存し、開始・終了で状態を書くので、ワーカーがまだ取り出していないジョブも見える（メモリ上のキューは取り出し済みしか知らない）。再起動は generate・repair を失敗にし、`queued` は再起動後すぐ走り直すので、どちらも「実行中の作業」として数える |
| `editing` | `EditorService.Busy()`: いずれかの Project で編集の工程（Rough Cut・Editor Judge・音声・Final QC・Export）または AUTO の連鎖（`stage`・`auto` の `keylock`）、再開ワーカーが動いている | **メモリ**。これらは DB に行が無く `keylock` が実行中を表す唯一の情報 |
| `media` | `media.Running() > 0`: ffmpeg / ffprobe が動いている（プレビュー・Partial Take Use・書き出し・変換。`media.run` が起動と終了で数える） | **メモリ（プロセス数）**。再起動は子プロセスを殺し、書きかけの出力が残る |
| `check_failed` | 上のいずれかの照会が失敗した | 安全と確かめられないため止める |

止まった間は `Status.Blocked` になり（画面に待っている理由を出す）、§6.1 の間隔で再確認する。ユーザーが作業を始めた直後に更新が走る窓は、取得直後の再確認から終了までの数秒間に残る（それより狭くはできない: 終了は呼び出し側の非同期操作）。ゲートは `updater.Gate` インターフェースで、単体テストは偽物（`fakeGate`・`fakeJobs`）で検証する。

### 6.3 署名と検証

- `checksums.txt` は 2 つのインストーラー（Windows の setup exe と macOS の dmg）を対象にした `sha256sum` の出力（`<hex>  mam-<X.Y.Z>-windows-amd64-setup.exe` と `<hex>  mam-<X.Y.Z>-darwin-arm64.dmg`。ポータブル版は載せない）で、選んだインストーラーと**同じ名前**の行だけを探す（別の名前の行しか無ければ `ErrorVerification`）
- SHA-256 だけでは、同じリリースにあるファイル同士なので差し替えを検出できない。`release.yml` の `sign` ジョブの最後のスクリプト工程（`Sign checksums`、`scripts/sign-checksums.sh`）が `checksums.txt` に**ed25519 の分離署名**（生の 64 バイトの base64）`checksums.txt.sig` を付け、同じステップで公開鍵を使って署名を検証する（鍵が合わなければジョブが落ちる）。署名鍵（GitHub Actions のシークレット `RELEASE_SIGNING_KEY`）を見るのはこのステップだけで、`release` ジョブは署名せず、受け取ったファイル（5 つ）を公開するだけ。公開鍵（生の 32 バイトの base64）は `-ldflags -X …/internal/version.ReleasePublicKey=` でバイナリに埋め込む（リポジトリ変数 `RELEASE_SIGNING_PUBLIC_KEY`）
- 公開鍵が埋め込まれているとき、署名の欠落・形式の誤り・不一致、鍵そのものの形式の誤りは、すべて検証エラー（`ErrorVerification`。繰り返さない）にして、ダウンロードしたファイルを全部消す（fail closed）。インストーラーは署名の検証が通る前には取得もしない。公開鍵が空の開発ビルドは署名の検証を飛ばして警告を出し、SHA-256 だけで確かめる
- ネットワークの堅牢化: リリース照会 30 秒・ダウンロード全体 10 分のデッドライン、ダウンロードサイズの上限（インストーラー 512 MiB・`checksums.txt` 1 MiB・署名 4 KiB。GitHub が返す `size` があればそれに一致）、ダウンロード URL は `https://github.com/ousiassllc/mam/releases/download/` の配下だけ許す
- 版は `version.Version`（リリースビルドが `v<X.Y.Z>` を埋め込む）。`dev`（semver でない値）の開発ビルドは確認そのものをしない。リリースのタグは `v<X.Y.Z>`
- 古い版からのつなぎ: 自動更新は MAM 自身の Releases だけを見る。`ousiassllc/mam-releases` を確認する版は一度も配布されておらず（このリポジトリには Release もタグも無い）、つなぐ実装は要らない。すでに配布された v0.1.0・v0.1.1 は自動更新を持たず、`checksums.txt` も署名も付いていないので、利用者は署名付きの最初のリリースを手動でインストールする（以後は自動で更新される）

### 6.4 ユーザー単位のインストール

インストーラーは `WAILS_INSTALL_SCOPE=user`（`$LOCALAPPDATA\Programs\MAM`、`RequestExecutionLevel user`）で作る。管理者権限が要らないので、アプリが `/S` で起動するインストーラーは UAC の確認を出さず、更新後に起動するアプリも管理者権限を持たない。**これまでの v0.1.x の管理者権限（`%ProgramFiles%\MAM`）のインストールは移行しない**: 新しいインストーラーを手動で入れると別の場所に並ぶので、古い版をアンインストールしてから入れる。古い版は更新を確認しないため、自動では置き換わらない。データディレクトリ（`%AppData%\mam`）はどちらからも共有され、消えない。ポータブル版の `mam.exe` もインストーラー版と同じバイナリなので同じく更新を確認し、新版が見つかると検証済みのインストーラーでユーザー単位にインストールされる（ポータブルの exe 自体は書き換えない）。

### 6.5 画面で確かめられる状態

設定画面（`GET /settings`）の「バージョンとアップデート」（§6.7）は、`appupdate.View` の状態を出す: 未確認・確認中・最新・新版あり（取得中）・待機中（作業が動いているため。待っている理由付き）・更新します（検証済み）・エラー（`ErrorKind`: ネットワーク・取得回数の上限・アクセス拒否・検証失敗・リリースの内容・その他）・リリースなし・開発版。エラーの生のメッセージは画面に出さずログに残す。確認の失敗は、定期確認でも手動確認（`POST /settings/update/check`）でも、`appupdate.Service.Check` が `ERROR` で 1 回だけ記録する（`kind`（`ErrorKind`）・新版が分かっていれば `version`・`err`。検証失敗も同じ。「リリースなし」と、アプリの終了や接続の切断で取り消された確認は記録しない）。スケジュールは再試行するかどうかの判断だけを `INFO` で記録する。

### 6.6 テストできること・できないこと

- 単体テスト（Linux の CI）: 偽の GitHub（`httptest`）での取得・semver 比較・チェックサム不一致・署名の各失敗（改ざんしたインストーラー・差し替えた `checksums.txt`・他の鍵・欠落・形式の誤り）・サイズ上限・タイムアウト・安全ゲート（偽の DB・偽のレジストリ）・取得後の再確認・開発版の打ち切り・スケジュールの再試行・インストーラーのコマンドの組み立て（`InstallCommand`）・`Updater` の終了と起動の順序・画面。
- **未確認（Windows 実機が要る）**: Windows でインストーラーを `/S` で実際に起動して入れ替えること、`.onInit` の待機と `.onInstSuccess` の再起動、切り離したプロセス（`DETACHED_PROCESS`）が親の終了後も生き残ること、実際の GitHub の `releases/latest`。新しい版が公開されたら、古い版からの更新を実機で一度通す。

### 6.7 設定画面

`organisms/updatepanel`（表示）・`handler/appupdate`（`SectionData`・`Check`）・`router/settingsroutes`。ヘッドレスの `cmd/server`（`settingsroutes.Services.Update` が nil）は版だけを出し、確認ボタンと `POST /settings/update/check` は無い。開発ビルドの版は「開発版」と表示する。

### 6.8 `comfyui` ディレクトリと更新

自動更新（`setup.exe /S`）が入れ替えるのはインストーラーが置くファイルだけで、`project.nsi` の `install` セクションは `$INSTDIR\comfyui` を消さない（`Delete`・`RMDir` を呼ばず、`File` で上書きするだけ）。よって導入済みの ComfyUI エンジンとモデルは更新後もそのまま使える。**エンジンの更新は MAM の更新とは別**: サイレントインストーラーはエンジンを取り直さず、固定版（カタログ）が変わったときだけ、設定画面の「エンジンを更新」で入れ替える（`model-manager/model-manager.md` §10）。再起動の前に ComfyUI の子プロセスを止める（`Services.Close`）ので、更新がエンジンのファイルに触れて失敗することはない。`comfyui\` の実機での扱い（更新後の残存・アンインストールでの削除）は未確認。

**アンインストールと動いている ComfyUI**: 子プロセスの ComfyUI は、Job Object（`KILL_ON_JOB_CLOSE`、`internal/modelmgr/proc`）により mam の終了で終わるので、`waitForAppToExit` が待つ「インストール済みの `mam.exe` を書き込みで開けるまで」の間に、エンジンも止まる。アンインストーラーには `un.onInit` を足し、同じ待ち（最大 10 秒。サイレントでなくても待つ）をしてから、`RMDir /r $INSTDIR` を `comfyui` が消えるまで最大 5 秒（0.5 秒おき 10 回）くり返す（ComfyUI の子プロセスがファイルを放すまでの短い間に消せないことがある）。新しいインストーラーのページは足さない（エンジンの導入はアプリの中のボタン）。**`makensis` はこの開発ホストに無く、`project.nsi` の構文は目視だけで確認した（未ビルド）**。

## 7. macOS 版（Apple Silicon・署名なし）

Issue #853。Windows 版と同じ `cmd/wails`（サーバーを維持し、ウィンドウはその URL を開く。§1・§2）を macOS でも動かし、同じリリースで配布して自動更新に対応する。対象は **Apple Silicon（arm64）だけ**で、Intel（amd64）版・ユニバーサル版は作らない。最低 OS は macOS 12（`LSMinimumSystemVersion`）。**Apple の Developer ID 署名と公証はしない**（アドホック署名 `codesign --sign -` だけ）。

### 7.1 ビルドとパッケージング

- `scripts/package-macos.sh <version>`（`X.Y.Z`）。Apple Silicon の Mac で動かす（それ以外は失敗）。templ → esbuild + tailwind の後、`CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -tags production -trimpath`（`./backend/cmd/wails`。`-mmacosx-version-min=12.0`）で `MAM.app/Contents/MacOS/mam` を作り、`version.Version` と `version.ReleasePublicKey`（環境変数 `RELEASE_SIGNING_PUBLIC_KEY`）を `package-windows.sh` と同じ `-ldflags -X` で埋め込む。Wails CLI（`wails3`）と Taskfile は使わない（darwin の資産は `build/darwin/Info.plist` だけ。`@VERSION@` を版に置き換える）
- アイコン `icons.icns` は `build/appicon.png` から `sips` と `iconutil` でビルド時に作る（コミットしない）。`LICENSE` と `THIRD_PARTY_NOTICES.md` は `Contents/Resources/` に入れる
- バンドルは `codesign --force --deep --sign -` でアドホック署名し（arm64 のバイナリは署名が無いと起動できない）、`codesign --verify --deep --strict` で確かめる。続けて `mam --detect-gpu`（ウィンドウもデータディレクトリも使わず終わる）を実行し、バイナリが読み込めること（dyld・フレームワーク）を確かめる
- ディスクイメージは `hdiutil create`（`-volname MAM`、`UDZO`）で `MAM.app` と `/Applications` へのリンクを入れて作る。名前は `bin/mam-<version>-darwin-arm64.dmg`。サードパーティのインストーラーは使わない
- Runner（GitHub-hosted の `macos-latest`。CI は何も入れない・`sudo` を使わない）の前提: Xcode コマンドラインツール（cgo の clang。イメージに入っている）。`sips`・`iconutil`・`plutil`・`codesign`・`ditto`・`hdiutil`・`lipo` は macOS に入っている。Go と Node.js は `actions/setup-go`・`actions/setup-node` が入れる。手元の Mac では Xcode コマンドラインツールを自分で入れる（`xcode-select --install`）
- 作業ファイルは `$RUNNER_TEMP`（無ければ `$TMPDIR`）配下の実行ごとの一時ディレクトリに作り、終了時に消す（スクリプトの後始末）。Runner は実行ごとに新しい VM で捨てられるので、ワークスペースの掃除（`git clean`）はしない

### 7.2 配布（`release.yml`）

- Mac のジョブ `build-macos`（`release.yml` から再利用ワークフロー `release-macos.yml` を呼ぶ。`secrets:` は渡さない。`release-macos.yml` の `build` ジョブが `runs-on: macos-latest`）。`contents: read` で、シークレットも書き込みトークンも持たない（公開鍵は `vars.RELEASE_SIGNING_PUBLIC_KEY`。`checkout` は `persist-credentials: false`）。`setup-go`・`setup-node` は `ci.yml` と同じキャッシュ指定（`cache: true`・`cache: npm`）
- **外部の PR でリリースのビルドが動いてはならない**。このジョブは `if:` で、「`v*` タグの push」か「このリポジトリの `main` の push に対して成功した CI の `workflow_run`（`head_repository.full_name == github.repository`・`event == 'push'`・`head_branch == 'main'`・`conclusion == 'success'`）」のときだけ動く（`plan` の条件と重ねて、トリガーを広げても守られるようにする）。PR（フォークを含む）は `ci.yml` の `pull_request` で動くだけで、`release.yml` は起動しない（PR で Mac の検査をするのは `ci.yml` の `macos` ジョブで、このリポジトリのブランチの PR だけ。§7.5）
- 署名鍵 `RELEASE_SIGNING_KEY` を見るのは、専用の `sign` ジョブ（`ubuntu-latest`、`contents: read`）の最後のスクリプト工程 `Sign checksums`（`scripts/sign-checksums.sh`）だけ。`sign` は Windows の成果物（`build-windows`）と macOS の dmg（`build-macos`）の artifact を受け取り、`scripts/make-checksums.sh` で 2 つのインストーラーの `checksums.txt` を作ってから署名する。どちらかが欠けるとリリースは作られない。`release` ジョブ（`contents: write`）はすべての artifact を公開するだけ
- 信頼の範囲: dmg は実行ごとに新しい GitHub-hosted の VM（常駐する自社の機械は無い）で、`contents: read`・シークレットなし・`persist-credentials: false` で作られ、`sign` はそれをそのまま署名する。前の実行のファイルやプロセスが次の実行に残ることは無い。ビルドの工程（`go build`・`npm ci`・`package-macos.sh`）が `main` のコミットと依存のコードを実行する点は `build-windows` と同じで、署名鍵と書き込みトークンはこのジョブに渡らない
- **失敗したとき**: `build-macos` が失敗すると `sign`・`release` は動かず、リリースは作られない（Windows 版だけを先に出す構成にはしていない）。Windows 版の公開も Mac のジョブの成否に従う。ただし `ci.yml` の `macos` ジョブが同じコミットで同じ `package-macos.sh` を通すので、`main` の CI が通ったコミットでは、ビルド工程そのものの失敗は CI で先に分かる（公開鍵を埋め込む手順と、リリース時のランナーの状態の違いは CI で確かめられない）
- リリースノートは `--generate-notes` に、macOS 版の初回起動の手順（`.github/release-notes-macos.md`。下記）を先頭に付ける

### 7.3 初回起動（Gatekeeper）と自動更新

- 署名・公証が無いので、ダウンロードした dmg から入れた `MAM.app` を初めて開くと Gatekeeper が止める。右クリック →「開く」（macOS 15 以降は「システム設定 > プライバシーとセキュリティ」の「このまま開く」）、または `xattr -dr com.apple.quarantine /Applications/MAM.app`。手順はリリースノートと `README.md` に書く
- 自動更新の流れは §6 と同じ（確認・安全ゲート・署名と SHA-256 の検証・取得直後の再確認・終了して適用・再起動）で、アセットの選び方だけが違う: `updater.DefaultPlatform(runtime.GOOS, runtime.GOARCH)` が darwin/arm64 のとき `mam-<X.Y.Z>-darwin-arm64.dmg`（`Platform.InstallerName`）に**完全一致**するアセットだけを選ぶ。Windows のインストーラーや別の版の dmg は選ばず、無ければ `ErrorRelease`。`checksums.txt` の同じ名前の行で検証する。`DefaultPlatform` は darwin/arm64 と windows/amd64 以外（Linux・Intel Mac・Windows on ARM など、インストーラーを配布していない GOOS/GOARCH）ではゼロ値の `Platform{}` を返し（`Platform.Supported` が false）、そこでは自動更新を無効にする。`cmd/wails` は起動時に `desktop.CheckCanInstall` でこれを確かめ、無効のとき `Update` を渡さない（`Checker` は作られず、設定画面は「自動更新に対応していません」を出す）。ゼロ値の `Platform` の `Checker` を作ってしまった場合も、リリースを取りに行かず `ErrUnsupportedPlatform`（`ErrorRelease`・`Permanent`）で失敗するだけで、別の OS のインストーラーは選ばない
- 適用（`desktop.MacInstallCommand`。`OnShutdown` でサーバーが止まってから `desktop.Updater.RunPending` が起動する）: `/bin/sh -c <スクリプト> mam-update <pid> <dmg> <MAM.app>` を、新しいセッション（`Setsid`）の切り離したプロセスとして起動し、アプリは終わる。スクリプトは、① 旧プロセス（`<pid>`）の終了を待つ（最長 60 秒）② `hdiutil attach -nobrowse -readonly -mountpoint <一時ディレクトリ> <dmg>` で dmg を開く ③ `MAM.app` を、インストール済みのバンドルと**同じフォルダ**へ `ditto` でコピーし（同じボリュームで `mv` が rename になる）、`xattr -dr com.apple.quarantine` で隔離属性を外し（アプリが取得した dmg には元々無く、`xattr` が「無い」ことを失敗で返しても、更新は続ける）、`mv` で入れ替える（失敗したら元に戻す）④ `hdiutil detach` し、`open <MAM.app>` で新しい版を起動する。失敗したときは、インストール済みのバンドルをそのまま残し、`osascript` のダイアログで理由を知らせ、いまの版を `open` し直す。パスは引数で渡し、スクリプトの文字列には埋め込まない（テストが確かめる）。`sudo` は使わない（ユーザーが書き込めるフォルダにあるバンドルだけを更新する）
- **更新できる場所**: `desktop.MacUpdatableBundle` が、実行中の実行ファイルのパスから `*.app/Contents/MacOS/mam` のバンドルを割り出し、App Translocation（ダウンロードしたまま開いたときの読み取り専用のコピー）か、バンドルのあるフォルダに書き込めないときは更新を無効にする。書き込めるかは、そのフォルダに一時ファイルを作れるかで確かめる（書き込みプローブ）。ディスクイメージ（`hdiutil create -format UDZO` の dmg）の中は読み取り専用のボリュームなので、このプローブが失敗して無効になる。`/Volumes/` で始まるかどうかは見ない（書き込める外付けドライブなどの上の `MAM.app` は、ほかのフォルダと同じく更新できる。dmg のマウントを `statfs` などで見分ける方法は、プローブが同じ判定をすでにしており、macOS 専用のコードが増えるだけなので取らない）（起動時の `desktop.CheckCanInstall`。無効のとき `cmd/wails` は `Update` を渡さず、設定画面は「自動更新に対応していません」と、`MAM.app` を `/Applications` に移して起動し直す案内を出す）。確認ボタンも出ない
- 設定画面の更新状態の表示（§6.5・§6.7）と「今すぐ確認」は、`appupdate.Service` が `cmd/wails` から渡されたときに出る。macOS 版でも Windows 版と同じ実装で動く（OS で分岐する箇所は無い）

### 7.4 `cmd/wails` の macOS での違い

- ウィンドウ（`Title`・既定サイズ・最小サイズ・背景色）・単一インスタンス（`UniqueID: com.ousiassllc.mam`）・終了（`OnShutdown` でサーバーを止める）・更新の有効化（`main.go` の `updateOptions`。`desktop.CheckCanInstall` が通るときだけ `Update` を渡す）は Windows と同じコード（`main.go`）。違いは `platform_darwin.go`（ナビゲーション完了のイベントは `events.Mac.WebViewDidFinishNavigation`、`Mac.ApplicationShouldTerminateAfterLastWindowClosed = true`）と `dialog_darwin.go`（起動エラーのダイアログは `osascript` の `display alert`。Wails のダイアログはイベントループが要るため使わない。タイトルと本文は引数で渡す）
- データディレクトリの既定は `os.UserConfigDir()` 配下の `mam`、つまり `~/Library/Application Support/mam`。API キーなどは macOS のキーチェーンに保存する
- macOS 版で動かない・制限がある機能（別 Issue）: 同梱の ComfyUI エンジン（#854）・Codex 本体の取得（#855）。顔同一性の定量評価（ONNX Runtime）はブラウザ版と同じく、`libonnxruntime` とモデルを自分で用意したときだけ動く
- 実機でしか確かめられない項目（WKWebView でのダウンロード・`target="_blank"`・WebSocket、単一インスタンス、⌘Q での終了、Gatekeeper の手順、実際の `open` での再起動、2 つの実際のリリースをまたぐ更新、App Translocation の挙動）は未確認で、#857 で行う。`cmd/wails` の darwin でのコンパイルと、dmg の作成・入れ替えスクリプトの本物の `hdiutil`・`ditto` での実行は、`ci.yml` の `macos` ジョブが確かめる（§7.5）

### 7.5 テストできること・できないこと

- 単体テスト（Linux の CI）: アセット名の選択（`Platform`・`DefaultPlatform`・`selectAssets`。darwin/arm64 の dmg だけを選ぶ・Windows のインストーラーと取り違えない・未対応の GOOS/GOARCH ではゼロ値になり、その `Checker` は通信せずに失敗する・`desktop.CheckCanInstall` がそこで自動更新を無効にする）、dmg の取得と検証（`Checker`）、バンドルのパスの割り出し（`MacAppBundle`・`MacUpdatableBundle`。`/Volumes/` 配下は、読み取り専用のボリュームを模したプローブの失敗で無効、書き込めるボリュームで有効になること）、コマンドの組み立て（`MacInstallCommand`）、入れ替えスクリプトの実行（`hdiutil`・`ditto`・`xattr`・`open`・`osascript` を模した実行ファイルで、手順の順序・失敗時にバンドルが変わらないこと・後始末を確かめる）、`scripts/make-checksums.sh` のテスト。`GOOS=darwin GOARCH=arm64 CGO_ENABLED=0` の `go build`・`go vet`・golangci-lint（`cmd/wails` を除く）は `ci.yml` の `ci` ジョブ（Linux）が通す
- **macOS 上のネイティブの検査（`ci.yml` の `macos` ジョブ。GitHub-hosted の `macos-latest`、Apple Silicon）**: `main` への push と、このリポジトリのブランチからの PR だけで動く（フォークの PR では動かない）。`contents: read`・シークレットなしで、次を確かめる。(1) `bash scripts/package-macos.sh 0.0.1`: リリースと同じスクリプトで `cmd/wails` の darwin 版（cgo・Apple SDK）をコンパイルし、`MAM.app` のアドホック署名と `codesign --verify --deep --strict`・`mam --detect-gpu`（バイナリが読み込めること）・`hdiutil create` までを通して `bin/mam-0.0.1-darwin-arm64.dmg` を作る（公開鍵は埋め込まない。この dmg は公開せず、3 日だけ残る artifact `macos-dmg-<run_id>`）。(2) `go vet -tags production ./backend/cmd/wails/`（darwin・cgo）。(3) `go test` の `./backend/internal/desktop/...`・`./backend/internal/service/updater/...`・`./backend/internal/service/appupdate/...` を macOS 上でネイティブに実行する（darwin 向けのアセット選択・バンドルとインストールの経路）。(4) `TestMacInstallScript_RealDmgReplacesTheBundle`（`backend/internal/desktop/macinstall_darwin_test.go`。環境変数 `MAM_TEST_DMG` に (1) の dmg を渡したときだけ動き、無ければ Skip）: 更新を適用するスクリプトを、いま作った dmg に対して、本物の `hdiutil`・`ditto`・`xattr`・`mv` で実行する（模すのは `open` と `osascript` だけ）。入れ替わったバンドルが `codesign --verify` を通り、`mam --detect-gpu` が動くこと、ステージしたコピー・旧バンドル・マウントポイントが残らないこと、dmg が detach されていること、失敗のダイアログ無しで新しい版が元の場所で `open` されることを確かめる
- **未確認（macOS 実機・人の操作が要る。CI では確かめない）**: Gatekeeper の初回起動の挙動（右クリック →「開く」の手順。隔離属性の付いた dmg から人が開く操作）、WKWebView の挙動（ダウンロード・`target="_blank"`・WebSocket、ウィンドウが開くこと）、単一インスタンスと ⌘Q での終了、本物の `open` による再起動（CI は `open` を模し、新しい版のウィンドウが開くことは見ない）、2 つの実際のリリース（旧版 → 新版）をまたぐ更新（CI は、現在のコミットから作った dmg を、ダミーの旧バンドルへ入れ替えるだけで、GitHub の Release からの取得・署名の検証・安全ゲートを含む流れ全体は通さない）、App Translocation の実際の挙動（`MacUpdatableBundle` が割り出す判定は単体テストだけ）、公開鍵を埋め込んだ release の dmg（CI の dmg には公開鍵が無い）。`macos-latest` の指す OS・Xcode の版は GitHub のイメージ更新で動き、固定していない

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-03 | 新規作成。`spec/architecture/overview.md` §11 に書いた Windows デスクトップ版（方式・`cmd/wails` の挙動・未検証項目・パッケージング）をこのファイルへ移した | Issue #480。`overview.md` が行数上限（300 行、`.linterly.yml`）を超えるため、`model-adapters.md` と同じく `spec/architecture/` 内の別ファイルに切り出した |
| 1.1 | 2026-10-11 | §2 に構成（`internal/desktop`）・背景色・ダイアログの実装方法・終了時の確認ダイアログなしを、§3 に v3.0.0-beta.27 のソースで確かめた結果（`DownloadStarting`・`NewWindowRequested` が非公開、ランタイムは露出）と実機で未確認の項目を追記 | Issue #480 の実装 |
| 1.2 | 2026-10-11 | §3 のダウンロードの実機確認の対象に、Review Package の ZIP のリンク（`organisms/review_list.templ`）を加えた | Issue #514。`<a download>` を持つテンプレートは 4 つあるのに列挙が 3 つで、リリース前の実機確認が Review Package のダウンロードを確認漏れにするため（応答は `handler/review.go` の `Content-Disposition: attachment`） |
| 1.3 | 2026-10-12 | §4 の配布の項に、`release.yml` が `verify` → `windows-latest` の `build` → `release` の 3 ジョブで、書き込み権限は `release` だけが持つことを追記した（本文は 56a6e39 で書き換え済み。版の行をこの版で足した） | Issue #565。`release.yml` を検証・ビルド・公開に分け、ビルド工程が書き込み用トークンを持たない構成にしたため（`56a6e39`、`setup.md` 1.11）。`desktop.md` の改訂履歴だけ行が足されていなかった |
| 1.4 | 2026-10-12 | §2 の終了に、10 秒（`shutdownTimeout`）で終わらないリクエストは中断し（コンテキストをキャンセルして接続を閉じる）、ハンドラが戻るまで最大 5 秒（`abortTimeout`）待ってからジョブキューや DB を閉じること、ウィンドウを閉じてからアプリが終わるまでの最長が約 15 秒であることを足した | Issue #597。#577 が `runHTTP` に中断フェーズを足したのに、終了の記述がグレースフルシャットダウンの 10 秒だけだった |
| 1.5 | 2026-10-12 | §3 のダウンロードの実機確認の対象に、チャットの書き出し完了通知のリンク（`molecules/text_message.templ`。`ref_type=export`）を加えた | Issue #648。#631 が 5 つ目の `<a download>`（「書き出した動画をダウンロード」）を足したのに、列挙が 4 つのままで、リリース前の実機確認がこのリンクを確認漏れにするため（#514 と同じ種類の列挙漏れ） |
| 1.6 | 2026-10-12 | §3 の `target="_blank"` リンクの実機確認の対象のうち、存在しない `world_state_view.templ` を実体の `organisms/worldstate/view.templ` に直した | Issues #690・#691。#685・#686 が World State の表示を `organisms/worldstate/` サブパッケージ（`view.templ`・`responses.templ`・`data.go`）へ移し、`world_state_view.templ` が無くなったのに列挙が旧名のままで、実機確認の担当者がこのリンクを探せず確認漏れにするため（#514・#648 と同じ種類の列挙のずれ）。 |
| 1.7 | 2026-10-13 | §3 のダウンロードの列挙で `Content-Disposition: attachment` を返すハンドラに `handler/editor.go` の `ExportVideo` を足した | Issue #706。書き出した動画のダウンロードにも保存名（`mam-export-<日付>-<exportId>.<ext>`）を付けたため、実機確認の担当者が列挙から保存名の確認対象を読み取れるように |
| 1.8 | 2026-10-16 | §3 のダウンロードの実機確認の列挙で、存在しない `editor_panel.templ` を実体の `organisms/editorpanel/stages.templ`（書き出しの一覧の `<a download>`）に直した | Issues #737・#740。#731 が `editor_panel.templ` を `organisms/editorpanel/` サブパッケージへ分けて無くなったのに列挙が旧名のままで、実機確認（Issue #480）の担当者が確認対象を探せないため（1.6 と同じ種類の列挙のずれ） |
| 1.9 | 2026-10-16 | §3 のダウンロードの実機確認の列挙で、`review_list.templ` を移動先の `organisms/reviewlist/list.templ` に直した | Issue #743。レビュー一覧をサブパッケージ `organisms/reviewlist/` に移したため |
| 1.10 | 2026-10-16 | §4 のパッケージングの項に、`wails3 package` は `build/config.yml` を読まず、exe のリソースは `build/windows/info.json`（`package-windows.sh` がリリース版を書いた複製を `INFO_JSON` で渡す）、インストーラーの版は `VERSION` が入力であることを足した | Issue #766。`build/config.yml` のコメントが「`package-windows.sh` が `version` を上書きし `wails3 update build-assets` を実行する」と書いていたが、スクリプトはどちらもせず、`config.yml` を書き換えても成果物に反映されないため、リリース担当者が誤った場所を直す恐れがあった（コメントも実態に合わせた） |
| 1.11 | 2026-10-16 | §2 に、ffmpeg / ffprobe の起動でコンソールウィンドウを出さない（`internal/media` の `run` が Windows で `HideWindow` と `CREATE_NO_WINDOW` を付ける）ことを足した | Issue #770。`-H windowsgui` の GUI プロセスはコンソールを持たないため、子のコンソールアプリ（ffmpeg / ffprobe）を起動するたびに新しいコンソールウィンドウが点滅しフォーカスを奪っていた |
| 1.12 | 2026-10-16 | §2 の「子プロセス」に、`HideWindow` と `CREATE_NO_WINDOW` が付くことを `internal/media/run_windows_test.go` が確かめ、リリースの `build` ジョブが Windows 上で動かすことを足した | Issue #774。#770 のテストは Linux だけの `ci.yml` でも `release.yml` でも実行されず、検証する手段がどこにも書かれていなかった（setup.md の改訂 1.27 で `release.yml` に手順を足した） |
| 1.13 | 2026-10-16 | §2 の「子プロセス」で、`run_windows_test.go` が確かめる内容を、`hideWindow` 単体の検査と、`run` を実 exe で通して `exec.Cmd` を見る検査（`TestRunHidesConsoleWindow`）に分けて書いた | Issue #777。従来のテストは `hideWindow` を直接呼ぶだけで `run` を通らず、`run` から `hideWindow` を消しても通ったため、記述（`run` の退行を検出する）が実態と合っていなかった（setup.md の改訂 1.29） |
| 1.14 | 2026-10-21 | §4 のバージョンと配布の項を、`main` の CI 成功後に次の patch 版を自動でリリースする構成（手動の `v*` タグは minor・major を上げる用）と、`plan` を加えた 4 ジョブへ改めた | `release.yml` を、タグを手で打たなくても `main` への push で配布物が出るようにしたため |
| 1.15 | 2026-10-21 | §4 の配布の項と §3 の未確認事項を、リリースを self-hosted の Linux runner でのクロスビルドへ移したことに合わせた | 初回のリリース実行が GitHub-hosted runner の支払い失敗で動かず、self-hosted へ移したため（setup.md の改訂 1.31） |
| 1.16 | 2026-10-24 | §4 の配布の項の実行先を、self-hosted の Linux runner から GitHub-hosted の `ubuntu-latest` に改めた（Windows 版は引き続き Linux 上のクロスビルド） | リポジトリを公開する準備として CI と `release.yml` を GitHub-hosted runner へ戻したため（Issue #825、setup.md の改訂 1.33） |
| 1.17 | 2026-10-24 | §4 の成果物の項に、インストーラーが `LICENSE` と `THIRD_PARTY_NOTICES.md` をインストール先に置くことを足した | Issue #826。ライセンスを AGPL-3.0 にし、サードパーティのライセンス表記を足した（`build/windows/nsis/project.nsi` が 2 つのファイルを置く）。ポータブル版の表記の扱いは決めていない |
| 1.18 | 2026-10-21 | §6 に自動更新（流れ・安全ゲート・署名と検証・ユーザー単位のインストール・再起動・テストの範囲・設定画面）を足し、§4 のインストール先をユーザー単位に、§5 の対象外から自動更新を外した | Windows 版が新しいリリースを自分で入れ替えるようにしたため。リポジトリは非公開なので、リリースの配布物だけを持つ公開リポジトリ `ousiassllc/mam-releases` を確認する |
| 1.19 | 2026-10-24 | §6.1・§6.3 の更新の確認先を、別の公開リポジトリ `ousiassllc/mam-releases` から MAM 自身の Releases（`ousiassllc/mam`）に改めた。非公開の間は 404 で「リリースなし」になること、古い版からのつなぎ（`mam-releases` を見る版は配布されておらず、v0.1.0・v0.1.1 は手動で入れ替える）を足した | 公開後は mam 自身の Release から配る（Issue #827） |
| 1.20 | 2026-10-24 | §4 の配布の「2 つの成果物」を、実際の 4 ファイル（インストーラー・ポータブル・`checksums.txt`・`checksums.txt.sig`）に直した。§6.1 のインストーラーの選び方と §6.3 のチェックサムの引き方を、リリースの `tag_name` の版が入った名前との完全一致にした（別の版のインストーラーは選ばず `ErrorRelease`、別の名前の行しか無ければ `ErrorVerification`）。§6.3 の署名を作る場所を、`build` ジョブの最後のステップ `Sign checksums`（同じステップで公開鍵を使って検証。`release` ジョブは署名しない）に直した。§6.5 に、確認の失敗を定期確認でも手動確認でも `Service.Check` が `ERROR` で 1 回記録することを足した | PR #824 のレビュー指摘。インストーラーを glob で選ぶと、タグと無関係な版のインストーラーを入れてしまう。署名の場所と成果物の数が実装（`release.yml`）と食い違っていた。手動確認の失敗が（`ErrorVerification` を含め）ログに残らなかった |
| 1.21 | 2026-10-24 | §1 に、MCP サーバーが `internal/app` の中で起動するため Wails 版でも使えることを足した | Issue #830。Windows デスクトップ版でも MCP に接続できることが完了条件のため |
| 1.22 | 2026-10-25 | §4 に、ComfyUI は同梱せず `<mam.exe のディレクトリ>\comfyui` へ実行時に取得すること、`RMDir /r $INSTDIR` でアンインストールが消すことを、§6.2 の安全ゲートに `model_install`・`gentest` を、§6.8 に更新と `comfyui` ディレクトリの関係を足した | Issue #833。インストーラーの大きさと更新の上限を保つため、エンジンは利用者の操作で取得する |
| 1.23 | 2026-10-25 | §6.8 に、アンインストーラーが動いている `mam.exe` の終了を待ち（`un.onInit`）、`comfyui` ディレクトリが消えるまで `RMDir /r` をくり返すことを足した | Issue #833。ComfyUI の子プロセスが放すまでの間にファイルが消えないことがある |
| 1.24 | 2026-10-27 | 冒頭と §2・§4・§5・§6 を、macOS（Apple Silicon・署名なし）の追加に合わせて直し、§7（macOS 版: ビルドとパッケージング・配布・初回起動と自動更新・`cmd/wails` の違い・テスト）を足した。`release.yml` を `plan` → `verify` → `build-windows` → `build-macos` → `sign` → `release` の 6 ジョブにし、チェックサムと署名を専用の `sign` ジョブに移した | Issue #853。macOS 版（Apple Silicon・署名なし）を同じリリースで配布し、自動更新に対応する。署名鍵を最後の工程だけが見る構成を保つため、Windows と macOS の 2 つのインストーラーをまとめて署名する `sign` ジョブを分けた |
| 1.25 | 2026-10-27 | §4 の配布の項・§7.1・§7.2 を、macOS 版のビルドを組織の self-hosted の Mac runner から GitHub-hosted の `macos-latest` へ移したことに合わせて直した（常駐 runner の作法（`$RUNNER_TEMP` の掃除と `git clean -ffdx`）を除き、信頼の範囲を実行ごとに新しい VM に改め、`build-macos` の失敗で `sign`・`release` が動かないことを明記した）。§7.4・§7.5 に、`ci.yml` の `macos` ジョブが macOS 上でネイティブに確かめること（dmg の作成・`cmd/wails` の darwin での vet・更新まわりのテスト・本物の dmg への更新の適用）と、なお実機でしか確かめられないことを書いた | Issue #853。Mac の常駐する機械を持たずに macOS 版をビルドし、CI で dmg の作成と更新の適用を確かめるため（`setup.md` の改訂 1.43） |
| 1.26 | 2026-11-01 | §7.3 の自動更新に、`DefaultPlatform` が darwin/arm64・windows/amd64 以外ではゼロ値を返し自動更新を無効にすること（`Platform.Supported`・`desktop.CheckCanInstall`・`ErrUnsupportedPlatform`）を、§7.5 のテストの範囲にその確認を足した | Issue #859。darwin/amd64 などで自前ビルドすると、Windows の setup.exe を選んでダウンロードし、適用で失敗していたため |
| 1.27 | 2026-11-01 | §7.3 の「更新できる場所」から `/Volumes/` 配下を一律に除く判定を外し、バンドルのあるフォルダの書き込みプローブに任せることにした（dmg のボリュームは読み取り専用なので、プローブで無効になる）。§7.5 のテストの範囲に、プローブを差し替えた `/Volumes/` 配下の確認を足した | Issue #860。外付けドライブなど `/Volumes/` 配下の書き込める `MAM.app` も自動更新が無効になり、コメントと仕様が「ディスクイメージ」と説明する実態より広く拒否していたため |
| 1.28 | 2026-11-01 | §7.4 の差分の説明から `platform_darwin.go` の「更新の有効化」を外し、更新の有効化は OS 共通の `main.go` の `updateOptions`（`desktop.CheckCanInstall`）が行うと書いた | #859 の統合で `updateOptions` が `platform_*.go` から `main.go` に移り、仕様の説明が実装とずれたため |
| 1.29 | 2026-11-01 | §2 の ffmpeg の探し方と §7 の macOS の制限から、Finder から起動したときの ffmpeg の探索（#856）を外し、設定のパスと Homebrew の場所を探すこと（[ffmpeg-lookup.md](ffmpeg-lookup.md)）を書いた | Issue #856 |
