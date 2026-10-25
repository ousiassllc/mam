# ChatGPT のサブスクでログインする（Sign in with ChatGPT）

ChatGPT のサブスク（Plus / Pro など）を持つ利用者が、API キーなしで mam のテキスト生成と画像生成を使えるようにする設計と、確認できたこと・できていないこと。Issue #832。LLM の割り当ては `spec/architecture/model-adapters.md` §2.1、識別子は同 §6、セキュリティは `spec/requirements/non-functional.md` §5、MCP ツールは `spec/api/mcp-tools-chatgpt.md`。

## 1. 決定: 3 つの別々のログイン

OpenAI の 2 つのクライアントは**トークンを使い回せない**ため、ログインを分け、保存も別にする（各 1 アカウント）。3 つ目の Codex 経由（`codex`、§8）は、Codex の app-server が自分でログインして自分で保存するので、mam はトークンを持たない。

| 系統 | 用途 | クライアント | 経路 | 既定 |
|------|------|-------------|------|------|
| **ChatGPT サブスク（公式）**（`official`） | テキスト生成（Reasoning LLM のプロバイダー `chatgpt-subscription`） | 公式の動的登録（`dynamic_agent_client` で初回登録し、発行された `oaiapp_…` を保存して以後使う）。公式の「ChatGPT plan usage」（オープンソース・ローカル向け） | 公式の Responses API `https://api.openai.com/v1/responses` | ログインすれば使える（LLM の割り当てでプロバイダーを選ぶ） |
| **ChatGPT 画像生成（非公式）**（`image`） | 画像の用途のモデル `gpt-image-2:chatgpt-subscription` | Codex CLI の公開クライアント `app_EMoamEEZ73f0CkXaXp7hrann`（`codex-rs/login`、`imagine-cli` と同じ） | `https://chatgpt.com/backend-api/codex/responses` の `image_generation` ツール | **OFF**。ON にするときにリスクを表示して確認を取り、ON のときだけログインを始められる |
| **ChatGPT 経由の Codex**（`codex`。§8） | テキスト生成のみ（プロバイダー `chatgpt-codex`）。Business / Enterprise / Edu のワークスペースのアカウントも使える | Codex の app-server（OpenAI 公式。公式のリリースから実行時に取得、または利用者が入れた codex のパス）。ログインは app-server が行う | app-server の JSON-RPC（標準入出力）。トークンは mam が読まない | 既定の `routes` には入れない。設定画面で方式を「Codex 経由」にして、LLM の割り当てで選ぶ |

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

- 設定画面（`GET /settings`）の「ChatGPT でログイン」: 方式の切り替え（公式／Codex 経由。Codex 経由は §8.3）。公式（状態・メール・プラン・期限・「再ログインが必要」・ログアウト・モデル一覧）と、画像生成（非公式）のリスク・ON/OFF・ブラウザ／デバイスコードのログイン。ログイン中は区画が 2 秒ごとに自分を取り直す。ルートは `spec/api/endpoints.md`。
- MCP ツール 8 つ（`spec/api/mcp-tools-chatgpt.md`）: ログイン開始・状態の取得・ログアウト・画像生成の ON/OFF・Codex のエンジンの取得とログイン。トークンはどの応答にも載せない。ブラウザのログインはコールバックが mam の PC の `127.0.0.1` に来るため、リモートの利用者は mam の PC のブラウザで URL を開く（公式にデバイスコードはない）。画像生成のログインだけデバイスコードで遠隔から行える。

## 7. 注意

- 公式の「ChatGPT plan usage」はオープンソース・ローカルホストのアプリ向けで、有料の・リモートで配る製品は OpenAI の窓口への申請が必要とされる。mam の配布形態がこの条件に当たるかは、ライセンス・製品の判断（オーナー）。
- Windows の実機（Credential Manager・Wails の既定ブラウザ）での動作は未確認（Linux の CI と、Windows 向けのビルドとモックの上限 2560 バイトで確かめた）。
- 公式のログインの実機確認（初回登録の画面・発行される `client_id`・トークンの実際の長さ・`text.format`）は、利用者の ChatGPT アカウントでの対話ログインが要り、未実施。

