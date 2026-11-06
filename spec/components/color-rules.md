# コンポーネント仕様: 色の決まり（用途 → 許可するトークンのクラス）

> **状態: 実装済み。** Issue #867（用途ごとの許可クラス）→ Issue #1036（Ousiass 共通デザインガイドのトークンへ移行）。画面ごとに色のトーンがばらつくのを止めるため、用途ごとの許可クラスを 1 か所に決める。実装は `backend/internal/web/a11y/color_allowlist_test.go` がこの表を機械的に守らせる。

[shared-ui-parts.md](shared-ui-parts.md) §1「見た目の決まり」は、ページ・カード・補助の文字・フォーカスの輪など、一部の用途の色を決めている。この文書はそれを**網羅した**ものにする（§1 から参照する）。[overview.md](overview.md) §3 のコンポーネントはすべてこの表に従う。

## 1. 方針

- **色は Ousiass 共通デザインガイド（brand-guide `622ea73`）のトークンだけ。** `static/src/css/tokens.css` が唯一の「値を書く場所」で、`--ink` `--bone` `--mute` `--surface` `--border` `--accent` `--accent-soft` `--focus` と状態色 `--success` `--warning` `--danger` `--info` を、ライト（`:root`）とダーク（`:root[data-theme="dark"]`）の両方で定義する。Tailwind 4 の `@theme inline` が `--color-*: initial` で既定のパレットを消し、トークンを `bg-bone` `text-ink` `border-border` のクラスにする。**`bg-neutral-900` や `text-indigo-300` のような既定パレットのクラスは、コンパイルされず、テストも落とす。**
- **ライト / ダークの切り替え**: `<html data-theme="light|dark">`（左メニューのテーマボタンが `mam_theme` クッキーと `data-theme` を書き、サーバーが次のページを同じテーマで描く。`backend/internal/web/themestate`）。クッキーが無いときは OS の設定（`prefers-color-scheme`）に従う。クラスに `dark:` は付けない（トークンが切り替わる）。
- 意味は色相でなく**トークン**で決める: **危険 = `danger`・警告 = `warning`・成功 = `success`・情報／進行中 = `info`・主アクション／リンク／選択 = `accent`（インク色）・それ以外 = `ink` / `mute` / `border` / `surface` / `bone`**。状態色は彩度を落とした無彩色寄りで、色だけで意味を伝えない（文言とアイコン `atoms.Icon` を添える）。
- **同じ意味に新しいクラスや `/opacity` を足さない。** 枠は `-/60`、薄い面は `/10`（弱い）か `/20`（バッジのような塗り）。足したいときは、画面のクラスを変えるのでなく、この文書の表を変える（テストの `colorAllowlist` も同じ版で変える）。
- プリズム（`--prism-*`）はロゴ・ヒーロー・キービジュアル専用で、UI のクラスに使わない。フォーカスだけが `--focus`（プリズムの blue を薄く借りる）を使う。
- コントラストは `a11y/contrast_test.go`（補助の文字に `text-neutral-500` を使わない）と `a11y/field_border_test.go`（入力欄の枠は、ライト・ダークのどちらでも面に対して 3:1 以上）が守る。
- 共通部品が表の正: 色は `atoms.Badge`（`BadgeNeutral`・`BadgeInfo`・`BadgeWarn`・`BadgeOK`・`BadgeError`）と `atoms.Button`（`TonePrimary`・`ToneDanger`・`ToneNeutral`）から決まる。画面・molecules・organisms・Lit は、同じ意味ならこれと同じクラスを使う。
- **フォーカスの輪**は色のクラスでなく、`focus-ring`（`tokens.css` の `@utility`。`outline: 2px solid var(--focus)` + `outline-offset: 2px`）を `focus-visible:focus-ring`・`peer-focus-visible:focus-ring` のように付ける。`ring-*` / `ring-offset-*` の影の輪は使わない。

## 2. 検査の範囲

`TestSources_ColorUtilitiesAreInTheAllowlist`（`backend/internal/web/a11y/color_allowlist_test.go`）が、次のファイルの色のクラスを 1 行ずつ調べ、§3・§4 に無いクラスがあれば `file:line` とクラス名を出して落ちる。

