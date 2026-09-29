# コンポーネント設計

`spec/architecture/overview.md` のレイヤー構成・Agent Architecture を実装単位のコンポーネントへ分割する。

## 1. Agent コンポーネント（`internal/agent`）

| コンポーネント | 責務 | 入力 | 出力 | 依存 |
|--------------|------|------|------|------|
| **Orchestrator Agent** | Core State Machine（DRAFT〜EXPORTED）の管理。次に実行するAgentの決定、人間確認要否判定、Retake/部分修正/別モデル利用の判断、コスト上限監視、Editor→Reshoot差し戻し判定 | Project State、Confirmation Level、Quality/Cost Policy | 次アクション指示、Project State更新 | 全Agent、`repository` |
| **Producer Agent** | Creative Brief から Concept/Structure を展開 | Creative Brief | Concept/Structure | `domain.Brief` |
| **Writer Agent** | Concept/Structure から Script を生成。M2 では Notes（制作メモ）を推論の文脈として参照する | Concept/Structure、Notes（M2） | Script | Producer出力 |
| **Director Agent** | Script から Scene/Shot Planning を実施。DIRECTOR MODEでのCommand Interpreter結果も受理。M2 では Notes を推論の文脈として参照する | Script、DIRECTOR指示、Notes（M2） | Scene/Shot構成 | Writer出力、Command Interpreter |
| **Storyboard Agent** | Scene/Shotから絵コンテを生成 | Scene/Shot Planning | Storyboard | Director出力 |
| **Art Director Agent** | Reference Pack（Character/Product/Location/Style/Lighting Reference, Keyframe）を構築 | Storyboard、Assets | Reference Pack | Storyboard出力 |
| **Prompt Planner** | Canonical Shot Spec（モデル非依存YAML）を生成。Prompt Candidate Generation（Stability First/Acting First/Dynamic Camera）も担当。M2 では Notes を推論の文脈として参照し、参照したメモのバージョンを `prompt_version.note_refs` に記録する（ピン留めは常時、他は関連度順にコンテキスト予算内で選別） | Reference Pack、World State、Brand Constraints、Notes（M2）、ユーザー自然言語指示 | Canonical Shot Spec | Art Director出力、World State |
| **Model Adapter** | Canonical Shot Spec を各画像/動画生成モデル固有のプロンプト/パラメータへ変換し、外部API呼び出しを実行。採用プロバイダー・ルーティングは `spec/architecture/model-adapters.md` 参照（画像: Imagen 4/Nano Banana/FLUX.2/GPT Image 2、動画: Kling 3.0/Seedance 2.0/Runway Gen-4.5） | Canonical Shot Spec、Quality/Cost Policy | Take（複数候補） | Prompt Planner出力、外部モデルAPI |
| **AI Video Judge** | 生成Takeを解析し、Prompt Adherence/Character/Product/Face/Hands/Pose/Acting/Motion/Camera/Lighting/Continuity等を評価。顔同一性は Face Identity Similarity（参照顔との埋め込み類似度）をローカルで数値化して併用（`spec/spec.md` §12.4）。フレーム単位Issueと Repair Strategy を出力 | Take、Shot Spec、World State、Brand Constraints | Judge Result（pass/fail, issues, metrics, repair_strategy） | Model Adapter出力。M1の実装は `internal/agent/judge`（定性評価は Reasoning LLM、顔の数値化は `internal/facesim`）。判定は決定的ルールで、LOCK 違反は score に関係なく FAIL（`spec/architecture/model-adapters.md` §10） |
| **Repair Agent** | Judge Resultから Prompt Repair/Partial Regeneration/Full Retake/Different Model/Reference Reinforcement/Edit-Level Repair を選択・実行。戦略選択の前段に Decision Classifier（Jev、`spec/architecture/model-adapters.md`§4.5）を任意で置き、confidence が低い場合のみ LLM が判断する。Retry Policy（Max retries/cost/score/Escalation）を適用 | Judge Result、Retry Policy | 次アクション（再生成指示 or Escalation） | Judge出力 |
| **Editor Agent** | Selected TakesからRough Cut生成、Shot Selection/Cut Point/Timing/Pace/Transition/Reaction Shot/Insert Shot/Product Exposure/Narrative Clarity/Hook/Retention/CTA/Captionを編集。Editor Judgeで素材不足を検知しDirectorへ差し戻す | Selected Takes | Edit Timeline、素材不足時はAdditional Shot要求 | Take選択結果 |
| **Audio Director** | Dialogue/Voice/Narration/Lip Sync/BGM/SE/Foley/Ambience/Mixing/Loudness最適化。Scene Emotionと編集テンポを参照。TTS/Music/SFXはElevenLabs、Lip SyncはSync Labs、Mixing/LoudnessはローカルDSP処理（詳細は`spec/architecture/model-adapters.md`§5） | Edit Timeline、Scene情報 | Audio Timeline | Editor出力 |
| **Final QC** | 最終的な品質・Brand/Safety Constraint適合をチェック | Edit Timeline、Audio Timeline、Brand Constraints | PASS/FAIL（FAIL時は該当工程へ差し戻し） | Editor/Audio出力 |
| **Command Interpreter** | 自然言語のDIRECTOR指示を構造化操作（LOCK設定/パラメータ更新/再生成/Branch作成等）へ変換。対象 Scene/Shot/Take はカードへの返信・発言中の番号・直前の文脈から解決し、解決できなければ聞き返しの質問を返す（M1 の操作は `set_lock`/`regenerate`/`set_mode`、Branch 作成は M2 まで `unsupported`）。再生成は変更する部分と保持する部分（Character/Acting/Environment 等）を分けて返す。M2 では「覚えておいて」等の発言を Project スコープの Note 作成（`source=chat`）へ変換する | 自然言語テキスト | 構造化コマンド | Director Agentから呼び出される |
| **State Extractor** | Shot完了後の最終状態を抽出し、World Stateとの差分（State Diff）を生成。LOCK項目は除外。M2 では Note の新規/更新バージョンから World State に構造化できる項目を検出し、非ブロッキングの昇格提案（`note_promotion`、LOCK項目は除外）を生成する | 完了Shot、現在のWorld State、Note Version（M2） | State Diff、新World State Version、昇格提案（M2） | Orchestratorから呼び出される |

