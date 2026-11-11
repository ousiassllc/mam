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

- **割り当て表（`assignments`）**: `param` の値をノード `node` の入力 `input` に入れる。使える `param`: `prompt`、`negative_prompt`、`seed`（実行ごとに乱数。単発生成と `run_workflow` は固定もできる）、`steps`・`cfg`・`sampler`・`scheduler`（§4.3）、`width`、`height`、`frames`（`offset` でフレーム数に整数を足せる。§4.3.1）、`fps`、`reference_image`（`index` で何枚目か。`optional: true` なら、その枚数が無い実行ではノードごと外れる）、`reference_denoise`（画像のみ。§4.2）、`first_frame`、`last_frame`。画像は `frames`・`fps`・`first_frame`・`last_frame` を使えない。幅・高さは縦横比と解像度から決め、`size_multiple` の倍数に丸める。
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
- **`ratio`（`steps` の割り当てだけ。0〜1 の間）**: ステップ数そのものではなく `round(steps × ratio)` を入れる。2 段のサンプラー（Wan 2.2 14B の高ノイズ・低ノイズ。`KSamplerAdvanced` の `end_at_step`・`start_at_step`）の切り替えのステップが、ステップ数を変えても同じ割合の場所に残る（Wan 2.2 14B は 0.5、Remix は 20 ステップ中の 10 ではなく 12 ステップ中の 10 なので 0.8333）。結果は 1 以上で、後の段が動く余地を残す。ただし 1 ステップでは後の段が何もしない（`end_at_step`＝`start_at_step`＝1 で、ノイズのままの動画になる）ので、`ratio` を持つ `steps` の割り当てがあるワークフローは **`sampling.Caps.TwoStage`** を立て、ステップ数の下限を 2 にする（`Caps.MinSteps()`・`Settings.ValidateFor`。単発生成・`run_workflow` の検査と、画面のスライダーの下限 `data-min-steps` が同じ値を使う。速さのモードが steps を決めるワークフローは依頼の値を使わないので、印を立てない）。保存済みの割り当てを送り返す更新（`update_local_model`・設定画面）は `ratio` を引き継ぐ。
- **速さのモードが優先（決定）**: `speed_modes` を持つワークフロー（動画の Wan 2.2 14B + LightX2V、画像の Qwen-Image-Edit 2511 + Lightning など）は、蒸留 LoRA がそのステップ数と cfg でしか効かないので、`steps`・`cfg` を**速さのモードが決める**。リクエストの `steps`・`cfg` は、画面では欄を無効にして理由を出し、サーバー（`localcfg/sampling.Caps` の `Applicable`）でも**無視する**（警告 `sampling_ignored`）。`sampler`・`scheduler` は上書きできる。
- **`sampling.Caps`（`localcfg/sampling`）**: ワークフローが受け取る値と既定（`Workflow.SamplingCaps()`）。`modelcatalog.Info.Sampling`・MCP の `list_generation_models` が返す。範囲は steps 1〜150・cfg 0〜30（`MinSteps` ほか。画面のスライダーと同じ）。サンプラー・スケジューラー名は ComfyUI の `comfy/samplers.py` の一覧（`sampling.Samplers`・`Schedulers`）。
- **検証**: 保存時（`validateSampling`）、`ratio` は `steps` だけで 0〜1 の間、`steps`・`cfg` は数値の入力、`sampler`・`scheduler` は文字の入力に割り当てる。
- **品質のプリセット（`quality_presets`。Issue #977）**: ワークフローの `quality_presets` = `{"fast": {"steps": N, "cfg": C}, "high": {…}}`（各項目は省ける）は、画面の「速い／きれい」と MCP の `quality` が使う steps・cfg。**標準は書かない**（標準 = グラフが持つ値のまま）。`steps`・`cfg` の割り当てを持つワークフローにだけ書け、速さのモード（`speed_modes`）を持つものには書けない（モードが steps・cfg を決める）。速いは steps が少なく、きれいは多い。値は mam の目安で GPU では確かめていない。ワークフローの steps から変えていない導入済みのコピーは、起動時に `wfrefresh` が更新する。詳しくは [gentest/gentest.md](gentest/gentest.md) §3.6。


#### 4.3.1 フレーム数のオフセット（`frames` の割り当ての `offset`。Issue #1076）

- **`offset`（`frames` の割り当てだけ。整数、−64〜64、省略は 0）**: フレーム数そのものではなく **フレーム数＋offset** を入れる（1 未満になるときは 1。`Assignment.FramesFor`）。フレーム数は、長さ × `fps` を `frames_step`（Wan は 4n+1）で丸めた値（`Workflow.Frames`）。式は「frames に足す整数」だけで、掛け算・別のパラメーターは使わない（`frames-2` は `offset: -2`）。
- **使いどころ**: 最初と最後のフレームを渡す動画で、間の灰色のフレームの数が `frames-2` になるグラフ（Phr00t の Mega の FLF2V テンプレート `flf2v-wan22-14b-rapid-mega`。[model-manager/model-manager.md](model-manager/model-manager.md) §4.2 の #1076）。同じ `frames` を複数のノードに割り当てられる（`WanVaceToVideo` の `length` は offset 0、`RepeatImageBatch` の `amount` は offset −2）。
- **検証**: 保存時（`validateWorkflow`）、`offset` は `frames` だけで、−64〜64（`maxFramesOffset`）。ほかの `param` に付けるとエラー（`offset applies to frames only`）。MCP の割り当て（`localmodels` の `assignments`・`workflows` の割り当て）も `offset` を持ち、`update_local_model` が `offset` を省いた割り当ては、保存済みの同じ割り当て（param・index・node・input が同じもの）の値を引き継ぐ（`ratio` と同じ）。

### 4.4 プロンプトのウェイト（`prompt_weight`。Issue #972）

