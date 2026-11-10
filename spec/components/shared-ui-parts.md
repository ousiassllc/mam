# コンポーネント仕様: 共通の画面部品（カード・アイコン・モーダル・一覧の行・「?」ヘルプ）

> **状態: 一部実装済み。** Issue #846（トップ画面のカード）と #847（設定画面のサブページ化）が共有する部品の見た目と契約。**`atoms.Icon`（#846 の 8 個。§2）と `molecules.ToolCard`（`StatusURL` の形。§3）は #846 で、モーダル（§4）・一覧の行のカード `SettingsCard`（§5。実体は `settingsui.CardItem`）・#847 のアイコン 7 個（`IconKey`・`IconText`・`IconMedia`・`IconLocal`・`IconPlug`・`IconUpdate`・`IconClose`）は #847 の段階 ①・②-1・②-2 で実装済み**（2026-10-25）。表 `SettingsTable`・`TableRowItem`（§5。`settingsui.SettingsTable` ほか）と `IconCopy`（MCP のトークンのコピーに使う）は段階 ②-3 で実装済み（2026-10-25）。オーナーは §8 の質問を推奨する既定で決定した。

[overview.md](overview.md) §3 のコンポーネントの階層に、次の部品を足す案。使う画面は [home-screen.md](home-screen.md)（`tool_card`・`atoms.Icon`）と [settings-screen.md](settings-screen.md)・[settings-screen-categories.md](settings-screen-categories.md)（すべて）。`atoms.Icon`・`molecules.ToolCard` は先に出る #846 が作る（#846 決定事項 1）。**モーダルは #847 で作る**（2 つの Issue の文面が食い違っていたが、オーナーが決定。§6.1）。一覧の行・サイドナビは #847 が作る。

## 1. 見た目の決まり（色は Ousiass 共通デザインガイドのトークンだけ）

**用途ごとの許可クラスの網羅した表（と例外・検査）は [color-rules.md](color-rules.md)（#867 → #1036 でガイドのトークンへ移行）。この節の表はその一部で、食い違うときは color-rules.md が正。**

`static/src/css/app.css` は `@import "tailwindcss"`・トークン（`tokens.css`。ライト / ダークの値と Tailwind の `@theme`、`focus-ring`）・スライダーの CSS（`slider.css`。下の表）と `@source` だけ。色は `tokens.css` のトークン（ガイドの値。独自のパレットは作らない）だけを使い、既定のパレット（`neutral-*` `indigo-*` など）は使えない。

| 用途 | クラス | 備考 |
|------|--------|------|
| ページの背景・本文 | `bg-bone text-ink`（`AppLayout`） | 変えない |
| カード・モーダルの面 | `bg-surface border border-border rounded-lg`、内側 `p-4` | 既存の `GenerationTest` の選択カードが `bg-surface`、`ProjectList` が `border-border` |
| 補助の文字 | `text-mute`（補助・キャプション用。本文の長文には使わない） | 旧パレットの `text-neutral-500` は使わない（`a11y/contrast_test.go`） |
| 主要な文字 | `text-ink` | |
| アイコンの色 | `text-accent`（インク色） | 飾りなので 1.4.11 の対象外だが、見える色にしておく |
| フォーカスの輪 | `focus-visible:focus-ring`（`tokens.css` の `@utility`。`outline: 2px solid var(--focus)` + `outline-offset: 2px`。`--focus` はライト `#4a72d0`・ダーク `#60a5fa`） | `peer-focus-visible:focus-ring`（チャットの添付）も同じ。輪は `outline` なので Windows のハイコントラスト（`forced-colors`）でも描かれ、`ring-*` の影の輪は使わない。輪を出さない見出しなどは `focus-visible:outline-none` |
| ホバー | `hover:border-mute hover:bg-accent-soft/60` | 見た目の補助。ホバーだけに意味を持たせない |
| 動き | 色・背景の `transition-colors`（`--motion-base` 180ms・`--ease-out`）と `motion-reduce:transition-none`。拡大・移動・アニメーションは使わない。例外は下の「動き（#1040）」 | #797（`prefers-reduced-motion`）。スピナーは処理中表示なので、reduced-motion でも回す（#1040） |
| 主ボタンの `bg-accent` | `atoms.TonePrimary` の実ボタン専用 | カードの見た目に使わない（カードはリンクであってボタンではない） |
| 状態 | `atoms.Badge`（neutral / info / warn / ok / error）と文字。**色だけで伝えない**（#790・#795） | §2 の対応表 |
| カードの枠 | `border-border` は飾り。カードはタイトルの文字とフォーカスの輪で見分ける | 入力欄の枠のコントラスト（`field_border_test`）の対象ではない |
| スライダー（`input[type=range]`。#955） | アプリの**すべて**のスライダーが `static/src/css/slider.css` の共通の見た目（レール 6px・つまみ 16px の丸。塗り `--accent`・残り `--accent-soft`・つまみ `--surface` に `--accent` の縁）。ホバー（つまみの面が `--accent-soft`）・ドラッグ中（つまみが `--accent`）・`focus-visible`（`--focus` の輪）・無効（`--mute`）を持つ。色は [color-rules.md](color-rules.md) §4.1。値は `<output>` を横（参照画像・LoRA の強さ）か上（プロンプトマッチの目標）に置く（動画の再生位置は、下のタイムコードが値） | 要素は `<input type=range>` のまま（矢印キー・ドラッグ・範囲・刻み・初期値は変えない）。WebKit にレールの塗りの疑似要素が無いので、塗りは `--fill`（`components/slider/slider.ts` が値から書く。Lit は `sliderStyle()`）と `linear-gradient` で描く。新しいスライダーは class で見た目を足さず、幅・並べ方だけ書く |

### 文字（Issue #1037）

文字は Ousiass 共通デザインガイド（T1〜T5）に従う。値は `static/src/css/tokens.css` の 1 か所に書き、画面は名前だけを使う。検査は `a11y/typography_test.go`。

| 項目 | 決まり |
|------|--------|
| フォント（T1） | 英字・UI・日本語は `font-sans`（Sora → Noto Sans JP）、コードと桁を揃える数値は `font-mono`（JetBrains Mono）。いずれも SIL OFL。`static/fonts.mjs` が `@fontsource-variable/*` から `dist/fonts.css` と `dist/fonts/*.woff2` を作り、レイアウトが `paths.StaticFontsCSS` を読む（ネットワークからは取らない）。ライセンス全文は `THIRD_PARTY_NOTICES.md` |
| サイズ（T3） | `text-xs` 12 · `text-sm` 14 · `text-base` 16 · `text-lg` 18 · `text-xl` 24（h3）· `text-2xl` 32（h2）· `text-3xl` 40（h1）。Tailwind の既定の大きさ（20・30・36）は無効。本文（文章として読ませる `<p>`）は `text-sm` 以上、`text-xs` は注記・バッジ・表の見出し |
| 行間（T4） | サイズごとに `xs` 1.6 · `sm` 1.7 · `base` 1.8 · `lg` 1.7 · `xl` 1.5 · `2xl` 1.4 · `3xl` 1.3。`body` は 16px・1.8 |
| ウェイト（T2） | Regular（400）と SemiBold（600）の 2 段だけ。SemiBold は見出し（`h1`〜`h4`）・ボタン・`<summary>`・表の見出し行だけ。ナビ・リンク・ラベル・数値には付けない（現在地は色と背景で示す）。`font-medium` は使わない |
| 数値（T5） | 桁を揃える数値（金額・容量）は `font-mono tabular-nums`、表の数値列は右寄せ。表の見出し行は `text-xs font-semibold text-mute` |

### 形・余白・線（Issue #1038）

形・余白・線・影・レイヤー・動きは Ousiass 共通デザインガイド（S1〜S3・O4・M1）に従う。値は `static/src/css/tokens.css` の `@theme` に書き、Tailwind の既定の値は無効（`--radius-*`・`--shadow-*`・`--text-*` を `initial` にしている）。検査は `a11y/shape_test.go`。

| 項目 | 決まり |
|------|--------|
| 角丸（S1） | `rounded-sm` 4（タグ・チェック・バッジの角）· `rounded-md` 8（入力・ボタン・カードの中の入れ子の面）· `rounded-lg` 12（カード）· `rounded-xl` 16（モーダル `<dialog>`）· `rounded-full`（ピル・アバター）。素の `rounded` は使わない |
| 余白（S2） | 4px 基準（`1`=4・`2`=8・`3`=12・`4`=16・`6`=24 …）。`.5` の値（`gap-1.5` など）は使わない。メニュー項目は高さ 40（`min-h-10`）・項目間 8（`gap-2`）、ボタンは小 32（`min-h-8`）・標準 40（`min-h-10`） |
| 影（S3） | 影はモーダルの `<dialog>` の `shadow-modal`（`0 1px 2px rgba(0,0,0,.06)`）だけ。カード・パネル・トースト・通知は影なしで、`border border-border` で区切る。ホバーの光る縁取りは使わない（色・背景・枠の色だけ） |
| レイヤー（O4） | `z-dropdown` 100 · `z-sticky` 200 · `z-drawer` 300 · `z-modal` 400 · `z-toast` 500。カードの中で、全面を覆うクリック領域の上に出す部品だけ `z-raised`（部品の中の積み順で、ページのレイヤーではない）。`z-10` などの数字は使わない |
| 動き（M1） | `transition-colors`（既定で `--motion-base` 180ms・`--ease-out`）と `motion-reduce:transition-none`。`transition`（すべての値）・`transition-shadow`・`transition-transform` は使わない |

