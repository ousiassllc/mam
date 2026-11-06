# モデルのフィルターと「選べる状態」（Issue #1011・#1013 のバックエンド）

モデルを探す・選ぶ画面（モデルマネージャーのカタログ・導入済み、生成テスト、プロジェクト・Shot の生成設定、LLM の割り当て、参照画像の役割ごとのモデル選択）と、MCP の `list_model_catalog`・`list_generation_models` が**同じ条件・同じ言葉**で絞り込むための土台。画面（フィルターバー・パネル・モデル選択のモーダル・カード・バッジ）は §8 のとおり、ここに書く API の上に作る。この文書は「API」と「画面」の正本。

## 1. 構成

| 層 | 置き場所 | 中身 |
|---|---|---|
| 条件・一致・相互変換 | `backend/internal/modelfilter`（葉。カタログも画面も知らない） | `Condition`（フィルターパネルの状態）・`Item`（モデルを条件が見る項目に縮めたもの）・`Match`・`Filter`・クエリ文字列と JSON の相互変換・`Availability`（選べる状態）・`RouteOf`（理由から直す画面の道すじ） |
| カタログの構造化データ | `backend/internal/modelmgr/catalog/traits` | `Traits`（パラメータ数・非検閲・速度/品質の目安・言語）・`Input`（入力の種類）・`Facts`（ワークフローが言う入力）・`Derived`（モデルの入力・音声） |
| 各画面・ツールの `Item` への変換 | `mmview.Entry.Item`（カタログ）・`modelcatalog.Info.Item`（生成テスト）・`llmmodels.Candidate.Item`（LLM）・`modelsettings.ImageOption.Item` / `VideoOption.Item`（プロジェクト・Shot の生成設定の候補） | 各層が自分のモデルの型を `Item` にする。絞り込みの規則はここに 1 つしか無い |

## 2. 条件（`modelfilter.Condition`）

画面の URL・フォーム、保存した選択、MCP の引数は、**同じキー名**を使う。複数を組み合わせられ、**設定した部分はすべて満たす**ものだけが残る。ゼロ値（何も設定しない）は何も絞らない。

| キー | 型 | 意味 |
|---|---|---|
| `kinds` | `image` / `video` / `image_edit` / `upscale` / `video_upscale` / `video_interpolate` / `llm` / `audio` の一覧 | 種類（**どれか**）。`image_edit` は画像モデルのうち入力に `edit` か `inpaint` を持つもの（画像モデルは `image` でもある）。`video_upscale`・`video_interpolate` は動画を大きくする・滑らかにするモデル（Issue #1031。[model-manager.md](model-manager.md) §4.7） |
| `inputs` | `traits.Input` の一覧 | 入力（指定した**すべて**を持つモデル）。§3.2 |
| `adult` | `all` / `general_only` / `adult_only` | 成人向け（`content_rating`）。三値。`all` は条件なし |
| `uncensored` | `all` / `censored_only` / `uncensored_only` | 非検閲（`traits.uncensored`）。成人向けとは別の軸 |
| `params` | `small` / `medium` / `large` の一覧 | パラメータ数の帯（**どれか**）。**5B ちょうどは `small`、14B ちょうどは `large`**（小 〜5B、中 5B より大きく 14B 未満、大 14B 以上）。数が分からないモデル（`params_b` が空）はどの帯にも入らない |
| `vram_fit` | 真偽値 | VRAM 16GB の GPU で動く（`vram.min_gb` が 16 以下。`vramfit.For` が 16 GB の GPU に出す `enough`・`limited` と同じ）。VRAM の目安が無いモデル（クラウド・LLM）は入らない。この PC の GPU から見た判定（`vram_fit` の出力）とは別 |
| `commercial` | `all` / `ok` / `ok_or_conditional`、または `ok`・`conditional`・`unknown`・`ng` のカンマ区切り | 商用利用。**既存の `commercial_filter`（`modellicense.Filter`）の 3 語と互換**で、4 語のカンマ区切りで「商用OK と 不明」のような組み合わせも言える。空は「設定『商用OKのモデルだけを使う』に従う」、`all` は「従わずすべて」（`Normalize` は明示の `all` と 4 語すべてを `all` にし、空と区別する） |
| `installed` | `all` / `installed_only` / `not_installed_only` | 導入済みか。クラウドのモデルは導入済みの扱い |
| `origins` | `local` / `api` の一覧 | ローカル（この PC・登録した接続先）か API（クラウド・サブスク）か |
| `providers` | 文字列の一覧 | 提供元（`local`・`anthropic`・`openai`・`google`・`chatgpt-subscription`・クラウド画像／動画モデルのベンダー名） |
| `audio` | 真偽値 | 音声つきの動画モデルだけ |
| `speeds` / `qualities` | `fast`・`standard`・`slow` / `basic`・`standard`・`high` の一覧 | 速度・品質の目安（**どれか**）。目安が無いモデルは入らない |
| `available_only` | 真偽値 | 今選べるものだけ（§5）。画面の「選べるものだけ」。**モデルを選ぶ画面は `modelfilter.Default()`（これがオン）から始め、「使えないものも表示」でオフにする**。ゼロ値（オフ）は使えないものも理由つきで返す（モデルマネージャーのカタログ・MCP） |
| `q` | 文字列 | テキスト検索。空白で区切った語を**すべて**含む（大文字小文字は区別しない）。名前・ID・提供元・タグ（種別・区分・トリガー語など）を見る |

