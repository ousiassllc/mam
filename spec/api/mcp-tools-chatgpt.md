# MCP ツール: ChatGPT でログイン

[`mcp-tools.md`](mcp-tools.md) の規約に従う、設定画面「ChatGPT でログイン」の操作のツール（Issue #832）。設計は [`../architecture/chatgpt-signin.md`](../architecture/chatgpt-signin.md)。実装は `backend/internal/mcp/tools/chatgptauth`（サービスは `backend/internal/chatgpt`。アプリが持たないとき、つまり `Settings.ChatGPT` が無いときは全ツールが `unavailable`）。

## 方針

- **トークンはどのツールの応答にも載せない**（アクセス・リフレッシュ・ID トークン、`id_token_hint` つきの URL も）。返すのは、ログインしているか・メール・プラン・アクセストークンの期限・再ログインの要否・画像生成の ON/OFF だけ。全ツールの結果の全文に、保存されたトークンの値と JWT らしい値（`eyJ`）が無いことを、テスト（`chatgptauth_test.go`）で確かめている。
- ログインは 2 系統（`account`）: `official`（公式。テキスト生成）と `image`（画像生成・非公式）。
- **ブラウザのログインはコールバックが mam の PC の `127.0.0.1` に来る**。`start_chatgpt_login` は mam の PC の既定ブラウザを開き、`auth_url` も返す。tailnet などリモートから使うときは、mam の PC のブラウザでその URL を開いてもらう（公式のログインにデバイスコードはない: 確認済み、設計 §2）。画像生成のログインだけ、デバイスコードでどの端末からも行える。
- **画像生成（非公式）は既定で OFF**。`set_chatgpt_image_generation` の説明と応答にリスクを載せ、ON にするには `acknowledge_risks: true`（利用者がリスクを確認したことを呼び出し側が確かめて渡す）が要る。OFF のあいだは、画像生成のログインも開始できず（`conflict`）、この経路は一切呼ばれない。
- ログアウトはリフレッシュトークンの失効を OpenAI に求めてから、保存したトークンを消す。失効を確認できなくてもローカルのトークンは消え、応答の `revoke_confirmed: false` と `message` で知らせる（ChatGPT の設定で接続を解除してもらう）。

## ツール

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_chatgpt_status` | 2 つのログインの状態と画像生成のスイッチ。トークンは返さない | なし | `official` / `image`: `signed_in`, `needs_relogin`, `relogin_reason`, `email`, `plan`, `access_token_expires_at`, `plan_usage_granted`（公式のみ）。`image_generation_enabled`, `image_generation_risks[]`, `usage_url` | はい |
| `start_chatgpt_login` | 公式の Sign in with ChatGPT を始める（ブラウザ。mam の PC の既定ブラウザを開く）。初回は動的登録 | なし | `login_id`, `account`（`official`）, `method`（`browser`）, `state`, `auth_url`, `browser_opened`, `expires_at`, `note`（リモートの案内） | いいえ（openWorld） |
| `start_chatgpt_image_device_login` | 画像生成（非公式）のログインをデバイスコードで始める。画像生成が ON のときだけ（OFF は `conflict`） | なし | `login_id`, `account`（`image`）, `method`（`device`）, `state`, `verification_url`, `user_code`, `interval_seconds`, `expires_at`, `note` | いいえ（openWorld） |
| `get_chatgpt_login_status` | `login_id` のログインの進み具合。`pending` の間は `interval_seconds`（無ければ数秒）ごとに呼ぶ。知らない ID は `not_found` | `login_id` | 上の `login_id` … `expires_at` に `state`（`pending` / `succeeded` / `failed` / `cancelled`）と `message`（失敗の理由） | はい |
| `logout_chatgpt` | ログアウト（失効の要求 → トークンの削除。冪等） | `account`（`official` / `image`） | `account`, `was_signed_in`, `revoke_confirmed`, `message` | いいえ（破壊的・openWorld） |
| `set_chatgpt_image_generation` | 画像生成（非公式）の ON/OFF（冪等）。ON は `acknowledge_risks: true` が必要（無ければ `validation`、リスクの全文つき） | `enabled`, `acknowledge_risks?` | `image_generation_enabled`, `image_generation_risks[]`, `next` | いいえ |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。6 ツール（`get_chatgpt_status`・`start_chatgpt_login`・`start_chatgpt_image_device_login`・`get_chatgpt_login_status`・`logout_chatgpt`・`set_chatgpt_image_generation`） | Issue #832。ChatGPT のログインを MCP でも扱う（トークンは返さない） |
