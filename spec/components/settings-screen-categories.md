# 画面仕様: 設定画面の分類ごとの一覧とモーダル

> **状態: §1（API キーとログイン）・§2（LLM）・§3（画像・動画）・§4（MCP）・§5（ローカル・代替モデル）は実装済み（2026-10-25）。** Issue #847 ステージ ②。オーナーは §7 を推奨する既定で決定した（2026-10-25）。枠・ナビ・URL・概要は [settings-screen.md](settings-screen.md)、ルートと応答の決まりは [settings-screen-routes.md](settings-screen-routes.md)、部品は [shared-ui-parts.md](shared-ui-parts.md)。`<dialog>` の決まり（開く・閉じる・フォーカス・成功・422）はすべて shared-ui-parts §4 に従い、ここでは繰り返さない。

方式の凡例（決定事項 8(a)）: **モーダル** = 1 つの対象の短い編集（項目が数個・1 回の保存）。**サブページ** = 一覧を持つもの・長い作業（URL を持つ）。削除の確認は今のまま `hx-confirm`。

## 1. API キーとログイン（`/settings/api-keys`）

### 1.1 API キー（10 プロバイダー。`service.CredentialProviders` の順）

方式: **カード（一覧）＋ 設定・変更はモーダル ＋ 削除は `hx-confirm`**。パスワード欄は出しっぱなしにしない。

カード（`SettingsCard`。行の `id` は今のまま `credential-row-<provider>`、`data-testid="credential-row"` `data-provider`）:

| 要素 | 内容 |
|------|------|
| 名前（`h3`） | `CredentialProviders` の `Label`（例: 「Kling」） |
| 用途（1 行） | 下表の「何に使うキーか」（`model-adapters.md` の記載から） |
| 状態 | `Badge`: 設定済み（ok）／未設定（neutral） |
| ボタン | 未設定: 「設定」。設定済み: 「変更」「削除」（`ButtonFor` で `aria-label` に名前） |

| provider | 何に使うキーか（案。`model-adapters.md` の節） | 接続テスト（実装済み。呼び出しの詳細は [endpoints-settings.md](../api/endpoints-settings.md) §4） | 公式資料の確認（2026-10-25）と実キー |
|----------|---------------------------------------------|----------------------------------------|------------|
| `anthropic` | テキスト生成（Claude。§2） | `GET https://api.anthropic.com/v1/models?limit=1`（`x-api-key`・`anthropic-version`） | モデル一覧 API の記載を確認。**実キーでは未検証** |
| `openai` | テキスト生成（GPT-5 系）と画像生成（GPT Image 2。§2・§3） | `GET https://api.openai.com/v1/models`（`Authorization: Bearer`） | 記載を確認。**実キーでは未検証** |
| `google` | テキスト生成（Gemini）と画像生成（Nano Banana。§2・§3） | `GET https://generativelanguage.googleapis.com/v1beta/models?pageSize=1`（`x-goog-api-key`） | 記載を確認。**実キーでは未検証** |
| `flux` | 画像生成（FLUX.2。§3） | `GET https://api.bfl.ai/v1/credits`（`x-key`。残高を読むだけ） | 公式の OpenAPI（`docs.bfl.ml`）に記載。**実キーでは未検証** |
| `kling` | 動画生成（Kling 3.0。§4） | なし（キーは `ACCESS_KEY:SECRET_KEY` で、JWT を作って呼ぶ。読むだけの無料の API を公式資料で確認できなかった） | 候補なし。「保存したあと、最初の生成で確かめられます」を出す |
| `seedance` | 動画生成（Seedance 2.0。§4） | なし（BytePlus ModelArk。読むだけの無料の API を公式資料で確認できなかった） | 候補なし。同上 |
| `runway` | 動画生成（Runway Gen-4.5。§4） | `GET https://api.dev.runwayml.com/v1/organization`（`Authorization: Bearer`・`X-Runway-Version`。組織の残高と階層を読むだけ） | 公式の `api.md` に記載。**実キーでは未検証**。`X-Runway-Version` の値は endpoints-settings.md §4 |
| `elevenlabs` | 音声（TTS・Music・SFX。§5） | `GET https://api.elevenlabs.io/v1/user/subscription`（`xi-api-key`。契約の状態を読むだけ） | 認証ヘッダーは公式の認証の頁、エンドポイントは記載を確認。**実キーでは未検証** |
| `synclabs` | Lip Sync（§5） | なし（読むだけの無料の API を公式資料で確認できなかった） | 候補なし。同上 |
| `typesafe` | Repair の前段選別（Jev。§4.5） | なし（`POST /v1/systemone` だけ。読むだけの無料の API を公式資料で確認できなかった） | 候補なし。同上 |

