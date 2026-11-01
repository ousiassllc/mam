# コンポーネント仕様: 色の決まり（用途 → 許可する Tailwind のクラス）

> **状態: 実装済み。** Issue #867。**§3 の表はオーナー確認済み（2026-10-28。Issue の「用途 → 許可クラス（案）」の表をそのまま確定した）。** 画面ごとに色のトーンがばらつく（同じ意味なのにシェードや `/opacity` が違う）のを止めるため、用途ごとの許可クラスを 1 か所に決める。実装は `backend/internal/web/a11y/color_allowlist_test.go` がこの表を機械的に守らせる。

[shared-ui-parts.md](shared-ui-parts.md) §1「見た目の決まり」は、ページ・カード・補助の文字・フォーカスの輪など、一部の用途の色を決めている。この文書はそれを**網羅した**ものにする（§1 から参照する）。[overview.md](overview.md) §3 のコンポーネントはすべてこの表に従う。

## 1. 方針

- **新しいデザイントークンは作らない。** CSS カスタムプロパティ・`DESIGN.md`・`tailwind.config` は置かない。`static/src/css/app.css` は `@import "tailwindcss"` と `@source` だけのままにし、Tailwind 4 の既存のパレットから選ぶ。
- 色相は **neutral / indigo / red / amber / emerald / sky**（＋ `white` / `black`）だけ。意味は色相で決める: **危険 = red・警告 = amber・成功 = emerald・情報／進行中 = sky・主アクション／リンク／選択 = indigo・それ以外 = neutral**。意味のある色を neutral に潰さない。
- **同じ意味に新しいシェードや `/opacity` を足さない。** 文字は `-300`、枠は `-500/60`、薄い面は `/10`（弱い）か `/20`（バッジのような塗り）。足したいときは、画面のクラスを変えるのでなく、この文書の表を変える（テストの `colorAllowlist` も同じ版で変える）。
- 色だけで意味を伝えない（[shared-ui-parts.md](shared-ui-parts.md) §1「状態」。#790・#795）。文字・`Badge` の文言を添える。
- コントラストは `a11y/contrast_test.go`（補助の文字に `text-neutral-500` を使わない）と `a11y/field_border_test.go`（入力欄の枠は 3:1 以上）が守る。**`text-neutral-500` は許可リストにも例外にも入れない。**
- 共通部品が表の正: 色は `atoms.Badge`（`BadgeNeutral`・`BadgeInfo`・`BadgeWarn`・`BadgeOK`・`BadgeError`）と `atoms.Button`（`TonePrimary`・`ToneDanger`・`ToneNeutral`）から決まる。画面・molecules・organisms・Lit は、同じ意味ならこれと同じクラスを使う。

## 2. 検査の範囲

`TestSources_ColorUtilitiesAreInTheAllowlist`（`backend/internal/web/a11y/color_allowlist_test.go`）が、次のファイルの色のクラスを 1 行ずつ調べ、§3・§4 に無いクラスがあれば `file:line` とクラス名を出して落ちる。

- `backend/internal/web/**/*.templ`・`backend/internal/web/**/*.go`（`*_test.go`・生成された `*_templ.go`・`testdata/` を除く）。`format.go` や `settings_format.go` のように Go の文字列でクラスを返すものを含む。
- `static/src/**/*.ts`・`static/src/**/*.css`（`*.test.ts`・`*.wtest.ts`・`testdata/`・`dist/` を除く）。
- 色のクラス: パレットの色（`bg-` `text-` `border-` `border-t-` `divide-` `ring-` `ring-offset-` `outline-` `fill-` `stroke-` `accent-` など）、`white` / `black` / `transparent` / `current`、任意の値の色（`bg-[#123456]`・`text-[rgb(…)]`）。`text-[10px]` のような大きさは色でない。**バリアント付き**（`hover:`・`focus-visible:`・`backdrop:`・`has-[…]:` など）は、バリアントまで含めた形で §3・§4 と一致させる。
- hex（6・8 桁）・`rgb(`・`hsl(`・`oklch(` などの直書きも落とす（コメントの Issue 番号 `#865` は 3 桁なので対象外）。

