# 画面仕様: 全体の設定画面（分類ごとのサブページ）

> **状態: 段階 ①・②-1〜②-6 実装済み（2026-10-25）。** Issue #847。オーナーは §11 を推奨する既定で決定した（2026-10-25）。分類ごとの一覧とモーダルは [settings-screen-categories.md](settings-screen-categories.md)、新しいルートと応答の決まりは [settings-screen-routes.md](settings-screen-routes.md)、共通の部品（カード・アイコン・モーダル・一覧の行）は [shared-ui-parts.md](shared-ui-parts.md)。確認が要る点は §11。

[overview.md](overview.md) §3 の `global_settings_page`・`global_settings_body`（`GET /settings`。API キー・ChatGPT・LLM・画像動画モデル・ローカル・MCP・バージョンが 1 ページに縦に並ぶ）を、**左のサイドナビ ＋ 分類ごとのサブページ**に分ける。プロジェクトの設定（`/projects/:id/settings`）・モデルマネージャーの作り直し・全画面共通のナビゲーションは範囲外。

## 1. 決定事項（2026-10-25 オーナー決定）と、この仕様での扱い

| # | 決定 | この仕様 |
|---|------|---------|
| 1 | (a) 実装の前に画面仕様を書き、オーナーが確認する | この文書群 |
| 2 | (a) 左のサイドナビ ＋ 分類ごとのサブページ | §4 |
| 3 | (a) 7 つの分類 | §2 |
| 4 | (a) `/settings/<分類>`。今の断片の GET は HTMX でないときページ全体 | §3 |
| 5 | (a) 無料で読むだけの API があるプロバイダーだけ接続テスト | [categories](settings-screen-categories.md) §1 |
| 6 | (a) モデルマネージャーは別の画面のまま。「ローカル・代替モデル」の先頭と概要にカード | §5・[categories](settings-screen-categories.md) §5 |
| 7 | (a) プロジェクトの設定は別の Issue | 触らない |
| 8 | (a) 短い編集はモーダル、一覧を持つもの・長い作業はサブページ、削除の確認は `hx-confirm` のまま | [categories](settings-screen-categories.md) 各節の「方式」 |
| 9 | (a) LLM は Agent ごとに保存（`PUT /settings/llm/agents/:agent`） | [categories](settings-screen-categories.md) §2・[routes](settings-screen-routes.md) |
| 10 | (a) 部品ができた段階と完成の段階でデザインレビュー | §7 |
| 11 | (a) 2 段階: ① ナビ・サブページ・概要 ② 各分類の一覧とモーダル | §7 |

## 2. 分類・URL・スラッグ（決定事項 3(a)・4(a)。スラッグは案）

| 順 | ナビの名前（`h2`） | URL | 中身（今の区画） | 出す条件 |
|----|-------------------|-----|-----------------|---------|
| 0 | 概要 | `/settings` | 各分類の状態のカード（§5） | 常に |
| 1 | API キーとログイン | `/settings/api-keys` | API キー（`credentialpanel`）＋ ChatGPT でログイン（`chatgptpanel`。#845 の Codex 経由も） | 常に |
| 2 | テキスト生成（LLM） | `/settings/llm` | LLM の割り当て（`llmassign`） | `Settings` サービスがある |
| 3 | 画像・動画モデル | `/settings/models` | 画像の用途・Quality Policy ごとの割り当て（`modelassignment`）。年齢確認がオフの間は成人向けのモデルを選択肢から外し、保存済みの割り当ては「（現在は使えません）」付きで見せる（[settings-screen-categories.md](settings-screen-categories.md) §3） | 同上 |
| 4 | ローカル・代替モデル | `/settings/local-models` | 接続先とモデル（`localmodels`）＋モデルマネージャーへのカード | `Services.LocalModels` がある |
| 5 | MCP サーバー | `/settings/mcp` | 待ち受け先・トークン（`organisms/settings/mcp`） | `Services.MCP` がある |
| 6 | バージョンとアップデート | `/settings/update` | 版と「今すぐ確認」（`updatepanel`） | 常に（版の表示だけのときもある） |