- 接続テストは、決定事項 5(a)（無料で読むだけの API があるプロバイダーだけ）に沿って付けた（上の 6 つ）。**実キーでの動作は所有者の実機で確かめる（未検証）**。公式資料で確認できなかった 4 つには接続テストのボタンを出さず、「保存したあと、最初の生成で確かめられます」を出す。
- 「キーの取り方のリンク」（各社のコンソール）: anthropic `https://platform.claude.com/settings/keys`、google `https://aistudio.google.com/apikey`、flux `https://dashboard.bfl.ai`、runway `https://dev.runway.com`（公式の setup の頁で確認）、elevenlabs `https://elevenlabs.io/app/api/api-keys`（公式のクイックスタートで確認）。そのほかのプロバイダーはリンクを出さない（公式の取得先を確認できていない）。リンクは `target="_blank" rel="noopener noreferrer"`。接続テストの呼び出しとその結果の分類は [../api/endpoints-settings.md](../api/endpoints-settings.md) §4。

### 1.2 API キーのモーダル（`GET /settings/credentials/:provider/edit`）

- 見出し `<h2 id="mam-modal-title">`: 「<Label> の API キー」。説明: 用途・キーの取り方のリンク（`target="_blank" rel="noopener noreferrer"`。外部リンクと分かる文言）・「キーは OS のセキュアストレージに保存され、画面には設定済みかどうかだけが出ます」。
- 欄: パスワード欄 1 つ（`type=password autocomplete=off`、`<label>` つき、`name=api_key`、`maxlength` は `service.MaxAPIKeyBytes`）。**値は画面にも応答にも出さない・描き戻さない**（422 でも空に戻る）。
- ボタン: 「接続テスト」（接続テストがあるプロバイダーだけ。`POST /settings/credentials/:provider/test`。結果は `role="status"` の 1 行（成功・キーが拒否された・通信できない・上限に達した）に出す。**保存はしない**。入力欄が空で設定済みなら、保存済みのキーで試す）、「保存」（`PUT /credentials/:provider`。今のルートのまま）、「キャンセル」。
- 保存の成功: 行を OOB で差し替え、モーダルを閉じる（`HX-Trigger-After-Swap: modal-close`）。閉じたあとのフォーカスは、押した「設定」「変更」のボタン（行が差し替わるので、同じ `data-focus-key="credential-<provider>"` のボタン）。
- 失敗: 空・空白入り・長すぎる → 422（今と同じ理由の文）、キーストアの失敗 → 503。どちらもモーダルの断片（`role="alert"`）。
- 削除: 設定済みの行の「削除」は今のとおり `hx-delete="/credentials/:provider" hx-confirm=…`、行を差し替えるだけ（モーダルは使わない）。
- キーストアが読めないとき（`Unavailable`）: 区画の先頭に今の `role="alert"` の文を出し、カードの「設定」「変更」は無効（`disabled`、`title` に理由）。

### 1.3 ChatGPT でログイン（方式ごとのカード）

方式: **カード ＋ ログインの途中はモーダル**。状態の取得とポーリングは今の `GET /settings/chatgpt`（HTMX の要求）を使う。

| カード | 状態の表示 | ボタン |
|--------|-----------|--------|
| Sign in with ChatGPT（公式。テキスト生成） | ログイン済み（メール・プラン）／未ログイン／再ログインが必要 | 「ログイン」／「ログアウト」／「利用できるモデルを表示」 |
| 画像生成（非公式・既定 OFF） | ON/OFF とログインの状態。ON にするときのリスクの確認は今のまま（`acknowledge_risks`） | 「ON にする」「ログイン」「ログアウト」 |
| Codex 経由（#845。**3 つ目**。実装済みの区画は `chatgptpanel/codex.templ`） | エンジンの状態（未取得・取得中・利用可・動作中・失敗）・`account/read` のメール・プラン・利用上限 | 「Codex をダウンロード」「中断」「パスを設定」「ブラウザでログイン」「デバイスコードでログイン」「状態と利用上限を更新」「ログアウト」「利用できるモデルを表示」（文言は今の区画のまま移す） |

