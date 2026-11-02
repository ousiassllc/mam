# モデルマネージャー（ComfyUI エンジンの取得・起動とモデルの導入）

[local-models.md](../local-models.md)（利用者が登録した ComfyUI・OpenAI 互換の接続先）に、**MAM が自分で ComfyUI を取得・起動し、カタログのモデルを入れる**経路を足す。実装は `backend/internal/modelmgr/*`（部品）と `backend/internal/service/modelmanager`（Service）。画面（[../../api/endpoints-model-manager.md](../../api/endpoints-model-manager.md)）・MCP ツール（[../../api/mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)）はこの Service API の上に載る（§12）。インストーラーは変わらない（desktop.md §4・§6.8）。声（GPT-SoVITS）・口パク（MuseTalk）のローカルランタイムは同じ作りで別の文書（[local-runtime.md](local-runtime.md)）。改訂履歴は [model-manager.changelog.md](model-manager.changelog.md)。

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

**通信が切れたときの自動再開**（`fetch.Options.Retry`・`OnRetry`。エンジン・カタログのモデル・HF からの導入・Codex のエンジン・ffmpeg の取得が共有する）:

- 取得の途中で接続が切れた（接続のリセット・強制切断（Windows の `wsarecv: An existing connection was forcibly closed by the remote host`）・本体が途中で終わる・タイムアウト・アイドルで中断）ときと、サーバーが 5xx・408・429 を返したときは、`.part` の続きから `Range` で自動的に取り直す。再開のたびにディスク上のバイト列を読み直すので、SHA-256 は常にファイル全体にかかる。
- **回数と待ち時間には上限がある**: 最初の取得を含めて最大 6 回（`Retry.Attempts`）、再開の前の待ちは 2 秒から倍に増やして最大 30 秒（`Retry.Backoff`・`MaxBackoff`）。待ちの間も取り消しが効く。
- **取り直さないもの**: 利用者の取り消し（`ctx` の取り消し）、ディスクの空き不足・書き込みの失敗（`fsutil.LocalError`・`NoSpaceError`）、SHA-256・サイズの不一致、4xx（408・429 を除く）、アドレス規則の拒否など、通信の切断でないエラー。
- 上限まで切れ続けたときの失敗は `*fetch.ConnectionLostError` で、`.part` は残る。モデルマネージャーのジョブは「通信が切れました。もう一度導入すると続きから再開します」で失敗する（原因の詳細は `userError` の裏側に残り、ログに出る）。
- 再開は WARN（`download interrupted, resuming from the part file`。`job_id`・`target`・`attempt`・`max_attempts`・`retry_in`・`err`）、導入ジョブの結末は成功が INFO（`install succeeded`）、利用者の中断が INFO（`install cancelled by the user`）、シャットダウンでの中断が WARN（`install interrupted`）、失敗が ERROR（`install failed`。原因の `err` つき）で、いずれも `job_id` と対象（`target` = `engine`・`model:<id>`・`hf:…`）を持つ。

取得できるホストは `huggingface.co`・`hf.co`・`github.com`・`githubusercontent.com` だけで、リダイレクト先も同じ規則で調べる（`fetch.AllowedURL`。エンジンのアーカイブとカタログのモデルの取得）。**Hugging Face の導入経路（`hf` のクライアントと導入したファイルの取得）は `huggingface.co` と `*.hf.co` だけ**（`fetch.HFAllowedURL`。GitHub へのリダイレクトは断る。[model-manager-hf.md](model-manager-hf.md) §2）。`Authorization` は最初の要求の宛先ホストにだけ付け、ホストが変わると外す（§5.3）。

**7z の方式**: 公式パッケージは LZMA2＋BCJ2（`-m0=lzma2 -mf=BCJ2 -md=768m`）。`bodgit/sevenzip` v1.6.0 が同じ方式で作った合成アーカイブ（`extract/testdata/bcj2.7z`、1.5 GB のものでも）を読めることを確かめた。展開時の最大メモリは 1.5 GB のアーカイブで約 1.6 GB。実物の約 2 GB を丸ごと取得・展開した検証は未実施（§11）。

### 3.4 プロセス（`modelmgr/proc`）

起動引数: `--listen 127.0.0.1 --port <空きポート> --disable-auto-launch --disable-all-custom-nodes [--whitelist-custom-nodes <フォルダ…>] --base-directory <data>`（`--cpu` は CPU を選んだときだけ。`--whitelist-custom-nodes` は、mam が導入して固定の内容のままの追加ノードがあるときだけ、そのフォルダ名を渡す。許可リストにないフォルダは読み込まれない。[custom-nodes.md](custom-nodes.md) §3）。`--windows-standalone-build` は付けない（v0.42.1 ではブラウザを開き assets API を有効にするだけのため）。

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
| `files[]` | `name`・`folder`（`checkpoints`・`diffusion_models`・`text_encoders`・`vae`・`loras`・`latent_upscale_models`・`controlnet`・`clip_vision`・`ipadapter`。最後の 3 つは Issue #976）・`url`（**コミット固定の** Hugging Face の URL、https）・`sha256`・`size`・`source_note`（第三者のミラーのとき: 原本の場所と、SHA-256 が一致することを確かめた日付。省略可） |
| `vram` | `min_gb`（目安。これ未満の GPU は「足りない」と画面・MCP が判定する）・`recommended_gb`（省略可。ベンダーが推奨するメモリ。`min_gb` より大きいときは `limits` が要る）・`limits`（推奨に届かない GPU で何を諦めるか。量子化・タイル VAE・オフロード・速度など）・`source`（出典。無いものは保守的な値）・`estimate`（true は MAM の推定で GPU では未確認）。`min_gb` 以上 `recommended_gb` 未満の GPU は「動くが制約あり」（§4.4） |
| `license` | `id`・`version`・`name`・`url`・`summary`・`commercial`（`yes` / `conditional` / `no` / `unknown`）・`commercial_condition`・`checked_at`（原文を読んだ日）・`components[]`（ベースと派生でライセンスが違うときの部品ごとの内訳。全体の `commercial` はその最も厳しい答え）・`excluded_regions`・`notice`。意味・置き場所・各モデルの答えは [../model-license.md](../model-license.md)（Issue #928） |
| `consent_required` | true のモデルは、同意の記録が無いと導入のジョブを作れない（§6） |
| `requires_hf_token` | true のモデルは、Hugging Face のログイン（トークン）が要るファイルを持つ（gated。LTX-2.5）。`InstallModel` が、トークンの有無とアクセスの権限を**ジョブを作る前に**確かめる（§5.3）。画面のカードに「HF トークンが必要」のバッジを出す。それ以外のモデルは false（ログイン不要の `Comfy-Org` ミラーなど）。true のモデルのファイルは、すべて `huggingface.co` の固定 URL でなければならない（第三者のミラーは使わない。`catalog.Validate`） |
| `workflows[]` | `name`（`local_models` のワークフロー名）と `template`（`localcfg` のテンプレート。local-models.md §4） |

### 4.2 v1 のモデル

| ID | 種別 | 区分 | 合計サイズ | VRAM 目安 | ライセンス |
|---|---|---|---|---|---|
| `sdxl-base` | 画像 | 標準 | 6.9 GB | 8 GB（推定） | CreativeML OpenRAIL++-M |
| `sdxl-controlnet-canny` | 画像 | 標準 | 約 9.4 GB（SDXL ベース 6.9 GB と共有。ControlNet 2.5 GB） | 10 GB（推定） | OpenRAIL++-M（ベース）・Apache-2.0（ControlNet） |
| `sdxl-ipadapter-plus` | 画像 | 標準 | 約 10.3 GB（SDXL ベースと共有。IP-Adapter 0.85 GB・CLIP ViT-H 2.5 GB） | 10 GB（推定） | OpenRAIL++-M（ベース）・Apache-2.0（IP-Adapter）・MIT（CLIP ViT-H）。追加ノード `ipadapter-plus` が要る |
| `flux1-schnell` | 画像 | 標準 | 17.2 GB | 16 GB（推定） | Apache-2.0 |
| `flux2-klein-4b` | 画像 | 標準 | 12.5 GB | 12 GB（推定。fp8 蒸留版） | Apache-2.0（可） |
| `qwen-image` | 画像 | 標準 | 30.1 GB | 24 GB（推定） | Apache-2.0 |
| `wan22-ti2v-5b` | 動画 | 標準 | 18.1 GB | 16 GB（推定。ComfyUI のネイティブ offload） | Apache-2.0（可） |
| `wan22-ti2v-5b-fp8` | 動画 | 標準 | 13.4 GB | 16 GB（推定。fp8 の重みは約 5.3 GB だが、下限を下げる測定は無い） | Apache-2.0（可） |
| `ltx-video-2b` | 動画 | 標準 | 11.2 GB | 12 GB（推定） | LTX-Video Open RAIL-M |
| `ltx-2.5-22b-distilled` | 動画 | 同意（HF トークン必須） | 44.9 GB（プロンプト拡張の約 5.2 GB を含む） | 16 GB 以上・推奨 32 GB 以上（推奨は Lightricks の文書。16 GB は INT8・タイル VAE・オフロードが前提のコミュニティ報告で、GPU では未確認） | LTX-2.x Community License（年間売上 1,000 万 US ドル未満は無償） |
| `ltx-2.5-22b-distilled-nvfp4` | 動画 | 同意（HF トークン必須） | 36.9 GB | `ltx-2.5-22b-distilled` と同じ値（推定）。NVIDIA Blackwell 世代（RTX 50 など）の GPU が前提 | 同上 |
| `ltx-2.5-22b-distilled-dfr` | 動画 | 同意（HF トークン必須・リポジトリ 2 つ） | 40.3 GB | `ltx-2.5-22b-distilled` と同じ値（推定）。3 段なので遅い見込み | 同上（IC-LoRA も同じ LTX-2.x Community） |
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
| `wan22-remix-nsfw-i2v` | 動画 | 成人向け | 35.6 GB | 16 GB（推定） | ライセンスの記載なし（`unspecified`。**不明**） | Remix I2V v3.0 高・低（ミラー）＋共有の TE・VAE |
| `wan22-i2v-14b-lightx2v` | 動画 | 標準 | 37.0 GB（導入済みの `wan22-i2v-14b` があれば LoRA の 1.4 GB だけ取得） | 16 GB（推定） | Apache-2.0（可） | `wan22-i2v-14b` のファイル（共有）＋ LightX2V の 4 ステップ蒸留 LoRA（高・低ノイズ、`loras`）。速さのモード `fast`（4 ステップ）/ `standard`（通常 20 ステップ・LoRA なし）。Issue #930 |
| `wan22-remix-nsfw-t2v` | 動画 | 成人向け | 35.6 GB | 16 GB（推定） | ライセンスの記載なし（`unspecified`。**不明**） | Remix T2V v2.0 高・低（ミラー）＋共有の TE・VAE。テキストから動画。Issue #930 |
| `wan22-i2v-14b-lightx2v-uncensored` | 動画 | 成人向け | 38.1 GB（導入済みの `wan22-i2v-14b` があれば LoRA の 2.5 GB だけ取得） | 16 GB（推定） | Apache-2.0 の宣言だけ（`apache-2.0-declared`。**不明**） | `wan22-i2v-14b` のファイル（共有）＋ rzgar の非検閲 4 ステップ LoRA（高・低ノイズ）。Issue #930 |
| `wan22-i2v-14b-nsfw-lora` | 動画 | 成人向け | 36.8 GB（導入済みの `wan22-i2v-14b` があれば LoRA の 1.2 GB だけ取得） | 16 GB（推定） | ライセンスの記載なし（`unspecified`。**不明**） | `wan22-i2v-14b` のファイル（共有）＋ NSFW-22-H/L-e8（`loras`、ミラー）。トリガー語 `nsfwsks` |
| `ltx-2.5-22b-distilled-uncensored` | 動画 | 成人向け（HF トークン必須） | 37.5 GB（導入済みの `ltx-2.5-22b-distilled` があれば、テキストエンコーダーの 13.2 GB だけ取得） | 16 GB 以上・推奨 32 GB 以上（標準と同じ） | LTX-2.x Community ＋ 変換版テキストエンコーダーは本文なし（**不明**。厳しい方） | LTX-2.5 のテキストエンコーダーだけをコミュニティ製の非検閲版（Gemma 4 12B の heretic 派生。INT8 ConvRot）に替えた組み合わせ。ほかの 4 ファイルは標準と共有 |

