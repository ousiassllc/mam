# API 共通エラーコード

[endpoints.md](endpoints.md) §0 の「共通ビジネスエラーコード」。HTTP ステータスへの写像はハンドラ共通の `statusOf`（`backend/internal/web/handler/errors.go`）で、全ルートに共通する。各ルートの行には、そのルート固有の条件だけを書く。本文の行数を増やさないよう別ファイルに分けている（環境構築 spec の Linterly 節）。

| HTTPステータス | 意味 |
|---|---|
| 400 | 本文が読み取れない。(1) 不正なパーセントエンコードなどで解析できない `application/x-www-form-urlencoded` フォーム: `MaxBody` ミドルウェアが全ルートで返す（非 HTMX は JSON `{"error":"Bad Request"}`、HTMX はトースト）。(2) multipart ではない・途中で切断された・読み取りが 1 分止まった（無通信、非機能要件 §3）`multipart/form-data`: `MaxBody` はアップロードルートの multipart をそのまま通すので、`readMultipart` を呼ぶアップロードルートのハンドラが「送信内容の形式が正しくありません」としてトースト／フォーム再描画で返す（JSON ではない） |
| 413 | リクエスト本文が上限を超えた（`MaxBody` ミドルウェアが 1MiB。`readMultipart` を呼ぶアップロードルートの `multipart/form-data` の本文だけが各上限で（`POST /projects/import` は 8 GB + 1 MiB、§2.1。参照画像 `POST /projects/:id/references` は 20 MB + 1 MiB、音声サンプル `POST /projects/:id/audio/voices` は 10 MB + 1 MiB、Review Response `POST /projects/:id/reviews/feedback` は 1 MiB + 1 MiB、Review Package `POST /reviews/import` は 2 GiB + 2 MiB、チャット作成 `POST /projects` とチャット発言 `POST /projects/:id/messages` は添付 8 件 × 動画上限 300 MB + 1 MiB（約 2.34 GiB）。つまり「ファイルが各ファイルの上限を超え、本文全体は上限内」のときだけ各ルートの 422 になり、本文全体が上限を超えれば 413 になる）。`Content-Length` が上限を超えていれば本文を読む前に 413 にする、アップロードルートでも multipart 以外（urlencoded など）の本文は 1MiB のまま。Content-Type を `multipart/form-data` にしてもそれ以外のルートの上限は外れない。宣言された長さが上限を超える本文は読まずに 413） |
| 402 | Project/Shot budget ceiling 超過（F-22） |
| 403 | Brand/Safety Constraint 違反、CSRF トークンの不正、または非ループバックからのアクセス |
| 404 | 対象（Project・Shot・Take・カードなど）が見つからない。他の Project の対象も 404 |
| 409 | 状態が合わず実行できない（多重実行・状態変化の防止）。対象 Shot が既に生成中、同じ Shot への Continue の提案・確定が進行中、別の編集処理（Rough Cut・判定・音声・Final QC・書き出し）や Take の Judge が実行中、カードが応答済みか状態が変わっている、前提の欠落（Judge 対象の動画ファイルがない・Repair 対象の Take がまだ Judge されていない）、Judge に合格済みの Take の Repair |
| 423 | LOCK されたパラメータ/State への更新要求 |
| 422 | 入力の検証エラー（必須項目の欠落・範囲外・形式不正など）。フォームを持つルートはエラー付きで再描画する |
| 429 | 外部モデル API（Model Adapter・Reasoning LLM・ElevenLabs 等）のレート制限。同期で応答できる経路では `service.ErrRateLimited`（`AIError` が 429 を包んでいれば一致）が 429 になる。生成ジョブ内の 429 はチャットの失敗理由として通知する |
| 500 | 想定外のエラー（上のどれにも当たらないもの）。内容は応答に出さず「内部エラーが発生しました」とだけ表示し、詳細はログに記録する |
| 502 | 外部の生成モデル・Reasoning LLM の失敗（`service.AIError`。レート制限 429 を除く）。失敗の理由を表示する |
| 503 | 生成機能が有効になっていない（API キーと設定が未構成。`service.ErrUnavailable`）。ffmpeg が無いときのサムネイル取得など、各ルートが個別に返す 503 もある |
