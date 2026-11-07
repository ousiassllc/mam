# 画面仕様: 左側メニューと画面の枠（Issue #915）

> **状態: 実装済み（Issue #915。2026-11-02）。** 「シンプルだけどかっこいい」（悠太さん 2026-11-01）。色と見た目の方向性は既存のテーマ（ダーク、`neutral-950` の面と `indigo` の強調）と [color-rules.md](color-rules.md) のまま変えない。**実機（Windows の同梱デスクトップアプリ・macOS）での見た目は未確認**（ローカルの Chromium で 1280×800・1440×900・375 幅だけ確かめた）。

[overview.md](overview.md) §3 の `layout`（`AppLayout`）に、全画面で共通の**固定のメニュー**を足し、`pages/` の各画面の枠（余白・見出し・幅）をそろえる。#954（モデルマネージャーのカードと詳細モーダル）はこの枠の上に載る。

## 1. 決めたこと

| # | 決定 | 理由 |
|---|------|------|
| 1 | メニューは `AppLayout` の中にあり、すべてのページが持つ。初回のガイド（`onboarding_page`）だけ持たない（`sidenav.Off()`） | 初回の流れを邪魔しない。ほかの画面から外へ出る道が常にある |
| 2 | 項目は主な画面 9 つ（プロジェクト・生成テスト・モデルマネージャー・ジョブ・ワークフロー・ノード・レビュー・制作指標・設定）。Project の画面（チャット・Brief・Scene / Shot・編集 / 書き出し・設定 / コスト・制作指標・Branch・World State・Reference Library・Notes・レビュー・Series）では、その下に同じ形で Project の項目を足す | 「補助ビュー」のメニューをチャットから移した。`/projects/:id/references` が「リファレンス」。#980 で「ジョブ」（[jobs-screen.md](jobs-screen.md)）を足して 8 項目から 9 項目にした |
| 3 | 項目のリンク先は `paths` の関数だけで作る（`organisms/sidenav/menu.go`）。存在しない画面へのリンクは作らない | `web/sidenavtest/sidenav_test.go` が全項目を実際のルーターで GET し、200 とそのページの `aria-current="page"` を確かめる（Wails も同じルーターを使う） |
| 4 | たたむ状態は **cookie `mam_nav`**（`folded` / `open`）。サーバーが読み、最初のバイトからたたんだ形で描く | `localStorage` は Wails がループバックの別ポートで開くと共有されず、ページ描画の後に畳むと一瞬ちらつく。cookie は同じホストならポートを超えて共有される。HttpOnly にしない（`sidenav.ts` が書く）。`SameSite=Strict`、1 年 |
| 5 | 幅 768 px 未満は、たたむ状態にかかわらずアイコンだけ（ボタンも出さない） | 狭い窓で本文を広く使う |
| 6 | 色・文字サイズは既存のトークンだけ（新しい生の色を足さない） | #867 |
| 7 | 画面上の文字は増やさない。ラベルは既存の画面名（短い語）。メニューの名前（`メニュー`）・たたむ/開くボタンの名前・「本文へ」は読み上げ用で画面には出ない | Issue の受け入れ条件 |

## 2. 構成

```
body
├ a "本文へ"            … 見えない。フォーカスが来たときだけ出る。#content へ飛ぶ（メニューを飛ばす）
└ div.flex
   ├ nav#sidenav[aria-label="メニュー"][data-folded]   … sticky・画面の高さ・中だけスクロール
   │   ├ "mam" ＋ たたむ/開くボタン（aria-expanded・aria-controls・名前が状態で変わる）
   │   ├ ul#sidenav-screens   … 主な画面
   │   ├ （Project の画面だけ）Project 名（見出し代わり）＋ ul#sidenav-views[aria-labelledby=sidenav-project]
   │   └ （モデルマネージャーの概要と分類ページだけ。#1101）見出し「モデルマネージャー」p#sidenav-mm ＋ ul#sidenav-mm-views[aria-labelledby=sidenav-mm]（「概要」と分類の項目。順・名前・アイコンは `mmcat.All()`。[model-manager-screen.md](model-manager-screen.md)「分類のサブページ（#1101）」）
   └ div#content           … 各ページの <main>
```

- 項目は `<a>` のリンク。現在の画面は `aria-current="page"`（強調: 左の `accent` の線・`accent-soft` の面・`accent` のアイコン）。**強調は色だけにしない**（太字と左の線）。
- たたんだとき、ラベルは `sr-only`（視覚的にだけ隠し、スクリーンリーダーには読まれる）。マウスのために `title` にラベルを持つ。
- キーボード: Tab でリンクを順に、Enter で開く。たたむボタンは Enter / Space。フォーカスの輪は共通の `focus-ring`（`--focus`）（[shared-ui-parts.md](shared-ui-parts.md) §1）。メニューのランドマークは 1 つ（`<nav aria-label="メニュー">`）。Project の一覧は Project 名で名付けた `<ul>`。
- Series の画面は Project ではないので、メニューは「プロジェクト」が現在。Project が Series の Episode のとき（`Project.SeriesID` がある）は、Project の項目の最後に「Series」（その Series のページ）を足す。

## 3. ページの枠（`molecules/pagehead`）

どの画面も `pagehead.Main(幅)` と `pagehead.Header(題, 副題, 説明)` で書く。

