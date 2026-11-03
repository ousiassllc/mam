# MCP ツール: ChatGPT でログイン

[`mcp-tools.md`](mcp-tools.md) の規約に従う、設定画面「ChatGPT でログイン」の操作のツール（Issue #832）。設計は [`../architecture/chatgpt-signin.md`](../architecture/chatgpt-signin.md)。実装は `backend/internal/mcp/tools/chatgptauth`（サービスは `backend/internal/chatgpt`。アプリが持たないとき、つまり `Settings.ChatGPT` が無いときは全ツールが `unavailable`）。

## 方針

- **モデルの一覧**（`list_llm_models`、#987・#988）: `set_llm_assignment` に渡すモデル名を確かめるためのツール。トークン・API キーは返さない。`set_llm_assignment` は、一覧にないモデル名を**拒否せず保存し**、応答の `warnings[]`（`kind: unlisted_model`, `agent`, `role`, `provider`, `model`, `message`）で知らせる（[`mcp-tools-settings-policy.md`](mcp-tools-settings-policy.md)）。
- **トークンはどのツールの応答にも載せない**（アクセス・リフレッシュ・ID トークン、`id_token_hint` つきの URL も）。返すのは、ログインしているか・メール・プラン・アクセストークンの期限・再ログインの要否・画像生成の ON/OFF だけ。全ツールの結果の全文に、保存されたトークンの値と JWT らしい値（`eyJ`）が無いことを、テスト（`chatgptauth_test.go`）で確かめている。
- ログインは 3 系統（`account`）: `official`（公式。テキスト生成）、`codex`（Codex の app-server 経由。テキスト生成のみ。Business / Enterprise / Edu のワークスペースのアカウントも使える。プロバイダー `chatgpt-codex`。設計 `chatgpt-codex/codex-login.md`）、`image`（画像生成・非公式）。`codex` もトークンを返さない（mam は Codex のトークンを読まない）。
- **ブラウザのログインはコールバックが mam の PC の `127.0.0.1` に来る**。`start_chatgpt_login` は mam の PC の既定ブラウザを開き、`auth_url` も返す。tailnet などリモートから使うときは、mam の PC のブラウザでその URL を開いてもらう（公式のログインにデバイスコードはない: 確認済み、設計 §2）。画像生成のログインだけ、デバイスコードでどの端末からも行える。
- **画像生成（非公式）は既定で OFF**。`set_chatgpt_image_generation` の説明と応答にリスクを載せ、ON にするには `acknowledge_risks: true`（利用者がリスクを確認したことを呼び出し側が確かめて渡す）が要る。OFF のあいだは、画像生成のログインも開始できず（`conflict`）、この経路は一切呼ばれない。
- ログアウトはリフレッシュトークンの失効を OpenAI に求めてから、保存したトークンを消す。失効を確認できなくてもローカルのトークンは消え、応答の `revoke_confirmed: false` と `message` で知らせる（ChatGPT の設定で接続を解除してもらう）。

