# AI Video Production Agent — Product Specification v1.0

> AIが企画・演出・撮影・品質判定・撮り直し・編集までを行う、自律型AI映像制作スタジオ。

**Document status:** Product / System Specification  
**Version:** 1.0  
**Primary modes:** AUTO MODE / DIRECTOR MODE

---

## 1. Product Vision

AI Video Production Agentは、単一のプロンプトから動画を出力する「動画生成ツール」ではない。

ユーザーが目的・素材・世界観・制約を伝えると、AIが制作チームとして企画、脚本、絵コンテ、キーフレーム、プロンプト設計、動画生成、品質判定、撮り直し、編集、音声、最終QCまでを継続的に実行する **AI Production Studio** である。

ユーザーに必要なのは高度なプロンプトエンジニアリングではなく、「何を作りたいか」「どこを自分で決めたいか」を伝えること。

### 1.1 Target Content

- 商品紹介 / 商品PV
- TVCM / Web CM
- SNS広告 / 縦型ショート動画
- ドラマ / ショートドラマ
- アニメーション
- MV / イメージ映像
- YouTubeコンテンツ
- ブランドムービー
- シリーズ / エピソード型コンテンツ

### 1.2 Core Value

本プロダクトの中心価値は生成モデルそのものではなく、以下の制作OSにある。

1. Prompt Autopilot
2. World State / Continuity Memory
3. AI Video Judge
4. Shot / Take / Retake System
5. DIRECTOR ↔ AUTO の自由な切り替え
6. Editorから撮影工程へ戻れる自己修正ループ
7. モデル非依存のGeneration Orchestration

---

## 2. Product Principles

### 2.1 Intent First

ユーザーは「動画生成用プロンプト」ではなく、制作意図を入力する。

例：

> この美容液を20〜30代女性向けに、高級感のある15秒Instagram広告にして。夜のホテルのような雰囲気。商品画像はこれ。

AIがこれを制作仕様へ展開する。

### 2.2 AI Generates Detail

不足している撮影・演出情報はAIが自律補完する。

- カメラ
- レンズ
- 構図
- 演技
- モーション
- ライティング
- 被写界深度
- Shot duration
- Continuity constraints
- Negative constraints
- モデル固有プロンプト

### 2.3 Human Controls Only What Matters

ユーザーは制作工程すべてを確認する必要はない。

AUTOに任せる項目と、自分で決める項目を工程単位・パラメータ単位で選択できる。

### 2.4 Generate → Watch → Judge → Repair

生成は一回で終了しない。

AI自身が生成結果を視聴・解析し、問題があればプロンプト修正、部分再生成、Take追加、別モデル利用などを判断する。

### 2.5 Continuity Is State

前の動画を単なるReference Videoとして扱わない。

キャラクター、商品、場所、物体、感情、ストーリー、カメラ、光などを構造化されたWorld Stateとして保持し、次のShotへ継承する。

---

## 3. Main User Experience

本プロダクトには2つの主要モードを設ける。

- **AUTO MODE — 自動生成モード**
- **DIRECTOR MODE — 監督モード**

両者は別プロジェクトではなく、同一プロジェクト上でいつでも切り替え可能とする。

典型的な利用方法：

```text
AUTOで初稿を80%まで制作
        ↓
気になるSceneだけDIRECTOR MODE
        ↓
演技 / Camera / Takeを修正
        ↓
AUTO MODEへ戻す
        ↓
編集・音声・Final QC
        ↓
DELIVERY
```

---

# 4. AUTO MODE — 自動生成モード

## 4.1 Purpose

「作りたい動画」を指定すると、AIが完成まで自律的に制作するモード。

## 4.2 Minimum Input

最低限以下を入力できれば制作開始可能とする。

| Field | Example |
|---|---|
| Content Type | 商品CM |
| Objective | Instagram広告 |
| Subject | 美容液 |
| Target | 20〜30代女性 |
| Duration | 15秒 |
| Aspect Ratio | 9:16 |
| Mood | 高級 / ミニマル / 夜 |
| Assets | 商品画像3枚 |
| References | URL / Image / Video |
| Additional Instruction | 任意 |