- `backend/internal/web/**/*.templ`・`backend/internal/web/**/*.go`（`*_test.go`・生成された `*_templ.go`・`testdata/` を除く）。`format.go` や `settings_format.go` のように Go の文字列でクラスを返すものを含む。
- `static/src/**/*.ts`・`static/src/**/*.css`（`*.test.ts`・`*.wtest.ts`・`testdata/`・`dist/` を除く）。
- 色のクラス: トークンの色（`bg-` `text-` `border-` `border-t-` `divide-` `outline-` `fill-` `stroke-` `accent-` など）、パレットの色（あれば必ず落ちる）、`white` / `black` / `transparent` / `current`、任意の値の色（`bg-[#123456]`・`text-[rgb(…)]`）。`text-[10px]` のような大きさは色でない。**バリアント付き**（`hover:`・`focus-visible:`・`backdrop:`・`has-[…]:` など）は、バリアントまで含めた形で §3・§4 と一致させる。
- hex（6・8 桁）・`rgb(`・`hsl(`・`oklch(` などの直書きも落とす（コメントの Issue 番号 `#865` は 3 桁なので対象外）。**値を書いてよいのは `static/src/css/tokens.css` だけ。**

`TestColorRulesSpec_MirrorsTheTests` が、§3・§4 の表とテストの一覧が 1 クラスずつ一致することを確かめる（表だけ・テストだけを変えると落ちる）。

## 3. 用途 → 許可するクラス

<!-- color-allowlist:begin -->

| 用途 | 許可するクラス | 根拠の目安 |
|------|---------------|-----------|
| ページ背景・本文 | `bg-bone` / `text-ink` | §1・`AppLayout` |
| トースト | `bg-ink` / `text-bone` | O3（インク地にボーン字のピル。`atoms.Toast`） |
| カード・モーダルの面 | `bg-surface` / `border-border` | §1 |
| 補助・説明の文字 | `text-mute`（本文の長文には使わない） | §1・`contrast_test` |
| 枠・区切り | `border-border` / `divide-border` | §1・一覧 |
| 据え置きの面（現在地・コード欄・チップ） | `bg-accent-soft` | ガイドの `--accent-soft` |
| ホバー面 | `hover:bg-accent-soft` / `hover:border-mute` / `peer-not-checked:hover:border-mute` / `hover:bg-accent-soft/60` | §1・`peer-not-checked:` は選ぶカード（選んだカードの枠をホバーで灰色に戻さない） |
| ホバーの文字 | `hover:text-ink` | §1 |
| 主アクション（実ボタン） | `bg-accent` / `hover:bg-accent/85` / `text-bone` | `TonePrimary` |
| リンク・アイコン強調 | `text-accent` / `decoration-mute`（リンクは `underline` を添える） | §1・`Icon` 周り |
| 選択・現在地 | `border-accent` / `has-[:checked]:border-accent` / `peer-checked:border-accent` / `peer-checked:bg-accent-soft` / `hover:border-accent` / `border-t-accent` | 選ぶカード・ナビ・スピナーの回る弧 |
| 危険 | 文字 `text-danger`、枠/面 `border-danger/60`・`bg-danger/10`・`bg-danger/20`（Badge）・`hover:bg-danger/10`（`ToneDanger` のホバー） | `ToneDanger`・`BadgeError` |
| 警告 | 文字 `text-warning`、枠/面 `border-warning/60`・`bg-warning/10`・`bg-warning/20`（Badge） | `BadgeWarn` |
| 成功 | 文字 `text-success`、枠/面 `border-success/60`・`bg-success/10`・`bg-success/20`（Badge） | `BadgeOK` |
| 情報・進行中 | 文字 `text-info`、枠/面 `border-info/60`・`bg-info/10`・`bg-info/20`（Badge） | `BadgeInfo` |

### 3.1 意味の色の枠・薄い面

警告・成功・情報・危険は、**枠は `-/60`、薄い面は `/10`、バッジのような塗りは `/20`**（表の読み。新しい色・不透明度は足さない）。注意書きの箱（枠だけ）・トースト・接続の通知・ステータスの帯がこれを使う。

<!-- color-allowlist:end -->

### 3.2 Issue #867 → #1036 の対応（旧パレットのクラスの寄せ先）

| 旧（既定パレット） | 新（トークン） |
|------|----------|
| `bg-neutral-950` | `bg-bone` |
| `bg-neutral-900` | `bg-surface` |
| `bg-neutral-800` / `bg-neutral-700` / `bg-indigo-500/20` | `bg-accent-soft` |
| `text-neutral-100` / `-200` / `-300` | `text-ink` |
| `text-neutral-400` | `text-mute` |
| `border-neutral-800` / `-700` / `-600` | `border-border` |
| `border-neutral-500`（入力欄） | `border-mute` |
| `bg-indigo-500 text-white hover:bg-indigo-400` | `bg-accent text-bone hover:bg-accent/85` |
| `text-indigo-300` / `border-indigo-400` | `text-accent` / `border-accent` |
| `text-red-300` など（`red` / `amber` / `emerald` / `sky`） | `text-danger` / `text-warning` / `text-success` / `text-info` |
| `focus-visible:ring-2 focus-visible:ring-indigo-400 focus-visible:ring-offset-*` | `focus-visible:focus-ring` |
| `shadow-indigo-400/40` などの光る縁取り | 外す（影はモーダルだけ。選択は枠 `border-accent` と面 `bg-accent-soft`） |

