# モデルマネージャー（ComfyUI エンジンの取得・起動とモデルの導入）

[local-models.md](../local-models.md)（利用者が登録した ComfyUI・OpenAI 互換の接続先）に、**MAM が自分で ComfyUI を取得・起動し、カタログのモデルを入れる**経路を足す。実装は `backend/internal/modelmgr/*`（部品）と `backend/internal/service/modelmanager`（Service）。画面（[../../api/endpoints-model-manager.md](../../api/endpoints-model-manager.md)）・MCP ツール（[../../api/mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)）はこの Service API の上に載る（§12）。インストーラーは変わらない（desktop.md §4・§6.8）。改訂履歴は [model-manager.changelog.md](model-manager.changelog.md)。

## 1. 方針（変えない前提）

- **ComfyUI はインストーラーにも Release にも同梱しない**（サイズと自動更新の上限のため）。MAM が、GPU に合う公式のポータブル版（7z）を実行時に取得する。ComfyUI は GPL-3.0 の別プログラムで、MAM は配布しない（`THIRD_PARTY_NOTICES.md`）。
- **置き場所は `<mam.exe のあるディレクトリ>\comfyui` だけ**（§2）。インストール版ならアンインストーラーがインストール先ごと消し、ポータブル版は exe の隣に残る。それ以外に永続する場所を作らない。書けない場所なら明確なエラーで、外部 URL の利用を案内する。
- **取得物は固定（ピン留め）して SHA-256 を検証する**: エンジンもモデルも、コミットまたはタグ・SHA-256・サイズをカタログ（埋め込みの JSON）に持つ。ハッシュが合わないものは使わず、消す。
- **Windows 版だけが同梱エンジンを持つ**。ブラウザ版・サーバー版（非 Windows）では同梱エンジンは使えず（`ErrUnsupported`）、外部の ComfyUI を接続先 URL（local-models.md）で使う。
- **外部の ComfyUI は読むだけ**: インストール済みモデルの一覧（`GET /models/{folder}`。`checkpoints`・`diffusion_models`・`text_encoders`・`vae`・`loras`）と、カタログと照らした「足りない」表示までで、リモートへの導入はしない（ComfyUI にモデルを置く API が無い）。年齢確認が済むまで、成人向けのモデルは照合の結果に出ない（§6.1）。
- 起動した ComfyUI 以外（ComfyUI Desktop など）は **起動も停止もしない**。

## 2. ディレクトリ

```
<mam.exe のディレクトリ>\comfyui\
  engine\        ComfyUI ポータブル版を展開したもの（python_embeded\ と ComfyUI\）。更新はこのディレクトリごと入れ替える
  data\          ComfyUI の --base-directory（models\・output\・user\ 等）。エンジンの更新で消えない
  downloads\     *.part と取得したアーカイブ（再開用）。完了・失敗で片づける
  logs\          engine.log（起動の失敗で末尾を表示する）
  state.json     導入状態（§7）
```

- ジョブの結果（試し生成）は MAM のデータディレクトリ（`<data-dir>/gentest/<バッチ ID>/`）に置く。`comfyui` の中ではない（アンインストールでもデータは残す、という既存の方針）。
- `store.Layout.CheckWritable` が作成と書き込みを試し、できなければ `ErrNotWritable`（`EngineView.Writable=false`）。
- `store.ForExecutable(exe)` が exe の実体（シンボリックリンクは解決）から `Layout` を作る。

## 3. エンジン

### 3.1 カタログ（`catalog/data/engine.json`）

ComfyUI **v0.42.1** のリリースアセット 4 種（`ComfyUI_windows_portable_nvidia.7z`＝cu130、`..._nvidia_cu126.7z`、`..._amd.7z`、`..._intel.7z`）の URL・SHA-256（GitHub Releases API の `digest`）・サイズ・展開後サイズ（実測）。起動は `python_embeded\python.exe -s ComfyUI\main.py ...`。

### 3.2 GPU の判定（`modelmgr/gpu`）

| 項目 | 内容 |
|---|---|
| 取得 | Windows は `dxgi.dll` を純 Go（syscall）で呼び、アダプター名・ベンダー ID（10DE=NVIDIA、1002=AMD、8086=Intel）・`DedicatedVideoMemory` を得る。他の OS は空（`Detector` インターフェース＋ビルドタグ。単体テストは偽物） |
| NVIDIA | `nvidia-smi --query-gpu=compute_cap` があればそれで、無ければアダプター名で世代を推定。compute capability 7.5 未満は `nvidia_cu126`、それ以上は `nvidia`（cu130）。名前が不明なら低い確度で `nvidia` |
| AMD | VRAM 4 GiB 以上で `amd` |
| Intel | 名前に「Arc」を含むものだけ `intel`（内蔵 GPU は対象外） |
| 対象外 | VRAM 4 GiB 未満・ソフトウェアアダプターは使わない。対応する GPU が無ければ変種は空で、**自動では何も入れない**（案内を出し、外部 URL を勧める）。CPU 実行は利用者が明示したときだけ（`EngineInstallInput.CPU`。nvidia 版を入れて `--cpu` で起動） |

### 3.3 取得・展開（`modelmgr/fetch`、`extract`、`engine`）