Wan 2.2 14B のベース（`wan22-i2v-14b`）は I2V のみ（決定 4）。Remix だけは T2V v2.0 も入れる（下の Issue #930）。ミラーのファイル（NSFW MASTER・Remix・Wan NSFW LoRA）は、Hugging Face のミラーの SHA-256 が Civitai の原本と一致することを 2026-10-25 に確かめた（`source_note`）。Remix と Wan NSFW LoRA は HF にライセンスの本文が無く、Civitai の表示（販売まで可／Civitai 上の利用だけ）に頼らず、商用利用は `unknown`（不明。「商用NG」とは断定せず、`notice` で目立たせる）として入れる（Issue #928。[../model-license.md](../model-license.md)）。除外したもの: LTX-2.3 Uncensored（テキストエンコーダーが GGUF で `ComfyUI-GGUF` が要る）・WAI-NSFW-Illustrious-SDXL（Civitai だけ）・abenzerps UC。

**Wan 2.2 Remix と LightX2V 4 ステップ LoRA（Issue #930）**。既にあった Remix I2V v3.0（`wan22-remix-nsfw-i2v`）に、**Remix T2V v2.0**（`wan22-remix-nsfw-t2v`）、**LightX2V の LoRA を載せた標準の 14B**（`wan22-i2v-14b-lightx2v`）、**非検閲の 4 ステップ LoRA**（`wan22-i2v-14b-lightx2v-uncensored`）を足した。ファイルの SHA-256・サイズは Hugging Face の API と応答ヘッダー（`X-Linked-ETag`・`X-Linked-Size`）で 2026-11-02 に取得し、Remix の 2 つは Civitai の API の `hashes` とも一致を確かめた。

- **ワークフローはコアノードだけ**: Issue は「コミュニティ製（KJNodes など）のワークフローが中心。mam が自動で入れるノードを決める」と書くが、**mam は入れるノードを「無し」と決めた**。同梱の ComfyUI は `--disable-all-custom-nodes` で起動し（§3.4）、この時点ではカスタムノードを入れる仕組みも無かった（Issue #976 で許可リストの仕組みができた: [custom-nodes.md](custom-nodes.md)。この節の判断は、その時点のテンプレートについてのもの）。作者の公式ワークフローは、Remix I2V・T2V の `*-comfy-*-workflow.json` が **コアのノード（`UNETLoader`・`KSamplerAdvanced`・`WanImageToVideo`・`EmptyHunyuanLatentVideo` ほか）に、VHS・rgthree・`FastUnsharpSharpen` だけを足した形**で、足している分は動画の保存・乱数・シャープ化で生成に要らないため、mam は `SaveVideo`・シードの割り当てに置き換えた。KJNodes・WanVideoWrapper の版（`Wan2.2-Remix-kijai-t2v-workflow.json`、LightX2V の `wan2.2_i2v_scale_fp8_comfyui_with_lora.json`）は使わない（ブロックスワップ・torch compile などの 16 GB 向けの調整が入るが、コアの自動 offload で足りる前提で、**mam の実機では未確認**）。`EmptyHunyuanLatentVideo` は ComfyUI v0.42.1 のコア（`comfy_extras/nodes_hunyuan.py`）にあることをテスト（`coreNodes`）で確かめる。
- **Remix**（`FX-FeiHou/wan2.2-Remix`。コミット `4369125a…` に固定）: 拡散モデルは fp8 の高ノイズ・低ノイズ（各 14.3 GB）で、I2V v3.0・T2V v2.0 とも作者の最新。16 GB の GPU で動かすには fp8 が現実的で、**fp16（各 28.6 GB）は入れない**。GGUF の Remix は、HF の `FX-FeiHou/wan2.2-Remix` には無く（HF API で確認）、読み込みに `ComfyUI-GGUF` が要るため対象外。サンプラーは作者の同梱ワークフローの値（12 ステップ・euler/simple・cfg 1、10 ステップ目で切り替え、shift は T2V が 5・I2V が 8）。Remix は Lightning 系の加速をマージに含む（ファイル名の `lighting`、作者の注記 `Integration: Fully compatible with lightx2v, no extra setup required`、12 ステップ・cfg 1 の同梱ワークフロー）ので、**LightX2V の LoRA は重ねない**（二重に加速することになる）。作者の推奨テキストエンコーダー（`NSFW-API/NSFW-Wan-UMT5-XXL`）ではなく、Wan 2.2 標準の umt5 fp8 を使う（I2V と共有。非検閲の語を理解する差は**未確認**）。
- **LightX2V の 4 ステップ LoRA**（`lightx2v/Wan2.2-Distill-Loras`。コミット `57adea58…`。モデルカードの `license: apache-2.0`）: I2V 用の 1022 版（高ノイズ 634,645,944 B・低ノイズ 739,472,104 B）。高ノイズ用は高ノイズのモデルに、低ノイズ用は低ノイズのモデルに、それぞれ `LoraLoaderModelOnly`（強さ 1）で重ねる（ComfyUI の公式テンプレート `video_wan2_2_14B_i2v` の 4 ステップ版と同じ構造）。サンプラーは 4 ステップ・euler/simple・cfg 1（ネガティブは効かない）・2 ステップ目で切り替え・shift 5。T2V 用の LoRA（1217 版）はベースの T2V を持たないので入れない。
- **ステップ数・CFG の切り替え（速さのモード）**: ワークフローに `speed_modes` を持たせた（`localcfg.SpeedMode`。[local-models.md](../local-models.md) §3）。`wan22-i2v-14b-lightx2v` のテンプレート `img2video-wan22-14b-lightx2v` は `fast`（既定。グラフそのまま = LoRA あり・4 ステップ・cfg 1）と `standard`（LoRA を外し、`img2video-wan22-14b` と同じ 20 ステップ・cfg 3.5・shift 8・10 ステップ目で切り替えに戻す）の 2 つを持つ。生成テストの画面（動画フォームの「速さ」）と MCP（`estimate_generation_test`・`run_generation_test` の `speed_mode`）で選び、その値は結果の `speed_mode` に残る（[gentest.md](../gentest/gentest.md) §6）。同じ入力で通常と 4 ステップを比べるには、`wan22-i2v-14b`（通常）と `wan22-i2v-14b-lightx2v`（`fast`）を並べて選ぶか、後者に `standard` を付ける。LoRA の強さは作者の指定の 1 に固定し、画像用の LoRA 選択（`loras`）は動画に使わない（LoRA の高・低ノイズの 2 本を別々に選ぶ入力を作らず、モードで束ねた。理由: 2 本を取り違えると壊れた動画になるため）。
- **非検閲の 4 ステップ LoRA（rzgar）**: `rzgar/Wan2.2_LightX2V_4Step_Uncensored`（コミット `adc4ca92…`。カードは `license: apache-2.0`）は、I2V・T2V の両方で使える LightX2V 蒸留に非検閲の学習を重ねた LoRA（高・低各 1,245,752,600 B。テンソル名は `diffusion_model.*.lora_A/B`。ComfyUI が読める形式のはずだが、実際の読み込みは**未確認**）。**載せた**（成人向け）。理由: 実在・SHA-256 を確認でき、カードにライセンスの宣言（Apache-2.0）があり、ComfyUI のコアの `LoraLoaderModelOnly` で動くため。ただし LICENSE の本文・学習データの出所は無く、宣言だけでは商用で使ってよいか確かめられないので、**商用利用の可否は「不明」**（`license.id = apache-2.0-declared`。ベースは Apache-2.0 で `components[]` に `yes`、LoRA は `unknown`）にして `notice` を目立たせる（確認できないものは断定せず不明にする、model-license.md §7 の 3）。成人向けの同意（§6.1）も必須で、公開は地域の法令と配信先の規約に従う旨を出す。作者は「強さを下げると速さの効果が弱まり、ブレて動きが崩れる」と書くため強さは 1 固定。作者はサンプラーの値を示さないので LightX2V の値（4 ステップ・cfg 1）を使う（効果は**未確認**）。
- **ライセンス**: `wan22-i2v-14b-lightx2v` は Apache-2.0（ベースも LoRA も。商用可・同意不要）。非検閲の LoRA は上のとおり不明。Remix の 2 つは HF が `license: other`（本文なし）、Civitai は「販売・マージの販売まで可」と表示するが、HF 側の本文が無いので**不明**（#928 の扱いのまま。画面は「不明」の注記）。表示は #928 の `components[]`・バッジ・同意の `notice` に乗る。
- **成人向け・年齢確認**: 3 つの新モデルのうち Remix T2V と非検閲 LoRA は `content_rating = adult`（`advanced`・同意必須）で、既存の成人向けの仕組み（§6.1・§6.2）に乗る。年齢確認が済むまで一覧・導入・生成テストに出ず、導入済みでもワークフロー（`wan22-remix-nsfw-t2v`・`wan22-i2v-14b-lightx2v-uncensored`）は使えない。非検閲 LoRA のファイル名（`Wan2.2_LightX2V_*_n54vv`）は成人向けだけが持つため、別名のワークフローでもグラフがそれを読めば成人向けとして断られる。一般向けの `wan22-i2v-14b-lightx2v` は成人向けにならない（テストで確認）。
- **VRAM と速さ**: `min_gb = 16`・`estimate = true`（Wan 14B fp8 と同じ。LoRA はほぼ増やさない）。「RTX 4070（16 GB）で Remix I2V 576×864・81 フレームが約 326 秒、GGUF（Q5/Q8）でも動く」は Issue のコミュニティ報告で、**mam の実測ではない**（実機が無く、生成時間は書かない）。ComfyUI の自動 offload が 14B fp8 を高・低の順に入れ替える前提。**4 ステップが通常より何倍速いかも未測定**。画面の「VRAM の目安」判定（§4.4）は `recommended_gb` を持たないので、このモデルでは出ない。
- **macOS**: 同梱エンジンは Windows 向けで、mam では macOS に入れられない。外部の ComfyUI（Apple Silicon）では、MPS が fp8（`float8_e4m3fn`）を扱えない既知の問題（§4.2 の FLUX.2 klein と同じ）があり、Remix の fp8・Wan 14B の fp8 とも**動かない可能性が高い**ので、各カードの説明に「Windows（NVIDIA）のみ対応」と書く（実機では未確認）。

**Wan 2.2 TI2V 5B（Issue #929）**。`wan22-ti2v-5b`（Wan-AI の `Wan2.2-TI2V-5B`。ComfyUI の公式の再配布 `Comfy-Org/Wan-2.2`）は、**テキストから動画（T2V）と先頭フレームから動画（I2V）を 1 つの 5B でこなす**軽い動画モデル。720p・24fps。生成テストのモデルは 2 つ（ワークフロー名）: `wan22-ti2v-5b`（テキストから。`txt2video-wan22-5b`）と `wan22-ti2v-5b-i2v`（先頭フレームから。`img2video-wan22-5b`）。同じファイルを使い、どちらを導入しても 2 つが出る。

- **ライセンス**: モデルカードの `license: apache-2.0`（商用可）。カタログは `commercial = yes`。
- **VRAM**: ComfyUI の公式ガイドは「ネイティブ offload なら 8 GB の VRAM に収まる」、Wan の公式の参照実装は offload ありで 24 GB 以上と書く（出典が食い違う）。mam の目安は `min_gb = 16`・`estimate = true`（拡散モデル fp16 の約 10 GB が載る大きさ。**mam の実機（16 GB）では未確認**）。既定の設定は ComfyUI の公式テンプレートと同じ（`UNETLoader` の `weight_dtype = default`、offload は ComfyUI の自動）で、量子化はしない。
- **14B との使い分け**: 速さと軽さを優先するなら 5B（約 18 GB、24fps、T2V も可）、動きの質と映像の作り込みを優先するなら `wan22-i2v-14b`（約 36 GB、16fps、先頭フレームだけ）。モデルのカードの説明（`description`）が互いを名指しして使い分けを書く。
- **生成時間**: mam の実機では未測定（開発ホストの GPU は 12 GB の RTX 3080 Ti で、対象の 16 GB ではなく、5B の重みも取得していない）。Issue の「約 8 GB・720p@24fps が現実的」は ComfyUI の公式ガイドとコミュニティの報告で、**実測値ではない**。
- **macOS**: mam の同梱エンジンは Windows 向け（`EngineView.Supported=false`）。macOS（Apple Silicon）で動くかは、fp16 の重みと Wan 2.2 の VAE の MPS 対応を実機で確かめていないので **未確認**（Windows のみ対応と考える）。

