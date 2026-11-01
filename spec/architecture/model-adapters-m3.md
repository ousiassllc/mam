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
3. **fit（Shot の性質への適合）**: Shot が要求する能力のうち、モデルが満たす割合。要求は (a) Shot の長さを 1 クリップで収められる、(b) Reference Pack に Keyframe があれば First Frame を取れる、(c) Keyframe 以外の参照画像があれば自由な参照画像を取れる。要求が無ければ 1。

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
| Dialogue / Narration（TTS、ローカル） | `gpt-sovits-tts`（API 費用 0 円。台帳には 0 円で記録する） | mam が導入・起動する GPT-SoVITS の `api_v2.py`（[local-runtime.md](model-manager/local-runtime.md)）への `POST /tts`（§14.8）。実行ごとに `elevenlabs`（既定）と選ぶ | なし（`127.0.0.1` のみ。キーは使わない） |
| 音声クローン | （生成物ではなくコストなし。音声 ID は `voice_clone` に保存） | `POST /v1/voices/add`（multipart。`name` と `files`）。応答の `voice_id` を保存する | `elevenlabs` |
| BGM（Music v2） | `elevenlabs-music` | `POST /v1/music`（`prompt`・`music_length_ms`・`model_id: music_v2`・`force_instrumental: true`）。長さは 3〜600 秒 | `elevenlabs` |
| SE / Foley / Ambience | `elevenlabs-sfx` | `POST /v1/sound-generation`（`text`・`duration_seconds`・`loop`・`model_id: eleven_text_to_sound_v2`）。長さは 0.5〜30 秒 | `elevenlabs` |
| Lip Sync | `sync-labs` | `POST https://api.sync.so/v2/generate`（multipart。`video`・`audio`・`model`）→ `GET /v2/generate/{id}` を `COMPLETED` までポーリングし、`outputUrl` をダウンロードする（ダウンロードには API キーを送らない） | `synclabs`（ヘッダー `x-api-key`） |
| Lip Sync（ローカル） | `musetalk-1.5`（API 費用 0 円。台帳には 0 円で記録する） | mam が導入・起動する MuseTalk 1.5 の口パクの入口（`localruntime.Service.LipSync`。[local-runtime.md §13.3](model-manager/local-runtime.md)）への `POST /v1/lipsync`（§14.9）。実行ごとに `synclabs`（既定）と選ぶ | なし（`127.0.0.1` のみ。キーは使わない） |

- 長さが範囲外の Music / SE はリクエストを送らずに拒否する。429 はレート制限として利用者に伝え、リトライしない。API キーはエラーメッセージに含めない。1 回の呼び出しの時間上限は、ElevenLabs の TTS・SFX・音声クローンが 60 秒、Music が 180 秒、Sync Labs の Lip Sync がアップロードからダウンロードまで 10 分（状態確認の間隔 2 秒）で、超えるとその呼び出しは失敗し、層は `skipped` に残る。生成した音声（TTS・Music・SFX の応答）は 32 MiB まで、Lip Sync の出力動画は 512 MiB までで、空または超えるものは失敗として扱う
- 各ベンダーの呼び出しはインターフェース（`Synthesizer` / `LocalSynthesizer` / `Composer` / `SoundMaker` / `VoiceCloner` / `LipSyncer` / `LocalLipSyncer`）の裏にあり、`audiodirector.Providers` で束ねて `service.Deps.Audio` に渡す。未設定のプロバイダーの層は作らず、Audio Timeline の `skipped` に残す（偽の音は作らない）

### 14.2 Audio Timeline の生成（Scene Emotion と編集テンポ）

1. **Scene の読み取り**: Edit Timeline のクリップを Scene ごとにまとめ、編集後の動画上の開始・終了、クリップ数、Scene の場所・目的・脚本の内容・Shot の目的を集める。編集テンポは Scene 内のクリップの平均の長さから決める（2.5 秒未満 `fast`、5 秒超 `slow`、それ以外 `medium`）
2. **プラン（Planner）**: Reasoning LLM（`llm.AgentAudioDirector`）が Scene ごとに Scene Emotion（感情）・BGM のプロンプト・Ambience のプロンプト・SE / Foley の置き場所（Scene 先頭からの秒数・長さ・プロンプト）を JSON で返す。Scene Emotion は 40 文字（rune）に切り詰めて Audio Timeline の `scenes[].emotion` に保存する。Quality Policy の範囲外の層、Scene の外・長さ 0.5 秒未満・最大 4 個を超える効果音は捨てる。採用する効果音の長さは、LLM が返した長さを 6 秒（Planner 固有の上限。§14.1 の SFX の上限 30 秒とは別）と Scene の終わりまでの残りのうち短い方に切り詰める（生成もコストの見積もりもこの長さで行う）。捨てるのは LLM が返した長さが 0.5 秒未満のものと、切り詰めた結果が 0.5 秒未満になるもの（Scene の終わりの直前に置かれたもの）で、0.5 秒未満の効果音は引き延ばさない。LLM が未設定のときは規則で作る（BGM はテンポとブリーフの mood、Ambience は Scene の場所から。効果音は置かない）。LLM の出力が使えない（Scene を 1 つも指していない等）ときは **プロバイダーへの支払いの前に** エラーにする
3. **層ごとの生成**: Dialogue / Narration は脚本の行を順に話し、行は Scene の開始位置か直前の行の直後（遅い方）に置く（行の間 0.3 秒）。話者に音声クローンがあれば Dialogue の行はその声で話す（Narration は既定の声）。実行がローカルの声（§14.8）を選んだときは、行の話者から Character の参照音声を引き当てて、その声で話す（ElevenLabs の音声クローンは使わない）。その後、BGM（Scene の長さ）→ Ambience（Scene の長さ。ループ）→ SE → Foley の順に作る。この順は予算が足りないときの優先順位でもある
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