関数:

| 関数 | 使い方 |
|---|---|
| `Condition.Match(Item) bool` / `Condition.Apply([]Item)` / `modelfilter.Filter(c, list, toItem)` | 一致の判定。順序は保つ |
| `Condition.Active() int` | 絞り込み中の部分の数（フィルターアイコンの件数バッジ。「3」）。商用チップは 1、検索語は 1、三値の `all` は 0。`available_only` は数えない（専用のスイッチがある） |
| `Condition.Validate() error` | 言葉が不正なら、そのキー名を含む文言（`params: "huge" is not small, medium or large`）。画面はそのまま見せ、MCP は `validation` で返す |
| `Condition.Normalize()` | 正規形: 小文字・空白除去・重複除去・画面の並び順・三値の `all` を空に。`Query`・`JSON` の出力は正規形 |
| `ParseQuery(url.Values)` / `Condition.Query(start)` | クエリ文字列との相互変換。リストは `kinds=image,video` でも `kinds=image&kinds=video` でも読める。真偽値は `1`・`true`。条件のキー以外（`page` など）は無視する。`Query(start)` の `start` は画面の出発点（選択画面は `Default()`、モデルマネージャーはゼロ値）: 出発点が `available_only` オンなのにオフにした選択は `available_only=0` と書いて往復で戻らないことを防ぐ |
| `ParseQueryOr(v, start)` | 条件のキーが 1 つも無いクエリ（画面を開いたところ。覚えた選択も無い）は `start` を返す |
| `ParseJSON([]byte)` / `Condition.JSON()` | JSON との相互変換（MCP の引数・画面ごとに覚える選択）。**未知のキーは拒む**（綴りの誤りで検索が広がらない） |
| `modelfilter.Keys()` | 条件のクエリのキーの一覧（フォームが送るもの・保存の対象） |
| `modelfilter.Kinds() / Origins() / Bands()` と各 `Label()`（`Band.Label`: 小（〜5B）・中（5〜14B）・大（14B〜））、`traits.All()` と `Input.Label()` | パネルの選択肢の元 |

画面ごとの選択の保存は、`Condition.JSON()`（またはクエリ）を画面のキーで保存すればよい（保存先は UI 作業側）。

## 3. カタログのモデルのスキーマ（§4.1 の追記。モデルを足す人はここを読む）

カタログの各モデル（`catalog/data/models/**/*.json`）は、`content_rating` の次に **`traits` オブジェクトを必ず持つ**（全件に `uncensored` を明示する。値を省くと「非検閲ではない」と読まれる漏れになるので、`traitstest` が生のファイルを見て拒む）。

