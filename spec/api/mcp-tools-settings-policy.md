# MCP ツール: 設定・ポリシー・制作指標・ヘルス

[`mcp-tools.md`](mcp-tools.md) の規約に従う、設定系の画面の操作のツール（Issue #830）。対象は、Project の設定画面（制作テンプレート・Quality / Cost Policy・予算・Take のアーカイブ・Brand 制約とテンプレート）、全体設定画面（LLM の割り当て・画像／動画モデルの割り当て・音声・口パクのプロバイダー・アップデート）、制作指標、ヘルスチェック。API キーは基盤の `list_credentials` / `set_credential` / `delete_credential`（[`mcp-tools.md`](mcp-tools.md)）。

実装は `backend/internal/mcp/tools/{policy,brand,analytics,health,settings}`。どれも画面と同じサービスを呼ぶ（`PolicyService`・`BrandService`・`AnalyticsService`・`HealthService`・`llmassign.Service`・`modelsettings.Service`・`appupdate.Service`）。MCP 専用の確認や上限は足さない。

## 方針

- **MCP トークンの管理は MCP からはできない**（設定画面だけ。`PUT /settings/mcp/bind`・`POST /settings/mcp/tokens`・`DELETE /settings/mcp/tokens/:id`）。トークンの発行・失効や待ち受け先の変更をエージェントに許すと、エージェントが自分の接続の権限を変えられてしまうため、ツールを作らない（`spec/architecture/mcp.md`）。`get_global_settings` にも MCP の区画は含めない。
- **LLM・モデルの割り当ては「保存した設定」を読み書きする**。画面と同じく、LLM の割り当ての反映にはアプリの再起動が必要（実行中の割り当てではない）。画像・動画モデルの割り当ては保存するとすぐに反映される（#831。`model` にはローカル・代替モデルの識別子も使える。[mcp-tools-local-models.md](mcp-tools-local-models.md)）。`--llm-config` / `--model-config` で固定されたファイルは読めるが変更できず、`conflict`（「起動オプションで固定されている…」）になる。
- `set_llm_assignment` / `set_model_assignment` は、**指定した Agent・用途・Quality Policy だけ**を変える（画面はフォーム全体を送るが、ツールは差分だけ受け、保存済みの値に重ねてから同じ検証 `check` を通す）。指定しなかったものは保存済みのまま。年齢確認がオフの間は、導入済みの成人向けモデルは `image_models` / `video_models` に出ず、割り当てがまだ指していれば `gated_models` に挙がる（そのモデルでの生成は `conflict` で断られる）。主・代替のどちらかが成人向けのモデルを指す保存は、変えていなくても `validation` で断る（メッセージに年齢確認をオンにするか別のモデルを選ぶ旨。`reset_model_assignment` はできる。[../architecture/model-manager/model-manager.md](../architecture/model-manager/model-manager.md) §6.1）。検証の失敗は `validation`（メッセージは画面と同じ）で、ファイルには何も書かない。
- **アップデート**は、アップデーターを持つ版（Windows デスクトップ）でだけ使える。ヘッドレス版では `get_update_status` / `check_for_update` が `unavailable`、`get_global_settings` の `update.available` が `false`（バージョンだけ返す）。LLM・モデルの区画を持たないアプリ（テスト用ルーター）では同様に各ツールが `unavailable`、`get_global_settings` の `llm.available` / `models.available` が `false`。
- **音声・口パクのプロバイダーは「保存した設定」を読み書きする**（`audiosettings.Service`。画面と同じ。#924）。反映は**次の音声作成から**で、再起動は要らない。`generate_audio` に provider の引数は無く、選択はこの 2 つのツールで行う。切り替えた実行は、Project のチャット（`list_chat_messages`）に「ローカル…を使えなかったため、この実行はクラウド…で作りました。使えない理由: …」と残る。ローカルを選べない環境（macOS など）では、`get_audio_providers` の `state` が `unsupported` で `support_reason` に理由とクラウドの案内が出る。
- キーの値はどのツールにも出ない。`get_global_settings` は API キーの区画を含まない（`list_credentials` を使う）。

## ツール

