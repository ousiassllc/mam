# 機能要件

`spec/overview.md` のスコープに基づく機能要件。原典は `spec/spec.md`（製品仕様ドラフト v1.0）。

## 1. ユースケース

### UC-1: AUTO MODE で完成動画まで自律制作する

- **Actor**: エンドユーザー（クリエイター/マーケター）
- **事前条件**: プロジェクト未作成、またはドラフト状態
- **フロー**:
  1. ユーザーがチャットで作りたい動画を伝える。AI が発言から Creative Brief（Content Type / Objective / Subject / Target / Duration / Aspect Ratio / Mood / Assets / References）を抽出し、不足項目は質問する（「AIに任せる」を選べる）。添付した画像・動画は Assets/References として取り込む
  2. Confirmation Level（FULL AUTO / KEY CHECK / CUSTOM CHECK）を選択する
  3. Producer → Writer → Director → Storyboard → Art Director → Prompt Planner → Video Generation → AI Video Judge → (Repair/Retake) → AI Editor → Audio Production → Final QC の順に自動実行される
  4. 設定した確認ポイントでのみ、承認カードとしてチャットに提示される。ユーザーはボタンまたは返信で承認/差し戻しを行う
- **事後条件**: Final QC を PASS した動画が Export 可能な状態になる

### UC-2: DIRECTOR MODE で特定 Shot の演技/カメラを修正する

- **Actor**: エンドユーザー
- **事前条件**: AUTO MODE で生成済みの Scene/Shot が存在する
- **フロー**:
  1. ユーザーがチャットで、対象の Scene/Shot/Take を示して（カードへの返信、または文脈から AI が解決）DIRECTOR MODE に切り替える
  2. 自然言語で演出指示を発言する（例:「もう少し寄って」「この瞬間だけ不安そうな顔にして」）
  3. Command Interpreter が指示を構造化操作（LOCK設定、パラメータ更新、再生成、Branch生成等）へ変換する
  4. 対象 Shot のみ再生成され、Character/Acting/Environment 等の指定要素は保持される。時間範囲を名指しした指示（M2、「2〜3秒目のカメラの揺れだけ直して」）は、その区間だけを差し替えた新しい Take（Partial Regeneration）になり、元の Take は変わらない
  5. 修正完了後、AUTO MODE に戻し後続工程を再開できる
- **事後条件**: 対象 Shot が更新され、World State に反映される

### UC-3: Take を比較し部分採用する

- **Actor**: エンドユーザー
- **事前条件**: 対象 Shot に複数 Take が生成済み
- **フロー**:
  1. Take 一覧（評価スコア: Product/Acting/Camera/Continuity, Status）を比較表示する
  2. ユーザーが Take 全体、または「Take A の 0-2秒 + Take B の 2-4秒」のように部分区間を指定する
  3. Editor/Repair Agent が指定区間を自然に接続する
- **事後条件**: Selected Take（または合成結果）が Shot に確定する

### UC-4: Continue で続きの Shot/Scene を生成する

- **Actor**: エンドユーザー
- **事前条件**: 完成済み Shot/Scene/Video が存在する
- **フロー**:
  1. AI が最終フレームおよび Story/Character/Position/Emotion/Object/Camera/Lighting/Motion/Dialogue/Music State を解析する
  2. 複数の展開候補（NEXT A/B/C/... + 自由指示 + AIに任せる）を提示する
  3. ユーザーが選択、または AI に一任する
  4. World State を引き継いだ次 Shot/Scene が生成される
- **事後条件**: 新しい Shot/Scene が既存プロジェクトに連結される

### UC-5: Branch で別展開を作成する

- **Actor**: エンドユーザー
- **事前条件**: Branch Point としたい Scene/Shot/Frame が存在する
- **フロー**:
  1. ユーザーが Branch Point を指定する
  2. Branch 作成時点の World State を Snapshot として保存する
  3. 複数 Branch（例: ドアを開ける / 逃げる / 電話する）を並行して生成・比較する
- **事後条件**: 各 Branch が独立した派生プロジェクトツリーとして保持される

