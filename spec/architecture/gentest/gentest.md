# 生成テスト（プロジェクトに属さない画像・動画の比較生成）

Issue #835。画面 `/generation-test`、MCP ツール（[../../api/mcp-tools-generation-test.md](../../api/mcp-tools-generation-test.md)）、Model Adapter の直接呼び出し（[gentest-adapter.md](gentest-adapter.md)）、テーブル（[er-gentest.md](er-gentest.md)）、ルート（[../../api/endpoints-gentest.md](../../api/endpoints-gentest.md)）の全体の設計。実装は `backend/internal/service/gentest`（サービス。下位に `gterr`・`modelcatalog`・`projectlink`・`history`・`record`・`runner`・`gentesttest` ほか。下の「パッケージの構成」）・`backend/internal/repository/gentestrepo`・`web/handler/gentest`・`web/organisms/gentest`・`mcp/tools/gentest`。

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

**上限**（`models.json` の `gentest`。`gentestcfg`）: `max_items`（既定 50）・`max_cost_usd`（既定 20。0 は上限なし）・`match_face`（プロンプトマッチの似てる度に顔を混ぜる `weight`（既定 0 = 混ぜない）・`floor`・`ceil`。[prompt-match.md](prompt-match.md)）。見積りの時点で `over_limit`（確認の画面は実行ボタンを無効にする）、`Run` でサーバー側でも拒否する（`max_items` は入力の誤り、コストは 409）。`models.json` が固定されているとき（`MAM_MODELS_PINNED`）は他の項目と同じく画面から変えられない。

**確認の取り決め**: `estimate_id` は、正規化した要求・モデルごとの内訳・合計の SHA-256。プロンプトとネガティブの改行（CRLF・CR）は LF にそろえてから数える（ブラウザは textarea を CRLF で送り、確認の hidden は LF で戻るため。モデルへ送る文は書き換えない）。単価なし（`unknown`）のモデルは内訳に `unit_usd`・`subtotal_usd` なしで入り、合計には入らないが、`estimate_id` の対象には含む。`Run` は同じ入力で見積りをやり直し、`estimate_id` か合計が違う（単価・設定・入力・可用性が変わった）、または上限を超えるときは `*ConflictError`（HTTP 409 / MCP `conflict`）で拒否し、バッチもジョブも作らない。

## 3.1 英訳（`translate_to_english`。Issue #913）

生成モデルは英語のほうが学習分布に合い良い結果が出やすいため、バッチは「プロンプトとネガティブプロンプトを英語に訳して送る」を切り替えられる（`Request.TranslateToEnglish`）。**既定は ON**（画面のスイッチの初期値と MCP の `translate_to_english` の省略時は同じ値 `gentest.DefaultTranslate`）。プロジェクトでの生成は Prompt Planner が Canonical Shot Spec を英語で作っている（§F-06/F-07）ので、生成テストだけが日本語のまま届いていた。

- **訳すもの**: ON のとき、プロンプトとネガティブプロンプトのうち**ラテン文字以外の文字（日本語・中国語・韓国語・キリル文字・ギリシャ文字など）を 1 字でも含む文だけ**。**文字の体系（Unicode の script）が Latin でも Common でもない「文字（letter）」**を数える。Common の文字（`µ`（U+00B5）・`ʼ`（U+02BC）などの文字、数字・記号・絵文字・空白）はどの言語の文字でもないので数えない。アクセント付きのラテン文字（café・ü・ñ）と全角のラテン文字（ＡＢＣ）は Latin なので数えない。したがって `5µm particles` のような英語の文は訳さない。`50Ω resistor` のようにギリシャ文字を含む英語の文は訳す対象に数えるが、LLM が原文のまま返したときは下の「訳せなければ止める」の対象にせず、原文のまま送る。すでに英語の文（アクセント付きのラテン文字・数字・記号・絵文字だけの文を含む）は LLM に送らず、そのまま送る（`prompttranslate.Needs`）。訳す文が 1 つも無ければ LLM を呼ばない。
- **使う LLM**: 設定の「LLM の割り当て」の **Prompt Planner** のルート（`llm.AgentPromptPlanner`。主・代替とフェイルオーバー・再試行・サーキットブレーカーは通常の LLM 呼び出しと同じ。`agent/prompttranslate`）。モデルの割り当てに「英訳」の役割は無く、モデル向けの英語のプロンプトを書く既存の役割を使う（役割を足さない）。
- **1 回の実行で 1 回だけ**: 訳す文を 1 回の LLM 呼び出しにまとめて送り、結果を実行の全モデル・全プロンプト・全本数で使い回す（本数ぶん・モデルぶん訳し直さない。シードの有無によらない）。入力画像には触れない（英訳はテキストだけ）。
- **訳せなければ止める**: LLM の失敗（API キー未登録・サーキットブレーカー・タイムアウト・使えない答え）、答えの件数が違う・空のときは `*gterr.UpstreamError`（画面 502・MCP `upstream`。メッセージは「何も生成していません。もう一度試すか、英訳を OFF にしてください」を含む）で `Run` を断る。バッチ・結果の行・プロンプト履歴・ジョブは作らず、モデルも呼ばない。**訳す対象の文が原文のまま返ったときは失敗にしない**: その文は入力した文のまま送り（`50Ω resistor` のように訳すものが無い文）、ほかの文は訳して使う。原文のまま送った文は `params.translation` に残さず、`translated` は偽になる（結果の行の `prompt` と `sent_prompt` は同じ）。**LLM の答えが無いのに黙って日本語で送ることはしない。**
- **見積りに入れる**: `Estimate` の内訳に、訳す文の数を `items` とする 1 行（`model` は `translate`、ラベルは「プロンプトの英訳（プロバイダー / モデル）」）を足す（LLM は割り当ての主のターゲットを名指しする）。**LLM には単価が無い**（Reasoning LLM の呼び出しは Take でもコスト台帳でもなく、料金表を持たない。ChatGPT のサブスクのように 1 回ごとの単価にならない経路もある）ので、既存の単価なしの扱い（§3 の `unknown`）に従い、**合計には入れず**、`unknown_models` に `translate` を載せ、警告 `no_price`（`model=translate`）で知らせる。`unknown_items` は結果の数なので増やさず、`total_items` にも入れない。ON/OFF は `estimate_id` の対象（切り替えると見積りの確認をやり直す）。見積りは LLM を呼ばない（訳すのは `Run` のとき）。
- **履歴には両方残す**: 結果の `prompt`・`negative_prompt` は入力した文のまま、訳した英語は結果の `params.translation`（`prompt`・`negative_prompt`）に残し、`ResultView` の `translated`・`translated_prompt`・`translated_negative_prompt` として出す。`sent_prompt` は実際に送った文（英語。ネガティブの欄が無いモデルは英語のネガティブを「Avoid: …」として書き足したもの）、ネガティブの欄があるモデルへは英語のネガティブを送る。バッチの `params` に `translate_to_english` を残す。プロンプト履歴（`gentest_prompt`）は入力した文で重複排除する。
- **ウェイト構文は残す**（Issue #972）: 英訳の指示に「`(token:weight)`・`(word)`・`((word))`・`[word]` は括弧・コロン・数字をそのまま残し、中の語だけを訳す。各語は自分のかっこと数字の中に置き、消す・足す・動かす・数字を変えることはしない」と、例（`(赤い狐:1.3), 夕暮れ` → `(red fox:1.3), sunset`）を入れてある（`agent/prompttranslate`）。LLM の答えは機械でも確かめる: 英訳のあとで、元の文にあった重みの数字と丸括弧が欠けていないかを調べ（`prompttranslate.LostWeights`）、欠けていたら**止めずに**結果の `params.translation.lost_weights` に残す（`ResultView.lost_weights`・結果の詳細の「英訳で消えたウェイト」。補正はしない: LLM が語を入れ替えたかっこは、機械では直せない）。英語だけの文は英訳にかからず、ウェイトごとそのまま届く。ComfyUI が読むのは `(…)` と `(…:数字)` で、`[…]` は読まない（ただの文字。§ [local-models.md](../local-models.md) §4.4）。
- **ウェイトの効き方の警告**: プロンプトに `(…)` があり、選んだモデルの `prompt_weight` が `unsupported`（Qwen-Image 系・FLUX.2 klein など。かっこと数字が文として読まれる）または `weak`（T5 系。効かないかもしれない）のとき、そのモデルに警告 `prompt_weight` を出す（代わりに文で強調する案内つき）。`unknown`（クラウド・自作）には出さない。
- モデルマネージャーの試し生成（1 モデル・1 プロンプト）は英訳しない（OFF。ローカルのモデルを入力のまま試す用途）。

