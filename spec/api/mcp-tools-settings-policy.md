# MCP ツール: 設定・ポリシー・制作指標・ヘルス

[`mcp-tools.md`](mcp-tools.md) の規約に従う、設定系の画面の操作のツール（Issue #830）。対象は、Project の設定画面（制作テンプレート・Quality / Cost Policy・予算・Take のアーカイブ・Brand 制約とテンプレート）、全体設定画面（LLM の割り当て・画像／動画モデルの割り当て・音声・口パクのプロバイダー・アップデート）、制作指標、ヘルスチェック。API キーは基盤の `list_credentials` / `set_credential` / `delete_credential`（[`mcp-tools.md`](mcp-tools.md)）。

実装は `backend/internal/mcp/tools/{policy,brand,analytics,health,settings,videoprovider}`（`videoprovider` は `get_video_provider`・`set_video_provider`。`tools/settings` が行数の上限に達したため別のパッケージにした）。どれも画面と同じサービスを呼ぶ（`PolicyService`・`BrandService`・`AnalyticsService`・`HealthService`・`llmassign.Service`・`modelsettings.Service`・`appupdate.Service`）。MCP 専用の確認や上限は足さない。

## 方針

- **MCP トークンの管理は MCP からはできない**（設定画面だけ。`PUT /settings/mcp/bind`・`POST /settings/mcp/tokens`・`DELETE /settings/mcp/tokens/:id`）。トークンの発行・失効や待ち受け先の変更をエージェントに許すと、エージェントが自分の接続の権限を変えられてしまうため、ツールを作らない（`spec/architecture/mcp.md`）。`get_global_settings` にも MCP の区画は含めない。
- **外部ツールの実行ファイルのパスは MCP からは設定しない**（ffmpeg・FaceFusion の Python とフォルダ。設定画面だけ。`PUT /settings/ffmpeg`・`PUT /settings/facefusion`）。パスは後で起動される実行ファイルを決めるので、MCP のクライアント（プロンプトインジェクションされたエージェントを含む）に決めさせない。FaceFusion は状態の読み取り（`get_facefusion_status`）と顔の入れ替え（`swap_generation_face`。[mcp-tools-generation-test.md](mcp-tools-generation-test.md)）だけを MCP に出す。
- **LLM・モデルの割り当ては「保存した設定」を読み書きする**。LLM の割り当て・画像／動画モデルの割り当ては、どちらも保存するとすぐに反映される（LLM は次の呼び出しから。#1006。再起動が要る設定は [restart.md](../architecture/restart.md) §2。`get_global_settings` の `llm.restart_pending` と `get_health` の `restart_pending` が、保存したが再起動で反映される設定を言い、`restart_app` で反映する）。画像・動画モデルの割り当ては保存するとすぐに反映される（#831。`model` にはローカル・代替モデルの識別子も使える。[mcp-tools-local-models.md](mcp-tools-local-models.md)）。`--llm-config` / `--model-config` で固定されたファイルは読めるが変更できず、`conflict`（「起動オプションで固定されている…」）になる。
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
| `set_project_budget` | 予算上限（USD）を設定する。省略した項目は上限なし（画面の空欄と同じ）。超える生成は `budget_exceeded`。チャットで予算を伝えたとき（確認カードのあと）も同じ経路で設定する（#1139）。冪等 | `project_id`, `project_budget_usd?`, `shot_budget_usd?`（0 より大きく 1,000,000 以下） | 同上 | いいえ |
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
| `get_global_settings` | 全体設定画面の内容（API キーの区画・MCP の区画を除く）。保存済みの設定（LLM・モデルともすぐに反映済み。再起動で反映されるものは `llm.restart_pending`） | なし | `llm`（`available`, `pinned`, `problem`, `providers[]`, `max_model_length`, `applies_at_once`（保存した割り当てが次の呼び出しからすぐ効くか）, `restart_pending[]`（保存したが再起動で反映される設定。無ければ空）, `provider_states[]`: `provider`, `available`（API キーが登録済み、またはサブスクでログイン済みなら真）, `unavailable_message?`, `settings_route?`, `route_warnings[]`: 今の割り当て（主・代替）が使えないプロバイダーを指しているもの（`agent`, `role`, `provider`, `model`, `message`, `settings_route`。Issue #1013。保存は拒まない）, `agents[]`: `agent`, `label`, `unused`, `primary`, `fallback`, `default_primary`, `default_fallback`）, `models`（`available`, `pinned`, `customized`, `problem`, `images[]`, `policies[]`, `image_models[]`（各 `available`, `unavailable_message?`, `settings_route?`。`available` が false のモデルも割り当てはできる。その生成は、既定の割り当てなら条件に合う使えるモデルに替わり、利用者が選んだ割り当て・固定した割り当て・成人向けのゲートなら断られる。実際の扱いは `routes[]`）, `video_models[]`（同）, `unusable_routes[]`: 使えないモデルを指している割り当て（`media`, `key`, `role`, `model`, `message`, `settings_route`。保存はできる。生成の扱いは `available` と同じで、`routes[]` が言う）, `routes[]`: 保存した割り当てを今実行したらどうなるか（生成せず読み取りだけ。`media`, `key`, `explicit`（利用者のもの。使えなくても別のモデルへ替えない）, `status`=`assignment_usable`/`condition_match`/`blocked`, `assigned[]`（割り当てのモデル。`role`, `model`, `route_kind`=`api`/`chatgpt-subscription`/`local`, `available`, `unavailable_message?`, `settings_route?`）, `use?`（最初に使うモデルと経路・費用の基準）, `selection?`, `reason?`, `candidates[]`（条件に合って今使えるモデル。選ぶ規則の順）, `message?`（`blocked` の原因と直し方）。[model-adapters.md](../architecture/model-adapters.md) §3.1）, `gated_models[]`）, `update`（`get_update_status` と同じ） | はい |
| `list_llm_models` | 「LLM の割り当て」で選べるモデルの一覧（API の一覧、取れなければ mam の既定候補。10 分キャッシュ。トークン・キーは返さない。[mcp-tools-chatgpt.md](mcp-tools-chatgpt.md)） | `provider?`, `available_only?`（今使えないプロバイダーを除く） | `providers[]`: `provider`, `source`（`api` / `default`）, `reason?`, `available`（API キー登録済み・ログイン済みか）, `unavailable_message?`, `settings_route?`, `models[]`（`id`, `name?`） | はい（openWorld） |
| `set_llm_assignment` | 指定した Agent の LLM（主・代替）を保存する。冪等。モデル名がそのプロバイダーの一覧（`list_llm_models`）にないときは**拒否せず保存し**、`warnings[]` で知らせる | `routes[]`: `agent`, `primary`, `fallback`（`provider`, `model`） | `get_global_settings` の `llm` に `warnings[]`（`kind`（`unlisted_model`）, `agent`, `role`（`primary` / `fallback`）, `provider`, `model`, `message`。無ければ空配列） | いいえ |
| `reset_llm_assignment` | LLM の割り当てを既定に戻す（上書きをすべて消す。冪等） | なし | 同上 | いいえ（破壊的） |
| `set_local_llm_server` | （`use_managed_runtime`〔true で「この PC の llama.cpp を使う」。mam が導入・起動する llama.cpp にアドレスなしで向かう。`get_global_settings` の `llm.local_server.managed_runtime` に状態〕は [local-llm.md §10](../architecture/local-llm.md)）ローカルの OpenAI 互換 LLM サーバー（Ollama・LM Studio・llama.cpp server。プロバイダー `local`。[`local-llm.md`](../architecture/local-llm.md)）のアドレス・任意の API キー・画像／動画の生成前に Ollama のモデルを降ろすスイッチを保存する。指定した項目だけ変わる。次の LLM 呼び出しからすぐ効く。リンクローカル等のアドレス・スキームの違う URL は `validation`、設定ファイルが固定されているときは `conflict`。キーの値は返さない（OS のセキュアストレージの `local`）。冪等 | `base_url?`（末尾の `/v1` は外して保存）, `use_managed_runtime?`, `api_key?`, `clear_api_key?`, `unload_before_generation?`。1 つ以上 | `available`, `base_url`, `default_base_url`, `key_configured`, `unload_before_generation`, `pinned`, `problem?`（`get_global_settings` の `llm.local_server` と同じ） | いいえ |
| `set_local_llm_server`（#1104 の追記。状態: 実装済み） | 上の `set_local_llm_server` に、`use_managed_ollama?`（true で mam が導入・起動する Ollama に向かう。`use_managed_runtime` と同時に true にはできず、矛盾する組み合わせは `validation`）を足す。`get_global_settings` の `llm.local_server` に `use_managed_ollama` と、どれに向かうかを示す `use`（`llama-cpp`・`ollama`・`custom`）を足す。ほかは変えない（[local-llm.md §11](../architecture/local-llm.md)）。対応する画面は設定ではなくモデルマネージャーの `PUT /model-manager/local-llm/use`・`PUT /model-manager/local-llm/connection` | `use_managed_ollama?` | `use_managed_ollama`, `use` | いいえ（冪等） |
| `test_local_llm_server` | ローカル LLM サーバーへの接続を確かめる（`GET /v1/models`）。引数を省くと保存した設定で試す（`api_key` を省いて別の `base_url` を試すときは、保存済みのキーを送らない）。接続できないときはツールのエラーにせず `ok: false` と `failure`（共通エラー [`errors.md`](../architecture/errors.md) の `local_llm_unreachable`: 原因・直し方・設定画面への導線）を返す | `base_url?`, `api_key?`（保存はしない） | `ok`, `message`, `model_count`, `models[]`（先頭 20 件）, `failure?`（`apperr.View`） | はい（openWorld） |
| `set_model_assignment` | 指定した画像の用途・Quality Policy のモデルを保存する。冪等 | `images?[]`: `purpose`（`new_reference` / `edit_reference` / `preview` / `unified_openai`）, `primary`, `fallback`（`model`, `fast?`）。`policies?[]`: `quality_policy`, `primary`, `fallback`（`model`, `resolution`）, `takes`（1 以上）。どちらかは必須 | `get_global_settings` の `models` | いいえ |
| `reset_model_assignment` | 画像・動画モデルの割り当てを既定に戻す（冪等） | なし | 同上 | いいえ（破壊的） |
| `get_video_provider` | クラウド動画モデル（Kling）を呼ぶ API プロバイダーの設定（画面の「API キー」の先頭の選択。`models.json` の `video_provider`。[model-adapters.md §9.6](../architecture/model-adapters.md)）。選択肢ごとに必要な API キーの有無と、選んだプロバイダーが各モデルを提供するか・提供しない理由・単価を返す | なし | `pinned`, `provider`（`official` / `openrouter` / `fal` / `replicate`）, `providers[]`（`provider`, `label`, `selected`, `key_provider`〔`set_credential` に渡す名前。`official` は空〕, `key_set`）, `models[]`（`model`, `label`, `supported`, `why?`, `prices?[]`〔`resolution`, `usd_per_second`〕） | はい |
| `set_video_provider` | API プロバイダーを保存する。冪等。すぐに反映され、次の見積りと生成から選んだ先の単価・提供状況で動く。未知の値は `validation`、`--model-config` で固定されていると `conflict`。選んだ先のキーは `set_credential`（`key_provider` の名前）で入れる | `provider`（`official` / `openrouter` / `fal` / `replicate`） | `get_video_provider` と同じ | いいえ |
| `get_audio_providers` | 音声・口パクのプロバイダーの設定（`audio.json`）と、ローカルの状態。保存済みの設定で、次の音声作成から使われる | なし | `available`, `problem`（`audio.json` が読めない理由）, `voice_provider`（`elevenlabs` / `local`〔GPT-SoVITS〕/ `irodori`〔Irodori-TTS〕）, `lip_sync_provider`（`synclabs` / `local`）, `fallback_to_cloud`, `voice`（GPT-SoVITS）・`irodori`（Irodori-TTS。`model`〔使うモデル〕・`cautions`〔使うときの制限〕つき）・`lip_sync`（`available`, `name`, `runtime_id`, `state`（`unsupported` / `installing` / `not_installed` / `weights_missing` / `ready` / `starting` / `running` / `failed`）, `usable`, `reason`（使えないときの文。画面・実行の失敗と同じ）, `support_reason`（この PC で動かせない理由とクラウドの案内。macOS はここ）, `process_error`, `vram_min_gb`, `vram_estimate`, `source_url`, `license`, `license_url`, `commercial_condition`, `unclear_commercial_use[]`, `cloud_provider`（`elevenlabs` / `synclabs`）, `cloud_key_set`（API キーの有無。値は返さない）） | はい |
| `set_audio_providers` | 声・口パクのプロバイダーと `fallback_to_cloud` を保存する。指定した項目だけ変わる（`false` も値）。この PC で動かせないローカルを選ぶと `validation`（ランタイム管理の理由つき）、`audio.json` が読めないと `conflict`。`fallback_to_cloud` をオンにすると、ローカルが使えない実行がクラウドで作られ API 費用が出ることがある。冪等 | `voice_provider?`（`elevenlabs` / `local`）, `lip_sync_provider?`（`synclabs` / `local`）, `fallback_to_cloud?`。1 つ以上 | `get_audio_providers` と同じ | いいえ |
| `get_update_status` | アップデートの状態 | なし | `available`, `version`, `state`（`checking` / `error` / `ready` / `blocked` / `available` / `dev_build` / `no_release` / `up_to_date` / `unchecked`）, `checked_at`, `latest`, `blocked_by`, `error_kind`, `failure?`（`state: "error"` のとき。共通エラー [`errors.md`](../architecture/errors.md) の `update_*`: 原因・直し方・導線。元のエラーの文は返さない。`apperr.View`） | はい |
| `restart_app` | アプリを再起動する（[restart.md](../architecture/restart.md)）。**`confirm: true` を明示したときだけ**動く（必須の引数。`false` は `validation`、省略はスキーマの検証で拒否される。どれも何も起きない）。動いている作業（生成などのジョブ・編集・ffmpeg・ワークフロー実行・ダウンロード）があるとき `when_busy` で選ぶ: `refuse`（既定: 再起動せず `conflict` で何が動いているか返す）/ `wait`（終わるまで待ってから再起動。応答は `state: "waiting"`。上限 `wait_minutes`、既定 30・最大 1440。過ぎたら再起動せずにやめる）/ `stop`（中断して今すぐ再起動。生成ジョブは失敗、再開できるジョブは再起動後に続く。**モデル・エンジン・ffmpeg のダウンロードや導入が動いていれば `stop` でも `conflict`**）。この版が自分で再起動できないとき（ブラウザ版 `cmd/server`）は `unavailable`。応答は `state: "restarting"` を返したあとアプリが終わるので、届かないことがある。再起動できたかは `get_health` の `boot_id` が変わり `ok: true` になることで確かめる | `confirm`, `when_busy?`（`refuse` / `wait` / `stop`）, `wait_minutes?` | `state`（`restarting` / `waiting`）, `boot_id`（頼んだ時点の実行の ID）, `busy[]`（`kind`（`jobs` / `editing` / `media` / `workflow_run` / `download` / `check_failed`）, `message`）, `wait_until?`, `next` | いいえ（破壊的） |
| `check_for_update` | 今すぐアップデートを確認する。検証できた新しいリリースがあると、画面と同じくアプリが終了してインストーラーが走る（応答は届かないことがある）。確認の失敗は `state: "error"` で返り、ツールエラーにはならない | なし | `get_update_status` と同じ | いいえ（`openWorld`） |

