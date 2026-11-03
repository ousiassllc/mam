# 追加ノード（ComfyUI のカスタムノードの許可リスト）

[model-manager.md](model-manager.md) の同梱 ComfyUI に、**mam が確認して固定したカスタムノードだけを導入し、読み込ませる**仕組み（Issue #976）。ControlNet・IP-Adapter を使えるようにする土台で、PuLID（#960）・VNCCS PoseStudio（#985）はここにノードを足す形で作る（§8）。実装は `backend/internal/modelmgr/nodecat`（許可リスト）・`modelmgr/nodes`（導入・検証）・`service/modelnodes`（導入ジョブ・同意・再起動）・`service/modelmanager/nodegate`（ノードの無いモデルを止める）。

## 1. 方針

- **許可リストにないノードは、読み込まない**。同梱の ComfyUI は `--disable-all-custom-nodes` のまま起動し、`--whitelist-custom-nodes <フォルダ名…>` に「導入済みで、固定した内容のままのノード」のフォルダだけを渡す（§3）。`custom_nodes` に手でフォルダを置いても、リストの外は読み込まれない（テスト: `engine/engine_nodes_test.go`、`nodes/nodes_test.go` の `TestAFolderMamDidNotInstallIsNeverEnabled`）。
- **ノードは任意のコード**: ComfyUI の中で mam を動かすアカウントの権限のまま動く（ファイル・ネットワークを使える）。だから、①許可リストの 1 件ごとに**ソースを読んだ記録**（`review`）と固定したコミット・SHA-256 を持ち、②導入の前に、ライセンスに加えて**注意事項（`modelnodes.SecurityNotice`）への同意**を必ず取る。確認は開発時の 1 度きりで、コードの安全を保証するものではない、と画面にも書く。
- **コアのノードで済むものは、ノードを入れない**。ControlNet は ComfyUI のコアの `ControlNetLoader`・`ControlNetApplyAdvanced` で動くので、追加ノード無しで使える（§6）。画像の前処理（Canny の線画など）は、`controlnet_aux` が要るためノードの導入が要るが、その依存が同梱の Python に無く、**導入しない**（§5）。
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
| `controlnet-aux`（Fannovel16/comfyui_controlnet_aux） | **導入しない**（`install_blocked`） | `a587a4c1…` | Apache-2.0 | 不明 | opencv・scikit-image・mediapipe・onnxruntime などが同梱の Python に無く、mam は pip を実行しない。さらに、実行時に Hugging Face から前処理のモデルを取得し、その重みのライセンスが `lllyasviel/Annotators` は `other`、Depth-Anything-V2-Large は CC-BY-NC-4.0 で、商用利用の可否が確かめられない（`commercial: unknown`） |
| `pulid-flux-chroma`（PaoloC68/ComfyUI-PuLID-Flux-Chroma。#960） | **導入しない**（`install_blocked`） | `52dc5068…`（アーカイブ SHA-256 `48d93b51…`、4,490,131 B、展開後 4,822,332 B） | LICENSE は Apache-2.0 の全文（著作権者の欄は雛形のまま）、README は「元の MIT License を引き継ぐ」で食い違う | 不明 | 4 クラス（`PulidFluxModelLoader`・`PulidFluxInsightFaceLoader`・`PulidFluxEvaClipLoader`・`ApplyPulidFlux`）。insightface・facexlib・onnxruntime・timm・ftfy・regex が同梱の Python に無く、mam は pip を実行しない。InsightFace antelopev2 の重みは非商用の研究用途のみで、同梱も自動の取得もしない。実行時に facexlib・EVA02-CLIP の重みを固定・検証なしで取得する。FLUX・Chroma に参照の顔の identity を足すが、**テンプレートは作らない**（同梱の Chroma1-HD での動作は未確認）。許可リストに固定を残して、依存を足す経路（別の Issue）が決まったときに導入できるようにしておく |
| `vnccs-utils`（AHEKOT/ComfyUI_VNCCS_Utils） | **導入しない**（`install_blocked`） | `334eee54…`（アーカイブ SHA-256 `cc5567fc…`、61,515,840 B、展開後 79,266,802 B、832 ファイル） | MIT（`LICENSE`。取り込んだ ComfyUI-Krea2Edit は Apache-2.0） | 不明 | PoseStudio（`VNCCS_PoseStudio`）はブラウザの画面が `vnccs_req_pose_sync` でポーズを返すのを最長 150 秒待つ作りで、API だけで流す mam のグラフでは動かない（`pose_data` は画面のウィジェットが書く JSON）。さらに、実行時に `huggingface_hub` で Hugging Face からポーズ集（`MIUProject/VNCCS_PoseLibrary_Main`・`Totemistyk/General_Poses_PoseStudio`。画面からの要求で自動更新）・モデルを取得し、`revision` を固定していない箇所がある（mam の固定・SHA-256 検証の方針に合わない）。同梱の Python に cv2・huggingface_hub・omegaconf・yacs・roma・timm・pytorch_lightning・color_matcher・dynamicprompts が無く、mam は pip を実行しない（import 時に要るのは torch・numpy・PIL と ComfyUI 本体だけ）。画面は esm.sh から three・ag-psd を読む。実機の `/object_info` は未確認（入出力は `__init__.py` の `INPUT_TYPES` から読んだ: 必須 `pose_data`、省略可 `pose_image`・`camera_prompt`・`animation_image_batch`、出力 `images`・`lighting_prompt`）。**テンプレートは作らない**（入出力名を実機で確かめられず、導入もできないため）。Qwen-Image 2.1 のポーズは、ポーズ集（Shisei）の画像を `pose` の役割の参照として渡す方法で足りる（[../gentest/gentest.md](../gentest/gentest.md) §6） |

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