### 動き（Issue #1040）

動きは Ousiass 共通デザインガイド（M1〜M5）に従う。値は `static/src/css/tokens.css` の `--motion-*` / `--ease-*`、モーションは `static/src/css/motion.css` と `static/src/components/ripple/`。

| 項目 | 決まり |
|------|--------|
| ホバー・フォーカス（M1） | 色・背景だけ、`--motion-base` 180ms（フォーカスは即時〜`--motion-fast` 120ms）。`transition-colors` が既定でこの値を取る。光彩・拡大はしない |
| 処理中表示（M3） | スピナー（`atoms.Spinner`）と更新ボタンの回る矢印は、`prefers-reduced-motion: reduce` でも回す（`motion-reduce:animate-none` を付けない）。移動・スライドの飾りだけを止める（`motion-reduce:transition-none`）。オーバーレイのフェードは reduce で 100ms 以内 |
| リップル（M4） | `data-ripple` の付いたボタン（`atoms.Button` / `SmallButton` / `ButtonLink` / `…For` / `IconButton`・モーダルとトーストの閉じる・左メニューのボタン）の押下で、押した点から円が広がって消える（`--motion-slow` 280ms・`--ease-out`。円は `currentColor` の 20%）。無効なボタンと reduced-motion では出ない |
| 出現・退出（M5） | `<dialog>`・popover・トーストは、出現がフェードイン `--motion-fade-in` 320ms（`--ease-out`）、退出がフェードアウト `--motion-fade-out` 240ms（`--ease-in`）。スクラム（`dialog::backdrop`）は `--scrim` を同じ時間でフェードする。移動は付けない |

### 部品・オーバーレイ（Issue #1041）

オーバーレイと部品は Ousiass 共通デザインガイド（O1〜O4・O7）に従う。

| 項目 | 決まり |
|------|--------|
| スクラムで閉じる（O1） | モーダルの `<dialog>` は、外側（スクラム）のクリックで閉じる（`static/src/components/scrim`。Esc・閉じるボタンも）。欄を変更した（`input` が起きた）ダイアログは、先に確認のモーダル「入力した内容は保存されません。閉じますか？」を出し、入力途中の内容を巻き込んで消さない。スクラムはダイアログと一緒にフェードする（M5） |
| 確認・破壊的操作（O2） | 確認は OS 標準の `confirm()` でなく、中央のモーダル（`modal.Confirm`。`--r-xl`・`shadow-modal`・危険ボタン「実行する」と「キャンセル」）。`hx-confirm` の要素は `htmx:confirm` をこのモーダルが受けてから送る（`static/src/components/confirm`）。`AppLayout` が `#confirm-template` を埋め込む。モデルマネージャーの年齢確認・ライセンス確認もページ内の枠でなく同じ `<dialog>`（`modal.InlineHost`。`showModal()` で開く） |
| トースト（O3） | 「保存しました」のような **1 行の短い成功の文だけ**。画面下中央（下から 24）・高さ 40・ピル・`bg-ink text-bone`・同時に 1 つ（新しいものが前を置き換える）・3 秒で自動消去。閉じるボタンも持たない。サーバーが返す成功は `atoms.ToastOOB`（`#toast-region` の中身を置き換える） |
| インラインアラート（O3） | エラー・業務エラー（403 / 409 / 423・予算超過・LOCK 違反など）・エラー詳細は、ページ上部（`#content` の先頭の `#alert-region`）に `atoms.Alert`（お知らせ / 成功 / 注意 / エラーの 4 種。アイコン＋見出し＋本文）で出す。閉じるまで残る（長い文を読み終えるため。WCAG 2.2.1）。同じ文は重ねない。サーバーは `atoms.ErrorAlertOOB`（`beforeend`）、スクリプトは `lib/alert.ts`（`#alert-template`） |
| 接続の通知（O3） | 接続が切れた通知（`atoms.ConnectionNotice`）は、警告のアイコン＋文言＋ `--surface` の地に `border-warning/60` の枠（スナックバー。影なし）。トーストではない |
| ボタン（O7） | `font-semibold`・`rounded-md`・無効は不透明度 .4・小は高さ 32（`min-h-8`）・標準は 40（`min-h-10`）。種類は主（`TonePrimary`）・サブ（`ToneNeutral`。透明＋ `border-border`）・ゴースト（`ToneGhost`）・危険（`ToneDanger`） |
| 独自ウィジェット（Lit。P3） | `mam-timeline-editor`・`mam-take-comparator`・`mam-mask-paint` は共通ガイドに無い独自ウィジェットで、見た目・状態表示・操作・文言は案件ガイド [lit-widgets-guide.md](lit-widgets-guide.md) が決める（ボタン・入力・バッジ・通知・数値は上の表と同じ部品を使う） |

### 商用利用の可否（Issue #928）

モデルの商用利用の可否は `Badge` の手書きではなく、`atoms.CommercialBadge`（アイコン + 語）・`atoms.CommercialMark`（アイコンだけ）と、それを包む `molecules/commercial`（`Badge`・`Mark`・`Note`・`Filter`）で出す。色は商用OK＝`BadgeOK`、条件つき＝`BadgeWarn`、商用NG＝`BadgeError`、不明＝`BadgeWarn` に枠を足したもの。詳細は [../architecture/model-license.md](../architecture/model-license.md) §4・§5。

### 状態の種類と `Badge` の対応（両 Issue で共通）

| 意味 | `Badge` | 文言の例 | 決まり |
|------|---------|---------|--------|
| 使える・済んでいる | `BadgeOK` | 起動中・ログイン済み・設定済み・接続できた | |
| 件数・中立の情報 | `BadgeNeutral`（0 件も同じ） | 保存 3 件・受け取り 0 件・未導入・停止中・未ログイン・未設定 | 「無い」ことは悪いことではない |
| 進行中・補足 | `BadgeInfo` | 起動しています・確認中・変更あり | |
| 対応が要る | `BadgeWarn` | ComfyUI が必要です・再ログインが必要・キーストアを利用できません | 文言に「必要」「できません」など**理由の言葉**を含める |
| 失敗 | `BadgeError` | 起動に失敗・接続できなかった | 理由は文字で添える |

### プロンプトの重み調整（`mam-prompt-weight`。Issue #1046）

`<mam-prompt-weight>`（Lit。`static/src/components/prompt-weight/`）は、プロンプトの語を選ぶと出る「強める + / 弱める −」のバー。**動き（語を 0.1 刻みで `(語:1.2)` にする・範囲 0.5〜1.5・ComfyUI の読み方・モデルごとの「効く・効かない」）は [gentest-tiers.md](gentest-tiers.md) §4「語のウェイト」が決めている**。ここには、バーを **Ousiass 共通デザインガイドの形で描く**ための、色・状態・操作・キーボード・文言の決まりを置く（モデルごとの注意を出す `mam-weight-note`・「?」の `mam-weight-help` も同じ決まりに従う）。バーは templ の `atoms` を使えない（Lit が描く）ので、同じ意味には `atoms.SmallButton`・`atoms.Badge` と**同じクラス**を書く（[color-rules.md](color-rules.md) §1「共通部品が表の正」）。

