# コンポーネント仕様: 共通の画面部品（カード・アイコン・モーダル・一覧の行）

> **状態: 案（実装前。オーナーの確認待ち）。** Issue #846（トップ画面のカード）と #847（設定画面のサブページ化）が共有する部品の見た目と契約。ここに書いた内容は、オーナーが確認するまで実装しない。確認が要る点は §8。

[overview.md](overview.md) §3 のコンポーネントの階層に、次の部品を足す案。使う画面は [home-screen.md](home-screen.md)（`tool_card`・`atoms.Icon`）と [settings-screen.md](settings-screen.md)・[settings-screen-categories.md](settings-screen-categories.md)（すべて）。2 つの Issue は先に出る #846 が `atoms.Icon`・`molecules.ToolCard` を作り、#847 がモーダル・一覧の行・サイドナビを足す（#846 決定事項 1(a)・#847 決定事項 1(a)）。

## 1. 見た目の決まり（新しいデザイントークンは作らない）

`static/src/css/app.css` は `@import "tailwindcss"` と `@source` だけで、独自のトークンは無い。既存のパレットだけを使う。コントラスト比は WCAG の式で計算した値（2026-10-25）。

| 用途 | クラス | 備考 |
|------|--------|------|
| ページの背景・本文 | `bg-neutral-950 text-neutral-100`（`AppLayout`） | 変えない |
| カード・モーダルの面 | `bg-neutral-900 border border-neutral-800 rounded-lg`、内側 `p-4` | 既存の `GenerationTest` の選択カードが `bg-neutral-900`、`ProjectList` が `border-neutral-800` |
| 補助の文字 | `text-neutral-400`（`neutral-900` 上 7.11:1、`neutral-950` 上 7.85:1） | `text-neutral-500` は使わない（`a11y/contrast_test.go`。3.78:1） |
| 主要な文字 | `text-neutral-100`（`neutral-900` 上 16.44:1） | |
| アイコンの色 | `text-indigo-300`（`neutral-800` 上 7.59:1、`neutral-900` 上 8.99:1） | 飾りなので 1.4.11 の対象外だが、見える色にしておく |
| フォーカスの輪 | `focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-indigo-400 focus-visible:ring-offset-2 focus-visible:ring-offset-neutral-950`（`indigo-400` は `neutral-950` 上 6.64:1、`neutral-900` 上 6.01:1） | モーダルの中は `ring-offset-neutral-900`。既存の `peer-focus-visible:ring-2 ring-indigo-400`（チャットの添付）と同じ色 |
| ホバー | `hover:border-neutral-600 hover:bg-neutral-800/60` | 見た目の補助。ホバーだけに意味を持たせない |
| 動き | `transition-colors motion-reduce:transition-none` のみ。拡大・移動・アニメーションは使わない | #797（`prefers-reduced-motion`）。スピナーの `motion-reduce:animate-none` が先例 |
| 主ボタンの `bg-indigo-500` | `atoms.TonePrimary` の実ボタン専用 | カードの見た目に使わない（カードはリンクであってボタンではない） |
| 状態 | `atoms.Badge`（neutral / info / warn / ok / error）と文字。**色だけで伝えない**（#790・#795） | §2 の対応表 |
| カードの枠 | `border-neutral-800` は飾り。カードはタイトルの文字とフォーカスの輪で見分ける | 入力欄の枠のコントラスト（`field_border_test`）の対象ではない |

### 状態の種類と `Badge` の対応（両 Issue で共通）

| 意味 | `Badge` | 文言の例 | 決まり |
|------|---------|---------|--------|
| 使える・済んでいる | `BadgeOK` | 起動中・ログイン済み・設定済み・接続できた | |
| 件数・中立の情報 | `BadgeNeutral`（0 件も同じ） | 保存 3 件・受け取り 0 件・未導入・停止中・未ログイン・未設定 | 「無い」ことは悪いことではない |
| 進行中・補足 | `BadgeInfo` | 起動しています・確認中・変更あり | |
| 対応が要る | `BadgeWarn` | ComfyUI が必要です・再ログインが必要・キーストアを利用できません | 文言に「必要」「できません」など**理由の言葉**を含める |
| 失敗 | `BadgeError` | 起動に失敗・接続できなかった | 理由は文字で添える |

## 2. `atoms.Icon`