入力項目が不足している場合、AIは可能な範囲で自動補完する。

## 4.3 AUTO Pipeline

```text
Creative Brief
      ↓
Producer Agent
      ↓
Concept / Structure
      ↓
Writer Agent
      ↓
Script
      ↓
Director Agent
      ↓
Scene / Shot Planning
      ↓
Storyboard Agent
      ↓
Art Director Agent
      ↓
Keyframes / Visual References
      ↓
Prompt Planner
      ↓
Image / Video Generation
      ↓
AI Video Judge
      ↓
PASS ? ── NO → Repair / Retake ──┐
  │                               │
 YES ←────────────────────────────┘
  ↓
AI Editor
  ↓
Rough Cut Judge
  ↓
Additional Shot Required ?
  ├ YES → Director / Generation
  └ NO
      ↓
Audio Production
      ↓
Final QC
      ↓
Delivery
```

---

## 4.4 Confirmation Levels

AUTO MODEでは、人間が確認する頻度を選択できる。

### FULL AUTO

制作開始後、Finalまで原則確認しない。

### KEY CHECK

主要マイルストーンのみ確認する。

標準ポイント：

1. Concept / Script
2. Main Visual / Character / Product
3. Rough Cut
4. Final

### CUSTOM CHECK

ユーザーが確認ポイントを自由設定する。

例：

- キャラクターデザインのみ確認
- Scriptのみ確認
- 商品Key Visualのみ確認
- Finalのみ確認
- 各Scene完成時に確認

---

# 5. DIRECTOR MODE — 監督モード

## 5.1 Purpose

ユーザー自身がAI制作チームを監督するモード。

ユーザーはプロンプトを書くのではなく、自然言語で演出指示を出す。

例：

> もう少し寄って。

> この瞬間だけ不安そうな顔にして。

> カメラは固定。この商品の向きは絶対変えない。

> Take Bの演技は残して、カメラだけTake Cに近づけて。

> 最初の2秒はこのTakeを使って、その後だけ撮り直して。

## 5.2 Scene / Shot / Take Hierarchy

```text
PROJECT
 └ SCENE 03 — Cafe / Night
      ├ SHOT 01
      │   ├ Take A
      │   ├ Take B ★ Selected
      │   └ Take C
      │
      ├ SHOT 02
      │   ├ Take A ★ Selected
      │   └ Take B
      │
      └ SHOT 03
          └ Generating...
```

### Scene

物語・場所・時間・目的などを共有する単位。

### Shot

一つの連続した撮影単位。

### Take

同一Shot Specを基準に生成された複数候補。

---

# 6. Granular AUTO / MANUAL / LOCK Controls

DIRECTOR MODEでも、すべてをユーザーが操作する必要はない。

各項目に以下の状態を設定できる。

- **AUTO** — AIが決定・変更可能
- **MANUAL** — ユーザーが指定
- **LOCK** — 現在値を固定し、AIによる変更禁止

例：

| Parameter | Mode |
|---|---|
| Story | AUTO |
| Dialogue | MANUAL |
| Character Identity | LOCK |
| Acting | AUTO |
| Camera | MANUAL |
| Lighting | AUTO |
| Product Geometry | LOCK |
| Prompt | AUTO |
| Video Model | AUTO |
| Take Selection | AUTO |
| Editing | MANUAL |
| Music | AUTO |

これにより、例えば「カメラだけ自分で決めて、残りはAI」という制作が可能になる。

---

# 7. Prompt Autopilot

## 7.1 Purpose

ユーザーの短い自然言語指示を、生成モデルが理解しやすい詳細なProduction Promptへ自動変換する。

### User Input

> 主人公が振り返って走り出す。

### Internal Shot Specification