| 項目 | 決まり |
|------|--------|
| 面・枠（色。W1） | バーの面は `bg-surface`・枠は `border border-border`・角は `rounded-md`・内側は `p-2`・並びは `gap-2`（S1・S2。`.5` の値は使わない）。**影・光らせる・アクセントの塗りは使わない**（S3。バーは入力欄の補助で、主役ではない）。文字は `text-ink`、範囲・ショートカットの 1 行は `text-mute`、モデルの注は `text-warning`（色だけで伝えない: 注は必ず文で言う。W5） |
| 文字（T1〜T5。W2） | 全体 `text-xs`（補助の部品なので 12px）。ボタンの文字は `font-semibold`。**いまの強さ（`1.2 倍`）は桁を揃える数値なので `font-mono tabular-nums`**（T5）、小数 1 桁（`toFixed(1)`）。ボタンの文言は「強める +」「弱める −」（`−` は U+2212。ハイフンにしない）。数値の後ろに必ず「倍」を付け、単位なしの数字にしない |
| ボタン（O7・M1・M4。W3） | `atoms.SmallButton`（`ToneNeutral`）と同じ: `inline-flex items-center justify-center rounded-md border border-border font-semibold text-ink hover:bg-accent-soft transition-colors motion-reduce:transition-none`、高さは **32 以上**（`min-h-8`）、`data-ripple`（M4）。**押せないときは `aria-disabled="true"`＋不透明度 .4（`aria-disabled:opacity-40`）＋ `aria-disabled:cursor-not-allowed`**。`disabled` 属性にしない（フォーカスを残し、注の理由を読めるようにするため）。フォーカスは `focus-visible:focus-ring`（輪は色のクラスでない。[color-rules.md](color-rules.md) §1） |
| 出る・消える（状態。W4） | **語を選んだときだけ**出て（`hidden` を外す）、選びを外す・フォーカスがバーとプロンプト欄の外へ移る・`Esc` で消える。**消えるときに動かさない**（M5 は `<dialog>`・popover・トーストの決まり。バーはフェードも移動もさせない。レイアウトが動くことの補足は下の「フォーカス」）。ネガティブプロンプトには出さない。動画のフォーム（段が無い）には付けない |
| 状態の見せ方（W5） | 状態は **文で言う**（色だけにしない）: ①強めても弱めても変えられない端（0.5・1.5）は、そのボタンが押せない（W3）＋範囲の 1 行「0.5〜1.5」 ②選ばれたモデルが全部 `unsupported` ならボタンは両方押せず、注は `text-warning` で「（モデル名）では、ウェイト (語:1.3) は効きません。強調したい語は、文で言い換えてください…」（`weight-facts.ts` の `cautionOf`。名前は表示名で、識別子は出さない） ③一部だけ効きが弱いモデルは、その行を注に足す（「名前: 効きが弱いかも」） ④文字数の上限を超えるときは、変えずに「文字数の上限（N 文字）を超えるので、ウェイトは付けられません」を注に出す（`text-warning`）。注は 1 つの領域（`data-testid="gt-weight-note"`）に並べ、複数でも 1 つの `role="status"` の中に入れる。エラーの `atoms.Alert`（O3）にはしない（入力の補助で、業務エラーではない） |
| 操作（W6） | マウス: ボタンを 1 回押すと 0.1 変わる。**押しても、プロンプト欄の選びとフォーカスを失わない**（`mousedown` の既定動作を止める）ので、続けて押せる。押したあとは、変えた部分全体（`(語:1.2)`）が選ばれたまま。元に戻すは、フォーカスがプロンプト欄にあるときはブラウザの「元に戻す」（`Ctrl/⌘+Z`）で 1 回分ずつ（`execCommand("insertText")`）。確認のモーダルは出さない（元に戻せるため。O2 の対象外） |
| キーボード（W7） | **プロンプト欄の中で `Ctrl/⌘ + ↑` が強める、`Ctrl/⌘ + ↓` が弱める**（ComfyUI の画面と同じ。バーの 1 行の説明にもこの順で書く）。`Esc` でバーを消す。ボタンは本物の `<button>`（`Tab` で届き、`Enter`・`Space` で押せる）。**バーの中（ボタンにフォーカスがあるとき）でも `Esc` で消え、フォーカスはプロンプト欄へ戻る**。矢印キー単独は、プロンプト欄の本来の動き（カーソル移動）を奪わない |
| アクセシビリティ（W8） | バーは `role="group"` と `aria-label="選んだ語のウェイト"`。注は `role="status"`（スクリーンリーダーに伝える領域。**バーが現れる前から DOM にあり、中身だけが変わる**ようにする。現れると同時に作った領域は、読み上げられないことがある）。ボタンは `aria-describedby` で注に結ぶ。タッチの的は高さ 32 以上（W3）、WCAG 2.5.8 の 24 を超える。ダーク・ライトとも `tokens.css` のトークンだけで、`prefers-reduced-motion` で変わる動きは無い（動かさないため） |

**実装との差分（2026-11-06 時点。直すのは #1094・#1095。#1046 では実装を変えない）**: 上の表に対し、`mam-prompt-weight.ts`（`BUTTON` の定数とバーの描画）は次の点で違う。

| # | 決まり | いまの実装 | 直し方 |
|---|--------|-----------|--------|
| 1 | W3: 高さ 32 以上（`min-h-8`） | ボタンは `px-2 py-1 text-xs` で、高さは文字の高さ＋余白まで（12px × 行間 1.6 + 8 + 枠 2 ≒ 29px）。`min-h-8` が無い | `BUTTON` に `min-h-8` を足す |
| 2 | W3 / M1: `motion-reduce:transition-none` | `transition-colors` だけで、`prefers-reduced-motion` で止めない | `BUTTON` に `motion-reduce:transition-none` を足す |
| 3 | W3 / M4: `data-ripple` | 付いていない（ほかのボタンは押すと波紋が出る） | ボタンに `data-ripple` を足す（`ripple.ts` は `:disabled, [aria-disabled="true"]` には波紋を出さないので、押せないボタンでも動きは出ない） |
| 4 | W2 / T5: 数値は `font-mono tabular-nums` | `<output>` は `tabular-nums` だけで、`font-mono` が無い | `<output>` に `font-mono` を足す |
| 5 | W7: バーの中の `Esc` | `Esc` を受けるのは区画の `keydown` のうちプロンプト欄が対象のものだけ（`onKey` の `promptBox(event.target)`）。ボタンにフォーカスがあると、`Esc` で消えない | `onKey` の対象にバーの中を加え、消したあとフォーカスを直前のプロンプト欄へ戻す |
| 6 | W8: 注の `role="status"` | 領域（`gt-weight-note`）はバーと一緒に描かれるため、バーが現れた時点で中身がすでにある（読み上げの対象になりにくい） | 領域だけは常に DOM に置き（`hidden` のバーの中に空で持つ）、中身だけを更新する |

差分は 2 件の Issue に分けて起票した: 見た目（1〜4）は #1094、操作・アクセシビリティ（5・6）は #1095（#1046 の受け入れ条件「差分が洗い出されている」を満たす）。`mam-weight-note`（`text-xs text-warning`・`role="status"`）と `mam-weight-help` は、この表のとおりで差分は無い。

## 2. `atoms.Icon`

- 署名の案: `templ Icon(name IconName, class string)`。`IconName` は文字列型の定数（綴りの間違いはコンパイルで落ちる）。未知の名前は描かない。
- 出力: `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" aria-hidden="true" focusable="false" class="{class}">…</svg>`（`stroke-linecap` / `stroke-linejoin` は各形に付く）。`<title>` は付けない。大きさは `class`（既定 `h-5 w-5 shrink-0`。ガイドの 16 / 20 / 24 = `h-4` / `h-5` / `h-6` だけ。スピナーは 20px）。色は `currentColor`（親の `text-*`）。
- アイコンだけで意味を持つボタンは作らない。必要ならボタンに `aria-label`（既存の `IconButton`）。このリポジトリのアイコンはすべて飾りで、意味は文字が持つ。
- 入手元は #846 決定事項 3(a) を #1039 で改めた: **Iconoir（MIT。Ousiass 共通デザインガイド I1）の SVG のパスを `atoms/icon.templ` にコピー**する（npm の依存は足さない。`regular` 版・線 1.5）。`atoms` の行数は約 414 行で、約 16 個で 150 行前後の見込み（1 ファイル 300 行以内に収める）。コピー元の版は `icon.templ` の先頭のコメントに固定する。

| `IconName`（案） | 当初の名前（Lucide。いまの絵は各 `case` のコメントの Iconoir の名前） | 使う場所 | 入れる Issue |
|------------------|---------------|---------|-------------|
| `IconFlask` | `flask-conical` | 単発生成 | #846 |
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
| `IconHelp` | `help-circle`（Iconoir。旧 Lucide `circle-question-mark`） | 「?」ヘルプのボタン（§9） | #866 |
| `IconProjects` | `folder-kanban` | 左メニュー: プロジェクト（`atoms/icon_nav.templ`。行数の上限のため `icon.templ` から分けた） | #915 |
| `IconChat` | `message-square` | 〃 Project のチャット | #915 |
| `IconBrief` | `file-text` | 〃 Brief | #915 |
| `IconEdit` | `scissors` | 〃 編集 / 書き出し | #915 |
| `IconCost` | `sliders-horizontal` | 〃 設定 / コスト | #915 |
| `IconBranch` | `git-branch` | 〃 Branch | #915 |
| `IconWorld` | `globe` | 〃 World State | #915 |
| `IconLibrary` | `library` | 〃 Reference Library | #915 |
| `IconNotes` | `sticky-note` | 〃 Notes | #915 |
| `IconSeries` | `users` | 〃 Series（`sidenav.Views` が SeriesID を持つとき） | #915 |
| `IconPanelClose` | `panel-left-close` | 〃 たたむボタン | #915 |
| `IconPanelOpen` | `panel-left-open` | 〃 開くボタン | #915 |
| `IconJobs` | `list-checks` | 左メニュー: ジョブ | #980 |
| （`IconMedia`・`IconReview`・`IconChart`・`IconSettings` などは左メニューでも再利用） | | Scene / Shot・レビュー・制作指標・設定 | #915 |

