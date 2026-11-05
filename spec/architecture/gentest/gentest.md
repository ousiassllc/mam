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

画像: 組み込みのクラウドモデルすべて（`domain.ImageModels()`）と、#831 のローカル・OpenAI 互換のモデル、#832 の ChatGPT サブスクの画像モデル（`gpt-image-2:chatgpt-subscription`。モデル選択・見積りの内訳・履歴には表示名 `GPT Image 2 (ChatGPT サブスク)` で出す。`chatgptid.Label`、`localbackends.Options.Publish` が `modelreg.SetLabels` で登録する。ON/OFF によらず登録する。Issue #870）、#834 の保存したワークフロー（`comfy:<id>@v<N>`）。動画: `domain.VideoModels()`（組み込みと #831 のローカル動画モデルと #834 のワークフロー）。**モデルの表示名（`Info.Label`）**: モデルマネージャーが入れたモデルはカタログの名前（`catalog.Model.Name`。1 項目が複数のワークフローを持つときは `SDXL base（img2img）` のように違いを括弧で足す。`mmview.DisplayName`、`modelfilter.CatalogInfo.Name`）、カタログが知らないローカルモデル（外部の ComfyUI など）は `comfyui:<接続先>/` を除いたモデル名、保存したワークフローは `<名前> (v<N>)`、クラウドはモデル名。画面（カード・ヘルプ・見積りの内訳・警告）は内部 ID を出さず、この名前で出す（Issue #1023）。

**今使えるものだけ選べる**: `Adapter.Unavailable(model)` が理由を返すモデルは、一覧に残して無効にし、理由を出す（API キー未登録・接続先を使えない・ChatGPT にログインしていない・サーキットブレーカーが開いている）。`Unavailable` は #835 で画像モデルにも広げた（バックエンドが無い・設定されていない・ブレーカー開）。ChatGPT サブスクはログアウト後も「呼び出しの失敗」になりうる（一覧はバックエンドの有無と設定だけを見る）。

**モデルマネージャーのカタログのモデルは、カタログの導入状態と同じ答え（Issue #1025）**: `modelcatalog.Catalog.availability` は、アダプターの理由（API キー・サーキットブレーカー・年齢確認）のあと、`CatalogInfo`（`mmview.InfoOf`。カタログのモデルをワークフロー名で引き当てる）の `NotInstalled` を見る。記録が無い・必須のファイルが欠けている・導入中のモデルは `unavailable: not_installed`（理由の文はカタログと同じ）で、必須のファイルが揃っていれば使える（任意の Lightning LoRA だけ無いときは使え、`catalog.missing_optional_files[]`・`catalog.partial_note` で「速さのモードの一部は未導入」と分かる。その任意ファイルを使う速さのモードは `speed_modes` から外れ、`blocked_speed_modes[]` に理由つきで載る。任意ファイルの取得のジョブが走っている間も、必須が揃っていれば使える）。`local_models` に登録があるだけでは使える扱いにならない。詳しくは [model-manager/model-manager.md](../model-manager/model-manager.md) §5.2。

**成人向けモデルの年齢確認（Issue #841）**: モデルマネージャーの成人向けのモデル（`comfyui:bundled-comfyui/<ワークフロー名>`）は、年齢確認のゲートが閉じている間、一覧（`Models`・`Model`）に出ず、名指しの要求は不明なモデル（`ErrNotFound`）として断る。`gentest.Deps.Hidden`（`modelmanager.Service.HidesModel`）が決める。ゲートはモデルマネージャーの Service が持つ（[../model-manager/model-manager.md](../model-manager/model-manager.md) §6.2）。直接呼び出し（`direct.Image`・`direct.Video`）も、一覧と始まる時の確認をすり抜けたモデルを `agegate.ErrRefused` で断る（何も呼ばない。§6.1 の「導入済みでも、年齢確認がオフの間は使えない」）。

モデルごとに次の能力を返す: 参照画像を使えるか（画像）、動画の長さ・解像度・参照枚数・最初／最後のフレーム・音声、**シード**（FLUX.2・Runway Gen-4.5 と、シードの割り当てがあるローカルのワークフロー）、**ネガティブの欄**（Kling・ネガティブの割り当てがあるワークフロー）、単価。

## 3. 見積り（`Estimate`）

バッチ = 選んだモデル × プロンプト × 1 組あたりの本数（全組み合わせ）。同じ（プロンプト, ネガティブ）は 1 件に数える。

| 状態（`basis`） | 意味 | 合計 |
|---|---|---|
| `priced` | 単価が分かる（画像: 1 枚の USD。動画: 解像度別の 1 秒の USD × `SnapDuration` した長さ） | 入る |
| `free` | ローカルのモデル（#831・#833・#834）。0 USD。「無料（ローカル）」と出す | 0 として入る |
| `unknown` | ChatGPT サブスクと、単価を持たないクラウドモデル | **入れない**（件数・モデルを `unknown_items`・`unknown_models` と注記で知らせる） |

警告（`warnings`）: モデルが使えない入力画像は外して知らせる（`inputs_dropped`。全部が使われないときは止める。§5.1）、長さの丸め（`duration_snapped`）、モデルが作らない解像度・縦横比（失敗する）、単価なし、ネガティブをプロンプトに書き足す（`negative_folded`）、シード非対応（同じ入力を本数ぶん呼ぶ）、最初／最後のフレームと自由な参照の併用、参照が多すぎる。Runway で 1:1 かつ最初のフレームなしは、実行前に警告する（ベンダーが受け付けない組み合わせ）。

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
- **やりたいこと**: `service/gentest/intent`（`intents.json`。埋め込み）が、選択肢（`photo`・`anime`・`landscape`・`face`）ごとに、モデルの候補（カタログのワークフロー名）を好みの順に持つ。`Service.Recommend(intent, hasReferences)` は、導入済みで呼べる（`Info.Available`。成人ゲートが閉じていれば成人向けは一覧に無い）・商用フィルター（`DefaultFilter`）に合う・この PC の OS／GPU の列の外でない（`Deps.PlatformWarning` = `mmview.PlatformWarning`。#979）・入力画像があれば画像を使える（顔の役割が要る選択肢は `face` を読める）最初の候補を選ぶ。選べなければ `Reason`（導入されていない／条件に合わない）。`intentapply.Apply(service, request, intent)`（`service/gentest/intentapply`） は `Models` を埋める（`models` と同時には指定できない。画像だけ。顔の写真が要る選択肢は入力画像が無ければ `validation`。「同じ顔で」は役割 `reference` の入力を `face` にする）。画面の見積りと MCP の `estimate_generation_test`・`run_generation_test` が使う。
- **復元**（結果の「もう1枚」「もう一度」）: `record.ResultDetail`（`TranslateToEnglish` を足した）から、画面の `redo.Of` が入力に戻す（`SpeedMode` を含む。モデルが今そのモードを持たなければ `redo.SpeedGone` のメモを添えて既定で作る。描き直しの結果は `inpaintscreen/again.go` が、同じシードのとき英訳つきなら送った英訳を入れて英訳を切る）。新しいサービスの呼び出しは無い（画面のハンドラだけ）。
- **前のバッチの入力（Issue #977 の残り）**: 入力の出どころに `batch_input`（`gentest.FromBatchInput`。`RefInput.id` は結果の詳細の `inputs[].input_id`）を足した。アップロードした画像はバッチの入力として保存されている（`gentest_input`。`GET /generation-test/inputs/:id/file`）ので、それを別のバッチの入力にできる（`ResolveInput` が `Repo.GetInput` のファイルを返し、その結果の入力は出どころ `batch_input` で残る）。これで、アップロードした画像を使った結果も「この設定でもう一度」で入力ごと戻せる（`redo.RefOf`）。コピーはバッチのディレクトリにあり、その結果をすべて削除すると消える（消えたあとは `validation`「過去の入力画像は残っていません」）。MCP の `references[].source` も同じ値を取る。描き直し・アップスケールの結果の「もう一度」は画面のハンドラだけ（[../../components/gentest-tiers.md](../../components/gentest-tiers.md) §5）。