## 4. 例外（許可リストに無いが、意味が違うので残すクラス）

表に行が無く、寄せると意味か見た目か a11y が壊れるものだけを、理由とともに列挙する。**ここに無い色のクラスは使えない。** 増やすときは理由を書いてこの表とテストの `colorExceptions` を同じ版で変える。

<!-- color-exceptions:begin -->

| クラス | 使う場所 | 理由 |
|--------|---------|------|
| `border-mute` | 入力欄（`atoms.fieldClass`）と、Lit が出す `<input>`・`<select>`・`<textarea>`（Take の指示欄・タイムラインのクリップ） | 入力欄の枠は、面に対して **3:1 以上**（WCAG 1.4.11）が必要で、`--border`（罫線用の薄い線）では足りない。`--mute` はライト・ダークのどちらでも 3:1 を超える（`a11y/field_border_test.go`） |
| `bg-black` | 動画・画像のプレビューの背景（`<video>`・`<img>` の余白）と動画プレイヤーの舞台 | 映像のレターボックスは黒でないと映像が浮く（どちらのテーマでも黒）。モーダルの背後（スクラム）はクラスでなく `tokens.css` の `--scrim`（`static/src/css/motion.css` が `dialog::backdrop` に当てる） |
| `border-transparent` | 現在地でないナビ・ステップの枠（`border-l-2` の幅を取っておく） | 枠の幅を保って現在地の切り替えで文字がずれないようにする（色はない） |
| `accent-danger`・`accent-warning`・`accent-success` | 予算の `<progress>`（危険・警告・成功） | ブラウザが描くメーターの塗りの色（`accent-*`）で、表の文字用・面用のクラスは使えない。意味は状態色（危険 = danger・警告 = warning・成功 = success）を表と揃える。推定コストと上限の文字を添える（色だけで伝えない） |
| `bg-danger/70`・`bg-warning/70`・`bg-success/70`・`bg-info/70` | 動画の上・Take のレーンの上に重ねる塗り（Judge の指摘の位置・選んだ区間・ドラッグ中の区間・読み込みの進捗） | 映像・レーンが透けて見える**重ね塗り**で、`/20`（バッジ）では映像の上で見えない。意味は表と同じ（danger = 重大・warning = 中・info = 軽い／ドラッグ中・success = 選んだ） |

<!-- color-exceptions:end -->

### 4.1 スライダー（`input[type=range]`。Issue #955）

`::-webkit-slider-*` / `::-moz-range-*` は Tailwind のクラスで書けないので、スライダーだけは CSS（`static/src/css/slider.css`）で描く。**色はトークンのカスタムプロパティ（`var(--accent)` など）だけ**で、hex・`rgb(` の直書きは §2 のテストが落とす。`--thumb`・`--rail`・`--fill` などのカスタムプロパティは、部品の中の大きさ・塗りの割合で、デザイントークンではない。

| 部位・状態 | 色（トークン） |
|-----------|--------------------|
| 塗り（選んだ値までのレール） | `--accent` |
| 残りのレール | `--accent-soft` |
| つまみ | 面 `--surface`・縁 `--accent` |
| ホバー | つまみの面が `--accent-soft`（色・背景だけ。光彩なし） |
| ドラッグ中（`:active`） | つまみの面が `--accent` |
| キーボードのフォーカス（`:focus-visible`） | つまみの外側に `--bone` の隙間 2px と `--focus` の輪 2px（ほかの部品の `focus-ring` と同じ値） |
| 無効（`:disabled`） | 塗り `--mute`・残り `--accent-soft`・つまみ `--accent-soft`（縁 `--mute`）。ホバー・ドラッグの変化なし |
| Windows のハイコントラスト（`forced-colors`） | システムの色（`Highlight`・`GrayText`・`ButtonFace`・`ButtonText`） |