- スラッグは、今の**書き込みのルートの接頭辞と同じ**にする（`PUT /settings/llm`・`/settings/models`・`/settings/mcp/…`・`/settings/update/check`）。同じパスに GET（ページ）と PUT・DELETE（操作）が並ぶ。API キーの分類だけ新しい接頭辞（`api-keys`）で、書き込みは今の `PUT /credentials/:provider` のまま。
- 出さない分類は、ナビに無く、その URL は 404（`GET /settings` だけは今も `Settings` サービスの有無にかかわらず開ける）。
- ページの `<title>` は「設定: <分類名>」（概要は「設定」）。分類ごとに異なる（WCAG 2.4.2）。

## 3. ルーティング（決定事項 4(a)）

### 今のルーターの調査（2026-10-25。`router/settingsroutes/settingsroutes.go`・`router/actions.go`）

「ルーターに同じパスの GET がすでに無いか」の確認結果。`/settings` 以下の **GET は 9 本だけ**（#845 で `/settings/chatgpt/codex/models` が増えた）で、新しいスラッグと重なるのは `local-models` の 1 本。

| GET | 今 | 新しいスラッグとの関係 |
|-----|----|---------------------|
| `/settings` | ページ（`GlobalSettingsPage`。HTMX のときは本体だけ） | 概要に変わる（§4） |
| `/settings/chatgpt` | 区画だけ（`#chatgpt-panel`。ログイン中に 2 秒ごとに取り直す） | スラッグではない。**API キーの分類のページを返す**側に回る（下） |
| `/settings/chatgpt/models`・`/settings/chatgpt/codex/models` | 利用できるモデルの断片（後者は #845 の Codex 経由） | 重ならない。HTMX でないときは `/settings/chatgpt` と同じ扱い |
| `/settings/local-models` | 区画だけ（`#local-models-panel`） | **スラッグと同じパス**。HTMX でなければ分類のページ、HTMX なら今の断片（下） |
| `/settings/local-models/endpoints/new`・`/endpoints/:name/edit`・`/endpoints/:name/models/new`・`/endpoints/:name/models/:model/edit` | 編集フォームの断片 | モーダル・接続先のサブページの断片になる（[routes](settings-screen-routes.md)） |

`/settings/llm`・`/settings/models`・`/settings/mcp`・`/settings/update` に GET は無い（PUT・DELETE・POST だけ）ので、新しく GET を足してよい。Gin v1.12.0 で、同じ位置の静的な `…/endpoints/new` とパラメーター `…/endpoints/:name`（と `:name/edit`・`:name/models/new`）が**同じメソッドで共存できる**ことを、使い捨てのプログラムで確かめた（`/new` は静的、`/foo` は `:name` に解決。`/endpoints/new/edit` は `:name=new` として解決）。

- **決定済み・実装済み**: 接続先の名前 `new` は `localcfg.ValidName` で有効（小文字・数字・`.` `_` `-`）だが、接続先のサブページを `GET /settings/local-models/endpoints/:name` に置くと、名前が `new` の接続先のサブページは追加フォームの `GET …/endpoints/new` に隠れて開けない。**決定**（§11 の質問 6）: 接続先の作成で名前 `new` を 422 にする（`service/localmodels` の `ReservedEndpointName`）。保存済みの設定にすでに `new` がある場合は、サブページへのリンクを出さず、サブページが無い旨をカードに出す（②-5）。

### HTMX かどうかの分け方

`handler.isHTMX`（`HX-Request: true` かつ `HX-History-Restore-Request` が `true` でない。`Vary: HX-Request, HX-History-Restore-Request` を付ける）と同じ判定を、新しいハンドラーでも使う。

| 要求 | `GET /settings`・`/settings/<スラッグ>` | `GET /settings/chatgpt` | `GET /settings/local-models` |
|------|--------------------------------------|------------------------|-----------------------------|
| ブラウザの通常の移動・再読み込み・戻る（`HX-Request` なし、または履歴の復元） | 分類のページ全体 | **API キーの分類のページ全体**（200。リダイレクトしない） | ローカル・代替モデルのページ全体 |
| HTMX の要求（ポーリング・操作の後の取り直し） | 分類の本体だけ（枠なし。`GET /settings` が今そうしている） | 今の区画 `#chatgpt-panel` の断片（ログイン中のポーリングが使う。ステージ ② ではカードの一覧の断片） | 今の区画 `#local-models-panel` の断片 |