### Project の設定（`tools/policy`）

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_project_settings` | 設定画面の内容 | `project_id` | `project_id`, `production_template`, `evaluation_axes[]`（優先度順）, `quality_policy`, `limits`（保存された上書き）, `effective`（既定を重ねた実効値: `max_retries`, `max_generation_seconds`, `min_acceptance_score`, `min_axis_score`）, `project_budget_usd`, `shot_budget_usd`（null は上限なし）, `cost`（Project の合計）, `storage` | はい |
| `set_production_template` | 制作テンプレートを切り替える（スレッドに記録。冪等） | `project_id`, `production_template`（`product_cm` / `drama` / `anime` / `sns_short`） | `get_project_settings` の方針の部分（`production_template` … `shot_budget_usd`） | いいえ |
| `set_quality_policy` | Quality Policy を変える。`limits` を付けると 4 項目すべてをその値にする（省略した項目は既定に戻る。画面のフォームと同じ）。付けなければ上限は変えない（コンポーザーの切り替えと同じ）。冪等 | `project_id`, `quality_policy`（`fast` / `balanced` / `max_quality`）, `limits?`（`max_retries` 0〜10, `max_generation_seconds` 10〜3600, `min_acceptance_score` 0〜100, `min_axis_score` 0〜100） | 同上 | いいえ |
| `set_project_budget` | 予算上限（USD）を設定する。省略した項目は上限なし（画面の空欄と同じ）。超える生成は `budget_exceeded`。冪等 | `project_id`, `project_budget_usd?`, `shot_budget_usd?`（0 より大きく 1,000,000 以下） | 同上 | いいえ |
| `get_cost_summary` | Project・Scene・Shot ごとの見積りと実績（USD） | `project_id` | `project`, `unassigned`（`estimated`, `actual`, `entries`, `actual_entries`）, `project_ceiling_usd`, `shot_ceiling_usd`, `scenes[]`（`scene_id`, `branch_id`, `branch_name`, `order_index`, `title`, `totals`, `shots[]`） | はい |
| `get_storage_usage` | ディスク使用量と保存されている Take | `project_id` | `takes[]`（`take_id`, `shot_id`, `scene_title`, `scene_index`, `shot_index`, `status`, `model_name`, `size_bytes`, `unneeded`, `archivable`。不要なものが先）, `asset_bytes`, `preview_bytes`, `free_bytes`, `free_known`, `unneeded_count`, `unneeded_bytes` | はい |
| `archive_take` | Take の動画ファイルを手放す（Take は履歴を残して Rejected）。選択中の Take と Timeline が使う Take は `conflict` | `project_id`, `take_id` | `get_storage_usage` と同じ | いいえ（破壊的） |
| `archive_unneeded_takes` | 不要な Take（Rejected、または同じ Shot で別の Take が選ばれたもの）の動画をまとめて手放す。冪等 | `project_id` | 同上 | いいえ（破壊的） |

### Brand 制約・テンプレート（`tools/brand`）

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_brand_constraints` | Project の Brand / Safety 制約 | `project_id` | `project_id`, `constraints[]`（`id`, `type`, `level`, `text`, `created_at`） | はい |
| `add_brand_constraint` | 制約を登録する。`level` 省略は `lock` | `project_id`, `type`（`brand_guideline` / `forbidden_expression` / `required_disclaimer` / `product_claim` / `logo_rule` / `character_restriction` / `asset_rights` / `platform_rule`）, `level?`（`lock` / `advisory`）, `text`（1000 文字まで） | `list_brand_constraints` と同じ | いいえ |
| `delete_brand_constraint` | 制約を削除する | `constraint_id` | 同上（制約を失った後の Project の制約） | いいえ（破壊的） |
| `list_brand_templates` | Brand Template の一覧（古い順。制約つき） | `project_id` | `project_id`, `templates[]`（`id`, `name`, `constraints[]`, `created_at`） | はい |
| `save_brand_template` | Project の今の制約を名前付きのスナップショットとして保存する。制約のない Project・同名・名前が 1〜100 文字でない場合は `validation` | `project_id`, `name` | `list_brand_templates` と同じ | いいえ |
| `apply_brand_template` | テンプレートの制約のうち Project が持たないものを Project にコピーする（コピーは Project のもの） | `project_id`, `template_id` | `list_brand_constraints` と同じ | いいえ |
| `delete_brand_template` | テンプレートを削除する（使った Project の制約は残る） | `project_id`, `template_id` | `list_brand_templates` と同じ | いいえ（破壊的） |

