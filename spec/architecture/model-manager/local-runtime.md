# ローカルランタイム（GPT-SoVITS・Irodori-TTS・MuseTalk・llama.cpp・Ollama）

[model-manager.md](model-manager.md)（同梱の ComfyUI エンジン）と同じ作りで、**声（GPT-SoVITS）と口パク（MuseTalk 1.5）をローカルで動かす実行環境を、MAM が自動で導入・起動・停止・状態表示まで管理する**ための基盤。親 Issue は #912（決定事項: 実行方式 B「ComfyUI の同梱エンジンと同じ感じで、mam が自動で入れて起動まで管理する」）。この文書は基盤と GPT-SoVITS の環境導入（Issue #918）、重み・依存ファイルの固定カタログと取得・検証・出典とライセンスの表示・画面と MCP（Issue #920。§12）、MuseTalk 1.5 の環境の組み立て・起動・口パクの入口（Issue #921。§13）を定める。実装は `backend/internal/modelmgr/runtime`（導入・判定・起動コマンド・重みの検証と取得）、`backend/internal/modelmgr/catalog`（カタログ）と `backend/internal/service/localruntime`（Service）。改訂履歴は [model-manager.changelog.md](model-manager.changelog.md)。

| 範囲 | Issue |
|---|---|
| ランタイム管理の基盤、GPT-SoVITS の環境の導入・起動・停止・状態 | **#918（この文書）** |
| 重み・依存ファイルの固定カタログ（固定 URL＋SHA-256、再開可）、出典とライセンスの画面表示、画面・MCP | **#920（§12）** |
| 固定した URL と SHA-256 の上流との定期照合（週 1 回の CI ジョブ） | **#937（§12.10）** |
| MuseTalk 1.5 のランタイム（環境の組み立て・起動・口パクの入口） | **#921（§13）** |
| Irodori-TTS のランタイム（環境の組み立て・起動）、モデルの選択、Gemma Terms の同意 | **#1015（§14）** |
| llama.cpp のランタイム（ローカル LLM。GGUF の重み 6 系統 8 本の選択、同梱ランタイムとしての起動、プロバイダー `local` への結びつき） | **#1008（後半。§17）** |
| Ollama のランタイムと、Ollama のモデルの取得（`ollama pull`。厳選 3 つ。認証なし・クラウド機能は無効。状態: 仕様のみ・未実装） | **#1105（§19）** |
| Irodori-TTS の声のプロバイダー（Audio Director。参照音声・キャプション・絵文字・GPU の扱い） | **#1015**（[model-adapters-m3.md §14.11・§14.12](../model-adapters-m3.md)。この文書の範囲外） |
| MuseTalk の口パクプロバイダー（Audio Director。実行ごとの選択の入口まで） | **#923**（[model-adapters-m3.md §14.9](../model-adapters-m3.md)。この文書の範囲外） |
| プロバイダーの選択の保存・クラウドへのフォールバック・利用者向けの文言 | #924（この文書の範囲外） |

## 1. 方針（変えない前提）