## 3.7 レシピと「画面に出す値」（Issue #977 ステージ 2）

**成人向けファイルを指す値（統合レビュー）**: `lora_name`・`ckpt_name` のようなファイル名を選ぶ入力を「画面に出す値」にして、リクエストの `exposed` で成人向けのファイルを渡すと、モデル自身のグラフは成人向けでなくても実行時にそのファイルを読む。`modelmanager.Service.HidesModelWith(モデル, 値)`（`gentest.Deps.HiddenWith`）が、値を入れた後のグラフ（`GraphWith`）を `adult.Files.Reads` で判定し、ゲートが閉じている間は `exposedreq.Refuse` が見積り・実行・ComfyUI で開く時に `validation` で断る。キューに積んだ後でゲートが閉じたときは、始まる時に `ReasonHidden` で失敗にする。選択肢は、ゲートが閉じている間、成人向けのファイルを外す（`exposedgate.Choices`・`Deps.HideChoices`。選択肢の値そのものを 1 ノードのグラフとして `adult.Files.Reads` で判定するので、まだ「画面に出す値」に選んでいない入力でも効く）。外す場所は、選ぶ画面（`exposedpick`）と、モデルの一覧が返す `exposed[].options`（画面・MCP の `list_generation_models`）の両方。グラフが今持っている値は残す。

- **レシピ**（`service/gentest/recipe`）: モデル（識別子）と画像の設定（`Params`: steps・cfg・sampler・scheduler・固定のシード・ネガティブ・LoRA と強さ・入力画像の強さ・縦横比・品質・速さのモード `speed_mode`・画面に出す値）に名前を付けて持つ。表は `gentest_recipe`（`repository/reciperepo`。[er-gentest.md](er-gentest.md)）で、`Service.Recipes`（`Deps.Recipes`）が持つ。プロンプトの本文と入力画像は持たない。`Save` は `paramsOf` が、リクエストと同じ検査（`sampling.Validate`・シードの範囲・`refs.CheckLoRA`・`exposedreq.Check`）を通す。速さのモードは、そのモデルが持つ（`Info.HasSpeedMode`）ときだけ保存し、持たなければ `Invalid`（黙って捨てない）。`Apply` は、リクエストが `speed_mode` を名指ししていなければレシピのモードを入れ（モデルが今は持たないモードは落とす）、画面の「使う」は `redo.OfRecipe` からフォームの速さのモードに入れる。`Apply` はレシピをリクエストに入れる（リクエストが名指しした項目が勝つ。`models`・`intent` と同時は 422。使えないレシピは理由つきで 422）。**使えるか**は `status`: `Service.Model`（成人ゲートで隠れた・未導入・削除）→ `Available` → `intent.Excluded`（商用フィルター・OS／GPU。かんたんのやりたいことと同じ関数）。使えなくてもレシピは消さない。
- **成人ゲートが閉じているとき（Issue #992）**: モデルが `Hidden`（`modelmanager.HidesModel`）に当たるレシピは、**保存は残し**、`List`・`EasyChoices`・`Get`・`Rename`・`SetEasy` が返す `View` では、モデルの名前（`ModelLabel` は「成人向けのモデル」）・識別子・設定（`Params` は空）を出さない。`Usable=false`、理由は「成人向けのモデルのため、設定で成人向けを表示にしているときだけ使えます」。画面の一覧・かんたんの選択肢・MCP の `list_generation_recipes` は `View` だけを読むので同じ扱いになる（レシピは「保存して理由つきで残す」設計のまま、名前と設定だけを出さない）。名前の変更とかんたんへの出し入れは、保存された設定をそのまま書き戻し（`load`）、ゲートを開くと元の表示に戻る。
- **画面に出す値**（`localcfg/exposed`・`service/gentest/exposedreq`）: モデルの `Info.Exposed`（`[]exposed.Cap`）がワークフローの `exposed_inputs` とグラフの現在値（`default`）を持つ。リクエストの `Exposed`（キー `<node>.<input>`）は `exposedreq.Check`（選んだモデルのどれかが持つキーだけ。型・範囲・選択肢）で検査し、`exposedreq.Plan` がモデルごとの値（指定した値、無ければグラフの値）を決める。持たないモデルには警告 `exposed_ignored`。値は `ResultParams.Exposed` に残り、実行は `ImageOptions.Exposed` → `comfyui` の `build` が**割り当てより先に**グラフへ入れる（`Workflow.GraphWith`）。
- **今の設定を ComfyUI で開く**（`service/gentest/comfyopen`・`service/workflows/graphui`）: `comfyui.GraphFor`（実行と同じ `build`）が、リクエストの値を入れた API 形式のグラフを返す。`graphui.Convert` が、`GET /object_info` を使って画面の形式にし、`workflows.Service.OpenUI`（`OpenInComfyUI` と同じ置き方）が ComfyUI の `workflows/mam/` に置く。
- **選ぶ側**（`service/gentest/exposedpick`）: グラフのノード・入力の一覧と選べない理由、保存（保存したワークフロー、または接続先のモデルに書く）。
- テスト: `service/gentest/recipetest`（保存・復元・削除・未導入・exposed の範囲と警告・結果）、`exposedpick`・`comfyopen`・`graphui`・`localcfg/exposed`・`comfyui/exposedtest`、`repository/reciperepo`（マイグレーション）。

## 3.5 一部だけ描き直す（inpaint。Issue #974）

**検査（統合レビュー）**: マスクは `image.DecodeConfig` で寸法だけ先に読み、幅 × 高さが `MaxMaskPixels`（8192 × 8192 画素）を超えれば全体を展開せずに `validation`（小さいファイルで巨大な寸法の PNG でメモリを使い切らせない）。白い画素の探索は型ごとの高速経路で、最初の 1 画素で打ち切る。`mask`・`denoise` のどちらも無いのにインペイント専用のモデル（`SupportsInpaint`）を選ぶと、見積りの時点で `validation`（「一部だけ描き直す専用のモデルです。mask を指定してください」。MCP・estimate も同じ）。

元画像 1 枚とマスク（白＝描き直す、黒＝残す）で、マスクの内側だけを描き直す。リクエストの `mask`（参照と同じ `source` / `id` / `project_id`。役割ではない）と `denoise`（0 より大きく 1 以下。省くとモデルの `denoise_default`）。対応は `modelcatalog.Info.SupportsInpaint`（`reference_mode` が `inpaint` で、グラフに範囲内の `denoise` がある）。対応しないモデルを選ぶと見積りの時点で拒否する（警告ではない）。`denoise` を受け取らないモデルに強さを付けると警告 `denoise_ignored` でテンプレートの値のまま作る。マスクが真っ黒（白に近い画素が無い）・動画・マスク無しの `denoise` は拒否する。