### 全体設定（`tools/settings`）

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_global_settings` | 全体設定画面の内容（API キーの区画・MCP の区画を除く）。保存済みの設定で、反映は次の起動から | なし | `llm`（`available`, `pinned`, `problem`, `providers[]`, `max_model_length`, `agents[]`: `agent`, `label`, `unused`, `primary`, `fallback`, `default_primary`, `default_fallback`）, `models`（`available`, `pinned`, `customized`, `problem`, `images[]`, `policies[]`, `image_models[]`, `video_models[]`, `gated_models[]`）, `update`（`get_update_status` と同じ） | はい |
| `set_llm_assignment` | 指定した Agent の LLM（主・代替）を保存する。冪等 | `routes[]`: `agent`, `primary`, `fallback`（`provider`, `model`） | `get_global_settings` の `llm` | いいえ |
| `reset_llm_assignment` | LLM の割り当てを既定に戻す（上書きをすべて消す。冪等） | なし | 同上 | いいえ（破壊的） |
| `set_model_assignment` | 指定した画像の用途・Quality Policy のモデルを保存する。冪等 | `images?[]`: `purpose`（`new_reference` / `edit_reference` / `preview` / `unified_openai`）, `primary`, `fallback`（`model`, `fast?`）。`policies?[]`: `quality_policy`, `primary`, `fallback`（`model`, `resolution`）, `takes`（1 以上）。どちらかは必須 | `get_global_settings` の `models` | いいえ |
| `reset_model_assignment` | 画像・動画モデルの割り当てを既定に戻す（冪等） | なし | 同上 | いいえ（破壊的） |
| `get_audio_providers` | 音声・口パクのプロバイダーの設定（`audio.json`）と、ローカルの状態。保存済みの設定で、次の音声作成から使われる | なし | `available`, `problem`（`audio.json` が読めない理由）, `voice_provider`（`elevenlabs` / `local`）, `lip_sync_provider`（`synclabs` / `local`）, `fallback_to_cloud`, `voice` と `lip_sync`（`available`, `name`, `runtime_id`, `state`（`unsupported` / `installing` / `not_installed` / `weights_missing` / `ready` / `starting` / `running` / `failed`）, `usable`, `reason`（使えないときの文。画面・実行の失敗と同じ）, `support_reason`（この PC で動かせない理由とクラウドの案内。macOS はここ）, `process_error`, `vram_min_gb`, `vram_estimate`, `source_url`, `license`, `license_url`, `commercial_condition`, `unclear_commercial_use[]`, `cloud_provider`（`elevenlabs` / `synclabs`）, `cloud_key_set`（API キーの有無。値は返さない）） | はい |
| `set_audio_providers` | 声・口パクのプロバイダーと `fallback_to_cloud` を保存する。指定した項目だけ変わる（`false` も値）。この PC で動かせないローカルを選ぶと `validation`（ランタイム管理の理由つき）、`audio.json` が読めないと `conflict`。`fallback_to_cloud` をオンにすると、ローカルが使えない実行がクラウドで作られ API 費用が出ることがある。冪等 | `voice_provider?`（`elevenlabs` / `local`）, `lip_sync_provider?`（`synclabs` / `local`）, `fallback_to_cloud?`。1 つ以上 | `get_audio_providers` と同じ | いいえ |
| `get_update_status` | アップデートの状態 | なし | `available`, `version`, `state`（`checking` / `error` / `ready` / `blocked` / `available` / `dev_build` / `no_release` / `up_to_date` / `unchecked`）, `checked_at`, `latest`, `blocked_by`, `error_kind` | はい |
| `check_for_update` | 今すぐアップデートを確認する。検証できた新しいリリースがあると、画面と同じくアプリが終了してインストーラーが走る（応答は届かないことがある）。確認の失敗は `state: "error"` で返り、ツールエラーにはならない | なし | `get_update_status` と同じ | いいえ（`openWorld`） |

### 制作指標・ヘルス（`tools/analytics`・`tools/health`）

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_production_metrics` | MVP Success Metrics（`spec/spec.md` §28）。`project_id` なしは全 Project の合計 | `project_id?` | `project_id`, `project_name`, `metrics`（`projects`, `started`, `completed`, `exported`, `shots`, `override_events`, 各指標は `{numerator, denominator, defined, value}`: `first_pass_shot_acceptance`, `final_shot_acceptance`, `character_continuity_fail`, `product_accuracy_fail`, `prompt_adherence`, `auto_completion`, `user_override`, `export_completion`, `north_star`, `average_retakes_per_shot`, `interventions_per_finished`, `cost_per_accepted_second`, `time_to_first_rough_cut`, `time_to_final_video`）。`defined` が false は測る対象がまだない（画面の「—」） | はい |
| `get_health` | 本体とデータベースが使えるか。使えなくてもエラーにせず `ok: false` | なし | `ok`, `database`（`ok` / `unavailable`） | はい |

