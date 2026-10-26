# 生成テスト（プロジェクトに属さない画像・動画の比較生成）

Issue #835。画面 `/generation-test`、MCP ツール（[../../api/mcp-tools-generation-test.md](../../api/mcp-tools-generation-test.md)）、Model Adapter の直接呼び出し（[gentest-adapter.md](gentest-adapter.md)）、テーブル（[er-gentest.md](er-gentest.md)）、ルート（[../../api/endpoints-gentest.md](../../api/endpoints-gentest.md)）の全体の設計。実装は `backend/internal/service/gentest`（サービス。下位に `gterr`・`modelcatalog`・`projectlink`・`gentesttest`）・`backend/internal/repository/gentestrepo`・`web/handler/gentest`・`web/organisms/gentest`・`mcp/tools/gentest`。

目的: モデルとプロンプトを組み合わせて、**プロジェクトを作らずに**画像・動画を生成して見比べ、気に入った結果だけをプロジェクトに取り込む。生成は Take・コスト台帳・アセットを作らず、結果は履歴（`gentest_*` のテーブルと `<data-dir>/gentest/`）にだけ残る。

## 1. 方針

- **実装は 1 つ**: #833 のモデルマネージャーの「試し生成」は、この仕組みの 1 モデル・1 プロンプト・1 本のバッチ（[../model-manager/model-manager.md](../model-manager/model-manager.md) §8）。ジョブ種別 `gentest`（`resumable=false`）・保存先 `<data-dir>/gentest/`・テーブルは共通。
- **モデルは指定どおり呼ぶ**: モデル識別子を指定して Model Adapter を直接呼び（`modeladapter/direct`）、プロンプトは入力のまま送る（補正しない）。落ちても代替モデルへは切り替えない（その結果が `failed`）。ブレーカー・ベンダーごとの同時実行数・再試行・タイムアウトは通常の呼び出しと同じ共有の経路（`modeladapter.Call`）を通る。
- **見積りの確認が先**: バッチはまず `Estimate`（実行しない）で本数と見積りを出し、利用者が確認した `estimate_id` と合計でだけ `Run` できる。`Run` はサーバーで見積りをやり直し、違えば 409。
- **履歴はプロジェクトから独立**: テーブルは Project のテーブルを参照しない（ID は TEXT）。プロジェクトを削除しても履歴は消えず、プロジェクトの書き出し（bundle）にも含めない。入力画像は履歴の側へ複製して持つ。
- **コストは別集計**: 履歴ページに合計の 1 行（`Spend`）だけを出す。解析画面・`CostSummary`・コスト台帳には入れない。

## 2. 選べるモデル（`modelcatalog`）

画像: 組み込みのクラウドモデルすべて（`domain.ImageModels()`）と、#831 のローカル・OpenAI 互換のモデル、#832 の ChatGPT サブスクの画像モデル（`gpt-image-2:chatgpt-subscription`）、#834 の保存したワークフロー（`comfy:<id>@v<N>`）。動画: `domain.VideoModels()`（組み込みと #831 のローカル動画モデルと #834 のワークフロー）。

**今使えるものだけ選べる**: `Adapter.Unavailable(model)` が理由を返すモデルは、一覧に残して無効にし、理由を出す（API キー未登録・接続先を使えない・ChatGPT にログインしていない・サーキットブレーカーが開いている）。`Unavailable` は #835 で画像モデルにも広げた（バックエンドが無い・設定されていない・ブレーカー開）。ChatGPT サブスクはログアウト後も「呼び出しの失敗」になりうる（一覧はバックエンドの有無と設定だけを見る）。

**成人向けモデルの年齢確認（Issue #841）**: モデルマネージャーの成人向けのモデル（`comfyui:bundled-comfyui/<ワークフロー名>`）は、年齢確認のゲートが閉じている間、一覧（`Models`・`Model`）に出ず、名指しの要求は不明なモデル（`ErrNotFound`）として断る。`gentest.Deps.Hidden`（`modelmanager.Service.HidesModel`）が決める。ゲートはモデルマネージャーの Service が持つ（[../model-manager/model-manager.md](../model-manager/model-manager.md) §6.2）。直接呼び出し（`direct.Image`・`direct.Video`）も、一覧と始まる時の確認をすり抜けたモデルを `agegate.ErrRefused` で断る（何も呼ばない。§6.1 の「導入済みでも、年齢確認がオフの間は使えない」）。

