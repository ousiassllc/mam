# ChatGPT 経由の Codex（Codex の app-server でのログインとテキスト生成）

Issue #845。[`../chatgpt-signin.md`](../chatgpt-signin.md) §8 の詳細。LLM の割り当ては [`../model-adapters.md`](../model-adapters.md) §2.1、セキュリティは `spec/requirements/non-functional.md` §5、MCP は `spec/api/mcp-tools-chatgpt.md`。

## 1. なぜ要るか

ChatGPT Business / Enterprise / Edu のワークスペースのアカウントは、公式の動的登録（§1 の `official`）では**プランを使えない**。サインインそのものは通るが、組織の Admin Console の「外部アクセス」（ID・ChatGPT 広告・連携アプリの 3 項目だけで、プランの利用を許可する項目は無い）に許可の設定が無く、Responses API 側が `3p_delegated_access_policy_denied` / `subscription_sharing_user_not_eligible` で拒む。そこで、OpenAI 自身のクライアントである **Codex の app-server** にログインとリクエストを任せ、同じアカウントを**テキスト生成だけ**に使う。画像・動画・音声には使わない（オーナー決定）。

## 2. 決定事項（Issue #845。オーナーが推奨を選んだもの）

1. **エンジン**: 既定は公式の GitHub リリース `rust-v0.164.0` の `codex-app-server-<arch>-pc-windows-msvc.exe.zip`（amd64 / arm64。約 88 MB・展開後約 270 MB）を実行時に取得し、zip と展開後の実行ファイルの SHA-256 とサイズを検証して、`<mam.exe のディレクトリ>\codex\` に置く（`installed.json` に版と来歴を記録。インストーラー版はアンインストールで一緒に消える）。取得は Windows 版のみ。利用者が自分で入れた codex の絶対パスを設定画面で指定してもよい（他の OS・ブラウザ版はこれだけ）。mam は Codex を再配布しない（`THIRD_PARTY_NOTICES.md`）。
2. **用途**: テキスト生成のみ。
3. **プロバイダー**: 別の `chatgpt-codex`（`llm.Providers()` の 5 つ目。既定の `routes` には入れない）。公式の `chatgpt-subscription` の経路には触らない。初めての Codex のログインに成功した区画に、LLM の割り当てで選ぶよう案内を出す（自動では切り替えない）。
4. **隔離**: `CODEX_HOME` はデータディレクトリの `codex-home`（利用者の `~/.codex` と混ぜない）。データディレクトリは起動時に絶対パスにする（相対のままだと子プロセスの作業ディレクトリで別の場所を指す）。ハンドシェイクで app-server が答えた `codexHome` が mam の `codex-home` と同じディレクトリか（文字列が違っても、シンボリックリンク・ジャンクションの先でも、`os.SameFile` で同一なら同じとみなす）を確かめ、違えば止める。`cli_auth_credentials_store` は `keyring`、使えなければ `auto`。子プロセスには環境の `CODEX_HOME`・`CODEX_SQLITE_HOME`・`CODEX_ACCESS_TOKEN`・`CODEX_API_KEY`・`OPENAI_API_KEY` を渡さない。
5. **版の固定**: 版・URL・SHA-256・サイズは埋め込みカタログ（`backend/internal/chatgpt/codex/catalog/data/catalog.json`）に固定する。プロトコルの型は手で書き、固定した版の `app-server` のスキーマ（`protocol/testdata/schema.json`。`scripts/codex-schema-snapshot.sh <版>` で取り直す）と突き合わせる CI のテストで、フィールド・列挙・メソッド・設定キーのずれを検知する。版を上げるときはカタログ・スキーマ・`THIRD_PARTY_NOTICES.md`（`scripts/third-party-notices.sh`）を一緒に直す（`catalog_test.go` がこの 3 つの版の一致を見る）。取得済みの版と固定した版が違うエンジンは使わず、再取得を案内する。
6. **アクセストークンの受け渡しはしない**（`chatgptAuthTokens`・`CODEX_ACCESS_TOKEN` は使わない）。mam はトークンを読まず、保存せず、プロトコルの型にもトークンの項目を持たない（テストで強制）。`account/read` は更新を頼まない。ログ・エラー・応答は `sanitize` を通す。
7. **名前**: プロバイダー `chatgpt-codex`、MCP のアカウント名 `codex`。
8. **システムプロンプト**: `thread/start` の `baseInstructions`（Codex 既定の、コーディング用の指示を置き換える）に入れる。`developerInstructions`（既定の指示に足す）へ切り替える**切り替え点は `codex.DefaultInstructions` の 1 値**。実機での比較は未実施（§7）。
9. **公式ログインは自動でログアウトしない**: ワークスペースのプランだと分かったとき・組織に拒否されたときは、案内（「Codex 経由のログインを使う」ボタンと `WorkspaceAdvice`）を出すだけで、公式のログインは消さない。

## 3. 使い方・構成

- 設定画面「ChatGPT でログイン」に方式の切り替え（公式／Codex 経由）を置く。Codex 経由の区画は、利用条件（「利用条件」の節）・エンジン（状態・版・取得・進捗・中断・パス指定）・アカウント（メール・プラン・Codex の利用上限。リセット時刻は日本時間）・ブラウザ／デバイスコードのログイン・モデル一覧・ログアウト（取得した本体と `CODEX_HOME` の削除つき）。管理者の前提（Codex Local の許可、デバイスコードの許可）も画面と README に書く。ルートは `spec/api/endpoints.md`、MCP は `spec/api/mcp-tools-chatgpt.md`。
- 実装は `backend/internal/chatgpt/codex/`: `catalog`（固定した版）・`engine`（取得・検証・展開・置き換え・削除）・`rpc`（JSON-RPC）・`protocol`（型・スキーマの突き合わせ）・`appserver`（起動・ハンドシェイク・異常終了の検知・サーバーからの要求の拒否）・`codextest`（偽の app-server）と、上位の `Service`（状態・導入・ログイン・ログアウト・モデル・`Complete`）。子プロセスは `modelmgr/proc` の標準入出力版（`StartPiped`）で起動し、Windows ではコンソール窓を出さず、mam の終了で一緒に終わる。app-server は最初に使うときに起動する（画面を開いただけでは起動しない）。エンジンの導入は同時に 1 つだけで、始めた時点（本体の差し替えのために app-server を止める前）から終わるまで app-server を起動しない（その間の起動は「導入中」で断る）。エンジンが無いまま使って起動に失敗しても、状態は「失敗」ではなく「未導入」のままにする。ログアウトと終了は、通知で始まったアカウントの読み直しの終わりを待ってから、記録とファイルを消す。
- ログインはブラウザ（`account/login/start` の `chatgpt`。app-server が認可 URL を返し、mam が既定のブラウザを開く）とデバイスコード（`chatgptDeviceCode`）。進行中は区画が 2 秒ごとに自分を取り直し、中止・期限切れ・`account/login/completed` で終わる。ログアウトは `account/logout` を頼む（app-server が資格情報を消せなかったときは何も消さず失敗にする）。エンジンが無いまま本体と `CODEX_HOME` を消したときは、`keyring` に資格情報が残りうるので、資格情報マネージャーでの削除を案内する。

## 4. リクエストと道具の制限

- `thread/start`: `ephemeral:true`・`sandbox:"read-only"`・`approvalPolicy:"never"`・作業ディレクトリは mam 専用の空のディレクトリ（`codex-work`）。システムプロンプトは §2 の 8。JSON は `instructions` で言葉で求め、`CompleteJSON` の検証で受ける。画像入力は `localImage`（一時ファイル。ターンの後に消す）で、`model/list` が画像入力に対応するとしたモデルだけに送る（対応しないモデルに画像を付けた呼び出しは `unsupported`）。
- 道具の無効化は何重にも掛ける: ① 起動引数 `-c` で `web_search="disabled"` と `features.shell_tool` / `unified_exec` / `apply_patch_freeform` / `view_image` / `js_repl` / `code_mode` / `multi_agent` / `collab` / `apps` / `plugins` / `memories` / `image_generation` / `browser_use` / `computer_use` を `false`（キーはスキーマと突き合わせる） ② 読み取り専用の sandbox と `approvalPolicy:"never"` ③ サーバーからの要求（承認・ユーザー入力・ツール呼び出し・認証トークンの更新など）はすべて拒否（古い承認の形式は `abort`、新しい形式は `decline`） ④ テキスト以外の項目（コマンド・ファイル変更・MCP・Web 検索など）を始めたターンは中断し、`permanent` の失敗にする。②〜④は実装済みでテストしている。①が実機の Codex で実際に道具を切るかは未検証（§7）。
- 利用上限は Codex の枠（`account/rateLimits/read` の `codex`）だけを表示する。上限に達した失敗の「リセット時刻」は、100% の枠のうち最も遅いリセット、無ければ最も早いリセット（日本時間で表示）。
- **同時実行数の上限は設けない**（`chatgpt-subscription` と同じ。app-server は 1 プロセスで複数のスレッドを扱う）。

## 5. 失敗の分け方（`codex.Failure` → `llm.APIError`）

| 種類 | 原因 | 扱い |
|------|------|------|
| `usage_limit` | プランの利用上限 | `Permanent`: 再試行せず・ブレーカーに数えず代替へ。理由・日本時間のリセット時刻・`https://chatgpt.com/settings/usage` をチャットに出す |
| `not_signed_in` / `unauthorized` | 未ログイン・認可の失敗 | `Permanent`: 代替へ。設定画面で Codex 経由のログインをやり直すよう案内 |
| `engine` | エンジンが無い・指定したパスが無い | `Permanent`: 代替へ。取得かパスの指定を案内 |
| `unsupported` | モデルが画像入力に対応しない | `Permanent`: 代替へ |
| `transient` | 接続・ストリーム・サーバーの不調・プロセスの終了 | 503 として指数バックオフで再試行（再試行・ブレーカー・代替は `model-adapters.md` §2.1） |
| `permanent` | コンテキスト超過・不正な要求・sandbox のエラー・道具を使ったターン | `Permanent` |

## 6. 利用条件

- **ローカル・オープンソースでの利用に限る**: この方式は、mam を自分の PC で動かして自分のワークスペースのアカウントで使うためのもので、**商用・ホスティング（mam をサーバーで動かして他の人に提供する形）では使えない**（画面・README・MCP の説明に出す）。ワークスペースの Codex の利用上限を使う。
- 管理者の前提: メンバーに「Codex をローカルで使う」権限（Codex Local）が許可されていること。デバイスコードでログインするには、デバイスコードのログイン（beta）も許可されていること。管理者の managed configuration（`requirements.toml`）が mam 専用の `CODEX_HOME` に届くかは未検証。

## 7. 未検証（実機・実アカウントが要る。`#839` に追記）

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
