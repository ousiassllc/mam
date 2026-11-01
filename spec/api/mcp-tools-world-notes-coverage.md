# MCP ツール: World State・参照・Branch・バージョン・Series・メモ — ルートの対応

[`mcp-tools-world-notes.md`](mcp-tools-world-notes.md) のツールが、ドメインの画面のどのルートを担うか。画面の HTMX の部分表示（フラグメントだけを返すもの）は、同じデータを返すツールに畳んである。このファイルの表は `backend/internal/mcp/tools/worldnotes/coverage_test.go` が読み、書かれたツールがすべて登録されていることを確かめる。

## ルート → ツール

| ルート | ツール |
|--------|--------|
| `GET /projects/:id/world-state` | `get_world_state`, `list_world_state_versions`（クエリ `branch_id` は引数） |
| `PUT /projects/:id/characters/:characterId` | `update_character` |
| `PUT /projects/:id/products/:productId` | `update_product` |
| `PUT /projects/:id/locations/:locationId` | `update_location` |
| `PUT /projects/:id/world-state/locks` | `set_world_field_lock` |
| `PUT /projects/:id/characters/:characterId/reference` | `assign_character_reference` |
| `PUT /projects/:id/products/:productId/reference` | `assign_product_reference` |
| `PUT /projects/:id/locations/:locationId/reference` | `assign_location_reference` |
| `GET /projects/:id/references` | `list_references` |
| `POST /projects/:id/references` | `create_upload_ticket`（基盤。`purpose=reference_image`） |
| `DELETE /projects/:id/references/:referenceId` | `delete_reference` |
| `POST /shots/:id/references` | `pin_reference` |
| `GET /projects/:id/assets/:assetId` | `get_asset_download_url`（基盤。Library の画像の `asset_id`） |
| `GET /projects/:id/branches` | `get_branch_tree` |
| `GET /branches/:id` | `get_branch` |
| `POST /shots/:id/branch` | `create_branch` |
| `PUT /branches/:id` | `update_branch` |
| `GET /branches/:id/versions` | `get_branch_versions` |
| `GET /shots/:id` | `list_shot_versions`（Shot Inspector のバージョン一覧だけ。Inspector 全体は Shot のドメインの `get_shot_inspector`） |
| `POST /shots/:id/versions/:versionNumber/restore` | `restore_shot_version` |
| `POST /projects/:id/world-state/versions/:versionNumber/restore` | `restore_world_state_version` |
| `POST /branches/:id/world-state/versions/:versionNumber/restore` | `restore_branch_world_state_version` |
| `POST /branches/:id/versions/:versionNumber/restore` | `restore_branch_version` |
| `GET /series/:id` | `get_series` |
| `POST /series` | `create_series` |
| `POST /series/:id/episodes` | `create_episode` |
| `PUT /series/:id/characters/:characterId` | `update_series_character` |
| `PUT /series/:id/style` | `update_series_style` |
| `PUT /series/:id/characters/:characterId/voice-reference` | `create_upload_ticket`（基盤。`purpose=character_voice_reference`）, `update_character_voice_reference` |
| `DELETE /series/:id/characters/:characterId/voice-reference` | `delete_character_voice_reference` |
| `GET /series/:id/characters/:characterId/voice-reference` | `get_character_voice_reference_url` |
| `PUT /projects/:id/characters/:characterId/voice-reference` | `create_upload_ticket`（基盤。`purpose=character_voice_reference`）, `update_character_voice_reference` |
| `DELETE /projects/:id/characters/:characterId/voice-reference` | `delete_character_voice_reference` |
| `GET /projects/:id/characters/:characterId/voice-reference` | `get_character_voice_reference_url` |
| `GET /projects/:id/notes` | `list_project_notes` |
| `GET /series/:id/notes` | `list_series_notes` |
| `POST /projects/:id/notes` | `create_project_note` |
| `POST /series/:id/notes` | `create_series_note` |
| `PUT /notes/:id` | `update_note`, `archive_note`（アーカイブは `archived=true` と同じ） |
| `DELETE /notes/:id` | `archive_note`（ノートを消さずアーカイブする） |
| `POST /notes/:id/versions/:versionNumber/restore` | `restore_note_version` |

## ツールにしないもの

| ルート・画面 | 理由 |
|--------------|------|
| `GET /series`（Series の一覧） | 画面にもルートにもない（Series へは Project の `series_id` から入る）。`list_projects` の `series_id` を使う |
| メモの World State への反映の提案（承認カード） | チャットの承認カードで答える操作。`respond_approval`（チャットのドメイン） |
| `GET /api/v1/...`（JSON API） | このドメインには無い |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。ルートとツールの対応表 | Issue #830。画面の全操作を MCP ツールにする |
| 1.1 | 2026-11-01 | 参照音声のルート 6 行を足した（`PUT`・`DELETE`・`GET` の Series と Project） | Issue #919（#912） |