- 「ログイン」はモーダルを開き（`GET /settings/chatgpt/:kind/login`。`:kind` は `official` / `image` / `codex`）、ブラウザ／デバイスコードの選択（フォーム `method` = `browser` / `device`）→ 開始 → 途中の表示（認可 URL・確認 URL・コード・期限）を**モーダルの中**に出す。ログイン中の 2 秒ごとの取り直し（`hx-trigger="every 2s"`）は、モーダルの本文が自分の URL（`GET /settings/chatgpt/:kind/login?poll=1`）を取り直す形。今の `GET /settings/chatgpt` のクエリ `method`（`official` / `codex`。#845）は取り直しに使わず、`card=<kind>` も足さない（[settings-screen-routes.md](settings-screen-routes.md) §6 の質問 5。決定済み）。完了・取り消しでモーダルを閉じ、カードの行を OOB で差し替える。
- 「利用できるモデルを表示」の結果は、小さなモーダル（読み取りだけ）に出す（今は区画の下にインラインで出す）。
- ログインのモーダルの詳細（デバイスコードの表示の文言・期限切れ・取り消し）は、今の `chatgptpanel` の文言を移す。変えない。
- **#845 との関係**: #845 は 3 つ目の区画と、その状態・ログインの処理を足した（実装済み）。このカードの枠・モーダルの部品・ルートの置き場（`organisms/settings/chatgpt/`）は #847 が作る。#845 の仕組み（取得・起動・`account/*`）は触らない。

## 2. テキスト生成（`/settings/llm`）

方式: **Agent ごとに 1 行の表 ＋ 「編集」はモーダル**。保存は Agent ごと（決定事項 9(a)）。

- 先頭: 「反映にはアプリの再起動が必要です」を**常に**出す（`role` なしの通常の段落）。全体の「既定に戻す」（`DELETE /settings/llm`。今のまま）を表の上に残す。固定（`Pinned`）のとき: 今の注意（`--llm-config` / `MAM_LLM_CONFIG`）を出し、表は読み取りだけで「編集」と「既定に戻す」は出さない。
- ChatGPT のプランを使うプロバイダー `chatgpt-subscription` の注意（今の `llm-chatgpt-note`）は表の上に残す。#845 の `chatgpt-codex`（実装済み）は、プロバイダーの `<select>` に並ぶだけ（`llm.Providers()` から）。

表（`SettingsTable`。行 `id="llm-row-<agent>"`）:

| 列 | 内容 |
|----|------|
| Agent（行見出し） | `labels` の名称（例: 「Writer（脚本）」） |
| 用途 | 1 行の説明（`labels` の括弧の中ではなく、`model-adapters.md` §2 の選定理由から短く。文言は §7 の質問 2） |
| プライマリ | 「anthropic / claude-…」（要約。折り返す） |
| 代替 | 同上 |
| 変更 | `Badge`: 「既定」（neutral）／「変更あり」（info）。既定の判定は `Route == Default` |
| 操作 | 「編集」（`SmallButtonFor`、`aria-label`「編集: Writer（脚本）」） |

- 使われていない Agent（`Unused`: Orchestrator・Final QC）は、今と同じく折りたたみ（`<details>`「現在は使われません」）に入れ、表の下に置く。折りたたみの中の表も同じ列・同じ編集。
- 14 行で、1440×900 では 1 画面に収まらず main がスクロールする。「用途」列は `xl:`（1280 px）以上でだけ出し、狭いときは行見出しの下に 1 行で出す（実装で変更: 当初は `lg:` の案だったが、サイドナビで表が約 700 px に狭まる 1024×700 の実画面で、`lg:` ではモデル名が割れて読みにくかった。2026-10-25 に両方を撮って比べた）。
- 実装（2026-10-25）: 全体の「既定に戻す」は「すべて既定に戻す」（`hx-confirm`。`DELETE /settings/llm`）。モーダルの 422 は `llmassign.InvalidError` の `Role`・`Field` で問題の欄に `aria-invalid`・`autofocus`。「再起動で反映」の印（`Badge` warn）は保存した直後の行だけ。『この Agent を既定に戻す』の確認なしは `destructive_confirm_test` の例外に理由つきで足した。用途の 1 行（§7 の質問 2）は `model-adapters.md` §2 の選定理由から要約した（`handler/settings/llm/purposes.go`）。`PUT /settings/llm`（全体）は MCP のツールとテストのために残し、応答は表の区画（`#llm-assignment-panel`）。

