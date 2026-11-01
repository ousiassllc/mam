# MCP ツール: 編集（エディター）

編集画面（`GET /projects/:id/edit`）の操作と、レビュー（[`mcp-tools-editor-review-reviews.md`](mcp-tools-editor-review-reviews.md)）の MCP ツール。規約・エラーコード・ファイル URL の使い方は [`mcp-tools.md`](mcp-tools.md)、仕組みは [`../architecture/mcp.md`](../architecture/mcp.md)。ツールは画面と同じサービス（`service.EditorService`）を呼ぶので、検証・状態の遷移・エラーの文面は画面と同じで、MCP 専用の確認や上限は無い。実装は `backend/internal/mcp/tools/{editor,editorstage,review}`。

## 1. 同期のツールと job_id を返すツール

サービスの編集ステージ（Rough Cut・Editor Judge・音声・Final QC・書き出し）は、1 回の呼び出しの中で最後まで走る（画面は応答を待つ）。ジョブキューの行は無い。

| ステージ | ツール | 応え方 |
|---------|--------|--------|
| Rough Cut・Final QC・追加 Shot の要求・タイムライン保存 | `create_rough_cut`・`run_final_qc`・`request_additional_shot`・`save_edit_timeline` | 数秒で終わるのでその場で結果を返す（`get_editor_view` と同じ形） |
| Editor Judge・音声生成・書き出し | `judge_edit_timeline`・`generate_audio`・`export_video` | すぐ `job_id`（`state: queued`）を返し、`get_job` で状態・結果を取る（種別 `judge_edit_timeline`・`generate_audio`・`export_video`。`target` は版の ID か Project の ID） |

`job_id` のツールは、ステージをバックグラウンドで走らせる。受け付けの検査（状態が違う・別のステージが実行中・オプションが不正・機能が無効・版が見つからない）は画面と同じエラーとして**呼び出しの結果**で返し、`job_id` は作られない（開始後 2 秒以内に失敗したものが対象）。それより後の失敗は `get_job` の `state: failed` と `error`（`<code>: <message>`）に出る。`get_job` の `result` はステージごとの要約（下表）。`job_id` は `job_<UUIDv7>` で、ジョブの履歴（`mcp_job_history`。30 日・10,000 行。[mcp.md](../architecture/mcp.md) §6）に残るので、mam を再起動したあとも `get_job` で引ける（ステージの終わりは終わった時点で履歴に書く）。ステージの途中で mam を止めると、画面のリクエストが途中で切れるのと同じく、そのステージは完了しない。止まったときに履歴に終わりが無かったジョブは、再起動後の `get_job` で `failed`（「mam が再起動したため、この操作の結果を確かめられません」）になる。

同じ Project のステージは 1 つずつしか走らない: 実行中に別のステージを呼ぶと `conflict`（別の編集処理が実行中）。