`TestColorRulesSpec_MirrorsTheTests` が、§3・§4 の表とテストの一覧が 1 クラスずつ一致することを確かめる（表だけ・テストだけを変えると落ちる）。

## 3. 用途 → 許可するクラス（2026-10-28 オーナー確認済み）

<!-- color-allowlist:begin -->

| 用途 | 許可するクラス | 根拠の目安 |
|------|---------------|-----------|
| ページ背景・本文 | `bg-neutral-950` / `text-neutral-100` | §1・`AppLayout` |
| カード・モーダルの面 | `bg-neutral-900` / `border-neutral-800`（モーダル枠は `border-neutral-700` も可） | §1 |
| 主要な文字 | `text-neutral-100` | §1 |
| 補助・説明の文字 | `text-neutral-400`（text-neutral-500 は不可） | §1・`contrast_test` |
| やや弱い本文・表セル | `text-neutral-300` / `text-neutral-200` | 出現が多い既存 |
| 枠・区切り | `border-neutral-800` / `border-neutral-700` / `divide-neutral-800` | §1・一覧 |
| ホバー面 | `hover:bg-neutral-800` / `hover:border-neutral-600` / `hover:bg-neutral-800/60` | §1 |
| フォーカス輪 | `focus-visible:ring-indigo-400` + `focus-visible:ring-offset-neutral-950`（モーダル内は `focus-visible:ring-offset-neutral-900`）。チャットの添付は `peer-focus-visible:ring-indigo-400` | §1 |
| 主アクション（実ボタン） | `bg-indigo-500` / `hover:bg-indigo-400` / `text-white` | `TonePrimary`・§1 |
| リンク・アイコン強調 | `text-indigo-300` | §1・`Icon` 周り |
| 危険 | 文字 `text-red-300`、枠/面 `border-red-500/60`・`bg-red-500/10`・`bg-red-500/20`（Badge）・`hover:bg-red-500/10`（`ToneDanger` のホバー） | `ToneDanger`・`BadgeError` |
| 警告 | 文字 `text-amber-300`、面 `bg-amber-500/20`（Badge） | `BadgeWarn` |
| 成功 | 文字 `text-emerald-300`、面 `bg-emerald-500/20`（Badge） | `BadgeOK` |
| 情報・進行中 | 文字 `text-sky-300`、面 `bg-sky-500/20`（Badge） | `BadgeInfo` |
| 中立バッジ | `bg-neutral-700` / `text-neutral-300` | `BadgeNeutral` |
| 無効・弱い操作 | `border-neutral-600` / `text-neutral-200`〜`text-neutral-400`（neutral の Button） | `ToneNeutral` |
| モーダルの背後 | `backdrop:bg-black/70` | 既存 modal |

### 3.1 意味の色の枠・薄い面（危険の行を、ほかの意味の色へ同じ形で）

表の「危険」は枠と面まで red 系に決めている。**警告・成功・情報も同じ形で、枠は `-500/60`、薄い面は `/10`**（表の「など」の読み。新しい色相・シェードは足さない）。注意書きの箱（枠だけ）・トースト・接続の通知・ステータスの帯がこれを使う。

| 意味 | 枠 | 薄い面 |
|------|----|--------|
| 警告 | `border-amber-500/60` | `bg-amber-500/10` |
| 成功 | `border-emerald-500/60` | `bg-emerald-500/10` |
| 情報・進行中 | `border-sky-500/60` | `bg-sky-500/10` |

<!-- color-allowlist:end -->

### 3.2 旧クラスの寄せ先（#867。同じ意味で別のシェード・透明度だったもの）

