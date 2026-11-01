# ローカルランタイム（GPT-SoVITS・のちに MuseTalk）

[model-manager.md](model-manager.md)（同梱の ComfyUI エンジン）と同じ作りで、**声（GPT-SoVITS）と口パク（MuseTalk 1.5）をローカルで動かす実行環境を、MAM が自動で導入・起動・停止・状態表示まで管理する**ための基盤。親 Issue は #912（決定事項: 実行方式 B「ComfyUI の同梱エンジンと同じ感じで、mam が自動で入れて起動まで管理する」）。この文書は基盤と GPT-SoVITS の環境導入（Issue #918）、重み・依存ファイルの固定カタログと取得・検証・出典とライセンスの表示・画面と MCP（Issue #920。§12）を定める。実装は `backend/internal/modelmgr/runtime`（導入・判定・起動コマンド・重みの検証と取得）、`backend/internal/modelmgr/catalog`（カタログ）と `backend/internal/service/localruntime`（Service）。改訂履歴は [model-manager.changelog.md](model-manager.changelog.md)。

| 範囲 | Issue |
|---|---|
| ランタイム管理の基盤、GPT-SoVITS の環境の導入・起動・停止・状態 | **#918（この文書）** |
| 重み・依存ファイルの固定カタログ（固定 URL＋SHA-256、再開可）、出典とライセンスの画面表示、画面・MCP | **#920（§12）** |
| MuseTalk 1.5 のランタイム | #921（この文書の枠に載せる） |

## 1. 方針（変えない前提）

- **ComfyUI を経由せず、ランタイムごとに独立した Python 環境とプロセスを持つ**。GPT-SoVITS は公式の `api_v2.py`（FastAPI）を、MAM が `127.0.0.1` 限定で起動して HTTP で呼ぶ。ComfyUI のカスタムノード経由にしない（親 Issue「ComfyUI のカスタムノードは古いものが多い」。決定事項 1 で、MAM が管理する独立の実行環境・プロセスに決まった）。
- **ランタイムの種類はデータ**（`catalog/data/runtimes/<名前>.json`）。コードは GPT-SoVITS を知らない。MuseTalk は同じ形の JSON を足し、起動の引数・確認パス・対応環境の判定の文を書くだけで載る（§9）。
- **MAM は何も同梱しない**。公式の配布先から取得する別プログラムで、`THIRD_PARTY_NOTICES.md`（GPT-SoVITS の節）に載せる。ライセンスと商用利用の可否の表示は §12（#920）。
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

**重みについて**: この統合パッケージは、v2Pro の**学習済みモデルの重み（`GPT_SoVITS\pretrained_models` ほか）を含む**（約 8 GB の大半）。MAM のインストーラー・リリースには含めず、ランタイムの取得で公式の配布先から一緒に入る。重みごとの出典・ライセンスの表示は §12。パッケージ内の Python パッケージ（約 100 個）のライセンスは確認していない（§11）。

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

ComfyUI の `modelmanager.Service` と同じ形（`JobRef`・`JobStatus`・`Watch`・`Prepare`・`CancelInstall`）。画面・MCP は #920（§12.5）で載せた。

```go
func New(Deps) (*Service, error)
func (*Service) Register(next jobqueue.Handler)         // ジョブキュー開始前に。model_install のハンドラーを包む（§7）
func (*Service) OnStartup(ctx)                          // 取りこぼしの掃除・AutoStart
func (*Service) Close()                                 // 全ランタイムを止める（ジョブの停止の後）
func (*Service) List(ctx) []View                        // カタログの全ランタイム
func (*Service) Status(ctx, id) (View, error)           // 導入版と固定版・対応可否と理由・プロセス・空き容量・書き込み可否・ログ末尾・導入ジョブ
func (*Service) Install(ctx, id) (JobRef, error)        // 導入・更新。対応しない環境は *SupportError
func (*Service) Remove(ctx, id) error                   // 止めて env・downloads・cache・tmp を消す（設定とログは残す）。導入中は ErrBusy
func (*Service) InstallWeights(ctx, id) (JobRef, error) // 重み・依存ファイルの確認と取得だけ（§12.3）。未導入は ErrNotInstalled
func (*Service) CheckStart(ctx, id) error               // Start の前提（対応環境・導入済み・導入中でない・重みがそろう）。満たさなければ理由のエラー
func (*Service) Start(ctx, id) (proc.Status, error)
func (*Service) Stop(id) error
func (*Service) Prepare(ctx, id) error
func (*Service) SetAutoStart(id, on) error
func (*Service) CancelInstall(jobID) error              // 動いている導入の中断（ErrNotFound: 動いていない）
func (*Service) JobStatus(ctx, jobID) (JobStatus, error)
func (*Service) Watch(func(Event)) (cancel func())      // プロセスの状態・導入の進捗
```

