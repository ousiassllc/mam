# 画面仕様: トップ画面（プロジェクト一覧）

> **#915 の追記（2026-11-02）**: 全画面に左メニューが付き、戻るリンク（`BackLink`）はメニューに置き換わった。画面の枠（余白・h1・「?」）は `molecules/pagehead`。以下に「戻るリンク」とあるのは、このメニューに読み替える。詳細は [side-menu.md](side-menu.md)。

> **状態: 実装済み（Issue #978。2026-11-03）。** #846 でトップ画面に置いたツールのカード（7 枚・3 グループ）は、左メニュー（[side-menu.md](side-menu.md)）と同じ行き先なので外した。トップ画面は「プロジェクトの新規作成と一覧」だけ。カードの状態の読み込み（§5）のうち、設定画面のカードが使うものだけが残る。**Windows 実機の同梱エンジンの 6 状態の表示は未検証**。

[overview.md](overview.md) §3 の `projects_page`（`GET /`）は、プロジェクトを新しく作る・読み込む・開くための画面。ほかの 7 画面（生成テスト・モデルマネージャー・ワークフロー・ノード・レビュー・制作指標・設定）へは、全画面にある左メニューから行く。

## 1. 経緯

| 版 | 内容 |
|----|------|
| #846（2026-10-25） | 文字のリンク 5 つと「受け取ったレビューパッケージを開く」を、アイコン・説明・状態つきのカード 7 枚（作る系・準備系・その他）に置き換えた。状態は枠を先に出し `hx-trigger="load"` で後から読んだ |
| #915（2026-11-02） | 全画面に左メニューが付き、7 画面すべてがメニューに入った。トップのカードと行き先が重なった |
| #978（2026-11-03） | トップ画面のカード（`pages.ToolSection`・`toolGroups`）を外した |

## 2. 画面の構成（上から）

| 順 | 区画 | 備考 |
|----|------|------|
| 1 | `<h1>` mam・「Autonomous Shot Studio」 | |
| 2 | 新しいプロジェクトのフォーム（`organisms.NewProjectForm`） | |
| 3 | バンドルの読み込み（`organisms.ProjectImportForm`） | |
| 4 | `<h2>` プロジェクト・`organisms.ProjectList` | プロジェクトが 0 件のときは一覧の空の文言 |

- 見出しは h1（mam）→ h2（プロジェクト）の順（#781。段を飛ばさない）。
- 幅: `<main class="mx-auto max-w-5xl p-8">`。区画 1〜4 は**すべて同じ幅**で、内側の `max-w-3xl` の枠は持たない（2026-10-25 オーナー決定）。1024 px のウィンドウでも横にスクロールしない。
- ツールのカードは置かない。`data-testid="tool-card"` も、`hx-trigger="load"` で状態を読む枠も、このページには無い（`pages/projects_page_test.go`）。

## 3. ツールのカードを外したこと（#978）

7 枚のカードの行き先は、すべて左メニューに入っている（`organisms/sidenav/menu.go`）。

| 旧カード | 左メニューの項目 | 旧カードの状態 | 外したあとの扱い |
|----------|------------------|----------------|------------------|
| 生成テスト | 生成テスト | なし | — |
| ノード | ノード | ComfyUI の状態 | 不要。ノード画面（`NodeData`）が表示する ComfyUI と、表示できないときの理由・モデルマネージャーへの案内（`ModelManagerURL`）を出す。状態の取得は削除 |
| ワークフロー | ワークフロー | 保存数 | 不要。ワークフロー画面が一覧を出す。状態の取得は削除 |
| モデルマネージャー | モデルマネージャー | ComfyUI の状態 | 画面が同梱エンジンの状態を出す。設定画面のモデルマネージャーのカードには同じ状態（`GET /tools/model-manager/status`）が残る（§5） |
| 設定 | 設定 | API キーの件数・ChatGPT のログイン | 設定画面の概要の「API キーとログイン」のカードが同じ状態を出す（サーバーで組み立てる。`toolstatus.Services.Settings`）。トップのカードの `hx-get` だけを削除 |
| 制作指標 | 制作指標 | なし | — |
| レビューパッケージ | レビュー | 受け取った件数 | 不要。レビュー画面が一覧を出す。状態の取得は削除 |

