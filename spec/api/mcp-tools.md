# MCP ツール一覧

[`spec/architecture/mcp.md`](../architecture/mcp.md) の MCP サーバーが提供するツール。エンドポイントは `POST /mcp`（Streamable HTTP、`Authorization: Bearer <token>` 必須）。画面の操作ごとのツールは、ドメインごとのファイル `mcp-tools-<ドメイン>.md` に足し、下の「ドメイン別」から参照する（1 ファイルの行数上限を守るため、このファイルには基盤のツールだけを書く）。

## 規約

- 名前は `snake_case` の `動詞_名詞`（`list_projects`・`create_project`・`get_job`）。重複と規約違反は起動時に panic する（`mcpkit.Registrar`）。
- 入力・出力は構造体で、`json` / `jsonschema` タグが入力スキーマ・出力スキーマになる。引数はスキーマで検証されてからハンドラが呼ばれる。出力は常にオブジェクト（一覧は 1 つのフィールドに入れる）。
- 説明（description）は日本語を主に、英語でも意味が通る短い形。
- アノテーション: 読み取りだけのツールは `readOnlyHint`、変更するツールは `destructiveHint: false`、削除・上書きするツールは `destructiveHint: true`。どれも `openWorldHint: false`（MCP の既定は `true`）。
- 失敗は MCP の**ツールエラー**（`isError: true`。プロトコルエラーではない）で、テキストは `<code>: <message>`。`code` は `validation`（422 相当。メッセージは画面と同じ）・`not_found`・`conflict`・`locked`・`forbidden`（Brand / Safety の LOCK 違反）・`budget_exceeded`・`rate_limited`・`unavailable`・`upstream`・`internal`（原因はログだけで、メッセージには出さない。代わりに**相関 ID** `error_id`（`err_<UUIDv7>`）を作り、原因のログ行（ERROR。属性 `error_id`）と、エラーの文面 `internal: 予期しないエラーです。mam のログを確認してください（error_id: err_…）` に同じ値を載せる。エージェントは `get_app_logs` の `attrs: {"error_id": "err_…"}` で原因の行を引く。ファイルの URL のエラー本文も `error.error_id` を持つ）。
- 長い操作は `job_id` を返す（`job_id` と `state` を持つ）。進捗と結果は `get_job` で取る。`job_id` は履歴（30 日・10,000 行）に残るので、mam を再起動したあとも引ける（[mcp.md](../architecture/mcp.md) §6）。`get_job` の `error_code` は `error` の `<code>: <message>` の `<code>`（`job` テーブルの理由のように `<code>` が無いときは省略）、`project_id` は対象が属する Project（なければ省略）、`ended_at` は終わった時刻（`job` テーブルの記録があればそれ、なければ mam が終了を最初に確かめた時刻）。ファイルは URL（`*_url`・`create_upload_ticket`）で受け渡す。

