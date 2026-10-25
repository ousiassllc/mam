# コンポーネント仕様: モデルマネージャー画面

[overview.md](overview.md) §3 のコンポーネントの階層に、モデルマネージャーの画面（Issue #833）を足す。ルートと挙動は [../api/endpoints-model-manager.md](../api/endpoints-model-manager.md)、ComfyUI エンジンとモデルの仕組みは [../architecture/model-manager/model-manager.md](../architecture/model-manager/model-manager.md)。

| 区分 | コンポーネント | 責務 |
|------|---------------|------|
| pages | `model_manager_page`（`ModelManagerPage`） | `GET /model-manager` の全ページ。戻るリンク・見出し・説明と `modelmgr.Body` |
| organisms（`organisms/modelmgr/`） | `Body` | 5 つの区画を `molecules.SettingsSection` で包んで縦に並べる |
| 〃 | `EngineCard`・`GPUBlock`・`DetectBlock` | エンジンのカード（導入の進捗・起動の状態・起動の失敗の理由・自動起動・更新・削除）、GPU の検出結果と導入フォーム、既存の ComfyUI の検出結果 |
| 〃 | `CatalogList`・`ConsentDialog`・`DialogClear` | モデルの一覧（状態・大きさ・VRAM・ライセンス・商用可否・ファイル・導入の進捗・成人向けのバッジ・トリガー語・原本の場所）と、同意が要るモデルのライセンスのダイアログ（成人向けは注意書きも） |
| 〃 | `AdultCard`・`AdultDialog` | 設定「成人向けモデルを表示する」（既定オフ）と、初めてオンにするときの年齢確認・注意書きのダイアログ |
| 〃 | `TrialSection`・`TrialFormView`・`TrialList` | 試し生成のフォームと結果（動いているものは進捗、完了は画像・動画、失敗は理由） |
| 〃 | `HFCard`・`ExternalCard` | Hugging Face のトークンの状態（値は出さない）、外部の ComfyUI の確認 |
| 〃 | `EngineWithCatalog`・`CatalogWithTrial`・`CatalogWithDialogClear`・`AdultWithCatalog` | 1 つの操作が複数の区画に及ぶときの応答（片方は `hx-swap-oob`） |
| 〃 | `HFImportSection`・`HFResultView`・`HFModelListView`・`HFModelsWithMsgClear`・`HFModelsWithTrial`・`HFMsg` | 区画「Hugging Face から導入」（Issue #842）: 検索・URL のフォーム、ヒット・リポジトリのファイルの一覧（導入できないファイルは理由を出してフォームを出さない・ファイルごとの導入フォーム）、追加したモデルと進行中の導入（進捗・中断・削除）、導入の拒否のエラーの行 |
| handler（`web/handler/modelmgr/hfimport/`） | `Handlers` | 上の区画のルート（`Register`。`hf-token` の保存・削除も）。`ListData`・`RepoOf`・`HitsOf` が `modelmanager` の型を画面のデータに写す。親のパッケージが行数の上限に近いため別パッケージで、親の `trialPanel` を引数で受けて、導入の完了・削除で試し生成の区画を描き直す。`jobview`（バイト数の表記・進捗・終了の文言）は親と子で共有する。画面テストの共通の部品は `web/screentest`（handler の外。テスト用の DB を開くため） |
| handler（`web/handler/modelmgr/`） | `Handlers` | ルート（`Register`）。`modelmanager.Service` を呼び、`mmview.Refuse` で拒否を状態とメッセージに写す。`view.go` がサービスの型を画面のデータ（`organisms/modelmgr` の `Data`）に写す。organism はサービスを import しない |

