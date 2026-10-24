# MCP ツール: レビュー（Team Collaboration）

レビュー画面（オーナー側 `GET /projects/:id/reviews`、レビュアー側 `GET /reviews`・`GET /reviews/:id`）の MCP ツール。編集のツールは [`mcp-tools-editor-review.md`](mcp-tools-editor-review.md)。規約・エラーコード・ファイル URL の使い方は [`mcp-tools.md`](mcp-tools.md)。ツールは `service.ReviewService` を直接呼ぶので、検証・エラーの文面は画面と同じ。実装は `backend/internal/mcp/tools/review`。ファイルが唯一の経路なので（`spec/spec.md` §27.1）、パッケージ・動画・応答はすべてファイル URL で受け渡す。

オーナーの操作は `project_id` を取り、レビュアーの操作は取り込んだパッケージの ID（**`inbox_id`**。`list_review_inbox` の `id`）を取る。`inbox_id` はオーナー側のレビュー依頼の ID（`review_id`）とは別で、レビュアーの instance にだけある。

## 1. ツール（オーナー）

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_project_reviews` | Project のレビュー依頼（状態・取り込んだ応答とコメント）と、依頼できる書き出し（新しい順）。`GET /projects/:id/reviews` と同じ | `project_id` | `project_id`, `reviews[]`（`id`, `export_id`, `export`, `note`, `status`: `pending` / `approved` / `changes_requested`, `open_comments`, `responses[]`（`id`, `reviewer_name`, `decision`, `comment`, `created_at`, `imported_at`, `comments[]`: `id`, `timecode_ms`, `timecode`, `body`, `resolved`）, `created_at`）, `exports[]`（`id`, `resolution`, `aspect_ratio`, `codec`, `exported_at`） | はい |
| `create_review_request` | 書き出しからレビュー依頼を作る（`POST /projects/:id/reviews`）。メモは 1000 文字まで（`validation`）。書き出しが別の Project のもの・無いものは `not_found`、書き出しのファイルが無いと `conflict` | `project_id`, `export_id`, `note` | `review_id`（作った依頼の ID）と `list_project_reviews` と同じ項目 | いいえ（破壊的でない） |
| `get_review_package_url` | レビュー依頼の Review Package（`manifest.json` と動画 1 本の ZIP）のダウンロード URL（15 分）。Project の ID・ファイルの場所・API キー・他の書き出しは入らない。別の Project の依頼は `not_found`、書き出しのファイルが無いと `conflict` | `project_id`, `review_id` | `url`, `expires_at`, `file_name` | はい |
| `set_review_comment_resolved` | 取り込んだレビューコメントを対応済み / 未対応にする（`PUT /projects/:id/review-comments/:commentId`。冪等） | `project_id`, `comment_id`, `resolved` | そのコメントを持つレビュー依頼（`list_project_reviews` の `reviews[]` の 1 件） | いいえ（破壊的でない） |

## 2. ツール（レビュアー）

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_review_inbox` | レビュアーとして取り込んだパッケージの一覧（新しい順）。`GET /reviews` と同じ | なし | `reviews[]`: `get_review` と同じ | はい |
| `get_review` | 取り込んだパッケージ（Project 名・オーナーのメモ・動画の情報・自分のコメントと判定）。`GET /reviews/:id` と同じ | `inbox_id` | `id`, `review_id`, `project_name`, `note`, `video`（`file_name`, `resolution`, `aspect_ratio`, `codec`）, `reviewer_name`, `decision`（未判定は空）, `comment`, `comments[]`（`id`, `timecode_ms`, `timecode`, `body`, `created_at`）, `can_respond`（判定があり応答を書き出せるか）, `created_at`, `updated_at` | はい |
| `add_review_comment` | レビュアーのコメントを足す（`POST /reviews/:id/comments`）。`position` は秒（`83.5`）か 分:秒（`1:23`）で、省略は動画全体。本文 1〜2000 文字・200 件まで（`validation`） | `inbox_id`, `position`, `body` | `get_review` と同じ | いいえ（破壊的でない） |
| `delete_review_comment` | レビュアーのコメントを削除する（`DELETE /reviews/:id/comments/:commentId`。冪等でなく、無い ID は `not_found`） | `inbox_id`, `comment_id` | `get_review` と同じ | いいえ（破壊的） |
| `decide_review` | レビュアーの名前（1〜50 文字）・判定（`approved` / `changes_requested`）・全体コメント（2000 文字まで）を書く（`PUT /reviews/:id/decision`）。書き直すと応答の ID が変わる | `inbox_id`, `reviewer_name`, `decision`, `comment` | `get_review` と同じ | いいえ（破壊的でない） |
| `get_review_video_url` | 取り込んだ動画のダウンロード URL（15 分。Range 可） | `inbox_id` | `url`, `expires_at`, `file_name` | はい |
| `get_review_response_url` | オーナーに返す Review Response（JSON）のダウンロード URL（15 分）。判定を書く前は `conflict` | `inbox_id` | `url`, `expires_at`, `file_name`（`mam-review-response-<ID>.json`） | はい |