| 意味 | 旧 | 新（表） |
|------|----|----------|
| 警告の枠 | `border-amber-500/50` / `border-amber-700` | `border-amber-500/60` |
| 警告の文字 | `text-amber-200` | `text-amber-300` |
| 成功の枠 | `border-emerald-500/50` / `border-emerald-700` / `border-emerald-500` | `border-emerald-500/60` |
| 成功の面（North Star） | `bg-emerald-950/30` | `bg-emerald-500/10` |
| トーストの文字（成功） | `text-emerald-200`・`hover:text-emerald-100` | `text-emerald-300`・`hover:text-neutral-100` |
| 危険の枠・面・文字 | `border-red-500/50`・`bg-red-950/40`・`text-red-200`・`hover:text-red-100` | `border-red-500/60`・`bg-red-500/10`・`text-red-300`・`hover:text-neutral-100` |
| 情報の枠 | `border-sky-500`（Take の「AI のおすすめ」） | `border-sky-500/60` |
| 情報の面（クリップの種類） | `bg-sky-900 text-sky-200` | `bg-sky-500/20 text-sky-300` |
| 選択・現在地の枠 | `border-indigo-500/50` | `border-indigo-400` |
| 選択・現在地の面 | `bg-indigo-500/10` / `bg-indigo-500/30` / `bg-indigo-600/30`（吹き出し） | `bg-indigo-500/20` |
| 主ボタン（Lit） | `bg-neutral-200 text-neutral-900` | `bg-indigo-500 text-white hover:bg-indigo-400` |
| 警告・選択の面の文字 | `text-amber-100` / `text-indigo-100` | `text-amber-300` / `text-indigo-300` |
| 一覧の行・ナビのホバー | `hover:bg-neutral-900` | `hover:bg-neutral-800/60` |
| リンク（文字）のホバー | `hover:text-indigo-200`（`text-indigo-300` のリンク）・`hover:text-neutral-200`（`text-neutral-400` の文字） | `hover:text-indigo-200` は外す（リンクは下線が手がかり）／`hover:text-neutral-100`（§4） |
| スピナーの輪・返信の引用線 | `border-neutral-500` | `border-neutral-600` / `border-neutral-700` |
| 動画の読み込みの進捗バーの塗り・ドラッグ中の区間・選んだ区間 | `bg-neutral-300`・`bg-sky-400/50`・`bg-emerald-500/60` | `bg-sky-500/70`・`bg-sky-500/70`・`bg-emerald-500/70`（§4。進行中は sky） |

## 4. 例外（許可リストに無いが、意味が違うので残すクラス）

表に行が無く、寄せると意味か見た目か a11y が壊れるものだけを、理由とともに列挙する。**ここに無い色のクラスは使えない。** 増やすときは理由を書いてこの表とテストの `colorExceptions` を同じ版で変える。

<!-- color-exceptions:begin -->