## 3.3 生成の詳細設定（シード・ステップ数・CFG・サンプラー・スケジューラー。Issue #973）

`Request` の `seed`（固定するシード）と `sampling`（`steps`・`cfg`・`sampler`・`scheduler`。`localcfg/sampling.Settings`）。画面の「詳細設定」と、MCP の `estimate_generation_test`・`run_generation_test` が同じ値を渡す。**何も指定しなければ今と同じ**（シードは 1 本ごとにランダム、サンプリングはモデルのテンプレートの値）。

- **範囲**: シード 0〜4294967295、ステップ数 1〜150（2 段のサンプラーの Wan 2.2 は 2〜150。[../local-models.md](../local-models.md) §4.3）、CFG 0〜30、サンプラー・スケジューラーは ComfyUI の名前の一覧にあるもの。外れていれば、何も始めずに `validation`（画面 400）。
- **シードを固定すると**: モデルごと・プロンプトごとの n 本目（0 から）に `seed + n` を使う（本数の最後も範囲内に収める）。同じシードでモデルを比べられ、本数が複数でも 1 本ずつ違う絵になる。シードを渡せないモデルには送らない（警告 `seed_ignored`）。空ならこれまでどおり 1 本ごとにランダム。結果の `seed` に使った値が残るので、同じ条件で作り直せる。
- **モデルごとの扱い**: モデルが受け取る値は `modelcatalog.Info.Sampling`（`localcfg.Workflow.SamplingCaps()`。受け取る項目と既定。クラウドのモデルと、蒸留の LTX-2.5 は無し）。受け取らない値は**そのモデルには送らず**警告 `sampling_ignored`（「このモデルにはありません」）。**Speed mode（蒸留 LoRA）のあるモデルでは、`steps`・`cfg` を速さのモードが決める**ので、指定しても無視して警告 `sampling_ignored`（「速さのモードが決めます」。**Speed mode が優先**）。`sampler`・`scheduler` は上書きできる。
- **シードの範囲の検査**: `seed + (本数 - 1)` が `SeedMax` を超える組み合わせは拒否する。比較は `seed > SeedMax - (本数 - 1)`（足し算にしない。int64 の最大値のシードで桁あふれして通らないように）。
- **履歴に残す**: 結果の `params.sampling`（`steps`・`cfg`・`sampler`・`scheduler`）に、そのモデルが**実際に使った値**（指定していない項目はモデルの既定）を残す。`ResultView.sampling` と結果の詳細の「サンプリング」の行、MCP の `get_generation_result` に出る。記録が無い結果（#973 より前の履歴）は、モデルが今これらを受け取るなら「この結果には記録がありません（古い結果）」、受け取らないモデルなら「このモデルは受け取りません（モデルの既定）」と詳細の「使った設定」に出す。速さのモードのモデルは `speed_mode` が使った値を決める（`steps`・`cfg` は残さない）。
- **見積り・ID**: 値は `Estimate.Request` に入り、見積りの ID に含まれる（確認した内容と違う値では実行できない）。単価は変わらない。
- 実装は `service/gentest/sampling.go`（検証・`seedFor`・`planSampling`）と、`comfyui/sampling.go`（グラフへの書き込み）。

## 3.6 品質のプリセットとやりたいこと（`quality`・`intent`。Issue #977）

画面の「かんたん」（[../../components/gentest-tiers.md](../../components/gentest-tiers.md)）と MCP が同じ仕組みを使う。

- **品質**: `Request.Quality`（`fast`・`standard`・`high`。`localcfg/sampling.Quality`）。モデルごとのプリセット（`localcfg.Workflow.QualityPresets` = テンプレートの `quality_presets`）の steps・cfg を、**リクエストの `sampling` が名指ししていない項目にだけ**入れる（`Caps.WithQuality`。名指しが勝つ）。`standard`・無しは何も上書きしない: **何も触らない要求のグラフは、これまでのテンプレートのグラフと同じ**（`service/gentest/qualitytest` がグラフを比べる）。`Caps.Presets` に載るので `modelcatalog.Info.Sampling.quality_presets`（`list_generation_models`）に出る。速さのモードのモデル（steps・cfg は `locked`）・受け取らないモデル・プリセットの無いモデルは、警告 `quality_ignored`（標準の値で作る）。実際に使った steps・cfg は `params.sampling` に残る（品質の名前は残さない）。
- **プリセットの検査**: 読み込み時（`localcfg.ValidateModel`、`localcfg/quality.go`）に、プリセットが受け取る値（steps・cfg の割り当て）にだけ書かれ、速さのモードが無く、範囲内で、速いはワークフローの steps より少なく・きれいは多いことを確かめる。導入済みのコピーは `wfrefresh` が、テンプレートから steps を変えていないものだけ更新する。
- **やりたいこと**: `service/gentest/intent`（`intents.json`。埋め込み）が、選択肢（`photo`・`anime`・`landscape`・`face`）ごとに、モデルの候補（カタログのワークフロー名）を好みの順に持つ。`Service.Recommend(intent, hasReferences)` は、導入済みで呼べる（`Info.Available`。成人ゲートが閉じていれば成人向けは一覧に無い）・商用フィルター（`DefaultFilter`）に合う・この PC の OS／GPU の列の外でない（`Deps.PlatformWarning` = `mmview.PlatformWarning`。#979）・入力画像があれば画像を使える（顔の役割が要る選択肢は `face` を読める）最初の候補を選ぶ。選べなければ `Reason`（導入されていない／条件に合わない）。`Service.ApplyIntent(request, intent)` は `Models` を埋める（`models` と同時には指定できない。画像だけ。顔の写真が要る選択肢は入力画像が無ければ `validation`。「同じ顔で」は役割 `reference` の入力を `face` にする）。画面の見積りと MCP の `estimate_generation_test`・`run_generation_test` が使う。
- **復元**（結果の「もう1枚」「もう一度」）: `record.ResultDetail`（`TranslateToEnglish` を足した）から、画面の `redo.Of` が入力に戻す。新しいサービスの呼び出しは無い（画面のハンドラだけ）。

