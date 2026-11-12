# コンポーネント仕様: 案件ガイド（独自ウィジェット: タイムライン編集・Take 比較・マスク描画）

> **状態: 規定を定めた（Issue #1043・#1044・#1045）。** Ousiass 共通デザインガイド（brand-guide）に載っていない独自ウィジェット 3 つ（`mam-timeline-editor`・`mam-take-comparator`・`mam-mask-paint`）の見た目・状態表示・操作・キーボード・文言を、この文書が**案件ガイドとして**定義する。実装との差分は §6 に洗い出した（直すのは別 Issue。§6 の各表の「Issue」欄）。

## 1. 位置づけ

- 共通ガイドの正は https://ousiassllc.github.io/brand-guide/ 。ガイドの P3「メディア・エディタ」は「独自ウィジェットは共通ガイドに置かず、そのプロダクトの案件ガイドで定義する。UI クロムにプリズムを使わない」と定めている。3 つのウィジェットはこの P3 に当たる。
- 参照した時点: 2026-11-06（要約の取り込み時点は brand-guide `73d8cf6`。色のトークンの版は [color-rules.md](color-rules.md) が `622ea73` と書いている）。食い違ったらガイドが正。
- **この文書は値を複製しない。** 色・文字・角丸・余白・動きは、トークンの名前とそれを出す Tailwind のクラス（[color-rules.md](color-rules.md) §3・§4、[shared-ui-parts.md](shared-ui-parts.md) §1）で書く。クラスの許可リストは `a11y/color_allowlist_test.go`・`shape_test.go`・`typography_test.go` が守る。
- ガイドにない部品・値・モーションは足さない。この文書が決めたのは、ガイドの既存の部品（ボタン・入力・ステータスピル・インラインアラート・トースト・テーブル・カード・スライダー・空状態）を、3 つのウィジェットのどこにどう使うかである。**共通ガイドへの追加提案は無い**（§7）。
- 対象外: `mam-video-player`（P3 の「動画」。別に決める）、`mam-comfy-frame`（iframe の枠）、`mam-prompt-weight`（Issue #1046）。
- 3 つとも Light DOM に描く（`createRenderRoot()` が `this`）。ページの Tailwind クラスがそのまま効く。ライト / ダークは `<html data-theme>` のトークンの切り替えで決まり、クラスに `dark:` は付けない。

## 2. 3 つのウィジェットに共通の規定

| # | 項目 | 決まり |
|---|------|--------|
| W1 | 色 | トークンのクラスだけ（[color-rules.md](color-rules.md) §3・§4）。面は `bg-surface`・枠は `border-border`・補助の文字は `text-mute`・選択と現在地は `border-accent`（＋`bg-accent-soft`）。意味の色は 4 トークン（`danger` `warning` `success` `info`）だけで、色だけで意味を伝えない（文言とアイコンを添える）。`#000` などの直書きはしない |
| W2 | 行・パネルの面 | 並べて操作する 1 件（クリップの行・区間の選択パネル）は、カード（`rounded-lg border border-border bg-surface`、影なし）。カードの中の入れ子の面（区間のチップ）は `rounded-sm bg-accent-soft` |
| W3 | ボタン | 共通ガイドの O7 と同じ（[shared-ui-parts.md](shared-ui-parts.md) §1「部品・オーバーレイ」）。主 = `bg-accent text-bone hover:bg-accent/85`、サブ = `border border-border text-ink hover:bg-accent-soft`。どれも `rounded-md font-semibold transition-colors motion-reduce:transition-none disabled:opacity-40`、小は `min-h-8 px-3 py-1 text-sm`（ウィジェットの中のボタンはすべて小）、押下は `data-ripple`、キーボードのフォーカスは `focus-visible:focus-ring`。行の中の補助の操作（「外す」「選択」「除外」「変更を戻す」「選び直す」）は `atoms.LinkButton` と同じ文字のボタン（`text-xs text-mute underline hover:text-ink`。リップルは付けない）で、同じく `focus-visible:focus-ring` を持つ |
| W4 | 記号だけのボタン（↑ ↓ ×） | 名前は `aria-label`（見える記号は名前ではない）。押せる大きさは 24×24px 以上（WCAG 2.5.8）。`atoms.IconButton` と同じ形（`text-mute hover:text-ink`、`data-ripple`）で `focus-visible:focus-ring`。行に同じ名前を並べない（`atoms.RowLabel` と同じ規約。[overview.md](overview.md) §4.2・§4.3） |
| W5 | 入力欄 | 枠は `border-mute`（入力欄の枠のコントラスト 3:1。[color-rules.md](color-rules.md) §4）・`rounded-md`・`text-ink`・`px-2 py-1`。地は `bg-surface`（面の上）か `bg-bone`（ページの上）で、どちらも `a11y/field_border_test` が見る。ラベルは入力の上に `text-xs text-mute`（Regular）。無効は不透明度 .4。`focus-visible:focus-ring` |
| W6 | ステータスの印 | `atoms.Badge` と同じ形（`rounded-full px-2 py-1 text-xs`＋ `bg-<状態>/20 text-<状態>`、中立は `bg-accent-soft text-ink`）。色は [shared-ui-parts.md](shared-ui-parts.md) §1「状態の種類と `Badge` の対応」に従う。**`rounded-sm` の枠だけの印や、`text-[10px]` のような 12px 未満の文字は使わない**（T3: 最小は `text-xs` 12px） |
| W7 | 数値 | 桁を揃える秒数・点数は `font-mono tabular-nums`（T5）。表の数値列は右寄せ |
| W8 | 通知 | 成功の 1 行（「保存しました」）は**トースト**（`components/toast` の `showToast()`。O3。3 秒・同時に 1 つ）。エラー・拒否・取り消せない結果・読み直して失われた編集のような**読み返す**ものは**インラインアラート**（`components/alert` の `appendAlert(#alert-region, …)`。アイコン＋見出し＋本文。閉じるまで残る。同じ文は重ねない）。ウィジェットが自分の中に通知の箱を持たない。スクリーンリーダー向けの `role="status"` の読み上げ（`data-announce`）は残す |
| W9 | 空状態・読み込み | 空状態は破線の枠（`rounded-lg border border-dashed border-border`・余白 `p-6`）に文言。次の一手があるものは文言で言う（ボタンは、そのウィジェットから押せる操作があるときだけ）。読み込み中は `text-sm text-mute` の文言（`role="status"`）。スケルトンは使わない |
| W10 | 破壊的操作 | 取り消せない操作は、OS の `confirm()` でなく中央のモーダル（`components/confirm` の `confirmDialog()`。O2）で確かめてから送る。取り消せる操作（保存前の編集）には確認を挟まない |
| W11 | 動き | 色・背景だけを `transition-colors`（`--motion-base`）で動かし、`motion-reduce:transition-none`。ドラッグ中の見た目は不透明度と枠の色だけ（拡大・移動のアニメーションは付けない） |
| W12 | ドラッグの代わり | ドラッグだけで済ませない（WCAG 2.5.7）。並べ替えは ↑ ↓ のボタン、区間の指定は言葉の入力、塗りはキーボード（矢印）が代わりになる。代わりの操作でもフォーカスが失われない（[overview.md](overview.md) §5.2） |

