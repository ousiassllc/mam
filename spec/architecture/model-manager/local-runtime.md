# ローカルランタイム（GPT-SoVITS・MuseTalk）

[model-manager.md](model-manager.md)（同梱の ComfyUI エンジン）と同じ作りで、**声（GPT-SoVITS）と口パク（MuseTalk 1.5）をローカルで動かす実行環境を、MAM が自動で導入・起動・停止・状態表示まで管理する**ための基盤。親 Issue は #912（決定事項: 実行方式 B「ComfyUI の同梱エンジンと同じ感じで、mam が自動で入れて起動まで管理する」）。この文書は基盤と GPT-SoVITS の環境導入（Issue #918）、重み・依存ファイルの固定カタログと取得・検証・出典とライセンスの表示・画面と MCP（Issue #920。§12）、MuseTalk 1.5 の環境の組み立て・起動・口パクの入口（Issue #921。§13）を定める。実装は `backend/internal/modelmgr/runtime`（導入・判定・起動コマンド・重みの検証と取得）、`backend/internal/modelmgr/catalog`（カタログ）と `backend/internal/service/localruntime`（Service）。改訂履歴は [model-manager.changelog.md](model-manager.changelog.md)。

| 範囲 | Issue |
|---|---|
| ランタイム管理の基盤、GPT-SoVITS の環境の導入・起動・停止・状態 | **#918（この文書）** |
| 重み・依存ファイルの固定カタログ（固定 URL＋SHA-256、再開可）、出典とライセンスの画面表示、画面・MCP | **#920（§12）** |
| MuseTalk 1.5 のランタイム（環境の組み立て・起動・口パクの入口） | **#921（§13）** |
| MuseTalk の口パクプロバイダー（Audio Director）・プロバイダーの選択とフォールバック | #923・#924（この文書の範囲外） |

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

## 9. MuseTalk が載せる形（実装は §13）

`catalog/data/runtimes/musetalk.json` を足し、**重みと依存ファイルは #920 が `catalog/data/assets/musetalk.json` に固定済み**（§12.7）。同じ ID `musetalk` で結び付く。GPT-SoVITS と違って**公式の配布物が無い**ので、環境は mam が固定した部品から組み立てる（§13.1）。それに必要な拡張（`parts` の変種・`build`（型と検証は `catalog/parts`）・`token_env`・`max_compute_cap`）はコードに足した。ランタイムの種類がデータである方針は変えていない（MuseTalk の固有の部分は JSON と、mam が同梱する薄い HTTP ラッパー `catalog/data/overlays/mam_musetalk_*.py` だけ）。

## 10. 先送りしたこと（決めたことの記録）

- **ファインチューン（学習）はしない**。参照音声（約 5 秒）のゼロショットだけ（学習は VRAM と時間がかかり、macOS 非推奨）。
- **クラウドへのフォールバックは利用者が設定で選ぶ**。黙って切り替えない。設定の追加と Audio Director のプロバイダーは #912 の別の子。
- **Apple Silicon での自動導入**はしない（公式は MPS / CPU の推論に対応するが、パッケージと検証が別に要る）。オーナーの判断で別 Issue。
- 重みの取得・ライセンス表示・画面・MCP は #920 で実装した（§12）。MuseTalk のランタイムのパッケージと起動は #921（§13）。
- **MuseTalk の第三者の MLX 版**（`mlx-community/MuseTalk-1.5-q4`）の自動導入は、この Issue では実装しない（オーナーの判断が要る別の事項）。macOS は理由の文とクラウド（Sync Labs）の案内を返す（§13.5）。

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
| MuseTalk の重みを置いた環境で MuseTalk が動くこと（`models/…` の配置が公式の既定と同じこと、S3FD の置き場所） | **Linux の CPU で確認した**（#921。§13.7）: 12 件の重みをカタログの `dest` に置き、ネットワークの無い名前空間で、公式の推論と同じ手順（ラッパー）が 1.2 秒の動画を作った。**Windows ＋ CUDA は未確認** |
| 各ライセンスの**原文との突き合わせ** | モデルカード・同梱の LICENSE・NLTK の配布一覧・MuseTalk の README / LICENSE.txt は読んだ。**法的な助言ではない**。`unclear` の 5 件（g2pW・SyncNet・face-parse・S3FD・resnet18）は、利用者が配布元に確認する前提（画面の注記） |
| 第三者のミラー（`ManyOtherFunctions/face-parse-bisent`・`XXXXRT/GPT-SoVITS-Pretrained`）が将来も残ること | 保証できない。消えたら `ErrSourceGone`（原本の場所つき）で失敗する |

