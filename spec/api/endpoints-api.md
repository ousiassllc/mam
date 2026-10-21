# API 仕様（API ルートと WebSocket）

[endpoints.md](endpoints.md)（§0 認証・共通仕様、§1 ページルート、§2 アクションルート）の続きで、§3 API ルート（Huma, JSON, `/api/v1`）と §4 WebSocket を置く。
行数上限（`spec/environment/setup.md` の「Linterly」節）のため分けたもので、節番号は endpoints.md から引き継ぐ。改訂履歴は [endpoints.changelog.md](endpoints.changelog.md)。

## 3. API ルート（Huma, JSON, `/api/v1`）

Lit Web Components 専用。HTMX リクエストは発行しない（`spec/architecture/overview.md` §7 の境界ルール）。

| Method | Path | 概要 | 利用コンポーネント |
|---|---|---|---|
| GET | `/api/v1/shots/:id` | Shot詳細（`id`/`scene_id`/`project_id`/`order_index`/`objective`/`duration_seconds`/`shot_spec`/`state`/`selected_take_id`（未選択なら省略））。存在しない Shot は 404 | 現状なし（Lit は使わない。`mam-video-player` は Take の `judge-result`、`mam-timeline-editor` は `edit-timeline` を読む） |
| GET | `/api/v1/shots/:id/takes` | Take一覧（古い順。応答の最上位は `shot_id`・`duration_seconds`（Shot の計画尺）・`takes`、下記の `partial_url`・`limits`。各 Take は `id`・`label`（A, B, …）・`status`（candidate/ai_pick/selected/rejected）・`status_label`（画面に出す状態名: 候補 / AI Pick / 採用 / 除外。Shot カードと同じ）・`adopted`（Shot が使う Take）・`model_name`（生成に使ったモデル名）・`created_at`・`scores`（product/acting/camera/continuity/overall、各 0-100。最新 Judge の Issue から算出: 各軸 100 から high 35・medium 15・low 5 を、Issue ごとにその `dimension` の軸から引く（下限 0。`domain.ScoreTake`）。product=`product_accuracy`/`product_geometry`/`logo_text_accuracy`/`object_consistency`、acting=`character_identity`/`character_consistency`/`face`/`hands`/`pose`/`acting`/`lip_sync`/`motion_quality`、camera=`camera_motion`/`composition`/`lighting`、continuity=`scene_continuity`/`background_consistency`/`temporal_consistency`/`flicker`/`shot_compatibility`/`physics`。`prompt_adherence`（Take 全体が指示と合っていない指摘）は同じ減点を 4 軸すべてから引く。`type=face_identity_drift` の Issue は dimension によらず acting だけから引く。22 個の dimension はどれかの軸に対応する。AI Pick の順位・Minimum acceptance/axis score の判定・Multi-model routing の quality・Take 比較の赤表示はこのスコアを使う。未 Judge は null）・`verdict`・`resolution`/`fps`/`duration_seconds`・`proxied`・`video_url`/`thumbnail_url`/`judge_result_url`、操作可能なときだけ返る `select_url`（選択可能な Take のみ）/`reject_url`（除外可能な Take のみ）、区間指定に使える Take を示す `partial_usable`、区間指定が可能なときだけ返る Shot の `partial_url`、および区間指定の上限と低スコアの下限を渡す `limits`（`max_segments`・`min_segment_seconds`・`min_axis_score`。前 2 つは `domain.MaxTakeSegments` / `MinTakeSegmentSeconds` と同じ値。`min_axis_score` は Project の Quality Policy にユーザーの上書きを当てた軸ごとの最低点（`domain.ProjectPolicy.Effective`。設定画面の「軸ごとの最低スコア」と同じ値）で、AI Video Judge はこれ未満の軸スコアを不合格にし、`mam-take-comparator` はスコア表のこれ未満の値を赤で出す。クライアントはどの値も持たない）） | `mam-take-comparator` |
| GET | `/api/v1/takes/:id/judge-result` | 最新のJudge Result（`result`/`overall_confidence`/`metrics`/`issues`（`time_range`/`frames`付き）/`preserve`/`repair_strategy`/`created_at`/`scores`）。`scores` は Production Template の評価軸ごとの 0-100 のスコアで、Judge が出さなかったときは省略する（Take 一覧の `scores` 4 軸とは別）。同一Takeの過去Judge履歴は含まない（最新1件）。Take が存在しない、または一度も Judge されていないとき 404 | `mam-video-player`（フレームマーカー重畳） |
| GET | `/api/v1/repair/jev-shadow-report` | Jev のシャドー実行の集計（`runs`/`errors`/`agreed`/`agreement_rate`（失敗を除く一致率）/`mean_confidence`/`mean_latency_ms`（失敗を除く平均）/`total_cost_usd`/`confidence_histogram`（0.1 刻み 10 区間）/`confidence_agreed`）。Jev による前段選別を有効にするかどうかの判断材料（`spec/architecture/model-adapters.md` §4.5・§11） | Repair Agent（F-14） |
| GET | `/api/v1/projects/:id/edit-timeline` | 現在の Edit Timeline（Shot順序/Cut Point/Transition）。Editor Judge の結果は返さない（編集画面の Templ が描く）。Rough Cut がなければ 404。クエリ `branch_id`（省略時=主系列トランク）で Branch ごとの版を返す。応答は `id`・`version_number`・`created_at`・`duration_seconds`・`readonly`・`save_url`・`preview_url`・`transitions`・`limits`（`min_clip_seconds`・`max_transition_seconds`・`max_caption_runes`）・`clips`、Branch の版のときだけ `branch_id`（トランクでは省略）。クリップは `shot_id`・`take_id`・`label`・`kind`・`caption`・`objective`・`in_point`・`out_point`・`start`・`transition` に加え、Take から決まる `source_seconds`（Take ファイルの長さ。Out 点の上限判定に使う参考値）・`video_url`・`thumbnail_url`（Take の動画・サムネイルの URL（`/takes/:id/video`・`/takes/:id/thumbnail`）。`source_seconds` は Take の長さが分からないとき省略）を含む。追加 Shot の依頼は Editor Judge の `need_shot` を受けた Templ の `hx-post`（`/edit-timelines/:id/request-additional-shot`）で行い、この応答に判定の指摘は無い（Lit は URL を受け取らない） | `mam-timeline-editor` |
| — | `PUT /edit-timelines/:id`（アクションルート、§2.6） | Edit Timeline 保存（ドラッグ&ドロップ操作の反映）。`/api/v1` では提供せず、JSON API の GET が返す `save_url` へ PUT する（Lit は API クライアント経由で CSRF トークン付きで送る）。`branch_id` は M2 | `mam-timeline-editor` |
| GET | `/api/v1/health` | ヘルスチェック。`/healthz` と同一の応答（`ok`/`database`、DB 障害時は 503） | 外形監視（Lit は使わない） |
| GET | `/api/v1/openapi.json` | OpenAPI 3.1 スペック（Huma自動生成）。`/api/v1/openapi.yaml`・`/api/v1/openapi-3.0.json`・`/api/v1/openapi-3.0.yaml` は同内容の別形式、`/api/v1/schemas/:schema` は各型の JSON Schema | - |

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
# GET /api/v1/projects/:id/edit-timeline   （mam-timeline-editor。保存は PUT /edit-timelines/:id のアクションルート）
Response 200:
  Body:
    id: "et_01"                  # 表示している版。保存時に PUT のパスへ使う
    version_number: 5
    created_at: "2026-10-12T09:00:00Z"
    duration_seconds: 9.5
    readonly: false              # 編集できない段階（承認待ち・READY 等）では true
    save_url: "/edit-timelines/et_01"
    preview_url: "/edit-timelines/et_01/preview"
    transitions: ["cut", "fade", "fade_black", "dissolve"]
    limits: { min_clip_seconds: 0.5, max_transition_seconds: 2, max_caption_runes: 60 }
    branch_id: "br_01"           # Branch の版のときだけ。トランクでは省略
    clips:
      - { shot_id: "shot_003", take_id: "take_010", label: "Scene 1 · Shot 1", objective: "駅のホームを歩く",
          source_seconds: 6.0,   # Take ファイルの長さ（参考値。Out 点の上限判定用。長さが不明なら省略）
          video_url: "/takes/take_010/video", thumbnail_url: "/takes/take_010/thumbnail",
          in_point: 0.0, out_point: 4.2, start: 0.0, transition: { type: "cut", seconds: 0 } }