## 3. `mam-timeline-editor`（Issue #1043）

Edit Timeline（Rough Cut のクリップの並び・切り出し・つなぎ方・字幕）を編集する。データと保存の契約は [overview.md](overview.md) §4.2。ここは見た目と操作だけを決める。

### 3.1 構成と見た目

| 区画 | 規定 |
|------|------|
| 全体 | 縦に `gap-4`。上から、アラート（W8。保存の失敗・拒否・版の差し替え）・プレビュー（`mam-video-player`）・クリップの一覧（`<ol>`）・ツールバー |
| クリップの行 | W2 のカード（`<li>`、行の間 `gap-2`、内側 `p-3`）。上段 = 番号（`text-xs text-mute`）・サムネイル（高さ 40・`rounded-md`・装飾なので `alt=""`）・名前（`text-sm`）と目的（`text-xs text-mute`）・開始秒と長さ（W7）・↑ ↓ 外す。下段 = 開始（秒）・終了（秒）・Take の長さ・前のクリップからの切り替えと長さ（先頭のクリップには切り替えが無い）。最下段 = 字幕（Caption）の `<textarea>`（2 行） |
| 追加 Shot の印 | `kind` が `insert` / `reaction` のクリップだけ、名前の横に W6 のバッジ（`BadgeInfo`。「インサート」「リアクション」） |
| ドラッグ | 行全体が `draggable`。ドラッグ中の行は不透明度 .5、落とす先の行は `border-accent`。編集できない間（`locked`）は `draggable=false` |
| ツールバー | 保存（主ボタン）・変更を戻す（W3 の補助）・合計の表示（W7。「合計 12.3 秒 / 4 クリップ」）。未保存の変更があるとき、合計の横に `BadgeInfo`「未保存の変更あり」。編集できない段階では `BadgeWarn`「この段階では編集できません」（理由の言葉を含める） |

### 3.2 状態

| 状態 | 見せ方 |
|------|-------|
| 読み込み中 | 「読み込み中…」（W9） |
| 初回の読み込みに失敗（404 以外） | 失敗の理由は `#alert-region` のエラーのアラート。ウィジェットには破線の枠（W9。`data-load-failed`）に「タイムラインを読み込めませんでした。理由は通知に出ています。」と、**「もう一度読み込む」**ボタン（`data-action="retry"`。W3 のテキストボタン）を出す（画面を開き直さずにやり直せる）。押すと「読み込み中…」（`role="status"`）を出して読み直し、成功すればタイムライン、404 なら空状態に替わる。また失敗したときはボタンが残り、フォーカスもボタンに戻す。空状態の文言にはしない |
| Rough Cut が無い（404） | 空状態（W9）。「Rough Cut がまだありません。各 Shot の採用 Take を選んでから「Rough Cut を作成」を押してください。」。エラーにしない |
| 編集できる | 全部の操作が有効。保存と「変更を戻す」は、未保存の変更があるときだけ有効 |
| 編集できない（`readonly`・保存中） | 行の入力・ボタンはすべて無効（不透明度 .4）、ドラッグ不可。`readonly` のときだけ `BadgeWarn` を出す（保存中は出さない） |
| 未保存の変更あり | `BadgeInfo`「未保存の変更あり」。保存と「変更を戻す」が有効になる |
| 保存した | トースト「保存しました」（W8）。`timeline-updated` を発火 |
| 保存は済んだが再読込に失敗 | インラインアラート（エラー）。文言は [overview.md](overview.md) §4.2 のとおり |
| 入力を拒否した（最短秒数・字幕の長さなど） | インラインアラート（エラー）「<クリップ名>: <理由>」。欄には最後に受け付けた値を戻す（字幕だけは入力した文を残し、短くできるようにする） |
| 新しい版に差し替わって未保存の編集を破棄した | インラインアラート（注意）「最新の版に差し替わったため、未保存の変更は破棄されました」（成功ではないので成功色にしない） |