## 13. MuseTalk 1.5 のランタイム（Issue #921）

親 Issue #912 の決定事項 1（B）: MuseTalk 1.5 の実行環境（Python 環境とプロセス）を、mam が GPT-SoVITS・ComfyUI と同じように、自動で導入・起動・停止・状態表示する。**ComfyUI のカスタムノードは使わない**（古いものが多い。§1）。MuseTalk の公式の推論スクリプト（`scripts/inference.py`）と同じ手順を、mam が同梱する薄い HTTP ラッパーから 1 度だけ読み込んで呼ぶ。Windows x64 ＋ NVIDIA だけが自動導入の対象（§13.5）。重み（約 6 GB）は §12.7 のカタログで、環境の導入と同じジョブの最後に取得・検証する（§12.3）。

### 13.1 実行環境の作り方（決定と理由）

**決定**: 公式の手順（README の Installation: Python 3.10・PyTorch 2.0.1 + CUDA 11.8・`mmcv==2.0.1`・`mmdet==3.1.0`・`mmpose==1.1.0` ほか）と**同じ版**を、公式の配布先から**ファイルごとに固定の URL・SHA-256・サイズ**で取得し、**mam 自身が展開して組み立てる**。`pip`・`setup.py`・任意のコードは実行しない（実行時にどこにも取りに行かない）。部品は 84 個（`runtimes/musetalk.json` の `variants.nvidia.parts`）:

| 部品 | 数 | 取得元（固定） | 置き場所（`env\` の下） |
|---|---|---|---|
| Python 3.10.11 の Windows 用の組み込み版 | 1 | `https://www.python.org/ftp/python/3.10.11/python-3.10.11-embed-amd64.zip`（SHA-256 は python.org の sigstore の署名が指すダイジェストと一致） | `python\` |
| MuseTalk のソース | 1 | `https://github.com/TMElyralab/MuseTalk/archive/0a89dec45a0192b824e3cf4daf96c239440c5ed8.zip`（コミット固定。`github.com` は `codeload.github.com` へ 302） | `env\` の直下（先頭の `MuseTalk-<コミット>/` を外す） |
| PyTorch 2.0.1+cu118・torchvision 0.15.2+cu118 | 2 | `https://download.pytorch.org/whl/cu118/…`（SHA-256 は PyTorch の公式の索引の `#sha256=`） | `python\Lib\site-packages\` |
| mmcv 2.0.1（torch 2.0.0 + cu118 向けの事前ビルド） | 1 | `https://download.openmmlab.com/mmcv/dist/cu118/torch2.0.0/mmcv-2.0.1-cp310-cp310-win_amd64.whl`（OpenMMLab の公式の索引にある。`mim install` が使うものと同じ） | 同上 |
| その他の Python パッケージ（mmengine・mmdet・mmpose・transformers・diffusers・numpy・opencv-python・librosa ほか） | 79 | PyPI（`files.pythonhosted.org`）。SHA-256・サイズは PyPI の JSON API（`digests.sha256`・`size`） | 同上 |