1. 空き容量を確認する（アーカイブの未取得分＋展開後サイズ。足りなければ `NoSpaceError`）。
2. `downloads\<名前>.part` へ取得する。あれば `Range` で続きから（サーバーが 200 を返す・416 のときは最初から）。アイドルが続けば中断する。
3. SHA-256 とサイズを検証してからリネームする。合わなければ `.part` を消してエラー（`ErrChecksum`・`ErrSize`）。
4. 7z を `engine.new\` へ展開し（`extract.Extractor`、実装は `bodgit/sevenzip`）、`engine\` と入れ替える（既存は `engine.old\` に退避し、成功で消す。失敗で戻す）。`..`・絶対パス・ドライブ指定・シンボリックリンクのエントリは拒否する。
5. 失敗・取り消しでは `engine.new\`・`.part`・退避を片づける。起動時にも前回の取りこぼしを消す（`CleanLeftovers`）。
6. 進捗（バイト・割合・メッセージ）は `track` に載せ、`JobStatus` と `Watch` で読める。

取得できるホストは `huggingface.co`・`hf.co`・`github.com`・`githubusercontent.com` だけで、リダイレクト先も同じ規則で調べる（`fetch.AllowedURL`。エンジンのアーカイブとカタログのモデルの取得）。**Hugging Face の導入経路（`hf` のクライアントと導入したファイルの取得）は `huggingface.co` と `*.hf.co` だけ**（`fetch.HFAllowedURL`。GitHub へのリダイレクトは断る。[model-manager-hf.md](model-manager-hf.md) §2）。`Authorization` は最初の要求の宛先ホストにだけ付け、ホストが変わると外す（§5.3）。

**7z の方式**: 公式パッケージは LZMA2＋BCJ2（`-m0=lzma2 -mf=BCJ2 -md=768m`）。`bodgit/sevenzip` v1.6.0 が同じ方式で作った合成アーカイブ（`extract/testdata/bcj2.7z`、1.5 GB のものでも）を読めることを確かめた。展開時の最大メモリは 1.5 GB のアーカイブで約 1.6 GB。実物の約 2 GB を丸ごと取得・展開した検証は未実施（§11）。

### 3.4 プロセス（`modelmgr/proc`）

起動引数: `--listen 127.0.0.1 --port <空きポート> --disable-auto-launch --disable-all-custom-nodes --base-directory <data>`（`--cpu` は CPU を選んだときだけ）。`--windows-standalone-build` は付けない（v0.42.1 ではブラウザを開き assets API を有効にするだけのため）。

| 項目 | 内容 |
|---|---|
| ポート | 前回のポート（`state.json` の `LastPort`）を優先し、塞がっていれば空きを選ぶ |
| 起動 | 既定は**遅延**（そのエンジンのモデルを初めて呼ぶとき、`Prepare`）。設定 `AutoStart` で MAM の起動時に開始（`OnStartup`）。待つのは `/system_stats` が応答するまで（タイムアウトあり） |
| ウィンドウ | Windows ではコンソールを出さない（`HideWindow`＋`CREATE_NO_WINDOW`、`internal/media/run_windows.go` と同じ方法） |
| 終了 | MAM の終了で止める（`Close`）。MAM が異常終了しても子が残らないよう、Windows は Job Object（`KILL_ON_JOB_CLOSE`）、Unix はプロセスグループと `Pdeathsig` |
| 状態 | `stopped` / `starting` / `running` / `failed`。起動の失敗・異常終了は `failed` とログの末尾（エラー文）で表し、`Watch` のイベントで通知する（画面・チャットへの出力は別作業） |

### 3.5 管理される接続先 `bundled-comfyui`

エンジンを入れるとき、`localmodels` に kind `comfyui` の接続先 `bundled-comfyui`（`http://127.0.0.1:<port>`）を作り、モデルの導入・削除でその `local_models`（local-models.md §2。識別子は `comfyui:bundled-comfyui/<名前>`）を作る・消す。接続先の URL はエンジンのポートに従う。

- 起動したポートが接続先と違うときは、`Prepare` が URL を更新して `ErrEndpointMoved` を返す（呼び出し中のバックエンドは古い URL を持つため。もう一度実行すると新しいポートに届く）。試し生成は `Prepare` を通らず、自分でエンジンを起動して最新の設定を読むので影響を受けない。
- 呼び出しの前処理 `localbackends.Options.Prepare` が、`Adapter` の画像・動画バックエンドに接続先ごとに挟まる（エンジンが止まっていれば起動する）。`--model-config` で固定した `models.json` では接続先を書けないので、導入は `ErrPinned`。

### 3.6 既存の ComfyUI の検出（`modelmgr/detect`）

`127.0.0.1:8188`・`:8000` の `/system_stats`（`system.comfyui_version` を持つ JSON）を調べる。`%APPDATA%\ComfyUI\config.json`（ComfyUI Desktop）は**ヒントにだけ**使う。ポート 8000 で見つかったものは ComfyUI Desktop として**外部 URL**に使い、MAM は起動も停止もしない。`Report{Found, Desktop, Recommended}`。

## 4. モデルのカタログ（`catalog/data/models/*.json`）

### 4.1 スキーマ

| フィールド | 内容 |
|---|---|
| `id` / `name` / `kind` / `tier` / `description` | `kind` は `image`・`video`。`tier` は `standard` か `advanced`（同意が要る） |
| `content_rating` | `general` か `adult`。`adult` は年齢確認（§6.2）が済むまで一覧・導入・試し生成に出ない。**`adult` は `tier = advanced`（同意必須）** |
| `trigger_words[]` | LoRA を含むモデルのトリガー語（プロンプトに入れる語）。`loras` のファイルを持つモデルだけ |
| `files[]` | `name`・`folder`（`checkpoints`・`diffusion_models`・`text_encoders`・`vae`・`loras`）・`url`（**コミット固定の** Hugging Face の URL、https）・`sha256`・`size`・`source_note`（第三者のミラーのとき: 原本の場所と、SHA-256 が一致することを確かめた日付。省略可） |
| `vram` | `min_gb`（目安）・`source`（出典。無いものは保守的な値）・`estimate`（true は MAM の推定で GPU では未確認） |
| `license` | `id`・`version`・`name`・`url`・`summary`・`commercial_use`・`commercial_condition`・`excluded_regions`・`notice` |
| `consent_required` | true のモデルは、同意の記録が無いと導入のジョブを作れない（§6） |
| `requires_hf_token` | 既定のカタログはすべて false（ログイン不要の `Comfy-Org` ミラー） |
| `workflows[]` | `name`（`local_models` のワークフロー名）と `template`（`localcfg` のテンプレート。local-models.md §4） |

