# エンドポイント: 生成テスト画面

プロジェクトに属さない画像・動画の比較生成の画面 `GET /generation-test`（Issue #835）。意味・見積り・ジョブ・失敗の扱いは [../architecture/gentest/gentest.md](../architecture/gentest/gentest.md)、テーブルは [../architecture/gentest/er-gentest.md](../architecture/gentest/er-gentest.md)。画面の全操作は、同じサービス（`gentest.Service`）を呼ぶ MCP ツールでもできる（[mcp-tools-generation-test.md](mcp-tools-generation-test.md)）。入口はプロジェクト一覧（分析・設定の隣）のリンクと、モデルマネージャーの区画「モデル」の「?」ヘルプの中のリンク（#1100 で、試し生成の区画のリンクから移った）。

POST・DELETE は CSRF 必須。操作は区画（`outerHTML`）を返し、ジョブの進捗は 2 秒ごとに取り直す（`hx-trigger="every 2s"`。終わると止まる。WebSocket は使わない）。拒否はページのトーストではなく区画の中のメッセージで返す（下の「エラーの対応」）。

## ルート

| メソッド | パス | 内容 |
|---|---|---|
| GET | `/generation-test` | 画面（作成フォーム・確認・進捗）。クエリ `model`（そのモデルを選んで開く。動画モデルなら動画のフォーム）・`prompt`（プロンプト履歴の ID でプロンプトを入れる）・`ref`（`<source>:<project>:<id>` の入力画像を足す）・`from_result`（プロンプトマッチの回など、結果の ID。その結果のプロンプト・モデル・縦横比を入れて開き、「モデルを足して実行すると同じプロンプトで比べられます」の案内を出す。Issue #965）・`tier`（`easy`／`detail`／`comfy`。画像のフォームの段。無い・知らない値はかんたん。モデルを名指す入口（`model`・`from_result`・`again`）は `tier` が無ければこだわり。`comfy` はノード画面などへの入口だけの画面。Issue #977）・`again`（結果の ID。その結果の設定を入れてこだわりで開く。`same_seed=1` でシードも入れる。入れられない項目は理由を 1 行で出す）・`ref_role`（`ref` の入力画像の役割。「この顔で作る」は `face`）・`intent`・`quality`（かんたんで選んでおく値） |
| POST | `/generation-test/recipes` | 今のフォームの設定をレシピとして保存（フォームの全項目＋`recipe_name`・`recipe_easy=1`。プロンプトの本文と入力画像は保存しない）。レシピの区画 `#gt-recipes` を返す（成功 200、名前の重複 409、検査の失敗 422 は理由つきで同じ区画） |
| POST | `/generation-test/recipes/:id/rename` | 名前を変える（`recipe_rename`）。区画 `#gt-recipes` を返す |
| POST | `/generation-test/recipes/:id/easy` | かんたんの選択肢に足す（`easy=1`）／外す（`easy=0`）。区画 `#gt-recipes` を返す |
| DELETE | `/generation-test/recipes/:id` | レシピを削除する。区画 `#gt-recipes` を返す |
| POST | `/generation-test/comfy/open` | 今のフォームの設定のグラフを ComfyUI のワークフロー（`workflows/mam/…`）に置き、`HX-Redirect: /nodes?opened=<ファイル>[&endpoint=<接続先>]` で 204。開けないときは 422 と理由の区画 `#gt-comfy-open` |
| GET | `/generation-test/comfy/exposed` | 「画面に出す値を選ぶ」の画面（クエリ `model`: そのモデルのグラフの入力が表になる） |
| POST | `/generation-test/comfy/exposed` | 選んだ入力（`model`・チェックした `row`＝`<ノード ID>.<入力名>`・`label.<key>`・`min.<key>`・`max.<key>`）をモデルの「画面に出す値」として保存し、画面を返す（検査の失敗は 422 で理由つき） |
| GET | `/generation-test/easy-pick` | かんたんの「使うモデル」と品質のボタンの区画 `#gt-easy-pick` だけ（やりたいことの選択で取り直す。クエリ: `intent`・`quality`・`ref`（入力画像の数をみる）。ブラウザで選んだ入力ファイルを壊さないために、フォーム全体は返さない）。Issue #977 |

