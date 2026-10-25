# API 仕様: モデルマネージャー画面

ローカルの画像・動画モデルの導入と試し生成の画面 `GET /model-manager`（Issue #833）。ComfyUI エンジン・モデルのカタログ・ライセンスの同意・試し生成の意味は [../architecture/model-manager/model-manager.md](../architecture/model-manager/model-manager.md)。画面の操作は、同じサービス（`modelmanager.Service`）を呼ぶ MCP ツールでもできる（[mcp-tools-model-manager.md](mcp-tools-model-manager.md)）。画面の構成は [../components/model-manager-screen.md](../components/model-manager-screen.md)。

`modelmanager.Service` を持たない router（ヘッドレスのテストなど）では、画面もルートも無い。入口は、プロジェクト一覧（`GET /`）の「モデルマネージャー」のリンクと、設定画面（`GET /settings`）の見出しの下のリンク。

## 1. ルート

すべて CSRF 必須（GET を除く）。操作は区画（`outerHTML`）を返す。ジョブ（導入・試し生成）は 202 で、区画が進捗を 2 秒ごとに取り直す（`hx-trigger="every 2s"`。動いているジョブがなくなると止まる。WebSocket は使わない）。拒否は、ページのトーストではなく、区画の中のメッセージで返す（下の「エラーの対応」）。

