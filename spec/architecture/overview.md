# アーキテクチャ設計

## 1. 設計原則

`spec/spec.md` §29 の技術設計原則、および HALT（`skill://halt`）のサーバーファースト原則をそのまま採用する。

- **Model-Agnostic Core**: Project State / Shot Spec / Judge Result を特定生成モデルの Prompt 形式に依存させない
- **Structured State Before Prompt**: Prompt 文字列を Source of Truth にしない。Source of Truth は Creative Intent / World State / Shot Spec / Constraints / References であり、Prompt はそこから生成される一時的な実行形式
- **Every Generation Is Traceable**: 各 Take について Source Shot Spec・World State version・Prompt version・Model・Parameters・References・Output・Judge result・Repair history を保存する
- **Reversible Decisions**: Shot/Take/Branch/State の主要変更をバージョン化し復元可能にする
- **バックエンドファースト（HALT）**: フロントエンドは API サーバーの中で動く。SPA を作らず、サーバーが UI 制御を握る
- **HATEOAS（HALT）**: サーバーは現在の状態・権限に基づき利用可能なアクションのみを HTML に含める。AUTO/MANUAL/LOCK の状態や Confirmation Level に応じたボタン・フォームの出し分けもこの原則に従う

## 2. 技術スタック

| レイヤー | 技術 | 役割 |
|---------|------|------|
| デスクトップシェル | Wails v3 | Go バックエンドとネイティブ WebView（WebView2/WKWebView/WebKitGTK）を単一バイナリに統合 |
| サーバーフレームワーク | Go + Gin | ルーティング + SSR。Wails の AssetServer に `http.Handler` としてそのままマウントする |
| API フレームワーク | Huma | OpenAPI 3.1 自動生成、`/api/v1` 配下の JSON API の入出力バリデーション |
| テンプレートエンジン | Templ | 型安全な Go HTML テンプレート（atoms/molecules/organisms/pages） |
| インタラクション | HTMX | サーバー駆動の DOM 更新 |
| リッチUI | Lit (Web Components) | Video Player / Timeline Editor / Take Comparator / Branch Tree のみ |
| スタイリング | Tailwind CSS | ユーティリティファースト CSS |
| ビルド | esbuild | TypeScript バンドル |
| 永続化 | SQLite (WAL mode) | Project Data Model の永続化。単一ユーザー・単一バイナリ運用に最適 |
| アセットストレージ | ローカルファイルシステム | Keyframe/Reference/生成動画・画像の保存 |
| バックグラウンド処理 | Go goroutine + 内部ジョブキュー | 生成・Judge・Repair 等の非同期実行、進捗は WebSocket で通知 |
| 外部生成モデル連携 | Model Adapter（アダプタパターン） | 画像/動画生成モデル API をベンダー非依存に抽象化。特定モデルへの構造的依存を作らない |

## 3. 全体構成図

```mermaid
flowchart TB
    subgraph App["Wails デスクトップアプリ（単一バイナリ）"]
        WebView["ネイティブ WebView<br/>(WebView2 / WKWebView / WebKitGTK)"]
        subgraph Backend["Go バックエンド"]
            Router["router（ページ/アクション/APIルート）"]
            Handler["web/handler"]
            Service["service（Agentロジック含む）"]
            Repo["repository"]
            Domain["domain"]
            Orchestrator["Orchestrator Agent<br/>状態機械 + goroutineワーカー"]
        end
        DB[("SQLite (WAL)")]
        FS[("ローカルFS<br/>Assets/Keyframe/Video")]
    end
    ExtAPI["外部 画像/動画生成モデルAPI群"]

    WebView <-->|"HTMXフラグメント / JSON"| Router
    Router --> Handler --> Service --> Repo --> Domain
    Service --> Orchestrator
    Orchestrator --> Repo
    Orchestrator --> FS
    Orchestrator -->|Model Adapter経由| ExtAPI
    Repo --> DB
```

## 4. Agent Architecture

`spec/spec.md` §20 の Agent 構成を `internal/agent` パッケージ群として実装する。Orchestrator は Core State Machine（§22: DRAFT→PLANNING→VISUAL_DESIGN→(AWAITING_APPROVAL)→GENERATING→JUDGING→(REPAIRING)→EDITING→EDIT_JUDGING→AUDIO→FINAL_QC→READY→EXPORTED）を管理する状態機械として実装する。