| GET | `/generation-test/form` | フォームを画像／動画で切り替える（クエリ: 入力済みのフォームの値。プロンプト・本数・縦横比は保つ。モデルの選択は外す） |
| GET | `/generation-test/prompt-row` | プロンプト欄を 1 つ足す。`?from=<プロンプト履歴の ID>` で内容を入れる |
| GET | `/generation-test/picker` | 入力画像の選択肢。`?project=<ID>`（そのプロジェクトのアセット・参照項目。無い ID は 404）または `?source=history`（履歴の成功した画像） |
| GET | `/generation-test/ref-row` | 選んだ入力画像の 1 行（`?ref=<source>:<project>:<id>`（ポーズ集の画像は `pose_library::<画像 ID>`。役割は `pose` で出す）、`?media=`、`?model=`（選んでいるモデル。役割の選択肢をそれに合わせる）。サムネイル・番号・外す・並べ替えのボタン・役割と強さ（画像は役割 5 種（`reference`・`face`・`style`・`edit`・`pose`）と強さ、動画は 3 種）つき。無ければ 404） |
| GET | `/generation-test/poses` | ポーズ集のモーダルの中身（Issue #986）。未導入なら導入の案内（出典・ライセンス・商用利用の可否・取得するサイズ・同意のチェックボックス `pose_accept`）、導入済みなら絞り込み（`pose_q` 文字・`pose_class` 分類・`pose_angle` アングル）つきの一覧。`?model=`（選んでいるモデル。複数可）で、そのモデルがポーズの役割を読めるかと商用利用の可否を添える。ポーズ集が無い構成は 404 |
| GET | `/generation-test/poses/results` | 一覧の部分だけ（絞り込みの変更で取り直す。クエリは `pose_q`・`pose_class`・`pose_angle`・`pose_kind`〔`line` 線画 / `depth` 深度 / `normal` 法線。既定は線画。Issue #1020〕。解釈できない値は絞り込みなし） |
| GET | `/generation-test/poses/image/:id` | ポーズの画像（`image/webp`。ID は `std_P001_front` の形、深度・法線マップは `std_P001_front.depth`・`.normal`。ポーズ集にない ID・マップの無いアングルは 404。成人向けのポーズ〔`sexy_`・`duo_`。Issue #1020〕は、導入済みでも年齢確認がオフの間は「ポーズ集にない」と同じ 404） |
| POST | `/generation-test/poses/install` | ポーズ集の導入（`pose_accept=on` が要る。無ければ 400 で理由を返し、案内を残す）。成功は一覧を返す。取得に失敗した理由は日本語で出す |
| POST | `/generation-test/poses/remove` | ポーズ集の削除（Issue #994。画像と目録を消す。導入していなくても成功）。成功は導入の案内を返す。導入中は 409 と理由の文 |
| POST | `/generation-test/poses/install-adult` | 成人向けのポーズ（`sexy`・`duo`。Issue #1020）の導入。`pose_accept=on` が要り、**サービスが年齢確認（model-manager.md §6.2）がオフなら断り**（4xx。何も取得せず、ポーズの名前・枚数にも触れない）、全年齢向けのポーズ集が未導入でも断る。成功は一覧を返す。画面はゲートがオンのときだけこの導入の節を出す |
| POST | `/generation-test/poses/remove-adult` | 成人向けのポーズだけを削除する（全年齢向けは残る。導入していなくても成功。ゲートの開閉に関わらずできる）。導入中は 409 |
| GET | `/generation-test/poses/progress` | 導入のダウンロードの進み具合の 1 行（導入の案内が 1 秒ごとに取り直す。「ダウンロード中: x / y（n%）」。導入していないときは空） |
| POST | `/generation-test/estimate` | 見積り（multipart: `tier`（`easy` のとき、`intent` からモデルを決めて見積もる。`model` は読まない）・`intent`・`quality`（`fast`／`standard`／`high`。steps・cfg を名指ししていない項目にモデルのプリセットを入れる）・`media`・`model`（複数）・`prompt`/`negative`（同じ順で複数）・`count`・`aspect_ratio`・`resolution`・`duration_seconds`・`translate`（`1` のとき英語に訳して送る。チェックボックスなので無いときは OFF。新しい画面は ON で開く）・`ref`（選んだ入力。ブラウザで選んだファイルは `upload::#<n>` = `upload` の n 番目）と `role_<ref>`（`reference`・`face`・`style`・`edit`・`pose`、動画は `first_frame`・`last_frame` も）・`strength_<ref>`（0〜1。無ければ既定 0.7）・`avoid_pose`（`on` でポーズ・構図を真似させない）・`lora`・`lora_strength`（既定 0.4）・ファイル `upload`・`upload_first`・`upload_last`）。アップロードは `_staging/` に置く。**何も実行しない**。200 で確認の区画（件数・モデルごとの内訳（`translate` が `1` のときは、訳す文の数の「プロンプトの英訳」の行が入る。LLM の単価が分かれば合計に入り、分からなければ単価なしで入る）・合計・単価なしの注記・警告・上限超過。確認の本文に `estimate_id` と `confirmed_total_usd` を hidden で持つ） |
| POST | `/generation-test/run-notice` | 見積りの警告 `role_unsupported` があるときだけ、確認の区画の実行ボタンが押されて呼ぶ（Issue #959）。確認のフォームをそのまま受け取り、見積りをやり直して、モデルが対応していない役割の参照を全体の参照として使うことを、モデルごとに言うモーダルの本文（HTML の断片。`#mam-modal-body` に入る）を返す。何も実行せず、課金もしない。選択肢は「戻る」（閉じるだけ）と「このまま実行」（`/generation-test/run` へ送る）の 2 つ。受け付けられない入力は 422 の理由の断片 |
| POST | `/generation-test/run` | 確認の区画のフォームの送信。サーバーが見積りをやり直し、`estimate_id` と合計が違う・上限（`gentest.max_items`・`gentest.max_cost_usd`）を超えるなら 409、入力の誤りは 422、英訳が必要なのに LLM が訳せなければ 502（何も生成しない）。202 で、この実行の進捗と比較表のブロック（`section#gt-batch-<batch_id>[data-batch]`）を返す。確認のフォームは `hx-target="#gt-batches" hx-swap="beforeend"` なので、ブロックは**前の実行のブロックの下へ追加**され、前のブロックは消えない（Issue #1109）す |
| GET | `/generation-test/batches/:id` | この実行 1 つ分のブロック（`section#gt-batch-<id>`。行＝モデル、列＝プロンプト。実行の開始時刻・`id`（先頭 8 文字）・確認した見積りの設定（枚数・縦横比・シード・品質ほか）つき）。終わるまで 2 秒ごとに**自分のブロックだけ**（`hx-target="this"`）取り直し、終わると `hx-get`・`hx-trigger` が消えて止まる。他の実行のブロックには触れない。無ければ 404（`id` つきのブロックで返す） |
| POST | `/generation-test/batches/:id/cancel` | 取り消し。始めていない結果を取り消し、呼び出し中のものは終わるのを待つ。終わったバッチは 409 |
| GET | `/generation-test/history` | 履歴（新しい順、24 件ずつ。クエリ `model`・`media`・`favorite=1`・`commercial`・`page`（1 始まり。数字でない・0 以下は 1、最終ページより先は最終ページ）。総件数と「N 件中 a–b 件」・ページ移動のリンク `<nav aria-label="ページ移動">` を持つ。Issue #1079）。直接開くとページ、HTMX は一覧だけ。合計コストの 1 行と、最終更新の時刻・更新ボタンを持つ。`live=1` を足すと、絞り込みの外の変わる部分（`#gt-history-live`: 合計・結果・ページ移動）だけを返す。結果に待機中・生成中のものがあるあいだ、その部分は `hx-trigger="every 2s"` で同じ条件（`model`・`media`・`favorite`・`commercial`・今のページの `page` と `live=1`）を取り直し、完了・失敗・取り消しだけになった答えは属性を持たず、ポーリングが止まる（Issue #925） |
| GET | `/generation-test/export` | 結果の ZIP をストリームで返す（Issue #932。`Content-Type: application/zip`・`Content-Disposition: attachment`・`Cache-Control: no-store`）。`photos/`（`01_20261101-173805_chroma1-hd_bce291.png`）と `prompts/prompts.csv`（UTF-8 BOM つき。列は [mcp-tools-generation-test.md](mcp-tools-generation-test.md) の `export_generation_results` と同じ）。クエリ: `id`（繰り返し。結果の指定。最大 500）・`batch`・`latest`（1〜500。`id`・`batch` が無ければ省略時 40）・`model`・`media`・`favorite=1`・`commercial`（`all` / `ok` / `ok_or_conditional`。省略時 `all`）。条件は AND。書き出せる結果が無い・値が不正は 422（`text/plain` で理由。ZIP を作らない）。**`HX-Request: true` のときは先に調べるだけ**: 書き出せないなら同じ状態（422・404・500）で理由の断片（`exportbar.Problem`。`text/html`、`data-testid="gt-export-error"`）を返し、書き出せるなら ZIP は返さず `204` と `HX-Redirect`（同じ URL）を返す（ブラウザ・デスクトップの窓がその URL をダウンロードとして読み込み、画面は変わらない）。画面の書き出しバーは 2 つの GET フォーム（`hx-get` で先に調べ、理由は `#gt-export-status` に出す。スクリプトが無くても素の GET として動く）で、選んだカードの `id` を送るものと、絞り込み（`model`・`media`・`favorite`・`commercial`）の新しい順 `latest` 件を送るもの |
| GET | `/generation-test/results/:id` | 結果の詳細（設定・実際に送ったプロンプト・シード・入力画像・取り込み先・取り込みフォーム）。無ければ 404。**一覧のタイル（Issue #1004）は htmx で `HX-Target: mam-modal-body` を付けて開き、その場合はページではなくモーダルの本文（`modal.Frame`。見出し＝モデル名、`data-modal-wide` で広いダイアログ）を返す**。`?pick=1` は履歴のタイルから（書き出しに選ぶチェックを出す）。`?upscale_scale=&upscale_model=` は「大きくする」の箱の初期値（アップスケールの結果の「同じ倍率でもう一度」）。無い・別の呼び方はページ。404 のモーダルは `modal.Notice`。応答は `Vary: HX-Request, HX-Target` |
| GET | `/generation-test/results/:id/file` | 結果のファイル。保存した MIME のまま `Content-Security-Policy: sandbox`・`X-Content-Type-Options: nosniff`。`?download=1` で `Content-Disposition: attachment`。完了していない・無い結果は 404 |
| GET | `/generation-test/inputs/:id/file` | バッチの入力画像のファイル（同じ扱い） |
| POST | `/generation-test/results/:id/favorite` | お気に入りの付け外し（`favorite=1`/`0`）。ボタンと、一覧のタイルのハート（`#gt-fav-mark-<id>`、`hx-swap-oob`）を返す（モーダルで付けた印が後ろの一覧に出る） |
| POST | `/generation-test/results/:id/import` | プロジェクトへの取り込み（`project_id`・`as`=`reference`/`asset`・`kind`・`name`・`tags`・`assign_target`/`assign_id`/`assign_slot`）。取り込みフォームの区画を返す |
| POST | `/generation-test/results/:id/upscale` | 結果を 2 倍・4 倍に大きくする（Issue #975。[../architecture/gentest/gentest.md](../architecture/gentest/gentest.md) §3.4）。フォーム `scale`（`2`/`4`）・`upscale_model`（方式のモデル識別子。省くとプロンプトの要らない最初のもの）。見積り・確認は要らない（ローカルのモデルで無料）。成功は HTMX なら `204` と `HX-Redirect: /generation-test/history`（そうでなければ `303`）で、新しい結果は別のバッチ・別のファイルの結果として履歴に出る。倍率が 2・4 以外・アップスケール用のモデルが入っていない・名指しのモデルが使えない・出力が上限（64 Mpx。2 パスは 16 Mpx）を超える・元が画像として使えない、は `422` で、箱（`#gt-upscale`）を理由つきで返す。結果が無ければ 404 |
| POST | `/generation-test/results/:id/video-enhance/estimate` | Topaz（有料クラウド）の**見積り**を取る（Issue #1031。[../architecture/gentest/gentest.md](../architecture/gentest/gentest.md) §3.9）。フォーム `op`（`upscale`/`interpolate`）・`model`（`topaz:<モデル名>`）・`size`（アップスケール: `scale-2`＝2 倍・`short-720`・`short-1080`＝短い辺をその長さに）・`interp`（コマ補間: `2`・`4`・`fps`）・`target_fps`（`interp=fps` のときの出力の fps。Topaz だけ。15〜240）。課金されない。成功は `200` で、見積りの枠（`#gt-venh-quote-<op>`。「このくらいのクレジットを使います（上限 N クレジット）。1 クレジットの金額はプランによります」・出力の大きさか fps・`Powered by Topaz Labs`・確認のチェック `confirm` 付きの実行フォーム。`estimate_id` と `confirmed_credits` は hidden）を返す。見積りが取れない・キーが無い・値が不正・ローカルのモデルは `422` などで、理由だけの枠（`gt-venh-quote-error`）を返し、実行のボタンは出さない |
| POST | `/generation-test/results/:id/video-enhance` | 動画の結果を大きくする・コマを増やして滑らかにする（Issue #1031）。フォームは上と同じ `op`・`model`・`size`・`interp`・`target_fps`。**ローカルのモデル**（無料）は確認なしでその場で始まる。**Topaz** は `estimate_id`・`confirmed_credits`（見積りの上限と同じ額）と `confirm=1`（見積りの内容を確認して Topaz に送るチェック）が揃わないと送らず（`confirm` が無ければ `422`、見積りの期限切れ・額や条件の違いは `409`）、使った見積りは使い切り。成功は HTMX なら `204` と `HX-Redirect: /generation-test/history`（そうでなければ `303`）で、できた結果は別のバッチ・別のファイルの結果として履歴に出る。断り（動画でない・モデルが使えない・値が不正・ffmpeg が無い 〔共通エラー〕など）は箱 `#gt-video-enhance` を理由つきで `422`（状態の食い違いは `409`、結果が無ければ `404`）で返す 元が成人向けのモデルの結果（処理した結果も同じ）のときは、さらに `adult_confirm=1`（成人向けの警告を読んで確認したチェック）が要る。なければ 422 で警告を返し、送らない。 |
| POST | `/generation-test/results/:id/face-swap` | 結果の顔を別の画像の顔に入れ替える（Issue #995。[../architecture/gentest/face-swap.md](../architecture/gentest/face-swap.md) §4）。multipart: 顔の画像 `face`（必須）と任意の `swapper`・`detector`・`enhancer`（既定 ghost_2_256・yunet・gfpgan_1.4。`enhancer=none` で復元しない）。成功は **204** と履歴への `HX-Redirect`（`/generation-test/history`。新しい結果が実行中のまま並ぶ）。顔の画像が無い・未知のモデル・FaceFusion が見つからない・入れ替え先が画像でないは **422**（箱 `#gt-faceswap` を選択を残して返す）、結果が無いは 404 |
| GET | `/generation-test/results/:id/face-swap/warnings` | 選んだ 3 つのモデル（クエリ `swapper`・`detector`・`enhancer`）のライセンスの警告の区画 `#gt-faceswap-warnings`（選択を変えたときに箱が取り直す。非商用・研究用・商用不明のモデルがあれば、その注意の文）。未知のモデルは警告なし（実行が断る） |
| DELETE | `/generation-test/results/:id` | 結果とファイルの削除。成功は `HX-Redirect: /generation-test/history`（結果のページから）。**モーダルから（`HX-Target: mam-modal-body`）は、一覧の要素 `#gt-result-<id>` を `hx-swap-oob="delete"` で外し、`HX-Trigger-After-Swap: modal-close` で閉じる（ページは動かさない。書き出しの選びや位置が残る）**。生成中は 409（`HX-Retarget: #gt-detail`・詳細を消さずに理由を上に出して返す）、無ければ 404 |
| GET | `/generation-test/prompts` | プロンプト履歴（再利用のリンク・削除） |
| GET | `/generation-test/match` | プロンプトマッチの画面（Issue #916。[../architecture/gentest/prompt-match.md](../architecture/gentest/prompt-match.md)）。参考の画像 1 枚・モデル・進め方（おまかせ / 1回だけ / 調整）・目標の似てる度・上限の回数・言葉の指示。クエリ `ref`（`<source>:<project>:<id>` の画像を参考にして開く）・`model`。プロンプトマッチが無いアプリは 503 |
| POST | `/generation-test/match/estimate` | プロンプトマッチの見積り（multipart: `upload`（参考の画像）または `ref`・`model`・`mode`（`auto`/`once`/`adjust`）・`target_score`（1〜100）・`max_rounds`（1〜10）・`max_cost_usd`（この実行で使ってよい費用。見積りが超えるなら実行ボタンが無効）・`instruction`（1000 文字まで）・続きは `continue_batch_id`）。確認の区画（最大の回数・モデルの内訳・LLM の呼び出しの行（単価の分かるものは合計に入る。分からないものは「単価なし」で入らない）・上限超過）を返す。拒否は 422・上限超過は確認にそのまま出る。`middleware` のアップロード用ルートの一覧に載せ、本文の上限を広げている |
| POST | `/generation-test/match/run` | 確認の区画のフォームの送信。見積りをやり直し、`estimate_id` と合計が違う・上限を超えるなら 409、入力の誤りは 422。202 でタイムライン（`#gt-batch`）と、言葉の指示のフォーム（`#gt-match-adjust`。帯域外）を返し、確認の区画は空にする |
| GET | `/generation-test/match/batches/:id` | タイムライン（いちばん似ている回・目標・各回の画像・似てる度（顔を混ぜているときは点の内訳）・直したところ・違い・プロンプトと直す前のプロンプト・ベストのプロンプトのコピー／生成テストで比べる・各回の「プロンプトで比べる」）。終わるまで 2 秒ごとに自分を取り直す。無ければ 404 |
| POST | `/generation-test/match/batches/:id/cancel` | 取り消し。始めていない回は取り消し、次の回は始まらない（作っている途中の絵は終わるのを待つ） |
| POST | `/generation-test/match/batches/:id/adjust` | 言葉の指示（`instruction`）と `mode`（`adjust` = 1 回直す・`auto` = 自動で続ける）。**実行中**は次の直しに取り込む（確認の区画に 1 行の通知）。**終わっていれば**続きの見積りを確認の区画に返す（実行は `/match/run` に `continue_batch_id` つきで） |
| DELETE | `/generation-test/prompts/:id` | プロンプト履歴の削除（結果は残る）。一覧の区画を返す |
| GET | `/generation-test/inpaint` | 一部だけ描き直す（Issue #974）。クエリ `ref`（`<source>:<project>:<id>` の元画像）・`model`（対応するモデルを選ぶ。対応しない `model` は最初の対応モデル）。マスクを塗る部品・プロンプト・描き直しの強さ・対応モデルだけが選べる。対応しないモデルは「一部だけ描き直すに対応していません」で無効 |
| POST | `/generation-test/inpaint/estimate` | 見積り（multipart: `model`・`prompt`・`negative`・`denoise`（0 より大きく 1 以下）・`translate`（`1` で英訳）・`ref` またはファイル `source`・ファイル `mask`（白が描き直す PNG））。200 で確認の区画（`#gt-inpaint-out`。hidden にステージした `ref` と `mask`、`estimate_id`、`confirmed_total_usd`）。対応しないモデル・真っ黒なマスク・範囲外の強さは 422 |
| POST | `/generation-test/inpaint/run` | 確認の hidden を送る。見積りが違えば 409、入力の誤りは 422。200 で「始めました」（結果は履歴の別ファイル。進捗の比較表は出さない） |

