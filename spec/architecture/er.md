# ER / データモデル

`spec/spec.md` §9（World State Schema）・§12（Judge Output）・§13（Repair/Retake）・§14（Take System）・§21（Project Data Model）・§22（Core State Machine）を SQLite テーブル設計へ落とし込む。永続化先は `spec/architecture/overview.md` の SQLite（WAL mode）。

## 1. 全体 ER 図

```mermaid
erDiagram
    PROJECT ||--|| BRIEF : has
    PROJECT ||--o{ ASSET : owns
    PROJECT ||--o{ CHARACTER : owns
    PROJECT ||--o{ PRODUCT : owns
    PROJECT ||--o{ LOCATION : owns
    PROJECT ||--o{ SCRIPT_VERSION : has
    PROJECT ||--o{ SCENE : contains
    PROJECT ||--o{ WORLD_STATE_VERSION : tracks
    PROJECT ||--o{ BRANCH : has
    PROJECT ||--o{ LOCK : configures
    PROJECT ||--o{ EDIT_TIMELINE_VERSION : has
    PROJECT ||--o{ AUDIO_TIMELINE_VERSION : has
    PROJECT ||--o{ EXPORT : produces
    PROJECT ||--o{ COST_LEDGER_ENTRY : accrues
    PROJECT ||--o{ BRAND_CONSTRAINT : enforces

    SCENE ||--o{ SHOT : contains
    SHOT ||--o{ PROMPT_VERSION : has
    SHOT ||--o{ STATE_DIFF : produces
    SHOT }o--o| TAKE : selected_take_ref

    PROMPT_VERSION ||--o{ TAKE : generates
    TAKE ||--o| JUDGE_RESULT : evaluated_by
    JUDGE_RESULT ||--o| REPAIR_ACTION : triggers
    REPAIR_ACTION }o--o| TAKE : produces_next

    STATE_DIFF }o--|| WORLD_STATE_VERSION : creates
    BRANCH }o--|| WORLD_STATE_VERSION : snapshot_of
    BRANCH }o--o| SHOT : branch_point
    BRANCH }o--o| SCENE : scene_branch_point
    BRANCH ||--o{ BRANCH_VERSION : has
    BRANCH ||--o{ SCENE : branch_scenes
    BRANCH ||--o{ WORLD_STATE_VERSION : branch_versions
    BRANCH ||--o{ CHARACTER : branch_characters
    BRANCH ||--o{ PRODUCT : branch_products
    BRANCH ||--o{ LOCATION : branch_locations
    BRANCH ||--o{ EDIT_TIMELINE_VERSION : branch_edit_timelines
    BRANCH ||--o{ AUDIO_TIMELINE_VERSION : branch_audio_timelines
    BRANCH ||--o{ EXPORT : branch_exports
    BRANCH_VERSION }o--o| WORLD_STATE_VERSION : version_snapshot

    EDIT_TIMELINE_VERSION ||--o{ EDITOR_JUDGE_RESULT : evaluated_by

    PROJECT {
        string id PK
        string state
        string mode
        string confirmation_level
        string production_template
        string quality_policy
        decimal project_budget_ceiling
        decimal shot_budget_ceiling
        datetime created_at
    }
```

## 2. ドメイン別 ER 図

### 2.1 制作進行（Scene / Shot / Take / Judge / Repair）

```mermaid
erDiagram
    SCENE ||--o{ SHOT : contains
    SHOT ||--o{ PROMPT_VERSION : has
    SHOT }o--o| TAKE : selected_take_ref
    PROMPT_VERSION ||--o{ TAKE : generates
    TAKE ||--o| JUDGE_RESULT : evaluated_by
    JUDGE_RESULT ||--o| REPAIR_ACTION : triggers
    REPAIR_ACTION }o--o| TAKE : produces_next

    SCENE {
        string id PK
        string project_id FK
        string branch_id FK
        int order_index
        string title
        string location_id FK
        string time
        string objective
        string state
    }
    SHOT {
        string id PK
        string scene_id FK
        int order_index
        string objective
        float duration_seconds
        json shot_spec
        string state
        string selected_take_id FK
    }
    PROMPT_VERSION {
        string id PK
        string shot_id FK
        int version_number
        json shot_spec_snapshot
        json model_adapter_output
        datetime created_at
    }
    TAKE {
        string id PK
        string prompt_version_id FK
        string model_name
        json model_parameters
        string file_path
        string status
        json scores
        datetime created_at
    }
    JUDGE_RESULT {
        string id PK
        string take_id FK
        string result
        float overall_confidence
        json issues
        json preserve
        datetime created_at
    }
    REPAIR_ACTION {
        string id PK
        string judge_result_id FK
        string action_type
        json instructions
        string resulting_take_id FK
        datetime created_at
    }
```

