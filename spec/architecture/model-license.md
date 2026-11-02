# モデルの商用利用の可否（Issue #928）

商用で使ってよいかは、モデルを選ぶときの大事な判断材料。mam は **すべてのモデル選択・生成結果にその答えを出し、どこでも絞り込める**ようにする。この文書はデータ（§1〜§3）・表示（§4）・フィルターと設定（§5）・MCP（§6）・後続のモデル追加の手順（§7）を決める。

関連: [model-manager/model-manager.md](model-manager/model-manager.md) §4（カタログの JSON）・§6（同意）、[gentest/gentest.md](gentest/gentest.md)、[../components/color-rules.md](../components/color-rules.md)（色）、[../components/shared-ui-parts.md](../components/shared-ui-parts.md)。

## 1. 4 つの答え

`backend/internal/domain/modellicense`（標準ライブラリだけの葉のパッケージ）が、画面・結果・MCP・カタログの共通の言葉を持つ。

| 値（`commercial`） | バッジの語 | 意味 |
|---|---|---|
| `yes` | 商用OK | ライセンス（規約）が商用利用を認めている。著作権表示などの義務だけがある |
| `conditional` | 条件つき | 条件を満たす間だけ商用で使える（年商の上限・契約のプランなど）。`condition` に短く書く |
| `no` | 商用NG | 非商用・研究用のライセンス。商用には別の契約が要る |
| `unknown` | 不明 | ライセンスが答えを言わない、または mam が一次情報で確かめられていない。**「商用NG」ではない**が、確認できるまで商用には使わない前提で扱う。必ず `notice` が付く |

- 厳しさの順は **商用OK < 条件つき < 不明 < 商用NG**。複数の部品でできたモデルの答えは `modellicense.Strictest` で決める（§3）。
- 1 モデルに付く情報（`modellicense.Info`）: `commercial`・`license_name`・`condition`・`source_url`（ライセンス原文）・`checked_at`（原文を読んだ日。YYYY-MM-DD）・`notice`（注意）。`Info.Check()` が、答えが 4 つのどれか・名前・https の出典・日付があり、`conditional` は条件、`yes` 以外は注意を持つことを確かめる。

## 2. データの置き場所（後続のモデル追加はここへ書く）

| 対象 | 置き場所 | 識別子 |
|---|---|---|
| ローカルのカタログのモデル | `backend/internal/modelmgr/catalog/data/models/<id>.json` の `license` | カタログ ID → 導入で作るワークフロー名（`comfyui:bundled-comfyui/<name>`） |
| クラウド・サブスクの組み込みモデル | `backend/internal/domain/modellicense/data/cloud.json`（ID をキーにした 1 つの表） | 生成テストのモデル ID（`kling-3.0`、`gpt-image-2:chatgpt-subscription`） |
| Hugging Face から足したファイル | 導入のときにモデルカードの `license` タグを記録（`state.json`）。`modellicense.FromHF` が読む。カードの `license_link` は **https の URL だけ**を原文のリンク（`SourceURL`）にし、相対パス・http・`javascript:`・`file:` などは空にする（href に入るため。カタログの `Info.Validate` と同じ規則） | 作ったモデルの ID |
| 利用者が登録した外部エンドポイントのモデル・保存したワークフロー | **持たない** | `unknown`（注意: 「mam は確認できていません」） |

ローカルのカタログの `license`（`catalog.License`）は、同意に使う `id`・`version`・`name`・`url`・`summary`・`notice`・`excluded_regions` に加えて次を持つ。

- `commercial`: `yes` / `conditional` / `no` / `unknown`（旧 `commercial_use` の真偽値は廃止）。
- `commercial_condition`: 条件。`conditional` では必須。
- `checked_at`: `url` の原文を読んだ日。**必須**。
- `components[]`: ベースと派生（LoRA・マージ・テキストエンコーダー・VAE）でライセンスが違うときの内訳。各 `{part, name, url, commercial}`。2 つ以上あるなら全体の `commercial` は **内訳の最も厳しい答え**でなければならず、検証（`catalog.Validate`）が弾く。