- **値**: ワークフローの `prompt_weight` = `supported`（効く）・`weak`（適用されるが効きが弱いかもしれない）・`unsupported`（効かない）・`unknown`（確かめていない。省略も同じ）。`modelcatalog.Info.PromptWeight`・`list_generation_models` が返し、画面の「?」と警告 `prompt_weight` の源になる。組み込みテンプレートの全部に値を入れてある。
- **根拠（ComfyUI `comfy/sd1_clip.py`・`comfy/text_encoders/*.py` を読んだ事実）**: ComfyUI は `(語:1.3)`・`(語)`（1.1 倍。入れ子は 1.1 ずつ）を、テキストエンコーダーのトークナイザーが `disable_weights` を立てていないときだけ読む（`SDTokenizer.tokenize_with_weights` が `token_weights` で切り出し、その語の埋め込みを重みで拡大する）。AUTOMATIC1111 の `[語]` は**読まない**（ただの文字）。`qwen_image`（Qwen-Image・Qwen-Image 2.1）・FLUX.2 klein（`Qwen3`/Mistral のテンプレート）・Hunyuan Video 1.5・LTX-2.5（Gemma）のトークナイザーは `disable_weights=True` なので、かっこと数字がそのまま言語モデルに文として渡る → `unsupported`。SDXL・SD 3.5 は CLIP が読む → `supported`。
- **断定しないもの**: Flux.1（T5-XXL + CLIP-L。ComfyUI は重みを読むが、文を読むのは T5 で、効きは確かめていない）・Chroma（T5）・Wan 2.2（UMT5）・LTX-Video 0.9.5（T5）は `weak`。GPU での効き方は未確認（実機の確認が要る）。ユーザー自作のワークフローと外部のモデルは `unknown`（警告は出さない。当て推量の警告は雑音になる）。
- **効かないモデルへの案内**: 画面の「?」と、見積りの警告 `prompt_weight`（プロンプトに `(…)` があるときだけ）が、「強調したい語を文で言い換える（特に・とても・大きく、先頭に置く、具体的に書く）」を出す。

### 4.5 画像を大きくするワークフロー（`upscale`。Issue #975）

- **印**: ワークフローの `upscale`（`{label, native, mode, max_output_pixels}`。`localcfg/upscale.Spec`。印の型・倍率の計算（`Ratio`）・検証は `localcfg` の下位パッケージ `upscale` に置いた。`promptweight` と同じ分け方）は、そのワークフローが**画像を作るのではなく、渡した画像を大きくする**ことを表す。`native` はアップスケール用モデルの倍率（4 倍のモデルなら 4）、`mode` は `model`（`UpscaleModelLoader` → `ImageUpscaleWithModel` → `ImageScaleBy`）か `two_pass`（そのあと `VAEEncodeTiled` → 低い denoise の `KSampler` → `VAEDecodeTiled`）。画像のワークフロー（`media: image`）の印として持つので、接続先・保存・版の仕組みはそのまま使う。
- **割り当て**: `reference_image`（大きくする画像。ちょうど 1 つ）と `upscale_ratio`（`ImageScaleBy` の `scale_by` に、依頼の倍率 ÷ `native` を入れる。ちょうど 1 つ。`two_pass` は加えて `prompt`・`negative_prompt`・`seed`・`steps`・`cfg`・`sampler`・`scheduler`）。印の無いワークフローに `upscale_ratio` は割り当てられない。印のあるワークフローはプロンプトの割り当てが要らない（`model` 方式）。グラフに `ImageUpscaleWithModel` が要る。`localcfg.validateUpscale` → `upscale.Validate`。
- **一覧**: 印のあるワークフローは、プロンプトを渡すモデルではないので、`localcfg.IDs`（画像・動画のモデルの識別子）・単発生成のモデルの一覧には**出ない**。単発生成の `modelcatalog.Catalog.Upscalers` が専用の一覧を返し、「大きくする」（[gentest/gentest.md](gentest/gentest.md) §3.4）が使う。
- **コアのノードだけ**: `ImageScaleBy`・`ImageUpscaleWithModel`・`UpscaleModelLoader`・`VAEEncodeTiled` をテストの `coreNodes`（テンプレートが使ってよいクラスの一覧）に足した。同梱エンジンは追加ノードなしで動く。

### 4.2 画像モデルの参照画像・強さ・役割・LoRA（#933）
画面と MCP は、どのモデルでも同じ「強さ」（0〜1、既定 0.7。役割「顔だけ」は 0.9、§4.5）と「役割」を渡す。ワークフローの作りの違いは `localcfg`・`comfyui` が吸収する。`reference_mode` が方式を決める（`localcfg.Model.ReferenceCaps()` が方式・上限枚数・選べる役割を返し、画面と `accepts_references`・`max_reference_images` の元になる）。

