# 共通のエラー（原因・直し方・詳細）

Issue #1010。mam で出る失敗を、画面でも MCP でも同じ形で「**なぜ失敗したか**（原因）」「**どうすればいいか**（直し方）」「**どの画面を開くか**（設定画面などへの導線）」「**機械が返した詳細**（元のメッセージ・ノード番号・ID）」の順に伝えるための共通の仕組み。後続の作業者（#1009: I2V の `first_frame` 422 と使われなかった入力の表示、#1011: UI）は、この文書の API を使う。

## 1. 方針

- 失敗は**コード（種類）**で分ける。コードごとに、日本語の原因と直し方を 1 か所（カタログ）に持つ。呼び出し側は文を組み立てず、コードと引数（`{param}` など）を渡す。
- 外部（ComfyUI・各 LLM プロバイダー・モデルのベンダー・HTTP・ファイル／ディスク）の失敗は、**変換層 `errmap`** がコードに直す。サービスが自分で言葉にした拒否（ライセンス未同意・状態の衝突など）は、サービスが `*apperr.Error` を作る（変換層はそれをそのまま通す）。
- 元のメッセージは捨てない。**詳細**として残し、画面では「詳細を見る」の中、MCP では `detail` に入れる。秘密（キー・トークン）は `logging.Redact` で伏せてから入れる。
- **種類が分からない失敗は `unknown`**（「原因がわかりませんでした」）にする。相関 ID（`err_<UUID>`）を付け、ログにも同じ ID（属性 `error_id`）で原因を残す。画面と MCP は ID を見せるので、報告とログがつながる。
- 既存の仕組みと二重にしない: `failmsg`（LLM キー未設定の文言）、`failtext`（ローカルのタイムアウト）、`generation.FailureReason`・`orchestrator.failureReason`（チャットの失敗通知）、`mcpkit.MapError`（ツールエラーのコード）は、すべてこの仕組みの上に載せ替えた（§6）。`mcpkit` のコード（`validation`・`upstream` など）は**クラス**として残し、`apperr.Class*` と同じ値にした。

## 2. パッケージと依存の向き

| パッケージ | 責務 | 依存 |
|---|---|---|
| `backend/internal/apperr` | 型（`Code`・`Error`・`View`・`Link`）、カタログ（`messages_ja.go`・`messages_en.go`）、`Text`・`ParseLang`、記録の直列化（`Marshal`・`Unmarshal`） | `logging`（`Redact`）だけ。上の層を知らない |
| `backend/internal/apperr/errmap` | 外部の失敗 → `*apperr.Error`（`Explain`・`Known`・`IsKnown`・`Report`・`LocalTimeout`・`Media`・`SaveFailure`・`UpdateCheck`） | `apperr`、`agent/llm`、`agent/modeladapter`、`agent/audiodirector`、`chatgpt`、`facefusion`、`media`、`modelmgr` |
| `backend/internal/failreport` | ffmpeg の失敗の文（`Media`: 原因・直し方・詳細の先頭・相関 ID）と、ハンドラが言葉にしなかった失敗の 1 行（`Line`: `errmap.Explain` の原因・直し方・分からない失敗の相関 ID。**元のメッセージは画面に出さず、ログにだけ残す。ログには `failreport.Logged` で秘密を伏せてから書く**）とログ（原因が分からない失敗の `error_id`）。`service` が `AIError` に包む。`Line` は `web/handler/gentest/gtshare.Problem` と `mcp/server` が使う | `apperr`、`apperr/errmap` |
| `backend/internal/agent/llm/failmsg` | LLM のキー未設定（`NoKeyError`）と Agent の名前（`Label`）。`NoKeyMessage` は `NoKeyError` の 1 文 | `apperr`、`agent/llm` |
| `backend/internal/web/molecules/errorbox` | 画面の共通エラー表示（templ） | `apperr` だけ（`web/` はサービス層を import しない） |
| `static/src/components/copy` | 「詳細をコピー」（既存のコピーボタンに `data-copy-done` と textarea を足した） | |

`agent/jobqueue`・`agent/generation`・`agent/orchestrator`・`service`・`failreport`・`web/handler`・`mcp/mcpkit` が `apperr` / `errmap` を使う。`errmap` は上の層を import しない（循環しない）。

## 3. 型と API（Go）

```go
// backend/internal/apperr
type Code string                       // "vram_oom" など。§4 の一覧
type Error struct {                    // error。errors.As で取れる
    Code   Code
    Args   map[string]string           // カタログの {name} に入る値
    Cause  string                      // 空でなければ、原因の文をこれにする（サービスが言葉にした拒否を残す）
    Detail string                      // 機械が返した詳細（秘密は伏せ済み）
    ID     string                      // 相関 ID（ログに残した失敗だけ）
    Err    error                       // 元の失敗（errors.Is / As が通る）
}
func New(code Code) *Error             // .With(name, value) .WithDetail(s) .WithCause(s) .Wrap(err)
func (e *Error) View(lang Lang) View   // 原因・直し方・導線を言葉にする
func (e *Error) Status() int           // クラスから HTTP ステータス（validation 422・upstream 502・unavailable 503・internal 500 …）
func (e *Error) Marshal() string       // 履歴の列に残す JSON
func Unmarshal(s string) (*Error, bool)
func As(err error) (*Error, bool)
func NewID() string
func Text(lang Lang, key string, args map[string]string) string  // i18n（§7）
func WithText(text string, e *Error) error                       // 文面つきで *Error を包む（ジョブが返す）

type View struct {                     // 画面と MCP が受け取る形（JSON のキーは下の MCP の節）
    Code, Class, Cause, Remedy string
    Settings []Link                    // {Label, Path}
    Detail, ErrorID string
}
func (v View) Sentence(lang Lang) string   // 原因 + 直し方
func (v View) Line(lang Lang) string       // Sentence + （詳細: 先頭 200 字）。履歴の 1 行の列用
func (v View) Report(lang Lang) string     // コピー用（コード・原因・直し方・エラー ID・詳細）
```

```go
// backend/internal/apperr/errmap
func Explain(err error) *apperr.Error   // 必ず返す（nil は nil）。分からなければ unknown
func Known(err error) (*apperr.Error, bool) // unknown にしない版。呼び出し側が「知っている失敗か」を見分けるのに使う
func Report(logger *slog.Logger, msg string, err error, attrs ...any) *apperr.Error // unknown ならログに error_id つきで残す
func LocalTimeout(err error) *apperr.Error  // ローカルの生成のタイムアウト 3 種（stalled / time_limit / endpoint_timeout）
func Media(what string, err error) *apperr.Error // ffmpeg の失敗。マシンの失敗（ffmpeg 無し・時間切れ・ディスク・権限）はそのコード、それ以外は media_failed（what は「Rough Cut の書き出し」などのカタログのキー）
func SaveFailure(err error) *apperr.Error   // 設定などの書き込みの失敗。save_failed（ディスク不足・権限は原因に足す）
func UpdateCheck(kind string) *apperr.Error // 更新の確認の失敗（updater の ErrorKind の文字列）→ update_*
```