- エラー: `ErrNotFound`・`ErrNotInstalled`・`ErrBusy`・`ErrWeightsMissing`（`*WeightsError`）・`ErrSourceGone`（§12.4）・`ErrUnsupported`（`*SupportError` が `errors.Is` で一致。`Support`＝コード・理由・案内を持ち、`Error()` は理由＋案内）。ディスクは `*fetch.NoSpaceError`、書き込めない場所は `store.ErrNotWritable`。
- **排他**: 同じランタイムの `Install` を重ねて呼ぶと同じジョブ（`JobRef.Existing=true`）。導入中の `Remove`・`Start` は `ErrBusy`。同じランタイムのジョブは 1 つずつ（ジョブ内で `work` ロック）。
- **ジョブ**: 種別は既存の `model_install`（**DB のマイグレーションなし**）、`target=runtime:<id>`、ペイロード `{target, variant, weights_only}`。再開可（`attempts` が 3 未満の間）。`CancelInstall` で失敗（理由「導入を中断しました…」）で終わり、`.part` が残って次の導入が続きから再開する。
- `View` は `Support`（`supported`・`code`・`reason`・`fallback`・`variant`・`gpu`）・`pinned_version`・`installed`・`broken`・`update_available`・`process`・`auto_start`・`dir`・`writable`・`free_bytes`・`log_tail`・`variants`（必要な空き容量つき）・`install_job`・`license`・`vram`・`weights`（§12.4）。

## 7. 配線（`internal/app`）

- `app/localruntime.go`: `modelmanager.New` の後に `localruntime.New` を作り、`Register(mgr.HandleInstall)` を呼ぶ。キューは種別ごとにハンドラーを 1 つしか持てないので、`Register` は `model_install` のハンドラーを**包み**、ペイロードの `target` が `runtime:` で始まるジョブはランタイムの、それ以外（`engine`・`model:<id>`・`hf:…`）は渡された `mgr.HandleInstall` に回す。`modelmanager.Service.handleInstall` は `HandleInstall` として公開しただけで、挙動は変えていない。
- `services.go`: `Services.LocalRuntimes`。`OnStartup` で取りこぼしを掃除し、`Close` はジョブの停止の後（ComfyUI のエンジンと同じ）。

## 8. 状態（`localai\<id>\state.json`）

`auto_start`・`last_port`・`installed`（`version`・`variant`・`installed_at`）。読めない記録は `state.json.corrupt` に退避して空から始める（MAM の起動を止めない。ファイルは残るので、`Status` は導入済みと見なさず、導入し直すと検証される）。ComfyUI の `comfyui\state.json` とは別のファイルで、互いに触れない。

## 9. MuseTalk（#921）が載せる形

`catalog/data/runtimes/musetalk.json` を足し、次を書く。**重みと依存ファイルは #920 が `catalog/data/assets/musetalk.json` に固定済み**（§12.7）。#921 は同じ ID `musetalk` のランタイムを足すだけで結び付き、必要なら asset を足す（asset の無いランタイムは検証が落ちる）。コードの変更は要らない（要るのは MuseTalk の固有の部分だけ）。

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
- 重みの取得・ライセンス表示・画面・MCP は #920 で実装した（§12）。MuseTalk のランタイムのパッケージと起動は #921。

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
| パッケージ内の Python パッケージ・重みのライセンスと商用利用の可否 | **重みと依存ファイルは §12 で確認した（`unclear` が 5 件）。パッケージ内の Python パッケージ（約 100 個）は一覧を調べていない**（`THIRD_PARTY_NOTICES.md` に明記） |
| 日本語の音声の質 | **未確認**（公式の数値がない。実機で聞き比べる。親 Issue のメモ） |

## 12. 重み・依存ファイルの固定カタログ（Issue #920）

ランタイムが動くために**ディスクに無ければならない重み（学習済みモデル）と依存ファイル**（辞書・コーパス・言語判定モデルなど）を、**ランタイムごとのカタログ**（`catalog/data/assets/<ランタイム ID>.json`）に固定する。親 Issue の決定事項 1（重みは同梱せず実行時にダウンロード。固定 URL＋SHA-256 検証、再開可）と 3（出典とライセンスを画面と docs に出し、商用利用がはっきりしないものは画面で目立つように注記）の実装。Go の型は `catalog.ResourceSet`（1 ランタイムの一覧）・`catalog.Resource`（1 件。JSON のキーは `assets`）で、`Asset`（§3 のアーカイブ）と名前が重ならないよう `Resource` と呼ぶ。

### 12.1 スキーマ

```json
{
 "runtime": "gpt-sovits",
 "licenses": [ { "id": "mit-gpt-sovits", "name": "…", "url": "https://…", "summary": "…", "commercial_use": "yes", "notice": "" } ],
 "assets": [
  { "name": "…", "description": "…", "dest": "GPT_SoVITS/pretrained_models/…",
    "url": "https://huggingface.co/<owner>/<repo>/resolve/<40 桁のコミット>/<パス>", "sha256": "…", "size": 155315150, "required": true,
    "source": { "name": "lj1995/GPT-SoVITS（Hugging Face）", "url": "https://huggingface.co/lj1995/GPT-SoVITS", "source_note": "" },
    "license": "mit-gpt-sovits" },
  { "name": "…", "description": "…", "format": "zip", "url": "…/G2PWModel.zip", "sha256": "…", "size": 588856634, "required": true,
    "members": [ { "entry": "G2PWModel/g2pW.onnx", "dest": "GPT_SoVITS/text/G2PWModel/g2pW.onnx", "size": 635212732, "sha256": "…" } ],
    "source": { … }, "license": "…" }
 ]
}
```

