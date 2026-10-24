# MCP ツール: モデルマネージャー

[`mcp-tools.md`](mcp-tools.md) の規約に従う、モデルマネージャー画面（[endpoints-model-manager.md](endpoints-model-manager.md)）の操作のツール（Issue #833）。ComfyUI エンジン・ローカルの画像・動画モデルのカタログ・ライセンスの同意・試し生成。意味は [../architecture/model-manager/model-manager.md](../architecture/model-manager/model-manager.md)。

実装は `backend/internal/mcp/tools/modelmgr`（エンジン・カタログ・トークン・外部の ComfyUI）と `backend/internal/mcp/tools/modeltrial`（試し生成）。画面と同じ `modelmanager.Service`（`service.Services.ModelManager`）を呼ぶ。アプリが持たないとき（`ModelManager` が nil）は全ツールが `unavailable`。

## 方針

- **同意はサービスが強制する**: 同意が要るモデル（`consent_required`）の `install_model` は、同意の記録（ライセンスの ID と版）が無ければ `conflict` で拒否し、ダウンロードもジョブも始めない。エージェントは `get_catalog_model` でライセンスの要約・URL・商用利用の可否・地域の制限を取って**ユーザーに見せ**、ユーザーが同意したら `accept_model_license`（または `install_model` の `license_id`・`license_version`）で記録する。ID か版がカタログと違えば `validation`。カタログの版が変わると改めて同意が要る。
- **長い操作は `job_id` を返す**: `install_model_engine`・`install_model`・`start_model_trial`。`get_job` が状態・進捗・結果・失敗の理由を返す（下の「ジョブ」）。同じ対象の導入がすでに動いていれば新しいジョブは作らず、それを返す（`existing: true`）。導入のジョブは再開できる（`job_id` はジョブキューの行の ID なので、再起動後も `get_job` で読める）。
- **拒否は画面と同じ文面**（`mmview.Refuse`）: 入力の誤り・GPU が無い（CPU は `cpu=true` の明示）は `validation`、同意が無い・エンジン未導入・導入中・設定ファイルの固定（`--model-config`）・Windows 以外・空き容量不足は `conflict`、無いものは `not_found`。空き容量・GPU・同意の検査は、ダウンロードを始める前に行う。
- **同梱エンジンは Windows 版だけ**: `get_model_engine_status` の `engine.supported` が `false` のときは、`install_model_engine`・`start_model_engine`・`install_model` は `conflict`（外部の ComfyUI の URL を [`create_local_endpoint`](mcp-tools-local-models.md) で登録して使う）。
- **Hugging Face のトークンは返さない**: 状態（設定済みか・末尾数文字）だけ。
- **ファイルは URL で受け渡す**: 試し生成の結果は `get_model_trial_file_url`（ダウンロードの種類 `model_trial_file`。base64 は返さない）、参照画像はアップロードの用途 `model_trial_reference`。

## エンジン

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_model_engine_status` | 同梱の ComfyUI エンジンの状態と、ComfyUI の接続先の一覧 | なし | `engine`（`supported`, `installed`, `version`, `pinned_version`, `update_available`, `variant`, `cpu`, `broken`, `process`（`state`: stopped / starting / running / failed, `url`, `error`）, `auto_start`, `dir`, `writable`, `free_bytes`, `variants[]`, `install_job`, `license`）, `endpoints[]`, `pinned` | はい |
| `detect_gpu` | この PC の GPU と、推奨する ComfyUI のビルド | なし | `os`, `adapters[]`, `recommendation`（`variant`（空なら対応する GPU なし）, `reason`, `guidance`, `cpu_available`） | はい |
| `detect_comfyui` | この PC で動いている既存の ComfyUI（127.0.0.1 の 8188・8000） | なし | `found[]`（`url`, `version`, `desktop`, `devices`）, `recommended_url` | はい |
| `install_model_engine` | エンジンを取得して導入する（ジョブ） | `variant`（`nvidia` / `nvidia_cu126` / `amd` / `intel`。省略で GPU の推奨）, `cpu` | `job_id`, `state`, `existing`, `target`（`engine`） | いいえ |
| `start_model_engine` | 同梱の ComfyUI を起動し、応答するまで待つ（冪等） | なし | `state`, `port`, `url`, `pid`, `error` | いいえ |
| `stop_model_engine` | 停止する（冪等） | なし | 同上 | いいえ |
| `remove_model_engine` | エンジンを削除する（モデルのファイルは残る。導入中は `conflict`） | なし | `removed` | いいえ（破壊的） |
| `set_model_engine_autostart` | mam の起動時に起動するか（冪等） | `on` | `auto_start` | いいえ |
| `cancel_model_install` | 動いている導入を中断する。ジョブは `failed`（理由「導入を中断しました…」）で終わり、取得済みの分は残って再開できる | `job_id` | `job_id`, `cancelled` | いいえ |

## モデルのカタログ

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_model_catalog` | カタログのモデルと、それぞれの状態 | なし | `models[]`（`id`, `name`, `kind`, `tier`, `total_size`, `need_bytes`, `vram`（`min_gb`, `source`, `estimate`）, `license`（`id`, `version`, `name`, `url`, `summary`, `commercial_use`, `commercial_condition`, `excluded_regions`, `notice`）, `consent_required`, `accepted`, `state`（`not_installed` / `installing` / `installed` / `incomplete`）, `install_job`, `model_ids[]`）, `blocked`（導入できない理由。空なら導入できる） | はい |
| `get_catalog_model` | 1 件（`files[]`・共有ファイルを含む） | `model_id` | `list_model_catalog` の要素 | はい |
| `accept_model_license` | ライセンスに同意したことを記録する（冪等）。ユーザーが確認した後にだけ呼ぶ | `model_id`, `license_id`, `license_version` | `model_id`, `accepted` | いいえ |
| `install_model` | モデルを取得して導入する（ジョブ）。同意が要るモデルは記録が無ければ `conflict` | `model_id`, `license_id`・`license_version`（同意をこの呼び出しで渡すとき） | `job_id`, `state`, `existing`, `target`（`model:<id>`） | いいえ |
| `delete_model` | 導入したモデルを削除する（使用中の用途・Policy があれば `validation`。共有ファイルは残る） | `model_id` | `model_id`, `deleted` | いいえ（破壊的） |
| `get_huggingface_token_status` | トークンの有無と末尾の数文字（値は返さない） | なし | `configured`, `last_chars` | はい |
| `set_huggingface_token` | トークンを保存・置き換える（冪等。応答に含まれない） | `token` | 同上 | いいえ |
| `clear_huggingface_token` | トークンを削除する（冪等） | なし | 同上 | いいえ（破壊的） |
| `get_external_inventory` | 外部の ComfyUI（種類 ComfyUI の登録済み接続先）が持つモデルファイルと、カタログのモデルに足りないファイル。読むだけ | `endpoint` | `endpoint`, `version`, `folders`, `models[]`（`id`, `present`, `missing`, `complete`） | はい（外部へ接続する） |