### UC-6: AI Editor が素材不足を検知し追加撮影ループへ戻る

- **Actor**: システム（Editor Agent）、エンドユーザー（承認時）
- **事前条件**: Selected Take群から Rough Cut が生成済み
- **フロー**:
  1. Editor Judge が Rough Cut を評価する（Shot Selection / Cut Point / Timing / Pace / Transition / Hook / Retention / CTA Timing 等）
  2. 「Editで修正可能」と判定された場合は Re-edit する
  3. 「素材不足」と判定された場合は Director Agent へ差し戻し、Additional Shot を Video Generation から再実行し、完了後 Editor に戻る
  4. 問題なしと判定されたら Audio Production へ進む
- **事後条件**: Rough Cut が Audio Production に進める品質になる

### UC-7: Production Template を切り替える

- **Actor**: エンドユーザー
- **フロー**: プロジェクト作成時またはブリーフ入力時に PRODUCT/CM・DRAMA・ANIME・SNS/SHORT VIDEO のいずれかを選択する。選択に応じて Judge/Editor の評価軸優先度（例: PRODUCT/CM は Product Accuracy 最優先、DRAMA は Story/Acting 最優先）が切り替わる
- **事後条件**: 以降の Judge/Editor 評価が選択テンプレートの優先度に従う

### UC-8: Quality/Cost ポリシーを選択する

- **Actor**: エンドユーザー
- **フロー**: FAST / BALANCED / MAX QUALITY のいずれかを選択し、必要に応じて Project budget ceiling・Shot budget ceiling・Max retries・Max generation time・Minimum quality thresholds を個別設定する
- **事後条件**: Generation Orchestrator と Repair/Retake Loop が設定ポリシーに従って動作する

### UC-9: Safety / Brand Constraints を適用する

- **Actor**: エンドユーザー（設定）、システム（適用）
- **フロー**: Project 単位で Brand Guidelines・Forbidden Expressions・Required Disclaimer・Product Claims・Logo Rules・Character Restrictions・Music/Asset Rights・Platform Rules を登録する。これらは Prompt 生成・AI Video Judge・Final QC すべてに適用される。LOCK 違反はスコアに関係なく FAIL とする
- **事後条件**: 制約に反する生成結果は自動的に不合格となる

### UC-10: 制作メモを登録し、制作に反映する（M2）

- **Actor**: エンドユーザー、システム（State Extractor）
- **事前条件**: Project が存在する（Series メモの場合は Series が存在する）
- **フロー**:
  1. ユーザーがチャットで「これ覚えておいて」と発言する（または補助ビューの Notes から登録・編集する）。Project の Notes に新しいメモバージョンが追加される
  2. 以降の Writer / Director / Prompt Planner が、ピン留めされたメモと関連するメモを推論の文脈として参照する
  3. State Extractor がメモから World State に構造化できる項目（髪型・衣装・性格等）を検出した場合、非ブロッキングの承認カードで World State への昇格を提案する。承認すると World State に新バージョンが追加される。LOCK 中の項目は提案から除外される
  4. Series 単位のメモは Episode 開始時に Project へコピーされる
- **事後条件**: メモが以降の制作の文脈として使われ、承認された項目は World State（構造化状態）に反映される。昇格カードへの応答がなくても制作は止まらない

## 2. 機能一覧

