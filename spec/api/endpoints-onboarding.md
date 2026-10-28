# API 仕様: 初回ガイド（`/onboarding`）のルートと応答

[endpoints.md](endpoints.md) §1 の `GET /` と `GET /onboarding`・`/onboarding/:step` の行の本体。画面の作りは [../components/onboarding-screen.md](../components/onboarding-screen.md)。Issue #863。

ガイドの設定の保存は、**既存の設定画面のルートをそのまま使う**（ガイドのページに設定画面の部品を埋め込む）。`PUT /credentials/:provider`（API キー、OS キーリング `mam` / `api-key:<provider>`）・`PUT /settings/llm/agents/:agent`（`llm.json`）・`PUT /settings/models/images/:purpose` / `…/policies/:policy`（`models.json`）・`/settings/chatgpt/…`（ChatGPT のログイン）は変えない。ガイドが足すのは下のルートだけで、`Services.Onboarding`（`settingsroutes.Services`）が無い構成（ルーターを設定なしで作るテスト）では、どれも無く、`GET /` も送らない。

## 1. ページ（GET）

すべてページ全体（HTMX の断片は返さない）。`Cache-Control: no-store`。`Vary: HX-Request, HX-History-Restore-Request`（`GET /` が HTMX かどうかで答えを変えるため）。

| Method | Path | 内容 | 状態コード |
|--------|------|------|-----------|
| GET | `/` | **変更**。フラグ（`<データフォルダ>/onboarding.json` の `completed`）が立っていなければ `/onboarding` へ **302**。立っていれば今までどおりプロジェクト一覧（`GET /projects` と同じ）。HTMX の要求は送らない。フラグを読めないとき（ファイルが壊れている）は送らず一覧を返す（エラーはログ） | 302 / 200 |
| GET | `/onboarding` | 最初のステップ（`/onboarding/flow`）へ **302**。フラグが立っていても開ける（設定画面からの再表示） | 302 |
| GET | `/onboarding/:step` | ステップのページ。`:step` は `flow`・`modes`・`prepare`（ウォークスルー）、`credentials`・`models`・`ffmpeg`・`mac`（セットアップ）。`mac` は macOS のときだけ。`models` は LLM・モデル設定のサービスがあるときだけ。ほかは 404 | 200 / 404 |
| GET | `/onboarding/credentials/status` | `credentials` のステップの状態の行（`#onboarding-credentials-status`）の断片。モーダルが閉じたときに取り直す。API キー 1 件以上または ChatGPT のログイン 1 つ以上で「設定済み」、無ければ警告 | 200 |

- ページの `<title>` は「はじめに: <ステップの見出し>」。`h1` は「はじめに」の 1 つ、ステップの見出しは `h2`。
- ステップのページの各リンクは通常の `<a href>`（`hx-boost` を使わない）。

## 2. 操作

| Method | Path | 内容 | 状態コード |
|--------|------|------|-----------|
| POST | `/onboarding/finish` | ガイドを終える（最後まで進んだ・スキップした）。CSRF 必須。`onboarding.json` に `{"completed": true}` を書く（ほかのキーは残す。すでに立っていても同じ）。本文なし。ヘッダー `HX-Redirect: /`。書けなかったときは 500 とエラーのトースト（`HX-Redirect` なし。フラグは立たない） | 200 / 500 |
| PUT | `/onboarding/codex-path` | **macOS のときだけ**（ほかは 404。ChatGPT のサービスが無い構成でも 404）。Mac のステップの「Codex のパス」。フォーム `path`（空なら指定を解除）。CSRF 必須。保存は設定画面の `PUT /settings/chatgpt/codex/path` と同じ `chatgpt.Service.Codex().SetCustomPath`。応答は結果の 1 文の断片（`#onboarding-codex-result`）: 成功は `role="status"`（「パスを設定しました。mam は実行して確かめません」/「パスの指定を解除しました」）、実在するファイルの絶対パスでなければ **422** で `role="alert"`（`swapsErrorResponse` が 4xx の HTML を swap する） | 200 / 422 / 404 |

## 3. 初回かどうかのフラグ

- 保存先: データフォルダの `onboarding.json`（`service/onboarding.FileName`）。形は `{"completed": true}`。`service/settingsfile` で読み書きする（ほかのキーを消さない。一時ファイルから置き換える）。
- 秘密を含まない。`--llm-config` のような「固定」の概念は無い（常に書ける）。
- API キーの数・ログインの有無・ほかの設定からは**推測しない**。

## 4. ffmpeg の確認

- `GET /onboarding/ffmpeg` の本文のために、`service/onboarding.Service.FFmpeg` が `exec.LookPath("ffmpeg")`・`exec.LookPath("ffprobe")` を呼ぶ（`internal/media` の `Look` と同じ探し方。実行はしない）。結果は 2 つのパス（見つからないものは空）。
- OS は `runtime.GOOS`（`windows` → winget、`darwin` → brew、ほか → 汎用の文）。画面の文言は [../components/onboarding-screen.md](../components/onboarding-screen.md) §4.3。

## 5. 変わらないこと

- `GET /projects`・`/projects/:id`・設定の各ページの意味は変えない。
- 既存の `PUT /credentials/:provider`・LLM・モデル・ChatGPT のルートの契約（パス・フォーム・状態コード・応答）は変えない。ガイドのページは、それらの応答（行の OOB・トースト・`HX-Trigger-After-Swap: modal-close`）が当たる `id` を、設定画面と同じ部品で持つ。