- `/settings/chatgpt` の正規の URL は `/settings/api-keys`。ページ全体を返すときは、`<link rel="canonical" href="/settings/api-keys">` を付ける。ナビの現在の分類（`aria-current`）は API キー。
- ナビのリンクは**通常の `<a href>`**（`hx-boost` も `hx-get` も使わない）。分類の移動はページ全体の読み込みで、戻る・再読み込み・ブックマークが素直に動く。
- 分類ごとのルートの登録は `settingsroutes.Register` に足す（分類の表示条件は §2。サービスが nil の分類は登録しない）。ハンドラーは `web/handler/settingspage/`（新しいパッケージ）。ページを作る処理は 1 つにまとめ、`category` の値で本体を切り替える。

## 4. 画面の枠・ナビ

```
        1440×900 の枠（max-w-6xl＝1152 px、p-8。内側 1088 px）
┌────────────────────────────────────────────────────────────────────────────────────────┐
│ ← プロジェクト一覧に戻る                                                              │
│ 設定                                         ← h1（全ページ共通）                      │
│ すべてのプロジェクトで共通の設定です。                                                  │
│ ┌ nav aria-label="設定" 224px ┐ ┌ main（残り 832px）───────────────────────────────┐ │
│ │ ▌概要            ←current   │ │ 概要                       ← h2                   │ │
│ │  API キーとログイン         │ │ ┌ [🔑]API キーと… ┐ ┌ [💬]テキスト生成 ┐        │ │
│ │  テキスト生成（LLM）        │ │ │ API キー 3 / 10 件│ │ 変更 2 / 14 Agent │        │ │
│ │  画像・動画モデル           │ │ │ 開く ›          │ │ 開く ›          │        │ │
│ │  ローカル・代替モデル       │ │ └──────────────────┘ └──────────────────┘        │ │
│ │  MCP サーバー               │ │ ┌ 画像・動画モデル ┐ ┌ ローカル・代替 ┐ …（2 列）  │ │
│ │  バージョンとアップデート   │ │ …MCP・バージョン・モデルマネージャーのカード…      │ │
│ └─────────────────────────────┘ └───────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

**1024×700 の変形**: 枠の幅は 960 px（`p-8`）。nav 224 px ＋ すき間 32 px ＋ main 704 px。概要は 2 列のまま（カード 344 px 幅）。表は列を詰めて折り返す（切り詰めない）。縦は main だけがスクロールし、nav は `lg:sticky lg:top-8`。1024 px 未満（ブラウザ版の狭い窓）は nav が上に出て（`flex-col`）、リンクが折り返して並ぶ。

- **構造**: `<main class="mx-auto max-w-6xl p-8">` の中に、戻るリンク・`<h1>設定</h1>`・説明（`help.Row` の「?」ヘルプのパネル）・`<div class="mt-6 flex flex-col gap-8 lg:flex-row">`（`nav` と `<div class="min-w-0 flex-1">`）。分類の見出しは `<h2>` で、その横に分類の説明を開く「?」ボタン（`settingsui.Category(title, explanation)`。説明は常時表示せず、ヘルプに入る。[shared-ui-parts.md](shared-ui-parts.md) §9・§10）。同じ分類の中の区画の見出しは `<h3>`（`molecules.SettingsSection` に見出しのレベルを渡す。ステージ ① で、API キーの分類の「API キー」「ChatGPT でログイン」が `h3` になる。分類名と区画名が同じ分類（LLM・画像動画・MCP・バージョン）は、区画の見出しを出さず説明だけにする）。
- **ナビ（`molecules/settingsui.SettingsNav`）**: `<nav aria-label="設定"><ul role="list">`。項目は `<a href class="block rounded-md px-3 py-2 text-sm …">`。**今の分類**は `aria-current="page"`・`bg-neutral-800 font-medium text-neutral-100`・左に `border-l-2 border-indigo-400`（色だけにせず、太さの変化と枠で示す）。ほかは `text-neutral-300 hover:bg-neutral-900 hover:text-neutral-100`。フォーカスは [shared-ui-parts.md](shared-ui-parts.md) §1 の輪。アイコンは付けない（文字を優先。§11 の質問 3）。
- 状態のバッジをナビの項目に出さない（概要で見る）。
- 戻るリンクは今のまま（`atoms.BackLink(paths.Home(), "← プロジェクト一覧に戻る")`）。

## 5. 概要（`GET /settings`）のカード

[shared-ui-parts.md](shared-ui-parts.md) §3 の `ToolCard` を 2 列に並べる（`grid grid-cols-2 gap-4`）。サーバーで状態を組み立てる（`Status`。ローカルで取れるものだけ。トップ画面の状態の仕組み（`hx-get`）は、確認が重い 1 項目だけに使う）。

| カード | 状態の行（取得元） | 行き先 |
|--------|------------------|--------|
| API キーとログイン | API キー n / 10 件（`CredentialService.Status`）、ChatGPT のログイン（`chatgpt.Service.Overview`。トップ画面と同じ規則）。キーストアが読めないときは warn ・ キーストアを利用できません | `/settings/api-keys` |
| テキスト生成（LLM） | 既定から変えた Agent の数「変更 n / 14 件」（`llmassign.Service.View` の `Route` と `Default` を比べる）。固定（`Pinned`）のときは neutral ・ 起動オプションで固定 | `/settings/llm` |
| 画像・動画モデル | 「既定のまま」／「変更あり」（`modelsettings` の `Customized`）。固定のときは neutral ・ 起動オプションで固定 | `/settings/models` |
| ローカル・代替モデル | 接続先 n 件・モデル m 件。接続の状態は **`hx-get` で後から**（`GET /settings/local-models/health`。接続先ごとに並行して既存の `localmodels.Service.TestEndpoint` を呼ぶが、概要では各 3 秒で打ち切る。「n 件中 m 件に接続できます」。取れないときは空）。接続先 0 件は「接続先なし」 | `/settings/local-models` |
| MCP サーバー | 待ち受け先（`mcpsettings.Service.View` の `Addr`）、有効なトークン n 件（失効済みを除く） | `/settings/mcp` |
| バージョンとアップデート | 版（`updatepanel.Data.Version`）と状態（最後の確認の結果。`Tone` をそのまま使う） | `/settings/update` |
| モデルマネージャー（決定事項 6(a)） | ComfyUI の状態（トップ画面と同じ。[home-screen.md](home-screen.md) §5。`hx-get="/tools/model-manager/status"` を使う）。説明は「別の画面を開きます」を含める | `paths.ModelManager()` |

- 7 枚で、並びはナビと同じ順にモデルマネージャーを最後に置く。`Settings` サービスが無い構成では LLM・画像動画のカードを出さない（ナビと同じ条件）。
- 「接続の状態」を概要に出すかは §11 の質問 5（既定は上の案）。
- カードの下に「初期セットアップの案内」の区画（`h3`）を置き、「案内をもう一度開く」のリンク（`/onboarding`）を出す（#863。[onboarding-screen.md](onboarding-screen.md) §7）。初回ガイドを出さない構成（`Services.Onboarding` が無い）では区画ごと無い。

## 6. チャットの「設定画面を開く」リンク（`text_message` の `ref_type=settings`）

今は `molecules/format.go` の `refLinkOf` が `ref_type=settings` を `paths.Project(m.ProjectID, "settings")`（**プロジェクトの設定**。ディスク容量の警告から Take のアーカイブへ行く。唯一の発行元は `agent/generation/orchestrator.go` の `warnLowDisk`）に結んでいる。**全体の設定の分類へ案内する経路は無い**。

- 案: 新しい `ref_type=global_settings`、`ref_id` に分類のスラッグ（`api-keys` など。未知・空は `/settings`）。`refLinkOf` に 1 つ `case` を足し、ラベルは分類ごと（例: 「API キーの設定を開く」）、リンクは `paths.SettingsCategory(slug)`。`chat_message.ref_type` は `TEXT`（CHECK 制約なし。`20260929000001_init_schema.sql`）なのでマイグレーションは要らない。`spec/architecture/er.md` の `ref_type` の説明と `domain/chat.go` の定数に足す。今の `settings`（プロジェクトの設定）は変えない。
- **実装済み**（2026-10-25）: `domain.RefGlobalSettings`、`molecules/settings_link.go`（スラッグ → リンクの文言。未知・空は概要。`ref_id` から URL は作らない）。`er.md`（738 行）の `ref_type` の説明は更新していない。文言を既存の行（`chat_message` の行）に足すだけで行数は増えないことを試したが、コミット前の検査（lefthook の `linterly` = `scripts/linterly-staged.sh`）は、コミットするファイルが 300 行を超えていれば、増減にかかわらず止める（`make lint-lines` の ratchet とは別の検査。`--no-verify` は使えない）。行数の ratchet は通るが、コミットできない。`er.md` の分割は Issue #850 で扱う。契約はこの節と `domain/chat.go` の定数のコメントにある。
- この Issue で新しく発行する通知は**足さない**。経路の契約だけを決める。最初の利用者は、将来の「API キーが未設定です」のようなチャットの通知（今は無い）。§11 の質問 7。

## 7. 提供の段階（決定事項 11(a)・10(a)）

| 段階 | 内容 | 主な変更 | デザインレビュー |
|------|------|---------|------------------|
| ① **実装済み** | ナビ・分類ごとの URL・概要。**中身は今の区画をそのまま**各分類のページに移す（フォーム・ルート・応答は変えない） | `pages/settings_pages.templ`・`molecules/settingsui`（`SettingsNav`）・`organisms/settings/overview/`・`handler/settingspage/`・`settingsroutes`・`SettingsSection` の見出しのレベル | ① の完成で 1024×700・1440×900 |
| ②-1 **実装済み** | **モーダルの部品**と一覧の行の部品を作り、API キーのカードで使う（モーダルの担当は #847 に決まった。[shared-ui-parts.md](shared-ui-parts.md) §8 の質問 7 の推奨どおり。#846 の決定事項 1 の「モーダル」は #847 が作る部品を使う） | `molecules/modal/`・`static/src/components/modal/`・`organisms/settings/credentials/` | **部品の段階**で 1024×700・1440×900（決定事項 10(a) の 1 回目） |
| ②-2 **実装済み**（API キーのカードと接続テストを含む） | ChatGPT のログインをカードに（ログイン中の表示はモーダル）。#845 の Codex 経由は 3 つ目のカード（§9） | `organisms/settings/chatgpt/` | |
| ②-3 **実装済み** | LLM の割り当て（Agent の表・Agent ごとの保存） | `organisms/settings/llm/`・`handler/settings/llm/`・`PUT`・`DELETE /settings/llm/agents/:agent`。旧 `organisms/llmassign/`・`handler/llmassign/` は消した | |
| ②-4 **実装済み** | 画像・動画モデル（画像の用途の表・Quality Policy の表・行ごとのモーダルと保存） | `organisms/settings/media/`・`handler/settings/media/`・`PUT /settings/models/images/:purpose`・`…/policies/:policy`・`modelsettings.Service.SaveImage`・`SavePolicy`。旧 `organisms/modelassignment/`・`handler/modelassign/` は消し、ローカルのモデルの応答の `#model-assignment` の OOB を外した | |
| ②-5 **実装済み** | ローカルの接続先（カード・モーダル）とサブページ。モデルマネージャーのカードを先頭に置く。成功の応答は行の OOB（`#model-assignment` の OOB は廃止）。名前 `new` の保存済みの接続先は、サブページが無い旨をカードに出す | `organisms/settings/local/`・`handler/settings/local/`・`handler/settingspage/local.go` | |
| ②-6 **実装済み** | MCP（待ち受け先・トークンの表・発行のモーダル。発行は 201 でモーダルの本文を 1 回だけの表示に替える。コピーのボタン） | `organisms/settings/mcp/`・`handler/settings/mcp/`・`static/src/components/copy/` | **完成の段階**で全分類を 1024×700・1440×900（10(a) の 2 回目） |

