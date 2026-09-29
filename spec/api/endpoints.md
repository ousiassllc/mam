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
| GET | `/projects/:id` | Chat Workspace（標準画面。`chat_thread` + `chat_composer`。Take カード内に `mam-video-player` を埋め込み、履歴は `GET /projects/:id/messages` と同じ内容を初期表示） | フルページ / ワークスペースフラグメント |
| GET | `/projects/:id/messages` | チャット履歴のフラグメント取得。クエリ `after`（メッセージID、省略時=全履歴）で新着のみ取得。応答済みになったカードは `hx-swap-oob` で差し替え、Project 状態バッジも同梱する | フラグメントのみ |
| GET | `/projects/:id/messages/:messageId` | 1 枚のカードの現在の描画（進捗カードの自動更新・応答済み Approval カードの再描画用） | フラグメントのみ |
| GET | `/projects/:id/assets/:assetId` | アップロード済みアセットの配信（当該 Project のアセットのみ。`X-Content-Type-Options: nosniff`、`Content-Security-Policy: sandbox`） | ファイル |
| GET | `/projects/:id/brief` | Creative Brief の確認・編集ページ（補助ビュー。Brief は通常チャットで作成する、F-01） | フルページ / フォームフラグメント |
| GET | `/projects/:id/scenes` | Scene / Shot / Take 階層（先頭 Scene を選択。Scene がなければ追加フォームのみ）。補助ビュー | フルページ / 階層フラグメント |
| GET | `/projects/:id/scenes/:sceneId` | Scene詳細（Shot一覧、各 Shot の絵コンテと Take）。クエリ `shot` で 1 つの Shot を選択。他 Project の Scene は 404 | フルページ / 階層フラグメント |
| GET | `/shots/:id` | Shot Inspector（Objective/Duration/Character/Acting/Camera/Lighting/Product/Continuity/References/Prompt/Model/Take List/Judge Results/Locks） | フルページ / Inspectorフラグメント |
| GET | `/shots/:id/takes` | Take比較ページ（`mam-take-comparator` を埋め込み） | フルページ（Lit埋め込みのため常にフルページ） |
| GET | `/projects/:id/edit` | AI Editor / Rough Cut編集ページ（`mam-timeline-editor` を埋め込み） | フルページ |
| GET | `/projects/:id/branches` | Branch ツリーページ（`mam-branch-tree` を埋め込み） | フルページ |
| GET | `/projects/:id/export` | Final Export ページ | フルページ / フラグメント |
| GET | `/projects/:id/settings` | Quality/Cost Policy・Brand Constraints・APIキー設定 | フルページ / フラグメント |
| GET | `/projects/:id/world-state` | World State 一覧（Character/Product/Location/Camera/Story の現在値表示） | フルページ / フラグメント |
| GET | `/projects/:id/notes` | Notes 一覧・編集（補助ビュー、M2。ピン留め・カテゴリ・アーカイブの表示、`version_history_panel` でバージョン復元） | フルページ / フラグメント |
| GET | `/series/:id/notes` | Series 正本 Notes の一覧・編集（M2） | フルページ / フラグメント |

## 2. アクションルート（Gin, フラグメントのみ）