- 16 個で、#846 の 8 個 + #847 の 8 個。Issue の「10 個前後」より多いのは、#847 が概要のカードにもアイコンを付けるため（§8 の質問 1。付けないなら #847 は `x` と `copy` の 2 個）。`IconClose`（`x`）はモーダルの部品が使うので、どちらの Issue が持つかはモーダルの担当（§8 の質問 7）に従う。表の「#847」は決定のとおり。
- 上の Lucide の名前は、2026-10-25 に `lucide-icons/lucide` の `main` の `icons/<名前>.svg` が存在することを確かめた（HTTP 200）。コピーする版（タグ）は実装のときに決め、その版に同じ名前があることを確かめる。名前は Lucide の版で変わることがある。
- **ライセンスの扱い（計画）**: Lucide の `LICENSE` は、ISC（Lucide Icons and Contributors）に加えて、Feather 由来のアイコンの一覧と MIT（Cole Bemis）の全文を持つ。上の表では `x`・`chevron-right` が Feather 由来。このため、取り込む版の `LICENSE` の**全文**を `backend/internal/web/atoms/icons/LICENSE`（置き場は案）に置き、`scripts/third-party-notices.sh` が「同梱した素材」の節として `THIRD_PARTY_NOTICES.md` に載せる（今のスクリプトは Go のモジュールと npm のパッケージだけを拾い、コピーした素材は拾わない）。`third-party-notices_test.sh` に、その節があることの検査を足す。README のライセンスの記述にも一行足す。

## 3. `molecules.ToolCard`（カード）

設定画面（概要・「ローカル・代替モデル」）で使う、リンクとしてのカード（トップ画面のツールのカードは #978 で外した）。**カードの中にボタンもリンクも入れない**（カード全体が 1 つの `<a>`）。

テンプレートは `ToolCardItem(c ToolCard)`（`<li>`）、状態の断片は `ToolStatus([]StatusLine)`。

```go
type ToolCard struct {
    ID          string        // 要素 id の元（"generation-test" など。英小文字とハイフン）
    Href        templ.SafeURL // paths.* から作る
    Icon        atoms.IconName
    Title       string        // リンクの名前になる
    Description string        // 1 行の説明（2 行までに折り返す）
    CTA         string        // 既定 "開く"。見た目だけ（aria-hidden）
    StatusURL   string        // 空でなければ、状態を hx-get で後から読む（モデルマネージャーのカード）
    // Status []StatusLine は #847 が足す（StatusURL が空のときの、サーバーで組み立てた状態。設定の概要）。#846 は StatusURL だけ
}
type StatusLine struct{ Tone atoms.BadgeTone; Text string } // Text は Badge の文字（状態を言葉で言う）。実装は #846
```

```html
<li>
  <a id="tool-<ID>" href="…" data-testid="tool-card" data-tool="<ID>"
     aria-labelledby="tool-<ID>-title" aria-describedby="tool-<ID>-desc tool-<ID>-status"
     class="flex h-full min-h-36 flex-col gap-3 rounded-lg border border-border bg-surface p-4 … (§1 のホバー・フォーカス)">
    <span class="flex items-center gap-3">
      <span class="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-accent-soft text-accent" aria-hidden="true">{Icon}</span>
      <span id="tool-<ID>-title" class="text-base font-semibold text-ink">{Title}</span>
    </span>
    <span id="tool-<ID>-desc" class="text-sm text-mute">{Description}</span>
    <span id="tool-<ID>-status" class="mt-auto flex min-h-6 flex-col gap-1 text-xs text-ink"
          hx-get="{StatusURL}" hx-trigger="load" hx-target="this" hx-swap="innerHTML"></span>
    <span class="inline-flex items-center gap-1 text-sm text-accent" aria-hidden="true">{CTA}{IconChevronRight}</span>
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

ネイティブの `<dialog>` を 1 つだけ、ページの枠に置く。中身は `hx-get` で差し込む。使う画面は #847 の設定画面だけで、トップ画面（#846）は使わない。**この部品は #847 で作る**（オーナー決定。§6.1・§8 の質問 7）。以下は部品そのものの契約。

```html
<dialog id="mam-modal" aria-labelledby="mam-modal-title" aria-describedby="mam-modal-desc" data-modal
        class="m-auto w-[min(40rem,calc(100vw-2rem))] max-h-[calc(100vh-4rem)] overflow-y-auto rounded-lg border border-border bg-surface p-0 text-ink backdrop:bg-black/70">
  <div id="mam-modal-body"></div>
  <p id="mam-modal-failure" role="alert" class="px-4 pb-2 text-sm text-danger" hidden></p>
  <button type="button" data-modal-close aria-label="閉じる" class="absolute right-3 top-3 …">{IconClose}</button>
</dialog>
```

- **置く場所**: 設定の各ページの枠（`pages/settings_pages.templ`。1 ページに 1 つ）。`AppLayout` には足さない（他の画面に不要な要素を出さない。§8 の質問 4）。幅は既定 40rem（640 px）で、1024 px のウィンドウでも余白が残る。広い内容（モデルの割り当て表）は `data-size="lg"` で `max-w-3xl`。高さは画面の高さ − 4rem までで、超えたら中がスクロールする（700 px の高さでも収まる）。
- **断片の形**（`GET …/edit` などが返す。ルートの根は `#mam-modal-content`）: `<h2 id="mam-modal-title">`（モーダルの名前）→ 任意で `<p id="mam-modal-desc">` → 本文（フォーム）→ フッター（`キャンセル`（`type=button data-modal-close`）と `保存`）。**最初に触る要素に `autofocus` を 1 つだけ付ける**（最初の入力欄。編集できる欄が無いときは閉じるボタン）。エラーは本文の先頭の `<p role="alert" id="mam-modal-error" tabindex="-1">`。
- **開く**: 開くボタンは `hx-get="{URL}" hx-target="#mam-modal-body" hx-swap="innerHTML" data-modal-open data-focus-key="{キー}"` を持つ（属性は `modal.OpenAttrs(url, focusKey)` が作る）。応答が `#mam-modal-body` に入ったら（`htmx:afterSwap`）、`static/src/components/modal/` が `dialog.showModal()` を呼び、`autofocus` の要素へフォーカスする。応答が 404・409 でも、理由の断片（`role="alert"` と「閉じる」だけ）を同じように表示する（`main.ts` が HTML のエラー応答を swap する）。
- **閉じる**: `Esc`（ネイティブの `cancel`）、`キャンセル`、`閉じる`、保存の成功（下）。**背景のクリックでは閉じない**（入力途中の消失を防ぐ。§8 の質問 5）。閉じたとき（`close` イベント）に `#mam-modal-body` を**空にする**（発行したトークン・入力値を DOM に残さない）。さらに `htmx:beforeHistorySave` で本文を空にし `open` を外す（`modal.ts` の `htmx:beforeHistorySave` のリスナーが行う。`lib/history.ts` の `sanitizeHistoryHtml` は変えていない。トークンが htmx の履歴スナップショット（sessionStorage）に残らないように）。
- **閉じるときの送信中の要求**: 閉じたときに、モーダルの中から送った**答えの来ていない GET**（ポーリング・`hx-trigger="load"`）だけを取りやめる（答えが空になった本文に戻らないように）。**POST・PUT・DELETE は取りやめない**: サーバーが適用済みかもしれず、答えの OOB の行（保存した設定・発行したトークンの件数）を反映するため。閉じたモーダルに答えが入ったときは、OOB を反映したあと本文を空にする（`htmx:afterSwap`。トークンは DOM に残らない）。
- **入れ替えの間は触れない**: 本文が入ったあと htmx が `hx-*` を処理する（swap が settle する）まで約 20ms ある。その間にフォームが送られるとブラウザ自身が送り、キーが URL に載る。そのため `modal.ts` は本文を `inert` にして、`htmx:afterSettle` で外して初期フォーカスを置く。キーのフォームは念のため `method="post"`（万一ブラウザが送っても、キーは URL ではなく本文）。2026-10-25 の実機（headless Chromium）で、本文が出た直後に打鍵と送信をするとこの競合が起きることを確かめて直した。
- **フォーカスの閉じ込め**: `showModal()` が外側を `inert` にするので、独自の閉じ込めは書かない。Windows 版の WebView2（Chromium）でも同じ動作の見込み（**未検証**。実機が要る）。
- **フォーカスの戻り**（`focus-restore` と同じ考え方）: 開くボタンを `htmx:beforeRequest` の時点で覚える（`document.activeElement`、`data-focus-key`、最も近い `id` 付きの祖先の id）。閉じたあと、(1) 覚えたボタンがまだ DOM にあればそこへ、(2) 行が OOB で差し替わっていれば、覚えた行の中で種類と `data-focus-key` が同じコントロールへ、(3) 無ければその行、(4) それも無ければ一覧の見出しへ（`tabindex="-1"` を付けて）。開くボタンには必ず `data-focus-key`（`edit-<行のキー>`・`add`・`issue` など。ラベルが変わってもキーは変えない）。
- **長い本文の頭**（Issue #1097）: スクロールするのはダイアログ自身（`overflow-y-auto`）。閉じるボタンは高さ 0 の `sticky top-0` の箱（`z-sticky`）に入れてダイアログの最初の子に置き、`Frame` の見出し `h2` も `sticky top-0 z-raised bg-surface` にするので、本文が長くても（高さ 400 px の横向きの端末でも）名前と閉じるボタンは上端に残り、本文だけがその下をスクロールする。`modal.ts` は開くたびに `scrollTop = 0` にする（`showModal()` が `autofocus` のブロックを見える位置へスクロールして頭が切れる・閉じたダイアログが前のスクロール位置で戻る・開いたままのダイアログに別の結果が入るのを防ぐ）。回帰テストは `modal_test.go`（構造）と `gentest-results/result-modal.wtest.ts`（高さ 400 px で実描画を測る）。
- **成功**: 保存の応答は 200 で、本文は**一覧の行の OOB だけ**（`hx-swap-oob="true"`。同じ `id` の行を置き換える。新しい行は `beforeend:#<一覧の id>`）、ヘッダーに `HX-Trigger-After-Swap: modal-close`。モーダルの本文には空が入る。`modal/` は `document` で `modal-close` を聞いて閉じる（htmx 2.0.11 の `htmx.js` は、リクエストを出した要素が swap で DOM から消えていると、`HX-Trigger-After-Swap` を `<body>` に起こす。2026-10-25 に読んで確認。`bubbles` なので `document` で受けられる）。
- **失敗（422）**: 応答の本文はモーダルの断片（`#mam-modal-body` に入り直す）。理由は `#mam-modal-error`、入力は**残す**（`value` を描き戻す）。`aria-invalid="true"` と `aria-describedby="mam-modal-error"` を、問題の欄に付ける。差し替え後は、問題の欄（無ければ `#mam-modal-error`）にフォーカス（`autofocus` を付け直す）。**秘密の値（API キー・トークン）は 422 でも描き戻さない**（入力し直す）。 理由の `#mam-modal-error` を取り除く答え（接続先のモーダルの接続テストの `DraftTestResult`）が入ったときは、同じ入れ替えのうちに、その理由を指していた欄の `aria-invalid` と `aria-describedby` を外す（`modal.ts`。宙に浮いた参照を残さない）。
- **トースト・アラートは見えない**: `#toast-region` と `#alert-region` は `<body>` の子で、モーダルは最前面の層（top layer）に出るため、モーダルが開いている間のトーストはモーダルの背後に隠れる。そのためモーダル内の失敗は、断片の `role="alert"` に出す。HTML でない失敗（通信断・タイムアウト・5xx の非 HTML）は `htmx:responseError` / `sendError` / `timeout` を `modal/` が聞き、`#mam-modal-failure` に `requestFailureMessage` の文言を出す（トーストは出さない）。
- **二重送信**: モーダルのフォームは `hx-sync="this:drop"` と `hx-disabled-elt="find button[type=submit]"` を持つ（`double_submit_test`）。削除などの危険な操作は `hx-confirm`（#847 決定事項 8(a)。`destructive_confirm_test`）。
- **リンクから開く・広いダイアログ**（Issue #1004）: `modal.OpenLinkAttrs(url, focusKey)` は `OpenAttrs` の `<a>` 版（`type` が無く `href` が同じ内容のページ。スクリプトが無い・新しいタブで開く操作はそのページへ行き、ふつうのクリックは本文を求める）。本文の最初のブロックに `data-modal-wide`（`modal.WideAttr`）を付けると、ダイアログがその間だけ広くなる（`has-[[data-modal-wide]]:w-[min(64rem,…)]`。単発生成の結果の詳細が、画像と設定を並べるのに使う）。同じ URL がページとモーダルの本文の両方を返すときは、`HX-Target` が `mam-modal-body` かどうかで分け、`Vary: HX-Request, HX-Target` を付ける。
- **置き場所**: `molecules/modal/`（`Host`・`Frame`・`Error`・`Actions`・`Cancel`・`Notice`・`OpenAttrs`）と `static/src/components/modal/`（`modal.ts`・`modal.wtest.ts`。Lit ではなく `focus-restore` と同じ `installModal()`。`main.ts` が呼ぶ）。テストは wtr: 開くとフォーカスが `autofocus` へ・`Esc` で閉じて開いたボタンへ戻る・行が差し替わっても戻る・成功で閉じる・422 で開いたまま入力が残る・閉じると本文が空・履歴の保存で空になる。