## 3.7 レシピと「画面に出す値」（Issue #977 ステージ 2）

**成人向けファイルを指す値（統合レビュー）**: `lora_name`・`ckpt_name` のようなファイル名を選ぶ入力を「画面に出す値」にして、リクエストの `exposed` で成人向けのファイルを渡すと、モデル自身のグラフは成人向けでなくても実行時にそのファイルを読む。`modelmanager.Service.HidesModelWith(モデル, 値)`（`gentest.Deps.HiddenWith`）が、値を入れた後のグラフ（`GraphWith`）を `adult.Files.Reads` で判定し、ゲートが閉じている間は `exposedreq.Refuse` が見積り・実行・ComfyUI で開く時に `validation` で断る。キューに積んだ後でゲートが閉じたときは、始まる時に `ReasonHidden` で失敗にする。選択肢は、ゲートが閉じている間、成人向けのファイルを外す（`exposedgate.Choices`・`Deps.HideChoices`。選択肢の値そのものを 1 ノードのグラフとして `adult.Files.Reads` で判定するので、まだ「画面に出す値」に選んでいない入力でも効く）。外す場所は、選ぶ画面（`exposedpick`）と、モデルの一覧が返す `exposed[].options`（画面・MCP の `list_generation_models`）の両方。グラフが今持っている値は残す。

- **レシピ**（`service/gentest/recipe`）: モデル（識別子）と画像の設定（`Params`: steps・cfg・sampler・scheduler・固定のシード・ネガティブ・LoRA と強さ・入力画像の強さ・縦横比・品質・画面に出す値）に名前を付けて持つ。表は `gentest_recipe`（`repository/reciperepo`。[er-gentest.md](er-gentest.md)）で、`Service.Recipes`（`Deps.Recipes`）が持つ。プロンプトの本文と入力画像は持たない。`Save` は `paramsOf` が、リクエストと同じ検査（`sampling.Validate`・シードの範囲・`refs.CheckLoRA`・`exposedreq.Check`）を通す。`Apply` はレシピをリクエストに入れる（リクエストが名指しした項目が勝つ。`models`・`intent` と同時は 422。使えないレシピは理由つきで 422）。**使えるか**は `status`: `Service.Model`（成人ゲートで隠れた・未導入・削除）→ `Available` → `intent.Excluded`（商用フィルター・OS／GPU。かんたんのやりたいことと同じ関数）。使えなくてもレシピは消さない。
- **成人ゲートが閉じているとき（Issue #992）**: モデルが `Hidden`（`modelmanager.HidesModel`）に当たるレシピは、**保存は残し**、`List`・`EasyChoices`・`Get`・`Rename`・`SetEasy` が返す `View` では、モデルの名前（`ModelLabel` は「成人向けのモデル」）・識別子・設定（`Params` は空）を出さない。`Usable=false`、理由は「成人向けのモデルのため、設定で成人向けを表示にしているときだけ使えます」。画面の一覧・かんたんの選択肢・MCP の `list_generation_recipes` は `View` だけを読むので同じ扱いになる（レシピは「保存して理由つきで残す」設計のまま、名前と設定だけを出さない）。名前の変更とかんたんへの出し入れは、保存された設定をそのまま書き戻し（`load`）、ゲートを開くと元の表示に戻る。
- **画面に出す値**（`localcfg/exposed`・`service/gentest/exposedreq`）: モデルの `Info.Exposed`（`[]exposed.Cap`）がワークフローの `exposed_inputs` とグラフの現在値（`default`）を持つ。リクエストの `Exposed`（キー `<node>.<input>`）は `exposedreq.Check`（選んだモデルのどれかが持つキーだけ。型・範囲・選択肢）で検査し、`exposedreq.Plan` がモデルごとの値（指定した値、無ければグラフの値）を決める。持たないモデルには警告 `exposed_ignored`。値は `ResultParams.Exposed` に残り、実行は `ImageOptions.Exposed` → `comfyui` の `build` が**割り当てより先に**グラフへ入れる（`Workflow.GraphWith`）。
- **今の設定を ComfyUI で開く**（`service/gentest/comfyopen`・`service/workflows/graphui`）: `comfyui.GraphFor`（実行と同じ `build`）が、リクエストの値を入れた API 形式のグラフを返す。`graphui.Convert` が、`GET /object_info` を使って画面の形式にし、`workflows.Service.OpenUI`（`OpenInComfyUI` と同じ置き方）が ComfyUI の `workflows/mam/` に置く。
- **選ぶ側**（`service/gentest/exposedpick`）: グラフのノード・入力の一覧と選べない理由、保存（保存したワークフロー、または接続先のモデルに書く）。
- テスト: `service/gentest/recipetest`（保存・復元・削除・未導入・exposed の範囲と警告・結果）、`exposedpick`・`comfyopen`・`graphui`・`localcfg/exposed`・`comfyui/exposedtest`、`repository/reciperepo`（マイグレーション）。

## 3.5 一部だけ描き直す（inpaint。Issue #974）

**検査（統合レビュー）**: マスクは `image.DecodeConfig` で寸法だけ先に読み、幅 × 高さが `MaxMaskPixels`（8192 × 8192 画素）を超えれば全体を展開せずに `validation`（小さいファイルで巨大な寸法の PNG でメモリを使い切らせない）。白い画素の探索は型ごとの高速経路で、最初の 1 画素で打ち切る。`mask`・`denoise` のどちらも無いのにインペイント専用のモデル（`SupportsInpaint`）を選ぶと、見積りの時点で `validation`（「一部だけ描き直す専用のモデルです。mask を指定してください」。MCP・estimate も同じ）。

元画像 1 枚とマスク（白＝描き直す、黒＝残す）で、マスクの内側だけを描き直す。リクエストの `mask`（参照と同じ `source` / `id` / `project_id`。役割ではない）と `denoise`（0 より大きく 1 以下。省くとモデルの `denoise_default`）。対応は `modelcatalog.Info.SupportsInpaint`（`reference_mode` が `inpaint` で、グラフに範囲内の `denoise` がある）。対応しないモデルを選ぶと見積りの時点で拒否する（警告ではない）。`denoise` を受け取らないモデルに強さを付けると警告 `denoise_ignored` でテンプレートの値のまま作る。マスクが真っ黒（白に近い画素が無い）・動画・マスク無しの `denoise` は拒否する。