グラフはコアノードだけ: 元画像とマスクを `LoadImage`、マスクは `ImageToMask`（`channel=red`）、`VAEEncode` → `SetLatentNoiseMask` → `KSampler` → `VAEDecode`、その結果を `ImageCompositeMasked` で元画像の上に戻す（`destination` が元画像、`source` がデコード、`mask` が `ImageToMask`）。外側の画素が元のままであることの確認は、このリンクのテストまで。画素の比較は GPU が要る（未実施）。テンプレートは `inpaint-sdxl` と `inpaint-flux-dev`（既定の denoise は 0.75）。既存のテンプレートのグラフは変えない。

**画面側のメモリ（Issue #999）**: 描き直しの画面のマスク部品（`<mam-mask-paint>`）は、マスクと赤い表示の 2 枚のキャンバスを持つ（4 バイト × 画素）。8192 × 8192 の画像では 256 MiB × 2 になるので、**長辺が 2048 を超える画像は、形を保って長辺 2048 に縮めたキャンバスに塗る**（`static/src/components/mask-paint/mask-size.ts` の `canvasSize`。2048² なら 16 MiB × 2。iOS の Safari のキャンバスの上限（約 1600 万画素）の内にも入る）。書き出すマスクもその大きさになる。サーバーは寸法の下限を見ず、ComfyUI の `SetLatentNoiseMask` と `ImageCompositeMasked` はマスクを画像の大きさに（双一次で）引き伸ばして使う（コアノードの実装による。**この引き伸ばしでの外側の画素の一致は GPU・実ブラウザでの確認が要る = 未実施**）。元画像の `<img>` はブラウザがデコードするので、その分のメモリは部品の外で、実機での測定（Chrome・Safari の巨大画像でのタブのメモリ）は未実施。

結果は別ファイル。`params.inpaint`（使った `denoise`、元画像の `source_kind` / `source_id`、マスク入力の id）に残し、元が履歴の結果なら詳細からその結果へ戻れる。マスクはバッチの入力 `role=mask` で、参照としては送らない。作成フォームではインペイント専用のモデルを選べない（「一部だけ描き直す画面から使います」）。画面は `/generation-test/inpaint`。導入済みの SDXL / FLUX.1 dev は、起動時の更新が新しいワークフロー名を足さないので、インペイント用のワークフローは入れ直すまで一覧に出ない。

## 3.4 大きくする（アップスケール。Issue #975）

成功した画像の結果を **2 倍・4 倍**にする。結果の詳細の「大きくする」（倍率と方式を選んで 1 操作）と、MCP の `upscale_generation_result`（結果 ID＋倍率）が同じ `upscale.Run`（`service/gentest/upscale`）を呼ぶ。**見積り・確認は無い**（アップスケールのワークフローはローカルの ComfyUI のモデルで、無料。`basis=free`）。

- **別の結果として残る**: 元の結果のファイルを入力として新しいバッチ（結果 1 件・入力 1 枚。origin は `history`）に複製し、ジョブ（種別 `gentest`）で動かす。元の結果は変わらず残り、大きくした結果は履歴の別の行・別のファイル。**元へのリンクと使った値**は結果の `params.upscale`（`record.Upscale`: `source_result_id`・`source_model`・`scale`・`model`・`mode`・`native`・元と出力の大きさ）に残し、`ResultView.upscale`・結果の詳細の「拡大」「大きさ」「元の結果」の行と元の結果へのリンク・MCP の `get_generation_result` に出る。元を後で消してもこの結果は残る（リンクだけが切れる）。大きくした結果をもう一度大きくしてもよい。
- **ワークフロー種別として扱う**: アップスケールは、**画像モデルの選択とは別の「ワークフロー種別」**にした。ComfyUI のワークフロー（`localcfg.Workflow`）の `upscale`（`localcfg/upscale.Spec` = `{label, native, mode, max_output_pixels}`）で印を付け、同じエンジン（同梱の `bundled-comfyui`）の画像ワークフローとして作る。印のあるワークフローは**プロンプトを渡すモデルの一覧に出さない**（`localcfg.IDs`・生成テストの `modelcatalog.Models`・モデルマネージャーの試し生成から除く）。「大きくする」は `modelcatalog.Catalog.Upscalers` が出す専用の一覧から選ぶ。既存の画像モデルの選択 UI・見積り・確認の流れには触れない（最も小さい変更）。
- **方式**（`upscale.mode`）: `model` = `LoadImage` → `UpscaleModelLoader` → `ImageUpscaleWithModel`（内部でタイルに分けて処理する。タイルは 512 から始め、メモリが足りなければ半分にする）→ `ImageScaleBy`（lanczos）→ `SaveImage`。`two_pass` = 同じ拡大のあと、`VAEEncodeTiled` → 低い denoise（0.3）の `KSampler`（SDXL base）→ `VAEDecodeTiled`（どちらもタイル 512・重なり 64）→ `SaveImage`。ComfyUI 本体のノードだけ。
- **倍率**: モデルの倍率（`upscale.native`。今のカタログは 4 倍）と依頼の倍率の比 `scale / native` を、`ImageScaleBy` の `scale_by` に入れる（値の割り当て `upscale_ratio`。4 倍の依頼はそのまま、2 倍は半分に縮める）。倍率は `UpscaleScales`（2・4）以外なら `validation`。
- **2 パス仕上げ**: 元の結果の**プロンプト・ネガティブプロンプト・送った文・シード**を引き継ぐ（元が英訳されていたら英訳の記録も）。モデルは SDXL base の固定（元の結果のモデルには依存しない。元が別のモデルでも、プロンプトは文として SDXL に渡す）。CLIP のウェイト構文は効く（`prompt_weight: supported`）。
- **上限**: 出力の画素数（幅 × 高さ）が `upscale.MaxOutputPixels`（64 Mpx = 8192 × 8192）を超える、またはワークフローの `max_output_pixels`（2 パスは 16 Mpx = 4096 × 4096）を超えるなら、何も始めずに `validation`（元の画像の大きさは、保存した画像の先頭から読む）。理由は、PNG が数百 MB になってエンジンの応答の上限（300 MiB）に近づくことと、2 パスは画像全体をサンプリングするため。**mam が決めた安全側の値で、実測の値ではない**。
- **入っているモデルだけ**: モデルマネージャーでアップスケール用のモデルを入れる（カタログの `kind: upscale`。[../model-manager/model-manager.md](../model-manager/model-manager.md) §4.2「アップスケール用モデル」）と、そのワークフローが同梱エンジンに作られる。無ければ「大きくする」はモデルマネージャーへの案内を出し、`Upscale` は `validation`（「アップスケール用のモデルが入っていません」）。`model` を省くと、入っているもののうちプロンプトの要らない方式（`mode=model`）の最初。名指しのモデルが入っていない・使えない（年齢確認・サーキットブレーカー）なら `validation`。
- **16GB の GPU（未測定）**: ComfyUI のタイル処理（`ImageUpscaleWithModel`・`VAEEncodeTiled`・`VAEDecodeTiled`）で、全体を一度に GPU に載せない作りにした。**16GB の GPU で 4 倍が落ちないことは、実機で測っていない（未測定。実機で確認が要る）**。VRAM の目安（カタログの `vram`）も mam の見積もりで、`estimate: true`。
- テストは `service/gentest/localtest/upscale_test.go`（ComfyUI の偽のサーバーを通して、別ファイルの結果・元の記録・倍率の比・2 パスのプロンプトとシード・拒否・上限・モデルの一覧から外れること）、`web/handler/gentest/upscale_test.go`（画面）、`mcp/tools/gentest/upscale_test.go`（MCP）。

