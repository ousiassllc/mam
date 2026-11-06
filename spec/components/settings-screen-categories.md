# 画面仕様: 設定画面の分類ごとの一覧とモーダル

> **状態: §1（API キーとログイン）・§2（LLM）・§3（画像・動画）・§4（MCP）・§5（ローカル・代替モデル）は実装済み（2026-10-25）。§8（ffmpeg。パスの指定と、ダウンロードして入れる部品）は Issue #856・#900 で実装済み（2026-11-01）。§9（音声・口パク。声と口パクのプロバイダーの選択）は Issue #924 で実装済み（2026-11-01）。§1.3 と §9 は Issue #1005 で API キーと同じカードの形にそろえた（2026-11-03）。** Issue #847 ステージ ②。オーナーは §7 を推奨する既定で決定した（2026-10-25）。枠・ナビ・URL・概要は [settings-screen.md](settings-screen.md)、ルートと応答の決まりは [settings-screen-routes.md](settings-screen-routes.md)、部品は [shared-ui-parts.md](shared-ui-parts.md)。`<dialog>` の決まり（開く・閉じる・フォーカス・成功・422）はすべて shared-ui-parts §4 に従い、ここでは繰り返さない。

方式の凡例（決定事項 8(a)）: **モーダル** = 1 つの対象の短い編集（項目が数個・1 回の保存）。**サブページ** = 一覧を持つもの・長い作業（URL を持つ）。削除の確認は今のまま `hx-confirm`。

**説明の出し方（Issue #866）**: 各分類の h2 の説明と、区画・カードの説明の長文は、常時表示せず、見出し（カードはタイトル）の横の「?」ボタンで開くヘルプに入れる（[shared-ui-parts.md](shared-ui-parts.md) §9・§10。分類ごとの対応は §10 の表）。操作のラベル・状態・結果・エラー・確認（`role="alert"`・`hx-confirm`）・操作の前提（LLM の「反映にはアプリの再起動が必要です」）はヘルプに入れず、見えたままにする。以下の各節で「説明」と書いたものは、ヘルプの中身のことである。

## 1. API キーとログイン（`/settings/api-keys`）

### 1.1 API キー（15 プロバイダー。`service.CredentialProviders` の順）

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
| `google` | テキスト生成（Gemini）・画像生成（Nano Banana。§2・§3）・動画生成（Gemini Omni Flash。§4） | `GET https://generativelanguage.googleapis.com/v1beta/models?pageSize=1`（`x-goog-api-key`） | 記載を確認。**実キーでは未検証** |
| `flux` | 画像生成（FLUX.2。§3） | `GET https://api.bfl.ai/v1/credits`（`x-key`。残高を読むだけ） | 公式の OpenAPI（`docs.bfl.ml`）に記載。**実キーでは未検証** |
| `kling` | 動画生成（Kling 3.0。§4） | なし（キーは `ACCESS_KEY:SECRET_KEY` で、JWT を作って呼ぶ。読むだけの無料の API を公式資料で確認できなかった） | 候補なし。「保存したあと、最初の生成で確かめられます」を出す |
| `kling4` | 動画生成（Kling 4.0。§4・[kling-api.md](../architecture/kling/kling-api.md) §5） | なし（キーは Kling の API キー 1 つで、そのまま `Bearer` に付ける。Kling 3.0 の `kling` とは別に保存する。読むだけの無料の API を公式資料で確認できなかった） | 候補なし。同上 |
| `minimax` | 動画生成（MiniMax H3・H3 Max。§4・[model-adapters.md](../architecture/model-adapters.md) §9.4） | なし（キーは MiniMax の API キー 1 つで、そのまま `Bearer` に付ける。従量課金（Pay-as-you-go）の API が要る。読むだけの無料の API を公式資料で確認できなかった） | 候補なし。同上 |
| `luma` | 動画生成（Luma Ray 3.2。§4・[model-adapters.md](../architecture/model-adapters.md) §9.4） | なし（キーは Luma Agents API の API キー 1 つで、そのまま `Bearer` に付ける。Luma のアプリのものとは別。読むだけの無料の API は公式資料の `GET /v1/credits`（残高）があるが、接続テストは未実装） | 候補なし。同上 |
| `alibaba` | 動画生成（Alibaba Cloud Model Studio の Wan 3.0。§4・[model-adapters.md](../architecture/model-adapters.md) §9.4） | なし（キーは `ワークスペース ID:API キー` の形（Singapore リージョン）で、前半はホスト名に、後半は `Bearer` に使う。読むだけの無料の API を公式資料で確認できなかった） | 候補なし。同上 |
| `seedance` | 動画生成（Seedance 2.0・2.5。§4） | なし（BytePlus ModelArk。読むだけの無料の API を公式資料で確認できなかった） | 候補なし。同上 |
| `runway` | 動画生成（Runway Gen-4.5。§4） | `GET https://api.dev.runwayml.com/v1/organization`（`Authorization: Bearer`・`X-Runway-Version`。組織の残高と階層を読むだけ） | 公式の `api.md` に記載。**実キーでは未検証**。`X-Runway-Version` の値は endpoints-settings.md §4 |
| `elevenlabs` | 音声（TTS・Music・SFX。§5） | `GET https://api.elevenlabs.io/v1/user/subscription`（`xi-api-key`。契約の状態を読むだけ） | 認証ヘッダーは公式の認証の頁、エンドポイントは記載を確認。**実キーでは未検証** |
| `topaz` | 動画のアップスケール・コマ補間（Topaz Video API。有料クラウド。生成テストの「動画を大きくする・滑らかにする」。[model-adapters.md](../architecture/model-adapters.md) §15、Issue #1031） | `GET https://api.topazlabs.com/account/v1/credits/balance`（`X-API-Key`。クレジット残高を読むだけ。公式の FAQ「Can I check my credit balance from code?」） | FAQ の記載を確認（2026-11-04）。**実キーでは未検証**。キーの取り方: `https://account.topazlabs.com/manage-api` |
| `synclabs` | Lip Sync（§5） | なし（読むだけの無料の API を公式資料で確認できなかった） | 候補なし。同上 |
| `typesafe` | Repair の前段選別（Jev。§4.5） | なし（`POST /v1/systemone` だけ。読むだけの無料の API を公式資料で確認できなかった） | 候補なし。同上 |