**ローカルの Lip Sync（MuseTalk 1.5）**: 実行が `LipSyncProvider: local` を選んだときは、上の Sync Labs の代わりに MuseTalk 1.5（§14.9）が同じ入力（切り出した動画・行を置いた wav）から口元を合わせた mp4 を作り、**保存先・`lip_sync` の記録・Export の使い方は同じ**（モデル名だけ `musetalk-1.5`）。範囲（MAX QUALITY のみ・話者付きの Dialogue の行があるクリップだけ・行がクリップをまたぐクリップは諦める）も同じで、ローカルでも変えない。止まり方の規則: Sync Labs では API キーが無いと残りのクリップも試さないのと同じく、ローカルでは**ランタイム全体が使えない（未導入・重み未取得・この PC で動かない）ことを、最初のクリップの前に 1 回だけ確かめて `skipped`（`reason: error`・`prompt` は空）に 1 件だけ残し、どのクリップも試さない**（途中でそうなったときも、最初の失敗で残りを止める）。クリップごとの失敗（顔が見つからない・ビデオメモリ不足・時間切れ・空の応答・動画が大きすぎる・サーバーが応答しない）は、そのクリップだけ `skipped` に理由つきで残して次のクリップへ進む。音声は常に作る

### 14.5 コストと予算（F-22）

- 料金はプロバイダー呼び出しごとの **見積** で `cost_ledger_entry`（`provider` は §14.1 の識別子）に記録する。単価の既定は TTS 1,000 文字あたり $0.30、Music 1 秒あたり $0.02、SFX 1 秒あたり $0.01、Lip Sync は Sync Labs の公開価格（25fps）の 1 秒あたり `lipsync-1.9.0-beta` $0.02・`lipsync-2` $0.04・`sync-3` $0.133。ベンダーの課金は plan・クレジットで実額と異なりうるため、予算管理のための見積である（`actual_cost` は空）。失敗した呼び出しは記録しない。ただし Sync Labs が受け付けたあとに失敗した Lip Sync（受理後の成果物欠如・ダウンロード失敗・ポーリング失敗・タイムアウト・キャンセル。`audiodirector.ChargedError`）は、課金が発生しているので、そのクリップは skipped にしつつ見積を台帳に残す（受理前の失敗と Sync Labs が FAILED / REJECTED を返した場合は記録しない）
- Project の予算上限（`project.project_budget_ceiling`）から、台帳の合計（`COALESCE(actual_cost, estimated_cost)` の和）と、予約中の見積（同じ Project の他の音声の実行と、画像・動画の生成が確保している額）を引いた残りが、各呼び出しの見積に足りなければその呼び出しは行わず `skipped`（`reason: budget`）に残す。見積は呼び出しの前に予算上限に対して予約し（`CostRepository.Reserve`。残額の確認と予約は 1 つの不可分な操作）、台帳に記録したあと（呼び出しが課金されなかったときはその時点）で解放する。このため同じ Project で同時に動く実行が、合わせて予算上限を超えることはない。セリフの音声合成の見積は、実行の開始時に合計額を一度に予約し、全行を台帳に記録したあとで解放する。その合計が残りを超えるときは、何も呼ばずに 409（`BudgetError`）にする
- Audio Timeline の `estimated_cost` にその実行の見積合計を保存する
- ローカルの声（`gpt-sovits-tts`）は API 費用が 0 円で、1 行ごとに `estimated_cost = 0` の台帳エントリを残す（何で作った行かを追えるようにするため）。セリフの見積は 0 円なので予算の予約に加わらず、予算上限が小さくても止まらない（BGM などの層の予算判定は従来どおり）。`elevenlabs-tts` の単価・見積・記録は変えない
- ローカルの Lip Sync（`musetalk-1.5`）も API 費用が 0 円で、**作れたクリップごとに** `estimated_cost = 0` の台帳エントリを残す（作れなかったクリップは記録しない。ローカルは「受理後の課金」が無いので `ChargedError` を使わない）。クリップの見積は 0 円なので予算の予約に加わらず、Sync Labs の見積（`sync-3` で 4 秒のクリップ約 $0.53）が残り予算に収まらないときでも止まらない。`sync-labs` の単価表・見積・記録は変えない