- **ComfyUI を経由せず、ランタイムごとに独立した Python 環境とプロセスを持つ**。GPT-SoVITS は公式の `api_v2.py`（FastAPI）を、mam が同梱する起動用スクリプト（トークンと Host の確認を足す。§5.2）経由で、MAM が `127.0.0.1` 限定で起動して HTTP で呼ぶ。ComfyUI のカスタムノード経由にしない（親 Issue「ComfyUI のカスタムノードは古いものが多い」。決定事項 1 で、MAM が管理する独立の実行環境・プロセスに決まった）。
- **ランタイムの種類はデータ**（`catalog/data/runtimes/<名前>.json`）。コードは GPT-SoVITS を知らない。MuseTalk は同じ形の JSON を足し、起動の引数・確認パス・対応環境の判定の文を書くだけで載る（§9）。
- **MAM は何も同梱しない**。公式の配布先から取得する別プログラムで、`THIRD_PARTY_NOTICES.md`（GPT-SoVITS の節）に載せる。ライセンスと商用利用の可否の表示は §12（#920）。
- **置き場所は `<mam.exe のあるディレクトリ>\localai\<ランタイム ID>\` だけ**（§2）。ライブラリのキャッシュと一時ファイルもその中に置く。インストール版はアンインストーラーが消し、ポータブル版は exe の隣に残る。書けない場所なら `store.ErrNotWritable` で断る。
- **取得物は固定して SHA-256 を検証する**（model-manager.md §1 と同じ）。ハッシュ・サイズが合わないものは使わず消す。取得は `fetch`（`Range` 再開・空き容量確認）と `extract` をそのまま使う。
- **対応は Windows x64 ＋ NVIDIA GPU だけ**（ComfyUI の同梱エンジンと同じ範囲）。それ以外は導入も起動もさせず、**理由の文とクラウド（ElevenLabs）を使う案内**を返す（§4）。macOS（Apple Silicon）の MPS / CPU 対応は §16 で調べた（確かな一次情報だけでは組み立てられないので、自動導入しない。ランタイムごとの理由を出す）。
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
| 1 | OS が `windows` でない | `os` | OS ごとの文（`darwin`: 「macOS では GPT-SoVITS の自動導入に対応していません。公式は macOS の MPS / CPU での推論に対応していますが、その導入手順は…（pyopenjtalk・jieba_fast・jieba が揃わない理由。#935 で具体化。§16.1）」、`linux`、その他） |
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
3. 7z を `env.new\` へ展開し、続けて**カタログの `extra_files`（mam が同梱する起動用スクリプト `mam_gptsovits_server.py`。`catalog/data/overlays/`）を `env.new\` に書き**、`runtime\python.exe` と入口のスクリプト（`mam_gptsovits_server.py`）があることを確かめて `env\` と入れ替える（既存は `env.old\` に退避して成功で消す。失敗で戻す）。導入のたびに環境を作り直すので、更新・再導入でもこのファイルは今の mam のものになる。さらに**起動のたび**（`Installer.Command`）に、同じファイルを `env\` へ書き直す（起動用スクリプトは守り（トークン・Host・許可する要求）そのものなので、mam の修正が、パッケージの再取得なしで次の起動から効く）。`extra_files` は `build.files` と同じ形（`dest`＋`overlay` か `lines`。検証は `parts.CheckFiles`）で、組み立てるランタイム（`build`）とは併用しない。`Status` は、導入済みと記録されていても `extra_files` のファイルが無い環境（このスクリプトが入る前に導入したもの）を `broken`（導入し直す）にする。
4. `state.json` に版・変種・時刻を記録し、アーカイブと `downloads\` を消す。

進捗は展開前の取得 70 %・展開 30 %。実行中のプロセスは、ファイルを入れ替える前に止める。失敗は日本語にして出す（SHA-256 不一致・大きさ不一致・空き容量・取得元の 4xx・書き込めない場所）。

### 5.2 起動（`runtime.Installer.Command`、`proc.Manager`）

| 項目 | 内容 |
|---|---|
| コマンド | `env\runtime\python.exe -I -u -X utf8 mam_gptsovits_server.py -a 127.0.0.1 -p <空きポート> -c GPT_SoVITS/configs/tts_infer.yaml`（作業ディレクトリは `env\`）。**公式の `api_v2.py` を直接は起動しない**: mam の起動用スクリプト（`catalog/data/overlays/mam_gptsovits_server.py`）が、`-I` で外れる `sys.path`（作業ディレクトリと `GPT_SoVITS`）を `api_v2.py` と同じに足して `api_v2` を import し（引数 `-a`・`-p`・`-c` は `api_v2.py` がそのまま読む。モデルを読み込んでからポートが開く）、その FastAPI アプリ（`api_v2.APP`）の前に下の守りを置いてから、`api_v2.py` の `__main__` と同じに `uvicorn.run(host, port, workers=1)` する。公式の `go-webui.bat`（`runtime\python.exe -I …`、`runtime` を `PATH` の先頭へ）と同じ作りで、`-I`（隔離モード）により利用者の Python・`PYTHONPATH` に影響されない |
| 待ち受け | **`127.0.0.1` だけ**（カタログの検証が、`127.0.0.1`・`{port}` を含み `0.0.0.0`・`::` を含まない引数だけを許す。起動用スクリプトも `-a` が `127.0.0.1` でなければ、モデルを読む前に終了コード 2 で断る）。ポートは前回のポートを優先し、空いていなければ空きポート（`proc.FreePort`） |
| 認証と Host の確認 | 公式の `api_v2.py` はトークンも Host の確認も持たず、`GET /control?command=exit\|restart`・`/set_gpt_weights`・`/set_sovits_weights`（任意のパスの重みを読む）・`/tts?ref_audio_path=` が、ブラウザの Web ページからの単純な GET（と DNS リバインディング）で呼べてしまう。そこで起動用スクリプトの ASGI ミドルウェアが、**全要求**に次を課す（順に判定）。①`Host` が `127.0.0.1:<port>`・`localhost:<port>`（大文字小文字は問わない・1 つだけ）でなければ **403**。②**許可する要求だけ**を通す: `POST /tts`（mam の声の呼び出しが使う唯一のもの。`agent/audiodirector/gptsovits`）と `GET /openapi.json`（起動確認）。それ以外（`/control`・`/set_gpt_weights`・`/set_sovits_weights`・`/set_refer_audio`・`GET /tts`・`/docs` ほか）は**トークンを持っていても 404**（トークンの持ち主でも、サーバーに任意のパスを読み込ませたり止めさせたりできない）。③`POST /tts` はヘッダー `X-Mam-Token` が環境変数 `MAM_RUNTIME_TOKEN`（カタログの `launch.token_env`）と一致すること（`hmac.compare_digest`。ヘッダーが 2 つなら不一致）。違えば **403**。`GET /openapi.json` はトークンなし（mam の起動確認がトークンを持たない。Host の確認は受ける）。トークンは起動のたびに mam が作る 24 バイトの乱数（§13.3 の MuseTalk と同じ `localruntime` の `withToken`/`currentToken`）で、`MAM_RUNTIME_TOKEN` が無い・16 文字未満のときは起動用スクリプトが起動しない（終了コード 2）。トークンはログにも画面にも出さない |
| 応答の待ち | `GET /openapi.json` が 200（FastAPI の既定。**モデルを読み込んだ後にポートが開く**ので、応答があれば使える）。最長 5 分（初回はモデルの読み込みに時間がかかる） |
| オフライン設定 | `HF_HUB_OFFLINE=1`・`TRANSFORMERS_OFFLINE=1`・`HF_DATASETS_OFFLINE=1`・`HF_HUB_DISABLE_TELEMETRY=1`・`DO_NOT_TRACK=1`・`GRADIO_ANALYTICS_ENABLED=False`（カタログの `launch.env`）。実行中にライブラリが重みを取りに行かない。必要な重みは取得済みのものだけを使う。ミラー（`modelscope`）も `MODELSCOPE_CACHE` をランタイムの `cache\` に向ける |
| 書き込み先 | `HF_HOME`・`TORCH_HOME`・`MODELSCOPE_CACHE`・`XDG_CACHE_HOME` は `cache\`、`TEMP`・`TMP`・`TMPDIR` は `tmp\`（どちらもランタイムのディレクトリの中）。`PATH` の先頭に `env\runtime`。`PYTHONPATH`・`PYTHONHOME`・`PYTHONSTARTUP`・`VIRTUAL_ENV` は子に渡さない |
| 子の管理 | `proc.Manager`（ComfyUI と共通）。コンソールを出さない（Windows の `CREATE_NO_WINDOW`）。Windows は Job Object（`KILL_ON_JOB_CLOSE`）、Linux は Pdeathsig とプロセスグループで、MAM の終了・異常終了で子が残らない。ランタイムの対応は Windows だけなので macOS では起動しないが、子の起動は ComfyUI と同じ `launch` で、macOS では見張りプロセスと次回起動の片付け（`runtime.pid`）が付く（[model-manager.md §3.4](model-manager.md)。実機で未確認） |
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
func (*Service) Token(id) (string, error)               // 今の起動のトークン（`token_env` の無いランタイムは ""）。呼び出し側の秘密で、`View` には載せない（§5.2）
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

- ~~**ファインチューン（学習）はしない**。参照音声（約 5 秒）のゼロショットだけ~~ → **Issue #936 で、Windows ＋ NVIDIA の GPU の PC だけで学習に対応した（§18）**。参照音声のゼロショットは、学習していないキャラクターの既定のまま残る。macOS では学習しない（ランタイムが対応しない。§16）。
- **クラウドへのフォールバックは利用者が設定で選ぶ**。黙って切り替えない。設定の追加と Audio Director のプロバイダーは #912 の別の子。
- **Apple Silicon での自動導入**はしない（公式は MPS / CPU の推論に対応するが、パッケージと検証が別に要る）。#935 で一次情報を調べ、確かな一次情報だけでは組み立てられないと判断した（§16）。
- 重みの取得・ライセンス表示・画面・MCP は #920 で実装した（§12）。MuseTalk のランタイムのパッケージと起動は #921（§13）。
- **MuseTalk の第三者の MLX 版**（`mlx-community/MuseTalk-1.5-q4`）の自動導入は、この Issue では実装しない。macOS は理由の文とクラウド（Sync Labs）の案内を返す（§13.5）。#935 で出典・ライセンス・できることを調べ、実装しないと判断した（§16.3）。

## 11. 未確認

| 項目 | 状態 |
|---|---|
| **実物の Windows ＋ NVIDIA での統合パッケージの導入（約 8 GB の取得と約 14 GB の展開）と、`mam_gptsovits_server.py` 経由での `api_v2.py` の起動・応答** | **未確認**。合成 7z（`extract/testdata/bcj2.7z`）と偽の取得元・偽のプロセス（このテストバイナリ自身）で、取得・`Range` 再開・ハッシュ／サイズ不一致・空き容量・取り消し・展開・`extra_files` の書き込み・入れ替え・起動・停止・失敗の分岐を確認。起動用スクリプトの守り（Host・トークン・許可する要求）は、偽の `api_v2`・`uvicorn` の Python のテスト（`catalog/gptsovitstest/testdata/test_gptsovits_server.py`）で確認した（実物の `api_v2.py` と組み合わせては動かしていない）。実物の 7z は、ヘッダー（58,007 個のファイル、展開後 14,128,680,597 バイト）だけを `Range` で読み、`runtime\python.exe`・`api_v2.py`・`GPT_SoVITS\configs\tts_infer.yaml` があることを確かめた（本体は取っていない） |
| 14 GB・5.8 万ファイルの 7z を `bodgit/sevenzip` で展開するときのメモリ・時間・Windows の長いパス | **未確認**（ComfyUI の 4.4 GB のものとは桁が違う。ヘッダーの読み込みは 166 回の `Range` で済んだ） |
| `-I` ＋ 作業ディレクトリ `env\` で `mam_gptsovits_server.py` が `api_v2`（と `tools`・`GPT_SoVITS`）を import できること、`api_v2` のモジュール変数（`APP`・`host`・`port`）が 20250606v2pro のままであること、`GET /openapi.json` が 200 になるまでの時間 | **未確認**（`api_v2.py` のソース（タグ `20250606v2pro`）で、`sys.path` への追加・import 時に `argparse` が `sys.argv` を読んでモデルを読み込むこと・`APP = FastAPI()`・`__main__` の `uvicorn.run(app=APP, host=host, port=port, workers=1)` を読んで確かめたが、実行していない） |
| オフライン設定（`HF_HUB_OFFLINE` ほか）で、統合パッケージの推論が重みを取りに行かず動くこと | **未確認**（重みは同梱されているが、実機で起動して確かめていない） |
| VRAM の下限 4 GB（推定） | **未確認**（公式に数値がない。`vram.estimate=true`） |
| RTX 50 シリーズ向けパッケージ（`nvidia50`）の起動、`nvidia-smi` の `compute_cap` による選択 | **未確認**（判定は純粋関数のテストだけ。実機の `nvidia-smi` は使っていない） |
| Windows の Job Object・非表示ウィンドウでの起動 | model-manager.md §11 と同じ（ビルドと vet のみ。Linux の `Pdeathsig` は子プロセスのテストで確認済み） |
| パッケージ内の Python パッケージ・重みのライセンスと商用利用の可否 | **重みと依存ファイルは §12 で確認した（`unknown` が 5 件）。パッケージ内の Python パッケージ（約 100 個）は一覧を調べていない**（`THIRD_PARTY_NOTICES.md` に明記） |
| 日本語の音声の質 | **未確認**（公式の数値がない。実機で聞き比べる。親 Issue のメモ） |

## 12. 重み・依存ファイルの固定カタログ（Issue #920）

ランタイムが動くために**ディスクに無ければならない重み（学習済みモデル）と依存ファイル**（辞書・コーパス・言語判定モデルなど）を、**ランタイムごとのカタログ**（`catalog/data/assets/<ランタイム ID>.json`）に固定する。親 Issue の決定事項 1（重みは同梱せず実行時にダウンロード。固定 URL＋SHA-256 検証、再開可）と 3（出典とライセンスを画面と docs に出し、商用利用がはっきりしないものは画面で目立つように注記）の実装。Go の型は `catalog.ResourceSet`（1 ランタイムの一覧）・`catalog.Resource`（1 件。JSON のキーは `assets`）で、`Asset`（§3 のアーカイブ）と名前が重ならないよう `Resource` と呼ぶ。

### 12.1 スキーマ

```json
{
 "runtime": "gpt-sovits",
 "licenses": [ { "id": "mit-gpt-sovits", "name": "…", "url": "https://…", "summary": "…", "commercial": "yes", "notice": "" } ],
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
- `license`: そのファイルの `licenses[].id`。`commercial` は `yes` / `conditional` / `no` / **`unknown`**（旧 `commercial_use` の `unclear` は #928 で `unknown` になった）。`conditional`・`no`・`unknown` は `notice`（何に気をつけるか）が必須。`summary` には、**確かめた根拠**（モデルカードの `license:`・同梱の LICENSE・配布一覧の記載）と、**確かめられなかったこと**を書く。`ComfyUI` のモデルの `catalog.License` とは別の型だが、`commercial` の 4 値は同じ（`modellicense.Commercial`。#928）。

### 12.2 検証（`catalog.Validate`）

URL は `https://huggingface.co/<owner>/<repo>/resolve/<40 桁のコミット>/<パス>` か、**GitHub の 2 つの形**（Issue #986。`https://github.com/<owner>/<repo>/releases/download/<タグ>/<ファイル>` のリリースのアセット、`https://raw.githubusercontent.com/<owner>/<repo>/<40 桁のコミット>/<パス>` のコミット固定のファイル）だけ（ブランチ名・http・他のホストを拒否。**取得の許可ホスト（`fetch.AllowedURL`）は増やさない**: github.com・githubusercontent.com は ComfyUI・Codex の導入で既に許可している。Google Drive にある原本は、SHA-256 を確かめた Hugging Face のコピーを使う）。**リリースのタグは付け替えられる**ので、リリースのアセットは URL では固定できず、SHA-256 とサイズが固定の本体になる（不一致は取得を破棄）。`source.url` が原本のリポジトリ（GitHub の `<owner>/<repo>`）と同じなら、第三者のミラーではなく `source_note` は要らない（`inRepoOf`）。SHA-256 は小文字 64 桁、サイズは正。出典・ライセンスの欠落、ライセンス ID が一覧に無い、同じ名前・同じ `dest` が 2 件、`conditional` / `no` / `unknown` で `notice` が空、`commercial` が 4 値以外、取得先と原本が違うのに `source_note` に実在する日付が無い、アーカイブの `members` が空・エントリが `..` を含む・`dest` が外へ出る・メンバーの SHA-256 かサイズが無い、`required` が 1 件も無い、を拒否する。テスト: `catalog/assets_test.go`。

### 12.3 取得・検証（`runtime.Installer.EnsureAssets`）

1. 各 asset を**ディスクと照合**する（`stat` だけ）: 全ファイルがあって大きさが同じで、`state.json` の `files`（検証済みのファイルのサイズ・更新時刻）と一致 → `ok`。ファイルが無い・大きさが違う → `missing`。あって大きさも同じだが記録が無い・違う → `unverified`（パッケージを展開した直後、または誰かが書き換えた）。
2. `unverified` は**その場で SHA-256 を計算**する（進捗フェーズ `verify`）。一致 → 記録して終わり（**取り直さない**）。不一致 → **そのファイルを消して**取得に回す。
3. `missing` は固定 URL から取得する（`fetch.Download`: 単一ファイルは最終の置き場所の `.part` へ `Range` で再開、SHA-256 とサイズを照合してからリネーム。不一致は `.part` を消して `fetch.ErrChecksum` / `ErrSize`。アーカイブは `downloads\` へ取得し、メンバーを取り出して照合してから、アーカイブを消す）。
4. 始める前に、**取得が要る分（`.part` の分は引く＋アーカイブは取り出したファイルの分）の空き容量**を確かめる（足りなければ `*fetch.NoSpaceError`。何も取得しない）。書き込める場所かも確かめる。
5. 失敗は `*runtime.AssetError`（どの asset か）。それ以前に終えた asset はそのまま残る。取り消し・ネットワークの失敗は `.part` を残し、次の実行が続きから再開する。進捗は、取得・検証が要る asset の大きさの合計に対するバイト数（フェーズ `verify` / `weights`）。
6. パッケージの導入（§5.1）は、**環境を入れ替えたら記録を消し**（`files` を空に）、同じジョブの最後にこの手順を行う（進捗: パッケージの取得 55 % → 展開 20 % → 重みの確認・取得 25 %）。`Remove` も記録を消す。

**重みだけを導入する**（`Service.InstallWeights`。`model_install` のジョブ、`target=runtime:<id>`、ペイロードの `weights_only`）: 導入済みのランタイムで、足りない・壊れたファイルだけを直す（ファイルを消した・ウイルス対策に隔離された・取得が途中で止まった）。未導入は `ErrNotInstalled`、対応しない PC は `*SupportError`、空き容量不足は `*fetch.NoSpaceError`（どれもジョブを作らず断る）。同じランタイムのジョブが動いていれば、それを返す（`Existing`）。

### 12.4 起動の前に分かる

`Service.CheckStart`（`Start`・`Prepare` が最初に呼ぶ）は、対応環境 → 導入済み → 導入中でない → **必須の重みがそろって検証済み**、の順に調べる。そろっていなければ `*WeightsError`（`errors.Is(err, ErrWeightsMissing)`）で断り、サーバーを起動しない（起動後の Python の読み込み失敗にしない）。メッセージは「GPT-SoVITS の重みが不足しています（<名前>、…）。「重みを導入」を実行してください」、`unverified` なら「…の重みの確認が済んでいません…」。`View.Weights` は `state`（`none`: 未導入 / `ready` / `missing` / `unverified`）・`message`（`missing` なら「**導入済み（重みが不足）**。「重みを導入」で、足りないファイルを取得元から取得します」）・`assets[]`（名前・説明・状態・必須・大きさ・出典・ライセンス）・`not_ready`・`need_bytes`・`total_bytes`・`unknown`（`commercial` が `yes` でない asset の数）。ライセンスと出典は**未導入でも、この PC で使えない環境でも**返す（導入の前に読めるように）。

取得元が消えたとき（404・410）は、ジョブを失敗にして `ErrSourceGone` で包み、「取得元が消えています（<名前>、HTTP 404）。…」に、**第三者のミラーなら `source_note`（原本の場所）、そうでなければ原本の URL** を続ける。ハッシュ不一致は「ダウンロードしたファイルの SHA-256 が一致しません。ダウンロードを破棄しました」（`asset` の名前つき）。

### 12.5 表示

- **画面**: モデルマネージャーの区画「ローカル音声・口パク」（[model-manager-screen.md](../../components/model-manager-screen.md)、[endpoints-model-manager.md](../../api/endpoints-model-manager.md)）。ランタイムごとのカードに、状態・導入・起動・停止・自動起動・削除・進捗、**この PC で使えない理由とクラウドの案内**、重みの一覧（出典のリンク・ライセンス名・商用利用の可否）を出す。**`unknown` / `no` の重みは、一覧の前に琥珀色の枠で件数と名前を挙げ、一覧を開いたままにし、その行にも `notice` を枠つきで出す**。
- **MCP**: `list_local_runtimes`・`get_local_runtime_status`（`weights.assets[]` に出典・ライセンス・`commercial` と `notice`）、`install_local_runtime`・`install_local_runtime_weights`・`start_local_runtime`・`stop_local_runtime`・`remove_local_runtime`・`set_local_runtime_autostart`（[mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)）。画面も MCP も同じ `localruntime.Service` を呼び、拒否は `mmview.RefuseRuntime`（`mmview.Refuse` の種類 409 / `conflict`、422 / `validation`、404 / `not_found` に、ランタイム固有のエラーを載せたもの）で同じ文面になる。
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
- **g2pW のモデルは `commercial: unknown`**（#928 の前は `unclear`）: コード（`GitYCC/g2pW`）は Apache-2.0 だが、配布されるモデル（ONNX）のライセンス表示を、配布先でも原本のリポジトリでも確認できなかった。画面に目立つ注記を出す。これが現時点で `unknown` の唯一の GPT-SoVITS の asset。

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
- **`unknown`（商用利用がはっきりしない）の 4 件**: ①LatentSync の SyncNet — モデルカードは `license: openrail++`（用途の制限つきで商用可）だが、README は SyncNet を VoxCeleb2・HDTF で学習したとし、そのデータの商用での条件を確認できていない。②face-parse-bisent の `79999_iter.pth` — コードは MIT だが、重みは CelebAMask-HQ（**非商用の研究目的だけ**と README に明記）で学習したもので、重みのライセンスの表示が無い。③S3FD の重み — face-alignment のコードは BSD-3-Clause だが、重みの元（`yxlijun/S3FD.pytorch`）にライセンスの表示が無い。④torchvision の `resnet18`（ImageNet で学習。torchvision の README が「学習済みモデルは独自のライセンス・条件を持つことがある」と注意している）。MuseTalk 1.5 の重み自体は、モデルカードの `license: mit` と README の「学習済みモデルは商用を含むどんな目的にも使える」から `yes`。
- **`yes` と確かめたもの**: MuseTalk 1.5（MIT）・sd-vae-ft-mse（MIT。モデルカード）・whisper-tiny（Apache-2.0。モデルカード）・DWPose（Apache-2.0。モデルカード）。MuseTalk の `LICENSE.txt` も sd-vae-ft-mse・whisper・face-parsing.PyTorch のコードを MIT、DWPose を Apache-2.0、face-alignment を BSD-3-Clause としている。

### 12.8 未確認（#920）

| 項目 | 状態 |
|---|---|
| **実物の Windows ＋ NVIDIA でのパッケージ導入後の検証（約 2.0 GB の SHA-256 計算）・足りない重みの取得・起動** | **未確認**。偽の Hugging Face（`Range`・取り消し・ハッシュ不一致・404・空き容量不足）と、偽のパッケージ・偽のプロセスで、取得・再開・検証・拒否・起動の拒否を確認した。実物の重み（GPT-SoVITS の asset は約 2.0 GB、MuseTalk は約 6.0 GB）を `Range` で取得して導入する経路は、実機で走らせていない（SHA-256 とサイズは、Hugging Face の API の `lfs.oid`、ダウンロードしたファイル、公式の 7z の中身から確かめた） |
| 実物の `nltk_data.zip`・`G2PWModel.zip`・`open_jtalk_dic…tar.gz` からのメンバーの取り出し（`exeinstall.Extract`）と、取り出したファイルで GPT-SoVITS が動くこと | **未確認**（アーカイブの一覧とメンバーの SHA-256 は実物で確かめたが、取り出し→起動は実機で試していない。合成のアーカイブでは確認した） |
| MuseTalk の重みを置いた環境で MuseTalk が動くこと（`models/…` の配置が公式の既定と同じこと、S3FD の置き場所） | **Linux の CPU で確認した**（#921。§13.7）: 12 件の重みをカタログの `dest` に置き、ネットワークの無い名前空間で、公式の推論と同じ手順（ラッパー）が 1.2 秒の動画を作った。**Windows ＋ CUDA は未確認** |
| 各ライセンスの**原文との突き合わせ** | モデルカード・同梱の LICENSE・NLTK の配布一覧・MuseTalk の README / LICENSE.txt は読んだ。**法的な助言ではない**。`unknown` の 5 件（g2pW・SyncNet・face-parse・S3FD・resnet18）は、利用者が配布元に確認する前提（画面の注記） |
| 第三者のミラー（`ManyOtherFunctions/face-parse-bisent`・`XXXXRT/GPT-SoVITS-Pretrained`）が将来も残ること | 保証できない。消えたら `ErrSourceGone`（原本の場所つき）で失敗する。消えたことは §12.10 の定期照合が先に知らせる |

### 12.9 Shisei のポーズ集（`data/assets/shisei-poses.json`。Issue #986）

生成テストの「ポーズを選ぶ」（[../gentest/gentest.md](../gentest/gentest.md) §6）が使う、ポーズの参照画像（マネキンの線画と、ControlNet 用の深度・法線マップ。マップは Issue #1020）。**ランタイム（プロセス）ではなく、画像のファイルだけの資産セット**で、ID は `shisei-poses`（`data/runtimes/` には定義を置かない。§12.1 の「ランタイムが無い ID のファイルがあってもよい」と同じ扱い）。取得・検証は `rtm.Installer.EnsureAssets`（§12.3）をそのまま使い、`service/gentest/poses.Library.Install` が、同意（`accepted`）を確かめてから呼ぶ。置き場所は MAM の実行ファイルと同じ場所の `localai\shisei-poses\env\`（`catalog.json`、線画 `poses/<シリーズ>_<ID>_<アングル>.webp`、深度 `depth/` と法線 `normal/` の同名のファイル）。

| 名前 | 取得元（固定） | サイズ | 取り出すもの |
|---|---|---|---|
| 標準シリーズの線画 Zip | `wquguru/shisei` の Release `v0.4.1`（`shisei-std-v0.4.1.zip`。SHA-256 `3b88bda3…`） | 9,409,110 B | `line/` の 290 枚のうち、全年齢向けのポーズの分 |
| 上半身シリーズの線画 Zip | 同（`shisei-upper-v0.4.1.zip`。SHA-256 `c10a2fa4…`） | 10,037,299 B | `line/` の 124 枚のうち、同上 |
| 深度マップ Zip（#1020） | 同（`shisei-depth-v0.4.1.zip`。SHA-256 `d572572a…`） | 7,110,041 B | `depth/` のうち、線画のあるポーズ・アングルと同じ名前の分（`std_`・`upper_` の 335 枚） |
| 法線マップ Zip（#1020） | 同（`shisei-normal-v0.4.1.zip`。SHA-256 `7271104b…`） | 16,185,565 B | `normal/` のうち、同上（335 枚） |
| 分類表 `catalog.json` | `raw.githubusercontent.com/wquguru/shisei/26522a70…/data/catalog.json`（コミット固定。SHA-256 `e01c7f83…`） | 264,767 B | 全体 |

- **取り出すのは全年齢向け（`rating: general`）で線画のあるポーズだけ**: 96 ポーズ・414 枚（展開後 約 10.85 MB、取得 約 19.7 MiB）。成人向けのシリーズ（`sexy`・`duo`）は、このセットでは**取り出さない**（メンバーに挙げない。Issue #1020 で**別の資産セット `shisei-poses-adult` に分けた**。下の「成人向けのシリーズ」）。`rating: suggestive` の `std`・`upper` のポーズ（55 ポーズ）は、どちらのセットにも入れない（シリーズが成人向けなのではなく個々のポーズの分類で、年齢確認の入口はシリーズ単位に絞った）。粘土風・写真は取らない。深度・法線は #1020 で足した（下）。
- **深度・法線マップ（Issue #1020）**: Shisei の README が「ControlNet Depth / Normal 用」と言う Release の Zip（`shisei-depth-v0.4.1.zip`・`shisei-normal-v0.4.1.zip`）を、線画と同じ資産セットに足した（`required: true`。ダウンロードは 約 19.7 MB から 約 43.0 MB になる。導入済みの利用者は、足りない 2 つだけを取り直す）。**取り出すのは線画と同じポーズ・アングルの `std_`・`upper_` の分だけ**（メンバーは 335 枚ずつ。Shisei は深度・法線を線画の全部には作っておらず、取り出す線画 414 枚のうち 79 枚はマップが無い）。Zip には `depth/`（`normal/`）に `std`・`upper`・`sexy`・`duo` の 944 枚と `LICENSE`・`LICENSE-images`・`NOTICE` が入っているが、**`sexy`・`duo` のファイルは Zip の中にあってもメンバーに挙げず、取り出さない**（Zip 自体はダウンロードの間だけ `downloads` に置かれ、展開が終わると消える）。マップの名前は線画と同じ `<シリーズ>_<ID>_<アングル>.webp` なので、画像 ID は線画の ID の後ろに `.depth`・`.normal` を付ける（`std_P001_front.depth`。`poses.Library.File`・`ImageOwner`。MCP の `get_pose_library` は `depth_images`・`normal_images` で返す）。深度・法線を持たないポーズ・アングルは、その種類の一覧に出ない。固定は線画と同じ（リリースの資産 URL と、確かめた SHA-256・サイズ。リリースは `immutable: false` なので、タグではなく資産のバイト列を固定する）。ライセンスは線画と同じ CC0 1.0（Zip の `NOTICE`・`LICENSE-images` は、コミット `26522a70…` のファイルとバイト列が一致することを確かめた）。マップの作り方は `docs/LICENSING.md` の規則 3 に「Depth Anything V2 Small（Apache-2.0）・Marigold Normals v1-1（CreativeML OpenRAIL++-M。その使用制限はモデルの使い方に掛かり、出力の再配布には掛からない）。出力は CC0」とある。
- **使い方（#1020）**: ポーズを選ぶ画面の「画像の種類」（線画・深度・法線）で、一覧と追加する画像の種類を選ぶ（ID が `…_front.depth` のように変わるだけで、取得元は `pose_library`、役割は `pose` のまま）。ControlNet のモデル（`sdxl-controlnet-depth`・`sdxl-controlnet-normal`。[model-manager.md §4.2](model-manager.md)）は参照画像を**そのまま制御画像にする**ので、モデルに合う種類を選ぶ（mam は深度・法線を推定しない。写真を渡しても期待した形にならない）。ControlNet のワークフローは、全体のほかに `pose` の役割を受ける（`ReferenceCaps`。警告「ポーズの参照に対応していない」は出ない）。既存の Canny（`sdxl-controlnet-canny`）は変えない（線画を渡すと従来どおり mam がエッジ画像にする）。
- **深度・法線を任意のファイルにはしない（#1020 の判断。現状のまま `required: true`）**: 線画だけ導入済みの利用者（#1020 より前の導入）は、深度・法線の Zip が足りないので、ライブラリは「未導入」に戻り、導入案内を出す。**再導入は足りない 2 つの Zip（約 22.2 MiB）だけ**で、導入済みの線画・目録は取り直さない（`Installer.EnsureAssets` は揃っているアセットを飛ばす）。画面は「導入済みのファイルは取り直しません。足りない分（深度・法線マップなど）だけ、今回 22.2 MiB をダウンロードします」と出し（`Status.RemainBytes`。MCP は `get_pose_library` の `download_remaining`）、再導入が要ることを隠さない。**任意のファイル**は、統合ブランチに `catalog.File.Optional`（#1025。モデルのファイル用。無くてもモデルは `installed`）が入ったが、これは**モデルカタログの `File`（`InstallModel` の経路）専用**で、ポーズ集が使う資産セットの `catalog.Resource`（`rtm.Installer.EnsureAssets` の経路。`Required` だけ）には無い。ポーズ集のためだけに別の仕組みを作ることはしない。任意にするなら、`Resource.Optional`・`NotReady` の読み替え・ギャラリーの「画像の種類」を入っている種類だけにする・`ReadIndex` が無い種類を黙って飛ばす（今は 1 つでも欠けると全体が未導入）の 4 つを、別の Issue で決める。
- **成人向けのシリーズ（`sexy`・`duo`。#1020 の項目 2。実装した）**: 製品判断は「**既存の年齢確認（model-manager.md §6.2 の `AdultContent`。設定「成人向けモデルを表示する」、既定オフ、オンにするのは利用者の明示の確認だけ）をそのまま使う**。新しい年齢確認の方法・方針は足さない」。成人向けのモデルが §6.1 の述語（`HidesModel`）で導入・一覧・選択・生成を止められているのと同じ線に、ポーズを載せた。`poses.Deps.AdultOpen`（アプリは `ModelManager.AdultContentEnabled` を渡す。nil は永久に閉）を**呼び出しのたびに**聞くので、年齢確認をオン・オフした直後から効く（再起動・再導入は要らない）。
  - **資産セット `data/assets/shisei-poses-adult.json`（ID `shisei-poses-adult`。`localai\shisei-poses-adult`）**: 全年齢向けのセットとは**別の Zip・別のディレクトリ・別の記録**（`Library` が 2 つ目の `Installer` を持つ）。年齢確認がオフの間はこのセットを**取得もしない・ディスクにも置かない**（全年齢向けのセットに混ぜて「あるが出さない」にはしない）。4 つの Zip（すべて `wquguru/shisei` の Release `v0.4.1`、`required: true`、CC0 1.0＝`commercial: yes`）: `shisei-sexy-v0.4.1.zip`（SHA-256 `1df01ee7…`、9,304,519 B。`line/` の 352 枚＝80 ポーズ）、`shisei-duo-v0.4.1.zip`（`ca80b68b…`、7,744,472 B。201 枚＝49 ポーズ）、`shisei-depth-v0.4.1.zip`・`shisei-normal-v0.4.1.zip`（全年齢向けのセットと同じファイル〔7,110,041 B・16,185,565 B〕から `sexy_`・`duo_` の 427 枚ずつだけ。Shisei は深度・法線を線画の全部には作っておらず、線画 553 枚のうち 126 枚はマップが無い）。ダウンロードは合計 40,344,597 B（約 38.5 MiB。深度・法線の Zip は全年齢向けのセットと同じものを**もう一度**取る。展開後 28,254,816 B・1,407 枚・129 ポーズ）。URL・SHA-256・大きさは 2026-11-04 に GitHub の Release API（`digest`）と、実際にダウンロードした Zip の SHA-256 で確かめた。**メンバーも 1 枚ずつ SHA-256 と大きさを固定**した（Zip の中身から計算）。分類表 `catalog.json`（ポーズの名前・分類）は全年齢向けのセットの 1 つを使い、このセットには持たない（先に全年齢向けのポーズ集を導入していないと導入を断る＝`ErrBaseNotInstalled`）。
  - **ライセンス**: 画像は CC0 1.0（`commercial: yes`。全年齢向けのセットと同じ `LICENSE-images`）。Shisei の README は、この 2 つを「suggestive adult pose references（faceless mannequins, no nudity detail）」と呼び、既定ではぼかして出すと書く（コミット `26522a70…` の README）。**MAM は年齢を自分で確認したことにしない**（§6.2 のとおり）。
  - **`rating` の扱い**: 区分は**シリーズ**（`Series[].Adult`。`sexy`・`duo`）で決め、ポーズごとの `rating` は見ない。目録 288 件のうち `sexy` 80（`suggestive` 75・`general` 5）と `duo` 55（`suggestive` 49・`general` 6。画像があるのは 49）は、`rating` が `general` でも成人向けのシリーズとして扱う（`Pose.Adult`）。全年齢向けのセットのポーズは従来どおり `rating: general` だけ。
  - **ゲートがオフの間、出さないもの**（`Library.Poses`・`File`・`ImageOwner`・`Pose` が閉じているときは、成人向けの分が無いものとして答える）: ①**画面**: ポーズを選ぶモーダルのギャラリー・検索・分類・「全身・sexy（成人向け）」「二人・duo（成人向け）」のグループ・画像（`/poses/image/:id` は 404）・行への追加（`/ref-row` は 404）・導入の案内（`Status.Adult` が nil。「成人向けのポーズ」という言葉も出さない）。②**MCP**: `get_pose_library` は `adult` の節を返さず、`series=sexy`／`duo`・名前・番号の検索にも出さない。`install_pose_library{adult:true}` は何も取得せず `validation` で断る。③**ジョブ一覧**: 生成テストのバッチは**モデル名と件数だけ**を載せ、入力画像の出どころ（ポーズの名前・画像）は載せない（ゲートの開閉どちらでもテストで確かめた）。④**履歴**: 結果の詳細（画面・MCP の `get_generation_result`）の `inputs[]` と「使われなかった入力」から、成人向けのポーズから取った入力（出どころが `pose_library` の `sexy_`・`duo_`、それを `batch_input` で取り直した写しも）を除き、その写しのファイル（`GET /generation-test/inputs/:id/file`）は 404、「この設定でもう一度」・新しい実行にも使えない（`history.Store.InputHidden`）。⑤**実行**: 成人向けのポーズの入力を持つバッチが、ゲートを閉じたあとに走り出そうとすると、何も呼ばずに失敗にする（`runner.ErrInputHidden`。メッセージにポーズ名・画像 ID を載せない）。ゲートを開け直すと元に戻る（写しはバッチに残る）。
  - **導入・削除**: 画面は、ゲートがオンのときだけギャラリーの下に「成人向けのポーズ（sexy・二人）」の節を出し、サイズ・ライセンス・取得元と**もう一度の同意のチェック**（`pose_accept`）で導入する（`POST /generation-test/poses/install-adult`。サービスもゲートが閉じていれば `ErrAdultGateClosed`、同意が無ければ `ErrNotAccepted`、全年齢向けが未導入なら `ErrBaseNotInstalled` で断る）。導入済みなら「成人向けのポーズを削除」（`POST /poses/remove-adult`、確認つき）。MCP は `install_pose_library{accepted, adult}`・`remove_pose_library{adult}`。**全年齢向けの「ポーズ集を削除」（`Library.Remove`）は成人向けのセットも一緒に消す**（名前の元の目録が無くなるため）。削除はゲートの開閉に関わらずできる（`DeleteModel` と同じ）。ゲートを閉じても、導入済みのファイルは消さない（隠すだけ）。
  - **ゲートが閉じたときの取得・キャッシュ（統合の研磨で足した）**: ①**取得中・待ちの間の閉鎖**: `Library.ensure` は導入を 1 つずつに絞る `mu` を取るが、`mu` は別の導入の全ダウンロードの間ずっと持たれるので、待っている間にゲートが閉じることがある。成人向けのセットの導入（`InstallAdult`）は、`mu` を取ったあとにもう一度ゲートを見て、閉じていれば何も取得せず `ErrAdultGateClosed` で断る（モデルの導入〔`runModelInstall`〕がロック待ちのあとにもう一度ゲートを見るのと同じ）。取得の途中で閉じたときは、進み具合の通知ごとにゲートを見て取得を止め（context の取り消し）、`downloads` に残った途中のファイルを消す（取り終えて展開した分のファイルは、ほかのファイルと同じく「あるが出さない」。ゲートを開ければ続きから入る）。②**ブラウザのキャッシュ**: 成人向けのポーズの画像（`GET /generation-test/poses/image/:id` の `sexy_`・`duo_`）の `Cache-Control` は `no-store`（全年齢向けは従来どおり `private, max-age=3600`）。1 時間まで残ると、ゲートを閉じても見えてしまうため。
  - **範囲と限界（受け入れた）**: (1) 成人向けのポーズから**作った結果の画像**は隠さない（成人向けのモデルの結果を年齢確認オフでも履歴に残すのと同じ。§6.1）。(2) プロンプトマッチの参照画像の URL（`/inputs/:id/file`）は、成人向けのポーズから取った写しでも URL 自体は出る（ファイルは 404）。(3) `catalog.json` は成人向けの 129 件の名前を含み、全年齢向けのセットを入れた時点でディスクに載る（画面・MCP には出さない。名前そのものはライセンスの対象外の目録）。(4) ポーズの ID の名前（`sexy_`・`duo_`）で出どころを判定するので、ID の付け方が変わる版では見直す。
- **未確認（#1020）**: 実機の GPU での、ControlNet（深度・法線）にポーズ集のマップを渡したときの**出来**（ポーズが写るか・Shisei の法線マップの向きの約束が xinsir の Union の `normal` と合うか・強さの既定 0.7 が合うか）。`txt2img-sdxl-controlnet-depth`・`-normal` はグラフの配線と偽の ComfyUI へのアップロードまでをテストで確かめただけで、GPU では動かしていない。法線のモデルに xinsir の ControlNet Union（`SetUnionControlNetType` で `normal`）を選んだのは、SDXL の専用の法線 ControlNet のうち商用の答えを確かめられるものが見つからなかったため（`Eugeoter/noob-sdxl-controlnet-normal` は FAIPL-1.0-SD で NoobAI 派生向け）。Union の `normal` の質は専用のものと違うかもしれない。 **成人向けのセット（`shisei-poses-adult`）の GitHub からの実取得・Windows での導入、ポーズ選択モーダルの見た目（成人向けの節・グループ）の実ブラウザでの確認も未確認**（テストは偽の GitHub サーバーと偽の年齢確認で、実物の Zip のハッシュは 2026-11-04 にダウンロードして確かめた）。
- **未確認（#1020 の研磨）**: 成人向けのポーズ画像の `Cache-Control: no-store` が実ブラウザのキャッシュ（戻る・進む、ディスクのキャッシュ）で効くこと、実際の GitHub からの大きなダウンロードの途中でゲートを閉じたときの中断の速さと、`downloads` の片付け（テストは偽のサーバーと偽のゲートで、進み具合の通知の 1 回目で閉じた場合まで）は、実機で確かめていない。
- **ライセンス**: 画像は CC0 1.0（`commercial: yes`）。出典は Shisei の `NOTICE`・`LICENSE-images`（コミット `26522a70…`）と `docs/LICENSING.md` の「AI が作る画像の権利」の規則。コードは Apache-2.0（取得しない）。**分類表（ポーズの選び方・番号・名前・分類）は ai-pose-gallery（ギア〔@giaaibusi〕）に従い、NOTICE が「credited, not licensed」と書くので、`commercial: unknown`**（画面は注意書きを出す）。
- **確かめたこと（2026-11-03、GitHub API とダウンロード）**: タグ `v0.4.1` のコミットは `26522a70…`、メイン（`8f61ceab…`）は先に進んでいる。分類表は 288 件で画像のあるものは 280、アングルごとの線画の数は diag 263・side 257・front 251・above 228・below 213（README の「250 ポーズ × 5 アングル」とは一致しない）。Zip のバイト列は SHA-256 を記録した。**実物の Windows での導入・GitHub からの実取得は未確認**（テストは偽の GitHub サーバーで、`posestest` が同じ手順を走らせる）。
- 画面は同意のチェック（`pose_accept`）が無いと導入を断り、取得元・サイズ・ライセンスを先に見せる。導入中は、案内が `GET /generation-test/poses/progress` を 1 秒ごとに取り直して「ダウンロード中: x / y（n%）」を出す（`Library.Progress`。`EnsureAssets` の進み具合）。一覧の下の「ポーズ集を削除」（確認つき）と MCP の `remove_pose_library` は `Library.Remove`（`Installer.Remove` と同じ。画像・目録・ダウンロード・記録を消して未導入に戻す。導入中は conflict）。MCP からの導入は `install_pose_library`（`accepted: true` が同意に当たる。[../../api/mcp-tools-generation-test.md](../../api/mcp-tools-generation-test.md)。Issue #994）。

### 12.10 固定した URL の定期照合（Issue #937）

取得時には、固定した SHA-256 とサイズで毎回検証する（§12.3）。それとは別に、**ミラーや上流が消えたり中身が変わったりしたことを、利用者の導入が失敗する前に maintainer が知る**ための定期ジョブを置く。

| 項目 | 内容 |
|---|---|
| コマンド | `backend/cmd/upstreamcheck`（実装は `modelmgr/upstream`）。ローカルでも同じものを `make upstream-check`（`UPSTREAM_CHECK_FLAGS="-only musetalk -v"` で絞る）か `go run ./backend/cmd/upstreamcheck` で動かせる。不一致が 1 件でもあれば終了コード 1 |
| 対象 | カタログのすべての固定 URL（エンジンの各ビルドと macOS の部品、モデルのファイル、ランタイムのパッケージと部品、`data/assets/*.json` の重み・依存）。同じ URL・SHA-256・サイズは 1 件に数える（現在 270 件） |
| 許可ホスト | 取得と同じ検査（`fetch.AllowedURL`、ランタイムのパッケージ・部品は `fetch.PackageURL`）を、最初の URL とリダイレクトの各先に掛ける。許可されないホストへは問い合わせず、不一致として報告する |
| 生存・サイズ | `HEAD`。サイズが分からなければ `Range: bytes=0-0` の `GET`（`Content-Range` の全体サイズ）。Hugging Face は 1 回目の応答の `X-Linked-Size` を使い、署名つきの CDN の先へは進まない。固定したサイズと違えば「サイズが変わった」、404・410 は「消えた」、その他の 4xx・5xx は「取得できない」。ネットワークの失敗・429・5xx は `-retries` 回まで繰り返す |
| SHA-256 | Hugging Face の LFS のファイルは、1 回目の応答の `X-Linked-Etag`（SHA-256）と照合する（ダウンロードしない）。`X-Linked-Etag` を返さず、`-hash-below`（既定 16 MiB）以下のファイルは、ダウンロードして計算する |
| 限界（**SHA-256 をダウンロードなしでは照合できないもの**） | PyPI・PyTorch・OpenMMLab の wheel、python.org・GitHub のリリース・アーカイブ（エンジンの 7z、`archive/<コミット>.zip`、python-build-standalone）は、上流が SHA-256 をヘッダーで返さない。これらは `-hash-below` 以下ならダウンロードして照合し、**それより大きいもの（PyTorch・mmcv・scipy などの大きな wheel、エンジンの 7z、macOS 用の torch など。現在 21 件）は生存とサイズだけ**を確かめる（`-v` で「size」として列挙される）。同じサイズのまま中身が変わったことは、実際の導入が SHA-256 の不一致で検出する（§12.3）。大きなファイル（合計で数十 GB）の毎週のダウンロードは重いので行わない |
| ログインが要るもの | `Lightricks/LTX-2.5` など、Hugging Face のログインが要る 10 件（`Model.RequiresHFToken`）は、リポジトリのシークレット `HF_TOKEN`（利用条件に同意したアカウントの read トークン）があるときだけ問い合わせる。トークンは huggingface.co / hf.co にだけ送り、CDN へのリダイレクト先には送らない。無いときは「skipped」と数えて失敗にしない（落ちるのは、確かめられないからではなく、不一致があったときだけ） |
| 定期実行 | `.github/workflows/upstream-check.yml`。`schedule`（毎週月曜 03:17 UTC）と `workflow_dispatch`。**PR・push では走らせず**、`ci.yml` とは独立（`ci.yml` はこのジョブに依存しない）。不一致があるとジョブが失敗し、GitHub の失敗通知で気づける。Issue を自動で立てる処理は入れない |
| 現在の結果 | 2026-11-03 に実機のネットワークで実行: 270 件中、不一致 0、SHA-256 で一致 239、サイズだけ 21、ログインが要るため未確認 10（`HF_TOKEN` なし） |

### 12.11 顔チェック用モデル（`data/assets/onnx-face-models.json`。Issue #1026）

生成テストの顔の類似度・「顔だけ」の参照の切り出し・Judge の顔の一致が読む YuNet と SFace の ONNX。ポーズ集（§12.9）と同じく**ランタイム（プロセス）ではない、ファイルだけの資産セット**で、ID は `onnx-face-models`（`data/runtimes/` に定義を置かない）。ID が `face-models` ではなく `onnx-face-models` なのは、`catalog.ResourceSets` が ID 順に並び、先頭の資産セット（`gpt-sovits`）を添字 0 で読んで検証の変異テストを作る既存のテストがあるため（先頭を変えない）。導入・削除・画面は [model-manager.md](model-manager.md) §5.4。

| 名前 | 取得元（固定） | サイズ | SHA-256 | ライセンス |
|---|---|---|---|---|
| YuNet（顔検出）`face_detection_yunet_2023mar.onnx` | `github.com/opencv/opencv_zoo/raw/47534e27c9851bb1128ccc0102f1145e27f23f98/models/face_detection_yunet/…` | 232,589 B | `8f2383e4…2fa4` | MIT（Copyright (c) 2020 Shiqi Yu）。`models/face_detection_yunet/LICENSE` |
| SFace（顔の照合）`face_recognition_sface_2021dec.onnx` | 同 `…/models/face_recognition_sface/…` | 38,696,353 B | `0ba9fbfa…4e79` | Apache-2.0（README「このディレクトリのファイルはすべて Apache 2.0 License」）。`models/face_recognition_sface/LICENSE` |

- **`dest` はファイル名だけ**で、置き場所（`Installer` の環境ディレクトリ）は `config.FaceModels()`（§5.4 の `store.RuntimeLayout.Env`）。両方 `required`。
- **URL の形**: opencv_zoo の ONNX は Git LFS で、`raw.githubusercontent.com/…` は LFS のポインターファイル（131 B）を返す。**`github.com/<repo>/raw/<40 桁のコミット>/<path>`** は `media.githubusercontent.com` の実体に転送される（`fetch.AllowedURL` の許可ドメイン `github.com`・`githubusercontent.com` の中。2026-11-04 に両方を取得して確かめた: 前者 131 B の別ファイル、後者が上の SHA-256・サイズ）。このため `validate_assets.go` の `githubAssetURL` に、コミットに固定した `github.com/<repo>/raw/<40 桁>/…`（ブランチ名は不可）を足した。`upstream-check`（§12.10）も同じ URL を照合する。
- **ライセンスの答えと注意**: どちらも `commercial: yes`（重みのライセンスについて）。SFace の `notice` は「学習データの権利は一次情報（opencv_zoo の README・LICENSE）に書かれていないため確かめられていない。Apache-2.0 は学習データの権利まで保証しない」。**一次情報が言う以上のことは書かない**（学習データの権利の確認は [../model-adapters.md](../model-adapters.md) §8 の未解決のまま）。区画は「商用OK」の答えと注意を**両方**出す（`commercial.Note` は `yes` では何も描かないので、区画が `notice` を自分で描く）。
- **コミットの固定**: `scripts/fetch-face-models.sh` と同じコミット（`47534e27…`、2026-05-28）。2 つのファイルの SHA-256 は、実物をダウンロードして計算した値（スクリプトの値と同じ）。コミットを変えるときは、この JSON・スクリプト・`THIRD_PARTY_NOTICES.md` の説明を一緒に変える。
- **テスト**: `catalog/assetstest/face_models_test.go`（ファイル名・ハッシュ・サイズ・URL がコミット固定の `github.com/…/raw/` 形・ライセンスの答えと注意・URL の形の検証〔コミットなら通り、ブランチ・`blob`・別ホストは拒否〕）。`assets_test.go` の資産セットの数は 6 → 7。

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
| macOS | `os` | 「macOS では MuseTalk 1.5 を自動導入できません。**公式の PyTorch 版は macOS（Apple Silicon）の GPU に対応しておらず（CUDA 前提。`mmcv 2.0.1` の Apple Silicon 向けの事前ビルドも無い）、第三者の MLX 版（mlx-community/MuseTalk-1.5-q4。MIT）は重みと UNet・VAE・Whisper の実装だけで、顔の検出・貼り戻しを含む起動できるサーバーが無いため、mam の自動導入の対象にしていません。**mam が自動で導入するのは Windows + NVIDIA の GPU の組み合わせだけです」（#935 で具体化。§16.3） |
| Linux・その他の OS | `os` | 自動導入に対応していない |
| 64 ビット x86 でない | `arch` | 同上 |
| GPU を取得できない・NVIDIA がない（AMD・Intel） | `no_gpu`・`not_nvidia` | 同上（§4） |
| NVIDIA の最大の専用メモリ < 4 GB | `low_vram` | 「{gpu} のビデオメモリは {vram_gb} GB で、MuseTalk 1.5 に必要な 4 GB に足りない」 |
| Compute Capability ≥ 12.0（RTX 50） | `compute_cap` | 「{gpu} は NVIDIA の RTX 50 シリーズ（Blackwell）で、MuseTalk の公式の手順が使う PyTorch 2.0.1（CUDA 11.8）が対応していない」 |

**VRAM の下限 4 GB は、公式の README の記載に基づく**（Gradio Demo の節: 「Windows の NVIDIA GeForce RTX 3050 Ti Laptop GPU、4GB VRAM で、fp16 で 8 秒の動画を約 5 分で生成」）。`vram.estimate=false`（出典あり）。ただし **mam 自身の実機での実測は無い**（§13.8）。出力の解像度・長さによっては 4 GB で足りず、そのときは `out_of_memory` でわかりやすい文を返す。**MLX 版の自動導入は実装しない**（#935 で調べた結果は §16.3）。**クラウドへの自動の切り替えはしない**（§10。利用者が設定で選ぶ。#924）。

### 13.6 ライセンスの記録

MuseTalk 本体（コード MIT・学習済みモデルは商用を含むどんな目的にも使える）は、README の原文（「The code of MuseTalk is released under the MIT License. There is no limitation for both academic and commercial usage.」「The trained model are available for any purpose, even commercially.」「Other open-source models used must comply with their license, such as whisper, ft-mse-vae, dwpose, S3FD, etc..」）を `license.summary` に引いた（コミット `0a89dec4` の README を 2026-11-01 に確認）。重みと依存ファイルの出典・ライセンス・商用利用の可否は §12.7（`unknown` の 4 件を画面で目立たせる。#920 の画面がそのまま MuseTalk のカードに出す）。環境の部品の Python パッケージ 82 個のライセンスは、各 wheel のメタデータの表記を `THIRD_PARTY_NOTICES.md`（「MuseTalk 1.5（利用者が取得する別プログラム）」）に一覧にした。**原文との突き合わせはしていない**（未確認。`torch` の CUDA 版の wheel が含む NVIDIA の DLL の条件、`opencv-python`・`soundfile`・`soxr` が含む LGPL のライブラリを明記）。mam はどれも同梱せず、再配布しない。

### 13.7 テスト

- 部品の組み立て・展開・拒否: `modelmgr/runtime/assemble_test.go`（偽の取得元に 4 つの部品（Python・ソース・wheel 2 つ）。展開の配置・`.data` の扱い・`._pth` の置き換え・SHA-256 不一致・展開爆弾（`ErrTooLarge`）・再開（ダウンロード済みの部品を取り直さない）・失敗しても既存の環境が残る）、`extract/zip_test.go`（`..`・絶対パス・ドライブ・上書き・取り消し）、`fetch/auth_test.go`（`PackageURL`）。
- カタログ: `catalog/musetalktest/musetalk_test.go`・`catalog/parts/parts_test.go`（固定した URL・SHA-256・合計・URL パターン・検証の拒否 16 通り）。
- 対応環境: `modelmgr/runtime/supporttest/musetalk_support_test.go`（macOS・Linux・AMD・GPU なし・2 GB・RTX 50 を断り、Sync Labs の案内が付く）。
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

## 14. Irodori-TTS のランタイムと、モデルの選択（Issue #1015）

日本語専用の音声合成 Irodori-TTS（Aratako 氏）を、GPT-SoVITS・MuseTalk と同じ基盤（§3〜§8・§12・§13）で導入・起動・停止する。**この節の範囲は、ランタイムの組み立て・重みの固定・モデルの選択・同意まで**。Audio Director のプロバイダー（参照音声・キャプション・絵文字・GPU の扱い）は [model-adapters-m3.md §14.11・§14.12](../model-adapters-m3.md)。

### 14.1 全体

| 項目 | 内容 |
|---|---|
| ランタイム ID | `irodori-tts`（`data/runtimes/irodori-tts.json`。用途 `tts`）。声のプロバイダー値は `irodori`（GPT-SoVITS の `local` は変えない） |
| 対応環境 | Windows x64 ＋ NVIDIA の GPU だけ（§4 の判定。GPT-SoVITS と同じ規則）。macOS・Linux・非 x64・NVIDIA 無しは `unsupported`（理由と ElevenLabs の案内は、カタログの文。macOS の文は「公式のサーバーは Apple silicon（MPS）での推論に対応していますが、mam が自動で導入するのは Windows + NVIDIA の GPU の組み合わせだけです」） |
| 入口 | 公式の API サーバー Irodori-TTS-Server（OpenAI 互換の `POST /v1/audio/speech`）。mam は公式の起動（`python -m irodori_openai_tts`）を**使わず**、mam の起動用スクリプト `mam_irodori_server.py`（`data/overlays`）が公式のアプリを読み込み、前に番人を置いて起動する（§14.3） |
| 重み | 同梱しない。モデルの重み・音声コーデック・SilentCipher の透かしの重みを、固定の URL（コミット固定）と SHA-256 で取得（§14.4）。モデルは利用者が選ぶ（§14.5） |
| 参照 | Issue #1015。親は #912（ローカルの声・口パクの基盤）。モデル一覧の絞り込みの項目は #1011（§14.8） |

### 14.2 実行環境の作り方（決定と理由）

公式は Windows 向けの配布物（統合パッケージ）を出していない。そこで MuseTalk（§13.1）と同じく、mam が**部品をカタログに固定して取得し、組み立てる**（`catalog.RuntimeVariant.Parts`・`catalog.Runtime.Build`）。部品は 160 個、取得は 3,156,040,034 B、展開後 5,382,468,374 B（**どの部品も実物のファイルで SHA-256 と大きさを測った**。wheel は公式サーバーの `uv.lock` のコミット `61012c76…` の Windows・Python 3.10・CUDA 12.8（`torch 2.10.0+cu128`）の分で、ロックの SHA-256 と一致することを確かめた）。

| 部品 | 数 | 置き場所（環境の下） |
|---|---|---|
| Python 3.10.11 の組み込み版（MuseTalk と同じ部品。`python.org`） | 1 | `python/` |
| Python の wheel（PyPI と `download.pytorch.org/whl/cu128`。torch・torchaudio の URL はロックの `download-r2.pytorch.org` を `download.pytorch.org` に直した） | 155 | `Lib/site-packages` |
| GitHub の zip（コミット固定）: Irodori-TTS-Server `61012c76…`・Irodori-TTS `89f9d8fb…`・DACVAE `414c2078…`・SilentCipher `d46d7d08…` | 4 | `irodori-tts-server/`・`irodori-tts/`・`dacvae-src/`・`silentcipher-src/` |
| PyPI の sdist（`.tar.gz`。wheel が無い純 Python の argbind 0.3.9・julius 0.2.7・randomname 0.2.1） | 3 | `vendor/<名前>/`（ビルドも実行もせず、検索パスに置く） |

- **PyPI の sdist を許す**: `parts.URLAllowed` に、`files.pythonhosted.org/packages/…/<名前>.tar.gz` を足した（`KindTarGz` の部品だけ。wheel の種類の部品は引き続き `.whl` だけ。ホストは変わらない）。展開は既存の `extract.TarGz`（`extracted_size` で打ち切り、環境の外へ書かない）。
- `python310._pth` は、`../irodori-tts-server/src`・`../irodori-tts`・`../dacvae-src`・`../silentcipher-src/src`・`../vendor/*` を足す（`-I` で起動しても効く。§13.1 と同じ）。
- 起動: `python -I -u -X utf8 mam_irodori_server.py --host 127.0.0.1 --port {port}`。準備の確認は `GET /health`（`ready_path`）。起動の待ちは 600 秒（モデルの読み込みを待つ）。トークンは環境変数 `MAM_RUNTIME_TOKEN`（§5.2）。ネットワークは `HF_HUB_OFFLINE=1`・`TRANSFORMERS_OFFLINE=1` で止める（トークナイザーは各モデルに同梱し、テキストエンコーダーの設定は safetensors のメタデータにあるので、実行中に Hugging Face へ取りに行かない）。
- ライセンス: コード（サーバー・推論・DACVAE・SilentCipher）は MIT。`commercial: yes`。重みは §14.4（`unknown` が 1 件）。

### 14.3 番人（`mam_irodori_server.py`）

公式のサーバーは、(1) Host の検査が無い、(2) `irodori.ref_wav` でこの PC の任意の音声ファイルを読む、(3) `irodori.lora_adapter` で任意のディレクトリの LoRA を読み込む、(4) `/v1/audio/voices` で誰でも声のファイルを足したり消したりできる、(5) `IRODORI_API_KEY` が無ければ認証が無い。ブラウザのページから `127.0.0.1` へ要求を送れるので、公式の起動のままでは危ない。そこで番人（標準ライブラリだけ。`torch`・`fastapi` 無しでテストできる）が、次だけに答える（GPT-SoVITS の番人 §5.2 と同じ考え方）。

| 要求 | 条件 |
|---|---|
| `POST /v1/audio/speech` | ヘッダー `X-Mam-Token`（その起動のトークン。`hmac.compare_digest`）が要る |
| `GET /health` | トークン不要（mam の準備確認） |

- Host が `127.0.0.1:<port>`・`localhost:<port>` 以外なら 403（DNS rebinding）。上の 2 つ以外の要求（声のアップロード・削除、`/v1/models`、ドキュメントの画面など）は、トークンがあっても 404。`lora_adapter` は mam が送らない。
- 使うモデルは `MAM_RUNTIME_CHOICE`（§14.5）の名前のフォルダ（`models/<選択>/model.safetensors`）。コーデックは `models/codec/weights.pth`、透かしは `models/silentcipher/44_1_khz/73999_iteration/`（mam が取得して検証したもの）。`IRODORI_*` の設定（ホスト・ポート・チェックポイント・コーデック・声のフォルダ・先読み・参照無しの許可）は番人が決めて、公式のアプリを読み込む前に環境変数に入れる。
- SilentCipher の読み込み（`silentcipher.get_model`）は、公式では Hugging Face から取りに行くので、番人が**取得済みのファイルを読む形に差し替える**。差し替えは番人のテスト（偽の `silentcipher`）で確かめた。**実物の動作は未確認**。

### 14.4 重みと依存ファイル（`data/assets/irodori-tts.json`）

どれも `huggingface.co/<repo>/resolve/<コミット>/…` で固定し、実物のファイルで SHA-256 と大きさを測った（2026-11-03）。**どのモデルも、実行中の取得は無い**。

| 名前 | 取得元（コミット） | 大きさ | 置き場所 | ライセンス |
|---|---|---|---|---|
| コーデック（Semantic-DACVAE-Japanese-32dim） | `Aratako/Semantic-DACVAE-Japanese-32dim`（`47376ee2…`） | 429,620,065 B | `models/codec/weights.pth` | MIT（`yes`） |
| 透かし（SilentCipher 44.1 kHz。4 ファイル） | `sony/silentcipher`（`a1c4d021…`） | 計 11,749,851 B（`dec_c` 2,008,794・`dec_m_0` 9,554,818・`enc_c` 184,765・`hparams.yaml` 1,474） | `models/silentcipher/44_1_khz/73999_iteration/` | **確認できていない（`unknown`）** |
| v4.1-Small | `Aratako/Irodori-TTS-v4.1-Small`（`2b28324d…`） | 3,064,295,596 B ＋ トークナイザー 6,719,163 B | `models/v4.1-small/` | MIT（`yes`） |
| v4.1-Small-MF | `Aratako/Irodori-TTS-v4.1-Small-MF`（`ccc78f5d…`） | 3,093,131,788 B ＋ 同 | `models/v4.1-small-mf/` | MIT（`yes`） |
| v4-Large（fp32） | `Aratako/Irodori-TTS-v4-Large`（`2e0c5542…`） | 13,152,893,348 B ＋ 33,385,189 B | `models/v4-large/` | Gemma Terms（`conditional`） |
| v4-Large int8 / int4 / fp8 | `Aratako/Irodori-TTS-v4-Large-Quantized`（`bdef9592…`） | 3,839,580,074 / 2,955,004,714 / 3,842,708,698 B ＋ 33,385,189 B | `models/v4-large-int8/` ほか | Gemma Terms（`conditional`） |

- 各モデルを選んだときの取得量（共通の部分〔コーデック・透かし 計 441,369,916 B〕を足した値）は、v4.1-Small 3.27 GiB、v4.1-Small-MF 3.30 GiB、v4-Large 12.69 GiB、int8 4.02 GiB、int4 3.19 GiB、fp8 4.02 GiB（画面と `list_local_runtime_models` の `size_bytes`）。
- **ライセンスの根拠**（モデルカードの `license:` を読んだ）: v4.1-Small・v4.1-Small-MF は `mit`、v4-Large・v4-Large-Quantized は `gemma`、Semantic-DACVAE は `mit`（元の `facebook/dacvae-watermarked` は Apache-2.0）。いずれもゲート（利用申請）は無い。**SilentCipher の重み（`sony/silentcipher`）は、リポジトリにもモデルカードにもライセンスの表示が無い**ので `unknown`。コードは MIT。Irodori-TTS は生成した音声に必ずこの透かしを入れる（切れない）ので、重みを省けない。画面に琥珀色の注記を出し、商用で使う前に配布元（Sony Research）に確認するよう言う。**法的な助言ではない**。
- 参照音声なしでキャプションだけで作る声（Voice Design）の注意と、声のコピーの倫理上の制限は、モデルごとの `cautions` に入れた（§14.6）。

### 14.5 モデルの選択（`ResourceSet.Choices`・`Resource.Choice`）

1 つのランタイムが複数のモデルを持つ仕組みを足した（**`catalog.go` の `Model` 構造体には触れない**。ランタイム側の資産セットに足した）。

- **カタログ**: `ResourceSet.Choices[]`（`catalog/choice.Choice`）と、`Resource.Choice`（その資産が属するモデル。空は全モデル共通）。`Choice` は `id`（slug）・`name`・`description`・`default`（ちょうど 1 つ）・`kind`（`tts`）・`languages`（`["ja"]`）・`license`（セットのライセンスの id）・`consent{version,text}`（同意が要るモデルだけ）・`cautions[]`・`vram_min_gb`/`vram_recommended_gb`/`vram_limits`/`vram_note`/`vram_estimate`・`env[]`（そのモデルで起動するときに足す環境変数）。検証は `choice.Check`（スキーマの項目・ライセンスの id・`vram_recommended_gb > vram_min_gb` なら `vram_limits`・モデルごとに資産が 1 つ以上・`default` がちょうど 1 つ・資産が名指すモデルが存在すること）。
- **選択の保存**: `localai/<id>/state.json` の `choice`（空は既定）。`Installer.ActiveSet()` が「選択中のモデルの資産＋共通の資産」を返し、`Assets`・`AssetNeedBytes`・`EnsureAssets` がそれだけを数える・取得する。**選ばれていないモデルの重みは取得しない**。カタログに無い選択（古い記録）は既定に戻る。
- **起動**: `Installer.Command` が、環境変数 `MAM_RUNTIME_CHOICE=<選択の id>` と、そのモデルの `env`（Large 系は `IRODORI_MODEL_PRECISION=bf16`）を足す。番人（§14.3）は `models/<MAM_RUNTIME_CHOICE>/model.safetensors` を読む。選択の無いランタイム（GPT-SoVITS・MuseTalk）には何も足さない。
- **`runtimechoices.Service`**（`service/runtimechoices`。`localruntime` の行数の上限のため別パッケージ）: `Views(ctx, runtimeID)`（モデルごとの `active`・`size_bytes`・ライセンス・`consent_needed`/`accepted`・`vram_fit`〔この PC の GPU との比較。推奨が最小より大きく `vram_limits` があるモデルだけ〕）と、`Select(ctx, runtimeID, choiceID, accept)`。`Select` は、導入の最中は `ErrBusy`、知らないモデルは `choice.ErrUnknown`、同意が要るのに `accept.Version` が一致しなければ `choice.ErrConsentRequired`（何も記録しない）。通れば `choice` を保存し（同意があれば §14.6 の記録も）、サーバーを**止める**（次の起動から新しいモデル。重みは取得しない。足りなければ「重みを導入」が要る）。
- **MCP**: `list_local_runtime_models(runtime_id)`・`select_local_runtime_model(runtime_id, model_id, accept_license_version?)`（[mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)）。
- **画面**: モデルマネージャーのランタイムのカードの詳細（`GET /model-manager/runtimes/:id/detail`）に「使うモデルの選択」（ラジオ。名前・説明・取得量・言語・VRAM の目安と、この PC の GPU との比較・ライセンス・商用・使うときの制限・同意のテキストとチェックボックス）。`PUT /model-manager/runtimes/:id/model`（`model`、同意のチェックは `accept_<モデル>`＝表示したテキストの版）。選んだことと「重みを導入」の案内を、同じ領域に出す。
- 選択を変えると、使う重みが変わる。`localruntime.View.Choice`（選択中のモデル）と `weights` はそのモデルの分。

### 14.6 同意（Gemma Terms of Use）

- v4-Large 系（`v4-large`・`-int8`・`-int4`・`-fp8`）は Gemma Terms of Use と禁止用途ポリシーの下にある。**同意するまで、選ぶ・重みを取得する・起動することはできない**（`localruntime.refuse` が `choice.ErrConsentRequired` を返す。導入・重み・起動のすべての入口）。
- 同意は、`Select` に**利用者自身の操作が渡す版**（画面のチェックボックスの値、MCP の `accept_license_version`）だけが記録する（`RuntimeState.Consents[ライセンスの id] = {version, accepted_at}`。モデルマネージャーの同意の記録 `store.Consent` と同じ形）。mam のコードは、同意を自動で行わない・代わりに渡さない（MCP の説明にも「エージェントが代わりに渡してはならない」と書いた）。テキストの版が変われば、もう一度同意を求める。版: `gemma-terms-2026-04-01+prohibited-use-2024-02-21`。**この版の文字列と、同意のテキストの文言は、オーナーの確認が要る**。
- 声のコピーの倫理上の制限（モデルカードより）は、全モデルの `cautions` に入れ、モデルの選択・設定の「音声・口パク」・Character の参照音声のフォームに同じ趣旨の文で出す: 本人の同意なしに実在の人の声をコピーしたりなりすましに使ったりしない／人をだます合成音声・ディープフェイクに使わない／参照音声なしの声が偶然実在の人に似ることがある／透かしが自動で入る（切れない）。

### 14.7 テスト

- `catalog/irodoritest`: 番人で起動すること、トークン・許可する要求だけ、ピン止め（部品の SHA-256・GitHub は 40 桁のコミット・重みの URL はコミット固定）、モデルごとの制限・ライセンス・同意、重みの置き場所。番人の Python のテスト（`testdata/test_irodori_server.py`。偽の公式アプリ・偽の `silentcipher`）を Python があれば実行する。
- `catalog/choice`（スキーマの検証）、`modelmgr/runtime/choicetest`（選択中のモデルの重みだけを数え・取得すること、起動の環境変数）、`service/runtimechoices`（同意の流れ。版が違う・無い・知らないモデルは何も記録しない）、`mcp/tools/runtimemodels`、`web/handler/modelmgr/runtimes`（モデルの選択の画面）。
- 実物の Windows ＋ NVIDIA での導入・起動・合成は、この開発環境（Linux・GPU 無し）では確かめられない（§14.9）。

### 14.8 #1011 との関係（フィルター）

モデルの絞り込み（Issue #1011。[model-filter.md](model-filter.md)）に、実行環境のモデル（`choice.Choice`）を載せた。**フィルターの語彙は #1011 のものに合わせ、`Choice` の `kind` は変えない**: `runtimechoices.View.Item()` が `Choice` を `modelfilter.Item` にする。

| `Item` の項目 | 値 |
|---|---|
| `Kinds` | `kind: tts` は `audio`（「音声」）、`kind: llm` は `llm` |
| `Languages` | `Choice.Languages`（ISO 639-1。Irodori-TTS の 6 モデルはすべて `ja`。llama.cpp の 8 モデルは `ja`・`en`） |
| `Adult`・`Uncensored` | `Choice.Uncensored`（非検閲は成人向けでもある） |
| `VRAMMinGB`・`Commercial` | `Choice.VRAMMinGB`・ライセンスの `commercial` |
| `Origin`・`Provider` | `local`・`local`（この PC で動く） |
| `Installed`・`Available` | どちらも `true`（モデルはどれでも選べる。重みは選んだあとに取得する。使用中でないモデルの重みは追跡しないので、「導入済み」では絞らない） |
| `Tags` | `kind` とライセンス名（テキスト検索が読む） |

#1011 の `Condition` には、このために **`languages`**（音声モデルの言語。どれか。ISO 639-1 の 2 字。言語を持たないモデル＝画像・動画は入らない）を足した（[model-filter.md §2](model-filter.md)）。`kinds`（`audio`・`llm`）は #1011 で定義済みの語。`runtimechoices.Where(views, cond)` が絞る。

- **MCP**: `list_local_runtime_models` は `runtime_id` を**省略可**にし、`Condition` の引数（`kinds`・`languages`・`uncensored`・`vram_fit`・`commercial`・`q` ほか）を取る。省略すると、モデルを持つすべての実行環境のモデルを返す（各モデルに `runtime_id`）。**日本語の声**は `kinds=["audio"]`・`languages=["ja"]`。`commercial` を省くと、設定「商用OKのモデルだけを使う」に従う（`list_model_catalog` と同じ。適用した値は `commercial_filter`）。不正な語は `validation`（キー名つき）。
- **画面**: 実行環境のカードの詳細の「使うモデルの選択」に、モデルマネージャーの一覧と同じフィルターバー（検索・漏斗・条件のチップ・件数。`modelpick`）を付けた。`GET /model-manager/runtimes/:id/models`（クエリは `Condition` のキー）が、条件に合うモデルの一覧と漏斗の件数（OOB）を返す。条件は実行環境ごとに覚える（Cookie `mam_mf_runtime-models-<id>`）。パネルに出すのは、その実行環境のモデルを見分ける項目だけ: VRAM（16GB で動く）・商用利用は常に、**種類・言語は、実行環境のモデルの間で違うときだけ**（Irodori-TTS の 6 モデルはすべて日本語の声なので、その一覧に「音声」「日本語」の欄を出しても何も絞れない。実行環境をまたぐ絞り込みは MCP）、非検閲は 1 つでも持つとき。フィルターで一覧を描き直しても、ユーザーが押していたラジオと、チェックした同意（`accept_<モデル>`。表示した版と同じときだけ）は残る（同意を代わりにチェックすることはしない）。絞り込みで一覧に出ないモデルは、フォームにも載らないので選べない。設定「商用OKのモデルだけを使う」がオンのときは、他の一覧と同じく商用 OK でないモデル（Gemma Terms の Large 系）は始めの状態では出ず、チップで条件が分かる（「すべてクリア」で全部出る）。
- テスト: `modelfilter`（`languages` の一致・クエリ・JSON・不正な語）、`runtimechoices`（`Item`・`Where`）、`web/handler/modelmgr/runtimes`（バー・パネルの項目・テキスト絞り込みとチップ・不正な条件・ラジオと同意の維持）、`web/molecules/modelpick`（言語の欄とチップ）、`mcp/tools/runtimemodels`（実行環境をまたぐ絞り込み・不正な語）。

### 14.9 未確認

| 項目 | 状態 |
|---|---|
| **実物の Windows ＋ NVIDIA での導入（部品 約 3.2 GB の取得と約 5.4 GB の展開）・起動・合成** | **未確認**。この開発環境は Linux で GPU が無い。部品の SHA-256 と大きさは実物のファイルで測ったが、Windows で実行していない。Go 側の偽のホスト・偽のプロセスと、番人の Python のテストだけ |
| 組み込み Python（`python310._pth`）で、`-I` ＋ 作業ディレクトリ ＋ `.pth` が期待どおりに効くこと、`import torch` が CUDA の DLL を見つけること | **未確認**（§13.8 と同じ。Visual C++ 再頒布可能パッケージが要る） |
| **`torchcodec` と FFmpeg の DLL**（wheel が FFmpeg の共有ライブラリを必要とする。Windows で見つかるか） | **未確認**。音声の読み込みが `torchaudio`・`soundfile` で足りるか、公式のコードが `torchcodec` を呼ぶかは実機で確かめる |
| 番人の SilentCipher の差し替え（取得済みのファイルを読む）が、実物の `silentcipher` でそのまま動くこと | **未確認** |
| 生成した音声に実際に透かしが入ること | **未確認** |
| **VRAM の数値**（v4.1-Small 6〜8 GB、v4-Large 10〜12 GB、int8 6〜8 GB、int4 5〜6 GB、fp8 6〜8 GB） | **mam の推定**で、実機では未確認（公式は推論に必要な数値を示していない。重みの大きさ＋コーデック約 0.4 GB＋作業領域）。`vram_estimate: true`。画面は「mam の推定です。実機では確かめていません」と言う |
| 合成の質・速さ（日本語の聞き比べ）、MF（MeanFlow・4 ステップ）の質 | **未確認**。数値は出さない |
| 量子化版の GPU 世代の条件（int4 は RTX 30 世代〔Ampere〕以降、fp8 は RTX 40 世代〔Ada〕以降。モデルカード） | 画面は注意書きを出すだけで、**GPU 世代で選択を制限しない**（オーナーの判断待ち） |
| bf16（Large 系の `IRODORI_MODEL_PRECISION=bf16`）の質 | **未確認** |
| SilentCipher の重みの商用利用の可否 | **確認できていない**（`unknown`。§14.4） |
| 部品の URL が将来も残ること | 保証できない。変われば SHA-256 の不一致・404 で失敗する |

## 15. 見送り一覧（Issue #938。#912 / PR #934 の研磨の minor 指摘）

PR #934 の研磨 2 周で出た minor（`--skip-minor` のため未対応だった）のうち、**直さないと決めたもの**と、**直さずに仕様として記録したもの**。直したものは [model-manager.changelog.md](model-manager.changelog.md) 1.44 に書いた。1 件 1 行（指摘・理由）。

| 指摘 | 判断と理由 |
|---|---|
| SyncNet の重み（`latentsync_syncnet.pt`、約 1.5 GB、ライセンス不明）が `required: true` で、推論では読まないのに導入に必須 | **見送り（オーナーの決定が要る）**。決定 3 の「表示する」を `required` にするかどうかはカタログの方針で、実機での確認（SyncNet を欠いた MuseTalk が起動すること）も要る。変えるなら `required: false` にして `CheckStart` が止めないようにする（1 行）。今は決定 3 のまま |
| `elevenlabs/` が外部テストだけで、本体は `audiodirector/elevenlabs.go` に残っている | **見送り**。本体を移すと、`audiodirector` の公開型への依存の向きと、すべての呼び出し元の import を変える作業になり、この指摘（置き場の見た目）に対して大きすぎる。行数の上限を保つためのテスト分割で、動作に影響はない。アダプターを移すときにまとめて直す |
| 展開したアーカイブを同じ URL の別の重みが使うとき、削除して 2 回ダウンロードする（`nltk_data.zip`、9.9 MB） | **見送り**。影響が 1 回分の 9.9 MB だけで、URL ごとにまとめて取得する変更は進捗・再開・合計サイズの計算に及ぶ。合計の重複（`TotalSize`）は表示で既に除いている |
| ランタイムの更新（版を上げる）で `env/` ごと重みも消え、重みをすべて取り直す | **見送り（§12.3 の 6 で決めた動作）**。更新は旧環境の丸ごと入れ替えで、古い重みを新しい環境へ移すには、新しい版が同じ配置であることの保証が要る。重みを `env/` の外へ出す変更は配置の仕様（§2）を変える |
| `Status()` が書き込みの確認・ログの末尾・重みの状態を毎回読む（TTS の 1 行ごと）ため、実行中の URL だけを返す軽い入口にしたい | **見送り**。`Ensure` が失敗する条件（実行中でない）は直した。軽い入口は `localruntime` の公開 API を増やす割に、1 行の TTS の間隔（秒単位）に対する費用は小さい |
| `audiosettings.View()` の表示のたびに `CheckStart` が GPU を新しく調べる（nvidia-smi 2 回）。1 分のキャッシュを使いたい | **見送り**。`CheckStart` は「起動できるか」の判定なので、キャッシュを使うと古い GPU の情報で通る。表示用に別の入口（`fresh` の引数）を作るのは API の変更で、設定画面を開くときだけの費用 |
| 重みの導入・修復（`WeightsOnly`）でも、動いているサーバーを止める | **見送り**。Windows では、動いているサーバーが開いている重みのファイルは置き換えられない（読み込み中のファイルの置き換えが失敗する）。どの重みが読み込み済みかは mam には分からないので、止めるのが安全。進行中の生成は `ErrUnreachable` で失敗し、再実行で直る |
| `audio.json` が壊れているときの `ErrUnreadable` が、`AIError`（HTTP 502「上流の AI の失敗」）で返る | **仕様として記録**。利用者へ出す文（`UserMessage`）は同じで、HTTP の分類だけが違う。この分類は `localspeech.Reason` の全体の規則（ローカルの設定の失敗も `AIError`）に合わせている。分類を変えるのは Audio Director の全プロバイダーの規則の変更になる |
| GPT-SoVITS と MuseTalk の VRAM を合算せず、片方の常駐のまま他方を起動する | **仕様として記録**。`vram.min_gb` はランタイムごとの目安で、MAX QUALITY で両方を使うには両方の合計が要る。実機（Windows ＋ NVIDIA）で確かめていないので、アイドルのランタイムを止める仕組みは入れない。§11 の未確認に含む |
| 新しい録音を置くとき、書き起こしの欄を触らないと、保存済みと同じ書き起こしは「送られていない」扱いで消える | **見送り**。サーバーは「触っていない」と「同じ文を入れ直した」を区別できない。画面は新しい録音を選んだときに書き起こしを入れ直すよう案内している（`spec/api/endpoints.md` §2.9）。クライアント側で欄を空にする変更は別 Issue |
| ある版のカノン（`series_version` のスナップショット）が参照する旧い参照音声のファイルは、カノンの声を置き換えて Episode がコピーしていなければ、消える | **仕様として記録**。履歴の版は読み取り専用の記録で、再生できなくなる。ファイルを残すにはスナップショットの参照も `InUse` で数える必要があり、容量が増える。必要になったら `InUse` の対象を広げる |

## 16. macOS（Apple Silicon）の調査と、ランタイムごとの判定（Issue #935）

#912 / PR #934 の後続。「Apple Silicon で声（GPT-SoVITS）と口パク（MuseTalk）をローカルで動かす」を、**一次情報（公式の README・ソース・PyPI の JSON API・Hugging Face の API・OpenMMLab の公式の索引）を 2026-11-04 に実際に取得して**調べた結果。**結論: どちらも mam が「固定した部品を展開して組み立てる」方式（§3・§13.1・§14）では、確かな一次情報だけでは組み立てられない。よって macOS（darwin/arm64）を対応 OS にせず、ランタイムごとの理由を画面・MCP・Audio Director に出す**（実機の Mac が無いので、組み立てても動作を確かめられない。確かめられない URL・SHA-256・サイズ・版を推測で入れない）。

### 16.1 判定の仕組み（変更なし。各ランタイムが自分の理由を持つ）

`Evaluate(runtime, Platform, gpu.Report)`（§4）は元からランタイムごとに `catalog.Runtime.Support`（`os_reasons.darwin`・`fallback`）を読む。macOS（Apple Silicon でも Intel でも）は、どのランタイムも `Code = os` で断られ、**そのランタイム自身の文**と**そのランタイムの代わりのクラウド**（GPT-SoVITS・Irodori-TTS は ElevenLabs、MuseTalk は Sync Labs）が付く。Apple Silicon のユニファイドメモリ（`gpu.VendorApple`、ComfyUI の同梱エンジンは [model-manager.md §3.7](model-manager.md) でこれを VRAM として扱う）は、**NVIDIA の VRAM としては数えない**（`bestNVIDIA` は NVIDIA だけを見るので、64 GB の Mac でも `Supported` にならない）。画面（モデルマネージャー・設定の「音声・口パク」）・MCP（`list_local_runtime`・`install_local_runtime`）・Audio Director（`localspeech.UnavailableFor` → `localvoice.UnavailableError{Kind: unsupported}`。クラウドへの切り替えは利用者が選ぶ。#924）は、同じ `Support.Message()` を使う。今回、`os_reasons.darwin` の文を、**ランタイムごとに具体的な理由**へ書き換えた（以前は「mam が自動で導入するのは Windows + NVIDIA だけです」だけ）。

| ランタイム | 画面・MCP に出る macOS の理由（要旨） | 代わりの案内 |
|---|---|---|
| GPT-SoVITS | 公式は macOS の MPS / CPU に対応するが、導入は conda＋pip でその場でビルドする方式で Apple Silicon 向けの配布物が無い。固定して取得できる wheel だけでは、起動に必須の `pyopenjtalk`・`jieba_fast`・`jieba` が揃わない（§16.2） | クラウドの音声合成（ElevenLabs） |
| MuseTalk 1.5 | 公式の PyTorch 版は CUDA 前提で、`mmcv==2.0.1` の Apple Silicon 向けの事前ビルドも無い。第三者の MLX 版（`mlx-community/MuseTalk-1.5-q4`、MIT）は重みと UNet・VAE・Whisper の実装だけで、顔の検出・貼り戻しを含む起動できるサーバーが無い（§16.3） | クラウドの口パク（Sync Labs） |
| Irodori-TTS | 変更なし（公式のサーバーは Apple silicon（MPS）に対応するが、mam は自動導入しない。この Issue の範囲外） | ElevenLabs |

テスト: `modelmgr/runtime/supporttest/macos_support_test.go`（Apple Silicon・Intel Mac の両方で、64 GiB のユニファイドメモリでも `Supported=false`・`Code=os`・ランタイムごとに別の理由と別のクラウド）、`service/localruntime/realcatalogtest/macos_fallback_test.go`（実カタログ・Mac で `Install`・`Start` が `ErrUnsupported` になり、Audio Director の `UnavailableError` の文が、そのランタイムの理由とそのランタイムのクラウドだけを含む）。`GOOS=darwin GOARCH=arm64` で `modelmgr/...`・`service/localruntime/...`・`service/localspeech/...` のビルドと vet が通ることを確かめた。

### 16.2 GPT-SoVITS（実装しなかった理由）

確かめた一次情報:

- **公式の macOS 対応**: README（タグ `20250606v2pro`）の Tested Environments に Apple silicon（Python 3.9 + PyTorch 2.5.1、Python 3.11 + PyTorch 2.7.0）と CPU がある。macOS の導入は `bash install.sh --device <MPS|CPU>`（conda 前提）で、README は「Mac で GPU を使って学習したモデルは品質が大きく劣るので、一時的に CPU を使う」と書く。**Windows の統合パッケージに当たる macOS の配布物は無い**（Hugging Face `lj1995/GPT-SoVITS-windows-package` は Windows 専用）。
- **`install.sh` の中身**（タグ `20250606v2pro`）: conda で ffmpeg・cmake・make・unzip を入れ、重み・G2PW・NLTK データ・OpenJTalk の辞書を Hugging Face の第三者の `XXXXRT/GPT-SoVITS-Pretrained`（コミット固定でなく `main`）から取得し、`pip install -r requirements.txt` を**その場で**実行する。mam の方式（§3: 固定した部品を展開するだけで、pip・ビルドを走らせない）には載らない。
- **`requirements.txt` の閉包（macOS arm64・Python 3.10）**: `uv pip compile --python-platform aarch64-apple-darwin --python-version 3.10` で 222 個に解決した。各固定版について PyPI の JSON API で「macOS arm64（または `any`）の cp310 の wheel があるか」を調べたところ、**wheel が無い（ソース配布 `.tar.gz` だけの）ものが 8 個**: `pyopenjtalk 0.4.1`・`jieba_fast 0.53`・`jieba 0.42.1`・`distance 0.1.3`・`crcmod 1.7`・`oss2 2.19.1`（`funasr` が要求）・`antlr4-python3-runtime 4.9.3`・`openai-whisper 20250625`。
- **そのうち起動に必須のもの**: 起動の import 連鎖は、`api_v2.py` → `TTS_infer_pack/TTS.py` → `TextPreprocessor.py` が `from text import chinese`（`chinese.py` が `import jieba_fast` を**無条件に**する）と `from text.LangSegmenter import LangSegmenter`（`import jieba`）を実行し、日本語の音素化は `text/japanese.py` の `import pyopenjtalk`（失敗しても例外の分岐で同じ import を再度して落ちる）。つまり **`jieba_fast`（C 拡張）・`jieba`・`pyopenjtalk`（C++ と cmake が要る）が無いと、日本語の声どころか起動できない**。
- **代用の候補を採らなかった理由**: PyPI には macOS の wheel を持つ `pyopenjtalk-plus 0.4.1.post10`（universal2）・`pyopenjtalk-prebuilt 0.3.0`（arm64）があるが、どちらも公式が固定していない第三者のフォークで、GPT-SoVITS が呼ぶ関数（`run_frontend`・`make_label`・`mecab_dict_index`・`update_global_jtalk_with_user_dict`）の互換と日本語の出力の同一性を**実機で確かめられない**。`jieba_fast`（C 拡張）には macOS の wheel が無く、`jieba` で代用すると公式のコードを書き換えることになる。
- 重み: Windows パッケージの重みは Windows 専用ではないが、macOS 用には `lj1995/GPT-SoVITS`（コミット `8f885756…`、ライセンス MIT）か `XXXXRT/GPT-SoVITS-Pretrained` から別に固定し直す必要があり、実機で動かさない固定は意味が薄い。

→ **実装しない**。macOS では上の理由を出し、ElevenLabs を案内する。

### 16.3 MuseTalk（実装しなかった理由）

確かめた一次情報:

- **公式（`TMElyralab/MuseTalk`、コミット `0a89dec4`）**: README は CUDA 前提（CUDA 11.7・`pytorch-cuda=11.8`）で macOS の記載が無い。mam の組み立て（§13.1）は `mmcv==2.0.1` の事前ビルド（OpenMMLab の索引 `…/mmcv/dist/cu118/torch2.0.0/`）を使うが、**macOS 向けの `mmcv 2.0.1` の wheel は OpenMMLab の索引にも PyPI（ソース配布のみ）にも無い**（索引にあるのは 2.0.0 の cp311 universal2 と、x86_64 の cp310 だけ。Python 3.11 は公式の検証範囲外）。さらに mam の `mam_musetalk_engine.py` は `cuda` か `cpu` だけを扱い、MPS を扱わない（実機で確かめられない書き換えになる）。
- **第三者の MLX 版 `mlx-community/MuseTalk-1.5-q4`**（Hugging Face API。コミット `ff57bd4b785b3813f43be689390c3d7494258a35`、2026-06-05）: **実在する**。ライセンス MIT（モデルカードの `license: mit`。上流 MuseTalk を MIT と書く）。作者は MVS Collective（xocialize-code）。UNet の Linear を int4（group_size 64）にし、VAE と Whisper の encoder は fp16。ファイルは `unet.safetensors`（1,325,610,215 B、SHA-256 `7a66b9f6…0f1b`）・`vae.safetensors`（167,335,279 B、`2a767078…0f98`）・`whisper_encoder.safetensors`（16,423,174 B、`c04baf72…36d3`）・`config.json`。fp16・q8 の兄弟もある。モデルカードの数値（34 顔/秒・約 7 GB・上流との差 2.74/255）は**自己申告で mam は確かめていない**。
- **コード `xocialize/musetalk-mlx`**（コミット `c6eb30eb…`）: `pip install -e .`（mlx・numpy・opencv-python・librosa・soundfile。**PyPI には無い**）で入る**ニューラルコアのライブラリ**（`MuseTalkPipeline.from_pretrained_mlx`・`generate_faces`）。README 自身が「顔の検出・DWPose・face-parse・貼り戻しは上流の CPU 前処理を**呼び出し側が**つなぐ」と書く。**サーバー（HTTP）も、動画から顔を検出して貼り戻す処理も無い**。リポジトリに `LICENSE` ファイルが無い（GitHub の API は license なし。`pyproject.toml` と README が MIT と書くだけ）。
- **他の Apple Silicon 向けの実装**: `Lulzx/fast-lipsync`（MIT。Core ML、macOS 15 以降、`uv`・`ffmpeg`・`git` が前提の `setup.sh` が MuseTalk を clone してパッチを当て、重みを取得し、Core ML にコンパイルする）、`dahai80/musetalk-mlx`（Apache-2.0。別のライブラリ fusion-mlx の上の業務層）、`barnent1/musetalk-mac`（ライセンス NOASSERTION。MPS と Mediapipe、FastAPI サーバーだが、公式の API と別形で、自前の `requirements-mac.txt` を pip で入れる）。いずれも、**固定した部品の展開だけで組み立てられる配布物ではなく**、セットアップに pip・clone・コンパイルが要る。
- **公式の API 形式との整合**: MuseTalk に公式の HTTP API は無い（§13.3）。mam の口パクの入口（`mam_musetalk_server.py` の `POST /v1/lipsync`）は mam 自身のラッパーなので、MLX 版を使うなら**新しいエンジンと顔検出・貼り戻しを mam が書く**ことになる（Windows 版では公式の `scripts/inference.py` を流用した部分）。実機（MLX が動く Mac）が無く、出来を確かめられない。

→ **実装しない**。MLX 版は「出典・ライセンス・できること」を上に記録し、画面・MCP の理由の文でも名前を挙げる（`mlx-community/MuseTalk-1.5-q4`、MIT）。実装するなら、(a) 顔の検出・貼り戻しを含む `/v1/lipsync` のエンジンの設計（face detection に mmpose を使わない形）、(b) mlx・opencv などの macOS arm64 の wheel と python-build-standalone の `aarch64-apple-darwin` の固定、(c) 実機での確認、が要る。

### 16.4 未確認・やっていないこと

| 項目 | 状態 |
|---|---|
| **実機の Mac（Apple Silicon）での、GPT-SoVITS・MuseTalk の導入・起動・生成** | **未確認（対応していないので、導入も起動も無い）**。この開発環境は Linux で、macOS・Apple Silicon の GPU が無い。上の調査は一次情報の取得（HTTP）と PyPI の wheel の有無の確認だけで、macOS で何も実行していない |
| macOS で `Evaluate` が断る文が、実際の画面・MCP でそのとおり出ること | 画面・MCP は `Support.Message()` を表示するだけ（既存のテストで確認済み）。実際の Mac の画面は未確認 |
| 上の調査が示す wheel の有無・第三者の実装の状態が将来も同じであること | 保証できない（2026-11-04 時点）。PyPI に `pyopenjtalk` の macOS の wheel が出る、MLX 版に顔検出つきのサーバーが出るなどすれば、再調査して対応を足せる |
| `mlx-community/MuseTalk-1.5-q4` の性能（34 顔/秒）・画質、`xocialize/musetalk-mlx` の LICENSE（ファイルが無い） | 未確認（モデルカードと `pyproject.toml` の自己申告だけ） |

## 17. llama.cpp のランタイム（ローカル LLM。Issue #1008 後半）

LLM のローカル実行用に、llama.cpp の `llama-server`（ggml-org/llama.cpp。MIT）を、GPT-SoVITS・Irodori-TTS・MuseTalk と**同じ基盤**（§3〜§8・§12・§14）で導入・起動・停止する。新しい 2 つ目の仕組みは作らず、足したのは次の 3 つだけ: ①Python を使わない**ネイティブのサーバー**の起動（`launch.program`）、②選んだモデルが起動の引数に値を渡す**固定のテンプレート**（`launch.args` のプレースホルダー）、③ランタイムの用途 `llm`。使う側（プロバイダー `local` への結びつき・VRAM の扱い）は [local-llm.md §10](../local-llm.md)。

### 17.1 実行環境（`data/runtimes/llama-cpp.json`）

| 項目 | 内容 |
|---|---|
| ID・用途 | `llama-cpp`・`llm`。画面の区画名は「ローカル音声・口パク・LLM」、カードの種類ラベルは「文章（LLM）」 |
| 固定する版 | GitHub リリース **`b11960`**（コミット `8642541b255bfb5dc39cd351a178be09013a55d6`。2026-10-28。GitHub の API で、タグの指すコミットとアセットの `digest`〔SHA-256〕を確かめた。リリースのタグは付け替えられるので、固定の本体は SHA-256 とサイズ）。`ggml-org/llama.cpp` の `releases/download/b<数字>/` の 2 種類のファイル名だけを取得できる（`parts.URLAllowed`。ソース・他のプラットフォームのビルド・他のリポジトリ・ミラーは拒否） |
| 対応 | **Windows x64 + NVIDIA の GPU だけ**（他のランタイムと同じ判定 §4。`vram.min_gb` は 6 の推定）。理由の文と代わりの案内（Ollama・LM Studio に接続するか、クラウドの LLM）はカタログの文 |
| 変種 | `nvidia`（CUDA 12.4。Compute Capability 12.0 未満＝GTX 10 〜 RTX 40）と `nvidia50`（CUDA 13.4。12.0 以上＝RTX 50）。GPU の選び方は §4 の `ChooseVariant` そのまま（Irodori・GPT-SoVITS の `nvidia`/`nvidia50` と同じ） |
| 部品 | 各変種に 2 つの zip: 本体のビルド（`llama-b11960-bin-win-cuda-<12.4\|13.4>-x64.zip`。`llama-server.exe` ほか 53 ファイル）と CUDA ランタイムの DLL（`cudart-llama-bin-win-cuda-<12.4\|13.4>-x64.zip`。`cudart`・`cublas`・`cublasLt`）。2 つは同じ名前のファイルを持たない（実物で確認）ので、同じディレクトリに展開する |
| サイズ | `nvidia`: 取得 671,206,851 B（279,763,224 + 391,443,627）、展開後 1,231,292,189 B。`nvidia50`: 取得 588,521,743 B（164,986,387 + 423,535,356）、展開後 766,250,605 B |
| SHA-256 | 本体 12.4 `8d9d8192…d684`、cudart 12.4 `8c79a9b2…e1d6`、本体 13.4 `31c75ea6…ea4a`、cudart 13.4 `738f8c25…be668`（全文はカタログ。実物をダウンロードして `sha256sum` で測り、API の `digest` と一致することを確かめた） |
| ライセンス | llama.cpp 本体は MIT（リポジトリの LICENSE・GitHub のライセンス表示）。CUDA ランタイムの DLL は NVIDIA のライセンスに従う（再配布の条件は確認していない）ので `commercial: conditional` で注記を出す（§17.5） |
| 同梱 | しない。実行時に GitHub の公式リリースから取得する（取得の許可ホストは増やしていない。github.com は ComfyUI・Codex の導入で許可済み） |

**CUDA の世代**: 12.4 版の `ggml-cuda.dll` は `sm_50`〜`sm_90`（Maxwell〜Hopper。RTX 30・40 を含む）と `compute_86/89` を持ち、RTX 50（`sm_120`）には 13.4 版を使う（`ggml-cuda.dll` の中の PTX/SASS の対応を実物の文字列で確かめた。13.4 版は `compute_120a`・`121a`・`86`・`89`）。実機のドライバーが CUDA 13.4 に足りるかは未確認（§17.7）。

**起動**: `llama-server.exe` を**直接**起動する（Python の環境・起動用スクリプトは無い）。コマンドラインは次の 1 つだけ（`catalog/data/runtimes/llama-cpp.json` の `launch.args`）:

```
llama-server.exe -m {model} --host 127.0.0.1 --port {port} --ctx-size {ctx_size} -ngl {ngl} --alias {choice}
                 --parallel 1 --no-webui --no-slots --sleep-idle-seconds 900 --offline
```

- **任意の実行ファイル・任意の引数は受け付けない**。実行ファイルは `launch.program`（環境の下の固定のパス）、引数はこのテンプレートだけで、利用者・MCP・設定が渡せるのは**モデルの選択（カタログのモデルの id）**だけ。値は次の 4 つのプレースホルダーに入る: `{port}`（mam が決めた空きポート。前回のポートを優先）、`{model}`（選んだモデルの GGUF の、環境の下の相対パス）、`{choice}`（モデルの id。`--alias` で API のモデル名になる）、`{ctx_size}`・`{ngl}`（モデルごとの値。`choice.Values`）。値は英数字と `. _ -` だけ（空白・引用符で引数を増やせない）。`port`・`model`・`choice` の名前は組み込みで、モデルの値では上書きできない。`launchargs.Expand` は値の無いプレースホルダーが残ると起動しない（`{model}` のまま起動することはない）。カタログの検証が、`--host 127.0.0.1` と `{port}` を含み `0.0.0.0`・`::` を含まないことを確かめる（§5.2 と同じ）。
- `--offline`・`HF_HUB_OFFLINE=1`: 実行中にモデルを取りに行かない。`--no-webui`: 組み込みの Web 画面を出さない。`--no-slots`: スロットの監視口を出さない。`--parallel 1`: 並列 1（VRAM を抑える）。
- **認証**: 起動のたびに作る乱数のトークンを環境変数 **`LLAMA_API_KEY`**（`launch.token_env`）で渡す。llama-server はこれを API キーとして読み、`/health` 以外のすべての要求に `Authorization: Bearer <トークン>` を求める（実物の `--help` で `--api-key`・env `LLAMA_API_KEY` を確かめた）。ブラウザのページからは送れないので、`127.0.0.1` で待ち受けても他のサイトから叩けない。このトークンは設定の API キーとは別のもので、プロバイダー `local` が管理サーバーを使う間だけ使う（§local-llm.md §10）。
- 準備の確認は `GET /health`（200 になるまで。起動の待ちは最長 900 秒＝27B をディスクから読み込む時間）。
- `--sleep-idle-seconds 900`: 15 分使われないと、llama-server 自身がモデルをメモリ（VRAM）から降ろす（次の呼び出しで読み込み直す）。**LLM の「長時間アイドルでアンロード」**はこれ（§local-llm.md §10.3）。

**`launch.program`**: ネイティブのサーバーのランタイムでは `python`・`entry` の代わりに `program` を使う（相対パス。どちらかだけ。検証が確かめる）。導入は、その実行ファイルが環境にあることを確かめて入れ替える。`parts.Build.site_packages` は wheel の部品があるときだけ必須（llama.cpp には wheel が無い）。

### 17.2 モデルの選択（`data/assets/llama-cpp.json`）

Irodori の選択肢（§14.5）と同じ仕組み（`ResourceSet.Choices`・`Resource.Choice`・`runtimechoices.Service`・`list_local_runtime_models`・`select_local_runtime_model`・モデルのラジオ画面）で、**8 つの選択肢**を載せた。重みは 1 モデル 1 ファイルで、`Resource.Role: "model"`（`launchargs` の `{model}` になる）。選ばれていないモデルの重みは取得しない。標準は Gemma 4 12B-it 純正。

どの GGUF も、`mradermacher/<名前>-GGUF` のリポジトリの**コミット固定の URL**（`https://huggingface.co/<repo>/resolve/<40 桁のコミット>/<ファイル>`）で、**2026-11-03 に Hugging Face の API（`/api/models/<repo>/tree/<コミット>?recursive=1` の `lfs.oid`・`size`）で確かめた**値。ダウンロード応答の `x-linked-etag`・`x-linked-size` が同じことと、先頭 4 バイトが `GGUF` であることも確かめた。どのリポジトリも gated ではない（同意なしで取得できる）。

| id | 名前（画面） | GGUF（コミット） | サイズ | SHA-256 | ライセンス（元の重み／GGUF 変換元のカードの `license`） |
|---|---|---|---|---|---|
| `gemma-4-12b-it`（標準） | Gemma 4 12B-it（純正・Q4_K_M） | `mradermacher/gemma-4-12B-it-GGUF`@`bd28046d` `gemma-4-12B-it.Q4_K_M.gguf` | 7,381,382,592（6.87 GiB） | `2a25529e…4e32` | Apache-2.0（商用可） |
| `gemma-4-12b-it-abliterated` | Gemma 4 12B-it 非検閲（Huihui abliterated・Q4_K_M） | `mradermacher/Huihui-gemma-4-12B-it-abliterated-GGUF`@`f28c4a14` | 7,381,382,720（6.87 GiB） | `46fce9e4…573a` | Apache-2.0（商用可） |
| `qwen3.8-27b` | Qwen3.8-27B（純正・IQ4_XS） | `mradermacher/Qwen3.8-27B-GGUF`@`860659a5` `Qwen3.8-27B.IQ4_XS.gguf` | 15,420,450,528（14.36 GiB） | `791e8775…3d1e` | Apache-2.0（商用可） |
| `qwen3.8-27b-q3km` | Qwen3.8-27B（純正・Q3_K_M 軽量版） | 同上 `Qwen3.8-27B.Q3_K_M.gguf` | 13,500,737,248（12.57 GiB） | `98da2db1…6f9d` | Apache-2.0（商用可） |
| `qwen3.8-27b-abliterated` | Qwen3.8-27B 非検閲（Huihui abliterated・IQ4_XS） | `mradermacher/Huihui-Qwen3.8-27B-abliterated-GGUF`@`c5f7e0aa` | 15,420,450,976（14.36 GiB） | `46cbac11…f6dc` | Apache-2.0（商用可） |
| `qwen3.8-27b-abliterated-q3km` | 同（Q3_K_M 軽量版） | 同上 | 13,500,737,696（12.57 GiB） | `e3bebdd3…09c2` | Apache-2.0（商用可） |
| `deepseek-r1-14b` | DeepSeek-R1-Distill-Qwen-14B（純正・Q4_K_M） | `mradermacher/DeepSeek-R1-Distill-Qwen-14B-GGUF`@`f5a17506` | 8,988,110,432（8.37 GiB） | `e6556ad1…0d80` | MIT（商用可） |
| `deepseek-r1-14b-abliterated-v2` | DeepSeek-R1-Distill-Qwen-14B 非検閲（Huihui abliterated-v2・Q4_K_M） | `mradermacher/DeepSeek-R1-Distill-Qwen-14B-abliterated-v2-GGUF`@`39e97fd1` | 8,988,110,912（8.37 GiB） | `5bc6f9ce…7e70` | **派生側に記載なし → 商用不明（`unknown`）** |

（SHA-256 の全文・各ファイルの全コミットはカタログ。上の表のサイズは Issue 追記の表の GiB と一致した。）

- **ライセンスの根拠**: 元の重み（`google/gemma-4-12B-it`・`huihui-ai/Huihui-gemma-4-12B-it-abliterated`・`Qwen/Qwen3.8-27B`・`huihui-ai/Huihui-Qwen3.8-27B-abliterated`・`deepseek-ai/DeepSeek-R1-Distill-Qwen-14B`）と GGUF 変換元のモデルカードの `license:` を読んだ（Gemma は `apache-2.0` ＋ `license_link: ai.google.dev/gemma/docs/gemma_4_license`、DeepSeek のカードは「コードと重みは MIT。Qwen-2.5 系〔Apache 2.0〕から派生」）。**`huihui-ai/DeepSeek-R1-Distill-Qwen-14B-abliterated-v2` と、その GGUF `mradermacher/…-abliterated-v2-GGUF` のカードには `license` が無い**（`base_model` だけ）ので、**「商用不明」とし、商用可として扱わない**（注記は画面・MCP に出る）。**法的な保証ではない**。
- **載せられなかったモデルは無い**: 6 本の元の重み・GGUF のリポジトリとファイル名はすべて Hugging Face に実在した（Issue の表の名前のまま。代わりの名前は使っていない）。
- **非検閲（abliterated）の 4 つ**は `uncensored: true`（`choice.Choice.Uncensored`）。画面は「成人向け相当（非検閲）」のバッジと注記（安全のための拒否を取り除いた派生で、暴力・性的表現などを拒否せず出力しうる・質や JSON を守る力が落ちうる・使い道は利用者の責任）を出す。MCP の `list_local_runtime_models` にも `uncensored` と `cautions` が出る。
- **同意**: どのモデルも `consent`（利用規約への同意）は無い（ライセンスが Apache-2.0・MIT で、同意を求める条件が無い）。**mam が `accept_terms`・同意を自動で行うことは無い**。モデルを選ぶ操作は利用者の選択（ラジオ・MCP の `select_local_runtime_model`）で、同意とは別。商用不明の注記は表示だけで、選択をふさがない。
- **VRAM の目安**（`vram_min_gb`。mam の推定で `vram_estimate: true`・実機では未確認）: Gemma 12B 9 GB・DeepSeek 14B 11 GB・Qwen3.8-27B の Q3_K_M 16 GB・IQ4_XS 18 GB（重み + 文脈 16384 の KV キャッシュ + 作業領域）。**Qwen 27B は Q4_K_M（15.66 GiB）だと 16 GB に収まらないので載せず、IQ4_XS を標準・Q3_K_M を軽量版にした**。`ngl` は Gemma・DeepSeek が `all`、Qwen が `auto`（収まらない分は llama.cpp が CPU に置く）。`ctx_size` は全モデル 16384（mam の企画・脚本の入力は長く、既定の小さい値だと途中が切れる）。
- **thinking**: Gemma・Qwen は考える過程があり、JSON を返す役割では mam が `chat_template_kwargs: {"enable_thinking": false}` を送ってオフにする（前半 [local-llm.md §3](../local-llm.md) の実装。`--jinja` は llama-server の既定で有効）。DeepSeek R1 Distill はオフにできないので、`<think>…</think>` を取り除いてから JSON を読む（同 §3.3）。これらが**このランタイム（偽 llama-server）でも効く**ことは `llamaservertest` で確かめた（§17.6）。

### 17.3 導入・起動・停止・状態

§5・§6 と同じ。`Service.Install` は GPU から変種（`nvidia`/`nvidia50`）を選び、本体と cudart の zip を取得・展開（`extract.Zip`。展開後サイズで打ち切り・`ErrExists` で同名ファイルを拒否）して `env\` と入れ替え、続けて**選んだモデル 1 つの重み**を取得する。モデルを変える（`select_local_runtime_model`・画面）とサーバーを止め、次の起動から新しいモデルを読む（重みが無ければ「重みを導入」）。`Remove`・`Stop`・`SetAutoStart`・`Watch`・ログ・排他は他のランタイムと同じ。**自動起動は既定でオフ**（最初の LLM の呼び出しで起動する。§local-llm.md §10）。

### 17.4 macOS（Apple Silicon）は「対応外」にした（理由）

公式のリリースには `llama-b11960-bin-macos-arm64.tar.gz`（Metal。16,465,074 B。SHA-256 `58e5a5e1…1e3c`）がある。ランタイム定義の `os_reasons.darwin` に理由を書き、画面・MCP・`Service.Install` は「macOS（Apple Silicon）では llama.cpp の自動導入に対応していません。公式のリリースには macOS arm64（Metal）のビルド済みバイナリがありますが、mam はこの開発環境で macOS の実機を使えず…」と代わりの案内（Ollama・LM Studio への接続、クラウドの LLM）を返す。**理由**: ①`.tar.gz` の展開と `llama-server` の実行権限・Gatekeeper（未署名の実行ファイル）・Metal の動作は、実機なしで確かめられない。②§10 と同じく Apple Silicon の自動導入はオーナーの判断（別 Issue）。③ GPU の判定（`gpu.Report` の Apple Silicon のユニファイドメモリ）と VRAM の目安が、Windows + NVIDIA の規則とは別に要る。**ランタイム定義・テスト（非対応の理由・案内・`Install` が断ること）は他のランタイムと同じ水準で入れた**。Apple Silicon の Mac では、Ollama または LM Studio を入れて設定の「ローカル LLM サーバー」で接続する（local-llm.md §8）。

### 17.5 表示（画面・MCP・docs）

- **画面**: モデルマネージャーの区画に、llama.cpp のカード（種類「文章（LLM）」）。**#1104 のあと、置き場所は分類 `local-llm` の区画「ローカルで動かすサーバー」（`#mm-llm-runtimes`）で、`local-runtimes`（声・口パク）には出ない**（[../../components/model-manager-screen.md](../../components/model-manager-screen.md)「ローカル LLM の統合（#1104）」。状態: 実装済み。`localruntime.View.Purpose` が `llm` のカードは `GET /model-manager/runtimes?purpose=llm` が返し、`purpose` なしの `local-runtimes` には出ない）。状態・導入・起動・停止・自動起動・削除・重み一覧（出典・ライセンス・商用利用）・**使うモデルの選択**（名前・説明・取得量・言語・VRAM の目安と GPU との比較・ライセンス・商用利用・注意）。**`unknown`（商用不明）の重みは琥珀色の枠**（§12.5）。非検閲のモデルは「成人向け相当（非検閲）」のバッジと注記。
- **MCP**: `list_local_runtimes`・`get_local_runtime_status`・`install_local_runtime`・`install_local_runtime_weights`・`start_local_runtime`・`stop_local_runtime`・`list_local_runtime_models`・`select_local_runtime_model` が、**既存のまま**このランタイムを扱う（カタログのランタイム一覧に載る）。
- **docs**: [THIRD_PARTY_NOTICES.md](../../../THIRD_PARTY_NOTICES.md) の「利用者が取得する別プログラム」に llama.cpp と 8 つの重みの一覧（出典・ライセンス・商用利用）。README の表と手順。

### 17.6 テスト

- `catalog/llamacpptest`: ピン（部品の SHA-256・サイズ・URL がコミット固定でないリリースのアセットだけ・展開後サイズの合計）、固定の 1 つのコマンドライン（`--host 127.0.0.1`・`{port}`・認証）、8 つのモデル（GGUF の URL がコミット固定・SHA-256・サイズ・Qwen は Q4_K_M を載せない・`ngl`/`ctx_size`）、ライセンス（DeepSeek の abliterated-v2 だけ `unknown`・商用不明の注記）、非検閲 4 つの `uncensored`、同意が無いこと、検証が拒否するもの（ミラー・http・ブランチ名・任意の引数を足す値・組み込みの名前の上書き）。
- `catalog/launchargs`・`catalog/choice`・`catalog/parts`・`modelmgr/runtime/choicetest`: プレースホルダーの展開（値が無ければ起動しない・組み込みが勝つ・値に空白があれば拒否）、ネイティブのプログラムの起動（引数は固定の並びで、選んだモデルのファイルと値）、llama.cpp の URL の許可と他の拒否、wheel が無いビルドの `site_packages`。
- `service/llamaserver/llamaservertest`（**偽の llama-server**: テストバイナリ自身を `llama-server.exe` として導入し、llama-server と同じ引数を読んで、知らない引数は断る）: 導入（本体 + cudart の zip を取得・展開・SHA-256 検証・選んだモデルの重みだけ）、RTX 50 世代は CUDA 13.4 版、起動（固定のコマンドライン・トークンなしの要求は 401・`/health` は通る）、停止、モデルの切り替え（停止して次の起動で別のファイル）、重みの SHA-256 不一致（取り直す／起動しない）、非対応の OS・GPU の理由、プロバイダー `local` からの呼び出し（管理サーバーへ結びつく・トークンを付ける・ベース URL の設定は使われない）、thinking（Gemma・Qwen は `enable_thinking=false` が効く・DeepSeek は `<think>` を除去して JSON を読む）、ComfyUI が生成中なら待つ・空いていれば `/free`（§local-llm.md §10）。
- 実物の Windows ＋ NVIDIA での導入・起動・推論は **device_only**（§17.7）。

### 17.7 未確認

| 項目 | 状態 |
|---|---|
| **実物の Windows ＋ NVIDIA での導入（本体 + cudart の zip の取得・展開）・`llama-server.exe` の起動・推論** | **未確認（device_only）**。この開発環境は Linux で GPU が無い。部品の SHA-256・サイズは実物のファイルで測り、API の `digest` と一致することを確かめた。引数は、同じリリースの Linux 版 `llama-server --help` で、使う引数（`-m`・`--host`・`--port`・`--ctx-size`・`-ngl`・`--alias`・`--parallel`・`--no-webui`・`--no-slots`・`--sleep-idle-seconds`・`--offline`・`LLAMA_API_KEY`）が実在することを確かめた。Windows 版の実行は未実行 |
| 12.4 / 13.4 の CUDA のビルドが、利用者のドライバーで動くか（13.4 は新しいドライバーが要る） | **未確認**。動かないドライバーは、起動の失敗（ログの末尾つき）として表示される |
| 各モデルの VRAM・速さ・出力の質・JSON の守り方、ComfyUI との同居（Gemma 12B は同居しやすいが、27B は同居できない） | **未確認**。`vram_estimate: true`。数値は出さない |
| `--sleep-idle-seconds` で VRAM が実際に空くこと | **未確認**（llama.cpp のヘルプの記述に基づく） |
| macOS（Apple Silicon）の自動導入 | **対応外**（§17.4） |
| 部品・重みの URL が将来も残ること | 保証できない。変われば SHA-256 の不一致・404 で失敗する |

## 18. GPT-SoVITS の fine-tune（学習。Issue #936）

手持ちの音声（1 分ほど）から、キャラクターの声を GPT-SoVITS で学習し、**画面と MCP から学習を始めて、できた声を声の合成に使う**。ゼロショット（参照音声だけ）はそのまま残り、学習した声があるキャラクターだけが重みを切り替える。実装は `backend/internal/service/voicetrain`（学習のサービス）、`service/voicerefsvc/finetune.go`（結び付け）、`modelmgr/catalog/data/overlays/mam_gptsovits_train.py`（学習の駆動スクリプト）、`mam_gptsovits_server.py`（重みの切り替え）、`modelmgr/runtime`（`Installer.Tool`）、`service/localruntime`（`BeginTool`）。

### 18.1 一次情報の調査（GPT-SoVITS のタグ `20250606v2pro`。2026-11-04 に実際に取得して読んだ）

固定した統合パッケージ（§3）が使うコードのタグの `webui.py`・`config.py`・各スクリプト・`README.md`・`api_v2.py` と、統合パッケージの 7z のヘッダー（`Range` で読んだ一覧。58,007 ファイル）を読んで確かめた。**実行はしていない**。

| 項目 | 読んで分かったこと |
|---|---|
| 公式の手順 | `webui.py` の「1A・1B・1C のデータ整形」→「1Aa SoVITS（S2）の学習」→「1Ab GPT（S1）の学習」。データの整形の前に、音声の分割（`tools/slice_audio.py`）・ノイズ除去・文字起こし（ASR。`tools/asr/*`）がある。README は「約 1 分の学習データで声の近さが上がる（Few-shot TTS）」とだけ書く |
| データ整形 | 入力は**リストファイル**（1 行 `ファイル名\|話者名\|言語\|書き起こし`）と音声のフォルダ。`1-get-text.py`（音素と、中国語だけの BERT 特徴 → `2-name2text.txt`）、`2-get-hubert-wav32k.py`（32 kHz の音声と SSL 特徴）、`3-get-semantic.py`（事前学習の S2G の重みでセマンティックトークン → `6-name2semantic.tsv`）。**設定は環境変数**（`inp_text`・`inp_wav_dir`・`exp_name`・`opt_dir`・`bert_pretrained_dir`・`cnhubert_base_dir`・`pretrained_s2G`・`s2config_path`・`is_half`・`_CUDA_VISIBLE_DEVICES`・`i_part`・`all_parts`）。v2Pro だけ `2-get-sv.py` が足される（mam が学習するのは v2。§18.3） |
| 学習 | S2 は `s2.json` を読んで `batch_size`・`epochs`・`text_low_lr_rate`・`pretrained_s2G`・**`pretrained_s2D`（判別器。学習だけが読む）**・`save_weight_dir`・`exp_dir` などを書いた一時の設定ファイルで `s2_train.py --config` を起動する。S1 は `s1longer-v2.yaml` に `batch_size`・`epochs`・`pretrained_s1`・`half_weights_save_dir`・`train_semantic_path`・`train_phoneme_path`・`output_dir` を書いて `s1_train.py --config_file` を起動する。fp16 が使えない GPU は、精度を 32 にしてバッチを半分にする |
| 既定値・上限 | SoVITS は既定 8 エポック（上限 25）、GPT は既定 15（上限 50）、バッチは「GPU のビデオメモリ（GB）の半分」（GPU が無ければメモリの 1/4）。クリップは 0.6〜54 秒だけが学習に入る（`data_utils.py`） |
| VRAM・所要時間 | **公式は数値を示していない**（README にも `webui.py` にも無い）。バッチの既定が VRAM の GB の半分、という手がかりだけ。mam は数値を出さない（§18.9） |
| 出力される重み | S2: `SoVITS_weights_v2/<名前>_e<エポック>_s<ステップ>.pth`、S1: `GPT_weights_v2/<名前>-e<エポック>.ckpt`（半精度に変換した推論用の重み。`process_ckpt.savee`・`s1_train.py`）。チェックポイントは `logs/<名前>/` に別に残る |
| 進み具合 | S2 は `logs/<名前>/train.log`（Python の `logging`）に `====> Epoch: N` を書き、標準出力には `start training from epoch` と `training done` だけ。S1 は PyTorch Lightning のログ（進捗バー）で、エポックの行の形は固定されていない → **mam は公式の出力を読まず、駆動スクリプトが 2 秒ごとに数える**（§18.3） |
| 重みの切り替え（`api_v2.py`） | `GET /set_gpt_weights?weights_path=…`・`GET /set_sovits_weights?weights_path=…`。**任意のパスを読み込ませる**ので、mam の起動用スクリプトは許可しない（§5.2 は変えない）。代わりに起動用スクリプトが `POST /mam/voice` を自分で答える（§18.5）。`TTS.init_*_weights` は読み込んだパスを設定ファイルへ書き戻すので、起動用スクリプトは設定ファイルの**コピー**を使う |
| 文字起こし（ASR）と分割 | 統合パッケージには `tools/asr/models/` の**空のフォルダ**（FunASR）しか無く、`fasterwhisper_asr.py` は `HF_ENDPOINT=https://hf-mirror.com`（第三者のミラー）から実行時に重みを取る。mam の「実行中に取得しない」方針（§1・§5.2）に合わないので、**mam は ASR も自動分割もしない**。利用者が書き起こしつきの短い録音（クリップ）を用意する（§18.2） |

### 18.2 学習に使うデータ（クリップ）

- 1 つのクリップは、**話している人が 1 人だけの録音と、その書き起こし**。`.wav/.mp3/.flac/.ogg/.m4a`（参照音声と同じ形式・署名の検査）、10 MB まで、**2〜30 秒**（ffprobe で測る。ffprobe が無いときはクリップを受け付けない: 長さを検査できない）。書き起こしは 1〜500 文字（`|`・制御文字を含められない: リストファイルの区切りだから）。言語は参照音声の言語。
- 合計は **60 秒以上（下限。公式 README の「約 1 分」）・30 分まで（上限）・200 個まで**。上限は mam の決めた値で、公式の制限ではない（公式のスクリプトは 0.6〜54 秒のクリップを読むだけ）。足りない・多すぎるときは、**学習を送らずに**理由（`problems`）を画面と MCP に出し、始めようとすると `invalid_input`（#1010 の共通エラー）で断る。
- 置き場所: `<assets>/voicetrain/_draft/<project-|series-><id>/<character id>/`（クリップのファイルと `index.json`）。`_draft` は学習済みの声の ID の形ではないので、合成のサーバーが読むことも、不要な重みの掃除の対象になることも無い。学習が始まると、クリップは**作業ディレクトリへコピー**する（学習の最中にクリップを消しても、学習は変わらない）。
- 前提: そのキャラクターに**参照音声がある**こと（合成は学習した声でも、参照音声を読む。§18.6）。

### 18.3 学習のジョブ

| 項目 | 内容 |
|---|---|
| 学習する版 | **v2 だけ**（`GPT_SoVITS/pretrained_models/gsv-v2final-pretrained/` の事前学習。§12.6 の重みと同じ）。v2Pro・v3・v4 は学習しない（mam が起動する既定が v2 で、合成側も v2 の重みを前提にしているため。ほかの版は実機で確かめてから） |
| 呼び方 | **任意のコマンド・スクリプト・パスを外から指定できない固定の組み立て**。`Installer.Tool(script, args, env)` は、カタログ（`runtimes/gptsovits.json`）の **`tool_files`**（mam が同梱する `mam_gptsovits_train.py` だけ）の `dest` 以外を `ErrNotATool` で断り、サーバーと同じ `python -I -u -X utf8 <script> <step> <job.json>`（作業ディレクトリは `env\`）、同じオフライン設定・キャッシュ・一時ファイルの場所で起動する。`<step>` は `text`・`hubert`・`semantic`・`sovits`・`gpt` の 5 つだけで、駆動スクリプトの表（`SCRIPTS`）が公式の 5 つのスクリプトに対応づける（それ以外は起動しない）。`job.json` は mam が書く（パスは作業ディレクトリの中だけ。`..`・シンボリックリンクで外へ出るパス・`exp_name` の不正・範囲外のエポック数は駆動スクリプトが終了コード 3 で断る）。MCP・画面の入力はキャラクターの ID・エポック数・クリップだけ |
| 駆動スクリプト | `mam_gptsovits_train.py`（job.json の検査・5 つのステップの表・環境変数と設定ファイルは、行数の上限のため同じ `tool_files` の `mam_gptsovits_train_job.py` に分けた。`Installer.Tool` は起動のたびに**すべての** tool file を書き直し、起動できるのは tool file の `dest` だけ）。標準ライブラリだけ。GPT の設定の読み書きだけ PyYAML を使う: パッケージにある）。公式の `webui.py` と同じ環境変数・一時設定を作って公式のスクリプトを起動し、**2 秒ごとに**進み具合を数えて標準出力に `MAM_TRAIN {"step","done","total"}` の行を出す（hubert: 作った特徴ファイルの数、sovits: `train.log` の `====> Epoch: N` の最大の N、gpt: 保存された重みの最大のエポック）。公式の出力は標準エラーへ。`tool_files` は `extra_files` と違い `Status` が検査しない（学習が入る前に導入した環境が `broken` にならない）。起動のたびに書き直す |
| 段階と進み具合 | 5 つの段階（text 0〜5 %・hubert 5〜15 %・semantic 15〜20 %・sovits 20〜60 %・gpt 60〜98 %。割合は**見た目だけの mam の値**で、時間の割合ではない）。**エポック n/m・経過・残り時間**を、ジョブ一覧（`Progress.Text`: 「SoVITS の学習 エポック 3 / 8（4 / 5 段階）・経過 15分0秒・この段階の残り約 20分0秒」）と `get_job` の `progress`（`live.View`: `node`＝段階、`step`/`max_step`＝エポック、`percent`、`elapsed_seconds`、`eta_seconds`。#1002 と同じ形）に出す。**残り時間はその段階のエポックの平均から**で、最初のエポックが終わるまでは出さず、`eta_unavailable` に理由を書く。次の段階の時間は分からないので、残りは「この段階の」だけ（全体の残りは言わない） |
| ジョブの種類 | **メモリ上の追跡（`internal/worktrack`。新しい種類 `voice_train`）**。理由: ①`job` テーブルのジョブはキューの再試行・再開（`attempts`）の仕組みに乗るが、学習は**再起動をまたいで再開しない**ので、再開の無い仕組みが要る（#1019 の動画の書き出し・音声の作成と同じ判断）、②テーブルに種類を足すと、CHECK 制約のマイグレーションと全画面の一覧が要るが、得るものが無い、③進み具合は子プロセスと同じ寿命の記録で足りる。ジョブ一覧には `ScreenProjectWorld`（Project の World State）・`ScreenSeries`（Series のページ）への「開く」つきで出る。MCP の `get_job` は `train_character_voice` の種類（`InMemory`: mam を再起動すると、その後は履歴の終わりだけで答える） |
| **再開しない（決定）** | 学習は数十分かかるが、**mam の再起動・異常終了をまたいだ、チェックポイントからの再開はしない**。公式の `s1_train.py` は `logs/` の最新のチェックポイントから再開する作りだが、①mam は作業ディレクトリごと消して、**部分的な出力を残さない**方針、②再起動後に「どの学習がどこまで進んだか」を信頼して引き継ぐには、データ・設定の同一性の確認と再開の UI が要る、③mam の再起動は稀、のため。再起動後に残った作業ディレクトリは、次の学習の前の掃除（`voicerefsvc.CleanFinetunes` は学習済みの声と `.tmp-` の途中のコピー。作業ディレクトリは `localruntime.Service.BeginTool` が、ランタイムを確保した直後（ほかの学習が動いていないことが確かな時点）に `localai\<id>\train\` の直下を消す。リンクはたどらず、`train` 自体がリンクならリンクだけを消す）で消える |
| 中止 | `Cancel(owner, characterID)`（画面「学習を中止」・MCP `cancel_character_voice_training`）。動いている段階のプロセスのグループを、まず穏やかに、次に強制で止め（`proc.Piped.Stop`。Windows は Job Object、Linux はプロセスグループ）、作業ディレクトリと、書きかけの重み（`.tmp-<id>/`）を消す。**部分的な出力は残らず、学習済みの声は付かない**。状態は `cancelled`。mam の終了（`Close`）も同じに止めて片付けを待つ |
| 失敗 | 段階のプロセスが 0 以外で終わると、標準エラーの末尾から `training_failed`（#1010 の共通エラー）にする。`out of memory` は原因の文「学習中に GPU のメモリが足りなくなりました」、`No space left` は `disk_full` にする。作業ディレクトリは消し、重みは残さない |
| 同時実行 | 学習は 1 つずつ（GPU は 1 つ）。別のキャラクターの学習が動いていれば `conflict`。学習の間、ランタイムは `BeginTool` が確保し、**合成のサーバーは止められ**、導入・重みの導入・起動・削除は `ErrTraining`（学習のファイルを入れ替えない。画面・MCP は `conflict`「声の学習が動いています。終わってから、または中止してからやり直してください」、音声合成の案内は同じ文の `process` 種別）。学習が終わる・中止・失敗で `End` が返す |
| **VRAM（ComfyUI）** | **学習を始めるとき**、`gpufree.Guard`（#1015。声のサーバーが起動する前に使う）に通す: 同梱の ComfyUI が**生成中なら始めない**（`conflict`。「同梱の ComfyUI が生成中で、GPU が空いていません」）、動いていても生成していなければモデルを解放（`POST /free`）させてから始める。**逆向き（学習の最中に ComfyUI が生成を始める）は、この Issue では止めていない**（§18.9。Irodori-TTS の声と同じ範囲） |
| 学習の前の検査 | データ量（§18.2）、参照音声、ランタイム（`BeginTool` が対応環境・導入済み・導入中でない・重みがそろう〔学習だけが読む判別器 `s2D2333k.pth` も。§18.4〕を確かめ、理由つきで断る）、**空き容量**: 作業ディレクトリのあるディスクに「6 GiB ＋ 録音 1 秒あたり 200 KiB ＋ クリップのサイズ」（mam の控えめな見積もり。チェックポイントは判別器・オプティマイザを含み大きい。**実測していない**）、重みを残すディスクに 512 MiB。足りなければ**何も送らず**、`disk_full`（「学習に約 X 必要で、空きは Y です。場所: …」）で断る。エポック数が範囲外なら `invalid_input` |
| バッチ | `GPU のビデオメモリ（GB）の半分`（1〜32）。S2・S1 とも。公式の既定と同じ考え方（`webui.py`）。fp16 が使えない GPU は、駆動スクリプトが公式と同じに半分にする |

### 18.4 重み（判別器）

学習の S2 は、事前学習の**判別器** `s2D2333k.pth` を読む（推論は読まない）。固定カタログ（`data/assets/gpt-sovits.json`）に、`lj1995/GPT-SoVITS` のコミット `8f885756…`（既存の v2 の重みと同じ）の `gsv-v2final-pretrained/s2D2333k.pth` を **`required: false`** で足した。サイズ 93,534,164 B・SHA-256 `8ae7fe8dd8c8f2e718de359e00edac88b0c71ab2fd10b07ad4cc45070eb8a836` は、Hugging Face の API の `lfs.oid` と、**実際にダウンロードしたバイトの SHA-256** の一致で確かめた（統合パッケージの 7z の一覧にも同じファイルがある）。ライセンスは既存の `mit-gpt-sovits`。`required: false` なので**声を作る側（サーバーの起動）は今まで通り要らず**、学習を始めるときだけ `NotReadyNamed` がそろっていることを確かめる（無い・壊れているなら、`*WeightsError`＝「重みを導入してください」）。取得は既存の「重みの導入」と同じ（固定 URL・SHA-256 検証・再開可）。

### 18.5 学習した声の結び付けと合成（`voiceref` の拡張）

| 項目 | 内容 |
|---|---|
| 保存 | `character.voice_reference`（JSON。**DB の列・マイグレーションは足さない**）に `finetune`（`voiceref.Finetune`: `id`・2 つの重みのサイズ・`version`・エポック数・クリップ数・データの秒数・`trained_at`）を足した。重みは**ユーザーデータ側**（`<assets>/voicetrain/<id>/gpt.ckpt`・`sovits.pth`）に置く。`id` は `domain.NewID` で、ディレクトリ名として安全な形だけが通る（`ValidFinetuneID`） |
| 書き込み | 学習が終わると、重みを `.tmp-<id>/` に置いてから `voicetrain/<id>/` へ移し（2 つとも揃うまで本来の名前では存在しない）、`voicerefsvc.BindFinetune` がキャラクターの行に付ける。**Series の正本**に付けると正本に新しいバージョンができる（参照音声と同じ）。以前の学習済みの声は、ほかの行が使っていなければ重みごと消える。**新しい参照音声を置いても、学習した声は残る**（同じ話者の別の録音）。参照音声を消すと学習した声も消える |
| 継承 | **正本→Episode は参照音声と同じコピー**: `POST /series/:id/episodes`・`POST /series` は `voice_reference` の JSON をそのままコピーするので、**以後に作る Episode は学習した声を引き継ぐ**。作成済みの Episode は変わらない（参照音声と同じ）。Episode のコピー・Branch のコピーは**同じ重みのファイルを共有**し、最後の行が外れるまで重みは消えない（`voicerefrepo.FinetuneInUse`） |
| 合成 | `voicerefsvc.ForSpeaker` が `Voice.Trained`（重みの `id`）を返し、`localspeech.Voices` が `localvoice.Voice.Trained` に載せる。`gptsovits.Client` は、**サーバーに読み込まれている声と違うときだけ**、`POST /mam/voice {"voice": "<id>"}`（トークンつき。ゼロショットの話者は `""`）を送ってから `POST /tts` する（話者が替わるたびに重みを読み直す。同じ話者の連続した行では読み込まない。サーバーの再起動はトークンが変わるので検知して読み直す）。**学習した重みがあるキャラクターは、必ずその重みで話す** |
| 起動用スクリプト | `mam_gptsovits_server.py` の許可する要求に **`POST /mam/voice`**（トークン必須）を足した。`voice` は `^[A-Za-z0-9-]{1,64}$` で、環境変数 `MAM_VOICE_WEIGHTS_DIR`（mam が起動時に渡す `<assets>/voicetrain`。`localruntime.Deps.Env`）の直下のディレクトリの `gpt.ckpt`・`sovits.pth`（`realpath` がそのディレクトリの中であること）だけを読む。**リクエストはパスを持てない**。`""` は起動時の重みに戻す。`/set_gpt_weights`・`/set_sovits_weights`・`/control` は引き続き 404。読み込みの失敗（壊れたファイル）は 500、ファイル無しは 404 |
| **壊れている・無いとき（決定）** | **ゼロショットに黙って戻さず、止める**。重みのファイルが無い・サイズが書いたときと違うキャラクターは `voicerefsvc.ErrNoWeights` → その話者は**声が使えない話者**として実行の前に断る（`localvoice.ErrTrainedVoice`。文: 「学習した声のファイルが無いか壊れている話者がいます: 「ユイ」。Character の「学習した声」を削除してゼロショットの声に戻すか、学習し直してください（別の声に黙って替えることはしません）」）。理由: 学習した声を期待している利用者に、別の声（ゼロショット）で作った声を黙って渡すと、シリーズの途中で声が変わる事故になる。**戻す手段は明示**: 「学習した声を削除」（画面・`delete_character_trained_voice`。ファイルが無くても削除できる）。サーバーが重みを読み込めなかった場合（`/mam/voice` の失敗）も、その行を別の声で作らず `UnavailableError`（理由つき）で止める |
| バンドル | **学習した重みは書き出し（プロジェクトバンドル）に入れない**。書き出し（`bundlezip.dropTrainedVoices`）は `finetune` をデータから外し、参照音声の録音だけを入れる。取り込み（`voiceref.Reference.Validate`）は `finetune` を持つ行を拒否する。理由: ①重みは約 250 MB の大きなファイルで、声の持ち主の同意の範囲を超えて渡る恐れがある、②重みはこの PC の GPT-SoVITS の版（v2）に結び付き、取り込む先で動く保証が無い、③バンドルの検証（相対パス・サイズ）の枠に入らない。取り込んだ先では、参照音声のゼロショットの声になる（学習し直せる） |
| バックアップ | 重みはデータディレクトリ（`<assets>/voicetrain/`）にあり、データの移行はアセットのフォルダごとコピーすれば付いていく（`character.voice_reference` の JSON と同じ場所） |

### 18.6 画面と MCP

- **画面**: Character の参照音声のフォーム（Series 正本の Character の欄・World State の Character の欄）の下に「声の学習（fine-tune）」の欄（`web/organisms/voicetrainpanel`）を足した。ページ読み込み時に `hx-trigger="load"` で自分を読み、参照音声を保存・削除すると（`HX-Trigger: voice-reference-changed`）読み直す。中身: 状態のバッジ（学習中／学習済み／未学習）、**声のコピーには本人の同意が要るという注意書き**（`data-testid="voice-train-consent-notice"`。参照音声のフォームと同じ趣旨。mam は同意を確かめない）、学習に使う音声の一覧（長さ・書き起こし・削除）と合計・限度・足りない理由、音声の追加（ファイル＋書き起こし）、エポック数と「学習を始める」（確認ダイアログで、時間・GPU・再起動でやり直し・本人の同意を言う）、実行中は `<progress>` と段階・エポック・経過・残り時間（3 秒ごとに自分を読み直す）と「学習を中止」、失敗は共通のエラーの箱（`errorbox`）、学習済みの声の情報と「学習した声を削除（ゼロショットに戻す）」。**見込み時間は、始める前には出せない**（実機で測っていない）と画面に書き、最初のエポックが終わったあとから残り時間を出す。
- ルート（Series 正本は `/series/:id/characters/:characterId/voice-training`、Project は `/projects/:id/characters/:characterId/voice-training`。どちらも同じ形）: `GET`（欄）、`POST`（開始。`epochs_sovits`・`epochs_gpt`）、`POST /cancel`、`POST /clips`（multipart `file`・`text`。アップロードのルート）、`DELETE /clips`・`DELETE /clips/:clipId`、`DELETE /voice`。応答はどれも欄だけ。
- **MCP**（[mcp-tools-world-notes.md](../../api/mcp-tools-world-notes.md)）: `get_character_voice_training`（状態・データ・限度・学習済み・いまの学習）、`start_character_voice_training`（`job_id` を返す。`get_job` で進み具合）、`cancel_character_voice_training`、`delete_character_voice_training_clips`、`delete_character_trained_voice`、アップロードの `purpose=character_voice_training_clip`。**任意のパス・コマンド・重みのファイル名を受けない**（入力はキャラクターの ID・エポック数・`clip_id`・クリップのファイル）。説明に同意の注意を入れた（エージェントが同意を代わりに確認したことにしてはならない）。

### 18.7 配線

`app/services.go` が `voicetrainwire.New`（`service/voicetrain/voicetrainwire`）で学習のサービスを作り、`Services.VoiceTrain` に置く。`localruntime.Deps.Env` が `MAM_VOICE_WEIGHTS_DIR` を GPT-SoVITS の起動に足す。ジョブ一覧は `worktrack.Source` に学習のサービスを加える。起動時に `voicerefsvc.CleanFinetunes`（どの行も使わない重みと、途中のコピーを消す）を呼び、終了時に学習を止めて片付けを待つ（ランタイムを閉じる前）。

### 18.8 テスト

- `service/voicetrain`: 偽の学習プロセス（テストバイナリ自身が、駆動スクリプトの代わりに進捗行と重みを書く）で、遷移（running→succeeded）・進捗（エポック・時間・割合）・**中止**（子プロセスが止まり、作業ディレクトリ・重みが残らない）・**失敗**（OOM・ディスク不足の文）・**ディスク不足の前検査**・データ量の下限・上限・参照音声が無い・同時実行・ComfyUI の生成中・ランタイムの断り（未導入・非対応・重みなし・導入中）、`-race`。
- `service/voicerefsvc`: 結び付け（サイズ・ID の検査）・継承（Episode のコピー）・**重みが壊れている・無いときに止まる**・削除・置き換えで古い重みが消える・掃除・**バンドル（書き出しは重みを外し、取り込んだ先はゼロショット）**。
- `agent/audiodirector/gptsovits`（偽の `api_v2`＋守り）: 学習した声を 1 度だけ読み込む・話者の切り替え・サーバーの再起動で読み直す・読み込めないときは行を止める。`service/localspeech`（学習した声の選択・壊れたときの文）。
- `modelmgr/catalog/gptsovitstest`: 起動用スクリプトと駆動スクリプトの Python のテスト（偽のパッケージ。`/mam/voice` が許可した 1 つのディレクトリの直下だけを読むこと・パスや `..`・シンボリックリンク・不正な ID の拒否、駆動スクリプトが 5 つの固定のスクリプトだけを起動し、範囲外のパス・名前・数を断ること）、`tool_files` の検査。`modelmgr/runtime`（`Installer.Tool`）、`service/localruntime`（`BeginTool`・`Env`）、`service/joblist`・`web/handler/jobs`（ジョブ一覧の行と「開く」）、`mcp/tools/voicetraining`（`get_job` の遷移・進捗・中止・失敗）、`router/voiceroutes`（画面のルート）。

### 18.9 未確認・実装しなかったこと

| 項目 | 状態 |
|---|---|
| **実際の学習**（実物の GPT-SoVITS・GPU・実重みでの 5 つの段階の完走）、**学習した声の質**（ゼロショットより声が近くなるか。日本語の聞き比べ） | **未確認**（`device_only`）。この開発環境は Linux で GPU が無く、実物の統合パッケージ（約 14 GB）も展開していない。偽のプロセス・偽のサーバー・偽のパッケージ（Python のテスト）まで |
| 公式のスクリプトが、駆動スクリプトの作る環境変数・一時設定で動くこと、進み具合の数え方（S2 の `train.log` の `====> Epoch: N`、S1 の保存される重みのエポック）が実物で合うこと、重みの保存先（駆動スクリプトが `save_weight_dir`・`half_weights_save_dir` を作業ディレクトリへ向ける）が効くこと | **未確認**。ソース（タグ `20250606v2pro`）を読んで合わせた。`if_save_every_weights`・`save_every_epoch` の効き方を含む |
| **学習の VRAM・所要時間・ディスクの使用量**（空き容量の見積もり 6 GiB ＋ 録音 1 秒あたり 200 KiB は mam の控えめな見積もり。バッチ＝VRAM の GB の半分は公式の既定の考え方） | **未確認**。公式に数値が無く、実測していない。画面は「見込み時間は、始める前には出せません」と言い、最初のエポックの後に残り時間を出す |
| 12 GB 以下の GPU で、バッチ・学習が収まるか。fp16 が使えない GPU の挙動 | **未確認** |
| `POST /mam/voice` の `init_vits_weights`／`init_t2s_weights` の実物での切り替え（学習した重みの読み込みと、`None` で起動時の重みに戻ること。設定ファイルのコピーを使うこと） | **未確認**（偽の `api_v2`・Python のテスト）。`TTS.py`（タグ `20250606v2pro`）を読んで書いた |
| 学習を始めた後の ComfyUI の生成（**逆向きの排他**） | **実装していない**。`gpufree.Guard` は「声（学習）が始まる前に ComfyUI の生成中を断る・空きを作る」だけで、学習の最中に ComfyUI が生成を始めるのは止めない（Irodori-TTS の声と同じ範囲）。止めるには、ComfyUI 側の生成の入口に学習中を断るフックが要る（別 Issue の候補） |
| 音声の自動分割・ノイズ除去・自動の文字起こし（ASR） | **実装していない**。統合パッケージに ASR の重みが無く、公式の ASR スクリプトは実行時に第三者のミラーから取得する（§18.1）。利用者が書き起こしつきのクリップを用意する |
| v2Pro・v3・v4 の学習、複数 GPU、チェックポイントからの再開、複数キャラクターの同時学習 | **しない**（§18.3 の決定） |
| 学習した重みのバンドルへの同梱・他の PC への移行 | **しない**（§18.5。同意の範囲・版の結び付き） |
| Windows の Job Object・非表示ウィンドウでの学習プロセスの起動と、キャンセルで孫プロセスまで止まること | **未確認**（Linux のプロセスグループでは偽のプロセスで確認。Windows は §5.2 と同じ未確認） |
| macOS | **対象外**（ランタイムが macOS に対応しない。§16） |


## 19. Ollama のランタイムと、Ollama のモデルの取得（ローカル LLM。Issue #1105）

> **状態: 仕様のみ（未実装。Issue #1105）**。#1104（ローカル LLM の統合。[../local-llm.md](../local-llm.md) §11）と同じ分類 `local-llm` に載る。Issue 本文の「エルラマ」は **Ollama**（音声入力の表記ゆれ。llama.cpp は §17 で導入済み）。コードの現状（確認済み）: Ollama を導入・起動・更新する部品は **無い**（`providers.local` の URL に、利用者が別に入れた Ollama を書くだけ。Ollama のメモリ降ろし `/api/ps`・`keep_alive: 0` は `service/llmlocal` が持つ）。`grep` で `ollama` を含む実装は `llmlocal`・`localllm`・設定の文言だけ。

### 19.1 実行環境（`data/runtimes/ollama.json`。新設）

llama.cpp（§17.1）と同じ「ネイティブのサーバー」のランタイム（`launch.program`）。Python の環境・起動用スクリプトは無い。

| 項目 | 内容 |
|---|---|
| id・用途・名前 | `ollama`・`llm`・「Ollama」 |
| 取得 | GitHub の公式リリース（`github.com/ollama/ollama/releases/download/<タグ>/…`。許可ホストは増えない）。固定する版は **v0.40.15**（2026-11-03 公開。GitHub API で確かめた）。タグはリリースのアセットで、コミット固定の URL ではない（llama.cpp §17.1・ComfyUI のエンジンと同じ扱い。URL・サイズ・SHA-256 を固定する） |
| 対応と変種 | **Windows x64 のみ**。`nvidia`（GPU が NVIDIA、または GPU なし〔CPU 実行〕）→ `ollama-windows-amd64.zip`、1,614,941,966 B、SHA-256 `9ca74a7ab66ec86870b1eecac7df160df08dc4e3a291ab5d2d59bcf9310221c6`（GitHub のアセットの `digest`。**ダウンロードして測り直すのは実装時**）。展開後のサイズは実装時に測る。AMD（Radeon）は別アセット `ollama-windows-amd64-rocm.zip`（258,790,357 B。標準の zip に重ねて展開する形。公式の windows.mdx の記述）で、**この Issue では扱わない**（「この PC では使えません。AMD の GPU は未対応です」の理由。決定が要ること）。Windows arm64（`ollama-windows-arm64.zip`）・MLX 版も扱わない |
| ライセンス | **MIT**（GitHub API の `license`、リポジトリの `LICENSE`）。商用利用: 可（`yes`）。カードの詳細のソース・ライセンスのリンクは `github.com/ollama/ollama` |
| 置き場所 | `<mam.exe のあるディレクトリ>\localai\ollama\`（§2 と同じ。`env\`・`downloads\`・`cache\`・`tmp\`・`models\`〔Ollama のモデル。下記〕・`state.json`・ログ）。Ollama 自身のホーム（`%USERPROFILE%\.ollama`）には**書かせない**: 環境変数 `OLLAMA_MODELS` を `localai\ollama\models` にし、`HOME`・`USERPROFILE` を `localai\ollama\home` にして、利用者が別に入れた Ollama のモデル・設定と混ざらないようにする（`USERPROFILE` の付け替えが Windows の Ollama で効くことは**未確認**。効かなければ利用者の `.ollama` に `history` などが書かれる。§19.7） |
| 起動 | `env\ollama.exe serve` を**直接**起動する。引数は `serve` だけ（任意の引数・実行ファイルは受け付けない。利用者・MCP・設定が渡せるのはモデルの名前だけ）。環境変数: `OLLAMA_HOST=127.0.0.1:{port}`（**`127.0.0.1` だけ**。カタログの検証が、`OLLAMA_HOST` に `127.0.0.1` と `{port}` を含み `0.0.0.0`・`::`・`[` を含まないことを確かめる）・`OLLAMA_MODELS`・`OLLAMA_KEEP_ALIVE=15m`（llama.cpp の `--sleep-idle-seconds 900` と同じ長さ。§17.1）・`OLLAMA_NOPRUNE`・`OLLAMA_ORIGINS` は既定のまま（既定は localhost 系のみ）。**Ollama のクラウド機能（クラウドのモデル・Web 検索）は無効にする**: `server.json` の `disable_ollama_cloud: true`（公式の faq.mdx の手順。置き場所は上の `home` の下の `.ollama\server.json`。mam が起動のたびに書く）。クラウドのモデルは、プロンプトを Ollama のサーバーへ送るので、「ローカルで動く」という Issue の前提に反する |
| 準備の確認 | `GET /api/version` が 200（最長 60 秒。モデルはこの時点では読み込まない。読み込みは最初の生成のとき） |
| 認証 | **無い**（Ollama には API キーの仕組みが無い。llama.cpp の `LLAMA_API_KEY` に当たるものが無い）。§19.4 |
| 同梱 | しない。実行時に取得する。`THIRD_PARTY_NOTICES.md`・README に Ollama（MIT）と 3 つのモデルの一覧（出典・ライセンス）を足す |

導入・起動・停止・自動起動（既定オフ。最初の LLM の呼び出しで起動）・削除・状態・排他・`View` は §5〜§8 と同じ。`Remove` は `env\`・`downloads\`・`cache\`・`tmp\`・`home\` を消し、**`models\`（取得したモデル。数十 GB）は消す前に確認する**（`hx-confirm`「取得した Ollama のモデルも消えます」。他のランタイムの `Remove`〔重みも消す〕と同じ振る舞いで、確認の文だけが違う）。更新（固定版が上がった）は導入し直しで、`models\` は残す。

### 19.2 Ollama のモデル（`data/assets/ollama.json`。新設）

Ollama のモデルは、**mam の `fetch` ではなく、管理している Ollama 自身の `POST /api/pull` で取得する**（Ollama のモデルは複数の blob と manifest の組で、置き場所の規則・再開・重複排除は Ollama が持つ。mam が直接書くと、`OLLAMA_MODELS` の内部形式に依存する）。mam が持つのは、**厳選した名前の一覧**（カタログ）と、取得後の照合だけ。

| 名前（Ollama の `<名前>:<タグ>`） | 画面の名前 | モデルの層（`model`）のサイズ・digest（レジストリの manifest で確認した値） | 取得の合計 | ライセンス（レジストリの `license` の層の本文） |
|---|---|---|---|---|
| `gemma4:12b` | Gemma 4 12B（純正） | 7,381,383,680 B・`sha256:bb722270d54346adc198f213851dbe0207e87c3ecf2a0aff4d92262726215391` | 約 8.02 GB（8,021,618,941 B。projector・draft の層を含む） | Apache-2.0（商用可） |
| `qwen3.8:27b` | Qwen3.8 27B（純正） | 16,810,714,464 B・`sha256:f5f1dd8920d417aac2718b0bda3403da274301efdd6760b4f0f4b864ff2ad57d` | 約 17.74 GB（17,741,872,154 B） | Apache-2.0（商用可） |
| `deepseek-r1:14b` | DeepSeek-R1-Distill-Qwen-14B（純正） | 8,988,109,952 B・`sha256:6e9f90f02bb3b39b59e81916e8cfce9deb45aeaeb9a54a5be4414486b907dc1e` | 約 8.99 GB（8,988,112,209 B） | MIT（商用可） |

（確認日 2026-11-07。`GET https://registry.ollama.ai/v2/library/<名前>/manifests/<タグ>` の `layers[]`、`license` の層の本文は `…/blobs/<digest>`。**Ollama のタグは書き換えられる**ので、digest は「その日の実物」を記録したもので、将来 `pull` が別の中身を返すことがある。照合の扱いは §19.3。）

- 選んだ 3 つは llama.cpp の選択肢（§17.2）の純正モデルと同じ系統。**非検閲（abliterated）の派生は載せない**（Ollama のレジストリのコミュニティ版は、ライセンスの層が無いものが多く、`huihui_ai/…` などの名前の空間にあって、mam がライセンスと成人向けの扱いを確かめられない。#1008 後半の llama.cpp では Hugging Face のカードで確かめた）。載せる名前を増やすときは、上と同じ確認（manifest の層・ライセンスの本文）を要する。
- **画面に出さない・使わない**: タグが `cloud` を含むもの（クラウドのモデル）。レジストリの名前の空間が `library` 以外のもの。

### 19.3 取得（`pull`）の流れ・進捗・中断・照合

- **ジョブ**: 種別は既存の `model_install`（**DB のマイグレーションなし**）、`target=ollama:<名前>:<タグ>`（例 `ollama:gemma4:12b`）、ペイロード `{target}`。再開可（`attempts` が 3 未満の間）。同じ名前を重ねて取得すると同じジョブ（`Existing=true`）。管理している Ollama が止まっていれば、ジョブが起動してから取得する（起動に失敗すれば、ログの末尾つきで失敗）。**導入中のランタイム・止まっている Ollama でも、取得のボタンは出す**（押すと起動する。理由の 1 行を添える）。未導入なら押せず、「先に Ollama を導入してください」。
- **取得**: `POST http://127.0.0.1:<port>/api/pull`（`{"model":"<名前>:<タグ>","stream":true}`）の NDJSON を読む。`status`（`pulling manifest`・`pulling <digest>`・`verifying sha256 digest`・`writing manifest`・`success`）と `total`・`completed` から進捗（%・バイト数・段階の日本語）を作る。層ごとの `total`・`completed` は、層が切り替わるたびに 0 に戻る（公式の api.md）ので、**合計の進捗は、カタログの合計のサイズ（上の表）に対する、確定した層の分＋今の層の `completed`** で出す（Ollama の応答に全体の合計は無い）。`error` のある行は失敗（日本語にして出す。ディスクの空き不足・ネットワーク・名前が無い）。
- **空き容量**: 取得の前に、`models\` のあるドライブの空きが、合計のサイズ＋ 1 GiB に足りなければ、ジョブを作らず 409（`*fetch.NoSpaceError`。他のモデルの導入と同じ文面「必要量・空き・場所」）。
- **中断**: `POST /model-manager/jobs/:job/cancel`（`target` が `ollama:` なら管理している Ollama への `/api/pull` の接続を閉じる）。Ollama は途中の層を残すので、取り直すと続きから進む想定（**未確認**。§19.7）。
- **照合**（`success` のあと）: `OLLAMA_MODELS` の `manifests\registry.ollama.ai\library\<名前>\<タグ>` を読み、`model` の層の digest・サイズがカタログと同じかを確かめる。**同じ → 状態 `verified`**。**違う → 状態 `changed`**（バッジ「上流で更新されています」と注記。「モデルの内容がカタログに記録した版と違います。使えますが、mam は確かめていません。出典のページで内容を確認してください」。**削除も使用の禁止もしない**: Ollama が自分で digest を検証して書いた正規のファイルで、タグが更新されただけの場合が多いため）。manifest が読めない（形式が変わった）ときも `changed`。Ollama 自身が各層の SHA-256 を manifest の digest と照合する（`verifying sha256 digest`）ので、**転送の破損は Ollama が検出する**。mam の固定との違いは、**「どの内容を取得するか」を mam が固定できない**（タグが動く）点で、これは llama.cpp・ComfyUI のモデル（コミット固定）より弱い。決定が要ること。
- **削除**: `DELETE /model-manager/ollama/models/<名前>:<タグ>`（管理している Ollama の `DELETE /api/delete`）。使われている LLM の割り当て（`llmassign` が `local` の `model` に指している）があれば 422（使われている Agent の名前を出す。HF の導入の削除と同じ）。取得中は 409。未取得は 409。Ollama が止まっていれば、削除の間だけ起動する。
- **一覧と状態**: `GET /api/tags`（Ollama が持つモデル）とカタログを合わせる。状態: `missing`（未取得）・`pulling`・`installed`（取得済み・`verified`）・`changed`・`unavailable`（Ollama が未導入・起動できない）。**カタログに無いモデル**（利用者が自分で `ollama pull` したもの）は、`GET /api/tags` にあれば「そのほかのモデル」として名前・サイズだけを一覧の末尾に出す（取得・削除はしない。削除は利用者の `ollama` で）。自由入力の取得は決定が要ること（既定: しない）。
- **LLM の割り当て**: 割り当ての `model` は `<名前>:<タグ>`（`gemma4:12b`）。`use` が `ollama` の間、モデルの選択（`GET /model-picker/…` の `llm`）には、`GET /api/tags` の取得済みのモデルだけが出る（llama.cpp の「選んだ 1 つだけ」〔local-llm.md §10.1〕と違い、Ollama は複数のモデルを持てるので、すべて出す）。**モデルを切り替えても Ollama は止まらない**（呼び出しの `model` で決まる）。
- **ComfyUI との取り合い（VRAM）**: llama.cpp と同じ規則（§19.5）。

### 19.4 認証が無いことと、受け入れる危険

管理している Ollama は `127.0.0.1` だけで待ち受けるが、**認証が無い**。同じ PC のほかのプログラム・ブラウザの Web ページ（DNS リバインディングや Content-Type を `text/plain` にした単純な POST）から `/api/pull`・`/api/delete`・`/api/generate` を呼べる可能性がある。llama.cpp（`LLAMA_API_KEY` で防ぐ。§17.1）・GPT-SoVITS（起動用スクリプトが Host を確かめる。§5.2）のような防ぎ方が、Ollama には**ない**。mam が前に認証つきのリバースプロキシを置く案もあるが、新しいプロセスと仕組みを足すので、この Issue では**足さない**（決定が要ること）。受け入れる範囲: 利用者が別に入れた Ollama を `custom` で使っても同じ状態であること、`OLLAMA_ORIGINS` の既定が localhost 系だけであること、Ollama の新しい版が Host の確認を持つこと（CVE-2024-28224 の修正。詳細は**未確認**。§19.7）。画面の「?」と詳細に、「このサーバーには認証がありません。信用できないソフトを同じ PC で動かさないでください」を書く。

### 19.5 VRAM・起動

llama.cpp（local-llm.md §10.3）と同じ `gpufree.Guard` に載せる: LLM の呼び出しの前に ComfyUI が生成中なら待つ・画像・動画の生成の前に Ollama のモデルを降ろす（`keep_alive: 0` を送る。メモリ降ろしの既存の仕組みと同じ呼び出し）・アイドルは `OLLAMA_KEEP_ALIVE=15m`。「画像・動画を作る前に、Ollama のモデルをメモリから降ろす」のスイッチ（`unload_before_generation`）は、管理している Ollama でも効く（local-llm.md §11）。27B のモデルは ComfyUI と同居できない（VRAM の目安は llama.cpp の表〔§17.2〕と同じ。mam の推定で未確認）。

### 19.6 macOS（Apple Silicon）は対応外にした（既定。決定が要ること）

公式のリリースには `ollama-darwin.tgz`（185,971,671 B。SHA-256 `607c79d5d6315ef9d896db83adde736a4c0057196329843544e79b95afb260a8`）と `Ollama-darwin.zip`（アプリ。229,106,611 B）がある。llama.cpp（§17.4）と同じ理由で、`os_reasons.darwin` に理由を書き、導入・起動のボタンを出さずに案内する: ①`.tgz` の展開後の構成・実行権限・Gatekeeper（未署名の実行ファイルの扱い）・Metal の動作は、macOS の実機なしで確かめられない。②Apple Silicon の自動導入はオーナーの判断（#1008 の前例）。案内は「Ollama の公式のアプリ（ollama.com）を入れて、『別のサーバー』（`http://localhost:11434`）につないでください」（local-llm.md §8.1 の手順を指す）。**この案内は、Ollama のカードの理由の行と、`#mm-llm-use` の「別のサーバー」の説明に出す**。

### 19.7 テスト・未確認

- **テスト（実装時）**: `catalog/ollamatest`（ピン: URL が `github.com/ollama/ollama/releases/download/` のアセットだけ・SHA-256・サイズ・`OLLAMA_HOST` が `127.0.0.1` と `{port}` を含み `0.0.0.0`・`::` を含まない・引数が `serve` だけ・`disable_ollama_cloud`・モデルの名前が `library` で `cloud` を含まない・ライセンスの答えが 3 つとも `yes`）、`service/ollamapull`（新設。偽の Ollama〔NDJSON を返す HTTP サーバー〕で、進捗の合計・層の切り替え・`error` の行・中断・`success` 後の照合〔`verified`・`changed`・manifest が読めない〕・空き容量不足・削除〔使用中の 422・取得中の 409〕）、`handler/modelmgr/ollama`（画面・ルート・CSRF・状態コード）、`runtimes_test.go`（Ollama のカードの表示・非対応の理由）。実物の Ollama・GPU・レジストリは使わない。
- **未確認**: 実物の Windows での導入（zip の取得・展開・`ollama.exe serve`・`/api/version`）・`/api/pull` の実際の応答（`total`・`completed` の動き）・中断後の再開・`USERPROFILE` の付け替えの効き方・`server.json` の `disable_ollama_cloud` が起動の前に置いて効くこと・展開後のサイズ・認証が無いことの実際の危険（§19.4）・VRAM と ComfyUI の同居。この開発環境は Linux で GPU が無い。公式の `docs/api.md`・`windows.mdx`・`faq.mdx`・GitHub のリリースの API・レジストリの manifest は読んで確かめた。
