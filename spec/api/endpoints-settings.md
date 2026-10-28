# API 仕様: 全体の設定画面（`/settings`）のルートと応答

[endpoints.md](endpoints.md) §1 の `GET /settings` の行の本体。画面の作りは [../components/settings-screen.md](../components/settings-screen.md)（枠・ナビ・概要）・[../components/settings-screen-categories.md](../components/settings-screen-categories.md)（分類ごとの一覧とモーダル）、部品は [../components/shared-ui-parts.md](../components/shared-ui-parts.md)、新しいルートの経緯と応答の決まりの下書きは [../components/settings-screen-routes.md](../components/settings-screen-routes.md)（Issue #847）。「ローカル・代替モデル」のルートは [endpoints-local-models.md](endpoints-local-models.md)。`endpoints.md` は行数の上限（300 行）に収めるため、この分類のルートはここに書く。

実装の段階（settings-screen.md §7）: ① ナビ・分類ごとの URL・概要、②-1 モーダルと一覧の行の部品、②-2 API キーとログイン、②-3 テキスト生成（LLM）、②-4 画像・動画モデル、②-5 ローカル・代替モデル、②-6 MCP サーバーが実装済み。

## 1. ページ（HTMX でないときはページ全体、HTMX のときは本体だけ）

すべて GET。`Cache-Control: no-store` と `Vary: HX-Request` を付ける（履歴の復元の再取得は `HX-History-Restore-Request: true` で、HTMX でも**ページ全体**を返す）。出す条件を満たさないルートは 404。

| Path | 内容 | 出す条件 |
|------|------|----------|
| `/settings` | **概要**。分類ごとのカード（`molecules.ToolCard`。名前・1 行の説明・いまの状態・「開く」）。先頭が「API キーとログイン」、続いて「テキスト生成（LLM）」「画像・動画モデル」「ローカル・代替モデル」「MCP サーバー」「バージョンとアップデート」、最後に「モデルマネージャー」（別の画面 `/tools/model-manager`）。カードの状態は設定ファイル・キーストアを読むだけで、外部へは通信しない | 常に |
| `/settings/api-keys` | API キーとログイン（API キーのカード、ChatGPT のログインのカード 3 枚） | 常に |
| `/settings/llm` | テキスト生成（Agent ごとの表。§3 の「表の行の保存」。使われない Agent は折りたたみ） | LLM のサービスがあるとき |
| `/settings/models` | 画像・動画モデル（画像の用途の表と Quality Policy の表。§3 の「表の行の保存」） | モデル設定のサービスがあるとき |
| `/settings/local-models` | ローカル・代替モデル（モデルマネージャーのカード・接続先のカード）。HTMX のときは本体だけ、HTMX でなければ分類のページ | `localmodels.Service` があるとき |
| `/settings/local-models/endpoints/:name` | 接続先のサブページ（接続先のカードと、そのモデルの表。モデルの追加・編集のフォームはこのページの中に開く）。タイトルは「設定: ローカル・代替モデル: <名前>」、ナビの現在の分類は「ローカル・代替モデル」。無い接続先は 404。名前が `new` の接続先（古い設定ファイルにだけある）は、追加フォームの URL に隠れるのでページが無い（一覧のカードがそう書く） | 同上 |
| `/settings/mcp` | MCP サーバー（今の区画をそのまま） | `app` が MCP サーバーを起動したとき |
| `/settings/update` | バージョンとアップデート（今の区画をそのまま） | 常に |
| `/settings/chatgpt` | HTMX: 今の区画の断片（`#chatgpt-panel`。クエリ `method` は読まない。ログイン中・ダウンロード中は区画が 2 秒ごとにこの URL を取り直す）。HTMX でなければ `/settings/api-keys` のページ全体で、`<link rel="canonical" href="/settings/api-keys">` を付ける（`/settings/chatgpt/models` も同じ） | ChatGPT のサービスがあるとき |
| `/settings/local-models/health` | 概要の「ローカル・代替モデル」のカード用: 接続先ごとの接続状態の断片（接続先ごとに接続テスト `localmodels.Service.TestEndpoint` と同じ確認を同時に行い、各 3 秒で打ち切る）。常に 200（接続先が無ければ空）、`Cache-Control: no-store` | `localmodels.Service` があるとき |