**Wan 2.2 TI2V 5B の fp8 版（Issue #967）**。`wan22-ti2v-5b-fp8`（ワークフロー名 `wan22-ti2v-5b-fp8`＝テキストから・`wan22-ti2v-5b-fp8-i2v`＝先頭フレームから。テンプレート `txt2video-wan22-5b-fp8`・`img2video-wan22-5b-fp8`）は、`wan22-ti2v-5b`（fp16）の**拡散モデルだけを fp8 にした**版。テキストエンコーダー（`umt5_xxl_fp8_e4m3fn_scaled`）と VAE（`wan2.2_vae`）は fp16 版と同じファイルで、導入済みなら共有する（取得は拡散モデルの約 5.3 GB だけ）。

- **ファイル**: `Kijai/WanVideo_comfy_fp8_scaled` のコミット `033a4e487f60220b3d6e469599a6aebc46e13cee` の `TI2V/Wan2_2-TI2V-5B_fp8_e4m3fn_scaled_KJ.safetensors`（5,277,255,650 バイト、SHA-256 `a83f54a2…6b48`）。Comfy-Org の `Comfy-Org/Wan-2.2` には 5B の fp8 版が無い（fp16 だけ）ので、コミュニティの変換を使う（e5m2 版も同じリポジトリにあるが、ComfyUI の標準の fp8 の形式である e4m3fn を選んだ）。第三者のミラーではなく、変換者自身のリポジトリ。SHA-256 は HF の LFS の oid（`X-Linked-Etag`）で、**ダウンロードしたファイルで計算した値とも一致**を 2026-11-02 に確認した。
- **ComfyUI が読めること**: ファイルの safetensors のヘッダーは、fp16 版（`Comfy-Org/Wan-2.2`）と同じキー（`blocks.N.*`・`head.head.*`・`patch_embedding.*` ほか。メタデータの `model_type` は `TI2V-5B`）に、`scaled_fp8` の目印と層ごとの `scale_weight` を足した形。ComfyUI v0.42.1 の `comfy.sd.load_diffusion_model_state_dict`（CPU）で読み込んで `WAN22` のモデルとして組み立てられることを確かめた。**GPU での fp8 の実行・生成は未確認**。
- **テンプレート**: fp16 版の 2 つとグラフも割り当ても同じで、`UNETLoader` の `unet_name` だけが fp8 のファイル（`weight_dtype` は `default`）。ComfyUI v0.42.1 の `execution.validate_prompt`（CPU・ファイル名だけのダミーのモデルを置いて実行）で、2 つとも検証を通ることを確認した。
- **ライセンス**: Kijai のモデルカードは `license: apache-2.0`（`base_model` は古い Wan 2.1 のままだが、ファイルのメタデータは TI2V-5B）。元の `Wan-AI/Wan2.2-TI2V-5B` も Apache-2.0（2026-11-02 に確認）。`commercial = yes`、部品（拡散モデル・テキストエンコーダー・VAE）ごとに書く。
- **VRAM**: `min_gb = 16`・`estimate = true`（fp16 版と同じ）。fp8 で拡散モデルの重みは約 10.0 GB から約 5.3 GB に減るが、**下限を下げる根拠になる測定が無い**ので、fp16 版より低い値は書かない。16 GB の PC で fp16 版より余裕を持って載るはず、という説明にとどめる。画質が fp16 版からどれだけ落ちるか、生成時間・VRAM は **未計測**（§4.5 の記入欄）。
- **macOS**: fp8 は MPS で動かない既知の問題（下の FLUX.2 klein と同じ）があり、Windows のみ対応と説明に書く。

**FLUX.2 [klein] 4B（Issue #931）**。`flux2-klein-4b`（Black Forest Labs の FLUX.2 [klein] 4B を ComfyUI の公式ガイドの **fp8 蒸留版**で入れる）は、**テキストから画像と、最大 4 枚の参照画像を使った編集**を 1 つのモデルでこなす高速な画像モデル（蒸留版は 4 ステップで数秒）。ワークフローは 1 つ（名前 `flux2-klein-4b`、テンプレート `txt2img-flux2-klein`）で、参照画像が無い実行はテキストから画像、あれば編集になる。

- **ファイル**（SHA-256・サイズは Hugging Face が返す LFS の oid・サイズ。2026-11-02 に取得）: 拡散モデル `diffusion_models/flux-2-klein-4b-fp8.safetensors`（BFL 公式の `black-forest-labs/FLUX.2-klein-4b-fp8`。ミラーではない・gated ではない。約 4.1 GB）、テキストエンコーダー `text_encoders/qwen_3_4b.safetensors`（Qwen3 4B。`Comfy-Org/Flux-2-Klein-4B`。約 8.0 GB）、VAE `vae/flux2-vae.safetensors`（同じ `Comfy-Org/Flux-2-Klein-4B`。約 0.3 GB）。公式のテンプレートは VAE を `Comfy-Org/flux2-dev` から取るが、そのリポジトリは FLUX.2 [dev]（非商用）の再配布で、klein の `flux2-vae.safetensors` は **Apache 2.0 の klein のリポジトリにもある**ので、そちらを使う（2 つはテンソルの名前・形式が違うファイルで、SHA-256 も別）。
- **9B と dev は入れない**: FLUX.2 [klein] 9B と FLUX.2 [dev] は非商用のライセンスで、モデルカードも 9B を非商用と書く。
- **ライセンス**: モデルカード（`black-forest-labs/FLUX.2-klein-4B` と `-4b-fp8`）の `license: apache-2.0`・本文は Apache License 2.0（`LICENSE.md`）。`commercial = yes`。内訳（`components[]`）は拡散モデル・テキストエンコーダー（Qwen3 4B、Apache-2.0）・VAE（klein のリポジトリの Apache-2.0）の 3 つで、すべて可。モデルカードは使用範囲の制限（違法・児童の搾取・同意のない性的な画像など）も別に書く（ライセンスの条件ではない）。
- **検閲**: 公式は NSFW・児童の性的な画像を抑えるための安全調整と、推論コードのフィルターを持つ（モデルカードの Responsible AI Development）。成人向けは `flux1-dev-nsfw-master`・`noctq-v4-qwen-image21` に任せ、カードの説明にもそう書く。
- **VRAM**: BFL のモデルカードは bf16 で約 13 GB、ComfyUI の公式ガイドは fp8 蒸留版で 8.4 GB（RTX 5090 での値）と書く。mam の目安は `min_gb = 12`・`estimate = true`（fp8 の 4.1 GB にテキストエンコーダーの 8 GB が載るかどうかを見た値）。**mam の実機（16 GB）では未確認**。既定は fp8 蒸留版のまま、ComfyUI の自動 offload に任せる。
- **参照画像による編集**（local-models.md §4.2）: `reference_mode = edit`・最大 4 枚。参照は画像ごとに `ImageScaleToTotalPixels`（約 1 メガピクセル）→ `VAEEncode` → `ReferenceLatent` を肯定・否定の条件に順に重ねる（ComfyUI の公式テンプレート `image_flux2_klein_image_edit_4b_distilled` と同じ。公式は 2 枚の例）。生成テスト・MCP の参照の入力は #933 の参照画像の仕組みに乗る。出力の大きさは公式の編集テンプレートのように 1 枚目の参照の大きさではなく、縦横比から決まる（画面の縦横比の指定に従う）。
- **生成時間**: mam の実機では未測定。公式ガイドの 1.2 秒（RTX 5090）はコミュニティ・公式の値で、mam の対象の RTX 4070 Ti SUPER の実測ではない。
- **macOS**: mam の同梱エンジンは Windows 向け。macOS（Apple Silicon）は、ComfyUI の MPS が fp8（`float8_e4m3fn`）を扱えず `Trying to convert Float8_e4m3fn to the MPS backend` で止まる既知の問題がある（Comfy-Org/ComfyUI の Issue #10292・#9255・#11626。CPU への退避の PR #12378 は 2026-11-02 時点で未マージ）。fp8 蒸留版を既定にしているため、**macOS では動かない可能性が高く、Windows のみ対応と明記する**（bf16 の `flux-2-klein-4b.safetensors` なら動くかは未確認。実機で試していない）。
- **bf16 版（macOS 用）は追加しない（Issue #967 の検討結果）**。理由: (1) **モデルマネージャーが macOS に ComfyUI を入れられない**（§3.1 のエンジンはすべて Windows のポータブル版、`EngineView.Supported=false`）。macOS 専用のエントリを足しても導入できない。カタログのスキーマにも OS の欄が無い。(2) Windows では fp8 版が使える（bf16 を足しても、ファイルが大きくなる以外の利点が無い）。(3) macOS（MPS）で bf16 の klein が動くことは、実機が無く未確認。足す条件は、macOS にエンジンを入れる経路ができること（§3）。そのときは `Comfy-Org/Flux-2-Klein-4B` のコミット `5b3abca5c0dc1ecd4eaf3340868d09063fc1c965` の `split_files/diffusion_models/flux-2-klein-4b.safetensors`（7,751,105,712 バイト、SHA-256 `ec3d4e733a771f61c052fb4856c48b336c55eaf2c65487c2a1faeb9bbda7a343`。Apache-2.0。2026-11-02 に HF の API で確認）を、テキストエンコーダー・VAE は今のファイルと共有して `flux2-klein-4b-bf16` として足す。テスト（`lightmodelstest`）が「カタログに bf16 の klein が無いこと」と「エンジンが Windows のビルドだけであること」を押さえる。

ワークフローは `localcfg/templates/image/`・`templates/video/` の **コアノード（ComfyUI 本体のノード）だけ**で書く。SDXL と Wan 2.2 は既存のテンプレートを使い、FLUX schnell/dev・SD3.5・Qwen-Image・LTX・HunyuanVideo のテンプレートを足した（`txt2img-flux-schnell` など 7 つ）。成人向けの追加で、`txt2img-qwen-image21`（`TextEncodeQwenImage21`、25 ステップ・euler/simple・cfg 3）・`txt2img-chroma`（`CLIPLoader type=chroma`、`T5TokenizerOptions`、`BetaSamplingScheduler`、`CFGGuider` cfg 3.8）・`txt2img-flux-dev-lora`（`LoraLoaderModelOnly`、強さ 0.8）・`img2video-wan22-14b`（高ノイズ→低ノイズの 2 段の `KSamplerAdvanced`、`WanImageToVideo`、16fps）・`img2video-wan22-14b-remix`（同じ形、12 ステップ・cfg 1）・`img2video-wan22-14b-lora`（高・低のそれぞれに `LoraLoaderModelOnly`、強さ 1）の 6 つを足した（合計 17）。`catalog.Validate` は、各テンプレートのローダーノード（`CheckpointLoaderSimple`・`UNETLoader`・`CLIPLoader`・`DualCLIPLoader`・`VAELoader`・`LoraLoaderModelOnly`）が、そのモデルのファイル名（と `loras` などのフォルダ）と一致することを確かめる。**テンプレートは構造テストだけで、GPU 実機では未検証**（画面にも「構造テストのみ・GPU 実機未検証」と出す。`ModelView.WorkflowCheck`）。

**LTX-2.5（Issue #877）**。`ltx-2.5-22b-distilled`（Lightricks 公式の `Lightricks/LTX-2.5`。ComfyUI のネイティブワークフローが使う蒸留版の 22B）。映像と同期した音声を 1 回の生成で作る。旧 `ltx-video-2b`（LTX-Video 2B v0.9.5）は**残す**（別のモデル・別のテンプレートで、既存の割り当てを壊さない。VRAM が小さい経路として使い続けられる）。