| `reference_mode` | モデル | 枚数 | 役割 | 強さ s の効き方 |
|---|---|---|---|---|
| `img2img` | `chroma1-hd`（`txt2img-chroma`）、`flux1-dev-nsfw-master`（`txt2img-flux-dev-lora`） | 1 | 全体のみ | 参照を VAE で潜在にして途中から生成する。`reference_denoise` の割り当てに `1 − (1 − floor)×s` を入れる（`floor` は `reference_denoise_min`。既定 0、`chroma1-hd` は 0.6。Chroma の schedule は denoise 0.6 未満では絵がほとんど変わらず 0.95 で参照が消えるため、強さの範囲を変化のある区間に広げる。下限は 0.02）。参照が無い実行は `no_reference`（空の潜在に戻す）を当てて参照のノードを外し、テキストから生成と同じになる |
| `edit` | `noctq-v4-qwen-image21`（`txt2img-qwen-image21`） | 最大 10（`reference_image` の `optional` スロット） | 全体・顔だけ・画風だけ・編集対象・ポーズだけ（`pose`。Issue #986） | 参照は `TextEncodeQwenImage21` の `image_1`〜に入る（プロンプトの `<image1>` が 1 枚目）。強さ s の参照は、サンプリングの最初の `round(s×steps)` ステップ（最低 1）だけ条件に入り、残りはプロンプトだけで仕上げる。強さが違う参照ごとにサンプリングを区切り（最大 6 区間。区切りが 5 つを超えるほど強さが散らばるときは、均等に選んだ 5 つを残し、ほかの参照の区切りは一番近い残した区切りへ寄せる。終わりまで残る参照にはならない）、各区間を別のエンコーダー・`SamplerCustomAdvanced` で回す。役割は文で伝え（顔だけ: 「顔と同一性だけを保つ」、画風だけ: 「画風・色・雰囲気だけ」）、顔・画風は VAE を渡さない（ビジョンだけで読む）。VAE はデコードのノード（`VAEDecode`）がつなぐ `VAELoader` |
| `edit`（FLUX.2） | `flux2-klein-4b`（`txt2img-flux2-klein`。#931） | 最大 4（`reference_image` の `optional` スロット） | 全体・顔だけ・画風だけ・編集対象・ポーズだけ（`pose`。Issue #986） | 参照は視覚モデルではなく潜在として入る: 画像ごとに `ImageScaleToTotalPixels`（約 1 メガピクセル）→ `VAEEncode` → `ReferenceLatent` を、肯定・否定の条件に順に重ねる（ComfyUI の公式の編集テンプレートと同じ。テンプレートは 4 枚分のスロットを持ち、使わない枚数は画像・縮小・VAEEncode・`ReferenceLatent` ごと外れ、`ReferenceLatent` は素通しに戻る）。強さの意味は Qwen の編集と同じ（最初の `round(s×steps)` ステップだけ参照が入る。蒸留版は 4 ステップなので 0.25 刻み。最大 3 つの区切りで、区間ごとに別のエンコーダー・否定の起点・`ReferenceLatent` の鎖・`CFGGuider`・`SamplerCustomAdvanced` で回す）。プロンプトの `<image1>` は klein の書き方の `image 1` に直す。顔・画風の役割は文でだけ伝える（視覚モデルがないので VAE の有無の違いはない）。出力の大きさは縦横比の指定に従い、1 枚目の参照の大きさにはしない |
| `edit`（Qwen-Image-Edit） | `qwen-image-edit-2511`（`txt2img-qwen-image-edit-2511`。#971） | 最大 3（`reference_image` の `optional` スロット） | 全体・顔だけ・画風だけ・編集対象・ポーズだけ（`pose`。Issue #986） | 参照は `TextEncodeQwenImageEditPlus` の `image1`〜`image3` に入る（`TextEncodeQwenImage21` と違い入力名が `imageN`。プロンプトの `<image1>` は `Picture 1` に直す）。このエンコーダーは**肯定と否定の 2 つのノード**なので、強さでサンプリングを区切るときは、各区間で両方を同じ参照で作り直す（否定のプロンプトは作り直さず元のまま）。それ以外（強さ s の意味・役割の文・顔と画風の参照に VAE を渡さないこと・LoRA）は `noctq-v4-qwen-image21` と同じ。`ModelSamplingAuraFlow`・`CFGNorm` を通すので、区間分けの `CFGGuider` のモデルもその出力を使う |
| `controlnet`（Issue #976） | `sdxl-controlnet-canny`（`txt2img-sdxl-controlnet`）・`sdxl-controlnet-depth`・`sdxl-controlnet-normal`（`txt2img-sdxl-controlnet-depth`・`-normal`。#1020） | ちょうど 1（参照が無い実行は「制御画像が要る」と拒否。`no_reference` と `optional` スロットは持てない） | 全体・ポーズだけ（`pose`。#1020） | 参照は**制御画像**で、`LoadImage` → `ControlNetApplyAdvanced` に入る（コアのノード。追加ノードは要らない。法線は間に `SetUnionControlNetType`）。強さ s は `reference_strength` の割り当てで ControlNet の `strength` に入る（テンプレートの既定 0.7）。前処理は Canny だけを mam が行う（canny のテンプレート。`reference_preprocess`）。深度・法線のテンプレートは前処理なしで、渡した画像（ポーズ集の深度・法線マップ。[model-manager/local-runtime.md §12.9](model-manager/local-runtime.md)）をそのまま制御画像にする |
| `ipadapter`（Issue #976） | `sdxl-ipadapter-plus`（`txt2img-sdxl-ipadapter`。`requires_nodes: ["ipadapter-plus"]`） | ちょうど 1（参照が無い実行は「IP-Adapter の参照画像が要る」と拒否） | 全体のみ | 参照は IP-Adapter の画像で、`IPAdapterAdvanced`（追加ノード `ipadapter-plus`）の `weight` に強さ s が入る。ノードが無い間は単発生成の一覧に出ない（custom-nodes.md §6） |