### 3.1 `traits`（カタログの項目。ワークフローからは分からないもの）

| フィールド | 型 | 内容 |
|---|---|---|
| `params_b` | 数値（十億単位） | 画像・動画を作るモデル本体のパラメータ数。**モデルの名前・説明・ベンダーのカードに書いてある確かな値だけ**（例 Wan 2.2 14B = `14`、LTX-2.5 22B = `22`、Chroma1-HD = `8.9`）。分からないときは**省く**（検証は許す。条件 `params` はその帯に入れない）。SDXL 系はベンダーが公表する UNet の 2.6B（`2.6`）。アップスケール用のモデルは持たない |
| `params_label` | 文字列 | 表示用（`14B`・`8.9B`・`2.6B（UNet）`）。`params_b` と**セット**（片方だけは検証が拒む）。形は `数字B` か `数字B（何の数か）` |
| `uncensored` | 真偽値（**必須**） | 重み・LoRA・テキストエンコーダー・マージのどれかが、検閲を外す・成人向けの出力を前提に作られ、配られている（ファイル名・説明・リポジトリに `uncensored`・`nsfw`・`heretic`・`abliterated` など）。**成人向け（`content_rating`）とは別**: 一般向けの用途も作れる成人向けのモデル（Juggernaut XL・RealVisXL など。作者が「SFW も NSFW も作れる」と書くもの）は `adult` で `uncensored: false`。**`uncensored: true` は必ず `content_rating: adult`**（検証が拒む）。逆に、ファイル名・URL・テンプレート名に上の語を持つモデルは `uncensored: true` でなければならない（検証が拒む。「非検閲の LoRA を組み合わせたモデル・非検閲のテキストエンコーダーを使うモデル」が一般向けに残る漏れを防ぐ） |
| `speed` | `fast` / `standard` / `slow`（省略可） | 速度の目安。`fast` は蒸留・少ステップ（Lightning・Turbo・LightX2V・schnell・klein）、`slow` は 14B 級の動画・22B 級。カタログの作者の目安で、計測ではない（実測は §4.5 の表） |
| `quality` | `basic` / `standard` / `high`（省略可） | 品質の目安（作者の目安。付けるのは説明が「質を優先するならこちら」と言うモデルだけ） |
| `languages` | ISO 639-1 の小文字 2 字の一覧（省略可） | 音声を作るモデルの言語（`ja`・`en`）。**音声つきのモデルだけ**（検証が拒む）。今のカタログに音声を作る画像・動画モデルの言語表は無く、空 |

### 3.2 `inputs`・`audio`（カタログには**書かない**。ワークフローから導く）

入力と音声つきかは、モデルの `workflows[].template`（`localcfg` のテンプレートの `assignments`・`reference_mode`・`video.native_audio`）から `traits.Derive` が導く。生成テストの `reference_mode`・`reference_roles`・`video`（`first_frame`・`last_frame`・`native_audio`）も**同じ事実（`traits.Facts`）**から読むので、二重管理にならず食い違わない。

| `traits.Input` | 導く元 |
|---|---|
| `t2i` | 画像のワークフローで、参照が無くても動く（ControlNet・IP-Adapter は参照が要るので持たない） |
| `i2i` | 参照画像を受ける画像のワークフロー（img2img・edit・inpaint） |
| `inpaint` / `edit` | `reference_mode` が `inpaint` / 編集のモデルの役割 `edit` |
| `t2v` | 動画のワークフローで先頭フレーム・参照が要らない（クラウドの動画モデルは、フレームを省けるので `first_frame` を持てば `t2v` も持つ） |
| `i2v` | 先頭フレーム（`first_frame`）か参照画像を受ける動画のワークフロー |
| `first_frame` / `last_frame` | 動画のワークフローの `first_frame` / `last_frame` の割り当て |
| `reference_whole` / `reference_face` / `reference_style` / `reference_pose` | 画像の参照の役割（`reference` / `face` / `style` / `pose`。編集のモデルは 4 つ、img2img・IP-Adapter は全体だけ） |
| `reference_control` | ControlNet の制御画像 |

