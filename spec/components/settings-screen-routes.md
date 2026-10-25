# 画面仕様: 設定画面の新しいルートと応答の決まり（下書き）

> **状態: 段階 ①・②-1・②-2・②-3・②-4 のぶんは実装済み**（`GET /settings`・`/settings/<slug>`・`/settings/local-models/health`・`GET /settings/credentials/:provider/edit`・`POST /settings/credentials/:provider/test`・`GET /settings/chatgpt/:kind/login`、`PUT`・`DELETE /credentials/:provider` の応答。実際の契約は [../api/endpoints-settings.md](../api/endpoints-settings.md)）。LLM（`GET`・`PUT`・`DELETE …/llm/agents/:agent` ほか）と画像・動画（`GET …/models/images/:purpose/edit`・`…/policies/:policy/edit`、`PUT …/images/:purpose`・`…/policies/:policy`）のルートも実装済み（②-3・②-4）。ローカル・MCP のルートは未実装（②-5 以降）。Issue #847。オーナーは §6 を推奨する既定で決定した（2026-10-25）。実装のとき、§1 の表を `spec/api/endpoints-settings.md`（新規。`endpoints.md` が上限に近いため。§4）と [../api/endpoints-local-models.md](../api/endpoints-local-models.md) に移し、`spec/api/endpoints.md` §1 の `/settings` の行は「概要」にして `endpoints-settings.md` への案内を足す。ここでは、その下書きと、モーダルの保存の応答の決まりを書く。画面は [settings-screen.md](settings-screen.md)・[settings-screen-categories.md](settings-screen-categories.md)、部品は [shared-ui-parts.md](shared-ui-parts.md)。

## 1. 新しいルート（すべて今のルートに足す。今のルートの意味は変えない）

GET は状態を変えない。PUT・POST・DELETE は CSRF 必須。`:provider`・`:agent`・`:purpose`・`:policy`・`:name` は今のルートと同じ検査（未知は 404 か 422 は各行のとおり）。

### ページ（HTMX でないときはページ全体、HTMX のときは本体だけ。settings-screen.md §3）

| Method | Path | 内容 | 出す条件（無ければルートごと無い＝404） |
|--------|------|------|----------------------------------------|
| GET | `/settings` | **概要**（変更）。今は全区画の縦並び | 常に |
| GET | `/settings/api-keys` | API キーとログイン | 常に |
| GET | `/settings/llm` | テキスト生成（LLM） | `Services` あり |
| GET | `/settings/models` | 画像・動画モデル | 同上 |
| GET | `/settings/local-models` | ローカル・代替モデル（**変更**: 今は HTMX の区画だけ。HTMX でなければページ全体） | `Services.LocalModels` あり |
| GET | `/settings/local-models/endpoints/:name` | 接続先のサブページ（モデルの表・追加）。無ければ 404 | 同上 |
| GET | `/settings/mcp` | MCP サーバー | `Services.MCP` あり |
| GET | `/settings/update` | バージョンとアップデート | 常に |
| GET | `/settings/chatgpt` | **変更**: HTMX でなければ `/settings/api-keys` のページ全体（`canonical` つき）。HTMX は今どおり区画の断片（今のクエリ `method` = `official` / `codex` を保つ。ログインのカードごとの取り直しに `card=<kind>` を足すかは §6 の質問 5） | `Services.ChatGPT` あり |
| GET | `/settings/local-models/health` | 概要のカード用: 接続先の接続状態の断片（各 3 秒で打ち切り。空の 200 もある） | `Services.LocalModels` あり |

### モーダルの本文（断片。HTMX 専用。HTMX でない GET は 400 ではなく、該当する分類のページ全体を返す）

