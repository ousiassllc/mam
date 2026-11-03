# MCP ツール: 生成テスト

[`mcp-tools.md`](mcp-tools.md) の規約に従う、生成テスト画面（[endpoints-gentest.md](endpoints-gentest.md)）の全操作のツール（Issue #835）。意味は [../architecture/gentest/gentest.md](../architecture/gentest/gentest.md)（画面・ジョブ・失敗の扱い）と [../architecture/gentest/gentest-adapter.md](../architecture/gentest/gentest-adapter.md)（モデルの直接呼び出し・シード）。

実装は `backend/internal/mcp/tools/gentest`。画面と同じ `gentest.Service`（`service.Services.Gentest`）を呼ぶ。アプリが持たないとき（`Gentest` が nil）は全ツールが `unavailable`。

## 方針

- **見積りの確認を強制する**: `run_generation_test` は `estimate_generation_test` が返した `estimate_id` と `confirmed_total_usd`（= `total_usd`）を必須とする。サーバーは同じ入力で見積りをやり直し、`estimate_id` か合計が違う（単価・設定・入力が変わった）、または上限（`gentest.max_cost_usd`・`gentest.max_items`）を超えるときは `conflict` で拒否し、ジョブも結果の行も作らない。単価が分からないモデル（`unknown`）は合計に入らないので、エージェントは `warnings` と `unknown_models` を**ユーザーに見せて**から実行する。
- **英訳（`translate_to_english`。Issue #913）**: `estimate_generation_test`・`run_generation_test` の引数。**省略時は `true`（画面のスイッチの初期値と同じ）**。`true` のとき、プロンプトとネガティブプロンプトのうち日本語など**ラテン文字以外の文字を含むものだけ**を、LLM（設定の「LLM の割り当て」の Prompt Planner）で英語に訳してから送る。すでに英語の文は訳さず、訳す文が無ければ LLM を呼ばない。訳すのは `run_generation_test` のときに 1 回だけ（全モデル・全本数で同じ英語）。**訳せなければ何も生成せず `upstream`（ジョブ・バッチ・結果の行は作らない。日本語のまま送らない）**。見積りには、訳す文の数の 1 行（`breakdown` の `model` が `translate`、`basis` は `unknown`）が入る。LLM には単価が無いので `total_usd` には入らず、`unknown_models` に `translate` が載り、警告 `no_price`（`model` が `translate`）が付く（`unknown_items`・`total_items` は結果の数なので増えない）。`estimate_id` は `translate_to_english` を含むので、切り替えたら見積りをやり直す。結果（`results[]`）の `prompt`・`negative_prompt` は入力した文、`translated`・`translated_prompt`・`translated_negative_prompt` は訳した英語、`sent_prompt` は実際に送った文。`false` なら訳さず入力のまま送る。
- **参照画像・LoRA（Issue #933）**: 画像モデルの `references[]` の各画像に `role`（`reference`＝全体・既定 / `face`＝顔だけ / `style`＝画風・雰囲気だけ / `edit`＝編集する元の画像 / `pose`＝ポーズだけ（下の「ポーズの参照」）。動画の `first_frame` / `last_frame` は動画だけ）と `strength`（0〜1。省くと 0.7。モデルごとの実現方法は隠す。動画では指定できず `validation`。強さを実現できるのは同梱 ComfyUI の参照対応モデルだけで、クラウドの画像モデルなどでは**警告なしに無視される**）を付けられる。リクエスト全体の `avoid_reference_pose`（画像だけ）は、**参照を 1 枚でも使うモデル**のネガティブプロンプトに「同じポーズ・同じ構図」を足す（ネガティブの欄が無いモデルは `Avoid: …` として書き足し、`negative_folded` の警告が出る。参照を使わないモデルには何もしない。参照画像が無い・参照を使うモデルが無いときも何も起きず、警告は出ない）。`lora`（`name`・`strength` 0〜1、省くと 0.4。画像だけ。`name` は 1〜200 文字で制御文字を含まない）は参照画像と一緒に使う。強さ 0 は 0 のまま保存し、モデルには既定（0.4）ではなくごく小さい値で送る。`name` は `list_generation_models` の `loras`（同梱 ComfyUI エンジンの LoRA ファイル名。エンジンが止まっていれば空。`media` が `video` のときはエンジンに問い合わせず空）から選ぶ。モデルごとの対応は `list_generation_models` の `reference_mode`（`edit`＝編集モデル: 複数枚・役割あり / `img2img`＝1 枚から始める）・`max_reference_images`・`reference_roles`・`supports_lora`。対応しない分は拒否せず**警告**にして、その分だけ使わない: `inputs_dropped`（参照を使えないモデル）、`too_many_refs`（枚数超過。先頭から `max_reference_images` 枚だけ使う）、`role_unsupported`（そのモデルが取らない役割。画像は全体の参照として使う）、`lora_unsupported`（LoRA は送らない）。クラウドモデルは `accepts_references` が true でも `max_reference_images` は 0（不明）・`reference_roles` は `["reference"]`。使った `avoid_reference_pose` と `lora` は結果（`avoid_reference_pose`・`lora`）に、各入力の `role`・`strength`（既定のときは省略）は `get_generation_result` の `inputs[]` に残る。
- **ポーズの参照（Issue #986）**: `references[]` の `role` に `pose`（ポーズだけ。体のポーズとカメラの角度だけを写す。画像のみ）と、`source` に `pose_library`（Shisei のポーズ集の画像。`id` は `std_P001_front` の形）を足した。`source` には `batch_input`（前のバッチの入力画像。`id` は結果の `inputs[].input_id`。アップロードした画像を使った結果を同じ入力で作り直すとき。その結果をすべて削除すると使えなくなる）も取れる（Issue #977）。ポーズ集は画面（`/generation-test` の「ポーズを選ぶ」）で利用者が同意して導入したものだけが使え、未導入なら導入の手順を添えて拒否する（導入そのものの MCP ツールは無い）。`list_generation_models` の `reference_roles` に `pose` が出るモデル（Qwen-Image 2.1・Qwen-Image-Edit-2511）だけがポーズを読む。読めないモデルは全体の参照として使い、警告 `role_unsupported` を返す。
- **一部だけ描き直す（Issue #974）**: `estimate_generation_test`・`run_generation_test` の `mask`（`references` と同じ `source` / `id` / `project_id`。`role` と `strength` は使わない。白が描き直す、黒が残す）と `denoise`（0 より大きく 1 以下。省くと各モデルの `denoise_default`）。元画像は `references` に 1 枚だけ。`list_generation_models` の `supports_inpaint` が false のモデルを含めると `validation`（選べない。警告ではない）。真っ黒なマスク、マスク無しの `denoise`、動画も `validation`。`denoise` を受け取れないモデルは警告 `denoise_ignored` でテンプレートの既定のまま作る。結果の `inpaint`（`denoise`・`source_kind`・`source_id`・`mask_input`）に使った値と元画像が残る。マスクは参照としては送らない。
- **モデルは指定どおりに呼ぶ**: プロンプトは補正せずそのまま送る（英訳が `true` のときは英訳だけを加える）。モデルが使えない（`list_generation_models` の `available` が false）・落ちたときも代替モデルへは切り替えない（その結果が `failed`）。ネガティブの欄が無いモデルへは「Avoid: …」としてプロンプトに書き足し、結果の `negative_mode`（none / field / folded）と `sent_prompt` に残す。
- **シード**: `supports_seed` のモデル（FLUX.2・Runway Gen-4.5・シードの割り当てがあるローカルの ComfyUI モデル）にはサーバーが結果ごとに別のシードを作って送り、送った値を `seed` に残す。ほかのモデルは同じ入力の n 回の呼び出しで、`seed` なし・`seed_sent` が false。
- **課金・台帳に入らない**: Take・コスト台帳・アセットは作らない。合計コストは `list_generation_results` の `spend` だけ（解析・CostSummary には出ない）。
- **長い操作は `job_id` を返す**: `run_generation_test`。`get_job` は種別 `gentest` の Probe で結果の件数から進捗を返す（下の「ジョブ」）。再起動後も読める。ジョブは再開しない（mam が落ちたとき、終わっていない結果は `failed`・理由「中断」。終わった結果は残る）。
- **ファイルは URL で受け渡す**: 入力画像はアップロードの用途 `gentest_reference`、結果のファイルは `get_generation_result` の `file`（ダウンロードの種類 `gentest_file`。トークン付き・15 分。base64 は返さない）。複数の結果は `export_generation_results` が ZIP にまとめる（ダウンロードの種類 `gentest_export`。Issue #932）。
- **拒否**: 入力の誤り（モデル・プロンプト・本数・縦横比・解像度・長さ・入力画像・使えないモデル・英訳に使う LLM が無い）は `validation`、英訳に LLM が失敗した（API キー未登録・サーキットブレーカー・タイムアウト・訳になっていない答え）ときは `upstream`、見積りの不一致・上限超え・終わっていないものの削除・終わったバッチの取り消しは `conflict`、無いものは `not_found`。取り込みのスロットが LOCK されていれば `locked`（画像は取り込み済みで、メッセージに `asset_id`・`reference_id` を含む）。