- **ファイル**（`Lightricks/LTX-2.5` の固定コミットの LFS オブジェクト。SHA-256・サイズは Hugging Face が返す LFS の oid とサイズ。第三者のミラーは使わない）: `diffusion_models/ltx-2.5-22b-distilled-transformer-comfy-int8-convrot.safetensors`、`text_encoders/gemma4-12b-with-proj-ltx-2.5-comfy-int8-convrot.safetensors`、`vae/ltx-2.5-video-vae-bf16.safetensors`、`vae/ltx-2.5-audio-vae-bf16.safetensors`、`latent_upscale_models/ltx-2.5-latent-spatial-upscaler-x2-bf16-1.0.safetensors`。ComfyUI の公式ワークフロー（Template Library の LTX-2.5 T2V・I2V）が読むファイルと同じ。プロンプト拡張用の `gemma4_e2b_it_int8_convrot`（`Comfy-Org/gemma-4`。Apache-2.0・ゲートなし）は、#927 では入れなかったが、**#968 で標準モデルに加えた**（下の「プロンプト拡張（Issue #968）」）。
- **`latent_upscale_models` フォルダ**: 公式のワークフローは 2 段で作る（半分の解像度で作り、潜在空間で 2 倍に拡大してから仕上げる）。拡大モデルを読む `LatentUpscaleModelLoader` の入力 `model_name` はこのフォルダのファイルなので、カタログの `files[].folder` と `catalog.Validate` のローダー表（`loaderInputs`）に足した。外部の ComfyUI の確認（`ExternalInventory`）もこのフォルダを読む。HF からの導入（model-manager-hf.md）のフォルダは 5 つのまま変えない。
- **ライセンス**: モデルカード（Hugging Face の `README.md`）の `license: other`・`license_name: ltx-2.x-community-license-agreement`、本文は `https://github.com/Lightricks/LTX-2/blob/main/LICENSE-2_x`（2026-08-11 版）。年間売上が 1,000 万 US ドル未満の個人・組織は、商用・制作での利用も無償（売上は子会社・関連会社を合算して数える）。1,000 万 US ドル以上の組織は、個人の趣味・研究や非本番の評価のような非商用の目的を除き、有償の商用ライセンス（licensing@ltx.io）が要る。派生モデルの第三者への譲渡、用途の制限（Attachment A）、AI 規制（EU AI Act など）の遵守と、透かし・開示の機能を外さないことが条件。カタログでは `commercial = conditional`・`commercial_condition`（売上の条件）・`consent_required = true`・`tier = advanced`（制限付きのライセンスは同意が要る。§6）。Hugging Face のゲートの文面は、プライバシーポリシーへの同意と、案内・広告の受け取りへの同意（「Agree and Access」）。
- **VRAM（Issue #927）**: `min_gb = 16`・`recommended_gb = 32`・`estimate = true`。推奨の 32 GB 以上は Lightricks の文書（docs.ltx.io の ComfyUI の前提条件「CUDA-compatible GPU with 32GB+ VRAM」。2026-11-02 に確認）。16 GB で動くのは、INT8（ConvRot）の拡散モデルとテキストエンコーダー・タイル VAE・オフロードを使った場合というコミュニティの報告（Issue #927）で、**mam の実機（RTX 4070 Ti SUPER）では未検証**。16 GB の PC が何を諦めるかは `limits` に書く（§4.4）。
- **16 GB 向けの既定（Issue #927）**: 既定のファイルとテンプレートが、もともと低 VRAM の組み合わせ。拡散モデルとテキストエンコーダーは INT8（ConvRot。Lightricks の「Lower-VRAM model files」）、映像のデコードは `VAEDecodeTiled`（タイル 512・時間方向 64。全フレームを一度にデコードしない）、重みの退避（オフロード）は ComfyUI v0.42.1 の標準の動き（`--lowvram` などの起動オプションは付けない。動的 VRAM が既定で有効）。BF16（42 GB の拡散モデル・26 GB のテキストエンコーダー）と NVFP4（Blackwell 向け）は入れない。
- **非検閲のテキストエンコーダー（Issue #927）**: `ltx-2.5-22b-distilled-uncensored`（成人向け・同意必須・年齢確認が要る）。標準の `gemma4-12b-with-proj-ltx-2.5-comfy-int8-convrot` だけを、`DeepNeuralNerd/Gemma-4-12B-it-uncensored-heretic-DeepNeuralNerd-LTX_2.5_ComfyUI`（`llmfan46/gemma-4-12B-it-uncensored-heretic` を LTX-2.5 の投影の重みを保ったまま ComfyUI の 1 ファイルに変換したもの）の INT8 ConvRot 版（13.2 GB、コミット `f8aad43b…`、SHA-256 `5da6d41c…`、HF が返す LFS の oid とサイズ）に替える。残りの 4 ファイル（拡散モデル・VAE 2 つ・拡大モデル）は標準と同じファイルで、共有される（導入済みなら再取得しない）。ワークフローは別（`ltx-2-5-uncensored-t2v`・`ltx-2-5-uncensored-i2v`。テンプレート `txt2video-ltx-2-5-uncensored`・`img2video-ltx-2-5-uncensored` は標準の複製で `CLIPLoader` の `clip_name` だけが違う）。元のファイル名は空白を含むので、ディスク上の名前は `gemma4-12b-uncensored-heretic-deepneuralnerd-ltx-2.5-int8-convrot.safetensors` に改め、URL は空白を `%20` に符号化する。この名前は成人向けのモデルだけが持つファイルなので、§6.1 の判定（名前・読むファイル）がそのまま働く（標準のテンプレートは成人向けとは判定されない）。作者自身が「テキストエンコーダーを替えても映像モデルの出力が非検閲になるわけではない（通常のプロンプトの取りこぼしを減らす狙い）」「保証しない」と書いており、mam は動作を確かめていない（§11）。
- **ライセンス（非検閲版）**: 部品ごとに書き、厳しい方を全体の答えにする（[../model-license.md](../model-license.md) §2）。拡散モデル・VAE は LTX-2.x Community（条件つき）、テキストエンコーダーの元の Gemma 4（Google の Apache License 2.0）と非検閲化した `llmfan46/gemma-4-12B-it-uncensored-heretic`（HF のタグ `apache-2.0`）は商用OK、LTX-2.5 用に変換した DeepNeuralNerd のリポジトリは `license: other` で本文が無く「上流の Gemma 4 と LTX-2 のライセンスを参照」とだけ書いてある（LTX-2.5 の投影の重みを含むので LTX-2.x の条件も及びうる）ため**不明**。全体は **不明**（2026-11-02 に確認）。通知には LTX-2.x の売上の条件（licensing@ltx.io）も残す。
- **音声つきの出力（Issue #927）**: 保存は `SaveVideo`（`format` / `codec` ともに `auto`）で、ComfyUI v0.42.1 の `comfy_api/latest/_input_impl/video_types.py` では `auto` は MP4（H.264 の映像と AAC の音声。WebM を選んだときだけ Opus）。`CreateVideo` が `LTXVAudioVAEDecode` の音声と映像を 1 本にまとめるので、保存されるのは**音声トラックつきの 1 本の mp4**。mam のバックエンド（`comfyui`）は `SaveVideo` が保存したファイルをそのまま（再エンコードせず）取り出して `video/mp4` で保存・配信する（`take`・Take の再生と生成テストの結果は同じ）。画面の `<video controls>`（試し生成・生成テストのカードと詳細）は音声を消さないので、再生すると音が出る。画面は、音声つきのモデル（`native_audio`）を、試し生成のフォームの注記・結果の「音声つき」バッジ・プレーヤーの名前（`aria-label`）・生成テストのモデルのヘルプで示す。**実際に音が入っていること、音声と映像のずれは、実機で未確認**（§11）。
- **macOS（Issue #927）**: **対応しない（Windows の NVIDIA のみ）**。同梱の ComfyUI は Windows 版だけで（§3）、macOS ではモデルマネージャーでの導入ができない。外部の ComfyUI（Apple Silicon の MPS）で LTX-2.5 を動かす報告は割れている: `Lightricks/LTX-2.5` の discussion #58 では、公式テンプレートが PyAV の `avcodec_send_frame() returned 22` で失敗し（NaN の潜在変数が原因）、KJNodes のローダーで回避した報告と、M4 Max（36 GB）で標準のワークフローがそのまま動いた報告がある。ComfyUI の Issue #15804（open）は、MPS で bf16 の LTX-2.x が 26 回中 19 回、全面が黒い動画になると報告している（アテンションの未初期化メモリが原因とされ、修正は未マージ）。Lightricks 自身の Mac 向けの案内は、ComfyUI ではなく LTX Desktop（MLX）。INT8 ConvRot が MPS で動くかは確認できる情報が無い。このため mam は macOS を対応外とし、外部の ComfyUI での利用は「動く場合もあるが不安定。mam では未確認」とする。
- **ComfyUI-LTXVideo（Lightricks のカスタムノード）は入れない（Issue #927・#968）**: LTX-2 は ComfyUI の本体に組み込まれており（`ComfyUI-LTXVideo` の README: "LTX-2 is built into ComfyUI core"）、Comfy 公式のテンプレート `video_ltx2_5_t2v` が使うノードはすべて `comfy-core`（同テンプレートの JSON の `cnr_id` で確認）。ComfyUI-LTXVideo は追加の機能（プロンプト拡張・マルチショットの補助など）のノード集で、このテンプレートには要らない。同梱エンジンはカスタムノードを読まない（`--disable-all-custom-nodes`。任意のコードを実行するため。§3.4）ので、自動導入の仕組みは作らない。**#968 の FLF2V・DFR・プロンプト拡張も、コアのノードだけで組めた**（下の「カスタムノードの自動導入（Issue #968）」）。ネイティブのマルチショット（1 回の生成で複数のカットを作る。Comfy の文書の紹介）は、mam では専用のテンプレート・画面を作らない（mam のショットは 1 カット 1 生成）。ComfyUI の文書にそのためのノードの案内は見当たらなかったが、動作は未確認。
- **テンプレート**（`localcfg/templates/video/`。ほかと同じく ComfyUI 本体のコアノードだけ）: `txt2video-ltx-2-5`（ワークフロー名 `ltx-2-5-t2v`）と `img2video-ltx-2-5`（`ltx-2-5-i2v`）。Comfy 公式の `video_ltx2_5_t2v`・`video_ltx2_5_i2v`（Comfy-Org/workflow_templates）のサブグラフを API 形式に展開し、mam の割り当てに接続した。24 fps、フレーム数は 8n+1（`frames_step` 8）。1 段目は最終の解像度の半分（`ComfyMathExpression` の `a/2`）で、`LTXVLatentUpsampler` で 2 倍にして 2 段目で仕上げる（このため最終の幅・高さは 64 の倍数 = `size_multiple` 64）。割り当て: `prompt`・`negative_prompt`・`seed`（1 段目の `RandomNoise`）・`width`・`height`・`frames`・`fps`、I2V は `first_frame`。サンプラー・シグマ・ガイダンスは公式の値（`euler_ancestral`、蒸留版の `ManualSigmas`、`LTXVDualCFGGuider` の映像・音声とも 1）。`native_audio` は true。この 2 つのテンプレートは、公式のプロンプト拡張（`TextGenerateLTX2Prompt`。公式は ON/OFF のスイッチ）を入れない（拡張つきは別名のワークフローとして #968 で足した。下）。画像の長辺 1536 への縮小（`ResizeImageMaskNode`。`LTXVImgToVideoInplace` が潜在空間の大きさへ合わせる）も入れない。マルチショットのテンプレートは範囲外。FLF2V・DFR は #968 で足した（下）。
- **同梱エンジン（ComfyUI v0.42.1）が必要なコアノードを持つか**: このテンプレートが使うノード（`LTXVConcatAVLatent`・`LTXVSeparateAVLatent`・`LTXVLatentUpsampler`・`LTXVAudioVAEDecode`・`LTXVEmptyLatentAudio`・`LTXVDualCFGGuider`・`LTXVImgToVideoInplace`・`LTXVPreprocess`・`LTXVConditioning`・`EmptyLTXVLatentVideo`・`LatentUpscaleModelLoader`・`ComfyMathExpression`・`CreateVideo`・`SaveVideo`・`SamplerCustomAdvanced` ほか）は、v0.42.1 のソース（`comfy_extras/*.py`・`nodes.py`）に実装がある。`CLIPLoader type=ltxv` の Gemma 4 12B の検出（`comfy/sd.py` の `TEModel.GEMMA_4_12B`）も v0.42.1 にある。**実際の読み込み・実行は未確認**（§11）。
- **テンプレートの置き場所（Issue #968）**: #968 で足した LTX-2.5 のテンプレート 9 つ（FLF2V・拡張つき・NVFP4・DFR）は `localcfg/templates/video/ltx25/` に置く（`templates/video/` が行数の上限〔1 ディレクトリ 2000 行。`.linterly.yml`〕に近いため。`localcfg.Templates()` は 3 つのディレクトリを読む）。
- **FLF2V（最初と最後のフレームから動画。Issue #968）**: テンプレート `flf2v-ltx-2-5`（ワークフロー名 `ltx-2-5-flf2v`）。Comfy 公式の `video_ltx2_5_flf2v`（Comfy-Org/workflow_templates）のサブグラフを API 形式に展開した。**1 段**（拡大モデルを使わない）で、最初のフレームを `LTXVAddGuide`（`frame_idx` 0・強さ 0.7）、最後のフレームを同（`frame_idx` -1・0.7）で空の潜在に固定する。`first_frame`・`last_frame` の 2 つの割り当てで、モデルが 2 枚の画像を取る（`VideoCaps.LastFrame`）。2 枚は同じ縦横比にする。幅と高さは 32 の倍数（`size_multiple` 32。モデルカードの制約）。同じグラフで拡散モデルだけ NVFP4 にした `flf2v-ltx-2-5-nvfp4`、テキストエンコーダーだけ非検閲にした `flf2v-ltx-2-5-uncensored` があり、標準・NVFP4・非検閲の各モデルが持つ（DFR のモデルは持たない）。1 段なので拡大モデルを読まないが、カタログの検証（§4.3）は「モデルのどれかのワークフローが読むファイル」を使う。
- **NVFP4（Issue #968）**: `ltx-2.5-22b-distilled-nvfp4`（ワークフロー名 `ltx-2-5-nvfp4-t2v`・`-i2v`・`-flf2v`）。`diffusion_models/ltx-2.5-22b-distilled-transformer-nvfp4.safetensors`（18,721,548,408 バイト、SHA-256 `4b94231e…6c30`。INT8 版の約 20.0 GiB に対し約 17.4 GiB。`Lightricks/LTX-2.5` の同じコミット）に替えるだけで、残りの 4 ファイル（テキストエンコーダー・VAE 2 つ・拡大モデル）は標準モデルと同じファイル（共有）。**NVFP4 は GPU の世代に依存する**: Lightricks の文書（docs.ltx.io の ComfyUI のページ）は「FP4 対応の NVIDIA Blackwell の GPU 向け」と書き、ComfyUI v0.42.1 は compute capability が 10 未満の GPU で nvfp4 の演算を使わない（`comfy/model_management.py` の `supports_nvfp4_compute`）。カタログは GPU の世代を持たない（メモリの量だけ）ので、**説明（`description`）と `vram.limits` に「RTX 50 世代向け・それ以前は INT8 版を選ぶ」と書く**（「Windows のみ」と同じ、説明で伝える流儀）。VRAM は INT8 版と同じ値（16 GB・推奨 32 GB。推定）を使い、NVFP4 では測っていない。**mam の実機（RTX 4070 Ti SUPER）は Blackwell ではないので、動作は確認していない**。テンプレートは INT8 版と同じグラフで、`unet_name` だけが NVFP4 のファイル。
- **プロンプト拡張（Issue #968）**: Comfy 公式の `TextGenerateLTX2Prompt`（コアのノード）が、Gemma 4 E2B のテキストエンコーダーで短いプロンプトを詳しい指示（映像と音の描写）に書き換える。ファイルは `Comfy-Org/gemma-4` のコミット `63d0f7c476756b88910170c1df75e2384ea1af31` の `text_encoders/gemma4_e2b_it_int8_convrot.safetensors`（5,199,997,904 バイト、SHA-256 `efeca0fc…7566`。リポジトリのカードは `license: apache-2.0`・ゲートなし）。**標準モデル `ltx-2.5-22b-distilled` にだけ加え**（合計 44.9 GB。ライセンスの部品に Apache-2.0 を足したが、全体の答えは LTX-2.x の条件つきのまま）、ワークフローを 3 つ足した: `ltx-2-5-t2v-enhanced`・`ltx-2-5-i2v-enhanced`・`ltx-2-5-flf2v-enhanced`（テンプレート `txt2video-ltx-2-5-enhanced`・`img2video-ltx-2-5-enhanced`・`flf2v-ltx-2-5-enhanced`）。元のテンプレートに、拡張用の `CLIPLoader`（`type=ltxv`）と `TextGenerateLTX2Prompt` の 2 ノードを足し、`prompt` の割り当てを拡張のノードに向けて、その出力を正のプロンプトの `CLIPTextEncode` に渡す（負のプロンプトは書き換えない。I2V・FLF2V は先頭フレームの画像も拡張に見せる）。拡張のサンプリングの乱数は `seed` の割り当てを使う。公式のテンプレートは ON/OFF のスイッチで（Comfy のテンプレートの JSON の入力値は OFF。LTX の文書は「既定で有効」と書き、食い違う。Comfy の文書は拡張に約 5 GB のモデルと 1〜2 分の生成時間が足されると書く）、mam の割り当てにスイッチは無いので、**拡張を常に使うワークフローと使わないワークフローを別名で持つ**。非検閲のモデル・NVFP4・DFR には付けない（非検閲の組み合わせに通常の Gemma 4 を通すと意図に反する。残りは必要が出てから）。**導入済みの標準モデルは、ファイルが 1 つ増えるので「不完全」（`ModelIncomplete`）と表示される**。導入し直すと拡張のファイルだけ取得する（共有のファイルは再取得しない）。
- **DFR（Diffusion Fidelity Rendering。Issue #968）**: `ltx-2.5-22b-distilled-dfr`（ワークフロー名 `ltx-2-5-dfr-i2v`、テンプレート `img2video-ltx-2-5-dfr`）。Comfy 公式の `video_ltx2_5_dfr_i2v`（先頭フレームから。ComfyUI の文書では「画像から、または `Switch to T2V` でテキストから」）の 88 ノードのサブグラフを API 形式に展開し（拡張・スイッチを固定して不要なノードを除いた 71 ノード）、割り当ては `prompt`・`negative_prompt`・`seed`（3 段の `RandomNoise` に同じ値）・`width`・`height`・`frames`（映像と音声の潜在）・`fps`・`first_frame`。**3 段**: 半分の解像度で、1 秒に 1 枚のキーフレームを一緒に作る（`LTXVAddGeneratedKeyframes`）→ 空間を 2 倍に拡大し、詳細化の IC-LoRA で 2 段目（3 ステップ）→ 時間方向に 2 倍（`ltx-2.5-latent-temporal-upscaler-x2`、48 fps になる。長さは変わらない）で 3 段目（4 ステップ）。ファイルは標準モデルと同じ 4 つに、時間方向の拡大モデル（同じリポジトリ。261,944,000 バイト、SHA-256 `2bc3300f…c3fe`）と、**別のゲートのリポジトリ** `Lightricks/LTX-2.5-22b-IC-LoRA-Pixel-Spatial-Upscaler` のコミット `59f26f1e0c11af42f1affa924b4ef231e9e98b2a` の `ltx-2.5-22b-ic-lora-pixel-spatial-upscaler-x2-1.0.safetensors`（327,322,640 バイト、SHA-256 `984851b7…1c1d`。モデルカードの `license_name: ltx-2.x-community-license`＝LTX-2.x Community。商用は年間売上の条件つき）を足した（合計 40.3 GB）。**2 つのリポジトリでそれぞれライセンスに同意してアクセスの承認を受ける必要がある**（導入のアクセス確認 `hfguide.CheckAccess` はファイルのリポジトリごとに先頭 1 バイトを確かめるので、2 つ目が未承認なら導入のジョブを作る前に断る）。**開発ホストの Hugging Face アカウントは、2 つ目のリポジトリのアクセスが未承認**なので、そのリポジトリのファイルの SHA-256・サイズ・コミットは、モデルの API（`/api/models/<repo>?blobs=true`。ゲートでもメタデータは返る）の値で、ファイルの本体の取得とヘッダーの確認はしていない。プロンプト拡張は付けない（公式の DFR のテンプレートの `refine_prompt` も OFF。付けるなら別モデルの判断）。3 段で時間方向の拡大も含むので、`ltx-2.5-22b-distilled` の I2V より時間もメモリも多く要る見込みで、16 GB の PC では特に遅くなる（**未計測**）。
- **dev 版は入れない（Issue #968 の `not_done`）**: `ltx-2.5-22b-dev-transformer-comfy-int8-convrot.safetensors`（21,504,034,224 バイト、SHA-256 `2edbdb44…f92b`、`Lightricks/LTX-2.5` の同じコミット。HF のモデルカードは「Full / trainable DiT」）は、HF にはあるが、**ワークフローの根拠が無い**。Comfy 公式のテンプレート（T2V・I2V・FLF2V・DFR）と ComfyUI-LTXVideo の `example_workflows/2.5/` の 13 本はすべて蒸留版の拡散モデルを使い（README: "All of them use the distilled transformer"）、dev 版を読む公式のワークフローが 2.5 には無い。dev 版は蒸留版と違うサンプリング（蒸留版は 8 ステップ・CFG 1 の固定のシグマ。dev 版は多いステップ・CFG・ガイダンスのはず）と、モデルカードが「dev-transformer workflows 用」と書く蒸留 LoRA（`loras/ltx-2.5-22b-distilled-lora-450-bf16.safetensors`、8,899,889,568 バイト、SHA-256 `86370bbf…bbf5`）を重ねる手順が要るはずだが、その値を決める出典が無い。**根拠なしに値を推測してワークフローを作ることはしない**（実機で確かめられず、16 GB の構成に 8.9 GB の LoRA を重ねる重い構成でもある）。足す条件は、Lightricks か Comfy が 2.5 の dev 版の ComfyUI のワークフローを公開すること、または実機で値を決められること。そのときは上の INT8 の dev 版と LoRA を、標準モデルと同じコミットのファイルとして足す。
- **カスタムノードの自動導入（Issue #968）は不要で、作らない**: その時点では、モデルマネージャーにカスタムノードを導入する仕組みは無かった（同梱の ComfyUI は `--disable-all-custom-nodes` で起動する。§3.4。許可リストの仕組みは Issue #976: [custom-nodes.md](custom-nodes.md)）。そして今回足したテンプレートはすべてコアのノードだけで動く。FLF2V・DFR・プロンプト拡張が使うノード（`LTXVAddGuide`・`LTXVCropGuides`・`LTXVAddGeneratedKeyframes`・`LTXVSeparateGeneratedKeyframes`・`LTXVGeneratedKeyframesToGuides`・`LTXVAddLatentGuide`・`LTXVFreezeLatent`・`TextGenerateLTX2Prompt`・`ResizeImageMaskNode`・`ComfySwitchNode`・`PrimitiveBoolean`・`PrimitiveFloat`・`SamplerEulerAncestral`・`GetImageSize` ほか）は、ComfyUI v0.42.1 のソース（`comfy_extras/nodes_lt.py`・`nodes_lt_keyframes.py`・`nodes_textgen.py` など）にあり、**v0.42.1 を CPU で起動して `execution.validate_prompt` に通し（ファイル名だけのダミーのモデルを置く）、新しいテンプレートが検証を通ることを確認した**（`flf2v-ltx-2-5`・`-enhanced`・`-nvfp4`、`txt2video-ltx-2-5-enhanced`・`img2video-ltx-2-5-enhanced`、`txt2video-ltx-2-5-nvfp4`・`img2video-ltx-2-5-nvfp4`、`img2video-ltx-2-5-dfr`、`txt2video-wan22-5b-fp8`・`img2video-wan22-5b-fp8`。`flf2v-ltx-2-5-uncensored` は元の `flf2v-ltx-2-5` とテキストエンコーダーのファイル名だけが違うグラフ）。ノードの存在と入力の型・値の範囲までの確認で、モデルの読み込みと生成は行っていない。`localcfg` の `coreNodes`（テンプレートが使ってよいクラスの一覧）に、これらのクラスを足した。