- ページのタイトルは概要が「設定」、分類が「設定: <分類名>」。ページ全体は `<nav aria-label="設定">`（現在の分類に `aria-current="page"`）と、分類の `<section id="settings-category">`（`h2#settings-category-title`）を持つ。分類を移るのは通常のリンク（`hx-boost` は使わない）。
- 分類の本文の区画は、API キーとログイン・画像・動画モデル（画像の用途の表・Quality Policy の表）・ローカル・代替モデル（「接続先」）・MCP サーバー（「トークン」）が `h3`（作り直した区画）、テキスト生成（LLM）は見出しの無い 1 つの区画（分類の `h2` が見出し）、ほかは今の区画のまま（見出しは分類の `h2` に統合され、区画の `h2` は出ない）。
- 概要のカードの「いまの状態」の文（例: 「設定済み 3 / 10」）は、サーバーが読める値だけで組み立てる。接続先の状態だけは `/settings/local-models/health` が後から差し込む（`hx-trigger="load"`。読めないときは空にする）。

## 2. モーダル

HTMX 専用の断片（`#mam-modal-body` に入る）。HTMX でない GET は 400 ではなく、該当する分類のページ全体を返す。

| Method | Path | 内容 |
|--------|------|------|
| GET | `/settings/credentials/:provider/edit` | API キーのモーダル。パスワード欄（`name=api_key`、`maxlength` は `service.MaxAPIKeyBytes`、`autofocus`、値は入れない）・「保存」・「キャンセル」、接続テストのあるプロバイダーは「接続テスト」も。未知の `provider` は 404（モーダルの断片）。キーストアが読めないときは理由と「閉じる」だけ |
| POST | `/settings/credentials/:provider/test` | API キーの接続テスト（§4）。フォーム `api_key`（空・空白だけなら保存済みのキー）。**保存しない**。接続テストの無いプロバイダーは 404。結果は `role="status"` の 1 行の断片（`#credential-test-result` に入る）。成功・キーが拒否された・通信できない・上限に達した・想定外の応答は、いずれも 200（要求自体は成功）。保存済みのキーが無く欄も空のときと、不正な入力は 422、保存済みのキーを読めないときは 503（どちらも同じ 1 行の断片）。キーは応答・ログ・URL のどこにも出さない。`Cache-Control: no-store` |
| GET | `/settings/llm/agents/:agent/edit` | LLM の Agent のモーダル（プライマリと代替の、プロバイダーの `<select>` とモデル名の `<input>`（`maxlength` は `llmassign.MaxModelBytes`）。既定を横に出す。「保存」・「この Agent を既定に戻す」・「キャンセル」）。未知の Agent は 404、固定（`--llm-config`）は 409（理由と「閉じる」だけ） |
| GET | `/settings/models/images/:purpose/edit` | 画像の用途のモーダル（主・代替のモデルの `<select>`、高速版を持つモデルがあるときだけ「高速版」）。開くたびに、いま選べるモデル（登録したローカルのモデルを含む）を読む。選べなくなった保存済みのモデルは「（現在は使えません）」つきで選択済みのまま。未知の用途は 404、固定・設定ファイルが読めないときは 409 |
| GET | `/settings/models/policies/:policy/edit` | Quality Policy のモーダル（主・代替の `<モデル>@<解像度>` の `<select>` とテイク数）。未知は 404、固定・設定ファイルが読めないときは 409 |
| GET | `/settings/chatgpt/:kind/login` | ログインのモーダル。`:kind` は `official` / `image` / `codex`（他は 404）。方式の選択（公式は「Continue with ChatGPT」のブラウザだけ、画像・Codex はブラウザかデバイスコード）。画像が OFF の `image` は 409、エンジンが無い `codex` は 409（理由の断片）。**`?poll=1`**: ログイン中のモーダルが 2 秒ごとに取り直す。ログイン中は進行の表示、終わっていたら成功で `HX-Trigger-After-Swap: modal-close` と区画（`#chatgpt-panel`）の OOB、失敗・取り消しは方式の選択に理由（失敗のとき）を添えて返す |
| GET | `/settings/local-models/endpoints/new`・`…/endpoints/:name/edit` | 接続先の追加・編集のモーダル。追加は名前・種類・`base_url`・同時実行数・タイムアウト・API キー（任意）と「入力中の内容で接続テスト」（`POST …/endpoints/test`。保存しない）。編集は `base_url`・同時実行数・タイムアウトだけ（名前と種類は文字）。**`?page=1`**: 接続先のサブページで開いたとき、保存はそのページの概要のカードを差し替える。固定ファイル（`--model-config`）は 409（理由の断片）、無い接続先の編集は 404 |
| GET | `/settings/mcp/bind/edit` | MCP の待ち受け先のモーダル（候補つきの 1 欄。保存済みのアドレスが入る） |
| GET | `/settings/mcp/tokens/new` | MCP トークンの発行のモーダル（名前の欄 1 つ） |
| GET | `/settings/chatgpt/models`・`/settings/chatgpt/codex/models` | 利用できるモデルの一覧のモーダル（読み取りだけ）。読めないときはその理由 |

