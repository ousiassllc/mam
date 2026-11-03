# 追加ノード（ComfyUI のカスタムノードの許可リスト）

[model-manager.md](model-manager.md) の同梱 ComfyUI に、**mam が確認して固定したカスタムノードだけを導入し、読み込ませる**仕組み（Issue #976）。ControlNet・IP-Adapter を使えるようにする土台で、PuLID（#960）・VNCCS PoseStudio（#985）も許可リストに載せたが、導入しない（§5.1〜5.4。Issue #993・#997 で決めた）。ノードを足す手順は §8。実装は `backend/internal/modelmgr/nodecat`（許可リスト）・`modelmgr/nodes`（導入・検証）・`service/modelnodes`（導入ジョブ・同意・再起動）・`service/modelmanager/nodegate`（ノードの無いモデルを止める）。前処理の Canny は `backend/internal/imgprep`（§5.2）。

## 1. 方針

- **許可リストにないノードは、読み込まない**。同梱の ComfyUI は `--disable-all-custom-nodes` のまま起動し、`--whitelist-custom-nodes <フォルダ名…>` に「導入済みで、固定した内容のままのノード」のフォルダだけを渡す（§3）。`custom_nodes` に手でフォルダを置いても、リストの外は読み込まれない（テスト: `engine/engine_nodes_test.go`、`nodes/nodes_test.go` の `TestAFolderMamDidNotInstallIsNeverEnabled`）。
- **ノードは任意のコード**: ComfyUI の中で mam を動かすアカウントの権限のまま動く（ファイル・ネットワークを使える）。だから、①許可リストの 1 件ごとに**ソースを読んだ記録**（`review`）と固定したコミット・SHA-256 を持ち、②導入の前に、ライセンスに加えて**注意事項（`modelnodes.SecurityNotice`）への同意**を必ず取る。確認は開発時の 1 度きりで、コードの安全を保証するものではない、と画面にも書く。
- **コアのノードで済むものは、ノードを入れない**。ControlNet は ComfyUI のコアの `ControlNetLoader`・`ControlNetApplyAdvanced` で動くので、追加ノード無しで使える（§6）。画像の前処理は、重みの要らない **Canny だけを mam の中（Go。`backend/internal/imgprep`）で行い**、`controlnet_aux` は導入しない（§5.2。Issue #993）。
- **pip を実行しない・git を使わない**。ノードが import するのは、同梱の Python 環境（ComfyUI 公式のポータブル版）に**既にあるパッケージだけ**（`nodecat/data/python.json`。v0.42.1 の `requirements.txt` から作った一覧）。足りないものがあるノードは、一覧に載せたまま `install_blocked` を付け、導入ボタンを出さない。
- **置き場所**: ノードは `<data>\custom_nodes\<folder>\`（`store.Layout.CustomNodesDir()`。ComfyUI の `--base-directory` の下。エンジンの更新で消えない）。導入の記録は `<comfyui>\nodes.json`（`Layout.NodesPath()`。`state.json` とは別のファイル: 同じファイルを 2 つの `store.Store` が書かないため）。アンインストールで `comfyui` ごと消える。

## 2. 許可リスト（`nodecat/data/nodes/*.json`）

埋め込みの JSON（1 ノード 1 ファイル）。`nodecat.Validate`（`nodecat_test.go` が毎回走らせる）が、次を満たさないエントリを拒否する。

| 項目 | 内容 |
|---|---|
| `id`・`name`・`description` | 画面・MCP に出す名前と説明 |
| `repo`・`commit` | GitHub のリポジトリと **40 桁のコミット**（ブランチ・タグは不可） |
| `archive_url`・`archive_sha256`・`archive_size`・`extracted_size` | `https://github.com/<owner>/<repo>/archive/<commit>.zip`。SHA-256 とサイズで検証し、展開後が `extracted_size` を超えるアーカイブは断る |
| `folder` | `custom_nodes` の下のフォルダ名（`--whitelist-custom-nodes` に渡す名前） |
| `license` | ノードの**コード**のライセンス（リポジトリの LICENSE の本文を読んだ結果）。`commercial`（yes / conditional / no / unknown）・`checked_at`・URL・要約。モデルのライセンスと同じ型（`catalog.License`）で、モデルマネージャーの画面に商用利用の可否が出る |
| `python_requires`・`python_optional` | 読み込み時に import するパッケージ／一部のノードだけが import するパッケージ。`python.json` に無いものがあれば `install_blocked` が必須 |
| `install_blocked` | 導入しない理由（あれば、導入ボタンは無く、`Install` は理由つきで拒否） |
| `node_classes` | このノードが足すクラス。導入後、動いている ComfyUI の `GET /object_info/<class>` で読み込まれたことを確かめる |
| `model_folders` | ノードが登録するモデルのフォルダ（`ipadapter` など） |
| `limits` | 同梱の環境で動かないノード（FaceID など）の説明 |
| `reviewed_at`・`review` | ソースを読んだ日と結果（ネットワーク・サブプロセス・`exec`・pickle の読み込みの有無） |

### 2.1 今の許可リスト

| id | 状態 | コミット | ライセンス（コード） | 商用 | 備考 |
|---|---|---|---|---|---|
| `ipadapter-plus`（cubiq/ComfyUI_IPAdapter_plus） | 導入できる | `a0f451a5…`（アーカイブ SHA-256 `c6c49c82…`、306,422 B、展開後 734,885 B） | GPL-3.0 | 可 | import は torch・torchvision・einops・PIL・comfy だけ。ネットワーク・サブプロセス・`exec` は 5 つの `.py` に無い（`IPAdapterLoadEmbeds` の `torch.load` が 1 つ）。FaceID 系は insightface を遅延 import するので、そのノードだけ動かない（`limits`）。GPL-3.0 は ComfyUI と同じ別プログラムとして実行時に取得するので mam（AGPL-3.0）へは混ざらない |
| `controlnet-aux`（Fannovel16/comfyui_controlnet_aux） | **導入しない**（`install_blocked`） | `a587a4c1…` | Apache-2.0 | 不明 | opencv・scikit-image・mediapipe・onnxruntime などが同梱の Python に無く、mam は pip を実行しない（§5.1）。さらに、実行時に Hugging Face から前処理のモデルを取得し、その重みのライセンスが `lllyasviel/Annotators` は `other`、Depth-Anything-V2-Large は CC-BY-NC-4.0 で、商用利用の可否が確かめられない（`commercial: unknown`）。**Canny は mam の中で作る**（§5.2）ので、導入しなくても写真から制御画像を作れる。ポーズ（OpenPose・DWPose）・深度は重みが要り、非商用／不明のため作らない |
| `pulid-flux-chroma`（PaoloC68/ComfyUI-PuLID-Flux-Chroma。#960） | **導入しない**（`install_blocked`。理由は §5.3） | `52dc5068…`（アーカイブ SHA-256 `48d93b51…`、4,490,131 B、展開後 4,822,332 B） | LICENSE は Apache-2.0 の全文（著作権者の欄は雛形のまま）、README は「元の MIT License を引き継ぐ」で食い違う | 不明 | 4 クラス（`PulidFluxModelLoader`・`PulidFluxInsightFaceLoader`・`PulidFluxEvaClipLoader`・`ApplyPulidFlux`）。insightface・facexlib・onnxruntime・timm・ftfy・regex が同梱の Python に無く、mam は pip を実行しない（§5.1）。InsightFace antelopev2 の重みは非商用の研究用途のみで、同梱も自動の取得もしない（§5.3）。実行時に facexlib・EVA02-CLIP の重みを固定・検証なしで取得する。FLUX・Chroma に参照の顔の identity を足すが、**テンプレートは作らない**（同梱の Chroma1-HD での動作は未確認）。顔の identity は、Qwen-Image の編集モデルの `face` の役割（#960）で足りる |
| `vnccs-utils`（AHEKOT/ComfyUI_VNCCS_Utils） | **導入しない**（`install_blocked`。理由は §5.4） | `334eee54…`（アーカイブ SHA-256 `cc5567fc…`、61,515,840 B、展開後 79,266,802 B、832 ファイル） | MIT（`LICENSE`。取り込んだ ComfyUI-Krea2Edit は Apache-2.0） | 不明 | PoseStudio（`VNCCS_PoseStudio`）はブラウザの画面が `vnccs_req_pose_sync` でポーズを返すのを最長 150 秒待つ作りで、API だけで流す mam のグラフでは動かない（`pose_data` は画面のウィジェットが書く JSON）。`pose_image` を渡す経路（SAM 3D）も画面の取り込みを待つ（§5.4）。さらに、実行時に `huggingface_hub` で Hugging Face からポーズ集（`MIUProject/VNCCS_PoseLibrary_Main`・`Totemistyk/General_Poses_PoseStudio`。画面からの要求で自動更新）・モデルを取得し、`revision` を固定していない箇所がある（mam の固定・SHA-256 検証の方針に合わない）。同梱の Python に cv2・huggingface_hub・omegaconf・yacs・roma・timm・pytorch_lightning・color_matcher・dynamicprompts が無く、mam は pip を実行しない（import 時に要るのは torch・numpy・PIL と ComfyUI 本体だけ）。画面は esm.sh から three・ag-psd を読む。実機の `/object_info` は未確認（入出力は `__init__.py` の `INPUT_TYPES` から読んだ: 必須 `pose_data`、省略可 `pose_image`・`camera_prompt`・`animation_image_batch`、出力 `images`・`lighting_prompt`）。**テンプレートは作らない**。Qwen-Image 2.1 のポーズは、ポーズ集（Shisei）の画像を `pose` の役割の参照として渡し、LoRA（`qwen-image-21-vnccs-pose`）を重ねる方法で足りる（[../gentest/gentest.md](../gentest/gentest.md) §6） |

数値の出典（2026-11-03 に GitHub・Hugging Face で取得）: アーカイブの SHA-256 は github.com と codeload.github.com の両方で同じ値を確かめた。

## 3. 起動（同梱エンジン）

`engine.Installer.CustomNodes`（`nodes.Manager.Enabled`）が、**導入済みで、固定と一致するノードのフォルダ**を返す。`Command` は `--disable-all-custom-nodes` のあとに `--whitelist-custom-nodes <folder…>` を足す（0 件なら足さない）。ComfyUI v0.42.1 のソースで確かめた挙動:

- `--whitelist-custom-nodes` は `nargs='+'` のフォルダ名の一覧で、`main.py` と `nodes.py` が `--disable-all-custom-nodes` と一緒に読み、一覧にあるフォルダだけを読み込む（`possible_module in args.whitelist_custom_nodes`）。
- prestartup のスクリプトも、一覧のフォルダだけが実行される。

`Enabled` が返す条件: ①記録（`nodes.json`）が許可リストの今のコミット・アーカイブ SHA-256 と一致する、②フォルダの**ツリーのダイジェスト**（ファイル名と内容の SHA-256。`__pycache__` も数える）が導入時に封をした値のまま。どちらかが崩れたノードは `outdated`（mam を更新して固定が変わった）・`modified`（ファイルが書き換わった）として画面に出し、**読み込まない**（導入し直しで直る）。エンジンは `PYTHONDONTWRITEBYTECODE=1` で起動する（`engine.Installer.Command` の `Env`）ので、Python 自身が `.pyc` を書くことはなく、`__pycache__` に置かれた `.pyc`（元のソースの代わりに実行される）はダイジェストを変えて `modified` になる。

## 4. 導入・削除（`modelnodes.Service`）

- **取得**: 固定のコミットの GitHub アーカイブを `fetch.Download`（許可ホスト・リダイレクト先の再検査・`.part` の再開・SHA-256 とサイズの検証。エンジン・モデルと同じ部品）で取る。取得のあと、zip を**一度だけメモリに読み込んでサイズと SHA-256 をもう一度照合し**（`readPinned`。ノードの zip は最大でも数十 MB）、展開はそのバイト列から行う（`extract.ZipFrom`。パスから開き直さない）。検証のあと展開の前に zip をすり替えられても（データディレクトリに書ける者）、展開には届かない。照合が崩れていれば `ErrArchiveChanged` で失敗し、zip を消す。展開は `..`・絶対パス・シンボリックリンクを拒否し、`extracted_size` を超える展開を断る。git・pip・スクリプトの実行は無い。GitHub のアーカイブは、まれに圧縮方式の更新でバイト列が変わる。その場合は SHA-256 の不一致で導入が失敗し（安全側）、許可リストのピンを更新して対処する（MuseTalk のランタイムのアーカイブと同じ前例）。
- **同意が先**: `Install` は、ライセンスの ID・版と注意事項の版（`NoticeVersion`）の同意が記録されていなければ、何も取得せずに拒否する（画面の同意フォームも MCP の `accept_custom_node_license` も記録するだけで、強制するのはサービス）。文面か版が変わると、改めて同意が要る。
- **ジョブ**: ジョブの種類は既存の `model_install`、`target` は `node:<id>`（[model-manager.md](model-manager.md) §5.1）。ハンドラーは `Service.Handler(next)` で包み、`node:` 以外は次のハンドラー（モデルマネージャー → ローカルランタイムの順に包む。`app/services.go`）に渡す。進み具合は、取得 50%・展開 30%・再起動と確認で 100%。中断は `POST /model-manager/nodes/:id/cancel`（MCP: `cancel_model_install`）で、ダウンロード済みの分は残る。
- **入れ替え**: 動いている ComfyUI は止め、展開と封印を済ませたフォルダを入れ替えて、記録を書き、**動いていたなら再起動して、`/object_info/<class>` で `node_classes` が読み込まれたことを確かめる**。読み込まれなければ、ノードを削除して失敗にする（動かないノードを「導入済み」にしない）。止まっていた ComfyUI は起動しない（次の起動で読み込まれる）。
- **削除**: エンジンを止め、フォルダと記録を消し、動いていたなら再起動する。そのノードを使うモデルは導入済みのまま残り、ノードを入れ直すまで選べない（§6）。導入中の削除は 409。起動時に `CleanLeftovers` が、途中で止まった導入の残り（`.new`・`.old`）を片づける。
- **状態**: `not_installed`・`installed`・`outdated`・`modified`・`blocked`。

## 5. 依存パッケージの決め方

1. ノードの全 `.py` の import を読み、`python_requires`（読み込み時）と `python_optional`（一部のノードが実行時に使う）に分ける。
2. `python.json`（同梱の ComfyUI v0.42.1 の `requirements.txt`）と突き合わせる。足りないものが `python_requires` にあれば `install_blocked` を書く。`python_optional` だけが足りないなら導入でき、`limits` にそのノードが動かないことを書く。
3. ComfyUI のエンジンを更新して `python.json` の元のバージョンが変わると、`Validate` が一覧を読み直すよう失敗する。

### 5.1 同梱の方針（Issue #993）

**mam は、ComfyUI の同梱環境に Python パッケージを足さない。** 同梱の環境は ComfyUI 公式のポータブル版そのままで、mam が保証する依存は `python.json`（本体の `requirements.txt`）だけ。決めた理由:

- **取得物の固定が合わない**: mam は取得するものすべてにコミット・SHA-256 を固定する。パッケージを足すと、OS・Python の版・CUDA ごとの wheel（opencv・onnxruntime-gpu・mediapipe は数十〜数百 MB）を 1 つずつ固定・検証して持つことになり、ComfyUI のエンジンを更新するたびに、ABI の合う wheel・依存の版の食い違いを mam が追い続ける。
- **ビルドが要るものがある**: insightface は Cython の拡張を C コンパイラで組む（sdist しか無い版がある）。mam は pip も、コンパイラも実行しない。
- **ライセンスが広がる**: 推移依存のパッケージごとにライセンスを確かめて `THIRD_PARTY_NOTICES.md` に載せる範囲が、許可リストの 1 ノードではなく環境全体に及ぶ。
- **効果が小さい**: 要るパッケージの多くは、ノードが実行時に取得する前処理の重み（非商用・不明のものを含む）とセットで使うもので、パッケージだけ足しても導入できる状態にならない（§5.2〜5.4）。

だから、`python.json` に無い依存が `python_requires` にあるノードは `install_blocked` のまま導入しない（`nodecat.Validate` が強制する）。やりたいことが 1 つの処理なら、**mam の中で行う**（§5.2）。将来この方針を変えるときは、①固定した wheel（URL・SHA-256・サイズ・対応する OS/Python/CUDA）を許可リストの項目として持ち、②ノードの外に隔離した置き場所へ展開し、③実機（Windows）で `/object_info` まで確かめる、という設計を別の Issue で決める。今は作らない。

### 5.2 前処理を mam の中で行う（controlnet_aux を入れない。Issue #993）

`controlnet_aux`（`controlnet-aux`）の導入と、mam 内での前処理を比べた結果、**Canny だけを mam の中（Go）で行う**ことに決めた。

| 前処理 | 決定 | 理由 |
|---|---|---|
| Canny（輪郭） | **mam の中で作る**（`imgprep.Canny`） | 重みも Python も要らない古典的なアルゴリズムで、純 Go（`image`・`golang.org/x/image`）で書ける。ライセンスの問題（重み）が無い。導入ボタン・同意・ノードの再起動も要らず、同梱エンジンでも外部の ComfyUI でも同じに動く |
| OpenPose・DWPose（ポーズ） | 作らない | 重みが要る（DWPose は `yzd-v/DWPose` が Apache-2.0 だが、`lllyasviel/Annotators` の OpenPose の重みは `license: other` で本文が無い）。ONNX の実行環境（Go の onnxruntime の同梱）も要る。ポーズは Shisei のポーズ集（`pose` の役割）で渡せる |
| 深度（Depth Anything V2 など） | 作らない | Depth-Anything-V2-Large は CC-BY-NC-4.0（非商用）。重みと推論環境が要る |
| 線画・エッジ（HED・PiDiNet・LineArt） | 作らない | `lllyasviel/Annotators` の重みで、ライセンスが `other`（本文なし）。商用利用の可否が確かめられない |

- **どこで行うか**: ワークフローの `reference_preprocess`（`localcfg.Workflow.ReferencePreprocess`。今は `canny` だけ。`controlnet` のモードだけに付けられ、`imgprep.Kinds` の外は `Validate` が拒否する）が付いたテンプレートでは、ComfyUI の `/upload/image` に送る**前**に、参照画像（PNG・JPEG・WebP）から制御画像（白い線・黒地の 8 ビットのグレーの PNG）を作って差し替える（`comfyui/graph.go` の `upload`）。長辺 1024 px を超える画像は縮小してから検出する（ControlNet は制御画像を生成のサイズに合わせる）。画像として読めないもの・6,700 万画素を超えるものは 422 で断る。
- **すでにエッジ画像のもの**（黒と白だけで、白が半分以下: 利用者が作った制御画像）は**そのまま**渡す。二重に輪郭を取ると線が倍になるため。黒い線の白地の絵は、写真と同じく輪郭を取る。
- **アルゴリズム**: ガウスぼかし（σ=1）→ Sobel（|gx|+|gy|）→ 勾配方向の非極大抑制 → しきい値 100・200 のヒステリシス（`controlnet_aux` の `CannyEdgePreprocessor` の既定と同じしきい値）。OpenCV の実装とは丸めが違い、画素単位では一致しない（ControlNet の制御画像としては同じ役目）。
- **対象**: テンプレート `txt2img-sdxl-controlnet`（`reference_preprocess: "canny"`）。これにより、画面・MCP の生成テストの参照画像に**写真をそのまま渡せる**（従来は線画を自分で用意する必要があった）。

### 5.3 PuLID（Issue #997）を導入できるようにしない理由

PuLID-Flux-Chroma を mam から導入できる経路は、**作らない**。確かめた事実（2026-11-03 取得の一次情報）と判断:

1. **InsightFace の重みは商用に使えない**: `deepinsight/insightface` の README は「コードは MIT。ただし、アノテーション付きの学習データ（とそれで学習したモデル）は**非商用の研究用途のみ**。リポジトリから手で取っても、Python ライブラリで自動で取っても同じ」と書き、`model_zoo/README.md` の冒頭は「ALL models are available for non-commercial research purposes only」、`python-package/README.md` も「non-commercial research only, whether downloaded automatically or manually」。antelopev2 はこのモデル集のもので、PuLID のノード（`PulidFluxInsightFaceLoader`）が使う。buffalo_l などは商用ライセンスの問い合わせ先（`recognition-oss-pack@insightface.ai`）が別に書かれている。
2. **「商用不可の表示と同意つきの導入」でも足りない**: 同意の画面を足すことはできるが、①依存（insightface は Cython のビルドが要る。facexlib・onnxruntime・timm・ftfy・regex）が同梱の Python に無く、pip を実行しない方針（§5.1）で導入できない。②ノードは antelopev2・facexlib の顔解析・EVA02-CLIP の重みを実行時に**固定も検証もなしで**取得し、mam の固定・SHA-256 の方針に合わない。③ノードのコードのライセンスが LICENSE（Apache-2.0）と README（MIT）で食い違い、著作権者の欄も空。導入の経路を作っても、動く構成にならない。
3. **商用の代替は確かめられない**: `lldacing/ComfyUI_PuLID_Flux_ll`（Apache-2.0）の README は FaceNet のローダー（`PulidFluxFaceNetLoader`）を「商用で使える」と書くが、その `requirements.txt` は insightface・facexlib・onnxruntime を引き続き要求し、`facenet-pytorch` は `pip install --no-deps` が要る（pip は実行しない）。FaceNet の重み（`facenet-pytorch` の `vggface2`・`casia-webface`）は VGGFace2・CASIA-WebFace のデータで学習したもので、そのデータの利用条件を確かめていない（README の主張は一次の証拠にならない）。
4. **顔の identity は別の経路で足りる**: Qwen-Image の編集モデルの `face` の役割（#960。参照の強さ既定 0.9）が、ノードも非商用の重みも要らずに参照の顔を渡す。

許可リストの `pulid-flux-chroma` は、コミット・SHA-256・ライセンスの調査結果の記録として残し、`install_blocked` を付けたままにする。前提が変わった（①依存の同梱の経路が §5.1 の設計で作られ、②商用で使える顔の解析の重みが一次の証拠つきで見つかった）ときに、導入できるようにする。ユーザーに代わって規約に同意する操作は、実装にも運用にも含めない。

### 5.4 VNCCS_Utils のポーズを画面なしで渡せるか（Issue #993・#985 ルート③④）

コミット `334eee54` のソース（`nodes/pose_studio.py`・`nodes/posestudio/library.py`・`nodes/factory3d/*`）を読んだ結果、**渡せない**。

- `VNCCS_PoseStudio.generate` は、`unique_id`（ComfyUI の実行器が必ず渡す隠し入力）がある限り、毎回 `vnccs_req_pose_sync` をブラウザへ送り、画面が `temp/vnccs_debug_<id>.json` に書く結果を最長 150 秒待つ。結果が無ければ `Live capture timed out` で失敗する。画面の取り込みが無いまま通る分岐は、保存済みの `capture_id` のキャッシュだけで、それも 1 度はブラウザで撮る必要がある。
- `pose_image`（写真からのポーズ取り込み）は SAM 3D で JSON にしたあと、`vnccs_apply_sam3d_pose` をブラウザへ送って返りを最長 150 秒待つ。これも画面が要る。
- 3D の人形の絵は、「バックエンドの 3D 描画は削除した」とソースにあるとおり、**ブラウザの WebGL**（three.js。esm.sh から読む）で描かれる。`VNCCS_3DFactory` も同様に画面の取り込みを待つ。

ファイルを書いて画面を迂回する方法（`temp/vnccs_debug_*.json` に `captured_images` を書く）は、ノードの内部の取り決めに頼った非公式の経路で、ノードの更新で壊れ、導入できない（§5.1）うえ、人形の絵そのものを mam が作れるわけではないので、**採らない**。

- **ルート③（人形の画像でポーズを写す）**: ノード無しで足りる。Shisei のポーズ集（#986。`pose` の役割）の人形の画像 + LoRA（`qwen-image-21-vnccs-pose`。#985・#984）。
- **ルート④（PoseStudio で人形を自分で動かす）**: ブラウザの画面が要るため、mam からは提供しない。ComfyUI の画面で PoseStudio を使って作った画像を、生成テストの参照画像（`pose` の役割）として渡す運用は、今のまま可能。
- **実行時の自動取得の revision・SHA-256 の固定**: ノードのソースを読んだ範囲では、ポーズ集の取得（`api.repo_info`・`hf_hub_download`）に `revision` が無く、manifest の `json_sha256`・`preview_sha256` は取得元のリポジトリが持つ値で、mam が固定した値ではない（`factory3d`・SAM 3D の重みだけが `revision` を固定している）。ノードのコードを書き換えて固定すると、封印したツリーのダイジェスト（§3）が変わり、許可リストの固定（コミット・SHA-256）と合わなくなる。導入しないので、この固定は行わない。将来導入するときの前提は、①画面なしでポーズを渡す公式の経路がノードに入ること、②ポーズ集の取得を `HF_HUB_OFFLINE=1` で止め、mam がポーズ集のファイルを固定して置くこと。

## 6. ノードを必要とするモデル（`requires_nodes`）

- テンプレートの `model.workflow.requires_nodes`（`localcfg.Workflow.RequiresNodes`）に、必要な許可リストの id を書く。テンプレートの全クラスは、コアのノードか、その `requires_nodes` のノードの `node_classes` でなければならない（`localcfg_test.go`）。
- `nodegate.Check` が `InstallModel` の入口で、ノードが読み込まれていなければ `InvalidError`（「追加ノード「…」が導入されていないため使えません。モデルマネージャーの「追加ノード」の節…」）で拒否し、ジョブを作らない。同じ理由で、生成テストの一覧（`gentest/modelcatalog`）はそのモデルを `Available=false`・`Unavailable=missing_node` にして、メッセージを付ける。**同梱エンジン（`bundled-comfyui`）のモデルだけが対象**で、外部の ComfyUI は自分のノードを持つので止めない。
- 画面のモデルのカードには「追加ノードが必要」のバッジと説明を出し、導入ボタンを無効にする。

### 6.1 コアのノードだけの ControlNet と IP-Adapter

- ControlNet（コアのノード）: モデル `sdxl-controlnet-canny`（`xinsir/controlnet-canny-sdxl-1.0`、Apache-2.0）。テンプレート `txt2img-sdxl-controlnet`（`LoadImage` → `ControlNetApplyAdvanced`）。**追加ノードは要らない**。参照画像は**写真でも絵でもよい**: テンプレートの `reference_preprocess: "canny"` で、mam が Go で Canny のエッジ画像を作ってから渡す（§5.2。すでにエッジ画像ならそのまま使う）。
- IP-Adapter: モデル `sdxl-ipadapter-plus`（`h94/IP-Adapter` の `ip-adapter-plus_sdxl_vit-h` と CLIP ViT-H の画像エンコーダー）。テンプレート `txt2img-sdxl-ipadapter`（`requires_nodes: ["ipadapter-plus"]`）。
- 参照画像のモード `controlnet`・`ipadapter`（`localcfg.ModeControlNet`・`ModeIPAdapter`）: 参照画像をちょうど 1 枚要求し（ない実行は、制御画像／IP-Adapter の参照画像が要ると言って拒否）、参照の強さ（`reference_strength`、既定 0.7）がノードの強さに入る。生成テストの参照画像の経路（[../gentest/gentest.md](../gentest/gentest.md)）をそのまま使う。

## 7. 画面・MCP

- 画面: モデルマネージャーの区画「追加ノード」（`#mm-nodes`）。ノードごとのカード（状態・商用利用のアイコン・導入できない理由・このノードを使うモデル）と、詳細モーダル（固定したコミット・アーカイブの SHA-256・ライセンスと商用利用の答え・必要な Python パッケージ・追加されるクラス・ソースの確認結果・注意事項）。導入ボタンは同意のダイアログ（注意事項・ライセンス・コミットと SHA-256 の予告・同意のチェック）を開く。ルートは [../../api/endpoints-model-manager.md](../../api/endpoints-model-manager.md)、見た目は [../../components/model-manager-screen.md](../../components/model-manager-screen.md)。
- MCP: `list_custom_nodes`・`get_custom_node_status`・`accept_custom_node_license`・`install_custom_node`・`remove_custom_node`（[../../api/mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)）。MCP のエージェントが同意を記録できる（`accept_custom_node_license`。`accept_model_license` と同じ扱いで、ユーザーが内容を確認した後にだけ呼ぶ）。ただし同意が記録されていなければ `install_custom_node` は何も取得せずに拒否する。

## 8. ノードの足し方（#960 PuLID・#985 PoseStudio など）

| 足すもの | ファイル | 内容 |
|---|---|---|
| ノードの定義 | `backend/internal/modelmgr/nodecat/data/nodes/<id>.json` | §2 の全項目。リポジトリの LICENSE の本文を読んで `license` を書き、ソースを読んで `review` を書く。コミットは 40 桁、アーカイブの SHA-256・サイズは `curl -L <archive_url>` で測って、github.com と codeload の両方で一致を確かめる |
| 依存パッケージ | `nodecat/data/python.json` | 通常は触らない（同梱の環境にあるものだけ。§5.1）。足りない依存があれば、エントリに `install_blocked` を書く（pip は実行しない。依存を同梱の環境に足さない方針は §5.1 で決めた。1 つの処理で足りるなら §5.2 のように mam の中で行う） |
| モデル（重み） | `backend/internal/modelmgr/catalog/data/models/<id>.json` | 通常のモデルと同じ（コミット固定・SHA-256・ライセンス）。ノードが登録するフォルダがあれば、`catalog/validate.go` の `modelFolders` に足し、ローダーのクラスと入力名を `loaderInputs` に足す（すべてのファイルがどれかのワークフローのローダーから読まれることを検証するため）。同じフォルダを `service/modelmanager/external.go` の `modelFolders` にも足す（外部の ComfyUI の一覧用） |
| テンプレート | `backend/internal/agent/modeladapter/localcfg/templates/image/<name>.json` | `model.workflow.requires_nodes: ["<id>"]`。割り当て（`params`）とモード（`reference.go`）は既存のもので足りなければ、`workflow.go` に**追加的に**足す（既存の値は変えない）。**全テンプレートの規約**: `prompt_weight`（`supported`・`weak`・`unsupported`）と、サンプラーのノードへの `steps`・`cfg`・`sampler`・`scheduler` の割り当てを付ける（`localcfg/samplingtest`・`comfyui/samplingtest` が検出する。#973）。ControlNet・IP-Adapter のように参照画像が要るテンプレートは、`comfyui/samplingtest/rig_test.go` が参照画像を付けて走らせる |
| テストの許可 | `localcfg_test.go`・`controlnet_test.go` | コアのノードの一覧（`coreNodes`）には足さない（ノードのクラスは `node_classes` から読まれる） |
| 同意の文面 | `service/modelnodes/view.go` | 注意事項を変えたら `NoticeVersion` を変える（改めて同意が要る） |
| 仕様 | このファイルの §2.1 の表と [model-manager.md](model-manager.md) §4.2、[model-manager.changelog.md](model-manager.changelog.md) | 足したノードの行を足す |

カタログのテスト（`catalog.Validate`）と `nodecat.Validate` は、足したものに足りない項目があると失敗する。画面・MCP・`InstallModel` の拒否・生成テストの一覧のガードは、`requires_nodes` を書けば自動で効く。

## 9. 未確認（実機が要る）

- 実機の GPU での ControlNet・IP-Adapter の生成（構造テストと偽のエンジンまで）。Canny のエッジ画像が、実際の写真で ControlNet の制御画像として効くか（`imgprep` は合成画像の輪郭で検証した。OpenCV の出力との画素単位の一致は見ていない）。
- InsightFace 以外の商用で使える顔の解析の重みの有無（§5.3。見つかるまで PuLID は導入できない）。
- VNCCS PoseStudio の画面なしの経路（§5.4。ノードに公式の経路が入るまで提供しない）。
- Windows の実機での、ノードの導入・ComfyUI の停止と再起動・`/object_info` での読み込みの確認。
- 実際の GitHub のアーカイブのダウンロード（SHA-256 は 2026-11-03 に取得した値）。
- IP-Adapter のノードが `ipadapter` フォルダ・CLIP Vision のフォルダのファイルを読むこと（ノードの `model_folders` の登録はソースで確認、読み込みは未実行）。