- **LoRA**: `lora_model_node`（モデルを出力するノード）がある画像ワークフローだけが受ける。リクエストの LoRA は、そのノードの出力 0 を使うノードの手前に `LoraLoaderModelOnly` を差し込む（強さ既定 0.4）。受けないモデルでは警告 `lora_unsupported` を出し、送らない。
- **速さのモード（Issue #930・#991）**: 動画・画像のワークフローは `speed_modes`（`{id, label, overrides[]}` の配列。ID は小文字・数字・`-`）を持てる。**先頭がグラフそのまま（既定）**で `overrides` を持たず、2 つ目以降は `overrides`（`{node, input, value}`。値は JSON か `["<node>", <出力>]` のリンク）でグラフの値（ステップ数・cfg・shift・LoRA を外すリンクの付け替え）を書き換える。リクエスト（`VideoRequest.SpeedMode`・`ImageRequest.Options.SpeedMode`）の `speed_mode` が空なら先頭、モードの名前なら その `overrides` を、割り当ての代入のあと・LoRA の挿入の前にグラフへ入れ、**出力ノードから辿れなくなったノード（外した LoRA、先頭のモードが使わない別のモードの LoRA）を送らない**（先頭のモードでも、このノードの削除は行う）（サーバーが使わないファイルを読まないため）。ワークフローに無いモードを名指しすると 422（単発生成は見積りの時点で警告 `speed_mode_unsupported` を出し、そのモデルは既定で作る）。検証（`ValidateModel`）は、ID の形と重複・ラベル・先頭に `overrides` が無いこと・各 `overrides` が存在するノードの入力と JSON の値（リンクは存在するノード）を指すことを確かめる。組み込みのテンプレートで持つのは `img2video-wan22-14b-lightx2v`（`fast` / `standard`）と、画像の `txt2img-qwen-image-edit-2511`（`standard`〔既定。20 ステップ・cfg 4・LoRA なし〕/ `lightning-8` / `lightning-4`。Issue #991）だけ。Qwen-Image-Edit 2511 のモードは、`UNETLoader`（`37`）の出力を受ける `LoraLoaderModelOnly`（`90` = 4 ステップ、`91` = 8 ステップ。どちらも強さ 1）をグラフに持ち、`lightning-*` が `ModelSamplingAuraFlow`（`66`）の `model` をその LoRA へ付け替え、`KSampler` の `steps` を 4・8、`cfg` を 1 にする（`standard` ではどちらの LoRA も送らない）。リクエストの LoRA（`lora_model_node` = `37`）は `UNETLoader` の直後に入り、Lightning の LoRA はその後ろに重なる。速さのモードを持つので、このテンプレートに `quality_presets` は書けず（以前の速い 12・きれい 30 ステップは外した）、画面・MCP の `steps`・`cfg`・品質（`quality`）は使われない（速さのモードを選ぶ）。
- **インストール済みのモデルの更新**: モデルマネージャーはモデルの導入時にだけテンプレートを `local_models` へ写す。この対応より前に導入したモデルは参照を使えない古い写しのままなので、mam の起動時（`app` が `wfrefresh.Run` を呼ぶ。接続先の登録のあと）に、カタログのワークフロー名と同じ名前の保存済みモデルで、`reference_mode` が無く、テンプレートには有り、**利用者が書き換えていない古い写し**であるものを、テンプレートの内容に置き換える（利用者が付けた価格は残す）。「書き換えていない古い写し」は、保存済みのグラフのすべてのノードがテンプレートにも同じ `class_type` で有り、保存済みの各ノードの各入力が、値がリテラルならテンプレートに同じ値で有り（モデルファイル `unet_name`・`ckpt_name`、`steps`、`cfg`、`lora_name` などが違えば利用者の編集とみなす）、リンクならテンプレートでは別のリンクでもよい（参照画像のためにつなぎ替えた）こと、割り当てがすべてテンプレートにも有り、出力ノードが同じことで決める。テンプレートに増えたノード・入力・割り当ては差し支えない。利用者が書き換えたモデル、`--model-config` で固定したファイル、テンプレートが参照に対応しないモデル、すでに最新のモデルは変えない。
- **導入したあとにカタログが足したワークフロー名（Issue #990）**: `wfrefresh.Run` は既存の保存済みモデルの中身だけを揃えるので、導入したあとにカタログに増えたワークフロー名（`sdxl-base-inpaint` など）は作らない。起動時に `wfrefresh.AddMissing`（`app` が `Run` の前に呼ぶ）が、導入記録にあるモデルのうち、カタログのファイルをすべて（パス・SHA-256）導入済みのものについて、記録に無いワークフロー名を保存済みモデルに足し、記録にも足す。同じ名前の保存済みモデルがあれば上書きせず（利用者のコピーは壊さない）、記録に名前が残っているもの（足した後に利用者が削除した）は足さない。詳細は [model-manager/model-manager.md](model-manager/model-manager.md) §5.2。
- **画面・MCP での編集**: 保存済みのモデルを画面（`PUT .../models/:model`）や `update_local_model` で編集しても、フォームに欄の無い `reference_mode`・`reference_denoise_min`・`no_reference`・`lora_model_node`・`reference_image` の `optional`、フォームが 1 行に出す値の 2 つ目以降の割り当て（Chroma の参照の縮小ノードの `width`・`height`）は、保存済みのワークフローのまま残る。画面は保存済みのワークフローを土台に、グラフ・割り当ての表の行・出力ノード・fps などだけを置き換える（テンプレートから作る新規のモデルも、隠しフィールド `template` のテンプレートを土台にする）。参照画像の行は、保存済みのワークフローが持つ枚数（Qwen-Image 2.1 は 10）まで出す（最低 4）。MCP の `assignments` の `optional` を省くと、保存済みの同じ（`param`・`index`・`node`・`input`）の割り当ての値を引き継ぐ。
- **参照なしの入力**: `reference_mode` があるモデルは、`ImageRequest.Options.Refs` が無いとき（キャラクターシートや Workflow のステップが `Inputs` だけを渡す経路）、`Inputs` を参照として扱わない（アップロードせず、参照のノードも使わない。テキストからの生成になる）。`reference_mode` が空のワークフロー（`img2img-sdxl` など利用者のもの）は、これまでどおり `Inputs` を取る。
- **使える LoRA の一覧**: 同梱エンジンの `GET /object_info/LoraLoaderModelOnly` の `lora_name`。
- **ポーズ・構図を真似させない**: ネガティブに `same pose as reference, same composition as reference, studio portrait` を足す（参照を 1 枚でも使うモデルだけ。ネガティブ欄が無いモデルは `Avoid:` としてプロンプトに畳み込む）。
- **実機の確認（2026-11-02、RTX 3080 Ti 12GB、ComfyUI 0.42.1、`noctq-v4-qwen-image21`、seed 固定、25 ステップ）**: 参照に生成した顔の画像を 1 枚渡し、出力の顔と参照の顔の類似度（insightface buffalo_l のコサイン類似度）を測った。役割 `edit`・「同じ顔のまま浜辺に立つ」のプロンプトで、参照なし −0.08、強さ 0.5 → 0.16、0.7 → 0.30、0.85 → 0.39、1.0 → 0.49（同一人物と見なせる水準は 0.4〜0.5 以上が目安）。役割 `face`（髪・服・ポーズはプロンプト優先）は同じ強さでも効きが弱く、強さ 1.0 で 0.26。参照なしからは上がるが、顔を固定しきるものではない。顔を確実に揃えるなら LoRA を併用する。img2img の 2 方式（`chroma1-hd`・`flux1-dev-nsfw-master`）はワークフローの構造のテストのみで、GPU では未実行。
  `chroma1-hd` の img2img（同条件、プロンプトは同じ人物の特徴を書いたもの）は、出力と参照の平均画素差（0〜255）が、参照なし 72、強さ 0.2 → 38、0.4 → 20、0.7 → 5.4、1.0 → 3.7 と単調に縮まる。`flux1-dev-nsfw-master` は重みを取得しておらず、LoRA の差し込みも実ファイルでは試していない（ワークフローの構造のテストと偽の ComfyUI でのテストのみ）。

### 4.6 「顔だけ」の参照の固定（第 1 段。Issue #960）

編集モデル（`noctq-v4-qwen-image21`・`qwen-image-edit-2511`。`comfyui/stage` を通る `edit` のテンプレート）の役割「顔だけ」（`face`）は、顔検出モデルなしでも指示文と強さだけで動く。そこに、**追加のダウンロードなし**で次の 3 つを足した。**どれも効果は未検証**（実機で顔の類似度を測っていない。PR #953 の測定は、役割 `face`・強さ 1.0 で buffalo_l のコサイン類似度 0.26）。

| 手 | 中身 | 実装 |
|---|---|---|
| ① 参照を顔に切り出す | 役割 `face` の参照を、編集モデルに渡す前に、最も大きい顔のまわり（顔の枠の長辺の 2 倍の正方形。額・髪の生え際・耳・首を含み、服と背景は減る。額の分だけ中心を上へ 0.1 枠分ずらす）に切る。画像の端の顔は、正方形を画像の中へ寄せる（縮めない）。**切らない**場合（顔が無い・顔が 64 px 未満・正方形が画像の 85% 以上＝顔のアップ・検出器の失敗）は元の画像のまま渡し、失敗はログだけに残す。切るのは**役割 `face` を取るモデルへ渡す画像だけ**（全体の参照に落ちるモデルには元の画像のまま）。結果の履歴に残る入力画像は元のまま | `facesim.CropFace`（切り出し。`crop.go`）、`runner/facecrop.go`（`FaceCropFunc`。バッチの入力を読む時に 1 回だけ。モデルごとには切らない）、`app/gentest.go`（`faceCropper`） |
| ② 指示文を強める | 役割 `face` のときだけ、指示文 `roleSentence` が 3 文になる: 顔と identity だけを取り髪・服・ポーズ・背景は記述に従う（従来の文）／同じ人物であること・顔の形・目・鼻・口・眉・肌の色・見かけの年齢／別の顔や理想化した顔を描かず、その画像から他を取らないこと。他の役割の文は変えない。EditPlus のエンコーダーでは `<imageN>` が `Picture N` になるのは従来どおり（番号付けは `promptFor` が数える） | `comfyui/stage/stage.go` の `roleSentence` |
| ③ 既定の強さを上げる | 役割 `face` で強さを送らない参照の既定を 0.7 から **0.9** にする（`localcfg.DefaultFaceReferenceStrength`・`DefaultStrengthOf(role)`。サンプリングの最初の 90% に参照が入る）。利用者が指定した強さは変えない。他の役割は 0.7 のまま。画面のスライダーは役割が「顔だけ」のとき 0.9 から始まり（`facelock.DefaultStrengthOf`）、利用者がスライダーを動かすまでは役割の切り替えに追従する（`inputs.ts` の `data-touched`）。MCP は強さを省けばエンジン側の 0.9 になる | `localcfg/reference.go`、`organisms/gentest/refs_data.go`・`refrow.templ`、`static/src/components/gentest/inputs.ts` |

