# API 仕様

HALT の3ルートパターン（`spec/architecture/overview.md` §7）に従う。ページルート/アクションルートは Gin が HTML（フルページ or フラグメント）を返し、`/api/v1` は Huma が JSON（OpenAPI 3.1 自動生成、`/api/v1/openapi.json`）を返す。JSON API は主に Lit Web Components（`mam-video-player` / `mam-timeline-editor` / `mam-take-comparator` / `mam-branch-tree`）向け。

## 0. 認証・共通仕様

- 単一ユーザー・ループバック限定（`127.0.0.1`）のため外部向け認証は行わない。すべてのリクエストに CSRF トークン（meta タグ由来）を付与する（`spec/requirements/non-functional.md` §5）
- 外部生成モデル API のキーはサーバー側（`agent/model_adapter`）でのみ保持し、フロントエンドには一切露出しない
- エラーレスポンスは HALT の方針（`spec/architecture/overview.md` 参照元 `skill://halt`）に従う:
  - ページ/アクションルートのバリデーションエラー（422）: フォームをエラー付きで再レンダリング
  - ビジネスエラー（403/409/423等）: OOBスワップでトースト表示
  - APIルートのエラー: Huma による RFC 7807 Problem Details
- 共通ビジネスエラーコード:
  | HTTPステータス | 意味 |
  |---|---|
  | 402 | Project/Shot budget ceiling 超過（F-22） |
  | 403 | Brand/Safety Constraint 違反、または LOCK 対象への変更試行 |
  | 409 | 対象 Shot が既に生成中（多重実行防止） |
  | 423 | LOCK されたパラメータ/State への更新要求 |
  | 429 | 外部生成モデル API のレート制限（Model Adapter からの伝播） |

## 1. ページルート（Gin, HX-Request 分岐）

| Method | Path | 概要 | 返却（通常/HTMX） |
|---|---|---|---|
| GET | `/projects` | プロジェクト一覧・新規作成導線 | フルページ / プロジェクト一覧フラグメント |
| GET | `/projects/:id` | Main Workspace（Scene/Shotタブ + Video Preview + Mode切替） | フルページ / ワークスペースフラグメント |
| GET | `/projects/:id/brief` | Creative Brief 入力ページ（F-01） | フルページ / フォームフラグメント |
| GET | `/projects/:id/scenes/:sceneId` | Scene詳細（Shot一覧） | フルページ / Shot一覧フラグメント |
| GET | `/shots/:id` | Shot Inspector（Objective/Duration/Character/Acting/Camera/Lighting/Product/Continuity/References/Prompt/Model/Take List/Judge Results/Locks） | フルページ / Inspectorフラグメント |
| GET | `/shots/:id/takes` | Take比較ページ（`mam-take-comparator` を埋め込み） | フルページ（Lit埋め込みのため常にフルページ） |
| GET | `/projects/:id/edit` | AI Editor / Rough Cut編集ページ（`mam-timeline-editor` を埋め込み） | フルページ |
| GET | `/projects/:id/branches` | Branch ツリーページ（`mam-branch-tree` を埋め込み） | フルページ |
| GET | `/projects/:id/export` | Final Export ページ | フルページ / フラグメント |
| GET | `/projects/:id/settings` | Quality/Cost Policy・Brand Constraints・APIキー設定 | フルページ / フラグメント |

## 2. アクションルート（Gin, フラグメントのみ）

### 2.1 Project / Brief / Mode

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects` | Brief から新規プロジェクト作成 | F-01, F-25 |
| PUT | `/projects/:id/brief` | Brief 更新 | F-01 |
| PUT | `/projects/:id/mode` | AUTO ⇄ DIRECTOR 切替 | F-02 |
| PUT | `/projects/:id/confirmation-level` | FULL AUTO / KEY CHECK / CUSTOM CHECK 設定 | F-03 |
| PUT | `/projects/:id/quality-policy` | FAST / BALANCED / MAX QUALITY 設定 | F-22 |
| PUT | `/projects/:id/budget` | Project/Shot budget ceiling 設定 | F-22 |
| PUT | `/projects/:id/production-template` | PRODUCT/CM・DRAMA・ANIME・SNS/SHORT VIDEO 切替 | F-21 |

### 2.2 Scene / Shot / Lock

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/scenes` | Scene 作成 | F-04 |
| POST | `/scenes/:id/shots` | Shot 作成 | F-04 |
| PUT | `/shots/:id/locks` | パラメータ単位の AUTO/MANUAL/LOCK 設定（Character Identity/Camera/Product Geometry 等） | F-05 |
| PUT | `/shots/:id/shot-spec` | Canonical Shot Spec の手動編集（DIRECTOR MODE） | F-06 |

### 2.3 Prompt / Generation / Judge / Repair

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/shots/:id/generate` | Prompt Autopilot → Model Adapter → Video Generation を実行（非同期ジョブ起動、202+進捗はWSで通知） | F-06, F-07, F-12 |
| POST | `/shots/:id/generate/candidates` | Prompt Candidate Generation（Quality/Balanced/Economy Mode） | F-08 |
| POST | `/takes/:id/judge` | AI Video Judge を明示的に再実行 | F-13 |
| POST | `/takes/:id/repair` | Judge結果に基づく Repair 実行（Prompt Repair/Partial Regeneration/Full Retake/Different Model/Reference Reinforcement/Edit-Level Repair） | F-14 |

### 2.4 Take / Continue / Branch

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/takes/:id/select` | Take を Selected として確定（DIRECTOR MODEでの手動選択） | F-15 |
| POST | `/shots/:id/takes/partial` | 複数Takeの区間指定による部分合成 | F-15 |
| DELETE | `/takes/:id` | Take を Rejected として除外 | F-15 |
| POST | `/shots/:id/continue` | Continue: 次展開候補（NEXT A/B/C/.../AIに任せる）を提示 | F-16 |
| POST | `/shots/:id/continue/confirm` | 選択した展開候補で次 Shot/Scene を生成 | F-16 |
| POST | `/shots/:id/branch` | Branch Point 保存 + Snapshot 生成 | F-17 |
| GET | `/branches/:id` | Branch 詳細（フラグメント） | F-17 |

