# ER / データモデル: モデルマネージャーのジョブ種別

[er.md](../er.md) の続き。`er.md` が行数上限を超えているため別ファイルにしている。改訂履歴は [er.changelog.md](../er.changelog.md) に足す。設計は [model-manager.md](model-manager.md)。

`job.kind` の CHECK 制約は `generate` / `judge` / `repair` / `plan` に、次の 2 つを足した 6 値になる（マイグレーション `20261025090000_job_kind_model.sql`。`job` を作り直して移す。前の `20260929070600_job_kind_plan.sql` と同じ方法）。

| `kind` | 再開 | 内容 |
|---|---|---|
| `model_install` | する（`resumable=true`。`attempts` が 3 未満の間だけ） | ComfyUI エンジン・カタログのモデル・利用者が Hugging Face から選んだ `.safetensors`（[model-manager-hf.md](model-manager-hf.md)）の取得・検証・展開。`payload` は `{"target": "engine" or "model:<id>" or "hf:<repo>@<commit>/<path>", ...}`（`hf:` の payload は、コミットに固定した URL・SHA-256・サイズ・フォルダ・名前を持つ。トークンは入れない）。`.part` を `Range` で続きから取得し、SHA-256 を検証し直すので、再実行しても結果が同じ |
| `gentest` | しない（`resumable=false`。再起動で、終わっていない結果は `failed`） | 生成テスト（試し生成を含む。[../gentest/gentest.md](../gentest/gentest.md)）のバッチ。`payload` は `{"batch_id": ...}` だけで、要求・結果・入力は `gentest_*` のテーブル（[../gentest/er-gentest.md](../gentest/er-gentest.md)）と `<data-dir>/gentest/<バッチ ID>/` にある。`take`・`cost_ledger_entry`・アセットには書かない |

`job` に Project の列は無く、どちらも Project に属さない。マイグレーションの down は 2 種別の行を消して、元の CHECK に戻す。`domain.JobKinds()` が 6 値を返し、スキーマのテストはこの関数から期待値を作る。進捗（割合・メッセージ）は DB に書かず、メモリ上の記録（`modelmgr/track`）と `payload` で表す。
