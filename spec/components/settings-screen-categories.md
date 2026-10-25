# 画面仕様: 設定画面の分類ごとの一覧とモーダル

> **状態: 案（実装前。オーナーの確認待ち）。** Issue #847 ステージ ②。枠・ナビ・URL・概要は [settings-screen.md](settings-screen.md)、ルートと応答の決まりは [settings-screen-routes.md](settings-screen-routes.md)、部品は [shared-ui-parts.md](shared-ui-parts.md)。`<dialog>` の決まり（開く・閉じる・フォーカス・成功・422）はすべて shared-ui-parts §4 に従い、ここでは繰り返さない。

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

| provider | 何に使うキーか（案。`model-adapters.md` の節） | 接続テストの候補（読むだけで無料のもの） | 確認の状態 |
|----------|---------------------------------------------|----------------------------------------|------------|
| `anthropic` | テキスト生成（Claude。§2） | `GET https://api.anthropic.com/v1/models`（`x-api-key`・`anthropic-version`） | **未確認**（広く知られた仕様だが、この仕様では一次資料を未読・実キーでは未実行） |
| `openai` | テキスト生成（GPT-5 系）と画像生成（GPT Image 2。§2・§3） | `GET https://api.openai.com/v1/models`（`Authorization: Bearer`） | **未確認**（同上） |
| `google` | テキスト生成（Gemini）と画像生成（Nano Banana。§2・§3） | `GET https://generativelanguage.googleapis.com/v1beta/models`（`x-goog-api-key`） | **未確認**（同上） |
| `flux` | 画像生成（FLUX.2。§3） | `GET https://api.bfl.ai/v1/credits`（`x-key`。残高を読むだけ） | 公式ドキュメント（docs.bfl.ai「Get the user's credits」）に記載があると検索結果の要約で確認（2026-10-25）。**実キーでは未検証** |
| `kling` | 動画生成（Kling 3.0。§4） | なし（キーは `ACCESS_KEY:SECRET_KEY` で、JWT を作って呼ぶ。読むだけの無料の API は**未確認**） | **未確認**→ 候補なし。「保存したあと、最初の生成で確かめられます」を出す |
| `seedance` | 動画生成（Seedance 2.0。§4） | なし（BytePlus ModelArk。読むだけの無料の API は**未確認**） | **未確認**→ 候補なし |
| `runway` | 動画生成（Runway Gen-4.5。§4） | `GET https://api.dev.runwayml.com/v1/organization`（`Authorization: Bearer`・`X-Runway-Version`。組織情報と残高を読むだけ） | 検索結果の要約で記載を確認（2026-10-25）。**実キーでは未検証**。`X-Runway-Version` の値は既存のアダプターに合わせる |
| `elevenlabs` | 音声（TTS・Music・SFX。§5） | `GET https://api.elevenlabs.io/v1/user/subscription`（`xi-api-key`。契約の状態を読むだけ） | 公式ドキュメントの記載を検索結果の要約で確認（2026-10-25）。**実キーでは未検証** |
| `synclabs` | Lip Sync（§5） | なし（**未確認**） | **未確認**→ 候補なし |
| `typesafe` | Repair の前段選別（Jev。§4.5） | なし（`POST /v1/systemone` だけ。課金の有無は**未確認**） | **未確認**→ 候補なし |

- 候補は、決定事項 5(a)（無料で読むだけの API があるプロバイダーだけ）に沿った案。**どのプロバイダーに付けるかは、実装のときに公式ドキュメントを 1 つずつ確かめて決め、実キーでの動作は所有者の実機で確かめる（未検証）**。確認できないものには接続テストのボタンを出さず、「保存したあと、最初の生成で確かめられます」を出す。
- 「キーの取り方のリンク」（各社のコンソール）の URL もすべて実装のときに公式ページで確かめる（この仕様には書かない。未確認）。

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

