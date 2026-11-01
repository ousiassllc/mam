# Model Adapter（M3: Multi-model routing / Audio Director）

[model-adapters.md](model-adapters.md)（§1〜§12）の続きで、M3 の §13 Multi-model routing と §14 Audio Director（M3）の実装を置く。
行数上限（`spec/environment/setup.md` の「Linterly」節）のため分けたもので、節番号と小節番号は model-adapters.md から引き継ぐ。改訂履歴は [model-adapters.changelog.md](model-adapters.changelog.md)。

## 13. Multi-model routing（M3、`internal/agent/generation`）

M3 Issue #26 の仕様。`spec/spec.md` §27 の「Multi-model routing」を、M1 のポリシー別ルーティング（§4、§9.3 の Generation Orchestrator）との差分として定義する。

### 13.1 M1 のポリシー別ルーティングとの違い

| 観点 | M1: ポリシー別ルーティング（§4） | M3: Multi-model routing |
|------|--------------------------------|------------------------|
| 決め方 | Quality Policy（FAST / BALANCED / MAX QUALITY）ごとに固定のプライマリ/代替 | Quality Policy の割り当てを**出発点（アンカー）**にし、Shot の性質・過去の Judge 実績・コストで、Shot ごとに使う動画モデルを入れ替える |
| 入力 | Quality Policy、Shot の長さ、予算残額 | 左に加えて Shot Spec の性質（Product・Character/Acting・Camera Movement・Continuity）、Reference の種類、Project 内の Take の Judge 実績、モデルごとの推定コスト、ベンダーのサーキットブレーカーの状態 |
| 実績の扱い | 使わない | Project 内で Judge 済みの Take の点数・合否を、モデルごとに集計して使う |
| 実績が無いとき | — | アンカーのまま（M1 と完全に同じ結果になる）。実績が十分に溜まったモデルだけが候補として切り替わる |
| 対象 | 動画 | **動画モデル（Take 生成）**。画像は用途別ルート（§3）、LLM は Agent 別ルート（§2）のまま。この 2 つは Take・Judge 実績を持たず、実績を判断基準に使えないため。ブレーカー連動のフェイルオーバー（§7）は従来どおり |

Multi-model routing が動かない場合（ユーザーが Video Model を MANUAL / LOCK で固定した Shot、`routing.enabled` が偽）は M1 の挙動のままである。固定された Shot は、その Video Model だけを使い（M1 と同じ）、決定理由に「ユーザーが固定」と記録する。

### 13.2 候補

Quality Policy ごとに候補（`routing.candidates.<policy>`、動画モデル + 解像度）を設定で持つ。既定は、そのポリシーのプライマリ・代替と、残りの動画モデル 1 つ（`policies` の解像度に近い、対応する最小の解像度）:

- FAST: Kling 3.0 720p / Seedance 2.0 480p / Runway Gen-4.5 720p
- BALANCED: Seedance 2.0 720p / Kling 3.0 720p / Runway Gen-4.5 720p
- MAX QUALITY: Runway Gen-4.5 720p / Kling 3.0 1080p / Seedance 2.0 1080p

### 13.3 判断基準

アンカー = Quality Policy のプライマリ（M1 の能力による入れ替え = Shot が最長クリップを超えるときの入れ替えの後）。アンカー以外の候補（以下「挑戦者」）を、次の 3 つの値（各 0〜1）で比べる。

1. **quality（過去の Judge 実績）**: Project 内で Judge 済みの Take（最新の Judge result があるもの）を、`take.model_name` ごとに集計する。`quality = 0.5 × 軸別平均点の加重和 / 100 + 0.5 × 合格率`。軸は `take.scores` の 4 軸（product / acting / camera / continuity）で、重みは **Shot の性質**から決める（下記）。実績（Judge 済み Take）が `min_samples`（既定 5）に満たないモデルは実績なしとして `prior_quality`（既定 0.75）を使う。
2. **cost（コスト効率）**: `候補の中で最も安い推定コスト / そのモデルの推定コスト`（推定コストは Shot を対応する長さに合わせたクリップ 1 本分。同じ Shot・同じ長さで比べる）。
3. **fit（Shot の性質への適合）**: Shot が要求する能力のうち、モデルが満たす割合。要求は (a) Shot の長さを 1 クリップで収められる、(b) Reference Pack に Keyframe があれば First Frame を取れる（Keyframe のファイルが読めず First Frame が付かないときは、Keyframe を要求として数えない。クリップの長さ・見積りと同じ条件）、(c) Keyframe 以外の参照画像があれば自由な参照画像を取れる。要求が無ければ 1。