- **閉包の決め方**: Linux（Python 3.10）に同じ版の環境を作り（`torch==2.0.1`・`mmcv==2.0.1` ほかを公式の README のとおりに入れ、`mmpose` は `chumpy`（sdist のみで Windows でビルドが壊れる。使われない）を避けて `--no-deps`）、**実際の推論が import するものだけ**を、足りないものを足しながら決めた（`xtcocotools`・`pycocotools`・`json_tricks`・`munkres`・`shapely`・`terminaltables`・`setuptools==69.5.1`（`pkg_resources`）が足りなかった）。`pip freeze` の各版について、**Windows cp310 の wheel が PyPI にあること**を確かめて固定した（`tokenizers` は `cp310-none-win_amd64`、`opencv-python` は `cp37-abi3-win_amd64`、`colorama` は Windows だけ必要）。sdist しか無いものは閉包に残っていない。
- **公式の `requirements.txt` から外したもの**: `tensorflow`・`tensorboard`（学習）、`gradio`（`app.py`）、`gdown`（重みの取得。mam は §12 で取得）、`imageio`・`moviepy`・`requests`（推論の経路が import しない）、`openmim`（mam が wheel を直接取る）。推論の経路は、これらが無い環境で動くことを確かめた（§13.7）。
- **wheel の展開**（`extract.Wheel`）: pip の代わりに mam が zip として展開する。パッケージのファイルと `*.dist-info`（`importlib.metadata` が読む。transformers が版を調べる）を `site-packages` に置き、`*.data/purelib`・`platlib` は `site-packages` の直下へ、`*.data/scripts`・`headers`・`data` は**展開しない**（`fonttools`・`numba`・`sympy` だけが持つ。スクリプトの実行ファイルは使わない）。84 個の全部で、`..`・絶対パス・ドライブ・リンク・同じパスを 2 つの部品が書くことが無いこと（`extract.ErrExists` で拒否する）を確かめた。
- **展開の上限**: 各部品の `extracted_size`（実物を測った値）。それより大きいアーカイブ（展開爆弾・違うファイル）は 1 バイトも書く前に断る（`extract.ErrTooLarge`）。部品の `size`・`extracted_size` の合計が変種の値（2,887,125,530 B と 5,739,113,930 B）と一致することを検証する。
- **組み込み Python の経路**: `python\python310._pth`（`build.files` の `lines`）を、`python310.zip`・`.`・`Lib/site-packages`・`..`（= `env\`。`musetalk` パッケージとラッパー）・`import site` に**置き換える**。組み込み版は環境変数を無視して、このファイルだけで `sys.path` を決める。
- **ラッパー**: `catalog/data/overlays/mam_musetalk_{server,engine,common}.py`（mam に `embed` で同梱。ソースの `env\` 直下に書く）。部品の後に書くので、部品が同じ名前のファイルを持っていても mam のものになる。

| 案 | 判断 |
|---|---|
| **固定した部品を mam が展開して組み立てる（採用）** | 取得物はすべてファイル単位の SHA-256 とサイズで検証され、実行時の `pip` の依存解決・sdist のビルド・任意のコード実行が無い。再現できる。Windows の `mmcv` は、OpenMMLab の公式の事前ビルドを使うのでコンパイラがいらない |
| `pip install --require-hashes -r …`（固定の要件ファイル） | 取らない。`pip` を組み込み Python で動かす手間（get-pip・ビルドツール）と、sdist に落ちたときのビルドの失敗が残る。許可ホストも同じだけ広がる |
| 展開済みの環境の 7z を HF に置いて配る | 取らない。mam が再配布することになる（オーナーの決定: 重みも環境も同梱・再配布しない）。Linux の開発環境で Windows の環境を作って検証できない |
| Conda・uv で解決する | 取らない。実行時にネットワークと解決（コード）が要る |

**GitHub のアーカイブのハッシュ**: `git archive` の zip は、GitHub の実装が変わると同じコミットでもバイト列が変わりうる（過去に一度起きた）。変わったときは SHA-256 の不一致で**導入が失敗する**（黙って別のものを使わない。「ダウンロードしたファイルの SHA-256 が一致しません」）。そのときはカタログの SHA-256 とサイズを更新する（コミットは同じなので中身は同じ）。未確認の保証として §13.8 に書く。

### 13.2 取得元の許可ホストの変更（`fetch.PackageURL`）

重みと ComfyUI のエンジンは、これまでどおり `fetch.AllowedURL`（`huggingface.co`・`hf.co`・`github.com`・`githubusercontent.com`）だけ。**環境の部品の取得だけ**は `fetch.PackageURL`（上に `python.org`・`pythonhosted.org`・`pytorch.org`・`openmmlab.com` を足す。サブドメインを含む。`download-r2.pytorch.org` は PyTorch の CDN）を使う。理由: Windows の PyTorch（CUDA）・mmcv の wheel は Hugging Face・GitHub に無く、PyPI・PyTorch・OpenMMLab の公式の配布先にしか無い。**ホストへの信頼ではなくハッシュで固定する**ので、許可の広がりは影響が小さい: ① 全部品が SHA-256 とサイズの固定つき（不一致は `.part` を捨てて失敗）、② `parts.URLAllowed` の URL パターン（`files.pythonhosted.org/packages/…`・`download.pytorch.org/whl/cu<N>/…whl`・`download.openmmlab.com/mmcv/dist/cu<N>/torch<N>/…whl`・python.org の組み込み版・GitHub の 40 桁のコミットの zip）に合わない URL はカタログの検証が拒否する（ドメインより狭い）、③ `safefetch`（ローカルのネットワークのアドレスに繋がない）とリダイレクトごとの再検査はそのまま。リダイレクト先は、`github.com` が `codeload.github.com`（許可済み）へ 302 するだけで、PyPI・PyTorch・OpenMMLab・python.org は直接配信（`Accept-Ranges: bytes` と `Range` の 206 を確かめた）。`Range` の再開・SHA-256・サイズの検証は §5.1・§12.3 と同じ `fetch.Download`。

### 13.3 プロセスの形と口パクの入口

**形**: MuseTalk には常駐のサーバーが無いので、**mam が同梱する薄い HTTP ラッパー**（`mam_musetalk_server.py`。標準ライブラリの `http.server` だけ）を、MuseTalk の Python モジュールと同じプロセスで動かし、**モデルを 1 度だけ読み込む**（案 (a)。(b) の 1 クリップごとの子プロセスは、毎回 Whisper・VAE・UNet・DWPose・S3FD を読み込み直すので、数十秒の無駄が毎回かかる）。待ち受けは `127.0.0.1` だけ（ラッパーが他のホストを拒否し、カタログの検証も `0.0.0.0` を拒否する）。**ポートが開くのはモデルの読み込みの後**なので、`GET /health` が 200 なら使える。起動・停止・状態は §5.2 と同じ（`stopped` / `starting` / `running` / `failed`。起動の失敗はログ末尾つきの `failed`、予期しない終了も `failed`）。

**起動**: `env\python\python.exe -I -u -X utf8 mam_musetalk_server.py --host 127.0.0.1 --port <空きポート> --use-float16`（作業ディレクトリ `env\`。MuseTalk の相対パス `./models/…`・`./musetalk/utils/dwpose/…` がそこで解決される）。fp16 は、公式が 4 GB の GPU で動かした設定（§13.5）。環境は §5.2 と同じ（オフライン設定・キャッシュと一時ファイルは `localai\musetalk` の中・`PATH` の先頭に `env\python`）。

**認証**: 起動のたびに mam が**ランダムなトークン**（24 バイト）を作り、環境変数 `MAM_RUNTIME_TOKEN`（カタログの `launch.token_env`）で子に渡す。サーバーは**生成の要求**にヘッダー `X-Mam-Token` を要求する（Web ページは送れない。再起動で古いトークンは無効）。`Host` が `127.0.0.1:<port>`・`localhost:<port>` でない要求は全部拒否する（DNS リバインディング）。`/health` はトークンなし（mam の起動確認がトークンを持たず、何も書かない）。トークンはログにも画面にも出さない。

**入口**（`modelmgr/lipsync`。呼び出し側は `localruntime.Service.LipSync(ctx, lipsync.Request)`。#923 の Audio Director の口パクプロバイダーが呼ぶ）:

| | 内容 |
|---|---|
| 要求 `lipsync.Request` | `video_path`（動画 mp4・mov・avi・mkv・flv か、顔が 1 つ写った画像 jpg・png・bmp・tif）・`audio_path`（ffmpeg が読める音声）・`output_path`（`.mp4`。入力と別）・`ffmpeg_path`（mam が探した ffmpeg。[../ffmpeg-lookup.md](../ffmpeg-lookup.md) の結果を呼び出し側が渡す。ランタイムは ffmpeg を持たない）・`fps`（画像のときだけ。省略で 25）。パスは**全部絶対パスでこの PC のもの**（動画のバイト列を HTTP で送らない）。`Request.Check()` が、サーバーと同じ規則（絶対パス・`.mp4`・出力が入力でない・fps 1〜60）を、ランタイムを起動する前に確かめる |
| 応答 `lipsync.Response` | `output_path`・`frames`・`fps`・`duration_sec`（出力の長さ）・`elapsed_sec`（生成にかかった時間）。出力は H.264（yuv420p・crf 18）＋ AAC の mp4 で、音声の長さに揃える |
| エラー `*lipsync.Error` | `code`（下表）・`status`（HTTP）・`message`（利用者向けの日本語の文。そのまま表示してよい）。`errors.Is` は `lipsync.Err…` に一致する |
| 起動前の拒否（`LipSync` が生成の前に返す） | `*SupportError`（macOS・非 NVIDIA・VRAM 不足・RTX 50。理由＋ Sync Labs の案内）・`ErrNotInstalled`・`ErrBusy`（導入中）・`*WeightsError`（重みが不足）。どれもサーバーを起動しない |
| 取りこぼし | サーバーが途中で止まったら `ErrUnreachable`（「…のサーバーが応答しません（途中で止まった可能性があります。ログを確かめて…）」）。呼び出し側の `ctx` の取り消しは待ちを終えるだけ（サーバーは始めた生成を続ける） |

| `code` | HTTP | 意味 | `errors.Is` |
|---|---|---|---|
| `bad_request` | 400 | フィールドが無い・正しくない、動画を読めない | `ErrBadRequest` |
| `forbidden` | 403 | トークンか Host が違う | `ErrForbidden` |
| `input_not_found` | 404 | 動画・音声・ffmpeg のファイルが無い | `ErrInputNotFound` |
| `busy` | 409 | 別の生成が動いている（サーバー側の保険。`Service.LipSync` は自分で 1 件ずつ順番を待つ） | `ErrBusy` |
| `too_large` | 413 | 動画のフレームがメモリ（6 GB）に収まらない | `ErrTooLarge` |
| `no_face` | 422 | 動画から顔を見つけられない | `ErrNoFace` |
| `ffmpeg_failed` | 422 | ffmpeg が入力を読めない・出力を書けない（末尾 3 行を含む） | `ErrFFmpegFailed` |
| `out_of_memory` | 503 | GPU のビデオメモリが足りない（`torch.cuda.OutOfMemoryError`。キャッシュを解放してサーバーは動き続ける） | `ErrOutOfMemory` |
| `internal` | 500 | それ以外（例外の種類とメッセージつき。未知のコード・本文が JSON でない応答も、これに落とす） | `ErrInternal` |

**同時実行**: GPU は 1 つなので、1 つのサーバーにつき生成は 1 件ずつ。`Service.LipSync` は 1 件ずつ順番に実行し（待っている呼び出しは自分の `ctx` で諦められる）、サーバーも `busy` で 2 件目を断る。

**生成の手順**（`mam_musetalk_engine.py`。`scripts/inference.py`（MIT）の v1.5 の既定値どおり）: ffmpeg で動画をフレームの PNG に分解 → 音声を 16 kHz のモノラルに変換 → Whisper の特徴 → DWPose＋S3FD で顔の位置を取り、顔が写らないフレームは外す（全部で 0 なら `no_face`）→ VAE で潜在変数 → UNet でバッチ（8）ごとに口元を生成 → 顔のパース（`jaw` モード・頬幅 90）で元のフレームに貼り戻す → ffmpeg で音声つきの mp4 にする。作業ファイルは一時ディレクトリに作り、成功も失敗も消す。

### 13.4 重みの結び付けと face-parse-bisent の方針

重みと依存ファイルは §12.7 のカタログ（12 件、すべて必須）。`dest` は MuseTalk の既定のパスで、環境ディレクトリ（= MuseTalk のリポジトリの根）の下: `models/musetalkV15/{unet.pth,musetalk.json}`・`models/sd-vae/…`・`models/whisper/…`・`models/dwpose/dw-ll_ucoco_384.pth`・`models/syncnet/latentsync_syncnet.pt`・`models/face-parse-bisent/{79999_iter.pth,resnet18-5c106cde.pth}`・`musetalk/utils/face_detection/detection/sfd/s3fd.pth`。MuseTalk 1.0 の重み（`models/musetalk/…`）は **v1.5 の推論が読まない**（Linux で、それが無い環境で動くことを確かめた）ので入れない。SyncNet（`latentsync_syncnet.pt`）は、公式の `download_weights` が取得し、推論のコード（`scripts/inference.py` から import される範囲）は読まない（学習の `musetalk/models/syncnet.py` だけが使う）が、オーナーの決定事項 3 が挙げているので、必須のまま検証・取得する（約 1.5 GB）。

**face-parse-bisent（オーナー指定: 方針を spec と PR 本文に書く）**: 原本は `zllrunning/face-parsing.PyTorch` の README が案内する Google Drive（`https://drive.google.com/open?id=154JgKpzCPW82qINcVieuPH3fZ2e0P812`）のファイルだけで、固定 URL も SHA-256 も無い。**方針: Hugging Face の第三者のミラー `ManyOtherFunctions/face-parse-bisent` のコミット固定（`0073b233a5a3c4b1377d4dbf49245017938a72b5`）のファイルを固定 URL として使い、SHA-256 は原本のファイルのものと一致することを確かめて固定する。mam 自身は再配布しない**（`source_note` に原本の場所と確かめた日付を書く。§12.1）。ミラーが消えたら `ErrSourceGone`（原本の場所つきのメッセージ）で失敗し、別の同一ハッシュのコピー（`gaga2210`・`n0x1103`・`vivym`・`leonelhs`）に URL を差し替えて、SHA-256 の一致を確かめ直す。