ローカルの実行環境（GPT-SoVITS・MuseTalk。`data/runtimes/*.json`）と重み・依存ファイル（`data/assets/*.json`）、エンジン（`data/engine.json`）も同じ 4 値を使う（`ResourceLicense.commercial`）。`unclear` は `unknown` になった。

**検知**: ローカルのカタログは `catalog.Validate`（と `TestEveryModelHasItsLicenseWithTheSourceAndTheDateItWasRead`）、クラウドの組み込みモデルは `TestEveryBuiltInModelHasItsCommercialUseLicense`（生成テストのモデル一覧のすべてに `cloud.json` の項目があるか）と `TestEveryEntryOfTheCloudLicensesIsAModel`（表の ID がモデルとして実在するか）が、ライセンスの無いモデルを落とす。

## 3. ベースと派生は厳しい方

- 各部品の答えは、その部品の一次情報（モデルカード・LICENSE の原文）から決める。配布元（Comfy-Org など）がミラーしているファイルは、元のリポジトリのライセンスに従う。
- 派生（LoRA・マージ）の作者がライセンスを示していないなら、その部品は `unknown`。ベースが商用OKでも、全体は `unknown`（ベースが商用NGなら `no`）。
- 画面は、内訳の部品が 2 つ以上あるときも **全体の答え 1 つ**をバッジに出す。内訳は `components[]` と詳細（MCP・同意）に出る。

### モデルごとの答えと確認日（`checked_at` はモデルごとの JSON が持つ。日付ごとの調査分は下）

- **2026-11-01**: エンジン（`engine.json`）・ローカル実行環境（GPT-SoVITS・MuseTalk）。
- **2026-11-02**: 下の表の、次に挙げる 2026-11-03 の分を除くローカルのモデル（`sdxl-base`・`flux1-*`・`wan22-*`・`ltx-*`・`chroma1-hd`・`sd35-medium`・`hunyuanvideo15-480p`・`noctq-v4-qwen-image21` など）と、クラウドのモデル（`cloud.json`）。
- **2026-11-03**: `qwen-image-edit-2511`・`sdxl-controlnet-canny`・`sdxl-ipadapter-plus`・`upscale-*` の 3 つ、成人向けのモデル 14 件（#984。`realvisxl-v50` から `qwen-image-21-uncensored-int8` まで）。

