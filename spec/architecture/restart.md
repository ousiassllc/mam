# アプリの再起動と「再起動して反映」

Issue #1006・#1007。設定を保存しても動いているアプリに反映されないものがあるとき、画面のボタンと MCP の `restart_app` で、実行中の作業を確かめてからアプリを再起動する。あわせて、LLM の割り当てを再起動なしで反映するようにした（これで再起動が要る設定は §2 の 2 つだけになった）。

実装: `internal/service/apprestart`（確認と要求）・`internal/service/restartpending`（再起動待ちの設定の一覧）・`internal/desktop`（`Restarter`・`RelaunchCommand`・`WaitForPredecessor`）・`cmd/wails/main.go`・`internal/app/restart.go`（作業の確認）・画面は `handler/settings/restart` と `organisms/settings/restart`・MCP は `tools/settings/restart.go` と `tools/health`。

## 1. LLM の割り当ては再起動なしで反映する

- 画面・MCP の保存（`llmassign.Service.Save` / `Reset`）は、`llm.json` に書いたあと、その設定を実行中のクライアント（`llm.Client`）に渡す（`llmassign.Service.SetApplier` → `llm.Client.Apply`）。**次の呼び出しから**新しい主・代替が使われる。呼び出しの途中のものは、始めたときの割り当てのまま終わる。
- 渡せるのは `routes` だけ。クライアントは起動時に作ったバックエンド（`base_url`）・タイムアウト・リトライ・サーキットブレーカーを持つので、`llm.json` を手で書き換えてそれらを変えたときだけ、`Apply` が「再起動で反映される設定」を返す（§2）。
- 単発生成の見積り（`prompttranslate.Translator`・`imagematch.Matcher`）が名指す主のモデルも、呼び出し時の割り当てを読む（`llm.Client.PrimaryOf`）。
- `--llm-config` / `MAM_LLM_CONFIG` で固定されたファイルは、画面も MCP も書かない（従来どおり）。手で書き換えたら、再起動で反映される。

## 2. 再起動が要る設定

| 設定 | 理由 | 画面・MCP での出方 |
|------|------|-------------------|
| `llm.json` の `providers.*.base_url`・`timeout_seconds`・`retry`・`breaker`（手で書き換えたとき） | クライアントとバックエンドを起動時に作る | 「llm.json の…」（`restart_pending`） |

これ以外（LLM の割り当て・画像／動画モデルの割り当て・ローカル・代替モデル・音声・口パクのプロバイダー・API キー）は、保存するとすぐ反映される。再起動待ちの一覧は `restartpending.Registry`（保存する側が `Set`、一覧は `Reasons`）。再起動すれば空になる（保存したものを起動時に読むため）。

## 3. 再起動の要求（`apprestart.Service.Request`）

画面のボタンか MCP の `restart_app` が呼ぶ。**頼まれたときだけ**動く（自動では再起動しない。MCP は `confirm: true` が無ければ動かない（必須の引数: 省略はスキーマで拒否、`false` は `validation`））。

- 動いている作業を `Deps.Busy` で調べる（`internal/app/restart.go`）: `jobs`（ジョブキューの `running` / `queued`。モデル等の導入を除く）・`editing`（編集の工程・AUTO の連鎖）・`media`（ffmpeg）・`workflow_run`（保存したワークフローの実行＝同梱 ComfyUI が生成中）・`download`（モデル・エンジン・ノード・ローカルランタイムの導入ジョブと ffmpeg のダウンロード、ポーズ集の導入、Codex エンジンの導入）・`check_failed`（調べられなかった。何も無いとは見なさない）。自動更新の安全ゲート（desktop.md §6.2）と同じ情報源に、ダウンロードとワークフロー実行を足したもの。
- 何も動いていなければ、すぐに前面（`Options.Restart`）へ終了と再起動を頼む。動いているとき、`when_busy`:
  - `refuse`（既定）: 再起動せず、動いている作業を `conflict`（画面は 409 と一覧）で返す。何も変えない。
  - `wait`: すぐ `state: "waiting"` を返し、5 秒ごとに見て、何も動かなくなったら再起動する。上限（既定 30 分・最大 24 時間）を過ぎたら再起動せずにやめる。待っている間は `get_health` の `restart_waiting` が `true`。待っている間に別の要求が先に再起動を始めたときは、待ちは何もせずに終わる（前面へ頼むのは 1 回だけ）。
  - `stop`（MCP だけ。画面には出さない）: 中断して今すぐ再起動する。生成などのジョブは `interrupted by shutdown` で失敗し（再開できるジョブは再起動後に続く）、編集・ワークフロー実行は中断される。**`download` が動いていれば `stop` でも `conflict`**: 数十 GB の途中のファイルを、誰も待たない再起動で残さないため。止めたいなら導入をキャンセルするか、`wait` で待つ。
- **ローカル ComfyUI を壊さない**: 再起動は自動更新と同じ穏やかな終了（`Server.Stop` → サービスの `Close`: ジョブキューの停止・同梱 ComfyUI の子プロセスの停止・ダウンロードの中止（ffmpeg は途中のファイルを残し、次の導入が続きから再開））を通る。プロセスを強制終了しない。
- ブラウザ版（`cmd/server`。自分のプロセスを再起動する前面が無い）は `Options.Restart` が無く、`restart_app` は `unavailable`、画面は「アプリを終了して、起動し直してください」と出す。

