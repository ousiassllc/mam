# MCP ツール: ローカル・代替モデル

[`mcp-tools.md`](mcp-tools.md) の規約に従う、設定画面「ローカル・代替モデル」の操作のツール（Issue #831）。接続先（ComfyUI・OpenAI 互換）とそのモデルを登録し、画像の用途・Quality Policy に割り当てる。意味・識別子・`models.json` のスキーマは [`spec/architecture/local-models.md`](../architecture/local-models.md)、画面とルートは [endpoints-local-models.md](endpoints-local-models.md)。

実装は `backend/internal/mcp/tools/localmodels`。画面と同じ `localmodels.Service`（`service.Services.LocalModels`）を呼ぶ。MCP 専用の確認や上限は足さない。

## 方針

- **保存するとすぐに効く**（再起動不要）。画面と同じで、保存の前に起動時と同じ検査を通し、通らなければ `validation`（メッセージは画面と同じ）でファイルに何も書かない。
- **名前と種類は作成後に変えられない**（Take とコスト台帳の識別子 `<kind>:<接続先>/<モデル>` が指すため）。`update_local_endpoint` / `update_local_model` の入力に名前の変更・種類の変更は無い（別の値を渡すと不明なフィールドとして `validation`）。変えたいときは削除して作り直す。
- **`--model-config` で固定されたファイルは読めるが変更できない**: 接続先・モデルの作成・変更・削除は `conflict`（「起動オプション（--model-config）で固定されている…」）。`list_*` の `pinned` が `true`。API キーの保存・削除と接続テストは固定でも使える。
- **API キーは返さない**: 接続先は `api_key.configured` と `last_chars`（末尾数文字。短いキーは空）だけを返す。キーは `credential.Store`（ベンダー名 `endpoint:<名前>`）に保存し、`models.json` に入らない。
- **リンクローカル**（`169.254.0.0/16`・`fe80::/10`）の `base_url` は常に `validation`。接続テストも同じメッセージを返す。
- **接続先の入力の検証の文言は日本語だけ**（画面と同じ。1 問題 1 行。値や英語の項目名、英語の補足は含まない）。MCP の呼び出し元もこの文言を受け取る（以前の英語の補足は無い）。
- 使用中のモデル（画像の用途・Quality Policy に割り当て済み）と、そのモデルを持つ接続先は削除できない（`validation`。メッセージに `images.new_reference.primary` のような使用箇所が入る）。
- **割り当ては既存の `set_model_assignment`**（[mcp-tools-settings-policy.md](mcp-tools-settings-policy.md)）で行う。`model` に識別子（`comfyui:gpu/wan`・`openai-compat:lm/flux`）を渡せ、`get_global_settings` の `models.image_models` / `video_models`（と動画の解像度）に登録済みのモデルが加わる。保存はすぐに反映される（モデルの割り当ては再起動を待たない。LLM の割り当ては従来どおり再起動後）。
- **生成は既存の生成ツール**（`generate_shot` ほか。`job_id` を返し、`get_job` で進捗・結果を取る）。ローカルモデルで作った Take の `model_name` と Cost Ledger の `provider` に識別子が残る（見積コストは既定 0 USD、0 でも行は書く）。専用の生成ツールは無い。
- ローカル・代替モデルの区画を持たないアプリ（テスト用ルーター）では各ツールが `unavailable`。

