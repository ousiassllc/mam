# コンポーネント仕様: モデルマネージャー画面

[overview.md](overview.md) §3 のコンポーネントの階層に、モデルマネージャーの画面（Issue #833）を足す。ルートと挙動は [../api/endpoints-model-manager.md](../api/endpoints-model-manager.md)、ComfyUI エンジンとモデルの仕組みは [../architecture/model-manager/model-manager.md](../architecture/model-manager/model-manager.md)。

| 区分 | コンポーネント | 責務 |
|------|---------------|------|
| pages | `model_manager_page`（`ModelManagerPage`） | `GET /model-manager` の全ページ。戻るリンク・見出し・説明（Issue #866 でも動かさない。h1 に「?」は付けない）と `modelmgr.Body` |
| organisms（`organisms/modelmgr/`） | `Body` | 区画を `molecules.SettingsSection` で包んで縦に並べる。各区画の説明は、見出しの横の「?」ヘルプ（ポップオーバー）の中（Issue #866。契約は [shared-ui-parts.md](shared-ui-parts.md) §9）。区画の本体には操作・状態・結果・エラーだけを残す |
| 〃 | `EngineCard`・`GPUBlock`・`DetectBlock` | エンジンのカード（導入の進捗・起動の状態・起動の失敗の理由・自動起動・更新・削除）、GPU の検出結果と導入フォーム、既存の ComfyUI の検出結果 |
| 〃 | `CatalogList`・`ConsentDialog`・`DialogClear` | モデルの一覧（1 つのモデルが 1 枚のカード `modelCard`。状態・大きさ・VRAM・ライセンス・商用可否・ファイル・導入の進捗・成人向けのバッジ・トリガー語・原本の場所）と、同意が要るモデルのライセンスのダイアログ（成人向けは注意書きも） |
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
- **モデルの一覧はカード**（Issue #865。`CatalogList` の中身。行の縦並び（`divide-y`）ではない）:
  - **見た目**: カードの面は共通部品と同じ `rounded-lg border border-neutral-800 bg-neutral-900 p-4`（[shared-ui-parts.md](shared-ui-parts.md) §1・§5。新しい色・影は足さない）。カード同士は `gap-4`。一覧は `<ul role="list" class="grid grid-cols-1 gap-4 lg:grid-cols-2" data-testid="mm-models">`（1024 px 以上は 2 列、それより狭い窓は 1 列。ページの幅は `max-w-4xl`）。`role="list"` は、`list-style: none` で一覧の意味が消える Safari・VoiceOver への対策（既存の設定のカードの一覧と同じ）。
  - **構造**: 1 枚は `<li data-testid="mm-model" data-model="<id>" data-state="<状態>">`。上から、①見出し行（左: 名前の `<h3>`・バッジの行（種類・成人向け・「ライセンスの同意が必要」・状態）・説明、右: 操作のボタン）、②メタの表 `<dl data-testid="mm-model-meta">`（大きさ・VRAM の目安・ライセンス・商用利用の 2 列 × 2 行。トリガー語・原本の場所は全幅）、③進捗（導入中。`role="status"`）か必要な空き容量（導入済みでないとき。足りないときは理由の文）、④ファイルの `<details>`（ファイルの一覧と導入されるモデル）。行と同じ情報を持ち、削らない。`data-testid`（`mm-model`・`mm-model-state`・`mm-adult-badge`・`mm-model-meta`・`mm-vram`・`mm-commercial`・`mm-trigger-words`・`mm-sources`・`mm-need`・`mm-progress`・`mm-files`・`mm-install`・`mm-consent-open`）と、導入・同意・中断・削除の `hx-…`、`#mm-catalog` の差し替え（ポーリング・OOB）は行のときのまま。
  - **操作は右上で `shrink-0`**: ボタンの箱（`flex shrink-0 flex-wrap`）は見出し行の右に置き、説明が長くても縮めず、押し出されない。カードの幅が約 352 px（コンテナクエリ `@min-[22rem]`。1 列の 375 px 画面のカードなど）より狭いときだけ、ボタンは名前・バッジ・説明の下に回って折り返し、押しやすい大きさのまま出る。名前・説明・メタの値は `break-words` で折り返し、切り詰めない（`min-w-0`）。
  - **a11y**: 名前は `<h3>`（区画の `<h2>` の下）で、カードの見出しになる。状態は文字のバッジ（導入済み・導入中・ファイルが不足・未導入。色だけで伝えない）。ボタンは `SmallButtonFor` で、`aria-label` が「<操作>: <モデル名>」（同じ文言のボタンが並ぶため）。カード自体は操作の対象ではなく（クリックで開かない）、タブの順はカードの中のボタン・リンク・`<summary>` のとおり。同意のダイアログが閉じたときのフォーカスの戻り先は、描き直されたカード（`data-return-focus="[data-model=…]"`）の最初の操作。
  - **既存部品との関係**: 面と見出し・状態のバッジの考え方は設定の `settingsui.CardItem` と同じだが、モデルのカードは複数のバッジ・メタの表・進捗・`<details>` を持ち、操作が右上にあるため、`organisms/modelmgr` の中の `modelCard` として持つ（`CardItem` は操作が下の行で、`ToolCard` はリンクのカード）。
  - **HF トークンが必要なモデル**（Issue #877。カタログの `requires_hf_token`。LTX-2.5）: バッジの行に既存の警告のバッジで「HF トークンが必要」（`data-testid="mm-token-badge"`）を足す。トークンが未設定（かつ未導入・導入中でない）の間は、**導入ボタンの横**（操作の箱の中。`modelActions` の既定の分岐で、`installButton` の次）に、既存の注意の文字（`text-xs text-amber-300`。幅は既存の `max-w-xs`）で案内（`data-testid="mm-token-guide"`。導入を拒むときの文言 A と同じ。`hfguide.NoToken`、model-manager.md §5.3）とトークンの区画（`#mm-hf`）へのリンクを出す。案内のあるカードだけ、操作の箱を見出し行の右ではなく名前・バッジ・説明の下（カードの幅いっぱい）に置き（`flex flex-wrap items-center gap-2`）、ボタンの隣に案内を並べる。カードが狭いときは案内がボタンの下に折り返す（`flex-wrap`）。案内のないカードの操作は、これまでどおり右上の `shrink-0` の箱。新しい色・部品・スタイルは足さない。トークンの区画の未設定の文は「ログインや同意が要るモデル（例: LTX-2.5）の取得に使います。それ以外のカタログのモデルは、トークンなしで取得できます」。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成 | Issue #833。モデルマネージャーの画面のコンポーネント |