```yaml
shot_objective: Character notices something behind her and starts running

duration: 4.0s

character:
  id: Character_A
  identity: locked
  wardrobe: inherit_previous
  hair: inherit_previous

acting:
  - time: 0.0-0.7
    action: subtle reaction
  - time: 0.7-1.3
    action: turn head right
  - time: 1.3-2.0
    action: rotate upper body
  - time: 2.0-4.0
    action: begin running

camera:
  lens: 35mm
  framing: medium
  movement: subtle handheld tracking

lighting:
  inherit: previous_shot
  key_direction: camera_left

continuity:
  start_from_previous_final_state: true

constraints:
  - preserve_face_identity
  - preserve_wardrobe
  - preserve_environment_geometry
  - do_not_add_characters
```

## 7.2 Model Adapter

内部Shot Specはモデル非依存で保持する。

その後、Model Adapterが各画像・動画生成モデルに最適化したプロンプト / パラメータへ変換する。

```text
User Intent
    ↓
Canonical Shot Spec
    ↓
Prompt Planner
    ↓
Model Adapter
 ┌────┼─────┐
Model A  Model B  Model C
```

これにより生成モデルを交換しても、プロジェクト構造を維持できる。

---

# 8. Prompt Candidate Generation

必要に応じてPrompt Plannerは複数の撮影戦略を作る。

```text
Candidate A
Stability First

Candidate B
Acting First

Candidate C
Dynamic Camera
```

### Generation Policy

**Quality Mode**

複数Candidateを並列生成し、JudgeがBest Takeを選択。

**Balanced Mode**

第一候補を生成し、NG時のみ追加Candidate生成。

**Economy Mode**

最小生成回数を優先し、重大問題のみRetake。

---

# 9. World State / Continuity Memory

## 9.1 Purpose

Scene間・Shot間・Episode間の一貫性を維持する。

## 9.2 World State Schema

```yaml
project_world:

  characters:
    Character_A:
      face_reference: ref_001
      body_reference: ref_002
      hairstyle: long_black
      wardrobe: black_coat_v2
      voice: voice_A
      personality: reserved
      emotion: anxious
      position: hallway_door
      current_action: reaching_for_handle

  products:
    Product_A:
      geometry_reference: product_mesh_ref
      label_reference: label_ref
      material: frosted_glass
      orientation: front_30deg
      condition: unopened

  location:
    id: Apartment_A
    time: sunset
    layout_reference: apartment_layout_01
    lighting_state: warm_window_light
    weather: clear

  camera:
    lens: 35mm
    position: hallway_A
    angle: eye_level
    movement: static

  story:
    scene: 8
    character_goal: leave_apartment
    known_information:
      - heard_noise_outside
    previous_event: phone_call_ended
```

## 9.3 State Update

各Shot完了後にAIが最終状態を抽出し、World Stateとの差分を更新する。

```text
Previous State
      +
Completed Shot
      ↓
State Extractor
      ↓
State Diff
      ↓
Updated World State
      ↓
Next Shot
```

ユーザーがLOCKしたStateは自動更新禁止とする。

---

# 10. Image / Keyframe Generation

動画生成前に必要に応じてKeyframeを生成する。

用途：

- キャラクターデザイン固定
- 商品外観固定
- Scene Look Development
- Camera Composition
- Lighting Reference
- First / Last Frame指定

Art Director AgentがReference Packを構築する。

```text
CHARACTER REFERENCE
PRODUCT REFERENCE
LOCATION REFERENCE
STYLE REFERENCE
LIGHTING REFERENCE
KEYFRAME
```

これらをShot Generation時に自動利用する。

---

# 11. Video Generation Engine

Generation OrchestratorがShot Spec、Reference、World State、コスト、モデル能力をもとに使用モデルと生成戦略を決定する。

入力：

```text
Shot Spec
World State
References
Previous Final Frame
Target First / Last Frame
Quality Policy
Cost Policy
```

出力：

```text
Take A
Take B
Take C
...
```

---

# 12. AI Video Judge

## 12.1 Purpose

生成動画をAI自身が視聴し、採用可能か判断する。

単なる総合スコアではなく、時間・フレーム単位のIssueとRepair Strategyを返す。

## 12.2 Evaluation Dimensions