サービス層の `mediaError(logger, what, err)`（`service/errors.go`）は、ffmpeg の失敗を `failreport.Media`（`backend/internal/failreport`。`errmap.Media` の結果に、原因が分からない失敗（`media_failed`）なら相関 ID（`err_…`）を付けて同じ ID でログに残し、画面・ツールエラーの 1 行目の文に ID と詳細の先頭を入れる）で `*apperr.Error` にし、`*AIError`（`AIError.Err` が `*apperr.Error`。`errors.As` で取れる）に包む。画面のハンドラは `web/handler/failure.Save(err)`（設定の保存の失敗の状態と文。MCP の `errmap.SaveFailure` と同じ失敗）を使う。更新の確認の失敗は `errmap.UpdateCheck(kind)`。

画面の部品（templ）:

```go
// backend/internal/web/molecules/errorbox
type Props struct { View apperr.View; Lang apperr.Lang; ID string } // ID は画面内で一意（結果の ID など）
templ Box(p Props)
```

`Box` は `role="alert"`。原因 → 直し方 → 導線リンク → `<details>`「詳細を見る」（読み取り専用の textarea に `Report` の文）→「詳細をコピー」ボタン → エラー ID の順。詳細は既定で閉じる。コピーは `static/src/components/copy` が `data-copy-source`（textarea の id）・`data-copy-status`・`data-copy-done`（言語ごとの「コピーしました」）で行う。クリップボードが使えないときは、textarea を選択してコピーできる。

## 4. コード一覧

クラスは `mcpkit` のエラーコード（`validation`・`not_found`・`conflict`・`locked`・`forbidden`・`budget_exceeded`・`rate_limited`・`unavailable`・`upstream`・`internal`）と同じ値。直し方と英語の文はカタログ（`apperr/messages_ja.go`・`messages_en.go`）にある。

| コード | クラス | 原因（日本語。`{…}` は引数で埋まる） | 導線 |
|---|---|---|---|
| `auth_failed` | `upstream` | 接続先が認証を拒否しました。API キーが違う・期限切れ、またはこの操作の権限がありません。 | /settings/api-keys |
| `budget_exceeded` | `budget_exceeded` | 予算の上限を超えるため、実行できません。 |  |
| `circuit_open` | `upstream` | このモデルのベンダーで失敗が続いているため、呼び出しを止めています（一時的に利用できません）。 |  |
| `conflict` | `conflict` | いまの状態では実行できません。 |  |
| `connection_failed` | `upstream` | 接続先につながりませんでした。 | /model-manager<br>/settings/local-models |
| `credits_insufficient` | `upstream` | 相手のサービスのクレジット（残高）が足りません（HTTP 402）。この処理に必要な分を確保できませんでした。 |  |
| `disk_full` | `unavailable` | ディスクの空き容量が足りません。 | /model-manager |
| `download_corrupt` | `upstream` | ダウンロードしたファイルが、固定した SHA-256 または大きさと一致しませんでした。破棄しました（取得元の改ざんか、転送の破損の可能性があります）。 | /model-manager |
| `endpoint_timeout` | `upstream` | ローカルの生成が接続先の待ち時間（）を超えたため打ち切りました。 | /settings/local-models |
| `engine_not_installed` | `unavailable` | ComfyUI エンジンが導入されていません。 | /model-manager<br>/settings/local-models |
| `facefusion_failed` | `upstream` | FaceFusion が画像を作れませんでした。（時間切れ・異常終了・出力なしのどれかを続けて言う） | `/model-manager/companion#mm-facefusion`（#1106 で、設定の「FaceFusion」から移った） |
| `facefusion_unavailable` | `unavailable` | FaceFusion を使えません。見つからないか、設定したパスが使えません。 | `/model-manager/companion#mm-facefusion`（#1106 で、設定の「FaceFusion」から移った。直し方の文は「モデルマネージャーの「付随ソフト」」で、入れ方の手順がそこにあることを言う） |
| `ffmpeg_missing` | `unavailable` | ffmpeg と ffprobe が見つかりません。 | `/model-manager/companion#mm-ffmpeg`（#1106 で、設定の「ffmpeg」から移った。直し方の文は「モデルマネージャーの「付随ソフト」」） |
| `forbidden` | `forbidden` | Brand / Safety Constraint などの制約に違反しています。 |  |
| `generation_time_limit` | `upstream` | 生成が制限時間（Max generation time）を超えました。 | /settings |
| `image_unavailable` | `unavailable` | ChatGPT サブスクリプションでの画像生成に使える Codex のモデルが見つかりませんでした（OpenAI が ChatGPT ログインで使えるモデルを入れ替えた可能性があります）。 | /settings/chatgpt |
| `image_model_pinned` | `unavailable` | 画像生成のために固定した Codex のモデル（`Options.Driver`）を、ChatGPT のアカウントが受け付けませんでした。固定したモデルだけを使う設定なので、ほかのモデルは試していません。 |  |
| `input_missing` | `validation` | このモデルが必ず使う入力（）が渡されていません。 |  |
| `input_unsupported` | `validation` | このモデルは、渡された入力画像や解像度の組み合わせを受け付けません。 |  |
| `input_unused` | `validation` | 添えた入力画像は、選んだモデルでは使われません。 |  |
| `install_failed` | `upstream` | 導入を完了できませんでした。 | /model-manager |
| `install_cancelled` | `conflict` | 導入を中断しました（利用者が止めた。ダウンロード済みの続きから再開できる）。 | /model-manager |
| `invalid_input` | `validation` | 入力が正しくありません。 |  |
| `key_missing` | `unavailable` | API キーが設定されていません。 | /settings/api-keys |
| `license_not_accepted` | `conflict` | このモデルのライセンスに、まだ同意していません。 | /model-manager |
| `face_models_missing` | `unavailable` | 顔の類似度を測る顔モデルがありません（{files}）。顔の類似度（SFace）はオフです。 | /model-manager |
| `face_runtime_missing` | `unavailable` | 顔の類似度を測る ONNX Runtime のライブラリを読み込めません。顔の類似度（SFace）はオフです。 |  |
| `llm_invalid_output` | `upstream` | LLM の答えが、求めた形式になっていませんでした。 | /settings/llm |
| `local_llm_unreachable` | `upstream` | ローカルサーバーに接続できません。ローカルの LLM サーバー（Ollama・LM Studio・llama.cpp server など）が応答しませんでした。 | /settings/llm |
| `llm_key_missing` | `unavailable` | LLM の API キーが設定されていません。 | /settings/api-keys<br>/settings/llm |
| `locked` | `locked` | LOCK されている項目は変更できません。 |  |
| `media_failed` | `upstream` | （書き出し・Rough Cut・Take の合成・音声のミキシングのどれか）に失敗しました。動画・音声の処理（ffmpeg）がエラーで終わりました。 |  |
| `media_timeout` | `upstream` | 動画・音声の処理（ffmpeg）が制限時間を超えました。 |  |
| `model_not_installed` | `unavailable` | 必要なモデルのファイルが見つかりません。 | /model-manager |
| `no_usable_model` | `unavailable` | 画像・動画の生成に使えるモデルがありません。割り当てのモデルごとの使えない理由と、条件に合って今使えるモデル（あれば）は、`modelroute.CauseOf(err)` が原因の末尾に足す。生成の開始前に断る（[model-adapters.md](model-adapters.md) §3.1）。HTTP は 409（`conflict`）で同じ文を返す | /settings/api-keys<br>/settings/chatgpt<br>/model-manager |
| `no_gpu` | `unavailable` | 対応する GPU が見つかりません。 | /model-manager<br>/settings/local-models |
| `node_missing` | `unavailable` | ワークフローが使うカスタムノードが、ComfyUI にありません。 | /model-manager |
| `not_found` | `not_found` | 対象が見つかりません。 |  |
| `not_writable` | `unavailable` | 保存先のフォルダに書き込めません。 | /model-manager |
| `rate_limited` | `rate_limited` | 相手のサービスの利用回数の上限（レート制限、HTTP 429）に達しました。 |  |
| `remote_not_found` | `upstream` | 接続先に、呼び出した URL かモデルがありません（HTTP 404）。 | /settings/local-models<br>/model-manager |
| `request_rejected` | `upstream` | 相手がリクエストを拒否しました。コンテンツポリシーに当たったか、入力や設定がそのモデルに合っていません。 |  |
| `save_failed` | `internal` | 設定やデータを保存できませんでした（ディスク不足・書き込み不可のときはその理由が続く）。 |  |
| `server_error` | `upstream` | 相手のサービス側で障害が起きています。 |  |
| `stalled` | `upstream` | ローカルの生成がのあいだ進まなかったため打ち切りました。 | /settings/local-models |
| `time_limit` | `upstream` | ローカルの生成が全体の上限（）を超えたため打ち切りました。 |  |
| `timeout` | `upstream` | 相手の応答が時間内に返りませんでした。 |  |
| `topaz_credit_difference` | `upstream` | アップロードした動画を Topaz が調べ直したところ、必要なクレジットが見積りと大きく違っていたため、処理を止めました（全額返金されます）。 |  |
| `topaz_job_failed` | `upstream` | Topaz が処理を完了できませんでした（相手側の失敗か、処理が取り消されました）。クレジットは返金されます。 |  |
| `topaz_job_interrupted` | `upstream` | 処理の途中で mam が止まったか、ジョブが取り消されたため、Topaz の結果を待つのをやめました。Topaz はこの処理をすでに受け付けているため、Topaz 側では続いている可能性があり、クレジットが引かれることがあります（リクエスト ID: ）。 |  |
| `topaz_job_timeout` | `upstream` | Topaz の処理が時間切れになりました（アップロード待ち・順番待ち・処理時間のどれか）。クレジットは返金されます。 |  |
| `topaz_maintenance` | `upstream` | Topaz のクラウド動画処理は、いまメンテナンス中で止まっています（HTTP 503）。 |  |
| `topaz_source_invalid` | `upstream` | Topaz が元の動画を処理できませんでした。課金はされません。 |  |
| `topaz_transfer_failed` | `upstream` | Topaz との動画のやり取り（元の動画のアップロード、または結果のダウンロード）に失敗しました。 |  |
| `training_failed` | `upstream` | 声の学習を完了できませんでした。 | /model-manager |
| `unavailable` | `unavailable` | この機能はいま使えません（API キーや設定が足りません）。 | /settings |
| `unknown` | `internal` | 原因がわかりませんでした。想定していない失敗です。 |  |
| `update_access` | `upstream` | 更新のリリースを GitHub から取得できませんでした（アクセスが拒否されたか、ファイルがありません）。 | /settings/update |
| `update_failed` | `internal` | 更新の確認に失敗しました。原因はわかりませんでした。 | /settings/update |
| `update_network` | `upstream` | 更新の確認で、ネットワークに接続できませんでした。 | /settings/update |
| `update_rate_limit` | `upstream` | GitHub の取得回数の上限に達したため、更新を確認できませんでした。 | /settings/update |
| `update_release` | `upstream` | 公開されているリリースの内容が正しくありません。 | /settings/update |
| `update_verification` | `upstream` | ダウンロードした更新のファイルを検証できませんでした（破損または改ざんの可能性があるため、更新しません）。 | /settings/update |
| `upstream_failed` | `upstream` | 外部のモデルやサービスが、使える答えを返しませんでした。 |  |
| `vram_oom` | `upstream` | GPU のメモリ（VRAM）が足りませんでした。 |  |