グラフはコアノードだけ: 元画像とマスクを `LoadImage`、マスクは `ImageToMask`（`channel=red`）、`VAEEncode` → `SetLatentNoiseMask` → `KSampler` → `VAEDecode`、その結果を `ImageCompositeMasked` で元画像の上に戻す（`destination` が元画像、`source` がデコード、`mask` が `ImageToMask`）。外側の画素が元のままであることの確認は、このリンクのテストまで。画素の比較は GPU が要る（未実施）。テンプレートは `inpaint-sdxl` と `inpaint-flux-dev`（既定の denoise は 0.75）。既存のテンプレートのグラフは変えない。

結果は別ファイル。`params.inpaint`（使った `denoise`、元画像の `source_kind` / `source_id`、マスク入力の id）に残し、元が履歴の結果なら詳細からその結果へ戻れる。マスクはバッチの入力 `role=mask` で、参照としては送らない。作成フォームではインペイント専用のモデルを選べない（「一部だけ描き直す画面から使います」）。画面は `/generation-test/inpaint`。導入済みの SDXL / FLUX.1 dev は、起動時の更新が新しいワークフロー名を足さないので、インペイント用のワークフローは入れ直すまで一覧に出ない。

## 3.4 大きくする（アップスケール。Issue #975）

成功した画像の結果を **2 倍・4 倍**にする。結果の詳細の「大きくする」（倍率と方式を選んで 1 操作）と、MCP の `upscale_generation_result`（結果 ID＋倍率）が同じ `Service.Upscale` を呼ぶ。**見積り・確認は無い**（アップスケールのワークフローはローカルの ComfyUI のモデルで、無料。`basis=free`）。

- **別の結果として残る**: 元の結果のファイルを入力として新しいバッチ（結果 1 件・入力 1 枚。origin は `history`）に複製し、ジョブ（種別 `gentest`）で動かす。元の結果は変わらず残り、大きくした結果は履歴の別の行・別のファイル。**元へのリンクと使った値**は結果の `params.upscale`（`record.Upscale`: `source_result_id`・`source_model`・`scale`・`model`・`mode`・`native`・元と出力の大きさ）に残し、`ResultView.upscale`・結果の詳細の「拡大」「大きさ」「元の結果」の行と元の結果へのリンク・MCP の `get_generation_result` に出る。元を後で消してもこの結果は残る（リンクだけが切れる）。大きくした結果をもう一度大きくしてもよい。
- **ワークフロー種別として扱う**: アップスケールは、**画像モデルの選択とは別の「ワークフロー種別」**にした。ComfyUI のワークフロー（`localcfg.Workflow`）の `upscale`（`localcfg/upscale.Spec` = `{label, native, mode, max_output_pixels}`）で印を付け、同じエンジン（同梱の `bundled-comfyui`）の画像ワークフローとして作る。印のあるワークフローは**プロンプトを渡すモデルの一覧に出さない**（`localcfg.IDs`・生成テストの `modelcatalog.Models`・モデルマネージャーの試し生成から除く）。「大きくする」は `modelcatalog.Catalog.Upscalers` が出す専用の一覧から選ぶ。既存の画像モデルの選択 UI・見積り・確認の流れには触れない（最も小さい変更）。
- **方式**（`upscale.mode`）: `model` = `LoadImage` → `UpscaleModelLoader` → `ImageUpscaleWithModel`（内部でタイルに分けて処理する。タイルは 512 から始め、メモリが足りなければ半分にする）→ `ImageScaleBy`（lanczos）→ `SaveImage`。`two_pass` = 同じ拡大のあと、`VAEEncodeTiled` → 低い denoise（0.3）の `KSampler`（SDXL base）→ `VAEDecodeTiled`（どちらもタイル 512・重なり 64）→ `SaveImage`。ComfyUI 本体のノードだけ。
- **倍率**: モデルの倍率（`upscale.native`。今のカタログは 4 倍）と依頼の倍率の比 `scale / native` を、`ImageScaleBy` の `scale_by` に入れる（値の割り当て `upscale_ratio`。4 倍の依頼はそのまま、2 倍は半分に縮める）。倍率は `UpscaleScales`（2・4）以外なら `validation`。
- **2 パス仕上げ**: 元の結果の**プロンプト・ネガティブプロンプト・送った文・シード**を引き継ぐ（元が英訳されていたら英訳の記録も）。モデルは SDXL base の固定（元の結果のモデルには依存しない。元が別のモデルでも、プロンプトは文として SDXL に渡す）。CLIP のウェイト構文は効く（`prompt_weight: supported`）。
- **上限**: 出力の画素数（幅 × 高さ）が `MaxUpscaleOutputPixels`（64 Mpx = 8192 × 8192）を超える、またはワークフローの `max_output_pixels`（2 パスは 16 Mpx = 4096 × 4096）を超えるなら、何も始めずに `validation`（元の画像の大きさは、保存した画像の先頭から読む）。理由は、PNG が数百 MB になってエンジンの応答の上限（300 MiB）に近づくことと、2 パスは画像全体をサンプリングするため。**mam が決めた安全側の値で、実測の値ではない**。
- **入っているモデルだけ**: モデルマネージャーでアップスケール用のモデルを入れる（カタログの `kind: upscale`。[../model-manager/model-manager.md](../model-manager/model-manager.md) §4.2「アップスケール用モデル」）と、そのワークフローが同梱エンジンに作られる。無ければ「大きくする」はモデルマネージャーへの案内を出し、`Upscale` は `validation`（「アップスケール用のモデルが入っていません」）。`model` を省くと、入っているもののうちプロンプトの要らない方式（`mode=model`）の最初。名指しのモデルが入っていない・使えない（年齢確認・サーキットブレーカー）なら `validation`。
- **16GB の GPU（未測定）**: ComfyUI のタイル処理（`ImageUpscaleWithModel`・`VAEEncodeTiled`・`VAEDecodeTiled`）で、全体を一度に GPU に載せない作りにした。**16GB の GPU で 4 倍が落ちないことは、実機で測っていない（未測定。実機で確認が要る）**。VRAM の目安（カタログの `vram`）も mam の見積もりで、`estimate: true`。
- テストは `service/gentest/localtest/upscale_test.go`（ComfyUI の偽のサーバーを通して、別ファイルの結果・元の記録・倍率の比・2 パスのプロンプトとシード・拒否・上限・モデルの一覧から外れること）、`web/handler/gentest/upscale_test.go`（画面）、`mcp/tools/gentest/upscale_test.go`（MCP）。

## 3.2 プロンプトマッチ（Issue #916）

参考の画像 1 枚から、AI が英語のプロンプトを作り、生成して、似ているかを測り、違いをもとに直して生成し直す（おまかせ・1回だけ・調整。言葉の指示で直せる）。生成テストのバッチとして動き、各回は結果として履歴に残る。設計は [prompt-match.md](prompt-match.md)。

