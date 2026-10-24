# ComfyUI ワークフローの保存・版・生成

ComfyUI のノード画面（[comfy-embedding.md](comfy-embedding.md)）で作ったワークフローを mam に保存し、画像・動画のモデル（Shot の生成方法）として使う（Issue #834）。実装は `backend/internal/repository/comfywf`（保存）・`service/workflows`（サービス）・`mcp/tools/workflows`（MCP）・`web/handler/workflows`＋`web/organisms/workflows`（画面）。テーブルは [er-comfy-workflows.md](er-comfy-workflows.md)。改訂履歴は [comfy-workflows.changelog.md](comfy-workflows.changelog.md)。

## 1. 持ち方

- **ワークフローはプロジェクトをまたぐ 1 つの表**（`comfy_workflow`）。プロジェクトごとには持たない。Shot・用途・Quality Policy は ID と版で参照する。
- 1 行が持つもの: 名前（大文字小文字を区別せず一意）、種類（`image` / `video`）、**接続先**（登録した ComfyUI。既定は同梱版 `bundled-comfyui`）、**画面の形式**（`ui_json`: ノード画面で開き直すため）、**API の形式**（`api_json`: 実行するため）、**割り当て**（`assignment_json`: [local-models.md](../local-models.md) §4 の入力の割り当て表、出力のノード、fps・フレームの刻み・倍数、動画は長さと解像度）、版、時刻。
- **版**は整数。**グラフ（`api_json`）か割り当てが変わる保存**で 1 つ上がる（名前の変更・接続先の変更・同じ内容の保存では上がらない）。実行できない下書き（割り当てが足りない）のあいだは、Take が名指しできないので版を上げず、その版を書き換える。上がるたびに、その版の `api_json` と `assignment_json` を `comfy_workflow_version` に残す（古い Take の版を追え、古い版を実行できるようにするための最小の記録）。
- **削除**は、画像の用途・Quality Policy が選んでいるワークフローは拒否する（`validation`）。Shot の Video Model の固定（LOCK）が指すワークフローは削除でき、Take には識別子が残る。固定の値は未知のモデルとして無視され、その Shot は AUTO と同じ経路で選ばれる（`generation.FixedModel`）。

## 2. モデルとしての公開（開いた集合）

保存したワークフローの**各版**は `comfy:<ワークフロー ID>@v<版>` というモデルになる（`modelreg.WorkflowID`）。

- 識別子は `take.model_name`・`cost_ledger_entry.provider`・Shot の `video_model` の LOCK 値に入る。`@v<N>` を持つので、編集して版が上がっても過去の Take は元の版の識別子のまま。
- **選択肢に出るのは現在の版だけ**: 画像の用途（`images.<purpose>`）の primary / fallback、Quality Policy の primary / fallback、Shot の Video Model（動画のワークフローのみ。画像のワークフローは出ない）。設定が保存した識別子は**最新の版に読み替えて**使う（`localcfg.Latest`。「このワークフローを使う」の意味なので、編集に追従する）。
- **古い版も動く**: スナップショットがある版は、実行できる（`run_workflow` の `version`、Shot の LOCK）。Shot が古い版に固定されているあいだ（`lock` の `video_model` がその識別子）、その版は Video Model の選択肢に残り、LOCK の検査を通る。固定する人がいなくなれば選択肢から消える。新しく固定できるのは現在の版だけ（`set_shot_workflow` の `version` は現在の版のみ。古い版は `validation`）。
- 公開は `service/workflows.Publish`（起動時にモデル設定の前、変更のたびに `localmodels.Reload` の前）が `localcfg.PublishWorkflows` へ入れ、`configcheck.Load` が `Config.Workflows` に付け、`localbackends` が版ごとのバックエンド（`comfyui.Backend` に保存したグラフと割り当てを渡したもの）を作り、`modelreg`（選択肢・表示名）と `modeladapter` の能力・プロンプト変換に出す。
- 見積コストは 0 USD（実行は自分の GPU）。**Cost Ledger の行は 0 USD でも必ず書く**（#831 と同じ）。
- 実行先は同梱版なら `modelmanager.Prepare`（止まっていれば起動。起動前のポート違いは `ErrEndpointMoved` で 1 回やり直し）、外部の接続先ならその接続先。ブレーカー・同時実行数は接続先ごと（`endpoint:<名前>`）で、通常の ComfyUI のモデルと共有する。
- **Shot への固定は既存の LOCK（`video_model`）**: `domain/lock.go` の検査（`ParamVideoModel` の値は `domain.VideoModels()` に含まれること）は、公開した版（上記）を含むので変えない。固定されていれば Different Model は掛からない（`model-adapters.md` §11.3）。ビルトインのモデルの扱いは変わらない。