- **顔検出器**: Judge の顔照合と同じ `facesim.Engine`（YuNet、MIT、OpenCV Zoo。[model-adapters.md](model-adapters.md) §10.1）を使う。InsightFace（antelopev2・buffalo_l など）の重みは**非商用**なので、この経路には使わない。モデルは同梱せず利用者が配置する既存の仕組み（`scripts/fetch-face-models.sh`。コミット固定・SHA-256 検証。`--face-model-dir`）のままで、**新しい重みは足していない**ので、カタログの `data/assets` には載せていない。モデルまたは ONNX Runtime が無いときは、①だけが働かず（元の画像のまま）、②③は働く。
- **実モデルでの確認**（開発機、OpenCV のサンプル画像 `lena.jpg` 512×512）: 切り出しは 412×412 で、元の画像との SFace 類似度 0.93（`TestONNX_CropFaceKeepsTheFace`。環境変数は model-adapters.md §10.1 と同じ）。生成した画像での効果は測っていない。
- **画面の説明**: 入力画像の「?」に、「顔だけ」を編集モデルで使うと指示文と強さで寄せる（顔検出モデルがあるときは切り出して渡す）が、同じ顔の固定は弱めで、Qwen-Image-Edit-2511 の方が向いているかもしれない、と書く（`organisms/gentest/facelock.Help`。画像だけ。効果を測っていないことも書く）。img2img のモデルで「顔だけ」が全体の参照に落ちることの案内は #959 の実行前のモーダルで、この Issue の範囲ではない。
- **第 2 段**: `qwen-image-edit-2511`（Apache-2.0。#971）は上の ①〜③ がそのまま効く（`comfyui/facelocktest` が 2 つのテンプレートで確かめる）。PuLID-Flux-Chroma は**許可リスト（[custom-nodes.md](model-manager/custom-nodes.md) §2.1）に載せたが導入できない**（`install_blocked`）。理由は次の表。
- **効果の測り方**（実機）: 同じ参照・同じ prompt・同じ seed で、参照なし／全体の参照／役割 `face`（切り出しなし・あり）を作り、`facesim` の SFace（Apache-2.0。重みは配布元がライセンスを明記しているが、学習データの権利は §8 のとおり確かめられていない）で参照と出力の顔の類似度を測る。PR #953 の buffalo_l の数値と**同じ画像セットで併記**するため、手動のテスト `TestONNX_FaceLockPairs`（`facesim/pairs_test.go`。`MAM_FACESIM_PAIRS` に `<name>.ref.png`・`<name>.out.png`・任意の `<name>.old`）が、全体の参照の類似度・切り出した参照の類似度・旧指標の値・「他のペアの出力との平均」（別人の水準）を 1 つの表に出す。閾値（合否）はこの表を見てから決める（今は置かない）。SFace と buffalo_l の相関は未測定。

| 候補 | 採否 | 理由 |
|---|---|---|
| 顔検出: YuNet（既存の `facesim`） | **採用** | MIT。実装・配置・検証の仕組みが既にあり、新しい重みも配布物も増えない。Go から ONNX Runtime を `purego` で呼ぶので `CGO_ENABLED=0` のまま |
| 顔検出: InsightFace（SCRFD・antelopev2） | 不採用 | 学習済みの重みが非商用の研究用途のみ（手動でも自動でも同じ文言） |
| 顔検出: YOLO-Face | 不採用 | GPL-3.0 |
| ワークフローの `ImageCrop`（中央寄りの切り出し） | 不採用 | 顔の位置を知らずに切るので、顔が中央に無い写真で顔を外す。検出器が使えるのでその必要がない |
| UI で利用者が顔を囲む | 今回は見送り | 効果の見込みが立つ前に画面（トリミング UI）を作らない。自動の切り出しの効果を測ってから決める |
| 切り出しをワークフロー（ComfyUI）の中で行う | 不採用 | 顔検出のノードが追加ノードになり、同梱の Python に依存が無い（controlnet-aux と同じ理由。custom-nodes.md §5）。mam の側で切れば、ノードも依存も要らない |
| 切り出しをモデルごとに行う | 不採用 | 同じバッチの同じ画像を、モデルの数だけ検出し直す。バッチの入力を読む時に 1 回切って、役割 `face` を取るモデルへだけ渡す |
| 既定の強さ 0.9 | 採用 | 強さ 1.0 は役割 `face` でも最後の 1 ステップまで参照が入り、プロンプトの服・背景の指示を弱める恐れがある。顔は後半（細部）で決まるので 0.7 の「最後の 30% に参照が無い」を縮める。0.9 の値は**測っていない選択**で、実測の表で見直す |
| 強さの下限を設ける（`face` で 0.5 未満を引き上げる） | 不採用 | 利用者が選んだ強さを黙って変えない（0.1 を選んだ人に 0.5 を送ると意図に反する）。既定の引き上げだけにした |
| PuLID-Flux-Chroma（`pulid-flux-chroma`） | **許可リストに載せるだけ**（`install_blocked`） | ノードの依存（insightface・facexlib・onnxruntime・timm・ftfy・regex）が同梱の Python（`python.json`）に無く、mam は pip を実行しない。InsightFace の antelopev2 は非商用で、mam は同梱も自動の取得もしない。実行時に facexlib（GitHub のリリース）と EVA02-CLIP（Hugging Face）の重みを固定も検証もなしに取得する。ノードのコードのライセンスは LICENSE（Apache-2.0）と README（MIT）が食い違い `commercial: unknown`。PuLID の重み（guozinan/PuLID）はカードが apache-2.0。テンプレートは作らない（Chroma の `time_in` の問題を直した fork だが、同梱の Chroma1-HD での動作は未確認） |
| FaceNet 経路の PuLID（`lldacing/ComfyUI_PuLID_Flux_ll`） | 調べていない | InsightFace を避けられるが、同じく依存が同梱の Python に無く、重みの条件も未確認。導入できる見込みが立たない |
| SFace を埋め込みに追加 | **既に入っている** | `facesim` の埋め込みは最初から SFace（Apache-2.0）。重みを足す必要はなく、足したのは旧指標との併記の手動テストだけ |

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