- 署名の案: `templ Icon(name IconName, class string)`。`IconName` は文字列型の定数（綴りの間違いはコンパイルで落ちる）。未知の名前は描かない。
- 出力: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false" class="{class}">…</svg>`。`<title>` は付けない。大きさは `class`（既定 `h-5 w-5 shrink-0`）。色は `currentColor`（親の `text-*`）。
- アイコンだけで意味を持つボタンは作らない。必要ならボタンに `aria-label`（既存の `IconButton`）。このリポジトリのアイコンはすべて飾りで、意味は文字が持つ。
- 入手元は #846 決定事項 3(a): **Lucide（ISC）の SVG のパスを `atoms/icon.templ` にコピー**する（npm の依存は足さない）。`atoms` の行数は約 414 行で、約 16 個で 150 行前後の見込み（1 ファイル 300 行以内に収める）。コピー元の版は `icon.templ` の先頭のコメントに固定する。

| `IconName`（案） | Lucide の名前 | 使う場所 | 入れる Issue |
|------------------|---------------|---------|-------------|
| `IconFlask` | `flask-conical` | 生成テスト | #846 |
| `IconNetwork` | `network` | ノード | #846 |
| `IconWorkflow` | `workflow` | ワークフロー | #846 |
| `IconPackage` | `package` | モデルマネージャー | #846 |
| `IconSettings` | `settings` | 設定 | #846 |
| `IconChart` | `chart-column` | 制作指標 | #846 |
| `IconReview` | `clipboard-check` | レビューパッケージ | #846 |
| `IconChevronRight` | `chevron-right` | カードの行き先の矢印 | #846 |
| `IconKey` | `key-round` | 設定の概要: API キーとログイン | #847 |
| `IconText` | `message-square-text` | 〃 テキスト生成（LLM） | #847 |
| `IconMedia` | `clapperboard` | 〃 画像・動画モデル | #847 |
| `IconLocal` | `hard-drive` | 〃 ローカル・代替モデル | #847 |
| `IconPlug` | `plug` | 〃 MCP サーバー | #847 |
| `IconUpdate` | `refresh-cw` | 〃 バージョンとアップデート | #847 |
| `IconClose` | `x` | モーダルの閉じるボタン | #847 |
| `IconCopy` | `copy` | MCP トークンのコピー | #847 |

- 16 個で、#846 の 8 個 + #847 の 8 個。Issue の「10 個前後」より多いのは、#847 が概要のカードにもアイコンを付けるため（§8 の質問 1。付けないなら #847 は `x` と `copy` の 2 個）。
- 上の Lucide の名前は、2026-10-25 に `lucide-icons/lucide` の `main` の `icons/<名前>.svg` が存在することを確かめた（HTTP 200）。コピーする版（タグ）は実装のときに決め、その版に同じ名前があることを確かめる。名前は Lucide の版で変わることがある。
- **ライセンスの扱い（計画）**: Lucide の `LICENSE` は、ISC（Lucide Icons and Contributors）に加えて、Feather 由来のアイコンの一覧と MIT（Cole Bemis）の全文を持つ。上の表では `x`・`chevron-right` が Feather 由来。このため、取り込む版の `LICENSE` の**全文**を `backend/internal/web/atoms/icons/LICENSE`（置き場は案）に置き、`scripts/third-party-notices.sh` が「同梱した素材」の節として `THIRD_PARTY_NOTICES.md` に載せる（今のスクリプトは Go のモジュールと npm のパッケージだけを拾い、コピーした素材は拾わない）。`third-party-notices_test.sh` に、その節があることの検査を足す。README のライセンスの記述にも一行足す。

## 3. `molecules.ToolCard`（カード）

2 つの画面で使う、リンクとしてのカード。**カードの中にボタンもリンクも入れない**（カード全体が 1 つの `<a>`）。

```go
type ToolCard struct {
    ID          string        // 要素 id の元（"generation-test" など。英小文字とハイフン）
    Href        templ.SafeURL // paths.* から作る
    Icon        atoms.IconName
    Title       string        // リンクの名前になる
    Description string        // 1 行の説明（2 行までに折り返す）
    CTA         string        // 既定 "開く"。見た目だけ（aria-hidden）
    StatusURL   string        // 空でなければ、状態を hx-get で後から読む（トップ画面）
    Status      []StatusLine  // StatusURL が空のときの、サーバーで組み立てた状態（設定の概要）
}
type StatusLine struct{ Tone atoms.BadgeTone; Label, Text string }
```