### 2.5 Director Command

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/shots/:id/director-command` | 自然言語指示 → Command Interpreter → 構造化操作（LOCK設定/パラメータ更新/再生成/Branch作成等）を実行 | F-24 |

### 2.6 Editor / Audio / Final QC / Export

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/edit/rough-cut` | Selected Takes から Rough Cut 生成 | F-18 |
| PUT | `/edit-timelines/:id` | タイムライン編集（Cut Point/Timing/Pace/Transition 等の手動調整） | F-18 |
| POST | `/edit-timelines/:id/judge` | Editor Judge 実行（PASS/Re-edit/素材不足を判定） | F-18, F-19 |
| POST | `/edit-timelines/:id/request-additional-shot` | 素材不足時の Director Agent への差し戻し（Editor→Reshoot Loop） | F-19 |
| POST | `/projects/:id/audio/generate` | Audio Production 実行（Dialogue/Narration/BGM/SE/Mixing） | F-20 |
| POST | `/projects/:id/final-qc` | Final QC 実行 | F-26 |
| POST | `/projects/:id/export` | 最終書き出し実行（Aspect Ratio/解像度/コーデック指定） | F-26 |

### 2.7 Brand / Cost

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/brand-constraints` | Brand Guideline/禁止表現/Disclaimer/Product Claims/Logo Rules/Character Restrictions/Rights/Platform Rules 登録 | F-23 |
| DELETE | `/brand-constraints/:id` | 制約削除 | F-23 |

## 3. API ルート（Huma, JSON, `/api/v1`）

Lit Web Components 専用。HTMX リクエストは発行しない（`spec/architecture/overview.md` §7 の境界ルール）。

| Method | Path | 概要 | 利用コンポーネント |
|---|---|---|---|
| GET | `/api/v1/shots/:id` | Shot詳細（shot_spec, duration, selected_take_id等） | `mam-video-player`, `mam-timeline-editor` |
| GET | `/api/v1/shots/:id/takes` | Take一覧（スコア: Product/Acting/Camera/Continuity, Status） | `mam-take-comparator` |
| GET | `/api/v1/takes/:id/judge-result` | Judge Issue一覧（`time_range`/`frames`付き） | `mam-video-player`（フレームマーカー重畳） |
| GET | `/api/v1/projects/:id/edit-timeline` | 現在の Edit Timeline（Shot順序/Cut Point/Transition） | `mam-timeline-editor` |
| PUT | `/api/v1/projects/:id/edit-timeline` | Edit Timeline 保存（ドラッグ&ドロップ操作の反映） | `mam-timeline-editor` |
| GET | `/api/v1/projects/:id/branches` | Branch ツリー構造（親子関係、Snapshot参照） | `mam-branch-tree` |
| GET | `/api/v1/openapi.json` | OpenAPI 3.1 スペック（Huma自動生成） | - |

### リクエスト/レスポンス例

```yaml
# POST /shots/:id/generate （アクションルート、フラグメント返却）
Request:
  additional_instruction: "もう少しカメラを寄せて"   # 任意（DIRECTOR MODE指示があれば反映）
Response: 202 Accepted
  # フラグメント: Shot状態=GENERATINGのInspector表示
  # 完了通知は /ws/shots/:id 経由
```

```yaml
# PUT /api/v1/projects/:id/edit-timeline
Request:
  Body:
    shots:
      - shot_id: "shot_003"
        start: 0.0
        end: 4.2
        transition: "cut"
      - shot_id: "shot_004"
        start: 4.2
        end: 7.8
        transition: "cross_dissolve"
Response 200:
  Body:
    version_number: 5
    updated_at: "2026-09-28T10:00:00Z"
Errors:
  409: "他クライアントが同時編集中"（本製品はシングルユーザーのため通常発生しないが排他制御として定義）
  422: "shot_id が存在しない / start >= end"
```

```yaml
# GET /api/v1/takes/:id/judge-result
Response 200:
  Body:
    result: "fail"
    overall_confidence: 0.94
    issues:
      - type: "product_logo_deformation"
        severity: "high"
        time_range: "2.1-2.8s"
        frames: [63, 84]
    repair_strategy:
      action: "partial_regeneration"
      instructions:
        - "increase_product_reference_strength"
        - "preserve_logo_geometry"
```

## 4. WebSocket

| Path | 概要 |
|---|---|
| `/ws/shots/:id` | 生成中Shotの進捗（キュー待ち/生成中/Judge中/完了/失敗）をプッシュ通知 |
| `/ws/projects/:id` | プロジェクト全体イベント（Core State Machine遷移、コスト更新、Editor→Reshootループの発火） |

`lib/ws.ts` 経由で接続し、自動再接続（指数バックオフ、最大10回）を行う（`spec/architecture/overview.md` 参照元 HALT 標準パターン）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-28 | 新規作成 | 機能要件（F-01〜F-28）と ER 設計を HALT の3ルートパターンへ落とし込み定義 |
