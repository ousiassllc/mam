# コンポーネント仕様: モデルマネージャー画面

[overview.md](overview.md) §3 のコンポーネントの階層に、モデルマネージャーの画面（Issue #833）を足す。ルートと挙動は [../api/endpoints-model-manager.md](../api/endpoints-model-manager.md)、ComfyUI エンジンとモデルの仕組みは [../architecture/model-manager/model-manager.md](../architecture/model-manager/model-manager.md)。

| 区分 | コンポーネント | 責務 |
|------|---------------|------|
| pages | `model_manager_page`（`ModelManagerPage`） | `GET /model-manager` の全ページ。戻るリンク・見出し・説明と `modelmgr.Body` |
| organisms（`organisms/modelmgr/`） | `Body` | 5 つの区画を `molecules.SettingsSection` で包んで縦に並べる |
| 〃 | `EngineCard`・`GPUBlock`・`DetectBlock` | エンジンのカード（導入の進捗・起動の状態・起動の失敗の理由・自動起動・更新・削除）、GPU の検出結果と導入フォーム、既存の ComfyUI の検出結果 |
| 〃 | `CatalogList`・`ConsentDialog`・`DialogClear` | モデルの一覧（状態・大きさ・VRAM・ライセンス・商用可否・ファイル・導入の進捗）と、同意が要るモデルのライセンスのダイアログ |
| 〃 | `TrialSection`・`TrialFormView`・`TrialList` | 試し生成のフォームと結果（動いているものは進捗、完了は画像・動画、失敗は理由） |
| 〃 | `HFCard`・`ExternalCard` | Hugging Face のトークンの状態（値は出さない）、外部の ComfyUI の確認 |
| 〃 | `EngineWithCatalog`・`CatalogWithTrial`・`CatalogWithDialogClear` | 1 つの操作が複数の区画に及ぶときの応答（片方は `hx-swap-oob`） |
| handler（`web/handler/modelmgr/`） | `Handlers` | ルート（`Register`）。`modelmanager.Service` を呼び、`mmview.Refuse` で拒否を状態とメッセージに写す。`view.go` がサービスの型を画面のデータ（`organisms/modelmgr` の `Data`）に写す。organism はサービスを import しない |

- **進捗は HTMX のポーリング**: 動いているジョブのある区画（エンジンのカード・モデルの一覧・試し生成の一覧）は `hx-get` と `hx-trigger="every 2s"` を持ち、ジョブがなくなると属性が消えて止まる。取り直しの URL はジョブの ID を持ち（`?job=`・`?jobs=`）、ジョブが終わった取り直しで、結果（導入した／失敗の理由）を 1 度出す。WebSocket は使わない。
- **操作の応答は、変えた区画だけ**（`hx-target="#mm-…"`・`outerHTML`）。導入の完了・エンジンの削除・モデルの削除は、ほかの区画（モデルの一覧・試し生成のモデルの選択肢）を `hx-swap-oob` で描き直す。
- **破壊的な操作**（エンジンの削除・モデルの削除・導入の中断・試し生成の削除・トークンの削除）は `hx-confirm` を持つ。送信ボタンは `hx-disabled-elt` と `hx-sync="this:drop"` で二重送信を防ぐ（`double_submit_test`・`destructive_confirm_test` が検査する）。
- **入力の長さ**は定数（`MaxTokenLen`・`MaxAspectLen`、プロンプトは `gentest.MaxPromptRunes`）で、テンプレートに数字を書かない（`maxlength_test`）。
- **秘密を出さない**: Hugging Face のトークンは、状態（設定済みか・末尾 4 文字）だけを描く。
- **入口**: `projects_page`（プロジェクト一覧）のリンク「モデルマネージャー」と、`global_settings_page` の見出しの下のリンク。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成 | Issue #833。モデルマネージャーの画面のコンポーネント |
| 1.1 | 2026-10-26 | 試し生成の区画が生成テストの結果を出すことに合わせ、プロンプトの上限の出どころを `gentest.MaxPromptRunes` に改めた | Issue #835 |