**WebKit（macOS の WKWebView）には、塗りの部分を指す疑似要素が無い**ので、レールは `linear-gradient(to right, 塗り var(--stop), 残り var(--stop))` で描く。`--fill`（0〜1）は値から `static/src/components/slider/slider.ts` が書く（読み込み・HTMX の差し替え・`input` のたび）。Lit が描くスライダーは `sliderStyle()`（`slider-model.ts`）で `style` に書く。`appearance: none` でブラウザの標準の見た目に頼らないので、Chromium（Windows の WebView2）と WebKit で同じ形になる。値・範囲・刻み・矢印キー・ドラッグは `<input type=range>` のままで、変えない。値の表示は各画面の `<output>`（スライダーの横か上）が持つ。

- **プロンプトの重み調整（`mam-prompt-weight`・`mam-weight-note`。Issue #1046）**: バーの色は上の表の既存のクラスだけ（面 `bg-surface`・枠 `border-border`・文字 `text-ink` / `text-mute`・押せる所のホバー `hover:bg-accent-soft`・モデルの注 `text-warning`）。新しいクラスを足さない。用途ごとの決まり（W1〜W8）は [shared-ui-parts.md](shared-ui-parts.md) §1「プロンプトの重み調整」。

## 5. #865・#866 の UI もこの表に従う

#865（モデルマネージャーのモデルのカード。`organisms/modelmgr/catalog.templ`・`settingsui.Card`）と #866（「?」ヘルプ。`molecules/help`・ポップオーバーのパネル・`atoms.IconHelp`・`SettingsSection` のヘルプ）で足した UI は、この表に従う。カードの面は `bg-surface border-border`、状態は `Badge`、同意の注意は `text-warning`、「?」ボタンは `border-border text-ink`・ホバー `hover:border-mute`・フォーカス輪 `focus-visible:focus-ring`。**これから足す画面・部品も同じ**（表に無い色が要るなら §1 の方針に従って表を変える）。

## 6. 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-28 | 新規作成。用途 → 許可クラスの表（§3。Issue の案をオーナーが 2026-10-28 に確認してそのまま確定）、意味の色の枠・薄い面（§3.1）、旧クラスの寄せ先（§3.2）、例外（§4）、#865・#866 も従うこと（§5）、検査の範囲（§2）を決めた。UI のクラスをこの表に寄せ、`a11y/color_allowlist_test.go` で許可リスト外を落とすようにした | Issue #867 |
| 1.1 | 2026-11-01 | §4 の「選択」の行に、生成テストのモデルのカードが使う `peer-checked:border-indigo-400`・`peer-checked:bg-indigo-500/20`・`peer-checked:shadow-indigo-400/40` を足した（チェックボックスを見えなくして、カード全体を選択の枠と光る縁取りで示す） | Issue #906 |
| 1.2 | 2026-11-02 | §3 のフォーカス輪に `has-[[data-card-open]:focus-visible]:ring-indigo-400`、§4 の「選択」の行に `hover:border-indigo-400`・`hover:shadow-indigo-400/40` を足した（モデルマネージャーのカードは、タップするとモーダルが開く「開くカード」。ホバーとキーボードのフォーカスで、選ぶカードと同じ枠・光る縁取り・輪を使う。新しい色は足していない） | Issue #954 |
| 1.3 | 2026-11-02 | §4.1 を足した（スライダーの色・状態。`static/src/css/slider.css`）。§1 の `app.css` の書き方に `slider.css` の import を足した。新しい色相・シェードは足していない（表の indigo・neutral・white だけ） | Issue #955 |
| 2.0 | 2026-11-05 | 色を Ousiass 共通デザインガイド（brand-guide `622ea73`）のトークンへ移した。§1 を「トークンだけ。`static/src/css/tokens.css` が値を書く唯一の場所」に改め、§3・§4 の許可クラスを `bg-bone` `text-ink` `border-border` などへ、フォーカスの輪を `focus-ring` へ、§4.1 のスライダーをトークンの変数へ置き換えた。ライト / ダークの切り替え（`data-theme`）を足した。テストの許可表・入力欄の枠のコントラスト検査も同じ版で改めた | Issue #1036 |
| 2.1 | 2026-11-06 | §3 の「ホバー面」に `peer-not-checked:hover:border-mute` を足した（選ぶカードのホバーの枠は、選んでいないカードだけ。`hover:border-mute` は `peer-checked:border-accent` より後に描かれて、選んだカードの枠をホバーで灰色に戻していた）。新しい色は足していない | Issue #1061 |
| 2.2 | 2026-11-06 | §5 に、プロンプトの重み調整（`mam-prompt-weight`）が既存のクラスだけで描かれることと、決まりの置き場所（shared-ui-parts.md §1）を足した。許可クラスの表（§3・§4）は変えていない | Issue #1046 |