| Method | Path | 内容 |
|--------|------|------|
| GET | `/settings/credentials/:provider/edit` | API キーのモーダル。未知の `provider` は 404 |
| GET | `/settings/llm/agents/:agent/edit` | LLM の Agent ごとのモーダル。未知は 404。固定（`Pinned`）は 409（理由の断片） |
| GET | `/settings/models/images/:purpose/edit` | 画像の用途のモーダル。固定は 409 |
| GET | `/settings/models/policies/:policy/edit` | 動画の Quality Policy のモーダル。固定は 409 |
| GET | `/settings/mcp/bind/edit` | 待ち受け先のモーダル |
| GET | `/settings/mcp/tokens/new` | トークン発行のモーダル |
| GET | `/settings/local-models/endpoints/new`・`…/:name/edit` | **変更**: 今は `#local-models-editor` に差し込む断片。モーダルの本文になる（応答の形は変えるが、パスと意味は同じ） |
| GET | `/settings/chatgpt/:kind/login` | ログインのモーダル（方式の選択）。`:kind` は `official` / `image` / `codex`（#845） |

### 保存・操作（新規）

| Method | Path | 内容 |
|--------|------|------|
| PUT | `/settings/llm/agents/:agent` | **新規（決定事項 9(a)）**。1 つの Agent の保存。フォーム `primary.provider`・`primary.model`・`fallback.provider`・`fallback.model`。サーバーが、**保存済みの他の Agent と合わせて検証して**書く（既存の `llmassign.Service.Save` に 1 件だけの `map` を渡す。`routes` に無い Agent は保存済みのまま。`service/llmassign/llmassign.go`）。固定は 409。応答は §2。今の `PUT /settings/llm`（全体）は残す |
| DELETE | `/settings/llm/agents/:agent` | **新規**。その Agent を既定に戻す（`Save` に既定の route を渡す。既定と同じ route は設定ファイルから消える）。固定は 409。応答は §2 |
| PUT | `/settings/models/images/:purpose`・`/settings/models/policies/:policy` | **新規（案。categories §7 の質問 4）**。1 行の保存。内容は今の `PUT /settings/models` の 1 行ぶんで、保存の検証は `modelsettings.Service` が行う。固定は 409 |
| POST | `/settings/credentials/:provider/test` | **新規**。API キーの接続テスト（接続テストのあるプロバイダーだけ。無ければ 404）。フォーム `api_key`（空なら保存済みのキー）。結果の行だけ返す（成功・キーが拒否された・通信できない・上限に達した。いずれも 200。キーそのものは**応答にもログにも出さない**）。保存しない。外部への通信は 10 秒で打ち切る |
| PUT・DELETE | `/credentials/:provider` | **変更なし**（パス・フォーム・状態コードは今のまま）。応答の本文だけ §2 の形（OOB の行＋ `HX-Trigger-After-Swap`）に変える |
| PUT | `/settings/mcp/bind`・POST `/settings/mcp/tokens`・DELETE `/settings/mcp/tokens/:id` | パス・フォームは今のまま。応答は §2。**発行（201）だけは例外**（§3） |
| POST・PUT・DELETE | `/settings/local-models/endpoints/…`（`endpoints-local-models.md` §2） | パス・フォームは今のまま。応答は、保存した行（接続先のカード）の OOB ＋ `HX-Trigger-After-Swap: modal-close`。モデルの変更はサブページの表の行を OOB で差し替える |
| GET | `/tools/:tool/status` | [home-screen.md](home-screen.md) §5。概要の「モデルマネージャー」のカードも使う（`:tool` は `model-manager` など） |

## 2. モーダルの保存の応答の決まり（全分類共通）

| 結果 | 状態 | ヘッダー | 本文 |
|------|------|---------|------|
| 成功 | 200（作成は 201。発行のトークンだけ §3） | `HX-Trigger-After-Swap: modal-close`。`Cache-Control: no-store`（秘密を含む応答、トークンの発行、API キーの保存） | **一覧の行の OOB だけ**: `<li id="<種類>-row-<キー>" hx-swap-oob="true">…</li>`（新しい行は `hx-swap-oob="beforeend:#<一覧の id>"`）。モーダルの本文には空（`hx-target="#mam-modal-body"`・`hx-swap="innerHTML"`）。成功の通知（`role="status"`）は行の中の短い文か、`atoms.ToastOOB`（モーダルが閉じたあとで見えるので使える） |
| 入力の誤り | **422** | なし | モーダルの断片（`#mam-modal-body` に入り直す）。理由は `<p id="mam-modal-error" role="alert" tabindex="-1">`、入力は描き戻す（**秘密の値は描き戻さない**）、問題の欄に `aria-invalid="true"`・`aria-describedby`・`autofocus` |
| 固定ファイル・競合 | **409** | なし | モーダルの断片（理由と「閉じる」だけ。保存ボタンは出さない） |
| 対象が無い | **404** | なし | モーダルの断片（「対象が見つかりません」と「閉じる」）。一覧は変えない |
| 書き込みの失敗 | **500**（キーストアの失敗は **503**） | なし | モーダルの断片（今の文: 「設定ファイルを書き込めませんでした。保存先の権限と空き容量を確認してください」など） |
| 通信断・非 HTML の失敗 | — | — | `htmx:responseError` / `sendError` / `timeout` を `modal/` が聞き、モーダルの中の `#mam-modal-failure` に出す（shared-ui-parts §4） |