## 4. バッチとジョブ

`Run` の手順: 入力画像を解決して履歴の側へ複製 → `gentest_batch`・`gentest_input`・全結果の行（`queued`）・プロンプト履歴を 1 つのトランザクションで作る → ジョブ（種別 `gentest`、`payload` は `{"batch_id": ...}`）を投入。`resumable=false`。

ジョブ（`handle`）: ベンダーごとにワーカーを置き（数は `Adapter.ConcurrencyLimit(vendor)`）、ベンダー同士は並行して動かす。1 件ごとに `running` → 呼び出し → `succeeded`/`failed` を、`context.WithoutCancel` でその都度記録する（mam が終了中でも、完了済みの結果は残る）。結果ファイルは `<data-dir>/gentest/<バッチ ID>/<結果 ID>.<拡張子>` に原子的に書く。

- **進捗**はテーブルの件数（状態ごとの `COUNT`）から求める。画面は 2 秒ごとに取り直し（HTMX）、MCP は `get_job`・`get_generation_test`。件数のほかに、ローカルの ComfyUI が動いている間だけ、ステップの進み（`progress`: ステップ・%・残りの見積り）を、メモリ上の記録（`agent/modeladapter/comfyui/live`。ジョブキューのジョブ ID（`BatchStatus.job_id`）を持ち主にして、ComfyUI のバックエンドが書く。get_gpu_status と共通）から読む。永続化せず、無ければ省く（Issue #966）。
- **取り消し**（`Cancel`）: まだ始めていない結果を `cancelled`（理由「キャンセルされました」）にし、新しい呼び出しを始めない。**呼び出し中のものは終わるのを待ち**、その結果は残す（ベンダー側では生成が続き課金されうるため）。終わったバッチの取り消しは 409。取り消しの登録はメモリ上（`runs`）。
- **削除**: 結果の削除は行とファイルを消し、バッチの最後の 1 件を消すとバッチ・入力・ディレクトリも消す。終わっていない結果は 409。バッチが先に消されたジョブは何もせず終わる。

## 5. 失敗・中断の扱い

- 1 件の失敗（キー未登録・ブレーカー開・タイムアウト・ベンダーの拒否・入力を受け付けない）はその結果を `failed`・理由つきにし、他の結果は続ける。理由は日本語に直す（`Explain`: モデルマネージャーの拒否の文言。ベンダーのタイムアウトは「ベンダー側では生成が続き、課金される場合があります」を添える。ローカル（登録した接続先）のタイムアウトは `localcfg.TimeoutError`（接続先のベンダー `endpoint:<名>` への呼び出しのタイムアウトを `modeladapter.call` が包む。`ErrTimeout` のままでもある）で見分け（文言は `gentest/failtext`）、課金には触れず、ComfyUI のジョブが止まったとき（`TimeoutError.Stalled`）は「N のあいだ進まなかったため打ち切りました。ComfyUI が止まっていないか確かめてください…タイムアウト（進みが止まってから打ち切るまでの時間）を延ばしてください」、全体の上限（24 時間）に達したときは「全体の上限（24時間）を超えたため…解像度や秒数を小さくして実行し直してください」、互換の接続先は「接続先の待ち時間（N 時間）を超えたため打ち切りました。設定画面の接続先でタイムアウトを延ばすか、解像度や秒数を小さくして実行し直してください」と出す）。**再試行して別モデルへ回すことはしない**。
- **mam の異常終了**: ジョブは再開しない（生成は課金されるため）。起動時（`jobqueue.Recover` のあと）に `OnStartup` が、`queued`・`running` のまま残った結果を `failed`（理由「mam が停止したため中断されました（課金される生成は再開しません）」）にする。完了済みの結果は残る。24 時間以上前の入力のステージング（`_staging/`）も消す。
- ジョブ自体が失敗するのは、バッチを始められないとき（入力画像のファイルが読めない等）だけ。その場合は未着手の結果をすべて `failed` にする。

## 6. 入力画像

入力は、アップロード（画面は見積り時に、MCP は `gentest_reference` で `_staging/` に置く）、プロジェクトのアセット・Reference Library の項目（`project_asset` / `reference_item`）、履歴の成功した画像結果（`history`）。選んだ画像は**バッチの `gentest_input` に複製**する（`origin` に元を記録）ので、元のプロジェクトや結果を後で消しても履歴は読める。画像は PNG・JPEG・WebP を中身で検査し、1 枚 20 MiB まで・1 バッチ 10 枚まで。

動画の役割は `reference`（自由な参照）/ `first_frame` / `last_frame`。モデルの能力（`Capabilities`）で、使えない役割は**そのモデルからだけ外して警告**する。

**動画の速さのモード（Issue #930）**: 動画のリクエストの `speed_mode`（`fast` / `standard` など。省くと各モデルの既定）は、ComfyUI のワークフローが `speed_modes`（[local-models.md](../local-models.md) §3）を持つモデルだけに効く（Wan 2.2 14B + LightX2V: `fast` = 4 ステップ・cfg 1、`standard` = LoRA なしの通常 20 ステップ・cfg 3.5）。モードは `modelcatalog.Info.video.speed_modes`（`id`・`label`・`default`）にあり、画面は動画のフォームに「速さ」の選択（`data-testid="gt-speed-mode"`。どれかのモデルが持つときだけ描く。選択肢は「モデルの既定」とモードのラベル）を、MCP は `list_generation_models` の `video.speed_modes` と `estimate_generation_test`・`run_generation_test` の `speed_mode` を出す。そのモードを持たないモデルは **警告 `speed_mode_unsupported`（拒否ではない）で既定のまま**作り、画像のリクエストに付けるか、名前が `^[a-z0-9][a-z0-9-]{0,39}$` でなければ 400（`invalid`）。選んだモードは見積りの確認（hidden の `speed_mode`）を通って結果の `params.speed_mode` に残り、`get_generation_test` の結果の `speed_mode` に出る。見積りの ID（`estimate_id`）はリクエスト全体の要約なので、モードを変えると別の見積りになる。

**画像モデルの参照（Issue #933）**: 画像の入力は、役割 `reference`（全体。既定）/ `face`（顔だけ）/ `style`（画風・雰囲気だけ）/ `edit`（編集する元の画像）と、強さ `strength`（0〜1。省くと `localcfg.DefaultStrengthOf(role)`: 既定は `DefaultReferenceStrength` = 0.7、役割 `face` は `DefaultFaceReferenceStrength` = 0.9。[local-models.md](../local-models.md) §4.5）を持つ。強さの実現方法（img2img の denoise、編集モデルへの指示の強さ、アダプタの重み）はモデルごとに違い、**画面と MCP は同じ「強さ」の 1 つの値だけを出す**。範囲外の強さ・存在しない役割・動画への強さ・LoRA・ポーズ設定は入力の誤り。リクエスト全体に、`avoid_reference_pose`（参照を使うモデルのネガティブプロンプトに `localcfg.AvoidPoseNegative` を足す。ネガティブの欄が無いモデルは既存の畳み込み（`Avoid: …`、警告 `negative_folded`）。参照を使わないモデルは何もしない）と `lora`（名前・強さ 0〜1、既定 `localcfg.DefaultLoRAStrength` = 0.4。画像だけ）がある。名前は 1〜200 文字で制御文字を含まない（サブフォルダの `/` と `.` は可）。強さを実現できるのは同梱 ComfyUI の参照対応モデルだけで、クラウドの画像モデルなどでは無視される（警告は出さない）。**強さの保存値は入口で違う**: 画面のフォームは常に明示の強さを送る（省いた欄は 0.7 として保存）が、MCP は省くと保存しない（`gentest_input.strength` は NULL で、モデルが既定を使う）。

