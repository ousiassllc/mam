# MCP ツール一覧

[`spec/architecture/mcp.md`](../architecture/mcp.md) の MCP サーバーが提供するツール。エンドポイントは `POST /mcp`（Streamable HTTP、`Authorization: Bearer <token>` 必須）。画面の操作ごとのツールは、ドメインごとのファイル `mcp-tools-<ドメイン>.md` に足し、下の「ドメイン別」から参照する（1 ファイルの行数上限を守るため、このファイルには基盤のツールだけを書く）。

## 規約

- 名前は `snake_case` の `動詞_名詞`（`list_projects`・`create_project`・`get_job`）。重複と規約違反は起動時に panic する（`mcpkit.Registrar`）。
- 入力・出力は構造体で、`json` / `jsonschema` タグが入力スキーマ・出力スキーマになる。引数はスキーマで検証されてからハンドラが呼ばれる。出力は常にオブジェクト（一覧は 1 つのフィールドに入れる）。
- 説明（description）は日本語を主に、英語でも意味が通る短い形。
- アノテーション: 読み取りだけのツールは `readOnlyHint`、変更するツールは `destructiveHint: false`、削除・上書きするツールは `destructiveHint: true`。どれも `openWorldHint: false`（MCP の既定は `true`）。
- 失敗は MCP の**ツールエラー**（`isError: true`。プロトコルエラーではない）で、テキストは `<code>: <message>`。`code` は `validation`（422 相当。メッセージは画面と同じ）・`not_found`・`conflict`・`locked`・`forbidden`（Brand / Safety の LOCK 違反）・`budget_exceeded`・`rate_limited`・`unavailable`・`upstream`・`internal`（原因はログだけで、メッセージには出さない）。
- 長い操作は `job_id` を返す（`job_id` と `state` を持つ）。進捗と結果は `get_job` で取る。ファイルは URL（`*_url`・`create_upload_ticket`）で受け渡す。

## 基盤のツール

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_server_info` | サーバーの名前・バージョン・待ち受けアドレス・登録ツール数。接続とトークンの確認用（サービスを使わない） | なし | `name`, `version`, `listen_addr`, `tool_count` | はい |
| `list_projects` | Project の一覧（新しい順）。ホーム画面と同じ | なし | `projects[]`: `id`, `name`, `state`, `mode`, `confirmation_level`, `series_id`, `episode_number`, `created_at` | はい |
| `get_job` | 長い操作の状態・進捗・結果・失敗理由 | `job_id` | `job_id`, `kind`, `target`, `state`（`queued` / `running` / `succeeded` / `failed` / `cancelled`）, `message`, `done`, `total`, `result`, `error`, `started_at`, `finished_at` | はい |
| `list_credentials` | 外部モデルの API キーの登録状況。キーの値は返さない | なし | `credentials[]`: `provider`, `label`, `configured`, `last_chars`（末尾 4 文字まで。短いキーは空） | はい |
| `set_credential` | API キーを登録・置き換える（OS のセキュアストレージ。再起動不要。冪等） | `provider`, `api_key` | `provider`, `label`, `configured`, `last_chars`（キーは返さない） | いいえ（破壊的でない） |
| `delete_credential` | API キーを削除する（未設定でもエラーにならない。冪等） | `provider` | 同上 | いいえ（破壊的） |
| `list_upload_purposes` | アップロードできる用途と、必須項目・上限サイズ | なし | `purposes[]`: `purpose`, `description`, `max_bytes`, `params[]`（`name`, `description`, `required`） | はい |
| `create_upload_ticket` | 用途に結び付いた、15 分・1 回限りのアップロード URL を発行する | `purpose`, `params` | `url`, `expires_at`, `purpose`, `method`（`POST`）, `content_type`（`multipart/form-data`）, `file_field`（`file`）, `headers`, `max_bytes`, `curl_example` | いいえ（破壊的でない） |
| `get_asset_download_url` | Project にアップロードされた画像・動画（アセット）のダウンロード URL（15 分） | `project_id`, `asset_id` | `url`, `expires_at`, `file_name` | はい |

### プロバイダー ID（`provider`）

`anthropic`・`openai`・`google`・`flux`・`kling`・`seedance`・`runway`・`elevenlabs`・`synclabs`・`typesafe`（画面の「API キー」と同じ。`list_credentials` が返す一覧が正）。未対応の値は `validation`。

### アップロードの用途（`purpose`）

| purpose | `params` | 上限 | 結果 |
|---------|---------|------|------|
| `import_bundle` | なし | 8 GiB（`service.MaxBundleBytes`） | `{"project_id"}`（バンドルから新しい Project を作る。同じ ID があれば `conflict`） |
| `reference_image` | `project_id`（必須）, `kind`（必須: character / product / location / style / lighting）, `name`（必須）, `tags`（カンマ区切り） | 画像 20 MB | `{"reference_id", "asset_id"}`（Reference Library に登録） |

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
| Project 設定・Brand・原価・制作指標・全体設定（LLM・モデル・更新） | [mcp-tools-settings-policy.md](mcp-tools-settings-policy.md) |
| 編集（Edit Timeline・音声・書き出し） | [mcp-tools-editor-review.md](mcp-tools-editor-review.md) |
| レビュー（依頼・受信箱・コメント・判定） | [mcp-tools-editor-review-reviews.md](mcp-tools-editor-review-reviews.md) |

MCP ツールの対象外: MCP サーバー自身の設定（待ち受け先の変更・トークンの発行・失効）は設定画面だけで行う（エージェントが自分のアクセス権を変えないため）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。規約と基盤のツール（`get_server_info`・`list_projects`・`get_job`・`list_credentials`・`set_credential`・`delete_credential`・`list_upload_purposes`・`create_upload_ticket`・`get_asset_download_url`）、アップロードの用途 `import_bundle`・`reference_image` | Issue #830。画面の全操作を MCP ツールにするための基盤 |
| 1.1 | 2026-10-24 | 「ドメイン別」に 6 ファイル（Project・チャット／Shot・生成／World・メモ／設定・Brand／編集／レビュー）を載せ、MCP サーバー自身の設定を MCP の対象外にすると書いた | Issue #830。画面の全操作の MCP ツール化 |
