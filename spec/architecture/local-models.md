# ローカル・代替の画像／動画モデル（ComfyUI / OpenAI 互換 API）

[model-adapters.md](model-adapters.md) の Model Adapter（§3 画像・§4 動画）に、利用者が登録した接続先（ComfyUI サーバー、OpenAI 互換の画像 API サーバー）のモデルを足す。クラウドの組み込みモデルと同じ経路（用途・Quality Policy の割り当て、フェイルオーバー、ブレーカー、同時実行数、Cost Ledger）に載り、違いは接続先と識別子だけである。Issue #831。ワークフローの編集画面（ComfyUI のノード UI）は含まない（#834）。

MAM が ComfyUI を自分で取得・起動し、モデルを入れる経路は [model-manager.md](model-manager/model-manager.md)（自動作成される接続先 `bundled-comfyui` はこの文書の通常の ComfyUI 接続先として扱う）。

## 1. 接続先の種類

| 種類（`kind`） | 作れるモデル | 呼び出し |
|---|---|---|
| `comfyui` | 画像・動画 | ワークフロー（API 形式の JSON）を `POST /prompt` で実行する（§4） |
| `openai-compat` | **画像のみ** | `POST /v1/images/generations`、参照画像があるときは `POST /v1/images/edits`。応答は `b64_json` と `url` の両方を読む。API キーは任意 |

- **OpenAI 互換の動画は対象外**: 公式の Videos API は 2026-09-24 に終了し、互換サーバーごとに仕様が分かれているため。ローカルの動画モデルは ComfyUI で動かす。
- 接続先は `base_url` を持つ。**http・`localhost`・LAN のプライベートアドレス・tailnet（`100.64.0.0/10`、MagicDNS 名）を許す**。リンクローカル（`169.254.0.0/16`、`fe80::/10`）は常に拒否する（§6）。

## 2. 識別子

`take.model_name` と `cost_ledger_entry.provider` には `<kind>:<接続先名>/<モデル名>` を保存する。

```
comfyui:gpu-box/wan22-i2v
openai-compat:lmstudio/flux-schnell
```

- 接続先名・モデル名は小文字英数字と `.` `_` `-`（slug）。**作成後は変えられない**（Take と Cost Ledger の識別子が指すため）。名前の変更や `kind` の変更は 422（削除してから作り直す）。
- `domain.ModelName` と `domain.Vendor` は開いた集合になる。組み込みの識別子（§6）は定数のまま、登録済みのモデルは `domain/modelreg` が持つ一覧を `domain.ImageModels()` / `VideoModels()` が足す。コマンドインタープリタのシステムプロンプトは呼び出しごとに作る（登録は動作中に変わる）。
- 動画の能力と生成プロンプトの変換（`modeladapter.Capabilities`・`Prepare`）は名前だけで引くので、登録済みのモデルは `modeladapter.PublishLocal`（`localbackends.Publish` が `modelreg` と一緒に呼ぶ）で公開する。
- ブレーカー・同時実行数のベンダー名は接続先ごとに `endpoint:<接続先名>`。同じ接続先のモデルは 1 つのブレーカーと上限を共有する。
- 見積コストが 0 USD でも Cost Ledger の行は必ず書く（`actual_cost` も 0）。

## 3. `models.json` のスキーマ

`model-adapters.md` §9.2 のファイルに、2 つのキーを足す（未知のキーはエラーのまま）。

```json
{
  "endpoints": {
    "gpu-box": {"kind": "comfyui", "base_url": "http://100.101.102.103:8188", "max_concurrency": 1, "timeout_seconds": 10800},
    "lmstudio": {"kind": "openai-compat", "base_url": "http://127.0.0.1:1234"}
  },
  "local_models": {
    "gpu-box/wan22-i2v": {
      "media": "video",
      "workflow": {"graph": {"...": "API 形式"}, "assignments": [{"param": "prompt", "node": "6", "input": "text"}], "output_node": "58", "fps": 16, "frames_step": 4, "size_multiple": 16},
      "video": {"durations": [3, 4, 5], "resolutions": ["480p", "720p"], "native_audio": false},
      "cost_per_second_usd": 0
    },
    "lmstudio/flux-schnell": {"media": "image", "model": "flux-schnell", "accepts_image_inputs": true, "cost_per_image_usd": 0}
  }
}
```

| 項目 | 内容 | 既定・検査 |
|---|---|---|
| `endpoints.<名>` | `kind`、`base_url`、`max_concurrency`、`timeout_seconds` | 同時実行 1（GPU は通常 1 ジョブ）。タイムアウトは ComfyUI 10800 秒（3 時間）・互換 900 秒（上限 24 時間）。設定画面・`create_local_endpoint` / `update_local_endpoint` で接続先ごとに変えられる。**ComfyUI では、ジョブが進まなくなってから打ち切るまでの時間**（実行の全体の長さではない。14B の動画モデルは 16GB のカードで 1 時間を超えるが、進んでいる間は延びる。下の「タイムアウト」）で、互換は 1 回の呼び出し全体。越えた呼び出しは打ち切り、ComfyUI のそのジョブも止める（下の「取り消し」）。`base_url` は http(s) のホストで、資格情報・クエリ・フラグメントを含まず、リンクローカル・未指定・マルチキャストの IP でない |
| `local_models.<接続先>/<名>` | `media`（image/video）。接続先が存在すること。`comfyui` は `workflow`、`openai-compat` は `model`（サーバーに渡すモデル ID）が必要 | `openai-compat` の動画は拒否 |
| `workflow` | `graph`（API 形式）、`assignments`、`output_node`、`fps`（既定 16）、`frames_step`（正なら総フレーム数を `n×step+1` にする。Wan は 4、LTX は 8）、`size_multiple`（既定 16）、画像の参照用に `reference_mode`・`no_reference`・`lora_model_node`（§4.2）、組み込みテンプレートが必要とする追加ノードの `requires_nodes`（許可リストの id。[model-manager/custom-nodes.md](model-manager/custom-nodes.md) §6。同梱エンジンのモデルは、そのノードが入るまで導入も選択もできない） | 割り当てが指すノード・入力が `graph` に無い、`output_node` が無い、プロンプトの割り当てが無い、は 422 |
| `video` | `durations`（秒、昇順）、`resolutions`（短辺で `480p`・`720p`・`1080p` など）、`native_audio` | 動画モデルは必須。最初と最後のフレーム・参照画像の枚数は `assignments` から決まる |
| 価格 | `cost_per_image_usd`、`cost_per_second_usd` | 既定 0（参考値。見積コストにのみ使う） |
| 用途・Policy | `images.<purpose>`・`policies.<policy>` の `model` に識別子を書く（既存と同じ形式） | 参照先が `local_models` に無ければ起動時エラー。動画の対象は価格を要求しない |