## 3. 保存・操作の応答の決まり（モーダルを使う操作）

| 結果 | 状態 | ヘッダー | 本文 |
|------|------|---------|------|
| 成功 | 200 | `HX-Trigger-After-Swap: modal-close`、`Cache-Control: no-store`（キーの保存・ログインの開始と終了） | 一覧の行の OOB（`<li id="…" hx-swap-oob="true">`）と、成功のトースト（`atoms.SuccessToastOOB`。`role="status"`、閉じるまで残る）。モーダルの本文は空 |
| 入力の誤り | 422 | なし | モーダルの断片（`#mam-modal-body` に入り直す）。理由は `<p id="mam-modal-error" role="alert" tabindex="-1">`、問題の欄に `aria-invalid="true"`・`aria-describedby`・`autofocus`。**秘密の値は描き戻さない**（キーの欄は空に戻る） |
| 条件を満たさない（ChatGPT の画像が OFF・エンジン無し・登録できない） | 409 | なし | モーダルの断片（理由と「閉じる」） |
| 対象が無い | 404 | なし | モーダルの断片（「対象が見つかりません」と「閉じる」）。一覧は変えない |
| 保存先の失敗 | 503 | なし | モーダルの断片（保存できなかった理由。保存済みのキーは変わらない） |
| 通信断・非 HTML の失敗 | — | — | モーダルの中の `#mam-modal-failure`（`htmx:responseError` / `sendError` / `timeout`。`static/src/components/modal/`） |

- `main.ts` の `swapsErrorResponse` が HTML の 4xx/5xx を swap するので、422・409・404・503 の本文がそのまま `#mam-modal-body` に入る。
- `HX-Trigger-After-Swap` のイベントは、リクエストを出した要素がモーダルの本文の空によって DOM から消えるため、htmx 2.0.11 が `<body>` に起こし、`document` で聞く（2026-10-25 に `htmx.js` で確認）。
- OOB の行は `hx-swap-oob="true"` なので、行の `id` が一覧に無いと swap されない。行の `id` は、API キーが `credential-row-<provider>`（一覧 `credential-list`）、ChatGPT が `chatgpt-card-<kind>`（一覧 `chatgpt-cards`）。外部由来の値は `selector.ID` を通す。
- キーストアが保存のあとで読めなくなったときだけ、行の代わりに区画全体（`#credential-panel`、`HX-Retarget` + `HX-Reswap: outerHTML`）を「キーストアの状態を確認できません」つきで返す（保存は成功しているので、成功を失敗に見せない）。

### 表の行の保存（LLM・画像・動画モデル）

| Method | Path | 内容 |
|--------|------|------|
| PUT | `/settings/llm/agents/:agent` | 1 つの Agent の保存。フォーム `primary.provider`・`primary.model`・`fallback.provider`・`fallback.model`。**保存済みの他の Agent と合わせて**検証して書く（`llmassign.Service.Save` に 1 件だけの `map`）。成功は 200、行（`<tr id="llm-row-<agent>" hx-swap-oob="true">`。**応答の先頭**。HTML のパーサーは先頭の `<tr>` だけを行として読む）・成功のトースト・`HX-Trigger-After-Swap: modal-close`。行には `再起動で反映` の印（保存した直後の行だけ）。422 は `InvalidError.Message`（例「Writer（脚本） のプライマリ: モデル名を入力してください」）をモーダルの `role="alert"` に出し、入力を残し、問題の欄に `aria-invalid` と `autofocus`。固定は 409、未知の Agent は 404、書き込みの失敗は 500 |
| DELETE | `/settings/llm/agents/:agent` | その Agent を既定に戻す（`Save` に既定の route を渡す。既定と同じ route は設定ファイルから消える）。`hx-confirm` は付けない（モーダルの中の操作で、いつでもやり直せる。オーナー決定）。応答は PUT と同じ |
| PUT | `/settings/models/images/:purpose` | 1 つの画像の用途の保存。フォームは全体の保存と同じ欄の名前（`image_<用途>_<primary\|fallback>_model`・`…_fast`）。**その行だけ**検査して書く（`modelsettings.Service.SaveImage`。他の行は保存済みのまま。他の行が年齢確認で使えないモデルを指していても、この行の保存は止まらない）。成功は 200、行（`media-image-<purpose>`）・`#model-status`（注意とすべて既定に戻すの置き場。保存で変わるので同じ応答で OOB）・トースト・`modal-close`。**すぐに反映される**（再起動は要らない）。422・409・404・500 は上と同じ形（422 は選択を残す） |
| PUT | `/settings/models/policies/:policy` | 1 つの Quality Policy の保存（`policy_<…>_<primary\|fallback>`・`policy_<…>_takes`。`SavePolicy`）。応答は上と同じ（行は `media-policy-<policy>`） |