## 4. 画面の「再起動して反映」

設定画面（`/settings` と全分類）の本文の上に、`#restart-slot`（常に在る）の中の欄を出す。再起動待ちの設定があるとき（§2）または要求に答えたとき: 待っている設定を並べ、`Services.Restart` が再起動できる版なら「再起動して反映」（`POST /settings/restart`、`when_busy=refuse`、`hx-confirm`）を出す。動いている作業で断られたときは 409 で作業の一覧と「作業が終わったら再起動する」（`when_busy=wait`）を出す。再起動できない版は「このビルドは自分で再起動できません」。

欄は、設定を保存する HTMX の要求のあと（GET 以外。自分のボタンを除く）、`GET /settings/restart` で読み直す（保存のあとに欄が現れる）。

## 5. 再起動の実行（`internal/desktop`）

既存の自動更新の仕組み（desktop.md §6）をそのまま使う: `Restarter.Restart` が Wails の `Quit()` を別 goroutine で呼ぶ → `OnShutdown` でサーバー・ジョブキュー・DB が閉じる → `Restarter.RunPending` が、切り離したプロセス（`detach`: Windows は `DETACHED_PROCESS`、それ以外は `Setsid`）で自分を起動し直す。自動更新が待っているとき（`Updater.Pending`）は、インストーラーが再起動するので、こちらは起動しない。

- **Windows**: `RelaunchCommand` は、同じ実行ファイルを同じ引数（`os.Args[1:]`）で、環境変数 `MAM_RESTART_WAIT_PID=<旧プロセスの pid>` を付けて起動する。新しいプロセスは `main` の最初（`application.New` の単一インスタンスの確認より前）に `WaitForPredecessor` で、旧プロセスが終わるのを待つ（`OpenProcess(SYNCHRONIZE)` と `WaitForSingleObject`。最大 60 秒）。旧プロセスはデータディレクトリのロックと単一インスタンスのロックを終了まで持つので、待たずに起動すると新しいほうが追い返される。待ち終わったら変数を消す（さらに起動するプロセスに引き継がない）。
- **macOS**: `/bin/sh -c <スクリプト> mam-restart <pid> /usr/bin/open <MAM.app> --args <引数…>`（`.app` の外では実行ファイルそのもの）。スクリプトは pid の終了を待ってから（最大 60 秒）`exec` する。`open` を使うのは、ふつうの起動と同じく Dock とウィンドウが前に出るため（自動更新の `macInstallScript` と同じ理由）。
- ポートは、旧プロセスが終わっていれば同じ（既定 8787・MCP 8788）。MCP のクライアントは数秒の切断のあとつなぎ直す。`get_health` の `boot_id`（実行ごとの ID。`apprestart.Deps.BootID`）が変われば再起動できている。

## 6. テストできること・できないこと

- 単体テスト: `Request` の 3 つの扱い・`stop` が `download` を断ること・`wait` の待ちと打ち切り・待ちの間に別の要求が先に再起動したときに前面へ 2 度頼まないこと・再起動できない版・`RelaunchCommand`（Windows・macOS のコマンドの組み立て）・`Restarter`（終了の要求と、サーバーが止まってからの起動の順序・起動の失敗）・`WaitForPredecessor`（実際の子プロセスの終了）・`Client.Apply`・`llmassign` の適用・MCP `restart_app`（`confirm` なしは動かない・refuse の conflict・wait・stop・unavailable）・画面の欄。**再起動の実行部分は差し替え（`Deps.Restart`・`Restarter.start`）で検証し、実プロセスは再起動しない。**
- **未確認（実機が要る）**: Windows で本物の `Quit()` → 新しいプロセスの起動 → 旧プロセスの終了待ち → ウィンドウが開くこと（切り離したプロセスが親の終了後も生き残ること、単一インスタンスのロックの解放）、macOS で本物の `open` による再起動と Dock・ウィンドウ、同梱 ComfyUI が再起動の前後で止まって起動し直すこと、ダウンロード中の `refuse` / `stop` の実機の挙動、WebView2 / WKWebView での欄の表示と保存後の読み直し。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-03 | 新規作成。LLM の割り当ての即時反映、再起動が要る設定、再起動の要求（`refuse` / `wait` / `stop`）、画面の欄、再起動の実行 | Issue #1006・#1007 |
| 1.1 | 2026-11-03 | §2: `Busy` の `download` に、ポーズ集の導入と Codex エンジンの導入を足した。§3: `wait` の待ちは、待っている間に別の要求が先に再起動を始めていたら、前面へ再起動を重ねて頼まない | 統合後レビューの指摘（再起動の確認が、ポーズ集と Codex の導入を数えず、待ちが再起動を 2 度頼んだ） |
| 1.2 | 2026-11-06 | §2 から ffmpeg・ffprobe のパスを外した。`media.Tools` を差し替え可能にし、保存・ダウンロードのすぐあとに全サービスが新しい ffmpeg を使うため、再起動は要らない（`FFmpegPaths` と「再起動して反映」の欄の項目をなくした） | Issue #1047 |
