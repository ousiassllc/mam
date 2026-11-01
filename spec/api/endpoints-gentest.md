# エンドポイント: 生成テスト画面

プロジェクトに属さない画像・動画の比較生成の画面 `GET /generation-test`（Issue #835）。意味・見積り・ジョブ・失敗の扱いは [../architecture/gentest/gentest.md](../architecture/gentest/gentest.md)、テーブルは [../architecture/gentest/er-gentest.md](../architecture/gentest/er-gentest.md)。画面の全操作は、同じサービス（`gentest.Service`）を呼ぶ MCP ツールでもできる（[mcp-tools-generation-test.md](mcp-tools-generation-test.md)）。入口はプロジェクト一覧（分析・設定の隣）のリンクと、モデルマネージャーの試し生成の区画のリンク。

POST・DELETE は CSRF 必須。操作は区画（`outerHTML`）を返し、ジョブの進捗は 2 秒ごとに取り直す（`hx-trigger="every 2s"`。終わると止まる。WebSocket は使わない）。拒否はページのトーストではなく区画の中のメッセージで返す（下の「エラーの対応」）。

## ルート

| メソッド | パス | 内容 |
|---|---|---|
| GET | `/generation-test` | 画面（作成フォーム・確認・進捗）。クエリ `model`（そのモデルを選んで開く。動画モデルなら動画のフォーム）・`prompt`（プロンプト履歴の ID でプロンプトを入れる）・`ref`（`<source>:<project>:<id>` の入力画像を足す） |
| GET | `/generation-test/form` | フォームを画像／動画で切り替える（クエリ: 入力済みのフォームの値。プロンプト・本数・縦横比は保つ。モデルの選択は外す） |
| GET | `/generation-test/prompt-row` | プロンプト欄を 1 つ足す。`?from=<プロンプト履歴の ID>` で内容を入れる |
| GET | `/generation-test/picker` | 入力画像の選択肢。`?project=<ID>`（そのプロジェクトのアセット・参照項目。無い ID は 404）または `?source=history`（履歴の成功した画像） |
| GET | `/generation-test/ref-row` | 選んだ入力画像の 1 行（`?ref=<source>:<project>:<id>`、`?media=`。動画は役割の選択つき。無ければ 404） |
| POST | `/generation-test/estimate` | 見積り（multipart: `media`・`model`（複数）・`prompt`/`negative`（同じ順で複数）・`count`・`aspect_ratio`・`resolution`・`duration_seconds`・`translate`（`1` のとき英語に訳して送る。チェックボックスなので無いときは OFF。新しい画面は ON で開く）・`ref`（選んだ入力）と `role_<ref>`・ファイル `upload`・`upload_first`・`upload_last`）。アップロードは `_staging/` に置く。**何も実行しない**。200 で確認の区画（件数・モデルごとの内訳（`translate` が `1` のときは、訳す文の数の「プロンプトの英訳」の行が単価なしで入る）・合計・単価なしの注記・警告・上限超過。確認の本文に `estimate_id` と `confirmed_total_usd` を hidden で持つ） |
| POST | `/generation-test/run` | 確認の区画のフォームの送信。サーバーが見積りをやり直し、`estimate_id` と合計が違う・上限（`gentest.max_items`・`gentest.max_cost_usd`）を超えるなら 409、入力の誤りは 422、英訳が必要なのに LLM が訳せなければ 502（何も生成しない）。202 で進捗と比較表の区画（`#gt-batch`）を返す |
| GET | `/generation-test/batches/:id` | 進捗と比較表（行＝モデル、列＝プロンプト）。終わるまで 2 秒ごとに自分を取り直す。無ければ 404 |
| POST | `/generation-test/batches/:id/cancel` | 取り消し。始めていない結果を取り消し、呼び出し中のものは終わるのを待つ。終わったバッチは 409 |
| GET | `/generation-test/history` | 履歴（新しい順、24 件ずつ。クエリ `model`・`media`・`favorite=1`・`offset`）。直接開くとページ、HTMX は一覧だけ。合計コストの 1 行を持つ |
| GET | `/generation-test/results/:id` | 結果の詳細（設定・実際に送ったプロンプト・シード・入力画像・取り込み先・取り込みフォーム）。無ければ 404 |
| GET | `/generation-test/results/:id/file` | 結果のファイル。保存した MIME のまま `Content-Security-Policy: sandbox`・`X-Content-Type-Options: nosniff`。`?download=1` で `Content-Disposition: attachment`。完了していない・無い結果は 404 |
| GET | `/generation-test/inputs/:id/file` | バッチの入力画像のファイル（同じ扱い） |
| POST | `/generation-test/results/:id/favorite` | お気に入りの付け外し（`favorite=1`/`0`）。ボタンを返す |
| POST | `/generation-test/results/:id/import` | プロジェクトへの取り込み（`project_id`・`as`=`reference`/`asset`・`kind`・`name`・`tags`・`assign_target`/`assign_id`/`assign_slot`）。取り込みフォームの区画を返す |
| DELETE | `/generation-test/results/:id` | 結果とファイルの削除。成功は `HX-Redirect: /generation-test/history`。生成中は 409、無ければ 404 |
| GET | `/generation-test/prompts` | プロンプト履歴（再利用のリンク・削除） |
| DELETE | `/generation-test/prompts/:id` | プロンプト履歴の削除（結果は残る）。一覧の区画を返す |