| ファイル | 原本から取得して計算した SHA-256（2026-11-01） | サイズ | ミラー（カタログ）の SHA-256 | 一致 |
|---|---|---|---|---|
| `79999_iter.pth` | Google Drive の `uc?export=download&id=154JgKpzCPW82qINcVieuPH3fZ2e0P812`（応答のファイル名 `79999_iter.pth`）から取得: `468e13ca13a9b43cc0881a9f99083a430e9c0a38abd935431d1c28ee94b26567` | 53,289,463 | `468e13ca…6567` | 一致 |
| `resnet18-5c106cde.pth` | `https://download.pytorch.org/models/resnet18-5c106cde.pth` から取得: `5c106cde386e87d4033832f2996f5493238eda96ccf559d1d62760c4de0613f8`（torchvision の慣習の、ファイル名の `5c106cde` が SHA-256 の先頭 8 桁と一致） | 46,827,520 | `5c106cde…13f8` | 一致 |

`resnet18` の原本（`download.pytorch.org`）は、**環境の部品の許可ホスト（§13.2）には入るが、重みの取得には使わない**: 重みのカタログの検証は Hugging Face のコミット固定の URL だけを許す（§12.2。重みの許可ホストは増やさない）ので、同じバイトの Hugging Face のミラーを固定 URL にしている。

