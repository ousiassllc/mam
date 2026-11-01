# 生成テスト（プロジェクトに属さない画像・動画の比較生成）

Issue #835。画面 `/generation-test`、MCP ツール（[../../api/mcp-tools-generation-test.md](../../api/mcp-tools-generation-test.md)）、Model Adapter の直接呼び出し（[gentest-adapter.md](gentest-adapter.md)）、テーブル（[er-gentest.md](er-gentest.md)）、ルート（[../../api/endpoints-gentest.md](../../api/endpoints-gentest.md)）の全体の設計。実装は `backend/internal/service/gentest`（サービス。下位に `gterr`・`modelcatalog`・`projectlink`・`gentesttest`）・`backend/internal/repository/gentestrepo`・`web/handler/gentest`・`web/organisms/gentest`・`mcp/tools/gentest`。

目的: モデルとプロンプトを組み合わせて、**プロジェクトを作らずに**画像・動画を生成して見比べ、気に入った結果だけをプロジェクトに取り込む。生成は Take・コスト台帳・アセットを作らず、結果は履歴（`gentest_*` のテーブルと `<data-dir>/gentest/`）にだけ残る。

## 1. 方針

- **実装は 1 つ**: #833 のモデルマネージャーの「試し生成」は、この仕組みの 1 モデル・1 プロンプト・1 本のバッチ（[../model-manager/model-manager.md](../model-manager/model-manager.md) §8）。ジョブ種別 `gentest`（`resumable=false`）・保存先 `<data-dir>/gentest/`・テーブルは共通。
- **モデルは指定どおり呼ぶ**: モデル識別子を指定して Model Adapter を直接呼び（`modeladapter/direct`）、プロンプトは補正しない（英訳が OFF のときは入力のまま。ON（既定）のときは §3.1 の英訳だけを加えて送る）。落ちても代替モデルへは切り替えない（その結果が `failed`）。ブレーカー・ベンダーごとの同時実行数・再試行・タイムアウトは通常の呼び出しと同じ共有の経路（`modeladapter.Call`）を通る。
- **見積りの確認が先**: バッチはまず `Estimate`（実行しない）で本数と見積りを出し、利用者が確認した `estimate_id` と合計でだけ `Run` できる。`Run` はサーバーで見積りをやり直し、違えば 409。
- **履歴はプロジェクトから独立**: テーブルは Project のテーブルを参照しない（ID は TEXT）。プロジェクトを削除しても履歴は消えず、プロジェクトの書き出し（bundle）にも含めない。入力画像は履歴の側へ複製して持つ。
- **コストは別集計**: 履歴ページに合計の 1 行（`Spend`）だけを出す。解析画面・`CostSummary`・コスト台帳には入れない。

## 2. 選べるモデル（`modelcatalog`）

画像: 組み込みのクラウドモデルすべて（`domain.ImageModels()`）と、#831 のローカル・OpenAI 互換のモデル、#832 の ChatGPT サブスクの画像モデル（`gpt-image-2:chatgpt-subscription`。モデル選択・見積りの内訳・履歴には表示名 `GPT Image 2 (ChatGPT サブスク)` で出す。`chatgptid.Label`、`localbackends.Options.Publish` が `modelreg.SetLabels` で登録する。ON/OFF によらず登録する。Issue #870）、#834 の保存したワークフロー（`comfy:<id>@v<N>`）。動画: `domain.VideoModels()`（組み込みと #831 のローカル動画モデルと #834 のワークフロー）。

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

**確認の取り決め**: `estimate_id` は、正規化した要求・モデルごとの内訳・合計の SHA-256。プロンプトとネガティブの改行（CRLF・CR）は LF にそろえてから数える（ブラウザは textarea を CRLF で送り、確認の hidden は LF で戻るため。モデルへ送る文は書き換えない）。単価なし（`unknown`）のモデルは内訳に `unit_usd`・`subtotal_usd` なしで入り、合計には入らないが、`estimate_id` の対象には含む。`Run` は同じ入力で見積りをやり直し、`estimate_id` か合計が違う（単価・設定・入力・可用性が変わった）、または上限を超えるときは `*ConflictError`（HTTP 409 / MCP `conflict`）で拒否し、バッチもジョブも作らない。

