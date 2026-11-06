# API 仕様: モデルマネージャー画面

ローカルの画像・動画モデルの導入の画面 `GET /model-manager`（Issue #833）。ComfyUI エンジン・モデルのカタログ・ライセンスの同意の意味は [../architecture/model-manager/model-manager.md](../architecture/model-manager/model-manager.md)。画面の操作は、同じサービス（`modelmanager.Service`）を呼ぶ MCP ツールでもできる（[mcp-tools-model-manager.md](mcp-tools-model-manager.md)）。画面の構成は [../components/model-manager-screen.md](../components/model-manager-screen.md)。

> **状態: 仕様のみ（未実装。Issue #1100）**: 「試し生成」（`/model-manager/trials/…`）は廃止する。導入したモデルを試すのは生成テスト画面（`GET /generation-test`。[endpoints-gentest.md](endpoints-gentest.md)）だけ。下の表・§3・§4 からは試し生成を外してある（実装が追いつくまで、コードには残っている）。

> **状態: 仕様のみ（未実装。Issue #1101）**: 画面を**分類ごとのサブページ**に分ける。`GET /model-manager` は概要になり、区画は `GET /model-manager/<slug>`（`comfyui`・`image-video`・`custom-nodes`・`local-runtimes`・`face-check`・`hf-external`）の分類ページに移る。下の §1 の表に概要と分類ページのルートを足し、§3 の区画の並びを分類の表に直し、§4 に受け入れ条件を足した。区画の部分ルート（`/model-manager/engine/…`・`/models/…` ほか）は、パス・フォームともそのまま。構成は [../components/model-manager-screen.md](../components/model-manager-screen.md) の「分類のサブページ（#1101）」。

`modelmanager.Service` を持たない router（ヘッドレスのテストなど）では、画面もルートも無い。入口は、プロジェクト一覧（`GET /`）の「モデルマネージャー」のリンクと、設定画面（`GET /settings`）の見出しの下のリンク。

## 1. ルート

すべて CSRF 必須（GET を除く）。操作は区画（`outerHTML`）を返す。ジョブ（導入）は 202 で、区画が進捗を 2 秒ごとに取り直す（`hx-trigger="every 2s"`。動いているジョブがなくなると止まる。WebSocket は使わない）。拒否は、ページのトーストではなく、区画の中のメッセージで返す（下の「エラーの対応」）。