モデルごとの扱いは `modelcatalog.Info` の `reference_mode`（`edit`＝編集モデル: 複数枚・役割あり / `img2img`＝1 枚から始める）・`max_reference_images`・`reference_roles`・`supports_lora`（どれもワークフローの割り当て・`reference_mode`・`lora_model_node` から `modelcatalog.ReferenceCapsOf`（`localcfg.Model.ReferenceCaps`）と `modelcatalog.SupportsLoRA` が決める。クラウドの画像モデルは参照を使えても枚数・役割は不明で、役割は `reference` だけ）で決まる。**対応しない分は拒否せず、そのモデルからだけ外して警告する**: 参照を使えないモデル `inputs_dropped`、枚数超過 `too_many_refs`（先頭から `max_reference_images` 枚。img2img のモデルは**先頭の 1 枚だけ**を使い、残りは `too_many_refs` の警告になる）、そのモデルが取らない役割 `role_unsupported`（画像は全体の参照として使う）、LoRA に対応しないモデル `lora_unsupported`（LoRA は送らない）。結果の行には各入力の `role`・`strength`（`gentest_input`）と、使った `avoid_pose`・`lora`（`params`）を残し、`direct.Image` へは `modeladapter.Input.Role` / `Strength` と `ImageRequest.LoRA` として渡す（強さ 0 は「既定」と区別するため、参照も LoRA も `refs.MinStrength` = 0.01 にして送る。履歴には選んだ 0 を残す）。LoRA の名前の一覧は同梱 ComfyUI エンジンの `GET /object_info/LoraLoaderModelOnly`（`lora_name` の選択肢）から `Service.LoRAs(ctx, media)` が数秒の間だけ覚えて返す（エンジンが止まっていれば空。失敗も同じ間覚え、取得中の呼び出しは 1 回の取得を共有するので、応答しないエンジンでも待つのは 1 回だけ。動画では LoRA を使えないのでエンジンに問い合わせない）。 警告 `role_unsupported` には、そのモデルが使わない（全体の参照に落とす）参照の役割 `roles`（`face` / `style` / `edit`。実際に使う参照だけ・重複なし・参照の並び順）が付く（`gentest.Warning.Roles`、`refs.Warning.Roles`）。画面は、これを源にして実行の前に「◯◯は「顔だけ」に対応していないので、全体の参照として使います。構図や服も寄ります」というモーダルをモデルごとに出す（Issue #959。[../../components/gentest-screen.md](../../components/gentest-screen.md)）。警告と全体への置き換え（`refs.RoleFor`）は変えない

**「顔だけ」の参照の切り出し（Issue #960）**: バッチを走らせるとき（`runner.RunJob`）、役割 `face` の入力画像を 1 回だけ顔のまわりに切る（`Deps.FaceCrop`。アプリは `facesim.CropFace`、顔検出モデルが無いと nil で何もしない）。結果ごとの呼び出しで、**役割 `face` を取るモデルへだけ**切った画像を渡し（`refs.InputOf` が決めた役割が `face` のとき）、全体の参照に落ちるモデルと、他の役割には元の画像を渡す。顔が無い・小さい・アップの画像と検出の失敗は元の画像のまま（失敗はログだけ）。履歴に残る入力は元の画像。詳細と効果の測り方は [local-models.md](../local-models.md) §4.5。

**ControlNet・IP-Adapter（Issue #976）**: `reference_mode` が `controlnet`（参照画像は制御画像。コアのノード）・`ipadapter`（IP-Adapter の参照画像。追加ノード `ipadapter-plus`）のモデルは、参照画像を**ちょうど 1 枚**要求し（`max_reference_images` 1、役割は `reference`）、参照の強さ（0〜1、既定 0.7）がノードの強さ（`ControlNetApplyAdvanced.strength`・`IPAdapterAdvanced.weight`）に入る（割り当て `reference_strength`）。画像が無い実行は、制御画像／IP-Adapter の参照画像が要ると言って拒否する。**ワークフローが追加ノードを要る（`requires_nodes`）同梱エンジンのモデルは、そのノードが導入・読み込み済みになるまで一覧で `available=false`・`unavailable=missing_node`（理由に、ノードの名前と導入する場所）になり、バッチに入れられない**（`modelcatalog.Catalog.MissingNodes`。`app/gentest.go` が `nodegate.ForModels` を渡す。外部の ComfyUI のモデルは対象外）。詳細は [../model-manager/custom-nodes.md](../model-manager/custom-nodes.md) §6。

**ポーズの参照（Issue #985・#986）**: 画像の参照の役割に `pose`（ポーズだけ。画像のみ・動画には使えない）を足した。参照画像の体のポーズとカメラの角度だけを写し、絵柄・色・顔・髪・服・背景は写さない役割で、`edit` 方式のモデル（Qwen-Image 2.1 の `txt2img-qwen-image21`〔TextEncodeQwenImage21〕・Qwen-Image-Edit-2511〔EditPlus〕）が持つ（`localcfg` の `ModeEdit` の役割）。エンコーダーが画像に付ける番号を指す 1 文（「`<imageN>` はポーズの参照としてだけ使う: …」。`comfyui/stage.roleSentence`）をプロンプトに足し、ポーズの画像は `face`・`style` と同じく視覚の入力にだけ渡し、VAE を渡さない段では VAE の潜在（latent）を作らない（絵に写させないため。`stage.seenOnly`）。ただし**「全体」「編集対象」の画像が 1 枚でも同じ段にあると、エンコーダー全体に VAE を渡すため、顔・画風・ポーズの画像の画素も参照の潜在として入ることがある**（`TextEncodeQwenImage21`・`TextEncodeQwenImageEditPlus` は `vae` があると全ての画像を潜在にする。ComfyUI v0.42.1 の `nodes_qwen.py` を読んだ範囲。この混在の段の実際の挙動は GPU で確かめていない: `needs_real_device`）。役割を持たないモデルは、ほかの役割と同じく**全体の参照として使い、警告 `role_unsupported`（「ポーズ」と名指し）**を出す。「参照画像のポーズ・構図を真似させない」（`avoid_reference_pose`）は、`pose` の参照を持つ画像モデルでは、その参照を除いて数える（ポーズを写すのが目的の参照のため）。