### 3.3 操作とキーボード

- **並べ替え**: ドラッグ、または各行の ↑ ↓（先頭は ↑、最後は ↓ が無効）。動かしたあとフォーカスは動かしたクリップの同じボタンに残る（端に着いたときは反対のボタン）。読み上げ用に `data-announce`（`role="status"`、画面外）へ「<名前> を n 番目へ移動しました」。
- **外す**: クリップを一覧から外す（残りが 1 つのときは無効）。確認は挟まない（保存するまで何も消えず、「変更を戻す」で戻せる。W10）。外したあと、フォーカスは繰り上がったクリップの「外す」へ。「<名前> を外しました（n クリップ）」を読み上げる。
- **切り出し・つなぎ方・字幕**: 欄を確定（`change`）すると反映。範囲外は拒否して理由を出す（§3.2）。サーバー（`domain.EditTimeline.Normalize`）が最終判定する。
- **保存**: ボタン。保存後、保存が成功したときはフォーカスを一覧へ置く（トーストはフォーカスを受けない）。失敗したときは保存ボタンに残る。
- **変更を戻す**: 未保存の編集を捨てて最新の版を読み直す。**取り消せないので確認のモーダルを出す**（W10。「未保存の変更を破棄して、最新の版を読み直しますか？」）。
- **キーボードのショートカットは持たない。** Tab の順は、プレビュー → 各行（↑ ↓ 外す・開始・終了・切り替え・長さ・字幕）→ 保存 → 変更を戻す。

### 3.4 文言

文言は実装のとおり（[overview.md](overview.md) §4.2）で、この文書では足さない。足すのは §3.3 の「変更を戻す」の確認の 1 文だけ。ボタンの名前は「<名前> を上へ」「<名前> を下へ」「外す: <名前>」。

## 4. `mam-take-comparator`（Issue #1044）

Shot の Take のスコアを比べ、1 つを選ぶ・除外する、また複数の Take の区間をつないで 1 本にする（Partial Take Use）。契約は [overview.md](overview.md) §4.3。

### 4.1 構成と見た目

| 区画 | 規定 |
|------|------|
| 全体 | 縦に `gap-4`。上から、アラート（W8）・スコア表・区間で採用（`partial_url` があるときだけ） |
| スコア表 | テーブル（`w-full text-sm`、横にあふれたら箱の中でスクロール）。見出し行は `text-xs font-semibold text-mute`、行は `border-t border-border`。列は Take・Product・Acting・Camera・Continuity・Status・操作（見出しは画面外）。行の見出し（`<th scope="row">`）に「Take A」、使用中の Take は `BadgeOK`「使用中」、その下に `text-xs text-mute` で「モデル · 解像度 · fps（· 4K（プロキシで再生））」。除外した Take の行は文字を `text-mute` |
| スコア | 数値は W7（`font-mono tabular-nums`、右寄せ）。基準（`limits.min_axis_score`）を下回る値は `text-danger`＋「▼」＋読み上げ用「（基準 N 未満）」。スコアが無いときは「—」＋読み上げ用「スコアなし」。基準が無いとき（未取得）は赤も ▼ も出さない |
| Status | `atoms.Badge`（Shot カードの `molecules.TakeStatusBadge` と同じ色: 候補 = `BadgeWarn`・AI Pick = `BadgeInfo`・採用 = `BadgeOK`・除外 = `BadgeNeutral`）。文字は `status_label`（サーバーが返す。クライアントは持たない）。除外は取り消し線を使わず、行の `text-mute` と文言で伝える。Judge の判定（PASS / FAIL）は印の隣の `text-xs text-mute` |
| 行の操作 | 「選択」「除外」は W3 の補助（`atoms.LinkButtonFor` と同じ形。名前は「選択: Take A」「除外: Take A」）。出すかどうかは `select_url` / `reject_url` の有無（`status` は見ない） |
| 区間で採用 | `<section aria-label="区間指定">`、見出し「区間で採用（Partial Take Use）」。Take ごとに、「Take A」・`mam-video-player`（`max-w-md`）・区間を選ぶレーン |
| レーン | 高さ 28（`h-7`）・`rounded-md bg-accent-soft`・`cursor-crosshair`。幅は Take の長さを最長の Take（または Shot の計画尺）に対する割合にして、長さの違いが見えるようにする。選んだ区間 = `bg-success/70`（番号は `text-xs text-bone`）、ドラッグ中 = `bg-info/70`（どちらも [color-rules.md](color-rules.md) §4 の重ね塗りの例外）。`role="group"`・`aria-label="Take A の区間を選ぶ"` |
| 選択パネル | W2 のカード（`tabindex="-1"`。フォーカスの戻し先）。選んだ区間が無いときは「Take の帯をドラッグして区間を選ぶと、選んだ順につなぎます。」。あるときは区間のチップ（`rounded-sm bg-accent-soft`、「Take A の 0.0-2.0秒」と、名前付きの × ボタン「区間 n を外す」）と「合計 N 秒（つなぎ目はクロスフェード）」 |
| 合成のボタン | 「この区間で合成」（主）= 区間が 2 つ以上・処理中でないときだけ有効。「選び直す」（W3 のテキストボタン）= 区間が無いと無効。「言葉で合成」（サブ）= 入力が空・処理中のとき無効 |
| 言葉の入力 | `<input type="text" name="instruction">`（W5。`aria-label="区間を言葉で指定"`。例「Take A の 0-2秒 + Take B の 2-4秒」）。解釈はサーバー。**レーンのドラッグに代わる入力**（W12） |