- Prompt Adherence
- Character Identity
- Character Consistency
- Product Accuracy
- Product Geometry
- Logo / Text Accuracy
- Object Consistency
- Face
- Face Identity Similarity（数値指標、§12.4）
- Hands
- Pose
- Acting
- Lip Sync
- Motion Quality
- Physics
- Camera Motion
- Composition
- Lighting
- Background Consistency
- Temporal Consistency
- Flicker
- Scene Continuity
- Previous / Next Shot Compatibility

## 12.3 Judge Output

```yaml
result: fail

overall_confidence: 0.94

metrics:
  face_identity:
    - character_id: Character_A
      similarity_mean: 0.71
      similarity_min: 0.42
      sampled_frames: 48
      undetected_ratio: 0.08

issues:
  - type: product_logo_deformation
    severity: high
    time_range: 2.1-2.8s
    frames: 63-84

  - type: camera_speed
    severity: medium
    time_range: 0.8-1.7s

  - type: face_identity_drift
    severity: high
    time_range: 3.0-3.9s

preserve:
  - character
  - acting
  - background

repair_strategy:
  action: partial_regeneration
  instructions:
    - increase_product_reference_strength
    - preserve_logo_geometry
    - reduce_camera_speed_40_percent
```

## 12.4 Face Identity Similarity（顔同一性の数値指標）

Judge の Face / Character Identity 評価を補うローカル計測。Shot 間・Episode 間の顔のブレを、定性評価の見逃しなく検知するために用いる。

- **基準**: World State の Character の `face_reference`（Reference Pack）
- **手順**: Take のフレームをサンプリング → 顔検出 → 顔埋め込み → 参照顔とのコサイン類似度を算出。1フレームに複数の顔がある場合は各 Character の参照顔と照合して割り当てる
- **出力**: Judge Result の `metrics.face_identity`（Character 別の `similarity_mean` / `similarity_min` / `sampled_frames` / `undetected_ratio`）。閾値割れが連続した区間は `face_identity_drift` Issue（`time_range` / `frames` 付き）として出力する
- **欠測**: 顔を検出できないフレーム（後ろ向き・遮蔽・極小）は欠測として扱い、FAIL 判定に含めない。`undetected_ratio` が高く数値評価が成立しない Take は、定性評価（Gemini）のみで判定する
- **閾値**: 警告 / FAIL の2段階の設定値（ハードコード禁止）。Reference Pack 内の参照画像同士の類似度分布を基準にキャリブレーションし、具体値は実測で決定する
- **LOCK 連動**: Character Identity が LOCK のとき、FAIL 閾値を割った Take は総合スコアに関係なく FAIL とする（§13）
- **Repair**: `face_identity_drift` は Reference Reinforcement を優先候補とする
- **Series**: Episode をまたいでも Series 正本の `face_reference` を基準に計測する（F-29）
- **実行**: ローカル実行。顔画像は外部へ送信しない。モデルのライセンス（商用利用可否）と Go 単一バイナリでの推論ランタイムは実装時に確認する（`spec/architecture/model-adapters.md` §4.5, §8）

---

# 13. Repair / Retake Loop

Judge結果からRepair Agentが次の処理を決定する。

```text
Generation #1
      ↓
Judge
      ↓
Issue Classification
      ↓
┌───────────────────────────┐
│ Prompt Repair             │
│ Partial Regeneration      │
│ Full Retake               │
│ Different Model           │
│ Reference Reinforcement   │
│ Edit-Level Repair         │
└───────────────────────────┘
      ↓
Generation #2
      ↓
Judge
      ↓
PASS
```

### Retry Policy

無限再生成を避けるため、以下を設定可能とする。

- Max retries per Shot
- Max cost per Shot
- Minimum acceptance score
- Critical constraints
- Escalation to human

重大なLOCK違反はスコアに関係なくFAILとする。

---

# 14. Take System

各Shotには複数Takeを保持できる。

例：

| Take | Product | Acting | Camera | Continuity | Status |
|---|---:|---:|---:|---:|---|
| A | 98 | 71 | 94 | 96 | Candidate |
| B | 97 | 94 | 90 | 96 | AI Pick |
| C | 72 | 96 | 97 | 93 | Rejected |

AUTO MODEではAIがTakeを選択する。

DIRECTOR MODEではユーザーがTakeを選択・比較・部分採用できる。

