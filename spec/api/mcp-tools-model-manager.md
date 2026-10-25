# MCP ツール: モデルマネージャー

[`mcp-tools.md`](mcp-tools.md) の規約に従う、モデルマネージャー画面（[endpoints-model-manager.md](endpoints-model-manager.md)）の操作のツール（Issue #833）。ComfyUI エンジン・ローカルの画像・動画モデルのカタログ・ライセンスの同意。試し生成は「生成テスト」の MCP ツール（[mcp-tools-generation-test.md](mcp-tools-generation-test.md)）に移した（1.1）。意味は [../architecture/model-manager/model-manager.md](../architecture/model-manager/model-manager.md)。

実装は `backend/internal/mcp/tools/modelmgr`（エンジン・カタログ・トークン・外部の ComfyUI）。画面と同じ `modelmanager.Service`（`service.Services.ModelManager`）を呼ぶ。アプリが持たないとき（`ModelManager` が nil）は全ツールが `unavailable`。

## 方針

- **同意はサービスが強制する**: 同意が要るモデル（`consent_required`）の `install_model` は、同意の記録（ライセンスの ID と版）が無ければ `conflict` で拒否し、ダウンロードもジョブも始めない。エージェントは `get_catalog_model` でライセンスの要約・URL・商用利用の可否・地域の制限を取って**ユーザーに見せ**、ユーザーが同意したら `accept_model_license`（または `install_model` の `license_id`・`license_version`）で記録する。ID か版がカタログと違えば `validation`。カタログの版が変わると改めて同意が要る。
- **年齢確認もサービスが強制する**: 成人向けのモデル（`content_rating=adult`）は、年齢確認と注意書きへの同意が `state.json` に記録されるまで、`list_model_catalog` に出ず、`get_catalog_model`・`accept_model_license`・`install_model` は `conflict`（ダウンロードもジョブも始めない）。確認を記録できるのは `set_adult_content` だけで、**ユーザーが `get_adult_content_status` の注意書きを見て自分で答えた後にだけ**呼ぶ。エージェントが年齢や同意を代わりに答えてはならず、確認を自動で済ませる仕組みは無い。モデルごとのライセンスの同意（上）は、年齢確認とは別に取る（成人向けのモデルは `tier=advanced` で、同意が必須）。他の機能（#842 の Hugging Face の取り込みなど）が「確認は済んだか」を知るには `get_adult_content_status` の `enabled`（サービスでは `AdultContentEnabled()`）を見る。
- **長い操作は `job_id` を返す**: `install_model_engine`・`install_model`。`get_job` が状態・進捗・結果・失敗の理由を返す（下の「ジョブ」）。同じ対象の導入がすでに動いていれば新しいジョブは作らず、それを返す（`existing: true`）。導入のジョブは再開できる（`job_id` はジョブキューの行の ID なので、再起動後も `get_job` で読める）。
- **拒否は画面と同じ文面**（`mmview.Refuse`）: 入力の誤り・GPU が無い（CPU は `cpu=true` の明示）は `validation`、同意が無い・エンジン未導入・導入中・設定ファイルの固定（`--model-config`）・Windows 以外・空き容量不足は `conflict`、無いものは `not_found`。空き容量・GPU・同意の検査は、ダウンロードを始める前に行う。
- **同梱エンジンは Windows 版だけ**: `get_model_engine_status` の `engine.supported` が `false` のときは、`install_model_engine`・`start_model_engine`・`install_model` は `conflict`（外部の ComfyUI の URL を [`create_local_endpoint`](mcp-tools-local-models.md) で登録して使う）。
- **Hugging Face のトークンは返さない**: 状態（設定済みか・末尾数文字）だけ。

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
| `list_model_catalog` | カタログのモデルと、それぞれの状態。**成人向け（`content_rating=adult`）のモデルは、年齢確認が済むまで出ない** | なし | `models[]`（`id`, `name`, `kind`, `tier`, `content_rating`（`general` / `adult`）, `trigger_words[]`（LoRA のトリガー語）, `total_size`, `need_bytes`, `vram`（`min_gb`, `source`, `estimate`）, `license`（`id`, `version`, `name`, `url`, `summary`, `commercial_use`, `commercial_condition`, `excluded_regions`, `notice`）, `consent_required`, `accepted`, `state`（`not_installed` / `installing` / `installed` / `incomplete`）, `install_job`, `model_ids[]`）, `blocked`（導入できない理由。空なら導入できる） | はい |
| `get_catalog_model` | 1 件（`files[]`・共有ファイル・ミラーの原本の場所 `source_note` を含む）。成人向けモデルは `adult_notice`（ライセンスの要約の後に見せる注意書き）も返し、年齢確認の前は `conflict` | `model_id` | `list_model_catalog` の要素 | はい |
| `accept_model_license` | ライセンスに同意したことを記録する（冪等）。ユーザーが確認した後にだけ呼ぶ。成人向けモデルは年齢確認の前は `conflict`。成人向けモデルの同意は、そのモデルだけに効く（年齢確認とは別） | `model_id`, `license_id`, `license_version` | `model_id`, `accepted` | いいえ |
| `install_model` | モデルを取得して導入する（ジョブ）。同意が要るモデルは記録が無ければ `conflict` | `model_id`, `license_id`・`license_version`（同意をこの呼び出しで渡すとき） | `job_id`, `state`, `existing`, `target`（`model:<id>`） | いいえ |
| `delete_model` | 導入したモデルを削除する（使用中の用途・Policy があれば `validation`。共有ファイルは残る） | `model_id` | `model_id`, `deleted` | いいえ（破壊的） |
| `get_huggingface_token_status` | トークンの有無と末尾の数文字（値は返さない） | なし | `configured`, `last_chars` | はい |
| `set_huggingface_token` | トークンを保存・置き換える（冪等。応答に含まれない） | `token` | 同上 | いいえ |
| `clear_huggingface_token` | トークンを削除する（冪等） | なし | 同上 | いいえ（破壊的） |
| `get_external_inventory` | 外部の ComfyUI（種類 ComfyUI の登録済み接続先）が持つモデルファイルと、カタログのモデルに足りないファイル。読むだけ | `endpoint` | `endpoint`, `version`, `folders`, `models[]`（`id`, `present`, `missing`, `complete`） | はい（外部へ接続する） |
| `get_adult_content_status` | 成人向けモデルの表示（年齢確認のゲート）の状態と、いまの注意書き | なし | `enabled`（ゲートが開いている）, `confirmed`（現行の注意書きの版の確認が記録済み）, `confirmed_at`, `notice_version`, `notice` | はい |
| `set_adult_content` | 成人向けモデルの表示を切り替える（冪等）。**オンにするのは、`notice` をユーザーに見せ、ユーザー自身が「18 歳以上（またはお住まいの地域で成年）」と注意書きへの同意を答えた後だけ**。エージェントが自分で確認したことにして呼ばない（`accept_model_license` と同じ扱い）。確認の日時と注意書きの版は `state.json` に記録される | `enabled`, `confirm_adult`（ユーザーが答えたとき true）, `notice_version`（`get_adult_content_status` の値）。一度確認すれば、オフ→オンでは `confirm_adult` を省ける | `get_adult_content_status` と同じ | いいえ |

