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

必須 7 項目（Content Type / Objective / Subject / Target / Duration / Aspect Ratio / Mood）が不足している場合、AI は自動補完せず 1 つずつ質問し、ユーザーが値を答えるか「AIに任せる」を選ぶまで PLANNING は始まらない。Assets / References / Additional Instruction は任意（§23.1）。

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

> **実装の範囲**: Orchestrator が自動で進めるのは Brief → Producer → Writer → Director → Storyboard（Scene / Shot Planning、Project は PLANNING → VISUAL_DESIGN）までである。Art Director 以降（Reference Pack・Keyframe・Image / Video Generation）は Shot 単位でユーザーが起動し、起動した Shot では AI Video Judge → Repair / Retake → AI Pick が AUTO MODE で連鎖する。AUTO MODE では、トランクの全 Shot にファイル付き Selected Take が揃うと Rough Cut の作成と Editor Judge が自動で始まる。確認ポイントを通過したあと（承認カードの回答後、または確認不要の設定で AUDIO に入ったあと）は Audio と Final QC まで自動で進む。承認カードが未応答のとき、Editor Judge が Re-edit のとき、Final QC が FAIL のときはそこで止まる（Editor Judge が `need_shot` のときは、AUTO MODE では追加 Shot の要求が自動で始まり、Retry Policy の範囲で繰り返し、上限に達したら Escalation で止まる。MANUAL ではユーザーが要求する）。Export は READY の動画に対するユーザーの操作である。Project 全体を一括で生成する段は持たない。

---

## 4.4 Confirmation Levels

AUTO MODEでは、人間が確認する頻度を選択できる。

### FULL AUTO

制作開始後、Finalまで原則確認しない。

### KEY CHECK

主要マイルストーンのみ確認する。

標準ポイント：

1. Concept / Script
2. Rough Cut
3. Final

> Main Visual / Character / Product の確認は、Art Director 以降が Shot 単位の手動起動であり、承認カードを出す箇所が無いため標準ポイントに含めない。

### CUSTOM CHECK

ユーザーが確認ポイントを自由設定する。選べるのは、パイプラインが承認カードを出す Script・Rough Cut・Final（`script` / `rough_cut` / `final`）。

例：

- Scriptのみ確認
- Rough Cutのみ確認
- Finalのみ確認

> キャラクターデザイン・商品Key Visual・各Scene完成時の確認（`character_design` / `product_key_visual` / `scene_complete`）と Main Visual（`main_visual`）は、該当する成果物の完了を検知して承認を求める段が無いため、選択肢に出さない。Art Director 以降の自動実行を実装する際に追加する。

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

> **実装の対応範囲（Editing / Music / Dialogue）**: Editing が MANUAL / LOCK の Shot は、Rough Cut を作り直しても（`POST /projects/:id/edit/rough-cut`）ユーザーが編集したクリップ（In / Out・字幕・Transition）をそのまま残し、Editor Agent の Pace / Hook / CTA Timing / Caption の調整もそのクリップには行わない。Music が MANUAL / LOCK の Project は、音声を作り直しても（`POST /projects/:id/audio/generate`）直前の Audio Timeline の BGM を引き継ぎ、BGM の再計画・再生成（と課金）をしない。Dialogue は Writer が PLANNING で書く脚本のセリフであり、Shot が存在した後に AI がセリフを書き換える経路が無いため、常に変更されない（Audio Director は脚本のセリフをそのまま読む）。

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

複数Candidateを並列生成し、JudgeがBest Takeを選択。予算が 3 Take 分あれば Stability First / Acting First / Dynamic Camera の 3 戦略すべて。1〜2 本だけ収まるときは先頭からその本数だけ生成し、1 本も収まらなければ生成しない（402）。

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

実装では「Shot 完了」を「Shot の Take を生成し終えた時点」とし、通常の生成と Repair の再生成では抽出は AI Video Judge の判定を待たない（Candidate Generation は Best Take を選ぶために Judge を待ち、その後に抽出する）。State Extractor の入力は Take の映像ではなく、その Take を生成した Canonical Shot Spec・Scene 情報・現在の World State（と LOCK）である。したがって World State は、採用された Take の映像から読み取った最終状態ではなく、生成を要求した Spec から推定した「その Shot が終わったときの状態」になる。

- 通常の生成では、Take を 1 本以上作れた時点で 1 回抽出する。Candidate Generation（Quality / Balanced）では、選ばれた Candidate の Spec から抽出する。
- Judge が FAIL にして撮り直す場合（Repair）は、Shot Spec が変わる Repair（Prompt 修正・Partial Regeneration）の Take を生成した直後にもう一度抽出し、新しい `world_state_version` を追記する。Judge で FAIL になった Take の Spec から追記された版も消さない（World State は追記のみで、版は履歴として残る。`spec/architecture/er.md` §4）。
- 抽出に失敗しても Take の生成は失敗させない（World State は更新されず、次の Shot は現在の World State から始まる）。

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

**M1 の範囲**: 上記のうち Character の `hairstyle/wardrobe/emotion/position/current_action`、Product の `orientation/condition`、Camera、Story を State Extractor が更新する。次の Shot の Prompt Planner は最新の World State（前 Shot が残した状態）を入力に使う。Location State（時間帯・照明・天気の自動更新）と LOCK 強化は M2。保持形式・diff 形式は `spec/architecture/er.md` §4 を参照。

**M2 の範囲（Location State・属性単位の LOCK、Issue #15）**:

- **Location State**: `time`（時間帯）・`lighting_state`・`weather` は State Extractor が Shot 完了ごとに更新する（更新対象は、その Shot の Canonical Shot Spec が指す Location のみ）。次の Shot の Prompt Planner が最新の World State を入力にすることで Shot・Scene をまたいで引き継がれる。`layout_reference` はライブラリ（§10.1）の画像をユーザーが割り当てる項目で、AI は変更しない。いずれもユーザーが編集・LOCK できる
- **属性単位の LOCK**: World State の項目ごとに AUTO / LOCK を切り替える（MANUAL は使わない。ユーザーが直接編集した値を守りたいときは LOCK）。LOCK 対象は、Character が編集項目（`hairstyle/wardrobe/voice/personality/emotion/position/current_action`）と `identity`、Product が編集項目（`material/orientation/condition`）と `geometry`・`label`、Location が編集項目（`time/lighting_state/weather`）と `layout_reference`、Camera（`lens/position/angle/movement`）と Story（`scene/character_goal/known_information/previous_event`）の全項目。LOCK 項目は State Extractor が diff に含めず、`ApplyStateDiff` も書き込まず、ユーザー編集は 423 になる
- **LOCK 違反の判定**: AI Video Judge は、Shot が使う Character / Product / Location の LOCK を次の評価軸に対応付け、その軸の Issue が severity medium 以上なら LOCK 違反とし、スコア・確信度に関係なく Take を FAIL にする（§13）。`identity` → `character_identity`・`face`、`wardrobe/hairstyle` → `character_consistency`、`geometry` → `product_geometry`、`label` → `logo_text_accuracy`、`orientation/material/condition` → `product_accuracy`、Location の `time/lighting_state` → `lighting`、`weather/layout_reference` → `background_consistency`。Shot 自身に LOCK したパラメータ（`PUT /shots/:id/locks`）も同様に対応付ける: Character Identity → `character_identity`・`face`、Product Geometry → `product_geometry`、Acting → `acting`、Camera → `camera_motion`、Lighting → `lighting`（Dialogue・Prompt・Video Model・Take Selection・Editing は評価軸を持たないため対象外）

## 9.4 Notes（制作メモ、M2）

World State（構造化状態）に収まらない文脈（登場人物の背景・世界観・口調・トーン・伏せておきたい情報など）を、自由記述で保持する。

- **位置づけ**: Notes は Source of Truth ではない（§29.2）。構造化状態（World State / Shot Spec / Constraints / References）を置き換えず、Reasoning LLM に渡す補助的な文脈として使う
- **スコープ**: Project 単位と Series 単位。Series のメモは Episode 開始時に Project へコピーし（`origin_id` で由来を保持）、Series 側の更新は既存 Episode へ自動伝播しない（F-29 と同じ規則）
- **登録**: チャットの発言（§24 Example 5）または補助ビューの Notes。チャットからの登録は Project スコープ。Series スコープは Series 側の Notes 画面で登録する。メモは追記のみのバージョンで保持し、以前のバージョンへ復元できる（§29.4）
- **チャットからの登録の判定**: 「覚えておいて」「メモして」「remember」などの依頼語を含む発言は、AUTO / DIRECTOR のどちらのモードでも Command Interpreter が読み、`create_note` を Project メモとして作成する（`source=chat`）。AUTO MODE ではこの操作だけを実行し、Shot への指示は実行しない。依頼語を含まない発言はメモにしない
- **参照先**: Writer / Director / Prompt Planner のみ。ピン留めしたメモは常に、それ以外は推論 LLM のコンテキスト予算の範囲で関連度順に渡す。アーカイブ済みのメモは渡さない
- **渡さない先**: 画像/動画生成モデル（モデル非依存の Canonical Shot Spec 経由でのみ影響する、§29.1）、AI Video Judge、Final QC。メモは判定基準にならない。守らせたいルールは Brand Constraint（§26）に登録する
- **Traceability**: Prompt Version に、Shot Spec 生成時に参照したメモのバージョンを記録する（§29.3）

### 昇格（World State への反映）

```text
Note Version（新規 / 更新）
      ↓
State Extractor（現在の World State と比較）
      ↓
昇格提案（構造化できる項目のみ）
      ↓
Approval カード（非ブロッキング）
      ↓ 承認
新しい World State Version
```

- **抽出**: 構造化フィールド（髪型・衣装・性格・Location の時間帯等）に落とせる項目だけを提案する。背景や経緯など構造化できない内容は提案せず、メモのまま保持する
- **カード**: Approval カード（§23.2）で提示する。Confirmation Level に関わらず**非ブロッキング**とし、Project は AWAITING_APPROVAL に遷移しない。応答がなくても制作は継続し、未応答の提案は反映されない（メモは参照専用のまま）。提案は1メモバージョンにつき1枚とし、同じメモバージョンから再提案しない
- **LOCK**: LOCK 中の項目は提案から除外し、カードに「LOCK 中のため未反映」と表示する。承認時にも再判定し、LOCK 中の項目は適用しない
- **反映**: 承認すると主系列（トランク）の World State に新しい World State Version を追記する（F-28 で復元可能）。修正依頼は却下として扱い、メモは残る。Branch 側の World State への反映は通常の World State 編集で行う
- **対象**: Project スコープのメモのみ。Series のメモは Episode へコピーされた後の Project メモが昇格の対象になる

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

## 10.1 Reference Library（M2）

Reference Pack の素材を Shot・Scene をまたいで再利用するための、Project 単位の参照画像の保管庫。`asset.type=reference` の画像に、種別・名前・タグを付けて保持する。

### 登録

- **自動登録**: Art Director が Reference Pack のために生成した Character / Product / Location / Style / Lighting Reference の画像は、生成時にライブラリへ登録される（`source=generated`、由来の Shot を記録）。Keyframe は Shot 固有なので登録しない
- **アップロード**: ユーザーが画像（jpg / png / webp / gif、1枚 20MB まで）に種別（character / product / location / style / lighting）と名前（必須、100文字以内）、タグ（任意、最大10個、各30文字以内）を付けて登録する（`source=uploaded`）

### 検索

名前とタグの部分一致（`q`、大文字小文字を区別しない）、種別（`kind`）、タグの完全一致（`tag`）で絞り込む。新しい登録順に並べる。

### 再利用