## ツール

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_local_endpoints` | 登録済みの接続先の一覧 | なし | `pinned`, `endpoints[]`（`name`, `kind`, `base_url`, `max_concurrency`, `timeout_seconds`（既定を反映した値）, `api_key`（`configured`, `last_chars`）, `models[]`（識別子）） | はい |
| `get_local_endpoint` | 接続先 1 件 | `name` | `list_local_endpoints` の 1 件 | はい |
| `create_local_endpoint` | 接続先を登録する（すぐに効く） | `name`（英小文字・数字・`.` `_` `-` の 1〜63 文字。`new` は設定画面の追加フォームの URL と同じなため 422）, `kind`（`comfyui` / `openai-compat`）, `base_url`, `max_concurrency?`（既定 1）, `timeout_seconds?`（ComfyUI 3600・互換 900）, `api_key?` | 接続先 | いいえ |
| `update_local_endpoint` | `base_url`・同時実行数・タイムアウトを変える（省略した数は既定に戻る）。冪等 | `name`, `base_url`, `max_concurrency?`, `timeout_seconds?` | 接続先 | いいえ |
| `delete_local_endpoint` | 接続先・そのモデル・API キーを削除する | `name` | `name` | いいえ（破壊的） |
| `test_local_endpoint` | 接続テスト。`name` だけなら保存済み、`kind`＋`base_url`（＋`api_key`、保存しない）なら未保存の内容を試す。`name` と一緒に渡すとその名前の保存済みキーを使う。ComfyUI は `/system_stats`（バージョン・GPU）、互換は `/v1/models`（件数。無ければ「到達できる」の注意付き成功）。到達できないのは `ok=false` の結果 | `name?` / `kind?`, `base_url?`, `api_key?` | `ok`, `message`, `note`, `version`, `models[]`（20 件まで） | はい（外部への接続） |
| `set_local_endpoint_key` | API キーを保存・置き換える（固定ファイルでも可。冪等） | `name`, `api_key` | `name`, `configured`, `last_chars`（キーは返さない） | いいえ |
| `clear_local_endpoint_key` | API キーを削除する（未設定でもエラーにならない。冪等） | `name` | 同上 | いいえ（破壊的） |
| `list_local_templates` | ComfyUI の組み込みワークフロー（`txt2img-sdxl`・`img2img-sdxl`・`txt2video-wan22-5b`・`img2video-wan22-5b`） | なし | `templates[]`（`id`, `label`, `media`, `description`, `assignments[]`, `output_node`, `video`） | はい |
| `import_local_workflow` | 「Save (API Format)」の JSON を読み、ノードとその入力の一覧を返す（保存しない）。エディタ形式は `validation`（書き出し方を案内） | `workflow_json` | `nodes[]`（`id`, `class_type`, `inputs[]`: `name`, `linked`, `value`）, `image_params[]`, `video_params[]` | はい |
| `list_local_models` | 登録済みのモデルの一覧 | なし | `pinned`, `models[]`（`endpoint`, `name`, `id`, `kind`, `media`, `model_id`, `accepts_references`, `assignments[]`, `output_node`, `fps`, `frames_step`, `size_multiple`, `video`（`durations`, `resolutions`, `native_audio`）, `max_reference_images`, `first_frame`, `last_frame`, `cost_per_image_usd`, `cost_per_second_usd`, `used_by[]`） | はい |
| `get_local_model` | モデル 1 件 | `endpoint`, `name` | `list_local_models` の 1 件 | はい |
| `create_local_model` | 接続先にモデルを定義する。ComfyUI は `template_id`（その能力・割り当てから始め、渡した項目で上書き）か、`workflow_json`＋`assignments`＋`output_node`。互換は `model_id`（`media` は `image`）。動画は `durations`・`resolutions` が必要 | `endpoint`, `name`, `template_id?`, `media?`, `workflow_json?`, `assignments?[]`（`param`, `node`, `input`, `index?`）, `output_node?`, `fps?`, `frames_step?`, `size_multiple?`, `model_id?`, `accepts_references?`, `durations?`, `resolutions?`, `native_audio?`, `cost_per_image_usd?`, `cost_per_second_usd?` | モデル | いいえ |
| `update_local_model` | モデルの定義を変える。渡した項目だけが変わり、`workflow_json` を替えるときは `assignments` も渡す（`template_id` は使えない）。冪等 | `endpoint`, `name`, 以降は `create_local_model` と同じ | モデル | いいえ |
| `delete_local_model` | モデルを削除する（使用中は `validation`） | `endpoint`, `name` | `name`（`<接続先>/<モデル>`） | いいえ（破壊的） |

### `param`（入力の割り当て）

`prompt`（必須）・`negative_prompt`・`seed`（実行ごとに乱数）・`width`・`height`・`reference_image`（`index` で何枚目か。0 から抜けなし）、動画は加えて `frames`・`fps`・`first_frame`・`last_frame`（`last_frame` は `first_frame` と一緒）。`node` / `input` は `import_local_workflow` が返すグラフの、ほかのノードがつないでいない入力。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成。接続先・API キー・接続テスト・テンプレート・ワークフローの取り込み・モデルの 15 ツールと方針 | Issue #831。ローカル・代替モデルの操作を MCP から行えるようにする（#830 の方針: 画面の全操作を MCP で操作できる。キーは設定済みと末尾だけ） |
| 1.1 | 2026-10-25 | `create_local_endpoint` の `name` に `new` を使えない（422）ことを書いた | Issue #847。設定画面の接続先のサブページ `/settings/local-models/endpoints/:name` が追加フォームの `…/endpoints/new` に隠れないようにするため |
| 1.2 | 2026-10-25 | 接続先の入力の検証の文言（`create_local_endpoint`・`update_local_endpoint`・`test_local_endpoint` の `validation`）は、画面と同じ**日本語だけ**の 1 問題 1 行になった（値や英語の項目名、リンクローカルの英語の補足は含まない）。MCP など画面でない呼び出し元も同じ文言を受け取る。検証の意味（名前・種類・`base_url`・同時実行数・タイムアウトの条件と順序）は変わらない | PR #849 第 2 周のレビューの指摘。設定画面の接続先のモーダルが、利用者向けの日本語の理由を出すようにしたため |
