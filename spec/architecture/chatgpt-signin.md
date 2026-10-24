# ChatGPT のサブスクでログインする（Sign in with ChatGPT）

ChatGPT のサブスク（Plus / Pro など）を持つ利用者が、API キーなしで mam のテキスト生成と画像生成を使えるようにする設計と、確認できたこと・できていないこと。Issue #832。LLM の割り当ては `spec/architecture/model-adapters.md` §2.1、識別子は同 §6、セキュリティは `spec/requirements/non-functional.md` §5、MCP ツールは `spec/api/mcp-tools-chatgpt.md`。

## 1. 決定: 2 つの別々のログイン

OpenAI の 2 つのクライアントは**トークンを使い回せない**ため、ログインを 2 系統に分け、保存も別にする（各 1 アカウント）。

| 系統 | 用途 | クライアント | 経路 | 既定 |
|------|------|-------------|------|------|
| **ChatGPT サブスク（公式）**（`official`） | テキスト生成（Reasoning LLM のプロバイダー `chatgpt-subscription`） | 公式の動的登録（`dynamic_agent_client` で初回登録し、発行された `oaiapp_…` を保存して以後使う）。公式の「ChatGPT plan usage」（オープンソース・ローカル向け） | 公式の Responses API `https://api.openai.com/v1/responses` | ログインすれば使える（LLM の割り当てでプロバイダーを選ぶ） |
| **ChatGPT 画像生成（非公式）**（`image`） | 画像の用途のモデル `gpt-image-2:chatgpt-subscription` | Codex CLI の公開クライアント `app_EMoamEEZ73f0CkXaXp7hrann`（`codex-rs/login`、`imagine-cli` と同じ） | `https://chatgpt.com/backend-api/codex/responses` の `image_generation` ツール | **OFF**。ON にするときにリスクを表示して確認を取り、ON のときだけログインを始められる |

- テキスト生成にはコーディング用の非公式経路を使わない（オーナー決定）。画像生成だけが非公式で、使う前に別のログインとリスクの確認が要る。
- 動画・音声にサブスクを使うことと、複数アカウントの切り替えは対象外。プランの利用量は表示しない（API がない。公式が認める「Manage usage」リンクと「Using ChatGPT plan」の表示だけ置く）。

## 2. 確認の結果（Issue の「やること 1」の要確認 3 点）

調査日 2026-10-24。

| # | 確認すること | 結果 | 根拠 |
|---|-------------|------|------|
| 1 | デバイスコードは公式の動的登録クライアントでも使えるか | **使えない（確認済み）**。リモート（tailnet・MCP）の利用者は、mam が動いている PC でログインする | 公式ドキュメント（sign-in・profiles-and-sessions・self-hosted-vms）にデバイスコードの記載がない。`POST https://auth.openai.com/api/accounts/deviceauth/usercode` に `{"client_id":"dynamic_agent_client"}` を送ると HTTP 400 `invalid_client`（"This app is unavailable"）。Codex の公開クライアントでは HTTP 200（`device_auth_id`・`user_code`・`interval:"5"`・有効期間約 15 分）。`/.well-known/openid-configuration` の `grant_types_supported` は `authorization_code`・`refresh_token` だけ（認証なしの探査）。**発行済みの `oaiapp_…` が通るかは未検証** |
| 2 | 公式のログインで得たトークンで `chatgpt.com/backend-api/codex/responses` を呼べるか | **未検証（実機の対話ログインが必要）**。呼べない見込みなので、画像は別ログインにした | 公式 models-and-inference は「`backend-api` を指さない」と明記。アクセストークンの `aud` は `https://api.openai.com/v1`。`ChatGPT-Account-Id` は Codex の ID トークンの `chatgpt_account_id` から取るが、公式の ID トークンが持つかは不明 |
| 3 | Codex のクライアントで得たトークンで公式の `https://api.openai.com/v1/responses` を呼べるか | **未検証**。呼べない見込みなので、テキストは別ログインにした | 公式 sign-in §4「ID トークンだけではプランの利用は認可されない。`chatgpt.tokens.use.direct` を確認」。この PC の Codex のアクセストークン（Plus）の主張を長さ・主張だけ読んだ: `aud=['https://api.openai.com/v1']`、`scp=['openid','profile','email','offline_access']`（`resource.invoke` と `chatgpt.tokens.use.direct` がない）、`client_id=app_EMoamEEZ…`。オーナーに 1〜2 回の呼び出しを許可されたが、**そのトークンは失効済み（`exp` が約 49 日前）で、更新するとリフレッシュトークンが回転するため、呼び出しはしなかった** |