### 制作指標・ヘルス（`tools/analytics`・`tools/health`）

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_production_metrics` | MVP Success Metrics（`spec/spec.md` §28）。`project_id` なしは全 Project の合計 | `project_id?` | `project_id`, `project_name`, `metrics`（`projects`, `started`, `completed`, `exported`, `shots`, `override_events`, 各指標は `{numerator, denominator, defined, value}`: `first_pass_shot_acceptance`, `final_shot_acceptance`, `character_continuity_fail`, `product_accuracy_fail`, `prompt_adherence`, `auto_completion`, `user_override`, `export_completion`, `north_star`, `average_retakes_per_shot`, `interventions_per_finished`, `cost_per_accepted_second`, `time_to_first_rough_cut`, `time_to_final_video`）。`defined` が false は測る対象がまだない（画面の「—」） | はい |
| `get_health` | 本体とデータベースが使えるか。使えなくてもエラーにせず `ok: false`。`boot_id` は実行（起動）ごとの ID で、`restart_app` のあとに変わる | なし | `ok`, `database`（`ok` / `unavailable`）, `boot_id`, `started_at`, `restart_pending[]`（保存したが再起動で反映される設定。無ければ空）, `restart_waiting`（`restart_app` が作業の終わりを待っている） | はい |

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
| `GET /settings/llm/agents/:agent/models/:role` | `list_llm_models`（画面のモデルのプルダウンの中身と同じ一覧。`provider` を指定する） |
| `DELETE /settings/llm` | `reset_llm_assignment` |
| `PUT /model-manager/local-llm/connection` | `set_local_llm_server`（画面の区画「別のサーバーにつなぐ」の保存。#1104。旧 `PUT /settings/llm/local`） |
| `PUT /model-manager/local-llm/use` | `set_local_llm_server`（use_managed_runtime・use_managed_ollama の保存。#1104 で新設） |
| `POST /model-manager/local-llm/connection/test` | `test_local_llm_server`（#1104。旧 `POST /settings/llm/local/test`） |
| `PUT /settings/models` | `set_model_assignment` |
| `DELETE /settings/models` | `reset_model_assignment` |
| `GET /settings/api-keys` | `get_video_provider`（区画の先頭の「動画モデルの API プロバイダー」の選択。API キーは `list_credentials`） |
| `PUT /settings/api-provider` | `set_video_provider` |
| `GET /settings/audio` | `get_audio_providers` |
| `PUT /settings/audio` | `set_audio_providers` |
| `POST /settings/update/check` | `check_for_update`（状態の読み取りは `get_update_status`） |
| `POST /settings/restart` | `restart_app`（画面は `refuse` と `wait` だけ。`stop` は MCP のみ） |
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
| 1.3 | 2026-11-03 | `list_llm_models` を足し、`set_llm_assignment` が一覧にないモデル名に `warnings[]` を返すようにした（保存は拒まない） | Issue #987・#988 |
| 1.4 | 2026-11-03 | `restart_app` を足し、`get_health` に `boot_id`・`started_at`・`restart_pending`・`restart_waiting`、`get_global_settings` の `llm` に `applies_at_once`・`restart_pending` を足した。LLM の割り当ては保存するとすぐ反映される（再起動不要）。ルートの対応に `POST /settings/restart` | Issue #1006・#1007 |
| 1.5 | 2026-11-03 | 方針に「外部ツールの実行ファイルのパスは MCP からは設定しない」を足した（FaceFusion の `set_facefusion_paths` は置かない） | 統合後レビューの指摘 |
| 1.6 | 2026-11-03 | `get_global_settings` に `llm.provider_states[]`・`llm.route_warnings[]`・`models.image_models[]`/`video_models[]` の `available` 等・`models.unusable_routes[]` を足し、`list_llm_models` の各プロバイダーに `available`・`unavailable_message`・`settings_route` と入力 `available_only` を足した。`set_llm_assignment` の出力にも `provider_states`・`route_warnings` が入る | Issue #1013。API キー未登録・未ログインのプロバイダーのモデルが選べるように見え、選ぶと失敗した |
| 1.7 | 2026-11-03 | `get_audio_providers`・`set_audio_providers` の `voice_provider` に `irodori`（Irodori-TTS）を足し、出力に `irodori`（ローカルの声の状態。`model`〔使うモデル〕・`cautions`〔使うときの制限〕つき）を足した | Issue #1015。意味は [../architecture/model-adapters-m3.md §14.11](../architecture/model-adapters-m3.md) |
| 1.8 | 2026-11-03 | `set_local_llm_server`・`test_local_llm_server` と、`get_global_settings` の `llm.local_server` を足した。`set_llm_assignment` / `list_llm_models` はプロバイダー `local` を受ける（モデルはサーバーが持つもの。接続できないと `available: false`・`unavailable_message: ローカルサーバーに接続できません`・`settings_route: /settings/llm`） | Issue #1008 |
| 1.9 | 2026-11-04 | `set_local_llm_server` に `use_managed_runtime`、`get_global_settings` の `llm.local_server` に `use_managed_runtime`・`managed_runtime`（`state`・`reason`・`model`）を足した。管理サーバーを選んでいる間、`list_llm_models` の `local` はそのモデル 1 つ | Issue #1008（後半） |
| 1.10 | 2026-11-04 | `get_update_status`・`check_for_update`・`get_global_settings`（`update`）に、確認の失敗の `failure`（共通エラーの `update_*`）を足した。設定の保存の失敗（ディスク・権限）は `disk_full`・`not_writable`・`save_failed`（共通エラー）で返る | Issue #1010 |
| 1.11 | 2026-11-06 | `set_local_llm_server` に `use_managed_ollama`、`get_global_settings` の `llm.local_server` に `use_managed_ollama`・`use` を足し、対応表の画面側のルートをモデルマネージャーの `local-llm` に直した（状態は仕様のみ） | Issue #1104 |
| 1.12 | 2026-11-07 | `set_local_llm_server` の `use_managed_ollama`、`get_global_settings` の `llm.local_server` の `use_managed_ollama`・`use` を実装した。状態を「実装済み」にした（一方を true にするともう一方は false になる。両方 true の指定は `validation`。管理する Ollama を持たない app で `use_managed_ollama: true` も `validation`） | Issue #1104 |
| 1.13 | 2026-11-08 | `get_global_settings` の `models` に `routes[]`（保存した割り当てを今実行したらどうなるか）を足した | Issue #1113（既定の割り当てが使えないとき、何で生成が動くか・止まるかを生成せずに知るため） |
| 1.14 | 2026-11-08 | `get_global_settings` の `models.image_models[].available` と `models.unusable_routes[]` の説明を、#1113 の扱い（既定の割り当ては条件に合う使えるモデルへ替わり、利用者が選んだ・固定した割り当てなら断られる。実際の扱いは `routes[]`）に合わせた。以前の「生成は断られる」の説明を直した | Issue #1113（レビュー指摘） |
| 1.15 | 2026-11-10 | 全体設定（`tools/settings`）に `get_video_provider`・`set_video_provider` と、ルートの対応（`PUT /settings/api-provider`）を足した。クラウド動画モデル（Kling）の API プロバイダー（公式 / OpenRouter / fal.ai / Replicate）の読み書き。画面と同じサービス（`modelsettings.Service`）を通る | Issue #1136 |
| 1.16 | 2026-11-10 | 冒頭の実装のパッケージの一覧に `tools/videoprovider`（`get_video_provider`・`set_video_provider` の実装）を足した | PR #1141 のレビュー指摘（一覧に無かった） |
| 1.17 | 2026-11-11 | `get_video_provider` の出力から `available` を外した（未提供のときは常にエラーで返り、`false` は現れなかった） | PR #1141 のレビュー指摘 |