## 3.8 顔の入れ替え（外部ツール FaceFusion。Issue #995）と顔の類似度（Issue #998）

結果の詳細から、成功した画像の顔を別の画像の顔に入れ替える。入れ替えは mam に同梱しない外部ツール **FaceFusion**（ffmpeg と同じ扱い）を別プロセスで呼ぶ（`facefusion.py headless-run`）。アップスケールと同じく、**独立したバッチ（結果 1 件・入力 2 枚）**として実行し、結果は履歴とジョブ一覧に通常の結果と同じに並ぶ（モデル識別子は `facefusion`）。非商用・研究用・商用不明のモデルは警告つきで選べ、成人向けの画像には使えない旨を画面に出す。設定（Python とフォルダ）・実行の引数・モデルのライセンス表（FaceFusion の公式一覧が一次情報）・画面・MCP は [face-swap.md](face-swap.md)。

役割 `face` の入力があった結果には、**参照の顔と出力の顔の類似度（SFace のコサイン類似度）**を結果ごとに自動で測って `params.face_similarity` に残す（測れない理由も残し、結果は成功のまま。顔のモデル・ONNX Runtime が無くて測らないときも `skipped: models_missing` / `runtime_unavailable` と理由・直し方を残し、`list_generation_models` の `face_similarity` と見積りの警告 `face_similarity_off` でも読める。モデルはモデルマネージャーの「顔チェック用モデル」からボタン 1 つで導入でき、導入・削除は再起動なしで次の結果から効く。入力画像の欄は、オフの間「顔チェック用モデルを入れる」のリンクを出す。Issue #1026）。計測の定義・目安の閾値・モデルの配布・ライセンスの一次情報・#960 の監査は [../local-models.md](../local-models.md) §4.9。

## 3.9 動画のアップスケールとコマ補間（Issue #1031）

成功した**動画**の結果を、大きくする（アップスケール）か、コマを増やして滑らかにする（フレーム補間）。画像の「大きくする」（§3.4）と同じく**独立した処理**で、元とは別の結果（別のバッチ・別のファイル）を **1 つ**作り、結果の詳細・MCP（`get_generation_result` の `video_enhance`）に元の `result_id`・操作・モデル・提供元・倍率や fps・元と出力の大きさが残る。アップスケールとコマ補間は**別々の処理**で、続けるときは、できた結果にもう一度かける（処理した結果をさらに処理してよい）。MCP は動画専用の 3 ツール（`list_generation_video_enhancers`・`estimate_generation_video_enhance`・`enhance_generation_video`。[../../api/mcp-tools-generation-test.md](../../api/mcp-tools-generation-test.md)）。画面（結果の詳細のボタン・Topaz の枠）は後続。