## 5. 外部エラーの変換表（`errmap`）

上から順に当てる。先に当たったものが勝つ。元のメッセージは必ず `detail` に残る。

| 条件 | コード |
|---|---|
| `llm.ErrNoAPIKey` | `llm_key_missing`（Agent・試したプロバイダーとモデル・足りないキー・再起動待ちの設定を引数にする） |
| `modeladapter.ErrNoAPIKey` / `audiodirector.ErrNoAPIKey`（ElevenLabs） | `key_missing` |
| `*localcfg.TimeoutError`（進みが止まった／全体の上限／接続先の待ち時間） | `stalled` / `time_limit` / `endpoint_timeout` |
| `modeladapter.ErrTimeLimit`、`media.ErrTimeout` | `timeout`、`media_timeout` |
| `topaz.ErrNoAPIKey` | `key_missing`（「Topaz Labs の」API キー） |
| `*topaz.Error`（Topaz の `errorCode`・転送の失敗。[model-adapters.md](model-adapters.md) §15.6） | `topaz_*`・`credits_insufficient`・`auth_failed`（コードで決める。HTTP の応答の `SERVER_MAINTENANCE` だけ `topaz_maintenance`、ほかは HTTP の状態で決める） |
| HTTP 402 | `credits_insufficient` |
| `media.ErrUnavailable`（ffmpeg・ffprobe が無い） | `ffmpeg_missing` |
| 429（`modeladapter.RateLimited`） | `rate_limited`（サーキットブレーカーより先。429 が開けた場合も 429 を原因にする） |
| `ErrCircuitOpen`（モデル・LLM） | `circuit_open` |
| `*modelroute.NoUsableError`（`modelroute.ErrNoUsableModel`） | `no_usable_model`（`generation.FailureReason` が原因に `Cause()` を使う） |
| `chatgpt.ErrNoImageModel` | `image_unavailable`（ベンダーの 400 は症状なので、詳細にだけ残す） |
| `chatgpt.ErrImageModelPinned` | `image_model_pinned`（固定したモデルの拒否。直し方は更新ではなく固定を直すこと） |
| `direct.ErrInputsUnsupported` / `ErrInputsConflict` / `ErrResolution` | `input_unsupported` |
| `llm.ErrInvalidOutput` | `llm_invalid_output` |
| `localllm.IsUnreachable`（プロバイダー `local` の `APIError`: status 0・原因あり・タイムアウトでもキャンセルでもない） | `local_llm_unreachable`（`net.Error` より先） |
| `fetch.ErrChecksum` / `ErrSize` | `download_corrupt` |
| `store.ErrNotWritable`、`fs.ErrPermission` | `not_writable` |
| `*fetch.NoSpaceError`、`syscall.ENOSPC` | `disk_full`（場所を原因に入れる） |
| `*fetch.ConnectionLostError` | `connection_failed` |
| `*facefusion.ConfigError`・`facefusion.ErrNotFound`（メッセージの判定の後。FaceFusion の出力に `out of memory` があれば `vram_oom` が先） | `facefusion_unavailable` |
| `*facefusion.RunError` | `facefusion_failed`（`Err` が時間切れ・異常終了・`nil`（出力なし）で `why` が変わる。出力の末尾は詳細） |
| メッセージが「the workflow needs <param> (<node>)」（ComfyUI の入力不足。`<param>` は `first_frame` など） | `input_missing`（`param`・`node` を引数にする） |
| メッセージに `out of memory`・`OutOfMemoryError`・`CUBLAS_STATUS_ALLOC_FAILED` など | `vram_oom` |
| メッセージに `No space left on device` など | `disk_full` |
| メッセージにライセンス未同意（`accept the license` など） | `license_not_accepted` |
| メッセージに `Value not in list` と `ckpt_name`・`unet_name` など（ComfyUI が持たないファイル名） | `model_not_installed` |
| メッセージに `missing_node_type` など | `node_missing` |
| HTTP 401 / 403 | `auth_failed` |
| 404 / 410 | `remote_not_found` |
| 408 / 504、`modeladapter.ErrTimeout`、`llm.ErrTimeout`、`context.DeadlineExceeded` | `timeout` |
| 5xx | `server_error` |
| その他の 4xx（422 を含む） | `request_rejected`（ベンダーの理由は `detail`） |
| `net.Error`・`ECONNREFUSED`・応答なし（status 0） | `connection_failed` |
| `llm.APIError` の `Notice`（プロバイダー自身の言葉。ChatGPT の利用上限など） | `upstream_failed`（`Cause` にプロバイダーの文） |
| どれでもない | `unknown`（`Report` はログに `error_id` で残す） |