| Method | Path | 内容 |
|--------|------|------|
| GET | `/model-manager` | 概要（#1101。`Overview`: 分類ごとのカードと導入状態のまとめ。`#mm-overview`）。HTMX のリクエスト（履歴の復元でないもの）には、ページの枠なしの本体 `#model-manager-body`（概要では `#mm-overview` を含む） |
| GET | `/model-manager/overview` | 概要の区画 `#mm-overview` だけ（#1101。更新ボタンとポーリング用）。動いている導入があるあいだだけ自分で取り直す（`every 2s`）。常に 200 |
| GET | `/model-manager/<slug>` | 分類ページ（#1101）。`<slug>` は `comfyui`（`#mm-engine` ほか）・`image-video`（`#mm-adult`・`#mm-catalog` ほか）・`custom-nodes`（`#mm-nodes`）・`local-runtimes`（`#mm-runtimes`）・`face-check`（`#mm-face-models`）・`hf-external`（`#mm-hfi`・`#mm-hf`・`#mm-external`）・`companion`（`#mm-companion-index`・`#mm-ffmpeg`・`#mm-facefusion`。#1106。ffmpeg と FaceFusion のサービスのどちらかがあるとき）。登録は `mmcat.All()`（新設）の一覧から 1 本ずつ（`:slug` のパラメーターのルートにしない）。HTMX のリクエスト（履歴の復元でないもの）には `#model-manager-body` だけ。実行環境・ノード・顔チェックのサービスが無い構成では、その `<slug>` を登録しない（404）。`<slug>` は下の部分ルートの名前と重ねない |
| GET | `/model-manager/engine` | エンジンのカード（`#mm-engine`）。導入中・起動中は自分で取り直す。クエリ `job` は報告する導入ジョブで、終わったら結果（導入した／失敗の理由）を出す（#1101 で、モデルの一覧の `hx-swap-oob` は廃止。一覧は別のページ） |
| GET | `/model-manager/engine/detail` | エンジンのカードが開く詳細モーダルの本体（Issue #970）。説明・この PC で使えない理由（Windows 版と Apple Silicon の Mac（macOS 14 以降）版だけ・書き込めない・ファイルが見つからない）・導入済みの版と build・待ち受け（起動中だけ）・導入先と空き・出典とライセンス。応答は `modal.Frame` で、ページに 1 つの `<dialog>` に入る |
| GET | `/model-manager/engine/gpu` | GPU の検出結果と導入フォーム（`#mm-gpu`）。未導入のカードが読み込み時に取る |
| POST | `/model-manager/engine/install` | エンジンの導入（ジョブ）。フォーム `variant`（省くと GPU の推奨）・`cpu=1`（GPU が無いときの明示的な CPU 実行）。202 |
| POST | `/model-manager/engine/start` | 起動（ComfyUI が応答するまで待たずに 202。カードが起動中を取り直し、失敗はカードに理由とログの末尾を出す） |
| POST | `/model-manager/engine/stop` | 停止 |
| PUT | `/model-manager/engine/autostart` | 自動起動。フォーム `on`（`1` / `0`） |
| DELETE | `/model-manager/engine` | エンジンの削除（モデルのファイルは残る）。導入中は 409 |
| POST | `/model-manager/engine/detect` | 既存の ComfyUI の検出（127.0.0.1 の 8188・8000）。結果（`#mm-detect`）だけを返す。見つかったものは設定の「ローカル・代替モデル」に接続先として登録して使う（mam は起動も停止もしない） |
| POST | `/model-manager/jobs/:job/cancel` | 動いている導入の中断（エンジンのカードか一覧を返す）。ジョブは失敗で終わり、ダウンロード済みの分は残って再開できる。動いていないジョブは 404 |
| GET | `/model-manager/models` | モデルの一覧（`#mm-catalog`）。導入中は自分で取り直す。クエリ `jobs`（カンマ区切り）は報告するジョブ。終わったら「導入しました」／失敗の理由を出す（ほかの区画は描き直さない） |
| GET | `/model-manager/models/:id/detail` | カードが開く詳細モーダルの本体（`modal.Frame`。ページに 1 つの `<dialog id="mam-modal">` の本体に入れる。Issue #954）。説明・状態・商用利用の注意・ライセンス・VRAM・原本の場所・ファイルごとの大きさと SHA-256。未知の ID は 404、年齢確認前の成人向けは 409（どちらも理由だけのモーダル。隠しているモデルの中身は出さない） |
| GET | `/model-manager/models/:id/consent` | ライセンスの確認ダイアログ（`#mm-dialog` に差し込む）。同意が要らないモデルは 404 |
| GET | `/model-manager/adult/confirm` | 成人向けモデルの年齢確認と注意書きのダイアログ（`#mm-dialog` に差し込む） |
| PUT | `/model-manager/adult` | 設定「成人向けモデルを表示する」（既定オフ）。フォーム `on`（`1` / `0`）。**オンにする最初の 1 回**は、ダイアログの `notice_version`（`Service.AdultContent().NoticeVersion`）と `agree=1`（18 歳以上の確認と注意書きへの同意）が要る。確認の記録（日時・注意書きの版）が `state.json` に残り、以後のオン／オフは `on` だけでよい。確認の記録が無く `notice_version` も無いオンは 409、`agree` が無い・版が違うのは 422（ダイアログに理由を出し、ゲートは開かない）。200 でカード（`#mm-adult`）を返し、モデルの一覧を `hx-swap-oob` で描き直し、ダイアログを閉じる。**確認が無い間は、一覧（`GET /model-manager/models`）に成人向けのモデルが出ず、生成テストのモデルの選択肢にも導入済みの成人向けモデルが出ず、名指しの要求は 404** |
| POST | `/model-manager/models/:id/install` | モデルの導入（ジョブ）。`requires_hf_token` のモデル（LTX-2.5）は、トークン未設定・トークンが無効（401）・アクセスが承認されていない（403）のとき、**ジョブを作らず 422** で案内（model-manager.md §5.3 の文言 A〜C。トークンの区画へのリンクとモデルのページへのリンクつき）を返す。同意が要るモデルは、フォーム `agree=1`・`license_id`・`license_version` で同意を渡す。**同意の記録が無ければ、画面を経ずに叩いても 409 で、ジョブは作らない**（サービスが強制する）。**成人向けのモデルは、年齢確認が無ければ同意を渡しても 409**（下の `adult`）。202 で一覧を返し、ダイアログを閉じる |
| DELETE | `/model-manager/models/:id` | モデルの削除。画像の用途・Quality Policy に使われていれば 422、導入中は 409、未導入は 409。他のモデルと共有するファイルは残る |
| GET | `/model-manager/face-models` | 区画「顔チェック用モデル」（`#mm-face-models`）を読み直す。YuNet・SFace の状態（`data-state`: `installed` / `unverified` / `missing`）・導入先・ファイルの取得元・ライセンスと商用の答え・注意（導入の前に出す）・同意のチェック・導入と削除のボタン。顔の類似度がオフの理由が `runtime_unavailable`（モデルは有るが ONNX Runtime が無い）のときは、その旨と直し方を出す（Issue #1026） |
| POST | `/model-manager/face-models/install` | 2 つのモデルを `--face-model-dir`（既定 `<data-dir>/models`）に取得して検証する。**同期**（終わるまで応答しない。進捗は下の `progress` を 1 秒ごとに取る）。フォーム `accept=on` が無ければ 422（何も取得しない）。固定した SHA-256 と合わなければ 422。区画を返す（導入済みの表示。再起動は要らない） |
| GET | `/model-manager/face-models/progress` | 導入の進み具合の 1 行（`ダウンロード中: 12.3 MiB / 37.1 MiB（33%）`）。導入していなければ空 |
| DELETE | `/model-manager/face-models` | 2 つのファイル（と取りかけのファイル）だけを削除する（フォルダの中のほかのファイルは残す）。進行中の計測を待ってから消す。導入中は 409。導入していなくても成功する |
| GET | `/model-manager/dialog` | 空（ダイアログを閉じる） |
| PUT | `/model-manager/hf-token` | Hugging Face のトークンの保存。フォーム `token`。応答は「設定済み（末尾 4 文字）」の状態だけで、**値は返さない** |
| DELETE | `/model-manager/hf-token` | トークンの削除（未設定でもエラーにならない） |
| GET | `/model-manager/hf` | Hugging Face の検索と、リポジトリのファイルの一覧（`#mm-hfi-result`）。クエリ `q` が URL（`://` を含む）か `owner/repo` ならそのリポジトリをコミットに固定して一覧し（`Service.HFRepo`）、それ以外はキーワード検索（`Service.SearchHF`、`kind` で絞る）。`q` が空・形式が不正（`hf.co`・http・他のホスト・`datasets/`）は 422、NFAA（コミットのタグか、リポジトリの今の先頭のタグ）を年齢確認の前に開くと 409（古いコミットの URL でも同じ）、gated でトークン・アクセス権が無いのは 422、無いリポジトリは 422 |
| POST | `/model-manager/hf/install` | ファイル 1 つの導入（ジョブ）。フォーム `repo`・`commit`・`path`・`folder`・`name`・`agree=1`・`terms_version`。202 で「追加したモデル」を返す（導入中の進捗つき）。拒否は `HX-Retarget: #mm-hfi-msg`・`HX-Reswap: innerHTML` でエラーの行に出す: terms の確認が無ければ 409、版違い・GGUF・pickle・名前の衝突（同じ宛先の別ファイル、別のフォルダ・名前で導入中の同じファイルを含む）・gated・フォルダ不明は 422、NFAA を年齢確認の前に導入しようとすると 409。SHA-256・サイズは送らない（Service が HF から読み直す） |
| GET | `/model-manager/runtimes` | ローカルの実行環境の区画全体。クエリ `purpose` なしは `#mm-runtimes`（用途が `llm` でないもの。GPT-SoVITS・Irodori-TTS・MuseTalk）、`purpose=llm`（#1104）は `#mm-llm-runtimes`（llama.cpp・Ollama）。区画の更新ボタン（Issue #962）が取り直す。各カードは自分のポーリングを持つので、時刻は出さない。`purpose` が `llm` 以外の値は 422 |
| GET | `/model-manager/runtimes/:id` | ローカルの実行環境（GPT-SoVITS・MuseTalk）のカード（`#mm-runtime-<id>`。Issue #920）。導入中・起動中は自分で取り直す。未知の ID は 404 |
| GET | `/model-manager/runtimes/:id/detail` | ランタイムのカードが開く詳細モーダルの本体（Issue #954）。説明・この PC で使えない理由とクラウドの案内・導入先・出典とライセンス・重み・依存ファイルごとの出典・ライセンス・商用利用・ファイルと SHA-256。未知の ID は 404（理由だけのモーダル） |
| POST | `/model-manager/runtimes/:id/install` | 環境の導入・更新（ジョブ。Python 環境・本体・重みまで）。この PC で使えなければ 409（理由とクラウドの案内つき）。202 |
| POST | `/model-manager/runtimes/:id/weights` | **重み・依存ファイルだけ**の確認と取得（ジョブ。足りない・壊れたファイルだけを取り直す）。未導入は 409。202 |
| POST | `/model-manager/runtimes/:id/start` | 起動。必須の重みが足りなければ 409「…の重みが不足しています（<名前>）。「重みを導入」を実行してください」（起動しない） |
| POST | `/model-manager/runtimes/:id/stop` | 停止 |
| PUT | `/model-manager/runtimes/:id/autostart` | 自動起動。フォーム `on`（`1` / `0`） |
| GET | `/model-manager/runtimes/:id/models` | 実行環境のモデルの一覧を、フィルターバーの条件（クエリ。`modelfilter.Condition` のキー。`languages` も）で絞って返す（`#mm-runtime-model-list-<id>` の差し替えと、漏斗の件数の OOB）。押していたラジオ `model` と同意のチェック `accept_<モデル>` は、クエリで受けて描き直した一覧に残す（同意は表示した版と同じときだけ。代わりにチェックはしない）。条件は実行環境ごとに Cookie（`mam_mf_runtime-models-<id>`）へ覚える。条件が不正: 200（理由を出し、始めの状態に戻す）、環境が無い: 404 |
| PUT | `/model-manager/runtimes/:id/model` | 複数のモデルを持つ環境（Irodori-TTS）のモデルの選択。フォーム `model`、同意が要るモデルは `accept_<モデルの id>`（値は表示したテキストの版）。応答はモデルの選択のフォーム（選んだ・拒否の理由つき）。同意が無い・版が違う: 409、知らないモデル: 422、環境が無い: 404 |
| DELETE | `/model-manager/runtimes/:id` | 環境と取得した重みの削除（設定は残る）。導入中は 409。`hx-confirm` つき |
| GET | `/model-manager/local-llm/use` | 「使うサーバー」（`#mm-llm-use`。#1104）。ラジオ（`use`: `llama-cpp`・`ollama`・`custom`）と状態の行 `#mm-llm-use-state`。`use` が管理サーバーで `starting`・導入中の間だけ自分で取り直す（`every 2s`）。常に 200 |
| PUT | `/model-manager/local-llm/use` | 使うサーバーの保存（#1104）。フォーム `use`。`llama-cpp` は `providers.local.use_managed_runtime=true`・`use_managed_ollama=false`、`ollama` は `use_managed_ollama=true`・`use_managed_runtime=false`、`custom` は両方 `false`（`base_url` に呼び出す）。200: 区画（「今は <名前> に呼び出します」）。422: `use` が上の 3 値でない（区画に理由を出し、保存しない）。409: 設定ファイルが `--llm-config` で固定、または管理サーバーを持たない app で `llama-cpp`・`ollama` を選んだ。導入・起動はしない。稼働中のクライアントにすぐ渡す（再起動は要らない） |
| PUT | `/model-manager/local-llm/connection` | 「別のサーバー」の保存（#1104。`PUT /settings/llm/local` の移し替え。意味は [local-llm.md](../architecture/local-llm.md) §5.1）。フォーム `base_url`・`api_key`（空は変更なし）・`clear_key`（`1` でキーを削除）・`unload`＋`unload_present`。200: 区画（「保存しました。次の LLM の呼び出しからすぐ使われます」）、422: 区画（理由・打った内容・`aria-invalid`。キーは返さない）、409: 設定ファイルが固定（キーだけの変更は固定でも保存できる）、503: キーストア。`use` は変えない |
| POST | `/model-manager/local-llm/connection/test` | 接続テスト（`GET /v1/models`。#1104。`POST /settings/llm/local/test` の移し替え）。フォーム `base_url`・`api_key`（空は保存したもの。保存しない）。`use` が `llama-cpp`・`ollama` の間は URL を試さず、そのサーバーの状態を言う（起動しない）。200: 結果、422: 拒否されるアドレス |
| GET | `/model-manager/ollama` | 区画「Ollama のモデル」（`#mm-ollama`。#1105）。状態の行・厳選した 3 つのカード・「そのほかのモデル」。クエリ `jobs`（カンマ区切り）は報告する取得のジョブで、終わったら結果（取得した／失敗の理由）を 1 度出す。動いている取得があるあいだ自分で取り直す（`every 2s`）。常に 200（Ollama が未導入・停止中でもカードと理由を出す） |
| GET | `/model-manager/ollama/models/detail` | カードが開く詳細モーダルの本体（#1105）。クエリ `name`（`gemma4:12b` など。名前にコロンがあるので、パスではなくクエリ）。説明・出典・ライセンス・層ごとのサイズ・model の層の digest・照合の結果・確認日・タグが動く注記。カタログに無い名前は 404（理由だけのモーダル） |
| POST | `/model-manager/ollama/models/pull` | Ollama のモデルの取得（ジョブ。#1105）。フォーム `name`（カタログの名前だけ。`library` 以外の名前空間・`cloud` を含むタグ・形式の不正・カタログに無い名前は 422 でジョブを作らない）。Ollama が未導入・非対応なら 409（理由つき）。空きが `models\` のドライブで合計＋ 1 GiB に足りなければ 409（必要量・空き・場所）。取得中の同じ名前は同じジョブ（`existing`）。止まっている Ollama はジョブが起動する。202 で区画を返す |
| DELETE | `/model-manager/ollama/models` | 取得したモデルの削除（#1105）。クエリ `name`。LLM の割り当てが指していれば 422（使っている Agent の名前）、取得中・未取得は 409、カタログに無い名前は 422、Ollama が未導入は 409。`hx-confirm` つき。止まっている Ollama は削除の間だけ起動する。区画を返す |
| GET | `/model-manager/nodes` | 追加ノードの区画全体（`#mm-nodes`。Issue #976）。区画の更新ボタンが取り直す |
| GET | `/model-manager/nodes/:id` | 追加ノードのカード（`#mm-node-<id>`）。導入中は自分で取り直す（クエリ `job` は報告するジョブ。終わったら結果を 1 度出す）。未知の ID は 404 |
| GET | `/model-manager/nodes/:id/detail` | カードが開く詳細モーダルの本体。固定したコミット・アーカイブの SHA-256・ライセンスと商用利用の答え・必要な Python パッケージ・ソースの確認結果・注意事項。未知の ID は 404（理由だけのモーダル） |
| GET | `/model-manager/nodes/:id/consent` | 導入ボタンが `#mm-nodes-dialog` に開く同意のダイアログ（注意事項・ライセンス・コミットと SHA-256 の予告） |
| POST | `/model-manager/nodes/:id/install` | 導入（ジョブ）。フォーム `license_id`・`license_version`・`notice_version`・`agree=1` が同意。同意が無い・版違いは 422（同意のダイアログを理由つきで返す）、導入できないノード（`install_blocked`）は 422、同梱エンジンが無い・Windows 以外は 409、未知は 404。202 |
| POST | `/model-manager/nodes/:id/cancel` | 導入の中断（フォーム `job`）。カードを返す |
| DELETE | `/model-manager/nodes/:id` | ノードの削除（ComfyUI は再起動）。導入中・未導入は 409。`hx-confirm` つき |
| GET | `/model-manager/hf/models` | 「追加したモデル」（`#mm-hfi-models`）。導入中は自分で取り直す。クエリ `jobs`（カンマ区切り）は報告するジョブで、終わったら結果を 1 度出す |
| GET | `/model-manager/hf/models/detail` | 追加したファイルのカードが開く詳細モーダルの本体（Issue #954）。クエリ `path`。リポジトリとコミット・ライセンス・SHA-256・作ったモデル。記録に無い `path` は 404（理由だけのモーダル） |
| POST | `/model-manager/hf/jobs/:job/cancel` | 動いている導入の中断（「追加したモデル」を返す。ダウンロード済みの分は残り、導入し直すと続きから再開する） |
| DELETE | `/model-manager/hf/models` | 追加したファイルの削除。クエリ `path`（`models/<folder>/<name>`）。作ったモデルも消える。使用中は 422、導入中・記録に無いのは 409。カタログのモデルと共有するファイルは、そのモデルが残る間は残る |
| GET | `/model-manager/external` | 外部の ComfyUI の確認（`#mm-external`）。クエリ `endpoint`（種類 ComfyUI の接続先の名前）を渡すと、その ComfyUI が持つモデルファイルと、カタログのモデルに足りないファイルを出す（読むだけ。導入の操作は無い）。同梱の `bundled-comfyui` は 422、未登録は 404、到達できないときは 502 |
| GET | `/model-manager/external/detail` | 外部の ComfyUI のモデルのカードが開く詳細モーダルの本体（Issue #954）。クエリ `endpoint`・`name`。ComfyUI に聞き直して、あるファイルと足りないファイルを出す。接続できなければ 502、接続先が不正なら 422、`name` が無ければ 404（理由だけのモーダル） |
| GET | `/model-manager/companion/ffmpeg` | ffmpeg の区画全体（`#mm-ffmpeg`。#1106。`GET /settings/ffmpeg` の中身の移し替え）。欄・探した結果・ダウンロードの部品。常に 200（`ffmpegsettings.Service` が無い構成ではルートが無い） |
| PUT | `/model-manager/companion/ffmpeg` | ffmpeg・ffprobe のパスの保存（#1106。`PUT /settings/ffmpeg` の移し替え。意味・状態コード・文言は [ffmpeg-lookup.md](../architecture/ffmpeg-lookup.md)・[endpoints-settings.md](endpoints-settings.md) §5.2 のまま）。フォーム `ffmpeg`・`ffprobe`（空は指定なし）。200: 区画（`role="status"`）、422: 区画（`role="alert"` の理由と入力値。書かない）、500: 書き込みの失敗 |
| GET・POST・DELETE | `/model-manager/companion/ffmpeg/download` | ffmpeg のダウンロードの部品（`#ffmpeg-download`。#1106。`/settings/ffmpeg/download` の移し替え。取得・検証・409 の理由・`Cache-Control: no-store`・進行中の `every 2s` は §5.2.1 のまま）。初回ガイドの同じ部品もこのルートを向く |
| POST | `/model-manager/companion/ffmpeg/download/cancel` | ダウンロードの中断（#1106。`/settings/ffmpeg/download/cancel` の移し替え） |
| GET | `/model-manager/companion/facefusion` | FaceFusion の区画全体（`#mm-facefusion`。#1106）。Python とフォルダの欄・探した結果・版と自己申告のライセンス・入れる手順・モデルの表・標準の注意。クエリなしで**探し直す**（保存しない・FaceFusion を実行しない）。常に 200（`facefusionsettings.Service` が無い構成ではルートが無い） |
| PUT | `/model-manager/companion/facefusion` | Python とフォルダのパスの保存（#1106。`PUT /settings/facefusion` の移し替え。意味・状態コードは [face-swap.md](../architecture/gentest/face-swap.md) §2 のまま）。フォーム `python`・`path`（空は指定なし）。200: 区画（`role="status"`）、422: 区画（`role="alert"` の理由と入力値）、500 |
| GET | `/model-manager/companion/index` | 付随ソフトの一覧（`#mm-companion-index`。#1106）だけ。更新ボタンが取り直す。常に 200 |
| GET | `/settings/ffmpeg`・`/settings/facefusion` | **303 で `/model-manager/companion`**（#1106。古いブックマークとエラーの導線のため。フラグメントは付けない）。`PUT`・`POST`・`DELETE /settings/ffmpeg…`・`PUT /settings/facefusion` は廃止（404） |