### 4.2 状態

| 状態 | 見せ方 |
|------|-------|
| 読み込み中 | 「読み込み中…」（W9） |
| Take が無い | 空状態（W9）「この Shot にはまだ Take がありません」 |
| 処理中（選択・除外・合成の送信中） | 行のボタンと合成のボタンが無効。終われば Take を読み直す |
| 選択した | 読み直して Status が変わる（トーストは出さない: 画面の変化が結果） |
| 除外する前 | 確認のモーダル「この Take を除外しますか？」（W10。Shot カードの `hx-confirm` と同じ文言）。断られたら送らない。除外は取り消せない |
| 区間を足せない（上限・短すぎる） | インラインアラート（注意）「区間は最大 N 個までです」「N 秒以上の区間をドラッグしてください」（N は `limits`。クライアントは値を持たない） |
| 合成の成功 | `partial-take-confirmed` を発火して Take を読み直す。選んだ区間と入力を空にする |
| 失敗（サーバーの拒否・通信） | インラインアラート（エラー） |
| `partial_url` が無い（ffmpeg が使えない・切り出せる Take が無い） | 区間で採用の区画ごと出さない |

### 4.3 操作とキーボード

- **選択 / 除外**: 行のボタン（Tab で行き、Enter / Space）。押したあと、フォーカスは同じ行の同じボタン、無ければもう一方、行の見出し、ウィジェットの順（`take-focus.ts`）。
- **区間の指定**: レーン上のポインタのドラッグ（フレームに丸め、選んだ順に番号を付ける）。**キーボードだけの人は、言葉の入力で指定する**（W12。ドラッグをキーボードで再現するコントロールは持たない）。区間のチップの × で 1 つ外し、「選び直す」で全部外す。外したあとフォーカスは次の区間の × （最後を外したときは前の × ）、全部外したときは選択パネル。
- **合成**: 「この区間で合成」または「言葉で合成」（言葉の入力で Enter でも送れる）。処理が終わったらフォーカスは合成のボタン（言葉のときは入力欄）。
- **Tab の順**: スコア表の各行（選択 → 除外）→ 各 Take のプレイヤーのコントロール → 選択パネル（区間の ×・この区間で合成・選び直す・言葉の入力・言葉で合成）。レーンは Tab で止まらない（ポインタ専用）。

### 4.4 文言

実装のとおりで、この文書では足さない。ボタンの名前は「選択: Take A」「除外: Take A」「区間 n を外す」。スコアの軸の名前（Product / Acting / Camera / Continuity）は Judge の軸の名前で、翻訳しない。

## 5. `mam-mask-paint`（Issue #1045）

単発生成の「一部だけ描き直す」の、描き直す部分を塗るマスクの部品（[gentest-screen.md](gentest-screen.md)）。白が描き直し、黒が残す。契約（キャンバスの大きさ・`mask-src`・`htmx:confirm` での PNG の差し込み）は [gentest-screen.md](gentest-screen.md) と `mask-size.ts`。

### 5.1 構成と見た目

| 区画 | 規定 |
|------|------|
| 全体 | 縦に `gap-2`。上から、道具（筆・消しゴム・全部消す）・太さ・画像とキャンバス |
| 道具のボタン | W3 の補助。選んでいる道具（筆 / 消しゴム）は `aria-pressed="true"`＋ `border-accent bg-accent-soft`（色だけでなく `aria-pressed` が状態）。「全部消す」は押しても選択状態にならない（`aria-pressed` を持たない）。`role="group"`・`aria-label="描き方"` |
| 太さ | 共用のスライダー（[color-rules.md](color-rules.md) §4.1。1〜128）。ラベル「太さ」の横に、現在の値を `<output>`（W7）で出す |
| 画像とキャンバス | 画像の上に同じ大きさでキャンバスを重ねる（画像は、マスクのキャンバスの大きさに縮めた写しを描いた装飾のキャンバス `aria-hidden="true"`。巨大画像を元の大きさで持たないため `<img>` は使わない。Issue #999）。塗った所は**赤の重ね塗り**で見せる（不透明度 .55）。赤は専用のトークンを読む（明るい写真は `--overlay-deep`、暗い写真は `--overlay-pale`。[color-rules.md](color-rules.md) §4.2）。直書きの原色は使わない。書き出すマスクは黒（残す）と白（描き直す）の PNG で、これは画面の色ではなくデータ（トークンの対象外） |
| 画像が無いとき | 空状態（W9）「画像を選ぶと、描き直す部分を塗れます」。キャンバスは出さない |