**ControlNet と IP-Adapter（Issue #976）**。SDXL に ControlNet（Canny）と IP-Adapter を足すモデル 2 つと、そのためのカスタムノードの許可リスト（[custom-nodes.md](custom-nodes.md)）。

- **`sdxl-controlnet-canny`**（テンプレート `txt2img-sdxl-controlnet`）: `xinsir/controlnet-canny-sdxl-1.0`（コミット `1271357e…`、`diffusion_pytorch_model.safetensors` 2,502,139,104 B、SHA-256 `bf47cd75…ff47c`、カードは `license: apache-2.0`、diffusers 形式の F16）。ComfyUI v0.42.1 の `load_controlnet_state_dict` が diffusers 形式を読む（キーを確認）。**コアのノード（`ControlNetLoader`・`ControlNetApplyAdvanced`）だけで動く**ので追加ノードは要らない。ファイルは `controlnet/xinsir-controlnet-canny-sdxl-1.0.safetensors` に置く。前処理は無いので、制御画像（参照画像）は線画にした画像を渡す。
- **`sdxl-ipadapter-plus`**（テンプレート `txt2img-sdxl-ipadapter`、`requires_nodes: ["ipadapter-plus"]`）: `h94/IP-Adapter` のコミット `018e4027…` の `sdxl_models/ip-adapter-plus_sdxl_vit-h.safetensors`（847,517,512 B、SHA-256 `3f5062b8…6581`）と `models/image_encoder/model.safetensors`（CLIP ViT-H、2,528,373,448 B、SHA-256 `6ca9667d…b030`。`clip_vision/CLIP-ViT-H-14-laion2B-s32B-b79K.safetensors` に置く）。カードは `apache-2.0`、laion の CLIP ViT-H は `mit`。ノード `ipadapter-plus` を入れるまで、導入も選択もできない（custom-nodes.md §6）。
- VRAM の目安はどちらも 10 GB（推定。SDXL ベースの 8 GB に ControlNet・IP-Adapter の分を足した）で、GPU では未計測（§11）。商用利用の答えは各ファイルのライセンスの最も厳しいもの（部品ごとの内訳は `license.components`）。