- 左メニューにバッジは足さない。上の表のとおり、状態はどれも行き先の画面か設定画面で見え、メニューに常に出す理由が無い（メニューは行き先の一覧で、全画面の毎回の表示で状態を読むと遅くなる）。
- 削除したもの: `pages.ToolSection`・`toolGroups`・`GET /tools/{nodes,workflows,settings,reviews}/status`（ほかの `:tool` は 404）・`paths.ToolNodes`/`ToolWorkflows`/`ToolSettings`/`ToolReviews`・`toolstatus` の `NodesLines`/`CountLine` と、ワークフロー・レビューの読み取り。`molecules.ToolCard` は設定画面の概要とモデルマネージャーのカードが使うので残る（[shared-ui-parts.md](shared-ui-parts.md) §3）。

## 5. 状態（設定画面のモデルマネージャーのカードが使う）

トップ画面ではなく、設定画面の概要と「ローカル・代替モデル」の先頭にあるモデルマネージャーのカード（`localview.ManagerCard`）が、枠を先に出して `hx-get="/tools/model-manager/status" hx-trigger="load"` で ComfyUI の状態を後から読む。

**外部の API は呼ばない**。ローカルの設定ファイル・ComfyUI の同梱エンジンの**プロセスの状態**だけを読む。外部の ComfyUI へ接続して確かめることもしない（最大 10 秒かかりうる。接続は各画面で確かめる）。

- **応答は常に 200**（読めなくても）。本文は `Badge` ＋短い文の断片、または**空**。空なら状態は出ない（カードは出る）。`Cache-Control: no-store`。サーバー側で 2 秒の締め切り（`context.WithTimeout`）を持ち、超えたら空を返す。5xx にしないのは、`main.ts` の失敗のトースト（`htmx:responseError`）を避けるため。
- 取得は `web/handler/toolstatus/`（`handler/` は基準値を超えているため別のパッケージ）。サービスは `modelmanager.Service.Brief()`（導入記録・プロセスの状態・接続先の一覧だけを読む。ディスクに何も書かず、空き容量・ビルドの一覧も取らない。`Status()`・`Engine()` は書き込み確認（`CheckWritable`）をするので使わない）。organism・molecule はサービスを import しない（`layers_test`）。ハンドラーが表示用の値に直して渡す。
- 設定画面の概要の「API キーとログイン」のカードの状態（API キーの件数・ChatGPT のログイン）も `toolstatus.Services.Settings` が組み立てる（ルートは無い。`CredentialService.Configured`・`chatgpt.Service.Overview`・`codex.Service.Status`。トークンは読まず、ネットワークも使わない）。

### ComfyUI の状態（モデルマネージャーのカード）

同梱エンジンの状態（`modelmanager.Brief`。`Process`（`stopped`/`starting`/`running`/`failed`）、`Installed`（壊れていれば未導入として扱う）・`Supported`）と、登録済みの外部 ComfyUI の件数（`Brief.External`。`localmodels` の接続先のうち種類 ComfyUI で、同梱でないもの）から決める。外部は件数のみ。状態を読むだけで `comfyui` ディレクトリは作らない（未導入の機械にも作らない。テスト `TestComfyUIStatus_NotInstalledWritesNothingToDisk`）。