`--model-config` で固定したファイルは読み取り専用（接続先・モデルの保存は `ErrPinned`）。API キーだけは保存できる。

## 4. ComfyUI

- **割り当て表（`assignments`）**: `param` の値をノード `node` の入力 `input` に入れる。使える `param`: `prompt`、`negative_prompt`、`seed`（実行ごとに乱数。生成テストと `run_workflow` は固定もできる）、`steps`・`cfg`・`sampler`・`scheduler`（§4.3）、`width`、`height`、`frames`、`fps`、`reference_image`（`index` で何枚目か。`optional: true` なら、その枚数が無い実行ではノードごと外れる）、`reference_denoise`（画像のみ。§4.2）、`first_frame`、`last_frame`。画像は `frames`・`fps`・`first_frame`・`last_frame` を使えない。幅・高さは縦横比と解像度から決め、`size_multiple` の倍数に丸める。
- **入力画像**: 参照画像・最初／最後のフレームは `POST /upload/image` に**元の拡張子のまま**アップロードし、返った名前を入力に入れる。
- **実行**: `/ws?clientId=…` を開いてから `POST /prompt`（`prompt_id` と `client_id` は mam が決める）。`/ws` の `progress`・`executing` から割合を求め、`/history/<id>` を確認して完了を待つ。結果は `GET /view` で取る（動画は `.mp4`・`.m4v`・`.webm`・`.mov` のみ。それ以外は投入後のエラー）。
- **進捗**: 割合は Shot の進捗（`/ws/shots/:id`、チャットの進捗カード、Take カード）のメッセージに「動画を生成しています（42%）」の形で入る。モデル呼び出しの文脈に進捗の関数を持たせ（`modeladapter/progress`）、`generation/relay` が Orchestrator の通知（Take 数など）のメッセージに割合を足して配信する（`Progress` の型は変えない）。
- **取り消し**: Take の生成を止めたときは、**自分の `prompt_id` だけ**を止める。まず `POST /api/jobs/<id>/cancel`、失敗（ルートが無い 404、画面の静的ファイルが先に答える 405 など）なら `GET /queue` で見て、実行中なら `POST /interrupt {"prompt_id"}`、待機中なら `POST /queue {"delete":[id]}`。対象を指さない割り込み・キューの全消しはしない。呼び出しがタイムアウトで打ち切られたときも同じ手順で ComfyUI 側のジョブを止め、後ろで待つジョブが始まる。接続テストは ComfyUI が 0.3.57 未満だと「取り消しが他のジョブを止めうる」と注意を出す。
- **タイムアウト（進み具合）**: ComfyUI の接続先の `timeout_seconds` は、ジョブが**進まなくなってから**打ち切るまでの時間（`localcfg.Endpoint.StallLimit`。既定 10800 秒）。進んでいる間は延びる（#961。#917 は既定を 1 時間から 3 時間にしただけで、実行が長いだけの正常なジョブを打ち切りうる点は残っていた）。`Model Adapter` が呼び出しに掛ける `context.WithTimeout` は、ComfyUI の接続先では `localcfg.Endpoint.CallLimit` = `MaxTimeoutSeconds`（24 時間）にし、進み具合の判定はバックエンド（`comfyui/stall.go`）が自分のジョブについて行う（互換の接続先は従来どおり `timeout_seconds` が呼び出し全体の上限）。ワークフロー画面の実行（`service/workflows`）も同じ上限と判定を使う。
  - **進みの印**: WebSocket で届く、そのジョブ自身（`prompt_id` が自分）のメッセージ（`execution_cached`・`executing`・`progress` ほか）を印とし、最後の印からの経過を `Client.Stall` と比べる。時計は `Client.Now`（テストで差し替える）。他のクライアントのジョブのメッセージは数えない。
  - **キュー待ち**: 他のクライアントのジョブの後ろで待つ間は印が出ない。そこで、上限に達したときに `GET /queue` を 1 回見て、自分のジョブが `queue_pending` にあれば打ち切らず、印を今に更新する（待っている間は何度でも延びる）。健全なジョブは追加のリクエストを出さない。キュー待ちの長さは全体の上限（24 時間）だけが縛る（待ち行列を作るのは利用者が ComfyUI を直接使うときだけで、mam 自身のジョブは接続先の同時実行数で mam 側が順番に出すため）。`queue_running` にある、または見つからないのに印が無いジョブは打ち切る。
  - **ソケットが無いとき**: WebSocket が無い・途切れたサーバーでは印が届かない。その場合は最後の印（無ければ投入の時刻）から数えるので、従来と同じ「全体でこの時間」の打ち切りになる（進んでいるかどうかを知る手段が無いため、延ばさない。保守的な側）。
  - **打ち切り**: ジョブが止まったときは `localcfg.TimeoutError{Stalled: true, After: <上限>}`（`modeladapter.ErrTimeout` でも `context.DeadlineExceeded` でもある。送信後の失敗の印つき）を返し、ジョブは上の「取り消し」で止める。全体の上限（24 時間）に達したときは従来の `TimeoutError`（`After` は 24 時間）。文言は `gentest/failtext`（止まった場合は「N のあいだ進まなかった」、全体の上限は「全体の上限」）。
  - **決定: 全体の上限を残す**。進み続けて終わらないジョブ（進捗だけ出し続けるワークフローの暴走）や、いつまでも空かないキューで、mam の同時実行の枠を握り続けないための最後の歯止めとして、`MaxTimeoutSeconds`（24 時間。設定値の上限と同じ）を残す。設定項目にはしない（`timeout_seconds` の上限と一致するので、進みの止まった判定の方が必ず先に効く）。
  - **決定: 画像と動画で上限を分けない**。進み具合で打ち切る形では、動画の長い実行（進んでいる間は延びる）と画像の取りこぼし（進みが止まれば同じ時間で切れる）を、1 つの「進まない時間」で両方まかなえるため、メディアごとの設定は要らない。設定項目を増やすと画面・MCP・設定ファイルのすべてに入力と検証が増える割に得るものが無い。動画で読み込み・デコードなど進みの出ない処理が長いモデルは、接続先の `timeout_seconds` を延ばして足りる。
  - **既定を縮めない**: 既定は 10800 秒のまま。進みを報せないカスタムノードのワークフローや、進みの出ない長い処理（モデルの読み込み・VAE のデコード）では印が出ず、既定を縮めるとそれらを従来より早く打ち切ってしまうため。進みを報せる ComfyUI では、利用者が `timeout_seconds` を下げれば固まったジョブを早く見つけられる。