- `main.ts` の `swapsErrorResponse` が HTML の 4xx/5xx を swap するので、422・409・404・500 の本文がそのまま `#mam-modal-body` に入る（今の仕組みを変えない）。
- 成功の OOB の行は**サーバーが保存したあとの値で組み立てる**（フォームの値ではない）。今は区画全体を返している応答（MCP・`localmodels`・`credentialpanel` など）は、モーダルから呼ぶルートでだけ行の OOB に変える。全体の保存（`PUT /settings/llm`・`PUT /settings/models`）は、MCP やテストのために今の応答（区画全体）のまま残す。
- `HX-Trigger-After-Swap` を使う根拠: モーダルのフォームは送信後に `hx-target` の内側が空になり、リクエストを出した要素が DOM から消えるので、htmx 2.0.11 はそのヘッダーのイベントを `<body>` に起こす（`htmx.js` の `afterSwapCallback`。2026-10-25 に確認）。`document` で聞けば足りる。
- OOB の行は `hx-swap-oob="true"` なので、**行の `id` が一覧に無い**と swap されない（新しい行は `beforeend:` を使う）。行の `id` は §5 の `<種類>-row-<キー>` で一意にする。

## 3. 例外: MCP トークンの発行（一度だけ表示）

- `POST /settings/mcp/tokens`（フォーム `name`）: 成功は **201**。本文はモーダルの断片を「発行したトークン」の表示に差し替える（モーダルは閉じない。`HX-Trigger-After-Swap` は付けない）。トークンの値は**この応答だけ**に入り、`Cache-Control: no-store`・`Pragma: no-cache`。表の新しい行（先頭 8 文字だけ）は同じ応答の OOB（`beforeend:#mcp-token-list`）。
- 利用者がモーダルを閉じる（`close`）と、本文を空にして値を DOM から消す（再表示の経路は無い）。履歴・サーバーのログ・OOB の行のどこにも値を残さない。名前が不正なら 422（モーダルの断片。名前を残す）。
- テストで確かめること: 応答にトークンが 1 回だけ現れる、表の行・`GET /settings/mcp` の再取得にトークンが現れない、`no-store`。

## 4. 変わる既存の応答と、取り込む仕様書（実装のとき）

| 仕様書 | 変更 |
|--------|------|
| `spec/api/endpoints.md` §1 | `/settings` の行を「概要」にし、`endpoints-settings.md` への案内を足す。**`endpoints.md` は 296 行で上限（300 行）まで 4 行**（2026-10-25 時点。#845 が 3 行足した）なので、新しいルートは `endpoints-settings.md`（新規）に書き、`endpoints.md` にはその一覧への 1 行の案内を足す（`endpoints-local-models.md` と同じやり方）。[home-screen.md](home-screen.md) の `GET /tools/:tool/status` の 1 行も `endpoints.md` に足すので、合わせて 298 行 |
| `spec/api/endpoints-local-models.md` | §1「画面」・§2 の表の GET の行（区画 → モーダル・サブページ・HTMX の分岐）、成功の応答（区画全体 → 行の OOB）、`#model-assignment` の OOB の削除 |
| `spec/components/overview.md` §3 | `global_settings_body`・`global_settings_page` の行を新しい作りに、`molecules` に `tool_card`・`settingsui`・`modal`、`atoms` に `icon` を足す（**この文書群への 1 行のポインターだけを今回足す**。本文の更新は実装のとき） |
| `spec/api/endpoints.changelog.md`・各仕様書の改訂履歴 | 追記 |

