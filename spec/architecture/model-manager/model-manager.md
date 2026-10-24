# モデルマネージャー（ComfyUI エンジンの取得・起動とモデルの導入）

[local-models.md](../local-models.md)（利用者が登録した ComfyUI・OpenAI 互換の接続先）に、**MAM が自分で ComfyUI を取得・起動し、カタログのモデルを入れる**経路を足す。実装は `backend/internal/modelmgr/*`（部品）と `backend/internal/service/modelmanager`（Service）。画面（[../../api/endpoints-model-manager.md](../../api/endpoints-model-manager.md)）・MCP ツール（[../../api/mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)）はこの Service API の上に載る（§12）。インストーラーは変わらない（desktop.md §4・§6.8）。改訂履歴は [model-manager.changelog.md](model-manager.changelog.md)。

## 1. 方針（変えない前提）

- **ComfyUI はインストーラーにも Release にも同梱しない**（サイズと自動更新の上限のため）。MAM が、GPU に合う公式のポータブル版（7z）を実行時に取得する。ComfyUI は GPL-3.0 の別プログラムで、MAM は配布しない（`THIRD_PARTY_NOTICES.md`）。
- **置き場所は `<mam.exe のあるディレクトリ>\comfyui` だけ**（§2）。インストール版ならアンインストーラーがインストール先ごと消し、ポータブル版は exe の隣に残る。それ以外に永続する場所を作らない。書けない場所なら明確なエラーで、外部 URL の利用を案内する。
- **取得物は固定（ピン留め）して SHA-256 を検証する**: エンジンもモデルも、コミットまたはタグ・SHA-256・サイズをカタログ（埋め込みの JSON）に持つ。ハッシュが合わないものは使わず、消す。
- **Windows 版だけが同梱エンジンを持つ**。ブラウザ版・サーバー版（非 Windows）では同梱エンジンは使えず（`ErrUnsupported`）、外部の ComfyUI を接続先 URL（local-models.md）で使う。
- **外部の ComfyUI は読むだけ**: インストール済みモデルの一覧（`GET /models/{folder}`）と、カタログと照らした「足りない」表示までで、リモートへの導入はしない（ComfyUI にモデルを置く API が無い）。
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

- ジョブの結果（試し生成）は MAM のデータディレクトリ（`<data-dir>/gentest/<job-id>/`）に置く。`comfyui` の中ではない（アンインストールでもデータは残す、という既存の方針）。
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

取得できるホストは `huggingface.co`・`hf.co`・`github.com`・`githubusercontent.com` だけで、リダイレクト先も同じ規則で調べる。`Authorization` は最初の要求の宛先ホストにだけ付け、ホストが変わると外す（§5.3）。

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
| `files[]` | `name`・`folder`（`checkpoints`・`diffusion_models`・`text_encoders`・`vae`）・`url`（**コミット固定の** Hugging Face の URL、https）・`sha256`・`size` |
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

ワークフローは `localcfg/templates/` の **コアノード（ComfyUI 本体のノード）だけ**で書く。SDXL と Wan 2.2 は既存のテンプレートを使い、FLUX schnell/dev・SD3.5・Qwen-Image・LTX・HunyuanVideo のテンプレートを足した（`txt2img-flux-schnell` など 7 つ）。`catalog.Validate` は、各テンプレートのローダーノード（`CheckpointLoaderSimple`・`UNETLoader`・`CLIPLoader`・`DualCLIPLoader`・`VAELoader`）が、そのモデルのファイル名と一致することを確かめる。**テンプレートは構造テストだけで、GPU 実機では未検証**（画面にも「構造テストのみ・GPU 実機未検証」と出す。`ModelView.WorkflowCheck`）。

### 4.3 検証（`catalog_test.go`）

すべてのファイルに https の固定 URL・64 桁の SHA-256・正のサイズがあること、同意が要るモデルにライセンス情報が揃うこと、ID と（フォルダ, 名前）の重複が無いこと、テンプレートが存在し構造が正しいこと。

## 5. モデルの導入

### 5.1 ジョブ