モデルの入力は**そのモデルのワークフローすべての和**（例: `wan22-ti2v-5b` は `t2v`・`i2v`・`first_frame`、`ltx-2.5-22b-distilled` は `t2v`・`i2v`・`first_frame`・`last_frame`）。`audio` は、どれかのワークフローの `video.native_audio` が真（LTX-2.5）。API は `mmview.Entry.Inputs`・`Entry.Audio`（カタログ）、`modelcatalog.Info.Inputs`・`Info.Video.NativeAudio`（生成テスト）。

### 3.3 検証（`catalog.Validate`・`traits.Traits.Validate`）

`params_b` と `params_label` は両方か両方なし、`params_label` は形に合う、`speed`・`quality` は上の語、`languages` は音声つきのモデルだけで 2 字のコード、`uncensored` なら `adult`、非検閲の語を持つファイル・テンプレートがあれば `uncensored`。テスト: `traitstest`（全件に `traits` と `uncensored` がある・入力がワークフローに従う・パラメータ数・非検閲の一覧・矛盾の拒否）。

### 3.4 既存のカタログを埋めた結果（2026-11-03）

- **パラメータ数**（確かなものだけ）: Wan 2.2 14B 系 6 件 = 14、Wan 2.2 TI2V 5B 系 2 件 = 5、LTX-2.5 22B 系 4 件 = 22、LTX-Video 2B = 2、FLUX.2 klein 4B = 4、Chroma1-HD = 8.9、FLUX.1 dev・schnell・NSFW MASTER = 12、Qwen-Image・Qwen-Image-Edit 2511・Rapid-AIO = 20、SD3.5 Medium = 2.5、HunyuanVideo 1.5 = 8.3、SDXL 系（`sdxl` のテンプレートのモデル）= 2.6（UNet）。**空のまま**: `noctq-v4-qwen-image21`・`qwen-image-21-uncensored-int8`・`qwen-image-21-vnccs-pose`（Qwen-Image 2.1。名前・説明に拡散モデルの数字が無い）、`lustify-v10-krea2-turbo-fp8`（Krea 2 Turbo。テキストエンコーダーの 4B だけが説明にある）、アップスケール 3 件（持たない）。FLUX.1・Qwen-Image・SD3.5 Medium・HunyuanVideo 1.5 の数字は、名前・説明には無く、ベンダーが公表している値（FLUX.1 = 12B、Qwen-Image = 20B、SD3.5 Medium = 2.5B、HunyuanVideo 1.5 = 8.3B）。
- **非検閲**: 成人向け 25 件のうち、**一般向けの用途も作れる 7 件**（`cyberrealistic-xl-v10`・`epicrealism-xl-lastfame`・`juggernaut-xl-ragnarok`・`realism-sdxl-stable-yogi-v8`・`thisisreal-sdxl-v3`・`realvisxl-v50`・`realvisxl-v50-lightning`。作者が成人向けに特化したと書いていない）が `uncensored: false`、残り 18 件が `true`。
- **成人向けの漏れの洗い出し**: 全 47 件のファイル名・URL・テンプレート名・説明を `uncensor`・`nsfw`・`heretic`・`abliterat` で走査した。一般向け（`general`）のモデルに、非検閲の LoRA・非検閲のテキストエンコーダーを組み合わせたものは**見つからなかった**（`ltx-2.5-22b-distilled` の説明にある「非検閲」は「非検閲ではない標準の Gemma で拡張する」の意味。`flux2-klein-4b` の説明の「検閲寄り」は公式が安全調整つきという意味。`qwen-image-21-vnccs-pose` の LoRA は作者がポーズ用と書くのみで非検閲の記載が確かめられない。いずれも一般向けのまま）。**今後は検証が漏れを止める**（§3.3）。