- ② の各段階は、古い区画を**新しい場所に作り直して古いものを消す**（二重に残さない）。段階の間は、まだ移していない分類は ① のままの区画が動く。
- ① の時点で、概要から各分類へ行け、「1 ページに全部並んで見にくい」は解消する。
- ②-3・②-4 の確認（2026-10-25、ヘッドレスの Chromium、使い捨てのデータフォルダ・一時のキーリング、実キーなし）: LLM と画像・動画モデルの表とモーダルを 1024×700・1440×900 で撮って見た。モーダルの `Esc` で閉じて開いたボタンへフォーカスが戻ること、422 で入力が残り問題の欄（無ければ理由）にフォーカスが移ること、保存で行が OOB で差し替わりフォーカスが同じキーのボタンに戻ることを確かめた。年齢確認がオフの行の「（現在は使えません）」は、実際の成人向けの ComfyUI のモデルが要るため実画面では撮れず、同じ HTML を静的に描いて見た（実画面は **未検証**）。Windows の WebView2 は **未検証**（実機が要る）。

## 8. 変わるテスト（実装のとき。今のテストの期待値は、表示の場所の違いを除いて変えない）

| テスト | ① | ② |
|--------|---|---|
| `web/global_settings_test.go` | `GET /settings` に全区画がある前提を、概要＋各分類のページに書き直す。API キーのパネルは `/settings/api-keys` | カードの `id`・`data-testid`（既存を維持）に合わせる |
| `web/llm_settings_test.go`・`model_settings_test.go`・`model_settings_screen_test.go`・`mcp_settings_test.go`・`update_settings_test.go` | ページの取得先を各分類の URL に変える | モーダルの断片・Agent ごとの保存・OOB の行・422 の本文を検査するものに書き換える（今の全体フォームの検査は、`PUT /settings/llm` と `/settings/models` が残るので残す） |
| `router/credential_test.go`・`web/credential_handler_test.go` | `GET /settings/api-keys` を取得する | 保存の応答を OOB の行＋ `HX-Trigger-After-Swap` に。422・キーが応答に出ないことは残す |
| `web/handler/settings/local/screen_test.go`・`endpoints_test.go` | 取得先を `/settings/local-models`（HTMX でない）に | カード・モーダル・サブページ。`#model-assignment` の OOB の検査は、別ページになるため外す |
| `web/handler/chatgptsettings/chatgptsettings_test.go` | `GET /settings/chatgpt`（HTMX なし）がページ全体、HTMX が断片 | カードの一覧の断片 |
| `web/destructive_confirm_test.go`・`double_submit_test.go`・`a11y/*` | 新しいテンプレートも検査の対象になる | モーダルのフォームの二重送信防止・削除の `hx-confirm`・行ごとのボタン名（`SmallButtonFor`）を足す |
| 新規 | `settingspage` のスラッグ・404（サービス nil）・HTMX 分岐・`Vary`・`canonical`、`SettingsNav` の `aria-current`・`nav` の名前、`paths.SettingsCategory` | wtr のモーダル（[shared-ui-parts.md](shared-ui-parts.md) §4） |