- **実装の置き場**: 契約（`Op`・`Source`・`Params`・`Cloud` の口など）は `agent/cloudenhance`、値の検査・見積りの置き場は `service/gentest/venhance`、サービスは `service/gentest/videoenhance`（`gentest.Service` の行数の上限のため別パッケージ。`gentest.Service` の `Deps` の `Cloud`・`Probe`・`VideoEstimates` を使う）。実行は `runner/venhance.go`（`execute` が `params.video_enhance` のある結果で呼ぶ）。
- **元の動画の事実**: 大きさ・fps・コマ数（ffprobe の `nb_frames`、無ければ fps × 長さ）・長さ（Topaz へ送るのは映像のストリームの長さ `VideoDuration`。無ければ形式全体の長さ）・音声の有無・ファイルの大きさは、**保存したファイルを ffprobe で読んだ値**（`media.Tools.Probe`。生成のときの指定ではない）。`Deps.Probe`（`venhance.Prober`）は `media.Tools`（`app` が ffmpeg の設定から作る。無ければ nil）。**ffmpeg / ffprobe が無いと処理できず**、共通エラー `ffmpeg_missing`（設定の「ffmpeg」で入れる・指定する、の直し方つき）を返す。
- **値**（`venhance.Resolve`）: アップスケールは `scale`（1 より大きく 4 以下。標準は 2）か `target_short_side`（720・1080 など。短い辺をその長さにし、長い辺は縦横比のまま。480×848 → 720 なら 720×1272）のどちらか一方。出力の幅・高さは、**ローカルは ComfyUI と同じ順で求める**（`round(辺 × 倍率)`〔Python の `round` と同じく 0.5 は偶数へ〕のあと、`SeedVR2PostProcessing` が奇数の辺を 1 引いて偶数にする。例: 1281×721 を 1.5 倍 → `round(1921.5)` = 1922・`round(1081.5)` = 1082）、**Topaz は 4 の倍数に切り上げる**（公式の「Rounded up to a multiple of 4」。`round(辺 × 倍率)` を 4 の倍数へ切り上げる）。SeedVR2 のノードは入力を 16 の倍数にそろえて処理し、後処理で元の大きさに合わせて切るので、8 の倍数への丸めは要らない。出力の 1 コマの画素数は 3840×2160 以下（モデルが `max_output_pixels` を持つときはそれ。SeedVR2 3B は 1920×1080）。補間は `multiplier`（2・4）か `target_fps` のどちらか一方で、**ローカル（RIFE）は元の fps の整数倍（2 倍・4 倍。誤差 0.01 fps まで）だけ**、それ以外の `target_fps` は断って、2 倍・4 倍か Topaz を案内する。**ローカルは出力 120 fps まで**（ComfyUI v0.42.1 の `CreateVideo` の `fps` が最大 120 で、それを超える値は ComfyUI が受け付けない。元の fps × 倍率が 120 を超えるときは `Resolve` が日本語で断り、「倍率を下げるか、Topaz（240 fps まで）を使ってください」と案内する。`venhance.MaxLocalFPS`）。Topaz は 15〜240 fps の任意の値（`slowmo` は使わず、目標の fps へ時間を合わせるだけ）。
- **ローカル**: ComfyUI のワークフロー（`localcfg.Workflow.VideoEnhance` の印。テンプレート `video-upscale-seedvr2-3b`・`video-interpolate-rife-v4.25`・`…-v4.26`）を `enhancevideo.Run`（`modeladapter/enhancevideo`） で呼ぶ。無料で確認なしにすぐ始まる（`CostBasis` は `free`）。入力画像は持たず、実行のとき元の結果のファイルを読んで ComfyUI へ上げる。モデルの選択・モデルの一覧には出ない（プロンプトを渡すモデルではない）。SeedVR2 / RIFE の RTX 4070 Ti SUPER 16GB での動作は**実機未確認**。
- **Topaz（有料クラウド）**: `Deps.Cloud`（`cloudenhance.Cloud`。実装は `internal/agent/topaz`）。モデルは識別子 `topaz:<Topaz のモデル名>` で一覧に並ぶ。API キー（設定の「API キー」）が無いと `available=false`・`unavailable=no_key`・#1013 の文言で、画面のモデル選択にも出さない。**見積り → 確認 → 実行**: `EstimateEnhance` が Topaz に見積りを聞き（課金されない）、不透明な `estimate_id` を返す。中身（Topaz のリクエスト ID・クレジット・値）は**メモリに 30 分だけ**持つ（`venhance.Estimates`。再起動で消える）。`Enhance` は `estimate_id` と `confirmed_credits`（= 見積りの `credits`。NaN・無限大・負は通さない。`!(差 <= 許容)` の形で書き、NaN との比較が常に偽になっても関門を抜けないようにする。画面の入力も NaN・Inf は読み取れない値として断る）が無いと送らない（`estimate_id` なし → `validation`、期限切れ・使用済み・未知 → `conflict`「見積りの有効期限が切れました。もう一度見積りを取ってください」、クレジットが違う・見積りのときと元・モデル・値が違う → `conflict`）。見積りは `Take` で**1 回だけ**使える（同時に 2 回呼ばれても片方だけ。クレジットの二重消費を防ぐ）。見積りの金額が取れなければ（0 や負も含む）見積りが失敗し、実行できない。Topaz の結果に残す出力の大きさ・fps（`video_enhance.width/height/fps`）は、見積りが返した値（4 の倍数）で、ローカルの予定ではない。
- **Topaz の実行**（`runner.enhanceInCloud`）: 元のファイルを**パスで**渡し（`RunInput.SourcePath`。メモリに全部は読まない）、結果は `<バッチ>/<結果>.mp4.part` に書いて、できてから `<結果>.mp4` に移す。進み具合は `live` のジョブ（`get_job`・`get_generation_test` の `progress`）に出る。**受け付けたあとの失敗は自動で再送しない**（ジョブは再開しない種別。見積りは使い切り）。失敗は既存の失敗の対応（`runner.failure`。Topaz のエラーは `apperr` / `errmap`）で日本語の原因と直し方になる。受け付けたあとに呼び出しが止められた（mam の停止・取り消し）ときは汎用の「mam が停止したため中断されました」にせず、`topaz_job_interrupted`（Topaz 側で続いている可能性・クレジットが引かれる可能性・取り消しは未実装・Topaz のアカウントでリクエスト ID `ve.Topaz.RequestID` を引く、を日本語で）にする。実行の前の失敗（クラウドが無い・元の結果の削除・元のファイルが読めない・見積りが残っていない）も、原因と直し方つきの共通エラー（`not_found`・`unavailable`・`invalid_input` に固定の日本語の原因）にして、結果の失敗の文を「原因がわかりませんでした」にしない。実際の費用は不明（`actual_cost` なし）。
- **費用の持ち方**: クレジットは金額ではないので USD に換算しない。結果の `cost_basis` は、ローカルが `free`、Topaz が `unknown`（見積りの `estimated_cost_usd` なし）。見積りのクレジットは `video_enhance.topaz.estimated_credits`、リクエスト ID は `request_id` に残る。
- **表示の義務**: Topaz を出す・使う場所に「Powered by Topaz Labs」（`attribution`）を出す（一覧・見積り・結果の `video_enhance.topaz`）。出力の商用利用の可否は公式ドキュメント（FAQ）に書かれていないので、ライセンスは `commercial: unknown`（出典 <https://developer.topazlabs.com/getting-started/faqs>、確認日 2026-11-04。統合は可で出典の表示が要る、と書いてある）。
- **成人向けの結果を Topaz に送るとき（2026-11-05 決定）**: 成人向け（モデルマネージャーのカタログの `adult`。成人向けの結果を処理した結果も成人向けとして引き継ぐ。`video_enhance.adult`）の動画も Topaz に送れる（送信は止めない）。そのかわり、見積り→確認の関門で警告 `venhance.AdultWarning`（成人向けの内容が外部のクラウド〔Topaz Labs〕に送られる・Topaz の利用規約で禁止されている内容は送らない・処理した動画の扱いは Topaz の規約に従う）を出し、読んで確認した（`Request.AdultConfirmed`）ときだけ送る。確認がなければ `validation` で警告を返し、見積りは使い切らない（確認し直せる）。成人向けでない結果・ローカルのモデルには出ない。テストは `videoenhance/adult_test.go`。
- **見積りの JSON の形**は Topaz の公式に未掲載のため、`internal/agent/topaz` が寛容に読む。形が分からなければ見積りは失敗する（実機未確認）。
- テストは `service/gentest/videoenhance`（偽の ffprobe・偽の ComfyUI のモデル・偽の `Cloud`: ローカルの成功と記録・短辺指定・連続処理・補間の整数倍・動画でない結果・ffmpeg が無いとき・値の検査・画素数の上限・見積り → 確認の関門〔無し／違う額／違う条件／未知の ID／使用済み〕・キー無しでは見積りも実行もしない・見積りが取れなければ実行しない・失敗した Topaz の実行は再送しない）、`service/gentest/venhance`（`Resolve`・`Estimates` の期限と使い切り）、`mcp/tools/gentestvideo`（3 ツールの一連）、`media`（`FrameCount`）。

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

## 5.1 入力画像の検査（preflight。Issue #1009）

方針: **モデルが受け付けない入力は、黙って捨てない。送る前に止めるか、はっきり警告する。** 検査は `service/gentest/inputs.go` の 1 組の関数で、`estimate`（`Estimate` と `Run` の両方が通る）が走らせる。画面の見積り、`estimate_generation_test`、`run_generation_test` は同じ `Service.Estimate` / `Run` を呼ぶので、**同じ検査・同じ文**になる（MCP と画面で別のチェックは持たない）。画面の入力欄の通知（動画）は同じ関数を `Service.CheckInputs(Request)` で、画像の中身を見ずに（フォーム入力中の、プロンプトやアップロードが未確定の状態で）呼ぶ。

| 状態 | 扱い | コード（`apperr`） |
|---|---|---|
| **モデルが必ず使う入力がリクエストに無い**（`modelcatalog.Info.requires_inputs`: I2V のワークフローは `first_frame`。ワークフローの割り当てが `optional` でないもの。`localcfg.Workflow.RequiredImages`）。画像が 1 枚も無い／添えた画像の役割が違う（`reference` のまま） | **止める**（送らない。見積り・実行とも 422 / MCP `validation`） | `input_missing`（`param`: `first_frame` など）。原因は「「<モデル>」は最初のフレームの画像が必要です。添えた画像が「参照」になっています。「最初のフレーム」に変えてください。」（画像が無いときは「画像を添えて、役割を「最初のフレーム」にしてください。」）、直し方は `input_missing.remedy` |
| **添えた画像が、選んだどのモデルにも 1 枚も使われない**（T2V に `first_frame`、参照画像 0 枚のモデル・自由参照 0 枚のモデルに参照画像、など） | **止める** | `input_unused`。原因は T2V なら「「<モデル>」は文章だけから作るモデルなので、添えた画像は使われません。」、直し方は「画像から作るなら、I2V（画像から動画）や参照画像に対応したモデルを選んでください。画像を使わずに作るなら、画像を外してから実行してください。」 |
| **一部の画像だけ、あるモデルに使われない**（最後のフレームを使えないモデルに first + last、枚数超過、フレームと自由参照の併用、あるモデルだけ参照を使えない比較バッチ） | **警告のまま実行を許す**（`warnings` の `inputs_dropped` / `too_many_refs` / `frames_over_refs`。画面は確認の上に琥珀色の枠「使われない入力画像があります」、動画の入力欄の通知にも出す）。結果にそのモデルの**使われなかった入力**を記録する | — |