- 行の `id` と一覧: LLM は `llm-row-<agent>`（表 `llm-table`、折りたたみの表 `llm-unused-table`）、画像・動画は `media-image-<purpose>`（`media-images`）・`media-policy-<policy>`（`media-policies`）。開くボタンの `data-focus-key` は `edit-<agent>`・`edit-image-<purpose>`・`edit-policy-<policy>`（行が差し替わっても同じ）。
- 全体の保存・リセット（`PUT` / `DELETE /settings/llm`・`/settings/models`）は MCP のツールとテストのために残す。応答は分類の本文（LLM は `#llm-assignment-panel`、画像・動画は `#model-assignment`。`hx-swap="outerHTML"`）で、画面の「すべて既定に戻す」（`hx-confirm` つき）がこの `DELETE` を呼ぶ。
- 「画像・動画モデル」の選択肢は、ローカルのモデルを足す・消す応答には添えない（`#model-assignment` の OOB は外した。モーダルを開くたびに最新を読む）。

### 今のルートに起きた変更（パス・フォーム・状態コードは変えない）

| Method | Path | 変更 |
|--------|------|------|
| PUT | `/credentials/:provider` | 成功の本文が、区画ではなく**カードの OOB ＋成功のトースト＋ `HX-Trigger-After-Swap: modal-close`**。空・空白入り・長すぎるキーは 422（モーダルの断片）、キーストアの拒否は 503（同）、未知の `provider` は 422（赤いトースト）。キーは返さない |
| DELETE | `/credentials/:provider` | 成功の本文が**カードだけ**（`hx-swap="outerHTML"`）＋成功のトースト。未設定でも 200。未知の `provider` は 422（トースト）、キーストアの拒否は 503（トースト） |
| POST | `/settings/chatgpt/:kind/login` | 本文が**モーダルの断片**（ブラウザ: 開いたことと認可 URL、デバイスコード: 確認 URL・コード・期限、どちらも 2 秒ごとの取り直しつき）＋区画の OOB。公式のデバイスコードは 422、画像が OFF は 409、エンジンが無い `codex` は 409（モーダルの断片に理由） |
| POST | `/settings/chatgpt/:kind/login/cancel` | ログインの中止。モーダルを閉じ（`HX-Trigger-After-Swap: modal-close`）、カードを OOB で差し替える |
| DELETE・PUT・POST | `/settings/chatgpt/…`（ログアウト・画像の ON/OFF・Codex の取得・中断・パス・更新） | 本文は今のまま区画（`#chatgpt-panel`、`hx-swap="outerHTML"`）。区画の中身がカードの一覧になった |
| GET | `/settings/local-models` | HTMX でなければ分類のページ全体、HTMX は本体だけ（上の §1）。今の断片（`#local-models-panel` の区画）はもう返さない |

## 4. API キーの接続テスト

`service.CredentialService.TestConnection` → `internal/credential/probe`。無料で読むだけの GET が公式ドキュメントにあるプロバイダーだけ（決定事項 5(a)）。外部への通信は 10 秒で打ち切り、別のホストへのリダイレクトは追わない（`httpclient.New`）。キーはヘッダーにだけ入れ、URL・ログ・応答には出さない。