### 5.2 状態

| 状態 | 見せ方 |
|------|-------|
| 画像を読み込む前 | 空状態（§5.1） |
| 塗れる | キャンバスが `tabindex="0"`・`aria-label="描き直す部分。白く塗ったところが描き直されます"`。キーボードのフォーカスで `focus-ring` |
| 前のマスクから始める（`mask-src`） | 画像を読み込んだあと、その白を赤で重ねた状態で始まる |
| 道具の選択 | 筆 / 消しゴムの片方が `aria-pressed="true"`（初期は筆） |
| 大きな画像 | 長辺 2048 に縮めたキャンバスに塗る（見た目は同じ。書き出すマスクもその大きさ） |

### 5.3 操作とキーボード

- **塗る**: ポインタのドラッグ（前の位置から丸い端の線でつなぎ、速くても点線にならない）。1 クリックで丸 1 つ。消しゴムは赤を消してマスクを黒に戻す。
- **キーボード**（キャンバスにフォーカスがあるとき）: `B` = 筆、`E` = 消しゴム、`[` / `]` = 太さを 2 減らす / 増やす、矢印 = 現在の位置から太さ 1 つ分の線を引く（画像の内側に収める）。いずれもブラウザの既定（スクロール）は止める。**キーはボタンの `aria-keyshortcuts` で知らせる**（筆 = `B`、消しゴム = `E`）。
- **全部消す**: 塗りを全部黒に戻す。**取り消せないので確認のモーダルを出す**（W10。「塗った部分をすべて消しますか？」）。
- **送信**: 親のフォームが送られる直前（`htmx:confirm`）に、`input[name=mask]` へ PNG を入れてから送る。

### 5.4 文言

実装のとおり（筆・消しゴム・全部消す・太さ・描き方）。足すのは §5.1 の空状態の 1 文と、§5.3 の「全部消す」の確認の 1 文だけ。

## 6. 実装との差分（2026-11-06 時点。直すのは別の Issue）

実装を §2〜§5 に照らして洗い出した。**この Issue（#1043〜#1045）は規定を決めるだけで、コードは直さない。** 「Issue」欄の番号が、直す先。

### 6.1 `mam-timeline-editor`

| # | 規定 | 実装（`static/src/components/timeline-editor/`） | Issue |
|---|------|------|-------|
| TL-1 | W3・W4 フォーカスの輪・小ボタンの大きさ・リップル・文字の大きさ | ↑ ↓ 外す 保存 変更を戻す のどれにも `focus-visible:focus-ring` が無い（`timeline-clip.ts:56-58`、`timeline-toolbar.ts:22,28`）。「保存」は `min-h-8` が無く `text-xs`、リップルが無い。↑ ↓ は `px-1` で 24px に届かず、リップルが無い。`transition-colors` と `motion-reduce:transition-none` が無い | #1091（解消） |
| TL-2 | W5 入力欄のフォーカスの輪 | 数値・切り替え・字幕の入力に `focus-visible:focus-ring` が無い（`timeline-clip.ts:63,67,75,84,96`） | #1091（解消） |
| TL-3 | W6 追加 Shot の印 | `rounded-sm bg-info/20 px-2 py-1 text-[10px]`（`timeline-clip.ts:51`）。`BadgeInfo` の形（`rounded-full … text-xs`）にする | #1091（解消） |
| TL-4 | W2 行のカード | 行の `<li>` に `bg-surface` が無い（`timeline-clip.ts:35`） | #1091（解消） |
| TL-5 | W7 数値 | 開始秒・長さ・合計・Take の長さが `font-mono tabular-nums` でない（`timeline-clip.ts:55,69`、`timeline-toolbar.ts:31`） | #1091（解消） |
| TL-6 | §3.1 ツールバーの状態の印 | 「未保存の変更あり」は合計の文の括弧書き（`timeline-toolbar.ts:31`）、「この段階では編集できません」は `text-warning` の文字だけ（`:32`）。`BadgeInfo` / `BadgeWarn` にする | #1091（解消） |
| TL-7 | W8 通知 | 「保存しました」を `text-success` の行で出す（`mam-timeline-editor.ts:244`）。トーストにする。エラーも自分の箱（`border-danger/60 bg-danger/10 text-xs`、アイコン無し。`:264`）で、`#alert-region` のインラインアラート（アイコン＋見出し）にする。トーストにすると `focusToolbar` の戻し先の `[data-notice]`（`timeline-focus.ts:34-39`）は一覧に変える | #1091（解消） |
| TL-8 | §3.2 版の差し替えの通知 | 未保存の変更を破棄した通知を成功色のメモ（`data-notice`）で出す（`mam-timeline-editor.ts:99,244`）。注意のインラインアラートにする | #1091（解消） |
| TL-9 | W9 空状態 | 「Rough Cut がまだありません…」が枠の無い `text-sm text-mute`（`mam-timeline-editor.ts:237`）。破線の枠にする | #1091（解消） |
| TL-10 | W10 / §3.3 変更を戻す | 確認なしで未保存の編集を捨てる（`mam-timeline-editor.ts:256`）。確認のモーダルを挟む | #1091（解消） |

