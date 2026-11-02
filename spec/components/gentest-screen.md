# コンポーネント仕様: 生成テスト画面

[overview.md](overview.md) §3 のコンポーネントの階層に、生成テストの画面（Issue #835）を足す。ルートと挙動は [../api/endpoints-gentest.md](../api/endpoints-gentest.md)、仕組みは [../architecture/gentest/gentest.md](../architecture/gentest/gentest.md)。

| 区分 | コンポーネント | 責務 |
|------|---------------|------|
| pages（`gentest_pages.templ`） | `GenerationTestPage`・`GenerationHistoryPage`・`GenerationResultPage`・`GenerationPromptsPage` | 4 つのページ。共通の枠 `generationShell`（戻るリンク・見出しとその横の「?」ヘルプ（リード文はヘルプの中。Issue #866。[shared-ui-parts.md](shared-ui-parts.md) §9・§10）・ページ間のリンク `generationNav`）に organisms を入れる |
| organisms（`organisms/gentest/`） | `ComposeForm`（`modelChoices`（`models.templ`）・`promptFields`・`parameterFields`・`inputFields`（`inputs.templ`）・`refOptions`・`pickerDialog`（`refopts.templ`））・`PromptRowFields`・`RefRow`（`refrow.templ`）・`Picker` | 作成フォーム（画像／動画・モデルのカード（`models.templ` の `modelChoices`・`modelCard`。タップで選ぶ。入力画像のアイコンつき）・複数のプロンプトとネガティブ・本数・縦横比・動画の解像度と長さ・入力画像（ドロップゾーン 1 つ・サムネイルの格子・ポーズを真似させない設定・LoRA））。無効なモデルは選べず、理由はヘルプと読み上げにある |
| 〃 | `ConfirmBox`・`ConfirmFailed`・`ConfirmEmpty` | 見積りの確認（件数・モデルごとの内訳・合計・単価なしの注記・警告・上限超過）と、実行のフォーム（見積りを hidden で持つ） |
| 〃 | `BatchBox`・`ResultCard` | 進捗（状態・件数・取り消し）と比較表（行＝モデル、列＝プロンプト、セル＝結果のカード） |
| 〃 | `HistoryList`・`HistoryCard`・`FavoriteButton` | 履歴（絞り込み・合計コスト 1 行・お気に入りの付け外し・次のページ） |
| 〃 | `ResultDetail`・`ImportBox` | 結果の詳細（設定・実際に送ったプロンプト・シード・入力画像・取り込み先）と取り込みフォーム |
| 〃 | `PromptHistory` | プロンプト履歴（再利用のリンク・削除） |
| handler（`web/handler/gentest/`） | `Handlers` | ルート（`Register`）。`gentest.Service` を呼ぶ。`format.go` が価格・能力・状態を表示用の文字列に直し、`request.go` がフォームを要求に直し、`batch.go` が結果を比較表にする。organism はサービスを import しない |
| 〃（`web/handler/gentest/refform/`） | `Refs`・`Options`・`Hidden`・`Stage` | 入力画像のフォーム（`ref`・`role_<ref>`・`strength_<ref>`・`avoid_pose`・`lora`・`lora_strength`・ファイル `upload`）を `gentest.RefInput` / `LoRAInput` に読み、確認の hidden に書き戻し、ブラウザが選んだファイルを保存して `upload::#<n>` の位置に入れる（ディレクトリの行数上限のため別パッケージ） |
| static（`static/src/components/gentest/`） | `inputs.ts`・`inputs-model.ts`・`inputs-models.ts` | 入力画像の動き（ブラウザで選んだ画像のサムネイル・並べ替え・拡大・モデルの選択に合わせた役割と強さの表示）。DOM を使わない規則は `inputs-model.ts`（vitest）、DOM は `inputs.wtest.ts`（実 Chrome） |