- **Shot への追加（ピン留め）**: ライブラリの画像を Shot の Reference Pack に加える。同じ種別・同じ名前の参照が Pack にあれば置き換える。ただし Pack には Style と Lighting が各 1 枚なので、この 2 種別は名前に関係なく種別だけで置き換える（Art Director の Style は名前を持たない）。ピン留めした参照は Reference Pack の再生成でも残り、再生成しない（Art Director が同じ種別・名前の参照（Style / Lighting は種別だけ）を計画しても新たに生成せず、ピン留めした画像を使う。コストは発生しない）
- **World State への割り当て**: Character の `face_reference` / `body_reference`、Product の `geometry_reference` / `label_reference`、Location の `layout_reference` に、同じ種別のライブラリ画像を割り当てる。割り当てた画像は、同じ名前の Character / Product / Location の Reference Pack 生成で（`face` / `geometry` / `layout` を）新たに生成せずに使い、AI Video Judge の顔参照と画像生成の入力にも使う。対応する LOCK（後述）が掛かっている項目への割り当ては拒否する
- **削除**: ライブラリから外す。画像そのものと、すでに Shot に付いた参照は残る

Series をまたぐ再利用は F-29 の範囲（本節は Project 内）。

### World State の LOCK との対応（M2）

`face_reference` / `body_reference` は `identity`、`geometry_reference` は `geometry`、`label_reference` は `label`、`layout_reference` は `layout_reference` の LOCK に従う。

---

# 11. Video Generation Engine

Generation OrchestratorがShot Spec、Reference、World State、コスト、モデル能力をもとに使用モデルと生成戦略を決定する。

M3のMulti-model routingでは、これに加えてShotの性質とProject内の過去のJudge実績（モデルごとの点数・合格率）を使い、Quality Policyの割り当てを出発点にShotごとに動画モデルを入れ替える（`spec/architecture/model-adapters.md` §13）。選択理由はTake単位で保存する（§29.3）。

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

Previous Final Frame / Target First / Last Frame は、現状では次のように扱う（実装の現状に合わせた記述）。

- **First Frame**: Reference Pack の Keyframe（Art Director が用意した生成画像）だけを動画モデルの First Frame として渡す。ユーザーが選んだ画像を First Frame にする経路は無い。Model Adapter がモデルごとに送信する（Kling 3.0 `image`、Seedance 2.0 `first_frame`）。Keyframe があるときも、自由な参照画像を受け取れるモデル（Seedance 2.0）には Reference Pack の他画像（Character / Product / Location / Style / Lighting）を First Frame と同時に渡す（上限 9 枚、`reference_image`）。Kling 3.0 と Runway Gen-4.5 は自由な参照画像を受け取らないので、First Frame（Kling は Last Frame も）だけを送る。Generation Orchestrator のモデル選択は、Keyframe があるとき First Frame に対応するモデルを適合（fit）で優先する。
- **Previous Final Frame**: 前の Shot の最終フレームを ffmpeg で取り出して次の Shot の First Frame にする処理は行わない。`continuity.start_from_previous_final_state: true` は、Prompt に「前の Shot の最終状態から始まる」という文を加え、World State（前 Shot までの変化）と Judge の `scene_continuity` 評価で連続性を担保する。また、この指定がある Shot ではモデル選択の Continuity 軸の重みを上げる。
- **Last Frame**: モデル側（Kling 3.0 `image_tail`・Seedance 2.0 `last_frame`）は送信に対応しているが、Shot Spec に Last Frame 指定の項目がないため、現状は渡さない。

前の Shot の最終フレームを First Frame にする機能、Last Frame の指定、および First / Last Frame のユーザー指定画像は未実装（将来の拡張）。`kind=keyframe` の画像を Reference Library へ登録することはできない（§10.1）。

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
- **閾値**: 警告 / FAIL は、その Character の参照画像同士の類似度分布から決める（ソースコードの固定値では決めない）。顔が取れた参照が 2 枚以上あり、参照同士の平均コサイン類似度がキャリブレーション下限（Judge 設定 `min_calibration_similarity`）以上のとき、警告閾値は平均 × `calibration_warn_scale`、FAIL 閾値は平均 × `calibration_fail_scale` とする（スケールは Judge 設定で、閾値そのものは分布から出る）。参照が 1 枚、または分布が使えない（参照が 2 枚未満、または平均が下限未満）ときは数値の警告 / FAIL を出さない（`calibrated = false`、`evaluated = false`）。類似度は記録し、合否は定性評価に任せる
- **LOCK 連動**: Character Identity が LOCK のとき、FAIL 閾値を割った Take は総合スコアに関係なく FAIL とする（§13）
- **Repair**: `face_identity_drift` は Reference Reinforcement を優先候補とする
- **Series**: Episode をまたいでも Series 正本の `face_reference` を基準に計測する（F-29）
- **実行**: ローカル実行。顔画像は外部へ送信しない。YuNet / SFace と ONNX Runtime（purego、CGO なしの Go 単一バイナリ）は M1 で検証済み（`spec/architecture/model-adapters.md` §10.1）。残る確認は SFace の学習データの権利関係（同 §8）。Windows では ONNX Runtime のバインディングが対応しないため数値評価は使えず、定性評価にフォールバックする（`spec/requirements/non-functional.md` §1）

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

### Retry Policy の適用（M1）