## 基盤のツール

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_server_info` | サーバーの名前・バージョン・待ち受けアドレス・登録ツール数。接続とトークンの確認用（サービスを使わない） | なし | `name`, `version`, `listen_addr`, `tool_count` | はい |
| `list_projects` | Project の一覧（新しい順）。ホーム画面と同じ | なし | `projects[]`: `id`, `name`, `state`, `mode`, `confirmation_level`, `series_id`, `episode_number`, `created_at` | はい |
| `get_job` | 長い操作の状態・進捗・結果・失敗理由 | `job_id` | `job_id`, `kind`, `target`, `state`（`queued` / `running` / `succeeded` / `failed` / `cancelled`）, `message`, `done`, `total`, `result`, `error`, `error_code`, `error_id`, `project_id`, `started_at`, `finished_at`, `ended_at` | はい |
| `get_app_logs` | mam のアプリログ（`mam.log` とローテーション済みの世代。圧縮されたものも）を新しい順に返す。`internal` エラーの原因はここだけにある。API キー・トークンなどの秘密は返さない（既知の形とキーストアの値を `[redacted]` にしてから絞り込む） | すべて省略可: `level`（`debug` / `info` / `warn` / `error`。省略は `info`）, `since`・`until`（RFC 3339。`until` は含まない）, `keyword`（メッセージ・属性名・属性値。大文字小文字を区別しない）, `attrs`（属性の絞り込み。キーと値の組がすべて一致した行。`project_id`・`shot_id`・`take_id`・`job_id`・`error_id` など。グループの属性は `a.b`。10 個まで）, `limit`（既定 100、上限 500） | `entries[]`: `time`, `level`, `msg`, `attrs`; `truncated`（件数かサイズ（約 200 KB）の上限で、まだ古い行がある）, `next_until`（`truncated` のとき、続きを読む `until`） | はい |
| `list_jobs` | 長い操作（ジョブ）の履歴を新しく始まった順に返す（再起動後も残る。30 日・10,000 件）。終わっていないものは一覧の前に状態を確かめ直す（終わりが見える）。`result` は含めない（`get_job` で取る） | すべて省略可: `kind`, `state`（`queued` / `running` / `succeeded` / `failed` / `cancelled`）, `error_code`（`validation` / `not_found` / `conflict` / `locked` / `forbidden` / `budget_exceeded` / `rate_limited` / `unavailable` / `upstream` / `internal`）, `project_id`, `since`・`until`（開始時刻。RFC 3339。`until` は含まない）, `limit`（既定 50、上限 200）, `cursor`（前のページの `next_cursor`） | `jobs[]`: `job_id`, `kind`, `target`, `project_id`, `state`, `message`, `done`, `total`, `error`, `error_code`, `error_id`, `started_at`, `finished_at`, `ended_at`; `next_cursor`（次のページがあるときだけ） | はい |
| `list_credentials` | 外部モデルの API キーの登録状況。キーの値は返さない | なし | `credentials[]`: `provider`, `label`, `configured`, `last_chars`（末尾 4 文字まで。短いキーは空） | はい |
| `set_credential` | API キーを登録・置き換える（OS のセキュアストレージ。再起動不要。冪等） | `provider`, `api_key` | `provider`, `label`, `configured`, `last_chars`（キーは返さない） | いいえ（破壊的でない） |
| `delete_credential` | API キーを削除する（未設定でもエラーにならない。冪等） | `provider` | 同上 | いいえ（破壊的） |
| `list_upload_purposes` | アップロードできる用途と、必須項目・上限サイズ | なし | `purposes[]`: `purpose`, `description`, `max_bytes`, `params[]`（`name`, `description`, `required`） | はい |
| `create_upload_ticket` | 用途に結び付いた、15 分・1 回限りのアップロード URL を発行する | `purpose`, `params` | `url`, `expires_at`, `purpose`, `method`（`POST`）, `content_type`（`multipart/form-data`）, `file_field`（`file`）, `headers`, `max_bytes`, `curl_example` | いいえ（破壊的でない） |
| `get_asset_download_url` | Project にアップロードされた画像・動画（アセット）のダウンロード URL（15 分） | `project_id`, `asset_id` | `url`, `expires_at`, `file_name` | はい |

### `list_jobs` の補足

- 順序は開始時刻（同時刻は `job_id`）の新しい順。`next_cursor` は「最後に返したジョブの位置」なので、ページの間にジョブが増えても減ってもページはずれない。不正な `cursor`・`state`・`error_code`・時刻・`limit` は `validation`。
- `error_code` は、失敗の理由が `<code>: <message>`（ツールのエラーと同じ形）のときの `<code>`。`job` テーブルの行が持つ理由（ジョブキューがワーカーの `error` をそのまま書いたもの。例: `interrupted by shutdown`）や Probe の理由には `<code>` が無く、`error_code` は空（`state: failed` では絞れる）。その失敗の原因は、ジョブキューが `job_id` と理由を付けて書く ERROR 行（`job failed`）を `get_app_logs` の `attrs: {"job_id": …}` で引く。
- `error_id` は、`internal` の失敗（編集ステージの Editor Judge・音声・書き出しが予期しないエラーで終わったときなど、mam がエラーを作った時点で原因をログに書いたもの）にだけ付く。`get_job` も同じ値を返す。
- `message`・`error` は、`get_job` と同じ文面から、既知の形の秘密を `[redacted]` にしたもの。

### `get_app_logs` の補足

- 不正な `level`・時刻・`limit`・`attrs` は `validation`。ログのディレクトリが無いビルドは `unavailable`。
- 1 行の属性は文字列が 2,000 文字で切られる。パースできない行は読み飛ばす。ログを書くときの属性名がそのままキーになる（`project_id`・`shot_id`・`job_id`・`tool`・`err` など）。
- 秘密の扱い: ログへは書かない（キーストア・トークンのサービス・アダプターは値をロガーに渡さない）。読み取りでは、既知の形と、キーストアにある API キー・Hugging Face トークンの値を伏せる。名前が `token`・`secret`・`password`・`api_key`・`authorization` などの文字列の属性は値を丸ごと `[redacted]` にする。伏せたあとの文字列で絞り込むので、`keyword` や `attrs` で秘密を探り当てることはできない。

### プロバイダー ID（`provider`）

`anthropic`・`openai`・`google`・`flux`・`kling`・`kling4`・`seedance`・`runway`・`minimax`・`alibaba`・`luma`・`elevenlabs`・`synclabs`・`typesafe`（画面の「API キー」と同じ。`list_credentials` が返す一覧が正）。未対応の値は `validation`。

### アップロードの用途（`purpose`）

| purpose | `params` | 上限 | 結果 |
|---------|---------|------|------|
| `import_bundle` | なし | 8 GiB（`service.MaxBundleBytes`） | `{"project_id"}`（バンドルから新しい Project を作る。同じ ID があれば `conflict`） |
| `reference_image` | `project_id`（必須）, `kind`（必須: character / product / location / style / lighting）, `name`（必須）, `tags`（カンマ区切り） | 画像 20 MB | `{"reference_id", "asset_id"}`（Reference Library に登録） |
| `character_voice_reference` | `series_id`（Series の正本の登場人物のとき）か `project_id`（Project の登場人物のとき）のどちらか一方, `character_id`（必須）, `text`（書き起こし、500 文字以内）, `language`（`ja` 既定 / `en` / `zh` / `ko` / `yue`） | 音声 10 MB（`.wav/.mp3/.flac/.ogg/.m4a`、3〜10 秒。長さは ffprobe があるときだけ検証） | `{"series_id"?, "project_id"?, "character_id", "voice_reference"}`（登場人物の参照音声を置き換える。[mcp-tools-world-notes.md](mcp-tools-world-notes.md)「参照音声」） |

用途は各ドメインのパッケージが `Files.RegisterUpload` で足す（チャットの添付・音声サンプルなど）。足したら、この表かドメイン別のファイルに書く。

### ファイル URL の使い方

```text
# アップロード: create_upload_ticket の url に multipart で 1 回だけ送る（Authorization は不要）
curl -X POST -F 'file=@/path/to/file' 'http://100.101.102.103:8788/mcp-files/<ticket>'
# → 201 {"purpose": "...", "result": {...}}    失敗: {"error": {"code": "...", "message": "..."}}