Errors:
  404: "Rough Cut がまだない"
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
    preserve: ["character", "acting", "background"]
    repair_strategy:            # PASS で戦略が無いときは省略
      action: "partial_regeneration"
      instructions:
        - "increase_product_reference_strength"
        - "preserve_logo_geometry"
    scores:                     # Judge が評価軸別のスコアを出さなかったときは省略
      product_accuracy: 88
      product_exposure: 72
    created_at: "2026-09-30T12:34:56Z"
Notes:
  metrics.face_identity_status: "measured" | "not_applicable"（顔参照を持つ Character が Shot にいない）| "unavailable"（顔モデル未配置・推論エラーで定性評価のみ）
  metrics.face_identity[]: 上記に加え evaluated（数値評価が成立したか。false は undetected_ratio が高い等で定性評価のみ）/ warn_threshold / fail_threshold / calibrated（参照画像でキャリブレーションした閾値か）
  issues[]: 上記に加え dimension（評価軸）/ character_id（face_identity_drift）/ description / lock_violation（LOCK 違反。true の Take は score に関係なく FAIL）/ brand_constraint_id（Brand / Safety Constraint の違反。`type=brand_constraint_violation`。`lock` の制約は lock_violation=true、`advisory` は severity low の警告）
  scores: Production Template の評価軸（`product_accuracy` / `hook` など）→ 0-100。Retry Policy の Minimum acceptance score の判定に使う。created_at: この Judge Result を記録した時刻（RFC 3339）
  time_range: "開始-終了s"（秒）。frames: [開始フレーム, 終了フレーム]（ソース動画のフレーム番号、両端を含む）