Shot の性質による軸の重み: 4 軸とも基準 1 に、Shot Spec に Product があれば product に +2、Character があるか Acting が 1 つ以上あれば acting に +2、`camera.movement` があり `static` でなければ camera に +2、`continuity.start_from_previous_final_state` が真または `lighting.inherit` があれば continuity に +2 を加え、合計が 1 になるよう正規化する。

`score = w_quality × quality + w_cost × cost + w_fit × fit`。重みは Quality Policy ごと（`routing.weights.<policy>`）: FAST は 0.4 / 0.5 / 0.1、BALANCED は 0.6 / 0.3 / 0.1、MAX QUALITY は 0.85 / 0.05 / 0.1（コスト重視の FAST は安さ、品質重視の MAX QUALITY は実績を優先する）。

### 13.4 切り替え条件

挑戦者は、次をすべて満たすときだけアンカーに代わってプライマリになる。

1. Video Model が固定されていない（13.1）。
2. そのモデルのベンダーのブレーカーが OPEN でなく、バックエンド（API キー）がある（13.5）。
3. 実績が `min_samples` 以上ある。
4. 実績の軸別平均点の加重和が、その Project の Minimum acceptance score（Quality/Cost Policy、UC-8）以上（最低限の品質に届かないモデルへは切り替えない）。
5. `score` がアンカーの `score` より `switch_margin`（既定 0.05）以上高い（僅差では動かない）。

複数が満たせば `score` 最大のものを選ぶ。切り替えたときは元のプライマリ（アンカー）を代替にする（Different Model・フェイルオーバーの行き先）。切り替えなければプライマリ/代替は Quality Policy のまま。ここまでで決めたプライマリ/代替に、既存の予算ロジック（Take 数の削減、プライマリが 1 Take も収まらなければ代替、収まらなければ 402）を適用する。

### 13.5 フォールバック・サーキットブレーカーとの関係

- Multi-model routing は「どのモデルを先に使うか」（プライマリ/代替の組）だけを決める。実行時のフェイルオーバー（ブレーカー OPEN・再試行後の失敗で代替へ）、再試行、429 での同時実行数の調整は Model Adapter が従来どおり行い、変えない（§7、§9.3、`non-functional.md` §6）
- ブレーカーが OPEN のベンダーのモデルは、挑戦者として選ばない（呼んでも `ErrCircuitOpen` で失敗するため）。アンカーのブレーカーが OPEN のときは、M1 と同じく Model Adapter が代替へフェイルオーバーする
- Repair Agent の Different Model は、決めたプライマリ/代替の間の切り替えとして動く（元の Take のモデルと別の方へ）

### 13.6 トレーサビリティ（F-27）

決定は `take.model_parameters.routing` に Take ごとに保存する: 方式（`adaptive`: 13.4 で挑戦者に切り替えた / `policy_default`: 比べたが Quality Policy のまま / `fixed`: ユーザーが固定 / `disabled`: 設定で無効）、Quality Policy のプライマリ/代替、選んだプライマリ/代替、理由（1 文）、候補ごとの quality・実績数・平均点・合格率・コスト・cost・fit・score・除外理由（`circuit_open` / `no_backend` / `insufficient_evidence` / `below_minimum_score` / `margin` など）、Shot の性質から決めた軸の重み。実際に使われたモデル（`fell_back`）は従来どおり `model_name` / `fell_back` で分かる。`GET /takes/:id/trace` の「Model routing」に表示する。Take を作らずに失敗したときは Take がないので記録しない（決定理由は `prompt_version.model_adapter_output.reasons` に従来どおり残る）。区間を繋いだ Take（Partial Take Use / Partial Regeneration）は基底 Take のモデルをそのまま使うので、`routing` も基底 Take のものを引き継ぐ。