- 接続テストは、決定事項 5(a)（無料で読むだけの API があるプロバイダーだけ）に沿って付けた（上の 6 つと、Issue #1031 で足した `topaz` の 1 つ）。**実キーでの動作は所有者の実機で確かめる（未検証）**。公式資料で確認できなかった 4 つには接続テストのボタンを出さず、「保存したあと、最初の生成で確かめられます」を出す。
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

方式: **カード（API キーと同じ形・2 列）＋ 詳細・ログイン・モデル一覧はモーダル**（Issue #1005 で、横幅いっぱいの大きいパネルから API キーのカードと同じ形にそろえた）。状態の取得とポーリングは今の `GET /settings/chatgpt`（HTMX の要求）を使う。

**カードに出すもの**（`settingsui.CardItem`。`ul#chatgpt-cards` は API キーと同じ `grid grid-cols-1 gap-4 xl:grid-cols-2`）: 名前（`h4`）・**状態の `Badge`**・**リスクの印（`Card.Marks`）**・1 行の説明・今の状態（誰がログインしているか・ログインの失敗の理由・ダウンロードの進み具合・利用上限）・主なボタン。長い説明・注意書き・トークンの期限・「Manage usage」・リスクの一覧・エンジンの詳細は、**「詳細」ボタンで開くモーダル**（`GET /settings/chatgpt/:kind/detail`）に移した。カードの「?」ボタンは付けない（説明は 1 行の説明と詳細）。

| カード | 状態の `Badge`・印 | 主なボタン（カード） | 詳細（モーダル） |
|--------|-------------------|---------------------|------------------|
| Sign in with ChatGPT（公式。テキスト生成） | ログイン済み／未ログイン／ログイン待ち／再ログインが必要／プランの利用が未許可。ログイン済みはメール・プラン。組織のアカウントには「Codex 経由のカードへ」の案内（`chatgpt-use-codex`）をカードに出す | 「ログイン」（「ログインし直す」「プランの利用を許可する」）／「利用できるモデルを表示」／「ログアウト」／「詳細」 | 説明（プロバイダー `chatgpt-subscription`・組織は Codex）・アクセストークンの期限・「Using ChatGPT plan」と「Manage usage」・ブラウザは mam が動いている PC で開くこと |
| ChatGPT 画像生成（非公式。既定 OFF） | **印「非公式」を ON/OFF にかかわらず常に出す**。状態は「オフ」／ログインの状態。オフの間は「オフの間は、この経路を一切呼びません」と、リスクの 1 行（予告なく使えなくなる・プランの利用上限を消費する） | OFF: 「リスクを確認してオンにする」（詳細を開く）。ON: 「ログイン」「ログアウト」「オフにする」「詳細」 | **リスクの一覧**（`chatgpt-risks`）・「Manage usage」。OFF の間は確認のチェック（`acknowledge_risks`。`required`）と「オンにする」（`hx-confirm` つき）。ON の間はアカウントとトークンの期限 |
| ChatGPT 経由の Codex（テキスト生成のみ。#845） | **印「商用注意」を常に出す**。状態は未ログイン／ログイン待ち／ログイン済み／エンジン未取得／エンジンが異常。カードに「商用・ホスティングでの提供には使えません（詳細に条件があります）」の 1 行 | 「Codex をダウンロード」（エンジンが無く取得できるとき。`hx-confirm` に取得元・大きさ・置き場所）／「中断」／「ログイン」／「状態と利用上限を更新」／「利用できるモデルを表示」／「ログアウト」／「詳細」 | **商用・ホスティングに使えない条件の全文**（`chatgpt-codex-terms`）・説明・エンジンの状態（バージョン・取得元・更新の有無・エラー）・「自分で入れた codex のパス」と「パスを設定」・保存先・Codex Local の権限・「ログアウトして Codex 本体と CODEX_HOME も削除」（エンジンが使えて、ログイン済みのとき） |

