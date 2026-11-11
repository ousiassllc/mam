# エンドポイント: ジョブ一覧

[endpoints.md](endpoints.md) の続き（Issue #980）。裏で動く作業（モデルの導入・単発生成・Shot の生成・Judge・Repair・企画・動画の書き出し・音声・Editor の判定・ワークフローの実行・Codex の導入・ffmpeg のダウンロード）の、いま動いているものと直近に終わったものを見る画面。意味・表示・a11y は [../components/jobs-screen.md](../components/jobs-screen.md)。**読み取り専用**で、ジョブを変更するルートは無い（キャンセルは導入・単発生成それぞれの画面のルート）。

| メソッド | パス | 内容 |
|---|---|---|
| GET | `/jobs` | 画面。`pages.JobsPage`（`paths.Jobs()`）。左メニューの「ジョブ」が現在の画面 |
| GET | `/jobs/list` | 一覧の区画だけ（`#jobs-list`。`paths.JobsList()`）。更新ボタン（`hx-get`・`outerHTML`）と、動いているジョブがあるあいだの 2 秒ごとの取り直し（`hx-trigger="every 2s"`）が使う |

- どちらも**常に 200**（`Cache-Control: no-store`）。`job` テーブルを読めなかったときは、一覧の中に `role="alert"` の文を出す（原因は応答に出さず、ルーターのロガーに残す）。
- 読むのは `job` テーブル（新しい 50 件＋終わっていないものすべて）と、種類ごとの対象・進み具合（`modelmanager`・`gentest` のサービス、Project・Shot の読み取り、動いている ComfyUI の進み具合）、それに `job` の行を持たない 6 種の作業（動画の書き出し・音声の作成・Editor の判定・ワークフローの実行・Codex の導入・ffmpeg のダウンロード）の、持ち主のメモリ上の記録（`internal/worktrack`。Issue #1019。再起動で消える。[jobs-screen.md](../components/jobs-screen.md) §2）。外部の API は呼ばない。何も書き込まない。
- ルートは、サービスがあるときだけ登録される（`settingsroutes.Services.JobList`。`internal/app` が組み立てる。`nil` なら 404）。
- 一覧の行は `data-testid="jobs-item"`、リンクは `data-testid="jobs-open"`（行き先は `paths.ModelManager()`〔#1101 で、導入のジョブは `target` に応じたモデルマネージャーの分類ページ。[../components/model-manager-screen.md](../components/model-manager-screen.md)「分類のサブページ（#1101）」〕・`paths.GenerationTestHistory()`・`/projects/:id/scenes`・`/projects/:id`、メモリ上の作業は `/projects/:id/edit`・`paths.Workflows()`・`paths.FFmpegSettings()`・`/settings/api-keys`）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-03 | 新規作成 | Issue #980 |
| 1.1 | 2026-11-03 | 読む対象に、`job` の行を持たない 6 種の作業のメモリ上の記録を足した。ルートは変わらない | Issue #1019 |
| 1.2 | 2026-11-06 | 導入のジョブの行の行き先を、`target` に応じたモデルマネージャーの分類ページにすると決めた（ルートは変わらない。状態は仕様のみ） | Issue #1101 |
| 1.3 | 2026-11-07 | 導入のジョブの行の行き先（`joblist.Dest.Category`）を実装した。`engine`→`comfyui`・`model:`→`image-video`・`runtime:`→`local-runtimes`・`node:`→`custom-nodes`・`hf:`→`hf-external`、ほかは概要 | Issue #1101 |
| 1.4 | 2026-11-10 | 画面名・機能名の「生成テスト」を「単発生成」に改めた（本文の文言だけ。仕様は変えない。過去の改訂履歴の文は当時のまま） | Issue #1137 |
