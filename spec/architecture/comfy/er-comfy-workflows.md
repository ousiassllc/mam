# ER / データモデル: ComfyUI ワークフロー

[er.md](../er.md) の続き（`er.md` が行数上限を超えているため別ファイル）。[comfy-workflows.md](comfy-workflows.md) のテーブル。改訂履歴は [er.changelog.md](../er.changelog.md)。

```mermaid
erDiagram
    COMFY_WORKFLOW ||--o{ COMFY_WORKFLOW_VERSION : "版ごとの記録"
    COMFY_WORKFLOW {
        text id PK "UUIDv7"
        text name UK "大文字小文字を区別しない。空でない"
        text kind "image / video"
        text endpoint "登録した ComfyUI の接続先（既定 bundled-comfyui）"
        text ui_json "画面の形式（空 = API 形式だけで取り込んだ）"
        text api_json "API の形式（JSON）"
        text assignment_json "入力の割り当て・出力ノード・動画の設定（JSON）"
        int version "現在の版（1 以上）"
        text created_at
        text updated_at
    }
    COMFY_WORKFLOW_VERSION {
        text workflow_id PK,FK
        int version PK
        text api_json
        text assignment_json
        text created_at
    }
```

| テーブル | 内容 |
|---|---|
| `comfy_workflow` | 保存したワークフロー。Project に属さない（全体で 1 つ）。`kind` と `version` は CHECK。`api_json`・`assignment_json` は有効な JSON（CHECK） |
| `comfy_workflow_version` | 各版の `api_json` と `assignment_json`。Take の `model_name`（`comfy:<id>@v<N>`）から「どのワークフローのどの版で作ったか」を追い、古い版を実行するための最小の記録。ワークフローの削除で一緒に消える（`ON DELETE CASCADE`） |

`take.model_name`・`cost_ledger_entry.provider`・Shot の `video_model` の LOCK 値は、外部キーを持たない文字列として識別子を持つ（ワークフローを削除しても Take は残る）。プロジェクトバンドルには含めない（取り込みでは足りない版を警告）。

マイグレーション: `20261025120000_comfy_workflow.sql`（up で 2 テーブルを作成、down で `DROP TABLE`。往復をテストする）。