## 9. 配置（行数の上限: 1 ファイル 300 行・1 ディレクトリ 2000 行）

| 置き場所（案） | 内容 | 今ある場所 |
|----------------|------|-----------|
| `pages/settings_pages.templ`（約 60 行） | 枠（戻る・h1・nav・分類の h2） | `pages/global_settings_page.templ` を置き換え |
| `organisms/settings/overview/` | 概要のカード | 新規（`global_settings_body.templ` を置き換え） |
| `organisms/settings/credentials/` | API キーのカード・モーダルの断片 | `organisms/credentialpanel/`（111 行）を作り直して消す |
| `organisms/settings/chatgpt/` | ログインのカード・ログイン中のモーダル・モデル一覧のモーダル | `organisms/chatgptpanel/`（`panel.templ` 274 行・Codex 経由の `codex.templ` 218 行。1 ファイルの上限に近いのでカード・モーダル・データに分ける） |
| `organisms/settings/llm/`・`media/`・`local/`・`mcp/` | 各分類 | `llmassign/`・`modelassignment/`・`localmodels/`（②-5 で `settings/local/` に移した）・`mcppanel/`（②-6 で `settings/mcp/` に移した）。バージョンとアップデートは `organisms/updatepanel/` のまま動かしていない（`GET /settings/update` もこれを使う） |
| `handler/settingspage/`・`handler/settings/<分類>/` | ページ・モーダルの断片・保存。`handler/` は基準値 2,895 行で上限を超え、増やせない | `handler/credential.go` の保存・削除は `handler/settings/credentials/` に移す |
| `molecules/settingsui/`・`molecules/modal/` | ナビ・カード・表・モーダル | 新規 |