## 4. API（UI 作業者が使う）

| 用途 | 使うもの |
|---|---|
| モデルマネージャーのカタログ（導入済み・未導入） | `mmview.Where(svc, cond) []Entry`。`Entry` は `ModelView` に `traits`・`inputs`・`audio`・`available`・`unavailable`・`unavailable_message`・`settings_route` を足したもの。`mmview.Entries(svc)`（全件）・`mmview.EntryOf(svc, view)`（1 件）。**モデルマネージャーは `cond` のゼロ値から始める**（未導入も見せる）。導入済みだけを選ぶ画面は `installed=installed_only` |
| 生成テスト・プロジェクト・Shot の生成設定のモデル選択 | `gentest.Service.ModelsWhere(ctx, media, cond)`（`modelcatalog.Info` の一覧。選択画面は `Default()` から）。`Info` に `available`・`unavailable`・`unavailable_message`・`settings_route`・`inputs`・`provider`・`origin`・`catalog`（カタログのモデルの成人向け・`traits`・`vram_min_gb`）。1 件の可否は `Service.Availability(id)` |
| 割り当て（設定の画像・動画モデル、Quality Policy）の候補 | `modelsettings.View.ImageModels` / `VideoModels` の各候補に `Availability`。`View.ImageModelsWhere(cond)` / `VideoModelsWhere(cond)`。今の割り当てが使えないモデルを指すとき `View.Unusable[]`（§5.3） |
| LLM の割り当ての候補 | `llmmodels.Service.Candidates(ctx, cond)`（`Candidate` の一覧。選択画面は `Default()` から）。プロバイダー 1 つの可否は `Availability(provider)`、各 `List` に `Availability` |
| 入力画像を添えたときのモデルの並べ替え（#1009） | `modelfilter.InputsOfRoles(roles []string) []traits.Input`（添えた画像の役割 `reference`・`face`・`style`・`edit`・`pose`・`first_frame`・`last_frame` から、モデルが受け付けるべき入力 `reference_whole`・`reference_face`・…・`first_frame`・`last_frame` を返す。重複なし）と `modelfilter.PreferForInputs[T](list []T, item func(T) Item, attached []traits.Input) []T`（`Item.Inputs` が `attached` を多く持つモデルを先に並べる**安定ソート**。同じ数ならもとの順。何も隠さない（持たないモデルは最後に残る）。`attached` が空なら並びは変えない。`list` を並べ替えて返す）。生成テストのモデル選択は、添えた画像の役割を `InputsOfRoles` に渡し、`ModelsWhere` の結果を `PreferForInputs` に通す |

## 5. 選べる状態（Issue #1013）

### 5.1 `modelfilter.Availability`

モデルを選ぶ画面の元データは、どれも同じ形で「今使えるか」を返す: `available`（真偽値）・`unavailable`（理由のコード `Reason`）・`unavailable_message`（利用者向けの文）・`settings_route`（直す画面の道すじ）。

| 理由のコード | 意味 | 道すじ（`RouteOf`） |
|---|---|---|
| `no_backend` | API プロバイダーの API キーが未登録（生成テストが前から使っているコード。値は変えない）。登録した接続先のモデルは接続先が使えない | `/settings/api-keys`（接続先のモデルは `/settings/local-models`） |
| `not_signed_in` | サブスク（ChatGPT）に未ログイン | `/settings/api-keys`（「ChatGPT でログイン」は同じ分類） |
| `not_installed` | ローカルのモデルが未導入・導入中・ファイル欠け（モデルマネージャーのカタログ） | `/model-manager` |
| `age_gate` | 成人向けのモデルで、年齢確認がオフ（アダプターの理由は文なので、一覧はこのコードで返す） | `/model-manager` |
| `missing_node` | ワークフローの追加ノードが未導入（生成テストが前から使っているコード） | `/model-manager` |
| `circuit_open` | ベンダーの失敗が続き、しばらく止めている（自然に戻る） | なし（空） |