### 2.2 World State / Continuity

```mermaid
erDiagram
    PROJECT ||--o{ CHARACTER : owns
    PROJECT ||--o{ PRODUCT : owns
    PROJECT ||--o{ LOCATION : owns
    PROJECT ||--o{ WORLD_STATE_VERSION : tracks
    SHOT ||--o{ STATE_DIFF : produces
    STATE_DIFF }o--|| WORLD_STATE_VERSION : creates
    BRANCH }o--|| WORLD_STATE_VERSION : snapshot_of
    BRANCH }o--o| SHOT : branch_point
    BRANCH }o--o| SCENE : scene_branch_point
    BRANCH ||--o{ BRANCH_VERSION : has
    BRANCH ||--o{ WORLD_STATE_VERSION : branch_versions
    BRANCH ||--o{ SCENE : branch_scenes
    BRANCH_VERSION }o--o| WORLD_STATE_VERSION : version_snapshot
    BRANCH ||--o{ CHARACTER : branch_characters
    BRANCH ||--o{ PRODUCT : branch_products
    BRANCH ||--o{ LOCATION : branch_locations

    CHARACTER {
        string id PK
        string project_id FK
        string branch_id FK
        string name
        string face_reference_asset_id FK
        string body_reference_asset_id FK
        string hairstyle
        string wardrobe
        string voice
        string personality
        string lock_state
    }
    PRODUCT {
        string id PK
        string project_id FK
        string branch_id FK
        string name
        string geometry_reference_asset_id FK
        string label_reference_asset_id FK
        string material
        string orientation
        string condition
        string lock_state
    }
    LOCATION {
        string id PK
        string project_id FK
        string branch_id FK
        string name
        string layout_reference_asset_id FK
        string time
        string lighting_state
        string weather
        string lock_state
    }
    WORLD_STATE_VERSION {
        string id PK
        string project_id FK
        string branch_id FK
        int version_number
        json state
        datetime created_at
    }
    STATE_DIFF {
        string id PK
        string shot_id FK
        string take_id FK
        json diff
        string world_state_version_id FK
        datetime applied_at
    }
    BRANCH {
        string id PK
        string project_id FK
        string branch_point_type
        string branch_point_scene_id FK
        string branch_point_shot_id FK
        float branch_point_offset_seconds
        string name
        string world_state_version_id FK
        datetime created_at
    }
    BRANCH_VERSION {
        string id PK
        string branch_id FK
        int version_number
        string name
        string world_state_version_id FK
        datetime created_at
    }
```

### 2.3 編集・音声・書き出し

```mermaid
erDiagram
    PROJECT ||--o{ EDIT_TIMELINE_VERSION : has
    EDIT_TIMELINE_VERSION ||--o{ EDITOR_JUDGE_RESULT : evaluated_by
    PROJECT ||--o{ AUDIO_TIMELINE_VERSION : has
    PROJECT ||--o{ EXPORT : produces
    BRANCH ||--o{ EDIT_TIMELINE_VERSION : branch_edit_timelines
    BRANCH ||--o{ AUDIO_TIMELINE_VERSION : branch_audio_timelines
    BRANCH ||--o{ EXPORT : branch_exports

    EDIT_TIMELINE_VERSION {
        string id PK
        string project_id FK
        string branch_id FK
        int version_number
        json timeline
        datetime created_at
    }
    EDITOR_JUDGE_RESULT {
        string id PK
        string edit_timeline_version_id FK
        string result
        json findings
        datetime created_at
    }
    AUDIO_TIMELINE_VERSION {
        string id PK
        string project_id FK
        string branch_id FK
        int version_number
        json timeline
        datetime created_at
    }
    EXPORT {
        string id PK
        string project_id FK
        string branch_id FK
        string file_path
        string resolution
        string aspect_ratio
        string codec
        datetime exported_at
    }
```

### 2.4 制御・コスト・ブランド制約

