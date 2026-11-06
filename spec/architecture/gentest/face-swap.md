# 顔の入れ替え（外部ツール FaceFusion。Issue #995）

生成した画像の顔を、別の画像の顔に入れ替える。入れ替えは **FaceFusion**（<https://github.com/facefusion/facefusion>）が行い、mam は **ffmpeg と同じように「同梱しない外部ツール」** として扱う。mam のバイナリにもインストーラーにも FaceFusion とそのモデルは入れない。利用者が別に入れたものを、mam が別のプロセスとして `facefusion.py headless-run`（FaceFusion の非対話モード）で呼ぶだけにする。**この方式は Issue #995 の方針で、変えない**（拡散モデルでの入れ替えなど別の方式は [../local-models.md](../local-models.md) §4.6 の「顔入れ替え」の整理と #960 のとおり別の話）。

関連: [gentest.md](gentest.md)（生成テスト。結果は履歴とジョブ一覧に並ぶ）、[../ffmpeg-lookup.md](../ffmpeg-lookup.md)（検出と設定のお手本）、[../local-models.md](../local-models.md) §4.6・§4.9（顔固定と顔の類似度の計測）。

## 1. 方針

- **同梱しない**: FaceFusion 本体は 2025-02 に MIT から **OpenRAIL-AS** になった。利用の制限が使う人に直接かかり、リポジトリに条文の全文は無い（条項は未確認で、mam は読んだことにしない）。mam は取得も配布もせず、設定画面で場所を指すだけにする。
- **mam は取得しない**: 入れ替えモデルのファイルは、初めて使うとき **FaceFusion 自身が**配布元から取得する（mam は取得しないし、同意の画面も出さない）。画面にその旨を書く（`facefusion.FirstRunNote`）。
- **既定のモデルは商用利用しやすいものに限る**: 入れ替え `ghost_2_256`（GHOST、Apache 2.0）・顔検出 `yunet`（MIT）・復元 `gfpgan_1.4`（Apache 2.0）。**GHOST は顔の特徴の変換に CrossFace（OpenRAIL-M）の重みも使う**ので、「商用の制限なし」ではなく「条件つき」とし、その旨を注に出す（条項は使う前に読む）。GHOST の 3 つの版（`ghost_1_256`・`ghost_2_256`・`ghost_3_256`）のどれが良く見えるかは**比べていない**（GPU が要る）。既定は 2 を仮に置いた。
- **非商用・研究用・商用不明のモデルは、利用者が自分で選んだときだけ使え、警告を出す**。選んだ瞬間（画面の選択を変えたとき）と、実行した結果（結果の詳細に残る）に出す。警告は「個人の試用に限り、商用の作品には使わない」。
- **どのモデルを選んでも付く注意**（`facefusion.ArcFaceNote`）: FaceFusion は顔の特徴の計算に **ArcFace**（InsightFace 由来。FaceFusion の一覧では Non-Commercial）を使い、`facefusion/face_recognizer.py` にそのモデルしか無く、選択肢で置き換えられない。つまり**既定の組み合わせでも、商用の作品に使えるとは確かめられない**。Issue 本文の「既定は商用利用しやすいものに限る」は、選べるモデルの範囲についてであって、全体としての商用可を意味しない（画面と MCP の `notes` に出す）。
- **成人向けの画像には使えない**（`facefusion.AdultNote`）: 公式はヌード・成人向けの処理を断り、NSFW の判定（content analyser）は無効にできない（<https://docs.facefusion.io/introduction/disclaimer>）。mam の成人向けモデルの結果には使えない前提で、その旨を画面（設定・結果の詳細の箱・結果の詳細の行）と MCP の `notes` に出す。mam は画像の中身を判定しない（NudeNet などは持たない）ので入れ替えを事前には断らず、FaceFusion が断ったらその出力の最後の数行が結果の失敗の理由になる。
- **非公式の ComfyUI ラッパー**（huygiatrng/Facefusion_comfyui。ライセンス表記 NOASSERTION）は使わない。
- **範囲**: 生成済みの画像 1 枚 + 顔の画像 1 枚（その中でいちばん大きい顔）→ 画像 1 枚。動画・複数の顔・顔の選択（左から何番目）・マスク・pixel boost・weight は今回は扱わない。