- 「ログイン」はモーダルを開き、ブラウザ／デバイスコードの選択（フォーム `method` = `browser` / `device`）→ 開始 → 途中の表示（認可 URL・確認 URL・コード・期限）を**モーダルの中**に出す。ログイン中の 2 秒ごとの取り直し（`hx-trigger="every 2s"`）は、モーダルの本文が自分を取り直す形（HTMX の `GET /settings/chatgpt`）。取り直しのクエリは、今の `GET /settings/chatgpt` が持つ `method`（`official` / `codex`。#845。取り直しでも保つ）を使う。**公式・Codex の 2 つはこの `method` で足りるが、画像生成（非公式。`image`）は `method` では指せない**。カードごとに取り直すための新しいクエリ（案: `card=<kind>`。`kind` は `official` / `image` / `codex`）を足すか、`method` を `kind` に置き換えるかは未決（[settings-screen-routes.md](settings-screen-routes.md) §6 の質問 5）。完了・取り消しでモーダルを閉じ、カードの行を OOB で差し替える。
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
- 14 行で、1440×900 では 1 画面に収まらず main がスクロールする。1024×700 では「用途」列を `lg:` 以上でだけ出し、狭いときは行見出しの下に 1 行で出す。

モーダル（`GET /settings/llm/agents/:agent/edit`。`:agent` は `llmassign.Agents()` の名前。未知は 404）:

- 見出し: 「<Agent 名> の LLM」。説明に用途と「反映にはアプリの再起動が必要です」。
- 欄: プライマリ（プロバイダーの `<select>`＋モデル名の `<input>`）、代替（同上）。**既定の値を横に出す**（「既定: anthropic / claude-…」。`text-neutral-400`）。モデル名の自由入力は残し、分かる範囲で候補を出す（`<datalist>`。ChatGPT のログインのモデル一覧（`GET /settings/chatgpt/models`）や #845 の `model/list`。**プロバイダーごとにモデルの一覧を取れるかは未検証**。候補が無いプロバイダーは自由入力だけ）。モデル名の長さは `MaxModelLength`。
- ボタン: 「保存」（`PUT /settings/llm/agents/:agent`）、「この Agent を既定に戻す」（`DELETE /settings/llm/agents/:agent`。`hx-confirm` なし。モーダルの中の操作で、いつでもやり直せるため。§7 の質問 3）、「キャンセル」。
- 成功: 行を OOB で差し替え、閉じる。**行の要約が新しい値になる**（「保存済み・再起動で反映」の印を行に出す: `Badge`「再起動で反映」（warn））。422: 理由（`InvalidError.Message`。例「Writer（脚本） のプライマリ: モデル名を入力してください」）と入力を残す。409（`Pinned`）: 固定の注意。
- **Pinned の扱い**: 固定のとき `GET …/edit` は 409 の断片（理由と「閉じる」）。`PUT`・`DELETE` も 409（今の全体の保存と同じ）。表の「編集」は出さない。

## 3. 画像・動画モデル（`/settings/models`）

方式: **表 2 つ（画像の用途・動画の Quality Policy）＋ 「編集」はモーダル**。

- 画像（行 `id="media-image-<purpose>"`）: 用途・説明（`Hint`）・主モデル・代替モデル・（高速版があるときだけ）高速版の印。「編集」。参照画像を使えないモデルの注意（`ReferenceNote`）は**該当する行にだけ**出す（今の区画の上の常時表示の注意は、行がその状態でなければ出さない）。
- 動画（行 `id="media-policy-<policy>"`）: Quality Policy・主モデル（モデル＠解像度）・代替・テイク数。「編集」。
- モーダル（`GET /settings/models/images/:purpose/edit`・`/settings/models/policies/:policy/edit`）: 1 行ぶんのフォーム（今の `imageRow`・`policyRow` の欄をそのまま入れる）。保存は `PUT /settings/models/images/:purpose`・`/settings/models/policies/:policy`（**新しいルート**。今の `PUT /settings/models` は全体の保存として残す。§7 の質問 4: 行ごとにしてよいか。決定事項 9(a) は LLM だけを決めている）。
- 固定（`Pinned`）: 表は読み取りだけで「編集」を出さず、今の `PinnedNote` を出す。全体の「既定に戻す」（`Customized` のときだけ。`DELETE /settings/models`）は表の上に残す。
- 反映: 画像・動画モデルの保存は**すぐに効く**（LLM と違い再起動は要らない。今の説明文のとおり）。ローカルのモデルを足す・消すと、選択肢が変わる（今は `#model-assignment` を OOB で更新している。別ページになるので、モーダルを開くたびに最新の選択肢を読む。OOB は外す）。

## 4. MCP サーバー（`/settings/mcp`）

方式: **待ち受け先の要約 ＋ 「変更」はモーダル、トークンの表 ＋ 「発行」はモーダル、「失効」は今のまま**。