**TL-1〜TL-10 は Issue #1091 で解消した**（実装を規定に合わせた。差分として残したものは無い）。表の「実装」欄は解消前の状態の記録で、直した先は次のとおり。

- TL-1・TL-2・TL-4・TL-5: `timeline-clip.ts`・`timeline-toolbar.ts` の各ボタン・入力欄に `focus-visible:focus-ring`・`transition-colors`・`motion-reduce:transition-none`、↑ ↓ は `min-h-6 min-w-6`＋`data-ripple`、「保存」は `min-h-8 text-sm`＋`data-ripple`、行に `bg-surface`、秒数は `font-mono tabular-nums`
- TL-3・TL-6: 追加 Shot の印・「未保存の変更あり」・「この段階では編集できません」は `rounded-full px-2 py-1 text-xs` のバッジ（`data-kind` / `data-dirty` / `data-readonly`）。合計の文は括弧書きを持たない
- TL-7・TL-8: 成功は `showToast('保存しました')`、エラーと拒否は `#alert-region` のエラーのアラート、未保存の編集を破棄した旨は注意のアラート（`alert.ts` の `appendAlert(region, message, 'warning')`・`AppLayout` の `#alert-warning-template`）。エディタの中の通知の箱（`data-error`・`data-notice`）は無くした。保存成功後のフォーカスの戻し先は一覧（`timeline-focus.ts` の `focusToolbar`）
- TL-9: 空状態は `rounded-lg border border-dashed border-border p-6`（`data-empty`）
- TL-10: 「変更を戻す」は `confirmDialog()` で確かめ、キャンセルなら何も読み直さない

### 6.2 `mam-take-comparator`

| # | 規定 | 実装（`static/src/components/take-comparator/`） | Issue |
|---|------|------|-------|
| TC-1 | W3・W4・W5 フォーカスの輪・小ボタンの大きさ・文字の太さ・リップル | 「選択」「除外」「選び直す」「×」「この区間で合成」「言葉で合成」と言葉の入力のどれにも `focus-visible:focus-ring` が無い（`take-table.ts:52,57`、`take-selection.ts:29,36,39,45,51`）。「この区間で合成」「言葉で合成」は `min-h-8` が無く `text-xs`、「言葉で合成」は `font-semibold` も無い。リップルが無い。「×」は 24px に届かない | #1092（解消） |
| TC-2 | W6 Status | `rounded-sm border px-2 py-1 text-xs`＋文字色（`take-status.ts`、`take-table.ts:43`）。候補は無彩色、除外は取り消し線。Shot カードの `TakeStatusBadge` と同じ `Badge`（候補 = 警告・AI Pick = 情報・採用 = 成功・除外 = 中立）にし、取り消し線をやめる。「使用中」も `BadgeOK`（現状は `text-xs text-success` の文字。`take-table.ts:35`） | #1092（解消） |
| TC-3 | W6 レーンの番号 | 選んだ区間の番号が `text-[10px] leading-7`（`mam-take-comparator.ts:285`）。12px（`text-xs`）にする | #1092（解消） |
| TC-4 | W7 スコア | スコアが左寄せで `font-mono tabular-nums` でない（`take-table.ts:38-41`、`take-cells.ts`）。右寄せにし、列の見出しも右寄せにする | #1092（解消） |
| TC-5 | W8 通知 | エラーを自分の箱（`border-danger/60 bg-danger/10 text-xs`、アイコン無し。`mam-take-comparator.ts:249`）で出す。`#alert-region` のインラインアラートにする。区間の上限・短すぎる区間（`:187-191`）は注意のアラート | #1092（解消） |
| TC-6 | W9 空状態 | 「この Shot にはまだ Take がありません」が枠の無い文（`mam-take-comparator.ts:200`）。破線の枠にする | #1092（解消） |
| TC-7 | W2 選択パネル | カードの面（`bg-surface`）が無い（`take-selection.ts:23`） | #1092（解消） |

**TC-1〜TC-7 は Issue #1092 で解消した**（実装を規定に合わせた。差分として残したものは無い）。表の「実装」欄は解消前の状態の記録で、直した先は次のとおり。