| ID | カテゴリ | 機能 | 概要 | MVP |
|----|---------|------|------|-----|
| F-01 | Brief | Creative Brief 入力 | チャットでの発言・添付から Content Type/Objective/Subject/Target/Duration/Aspect Ratio/Mood/Assets/References を抽出し、不足を質問して補完する。「AIに任せる」を選択可 | M1 |
| F-02 | Mode | AUTO ⇄ DIRECTOR 切替 | 同一プロジェクト上でモードをいつでも切替 | M1 |
| F-03 | Confirmation | Confirmation Level 設定 | FULL AUTO / KEY CHECK / CUSTOM CHECK | M1 |
| F-04 | Hierarchy | Scene/Shot/Take 階層管理 | 階層構造の表示・選択・生成状態管理 | M1 |
| F-05 | Control | AUTO/MANUAL/LOCK 粒度制御 | Story/Dialogue/Character Identity/Acting/Camera/Lighting/Product Geometry/Prompt/Video Model/Take Selection/Editing/Music 等パラメータ単位の制御 | M1 |
| F-06 | Prompt | Prompt Autopilot | 自然言語指示 → Canonical Shot Spec（YAML）への変換 | M1 |
| F-07 | Prompt | Model Adapter | Canonical Shot Spec をモデル固有プロンプトへ変換。モデル非依存のプロジェクト構造を維持。採用プロバイダー・ルーティングは `spec/architecture/model-adapters.md` 参照 | M1 |
| F-08 | Prompt | Prompt Candidate Generation | Stability First/Acting First/Dynamic Camera 等の複数撮影戦略生成（Quality/Balanced/Economy Mode） | M2 |
| F-09 | State | World State 管理 | characters/products/location/camera/story を構造化保持。M2 で Location State（time/lighting_state/weather）の自動更新と、項目単位の LOCK（`spec/spec.md` §9.3） | M1（Basic）/ M2（Lock/Continue拡張） |
| F-10 | State | State Update（差分抽出） | Shot完了後に State Extractor が差分をWorld Stateへ反映。LOCK項目は自動更新禁止 | M1 |
| F-11 | Keyframe | Keyframe / Reference Pack 生成 | Character/Product/Location/Style/Lighting Reference と Keyframe の生成。M2 で Reference Library（Project 内の参照画像の登録・検索・再利用、`spec/spec.md` §10.1） | M1（Reference Pack）/ M2（Reference Library） |
| F-12 | Generation | Video Generation Orchestration | Shot Spec/World State/References/Cost/Model能力から生成戦略決定、複数Take出力 | M1 |
| F-13 | Judge | AI Video Judge | Prompt Adherence/Character/Product/Face/Hands/Pose/Acting/Lip Sync/Motion/Physics/Camera/Composition/Lighting/Background/Temporal Consistency/Flicker/Scene Continuity 等の評価とフレーム単位Issue出力。顔同一性は Face Identity Similarity（参照顔との埋め込み類似度、`spec/spec.md` §12.4）も数値で評価する | M1 |
| F-14 | Repair | Repair/Retake Loop | Prompt Repair/Partial Regeneration/Full Retake/Different Model/Reference Reinforcement/Edit-Level Repair の選択、Retry Policy（Max retries/Max cost/Min score/Critical constraints/Escalation）。Partial Regeneration は M1 では Judge の `preserve` を保つ Shot 全体の再生成、M2 では加えて問題の時間範囲だけを差し替えたTake（新しい `take`、既存 Take は変えない）を作り、DIRECTOR MODE の自然言語指示（「2〜3秒目だけ直して」）からも起動できる（`spec/spec.md` §13） | M1（Partial Regeneration の時間範囲つなぎ込みは M2） |
| F-15 | Take | Take System | 複数Take保持、AI Pick（AUTO）/ユーザー選択（DIRECTOR）、Partial Take合成（Partial Regeneration のつなぎ込み〈M2〉も同じ合成を使う） | M1 |
| F-16 | Continuity | Continue | 次展開候補提示とWorld State引き継ぎでの続き生成 | M2 |
| F-17 | Continuity | Branch | Branch Point保存、Snapshot化、複数展開の並行生成 | M2 |
| F-18 | Editor | AI Editor | Shot Selection/Cut Point/Timing/Pace/Transition/Reaction Shot/Insert Shot/Product Exposure/Narrative Clarity/Hook/Retention/CTA Timing/Caption | M1（Simple）/ M3（Advanced） |
| F-19 | Editor | Editor → Reshoot Loop | Editor Judgeが素材不足を検知しDirector Agent/追加撮影へ差し戻す | M3 |
| F-20 | Audio | Audio Production | Dialogue/Voice/Narration/Lip Sync/BGM/SE/Foley/Ambience/Mixing/Loudness最適化。TTS/Music/SFXはElevenLabs、Lip SyncはSync Labs（`spec/architecture/model-adapters.md`§5） | M3（Audio Director含む）/ 簡易版はM1 |
| F-21 | Template | Production Templates | PRODUCT/CM・DRAMA・ANIME・SNS/SHORT VIDEO ごとの評価軸切替 | M1〜 |
| F-22 | Policy | Quality/Cost Control | FAST/BALANCED/MAX QUALITY、予算・リトライ上限設定 | M1 |
| F-23 | Safety | Safety/Brand Constraints | Brand Guidelines等の登録とPrompt/Judge/Final QCへの適用 | M2〜 |
| F-24 | Director | Natural Language Director Commands | 自然言語 → 構造化操作（LOCK設定/パラメータ更新/Branch作成等）へのCommand Interpreter変換 | M1 |
| F-25 | Project | Project Data Model管理 | Brief/Template/Preferences/Policy/World State/Assets/Scenes/Shots/Takes/Edit Timeline/Audio Timeline/Branches/Exports の保持 | M1 |
| F-26 | Export | Final Export | Final QC PASS後の動画書き出し | M1 |
| F-27 | Trace | Generation Traceability | Shot Spec/World State version/Prompt version/Model/Parameters/References/Output/Judge結果/Repair履歴の保存 | M2 |
| F-28 | Versioning | Reversible Decisions | Shot/Take/Branch/State/Note の主要変更のバージョン化と復元 | M2〜M3 |
| F-29 | Continuity | Series / Episode Memory | Series 単位で Character/Product/Location/Style の正本と Reference Pack を保持し、Episode（Project）開始時に継承する。前 Episode 終了時の World State を次 Episode の開始状態にできる。正本の更新は既存 Episode へ自動伝播しない（Traceability）。顔同一性は Series 正本の `face_reference` を基準に Episode 間でも計測する（`spec/spec.md` §12.4） | M2 |
| F-30 | UI | Chat Workspace | 1プロジェクト＝1チャットスレッドを標準画面とし、Brief 入力・確認ポイントの承認・演出指示・進捗確認・Take 視聴をスレッド内で完結できる。Inspector/Take比較/Timeline/Branch/World State は補助ビュー（`spec/spec.md` §23） | M1 |
| F-31 | Continuity | Notes（制作メモ） | Project / Series 単位の自由記述メモ（登場人物の背景・世界観・トーン等）。チャット発言（「覚えておいて」）または補助ビューから登録し、バージョン管理する。Writer/Director/Prompt Planner が推論の文脈として参照し（ピン留めは常時、他は関連度順）、画像/動画生成モデルには渡さない。Series メモは Episode 開始時にコピーし、自動伝播しない。State Extractor が構造化可能な内容を World State への昇格として非ブロッキングの承認カードで提案し、LOCK 項目は除外する。守らせたいルールは Brand Constraint（F-23）に登録する（`spec/spec.md` §9.4） | M2 |