- **組み込みテンプレート**: `localcfg/templates/image/*.json`・`localcfg/templates/video/*.json`（コアノードだけ。**`requires_nodes` を宣言したテンプレートだけが、許可リストの追加ノードを使える**。それ以外のクラスが入るとテストが落ちる）。`txt2img-sdxl`、`img2img-sdxl`、`txt2img-sdxl-clipskip2`・`img2img-sdxl-clipskip2`（SDXL に `CLIPSetLastLayer` で clip skip 2 を足したもの。Pony 系のモデル用。#984）、`txt2video-wan22-5b`、`img2video-wan22-5b`、LTX-2.5 の `txt2video-ltx-2-5`・`img2video-ltx-2-5`（§4.1）ほか（一覧は model-manager.md §4.2）。モデルファイル名（チェックポイント）は接続先にあるものへ直す。**GPU では実行していない**（開発環境に GPU が無い）。構造だけをテストする（割り当てが指すノード・入力と出力ノードの存在）。実機での検証は #833 で対象モデルを決めるときに行う。
- **取り込み**: 利用者の「Save (API Format)」の JSON を取り込める。エディタ形式（`nodes`/`links`）は拒否してヒントを返す。ノードと入力の一覧を返すので、割り当てを利用者が決める。

### 4.3 サンプリングの割り当て（steps・cfg・sampler・scheduler。Issue #973）

- **`param`**: `steps`（ステップ数）・`cfg`・`sampler`（KSampler の `sampler_name`）・`scheduler` を、画像・動画のどちらでも割り当てられる（`localcfg.ParamSteps` ほか）。テンプレートは、KSampler・KSamplerAdvanced・SamplerCustom（`cfg`）・SamplerCustomAdvanced 系（`BasicScheduler`・`BetaSamplingScheduler`・`Flux2Scheduler`・`LTXVScheduler` の `steps`、`CFGGuider` の `cfg`、`KSamplerSelect` の `sampler_name`）の**入力そのもの**に割り当てる。割り当てが無い値は、そのモデルでは設定できない（画面は欄を出さず、リクエストの値は警告つきで使わない）。LTX-2.5 は蒸留版で `ManualSigmas` と固定の `cfg 1` だけなので、割り当てを持たない。
- **既定は今のテンプレートの値**: グラフの値が既定で、リクエストが名前を出さない値は**グラフに書き込まない**。上書きしなければ、送るグラフは今と一字も変わらない（テスト `comfyui/samplingtest` が、組み込みテンプレートの全部でこれを確かめる）。
- **`ratio`（`steps` の割り当てだけ。0〜1 の間）**: ステップ数そのものではなく `round(steps × ratio)` を入れる。2 段のサンプラー（Wan 2.2 14B の高ノイズ・低ノイズ。`KSamplerAdvanced` の `end_at_step`・`start_at_step`）の切り替えのステップが、ステップ数を変えても同じ割合の場所に残る（Wan 2.2 14B は 0.5、Remix は 20 ステップ中の 10 ではなく 12 ステップ中の 10 なので 0.8333）。結果は 1 以上で、後の段が動く余地を残す。保存済みの割り当てを送り返す更新（`update_local_model`・設定画面）は `ratio` を引き継ぐ。
- **速さのモードが優先（決定）**: `speed_modes` を持つワークフロー（Wan 2.2 14B + LightX2V など）は、蒸留 LoRA がそのステップ数と cfg でしか効かないので、`steps`・`cfg` を**速さのモードが決める**。リクエストの `steps`・`cfg` は、画面では欄を無効にして理由を出し、サーバー（`localcfg/sampling.Caps` の `Applicable`）でも**無視する**（警告 `sampling_ignored`）。`sampler`・`scheduler` は上書きできる。
- **`sampling.Caps`（`localcfg/sampling`）**: ワークフローが受け取る値と既定（`Workflow.SamplingCaps()`）。`modelcatalog.Info.Sampling`・MCP の `list_generation_models` が返す。範囲は steps 1〜150・cfg 0〜30（`MinSteps` ほか。画面のスライダーと同じ）。サンプラー・スケジューラー名は ComfyUI の `comfy/samplers.py` の一覧（`sampling.Samplers`・`Schedulers`）。
- **検証**: 保存時（`validateSampling`）、`ratio` は `steps` だけで 0〜1 の間、`steps`・`cfg` は数値の入力、`sampler`・`scheduler` は文字の入力に割り当てる。