```html
<li>
  <a id="tool-<ID>" href="…" data-testid="tool-card" data-tool="<ID>"
     aria-labelledby="tool-<ID>-title" aria-describedby="tool-<ID>-desc tool-<ID>-status"
     class="flex h-full min-h-36 flex-col gap-3 rounded-lg border border-neutral-800 bg-neutral-900 p-4 … (§1 のホバー・フォーカス)">
    <span class="flex items-center gap-3">
      <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-neutral-800 text-indigo-300" aria-hidden="true">{Icon}</span>
      <span id="tool-<ID>-title" class="text-base font-semibold text-neutral-100">{Title}</span>
    </span>
    <span id="tool-<ID>-desc" class="text-sm text-neutral-400">{Description}</span>
    <span id="tool-<ID>-status" class="mt-auto flex min-h-6 flex-col gap-1 text-xs text-neutral-300"
          hx-get="{StatusURL}" hx-trigger="load" hx-target="this" hx-swap="innerHTML"></span>
    <span class="inline-flex items-center gap-1 text-sm font-medium text-indigo-300" aria-hidden="true">{CTA}{IconChevronRight}</span>
  </a>
</li>
```

- **名前と説明**: リンクの名前はタイトルだけ（`aria-labelledby`。WCAG 2.5.3 の「見える名前を含む」を、タイトルを名前とする方針で満たす）。説明と状態は `aria-describedby`。CTA は見た目だけなので読ませない（`aria-hidden`）。
- 並べ方は `<ul role="list">`（Safari の VoiceOver が `list-style: none` のリストを読まないため）。1 グループのカードは 1 つの `<ul>`。
- `StatusURL` の枠は、読み込み前は空（高さ `min-h-6` を確保してレイアウトが跳ねないようにする）。読み込みに失敗しても枠は空のままで、エラーのトーストは出さない（応答は常に 200。[home-screen.md](home-screen.md) §5）。状態の枠は `aria-live` にしない（読み上げで割り込ませない。フォーカスしたときに `aria-describedby` で読まれる）。
- `hx-*` は子の `<span>` に置く。`<a>` 自体には付けない（通常のリンクのまま、右クリック・中クリック・「リンクをコピー」が使える）。
- 置き場所: `backend/internal/web/molecules/tool_card.templ` と `tool_card_test.go`（`molecules/` は約 1,663 行で、上限 2,000 行まで約 337 行）。#847 の部品は §7 の別パッケージに置く。
- テスト（実装のとき）: ①リンクの名前がタイトルで、カードの中に `<button>`・入れ子の `<a>` が無い ②`aria-describedby` の id がすべて存在する ③一覧のカードの名前が重複しない（`a11y` に足す）④`text-neutral-500` が無い（既存の検査）。

## 4. `molecules` のモーダル（`Modal`）

ネイティブの `<dialog>` を 1 つだけ、ページの枠に置く。中身は `hx-get` で差し込む。使うのは #847（トップ画面は使わない）。

```html
<dialog id="mam-modal" aria-labelledby="mam-modal-title" aria-describedby="mam-modal-desc" data-modal
        class="m-auto w-[min(40rem,calc(100vw-2rem))] max-h-[calc(100vh-4rem)] overflow-y-auto rounded-lg border border-neutral-700 bg-neutral-900 p-0 text-neutral-100 backdrop:bg-black/70">
  <div id="mam-modal-body"></div>
  <p id="mam-modal-failure" role="alert" class="px-4 pb-2 text-sm text-red-300" hidden></p>
  <button type="button" data-modal-close aria-label="閉じる" class="absolute right-3 top-3 …">{IconClose}</button>
</dialog>
```