- **モデルはカードをタップして選ぶ**（Issue #906）: 1 モデル 1 枚のカード（`<label>` が見えないチェックボックス `input[type=checkbox][name=model]`（`class="peer sr-only"`）を包む）。カードのタップ・クリックで選ぶ／外す（複数選べる）。見えないだけで本物のチェックボックスなので、フォームが送る値・キーボード（`Tab` で移り `Space` で選ぶ外す）・スクリーンリーダー（「チェックボックス、チェック済み／未チェック」）がそのまま使える。選択は **枠 `peer-checked:border-indigo-400`・面 `peer-checked:bg-indigo-500/20`・光る縁取り `peer-checked:shadow-indigo-400/40`** と右上の ✓ で示す（色だけにしない）。フォーカスの輪は `peer-focus-visible:ring-indigo-400`。選べないモデル（`disabled`）は薄くして、カードには「選べません」のバッジだけを出し、理由は読み上げ用（`aria-describedby` の `sr-only`）とヘルプにある。カードに出す文字は名前・出どころ・単価だけ。色は [color-rules.md](color-rules.md) の §4「選択」の行（カードごとの黄色の行は無い）。
- **ヘルプに移した説明**（Issue #906。[shared-ui-parts.md](shared-ui-parts.md) §9・§10）: 区画の見出し（モデル・プロンプト・入力画像（省略可））の横の「?」に、各区画の説明（旧 legend の括弧書き）を移した。**モデルの「?」**には、複数選べること・代替へ切り替えないこと、モデルごとの性質（シード対応か・ネガティブの欄か書き足しか・参照画像の可否）と、選べない理由（API キー未登録など）が並ぶ。カードごとの毎回の注記と、API キー未登録の黄色い行は出さない。選べないモデルがあるときは、見出しの下に「N 件は今は選べません（理由は「?」に）」の 1 行だけを出す。パラメーター（本数・縦横比・長さ）の横の「?」に本数・縦横比の例・長さの丸め、見積りボタンの横の「?」に 1 回の件数の上限と見積りの上限、実行ボタンの横の「?」に補正も代替もしないこと・履歴に残ること。ネガティブ欄の長いプレースホルダも「?」へ。
- **英訳のスイッチ**（Issue #913。[../architecture/gentest/gentest.md](../architecture/gentest/gentest.md) §3.1）: プロンプトの区画に、チェックボックス `input[type=checkbox][name=translate][value=1]` と短いラベル「英語に訳して送る」を置く（`label[data-testid=gt-translate]`）。**新しい画面は ON**（`gentest.DefaultTranslate`。MCP の既定と同じ）。チェックボックスは外すと送られないので、`translate=1` が無いフォームは OFF として読み、フォームの描き直し（画像／動画の切り替え）は `hx-include` でスイッチの状態を保つ。確認の hidden は ON のとき `translate=1` を持ち、「この内容で実行する」は見積りと同じ設定で走る。説明（LLM の割り当ての Prompt Planner を使う・英語の文は訳さない・訳せなければ日本語のまま送らずエラー・見積りでは単価なしの行になる・OFF なら入力のまま）はスイッチの横の「?」（`help.Toggle(help.ID("英訳"), "英訳")`）の中にだけ置き、画面には出さない。見積りの確認には内訳の 1 行「プロンプトの英訳（プロバイダー / モデル）」が単価なし（「合計に入りません」）で入り、警告は既存の `text-amber-300` の一覧に並ぶ。訳した英語は、比較表の列（入力した文の下に「英訳: …」。`text-neutral-200`）と結果の詳細（「英訳（送った英語）」の枠 `border-neutral-700`。「入力したプロンプト」は設定の一覧に残る）に出す。訳せなかった実行は、進捗の区画に 502 でメッセージ（`text-red-300`）を出し、何も始めない。色は新しいクラスを足さず、既存の許可リストだけを使う（[color-rules.md](color-rules.md)）。375 px で横にはみ出さないことは `layout.wtest.ts`、スイッチは `static/src/components/gentest/translate.wtest.ts`（実 Chrome）。
- **進捗は HTMX のポーリング**: 動いているバッチの区画 `#gt-batch` は `hx-get` と `hx-trigger="every 2s"` を持ち、終わると属性が消えて止まる。取り消しは `hx-confirm` つき。
- **見積りが先**: 「見積りを見る」は何も実行しない。「この内容で実行する」だけが `POST /generation-test/run` で、確認の hidden（`estimate_id`・`confirmed_total_usd`・同じ入力）を送る。上限を超える見積りは実行ボタンが無効で、サーバーも拒否する。
- **ファイルの出し方**: 結果・入力のファイルは `Content-Security-Policy: sandbox` と `nosniff` つきで返す。画像は `<img>`、動画は `<video controls>`。
- **狭い幅（約 375 px）**: カードは 1 列（`sm:` 以上で 2 列）、文字は `break-words`。`static/src/components/layout/layout.wtest.ts` が画像・動画のフォームを 375 px で開き、横にはみ出さないことを確かめる（フィクスチャは `layout_fixture_test.go`）。カードの選択・キーボード・光る縁取りは `static/src/components/gentest/models.wtest.ts`（実 Chrome）。
- **入力の長さ**は定数（`MaxAspectLen`・`MaxNameLen`・プロンプトは `gentest.MaxPromptRunes`）で、テンプレートに数字を書かない（`maxlength_test`）。
- **コントラスト**: 補助の文字は `text-neutral-400` 以上（`text-neutral-500` は使わない。`a11y` のテスト）。
- **入口**: `projects_page`（プロジェクト一覧）の分析・設定の隣のリンク「生成テスト」、モデルマネージャーの試し生成の区画のリンク、詳細の「入力画像として使う」。
- **入力画像（Issue #933）**: 区画は上から、見出し（「?」にルール）・**ドロップゾーン 1 つ**（画像をドロップ、またはクリックでファイルを選ぶ（複数。PNG・JPEG・WebP）。ゾーンの中に「プロジェクトの画像から選ぶ」「履歴の画像から選ぶ」のボタンがあり、`<dialog data-gt-picker-dialog>` の中に既存の `GET /generation-test/picker` を HTMX で入れる。ルール（形式・最大枚数・使えないモデル）は常時の文では出さず、ゾーンの `title` と「?」に置き、破ったときだけ 1 行の小さなエラー `[data-gt-error]` で言う）・使えないモデルを選んでいて入力画像があるときの 1 行 `[data-gt-noref]`（クライアントが `data-accepts-references` から即座に出す。見積りの警告も残す）・カウンター「3 / 10」・**サムネイルの格子**（`ul#gt-refs`）・参照画像のオプション。
  - サムネイル（`RefRow`）: 番号バッジ（1, 2, 3…。モデルに渡す順で、Qwen の `<image1>` `<image2>` と同じ）・× で外す・ドラッグで並べ替え（HTML5 DnD。キーボードの代わりに ‹ › ボタン）・クリックで拡大（`<dialog data-gt-lightbox>`）。ブラウザで選んだファイルは見積りまでサーバーに無いので、サムネイルは `URL.createObjectURL` で出し、ファイルは `<input name=upload>` に並び順どおり入れ直す。そのサムネイルの `ref` は `upload::#<n>`（`upload` の n 番目のファイル）で、`role_upload::#<n>`・`strength_upload::#<n>` と一緒に送り、サーバー（`refform.Stage`）が保存して id に置き換える。
  - 役割と強さ（画像のみ。動画は従来どおり 参照・最初のフレーム・最後のフレームで強さなし）: `select[name=role_<ref>]`（全体 `reference`／顔だけ `face`／画風だけ `style`／編集対象 `edit`）と `input[type=range][name=strength_<ref>]`（0〜1・刻み 0.05・既定 0.7・値を横に表示）。**選んでいるモデルが 1 つでも入力画像を使えるときだけ**出し、役割の選択肢は選んだモデルの `ReferenceRoles` の和集合（画像から型だけなら「全体」だけ）。強さはモデルによらず同じ「強さ」で、モデルがどう実現するかは見せない。出さない部品は `disabled` にして何も送らない。
  - オプション（画像のみ）: チェックボックス `avoid_pose`「参照画像のポーズ・構図を真似させない」（入力画像を使えるモデルを選んでいるとき）と、LoRA の選択 `lora`（`GET` のエンジンの一覧 `Service.LoRAs`。先頭は「なし」）＋強さ `lora_strength`（既定 0.4）（LoRA に対応するモデルを選んでいて、一覧が空でないときだけ）。確認の hidden は `strength_<ref>`・`avoid_pose`・`lora`・`lora_strength` も持つので、見積り ID はこれらが変わると変わる。
  - モデルのカード: 入力画像のアイコン（`role=img` と `title`・`aria-label`。編集 ＝ `image-plus`＋「編集」、画像から ＝ `image-up`＋「画像から」、使えない ＝ `image-off`、中身を言わない対応 ＝ `image`）を、既存のバッジ行の先頭に出す。`<li>` は `data-accepts-references`・`data-reference-mode`・`data-reference-roles`・`data-supports-lora`・`data-label` を持ち、`inputs-models.ts` はそれを読んで役割・強さ・オプション・注意を出し入れする。
  - 動き（Issue #953 のレビュー）: 0 バイトの画像は選べず（「空の画像ファイルは除きました」の 1 行）、サーバーも空のファイルを保存しないが `upload::#<n>` の番号はファイルの位置のままで、後ろのサムネイルの画像はずれない（空のファイルの欄は捨て、同じ番号を 2 つの欄が指しても先の 1 つだけ）。画像はゾーンだけでなくフォームのどこにドロップしてもブラウザが開かず、サムネイルの格子にドロップしたものは追加し、それ以外では何もしない（プロンプトを失わない）。メディアを切り替える（フォームを差し替える）と、ブラウザで選んだファイルは消える（URL も解放する）。「画像から」のモデルは 1 枚目の画像だけを使う。強さ 0 は 0 のまま再表示する。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-26 | 新規作成 | Issue #835。生成テストの画面のコンポーネント |