| provider | 呼び出し | 公式資料の確認（2026-10-25） | 実キー |
|----------|---------|------------------------------|--------|
| `anthropic` | `GET https://api.anthropic.com/v1/models?limit=1`（`x-api-key`・`anthropic-version: 2023-06-01`） | モデル一覧 API の記載を確認。ヘッダーはアダプター（`agent/modeladapter`）と同じ | 未検証 |
| `openai` | `GET https://api.openai.com/v1/models`（`Authorization: Bearer`） | 記載を確認 | 未検証 |
| `google` | `GET https://generativelanguage.googleapis.com/v1beta/models?pageSize=1`（`x-goog-api-key`。キーが不正なときの 400 も「拒否」） | 記載を確認 | 未検証 |
| `flux` | `GET https://api.bfl.ai/v1/credits`（`x-key`。残高を読むだけ） | `docs.bfl.ml` の OpenAPI に記載 | 未検証 |
| `runway` | `GET https://api.dev.runwayml.com/v1/organization`（`Authorization: Bearer`・`X-Runway-Version: 2024-11-06`） | `api.md` に記載（組織の残高と階層） | 未検証 |
| `elevenlabs` | `GET https://api.elevenlabs.io/v1/user/subscription`（`xi-api-key`） | 認証ヘッダー `xi-api-key` は公式の認証の頁で確認。このエンドポイントは記載を確認 | 未検証 |
| `kling`・`kling4`・`seedance`・`minimax`・`alibaba`・`luma`・`synclabs`・`typesafe` | なし | 読むだけの無料の API を公式資料で確認できなかった。ボタンは出さず「保存したあと、最初の生成で確かめられます」を出す | — |

結果の分類: 2xx = 受け付けられた、401/403 = キーが拒否された、429 = 上限に達した、通信の失敗・タイムアウト = 通信できない、そのほか = 想定外の応答（ステータスコードを添える）。

## 5. 今のルートのまま残すもの

§3・§5.1 のとおり、LLM・画像動画（②-3・②-4）とローカル・MCP（②-5・②-6）の保存の応答は行の OOB に作り直した。全体の保存・リセット（`PUT`・`DELETE /settings/llm`・`/settings/models`）は今のルートのまま（§6）。

## 5.1 ローカル・代替モデルと MCP サーバーの保存の応答（②-5・②-6）

§3 の決まり（成功は行の OOB ＋トースト、モーダルの閉じる合図。誤りは 422 でモーダルの断片）に従う。パス・フォーム・状態コードは [endpoints-local-models.md](endpoints-local-models.md) §2 と [endpoints.md](endpoints.md) のとおり。応答の形だけが変わった。

| 操作 | 成功の本文 | 失敗 |
|------|-----------|------|
| `POST …/local-models/endpoints`（追加） | 新しいカードを `<ul hx-swap-oob="beforeend:#local-endpoints">` で足し、「接続先はまだ…」の文（`#local-endpoints-empty`）を `hx-swap-oob="delete"` で外す。モーダルを閉じる。API キーの保存だけが失敗したときは、接続先は追加したので、エラーのトースト（理由）とその状態コードで、カードは足す | 名前の重複は 409、入力の誤り（名前 `new`・リンクローカルのアドレス・数字でない値）は 422。どちらもモーダルの断片（理由と、キー以外の入力値）。固定ファイルは 409（理由だけ） |
| `PUT …/endpoints/:name`（変更） | カード（`#local-endpoint-<名前>`）を OOB で置き換える。フォームの `page=1` のとき（サブページで開いたモーダル）は、サブページの概要のカード（削除ボタンとモデルへのリンクが無い形）に置き換える | 422・409・404 は同上 |
| `DELETE …/endpoints/:name` | 残りのカードを一覧（`#local-endpoints`）に `innerHTML` で入れる（押したボタンが消えても、フォーカスは次のカードの同じボタンに戻る）。最後の 1 件なら「接続先はまだ…」の文を `beforebegin:#local-endpoints` で足す。トースト | 使用中のモデルがあると 422、固定は 409、無いと 404。いずれもトーストだけ（`HX-Reswap: none`）で、カードは残る |
| `POST`・`PUT …/endpoints/:name/models(/:model)` | サブページのエディタ（`#local-models-editor`）を空にし、モデルの行（`#local-model-<接続先>-<モデル>`）を OOB で置き換える（追加は表 `#local-models-table` に足し、「モデルはまだ…」の行を外す）。概要のカードも OOB で件数を直す。トースト。**`#model-assignment` は返さない**（「画像・動画モデル」は別のページで、開くたびに最新のモデルを選択肢にする） | 422・409 は、エディタの中にフォームと理由。404・固定は理由だけ |
| `DELETE …/models/:model` | 残りの行を表に `innerHTML` で入れる（先頭に置く。表の行は先頭でないと HTML として読まれない）。概要のカードを OOB で直す。トースト | 使用中は 422（トースト） |
| `GET …/endpoints/:name/models/new`・`…/:model/edit`・`POST …/models/start` | エディタの中のフォーム（ComfyUI は 2 段階）。固定ファイルは 409。HTMX でない `GET` は、接続先のサブページ全体 | |
| `PUT /settings/mcp/bind` | 待ち受け先のカード（`#mcp-bind-row`）を OOB で置き換え、トースト、モーダルを閉じる | 許されないアドレスは 422、開けない・保存できないは 500。モーダルの断片（理由と入力値） |
| `POST /settings/mcp/tokens` | **201**。モーダルの本文を「発行したトークン」に替える（`<input readonly>` の値・コピーのボタン・「閉じると二度と表示できません」）。表に新しい行（先頭 8 文字だけ）を足し、「発行済みのトークンはありません」の行を外す。**`HX-Trigger-After-Swap` は付けない**（モーダルを閉じない）。`Cache-Control: no-store`。値はこの応答の欄に 1 回だけ。モーダルを閉じると本文が空になる | 名前が空・長すぎるは 422（モーダルに理由と入力値）。値は出ない |
| `DELETE /settings/mcp/tokens/:id` | そのトークンの行を OOB ではなくそのまま（押したボタンのある行を `outerHTML` で）「失効済み」にして返す。トースト | 無いトークンは 404（トースト） |