- 「ログイン」はモーダルを開き（`GET /settings/chatgpt/:kind/login`。`:kind` は `official` / `image` / `codex`）、ブラウザ／デバイスコードの選択（フォーム `method` = `browser` / `device`）→ 開始 → 途中の表示（認可 URL・確認 URL・コード・期限）を**モーダルの中**に出す。ログイン中の 2 秒ごとの取り直し（`hx-trigger="every 2s"`）は、モーダルの本文が自分の URL（`GET /settings/chatgpt/:kind/login?poll=1`）を取り直す形。今の `GET /settings/chatgpt` のクエリ `method`（`official` / `codex`。#845）は取り直しに使わず、`card=<kind>` も足さない（[settings-screen-routes.md](settings-screen-routes.md) §6 の質問 5。決定済み）。
- 「利用できるモデルを表示」の結果は、小さなモーダル（読み取りだけ）に出す。
- **詳細のモーダルの操作の答え**（画像生成の「オンにする」・Codex のパス・「ログアウトして…も削除」）は、HTMX の `HX-Target` が `mam-modal-body`（モーダルの本文）のとき、成功なら `HX-Trigger-After-Swap: modal-close` でモーダルを閉じてカードの一覧（`#chatgpt-panel`）を OOB で差し替え、失敗（422 など）ならモーダルの本文に `role="alert"`（`#mam-modal-error`）で理由を出してフォームを残す（カードの一覧は OOB で最新にする）。カードのボタン（「オフにする」「ログアウト」ほか）は今までどおり `#chatgpt-panel` を `outerHTML` で差し替える。
- **リスク・条件の見落とし防止**: 詳細に移した注意は、カードの**印（`Badge`。文字）**と 1 行で必ず分かるようにする（「非公式」「商用注意」。色だけにしない）。
- ログインのモーダルの詳細（デバイスコードの表示の文言・期限切れ・取り消し）は、今の `chatgptpanel` の文言を移す。変えない。
- **#845 との関係**: #845 は 3 つ目の区画と、その状態・ログインの処理を足した（実装済み）。このカードの枠・モーダルの部品・ルートの置き場（`organisms/settings/chatgpt/`）は #847 が作る。#845 の仕組み（取得・起動・`account/*`）は触らない。
- 長いメール・ファイル名・失敗の理由は折り返す（`break-words` / `break-all`。カードの外へはみ出さない。`long-names.wtest.ts` が `layout-long-names-chatgpt-cards.html` で 375 px・1280 px を測る）。

## 2. テキスト生成（`/settings/llm`）

方式: **Agent ごとに 1 行の表 ＋ 「編集」はモーダル**。保存は Agent ごと（決定事項 9(a)）。