### 2.1 Project / Brief / Mode

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects` | 新規プロジェクト作成。Body（`multipart/form-data`）に最初の発言（`content`）と添付（`attachments`、画像 jpg/png/webp/gif ≤20MB・動画 mp4/mov/webm ≤300MB、最大8件。拡張子と内容の署名を検証し不正は 422）を指定でき、Brief はその発言から抽出してチャットで補完する。成功時は `HX-Redirect: /projects/:id`（201）。既存アセットの再利用は `POST /projects/:id/messages` の `attachment_asset_ids`。Series 作成後の Episode は `POST /series/:id/episodes` を使う | F-01, F-25, F-30 |
| PUT | `/projects/:id/brief` | Brief 更新 | F-01 |
| PUT | `/projects/:id/mode` | AUTO ⇄ DIRECTOR 切替 | F-02 |
| PUT | `/projects/:id/confirmation-level` | FULL AUTO / KEY CHECK / CUSTOM CHECK 設定。Body `level`=`full_auto\|key_check\|custom_check`、`checkpoints`（CUSTOM CHECK の確認ポイント、複数可。空なら前回の選択、無ければ script+final）。変更内容はスレッドに AI 発言として記録される | F-03 |
| PUT | `/projects/:id/quality-policy` | FAST / BALANCED / MAX QUALITY 設定 | F-22 |
| PUT | `/projects/:id/budget` | Project/Shot budget ceiling 設定 | F-22 |
| PUT | `/projects/:id/production-template` | PRODUCT/CM・DRAMA・ANIME・SNS/SHORT VIDEO 切替 | F-21 |
| PUT | `/projects/:id/locks` | Story/Music という Shot に属さないパラメータの AUTO/MANUAL/LOCK 設定（`target_type`=`story\|music` を受理） | F-05 |

### 2.1a Chat / Approval

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/messages` | チャットへの発言。Body `content`、`reply_to_message_id`（任意、返信先カード）、`attachment_asset_ids`（任意、当該 Project の既存アセット）、`attachments`（任意、`multipart/form-data` の新規ファイル。検証は `POST /projects` と同じ）。応答は追加された発言・AI 発言のフラグメント。AI 発言への回答（Question/Escalation カードの選択肢押下を含む）もこの経路。内容は Command Interpreter が解釈し、対象解決・構造化操作・聞き返しのいずれかを実行して AI 発言をスレッドに追記する | F-30, F-24, F-01 |
| POST | `/projects/:id/approvals/:messageId` | Approval カードへの応答。Body `decision`=`approve\|request_changes`、`comment`（任意）。`approve` で AWAITING_APPROVAL から次工程へ進む（`ref_type=note_promotion` のカードを除く）。`ref_type=note_promotion` のカード（M2）は非ブロッキングで、`approve` は `proposed_diff` を主系列 World State に適用して `applied`（LOCK 項目は再判定して除外し、エラーにはしない）、`request_changes` は `rejected` とする。いずれも Project の状態は変わらない。応答は応答済みのカード（ボタンなし）で、`HX-Trigger: chat:refresh` により後続メッセージを取得させる。応答済みのカードへの再応答は 409 | F-30, F-03, F-31 |