モーダル（`GET /settings/llm/agents/:agent/edit`。`:agent` は `llmassign.Agents()` の名前。未知は 404）:

- 見出し: 「<Agent 名> の LLM」。説明に用途と「反映にはアプリの再起動が必要です」。
- 欄: プライマリ（プロバイダーの `<select>`＋モデル名の `<input>`）、代替（同上）。**既定の値を横に出す**（「既定: anthropic / claude-…」。`text-neutral-400`）。モデル名の自由入力は残し、分かる範囲で候補を出す（`<datalist>`。ChatGPT のログインのモデル一覧（`GET /settings/chatgpt/models`）や #845 の `model/list`。**プロバイダーごとにモデルの一覧を取れるかは未検証**。候補が無いプロバイダーは自由入力だけ）。モデル名の長さは `MaxModelLength`。
- ボタン: 「保存」（`PUT /settings/llm/agents/:agent`）、「この Agent を既定に戻す」（`DELETE /settings/llm/agents/:agent`。`hx-confirm` なし。モーダルの中の操作で、いつでもやり直せるため。§7 の質問 3）、「キャンセル」。
- 成功: 行を OOB で差し替え、閉じる。**行の要約が新しい値になる**（「保存済み・再起動で反映」の印を行に出す: `Badge`「再起動で反映」（warn））。422: 理由（`InvalidError.Message`。例「Writer（脚本） のプライマリ: モデル名を入力してください」）と入力を残す。409（`Pinned`）: 固定の注意。
- **Pinned の扱い**: 固定のとき `GET …/edit` は 409 の断片（理由と「閉じる」）。`PUT`・`DELETE` も 409（今の全体の保存と同じ）。表の「編集」は出さない。

## 3. 画像・動画モデル（`/settings/models`）

方式: **表 2 つ（画像の用途・動画の Quality Policy）＋ 「編集」はモーダル**。

- 実装（2026-10-25）: 表は 2 つ（`media-images`・`media-policies`。見出しは `h3`）、行のモーダルは `GET …/images/:purpose/edit`・`…/policies/:policy/edit`、保存は `PUT …/images/:purpose`・`…/policies/:policy`（**その行だけ**検査して書く。`SaveImage`・`SavePolicy`。他の行が年齢確認で使えないモデルを指していても、この行の保存は止まらない）。成功の応答は行・`#model-status`（年齢確認の注意とすべて既定に戻す。保存で変わるので同じ応答で OOB）・トースト。反映は即時で、モーダルを開くたびに最新の選択肢（ローカルのモデルを含む）を読む。全体の保存 `PUT /settings/models`・`DELETE /settings/models` は残す（応答は `#model-assignment`）。
- 画像（行 `id="media-image-<purpose>"`）: 用途・説明（`Hint`）・主モデル・代替モデル・（高速版があるときだけ）高速版の印。「編集」。参照画像を使えないモデルの注意（`ReferenceNote`）は**該当する行にだけ**出す（今の区画の上の常時表示の注意は、行がその状態でなければ出さない）。
- 動画（行 `id="media-policy-<policy>"`）: Quality Policy・主モデル（モデル＠解像度）・代替・テイク数。「編集」。
- モーダル（`GET /settings/models/images/:purpose/edit`・`/settings/models/policies/:policy/edit`）: 1 行ぶんのフォーム（今の `imageRow`・`policyRow` の欄をそのまま入れる）。保存は `PUT /settings/models/images/:purpose`・`/settings/models/policies/:policy`（**新しいルート**。今の `PUT /settings/models` は全体の保存として残す。§7 の質問 4: 行ごとにしてよいか。決定事項 9(a) は LLM だけを決めている）。
- 固定（`Pinned`）: 表は読み取りだけで「編集」を出さず、今の `PinnedNote` を出す。全体の「既定に戻す」（`Customized` のときだけ。`DELETE /settings/models`）は表の上に残す。
- 反映: 画像・動画モデルの保存は**すぐに効く**（LLM と違い再起動は要らない。今の説明文のとおり）。ローカルのモデルを足す・消すと、選択肢が変わる（今は `#model-assignment` を OOB で更新している。別ページになるので、モーダルを開くたびに最新の選択肢を読む。OOB は外す）。
- 年齢確認（モデル管理の「成人向けモデルを表示する」。[model-manager.md](../architecture/model-manager/model-manager.md) §6.1）がオフの間: **成人向けのモデル（名前・読むファイルで判る。§6.1 の述語 `HidesModel`）は、主・代替のどちらの選択肢にも出さない**。保存済みの割り当てがそのモデルを指していれば、その行は**選択済みのまま「（現在は使えません）」を付けて見せ**（別のモデルが選ばれたように見せない）、区画の上に注意を出す（`View.Gated`）。保存は、成人向けのモデルを指したままだと 422 で理由つきに断る（変えずに保存し直しても断る）。「既定に戻す」は通る。年齢確認をオンにすれば、同じ割り当てがそのまま使える。Shot の Video Model のロックの選択肢は除かない（選べるが生成は断る。§6.1）。