| モデル | 答え | 根拠（一次情報） |
|---|---|---|
| `sdxl-base`・`ltx-video-2b` | 商用OK | OpenRAIL(++)-M。使用制限（Attachment A）つき |
| `flux1-schnell`・`qwen-image`・`wan22-*`（ベース）・`chroma1-hd` | 商用OK | Apache-2.0 |
| `flux2-klein-4b` | 商用OK | Apache-2.0（モデルカード `black-forest-labs/FLUX.2-klein-4B`・`-4b-fp8` の `license: apache-2.0` と `LICENSE.md`）。テキストエンコーダー Qwen3 4B も Apache-2.0。VAE は FLUX.2 [dev]（非商用）の再配布ではなく、Apache-2.0 の klein のリポジトリ（`Comfy-Org/Flux-2-Klein-4B`）のファイルを使う。9B と dev は非商用のため入れない |
| `qwen-image-edit-2511` | 商用OK | Apache-2.0（Qwen のモデルカード `Qwen/Qwen-Image-Edit-2511` の `license: apache-2.0` と「Qwen-Image is licensed under Apache 2.0.」。取得元の `Comfy-Org/Qwen-Image-Edit` のカードも `license: apache-2.0`。`checked_at` は 2026-11-03）。テキストエンコーダーとVAEは `qwen-image` と同じファイル。Qwen 2.1 の `noctq-v4-qwen-image21`（Qwen Research License・非商用）とは別のライセンス |
| `sdxl-controlnet-canny` | 商用OK | ControlNet（`xinsir/controlnet-canny-sdxl-1.0`）はモデルカードの `license: apache-2.0`（README の Model Description も `License: apache-2.0`）。ベースの SDXL は CreativeML Open RAIL++-M で、使用制限つきで商用可。2 部品とも商用OK（#976。`checked_at` は 2026-11-03） |
| `sdxl-ipadapter-plus` | 商用OK | IP-Adapter（`h94/IP-Adapter`）はモデルカードの `license: apache-2.0`。画像エンコーダー（OpenCLIP ViT-H/14 laion2B。元のカード `laion/CLIP-ViT-H-14-laion2B-s32B-b79K`）は `license: mit`。ベースの SDXL は CreativeML Open RAIL++-M。顔の IP-Adapter（FaceID 系）は InsightFace を使うが、ここの Plus（顔以外）は使わない。3 部品とも商用OK（#976。`checked_at` は 2026-11-03） |
| `flux1-dev` | 商用NG | FLUX.1 [dev] Non-Commercial License v1.1.1 |
| `flux1-dev-nsfw-master` | 商用NG | ベースが非商用。LoRA（NSFW MASTER）は HF が MIT でも原本の作者のライセンスを確認できず、内訳は不明 |
| `noctq-v4-qwen-image21` | **商用NG** | `LICENSE`（Qwen RESEARCH LICENSE AGREEMENT、2026-09-20）§2(a) は「非商用の目的に限る」、§2(b) は「別の商用ライセンス無しに商用目的で使わない」と書く。Noct Q の重みも、テキストエンコーダー・VAE（Comfy-Org の配布。カードは `license: other / qwen-research`）も同じ。Qwen/Qwen-Image-2.1 本体のカードも `qwen-research` |
| `sd35-medium` | 条件つき | 年間売上 100 万米ドル未満 |
| `ltx-2.5-22b-distilled` | 条件つき | 年間売上 1,000 万米ドル未満。テキストエンコーダーの Gemma 4 は Apache-2.0 |
| `ltx-2.5-22b-distilled-nvfp4`・`ltx-2.5-22b-distilled-dfr` | 条件つき | `ltx-2.5-22b-distilled` と同じ（LTX-2.x Community。年間売上 1,000 万米ドル未満）。DFR の詳細化の IC-LoRA（`Lightricks/LTX-2.5-22b-IC-LoRA-Pixel-Spatial-Upscaler`）もモデルカードが `license_name: ltx-2.x-community-license`（同じ条件）。標準モデルが足したプロンプト拡張の Gemma 4 E2B（`Comfy-Org/gemma-4`）は `license: apache-2.0`。#968 |
| `wan22-ti2v-5b-fp8` | 商用OK | Apache-2.0。fp8 のファイルは Kijai（`Kijai/WanVideo_comfy_fp8_scaled`）の変換で、モデルカードも `license: apache-2.0`、元の `Wan-AI/Wan2.2-TI2V-5B` も Apache-2.0。#967 |
| `ltx-2.5-22b-distilled-uncensored` | **不明** | 拡散モデル・VAE は LTX-2.x Community（条件つき）、元の Gemma 4 と非検閲化した `llmfan46/gemma-4-12B-it-uncensored-heretic`（HF のタグ `apache-2.0`）は商用OK。LTX-2.5 用に変換した `DeepNeuralNerd/…LTX_2.5_ComfyUI` は `license: other` で本文が無く、「上流の Gemma 4 と LTX-2 のライセンスを参照」とだけ書く。厳しい方の「不明」（#927。2026-11-02） |
| `hunyuanvideo15-480p` | 条件つき | 月間 1 億人超は許諾。EU・英国・韓国では使えない |
| `wan22-i2v-14b-lightx2v` | 商用OK | ベースの Wan 2.2 も LightX2V の 4 ステップ LoRA（`lightx2v/Wan2.2-Distill-Loras`）も Apache-2.0（LoRA はモデルカードの `license: apache-2.0`。LICENSE のファイルは無く、カードの宣言に従う）。#930 |
| `wan22-i2v-14b-lightx2v-uncensored` | **不明** | ベースは Apache-2.0（商用OK）。非検閲の LoRA（`rzgar/Wan2.2_LightX2V_4Step_Uncensored`）はカードが `license: apache-2.0` と宣言するだけで、LICENSE の本文も、非検閲の学習に使ったデータの出所も無い。宣言だけでは確かめられないので、厳しい方の「不明」（`license.id = apache-2.0-declared`）。成人向けなので同意必須。#930 |
| `wan22-i2v-14b-nsfw-lora`・`wan22-remix-nsfw-i2v`・`wan22-remix-nsfw-t2v` | **不明** | ベースの Wan 2.2 は Apache-2.0。LoRA・マージの側にライセンスの本文が無い（旧版は「商用不可」と扱っていた。確認できないものは断定せず不明にした） |
| `realvisxl-v50` | 商用OK | 作者 SG161222 のモデルカード（`SG161222/RealVisXL_V5.0`）の `license: openrail++`。ベースの SDXL も同じ OpenRAIL++-M。成人向けなので同意必須（#984） |
| `cyberrealistic-xl-v10` | 商用OK | 作者 Cyberdelia のモデルカード（`cyberdelia/CyberRealisticXL`）が「CreativeML Open RAIL++-M License」と書き、`license: creativeml-openrail-m`（#984） |
| `juggernaut-xl-ragnarok` | 商用OK | 作者 KandooAI のモデルカード（`Kandoo/Juggernaut-XL-Ragnarok`）の `license: creativeml-openrail-m` と本文「モデルも生成物も商用で使える。使用制限に挙げた目的には使えない」。法人向けの商用ライセンスの相談先は別に載る（#984） |
| `bigasp-v2`・`big-lust-v16`・`epicrealism-xl-lastfame`・`lustify-sdxl-zenith-v9`・`realism-sdxl-stable-yogi-v8`・`thisisreal-sdxl-v3`・`wai-illustrious-sdxl-v17` | **不明** | Civitai のモデルページにも、配布元の HF のカードにも、作者のライセンスの名前・本文が無い（`license.id = unspecified`）。**Civitai の `allowCommercialUse` は参考で、これだけでは商用OKにしない**（`summary` に併記）。第三者のミラーの HF の `license` タグ（`creativeml-openrail-m`・`apache-2.0`・`mit`・`unknown` が割れる）は原本の作者の宣言ではないので採らない。WAI は Illustrious 系の上流が Fair AI Public License 1.0-SD だが、ページはどれに従うかを書かない（#984） |
| `pony-diffusion-v6-xl` | **商用NG** | Civitai のページ（model 257749）が「modified Fair AI Public License 1.0-SD」と書く: 課金・優先枠など収益化する Web サイト・アプリでの推論を許さず、商用は作者へ連絡。配布元の HF のカードの `license: creativeml-openrail-m` より、作者のページの条項を厳しい方として採る（#984） |
| `cyberrealistic-pony-v18` | **商用NG** | 作者のカードは `creativeml-openrail-m` だが、ベースの Pony Diffusion V6（上の行）の制限が及ぶので内訳の厳しい方（#984） |
| `noobai-xl-v11` | **商用NG** | 作者 Laxhar のカード（`Laxhar/noobai-XL-1.1`）が fair-ai-public-license-1.0-sd を継承し、追加条項「II. Commercial Prohibition: モデル・派生モデル・生成物の収益化・商用利用を含むあらゆる商業化を禁じる」を書く（#984） |
| `qwen-image-21-uncensored-int8` | **商用NG** | 配布元（`abenzerps/Qwen-Image-2.1-Uncensored-Quantized`）のカードが `license: other / license_name: qwen-research` で、Qwen/Qwen-Image-2.1 の量子化と書く。本文は Qwen RESEARCH LICENSE AGREEMENT（`noctq-v4-qwen-image21` と同じ。テキストエンコーダー・VAE も同じ）（#984） |
| `upscale-nomos-uni-span` | 商用OK | 4xNomosUni_span_multijpg のモデルカード（メタデータ `license: cc-by-4.0`・本文 `License: CC BY 4.0`）。作者（Philip Hofmann）の表示が条件。学習データの条件は未確認（#975） |
| `upscale-nomos-webphoto-esrgan` | **不明** | 4xNomosWebPhoto_esrgan のモデルカードは、メタデータが `license: cc-by-4.0` で本文が `License: CC-BY-0.4`（存在しない版）と食い違い、作者の GitHub リリースも `CC-BY-0.4`。LICENSE ファイルは無い。確かめられないので不明（#975） |
| `upscale-2pass-sdxl-span` | 商用OK | SDXL base（CreativeML Open RAIL++-M）と SPAN（CC BY 4.0）の 2 部品で、どちらも商用OK |
| クラウド: `nano-banana`・`nano-banana-lite`・`gemini-omni-flash`（Gemini API。「Use of Generated Content」: Google は生成物の所有権を主張しない）、`flux-2`（BFL。生成物の所有権を主張しない）、`gpt-image-2`・ChatGPT サブスク（OpenAI。Output は利用者のもの）、`runway-gen-4.5`（Runway。入力と生成物の所有権を主張しない） | 商用OK | 各社の利用規約を 2026-11-02 に読み、生成物の所有権を主張しない・利用者に帰属させるとしている（出典 URL は `cloud.json`）。ChatGPT サブスクは mam が使う経路が非公式で、注意を付ける |
| クラウド: `luma-ray-3.2` | 条件つき | 規約 §4: 商用利用を認めるサブスクリプションに限る |
| クラウド: `kling-3.0`・`kling-4.0`・`minimax-h3`・`minimax-h3-max`・`wan-3.0`・`seedance-2.0`・`seedance-2.5`（BytePlus） | **不明** | 規約の本文を読めなかった、または商用利用を許す文を見つけられなかった（公開ページが JS 描画・地域制限・契約で非公開）。商用の前に契約を確認する |