`errmap.Media(what, err)`（ffmpeg の失敗。書き出し・Rough Cut・Take の合成・音声のミキシング）は、マシンの失敗（`ffmpeg_missing`・`media_timeout`・`disk_full`・`not_writable`）だけを上の表のコードにし、それ以外は `media_failed`（`what` に作業の名前。詳細は ffmpeg の出力）にする。ffmpeg の出力に `out of memory` があっても `vram_oom` にはしない（GPU の話ではないため）。`errmap.SaveFailure(err)` は、設定の書き込みの失敗を `save_failed` にし、ディスク不足・権限なら原因に「（ディスクの空き容量が足りません）」「（保存先のフォルダに書き込めません）」を足す（`disk_full`・`not_writable` はモデル管理向けの直し方・導線なので使わない）。

サービスの拒否は `mmview.Failure`（`ErrConsentRequired` → `license_not_accepted`、`ErrEngineNotInstalled` → `engine_not_installed`、`ErrNotInstalled` → `model_not_installed`、`ErrNoGPU` → `no_gpu`、導入の失敗でインストーラーが言葉にしたもの → `install_failed`、その他の拒否は文を残して `invalid_input` / `not_found` / `conflict`）が `*apperr.Error` にする。単発生成の `Deps.Explain` とジョブキューの `Options.Explain` に渡してある。

## 6. 失敗の経路と載せ替え状況