境界の理由: 添えた画像が 1 枚も使われないバッチは、ほぼ確実に選び間違い（モデルかの役割）で、成功扱いの結果を作ると「画像が効いていない」と誤解される（#1009 の追記）ので止める。複数モデルの比較や、フレーム 2 枚のうち 1 枚だけ使えないモデルは、意図した使い方がありうるので警告に留め、結果ごとに何が使われなかったかを残す。画像モデルは、選んだモデルのうち 1 つでも使えば実行を許す（使えないモデルは `inputs_dropped` の警告と結果の記録）。

**自動で役割を書き換えない**。`reference` 1 枚だけ `first_frame` しか受けないモデルへ回す処理は無い（止めて「「最初のフレーム」に変えてください」と言う）。変えてよいのは**初期値だけ**で、I2V（`requires_inputs` に `first_frame` があるモデル）を選んでいるあいだ、画面で行・ファイルを足したときの役割の初期値を `first_frame` にする（`Service.DefaultInputRole`。最初の 1 枚だけ。役割の select は見えており、利用者が選んだ役割は動かさない）。

**使われなかった入力の記録**: `gentest_result.params`（JSON）の `unused_inputs[]`（`input_id`・`role`・`reason`）。列は足していない（マイグレーションなし）。`record.ResultView.unused_inputs` として `get_generation_test` / `get_generation_result` / `list_generation_results` に出る。結果の詳細に「使われなかった画像」の枠（画像・役割・理由）、結果のタイルに「画像 n 枚が未使用」のマーク。`used_inputs` は従来どおり渡した入力の ID。

**入力に合わせたモデルの並べ替え**: `modelfilter.InputsOfRoles` / `modelfilter.PreferForInputs`（[../model-manager/model-filter.md](../model-manager/model-filter.md) §4。画像を添えたとき、使えるモデルを先に並べる安定ソート）。バックエンドの関数だけで、選択 UI は #1011 の画面が使う。

## 6. 入力画像

入力は、アップロード（画面は見積り時に、MCP は `gentest_reference` で `_staging/` に置く）、プロジェクトのアセット・Reference Library の項目（`project_asset` / `reference_item`）、履歴の成功した画像結果（`history`）。選んだ画像は**バッチの `gentest_input` に複製**する（`origin` に元を記録）ので、元のプロジェクトや結果を後で消しても履歴は読める。画像は PNG・JPEG・WebP を中身で検査し、1 枚 20 MiB まで・1 バッチ 10 枚まで。

動画の役割は `reference`（自由な参照）/ `first_frame` / `last_frame`。モデルの能力（`Capabilities`）で、使えない役割は**そのモデルからだけ外して警告**する。ただし必須の入力が無い、または添えた画像が選んだどのモデルにも使われないときは、外さずに**送る前に止める**（§5.1）。

**速さのモード（Issue #930・#991）**: リクエストの `speed_mode`（動画も画像も）（`fast` / `standard` など。省くと各モデルの既定）は、ComfyUI のワークフローが `speed_modes`（[local-models.md](../local-models.md) §3）を持つモデルだけに効く（動画の Wan 2.2 14B + LightX2V: `fast` = 4 ステップ・cfg 1、`standard` = LoRA なしの通常 20 ステップ・cfg 3.5。画像の Qwen-Image-Edit 2511 + Lightning: `standard` = 20 ステップ・cfg 4、`lightning-8` = 8 ステップ・cfg 1、`lightning-4` = 4 ステップ・cfg 1）。モードは `modelcatalog.Info.speed_modes`（`id`・`label`・`default`。#991 で動画の `video.speed_modes` から画像と共通のモデル直下へ移した）にあり、**steps・cfg はモードが決める**（#973 の決定。`sampling.steps`・`sampling.cfg` の `locked` が `speed_mode`。リクエストの steps・cfg・かんたんの品質は警告 `sampling_ignored` で使わず、画面は詳細設定のステップ数・CFG の欄を出さずに「速さのモードが決めるため、指定は使いません」と理由を出し、かんたんの品質の欄は「このモデルは速さのモードがステップ数・CFG を決めるため、品質は選べません」と言う）。画面は動画・画像どちらのフォームにも「速さ」の選択（`data-testid="gt-speed-mode"`。どれかのモデルが持つときだけ描く。選択肢は「モデルの既定」とモードのラベル）を、MCP は `list_generation_models` の `speed_modes` と `estimate_generation_test`・`run_generation_test` の `speed_mode` を出す。そのモードを持たないモデルは **警告 `speed_mode_unsupported`（拒否ではない）で既定のまま**作り（モードを持つが、そのモードが使う任意ファイル〔Lightning LoRA〕が導入されていないモデルも同じ。警告に理由が入り、`blocked_speed_modes[]` に載る。Issue #1025）、名前が `^[a-z0-9][a-z0-9-]{0,39}$` でなければ 400（`invalid`）。画像のフォームの選択肢は画像のモデルのモードだけ（動画のモードは出ない）。選んだモードは見積りの確認（hidden の `speed_mode`）を通って結果の `params.speed_mode` に残り、`get_generation_test` の結果の `speed_mode` に出る。見積りの ID（`estimate_id`）はリクエスト全体の要約なので、モードを変えると別の見積りになる。

**画像モデルの参照（Issue #933）**: 画像の入力は、役割 `reference`（全体。既定）/ `face`（顔だけ）/ `style`（画風・雰囲気だけ）/ `edit`（編集する元の画像）と、強さ `strength`（0〜1。省くと `localcfg.DefaultStrengthOf(role)`: 既定は `DefaultReferenceStrength` = 0.7、役割 `face` は `DefaultFaceReferenceStrength` = 0.9。[local-models.md](../local-models.md) §4.5）を持つ。強さの実現方法（img2img の denoise、編集モデルへの指示の強さ、アダプタの重み）はモデルごとに違い、**画面と MCP は同じ「強さ」の 1 つの値だけを出す**。範囲外の強さ・存在しない役割・動画への強さ・LoRA・ポーズ設定は入力の誤り。リクエスト全体に、`avoid_reference_pose`（参照を使うモデルのネガティブプロンプトに `localcfg.AvoidPoseNegative` を足す。ネガティブの欄が無いモデルは既存の畳み込み（`Avoid: …`、警告 `negative_folded`）。参照を使わないモデルは何もしない）と `lora`（名前・強さ 0〜1、既定 `localcfg.DefaultLoRAStrength` = 0.4。画像だけ）がある。名前は 1〜200 文字で制御文字を含まない（サブフォルダの `/` と `.` は可）。強さを実現できるのは同梱 ComfyUI の参照対応モデルだけで、クラウドの画像モデルなどでは無視される（警告は出さない）。**強さの保存値は入口で違う**: 画面のフォームは常に明示の強さを送る（省いた欄は 0.7 として保存）が、MCP は省くと保存しない（`gentest_input.strength` は NULL で、モデルが既定を使う）。