## 4. 表示（モデルを選ぶ・モデルが出てくるすべての画面）

部品は 3 つ。**どの画面もこれ以外の手書きをしない**（`atoms.CommercialBadge` / `CommercialMark` と `molecules/commercial` の `Badge` / `Mark` / `Note` / `Filter`）。

- **バッジ（`Badge`）**: アイコン + 短い語（商用OK / 条件つき / 商用NG / 不明）。ツールチップ（`title`）と `aria-label` にライセンス名と条件・注意。`data-commercial-badge="<値>"`。
- **マーク（`Mark`）**: アイコンだけ。結果のカード・表の細いセルなど、語が入らない所。意味はツールチップと `aria-label`。
- **ノート（`Note`）**: 場所に余裕がある所（モデルのカード・結果の詳細）で、条件・注意をことばで出す。ライセンス原文へのリンクと「（YYYY-MM-DD に確認）」を付ける。`yes` は何も出さない。`unknown` と `no` は枠つきで目立たせる。
- アイコン（Lucide）: 商用OK＝`badge-check`、条件つき＝`badge-alert`、不明＝`badge-question-mark`、商用NG＝`ban`。
- 色は #867 の意味の色（[color-rules.md](../components/color-rules.md) §3.1）に従う: 商用OK＝成功（emerald）、条件つき＝警告（amber）、商用NG＝危険（red）、不明＝警告（amber）に **枠（`border-amber-500/60`）** を足して見落とさせない。新しい色は作らない。
- `<select>` の `<option>` は文字しか入らないので、`モデル名（商用OK）` のように括弧で答えを続ける（`commercial.OptionLabel`）。

