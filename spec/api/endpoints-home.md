# エンドポイント: モデルマネージャーのカードの状態

[endpoints.md](endpoints.md) の続き（Issue #846。#978 でトップ画面のツールのカードを外したので、残るのは設定画面のカードの 1 本）。画面の意味は [../components/home-screen.md](../components/home-screen.md) §5。ハンドラは `web/handler/toolstatus`、登録は `router/settingsroutes/tools.go`。

設定画面の概要（`GET /settings`）と「ローカル・代替モデル」（`GET /settings/local-models`）は、モデルマネージャーのカードの枠を先に出し、`hx-get` + `hx-trigger="load"` で次のルートを 1 回だけ読む。

| メソッド | パス | 説明 |
|---|---|---|
| GET | `/tools/model-manager/status` | モデルマネージャーのカードの状態の断片（`paths.ToolStatus(paths.ToolModelManager)`）。ほかの `tool`（`nodes`・`workflows`・`settings`・`reviews`・生成テスト・制作指標を含む）のルートは無く、404（#978 でトップ画面のカードと一緒に削除） |

- 応答は**常に 200**。本文は `atoms.Badge` の並び（1 行に 1 つ）か、**空**。読めない・サーバー側の締め切り（2 秒）を超えた・サービスがルーターに無い（テスト用）ときは空で、カードは出る。5xx にしないのは、`htmx:responseError` のトーストを避けるため。
- `Cache-Control: no-store`。状態を変えない GET なので、認証・CSRF は他の GET と同じ。
- 読むのはローカルだけ（設定ファイル・同梱の ComfyUI のプロセスの状態）。外部の API も、外部の ComfyUI への接続も使わない。ディスクには何も書かない（`modelmanager.Service.Brief()` は書き込み確認・空き容量を取らず、未導入でも `comfyui` ディレクトリを作らない）。
- 状態の読み取りが失敗・パニック・締め切り超えのときは、ルーターのロガーに warn（パニックは error）でツール名つきで記録し、応答は空の 200 のまま。
- 表示は次のとおり。`Badge` の文字が状態を言う（色は補助）。

| tool | 表示（トーン ・ 文） |
|------|--------------------|
| `model-manager` | 同梱の ComfyUI が導入済みなら、ok ・ ComfyUI: 起動中 ／ info ・ ComfyUI: 起動しています ／ neutral ・ ComfyUI: 停止中 ／ error ・ ComfyUI: 起動に失敗。未導入・壊れている（`broken`）・非対応（Windows・Apple Silicon の Mac 以外）で外部の ComfyUI が登録済みなら、neutral ・ 外部の ComfyUI: n 件。どちらも無いとき、neutral ・ ComfyUI: 未導入（非対応は 同梱の ComfyUI は Windows 版と Apple Silicon の Mac 版のみ） |

設定画面の概要の「API キーとログイン」のカードの状態（1 行目: info ・ API キー n / 14 件（0 件は warn。キーストアが読めないときは warn ・ キーストアを利用できません）。2 行目（ChatGPT のサービスがあり、読めるとき）: warn ・ ChatGPT: 再ログインが必要 ／ ok ・ ChatGPT: ログイン済み（公式・画像生成（非公式）・Codex 経由のどれか）／ neutral ・ ChatGPT: 未ログイン）は、同じパッケージの `toolstatus.Services.Settings` がサーバーで組み立てる。ルートは無い。