### 4.7 一部だけ描き直す（`reference_mode: inpaint`。Issue #974）

`inpaint` は元画像 1 枚（`reference_image`）とマスク 1 枚（`mask_image`。白が描き直す、黒が残す）と `denoise`（0 より大きく 1 以下。テンプレートのリテラルが既定）を必須にする。組み込みは `inpaint-sdxl`（カタログ `sdxl-base` のワークフロー `sdxl-base-inpaint`）と `inpaint-flux-dev`（`flux1-dev` の `flux1-dev-inpaint`）。どちらもコアノードだけ（`LoadImage`・`ImageToMask` channel=red・`VAEEncode`・`SetLatentNoiseMask`・`KSampler`・`VAEDecode`・`ImageCompositeMasked`・`SaveImage`）。`ImageCompositeMasked` の `destination` は元画像、`source` はデコード結果、`mask` は `ImageToMask` なので、マスクの外は元の画素のまま残る（画素比較は GPU 未実施）。リクエストが `denoise` を省くとグラフの値を書き換えない。モデル情報の「インペイント対応」は `supports_inpaint`（この方式で denoise の既定が読めたとき）。起動時の更新（§4.2）は既存ワークフローの中身を揃えるだけで、新しいワークフロー名は足さない。インペイント用は、モデルを入れ直すと入る。

### 4.8 画面に出す値（`exposed_inputs`。Issue #977）

- **何か**: ワークフローの `exposed_inputs`（`localcfg/exposed.Input`）= グラフのノードの入力のうち、単発生成の「こだわり」が入力欄として出すもの。`{"node": "3", "input": "denoise", "type": "float", "label": "ノイズ", "min": 0, "max": 1}`。型は `int`・`float`・`string`・`enum`（`options`）だけ。**加算的**: `assignments`（mam の値の割り当て）は変えず、`exposed_inputs` が無いワークフロー（今のテンプレートの全部）は今と同じに動く。値の既定は、グラフが持つ値。
- **検査**（`ValidateModel`・`ValidateSaved`。`validateExposed`）: ノードと入力がグラフにある・ほかのノードがつないでいない（リンクは選べない）・`assignments` が使っていない・値の型に合う型・int と float は `min` と `max` の両方があり、グラフの値がその中・enum は 1 つ以上の選択肢にグラフの値が入る・表示名は 60 文字以内の 1 行・同じ入力を 2 回出さない・30 件まで。
- **グラフへの適用**（`Workflow.GraphWith`・`exposed.Apply`）: リクエストの値（`ImageOptions.Exposed`。キー `<node>.<input>`）を、型・範囲・選択肢で検査して、**割り当てを入れる前に**グラフへ入れる（`comfyui.Backend.build`）。範囲外・型違い・知らないキーは拒否。
- **一覧**（`Workflow.ExposedCaps`）: `modelcatalog.Info.Exposed`・MCP の `list_generation_models` が、キー・名前・型・範囲・選択肢と、グラフの値（`default`）を返す。
- **保存**: 保存したワークフローは `comfy_workflow.assignment_json` の `exposed_inputs`、接続先のモデルは `models.json` の `local_models[].workflow.exposed_inputs`。編集は ComfyUI の段の「画面に出す値を選ぶ」（`service/gentest/exposedpick`）、MCP の `exposed_inputs`、設定画面の保存（保存した値を変えない）。`workflows.Service.Update` は、`exposed_inputs` を持たない設定の保存では今の値を残す。`wfrefresh`（導入済みのコピーをテンプレートに合わせる更新）は、利用者が選んだ「画面に出す値」を残す（テンプレートが割り当てを足した入力は、割り当てと重ならないよう外す）。
- テンプレートには付けない（利用者が選ぶ）。画像だけ（動画のワークフローの値は単発生成の画像のフォームに出ない）。

### 4.9 単発生成の顔の類似度の自動計測（SFace。Issue #998）と #960 の監査

顔固定の効き目を印象でなく**数字で**見るため、単発生成の結果ごとに、参照の顔と出力の顔の類似度を自動で測って残す。#960 の「評価方法」のうち、**物差しを商用可能なもの（SFace）にそろえる部分**がこれ。実機で改善幅を測るのは含まない（下の「実機が必要」）。