| 1.1 | 2026-10-28 | 4 つのページのリード文（見出しの下の説明）を、見出しの横の「?」ヘルプ（ポップオーバー）に移した | Issue #866。画面の文字を減らす |
| 1.2 | 2026-11-01 | モデルの選択をチェックボックスからタップで選ぶカード（光る縁取り・フォーカスの輪・✓）に変え、カードごとの注記・API キー未登録の黄色い行・長いラベルの括弧書きを「?」ヘルプへ移した（モデル・プロンプト・入力画像・パラメーター・見積り・実行） | Issue #906。画面の文字を減らし、色を #867 に合わせる |
| 1.3 | 2026-11-01 | プロンプトの区画に英訳のスイッチ「英語に訳して送る」（既定 ON・「?」の説明つき）を足し、見積りの確認に単価なしの「英訳」の行、比較表の列と結果の詳細に訳した英語を出す | Issue #913。生成テストを英語のプロンプトで試せるようにする |
| 1.4 | 2026-11-02 | 入力画像の区画を作り直した: ドロップゾーン 1 つ（ファイル・プロジェクト・履歴のモーダル）・サムネイルの格子（番号・取り除く・並べ替え・拡大）・カウンター・役割と強さ（モデルに合わせて出す）・ポーズを真似させない設定・LoRA、モデルのカードに入力画像のアイコンと即時の注意。ハンドラは `refform` に分けた | Issue #933 |
| 1.5 | 2026-11-02 | 入力画像のレビュー指摘: 0 バイトの画像の除外・欠けた番号を詰めない `Stage`・フォーム全体でのドロップ・メディアを切り替えるとブラウザで選んだファイルが消えること・画像から型のモデルは 1 枚目だけを使うこと・強さ 0 の再表示を書いた | Issue #933（PR #953 のレビュー） |
