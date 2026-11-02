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
    "gpu-box": {"kind": "comfyui", "base_url": "http://100.101.102.103:8188", "max_concurrency": 1, "timeout_seconds": 3600},
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
| `endpoints.<名>` | `kind`、`base_url`、`max_concurrency`、`timeout_seconds` | 同時実行 1（GPU は通常 1 ジョブ）。タイムアウトは ComfyUI 3600 秒・互換 900 秒（上限 24 時間）。`base_url` は http(s) のホストで、資格情報・クエリ・フラグメントを含まず、リンクローカル・未指定・マルチキャストの IP でない |
| `local_models.<接続先>/<名>` | `media`（image/video）。接続先が存在すること。`comfyui` は `workflow`、`openai-compat` は `model`（サーバーに渡すモデル ID）が必要 | `openai-compat` の動画は拒否 |
| `workflow` | `graph`（API 形式）、`assignments`、`output_node`、`fps`（既定 16）、`frames_step`（正なら総フレーム数を `n×step+1` にする。Wan は 4、LTX は 8）、`size_multiple`（既定 16）、画像の参照用に `reference_mode`・`no_reference`・`lora_model_node`（§4.2） | 割り当てが指すノード・入力が `graph` に無い、`output_node` が無い、プロンプトの割り当てが無い、は 422 |
| `video` | `durations`（秒、昇順）、`resolutions`（短辺で `480p`・`720p`・`1080p` など）、`native_audio` | 動画モデルは必須。最初と最後のフレーム・参照画像の枚数は `assignments` から決まる |
| 価格 | `cost_per_image_usd`、`cost_per_second_usd` | 既定 0（参考値。見積コストにのみ使う） |
| 用途・Policy | `images.<purpose>`・`policies.<policy>` の `model` に識別子を書く（既存と同じ形式） | 参照先が `local_models` に無ければ起動時エラー。動画の対象は価格を要求しない |

`--model-config` で固定したファイルは読み取り専用（接続先・モデルの保存は `ErrPinned`）。API キーだけは保存できる。

## 4. ComfyUI

- **割り当て表（`assignments`）**: `param` の値をノード `node` の入力 `input` に入れる。使える `param`: `prompt`、`negative_prompt`、`seed`（実行ごとに乱数）、`width`、`height`、`frames`、`fps`、`reference_image`（`index` で何枚目か。`optional: true` なら、その枚数が無い実行ではノードごと外れる）、`reference_denoise`（画像のみ。§4.2）、`first_frame`、`last_frame`。画像は `frames`・`fps`・`first_frame`・`last_frame` を使えない。幅・高さは縦横比と解像度から決め、`size_multiple` の倍数に丸める。
- **入力画像**: 参照画像・最初／最後のフレームは `POST /upload/image` に**元の拡張子のまま**アップロードし、返った名前を入力に入れる。
- **実行**: `/ws?clientId=…` を開いてから `POST /prompt`（`prompt_id` と `client_id` は mam が決める）。`/ws` の `progress`・`executing` から割合を求め、`/history/<id>` を確認して完了を待つ。結果は `GET /view` で取る（動画は `.mp4`・`.m4v`・`.webm`・`.mov` のみ。それ以外は投入後のエラー）。
- **進捗**: 割合は Shot の進捗（`/ws/shots/:id`、チャットの進捗カード、Take カード）のメッセージに「動画を生成しています（42%）」の形で入る。モデル呼び出しの文脈に進捗の関数を持たせ（`modeladapter/progress`）、`generation/relay` が Orchestrator の通知（Take 数など）のメッセージに割合を足して配信する（`Progress` の型は変えない）。
- **取り消し**: Take の生成を止めたときは、**自分の `prompt_id` だけ**を止める。まず `POST /api/jobs/<id>/cancel`、404 なら `GET /queue` で見て、実行中なら `POST /interrupt {"prompt_id"}`、待機中なら `POST /queue {"delete":[id]}`。対象を指さない割り込み・キューの全消しはしない。接続テストは ComfyUI が 0.3.57 未満だと「取り消しが他のジョブを止めうる」と注意を出す。
- **組み込みテンプレート**: `localcfg/templates/image/*.json`・`localcfg/templates/video/*.json`（コアノードだけ。カスタムノード不要）。`txt2img-sdxl`、`img2img-sdxl`、`txt2video-wan22-5b`、`img2video-wan22-5b`、LTX-2.5 の `txt2video-ltx-2-5`・`img2video-ltx-2-5`（§4.1）ほか（一覧は model-manager.md §4.2）。モデルファイル名（チェックポイント）は接続先にあるものへ直す。**GPU では実行していない**（開発環境に GPU が無い）。構造だけをテストする（割り当てが指すノード・入力と出力ノードの存在）。実機での検証は #833 で対象モデルを決めるときに行う。
- **取り込み**: 利用者の「Save (API Format)」の JSON を取り込める。エディタ形式（`nodes`/`links`）は拒否してヒントを返す。ノードと入力の一覧を返すので、割り当てを利用者が決める。