```mermaid
erDiagram
    PROJECT ||--o{ LOCK : configures
    PROJECT ||--o{ COST_LEDGER_ENTRY : accrues
    PROJECT ||--o{ BRAND_CONSTRAINT : enforces

    LOCK {
        string id PK
        string project_id FK
        string target_type
        string target_id
        string parameter_name
        string mode
        json value
    }
    COST_LEDGER_ENTRY {
        string id PK
        string project_id FK
        string shot_id FK
        string take_id FK
        string provider
        decimal estimated_cost
        decimal actual_cost
        datetime created_at
    }
    BRAND_CONSTRAINT {
        string id PK
        string project_id FK
        string type
        string enforcement_level
        json value
    }
```

## 3. テーブル定義・制約・インデックス

| テーブル | 主要制約 | インデックス |
|---------|---------|------------|
| `project` | `id` PK。`state` は Core State Machine（§22）の値のみ許可（CHECK制約）。`mode` は `auto/director`、`confirmation_level` は `full_auto/key_check/custom_check`、`production_template` は `product_cm/drama/anime/sns_short`、`quality_policy` は `fast/balanced/max_quality` のいずれか（各CHECK制約） | `idx_project_state (state)` |
| `brief` | `project_id` UNIQUE FK（1:1） | - |
| `asset` | `project_id` FK。`type` は `image/video/reference` のいずれか | `idx_asset_project (project_id)` |
| `character` / `product` / `location` | `project_id` FK。`branch_id` FK（NULL許容、NULL=主系列、非NULL=当該BranchのWorld State固有値）。`lock_state` は `auto/manual/lock` のいずれか（CHECK制約） | `idx_character_project (project_id, branch_id)` 等 |
| `script_version` | `project_id` FK。`(project_id, version_number)` UNIQUE | - |
| `scene` | `project_id` FK。`branch_id` FK（NULL許容、NULL=主系列/トランク、非NULL=当該Branchの派生ツリーに属するScene）。`(project_id, branch_id, order_index)` UNIQUE | `idx_scene_project_branch_order (project_id, branch_id, order_index)` |
| `shot` | `scene_id` FK。`selected_take_id` は `take.id` への FK（NULL許容）。`state` は Core State Machine のサブ状態のみ許可 | `idx_shot_scene_order (scene_id, order_index)` |
| `prompt_version` | `shot_id` FK。`(shot_id, version_number)` UNIQUE | `idx_prompt_version_shot (shot_id)` |
| `take` | `prompt_version_id` FK。`status` は `candidate/ai_pick/selected/rejected` のいずれか | `idx_take_prompt_version (prompt_version_id)`, `idx_take_status (status)` |
| `judge_result` | `take_id` UNIQUE FK（最新判定を1件、履歴は再生成のたびに新規Takeを作るため1:1で十分）。`result` は `pass/fail` | `idx_judge_result_take (take_id)` |
| `repair_action` | `judge_result_id` FK。`resulting_take_id` は次に生成された `take.id`（NULL許容 = 未生成） | `idx_repair_action_judge_result (judge_result_id)` |
| `world_state_version` | `project_id` FK。`branch_id` FK（NULL許容、NULL=主系列、非NULL=当該Branchが独立して持つバージョン系列）。`(project_id, branch_id, version_number)` UNIQUE、Branch単位で`version_number`は独立して単調増加 | `idx_world_state_version_project (project_id, branch_id, version_number)` |
| `state_diff` | `shot_id` FK、`take_id` FK、`world_state_version_id` FK | `idx_state_diff_shot (shot_id)` |
| `branch` | `project_id` FK。`branch_point_type` は `scene/shot/frame` のいずれか（CHECK制約）。`branch_point_scene_id`（type=scene時）/`branch_point_shot_id`（type=shot/frame時）はいずれかNULL許容。`branch_point_offset_seconds`（type=frame時のみ使用、他はNULL）。`world_state_version_id` FK（Snapshot） | `idx_branch_project (project_id)` |
| `branch_version` | `branch_id` FK。`(branch_id, version_number)` UNIQUE、`version_number` は単調増加。Branch自体の主要変更（名称変更・World State参照更新等）を追記のみで履歴化する | `idx_branch_version_branch (branch_id, version_number)` |
| `edit_timeline_version` / `audio_timeline_version` | `project_id` FK。`branch_id` FK（NULL許容、NULL=主系列トランク、非NULL=当該BranchのTimeline）。`(project_id, branch_id, version_number)` UNIQUE | `idx_edit_timeline_project (project_id, branch_id, version_number)` |
| `editor_judge_result` | `edit_timeline_version_id` FK。`result` は `pass/re_edit/need_shot` | `idx_editor_judge_result_timeline (edit_timeline_version_id)` |
| `export` | `project_id` FK。`branch_id` FK（NULL許容、NULL=主系列トランクからの書き出し） | `idx_export_project (project_id, branch_id)` |
| `lock` | `project_id` FK。`(project_id, target_type, target_id, parameter_name)` UNIQUE。`mode` は `auto/manual/lock` | `idx_lock_project_target (project_id, target_type, target_id)` |
| `cost_ledger_entry` | `project_id` FK。`shot_id`/`take_id` は NULL 許容（プロジェクト全体コストの場合） | `idx_cost_ledger_project (project_id)` |
| `brand_constraint` | `project_id` FK。`type` は `brand_guideline/forbidden_expression/required_disclaimer/product_claim/logo_rule/character_restriction/asset_rights/platform_rule` のいずれか。`enforcement_level` は `lock/advisory` のいずれか（CHECK制約、`lock`はスコアに関係なく違反時FAIL） | `idx_brand_constraint_project (project_id)` |