## 3. 画面・操作フロー

### 3.1 Chat Workspace（標準画面）

```text
┌────────────────────────────────────────────────────┐
│ PROJECT: <プロジェクト名>      [ ⋯ 補助ビュー ] Export │
├────────────────────────────────────────────────────┤
│ チャットスレッド（ユーザー発言 / AI 発言 / カード）  │
│                                                    │
├────────────────────────────────────────────────────┤
│ [＋] 指示を入力…                          [ 送信 ] │
│ MODE [ AUTO | DIRECTOR ]   Quality: Balanced        │
└────────────────────────────────────────────────────┘
```

- ユーザーの操作は、スレッドへの発言とスレッド内カードのボタン操作で完結する。補助ビューは使わなくても制作を完了できる
- カードの種類: Question / Approval / Keyframe / Take / Progress / Escalation（`spec/spec.md` §23.2）
- 指示の対象 Scene/Shot/Take は、カードへの返信または直前の文脈から AI が解決し、解決できない場合は聞き返す
- MODE トグルで AUTO ⇄ DIRECTOR をいつでも切替可能（同一プロジェクト内、状態は保持）
- Quality セレクタで FAST/BALANCED/MAX QUALITY を切替
- スレッドは Project ごとに永続化され、再度開いたときに履歴を再表示する

