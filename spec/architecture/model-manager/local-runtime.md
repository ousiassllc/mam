# ローカルランタイム（GPT-SoVITS・のちに MuseTalk）

[model-manager.md](model-manager.md)（同梱の ComfyUI エンジン）と同じ作りで、**声（GPT-SoVITS）と口パク（MuseTalk 1.5）をローカルで動かす実行環境を、MAM が自動で導入・起動・停止・状態表示まで管理する**ための基盤。親 Issue は #912（決定事項: 実行方式 B「ComfyUI の同梱エンジンと同じ感じで、mam が自動で入れて起動まで管理する」）。この文書は基盤と GPT-SoVITS の環境導入（Issue #918）を定める。実装は `backend/internal/modelmgr/runtime`（導入・判定・起動コマンド）と `backend/internal/service/localruntime`（Service）。改訂履歴は [model-manager.changelog.md](model-manager.changelog.md)。

| 範囲 | Issue |
|---|---|
| ランタイム管理の基盤、GPT-SoVITS の環境の導入・起動・停止・状態 | **#918（この文書）** |
| 学習済みモデルの重みの取得（固定 URL＋SHA-256、再開可）、出典とライセンスの画面表示、画面・MCP | #920 |
| MuseTalk 1.5 のランタイム | #921（この文書の枠に載せる） |

## 1. 方針（変えない前提）

- **ComfyUI を経由せず、ランタイムごとに独立した Python 環境とプロセスを持つ**。GPT-SoVITS は公式の `api_v2.py`（FastAPI）を、MAM が `127.0.0.1` 限定で起動して HTTP で呼ぶ。ComfyUI のカスタムノード経由にしない（親 Issue「ComfyUI のカスタムノードは古いものが多い」。決定事項 1 で、MAM が管理する独立の実行環境・プロセスに決まった）。
- **ランタイムの種類はデータ**（`catalog/data/runtimes/<名前>.json`）。コードは GPT-SoVITS を知らない。MuseTalk は同じ形の JSON を足し、起動の引数・確認パス・対応環境の判定の文を書くだけで載る（§9）。
- **MAM は何も同梱しない**。公式の配布先から取得する別プログラムで、`THIRD_PARTY_NOTICES.md`（GPT-SoVITS の節）に載せる。ライセンスと商用利用の可否の表示は #920。
- **置き場所は `<mam.exe のあるディレクトリ>\localai\<ランタイム ID>\` だけ**（§2）。ライブラリのキャッシュと一時ファイルもその中に置く。インストール版はアンインストーラーが消し、ポータブル版は exe の隣に残る。書けない場所なら `store.ErrNotWritable` で断る。
- **取得物は固定して SHA-256 を検証する**（model-manager.md §1 と同じ）。ハッシュ・サイズが合わないものは使わず消す。取得は `fetch`（`Range` 再開・空き容量確認）と `extract` をそのまま使う。
- **対応は Windows x64 ＋ NVIDIA GPU だけ**（ComfyUI の同梱エンジンと同じ範囲）。それ以外は導入も起動もさせず、**理由の文とクラウド（ElevenLabs）を使う案内**を返す（§4）。macOS の MPS / CPU 対応は、オーナーの判断が要る（既定は「自動導入しない」）。
- **ComfyUI のエンジン・既存のモデル導入は変えない**。`model_install` のジョブ種別・ダウンローダー・抽出器・プロセス管理を共有するだけで、状態（`state.json`）もディレクトリも別。

## 2. ディレクトリ

```
<mam.exe のディレクトリ>\localai\<ランタイム ID>\      例: localai\gpt-sovits\
  env\          公式パッケージを展開したもの（runtime\python.exe と本体。更新はこのディレクトリごと入れ替える）
  downloads\    *.part と取得したアーカイブ（再開用）。完了・失敗で片づける
  cache\        HF_HOME・TORCH_HOME など、ライブラリのキャッシュ（ユーザープロファイルに書かせない）
  tmp\          TEMP・TMP・TMPDIR
  logs\         runtime.log（起動の失敗で末尾を表示する。5 MiB を超えたら作り直す）
  state.json    導入状態（§8）