- **#845（Codex 経由）の取り込み**: 「API キーとログイン」の分類に、方式ごとのカードを並べる（`organisms/settings/chatgpt/` のカードの一覧が `[]AccountCard` を受け取る。公式・画像生成（非公式）・Codex 経由の順。#845 は 3 つ目を足すだけで、枠・モーダルの部品は触らない）。#845 は入っていて、Codex 経由は今の縦のブロック（`chatgptpanel/codex.templ`。方式のタブ・エンジンの取得・パスの指定・ログイン・利用上限の更新・モデル一覧）として足されており、②-2 が 3 つを一緒にカードにする。Codex の取得・ログインの仕組み（#845 の範囲）は変えない。
- **方式のタブ（#845 が足した `role="tablist"`）**: #847 の Issue の背景は「`role="tablist"` はリポジトリに見当たらない」と書いている（起票の時点。2026-10-25）が、#845 が入ったあとの `chatgptpanel/codex.templ`（`aria-label="ログインの方式"`、`aria-selected`）には、方式の切り替えのタブがある。②-2 でカードに置き換えるとき、このタブはなくなる（方式ごとのカードに分かれるため）。タブを検査しているテストがあれば、②-2 で一緒に直す。
- 層の規則（`layers_test`）: organism・molecule はサービスを import せず、ハンドラーが表示用の値を渡す。