## 5. 一覧の行（`SettingsCard`・`SettingsTable`・`SettingsTableRow`）

#847 の一覧は 2 つの形。どちらも**行の `id` が OOB の差し替え先**（既存の `credential-row-<provider>` などの `id` と `data-testid` はそのまま使う。既存のテストの変更を減らすため）。

| 形 | 使う所 | 構造 |
|----|-------|------|
| `SettingsCard` | API キー・ChatGPT のログイン・ローカルの接続先・MCP の待ち受け先 | `<li id="<種類>-row-<キー>" data-testid="settings-card">`。名前（`<h3>`。`<h4>` も可）・状態の `Badge`・**リスクの印（`Card.Marks`。「非公式」「商用注意」。状態の前に並べる文字のバッジ）**・1 行の説明・要約・ボタンの行。`ul role="list"`、`grid-cols-2`（1024 px でも 2 列）。`Card.Class` は `<li>` に足す class（音声・口パクの選択中の枠 `has-[:checked]:border-accent`）。**長い説明・注意書き・ライセンスはカードに出さず、「詳細」ボタンで開くモーダルへ置く（#1005。ChatGPT のログインと音声・口パクのカード。API キーと同じ形）**。名前・バッジは長くても折り返す（`break-words`。`Badge` は `max-w-full`） |
| `SettingsTable` ＋ `SettingsTableRow` | LLM の割り当て・画像・動画モデル・MCP のトークン | `<table class="w-full text-sm">`、`<caption class="sr-only">`、`<th scope="col">`、行の見出し `<th scope="row">`、`<tr id="<種類>-row-<キー>">`。1 つの行に要約と「編集」 |

- 行のボタンは **`atoms.SmallButtonFor(tone, "編集", 行の名前, attrs)`**（見える文言は「編集」のまま、`aria-label` が「編集: <行の名前>」。#783・#793。`a11y/row_names_*` の検査に足す）。モーダルを開くボタンは `ModalOpen` の属性を持つ。
- 状態は `Badge` ＋文字（§1）。「変更あり」「既定」のような印も文字のバッジ（色だけにしない）。
- 説明・要約は `text-mute`、折り返しを許す（`break-words`）。切り詰め（`truncate`）で情報を隠さない。
- 実装（`settingsui/table.templ`）: `SettingsTable(bodyID, caption, []Column)`（`<tbody id=bodyID>` が一覧。新しい行の OOB は `beforeend:#<bodyID>`。広い表はその箱の中で横にスクロールし、ページを広げない）・`TableRowItem(TableRow{ID, TestID, Attrs, OOB})`（`SettingsTableRow` の実体）・`RowHead`（`<th scope="row">`）・`Cell`。`Column.Class` を `<th>` と同じ列の `Cell` に付けて、狭い窓で列を隠す（`hidden lg:table-cell`）。**OOB の `<tr>` は応答の先頭に置く**（HTML のパーサーは先頭の `<tr>` だけを行として読む）。
- 置き場所: `molecules/settingsui/`（`card.templ`・`table.templ`・`nav.templ`）。`molecules/` の直下に置くと上限（2,000 行）に近づくため、サブパッケージにする（§8 の質問 3）。サイドナビ `SettingsNav` は [settings-screen.md](settings-screen.md) §4。
- **モデルマネージャーのモデルのカード**（Issue #865）: `SettingsCard` と同じ面（`rounded-lg border border-border bg-surface p-4`・見出しは `<h3>`・状態は文字の `Badge`・`ul role="list"` の `grid`）を使うが、複数のバッジ・メタの表・進捗・`<details>` と、右上の操作（`shrink-0`）が要るため、`CardItem` ではなく `organisms/modelmgr` の `modelCard` として持つ。**#954 で、モデルを並べる所はすべて `molecules/modelcard` の 1 つのカード（名前・アイコン・大きさ・状態・商用利用のアイコン・ボタンだけ。細かい情報はタップで開く詳細モーダル）になった**（`pick.OpenFrame` の枠）。見た目と構造は [model-manager-screen.md](model-manager-screen.md) の「カードと詳細モーダル（#954）」。`ToolCard` はリンクのカードで、使わない。

## 6. 2 つの Issue の取り合い

### 6.1 モーダルの担当（2 つの Issue の文面が食い違っていた。決定: #847）