モデルごとの扱いは `modelcatalog.Info` の `reference_mode`（`edit`＝編集モデル: 複数枚・役割あり / `img2img`＝1 枚から始める）・`max_reference_images`・`reference_roles`・`supports_lora`（どれもワークフローの割り当て・`reference_mode`・`lora_model_node` から `modelcatalog.ReferenceCapsOf`（`localcfg.Model.ReferenceCaps`）と `modelcatalog.SupportsLoRA` が決める。クラウドの画像モデルは参照を使えても枚数・役割は不明で、役割は `reference` だけ）で決まる。**対応しない分は拒否せず、そのモデルからだけ外して警告する**: 参照を使えないモデル `inputs_dropped`、枚数超過 `too_many_refs`（先頭から `max_reference_images` 枚。img2img のモデルは**先頭の 1 枚だけ**を使い、残りは `too_many_refs` の警告になる）、そのモデルが取らない役割 `role_unsupported`（画像は全体の参照として使う）、LoRA に対応しないモデル `lora_unsupported`（LoRA は送らない）。結果の行には各入力の `role`・`strength`（`gentest_input`）と、使った `avoid_pose`・`lora`（`params`）を残し、`direct.Image` へは `modeladapter.Input.Role` / `Strength` と `ImageRequest.LoRA` として渡す（強さ 0 は「既定」と区別するため、参照も LoRA も `refs.MinStrength` = 0.01 にして送る。履歴には選んだ 0 を残す）。LoRA の名前の一覧は同梱 ComfyUI エンジンの `GET /object_info/LoraLoaderModelOnly`（`lora_name` の選択肢）から `Service.LoRAs(ctx, media)` が数秒の間だけ覚えて返す（エンジンが止まっていれば空。失敗も同じ間覚え、取得中の呼び出しは 1 回の取得を共有するので、応答しないエンジンでも待つのは 1 回だけ。動画では LoRA を使えないのでエンジンに問い合わせない）。 警告 `role_unsupported` には、そのモデルが使わない（全体の参照に落とす）参照の役割 `roles`（`face` / `style` / `edit`。実際に使う参照だけ・重複なし・参照の並び順）が付く（`gentest.Warning.Roles`、`refs.Warning.Roles`）。画面は、これを源にして実行の前に「◯◯は「顔だけ」に対応していないので、全体の参照として使います。構図や服も寄ります」というモーダルをモデルごとに出す（Issue #959。[../../components/gentest-screen.md](../../components/gentest-screen.md)）。警告と全体への置き換え（`refs.RoleFor`）は変えない

**「顔だけ」の参照の切り出し（Issue #960）**: バッチを走らせるとき（`runner.RunJob`）、役割 `face` の入力画像を 1 回だけ顔のまわりに切る（`Deps.FaceCrop`。アプリは `facesim.CropFace`、顔検出モデルが無いと nil で何もしない）。結果ごとの呼び出しで、**役割 `face` を取るモデルへだけ**切った画像を渡し（`refs.InputOf` が決めた役割が `face` のとき）、全体の参照に落ちるモデルと、他の役割には元の画像を渡す。顔が無い・小さい・アップの画像と検出の失敗は元の画像のまま（失敗はログだけ）。履歴に残る入力は元の画像。詳細と効果の測り方は [local-models.md](../local-models.md) §4.5。

**ControlNet・IP-Adapter（Issue #976）**: `reference_mode` が `controlnet`（参照画像は制御画像。コアのノード）・`ipadapter`（IP-Adapter の参照画像。追加ノード `ipadapter-plus`）のモデルは、参照画像を**ちょうど 1 枚**要求し（`max_reference_images` 1、役割は `reference`）、参照の強さ（0〜1、既定 0.7）がノードの強さ（`ControlNetApplyAdvanced.strength`・`IPAdapterAdvanced.weight`）に入る（割り当て `reference_strength`）。画像が無い実行は、制御画像／IP-Adapter の参照画像が要ると言って拒否する。**制御画像は mam が作る（Issue #993）**: `controlnet` のテンプレートに `reference_preprocess: "canny"` があると、ComfyUI に画像を送る前に、参照画像（PNG・JPEG・WebP）から Go で Canny のエッジ画像（黒地に白い線）を作って差し替える（`backend/internal/imgprep`。すでにエッジ画像ならそのまま。画像として読めなければ 422）。写真をそのまま参照に渡せる。ポーズ・深度などの重みが要る前処理は行わない（[../model-manager/custom-nodes.md](../model-manager/custom-nodes.md) §5.2）。**ワークフローが追加ノードを要る（`requires_nodes`）同梱エンジンのモデルは、そのノードが導入・読み込み済みになるまで一覧で `available=false`・`unavailable=missing_node`（理由に、ノードの名前と導入する場所）になり、バッチに入れられない**（`modelcatalog.Catalog.MissingNodes`。`app/gentest.go` が `nodegate.ForModels` を渡す。外部の ComfyUI のモデルは対象外）。詳細は [../model-manager/custom-nodes.md](../model-manager/custom-nodes.md) §6。

**ポーズの参照（Issue #985・#986）**: 画像の参照の役割に `pose`（ポーズだけ。画像のみ・動画には使えない）を足した。参照画像の体のポーズとカメラの角度だけを写し、絵柄・色・顔・髪・服・背景は写さない役割で、`edit` 方式のモデル（Qwen-Image 2.1 の `txt2img-qwen-image21`〔TextEncodeQwenImage21〕・Qwen-Image-Edit-2511〔EditPlus〕）が持つ（`localcfg` の `ModeEdit` の役割）。エンコーダーが画像に付ける番号を指す 1 文（「`<imageN>` はポーズの参照としてだけ使う: …」。`comfyui/stage.roleSentence`）をプロンプトに足し、ポーズの画像は `face`・`style` と同じく視覚の入力にだけ渡し、VAE を渡さない段では VAE の潜在（latent）を作らない（絵に写させないため。`stage.seenOnly`）。ただし**「全体」「編集対象」の画像が 1 枚でも同じ段にあると、エンコーダー全体に VAE を渡すため、顔・画風・ポーズの画像の画素も参照の潜在として入ることがある**（`TextEncodeQwenImage21`・`TextEncodeQwenImageEditPlus` は `vae` があると全ての画像を潜在にする。ComfyUI v0.42.1 の `nodes_qwen.py` を読んだ範囲。この混在の段の実際の挙動は GPU で確かめていない: `needs_real_device`）。役割を持たないモデルは、ほかの役割と同じく**全体の参照として使い、警告 `role_unsupported`（「ポーズ」と名指し）**を出す。「参照画像のポーズ・構図を真似させない」（`avoid_reference_pose`）は、`pose` の参照を持つ画像モデルでは、その参照を除いて数える（ポーズを写すのが目的の参照のため）。