### 4.2 v1 のモデル

| ID | 種別 | 区分 | 合計サイズ | VRAM 目安 | ライセンス |
|---|---|---|---|---|---|
| `sdxl-base` | 画像 | 標準 | 6.9 GB | 8 GB（推定） | CreativeML OpenRAIL++-M |
| `flux1-schnell` | 画像 | 標準 | 17.2 GB | 16 GB（推定） | Apache-2.0 |
| `qwen-image` | 画像 | 標準 | 30.1 GB | 24 GB（推定） | Apache-2.0 |
| `wan22-ti2v-5b` | 動画 | 標準 | 18.1 GB | 24 GB（Wan2.2 の README: 4090 級で動作） | Apache-2.0 |
| `ltx-video-2b` | 動画 | 標準 | 11.2 GB | 12 GB（推定） | LTX-Video Open RAIL-M |
| `flux1-dev` | 画像 | 同意 | 17.2 GB | 16 GB（推定） | FLUX.1 \[dev\] Non-Commercial（商用不可） |
| `sd35-medium` | 画像 | 同意 | 11.6 GB | 12 GB（推定） | Stability AI Community（年間売上 100 万 US ドル未満は無償） |
| `hunyuanvideo15-480p` | 動画 | 同意 | 20.7 GB | 24 GB（Hunyuan の文書） | Tencent Hunyuan Community（EU・英国・韓国では無効。月間 1 億人超は別途許諾） |

**成人向け・LoRA の追加（Issue #841）**。VRAM はすべて推定（`estimate=true`。出典は Issue の付録の Notion の記載または MAM の推定で、GPU では未検証）。合計サイズは共有ファイルを 1 回だけ数えたものではなく、そのモデルのファイルの合計。

| ID | 種別 | 区分 | 合計サイズ | VRAM 目安 | ライセンス（商用） | 備考 |
|---|---|---|---|---|---|---|
| `wan22-i2v-14b` | 動画 | 標準 | 35.6 GB | 16 GB（推定） | Apache-2.0（可） | 公式のベース（I2V 14B fp8、高・低ノイズ）。umt5 は `wan22-ti2v-5b` と、VAE `wan_2.1_vae` は下の 2 つと共有 |
| `noctq-v4-qwen-image21` | 画像 | 成人向け | 17.3 GB | 16 GB（推定） | Qwen Research License（**不可**） | Noct Q V4（int8_convrot）＋共有の Qwen3-VL 8B テキストエンコーダー・VAE |
| `chroma1-hd` | 画像 | 成人向け | 14.7 GB | 12 GB（推定） | Apache-2.0（可。ただし成人向けなので同意必須） | fp8mixed ＋ T5-XXL fp8 ＋ FLUX の `ae` |
| `flux1-dev-nsfw-master` | 画像 | 成人向け | 17.4 GB（導入済みの `flux1-dev` があれば LoRA の 0.17 GB だけ取得） | 16 GB（推定） | FLUX.1 \[dev\] 非商用（**不可**） | `flux1-dev-fp8` を `flux1-dev` と共有＋ LoRA NSFW MASTER（`loras`、ミラー）。トリガー語 `NSFW`・`Nude` |
| `wan22-remix-nsfw-i2v` | 動画 | 成人向け | 35.6 GB | 16 GB（推定） | ライセンスの記載なし（`unspecified`。**不可扱い**） | Remix I2V v3.0 高・低（ミラー）＋共有の TE・VAE |
| `wan22-i2v-14b-nsfw-lora` | 動画 | 成人向け | 36.8 GB（導入済みの `wan22-i2v-14b` があれば LoRA の 1.2 GB だけ取得） | 16 GB（推定） | ライセンスの記載なし（`unspecified`。**不可扱い**） | `wan22-i2v-14b` のファイル（共有）＋ NSFW-22-H/L-e8（`loras`、ミラー）。トリガー語 `nsfwsks` |

Wan 2.2 14B は T2V を入れない（I2V のみ。決定 4）。ミラーのファイル（NSFW MASTER・Remix・Wan NSFW LoRA）は、Hugging Face のミラーの SHA-256 が Civitai の原本と一致することを 2026-10-25 に確かめた（`source_note`）。Remix と Wan NSFW LoRA は HF にライセンスの本文が無く、Civitai の表示（販売まで可／Civitai 上の利用だけ）に頼らず、商用不可として入れる。除外したもの: LTX-2.3 Uncensored（テキストエンコーダーが GGUF で `ComfyUI-GGUF` が要る）・WAI-NSFW-Illustrious-SDXL（Civitai だけ）・abenzerps UC。
ワークフローは `localcfg/templates/` の **コアノード（ComfyUI 本体のノード）だけ**で書く。SDXL と Wan 2.2 は既存のテンプレートを使い、FLUX schnell/dev・SD3.5・Qwen-Image・LTX・HunyuanVideo のテンプレートを足した（`txt2img-flux-schnell` など 7 つ）。成人向けの追加で、`txt2img-qwen-image21`（`TextEncodeQwenImage21`、25 ステップ・euler/simple・cfg 3）・`txt2img-chroma`（`CLIPLoader type=chroma`、`T5TokenizerOptions`、`BetaSamplingScheduler`、`CFGGuider` cfg 3.8）・`txt2img-flux-dev-lora`（`LoraLoaderModelOnly`、強さ 0.8）・`img2video-wan22-14b`（高ノイズ→低ノイズの 2 段の `KSamplerAdvanced`、`WanImageToVideo`、16fps）・`img2video-wan22-14b-remix`（同じ形、12 ステップ・cfg 1）・`img2video-wan22-14b-lora`（高・低のそれぞれに `LoraLoaderModelOnly`、強さ 1）の 6 つを足した（合計 17）。`catalog.Validate` は、各テンプレートのローダーノード（`CheckpointLoaderSimple`・`UNETLoader`・`CLIPLoader`・`DualCLIPLoader`・`VAELoader`・`LoraLoaderModelOnly`）が、そのモデルのファイル名（と `loras` などのフォルダ）と一致することを確かめる。**テンプレートは構造テストだけで、GPU 実機では未検証**（画面にも「構造テストのみ・GPU 実機未検証」と出す。`ModelView.WorkflowCheck`）。

