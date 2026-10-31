# MCP サーバー

画面でできる操作を、エージェント（tailnet 上の Grok Bot など）から MCP（Model Context Protocol）のツールとして行えるようにする、mam 内蔵のサーバー（Issue #830）。トランスポートは Streamable HTTP。ツールの一覧は [`spec/api/mcp-tools.md`](../api/mcp-tools.md)、テーブルは [`er-mcp.md`](er-mcp.md)、実装の手引き（ツールの足し方）は `backend/internal/mcp/README.md`。

## 1. 範囲と方針

- 対象は画面でできる操作のすべて。ツールは **サービス層（`service.Services`）を直接呼ぶ**（HTTP の自己呼び出しはしない）。入力の検証・状態の検査・LOCK / Brand の拒否は画面と同じコードを通る。ハンドラにしかなかった処理（multipart の解析など）は、サービスへ移して両方から使う（複製しない）。
- MCP 専用の確認や制限は入れない（画面と同じ扱い）。コストの歯止めは既存の Retry Policy・予算上限の範囲どおり。破壊的なツールにも確認は付けない（エージェントはブラウザの確認ダイアログに答えられない）。
- ブラウザ版（`cmd/server`）でも Windows のウィンドウ版（`cmd/wails`）でも、両者が呼ぶ `app.Run` の中で起動するので、アプリを起動していれば同じ MCP サーバーが使える。

## 2. 構成

```text
app.Run ─ serve ─┬─ router（gin。画面と /api/v1。127.0.0.1:8787）
                 └─ mcp/server（net/http。/mcp と /mcp-files/。bind 設定に従う。既定 127.0.0.1:8788）
```

| 部品 | パッケージ | 責務 |
|------|-----------|------|
| サーバーと認証 | `mcp/server` | Streamable HTTP のハンドラ（公式 SDK `github.com/modelcontextprotocol/go-sdk`）、ベアラー検査、Origin 検査、`Manager`（リスナーの所有とライブ切り替え） |
| 待ち受け先の規則 | `mcp/bind` | アドレスの検証（§3）と `mcp.json` の読み書き |
| ツールの土台 | `mcp/mcpkit` | `Registrar`（型付きの `Read` / `Write` / `Destroy`、エラー写像）、`Jobs`（§6）、`Files`（§7）。ツールのパッケージに依存しない |
| ツール | `mcp/tools/<ドメイン>` | 各パッケージが `Register(r *mcpkit.Registrar, svc *service.Services)` を公開する。一覧は `mcp/toolset/all.go` の 1 か所だけ |
| ログの読み取り | `logging`（`Reader`・`Redact`）、`mcp/tools/logs` | `get_app_logs`: `mam.log` とローテーション済みの世代を読み、絞り込み、秘密を伏せて返す（[mcp-tools.md](../api/mcp-tools.md)、[non-functional.md](../requirements/non-functional.md) §7）。ログのディレクトリは `app.startMCP` が `Kit.LogDir` に渡す |
| トークン | `service/mcptoken`、`repository/mcptoken` | 発行・失効・検証（§4） |
| 設定画面 | `service/mcpsettings`、`web/handler/settings/mcp`、`web/organisms/settings/mcp` | 「MCP サーバー」の区画（§9） |

`app.startMCP` が `server.New`（ツールの登録）と `Manager.Start`（リスナーを開く）を行い、`defer` で `Manager.Close` を呼ぶ。サービスを閉じる前に止まる。MCP を開けなくてもアプリは動き続ける（ログに残し、設定画面に「待ち受けていません」と出す）。

## 3. 待ち受け先