## ルートとツールの対応

| ルート | ツール |
|--------|--------|
| `GET /projects/:id/settings` | `get_project_settings`（Brand は `list_brand_constraints`・`list_brand_templates`、Scene 別のコストは `get_cost_summary`、ディスクの区画だけは `get_storage_usage`） |
| `PUT /projects/:id/production-template` | `set_production_template` |
| `PUT /projects/:id/quality-policy` | `set_quality_policy` |
| `PUT /projects/:id/budget` | `set_project_budget` |
| `GET /projects/:id/cost-summary` | `get_cost_summary` |
| `POST /projects/:id/takes/:takeId/archive` | `archive_take` |
| `POST /projects/:id/storage/archive-unneeded` | `archive_unneeded_takes` |
| `POST /projects/:id/brand-constraints` | `add_brand_constraint` |
| `DELETE /brand-constraints/:id` | `delete_brand_constraint` |
| `POST /projects/:id/brand-templates` | `save_brand_template` |
| `POST /projects/:id/brand-templates/:templateId/apply` | `apply_brand_template` |
| `DELETE /projects/:id/brand-templates/:templateId` | `delete_brand_template` |
| `GET /projects/:id/analytics` | `get_production_metrics` |
| `GET /analytics` | `get_production_metrics` |
| `GET /healthz` | `get_health` |
| `GET /api/v1/health` | `get_health` |
| `GET /settings` | `get_global_settings`（API キーは `list_credentials`） |
| `PUT /settings/llm` | `set_llm_assignment` |
| `DELETE /settings/llm` | `reset_llm_assignment` |
| `PUT /settings/models` | `set_model_assignment` |
| `DELETE /settings/models` | `reset_model_assignment` |
| `GET /settings/audio` | `get_audio_providers` |
| `PUT /settings/audio` | `set_audio_providers` |
| `POST /settings/update/check` | `check_for_update`（状態の読み取りは `get_update_status`） |
| `PUT /credentials/:provider` | `set_credential`（基盤） |
| `DELETE /credentials/:provider` | `delete_credential`（基盤） |
| `PUT /settings/mcp/bind` | none: MCP の待ち受け先は設定画面だけで変える（エージェントが自分の接続を変えられないようにする） |
| `POST /settings/mcp/tokens` | none: トークンの発行は設定画面だけ（MCP から発行できると、トークンの失効管理が意味を失う） |
| `DELETE /settings/mcp/tokens/:id` | none: トークンの失効は設定画面だけ |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。Project の設定（制作テンプレート・Quality Policy・予算・コスト・Take のアーカイブ）、Brand 制約とテンプレート、全体設定（LLM・モデルの割り当て・アップデート）、制作指標、ヘルスのツールと、ルートの対応表。MCP トークンの管理はツールにしない方針 | Issue #830。画面の全操作を MCP ツールにする（設定・ポリシー・指標・ヘルスの担当分） |
| 1.1 | 2026-10-25 | `get_global_settings` の `models` に `gated_models[]` を足した。年齢確認がオフの間は成人向けモデルを選択肢から外し、`set_model_assignment` がそれを指す経路を断る | Issue #848 |
| 1.2 | 2026-11-01 | 全体設定（`tools/settings`）に `get_audio_providers`・`set_audio_providers` と、ルートの対応（`GET`・`PUT /settings/audio`）を足した。声・口パクのプロバイダーと「ローカルが使えないときはクラウドに切り替える」の保存。ローカルの状態・使えない理由・クラウドの API キーの有無（値は返さない）を返す | Issue #924（#912 声と口パクのローカル化） |