## 2. エラーの対応

サービスの拒否は `mmview.Refuse`（`service/modelmanager/mmview`）が種類とメッセージに写す。画面と MCP ツールで同じ文面になる。

| サービスのエラー | HTTP | MCP の code | 画面のメッセージ（要約） |
|-----------------|------|-------------|------------------------|
| `*InvalidError`・`ErrNoGPU`・`engine.ErrVariant` | 422 | `validation` | サービスのメッセージ（GPU が無いときは案内。CPU は明示的に選ぶ） |
| `ErrConsentRequired` | 409 | `conflict` | 「このモデルのライセンスに同意してから導入してください」（同意ダイアログを開いたままにする） |
| `ErrAdultNotConfirmed` | 409 | `conflict` | 「成人向けモデルです。年齢の確認と注意書きへの同意を済ませ、「成人向けモデルを表示する」をオンにしてください」（導入・同意・読み取りのどれも、何も記録せず何もダウンロードしない） |
| `ErrBusy` | 409 | `conflict` | 導入が実行中 |
| `ErrPinned` | 409 | `conflict` | `--model-config` で固定されているため、導入・削除できない |
| `ErrUnsupported` | 409 | `conflict` | 同梱の ComfyUI は Windows 版と Apple Silicon の Mac（macOS 14 以降）版だけ。外部の ComfyUI の URL を使う |
| `ErrEngineNotInstalled`・`ErrAlreadyInstalled`・`ErrNotInstalled`・`ErrEndpointMoved`・`fetch.NoSpaceError`（空き容量不足）・`store.ErrNotWritable`・`engine.ErrBusyRunning` | 409 | `conflict` | それぞれの理由（空き容量は必要量・空き・場所） |
| `localruntime.ErrWeightsMissing`（`*WeightsError`）・`ErrSourceGone`・`ErrNotInstalled`・`ErrUnsupported`（`*SupportError`）・`ErrBusy` | 409 | `conflict` | ローカルの実行環境（`mmview.RefuseRuntime`。`mmview.Refuse` の上に載せた）。重みの不足は不足している名前と「重みを導入」の案内、取得元が消えたときは取得元の名前・HTTP の状態・原本の場所、使えない PC は理由とクラウドの案内 |
| `ErrNotFound` | 404 | `not_found` | 対象が見つかりません |
| それ以外 | 500 | `internal` | 「処理できませんでした」（原因はログだけ。画面に出さない） |