- **進捗は HTMX のポーリング**: 動いているジョブのある区画（エンジンのカード・モデルの一覧・試し生成の一覧）は `hx-get` と `hx-trigger="every 2s"` を持ち、ジョブがなくなると属性が消えて止まる。取り直しの URL はジョブの ID を持ち（`?job=`・`?jobs=`）、ジョブが終わった取り直しで、結果（導入した／失敗の理由）を 1 度出す。WebSocket は使わない。
- **操作の応答は、変えた区画だけ**（`hx-target="#mm-…"`・`outerHTML`）。導入の完了・エンジンの削除・モデルの削除は、ほかの区画（モデルの一覧・試し生成のモデルの選択肢）を `hx-swap-oob` で描き直す。
- **破壊的な操作**（エンジンの削除・モデルの削除・導入の中断・試し生成の削除・トークンの削除）は `hx-confirm` を持つ。送信ボタンは `hx-disabled-elt` と `hx-sync="this:drop"` で二重送信を防ぐ（`double_submit_test`・`destructive_confirm_test` が検査する）。
- **入力の長さ**は定数（`MaxTokenLen`・`MaxAspectLen`、プロンプトは `gentest.MaxPromptRunes`）で、テンプレートに数字を書かない（`maxlength_test`）。
- **秘密を出さない**: Hugging Face のトークンは、状態（設定済みか・末尾 4 文字）だけを描く。
- **入口**: `projects_page`（プロジェクト一覧）のリンク「モデルマネージャー」と、`global_settings_page` の見出しの下のリンク。
- **成人向けモデル**（Issue #841）: モデルの区画の先頭に設定「成人向けモデルを表示する」（`AdultCard`、`#mm-adult`。既定はオフ）。初めてオンにするとき `AdultDialog`（`#mm-dialog`）が、注意書き（`Service.AdultContent().Notice`）と「18 歳以上（またはお住まいの地域で成年）であり、注意書きに同意します」のチェックを出し、`PUT /model-manager/adult` で送る（`notice_version` と `agree=1`）。ダイアログは `<section role="dialog" data-inline-dialog>`（ページの中の区画で、`<dialog>` のモーダルではない）。**開いたら、フォーカスは理由（422 のとき）かダイアログ自身に移り、Esc で閉じ（「閉じる」ボタン `data-inline-dialog-close` を押すのと同じ）、閉じたらフォーカスを開いたボタンに戻す**（そのボタンが応答で置き換わったときは、`data-return-focus` の区画 `#mm-adult` の最初の操作）。実装は `static/src/components/inline-dialog/`（同意ダイアログ `#mm-consent` も同じ）。画面は確認を渡す手段で、**ゲートは Service が閉じる**（確認が無い間は、成人向けのモデルが一覧・導入・試し生成の選択肢に出ず、直接叩いても 409。endpoints-model-manager.md）。確認の後は、同じカードの切り替え（オン／オフ）だけで、聞き直さない。切り替えの応答はカードと、一覧・試し生成の区画（`hx-swap-oob`）・ダイアログの閉じ（`AdultWithCatalog`）。成人向けのモデルのカードには、バッジ「成人向け」（`mm-adult-badge`）・ライセンス・商用可否・**原本の場所**（ミラーのファイルのとき。`mm-sources`）・**トリガー語**（LoRA のとき。`mm-trigger-words`）を出す。同意ダイアログは、ライセンスの要約の後に、成人向けの注意書き（`mm-consent-adult`）と原本の場所（`mm-consent-sources`）を出す。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成 | Issue #833。モデルマネージャーの画面のコンポーネント |
| 1.1 | 2026-10-26 | 試し生成の区画が生成テストの結果を出すことに合わせ、プロンプトの上限の出どころを `gentest.MaxPromptRunes` に改めた | Issue #835 |
| 1.2 | 2026-10-25 | モデルの一覧に「成人向け」のバッジ・トリガー語・原本の場所（ミラー）を足し、同意ダイアログに成人向けの注意書きと原本の場所を足した。設定「成人向けモデルを表示する」（`AdultCard`）と年齢確認のダイアログ（`AdultDialog`）・`AdultWithCatalog` を足した | Issue #841。成人向けモデルと年齢確認 |
| 1.3 | 2026-10-25 | 区画「Hugging Face から導入」の organism（`HFImportSection`・`HFResultView`・`HFModelListView` ほか）と、ハンドラのパッケージ `web/handler/modelmgr/hfimport`（`jobview` を共有。画面テストの部品は `web/screentest`）を足した。`hf-token` のハンドラは `hfimport` に移した | Issue #842 |
| 1.4 | 2026-10-25 | 年齢確認・同意のダイアログに、開いたらフォーカスが入る・Esc で閉じる・閉じたら開いたボタンへフォーカスが戻る、を足した。モデルの行は説明が長くても操作ボタンを右上に置く（`shrink-0`） | 最終の実画面の確認（Issue #841 の指摘） |