## 2. 検出と設定

`internal/facefusion`（プロセスの起動とモデルの表）と `service/facefusionsettings`（設定の保存。`ffmpegsettings` と同じ作り）。設定ファイルは `<data-dir>/facefusion.json`（`{"python": "...", "path": "..."}`。ほかのキーは残す）。

| 項目 | 内容 |
|---|---|
| `python` | FaceFusion の環境の Python の実行ファイルの絶対パス。空なら、conda の環境 `facefusion` の `python`（`~/miniconda3`・`~/anaconda3`・`~/miniforge3` の `envs/facefusion`。FaceFusion の公式手順がこの名前を使う）、なければ PATH の `python3`・`python` |
| `path` | FaceFusion のフォルダ（`facefusion.py` と `facefusion/metadata.py` があるところ。`facefusion.py` のパスを書いても、その親として扱う）の絶対パス。空なら `~/facefusion`・`~/Documents/facefusion`・`~/Desktop/facefusion` を順に |
| 使えないパス | 絶対パスでない・実行ファイルでない・`facefusion.py` が無い・FaceFusion のフォルダでない（`facefusion/metadata.py` が無い）は `*facefusion.ConfigError`（保存せず、画面は 422）。**保存済みのパスが後で使えなくなっても、探索には切り替えない**（別のものを起動しないため。ffmpeg と同じ） |
| 状態の表示 | Python とフォルダのそれぞれの「見つかりました（パス）／見つかりません（理由）」、見つかった FaceFusion の版とライセンス（`facefusion/metadata.py` の `version`・`license` の自己申告）、フォルダを探す場所 |
| 反映 | **保存するとすぐ**使われる（ffmpeg も同じ。ffmpeg は保存のすぐあとに探し直して全サービスの `media.Tools` を入れ替える。FaceFusion は入れ替えのたびに探す） |
| FaceFusion を実行して確かめない | 状態の表示は、ファイルの有無と `metadata.py` の読み取りだけ（`facefusion.py --version` も走らせない。モデルのランタイムを読み込むので数秒かかる） |

画面は設定の分類「FaceFusion」（`/settings/facefusion`。ffmpeg の次）。概要のカードは「FaceFusion を使えます」（ok）／「未設定（使うときだけ入れます）」（neutral）／「設定ファイルを読めません」（warn）。表に、入れ替え・顔検出・復元の各モデルの ID・ライセンスの級（バッジ）・FaceFusion の一覧の表記・既定・注を並べ、標準の注意（§1）とライセンスの出典へのリンクを付ける。ルートは `PUT /settings/facefusion`（フォーム `python`・`path`。空は指定なし。200 は分類の本文（`#facefusion-panel`）と通知、使えない値は 422 で入力を残す）。`settingsroutes.Services.FaceFusion` があるときだけ分類とルートがある（ない構成では 404）。

## 3. 入れ替えの実行

`face.Swap`（`service/gentest/face`。`service/gentest` が行数の上限なので別パッケージで、サービスの公開メソッドだけを使う。画面の POST と MCP の `swap_generation_face` が呼ぶ）。**アップスケールと同じ形**で、既存の仕組み（ジョブ種別 `gentest`・履歴・ジョブ一覧）に載せた。

