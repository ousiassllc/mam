# プロジェクト概要

## 目的

AI Video Production Agent は、単一プロンプトで動画を出す「生成ツール」ではなく、企画・脚本・演出・撮影・品質判定・撮り直し・編集・音声・最終QCまでを AI が自律的に遂行する **AI Production Studio** を、ローカルで動作する Web アプリとして提供する。ユーザーは高度なプロンプトエンジニアリングではなく、「何を作りたいか」「どこを自分で決めたいか」を伝えるだけで、商品CM・SNS広告・ドラマ・アニメ・MV 等の映像を完成まで到達させることを目的とする。

## 背景

生成AI単体の性能向上だけでは「完成作品」に到達しない。単発生成物の寄せ集めではなく、キャラクター・商品・世界観・物語の一貫性（World State）を Shot 間で維持し、生成結果を AI 自身が視聴・判定し、不合格なら撮り直す、というプロフェッショナルな制作プロセスをソフトウェア化する必要がある。詳細な思想・ワークフローは製作初期に `spec/spec.md`（製品仕様ドラフト v1.0、全31章）としてまとめられており、本ドキュメント群はそれを実装可能な仕様セットへ構造化したものである。

## スコープ

### 含むもの

- **AUTO MODE**: Creative Brief から完成動画までの自律制作パイプライン（Producer → Writer → Director → Storyboard → Art Director → Prompt Planner → Generation → Judge → Repair → Editor → Audio → Final QC）
- **DIRECTOR MODE**: 自然言語による演出指示（Command Interpreter）と Scene / Shot / Take 階層の監督操作
- **Granular AUTO / MANUAL / LOCK**: 工程・パラメータ単位での制御粒度切り替え
- **Prompt Autopilot**: 自然言語指示から Canonical Shot Spec（モデル非依存）への変換と Model Adapter によるモデル別最適化
- **World State / Continuity Memory**: キャラクター・商品・場所・物語状態の Shot 間引き継ぎ
- **AI Video Judge / Repair・Retake Loop**: フレーム単位の合否判定と自動修復戦略
- **Take System / Continue / Branch**: 複数 Take 管理、続きの生成、別展開の分岐保存
- **AI Editor Loop**: 素材不足時に撮影工程へ戻る自己修正ループ、音声制作連携
- **Production Templates**: PRODUCT/CM・DRAMA・ANIME・SNS/SHORT VIDEO ごとの評価軸切り替え
- **Quality/Cost Policy**: FAST / BALANCED / MAX QUALITY と予算・リトライ上限設定
- **Safety / Brand Constraints**: ブランドガイドライン・禁止表現・権利メタデータの Prompt/Judge 適用
- 対象コンテンツ種別: 商品PV、TVCM/Web CM、SNS広告・縦型ショート、ドラマ、アニメーション、MV、YouTubeコンテンツ、ブランドムービー、シリーズ/エピソード型コンテンツ
- 実行基盤: HALT（HTMX + Atomic Design + Lit + Templ）構成のサーバー内蔵型フロントエンドを持つ、ローカル起動型 Web アプリ（Go 単一バイナリ + ブラウザ）。ネイティブデスクトップパッケージング（Wails 等）は将来拡張とする

### 含まないもの

- SaaS マルチテナント化・サーバー運用基盤（本製品は個人クリエイター/マーケター向けのシングルユーザー・デスクトップツールとして設計する）
- リアルタイム同時共同編集（マルチユーザー同時操作）。チーム利用は MVP3 の Team Collaboration まで対象外
- 画像/動画生成モデルの自前学習・ファインチューニング（外部モデルAPIの利用のみを前提とする）
- モバイルアプリ（iOS/Android）
- 特定ベンダーの生成モデルへの構造的ロックイン（Model Adapter 層でモデル差し替え可能に保つ）

## ステークホルダー / 想定ユーザー

| 区分 | 役割 | 備考 |
|------|------|------|
| エンドユーザー（個人クリエイター/マーケター） | 企画者・監督・クライアントとして AI 制作チームを監督し、映像を完成させる | 1デバイス・1ユーザー前提。プロジェクトファイルはローカル保存 |
| 開発者（本プロジェクトの実装者） | Go バックエンド（Orchestrator/各Agent/Model Adapter）とHALTフロントエンドの実装・保守 | サーバー運用チームは存在しない（ローカル起動型 Web アプリのため） |

## システム全体像

```mermaid
flowchart TB
    subgraph Local["ローカル環境（Go 単一バイナリ、Windows / macOS / Linux）"]
        Browser["既定ブラウザ<br/>（アプリ起動時に自動オープン）"]
        UI["HALT フロントエンド<br/>Templ SSR + HTMX + Lit Web Components"]
        Server["Go バックエンド<br/>Gin + Huma（レイヤード: domain/repository/service/web）"]
        Orchestrator["Orchestrator Agent<br/>状態機械 + goroutine ワーカー"]
        DB[("SQLite<br/>Project Data Model")]
        FS[("ローカルファイルシステム<br/>Assets / Keyframe / Video")]
    end
    ExtAPIs["外部 画像/動画生成モデルAPI群<br/>（Model Adapter 経由・ベンダー非依存）"]

    Browser <-->|HTMX フラグメント / JSON API（ループバック）| Server
    Server --> UI
    Server --> Orchestrator
    Orchestrator --> DB
    Orchestrator --> FS
    Orchestrator -->|Canonical Shot Spec → Model別プロンプト| ExtAPIs
    Server --> DB
    Server --> FS
```