| 1.1 | 2026-10-26 | 試し生成の区画が生成テストの結果を出すことに合わせ、プロンプトの上限の出どころを `gentest.MaxPromptRunes` に改めた | Issue #835 |
| 1.2 | 2026-10-25 | モデルの一覧に「成人向け」のバッジ・トリガー語・原本の場所（ミラー）を足し、同意ダイアログに成人向けの注意書きと原本の場所を足した。設定「成人向けモデルを表示する」（`AdultCard`）と年齢確認のダイアログ（`AdultDialog`）・`AdultWithCatalog` を足した | Issue #841。成人向けモデルと年齢確認 |
| 1.3 | 2026-10-25 | 区画「Hugging Face から導入」の organism（`HFImportSection`・`HFResultView`・`HFModelListView` ほか）と、ハンドラのパッケージ `web/handler/modelmgr/hfimport`（`jobview` を共有。画面テストの部品は `web/screentest`）を足した。`hf-token` のハンドラは `hfimport` に移した | Issue #842 |
| 1.4 | 2026-10-25 | 年齢確認・同意のダイアログに、開いたらフォーカスが入る・Esc で閉じる・閉じたら開いたボタンへフォーカスが戻る、を足した。モデルの行は説明が長くても操作ボタンを右上に置く（`shrink-0`） | 最終の実画面の確認（Issue #841 の指摘） |
| 1.5 | 2026-10-28 | モデルの一覧を、行の縦並びからカードのグリッドにした（見た目・構造・操作の位置・a11y を「モデルの一覧はカード」に書いた）。`modelRow` は `modelCard` に、`modelRowSelector` は `modelCardSelector` になった | Issue #865 |
| 1.6 | 2026-10-28 | 各区画（ComfyUI エンジン・モデル・試し生成・Hugging Face から導入・トークン・外部の ComfyUI）の説明を、見出しの横の「?」ヘルプ（ポップオーバー）に移した（h1 の下の説明は動かさない） | Issue #866。画面の文字を減らす |
| 1.7 | 2026-10-29 | モデルのカードに、`requires_hf_token` のモデルの「HF トークンが必要」のバッジと、トークン未設定の間の案内（トークンの区画へのリンクつき）を足した。トークンの区画の未設定の文を、ログインや同意が要るモデル（LTX-2.5）で使う、という文言に改めた | Issue #877。既存の部品・クラスだけで、見た目は変えない |