モデルごとに次の能力を返す: 参照画像を使えるか（画像）、動画の長さ・解像度・参照枚数・最初／最後のフレーム・音声、**シード**（FLUX.2・Runway Gen-4.5 と、シードの割り当てがあるローカルのワークフロー）、**ネガティブの欄**（Kling・ネガティブの割り当てがあるワークフロー）、単価。

## 3. 見積り（`Estimate`）

バッチ = 選んだモデル × プロンプト × 1 組あたりの本数（全組み合わせ）。同じ（プロンプト, ネガティブ）は 1 件に数える。

| 状態（`basis`） | 意味 | 合計 |
|---|---|---|
| `priced` | 単価が分かる（画像: 1 枚の USD。動画: 解像度別の 1 秒の USD × `SnapDuration` した長さ） | 入る |
| `free` | ローカルのモデル（#831・#833・#834）。0 USD。「無料（ローカル）」と出す | 0 として入る |
| `unknown` | ChatGPT サブスクと、単価を持たないクラウドモデル | **入れない**（件数・モデルを `unknown_items`・`unknown_models` と注記で知らせる） |

警告（`warnings`）: モデルが使えない入力画像は外して知らせる（`inputs_dropped`）、長さの丸め（`duration_snapped`）、モデルが作らない解像度・縦横比（失敗する）、単価なし、ネガティブをプロンプトに書き足す（`negative_folded`）、シード非対応（同じ入力を本数ぶん呼ぶ）、最初／最後のフレームと自由な参照の併用、参照が多すぎる。Runway で 1:1 かつ最初のフレームなしは、実行前に警告する（ベンダーが受け付けない組み合わせ）。

**上限**（`models.json` の `gentest`。`gentestcfg`）: `max_items`（既定 50）・`max_cost_usd`（既定 20。0 は上限なし）。見積りの時点で `over_limit`（確認の画面は実行ボタンを無効にする）、`Run` でサーバー側でも拒否する（`max_items` は入力の誤り、コストは 409）。`models.json` が固定されているとき（`MAM_MODELS_PINNED`）は他の項目と同じく画面から変えられない。

**確認の取り決め**: `estimate_id` は、正規化した要求・モデルごとの内訳・合計の SHA-256。`Run` は同じ入力で見積りをやり直し、`estimate_id` か合計が違う（単価・設定・入力・可用性が変わった）、または上限を超えるときは `*ConflictError`（HTTP 409 / MCP `conflict`）で拒否し、バッチもジョブも作らない。

## 4. バッチとジョブ

`Run` の手順: 入力画像を解決して履歴の側へ複製 → `gentest_batch`・`gentest_input`・全結果の行（`queued`）・プロンプト履歴を 1 つのトランザクションで作る → ジョブ（種別 `gentest`、`payload` は `{"batch_id": ...}`）を投入。`resumable=false`。

ジョブ（`handle`）: ベンダーごとにワーカーを置き（数は `Adapter.ConcurrencyLimit(vendor)`）、ベンダー同士は並行して動かす。1 件ごとに `running` → 呼び出し → `succeeded`/`failed` を、`context.WithoutCancel` でその都度記録する（mam が終了中でも、完了済みの結果は残る）。結果ファイルは `<data-dir>/gentest/<バッチ ID>/<結果 ID>.<拡張子>` に原子的に書く。

- **進捗**はテーブルの件数（状態ごとの `COUNT`）から求める。画面は 2 秒ごとに取り直し（HTMX）、MCP は `get_job`・`get_generation_test`。メモリ上の進捗の記録は持たない。
- **取り消し**（`Cancel`）: まだ始めていない結果を `cancelled`（理由「キャンセルされました」）にし、新しい呼び出しを始めない。**呼び出し中のものは終わるのを待ち**、その結果は残す（ベンダー側では生成が続き課金されうるため）。終わったバッチの取り消しは 409。取り消しの登録はメモリ上（`runs`）。
- **削除**: 結果の削除は行とファイルを消し、バッチの最後の 1 件を消すとバッチ・入力・ディレクトリも消す。終わっていない結果は 409。バッチが先に消されたジョブは何もせず終わる。

## 5. 失敗・中断の扱い