| Issue | 文面（原文） | 読み取れること |
|-------|-------------|---------------|
| #846 決定事項 1（オーナー決定: 推奨どおり (a)） | 「カード・アイコン・モーダルの部品は、先に出るトップ画面の Issue で作り、設定画面でも使う」。関連の節にも「カード・アイコン・モーダルの部品を共有する」とある | モーダルは #846 で作る。ただし #846 の「やること」（§1 カード・§2 アイコン・§3 トップ画面）に、モーダルの項目は無い。トップ画面もモーダルを使わない |
| #847 「やること」2「共通の部品」 | 「**モーダル**（molecules ＋ 小さな TS）」の箇条書き（`<dialog>` の `showModal()`・フォーカスの閉じ込めと戻り・`hx-get`・OOB・422）。「**概要のカード**: トップ画面の Issue の `tool_card` と同じ部品を使う」 | モーダルは #847 で作る。カードだけを #846 から借りる。受け入れ条件にもモーダルの開閉・フォーカスの戻りがある |

オーナーは 2026-10-25 に「モーダルの部品は #847 で作ります」と決定した（#846 の Issue のコメント）。#846 ではモーダルを作らない。

### 6.2 そのほかの部品

| 部品 | #846 | #847 | 備考 |
|------|------|------|------|
| `atoms.Icon` | 作る（8 個） | 8 個を足す | 先に出るほうが `atoms/icon.templ` と `LICENSE`・`THIRD_PARTY_NOTICES.md` の仕組みを作る |
| `molecules.ToolCard` | 作る（`StatusURL`） | 概要で使う（`Status`） | `Status` を持つ形は #847 が足してよい。#846 の時点では `StatusURL` だけでも契約は変わらない |
| 一覧の行・サイドナビ | 使わない | 作る | |
| モーダル | 作らない | 作る（決定。§6.1） | 画面で使うのは #847 だけ |
| 状態の `Badge` の対応（§1） | 使う | 使う | 同じ文言・同じ色 |

## 7. 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 0.1 | 2026-10-25 | 新規作成（案） | Issue #846 決定事項 5(a)・#847 決定事項 1(a)。実装前にオーナーが確認するための共通部品の仕様 |
| 0.2 | 2026-10-25 | モーダルの担当を未決にした（§6.1 に #846・#847 の原文を併記、§8 の質問 7 を追加） | 独立レビュー（PR #849）の指摘。#846 決定事項 1 は #846 で作ると読め、#847 の「やること」は #847 で作ると読める。仕様書が片方に決めていた |
| 0.3 | 2026-10-25 | 状態を「一部実装済み」にした（`atoms.Icon` の 8 個と `molecules.ToolCard` を #846 で実装）。§8 をオーナーの決定の表にし、モーダルを #847 で作ることを §6.1 に記録した | Issue #846 の実装と、オーナーの 2026-10-25 の判断 |
| 0.4 | 2026-10-25 | §1 のフォーカスの輪を `outline-none` から `outline-hidden` にした（輪郭が `forced-colors` で描かれる） | 独立レビュー（PR #849 の 2 周目）の指摘。輪は box-shadow で、Windows のハイコントラストでは消える |
| 0.5 | 2026-10-25 | §4 に「入れ替えの間は触れない」を足した。状態を更新した。`atoms.Icon` の #847 の 7 個・モーダル（`molecules/modal/`・`static/src/components/modal/`）・`SettingsCard`（`settingsui.CardItem`）を実装した。`SettingsTable`・`IconCopy` は後続。モーダルの関数の名前を実装に合わせた（`ModalOpen` → `OpenAttrs`）。成功の通知は `atoms.SuccessToastOOB` | Issue #847 段階 ①・②-1・②-2 の実装 |
| 0.6 | 2026-10-25 | `SettingsTable`・`TableRowItem`・`RowHead`・`Cell` と `IconCopy` を実装した（§5 に実装の形を足した。状態を更新した） | Issue #847 段階 ②-3 の実装 |
| 0.7 | 2026-10-25 | 「コピー」のボタン（`data-copy-source`・`data-copy-status`。`static/src/components/copy/`）を MCP のトークンの発行のモーダルに実装した。`SettingsTable` は ②-5（モデルの表）・②-6（トークンの表）が使う | Issue #847 ②-5・②-6 |
| 0.8 | 2026-10-25 | §4 のモーダルの履歴の掃除の置き場所を、実装どおり `modal.ts` の `htmx:beforeHistorySave` に直した（`lib/history.ts` は変えていない） | PR #849 の第 2 周の独立レビューの指摘 |
| 0.9 | 2026-10-25 | §4 に「閉じるときの送信中の要求」（GET だけ取りやめ、POST・PUT・DELETE は答えを受けて OOB を反映し本文を空にする）を足し、理由を取り除く答えが欄の `aria-invalid`・`aria-describedby` も外すと書いた | PR #849 の第 3 周の独立レビューの指摘 |
| 0.10 | 2026-10-28 | §5 に、モデルマネージャーのモデルのカード（`modelCard`）と `SettingsCard`・`ToolCard` の関係を足した | Issue #865 |
| 0.11 | 2026-10-28 | §9「「?」ヘルプ」と §10「ヘルプに移した説明の一覧」を足した。§2 に `IconHelp` を足した。`SettingsSection` の説明は常時表示でなくヘルプになった | Issue #866 |
| 0.12 | 2026-10-28 | §9 のヘルプを、見出しの下に開く開示（インライン）から、ネイティブの Popover API のポップオーバー（ボタンの近くに浮かぶ）に変えた。同時に開くのは 1 つ・`Esc` でボタンへ戻る・外側のクリックで閉じる・`Tab` の動き・`role="group"`・狭い画面での位置（ずらす・反転）を決めた。モデルマネージャーの h1 の説明と「?」は付けない（動かさない）に戻した | オーナーの決定（#866） |
| 0.13 | 2026-10-28 | §1 の冒頭に、色の用途別の許可クラスの表 [color-rules.md](color-rules.md) への参照を足した。`Badge`・`Button` の色は、その表の正とした | Issue #867 |
| 0.14 | 2026-10-29 | §9.1 に、ポーリングで差し替わる範囲のカードの「?」のパネルを範囲の外に置き、差し替えても開いたままにする例外を足した（`Card.HelpPanelElsewhere`・`help.ts` の `htmx:afterSettle`）。§9.2 の `Card.Help` の行を合わせた | Issue #866。オーナーの決定（案 A） |
| 0.15 | 2026-11-01 | §10 の単発生成の行に、作成フォームの区画の説明・モデルごとの性質と選べない理由・パラメーター・見積り・実行の補足を足した | Issue #906 |
| 0.16 | 2026-11-02 | §1 の表に、スライダーの共通の見た目（`slider.css`・`--fill`・`components/slider/`）を足した。`app.css` の説明に `slider.css` の import を足した | Issue #955 |
| 0.17 | 2026-11-03 | §10 の単発生成の行に、かんたんのプロンプトの見出しに足した「プロンプトのウェイト」の「?」（`help.Row`。`gentestadv.WeightIntro`）を足した | Issue #977（ステージ 2） |
| 0.18 | 2026-11-03 | §4 に `modal.OpenLinkAttrs`（リンク版の開く属性）と `data-modal-wide`（本文が広いダイアログを求める）を足した | Issue #1004（単発生成の結果の詳細を、一覧のタイルからモーダルで開く） |
| 0.19 | 2026-11-03 | §5 の `SettingsCard` に、リスクの印（`Card.Marks`）・`Card.Class`・折り返し、長い説明は「詳細」のモーダルへ置く決まりを足した。§9.2 の `Card.Help` の行を、ChatGPT のカードが「?」をやめたことに合わせた | Issue #1005 |
| 0.20 | 2026-11-06 | §1「部品・オーバーレイ」の表に、独自ウィジェット（Lit）の案件ガイド [lit-widgets-guide.md](lit-widgets-guide.md) への案内の行を足した | Issues #1043・#1044・#1045 |
| 0.21 | 2026-11-06 | §1 に「プロンプトの重み調整（`mam-prompt-weight`）」を足した（面・文字・ボタン・出る・消える・状態の見せ方・操作・キーボード・アクセシビリティの 8 項目 W1〜W8）。動きは gentest-tiers.md §4 が決めたままで、ここは描き方だけ。実装との差分 6 件を表にし、#1094・#1095 に分けた（修正は別 Issue） | Issue #1046。`mam-prompt-weight` の見た目・操作が案件ガイドに載っていなかった |
| 0.22 | 2026-11-06 | §4 に、長い本文のモーダルの頭の決まりを足した（閉じるボタンと見出しは、スクロールするダイアログの上端に貼り付く。開くたびに先頭から始まる） | Issue #1097 |
| 0.23 | 2026-11-06 | §10 のモデルマネージャーの行から「試し生成」の区画を外し、区画「モデル」の「?」に単発生成画面へのリンクを入れることを書いた（状態は仕様のみ） | Issue #1100 |

## 8. オーナーへの確認事項と決定（2026-10-25: すべて推奨する既定で決定）

