# コンポーネント仕様: 生成テスト画面

[overview.md](overview.md) §3 のコンポーネントの階層に、生成テストの画面（Issue #835）を足す。ルートと挙動は [../api/endpoints-gentest.md](../api/endpoints-gentest.md)、仕組みは [../architecture/gentest/gentest.md](../architecture/gentest/gentest.md)。

| 区分 | コンポーネント | 責務 |
|------|---------------|------|
| pages（`gentest_pages.templ`） | `GenerationTestPage`・`GenerationHistoryPage`・`GenerationResultPage`・`GenerationPromptsPage` | 4 つのページ。共通の枠 `generationShell`（戻るリンク・見出し・説明・ページ間のリンク `generationNav`）に organisms を入れる |
| organisms（`organisms/gentest/`） | `ComposeForm`（`modelChoices`・`promptFields`・`parameterFields`・`inputFields`）・`PromptRowFields`・`RefRow`・`Picker` | 作成フォーム（画像／動画・モデルのチェックボックス・複数のプロンプトとネガティブ・本数・縦横比・動画の解像度と長さ・入力画像のアップロードとプロジェクト・履歴からの選択）。無効なモデルは理由つき |
| 〃 | `ConfirmBox`・`ConfirmFailed`・`ConfirmEmpty` | 見積りの確認（件数・モデルごとの内訳・合計・単価なしの注記・警告・上限超過）と、実行のフォーム（見積りを hidden で持つ） |
| 〃 | `BatchBox`・`ResultCard` | 進捗（状態・件数・取り消し）と比較表（行＝モデル、列＝プロンプト、セル＝結果のカード） |
| 〃 | `HistoryList`・`HistoryCard`・`FavoriteButton` | 履歴（絞り込み・合計コスト 1 行・お気に入りの付け外し・次のページ） |
| 〃 | `ResultDetail`・`ImportBox` | 結果の詳細（設定・実際に送ったプロンプト・シード・入力画像・取り込み先）と取り込みフォーム |
| 〃 | `PromptHistory` | プロンプト履歴（再利用のリンク・削除） |
| handler（`web/handler/gentest/`） | `Handlers` | ルート（`Register`）。`gentest.Service` を呼ぶ。`format.go` が価格・能力・状態を表示用の文字列に直し、`request.go` がフォームを要求に直し、`batch.go` が結果を比較表にする。organism はサービスを import しない |

- **進捗は HTMX のポーリング**: 動いているバッチの区画 `#gt-batch` は `hx-get` と `hx-trigger="every 2s"` を持ち、終わると属性が消えて止まる。取り消しは `hx-confirm` つき。
- **見積りが先**: 「見積りを見る」は何も実行しない。「この内容で実行する」だけが `POST /generation-test/run` で、確認の hidden（`estimate_id`・`confirmed_total_usd`・同じ入力）を送る。上限を超える見積りは実行ボタンが無効で、サーバーも拒否する。
- **ファイルの出し方**: 結果・入力のファイルは `Content-Security-Policy: sandbox` と `nosniff` つきで返す。画像は `<img>`、動画は `<video controls>`。
- **入力の長さ**は定数（`MaxAspectLen`・`MaxNameLen`・プロンプトは `gentest.MaxPromptRunes`）で、テンプレートに数字を書かない（`maxlength_test`）。
- **コントラスト**: 補助の文字は `text-neutral-400` 以上（`text-neutral-500` は使わない。`a11y` のテスト）。
- **入口**: `projects_page`（プロジェクト一覧）の分析・設定の隣のリンク「生成テスト」、モデルマネージャーの試し生成の区画のリンク、詳細の「入力画像として使う」。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-26 | 新規作成 | Issue #835。生成テストの画面のコンポーネント |