### 14.6 M1 の簡易音声からの移行と Branch ごとの版管理

- M1 の簡易音声（`mode` なし、セリフの行だけのクリップ）の Audio Timeline は **移行せずそのまま読める**。`clips` の `layer` が空のクリップは話者の種別（`kind`）から Dialogue / Narration として扱い、Final QC・Export・画面表示は従来どおり動く。スキーマの変更は `branch_id`（#18 で追加済み）のみで、既存データの書き換えは行わない
- `POST /projects/:id/audio/generate` は、以後は常に Audio Director で作り、`mode: "director"` の版を追記する。M1 の簡易版だけを作る経路は残さない（FAST は「セリフ + BGM」まで）
- Audio Timeline の版は `(project_id, branch_id)` ごとに `version_number` を 1 から振り、追記のみ（Edit Timeline と同じ）。Branch の音声は、その Branch の最新の Edit Timeline を元に、Branch 自身の系列へ保存する。Final QC が検査するのは主系列トランクの最新版で、書き出し（`POST /projects/:id/export`）は `branch_id`（省略時=主系列トランク）で指定した Branch の最新の Edit Timeline と Audio Timeline を使う

### 14.7 音声クローン

`POST /projects/:id/audio/voices`（multipart。`speaker`・`sample`（音声ファイル、最大 10 MB）・任意の `name`）で、サンプル音声から ElevenLabs の声を作り、`voice_clone`（Project × 話者で 1 件。再クローンは置き換え）に保存する。以後の音声生成でその話者の Dialogue の行はこの声で話す。クローンはアカウントの声として ElevenLabs 側に作られ、Project の削除では消さない（ベンダー側の削除は設定画面で行う）。

### 14.8 ローカルの声（GPT-SoVITS）（Issue #922）

