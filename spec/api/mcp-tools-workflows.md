# MCP ツール: ComfyUI ワークフロー

[`mcp-tools.md`](mcp-tools.md) の規約に従う、保存した ComfyUI ワークフロー（Issue #834）のツール。意味・版・識別子・警告は [../architecture/comfy/comfy-workflows.md](../architecture/comfy/comfy-workflows.md)。実装は `backend/internal/mcp/tools/workflows`（`service.Services.Workflows`）。**ノード画面（GUI）はツールの対象外**で、ツールはワークフローの JSON を扱う。ワークフローを使う画面（`/nodes`・`/workflows`）は [endpoints-workflows.md](endpoints-workflows.md)。

## 方針

- ワークフローは全体で 1 つの表。`workflow_id` は UUID。現在の版の識別子は `model_id`（`comfy:<workflow_id>@v<版>`）で、`set_model_assignment`（画像の用途・Quality Policy。現在の版に読み替えられる）・`set_shot_lock`・`set_shot_workflow`・`take.model_name` に使う。
- 保存（`import_workflow_json`・`import_workflow_from_comfyui`・`update_workflow_assignment`）の出力は `check`（つないでいる ComfyUI との照合: `reachable`・`reason`・`warnings[]`）を含む。**警告は保存を止めない**。`run_workflow` の出力にも実行前の警告（`warnings[]`）が入る。
- 取り込めない入力は `validation`（エディタ形式の JSON・割り当てがグラフに無いノード・出力ノードが無い・名前が長すぎるなど。メッセージは画面と同じ）、名前が使われていれば `conflict`、無いものは `not_found`。サービスが無いアプリでは `unavailable`。
- 実行は ComfyUI の API（`/prompt`・`/ws`・`/history`・`/view`）。同梱版は止まっていれば起動する。結果のファイルはトークン付きのダウンロード URL（15 分）で受け渡す。