- **置き場所は 1 ランタイム 1 ファイル**（`data/assets/<ランタイム ID>.json`）。GPT-SoVITS の ID は `gpt-sovits`（Issue の本文は `gptsovits.json` に置くとしていたが、ランタイムの定義（`data/runtimes/*.json`）とは別にした。理由: ランタイムのパッケージ（§3）の固定と重みの固定は別の変更で、別にレビューされる。MuseTalk のランタイムのパッケージは #921 で決まるので、その前に重みだけを記録できる）。ランタイムが無い ID のファイルがあってもよい（MuseTalk。#921 がランタイムを足すと、同じ ID で結び付く）。逆に、ファイルが無いランタイムは検証が落ちる。
- `dest` は**環境ディレクトリ（`env\`）の下の相対パス**（`/` 区切り。`..`・絶対パス・ドライブを拒否）。パッケージの中のファイルと、パッケージの外から足すファイルを区別しない（無ければ取得、あれば検証、が同じ規則）。
- **単一のファイル**（`dest`）か、**アーカイブ**（`format` が `zip` / `tar.gz` と `members`）。アーカイブの `sha256`・`size` はアーカイブのもの、`members[]` は取り出す各ファイルのエントリ名・`dest`・サイズ・SHA-256。公式が配る形が zip / tar.gz だけの依存（G2PW・NLTK のデータ・Open JTalk の辞書）のために要る。取り出しは `exeinstall.Extract`（ComfyUI 以外の Codex・ffmpeg の導入と共通。エントリのパスで書かず、名前・サイズ・SHA-256 を照合し、失敗なら書いたものを消す）。
- `required`: true は、無いと**起動を断る**。false は取得するが起動は止めない。
- `source`（出典）: `name` と `url`（**原本**のリポジトリ・ファイルの場所）。取得先（`url`）が原本と同じ Hugging Face のリポジトリならそれだけ。**そうでない（第三者のミラー・原本が Google Drive や GitHub にある）ときは `source_note` が必須**で、原本の場所と、**原本と SHA-256 が一致することを確かめた日付（`YYYY-MM-DD`）**を書く（[model-manager.md §4.1](model-manager.md) の `files[].source_note` と同じ作り）。
- `license`: そのファイルの `licenses[].id`。`commercial_use` は `yes` / `no` / **`unclear`**。`no` と `unclear` は `notice`（何に気をつけるか）が必須。`summary` には、**確かめた根拠**（モデルカードの `license:`・同梱の LICENSE・配布一覧の記載）と、**確かめられなかったこと**を書く。`ComfyUI` のモデルの `catalog.License`（`commercial_use` が bool）とは別の型にした（`unclear` は bool で表せない）。

### 12.2 検証（`catalog.Validate`）

URL は `https://huggingface.co/<owner>/<repo>/resolve/<40 桁のコミット>/<パス>` だけ（ブランチ名・http・他のホストを拒否。**取得の許可ホストは増やさない**。GitHub や Google Drive にある原本は、SHA-256 を確かめた Hugging Face のコピーを使う）。SHA-256 は小文字 64 桁、サイズは正。出典・ライセンスの欠落、ライセンス ID が一覧に無い、同じ名前・同じ `dest` が 2 件、`unclear` / `no` で `notice` が空、`commercial_use` が 3 値以外、取得先と原本が違うのに `source_note` に実在する日付が無い、アーカイブの `members` が空・エントリが `..` を含む・`dest` が外へ出る・メンバーの SHA-256 かサイズが無い、`required` が 1 件も無い、を拒否する。テスト: `catalog/assets_test.go`。

### 12.3 取得・検証（`runtime.Installer.EnsureAssets`）