## 6. ノードを必要とするモデル（`requires_nodes`）

- テンプレートの `model.workflow.requires_nodes`（`localcfg.Workflow.RequiresNodes`）に、必要な許可リストの id を書く。テンプレートの全クラスは、コアのノードか、その `requires_nodes` のノードの `node_classes` でなければならない（`localcfg_test.go`）。
- `nodegate.Check` が `InstallModel` の入口で、ノードが読み込まれていなければ `InvalidError`（「追加ノード「…」が導入されていないため使えません。モデルマネージャーの「追加ノード」の節…」）で拒否し、ジョブを作らない。同じ理由で、生成テストの一覧（`gentest/modelcatalog`）はそのモデルを `Available=false`・`Unavailable=missing_node` にして、メッセージを付ける。**同梱エンジン（`bundled-comfyui`）のモデルだけが対象**で、外部の ComfyUI は自分のノードを持つので止めない。
- 画面のモデルのカードには「追加ノードが必要」のバッジと説明を出し、導入ボタンを無効にする。

### 6.1 コアのノードだけの ControlNet と IP-Adapter

- ControlNet（コアのノード）: モデル `sdxl-controlnet-canny`（`xinsir/controlnet-canny-sdxl-1.0`、Apache-2.0）。テンプレート `txt2img-sdxl-controlnet`（`LoadImage` → `ControlNetApplyAdvanced`）。**追加ノードは要らない**。画像の前処理は無いので、**参照画像として、すでに線画になっている画像（Canny の出力など）を渡す**。
- IP-Adapter: モデル `sdxl-ipadapter-plus`（`h94/IP-Adapter` の `ip-adapter-plus_sdxl_vit-h` と CLIP ViT-H の画像エンコーダー）。テンプレート `txt2img-sdxl-ipadapter`（`requires_nodes: ["ipadapter-plus"]`）。
- 参照画像のモード `controlnet`・`ipadapter`（`localcfg.ModeControlNet`・`ModeIPAdapter`）: 参照画像をちょうど 1 枚要求し（ない実行は、制御画像／IP-Adapter の参照画像が要ると言って拒否）、参照の強さ（`reference_strength`、既定 0.7）がノードの強さに入る。生成テストの参照画像の経路（[../gentest/gentest.md](../gentest/gentest.md)）をそのまま使う。

## 7. 画面・MCP