### 4.3 検証（`catalog_test.go`）

すべてのファイルに https の固定 URL・64 桁の SHA-256・正のサイズがあること、同意が要るモデルにライセンス情報が揃うこと、ID と（フォルダ, 名前）の重複が無いこと、テンプレートが存在し構造が正しいこと。`content_rating` が `general` か `adult` で、`adult` は `advanced`（同意必須）であること、`license.id = unspecified` は商用利用が `yes`・`conditional` でないこと（`no` か `unknown`。`validate.go` の `validateRating`）、`trigger_words` は `loras` のファイルを持つモデルだけにあること、`source_note` に確認の日付があること、`loras` のファイルと `LoraLoaderModelOnly` のローダーがモデルのファイルと一致すること、`LatentUpscaleModelLoader` の `model_name` が `latent_upscale_models` のファイルと一致すること。新しい 6 モデルの URL・SHA-256・サイズは、Hugging Face の LFS の oid とコミットに照らした値で固定されている（`adult_test.go`）。**`requires_hf_token` が true のモデル**（LTX-2.5）は、すべてのファイルが `huggingface.co` の固定 URL で、第三者のミラーの注記（`source_note`）を持たないこと（ログインの要るファイルを第三者のミラーに置き換えない）。`ltx-2.5-22b-distilled` のファイル名・SHA-256・サイズ・コミットは `ltx25_test.go` が固定する。 `vram` は `recommended_gb` と `limits` が一緒で、`recommended_gb` が `min_gb` より大きいこと（`limits` だけ・`recommended_gb` だけ・同じか小さい値は拒む）。非検閲の LTX-2.5（`ltx-2.5-22b-distilled-uncensored`）は、テキストエンコーダー以外の 4 ファイルが標準と同じ（パス・SHA-256・サイズ）で、ワークフローが非検閲のテキストエンコーダーだけを読むこと（`catalog/ltx25test`、`adult/ltx25_test.go`）。

「カタログのファイルが、そのモデルのどれかのワークフローに読まれること」を検証する（Issue #968）。それまでは、モデルの**各**ワークフローがすべてのファイルを読むことを求めていたが、1 段の FLF2V は拡大モデルを読まず、プロンプト拡張のファイルは拡張つきのワークフローしか読まない。どのワークフローも読まないファイルは今までどおり拒む（`ltx25test` の `TestAFileNoWorkflowOfTheModelLoadsIsRefused`）。

### 4.4 GPU のメモリの判定（Issue #927）

カタログのモデルの `vram`（`min_gb`・`recommended_gb`・`limits`）を、この PC の最大の GPU（ソフトウェアのアダプターを除く専用メモリ。`hfplan.VRAMOf`）と比べる（判定は `vramfit.For`。画面・MCP 側は `mmview.FitOf`。マーケティング上の GB に合わせ、バイト数を GiB に四捨五入してから比べる。RTX 4070 Ti SUPER の 16376 MiB は 16 GB）。

GPU のメモリの検出は子プロセス（DXGI・`nvidia-smi`。最大 5 秒）を起こす。モデルの一覧は描画のたび・2 秒ごとのポーリング・詳細モーダルのたびにこれを使うので、**`mmview.GPUVRAM`（`Service.VRAM`＝`vramcache.Cache`。MCP の `list_model_catalog` も同じ）は 5 分間覚える**（PC の構成は動かしている間ほとんど変わらない）。検出に失敗したとき（`Report.Error`）は 30 秒、呼び出しが取り消されたときは覚えない。**`DetectGPU`（`detect_gpu`・エンジンの GPU 表示・`get_gpu_status` の「今の状態」）は覚えず、毎回検出する**（Issue #927 の統合レビュー。#954）。

| 判定（`vram_fit.fit`） | 条件 | 画面・MCP の言葉 |
|---|---|---|
| `unknown` | GPU のメモリが分からない（GPU なし・検出の失敗） | 何も出さない（MCP は `vram_fit` から省く） |
| `short` | `min_gb` 未満 | 「目安の N GB に足りないため、動かない可能性が高い」（赤） |
| `limited` | `min_gb` 以上 `recommended_gb` 未満（`recommended_gb` があるときだけ） | 「推奨の N GB に届きません。**動くが制約あり**です: （`limits`）」（警告の色） |
| `enough` | `recommended_gb` 以上（`recommended_gb` が無いモデルは `min_gb` 以上） | 「目安を満たしています」 |

判定するのは、**`recommended_gb` を持つモデルだけ**（ほかのモデルの `min_gb` は MAM の推定で、そこから「動かない可能性が高い」と言うと、誰も測っていない主張になる）。これは目安の判定で、**生成を止めない**（導入も生成も、`short` でも拒まない。利用者が外部の ComfyUI や別の GPU で使うことがある）。画面はモデルのカードの「VRAM の目安」の下に 1 行（`data-testid="mm-vram-fit"`、`data-fit`）、MCP は `list_model_catalog` の出力 `vram_fit`（モデル ID → `fit`・`text`）に出す。LTX-2.5 は、最小 16 GB・推奨 32 GB なので、16 GB の GPU は `limited`、24 GB も `limited`、32 GB 以上が `enough`。

### 4.5 16 GB 実機での計測（Issue #967・#968。未計測）

次のモデルの生成時間とピーク VRAM は、**omp の環境（GPU が無い・または 16 GB でない）では測れない**ので、**未計測**。値は測るまで書かない（下の表の「—」は測っていない）。測るときは、(1) 16 GB の GPU で、導入したモデルを生成テスト（§8）で 1 回走らせ、結果の生成時間を記録する、(2) 生成の間 `nvidia-smi --query-gpu=memory.used --format=csv -l 1` を並べて取り、最大値をピーク VRAM とする、(3) GPU の名前・ドライバー・ComfyUI のバージョン（v0.42.1）・解像度・長さ・シードを一緒に書く。

| モデル（ワークフロー） | 解像度・長さ | 生成時間 | ピーク VRAM | GPU・環境 |
|---|---|---|---|---|
| `wan22-ti2v-5b`（fp16。比較の基準） | — | — | — | — |
| `wan22-ti2v-5b-fp8`（T2V・I2V） | — | — | — | — |
| `ltx-2.5-22b-distilled`（`ltx-2-5-i2v`。基準） | — | — | — | — |
| `ltx-2.5-22b-distilled`（`ltx-2-5-i2v-enhanced`。拡張のぶん） | — | — | — | — |
| `ltx-2.5-22b-distilled`（`ltx-2-5-flf2v`） | — | — | — | — |
| `ltx-2.5-22b-distilled-dfr`（`ltx-2-5-dfr-i2v`） | — | — | — | — |
| `ltx-2.5-22b-distilled-nvfp4`（Blackwell の GPU のみ） | — | — | — | — |

## 5. モデルの導入

### 5.1 ジョブ

| 種別 | 再開 | 内容 |
|---|---|---|
| `model_install` | **する**（`resumable=true`） | `target` は `engine` か `model:<id>`（ローカルランタイムは `runtime:<id>`。[local-runtime.md](local-runtime.md) §6。追加ノードは `node:<id>`。[custom-nodes.md](custom-nodes.md) §4）。ファイルを順に `downloads\` の `.part` へ取得（`Range` で再開）→ SHA-256 を再検証 → `data\models\<folder>\` へ移す。冪等で、検証済みのファイルは取り直さない |
| `gentest` | しない（`resumable=false`） | 試し生成（§8）。生成テストのバッチ（[../gentest/gentest.md](../gentest/gentest.md)）として動く。MAM が落ちたら、終わっていない結果は失敗になる |

- 再起動のたびに再開するのは `jobqueue.DefaultMaxAttempts`（3）回まで。それを超えると失敗として残り、利用者が導入をやり直す。
- ジョブの再開（上）とは別に、**1 回の取得の中の通信切断は、ジョブの中で自動的に `.part` から取り直す**（§3.3「通信が切れたときの自動再開」）。上限まで切れたジョブは失敗で終わり、再起動の再開の回数には数えない（ハンドラーがエラーを返すと、キューは再開せず失敗にする）。
- 同時に流す導入（ダウンロード）の数は絞っていない（ワーカー数の分だけ並ぶ）。絞るかどうかは未決定（Issue #909 の質問）。
- 同じ対象のジョブが動いている間の再実行は、新しいジョブを作らずそのジョブを返す（`JobRef.Existing`）。対象ごとに直列化する。**書き込み先（`models/<folder>/<name>`）ごとにも直列化する**（対象が違っても同じ宛先なら 1 つずつ動き、ロックを取った後に宛先を調べ直す。`model:<id>` の導入は、利用者が HF から足した別の SHA-256 のファイルを上書きせず `*InvalidError` で断る。[model-manager-hf.md](model-manager-hf.md) §4）。
- 他のモデルが検証済みの同じファイル（パス・SHA-256・サイズが同じ。VAE・テキストエンコーダーなど）は、再ハッシュせず信用する。
- 2 つの種別は 1 つのマイグレーション（`20261025090000_job_kind_model.sql`）で `job.kind` の CHECK に加える（[er-model-manager.md](er-model-manager.md)）。
- 更新の安全ゲート（desktop.md §6.2）は種別を見ずに `running`・`queued` の `job` を数えるので、導入と試し生成の間は自動更新が保留される。

### 5.2 インストール後

導入の完了で、ワークフローごとの `local_models`（`bundled-comfyui`）を作る。再導入や、ファイルが欠けた `incomplete` の復旧は同じ操作で行う。

### 5.3 Hugging Face のトークンと gated のモデル

`credential.Store` のプロバイダー `huggingface`（`SetHuggingFaceToken`。画面の「Hugging Face のトークン（任意）」の区画と、MCP の `get_`・`set_`・`clear_huggingface_token`）。トークンは `huggingface.co` への最初の要求にだけ付き、別ホストへのリダイレクト（CDN）では外す。カタログの導入（`install.go`）も、HF からの導入（model-manager-hf.md）も、保存したトークンを同じ方法で使う。**値はログ・画面・エラー・MCP の応答のどこにも出さない**（状態は「設定済み・末尾 4 文字」だけ）。

**ログインが要るモデル（`requires_hf_token = true`。LTX-2.5）の導入前チェック**。`InstallModel`（画面と MCP の共通の入口）が、**ジョブを作る前**に次の順で確かめ、どれかで止まるときはジョブを作らず、何もダウンロードしない:

1. **トークン未設定**: 同意の記録（§6）より前に止める（文言 A。同意も記録しない）。
2. トークンがあれば、同意の記録の後に、**アクセスを確かめる**: `hfplan` の `checkAccess` と同じ方式で、モデルのファイルの**先頭 1 バイト**（`Range: bytes=0-0`。`hf.Client.Probe`）を `huggingface.co` に要求する。リポジトリごとに、固定コミットのファイル 1 つで足りる（LTX-2.5 は 1 リポジトリ）。ファイルの本体は取らない。
   - `401`: トークンが無効または失効している（文言 B）。
   - `403`（`X-Error-Code: GatedRepo`）: トークンは有効だが、このモデルへのアクセスが承認されていない（文言 C）。
   - `404`（ファイルが無い）・`429`（レート制限）・`5xx`（Hugging Face の不調）: トークンの問題ではないと分かる短い案内（文言 A・B・C とは別。`hfguide.CheckAccess`）を `*InvalidError` で返す。
   - それ以外の失敗（ネットワーク）は、`hfError` の既存の文言（「ネットワークを確かめてからやり直してください」）で返す。トークンを「無効」や「同意が必要」とは案内しない（どの場合もジョブは作らない）。
3. **ダウンロードの途中の 401・403**（承認の取り消し・トークンの失効）も、同じ文言で**ジョブを失敗**にする（トークンが消えているなら文言 A、`401` は B、`403` は C）。`requires_hf_token` のモデルと HF からの導入だけで、他のカタログのモデルは従来の「取得元が認証を求めています（401/403）。Hugging Face のトークンを設定してください」のまま。

**案内の文言**は 1 か所（`service/modelmanager/hfguide`（`hfguide.NoToken`・`InvalidToken`・`Denied`。`modelmanager` の行数の上限のため別パッケージ））で定義し、画面・MCP・エラー・ジョブの失敗理由が同じ内容を出す:

| 文言 | 状況 | 内容 |
|---|---|---|
| A | トークン未設定 | このモデルの取得には Hugging Face のトークンが要る。モデルマネージャーの「Hugging Face のトークン（任意）」の区画（`/model-manager#mm-hf`）で設定する（MCP では `set_huggingface_token`） |
| B | `401` | トークンが無効または失効している。Hugging Face で発行し直し、同じ区画で保存し直す |
| C | `403`・`GatedRepo` | モデルのページ（`https://huggingface.co/<owner>/<repo>`）でライセンスに同意してアクセスを申請し、承認されてからやり直す（**申請は mam から API ではしない**） |