## 4. MCP サーバー（`/settings/mcp`）

方式: **待ち受け先の要約 ＋ 「変更」はモーダル、トークンの表 ＋ 「発行」はモーダル、「失効」は今のまま**。

- 待ち受け先（`SettingsCard`、`id="mcp-bind-row"`）: 「待ち受け中のアドレス: `http://<addr>/mcp`」（待ち受けていないときは「待ち受けていません」。`Problem` は `role="alert"`）、「変更」。モーダル（`GET /settings/mcp/bind/edit`）: 今の `bindForm`（候補の `<select>` と手入力）。保存は今の `PUT /settings/mcp/bind`（成功は行の OOB。拒否は 422。今の仕様は `endpoints.md` のまま）。
- トークンの表（`SettingsTable`、行 `id="mcp-token-<id>"`）: 名前・作成日・最後に使った日（未使用は「未使用」）・状態（`Badge`: 有効（ok）／失効済み（neutral））・「失効」（有効のときだけ。今の `DELETE /settings/mcp/tokens/:id`、`hx-confirm` のまま。`SmallButtonFor`）。
- 「トークンを発行」（表の上）→ モーダル（`GET /settings/mcp/tokens/new`）: 名前の欄（`MaxNameLen`）と「発行」。`POST /settings/mcp/tokens`: 成功は **201 で、同じモーダルの本文を「発行したトークン」の表示に差し替え**（`HX-Trigger-After-Swap` は付けない。モーダルを閉じない）、トークンの値を**この応答に 1 回だけ**入れ（`Cache-Control: no-store`）、「コピー」ボタン（`navigator.clipboard`。失敗時は欄を選択状態にして手動のコピーを案内）と、`role="status"` の「コピーしました」。表の新しい行は同じ応答の OOB（`beforeend:#mcp-token-list`）で足す（値は載せない。先頭 8 文字だけ）。
- トークンの表示は `readonly` の `<input>`（`aria-label`「発行したトークン」）に入れ、モーダルを**閉じると本文ごと消す**（二度と出さない。今の決まりのまま）。閉じる前に「このトークンはもう表示されません」の注意を出す。履歴のスナップショットにも残さない（shared-ui-parts §4）。名前が不正なら 422（入力を残す）。
- 発行の応答を返したあとに再読み込み・戻るをしても、トークンは出ない（今と同じ）。
- **実装の注記**（2026-10-25。ヘッドレスの Chromium（Linux）で確かめた）: コピーのボタンは `data-copy-source`（欄の id）・`data-copy-status`（`role="status"` の行）を持ち、`static/src/components/copy/` が押下を聞く。クリップボードが使えないときは欄を選択状態にして手動のコピーを案内する。トークンの表の行（`<tr>`）と `<tbody>` の OOB は `<template>` に包む（[../api/endpoints-settings.md](../api/endpoints-settings.md) §5.1）。失効したあとは行（`<tr tabindex="-1">`）にフォーカスが移る（押したボタンが消えるため）。待ち受け先のモーダルは `datalist` つきの 1 欄で、保存済みのアドレスが入る。 Windows 版（WebView2）での `navigator.clipboard` によるコピーは**未検証**（実機が要る。使えないときは欄を選択して手動のコピーを案内する）。

## 5. ローカル・代替モデル（`/settings/local-models`）

方式: **モデルマネージャーのカード ＋ 接続先のカード ＋ 追加・編集はモーダル ＋ モデルの一覧は接続先のサブページ**（決定事項 6(a)・8(a)）。

