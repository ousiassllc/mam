# モデルマネージャー: Hugging Face からのモデルの導入

[model-manager.md](model-manager.md) の続き（`model-manager.md` が行数上限に近いため別ファイル）。カタログに無いモデルのファイルを、**利用者が選んだ Hugging Face（HF）のリポジトリから、同梱の ComfyUI に導入する**。実装は `backend/internal/modelmgr/hf`（HF の API クライアントと偽の HF `hftest`）・`modelmgr/hfplan`（判定・固定・名前・フォルダ・ワークフロー）と、`service/modelmanager` の `hfimport.go`・`hfrun.go`（Service の部分。年齢ゲート・トークン・記録・ジョブ）。画面は [../../api/endpoints-model-manager.md](../../api/endpoints-model-manager.md)、MCP ツールは [../../api/mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)。Issue #842。

## 1. 範囲

| 決めたこと（Issue #842 の「決まったこと」） | この実装 |
|---|---|
| 1. 入口は URL・リポジトリ ID の貼り付けと、キーワード検索 | `HFRepo`（URL・`owner/repo`）・`SearchHF`（キーワード。`pipeline_tag` の絞り込み） |
| 2. GGUF は一覧に出すが選べない | `installable=false`、理由 `ReasonGGUF`（ComfyUI-GGUF のカスタムノードが要る） |
| 3. pickle 形式は拒否 | `.ckpt` `.pt` `.pth` `.bin` `.pkl` ほか。`ReasonPickle`。導入しない（API でも 422） |
| 4. フォルダは自動判定し、利用者が確認する | パス（`vae/`・`loras/` など）→ safetensors のヘッダー（§4）の順に推定。画面は既定値を選んだフォームを出し、利用者が変えられる |
| 5. ワークフローのテンプレートは SDXL の一体型チェックポイントだけ | ヘッダーが SDXL のチェックポイントだけ、`txt2img-sdxl`・`img2img-sdxl` の `ckpt_name` を差し替えた 2 つのモデルを作る。**LoRA を載せるワークフローは作らない**（LoRA はファイルを置くだけ） |
| 6. NFAA は年齢確認が済むまで隠す | §6 |
| 7. 利用者が足したモデルは `state.json` に記録 | `State.UserModels`（鍵はパス `models/<folder>/<name>`）・`State.HFConsents` |

範囲外: Civitai、GGUF・pickle の導入、カスタムノード、外部の ComfyUI への導入、任意のワークフローの生成、gated の利用申請を mam から行うこと。

## 2. 入力と取得元（`hf.ParseInput`・`hf.Client`）

- 受け付けるのは `owner/repo` と、`https://huggingface.co/<owner>/<repo>`（`/tree/<rev>[/<dir>]`・`/blob/<rev>/<path>`・`/resolve/<rev>/<path>` を含む）。`blob`・`resolve` はそのファイルを「選択中」にする。`datasets/`・`spaces/`、`http://`、ポート・ユーザー情報・`?`・`#` つきの URL、`hf.co` の短縮 URL は `*InvalidError`（`hf.ErrInput`）。
- **取得元は `huggingface.co` と `*.hf.co` だけ**。`fetch.HFAllowedURL`（`safefetch` の検査に、この 2 つのドメインとそのサブドメインだけの許可リスト）を、API・ダウンロード・リダイレクトのすべてに使う。GitHub（`github.com`・`githubusercontent.com`）は同梱エンジンの取得（`fetch.AllowedURL`）だけのもので、HF の取得はそこへリダイレクトされても断る。`resolve` は 302 で CDN（`*.hf.co`）へ向かう。
- **トークンは `huggingface.co` への最初の要求にだけ付ける**（CDN・別ホストへのリダイレクトでは外す）。値は画面・MCP・ログ・エラーに出さない。
- 429 は最大 3 回、`ratelimit` ヘッダーの待ち時間（読む秒数は 1 日で頭打ち）で再試行し、それでも駄目なら「HF のレート制限」の利用者向けメッセージ。JSON の応答の大きさは 8 MiB、ツリーは最大 5000 件・100 ページ（超えたら、同じカーソルが戻ってきたときも、`truncated`）。LFS の `oid` が 64 桁の 16 進でなければ SHA-256 とは見なさない（LFS 無しのファイルとして断る）。