## 5. 行の `id` の規則（OOB の対象）

| 種類 | 行の `id`（案。既存を維持） | 一覧の `id` |
|------|---------------------------|------------|
| API キー | `credential-row-<provider>`（今のまま） | `credential-list` |
| ChatGPT のログイン | `chatgpt-card-<kind>` | `chatgpt-cards`（今の `#chatgpt-panel` を一覧にする） |
| LLM | `llm-row-<agent>` | `llm-table` |
| 画像・動画 | `media-image-<purpose>`・`media-policy-<policy>` | `media-images`・`media-policies` |
| ローカルの接続先 | `local-endpoint-<name>` | `local-endpoints` |
| ローカルのモデル | `local-model-<name>-<model>` | `local-models-table` |
| MCP | `mcp-bind-row`・`mcp-token-<id>` | `mcp-token-list` |

- 外部由来の `<name>`・`<id>` は `selector.ID`（`internal/web/selector`）でセレクタにする（手で連結しない。overview.md §3 の末尾）。

## 6. オーナーへの確認事項（このファイルの範囲。推奨する既定つき）

| # | 質問 | 推奨する既定 |
|---|------|-------------|
| 1 | 新しいルートの一覧を `endpoints.md` に直接足さず、`endpoints-settings.md`（新規）に書くか（`endpoints.md` が 296 行で、上限の 300 行まで 4 行） | `endpoints-settings.md` を新規に作る |
| 2 | HTMX でない `GET` をモーダルの本文のパスに送ったとき、400 にするか、該当分類のページ全体を返すか | ページ全体（直接開いても壊れない。URL をブックマークされても 404 にならない） |
| 3 | モーダルを使う保存の応答を、全体の保存（`PUT /settings/llm`・`/settings/models`）と分けて持つことでよいか | 分ける（全体の保存は MCP・テストのために今のまま。モーダルのルートだけ OOB の行を返す） |
| 4 | 成功の通知を、行の中の文にするか、トースト（`atoms.ToastOOB`）にするか | トースト（モーダルが閉じたあとに見える。行は要約だけにする） |
| 5 | ログインのモーダルがカードごとに取り直すとき、`GET /settings/chatgpt` に新しいクエリ `card=<kind>` を足すか、今の `method`（`official` / `codex`）を保つだけにするか、`method` を `kind` に置き換えるか。今の `method` は画像生成（`image`）を指せない | `card=<kind>` を足し、`method` は互換のために残す（今の区画が `method` で動いているため。置き換えは #845 の取り込みを壊す） |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 0.1 | 2026-10-25 | 新規作成（案） | Issue #847 決定事項 4(a)・8(a)・9(a)。新しいルートと、モーダルの保存の応答の下書き |
| 0.2 | 2026-10-25 | §1 のログインのモーダルの `:kind` に `codex` を足した | 統合の見直し。#845 が先に入ったため |
| 0.3 | 2026-10-25 | `endpoints.md` の行数を 296 行に直し、`GET /tools/:tool/status` との合計を書いた。`GET /settings/chatgpt` の `card` と今の `method` の関係を未決の質問（§6 の 5）にした | 独立レビュー（PR #849）の指摘 |
| 0.4 | 2026-10-25 | 状態を更新した。実装済みのルートは [../api/endpoints-settings.md](../api/endpoints-settings.md) に移した。成功の通知はトースト（§6 の質問 4 の推奨。`atoms.SuccessToastOOB`）。§6 の質問 5 は、ログインのモーダルが自分の `GET /settings/chatgpt/:kind/login?poll=1` を取り直し、パネル全体の取り直しは `GET /settings/chatgpt` のままで、`card` は足さず `method` は読まない形にした | Issue #847 の実装 |
| 0.5 | 2026-10-25 | 状態を更新した。LLM・画像動画のルートを実装した。画像・動画の行ごとの保存（§6 の質問 4）は推奨どおり足した。OOB の行は**応答の先頭**に置く（HTML のパーサーは先頭の `<tr>` だけを行として読む）。画像・動画の保存は `#model-status`（年齢確認の注意とすべて既定に戻す）も OOB で返す | Issue #847 段階 ②-3・②-4 の実装 |