- TC-1・TC-7: 「選択」「除外」「選び直す」は `text-xs text-mute underline hover:text-ink focus-visible:focus-ring`（リップルなし）。「この区間で合成」「言葉で合成」は `min-h-8 text-sm font-semibold`＋`transition-colors motion-reduce:transition-none`＋`data-ripple`＋`focus-visible:focus-ring`、「×」は `min-h-6 min-w-6`＋`data-ripple`、言葉の入力は `text-ink focus-visible:focus-ring`、選択パネルは `bg-surface` のカード
- TC-2・TC-3: Status は `rounded-full px-2 py-1 text-xs`＋ Shot カードの `TakeStatusBadge` と同じ色（候補 = `bg-warning/20 text-warning`・AI Pick = `bg-info/20 text-info`・採用 = `bg-success/20 text-success`・除外 = `bg-accent-soft text-ink`。取り消し線は無し）。「使用中」は `bg-success/20` のバッジ（括弧は無し）。レーンの区間の番号は `text-xs`
- TC-4: スコアのセルは `text-right font-mono tabular-nums`、Product〜Continuity の列の見出しも右寄せ
- TC-5: エラーは `#alert-region` のエラーのアラート、区間の上限・短すぎる区間は注意のアラート（`appendAlert(region, message, 'warning')`）。ウィジェットは通知の箱を持たない
- TC-6: 空状態は `rounded-lg border border-dashed border-border p-6`（`data-empty`）

`mam-take-comparator-look.wtest.ts`（実際の Tailwind CSS で描く）が、Status のバッジの形と色・スコアの右寄せと等幅・フォーカスの輪（`:focus-visible` の outline）・ボタンの高さとリップル・選択パネルのカード・空状態の破線を確かめている。


### 6.3 `mam-mask-paint`（#1093。MP-3 は一部解消）

| # | 規定 | 実装（`static/src/components/mask-paint/mam-mask-paint.ts`） | Issue |
|---|------|------|-------|
| MP-1 | W3 道具のボタン | `min-h-8` が無く、リップル（`data-ripple`）・`motion-reduce:transition-none` が無い（`:8-10`） | #1093（解消） |
| MP-2 | §5.1 太さの値 | スライダーに値の表示（`<output>`）が無い（`:271-282`）。[shared-ui-parts.md](shared-ui-parts.md) §1 のスライダーの決まり（`<output>` を横か上に置く）にも反する | #1093（解消） |
| MP-3 | §5.1 赤の重ね塗り | 重ね塗りの色を `#f33` と `0xff 0x33 0x33` で直書きしている（`:112-114,192`）。`--danger` を読む。**ライト・ダークの写真の上で見えるかは、直すときに確かめる**（見えない場合は、共通ガイドへの追加を先に提案する） | #1093（解消: 重ね塗り専用のトークン `--overlay-deep`・`--overlay-pale` を足し、写真の明るさで選ぶ。下の「MP-3 の見え方」） |
| MP-4 | §5.2 キャンバスのフォーカス | `tabindex="0"` のキャンバスに `focus-visible:focus-ring` が無い（`:285-289`） | #1093（解消） |
| MP-5 | §5.3 キーの知らせ | キーボードの `B` / `E` / `[` / `]` / 矢印が画面のどこにも出ていない（`:259-262`）。ボタンに `aria-keyshortcuts` を付ける | #1093（解消） |
| MP-6 | W10 / §5.3 全部消す | 確認なしで塗りを全部消す（`:152-155,269`）。確認のモーダルを挟む | #1093（解消） |
| MP-7 | W9 画像が無いとき | `src` が空のとき、64×64 の空のキャンバスだけが出る（`:73,284-288`）。空状態の文言にする | #1093（解消） |

**MP-1〜MP-7 は Issue #1093 で解消した**（実装を規定に合わせた。差分として残したものは無い。MP-3 の色は共通ガイドに専用トークンを足した）。表の「実装」欄は解消前の状態の記録で、直した先は次のとおり。