| 種別 | 再開 | 内容 |
|---|---|---|
| `model_install` | **する**（`resumable=true`） | `target` は `engine` か `model:<id>`。ファイルを順に `downloads\` の `.part` へ取得（`Range` で再開）→ SHA-256 を再検証 → `data\models\<folder>\` へ移す。冪等で、検証済みのファイルは取り直さない |
| `gentest` | しない（`resumable=false`） | 試し生成（§8）。MAM が落ちたら失敗になる |

- 再起動のたびに再開するのは `jobqueue.DefaultMaxAttempts`（3）回まで。それを超えると失敗として残り、利用者が導入をやり直す。
- 同じ対象のジョブが動いている間の再実行は、新しいジョブを作らずそのジョブを返す（`JobRef.Existing`）。対象ごとに直列化する。
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

## 7. 状態・削除

`state.json`: `AutoStart`・`LastPort`・`Engine`（版・変種・CPU）・`Consents`・`Models`（導入済みのファイルとワークフロー）。読めない記録は `state.json.corrupt` に退避して空から始める（MAM の起動を止めない。ファイルは残り、導入は検証し直す）。

**削除**（`DeleteModel`）: 使われている画像用途・Quality Policy があれば `*InvalidError` で断る（`localmodels` の `UsedBy`）。`local_models` を消し、**他の導入済みモデルが使っていないファイルだけ**を消す（共有する VAE・テキストエンコーダーは、使うモデルが残る間は残す。参照数は記録した導入済みファイルから数える）。

## 8. 試し生成（`gentest`）

入力: モデル ID（`comfyui:bundled-comfyui/...` のほか、外部の ComfyUI・OpenAI 互換のモデルも可）・プロンプト・任意の参照画像・長さ・解像度・縦横比。`localbackends.Build` で作ったバックエンドを **`Adapter`・サーキットブレーカー・コスト台帳・Take を通さず**に直接呼ぶ。結果は `<data-dir>/gentest/<job-id>/`（`record.json`・結果ファイル・参照画像）に置き、`take`・`cost_ledger_entry`・`assets` には書かない。一覧・取得・削除は Service で行う。参照画像はジョブの作成前に `gentest/_inputs/` へ置き、ハンドラが試行のディレクトリへ移す（24 時間以上前の取りこぼしは起動時に消す）。Issue #835 の履歴・比較は、この種別の上に後で作る。

## 9. 起動・連携（`internal/app`）

- `services.go` が `modelmanager.Service` を作り、`Register()` をジョブキューの開始前に呼ぶ。`Services.ModelManager` で公開する。`mm.Close` はジョブの停止の後に走る（エンジンを使うジョブが先に止まる）。
- `mam.exe --detect-gpu` / `--detect-comfyui` は、ウィンドウもデータディレクトリも開く前に JSON を出して終わる（`app.RunDetect`、`cmd/server` と `cmd/wails` が最初に呼ぶ。両方を付けると `{"gpu":..., "comfyui":...}`。`--detect-output FILE` で標準出力ではなくファイルへ書く。setup.md）。

## 10. Service API（`internal/service/modelmanager`）

画面・MCP は次を呼ぶ（戻りは JSON タグ付きの構造体）。エラーは `ErrUnsupported`・`ErrNotFound`・`ErrEngineNotInstalled`・`ErrAlreadyInstalled`・`ErrNotInstalled`・`ErrConsentRequired`・`ErrBusy`・`ErrNoGPU`・`ErrPinned`・`ErrEndpointMoved`・`*InvalidError`（メッセージを利用者に見せる）。

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
// 試し生成
func (*Service) StartGentest(ctx, GentestInput) (JobRef, error)
func (*Service) Gentests(ctx) ([]GentestRecord, error)
func (*Service) Gentest(ctx, id string) (GentestRecord, error)
func (*Service) StageReference(Reference) (id string, err error) // 参照画像を先に置く（MCP のアップロード用途 model_trial_reference）。GentestInput.ReferenceID で渡す
func (*Service) GentestFile(ctx, id string) (path, mime string, err error)
func (*Service) DeleteGentest(ctx, id string) error
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
| モデルの VRAM の目安 | Wan 2.2（README）と HunyuanVideo（文書）以外は MAM の推定 |
| ポータブル版の Python・PyTorch のライセンスの一覧 | **未調査**（`THIRD_PARTY_NOTICES.md` に明記） |

## 12. 画面と MCP ツール

- **画面**: `GET /model-manager`（`web/handler/modelmgr`・`organisms/modelmgr`）。仕様は [endpoints-model-manager.md](../../api/endpoints-model-manager.md)・[model-manager-screen.md](../../components/model-manager-screen.md)。入口はプロジェクト一覧と設定画面のリンク。進捗は HTMX のポーリング（2 秒）。
- **MCP ツール**: `mcp/tools/modelmgr`（エンジン・カタログ・トークン・外部）と `mcp/tools/modeltrial`（試し生成）。仕様は [mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)。ジョブの種別 `model_install`・`gentest` の Probe を持ち、`get_job` が状態・進捗・結果・失敗の理由を返す。
- **共有**: サービスの拒否は `service/modelmanager/mmview.Refuse`（種類とメッセージ）で、画面の HTTP 状態と MCP のエラーコードに写す（`ErrConsentRequired`・`ErrBusy`・`ErrPinned`・`ErrUnsupported` は 409 / `conflict`、`ErrNoGPU`・`*InvalidError` は 422 / `validation`）。`mmview.TrialModels` は試し生成の選択肢（モデルの識別子・動画の解像度と長さ・参照画像を使えるか）。
- **導入の中断**: `CancelInstall`。ジョブは失敗で終わり（理由に「導入を中断しました」）、`.part` は残るので、導入し直すと続きから再開する。
- **同意の強制は API**: 画面のダイアログは同意を渡す手段で、`InstallModel` が記録を確かめる（§6）。MCP からも同じ（記録が無ければ 409 / `conflict`）。
- **テスト用の部品**: `service/modelmanager/mmtest` は偽の GitHub / Hugging Face（`Range`・保留 `Hold` つき）・偽のエンジンのプロセス・ジョブキューつきの `Service` を作る（画面と MCP のテストが共有する）。