| 部品 | 内容 |
|------|------|
| `Main` | `<main class="mx-auto flex w-full max-w-… flex-col gap-6 p-4 md:p-8">`。幅は `Narrow`（3xl）・`Medium`（4xl）・`Wide`（5xl）・`Wider`（6xl）・`Full`（7xl）。ブロックの間隔は 1 か所（`gap-6`）。狭い窓では余白 1rem |
| `Header` | `<h1 class="text-xl font-semibold">`（全画面で 1 つ・同じ大きさ）。副題は `text-sm text-mute`（Project 名など）。説明があれば題の横の「?」（`help.Row`、[shared-ui-parts.md](shared-ui-parts.md) §9） |

- **戻るリンクは持たない**。メニューが戻り道（`atoms.BackLink` は、メニューが持たない上位だけ: レビューの 1 件 → 一覧、Shot の Inspector → Scene / Shot、Series の Notes → Series）。
- 説明の段落は「?」ヘルプへ移した（ワークフロー・ノード・モデルマネージャー・設定・Project の設定・レビュー・Notes）。画面の文字は減っている。

## 4. 選ぶ操作はカード（`molecules/pick`）

「選ぶ」操作（ラジオ・複数選ぶチェック）は、タップで枠が光るカード `pick.Card` にした（#906 の方針。生成テストのモデルは既にカードで、光らせ方を `pick.Glow` に共通化）。下のコントロールは本物の `radio` / `checkbox`（`sr-only`）なので、キーボード（矢印・Space）・フォーム送信・スクリーンリーダー（「ラジオボタン、選択済み」）はそのまま。選択は枠・光・面の色（色だけにしない: ラジオは同じ名前の中で 1 つ）。

| 画面 | 部品 |
|------|------|
| 生成テスト | 画像 / 動画 |
| チャットの下 | MODE（AUTO / DIRECTOR）・確認レベルの検査点（CUSTOM CHECK） |
| Project の設定 | Production Template |
| レビュー | 判定（承認 / 修正依頼） |
| 設定 | 音声のプロバイダー・ローカルモデルのテンプレートと画像/動画・ChatGPT のログイン方法 |

**チェックボックスのまま残すもの**（「選ぶ」ではなくオン/オフの設定や、同意）: 生成テストの「英語に訳して送る」・年齢確認やライセンスの同意・カードの選択（#932 の履歴は既にカード）・各設定のオン/オフ。理由: 1 つの設定の ON/OFF は「選ぶ」操作ではなく、カードにすると何の ON/OFF かが失われる。

## 5. 画面ごとの整理

| 画面 | やったこと |
|------|-----------|
| 全画面 | 左メニュー・同じ余白・同じ h1・戻るリンクをメニューへ |
| プロジェクト（`/`） | h1 `mam` ＋副題。ツールのカードは外した（#978。左メニューと行き先が同じ。状態の扱いは [home-screen.md](home-screen.md) §3） |
| 生成テスト | 説明は「?」。4 つのページのリンクはタブの形（現在がわかる） |
| モデルマネージャー | メニュー・余白・見出しだけ。**モデルを並べる部分は変えていない**（#954 が扱う）。#908（導入ボタンの後のスクロール）にも触れていない |
| ジョブ | 裏で動く作業の一覧（#980。[jobs-screen.md](jobs-screen.md)）。メニューの項目はモデルマネージャーの次 |
| 設定 | 見出しと「?」、左の分類ナビ（画面内ナビ）はそのまま |
| チャット | ヘッダーの「補助ビュー」とプロジェクト一覧への戻りをメニューへ移した |

## 6. 試験

| 試験 | 内容 |
|------|------|
| `web/sidenavtest/sidenav_test.go` | 全項目が 200・自分の画面で `aria-current`・ランドマーク 1 つ・順序・エラーページ |
| `web/handler/workflows/sidenav_test.go` | ワークフローとノード（ワークフローのサービスがあるときだけ登録されるルート） |
| `organisms/sidenav/nav_test.go`・`navstate/navstate_test.go`・`layout/app_layout_test.go` | 部品の形・cookie の読み取り・スキップリンクの順序 |
| `static/src/components/sidenav/*` | cookie の形（vitest）・ボタンの動作（web-test-runner） |
| `static/src/components/layout/layout.wtest.ts` | 375・1280・1440 px で横に溢れない・たたんだ幅（56 px）・開いた幅（224 px） |

## 7. 未確認

- Windows 実機の同梱デスクトップアプリ（Wails の WebView2）と macOS（WKWebView）での見た目・cookie の保持。
- 画面の読み上げ（NVDA・VoiceOver）での実際の聞こえ方。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-02 | 新規作成（実装済み） | Issue #915 |
| 1.1 | 2026-11-03 | 主な画面を 8 つから 9 つ（「ジョブ」を足した）にし、§5 に行を足した。左メニューの見本（`layout-sidenav.html`）を作り直した | Issue #980 |
| 1.2 | 2026-11-06 | §2 の構成に、モデルマネージャーの概要と分類ページだけに出る 2 階層目（`ul#sidenav-mm-views`）を足した。状態は仕様のみ（未実装） | Issue #1101 |
| 1.3 | 2026-11-07 | モデルマネージャーの 2 階層目（`ul#sidenav-mm-views`。概要と `mmcat.All()` の分類）を実装した（`sidenav.InModelManager`）。画面の項目は、概要では `aria-current="page"`、分類ページでは `aria-current="true"`。各リストに `data-testid`（`sidenav-screens`・`sidenav-views`・`sidenav-mm-views`）を付けた | Issue #1101 |