- 先頭: モデルマネージャーへの `ToolCard`（`paths.ModelManager()`。ComfyUI の状態は `hx-get="/tools/model-manager/status"`）。
- 接続先のカード（行 `id` は今のまま `local-endpoint-<name>` を使う案。今の ID は実装で確かめる）: 名前・種類（ComfyUI / OpenAI 互換）・URL・接続の状態（「接続テスト」の結果。カードを開いたときは**自動では確かめない**。ボタンで確かめる）・モデルの数・API キーの有無。ボタン: 「接続テスト」（今の `POST …/endpoints/:name/test`。結果の行だけ差し替え）、「編集」（モーダル）、「モデル（n）」（サブページへのリンク）、「削除」（`hx-confirm`。使用中は 422）。「接続先を追加」（一覧の上）。
- 接続先の追加・編集のモーダル（`GET …/endpoints/new`・`…/endpoints/:name/edit`。今のフォームを入れる。**接続テストつき**: 入力中の内容を保存せずに試す `POST …/endpoints/test`）。名前と種類は編集で入力欄にしない（今のまま）。固定ファイル（`--model-config`）のときは追加・編集・削除のボタンを `disabled`（理由は `title`）にし、モーダルのルートは 409。
- API キーの行（接続先ごと）は、カードの中の小さな行として今の `#local-key-row-<name>` を残す（キーの保存・削除は今のルート。モーダルにするかは §7 の質問 5）。
- **サブページ**（`GET /settings/local-models/endpoints/:name`）: 接続先の概要（カードと同じ内容）と、モデルの表（名前・種類（画像／動画）・モデル ID・使用中の場所）。「モデルを追加」（今の 2 段階のフォームを入れる。ComfyUI のテンプレート／ワークフロー JSON の選択 → 割り当て表は項目が多く長い作業のため、**このサブページの中に開く**。モーダルにしない）・モデルごとの「編集」「削除」。ナビの現在の分類は「ローカル・代替モデル」、見出しは h2「ローカル・代替モデル」の下に h3 で接続先名。パンくず（「← ローカル・代替モデルに戻る」）を最上部に置く。
- 名前が無い接続先の URL は 404。HTMX でない `GET` はページ全体、HTMX は断片（settings-screen.md §3 と同じ）。
- **実装の注記**（2026-10-25。ヘッドレスの Chromium（Linux）で確かめた）:
  - カードの行 `id` は `local-endpoint-<名前>`、一覧は `#local-endpoints`（`<ul>`）、「接続先はまだ…」の文は `#local-endpoints-empty`。接続の状態は「未確認」で、「接続テスト」の結果が `#local-test-<名前>` に入る。カードの要約は「同時実行・タイムアウト・モデル n 件」。
  - 追加・編集のモーダルの保存は、カードの OOB（追加は一覧の末尾に足す）とトースト。名前の重複は 409、入力の誤りは 422 で、モーダルに理由と入力値（API キーを除く）。サブページで開いた編集（`?page=1`）は、サブページ先頭の概要のカード（削除ボタンとモデルへのリンクが無い形。接続先をここで削除すると開いているページが無くなるため）を置き換える。
  - 削除（接続先・モデル）は、押したボタンのある一覧（`<ul>`・`<tbody>`）の中身を、残りのカードや行で置き換える（フォーカスの戻りのため。対象の要素そのものを消すと `htmx:afterSwap` が文書まで届かず、フォーカスが `<body>` に落ちる）。使用中は 422 のトースト。
  - モデルのフォーム（ComfyUI は 2 段階）はサブページの表の上のエディタ（`#local-models-editor`）に開き、見出し（`<h4 tabindex="-1" autofocus>`）にフォーカスが移る。保存の成功でエディタは空になり、表の行を OOB で置き換える。「閉じる」はサブページを取り直す。
  - 固定ファイル（`--model-config`）では、追加・編集・削除・「モデルを追加」が `disabled`（`title` に理由）。モーダルとエディタの `GET` は 409。
  - 名前 `new` の保存済みの接続先: カードに「モデル」のリンクを出さず、理由（追加の画面と同じ URL になる）と、削除して作り直す案内を出す。編集・削除・接続テストは使える。新しく作るのは 422。

## 6. バージョンとアップデート（`/settings/update`）

今のまま（`updatepanel` の中身）をサブページに移す。「今すぐ確認」（`POST /settings/update/check`。Wails 版だけ）も今のまま。見出しは分類の h2 だけで、区画の見出しは出さない。

## 7. オーナーへの確認事項（このファイルの範囲。推奨する既定つき）

