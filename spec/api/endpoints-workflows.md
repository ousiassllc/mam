# エンドポイント: ノード画面と ComfyUI ワークフロー

[endpoints.md](endpoints.md) の続き（Issue #834）。画面の意味は [../architecture/comfy/comfy-embedding.md](../architecture/comfy/comfy-embedding.md)・[../architecture/comfy/comfy-workflows.md](../architecture/comfy/comfy-workflows.md)。ハンドラは `web/handler/workflows`、登録は `router/settingsroutes`（`Services.Workflows` が無いとルートは無い）。HTMX の変更系は `X-CSRF-Token` 必須（CSRF ミドルウェア）。プロジェクトの一覧ページ（`/`）に「ノード（ComfyUI のワークフロー）」のリンクがある。

| メソッド | パス | 説明 |
|---|---|---|
| GET | `/nodes` | ノード画面（`paths.Nodes()`）。ComfyUI のノード UI を `<mam-comfy-frame>`（iframe）で出す。`?endpoint=<名前>` で登録した外部の ComfyUI を選ぶ（既定: 同梱版）。`?opened=mam/<名前>.json` は「ワークフロー」から開くファイルの案内。応答の CSP は、描画する ComfyUI のオリジンだけの `frame-src` を持つ（開けないときは持たない）。開けないときは理由・モデルマネージャー（`/model-manager`）への導線・「ブラウザで開く」を出す |
| POST | `/nodes/start` | 同梱版の ComfyUI を起動する。成功は `HX-Redirect: /nodes`（204）、同梱版が無い・起動に失敗は 422 でその理由を載せたノード画面 |
| GET | `/workflows` | 保存したワークフローの画面（`paths.Workflows()`）。HTMX 要求は区画だけ。一覧（名前・種類・版・識別子・実行できるか・使われている用途）、貼り付け・ファイルの取り込みフォーム、「ComfyUI から取り込む」 |
| POST | `/workflows/import` | API 形式の JSON の取り込み（multipart: `name`, `kind`, `endpoint`, `api_json` か `api_file`）。成功は 200（保存時の照合の警告つき）、重複は 409、不正は 422 |
| GET | `/workflows/remote` | `?endpoint=<名前>`: ComfyUI サーバーが持つワークフローの一覧（取り込みフォーム） |
| POST | `/workflows/import/comfyui` | ComfyUI から取り込む（`remote_path`, `name`, `kind`, `endpoint`, `replace`）。実行が無いなどは 422 |
| GET | `/workflows/:id/edit` | 編集フォーム: 名前・接続先・入力の割り当て（ノードの一覧から選ぶ。`/object_info` で型を引く）・出力ノード・動画の設定 |
| PUT | `/workflows/:id` | 保存する。変わっていれば版が上がる。不正は 422（フォームを残す） |
| DELETE | `/workflows/:id` | 削除する。使われていれば 422 |
| POST | `/workflows/:id/check` | つないでいる ComfyUI との照合（足りないノード・モデルの警告、つながらない理由） |
| POST | `/workflows/:id/open` | 画面の形式を ComfyUI の `workflows/mam/` へ書き、`HX-Redirect` でノード画面へ（204） |

| 版 | 日付 | 変更内容 | 変更理由 |
|---|---|---|---|
| 1.0 | 2026-10-25 | 新規。ノード画面（`/nodes`・`/nodes/start`）と保存したワークフローの画面（`/workflows/…`） | Issue #834 |