- **別の `net.Listener` と別の `http.Server`**（gin を通さない専用の mux。パスは `/mcp` と `/mcp-files/` だけで、他は 404）。画面側のエンジンとは、ミドルウェア（LoopbackOnly・CSRF・SecurityHeaders・MaxBody）も、ポートも、停止のタイミングも共有しない。
- 既定は `127.0.0.1`、ポートは既定 8788（`--mcp-port` / `MAM_MCP_PORT`。使用中なら OS が選ぶ空きポート。実際のアドレスはログと設定画面に出す）。
- **バインドするアドレスは設定（データディレクトリの `mcp.json` の `bind_address`）で決める。許す値は「このマシンのネットワークインターフェースに割り当てられている IP」で、かつ「ループバック（127.0.0.0/8、::1）か `100.64.0.0/10`（tailnet）」に入るもの。** `0.0.0.0`・`::`・LAN やグローバルのアドレス・ホスト名・ポート付きの文字列は理由付きで拒否する（`bind.Validate`）。tailnet の IP が実在するかは、検証のたびに `net.InterfaceAddrs` で確かめる。
- 設定画面で保存すると、リスナーを**その場で切り替える**（`Manager.Rebind`）。新しいリスナーを先に開き、設定ファイルを書き、成功してから古いほうを閉じる（実行中のリクエストには 5 秒の猶予）。開けない・保存できないときは古いリスナーと設定のまま、理由を画面に出す。`127.0.0.1` のときもベアラートークンは必須。
- 起動時に保存済みのアドレスが使えない（Tailscale が未起動でインターフェースが無い、`mcp.json` が壊れている）ときは、**`127.0.0.1` で待ち受け**、理由を `Status.Problem` に持って画面に警告として出す。保存した選択は書き換えない（Tailscale が上がった次の起動で戻る）。選択より広いアドレスには決して開かない。
- Windows のウィンドウ版でも同じ。ウィンドウが閉じてアプリが終了すれば MCP も止まる（常駐しない）。

## 4. 認証

- すべてのリクエスト（`/mcp`）に `Authorization: Bearer <token>` が要る。ないもの・知らない値・失効済みは **401**（`WWW-Authenticate: Bearer`）で、MCP のセッションは作られない。トークンは SDK の `auth.RequireBearerToken` で検査し、トークン ID をユーザー ID にするので、**セッションは開いたトークンにだけ使える**（別のトークンがセッション ID を持ち込むと 403）。
- 値の形式は `mam_mcp_` ＋ 32 バイトの乱数の base64url（43 文字）。発行時に一度だけ画面に出し、**保存するのは SHA-256 だけ**（`mcp_token.token_hash`）。表示用に乱数部の先頭 8 文字（`token_prefix`）を持つ。値はログにも出さない。
- トークンは複数発行でき、個別に失効できる。一覧には名前・作成日時・最終利用日時・状態を出す。失効は即時に効く（次のリクエストが 401。開いているセッションも続かない）。セッションは 1 時間使われなければ SDK が閉じる。
- `last_used_at` は検証に成功したとき、トークンごとに最大 1 分に 1 回だけ書く（書き込みの間引き。書けなくてもリクエストは通る）。
- DB が漏れても、ハッシュからは値を作れないので接続できない。

## 5. Origin の検査

`/mcp` と `/mcp-files/` は、`Origin` ヘッダーがあるときに**リクエストの `Host` と同じホストでなければ 403**（`null`・解釈できない値も 403）。ネイティブの MCP クライアントは `Origin` を送らないので通る。ブラウザは必ず送るので、他のサイトのページが利用者のブラウザ経由でこのサーバーを叩けない（MCP 仕様の Streamable HTTP が求める DNS リバインディング対策）。検査はトークンの検査より前なので、他サイトのページにはトークンの有無すら分からない。ループバックで待ち受けているときの `Host` 検査（loopback 以外の `Host` は 403）は SDK が行う。`Host` の偽装による攻撃も、トークンを知らなければ 401 で終わる。

## 6. 長い操作（ジョブ）

生成・Judge・Repair などはすぐ **`job_id`** を返し（`mcpkit.JobRef`）、`get_job` で状態（`queued` / `running` / `succeeded` / `failed` / `cancelled`）・進捗（`message`・`done` / `total`）・結果（`result`。例: `take_ids`）・失敗の理由（`error`）を取る。何度でも問い合わせられ、終わったあとも同じ答えを返す。