## 3. 一覧と固定（`hfplan.Browse`・`Pin`）

1. `revision/<rev>` でコミットのハッシュ・gated・タグ（NFAA）・ライセンス（`cardData`）を読み、**以後の要求はすべてそのコミットに固定**する（`resolve/<commit>/<path>`。ブランチ名・タグ名のままでは取得しない）。改名されたリポジトリ（307）は新しい名前へ追う。**NFAA は「そのコミットのタグ」と「リポジトリの今の先頭（`revision/main`）のタグ」の OR**（古いコミットはタグが付く前の `cardData` を持つことがあり、コミットを名指しするだけでゲートを越えられないようにする）。`Browse`・`Pin`（導入時の読み直しを含む）・検索・MCP・画面のすべてがこの `hf.Client.Revision` を通る。タグは大文字小文字を区別せず、`nfaa` も NFAA と見なす（`hf.IsNFAA`）。
2. ツリーを、`Link` ヘッダーのカーソルで読む。モデルのファイル（拡張子 `.safetensors` `.sft` `.gguf` と pickle 系）だけを残す。
3. `paths-info`（`expand=true`）で、LFS の SHA-256（`lfs.oid`）・サイズ・セキュリティのスキャン（`securityFileStatus`）を読む。
4. 各ファイルを判定する（`hfplan.Judge`）。導入できるのは、**LFS の `.safetensors`/`.sft` で、単一ファイル（分割形式でない）・サイズが 0 でなくツリーと LFS が一致・スキャンが `unsafe` でない**もの。できないものは理由（`ReasonGGUF`・`ReasonPickle`・`ReasonSplit`・`ReasonNoLFS`・`ReasonUnsafe`・`ReasonEmpty`）を付けて一覧に残す。
5. 同じモデルの版（`fp8`・`bf16` など）は、ファイル名から `group`・`variant` を作ってまとめる。VRAM の目安（§5）と、導入済みの印（同じ SHA-256 の導入済みファイル）、動いている導入ジョブ（`install_job`）を付ける。

`InstallHFFile` は、画面や MCP が渡す SHA-256 やサイズを**信用しない**。渡されるのは `repo`・`commit`・`path` だけで、Service が `Revision`（コミットが自分自身に解決されること）・`PathsInfo`・`Judge` をやり直し、その結果を固定として使う。ダウンロードは `fetch.Download`（再開・空き容量・SHA-256 とサイズの検証。合わなければファイルを残さない）で、導入のジョブ（`model_install`、`target` は `hf:<repo>@<commit>/<path>`）として動く。

導入できる条件: 同梱エンジンがある（Windows 版）・エンジンが導入済み・`--model-config` で固定されていない。gated のリポジトリはトークンとアクセス権が要り、無ければ（ダウンロードの前に）理由とリポジトリのページを案内する `*InvalidError`（mam からアクセスの申請はしない）。

## 4. フォルダと名前