- **置く場所**: 設定の各ページの枠（`pages/settings_pages.templ`。1 ページに 1 つ）。`AppLayout` には足さない（他の画面に不要な要素を出さない。§8 の質問 4）。幅は既定 40rem（640 px）で、1024 px のウィンドウでも余白が残る。広い内容（モデルの割り当て表）は `data-size="lg"` で `max-w-3xl`。高さは画面の高さ − 4rem までで、超えたら中がスクロールする（700 px の高さでも収まる）。
- **断片の形**（`GET …/edit` などが返す。ルートの根は `#mam-modal-content`）: `<h2 id="mam-modal-title">`（モーダルの名前）→ 任意で `<p id="mam-modal-desc">` → 本文（フォーム）→ フッター（`キャンセル`（`type=button data-modal-close`）と `保存`）。**最初に触る要素に `autofocus` を 1 つだけ付ける**（最初の入力欄。編集できる欄が無いときは閉じるボタン）。エラーは本文の先頭の `<p role="alert" id="mam-modal-error" tabindex="-1">`。
- **開く**: 開くボタンは `hx-get="{URL}" hx-target="#mam-modal-body" hx-swap="innerHTML" data-modal-open data-focus-key="{キー}"` を持つ（属性は `molecules.ModalOpen(url, focusKey)` が作る）。応答が `#mam-modal-body` に入ったら（`htmx:afterSwap`）、`static/src/components/modal/` が `dialog.showModal()` を呼び、`autofocus` の要素へフォーカスする。応答が 404・409 でも、理由の断片（`role="alert"` と「閉じる」だけ）を同じように表示する（`main.ts` が HTML のエラー応答を swap する）。
- **閉じる**: `Esc`（ネイティブの `cancel`）、`キャンセル`、`閉じる`、保存の成功（下）。**背景のクリックでは閉じない**（入力途中の消失を防ぐ。§8 の質問 5）。閉じたとき（`close` イベント）に `#mam-modal-body` を**空にする**（発行したトークン・入力値を DOM に残さない）。さらに `htmx:beforeHistorySave` で本文を空にし `open` を外す（`lib/history.ts` の `sanitizeHistoryHtml` に足す。トークンが htmx の履歴スナップショット（sessionStorage）に残らないように）。
- **フォーカスの閉じ込め**: `showModal()` が外側を `inert` にするので、独自の閉じ込めは書かない。Windows 版の WebView2（Chromium）でも同じ動作の見込み（**未検証**。実機が要る）。
- **フォーカスの戻り**（`focus-restore` と同じ考え方）: 開くボタンを `htmx:beforeRequest` の時点で覚える（`document.activeElement`、`data-focus-key`、最も近い `id` 付きの祖先の id）。閉じたあと、(1) 覚えたボタンがまだ DOM にあればそこへ、(2) 行が OOB で差し替わっていれば、覚えた行の中で種類と `data-focus-key` が同じコントロールへ、(3) 無ければその行、(4) それも無ければ一覧の見出しへ（`tabindex="-1"` を付けて）。開くボタンには必ず `data-focus-key`（`edit-<行のキー>`・`add`・`issue` など。ラベルが変わってもキーは変えない）。
- **成功**: 保存の応答は 200 で、本文は**一覧の行の OOB だけ**（`hx-swap-oob="true"`。同じ `id` の行を置き換える。新しい行は `beforeend:#<一覧の id>`）、ヘッダーに `HX-Trigger-After-Swap: modal-close`。モーダルの本文には空が入る。`modal/` は `document` で `modal-close` を聞いて閉じる（htmx 2.0.11 の `htmx.js` は、リクエストを出した要素が swap で DOM から消えていると、`HX-Trigger-After-Swap` を `<body>` に起こす。2026-10-25 に読んで確認。`bubbles` なので `document` で受けられる）。
- **失敗（422）**: 応答の本文はモーダルの断片（`#mam-modal-body` に入り直す）。理由は `#mam-modal-error`、入力は**残す**（`value` を描き戻す）。`aria-invalid="true"` と `aria-describedby="mam-modal-error"` を、問題の欄に付ける。差し替え後は、問題の欄（無ければ `#mam-modal-error`）にフォーカス（`autofocus` を付け直す）。**秘密の値（API キー・トークン）は 422 でも描き戻さない**（入力し直す）。
- **トーストは見えない**: `#toast-region` は `<body>` の子で、モーダルは最前面の層（top layer）に出るため、モーダルが開いている間のトーストはモーダルの背後に隠れる。そのためモーダル内の失敗は、断片の `role="alert"` に出す。HTML でない失敗（通信断・タイムアウト・5xx の非 HTML）は `htmx:responseError` / `sendError` / `timeout` を `modal/` が聞き、`#mam-modal-failure` に `requestFailureMessage` の文言を出す（トーストは出さない）。
- **二重送信**: モーダルのフォームは `hx-sync="this:drop"` と `hx-disabled-elt="find button[type=submit]"` を持つ（`double_submit_test`）。削除などの危険な操作は `hx-confirm`（#847 決定事項 8(a)。`destructive_confirm_test`）。
- **置き場所**: `molecules/modal/`（`Host`・`Frame`・`ModalOpen`）と `static/src/components/modal/`（`modal.ts`・`modal.wtest.ts`。Lit ではなく `focus-restore` と同じ `installModal()`。`main.ts` が呼ぶ）。テストは wtr: 開くとフォーカスが `autofocus` へ・`Esc` で閉じて開いたボタンへ戻る・行が差し替わっても戻る・成功で閉じる・422 で開いたまま入力が残る・閉じると本文が空・履歴の保存で空になる。