### 5.2 初期状態は「選べるものだけ」

モデルを選ぶ画面（LLM の割り当て・生成テスト・プロジェクト・Shot の生成設定）は、`modelfilter.Default()`（`available_only` がオン）から始め、使えないものを隠す。フィルターの「使えないものも表示」（`available_only` をオフ）で出し、そのとき各モデルの `unavailable_message` と `settings_route`（設定画面への導線）を見せる。「N 件は今は選べません」の件数は、`available_only` をオフにした一覧と `Default()` の一覧の差。**設定画面の API キーのカード（`credentials` の一覧）は今まで通り全プロバイダーを出す**（ここは隠さない。このバックエンドは変えていない）。

### 5.3 今の割り当てが使えないものを指しているとき

- **LLM**: `llmmodels.Service.Warnings(targets)` が、保存した割り当てのうち API キーが無い・ログインしていないプロバイダーを指すものを `Warning`（Agent・役割 primary / fallback・プロバイダー・理由・`Availability.Route`・利用者向けの文）で返す。割り当て画面が出す。保存は拒まない（キーは後から入れられる）。MCP は `get_global_settings` の `llm.route_warnings[]`。
- **画像・動画モデル**: `modelsettings.View.Unusable[]`（`Media`・`Key`（用途か Quality Policy）・`Role`・`Model`・`Availability`）。MCP は `models.unusable_routes[]`。年齢確認で外れたモデルを指す `Gated`（`gated_models`）とは別。
- 可否の判定は 1 か所: 画像・動画は `gentest.Service.Availability`（`modelcatalog.Catalog.availability`: アダプターの理由→追加ノードの不足）を `modelsettings.Service.SetAvailability` に渡す（`internal/app/settings.go`）。LLM は `llmmodels.Service.Availability`（キーの有無・ログインの有無だけを読む。ネットワークは使わない）。

## 6. MCP

[mcp-tools-model-manager.md](../../api/mcp-tools-model-manager.md)（`list_model_catalog`）・[mcp-tools-generation-test.md](../../api/mcp-tools-generation-test.md)（`list_generation_models`）・[mcp-tools-settings-policy.md](../../api/mcp-tools-settings-policy.md)（`get_global_settings`・`list_llm_models`）。引数は §2 の `Condition` の JSON キーそのもの（すべて省略可）。`commercial` を省くと設定「商用OKのモデルだけを使う」に従う（前と同じ）。`available_only` を省くと、MCP は**使えないモデルも理由つきで返す**（プログラムから呼ぶので、`available` で見分けられる。画面の出発点とは違う）。

## 7. 未確認

画面（§8）は、偽のサーバー・Chrome（`npm run test:components`）までの確認で、実機のブラウザ・デスクトップアプリ（Wails）の確認はしていない。実機の GPU・実際の API キー・ChatGPT のログインでの確認はしていない（判定は偽のストア・偽のアダプターのテストまで）。パラメータ数の表の値は、モデルの名前・説明・ベンダーのカードに書いてあるものだけで、ファイルのテンソルから数えた値ではない。

## 8. 画面（Issue #1011 の UI）

モデルを探す・選ぶ画面は、すべて同じ部品を使う。**絞り込みはサーバーが `modelfilter` で行う**（スクリプトは絞らない）: 条件が変わるとバーが §2 のキーのクエリ文字列を送り、サーバーが一覧（カード）を描き直して返す。

### 8.1 部品