1. 各 asset を**ディスクと照合**する（`stat` だけ）: 全ファイルがあって大きさが同じで、`state.json` の `files`（検証済みのファイルのサイズ・更新時刻）と一致 → `ok`。ファイルが無い・大きさが違う → `missing`。あって大きさも同じだが記録が無い・違う → `unverified`（パッケージを展開した直後、または誰かが書き換えた）。
2. `unverified` は**その場で SHA-256 を計算**する（進捗フェーズ `verify`）。一致 → 記録して終わり（**取り直さない**）。不一致 → **そのファイルを消して**取得に回す。
3. `missing` は固定 URL から取得する（`fetch.Download`: 単一ファイルは最終の置き場所の `.part` へ `Range` で再開、SHA-256 とサイズを照合してからリネーム。不一致は `.part` を消して `fetch.ErrChecksum` / `ErrSize`。アーカイブは `downloads\` へ取得し、メンバーを取り出して照合してから、アーカイブを消す）。
4. 始める前に、**取得が要る分（`.part` の分は引く＋アーカイブは取り出したファイルの分）の空き容量**を確かめる（足りなければ `*fetch.NoSpaceError`。何も取得しない）。書き込める場所かも確かめる。
5. 失敗は `*runtime.AssetError`（どの asset か）。それ以前に終えた asset はそのまま残る。取り消し・ネットワークの失敗は `.part` を残し、次の実行が続きから再開する。進捗は、取得・検証が要る asset の大きさの合計に対するバイト数（フェーズ `verify` / `weights`）。
6. パッケージの導入（§5.1）は、**環境を入れ替えたら記録を消し**（`files` を空に）、同じジョブの最後にこの手順を行う（進捗: パッケージの取得 55 % → 展開 20 % → 重みの確認・取得 25 %）。`Remove` も記録を消す。

**重みだけを導入する**（`Service.InstallWeights`。`model_install` のジョブ、`target=runtime:<id>`、ペイロードの `weights_only`）: 導入済みのランタイムで、足りない・壊れたファイルだけを直す（ファイルを消した・ウイルス対策に隔離された・取得が途中で止まった）。未導入は `ErrNotInstalled`、対応しない PC は `*SupportError`、空き容量不足は `*fetch.NoSpaceError`（どれもジョブを作らず断る）。同じランタイムのジョブが動いていれば、それを返す（`Existing`）。

### 12.4 起動の前に分かる

`Service.CheckStart`（`Start`・`Prepare` が最初に呼ぶ）は、対応環境 → 導入済み → 導入中でない → **必須の重みがそろって検証済み**、の順に調べる。そろっていなければ `*WeightsError`（`errors.Is(err, ErrWeightsMissing)`）で断り、サーバーを起動しない（起動後の Python の読み込み失敗にしない）。メッセージは「GPT-SoVITS の重みが不足しています（<名前>、…）。「重みを導入」を実行してください」、`unverified` なら「…の重みの確認が済んでいません…」。`View.Weights` は `state`（`none`: 未導入 / `ready` / `missing` / `unverified`）・`message`（`missing` なら「**導入済み（重みが不足）**。「重みを導入」で、足りないファイルを取得元から取得します」）・`assets[]`（名前・説明・状態・必須・大きさ・出典・ライセンス）・`not_ready`・`need_bytes`・`total_bytes`・`unclear`（`commercial_use` が `yes` でない asset の数）。ライセンスと出典は**未導入でも、この PC で使えない環境でも**返す（導入の前に読めるように）。

取得元が消えたとき（404・410）は、ジョブを失敗にして `ErrSourceGone` で包み、「取得元が消えています（<名前>、HTTP 404）。…」に、**第三者のミラーなら `source_note`（原本の場所）、そうでなければ原本の URL** を続ける。ハッシュ不一致は「ダウンロードしたファイルの SHA-256 が一致しません。ダウンロードを破棄しました」（`asset` の名前つき）。

### 12.5 表示

- **画面**: モデルマネージャーの区画「ローカル音声・口パク」（[model-manager-screen.md](../../components/model-manager-screen.md)、[endpoints-model-manager.md](../../api/endpoints-model-manager.md)）。ランタイムごとのカードに、状態・導入・起動・停止・自動起動・削除・進捗、**この PC で使えない理由とクラウドの案内**、重みの一覧（出典のリンク・ライセンス名・商用利用の可否）を出す。**`unclear` / `no` の重みは、一覧の前に琥珀色の枠で件数と名前を挙げ、一覧を開いたままにし、その行にも `notice` を枠つきで出す**。
- **MCP**: `list_local_runtimes`・`get_local_runtime_status`（`weights.assets[]` に出典・ライセンス・`commercial_use` と `notice`）、`install_local_runtime`・`install_local_runtime_weights`・`start_local_runtime`・`stop_local_runtime`・`remove_local_runtime`・`set_local_runtime_autostart`（[mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)）。画面も MCP も同じ `localruntime.Service` を呼び、拒否は `mmview.RefuseRuntime`（`mmview.Refuse` の種類 409 / `conflict`、422 / `validation`、404 / `not_found` に、ランタイム固有のエラーを載せたもの）で同じ文面になる。
- **docs**: [THIRD_PARTY_NOTICES.md](../../../THIRD_PARTY_NOTICES.md) の「利用者が取得する別プログラム」の節に、GPT-SoVITS・MuseTalk の重みと依存の一覧（出典・ライセンス・商用利用）を載せる。下の 12.6・12.7 の表と同じ内容で、`scripts/third-party-notices.sh` の対象（同梱物）ではないので手で書く。カタログを変えたらこの 2 か所を一緒に直す。

### 12.6 GPT-SoVITS の重みと依存（`data/assets/gpt-sovits.json`）

**洗い出し**: 公式の Windows 統合パッケージ（§3。`GPT-SoVITS-v2pro-20250604.7z`）の中身のヘッダー（58,007 ファイル）と、実際に展開したファイルの SHA-256 を調べ、**推論（`api_v2.py` と、既定の設定 `custom` = v2、日本語・英語・中国語）が読むファイル**と、**ライブラリが実行時に自分で取りに行く先**を突き合わせた。

| ライブラリ | 無いときの実行時の取得 | 結果 |
|---|---|---|
| `fast_langdetect` 0.3.1 | `lid.176.bin` を Facebook のサイトから取得（保存先 `GPT_SoVITS/pretrained_models/fast_langdetect`） | パッケージに同梱。asset にした |
| `pyopenjtalk` 0.3.4 | Open JTalk の辞書を GitHub から取得 | パッケージに同梱（`site-packages/pyopenjtalk/open_jtalk_dic_utf_8-1.11`）。asset にした |
| `nltk` 3.8.1・`g2p_en` 2.1.0 | `averaged_perceptron_tagger`・`cmudict` を取得（3.8.1 は `_eng` を要求しない） | パッケージに同梱（`runtime/nltk_data`）。asset にした |
| g2pW（`text/chinese2.py`） | モデルは `GPT_SoVITS/text/G2PWModel` を読むだけ（取得しない） | パッケージに同梱。asset にした |
| `transformers`・`huggingface_hub` | `from_pretrained` は `GPT_SoVITS/pretrained_models/...` のローカルのパス（`HF_HUB_OFFLINE=1` でも読める） | 重みは asset にした |
| BigVGAN・v3/v4 の重み | v3/v4 の設定だけが読む | 既定の設定（v2）では読まないので asset にしない（パッケージには入っている） |

結論: **統合パッケージは推論の依存を全部含むので、隠れた実行時の取得は残っていない**。asset は「パッケージが置くはずのファイル」を固定して**検証し、無い・壊れていれば取り直す**ためのもので、パッケージを導入した直後は、取得は起きず SHA-256 の検証だけが走る（25 ファイルすべて、公式の 7z の中身のバイトと一致することを確かめた）。v2Pro の設定（`s1v3.ckpt`・`v2Pro/s2Gv2Pro.pth`・`sv/…`）は、`launch.args` が `custom`（v2）を既定にしているので asset にしない。v2Pro / v4 を使うときに、その重みを足す。

| 名前 | 置き場所（環境ディレクトリの下） | 取得元（コミット固定） | サイズ | 出典 | ライセンス | 商用利用 |
|---|---|---|---|---|---|---|
| GPT-SoVITS v2 の GPT モデル | `GPT_SoVITS/pretrained_models/gsv-v2final-pretrained/s1bert25hz-5kh-longer-epoch=12-step=369668.ckpt` | `lj1995/GPT-SoVITS`@`8f885756` `gsv-v2final-pretrained/s1bert25hz-5kh-longer-epoch=12-step=369668.ckpt` | 155,315,150 | lj1995/GPT-SoVITS（Hugging Face） | MIT License（GPT-SoVITS の学習済みモデル） | 可 |
| GPT-SoVITS v2 の SoVITS モデル | `GPT_SoVITS/pretrained_models/gsv-v2final-pretrained/s2G2333k.pth` | `lj1995/GPT-SoVITS`@`8f885756` `gsv-v2final-pretrained/s2G2333k.pth` | 106,035,259 | lj1995/GPT-SoVITS（Hugging Face） | MIT License（GPT-SoVITS の学習済みモデル） | 可 |
| chinese-hubert-base の設定 | `GPT_SoVITS/pretrained_models/chinese-hubert-base/config.json` | `lj1995/GPT-SoVITS`@`8f885756` `chinese-hubert-base/config.json` | 1,449 | lj1995/GPT-SoVITS（Hugging Face。原本は TencentGameMate/chinese-hubert-base） | MIT License（chinese-hubert-base） | 可 |
| chinese-hubert-base の前処理の設定 | `GPT_SoVITS/pretrained_models/chinese-hubert-base/preprocessor_config.json` | `lj1995/GPT-SoVITS`@`8f885756` `chinese-hubert-base/preprocessor_config.json` | 212 | lj1995/GPT-SoVITS（Hugging Face。原本は TencentGameMate/chinese-hubert-base） | MIT License（chinese-hubert-base） | 可 |
| chinese-hubert-base の重み | `GPT_SoVITS/pretrained_models/chinese-hubert-base/pytorch_model.bin` | `lj1995/GPT-SoVITS`@`8f885756` `chinese-hubert-base/pytorch_model.bin` | 188,811,417 | lj1995/GPT-SoVITS（Hugging Face。原本は TencentGameMate/chinese-hubert-base） | MIT License（chinese-hubert-base） | 可 |
| chinese-roberta-wwm-ext-large の設定 | `GPT_SoVITS/pretrained_models/chinese-roberta-wwm-ext-large/config.json` | `lj1995/GPT-SoVITS`@`8f885756` `chinese-roberta-wwm-ext-large/config.json` | 963 | lj1995/GPT-SoVITS（Hugging Face。原本は hfl/chinese-roberta-wwm-ext-large） | Apache License 2.0（chinese-roberta-wwm-ext-large） | 可 |
| chinese-roberta-wwm-ext-large のトークナイザー | `GPT_SoVITS/pretrained_models/chinese-roberta-wwm-ext-large/tokenizer.json` | `lj1995/GPT-SoVITS`@`8f885756` `chinese-roberta-wwm-ext-large/tokenizer.json` | 268,962 | lj1995/GPT-SoVITS（Hugging Face。原本は hfl/chinese-roberta-wwm-ext-large） | Apache License 2.0（chinese-roberta-wwm-ext-large） | 可 |
| chinese-roberta-wwm-ext-large の重み | `GPT_SoVITS/pretrained_models/chinese-roberta-wwm-ext-large/pytorch_model.bin` | `lj1995/GPT-SoVITS`@`8f885756` `chinese-roberta-wwm-ext-large/pytorch_model.bin` | 651,225,145 | lj1995/GPT-SoVITS（Hugging Face。原本は hfl/chinese-roberta-wwm-ext-large） | Apache License 2.0（chinese-roberta-wwm-ext-large） | 可 |
| fastText の言語判定モデル（lid.176） | `GPT_SoVITS/pretrained_models/fast_langdetect/lid.176.bin` | `XXXXRT/GPT-SoVITS-Pretrained`@`0c47645e` `pretrained_models/fast_langdetect/lid.176.bin` | 131,266,198 | fastText（Facebook AI Research）の lid.176.bin | CC BY-SA 3.0（fastText の言語判定モデル） | 可 |
| NLTK の cmudict（英語の発音辞書） | （アーカイブ `zip` の 1 ファイル: `cmudict`） | `XXXXRT/GPT-SoVITS-Pretrained`@`0c47645e` `nltk_data.zip` | 9,924,448 | NLTK Data（nltk/nltk_data） | CMU Pronouncing Dictionary の利用条件 | 可 |
| NLTK の Averaged Perceptron Tagger（英語の品詞推定） | （アーカイブ `zip` の 1 ファイル: `averaged_perceptron_tagger.pickle`） | `XXXXRT/GPT-SoVITS-Pretrained`@`0c47645e` `nltk_data.zip` | 9,924,448 | NLTK Data（nltk/nltk_data） | MIT License（Averaged Perceptron Tagger） | 可 |
| Open JTalk の辞書（日本語の読みと音の並び） | （アーカイブ `tar.gz` の 8 ファイル: `sys.dic`、`matrix.bin`、`char.bin`、`unk.dic`、`left-id.def`、`right-id.def`、`pos-id.def`、`rewrite.def`） | `XXXXRT/GPT-SoVITS-Pretrained`@`0c47645e` `open_jtalk_dic_utf_8-1.11.tar.gz` | 23,646,843 | Open JTalk の辞書 open_jtalk_dic_utf_8-1.11（pyopenjtalk） | BSD 3-Clause（Open JTalk の辞書） | 可 |
| g2pW（中国語の読みの推定） | （アーカイブ `zip` の 6 ファイル: `g2pW.onnx`、`config.py`、`char_bopomofo_dict.json`、`bopomofo_to_pinyin_wo_tune_dict.json`、`MONOPHONIC_CHARS.txt`、`POLYPHONIC_CHARS.txt`） | `XXXXRT/GPT-SoVITS-Pretrained`@`0c47645e` `G2PWModel.zip` | 588,856,634 | g2pW（GitYCC/g2pW）の ONNX モデル G2PWModel | g2pW の学習済みモデル（配布条件を確認できていない） | **はっきりしない** |

- 公式の重み（`lj1995/GPT-SoVITS`）は、GPT-SoVITS の作者のリポジトリで、モデルカードに `license: mit`。コードも MIT。**学習データのライセンスまでは確認していない**（`summary` にそう書く）。
- `chinese-hubert-base`・`chinese-roberta-wwm-ext-large` は、`lj1995/GPT-SoVITS` が配る**変換後のファイル**（原本より小さい）。原本のリポジトリ（`TencentGameMate/chinese-hubert-base`: MIT、`hfl/chinese-roberta-wwm-ext-large`: Apache-2.0）のライセンスを、そのまま当てている。
- `lid.176.bin`・`nltk_data.zip`・`open_jtalk_dic_utf_8-1.11.tar.gz`・`G2PWModel.zip` は、GPT-SoVITS 公式の `install.sh` が使う Hugging Face の配布先 `XXXXRT/GPT-SoVITS-Pretrained`（`license: mit`）のコピーで、`source_note` に原本の場所と確かめた日付（2026-11-01）を書いた。lid.176.bin は Facebook の原本、Open JTalk の辞書は `r9y9/open_jtalk` v1.11.1 のリリース、NLTK の 2 つは NLTK の配布一覧の SHA-256 と一致することを確かめた。
- **g2pW のモデルは `commercial_use: unclear`**: コード（`GitYCC/g2pW`）は Apache-2.0 だが、配布されるモデル（ONNX）のライセンス表示を、配布先でも原本のリポジトリでも確認できなかった。画面に目立つ注記を出す。これが現時点で `unclear` の唯一の GPT-SoVITS の asset。

### 12.7 MuseTalk 1.5 の重みと依存（`data/assets/musetalk.json`）

MuseTalk のランタイムのパッケージは #921 が決める。ここでは、**決定事項 3 に挙がった依存の重み**（whisper-tiny・sd-vae-ft-mse・DWPose・face-parse-bisent・LatentSync の SyncNet）と MuseTalk 1.5 の重み、さらに**実行時に自分で取りに行く先**を、MuseTalk の `download_weights.sh` と推論のコード（コミット `0a89dec4`）から洗い出して固定した。配置は MuseTalk の既定のパス（`models/musetalkV15/unet.pth` ほか。環境ディレクトリ＝ MuseTalk のリポジトリの根）。

| 名前 | 置き場所（環境ディレクトリの下） | 取得元（コミット固定） | サイズ | 出典 | ライセンス | 商用利用 |
|---|---|---|---|---|---|---|
| MuseTalk 1.5 の重み（unet.pth） | `models/musetalkV15/unet.pth` | `TMElyralab/MuseTalk`@`2bcb936e` `musetalkV15/unet.pth` | 3,400,074,924 | TMElyralab/MuseTalk（Hugging Face） | MIT License（MuseTalk の学習済みモデル） | 可 |
| MuseTalk 1.5 の設定（musetalk.json） | `models/musetalkV15/musetalk.json` | `TMElyralab/MuseTalk`@`2bcb936e` `musetalkV15/musetalk.json` | 748 | TMElyralab/MuseTalk（Hugging Face） | MIT License（MuseTalk の学習済みモデル） | 可 |
| sd-vae-ft-mse の重み | `models/sd-vae/diffusion_pytorch_model.bin` | `stabilityai/sd-vae-ft-mse`@`31f26fde` `diffusion_pytorch_model.bin` | 334,707,217 | stabilityai/sd-vae-ft-mse（Hugging Face） | MIT License（sd-vae-ft-mse） | 可 |
| sd-vae-ft-mse の設定 | `models/sd-vae/config.json` | `stabilityai/sd-vae-ft-mse`@`31f26fde` `config.json` | 547 | stabilityai/sd-vae-ft-mse（Hugging Face） | MIT License（sd-vae-ft-mse） | 可 |
| whisper-tiny の重み | `models/whisper/pytorch_model.bin` | `openai/whisper-tiny`@`169d4a43` `pytorch_model.bin` | 151,095,027 | openai/whisper-tiny（Hugging Face） | Apache License 2.0（whisper-tiny） | 可 |
| whisper-tiny の設定 | `models/whisper/config.json` | `openai/whisper-tiny`@`169d4a43` `config.json` | 1,983 | openai/whisper-tiny（Hugging Face） | Apache License 2.0（whisper-tiny） | 可 |
| whisper-tiny の前処理の設定 | `models/whisper/preprocessor_config.json` | `openai/whisper-tiny`@`169d4a43` `preprocessor_config.json` | 184,990 | openai/whisper-tiny（Hugging Face） | Apache License 2.0（whisper-tiny） | 可 |
| DWPose の重み（dw-ll_ucoco_384.pth） | `models/dwpose/dw-ll_ucoco_384.pth` | `yzd-v/DWPose`@`1a714410` `dw-ll_ucoco_384.pth` | 406,878,486 | yzd-v/DWPose（Hugging Face） | Apache License 2.0（DWPose） | 可 |
| LatentSync の SyncNet（latentsync_syncnet.pt） | `models/syncnet/latentsync_syncnet.pt` | `ByteDance/LatentSync`@`405eda8e` `latentsync_syncnet.pt` | 1,488,019,828 | ByteDance/LatentSync（Hugging Face） | CreativeML OpenRAIL++（LatentSync の配布物） | **はっきりしない** |
| S3FD の顔検出の重み（s3fd-619a316812.pth） | `musetalk/utils/face_detection/detection/sfd/s3fd.pth` | `ByteDance/LatentSync`@`405eda8e` `auxiliary/s3fd-619a316812.pth` | 89,843,225 | face-alignment（1adrianb）の S3FD の重み | S3FD の学習済みモデル（ライセンスの表示を確認できていない） | **はっきりしない** |
| face-parse-bisent の重み（79999_iter.pth） | `models/face-parse-bisent/79999_iter.pth` | `ManyOtherFunctions/face-parse-bisent`@`0073b233` `79999_iter.pth` | 53,289,463 | zllrunning/face-parsing.PyTorch の学習済みモデル 79999_iter.pth | face-parsing.PyTorch の学習済みモデル（学習データが研究用） | **はっきりしない** |
| face-parse-bisent の ResNet-18（resnet18-5c106cde.pth） | `models/face-parse-bisent/resnet18-5c106cde.pth` | `ManyOtherFunctions/face-parse-bisent`@`0073b233` `resnet18-5c106cde.pth` | 46,827,520 | torchvision の resnet18-5c106cde.pth | torchvision の学習済みモデル（ResNet-18） | **はっきりしない** |

- **face-parse-bisent は Google Drive だけの配布**（`zllrunning/face-parsing.PyTorch` の README が案内する Drive のファイル。MuseTalk の `download_weights.sh` は `gdown` で取る）。Drive には固定 URL（コミット固定）も SHA-256 も無い。**決定: Hugging Face の第三者のミラー `ManyOtherFunctions/face-parse-bisent` のコミット固定のファイルを使い、Drive の原本から取得したファイルと SHA-256 が一致すること（`468e13ca…6567`、53,289,463 バイト）を 2026-11-01 に確かめて `source_note` に書く**。同じ SHA-256 のコピーは、他に 4 つのリポジトリ（`gaga2210`・`n0x1103`・`vivym`・`leonelhs`）にもあった。ミラーが消えたら取得は `ErrSourceGone` で失敗し、メッセージが原本の場所を示す（そのときは別のコピーに差し替えて、SHA-256 の一致を確かめ直す）。`resnet18-5c106cde.pth`（`download.pytorch.org`）も同じミラーにあり、原本と SHA-256 が一致する。
- **S3FD（顔検出）の重みは MuseTalk が実行時に自分で取りに行く**（`musetalk/utils/face_detection/detection/sfd/sfd_detector.py`: 手元に `s3fd.pth` が無いと `https://www.adrianbulat.com/downloads/python-fan/s3fd-619a316812.pth` を取得）。`download_weights.sh` には無く、そのままだとオフラインで失敗するか、固定されない取得が起きる。`ByteDance/LatentSync` の `auxiliary/s3fd-619a316812.pth` が原本とバイトで同じ（SHA-256 `619a3168…3543`、ファイル名の接頭辞とも一致）ので、これを取得して `musetalk/utils/face_detection/detection/sfd/s3fd.pth` に置く。
- **`unclear`（商用利用がはっきりしない）の 4 件**: ①LatentSync の SyncNet — モデルカードは `license: openrail++`（用途の制限つきで商用可）だが、README は SyncNet を VoxCeleb2・HDTF で学習したとし、そのデータの商用での条件を確認できていない。②face-parse-bisent の `79999_iter.pth` — コードは MIT だが、重みは CelebAMask-HQ（**非商用の研究目的だけ**と README に明記）で学習したもので、重みのライセンスの表示が無い。③S3FD の重み — face-alignment のコードは BSD-3-Clause だが、重みの元（`yxlijun/S3FD.pytorch`）にライセンスの表示が無い。④torchvision の `resnet18`（ImageNet で学習。torchvision の README が「学習済みモデルは独自のライセンス・条件を持つことがある」と注意している）。MuseTalk 1.5 の重み自体は、モデルカードの `license: mit` と README の「学習済みモデルは商用を含むどんな目的にも使える」から `yes`。
- **`yes` と確かめたもの**: MuseTalk 1.5（MIT）・sd-vae-ft-mse（MIT。モデルカード）・whisper-tiny（Apache-2.0。モデルカード）・DWPose（Apache-2.0。モデルカード）。MuseTalk の `LICENSE.txt` も sd-vae-ft-mse・whisper・face-parsing.PyTorch のコードを MIT、DWPose を Apache-2.0、face-alignment を BSD-3-Clause としている。