```

- `store.ForRuntime(store.LocalAIRoot(exe), id)` が `RuntimeLayout` を作る。`CheckWritable` が作成と書き込みを試し、できなければ `store.ErrNotWritable`。
- 対応していない環境では**何も作らない**（`Status` も書き込み可否を調べない）。
- `env.new\`・`env.old\` は更新の途中に現れる（ComfyUI の `engine.new\`・`engine.old\` と同じ）。

## 3. 実行環境の作り方（決定と理由）

**決定**: 公式の Windows 統合パッケージ（Hugging Face `lj1995/GPT-SoVITS-windows-package` の 7z）を、**コミットを固定した URL**・SHA-256・サイズで取得し、展開する。ComfyUI の同梱エンジンと同じ方式で、`fetch`・`extract`・`Installer` をそのまま使う。

| 案 | 判断 |
|---|---|
| **公式の統合パッケージの 7z（採用）** | 公式が配布する、動作確認済みの Python 環境・PyTorch・本体一式。SHA-256 とサイズは Hugging Face の API（`lfs.oid`・`x-linked-size`）で確かめられ、取得は既存の許可ホスト（`huggingface.co`・`*.hf.co`）で足りる（変更なし）。Python は埋め込みの `runtime\python.exe` で、システムの Python に触れない |
| 埋め込み Python に固定した wheel をハッシュ固定で入れる | 取らない。PyTorch（CUDA）と約 100 のパッケージを、Windows の cp311・CUDA ごとに全部ハッシュ固定する必要があり、実機で動かして確かめられない（この開発環境は Linux）。許可ホストも `pypi.org`・`download.pytorch.org`・`python.org` に広げることになる |
| GitHub のソースの zip ＋ pip | 取らない。同じ理由に加えて、実行時に pip が任意のホストへ取りに行く |

**固定する版**: `GPT-SoVITS-v2pro-20250604`（コードは GitHub のタグ `20250606v2pro`、`api_v2.py` はそのタグ）。日本語に公式対応する v2Pro で、公式の最後の GitHub リリース。RTX 50 シリーズは別パッケージ `…-nvidia50.7z`。パッケージの先頭ディレクトリ（`archive_root`）の下に `runtime\python.exe`・`api_v2.py`・`GPT_SoVITS\configs\tts_infer.yaml` があることを、実際の 7z のヘッダーを HTTP の `Range` で読んで確かめた（§11）。

| 変種 | 対象 | URL（コミット `c8b61d4f627d1f02e74c1ba158a4844051d2289f`）のファイル名 | サイズ | 展開後 | SHA-256 |
|---|---|---|---|---|---|
| `nvidia` | Compute Capability 12.0 未満（GTX 10 〜 RTX 40。PyTorch 2.0.0+cu118） | `GPT-SoVITS-v2pro-20250604.7z` | 8,185,086,602 | 14,128,680,597 | `bd60d079…ed6` |
| `nvidia50` | 12.0 以上（RTX 50） | `GPT-SoVITS-v2pro-20250604-nvidia50.7z` | 8,835,144,925 | 14,964,402,944 | `97b4edcd…008` |

SHA-256 は Hugging Face の API（`/api/models/lj1995/GPT-SoVITS-windows-package/tree/<commit>?recursive=1` の `lfs.oid`）と、ダウンロード応答の `x-linked-etag`・`x-linked-size` の一致を確かめた値。展開後サイズは 7z のヘッダーの合計。

**重みについて**: この統合パッケージは、v2Pro の**学習済みモデルの重み（`GPT_SoVITS\pretrained_models` ほか）を含む**（約 8 GB の大半）。MAM のインストーラー・リリースには含めず、ランタイムの取得で公式の配布先から一緒に入る。重みごとの出典・ライセンスの表示と、参照音声から作るモデルの扱いは #920。パッケージ内の依存ライブラリと重みのライセンスは確認していない（§11）。

## 4. 対応環境の判定（`modelmgr/runtime.Evaluate`）

判定は **純粋関数**（`Evaluate(runtime, Platform{OS, Arch}, gpu.Report)`）。OS・CPU・アダプターを注入できるので、Linux の CI で macOS・非 NVIDIA の分岐もテストする。理由の文・代わりの案内は**カタログのデータ**（`support`）に持ち、画面・MCP・Audio Director が同じ文を使う。

| 順 | 条件 | `Support.Code` | 理由（カタログの文） |
|---|---|---|---|
| 1 | OS が `windows` でない | `os` | OS ごとの文（`darwin`: 「macOS では GPT-SoVITS の自動導入に対応していません。公式は macOS の MPS / CPU での推論に対応していますが、mam が自動で導入するのは Windows + NVIDIA の GPU の組み合わせだけです。」、`linux`、その他） |
| 2 | CPU が `amd64` でない | `arch` | 「この CPU（64 ビットの x86 以外）では…」 |
| 3 | アダプターを 1 つも取得できない | `no_gpu` | 「GPU を取得できなかったため…」 |
| 4 | NVIDIA のアダプター（ソフトウェアアダプターを除く）がない | `not_nvidia` | 「NVIDIA の GPU が見つからないため…AMD・Intel の GPU では使えません」 |
| 5 | NVIDIA の最大の専用メモリ < `vram.min_gb` | `low_vram` | 「{gpu} のビデオメモリは {vram_gb} GB で、GPT-SoVITS に必要な {min_vram_gb} GB に足りない…」 |
| — | 上のどれでもない | `ok` | `Variant` に、最大メモリの NVIDIA の Compute Capability に合う変種（`min_compute_cap` が最大のうち CC 以下のもの。CC 不明は基底の変種） |

- どの断りも `Fallback`（「代わりに、クラウドの音声合成（ElevenLabs）を使ってください。」）を伴う。`Support.Message()` は理由＋案内。ElevenLabs は引き続き選べる（クラウドへの切り替えは利用者が選ぶ。**黙って切り替えない**。設定の追加は #912 の別の子）。
- **VRAM の下限は 4 GB（推定）**。公式は推論に必要なメモリの数値を示していない。`vram.estimate=true` で、実機での確認が済むまで推定として表示する（§11）。
- GPU の取得は `gpu.DetectSystem`（DXGI と `nvidia-smi`）。`gpu.Adapter.ComputeCap`（NVIDIA のみ。`nvidia-smi` の値、無ければ名前から）を足した。`Status` / `List` は検出の結果を 1 分間使い回し（画面の定期読み込みのたびに `nvidia-smi` を起動しない）、`Install`・`Start`・`Prepare` は毎回検出し直す。
- 判断はジョブの実行時にも**もう一度**行う（待っている間・再起動で再開した間に環境が変わっても、対応していない環境には入れない）。導入済みでも、対応しなくなった環境（GPU を外した）では `Start` を断る（ファイルは残る。`Remove` で消す）。

## 5. 導入・起動・停止

### 5.1 導入（`runtime.Installer`）

1. 対応を判定（§4）→ 書き込み可否 → 空き容量（未取得分＋展開後。足りなければ `*fetch.NoSpaceError`）。
2. `downloads\<名前>.part` へ `Range` 再開つきで取得し、SHA-256・サイズを検証してからリネーム（`ErrChecksum`・`ErrSize` は `.part` を消す。取り消し・ネットワークの失敗は残して、次の導入が続きから再開する）。
3. 7z を `env.new\` へ展開し、`runtime\python.exe` と入口のスクリプト（`api_v2.py`）があることを確かめて `env\` と入れ替える（既存は `env.old\` に退避して成功で消す。失敗で戻す）。
4. `state.json` に版・変種・時刻を記録し、アーカイブと `downloads\` を消す。

進捗は展開前の取得 70 %・展開 30 %。実行中のプロセスは、ファイルを入れ替える前に止める。失敗は日本語にして出す（SHA-256 不一致・大きさ不一致・空き容量・取得元の 4xx・書き込めない場所）。

### 5.2 起動（`runtime.Installer.Command`、`proc.Manager`）

| 項目 | 内容 |
|---|---|
| コマンド | `env\runtime\python.exe -I -u -X utf8 api_v2.py -a 127.0.0.1 -p <空きポート> -c GPT_SoVITS/configs/tts_infer.yaml`（作業ディレクトリは `env\`）。公式の `go-webui.bat`（`runtime\python.exe -I …`、`runtime` を `PATH` の先頭へ）と同じ作りで、`-I`（隔離モード）により利用者の Python・`PYTHONPATH` に影響されない。`api_v2.py` が自分で `sys.path` に作業ディレクトリを足す |
| 待ち受け | **`127.0.0.1` だけ**（カタログの検証が、`127.0.0.1`・`{port}` を含み `0.0.0.0`・`::` を含まない引数だけを許す）。ポートは前回のポートを優先し、空いていなければ空きポート（`proc.FreePort`） |
| 応答の待ち | `GET /openapi.json` が 200（FastAPI の既定。**モデルを読み込んだ後にポートが開く**ので、応答があれば使える）。最長 5 分（初回はモデルの読み込みに時間がかかる） |
| オフライン設定 | `HF_HUB_OFFLINE=1`・`TRANSFORMERS_OFFLINE=1`・`HF_DATASETS_OFFLINE=1`・`HF_HUB_DISABLE_TELEMETRY=1`・`DO_NOT_TRACK=1`・`GRADIO_ANALYTICS_ENABLED=False`（カタログの `launch.env`）。実行中にライブラリが重みを取りに行かない。必要な重みは取得済みのものだけを使う。ミラー（`modelscope`）も `MODELSCOPE_CACHE` をランタイムの `cache\` に向ける |
| 書き込み先 | `HF_HOME`・`TORCH_HOME`・`MODELSCOPE_CACHE`・`XDG_CACHE_HOME` は `cache\`、`TEMP`・`TMP`・`TMPDIR` は `tmp\`（どちらもランタイムのディレクトリの中）。`PATH` の先頭に `env\runtime`。`PYTHONPATH`・`PYTHONHOME`・`PYTHONSTARTUP`・`VIRTUAL_ENV` は子に渡さない |
| 子の管理 | `proc.Manager`（ComfyUI と共通）。コンソールを出さない（Windows の `CREATE_NO_WINDOW`）。Windows は Job Object（`KILL_ON_JOB_CLOSE`）、Linux は Pdeathsig とプロセスグループで、MAM の終了・異常終了で子が残らない |
| 状態 | `stopped` / `starting` / `running` / `failed`（`proc.Status`）。起動の失敗は、エラー文（`GPT-SoVITS が起動しませんでした: …`）とログ末尾つきの `failed`。予期しない終了も `failed`。次の `Start` が再び試す |
| 遅延起動 | `Prepare(id)`（呼び出しの前に呼ぶ。動いていれば何もせず、止まっていれば `Start`）。**既定は遅延起動**。`SetAutoStart` で MAM の起動時に起動（`OnStartup`。待たずに別 goroutine で起動し、失敗はログと `Status` に出る） |
| 起動前の確認 | 対応環境（§4）→ 導入済み（`ErrNotInstalled`）→ 導入中でない（`ErrBusy`）。どれも通らなければ起動しない |

## 6. Service API（`internal/service/localruntime`）

ComfyUI の `modelmanager.Service` と同じ形（`JobRef`・`JobStatus`・`Watch`・`Prepare`・`CancelInstall`）。画面・MCP は #920 で載せる。

```go
func New(Deps) (*Service, error)
func (*Service) Register(next jobqueue.Handler)         // ジョブキュー開始前に。model_install のハンドラーを包む（§7）
func (*Service) OnStartup(ctx)                          // 取りこぼしの掃除・AutoStart
func (*Service) Close()                                 // 全ランタイムを止める（ジョブの停止の後）
func (*Service) List(ctx) []View                        // カタログの全ランタイム
func (*Service) Status(ctx, id) (View, error)           // 導入版と固定版・対応可否と理由・プロセス・空き容量・書き込み可否・ログ末尾・導入ジョブ
func (*Service) Install(ctx, id) (JobRef, error)        // 導入・更新。対応しない環境は *SupportError
func (*Service) Remove(ctx, id) error                   // 止めて env・downloads・cache・tmp を消す（設定とログは残す）。導入中は ErrBusy
func (*Service) Start(ctx, id) (proc.Status, error)
func (*Service) Stop(id) error
func (*Service) Prepare(ctx, id) error
func (*Service) SetAutoStart(id, on) error
func (*Service) CancelInstall(jobID) error              // 動いている導入の中断（ErrNotFound: 動いていない）
func (*Service) JobStatus(ctx, jobID) (JobStatus, error)
func (*Service) Watch(func(Event)) (cancel func())      // プロセスの状態・導入の進捗
```

- エラー: `ErrNotFound`・`ErrNotInstalled`・`ErrBusy`・`ErrUnsupported`（`*SupportError` が `errors.Is` で一致。`Support`＝コード・理由・案内を持ち、`Error()` は理由＋案内）。ディスクは `*fetch.NoSpaceError`、書き込めない場所は `store.ErrNotWritable`。
- **排他**: 同じランタイムの `Install` を重ねて呼ぶと同じジョブ（`JobRef.Existing=true`）。導入中の `Remove`・`Start` は `ErrBusy`。同じランタイムのジョブは 1 つずつ（ジョブ内で `work` ロック）。
- **ジョブ**: 種別は既存の `model_install`（**DB のマイグレーションなし**）、`target=runtime:<id>`、ペイロード `{target, variant}`。再開可（`attempts` が 3 未満の間）。`CancelInstall` で失敗（理由「導入を中断しました…」）で終わり、`.part` が残って次の導入が続きから再開する。
- `View` は `Support`（`supported`・`code`・`reason`・`fallback`・`variant`・`gpu`）・`pinned_version`・`installed`・`broken`・`update_available`・`process`・`auto_start`・`dir`・`writable`・`free_bytes`・`log_tail`・`variants`（必要な空き容量つき）・`install_job`・`license`・`vram`。

## 7. 配線（`internal/app`）

- `app/localruntime.go`: `modelmanager.New` の後に `localruntime.New` を作り、`Register(mgr.HandleInstall)` を呼ぶ。キューは種別ごとにハンドラーを 1 つしか持てないので、`Register` は `model_install` のハンドラーを**包み**、ペイロードの `target` が `runtime:` で始まるジョブはランタイムの、それ以外（`engine`・`model:<id>`・`hf:…`）は渡された `mgr.HandleInstall` に回す。`modelmanager.Service.handleInstall` は `HandleInstall` として公開しただけで、挙動は変えていない。
- `services.go`: `Services.LocalRuntimes`。`OnStartup` で取りこぼしを掃除し、`Close` はジョブの停止の後（ComfyUI のエンジンと同じ）。

## 8. 状態（`localai\<id>\state.json`）

`auto_start`・`last_port`・`installed`（`version`・`variant`・`installed_at`）。読めない記録は `state.json.corrupt` に退避して空から始める（MAM の起動を止めない。ファイルは残るので、`Status` は導入済みと見なさず、導入し直すと検証される）。ComfyUI の `comfyui\state.json` とは別のファイルで、互いに触れない。

## 9. MuseTalk（#921）が載せる形

`catalog/data/runtimes/musetalk.json` を足し、次を書く。コードの変更は要らない（要るのは MuseTalk の固有の部分だけ）。

| カタログの項目 | MuseTalk で決めること |
|---|---|
| `purpose` | `lip_sync` |
| `variants`・`archive_root` | 公式の Windows 向け配布物（7z）。Hugging Face のコミット固定が条件（`catalog.Validate` が、`https://huggingface.co/<repo>/resolve/<40 桁のコミット>/….7z` 以外を拒否する）。GitHub Releases が元なら、検証のパターンを広げる変更とその理由を spec に書く |
| `launch`（`python`・`entry`・`args`・`ready_path`・`env`） | 口パクのサーバーの起動。HTTP の入口が無ければ、その入口（薄いラッパーのスクリプト）をどこに置くかを #921 で決める（`api_v2.py` に相当するものが公式にない場合）。待ち受けは `127.0.0.1` だけ |
| `support` | 同じ判定（Windows x64 ＋ NVIDIA）。`fallback` は「Sync Labs」。macOS の第三者の MLX 版は #921 の判断 |
| `vram` | MuseTalk の下限（公式は 4 GB でも動作例ありと記載） |