## ツール

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_generation_models` | 選べるモデル（組み込みのクラウドモデル・ローカル・外部 ComfyUI・保存したワークフロー・ChatGPT サブスク）。使えないものは理由つき。各モデルの `license` に商用利用の可否（#928。[model-license.md](../architecture/model-license.md)） | `media`（省略可）, `commercial`（`all` / `ok` / `ok_or_conditional`。省くと設定「商用OKのモデルだけを使う」に従う。不正な値は `validation`） | `models[]`（`id`, `label`, `media`, `source`, `available`, `unavailable`, `unavailable_message`, `accepts_references`, `reference_mode`, `max_reference_images`, `reference_roles`, `supports_lora`, `video`（`durations`, `resolutions`, `max_reference_images`, `first_frame`, `last_frame`, `native_audio`）, `supports_seed`, `negative_field`, `price`（`basis`: priced / free / unknown, `per_image_usd`, `per_second_usd`）, `license`（`commercial`: `yes` / `conditional` / `no` / `unknown`, `license_name`, `condition`, `source_url`, `checked_at`, `notice`））, `commercial_filter`（適用した絞り込み）, `loras[]`（LoRA ファイル名。昇順）, `limits`（`max_items`, `max_cost_usd`） | はい |
| `estimate_generation_test` | バッチ（モデル × プロンプト × 本数）の本数と見積り。実行しない | `media`, `models[]`, `prompts[]`（`prompt`, `negative_prompt`）, `count`, `aspect_ratio`, `resolution`, `duration_seconds`, `translate_to_english`（省略時 true）, `references[]`（`source`: upload / project_asset / reference_item / history / pose_library, `id`, `project_id`, `role`: reference / face / style / edit / pose（画像）・reference / first_frame / last_frame（動画）, `strength`: 0〜1（画像のみ））, `avoid_reference_pose`（画像のみ）, `lora`（`name`, `strength`。画像のみ）, `speed_mode`（動画のみ。モデルの `video.speed_modes` の `id`。省くと既定） | `estimate_id`, `total_items`, `total_usd`, `breakdown[]`（`model`, `items`, `basis`, `unit_usd`, `subtotal_usd`）, `unknown_models`（英訳の行は `translate`）, `unknown_items`, `warnings[]`（`code`, `model`, `message`）, `over_limit`, `max_items`, `max_cost_usd`, `request` | はい |
| `run_generation_test` | 確認した見積りどおりに実行する（ジョブ） | 上と同じ + `estimate_id`, `confirmed_total_usd` | `job_id`, `state`, `batch_id`, `total_items` | いいえ（外部へ接続・課金あり） |
| `estimate_prompt_match` | 参考の画像 1 枚から画像モデルで似た絵を出すプロンプトを作る「プロンプトマッチ」の見積り（Issue #916）。最大の回数 × モデルの単価（ローカルは無料）に、LLM の呼び出しの見積りを足す（Issue #965。プロンプトの作成・修正は `match_llm`、比較は `match_llm_compare` の行。トークン単価は `llm.PriceOf` の表に載った公式の価格だけで、載っていないモデル・ChatGPT のプランの行は単価なしで合計に入らず、警告 `no_price`）。上限超過は `over_limit` つき。実行しない | `reference`（`source`, `id`, `project_id`）, `model`, `mode`（`auto` 既定 / `once` / `adjust`）, `target_score`（1〜100、既定 80）, `max_rounds`（1〜10、既定 5）, `max_cost_usd`（この実行で使ってよい費用。見積りが超えるなら `over_limit`）, `aspect_ratio`, `instruction`、続きは `continue_batch_id` と `mode`・`instruction` | `estimate_id`, `request`, `rounds`, `breakdown[]`, `total_usd`, `warnings[]`, `max_cost_usd`, `over_limit` | はい |
| `run_prompt_match` | 確認した見積りどおりに実行する（ジョブ。種別 `gentest`）。サーバーが見積りをやり直し、`estimate_id`・合計が違う、上限超過は `conflict`（LLM も呼ばず何も生成しない） | 上と同じ + `estimate_id`, `confirmed_total_usd` | `job_id`, `state`, `batch_id`, `total_items`（最大の回数） | いいえ（外部へ接続・課金あり） |
| `get_prompt_match` | マッチの状態と全ての回 | `batch_id` | `finished`, `stop`（`reached` / `limit` / `cancelled` / `error`）, `error`, `target_score`, `best_round`, `best_score`, `best_prompt`（そのまま使える。生成テストの `prompts` に渡す）, `rounds[]`（`round`, `result_id`, `status`, `prompt`（英語）, `score`, `scoring`（顔を混ぜているときだけ。`llm` = LLM の点、`face` = `cosine`・`points`・`weight`、顔を使わなかったときは `face_skipped` = `unavailable` / `reference_no_face` / `generated_no_face` / `error`。Issue #964）, `differences[]`, `based_on`, `base_prompt`（直す前）, `change`（直したところ）, `instruction`, `error`, `best`） | はい |
| `instruct_prompt_match` | **実行中**のマッチに、言葉（日本語でよい）の指示を足す。次にプロンプトを直すときに取り込む。終わったマッチは `conflict` | `batch_id`, `instruction` | `status` | いいえ |
| `get_generation_test` | バッチの進捗と全結果。動いているローカルの ComfyUI の生成があれば、その進み具合 `progress`（`node`, `step`, `max_step`, `percent`, `eta_seconds`。取れないときは省く。Issue #966） | `batch_id` | `state`, `total`, `done`, `queued`, `running`, `succeeded`, `failed`, `cancelled`, `finished`, `estimate`, `progress`, `results[]`（下の結果） | はい |
| `cancel_generation_test` | バッチを取り消す。始めていない結果は取り消し、呼び出し中のものは終わるのを待つ | `batch_id` | `cancelled`, `running` | いいえ |
| `list_generation_results` | 履歴（新しい順） | `model`, `media`, `favorite`, `limit`, `offset` | `results[]`, `spend`（`items`, `priced_usd`, `unknown_items`） | はい |
| `get_generation_result` | 結果 1 件（入力画像・取り込み先・ファイルの URL） | `result_id` | 結果 + `inputs[]`（`input_id`, `role`, `strength`, `mime_type`, `size_bytes`, `origin`）, `imports[]`, `file`（`url`, `expires_at`） | はい |
| `export_generation_results` | 結果を ZIP にまとめるダウンロード URL を発行する（Issue #932。15 分有効）。ZIP は `photos/`（画像・動画。`01_20261101-173805_chroma1-hd_bce291.png` = 番号 2 桁以上・作成時刻（JST）・モデル・結果 id の末尾 6 文字）と `prompts/prompts.csv`（列 `file`, `created_at`（JST）, `media`, `model`, `seed`, `prompt`, `sent_prompt`, `result_id`。UTF-8 BOM つき。`file` は `photos/` のファイル名と同じ）。選び方は `result_ids[]`（最大 500）/ `batch_id` / `latest`（新しい順の N 件、1〜500。`result_ids`・`batch_id` が無ければ省略時 40）のどれか（何も無ければ直近 40 件）に、`favorite`・`models[]`・`media`・`commercial`（`all` / `ok` / `ok_or_conditional`。省略時 `all`。設定「商用OKのモデルだけを使う」には従わない）が **AND** で重なる。成功してファイルのある結果だけが入り、番号は古い順の 1 から。大きいときは `split_items`（1 つの ZIP の最大件数）・`split_mb`（ファイル合計の上限 MB。超える結果から次の ZIP。1 件で超えるものはその 1 件で 1 つ）で分け、番号は全体の通し番号（2 つ目の ZIP は続きの番号から。最大 20 個） | `result_ids`, `batch_id`, `latest`, `favorite`, `models`, `media`, `commercial`, `split_items`, `split_mb` | `files[]`（`url`, `expires_at`, `file_name`, `part`, `items`, `first`, `last`, `bytes`）, `items[]`（`number`, `file`, `result_id`, `media`, `model`, `created_at`）, `skipped[]`（指定したが入らなかった `result_id`） | はい |
| `delete_generation_result` | 結果とファイルを削除（生成中は `conflict`。バッチの最後の 1 件ならバッチも消える） | `result_id` | `id`, `deleted` | いいえ（破壊的） |
| `set_generation_favorite` | お気に入りの付け外し（冪等） | `result_id`, `favorite` | `result_id`, `favorite` | いいえ |
| `list_generation_prompts` | プロンプト履歴（同じ（プロンプト, ネガティブ）は 1 件。使用回数・最後に使った日時） | なし | `prompts[]` | はい |
| `delete_generation_prompt` | プロンプト履歴を 1 件削除（結果の履歴には影響しない） | `prompt_id` | `id`, `deleted` | いいえ（破壊的） |
| `import_generation_result` | 成功した結果を Project に取り込む（お気に入りでなくてよい）。`as=reference` は Reference Library（画像のみ・`source=generated`・`kind`, `name`, `tags`）、`as=asset` はアセット。ファイルは Project の assets に複製する。`assign` で Character の face/body・Product・Location のスロットへ割り当てられる（種類が合わなければ `validation`） | `result_id`, `project_id`, `as`, `kind`, `name`, `tags`, `assign`（`target`, `id`, `slot`） | `import_id`, `asset_id`, `reference_id`, `assigned` | いいえ |
| `list_generation_upscalers` | 結果を大きくするワークフロー（アップスケール）の一覧（Issue #975）。モデルマネージャーで入れたアップスケール用モデルのものだけが出る（無ければ空配列）。各要素: `id`（`upscale_generation_result` の `model`）, `label`, `mode`（`model` = アップスケール用モデルだけ / `two_pass` = そのあと低い denoise でもう一度サンプリングして仕上げる。元のプロンプトとシードを引き継ぐ）, `native`（モデルの倍率）, `takes_prompt`, `max_output_pixels`, `available`・`unavailable`・`unavailable_message`, `license`（商用利用の可否 `commercial`: yes / conditional / no / unknown・ライセンス名・条件・出典 URL・確認日。確かめられないものは unknown）。ほかに `scales`（`[2, 4]`）と `max_output_pixels`（64 Mpx） | なし | `upscalers[]`, `scales`, `max_output_pixels` | はい |
| `upscale_generation_result` | 成功した画像の結果を 2 倍か 4 倍に大きくする（Issue #975。[../architecture/gentest/gentest.md](../architecture/gentest/gentest.md) §3.4）。結果は元とは別の結果（別のバッチ・別のファイル）として履歴に残り、`get_generation_result` の `upscale`（`source_result_id`・`scale`・`model`・`mode`・`native`・元と出力の大きさ）に元と使った値が入る。ローカルのモデルなので**見積り・確認は無く、すぐ始まる**（無料）。`model` を省くと、入っているもののうちプロンプトの要らない `mode=model` の最初。2 パス（`two_pass`）は元の結果のプロンプト・ネガティブ・シードを引き継ぐ。倍率が 2・4 以外・元が画像として使えない・アップスケール用のモデルが入っていない・名指しのモデルが入っていないか今は使えない・出力が上限（`max_output_pixels`）を超える、は `validation`。元の結果が無ければ `not_found`。16GB の GPU での 4 倍の動作は実機で未確認 | `result_id`, `scale`（2 / 4）, `model` | `job_id`, `state`, `batch_id`, `result_id` | いいえ（ローカルの ComfyUI を動かす） |

結果（`results[]`）: `result_id`, `batch_id`, `position`, `media`, `model`, `model_label`, `api_model`, `prompt`, `sent_prompt`, `translated`, `translated_prompt`, `translated_negative_prompt`, `negative_prompt`, `negative_mode`, `aspect_ratio`, `resolution`, `duration_seconds`, `seed`, `seed_sent`, `status`（queued / running / succeeded / failed / cancelled）, `error`, `has_file`, `mime_type`, `size_bytes`, `cost_basis`, `estimated_cost_usd`, `actual_cost_usd`（ベンダーが返したときだけ）, `favorite`, `used_inputs`, `avoid_reference_pose`, `lora`（`name`, `strength`。使ったときだけ）, `created_at`, `finished_at`。

### アップロードの用途とダウンロードの種類

| 種類 | 名前 | 内容 |
|------|------|------|
| アップロードの用途 | `gentest_reference` | 入力画像（画像モデルの参照画像・動画モデルの最初／最後のフレーム・自由な参照）。`create_upload_ticket` で URL を取り、multipart で 1 回送る。上限 20 MiB、PNG・JPEG・WebP（中身を検査する）。結果 `{"reference_id","mime_type"}` の `reference_id` を `references[]` に `{source: "upload", id}` として渡す。使わないまま 24 時間たつと、mam の次の起動で消える |
| ダウンロードの種類 | `gentest_file` | 成功した結果のファイル（リソース ID は `result_id`）。`get_generation_result` が URL にする |
| ダウンロードの種類 | `gentest_export` | 結果の ZIP（リソース ID は、URL を発行したときに選ばれた結果の id の並びと番号の開始位置。サーバーだけが作れる文字列で、URL には載らない）。`export_generation_results` が URL にする。ZIP はダウンロードのたびにファイルを 1 つずつディスクから流して作る（メモリに溜めない）。URL を発行したあとに削除された結果は抜け、全部消えていれば 404 |

### ZIP のエクスポート（`export_generation_results`。Issue #932）

- **選ぶ処理は 1 つ**: 画面（`GET /generation-test/export`）と MCP は同じ `service/gentest/export`（`Selection` → `Exporter`）を呼ぶ。条件の意味は両方で同じ。
- **認可**: URL は他のダウンロードと同じチケット（ランダム値・15 分・発行したトークンが失効すると 401。ローカル限定のリスナー）で、ツール自体は MCP のトークンが要る。チケットの URL は結果 id を含まず、チケットは発行時に選ばれた結果だけを返すので、チケットを持たない者は結果を落とせない。
- **CSV**: UTF-8 BOM つき・RFC 4180（改行・カンマ・引用符を含む文も壊れない）。`=` `+` `-` `@`・タブ・復帰で始まるセル（モデル名・プロンプト・送った文）は先頭に `'` を足して表計算ソフトが数式として実行しないようにする（CSV インジェクション対策。履歴の原文は mam に残る）。
- **ファイル名**: モデル名などは英数字・`.`・`_`・`-` 以外を `-` にし、`..`・`/`・`\` を残さない（パストラバーサルの対策。最大 40 文字）。

## ジョブ（`get_job`）

| 種別（`kind`） | 開始するツール | `target` | `done` / `total` | `result` |
|---------------|---------------|----------|------------------|----------|
| `gentest` | `run_generation_test`・`run_prompt_match` | なし | 終わった結果の数 / 全結果の数 | `batch_id`, `succeeded`, `failed`, `cancelled`, `next` |

`progress`（`get_job` と `get_generation_test` の両方。Issue #966）: バッチが `running` の間、バッチのジョブ（`job_id`）が呼んでいるローカルの ComfyUI の呼び出しが報告した最新のステップ（`node`・`step` / `max_step`・`percent`・`eta_seconds`。意味は [mcp-tools.md](mcp-tools.md)）。同時に複数の呼び出しが動くときは、最後に進んだもの 1 つ。クラウドのモデル・読み込み中・終わったバッチ・mam の再起動後は**フィールドごと省く**。`done` / `total` の件数は変わらない。

`state`: 結果に動いているものがあれば `running`（まだ何も始まっていなければ `queued`）、終わっていて 1 件でも成功なら `succeeded`、全部失敗なら `failed`（`error` はジョブの理由）、取り消された結果があれば `cancelled`。バッチが削除されると `cancelled`。モデルマネージャーの画面の試し生成も同じ種別のジョブなので、`get_job` で読める。

## テスト

`mcptest` と `gentesttest`（組み込みの全モデルに偽のバックエンド・実際の DB・動くジョブキュー。参照画像・LoRA の検査には ComfyUI の偽のエンジンを足したモデルも使う）で、全組み合わせの実行と `get_job`・`get_generation_test`、見積りの確認（`estimate_id` なし・別の値・条件の変更・上限超え・`max_items` 超え）、取り消し、履歴の絞り込み・お気に入り・削除・プロンプト履歴、入力画像のアップロード（中身の検査）と結果のトークン URL、取り込み（参照・アセット・スロット割り当て・LOCK）、参照画像の役割・強さ・LoRA・ポーズを真似ない設定の往復（見積り・実行・履歴）と、枚数超過・役割・LoRA 非対応の警告、範囲外の強さ・動画への指定の拒否、`list_generation_models` の `reference_mode` など・`loras`、Take・コスト台帳・アセットが作られないこと、`get_job` が画面で始めたバッチを読めること、`unavailable`。

## 詳細設定とプロンプトのウェイト（Issue #973・#972）

- **入力**: `estimate_generation_test`・`run_generation_test` に `seed`（0〜4294967295。n 本目は `seed + n − 1`。省くと結果ごとにランダム）と `sampling`（`steps` 1〜150・`cfg` 0〜30・`sampler`・`scheduler`。省いた項目はモデルの既定）を足した。範囲外は `validation`。
- **`list_generation_models`**: 各モデルに `sampling`（そのモデルが受け取る `steps`・`cfg`・`sampler`・`scheduler` と `default`。無い項目は受け取らない。`steps`・`cfg` の `locked: "speed_mode"` は速さのモードが決める＝指定しても無視）と `prompt_weight`（`supported` / `weak` / `unsupported` / `unknown`。`(語:1.3)` が効くか。`unsupported` のモデルは、強調したい語を文で言い換える）を足した。
- **警告**（見積りの `warnings[]`）: `sampling_ignored`（モデルが受け取らない値、または速さのモードがあるモデルの `steps`・`cfg`。**速さのモードが優先**）・`seed_ignored`（シードを渡せないモデル）・`prompt_weight`（プロンプトに `(…)` があり、モデルが読まない `unsupported` か効きが弱い `weak`）。
- **結果**（`get_generation_result`・`get_generation_test` の `results[]`）: `sampling`（実際に使った `steps`・`cfg`・`sampler`・`scheduler`）と `seed` が残るので、同じ条件で作り直せる。英訳でウェイト（重みの数字・括弧）が消えたときは `lost_weights[]`。
- 英訳は `(token:weight)`・`(word)`・`[word]` をそのまま残す指示で行う（[../architecture/gentest/gentest.md](../architecture/gentest/gentest.md) §3.1）。

## 品質とやりたいこと（Issue #977）

- **入力**（画像だけ）: `estimate_generation_test`・`run_generation_test` に `quality`（`fast` 速い／`standard` 標準／`high` きれい。省くと標準）と `intent`（`photo`・`anime`・`landscape`・`face`）を足した。**既存の引数は何も変わらない**（`quality`・`intent` を省いたリクエストは今までと同じに動く。`models` は `intent` を使うときだけ省ける）。
- **`quality`**: モデルの `sampling.quality_presets`（`list_generation_models`）の steps・cfg で作る。`sampling` に `steps`・`cfg` を書いた項目はそちらが優先。標準は何も上書きしない（モデルの既定のまま）。プリセットの無いモデル（クラウド・蒸留・速さのモード）は警告 `quality_ignored` で標準の値で作る。範囲外の名前は `validation`。
- **`intent`**: 導入済みで呼べて、商用フィルター・成人ゲート・この PC の OS／GPU の条件に合うモデルを、mam が選ぶ（`models` と同時には指定できない。`validation`）。「同じ顔で」（`face`）は `references`（顔の写真）が要り、役割 `reference` のものは `face` として使う。合うモデルが無いときは、理由（導入されていない・条件に合わない）つきの `validation`。見積りの `request.models` に、選ばれたモデルが入る。
- **`list_generation_models`**: `intents[]`（`id`・`label`・`hint`・`needs_references`・いま選ばれる `model`・`model_label`・`note`・選べない `reason`）と、各モデルの `sampling.quality_presets`（`fast`・`high` の `steps`・`cfg`）を足した。
- 画面の「かんたん」と同じ `gentest.Service.Recommend`・`ApplyIntent`（[../architecture/gentest/gentest.md](../architecture/gentest/gentest.md) §3.6）を呼ぶ。テストは `mcp/tools/gentest/settingstest/quality_test.go`。
- 結果の「もう1枚」「もう一度」「この顔で作る」（画面の再現）に当たる MCP の操作は、既存の `get_generation_result`（使った `sampling`・`seed`）と `run_generation_test`（`seed`・`sampling`）の組み合わせで足りる（レシピは #977 のステージ 2）。

## 大きくする（アップスケール。Issue #975）

- 画面の「大きくする」と同じ `gentest.Service.Upscale` を呼ぶ（`mcp/tools/gentest/upscale.go`）。実行は種別 `gentest` のジョブ（`get_job`・`get_generation_test`）で、進み具合は `progress` に出る。
- 結果は `result_id`（`get_generation_result` で読む）。`upscale` を持つ結果は大きくした結果で、元は `source_result_id`。大きくした結果をもう一度大きくしてもよい。
- `list_generation_models` には**アップスケールのワークフローは出ない**（プロンプトを渡すモデルではない）。一覧は `list_generation_upscalers`。
- ジョブの `result` は `batch_id`・`succeeded`・`failed`・`cancelled`（既存と同じ）。取り消しは `cancel_generation_test`。

## レシピと「画面に出す値」（Issue #977 ステージ 2）

画面の「レシピ」（[../components/gentest-tiers.md](../components/gentest-tiers.md) §9）と同じものを、4 つのツールで扱う。実装は `backend/internal/mcp/tools/gentestrecipe`（`tools/gentest` は行数の上限まで来ているため別パッケージ）。サービスは `service/gentest/recipe`。アプリが生成テストを持たないとき（`Gentest` が nil）は `unavailable`。

| ツール | 内容 | 入力 | 出力 |
|---|---|---|---|
| `list_generation_recipes` | レシピの一覧（名前順） | なし | `recipes[]`: `id`・`name`・`model`・`model_label`・`easy`（かんたんの選択肢に出ているか）・`usable`・`reason`（使えない理由）・保存した設定（`sampling`・`quality`・`seed`・`negative_prompt`・`lora`・`reference_strength`・`aspect_ratio`・`exposed`）・`created_at`・`updated_at` |
| `save_generation_recipe` | 設定をレシピとして保存する（モデル 1 つ。プロンプトの本文と入力画像は保存しない） | `name`（1〜60 文字の 1 行。大文字小文字を区別せず一意: 重複は `conflict`）・`model`・`easy?`・上の設定の各項目 | 保存したレシピ |
| `update_generation_recipe` | 名前を変える・かんたんの選択肢に足す／外す（冪等） | `id`・`name?`・`easy?`（どちらも無ければ `validation`） | 変えたレシピ |
| `delete_generation_recipe` | レシピを削除する（破壊的）。生成の結果は残る | `id` | `deleted` |

- **`recipe`**（`estimate_generation_test`・`run_generation_test` の引数。画像だけ）: レシピのモデルと設定で作る。**リクエストが名指しした項目（`sampling`・`seed`・`quality`・`aspect_ratio`・`lora`・`exposed`・プロンプトごとの `negative_prompt`・入力画像の `strength`）が勝つ**。`models`・`intent` とは同時に指定できない（`validation`）。レシピが無い／消えたものは `not_found`。使えないレシピ（モデルの削除・未導入・成人ゲート・「商用OKのモデルだけを使う」・OS／GPU）は、理由つきの `validation`（レシピは消えない）。見積りの `request.models` にレシピのモデルが入る。
- **`exposed`**（同。画像だけ）: モデルの「画面に出す値」（`list_generation_models` の各モデルの `exposed[]`: `key`＝`<ノード ID>.<入力名>`・`label`・`type`（int / float / string / enum）・`min`・`max`・`options`・`default`＝ワークフローのグラフが持つ値）の値を `{"<key>": 値}` で渡す。型・範囲・選択肢の外、どのモデルにも無いキーは `validation`。そのキーを持たないモデルには警告 `exposed_ignored` を出して使わない。結果の `exposed`（`get_generation_result` ほか）に使った値が残り、そのまま次のリクエストの `exposed` に渡せば同じ条件で作り直せる。
- 「画面に出す値」を決める側（ワークフローの `exposed_inputs`）は [mcp-tools-workflows.md](mcp-tools-workflows.md)・[mcp-tools-local-models.md](mcp-tools-local-models.md)。
- テストは `mcp/tools/gentestrecipe/recipes_test.go`（保存 → 一覧 → レシピ指定の見積り・実行で同じ設定になる → 名前の変更・外す → 削除）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-26 | 新規作成。12 ツール、ジョブの種別 `gentest` の Probe（#833 から移動）、アップロードの用途 `gentest_reference`、ダウンロードの種類 `gentest_file` | Issue #835（#830 の方針: 画面の全操作を MCP で操作できるようにする） |
| 1.1 | 2026-11-01 | `estimate_generation_test`・`run_generation_test` に `translate_to_english`（省略時 true）を足し、方針に英訳（単価なしの `translate` の行・訳せなければ `upstream`）、結果に `translated`・`translated_prompt`・`translated_negative_prompt`、拒否に `upstream` を足した | Issue #913 |
| 1.2 | 2026-11-02 | 画像の `references[]` に `role`（face / style / edit を追加）と `strength`、リクエストに `avoid_reference_pose` と `lora`、`list_generation_models` に `reference_mode`・`max_reference_images`・`reference_roles`・`supports_lora`・`loras`、結果に `avoid_reference_pose`・`lora`、警告 `role_unsupported`・`lora_unsupported` を足した | Issue #933 |
| 1.3 | 2026-11-02 | 参照の `strength`・`avoid_reference_pose` が効かないモデルでは警告なしに無視されること、LoRA の `name` の検査（1〜200 文字・制御文字なし）、強さ 0 の送り方、動画では `loras` が空（エンジンに問い合わせない）ことを明記した | Issue #933 のレビュー |
| 1.4 | 2026-11-02 | プロンプトマッチの 4 ツール（`estimate_prompt_match`・`run_prompt_match`・`get_prompt_match`・`instruct_prompt_match`）を足した。見積り → 確認 → 実行の流れは `run_generation_test` と同じ。実行は種別 `gentest` のジョブで、`get_job` と `get_generation_test` は、最後の測定が終わるまで `running`。取り消しは `cancel_generation_test`、各回の画像は `get_generation_result` | Issue #916 |
| 1.5 | 2026-11-02 | `export_generation_results` とダウンロードの種類 `gentest_export` を足した（結果を `photos/` と `prompts/prompts.csv` の ZIP にまとめる。直近 N 件・バッチ・指定・お気に入り・モデル・種類・商用利用で選び、大きいときは分割）。「ZIP は無い」を改めた | Issue #932 |
| 1.6 | 2026-11-02 | `estimate_generation_test`・`run_generation_test` に動画の `speed_mode`（Wan 2.2 14B + LightX2V の `fast` = 4 ステップ / `standard` = 通常）、`list_generation_models` のモデルの `video.speed_modes`（`id`・`label`・`default`）、警告 `speed_mode_unsupported`、結果の `speed_mode` を足した | Issue #930 |
| 1.7 | 2026-11-02 | `get_job`（種別 `gentest`）と `get_generation_test` に、動いているローカル ComfyUI の生成の `progress`（ステップ・%・残りの見積り。取れないときは省く）を足した | Issue #966 |
| 1.8 | 2026-11-02 | `estimate_prompt_match` の見積りに LLM の呼び出し 2 行（`match_llm`・`match_llm_compare`）を足し、単価の確かめられたモデルは合計に入れ、確かめられない行だけ `no_price` にした。`get_prompt_match` の回に `scoring`（顔を混ぜているときの点の内訳）を足した | Issue #964・#965 |
| 1.9 | 2026-11-03 | `estimate_generation_test`・`run_generation_test` に `seed`・`sampling`、`list_generation_models` に `sampling`・`prompt_weight`、結果に `sampling`・`lost_weights`、警告 `sampling_ignored`・`seed_ignored`・`prompt_weight` を足した | Issue #973・#972 |
| 1.10 | 2026-11-03 | `list_generation_upscalers`・`upscale_generation_result`（結果 ID＋倍率 2 / 4 で大きくする。別の結果として残り、結果の `upscale` に元と使った値）を足した。`list_generation_models` にはアップスケールのワークフローは出さない | Issue #975 |
| 1.11 | 2026-11-03 | `references[]` の `role` に `pose`、`source` に `pose_library` を足した（`list_generation_models` の `reference_roles` にも `pose`） | Issue #986 |
| 1.12 | 2026-11-02 | `estimate_generation_test`・`run_generation_test` に `mask`・`denoise`、`list_generation_models` に `supports_inpaint`・`denoise_default`、結果に `inpaint`、警告 `denoise_ignored` を足した。対応しないモデルは `validation` | Issue #974 |
| 1.13 | 2026-11-03 | `estimate_generation_test`・`run_generation_test` に `quality`（fast / standard / high）と `intent`（photo / anime / landscape / face）、`list_generation_models` に `intents[]` とモデルの `sampling.quality_presets`、警告 `quality_ignored` を足した。既存の引数は変えない | Issue #977（ステージ 1） |
| 1.14 | 2026-11-04 | `list_generation_recipes`・`save_generation_recipe`・`update_generation_recipe`・`delete_generation_recipe`、`estimate_generation_test`・`run_generation_test` に `recipe` と `exposed`、`list_generation_models` の各モデルに `exposed[]`、結果に `exposed`、警告 `exposed_ignored` を足した | Issue #977（ステージ 2） |
| 1.15 | 2026-11-03 | `references[].source` に `batch_input`（前のバッチの入力画像。`id` は `get_generation_test_result` などの結果の `inputs[].input_id`）を足した。アップロードした画像を使った結果を、同じ入力で作り直せる | Issue #977 |