- Max retries per Shot は、新しい Take を作った再生成系の Repair の数で数える。Max cost per Shot は Shot の予算上限（`shot_budget_ceiling`）で、次の再生成の見積りが上限を超えるなら再生成しない。上限に達したら Escalation（チャットのカード）で人間に判断を求める
- Minimum acceptance score は、Judge が付けた Production Template の軸別点数の総合点と各軸の最低点に適用する。LOCK 違反・Critical constraints・上限判定は決定的ルールで行い、LLM や Jev を経由しない
- 戦略は決定的ルールで候補を絞り、決まらないときだけ Repair Agent（LLM）が選ぶ（`face_identity_drift` は Reference Reinforcement を優先）。Jev は M1 ではシャドー実行のみ（`spec/architecture/model-adapters.md` §4.5・§11）
- Partial Regeneration は、Judge の `preserve`（良かった部分）とLOCK された部分を保ったまま Shot 全体を作り直す（M1・実装済み）。M2 ではこれに、問題のあった時間範囲だけを差し替える「つなぎ込み」を加える。動画生成モデルはクリップ全体しか作れないため、区間だけの再生成は「Shot 全体を新しい Take として作る → 修復元の Take の問題区間だけをその Take の同じ区間に差し替えた Take を、もう 1 本新しい Take として足す」ことで実現する。判定は §13 の下の「Partial Regeneration の範囲（M1 と M2）」を正とする

### Partial Regeneration の範囲（M1 と M2）

| | M1（F-14） | M2（F-14 の Partial Regeneration の拡張） |
|---|---|---|
| 新しい Take | Shot 全体を作り直した 1 本 | 同左に加え、問題区間だけを差し替えてつないだ 1 本（`take.model_parameters.kind = partial_take`、`source = partial_regeneration`、`regenerated_take_id`）。区間を差し替えない条件（下）のときは足さない |
| 保持するもの | Judge の `preserve` と LOCK された部分（Shot Spec の該当部分を Prompt Planner が固定する） | 同左に加え、問題区間の外側は修復元の Take の映像そのもの |
| 起動 | Repair Agent が戦略として選ぶ（AUTO は自動、DIRECTOR は `POST /takes/:id/repair`） | 同左。加えて DIRECTOR MODE の自然言語指示（「2〜3秒目のカメラだけ直して」→ `partial_regenerate`） |
| 既存 Take | 変えない | 変えない（修復元・再生成した Take・つないだ Take は別々の行）。つないだ Take は候補で、Shot の選択 Take を置き換えない。ユーザーは Partial Take Use（§14）でさらに区間を組み替えられる |

つなぎ込みの規則（決定的。LLM を使わない）:

- 差し替える区間は、Judge の Issue の `time_range` を前後 0.2 秒ずつ広げたもの（クロスフェードの境目が問題の映像にかからないようにする）。**すべての Issue が `time_range` を持つとき**だけつなぐ。Shot 全体の Issue が 1 つでもあれば、つながずに Shot 全体の Take だけを残す
- 区間は 0.3 秒未満にならないよう広げ、0.3 秒未満の間隔しかない区間は 1 つにまとめる。先頭・末尾に 0.3 秒未満しか元の映像が残らないときはその部分も再生成した映像にする。区間が Shot のほとんどを占めて元の映像が残らないとき、Take が短すぎるとき、必要な部品が 8 個（Partial Take Use の上限）を超えるときは、つながずに Shot 全体の Take だけを残す
- ffmpeg が無いときはつなぎ込めない。Repair は Shot 全体の Take だけを作り（M1 と同じ）、DIRECTOR MODE の区間指定の再生成は「ffmpeg が必要」と返して何も作らない
- つないだ Take は自動で Judge される（AUTO MODE の Repair / Retake Loop はそれも含めて続ける）
- つないだ Take の Prompt version とモデルは、最初の区間の Take のものを引き継ぐ（Partial Take Use も同じ）。Judge はその Prompt version の Shot Spec で判定し、つないだ区間の由来は `model_parameters.segments` と `regenerated_take_id` で辿る。区間ごとの Prompt version は持たない
- DIRECTOR MODE の区間指定の再生成は、ユーザーが求めた操作なので Retry Policy の再試行回数には数えない（予算上限は適用する）。元の Take は、ユーザーが選んだ Take、なければ AI Pick、なければ最新の（除外されていない）Take

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

Partial Regeneration（§13）が作るつなぎ込みも同じ合成（区間の境目をクロスフェードする）を使う。

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

実装では、Continue Agent への入力は Brief・Scene/Shot の目的・Storyboard・Canonical Shot Spec・World State のテキストに加え、ffmpeg が使えるときは起点にする Take（選択済みの Take、なければ AI Pick、なければ最新の却下されていない Take）の**実際の最終フレーム**の画像である。画像と Shot Spec が食い違うときは画像を正とする。ffmpeg が無い、または最終フレームを取り出せないときは、Shot Spec と World State のテキストだけから解析する（Continue は失敗させない）。次の Shot の First Frame は、この最終フレームではなく Art Director の Keyframe のままである（§11）。

その上で次の展開候補を提示できる。

```text
NEXT

A. 彼女がドアを開ける
B. 背後から声が聞こえる
C. 突然照明が消える
D. 自由に指示する
E. AIに任せる
```

実装では、候補は 1〜3 件で、重複・空・100 文字を超える候補は捨てられるため 3 件に満たないことがある。その場合、自由指示と「AIに任せる」の記号は最後の候補の次の記号から振り直す（候補 2 件なら C が自由指示、D が「AIに任せる」）。

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

Branch作成時ではなく分岐点（Branch Point）のWorld StateをSnapshotとして保存し、Branch専用のWorld Stateの版として持つ。

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
├── Notes（M2）
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
AWAITING_APPROVAL (optional: 脚本の確認ポイント)
 ↓
VISUAL_DESIGN
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
AWAITING_APPROVAL (optional: Rough Cut の確認ポイント)
      ↓
AUDIO
      ↓
FINAL_QC
 ├ FAIL → Relevant Stage
 └ PASS
      ↓
AWAITING_APPROVAL (optional: Final の確認ポイント)
      ↓
READY
      ↓