1. **入力**: 入れ替え先（`Target`）と顔（`Face`）は `RefInput`（history・upload・project_asset・reference_item・pose_library）。どちらも `ResolveInput` で解決し（PNG・JPEG・WebP、20 MiB 以下）、バッチのディレクトリへ複製する。入力の役割は入れ替え先が `edit`、顔が `face`。
2. **バッチ**: 結果 1 件・入力 2 枚の**独立したバッチ**。結果のモデル識別子は固定の `facefusion`（`face.SwapModel`）、`api_model` は入れ替えモデルの ID。見積り・確認は無い（ローカルの処理で費用が無い。`cost_basis: free`）。ジョブ（`gentest`）にして `SetBatchJob`。ジョブ一覧の「生成テスト」に並び、履歴（ギャラリー）に通常の結果と同じに並ぶ。履歴の「モデル」の絞り込みにも `facefusion` が出る。
3. **実行**（`runner.faceSwap`）: `FaceSwapFunc`（アプリは `facefusion.Install.Swap`）を呼び、返った画像を `<result_id>.png` として保存して成功にする。FaceFusion が使えないときの失敗は `facefusion_unavailable`（導線 `/settings/facefusion`。共通エラー [../errors.md](../errors.md)）として結果の `failure` に出る。
4. **起動する引数**（`facefusion.Args`。`python` の引数）:

   ```
   facefusion.py headless-run
     --source-paths <顔の画像> --target-path <入れ替え先> --output-path <出力.png>
     --jobs-path <作業用>/jobs --temp-path <作業用>/temp
     --processors face_swapper face_enhancer
     --face-swapper-model ghost_2_256 --face-detector-model yunet --face-enhancer-model gfpgan_1.4
   ```

   復元を使わない（`enhancer: none`）ときは `--processors face_swapper` だけで、`--face-enhancer-model` は付けない。作業ディレクトリは FaceFusion のフォルダ（相対の `facefusion.py` と `.assets` のため）。**入力・出力・ジョブ・一時ファイルは mam が作る一時ディレクトリ（`mam-facefusion-*`。終わったら消す）に置き**、FaceFusion のフォルダには書かせない。環境変数 `PYTHONIOENCODING=utf-8`・`PYTHONUTF8=1`・`PYTHONUNBUFFERED=1` を足す。Windows では `CREATE_NO_WINDOW`（コンソールの窓を出さない）。
5. **モデルの検査**: 入れ替え・顔検出・復元のモデル ID は mam が持つ表（`facefusion.Swappers`・`Detectors`・`Enhancers`。FaceFusion 3.x の `choices` と同じ名前）にあるものだけ。無い ID は、プロセスを起動する前に「選べません（選べるもの一覧）」で拒否する。
6. **失敗**: 非ゼロの終了・時間切れ（既定 30 分。初回はモデルの取得で長い）・終了しても出力が無い・出力が画像でない、は `*facefusion.RunError`。出力（標準出力・標準エラーの末尾の数行。キャリッジリターンも改行として扱う）を結果の理由に含める。結果の `failure` は `facefusion_failed`（原因は終わり方: 時間切れ・異常終了・出力なし。直し方は顔がはっきり写っているか・成人向けの画像は断られる・GPU のメモリ・設定画面の確認。出力の末尾は詳細）。出力に `out of memory` などがあれば `vram_oom`（共通エラー §5）。英語の出力そのものは `detail` にだけ出る。ジョブが取り消されたとき（コンテキスト）は FaceFusion の失敗として扱わない。
7. **記録**（`record.FaceSwap`、結果の `params.face_swap`）: 元の結果 ID（入れ替え先が履歴の結果のとき）・入れ替え / 顔検出 / 復元のモデル・**ライセンスの警告**。元の結果を後で消しても、この結果は残る。
8. **顔の類似度**: 顔の画像は役割 `face` の入力なので、結果には通常の生成と同じ顔の類似度（SFace。[../local-models.md](../local-models.md) §4.9）も測って付く。入れ替えで顔が実際に近づいたかを同じ物差しで見られる。

## 4. 画面

結果の詳細（`/generation-test/results/:id`）の「大きくする」の下に、**「顔を入れ替える（FaceFusion）」の箱**（`organisms/gentest/faceswapbox`、`handler/gentest/faceswapscreen`）。画像で、ファイルがある結果にだけ出る。