| クラス | 使う場所 | 理由 |
|--------|---------|------|
| `border-neutral-500` | 入力欄（`atoms.fieldClass`）と、Lit が出す `<input>`・`<select>`・`<textarea>`（Take の指示欄・タイムラインのクリップ） | 入力欄の枠は、塗り・カード・ページの上で **3:1 以上**（WCAG 1.4.11）が必要で、`border-neutral-600` では足りない（`a11y/field_border_test.go`）。表の `border-neutral-700` / `border-neutral-600` は欄の枠に使えない |
| `bg-neutral-800` | 現在地のナビ・ステップ・コードの欄・アイコンの台（`ToolCard`）・チャットの添付・選んだ区間のチップ・Take のレーンと進捗バーの下地 | カード面（`neutral-900`）より一段明るい**据え置きの面**。表の `hover:bg-neutral-800` はホバーの面で、据え置きの面の行が表にない |
| `hover:text-neutral-100` | 文字のリンク・アイコンだけのボタン・戻るリンクのホバー | 基の `text-neutral-400` / `text-neutral-300` を主要な文字の色へ明るくする、表に行が無いホバーの文字。表の色（`text-neutral-100`）への寄せ先が 1 つで、新しいシェードではない |
| `border-indigo-400`・`has-[:checked]:border-indigo-400`・`peer-checked:border-indigo-400`・`bg-indigo-500/20`・`peer-checked:bg-indigo-500/20`・`peer-checked:shadow-indigo-400/40`・`border-t-indigo-400` | 選ばれた項目・現在地の枠と面（Shot カード・場面の一覧・Production Template の選択・生成テストのモデルのカード（枠・面・光る縁取り）・チャットの自分の吹き出し・ロックのトグル・実行の確認の箱・ナビ・ドラッグ中のクリップ）と、スピナーの回る弧 | 表の `indigo` は主ボタン・リンク・フォーカスの輪だけ。**選択**の意味の行が無い。選択は `indigo` 1 種類（枠 `-400`・面 `/20`。カードの光る縁取りだけ影 `shadow-indigo-400/40`）に揃え、枠が 2 つ重ねられるときは色だけで伝えず `aria-current` と文字を添える |
| `bg-black` | 動画・画像のプレビューの背景（`<video>`・`<img>` の余白）と動画プレイヤーの舞台 | 映像のレターボックスは黒でないと映像が浮く。表の `backdrop:bg-black/70` はモーダルの背後だけ |
| `border-transparent` | 現在地でないナビ・ステップの枠（`border-l-2` の幅を取っておく） | 枠の幅を保って現在地の切り替えで文字がずれないようにする（色はない） |
| `accent-red-400`・`accent-amber-400`・`accent-emerald-400` | 予算の `<progress>`（危険・警告・成功） | ブラウザが描くメーターの塗りの色（`accent-*`）で、表の文字用の `-300` / 面用の `/20` は使えない。意味は 3 つの色相（危険 = red・警告 = amber・成功 = emerald）を表と揃える。推定コストと上限の文字を添える（色だけで伝えない） |
| `bg-red-500/70`・`bg-amber-500/70`・`bg-emerald-500/70`・`bg-sky-500/70` | 動画の上・Take のレーンの上に重ねる塗り（Judge の指摘の位置・選んだ区間・ドラッグ中の区間・読み込みの進捗） | 映像・レーンが透けて見える**重ね塗り**で、`/20`（バッジ）では映像の上で見えない。意味の色相は表と同じ（red = 重大・amber = 中・sky = 軽い／ドラッグ中・emerald = 選んだ） |

<!-- color-exceptions:end -->

## 5. #865・#866 の UI もこの表に従う

#865（モデルマネージャーのモデルのカード。`organisms/modelmgr/catalog.templ`・`settingsui.Card`）と #866（「?」ヘルプ。`molecules/help`・ポップオーバーのパネル・`atoms.IconHelp`・`SettingsSection` のヘルプ）で足した UI は、この表に従う。カードの面は `bg-neutral-900 border-neutral-800`、状態は `Badge`、同意の注意は `text-amber-300`、「?」ボタンは `border-neutral-700 text-neutral-300`・ホバー `hover:border-neutral-600 hover:text-neutral-100`・フォーカス輪 `ring-indigo-400`。**これから足す画面・部品も同じ**（表に無い色が要るなら §1 の方針に従って表を変える）。

## 6. 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-28 | 新規作成。用途 → 許可クラスの表（§3。Issue の案をオーナーが 2026-10-28 に確認してそのまま確定）、意味の色の枠・薄い面（§3.1）、旧クラスの寄せ先（§3.2）、例外（§4）、#865・#866 も従うこと（§5）、検査の範囲（§2）を決めた。UI のクラスをこの表に寄せ、`a11y/color_allowlist_test.go` で許可リスト外を落とすようにした | Issue #867 |
| 1.1 | 2026-11-01 | §4 の「選択」の行に、生成テストのモデルのカードが使う `peer-checked:border-indigo-400`・`peer-checked:bg-indigo-500/20`・`peer-checked:shadow-indigo-400/40` を足した（チェックボックスを見えなくして、カード全体を選択の枠と光る縁取りで示す） | Issue #906 |