- 先頭: 「保存した割り当ては、次の呼び出しからすぐ使われます（再起動は要りません）。」を**常に**出す（`role` なしの通常の段落。`data-testid="llm-apply-note"`。#1006 で「反映にはアプリの再起動が必要です」から変えた。Issue #866 以降は、その横の「?」ヘルプに「保存した割り当ては、次の呼び出しからすぐ使われます」と、再起動で反映される設定（`llm.json` を手で書き換えた `base_url`・タイムアウト・リトライ・サーキットブレーカー）の説明と ChatGPT プランの長文を入れる）。再起動で反映される設定があるときは、全分類の上に「再起動して反映」の欄が出る（[../architecture/restart.md](../architecture/restart.md) §4）。全体の「既定に戻す」（`DELETE /settings/llm`。今のまま）を表の上に残す。固定（`Pinned`）のとき: 今の注意（`--llm-config` / `MAM_LLM_CONFIG`）を出し、表は読み取りだけで「編集」と「既定に戻す」は出さない。
- ChatGPT のプランを使うプロバイダー `chatgpt-subscription` の注意（今の `llm-chatgpt-note`）は、Issue #866 で表の上の常時表示から、再起動の注記の横の「?」ヘルプの中に移した（`data-testid` はそのまま）。#845 の `chatgpt-codex`（実装済み）は、プロバイダーの `<select>` に並ぶだけ（`llm.Providers()` から）。

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
- ローカル LLM サーバー（#1008）: 表の下に、カード（`organisms/settings/llmlocal`、`settingsui.CardItem`）「ローカル LLM サーバー（OpenAI 互換）」を置く。欄はベース URL・API キー（任意・`type=password`・値は出さない）・「保存したキーを削除する」・スイッチ「画像・動画を作る前に、Ollama のモデルをメモリから降ろす」、ボタン「保存」（`PUT /settings/llm/local`）「接続テスト」（`POST /settings/llm/local/test`。結果は共通のエラー表示）。「API キーとログイン」のカードは変えない。**カードの先頭に、チェックボックス「この PC の llama.cpp を使う」**（`use_managed`・`use_managed_present`。管理サーバーのある app だけ。状態 `data-state`・理由・モデル名・モデルマネージャーへのリンク。オンの間はベース URL・API キーを使わない。`spec/architecture/local-llm.md` §10）。詳細は `spec/architecture/local-llm.md` §5

モーダル（`GET /settings/llm/agents/:agent/edit`。`:agent` は `llmassign.Agents()` の名前。未知は 404）:

- 見出し: 「<Agent 名> の LLM」。説明に用途と「保存すると、次の呼び出しからすぐ使われます」。
- 欄: プライマリ・代替。**各 1 つの「モデル選択の欄」**（Issue #1011。[../architecture/model-manager/model-filter.md](../architecture/model-manager/model-filter.md) §8.3 の `settings-llm`）: 選んだプロバイダーとモデルをカードの要約で見せるボタン（「変更」）で、押すとモデル選択のモーダル（バー・詳細パネル・カードの一覧）が開き、カードを押すと `<role>.provider`・`<role>.model` の隠し `<input>` に入る。**既定の値を横に出す**（「既定: anthropic / claude-…」。`text-mute`）。候補は `llmmodels.Service.Candidates`（#988。API の一覧、取れなければ mam の既定候補）で、API キーのないプロバイダー・ログインしていないもの（#1013）は初期状態では隠し、「使えないものも表示」で理由と設定画面への導線つきで出す。保存済みの割り当てが使えないプロバイダーを指していても残し、欄の下に警告を出す（保存はできる）。一覧にない名前は `<role>.model_custom`（入れると優先）。割り当ての表の上には、使えないプロバイダーを指す割り当ての警告（`llmmodels.Service.Warnings`）を出す。
- ボタン: 「保存」（`PUT /settings/llm/agents/:agent`）、「この Agent を既定に戻す」（`DELETE /settings/llm/agents/:agent`。`hx-confirm` なし。モーダルの中の操作で、いつでもやり直せるため。§7 の質問 3）、「キャンセル」。
- 成功: 行を OOB で差し替え、閉じる。**行の要約が新しい値になる**（#1006 以降は保存した割り当てが実行中のクライアントにすぐ渡るので、行に「再起動で反映」の印は出さない）。422: 理由（`InvalidError.Message`。例「Writer（脚本） のプライマリ: モデル名を入力してください」）と入力を残す。画像・動画のモーダルの 422 は、理由が欄を名指すとき（`modelsettings.InvalidError.Field` が主モデル・代替モデル・テイク数）、その欄に `aria-invalid`・`aria-describedby`・`autofocus` を付けてフォーカスを移す（LLM のモーダルと同じ）。表の「編集」の列は右寄せで、2 つの表で同じ位置に揃える。409（`Pinned`）: 固定の注意。
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
  - 追加・編集のモーダルの保存は、カードの OOB（追加は一覧の末尾に足す）とトースト。名前の重複は 409、入力の誤りは 422 で、モーダルに理由と入力値（API キーを除く）。理由は問題ごとに 1 文の日本語（改行区切り。`localmodels` の `endpointError`。設定ファイルの検査の英語の文・`endpoints <名前>:` の前置き・生の `base_url` は出さない。問題の欄は `localcfg.EndpointError.Field`）。「入力中の内容で接続テスト」の結果を出したら、前の 422 の理由（`#mam-modal-error`）は `hx-swap-oob="delete"` で外す（成功と失敗を同時に出さない）。サブページで開いた編集（`?page=1`）は、サブページ先頭の概要のカード（削除ボタンとモデルへのリンクが無い形。接続先をここで削除すると開いているページが無くなるため）を置き換える。
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
| 7 | 概要・各分類の「再起動で反映」の印を、LLM の行ごとに出すか（保存済みで実行中のモデルと違うことを示す） | 出さない（#1006: LLM の割り当ては保存するとすぐ反映されるので、行の印は要らない。再起動が要る設定は、全分類の上の「再起動して反映」の欄が言う。当初は「保存した直後の行だけ出す」だった） |