結論: 1 は事実で、2・3 は「使い回せない」を前提に 2 系統にした。2・3 と下の `text.format` は、対話ログインのできる環境で下記の手順で確かめ、確かめたら `<日付> <確かめた人>` を付けてこの表を更新する。

- 2・3: 各系統でログインし、`curl -H "Authorization: Bearer $ACCESS_TOKEN"` で最小のプロンプトを、`https://api.openai.com/v1/responses`（公式のトークン・Codex のトークンの両方）と `https://chatgpt.com/backend-api/codex/responses`（両方）に 1 回ずつ送り、HTTP ステータスと `error.code` だけを記録する（計 4 回）。
- `text.format`: 公式のトークンで `text:{format:{type:"json_object"}}` を付けて 1 回送る。通るまで、JSON は `instructions` で言葉で求め、既存の `CompleteJSON` の検証で受ける。

## 3. ログイン

### 3.1 ブラウザ（Authorization Code + PKCE）

1. `state`・`nonce`・PKCE の verifier を**試行ごとに**作る（`oauth.NewAttempt`）。
2. ループバック `http://127.0.0.1:<port>/auth/callback` のリスナーを**先に**立てる。ポートは 1455、使用中なら 1457（Codex の許可リストはこの 2 つ）、公式はさらに空きポート。ホストは `localhost` にしない。
3. 既定ブラウザを開く（`app.Options.OpenBrowser`: ブラウザ版は `cmd/server/browser.go`、Wails 版は `application.Browser.OpenURL`。どちらも**既定ブラウザ**で、Wails のウィンドウの中では開かない）。開けなければ画面に認可 URL を出す。
4. `state` の合わないコールバックは 400 で捨て、試行は終わらせない。`Host` が `127.0.0.1` でない要求も捨てる。
5. コードを交換する（公式: フォーム＋`resource`、Codex: フォーム）。公式は ID トークンを JWKS（`https://auth.openai.com/.well-known/jwks.json`、`github.com/golang-jwt/jwt/v5` + 自前の JWKS 取得）で検証する: 署名（RS256）・`iss`・`aud`（発行された `client_id`）・`exp`・`nonce`。初回登録のコールバックが発行 ID を返さない、または `dynamic_agent_client` のままなら登録は未完了として捨てる。再ログインで別の `client_id` が返ったら捨てる。別アカウント（`sub` が違う）のトークンで上書きしない。
6. 認可 URL には `id_token_hint` を**付けない**（ID トークンを URL・MCP の応答に出さないため）。保存したメールを `login_hint` に付ける（アカウント選択画面は出る）。

### 3.2 デバイスコード（Codex のクライアントだけ）

`POST …/api/accounts/deviceauth/usercode` → ユーザーが `https://auth.openai.com/codex/device` でコードを入力 → `…/deviceauth/token` を応答の `interval` ごとにポーリング（403・404 は待機、期限 15 分で終了、ネットワーク・5xx が 5 回続いたら中止）→ 得た `authorization_code`・`code_verifier` を `redirect_uri=https://auth.openai.com/deviceauth/callback` で交換。画面と MCP には確認 URL・コード・期限・間隔を出し、「自分で始めたときだけ入力する」注意を添える。公式の系統は `ErrNoDeviceLogin`（遠隔の利用者は mam の PC でログインする）。

## 4. トークン