画面と MCP は、導入前チェックの文言を `*InvalidError`（画面は 422、MCP は `validation`）で受ける。カードには、`requires_hf_token` のモデルに「HF トークンが必要」のバッジを出し、トークンが未設定のときは導入ボタンの隣（操作の箱の中。幅が狭いときはボタンの下に折り返す。model-manager-screen.md）に文言 A とトークンの区画へのリンクを出す（既存のバッジ・案内の部品を使い、色・レイアウトは足さない）。

**取得の手順**（LTX-2.5。local-models.md §4.1）: (1) Hugging Face で `Lightricks/LTX-2.5` のページを開き、ライセンスに同意してアクセスを申請する（承認を待つ） → (2) Hugging Face の設定で読み取り用のトークンを発行する → (3) mam のモデルマネージャーの「Hugging Face のトークン」の区画で保存する → (4) カタログの `ltx-2.5-22b-distilled` の導入を押す（ライセンスの同意も要る）。

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
  2. **読むファイル**: ComfyUI のワークフローのグラフ（**任意の接続先のモデル**・取り込んだグラフ・名前を変えて作ったモデル・保存ワークフロー `comfy:<id>@v<N>`）のノードの入力の値（`ckpt_name`・`lora_name`・`unet_name`・`vae_name`・`clip_name*` などの文字列。**オブジェクトや配列の中の文字列も入れ子の底まで調べる**: rgthree の Power Lora Loader の `{"lora": "X.safetensors"}`、`["X.safetensors"]` など）が、**成人向けだけが持つファイル**を指すもの。成人向けだけが持つファイルとは、成人向けのカタログモデルの `files`・成人向けとして導入した記録の `files`・NFAA の HF モデルの `name` のうち、**成人向けでないカタログモデルの `files` にもあるものを除いたもの**（Flux.1 Dev のチェックポイントや Wan 2.2 の基盤のように、成人向けの LoRA の土台として共有されるファイルだけを読むグラフは成人向けではない。引くのは**カタログの静的な事実だけ**で、利用者が足したモデル・導入記録・成人向けでない HF モデルに同じ名前のファイルがあっても、成人向けのファイル名は一般のものにならない）。ファイル名は、サブフォルダ・`\`・大文字小文字を無視して比べる（フォルダは見ない）。文字列がファイル名そのものでないとき（プロンプトの `<lora:Name:0.8>`・`<lora:Name.safetensors:0.8>`・`<lyco:...>` のタグ、前後に空白のある名前、JSON を入れた文字列）は、大文字小文字を無視して次の 2 つを調べる。(a) 成人向けの**ファイル名を拡張子つきで**、1 語として含むか。1 語とは、前後が文字・数字・`_`・`-` でなく、`.` は前後が文字・数字のときだけ名前の一部とみなす（`Spicy.safetensors ` や `a cat, Spicy.safetensors.` に当たり、`spicy.v2.safetensors`・`Spicy.safetensors.bak` には当たらない）。(b) LoRA のタグ（`<lora:` と `<lyco:`）の名前（`:` か `>` まで。フォルダと大文字小文字を無視）が、成人向けのファイル名か、それから拡張子（`.safetensors`・`.sft`・`.ckpt`・`.pt`・`.pth`・`.bin`・`.gguf`）を除いた形に一致するか（`<lora:spicy:0.8>` は当たり、`<lora:spicy.v2:0.8>` は当たらない）。**拡張子なしの名前は、LoRA のタグの外の文章では調べない**（`ae`・`model`・`euler` のような短い一般的な語を含む文章や、一般のモデルの `ae.sft`・`flux/ae.pt` が、成人向けになってしまうため）。オブジェクトの**キー**も値と同じに調べる（`{"Spicy.safetensors": 0.8}`）。`{` か `[` で始まり JSON として読める文字列は、読み直して中を同じに調べる（文字列の中の JSON は 4 段まで。それより深いものは文字列として調べる）。読めないグラフは何も読まないものとして扱う（そもそも走らない）。
  3. 組み込みのモデル（Seedance・Kling・GPT Image など）、OpenAI 互換の接続先のモデル、成人向けのファイルを読まないグラフは、これまでどおり成人向けではない。
- **保存ワークフローの実行**（ワークフロー画面・MCP の `run_workflow`）は Model Adapter を通らず `comfyui.Backend` を直接呼ぶので、`workflows.Service.StartRun` が同じ `Adapter.Gate` を、実行の前に調べる（成人向けなら `*InvalidError`。文言は上と同じ）。
- **範囲と限界**: 判定はファイル名で、ファイルの中身は調べない。成人向けのファイルを別の名前にコピーして読ませるグラフ、ComfyUI のサーバー側で名前を付け替えたファイルは見分けられない（この判断は「導入済みの成人向けモデルが、年齢確認オフで使えない」の趣旨の範囲。ワークフローの保存の仕方は変えない）。保存ワークフロー自体の一覧・編集・保存は、ゲートが閉じていてもできる（走らせることと割り当てることだけを断る）。**調べないもの（残る限界）**: 名前を複数の入力に分けて持つもの（`Spi` と `cy.safetensors`）、Base64、Unicode の正規化（全角の `ＳＰＩＣＹ` など）・パーセントエンコード（`%5F`）は復号しないので、成人向けのファイルを指すと判定されない。ComfyUI の標準のローダーは、`/models/<folder>` が返すファイル名そのものしか受け付けないので、これらの形ではファイルを選べない。カスタムノードがこれらを復号して読むかどうかは [INFERENCE]（確かめていない）。
- **既知の過検出（受け入れた限界）**: 判定は名前だけなので、成人向けのモデルだけが持つが、名前が一般的なファイル（Flux の VAE の標準名 `ae.safetensors`、`t5xxl_fp8_e4m3fn_scaled.safetensors`、`qwen_image_2.1_vae_bf16.safetensors` など。一般のカタログモデルは持たない）は成人向けの印として扱う。利用者が自分で組んだ一般の Flux・Chroma・T5 のグラフがこれらの名前を読むと、年齢確認がオフの間は成人向けとして選択肢から外れ、生成を断られる（`txt2img-chroma` の組み込みテンプレートも同様）。理由: グラフが持つのはファイル名だけで、大きさや SHA-256 で見分けるには ComfyUI のサーバー側のファイルを調べる必要があり、別のマシンの接続先では調べられない（判定できないとゲートが開く）。**閉じる側に倒す**ほうを選んだ（年齢確認をオンにすれば使える）。許可リストで一般的な名前を外すと、成人向けのモデルがその名前でゲートを抜けるので置かない。プロンプトの文章に、成人向けのファイル名（拡張子つき）が 1 語として入っているグラフ、LoRA のタグが成人向けのファイルを名指しするグラフは、同じ理由で断る。拡張子なしの名前だけの語は、一般的な語（`ae`・`model`・`euler`）が一般のモデルを巻き込むので、タグの外では調べない。
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

**音声つきのモデル（Issue #927）**: `native_audio` のモデル（LTX-2.5）を選ぶと、フォームに「映像と同期した音声も作ります。結果は音声つきの mp4 で、下のプレーヤーで音が出ます」と注記を出し（`data-testid="mm-trial-audio-note"`）、結果には「音声つき」バッジ（`mm-trial-audio-badge`）を付け、`<video>` の `aria-label` を「試し生成の結果（音声つきの動画）: …」にする。保存したファイルは再エンコードせずそのまま `video/mp4` で配信する（`TestTrial_AVideoModelWithSoundSaysSoAndServesTheFileItSaved`）。

## 9. 起動・連携（`internal/app`）

- `services.go` が `modelmanager.Service` を作り、`Register()` をジョブキューの開始前に呼ぶ。`Services.ModelManager` で公開する。`mm.Close` はジョブの停止の後に走る（エンジンを使うジョブが先に止まる）。ローカルランタイム（`localruntime.Service`）も同じ順で配線する（[local-runtime.md](local-runtime.md) §7）。
- `mam.exe --detect-gpu` / `--detect-comfyui` は、ウィンドウもデータディレクトリも開く前に JSON を出して終わる（`app.RunDetect`、`cmd/server` と `cmd/wails` が最初に呼ぶ。両方を付けると `{"gpu":..., "comfyui":...}`。`--detect-output FILE` で標準出力ではなくファイルへ書く。setup.md）。

## 10. Service API（`internal/service/modelmanager`）

画面・MCP は次を呼ぶ（戻りは JSON タグ付きの構造体）。エラーは `ErrUnsupported`・`ErrNotFound`・`ErrEngineNotInstalled`・`ErrAlreadyInstalled`・`ErrNotInstalled`・`ErrConsentRequired`・`ErrAdultNotConfirmed`・`ErrBusy`・`ErrNoGPU`・`ErrPinned`・`ErrEndpointMoved`・`*InvalidError`（メッセージを利用者に見せる。`requires_hf_token` のモデルの導入前チェックの文言 A〜C もこれ。§5.3）。取得元が消えたときのジョブの失敗は `ErrSourceGone`（利用者向けのメッセージに包む）。

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
| モデルの VRAM の目安 | HunyuanVideo（文書）以外は MAM の推定（成人向けの 5 モデルと `wan22-i2v-14b`・`wan22-ti2v-5b` もすべて `estimate=true`）。`wan22-ti2v-5b` は ComfyUI の公式ガイド（8 GB）と Wan の参照実装（24 GB）の間の 16 GB（§4.2） |
| FLUX.2 klein 4B（`flux2-klein-4b`、Issue #931）の生成・参照画像つきの編集の実行と、`qwen_3_4b.safetensors`・`flux2-vae.safetensors` の取得 | **未確認**（構造テストと、偽の ComfyUI でのグラフの組み立てだけ。ノード `Flux2Scheduler`・`EmptyFlux2LatentImage`・`ReferenceLatent`・`ImageScaleToTotalPixels` と `CLIPLoader type=flux2` が v0.42.1 にあることはソースで確かめた。3 ファイルの SHA-256・サイズ・コミットは実物の Hugging Face の API・応答ヘッダーから読んだ。重みの本体は取っていない） |
| Wan 2.2 Remix T2V・LightX2V の LoRA・非検閲の 4 ステップ LoRA（Issue #930）の生成の実行と、速さのモードでの速度・質の差 | **未確認**（構造テストと、偽の ComfyUI でのグラフの組み立て〔`standard` で LoRA が外れ 20 ステップ・cfg 3.5 になること〕だけ。実機の RTX 4070 Ti SUPER が無く、生成時間・VRAM は実測していない。Issue の「約 326 秒」はコミュニティの報告） |
| Remix T2V v2.0・LightX2V 1022 版の取得（約 36 GB・1.4 GB） | **未実施**（SHA-256・サイズは HF の API・応答ヘッダー・Civitai の API で確認。ファイルの本体は取っていない） |
| 非検閲の 4 ステップ LoRA（rzgar）のライセンスの本文・学習データの出所 | **未確認**（カードの `license: apache-2.0` の宣言だけ。LICENSE のファイルは無い） |
| Wan 2.2 14B の fp8 を macOS（外部 ComfyUI・MPS）で動かすこと | **対応外・未確認**（§4.2 の Issue #930） |
| 成人向け・LoRA の 6 モデルのテンプレートの実行（`txt2img-qwen-image21`・`txt2img-chroma`・`txt2img-flux-dev-lora`・`img2video-wan22-14b`・`-remix`・`-lora`） | **GPU 実機で未検証**（構造テストと、ローダーとファイル名の一致だけ。サンプラーの値は作者の推奨と ComfyUI の公式手順に基づく） |
| Wan 2.2 14B に `wan_2.1_vae` を使うこと、Chroma の `CLIPLoader type=chroma` ＋ fp8mixed の組み合わせ | **未検証**（ComfyUI の公式手順・作者のワークフローに基づく。実機で確かめる） |
| NSFW MASTER の作者の原本のライセンス | **未確認**（HF のミラーのタグは MIT。Civitai の API はライセンス名を返さない。全体を非商用として扱う） |
| 実物の Hugging Face からの 6 モデルの取得（数十 GB）と、ミラーが消えたときの実際の応答 | **未実施**（偽の HF で、404 の `ErrSourceGone`・SHA-256 の不一致・共有ファイルの 1 回取得は確認） |
| 実物の Hugging Face からの LTX-2.5（Issue #877）の全ファイルの取得（数十 GB）と、gated のアクセス確認（先頭 1 バイト）の実物への動作 | **未実施**。`ltx-2.5-22b-distilled` の SHA-256・サイズ・コミットは、実物の Hugging Face の API・応答ヘッダーから読み取った（ファイルの本体は取っていない）。導入前チェックと途中の 401・403 は、偽の HF（`hftest`）の 4 通り（未設定・401・403 GatedRepo・成功）で確認 |
| LTX-2.5 のテンプレート（`txt2video-ltx-2-5`・`img2video-ltx-2-5`）の ComfyUI での読み込みと GPU 実機の生成、`CLIPLoader type=ltxv` での Gemma 4 12B（int8 convrot）の読み込み | **未確認**（構造テストと、ローダーとファイル名の一致だけ。v0.42.1 のソースに必要なノードがあることは読んで確かめたが、実行はしていない。サンプラー・シグマは Comfy 公式のワークフローの値） |
| LTX-2.5 の 16 GB での動作・生成時間・VRAM の実測（Issue #927） | **未実施**（実機の RTX 4070 Ti SUPER が無い）。「16 GB で動く」はコミュニティの報告で、Lightricks の推奨は 32 GB 以上。生成時間の目安は実測していないので書かない（実測したら `limits` と本書に実測値と環境を書く） |
| 非検閲のテキストエンコーダー（`ltx-2.5-22b-distilled-uncensored`）の読み込みと効果 | **未確認**（SHA-256・サイズ・コミットは HF の API と応答ヘッダーで確認。ファイルの本体は取っていない）。作者の記載は「LTX-2.5 の投影の重みを保って変換した」「保証しない」。変換版が `CLIPLoader type=ltxv` で読めること、プロンプトへの従い方が変わることは、実機で確かめていない |
| 音声つき mp4 に実際に音が入ること・音声と映像のずれ | **未確認**（ComfyUI v0.42.1 のソースで `SaveVideo` の `auto` が H.264 + AAC の MP4 になることと、mam が保存したファイルをそのまま配信することは、テストで確認。音声トラックの中身は偽の ComfyUI では検証できない） |
| LTX-2.5 を macOS（外部の ComfyUI・MPS）で動かすこと | **対応外・未確認**（§4.2「macOS」。報告は割れていて、mam では試していない） |
| 16 GB の GPU での生成時間・ピーク VRAM（Issue #967・#968。`wan22-ti2v-5b-fp8`・LTX-2.5 の FLF2V・拡張つき・DFR・NVFP4） | **未計測**（omp の環境には 16 GB の実機が無く、測れない）。値は書かず、§4.5 の表に記入欄（—）と計測の手順だけを置いた |
| `wan22-ti2v-5b-fp8` の GPU での fp8 の実行・生成、fp16 版との画質の差、約 5.3 GB の取得 | **未確認**（SHA-256 はダウンロードしたファイルで計算して HF の値と一致を確認。ComfyUI v0.42.1 が CPU で `WAN22` として読み込めることと、テンプレートが `validate_prompt` を通ることは確認。GPU の fp8 の計算と生成は未実行） |
| LTX-2.5 の NVFP4（`ltx-2.5-22b-distilled-nvfp4`）の動作 | **未確認**（実機の RTX 4070 Ti SUPER は Blackwell ではなく、NVFP4 を動かせない。SHA-256・サイズ・コミットは HF の API の値。Lightricks の文書と ComfyUI v0.42.1 のソースが「Blackwell 向け」の根拠） |
| LTX-2.5 の FLF2V・プロンプト拡張つき・DFR のテンプレートの GPU での生成と、拡張のファイル（約 5.2 GB）・時間方向の拡大モデル・IC-LoRA の取得 | **未確認**（ComfyUI v0.42.1 を CPU で起動して `validate_prompt` を通し、ノードの存在と入力の型・値の範囲までは確認。モデルの読み込み・生成は未実行。ファイルの本体は取っていない） |
| DFR の IC-LoRA（`Lightricks/LTX-2.5-22b-IC-LoRA-Pixel-Spatial-Upscaler`）のアクセスと、ファイルの内容 | **未確認**（開発ホストの HF アカウントは、このリポジトリのアクセスが未承認。SHA-256・サイズ・コミットはゲートでも返るモデルの API の値。ファイルの先頭 1 バイトの取得とヘッダーの確認はしていない） |
| LTX-2.5 の dev 版（Issue #968） | **入れていない**（2.5 の dev 版を読む公式のワークフローが無い。足す条件は §4.2「dev 版は入れない」） |
| FLUX.2 klein の bf16 版（macOS 用。Issue #967） | **入れていない**（マネージャーが macOS に入れられない。足す条件は §4.2 の klein の「bf16 版（macOS 用）は追加しない」） |
| ControlNet（`sdxl-controlnet-canny`）・IP-Adapter（`sdxl-ipadapter-plus`）の GPU での生成と、ControlNet・IP-Adapter・CLIP ViT-H のファイルの取得（約 2.5 GB・0.85 GB・2.5 GB。Issue #976） | **未確認**（構造テストと、偽の ComfyUI でのグラフの組み立てだけ。SHA-256・サイズ・コミットは HF の API の値。ControlNet の diffusers 形式は ComfyUI v0.42.1 のソースでキーを確認、実際の読み込みは未実行） |
| 追加ノードの実機での導入・ComfyUI の再起動・`/object_info` での読み込みの確認、GitHub のアーカイブの実際のダウンロード（Issue #976） | **未確認**（偽の GitHub と偽のエンジンで、取得・SHA-256 の検証・展開・フォルダの入れ替え・読み込まれないノードの削除までを確認。[custom-nodes.md](custom-nodes.md) §9） |
| ファイル名での成人向けの判定（§6.1）が、実物の ComfyUI のファイル名（サブフォルダ付き）と一致すること | **未検証**（`ckpt_name` などは ComfyUI の `/models/<folder>` が返す名前で、サブフォルダ付きのときは末尾の名前で比べる。実機のサーバーは使っていない）。中身が同じで名前だけ違うファイルは見分けられない。一般的な名前の成人向けのファイルの過検出は受け入れた限界（§6.1） |
| 年齢確認がオフのときの生成の断りを、実物の ComfyUI・クラウドに対して確かめること（呼び出しが届かないこと） | **未検証**（断りは Adapter の手前で、偽のバックエンドの呼び出し回数 0 と、同梱エンジンが起動しないことで確認。実機の ComfyUI・実アカウントは使っていない） |
| 実物の Hugging Face からの任意のモデルの導入（Issue #842: 実物の API・CDN・gated・GPU・SDXL の生成） | **未検証**（[model-manager-hf.md](model-manager-hf.md) §10。偽の HF で確認） |

## 12. 画面と MCP ツール

- **画面**: `GET /model-manager`（`web/handler/modelmgr`・`organisms/modelmgr`）。仕様は [endpoints-model-manager.md](../../api/endpoints-model-manager.md)・[model-manager-screen.md](../../components/model-manager-screen.md)。入口はプロジェクト一覧と設定画面のリンク。進捗は HTMX のポーリング（2 秒）。
- **MCP ツール**: `mcp/tools/modelmgr`（エンジン・カタログ・トークン・外部）と 生成テストの `mcp/tools/gentest`（試し生成もこれで行う）。仕様は [mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)・[mcp-tools-generation-test.md](../../api/mcp-tools-generation-test.md)。ジョブの種別 `model_install` の Probe はこの画面側、`gentest` の Probe は生成テスト側にあり、`get_job` が状態・進捗・結果・失敗の理由を返す。
- **共有**: サービスの拒否は `service/modelmanager/mmview.Refuse`（種類とメッセージ）で、画面の HTTP 状態と MCP のエラーコードに写す（`ErrConsentRequired`・`ErrBusy`・`ErrPinned`・`ErrUnsupported` は 409 / `conflict`、`ErrNoGPU`・`*InvalidError` は 422 / `validation`）。`mmview.TrialModels` は試し生成の選択肢（モデルの識別子・動画の解像度と長さ・参照画像を使えるか）。
- **導入の中断**: `CancelInstall`。ジョブは失敗で終わり（理由に「導入を中断しました」）、`.part` は残るので、導入し直すと続きから再開する。
- **通信切断での失敗**: 切断が続いて自動再開の上限を超えたジョブも、`.part` は残る。理由は「通信が切れました。もう一度導入すると続きから再開します」で、導入し直すと続きから再開する（§3.3）。
- **同意の強制は API**: 画面のダイアログは同意を渡す手段で、`InstallModel` が記録を確かめる（§6）。MCP からも同じ（記録が無ければ 409 / `conflict`）。**年齢確認も同じ**（§6.2）: 画面のダイアログ（`PUT /model-manager/adult`）と MCP の `set_adult_content` は確認を渡す手段で、`Catalog`・`InstallModel`・試し生成の一覧は Service が閉じる。`ErrAdultNotConfirmed` は 409 / `conflict`（`mmview.Refuse`）。
- **テスト用の部品**: `service/modelmanager/mmtest` は偽の GitHub / Hugging Face（`Range`・保留 `Hold` つき）・偽のエンジンのプロセス・ジョブキューつきの `Service` を作る（画面と MCP のテストが共有する）。
- **切断のまね**: `mmtest.Host.Drop(file, bytes, times)` が、そのファイルの次の `times` 回の要求を `bytes` バイトで切る。`mmtest.FetchRetry` が再開の待ちを決める（既定は数ミリ秒）。`service/modelmanager/retrytest` が、再開して完了・上限で失敗して次の導入で完了・404 は再開しない、を確かめる。`fetch` の単体テスト（`retry_test.go`）が、切断（FIN・RST）・アイドル・5xx・取り消し・待ちの途中の取り消しを確かめる。
- **Hugging Face からの導入**（Issue #842）: カタログに無い `.safetensors` を HF のリポジトリから導入する経路。意味・固定・フォルダ・年齢ゲート・記録・未検証は [model-manager-hf.md](model-manager-hf.md)。画面は `web/handler/modelmgr/hfimport`、MCP は `mcp/tools/modelmgr/hf.go` の 5 ツール。