## ツール

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_workflows` | 保存したワークフローの一覧 | なし | `workflows[]`: `id`, `name`, `kind`（`image` / `video`）, `endpoint`, `version`, `model_id`, `ready`（実行できる）, `problems[]`, `has_editor_format`, `used_by[]`, `updated_at` | はい |
| `get_workflow` | 1 件。入力の割り当て・ノード一覧・版の一覧 | `workflow_id`, `include_graph?`（API 形式のグラフも） | `list_workflows` の 1 件 ＋ `settings`（`assignments[]`, `output_node`, `fps`, `frames_step`, `size_multiple`, `video`）, `nodes[]`（`id`, `class_type`, `inputs[]`）, `versions[]`（`version`, `model_id`, `current`）, `graph?` | はい |
| `rename_workflow` | 名前を変える（版は上がらない。冪等） | `workflow_id`, `name`（100 文字以内） | ワークフロー | いいえ |
| `delete_workflow` | 削除する。画像の用途・Quality Policy が選んでいれば `validation` | `workflow_id` | `deleted` | いいえ（破壊的） |
| `update_workflow_assignment` | 入力の割り当て・出力ノード・動画の設定を変える。変わっていれば版が上がる（冪等） | `workflow_id`, `assignments[]`（`param`, `node`, `input`, `index?`, `ratio?`, `offset?`）, `output_node`, `fps?`, `frames_step?`, `size_multiple?`, `video?`（`durations`, `resolutions`, `native_audio`） | ワークフロー ＋ `check` | いいえ |
| `import_workflow_json` | API 形式の JSON（「Save (API Format)」。`{"prompt": …}` も可）を保存する。割り当てを省くと推測する | `name`, `kind`, `endpoint?`（既定 `bundled-comfyui`）, `api_json`（文字列）, `settings?`（`update_workflow_assignment` と同じ） | ワークフロー ＋ `check` | いいえ |
| `list_comfyui_workflows` | ComfyUI サーバーが持つ保存済みワークフロー（`/userdata`）。同梱版は止まっていれば起動する | `endpoint?` | `workflows[]`: `path`, `name` | はい |
| `import_workflow_from_comfyui` | ComfyUI のワークフロー（画面の形式）と、その最後の実行の API 形式を取り込む。実行が無ければ `validation`（先に ComfyUI で実行する） | `endpoint?`, `path?`（省略: 最後の実行）, `name?`, `kind?`, `replace_workflow_id?`（既存を更新。版が上がる） | ワークフロー ＋ `check` | いいえ |
| `check_workflow` | つないでいる ComfyUI にノードとモデルファイルがあるかを照合する。同梱版は起動しない | `workflow_id` | `endpoint`, `reachable`, `reason`, `warnings[]`（`kind`: `node` / `model` / `choice`, `node`, `class_type`, `input`, `value`, `message`） | はい |
| `run_workflow` | 保存したワークフローを入力の値で実行する。`job_id` を返す | `workflow_id`, `version?`（省略: 現在の版。古い版も、保存してあれば実行できる）, `prompt`, `negative_prompt?`, `aspect_ratio?`, `resolution?`, `duration_seconds?`, `references?[]` / `first_frame?` / `last_frame?`（`project_id`, `asset_id`） | `job_id`, `state`, `model_id`, `warnings[]` | いいえ |
| `set_shot_workflow` | Shot の生成方法を動画のワークフローに固定する（Video Model を `model_id` で LOCK）。解除は `set_shot_lock` の `video_model` を `auto` に | `shot_id`, `workflow_id`, `version?`（現在の版のみ） | `shot_id`, `model_id` | いいえ |

### 画面に出す値（`exposed_inputs`。Issue #977）

`import_workflow_json` の `settings`・`update_workflow_assignment`・`get_workflow` の `settings` に `exposed_inputs[]`（`node`・`input`・`type`（`int`/`float`/`string`/`enum`）・`label?`・`min`＋`max`（int・float。両方必要）・`options`（enum。1 つ以上））がある。単発生成のこだわりに入力欄として出る入力で、**割り当ては変わらない**。グラフに無いノード・入力、ほかのノードがつないでいる入力、割り当てが使っている入力、入力の今の値が型・範囲・選択肢に合わないもの、31 件目以降は `validation`。`update_workflow_assignment` が省くと今のまま（空の配列で全部外す）。使う側は [mcp-tools-generation-test.md](mcp-tools-generation-test.md)（`exposed`）。

### 詳細設定（`run_workflow`。Issue #973）

`run_workflow` は `seed`（0〜9007199254740991。省くと実行ごとにランダム）と `sampling`（`steps` 1〜150・`cfg` 0〜30・`sampler`・`scheduler`）を受ける。値は、ワークフローの割り当て（`param` が `steps`・`cfg`・`sampler`・`scheduler`。`update_workflow_assignment` の `assignments[]` に書く。`steps` は `ratio`（0〜1）も付けられ、2 段のサンプラーの切り替えのステップに使う。`frames` は `offset`〔−64〜64 の整数〕も付けられ、フレーム数に足した値を入れる。`frames-2` は `offset: -2`。Issue #1076）が指すノードの入力に入る。省いた項目はグラフの値のまま。割り当てが無い項目と、速さのモードがあるワークフローの `steps`・`cfg` は使わず、出力の `warnings[]` に `kind: sampling` で出す。出力と `get_job` の `result` に、使った `seed` と `sampling`（既定を含む）が残る。範囲外は `validation`。

### ジョブ（`run_workflow`）

`get_job` の `kind` は `run_workflow`。成功すると `result` に `model_id`・`media`・`download_url`（トークン付き）・`expires_at`・`file_name`・`mime_type`。失敗は `error` に ComfyUI の理由。`done` / `total` は割合（0〜100）。mam の再起動で止まった実行は `failed`。

### 既存のツールが識別子を受ける

`set_shot_lock`（`video_model`）・`send_director_command`・画像の用途と Quality Policy の `set_model_assignment` は、保存したワークフローの識別子（動画は固定、画像・動画の割り当ては現在の版）を受ける。存在しない識別子・画像のワークフローを Video Model に選ぶのは `validation`。生成は既存の `generate_shot` などで、`take.model_name` と Cost Ledger の `provider` に識別子が残る。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成。ワークフローの一覧・取得・名前の変更・削除・割り当ての変更・取り込み（JSON・ComfyUI から）・照合・実行・Shot への固定の 11 ツール | Issue #834（#830 の方針: 画面でできることは MCP でもできる。ノード画面そのものは対象外） |
| 1.1 | 2026-11-03 | `run_workflow` に `seed`・`sampling`、`assignments[]` の `param` に `steps`・`cfg`・`sampler`・`scheduler` と `ratio` を足した | Issue #973 |
| 1.2 | 2026-11-04 | `settings` に `exposed_inputs`（画面に出す値）を足した（保存・取得・更新。省くと今のまま） | Issue #977（ステージ 2） |
| 1.3 | 2026-11-06 | `assignments[]` に `offset`（`frames` だけ。フレーム数に足す整数。−64〜64）を足した（`update_workflow_assignment`・`get_workflow`） | Issue #1076 |