## ツール

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_chatgpt_status` | 3 つのログインの状態と画像生成のスイッチ。トークンは返さない | なし | `codex`: `signed_in`, `email`, `plan`, `engine`（`not_installed` / `installing` / `ready` / `running` / `failed`）, `engine_version`, `engine_source`, `engine_error`, `can_download`, `rate_limits[]`（`name`, `used_percent`, `resets_at`）。`official` / `image`: `signed_in`, `needs_relogin`, `relogin_reason`, `email`, `plan`, `access_token_expires_at`, `plan_usage_granted`（公式のみ）。`image_generation_enabled`, `image_generation_risks[]`, `usage_url` | はい |
| `start_chatgpt_login` | 公式の Sign in with ChatGPT を始める（ブラウザ。mam の PC の既定ブラウザを開く）。初回は動的登録 | なし | `login_id`, `account`（`official`）, `method`（`browser`）, `state`, `auth_url`, `browser_opened`, `expires_at`, `note`（リモートの案内） | いいえ（openWorld） |
| `start_chatgpt_image_device_login` | 画像生成（非公式）のログインをデバイスコードで始める。画像生成が ON のときだけ（OFF は `conflict`） | なし | `login_id`, `account`（`image`）, `method`（`device`）, `state`, `verification_url`, `user_code`, `interval_seconds`, `expires_at`, `note` | いいえ（openWorld） |
| `install_chatgpt_codex_engine` | Codex の app-server を公式の GitHub リリース（固定した版・SHA-256・サイズを検証。約 80〜90 MB。Windows 版と macOS 版（Apple Silicon）のみ）から `codex` ディレクトリ（Windows は `mam.exe` と同じ場所、macOS は `.app` の外のデータディレクトリ）へ取得する。中断しても再開できる。Windows・macOS（Apple Silicon）以外・書き込めない場所・ディスクの空き不足は `conflict` | なし | `job_id`, `state`, `existing?`。進捗は `get_job`（種類 `chatgpt_codex_install`。`done` は 0〜100 の割合） | いいえ（openWorld） |
| `start_chatgpt_codex_login` | Codex 経由のログインを始める。`browser`（既定。mam の PC の既定ブラウザ）か `device`（デバイスコード。ワークスペースの許可が必要）。エンジンが無いと `conflict`（`install_chatgpt_codex_engine` を案内）。ローカル・オープンソースの利用のみで、商用・ホスティングには使えない | `method?`（`browser` / `device`） | `login_id`, `account`（`codex`）, `method`, `state`, `auth_url` / `verification_url`, `user_code`, `expires_at`, `note` | いいえ（openWorld） |
| `get_chatgpt_login_status` | `login_id` のログインの進み具合（`codex` のログインも含む）。`pending` の間は `interval_seconds`（無ければ数秒）ごとに呼ぶ。知らない ID は `not_found` | `login_id` | 上の `login_id` … `expires_at` に `state`（`pending` / `succeeded` / `failed` / `cancelled`）と `message`（失敗の理由） | はい |
| `logout_chatgpt` | ログアウト（失効の要求 → トークンの削除。冪等）。`codex` は app-server に `account/logout` を頼み、`remove_files: true` で取得した本体と mam 専用の `CODEX_HOME` も削除する（アンインストールの前に使う） | `account`（`official` / `codex` / `image`）, `remove_files?`（`codex` のみ） | `account`, `was_signed_in`, `revoke_confirmed`, `message` | いいえ（破壊的・openWorld） |
| `set_chatgpt_image_generation` | 画像生成（非公式）の ON/OFF（冪等）。ON は `acknowledge_risks: true` が必要（無ければ `validation`、リスクの全文つき） | `enabled`, `acknowledge_risks?` | `image_generation_enabled`, `image_generation_risks[]`, `next` | いいえ |
| `list_llm_models` | 「LLM の割り当て」で選べるモデルの一覧（設定画面のプルダウンと同じ `llmmodels.Service`）。`chatgpt-subscription`（サインイン済みのアカウントの `GET https://api.openai.com/v1/models`、`visibility=="list"` の `slug`）・`chatgpt-codex`（Codex の `model/list`）・`openai` / `anthropic` / `google`（保存した API キーでの各社の一覧）は API の一覧、取得できないとき（キーなし・未ログイン・失敗）は mam の既定候補（`agent/llm/data/models.json`）。10 分キャッシュ（失敗は 1 分）。トークン・API キーは返さない。未対応の `provider` は `validation`。実装は `tools/settings`（LLM の割り当てと同じ区画）で、`Settings.LLMModels` が無いアプリでは `unavailable` | `provider?`（`get_global_settings` の `llm.providers`。省略で全部） | `providers[]`: `provider`, `source`（`api` / `default`）, `reason?`（`default` の理由）, `models[]`（`id`, `name?`）。`set_llm_assignment` の `model` には `id` を渡す | はい（openWorld） |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。6 ツール（`get_chatgpt_status`・`start_chatgpt_login`・`start_chatgpt_image_device_login`・`get_chatgpt_login_status`・`logout_chatgpt`・`set_chatgpt_image_generation`） | Issue #832。ChatGPT のログインを MCP でも扱う（トークンは返さない） |
| 1.1 | 2026-10-25 | `account: codex` を足し、`install_chatgpt_codex_engine`・`start_chatgpt_codex_login` を追加（8 ツール）。`get_chatgpt_status` に `codex`、`logout_chatgpt` に `remove_files` | Issue #845。Business / Enterprise / Edu のアカウントを Codex の app-server 経由で使えるようにする |
| 1.2 | 2026-11-01 | `install_chatgpt_codex_engine` を macOS（Apple Silicon）にも開き（取得先は `.app` の外のデータディレクトリの `codex`）、「Windows 版のみ」の書き方を直した。`get_chatgpt_status` の `can_download` も同じ | Issue #855。macOS 版でも Codex 本体を取得できるようにした（詳細は `architecture/chatgpt-codex/codex-login.md` §2） |
| 1.3 | 2026-11-03 | `list_llm_models` を追加（`chatgpt-subscription`・`chatgpt-codex` を含む各プロバイダーのモデルの一覧。API の一覧、取れなければ mam の既定候補）。`set_llm_assignment` が一覧にない名前に警告を返す | Issue #987・#988。実アカウントでの一覧の取得は未確認（実機） |