## 3. 画面

分類ページは `mmcat.All()` の順に: 

| `<slug>` | 名前 | 区画（`id`） |
|---|---|---|
| `comfyui` | ComfyUI エンジン | エンジンのカード（`#mm-engine`）・GPU の検出（`#mm-gpu`）・既存の ComfyUI の検出（`#mm-detect`） |
| `image-video` | 画像・動画モデル | 成人向けの設定（`#mm-adult`）・商用OKのモデルだけ・一覧（`#mm-catalog`）・ダイアログ（`#mm-dialog`・`#mm-consent`）。エンジンが無い・止まっているときは一覧の上に案内（`mm-engine-needed`）と `comfyui` へのリンク |
| `custom-nodes` | 追加ノード | `#mm-nodes`・同意のダイアログ（`#mm-nodes-dialog`） |
| `local-runtimes` | ローカル音声・口パク | `#mm-runtimes`（用途が `llm` のものは含まない。#1104） |
| `local-llm`（#1104） | ローカル LLM | `#mm-llm-use`・`#mm-llm-runtimes`（llama.cpp・Ollama のカード）・`#mm-ollama`（#1105）・`#mm-llm-connection` |
| `face-check` | 顔チェック用モデル | `#mm-face-models` |
| `hf-external` | Hugging Face・外部の ComfyUI | Hugging Face から導入（`#mm-hfi`）・トークン（`#mm-hf`）・外部の ComfyUI（`#mm-external`） |
| `companion`（#1106） | 付随ソフト | `#mm-companion-index`・`#mm-ffmpeg`・`#mm-facefusion` |

「試し生成」（`#mm-trial`・`#mm-trials`）は #1100 で廃止。

- **エンジンのカード**: 状態は 未導入 / 導入中（進捗バー・バイト数・「中断する」）/ 導入済み（版・ビルド・CPU 実行か）+ 停止中 / 起動しています / 起動中（`127.0.0.1:<port>`）/ 起動に失敗（理由とログの末尾）。導入済みの版が固定版と違えば「更新する」。ボタン: 起動・停止・自動起動の切り替え・エンジンを削除。**Windows 以外（ブラウザ版・サーバー版）では導入のボタンを出さず**、外部の ComfyUI の URL の案内を出す。`mam.exe` のあるフォルダに書き込めないときも同じ（理由を出す）。導入先 `<exe のディレクトリ>\comfyui` と空き容量を出し、「アンインストールするとここも消える」と書く。ComfyUI は GPL-3.0 の別プログラムで mam に含まれないこと、取得元（ソース）とライセンスの URL を出す。
- **GPU の検出**（未導入のとき、読み込み時に取る）: アダプターの名前と VRAM、推奨するビルド（確度が低いときは「推定です」）。ビルドの選択（サイズと必要な空き容量）。**対応する GPU が無いとき**は推奨が空で、案内（外部の ComfyUI を使う）を出し、CPU 実行はチェックボックスの明示的な選択にだけ表示する（自動では何も入れない）。
- **既存の ComfyUI の検出**: 見つかった URL・版・GPU（ComfyUI Desktop の印）と、その URL を設定の「ローカル・代替モデル」に登録する案内。
- **モデルの一覧**: 種別（画像 / 動画）・ライセンスの同意が要るモデルの印・導入の状態（未導入 / 導入中 / 導入済み / ファイルが不足）・説明・大きさ・VRAM の目安（出典。mam の推定は「GPU では未確認」と書く）・ライセンス名（URL へのリンク）・**商用利用の可否**・導入に必要な空き容量（足りなければ赤字で「導入できません」とし、ボタンを無効にする）・ファイルの一覧（他のモデルと共有するものは「使うモデルが残る間は消えません」）・導入されるモデルの識別子とワークフローの検証の範囲（「構造テストのみ・GPU 実機未検証」）。エンジンが無い・Windows 以外・設定ファイルが固定のときは、理由を出してボタンを無効にする。導入は「導入」・「不足分を取得して復旧」、進行中は進捗バーと「中断」、導入済みは「削除」（確認つき）。
- **ライセンスの同意ダイアログ**: 同意が要るモデルでは「ライセンスを確認して導入」が `#mm-dialog` にダイアログを開く。ライセンス全文へのリンク・要約・商用利用の可否・**地域の制限**（例: EU・英国・韓国では使えない）・注意書き・「モデルの重みは mam が再配布しない」の説明を出し、チェックボックスに同意してから「同意して導入」。同意は ID と版を送り、サービスが記録して強制する。
- **トークン**: 設定済みか（末尾 4 文字）だけを出す。入力欄は `type=password`。
- **Hugging Face から導入**（区画「Hugging Face から導入」、`#mm-hfi`。Issue #842）: 上から、検索フォーム（キーワード・`owner/repo`・URL のどれか 1 つと、キーワード検索の種別）、エラーの行（`#mm-hfi-msg`。導入の拒否はここに出る）、結果（`#mm-hfi-result`）、「追加したモデル」（`#mm-hfi-models`）。結果はヒットしたリポジトリの一覧（gated・NFAA の印・ダウンロード数・「ファイルを見る」）か、1 つのリポジトリ: コミットに固定したことと、ライセンス・警告（記載が無いとき）・gated の案内（トークンの設定とリポジトリのページでのアクセスの申請。mam からは申請できない）・terms と、モデルのファイルをグループ（同じモデルの版）ごとに出す。各ファイルは大きさ・形式・SHA-256 の先頭・Hub のスキャン・VRAM の目安（収まる見込み／ぎりぎり／収まらない見込み／VRAM 不明）・導入済みの印を出し、**導入できないファイル（GGUF・pickle・分割形式・unsafe）は理由を出してフォームを出さない**。導入できるファイルは「導入する…」を開くと、フォルダ（推定の既定値つき・判定できなければ「選んでください」）・ファイル名・（そのリポジトリのコミットで未確認なら）terms のチェックボックスのフォームが出る。SDXL のチェックポイントには「生成テストで選べる画像モデルになる」、それ以外には「置くだけでワークフローは作らない」と書く。導入すると 202 で「追加したモデル」を返し、動いている間は 2 秒ごとに自分を取り直す（進捗バー・「中断」。終わったら「追加したモデル」だけを描き直す）。各モデルは、リポジトリ・コミット・大きさ・ライセンス・共有しているカタログのモデルを出し、「削除」（確認つき）を持つ。**NFAA のモデルは年齢確認が済むまで出ない**（Service が隠す）。
- **外部の ComfyUI**: 接続先の選択と「導入済みのモデルを確認」。結果は、カタログのモデルごとに「そろっています」／「足りないファイルがあります（足りない: …）」、フォルダごとのファイル（`loras` を含む）。成人向けのモデルは、年齢確認が済むまで出ない。「外部の ComfyUI へのモデルの導入は、その ComfyUI の側で」と書く。

