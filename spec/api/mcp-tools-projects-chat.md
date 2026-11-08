# MCP ツール: Project・Brief・チャット

[`mcp-tools.md`](mcp-tools.md) の規約に従う、Project の作成・Brief・確認レベル・バンドルの書き出し・チャット（発言・カード・承認）のツール。Project の一覧（`list_projects`）・アセットのダウンロード（`get_asset_download_url`）・バンドルの取り込み（用途 `import_bundle`）は基盤にある。実装は `backend/internal/mcp/tools/{projectcore,chat,projectbundle,stage}`。

## 概要

- 画面は発言と添付を 1 つの multipart リクエストで送る。MCP では**先にファイルを上げ、後でツールが `upload_id` で参照する**（`create_upload_ticket` → `curl -F file=@…` → 応答の `upload_id` を `attachment_upload_ids` に渡す）。上げたファイルは OS の一時ディレクトリに 15 分だけ置かれ、`post_chat_message` / `create_project` が使うとアセットになる（形式・サイズ・内容の検証は画面と同じ `service/upload.go`。ツールの呼び出しが失敗したときは `upload_id` を使い切らず、同じ値で再試行できる）。`upload_id` を別の Project のチャットや新規作成に使うと `validation`。
- チャットの WebSocket（`/ws/projects/:id`）は「新着がある」ことだけを知らせる通知なので、MCP には `list_chat_messages` の `after`（直前の `last_message_id`）によるポーリングで足りる。進捗カード・承認カードの更新は `get_chat_message`（`pending` が `true` の間は変わりうる）で取り直す。
- チャットのカードの動画・ポスターの URL（画面のブラウザ用のパス）は出力に含めない。Take は `take.take_id`、キーフレーム画像は `ref_id`（asset_id）、添付は `attachments[].asset_id` で、ファイルは `get_asset_download_url` か Shot / Take のツールの URL で取る。

## アップロードの用途（`purpose`）

| purpose | `params` | 上限 | 結果 |
|---------|---------|------|------|
| `chat_attachment` | `project_id`（必須） | 動画 300 MB（画像は 20 MB。`service/upload.go` が拡張子ごとに検証） | `{"upload_id", "name", "size_bytes", "expires_at"}`。`post_chat_message` の `attachment_upload_ids` に渡す |
| `new_project_attachment` | なし | 同上 | 同上。`create_project` の `attachment_upload_ids` に渡す |

## ダウンロードの種別

| kind | リソース ID | 内容 |
|------|-----------|------|
| `project_bundle` | `project_id` | プロジェクトバンドル（ZIP。`GET /projects/:id/bundle` と同じ。書き出し中に作る） |