| # | 質問 | 決定（推奨する既定） |
|---|------|-------------|
| 1 | #847 の概要のカードとサイドナビにアイコンを付けるか | カードには付ける（`ToolCard` と同じ部品）。サイドナビは文字だけ（幅 200〜240 px で文字を優先）。付けないなら #847 のアイコンは 2 個で済む |
| 2 | カードの「開く」を、枠のあるボタン風にするか、文字＋矢印にするか | 文字＋矢印（カード全体がリンクなので、別のボタンに見せない。`bg-accent` は実ボタン専用） |
| 3 | #847 の部品を `molecules/settingsui/`・`molecules/modal/` のサブパッケージに置くか、`.linterly-baseline` を広げて `molecules/` に置くか | サブパッケージ（`organisms/` が基準値を超えた経緯を繰り返さない）。`ToolCard` だけは `molecules/` の直下 |
| 4 | モーダルの `<dialog>` を `AppLayout` に置くか、設定のページごとに置くか | 設定のページごと（他の画面に影響しない） |
| 5 | 背景のクリックでモーダルを閉じるか | 閉じない（`Esc` と「キャンセル」「閉じる」だけ。入力途中の消失を防ぐ） |
| 6 | Lucide の `LICENSE` の置き場所と、`THIRD_PARTY_NOTICES.md` への載せ方 | §2 の案（全文を `atoms/icons/LICENSE` に置き、スクリプトの「同梱した素材」の節で載せる） |
| 7 | **モーダルの部品をどちらの Issue で作るか（決定: #847 で作る）**。#846 決定事項 1 は「カード・アイコン・モーダルの部品は、先に出るトップ画面の Issue で作り」、#847 の「やること」2 は「モーダル（molecules ＋ 小さな TS）」を #847 の共通の部品に挙げていて、食い違っている | 【決定】**#847 で作る**。理由: ①トップ画面はモーダルを使わない（#846 の「やること」にもモーダルの項目が無い）ので、#846 で作ると使い道の無い部品を先に出すことになり、動作（フォーカスの戻り・Esc・422）を実画面で確かめられない ②#847 の受け入れ条件がモーダルの開閉とフォーカスの戻りを含む ③#847 の実装順（settings-screen.md §7 の ②-1）が、モーダルを API キーのカードで先に確かめる。#846 で作る場合は、#846 に `molecules/modal/`・`static/src/components/modal/`・`IconClose`・wtr のテストを足し、#846 のレビューの範囲が広がる。どちらでも §4 の契約は変わらない |

## 9. 「?」ヘルプ（`molecules/help`。Issue #866）

画面の説明の段落（長い文・ヒント）を、いつも見える場所から外し、見出しの横の「?」ボタンで開く短い**ポップオーバー**（ボタンの近くに浮かぶ、モーダルでない小さなパネル）にする部品。**モーダル（§4。設定の編集・ページに 1 つ）でも、`inline-dialog`（モデルマネージャーの同意・確認）でもない**: ヘルプは操作を止めず、背景を `inert` にせず、フォーカスも閉じ込めない。中身は**今まで画面に出していた文言をそのまま移したもの**で、ヘルプのために新しい文は書かない。

### 9.1 形（ネイティブの Popover API）

```html
<div class="flex items-center gap-2">
  <h2 class="text-base font-semibold">見出し</h2>
  <button type="button" data-help-toggle popovertarget="help-<ID>" aria-expanded="false" aria-controls="help-<ID>" aria-label="見出しの説明" class="…">{IconHelp}</button>
  <div id="help-<ID>" popover role="group" aria-label="見出しの説明" data-help-panel class="inset-auto m-0 max-h-[calc(100dvh-1rem)] w-[min(24rem,calc(100vw-1rem))] overflow-y-auto break-words rounded-lg border border-border bg-surface p-3 text-sm text-ink"><p>説明…</p></div>
</div>
```

**なぜネイティブの Popover API か**: ①最前面の層（top layer）に出るので、`overflow-hidden` の親・カード・表の箱に切られない（近くに浮かべる部品の最大の問題）②`Esc`・外側のクリック／タップで閉じる・1 つ開くと他が閉じる、をブラウザが持つ（自前で書くとバグの元）③ボタンの `popovertarget` で、クリック・`Enter`・`Space` の開閉が JS なしでも動く。対象のブラウザ（Windows の WebView2 = Chromium、macOS の WKWebView = Safari 17 以降、開発用の Chrome）は、いずれも Popover API に対応している（Chrome 114・Safari 17・Firefox 125。2026-10-28 時点）。CSS の anchor positioning は Safari 26 以降のため使わず、位置は §9.1 の「位置」のとおり `help.ts` が決める。

- **開く・閉じる**: ボタンのクリック・`Enter`・`Space` で開閉する。初めは閉じている（開いた状態は覚えない。ページを読み直す・本文が差し替わる（OOB でカードが入れ替わる）と、差し替えられる範囲の中のパネルは新しい要素なので閉じる）。`aria-expanded` は `toggle` イベントで `help.ts` が追随させる。**例外（Issue #866 の決定、2026-10-29）**: ポーリングで 2 秒ごとに差し替わる範囲（ChatGPT の `#chatgpt-panel`）のカードの「?」は、パネルを差し替えの範囲の外に置く（`settingsui.Card.HelpPanelElsewhere`。カードはボタンだけを描き、パネルは `Section` が `Panel` の外に描く）。差し替わってもパネルは開いたままで、`help.ts` が `htmx:afterSettle` で、新しいボタンの `aria-expanded` を開いている状態に合わせ、パネルを新しいボタンの近くへ置き直す。`Esc` で閉じるとフォーカスは新しいボタンへ戻る。この形のパネルは、ボタンの直後（`Tab` の順）にならない。説明にリンクを入れない（今の 3 枚は文だけ）。
- **同時に開くのは 1 つ**（決定）: `popover`（`auto`）の標準の動きで、別のヘルプのボタンを押すと、開いていたヘルプは閉じる。理由: 複数が重なると狭い画面で読めず、閉じる操作も増える。
- **外側のクリック／タップで閉じる**: 標準の light dismiss。パネルの中のクリックでは閉じない。
- **`Esc`**: フォーカスがボタン・開いているパネルの中・ページ（`body`。パネルの文字をクリックした後など）にあるとき、パネルを閉じて**フォーカスをボタンへ戻す**（`help.ts` が行う。ブラウザの自前の戻しは、タッチや Safari のようにボタンが押してもフォーカスされない環境で `body` に落ちるため頼らない）。その `Esc` は `preventDefault` され、同時に開いている `inline-dialog` を閉じない。ヘルプが開いていないとき、フォーカスが別の部品にあるときは何もしない（モーダル・`inline-dialog` の `Esc` とぶつけない）。
- **フォーカスと `Tab`**（決定）: 開いてもフォーカスは**ボタンに残す**（ダイアログではない。もう一度 `Enter` で閉じられる）。パネルは DOM でボタンのすぐ後ろにあるので、`Tab` でボタンの次にパネルの中のリンクへ進む（読み上げも続きとして読む）。**フォーカスがボタンとパネルの外へ出たら（`Tab`・`Shift+Tab`）閉じる**（外側のクリックのキーボード版。`focusin` で見る）。リンクの無い説明では、`Tab` は次のコントロールへ進んで閉じる。
- **ARIA**（決定）: ボタンの名前は「<主題>の説明」（`help.ToggleLabel`。ページ中の「?」を一覧で見分けられるよう「ヘルプ」だけにしない）、`aria-expanded`、`aria-controls`。アイコンは飾り（§2）。パネルは `role="dialog"` にしない（ダイアログは開いたらフォーカスを中へ移し、名前と閉じ方を持つもので、非モーダルの説明に合わない）。`role="group"` と `aria-label`（ボタンと同じ「<主題>の説明」）にする（ランドマークを増やさず、読み上げが「<主題>の説明、グループ」と名前を告げる）。閉じている間は `display: none`（ブラウザ）で、読まれず、キーボードでも届かない。そのため**パネルに `flex`・`grid`・`block` などの display のクラスを付けない**（閉じたまま見えてしまう。`help_test`）。
- **位置と狭い画面（約 375 px）**（`help.ts` の `place`）: パネルは `position: fixed`（top layer はビューポート基準）で、`toggle` で開いた直後に**ボタンの下**（ボタンの左端にそろえ、間 6 px）へ置く。幅は `min(24rem, 100vw − 1rem)`（375 px では 359 px）で、**はみ出すときは左へずらす**（左右とも 8 px の余白。`left = clamp(ボタンの左, 8, ビューポートの幅 − パネルの幅 − 8)`）。**下に高さが足りなければボタンの上へ反転する**（`top = ボタンの上 − 6 − パネルの高さ`。上にも足りなければ、窓の下端から 8 px 上にそろえる）。高さは `max-h-[calc(100dvh-1rem)]` で、超えたらパネルの中がスクロールする。ウィンドウのリサイズとページのスクロール（ボタンが動く）で置き直す。ページを横にスクロールさせない（`fixed` で、幅が窓を超えない）。親の `overflow` で切られない（top layer）。広い画面と同じ部品・同じ出し方で、画面幅による分岐は無い（上の計算だけ）。
- **見た目**: 新しい色・影は作らない（§1）。ボタンは 28 px 角（`h-7 w-7`。WCAG 2.5.8 の 24 px 以上）の丸（`rounded-[50%]`。`atoms.Badge` の形を手書きしない検査 `badge_test` が `rounded-full` を禁じるため）で、`border-border text-ink`（ホバーで `border-border text-ink`。[color-rules.md](color-rules.md) §3 の「ホバー面」と §4 の `hover:text-ink`）、輪は §1 のフォーカスの輪、`transition-colors motion-reduce:transition-none`。パネルは §1 のカードと同じ面（`bg-surface`・`border-border`・`rounded-lg`・`p-3`）で文字は `text-ink`。アイコンは `atoms.IconHelp`（§2。Lucide `circle-question-mark`）。
- **置き場所**: `backend/internal/web/molecules/help/`（`ID`・`ToggleLabel`・`Toggle`・`Panel`・`Text`・`Row`。サブパッケージにするのは `molecules/` の行数の上限を守るため）と `static/src/components/help/`（`help.ts`・`help.wtest.ts`。Lit ではなく `inline-dialog` と同じ `installHelp()`。`main.ts` が呼ぶ。委譲のイベントなので、後から入った本文の「?」も動く）。`main.ts` では **`installInlineDialog()` より前**に呼ぶ（どちらも `document` の `keydown` で `Esc` を受け、先に走った方が `preventDefault` して取る。ヘルプが先でないと、ヘルプを開いたままフォーカスがページにあるとき `Esc` がヘルプでなく `inline-dialog` を閉じる。`help.wtest.ts` が見張る）。