`POST /generation-test/estimate` は `middleware.MaxBody` のアップロード用ルートの一覧に載せ、入力画像のために本文の上限を広げている（読み取りはハンドラが `MaxBytesReader` で制限する）。

## エラーの対応

| サービスのエラー | HTTP | MCP |
|---|---|---|
| `*InvalidError`（モデル・プロンプト・本数・縦横比・解像度・長さ・入力画像・使えないモデル・取り込みの種類・英訳に使う LLM が無い〔Translator が nil で、英訳 ON のまま英訳する文があるとき〕） | 422 | `validation` |
| `*ConflictError`（見積りの不一致・上限超え・終わっていない結果の削除・終わったバッチの取り消し） | 409 | `conflict` |
| `*PartialImportError`（取り込み済みで、スロットへの割り当てが LOCK 等で拒否された） | 409（メッセージに「画像は取り込みました」） | `locked` 等（メッセージに ID） |
| `*UpstreamError`（英訳に LLM が使えない・訳せなかった。何も生成していない。Issue #913。リクエストの取り消し・期限切れは対象外: LLM の失敗ではないので 502 にしない。Issue #1072） | 502 | `upstream` |
| `ErrNotFound` | 404 | `not_found` |
| `ErrUnavailable`・アプリに生成テストが無い | 503 | `unavailable` |
| 画像が大きすぎる | 413 | `validation` |