画面ごとの場所:

| 画面 | 出す所 |
|---|---|
| 生成テスト: モデルのカード | カードにバッジ。条件つき・不明は 1 行の短い注記。「?」ヘルプにモデルごとのライセンス名と条件 |
| 生成テスト: 比較の表 | 行の見出し（モデル名）の下にバッジ。各結果のカードにマーク |
| 生成テスト: プロンプトマッチ（#916） | モデルの選択肢の語（`OptionLabel`）とフィルター（既定は設定に従う）、結果のモデル名にバッジ |
| 生成テスト: 履歴・結果の詳細 | 履歴の各カードにマーク。詳細にノート |
| モデルマネージャー: モデルの一覧・同意ダイアログ | 一覧の各カードにバッジとノート。同意ダイアログは「商用利用」の行にバッジ |
| モデルマネージャー: 試し生成 | モデルの選択肢の語と、各結果にマーク |
| モデルマネージャー: Hugging Face の導入・足したモデルの一覧 | リポジトリのライセンスの行と各モデルにバッジ（カードの `license` タグから） |
| モデルマネージャー: ローカル音声・口パクの重み | 重みごとのバッジ（4 値） |
| 設定: 画像・動画モデルの割り当て（モーダル） | 選択肢の語。フィルター |
| ショットの動画モデルの固定（LOCK）・Take の行・Take の追跡 | 選択肢の語、Take のマーク、追跡にバッジ |