## 10. MCP が `web/` に依存しないことの調査（#847「背景」の未検証の項目）

**結果: 依存していない（2026-10-25、コードを読んで確認）。**

- `backend/internal/mcp/` の Go ファイルが import する自リポジトリのパッケージを数えると、`mcp`・`service`・`domain`・`agent`・`repository`・`credential`・`modelmgr`・`chatgpt`・`testenv`・`media` だけで、`internal/web` は 0 件。
- `go list -deps ./backend/internal/mcp/...` の出力にも `internal/web` を含むものは 0 件（ツール・サーバーの推移的な依存まで含む）。
- 設定の MCP ツールは、サービス層を直接呼ぶ（`mcp/tools/settings/llm.go` は `service/llmassign.Service` の `View`・`Save`・`Reset`）。画面の organism・handler は通らない。
- 帰結: 画面の作り直しは MCP を変えない。サービスの既存の関数（`llmassign.Service.Save`・`Reset` など）の意味は変えない。Agent ごとの保存は、既存の `Save` が「`routes` に無い Agent は保存済みのまま」（`service/llmassign/llmassign.go`）なので、1 件だけの `map` を渡せば足り、サービスの関数は足さない（[routes](settings-screen-routes.md) §2）。`web/` から `mcp/` への逆向きの依存を禁じる検査は今のところ無い（§11 の質問 8）。

## 11. オーナーへの確認事項（決まっていない点と、推奨する既定）

| # | 質問 | 推奨する既定 |
|---|------|-------------|
| 1 | スラッグ（`api-keys`・`llm`・`models`・`local-models`・`mcp`・`update`）と分類名（§2）でよいか | §2 のとおり（書き込みの URL の接頭辞と同じにして、新しい接頭辞を `api-keys` だけにする） |
| 2 | `/settings/chatgpt` を HTMX でなく開いたとき、ページ全体を返す（決定事項 4(a)）か、正規の URL への 301/303 のリダイレクトか | 決定のとおりページ全体を返す（`canonical` を付ける）。リダイレクトのほうが URL は 1 つになるが、決定の文言と違う |
| 3 | ナビの項目にアイコンを付けるか | 付けない（文字だけ。幅 224 px で文字を優先）。概要のカードには付ける |
| 4 | 概要に「モデルマネージャー」のカードを 7 枚目として置くか（決定事項 6(a) は「概要にカード」） | 置く（6 つの分類のあとに） |
| 5 | 概要のローカル接続先の「接続の状態」を、`hx-get` で後から確かめるか（Issue の本文は「接続の状態」を含む） | 後から確かめる（各 3 秒で打ち切り。取れなければ空）。件数だけにする案もある（接続は 10 秒かかりうるため） |
| 6 | 接続先の名前 `new` を作成で 422 にするか（§3） | 422 にする（`…/endpoints/:name` のサブページと衝突させない） |
| 7 | チャットの設定リンクに `ref_type=global_settings` ＋ `ref_id=<スラッグ>` を足すか（§6）。今のリンクはプロジェクトの設定 | 契約として足す（新しい通知はこの Issue では作らない）。足さずに別の Issue にする案もある |
| 8 | `mcp/` が `web/` を import しないことを検査するテストを足すか（§10） | 足さない（調査結果の記録にとどめる。範囲外） |
| 9 | 概要・分類の本文の説明文（各分類の h2 のあとの 1 文）の文言 | 今の `SettingsSection` の説明をそのまま使う。**Issue #866 で、常時表示から h2 の横の「?」ヘルプへ移した**（文言は同じ） |
| 10 | ② の分類ごとの PR の順（②-1〜②-6） | §7 の順（モーダルの部品を API キーで先に確かめる） |