各Agentの自然言語推論は Reasoning LLM Adapter（`spec/architecture/overview.md` §2）経由でLLM APIを呼び出す。Agentごとのプライマリ/代替プロバイダー（Claude Opus系/GPT-5系/Gemini 3.1 Pro）の割当は `spec/architecture/model-adapters.md` §2 を参照。

## 2. 依存関係図

```mermaid
flowchart TB
    Handler[web/handler] --> Service[service]
    Service --> Agent[agent]
    Service --> Repo[repository]
    Agent --> Repo
    Agent --> Domain[domain]
    Repo --> Domain
    Handler --> Templ[Templ: atoms/molecules/organisms/pages]
    Templ -.埋め込み.-> Lit[Lit Web Components]
    Lit -->|JSON API のみ| APIRoute["/api/v1 (Huma)"]
    APIRoute --> Service
```

依存の方向は一方向（`spec/architecture/overview.md` §5のレイヤー依存ルールに従う）。Lit は Templ に埋め込まれるが、Templ から Lit への内部状態アクセスは行わない（HTML属性注入のみ）。

## 3. HALT側コンポーネント（Templ, Atomic Design）

| 区分 | コンポーネント例 | 責務 |
|------|-----------------|------|
| atoms | `button`, `input`, `textarea`, `select`, `avatar`, `badge`, `spinner`, `toast`, `lock_toggle`（AUTO/MANUAL/LOCK 3値スイッチ） | 最小のUI要素。単一責務 |
| molecules | `brief_form`（補助ビューの Brief 確認・編集用）, `shot_card`, `take_row`, `world_state_field`, `scene_tab`, `production_template_select`, `quality_policy_select`, `mode_control`（AUTO ⇄ DIRECTOR 切替）, `judge_issue_badge`, `message_bubble`（ユーザー/AI の発言）, `question_card`, `approval_card`（M2 では Notes の昇格提案＝差分の一覧と「LOCK 中のため未反映」表示も担う）, `keyframe_card`, `take_card`（`mam-video-player` 埋め込み）, `progress_card`, `escalation_card`, `attachment_chip`, `note_card`（M2、Notes の1件表示。タイトル・カテゴリ・ピン留め・最新バージョン） | atomsを組み合わせた複合要素 |
| organisms | `chat_thread`（メッセージ履歴の表示と新着追記）, `chat_composer`（入力欄・添付・MODE/Quality コントロール・返信対象の表示）, `main_workspace_header`, `scene_shot_navigator`, `inspector_panel`, `take_comparison_table`, `director_command_input`（`chat_composer` に統合し補助ビュー単体では使わない）, `brand_constraint_list`, `cost_summary_panel`, `version_history_panel`（M2 では Note のバージョン復元にも使う）, `note_list`（M2、`note_card` の一覧と作成・編集・アーカイブ） | molecules/atomsを組み合わせた機能単位 |
| pages | `project_list_page`, `chat_workspace_page`（標準画面、`chat_thread` + `chat_composer`）, `brief_page`（補助ビュー）, `shot_inspector_page`, `take_comparison_page`（`mam-take-comparator`埋め込み）, `edit_page`（`mam-timeline-editor`埋め込み）, `branch_page`（`mam-branch-tree`埋め込み）, `world_state_page`, `notes_page`（M2、Project / Series 共用）, `export_page`, `settings_page` | organismsを組み合わせたページ全体 |
| layout | `app_layout` | 共通レイアウト（ナビゲーション。Mode切替・Qualityセレクタは `chat_composer` に置く） |

