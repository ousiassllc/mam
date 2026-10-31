# MCP ツール: Scene / Shot / 生成 / Take

[`mcp-tools.md`](mcp-tools.md) の規約に従う、Scene・Shot・Director・生成・Continue・Judge・Repair・Take のツール（Issue #830）。ツールはサービス層を直接呼ぶ（画面と同じ検証・LOCK・Brand・予算・状態の検査を通る）。実装は `backend/internal/mcp/tools/{shotplan,director,shotgen,takes}`（共通の Shot カードは `shotview`）。

## 共通

- **Shot カード**（`shot` を返すツールの出力）: `id`・`scene_id`・`project_id`・`order_index`・`objective`・`duration_seconds`・`state`（PLANNING / GENERATING / JUDGING / REPAIRING / EDITING …）・`selected_take_id`・`storyboard`・`takes[]`（`id`・`status`・`model_name`・`adopted`・`has_video`・`created_at`）・`judgeable_take_ids`（今 `judge_take` できる）・`repairable_take_ids`（今 `repair_take` できる）・`uses_candidates`（画面の「生成」が Candidates 生成になるか）。
- **長い操作**は `job_id`（と `state`）を返す。進捗と結果は `get_job`。種別と結果は下の「ジョブ」。サービスがジョブキューの行を返すもの（生成・Candidates・Reference Pack・Judge・Repair）は、`job_id` がその行の ID になり、再起動後も `get_job` で読める。
- ファイルは URL で渡す（`get_take_video_url`・`get_take_thumbnail_url`）。アップロードの用途は足さない。
- `lock` の値は `auto` / `manual` / `lock`。LOCK 済みの値を置き換える変更は `locked`、LOCK のパラメータを変える指示・LOCK の Brand 違反（`forbidden`）は何も作らず拒否される。