## 詳細設定のフィールド（Issue #973・#972）

`POST /generation-test/estimate` と `/run`（確認の hidden）は、次の項目も受ける。どれも空なら送らない（モデルの既定。シードはランダム）。

|name|意味|範囲|
|---|---|---|
|`seed`|固定するシード（n 本目は `seed + n`）|0〜4294967295|
|`steps`|ステップ数|1〜150|
|`cfg`|CFG|0〜30|
|`sampler`・`scheduler`|サンプラー・スケジューラー（ComfyUI の名前）|一覧にあるもの|

範囲外・数でない値は 400（`validation`。理由は確認の区画に出る）。受け取らないモデルへの値と、Speed mode のモデルの `steps`・`cfg`（速さのモードが優先）は、見積りの警告 `sampling_ignored`（シードは `seed_ignored`）で使わない。プロンプトのウェイトを読まないモデルには警告 `prompt_weight`。仕様は [../architecture/gentest/gentest.md](../architecture/gentest/gentest.md) §3.1・§3.3。画面は `#gt-compose` の中の折りたたみ `details[data-gt-advanced]`（[../components/gentest-screen.md](../components/gentest-screen.md)）。

## かんたん・こだわり・結果のボタン（Issue #977）

- **段**: 画像のフォームは `tier`（`easy`・`detail`・`comfy`）。かんたん（`easy`）のフォームは `model` を持たず、`intent`（`photo`・`anime`・`landscape`・`face`）と `quality`（`fast`・`standard`・`high`）と `aspect_ratio`（`9:16`・`16:9`・`1:1`）を送る。`POST /generation-test/estimate` は `intent` から、導入済みで使えるモデルを選び（`intentapply.Apply`（`service/gentest/intentapply`））、確認の hidden に `model` を入れて返す（`/run` は見積りと同じ入力を走らせる）。合うモデルが無い・顔の写真が無い（`face`）ときは 422 で理由（モデルマネージャーでの導入の案内を含む）。
- **品質**: `quality` は `Request.Quality`。`fast`・`high` は、**リクエストが `steps`・`cfg` を名指ししていない項目だけ**に、モデルのプリセット（`list_generation_models` の `sampling.quality_presets`）の値を入れる。`standard`・無しは何も上書きしない（テンプレートの値のまま）。`quality` の値が違えば 422（`validation`）。プリセットの無いモデル（クラウド・蒸留・速さのモード）への `quality` は、見積りの警告 `quality_ignored`（標準の値で作る）。確認の hidden は `quality`（標準以外）を持つ。
- **こだわり**: 詳細設定のフィールド（上の節）に加え、かんたんから移るときは `easy_intent`・`easy_quality`（フォームが持ち回るだけ。リクエストは読まない）。
- **結果の「もう1枚」・「もう一度」**: `GET /generation-test?tier=detail&again=<結果の ID>[&same_seed=1]`。記録された `params` から、モデル・プロンプトとネガティブ・英訳・縦横比・steps・cfg・サンプラー・スケジューラー・LoRA・ポーズを真似させない設定・入力画像（履歴・プロジェクト・ポーズ集。役割と強さ）を入れる。`same_seed=1` はシードも入れる（シードを渡せなかったモデルは入れず、理由を出す）。結果が英訳つきなら、`same_seed=1` は**前回送った英訳（`translated_prompt`・`translated_negative_prompt`）をプロンプト欄にそのまま入れ、英訳のスイッチは切る**（英訳を LLM にやり直させると、同じシードでも別の絵になりうるため。理由は画面に出す。`same_seed` なしは入力した元の文と英訳のスイッチを戻す）。アップロードした入力画像・消えた入力画像・使えなくなったモデルは理由を出す。画像の結果でファイルがあり、インペイント・アップスケールでない結果だけ。
- 結果の詳細は、ボタン列（`data-testid="gt-next"`）と折りたたみの「使った設定」（`data-testid="gt-used"`）を返す（画像でファイルがある結果だけ）。