- `job_id` は `(kind, target)` の組（例: `generate_shot`, `shot_id`）に対応する。サービスが永続ジョブ（`job` テーブル）の ID を返せるときはそれを `job_id` にする（再起動後も使える）。返せないときは `job_<UUIDv7>` を割り当てる。どちらも履歴（下の「履歴」）に残るので、再起動後も `get_job` で引ける。
- 状態は 2 か所から決める。`job` テーブルの行（`queued` / `running` / `failed` とその理由）と、種別ごとの **Probe**（永続化済みの状態、たとえば Shot の状態と開始後に作られた Take から進捗と結果を導く。副作用なし）。行が `queued` / `running` / `failed` なら行が勝ち、`succeeded` のときは Probe が見ている仕事が続いていれば Probe に従う。
- 種別 `generate_shot`（Shot の生成・Prompt Candidate Generation）の Probe は基盤が持つ: Shot が GENERATING / JUDGING / REPAIRING の間は `running`、止まって開始後の Take があれば `succeeded`（`take_ids`）、なければ `failed`。他の種別は各ツールのパッケージが `Jobs.RegisterKind` で足す。メモリ上の記録だけを読む種別（編集ステージの追跡、Codex エンジンの導入の状態）は `mcpkit.InMemory()` を付けて登録する。
- **履歴**: `job_id` を返した操作は、すべて `mcp_job_history` テーブルに 1 行残る（ER は [er-mcp.md](er-mcp.md)）。`Jobs.Run` / `RunResolved` が開始時に行を書き（`job_id`・種別・対象・対象の Project（対象の ID が Project・Shot・Take・Edit Timeline のときに引く。無ければ空）・`job` テーブルの行 ID・開始時刻・`queued`）、`get_job` が状態を取るたびに、見つけた状態（`state`・`message`・`done` / `total`・失敗の理由とコード・`finished_at`・終わった時刻 `ended_at`）を行へ書き戻す。`ended_at` は `job` テーブルが終了を記録していればその時刻（`finished_at`）、なければ mam が終了を最初に確かめた時刻。**状態の決め方は変えない**: `get_job` は今までどおり、行（`job` テーブル）と Probe から毎回決める。履歴が足すのは、メモリが持っていた `(job_id, kind, target, started_at, queue_job_id)` と、記録された終わりだけ。
- **再起動をまたぐ `get_job`**: メモリ上の `job_id` も履歴から `(kind, target, started_at, queue_job_id)` を戻して、Probe と行から答える。Probe が永続化済みの状態を読む種別（Shot・Take・Branch・ワークフローの実行など）は、再起動の前後で同じ答えになる。Probe が**メモリだけ**を読む種別（`InMemory`）で、この mam の起動より前に始まった操作は、Probe を呼ばず履歴の記録で答える: 終わりが記録されていればその状態・結果・失敗（例: Editor Judge の `verdict`）を返し、終わりが記録されていなければ `failed`（「mam が再起動したため、この操作の結果を確かめられません」）にして、その終わりを記録する。編集ステージは終わった時点で履歴に終わりを書くので（`Jobs.Observe`）、`get_job` を呼ばないまま mam を止めても終わりは残る。履歴にも `job` の行にもない ID は `not_found`。
- **相関 ID（`internal` の失敗）**: `mcpkit.ReportError` が、`MapError` の知らないエラー（`internal`）を作った時点で相関 ID `err_<UUIDv7>` を決める。原因を ERROR のログ行に**属性 `error_id`** つきで書き（ツールの失敗は `mcp tool failed`、編集ステージの失敗は `mcp editing stage failed`、ファイル URL の失敗も同じ）、返すエラーの文面にも `（error_id: err_…）` を付ける（原因は出さない）。ジョブでは、Probe が `Progress.ErrorID` で返し、履歴の `error_id` 列・`get_job` / `list_jobs` の `error_id` に出る。エージェントは `get_app_logs` の `attrs: {"error_id": …}` で原因の行を引く。すでに ID を持つエラーは再度ログに書かない。`job` テーブルの行の失敗（ジョブキューが書いた理由）は `<code>` も相関 ID も持たず、原因のログ行は `job_id` で引く。
- **一覧（`list_jobs`）**: 履歴を `started_at` の新しい順（同時刻は `id`）に、カーソル（最後のジョブの `started_at` と `id`）でページングして返す（`jobhistory.Repository.List`）。種別・状態・エラーコード・Project・開始時刻の範囲で絞る。一覧の前に、終わっていないジョブ（最大 200 件）を `get_job` と同じ方法で状態を確かめ直して履歴に書くので、誰も `get_job` を呼ばなかったジョブの終わりも検索に出る。
- **保存期間・件数**: **30 日**（ログの保持 `logging.maxAgeDays` と揃える）と **10,000 行**。開始時刻（`started_at`）が 30 日より古い行と、10,000 行を超えたぶん（`started_at` の古い順）を、行の追加と同じトランザクションと mam の起動時に消す（`jobhistory.Retention` / `MaxRows`）。以前のメモリ上の記録（24 時間・4096 件）の約 2.4 倍の件数で、1 日数百回の操作の 30 日ぶんを収められる。1 行 0.5 KB ほどなので、テーブルは 5 MB 前後に収まる。`started_at` に索引があり、一覧も削除も安い。