- **フォルダ**は `checkpoints`・`diffusion_models`・`text_encoders`・`vae`・`loras` の 5 つだけ（カタログと同じ）。パスにこれらの名前（または `split_files/<folder>/`）があればそれ。無ければ safetensors のヘッダー（先頭 8 バイトの長さ + JSON を `Range` で読む。上限 8 MiB。一度に 12 ファイル・4 並列まで）のテンソル名から推定する（`hfplan.Infer`）: LoRA のテンソル名（`lora_unet_`・`lora_te`・`.lora_A.`・`.lora_down.`・`.alpha`）→ `loras`、`model.diffusion_model`・`conditioner`/`cond_stage_model`・`first_stage_model` がそろう一体型 → `checkpoints`（family `sdxl`／`sdxl-refiner`／`sd1`）、`model.diffusion_model` のみまたは transformer のテンソル名（`double_blocks.` など）→ `diffusion_models`、`text_model.`・`encoder.block.`・`model.layers.` → `text_encoders`、`encoder.`+`decoder.`（と `quant_conv`）だけ → `vae`。**判定表に無い構成は空のまま**にし、利用者が選ぶ（API は `folder` の指定が無ければ `*InvalidError`）。
- **ファイル名**は `[A-Za-z0-9][A-Za-z0-9._+-]*` の `.safetensors`/`.sft`・120 文字まで・Windows の予約名でない。リポジトリ内の名前から作る（`SanitizeName`）。`name` で上書きできる。
- **衝突**: 同名で中身の違うファイルがあれば上書きせず `*InvalidError`（別の名前を提案する）。同名で SHA-256 とサイズが記録と同じなら導入済みとして扱う。記録に無い既存ファイルは、サイズが同じならハッシュして合えば採用し、違えば同様に断る。
- **同じ宛先への導入は直列にする**: 導入のジョブは、ジョブの対象（`hf:<repo>@<commit>/<path>`・`model:<id>`）のロックに加えて、**書き込み先（`models/<folder>/<name>`、大文字小文字は区別しない）ごとのロック**を取る（`Service.lockDests`。複数あるときは並べ替えた順に取り、互いに待たない）。ロックは記録を書くまで持ち、**ロックを取った後で宛先の検査（`CheckDest`）をもう一度行う**。要求の時点では宛先が空だった 2 つの導入（同じ名前の別のリポジトリ、HF とカタログの同名ファイル）は 1 つずつ動き、後のものは先のものの記録を見て上書きせずに失敗する。**カタログのモデルの導入も、同じ宛先に利用者が足した別の SHA-256 のファイルがあれば上書きしない**（要求のときと、ロックを取った後のジョブで `*InvalidError`）。同じ SHA-256・サイズなら両方の記録に載せて共有する。
- 同じファイルの導入が動いている間に、別のフォルダ・名前でもう一度頼むと、先のジョブを返さず `*InvalidError`（先のジョブの宛先を示す）。同じ宛先ならこれまでどおり同じジョブ（`Existing`）を返す。
- SDXL のチェックポイントのモデル名は `hf-<slug>-<sha6>`（と `-img2img`）。ワークフローは埋め込みのテンプレートの `ckpt_name` だけを差し替える（`hfplan.WithCheckpoint`）。作ったモデル名は `UserModel.Workflows` に記録し、削除で一緒に消す。

## 5. VRAM の目安（要確認 → 保守的に決めた）

重みの大きさ × 1.2（`VRAMFactor`）を必要量とし、GPU の VRAM の 85% 以下なら `fits`、100% 以下なら `tight`、それより大きければ `over`、GPU が分からなければ `unknown`。目安であり、導入を止めない。

## 6. 年齢ゲート（#841）と NFAA

- リポジトリのタグ `not-for-all-audiences`（NFAA）は成人向けとして扱う（§3 のとおり、コミットのタグと今の先頭のタグの OR）。**Service が強制する**（画面だけではない）: 年齢確認（`AdultContentEnabled()`）が済むまで、`SearchHF` の結果から外し、`HFRepo`・`InstallHFFile` は `ErrAdultNotConfirmed`（409）、ツリーを読む前に断る。古いコミットの URL（`tree/<commit>`・`blob/<commit>`・`resolve/<commit>`）や `install_hf_file` の `commit` を名指ししても同じ。
- 導入済みの NFAA のファイルは、ゲートが閉じている間 `UserModels()` から隠れ、作ったモデルは `HidesModel` で隠れる（試し生成の選択肢にも出ない）。`UserModels()` の `shared_with` も、ゲートが閉じている間は成人向けのカタログのモデルを挙げない。キューに入った後でゲートが閉じたら、実行時に `ErrAdultNotConfirmed` で失敗する。`DeleteUserModel` はゲートに関係なく動く（片付けは常にできる）。
- 年齢の確認を自動で済ませる仕組みは無い（`set_adult_content` と画面のダイアログだけ）。

## 7. 注意書き（terms）の確認