入力の出どころに `pose_library`（ポーズ集。`RefInput.source`、`ref` の値は `pose_library::<画像 ID>`、画像 ID は `std_P001_front` の形）を足した。ポーズ集は **Shisei**（`wquguru/shisei`。マネキンの線画。コードは Apache-2.0、画像は CC0 1.0）を、利用者が同意して導入したときだけ取得して使う（[model-manager/local-runtime.md](../model-manager/local-runtime.md) §12.9。mam は同梱しない）。`service/gentest/poses`（`Library`）が導入・一覧・絞り込み（文字・分類・アングル）・ファイルの解決を持ち、全年齢向け（`rating: general`）で線画のあるポーズだけを出す。未導入・ポーズ集が無い・ポーズ集にない画像は、入力の誤りとして理由と手順を日本語で拒否する。選んだ画像は他の出どころと同じく**バッチへ複製**し（`origin` は `{kind: "pose_library", id}`）、ポーズ集を消しても履歴の入力は読める。ポーズ集は画面（一覧の下の削除ボタン・導入中の進み具合）と MCP（`get_pose_library`・`install_pose_library`・`remove_pose_library`。実装は `mcp/tools/gentestposes`）から導入・状態・削除できる（Issue #994）。画面は入力画像の区画の「ポーズを選ぶ」から、導入の案内（出典・ライセンス・同意）か絞り込み付きの一覧を出す（[../../components/gentest-screen.md](../../components/gentest-screen.md)）。**ポーズの画面にも、選んでいるモデルの商用利用の可否（モデルカードと同じバッジと注意書き）を出す**（Qwen-Image 2.1・Noct Q は Qwen RESEARCH LICENSE で非商用）。

**VNCCS PoseStudio（Issue #985）**: ルート ①（T2I + LoRA）と ②（Edit）は、コアのノードだけの既存テンプレート（`txt2img-qwen-image21`・`txt2img-qwen-image-edit-2511`）と LoRA の差し込み（`lora_model_node`）で動く。ルート ③④（`VNCCS_PoseStudio` のノード）は、追加ノード `vnccs-utils`（[model-manager/custom-nodes.md](../model-manager/custom-nodes.md) §2.1）が要るが、ノードがブラウザの画面でポーズを取り込むのを待つ作りで、API だけで流す mam のグラフでは動かせず、実行時の自動取得も固定できないため、**許可リストに載せるだけで導入できず、テンプレートも作らない**。LoRA `MIUProject/VNCCS_PoseStudio_QI2.1`（V2。約 159 MB。ライセンスの記載なし）は、PoseStudio の 3D 人形の画像を入力に学習されている（作者のワークフローのプロンプトは「replace the pose of <image 2> with the pose of <image 1>. keep the character of <image 2>」、強さ 1.0）ため、Shisei の線画との組み合わせは未検証で、mam のカタログには載せていない。ポーズの参照は、LoRA なしでも `pose` の役割で使える。

## 7. プロジェクトへの取り込み（`projectlink`）

成功した結果ならどれでも取り込める（お気に入りは目印にすぎない）。取り込み先: **参照**（Reference Library の項目。画像のみ。`source='generated'`、`source_shot_id` は NULL、`kind`・`name`・`tags`）か**アセット**（画像・動画）。ファイルはプロジェクトの assets ディレクトリへ複製する。続けて、参照を Character の face/body・Product・Location のスロットに割り当てられる（`WorldService.AssignReference`。スロットの種類と参照の種類が合わなければ入力の誤り、LOCK されていれば 409）。割り当てに失敗しても取り込みは残り、そのことを知らせる（`PartialImportError`）。取り込み先は `gentest_import` に残す（TEXT の ID のみ）。

## 7.1 結果の ZIP エクスポート（`service/gentest/export`。Issue #932）

履歴の結果を ZIP にまとめて持ち出す。ZIP は `photos/`（画像・動画。`01_20261101-173805_chroma1-hd_bce291.png` = 番号・作成時刻（JST）・モデル・結果 id の末尾 6 文字）と `prompts/prompts.csv`（`file`, `created_at`（JST）, `media`, `model`, `seed`, `prompt`, `sent_prompt`, `result_id`）の 2 フォルダ。番号は古い順の 1 から、ファイル名の番号と CSV の `file` 列が同じ。

- **選ぶ処理は 1 か所**: `export.Selection`（結果の指定 `ResultIDs`・`BatchID`・`Latest`（新しい順の N 件。上限 500）・`FavoriteOnly`・`Models`・`Media`・`Commercial`）を `Exporter.Plan`（MCP の `export_generation_results`）と `Exporter.Archive`（画面の `GET /generation-test/export`）が同じ `resolve` で解く。条件は AND。成功してファイルが実在する結果だけが入る。何も言わなければ直近 40 件（`ResultIDs`・`BatchID` があれば全部）。`Commercial` は `all` / `ok` / `ok_or_conditional`（#928 と同じ `modellicense.Filter`）で、省略は `all`（設定の既定には従わない。書き出しは頼まれない限り絞らない）。指定の `id` が入らなかったときは `Plan.Skipped` に出す。
- **ストリーム**: `Archive.Write` は CSV を 1 行ずつ書き、ファイルを `io.Copy` で 1 つずつ ZIP に流す（画像・動画は圧縮済みなので `Store`）。巨大な ZIP でもメモリに溜めない。途中で失敗したら中央ディレクトリを書かずに止め、壊れた ZIP は開けない。
- **分割**: `Split{Items, Bytes}`。件数かファイル合計のバイト数を超える結果から次の ZIP にする（最大 `MaxParts` = 20）。番号は全体の通し番号。各 ZIP は `Ref`（`part/parts/first/total/id,id,…`）で結果を指し、MCP のダウンロードチケットのリソース ID になる（URL には載らない）。発行時に選ばれた結果を返し、後から履歴が増えても変わらない。
- **CSV の安全**: UTF-8 BOM つき・`encoding/csv`（改行・カンマ・引用符は RFC 4180 でエスケープ）。モデル名・プロンプト・送った文が `=` `+` `-` `@`・タブ・復帰で始まるときは先頭に `'` を足す（CSV インジェクション。Excel・LibreOffice・Google スプレッドシートが数式として実行しない。プロンプトは MCP クライアントが外から読んだ文を書き込み得る自由な文）。数値のシードと mam が作る列は対象外。
- **ファイル名**: 英数字・`.`・`_`・`-` 以外を `-` にし、`..` を 1 つの `.` にし、両端の `-` `.` を削る（最大 40 文字。空は `model`）。パスの区切りも `..` も残らない。拡張子は保存ファイルの拡張子（英小文字・数字 5 文字まで）、無ければ MIME から。
- **認可**: MCP は MCP のトークンが要るツールが、既存のダウンロードチケット（15 分・発行したトークンの失効で 401）の URL を返す。画面はローカル限定のサーバーの GET。ZIP は結果 id の指定を検証して（`[0-9A-Za-z_-]{1,64}`）から SQL の `IN (?)` に渡す。