| 同梱エンジン | 外部 ComfyUI | 表示（`Badge` ・ 文） |
|--------------|-------------|----------------------|
| 起動中（`running`） | — | ok ・ ComfyUI: 起動中 |
| 起動しています（`starting`） | — | info ・ ComfyUI: 起動しています |
| 停止中（導入済み） | — | neutral ・ ComfyUI: 停止中 |
| 起動に失敗（`failed`） | — | error ・ ComfyUI: 起動に失敗 |
| 未導入、または非対応（Windows 以外） | 1 件以上 | neutral ・ 外部の ComfyUI: n 件 |
| 未導入 | なし | neutral ・ ComfyUI: 未導入 |
| 非対応（Windows 以外） | なし | neutral ・ 同梱の ComfyUI は Windows 版のみ |

- 同梱が導入済みで外部もあるときは、同梱の状態を出す（外部の件数は足さない）。
- 上の表のうち同梱エンジンの 6 状態は Windows 実機での確認が要る（**未検証**）。非 Windows の「非対応」は Linux 上でも確かめられる。

### エンドポイント

仕様は [../api/endpoints-home.md](../api/endpoints-home.md)。

| Method | Path | 内容 |
|--------|------|------|
| GET | `/tools/model-manager/status` | モデルマネージャーのカードの状態の断片。`Badge` ＋短い文を返し、読めないときは**空の 200**。`:tool` がほかの値（`nodes` など）のルートは無く 404。`paths.ToolStatus(paths.ToolModelManager)` で組み立てる |

## 6. アイコン

トップ画面のカードで使っていたアイコンは、左メニュー（`sidenav`）・設定画面のカード・モデルマネージャーが使い続けるので、`atoms` から消さない。Lucide の名前・入手元・ライセンスの計画は [shared-ui-parts.md](shared-ui-parts.md) §2。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 0.1 | 2026-10-25 | 新規作成（案） | Issue #846 決定事項 5(a)。実装前にオーナーが確認するためのトップ画面の仕様 |
| 0.2 | 2026-10-25 | §5 の ChatGPT の行を #845 の実装の現状（Codex 経由は `Overview` に含まれない）に合わせ、§9 に質問 9 を足した | 統合の見直し。#845 が先に入ったため |
| 0.3 | 2026-10-25 | 決定事項 1 の行にモーダルの担当が未決であることを足し、§5 に `endpoints.md` の行数（296 行）と置き場の案を書いた | 独立レビュー（PR #849）の指摘 |
| 0.4 | 2026-10-25 | 状態を「実装済み」にした。§9 をオーナーの決定（推奨する既定）の表にし、§5 のエンドポイントを [endpoints-home.md](../api/endpoints-home.md) に移した | Issue #846 の実装 |
| 0.5 | 2026-10-25 | §5 の取得元を `modelmanager.Service.Brief()`（ディスクに書かない）にし、ChatGPT の行と §9 の質問 9 を実装（Codex はログイン済みの判定だけ。再ログイン要は公式・画像生成（非公式）だけ）に合わせた。フォーカスの輪に `outline-hidden` を足した | 独立レビュー（PR #849 の 2 周目）の指摘 |
| 0.6 | 2026-10-25 | §2 の幅を、ページ全体 `max-w-5xl`（内側の `max-w-3xl` の枠をやめる）にし、§4 のグリッドをどの幅でも 3 列（`grid-cols-3`。1024×700 でも 309 px 幅）にした。§4 のワイヤーフレームと 1024×700 の変形、§8 の確認、§9 質問 2 の決定を合わせた | オーナー決定（2026-10-25）。上の区画だけ細い今の見た目より、ページ全体の幅をそろえ、カードも 1024 px で 3 列にする。実装は `pages/projects_page.templ`・`tool_section.templ` |
| 0.7 | 2026-11-03 | トップ画面のツールのカードを外した（#978）。§2 を 4 区画にし、§3 を外したカードと状態の行き先に、§5 を設定画面のモデルマネージャーのカードと設定の概要が使う状態だけに書き直した。旧 §4・§7〜§9（カードの見た目・アクセシビリティ・状態の場面・オーナーへの質問）は削除 | #978。左メニューと同じ行き先のカードだった |