- 待ち受け先（`SettingsCard`、`id="mcp-bind-row"`）: 「待ち受け中のアドレス: `http://<addr>/mcp`」（待ち受けていないときは「待ち受けていません」。`Problem` は `role="alert"`）、「変更」。モーダル（`GET /settings/mcp/bind/edit`）: 今の `bindForm`（候補の `<select>` と手入力）。保存は今の `PUT /settings/mcp/bind`（成功は行の OOB。拒否は 422。今の仕様は `endpoints.md` のまま）。
- トークンの表（`SettingsTable`、行 `id="mcp-token-<id>"`）: 名前・作成日・最後に使った日（未使用は「未使用」）・状態（`Badge`: 有効（ok）／失効済み（neutral））・「失効」（有効のときだけ。今の `DELETE /settings/mcp/tokens/:id`、`hx-confirm` のまま。`SmallButtonFor`）。
- 「トークンを発行」（表の上）→ モーダル（`GET /settings/mcp/tokens/new`）: 名前の欄（`MaxNameLen`）と「発行」。`POST /settings/mcp/tokens`: 成功は **201 で、同じモーダルの本文を「発行したトークン」の表示に差し替え**（`HX-Trigger-After-Swap` は付けない。モーダルを閉じない）、トークンの値を**この応答に 1 回だけ**入れ（`Cache-Control: no-store`）、「コピー」ボタン（`navigator.clipboard`。失敗時は欄を選択状態にして手動のコピーを案内）と、`role="status"` の「コピーしました」。表の新しい行は同じ応答の OOB（`beforeend:#mcp-token-list`）で足す（値は載せない。先頭 8 文字だけ）。
- トークンの表示は `readonly` の `<input>`（`aria-label`「発行したトークン」）に入れ、モーダルを**閉じると本文ごと消す**（二度と出さない。今の決まりのまま）。閉じる前に「このトークンはもう表示されません」の注意を出す。履歴のスナップショットにも残さない（shared-ui-parts §4）。名前が不正なら 422（入力を残す）。
- 発行の応答を返したあとに再読み込み・戻るをしても、トークンは出ない（今と同じ）。

## 5. ローカル・代替モデル（`/settings/local-models`）

方式: **モデルマネージャーのカード ＋ 接続先のカード ＋ 追加・編集はモーダル ＋ モデルの一覧は接続先のサブページ**（決定事項 6(a)・8(a)）。

- 先頭: モデルマネージャーへの `ToolCard`（`paths.ModelManager()`。ComfyUI の状態は `hx-get="/tools/model-manager/status"`）。
- 接続先のカード（行 `id` は今のまま `local-endpoint-<name>` を使う案。今の ID は実装で確かめる）: 名前・種類（ComfyUI / OpenAI 互換）・URL・接続の状態（「接続テスト」の結果。カードを開いたときは**自動では確かめない**。ボタンで確かめる）・モデルの数・API キーの有無。ボタン: 「接続テスト」（今の `POST …/endpoints/:name/test`。結果の行だけ差し替え）、「編集」（モーダル）、「モデル（n）」（サブページへのリンク）、「削除」（`hx-confirm`。使用中は 422）。「接続先を追加」（一覧の上）。
- 接続先の追加・編集のモーダル（`GET …/endpoints/new`・`…/endpoints/:name/edit`。今のフォームを入れる。**接続テストつき**: 入力中の内容を保存せずに試す `POST …/endpoints/test`）。名前と種類は編集で入力欄にしない（今のまま）。固定ファイル（`--model-config`）のときは追加・編集・削除のボタンを `disabled`（理由は `title`）にし、モーダルのルートは 409。
- API キーの行（接続先ごと）は、カードの中の小さな行として今の `#local-key-row-<name>` を残す（キーの保存・削除は今のルート。モーダルにするかは §7 の質問 5）。
- **サブページ**（`GET /settings/local-models/endpoints/:name`）: 接続先の概要（カードと同じ内容）と、モデルの表（名前・種類（画像／動画）・モデル ID・使用中の場所）。「モデルを追加」（今の 2 段階のフォームを入れる。ComfyUI のテンプレート／ワークフロー JSON の選択 → 割り当て表は項目が多く長い作業のため、**このサブページの中に開く**。モーダルにしない）・モデルごとの「編集」「削除」。ナビの現在の分類は「ローカル・代替モデル」、見出しは h2「ローカル・代替モデル」の下に h3 で接続先名。パンくず（「← ローカル・代替モデルに戻る」）を最上部に置く。
- 名前が無い接続先の URL は 404。HTMX でない `GET` はページ全体、HTMX は断片（settings-screen.md §3 と同じ）。

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