- **保存**: `credential.Store`（OS のセキュアストレージ）に**項目ごと**（`access`・`refresh`・`id_token`・`meta`・`registration`）に別のエントリで保存する。アカウント名は `api-key:chatgpt/<official|image>/<項目>`。1 つの JSON にまとめない。2000 バイトを超える項目は 1800 バイト以下の断片（`<キー>/<n>`）と個数のマーカー（`mam-chunks:<n>`）に透過的に分け、小さくなったら余った断片を消す。1 項目の上限 64 KiB（API キーの `MaxAPIKeyBytes` とは別）。測定: Codex の ID トークン 1704・アクセス 1657・リフレッシュ 211 文字（合計 3572 > Windows の 2560 バイト）。公式のトークンの長さは未測定（不透明な `encrypted_auth_metadata` を持つので Codex 以上と見る）。
- 書き込み順は「リフレッシュ → ID → アクセス → メタ」。メタ（期限・メール・プラン・スコープ・再ログイン要否）は秘密を含まない。断片が欠けた保存は「未ログイン」として扱う。
- `service.CredentialProviders`（キー画面の固定の一覧）にトークンの項目は載せない。設定ファイル・データディレクトリ・ログ・画面・API の応答・エラーには値を出さない（`chatgpt.Credentials` は `%v`・`%#v`・slog で伏せ字、エラーは HTTP ステータスとエラーコードだけ、URL は落とす）。残るのは `chatgpt.json`（データディレクトリ）の画像生成の ON/OFF だけ。
- **更新**: 有効期限の 5 分前から（公式のアクセストークンは 1 時間、リフレッシュは 30 日で更新のたびに回転。Codex は 10 日）。同一プロセスは mutex、プロセス間は**ブロックするロックファイル** `os.UserConfigDir()/mam/.auth-refresh.lock`（`fsutil.LockFileBlocking`: Unix は `flock`、Windows は `LockFileEx`）。ロックを取ってからトークンを**読み直し**、まだ期限が近いときだけ更新する（OS のストレージは全データディレクトリで共有されるので、`--data-dir` を変えた複数起動でも更新は 1 回）。更新で新しいアクセス・リフレッシュ・期限・スコープを一緒に保存し、応答にない項目（ID トークン）は残す。公式はフォーム＋`resource`・`scope` なし、Codex は JSON。
- **再ログインが要る**: 更新が `invalid_grant`・`invalid_refresh_token`・`token_expired`・`refresh_token_expired`・`refresh_token_invalidated`・`refresh_token_reused`（HTTP 4xx）で拒まれたとき。トークンは残し、メタに印を付けて画面・MCP に「再ログインが必要」と出し、その経路は呼ばない。ネットワーク断・5xx・未知の 4xx ではトークンを消さず印も付けない。期限内のトークンは、更新に失敗しても使い続ける。公式のログインで `chatgpt.tokens.use.direct` が付与されなかったときは「プランの利用が未許可」（`ErrPlanUsageNotGranted`）として再ログインを促す。
- **ログアウト**: リフレッシュトークンを失効させてから（公式: 設定ドキュメントの `revocation_endpoint` へフォーム `token`・`token_type_hint=refresh_token`・`client_id`（発行された ID）。自ホスト以外の URL は信じない。Codex: `https://auth.openai.com/oauth/revoke` へ JSON）、ネットワーク・5xx は 3 回まで再試行し、**失効を確認できなくてもローカルのトークンは消す**（画面と MCP は「失効を確認できなかったので ChatGPT の設定で接続を解除して」と伝える）。公式の `registration`（ホスト ID `urn:uuid:…`・発行された `client_id`）は残す。

## 5. 使い道

### 5.1 テキスト生成（`internal/agent/llm/chatgptsub`）

- プロバイダー `chatgpt-subscription`（`llm.Providers()` に追加。設定画面の「LLM の割り当て」で Agent の primary / fallback に選べる）。API キーではなくログインを使う。モデル名は固定せず、`GET https://api.openai.com/v1/models`（同じアクセストークン。応答は `models[]`、`visibility=="list"` の `slug`）を設定画面の「利用できるモデルを表示」で出し、利用者が割り当てに入力する。
- リクエストは公式のプレビューの制限に合わせる: `store:false`・`stream:true`・`input` は配列・システムプロンプトは `instructions`（`role:"system"` は使わない）・`max_output_tokens`・`temperature` など非対応の項目は送らない。JSON は `text.format` を使わず、`instructions` で言葉で求め、`CompleteJSON` の検証で受ける。画像入力は `input_image`（Art Director・Video Judge・Editor など。モデルが対応していれば使える）。
- 成功は `response.completed` だけ。`response.failed`・`response.incomplete`・途中で切れたストリームは別のエラー。401 は更新で確かめてから 1 回だけ再試行する。
- エラー（公式 errors-and-recovery。網羅ではない）: `subscription_sharing_usage_limit_exceeded`（429）・`subscription_sharing_user_not_eligible`（403）は**再試行せず**、サーキットブレーカーにも数えず代替へ回す。理由と `https://chatgpt.com/settings/usage` のリンクをチャットに出す（`llm.WithFallbackNotice`。Orchestrator のジョブが Project のチャットに 1 実行 1 回出す。代替も失敗したときは失敗の文面に同じ理由が入る）。`…_usage_unavailable`・`…_user_unavailable`（503）と 5xx は指数バックオフで再試行（再試行・ブレーカー・代替は §2.1 と同じ）。`…_unsupported_capability`（400）・`…_route_not_supported`（403）・`chatpass_v2_*`（403）・未知の 4xx は再試行しない。未ログイン・再ログイン要・プラン未許可はその理由を通知して代替へ。
- 画面に出す文言は公式の UI/UX 規定に従う: ボタンは **Continue with ChatGPT**、選択中は **Using ChatGPT plan** と **Manage usage**（`https://chatgpt.com/settings/usage`）。

### 5.2 画像生成（非公式、`internal/agent/modeladapter/chatgptimage`）