### 12.8 未確認（#920）

| 項目 | 状態 |
|---|---|
| **実物の Windows ＋ NVIDIA でのパッケージ導入後の検証（約 2.0 GB の SHA-256 計算）・足りない重みの取得・起動** | **未確認**。偽の Hugging Face（`Range`・取り消し・ハッシュ不一致・404・空き容量不足）と、偽のパッケージ・偽のプロセスで、取得・再開・検証・拒否・起動の拒否を確認した。実物の重み（GPT-SoVITS の asset は約 2.0 GB、MuseTalk は約 6.0 GB）を `Range` で取得して導入する経路は、実機で走らせていない（SHA-256 とサイズは、Hugging Face の API の `lfs.oid`、ダウンロードしたファイル、公式の 7z の中身から確かめた） |
| 実物の `nltk_data.zip`・`G2PWModel.zip`・`open_jtalk_dic…tar.gz` からのメンバーの取り出し（`exeinstall.Extract`）と、取り出したファイルで GPT-SoVITS が動くこと | **未確認**（アーカイブの一覧とメンバーの SHA-256 は実物で確かめたが、取り出し→起動は実機で試していない。合成のアーカイブでは確認した） |
| MuseTalk の重みを置いた環境で MuseTalk が動くこと（`models/…` の配置が公式の既定と同じこと、S3FD の置き場所） | **未確認**（#921 のランタイムの実装で確かめる。配置は `download_weights.sh` と推論のコードから読んだ） |
| 各ライセンスの**原文との突き合わせ** | モデルカード・同梱の LICENSE・NLTK の配布一覧・MuseTalk の README / LICENSE.txt は読んだ。**法的な助言ではない**。`unclear` の 5 件（g2pW・SyncNet・face-parse・S3FD・resnet18）は、利用者が配布元に確認する前提（画面の注記） |
| 第三者のミラー（`ManyOtherFunctions/face-parse-bisent`・`XXXXRT/GPT-SoVITS-Pretrained`）が将来も残ること | 保証できない。消えたら `ErrSourceGone`（原本の場所つき）で失敗する |