## 3. 保存と取り込み

|入口|内容|
|---|---|
|貼り付け・ファイル（画面 `POST /workflows/import`）、MCP `import_workflow_json`|API 形式の JSON（`{"prompt": …}` も可）。エディタ形式（`nodes`/`links`）は拒否してヒントを返す。割り当ては、省略すると一般的なノード（CLIPTextEncode・KSampler・Empty*Latent・Save*）から推測し、足りなければ下書き|
|ComfyUI から取り込む（画面 `POST /workflows/import/comfyui`、MCP `list_comfyui_workflows`・`import_workflow_from_comfyui`）|対象は同梱版（既定）か選んだ外部の接続先。一覧は `GET /userdata?dir=workflows`、画面の形式は `GET /userdata/workflows/<path>`、API の形式は `GET /history` の**最後の実行**（`prompt` ＋ `extra_data.extra_pnginfo.workflow`）のうち、画面の形式の `id` が一致するもの。一致する実行が無ければ、先に ComfyUI で 1 回実行するよう案内する（保存しない）|
|再取り込み（`replace`）|既存のワークフローのグラフと画面の形式を入れ替える。変わっていれば版が上がる|
|ノード画面で開く（画面 `POST /workflows/:id/open`）|画面の形式を ComfyUI のユーザーデータ（`workflows/mam/<名前>.json`）へ書き、ノード画面へ移る（左の「ワークフロー」から開く）。API 形式だけで取り込んだものは開けない（理由を表示）|

カスタムノードやフロントエンドの拡張は入れない（`postMessage` で渡す方式は採らない。ComfyUI のフロントエンドにその口が無く、同梱版は `--disable-all-custom-nodes` のため）。

## 4. 割り当ての編集と警告

- 割り当ての編集は、グラフのノードと入力の一覧から選ぶ（ほかのノードがつないでいる入力は選べない）。`GET /object_info` で入力の型（数値・文字・選択肢）を取り、値の入力に合う候補だけを出す（取れなければ全部）。
- **警告（保存時と実行前）**: 保存した（取り込み・割り当て変更・再取り込み）とき、実行を始めるとき（`run_workflow`・画面の実行）に、つないでいる ComfyUI を `GET /object_info`（ノードの有無）と `GET /models/<folder>`（ローダーが指すファイルの有無、選択肢の値）で照合し、足りないものを警告として返す（種類 `node` / `model` / `choice`）。**警告は保存も実行も止めない**（サーバーにあとからノードやファイルが入ることがある）。ComfyUI に問い合わせられないときだけ、「つながらない」理由を返す（`reachable: false`）。同梱版が止まっているときは、保存時は「止まっている」と案内し、実行は起動してから進める。`POST /workflows/:id/check`・MCP `check_workflow` はいつでも照合できる。

## 5. 実行（`run_workflow`・画面なしの試し実行）

保存したグラフと割り当てを、Shot と同じ `comfyui.Backend` で実行する。入力: プロンプト・ネガティブ・縦横比・（動画）解像度と長さ（ワークフローが作れる長さに丸める）・参照画像／最初と最後のフレーム（Project の素材）。Project には何も書かず、Cost Ledger の行も作らない（ワークフロー単体の試し）。結果は `<data-dir>/workflow-runs/<run id>/` に置き、7 日で消す。MCP は `job_id` を返し、`get_job` が進捗と結果（トークン付きのダウンロード URL）を返す。mam の再起動で止まった実行は失敗として扱う。

## 6. プロジェクトバンドル

ワークフローは全体で持つので**バンドルに含めない**（`bundle` の `unbundledTables` に理由つきで載せる）。バンドルを取り込んだとき、その Project の Take（`model_name`）と Video Model の LOCK が指す `comfy:<id>@v<N>` のうち、この mam に無いワークフロー（または版）があれば、**取り込みは成功させ**、その Project のチャットに「見つからない」と一覧を出して警告する（`repository/bundle.MissingWorkflows`）。該当の Shot は、ワークフローを取り込むか別のモデルを選び直すまで、固定が無視されて AUTO と同じ経路で選ばれる。

## 7. 検証できていないこと

実機の ComfyUI と GPU では実行していない（偽の ComfyUI `comfyuitest` が `/object_info`・`/userdata`・`/history`・`/models/<folder>` と実行を再現する）。`/history` の記録の形（`prompt` タプルと `extra_data.extra_pnginfo.workflow`）は ComfyUI のソースから読んだもので、実機での確認が残っている。