## 4. 設計メモ

- **Shot Spec の格納**: `shot.shot_spec` および `prompt_version.shot_spec_snapshot` は `spec/spec.md` §7.1 の YAML 構造を JSON にシリアライズして格納する（モデル非依存の Canonical Shot Spec を Source of Truth とする、`spec/architecture/overview.md` §1 の設計原則に対応）
- **Take と再生成の関係**: Repair/Retake により再生成された結果は新しい `take` 行として追加する（既存 Take は不変・Traceability保持）。`repair_action.resulting_take_id` で追跡する
- **World State のバージョニング**: Shot完了ごとに `state_diff` を生成し、新しい `world_state_version` を追加する（追記のみ、上書きしない）。ユーザーが LOCK した項目は State Extractor（Agent側ロジック）が diff 対象から除外する
- **Branch の Snapshot**: Branch作成時点の `world_state_version_id` を参照するのみで、Branch自体は新しい `world_state_version` 系列を独立して持つ（Branch以降の変更は元系列に影響しない）
- **JSON カラム**: SQLite の `JSON` 型（TEXT + `json_valid` CHECK制約）を用いる。将来的な検索要件が増えた場合は生成列（generated column）でインデックスを追加する

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-28 | 新規作成 | `spec/spec.md` §9,12,13,14,21 の概念モデルを SQLite テーブル設計として具体化 |
| 1.1 | 2026-09-28 | 全体ER図で `SHOT` から自分自身への誤った自己参照になっていた `selected_take_ref` の関係定義を `SHOT` → `TAKE` へ修正 | `shot.selected_take_id` が `take.id` への FK である実際のテーブル定義と図の関係定義が矛盾していたための表記修正 |
| 1.2 | 2026-09-28 | 全体ER図の `PROJECT` エンティティに `mode`/`confirmation_level`/`production_template`/`quality_policy`/`project_budget_ceiling`/`shot_budget_ceiling` 属性を追加し、`project` テーブル定義の制約説明を更新 | F-02/F-03/F-21/F-22 に対応する `PUT /projects/:id/mode` 等のAPIエンドポイントの書き込み先カラムが仕様上存在しなかったため（整合性レビュー指摘） |
| 1.3 | 2026-09-28 | `BRAND_CONSTRAINT` に `enforcement_level`（`lock/advisory`）カラムを追加 | F-23/UC-9の「LOCK違反はスコアに関係なくFAIL」を判定するための強制レベル区分が存在しなかったため（整合性レビュー指摘） |
| 1.4 | 2026-09-28 | 全体ER図の `SHOT }o--\|\| TAKE` / `REPAIR_ACTION }o--\|\| TAKE` のカーディナリティを `}o--o\|`（zero-or-one）へ修正 | `shot.selected_take_id`・`repair_action.resulting_take_id` は §3 でNULL許容と明記されているのに図では「exactly one」表記になっていたため（品質レビュー指摘、自動修正） |
| 1.5 | 2026-09-28 | `BRANCH` に `branch_point_type`/`branch_point_scene_id`/`branch_point_offset_seconds` を追加し、Scene/Frame単位の分岐点を表現可能にした | UC-5/用語集の「任意のScene/Shot/Frameを分岐点として保存」に対しShotのみしか表現できていなかったため（整合性レビュー指摘） |
| 1.6 | 2026-09-28 | `BRANCH` に `version_number` を追加し `(project_id, version_number)` UNIQUE制約とした | F-28「Shot/Take/Branch/State の主要変更のバージョン化と復元」に対しBranch自体のバージョン管理カラムが存在しなかったため（整合性レビュー指摘） |
| 1.7 | 2026-09-28 | `BRANCH` から `version_number` を削除し、代わりに `BRANCH \|\|--o{ BRANCH_VERSION` の子テーブル `branch_version`（`branch_id`, `(branch_id, version_number)` UNIQUE）を追加。§2.2の関係線を `BRANCH }o--o\| SHOT`・`BRANCH }o--o\| SCENE`・`BRANCH \|\|--o{ BRANCH_VERSION` へ修正し §1/§3と整合させた | `(project_id, version_number)` UNIQUEでは1 Branch行につきversion_numberが1つしか持てず「Branchごとの複数バージョン履歴」を表現できなかったため。また§2.2の関係線が§1・§3のNULL許容/子テーブル構造と食い違っていたため（整合性・品質レビュー指摘） |
| 1.8 | 2026-09-28 | `scene`・`world_state_version` に nullable `branch_id` を追加（NULL=主系列）。`world_state_version` のUNIQUE制約を `(project_id, branch_id, version_number)` へ変更しBranchごとに独立したバージョン系列を持てるようにした | UC-5「各Branchが独立した派生プロジェクトツリーとして保持される」・設計メモ「Branch自体は新しいworld_state_version系列を独立して持つ」を実現するカラムがER図に存在しなかったため（整合性レビュー指摘） |
| 1.9 | 2026-09-28 | §1・§2.2に `BRANCH \|\|--o{ SCENE : branch_scenes`（§1のみ存在し§2.2に欠落）と `BRANCH_VERSION }o--o\| WORLD_STATE_VERSION : version_snapshot`（両図とも欠落）を追加 | `scene.branch_id`・`branch_version.world_state_version_id` というFKに対応する関係線が図に描かれておらず、他のFKと非対称だったため（整合性・品質レビュー指摘） |
| 1.10 | 2026-09-28 | `character`/`product`/`location` に nullable `branch_id` を追加し、§1・§2.2に `BRANCH \|\|--o{ CHARACTER/PRODUCT/LOCATION` 関係線を追加 | Branch上でのCharacter/Product/Location編集がトランクや他Branchに影響してしまい、round3で導入したBranch独立性（world_state_version/sceneのbranch_id）と設計意図が食い違っていたため（整合性レビュー指摘） |
| 1.11 | 2026-09-28 | §2.1に `SHOT }o--o\| TAKE : selected_take_ref` と `REPAIR_ACTION }o--o\| TAKE : produces_next` を追加 | `shot.selected_take_id`・`repair_action.resulting_take_id` というFK（§2.1のSHOT/REPAIR_ACTION属性ブロックには既に記載済み）に対応する関係線が§1には存在するのに§2.1には描かれておらず、TAKE/SHOT/REPAIR_ACTIONが全て§2.1に描画済みであるにもかかわらず非対称だったため（品質レビュー指摘） |
| 1.12 | 2026-09-28 | `scene` のUNIQUE制約を `(project_id, order_index)` から `(project_id, branch_id, order_index)` へ変更 | round3で`scene.branch_id`を追加した際、UNIQUE制約が未修正のままでBranch配下のSceneがトランク/他Branchと同じ`order_index`を使うと制約違反になり、UC-5「各Branchが独立した派生プロジェクトツリー」を満たせなかったため（整合性レビュー指摘） |
| 1.13 | 2026-09-28 | `brand_constraint.type` のCHECK制約に `brand_guideline` を追加 | UC-9・`POST /projects/:id/brand-constraints`の概要が「Brand Guideline」を登録対象の先頭に挙げているのに対し、typeの許容値7種にbrand_guidelineが存在せずCHECK制約違反となっていたため（整合性レビュー指摘） |
| 1.14 | 2026-09-28 | `edit_timeline_version`/`audio_timeline_version`/`export` に nullable `branch_id` を追加。UNIQUE制約を `(project_id, branch_id, version_number)` へ変更し、§1・§2.3に `BRANCH \|\|--o{ EDIT_TIMELINE_VERSION/AUDIO_TIMELINE_VERSION/EXPORT` 関係線を追加 | scene/character/product/location/world_state_versionはBranch独立化済みだったが編集・音声・書き出しにはbranch_idが無く、Branch別の編集・音声・Exportができなかったため（整合性レビュー指摘） |
