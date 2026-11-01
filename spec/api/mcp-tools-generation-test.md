# MCP ツール: 生成テスト

[`mcp-tools.md`](mcp-tools.md) の規約に従う、生成テスト画面（[endpoints-gentest.md](endpoints-gentest.md)）の全操作のツール（Issue #835）。意味は [../architecture/gentest/gentest.md](../architecture/gentest/gentest.md)（画面・ジョブ・失敗の扱い）と [../architecture/gentest/gentest-adapter.md](../architecture/gentest/gentest-adapter.md)（モデルの直接呼び出し・シード）。

実装は `backend/internal/mcp/tools/gentest`。画面と同じ `gentest.Service`（`service.Services.Gentest`）を呼ぶ。アプリが持たないとき（`Gentest` が nil）は全ツールが `unavailable`。

## 方針

- **見積りの確認を強制する**: `run_generation_test` は `estimate_generation_test` が返した `estimate_id` と `confirmed_total_usd`（= `total_usd`）を必須とする。サーバーは同じ入力で見積りをやり直し、`estimate_id` か合計が違う（単価・設定・入力が変わった）、または上限（`gentest.max_cost_usd`・`gentest.max_items`）を超えるときは `conflict` で拒否し、ジョブも結果の行も作らない。単価が分からないモデル（`unknown`）は合計に入らないので、エージェントは `warnings` と `unknown_models` を**ユーザーに見せて**から実行する。
- **英訳（`translate_to_english`。Issue #913）**: `estimate_generation_test`・`run_generation_test` の引数。**省略時は `true`（画面のスイッチの初期値と同じ）**。`true` のとき、プロンプトとネガティブプロンプトのうち日本語など**ラテン文字以外の文字を含むものだけ**を、LLM（設定の「LLM の割り当て」の Prompt Planner）で英語に訳してから送る。すでに英語の文は訳さず、訳す文が無ければ LLM を呼ばない。訳すのは `run_generation_test` のときに 1 回だけ（全モデル・全本数で同じ英語）。**訳せなければ何も生成せず `upstream`（ジョブ・バッチ・結果の行は作らない。日本語のまま送らない）**。見積りには、訳す文の数の 1 行（`breakdown` の `model` が `translate`、`basis` は `unknown`）が入る。LLM には単価が無いので `total_usd` には入らず、`unknown_models` に `translate` が載り、警告 `no_price`（`model` が `translate`）が付く（`unknown_items`・`total_items` は結果の数なので増えない）。`estimate_id` は `translate_to_english` を含むので、切り替えたら見積りをやり直す。結果（`results[]`）の `prompt`・`negative_prompt` は入力した文、`translated`・`translated_prompt`・`translated_negative_prompt` は訳した英語、`sent_prompt` は実際に送った文。`false` なら訳さず入力のまま送る。
- **モデルは指定どおりに呼ぶ**: プロンプトは補正せずそのまま送る（英訳が `true` のときは英訳だけを加える）。モデルが使えない（`list_generation_models` の `available` が false）・落ちたときも代替モデルへは切り替えない（その結果が `failed`）。ネガティブの欄が無いモデルへは「Avoid: …」としてプロンプトに書き足し、結果の `negative_mode`（none / field / folded）と `sent_prompt` に残す。
- **シード**: `supports_seed` のモデル（FLUX.2・Runway Gen-4.5・シードの割り当てがあるローカルの ComfyUI モデル）にはサーバーが結果ごとに別のシードを作って送り、送った値を `seed` に残す。ほかのモデルは同じ入力の n 回の呼び出しで、`seed` なし・`seed_sent` が false。
- **課金・台帳に入らない**: Take・コスト台帳・アセットは作らない。合計コストは `list_generation_results` の `spend` だけ（解析・CostSummary には出ない）。
- **長い操作は `job_id` を返す**: `run_generation_test`。`get_job` は種別 `gentest` の Probe で結果の件数から進捗を返す（下の「ジョブ」）。再起動後も読める。ジョブは再開しない（mam が落ちたとき、終わっていない結果は `failed`・理由「中断」。終わった結果は残る）。
- **ファイルは URL で受け渡す**: 入力画像はアップロードの用途 `gentest_reference`、結果のファイルは `get_generation_result` の `file`（ダウンロードの種類 `gentest_file`。トークン付き・15 分。base64 は返さない、ZIP は無い）。
- **拒否**: 入力の誤り（モデル・プロンプト・本数・縦横比・解像度・長さ・入力画像・使えないモデル・英訳に使う LLM が無い）は `validation`、英訳に LLM が失敗した（API キー未登録・サーキットブレーカー・タイムアウト・訳になっていない答え）ときは `upstream`、見積りの不一致・上限超え・終わっていないものの削除・終わったバッチの取り消しは `conflict`、無いものは `not_found`。取り込みのスロットが LOCK されていれば `locked`（画像は取り込み済みで、メッセージに `asset_id`・`reference_id` を含む）。