## 8. ffmpeg（`/settings/ffmpeg`）

方式: **フォーム（2 欄）＋ 状態の表示**。モーダルにしない（欄が 2 つで、保存は 1 回。結果をその場で読ませたい）。出す条件: `Services.FFmpeg` がある。探し方と方針は [../architecture/ffmpeg-lookup.md](../architecture/ffmpeg-lookup.md)（Issue #856）。

- 見出し（`h2`）は「ffmpeg」。「?」ヘルプ: 何に使うか（Take のプレビュー・サムネイル・部分採用・Judge）・パスを空にしたときの探し方・保存したパスとダウンロードしたものは、すぐに使われる（再起動は要らない）こと。
- 欄（`<label>` つき、`type=text autocomplete=off`）: 「ffmpeg のパス」（`name=ffmpeg`）・「ffprobe のパス」（`name=ffprobe`）。空は「自動で探す」（`placeholder`）。保存済みの値が入る。
- 欄ごとの状態（`data-testid=ffmpeg-state-<欄>`）: 使えるときは `Badge`「使えます」とそのファイルのパス（指定があればそれ、無ければ探して見つけたもの）。使えないときは `Badge`（warn）「見つかりません」と理由（指定したパスが使えない: 絶対パスでない／実在する実行ファイルでない、または探した場所のどこにも無い）。
- 欄の下に「パスを空にしたときの探し方」として、探すディレクトリを順に並べる（`PATH` の各項目 → macOS は `/opt/homebrew/bin`・`/usr/local/bin` → **mam がダウンロードして入れた `ffmpeg` フォルダの `bin/`**（最後。入れた記録があり、ファイルがそろっているときだけ。[../architecture/ffmpeg-lookup.md](../architecture/ffmpeg-lookup.md) §1））。
- ボタン: 「保存」（`PUT /settings/ffmpeg`）。成功は `role="status"`「保存しました。すぐに使われます（mam を再起動する必要はありません）」（両方空にしたときは「パスの指定を解除しました。すぐに、自動で探した ffmpeg に切り替わります」）。**パスが使えないときは 422 で書かず**、`role="alert"` に「ffmpeg のパス（…）を保存できませんでした。<理由>」を出し、入力を残す（探し直しに切り替えない。ffmpeg-lookup.md §2）。保存の失敗は 500。
- 設定ファイル（`ffmpeg.json`）が読めないときは、分類の `role="alert"` の 1 文だけを出す（ほかの分類は使える）。概要のカードは「ffmpeg・ffprobe を使えます」（ok）／「ffmpeg が見つかりません」などの警告（warn）／「設定ファイルを読めません」。
- **反映は保存のすぐあと**（Issue #1047）。保存・ダウンロード・削除のあとに、mam は ffmpeg を探し直して全サービスの `media.Tools` を入れ替える。「使えます」は、Judge・プレビュー・Export がいま動かせるものと一致する（表示した探索の結果がサービスの持つものと違えば、その場で入れ替える。[../architecture/ffmpeg-lookup.md](../architecture/ffmpeg-lookup.md) §3）。「再起動して反映」の欄には出ない（[../architecture/restart.md](../architecture/restart.md) §2）。
- **ダウンロードの部品（Issue #900）**: フォームの下に、`Services.FFmpegDownload` があり、この環境（Windows x64・macOS arm64）で取得できるときだけ `DownloadPanel`（`#ffmpeg-download`）を出す。見出し「mam でダウンロードして入れる」と状態（`Badge`: 未取得／入れてあります／ダウンロード中／壊れています）、バージョン。ボタンの前に、**ビルド名・配布元・大きさ（取得と展開後）・SHA-256 で確かめて一致しなければ削除すること・ライセンス（リンク）・この版のソース（リンク）・置き場所・「ボタンを押したときだけ通信します（自動の更新確認はしません）」**を書く。別の場所の ffmpeg が見つかっているときは、そちらが優先されるのでダウンロードは要らない、と添える。ボタン「ffmpeg をダウンロードして入れる」（新しい版があるときは「新しい版をダウンロード」と、入れてある版は使えるという注意）。進行中は `<progress>`・段階の文・「中断」、2 秒ごとに取り直す。中断は失敗として赤く出さない。成功は「mam を一度終了して、起動し直してください」。入れてあれば「削除」（確認つき）。探すディレクトリの一覧の最後が、入れたもの。ルートは [../api/endpoints-settings.md](../api/endpoints-settings.md) §5.2.1、方針は [../architecture/ffmpeg-download.md](../architecture/ffmpeg-download.md)。

