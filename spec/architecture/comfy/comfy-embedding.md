# ComfyUI のノード画面の埋め込み

mam の中に ComfyUI のノード画面（`Comfy-Org/ComfyUI_frontend`）を出し、ワークフローを作り・直せるようにする（Issue #834）。ワークフローの保存・版・生成との結びつきは [comfy-workflows.md](comfy-workflows.md)。改訂履歴は [comfy-embedding.changelog.md](comfy-embedding.changelog.md)。

## 1. 決めたこと

**案 B-1: ComfyUI のオリジンを iframe で開く。** mam の CSP に、そのときのノード画面が使う ComfyUI のオリジン**だけ**を `frame-src` として足す。案 B-2（mam が `/comfy/` でリバースプロキシし、同じオリジンで出す）は採らない。

|比べること|B-1（採用）|B-2（不採用）|
|---|---|---|
|ComfyUI の JS が読めるもの|ComfyUI 自身のオリジンだけ。mam の `<meta name="csrf-token">` は別オリジンなので読めない|mam と同じオリジンで動くので、`<meta name="csrf-token">` と mam の API を読める|
|mam の CSP（`script-src 'self'`）|ComfyUI のページには mam の CSP は掛からない（ComfyUI が自分のオリジンで配る）|ComfyUI のスクリプトも `default-src 'self'` に縛られ、動かすには緩める必要がある|
|`LoopbackOnly`・CSRF|ComfyUI の画面から mam への書き込みは、`Origin` が mam と違うので `LoopbackOnly` が 403 にする|ComfyUI の画面の POST/WebSocket が CSRF・`Origin` 検査に引っかかる（トークンを付ける改造が要る）|
|外部の ComfyUI（tailnet）|そのまま開ける（§4 の条件付き）|プロキシ経由で WebSocket を中継する必要がある|

ComfyUI v0.42.1 のサーバーは `X-Frame-Options` も `frame-ancestors` も送らない（ソースを確認済み）ので、iframe に入れられる。`frame-ancestors 'none'`（mam 自身が枠に入れられないようにする）は変えない。

## 2. CSP の足し方（`middleware.AllowFrame`）

- 全ページの CSP は今までどおり `frame-src` を持たない（子フレームは `default-src 'self'` に従い、他サイトは入らない）。
- `GET /nodes` のハンドラだけが、描画する ComfyUI のオリジンを `middleware.AllowFrame(c, origin)` で足す。`frame-ancestors` の直前に `frame-src <origin>` を 1 つだけ入れる。**1 つのオリジン**（`scheme://host[:port]`、`http` か `https`、パス・資格情報・ワイルドカードなし）以外は足さない（足せなければ `false` を返し、ページは理由の表示になる）。
- オリジンは要求ごとに決める: 同梱版は `modelmanager` が今起動しているエンジンの URL（ポートは起動のたびに変わりうる）、外部は選んだ接続先の `base_url` のオリジン。設定に無いオリジン・起動していないエンジン・つながらない外部サーバーには足さない。
- テスト（`middleware`・`handler/workflows`）は両方向を確かめる: 許す対象のオリジンが `frame-src` に入る／入らない場合（エンジンが止まっている・つながらない・未登録の接続先・他のページ）は `frame-src` が無い／`frame-ancestors 'none'` が変わらない。

## 3. iframe の URL のホスト

`LoopbackOnly` は `localhost` と `127.x` の両方の `Host` を受ける。ブラウザは `localhost` と `127.0.0.1` を**別のサイト**として扱い、`Sec-Fetch-Site: cross-site` を付ける。ComfyUI の `origin_only_middleware` はそれを 403 にする。そこで iframe の URL は、**mam への要求の `Host` と同じ文字（`127.0.0.1` か `localhost`）のホスト**で組む（ポートは ComfyUI のもの）。同じサイト内なら ComfyUI 自身の `fetch`・WebSocket の `Origin` も自分と一致して通る。この規則は、同梱版と、`localhost` / `127.x` で登録した外部の接続先（同じ PC の ComfyUI）に適用する（登録した URL のホストが `127.0.0.1` でも、ページを `localhost` で開いていれば `localhost` で開く）。それ以外のホスト（tailnet など）は登録した URL のまま（§4）。

## 4. 外部の ComfyUI

- tailnet などの外部サーバー（`https://box.tailnet.ts.net:8188` など）は、mam のページから見ると別サイトで、ComfyUI は既定で 403 にする（`--enable-cors-header` が無いとき）。mam は**ページを描く前に**サーバー側から同じ種類の要求（`Sec-Fetch-Site: cross-site`・`Origin` は mam のオリジン）を 1 回送り、403 なら iframe を描かずに次を出す: 理由、ComfyUI を `--enable-cors-header <mam のオリジン>` で起動し直す案内、「ブラウザで開く」リンク（新しいタブで ComfyUI 自身のページを開く。mam の外なので制限を受けない）。
- iframe は描くが中身が出ない場合（ネットワーク・WebView の制約）は、Lit のコンポーネント `mam-comfy-frame`（`static/src/components/comfy-frame`）が `load` が一定時間（既定 20 秒）来ない・`error` のときに同じ案内と「ブラウザで開く」を出す（インラインスクリプトは CSP が禁じるので、検知はこのコンポーネントが行う）。

## 5. つながらないとき

ノード画面は ComfyUI の状態を `NodeTarget` で決める: `ready`（開ける）／ `stopped`（同梱版が入っているが止まっている: 「起動」ボタン `POST /nodes/start`）／ `starting` ／ `failed`（理由とログの末尾）／ `not_installed`（同梱版が未導入）／ `unsupported`（Windows 版以外）／ `unreachable`（外部につながらない）／ `rejected`（§4）。`ready` 以外は `frame-src` を足さず、理由を出す。同梱版が未導入・失敗のときは**モデルマネージャー（`/model-manager`、#833）への導線**を出し、外部の接続先の登録は設定画面の「ローカル・代替モデル」へ。

## 6. CSRF・オリジンの分離（B-1 でも守られる理由）

- `mam_csrf` クッキーは HttpOnly・SameSite=Strict。クッキーはホストで区切られポートでは区切られないので、ブラウザは `127.0.0.1:<comfy のポート>` にも送るが、ComfyUI のページの JS は読めず、mam の `<meta name="csrf-token">` も（別オリジンなので）読めない。
- ComfyUI のページから mam への `GET` は届いても、応答は CORS ヘッダーが無いので JS から読めない。`GET/HEAD/OPTIONS` 以外と WebSocket は `LoopbackOnly` が `Origin` を mam 自身のオリジンに限るので 403 になる。つまり ComfyUI（とそのカスタムノード）は mam の API を操作できない。
- 逆向き（mam → ComfyUI）は、mam のサーバーが送る要求（`Origin` なし）なので ComfyUI の `origin_only_middleware` を通る。mam のブラウザ側の JS は ComfyUI を直接呼ばない（`connect-src` は変えない）。

## 7. Windows（WebView2）

mam のウィンドウは mam の http URL をそのまま開く（`desktop.md` §1）ので、iframe が mam のコードに止められることはない。WebView2 は Chromium で、別オリジンの iframe とその中の WebSocket を扱える（文書上の制限は見当たらない）。**WebView2 での iframe と WebSocket の動作は、この環境（Windows 機なし）では確かめていない。** 実機で確かめる項目として残す（Issue の完了条件）。うまく動かない場合の退路は §4 の「ブラウザで開く」。