```mermaid
flowchart TB
    Orchestrator[Orchestrator Agent] --> Producer[Producer Agent]
    Orchestrator --> Director[Director Agent]
    Orchestrator --> Editor[Editor Agent]
    Producer --> Writer[Writer Agent]
    Director --> Storyboard[Storyboard Agent]
    Storyboard --> ArtDirector[Art Director Agent]
    ArtDirector --> PromptPlanner[Prompt Planner]
    PromptPlanner --> ModelAdapter[Model Adapter]
    ModelAdapter --> Models[Image/Video Generation Models]
    Models --> Judge[AI Video Judge]
    Judge --> Repair[Repair Agent]
    Repair --> ModelAdapter
    Editor --> AudioDirector[Audio Director]
    Editor --> FinalQC[Final QC]
```

Orchestrator の責務（§20.1）: 次に実行する Agent の決定、人間確認要否判定、生成続行/Retake/部分修正/別モデル利用の判断、コスト上限監視、編集での修正可否判定、撮影工程への差し戻し判定、Project State 更新。

## 5. ディレクトリ構成

```
.
├── main.go                 # Wails エントリーポイント（AssetServer に Gin Handler をマウント）
├── wails.json
├── backend/
│   ├── cmd/                # サーバー単体起動用（開発/テスト時）
│   └── internal/
│       ├── domain/         # Project/Scene/Shot/Take/WorldState等のドメインモデル
│       ├── repository/     # SQLite アクセス層（domainのみに依存）
│       ├── agent/          # Producer/Writer/Director/Storyboard/ArtDirector/
│       │                   # PromptPlanner/ModelAdapter/VideoJudge/RepairAgent/
│       │                   # EditorAgent/AudioDirector/FinalQC/Orchestrator
│       ├── service/        # アプリケーションサービス層（agentを利用しユースケースを実装）
│       ├── router/         # ページ/アクション/APIルート定義
│       └── web/
│           ├── handler/    # brief.go, scene.go, shot.go, take.go, director.go,
│           │                # branch.go, export.go, settings.go ...
│           ├── middleware/  # CSRF、ロギング等
│           ├── atoms/       # button, input, badge, spinner, toast, slider(AUTO/MANUAL/LOCK) 等
│           ├── molecules/   # shot_card, take_row, world_state_field, brief_form 等
│           ├── organisms/   # main_workspace, inspector, scene_tabs, take_comparison_table 等
│           ├── pages/       # project_page, brief_page, export_page 等
│           └── layout/      # レイアウトラッパー
└── static/
    └── src/
        ├── components/
        │   ├── video-player/         # mam-video-player
        │   ├── timeline-editor/      # mam-timeline-editor
        │   ├── take-comparator/      # mam-take-comparator
        │   ├── branch-tree/          # mam-branch-tree
        │   └── lib/
        │       ├── api.ts
        │       ├── ws.ts
        │       ├── logger.ts
        │       ├── video.ts          # フレーム精度シーク・タイムコード変換
        │       └── timeline-model.ts # タイムライン共通データモデル
        ├── css/
        └── dist/
```

### レイヤー依存ルール

```
handler → service → agent → repository → domain
                        ↓
   Templ テンプレート（atoms/molecules/organisms/pages）
```

- `domain/`: 他レイヤーに依存しない
- `repository/`: `domain/` のみに依存
- `agent/`: `domain/`, `repository/`, Model Adapter経由の外部API呼び出しに依存
- `service/`: `domain/`, `repository/`, `agent/` に依存。ユースケース単位のオーケストレーション
- `web/handler/`: `service/`, `domain/` に依存。`repository/`・`agent/` を直接使わない
- `router/`: `handler/` を参照してルートを定義

## 6. Wails 統合方式

Wails v3 の AssetServer に Gin の `http.Handler` をそのまま渡すことで、HALT の SSR + HTMX 構成を変更せずデスクトップアプリ内で動作させる。

```go
// main.go（概念コード）
ginEngine := router.New(services) // Gin *gin.Engine（HALTのSSR+HTMXルート+Huma APIを登録済み）

app := application.New(application.Options{
    Assets: application.AssetOptions{
        Handler: ginEngine, // http.Handler として直接マウント
    },
})
app.NewWebviewWindow()
app.Run()
```