- **いつ測るか**: 結果が画像で、バッチに役割 `face` の入力があるとき、結果の画像を保存した直後に 1 回（`runner.scoreFace`）。役割 `face` 以外（`reference`・`style` など）は、顔を保つ目的ではないので測らない。複数の `face` があるときは先頭の 1 枚。プロンプトマッチ（`imagematch`）の各回は入力を持たないので測らない（あちらは `match_face` で顔を点に混ぜる。[gentest/prompt-match.md](gentest/prompt-match.md)）。顔の入れ替え（[gentest/face-swap.md](gentest/face-swap.md)）の結果は、顔の画像が役割 `face` の入力なので測られる。
- **何と何を比べるか**: 参照は**アップロードされたままの画像**（「顔だけ」の切り出し（§4.6 ①）の前の画像。検出器はどちらでも最大の顔を取る）、出力はモデルが作った画像。それぞれの**いちばん大きい顔**の SFace の埋め込み（`facesim.Compare` → `facesim.Similarity`）のコサイン類似度（−1〜1）。顔の検出は YuNet、埋め込みは SFace で、`TestONNX_FaceLockPairs`（旧指標との併記）・Judge の顔照合・プロンプトマッチの顔の混ぜと**同じ `facesim.Engine`**（同じ ONNX Runtime・同じ 2 つのモデル）。
- **目安の閾値**: 0.363（`facesim.SameFaceThreshold`）。OpenCV Zoo の SFace の既定の閾値（`models/face_recognition_sface/sface.py` の `_threshold_cosine = 0.363`）。**目安であって保証ではない**（同じ人でも服・照明・角度で下がり、似た別人で上がる）。旧指標（buffalo_l）の「同一人物は 0.4〜0.5 以上」とは**数値の尺度が違う**ので、同じ画像で併記する（`TestONNX_FaceLockPairs`）まで、旧指標の数字と直接比べない。両指標の換算（相関）は**未検証**。
- **記録**: 結果の `params.face_similarity`（`record.FaceSimilarity`）: `metric`（`sface`）・`cosine`・`threshold`・`same_person`（`cosine >= threshold`）・`skipped`。測れなかったときは `skipped` に理由（`reference_no_face` = 参照に顔が見つからない / `result_no_face` = 出力に顔が見つからない（小さい・横向き・隠れている・入れ替えの失敗）/ `error` = 画像の読み取りか推論の失敗）を残す。**測れなくても結果は成功のまま**（画像は利用者のもの）。顔のモデル・ONNX Runtime が無いとき（`ErrUnavailable`）は、**役割 `face` の参照がある結果に `face_similarity` を必ず残す**（Issue #1026）: `skipped` は `models_missing`（YuNet か SFace の ONNX がモデルのフォルダに無い。`missing_files[]`・`model_dir`）か `runtime_unavailable`（モデルはあるが ONNX Runtime のライブラリが無い・読めない）。`reason`（なぜ測れないか）と `fix`（どのファイルをどこへ置くか）は日本語で、`apperr` のコード `face_models_missing`・`face_runtime_missing` の原因と直し方（[errors.md](errors.md)）。`cosine`・`same_person` は意味が無い（0・false）。役割 `face` の参照が無い結果には、オンでもオフでも付けない（測る相手が無いため。見分けは「`face_similarity` が無い = 顔の参照が無い」）。理由の見分け: `reference_no_face`・`result_no_face`・`error`（オンで測ろうとして測れなかった）と `models_missing`・`runtime_unavailable`（オフ）は別の値。ほかのキー（プロンプトマッチの `match` など）は残す。
- **オンかオフかを読む手段（Issue #1026）**: MCP の `list_generation_models` の `face_similarity`（`enabled`。オフのときは `off` に、結果に残るのと同じ `skipped`・`reason`・`fix`・`model_dir`・`missing_files`）。実行前の `estimate_generation_test` も、役割 `face` の参照があるリクエストには警告 `face_similarity_off` を返す（生成はでき、数値は付かない）。起動ログの WARN（`face identity similarity is off`）は、`reason`・`missing` を足した。オフの理由の診断は `facesim.ONNXConfig.Diagnose`（モデルのファイルの有無を見て、無ければ `models_missing`、揃っていれば `runtime_unavailable`）。
- **見え方**: 結果の詳細の「顔の類似度」の行（「0.512（SFace のコサイン類似度。同じ人と見なす目安 0.363 以上。…）」）、MCP は `get_generation_result`・`get_generation_test`・`list_generation_results` の `face_similarity`。履歴の一覧の札には出さない（詳細だけ）。
- **バッチのまとめ（Issue #998）**: 結果ごとの数字を、バッチのモデルごとにまとめて読める（`record.SummarizeFaces`。測れた数・測れなかった数・平均・最小・最大・目安以上の数と割合。測れなかった結果は平均に入れない）。MCP は `get_generation_test` の `face_summaries`、画面は比較表のモデルの行の下の 1 行。参照の強さ・モデル・プロンプトだけを変えた 2 つのバッチの**平均を並べて**、設定の効き目（#960 の第 1 段の改善幅）を数字で比べられる。GPU で実際に生成して改善幅を出すこと自体は、下の「実機が必要」のとおりまだ。意味は [gentest/gentest.md](gentest/gentest.md) §3.8。
- **モデルファイルの配布**（Issue #1026 で変更。それまでは利用者が `scripts/fetch-face-models.sh` で取得するだけで、カタログには載せていなかった）: YuNet（`face_detection_yunet_2023mar.onnx`・MIT）と SFace（`face_recognition_sface_2021dec.onnx`・Apache-2.0。2 つで約 39 MB、model-adapters.md §10.1）は、モデルマネージャーの区画「顔チェック用モデル」から**ボタン 1 つ**で `--face-model-dir`（既定 `<data-dir>/models`）に導入できる（[model-manager/model-manager.md](model-manager/model-manager.md) §5.4）。カタログの資産セット `onnx-face-models`（`modelmgr/catalog/data/assets/onnx-face-models.json`。[model-manager/local-runtime.md](model-manager/local-runtime.md) §12.11）が、opencv_zoo のコミット `47534e27…`（`scripts/fetch-face-models.sh` と同じ）の URL・SHA-256・サイズ・ライセンスの答えを持つ。**新しい重みは足していない**（これまで利用者が取得していたのと同じ 2 ファイル）。ダウンロードの前に取得元・大きさ・ライセンス（SFace の学習データの注意を含む）を画面に出し、同意のチェックが要る（MCP は `install_face_models` の `accepted`）。スクリプトでの取得・手で置く方法も使える（置いたファイルは「SHA-256 は未確認」と出て、導入ボタンが確認だけをする）。**導入後に mam を再起動する必要は無い**: 顔エンジンは初めて使うときに読み込む（`facesim/holder`）ので、次の計測から数値が付く。モデルが無い間は、結果の `face_similarity.skipped: models_missing` の `fix` が「モデルマネージャーの『顔チェック用モデル』から導入」を指し、入力画像の欄に同じリンクを出す。ONNX Runtime のライブラリが無いのは別の理由（`runtime_unavailable`）で、モデルを入れても直らない（直し方はライブラリを用意すること。`fix` もそう書く）。**未確認**: 実機（Windows）で、モデルを入れたあとに `face_similarity` の数値が付くこと（#998 の実測そのもの）と、実物の GitHub（LFS の転送）からのダウンロード、ONNX を読み込んだ状態での削除（ファイルのロック）。
- **ライセンス（一次情報。2026-11-03 に再確認）**: SFace の重みは OpenCV Zoo の `models/face_recognition_sface` で **Apache-2.0**（[LICENSE](https://github.com/opencv/opencv_zoo/blob/main/models/face_recognition_sface/LICENSE)。[README](https://github.com/opencv/opencv_zoo/blob/main/models/face_recognition_sface/README.md) は、MobileFaceNet を SFace の損失で学習したもので、Yaoyao Zhong の寄稿、ONNX への変換は Chengrui Wang、と書く）。閾値の出典は [sface.py](https://github.com/opencv/opencv_zoo/blob/main/models/face_recognition_sface/sface.py)。**学習データの権利は README にも書かれておらず、一次情報では確かめられない**（[model-adapters.md](model-adapters.md) §8 のとおり）。そのため、重みを配るのではなく利用者が自分で取得する今の形を変えず、自動計測は**結果を作り変えず、点にも混ぜない**（診断の数字を残すだけ）。YuNet は MIT（[LICENSE](https://github.com/opencv/opencv_zoo/blob/main/models/face_detection_yunet/LICENSE)）。
- **テスト**: `facesim.Compare` と閾値は単体（偽のエンジン）。結果ごとの計測（測る・測れない 3 種・顔の役割が無い・モデルが無い）は `service/gentest/face/score_test.go`。実モデルでは `TestONNX_CompareOfFilesSeparatesTheSamePersonFromAnotherAndNamesNoFace`（`MAM_FACESIM_MODEL_DIR` と `MAM_FACESIM_PHOTOS` が無いと Skip する既存の慣習。CI では動かさない）。

**#960 の監査（2026-11-03）**。第 1 段（PR #989）以外の項目を、この時点の実装と照らした。

| #960 の項目 | 状態 | 備考 |
|---|---|---|
| 緩和 1: 参照を顔付近に自動クロップ | 済（#989） | §4.6 ① |
| 緩和 2: 指示文の強化 | 済（#989） | §4.6 ② |
| 緩和 3: strength の見直し | 一部済（#989: 既定 0.9） | 下限を設けることは不採用（§4.6 の表）。0.9 が良いかは**未検証** |
| 緩和 4: 二段目（顔マスクの弱い img2img / inpaint） | **未着手**（残り） | 部品はある（YuNet の顔の枠 + 一部描き直し §4.7 の inpaint）が、枠 → マスク → 低 denoise の再サンプリングの 2 段のジョブと画面を足す設計になり、効果の見込みも GPU での実測が先に要る。**安くない**ので今回は入れない |
| 緩和 5: UI の案内 | 済（#989 / #959） | `facelock.Help` と実行前のモーダル |
| 評価方法: コサイン類似度（検出 → クロップ → 埋め込み） | **済（#998）** | 上のとおり、結果ごとに自動 |
| 評価方法: 商用可能な埋め込み（SFace） | **済**（#989 の手動テスト + #998） | 学習データの権利は未確認（§8） |
| 評価方法: 旧指標との併記 | 済（手動。#989） | `TestONNX_FaceLockPairs`。換算（相関）は**未検証** |
| 評価方法: 3 プロンプト × 複数 seed、目標値の決定 | **残り（実機が要る）** | GPU で生成してから閾値を決める（Issue 本文どおり、いまは仮置きしない） |
| 第 2 段: `qwen-image-edit-2511`（Apache-2.0） | 済（#971） | §4.6 |
| 第 2 段: PuLID-Flux-Chroma | 許可リストに載せたが導入不可（`install_blocked`） | 導入の経路は別の作業（カスタムノード）の領域 |
| 顔入れ替え（後処理） | 外部ツール FaceFusion として実装（#995） | [gentest/face-swap.md](gentest/face-swap.md)。拡散モデルでの入れ替え（Qwen Edit の二段）は未着手 |
| 未検証（全項目） | 残り | 改善幅の数値、macOS、Chroma + PuLID、各 LoRA のライセンスなど。**どれも実機が必要**で、#998 の計測が揃ってから測る |

**実機が必要（今回できないこと）**: ① 第 1 段（切り出し・指示文・既定 0.9）の**改善幅**を、同じ参照・prompt・seed で生成して SFace の数字で示すこと。計測の仕組み（結果ごとの数字、`TestONNX_FaceLockPairs` の表）は入ったので、GPU で生成すれば出せる。② SFace と旧指標（buffalo_l）の相関。③ 実モデルのテストの CI への組み込み（写真・モデルを置けない）。

### 4.10 動画を大きくする・滑らかにするワークフロー（`video_enhance`。Issue #1031）

- **印**: ワークフローの `video_enhance`（`{label, op, max_output_pixels}`。`localcfg/videoenhance.Spec`。`upscale`（§4.5）と同じく下位パッケージに置いた）は、そのワークフローが**動画を作るのではなく、渡した動画を大きくする（`op: upscale`）か滑らかにする（`op: interpolate`）**ことを表す。`media: video` の印として持つので、接続先・導入・版の仕組みはそのまま使う。`max_output_pixels` は 1 コマの最大の画素数（0 は上限なし）。
- **割り当て（動画のワークフローだけ。印の無いワークフローには割り当てられない）**: `source_video`（アップロードした動画のファイル名。`LoadVideo.file`。ちょうど 1 つで、グラフに `LoadVideo` が要る）。`op: upscale` は `video_scale`（`ResizeImageMaskNode` の倍率。ちょうど 1 つ）、`op: interpolate` は `frame_multiplier`（`FrameInterpolate.multiplier`。整数）と `output_fps`（`CreateVideo.fps`）をちょうど 1 つずつ。プロンプトは要らない。`video.durations`・`video.resolutions` も要らない（長さと大きさは元の動画のもの。`ValidateModel`・`ValidateSaved` は印のあるワークフローには求めない）。検査は `localcfg.validateVideoEnhance` → `videoenhance.Validate`。
- **一覧**: `IsUpscale()` と同じく、`Model.IsVideoEnhance()` のワークフローは `localcfg.IDs`（画像・動画のモデルの識別子）・単発生成のモデルの一覧には**出ない**（プロンプトを渡すモデルではない）。
- **実行（`modeladapter`）**: `enhancevideo.Run(ctx, adapter, model, enhancevideo.Request{Source, Scale, Multiplier, OutputFPS})`（パッケージ `modeladapter/enhancevideo`。`modeladapter` 本体は行数上限のため増やさない） が ComfyUI の `comfyui.Backend.EnhanceVideo` を呼ぶ（代替モデルへの切り替えはしない。印の無いモデルは `enhancevideo.ErrNotEnhance`）。元の動画は `POST /upload/image` に**コンテナに合う拡張子**（`video/mp4` は `.mp4`、`video/webm` は `.webm`、`video/quicktime` は `.mov`、`video/x-matroska` は `.mkv`。`LoadVideo` が入力ディレクトリのファイルを内容の種類で選ぶため）でアップロードし、返った名前を `source_video` に入れ、依頼の値（倍率・整数倍・fps）を割り当てへ入れて実行し、`SaveVideo` の動画を取って返す（上限 `localhttp.MaxFileBytes`＝300 MB）。依頼の値が足りない（倍率が 1 以下・整数倍が 2〜16 の外・出力 fps が無い・元の動画が空）ときは、アップロードの前に断る。ジョブが走り出したあとの失敗は `AfterSubmit`（同じ動画を二重に走らせない）。
- **テンプレートとカタログ**: `templates/videoenhance/`（`video-upscale-seedvr2-3b`・`video-interpolate-rife-v4.25`・`-v4.26`）。コアのノード（`LoadVideo`・`GetVideoComponents`・`ResizeImageMaskNode`・`SeedVR2Preprocess`・`SeedVR2Conditioning`・`SeedVR2TemporalChunk`・`SeedVR2TemporalMerge`・`SeedVR2PostProcessing`・`FrameInterpolationModelLoader`・`FrameInterpolate` など）だけで、ComfyUI v0.42.1 の `nodes.py` が読み込む `comfy_extras` のソースで node_id を確かめた。モデルは [model-manager/model-manager.md](model-manager/model-manager.md) §4.7。**GPU では動かしていない**（実機未確認。値の差し込みと構造のテストまで）。

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