## 8. 画面（`/generation-test`）

プロジェクト一覧の分析・設定の隣にリンク。画像／動画の切り替え、モデルのカード（タップで選ぶ。選べないものは「選べません」のバッジで、理由は「?」ヘルプと読み上げにある）、プロンプトの追加（複数）とネガティブ、本数・縦横比（動画は解像度・長さ）、入力画像（ドロップゾーン 1 つ＋プロジェクト・履歴のモーダル。サムネイルの格子で番号・外す・並べ替え・拡大。画像は役割（全体・顔だけ・画風だけ・編集対象）と強さを、選んだモデルが使えるときだけ出し、ポーズを真似させない設定と LoRA（名前・強さ）がつく。画像／動画を切り替えると、ブラウザで選んだファイルは外れる（画面ごと入れ替わるため）。モデルのカードに入力画像のアイコン。動画は最初／最後のフレーム・自由な参照の役割）、「見積りを見る」→ 確認（件数・モデルごとの内訳・合計・単価なしの注記・警告・上限超過）→ 「この内容で実行する」→ 進捗（2 秒ごと・取り消し）と比較表（行＝モデル、列＝プロンプト）。`/generation-test/history`（モデル・種類・お気に入りで絞り込み、詳細、削除、お気に入り、ファイルのダウンロード、合計コストの 1 行）、`/generation-test/prompts`（プロンプト履歴: 再利用・削除）。`/generation-test/inpaint`（一部だけ描き直す。§3.5。ナビ「一部だけ描き直す」。結果の詳細の同じ名前のボタンから、その結果を元画像にして開く）。詳細は取り込みフォーム（参照／アセット・種類・名前・タグ・スロット）を持つ。ルートは [endpoints-gentest.md](../../api/endpoints-gentest.md)。英訳のスイッチ「英語に訳して送る」（既定 ON。説明は「?」。§3.1）があり、訳した英語は比較表の列と結果の詳細に出る（入力した文と送った英語の両方）。

プロンプト履歴は（プロンプト, ネガティブ）で重複排除し、使用回数と最後に使った日時を持つ。**明示の削除だけ**で消え、結果の履歴とは独立（結果を消しても残り、消しても結果に影響しない）。

## 9. テスト

見積り（3 状態・丸め・上限）・直接呼び出し（代替なし・プロンプトそのまま・シードは対応モデルだけ・ブレーカーと同時実行）・バッチのジョブ（全組み合わせ・取り消し・クラッシュ後の整理・Take・台帳が増えないこと）・履歴（絞り込み・お気に入り・プロンプトの重複排除・削除）・取り込み（参照・アセット・スロット・LOCK・ファイルの複製）・マイグレーションの up/down・画面（CSRF・流れ・HTMX の進捗）・MCP（見積り → 実行の 409・`get_job`・トークン URL）。英訳（ON/OFF・英語はそのまま LLM を呼ばない・1 回の実行で 1 回だけ・失敗でモデルを呼ばない・履歴に両方・見積りの単価なしの行・ネガティブの欄あり／なし。`servicetest/translate_test.go`、`agent/prompttranslate`）。偽のバックエンドは `service/gentest/gentesttest`、ローカルの ComfyUI を通した結果までは `service/gentest/localtest`。実際のベンダー API（シードの効き・価格）は実機で未確認。 ZIP のエクスポート（`export`: `photos/` と `prompts/prompts.csv` の構造・BOM・`encoding/csv` での再読み込み・番号とファイル名の対応・直近 N 件／バッチ／指定／お気に入り／モデル／種類／商用利用の選び方・分割・数式で始まるセル・ファイル名の無害化・ストリームの失敗）と、画面の選択（`export-pick.wtest.ts`: タップで枠が光る・自動更新で選択が消えない・隠れたカードを送らない）。

## 10. パッケージの構成（Issue #963）

`service/gentest` は 1 ディレクトリ 行数上限（linterly。ディレクトリ 2000 行）に収めるため、責務ごとにサブパッケージへ分けてある。依存は一方向（下から上）で、サブパッケージは `service/gentest` を import しない（`service` も import しない。拒否は `gterr`）。

| 置き場所 | 責務 |
|---|---|
| `service/gentest`（直下） | `Service`・`Deps`（下のサブパッケージを束ねて公開の窓口にする）、リクエスト（`Request`・検査）、見積り（`Estimate`・`Confirmation`・`planModel`）、バッチの作成と実行の開始（`Run`）、入力画像の置き場と解決（`StageReference`・`ResolveInput`）、バッチの状態（`BatchStatus`）、英訳、モデルの一覧 |
| `runner` | ジョブ `domain.JobGentest` のハンドラ（`RunJob`）、結果 1 件の生成と確定（`execute`・`settle`）、`Generate`・`Cancel`、失敗の理由（`Reason*`・日本語への直し） |
| `history` | 履歴（一覧・詳細・ファイル・お気に入り・削除・プロンプト履歴・累計）。`Store` |
| `record` | 履歴の JSON 列（`BatchParams`・`ResultParams`・`SentText`）、ジョブのペイロード、結果・入力・取り込み・プロンプトのビュー（`ResultView` ほか）。`runner`・`history`・直下が共有する型 |
| `export` / `refs` / `match` / `matchinfo` / `modelcatalog` / `projectlink` / `loras` / `failtext` / `gterr` | それぞれ ZIP エクスポート、入力画像と LoRA の判定、プロンプトマッチ、マッチの記録、モデルの一覧、プロジェクト側、LoRA の一覧、失敗の文言、拒否の型 |
| `poses` / `posetest` | Shisei のポーズ集（導入・一覧・絞り込み・ファイルの解決。`posestest` は偽の GitHub で導入までを走らせる道具）と、ポーズの画像を入力にした実行のテスト（Issue #986） |
| `intent` / `qualitytest` | やりたいこと（`intents.json`。埋め込みの選択肢と候補モデル・`Recommend` の選び方。Issue #977）と、品質のプリセット・やりたいことの選択・何も触らないときのグラフを走らせるテスト |
| `recipe` / `exposedreq` / `exposedpick` / `comfyopen` / `recipetest` | レシピ（保存・一覧・使う・使えない理由）、リクエストの画面に出す値の検査と計画、画面に出す値を選ぶ側、今の設定を ComfyUI で開く、それらのテスト（Issue #977 ステージ 2。§3.7） |
| `gentesttest` / `servicetest` / `localtest` / `speedtest` | テストの道具と、パッケージをまたぐテスト |

`Service` は `projectlink.Link`・`runner.Runner`・`history.Store` を埋め込み、これらのメソッドを自分のものとして公開する。`Deps.Extra`（プロンプトマッチ）は `runner.Runner` のフィールドで、`match.New` が組み立てるときに入れる。

## 改訂履歴

[gentest.changelog.md](gentest.changelog.md)