- **ローカル音声・口パク・LLM**（区画 `#mm-runtimes`。llama.cpp〔`llm`〕のカードを含む。local-runtime.md §17）（区画 `#mm-runtimes`、Issue #920。ハンドラーは `handler/modelmgr/runtimes`、見た目は `organisms/modelmgr/runtimecard`・`weightlist`。`localruntime.Service` を持つときだけ。意味は [local-runtime.md](../architecture/model-manager/local-runtime.md) §12）: ランタイムごとに 1 枚のカード（`data-testid="mm-runtime"`）。状態（未導入 / 導入中 / 導入済み + 停止中・起動中・失敗）、導入中の進捗バーと「中断する」（`hx-confirm`。`POST /model-manager/jobs/:job/cancel` がジョブの `target=runtime:<id>` を見て振り分ける）、版・導入先・空き容量、起動の失敗はログの末尾つき。**この PC で使えないとき**（macOS ほか）は、導入・起動のボタンの代わりに**理由**（`mm-runtime-reason`）と**クラウドの案内**（`mm-runtime-fallback`。ElevenLabs / Sync Labs を選べる）を枠で出す。**重み**（`<details data-testid="mm-runtime-weights">`）は、asset ごとに名前・大きさ・必須か・状態（あり / なし / 未確認）・出典のリンク・ライセンスのリンク・商用利用のバッジ（「商用利用: 可」/「不可」/「**はっきりしません**」）を出す。`unclear` / `no` の asset は、その行に琥珀色の枠で `notice`、一覧の前に「商用利用がはっきりしない重みを含みます（N 件）」の枠を出し、一覧を最初から開く。導入済みでも必須の重みが足りないときは「導入済み（重みが不足）」を出し、起動の代わりに「重みを導入」を出す。削除は `hx-confirm`、導入・起動・重みの導入は `hx-sync` と `hx-disabled-elt`（二重送信の防止）。
- **ローカル LLM**（区画 `#mm-llm-use`・`#mm-llm-runtimes`・`#mm-llm-connection`。#1104。見た目は [../components/model-manager-screen.md](../components/model-manager-screen.md) の「ローカル LLM の統合（#1104）」）: 上の `#mm-runtimes` の記述のうち llama.cpp のカードに関するものは、`#mm-llm-runtimes` の中で今のまま成り立つ（`data-testid="mm-runtime"`・`mm-runtime-reason`・`mm-runtime-fallback`・重み・使うモデルの選択・導入と起動の `hx-sync`・`hx-confirm`）。ルート `/model-manager/runtimes/:id/…` は用途に関わらず同じ。
- **追加ノード**（区画 `#mm-nodes`、Issue #976。ハンドラーは `handler/modelmgr/nodes`、見た目は `organisms/modelmgr/nodecard`。`modelnodes.Service` を持つときだけ。意味は [custom-nodes.md](../architecture/model-manager/custom-nodes.md)）: ノードごとに 1 枚のカード（`data-testid="mm-node"`、`data-node`・`data-state`・`data-commercial`）。導入ボタンは同意のダイアログを開き、同意の後に導入のジョブを始める（サービスが同意を強制する）。`install_blocked` のノードには導入ボタンが無く、理由を出す。**カードにノードの説明は出さず**（#1103。`mm-node-description` は無い）、固定のコミットとともに詳細モーダル（`GET /model-manager/nodes/:id/detail`。説明は `#mam-modal-desc`）に出す。モデルのカードは、必要なノードが無いとき「追加ノードが必要」のバッジと説明を出し、導入ボタンを無効にする。
- **付随ソフト**（区画 `#mm-companion-index`・`#mm-ffmpeg`・`#mm-facefusion`。#1106。見た目と意味は [../components/model-manager-screen.md](../components/model-manager-screen.md) の「付随ソフトの導入（#1106）」。ハンドラーは `handler/modelmgr/companion`〔新設。`handler/settings/ffmpeg`・`handler/settings/facefusion` から移す〕）: 一覧の行は `data-testid="mm-companion-row"`（`data-tool`・`data-state`）、FaceFusion の手順は `mm-facefusion-steps`・`mm-facefusion-cmd`・`mm-facefusion-refind`・`mm-facefusion-version-note`。**mam は FaceFusion を取得しない**（face-swap.md §1）ので、導入のルート・ジョブ・同意のダイアログは無い。

## 4. テスト

`handler/modelmgr` の画面テスト（`mmtest` の偽の GitHub・Hugging Face・エンジンのプロセス、`comfyuitest` の偽の ComfyUI で、HTMX のリクエストを CSRF のクッキーとヘッダーつきで送る）: エンジンの導入・進捗・完了・起動・停止・自動起動・削除・中断と再導入、GPU が無いとき、Windows 以外、空き容量不足、同意の強制（ダイアログを経ない要求の 409・別のライセンスの 422・同意後の導入）、固定された設定ファイル、モデルの導入・削除・共有ファイル、トークンが画面に出ないこと、外部 ComfyUI の一覧、CSRF のトークンが無い要求の 403。

`handler/modelmgr/hfimport` の画面テスト（`mmtest` の偽の HF `hftest`。`web/screentest` の `Screen` で送る）: キーワード検索・URL（`blob` を含む）と ID・短縮 URL／http／他のホストの 422、GGUF・pickle の理由と導入の 422、terms の確認が無い 409・版違いの 422、導入の進捗→完了→削除、NFAA の検索からの除外と 409、gated のトークンの案内と 422。実物の HF・GPU は使わない。

**試し生成の廃止の受け入れ条件（Issue #1100。実装時にこのテストを足す）**:

- `GET /model-manager` の本文に `#mm-trial`・`#mm-trials`・`#mm-trial-form`・`mm-trial-*` の `data-testid` が無い。区画「モデル」の「?」ヘルプの中に `data-testid="mm-open-gentest"` のリンクが 1 つあり、`href` は `paths.GenerationTest()`（`/generation-test`）。
- `GET /model-manager/trials`・`GET /model-manager/trials/form`・`POST /model-manager/trials`・`DELETE /model-manager/trials/:id`・`GET /model-manager/trials/:id/file` は 404（ルートを登録しない）。`middleware.MaxBody` のアップロード用ルートの一覧から `/model-manager/trials` が消えている（21 MiB への広げが残っていない）。
- モデルの導入の完了（`GET /model-manager/models?jobs=…`）・成人向けの切り替え（`PUT /model-manager/adult`）・追加したファイルの導入の完了と削除（`GET`・`DELETE /model-manager/hf/models`）の応答に、`hx-swap-oob` の試し生成の区画が含まれない（一覧・カードだけ）。
- `/generation-test` と `/generation-test/…`・MCP の生成テストのツール・生成テストの履歴は変わらない。履歴に、試し生成で作った結果が今までどおり出る。`GET /model-picker/model-trial` は 404（`modelpicker.SourceTrial`・`trialModels`・`mmview.TrialModels` とそのテストを消す。ほかに使う所は無い）。
- `go build`・`go vet`・`make test` が通り、`templ generate` の差分が無い。fixture（`scroll-mm-*.html`・`layout-long-names-*.html`）は試し生成の区画を含まない形で作り直し、`scroll.wtest.ts`・`long-names.wtest.ts` が通る。

**分類のサブページの受け入れ条件（Issue #1101。実装時にこのテストを足す）**:

- `mmcat.All()`（`organisms/modelmgr/mmcat`。新設）の全 `<slug>` を実際のルーターで `GET /model-manager/<slug>` すると 200 で、本文に `data-testid="model-manager"` かつ `data-category="<slug>"` の要素が 1 つ。§3 の表のその分類の区画の `id` が**ちょうど 1 つずつ**あり、ほかの分類の区画の `id`（たとえば `image-video` に `#mm-nodes`）は無い。戻るリンク `data-testid="mm-back"` の `href` は `/model-manager`。
- `GET /model-manager`（概要）の `data-testid="mm-category"` の数と順が `mmcat.All()` と同じ。各カードのリンク `mm-category-open` の `href` は `/model-manager/<slug>`。まとめ（`mm-category-summary`）は文字で、`data-state` を持つ。トークンの値・成人向けのゲートが閉じている間の隠しているモデルの数は出ない。概要に区画（`#mm-engine`・`#mm-catalog` ほか）の `id` が無い。
- 動いている導入が無いとき `#mm-overview` に `hx-trigger` が無く、あるとき `every 2s`（`hx-get="/model-manager/overview"`）。`GET /model-manager/overview` は常に 200 で、詳細モーダルを含まない。
- HTMX のリクエスト（`HX-Request: true`・`HX-History-Restore-Request` なし）の `GET /model-manager`・`GET /model-manager/<slug>` は `#model-manager-body` だけを返し、`HX-History-Restore-Request: true` ならページ全体を返す。どちらも `Vary: HX-Request, HX-History-Restore-Request`。
- 実行環境・ノード・顔チェックのサービスを持たない構成では、`local-runtimes`・`custom-nodes`・`face-check` は 404 で、概要にもメニューにも出ない。
- **応答の `hx-swap-oob` の `id` は、その応答を取った分類ページにある**: `POST /model-manager/engine/install`・`DELETE /model-manager/engine`・`GET /model-manager/engine?job=…` の応答に `#mm-catalog` の OOB が無い（`oob.EngineWithCatalog` は無い）。モデルの導入・削除・成人向けの切り替え・商用OKのスイッチ・追加したファイルの導入と削除の応答の OOB は、すべて `image-video`・`hf-external` のそれぞれ同じページの `id` だけを指す。各 POST・PUT・DELETE と、ポーリングの GET で確かめる。
- `image-video` ページは、エンジンが未導入・停止中のとき一覧の上に `data-testid="mm-engine-needed"`（`comfyui` へのリンクつき）を出し、導入済みで起動しているときは出さない。「HF トークンが必要」のカードの案内のリンクの `href` は `/model-manager/hf-external#mm-hf`。「追加ノードが必要」のカードに `mm-node-needed-link`（`/model-manager/custom-nodes#mm-nodes`）がある。
- 生成テストの `gt-face-off-install`・`gt-face-off-status` の `href` は `/model-manager/face-check#mm-face-models`（`paths.ModelManagerFaceModels()`）。ジョブ一覧の導入のジョブの `jobs-open` の `href` は、`target` の種類に応じた分類ページ（`engine`→`comfyui`・`model:` →`image-video`・`runtime:` →`local-runtimes`・`node:` →`custom-nodes`・`hf:` →`hf-external`）。`modelfilter.RouteOf` の行き先は `image-video`（`ReasonAgeGate`・`ReasonNotInstalled`）・`custom-nodes`（`ReasonMissingNode`）。
- 左メニュー: 概要と全分類ページに `ul#sidenav-mm-views`（見出し `#sidenav-mm`「モデルマネージャー」・「概要」＋各分類）があり、現在のページの項目だけ `aria-current="page"`（分類ページでは画面の項目「モデルマネージャー」は `aria-current="true"`）。それ以外の画面にこの一覧は出ない。`web/sidenavtest` が確かめる。
- fixture（`scroll-mm-*.html`・`layout-long-names-*.html`）と `scroll.wtest.ts`・`long-names.wtest.ts` は、分類ページごとの描画（`image-video` の一覧・`comfyui` のエンジンのカード・`hf-external` ほか）に合わせて作り直し、通る。`go build`・`go vet`・`make test`・`templ generate` の差分なし・lint がクリーン。

**カードの列数の受け入れ条件（Issue #1102。実装時にこのテストを足す）**:

- カードを並べる `<ul>`（`mm-models`・`mm-external-models`・`mm-hfi-model-list`・`#mm-runtimes` の中・`#mm-nodes` の中）の class はすべて `modelcard.GridClass`（`grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3`）で、`organisms/modelmgr` の下に `GridClass` 以外の `<ul>` の `grid-cols-` が無い（`<dl>` のメタの表と、エンジンのカードの `mm-engine-list`〔`grid grid-cols-1`〕を除く。`grid_test.go`）。
- 実 Chrome（`model-manager-grid.wtest.ts`）: 639 px で 1 列・640 px と 1279 px で 2 列・1280 px で 3 列。左メニューを開いた 1280 px で 1 枚の幅が 280 px 以上。どの幅でも横にあふれない。カードの一覧を持つ分類ページは `max-w-5xl`。
- ルート・応答の中身・`id`・ポーリング・`data-testid` は変えない（HTML の class だけが変わる）。

`handler/modelmgr/runtimes` の `runtimes_test.go`（`screentest.NewWithRuntimes`。`service/localruntime/lrtest` の偽のパッケージ・偽の Hugging Face・偽のプロセス）: カードの表示（未導入・導入中・導入済み・重みが不足）、出典・ライセンス・`unclear` の注記とバナー、使えない環境の理由とクラウドの案内、導入・重みの導入・起動・停止・自動起動・削除、重みが不足した起動の 409 と文面、取得元が消えたときの失敗の表示、取り消し、未知の ID の 404、サービスが無い画面にはルートが無いこと、スクリーンリーダー用のラベルと鍵・トークンを出さないこと。

**追加ノードの説明の受け入れ条件（Issue #1103。実装時にこのテストを足す）**:

- `nodecat` の全ノード（今は 5 件）で、`GET /model-manager/custom-nodes`（#1101 のあと。それまでは `GET /model-manager`）と `GET /model-manager/nodes`・`GET /model-manager/nodes/:id` の応答に、`data-testid="mm-node-description"` が無く、ノードの `Description` の文字列も含まれない。
- 同じ全ノードで、`GET /model-manager/nodes/:id/detail` の `#mam-modal-desc` の文字列が `Description` と同じ。応答は今までどおり `modal.Frame`（ページに 1 つの `<dialog id="mam-modal">` の本体に入る）で、未知の ID は 404（理由だけのモーダル）。
- `install_blocked` のノードのカードに `mm-node-blocked`（導入できない理由）が残り、導入ボタンが無い。`mm-node-detail-note`・`mm-node-models`・進捗・導入／中断／削除のボタンは変わらない。
- 区画の「?」ヘルプに「ノードの説明」の語があり、ヘルプのほかの文は変わらない。

**ローカル LLM の統合の受け入れ条件（Issue #1104。実装時にこのテストを足す。#1101 のあと）**:

- `GET /model-manager/local-llm` が 200 で、`#mm-llm-use`・`#mm-llm-runtimes`・`#mm-llm-connection` がちょうど 1 つずつ。`GET /model-manager/local-runtimes` に用途 `llm` のカード（`#mm-runtime-llama-cpp`・`#mm-runtime-ollama`）が無い。`GET /model-manager/runtimes`（`purpose` なし）に `#mm-runtime-llama-cpp` が無く、`?purpose=llm` に有る。`?purpose=x` は 422。
- `PUT /model-manager/local-llm/use`: `llama-cpp`・`ollama`・`custom` の 3 値を保存すると、`llm.json` の `providers.local` の `use_managed_runtime`・`use_managed_ollama` が仕様の表のとおりになり、2 つが同時に真にならない。応答の `mm-llm-use-state` に選んだ名前。`x` は 422 で保存しない。`--llm-config` で固定は 409。管理サーバーを持たない app（`Data.Runtimes` が nil）では、ラジオに `llama-cpp`・`ollama` が出ず（理由の 1 行）、直接 PUT しても 409。導入・起動は始まらない（`localruntime.Service` の呼び出しが 0 回）。
- `PUT /model-manager/local-llm/connection` と `POST …/test` が、旧 `PUT /settings/llm/local`・`POST /settings/llm/local/test` と同じ状態コード・文言・キーを応答に出さないこと（`llmsettingstest` の移し替え）。`use` が `llama-cpp` のとき、接続テストはフォームの URL に接続しない（偽のサーバーへの要求が 0 回）。
- `PUT /settings/llm/local`・`POST /settings/llm/local/test` は 404。`GET /settings/llm` の「ローカル LLM サーバー」のカードに `<input>` が無く、`data-testid="llm-local-link"` の `href` が `/model-manager/local-llm`、`llm-local-state` に `use` の今の状態。
- `joblist.Dest`・`modelfilter.RouteOf`（`ReasonEndpoint` ＋ `SourceLocalLLM`）の行き先が `/model-manager/local-llm`。MCP の `set_local_llm_server`（`use_managed_ollama` の入出力）・`test_local_llm_server`・`get_global_settings` の `llm.local_server` が同じ保存を読み書きする（`use` の 3 値と矛盾する組み合わせを MCP から保存しようとすると 422 / `validation`）。
- `go build`・`go vet`・`make test`・`templ generate` の差分なし・lint がクリーン。`web/sidenavtest`・`a11y`（ラジオの `fieldset`・`legend`）。