### 4.4 プロンプトのウェイト（`prompt_weight`。Issue #972）

- **値**: ワークフローの `prompt_weight` = `supported`（効く）・`weak`（適用されるが効きが弱いかもしれない）・`unsupported`（効かない）・`unknown`（確かめていない。省略も同じ）。`modelcatalog.Info.PromptWeight`・`list_generation_models` が返し、画面の「?」と警告 `prompt_weight` の源になる。組み込みテンプレートの全部に値を入れてある。
- **根拠（ComfyUI `comfy/sd1_clip.py`・`comfy/text_encoders/*.py` を読んだ事実）**: ComfyUI は `(語:1.3)`・`(語)`（1.1 倍。入れ子は 1.1 ずつ）を、テキストエンコーダーのトークナイザーが `disable_weights` を立てていないときだけ読む（`SDTokenizer.tokenize_with_weights` が `token_weights` で切り出し、その語の埋め込みを重みで拡大する）。AUTOMATIC1111 の `[語]` は**読まない**（ただの文字）。`qwen_image`（Qwen-Image・Qwen-Image 2.1）・FLUX.2 klein（`Qwen3`/Mistral のテンプレート）・Hunyuan Video 1.5・LTX-2.5（Gemma）のトークナイザーは `disable_weights=True` なので、かっこと数字がそのまま言語モデルに文として渡る → `unsupported`。SDXL・SD 3.5 は CLIP が読む → `supported`。
- **断定しないもの**: Flux.1（T5-XXL + CLIP-L。ComfyUI は重みを読むが、文を読むのは T5 で、効きは確かめていない）・Chroma（T5）・Wan 2.2（UMT5）・LTX-Video 0.9.5（T5）は `weak`。GPU での効き方は未確認（実機の確認が要る）。ユーザー自作のワークフローと外部のモデルは `unknown`（警告は出さない。当て推量の警告は雑音になる）。
- **効かないモデルへの案内**: 画面の「?」と、見積りの警告 `prompt_weight`（プロンプトに `(…)` があるときだけ）が、「強調したい語を文で言い換える（特に・とても・大きく、先頭に置く、具体的に書く）」を出す。

### 4.5 画像を大きくするワークフロー（`upscale`。Issue #975）

- **印**: ワークフローの `upscale`（`{label, native, mode, max_output_pixels}`。`localcfg/upscale.Spec`。印の型・倍率の計算（`Ratio`）・検証は `localcfg` の下位パッケージ `upscale` に置いた。`promptweight` と同じ分け方）は、そのワークフローが**画像を作るのではなく、渡した画像を大きくする**ことを表す。`native` はアップスケール用モデルの倍率（4 倍のモデルなら 4）、`mode` は `model`（`UpscaleModelLoader` → `ImageUpscaleWithModel` → `ImageScaleBy`）か `two_pass`（そのあと `VAEEncodeTiled` → 低い denoise の `KSampler` → `VAEDecodeTiled`）。画像のワークフロー（`media: image`）の印として持つので、接続先・保存・版の仕組みはそのまま使う。
- **割り当て**: `reference_image`（大きくする画像。ちょうど 1 つ）と `upscale_ratio`（`ImageScaleBy` の `scale_by` に、依頼の倍率 ÷ `native` を入れる。ちょうど 1 つ。`two_pass` は加えて `prompt`・`negative_prompt`・`seed`・`steps`・`cfg`・`sampler`・`scheduler`）。印の無いワークフローに `upscale_ratio` は割り当てられない。印のあるワークフローはプロンプトの割り当てが要らない（`model` 方式）。グラフに `ImageUpscaleWithModel` が要る。`localcfg.validateUpscale` → `upscale.Validate`。
- **一覧**: 印のあるワークフローは、プロンプトを渡すモデルではないので、`localcfg.IDs`（画像・動画のモデルの識別子）・生成テストのモデルの一覧・モデルマネージャーの試し生成には**出ない**。生成テストの `modelcatalog.Catalog.Upscalers` が専用の一覧を返し、「大きくする」（[gentest/gentest.md](gentest/gentest.md) §3.4）が使う。
- **コアのノードだけ**: `ImageScaleBy`・`ImageUpscaleWithModel`・`UpscaleModelLoader`・`VAEEncodeTiled` をテストの `coreNodes`（テンプレートが使ってよいクラスの一覧）に足した。同梱エンジンは追加ノードなしで動く。

### 4.2 画像モデルの参照画像・強さ・役割・LoRA（#933）
画面と MCP は、どのモデルでも同じ「強さ」（0〜1、既定 0.7）と「役割」を渡す。ワークフローの作りの違いは `localcfg`・`comfyui` が吸収する。`reference_mode` が方式を決める（`localcfg.Model.ReferenceCaps()` が方式・上限枚数・選べる役割を返し、画面と `accepts_references`・`max_reference_images` の元になる）。