## 3.1 英訳（`translate_to_english`。Issue #913）

生成モデルは英語のほうが学習分布に合い良い結果が出やすいため、バッチは「プロンプトとネガティブプロンプトを英語に訳して送る」を切り替えられる（`Request.TranslateToEnglish`）。**既定は ON**（画面のスイッチの初期値と MCP の `translate_to_english` の省略時は同じ値 `gentest.DefaultTranslate`）。プロジェクトでの生成は Prompt Planner が Canonical Shot Spec を英語で作っている（§F-06/F-07）ので、生成テストだけが日本語のまま届いていた。

- **訳すもの**: ON のとき、プロンプトとネガティブプロンプトのうち**ラテン文字以外の文字（日本語・中国語・韓国語・キリル文字・ギリシャ文字など）を 1 字でも含む文だけ**。**文字の体系（Unicode の script）が Latin でも Common でもない「文字（letter）」**を数える。Common の文字（`µ`（U+00B5）・`ʼ`（U+02BC）などの文字、数字・記号・絵文字・空白）はどの言語の文字でもないので数えない。アクセント付きのラテン文字（café・ü・ñ）と全角のラテン文字（ＡＢＣ）は Latin なので数えない。したがって `5µm particles` のような英語の文は訳さない。`50Ω resistor` のようにギリシャ文字を含む英語の文は訳す対象に数えるが、LLM が原文のまま返したときは下の「訳せなければ止める」の対象にせず、原文のまま送る。すでに英語の文（アクセント付きのラテン文字・数字・記号・絵文字だけの文を含む）は LLM に送らず、そのまま送る（`prompttranslate.Needs`）。訳す文が 1 つも無ければ LLM を呼ばない。
- **使う LLM**: 設定の「LLM の割り当て」の **Prompt Planner** のルート（`llm.AgentPromptPlanner`。主・代替とフェイルオーバー・再試行・サーキットブレーカーは通常の LLM 呼び出しと同じ。`agent/prompttranslate`）。モデルの割り当てに「英訳」の役割は無く、モデル向けの英語のプロンプトを書く既存の役割を使う（役割を足さない）。
- **1 回の実行で 1 回だけ**: 訳す文を 1 回の LLM 呼び出しにまとめて送り、結果を実行の全モデル・全プロンプト・全本数で使い回す（本数ぶん・モデルぶん訳し直さない。シードの有無によらない）。入力画像には触れない（英訳はテキストだけ）。
- **訳せなければ止める**: LLM の失敗（API キー未登録・サーキットブレーカー・タイムアウト・使えない答え）、答えの件数が違う・空のときは `*gterr.UpstreamError`（画面 502・MCP `upstream`。メッセージは「何も生成していません。もう一度試すか、英訳を OFF にしてください」を含む）で `Run` を断る。バッチ・結果の行・プロンプト履歴・ジョブは作らず、モデルも呼ばない。**訳す対象の文が原文のまま返ったときは失敗にしない**: その文は入力した文のまま送り（`50Ω resistor` のように訳すものが無い文）、ほかの文は訳して使う。原文のまま送った文は `params.translation` に残さず、`translated` は偽になる（結果の行の `prompt` と `sent_prompt` は同じ）。**LLM の答えが無いのに黙って日本語で送ることはしない。**
- **見積りに入れる**: `Estimate` の内訳に、訳す文の数を `items` とする 1 行（`model` は `translate`、ラベルは「プロンプトの英訳（プロバイダー / モデル）」）を足す（LLM は割り当ての主のターゲットを名指しする）。**LLM には単価が無い**（Reasoning LLM の呼び出しは Take でもコスト台帳でもなく、料金表を持たない。ChatGPT のサブスクのように 1 回ごとの単価にならない経路もある）ので、既存の単価なしの扱い（§3 の `unknown`）に従い、**合計には入れず**、`unknown_models` に `translate` を載せ、警告 `no_price`（`model=translate`）で知らせる。`unknown_items` は結果の数なので増やさず、`total_items` にも入れない。ON/OFF は `estimate_id` の対象（切り替えると見積りの確認をやり直す）。見積りは LLM を呼ばない（訳すのは `Run` のとき）。
- **履歴には両方残す**: 結果の `prompt`・`negative_prompt` は入力した文のまま、訳した英語は結果の `params.translation`（`prompt`・`negative_prompt`）に残し、`ResultView` の `translated`・`translated_prompt`・`translated_negative_prompt` として出す。`sent_prompt` は実際に送った文（英語。ネガティブの欄が無いモデルは英語のネガティブを「Avoid: …」として書き足したもの）、ネガティブの欄があるモデルへは英語のネガティブを送る。バッチの `params` に `translate_to_english` を残す。プロンプト履歴（`gentest_prompt`）は入力した文で重複排除する。
- モデルマネージャーの試し生成（1 モデル・1 プロンプト）は英訳しない（OFF。ローカルのモデルを入力のまま試す用途）。

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