### 9.2 画面の部品からの使い方

| 部品 | 使い方 |
|------|-------|
| `molecules.SettingsSection(title, explanation)`・`SettingsSectionH3(title, explanation)` | 見出し（h2 / h3）の横にボタン、下に `explanation`（文字列。改行で段落）のパネル。`explanation` が空ならボタンも出さない。`SettingsSectionHelp(level, title, component)` は説明が段落でないとき（リンク・複数の要素） |
| `molecules.SettingsSectionAt(level, title, description)` | **状態を言う 1 行**（バージョンとアップデートの「現在のバージョン…」）だけ。説明には使わない |
| `settingsui.Category(title, explanation)` | 分類の h2 の横にボタン。`explanation` は `templ.Component`（`help.Text(...)`・`nil` でボタン無し） |
| `settingsui.Card.Help` | カードのタイトルの横にボタン、その直後にパネル（`SettingsCard`。§5）。パネルの id は `help.ID(カードの ID)`。`HelpPanelElsewhere` を立てると、ボタンだけを描く（パネルは呼び出し側が同じ id で、差し替えの範囲の外に描く。§9.1 の例外。#1005 で ChatGPT のカードは「?」をやめて「詳細」のモーダルにしたので、今これを使うカードは無い） |
| `help.Row(id, subject, explanation)` | 上の部品を使わない見出し（単発生成のページの h1 など）。子が見出し。ボタンの直後にパネルを置く（`Tab` の順） |

- パネルの id は主題（見出しの文字・カードの ID）から決まる（`help.ID`。同じ入力は同じ id）。1 ページの主題は重複させない。

### 9.3 ヘルプに入れてよいもの・いけないもの

- **入れる**: 画面・区画・カードが**何のためか**、仕組みの補足、制限・注意の長文、使い方の手順（今まで見出しの下・カードの説明に常時出していた段落）。
- **入れない（見えたままにする）**: 操作のラベル、状態（バッジ・「ログイン済み」・「設定済み」）、結果・エラー（`role="alert"`・`role="status"`・トースト）、確認（`hx-confirm`・同意）、固定されていて変えられない理由（`llm-pinned`）、操作の可否に関わる前提（「反映にはアプリの再起動が必要です」）。ヘルプを開かないと操作の結果や失敗の理由が分からない画面にしない。
- パネルの中に操作（フォーム・ボタン）は置かない。リンクは可（`Tab` でボタンから入れる）。
- **新しい文を足さない**: ヘルプの中身は、今まで常時表示していた文言の移動だけ（段落の区切りを変えるのは可。ヘルプのための言い換え・追加は不可）。

### 9.4 テスト

- `molecules/help/help_test.go`: ボタンの名前・`popovertarget`・`aria-expanded="false"`・`aria-controls` とパネルの id が一致・パネルは `popover` + `role="group"` + 同じ名前で display のクラスが無い・説明が空ならボタンが出ない・id が主題で決まる。
- `molecules/settings_section_test.go`・`molecules/settingsui/settingsui_test.go`・`organisms/settings/llm/section_test.go`: 説明がパネルの中にだけある・ボタンが見出しの後ろ・操作に関わる文（再起動の注記・固定・エラー）は本体に残る。
- `pages/help_moved_test.go`: §10 の対象画面で、長い説明がパネルの中にだけあり（ページの本体には無く）、見出し・状態は本体に残る。パネルごとにそれを開くボタンが 1 つある。
- `static/src/components/help/help.wtest.ts`（実 Chrome）: クリック・`Enter`・`Space` で開閉し `aria-expanded` が追随・`Esc`（ボタンから・パネルの中のリンクから・フォーカスがページにあるとき）で閉じてボタンにフォーカスが戻る・外側の（実ポインターの）クリックで閉じ中のクリックでは閉じない・1 つ開くと他が閉じる・`Tab` でボタンからパネルのリンクへ入り、さらに `Tab`・`Shift+Tab` で出ると閉じる・`overflow: hidden` の親に切られない・デスクトップ幅（1280）と狭い幅（375）で、ボタンが右端・下端・左下のどこにあってもパネルがウィンドウの内側に収まり、ページが横にあふれない。

## 10. ヘルプに移した説明の一覧（Issue #866）

| 画面 | ヘルプへ移した説明（常時表示から） | 本体に残すもの |
|------|-------------------------------|---------------|
| モデルマネージャー（[model-manager-screen.md](model-manager-screen.md)） | 各区画（ComfyUI エンジン・モデル・Hugging Face から導入・Hugging Face のトークン・外部の ComfyUI。「試し生成」は #1100 で廃止）の説明。ローカル音声・口パクとモデルの区画の「?」には、カードをタップすると詳細（出典・ライセンス・ファイル一覧・SHA-256）が見られること・実行環境の扱い・自動起動の意味も入る（#954）。区画「モデル」の「?」の末尾には、単発生成画面へのリンク（`mm-open-gentest`。リンクはパネルに置いてよい。§9.3）も入る（#1100）。**h1 の下の説明は動かさない**（h1 に「?」は付けない。オーナーの決定） | h1・h1 の下の説明・各区画の h2・操作・状態・結果・エラー |
| 設定: API キーとログイン（[settings-screen-categories.md](settings-screen-categories.md)） | 「API キー」「ChatGPT でログイン」の説明（ChatGPT の公式・Codex・画像のカードの長い説明は、#1005 から「?」ではなくカードの「詳細」のモーダル） | h3・カードの名前・状態・要約・操作 |
| 設定: テキスト生成（LLM） | 分類の説明、反映の補足（次の呼び出しから使われる・再起動で反映される設定）、ChatGPT プランの長文（`llm-chatgpt-note`） | 「保存した割り当ては、次の呼び出しからすぐ使われます」（操作の前提。`llm-apply-note`）・固定・エラー・結果・表 |
| 設定: 画像・動画モデル | 分類の説明（`models.json` の保存先を含む）、「画像モデル（用途ごと）」「動画モデル（Quality Policy ごと）」の説明 | h2・h3・表 |
| 設定: ローカル・代替モデル | 分類の説明（`local.Description`） | h2・固定の注意・接続先のカード・エラー |
| 設定: MCP サーバー | 分類の説明（`mcp.Description`） | h2・待ち受け先・トークンの表・エラー |
| 設定: 概要 | 分類の説明 | h2・カード（各分類の状態） |
| 単発生成（[gentest-screen.md](gentest-screen.md)） | （Issue #977 ステージ 2: かんたんのプロンプトの見出しにも「プロンプトのウェイト」の「?」を足した。書き方とボタンの使い方だけで、モデルごとの効き方は語を選んだときのバーに出る。[gentest-tiers.md](gentest-tiers.md) §4）各ページのリード文（h1 の下）。作成フォームでは、モデル・プロンプト・入力画像の区画の説明、モデルごとの性質（シード・ネガティブ・参照画像）と選べない理由、パラメーター・見積り・実行の補足（Issue #906） | h1・モデルのカード（名前・出どころ・単価・選べないバッジ）・フォーム・結果・エラー |
| プロジェクトの設定 | Production Template・Quality Policy・ディスク・Brand / Safety Constraints・Brand Templates・プロジェクトバンドルの区画の説明 | h2・フォーム・状態・結果 |

初回ガイド（`organisms/onboarding/`。[onboarding-screen.md](onboarding-screen.md)）は #866 の対象外で、変えない。ただし「API キーとログイン」の区画（`credentials.Section`・`chatgpt.SectionIf`）を共有しているため、そこに入る「?」は初回ガイドにも出る（同じ部品・同じ動き。§9）。