EXPORTED
```

ユーザーは任意の段階でDIRECTOR MODEに入り、状態を修正できる。

> **Project 状態と Shot 状態（実装の範囲）**: 上の状態遷移のうち、Project の状態として実際に遷移するのは DRAFT → PLANNING → VISUAL_DESIGN（Orchestrator）、確認ポイントでの AWAITING_APPROVAL、および EDITING 以降（EDIT_JUDGING・AUDIO・FINAL_QC・READY・EXPORTED）である。EDITING の開始は `POST /projects/:id/edit/rough-cut` だけではない。ユーザーが Rough Cut を作る／編集したときに加え、AUTO MODE ではトランクの全 Shot に Selected Take が揃った時点で Rough Cut と Editor Judge が自動で始まり EDITING に入る。開始時、VISUAL_DESIGN からは GENERATING → JUDGING → EDITING を一括で辿る（全 Shot に Selected Take があることが前提）。確認ポイント通過後の AUDIO と Final QC も AUTO MODE では自動で進み、Export はユーザー操作である。GENERATING / JUDGING / REPAIRING は Shot 単位（`shot.state`、§13）で進み、Shot の生成・Judge・Repair は Shot ごとに並行して起動されるため、Project 全体の状態としては遷移させない（Project は VISUAL_DESIGN のまま）。例外は Reshoot Loop の追加 Shot（Project を GENERATING にする）と、Escalation への回答後（Project は REPAIRING になり、Editor Judge／編集操作で EDITING へ戻る）である。

図の AWAITING_APPROVAL は確認ポイント（脚本・Rough Cut・Final）の位置を示す。AWAITING_APPROVAL に遷移するのは、確認ポイントの Approval カードと Escalation カードのみとする。Notes の昇格提案カード（`ref_type=note_promotion`、§9.4）は非ブロッキングで、Core State Machine の状態を変えない。

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
- **確認ポイントはスレッド内カード**: Confirmation Level で設定した確認ポイント（脚本/Rough Cut/Final）と Escalation は、承認カードとして表示する。ボタン（承認 / 修正を依頼）か、自然言語の返答で応答できる。応答があるまで Core State Machine は AWAITING_APPROVAL に留まる（§22。Notes の昇格提案カードを除く）
- **発言の対象**: 指示の対象 Scene/Shot/Take は、カードへの返信（引用）で明示するか、直前の文脈から AI が解決する。解決できない場合は AI が聞き返す。§24 の Command Interpreter がこの解決を含めて構造化操作へ変換する
- **MODE / Quality**: 入力欄の下に小さく置く。AUTO では AI が主導し、DIRECTOR では指示した箇所だけを直して待機する。切替はいつでも可能で、状態は保持される
- **添付**: 画像・動画をドラッグ&ドロップまたは［＋］で添付でき、Assets/References として Brief に取り込まれる
- **非同期**: 生成・Judge・Repair の進捗はスレッド内の進捗カードにリアルタイム表示される。入力は生成中も可能

## 23.2 スレッド内カード

| カード | 内容 | 操作 |
|--------|------|------|
| Question | Brief 補完・曖昧な指示の聞き返し | 選択肢ボタン / 自由入力 / 「AIに任せる」 |
| Approval | 確認ポイントの成果物（脚本・Rough Cut・Final）。M2 では Notes の World State 昇格提案（§9.4）も同じカードで提示する。昇格提案は非ブロッキング | 承認 / 修正を依頼（昇格提案では「修正を依頼」は却下） |
| Keyframe | Keyframe / Reference Pack 画像 | 拡大、返信で修正指示 |
| Take | 動画プレーヤー、Judge 要約、Take 状態（AI Pick 等） | 再生、返信で指示、Take比較を開く |
| Progress | 生成・Judge・Repair の進捗 | なし（自動更新） |
| Escalation | Retry Policy 上限到達などで AI が判断を求める内容。回答（選択肢ボタン / 自由入力）は記録されるだけで自動実行されない。回答後は Project が REPAIRING に戻り、AI は「この判断は自動では実行されません」と返すので、ユーザーが設定の変更・Take の採用・Shot の指示の修正を画面から行う | 選択肢ボタン / 自由入力 |

## 23.3 補助ビュー

チャットの横に開く/別ページで開くビューで、スレッド内カードや［⋯］メニューから到達する。いずれも操作結果は同じ Project の状態に反映される。スレッドに発言またはカードとして記録されるのは、Scene・Shot の追加、Brief の更新と Confirmation Level の設定、Quality Policy・予算上限・Production Template の変更、編集・音声・書き出しの各工程（Editor / Audio / Export）、Review Response の取り込み、Take の部分採用（つなぎ込み）、DIRECTOR の指示、Continue による次の Shot・Scene の作成で、Take の採用/除外、LOCK の変更、Shot Spec の手動編集、World State の編集・復元、Branch の作成・改名、Notes、Brand Constraint の操作はスレッドには記録されない（各ビューの表示と履歴で確認する）。

- **Inspector**（選択中 Shot の詳細）: Shot Objective / Duration / Character / Acting / Camera / Lighting / Product / Continuity / References / Generation Model / Take List / Judge Results / Locks。モデル別プロンプトは Inspector に常設せず、各 Take の「生成の経緯」（Generation Trace、`GET /takes/:id/trace`）で確認する。各項目は AUTO / MANUAL / LOCK を切替可能
- Take 比較、Timeline、Branch ツリー、World State、Notes（M2、§9.4）、Cost、Settings

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

> **実装の範囲**: Command Interpreter が構造化できる操作は、ロック設定・Shot の再生成・部分再生成・モード切替・ノート作成・Branch 作成のみで、チャットの発言から Edit Timeline（クリップの尺など）の変更や追加 Shot の要求は導かない。これらは、タイムラインエディタでの編集（`PUT /edit-timelines/:id`）、Editor Judge の NEED_SHOT が出す追加 Shot の提案（Reshoot Loop、`POST /edit-timelines/:id/request-additional-shot`）から行う。この Example 4 の発言には「その操作にはまだ対応していません」と返す。

### Example 5（M2）

> 主人公は落ち着いた口調で、黒髪ロングにしたい。過去の火事の話は序盤では出さないで。これ覚えておいて。

```text
Action:
Create Note (Project scope)
State Extractor: propose promotion
  Character_A.hairstyle = long_black   ← 承認カードで提示