## 2. ツール

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_editor_view` | 編集画面の状態: Project の状態、最新の Edit Timeline の概要と Editor Judge の結果、最新の Audio Timeline、複製した声、最新の Final QC、書き出し一覧、書き出しの既定値と選択肢。`GET /projects/:id/edit` の `EditorView` と同じ | `project_id`, `branch_id`（省略=本線） | `project_id`, `project_name`, `state`, `branch_id`, `pending_shots`, `can_render`（ffmpeg が無いと false）, `timeline`（`id`, `version_number`, `clip_count`, `duration_seconds`, `editable`, `judge`（`verdict`: `pass` / `re_edit` / `need_shot`, `findings[]`: `clip_index`, `issue`, `action`, `need_shot`））, `audio`, `trunk_audio`（Branch を指定したときだけ）, `voices[]`, `final_qc`（`verdict`, `findings[]`）, `exports[]`, `export_defaults`, `export_choices` | はい |
| `get_edit_timeline` | 最新の Edit Timeline の全クリップ（Shot の順序・Cut Point・Transition・字幕・各クリップの開始秒）。`GET /api/v1/projects/:id/edit-timeline` と同じ。Rough Cut 前は `not_found` | `project_id`, `branch_id` | `get_editor_view` の `timeline` の項目に加え `clips[]`（`index`（1 始まり）, `shot_id`, `take_id`, `label`, `objective`, `kind`, `caption`, `in_point`, `out_point`, `source_seconds`, `start_seconds`, `transition`（`type`: `cut` / `fade` / `fade_black` / `dissolve`, `seconds`））, `limits`（`min_clip_seconds`, `max_transition_seconds`, `max_caption_runes`, `max_clips`） | はい |
| `save_edit_timeline` | クリップ列を Edit Timeline の次の版として保存する（`PUT /edit-timelines/:id`）。全クリップを再生順に渡す。`timeline_id` が最新でなければ `conflict`、Shot が Project に無い・Take が Shot のものでない・Take のファイルが無い・区間が 0.5 秒未満・終了点が Take の長さ超過・字幕 60 文字超・Transition が不正は `validation`。保存すると Project は EDITING に戻る | `timeline_id`, `clips[]`（`shot_id`, `take_id`, `kind`, `caption`, `in_point`, `out_point`, `transition`（`type`, `seconds`）） | `get_edit_timeline` と同じ（保存した版） | いいえ（破壊的でない: 版が増える） |
| `create_rough_cut` | 全 Shot の Selected Take から Rough Cut を作り、Project を EDITING にする（`POST /projects/:id/edit/rough-cut`）。Editing が MANUAL / LOCK の Shot の手編集は残る。Selected Take（ファイルのあるもの）が無い Shot がある・Shot が無いと `validation`、EDITING へ進めない状態は `conflict` | `project_id`, `branch_id` | `get_editor_view` と同じ | いいえ（破壊的でない: 版が増える） |
| `judge_edit_timeline` | Edit Timeline の版を Editor Judge にかける（`POST /edit-timelines/:id/judge`）。最新版・EDITING / EDIT_JUDGING のみ。PASS は AUDIO へ（Rough Cut の承認カードを求める確認レベルなら承認待ち）、`re_edit` は EDITING のまま指摘がチャットに出る、`need_shot` は `request_additional_shot` で追加 Shot を頼める。モデルを呼ぶ | `timeline_id` | `job_id`, `state`。`get_job` の `result`: `project_id`, `project_state`, `verdict`, `findings`（件数）, `pending_shots` | いいえ（破壊的でない。open world） |
| `request_additional_shot` | Editor Judge が素材不足（`need_shot`）と判定した版について、Director に追加 Shot を要求して生成を始める（`POST /edit-timelines/:id/request-additional-shot`。Editor → Reshoot Loop）。Retry Policy の上限では Escalation カード（AWAITING_APPROVAL）になる。Judge 前・`need_shot` でない・EDIT_JUDGING でない・前回の追加がまだできていないのは `conflict`、生成が使えないのは `unavailable`。追加 Shot の生成はジョブキューで進み、できると編集に戻る（`get_editor_view` の `pending_shots` と `state` で見る） | `timeline_id` | `get_editor_view` と同じ | いいえ（破壊的でない。open world） |
| `generate_audio` | Audio Timeline を作る（`POST /projects/:id/audio/generate`）。Audio Director がセリフ・ナレーションを読み上げ、Quality Policy が許す範囲で BGM・環境音・SE・Foley・Lip Sync を作ってミックスし、ラウドネスを揃える。Project が AUDIO のときだけ（`conflict`）。音声のベンダーか ffmpeg が無いと `unavailable`。コストがかかる | `project_id`, `branch_id` | `job_id`, `state`。`get_job` の `result`: `project_id`, `project_state`, `audio_timeline_id`, `version_number`, `speech_lines`, `measured_lufs`, `skipped_steps`、ローカルからクラウドに切り替えた実行のときだけ `provider_notes`（切り替えの理由を 1 件ずつ書いた文字列の配列。チャットの要約と同じ文。[model-adapters-m3.md](../architecture/model-adapters-m3.md) §14.10） | いいえ（破壊的でない。open world） |
| `run_final_qc` | Final QC を実行する（`POST /projects/:id/final-qc`）。常に本線の最新の Edit / Audio Timeline と Brand 制約を検査する（`branch_id` は応答に載せる Branch だけを決める）。PASS は READY（Final checkpoint があれば承認待ち）、FAIL は EDITING か AUDIO に戻る。Project が AUDIO（または FINAL_QC）のときだけ（`conflict`） | `project_id`, `branch_id` | `get_editor_view` と同じ（`final_qc` が結果） | いいえ（破壊的でない） |
| `export_video` | 完成動画を書き出す（`POST /projects/:id/export`）。省略した項目は Brief の縦横比・`1080p`・`h264`。Project が READY か EXPORTED（再書き出し）のときだけ（`conflict`）。最新の編集に対する音声が無いと `conflict`（音声が一度も無いと画面と同じ `not_found`）。不正なオプションは `validation`。ffmpeg が無いと `unavailable` | `project_id`, `branch_id`, `aspect_ratio`（`16:9` / `9:16` / `1:1` / `4:5`）, `resolution`（`720p` / `1080p` / `1440p` / `2160p`）, `codec`（`h264` / `h265` / `vp9`） | `job_id`, `state`。`get_job` の `result`: `project_id`, `project_state`（`EXPORTED`）, `export_id`, `aspect_ratio`, `resolution`, `codec`。ファイルは `get_export_download_url` | いいえ（破壊的でない） |
| `list_voice_clones` | Project の話者ごとの複製した声（話者名・名前）。ベンダーの voice id は返さない | `project_id` | `voices[]`: `id`, `speaker`, `name`, `created_at` | はい |
| `get_edit_preview_url` | Edit Timeline の版のレンダリング済み Rough Cut（`video/mp4`）のダウンロード URL（15 分）。その版の初回はこの呼び出しの中でレンダリングする（ffmpeg が無いと `unavailable`） | `timeline_id` | `url`, `expires_at`, `file_name` | はい |
| `get_export_download_url` | 書き出した動画のダウンロード URL（15 分。Range 可）。別の Project の書き出し・ファイルが無いものは `not_found` | `project_id`, `export_id` | `url`, `expires_at`, `file_name` | はい |

## 3. アップロードの用途とダウンロードの種別

| 種類 | 名前 | 内容 |
|------|------|------|
| アップロード | `voice_sample` | `params`: `project_id`（必須）, `speaker`（必須）, `name`。話者の声のサンプル録音（音声ファイル、10 MB まで）から声を複製し（`POST /projects/:id/audio/voices` と同じ `CloneVoice`）、その話者のセリフの声にする。同じ話者の新しい複製は古いものを置き換える。結果: `{"voice_clone_id", "speaker", "name"}`。ElevenLabs が無いと 503（`unavailable`）、サンプルが空・大きすぎる・話者名が不正は 422 |
| ダウンロード | `edit_preview` | リソース ID は Edit Timeline の版の ID。`video/mp4` |
| ダウンロード | `project_export` | リソース ID は `<project_id>/<export_id>`。書き出したコーデックの形式（`video/mp4` / `video/webm`）。ファイル名は `mam-export-<日付>-<ID>.<拡張子>` |

## 4. ルートとツールの対応（編集）

`none:` はツールが無いルートで、理由を書く。`upload:` はアップロードの用途（`create_upload_ticket` で URL を取る）。

| ルート | ツール |
|--------|--------|
| `GET /projects/:id/edit` | `get_editor_view` |
| `GET /projects/:id/export` | `get_editor_view`（同じ画面の別名） |
| `GET /projects/:id/edit/actions` | `get_editor_view`（各ステージの領域だけを返す HTMX の断片。同じデータ） |
| `GET /api/v1/projects/:id/edit-timeline` | `get_edit_timeline` |
| `POST /projects/:id/edit/rough-cut` | `create_rough_cut` |
| `PUT /edit-timelines/:id` | `save_edit_timeline` |
| `POST /edit-timelines/:id/judge` | `judge_edit_timeline` |
| `POST /edit-timelines/:id/request-additional-shot` | `request_additional_shot` |
| `POST /projects/:id/audio/generate` | `generate_audio` |
| `POST /projects/:id/audio/voices` | `upload: voice_sample` |
| `POST /projects/:id/final-qc` | `run_final_qc` |
| `POST /projects/:id/export` | `export_video` |
| `GET /edit-timelines/:id/preview` | `get_edit_preview_url` |
| `GET /projects/:id/exports/:exportId/video` | `get_export_download_url` |

声の一覧（画面は編集画面の一部として出す）は `list_voice_clones` と `get_editor_view` の `voices`。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。編集のツール 12 個（`get_editor_view`・`get_edit_timeline`・`save_edit_timeline`・`create_rough_cut`・`judge_edit_timeline`・`request_additional_shot`・`generate_audio`・`run_final_qc`・`export_video`・`list_voice_clones`・`get_edit_preview_url`・`get_export_download_url`）、アップロードの用途 `voice_sample`、ダウンロードの種別 `edit_preview`・`project_export`、ルートとの対応 | Issue #830。編集画面の全操作を MCP ツールにするため |