`POST /generation-test/estimate` は `middleware.MaxBody` のアップロード用ルートの一覧に載せ、入力画像のために本文の上限を広げている（読み取りはハンドラが `MaxBytesReader` で制限する）。

## エラーの対応

| サービスのエラー | HTTP | MCP |
|---|---|---|
| `*InvalidError`（モデル・プロンプト・本数・縦横比・解像度・長さ・入力画像・使えないモデル・取り込みの種類） | 422 | `validation` |
| `*ConflictError`（見積りの不一致・上限超え・終わっていない結果の削除・終わったバッチの取り消し） | 409 | `conflict` |
| `*PartialImportError`（取り込み済みで、スロットへの割り当てが LOCK 等で拒否された） | 409（メッセージに「画像は取り込みました」） | `locked` 等（メッセージに ID） |
| `*UpstreamError`（英訳に LLM が使えない・訳せなかった。何も生成していない。Issue #913） | 502 | `upstream` |
| `ErrNotFound` | 404 | `not_found` |
| `ErrUnavailable`・アプリに生成テストが無い | 503 | `unavailable` |
| 画像が大きすぎる | 413 | `validation` |

## 画面

区画は上から: **作成フォーム**（`#gt-compose`: 画像／動画・モデル・プロンプト・パラメーター・入力画像）・**確認**（`#gt-confirm`）・**進捗と比較表**（`#gt-batch`）。モデルはカード（タップで選ぶ。見えないチェックボックス `model` が値を送る）で無効なものも並べ、選べない理由はカードでなく「?」ヘルプと読み上げにある（Issue #906）。見積りの確認は、件数・モデルごとの内訳と合計・「単価なし」のモデルは合計に入らないという注記・警告・上限超過（実行ボタンを無効にする）を出す。結果のカードは完了なら画像（`<img>`）か動画（`<video controls>`）、失敗なら理由。

## テスト

`handler/gentest` の画面テスト（`gentesttest` の偽のバックエンド・実際の DB とジョブキューで、HTMX のリクエストを CSRF のクッキーとヘッダーつきで送る）: モデルの一覧と理由・フォームの切り替え・プロンプト履歴とピッカー、見積り（内訳・警告・拒否・上限超過・単価なし）、実行（全組み合わせ・比較表・確認と違う見積りの 409・アップロードの入力）、取り消し、履歴（絞り込み・ページ・お気に入り・ファイルの sandbox・詳細・削除）、プロンプト履歴の削除、取り込み（参照・アセット・スロット・拒否・LOCK）、CSRF。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-26 | 新規作成（ルート・エラーの対応・画面・テスト） | Issue #835。生成テストの画面 |
| 1.1 | 2026-11-01 | 「画面」の節で、モデルはカードのタップで選ぶもの（見えないチェックボックス `model` が値を送る）に改め、選べない理由はカードでなく「?」ヘルプと読み上げにあることを足した | Issue #906。生成テスト画面の文字を減らし、モデルをカード選択にした |
| 1.2 | 2026-11-01 | 見積りの `translate`（英語に訳して送る。無ければ OFF）と確認の「プロンプトの英訳」の行、`/run` の 502（訳せなかった）、エラーの対応の `*UpstreamError` を足した | Issue #913 |
