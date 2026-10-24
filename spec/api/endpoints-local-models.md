# API 仕様: 設定画面「ローカル・代替モデル」

[endpoints.md](endpoints.md) §1 の `GET /settings` の区画「ローカル・代替モデル」と、そのアクションルート（Issue #831）。接続先・モデルの意味、`models.json` のスキーマ、識別子は [../architecture/local-models.md](../architecture/local-models.md)。画面の操作は、同じサービス（`localmodels.Service`）を呼ぶ MCP ツールでもできる（[mcp-tools-local-models.md](mcp-tools-local-models.md)）。

## 1. 画面

区画は `#local-models-panel` で、「画像・動画モデル」の次に並ぶ（`localmodels.Service` を持たない router では区画もルートも無い）。

- **接続先の一覧**: 名前・種類（ComfyUI / OpenAI 互換）・`base_url`・同時実行数・タイムアウト（既定を反映した値）。行ごとに「接続テスト」「編集」「削除」、API キーの行、モデルの一覧と「モデルを追加」。区画の下に「接続先を追加」。
- **API キー**: 「設定済み（末尾 4 文字）」か「未設定」だけを出し、値は返さない（入力欄は `type=password`）。キーは行（`#local-key-row-<名前>`）だけを差し替えるので、他の入力欄は消えない。キーは `credential.Store`（ベンダー名 `endpoint:<名前>`）に保存し、`models.json` には入らない。
- **名前と種類は作成後に変えられない**: 編集フォームは名前と種類を文字で示し、入力欄にしない（モデルの名前も同じ）。手で作ったリクエストが別の名前・種類を送っても無視する。変えたいときは削除して作り直す。
- **モデルの追加**: ComfyUI は 2 段階。(1) テンプレート（`txt2img-sdxl` ほか）か、自分のワークフローの JSON（「Save (API Format)」。エディタ形式は 422 で、書き出し方を案内する）を選び、(2) フォームで名前・入力の割り当て・出力ノード・（動画は）fps・フレーム数の刻み・長さ・解像度・音声・見積コストを決める。割り当て表は、mam の値（Prompt・Negative Prompt・Seed・幅・高さ・フレーム数・fps・参照画像 1〜4・最初／最後のフレーム。画像は動画の値を持たない）ごとに、取り込んだグラフの「ほかのノードがつないでいない入力」から選ぶ（`<ノード ID>|<入力名>`）。参照画像は選んだ順に 0, 1, … と番号を振る。OpenAI 互換は 1 段階（サーバーに渡すモデル ID・参照画像を受け取れるか・見積コスト。画像のみ）。
- **固定されたファイル**（`--model-config`）: 起動オプションが理由の注意を出し、接続先・モデルの追加・編集・削除のボタンは無効（`disabled`、`title` に理由）。一覧は読める。API キーの保存・削除と接続テストは使える。変更のルートは 409。
- **反映**: 保存はすぐに効く（`Adapter.Reload`）。接続先・モデルを足す・消す応答は、「画像・動画モデル」のフォーム（`#model-assignment`）を `hx-swap-oob` で添え、追加したモデルが主・代替（画像の 4 用途、Quality Policy）の選択肢に並ぶ。
- **削除**: 画像の用途・Quality Policy に使われているモデル（とそれを持つ接続先）は 422 で断り、使われている場所（`images.new_reference.primary` など）を示す。一覧の「使用中: …」にも出る。

## 2. ルート

すべて CSRF 必須。`:name` は接続先、`:model` はモデルの名前。成功は 200 で区画（`#local-models-panel`、`hx-swap="outerHTML"`）を返し、`Notice` に結果を添える。失敗は区画に理由を添えて返し、開いていたフォームは入力値のまま残る（API キーは返さないので、入力し直す）。ステータスは、固定ファイル・名前の重複が 409、接続先・モデルが無いのが 404、入力の検査の失敗が 422、書き込みの失敗が 500。

| Method | Path | 内容 |
|--------|------|------|
| GET | `/settings/local-models` | 区画だけ（「閉じる」） |
| GET | `/settings/local-models/endpoints/new` | 接続先の追加フォーム（`#local-models-editor` に差し込む断片）。固定ファイルでは 409 で区画を返す（`HX-Retarget` で区画を差し替える） |
| POST | `/settings/local-models/endpoints` | 接続先の登録。フォーム `name`・`kind`（`comfyui` / `openai-compat`）・`base_url`・`max_concurrency`・`timeout_seconds`・`api_key`（任意）。リンクローカルの `base_url` は 422 |
| GET | `/settings/local-models/endpoints/:name/edit` | 編集フォーム（`base_url`・`max_concurrency`・`timeout_seconds` だけ） |
| PUT | `/settings/local-models/endpoints/:name` | 変更。名前・種類のフィールドは読まない |
| DELETE | `/settings/local-models/endpoints/:name` | 接続先とそのモデル・キーの削除（使用中のモデルがあれば 422） |
| POST | `/settings/local-models/endpoints/:name/test` | 保存済みの接続先の接続テスト。結果の行（`TestResult`）だけを返す。到達できないのは 200 の失敗の行（要求自体は成功） |
| POST | `/settings/local-models/endpoints/test` | 入力中の内容（`kind`・`base_url`・`api_key`、編集では `name`）の接続テスト。保存しない。リンクローカルは 422（理由を表示） |
| PUT | `/settings/local-models/endpoints/:name/key` | API キーの保存（フォーム `api_key`）。キーの行だけを返す。固定ファイルでも可。空・空白入り・長すぎるキーは 422 |
| DELETE | `/settings/local-models/endpoints/:name/key` | API キーの削除（行だけを返す。未設定でも 200） |
| GET | `/settings/local-models/endpoints/:name/models/new` | モデルの追加の最初のフォーム（ComfyUI: テンプレートか JSON の選択、互換: モデルのフォーム） |
| POST | `/settings/local-models/endpoints/:name/models/start` | ComfyUI の 2 段階目。`template` か（空で）`media`・`workflow_json`。モデルのフォーム（ノード一覧つき）を返す。読めないワークフローは 422 で最初のフォームを返す |
| POST | `/settings/local-models/endpoints/:name/models` | モデルの定義。`name`・`media`・割り当て表（`assign_<param>`、参照画像は `assign_reference_image_<n>`）・`output_node`・`graph`・動画の項目・コスト、互換は `model_id`・`accepts_image_inputs` |
| GET | `/settings/local-models/endpoints/:name/models/:model/edit` | モデルの編集フォーム（名前は文字） |
| PUT | `/settings/local-models/endpoints/:name/models/:model` | モデルの変更。名前のフィールドは読まない |
| DELETE | `/settings/local-models/endpoints/:name/models/:model` | モデルの削除（使用中は 422） |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成。設定画面「ローカル・代替モデル」の画面の仕様とルート | Issue #831。`endpoints.md` の行数上限（300 行）に収めるため、区画のルートを別ファイルにした |