## 4. Lit Web Components インターフェース

`spec/architecture/overview.md` §8 の4コンポーネントについて、HATEOAS原則（サーバーがURL・権限をHTML属性で注入、クライアントはURLをハードコードしない）に従ったインターフェースを定義する。

### 4.1 `<mam-video-player>`

```typescript
@customElement('mam-video-player')
export class MamVideoPlayer extends LitElement {
  @property({ type: String, attribute: 'api-url' }) apiUrl = '';       // 例: /api/v1/takes/:id/judge-result
  @property({ type: String, attribute: 'video-src' }) videoSrc = '';   // サーバーが解決したローカルファイルURL
  @property({ type: String, attribute: 'ws-url' }) wsUrl = '';         // 生成中は /ws/shots/:id
  @state() private issues: JudgeIssue[] = [];                          // フレームマーカー描画用

  // イベント: 'seek-requested'（他コンポーネントへ再生位置を伝播）
}
```

責務: フレーム精度シーク、Judge Issueの時間範囲/フレーム範囲をタイムバー上にマーカー表示、生成中はWebSocket進捗をプレースホルダー表示に反映。

### 4.2 `<mam-timeline-editor>`

```typescript
@customElement('mam-timeline-editor')
export class MamTimelineEditor extends LitElement {
  @property({ type: String, attribute: 'api-url' }) apiUrl = '';   // GET/PUT /api/v1/projects/:id/edit-timeline
  @property({ type: Boolean }) readonly = false;                   // 権限に応じてサーバーが注入
  @state() private timeline: EditTimelineShot[] = [];

  // イベント: 'timeline-updated'（HTMX側がhx-triggerで受信しRough Cutプレビューを更新）
}
```

責務: Shotのドラッグ&ドロップ並び替え、Cut Point/Transition編集、保存はAPIルート経由。保存完了時に`timeline-updated` CustomEventを発火し、HTMX側で関連プレビューをサーバー経由更新する（HALTのLit→HTMX間接連携パターン）。

### 4.3 `<mam-take-comparator>`

```typescript
@customElement('mam-take-comparator')
export class MamTakeComparator extends LitElement {
  @property({ type: String, attribute: 'api-url' }) apiUrl = '';  // GET /api/v1/shots/:id/takes
  @state() private takes: TakeScore[] = [];
  @state() private selection: { takeId: string; start: number; end: number }[] = [];

  // イベント: 'partial-take-confirmed'（区間選択確定 → HTMXが /shots/:id/takes/partial へPOST）
}
```

責務: 複数Takeのスコア（Product/Acting/Camera/Continuity）比較表示、タイムライン上でのドラッグによる区間指定（Partial Take Use）。

### 4.4 `<mam-branch-tree>`

```typescript
@customElement('mam-branch-tree')
export class MamBranchTree extends LitElement {
  @property({ type: String, attribute: 'api-url' }) apiUrl = '';  // GET /api/v1/projects/:id/branches
  @state() private tree: BranchNode[] = [];

  // イベント: 'branch-selected'（HTMXが /branches/:id へ遷移。branch_page内には選択中Branchの World State（`branch_id`スコープ）へ遷移するリンク `/projects/:id/world-state?branch_id=...` も併設）
}
```

責務: Scene/Shot配下のBranch階層をCanvas/SVGで可視化し、選択・切替操作を提供。

## 5. 共通ライブラリ（`static/src/components/lib`）

| ファイル | 責務 |
|---------|------|
| `api.ts` | CSRFトークン自動付与のHTTPクライアント（get/post/put/patch/del/uploadFile） |
| `ws.ts` | WebSocketラッパー（自動再接続、JSONパース） |
| `logger.ts` | ロギングユーティリティ |
| `video.ts` | フレーム精度シーク・タイムコード変換（`mam-video-player`用） |
| `timeline-model.ts` | タイムライン共通データモデル（`mam-timeline-editor` / `mam-take-comparator`で共有） |

