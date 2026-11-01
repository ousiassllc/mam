# ER / データモデル: 生成テスト

[er.md](../er.md) の続き（`er.md` が行数上限を超えているため別ファイル）。設計は [gentest.md](gentest.md)。マイグレーションは `20261025150000_gentest.sql`（down は 5 テーブルを依存の逆順に削除）。**どのテーブルも Project のテーブルを参照しない**（Project・アセット・参照項目の ID は TEXT）。5 テーブルとも `repository/bundle` の `unbundledTables` に入れ、プロジェクトの書き出し・取り込みには含めない。ファイルは `<data-dir>/gentest/<バッチ ID>/`（アセットのディレクトリではない）。

## gentest_batch（1 回の実行）

| 列 | 内容 |
|---|---|
| `id` | PK |
| `media` | `image` / `video` |
| `job_id` | ジョブ（種別 `gentest`）の ID。`idx_gentest_batch_job` |
| `params` | JSON: `aspect_ratio`・`resolution`・`duration_seconds`・`count`（1 組あたりの本数）・`translate_to_english`（英訳を ON にして実行したとき true。[gentest.md](gentest.md) §3.1） |
| `estimated_cost` | 確認した見積りの合計（USD。単価なしは含まない） |
| `estimate` | JSON: 確認した見積り（要求・モデルごとの内訳・警告） |
| `created_at` / `finished_at` | 作成・全結果が終わった日時 |

## gentest_input（バッチに渡した入力画像）

`batch_id`（FK → `gentest_batch`、削除でカスケード）・`role`（`reference` / `first_frame` / `last_frame`）・`file_path`（`<バッチ ID>/` 以下の相対パス。**入力は履歴の側へ複製**するので、元のプロジェクトや結果を消しても残る）・`mime_type`・`size_bytes`・`origin`（JSON: `kind` = `upload` / `project_asset` / `reference_item` / `history`、`id`、`project_id`）・`position`。

## gentest_result（1 件の生成）

| 列 | 内容 |
|---|---|
| `id`・`batch_id`・`position` | PK・FK（カスケード）・バッチ内の計画順（モデル → プロンプト → 本数） |
| `media`・`model`・`api_model` | 種類・**正規のモデル識別子（代替しないので実際に使ったモデル）**・ベンダーの識別子 |
| `prompt`・`negative_prompt`・`sent_prompt`・`negative_mode` | 入力・実際に送った文・`none` / `field`（モデルの欄）/ `folded`（「Avoid: …」としてプロンプトに書き足した） |
| `params`・`used_inputs` | JSON: この結果の縦横比・解像度・長さ・`translation`（英訳して送ったときだけ。`prompt`・`negative_prompt` に訳した英語。列の `prompt`・`negative_prompt` は入力した文のまま、`sent_prompt` は実際に送った文） / このモデルに渡した入力画像の ID |
| `seed`・`seed_sent` | 送ったシード（対応しないモデルは NULL・0）。実際に送った値を保存する |
| `status`・`error` | `queued` / `running` / `succeeded` / `failed` / `cancelled`・失敗の理由 |
| `file_path`・`mime_type`・`size_bytes` | 結果のファイル（成功したときだけ） |
| `cost_basis`・`estimated_cost`・`actual_cost` | `priced` / `free` / `unknown`・見積り USD（unknown は NULL）・ベンダーが返したときの実費 |
| `favorite` | お気に入り（取り込みの目印にすぎない） |
| `created_at`・`finished_at` | 日時 |

索引: `created_at DESC`（履歴）・`batch_id, position`・`model`・`favorite`（部分）・`status`。

## gentest_prompt（プロンプト履歴）

`id`・`prompt`・`negative_prompt`（`UNIQUE (prompt, negative_prompt)` で重複排除）・`use_count`（バッチで使うたびに +1）・`created_at`・`last_used_at`。**結果へのリンクを持たない**: 結果を消してもプロンプトは残り、プロンプトを消しても結果は残る（明示の削除だけで消える）。

## gentest_import（プロジェクトへの取り込みの記録）

`result_id`（FK → `gentest_result`、カスケード）・`project_id`・`asset_id`・`reference_item_id`（NULL 可）は **TEXT で外部キーなし**（プロジェクトが消えても記録は残る）・`target`（JSON: `as` = `reference` / `asset`、`kind`・`name`・`tags`、割り当てたスロットの `assign`: `target`・`id`・`slot`）・`created_at`。

## 他のテーブルとの関係

`take`・`cost_ledger_entry`・`asset`・`reference_item` には**生成の時点では何も書かない**。取り込み（`Import`）だけが、指定のプロジェクトに `asset`（と参照なら `reference_item`、`source='generated'`、`source_shot_id` は NULL）を 1 組作る。`job` は種別 `gentest`（[../model-manager/er-model-manager.md](../model-manager/er-model-manager.md)）。

## 改訂履歴

[gentest.changelog.md](gentest.changelog.md)