## 8. 3 つ目の系統: Codex 経由（Issue #845）

### 8.1 なぜ要るか

ChatGPT Business / Enterprise / Edu のワークスペースのアカウントは、公式の動的登録（§1 の `official`）では**プランを使えない**。サインインそのものは通るが、組織の Admin Console の「外部アクセス」（ID・ChatGPT 広告・連携アプリの 3 項目だけで、プランの利用を許可する項目は無い）に許可の設定が無く、Responses API 側が `3p_delegated_access_policy_denied` / `subscription_sharing_user_not_eligible` で拒む。そこで、OpenAI 自身のクライアントである **Codex の app-server** にログインとリクエストを任せ、同じアカウントを**テキスト生成だけ**に使う。画像・動画・音声には使わない（オーナー決定）。

### 8.2 決定事項（Issue #845。オーナーが推奨を選んだもの）

1. **エンジン**: 既定は公式の GitHub リリース `rust-v0.164.0` の `codex-app-server-<arch>-pc-windows-msvc.exe.zip`（amd64 / arm64。約 88 MB・展開後約 270 MB）を実行時に取得し、zip と展開後の実行ファイルの SHA-256 とサイズを検証して、`<mam.exe のディレクトリ>\codex\` に置く（`installed.json` に版と来歴を記録。インストーラー版はアンインストールで一緒に消える）。取得は Windows 版のみ。利用者が自分で入れた codex の絶対パスを設定画面で指定してもよい（他の OS・ブラウザ版はこれだけ）。mam は Codex を再配布しない（`THIRD_PARTY_NOTICES.md`）。
2. **用途**: テキスト生成のみ。
3. **プロバイダー**: 別の `chatgpt-codex`（`llm.Providers()` の 5 つ目。既定の `routes` には入れない）。公式の `chatgpt-subscription` の経路には触らない。初めての Codex のログインに成功した区画に、LLM の割り当てで選ぶよう案内を出す（自動では切り替えない）。
4. **隔離**: `CODEX_HOME` はデータディレクトリの `codex-home`（利用者の `~/.codex` と混ぜない）。`cli_auth_credentials_store` は `keyring`、使えなければ `auto`。子プロセスには環境の `CODEX_HOME`・`CODEX_SQLITE_HOME`・`CODEX_ACCESS_TOKEN`・`CODEX_API_KEY`・`OPENAI_API_KEY` を渡さない。
5. **版の固定**: 版・URL・SHA-256・サイズは埋め込みカタログ（`backend/internal/chatgpt/codex/catalog/data/catalog.json`）に固定する。プロトコルの型は手で書き、固定した版の `app-server` のスキーマ（`protocol/testdata/schema.json`。`scripts/codex-schema-snapshot.sh <版>` で取り直す）と突き合わせる CI のテストで、フィールド・列挙・メソッド・設定キーのずれを検知する。版を上げるときはカタログ・スキーマ・`THIRD_PARTY_NOTICES.md`（`scripts/third-party-notices.sh`）を一緒に直す（`catalog_test.go` がこの 3 つの版の一致を見る）。取得済みの版と固定した版が違うエンジンは使わず、再取得を案内する。
6. **アクセストークンの受け渡しはしない**（`chatgptAuthTokens`・`CODEX_ACCESS_TOKEN` は使わない）。mam はトークンを読まず、保存せず、プロトコルの型にもトークンの項目を持たない（テストで強制）。`account/read` は更新を頼まない。ログ・エラー・応答は `sanitize` を通す。
7. **名前**: プロバイダー `chatgpt-codex`、MCP のアカウント名 `codex`。
8. **システムプロンプト**: `thread/start` の `baseInstructions`（Codex 既定の、コーディング用の指示を置き換える）に入れる。`developerInstructions`（既定の指示に足す）へ切り替える**切り替え点は `codex.DefaultInstructions` の 1 値**。実機での比較は未実施（§8.7）。
9. **公式ログインは自動でログアウトしない**: ワークスペースのプランだと分かったとき・組織に拒否されたときは、案内（「Codex 経由のログインを使う」ボタンと `WorkspaceAdvice`）を出すだけで、公式のログインは消さない。

### 8.3 使い方・構成

- 設定画面「ChatGPT でログイン」に方式の切り替え（公式／Codex 経由）を置く。Codex 経由の区画は、利用条件（§8.6）・エンジン（状態・版・取得・進捗・中断・パス指定）・アカウント（メール・プラン・Codex の利用上限。リセット時刻は日本時間）・ブラウザ／デバイスコードのログイン・モデル一覧・ログアウト（取得した本体と `CODEX_HOME` の削除つき）。管理者の前提（Codex Local の許可、デバイスコードの許可）も画面と README に書く。ルートは `spec/api/endpoints.md`、MCP は `spec/api/mcp-tools-chatgpt.md`。
- 実装は `backend/internal/chatgpt/codex/`: `catalog`（固定した版）・`engine`（取得・検証・展開・置き換え・削除）・`rpc`（JSON-RPC）・`protocol`（型・スキーマの突き合わせ）・`appserver`（起動・ハンドシェイク・異常終了の検知・サーバーからの要求の拒否）・`codextest`（偽の app-server）と、上位の `Service`（状態・導入・ログイン・ログアウト・モデル・`Complete`）。子プロセスは `modelmgr/proc` の標準入出力版（`StartPiped`）で起動し、Windows ではコンソール窓を出さず、mam の終了で一緒に終わる。app-server は最初に使うときに起動する（画面を開いただけでは起動しない）。
- ログインはブラウザ（`account/login/start` の `chatgpt`。app-server が認可 URL を返し、mam が既定のブラウザを開く）とデバイスコード（`chatgptDeviceCode`）。進行中は区画が 2 秒ごとに自分を取り直し、中止・期限切れ・`account/login/completed` で終わる。ログアウトは `account/logout` を頼む（app-server が資格情報を消せなかったときは何も消さず失敗にする）。エンジンが無いまま本体と `CODEX_HOME` を消したときは、`keyring` に資格情報が残りうるので、資格情報マネージャーでの削除を案内する。

### 8.4 リクエストと道具の制限

- `thread/start`: `ephemeral:true`・`sandbox:"read-only"`・`approvalPolicy:"never"`・作業ディレクトリは mam 専用の空のディレクトリ（`codex-work`）。システムプロンプトは §8.2 の 8。JSON は `instructions` で言葉で求め、`CompleteJSON` の検証で受ける。画像入力は `localImage`（一時ファイル。ターンの後に消す）で、`model/list` が画像入力に対応するとしたモデルだけに送る（対応しないモデルに画像を付けた呼び出しは `unsupported`）。
- 道具の無効化は何重にも掛ける: ① 起動引数 `-c` で `web_search="disabled"` と `features.shell_tool` / `unified_exec` / `apply_patch_freeform` / `view_image` / `js_repl` / `code_mode` / `multi_agent` / `collab` / `apps` / `plugins` / `memories` / `image_generation` / `browser_use` / `computer_use` を `false`（キーはスキーマと突き合わせる） ② 読み取り専用の sandbox と `approvalPolicy:"never"` ③ サーバーからの要求（承認・ユーザー入力・ツール呼び出し・認証トークンの更新など）はすべて拒否（古い承認の形式は `abort`、新しい形式は `decline`） ④ テキスト以外の項目（コマンド・ファイル変更・MCP・Web 検索など）を始めたターンは中断し、`permanent` の失敗にする。②〜④は実装済みでテストしている。①が実機の Codex で実際に道具を切るかは未検証（§8.7）。
- 利用上限は Codex の枠（`account/rateLimits/read` の `codex`）だけを表示する。上限に達した失敗の「リセット時刻」は、100% の枠のうち最も遅いリセット、無ければ最も早いリセット（日本時間で表示）。
- **同時実行数の上限は設けない**（`chatgpt-subscription` と同じ。app-server は 1 プロセスで複数のスレッドを扱う）。

### 8.5 失敗の分け方（`codex.Failure` → `llm.APIError`）

| 種類 | 原因 | 扱い |
|------|------|------|
| `usage_limit` | プランの利用上限 | `Permanent`: 再試行せず・ブレーカーに数えず代替へ。理由・日本時間のリセット時刻・`https://chatgpt.com/settings/usage` をチャットに出す |
| `not_signed_in` / `unauthorized` | 未ログイン・認可の失敗 | `Permanent`: 代替へ。設定画面で Codex 経由のログインをやり直すよう案内 |
| `engine` | エンジンが無い・指定したパスが無い | `Permanent`: 代替へ。取得かパスの指定を案内 |
| `unsupported` | モデルが画像入力に対応しない | `Permanent`: 代替へ |
| `transient` | 接続・ストリーム・サーバーの不調・プロセスの終了 | 503 として指数バックオフで再試行（再試行・ブレーカー・代替は `model-adapters.md` §2.1） |
| `permanent` | コンテキスト超過・不正な要求・sandbox のエラー・道具を使ったターン | `Permanent` |