すべてのLitコンポーネントはこれらのlibを経由してAPI/WebSocketにアクセスし、直接`fetch()`や`new WebSocket()`を呼ばない（`spec/architecture/overview.md`参照元HALT標準パターン）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-28 | 新規作成 | Agent Architecture（`spec/spec.md` §20）とHALTコンポーネント構成をコンポーネント設計として具体化 |
| 1.1 | 2026-09-28 | Prompt Planner・AI Video Judge の入力列に Brand Constraints を追加 | non-functional.md §5・F-23/UC-9の「Prompt生成・AI Video Judge・Final QCの全段階でBrand/Safety Constraintを適用する」との対応が取れていなかったため（整合性レビュー指摘） |
| 1.2 | 2026-09-28 | `workspace_page` に `mam-video-player` 埋め込みの注記を追加 | api/endpoints.md（1周目改訂1.2）で明記済みの埋め込みが、pagesテーブル側では他3ページと異なり未記載だったため（整合性レビュー指摘） |
| 1.3 | 2026-09-28 | Editor Agent の責務説明に「Product Exposure」「Narrative Clarity」を追加 | F-18のAI Editor機能範囲と表現が一致していなかったため（整合性レビュー指摘） |
| 1.4 | 2026-09-28 | organisms一覧に `version_history_panel`（Shot/World State/Branchのバージョン一覧表示・復元ボタン）を追加 | F-28・api/endpoints.mdの復元エンドポイント群（Shot/WorldState/Branch）に対応するUI導線コンポーネントが定義されていなかったため（整合性レビュー指摘） |
| 1.5 | 2026-09-28 | pagesテーブルに `world_state_page` を追加 | api/endpoints.mdの`GET /projects/:id/world-state`に対応するページがpagesテーブルに存在しなかったため（整合性レビュー指摘） |
| 1.6 | 2026-09-28 | `<mam-branch-tree>` の `branch-selected` イベント注記に、選択中Branchの World State（`branch_id`スコープ）へ遷移するリンクの併設を追記 | api/endpoints.md 1.11/1.12でCharacter/Product/Location・World State取得エンドポイントに`branch_id`クエリが追加されたのに対し、Branch選択後にそのBranchのWorld Stateへ遷移するUI導線が定義されていなかったため（整合性レビュー指摘） |
| 1.7 | 2026-09-29 | Model Adapter・Audio Director行に採用プロバイダーを明記。Agentテーブル直後にReasoning LLM Adapterの参照割当に関する注記を追加 | ユーザーとの協議によりエージェント推論LLM・画像/動画生成/音声プロバイダーを具体的に選定したため（`spec/architecture/model-adapters.md` 新設に対応） |
| 1.8 | 2026-09-29 | AI Video Judge に Face Identity Similarity、Repair Agent に Decision Classifier（Jev）を追記 | 顔同一性の数値化と、Repair 戦略選択の低コスト化（低 confidence は LLM へ回す）を導入するため |
| 1.9 | 2026-09-29 | チャット中心UIに合わせ、molecules に各種カード、organisms に `chat_thread`/`chat_composer`、pages に `chat_workspace_page` を追加。`workspace_page` を置換。Mode切替・Qualityセレクタを `app_layout` から `chat_composer` へ移動 | ユーザー要望「基本的にはただのチャット画面にしたい」（`spec/spec.md` §23） |
| 1.10 | 2026-09-29 | Writer/Director/Prompt Planner の入力に Notes を追加。Command Interpreter に Note 作成、State Extractor に昇格提案を追記。`note_card`/`note_list`/`notes_page` を追加し、`approval_card`・`version_history_panel` の用途を拡張 | F-31（Notes, M2）を実現する Agent の入出力と UI コンポーネントが定義されていなかったため |
| 1.11 | 2026-09-29 | Orchestrator の M1 範囲（DRAFT → PLANNING → VISUAL_DESIGN、Producer → Writer →（脚本の確認ポイント）→ Director → Storyboard）を `plan` ジョブとして実装。Brief が揃った AUTO Project でのみ起動し、脚本の Approval が未応答の間は次工程へ進まない。修正依頼は Producer/Writer に前版とコメントを渡して新しい `script_version` を作る。失敗はチャットに通知し、次のユーザー操作で完了済みの工程を飛ばして再開する | M1「Reasoning LLM Adapter と AUTO パイプライン」（Issue #6）の実装で確定した動作を仕様へ反映するため |
| 1.12 | 2026-09-29 | AI Video Judge の M1 実装（`internal/agent/judge`・`internal/facesim`）と判定ルールへの参照を追記 | M1「AI Video Judge（Face Identity Similarity 含む）」（Issue #9）で実装先と判定方式を明示するため |
| 1.13 | 2026-09-29 | Command Interpreter の M1 範囲（操作の種類・対象解決・保持する部分の扱い）を追記。`lock_toggle` の 3 値スイッチと `mode_control`（`chat_composer` 内の AUTO/DIRECTOR 切替）、`shot_inspector` を実装 | M1「AUTO/MANUAL/LOCK と DIRECTOR MODE」（Issue #12）で確定した動作を仕様へ反映するため |