### 4.3 検証（`catalog_test.go`）

すべてのファイルに https の固定 URL・64 桁の SHA-256・正のサイズがあること、同意が要るモデルにライセンス情報が揃うこと、ID と（フォルダ, 名前）の重複が無いこと、テンプレートが存在し構造が正しいこと。`content_rating` が `general` か `adult` で、`adult` は `advanced`（同意必須）であること、`license.id = unspecified` は商用不可であること、`trigger_words` は `loras` のファイルを持つモデルだけにあること、`source_note` に確認の日付があること、`loras` のファイルと `LoraLoaderModelOnly` のローダーがモデルのファイルと一致すること。新しい 6 モデルの URL・SHA-256・サイズは、Hugging Face の LFS の oid とコミットに照らした値で固定されている（`adult_test.go`）。

## 5. モデルの導入

### 5.1 ジョブ

| 種別 | 再開 | 内容 |
|---|---|---|
| `model_install` | **する**（`resumable=true`） | `target` は `engine` か `model:<id>`。ファイルを順に `downloads\` の `.part` へ取得（`Range` で再開）→ SHA-256 を再検証 → `data\models\<folder>\` へ移す。冪等で、検証済みのファイルは取り直さない |
| `gentest` | しない（`resumable=false`） | 試し生成（§8）。生成テストのバッチ（[../gentest/gentest.md](../gentest/gentest.md)）として動く。MAM が落ちたら、終わっていない結果は失敗になる |

- 再起動のたびに再開するのは `jobqueue.DefaultMaxAttempts`（3）回まで。それを超えると失敗として残り、利用者が導入をやり直す。
- 同じ対象のジョブが動いている間の再実行は、新しいジョブを作らずそのジョブを返す（`JobRef.Existing`）。対象ごとに直列化する。**書き込み先（`models/<folder>/<name>`）ごとにも直列化する**（対象が違っても同じ宛先なら 1 つずつ動き、ロックを取った後に宛先を調べ直す。`model:<id>` の導入は、利用者が HF から足した別の SHA-256 のファイルを上書きせず `*InvalidError` で断る。[model-manager-hf.md](model-manager-hf.md) §4）。
- 他のモデルが検証済みの同じファイル（パス・SHA-256・サイズが同じ。VAE・テキストエンコーダーなど）は、再ハッシュせず信用する。
- 2 つの種別は 1 つのマイグレーション（`20261025090000_job_kind_model.sql`）で `job.kind` の CHECK に加える（[er-model-manager.md](er-model-manager.md)）。
- 更新の安全ゲート（desktop.md §6.2）は種別を見ずに `running`・`queued` の `job` を数えるので、導入と試し生成の間は自動更新が保留される。

### 5.2 インストール後

導入の完了で、ワークフローごとの `local_models`（`bundled-comfyui`）を作る。再導入や、ファイルが欠けた `incomplete` の復旧は同じ操作で行う。

### 5.3 Hugging Face のトークン（任意）

`credential.Store` のプロバイダー `huggingface`（`SetHuggingFaceToken`）。既定のカタログは使わない。トークンは `huggingface.co` への最初の要求にだけ付き、別ホストへのリダイレクトでは外す。

## 6. 同意（ライセンスの確認）

- `consent_required` のモデルは、`InstallModel` が**同意の記録（ライセンス ID＋版）を確かめて**から、ジョブを作る。画面の確認だけに頼らず、API が強制する（MCP など他の入口でも同じ）。
- 記録は `state.json` の `Consents`（ライセンス ID ごとに ID・版・日時）。`InstallModel` の `accept` 引数、または `AcceptLicense` で記録する。`accept` の ID や版がカタログと違えば `*InvalidError`、記録が無ければ `ErrConsentRequired`。カタログの版が変わると、改めて同意が要る。
- ライセンスの要約・URL・商用可否・地域の制限は `ModelView.License` で画面が出す。モデルの重みは MAM が再配布せず、ライセンスはアプリ内に表示する。
- **同意の記録の鍵**: 一般のモデルはライセンス ID（同じライセンスのモデルは 1 回の同意で足りる）。**成人向けのモデルはモデルごと**（`adult:<モデル ID>`）。同じライセンス ID でも、成人向けの注意書きと原本の場所は、その同意で見せたものだからである（`consentKey`）。
- 成人向けのモデルの同意の文言は、**ライセンスの要約の後に、成人向けの注意書き（`ModelView.AdultNotice`）と、ミラーの原本の場所（`FileView.SourceNote`）を付ける**。ライセンスの記載が無いモデル（`license.id = unspecified`）と商用不可のモデルは、導入の前にこの要約と原本の場所を出し、同意してから導入する。

### 6.1 成人向けモデル（`content_rating = adult`）の扱い

- 成人向け（非検閲のモデル、または非検閲にする LoRA）は `content_rating = adult`。**必ず `tier = advanced`・`consent_required = true`**（Apache-2.0 の Chroma1-HD も。成人向けの利用条件への同意を取るため。`catalog.Validate` が確かめる）。
- **年齢確認が済むまで、成人向けのモデルは出さず、導入させない**。画面ではなく **Service が強制する**（§6.2）。モデルのカードには「成人向け」のバッジを付ける。一覧・詳細・導入・ライセンスの同意・試し生成の選択肢に加えて、**外部の ComfyUI の確認（`ExternalInventory`）にも出さない**。キューに入った試し生成の結果は、始まる時にゲートをもう一度調べ（`ReasonHidden`）、閉じていて成人向けのモデルなら呼ばずに失敗にする。
- **導入済みの成人向けモデルも、年齢確認がオフの間は使えない**（Issue #848 の判断。「導入済みなら使える」とはしない）。ゲートは導入だけでなく、選ぶことと生成を止める: (1) **割り当ての選択肢から外す**（`modelsettings` の `ImageModels`・`VideoModels`。画面・MCP の `get_global_settings` の `image_models`・`video_models`）。保存済みの割り当てが成人向けのモデルを指していれば、`View.Gated`（MCP は `gated_models`）に挙げ、画面は「（現在は使えません）」を付けたそのモデルを選択済みで見せて注意を出す（別のモデルが選ばれたように見せない）。`Save`・`set_model_assignment` は、その経路（主・代替どちらでも、変えていなくても）を理由つきの 422 / `validation` で断る。「既定に戻す」はできる。(2) **生成を断る**: Model Adapter が経路を呼ぶ前に調べる（`Adapter.Gate`。`GenerateVideo`・`GenerateImage`・直接呼び出し `direct.Image`・`direct.Video`）。**割り当てが成人向けのモデルを指したままでも断る**。(3) **削除はできるまま**（§6.2 の表）。**代替には回さない**: 通常の代替（主モデルが落ちた・ブレーカー OPEN のとき代替を呼ぶ。model-adapters.md §7）は、主モデルが使えないときに代替が同じ結果を作れるという前提で、Take の `fell_back` に残すだけの扱いである。ゲートの拒否は「使えない」ではなく「使ってはいけない」なので、**経路の主・代替のどちらかが成人向けなら、経路ごと断る**（代替が成人向けで主が非成人向けでも断る。成人向けの主モデルが、気づかれないまま非成人向けの代替の結果に変わらないように）。何も呼ばず（ComfyUI も起動せず）、課金もない。断りは `*agegate.RefusedError`（`errors.Is(err, agegate.ErrRefused)`）で、文言は「<モデル> は成人向けモデルで、年齢確認がオフのため使えません。モデル管理で年齢確認をオンにするか、モデルの割り当て（品質ポリシー・画像の用途）を別のモデルに変えてください」。Shot の生成・Reference Pack の作成は**キューに入れる前**に断り（`Orchestrator` の `decide`・`StartReferencePack`。画面・MCP は 409 / `conflict` でこの文言）、キューに入った後でゲートが閉じたジョブは実行時にもう一度断り、チャットに同じ文言を出す。Multi-model routing の候補は `Adapter.Unavailable` が「年齢確認がオフ」で外す。Shot の Video Model の MANUAL / LOCK が成人向けのモデルを指していても同じく断る（固定は他のモデルに替えない）。**Shot の Video Model のロックの選択肢（`domain.VideoModels()`）とディレクターのコマンド解釈に渡すモデルの一覧は、年齢確認がオフでも成人向けのモデルを除かない**（除くのは割り当て（`modelsettings`）の選択肢と試し生成の一覧だけ）。選べるが、生成は上のとおり断る。ゲートを開けば、同じ割り当てがそのまま使える。
- **何を成人向けとするか（述語 `Service.HidesModel`）**: ゲートを閉じている間、次のどれかに当たるモデルは成人向けとして扱う。Adapter の `Gate`（すべての生成）・割り当ての選択肢と保存（`SetHidden`）・試し生成の一覧と実行・保存ワークフローの実行（下）は、**この 1 つの述語**を使う。
  1. **名前**: 同梱エンジンのモデル（`comfyui:bundled-comfyui/<名前>`）で、名前が成人向けのカタログモデルの `workflows`、**導入記録（`state.json` の `models[].workflows`。成人向けとして導入したものは `adult = true`）**の `workflows`、NFAA の HF モデル（`user_models[].workflows`）のどれかと一致するもの。記録も見るので、後の版のカタログが成人向けのモデルを外す・名前を変えても、導入済みのモデルは成人向けのまま残る。
  2. **読むファイル**: ComfyUI のワークフローのグラフ（**任意の接続先のモデル**・取り込んだグラフ・名前を変えて作ったモデル・保存ワークフロー `comfy:<id>@v<N>`）のノードの入力（`ckpt_name`・`lora_name`・`unet_name`・`vae_name`・`clip_name*` などの文字列）が、**成人向けだけが持つファイル**を指すもの。成人向けだけが持つファイルとは、成人向けのカタログモデルの `files`・成人向けとして導入した記録の `files`・NFAA の HF モデルの `name` のうち、**成人向けでないカタログモデルの `files` にもあるものを除いたもの**（Flux.1 Dev のチェックポイントや Wan 2.2 の基盤のように、成人向けの LoRA の土台として共有されるファイルだけを読むグラフは成人向けではない）。ファイル名は、サブフォルダ・`\`・大文字小文字を無視して比べる（フォルダは見ない）。読めないグラフは何も読まないものとして扱う（そもそも走らない）。
  3. 組み込みのモデル（Seedance・Kling・GPT Image など）、OpenAI 互換の接続先のモデル、成人向けのファイルを読まないグラフは、これまでどおり成人向けではない。
- **保存ワークフローの実行**（ワークフロー画面・MCP の `run_workflow`）は Model Adapter を通らず `comfyui.Backend` を直接呼ぶので、`workflows.Service.StartRun` が同じ `Adapter.Gate` を、実行の前に調べる（成人向けなら `*InvalidError`。文言は上と同じ）。
- **範囲と限界**: 判定はファイル名で、ファイルの中身は調べない。成人向けのファイルを別の名前にコピーして読ませるグラフ、ComfyUI のサーバー側で名前を付け替えたファイルは見分けられない（この判断は「導入済みの成人向けモデルが、年齢確認オフで使えない」の趣旨の範囲。ワークフローの保存の仕方は変えない）。保存ワークフロー自体の一覧・編集・保存は、ゲートが閉じていてもできる（走らせることと割り当てることだけを断る）。
- **LoRA だけを足すモデル**（`flux1-dev-nsfw-master`・`wan22-i2v-14b-nsfw-lora`）は、ベースのファイルも `files` に含める。ベースが導入済みなら取り直さず、ベースのモデルを消しても、LoRA のモデルが残る間は共有ファイルは消えない（§7）。`trigger_words`（LoRA のトリガー語）は一覧・MCP に出す。
- 第三者のミラー（原本が Civitai のもの）のファイルは `source_note`（原本の場所と、SHA-256 が一致することを確かめた日付）を持つ。ミラーが消えて 404・410 になったときは、取得の失敗ではなく**「取得元が消えています」**（`ErrSourceGone`。ファイル名・HTTP の状態・`source_note` を添える）で失敗する。

### 6.2 年齢確認（`AdultContent`）

設定「成人向けモデルを表示する」（既定はオフ）。**オンにするときに一度だけ**、18 歳以上（またはその地域の成年）であることの確認と注意書き（`AdultNotice`）への同意を求め、`state.json` の `adult_content`（`enabled`・`notice_version`・`confirmed_at`）に記録する。オフにしても記録は残り、もう一度オンにするときは聞き直さない。注意書きの文言を変えたら `AdultNoticeVersion` を上げる（古い版の確認ではゲートは開かず、聞き直す）。モデルごとのライセンスの同意（上）は、これとは別に取る。

| 呼び出し | ゲートが閉じているとき |
|---|---|
| `Catalog()` | 成人向けのモデルを返さない |
| `Model(id)`・`AcceptLicense`・`InstallModel` | `ErrAdultNotConfirmed`（導入は何もダウンロードせず、同意も記録しない。画面・MCP は 409 / `conflict`） |
| 導入のジョブ（`runModelInstall`） | 待っている間に閉じられたら、始めに同じ理由で失敗する |
| 試し生成のモデルの一覧（`mmview.TrialModels`、生成テストの `Models`・`Model`） | 導入済みの成人向けモデル（`comfyui:bundled-comfyui/<ワークフロー名>`）も出さず、名指しの要求は不明なモデルとして断る（`gentest.Deps.Hidden` = `HidesModel`） |
| 画像・動画モデルの割り当て（`modelsettings`、画面・MCP） | 導入済みの成人向けモデルを選択肢に出さず、それを指す割り当ては保存できない（`HidesModel` = `modelsettings.Service.SetHidden`）。§6.1 |
| 生成（Shot・Reference Pack・生成テスト・Adapter の直接呼び出し） | 割り当てが指していても断る（`agegate.ErrRefused`。何も呼ばず、代替にも回さない）。§6.1 |
| `DeleteModel` | 影響しない（導入済みのファイルの片づけは、いつでもできる。ゲートが閉じていても導入済みのモデルを消せる。ただし割り当てが指していれば従来どおり `*InvalidError`） |

```go
func (*Service) AdultContent() AdultContentStatus          // enabled・confirmed・confirmed_at・notice_version・notice
func (*Service) AdultContentEnabled() bool                 // ゲートが開いているか。#842（Hugging Face の取り込み）などが「成人向けの確認は済んだか」を聞くのはこれ
func (*Service) SetAdultContent(on bool, *AdultConfirmation) error  // オンは確認（Adult=true・現行の NoticeVersion）が要る。確認が無く、記録も無ければ ErrAdultNotConfirmed、確認が足りなければ *InvalidError
func (*Service) HidesModel(id string) bool                 // 試し生成・割り当ての選択肢・生成（Adapter の Gate）が、成人向けのモデルを隠す・断るかを聞く
```

**MAM は年齢を自分で確認したことにしない**。画面は「同意する」のチェックの後にだけ `AdultConfirmation` を渡し、MCP の `set_adult_content` は、ユーザー自身が確認して答えた後にだけ `confirm_adult=true` で呼ぶ（`accept_model_license` と同じ扱い。mcp-tools-model-manager.md）。生成物の内容は選別しない（ローカルでの利用は利用者の責任）。

## 7. 状態・削除

`state.json`: `AutoStart`・`LastPort`・`Engine`（版・変種・CPU）・`Consents`・`AdultContent`（§6.2）・`Models`（導入済みのファイルとワークフロー、導入時の成人向けの格付け `adult`）。読めない記録は `state.json.corrupt` に退避して空から始める（MAM の起動を止めない。ファイルは残り、導入は検証し直す）。

**削除**（`DeleteModel`）: 使われている画像用途・Quality Policy があれば `*InvalidError` で断る（`localmodels` の `UsedBy`）。`local_models` を消し、**他の導入済みモデルが使っていないファイルだけ**を消す（共有する VAE・テキストエンコーダーは、使うモデルが残る間は残す。参照数は記録した導入済みファイルから数える）。

## 8. 試し生成（`gentest`）

試し生成は**生成テスト（[../gentest/gentest.md](../gentest/gentest.md)）の 1 モデル・1 プロンプト・1 本のバッチ**として動く。実装は 1 つ（`service/gentest`。このサービスにはもう試し生成のコードも保存場所もない）。画面（`handler/modelmgr`）は `gentest.Service` の `Estimate`→`Run` を呼び、一覧は履歴のうち「この画面のローカルのモデル」の結果を新しい順に出す。モデル（`comfyui:bundled-comfyui/...`、外部の ComfyUI、OpenAI 互換）はモデル名を指定した直接呼び出し（代替なし。[../gentest/gentest-adapter.md](../gentest/gentest-adapter.md)）で呼ぶ。同梱エンジンのモデルは、`localbackends.Options.Prepare`（`Service.Prepare`）が呼び出しの前にエンジンを起動する。結果は `<data-dir>/gentest/<バッチ ID>/` に置き、`take`・`cost_ledger_entry`・アセットには書かない。参照画像は `gentest/_staging/` に置いてバッチの作成時に入力として複製する（24 時間以上前の取りこぼしは起動時に消す）。

## 9. 起動・連携（`internal/app`）

- `services.go` が `modelmanager.Service` を作り、`Register()` をジョブキューの開始前に呼ぶ。`Services.ModelManager` で公開する。`mm.Close` はジョブの停止の後に走る（エンジンを使うジョブが先に止まる）。
- `mam.exe --detect-gpu` / `--detect-comfyui` は、ウィンドウもデータディレクトリも開く前に JSON を出して終わる（`app.RunDetect`、`cmd/server` と `cmd/wails` が最初に呼ぶ。両方を付けると `{"gpu":..., "comfyui":...}`。`--detect-output FILE` で標準出力ではなくファイルへ書く。setup.md）。

## 10. Service API（`internal/service/modelmanager`）

画面・MCP は次を呼ぶ（戻りは JSON タグ付きの構造体）。エラーは `ErrUnsupported`・`ErrNotFound`・`ErrEngineNotInstalled`・`ErrAlreadyInstalled`・`ErrNotInstalled`・`ErrConsentRequired`・`ErrAdultNotConfirmed`・`ErrBusy`・`ErrNoGPU`・`ErrPinned`・`ErrEndpointMoved`・`*InvalidError`（メッセージを利用者に見せる）。取得元が消えたときのジョブの失敗は `ErrSourceGone`（利用者向けのメッセージに包む）。

```go
func New(Deps) (*Service, error)
func (*Service) Register()                      // ジョブキュー開始前に
func (*Service) OnStartup(ctx)                  // 取りこぼしの掃除・接続先の登録・AutoStart
func (*Service) Close()                         // エンジンを止める
// 状態・検出
func (*Service) Status(ctx) (Status, error)     // エンジン・ComfyUI の接続先・固定の有無
func (*Service) DetectGPU(ctx) gpu.Report
func (*Service) DetectComfyUI(ctx) detect.Report
func (*Service) Watch(func(Event)) (cancel func())   // エンジンの状態・ジョブの進捗
func (*Service) JobStatus(ctx, jobID) (JobStatus, error)
// エンジン
func (*Service) Engine() EngineView             // 導入版と固定版・変種・空き容量・書き込み可否・プロセス
func (*Service) InstallEngine(ctx, EngineInstallInput{Variant, CPU}) (JobRef, error)  // 導入・更新
func (*Service) RemoveEngine(ctx) error
func (*Service) StartEngine(ctx) (proc.Status, error)
func (*Service) StopEngine() error
func (*Service) CancelInstall(jobID string) error  // 動いている導入の中断（ErrNotFound: 動いていない）
func (*Service) SetAutoStart(on bool) error
func (*Service) Prepare(ctx, endpoint string) error  // Adapter の呼び出し前に呼ばれる（画面は使わない）
// モデル
func (*Service) Catalog() []ModelView
func (*Service) Model(id string) (ModelView, error)
func (*Service) AcceptLicense(modelID string, LicenseRef) error
func (*Service) InstallModel(ctx, modelID string, accept *LicenseRef) (JobRef, error)
func (*Service) DeleteModel(ctx, modelID string) error
func (*Service) SetHuggingFaceToken(token string) error
func (*Service) ClearHuggingFaceToken() error
func (*Service) HuggingFaceTokenStatus() (configured bool, lastChars string, err error)
func (*Service) ExternalInventory(ctx, endpoint string) (ExternalInventory, error)  // 外部の ComfyUI（読むだけ）
// 成人向けモデルの年齢確認（§6.2）
func (*Service) AdultContent() AdultContentStatus
func (*Service) AdultContentEnabled() bool
func (*Service) SetAdultContent(on bool, *AdultConfirmation) error
func (*Service) HidesModel(id string) bool
// Hugging Face からの導入（model-manager-hf.md）
func (*Service) SearchHF(ctx, query, kind string, limit int) ([]hf.SearchHit, error)  // NFAA はゲートが閉じている間は除く
func (*Service) HFRepo(ctx, input string) (HFRepoView, error)                         // URL・owner/repo → コミットに固定したファイルの一覧
func (*Service) InstallHFFile(ctx, HFInstallInput) (JobRef, error)                    // repo・commit・path。SHA-256・サイズは Service が HF から読み直す
func (*Service) ActiveHFJobs() map[string]string                                      // 動いている導入（target → job_id）
func (*Service) UserModels() []UserModelView                                          // 利用者が足したファイル
func (*Service) DeleteUserModel(ctx, path string) error
```

- **エンジンの更新**: MAM の更新とは独立。サイレントインストーラーはエンジンを取り直さない。`Engine()` の導入版と固定版（カタログ）が違えば更新できる状態で、`InstallEngine` が新しい固定版を入れる（`engine\` を入れ替え、`data\` は残る）。
- **進捗**: ジョブは `JobStatus.Progress`（割合・メッセージ・バイト）を持つ。メモリ上（`track`、最新 128 件）で、再起動後は DB の状態だけになる。`get_job` 相当の問い合わせは `JobStatus` を使う。
- **画面の注意**: 外部 ComfyUI の `ExternalModel.Missing` は表示だけで、導入の操作は出さない。`EngineView.Supported=false`（非 Windows）では導入のボタンを出さず、外部 URL の設定を案内する。`Writable=false` のときも同じ。

## 11. 未確認・テストの範囲

| 項目 | 状態 |
|---|---|
| Windows の DXGI による GPU 検出（`dxgi_windows.go`） | ビルドと vet のみ。**Linux の開発ホストでは実行できず未検証** |
| Windows の Job Object・非表示ウィンドウでの子の起動 | 同上（Linux の `Pdeathsig` は子プロセスのテストで確認済み） |
| 実物の ComfyUI の起動、実際の生成、ワークフロー（テンプレート）の出力 | **未検証**（テンプレートは構造と、ローダーとファイル名の一致だけ。HunyuanVideo のサンプラー設定は推定） |
| 実物のエンジン（約 2 GB）・モデル（数十 GB）の取得と展開 | **未実施**。合成アーカイブと偽の GitHub・HF サーバー（`Range`・ハッシュ不一致・再開・空き容量不足）で確認。実アーカイブのヘッダーと BCJ2 のファイルは HTTP の `Range` で読めることを確かめた |
| モデルの VRAM の目安 | Wan 2.2（README）と HunyuanVideo（文書）以外は MAM の推定（成人向けの 5 モデルと `wan22-i2v-14b` もすべて `estimate=true`） |
| 成人向け・LoRA の 6 モデルのテンプレートの実行（`txt2img-qwen-image21`・`txt2img-chroma`・`txt2img-flux-dev-lora`・`img2video-wan22-14b`・`-remix`・`-lora`） | **GPU 実機で未検証**（構造テストと、ローダーとファイル名の一致だけ。サンプラーの値は作者の推奨と ComfyUI の公式手順に基づく） |
| Wan 2.2 14B に `wan_2.1_vae` を使うこと、Chroma の `CLIPLoader type=chroma` ＋ fp8mixed の組み合わせ | **未検証**（ComfyUI の公式手順・作者のワークフローに基づく。実機で確かめる） |
| NSFW MASTER の作者の原本のライセンス | **未確認**（HF のミラーのタグは MIT。Civitai の API はライセンス名を返さない。全体を非商用として扱う） |
| 実物の Hugging Face からの 6 モデルの取得（数十 GB）と、ミラーが消えたときの実際の応答 | **未実施**（偽の HF で、404 の `ErrSourceGone`・SHA-256 の不一致・共有ファイルの 1 回取得は確認） |
| ファイル名での成人向けの判定（§6.1）が、実物の ComfyUI のファイル名（サブフォルダ付き）と一致すること | **未検証**（`ckpt_name` などは ComfyUI の `/models/<folder>` が返す名前で、サブフォルダ付きのときは末尾の名前で比べる。実機のサーバーは使っていない）。中身が同じで名前だけ違うファイルは見分けられない |
| 年齢確認がオフのときの生成の断りを、実物の ComfyUI・クラウドに対して確かめること（呼び出しが届かないこと） | **未検証**（断りは Adapter の手前で、偽のバックエンドの呼び出し回数 0 と、同梱エンジンが起動しないことで確認。実機の ComfyUI・実アカウントは使っていない） |
| 実物の Hugging Face からの任意のモデルの導入（Issue #842: 実物の API・CDN・gated・GPU・SDXL の生成） | **未検証**（[model-manager-hf.md](model-manager-hf.md) §10。偽の HF で確認） |

## 12. 画面と MCP ツール

- **画面**: `GET /model-manager`（`web/handler/modelmgr`・`organisms/modelmgr`）。仕様は [endpoints-model-manager.md](../../api/endpoints-model-manager.md)・[model-manager-screen.md](../../components/model-manager-screen.md)。入口はプロジェクト一覧と設定画面のリンク。進捗は HTMX のポーリング（2 秒）。
- **MCP ツール**: `mcp/tools/modelmgr`（エンジン・カタログ・トークン・外部）と 生成テストの `mcp/tools/gentest`（試し生成もこれで行う）。仕様は [mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)・[mcp-tools-generation-test.md](../../api/mcp-tools-generation-test.md)。ジョブの種別 `model_install` の Probe はこの画面側、`gentest` の Probe は生成テスト側にあり、`get_job` が状態・進捗・結果・失敗の理由を返す。
- **共有**: サービスの拒否は `service/modelmanager/mmview.Refuse`（種類とメッセージ）で、画面の HTTP 状態と MCP のエラーコードに写す（`ErrConsentRequired`・`ErrBusy`・`ErrPinned`・`ErrUnsupported` は 409 / `conflict`、`ErrNoGPU`・`*InvalidError` は 422 / `validation`）。`mmview.TrialModels` は試し生成の選択肢（モデルの識別子・動画の解像度と長さ・参照画像を使えるか）。
- **導入の中断**: `CancelInstall`。ジョブは失敗で終わり（理由に「導入を中断しました」）、`.part` は残るので、導入し直すと続きから再開する。
- **同意の強制は API**: 画面のダイアログは同意を渡す手段で、`InstallModel` が記録を確かめる（§6）。MCP からも同じ（記録が無ければ 409 / `conflict`）。**年齢確認も同じ**（§6.2）: 画面のダイアログ（`PUT /model-manager/adult`）と MCP の `set_adult_content` は確認を渡す手段で、`Catalog`・`InstallModel`・試し生成の一覧は Service が閉じる。`ErrAdultNotConfirmed` は 409 / `conflict`（`mmview.Refuse`）。
- **テスト用の部品**: `service/modelmanager/mmtest` は偽の GitHub / Hugging Face（`Range`・保留 `Hold` つき）・偽のエンジンのプロセス・ジョブキューつきの `Service` を作る（画面と MCP のテストが共有する）。
- **Hugging Face からの導入**（Issue #842）: カタログに無い `.safetensors` を HF のリポジトリから導入する経路。意味・固定・フォルダ・年齢ゲート・記録・未検証は [model-manager-hf.md](model-manager-hf.md)。画面は `web/handler/modelmgr/hfimport`、MCP は `mcp/tools/modelmgr/hf.go` の 5 ツール。
