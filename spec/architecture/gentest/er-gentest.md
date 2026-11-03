# ER / データモデル: 生成テスト

[er.md](../er.md) の続き（`er.md` が行数上限を超えているため別ファイル）。設計は [gentest.md](gentest.md)。マイグレーションは `20261025150000_gentest.sql`（down は 5 テーブルを依存の逆順に削除）。**どのテーブルも Project のテーブルを参照しない**（Project・アセット・参照項目の ID は TEXT）。5 テーブルとも `repository/bundle` の `unbundledTables` に入れ、プロジェクトの書き出し・取り込みには含めない。ファイルは `<data-dir>/gentest/<バッチ ID>/`（アセットのディレクトリではない）。

## gentest_batch（1 回の実行）

| 列 | 内容 |
|---|---|
| `id` | PK |
| `media` | `image` / `video` |
| `job_id` | ジョブ（種別 `gentest`）の ID。`idx_gentest_batch_job` |
| `params` | JSON: プロンプトマッチ（[prompt-match.md](prompt-match.md)）のバッチは `match`（`{"target_score": N}`）を持つ。`aspect_ratio`・`resolution`・`duration_seconds`・`count`（1 組あたりの本数）・`translate_to_english`（英訳を ON にして実行したとき true。[gentest.md](gentest.md) §3.1） |
| `estimated_cost` | 確認した見積りの合計（USD。単価なしは含まない） |
| `estimate` | JSON: 確認した見積り（要求・モデルごとの内訳・警告） |
| `created_at` / `finished_at` | 作成・全結果が終わった日時 |

## gentest_input（バッチに渡した入力画像）

`batch_id`（FK → `gentest_batch`、削除でカスケード）・`role`（`reference` / `face` / `style` / `edit` / `pose` / `first_frame` / `last_frame` / `mask`。画像モデルの参照は `first_frame` の前の 5 つ（`pose` は Issue #986）、動画は `reference` / `first_frame` / `last_frame`、`mask` は一部だけ描き直すマスクで参照ではない）・`file_path`（`<バッチ ID>/` 以下の相対パス。**入力は履歴の側へ複製**するので、元のプロジェクトや結果を消しても残る）・`mime_type`・`size_bytes`・`origin`（JSON: `kind` = `upload` / `project_asset` / `reference_item` / `history` / `pose_library`、`id`、`project_id`）・`position`・`strength`（REAL、NULL 許容、CHECK 0〜1。画像モデルの参照を**どれだけ強く反映するか**。NULL は既定（0.7）と、強さのないフレーム）。

## gentest_result（1 件の生成）