プロジェクト一覧の分析・設定の隣にリンク。画像／動画の切り替え、モデルのカード（タップで選ぶ。選べないものは「選べません」のバッジで、理由は「?」ヘルプと読み上げにある）、プロンプトの追加（複数）とネガティブ、本数・縦横比（動画は解像度・長さ）、入力画像（アップロード・プロジェクト・履歴。動画は最初／最後のフレーム・自由な参照の役割）、「見積りを見る」→ 確認（件数・モデルごとの内訳・合計・単価なしの注記・警告・上限超過）→ 「この内容で実行する」→ 進捗（2 秒ごと・取り消し）と比較表（行＝モデル、列＝プロンプト）。`/generation-test/history`（モデル・種類・お気に入りで絞り込み、詳細、削除、お気に入り、ファイルのダウンロード、合計コストの 1 行）、`/generation-test/prompts`（プロンプト履歴: 再利用・削除）。詳細は取り込みフォーム（参照／アセット・種類・名前・タグ・スロット）を持つ。ルートは [endpoints-gentest.md](../../api/endpoints-gentest.md)。英訳のスイッチ「英語に訳して送る」（既定 ON。説明は「?」。§3.1）があり、訳した英語は比較表の列と結果の詳細に出る（入力した文と送った英語の両方）。

プロンプト履歴は（プロンプト, ネガティブ）で重複排除し、使用回数と最後に使った日時を持つ。**明示の削除だけ**で消え、結果の履歴とは独立（結果を消しても残り、消しても結果に影響しない）。

## 9. テスト

見積り（3 状態・丸め・上限）・直接呼び出し（代替なし・プロンプトそのまま・シードは対応モデルだけ・ブレーカーと同時実行）・バッチのジョブ（全組み合わせ・取り消し・クラッシュ後の整理・Take・台帳が増えないこと）・履歴（絞り込み・お気に入り・プロンプトの重複排除・削除）・取り込み（参照・アセット・スロット・LOCK・ファイルの複製）・マイグレーションの up/down・画面（CSRF・流れ・HTMX の進捗）・MCP（見積り → 実行の 409・`get_job`・トークン URL）。英訳（ON/OFF・英語はそのまま LLM を呼ばない・1 回の実行で 1 回だけ・失敗でモデルを呼ばない・履歴に両方・見積りの単価なしの行・ネガティブの欄あり／なし。`servicetest/translate_test.go`、`agent/prompttranslate`）。偽のバックエンドは `service/gentest/gentesttest`、ローカルの ComfyUI を通した結果までは `service/gentest/localtest`。実際のベンダー API（シードの効き・価格）は実機で未確認。

## 改訂履歴

[gentest.changelog.md](gentest.changelog.md)