## 5. フィルターと設定

- **フィルター（`molecules/commercial.Filter` と `static/src/components/commercial-filter`）**: すべて / 商用OKのみ / 条件つきまで。ラジオの並びで、`data-commercial-filter-target` が絞る一覧の CSS セレクターを持つ。一覧の中の `data-commercial="<値>"` を持つ要素（カード・行・`<option>`）を、選んだ答えが許さなければ隠し（`hidden`）、フォームの部品は無効にして送らない。広げるとスクリプトが無効にしたものだけ戻す（サーバーが無効にしたもの＝選べないモデルは戻さない）。選択中の `<option>` は隠さない（保存済みの割り当てを勝手に変えない）。隠した件数は「N 件を隠しています」と読み上げる。絞り込んだあとに `commercial-filter-applied` を投げ、生成テストは選択中のモデルの数えなおしに使う。
- **フィルターを置く画面**: 生成テストのモデルのカード・プロンプトマッチ（「画像から似せる」）のモデルの選択（`#gt-match-model`）・履歴（`commercial` をクエリに持ち、ページ送りで保つ）、モデルマネージャーのモデルの一覧と試し生成のモデルの選択、設定の画像・動画モデルのモーダル。
- **設定「商用OKのモデルだけを使う」**（既定はオフ。`state.json` の `commercial_only`）: オンの間、上のフィルターは「商用OKのみ」から始まり、MCP の `list_generation_models`・`list_model_catalog` は `commercial` 引数を省くと商用OKだけを返す（`modellicense.DefaultFilter`）。**一覧を絞るだけで、生成そのものは止めない**（ほかの答えのモデルを画面で広げて選べる。名指しの生成も断らない。代替のモデルへは切り替えない）。画面はモデルマネージャーの「商用利用で絞り込む」の区画のスイッチ（`PUT /model-manager/commercial`）、MCP は `get_commercial_only` / `set_commercial_only`。

## 6. MCP

- `list_generation_models`: 各モデルに `license`（`commercial`・`license_name`・`condition`・`source_url`・`checked_at`・`notice`）。入力 `commercial`（`all` / `ok` / `ok_or_conditional`。省くと設定に従う）で絞る。出力の `commercial_filter` は適用した絞り込み。不正な値は `validation`。
- `list_model_catalog`: `models[].license` に同じ項目（`commercial` は 4 値、`components[]`）。入力 `commercial`、出力 `commercial_filter` は同じ。
- `get_commercial_only` / `set_commercial_only`（§5）。
- 詳細は [../api/mcp-tools-generation-test.md](../api/mcp-tools-generation-test.md)・[../api/mcp-tools-model-manager.md](../api/mcp-tools-model-manager.md)。

## 7. 後続のモデル追加（#927・#929〜#931 など）の手順

1. ローカルのモデル: `data/models/<id>.json` の `license` に `commercial`・`commercial_condition`（条件つき）・`checked_at`・`url`（**一次情報**: モデルカードの LICENSE 原文）・`notice` を書く。ベースと派生でライセンスが違うなら `components[]` に部品ごとに書き、全体は最も厳しい答えにする。
2. クラウドのモデル: `domain/modellicense/data/cloud.json` に、生成テストのモデル ID で 1 項目を足す（`license_name`・`source_url`・`checked_at`・条件つき・不明・商用NG は `notice`）。
3. 一次情報で確かめられないものは **`unknown` にして `notice` で目立たせる**。推測で `yes` にしない。
4. テスト（`go test ./internal/modelmgr/catalog/ ./internal/service/gentest/...`）が、項目の欠け・内訳と全体の食い違い・表の ID の誤りを検知する。