### Partial Take Use

例：

> Take Aの0〜2秒 + Take Bの2〜4秒

Editor / Repair Agentが自然な接続を試みる。

---

# 15. Continue — 次の動画を作る

完成Shot / Scene / Videoから、そのまま続きを生成できる。

AIは最終フレームだけでなく以下を解析する。

- Story State
- Character State
- Position
- Emotion
- Object State
- Camera
- Lighting
- Motion
- Dialogue
- Music State

その上で次の展開候補を提示できる。

```text
NEXT

A. 彼女がドアを開ける
B. 背後から声が聞こえる
C. 突然照明が消える
D. 自由に指示する
E. AIに任せる
```

選択後、World Stateを引き継いで次Shot / Sceneを生成する。

---

# 16. Branch — 別展開を作る

任意のScene / Shot / FrameをBranch Pointとして保存できる。

```text
Scene 8
  ↓
Shot 12
  ↓
  ├ Branch A — ドアを開ける
  │
  ├ Branch B — 逃げる
  │
  └ Branch C — 電話する
```

用途：

- ドラマの別展開
- A/B広告
- SNSプラットフォーム別動画
- CTA違い
- 商品違い
- ターゲット別クリエイティブ

Branch作成時点のWorld StateをSnapshotとして保存する。

---

# 17. AI Editor

## 17.1 Purpose

生成されたShotを単純連結するのではなく、完成作品として編集する。

Editor Agentは以下を扱う。

- Shot Selection
- Cut Point
- Timing
- Pace
- Transition
- Reaction Shot
- Insert Shot
- Product Exposure
- Narrative Clarity
- Hook
- Retention
- CTA Timing
- Caption

## 17.2 Editor Feedback Loop

```text
Selected Takes
      ↓
Rough Cut
      ↓
Editor Judge
      ↓
問題なし ─────────→ Audio
      │
      ├ Editで修正可能 → Re-edit
      │
      └ 素材不足
             ↓
        Director Agent
             ↓
        Additional Shot
             ↓
        Video Generation
             ↓
        Editorへ戻る
```

例：

```text
EDITOR FINDING

Shot 03 is 1.2s too long.
Transition between Shot 05 and 06 lacks reaction.
Product visibility is below campaign requirement.
Opening 3 seconds lack a strong visual hook.

ACTION
- shorten Shot 03
- generate reaction insert
- add 1.5s product hero shot
```

---

# 18. Audio Production

映像編集と連動して音声制作を行う。

対象：

- Dialogue
- Voice Generation
- Narration
- Lip Sync
- BGM
- Sound Effects
- Foley
- Ambience
- Mixing
- Loudness / Platform Optimization

Audio Director AgentはScene Emotionと編集テンポを参照する。

映像尺変更時には必要に応じて音楽・Narration・SEを再構成する。

---

# 19. Production Templates

コアエンジンは共通とし、コンテンツ種類によってAgentの評価軸・優先度・Workflowを変更する。

## 19.1 PRODUCT / CM

優先項目：

1. Product Accuracy
2. Brand Guideline
3. Logo / Package Accuracy
4. Key Message
5. Hook
6. Product Exposure
7. CTA
8. Visual Quality

## 19.2 DRAMA

優先項目：

1. Story
2. Acting
3. Character Identity
4. Continuity
5. Dialogue
6. Emotion
7. Camera
8. Editing Rhythm

## 19.3 ANIME

優先項目：

1. Character Design
2. Style Consistency
3. Line / Shape Consistency
4. Animation
5. Expression
6. World Design
7. Continuity

## 19.4 SNS / SHORT VIDEO

優先項目：

1. Opening Hook
2. Retention
3. Tempo
4. Message Clarity
5. Caption
6. Platform Format
7. CTA

---

# 20. Agent Architecture

```text
                    ┌──────────────────────┐
                    │ Orchestrator Agent   │
                    └──────────┬───────────┘
                               │
       ┌───────────────────────┼────────────────────────┐
       │                       │                        │
Producer Agent           Director Agent           Editor Agent
       │                       │                        │
Writer Agent             Storyboard Agent         Audio Director
                               │                        │
                         Art Director              Final QC
                               │
                         Prompt Planner
                               │
                         Model Adapter
                               │
                    Image / Video Models
                               │
                         Video Judge
                               │
                         Repair Agent
```