## レシピ・ComfyUI の段（Issue #977 ステージ 2）

- **フォームの項目**: `exposed.<ノード ID>.<入力名>`（モデルの「画面に出す値」。空欄はグラフの値のまま。見積りの確認の hidden にも入る）。`GET /generation-test` と `GET /generation-test/form` の `recipe=<id>` は、レシピの設定をこだわりのフォームに入れる（プロンプトと入力画像は今のまま）。かんたんの `intent` は `recipe:<id>`（選択肢のレシピ）も取り、見積りはそのレシピのモデルと設定で行う。
- **結果**: 詳細の「使った設定」に「画面に出した値 <key>」の行が付く。「もう1枚」「もう一度」は入力欄に値を戻す。
- 意味は [../components/gentest-tiers.md](../components/gentest-tiers.md) §9・§10。

## 画面

区画は上から: **作成フォーム**（`#gt-compose`: 画像／動画・モデル・プロンプト・パラメーター・入力画像）・**確認**（`#gt-confirm`）・**実行のブロックの列**（`#gt-batches`。「この内容で実行する」のたびに、その実行のブロック `#gt-batch-<batch_id>`〈進捗と比較表〉が下へ足される。Issue #1109）。モデルはカード（タップで選ぶ。見えないチェックボックス `model` が値を送る）で無効なものも並べ、選べない理由はカードでなく「?」ヘルプと読み上げにある（Issue #906）。見積りの確認は、件数・モデルごとの内訳と合計・「単価なし」のモデルは合計に入らないという注記・警告・上限超過（実行ボタンを無効にする）を出す。結果のカードは完了なら画像（`<img>`）か動画（`<video controls>`）、失敗なら理由。