- 標準の注意（成人向けの画像には使えない・本体のライセンス・ArcFace・モデルは FaceFusion が取得する）を常に出す。
- FaceFusion が見つからないときは、フォームの代わりに理由と設定の「FaceFusion」へのリンクを出す。
- フォーム: 顔の画像のアップロード（必須）、入れ替えモデル・顔検出モデル・顔の復元モデルの選択（各選択肢に「（商用NG）」などの級を付ける。既定が選ばれている）。選択を変えるたびに `GET /generation-test/results/:id/face-swap/warnings`（`swapper`・`detector`・`enhancer`）で警告の区画だけ取り直す。「顔を入れ替える」は `POST /generation-test/results/:id/face-swap`（multipart）。
- 成功は履歴への `HX-Redirect`（新しい結果が実行中のまま並ぶ）。断り（顔の画像が無い・モデルが無い・FaceFusion が使えない）は、箱を 422 で選択を残したまま返す。
- 入れ替えの結果は詳細に「顔の入れ替え」（モデル）・「ライセンスの注意」・「成人向けの画像」・「顔の類似度」の行と、元の結果へのリンクが出る。**「この設定でもう一度」「似た条件でもう 1 枚」は出さない**（プロンプトが無い）。

## 5. MCP

画面と同じ操作を `mcp/tools/gentestface/face.go` の 2 ツールで行う（[../../api/mcp-tools-generation-test.md](../../api/mcp-tools-generation-test.md)）。

| ツール | 内容 |
|---|---|
| `get_facefusion_status` | 見つかったか・保存したパス・版とライセンスの自己申告・探した場所・選べるモデルと級・既定・標準の注意（`notes`）・出典の URL |
| `swap_generation_face` | `target`・`face`（どちらも `{source, id}`）と任意の `swapper`・`detector`・`enhancer`。`job_id`・`batch_id`・`result_id`・`warnings` を返す。FaceFusion が見つからない・未知のモデルは `validation` |

## 6. モデルのライセンス表（一次情報）

出典: FaceFusion の公式ドキュメント **「Licenses」<https://docs.facefusion.io/introduction/licenses>**（2026-11-03 に確認。FaceFusion 3.9.2）。Issue 本文の列挙は信じず、この表を元にした。**一覧に載っていない・ライセンスが「Unknown」「None」のものは「商用不明」と表示する**。mam の級は次のとおり: 商用の制限なし（`open`）・商用は条件つき（`conditional`）・商用NG（`noncommercial`）・研究用のみ（`research`）・商用不明（`unknown`）。「警告」は級が `noncommercial`・`research`・`unknown` のとき。

| 種類 | モデル ID | FaceFusion の一覧の表記（Vendor） | 級 | 警告 |
|---|---|---|---|---|
| 入れ替え | `ghost_1_256`・`ghost_2_256`・`ghost_3_256` | GHOST（ai-forever）: Apache 2.0。CrossFace（FaceFusion）: OpenRAIL-M | conditional（CrossFace の条項は未確認） | なし（注あり） |
| 入れ替え | `inswapper_128`・`inswapper_128_fp16` | INSwapper（InsightFace）: Non-Commercial | noncommercial | あり |
| 入れ替え | `simswap_256`・`simswap_unofficial_512` | SimSwap（neuralchen）: Non-Commercial | noncommercial | あり |
| 入れ替え | `blendswap_256` | BlendSwap（mapooon）: Non-Commercial | noncommercial | あり |
| 入れ替え | `alphaface_256` | AlphaFace（andrewyu90）: Non-Commercial | noncommercial | あり |
| 入れ替え | `hyperswap_1a_256`・`hyperswap_1b_256`・`hyperswap_1c_256` | HyperSwap（FaceFusion）: ResearchRAIL | research | あり |
| 入れ替え | `hififace_unofficial_256` | HifiFace（GuijiAI）: Unknown | unknown | あり |
| 入れ替え | `uniface_256` | UniFace（xc-csc101）: None | unknown（ライセンスが無い = 使ってよい、ではない） | あり |
| 顔検出 | `yunet` | YuNet（OpenCV）: MIT | open | なし |
| 顔検出 | `yolo_face` | YOLO Face（derronqi）: GPL-3.0 | conditional（コピーレフト） | なし（注あり） |
| 顔検出 | `retinaface`・`scrfd` | RetinaFace・SCRFD（InsightFace）: Non-Commercial | noncommercial | あり |
| 顔検出 | `many` | 上の 3 つを併用 | noncommercial（いちばん厳しいものに従う） | あり |
| 復元 | `gfpgan_1.2`・`1.3`・`1.4` | GFPGAN（TencentARC）: Apache 2.0 | open | なし |
| 復元 | `restoreformer_plus_plus` | RestoreFormer（wzhouxiff）: Apache 2.0 | open | なし |
| 復元 | `codeformer` | CoderFormer（sczhou）: S-Lab 1.0 | noncommercial | あり |
| 復元 | `gpen_bfr_256`・`512`・`1024`・`2048` | GPEN（yangxy）: Non-Commercial | noncommercial | あり |
| （選べない） | ArcFace | InsightFace: Non-Commercial | — | 常に注意（§1） |