### 2.2 Scene / Shot / Lock

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/scenes` | Scene 作成。Body `title`（必須、100文字以内）、`location`/`time`/`objective`（任意）。成功時は追加した Scene を選択した階層フラグメント（201）、入力不正は入力値を保ったまま再描画（422）。スレッドに AI 発言として記録される | F-04 |
| POST | `/scenes/:id/shots` | Shot 作成。Body `objective`（必須、300文字以内）、`duration_seconds`（必須、0 より大きく 60 以下）。成功時は階層フラグメント（201）、入力不正は 422、存在しない Scene は 404 | F-04 |
| PUT | `/shots/:id/locks` | パラメータ単位の AUTO/MANUAL/LOCK 設定（Character Identity/Camera/Product Geometry 等） | F-05 |
| PUT | `/shots/:id/shot-spec` | Canonical Shot Spec の手動編集（DIRECTOR MODE） | F-06 |

M1 の入出力契約（Issue #12）:

- `PUT /projects/:id/mode`: Body `mode`=`auto|director`。応答は MODE コントロール（`chat_composer` 内）のフラグメントで、`HX-Trigger: chat:refresh` によりスレッドに追記された AI 発言（モード切替の通知）を取得させる。不正値は 422、存在しない Project は 404。DIRECTOR の間はパイプラインが工程を進めない（指示した箇所だけを直して待機する）。AUTO に戻すと、DIRECTOR 中に止まっていた工程から再開する。モードは `project.mode` に保持され、切替でほかの状態は変わらない
- `PUT /shots/:id/locks`: Body `parameter`（`dialogue|character_identity|acting|camera|lighting|product_geometry|prompt|video_model|take_selection|editing`）と `mode`（`auto|manual|lock`）、`video_model` のみ `value`（`kling-3.0|seedance-2.0`）。応答は当該パラメータの `lock_toggle` フラグメント。`video_model` は MANUAL でモデル名が必須、LOCK で省略時は最新 Take のモデルを固定する。すでに LOCK 中の値を別の値で LOCK し直す要求は 423。不正なパラメータ/モードは 422、存在しない Shot は 404。`lock` に行が無いパラメータは AUTO
- `PUT /projects/:id/locks`: Body `target_type`（または `parameter`）=`story|music` と `mode`。Story を LOCK すると State Extractor は Story の全項目を変更しない。Shot のパラメータは 422、存在しない Project は 404
- `PUT /shots/:id/shot-spec`: Body `shot_spec` は Canonical Shot Spec の JSON（未知のキーは 422。`duration` は Shot の長さに固定）。`domain.ShotSpec.Validate` を満たさない入力は 422 でフォームを入力値と理由付きで再描画。LOCK 中のパラメータの部分を変える編集は 423 で何も書かず（Prompt を LOCK した Shot は Spec のどの部分も変えられない）、生成中の Shot は 409。成功時は新しい `prompt_version` を追記して Inspector を返す
- `GET /shots/:id`: `HX-Request` でフラグメント、なければフルページ（`shot_inspector_page`）。Canonical Shot Spec・Take List・全パラメータの AUTO/MANUAL/LOCK 切替・Spec の手動編集フォーム・自然言語の指示フォームを表示する
- `POST /shots/:id/director-command`: Body `content`（必須、`POST /projects/:id/messages` と同じ上限）。Command Interpreter が返した操作を実行する。順序は LOCK 設定 → 再生成 → モード切替。再生成は対象 Shot だけを起動し（202、他は 200）、変更する部分以外（Character/Acting/Environment 等）と LOCK 中の部分は Shot Spec に引き継ぐ。LOCK 中の部分を変える再生成を含む指示は 423 で、発言も記録せず何も実行しない（同じ指示の中で LOCK を外していれば許可）。LLM 未構成は 503。発言と結果は AI 発言としてスレッドに記録される
- `POST /projects/:id/messages`: DIRECTOR MODE で Brief 確定後（DRAFT 以外）の発言は Command Interpreter が解釈する（Approval / Question / Escalation カードへの返信は従来どおりその回答）。対象 Shot は「Take カードへの返信 → 発言中の Scene/Shot 番号 → スレッドの直近の Take カード → Shot が 1 つだけの Project」の順で解決し、解決できなければ AI が聞き返して何も実行しない。LOCK 中の部分を変える再生成は実行せず、AI 発言でその旨を伝える。Branch 作成の指示は M2 まで「未対応」と返す
- `POST /shots/:id/generate`: `additional_instruction` を伴う要求は、Prompt が LOCK された Shot では 423。LOCK / 指示で保持する部分は Shot Spec を作り直す際にも変更されず、`video_model` が MANUAL/LOCK の Shot はそのモデルだけで生成する（長さやコストによる別モデルへの切替は行わない）

### 2.3 Prompt / Generation / Judge / Repair

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/shots/:id/reference-pack` | Keyframe / Reference Pack（Character/Product/Location/Style/Lighting Reference）生成・再生成。非同期ジョブを起動し、`/shots/:id/generate` と同じく 202 と GENERATING の Shot カードを返す | F-11 |
| POST | `/shots/:id/generate` | Prompt Autopilot → Model Adapter → Video Generation を実行（非同期ジョブ起動、202+進捗はWSで通知）。Body `additional_instruction`（任意、1000 文字以内）。予算上限に 1 Take も収まらなければ 402、生成中の Shot は 409、存在しない Shot は 404、生成機能が未構成（Model Adapter / LLM / ジョブキューなし）は 503。完了した Take は Take カードとしてチャットに追記され、`take.model_name` と `cost_ledger_entry.provider` は `spec/architecture/model-adapters.md` §6 の正規識別子で保存される | F-06, F-07, F-12 |
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
| POST | `/shots/:id/branch` | Branch Point 保存 + Snapshot 生成（Body: `branch_point_type`=`scene\|shot\|frame`、`frame`時は`offset_seconds`を指定。`scene`指定時は当該Shotの所属Sceneを分岐点とする） | F-17 |
| GET | `/branches/:id` | Branch 詳細（フラグメント） | F-17 |
| PUT | `/branches/:id` | Branch 名称変更・World State参照更新（`branch_version` に新規バージョン行を追記） | F-17, F-28 |
| POST | `/shots/:id/versions/:versionNumber/restore` | Shot/Take（`prompt_version`）の指定バージョンへ復元 | F-28 |
| POST | `/projects/:id/world-state/versions/:versionNumber/restore` | 主系列（トランク、`branch_id`=NULL）World State の指定バージョンへ復元 | F-28 |
| POST | `/branches/:branchId/world-state/versions/:versionNumber/restore` | 当該Branchが独立して持つ World State バージョン系列の指定バージョンへ復元 | F-28 |
| POST | `/branches/:id/versions/:versionNumber/restore` | Branch自体（`branch_version`）の指定バージョンへ復元 | F-28 |