| `reference_mode` | モデル | 枚数 | 役割 | 強さ s の効き方 |
|---|---|---|---|---|
| `img2img` | `chroma1-hd`（`txt2img-chroma`）、`flux1-dev-nsfw-master`（`txt2img-flux-dev-lora`） | 1 | 全体のみ | 参照を VAE で潜在にして途中から生成する。`reference_denoise` の割り当てに `1 − (1 − floor)×s` を入れる（`floor` は `reference_denoise_min`。既定 0、`chroma1-hd` は 0.6。Chroma の schedule は denoise 0.6 未満では絵がほとんど変わらず 0.95 で参照が消えるため、強さの範囲を変化のある区間に広げる。下限は 0.02）。参照が無い実行は `no_reference`（空の潜在に戻す）を当てて参照のノードを外し、テキストから生成と同じになる |
| `edit` | `noctq-v4-qwen-image21`（`txt2img-qwen-image21`） | 最大 10（`reference_image` の `optional` スロット） | 全体・顔だけ・画風だけ・編集対象 | 参照は `TextEncodeQwenImage21` の `image_1`〜に入る（プロンプトの `<image1>` が 1 枚目）。強さ s の参照は、サンプリングの最初の `round(s×steps)` ステップ（最低 1）だけ条件に入り、残りはプロンプトだけで仕上げる。強さが違う参照ごとにサンプリングを区切り（最大 6 区間。区切りが 5 つを超えるほど強さが散らばるときは、均等に選んだ 5 つを残し、ほかの参照の区切りは一番近い残した区切りへ寄せる。終わりまで残る参照にはならない）、各区間を別のエンコーダー・`SamplerCustomAdvanced` で回す。役割は文で伝え（顔だけ: 「顔と同一性だけを保つ」、画風だけ: 「画風・色・雰囲気だけ」）、顔・画風は VAE を渡さない（ビジョンだけで読む）。VAE はデコードのノード（`VAEDecode`）がつなぐ `VAELoader` |
| `edit`（FLUX.2） | `flux2-klein-4b`（`txt2img-flux2-klein`。#931） | 最大 4（`reference_image` の `optional` スロット） | 全体・顔だけ・画風だけ・編集対象 | 参照は視覚モデルではなく潜在として入る: 画像ごとに `ImageScaleToTotalPixels`（約 1 メガピクセル）→ `VAEEncode` → `ReferenceLatent` を、肯定・否定の条件に順に重ねる（ComfyUI の公式の編集テンプレートと同じ。テンプレートは 4 枚分のスロットを持ち、使わない枚数は画像・縮小・VAEEncode・`ReferenceLatent` ごと外れ、`ReferenceLatent` は素通しに戻る）。強さの意味は Qwen の編集と同じ（最初の `round(s×steps)` ステップだけ参照が入る。蒸留版は 4 ステップなので 0.25 刻み。最大 3 つの区切りで、区間ごとに別のエンコーダー・否定の起点・`ReferenceLatent` の鎖・`CFGGuider`・`SamplerCustomAdvanced` で回す）。プロンプトの `<image1>` は klein の書き方の `image 1` に直す。顔・画風の役割は文でだけ伝える（視覚モデルがないので VAE の有無の違いはない）。出力の大きさは縦横比の指定に従い、1 枚目の参照の大きさにはしない |
| `edit`（Qwen-Image-Edit） | `qwen-image-edit-2511`（`txt2img-qwen-image-edit-2511`。#971） | 最大 3（`reference_image` の `optional` スロット） | 全体・顔だけ・画風だけ・編集対象 | 参照は `TextEncodeQwenImageEditPlus` の `image1`〜`image3` に入る（`TextEncodeQwenImage21` と違い入力名が `imageN`。プロンプトの `<image1>` は `Picture 1` に直す）。このエンコーダーは**肯定と否定の 2 つのノード**なので、強さでサンプリングを区切るときは、各区間で両方を同じ参照で作り直す（否定のプロンプトは作り直さず元のまま）。それ以外（強さ s の意味・役割の文・顔と画風の参照に VAE を渡さないこと・LoRA）は `noctq-v4-qwen-image21` と同じ。`ModelSamplingAuraFlow`・`CFGNorm` を通すので、区間分けの `CFGGuider` のモデルもその出力を使う |
| `controlnet`（Issue #976） | `sdxl-controlnet-canny`（`txt2img-sdxl-controlnet`） | ちょうど 1（参照が無い実行は「制御画像が要る」と拒否。`no_reference` と `optional` スロットは持てない） | 全体のみ | 参照は**制御画像**で、`LoadImage` → `ControlNetApplyAdvanced` に入る（コアのノード。追加ノードは要らない）。強さ s は `reference_strength` の割り当てで ControlNet の `strength` に入る（テンプレートの既定 0.7）。前処理（Canny など）は行わないので、線画にした画像を渡す |
| `ipadapter`（Issue #976） | `sdxl-ipadapter-plus`（`txt2img-sdxl-ipadapter`。`requires_nodes: ["ipadapter-plus"]`） | ちょうど 1（参照が無い実行は「IP-Adapter の参照画像が要る」と拒否） | 全体のみ | 参照は IP-Adapter の画像で、`IPAdapterAdvanced`（追加ノード `ipadapter-plus`）の `weight` に強さ s が入る。ノードが無い間は生成テストの一覧に出ない（custom-nodes.md §6） |