行（`<tr>`）と表の本体（`<tbody>`）を OOB で足す・置き換えるときは、`<template>` に包む（HTML の構文解析は、`<tr>` を表の外で読むと捨てるため。htmx は `<template>` の中の OOB を読む）。`<ul>` に足すときは `<ul hx-swap-oob="beforeend:#…">` の子として、置き換えでない OOB は要素の**中身**が入ることに注意する。

## 6. 区画の保存の規則（LLM の割り当て・画像・動画モデルの全体の保存と、アップデート）。LLM・画像動画の画面は §3 の「表の行の保存」の表とモーダルに作り直した

旧 `GET /settings` の行（`endpoints.md` §1。Issue #847 で本文をここへ移した）から。

- 「API キー」は §2・§3・§4 のとおり作り直した（カード＋モーダル）。キーの保存・削除は今のパスのまま `PUT` / `DELETE /credentials/:provider`。キーストアが読めないときはその区画だけが「利用できません」になり、画面自体は 200 で返る。
- 「LLM の割り当て」は Agent ごとの主・代替の Provider / モデル、「画像・動画モデル」は画像の用途（`new_reference` / `edit_reference` / `preview` / `unified_openai`）ごとの主・代替モデルと「高速版」、Quality Policy ごとの主・代替の動画モデル（そのモデルが対応する解像度との組み合わせで選ぶ）とテイク数。どちらも保存済みの割り当て（データディレクトリの `llm.json` / `models.json`。無ければ既定値）を表示し、実行中のものではない。`--llm-config` / `--model-config`（`MAM_LLM_CONFIG` / `MAM_MODEL_CONFIG`）で固定されている区画は読み取り専用で、その旨を表示する。設定ファイルが読めない区画は、その理由だけを表示する（画面は 200）。
- 年齢確認がオフの間は、導入済みの成人向けモデルを画像・動画モデルの選択肢に出さず、それを指す保存済みの割り当ては「（現在は使えません）」を付けて選択済みのまま見せ、区画の上に注意（`model-assignment-gated`）を出す。保存は、その割り当てが残っていれば 422（[../architecture/model-manager/model-manager.md](../architecture/model-manager/model-manager.md) §6.1。Issue #848）。
- 「バージョンとアップデート」（`updatepanel`）は実行中の版（開発ビルドは「開発版」）と、最後の確認の時刻・結果（最新・新版あり（取得中）・作業が動いているため待機中・エラーの種類・リリースなし・開発版）を表示し、自動更新に対応する Wails 版では「今すぐ確認」ボタンも出す（ヘッドレスの `cmd/server` は版だけ）。
- ランディングページ（`GET /`）の「設定」カードと `GET /projects/:id/settings` のリンクから開き、「← プロジェクト一覧に戻る」で戻る。キーの保存・削除は `PUT` / `DELETE /credentials/:provider`。LLM の割り当ては `PUT` / `DELETE /settings/llm`（Form `<agent>.<primary\|fallback>.<provider\|model>`。14 Agent すべて）、画像・動画モデルは `PUT` / `DELETE /settings/models`（Form `image_<用途>_<primary\|fallback>_model`・`image_<用途>_<primary\|fallback>_fast`・`policy_<fast\|balanced\|max_quality>_<primary\|fallback>`（値は `<モデル>@<解像度>`）・`policy_<…>_takes`）。PUT は既定と違う割り当てだけを設定ファイルへ書き（既定と同じものは書かないので、後で既定が変わればそれに従う。ほかのキー — `providers`・`retry`・`models`・`routing` など — は手書きのままそのまま残し、残るものが無ければファイルを消す）、DELETE はその区画の上書きだけを消す。書く前に起動時と同じ検査（`llm.LoadConfig` / `configcheck.Load`）を通し、画像・動画モデルは、そのモデルが対応しない解像度・動画モデルでないモデル・テイク数 0 以下・高速版を持たないモデルの「高速版」を 422 で断る。応答はどちらも区画（`#model-assignment` など）の HTML で、200 は、LLM が「保存しました。反映にはアプリの再起動が必要です」（LLM のクライアントは起動時に 1 度だけ作るため、反映は再起動後）、画像・動画モデルが「保存しました。すぐに反映されます」（保存で Model Adapter を作り直す。#831）を添える（DELETE は「既定に戻しました。…」）。422 は理由と入力値付きで区画を返しファイルは変えない。固定された区画は 409、書き込みの失敗は 500。画像の用途で参照画像を使えないモデル（`modeladapter.AcceptsImageInputs` が偽。組み込みにはなく、参照画像を受け取れない登録モデルだけ）を選ぶと、そのモデルで作るときは参照画像を渡さず Prompt だけから作る（ログに警告。画面はその旨の注意を出す）。保存済みの `models.json` が終了した Imagen 4（`imagen-4`）を指していれば、読み込み時に Nano Banana（高速版は Nano Banana Lite）として表示・検査する（`spec/architecture/model-adapters.md` §6）

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成。分類ごとのページ、API キーとログインのモーダル・接続テスト、モーダルの保存の応答の決まりを書いた | Issue #847。`endpoints.md` の行数上限（300 行）に収めるため、設定画面の分類のルートを別ファイルにした |
| 1.1 | 2026-10-25 | §6 に、年齢確認がオフの間の成人向けモデルの扱い（選択肢から外す・保存済みの割り当ては「（現在は使えません）」・保存は 422）を足した | Issue #848 が `endpoints.md` §1 の `GET /settings` の行に足した規則を、#847 で行をここへ移したのに合わせて移した |
| 1.2 | 2026-10-25 | LLM・画像動画の分類を表とモーダルにした。`GET /settings/llm/agents/:agent/edit`・`/settings/models/images/:purpose/edit`・`…/policies/:policy/edit`、`PUT`・`DELETE /settings/llm/agents/:agent`、`PUT /settings/models/images/:purpose`・`…/policies/:policy` を足した（§2・§3）。全体の保存・リセットは残し、応答を分類の本文にした。ローカルのモデルの変更の応答は `#model-assignment` を添えなくなった | Issue #847 段階 ②-3・②-4 |
| 1.3 | 2026-10-25 | §1 に接続先のサブページ（`GET /settings/local-models/endpoints/:name`）、§2 に接続先・MCP のモーダル（`GET …/endpoints/new`・`…/:name/edit`・`/settings/mcp/bind/edit`・`/settings/mcp/tokens/new`）、§5.1 にローカル・代替モデルと MCP の保存の応答（行の OOB、`#model-assignment` の OOB の廃止、トークンの発行の 201）を書いた。§5 の「実装していないもの」を「今のルートのまま残すもの」に改めた | Issue #847 ②-5・②-6 |
| 1.4 | 2026-10-25 | §1 の概要の接続先の状態を、読めないとき空にする（実装どおり）に直した。§6 のランディングページの入口の名前を、カードの現在の名前「設定」に直した | PR #849 の第 2 周の独立レビューの指摘 |