### 2.5 Director Command

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/shots/:id/director-command` | 対象 Shot が確定している自然言語指示 → Command Interpreter → 構造化操作（LOCK設定/パラメータ更新/再生成/Branch作成等）を実行。チャットからの発言は対象解決を含む `POST /projects/:id/messages`（§2.1a）を使い、内部で本エンドポイントと同じ処理を呼ぶ | F-24 |

### 2.6 Editor / Audio / Final QC / Export

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/edit/rough-cut` | Selected Takes から Rough Cut 生成。Body `branch_id`（省略時=主系列トランク）で対象Branchを指定 | F-18 |
| PUT | `/edit-timelines/:id` | タイムライン編集（Cut Point/Timing/Pace/Transition 等の手動調整） | F-18 |
| POST | `/edit-timelines/:id/judge` | Editor Judge 実行（PASS/Re-edit/素材不足を判定） | F-18, F-19 |
| POST | `/edit-timelines/:id/request-additional-shot` | 素材不足時の Director Agent への差し戻し（Editor→Reshoot Loop） | F-19 |
| POST | `/projects/:id/audio/generate` | Audio Production 実行（Dialogue/Narration/BGM/SE/Mixing）。Body `branch_id`（省略時=主系列トランク）で対象Branchを指定 | F-20 |
| POST | `/projects/:id/final-qc` | Final QC 実行 | F-26 |
| POST | `/projects/:id/export` | 最終書き出し実行（Aspect Ratio/解像度/コーデック指定）。Body `branch_id`（省略時=主系列トランク）で対象Branchを指定 | F-26 |