- 1 件の失敗（キー未登録・ブレーカー開・タイムアウト・ベンダーの拒否・入力を受け付けない）はその結果を `failed`・理由つきにし、他の結果は続ける。理由は日本語に直す（`Explain`: モデルマネージャーの拒否の文言。ベンダーのタイムアウトは「ベンダー側では生成が続き、課金される場合があります」を添える）。**再試行して別モデルへ回すことはしない**。
- **mam の異常終了**: ジョブは再開しない（生成は課金されるため）。起動時（`jobqueue.Recover` のあと）に `OnStartup` が、`queued`・`running` のまま残った結果を `failed`（理由「mam が停止したため中断されました（課金される生成は再開しません）」）にする。完了済みの結果は残る。24 時間以上前の入力のステージング（`_staging/`）も消す。
- ジョブ自体が失敗するのは、バッチを始められないとき（入力画像のファイルが読めない等）だけ。その場合は未着手の結果をすべて `failed` にする。

## 6. 入力画像

入力は、アップロード（画面は見積り時に、MCP は `gentest_reference` で `_staging/` に置く）、プロジェクトのアセット・Reference Library の項目（`project_asset` / `reference_item`）、履歴の成功した画像結果（`history`）。選んだ画像は**バッチの `gentest_input` に複製**する（`origin` に元を記録）ので、元のプロジェクトや結果を後で消しても履歴は読める。画像は PNG・JPEG・WebP を中身で検査し、1 枚 20 MiB まで・1 バッチ 10 枚まで。

動画の役割は `reference`（自由な参照）/ `first_frame` / `last_frame`。モデルの能力（`Capabilities`）で、使えない役割は**そのモデルからだけ外して警告**する。

## 7. プロジェクトへの取り込み（`projectlink`）

成功した結果ならどれでも取り込める（お気に入りは目印にすぎない）。取り込み先: **参照**（Reference Library の項目。画像のみ。`source='generated'`、`source_shot_id` は NULL、`kind`・`name`・`tags`）か**アセット**（画像・動画）。ファイルはプロジェクトの assets ディレクトリへ複製する。続けて、参照を Character の face/body・Product・Location のスロットに割り当てられる（`WorldService.AssignReference`。スロットの種類と参照の種類が合わなければ入力の誤り、LOCK されていれば 409）。割り当てに失敗しても取り込みは残り、そのことを知らせる（`PartialImportError`）。取り込み先は `gentest_import` に残す（TEXT の ID のみ）。

## 8. 画面（`/generation-test`）

プロジェクト一覧の分析・設定の隣にリンク。画像／動画の切り替え、モデルのチェックボックス（無効なものは理由つき）、プロンプトの追加（複数）とネガティブ、本数・縦横比（動画は解像度・長さ）、入力画像（アップロード・プロジェクト・履歴。動画は最初／最後のフレーム・自由な参照の役割）、「見積りを見る」→ 確認（件数・モデルごとの内訳・合計・単価なしの注記・警告・上限超過）→ 「この内容で実行する」→ 進捗（2 秒ごと・取り消し）と比較表（行＝モデル、列＝プロンプト）。`/generation-test/history`（モデル・種類・お気に入りで絞り込み、詳細、削除、お気に入り、ファイルのダウンロード、合計コストの 1 行）、`/generation-test/prompts`（プロンプト履歴: 再利用・削除）。詳細は取り込みフォーム（参照／アセット・種類・名前・タグ・スロット）を持つ。ルートは [endpoints-gentest.md](../../api/endpoints-gentest.md)。

プロンプト履歴は（プロンプト, ネガティブ）で重複排除し、使用回数と最後に使った日時を持つ。**明示の削除だけ**で消え、結果の履歴とは独立（結果を消しても残り、消しても結果に影響しない）。

## 9. テスト

見積り（3 状態・丸め・上限）・直接呼び出し（代替なし・プロンプトそのまま・シードは対応モデルだけ・ブレーカーと同時実行）・バッチのジョブ（全組み合わせ・取り消し・クラッシュ後の整理・Take・台帳が増えないこと）・履歴（絞り込み・お気に入り・プロンプトの重複排除・削除）・取り込み（参照・アセット・スロット・LOCK・ファイルの複製）・マイグレーションの up/down・画面（CSRF・流れ・HTMX の進捗）・MCP（見積り → 実行の 409・`get_job`・トークン URL）。偽のバックエンドは `service/gentest/gentesttest`、ローカルの ComfyUI を通した結果までは `service/gentest/localtest`。実際のベンダー API（シードの効き・価格）は実機で未確認。

## 改訂履歴

[gentest.changelog.md](gentest.changelog.md)