## 20.1 Orchestrator Responsibilities

Orchestratorは制作全体の状態機械として機能する。

判断対象：

- 次にどのAgentを実行するか
- 人間確認が必要か
- 生成を続行するか
- Retakeするか
- 部分修正するか
- 別モデルを利用するか
- コスト上限に達したか
- 編集で修正可能か
- 撮影工程へ戻る必要があるか
- Project Stateを更新するか

---

# 21. Project Data Model

```text
Project
├── Brief
├── Production Template
├── User Preferences
├── Confirmation Policy
├── Budget / Quality Policy
├── World State
├── Assets
├── Characters
├── Products
├── Locations
├── References
├── Script
├── Scenes[]
│    ├── Scene State
│    └── Shots[]
│         ├── Shot Spec
│         ├── Prompt Versions[]
│         ├── Takes[]
│         ├── Judge Results[]
│         ├── Selected Take
│         └── State Diff
├── Edit Timeline
├── Audio Timeline
├── Branches[]
└── Exports[]
```

---

# 22. Core State Machine

```text
DRAFT
 ↓
PLANNING
 ↓
VISUAL_DESIGN
 ↓
AWAITING_APPROVAL (optional)
 ↓
GENERATING
 ↓
JUDGING
 ├ FAIL → REPAIRING → GENERATING
 └ PASS
      ↓
EDITING
      ↓
EDIT_JUDGING
 ├ NEED_SHOT → PLANNING / GENERATING
 ├ RE_EDIT → EDITING
 └ PASS
      ↓
AUDIO
      ↓
FINAL_QC
 ├ FAIL → Relevant Stage
 └ PASS
      ↓
READY
      ↓
EXPORTED
```

ユーザーは任意の段階でDIRECTOR MODEに入り、状態を修正できる。

---

# 23. UI Concept

## 23.1 Chat Workspace（標準画面）

標準画面は1プロジェクト＝1本のチャットスレッドとする。ユーザーの操作は原則すべて、このスレッドへの発言とスレッド内カードへのボタン操作で完結する。Scene/Shot/Take の階層、Inspector、Take比較、タイムライン、Branch は補助ビュー（§23.3）であり、使わなくても最後まで制作できる。

```text
┌────────────────────────────────────────────────────┐
│ PROJECT: Serum Commercial     [ ⋯ 補助ビュー ] Export │
├────────────────────────────────────────────────────┤
│ あなた: 美容液の縦型15秒CMを作りたい。上品な雰囲気で │
│                                                    │
│ AI: 確認です。ターゲットは誰ですか？                │
│     [ 20代女性 ] [ 30〜40代女性 ] [ AIに任せる ]    │
│                                                    │
│ AI: コンセプトと脚本を作りました。                  │
│     ┌──────────────────────────────┐              │
│     │ 脚本 v1（3 Scene / 15秒）     │              │
│     │ [ 承認 ]  [ 修正を依頼 ]      │              │
│     └──────────────────────────────┘              │
│                                                    │
│ AI: Scene 02 を生成しました。                       │
│     ┌──────────────────────────────┐              │
│     │ ▶ Take 2（AI Pick / 0.91）    │              │
│     └──────────────────────────────┘              │
│                                                    │
│ あなた（上のカードに返信）: 商品に寄って。          │
│   ラベルは固定して、高級感を強めて                  │
├────────────────────────────────────────────────────┤
│ [＋] 指示を入力…                          [ 送信 ] │
│ MODE [ AUTO | DIRECTOR ]   Quality: Balanced        │
└────────────────────────────────────────────────────┘
```