### 3.2 補助ビュー

チャットの横、または別ページで開く。スレッド内カードと［⋯］メニューから到達でき、操作結果は同じ Project の状態とスレッドに反映される。

- **Inspector**（選択中 Shot の詳細）: Shot Objective / Duration / Character / Acting / Camera / Lighting / Product / Continuity / References / Prompt / Generation Model / Take List / Judge Results / Locks。各項目には AUTO / MANUAL / LOCK の切替コントロールを持つ
- Take 比較（§3.4）、Timeline、Branch ツリー（§3.5）、World State、Notes（M2）、Cost、Settings

### 3.3 典型的な操作フロー（モード遷移）

```mermaid
flowchart LR
    A[AUTOで初稿を80%まで制作] --> B[気になるSceneだけDIRECTOR MODEへ]
    B --> C[演技/Camera/Takeを修正]
    C --> D[AUTO MODEへ戻す]
    D --> E[編集・音声・Final QC]
    E --> F[DELIVERY]
```

### 3.4 Take 比較 UI

Take一覧をスコア表（Product/Acting/Camera/Continuity, Status: Candidate/AI Pick/Selected/Rejected）で表示し、区間指定による部分採用（例:「Take Aの0〜2秒 + Take Bの2〜4秒」）を自然言語入力またはタイムライン上のドラッグで指定できる。

### 3.5 Branch ツリー UI

Scene/Shot 配下に複数 Branch をツリー表示し、Branch 間の比較・切替・削除を行える。

### 3.6 M1 の編集・音声・書き出しの範囲（F-18 Simple / F-20 簡易版 / F-26）

**F-18 Simple（M1）で扱う編集項目**は次の 4 つ。それ以外は M3（Advanced）とする。

| 項目 | M1 で扱う内容 |
|------|--------------|
| Shot Selection | Rough Cut は各 Shot の採用 Take（Selected Take）を Scene 順・Shot 順に並べて作る。編集では Shot の並べ替えと除外ができる（Shot の追加は M3） |
| Cut Point | 各クリップの Take 内の開始点・終了点（In / Out）。Rough Cut の初期値は Take の先頭から Shot の計画尺まで |
| Timing | クリップの長さ（Out − In）。最短 0.5 秒 |
| Transition | クリップの前に置く切り替え（`cut` / `fade` / `fade_black` / `dissolve`）と長さ。Rough Cut の初期値は Scene 内が `cut`、Scene の切れ目が `fade`（0.25 秒） |

**M3（Advanced）に残す項目**: Pace の自動調整、Reaction Shot / Insert Shot、Product Exposure、Narrative Clarity、Hook / Retention / CTA Timing の最適化、Caption。Editor Judge の判定は PASS / Re-edit のみで、素材不足（NEED_SHOT）の差し戻しは F-19（M3）で扱う。

**F-20 簡易版（M1）の範囲**は次のとおり。BGM / SE / Foley / Ambience / Lip Sync / 音声クローン / Audio Director による音楽・Narration の再構成（映像尺変更への追従）は M3 とする。

- **Dialogue / Narration**: 脚本（`script_version`）の Scene ごとの `dialogue` 行（`話者: セリフ`、話者なしまたは `ナレーション` / `NA` は Narration）を ElevenLabs の TTS で音声化し、Scene の先頭 Shot の開始時刻から順に置く。行が 1 つもない Project は TTS を呼ばない
- **ミキシング**: Take 自体の音声（あれば）と TTS 音声をローカルの ffmpeg で 1 本に混ぜる
- **ラウドネス正規化**: ローカル DSP（ffmpeg `loudnorm` の 2 パス）で目標ラウドネス（既定 −14 LUFS、SNS 向け）に揃え、測定値を Audio Timeline に保存する。外部 API は呼ばない