### 13.5 対応環境の判定（MuseTalk）

判定は §4 と同じ純関数。**Windows x64 ＋ NVIDIA の専用メモリ 4 GB 以上**、**RTX 50 シリーズ（Compute Capability 12.0 以上）は断る**（新しい `Support.Code = compute_cap`。カタログの `support.max_compute_cap` と `compute_cap_reason`）。理由: 環境は公式の PyTorch 2.0.1（CUDA 11.8）で、その wheel の対応する GPU の世代に Blackwell（sm_120）が無い（`torch.cuda.get_arch_list()` は sm_90 まで）ので、導入しても GPU にモデルを載せる時点で失敗する。GPT-SoVITS のような RTX 50 向けの別の変種は、公式の MuseTalk に無い（`mmcv==2.0.1` が PyTorch 2.0 向けのため）。断りの文には必ずクラウドの案内がつく: **「代わりに、クラウドの口パク（Sync Labs）を使ってください。」**（理由の文は画面・MCP・Audio Director が共有する）。

| 条件 | `Code` | 理由（カタログの文の要旨） |
|---|---|---|
| macOS | `os` | 「macOS では MuseTalk 1.5 を自動導入できません。**公式の PyTorch 版は macOS（Apple Silicon）の GPU に対応しておらず、第三者の MLX 版（mlx-community/MuseTalk-1.5-q4）は mam の自動導入の対象にしていません。**mam が自動で導入するのは Windows + NVIDIA の GPU の組み合わせだけです」 |
| Linux・その他の OS | `os` | 自動導入に対応していない |
| 64 ビット x86 でない | `arch` | 同上 |
| GPU を取得できない・NVIDIA がない（AMD・Intel） | `no_gpu`・`not_nvidia` | 同上（§4） |
| NVIDIA の最大の専用メモリ < 4 GB | `low_vram` | 「{gpu} のビデオメモリは {vram_gb} GB で、MuseTalk 1.5 に必要な 4 GB に足りない」 |
| Compute Capability ≥ 12.0（RTX 50） | `compute_cap` | 「{gpu} は NVIDIA の RTX 50 シリーズ（Blackwell）で、MuseTalk の公式の手順が使う PyTorch 2.0.1（CUDA 11.8）が対応していない」 |