| 経路 | 状況 | 内容・理由 |
|---|---|---|
| 単発生成（`runner/execute.go`） | **載せ替えた** | 失敗を `*apperr.Error` にして、結果の `error`（1 行の日本語）と `error_info`（JSON。`gentest_result.error_info`）に残す。結果の詳細の画面は `errorbox.Box`。`record.ResultView.Failure`（`get_generation_result`・`get_generation_test`・`list_generation_results`）。ベンダーが 422 などで返した英語は `detail` にだけ出る。`gentest/failtext` の `LocalTimeout` は `errmap.LocalTimeout` に移した。テスト: `service/gentest/errtest`、`web/handler/gentest/screentest`（モーダルのエラーボックス）、`mcp/tools/gentest/errtest` |
| Shot の生成・Reference Pack（`agent/generation/failure.go`） | **載せ替えた** | `generation.Failure(err)` が `*apperr.Error`。チャットの失敗通知は原因 + 直し方（unknown は相関 ID つき）。ジョブの `error_info` に残る。`FailureReason` は `Failure(...).View().Sentence()`。テスト: `agent/generation/failuretest` |
| 企画・脚本・Shot 計画（`agent/orchestrator/failure.go`） | **載せ替えた** | 同上（`planning.ErrInvalidPlan` は `llm_invalid_output`）。unknown は ID を通知に入れ、ログにも残す。テスト: `agent/orchestrator`（失敗の通知） |
| ジョブキュー（`agent/jobqueue`） | **載せ替えた** | 失敗したジョブの `job.error_info` に `*apperr.Error` を残す（unknown は ID をログと一緒に）。`Options.Explain` でサービスの拒否を先に聞く。`get_job`・`list_jobs` の `failure`。テスト: `agent/jobqueue/failure_test.go`、`mcp/tools/jobs`。**ジョブ一覧の画面**（`/jobs`）も `error_info`（`jobrecent` が読む）と、メモリ上の作業の `worktrack.Record.ErrorInfo` から、失敗した行を `errorbox.Box` で出す（[../components/jobs-screen.md](../components/jobs-screen.md)） |
| モデルの導入・ダウンロード（モデル・エンジン・カスタムノード・ローカルランタイム） | **載せ替えた**（ジョブ経由。画面も #1010 で載せ替えた） | `mmview.Failure` → ジョブの `error_info`（空き容量不足は `disk_full`、取得元の認証は `auth_failed`、SHA-256 不一致は `download_corrupt`、ライセンス未同意・エンジン未導入・GPU なしはそれぞれのコード、利用者が止めたのは `install_cancelled`、取得元が消えた・応答しないなど導入の側が言葉にしたものは `install_failed` でその文が原因）。**取得元が消えた**（404）のような導入の側が言葉にした失敗は、ベンダー向けの `remote_not_found`（「接続先の URL とモデル名を確かめてください」）にせず、導入の文を原因に残す（`mmview.Failure`）。**画面**: モデル管理のエンジン・カタログ・Hugging Face の導入・カスタムノード・ローカルランタイムの導入の終わり（`jobview.Ended` / `Ending`）は、`JobStatus.Failure`（`job.error_info` を `apperr.ViewOf` で読んだもの）があれば `modelmgr.Message.Failures` として**エラーボックス**（`organisms/modelmgr/failbox`。タイトル「<名前>の導入に失敗しました」の下。`jobview.Settle` が節のメッセージに足す）で出し、無い古いジョブは従来の 1 行（`Message.Error`）。テスト: `web/handler/modelmgr/jobview`・`web/handler/modelmgr/failuretest`（エンジン・カタログの中断）・`web/handler/modelmgr/runtimes`・`service/modelmanager/mmview/failure_test.go` |
| 音声（`service/audio.go` の `audioError`） | **載せ替えた** | ElevenLabs のキー未設定・429・5xx・ディスク不足などは errmap の原因 + 直し方。ベンダーの応答は出さない。音声のミキシング（ffmpeg）は `mediaError`（下の行）。テスト: `service/audio_voice_test.go` |
| 画面のトースト（`web/handler/errors.go` の `statusOf`） | **載せ替えた**（フォールバック） | 既存の業務エラーの後で、`errmap` が知っている失敗（外部サービス・ディスク・ネットワーク）は原因 + 直し方とクラスのステータス、知らない失敗は従来どおり 500「内部エラーが発生しました」（原因はログ）。ハンドラが言葉にしていない外部サービス・ディスク（モデルの導入向けの文）・ネットワークの失敗はここで日本語になる。テスト: `web/handler/failure` |
| MCP のツールエラー（`mcpkit.MapError`） | **載せ替えた** | `ToolError.Failure`（`apperr.View`）。コードは従来のクラスのまま、メッセージは原因 + 直し方。テキストの 2 行目に View の JSON。`AIError` に包まれた失敗にも付く。テスト: `mcpkit/errors_test.go` |
| 単発生成のプロンプトの英訳の失敗（`failtext.Translate`） | 一部 | キー未設定の文は `failmsg.NoKeyError` の文。それ以外は従来の日本語の文（原因は書いてあり、直し方は「英訳を OFF」）。構造化（`failure`）はまだ付けない: 失敗が結果に残らず、リクエストの応答（`upstream`）で返るため |
| プロンプトの似せ合わせ（`gentest/match`） | 一部 | キー未設定は `failmsg.NoKeyMessage`。それ以外の失敗は 1 文のまま |
| 書き出し（`editor_export.go` の `renderExport`）・Rough Cut のプレビュー（`rough`）・Take の合成（`take.go` の `composeTake`）・音声のミキシング（`audio.go` の `audioError`） | **載せ替えた**（#1010 の残り） | ffmpeg の失敗を `mediaError`（`failreport.Media` → `errmap.Media`）で `*apperr.Error` にして `*AIError` に包む。画面（トースト）は `statusOf` が `AIError.Reason`（原因 + 直し方 + 詳細の先頭 + 相関 ID）、MCP は `MapError` が `failure`（コード・原因・直し方・詳細・`error_id`）にする。ffmpeg 無し・時間切れ・ディスク不足・権限は専用のコード、それ以外は `media_failed`（ID をログに `error_id` で残す）。ffmpeg の英語のメッセージは詳細にだけ出る。テスト: `failreport/failreport_test.go`（表駆動・ログの突き合わせ。未知の失敗は相関 ID がログと文で一致）、`mcp/tools/editorerrtest`（実際の ffmpeg が書き込めずに失敗する書き出し・Rough Cut が、画面と MCP の 1 行目 + 2 行目の JSON で同じコードになる） |
| Rough Cut の組み立て（`editor_timeline.go`）・Final QC（`editor_final.go`）・Editor Judge / Continue / Command Interpreter の LLM 呼び出し（`aiError`）・Take の Judge / Repair の起動 | 載せ替えない | 外部のツール・サービスを呼ばない（Rough Cut の編集と Final QC は決まった規則の判定）。失敗の文は、Shot が無い・Take のファイルが無い・状態の衝突など、**サービス自身の拒否で、日本語の原因と直し方が書いてある**（`service.ValidationError` / `ConflictError`）。残りはデータベースの失敗で、トースト（上の `statusOf`）では「内部エラーが発生しました」＋ログ（[../api/errors.md](../api/errors.md)。想定外の失敗の内容は画面に出さない方針）。Judge / Repair の実行中の失敗はジョブ（上のジョブキューの行）で `error_info` になる。LLM の呼び出し（`aiError`）は #1024 のまま `generation.FailureReason`（原因 + 直し方。分からない失敗は「原因がわかりませんでした」だが、**相関 ID とログは付かない**。`web/handler` と `service` のディレクトリが行数の上限（`.linterly-baseline`）に張り付いており、載せるには先に分割が要る。未対応）。`*apperr.Error` に書き換える利点が無い |
| アップデート（`appupdate`） | **載せ替えた**（#1010 の残り） | `errmap.UpdateCheck(kind)` が、確認の失敗の種類（`ErrorKind`）をコード（`update_network`・`update_rate_limit`・`update_access`・`update_verification`・`update_release`・`update_failed`）にする。設定画面のパネルは従来の状態の行（「更新の確認に失敗しました。」）の下に `errorbox.Box` を出し、MCP の `get_update_status` / `check_for_update` / `get_global_settings` の `update.failure` に同じ View を返す（`error_kind` はそのまま）。元のエラーの文は画面にも MCP にも出さない（ログに 1 回記録される。既存の方針のまま）。テスト: `apperr/errmap/tools_test.go`、`web/update_settings_test.go`、`mcp/tools/settings/settings_test.go` |
| 設定の保存（各 `settings` のハンドラと MCP の設定のツール） | **載せ替えた**（書き込みの失敗）／載せ替えない（入力検証） | 書き込めなかった失敗（LLM・ローカル LLM・画像／動画モデル・ローカル／代替モデル・音声・ffmpeg・FaceFusion・ChatGPT 画像のオン／オフ）は、`web/handler/failure.Save` ＝ `errmap.SaveFailure` で、コード `save_failed`（500）の原因と直し方（ディスク不足・権限なら原因にそれを足す。元のエラーは詳細）に統一した（従来は、画面ごとに別の文を持っていた。ChatGPT のものは「ログを確認してください」だけで直し方が無かった）。MCP も `mapSaveError`・音声・ローカル LLM のツールで同じ（ツールエラーの 1 行目は `internal: …`、2 行目に `failure`）。入力の誤りは、フィールドごとの検証文（`service.ValidationError`）のまま。テスト: `apperr/errmap/tools_test.go`（表駆動）、`web/ffmpegscreen`・`mcp/tools/settings`（読み取り専用のフォルダへの保存。画面と MCP が同じ文） |
| FaceFusion（顔の入れ替え。`facefusion`・`service/gentest/face`） | **載せ替えた**（#1010 の残り） | 見つからない・設定したパスが使えない → `facefusion_unavailable`（導線 `/settings/facefusion`。探した場所・見つからなかった理由は詳細。`face.Available` は 422 の `gterr.Invalid` から `*apperr.Error`（503）に変わった）、実行が失敗（時間切れ・異常終了・出力なし）→ `facefusion_failed`（FaceFusion の出力の末尾は詳細）。結果の `failure`・`errorbox`・MCP の `swap_generation_face`（`failure`）で同じコード。GPU のメモリ不足は `vram_oom`。テスト: `apperr/errmap/tools_test.go`、`service/gentest/face/swap_test.go`、`mcp/tools/gentestface/face_test.go` |
| 単発生成の画面の 1 行（ComfyUI の「画面に出す値」の選び口 `comfyscreen`・かんたんの層の `tierform` のおすすめ・顔の入れ替えの箱 `faceswapscreen`）・MCP サーバーの待ち受けの理由（`mcp/server` の `Manager`）・`list_local_runtime_models` の絞り込みの誤り | **載せ替えた**（#1010 の残り） | これらの最後の落ち方が、`err.Error()` をそのまま画面・`Status.Problem`・ツールエラーの文にしていた。いまは、画面の 3 か所は `gtshare.Problem`（`*apperr.Error` ならその原因 + 直し方、`*gterr.InvalidError` ならその文、**それ以外は `failreport.Line`**）、MCP サーバーの理由（読み込み・保存済みの待ち受け先の検査・待ち受けの失敗の 4 か所）は、マシンの失敗なら `mcp/server` 自身の文（`mcp.json` が JSON として読めない・`bind_address` が文字列でない〔`bind.ErrMalformed`。直し方は書き直すか削除〕、ネットワークインターフェースの一覧を調べられない〔`LocalAddrs`。`*net.OpError` で返るが開けない失敗ではない〕、`*net.OpError`＝アドレス・ポートを開けない、`*fs.PathError`＝`mcp.json` を開けない）（`errmap` の「接続先につながらない」「書き込めない」の直し方は ComfyUI・ベンダー向けで、この待ち受けには合わない。元のメッセージはログだけ）、それ以外は `failreport.Line` で、`errmap.Explain` の原因 + 直し方 + 分からない失敗（`unknown`、「原因がわかりませんでした」）の相関 ID（ログに `error_id` で同じ ID）にする。**元のメッセージは 1 行の画面に出さず（英語で、「詳細を見る」も無い）、ログに残す**（知っている失敗は WARN、分からない失敗は ERROR で `error_id` つき）。`list_local_runtime_models` の `kinds` / `languages` などの誤りは `invalid_input`（原因は日本語の 1 文を `WithCause`、フィルターの英語のメッセージは `detail`）。すでに日本語の文になる型（`*apperr.Error`・`*gterr.InvalidError`・`bind` の拒否）の枝は、そのまま残す。テスト: `web/handler/gentest/gtshare`（`TestProblem`）、`failreport`（`TestLine_…`）、`mcp/server`（`TestManager_StartTellsAnUnknownFailureWithoutItsRawMessage`）、`mcp/tools/runtimemodels` |
| ChatGPT ログイン・Codex・ローカル音声の導入・ffmpeg のダウンロードなど、各機能の画面に出す固有の失敗 | 載せ替えない（理由を更新） | **ChatGPT / Codex**: 失敗の出どころが ChatGPT のバックエンドや Codex の app-server（外部のプロトコル）で、`errmap` の直し方は「モデルのベンダー・ComfyUI」を前提に書いてある（接続できないときの直し方が ComfyUI のエンジンの話になる、など）。そのまま載せると、ログインの状態の画面に**違う直し方**が出るので、画面ごとの専用の文（ログインの状態・ポート 1455 / 1457・OS のキーストア）を残す。ChatGPT の画像生成そのものの失敗（`chatgpt.ErrNoImageModel`）は載せ替え済み。**ffmpeg のダウンロード・ローカル音声の導入**: すでに原因と直し方のある文（ディスク不足・書き込み不可・非対応の環境）を画面の状態として出している。ChatGPT / Codex を載せるには、`errmap` の直し方をプロバイダー別にする（`Error.Args` の切り替え）設計が先に要る。画面ごとに `errorbox` へ寄せるかは #1011 で決める |