**Final QC（F-26）の M1 の判定**は、決定的なチェック（Audio Timeline が最新の Edit Timeline と一致しているか、映像と音声の長さの差、ラウドネスが目標から ±1.5 LU 以内か、音声が入っているべき Project に音声があるか）のみとする。FAIL のときは原因の工程（編集の不一致 → EDITING、音声の不備 → AUDIO）へ戻す。Brand 制約の適用は M2。

**Export（F-26）**: Aspect Ratio（Project の Brief の値を既定とし、`16:9` / `9:16` / `1:1` / `4:5` から選択）、解像度（短辺 `720p` / `1080p` / `1440p` / `2160p`）、コーデック（`h264` / `h265` / `vp9`）を指定してローカルへ書き出す。書き出しはアトミックに行い（一時ファイルへ書いて完了後にリネーム）、途中で失敗しても不完全なファイルは残らない。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-28 | 新規作成 | `spec/spec.md` §3〜19, 23〜26 を機能要件として構造化 |
| 1.1 | 2026-09-28 | UC-9 のフローから「Organization 単位」の記述を削除し「Project 単位」に統一 | `spec/overview.md` のスコープ（SaaSマルチテナント化・組織単位運用は対象外の単一ユーザー向けツール）と、ER図（`er.md`）にOrganizationエンティティが存在しないことの矛盾を解消するため（整合性レビュー指摘） |
| 1.2 | 2026-09-28 | 3.4 Take比較UIのStatus表示に「Selected」を追加 | er.mdの`take.status` CHECK制約（candidate/ai_pick/selected/rejected）およびF-15のDIRECTORモードでのユーザー選択に対応する状態表示が欠落していたため（整合性レビュー指摘） |
| 1.3 | 2026-09-29 | F-07（Model Adapter）・F-20（Audio Production）に採用プロバイダーへの参照を追加 | ユーザーとの協議によりエージェント推論LLM・画像/動画生成/音声プロバイダーを具体的に選定したため（`spec/architecture/model-adapters.md` 新設に対応） |
| 1.4 | 2026-09-29 | F-13 に Face Identity Similarity を追記。F-29（Series / Episode Memory, M2）を追加 | シリーズ制作で顔が毎回ブレることを避けるための数値指標と、話数をまたぐ正本管理が仕様になかったため |
| 1.5 | 2026-09-29 | 標準画面を Scene/Shot タブ + Video Preview 構成から、チャット中心の Chat Workspace に変更（§3.1, §3.2）。F-30 を追加。UC-1/UC-2 と F-01 をチャット入力前提に更新 | ユーザー要望「基本的にはただのチャット画面にしたい」。従来の Inspector 等は補助ビューへ位置づけ直した |
| 1.6 | 2026-09-29 | UC-10（制作メモの登録と反映）と F-31（Notes, M2）を追加。F-28 のバージョン化対象に Note を追加。§3.2 補助ビューに Notes を追加 | 登場人物の背景・世界観など構造化フィールドに収まらない文脈を置く場所がなく、World State と Brand Constraint だけでは制作の記憶を保持できなかったため（ユーザー要望） |
| 1.7 | 2026-09-30 | F-14 の Partial Regeneration を「M1: Judge の `preserve` を保つ Shot 全体の再生成 / M2: 問題の時間範囲だけを差し替えるつなぎ込み」に分けて明記（`spec/spec.md` §13・§27 と一致）。UC-2 に区間指定の指示を追加 | `spec.md` §27 は Partial Regeneration を MVP 2、F-14 は M1 としており食い違っていたため（Issue #16） |
| 1.8 | 2026-09-30 | §3.6 を追加し、F-18 Simple の編集項目（Shot Selection / Cut Point / Timing / Transition）、F-20 簡易版の範囲（Dialogue / Narration の TTS + ローカル DSP のラウドネス正規化）、Final QC の M1 判定、Export の指定項目を確定 | Issue #14 の着手前確定事項（M1 の Simple / 簡易版と Advanced / M3 の境界が仕様になかったため） |