詳細なアーキテクチャ（レイヤー構成、HALT の技術スタック、将来の Wails 拡張方針）は `spec/architecture/overview.md` を参照。

## ドキュメントマップ

このプロジェクトの仕様は以下のドキュメントで構成される。実装時はそれぞれ参照すること。

| ドキュメント | パス | 概要 |
|------------|------|------|
| 機能要件 | `spec/requirements/functional.md` | AUTO/DIRECTOR MODE、Prompt Autopilot、World State、Judge、Repair/Retake、Take/Continue/Branch、Editor、Audio 等のユースケースと機能一覧 |
| 非機能要件 | `spec/requirements/non-functional.md` | ローカル起動型 Web アプリとしての性能・可用性・データ整合性・セキュリティ・コスト制御要件 |
| アーキテクチャ設計 | `spec/architecture/overview.md` | HALT（Gin/Huma/Templ/HTMX/Lit）レイヤー構成、Agent Architecture、通信フロー、将来の Wails 拡張方針 |
| ER / データモデル | `spec/architecture/er.md` | Project Data Model（Scene/Shot/Take/World State 等）の ER 図とテーブル定義 |
| API 仕様 | `spec/api/endpoints.md` | ページルート/アクションルート（HTMX）と `/api/v1` JSON API（Huma/OpenAPI）のエンドポイント仕様 |
| コンポーネント設計 | `spec/components/overview.md` | Agent 群（Producer/Director/Editor 等）とHALT側コンポーネント（atoms/molecules/organisms/Lit）の責務分割 |

## マイルストーン / リリース計画

時期は未定。`spec/spec.md` §27 の MVP 区分をそのまま採用する。

- **M1 — Autonomous Shot Studio**: Creative Brief、AUTO/DIRECTOR 切替、Scene/Shot/Take、Keyframe Generation、Prompt Autopilot、1〜2 Video Model Adapter、Video Judge、Retake Loop、Basic World State、Simple Timeline Editing、Final Export。1本の短尺動画をAIが自律制作できることを検証する
- **M2 — Production Memory**: Character Lock、Product Lock、Location State、Continue、Branch、Reference Library、Partial Regeneration を追加
- **M3 — AI Production Studio**: Advanced Editor Agent、Editor→Reshoot Loop、Audio Director、Multi-model routing、Series/Episode Memory、Brand Templates、Team Collaboration、Production Analytics を追加

## 用語集

`spec/spec.md` の英語用語を規範用語として採用し、日本語定義を付与する。

| 用語 | 定義 |
|------|------|
| AUTO MODE | Creative Brief から完成動画まで AI が自律的に制作するモード |
| DIRECTOR MODE | ユーザーが自然言語で AI 制作チームを監督するモード |
| Scene / Shot / Take | Scene: 物語・場所・時間・目的を共有する単位。Shot: 一つの連続した撮影単位。Take: 同一 Shot Spec を基準に生成された複数候補 |
| Shot Spec | ユーザー指示から展開される、モデル非依存の構造化された撮影仕様（YAML形式で表現） |
| World State | キャラクター・商品・場所・カメラ・物語状態を保持し、Shot 間で継承される状態 |
| Model Adapter | Canonical Shot Spec を各画像/動画生成モデル固有のプロンプト/パラメータへ変換する層 |
| AI Video Judge | 生成動画を AI 自身が視聴し、フレーム/時間単位で Issue と Repair Strategy を返す評価機構 |
| Repair / Retake Loop | Judge の Issue 分類に基づき Prompt 修正・部分再生成・全撮り直し・別モデル利用等を選択する自己修正ループ |
| Continue | 完成 Shot/Scene から World State を引き継いで続きを生成する機能 |
| Branch | 任意の Scene/Shot/Frame を分岐点として保存し、別展開を生成する機能 |
| AUTO / MANUAL / LOCK | 各パラメータの制御モード。AUTO: AIが決定・変更可能、MANUAL: ユーザー指定、LOCK: 現在値固定でAI変更禁止 |
| Confirmation Level | AUTO MODE 内での人間確認頻度。FULL AUTO / KEY CHECK / CUSTOM CHECK の3段階 |
| Orchestrator Agent | 制作全体を状態機械として管理し、次に実行する Agent や人間確認要否を判断する中枢 Agent |
| Production Template | コンテンツ種別（PRODUCT/CM・DRAMA・ANIME・SNS/SHORT VIDEO）ごとの評価軸・優先度・Workflow定義 |
| HALT | HTMX + Atomic Design + Lit + Templ による、フロントエンドが API サーバー内で動くサーバー内蔵型アーキテクチャ |
| Wails | Go バックエンドとネイティブ WebView を組み合わせてデスクトップアプリを構築するフレームワーク。現段階はローカル Web サーバー + ブラウザ構成とし、Wails 採用は将来のネイティブパッケージング拡張として検討する（同一の `http.Handler` をそのまま AssetServer へ移植可能な設計を維持する） |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-28 | 新規作成 | `spec/spec.md`（製品仕様ドラフト v1.0）を構造化ドキュメントセットへ展開する初版 |
| 1.1 | 2026-09-28 | 実行基盤を Wails デスクトップアプリ前提からローカル起動型 Web アプリ（Go 単一バイナリ + ブラウザ）前提へ変更。Wails は将来拡張として位置付け | ユーザー指示により、Wails 統合は現段階のスコープ外とし将来拡張に切り出す |