## 7. ファイル

添付のアップロードも動画・画像のダウンロードも、**トークン付きの HTTP URL** で行う（ツールの引数・戻り値に base64 は載せない）。URL は同じ MCP リスナーの `/mcp-files/<ticket>`。

- **チケット**は 24 バイトの乱数（base64url）。**方向（download / upload）・種別・1 つのリソース**に結び付き、**15 分**で切れ、メモリだけに持つ。ticket が分からない・切れた・使用済み・方向違いは区別せず同じ 404（`not_found`）。チケットを発行したトークンが失効したら、そのチケットは 401 になる。
- ダウンロード: `GET`（`HEAD`・Range 可）。`mcpkit.Files.DownloadURL(ctx, kind, id)` が URL を返し、ツールが `video_url` などとして結果に入れる。種別は `RegisterDownload` で足す（ファイルのパス、または書き出しながら送る `Write`）。繰り返し使える（15 分まで）。
- アップロード: `create_upload_ticket(purpose, params)` が URL・メソッド（`POST`）・`multipart/form-data`・フィールド名 `file`・必須ヘッダー（なし）・上限サイズ・curl の例を返す。紐付ける値（`project_id` など）は発行時に固定し、リクエストからは受け取らない。**1 回だけ**使える（サービスが拒否したとき・ファイルが無いときは使い切らず、直して再送できる）。検証は画面と同じ `service/upload.go`（拡張子・内容の署名・サイズ・件数）。用途は `RegisterUpload`（基盤が `import_bundle`・`reference_image` を持つ）。
- URL の基点は、クライアントがそのリクエストで使った `Host`（tailnet の名前や SSH トンネルのポートでも届く）。Authorization ヘッダーがあるときは有効なトークンでなければ 401（チケットだけでも通る）。

## 8. API キー

画面の「API キー」と同じ検証で、`list_credentials`（プロバイダーごとの設定済みか・末尾 4 文字まで。キーが短いと末尾は出さない: `credential.Hint`）・`set_credential`・`delete_credential`。**どのツールもキーの値を返さず**、エラーにも含めない（`service.CredentialService` に値を返すメソッドは無いまま。`Hint` だけが末尾を読む）。ログにも出さない。

## 9. 設定画面