### 13.7 設定

`--model-config` の JSON に `routing` を追加する（未知のキーはエラー）。

| キー | 内容 | 既定 |
|---|---|---|
| `routing.enabled` | Multi-model routing を有効にする。偽なら M1 のポリシー別ルーティングのみ | 真 |
| `routing.min_samples` | 実績として使うのに必要な Judge 済み Take 数 | 5 |
| `routing.prior_quality` | 実績が足りないモデルの quality（0〜1） | 0.75 |
| `routing.switch_margin` | 切り替えに必要な score の差 | 0.05 |
| `routing.weights.<fast\|balanced\|max_quality>` | `quality` / `cost` / `fit` の重み（負でなく、合計が正） | §13.3 の重み |
| `routing.candidates.<fast\|balanced\|max_quality>` | `model` と `resolution` の配列（各モデルは動画モデルで、`policies` と同じ検証を受ける） | §13.2 の候補 |

## 14. Audio Director（M3）の実装（`internal/agent/audiodirector`）

M3 Issue #25 の仕様。§5 の全機能（TTS・音声クローン / Music / Sound Effects / Lip Sync / Mixing・Loudness）を実装する。

### 14.1 プロバイダーと識別子

| 機能 | 識別子（`cost_ledger_entry.provider`、`audio_timeline` のクリップの `model`） | 呼び出し | 認証（設定画面のキー名） |
|------|------|------|------|
| Dialogue / Narration（TTS） | `elevenlabs-tts` | `POST /v1/text-to-speech/{voice_id}`（モデル `eleven_multilingual_v2`） | `elevenlabs`（ヘッダー `xi-api-key`） |
| 音声クローン | （生成物ではなくコストなし。音声 ID は `voice_clone` に保存） | `POST /v1/voices/add`（multipart。`name` と `files`）。応答の `voice_id` を保存する | `elevenlabs` |
| BGM（Music v2） | `elevenlabs-music` | `POST /v1/music`（`prompt`・`music_length_ms`・`model_id: music_v2`・`force_instrumental: true`）。長さは 3〜600 秒 | `elevenlabs` |
| SE / Foley / Ambience | `elevenlabs-sfx` | `POST /v1/sound-generation`（`text`・`duration_seconds`・`loop`・`model_id: eleven_text_to_sound_v2`）。長さは 0.5〜30 秒 | `elevenlabs` |
| Lip Sync | `sync-labs` | `POST https://api.sync.so/v2/generate`（multipart。`video`・`audio`・`model`）→ `GET /v2/generate/{id}` を `COMPLETED` までポーリングし、`outputUrl` をダウンロードする（ダウンロードには API キーを送らない） | `synclabs`（ヘッダー `x-api-key`） |

- 長さが範囲外の Music / SE はリクエストを送らずに拒否する。429 はレート制限として利用者に伝え、リトライしない。API キーはエラーメッセージに含めない。1 回の呼び出しの時間上限は、ElevenLabs の TTS・SFX・音声クローンが 60 秒、Music が 180 秒、Sync Labs の Lip Sync がアップロードからダウンロードまで 10 分（状態確認の間隔 2 秒）で、超えるとその呼び出しは失敗し、層は `skipped` に残る。生成した音声（TTS・Music・SFX の応答）は 32 MiB まで、Lip Sync の出力動画は 512 MiB までで、空または超えるものは失敗として扱う
- 各ベンダーの呼び出しはインターフェース（`Synthesizer` / `Composer` / `SoundMaker` / `VoiceCloner` / `LipSyncer`）の裏にあり、`audiodirector.Providers` で束ねて `service.Deps.Audio` に渡す。未設定のプロバイダーの層は作らず、Audio Timeline の `skipped` に残す（偽の音は作らない）

### 14.2 Audio Timeline の生成（Scene Emotion と編集テンポ）