# ダウンロード: 返された url を GET する（Range 可。15 分以内なら繰り返せる）
curl -OJ 'http://100.101.102.103:8788/mcp-files/<ticket>'
```

ステータス: 無効・期限切れ・使用済み・方向違いは 404、サービスの検証の失敗は 422、上限超過は 413、発行したトークンが失効していれば 401。

## ドメイン別

ドメインごとのツールは、次のファイルに足す。

| ドメイン | ファイル |
|---------|---------|
| （基盤: このファイル） | `mcp-tools.md` |
| Project・Brief・チャット・バンドル | [mcp-tools-projects-chat.md](mcp-tools-projects-chat.md) |
| Scene・Shot・Director・生成・Judge・Repair・Take | [mcp-tools-shots-generation.md](mcp-tools-shots-generation.md) |
| World State・Reference Library・Branch・バージョン・Series・メモ | [mcp-tools-world-notes.md](mcp-tools-world-notes.md)（対応表: [mcp-tools-world-notes-coverage.md](mcp-tools-world-notes-coverage.md)） |
| Project 設定・Brand・原価・制作指標・全体設定（LLM・モデル・音声・口パクのプロバイダー・更新） | [mcp-tools-settings-policy.md](mcp-tools-settings-policy.md) |
| ローカル・代替モデル（ComfyUI・OpenAI 互換の接続先とモデル） | [mcp-tools-local-models.md](mcp-tools-local-models.md) |
| ComfyUI ワークフロー（保存・取り込み・実行・Shot への固定） | [mcp-tools-workflows.md](mcp-tools-workflows.md) |
| ChatGPT でログイン（ログイン・ログアウト・状態・画像生成の ON/OFF） | [mcp-tools-chatgpt.md](mcp-tools-chatgpt.md) |
| モデルマネージャー（ComfyUI エンジン・ローカルモデルのカタログと導入・ライセンスの同意） | [mcp-tools-model-manager.md](mcp-tools-model-manager.md) |
| 生成テスト（プロジェクトに属さない画像・動画の比較生成・履歴・プロンプト履歴・プロジェクトへの取り込み） | [mcp-tools-generation-test.md](mcp-tools-generation-test.md) |
| 編集（Edit Timeline・音声・書き出し） | [mcp-tools-editor-review.md](mcp-tools-editor-review.md) |
| レビュー（依頼・受信箱・コメント・判定） | [mcp-tools-editor-review-reviews.md](mcp-tools-editor-review-reviews.md) |

MCP ツールの対象外: MCP サーバー自身の設定（待ち受け先の変更・トークンの発行・失効）は設定画面だけで行う（エージェントが自分のアクセス権を変えないため）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。規約と基盤のツール（`get_server_info`・`list_projects`・`get_job`・`list_credentials`・`set_credential`・`delete_credential`・`list_upload_purposes`・`create_upload_ticket`・`get_asset_download_url`）、アップロードの用途 `import_bundle`・`reference_image` | Issue #830。画面の全操作を MCP ツールにするための基盤 |
| 1.1 | 2026-10-24 | 「ドメイン別」に 6 ファイル（Project・チャット／Shot・生成／World・メモ／設定・Brand／編集／レビュー）を載せ、MCP サーバー自身の設定を MCP の対象外にすると書いた | Issue #830。画面の全操作の MCP ツール化 |
| 1.2 | 2026-10-24 | 「ドメイン別」に `mcp-tools-chatgpt.md`（ChatGPT でログイン）を足した | Issue #832。ChatGPT のログイン・ログアウト・状態・画像生成の ON/OFF の MCP ツール |
| 1.3 | 2026-10-25 | 「ドメイン別」に [mcp-tools-local-models.md](mcp-tools-local-models.md)（ローカル・代替モデル）を足した | Issue #831。ComfyUI・OpenAI 互換の接続先とモデルの操作を MCP ツールにする（#830 の方針: 画面の全操作を MCP で操作できる） |
| 1.4 | 2026-10-25 | 「ドメイン別」に [mcp-tools-model-manager.md](mcp-tools-model-manager.md)（モデルマネージャー）を足した | Issue #833。ComfyUI エンジン・モデルのカタログ・導入・同意・試し生成を MCP ツールにする（#830 の方針） |
| 1.5 | 2026-10-25 | 「ドメイン別」に [mcp-tools-workflows.md](mcp-tools-workflows.md)（ComfyUI ワークフロー）を足した | Issue #834。保存したワークフローの一覧・取り込み・実行・Shot への固定を MCP ツールにする（ノード画面は対象外） |
| 1.6 | 2026-10-26 | 「ドメイン別」に [mcp-tools-generation-test.md](mcp-tools-generation-test.md)（生成テスト）を足し、モデルマネージャーの試し生成のツールをそこへ統合した | Issue #835。モデル × プロンプトの比較生成・履歴・取り込みを MCP ツールにする（#830 の方針） |
| 1.7 | 2026-10-31 | `get_job` の出力に `error_code`・`project_id`・`ended_at` を足し、`job_id` が履歴（30 日・10,000 行）に残って再起動後も引けることを書いた。状態の決め方は変えない | Issue #890（親 #889） |
| 1.8 | 2026-10-31 | 基盤のツールに `get_app_logs`（アプリログの読み取り。レベル・時刻・件数・キーワード・属性で絞り込み、秘密は返さない）を足した | Issue #891（親 #889） |
| 1.9 | 2026-10-31 | `list_jobs`（履歴の一覧。種別・状態・エラーコード・Project・期間で絞り込み、新しい順・カーソルのページング）を足し、`get_job` に `error_id` を足した。`internal` エラーに相関 ID `error_id` を持たせ、ログの ERROR 行・エラーの文面・ジョブの履歴・`get_job` / `list_jobs` に同じ値を載せ、`get_app_logs` の属性の絞り込みでログ行をたどれることを規約に書いた | Issue #892（親 #889） |
| 1.10 | 2026-11-01 | アップロードの用途の表に `character_voice_reference`（登場人物の参照音声。Series の正本か Project の登場人物）を足した | Issue #919（#912）。参照音声の MCP の入口 |
| 1.11 | 2026-11-01 | ツールの一覧の「全体設定」の行に、音声・口パクのプロバイダー（`get_audio_providers`・`set_audio_providers`。`mcp-tools-settings-policy.md`）を足した | Issue #924。ツールの詳細は [mcp-tools-settings-policy.md](mcp-tools-settings-policy.md) |
| 1.12 | 2026-11-01 | `generate_audio` のジョブの結果に、ローカルからクラウドに切り替えた実行のときだけ `provider_notes`（切り替えの理由の配列）が付くことを [mcp-tools-editor-review.md](mcp-tools-editor-review.md) に足した | Issue #912。`model-adapters-m3.md` §14.10 が「ジョブの結果に書く」と言っていたのに、実装がチャットの要約にしか書いていなかったため |