### 2.7 Brand / Cost

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/brand-constraints` | Brand Guideline/禁止表現/Disclaimer/Product Claims/Logo Rules/Character Restrictions/Rights/Platform Rules 登録 | F-23 |
| DELETE | `/brand-constraints/:id` | 制約削除 | F-23 |
| GET | `/projects/:id/cost-summary` | Shot/Scene/Project 別の推定コスト・実コスト集計（`cost_summary_panel`向けフラグメント） | F-22 |

### 2.8 World State

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| PUT | `/projects/:id/characters/:characterId` | Character 属性更新（wardrobe/hairstyle/personality 等、LOCK対象項目は423）。クエリ `branch_id`（省略時=主系列トランク）で更新対象Branchを指定 | F-09 |
| PUT | `/projects/:id/products/:productId` | Product 属性更新（material/orientation/condition 等、LOCK対象項目は423）。クエリ `branch_id`（省略時=主系列トランク）で更新対象Branchを指定 | F-09 |
| PUT | `/projects/:id/locations/:locationId` | Location 属性更新（lighting_state/weather 等、LOCK対象項目は423）。クエリ `branch_id`（省略時=主系列トランク）で更新対象Branchを指定 | F-09 |
| GET | `/projects/:id/world-state` | 現在の World State（Character/Product/Location/Camera/Story）フラグメント取得。クエリ `branch_id`（省略時=主系列トランク）で対象Branchの派生World Stateを取得 | F-09, F-10 |

M1 の入出力契約（Issue #8）:

- `PUT /projects/:id/characters|products|locations/:entityId`: Body は `application/x-www-form-urlencoded`。編集できる項目は Character が `hairstyle/wardrobe/voice/personality/emotion/position/current_action`、Product が `material/orientation/condition`、Location が `time/lighting_state/weather`（各 200 文字以内、前後の空白は除去）。送った項目だけを更新し、空文字は項目のクリア。応答は更新後の World State ビュー（`#world-state-view` フラグメント、200）で、新しい `world_state_version` を追記する。エラーはトースト（`HX-Reswap: none`）: LOCK 対象を1項目でも含むと **423** で全体を拒否（何も書き込まない）、更新項目なし・未知の項目・長すぎる値は 422、Project または当該 Project のエンティティが無ければ 404
- `GET /projects/:id/world-state`: `HX-Request` でフラグメント、なければフルページ（`world_state_page`）。LOCK 中の項目は「LOCK」バッジ付きで編集不可。Camera/Story は Shot 完了ごとに State Extractor が更新するため参照専用
- `branch_id` クエリは Branch（M2）が存在しない M1 では、指定されていれば 404 とする

### 2.9 Series / Episode（M2）

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/series` | Series 作成。Body `from_project_id`（任意）で既存 Project の Character/Product/Location と有効な Notes を Series 正本として登録 | F-29, F-31 |
| POST | `/series/:id/episodes` | Series 正本を継承した Episode（Project）を作成（Character/Product/Location/Notes をコピー）。Body `previous_episode_id`（任意）で前 Episode の最終 World State を開始状態にする | F-29, F-25, F-31 |
| PUT | `/series/:id/characters/:characterId` | Series 正本の Character 更新。既存 Episode には自動伝播しない | F-29 |

### 2.10 Notes（M2）

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/notes` | Project メモ作成。Body `title`、`content`、`category`（任意、省略時は AI が分類）、`pinned`（任意）。チャットからは `POST /projects/:id/messages` の Command Interpreter が同じ処理を呼ぶ（`source=chat`）。作成後、State Extractor が昇格提案を非同期で生成する | F-31 |
| POST | `/series/:id/notes` | Series 正本メモ作成（昇格提案は生成しない） | F-31, F-29 |
| PUT | `/notes/:id` | メモ更新。`title`/`content` が変わる場合は新しい `note_version` を追記する（Project スコープは昇格提案を再生成）。`category`/`pinned`/`archived`（`false` でアーカイブ解除）は行を更新し、バージョンは増えない。Series 正本の更新はコピー済みの Episode へ伝播しない | F-31, F-28 |
| DELETE | `/notes/:id` | メモをアーカイブ（`archived_at` を設定。以降の推論文脈には渡さない） | F-31 |
| POST | `/notes/:id/versions/:versionNumber/restore` | 指定バージョンの内容を新しい `note_version` として追記して復元 | F-31, F-28 |

## 3. API ルート（Huma, JSON, `/api/v1`）

Lit Web Components 専用。HTMX リクエストは発行しない（`spec/architecture/overview.md` §7 の境界ルール）。