## 5. 一覧の行（`SettingsCard`・`SettingsTable`・`SettingsTableRow`）

#847 の一覧は 2 つの形。どちらも**行の `id` が OOB の差し替え先**（既存の `credential-row-<provider>` などの `id` と `data-testid` はそのまま使う。既存のテストの変更を減らすため）。

| 形 | 使う所 | 構造 |
|----|-------|------|
| `SettingsCard` | API キー・ChatGPT のログイン・ローカルの接続先・MCP の待ち受け先 | `<li id="<種類>-row-<キー>" data-testid="settings-card">`。名前（`<h3>`）・状態の `Badge`・1 行の説明・要約・ボタンの行。`ul role="list"`、`grid-cols-2`（1024 px でも 2 列） |
| `SettingsTable` ＋ `SettingsTableRow` | LLM の割り当て・画像・動画モデル・MCP のトークン | `<table class="w-full text-sm">`、`<caption class="sr-only">`、`<th scope="col">`、行の見出し `<th scope="row">`、`<tr id="<種類>-row-<キー>">`。1 つの行に要約と「編集」 |

- 行のボタンは **`atoms.SmallButtonFor(tone, "編集", 行の名前, attrs)`**（見える文言は「編集」のまま、`aria-label` が「編集: <行の名前>」。#783・#793。`a11y/row_names_*` の検査に足す）。モーダルを開くボタンは `ModalOpen` の属性を持つ。
- 状態は `Badge` ＋文字（§1）。「変更あり」「既定」のような印も文字のバッジ（色だけにしない）。
- 説明・要約は `text-neutral-400`、折り返しを許す（`break-words`）。切り詰め（`truncate`）で情報を隠さない。
- 置き場所: `molecules/settingsui/`（`card.templ`・`table.templ`・`nav.templ`）。`molecules/` の直下に置くと上限（2,000 行）に近づくため、サブパッケージにする（§8 の質問 3）。サイドナビ `SettingsNav` は [settings-screen.md](settings-screen.md) §4。

## 6. 2 つの Issue の取り合い

| 部品 | #846 | #847 | 備考 |
|------|------|------|------|
| `atoms.Icon` | 作る（8 個） | 8 個を足す | 先に出るほうが `atoms/icon.templ` と `LICENSE`・`THIRD_PARTY_NOTICES.md` の仕組みを作る |
| `molecules.ToolCard` | 作る（`StatusURL`） | 概要で使う（`Status`） | `Status` を持つ形は #847 が足してよい。#846 の時点では `StatusURL` だけでも契約は変わらない |
| モーダル・一覧の行・サイドナビ | 使わない | 作る | |
| 状態の `Badge` の対応（§1） | 使う | 使う | 同じ文言・同じ色 |

## 7. 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 0.1 | 2026-10-25 | 新規作成（案） | Issue #846 決定事項 5(a)・#847 決定事項 1(a)。実装前にオーナーが確認するための共通部品の仕様 |

## 8. オーナーへの確認事項（決まっていない点と、推奨する既定）

| # | 質問 | 推奨する既定 |
|---|------|-------------|
| 1 | #847 の概要のカードとサイドナビにアイコンを付けるか | カードには付ける（`ToolCard` と同じ部品）。サイドナビは文字だけ（幅 200〜240 px で文字を優先）。付けないなら #847 のアイコンは 2 個で済む |
| 2 | カードの「開く」を、枠のあるボタン風にするか、文字＋矢印にするか | 文字＋矢印（カード全体がリンクなので、別のボタンに見せない。`bg-indigo-500` は実ボタン専用） |
| 3 | #847 の部品を `molecules/settingsui/`・`molecules/modal/` のサブパッケージに置くか、`.linterly-baseline` を広げて `molecules/` に置くか | サブパッケージ（`organisms/` が基準値を超えた経緯を繰り返さない）。`ToolCard` だけは `molecules/` の直下 |
| 4 | モーダルの `<dialog>` を `AppLayout` に置くか、設定のページごとに置くか | 設定のページごと（他の画面に影響しない） |
| 5 | 背景のクリックでモーダルを閉じるか | 閉じない（`Esc` と「キャンセル」「閉じる」だけ。入力途中の消失を防ぐ） |
| 6 | Lucide の `LICENSE` の置き場所と、`THIRD_PARTY_NOTICES.md` への載せ方 | §2 の案（全文を `atoms/icons/LICENSE` に置き、スクリプトの「同梱した素材」の節で載せる） |