- MP-1・MP-5: 道具のボタンは `min-h-8 px-3 text-sm`＋`motion-reduce:transition-none`＋`data-ripple`（選んでいる道具は `border-accent bg-accent-soft`、そうでなければ `border-border`）。筆に `aria-keyshortcuts="B"`、消しゴムに `E`
- MP-2: ラベル「太さ」の横、スライダーの右に `<output class="font-mono tabular-nums">`（`data-size`）。キー（`[` `]`）でもスライダーでも更新される
- MP-3: 赤の重ね塗りは、写真の明るさで選ぶ専用トークンを塗る時（筆・前のマスクの重ね）に `getComputedStyle(this).getPropertyValue(...)` で読む。写真を縮めた 16 × 16 の写しの平均の相対輝度（WCAG。線形光）が 0.11 より暗ければ `--overlay-pale`（`#ff8c96`）、そうでなければ `--overlay-deep`（`#5a0000`。読めない写真もこちら）。トークンは [color-rules.md](color-rules.md) §4.2。直書きの `#f33`・`0xff 0x33 0x33` と `--danger` は使わない。書き出すマスクは今までどおり黒と白のデータ。**MP-3 の見え方（確かめた結果）:** 重ね塗りは不透明度 .55 の半透明で、色が 1 つなら下地に対して 3:1 を保てない下地が必ずできる（中間のグレーでは、.55 のどんな色でも 3:1 に届かない）。そこで色を 2 つに分け、`--overlay-deep` は明るい下地（白 4.0:1・明るい壁 3.6:1・空色 3.5:1）、`--overlay-pale` は暗い下地（ほぼ黒 3.3:1・黒 3.3:1）で 3:1 以上を保つ（`backend/internal/web/a11y/overlay_test.go` が確かめる。ライト・ダークのテーマで値は同じ。重ね塗りは写真の上にあり、画面の面の上ではない）。切り替えは、2 つが同じ見え方になる輝度（0.11、グレーの 96 あたり）。**切り替えの付近（輝度 0.02〜0.4 の中間調の写真）は 3:1 に届かない**（深い色は輝度 0.2 のグレーで 2.3:1、淡い色は輝度 0.02 のグレーで 3.0:1。一枚の写真に明るい所と暗い所が混ざるときも、片方の所は弱い）。色相は赤のままなので塗った所は見分けられるが、コントラストの 3:1 は保証しない。これ以上は、塗りの不透明度を上げる（写真が見えにくくなる）か、縁取りを足す（部品を変える）ことになるので、今回はしない
- MP-4: 輪は、キャンバスを包む枠（`data-stage`。`has-[canvas:focus-visible]:focus-ring`）に付ける。**キャンバス自身は不透明度 .55 で描くので、キャンバスに輪を付けると輪まで薄くなり**、`focus-visible:outline-none` で自身の既定の輪を消している。枠は画像の大きさに合わせる（`self-start`。以前は列の幅いっぱいに伸びて、重ね塗りが画像からずれた）
- MP-6: 「全部消す」は `confirmDialog('塗った部分をすべて消しますか？')` で確かめ、キャンセルなら塗りを残す。画像が無いとき（空状態）は消す塗りが無いので、ボタンは `disabled`（`disabled:opacity-40`）にして、何もしない確認を出さない
- MP-7: `src` が空のときは `rounded-lg border border-dashed border-border p-6` の空状態（`data-empty`）「画像を選ぶと、描き直す部分を塗れます」で、キャンバスは出さない（道具と太さは出したまま）。書き出すマスクは `width`×`height` の黒のまま（フォームは送れる）。画像を選ぶと（`src` が入ると）キャンバスに替わる

`mam-mask-paint.wtest.ts`・`mam-mask-paint-look.wtest.ts` が、空状態・`<output>`・ボタンの高さとリップルと `aria-keyshortcuts`・枠のフォーカスの輪・写真の明るさで選ぶ `--overlay-deep` / `--overlay-pale` の色の重ね塗り（と、切り替えの輝度）・「全部消す」の確認（はい / いいえ）・枠と重ね塗りが画像に合うことを確かめている。


## 7. ガイド未収載・逸脱

| 部品 | 扱い | 定義の場所 |
|------|------|-----------|
| タイムライン編集（`mam-timeline-editor`） | 案件固有（P3 の独自ウィジェット） | この文書の §3 |
| Take 比較（`mam-take-comparator`） | 案件固有（P3 の独自ウィジェット） | この文書の §4 |
| マスク描画（`mam-mask-paint`） | 案件固有（P3 の独自ウィジェット） | この文書の §5 |

共通ガイドへ追加したもの: マスクの重ね塗りの色（§6.3 の MP-3。専用トークン `--overlay-deep`・`--overlay-pale`。[color-rules.md](color-rules.md) §4.2）。ガイドからの**逸脱: なし**（`bg-success/70`・`bg-info/70` の重ね塗りは [color-rules.md](color-rules.md) §4 の例外として承認済み）。

## 8. 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-06 | 新規作成。3 つのウィジェットに共通の規定（§2）と、タイムライン編集（§3）・Take 比較（§4）・マスク描画（§5）の見た目・状態・操作・文言を決め、実装との差分（§6）を洗い出した | Issue #1043・#1044・#1045（ガイド未収載の独自ウィジェットの案件ガイド定義） |
| 1.1 | 2026-11-06 | §5.1 マスク描画の「画像とキャンバス」: 元画像を `<img>` でなく、マスクのキャンバスの大きさに縮めた写しの装飾キャンバスで出すことにした | Issue #999（巨大画像でのタブのメモリ） |
| 1.2 | 2026-11-10 | 画面名・機能名の「生成テスト」を「単発生成」に改めた（本文の文言だけ。仕様は変えない。過去の改訂履歴の文は当時のまま） | Issue #1137 |
| 1.3 | 2026-11-12 | §6.1 の TL-1〜TL-10 を解消済みにし、直した先を書いた（`mam-timeline-editor` のフォーカスの輪・バッジ・数値・トースト・アラート・空状態・確認）（§3.2 の注意のアラートは `#alert-warning-template`） | Issue #1091 |
| 1.4 | 2026-11-12 | §6.3 の MP-3 を解消済みにした（重ね塗りの専用トークン `--overlay-deep`・`--overlay-pale` を足し、写真の明るさで選ぶ。中間調の写真は 3:1 に届かないことを書いた）。§3.2 に、タイムライン編集の初回の読み込みに失敗したときの「もう一度読み込む」を足した | Issue #1093・#1091 |