## 8. 範囲外・未確認

- 「商用OK」は mam が一次情報を読んだ日の判断で、法的な助言ではない。バッジの `checked_at` と原文へのリンクで確かめられる。
- クラウドの規約は契約・プラン・地域で変わりうる。`unknown` の 7 モデルは、規約の本文を読めた段階で `cloud.json` を更新する。
- キャラクター作成（シリーズのキャラ）の画面は、現時点でモデルを選ぶ入力を持たない（モデルの割り当ては設定の画像・動画モデルのモーダルとショットの固定で行う）。選ぶ入力が足されたら、同じ `commercial.Filter` と `OptionLabel` を使う（§4）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 理由 |
|---|---|---|---|
| 1.0 | 2026-11-02 | 新規。4 つの答え・データの置き場所・ベースと派生は厳しい方・バッジ／フィルター／設定・MCP・後続の追加手順 | Issue #928 |
| 1.1 | 2026-11-02 | 非検閲のテキストエンコーダーの LTX-2.5（`ltx-2.5-22b-distilled-uncensored`）の答え（不明）を足した | Issue #927 |
| 1.2 | 2026-11-02 | §3 の答えの表に `flux2-klein-4b`（商用OK）を足した。VAE を FLUX.2 [dev] の再配布から取らず klein のリポジトリのものにした理由を書いた | Issue #931 |
| 1.3 | 2026-11-02 | §3 の答えの表に `wan22-i2v-14b-lightx2v`（商用OK）・`wan22-i2v-14b-lightx2v-uncensored`（不明。宣言だけ）・`wan22-remix-nsfw-t2v`（不明。I2V と同じ）を足した | Issue #930 |
| 1.4 | 2026-11-02 | §4・§5 にプロンプトマッチの画面（モデルの選択のフィルターと既定）を足した | 統合レビュー。#916 のモデルの選択だけフィルターと設定の既定が無かった |
| 1.5 | 2026-11-02 | §2: HF のカードの `license_link` は https のものだけをリンクにする | 統合レビュー。検証なしに `SourceURL` へ入れ、`templ.SafeURL` で href にしていた（`javascript:` が通る）。HF の `license` タグだけで商用OKにする点は変えていない（方針は人が決める） |
| 1.6 | 2026-11-02 | §3 の答えの表に `ltx-2.5-22b-distilled-nvfp4`・`-dfr`（条件つき）・`wan22-ti2v-5b-fp8`（商用OK）を足した | Issue #967・#968 |
| 1.7 | 2026-11-03 | §3 の答えの表に `qwen-image-edit-2511`（商用OK。Apache-2.0）を足した | Issue #971 |
| 1.8 | 2026-11-03 | §3 の答えの表に、非検閲（成人向け）の実写中心 14 モデルを足した（商用OK 3・不明 7・商用NG 4）。ライセンスは一次情報（作者のモデルカード・Civitai のページの文言）だけを読み、Civitai の `allowCommercialUse` だけでは商用OKにしない | Issue #984 |
| 1.9 | 2026-11-03 | §3 の答えの表にアップスケール用のモデル（`upscale-nomos-uni-span`＝商用OK・`upscale-nomos-webphoto-esrgan`＝不明（カードのメタデータと本文のライセンスが食い違う）・`upscale-2pass-sdxl-span`＝商用OK）を足した | Issue #975 |
| 1.10 | 2026-11-03 | §3 の見出しの「`checked_at` はすべて 2026-11-02」を、日付（2026-11-01・02・03）ごとの調査分の書き方に直し、答えの表に `sdxl-controlnet-canny`・`sdxl-ipadapter-plus`（どちらも商用OK。カタログの `license` の根拠どおり）の行を足した | Issue #976 の統合レビュー（表に行が無く、見出しの日付が実際と違った） |