- **Brief は会話で作る**: 最初の発言から Creative Brief を抽出し、不足項目は AI が質問する（選択肢ボタンと「AIに任せる」を併記）。確定した Brief は補助ビューで確認・編集できる
- **確認ポイントはスレッド内カード**: Confirmation Level で設定した確認ポイント（脚本/Main Visual/Rough Cut/Final 等）と Escalation は、承認カードとして表示する。ボタン（承認 / 修正を依頼）か、自然言語の返答で応答できる。応答があるまで Core State Machine は AWAITING_APPROVAL に留まる（§22）
- **発言の対象**: 指示の対象 Scene/Shot/Take は、カードへの返信（引用）で明示するか、直前の文脈から AI が解決する。解決できない場合は AI が聞き返す。§24 の Command Interpreter がこの解決を含めて構造化操作へ変換する
- **MODE / Quality**: 入力欄の下に小さく置く。AUTO では AI が主導し、DIRECTOR では指示した箇所だけを直して待機する。切替はいつでも可能で、状態は保持される
- **添付**: 画像・動画をドラッグ&ドロップまたは［＋］で添付でき、Assets/References として Brief に取り込まれる
- **非同期**: 生成・Judge・Repair の進捗はスレッド内の進捗カードにリアルタイム表示される。入力は生成中も可能

## 23.2 スレッド内カード

| カード | 内容 | 操作 |
|--------|------|------|
| Question | Brief 補完・曖昧な指示の聞き返し | 選択肢ボタン / 自由入力 / 「AIに任せる」 |
| Approval | 確認ポイントの成果物（脚本・Main Visual・Rough Cut・Final 等） | 承認 / 修正を依頼 |
| Keyframe | Keyframe / Reference Pack 画像 | 拡大、返信で修正指示 |
| Take | 動画プレーヤー、Judge 要約、Take 状態（AI Pick 等） | 再生、返信で指示、Take比較を開く |
| Progress | 生成・Judge・Repair の進捗 | なし（自動更新） |
| Escalation | Retry Policy 上限到達などで AI が判断を求める内容 | 選択肢ボタン / 自由入力 |

## 23.3 補助ビュー

チャットの横に開く/別ページで開くビューで、スレッド内カードや［⋯］メニューから到達する。いずれも操作結果は同じ Project の状態に反映され、スレッドにも発言またはカードとして記録される。

- **Inspector**（選択中 Shot の詳細）: Shot Objective / Duration / Character / Acting / Camera / Lighting / Product / Continuity / References / Prompt / Generation Model / Take List / Judge Results / Locks。各項目は AUTO / MANUAL / LOCK を切替可能
- Take 比較、Timeline、Branch ツリー、World State、Cost、Settings

---

# 24. Natural Language Director Commands

自然言語入力をCommand Interpreterが構造化操作へ変換する。入力はチャット画面（§23.1）の発言で、対象 Scene/Shot/Take はカードへの返信または直前の文脈から解決し、解決できない場合は聞き返す。

### Example 1

> この顔は好きだから固定。

```text
Action:
LOCK Character_A.face_identity
```

### Example 2

> カメラ動かしすぎ。もっと静かに。

```text
Action:
Update Shot.camera.movement
Regenerate current shot
Preserve character / acting / environment
```

### Example 3

> このカットから別展開を3つ作って。

```text
Action:
Create Branch Snapshot
Generate 3 narrative continuations
```

### Example 4

> 商品をあと1秒長く見せて。

AIが文脈から、編集変更で済むか追加Shotが必要かを判断する。

---

# 25. Quality / Cost Controls

ユーザーは生成方針を選択できる。

### FAST

- 少ないTake
- Retake最小化
- 高速モデル優先

### BALANCED

- 標準Take数
- 重大Issueを自動修正
- 品質とコストのバランス

### MAX QUALITY

- 複数Prompt Candidate
- 複数Take
- 厳格Judge
- 必要に応じてモデル切替
- Editorによる追加撮影を積極許可

追加設定：

- Project budget ceiling
- Shot budget ceiling
- Max retries
- Max generation time
- Minimum quality thresholds

---

# 26. Safety and Brand Constraints

Project / Organization単位で制作制約を登録可能とする。

例：

- Brand Guidelines
- Forbidden Expressions
- Required Disclaimer
- Product Claims
- Logo Rules
- Character Restrictions
- Music / Asset Rights Metadata
- Platform Rules