詳細な質問（API キーの接続テスト・LLM・画像動画の保存の単位・ローカルのモデルの編集の方式・MCP など）は [settings-screen-categories.md](settings-screen-categories.md) §7 と [settings-screen-routes.md](settings-screen-routes.md) §4。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 0.1 | 2026-10-25 | 新規作成（案） | Issue #847 決定事項 1(a)。実装前にオーナーが確認するための設定画面の仕様 |
| 0.2 | 2026-10-25 | §3 の GET の数（8 → 9。`/settings/chatgpt/codex/models`）と §9 の `chatgptpanel` の行数・#845 の取り込みの記述を、入った #845 の実装に合わせた | 統合の見直し。#845 が先に入ったため |
| 0.3 | 2026-10-25 | ②-1 のモーダルの担当を未決とし（[shared-ui-parts.md](shared-ui-parts.md) §8 の質問 7）、#845 のタブ（`role="tablist"`）の扱いを §9 に足した | 独立レビュー（PR #849）の指摘 |
| 0.4 | 2026-10-25 | §2 の分類の表の「画像・動画モデル」の行に、年齢確認がオフの間の画面の挙動（成人向けを選択肢から外す・「（現在は使えません）」・保存は 422）への参照を足した。詳細は settings-screen-categories.md §3（0.4） | PR #849 の独立レビュー（#848）の指摘 |
| 0.5 | 2026-10-25 | 状態を「段階 ①・②-1・②-2 実装済み」にした（§7 の表に印）。実装: ナビ・概要・分類ごとのページ・モーダル・API キーとログイン。接続先の名前 `new` は 422（§11 の質問 6 の推奨）。ログインのカードの取り直しは `GET /settings/chatgpt/:kind/login?poll=1`（モーダル）とパネル全体の取り直しで行い、`card=<kind>` は使わなかった（`method` クエリは読まない） | Issue #847 の実装 |
| 0.6 | 2026-10-25 | 状態を「段階 ①〜②-4 実装済み」にした（§7 の表に印）。実装: テキスト生成（LLM）と画像・動画モデルを表とモーダルに（`SettingsTable`）。1024×700 では「用途」の列を `lg` ではなく `xl` から出す（サイドナビで表が約 700 px に狭まり、`lg` ではモデル名が割れるため。[settings-screen-categories.md](settings-screen-categories.md) §2） | Issue #847 段階 ②-3・②-4 の実装 |
| 0.7 | 2026-10-25 | 段階 ②-5（ローカル・代替モデル）と ②-6（MCP サーバー）を実装済みにした。チャットのリンク `ref_type=global_settings` の経路（契約だけ。通知は足さない）を §6 に実装済みと書いた。`localmodels`・`mcppanel` は `organisms/settings/local/`・`settings/mcp/`（と `handler/settings/local/`・`settings/mcp/`）に移した | Issue #847 の実装 |
| 0.8 | 2026-10-25 | 古い記述を決定・実装に合わせた: §3 の名前 `new`（決定済み・実装済み）、§6 の `er.md` を更新しない理由（コミット前の検査は、増減に関わらず 300 行超のファイルを止める。ratchet とは別）、§7 ②-1 のモーダルの担当（#847 に決定）、§9 の `updatepanel`（移していない） | PR #849 の第 2 周の独立レビューの指摘 |
| 0.9 | 2026-10-28 | §5 に、概要の「初期セットアップの案内」の区画（初回ガイドを開き直すリンク）を足した | Issue #863。設定画面からいつでも案内をもう一度開ける |
| 0.10 | 2026-10-28 | §4 の枠と §11 の質問 9 を、分類の説明が h2 の横の「?」ヘルプに入ることに合わせた | Issue #866。画面の文字を減らす |