- モデル名は FaceFusion 3.x のソース（`facefusion/processors/modules/face_swapper/types.py`・`face_enhancer/types.py`・`facefusion/types.py` の `FaceDetectorModel`）から取った。FaceFusion が名前を変えたら、表（`facefusion/models.go`）も合わせて直す（無い名前は FaceFusion が引数エラーにするので、結果が失敗になる）。
- 一覧の表記は FaceFusion が付けたもので、mam が各モデルの元のリポジトリの条文を読み直したわけではない。GFPGAN は第三者の部品・学習データの条件が議論される（[../local-models.md](../local-models.md) §4.6 の表の指摘）が、ここでは FaceFusion の表記に従った。

## 7. 実機が必要な項目（今回確かめていないこと）

- **実際の FaceFusion の起動**: テストは `facefusion.py` と Python の代わりのシェルスクリプト（偽の `python`）で、引数・作業ディレクトリ・出力の取り込み・失敗・時間切れ・取り消しを確かめた。実際の FaceFusion（ONNX Runtime の CUDA・初回のモデル取得・出力ファイルの名前の扱い・`--output-path` が `.png` のファイルを受けるか）は**実機で未確認**。Windows・16GB VRAM の動作確認は別途（Issue の完了条件の 5 つ目）。公式の目安は VRAM 8GB〜（12GB 以上を推奨）。必要なディスクは未確認。
- GHOST の 3 つの版のどれを既定にするか、復元の強さ・ブレンドを渡すか。
- macOS での FaceFusion（Apple Silicon の ONNX Runtime の経路）。
- 「成人向けの結果」を FaceFusion が断るときの終了コードと出力の文言（mam は出力の最後の数行を理由に出すだけで、文言を判別しない）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 理由 |
|---|---|---|---|
| 1.0 | 2026-11-03 | 新規。FaceFusion を同梱しない外部ツールとして扱う方針、検出と設定（`facefusion.json`）、`headless-run` での入れ替え（既定 ghost＋yunet＋gfpgan）、画面・MCP、モデルのライセンス表（FaceFusion の公式一覧が一次情報）、ArcFace の注意、実機が要る項目 | Issue #995 |
| 1.1 | 2026-11-03 | §2・§5: MCP の `set_facefusion_paths` を外した。パスの設定は設定画面だけ（パスは後で起動される実行ファイルを決めるので、MCP のクライアントに決めさせない。ffmpeg のパスに MCP ツールが無いのと同じ）。MCP は `get_facefusion_status`（読み取り）と `swap_generation_face` の 2 つ | 統合後レビューの指摘 |
| 1.2 | 2026-11-04 | 失敗を共通エラーにした: FaceFusion が使えないときは `facefusion_unavailable`（503。従来は 422 の入力エラー）、実行の失敗は `facefusion_failed`（出力の末尾は詳細）。結果の `failure`・画面のエラーボックス・MCP の `failure` で同じコード | Issue #1010 |