1. **Scene の読み取り**: Edit Timeline のクリップを Scene ごとにまとめ、編集後の動画上の開始・終了、クリップ数、Scene の場所・目的・脚本の内容・Shot の目的を集める。編集テンポは Scene 内のクリップの平均の長さから決める（2.5 秒未満 `fast`、5 秒超 `slow`、それ以外 `medium`）
2. **プラン（Planner）**: Reasoning LLM（`llm.AgentAudioDirector`）が Scene ごとに Scene Emotion（感情）・BGM のプロンプト・Ambience のプロンプト・SE / Foley の置き場所（Scene 先頭からの秒数・長さ・プロンプト）を JSON で返す。Scene Emotion は 40 文字（rune）に切り詰めて Audio Timeline の `scenes[].emotion` に保存する。Quality Policy の範囲外の層、Scene の外・長さ 0.5 秒未満・最大 4 個を超える効果音は捨てる。採用する効果音の長さは、LLM が返した長さを 6 秒（Planner 固有の上限。§14.1 の SFX の上限 30 秒とは別）と Scene の終わりまでの残りのうち短い方に切り詰める（生成もコストの見積もりもこの長さで行う）。捨てるのは LLM が返した長さが 0.5 秒未満のものと、切り詰めた結果が 0.5 秒未満になるもの（Scene の終わりの直前に置かれたもの）で、0.5 秒未満の効果音は引き延ばさない。LLM が未設定のときは規則で作る（BGM はテンポとブリーフの mood、Ambience は Scene の場所から。効果音は置かない）。LLM の出力が使えない（Scene を 1 つも指していない等）ときは **プロバイダーへの支払いの前に** エラーにする
3. **層ごとの生成**: Dialogue / Narration は脚本の行を順に話し、行は Scene の開始位置か直前の行の直後（遅い方）に置く（行の間 0.3 秒）。話者に音声クローンがあれば Dialogue の行はその声で話す（Narration は既定の声）。その後、BGM（Scene の長さ）→ Ambience（Scene の長さ。ループ）→ SE → Foley の順に作る。この順は予算が足りないときの優先順位でもある
4. **ミキシング・ラウドネス**: Take 自体の音声（あれば）と全クリップをローカルの ffmpeg で 1 本にする。BGM・Ambience は Scene の両端で 0.5 秒フェードし、層ごとの音量は Dialogue / Narration 0 dB、BGM −16 dB、Ambience −22 dB、SE −8 dB、Foley −12 dB（ファイルに対して）。そのうえで §12 と同じ 2 パスの `loudnorm` で −14 LUFS / −1.5 dBTP に揃え、測定値を保存する（外部 API は呼ばない）。セリフも Take の音声も足す音もなければ無音とし、ミックスは作らない

### 14.3 Quality Policy が決める層

Dialogue と Narration は常に作る。

| Quality Policy | 足す層 | Lip Sync |
|------|------|------|
| FAST | BGM | なし |
| BALANCED | BGM・Ambience・SE | なし |
| MAX QUALITY | BGM・Ambience・SE・Foley | あり（Sync Labs `sync-3`） |

ポリシーが作らない層は `skipped`（`reason: policy`）に残す。

### 14.4 Lip Sync

MAX QUALITY のみ。話者付きの Dialogue の行（Narration は対象外）が入っているクリップごとに、そのクリップの Take の該当範囲を切り出した動画と、そのクリップの行を本来の時刻に置いた音声（ffmpeg で作る）を Sync Labs に渡し、返った動画を `<project_id>/audio/<edit_timeline_id>/<run>/lipsync-NN.mp4`（assets ディレクトリ直下からの相対パス）に保存して Audio Timeline の `lip_sync` に記録する。書き出し（Export）はそのクリップの映像を Take の代わりにこの動画から切り出す。行がクリップをまたぐ・どのクリップにも入らない場合と、失敗した場合は、そのクリップの Lip Sync だけ諦めて `skipped` に残す（音声は作る。Sync Labs が受け付けたあとの失敗は見積を台帳に残す。§14.5）。クリップに入っているかの判定には 0.1 秒の許容幅を持つ（行の開始がクリップの開始の 0.1 秒前以降でクリップの終わりより前、かつ行の終わりがクリップの終わりの 0.1 秒後までなら、そのクリップに入っていると見なす）。Sync Labs のキーがなければ、残りのクリップも試さない。

