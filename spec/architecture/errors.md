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
| `backend/internal/apperr/errmap` | 外部の失敗 → `*apperr.Error`（`Explain`・`Known`・`Report`・`LocalTimeout`） | `apperr`、`agent/llm`、`agent/modeladapter`、`agent/audiodirector`、`chatgpt`、`media`、`modelmgr` |
| `backend/internal/agent/llm/failmsg` | LLM のキー未設定（`NoKeyError`）と Agent の名前（`Label`）。`NoKeyMessage` は `NoKeyError` の 1 文 | `apperr`、`agent/llm` |
| `backend/internal/web/molecules/errorbox` | 画面の共通エラー表示（templ） | `apperr` だけ（`web/` はサービス層を import しない） |
| `static/src/components/copy` | 「詳細をコピー」（既存のコピーボタンに `data-copy-done` と textarea を足した） | |

`agent/jobqueue`・`agent/generation`・`agent/orchestrator`・`service`・`web/handler`・`mcp/mcpkit` が `apperr` / `errmap` を使う。`errmap` は上の層を import しない（循環しない）。

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
```

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
| `disk_full` | `unavailable` | ディスクの空き容量が足りません。 | /model-manager |
| `download_corrupt` | `upstream` | ダウンロードしたファイルが、固定した SHA-256 または大きさと一致しませんでした。破棄しました（取得元の改ざんか、転送の破損の可能性があります）。 | /model-manager |
| `endpoint_timeout` | `upstream` | ローカルの生成が接続先の待ち時間（）を超えたため打ち切りました。 | /settings/local-models |
| `engine_not_installed` | `unavailable` | ComfyUI エンジンが導入されていません。 | /model-manager<br>/settings/local-models |
| `forbidden` | `forbidden` | Brand / Safety Constraint などの制約に違反しています。 |  |
| `generation_time_limit` | `upstream` | 生成が制限時間（Max generation time）を超えました。 | /settings |
| `image_unavailable` | `unavailable` | ChatGPT サブスクリプションでの画像生成に使える Codex のモデルが見つかりませんでした（OpenAI が ChatGPT ログインで使えるモデルを入れ替えた可能性があります）。 | /settings/chatgpt |
| `input_missing` | `validation` | このモデルが必ず使う入力（）が渡されていません。 |  |
| `input_unsupported` | `validation` | このモデルは、渡された入力画像や解像度の組み合わせを受け付けません。 |  |
| `input_unused` | `validation` | 添えた入力画像は、選んだモデルでは使われません。 |  |
| `install_failed` | `upstream` | 導入を完了できませんでした。 | /model-manager |
| `invalid_input` | `validation` | 入力が正しくありません。 |  |
| `key_missing` | `unavailable` | API キーが設定されていません。 | /settings/api-keys |
| `license_not_accepted` | `conflict` | このモデルのライセンスに、まだ同意していません。 | /model-manager |
| `llm_invalid_output` | `upstream` | LLM の答えが、求めた形式になっていませんでした。 | /settings/llm |
| `local_llm_unreachable` | `upstream` | ローカルサーバーに接続できません。ローカルの LLM サーバー（Ollama・LM Studio・llama.cpp server など）が応答しませんでした。 | /settings/llm |
| `llm_key_missing` | `unavailable` | LLM の API キーが設定されていません。 | /settings/api-keys<br>/settings/llm |
| `locked` | `locked` | LOCK されている項目は変更できません。 |  |
| `media_timeout` | `upstream` | 動画・音声の処理（ffmpeg）が制限時間を超えました。 |  |
| `model_not_installed` | `unavailable` | 必要なモデルのファイルが見つかりません。 | /model-manager |
| `no_gpu` | `unavailable` | 対応する GPU が見つかりません。 | /model-manager<br>/settings/local-models |
| `node_missing` | `unavailable` | ワークフローが使うカスタムノードが、ComfyUI にありません。 | /model-manager |
| `not_found` | `not_found` | 対象が見つかりません。 |  |
| `not_writable` | `unavailable` | 保存先のフォルダに書き込めません。 | /model-manager |
| `rate_limited` | `rate_limited` | 相手のサービスの利用回数の上限（レート制限、HTTP 429）に達しました。 |  |
| `remote_not_found` | `upstream` | 接続先に、呼び出した URL かモデルがありません（HTTP 404）。 | /settings/local-models<br>/model-manager |
| `request_rejected` | `upstream` | 相手がリクエストを拒否しました。コンテンツポリシーに当たったか、入力や設定がそのモデルに合っていません。 |  |
| `save_failed` | `internal` | 設定やデータを保存できませんでした。 |  |
| `server_error` | `upstream` | 相手のサービス側で障害が起きています。 |  |
| `stalled` | `upstream` | ローカルの生成がのあいだ進まなかったため打ち切りました。 | /settings/local-models |
| `time_limit` | `upstream` | ローカルの生成が全体の上限（）を超えたため打ち切りました。 |  |
| `timeout` | `upstream` | 相手の応答が時間内に返りませんでした。 |  |
| `training_failed` | `upstream` | 声の学習を完了できませんでした。 | /model-manager |
| `unavailable` | `unavailable` | この機能はいま使えません（API キーや設定が足りません）。 | /settings |
| `unknown` | `internal` | 原因がわかりませんでした。想定していない失敗です。 |  |
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
| 429（`modeladapter.RateLimited`） | `rate_limited`（サーキットブレーカーより先。429 が開けた場合も 429 を原因にする） |
| `ErrCircuitOpen`（モデル・LLM） | `circuit_open` |
| `chatgpt.ErrNoImageModel` | `image_unavailable`（ベンダーの 400 は症状なので、詳細にだけ残す） |
| `direct.ErrInputsUnsupported` / `ErrInputsConflict` / `ErrResolution` | `input_unsupported` |
| `llm.ErrInvalidOutput` | `llm_invalid_output` |
| `localllm.IsUnreachable`（プロバイダー `local` の `APIError`: status 0・原因あり・タイムアウトでもキャンセルでもない） | `local_llm_unreachable`（`net.Error` より先） |
| `fetch.ErrChecksum` / `ErrSize` | `download_corrupt` |
| `store.ErrNotWritable`、`fs.ErrPermission` | `not_writable` |
| `*fetch.NoSpaceError`、`syscall.ENOSPC` | `disk_full`（場所を原因に入れる） |
| `*fetch.ConnectionLostError` | `connection_failed` |
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

サービスの拒否は `mmview.Failure`（`ErrConsentRequired` → `license_not_accepted`、`ErrEngineNotInstalled` → `engine_not_installed`、`ErrNotInstalled` → `model_not_installed`、`ErrNoGPU` → `no_gpu`、導入の失敗でインストーラーが言葉にしたもの → `install_failed`、その他の拒否は文を残して `invalid_input` / `not_found` / `conflict`）が `*apperr.Error` にする。生成テストの `Deps.Explain` とジョブキューの `Options.Explain` に渡してある。

## 6. 失敗の経路と載せ替え状況

| 経路 | 状況 | 内容・理由 |
|---|---|---|
| 生成テスト（`runner/execute.go`） | **載せ替えた** | 失敗を `*apperr.Error` にして、結果の `error`（1 行の日本語）と `error_info`（JSON。`gentest_result.error_info`）に残す。結果の詳細の画面は `errorbox.Box`。`record.ResultView.Failure`（`get_generation_result`・`get_generation_test`・`list_generation_results`）。ベンダーが 422 などで返した英語は `detail` にだけ出る。`gentest/failtext` の `LocalTimeout` は `errmap.LocalTimeout` に移した。テスト: `service/gentest/errtest`、`web/handler/gentest/screentest`（モーダルのエラーボックス）、`mcp/tools/gentest/errtest` |
| Shot の生成・Reference Pack（`agent/generation/failure.go`） | **載せ替えた** | `generation.Failure(err)` が `*apperr.Error`。チャットの失敗通知は原因 + 直し方（unknown は相関 ID つき）。ジョブの `error_info` に残る。`FailureReason` は `Failure(...).View().Sentence()`。テスト: `agent/generation/failuretest` |
| 企画・脚本・Shot 計画（`agent/orchestrator/failure.go`） | **載せ替えた** | 同上（`planning.ErrInvalidPlan` は `llm_invalid_output`）。unknown は ID を通知に入れ、ログにも残す。テスト: `agent/orchestrator`（失敗の通知） |
| ジョブキュー（`agent/jobqueue`） | **載せ替えた** | 失敗したジョブの `job.error_info` に `*apperr.Error` を残す（unknown は ID をログと一緒に）。`Options.Explain` でサービスの拒否を先に聞く。`get_job`・`list_jobs` の `failure`。テスト: `agent/jobqueue/failure_test.go`、`mcp/tools/jobs` |
| モデルの導入・ダウンロード | **載せ替えた**（ジョブ経由） | `mmview.Failure` → ジョブの `error_info`（空き容量不足は `disk_full`、取得元の認証は `auth_failed`、SHA-256 不一致は `download_corrupt`、ライセンス未同意・エンジン未導入・GPU なしはそれぞれのコード）。モデル管理の画面の導入の失敗は、従来の 1 文（`userFacing`）のまま（`error_info` を画面で読んでエラーボックスにするのは #1011）|
| 音声（`service/audio.go` の `audioError`） | **載せ替えた** | ElevenLabs のキー未設定・429・5xx・ディスク不足などは errmap の原因 + 直し方。ベンダーの応答は出さない。テスト: `service/audio_voice_test.go` |
| Continue・Command Interpreter・Audio Director の LLM（`service/continue.go` の `aiError`） | **載せ替えた** | `generation.FailureReason` 経由 |
| 画面のトースト（`web/handler/errors.go` の `statusOf`） | **載せ替えた**（フォールバック） | 既存の業務エラーの後で、`errmap` が知っている失敗（外部サービス・ディスク・ネットワーク）は原因 + 直し方とクラスのステータス、知らない失敗は従来どおり 500「内部エラーが発生しました」（原因はログ）。設定の保存・書き出しなどで、ハンドラが言葉にしていないディスク／権限の失敗はここで日本語になる。テスト: `web/handler/failure` |
| MCP のツールエラー（`mcpkit.MapError`） | **載せ替えた** | `ToolError.Failure`（`apperr.View`）。コードは従来のクラスのまま、メッセージは原因 + 直し方。テキストの 2 行目に View の JSON。`AIError` に包まれた失敗にも付く。テスト: `mcpkit/errors_test.go` |
| 生成テストのプロンプトの英訳の失敗（`failtext.Translate`） | 一部 | キー未設定の文は `failmsg.NoKeyError` の文。それ以外は従来の日本語の文（原因は書いてあり、直し方は「英訳を OFF」）。構造化（`failure`）はまだ付けない: 失敗が結果に残らず、リクエストの応答（`upstream`）で返るため |
| プロンプトの似せ合わせ（`gentest/match`） | 一部 | キー未設定は `failmsg.NoKeyMessage`。それ以外の失敗は 1 文のまま |
| 書き出し（`editor_export.go`）・Rough Cut・Final QC・Judge・Repair | 載せ替えない | 失敗の文は、ffmpeg が無い・動画が無い・状態の衝突など、**サービス自身の拒否で、すでに日本語の原因と直し方が書いてある**。外部の失敗（ディスク・タイムアウト）はトースト／チャットの経路（上の 3 行）で日本語になる。`*apperr.Error` に書き換える利点が小さい |
| アップデート（`appupdate`） | 載せ替えない | 状態（`appupdate.View`）が文を持ち、「失敗」は画面の状態として出る。エラーの 1 経路ではない |
| 設定の保存（各 `settings` のハンドラ） | 載せ替えない（入力検証）／上のトースト（ディスク） | 入力の誤りは、フィールドごとの検証文（`service.ValidationError`）。ディスク・権限の失敗は `statusOf` のフォールバックで日本語になる |
| ChatGPT ログイン・Codex・FaceFusion・ローカル音声の導入など、各機能の画面に出す固有の失敗 | 載せ替えない | それぞれの画面が専用の部品と文を持つ（ログインの状態など）。範囲が広いので、#1011 で画面ごとに `errorbox` へ寄せるかを決める |

## 7. i18n

- カタログ `apperr.Text(lang, key, args)`。`lang` は `apperr.LangJa`（基準）か `apperr.LangEn`。キーは `<code>.cause`・`<code>.remedy`・`place.<place>`・`ui.<name>`・補助の文（`advice.chatgpt` など）。英語に無いキーは日本語で読む。どちらにも無いキーはキーそのものが出る（テストで検出する）。
- 文の中の `{name}` は `Error.Args` の値。無い引数は空になる。`{{key}}` は別のキーの文を入れる（引数も引き継ぐ）。条件で文が入る・入らない部分（キーの無い LLM、再起動待ちなど）は、引数に `{{key}}` を入れて切り替える。
- `apperr.ParseLang("en-US,en;q=0.9")` は `Accept-Language` から言語を決める。画面は現状どこも `LangJa` を渡している（**画面全体の i18n は範囲外**）。`errorbox` の `Props.Lang` に `LangEn` を渡すと、箱の文言と原因・直し方が英語になる。MCP は日本語（相手はエージェントと日本語の利用者）。
- テスト `apperr_test.go` が、全コードに日本語と英語の原因と直し方があること、日英のキーが一致することを確かめる。

## 8. MCP のフィールド

- `get_generation_result`・`get_generation_test` の `results[]`・`list_generation_results`: 失敗した結果の **`failure`**（従来の `error`（1 行）はそのまま）。
- `get_job`・`list_jobs`: 失敗したジョブの **`failure`**（従来の `error`・`error_code`・`error_id`・`next_steps` はそのまま）。履歴（`mcp_job_history.error_info`）に残るので、ジョブの行が消えたあとも読める。
- ツールエラー: `ToolError.Failure`。エラーの文は `<クラス>: <原因><直し方>`（従来どおり 1 行目）、既知の失敗では 2 行目に `failure` の JSON。

`failure` のキー: `code`（§4。エージェントはこれで分岐する）・`class`・`cause`（日本語の原因）・`remedy`（日本語の直し方）・`settings[]`（`label`・`path`。mam の画面のパス）・`detail`（元のメッセージ。英語のことがある）・`error_id`（unknown のとき。`get_app_logs` の `attrs` に `{"error_id": …}` を渡すと原因のログが見つかる）。

## 9. 後続の作業者へ

- 新しい失敗の種類を足すとき: `apperr/code.go` にコードとクラス・導線を足し、`messages_ja.go`・`messages_en.go` に `<code>.cause`・`<code>.remedy` を足す（`TestEveryCodeHasItsTexts` が両方を要求する）。外部の失敗なら `errmap` の表に条件を足し、`errmap_test.go` に行を足す。サービスが自分で言葉にする拒否なら、`apperr.New(code).WithCause(…)` を返す。
- 画面に出すとき: ハンドラが `apperr.View` を作り（`apperr.As(err)` か `errmap.Explain(err)` の `View(apperr.LangJa)`）、templ で `@errorbox.Box(errorbox.Props{View: v, Lang: apperr.LangJa, ID: <画面内で一意の id>})`。
- **#1009 の実装（2026-11-03）**: 実行前の検査が `input_missing`（必須の入力が無い。`param`＝`first_frame` など、原因は「「<モデル>」は最初のフレームの画像が必要です。添えた画像が「参照」になっています。「最初のフレーム」に変えてください」を `WithCause` で上書き）と `input_unused`（添えた画像がどのモデルにも使われない）を、ベンダーに送る前に返す（画面は `errorbox.Box`、MCP は `failure`）。`input_missing.remedy` に「添えた画像が「参照」になっているときは「最初のフレーム」に変えてください」を足した。422 の後追い（`byMessage` の `the workflow needs`）は従来どおり残る（検査をすり抜けた場合の保険）。詳しくは [gentest/gentest.md](gentest/gentest.md) §5.1。
- **#1009（I2V の first_frame 422）**: 同梱 ComfyUI の「the workflow needs first_frame (12), the request has none」は、すでに `input_missing`（`param=first_frame`、`node=12`）になり、結果の `failure`・詳細のエラーボックスに出る。使われなかった入力の表示は、結果の `used_inputs` と合わせて、この `failure` の `cause` / `remedy` を使うこと。新しい文を足すときは `input_missing.cause` / `.remedy`（カタログ）に足す。
- **#1011（UI）**: 結果のカードの 1 行（`Card.Error`）は、いまは `error`（原因 + 直し方 + 詳細の先頭）。カードにも `errorbox` を出すなら `Card.Failure`（`resulttile.Card`）がある。モデル管理の導入の失敗は `job.error_info`（`apperr.Unmarshal`）から読める。
- 履歴の古い失敗（`error_info` が空）は `error` の 1 文だけ。`failure` は無い。

## 10. 未確認

- 実機（GPU・実際の ComfyUI・各ベンダー）での、実際のエラーメッセージとの突き合わせ。`errmap` の文字列の判定（`out of memory`・`Value not in list` など）は、ComfyUI の公開されている挙動と既存のテストの文面からの推定で、実機では確かめていない。
- ブラウザでの「詳細をコピー」の実機操作（`static/src/components/copy` の wtest は偽のクリップボードで確かめた）。

## 11. 改訂履歴

| 版 | 日付 | 変更 | 理由 |
|---|---|---|---|
| 1.0 | 2026-11-03 | 新規。共通のエラー型・変換層・画面部品・MCP のフィールド・i18n・載せ替え状況 | Issue #1010 |
| 1.1 | 2026-11-03 | `input_missing` の直し方に「添えた画像が参照のときは最初のフレームに変える」を足し、`input_unused`（添えた画像がどのモデルにも使われない）を新設。生成テストが送る前の検査で返す（§6 の表、#1009 の節） | Issue #1009 |
| 1.2 | 2026-11-03 | コード `local_llm_unreachable` を足した（ローカルの OpenAI 互換 LLM サーバーに接続できない。`errmap` は `localllm.IsUnreachable` で見分ける）。設計は `local-llm.md` | Issue #1008 |