**VRAM の下限 4 GB は、公式の README の記載に基づく**（Gradio Demo の節: 「Windows の NVIDIA GeForce RTX 3050 Ti Laptop GPU、4GB VRAM で、fp16 で 8 秒の動画を約 5 分で生成」）。`vram.estimate=false`（出典あり）。ただし **mam 自身の実機での実測は無い**（§13.8）。出力の解像度・長さによっては 4 GB で足りず、そのときは `out_of_memory` でわかりやすい文を返す。**MLX 版の自動導入は実装しない**（オーナーの判断が要る別の事項）。**クラウドへの自動の切り替えはしない**（§10。利用者が設定で選ぶ。#924）。

### 13.6 ライセンスの記録

MuseTalk 本体（コード MIT・学習済みモデルは商用を含むどんな目的にも使える）は、README の原文（「The code of MuseTalk is released under the MIT License. There is no limitation for both academic and commercial usage.」「The trained model are available for any purpose, even commercially.」「Other open-source models used must comply with their license, such as whisper, ft-mse-vae, dwpose, S3FD, etc..」）を `license.summary` に引いた（コミット `0a89dec4` の README を 2026-11-01 に確認）。重みと依存ファイルの出典・ライセンス・商用利用の可否は §12.7（`unclear` の 4 件を画面で目立たせる。#920 の画面がそのまま MuseTalk のカードに出す）。環境の部品の Python パッケージ 82 個のライセンスは、各 wheel のメタデータの表記を `THIRD_PARTY_NOTICES.md`（「MuseTalk 1.5（利用者が取得する別プログラム）」）に一覧にした。**原文との突き合わせはしていない**（未確認。`torch` の CUDA 版の wheel が含む NVIDIA の DLL の条件、`opencv-python`・`soundfile`・`soxr` が含む LGPL のライブラリを明記）。mam はどれも同梱せず、再配布しない。