| Method | Path | 概要 | 利用コンポーネント |
|---|---|---|---|
| GET | `/api/v1/shots/:id` | Shot詳細（shot_spec, duration, selected_take_id等） | `mam-video-player`, `mam-timeline-editor` |
| GET | `/api/v1/shots/:id/takes` | Take一覧（スコア: Product/Acting/Camera/Continuity, Status） | `mam-take-comparator` |
| GET | `/api/v1/takes/:id/judge-result` | 最新のJudge Issue一覧（`time_range`/`frames`付き）。同一Takeの過去Judge履歴は含まない（最新1件） | `mam-video-player`（フレームマーカー重畳） |
| GET | `/api/v1/projects/:id/edit-timeline` | 現在の Edit Timeline（Shot順序/Cut Point/Transition）。クエリ `branch_id`（省略時=主系列トランク）で対象Branchを指定 | `mam-timeline-editor` |
| PUT | `/api/v1/projects/:id/edit-timeline` | Edit Timeline 保存（ドラッグ&ドロップ操作の反映）。クエリ `branch_id`（省略時=主系列トランク）で対象Branchを指定 | `mam-timeline-editor` |
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
    created_at: "2026-09-28T10:00:00Z"
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
    metrics:
      face_identity:
        - character_id: "Character_A"
          similarity_mean: 0.71
          similarity_min: 0.42
          sampled_frames: 48
          undetected_ratio: 0.08
    issues:
      - type: "product_logo_deformation"
        severity: "high"
        time_range: "2.1-2.8s"
        frames: [63, 84]
      - type: "face_identity_drift"
        severity: "high"
        time_range: "3.0-3.9s"
    repair_strategy:
      action: "partial_regeneration"
      instructions:
        - "increase_product_reference_strength"
        - "preserve_logo_geometry"