## 3. アップロードの用途とダウンロードの種別

| 種類 | 名前 | 内容 |
|------|------|------|
| アップロード | `review_response` | `params`: `project_id`（必須）。レビュアーから返った Review Response（JSON、1 MB まで）を、その Project のレビュー依頼への応答として取り込む（`POST /projects/:id/reviews/feedback` と同じ `ImportResponse`。結果はチャットに出る）。結果: `{"review_id", "project_id"}`。形式違い・未知のフィールド・別 Project の依頼への応答・不正な値は 422、取り込み済みは 409、何も保存されない |
| アップロード | `review_package_import` | `params` なし。Review Package（ZIP、2 GiB + 1 MiB まで）をレビュアーとして取り込む（`POST /reviews/import` と同じ `ImportPackage`）。結果: `{"inbox_id", "review_id", "project_name"}`。ZIP でない・`manifest.json` と動画 1 本以外が入っている・動画の形式が違う・大きすぎるは 422、取り込み済みは 409、何も保存されない |
| ダウンロード | `review_package` | リソース ID は `<project_id>/<review_id>`。`application/zip`。ZIP は送りながら書く |
| ダウンロード | `review_video` | リソース ID は `inbox_id`。`video/mp4` / `video/webm` |
| ダウンロード | `review_response_file` | リソース ID は `inbox_id`。`application/json`。ダウンロードのたびに作る（`created_at` は時刻） |

## 4. ルートとツールの対応（レビュー）

| ルート | ツール |
|--------|--------|
| `GET /projects/:id/reviews` | `list_project_reviews` |
| `POST /projects/:id/reviews` | `create_review_request` |
| `GET /projects/:id/reviews/:reviewId/package` | `get_review_package_url` |
| `POST /projects/:id/reviews/feedback` | `upload: review_response` |
| `PUT /projects/:id/review-comments/:commentId` | `set_review_comment_resolved` |
| `GET /reviews` | `list_review_inbox` |
| `POST /reviews/import` | `upload: review_package_import` |
| `GET /reviews/:id` | `get_review` |
| `GET /reviews/:id/video` | `get_review_video_url` |
| `GET /reviews/:id/response` | `get_review_response_url` |
| `POST /reviews/:id/comments` | `add_review_comment` |
| `DELETE /reviews/:id/comments/:commentId` | `delete_review_comment` |
| `PUT /reviews/:id/decision` | `decide_review` |

ルートの `none:` は無い（レビューの全ルートにツールかアップロードの用途がある）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。レビューのツール 11 個（`list_project_reviews`・`create_review_request`・`get_review_package_url`・`set_review_comment_resolved`・`list_review_inbox`・`get_review`・`add_review_comment`・`delete_review_comment`・`decide_review`・`get_review_video_url`・`get_review_response_url`）、アップロードの用途 `review_response`・`review_package_import`、ダウンロードの種別 `review_package`・`review_video`・`review_response_file`、ルートとの対応 | Issue #830。レビュー画面の全操作を MCP ツールにするため |