## ツール

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_generation_models` | 選べるモデル（組み込みのクラウドモデル・ローカル・外部 ComfyUI・保存したワークフロー・ChatGPT サブスク）。使えないものは理由つき | `media`（省略可） | `models[]`（`id`, `label`, `media`, `source`, `available`, `unavailable`, `unavailable_message`, `accepts_references`, `video`（`durations`, `resolutions`, `max_reference_images`, `first_frame`, `last_frame`, `native_audio`）, `supports_seed`, `negative_field`, `price`（`basis`: priced / free / unknown, `per_image_usd`, `per_second_usd`））, `limits`（`max_items`, `max_cost_usd`） | はい |
| `estimate_generation_test` | バッチ（モデル × プロンプト × 本数）の本数と見積り。実行しない | `media`, `models[]`, `prompts[]`（`prompt`, `negative_prompt`）, `count`, `aspect_ratio`, `resolution`, `duration_seconds`, `translate_to_english`（省略時 true）, `references[]`（`source`: upload / project_asset / reference_item / history, `id`, `project_id`, `role`: reference / first_frame / last_frame） | `estimate_id`, `total_items`, `total_usd`, `breakdown[]`（`model`, `items`, `basis`, `unit_usd`, `subtotal_usd`）, `unknown_models`（英訳の行は `translate`）, `unknown_items`, `warnings[]`（`code`, `model`, `message`）, `over_limit`, `max_items`, `max_cost_usd`, `request` | はい |
| `run_generation_test` | 確認した見積りどおりに実行する（ジョブ） | 上と同じ + `estimate_id`, `confirmed_total_usd` | `job_id`, `state`, `batch_id`, `total_items` | いいえ（外部へ接続・課金あり） |
| `get_generation_test` | バッチの進捗と全結果 | `batch_id` | `state`, `total`, `done`, `queued`, `running`, `succeeded`, `failed`, `cancelled`, `finished`, `estimate`, `results[]`（下の結果） | はい |
| `cancel_generation_test` | バッチを取り消す。始めていない結果は取り消し、呼び出し中のものは終わるのを待つ | `batch_id` | `cancelled`, `running` | いいえ |
| `list_generation_results` | 履歴（新しい順） | `model`, `media`, `favorite`, `limit`, `offset` | `results[]`, `spend`（`items`, `priced_usd`, `unknown_items`） | はい |
| `get_generation_result` | 結果 1 件（入力画像・取り込み先・ファイルの URL） | `result_id` | 結果 + `inputs[]`, `imports[]`, `file`（`url`, `expires_at`） | はい |
| `delete_generation_result` | 結果とファイルを削除（生成中は `conflict`。バッチの最後の 1 件ならバッチも消える） | `result_id` | `id`, `deleted` | いいえ（破壊的） |
| `set_generation_favorite` | お気に入りの付け外し（冪等） | `result_id`, `favorite` | `result_id`, `favorite` | いいえ |
| `list_generation_prompts` | プロンプト履歴（同じ（プロンプト, ネガティブ）は 1 件。使用回数・最後に使った日時） | なし | `prompts[]` | はい |
| `delete_generation_prompt` | プロンプト履歴を 1 件削除（結果の履歴には影響しない） | `prompt_id` | `id`, `deleted` | いいえ（破壊的） |
| `import_generation_result` | 成功した結果を Project に取り込む（お気に入りでなくてよい）。`as=reference` は Reference Library（画像のみ・`source=generated`・`kind`, `name`, `tags`）、`as=asset` はアセット。ファイルは Project の assets に複製する。`assign` で Character の face/body・Product・Location のスロットへ割り当てられる（種類が合わなければ `validation`） | `result_id`, `project_id`, `as`, `kind`, `name`, `tags`, `assign`（`target`, `id`, `slot`） | `import_id`, `asset_id`, `reference_id`, `assigned` | いいえ |

結果（`results[]`）: `result_id`, `batch_id`, `position`, `media`, `model`, `model_label`, `api_model`, `prompt`, `sent_prompt`, `translated`, `translated_prompt`, `translated_negative_prompt`, `negative_prompt`, `negative_mode`, `aspect_ratio`, `resolution`, `duration_seconds`, `seed`, `seed_sent`, `status`（queued / running / succeeded / failed / cancelled）, `error`, `has_file`, `mime_type`, `size_bytes`, `cost_basis`, `estimated_cost_usd`, `actual_cost_usd`（ベンダーが返したときだけ）, `favorite`, `used_inputs`, `created_at`, `finished_at`。

### アップロードの用途とダウンロードの種類

| 種類 | 名前 | 内容 |
|------|------|------|
| アップロードの用途 | `gentest_reference` | 入力画像（画像モデルの参照画像・動画モデルの最初／最後のフレーム・自由な参照）。`create_upload_ticket` で URL を取り、multipart で 1 回送る。上限 20 MiB、PNG・JPEG・WebP（中身を検査する）。結果 `{"reference_id","mime_type"}` の `reference_id` を `references[]` に `{source: "upload", id}` として渡す。使わないまま 24 時間たつと、mam の次の起動で消える |
| ダウンロードの種類 | `gentest_file` | 成功した結果のファイル（リソース ID は `result_id`）。`get_generation_result` が URL にする |

## ジョブ（`get_job`）

| 種別（`kind`） | 開始するツール | `target` | `done` / `total` | `result` |
|---------------|---------------|----------|------------------|----------|
| `gentest` | `run_generation_test` | なし | 終わった結果の数 / 全結果の数 | `batch_id`, `succeeded`, `failed`, `cancelled`, `next` |

`state`: 結果に動いているものがあれば `running`（まだ何も始まっていなければ `queued`）、終わっていて 1 件でも成功なら `succeeded`、全部失敗なら `failed`（`error` はジョブの理由）、取り消された結果があれば `cancelled`。バッチが削除されると `cancelled`。モデルマネージャーの画面の試し生成も同じ種別のジョブなので、`get_job` で読める。

## テスト

`mcptest` と `gentesttest`（組み込みの全モデルに偽のバックエンド・実際の DB・動くジョブキュー）で、全組み合わせの実行と `get_job`・`get_generation_test`、見積りの確認（`estimate_id` なし・別の値・条件の変更・上限超え・`max_items` 超え）、取り消し、履歴の絞り込み・お気に入り・削除・プロンプト履歴、入力画像のアップロード（中身の検査）と結果のトークン URL、取り込み（参照・アセット・スロット割り当て・LOCK）、Take・コスト台帳・アセットが作られないこと、`get_job` が画面で始めたバッチを読めること、`unavailable`。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-26 | 新規作成。12 ツール、ジョブの種別 `gentest` の Probe（#833 から移動）、アップロードの用途 `gentest_reference`、ダウンロードの種類 `gentest_file` | Issue #835（#830 の方針: 画面の全操作を MCP で操作できるようにする） |
| 1.1 | 2026-11-01 | `estimate_generation_test`・`run_generation_test` に `translate_to_english`（省略時 true）を足し、方針に英訳（単価なしの `translate` の行・訳せなければ `upstream`）、結果に `translated`・`translated_prompt`・`translated_negative_prompt`、拒否に `upstream` を足した | Issue #913 |