```

## 4. WebSocket

| Path | 概要 |
|---|---|
| `/ws/shots/:id` | 生成中Shotの進捗（キュー待ち/生成中/Judge中/完了/失敗）をプッシュ通知。メッセージは JSON `{"shot_id","action"?:"generate\|reference_pack","stage":"idle\|queued\|generating\|done\|failed","message"?,"takes_done"?,"takes_total"?}`。接続直後に生成中でなければ `stage=idle` を 1 通送る。同一オリジンの接続のみ受理し、存在しない Shot は 404。M1 では Judge 中の通知はまだ流れない（F-13 で追加） |
| `/ws/projects/:id` | プロジェクト全体イベント（Core State Machine遷移、コスト更新、Editor→Reshootループの発火）と、チャットの通知。チャットの通知は本文を含まない JSON `{"type":"message.created","message_id":"<最新メッセージID>"}`（新着あり。クライアントは `GET /projects/:id/messages?after=` で取得）と `{"type":"card.updated","message_id":"<カードID>"}`（進捗カード等の更新。`GET /projects/:id/messages/:messageId` で取得）。同一オリジンの接続のみ受理する |

`lib/ws.ts` 経由で接続し、自動再接続（指数バックオフ、最大10回）を行う（`spec/architecture/overview.md` 参照元 HALT 標準パターン）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-28 | 新規作成 | 機能要件（F-01〜F-28）と ER 設計を HALT の3ルートパターンへ落とし込み定義 |
| 1.1 | 2026-09-28 | World State（Character/Product/Location）のページルート・アクションルート（`GET /projects/:id/world-state`、`PUT /projects/:id/characters/:characterId` 等）を追加 | F-09/F-10（World State管理・差分反映）に対応するエンドポイントが未定義だったため（整合性レビュー指摘） |
| 1.2 | 2026-09-28 | `GET /projects/:id`（Main Workspace）の概要に `mam-video-player` の埋め込みを明記 | 他3つのLit Web Components（timeline-editor/take-comparator/branch-tree）は埋め込み先ページが明記されているのに `mam-video-player` だけ記載が無かったため（整合性レビュー指摘） |
| 1.3 | 2026-09-28 | `POST /shots/:id/reference-pack`（Keyframe/Reference Pack生成）を追加 | F-11（M1必須機能）に対応するエンドポイントが未定義だったため（整合性レビュー指摘） |
| 1.4 | 2026-09-28 | `POST /shots/:id/versions/:versionNumber/restore`（バージョン復元）を追加 | F-28（Reversible Decisions）の「復元」を実行するエンドポイントが未定義だったため（整合性レビュー指摘） |
| 1.5 | 2026-09-28 | `PUT /api/v1/projects/:id/edit-timeline` レスポンス例の `updated_at` を `created_at` に修正 | er.md の `edit_timeline_version` テーブルには `created_at` のみ定義され `updated_at` は存在しない（追記のみで上書きしない設計）ため、API例が実際のカラムと矛盾していた（整合性レビュー指摘） |
| 1.6 | 2026-09-28 | `POST /shots/:id/branch` のリクエストボディに `branch_point_type`（scene/shot/frame）・`offset_seconds` を追加 | UC-5が要求するScene/Frame単位の分岐点作成にShot専用パスしか対応していなかったため（整合性レビュー指摘） |
| 1.7 | 2026-09-28 | 復元エンドポイントをスコープ別に分割（Shot/Take用に加え `POST /projects/:id/world-state/versions/:versionNumber/restore`・`POST /branches/:id/versions/:versionNumber/restore` を追加） | F-28「Shot/Take/Branch/State」全対象の復元を単一のShotスコープパスでは表現できなかったため（整合性レビュー指摘） |
| 1.8 | 2026-09-28 | `PUT /projects/:id/locks`（Story/Music等ShotスコープでないパラメータのAUTO/MANUAL/LOCK設定）を追加 | F-05がShot外パラメータもLOCK対象とするのに対し `PUT /shots/:id/locks` しか存在しなかったため（整合性レビュー指摘） |
| 1.9 | 2026-09-28 | `GET /projects/:id/cost-summary`（Shot/Scene/Project別コスト集計）を追加 | components/overview.mdの`cost_summary_panel`・non-functional.md §6の「コスト可視化」に対応するエンドポイントが未定義だったため（整合性レビュー指摘） |
| 1.10 | 2026-09-28 | `PUT /projects/:id/locks` の概要から「等」を削除し「Story/Music という」に修正 | 対象を`target_type`=`story\|music`の2値に限定する記述と「等」（他にもあることを示唆）が自己矛盾していたため（品質レビュー指摘） |
| 1.11 | 2026-09-28 | `GET /projects/:id/world-state` に `branch_id` クエリを追加。`POST /branches/:branchId/world-state/versions/:versionNumber/restore`（Branch専用World State復元）を追加し、既存の `/projects/:id/world-state/.../restore` は主系列（トランク）専用と明記 | er.mdの`world_state_version`が`(project_id, branch_id, version_number)`UNIQUEとなったのに対し、既存エンドポイントにBranch指定手段がなくトランクとBranchの`version_number`衝突時に対象を一意特定できなかったため（整合性レビュー指摘） |
| 1.12 | 2026-09-28 | Character/Product/Location更新エンドポイントに `branch_id` クエリを追加 | er.md 1.10 で `character`/`product`/`location` に `branch_id` を追加したのに対し、更新対象BranchをAPIから指定する手段がなかったため（整合性レビュー指摘） |
| 1.13 | 2026-09-28 | `PUT /branches/:id`（Branch名称変更・World State参照更新、branch_version新規行生成）を追加 | er.mdの`branch_version`が「Branch自体の主要変更を追記のみで履歴化」としていたのに対し、2件目以降の生成契機となるエンドポイントが存在しなかったため（整合性レビュー指摘） |
| 1.14 | 2026-09-28 | Editor/Audio/Export/edit-timeline取得APIに `branch_id` パラメータ（Body/Query）を追加 | er.md 1.14で`edit_timeline_version`/`audio_timeline_version`/`export`にbranch_idを追加したのに対し、APIから対象BranchのTimeline/Exportを指定する手段が存在しなかったため（整合性レビュー指摘） |
| 1.15 | 2026-09-28 | `GET /api/v1/takes/:id/judge-result` の概要に「最新1件のみ返す」ことを明記 | er.md 1.16で`judge_result`のUNIQUE制約を削除し複数回のJudge履歴を保持可能にしたため、取得APIがどのレコードを返すか明示する必要があったため（整合性レビュー指摘） |
| 1.16 | 2026-09-29 | Series / Episode 用エンドポイント（§2.9）を追加。`GET /api/v1/takes/:id/judge-result` の応答例に `metrics` と `face_identity_drift` Issue を追加 | F-29（Series / Episode Memory, M2）と F-13 の Face Identity Similarity（`spec/spec.md` §12.4）に対応する API 契約がなかったため |
| 1.17 | 2026-09-29 | チャット中心UIに対応し、`GET /projects/:id` を Chat Workspace に変更、`GET /projects/:id/messages`・`POST /projects/:id/messages`・`POST /projects/:id/approvals/:messageId`（§2.1a）を追加。`POST /projects` の Body に最初の発言を追加。`GET /projects/:id/brief` を補助ビューに位置づけ直し | ユーザー要望「基本的にはただのチャット画面にしたい」（`spec/spec.md` §23）。既存の Shot 単位エンドポイントだけではチャットの発言・承認カード応答を受ける経路がなかったため |
| 1.18 | 2026-09-29 | Notes 用のページルート（`/projects/:id/notes`、`/series/:id/notes`）と §2.10 を追加。`POST /projects/:id/approvals/:messageId` に昇格提案カードの挙動を追記。`POST /series`・`POST /series/:id/episodes` に Notes の登録・コピーを追記 | F-31（Notes, M2）に対応する API 契約がなかったため |
| 1.19 | 2026-09-29 | `GET /projects/:id/messages/:messageId`・`GET /projects/:id/assets/:assetId` を追加。`POST /projects` を `multipart/form-data`（添付検証の上限を明記）にし、`PUT /projects/:id/confirmation-level` に `checkpoints` を追加。`/ws/projects/:id` のチャット通知ペイロードと Approval 応答の挙動（`HX-Trigger`、409）を明記 | M1「Project とチャット画面」（Issue #5）の実装で、カードの部分更新・添付ファイルの配信/検証・CUSTOM CHECK の選択・WebSocket ペイロードの契約が未定義だったため |
| 1.20 | 2026-09-29 | `GET /projects/:id/scenes` を追加。Scene/Shot 作成エンドポイントと `GET /projects/:id/scenes/:sceneId` の入力・エラー契約を明記 | M1「Reasoning LLM Adapter と AUTO パイプライン」（Issue #6）で Scene/Shot/Take 階層の表示・選択・作成 API の契約が未定義だったため |
| 1.21 | 2026-09-29 | `POST /shots/:id/generate` の Body・エラー（402/404/409/503）、`POST /shots/:id/reference-pack` の非同期応答、`/ws/shots/:id` のメッセージ形式を明記 | M1「Prompt Autopilot・Model Adapter・Reference Pack・生成オーケストレーション」（Issue #7）で生成 API と進捗通知の契約が未定義だったため |
| 1.22 | 2026-09-29 | §2.8 に M1 の入出力契約を追記（編集できる項目・応答・423/422/404 の条件、`branch_id` は M2 まで 404） | M1「World State（Basic）と State Extractor」（Issue #8）で `PUT .../characters/:id` 等の Body・応答・LOCK 時の挙動が名称レベルでしか定義されていなかったため |
| 1.23 | 2026-09-29 | §2.2 に M1 の入出力契約（mode / locks / shot-spec / Inspector / director-command / チャットの DIRECTOR 解釈）を追記。`POST /shots/:id/generate` の LOCK・Video Model 固定時の挙動を追記 | M1「AUTO/MANUAL/LOCK と DIRECTOR MODE」（Issue #12）で、Body・応答・423/422/409/202 の条件、対象 Shot の解決順、LOCK の判定範囲が名称レベルでしか定義されていなかったため |
