# モデルのフィルターと「選べる状態」（Issue #1011・#1013 のバックエンド）

モデルを探す・選ぶ画面（モデルマネージャーのカタログ・導入済み、生成テスト、プロジェクト・Shot の生成設定、LLM の割り当て、参照画像の役割ごとのモデル選択）と、MCP の `list_model_catalog`・`list_generation_models` が**同じ条件・同じ言葉**で絞り込むための土台。画面（フィルターパネル・モデル選択のモーダル・カード・バッジ）は後続の UI 作業が、ここに書く API を使って作る。この文書は「UI 作業者が使う API」の正本。

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
| `kinds` | `image` / `video` / `image_edit` / `upscale` / `llm` / `audio` の一覧 | 種類（**どれか**）。`image_edit` は画像モデルのうち入力に `edit` か `inpaint` を持つもの（画像モデルは `image` でもある） |
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

画面（フィルターパネル・モデル選択のモーダル・URL への保存）は未実装（後続の UI 作業）。実機の GPU・実際の API キー・ChatGPT のログインでの確認はしていない（判定は偽のストア・偽のアダプターのテストまで）。パラメータ数の表の値は、モデルの名前・説明・ベンダーのカードに書いてあるものだけで、ファイルのテンソルから数えた値ではない。