- HTMX のフラグメント swap・`hx-boost`・WebSocket は通常の HTTP リクエストとして WebView → 埋め込み Gin サーバーへループバックする（実ネットワークを経由しないローカル配信）
- 開発時は `templ generate --watch` と `esbuild --watch`（CSS/JS）を並行起動し、Wails の Dev モードでホットリロードする
- 本番ビルドでは `static/dist` を `go:embed` し、単一バイナリに同梱する

## 7. HTMX ルーティングパターン

HALT の3パターン（ページルート/アクションルート/APIルート）をそのまま適用する。

| パターン | 例 | HX-Request分岐 | 返却 | 登録先 |
|---------|-----|----------------|------|--------|
| ページルート | `GET /projects/:id`, `GET /projects/:id/scenes/:sceneId` | する | フルページ or フラグメント | Gin |
| アクションルート | `POST /shots/:id/generate`, `POST /takes/:id/select`, `PUT /shots/:id/locks` | しない | フラグメントのみ | Gin |
| APIルート | `GET /api/v1/projects/:id`, `POST /api/v1/shots/:id/generate` | しない | JSON（Lit Web Components向け） | Huma |

Lit Web Components（Video Player/Timeline Editor/Take Comparator/Branch Tree）は `/api/v1` の JSON API のみを利用し、HTMX リクエストをトリガーしない（HALTの境界ルールに従う）。生成進捗は WebSocket（`/ws/shots/:id`）で配信する。

## 8. Lit Web Components 一覧

アプリ名プレフィックス: `mam-`。HALT の使用基準（リアルタイム編集/複雑なD&D/Canvas描画/WebSocket同期）に該当する4コンポーネントのみ実装する。それ以外（Brief フォーム、Inspector、AUTO/MANUAL/LOCKトグル、Branchの一覧表示等）は HTMX + Templ で実装する。

| コンポーネント | 用途 | 該当基準 |
|--------------|------|---------|
| `<mam-video-player>` | Take/Rough Cutのフレーム精度プレビュー、Judge Issueのフレームマーカー重畳表示 | Canvas描画、WebSocketでの生成進捗同期 |
| `<mam-timeline-editor>` | AI Editorのカット点・尺・トランジション編集 | 複雑なドラッグ＆ドロップ |
| `<mam-take-comparator>` | 複数Takeのスコア比較、区間指定によるPartial Take合成 | 複雑なドラッグ＆ドロップ（区間指定） |
| `<mam-branch-tree>` | Scene/Shot配下のBranch階層可視化・切替 | Canvas/SVG描画 |

## 9. ビルド / 開発ワークフロー

```makefile
.PHONY: dev
dev:
	@concurrently \
		"wails dev" \
		"templ generate --watch" \
		"npm run dev --prefix static"
```

- `.templ` 編集 → `templ generate --watch` が `_templ.go` を再生成 → `wails dev` が Go 変更を検知しサーバー再起動
- `.ts` 編集 → esbuild がバンドル → WebView リロードで反映
- `.css` 編集 → Tailwind がビルド → WebView リロードで反映
- 本番ビルド: `wails build` が `templ generate` → `esbuild`（minify） → `tailwind`（minify） → Go バイナリ埋め込みを一括実行する

## 10. テスト戦略

HALT のレイヤー別テスト方針を踏襲する。

| レイヤー | 手法 | 検証内容 |
|---------|------|---------|
| Go ハンドラ | `httptest` + HTMLアサーション | 正しいフラグメント/フルページ、ステータスコード |
| Templ テンプレート | `Render()` → HTML文字列アサーション | コンポーネント出力 |
| Agent（Producer/Judge/RepairAgent等） | Go ユニットテスト（外部APIはモック） | Shot Spec変換、Judge出力パース、Repair戦略選択ロジック |
| Lit コンポーネント | `@open-wc/testing` + `@web/test-runner` | 単体の振る舞い |
| E2E | Playwright（Ginサーバーを直接ブラウザから起動して検証） | HTMXインタラクション込みのユーザーフロー。パッケージ済みWailsバイナリは主要フローの手動スモークテストで補完する |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-28 | 新規作成 | HALT（HTMX+Atomic+Lit+Templ）+ Wails によるデスクトップアプリ構成を定義 |