| 列 | 内容 |
|---|---|
| `id`・`batch_id`・`position` | PK・FK（カスケード）・バッチ内の計画順（モデル → プロンプト → 本数） |
| `media`・`model`・`api_model` | 種類・**正規のモデル識別子（代替しないので実際に使ったモデル）**・ベンダーの識別子 |
| `prompt`・`negative_prompt`・`sent_prompt`・`negative_mode` | 入力・実際に送った文・`none` / `field`（モデルの欄）/ `folded`（「Avoid: …」としてプロンプトに書き足した） |
| `params`・`used_inputs` | JSON: この結果の縦横比・解像度・長さ・顔の入れ替え（[face-swap.md](face-swap.md)）の結果は `face_swap`（元の結果・モデル・ライセンスの警告）を、役割 `face` の入力があった結果は `face_similarity`（SFace の顔の類似度。[../local-models.md](../local-models.md) §4.9）を持つ。`unused_inputs`（このモデルに渡さなかった入力画像: `input_id`・`role`・`reason`。使われなかった入力があったときだけ。#1009）・`avoid_pose`（参照を使うモデルのネガティブに「ポーズ・構図を真似ない」を足したとき true）・`lora`（`name`・`strength`。使ったときだけ）・`translation`（英訳して送ったときだけ。`prompt`・`negative_prompt` に訳した英語。列の `prompt`・`negative_prompt` は入力した文のまま、`sent_prompt` は実際に送った文） / このモデルに渡した入力画像の ID |
| `seed`・`seed_sent` | 送ったシード（対応しないモデルは NULL・0）。実際に送った値を保存する |
| `status`・`error`・`error_info` | `queued` / `running` / `succeeded` / `failed` / `cancelled`・失敗の理由（原因 + 直し方 + 詳細の先頭の 1 行）・失敗を共通のエラーにした JSON（`apperr.Record`: `code`・`args`・`cause`・`detail`・`id`。`` は失敗していない結果と、この列より前に失敗した結果。[共通のエラー](../errors.md)） |
| `file_path`・`mime_type`・`size_bytes` | 結果のファイル（成功したときだけ） |
| `cost_basis`・`estimated_cost`・`actual_cost` | `priced` / `free` / `unknown`・見積り USD（unknown は NULL）・ベンダーが返したときの実費 |
| `favorite` | お気に入り（取り込みの目印にすぎない） |
| `created_at`・`started_at`・`finished_at` | 日時。`started_at` は `queued` → `running` にした時刻（実行時間 = `finished_at − started_at`。`estimate_generation_test` のローカルモデルの所要時間の根拠。キューの待ち時間を含まない。マイグレーション `20261105090000_gentest_result_started_at.sql` より前の結果と、まだ動いていない結果は NULL）。実行していない結果（取り消し）も NULL |

索引: `created_at DESC`（履歴）・`batch_id, position`・`model`・`favorite`（部分）・`status`。

## gentest_prompt（プロンプト履歴）

`id`・`prompt`・`negative_prompt`（`UNIQUE (prompt, negative_prompt)` で重複排除）・`use_count`（バッチで使うたびに +1）・`created_at`・`last_used_at`。**結果へのリンクを持たない**: 結果を消してもプロンプトは残り、プロンプトを消しても結果は残る（明示の削除だけで消える）。

## gentest_recipe（レシピ。Issue #977）

`id`・`name`（`NOT NULL COLLATE NOCASE UNIQUE`・空でない。大文字小文字を区別せず一意）・`params`（JSON。`service/gentest/recipe.Params`: モデルの識別子・steps・cfg・sampler・scheduler・固定のシード・ネガティブ・LoRA・入力画像の強さ・縦横比・品質・速さのモード・画面に出す値。**プロンプトの本文と入力画像は持たない**）・`easy`（0/1。かんたんの選択肢に出す）・`created_at`・`updated_at`。**外部キーなし**（モデルは識別子で名指しし、削除・未導入でもレシピは残る）。マイグレーション `20261104100000_gentest_recipe.sql`、`repository/reciperepo`。プロジェクトのバンドルには含めない（`unbundledTables`）。結果の `gentest_result.params` に `exposed`（使った画面に出す値）が足された（JSON の項目。列は変わらない）。

## gentest_import（プロジェクトへの取り込みの記録）

`result_id`（FK → `gentest_result`、カスケード）・`project_id`・`asset_id`・`reference_item_id`（NULL 可）は **TEXT で外部キーなし**（プロジェクトが消えても記録は残る）・`target`（JSON: `as` = `reference` / `asset`、`kind`・`name`・`tags`、割り当てたスロットの `assign`: `target`・`id`・`slot`）・`created_at`。

## 他のテーブルとの関係

`take`・`cost_ledger_entry`・`asset`・`reference_item` には**生成の時点では何も書かない**。取り込み（`Import`）だけが、指定のプロジェクトに `asset`（と参照なら `reference_item`、`source='generated'`、`source_shot_id` は NULL）を 1 組作る。`job` は種別 `gentest`（[../model-manager/er-model-manager.md](../model-manager/er-model-manager.md)）。

## 改訂履歴

[gentest.changelog.md](gentest.changelog.md)