- **LoRA**: `lora_model_node`（モデルを出力するノード）がある画像ワークフローだけが受ける。リクエストの LoRA は、そのノードの出力 0 を使うノードの手前に `LoraLoaderModelOnly` を差し込む（強さ既定 0.4）。受けないモデルでは警告 `lora_unsupported` を出し、送らない。
- **速さのモード（Issue #930）**: 動画ワークフローは `speed_modes`（`{id, label, overrides[]}` の配列。ID は小文字・数字・`-`）を持てる。**先頭がグラフそのまま（既定）**で `overrides` を持たず、2 つ目以降は `overrides`（`{node, input, value}`。値は JSON か `["<node>", <出力>]` のリンク）でグラフの値（ステップ数・cfg・shift・LoRA を外すリンクの付け替え）を書き換える。リクエストの `speed_mode` が空なら先頭、モードの名前なら その `overrides` を、割り当ての代入のあと・LoRA の挿入の前にグラフへ入れ、**出力ノードから辿れなくなったノード（外した LoRA）を送らない**（サーバーが使わないファイルを読まないため）。ワークフローに無いモードを名指しすると 422（生成テストは見積りの時点で警告 `speed_mode_unsupported` を出し、そのモデルは既定で作る）。検証（`ValidateModel`）は、動画だけ・ID の形と重複・ラベル・先頭に `overrides` が無いこと・各 `overrides` が存在するノードの入力と JSON の値（リンクは存在するノード）を指すことを確かめる。組み込みのテンプレートで持つのは `img2video-wan22-14b-lightx2v`（`fast` / `standard`）だけ。
- **インストール済みのモデルの更新**: モデルマネージャーはモデルの導入時にだけテンプレートを `local_models` へ写す。この対応より前に導入したモデルは参照を使えない古い写しのままなので、mam の起動時（`app` が `wfrefresh.Run` を呼ぶ。接続先の登録のあと）に、カタログのワークフロー名と同じ名前の保存済みモデルで、`reference_mode` が無く、テンプレートには有り、**利用者が書き換えていない古い写し**であるものを、テンプレートの内容に置き換える（利用者が付けた価格は残す）。「書き換えていない古い写し」は、保存済みのグラフのすべてのノードがテンプレートにも同じ `class_type` で有り、保存済みの各ノードの各入力が、値がリテラルならテンプレートに同じ値で有り（モデルファイル `unet_name`・`ckpt_name`、`steps`、`cfg`、`lora_name` などが違えば利用者の編集とみなす）、リンクならテンプレートでは別のリンクでもよい（参照画像のためにつなぎ替えた）こと、割り当てがすべてテンプレートにも有り、出力ノードが同じことで決める。テンプレートに増えたノード・入力・割り当ては差し支えない。利用者が書き換えたモデル、`--model-config` で固定したファイル、テンプレートが参照に対応しないモデル、すでに最新のモデルは変えない。
- **画面・MCP での編集**: 保存済みのモデルを画面（`PUT .../models/:model`）や `update_local_model` で編集しても、フォームに欄の無い `reference_mode`・`reference_denoise_min`・`no_reference`・`lora_model_node`・`reference_image` の `optional`、フォームが 1 行に出す値の 2 つ目以降の割り当て（Chroma の参照の縮小ノードの `width`・`height`）は、保存済みのワークフローのまま残る。画面は保存済みのワークフローを土台に、グラフ・割り当ての表の行・出力ノード・fps などだけを置き換える（テンプレートから作る新規のモデルも、隠しフィールド `template` のテンプレートを土台にする）。参照画像の行は、保存済みのワークフローが持つ枚数（Qwen-Image 2.1 は 10）まで出す（最低 4）。MCP の `assignments` の `optional` を省くと、保存済みの同じ（`param`・`index`・`node`・`input`）の割り当ての値を引き継ぐ。
- **参照なしの入力**: `reference_mode` があるモデルは、`ImageRequest.Options.Refs` が無いとき（キャラクターシートや Workflow のステップが `Inputs` だけを渡す経路）、`Inputs` を参照として扱わない（アップロードせず、参照のノードも使わない。テキストからの生成になる）。`reference_mode` が空のワークフロー（`img2img-sdxl` など利用者のもの）は、これまでどおり `Inputs` を取る。
- **使える LoRA の一覧**: 同梱エンジンの `GET /object_info/LoraLoaderModelOnly` の `lora_name`。
- **ポーズ・構図を真似させない**: ネガティブに `same pose as reference, same composition as reference, studio portrait` を足す（参照を 1 枚でも使うモデルだけ。ネガティブ欄が無いモデルは `Avoid:` としてプロンプトに畳み込む）。
- **実機の確認（2026-11-02、RTX 3080 Ti 12GB、ComfyUI 0.42.1、`noctq-v4-qwen-image21`、seed 固定、25 ステップ）**: 参照に生成した顔の画像を 1 枚渡し、出力の顔と参照の顔の類似度（insightface buffalo_l のコサイン類似度）を測った。役割 `edit`・「同じ顔のまま浜辺に立つ」のプロンプトで、参照なし −0.08、強さ 0.5 → 0.16、0.7 → 0.30、0.85 → 0.39、1.0 → 0.49（同一人物と見なせる水準は 0.4〜0.5 以上が目安）。役割 `face`（髪・服・ポーズはプロンプト優先）は同じ強さでも効きが弱く、強さ 1.0 で 0.26。参照なしからは上がるが、顔を固定しきるものではない。顔を確実に揃えるなら LoRA を併用する。img2img の 2 方式（`chroma1-hd`・`flux1-dev-nsfw-master`）はワークフローの構造のテストのみで、GPU では未実行。
  `chroma1-hd` の img2img（同条件、プロンプトは同じ人物の特徴を書いたもの）は、出力と参照の平均画素差（0〜255）が、参照なし 72、強さ 0.2 → 38、0.4 → 20、0.7 → 5.4、1.0 → 3.7 と単調に縮まる。`flux1-dev-nsfw-master` は重みを取得しておらず、LoRA の差し込みも実ファイルでは試していない（ワークフローの構造のテストと偽の ComfyUI でのテストのみ）。

### 4.1 LTX-2.5（Lightricks。映像と音声を一緒に作る動画モデル）