## Scene / Shot

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_scene_hierarchy` | Scene / Shot / Take の階層。Scene ページと同じ | `project_id`, `scene_id`（省略で最初の Scene） | `project_id`, `scenes[]`（`id`, `order_index`, `title`, `location`, `time`, `objective`, `state`, `shot_count`）, `scene`（選択中）, `shots[]`（Shot カード） | はい |
| `create_scene` | Scene を末尾に追加（チャットに記録） | `project_id`, `title`（必須）, `location`, `time`, `objective` | `scene` | いいえ |
| `create_shot` | Shot を Scene の末尾に追加 | `scene_id`, `objective`（必須）, `duration_seconds`（0 超〜上限） | `shot`（Shot カード） | いいえ |
| `get_shot` | Shot の基本情報と `shot_spec`・採用 Take（`GET /api/v1/shots/:id`） | `shot_id` | `id`, `scene_id`, `project_id`, `order_index`, `objective`, `duration_seconds`, `shot_spec`（オブジェクト。無ければ `{}`）, `state`, `selected_take_id` | はい |
| `get_shot_inspector` | Shot Inspector の内容 | `shot_id` | `project_id`, `project_mode`, `scene_id`, `scene_title`, `shot`, `spec`（Canonical Shot Spec。無ければ省略）, `locks`（全パラメータ → `{mode, value}`）, `takes[]`（Judge の `judge` つき）, `reference_pack[]`, `library[]`, `versions[]` | はい |

## Director

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `set_project_mode` | AUTO / DIRECTOR の切り替え（冪等。チャットに記録） | `project_id`, `mode`（`auto` / `director`） | `project_id`, `mode`, `state` | いいえ |
| `get_project_locks` | Story / Music の設定 | `project_id` | `locks`（`story`, `music`） | はい |
| `set_project_lock` | Story / Music を AUTO / MANUAL / LOCK に | `project_id`, `parameter`（`story` / `music`）, `mode` | `locks` | いいえ |
| `set_shot_lock` | Shot のパラメータ 1 つを AUTO / MANUAL / LOCK に。`video_model` は `value`（モデル名。LOCK で省略は最新 Take のモデル） | `shot_id`, `parameter`（`dialogue` / `character_identity` / `acting` / `camera` / `lighting` / `product_geometry` / `prompt` / `video_model` / `take_selection` / `editing`）, `mode`, `value` | `locks`（全 10 パラメータ） | いいえ |
| `update_shot_spec` | 手で編集した Canonical Shot Spec を保存（英語。`duration` は Shot に合わせて上書き。LOCK 中の変更は `locked`、生成中は `conflict`、不正は `validation`） | `shot_id`, `shot_spec`（オブジェクト） | `shot`, `spec`, `versions[]` | いいえ |
| `send_director_command` | 自然言語の指示を Shot に実行（Command Interpreter。モデルを呼ぶ）。再生成が始まったら `job_id` | `shot_id`, `content` | `started`, `job_id`, `job_state`, `inspector`（Inspector と同じ） | いいえ |

## 生成・Continue・Judge・Repair

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `generate_shot` | 単発の生成を始める（予算超過 `budget_exceeded`・生成中 `conflict`・LOCK Prompt + 指示 `locked`・Brand 違反 `forbidden`・未構成 `unavailable`） | `shot_id`, `additional_instruction`（1000 文字以内） | `job_id`, `state`, `shot_id`, `shot_state` | いいえ |
| `generate_shot_candidates` | Prompt Candidate Generation（Judge が Best Take を選ぶ。Judge 未構成は `unavailable`） | 同上 | 同上 | いいえ |
| `build_reference_pack` | Reference Pack の（再）生成 | `shot_id` | 同上 | いいえ |
| `propose_continue` | 完了した Shot の続きの候補を、チャットの質問カードとして提案（同期。Take が無ければ `conflict`） | `shot_id` | `project_id`, `message_id`, `text`, `options[]` | いいえ |
| `confirm_continue` | 続きの Shot（新しい Scene もあり）を作って生成を始める | `shot_id`, `instruction` または `delegate=true` | `job_id`, `state`, `shot`（新しい Shot のカード） | いいえ |
| `judge_take` | AI Video Judge を実行（動画なし・Shot が生成中は `conflict`、Judge 中は `conflict`） | `take_id` | `job_id`, `state`, `take_id`, `shot_id`, `shot_state` | いいえ |
| `repair_take` | Judge 結果から Repair（未 Judge・採用基準を満たす・Shot が生成中は `conflict`） | `take_id` | 同上 | いいえ |
| `get_take_judge_result` | 最新の Judge 結果（`GET /api/v1/takes/:id/judge-result`。未 Judge は `not_found`） | `take_id` | `take_id`, `judge_result`（`result`, `overall_confidence`, `metrics`, `issues[]`, `preserve`, `repair_strategy`, `created_at`, `scores`） | はい |
| `get_jev_shadow_report` | Repair Agent の Jev シャドー実行のレポート（`GET /api/v1/repair/jev-shadow-report`） | なし | `report`（`runs`, `errors`, `agreed`, `agreement_rate`, `mean_confidence`, `mean_latency_ms`, `total_cost_usd`, …） | はい |

### ジョブ

| 種別（`get_job` の `kind`） | 始めるツール | `target` | 結果（`result`） |
|----------------------------|-------------|----------|-----------------|
| `generate_shot` | `generate_shot`, `generate_shot_candidates`, `send_director_command`（再生成）, `confirm_continue` | Shot ID（`confirm_continue` は新しい Shot） | `shot_id`, `shot_state`, `take_ids`（開始後に作られた Take）。Shot が止まって新しい Take が無ければ `failed` |
| `build_reference_pack` | `build_reference_pack` | Shot ID | `shot_id`, `reference_count` |
| `judge_take` | `judge_take` | Take ID | `take_id`, `verdict`, `overall_confidence`。結果が作られずに止まれば `failed` |
| `repair_take` | `repair_take` | Take ID | `take_id`, `action`, `new_take_id`。新しい Take を作らない Repair（Edit-Level Repair）は `new_take_id` が空で `succeeded`、作るはずが作れなければ `failed`。Retry Policy の上限で止まったときも `failed` |

`send_director_command` の再生成と `confirm_continue` は、サービスがジョブ行を返さないため `job_id` は `job_<UUID>`（ジョブの履歴に残るので再起動後も引ける）。他は `job` テーブルの行の ID。

## Take

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_shot_takes` | Take の比較（`GET /shots/:id/takes` と `GET /api/v1/shots/:id/takes`） | `shot_id` | `shot_id`, `duration_seconds`, `partial_use`, `limits`（`max_segments`, `min_segment_seconds`, `min_axis_score`）, `takes[]`（`id`, `label`, `status`, `status_label`, `adopted`, `model_name`, `resolution`, `fps`, `duration_seconds`, `verdict`, `scores`, `proxied`, `has_video`, `selectable`, `rejectable`, `partial_usable`, `created_at`。古い順） | はい |
| `select_take` | 採用 Take にする（除外済み・アーカイブ済み・選択済みは `validation`） | `take_id` | `shot`（Shot カード） | いいえ |
| `reject_take` | Take を除外（ファイルと履歴は残る。取り消しは無い。除外済み・アーカイブ済みは `validation`） | `take_id` | `shot` | いいえ（破壊的） |
| `use_partial_takes` | 複数の Take の区間をつないで新しい Take（Partial Take Use。2〜8 区間・各 0.3 秒以上。ffmpeg が無いと `unavailable`） | `shot_id`, `segments[]`（`take_id`, `start`, `end`）または `instruction`（"Take A の 0-2秒 + Take B の 2-4秒"） | `shot` | いいえ |
| `get_take_video_url` | プレビュー動画の URL（種別 `take_video`。4K は 1080p プロキシ。アーカイブ済みは `not_found`） | `take_id` | `take_id`, `url`, `expires_at`, `file_name` | はい |
| `get_take_thumbnail_url` | 先頭フレームの JPEG の URL（種別 `take_thumbnail`。ffmpeg が無いと `unavailable`） | `take_id` | 同上 | はい |
| `get_take_trace` | Generation Traceability（元の Shot Spec・Prompt・World State 版・モデルとパラメータ・Routing・Reference・出力・Judge・Repair） | `take_id` | `project_id`, `take`, `model_parameters`, `routing`, `prompt_version`, `world_state_version`, `references[]`, `output`, `judgements[]`, `repairs[]`, `produced_by` | はい |