**トーストの未知の失敗**: `statusOf` の最後（どの業務エラーでも `errmap` でも分からない失敗）は、[../api/errors.md](../api/errors.md) の方針（想定外の失敗の内容は画面に出さない）のまま「内部エラーが発生しました」＋ログ（`c.Error`）。`原因がわかりませんでした` と相関 ID を出し、同じ ID でログに残す経路は、失敗を `*apperr.Error` に載せた経路（単発生成・ジョブ・Shot の生成・企画／脚本／Shot 計画・書き出し・Rough Cut・Take の合成・音声のミキシング・MCP のツールエラー）。MCP のツールエラーは、分からない失敗にも `failure`（`unknown`）を付け、`ReportError` が `error_id` を付けて原因をログに残す。

## 7. i18n

repo に既存の i18n の仕組みは無かった（`grep -ri i18n backend static` は、このカタログだけを指す）。エラーの文言はこの `apperr` のカタログに載せてあり、画面全体の i18n（英語表示）は **この Issue の範囲外**。範囲は、エラー文言のカタログ化と、エラー表示コンポーネントが言語を受け取れるところまで。

- カタログ `apperr.Text(lang, key, args)`。`lang` は `apperr.LangJa`（**基準・正本**）か `apperr.LangEn`。辞書は `messages_ja.go`・`messages_en.go`（キー → 文）。ロケールの選択は `apperr.ParseLang("en-US,en;q=0.9")`（`Accept-Language` や言語タグ。英語のタグが先頭なら英語、それ以外・空は日本語）。キーは `<code>.cause`・`<code>.remedy`・`place.<place>`・`ui.<name>`・補助の文（`advice.chatgpt` など）。
- **未訳は日本語にフォールバック**する（英語に無いキーは日本語で読む。知らない言語も日本語）。どちらにも無いキーはキーそのものが出る（テストで検出する）。新しい文は、日本語で先に書けば英語が無くても画面は壊れない。
- 文の中の `{name}` は `Error.Args` の値。無い引数は空になる。`{{key}}` は別のキーの文を入れる（引数も引き継ぐ）。条件で文が入る・入らない部分（キーの無い LLM、再起動待ちなど）と、**言語によって変わる語**は、引数に `{{key}}` を入れて切り替える: 時間（`duration.hours` / `.minutes` / `.seconds`。`{n}` に数）、Agent の名前（`agent.<id>`）、列挙の区切り（`list.separator`・`list.join`）、サービス名（`key_missing.elevenlabs`）、作業の名前（`what.export` など）、FaceFusion の失敗の言い方（`facefusion.*`）。`errmap`・`failmsg` が引数に入れるのは日本語の語ではなく、このキー（`{{…}}`）である。
- **英語の辞書は、`errmap` が作る全コード**（§4 の全コード）の原因と直し方、導線の名前、箱の文言（`ui.*`）、補助の文を持つ。サービスが自分で言葉にした原因（`Error.Cause`。`WithCause` で渡す日本語の 1 文。生成の予算超過・Brand 違反・年齢確認など）は、その言語の文のまま通り、直し方だけがカタログから出る（それらを英語にするには、サービス側の文をキー化する必要があり、画面全体の i18n と一緒に行う）。
- 画面は現状どこも `LangJa` を渡している。`errorbox` の `Props.Lang` に `LangEn` を渡すと、箱の文言と原因・直し方が英語になる（`errorbox_test.go`）。MCP は日本語（相手はエージェントと日本語の利用者）。
- テスト: `apperr_test.go` が、全コードに日本語と英語の原因と直し方があること、日英のキーが一致すること、英語の文に日本語の文字が混じらないこと、`i18n_internal_test.go` が、未訳キーの日本語へのフォールバック・英語だけのキーが無いこと・`{{key}}` の参照先が基準にあることを確かめる。`errmap/tools_test.go` が、`errmap` の作る文の英語（時間の単位・サービス名・FaceFusion の失敗）を確かめる。

## 8. MCP のフィールド