導入の前に、利用者が **ライセンス（HF のカードの記載）と取得元を自分で確かめた**ことを確認する（`hfplan.Terms`・`TermsVersion`）。確認は**リポジトリ+コミットごとに一度**で、`State.HFConsents`（版・ライセンスのスナップショット・日時）に残る。確認が無い要求は `ErrConsentRequired`（409）、渡した版が違えば `*InvalidError`。ライセンスの記載が無い（または `other` で名前も無い）リポジトリには `LicenseWarning` を出す。**mam はファイルの中身・安全性・権利関係を確認していない**ことを明記する。

## 8. 記録・削除・一覧

- **`UserModel`**（鍵 `models/<folder>/<name>`）: リポジトリ・コミット・リポジトリ内のパス・SHA-256・サイズ・フォルダ・ライセンスのスナップショット・gated・NFAA・family・作ったモデル名・日時。
- **`UserModels()`**: 新しい順。`present`（ファイルが実在し大きさが合う）・`shared_with`（同じファイルを持つ導入済みのカタログのモデル）・`model_ids` を付ける。
- **`DeleteUserModel(path)`**: 画像用途・Quality Policy が作ったモデルを使っていれば `*InvalidError`、導入中は `ErrBusy`、記録に無いパスは `ErrNotInstalled`。作ったモデル（`local_models`）と記録を消し、**カタログのモデルが同じファイルを持っている間（記録済みのものと、導入が動いているもの）はファイルを残す**（カタログの削除も、利用者のモデルが持つファイルと、導入が動いている別のモデルのファイルを残す。参照は記録と動いているジョブから数える）。

## 9. 保守的に決めた「要確認」の項目

| 項目 | 決めたこと |
|---|---|
| `hf.co` の短縮 URL | 受け付けない（`huggingface.co` の URL か `owner/repo`）。HF の取得元の許可リストは `huggingface.co` と `*.hf.co`（CDN）だけ（GitHub はエンジンの取得だけ） |
| VRAM の式 | §5。目安であり、導入は止めない |
| ヘッダーによるフォルダ判定の表の範囲 | §4 の 5 種と SDXL の family だけ。知らない構成は空のまま、利用者が選ぶ |
| LoRA のワークフロー | 作らない（決定 5。LoRA はファイルを置くだけ。カタログの基盤モデルに載せる LoRA のテンプレートはこの Issue の範囲外） |
| フォルダ | カタログの 5 つに限る（任意のフォルダは作らない） |
| 記録に無い既存ファイル | サイズが同じならハッシュして採用、名前が合わなければ `*InvalidError`（別の名前を提案） |

## 10. 未検証（実機・実ネットワーク）

| 項目 | 状態 |
|---|---|
| 実物の `huggingface.co` からのダウンロードと、CDN・xet の実際の挙動（リダイレクト先・`Range`・大きなファイル） | **未検証**。偽の HF（`hftest`）で、302・`Range`・再開・SHA-256 の不一致・429・gated の 401・改名の 307 を確認。実物の API の応答の形（revision・tree・paths-info・search・`Link` ヘッダー・429 のヘッダー）は、読み取りだけの要求で確かめた |
| 実物の gated のリポジトリを、実物のトークンとアクセス権で取得すること | **未検証**（トークンの扱いは偽の HF で、API ホストにだけ付くことを確認） |
| GPU の実機での VRAM の検出と `fits`・`tight`・`over` の印 | **未検証**（偽のアダプターの値で確認） |
| 導入した SDXL のチェックポイントで、実物の ComfyUI・GPU による生成 | **未検証**（ワークフローは構造と `ckpt_name` の差し替えだけ） |

## 11. テスト

`hf`（入力の解釈・クライアント・トークンのホスト・429・ページング）、`hfplan`（判定・フォルダ・名前・VRAM・一覧・固定）、`service/modelmanager/integration`（導入・検証失敗・再開・衝突・同じ宛先の同時導入・gated・NFAA（古いコミットを含む）・削除と共有）、`web/handler/modelmgr/hfimport`（画面）、`mcp/tools/modelmgr`（ツール）。いずれも偽の HF（`hf/hftest`）を使い、実物のモデルはダウンロードしない。固定時間の待ちは使わない。