### Hugging Face からの導入（Issue #842）

意味は [model-manager-hf.md](../architecture/model-manager/model-manager-hf.md)。**terms の確認・年齢ゲート・コミット固定・SHA-256 の検証は Service が強制する**（`install_hf_file` は `accept_terms` が無く記録も無ければ `conflict`。NFAA のリポジトリは年齢確認の前は `search_hf_models` に出ず、`get_hf_model_files`・`install_hf_file` は `conflict`）。`accept_terms` は、ユーザーが `get_hf_model_files` のライセンス・terms を見て自分で答えた後にだけ渡す（エージェントが代わりに true にしない）。GGUF は一覧に出るが `installable=false`（導入は `validation`）、pickle 形式は導入しない。

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `search_hf_models` | HF のモデルをキーワードで検索（ダウンロード数の多い順）。NFAA は年齢確認が済むまで出ない | `q`, `kind`（`text-to-image` / `text-to-video` / `image-to-video`。省略で全部）, `limit`（既定 20・最大 50） | `hits[]`（`repo`, `downloads`, `likes`, `gated`, `nfaa`, `pipeline_tag`） | はい（外部へ接続する） |
| `get_hf_model_files` | リポジトリ（`owner/repo` か `huggingface.co` の URL）をコミットに固定してファイルを一覧する | `repo` | `repo`（`id`, `commit`, `license`, `gated`, `nfaa`, `tags`）, `page`, `terms`, `terms_version`, `license_warning`, `consented`, `token_configured`, `selected`, `files[]`（`path`, `name`, `size`, `sha256`, `format`, `installable`, `reason`, `security`, `group`, `variant`, `folder`・`folder_source`・`folder_reason`・`family`, `fit`・`need_bytes`, `selected`, `installed`, `install_job`）, `truncated` | はい（外部へ接続する） |
| `install_hf_file` | `.safetensors` を 1 つ導入する（ジョブ）。SHA-256・サイズは Service が HF から読み直す | `repo`, `commit`, `path`（`get_hf_model_files` の値）, `folder`・`name`（省略でパス・ヘッダーから決める）, `accept_terms`・`terms_version` | `job_id`, `state`, `existing`, `target`（`hf:<repo>@<commit>/<path>`） | いいえ |
| `list_user_models` | 利用者が HF から足したファイル（NFAA は年齢確認が済むまで出ない） | なし | `models[]`（`path`, `name`, `folder`, `repo`, `commit`, `repo_path`, `sha256`, `size`, `license`, `gated`, `nfaa`, `family`, `workflows[]`, `model_ids[]`, `shared_with[]`, `present`, `installed_at`） | はい |
| `delete_user_model` | 足したファイルと、作ったモデルを削除する（使用中の用途・Policy があれば `validation`、導入中は `conflict`、記録に無ければ `conflict`）。カタログのモデルと共有しているファイルは、そのモデルが残る間は消えない | `path`（`list_user_models` の `path`） | `path`, `deleted` | いいえ（破壊的） |