## 10. 先送りしたこと（決めたことの記録）

- **ファインチューン（学習）はしない**。参照音声（約 5 秒）のゼロショットだけ（学習は VRAM と時間がかかり、macOS 非推奨）。
- **クラウドへのフォールバックは利用者が設定で選ぶ**。黙って切り替えない。設定の追加と Audio Director のプロバイダーは #912 の別の子。
- **Apple Silicon での自動導入**はしない（公式は MPS / CPU の推論に対応するが、パッケージと検証が別に要る）。オーナーの判断で別 Issue。
- 重みの取得・ライセンス表示（#920）、画面・MCP（#920）、MuseTalk（#921）。

## 11. 未確認

| 項目 | 状態 |
|---|---|
| **実物の Windows ＋ NVIDIA での統合パッケージの導入（約 8 GB の取得と約 14 GB の展開）と、`api_v2.py` の起動・応答** | **未確認**。合成 7z（`extract/testdata/bcj2.7z`）と偽の取得元・偽のプロセス（このテストバイナリ自身）で、取得・`Range` 再開・ハッシュ／サイズ不一致・空き容量・取り消し・展開・入れ替え・起動・停止・失敗の分岐を確認。実物の 7z は、ヘッダー（58,007 個のファイル、展開後 14,128,680,597 バイト）だけを `Range` で読み、`runtime\python.exe`・`api_v2.py`・`GPT_SoVITS\configs\tts_infer.yaml` があることを確かめた（本体は取っていない） |
| 14 GB・5.8 万ファイルの 7z を `bodgit/sevenzip` で展開するときのメモリ・時間・Windows の長いパス | **未確認**（ComfyUI の 4.4 GB のものとは桁が違う。ヘッダーの読み込みは 166 回の `Range` で済んだ） |
| `-I` ＋ 作業ディレクトリ `env\` で `api_v2.py` が `tools`・`GPT_SoVITS` を import できること、`GET /openapi.json` が 200 になるまでの時間 | **未確認**（`api_v2.py` のソースで `sys.path` への追加と FastAPI の既定の `/openapi.json` を読んで確かめたが、実行していない） |
| オフライン設定（`HF_HUB_OFFLINE` ほか）で、統合パッケージの推論が重みを取りに行かず動くこと | **未確認**（重みは同梱されているが、実機で起動して確かめていない） |
| VRAM の下限 4 GB（推定） | **未確認**（公式に数値がない。`vram.estimate=true`） |
| RTX 50 シリーズ向けパッケージ（`nvidia50`）の起動、`nvidia-smi` の `compute_cap` による選択 | **未確認**（判定は純粋関数のテストだけ。実機の `nvidia-smi` は使っていない） |
| Windows の Job Object・非表示ウィンドウでの起動 | model-manager.md §11 と同じ（ビルドと vet のみ。Linux の `Pdeathsig` は子プロセスのテストで確認済み） |
| パッケージ内の Python パッケージ・重みのライセンスと商用利用の可否 | **未確認**（一覧を調べていない。`THIRD_PARTY_NOTICES.md` に明記。重みごとの確認と画面の表示は #920） |
| 日本語の音声の質 | **未確認**（公式の数値がない。実機で聞き比べる。親 Issue のメモ） |
