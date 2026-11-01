# コンポーネント仕様: 生成テスト画面

[overview.md](overview.md) §3 のコンポーネントの階層に、生成テストの画面（Issue #835）を足す。ルートと挙動は [../api/endpoints-gentest.md](../api/endpoints-gentest.md)、仕組みは [../architecture/gentest/gentest.md](../architecture/gentest/gentest.md)。

| 区分 | コンポーネント | 責務 |
|------|---------------|------|
| pages（`gentest_pages.templ`） | `GenerationTestPage`・`GenerationHistoryPage`・`GenerationResultPage`・`GenerationPromptsPage` | 4 つのページ。共通の枠 `generationShell`（戻るリンク・見出しとその横の「?」ヘルプ（リード文はヘルプの中。Issue #866。[shared-ui-parts.md](shared-ui-parts.md) §9・§10）・ページ間のリンク `generationNav`）に organisms を入れる |
| organisms（`organisms/gentest/`） | `ComposeForm`（`modelChoices`（`models.templ`）・`promptFields`・`parameterFields`・`inputFields`）・`PromptRowFields`・`RefRow`・`Picker` | 作成フォーム（画像／動画・モデルのカード（`models.templ` の `modelChoices`・`modelCard`。タップで選ぶ）・複数のプロンプトとネガティブ・本数・縦横比・動画の解像度と長さ・入力画像のアップロードとプロジェクト・履歴からの選択）。無効なモデルは選べず、理由はヘルプと読み上げにある |
| 〃 | `ConfirmBox`・`ConfirmFailed`・`ConfirmEmpty` | 見積りの確認（件数・モデルごとの内訳・合計・単価なしの注記・警告・上限超過）と、実行のフォーム（見積りを hidden で持つ） |
| 〃 | `BatchBox`・`ResultCard` | 進捗（状態・件数・取り消し）と比較表（行＝モデル、列＝プロンプト、セル＝結果のカード） |
| 〃 | `HistoryList`・`HistoryCard`・`FavoriteButton` | 履歴（絞り込み・合計コスト 1 行・お気に入りの付け外し・次のページ） |
| 〃 | `ResultDetail`・`ImportBox` | 結果の詳細（設定・実際に送ったプロンプト・シード・入力画像・取り込み先）と取り込みフォーム |
| 〃 | `PromptHistory` | プロンプト履歴（再利用のリンク・削除） |
| handler（`web/handler/gentest/`） | `Handlers` | ルート（`Register`）。`gentest.Service` を呼ぶ。`format.go` が価格・能力・状態を表示用の文字列に直し、`request.go` がフォームを要求に直し、`batch.go` が結果を比較表にする。organism はサービスを import しない |

- **モデルはカードをタップして選ぶ**（Issue #906）: 1 モデル 1 枚のカード（`<label>` が見えないチェックボックス `input[type=checkbox][name=model]`（`class="peer sr-only"`）を包む）。カードのタップ・クリックで選ぶ／外す（複数選べる）。見えないだけで本物のチェックボックスなので、フォームが送る値・キーボード（`Tab` で移り `Space` で選ぶ外す）・スクリーンリーダー（「チェックボックス、チェック済み／未チェック」）がそのまま使える。選択は **枠 `peer-checked:border-indigo-400`・面 `peer-checked:bg-indigo-500/20`・光る縁取り `peer-checked:shadow-indigo-400/40`** と右上の ✓ で示す（色だけにしない）。フォーカスの輪は `peer-focus-visible:ring-indigo-400`。選べないモデル（`disabled`）は薄くして、カードには「選べません」のバッジだけを出し、理由は読み上げ用（`aria-describedby` の `sr-only`）とヘルプにある。カードに出す文字は名前・出どころ・単価だけ。色は [color-rules.md](color-rules.md) の §4「選択」の行（カードごとの黄色の行は無い）。
- **ヘルプに移した説明**（Issue #906。[shared-ui-parts.md](shared-ui-parts.md) §9・§10）: 区画の見出し（モデル・プロンプト・入力画像（省略可））の横の「?」に、各区画の説明（旧 legend の括弧書き）を移した。**モデルの「?」**には、複数選べること・代替へ切り替えないこと、モデルごとの性質（シード対応か・ネガティブの欄か書き足しか・参照画像の可否）と、選べない理由（API キー未登録など）が並ぶ。カードごとの毎回の注記と、API キー未登録の黄色い行は出さない。選べないモデルがあるときは、見出しの下に「N 件は今は選べません（理由は「?」に）」の 1 行だけを出す。パラメーター（本数・縦横比・長さ）の横の「?」に本数・縦横比の例・長さの丸め、見積りボタンの横の「?」に 1 回の件数の上限と見積りの上限、実行ボタンの横の「?」に補正も代替もしないこと・履歴に残ること。ネガティブ欄の長いプレースホルダも「?」へ。
- **進捗は HTMX のポーリング**: 動いているバッチの区画 `#gt-batch` は `hx-get` と `hx-trigger="every 2s"` を持ち、終わると属性が消えて止まる。取り消しは `hx-confirm` つき。
- **見積りが先**: 「見積りを見る」は何も実行しない。「この内容で実行する」だけが `POST /generation-test/run` で、確認の hidden（`estimate_id`・`confirmed_total_usd`・同じ入力）を送る。上限を超える見積りは実行ボタンが無効で、サーバーも拒否する。
- **ファイルの出し方**: 結果・入力のファイルは `Content-Security-Policy: sandbox` と `nosniff` つきで返す。画像は `<img>`、動画は `<video controls>`。
- **狭い幅（約 375 px）**: カードは 1 列（`sm:` 以上で 2 列）、文字は `break-words`。`static/src/components/layout/layout.wtest.ts` が画像・動画のフォームを 375 px で開き、横にはみ出さないことを確かめる（フィクスチャは `layout_fixture_test.go`）。カードの選択・キーボード・光る縁取りは `static/src/components/gentest/models.wtest.ts`（実 Chrome）。
- **入力の長さ**は定数（`MaxAspectLen`・`MaxNameLen`・プロンプトは `gentest.MaxPromptRunes`）で、テンプレートに数字を書かない（`maxlength_test`）。
- **コントラスト**: 補助の文字は `text-neutral-400` 以上（`text-neutral-500` は使わない。`a11y` のテスト）。
- **入口**: `projects_page`（プロジェクト一覧）の分析・設定の隣のリンク「生成テスト」、モデルマネージャーの試し生成の区画のリンク、詳細の「入力画像として使う」。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-26 | 新規作成 | Issue #835。生成テストの画面のコンポーネント |
| 1.1 | 2026-10-28 | 4 つのページのリード文（見出しの下の説明）を、見出しの横の「?」ヘルプ（ポップオーバー）に移した | Issue #866。画面の文字を減らす |
| 1.2 | 2026-11-01 | モデルの選択をチェックボックスからタップで選ぶカード（光る縁取り・フォーカスの輪・✓）に変え、カードごとの注記・API キー未登録の黄色い行・長いラベルの括弧書きを「?」ヘルプへ移した（モデル・プロンプト・入力画像・パラメーター・見積り・実行） | Issue #906。画面の文字を減らし、色を #867 に合わせる |