**Ollama の導入とモデルの取得の受け入れ条件（Issue #1105。実装時にこのテストを足す。#1104 のあと）**:

- カタログ（`ollama` ランタイムの定義。`catalog/ollamatest`）: 取得先は `github.com/ollama/ollama/releases/download/` のアセットだけで、URL・サイズ・SHA-256 が固定されている（v0.40.15 の `ollama-windows-amd64.zip`）。`launch` の `OLLAMA_HOST` が `127.0.0.1` と `{port}` を含み `0.0.0.0`・`::`・`[` を含まない。引数は `serve` だけ。起動前に `server.json` へ `disable_ollama_cloud: true` を書く。ライセンスは MIT・商用 `yes`。
- `GET /model-manager/local-llm` に `#mm-ollama` がちょうど 1 つ、`#mm-runtime-ollama` が `#mm-llm-runtimes` の中に 1 つ。`#mm-ollama` の `mm-ollama-model` が 3 つで、`data-model` が `gemma4:12b`・`qwen3.8:27b`・`deepseek-r1:14b`（カタログと同じ順）。カードの `<ul>` は `modelcard.GridClass`。ライセンスの本文・digest はカードに出ず、詳細モーダルにある。
- Ollama が未導入のとき、取得のボタンが `disabled` で `mm-ollama-state` に「未導入」と `#mm-runtime-ollama` へのリンク。macOS・AMD の GPU の PC は、Ollama のカードに導入のボタンが無く、理由（`mm-runtime-reason`）と案内（`mm-runtime-fallback`）。`POST …/models/pull` は 409。
- `POST /model-manager/ollama/models/pull`（偽の Ollama。NDJSON の `/api/pull`）: 進捗が「確定した層の合計＋今の層の `completed`」をカタログの合計で割った値になり、層が切り替わっても前の値に戻らない。`success` のあと manifest（`OLLAMA_MODELS`）の model の層がカタログと同じなら `data-state="installed"`、違う・読めないなら `data-state="changed"`（バッジ「上流で更新されています」。モデルは消えず、使える）。`error` の行は日本語の失敗の理由。`POST /model-manager/jobs/:job/cancel`（`target=ollama:<名前>`）で `/api/pull` の接続を閉じる。空き不足は 409 でジョブを作らない。名前が `qwen3.8:27b-cloud`・`someone/model:latest`・`../x` は 422（偽の Ollama への要求が 0 回）。
- `DELETE /model-manager/ollama/models?name=…`: 取得済みは `DELETE /api/delete` を呼び、LLM の割り当てが指していれば 422 で何も消えない。取得中・未取得は 409。
- 「そのほかのモデル」: 偽の Ollama の `GET /api/tags` にカタログ外のモデルがあれば `mm-ollama-others` に名前とサイズだけが出て、ボタンは無い。Ollama が止まっている間は出ない。
- 詳細モーダル: `mm-sha256` に model の層の digest（カタログと同じ）、`mm-ollama-tag-note`、Ollama のカードの詳細に `mm-ollama-noauth`。
- ポーリング: 取得中があるとき `#mm-ollama` に `hx-trigger="every 2s"` と `?jobs=`、なくなると属性が消える。取得の完了は `#mm-llm-use-state` だけを `hx-swap-oob` で描き直す（その `id` が同じページにある）。CSRF のトークンが無い POST・DELETE は 403。
- MCP（[mcp-tools-model-manager.md](mcp-tools-model-manager.md)）: `list_ollama_models`・`pull_ollama_model`・`delete_ollama_model` が同じサービスを呼び、同じ拒否（422 / `validation`・409 / `conflict`・404 / `not_found`）を返す。`install_local_runtime`（`runtime_id=ollama`）で本体を導入できる。
- `go build`・`go vet`・`make test`・`templ generate` の差分なし・lint がクリーン。

`handler/modelmgr/nodes` の `nodes_test.go`（`screentest.NewWithNodes`。`service/modelnodes/nodetest` の偽の GitHub のアーカイブ・偽のエンジン）: 区画の一覧（未導入・導入できない理由・このノードを使うモデル）、詳細モーダル（コミット・SHA-256・ライセンス・確認結果）、同意の無い導入と古い版の同意の拒否（何も読み込まれない）、導入→読み込み対象に入る→削除、導入できないノードの拒否、エンジンが無いときの拒否。`organisms/modelmgr/nodecard` の `nodecard_test.go`: ノードの無いモデルの導入ボタンが無効でバッジと理由が出ること、内容が変わったノードが「導入済み」と出ないこと。

**付随ソフトの導入の受け入れ条件（Issue #1106。実装時にこのテストを足す。#1101 のあと）**:

- `GET /model-manager/companion` が 200 で、`data-category="companion"`、`#mm-companion-index`・`#mm-ffmpeg`・`#mm-facefusion` がちょうど 1 つずつ。一覧の `mm-companion-row` が `ffmpeg`・`facefusion`・`ollama`・`face-models`・`comfyui`・`local-runtimes`・`custom-nodes`・`codex` の順（その部品を持たない構成の行は無い）。`ollama`・`face-models` ほかの行の `href` が `/model-manager/local-llm#mm-runtime-ollama`・`/model-manager/face-check#mm-face-models` など各分類を向く。`codex` の行は `/settings/api-keys`。
- ffmpeg: 取得・中断・削除・パスの保存（200・422・500）が、`/model-manager/companion/ffmpeg…` で `/settings/ffmpeg…` と同じ本文・状態コード・`role`・`data-testid` を返す（`ffmpegdl` の偽の配布元。チェックサムの不一致でファイルが消えること・409 の理由も同じ）。初回ガイドの部品の `hx-post` が新しいルート。ffmpeg が無い PC で、取得したあとに探索が見つけ、`Probe`・`Thumbnail` が動く既存のテストは新しいルートで通る。
- FaceFusion: 見つからないとき `mm-facefusion-steps` が `open` で、番号つきの 5 つの手順を含み、`git checkout 3.9.2`（`facefusion.RecommendedTag`）・`conda create --name facefusion python=3.12 pip=25.0`・`python install.py cuda@12` を含む。**取得するボタン・導入のジョブは無い**（`install`・`pull` に当たる要素・ルートが無いことを確かめる）。見つかったとき（偽のフォルダ・`metadata.py`）は閉じていて、版 `3.9.2` では `mm-facefusion-version-note` が無く、`4.0.0` では出る。`mm-facefusion-refind`（`GET …/companion/facefusion`）が探し直して状態を変える（FaceFusion を実行しない）。保存した欄・422 の入力値の保持は今と同じ。
- `GET /settings/ffmpeg`・`GET /settings/facefusion` が 303 で `/model-manager/companion`。`PUT /settings/ffmpeg`・`PUT /settings/facefusion`・`/settings/ffmpeg/download…` は 404。設定の左メニューと概要に「ffmpeg」「FaceFusion」が無く（11 → 10 枚）、概要に「付随ソフト」のカード（`href` が `/model-manager/companion`）が 1 枚ある。
- 導線: `apperr.PlaceFFmpeg`・`PlaceFaceFusion`・`handler/jobs` の ffmpeg のジョブの行・`faceswapscreen` の `SettingsURL` が `/model-manager/companion#mm-ffmpeg`・`#mm-facefusion` を向く（`paths/apperr_test.go` の対応表を新しい URL に更新）。
- 概要（`GET /model-manager`）の `companion` のカードの `data-state` が `full`・`partial`・`attention` を、ffmpeg の有無と FaceFusion の有無の 4 通りで返す。FaceFusion が未設定でも `attention` にならない。
- `web/sidenavtest`・`a11y`（一覧の行の名前・手順の `<ol>`・コマンドのブロックの `aria-label`）・`go build`・`go vet`・`make test`・`templ generate` の差分なし・lint がクリーン。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成（ルート・エラーの対応・画面・テスト） | Issue #833。モデルマネージャーの画面 |
| 1.1 | 2026-10-26 | 試し生成のルートを、生成テスト（[endpoints-gentest.md](endpoints-gentest.md)）の 1 件のバッチを動かすものに改めた（一覧は履歴の結果・`jobs` クエリを廃止・`:id` は結果の ID）。試し生成の区画に生成テスト画面へのリンクを足した | Issue #835 |
| 1.2 | 2026-10-25 | 成人向けモデルの年齢確認のルート（`GET /model-manager/adult/confirm`・`PUT /model-manager/adult`）と、`ErrAdultNotConfirmed`（409）を足した。モデルの一覧・導入・試し生成の選択肢は、確認が無い間は成人向けのモデルを出さず、導入は 409 | Issue #841 |
| 1.3 | 2026-10-25 | Hugging Face からの導入のルート（`/model-manager/hf`・`hf/install`・`hf/models`・`hf/jobs/:job/cancel`）と区画「Hugging Face から導入」（`#mm-hfi`）を足した。`hf-token` のハンドラは新しいパッケージ `hfimport` に移した（ルートは同じ） | Issue #842。意味は [../architecture/model-manager/model-manager-hf.md](../architecture/model-manager/model-manager-hf.md) |
| 1.4 | 2026-10-25 | `/model-manager/external` が成人向けのモデルを年齢確認が済むまで出さず、`loras` フォルダを読むようにした。`/model-manager/hf`・`/model-manager/hf/install` の NFAA 判定を、コミットのタグと今の先頭のタグの OR にした（`tree/<commit>` などで古いコミットを指しても 409）。別のフォルダ・名前で導入中の同じファイルは 422 | PR #849 のレビューの指摘（#841・#842）への修正 |
| 1.5 | 2026-10-29 | `POST /model-manager/models/:id/install` が、`requires_hf_token` のモデルでトークンとアクセスを確かめ、足りなければジョブを作らず 422 で案内することを足した | Issue #877 |
| 1.6 | 2026-11-01 | ローカルの実行環境（GPT-SoVITS・MuseTalk）のルート（`/model-manager/runtimes/:id` と `install`・`weights`・`start`・`stop`・`autostart`）、エラーの対応（`ErrWeightsMissing`・`ErrSourceGone` ほか。`mmview.RefuseRuntime`）、区画「ローカル音声・口パク」（出典・ライセンス・商用利用の表示、使えない環境の理由とクラウドの案内）、テストを足した。`POST /model-manager/jobs/:job/cancel` が `target=runtime:<id>` のジョブも中断する | Issue #920。重み・依存ファイルの出典とライセンスの画面表示、未導入・重み不足の状態 |
| 1.7 | 2026-11-01 | ローカルの実行環境のルートが MuseTalk 1.5（`musetalk`）でも同じ形で使えることを書いた（導入のジョブは部品 84 個の取得・展開と重みの取得。使えない PC の理由・案内は `Support.Code` に `compute_cap` が増えた）。ルート・フィールドは変えていない。意味は [local-runtime.md §13](../architecture/model-manager/local-runtime.md) | Issue #921 |
| 1.8 | 2026-11-02 | カードが開く詳細モーダルのルート（`models/:id/detail`・`runtimes/:id/detail`・`hf/models/detail`・`external/detail`）を足した。一覧・カードの応答にモーダルは含まれない（ページに 1 つ）。見た目は [model-manager-screen.md](../components/model-manager-screen.md) の「カードと詳細モーダル（#954）」 | Issue #954 |
| 1.9 | 2026-11-02 | 更新ボタン（`molecules/refresh.Bar`）を、エンジンのカード（`GET /model-manager/engine`）・モデルの一覧（`GET /model-manager/models`）・実行環境の区画（新しい `GET /model-manager/runtimes`）・試し生成の結果（`GET /model-manager/trials`）に足した。エンジンのカードが開く詳細モーダルのルート `GET /model-manager/engine/detail` を足した。見た目は [model-manager-screen.md](../components/model-manager-screen.md) の「更新ボタンとエンジンのカード（#962・#970）」 | Issue #962・#970 |
| 1.10 | 2026-11-03 | 追加ノードの区画のルート（`/model-manager/nodes`・`:id`・`detail`・`consent`・`install`・`cancel`）と、モデルのカードの「追加ノードが必要」を足した | Issue #976 |
| 1.11 | 2026-11-03 | `PUT /model-manager/runtimes/:id/model`（モデルの選択と Gemma Terms の同意）を足した。ランタイムのカードの詳細モーダルに「使うモデルの選択」を出す | Issue #1015。意味は [../architecture/model-manager/local-runtime.md §14.5・§14.6](../architecture/model-manager/local-runtime.md) |
| 1.12 | 2026-11-04 | 区画「顔チェック用モデル」のルート（`GET /model-manager/face-models`・`POST …/install`・`GET …/progress`・`DELETE /model-manager/face-models`）を足した。意味は [../architecture/model-manager/model-manager.md](../architecture/model-manager/model-manager.md) §5.4 | Issue #1026 |
| 1.13 | 2026-11-06 | 試し生成に英訳のスイッチは付けないと判断し、理由とテストを §3 の「試し生成」に書いた（動作は今のまま。`startTrial` が `TranslateToEnglish: false` を明示する） | Issue #1068 |
| 1.14 | 2026-11-06 | `GET /model-manager/runtimes/:id/models`（実行環境のモデルの一覧のフィルター。Issue #1015・#1011）を足した | Issue #1015 |
| 1.15 | 2026-11-06 | 「試し生成」のルート 5 本（`/model-manager/trials`・`trials/form`・`POST trials`・`DELETE trials/:id`・`trials/:id/file`）・`POST /model-manager/trials` のアップロード上限の広げ・区画「試し生成」（§3）・試し生成の英訳をしない判断（1.13。Issue #1068）・試し生成のテストを廃止し、導入の完了・成人向けの切り替え・追加したファイルの導入と削除の応答から「試し生成の区画の描き直し」を外した。区画の並びを実際の順に直した。受け入れ条件を §4 に足した。状態は仕様のみ（未実装） | Issue #1100 |
| 1.16 | 2026-11-06 | 画面を分類ごとのサブページに分けると決め、概要（`GET /model-manager`・`GET /model-manager/overview`）と分類ページ（`GET /model-manager/<slug>`。6 つ）のルート、§3 の分類の表、§4 の受け入れ条件を足した。`GET /model-manager/engine` の `job` の応答から、モデルの一覧の OOB を外した。区画の部分ルートは変えない。状態は仕様のみ（未実装） | Issue #1101 |
| 1.17 | 2026-11-06 | §4 に、カードの列数の受け入れ条件（`modelcard.GridClass`・1 → 2 → 3 列の境 640 px・1280 px）を足した。ルート・応答は変えない。状態は仕様のみ（未実装） | Issue #1102 |
| 1.18 | 2026-11-06 | §3 の「追加ノード」と §4 に、カードにノードの説明を出さず詳細モーダルだけに出すこと（`mm-node-description` の廃止）と、その受け入れ条件を足した。ルートは変えない。状態は仕様のみ（未実装） | Issue #1103 |
| 1.19 | 2026-11-06 | §1 に `GET`・`PUT /model-manager/local-llm/use`・`PUT /model-manager/local-llm/connection`・`POST …/connection/test` を足し、`GET /model-manager/runtimes` に `purpose=llm` を足した。§3 の分類の表に `local-llm` を足し、`local-runtimes` から llama.cpp を外した。受け入れ条件を §4 に足した。設定の `PUT /settings/llm/local`・`POST …/test` は廃止（[endpoints-settings.md](endpoints-settings.md)）。状態は仕様のみ（未実装） | Issue #1104 |
| 1.20 | 2026-11-06 | §1 に `GET /model-manager/ollama`・`GET …/ollama/models/detail`・`POST …/ollama/models/pull`・`DELETE …/ollama/models` を足し、§4 に Ollama の導入とモデルの取得の受け入れ条件を足した。Ollama 本体は既存の `/model-manager/runtimes/ollama/…` で扱う。状態は仕様のみ（未実装） | Issue #1105 |
| 1.21 | 2026-11-06 | §1 に `/model-manager/companion/ffmpeg`（`GET`・`PUT`・`…/download`・`…/download/cancel`）・`/model-manager/companion/facefusion`（`GET`・`PUT`）・`/model-manager/companion/index` を足し、`/settings/ffmpeg`・`/settings/facefusion` を 303 にして `PUT`・`POST`・`DELETE` を廃止した。§3 に分類 `companion` を足し、§4 に受け入れ条件を足した。FaceFusion は取得しない（導入のルートは無い）。状態は仕様のみ（未実装） | Issue #1106 |