### 13.7 テスト

- 部品の組み立て・展開・拒否: `modelmgr/runtime/assemble_test.go`（偽の取得元に 4 つの部品（Python・ソース・wheel 2 つ）。展開の配置・`.data` の扱い・`._pth` の置き換え・SHA-256 不一致・展開爆弾（`ErrTooLarge`）・再開（ダウンロード済みの部品を取り直さない）・失敗しても既存の環境が残る）、`extract/zip_test.go`（`..`・絶対パス・ドライブ・上書き・取り消し）、`fetch/auth_test.go`（`PackageURL`）。
- カタログ: `catalog/musetalktest/musetalk_test.go`・`catalog/parts/parts_test.go`（固定した URL・SHA-256・合計・URL パターン・検証の拒否 16 通り）。
- 対応環境: `modelmgr/runtime/musetalk_support_test.go`（macOS・Linux・AMD・GPU なし・2 GB・RTX 50 を断り、Sync Labs の案内が付く）。
- 起動・停止・状態・口パクの入口: `service/localruntime/musetalktest`（偽のホスト・**偽のプロセス＝テストバイナリ自身**が、ラッパーと同じ契約で応答する）。導入（部品＋重み）・遅延起動・トークンが通る/他のトークンは 403・`no_face`・`out_of_memory` の中継・起動前の拒否（未導入・重みが不足・macOS・RTX 50・VRAM 不足・不正な要求）・1 件ずつの実行（待つ呼び出しが自分の `ctx` で諦める）。入口の型: `modelmgr/lipsync`（偽のサーバー: 要求・トークン・全コードの対応・未知のコード・応答が壊れている・サーバーが無い・取り消し）。
- **Python 側の契約**: `catalog/musetalktest/testdata/test_musetalk_server.py` を Go のテスト `TestMuseTalkServerHonoursTheClientContract` が実行する（偽のエンジン。トークン・Host・不正な要求 13 通り・各コードの HTTP ステータス・1 件ずつ・ffmpeg の失敗。Python 3 が無い環境ではスキップ）。
- **実物の確認（Linux の CPU）**: 開発機で、公式の手順と同じ版の環境（PyTorch 2.0.1 の CPU 版・mmcv 2.0.1・mmdet 3.1.0・mmpose 1.1.0 ほか）に、**カタログの 12 件の重みを `dest` の位置に置き**（SHA-256・サイズはカタログと一致することを取得時に確かめた）、**ネットワークの無い名前空間（`unshare -rn`。ループバックだけ）**で、MuseTalk のソース（コミット `0a89dec4`）と `mam_musetalk_server.py` を起動し、`/health` が 200 になってから `POST /v1/lipsync` に 1 秒の動画（576×768・25 fps）と 1.2 秒の音声を送った。**1.2 秒・30 フレーム・H.264＋AAC の mp4 が返った**（約 95 秒。CPU）。ネットワークの無い環境で動いたので、推論の経路が実行時にどこにも取りに行かないことの確認にもなる（S3FD・ResNet-18・Whisper・VAE・DWPose はすべて `models/…` から読んだ）。84 個の部品は、実物を取得して SHA-256 が固定した値と一致すること、wheel の中のパスが安全なことを確かめ、実物の部品を `Installer.Install` に通して `env\` に組み立てた。

### 13.8 未確認

| 項目 | 状態 |
|---|---|
| **実物の Windows ＋ NVIDIA での導入（部品 2.9 GB の取得と約 5.7 GB の展開）・起動・口パクの生成** | **未確認**。この開発環境は Linux で、Windows・NVIDIA の GPU が無い。上の §13.7 の Linux の CPU の確認と、Go 側の偽のホスト・偽のプロセスのテストだけ。**実際の口パクの生成の品質・速度も未確認**（日本語の音声での出来は、親 Issue のメモのとおり実機で聞き比べる） |
| 組み込み Python（`python310._pth`）で、`-I` ＋ 作業ディレクトリ `env\` ＋ `site-packages` の `.pth`（setuptools の `distutils-precedence.pth`）が期待どおりに効くこと、`import torch` が CUDA の DLL を見つけること（Visual C++ 再頒布可能パッケージが要る） | **未確認**（組み込み版の ZIP の中身と `._pth` の書式は実物を確かめた。Windows で実行していない。Python のドキュメントの「Embeddable package」の仕様どおりに書いた） |
| `<exe のディレクトリ>\localai\musetalk\env\python\Lib\site-packages\…` の深いパスが、Windows の 260 文字の上限に当たること（Python の import は長いパス名に弱い） | **未確認**。mam.exe を浅いフォルダに置けば避けられる（GPT-SoVITS・ComfyUI も同じ。§11） |
| ラッパーが、顔が写らないフレームを外した動画に音声を重ねる（公式の `scripts/inference.py` は、そのフレームの出力を飛ばして、動画がそこで終わる） | 実装の判断（§13.3）。顔が全フレームで写る動画だけ、Linux で確かめた |
| fp16（`--use-float16`）・CUDA での動作、VRAM 4 GB での動作（公式の README の記載があるだけ） | **未確認**（Linux の確認は fp32・CPU） |
| Windows の cp310 wheel を Linux の同じ版で代用して決めた閉包が、Windows でも完全であること | **未確認**。Python のコードはプラットフォームに依存しない前提（`colorama` を足した）。`import` が足りなければ、起動の失敗がログ末尾つきの `failed` で出る |
| GitHub の zip アーカイブのバイト列が将来も同じであること（§13.1）、PyPI・PyTorch・OpenMMLab・python.org の URL が将来も残ること | 保証できない。変わったら導入は SHA-256 の不一致・404 で失敗する（`ErrSourceGone` と同様に、旧い URL を差し替える更新が要る） |
| 82 個の Python パッケージ・Python 本体・NVIDIA の DLL のライセンスの原文との突き合わせ | **未確認**（`THIRD_PARTY_NOTICES.md`） |
| `ffmpeg` の libx264・aac を持つビルド（mam が導入する ffmpeg は GPL ビルドで持つが、利用者が設定で指す ffmpeg は未確認） | 出力の書き込みに失敗したら `ffmpeg_failed`（ffmpeg の末尾のメッセージつき） |