- `get_generation_result`・`get_generation_test` の `results[]`・`list_generation_results`: 失敗した結果の **`failure`**（従来の `error`（1 行）はそのまま）。
- `get_job`・`list_jobs`: 失敗したジョブの **`failure`**（従来の `error`・`error_code`・`error_id`・`next_steps` はそのまま）。履歴（`mcp_job_history.error_info`）に残るので、ジョブの行が消えたあとも読める。
- `get_update_status`・`check_for_update`・`get_global_settings`（`update`）: 確認が失敗したとき（`state: "error"`）の **`failure`**（コードは `update_*`。`error_kind` はそのまま。元のエラーの文は返さない）。
- `swap_generation_face` などの顔の入れ替え: FaceFusion を使えないときは `unavailable` のツールエラー（`failure.code` が `facefusion_unavailable`）。
- ツールエラー: `ToolError.Failure`。エラーの文は `<クラス>: <原因><直し方>`（従来どおり 1 行目）、**失敗には 2 行目に `failure` の JSON**（分からない失敗も `code: unknown`・`class: internal`。元のエラーの文は、パスやベンダーの応答が入りうるので、`detail` にも入れない。`ReportError` が `error_id` を付けて、原因をログに残す）。

`failure` のキー: `code`（§4。エージェントはこれで分岐する）・`class`・`cause`（日本語の原因）・`remedy`（日本語の直し方）・`settings[]`（`label`・`path`。mam の画面のパス）・`detail`（元のメッセージ。英語のことがある）・`error_id`（unknown のとき。`get_app_logs` の `attrs` に `{"error_id": …}` を渡すと原因のログが見つかる）。

## 9. 後続の作業者へ

- 新しい失敗の種類を足すとき: `apperr/code.go` にコードとクラス・導線を足し、`messages_ja.go`・`messages_en.go` に `<code>.cause`・`<code>.remedy` を足す（`TestEveryCodeHasItsTexts` が両方を要求する）。外部の失敗なら `errmap` の表に条件を足し、`errmap_test.go` に行を足す。サービスが自分で言葉にする拒否なら、`apperr.New(code).WithCause(…)` を返す。
- 画面に出すとき: ハンドラが `apperr.View` を作り（`apperr.As(err)` か `errmap.Explain(err)` の `View(apperr.LangJa)`）、templ で `@errorbox.Box(errorbox.Props{View: v, Lang: apperr.LangJa, ID: <画面内で一意の id>})`。
- **#1009 の実装（2026-11-03）**: 実行前の検査が `input_missing`（必須の入力が無い。`param`＝`first_frame` など、原因は「「<モデル>」は最初のフレームの画像が必要です。添えた画像が「参照」になっています。「最初のフレーム」に変えてください」を `WithCause` で上書き）と `input_unused`（添えた画像がどのモデルにも使われない）を、ベンダーに送る前に返す（画面は `errorbox.Box`、MCP は `failure`）。`input_missing.remedy` に「添えた画像が「参照」になっているときは「最初のフレーム」に変えてください」を足した。422 の後追い（`byMessage` の `the workflow needs`）は従来どおり残る（検査をすり抜けた場合の保険）。詳しくは [gentest/gentest.md](gentest/gentest.md) §5.1。
- **#1009（I2V の first_frame 422）**: 同梱 ComfyUI の「the workflow needs first_frame (12), the request has none」は、すでに `input_missing`（`param=first_frame`、`node=12`）になり、結果の `failure`・詳細のエラーボックスに出る。使われなかった入力の表示は、結果の `used_inputs` と合わせて、この `failure` の `cause` / `remedy` を使うこと。新しい文を足すときは `input_missing.cause` / `.remedy`（カタログ）に足す。
- **#1011（UI）**: 結果のカードの 1 行（`Card.Error`）は、いまは `error`（原因 + 直し方 + 詳細の先頭）。カードにも `errorbox` を出すなら `Card.Failure`（`resulttile.Card`）がある。モデル管理の導入の失敗は、#1010 でエラーボックスにした（`JobStatus.Failure`。§6）。
- 履歴の古い失敗（`error_info` が空）は `error` の 1 文だけ。`failure` は無い。

## 10. 未確認

- 実機（GPU・実際の ComfyUI・各ベンダー）での、実際のエラーメッセージとの突き合わせ。`errmap` の文字列の判定（`out of memory`・`Value not in list` など）は、ComfyUI の公開されている挙動と既存のテストの文面からの推定で、実機では確かめていない。
- ブラウザでの「詳細をコピー」の実機操作（`static/src/components/copy` の wtest は偽のクリップボードで確かめた）。
- **ffmpeg の実際のエラーの文面**: 書き出し・Rough Cut・合成・ミキシングの `media_failed` は、ffmpeg の失敗を種類で分けず、出力の末尾を詳細に残すだけにした（壊れた素材・コーデックの非対応・書き込めない場所などの文面は、実機の ffmpeg の版ごとに違う。テストは、書き込めない出力先で実際の ffmpeg を失敗させた 1 例だけ）。
- **FaceFusion の実際の失敗の文面**: 実機の FaceFusion（顔が見つからない・成人向けの画像の拒否・GPU のメモリ不足・モデルの取得の失敗）が出す文面は確かめていない。`facefusion_failed` は終わり方（時間切れ・異常終了・出力なし）だけで分け、理由は出力の末尾（詳細）に任せる。出力に `out of memory` などがあれば `vram_oom` になる（ComfyUI 向けの判定を流用）。
- **アップデートの失敗の画面**: `update_*` のエラーボックスを、実際の GitHub への接続の失敗・取得回数の上限・検証の失敗で出して見ていない（種類は `updater` の `ErrorKind` のテストで確かめた分類のまま）。
- **ディスク不足・権限での設定の保存**: 読み取り専用のフォルダでの保存は確かめた（`web/ffmpegscreen`）が、本当にディスクが満杯のときの保存の失敗（`ENOSPC`）は実機では確かめていない（`errmap` の単体テストのみ）。
- 英語の表示は、画面にまだ言語の切り替えが無いため、箱（`errorbox`）に `LangEn` を渡したときの文面をテストで確かめただけで、ブラウザで英語の画面としては見ていない。英語の訳の質（言い回し）は未確認。

## 11. 改訂履歴