`/settings/mcp`（分類「MCP サーバー」。`#mcp-panel`）。待ち受け先のカード（`#mcp-bind-row`。待ち受け中のアドレス `http://<addr>/mcp` と `Problem` の警告、「変更」で開くモーダル `GET /settings/mcp/bind/edit`・`PUT /settings/mcp/bind`。候補は `127.0.0.1` とこのマシンの tailnet の IP）、トークンの表（名前・先頭 8 文字・作成・最終利用・状態・失効ボタン `DELETE /settings/mcp/tokens/:id`）、トークンの発行のモーダル（`GET /settings/mcp/tokens/new`・`POST /settings/mcp/tokens`。発行の応答 201 は、モーダルの本文を発行したトークンの表示に替える。値は**この応答に一度だけ**出し、コピーのボタンが付く。モーダルを閉じると本文ごと消え、表の行・再読み込み・履歴には値が出ない。`Cache-Control: no-store`）。CSRF は他の画面操作と同じ（`X-CSRF-Token`）。ルートは `app` が MCP サーバーを起動したときだけ存在する。画面の作りは [../components/settings-screen-categories.md](../components/settings-screen-categories.md) §4。

## 10. セキュリティの根拠

- **なぜ別リスナーか**: 画面のサーバーは `127.0.0.1` 固定で、LoopbackOnly と CSRF がその前提で作られている。MCP を同じエンジンに載せると、tailnet からの接続のためにその前提を緩めることになり、画面のルートまで外に出る。別のリスナーなら、tailnet に開くのは `/mcp` と `/mcp-files/` だけで、画面と `/api/v1` は `127.0.0.1` のまま。
- **なぜ 0.0.0.0 を許さないか**: ベアラートークンだけが LAN・インターネットとの間の防壁になってしまう。tailnet は WireGuard で暗号化と相互認証がされるので、平文の HTTP でも盗聴されない。
- トークンはハッシュで保存し、チケットは短命・単一リソース・メモリだけ。キーの値は MCP からは読めない。

## 11. テスト

トークン（発行でハッシュだけ保存・失効・未知/失効は拒否・最終利用の間引き）、アドレス検証（インターフェース一覧を差し込める純関数）、`Manager`（切り替え・失敗時の据え置き・起動時のフォールバック・Close）、認証と Origin（`httptest`）、ジョブ（再起動をまたぐ `get_job`・履歴の保存期間と件数の上限・`list_jobs` の絞り込みとページング・相関 ID がログ行・履歴・`get_job` / `list_jobs` に出る）、チケット（期限・単一リソース・方向・単回・トークン失効）、ツール（インメモリの MCP クライアント `mcpkit/mcptest`）、設定画面（CSRF・値の一回表示）、ログの読み取り（絞り込み・ローテーション済みの世代と圧縮・秘密を返さない・サイズの上限。`mcptest` と実サーバー）、マイグレーション（`mcp_token`・`mcp_job_history`）の up / down。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。別リスナー・待ち受け先の規則・トークン・Origin 検査・ジョブ・ファイルのチケット・API キー・設定画面を定めた | Issue #830。画面の全操作を MCP（Streamable HTTP、tailnet 経由）で操作できるようにする基盤 |
| 1.1 | 2026-10-25 | §9 を、待ち受け先のカード・トークンの表・2 つのモーダルの作りに改めた（`GET /settings/mcp/bind/edit`・`/settings/mcp/tokens/new`）。トークンの値を一度だけ出す決まりは変えていない | Issue #847 ②-6 |
| 1.2 | 2026-10-31 | §6 に、`job_id` を返した全種別の履歴（`mcp_job_history`）・再起動をまたぐ `get_job`・保存期間 30 日と 10,000 行を足し、メモリ上・24 時間・4096 件の記述を改めた。`get_job` の状態の決め方（`job` テーブルの行と Probe）は変えない | Issue #890（親 #889）。メモリ上の `job_id` が再起動で消え、エージェントが失敗をたどれなかった |
| 1.3 | 2026-10-31 | §2 に、ログを読む `get_app_logs`（`logging.Reader`・`mcp/tools/logs`）を足した | Issue #891（親 #889）。エージェントが `internal` エラーの原因をログからたどれるようにする |
| 1.4 | 2026-10-31 | §6 に、`internal` の失敗の相関 ID（`error_id`。ログの行・エラーの文面・履歴・`get_job` / `list_jobs`）と、履歴の一覧 `list_jobs` を足した | Issue #892（親 #889）。`internal` の失敗から原因のログ行をたどれるようにし、最近の失敗を探せるようにする |