## ツール

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_project` | Project 1 件の状態・モード・確認レベル・Production Template・Quality Policy | `project_id` | `project`: `id`, `name`, `state`, `mode`, `confirmation_level`, `custom_checkpoints[]`, `production_template`, `quality_policy`, `series_id`, `episode_number`, `created_at` | はい |
| `create_project` | 最初の発言から Project を作る。Brief を抽出し、AI の最初の質問をチャットに投稿し、AUTO パイプラインを始める | `content`（4000 文字まで。添付があれば省略可）, `brand_template_id`（任意）, `attachment_upload_ids[]`（8 件まで。画像は Brief の Assets、動画は References） | `project`（上と同じ） | いいえ |
| `list_new_project_templates` | 新規 Project に適用できる Brand Template の一覧（古い順） | なし | `templates[]`: `id`, `name`, `constraint_count`, `created_at` | はい |
| `get_brief` | Creative Brief と、まだ決まっていない項目・各項目の出どころ | `project_id` | `project_id`, `state`, `fields`（`content_type` / `objective` / `subject` / `target` / `duration` / `aspect_ratio` / `mood`。未決定は空文字）, `missing[]`, `delegated[]`, `sources`（値のある項目ごとの出どころ。`user` / `inferred_from_request`（発言から読み取った未確認の値）/ `recommended`（目的から決めた仮の値）。`additional_instruction` も含む。`spec/spec.md` §4.2）, `proposed_subject`（Subject が食い違って利用者の判断待ちのときだけ、もう一方の候補）, `additional_instruction`, `style`, `assets[]`・`reference_assets[]`（`asset_id`, `type`, `mime_type`）, `references[]`（URL と動画アセット ID） | はい |
| `update_brief` | Brief を更新する。渡した項目だけ変わって出どころが `user` になり、渡さなかった項目は値も出どころ（`recommended` など）も変わらない。空文字は「未決定」に戻す。`confirm=true` で、Brief にある推奨・読み取りの値をすべて `user` にする（チャットの「この内容でよいですか？」への承認と同じ。未設定の項目は残る）。保存するとチャットに AI の発言が付き、パイプラインが再開する。画面の編集フォームと同じ検証（各項目 200 文字、`aspect_ratio` は `9:16` / `16:9` / `1:1`、`references` は http(s) の URL、追加指示は 4000 文字）。冪等 | `project_id`, `content_type`, `objective`, `subject`, `target`, `duration`, `aspect_ratio`, `mood`, `additional_instruction`, `references[]`, `confirm`（いずれも任意。`references` は URL の全件で、動画アセットの参照は残る） | `get_brief` と同じ | いいえ |
| `set_confirmation_level` | 確認レベルを変える（`full_auto` / `key_check` / `custom_check`。AUTO の生成の開始は、どの確認レベルでも承認カードを出さずに自動で始まる（AUTO を選ぶことが同意。`spec/spec.md` §4.3）。確認レベルが決めるのは脚本・Rough Cut・Final の確認だけで、変更時のチャットの通知もそう言う。`PUT /projects/:id/confirmation-level` と同じ）。`custom_check` では `checkpoints`（`script` / `rough_cut` / `final`）を選べ、省略すると前回の選択（なければ `script` と `final`）。変更はチャットに記録される。冪等 | `project_id`, `level`, `checkpoints[]` | `project` | いいえ |
| `export_project_bundle` | Project のバンドル（ZIP）のダウンロード URL（15 分有効）。別の mam では `import_bundle` で取り込める | `project_id` | `url`, `expires_at`, `file_name` | はい |
| `list_chat_messages` | チャットのメッセージ・カードを時系列で返す。`after` を渡すとそれ以降だけ（進捗の確認はこのポーリング）。画面の Chat Workspace・`GET /projects/:id/messages` と同じ内容 | `project_id`, `after`（メッセージ ID。省略で全件） | `project`（`id`, `state`, `mode`, `confirmation_level`）, `messages[]`, `updated[]`（返したメッセージが答えた以前のカードの最新）, `last_message_id`（次の `after`。新着がなければ渡した `after`） | はい |
| `get_chat_message` | カード 1 件の今の状態 | `project_id`, `message_id` | `project`, `message` | はい |
| `post_chat_message` | チャットに発言する。Brief の質問・Continue・Escalation への回答、自由な指示、DIRECTOR MODE の指示になる。返信先は `reply_to_message_id`。AI の返答は同じ応答の `messages` に入り、生成を始める返答の後続のカードは `list_chat_messages` で読む | `project_id`, `content`（添付だけなら省略可）, `reply_to_message_id`, `attachment_asset_ids[]`（既存アセットの再利用）, `attachment_upload_ids[]`（`chat_attachment` のもの。既存と合わせて 8 件まで） | `list_chat_messages` と同じ（発言とその返答の `messages`、`updated[]`、`last_message_id`） | いいえ |
| `respond_approval` | 承認カードに答える。カードは 1 回だけ答えられ（2 回目は `conflict`）、最後の承認待ちなら次の工程へ進む。AUTO は生成の承認を求めないが、v0.1.34 の旧「生成の承認」カード（`ref_type=generation`）が残っていれば同じように答えられ（エラーにしない）、記録（`generation_approval`）の無い旧カード（取り込んだ Project のもの。`message.unrecorded`＝true）は、どの答えでも承認として扱わず、返答もそう書く（`spec/spec.md` §4.3）。昇格提案カード（Notes）は World State へ書き込む | `project_id`, `message_id`, `decision`（`approve` / `request_changes`）, `comment`（4000 文字まで） | `project`, `message`（答えた後のカード） | いいえ |

### `message`（`messages[]` の要素）

`id`, `role`（`user` / `assistant`）, `kind`（`text` / `question` / `approval` / `keyframe` / `take` / `progress` / `escalation`）, `text`（本文。question・escalation は文面、take はタイトル）, `options[]`（question・escalation のクイック回答）, `non_blocking`, `reply_to_message_id`, `ref_type`, `ref_id`, `approval_status`（`pending` / `approved` / `changes_requested`）, `unrecorded`（記録の無い旧「生成の承認」カードのとき true。承認としては扱わない）, `open`（回答待ち）, `pending`（まだ変わりうる）, `attachments[]`（`asset_id`, `type`, `mime_type`, `size_bytes`）, `take`（`take_id`, `shot_id`, `title`, `badge`, `score`, `summary`）, `job`（progress: `id`, `kind`, `status`, `error`, `next_steps`。`next_steps` は企画のジョブが LLM の API キー未設定で失敗したときだけ。[mcp-tools.md](mcp-tools.md) の `list_jobs` の補足）, `created_at`。

## ルートとの対応

| ルート | ツール |
|--------|--------|
| `GET /`・`GET /projects` | `list_projects`（基盤） |
| `POST /projects` | `create_project`（添付は `new_project_attachment`、Brand Template の選択肢は `list_new_project_templates`） |
| `GET /projects/:id` | `get_project`・`list_chat_messages` |
| `GET /projects/:id/messages` | `list_chat_messages` |
| `GET /projects/:id/messages/:messageId` | `get_chat_message` |
| `POST /projects/:id/messages` | `post_chat_message`（添付は `chat_attachment`） |
| `POST /projects/:id/approvals/:messageId` | `respond_approval` |
| `GET /projects/:id/brief` | `get_brief` |
| `PUT /projects/:id/brief` | `update_brief` |
| `PUT /projects/:id/confirmation-level` | `set_confirmation_level` |
| `GET /projects/:id/assets/:assetId` | `get_asset_download_url`（基盤） |
| `GET /projects/:id/bundle` | `export_project_bundle` |
| `POST /projects/import` | none: 用途 `import_bundle`（基盤。`create_upload_ticket`） |
| `GET /ws/projects/:id` | none: 通知だけの WebSocket。`list_chat_messages` の `after` によるポーリングで代える |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。Project・Brief・確認レベル・バンドルの書き出し・チャットのツール、アップロードの用途 `chat_attachment`・`new_project_attachment`、ダウンロードの種別 `project_bundle` | Issue #830。画面の全操作を MCP ツールにする。添付は発言と同じリクエストでは送れないため、先に上げて `upload_id` で参照する形にした |
| 1.1 | 2026-11-08 | `get_brief` の出力に項目ごとの出どころ（`sources`）と Subject の食い違い（`proposed_subject`）、`update_brief` の入力に `confirm` を足し、渡した項目だけが `user` になることを明記した | Issue #1114。推奨値（仮）と利用者が確定した値を MCP でも区別し、確認を MCP からもできるようにするため |
| 1.2 | 2026-11-08 | `respond_approval`・`list_chat_messages`・`get_chat_message` の `message` に `unrecorded`（記録の無い旧「生成の承認」カードのとき true）を足し、`respond_approval` の説明に、記録の無い旧カードはどの答えでも承認として扱わないことを足した | Issue #1116: 画面・チャット・MCP で、記録の無い旧カードの扱いを揃える |
| 1.3 | 2026-11-08 | `message` のフィールド一覧に `unrecorded` を足した。`list_chat_messages` の `after` の「それ以降」が、メッセージ ID の大小ではなく、この環境が行を作った順（`rowid`）であることを足した | Issue #1116: 1.2 で足した `unrecorded` が一覧に無かった。取り込んだ巨大な ID のメッセージが新着の判定を壊さないため |