| 版 | 日付 | 変更 | 理由 |
|---|---|---|---|
| 1.0 | 2026-11-03 | 新規。共通のエラー型・変換層・画面部品・MCP のフィールド・i18n・載せ替え状況 | Issue #1010 |
| 1.1 | 2026-11-03 | `input_missing` の直し方に「添えた画像が参照のときは最初のフレームに変える」を足し、`input_unused`（添えた画像がどのモデルにも使われない）を新設。生成テストが送る前の検査で返す（§6 の表、#1009 の節） | Issue #1009 |
| 1.2 | 2026-11-03 | コード `local_llm_unreachable` を足した（ローカルの OpenAI 互換 LLM サーバーに接続できない。`errmap` は `localllm.IsUnreachable` で見分ける）。設計は `local-llm.md` | Issue #1008 |
| 1.3 | 2026-11-04 | コード `face_models_missing`・`face_runtime_missing` を足した（顔の類似度がオフのとき、役割 `face` の参照がある結果の `face_similarity.reason` / `fix` の言葉。YuNet・SFace の ONNX は mam が取得せず、カタログにも無いので、置き場所と `scripts/fetch-face-models.sh` を直し方に書く）。[local-models.md](local-models.md) §4.9 | Issue #1026 |
| 1.4 | 2026-11-04 | 載せ替えなかった経路（§6）の見直し: ffmpeg の失敗（書き出し・Rough Cut・Take の合成・音声のミキシング。`media_failed`・`ffmpeg_missing`）、アップデート（`update_*`）、設定の書き込みの失敗（`errmap.SaveFailure`・`web/handler/failure.Save`）、FaceFusion（`facefusion_unavailable`・`facefusion_failed`）を載せ替えた。ChatGPT / Codex などは理由を更新して見送り。i18n（§7）: 言語によって変わる語（時間・Agent の名前・区切り・作業の名前）をカタログのキーにし、英語の辞書が `errmap` の全コードを持つことと、未訳の日本語へのフォールバックをテストで固定した。MCP のツールエラーは、分からない失敗にも 2 行目の `failure`（`unknown`）を付ける | Issue #1010（残り） |
| 1.5 | 2026-11-04 | `face_models_missing` の直し方を、モデルマネージャーの「顔チェック用モデル」からの導入（ボタン 1 つ・再起動なし）に変え、案内先に `/model-manager` を足した（手で置く方法と `--face-model-dir` は残す）。`face_runtime_missing` の直し方に「モデルを入れ直しても直らない。ONNX Runtime のライブラリを用意する」を足し、2 つの理由を取り違えないようにした。再起動の案内を外した（導入・ライブラリの追加は次の計測から効く。エンジンは読み込めなかった結果を覚えない）。[model-manager/model-manager.md](model-manager/model-manager.md) §5.4 | Issue #1026 |
| 1.6 | 2026-11-04 | コード `credits_insufficient`（HTTP 402）と `topaz_*` の 7 つ（Topaz Video API の失敗。`topaz_job_interrupted` は受理のあとに mam が止まったときの、リクエスト ID つきの文）を足した | Issue #1031 |
| 1.7 | 2026-11-06 | モデルの導入の失敗（エンジン・カタログ・Hugging Face・カスタムノード・ローカルランタイム）とジョブ一覧の失敗の行を、共通のエラーボックスで出すようにした（§6）。コード `install_cancelled`（利用者が導入を止めた）を足した。導入の側が言葉にした失敗（取得元が消えた・応答しない）は `install_failed` でその文を原因に残す。`worktrack` が `ErrorInfo` を持つ。`apperr.ViewOf` を足した | Issue #1010 |
| 1.8 | 2026-11-06 | コード `image_model_pinned` を足した（`Options.Driver` で固定した Codex のモデルが ChatGPT のアカウントに拒否された。`image_unavailable` の「mam を最新版に更新して」は当てはまらない）。`errmap` は `chatgpt.ErrImageModelPinned` で見分ける | Issue #1071 |
| 1.9 | 2026-11-06 | `facefusion_failed`・`facefusion_unavailable`・`ffmpeg_missing` の導線を、設定画面からモデルマネージャーの `companion`（`#mm-facefusion`・`#mm-ffmpeg`）へ移すと決めた。直し方の文の「設定画面の…」を「モデルマネージャーの「付随ソフト」」に改める（`apperr` の `Place` の行き先と `messages_ja.go`・`messages_en.go`。状態は仕様のみ） | Issue #1106 |
| 1.10 | 2026-11-07 | `facefusion_failed`・`facefusion_unavailable`・`ffmpeg_missing` の導線を、モデルマネージャーの `companion`（`#mm-facefusion`・`#mm-ffmpeg`）へ移した（#1106）。直し方の文を「モデルマネージャーの「付随ソフト」」に改め、`facefusion_unavailable` は入れ方の手順がそこにあることを言う（`apperr` の `Place` の行き先と `messages_ja.go`・`messages_en.go`）。状態は「実装済み」 | Issue #1106 |
| 1.11 | 2026-11-08 | コード `no_usable_model`（クラス `unavailable`。画像・動画の生成に使えるモデルがない）と、`*modelroute.NoUsableError` の対応を足した。直し方は API キーの登録・ChatGPT のログイン・ローカルモデルの導入・割り当ての変更 | Issue #1113（既定の割り当てが API キーのないモデルだけを指すと、Reference Pack がすべて失敗し、原因がチャットに出なかった） |
| 1.12 | 2026-11-10 | 画面名・機能名の「生成テスト」を「単発生成」に改めた（本文の文言だけ。仕様は変えない。過去の改訂履歴の文は当時のまま） | Issue #1137 |
| 1.13 | 2026-11-11 | §6 の表で、同じ 2 行（単発生成のプロンプトの英訳の失敗・プロンプトの似せ合わせ）が重複していたので、後ろの 2 行を消した（内容は変えない） | PR #1141 のレビュー指摘 |
| 1.14 | 2026-11-10 | §6 に、生成テストの画面の 3 か所（`comfyscreen` の選び口・`tierform` のおすすめ・`faceswapscreen` の箱）・`mcp/server` の待ち受けの理由・`list_local_runtime_models` の絞り込みの誤りを足した（`err.Error()` を出していた落ち方を、`failreport.Line` / `gtshare.Problem` / `invalid_input` で共通のエラーにした）。§2 の `failreport` に `Line` を足した | Issue #1010（残りの経路。悠太さんの決定: この 5 か所だけを載せ替える） |
| 1.15 | 2026-11-10 | `failreport.Line` が、元のメッセージ（詳細の先頭 200 字）を 1 行に出すのをやめ、ログにだけ残すようにした（知っている失敗も WARN で `code` と元のメッセージを書く）。`mcp/server` の `Start` の残り 2 か所（保存済みの待ち受け先の検査の `%v`・待ち受けの失敗の `%v`）も `failreport.Line` にした（`bind` の拒否は日本語の文なのでそのまま） | PR #1135 のレビューの minor（悠太さんの決定: 小さい指摘 4 つも直す） |
| 1.16 | 2026-11-10 | `mcp/server` の `Manager` の理由で、`*net.OpError`・`*fs.PathError` は `errmap` の表でなくこの待ち受けの文で言うようにした（実際の listen の失敗が「接続先につながりません…ComfyUI」、`mcp.json` の権限の失敗が「書き込めません」と出ていたのを直した）。それ以外の失敗は `failreport.Line` のまま | PR #1135 の再レビュー（major） |
| 1.17 | 2026-11-10 | `mcp/server` の理由に、`mcp.json` の中身の誤り（`bind.ErrMalformed`。JSON の誤り・`bind_address` が文字列でない）と、ネットワークインターフェースの一覧の失敗（`LocalAddrs`）の文を足した（それまでは「原因がわかりませんでした」と、「アドレスとポートを開けませんでした」だった）。`failreport` が元のエラーをログへ書くときは `failreport.Logged`（`logging.Redact`）で秘密を伏せるようにした（`Line`・`Media` の ERROR・WARN と、`mcp/server` の WARN） | PR #1135 の再レビューの minor（悠太さんの決定: 残りの minor も直す） |
