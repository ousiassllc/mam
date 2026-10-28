# エンドポイント: トップ画面のツールのカードの状態

[endpoints.md](endpoints.md) の続き（Issue #846）。画面の意味は [../components/home-screen.md](../components/home-screen.md) §5。ハンドラは `web/handler/toolstatus`、登録は `router/settingsroutes/tools.go`。

トップ画面（`GET /`）は、カードの枠を先に出し、状態を持つカードごとに `hx-get` + `hx-trigger="load"` で次のルートを 1 回だけ読む。

| メソッド | パス | 説明 |
|---|---|---|
| GET | `/tools/nodes/status`・`/tools/workflows/status`・`/tools/model-manager/status`・`/tools/settings/status`・`/tools/reviews/status` | そのカードの状態の断片（`paths.ToolStatus(tool)`。`tool` は `paths.ToolNodes` など）。ほかの `tool`（生成テスト・制作指標を含む）のルートは無く、404 |

- 応答は**常に 200**。本文は `atoms.Badge` の並び（1 行に 1 つ）か、**空**。読めない・サーバー側の締め切り（2 秒）を超えた・サービスがルーターに無い（テスト用）ときは空で、カードは出る。5xx にしないのは、`htmx:responseError` のトーストがカードの数だけ並ぶのを避けるため。
- `Cache-Control: no-store`。状態を変えない GET なので、認証・CSRF は他の GET と同じ。
- 読むのはローカルだけ（DB・設定ファイル・キーストア・同梱の ComfyUI のプロセスの状態）。外部の API も、外部の ComfyUI への接続も使わない。ディスクには何も書かない（`modelmanager.Service.Brief()` は書き込み確認・空き容量を取らず、未導入でも `comfyui` ディレクトリを作らない）。
- 状態の読み取りが失敗・パニック・締め切り超えのときは、ルーターのロガーに warn（パニックは error）でツール名つきで記録し、応答は空の 200 のまま。キーストアの読み取りはリクエストの文脈を見て、カードが諦めた後は次の項目を読まない（応答が遅いキーストアに読み取りが積み重ならないように）。
- 表示は次のとおり。`Badge` の文字が状態を言う（色は補助）。

| tool | 表示（トーン ・ 文） |
|------|--------------------|
| `nodes`・`model-manager` | 同梱の ComfyUI が導入済みなら、ok ・ ComfyUI: 起動中 ／ info ・ ComfyUI: 起動しています ／ neutral ・ ComfyUI: 停止中 ／ error ・ ComfyUI: 起動に失敗。未導入・壊れている（`broken`）・非対応（Windows 以外）で外部の ComfyUI が登録済みなら、neutral ・ 外部の ComfyUI: n 件。どちらも無いとき、`nodes` は warn ・ ComfyUI が必要です、`model-manager` は neutral ・ ComfyUI: 未導入（非対応は 同梱の ComfyUI は Windows 版のみ） |
| `workflows` | neutral ・ 保存 n 件 ／ 保存なし |
| `reviews` | neutral ・ 受け取り n 件 ／ 受け取りなし |
| `settings` | 1 行目: info ・ API キー n / 11 件（0 件は warn。キーストアが読めないときは warn ・ キーストアを利用できません）。2 行目（ChatGPT のサービスがあり、読めるとき）: warn ・ ChatGPT: 再ログインが必要（公式・画像生成（非公式）のどちらかが再ログイン要。Codex 経由には再ログイン要の状態が無い）／ ok ・ ChatGPT: ログイン済み（公式・画像生成（非公式）・Codex 経由のどれかがログイン済み）／ neutral ・ ChatGPT: 未ログイン |