### 4.2 画像モデルの参照画像・強さ・役割・LoRA（#933）
画面と MCP は、どのモデルでも同じ「強さ」（0〜1、既定 0.7）と「役割」を渡す。ワークフローの作りの違いは `localcfg`・`comfyui` が吸収する。`reference_mode` が方式を決める（`localcfg.Model.ReferenceCaps()` が方式・上限枚数・選べる役割を返し、画面と `accepts_references`・`max_reference_images` の元になる）。

| `reference_mode` | モデル | 枚数 | 役割 | 強さ s の効き方 |
|---|---|---|---|---|
| `img2img` | `chroma1-hd`（`txt2img-chroma`）、`flux1-dev-nsfw-master`（`txt2img-flux-dev-lora`） | 1 | 全体のみ | 参照を VAE で潜在にして途中から生成する。`reference_denoise` の割り当てに `1 − (1 − floor)×s` を入れる（`floor` は `reference_denoise_min`。既定 0、`chroma1-hd` は 0.6。Chroma の schedule は denoise 0.6 未満では絵がほとんど変わらず 0.95 で参照が消えるため、強さの範囲を変化のある区間に広げる。下限は 0.02）。参照が無い実行は `no_reference`（空の潜在に戻す）を当てて参照のノードを外し、テキストから生成と同じになる |
| `edit` | `noctq-v4-qwen-image21`（`txt2img-qwen-image21`） | 最大 10（`reference_image` の `optional` スロット） | 全体・顔だけ・画風だけ・編集対象 | 参照は `TextEncodeQwenImage21` の `image_1`〜に入る（プロンプトの `<image1>` が 1 枚目）。強さ s の参照は、サンプリングの最初の `round(s×steps)` ステップ（最低 1）だけ条件に入り、残りはプロンプトだけで仕上げる。強さが違う参照ごとにサンプリングを区切り（最大 6 区間）、各区間を別のエンコーダー・`SamplerCustomAdvanced` で回す。役割は文で伝え（顔だけ: 「顔と同一性だけを保つ」、画風だけ: 「画風・色・雰囲気だけ」）、顔・画風は VAE を渡さない（ビジョンだけで読む） |

- **LoRA**: `lora_model_node`（モデルを出力するノード）がある画像ワークフローだけが受ける。リクエストの LoRA は、そのノードの出力 0 を使うノードの手前に `LoraLoaderModelOnly` を差し込む（強さ既定 0.4）。受けないモデルでは警告 `lora_unsupported` を出し、送らない。
- **インストール済みのモデルの更新**: モデルマネージャーはモデルの導入時にだけテンプレートを `local_models` へ写す。この対応より前に導入したモデルは参照を使えない古い写しのままなので、mam の起動時（`app` が `wfrefresh.Run` を呼ぶ。接続先の登録のあと）に、カタログのワークフロー名と同じ名前の保存済みモデルで、`reference_mode` が無く、テンプレートには有るものを、テンプレートの内容に置き換える（利用者が付けた価格は残す）。利用者がグラフを書き換えたモデル、`--model-config` で固定したファイル、テンプレートが参照に対応しないモデル、すでに最新のモデルは変えない。
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