カタログ `ltx-2.5-22b-distilled`（[model-manager.md](model-manager/model-manager.md) §4.2）が入れるモデルと、組み込みテンプレート `txt2video-ltx-2-5`（ワークフロー名 `ltx-2-5-t2v`。テキストから動画）・`img2video-ltx-2-5`（`ltx-2-5-i2v`。先頭フレームから動画）。Comfy 公式の LTX-2.5 のワークフロー（Template Library の Video。出典: [LTX-2.5 in ComfyUI](https://docs.comfy.org/tutorials/video/ltx/ltx-2-5)）と同じファイル・同じ手順（2 段: 半分の解像度 → 潜在空間で 2 倍 → 仕上げ）で、24 fps・フレーム数 8n+1・幅と高さは 64 の倍数。出力は音声つきの動画（`video.native_audio`）。旧 `ltx-video-2b`（LTX-Video 2B v0.9.5。`txt2video-ltx-v095`・`img2video-ltx-v095`）はそのまま残る。

モデルのファイルは Hugging Face の **gated** なリポジトリ `Lightricks/LTX-2.5` にある。**mam からアクセスを申請することはできない**ので、導入の前に次の順で準備する:

1. **同意とアクセスの申請**: Hugging Face にログインして `https://huggingface.co/Lightricks/LTX-2.5` を開き、ライセンスに同意してアクセスを申請する。承認されるまで取得は 403 になる。
2. **トークンの発行**: Hugging Face の設定（Access Tokens）で、読み取り（Read）権限のトークンを発行する。
3. **トークンの保存**: モデルマネージャーの「Hugging Face のトークン（任意）」の区画に貼り付けて保存する（OS の資格情報ストアに入る。画面には出ない）。MCP は `set_huggingface_token`。
4. **導入**: カタログの `ltx-2.5-22b-distilled`（「HF トークンが必要」のバッジ）の導入を押し、mam のライセンスの確認にも同意する。トークンが無い・無効・承認前のときは、ジョブを作る前に、状況に合う案内（model-manager.md §5.3 の文言 A・B・C）で止まる。

**外部の ComfyUI**（macOS・ブラウザ版・サーバー版など、同梱エンジンを使わない環境）では、mam はモデルを入れない。上の 1〜2 のあと、ComfyUI 側で同じ名前のファイルを `diffusion_models/`・`text_encoders/`・`vae/`・`latent_upscale_models/` に置く（Comfy の公式ワークフローを開くと、ファイルの取得を案内される）。置いたあと、設定の「ローカル・代替モデル」にテンプレートからワークフローを登録する。ComfyUI は、必要なノード（`LTXV*`・`ComfyMathExpression`・Gemma 4 の `CLIPLoader type=ltxv`）を持つ新しい版にする（同梱エンジンの v0.42.1 は持つ）。GPU での生成は未確認（model-manager.md §11）。

**非検閲のテキストエンコーダー・音声つきの出力・macOS（Issue #927）**: 標準のテキストエンコーダーの代わりに、コミュニティ製の非検閲版を使うテンプレートが `txt2video-ltx-2-5-uncensored`・`img2video-ltx-2-5-uncensored`（成人向け。カタログ `ltx-2.5-22b-distilled-uncensored`。違いは `CLIPLoader` の `clip_name` だけ。[model-manager.md](model-manager/model-manager.md) §4.2）。保存は `SaveVideo`（`format`・`codec` ともに `auto` = H.264 と AAC の mp4）で、音声トラックは 1 本の mp4 に入り、mam は再エンコードせずに保存・再生する。macOS の外部 ComfyUI（MPS）は、LTX-2.5 が黒い動画になる・保存に失敗する報告があり、**動く場合もあるが不安定で、mam では未確認**（同 §4.2「macOS」）。

**FLF2V・プロンプト拡張・DFR・NVFP4（Issue #968）**: 最初と最後のフレームの 2 枚から動画を作るテンプレート `flf2v-ltx-2-5`（ほかに `-nvfp4`・`-uncensored`。`first_frame` と `last_frame` の割り当て）、短いプロンプトを Gemma 4 E2B で書き換える `txt2video-ltx-2-5-enhanced`・`img2video-ltx-2-5-enhanced`・`flf2v-ltx-2-5-enhanced`、3 段の DFR `img2video-ltx-2-5-dfr`（時間方向の拡大・詳細化の IC-LoRA。IC-LoRA は別のゲートのリポジトリで、そのページでも同意とアクセスの承認が要る）、NVFP4 の拡散モデルの `*-ltx-2-5-nvfp4`（NVIDIA Blackwell 世代の GPU 向け）を足した。どれもコアのノードだけで、カスタムノードは使わない。カタログのモデル・ファイル・ライセンス・未計測の項目は [model-manager/model-manager.md](model-manager/model-manager.md) §4.2・§4.5・§11。

## 5. 接続テスト

ComfyUI は `GET /system_stats`（バージョンを返す）、OpenAI 互換は `GET /v1/models`（先頭 20 件のモデル ID を返す）。互換サーバーが 404/405 を返したら「到達できる。`/v1/models` は提供されていない」を注意付きの成功とする。

## 6. ダウンロードの方針（緩和は登録した接続先のみ）

- 登録した接続先の**同じスキーム・ホスト・ポート**の URL（ComfyUI の `/view`、互換サーバーの `url` が自分自身を指す場合）は、http・`localhost`・プライベート・`100.64.0.0/10` へも接続できる（`safefetch.OriginClient`）。
- それ以外の URL は `safefetch` の規則のまま（https・公開アドレスのみ）。**第三者ベンダーの応答 URL が `localhost`・プライベート・`100.64.0.0/10` を指しても拒否する。**
- ホスト違い、https から http へのリダイレクトは拒否する。API キーは接続先の `base_url` 配下にだけ付ける。
- **リンクローカルは登録した接続先でも常に拒否**する。判断は接続時に解決した IP で行う（`httpclient.NewGuarded` のダイヤラの `Control`）ので、DNS リバインディングで解決後に変わっても防げる。拒否は 422 の `ProviderError`（`safefetch.ErrRefused` を包む）で、再試行せず、ブレーカーにも数えない。

## 7. 反映

保存した接続先・モデルは再起動なしで効く。`Adapter.Reload(cfg, backends)` が構成の一式を 1 回のアトミックな入れ替えで差し替え、実行中の呼び出しは古い構成のまま続く。ブレーカーは（同時実行数が同じなら上限も）変えなかったベンダーで引き継ぐ。`models.json` の `images`・`policies` を保存する `modelsettings` も同じ入れ替えを呼ぶ。

## 8. Service API（`internal/service/localmodels`）

設定画面と MCP ツールはこのサービスを呼ぶ（`service.Services.LocalModels`）。`service` パッケージの行数の上限のため `CredentialService` は広げず、接続先の API キーはここで扱う（`credential.Store` のベンダー名 `endpoint:<名>`。値は返さない）。`models.json` の編集は `settingsfile.Lock` でほかの設定の保存と直列化する。

```go
func New(Deps) *Service
// 接続先
func (s *Service) CreateEndpoint(ctx, EndpointInput{Name, Kind, BaseURL, MaxConcurrency, TimeoutSeconds}) error
func (s *Service) UpdateEndpoint(ctx, name string, EndpointInput) error      // Name・Kind は繰り返すだけ
func (s *Service) DeleteEndpoint(ctx, name string) error                     // 接続先のモデルも消す。用途・Policy に使われていれば 422 で拒否。キーも消す
func (s *Service) EndpointKeyStatus(name string) (KeyStatus{Configured, LastChars}, error)
func (s *Service) SetEndpointKey(name, apiKey string) error                   // 固定ファイルでも可
func (s *Service) ClearEndpointKey(name string) error
// モデル
func (s *Service) CreateModel(ctx, ModelInput{Endpoint, Name, Model localcfg.Model}) error
func (s *Service) UpdateModel(ctx, endpoint, name string, ModelInput) error  // 名前の変更は 422
func (s *Service) DeleteModel(ctx, endpoint, name string) error              // 用途・Policy に使われていれば 422
// 参照・補助
func (s *Service) View() (View{Pinned, Endpoints []EndpointView, Models []ModelView}, error)  // ModelView.UsedBy
func (s *Service) Test(ctx, TestInput{Name, Kind, BaseURL, APIKey}) (TestResult{OK, Message, Note, Version, Models}, error)
func (s *Service) TestEndpoint(ctx, name string) (TestResult, error)
func (s *Service) ImportWorkflow(raw []byte) (ImportedWorkflow{Graph, Nodes}, error)
func (s *Service) Templates() []localcfg.Template
func (s *Service) Reload() error                                            // ファイルを読み直して反映（起動・他の保存の後）
```

- 失敗は `ErrPinned`（固定ファイル）、`ErrNotFound`、`ErrExists`、`*InvalidError`（`Message` を利用者に出す。HTTP は 422）。保存前に起動時と同じ `configcheck` を通し、通らなければ何も書かない。
- **用途・Quality Policy への割り当て**は既存の `modelsettings.Service`（`images.<purpose>`・`policies.<policy>`）で行う。`images` と `policies` の選択肢に、登録済みの画像・動画モデルが識別子で加わる（動画の解像度は `video.resolutions` から）。
- `View` と `EndpointKeyStatus` はキーを返さない（設定の有無と末尾 4 文字のみ）。`Test` の `APIKey` はテスト用で保存しない。
- 用途・Policy に使われているモデル（接続先ごと消す場合も）は削除できない（`*InvalidError`）。`View` の `ModelView.UsedBy` が参照元（`images.new_reference.primary` など）を返す。

## 9. テスト

偽の ComfyUI（`httptest` と WebSocket、`comfyuitest`）と偽の OpenAI 互換サーバーによる契約テストで、アップロード・割り当て・進捗・取り消し・失敗・キーの送り先・フェイルオーバー・即時反映・ダウンロード方針の両方向（登録した接続先は許す、第三者・リンクローカルは拒否）を確かめる。Generation Orchestrator を通した Reference Pack と Take の生成（`take.model_name`・Cost Ledger の識別子、進捗の割合。Keyframe は最初のフレームとして ComfyUI に渡る）は `generation/relay` のテストが担う。実機の GPU が要るもの（テンプレートの実行、実際の生成時間・VRAM）は未検証。

## 10. 設定画面と MCP ツール

- **設定画面**（`GET /settings/local-models` の「ローカル・代替モデル」）: §8 の Service API を HTMX の断片（追加・編集はモーダル、モデルの一覧は接続先のサブページ）で操作する。画面の仕様とルートは [../api/endpoints-local-models.md](../api/endpoints-local-models.md)。固定ファイル（`--model-config`）は読み取り専用で、理由を表示して変更のボタンを無効にする（API キーと接続テストは使える）。名前と種類は編集で変えられない。リンクローカルの `base_url` は 422 で、接続テストも理由を表示する。API キーは設定済みかと末尾 4 文字だけを表示する。
- **反映の確認**: 接続先・モデルの追加・削除の応答に「画像・動画モデル」のフォームは添えない（`hx-swap-oob` の OOB は無い）。そのモーダルを開くたびに最新の選択肢を読むので、追加したモデルが画像の 4 用途（primary / fallback）と Quality Policy の選択肢に並ぶ。「画像・動画モデル」の保存も再起動を待たない（`modelsettings` の保存が `Reload` を呼ぶ）。
- **MCP ツール**: 同じサービスを呼ぶ 15 のツール（接続先・API キー・接続テスト・テンプレート・ワークフローの取り込み・モデル）。割り当ては既存の `set_model_assignment` に識別子を渡す。生成は既存の `generate_shot` などで、ジョブは `get_job` で見る。一覧は [../api/mcp-tools-local-models.md](../api/mcp-tools-local-models.md)。