Dialogue / Narration を、クラウドの ElevenLabs の代わりに、この PC の GPT-SoVITS（[local-runtime.md](model-manager/local-runtime.md) が導入・起動・停止を管理する `api_v2.py`）で作る。**この節の範囲はプロバイダーの実装と、1 回の実行ごとの選択の入口まで**。設定画面・MCP での選択の保存、クラウドへのフォールバック、利用者向けの文言は [§14.10](#1410-プロバイダーの選択クラウドへのフォールバック利用者向けのエラー文issue-924)（#924）。重み・依存の導入は #918・#920、キャラクターの参照音声は #919。

- **選択の入口**: `audiodirector.Input.VoiceProvider`（`elevenlabs` / `local`。未指定は `elevenlabs`）。`service.EditorService.GenerateAudioWith(ctx, projectID, branchID, AudioOptions{VoiceProvider})` が渡し、`GenerateAudio` は `elevenlabs` の実行（従来どおり）。知らない値は `ValidationError`（`voice_provider`）で、課金も起動もしない。HTTP・MCP の引数には出さない（`GenerateAudio` は保存した設定を読んで `GenerateAudioWith` に渡す。§14.10）。`audiodirector.Providers.LocalSpeech`（`LocalSynthesizer`）が実装で、`app` が `gptsovits.Client` を配線する。ランタイムが未導入でも mam の起動は妨げない（選ばれたときに種類つきのエラーで知らせる）
- **話者 → 参照音声**: `GenerateAudioWith` が `localspeech.Voices` で、脚本の行の話者ごとに `voicerefsvc.ForSpeaker(project, branch, 話者名)` を引き、`Input.LocalVoices`（話者名 → `localvoice.Voice{Path, Text, Language, Cause}`）に渡す。Narration は「ナレーション」という名前の Character の参照音声（`voiceref.NarrationName`）。Series の Episode は正本から継承したコピーを持つので、同じキャラの 2 つの Episode は**同じファイル**で呼ばれる。Branch は Branch の Character から引く。`ElevenLabs` の `Input.Voices`（音声クローンの `voice_id`）とは別の項目で、ローカルの実行では Dialogue の行に載せない
- **事前の検査（課金・起動の前）**: ローカルの実行は、Planner の LLM 呼び出し・予算の予約・ランタイムの起動より前に、(1) 行のある話者**全員**に使える参照音声があること（無い話者は `*localvoice.MissingVoiceError` に、話者名と理由（`voicerefsvc.ErrNoCharacter` / `ErrNoReference` / `ErrAmbiguous` / `ErrFileMissing`）をすべて並べて返す。黙って別の声にしない）、(2) プロバイダーがあること、(3) ランタイムが動くこと（`Prepare`: 停止中なら `localruntime.Service.Prepare` で起動して応答を待つ）を確かめる。行が 1 つも無い脚本はどれも要らない
- **`POST /tts` の呼び方**（GPT-SoVITS 公式タグ `20250606v2pro` の `api_v2.py` と `TTS_infer_pack/TTS.py` のソースを読んで確かめた）: 1 行ごとに JSON で `text`（行の本文）、`text_lang`（既定 `ja`）、`ref_audio_path`（参照音声の絶対パス。サーバーは同じ PC のファイルを自分で読む）、`prompt_text`（参照音声の書き起こし `reference_text`）、`prompt_lang`（参照音声の言語。空は `ja`）、`text_split_method: cut5`、`media_type: wav`、`streaming_mode: false` を送り、wav を `Speech{Ext: "wav"}` で受ける
- **書き起こしが空のとき**: `api_v2.py` の `prompt_text` は省略可で、空だと `TTS.run` が「プロンプトテキスト無し」の経路（参照音声の意味トークンを使わずに合成する）に入る。mam が入れる既定の設定は v2（`custom`: `version: v2`）で、この経路を受け付ける（v3 / v4 系は `NO_PROMPT_ERROR` で失敗するが、mam は使わない）。そこで**空のまま `prompt_text: ""` を送って合成する**（`prompt_lang` は `api_v2.py` が必須とするので参照音声の言語を送る）。声の再現度は書き起こしがあるより下がりうるので、Character の画面で書き起こしを入れることを勧める（#919 の表示）。「書き起こしが無いと合成しない」とはしない。参照音声の長さは 3〜10 秒（`api_v2` の範囲外エラーは HTTP 400 として返る）
- **上限・失敗**: 1 行の時間上限は 3 分（超えると種類 `timeout`）。応答は 32 MiB まで（ElevenLabs と同じ）。空の応答、wav（`RIFF…WAVE`）でない応答、HTTP 400 などの 2xx 以外は失敗（`audiodirector.ProviderError{Vendor: "gpt-sovits"}`）。HTTP エラーの本文（`{"message": "tts failed", "Exception": "…"}`）の先頭は Status とともに残す。サーバーは GPU を 1 つ使うので、行は直列に作る（`speak` は元から直列で、`Client` も同時に 1 行までしか送らない）
- **種類つきのエラー**（`*localvoice.UnavailableError{Kind, Detail, Err}`。画面・MCP・チャット・ジョブの失敗理由は、この `Kind` から **同じ文**（`localvoice.Reason`。§14.10）を作る）:

| Kind | 意味 | 出どころ |
|---|---|---|
| `not_installed` | ランタイムが未導入、導入の最中、プロバイダーが未配線 | `localruntime.ErrNotInstalled` / `ErrBusy` / `ErrNotFound` |
| `weights_missing` | 重みが未ダウンロード・未検証 | `localruntime.ErrWeightsMissing`（`*WeightsError` が不足の名前を持つ） |
| `unsupported` | この環境では動かない（macOS・Linux・非 x64・NVIDIA の GPU 無し）。理由と代わりの案内は #918 の文 | `*localruntime.SupportError` |
| `gpu_memory` | ビデオメモリが足りない（導入時の判定 `low_vram`、または 1 行の合成中の CUDA の `out of memory`） | `SupportError`（`CodeLowVRAM`）、`api_v2` の応答の本文 |
| `process` | サーバーが起動しない・応答しない・途中で落ちた | 起動の失敗、接続の失敗 |
| `timeout` | 起動が間に合わない、または 1 行が上限を超えた | 起動の `context.DeadlineExceeded`、1 行の上限 |

  呼び出し元が自分でやめた（`context.Canceled`）ときは、ランタイムの失敗にしない。`service.audioError` は `UnavailableError`・`MissingVoiceError`（話者名つきの文）を `AIError`（HTTP 502）にする
- **ledger とクリップ**: `AudioClip.Model` と `cost_ledger_entry.provider` は `gpt-sovits-tts`（§14.5）。ミックス・ラウドネスは ElevenLabs の行と同じ（wav も ffmpeg が読む）
- **未確認（実機）**: 実物の GPT-SoVITS・Windows + NVIDIA での合成、日本語の質（ElevenLabs との聞き比べ）、書き起こしが空のときの声の再現度、CUDA の `out of memory` の実際のメッセージ文字列、1 行あたりの時間は**未確認**（この開発環境は Linux で、`api_v2.py` のソースの読み取りと、`httptest` の偽のサーバーでの確認だけをした）。日本語の質の数値は出さない。親 Issue #912 のとおり実装後に実機で聞き比べる

### 14.9 ローカルの Lip Sync（MuseTalk 1.5）（Issue #923）

Lip Sync を、クラウドの Sync Labs の代わりに、この PC の MuseTalk 1.5（[local-runtime.md](model-manager/local-runtime.md) §13 が導入・起動・停止を管理し、`modelmgr/lipsync` が口パクの入口を定める）で作る。**この節の範囲はプロバイダーの実装と、1 回の実行ごとの選択の入口まで**（§14.8 の声と同じ切り方）。設定画面・MCP での選択の保存、クラウドへのフォールバック、利用者向けの文言は §14.10（#924）。

- **選択の入口**: `audiodirector.Input.LipSyncProvider`（`synclabs` / `local`。未指定は `synclabs`＝従来どおり。型は `localvoice.LipSyncProvider`）。`service.EditorService.GenerateAudioWith(ctx, projectID, branchID, AudioOptions{VoiceProvider, LipSyncProvider})` が渡し、`GenerateAudio` は両方とも未指定の実行。知らない値は `ValidationError`（`lip_sync_provider`。声の `voice_provider` と同じ検査）で、課金も起動もしない（`Produce` に直接渡されたときは `localvoice.ErrUnknownLipSyncProvider`）。HTTP・MCP の引数には出さない（`GenerateAudio` は保存した設定を読んで `GenerateAudioWith` に渡す。§14.10）。`audiodirector.Providers.LocalLipSync`（`LocalLipSyncer` ＝ `Prepare(ctx)` ＋ `LipSyncer`）が実装で、`app` が `musetalk.Client`（`audiodirector/musetalk`）を `service/locallipsync.Runner`（ランタイム管理への接続）つきで配線する。ランタイムが未導入でも mam の起動は妨げない（選ばれたときに知らせる）。`Providers.LocalLipSync` が無いときは、`skipped` に「ローカルの口パクのプロバイダーが設定されていません」と 1 件残し、**Sync Labs に黙って切り替えない**
- **呼び出し**: クリップごとに、Director が ffmpeg で作った動画（`Compose`）と音声（`PlaceVoices`）の bytes を `LipSyncRequest`（`Seconds` に動画の長さを足した）で渡す。`musetalk.Client.Sync` は、それを**一時ディレクトリのファイルにして**（ランタイムは同じ PC のファイルを絶対パスで読む。動画を HTTP で送らない）`localruntime.Service.LipSync(lipsync.Request{video_path, audio_path, output_path, ffmpeg_path})` を呼び、出力の mp4 を読んで返す。`ffmpeg_path` は mam が探した ffmpeg（`media.Tools.FFmpegPath()`。ランタイムは ffmpeg を持たない）。呼び出しの前に、Director が `Prepare`（停止中のランタイムを起動して `/health` を待つ。`localruntime.Service.Prepare`）を **MAX QUALITY で対象のクリップがあるときだけ、実行ごとに 1 回**呼ぶ。GPU は 1 つなので `Service.LipSync` が 1 件ずつ順番に実行する（Director もクリップを順に作る）
- **入力の整え方**: **mam は動画・音声を整えない**。MuseTalk のラッパー（§13.3）が、動画のフレームレートを自分で読み（全フレームを取り出す。画像だけ `fps` 省略で 25）、音声を 16 kHz のモノラルに自分で変換し、顔のあるフレームだけを使い、出力は音声の長さに揃える。動画の解像度の前提は無い（フレームがメモリ 6 GB に収まれば良く、収まらないと `too_large`）。そのため Director の入力（Take の該当範囲を切り出した H.264 の mp4 ＋ 行を置いた wav）をそのまま渡す
- **上限**: 渡す動画は 512 MiB まで、音声は 128 MiB まで、出力の mp4 は 512 MiB まで（Sync Labs の出力の上限と同じ）。超える入力はランタイムに渡さず、超える出力・空の出力・無い出力は失敗。**1 クリップの時間上限は動画の長さに比例**（GPU の速さが機種で大きく違うため、固定の秒数にしない）: 3 分 ＋ 動画 1 秒あたり 30 秒、最大 30 分（Config で変えられる）。超えると種類 `timeout`。呼び出し元が自分でやめた（`context.Canceled`）ときは、ランタイムの失敗にしない
- **種類つきのエラー**（`*localvoice.UnavailableError{Kind, Vendor: "musetalk", Detail, Err}`。型は §14.8 の 1 か所だけを共有し、`Vendor` を足して声と口パクを見分ける。`Systemic()` は ランタイム全体が使えない種類＝`not_installed`・`weights_missing`・`unsupported`。画面・MCP・チャットは `Kind` から同じ文（`localvoice.Reason`。§14.10）を作り、口パクでは `localvoice.LipSyncReason` が `skipped[].detail` に入れる）:

| Kind | 意味 | 出どころ |
|---|---|---|
| `not_installed` / `weights_missing` / `unsupported` / `gpu_memory`（導入時の判定） / `process` / `timeout` | §14.8 と同じ。出どころはランタイム管理（`localspeech.UnavailableFor`）。macOS・Linux・非 NVIDIA・RTX 50 では `unsupported` で、理由と Sync Labs の案内は [local-runtime.md §13.5](model-manager/local-runtime.md) の文がそのまま `Detail` に入る | `localruntime.Service.Prepare` / `LipSync` の起動前の拒否 |
| `no_face` | 動画に顔が見つからない（そのクリップだけの事実。`Systemic` ではない） | サーバーの `no_face` |
| `gpu_memory` | 1 クリップの生成中にビデオメモリが足りない（次の短いクリップは通りうる。`Systemic` ではない） | サーバーの `out_of_memory` |
| `too_large` | 動画がメモリに収まらない・入出力が上限を超えた | サーバーの `too_large`、上限の検査 |
| `process` | サーバーが応答しない・途中で落ちた・空の応答・それ以外の失敗 | `ErrUnreachable`・`internal` ほか |

  `skipped[].detail`（利用者向けの文）には、`Kind` ごとの文にサーバーの文（`lipsync.Error.Message`。日本語でそのまま表示してよい）か理由を括弧で添える。`ChargedError` は使わない
- **ledger とクリップ**: `LipSyncClip.Model` と `cost_ledger_entry.provider` は `musetalk-1.5`（`domain.ModelMuseTalk`。§14.5）。出力は `lipsync-NN.mp4`（§14.4 と同じ場所）。書き出しは変えない（ファイルが無いと Sync Labs のときと同じ `conflict` で止まる。サービスのテストで固定）
- **テスト**: 偽のランタイムの入口（`musetalk.Runner` の偽物。実物の Python は使わない）に対して、Director → `musetalk.Client` → 偽の入口を通して、全クリップの記録・0 円の台帳・予算・Sync Labs が既定のままであること・クリップごとの失敗と時間切れ・ランタイム全体の失敗が 1 件だけ残ること・途中でランタイムが使えなくなったときに残りを試さないこと・キャンセルを失敗にしないこと・MAX QUALITY 以外で起動しないことを確かめる。`EditorService` では、`lip_sync` の記録・台帳・書き出しがそのファイルを使うこと（消すと書き出しが止まる）を確かめる
- **未確認（実機）**: 実物の MuseTalk 1.5・Windows + NVIDIA での口パクの生成、**口パクの質・日本語の音声での出来**、1 クリップあたりの時間（上の時間上限の 3 分 ＋ 1 秒あたり 30 秒は、公式の「4 GB でも動く」ことと、Linux の CPU で 1.2 秒のクリップに約 95 秒かかったこと（[local-runtime.md §13.7](model-manager/local-runtime.md)）からの見積で、実機で確かめていない）、実物のランタイムを通した Audio Director の実行は**未確認**（この開発環境は Linux で GPU が無く、偽の入口に対するテストだけをした）。親 Issue #912 のとおり、日本語の質は実装後に実機で聞き比べる

### 14.10 プロバイダーの選択・クラウドへのフォールバック・利用者向けのエラー文（Issue #924）

§14.8・§14.9 のローカルの声・口パクを、利用者が**設定画面と MCP で選べる**ようにし、使えないときの理由を**画面・チャット・MCP・ジョブの失敗理由で同じ文**にし、「ローカルが使えないときはクラウドにする」を**利用者がオンにしたときだけ**行う。ElevenLabs と Sync Labs は選べるまま残す（[親 Issue #912](https://github.com/ousiassllc/mam/issues/912) の決定事項 1）。

#### 保存する設定（`audio.json`）

`<データフォルダ>/audio.json`（`config.Config.AudioSettingsFile()`。`ffmpeg.json`・`mcp.json` と同じく設定画面が書く。書き込みは `settingsfile` の原子的な書き込み＋ロックで、このサービスが持たないキーは残す）。サービスは `service/audiosettings`。

| キー | 値 | 既定（ファイルが無い・キーが無い） |
|------|----|----------------------------------|
| `voice_provider` | `elevenlabs` / `local`（GPT-SoVITS） | `elevenlabs` |
| `lip_sync_provider` | `synclabs` / `local`（MuseTalk 1.5） | `synclabs` |
| `fallback_to_cloud` | `true` / `false` | `false`（オフ） |

- **未設定の既存利用者は今までと同じ（クラウド）**。ファイルは実行のたびに読むので、保存した選択は**再起動なしで次の音声作成から**使われ、再起動後も残る。
- **読めないファイル**（JSON でない・値がプロバイダーでない・型が違う）は、実行も保存も `audiosettings.ErrUnreadable` で止める（どのプロバイダーを意図したか推測しない。推測すると、利用者が選んでいない API 費用が出うる）。画面は分類の `role="alert"` の 1 文（「設定ファイル（audio.json）を読めません。壊れていれば、直すか削除してください…」）を出し、書き換えない。
- **この PC で動かせないローカルは選べない**（保存は `validation`。`voice_provider` / `lip_sync_provider`）。判定は #918・#921 の `localruntime.Service.Status` の `support`（macOS・Linux・非 x64・NVIDIA の GPU 無し・ビデオメモリ不足・RTX 50）で、文は**ランタイム管理の理由と代わりのクラウドの案内**（macOS は「公式の PyTorch 版が使えない」など。[local-runtime.md](model-manager/local-runtime.md)）。クラウドは常に選べる。別の PC からコピーされたファイルが動かせないローカルを指しているときは、実行がローカルを試して理由つきのエラーで止まる（クラウドに切り替えない。切り替えるのは次の節の条件のときだけ）。

#### 実行の入口

`EditorService.GenerateAudio(projectID, branchID)`（HTTP `POST /projects/:id/audio/generate`・MCP `generate_audio`・AUTO の連鎖の共通の入口）が、**実行のたびに** `audiosettings.Service.Load` で設定を読み、`GenerateAudioWith(…, AudioOptions)`（`AudioOptions` は `audiosettings.Settings`）に渡す。`GenerateAudioWith` に呼び出し元が選択を直接渡す入口は残り（テスト・内部の呼び出し）、保存した設定より優先する。HTTP・MCP の `generate_audio` に provider の引数は無い（選択は設定の画面・`set_audio_providers` で行う。実行ごとの上書きは持たない）。

#### フォールバックの規則（決定）

| 場合 | 動き |
|------|------|
| `fallback_to_cloud` が**オフ**（既定）で、ローカルが使えない | **クラウドに自動では切り替えない**。声は理由つきのエラー（`AIError`。HTTP 502）で止まり、課金も起動もしない。口パクは `skipped[]` に理由を 1 件だけ残して、口パク無しで続ける（§14.9）。無断で API 費用が出ない |
| `fallback_to_cloud` が**オン**で、**実行の開始前の検査**でローカルが使えないと分かった | **その実行の全体をクラウドで行う**（声は全行を ElevenLabs、口パクは全クリップを Sync Labs。途中で声が混ざらない）。クラウドの費用は今までどおり予算の予約・台帳に載る。切り替えは、チャットの要約と `generate_audio` のジョブの結果（`provider_notes`）に**理由つきで書く** |
| オンで、切り替え先の**クラウドの API キーが無い** | `*localvoice.NoCloudKeyError`。**キーの登録の案内**（設定の「API キーとログイン」で `elevenlabs` / `synclabs`）と、ローカルが使えない理由を言って止まる。何も起動せず、何も払わない |
| 実行の**途中**のローカルの失敗（GPU の OOM・サーバーの停止・1 行／1 クリップの時間切れ・動画に顔が無い） | 切り替えない。その行・クリップの失敗として扱う（声は実行の失敗、口パクはそのクリップの `skipped`）。**実行の中でプロバイダーを混ぜない** |

- **実行の開始前の検査**（`audiosettings.Service.Preflight`／`Settle`。`GenerateAudioWith` が Scene・参照音声・クリップを集めたあと、`fixMusic` と Director の前）: 声は、行のある話者**全員**に使える参照音声があること（無い話者が 1 人でもいれば、声は全体がクラウド。ローカルが読む話者だけ残さない）、ランタイムが `CheckStart` を通ること（この PC で動かせる・導入済み・重みが検証済み。起動はしない）。口パクは、Quality Policy が口パクを作り（MAX QUALITY）かつクリップがあるときだけ、同じ `CheckStart`。行が無い脚本は声を、口パクを作らない実行は口パクを、検査しない。声の検査のあと、`LocalVoices` は捨てる（クラウドは参照音声を読まない）。
- **検査しないもの**: サーバーの起動の失敗（`process`・`timeout`）は、実行の中の失敗として声はエラー・口パクは `skipped` で止まり、切り替えない。起動を試す検査を足すと、検査の時点でサーバーを起動してしまい、切り替えたあとにも GPU が塞がるため。
- 声と口パクは別々に切り替わる（声だけクラウド・口パクはローカルのまま、のように）。

#### 利用者向けのエラー文（1 か所）

`localvoice.Reason(*UnavailableError)`（`agent/audiodirector/localvoice/reason.go`）が、`Kind` と `Vendor`（声 / `musetalk`）ごとの日本語の文を **1 か所**で作る。Director の `skipped[].detail`・`service.audioError`（`AIError.Reason`）・設定画面の「使えない理由」・MCP の `get_audio_providers` の `reason`・クラウドへ切り替えたときの通知（`localvoice.Switched`）が、すべてこの関数の文を使う。形は「何が起きたか（括弧で、ランタイムやサーバーの理由）。直し方。クラウドを使う方法」。

| Kind | 文（声の場合。口パクは「ローカルの口パク（MuseTalk 1.5）」「クラウドの口パク（Sync Labs）」に替わる） |
|------|------|
| `not_installed` | ローカルの声（GPT-SoVITS）が導入されていません。モデルマネージャーの「ローカル音声・口パク」から導入してください。クラウドの声（ElevenLabs）を使うには、設定の「音声・口パク」で声のプロバイダーに ElevenLabs を選ぶか、「ローカルが使えないときはクラウドに切り替える」をオンにしてください |
| `weights_missing` | …の重みが取得されていません（足りない重み・依存ファイルの名前を括弧で）。モデルマネージャーの「ローカル音声・口パク」で「重みを導入」を実行してください。＋クラウドの案内 |
| `unsupported` | この PC ではローカルの声（GPT-SoVITS）を使えません（ランタイム管理の理由。macOS は「公式の PyTorch 版が使えません」など）。＋クラウドの案内 |
| `gpu_memory` | …に GPU のメモリが足りません（必要なビデオメモリと今の値、または CUDA の文）。他のアプリ（ゲーム・ブラウザ・画像や動画の生成）を閉じて GPU のメモリを空けると使えることがあります。＋クラウドの案内 |
| `process` | …が動きませんでした。モデルマネージャーのカードで、ログの末尾と導入先を確認してください。＋クラウドの案内 |
| `timeout` | …が時間内に終わりませんでした。もう一度試すか、PC が忙しくないときに試してください。＋クラウドの案内 |
| `no_face`（口パクのみ） | 動画から顔を見つけられなかったので、口パクを作れませんでした（そのクリップだけの事実なので、案内は付けない） |
| `too_large`（口パクのみ） | 動画が大きすぎて、ローカルの口パク（MuseTalk 1.5）で扱えませんでした。動画を短くしてください。＋クラウドの案内 |

- 参照音声が無い話者（`MissingVoiceError`）は「ローカルの声（GPT-SoVITS）で話せません。参照音声が無い話者がいます: 「…」。Character に参照音声を設定するか、声のプロバイダーを ElevenLabs に切り替えてください」（`localspeech.Reason`）。
- 重みの不足は `*localruntime.WeightsError` の資産名を `Detail` に入れて文に出す（`localspeech.UnavailableFor`。導入の最中の `ErrBusy` は「導入の最中です。終わってからもう一度実行してください」）。「GPU が足りない」の必要量は #920 のカタログの `vram.min_gb`（**mam の推定のこともある**。画面は「mam の推定です。実機では確かめていません」と言う）。
- 声のサンプルからの ElevenLabs クローン（`POST /projects/:id/audio/voices`。§14.7）は**変えない**（クラウドの機能）。設定画面の声のカードは、ローカルの声は Character の参照音声で話し、ElevenLabs のクローンは ElevenLabs の声にだけ使われ、ローカルの声には使われない、と言う。

#### 実機で確かめていないこと（#912 全体。1 か所の表）

#918〜#924 の実装は、この開発環境（Linux・GPU 無し）の偽のランタイム・偽のサーバーに対するテストだけで確かめた。次は**未確認**（実機が要る）。確かめたら日付と環境を足し、この表を更新する。

| # | 未確認の項目 | 出典 |
|---|------------|------|
| 1 | 実物の GPT-SoVITS・MuseTalk 1.5 を、Windows + NVIDIA で mam が導入・起動・停止できること（導入にかかる時間・ディスク・空き容量の検査・サーバーの起動時間） | [local-runtime.md](model-manager/local-runtime.md) §11・§13.8 |
| 2 | **日本語の質**（声: ElevenLabs との聞き比べ、書き起こしが空のときの声の再現度。口パク: 日本語の音声での出来）。公式に数値が無く、数値は出さない | §14.8・§14.9（親 Issue #912 の「メモ」） |
| 3 | 1 行・1 クリップあたりの時間（時間上限は見積） | §14.8・§14.9 |
| 4 | ビデオメモリの下限（声 4 GB は mam の推定、口パク 4 GB は公式の README の例）と、CUDA の `out of memory` の実際のメッセージ文字列（`gpu_memory` の判定は文字列に頼る） | [local-runtime.md](model-manager/local-runtime.md) §11・§13.8 |
| 5 | 依存の重み（whisper-tiny・sd-vae-ft-mse・DWPose・face-parse-bisent・syncnet など）の**商用利用の可否**。はっきりしないものは画面で目立つ注記を出す。face-parse-bisent は Google Drive 配布で、固定 URL＋SHA-256 に載せる方法は #921 の方針（§13.4）に従う | [local-runtime.md](model-manager/local-runtime.md) §12.8・§13.4・§13.6 |
| 6 | macOS の第三者の MLX 版（`mlx-community/MuseTalk-1.5-q4`）は mam の自動導入の対象外（macOS ではクラウドを使う） | local-runtime.md §13 |
| 7 | このフォールバックを実物のランタイムが使えない PC（Windows の NVIDIA 無し・導入前・重み無し）で通しで確かめたこと | §14.10（偽のランタイムでのテストだけ） |

#### #912「やってほしいこと」との対応

| 項目 | 実装 | 仕様 |
|------|------|------|
| Audio Director の声と口パクにローカルのプロバイダーを足す（ElevenLabs・Sync Labs も選べる） | #922・#923・#924（選択） | §14.8・§14.9・§14.10 |
| キャラクター（Series のキャラ含む）ごとの参照音声で、毎回同じ声 | #919 | §14.8（Character の参照音声の持たせ方は [er.md](er.md)） |
| 重みは同梱せず、モデル管理から実行時にダウンロード（入手元とライセンスを表示） | #918・#920 | [local-runtime.md](model-manager/local-runtime.md)・[model-manager-screen.md](../components/model-manager-screen.md)。設定画面にも出典・ライセンスと、商用利用がはっきりしない依存の注記を出す（[settings-screen-categories.md §9](../components/settings-screen-categories.md)） |
| ComfyUI 経由か自前か | 決定: mam 自身がランタイムを管理（#912 決定事項 1） | [local-runtime.md](model-manager/local-runtime.md) |
| macOS で動かせないものは理由を画面で見せ、クラウドにフォールバック | #918・#921（理由）・#924（画面・フォールバック） | §14.10・settings-screen-categories.md §9 |
| 使えないとき（GPU 不足・未ダウンロード）のエラーをわかりやすく | #924 | §14.10 |
| テスト | 各 Issue | 各節の「テスト」 |