これらはPrompt生成だけでなくJudge / Final QCにも適用する。

---

# 27. MVP Scope

最初からすべてを実装しない。

## MVP 1 — Autonomous Shot Studio

目的：1本の短尺動画をAIが自律制作できることを検証。

実装：

- Chat Workspace（チャット中心UI、§23）
- Creative Brief
- AUTO / DIRECTOR切替
- Scene / Shot / Take
- Keyframe Generation
- Prompt Autopilot
- 1〜2 Video Model Adapter
- Video Judge
- Retake Loop
- Basic World State
- Simple Timeline Editing
- Final Export

## MVP 2 — Production Memory

追加：

- Character Lock
- Product Lock
- Location State
- Continue
- Branch
- Reference Library
- Partial Regeneration
- Series / Episode Memory

## MVP 3 — AI Production Studio

追加：

- Advanced Editor Agent
- Editor → Reshoot Loop
- Audio Director
- Multi-model routing
- Brand Templates
- Team Collaboration
- Production Analytics

---

# 28. MVP Success Metrics

単なる生成回数ではなく「完成作品まで到達する能力」を測定する。

主要指標：

- First-pass Shot Acceptance Rate
- Final Shot Acceptance Rate
- Average Retakes per Shot
- Human Interventions per Finished Video
- Time to First Rough Cut
- Time to Final Video
- Cost per Accepted Second
- Character Continuity Failure Rate
- Product Accuracy Failure Rate
- Prompt Adherence Rate
- AUTO Completion Rate
- User Override Rate
- Export / Completion Rate

特に重要なNorth Star候補：

> **人間の細かな生成操作なしで、採用可能な完成動画に到達した割合**

---

# 29. Technical Design Principles

### 29.1 Model-Agnostic Core

Project State / Shot Spec / Judge Resultを特定生成モデルのPrompt形式に依存させない。

### 29.2 Structured State Before Prompt

Prompt文字列そのものをSource of Truthにしない。

Source of Truth：

```text
Creative Intent
World State
Shot Spec
Constraints
References
```

Promptはそれらから生成される一時的な実行形式とする。

### 29.3 Every Generation Is Traceable

各Takeについて以下を保存する。

- Source Shot Spec
- World State version
- Prompt version
- Model
- Model parameters
- References
- Output
- Judge result
- Repair history

### 29.4 Reversible Decisions

ユーザー・AI双方の主要変更をVersion化し、以前のShot / Take / Branch / Stateへ戻れるようにする。

---

# 30. Product Definition

AI Video Production Agentを一言で定義すると：

> **「プロンプトから動画を出すAI」ではなく、「作品を完成させるために、自分で考え、撮り、観て、撮り直し、編集するAI制作スタジオ」。**

ユーザーはプロンプトエンジニアではなく、必要に応じて **企画者・監督・クライアント** の席に座る。

AIは単なるGeneratorではなく、Producer / Director / Camera / Editor / QCとして動く。

最終的な体験は以下を目指す。

```text
IDEA
 ↓
「こんな動画を作りたい」
 ↓
AI PRODUCTION STUDIO
 ↓
必要なところだけ人間が監督
 ↓
FINISHED VIDEO
```

---

# 31. Core Differentiators — Summary

本プロダクトの差別化要素を最終的に7つに集約する。

1. **AUTO MODE** — Briefから完成動画まで自律制作
2. **DIRECTOR MODE** — 自然言語でAI制作チームを監督
3. **PROMPT AUTOPILOT** — 制作意図から詳細な生成指示を自動生成
4. **WORLD STATE** — キャラ・商品・世界・物語の状態をShot間で維持
5. **AI VIDEO JUDGE** — 生成物をAI自身がコマ単位で評価
6. **TAKE / RETAKE LOOP** — 問題を分析し、自動で撮り直す
7. **AI EDITOR LOOP** — 編集時に素材不足を検知し、再撮影まで戻る

この7つを中心に据えることで、基盤となる画像・動画生成モデルが更新されても、その上位レイヤーである「映像制作OS」として継続的な価値を持つ設計とする。