口調・「火事の話は序盤では出さない」はメモのまま保持し、Writer / Director が参照する
```

### Example 6（M2）

> 2〜3秒目のカメラの揺れだけ直して。他はそのままで。

```text
Action:
Partial Regeneration of the current take, range 2.0-3.0s
Update Shot.camera.movement
Preserve character / acting / environment
```

区間を名指しした再生成は `partial_regenerate` になる。区間を言わない「カメラ動かしすぎ」は Shot 全体の再生成（Example 2）のまま。

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

Project単位で制作制約を登録可能とする（`functional.md` UC-9 と `er.md` に合わせる。Organization エンティティは持たないため、Organization 単位の制約は設けない。複数の Project で同じ制約を使いたい場合は Project ごとに登録する）。

種類（`brand_constraint.type`）：

- Brand Guidelines
- Forbidden Expressions
- Required Disclaimer
- Product Claims
- Logo Rules
- Character Restrictions
- Music / Asset Rights Metadata
- Platform Rules

これらはPrompt生成だけでなくJudge / Final QCにも適用する。

## 26.1 強制レベルと適用の仕方

各制約は `enforcement_level` を持つ。

- **`lock`**（既定）: 違反した Take・成果物は、スコアや Judge のモデルの verdict に関係なく FAIL とする。違反の操作は HTTP 403 で拒否する
- **`advisory`**: 警告扱い。違反を Issue／指摘として記録・表示するが、FAIL にも 403 にもしない

制約の内容は自由記述（`value` の `text`）。`forbidden_expression` は 1 行に 1 つの禁止表現（大文字小文字・空白の違いを無視した部分一致）、`required_disclaimer` は表示すべき文言をそのまま書く。他の種類は文章のルールとしてモデルへ渡す。

適用箇所：

| 段階 | 適用 |
|---|---|
| Prompt Planner | 全制約を英語の指示として入力に含める（各制約の ID 付き）。生成した Canonical Shot Spec に `lock` の禁止表現が含まれれば 403（Take を作らない）。ユーザーの指示文に `lock` の禁止表現が含まれる場合も、生成・再生成・候補生成を開始する前に 403 で拒否する |
| AI Video Judge | 全制約をモデルへ渡し、違反は `type=brand_constraint_violation`・`brand_constraint_id` 付きの Issue にする。`lock` の違反は `lock_violation=true`（severity は high）で Take を FAIL、`advisory` の違反は severity low の警告で FAIL にしない。Shot Spec に `lock` の禁止表現があれば、モデルの判断に依らず決定的に `lock_violation` の Issue にする |
| Final QC | セリフ・ナレーションと各クリップの Shot Spec に、`lock` の禁止表現があれば FAIL（セリフ → AUDIO、Shot Spec → EDITING に差し戻し）。必須の免責表記がセリフ・Shot Spec のどこにも無ければ FAIL（EDITING）。各クリップの Take の Judge が出した Brand 違反 Issue（`lock_violation`）も FAIL（EDITING）。`advisory` の違反はすべて警告として指摘に残し、FAIL にしない |

Shot Spec は英語で書かれるため、Shot Spec に対する禁止表現の一致は、英語で登録された表現・ロゴ等の固有名に対して働く。日本語の禁止表現は、ユーザーの指示文、セリフ・ナレーション、映像のフレーム（Judge のモデル）に対して働く。

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
- Partial Regeneration（時間範囲の再生成とつなぎ込み、§13。M1 の Retake Loop が実装した「Judge の `preserve` を保つ Shot 全体の再生成」を、問題の区間だけを差し替える形に広げる）
- Series / Episode Memory
- Notes（制作メモ、§9.4）

## MVP 3 — AI Production Studio

追加：

- Advanced Editor Agent
- Editor → Reshoot Loop
- Audio Director
- Multi-model routing（Shot の性質・Project 内の Judge 実績・コストに応じて、Shot ごとに使う動画モデルを Quality Policy の割り当てから入れ替える。実績が無ければ M1 のポリシー別ルーティングと同じ。判断基準・切り替え条件は `spec/architecture/model-adapters.md` §13）
- Brand Templates
- Team Collaboration（Review Package によるレビュー・承認、§27.1）
- Production Analytics

## 27.1 Team Collaboration（MVP 3）

複数のメンバーで 1 本の動画をレビューし、承認できるようにする。**同時編集も、サーバー・アカウントも使わない**: 制作の書き込みは常に Project を持つ 1 つのインスタンス（オーナー）だけが行い、共有はファイルの受け渡し（非同期）で行う。各メンバーは自分のデバイスで自分のインスタンスを動かすので、「1 デバイス・1 ユーザー」の前提は変わらない。

### スコープの判定（`spec/overview.md` の「含まないもの」との関係）

| 除外項目 | 本設計での扱い |
|---|---|
| SaaS マルチテナント化・サーバー運用基盤 | 超えない。共有用のサーバー・クラウド保存・ネットワーク送受信を持たない。共有物は利用者が自分で選んだ経路（メール・チャット・共有ドライブ等）で渡すファイル 1 つ |
| リアルタイム同時共同編集 | 超えない。Project を編集できるのはオーナーだけで、レビュアーは動画の閲覧・コメント・判定だけができる。オーナーとレビュアーの状態は同期されず、レビュアーの応答はファイルで取り込む |

したがってスコープの改訂は不要で、「チーム利用は MVP3 の Team Collaboration まで対象外」の文言は「チーム利用は、非同期のファイル受け渡しによるレビュー・承認（本節）だけを対象とし、それ以外は対象外」と明確にする。

### 役割

| 役割 | できること | できないこと |
|---|---|---|
| オーナー | Project の全操作。書き出しからレビュー依頼（Review Package）を作る。レビュアーの応答（Review Response）を取り込み、コメントを解決済みにする | - |
| レビュアー | Review Package を自分のインスタンスに読み込み、動画を視聴し、時刻付きコメントを付け、判定（承認 / 修正依頼）を書き、Review Response を書き出してオーナーに返す | Project の中身（Brief・チャット・Shot・アセット等）を見ること、Project を編集すること |

同じインスタンスをオーナーとレビュアーの両方に使ってもよい（自分のレビュー依頼を自分で読み込める）。

### 認証モデル

- アカウント・ログイン・トークンを持たない。ローカルサーバーは引き続き `127.0.0.1` のみで待ち受け、CSRF トークンも維持する（`spec/requirements/non-functional.md` §5）
- レビュアーの識別は**自己申告の表示名**（1〜50 文字）。検証せず、信頼の根拠にしない。Review Response が本人のものかを確かめる署名は設けない（受け渡し経路の信頼は利用者に委ねる）
- Review Response は `review_id` でオーナー側のレビュー依頼に結び付ける。これは取り違え防止の整合性チェックであり、認証ではない。オーナーのインスタンスに存在しない `review_id`、別 Project のレビュー依頼、取り込み済みの応答（`response_id` 重複）は取り込まない

### Review Package（オーナー → レビュアー）

ZIP ファイル 1 つ。中身は次の 2 エントリだけ（allowlist）。

- `manifest.json`: `format=mam-review-package`・`version`（1）・`review_id`・`project_name`・`note`（オーナーがレビュアーに向けて書いたメモ、1000 文字以内）・`exported_at`・`video`（`file_name`・`resolution`・`aspect_ratio`・`codec`）
- `video.mp4` または `video.webm`: レビュー対象の Final Export 1 本（Final QC PASS 後の書き出しのうちオーナーが選んだもの）

### Review Response（レビュアー → オーナー）

JSON ファイル 1 つ（`format=mam-review-response`・`version`・`response_id`・`review_id`・`reviewer_name`・`decision`（`approved` / `changes_requested`）・`comment`（全体コメント、2000 文字以内）・`comments`（最大 200 件。`timecode_ms`（省略可、動画内の位置）・`body`（1〜2000 文字））・`created_at`）。判定を書いた後にだけ書き出せる。コメントや判定を変えると `response_id` が変わり、変更前のファイルとは別の応答として取り込まれる。

### レビューの状態

レビュー依頼の状態は、レビュアー表示名ごとの最新の応答から決める: 応答が無ければ `pending`（未回答）、全員の最新が `approved` なら `approved`（承認済み）、1 人でも `changes_requested` なら `changes_requested`（修正依頼あり）。承認はオーナーへの意思表示であり、Project の状態遷移（§22）や Final Export をブロック/解除しない。応答を取り込むと、Chat Workspace のスレッドに AI の発言として「レビュー結果（表示名・判定・コメント件数）」が記録される（§23.3）。

### 共有物に含めないもの（`spec/requirements/non-functional.md` §5）

Review Package と Review Response は、DB の行やファイルをそのまま渡さず、上記の項目だけを固定の構造体から組み立てる。次のものは**入らない**（テストで検証する）。

- 外部モデル API キー・認証情報（OS のセキュアストレージにだけ保存され、DB にもバンドルにも書かれない）
- 顔データ: Character の顔基準画像（`face_reference_asset_id`）・Series 正本の顔基準・Reference Library の参照画像・アップロードされた参照画像/動画・Judge の顔同一性指標。動画そのものに映る人物はレビュー対象の成果物であり、ここでいう顔データではない
- Brief・チャット履歴・Notes・World State・Shot Spec・Prompt・Brand Constraints・コスト台帳・Take・他の Export・Project ID・ファイルシステムの絶対パス

動画は `video.mp4|webm` の固定名で入れ、元のパスを名前に残さない。共有されるのは Project 名とオーナーのメモ・動画・レビュアーの応答だけである（UI で共有される内容を明示する）。

読み込み側は、allowlist 以外のエントリを含むアーカイブ、サイズ上限（動画 2GB、応答 1MB）を超えるもの、形式・バージョンが違うもの、動画の署名（MP4 の `ftyp` / WebM の EBML ヘッダー）が合わないものを拒否し、何も残さない。

### プロジェクトバンドルとの違い

プロジェクトバンドル（`spec/requirements/non-functional.md` §8）は、自分の端末間の移行・バックアップのために Project 全体（顔基準画像・アップロード素材を含む）を書き出す。**他のメンバーに渡すためのものではない**。チームでの共有には Review Package を使う。

### 画面

- オーナー: 補助ビュー「レビュー」（`/projects/:id/reviews`）でレビュー依頼の作成・パッケージのダウンロード・応答の取り込み・コメントの解決済み切替を行う
- レビュアー: プロジェクト一覧から入る「レビュー」（`/reviews`）でパッケージの読み込み、動画の視聴、コメント・判定、応答の書き出しを行う（Project を持たないので Project の画面には入らない）

---

# 28. MVP Success Metrics

単なる生成回数ではなく「完成作品まで到達する能力」を測定する。

主要指標（計算式は §28.2、表示は §28.4）：

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

### 28.1 集計の対象と用語

指標は Project 単位、またはすべての Project の合計として算出する（Project が 0 件、または分母が 0 の指標は「—」と表示し、0% とは区別する）。

| 用語 | 定義 |
|------|------|
| 着手済み Project | 状態が `DRAFT` 以外の Project |
| 完成した動画 | 状態が `READY` / `EXPORTED` の Project、または書き出し（`export`）がある Project |
| 書き出し済み | `export` 行がある Project |
| 計測対象の Shot | Take を 1 つ以上持つ Shot |
| 採用された Shot | `shot.selected_take_id` がある Shot（AUTO では Judge を通過した AI Pick、DIRECTOR ではユーザーの選択） |
| 最初の Take | Shot の最も古い Take。その最初の Judge 結果を「最初の判定」とする |
| 最新の判定 | Take の最も新しい `judge_result` |
| 人間の介入（Human Intervention） | ユーザーが生成の中身を手で操作した記録（§28.3）。承認カードの「承認」、モード切替、閲覧は含まない |
| 上書き（Override） | 人間の介入のうち、AI の判断に反するもの（§28.3） |
| 引き継ぎ | AUTO の Project を DIRECTOR MODE に切り替えたこと（`switch_to_director`）。人間の介入には数えないが、AUTO Completion Rate と North Star が見る |

### 28.2 計算式

| 指標 | 計算式 | 出どころ |
|------|-------|---------|
| First-pass Shot Acceptance Rate | 最初の判定が `pass` で、最初の Take が除外されていない Shot ÷ 最初の Take が判定された Shot | `take` / `judge_result` |
| Final Shot Acceptance Rate | 採用された Shot ÷ 計測対象の Shot | `shot.selected_take_id` |
| Average Retakes per Shot | （Repair が作った Take（`repair_action.resulting_take_id`）＋ユーザーが依頼した再生成（`human_intervention.kind=regenerate`））÷ 計測対象の Shot | `repair_action` / `human_intervention` |
| Human Interventions per Finished Video | 完成した動画の人間の介入の数 ÷ 完成した動画の数（引き継ぎは数えない） | `human_intervention` / `project` / `export` |
| Time to First Rough Cut | Project 作成から最初の Edit Timeline（Rough Cut）の保存までの秒数の平均（Rough Cut がある Project） | `project.created_at` / `edit_timeline_version` |
| Time to Final Video | Project 作成から最初の書き出しまでの秒数の平均（書き出し済みの Project） | `project.created_at` / `export` |
| Cost per Accepted Second | Cost Ledger の合計（`actual_cost` があればそれ、なければ `estimated_cost`。採用されなかった Take の費用も含む）÷ 採用された Shot の秒数の合計（採用 Take の `duration_seconds`、なければ Shot の長さ） | `cost_ledger_entry` / `take` / `shot` |
| Character Continuity Failure Rate | 最新の判定に `severity` が medium / high で `dimension` が `character_identity` / `character_consistency` / `face` の Issue がある Take ÷ 判定された Take | `judge_result.issues` |
| Product Accuracy Failure Rate | 同様に `dimension` が `product_accuracy` / `product_geometry` / `logo_text_accuracy` / `object_consistency` の Issue がある Take ÷ 判定された Take | `judge_result.issues` |
| Prompt Adherence Rate | 最新の判定に `prompt_adherence` の medium / high の Issue が**ない** Take ÷ 判定された Take | `judge_result.issues` |
| AUTO Completion Rate | AUTO で着手し（現在も AUTO、または引き継ぎ済み）、引き継がれずに完成した Project ÷ AUTO で着手した Project | `project` / `human_intervention` |
| User Override Rate | 上書きが 1 回以上ある Shot ÷ 計測対象の Shot | `human_intervention.override` |
| Export / Completion Rate | 書き出し済みの Project ÷ 完成した動画の数 | `export` / `project` |
| **North Star** | 完成し、引き継がれず、人間の介入が 1 件もない Project ÷ 着手済み Project | `project` / `human_intervention` |

### 28.3 記録する人間の介入

既存のテーブルから読み取れない「人間が手で操作した」事実は `human_intervention`（`spec/architecture/er.md`）に 1 操作 1 行で追記する。記録は操作の成功後に行い、記録に失敗してもユーザーの操作は失敗させない。

| `kind` | 記録するタイミング | 上書き（`override`） |
|-------|------------------|---------------------|
| `generate` | ユーザーが Take のない Shot の生成を依頼した（`POST /shots/:id/generate` ほか、DIRECTOR の指示、Continue の確認で追加した Shot、Branch の最初の Shot） | しない |
| `regenerate` | ユーザーが Take のある Shot の再生成を依頼した | Judge を通過した（除外されていない）Take が既にあるとき |
| `repair` | ユーザーが Take の Repair を依頼した（`POST /takes/:id/repair`、DIRECTOR の部分再生成） | しない |
| `select_take` | ユーザーが Take を選んだ | AI Pick が別の Take のとき |
| `reject_take` | ユーザーが Take を除外した | 除外した Take が AI Pick のとき |
| `partial_take` | Partial Take Use で Take を合成した | しない |
| `edit_shot_spec` | Shot Spec を手で編集した | する |
| `lock_change` | パラメータを MANUAL / LOCK にした（AUTO に戻す操作は記録しない） | しない |
| `edit_timeline` | Edit Timeline を手で保存した | する |
| `request_changes` | 承認カード（チェックポイント）で「変更を依頼」した | する |
| `switch_to_director` | AUTO の Project を DIRECTOR MODE に切り替えた | しない（人間の介入にも数えない） |

AUTO のパイプラインが自分で行う生成・Judge・Repair・選択は記録しない（`repair_action` / `take` から読み取る）。`lock_change` は Shot / Project のパラメータの LOCK だけを数え、World State の属性の LOCK と World State・Shot Spec の過去版への復元（§29.4）は、この表に無いので記録しない（介入数の対象外）。

### 28.4 表示と外部送信

- 画面: `GET /projects/:id/analytics`（Project 単位）と `GET /analytics`（すべての Project の合計）。Project 一覧と Project のメニューからリンクする。
- 最上段に North Star（割合・分子/分母）、その下に Shot / 人間の介入 / 時間・コスト / 品質 / 完成の各指標を、名称・値・根拠（分子/分母や件数）の組で表示する。
- 集計はローカルの SQLite だけから行い、外部へ送信しない（`spec/requirements/non-functional.md` §5）。

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
- Model routing（選択理由、M3）
- References
- Output
- Judge result
- Repair history
- Note version（参照した制作メモ、M2）

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