- 画面: モデルマネージャーの区画「追加ノード」（`#mm-nodes`）。ノードごとのカード（状態・商用利用のアイコン・導入できない理由・このノードを使うモデル）と、詳細モーダル（固定したコミット・アーカイブの SHA-256・ライセンスと商用利用の答え・必要な Python パッケージ・追加されるクラス・ソースの確認結果・注意事項）。導入ボタンは同意のダイアログ（注意事項・ライセンス・コミットと SHA-256 の予告・同意のチェック）を開く。ルートは [../../api/endpoints-model-manager.md](../../api/endpoints-model-manager.md)、見た目は [../../components/model-manager-screen.md](../../components/model-manager-screen.md)。
- MCP: `list_custom_nodes`・`get_custom_node_status`・`accept_custom_node_license`・`install_custom_node`・`remove_custom_node`（[../../api/mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)）。MCP のエージェントが同意を記録できる（`accept_custom_node_license`。`accept_model_license` と同じ扱いで、ユーザーが内容を確認した後にだけ呼ぶ）。ただし同意が記録されていなければ `install_custom_node` は何も取得せずに拒否する。

## 8. ノードの足し方（#960 PuLID・#985 PoseStudio など）

| 足すもの | ファイル | 内容 |
|---|---|---|
| ノードの定義 | `backend/internal/modelmgr/nodecat/data/nodes/<id>.json` | §2 の全項目。リポジトリの LICENSE の本文を読んで `license` を書き、ソースを読んで `review` を書く。コミットは 40 桁、アーカイブの SHA-256・サイズは `curl -L <archive_url>` で測って、github.com と codeload の両方で一致を確かめる |
| 依存パッケージ | `nodecat/data/python.json` | 通常は触らない（同梱の環境にあるものだけ）。足りない依存があれば、エントリに `install_blocked` を書く（pip は実行しない。依存を同梱の環境に足すのは別の Issue で決める） |
| モデル（重み） | `backend/internal/modelmgr/catalog/data/models/<id>.json` | 通常のモデルと同じ（コミット固定・SHA-256・ライセンス）。ノードが登録するフォルダがあれば、`catalog/validate.go` の `modelFolders` に足し、ローダーのクラスと入力名を `loaderInputs` に足す（すべてのファイルがどれかのワークフローのローダーから読まれることを検証するため）。同じフォルダを `service/modelmanager/external.go` の `modelFolders` にも足す（外部の ComfyUI の一覧用） |
| テンプレート | `backend/internal/agent/modeladapter/localcfg/templates/image/<name>.json` | `model.workflow.requires_nodes: ["<id>"]`。割り当て（`params`）とモード（`reference.go`）は既存のもので足りなければ、`workflow.go` に**追加的に**足す（既存の値は変えない）。**全テンプレートの規約**: `prompt_weight`（`supported`・`weak`・`unsupported`）と、サンプラーのノードへの `steps`・`cfg`・`sampler`・`scheduler` の割り当てを付ける（`localcfg/samplingtest`・`comfyui/samplingtest` が検出する。#973）。ControlNet・IP-Adapter のように参照画像が要るテンプレートは、`comfyui/samplingtest/rig_test.go` が参照画像を付けて走らせる |
| テストの許可 | `localcfg_test.go`・`controlnet_test.go` | コアのノードの一覧（`coreNodes`）には足さない（ノードのクラスは `node_classes` から読まれる） |
| 同意の文面 | `service/modelnodes/view.go` | 注意事項を変えたら `NoticeVersion` を変える（改めて同意が要る） |
| 仕様 | このファイルの §2.1 の表と [model-manager.md](model-manager.md) §4.2、[model-manager.changelog.md](model-manager.changelog.md) | 足したノードの行を足す |

カタログのテスト（`catalog.Validate`）と `nodecat.Validate` は、足したものに足りない項目があると失敗する。画面・MCP・`InstallModel` の拒否・生成テストの一覧のガードは、`requires_nodes` を書けば自動で効く。

## 9. 未確認（実機が要る）

- 実機の GPU での ControlNet・IP-Adapter の生成（構造テストと偽のエンジンまで）。
- Windows の実機での、ノードの導入・ComfyUI の停止と再起動・`/object_info` での読み込みの確認。
- 実際の GitHub のアーカイブのダウンロード（SHA-256 は 2026-11-03 に取得した値）。
- IP-Adapter のノードが `ipadapter` フォルダ・CLIP Vision のフォルダのファイルを読むこと（ノードの `model_folders` の登録はソースで確認、読み込みは未実行）。