## 9. 音声・口パク（`/settings/audio`）

方式: **1 つのフォーム（声と口パクの見出しごとに、プロバイダーのカードを選ぶ形。API キーと同じカード・2 列）＋ 状態の表示 ＋ 長い説明はカードの「詳細」のモーダル（読み取りだけ）**（Issue #1005）。選ぶのは 3 つで、保存は 1 回（モーダルでは保存しない）。出す条件: `Services.Audio` がある。仕組みと規則は [../architecture/model-adapters-m3.md](../architecture/model-adapters-m3.md) §14.10（Issue #924）。部品は既存の `SettingsCard`（`CardItem`。印 `Marks`・`Class` を足した）・`Badge`・`Choice`・`Button`・`ButtonLink`・`modal` で、新しい色・影は足さない。

- 見出し（`h2`）は「音声・口パク」。「?」ヘルプ: 声と口パクをクラウドかローカルかで選ぶこと・何も選ばないときはクラウドであること・ローカルは Windows + NVIDIA の GPU だけで、mam が自動で導入・起動すること・保存した内容は次の音声作成から使われること。
- **声**（見出し `h3`「声（セリフ・ナレーション）」。`ul[data-testid=audio-voice_provider]`）と**口パク**（見出し「口パク」。`audio-lip_sync_provider`）。それぞれの見出しの下に、**プロバイダーごとのカード**（`data-testid=audio-card`・`data-field`・`data-which`＝`cloud` / `local`）を 2 列で並べる: クラウド（「ElevenLabs（クラウド）」/「Sync Labs（クラウド）」）とローカル（「GPT-SoVITS（この PC）」/「MuseTalk 1.5（この PC）」。ローカルのランタイムが無いアプリではカードを出さない）。見出しの説明（それぞれ何のためか）は見出しの「?」。
- **選択中が分かる**: 各カードに、本物のラジオ（`name=voice_provider` / `lip_sync_provider`。`value=elevenlabs` / `synclabs` / `local`。見えなくしたまま、`data-testid=audio-<field>-cloud` / `-local`）を、ボタンの形の `<label>`「選ぶ」／選んだら「✓ 選択中」で出す。**選ばれたカードは枠が `has-[:checked]:border-accent` になる**（色だけにせず、文字「選択中」も出す）。何も保存していないときはクラウドが選択中。キーボード（矢印・Space）・フォーム・スクリーンリーダーは本物のラジオのまま動く。
- **クラウドのカード**: 状態の `Badge`「API キー設定済み」／「API キー未設定」（warn）。本文に API キー（`elevenlabs` / `synclabs`）の設定の有無（値は出さない）。未設定のときは「API キーとログイン」（`/settings/api-keys`）へのリンクと、ボタン「API キーを設定」。「詳細」ボタン。
- **ローカルのカード**: 状態の `Badge`（導入済み／未導入／重みが未取得／導入中／起動しています／起動中／起動に失敗／**この PC では使えません**。`audiosettings.State`。#918・#920 のランタイムの状態）。**印「商用注意」**（`Card.Marks`）を、`Condition`（商用利用の条件）があるとき、または商用利用が確認できていない（不可・不明）重み・依存ファイルがあるときに出す。保存済みがローカルなのにこの PC で動かせないときと、はっきりしない依存があるときは、1 行（`role="status"` など）もカードに出す。ボタン「モデルマネージャーを開く」（`/model-manager`。導入・重みの取得・起動・停止は、そこの「ローカル音声・口パク」の区画）と「詳細」。
- **「詳細」のモーダル**（`GET /settings/audio/:choice/:provider`。`:choice` は `voice` / `lip-sync`、`:provider` は `cloud` / `local`。HTMX でなければ `/settings/audio` のページ全体。知らない `:choice`・`:provider`・ローカルのランタイムが無いアプリの `local`・読めない設定ファイルは 404 のモーダル「対象が見つかりません」）。クラウド: API キーの有無と案内・声のカードの注記。ローカル: 今の状態の `Badge`・使えない理由（`Reason`。使えるときは出さない。実行の失敗と**同じ文**）・サーバーが起動しなかったときのログの末尾・必要なビデオメモリ（声は mam の推定のときは「mam の推定です。実機では確かめていません」）・**出典のリンクとライセンス**（プログラムの入手元・ライセンス。重みと依存ファイルの出典・ライセンスはモデルマネージャーの一覧）・`Condition`（商用利用の条件）・商用利用がはっきりしない重み・依存ファイルの件数と名前（枠つきの注記。`role="note"`。商用の動画に使う前にモデルマネージャーの一覧で出典とライセンスを確認する案内）・「モデルマネージャーを開く」・声のカードの注記。
- **この PC で動かせないとき**（macOS・Linux・NVIDIA の GPU 無し・ビデオメモリ不足・RTX 50。`Support.Supported == false`）: ローカルのラジオは `disabled`（カードのボタンは「選べません」）。カードの `Badge` は「この PC では使えません」。詳細に、枠つきの注記（`role="note"`）で「この PC では GPT-SoVITS を選べません。ElevenLabs（クラウド） のまま使えます」と、**ランタイム管理の理由と代わりのクラウドの案内**（macOS は「公式の PyTorch 版が使えない」など。#918・#921 の文）を出す。クラウドのラジオがチェック済みで、保存するとクラウドになる。保存済みの設定がローカルのままのとき（別の PC からコピーされたファイル）は、「保存済みの設定はローカルですが、この PC では動かせないため、今は使われません（保存するとクラウドに切り替わります）」をカードにも詳細にも出す。
- **チェック「ローカルが使えないときは、クラウドに切り替える（API 費用が出ます）」**（`name=fallback_to_cloud` `value=1`。**既定はオフ**）。説明: 実行を始める前の検査で、ローカルが使えない（この PC で動かせない・導入されていない・重みが無い・参照音声が無い話者がいる）と分かったときだけ、その実行の全体をクラウドで作る。実行の途中で切り替えることはなく、切り替えたときはチャットに書く。オフのときは、理由を出して止まる（クラウドの費用は出ない）。
- **Irodori-TTS（#1015）**: 声のカードは 3 枚（`data-which`＝`cloud` / `local`〔GPT-SoVITS〕/ `irodori`）。ラジオの `value` は `elevenlabs` / `local` / `irodori`、`data-testid` は `audio-voice_provider-irodori`。ラベルは「Irodori-TTS（この PC。日本語専用）」。詳細は `GET /settings/audio/voice/irodori`（ローカルのランタイムが無いアプリでは 404）。詳細には、使うモデル（「使うモデル: …（変えるにはモデルマネージャーの「モデルの選択」）」。`audio-model-voice_provider`）と、そのモデルの**使うときの制限**（モデルカードの倫理上の制限。`audio-cautions-voice_provider`。琥珀色の枠）を出す。声の見出しの下には、**声のコピーの注記**（`audio-clone-notice-voice_provider`。`role="note"`。「本人の同意を得た音声だけ…実在の人の声をコピーしたりなりすましに使ったり、人をだます合成音声・ディープフェイクに使うことは、Irodori-TTS のモデルカードで禁じられています」）を出す。概要（`/settings`）の「声」の行は、選んだ Irodori-TTS の名前を出す。使うモデルの選択はモデルマネージャー（local-runtime.md §14.5）。
- 声の注記（詳細に出す）: ローカルの声は Character に設定した参照音声で話す。プロジェクトの「声のサンプルから ElevenLabs でクローン」は ElevenLabs の声で、ローカルの声には使われない（どちらがどのプロバイダーの声かを混同させない）。
- ボタン: 「保存」（`PUT /settings/audio`。`hx-disabled-elt`・`hx-sync="this:drop"` の二重送信の防止）。成功は `role="status"`「保存しました。次の音声作成から使われます」。この PC で動かせないローカルを選んで送られたときは 422 で書かず、`role="alert"` に理由（ランタイム管理の文）を出す（フォームは保存済みの値のまま）。
- 設定ファイル（`audio.json`）が読めないときは、分類の `role="alert"` の 1 文だけを出す（ほかの分類は使える）。保存は 409 で、ファイルを書き換えない。概要のカードは warn「設定ファイルを読めません」。
- 反映: 保存した選択は**次の音声作成から**（再起動は要らない）。
- **MCP**: 同じ内容を `get_audio_providers`・`set_audio_providers` で読み書きする（[../api/mcp-tools-settings-policy.md](../api/mcp-tools-settings-policy.md)）。

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
| 0.9 | 2026-10-25 | §3 に画像・動画モーダルの 422 のフォーカス（欄を名指すとき）と「編集」の列の位置を、§5 に接続先のモーダルの 422 の文（日本語・問題ごとに 1 文）と接続テストの結果が前の 422 を外すことを足した | 最終の実画面の確認の指摘 |
| 0.10 | 2026-10-28 | 「説明の出し方」を足した。各分類の説明・区画とカードの説明・LLM の再起動の補足と `llm-chatgpt-note` を「?」ヘルプへ移した（画像・動画の `models.json` の注記は分類のヘルプへ） | Issue #866。画面の文字を減らす |
| 0.11 | 2026-11-01 | §8（ffmpeg）を足した。ffmpeg と ffprobe のパスを指定する分類（`/settings/ffmpeg`）。指定したパスが使えないときは 422 で書かず、探し直さない | Issue #856 |
| 0.12 | 2026-11-01 | §8（ffmpeg）に、ダウンロードの部品（ボタン・進み具合・中断・削除。取得元・検証・ライセンスの表示）を足した | Issue #900 |
| 0.13 | 2026-11-01 | 状態の行（冒頭）に §8（ffmpeg）の実装済みを足し、§8 の「探し方」の順の最後に、ダウンロードして入れた `ffmpeg` フォルダの `bin/` を足した（実装は `ffmpegsettings.Look` の `SearchDirs`） | PR #911 のレビューの指摘 |
| 0.14 | 2026-11-01 | §9（音声・口パク）を足した。声と口パクのプロバイダーの選択、ローカルの状態・この PC で動かせない理由・出典とライセンス・商用利用がはっきりしない依存の注記、クラウドの API キーの有無、「ローカルが使えないときはクラウドに切り替える」（既定オフ）。`PUT /settings/audio` | Issue #924（#912 声と口パクのローカル化） |
| 0.15 | 2026-11-03 | §2 のモーダルのモデル名を、プロバイダーごとの候補のプルダウンにした（API の一覧、取れなければ mam の既定候補。出所の表示、一覧にない保存済みモデルの警告、一覧にない名前の入力欄） | Issue #988 |
| 0.16 | 2026-11-03 | §1.3（ChatGPT でログイン）と §9（音声・口パク）を、API キーと同じカード（2 列。名前・説明・状態の `Badge`・印・主なボタン）にそろえた。長い説明・注意・トークンの期限・リスク・商用利用の条件・出典とライセンスは「詳細」のモーダルへ移した。「非公式」「商用注意」の印をカードに常に出す。音声・口パクはプロバイダーごとのカードを選ぶ形（選択中は枠と文字）。`GET /settings/chatgpt/:kind/detail`・`GET /settings/audio/:choice/:provider` を足した | Issue #1005 |
| 0.17 | 2026-11-03 | §9 に Irodori-TTS のカード（ラジオ値 `irodori`・詳細・使うモデルと制限・声のコピーの注記）を足した | Issue #1015 |
| 0.18 | 2026-11-03 | §2 に、ローカル LLM サーバーのカードを足した | Issue #1008 |
| 0.19 | 2026-11-04 | §2 の LLM のモデル欄を、プロバイダーとモデルを一緒に選ぶモデル選択の欄にした。使えないプロバイダーを指す割り当ての警告を足した | Issue #1011・#1013 |
| 0.20 | 2026-11-04 | §2 のローカル LLM サーバーのカードに「この PC の llama.cpp を使う」を足した | Issue #1008（後半） |
| 0.21 | 2026-11-04 | §1.1 の表に `topaz`（Topaz Video API。接続テストは `GET /account/v1/credits/balance`）を足した | Issue #1031 |
| 0.22 | 2026-11-06 | §8 の ffmpeg の反映を、次の起動から → 保存のすぐあと（再起動の欄に出さない）に改めた。概要カードの「使えます」はサービスが動かせるものと一致する | Issue #1047 |