## 試し生成

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_trial_models` | 試し生成に使えるモデル | なし | `models[]`（`id`, `endpoint`, `name`, `media`, `bundled`, `accepts_reference`, `resolutions`, `durations`） | はい |
| `start_model_trial` | プロジェクトの外で試し生成する（ジョブ。課金なし、Take・コスト台帳・アセットに入らない）。検査（プロンプト 1〜4000 文字・画像モデルに動画の項目を渡さない・動画の長さと解像度・参照画像の可否・縦横比）に通らなければ `validation` でジョブを作らない | `model_id`, `prompt`, `negative_prompt`, `aspect_ratio`, `resolution`, `duration_seconds`, `reference_id` | `job_id`, `state`, `trial_id`（= `job_id`） | いいえ |
| `list_model_trials` | 試し生成の一覧（新しい順） | なし | `trials[]`（`id`, `model_id`, `media`, `prompt`, `status`: running / succeeded / failed, `error`, `file`, `mime_type`, `size_bytes`, `created_at`, …） | はい |
| `get_model_trial` | 1 件 | `trial_id` | 同上の要素 | はい |
| `get_model_trial_file_url` | 完了した結果のファイルのダウンロード URL（トークン付き・15 分）。完了していないものは `not_found` | `trial_id` | `trial_id`, `url`, `expires_at`, `file_name` | はい |
| `delete_model_trial` | 試し生成を削除する（生成中は `conflict`） | `trial_id` | `trial_id`, `deleted` | いいえ（破壊的） |

### アップロードの用途とダウンロードの種類

| 種類 | 名前 | 内容 |
|------|------|------|
| アップロードの用途 | `model_trial_reference` | 試し生成の参照画像（画像モデルの参照画像・動画モデルの最初のフレーム）。`create_upload_ticket` で URL を取り、multipart で 1 回送る。上限 20 MiB、PNG・JPEG・WebP。結果 `{"reference_id"}` を `start_model_trial` に渡す（1 回の試し生成で使い切る。使わないまま 24 時間たつと、mam の次の起動で消える） |
| ダウンロードの種類 | `model_trial_file` | 試し生成の結果（リソース ID は試し生成の ID）。`get_model_trial_file_url` が URL にする |

## ジョブ（`get_job`）

| 種別（`kind`） | 開始するツール | `target` | `done` / `total` | `result` |
|---------------|---------------|----------|------------------|----------|
| `model_install` | `install_model_engine`・`install_model` | `engine` / `model:<id>` | 進捗の割合 / 100 | `target`, `phase`（download / extract / register / done など）, `done_bytes`, `total_bytes` |
| `gentest` | `start_model_trial` | モデルの識別子 | 進捗の割合 / 100 | 完了後: `trial_id`, `model_id`, `media`, `file_name`, `mime_type`, `size_bytes`, `next` |

`state` は `queued` / `running` / `succeeded` / `failed`。失敗の `error` はジョブキューの行の理由（SHA-256 の不一致・空き容量・取得元が認証を求めた・中断・ComfyUI の失敗など）。導入は再開できる（再起動のたびに最大 3 回）が、試し生成は再開しない（mam が落ちると失敗）。進捗の割合はメモリ上で、ジョブの終了直前や再起動の後は 0 に戻ることがある（`succeeded` では 100）。

## テスト

`mcptest` で、偽の Hugging Face・偽のエンジンのプロセス・偽の ComfyUI（`mmtest`・`comfyuitest`）を使う。エンジンの導入から `get_job`、起動・停止・削除、GPU が無いとき・Windows 以外・固定された設定ファイル・空き容量不足の拒否、同意の強制（記録が無ければダウンロードしない・別のライセンスや版は拒否・同意後に導入）、中断と再導入、共有ファイルを残す削除、トークンが結果に出ないこと、試し生成のジョブ・結果のトークン URL・参照画像のアップロード・拒否・失敗、`get_job` が別の入口（画面）で始めたジョブを読めること。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成。エンジン 9 ツール・カタログ 9 ツール・試し生成 6 ツール、ジョブの種別 `model_install`・`gentest`、アップロードの用途 `model_trial_reference`、ダウンロードの種類 `model_trial_file` | Issue #833（#830 の方針: 画面の全操作を MCP で操作できるようにする） |