| Method | Path | 内容 |
|--------|------|------|
| GET | `/model-manager` | 画面（HTMX のリクエストには、ページの枠なしの本体 `#model-manager-body`） |
| GET | `/model-manager/engine` | エンジンのカード（`#mm-engine`）。導入中・起動中は自分で取り直す。クエリ `job` は報告する導入ジョブで、終わったら結果（導入した／失敗の理由）を出し、モデルの一覧を `hx-swap-oob` で描き直す |
| GET | `/model-manager/engine/gpu` | GPU の検出結果と導入フォーム（`#mm-gpu`）。未導入のカードが読み込み時に取る |
| POST | `/model-manager/engine/install` | エンジンの導入（ジョブ）。フォーム `variant`（省くと GPU の推奨）・`cpu=1`（GPU が無いときの明示的な CPU 実行）。202 |
| POST | `/model-manager/engine/start` | 起動（ComfyUI が応答するまで待たずに 202。カードが起動中を取り直し、失敗はカードに理由とログの末尾を出す） |
| POST | `/model-manager/engine/stop` | 停止 |
| PUT | `/model-manager/engine/autostart` | 自動起動。フォーム `on`（`1` / `0`） |
| DELETE | `/model-manager/engine` | エンジンの削除（モデルのファイルは残る）。導入中は 409 |
| POST | `/model-manager/engine/detect` | 既存の ComfyUI の検出（127.0.0.1 の 8188・8000）。結果（`#mm-detect`）だけを返す。見つかったものは設定の「ローカル・代替モデル」に接続先として登録して使う（mam は起動も停止もしない） |
| POST | `/model-manager/jobs/:job/cancel` | 動いている導入の中断（エンジンのカードか一覧を返す）。ジョブは失敗で終わり、ダウンロード済みの分は残って再開できる。動いていないジョブは 404 |
| GET | `/model-manager/models` | モデルの一覧（`#mm-catalog`）。導入中は自分で取り直す。クエリ `jobs`（カンマ区切り）は報告するジョブ。終わったら「導入しました」／失敗の理由を出し、試し生成の区画を `hx-swap-oob` で描き直す |
| GET | `/model-manager/models/:id/consent` | ライセンスの確認ダイアログ（`#mm-dialog` に差し込む）。同意が要らないモデルは 404 |
| GET | `/model-manager/adult/confirm` | 成人向けモデルの年齢確認と注意書きのダイアログ（`#mm-dialog` に差し込む） |
| PUT | `/model-manager/adult` | 設定「成人向けモデルを表示する」（既定オフ）。フォーム `on`（`1` / `0`）。**オンにする最初の 1 回**は、ダイアログの `notice_version`（`Service.AdultContent().NoticeVersion`）と `agree=1`（18 歳以上の確認と注意書きへの同意）が要る。確認の記録（日時・注意書きの版）が `state.json` に残り、以後のオン／オフは `on` だけでよい。確認の記録が無く `notice_version` も無いオンは 409、`agree` が無い・版が違うのは 422（ダイアログに理由を出し、ゲートは開かない）。200 でカード（`#mm-adult`）を返し、モデルの一覧・試し生成の区画を `hx-swap-oob` で描き直し、ダイアログを閉じる。**確認が無い間は、一覧（`GET /model-manager/models`）に成人向けのモデルが出ず、試し生成のフォームにも導入済みの成人向けモデルが出ず、名指しの要求は 404** |
| POST | `/model-manager/models/:id/install` | モデルの導入（ジョブ）。同意が要るモデルは、フォーム `agree=1`・`license_id`・`license_version` で同意を渡す。**同意の記録が無ければ、画面を経ずに叩いても 409 で、ジョブは作らない**（サービスが強制する）。**成人向けのモデルは、年齢確認が無ければ同意を渡しても 409**（下の `adult`）。202 で一覧を返し、ダイアログを閉じる |
| DELETE | `/model-manager/models/:id` | モデルの削除。画像の用途・Quality Policy に使われていれば 422、導入中は 409、未導入は 409。他のモデルと共有するファイルは残る |
| GET | `/model-manager/dialog` | 空（ダイアログを閉じる） |
| PUT | `/model-manager/hf-token` | Hugging Face のトークンの保存。フォーム `token`。応答は「設定済み（末尾 4 文字）」の状態だけで、**値は返さない** |
| DELETE | `/model-manager/hf-token` | トークンの削除（未設定でもエラーにならない） |
| GET | `/model-manager/external` | 外部の ComfyUI の確認（`#mm-external`）。クエリ `endpoint`（種類 ComfyUI の接続先の名前）を渡すと、その ComfyUI が持つモデルファイルと、カタログのモデルに足りないファイルを出す（読むだけ。導入の操作は無い）。同梱の `bundled-comfyui` は 422、未登録は 404、到達できないときは 502 |
| GET | `/model-manager/trials` | 試し生成の一覧（`#mm-trials`）。生成テストの履歴のうち、この画面のローカルのモデルの結果を新しい順に 20 件。待機中・生成中のものがあれば自分で取り直す |
| GET | `/model-manager/trials/form` | 選んだモデル向けの試し生成フォーム（動画モデルは解像度・長さ・ネガティブプロンプト、参照画像を使えるモデルだけ画像の欄）。入力済みのプロンプトは保ったまま差し替える |
| POST | `/model-manager/trials` | 試し生成の開始（生成テストの 1 件のバッチ。見積り（ローカルは 0 USD）を確認したものとして実行する）。multipart: `model_id`・`prompt`・`negative_prompt`・`aspect_ratio`・`resolution`・`duration_seconds`・`reference`（任意のファイル。PNG・JPEG・WebP、20 MiB まで）。モデルの検査（プロンプトの長さ・動画の長さと解像度・参照画像の可否・縦横比・画像の種類）に通らなければ 422 でジョブを作らない。202 |
| DELETE | `/model-manager/trials/:id` | 試し生成の結果の削除（`:id` は生成テストの結果 ID。履歴からも消える）。動いているものは 409、無ければ 404 |
| GET | `/model-manager/trials/:id/file` | 結果のファイル（画像・動画）。保存した MIME のまま `Content-Security-Policy: sandbox` を付けて返す（Take のメディアと同じ `serveMedia` の扱い）。完了していない・無い結果は 404（`/generation-test/results/:id/file` と同じ） |