## 試し生成

モデルマネージャーの「試し生成」は、生成テスト（[mcp-tools-generation-test.md](mcp-tools-generation-test.md)）の 1 件のバッチとして動く。専用のツールは無い。ローカルのモデルを試すには `list_generation_models`・`estimate_generation_test`・`run_generation_test`（参照画像は用途 `gentest_reference`、結果のファイルは `get_generation_result` の URL）を使う。**導入済みの成人向けモデルは、年齢確認のゲートが閉じている間は `list_generation_models` に出ず、名指しても不明なモデルとして断られる**（Service が強制する。[model-manager.md](../architecture/model-manager/model-manager.md) §6.2）。1.0 の `list_trial_models`・`start_model_trial`・`list_model_trials`・`get_model_trial`・`get_model_trial_file_url`・`delete_model_trial`、アップロードの用途 `model_trial_reference`、ダウンロードの種類 `model_trial_file` は削除した。

## ジョブ（`get_job`）

| 種別（`kind`） | 開始するツール | `target` | `done` / `total` | `result` |
|---------------|---------------|----------|------------------|----------|
| `model_install` | `install_model_engine`・`install_model`・`install_hf_file` | `engine` / `model:<id>` / `hf:<repo>@<commit>/<path>` | 進捗の割合 / 100 | `target`, `phase`（download / extract / register / done など）, `done_bytes`, `total_bytes` |

`state` は `queued` / `running` / `succeeded` / `failed`。失敗の `error` はジョブキューの行の理由（SHA-256 の不一致・空き容量・取得元が認証を求めた・**取得元が消えた（404・410。ミラーの削除など。ファイル名と、ミラーなら原本の場所を添える）**・中断・ComfyUI の失敗など）。導入は再開できる（再起動のたびに最大 3 回）。進捗の割合はメモリ上で、ジョブの終了直前や再起動の後は 0 に戻ることがある（`succeeded` では 100）。

## テスト

`mcptest` で、偽の Hugging Face・偽のエンジンのプロセス・偽の ComfyUI（`mmtest`・`comfyuitest`）を使う。エンジンの導入から `get_job`、起動・停止・削除、GPU が無いとき・Windows 以外・固定された設定ファイル・空き容量不足の拒否、同意の強制（記録が無ければダウンロードしない・別のライセンスや版は拒否・同意後に導入）、中断と再導入、共有ファイルを残す削除、トークンが結果に出ないこと、`get_job` が別の入口（画面）で始めたジョブを読めること。年齢確認（`adult_test.go`）: 確認が無い間は一覧に出ず・読めず・同意も導入もできない（ダウンロードしない）こと、`confirm_adult`・`notice_version` が無い・違う呼び出しは拒否されること、確認の後に一覧・導入ができライセンスの同意は別に要ること、オフで再び隠れ確認の記録は残ること。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成。エンジン 9 ツール・カタログ 9 ツール・試し生成 6 ツール、ジョブの種別 `model_install`・`gentest`、アップロードの用途 `model_trial_reference`、ダウンロードの種類 `model_trial_file` | Issue #833（#830 の方針: 画面の全操作を MCP で操作できるようにする） |
| 1.1 | 2026-10-26 | 試し生成の 6 ツール・アップロードの用途 `model_trial_reference`・ダウンロードの種類 `model_trial_file` を削除し、生成テストのツール（[mcp-tools-generation-test.md](mcp-tools-generation-test.md)）に統合した。ジョブの種別 `gentest` の Probe も生成テスト側に移った | Issue #835。生成テストの実装を 1 つにする（試し生成は 1 モデル 1 プロンプトのバッチ） |
| 1.2 | 2026-10-25 | 成人向けモデルの年齢確認のツール `get_adult_content_status`・`set_adult_content` を足した。`list_model_catalog`・`get_catalog_model` に `content_rating`・`trigger_words`（と `files[].source_note`・`adult_notice`）を足し、ゲートが閉じている間は成人向けモデルを一覧に出さず、`get_catalog_model`・`accept_model_license`・`install_model` は `conflict` にした。取得元が消えた導入の失敗の理由を足した | Issue #841。成人向け（非検閲）のモデル・LoRA と年齢確認 |
| 1.3 | 2026-10-26 | Hugging Face の任意の `.safetensors` の導入のツール `search_hf_models`・`get_hf_model_files`・`install_hf_file`・`list_user_models`・`delete_user_model` を足した。ジョブの `target` に `hf:<repo>@<commit>/<path>` を足した | Issue #842。意味は [../architecture/model-manager/model-manager-hf.md](../architecture/model-manager/model-manager-hf.md) |