```

## 4. WebSocket

| Path | 概要 |
|---|---|
| `/ws/shots/:id` | 生成中Shotの進捗（キュー待ち/生成中/Judge中/完了/失敗）をプッシュ通知。メッセージは JSON `{"shot_id","action"?:"generate\|candidates\|reference_pack\|judge\|repair","stage":"idle\|queued\|generating\|judging\|repairing\|done\|failed","message"?,"takes_done"?,"takes_total"?}`。候補生成中も `action=candidates` で、`stage` はこの列挙のまま（`queued` / `generating` / `judging` / `done` / `failed`）である。接続直後に 1 通送る: GENERATING / JUDGING / REPAIRING の Shot は現在の `stage`（`generating` / `judging`＋`action=judge` / `repairing`＋`action=repair`）、それ以外は `stage=idle`（購読してから状態を読むので、その間に届いた `done` / `failed` は失われない）。同一オリジンの接続のみ受理し、存在しない Shot は 404。Judge 中は `action=judge` で `stage=queued\|judging\|done\|failed`（`stage` に `judging` を追加）。Repair 中は `action=repair` で `stage=queued\|repairing\|done\|failed`（`stage` に `repairing` を追加。Retry Policy の上限で止めたときは `done` とその理由の `message`） 生成中・Judge 中の Shot カードは `mam-video-player` が接続し、完了（done/failed/idle）の `progress-finished` でカードを再取得する。プレーヤーのないカード（Repair 中）は `shot.ts` が接続する。カード 1 枚につき接続は 1 本 |
| `/ws/projects/:id` | チャットの通知のみを配信する（Core State Machine 遷移・コスト更新・Editor→Reshoot ループの発火のための専用イベントは配信しない。これらの変化はチャットに投稿されたメッセージ・カードとして `message.created` / `card.updated` で届くか、画面が HTTP で取得する。コストは Cost Summary パネルが 15 秒間隔の HTMX ポーリングで更新する）。通知は本文を含まない JSON `{"type":"message.created","message_id":"<最新メッセージID>"}`（新着あり。クライアントは `GET /projects/:id/messages?after=` で取得）と `{"type":"card.updated","message_id":"<カードID>"}`（進捗カード等の更新。`GET /projects/:id/messages/:messageId` で取得）。同一オリジンの接続のみ受理する |

`lib/ws.ts` 経由で接続し、自動再接続（指数バックオフ、最大10回）を行う。接続してから 1 秒以内に閉じた接続は失敗として数える（開いてすぐ閉じるサーバーを 500ms 間隔で繰り返さず、10回で断念する）。断念した後、および再接続待ちの間は、ブラウザの `online`・`visibilitychange`（表示）または画面の「再接続」で直ちに再試行する。断念するとチャット・生成進捗（Shot カード・`mam-video-player`）は「接続が切れました」の通知（「再接続」「再読み込み」ボタン付き）を表示する。チャット画面は再接続後、新着メッセージに加えて、まだ変わりうるカード（未回答の承認・質問・エスカレーション、未完了ジョブの進行カード。サーバーが `data-card-pending` を付ける）を `GET /projects/:id/messages/:messageId` で取り直す（切断中の `card.updated` の取りこぼしを補う）（`spec/architecture/overview.md` 参照元 HALT 標準パターン）。