`get_take_video_url` / `get_take_thumbnail_url` の URL は 15 分有効の GET（Range 可）で、動画は画面のプレイヤーと同じファイル（`TakeService.Video`）。

## ルートとツールの対応

担当ドメインのすべてのルート。`none` は理由つき。

| ルート | ツール |
|--------|--------|
| `GET /projects/:id/scenes` | `get_scene_hierarchy` |
| `GET /projects/:id/scenes/:sceneId` | `get_scene_hierarchy` |
| `POST /projects/:id/scenes` | `create_scene` |
| `POST /scenes/:id/shots` | `create_shot` |
| `GET /shots/:id` | `get_shot_inspector` |
| `GET /api/v1/shots/:id` | `get_shot` |
| `PUT /projects/:id/mode` | `set_project_mode` |
| `PUT /projects/:id/locks` | `set_project_lock`（読み取りは `get_project_locks`） |
| `PUT /shots/:id/locks` | `set_shot_lock` |
| `PUT /shots/:id/shot-spec` | `update_shot_spec` |
| `POST /shots/:id/director-command` | `send_director_command` |
| `POST /shots/:id/generate` | `generate_shot` |
| `POST /shots/:id/generate/candidates` | `generate_shot_candidates` |
| `POST /shots/:id/reference-pack` | `build_reference_pack` |
| `POST /shots/:id/continue` | `propose_continue` |
| `POST /shots/:id/continue/confirm` | `confirm_continue` |
| `POST /takes/:id/judge` | `judge_take` |
| `POST /takes/:id/repair` | `repair_take` |
| `GET /api/v1/takes/:id/judge-result` | `get_take_judge_result` |
| `GET /api/v1/repair/jev-shadow-report` | `get_jev_shadow_report` |
| `GET /shots/:id/takes` | `list_shot_takes` |
| `GET /api/v1/shots/:id/takes` | `list_shot_takes` |
| `POST /takes/:id/select` | `select_take` |
| `DELETE /takes/:id` | `reject_take` |
| `POST /shots/:id/takes/partial` | `use_partial_takes` |
| `GET /takes/:id/video` | `get_take_video_url` |
| `GET /takes/:id/thumbnail` | `get_take_thumbnail_url` |
| `GET /takes/:id/trace` | `get_take_trace` |
| `GET /ws/shots/:id` | none: WebSocket の進捗の push。`get_job` が同じ状態（キュー・生成中・Judge 中・Repair 中・完了・失敗）を返す |

他の担当: Shot へのピン留め（`POST /shots/:id/references`）・バージョンの復元（`POST /shots/:id/versions/:n/restore`）・Branch（`POST /shots/:id/branch`）は worldref、Take のアーカイブ（`POST /projects/:id/takes/:takeId/archive`）は policy、追加撮影（`POST /edit-timelines/:id/request-additional-shot`）は editorrev。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。Scene / Shot・Director・生成・Continue・Judge・Repair・Take のツール 27 個、ジョブ種別 `build_reference_pack`・`judge_take`・`repair_take`、ダウンロード種別 `take_video`・`take_thumbnail` | Issue #830。画面の全操作を MCP ツールにする（Scene / Shot / 生成 / Take の担当分） |