入力の出どころに `pose_library`（ポーズ集。`RefInput.source`、`ref` の値は `pose_library::<画像 ID>`、画像 ID は `std_P001_front` の形。深度・法線マップは後ろに `.depth`・`.normal` を付けた `std_P001_front.depth` の形〔Issue #1020〕）を足した。ポーズ集は **Shisei**（`wquguru/shisei`。マネキンの線画と、ControlNet 用の深度・法線マップ。コードは Apache-2.0、画像は CC0 1.0）を、利用者が同意して導入したときだけ取得して使う（[model-manager/local-runtime.md](../model-manager/local-runtime.md) §12.9。mam は同梱しない）。`service/gentest/poses`（`Library`）が導入・一覧・絞り込み（文字・分類・アングル）・ファイルの解決を持ち、全年齢向け（`rating: general`）で線画のあるポーズだけを出す。未導入・ポーズ集が無い・ポーズ集にない画像は、入力の誤りとして理由と手順を日本語で拒否する。選んだ画像は他の出どころと同じく**バッチへ複製**し（`origin` は `{kind: "pose_library", id}`）、ポーズ集を消しても履歴の入力は読める。ポーズ集は画面（一覧の下の削除ボタン・導入中の進み具合）と MCP（`get_pose_library`・`install_pose_library`・`remove_pose_library`。実装は `mcp/tools/gentestposes`）から導入・状態・削除できる（Issue #994）。画面は入力画像の区画の「ポーズを選ぶ」から、導入の案内（出典・ライセンス・同意）か絞り込み付きの一覧を出す（[../../components/gentest-screen.md](../../components/gentest-screen.md)）。**ポーズの画面にも、選んでいるモデルの商用利用の可否（モデルカードと同じバッジと注意書き）を出す**（Qwen-Image 2.1・Noct Q は Qwen RESEARCH LICENSE で非商用）。 **成人向けのシリーズ（`sexy`・`duo`。Issue #1020）**は別の資産セットで、モデル管理の年齢確認（[model-manager/model-manager.md](../model-manager/model-manager.md) §6.2）がオンの間だけ、導入できて、一覧・画像・`pose_library` の参照・履歴に出る。**オフの間は、ポーズの名前・画像が画面・MCP・ジョブ一覧・履歴のどこにも出ない**（`poses.Library` が `AdultOpen` を呼ぶたびに聞く。履歴は `history.Store.InputHidden` が、成人向けのポーズから取った入力〔`batch_input` の写しも〕を詳細・ファイル・再利用から外し、走り出す前に閉じたバッチは `runner.ErrInputHidden` で失敗にする。詳細は [model-manager/local-runtime.md](../model-manager/local-runtime.md) §12.9）。

**成人向けのポーズのゲートの抜け止め（Issue #1020。統合の研磨で足した）**: ①**見積り・バッチ状態の `Request` からも除く**: バッチは確認した見積り（`Request` ごと。`references[]` の `{source: pose_library, id}`・`mask`）を保存し、`BatchStatus.Estimate`（`Service.Batch`・`BatchOfJob`。MCP `get_generation_test` もここを通る）がそのまま返していたので、ゲートが閉じていても成人向けのポーズの画像 ID が出た。`Service.status` は返す前に、`Request.References` と `Mask` のうち、成人向けのポーズ（出どころが `pose_library` の `sexy_`・`duo_`）か、その写し（`batch_input` を辿った先が成人向け）を取り除く（`history.Store.RefHidden`。保存した行は変えないので、ゲートを開ければ戻る）。ほかに保存した `Request`・見積り・入力をそのまま返す経路は、ジョブ一覧（モデル名と件数だけ）・`Batches`（見積りを載せない）・レシピ（強さ以外の入力を持たない）・結果の詳細（`InputHidden` で除く）で、いずれも名前も ID も出ない。②**マッチも、閉じたあとに走り出すなら失敗にする**: プロンプトマッチ（`Extra.Handle`）は `loadInputs` の前に走るので、`match.Service.reference` が参考画像（`Inputs()[0]`）を読む前に同じ `InputHidden` を確かめ、閉じていれば `runner.ErrInputHidden` で失敗にする（LLM に画像を見せない・生成しない・ラウンドは失敗で確定）。終わったマッチの続き（`ContinueBatchID`）も、見積り（`match.Service.continued`）で同じ検査をして `invalid` で断る（理由に名前を出さない。実行は見積りを通るので同じく止まる）。

**VNCCS PoseStudio（Issue #985・#993）**: ルート ①（T2I + LoRA）と ②（Edit）は、コアのノードだけの既存テンプレート（`txt2img-qwen-image21`・`txt2img-qwen-image-edit-2511`）と LoRA の差し込み（`lora_model_node`）で動く。ルート ③（3D 人形の画像でポーズを写す）は、**ノード無し**で、Shisei のポーズ集（`pose_library`。人形の画像）を 1 枚目の参照に、人物の画像を 2 枚目の参照に渡し、LoRA `MIUProject/VNCCS_PoseStudio_QI2.1`（V2。約 159 MB。ライセンスの記載なし）を重ねたモデル `qwen-image-21-vnccs-pose`（テンプレート `txt2img-qwen-image21-vnccs-pose`。作者のワークフローのプロンプト「replace the pose of <image 2> with the pose of <image 1>. keep the character of <image 2>」、強さ 1.0）で動く。Shisei の線画の人形との組み合わせの出来は実機で未確認。ルート ④（`VNCCS_PoseStudio` のノードで人形を動かす）は、追加ノード `vnccs-utils`（[model-manager/custom-nodes.md](../model-manager/custom-nodes.md) §2.1）が要るが、ノードがブラウザの画面でポーズを取り込むのを待つ作りで（`pose_image` の経路も画面が要る）、画面なしでポーズを渡す経路が無く、実行時の自動取得も固定できないため、**mam は提供しない**（導入できず、テンプレートも作らない。理由は custom-nodes.md §5.4）。ComfyUI の画面で作った人形の画像は、`pose` の役割の参照として渡せる。

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
| `facefusion`（`internal/facefusion`。サービスの外） | FaceFusion の検出・モデルの表とライセンスの級・`headless-run` の起動（[face-swap.md](face-swap.md)）。`runner/faceswap.go`・`runner/facescore.go` が呼ぶ |
| `face`（`internal/service/gentest/face`） | 顔の入れ替え（`Swap`・`Available`）と、アプリがサービスに渡す顔のモデルの口（`Crop`・`Score`・`Swapper`）。`service/gentest` の行数の上限のため別パッケージ。テストもここ |
| `venhance` / `videoenhance`（`internal/service/gentest/…`） | 動画の大きさ・コマ数を変える処理（Issue #1031。§3.9）。`venhance` は `Resolve`〔値の検査と出力の予定〕・見積りの置き場 `Estimates`・`Prober`（契約の型 `Op`・`Source`・`Params`・`Cloud` の口は `agent/cloudenhance`。ハンドラーは agent を import できないので `venhance` が `Op` と定数を別名で出す）、`videoenhance` は `Enhancers`・`EstimateEnhance`・`Enhance`。`runner/venhance.go` が結果 1 件を実行する。`service/gentest` の行数の上限のため別パッケージ。テストもここ |
| `gentesttest` / `servicetest` / `localtest` / `speedtest` | テストの道具と、パッケージをまたぐテスト |

`Service` は `projectlink.Link`・`runner.Runner`・`history.Store` を埋め込み、これらのメソッドを自分のものとして公開する。`Deps.Extra`（プロンプトマッチ）は `runner.Runner` のフィールドで、`match.New` が組み立てるときに入れる。

## 改訂履歴

[gentest.changelog.md](gentest.changelog.md)