`POST /model-manager/trials` は、`middleware.MaxBody` のアップロード用ルートの一覧に載せ、参照画像のために本文の上限を 21 MiB まで広げている（本文の読み取りはハンドラが `MaxBytesReader` で制限する）。

## 2. エラーの対応

サービスの拒否は `mmview.Refuse`（`service/modelmanager/mmview`）が種類とメッセージに写す。画面と MCP ツールで同じ文面になる。

| サービスのエラー | HTTP | MCP の code | 画面のメッセージ（要約） |
|-----------------|------|-------------|------------------------|
| `*InvalidError`・`ErrNoGPU`・`engine.ErrVariant` | 422 | `validation` | サービスのメッセージ（GPU が無いときは案内。CPU は明示的に選ぶ） |
| `ErrConsentRequired` | 409 | `conflict` | 「このモデルのライセンスに同意してから導入してください」（同意ダイアログを開いたままにする） |
| `ErrAdultNotConfirmed` | 409 | `conflict` | 「成人向けモデルです。年齢の確認と注意書きへの同意を済ませ、「成人向けモデルを表示する」をオンにしてください」（導入・同意・読み取りのどれも、何も記録せず何もダウンロードしない） |
| `ErrBusy` | 409 | `conflict` | 導入・試し生成が実行中 |
| `ErrPinned` | 409 | `conflict` | `--model-config` で固定されているため、導入・削除できない |
| `ErrUnsupported` | 409 | `conflict` | 同梱の ComfyUI は Windows 版だけ。外部の ComfyUI の URL を使う |
| `ErrEngineNotInstalled`・`ErrAlreadyInstalled`・`ErrNotInstalled`・`ErrEndpointMoved`・`fetch.NoSpaceError`（空き容量不足）・`store.ErrNotWritable`・`engine.ErrBusyRunning` | 409 | `conflict` | それぞれの理由（空き容量は必要量・空き・場所） |
| `ErrNotFound` | 404 | `not_found` | 対象が見つかりません |
| それ以外 | 500 | `internal` | 「処理できませんでした」（原因はログだけ。画面に出さない） |

## 3. 画面

区画は上から: **ComfyUI エンジン**（カード `#mm-engine`）・**モデル**（一覧 `#mm-catalog` とダイアログ `#mm-dialog`）・**試し生成**（`#mm-trial`: フォームと結果 `#mm-trials`）・**Hugging Face のトークン（任意）**（`#mm-hf`）・**外部の ComfyUI**（`#mm-external`）。