### 14.5 コストと予算（F-22）

- 料金はプロバイダー呼び出しごとの **見積** で `cost_ledger_entry`（`provider` は §14.1 の識別子）に記録する。単価の既定は TTS 1,000 文字あたり $0.30、Music 1 秒あたり $0.02、SFX 1 秒あたり $0.01、Lip Sync は Sync Labs の公開価格（25fps）の 1 秒あたり `lipsync-1.9.0-beta` $0.02・`lipsync-2` $0.04・`sync-3` $0.133。ベンダーの課金は plan・クレジットで実額と異なりうるため、予算管理のための見積である（`actual_cost` は空）。失敗した呼び出しは記録しない。ただし Sync Labs が受け付けたあとに失敗した Lip Sync（受理後の成果物欠如・ダウンロード失敗・ポーリング失敗・タイムアウト・キャンセル。`audiodirector.ChargedError`）は、課金が発生しているので、そのクリップは skipped にしつつ見積を台帳に残す（受理前の失敗と Sync Labs が FAILED / REJECTED を返した場合は記録しない）
- Project の予算上限（`project.project_budget_ceiling`）から、台帳の合計（`COALESCE(actual_cost, estimated_cost)` の和）と、予約中の見積（同じ Project の他の音声の実行と、画像・動画の生成が確保している額）を引いた残りが、各呼び出しの見積に足りなければその呼び出しは行わず `skipped`（`reason: budget`）に残す。見積は呼び出しの前に予算上限に対して予約し（`CostRepository.Reserve`。残額の確認と予約は 1 つの不可分な操作）、台帳に記録したあと（呼び出しが課金されなかったときはその時点）で解放する。このため同じ Project で同時に動く実行が、合わせて予算上限を超えることはない。セリフの音声合成の見積は、実行の開始時に合計額を一度に予約し、全行を台帳に記録したあとで解放する。その合計が残りを超えるときは、何も呼ばずに 409（`BudgetError`）にする
- Audio Timeline の `estimated_cost` にその実行の見積合計を保存する

### 14.6 M1 の簡易音声からの移行と Branch ごとの版管理

- M1 の簡易音声（`mode` なし、セリフの行だけのクリップ）の Audio Timeline は **移行せずそのまま読める**。`clips` の `layer` が空のクリップは話者の種別（`kind`）から Dialogue / Narration として扱い、Final QC・Export・画面表示は従来どおり動く。スキーマの変更は `branch_id`（#18 で追加済み）のみで、既存データの書き換えは行わない
- `POST /projects/:id/audio/generate` は、以後は常に Audio Director で作り、`mode: "director"` の版を追記する。M1 の簡易版だけを作る経路は残さない（FAST は「セリフ + BGM」まで）
- Audio Timeline の版は `(project_id, branch_id)` ごとに `version_number` を 1 から振り、追記のみ（Edit Timeline と同じ）。Branch の音声は、その Branch の最新の Edit Timeline を元に、Branch 自身の系列へ保存する。Final QC が検査するのは主系列トランクの最新版で、書き出し（`POST /projects/:id/export`）は `branch_id`（省略時=主系列トランク）で指定した Branch の最新の Edit Timeline と Audio Timeline を使う

### 14.7 音声クローン

`POST /projects/:id/audio/voices`（multipart。`speaker`・`sample`（音声ファイル、最大 10 MB）・任意の `name`）で、サンプル音声から ElevenLabs の声を作り、`voice_clone`（Project × 話者で 1 件。再クローンは置き換え）に保存する。以後の音声生成でその話者の Dialogue の行はこの声で話す。クローンはアカウントの声として ElevenLabs 側に作られ、Project の削除では消さない（ベンダー側の削除は設定画面で行う）。