### 8.6 利用条件

- **ローカル・オープンソースでの利用に限る**: この方式は、mam を自分の PC で動かして自分のワークスペースのアカウントで使うためのもので、**商用・ホスティング（mam をサーバーで動かして他の人に提供する形）では使えない**（画面・README・MCP の説明に出す）。ワークスペースの Codex の利用上限を使う。
- 管理者の前提: メンバーに「Codex をローカルで使う」権限（Codex Local）が許可されていること。デバイスコードでログインするには、デバイスコードのログイン（beta）も許可されていること。管理者の managed configuration（`requirements.toml`）が mam 専用の `CODEX_HOME` に届くかは未検証。

### 8.7 未検証（実機・実アカウントが要る。`#839` に追記）

次は、単体テストと偽の app-server では確かめられていない。**確認済みとは書かない**。

- Business / Enterprise のアカウントでの Codex のログイン（ブラウザ・デバイスコード）と、実際に返る `planType`
- app-server のブラウザのログインのコールバックのポートが、mam の公式ログインの 1455・1457 とぶつかるか
- `cli_auth_credentials_store="keyring"` が Windows の資格情報マネージャーの 1 件 2560 バイトの中で動くか、`auto` に落ちたときの動き
- 管理者の managed configuration（`requirements.toml`）が mam 専用の `CODEX_HOME` にも届くか
- Windows の `readOnly` の sandbox に `windowsSandbox/setupStart`（elevated の設定・UAC）が要るか
- `turn/start` の `image` に data URL を渡せるか（mam は `localImage` の一時ファイルを使う）
- `3p_delegated_access_policy_denied` が公式ログインのコールバックの `error` に来るか（来なければ時間切れで案内する）
- `baseInstructions` と `developerInstructions` の品質の比較、`model/list` の実際の応答
- `-c features.*=false` と読み取り専用の sandbox で、実機の Codex が本当に道具を使わないか
- Windows で app-server の子プロセスがコンソール窓を出さないこと・mam の終了でプロセスグループごと終わること
- 約 88 MB の zip の実際のダウンロード（SHA-256・サイズは固定済みで、検証の手続きは偽の zip・偽のサーバーでだけ試した）
- NSIS のアンインストールと、`keyring` に残る資格情報（README: アンインストールの前にログアウトする）
- Business の Codex Local・デバイスコードの許可の要件

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。ログイン 2 系統（公式・非公式の画像）の決定、要確認 3 点の結果（1 は確認済み、2・3 と `text.format` は未検証）、ブラウザ・デバイスコード、トークンの保存・更新・ログアウト、テキスト・画像の使い道、画面と MCP | Issue #832。ChatGPT のサブスクでログインしてテキスト生成と画像生成に使えるようにする |
| 1.1 | 2026-10-24 | §5.2: 画像モデルを #831 の開放集合（`modelreg`）に載せた。スイッチ ON のあいだだけ選択肢に出し、OFF では呼ばずに代替へ回る | Issue #832（#831 の取り込み後） |
| 1.2 | 2026-10-26 | §1 に 3 つ目の系統 `codex` を足し、§8 に Codex 経由（app-server）の決定 9 点・構成・リクエストと道具の制限・失敗の分け方・利用条件（ローカル・オープンソースに限る）・未検証の一覧を追加。§6 の画面・MCP を更新 | Issue #845。Business / Enterprise / Edu のワークスペースのアカウントでテキスト生成を使えるようにする |