| 部品 | 置き場所 | 中身 |
|---|---|---|
| フィルターバー | `molecules/modelpick`（`Bar`・`Panel`・`Summary`・`Oob`・`Filter`・`Facets`） | 検索ボックス（名前・タグ。`q`）と漏斗のボタン。漏斗には絞り込み中の数のバッジ（`Condition.Active`）。漏斗で**詳細フィルターのパネル**が開く（`aria-expanded`。Esc で閉じてフォーカスを漏斗へ戻す）。パネルの項目は画面ごとの `Facets` で出し入れする: 種類・入力・成人向け（すべて / 一般向けのみ / 成人向けのみ）・非検閲・パラメータ数（小・中・大）・16GB で動くか・商用利用・導入済み・ローカル / API・提供元・音声つき・速度・品質・選べるものだけ。変えるたびにその場で一覧が絞られる（ボタンで確定しない）。一覧の上に、かかっている条件を**チップ**で出し（各チップの × で 1 つずつ外す）、「すべてクリア」がある。「N 件」「（全 M 件のうち）」と、選べるものだけがオンのときの「K 件は今は選べません」・「使えないものも表示」 |
| モデルのカード | `molecules/modelcard`（`PickItem`・`Summary`。モデルマネージャーの開くカードは `Item`） | 絵・名前・種類と価格か大きさの 1 行・商用利用のアイコン・バッジ（成人向け・パラメータ数・VRAM・導入済み。`Facts`）。選べないモデルは薄くし、**理由**と設定画面への導線（`Reason.Route`）を下に出す（`aria-describedby`）。選び方は 2 つ: チェックボックス（`PickMany`。生成テストのモデル。複数選べる。枠と光る縁取りと ✓ で示す）、ボタン（`PickOne`。モデル選択のモーダル。いま欄に入っているモデルは枠と面で示す） |
| モデル選択の欄 | `molecules/modelpick.Field` と `Host` | 選んだモデルを**カードの要約**で見せるボタン（「変更」）。押すと `GET /model-picker/:source` が応え、ページに 1 つの `<dialog>`（`Host`）がバーとカードの一覧で開く。カードを押すとそのカードが持つ値（`data-mp-values`）が欄の隠し `<input>`（`data-mp-from`）に入って閉じ、欄に `change` が飛ぶ。背景のクリックでは閉じない（Esc と「閉じる」で閉じる）。設定のモーダルの上に重なる |
| スクリプト | `static/src/components/model-pick` | パネルとダイアログの開閉・選んだモデルを欄へ入れる・選んだカードの印だけ。絞り込みはしない |

### 8.2 画面ごとの条件の記憶

条件は**画面ごとに** Cookie `mam_mf_<キー>`（クエリ文字列。1 年。`HttpOnly`・`SameSite=Lax`）に覚え、条件を持たずに開いたときに読む（`handler/pickfilter`）。localStorage でなく Cookie なのは、デスクトップ版が使うループバックのポートが変わっても共有でき、初回から絞った一覧をサーバーが描けるため。キー: `model-manager`（モデルマネージャーのカタログ）・`gentest-image` / `gentest-video`（生成テストのモデル。メディアごと）・モデル選択の欄は §8.3 の `<source>`。出発点は、モデルを選ぶ画面は `modelfilter.Default()`（選べるものだけ）、モデルマネージャーのカタログはゼロ値（導入前のモデルも並べる）。設定「商用OKのモデルだけを使う」がオンなら、どの画面も**商用の選択だけ**「商用OKのみ」から始まる（パネルの「すべて」は別の選択になる）。

生成テストのフォームは、**条件に合わないモデルでも、フォームが選択済みとして持つモデル**（リンクの `?model=`・「もう一度」・レシピの「使う」・プロンプトマッチの引き継ぎ）のカードは描く（チェック済み）。カードが DOM に無いと POST にも載らず、記憶した条件のせいで選んだモデルが黙って外れるため。選択済みでないモデルは、これまでどおり条件に合うものだけを描く。

設定「Quality Policy」の動画モデルの欄（`settings-video`）の件数（「N 件（全 M 件のうち）」「K 件は今は選べません」）は、カードと同じ集合（`modelsettings.View.VideoModels` にある動画モデル）を**モデル単位**で数える（カードは解像度ごとに出るが、数は解像度の数ではない。生成テストの動画モデルすべてを数えない）。設定「画像」の欄（`settings-image`）と同じ数え方。

### 8.3 モデル選択の欄とそのソース（`GET /model-picker/:source`）