- 識別子 `gpt-image-2:chatgpt-subscription`（`take.model_name`・`cost_ledger_entry.provider`。API キー経由の `gpt-image-2` と区別できる。`model_name` / `provider` の列に制約はなくマイグレーション不要）。見積もりコスト 0 USD で、台帳には 0 円の行を 1 件残す。#831 の開放集合の仕組み（`domain/modelreg`）に載せる: `modeladapter/chatgptid` が識別子・ベンダー名（`chatgpt-subscription`: ブレーカー・同時実行数 1・タイムアウト 600 秒は専用）・Codex に渡すモデル名 `gpt-image-2` を持ち、`localbackends.Options.ChatGPTImage` のバックエンドを `Build` が `Image[識別子]` に載せる（再読み込みのたびに付け直す）。選択肢（`domain.ImageModels()`）に載るのは**スイッチが ON のあいだだけ**で（`Options.Publish`。スイッチを切り替えると `chatgpt.Service.SetOnImageChange` で一覧を作り直す）、設定画面からは OFF のとき選べない。設定ファイルにすでにある割り当ては OFF でも検証を通り、実行時に `ErrImageDisabled`（外部へ送らない）で失敗して代替モデルに回る。参照画像は受け付ける（`AcceptsImageInputs`）。
- `POST https://chatgpt.com/backend-api/codex/responses`: `model`（Codex のドライバー、既定 `gpt-5.5`）、`instructions`（ツールを確実に呼ばせる最小の文）、`input[{role:"user", content:[input_text, input_image…]}]`、`tools:[{type:"image_generation", model:"gpt-image-2", size, output_format}]`、`tool_choice`、`stream:true`、`store:false`。ヘッダー: `Authorization`、`ChatGPT-Account-Id`（ID トークンの `chatgpt_account_id`）、`OpenAI-Beta: responses=experimental`、`originator: codex_cli_rs`、`session_id`。結果は SSE の `response.output_item.done` の `image_generation_call.result`（base64）。無音が 180 秒続いたら中止。参照画像は `input_image`（`AcceptsImageInputs` は真）。
- **OFF（既定）のときは一切呼ばない**: トークンの読み取りも更新もログインの開始もしない（呼び出しのたびに先に ON/OFF を見る）。ON にするには、設定画面でリスクを見て確認のチェックを入れる／MCP は `acknowledge_risks: true`。OFF にするのに確認は要らず、ON にするたびに確認を取り直す。OFF にしてもログインは残る（ログアウトで消す）。
- 表示するリスク: ① OpenAI の公開 API ではない非公式の経路で、予告なく使えなくなることがある ② 公式のプレビューの制限では画像生成はサポート外（Unsupported tools） ③ ChatGPT のプランの利用上限を消費する。

## 6. 画面と MCP

- 設定画面（`GET /settings`）の「ChatGPT でログイン」: 公式（状態・メール・プラン・期限・「再ログインが必要」・ログアウト・モデル一覧）と、画像生成（非公式）のリスク・ON/OFF・ブラウザ／デバイスコードのログイン。ログイン中は区画が 2 秒ごとに自分を取り直す。ルートは `spec/api/endpoints.md`。
- MCP ツール 6 つ（`spec/api/mcp-tools-chatgpt.md`）: ログイン開始・状態の取得・ログアウト・画像生成の ON/OFF。トークンはどの応答にも載せない。ブラウザのログインはコールバックが mam の PC の `127.0.0.1` に来るため、リモートの利用者は mam の PC のブラウザで URL を開く（公式にデバイスコードはない）。画像生成のログインだけデバイスコードで遠隔から行える。

## 7. 注意

- 公式の「ChatGPT plan usage」はオープンソース・ローカルホストのアプリ向けで、有料の・リモートで配る製品は OpenAI の窓口への申請が必要とされる。mam の配布形態がこの条件に当たるかは、ライセンス・製品の判断（オーナー）。
- Windows の実機（Credential Manager・Wails の既定ブラウザ）での動作は未確認（Linux の CI と、Windows 向けのビルドとモックの上限 2560 バイトで確かめた）。
- 公式のログインの実機確認（初回登録の画面・発行される `client_id`・トークンの実際の長さ・`text.format`）は、利用者の ChatGPT アカウントでの対話ログインが要り、未実施。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。ログイン 2 系統（公式・非公式の画像）の決定、要確認 3 点の結果（1 は確認済み、2・3 と `text.format` は未検証）、ブラウザ・デバイスコード、トークンの保存・更新・ログアウト、テキスト・画像の使い道、画面と MCP | Issue #832。ChatGPT のサブスクでログインしてテキスト生成と画像生成に使えるようにする |
| 1.1 | 2026-10-24 | §5.2: 画像モデルを #831 の開放集合（`modelreg`）に載せた。スイッチ ON のあいだだけ選択肢に出し、OFF では呼ばずに代替へ回る | Issue #832（#831 の取り込み後） |