## テスト

`handler/gentest` の画面テスト（`gentesttest` の偽のバックエンド・実際の DB とジョブキューで、HTMX のリクエストを CSRF のクッキーとヘッダーつきで送る）: モデルの一覧と理由・フォームの切り替え・プロンプト履歴とピッカー、見積り（内訳・警告・拒否・上限超過・単価なし）、実行（全組み合わせ・比較表・確認と違う見積りの 409・アップロードの入力）、取り消し、履歴（絞り込み・ページ・お気に入り・ファイルの sandbox・詳細・削除）、プロンプト履歴の削除、取り込み（参照・アセット・スロット・拒否・LOCK）、CSRF。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-26 | 新規作成（ルート・エラーの対応・画面・テスト） | Issue #835。生成テストの画面 |
| 1.1 | 2026-11-01 | 「画面」の節で、モデルはカードのタップで選ぶもの（見えないチェックボックス `model` が値を送る）に改め、選べない理由はカードでなく「?」ヘルプと読み上げにあることを足した | Issue #906。生成テスト画面の文字を減らし、モデルをカード選択にした |
| 1.2 | 2026-11-01 | 見積りの `translate`（英語に訳して送る。無ければ OFF）と確認の「プロンプトの英訳」の行、`/run` の 502（訳せなかった）、エラーの対応の `*UpstreamError` を足した | Issue #913 |
| 1.3 | 2026-11-02 | 見積りに `strength_<ref>`・`avoid_pose`・`lora`・`lora_strength` と役割 `face`・`style`・`edit`、ブラウザで選んだファイルの `upload::#<n>`、`ref-row` に `?model=` を足した | Issue #933 |
| 1.4 | 2026-11-02 | `upload::#<n>` の n は届いたファイルの位置とし、空のファイルは保存せず番号を詰めない（その欄は捨てる。範囲外・数でない・同じ番号の 2 つ目も捨てる）。強さの値は範囲外・無限大を 0〜1 に丸める | Issue #933（PR #953 のレビュー） |
| 1.5 | 2026-11-02 | `GET /generation-test/history` に `live=1`（変わる部分だけ）、最終更新の時刻、更新ボタン、結果が動いているあいだの 2 秒ごとの取り直しを足した | Issue #925。履歴が生成の完了や進み具合をリアルタイムに映さなかった |
| 1.6 | 2026-11-02 | プロンプトマッチ（`GET /generation-test/match` と `/match/estimate`・`/match/run`・`/match/batches/:id`・`.../cancel`・`.../adjust`）を足した。`GET /generation-test` に `from_result`（プロンプトマッチの回の結果のプロンプト・モデル・縦横比を入れて開く）。結果の詳細に、回の記録と「画像から似せる」への入口を足した | Issue #916 |
| 1.7 | 2026-11-02 | `GET /generation-test/export`（結果の ZIP）を足した。履歴の書き出しバー（選んだカード・絞り込みの新しい順 N 件）の `id`・`latest` ほか | Issue #932 |
| 1.8 | 2026-11-02 | `GET /generation-test/export` に、`HX-Request` のときの事前の確認（理由の断片・`HX-Redirect`）を足した | 統合レビュー（#932）。書き出せないときのプレーンテキストが画面全体を置き換え、戻る導線が無かった |
| 1.9 | 2026-11-02 | `POST /generation-test/run-notice` を足した（未対応の役割のモーダルの本文）。`POST /generation-test/run` の応答に `HX-Trigger-After-Swap: modal-close` を付けた（モーダルから実行したときに閉じる） | Issue #959 |
| 1.10 | 2026-11-02 | プロンプトマッチ: 確認の区画の LLM の行を、単価の分かるものは合計に入れる 2 行に（Issue #965）。タイムラインに点の内訳（顔を混ぜているとき。Issue #964）と、各回の「プロンプトで比べる」（`GET /generation-test?from_result=<結果の id>`。その回のプロンプト・モデル・縦横比で開き、モデルを足して比較する案内を出す） | Issue #964・#965 |
| 1.11 | 2026-11-03 | 見積り・実行に詳細設定のフィールド `seed`・`steps`・`cfg`・`sampler`・`scheduler` を足した（範囲・警告 `sampling_ignored`・`seed_ignored`・`prompt_weight`） | Issue #973・#972 |
| 1.12 | 2026-11-03 | `POST /generation-test/results/:id/upscale`（結果を 2 倍・4 倍に大きくする。成功は履歴への `HX-Redirect`・断りは箱を 422 で返す）を足した。結果の詳細に「大きくする」の箱 | Issue #975 |
| 1.13 | 2026-11-03 | ポーズ集の 4 ルート（`/generation-test/poses`・`/poses/results`・`/poses/image/:id`・`POST /poses/install`）と、`ref-row` の `pose_library::<ID>`、役割 `pose` | Issue #986 |
| 1.14 | 2026-11-02 | `GET/POST /generation-test/inpaint`（見積り・実行）を足した。結果の詳細に「一部だけ描き直す」と、インペイント結果の「元の結果」 | Issue #974 |
| 1.15 | 2026-11-03 | 画像のフォームの 3 段（`tier`・`intent`・`quality`・`easy_intent`・`easy_quality`）、`GET /generation-test/easy-pick`、`GET /generation-test` の `tier`・`again`・`same_seed`・`ref_role`、見積りの警告 `quality_ignored`、結果の「もう1枚」「もう一度」「この顔で作る」を足した | Issue #977（ステージ 1） |
| 1.16 | 2026-11-04 | レシピの 4 ルート（`/generation-test/recipes`…）、`/generation-test/comfy/open`、`/generation-test/comfy/exposed`（GET・POST）、フォームの `exposed.<key>`・`recipe`、かんたんの `intent=recipe:<id>` | Issue #977（ステージ 2） |
| 1.17 | 2026-11-03 | `GET /generation-test/results/:id` を、一覧のタイルから `HX-Target: mam-modal-body` で呼ぶとモーダルの本文を返すようにした（`?pick=1`・`?upscale_scale=&upscale_model=`）。お気に入りはタイルのハートも out of band で返し、削除はモーダルからなら一覧の要素を外して閉じる（`HX-Redirect` は結果のページからだけ）。`GET /generation-test/inpaint?again=<結果>&same_seed=1`（描き直しの結果の「もう一度」: 画像・マスク・プロンプト・強さを入れて開く）、`POST /generation-test/inpaint/*` の `seed`、`ref` の `batch_input::<入力 ID>` | Issue #1004・#977 |
| 1.18 | 2026-11-03 | 「この設定でもう一度」（`same_seed=1`）: 英訳つきの結果は、前回送った英訳をそのまま入れて英訳を切る（英訳のやり直しで同じシードでも別の絵になるのを防ぐ） | Issue #994 (d) |
| 1.19 | 2026-11-03 | ポーズ集の削除 `POST /generation-test/poses/remove` と進み具合 `GET /generation-test/poses/progress` | Issue #994 (e) |
| 1.20 | 2026-11-03 | `POST /generation-test/results/:id/face-swap`（顔の入れ替え）と `GET …/face-swap/warnings`（選んだモデルのライセンスの警告）を足した。結果の詳細に「顔の類似度」の行（役割 `face` の参照があった結果。SFace）が出る | Issue #995・#998 |
| 1.21 | 2026-11-03 | `GET /generation-test/poses/results` に `pose_kind`（`line` / `depth` / `normal`。既定は線画）、`poses/image/:id` と `ref-row` の画像 ID に `.depth`・`.normal`（深度・法線マップ） | Issue #1020 |
| 1.22 | 2026-11-04 | `POST /generation-test/poses/install-adult`・`/poses/remove-adult` を足した。成人向けのポーズ（`sexy`・`duo`）の画像・一覧・行への追加は、年齢確認がオフの間は 404／出さない。履歴の入力（`GET /generation-test/inputs/:id/file`）は、成人向けのポーズから取った写しをゲートがオフの間は 404 にする | Issue #1020 |
| 1.23 | 2026-11-04 | `POST /generation-test/results/:id/video-enhance/estimate`・`POST /generation-test/results/:id/video-enhance`（成功した動画の結果を大きくする・コマを増やして滑らかにする。Topaz は見積り → 確認 → 実行）を足した。結果の詳細に動画用の箱 `#gt-video-enhance`、ボタン行に「大きくする」「滑らかにする」、設定の欄に `video_enhance` の行 | Issue #1031 |
| 1.24 | 2026-11-05 | `POST …/video-enhance` に `adult_confirm`（成人向けの結果を Topaz に送るとき必須）を足した。見積りの画面は警告を出す | Issue #1031（悠太さんの決定 2026-11-05） |
| 1.25 | 2026-11-06 | 見積りの「プロンプトの英訳」の行は、LLM の単価が分かれば合計に入る（Issue #1069）。英訳中のリクエストの取り消しは 502 にしない（Issue #1072） |
| 1.26 | 2026-11-06 | 入口の記述から「モデルマネージャーの試し生成の区画のリンク」を外し、「?」ヘルプのリンクにした（ルート・フィールドは変えない。状態は仕様のみ） | Issue #1100 |
| 1.27 | 2026-11-07 | `POST /generation-test/run` の答えを、実行ごとのブロック（`#gt-batch-<batch_id>`）にし、確認のフォームの送り先を `#gt-batches`（`hx-swap=beforeend`）にした。前の実行のブロックは残り、新しい実行は下へ足される。`GET …/batches/:id`・`POST …/batches/:id/cancel` の答えも同じブロックで、自分のブロックだけを置き換える（状態は実装済み） | Issue #1109 |