ソースは `handler/modelpicker` が持つ（未知のソースは 404）。一覧・パネルの項目・空のときの文言はソースごと。

| `<source>` | 置き場所 | 選ぶもの（欄が入れる値） |
|---|---|---|
| `match` | 生成テスト「画像から似せる」 | 画像モデル（`model`） |
| `inpaint` | 「一部だけ描き直す」 | 描き直せる画像モデル（`model`。描き直せないモデルは理由つきで出る） |
| `shot-video` | Shot の Video Model（MANUAL・LOCK） | 動画モデル（`value`。保存したワークフローも出る） |
| `model-trial` | モデルマネージャーの試し生成 | 試せるモデル（`model_id`） |
| `settings-image` | 設定「画像・動画モデル」の画像の用途 | `modelsettings.View.ImageModels` にあるモデル（`image_<用途>_<primary or fallback>_model`） |
| `settings-video` | 同 Quality Policy | 動画モデルと解像度（`policy_<方針>_<primary or fallback>`。値は `<モデル>@<解像度>`） |
| `video-enhance` | 生成結果の詳細（動画）「大きくする」「滑らかにする」（Issue #1031・#1011） | その操作（`op=upscale`・`interpolate`）と場所（`cloud=1` は Topaz、無ければこの PC）のモデル（`model`。`modelcatalog.VideoEnhancer.Item`・`Availability`。種類は `video_upscale`・`video_interpolate`。Topaz のキーが無いものは「選べません」で、API キーの設定への導線つき）。パネルは商用利用と選べるものだけ |
| `upscale` | 生成結果の詳細「大きくする」 | アップスケールのワークフロー（`upscale_model`。今は動かせないものは理由つきで出る） |
| `settings-llm` | 設定「テキスト生成（LLM）」の Agent | プロバイダーとモデルを**一緒に**（`<role>.provider`・`<role>.model`）。API キーのないプロバイダー・ログインしていないものは「選べません」で、理由と設定画面への導線つき（§5）。一覧にない名前は `<role>.model_custom` に入れる |

「選べるものだけ」がオン（出発点）のときは、使えないモデルの数を「K 件は今は選べません」で言い、「使えないものも表示」で理由つきで出す。**保存済みの割り当てが使えないモデルを指していても残し**（キーを後で足せる）、割り当て画面（LLM・画像・動画）の上に警告（`llmmodels.Service.Warnings`・`modelsettings.View.Unusable`）を出す。

### 8.4 画面ごとの置き場所

| 画面 | 一覧のリクエスト | 欄 |
|---|---|---|
| モデルマネージャー（カタログ・導入済み） | `GET /model-manager/models`（`mmview.Where`。ジョブ中の導入は条件に関わらず報告する） | — |
| 生成テスト | `GET /generation-test/models?media=image\|video`（`gentest.Service.ModelsWhere`。添えた入力画像を受け付けるモデルを先に並べる） | モデルは一覧のカード（複数選択）。カードの `<li>` は `data-requires-first-frame`（最初のフレームが無いと動かない I2V。#1009）ほか、入力欄・詳細設定のスクリプトが読む `data-*` を持つ。動画の入力欄の通知 `#gt-input-check`（#1009）は、一覧を描き直しても残る。かんたんは一覧を持たず、選んだ 1 つを隠しカードで送る |
| 生成テスト「履歴」 | `GET /generation-test/history`（商用利用の 3 択を `commercial` で送り、サーバーが答えのモデルで絞る） | モデルの `<select>` は履歴にあるモデルの名前の絞り込みで残す（モデルを**選ぶ**のではない） |
| 設定の画像・動画モデル・LLM、Shot の Video Model、「画像から似せる」・「一部だけ描き直す」・試し生成・「大きくする」 | §8.3 | §8.3 |

モデルを探す・選ぶ画面ではない選択（履歴のモデル名の絞り込み、ポーズの分類、倍率、FaceFusion の部品（swapper・detector・enhancer））は `<select>` のままにする。