| # | 質問 | 推奨する既定 |
|---|------|-------------|
| 1 | 接続テストを付けるプロバイダー（§1.1 の候補: Anthropic・OpenAI・Google・BFL・Runway・ElevenLabs。Kling・Seedance・Sync Labs・TypeSafe は候補なし） | 候補の 6 つを実装のときに 1 つずつ公式ドキュメントで確かめて付ける。確かめられないものは外す。**実キーでの動作は所有者の実機で確かめる（未検証）** |
| 2 | Agent の「用途」の 1 行の説明の文言 | `model-adapters.md` §2 の選定理由から 30 字前後に要約する（実装のときに草案を出す） |
| 3 | 「この Agent を既定に戻す」に確認（`hx-confirm`）を付けるか | 付けない（モーダルの中で、保存前に取り消せる。決定事項 8(a) は削除の確認だけを `hx-confirm` と言っている） |
| 4 | 画像・動画モデルの行ごとの保存（`PUT /settings/models/images/:purpose`・`/policies/:policy`）を足すか。決定事項 9(a) は LLM だけ | 足す（モーダルで編集して閉じたのに保存されていない取り違えを防ぐ。LLM と同じ理由）。足さず、全体のフォームの保存をモーダルの外に置く案は、9(b) と同じ問題が出る |
| 5 | ローカルの接続先の API キーを、モーダルにするか（カード内の行のまま） | カード内の行のまま（1 項目で、今のルート・応答がそのまま使える） |
| 6 | ログイン方式のカードの順（公式 → 画像生成（非公式）→ Codex） | その順（#845 が 3 つ目と書いている） |
| 7 | 概要・各分類の「再起動で反映」の印を、LLM の行ごとに出すか（保存済みで実行中のモデルと違うことを示す） | 出す（保存した直後の行だけ。再読み込みすると消える。サービスが「実行中の値」を持たないため、永続の印にはできない） |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 0.1 | 2026-10-25 | 新規作成（案） | Issue #847 決定事項 1(a)・3(a)・5(a)・8(a)・9(a)。分類ごとの一覧とモーダルの案 |
| 0.2 | 2026-10-25 | §1.3 の Codex 経由の行と §2 の `chatgpt-codex` の記述を、入った #845 の実装に合わせた | 統合の見直し。#845 が先に入ったため |
| 0.3 | 2026-10-25 | §1.3 のログインのモーダルの取り直しのクエリを、今の `GET /settings/chatgpt?method=` との関係つきで書き直した（`card` は未決の質問） | 独立レビュー（PR #849）の指摘 |
| 0.4 | 2026-10-25 | §3 に、年齢確認がオフの間の画像・動画モデルの画面（成人向けを選択肢から外す・保存済みの割り当ては「（現在は使えません）」付きで選択済みに見せる・保存は 422・既定に戻すは通る・ロックの選択肢は除かない）を足した | PR #849 の独立レビュー（#848）の指摘。#848 で入った画面の挙動が画面仕様に無かったため |
| 0.5 | 2026-10-25 | §1 を実装済みにした。§1.1 の接続テストの表の「確認の状態」を、公式資料で確かめた結果（6 つに接続テスト、4 つは候補なし）と、実キーでは未検証であることに書き直し、キーの取り方のリンクの URL を書いた。成功の通知はトースト（決定事項どおり） | Issue #847 の実装 |
| 0.6 | 2026-10-25 | §2・§3 を実装済みにした。§2 の「用途」の列を `lg:` から `xl:` にした（1024×700 の実画面で比べた結果）。実装の形（行の保存・`#model-status`・全体の保存の残し方）を足した | Issue #847 段階 ②-3・②-4 の実装 |
| 0.7 | 2026-10-25 | §4（MCP）・§5（ローカル・代替モデル）を実装済みにし、実装の注記（行の `id`・削除の応答の形・サブページで開いた編集・名前 `new`・コピーのボタン）を足した | Issue #847 ②-5・②-6 |
| 0.8 | 2026-10-25 | §1.1 の接続テストの表を、実装した接続テストと公式資料の確認（endpoints-settings.md §4）に合わせた。§1.3 のログインの取り直しは、`card=<kind>` を足さずモーダルが自分の URL を取り直す形に直した。「実機の Chromium」を「ヘッドレスの Chromium（Linux）」に直した | PR #849 の第 2 周の独立レビューの指摘 |