- **エンジンのカード**: 状態は 未導入 / 導入中（進捗バー・バイト数・「中断する」）/ 導入済み（版・ビルド・CPU 実行か）+ 停止中 / 起動しています / 起動中（`127.0.0.1:<port>`）/ 起動に失敗（理由とログの末尾）。導入済みの版が固定版と違えば「更新する」。ボタン: 起動・停止・自動起動の切り替え・エンジンを削除。**Windows 以外（ブラウザ版・サーバー版）では導入のボタンを出さず**、外部の ComfyUI の URL の案内を出す。`mam.exe` のあるフォルダに書き込めないときも同じ（理由を出す）。導入先 `<exe のディレクトリ>\comfyui` と空き容量を出し、「アンインストールするとここも消える」と書く。ComfyUI は GPL-3.0 の別プログラムで mam に含まれないこと、取得元（ソース）とライセンスの URL を出す。
- **GPU の検出**（未導入のとき、読み込み時に取る）: アダプターの名前と VRAM、推奨するビルド（確度が低いときは「推定です」）。ビルドの選択（サイズと必要な空き容量）。**対応する GPU が無いとき**は推奨が空で、案内（外部の ComfyUI を使う）を出し、CPU 実行はチェックボックスの明示的な選択にだけ表示する（自動では何も入れない）。
- **既存の ComfyUI の検出**: 見つかった URL・版・GPU（ComfyUI Desktop の印）と、その URL を設定の「ローカル・代替モデル」に登録する案内。
- **モデルの一覧**: 種別（画像 / 動画）・ライセンスの同意が要るモデルの印・導入の状態（未導入 / 導入中 / 導入済み / ファイルが不足）・説明・大きさ・VRAM の目安（出典。mam の推定は「GPU では未確認」と書く）・ライセンス名（URL へのリンク）・**商用利用の可否**・導入に必要な空き容量（足りなければ赤字で「導入できません」とし、ボタンを無効にする）・ファイルの一覧（他のモデルと共有するものは「使うモデルが残る間は消えません」）・導入されるモデルの識別子とワークフローの検証の範囲（「構造テストのみ・GPU 実機未検証」）。エンジンが無い・Windows 以外・設定ファイルが固定のときは、理由を出してボタンを無効にする。導入は「導入」・「不足分を取得して復旧」、進行中は進捗バーと「中断」、導入済みは「削除」（確認つき）。
- **ライセンスの同意ダイアログ**: 同意が要るモデルでは「ライセンスを確認して導入」が `#mm-dialog` にダイアログを開く。ライセンス全文へのリンク・要約・商用利用の可否・**地域の制限**（例: EU・英国・韓国では使えない）・注意書き・「モデルの重みは mam が再配布しない」の説明を出し、チェックボックスに同意してから「同意して導入」。同意は ID と版を送り、サービスが記録して強制する。
- **試し生成**: モデルの選択（導入済みの同梱モデル・登録済みの外部 ComfyUI・OpenAI 互換のモデル）・プロンプト・縦横比・（動画）ネガティブプロンプト・解像度・長さ・（参照画像を使えるモデルだけ）画像。選べるモデルが無いときは理由を出す。結果は新しい順で、動いているものは進捗、完了は画像（`<img>`）か動画（`<video>`）と「ファイルを開く」、失敗は理由。削除ボタン（確認つき）。課金は無く、Take・コスト台帳には入らないと書く。
- **トークン**: 設定済みか（末尾 4 文字）だけを出す。入力欄は `type=password`。
- **外部の ComfyUI**: 接続先の選択と「導入済みのモデルを確認」。結果は、カタログのモデルごとに「そろっています」／「足りないファイルがあります（足りない: …）」、フォルダごとのファイル。「外部の ComfyUI へのモデルの導入は、その ComfyUI の側で」と書く。

## 4. テスト

`handler/modelmgr` の画面テスト（`mmtest` の偽の GitHub・Hugging Face・エンジンのプロセス、`comfyuitest` の偽の ComfyUI で、HTMX のリクエストを CSRF のクッキーとヘッダーつきで送る）: エンジンの導入・進捗・完了・起動・停止・自動起動・削除・中断と再導入、GPU が無いとき、Windows 以外、空き容量不足、同意の強制（ダイアログを経ない要求の 409・別のライセンスの 422・同意後の導入）、固定された設定ファイル、モデルの導入・削除・共有ファイル、トークンが画面に出ないこと、外部 ComfyUI の一覧、試し生成（開始・進捗・結果ファイルの sandbox・削除・拒否・失敗）、CSRF のトークンが無い要求の 403。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成（ルート・エラーの対応・画面・テスト） | Issue #833。モデルマネージャーの画面 |
| 1.1 | 2026-10-26 | 試し生成のルートを、生成テスト（[endpoints-gentest.md](endpoints-gentest.md)）の 1 件のバッチを動かすものに改めた（一覧は履歴の結果・`jobs` クエリを廃止・`:id` は結果の ID）。試し生成の区画に生成テスト画面へのリンクを足した | Issue #835 |
| 1.2 | 2026-10-25 | 成人向けモデルの年齢確認のルート（`GET /model-manager/adult/confirm`・`PUT /model-manager/adult`）と、`ErrAdultNotConfirmed`（409）を足した。モデルの一覧・導入・試し生成の選択肢は、確認が無い間は成人向けのモデルを出さず、導入は 409 | Issue #841 |
