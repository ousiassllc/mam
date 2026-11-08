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

必須 7 項目（Content Type / Objective / Subject / Target / Duration / Aspect Ratio / Mood）の扱い（#1114）:

1. **発言から抜き出し、聞き直さない**: 最初の発言と以後の発言（ルールベース。LLM も外部通信も使わない）から、すでに述べられた主役・題材（Subject。人物が主人公なら、その人物）・目的と掲載先（Objective）・Content Type・Target・Duration・Aspect Ratio・雰囲気（Mood）と、テーマ・制約（成人・架空・着衣など）を Brief に取り込み、述べられた項目は聞き直さない。Subject は「X を主人公（主役）にした」「主人公は X」「題材は X」「X の縦型／15秒／CM／動画…」から読み取り、「A ではなく B」「A じゃなくて B」と言われたら B を題材とする（否定された A は題材にしない。「主人公は男性ではなく女性」は女性。「猫が主人公ではなく犬が主人公」は「猫が主人公」を丸ごと否定して犬。発言の中のただの「A ではなく B」を題材の変更と読むのは、同じ文にほかの項目の名前（ターゲット・雰囲気・長さ・比率など）が無く、否定された A が長さ・比率・動画の種類・掲載先・ターゲット・雰囲気の語でないときだけで、「ターゲットは学生ではなく社会人」「雰囲気は派手でなく地味」「長さは短めじゃなくて長め」は題材の変更ではなく、その項目も肯定された側だけから読む。さらに、そのただの「A ではなく B」が**決めた題材を置き換える明示した変更**になるのは、「A ではなく B」を述べている節（読点で区切った節）が「主人公」「主役」「題材」「テーマ」を述べているとき、その節が「の動画」「のCM」「の広告」を述べていて A か B が人物・動物・商品の名前（「猫ではなく犬の動画にして」）のとき、または A と B がどちらも人物・商品の名前（「男性ではなく女性」）のときだけで（人物・商品の名前は「女性」「インフルエンサー」「商品」「美容液」のような語で見分け、「個性」「様子」「作品」のように末尾の一字が同じだけの語は数えない）、「赤ではなく青の服にして」「朝じゃなくて夕方にして」「朝ではなく夜の動画にして」「冬ではなく夏の動画にして」は服の色・時間帯・季節・場所の話かもしれないので、題材は変えず、食い違い（下の 4）として聞く。題材の質問への答えは、答え全体が題材への答えなので、この制限なしに B（「主人公は猫ではなく犬」は犬）を取る）。題材は助詞（を・は・が）と読点で区切った名詞句で、掲載先・用途（「TikTok向けに」「広告用の」）や話題（「この動画は」）は含めない。ほかの項目の名前（「ターゲットは」「雰囲気は」「長さは」など）に続く節は、その名前の手前で題材を終え、その節の否定・変更の語を題材の変更に数えない（「主人公は猫でターゲットは学生ではなく社会人」の題材は猫、Target は社会人）。「今回」「次」「弊社」「この」のように依頼そのものや話し手を指す語、「なんでもいい」「お任せします」「未定」のように決めていない・任せるという答えは題材にしない（語の一覧に頼らず、形で退ける。「わかりません」「任せます」「決まっていません」のように述語だけで名詞が残らない文、「何がいいですか」「誰がいいですか」「おすすめありますか」のように聞き返す文（「か」「？」で終わる、または「誰・どんな・どういう・何・どれ・どちら」を含む）、助詞やつなぎの語を除くと文字が残らない文は、題材にしない。「AIにお任せします」「あなたにお任せします」「そちらで決めてください」のように、任せる相手（あなた・そちら・こちら・AI・アシスタントなど）しか残らない文（「お任せ」「決めて」「考え」「お願い」「ください」「思いつかない」のような、決定を委ねる・先送りする語があるときだけ。「AI」「AI動画」「AIの動画」「ChatGPT」「AIがいい」のようにその語を含むだけの文はその語を題材とする）と、「うーん」「えーと」「ちょっと考えさせて」「思いつかない」「浮かばない」のようなためらいも、題材にしない）。決めていない・先送り・お任せの語（「まだ決まっていない」「決めてない」「決められない」「未定」「分からない」「思いつかない」「後で決める」「お任せで」「特にありません」など。「後で」「あとで」は、先送りでないと言える形でなければ先送りとして数える（疑わしいときは先送り側に倒す）。続く語は問わず（「後でまた決めます」「後で見直します」「後でやります」「後で猫にします」「後で猫」「あとで猫の動画」「題材は後で相談」「主人公は後で検討」「主人公は後でお願い」「主人公は後で決めて」「主人公は後で決めるつもり」「主人公は後で決める予定」「題材は後でいいよ」）、節が動詞で終わることも求めない。先送りでないのは、名詞を修飾する動詞（語尾が う・く・ぐ・す・つ・ぬ・ぶ・む・る・た・だ・ない・たい）に名詞（漢字・カタカナ・英数字）が続き、助詞か文末で終わる形（「後で見返したくなる猫」「後で泣く猫」「後で歌う猫」「後で笑う猫の動画」「後で泣く猫の動画を作りたい」）だけである。続く名詞が「予定」「方針」「方向」「つもり」「こと」「形」「前提」「の」「もの」のような文を結ぶ語のとき（「後で決める予定」「後で決める方向で」）は、名詞を修飾していないので先送りである。漢字のあとの語の一部（「放課後で踊る猫」「食後でくつろぐ猫」）と「未定義の世界」の「未定」も先送りでない。先送りの判定は文ごとに行い、最初の文が題材を述べ、あとの文が**別の事柄を名指して**先送りにするだけのとき（「猫の動画を作りたい。後で見返したい」「猫の動画を作りたい。後で詳細を決める」「猫の動画を作りたい。BGMは後で決める」）は、あとの文の先送りで題材を捨てない。別の事柄は閉じた語の一覧（見返し・見直し・送る・確認・チェック・共有・詳細・細かい点・BGM・音楽・尺・長さ・秒数・ナレーション・字幕・テロップ・サムネ・公開・投稿日・予算・納期・スケジュール・日程・修正・編集・提出）で名指されたときだけで、先送りの節がどれもそれを含み、題材そのものを指す語（主人公・題材・テーマ・それ・何など）を含まないときに限る。名指さない先送りは題材そのものを先送りにしたのかもしれないので、迷ったときは安全側に倒して題材なしで聞き直す（題材そのものを先送りにする「猫の動画。題材は後で相談」「猫の動画。それは後で相談」に加え、「動画を作りたい。ネタは後で考える」「動画を作りたい。内容は後で」「動画を作りたい。後で決める」「動画を作りたい。後で猫にする」「ショート動画を作りたい。内容は後で決める」「猫の動画を作りたい。ネタは後で考える」も聞き直す）。最初の文が先送りのとき（「後で決める。猫の動画を作りたい」）も題材なしである）が題材の候補の**どこかに**入っている文は、語のあとが別の項目へ続いていても題材にしない（「主人公はまだ決まっていないけど、TikTokで出したい」「主人公は未定だが縦型動画」「主人公はお任せでTikTok向けの動画を作りたい」は題材なし。TikTok などほかの項目はそのまま読む）。変更や決定を告げるだけで名詞が残らない節（「主人公は変えたい」「主人公は変更したい」「主人公は後で変更します」）も題材にしない（決めた題材は置き換わらず、食い違いにも数えない）。「…を主人公にしてほしくない」「やめて」のような拒否の節からは何も読み取らない（「にはしない」「にしません」も拒否で、「猫の動画は作らない」「猫の動画は出さない」のように主人公・主役・題材の語がない節の「作らない」「作りたくない」「出さない」「出しません」も拒否。「架空の女性を主人公にして未成年は出さない」のように主人公の語がある節は、制約を述べているだけで拒否にしない。「作らない」「出さない」が節の述語で終わるときだけ拒否で、「猫の動画を作りたいが暴力は出さない」「ロボットの動画を作りたくて酒は出しません」のように述語のあとの「が」「けど」「ので」「て」で続く節は、その前の依頼の部分（猫・ロボット）を題材として読み、拒否の部分だけを読まない。「成人しか出さない猫の動画」のように「出さない」が名詞句を修飾するときは拒否ではないが、「作らない方向で」「出さない予定です」「出さない方針」「出さない事にした」「作らない人」「出さないつもり」のように「方向」「予定」「方針」「事」「こと」「つもり」「形」「人」などの名詞が続き、「の」や名詞でさらに続かないとき（文の結びになる）は拒否で、文全体が題材にならない（疑わしいときは拒否側に倒す。「出さない人の話」「出さない形の動画」のように「の」で名詞に続くときだけ拒否ではない）。ただし、そのあとの「で」に依頼（作る・描く・撮る・お願い）が同じ読点なしの節で続くとき（「暴力は出さない方向で猫の動画を作りたい」「猫の動画を暴力は出さない形で作りたい」「お酒は出さない前提で猫の動画を作りたい」）は、「で」で分けて、拒否の部分だけを読まず、依頼の題材（猫）を残す。「宣伝を出さない猫の動画を作りたい」の題材は、「出さない」が修飾する語のあとの「猫」で、「出さない猫」ではない（題材を読むどの経路でも同じで、「宣伝を出さない猫を主人公にした動画」も「猫」である。「出さない」「作らない」「出しません」「作りません」が修飾する語を外すのは、その前に自分の目的語か主題（「宣伝を」「広告は」）があり、それが安全に関わる語でないときだけで、「作りたくない猫」「作らない人物」「作らないキャラ」「作らない猫以外」「出さない猫」「題材は作りたくない猫」「題材は出さない猫」のように自分の目的語が無い拒否は、あとに続く名詞の拒否なので題材にならず聞き直す。「作りたくない」は目的語があっても同じ）。目的語か主題が安全に関わる語（未成年・子供・子ども・こども・児童・少女・少年・裸・ヌード・露出・実在・成人・性的・暴力・流血・グロ・過激・酒・飲酒・タバコ・喫煙）のときは、述べた制約を題材から切り捨てて跡形もなく消さないため、その節を外さず、題材を読まずに聞き直し（「未成年は出さない猫の動画を作りたい」「暴力を出さない猫を主人公にした動画」「お酒を出さない人の話」。きれいな名詞句でない拒否を題材として保存しない）、述べた拒否は Additional Instruction に「制約: 未成年は出さない」の行として残す（確認カードに見えて消せる）。依頼と拒否が「が」「けど」「ので」「て」などでつながる文（「猫の動画を作りたいが暴力は出さない」「ロボットの動画を作りたくて酒は出しません」）と、拒否が文を結ぶ名詞（方向・予定・方針・事・形など）で終わる文（「暴力は出さない方向で猫の動画を作りたい」）は、依頼の題材を残したまま、切り捨てた拒否を同じ「制約: 暴力は出さない」の行に残す。主人公の節に続く、名詞で文を結ぶ拒否（「猫を主人公にして暴力は出さない方向で」「猫が主人公で暴力は出さない予定です」「女性を主人公にして暴力は出さない人物」「主人公は猫で暴力は出さない方向で」）は、後ろの名詞を修飾する節ではなく題材（猫・女性）を読む（「猫を方向で」のように拒否の名詞をつないだ題材を作らない）。依頼そのものだけが残る文も題材にならない。「動画」「CM」「投稿」「ショート」「映像」「ビデオ」「作品」と依頼の述語（作りたい・作って・作る・撮りたい・撮って・お願い・ください・したい・欲しい・ほしい・作成）しか残らないとき（「暴力は出さない方向で動画を作りたい」「ショート動画を作ってください」「動画を作りたい」）は、題材なしで聞き直し（「猫の動画を作りたい」は猫）、「動画を作りたい」だけの答えも聞き直す。依頼が「けど」「けれど」「けれども」「んですけど」「のですけど」「んです」「のです」「と思います」「と思っています」「なと思って（います）」「なと思う」「て」で終わる（「です」「ですが」で終わる「動画を作りたいです」も依頼だけで、題材なし）「動画を作りたいけど」「動画を作りたくて」「動画を作りたいんですけど」「動画を作りたいなと思っています」「動画を作りたいと思います」も依頼だけなので同じで、「猫の動画を作りたいけど」は猫）。テーマと制約は Additional Instruction に「テーマ: …」「制約: 成人・架空・着衣」の行として残し（制約の語は `成人`・`架空`・`着衣` に正規化する。安全に関わる制約なので、語そのものの述語が否定する「成人ではない」「服は着ていない」「着衣なし」、否定の語の一部になった「ノンフィクション」、別の語の一部の「成人式」は記録しない。否定として数えるのは、その語の直後（または助詞と短い名詞の並びのあと。「成人の女性ではない」「成人女性ではなく少女にして」「架空の人物ではなく実在の人物」）に「ではない」「は出さない」「はいない」「にはしない」「着ない」「しない」などの否定が続くときだけで（「成人は出さない」「成人にはしない」「着衣しない」は記録しない。「成人だけではない」のように限定そのものを否定する言い方も記録しない）、同じ文の別の語への否定（「架空の成人女性が主人公で商品は紹介しない」「…で他に誰もいない」）では記録を落とさない。語のあとに「しか」「以外」「だけ」「のみ」が続く限定（「成人しか出ない」「成人以外は出さない」）と、語を否定した名詞句を出さないとする言い方（「成人でない人物は出さない」「成人ではない人は登場させない」「成人ではない子どもは一切出さない」。語が動詞なら、その否定が名詞句を修飾する「服を着ていない人物は出さない」「服を着ない人は登場させない」「着衣しない人物は出さない」も同じで、「一切」「絶対に」「画面に」「もちろん」「断じて」のような短い語（助詞の「が」「は」「を」を含まない5文字まで）や読点が出さない動詞の前にあってもよい（句点は挟めない。「成人ではない。人物は出さない」は「成人ではない」で語を否定して次の文に移っているので記録しない）。出さない動詞のあとに続く語は問わず（「出さないよ」「出さないね」「出さないつもり」「出さない予定」「出さない方針です」「出さないと思う」「出さないと決めた」「出さないこと」「出さない！」「出さない？」「出さないが」「出さないけど」「出さないし」も記録する。安全に関わる制約を落とさないため、疑わしいときは記録する側に倒す）、「出さないわけではない」「出さないとは言っていない」「出さないことはない」「出さないとは限らない」「出さないのではない」「出さないなんて言っていない」「出さないこともある」「出さないかもしれない」のように取り消す語（わけ・訳・とは・ことは・ことも・のでは・ってわけ・というわけ・とも限・とは限・なんて・かもしれない・かも）が続くときだけ記録しない。名詞句を出さないと言わない「服を着ていない人物は出す」「服を着ていない人物が主人公」は、語の否定で記録しない）は、その語を満たす人物だけを出すという制約を述べているので、記録し、取り下げにも数えない（落とした制約は見えず、余計に残した制約は確認カードに見えて消せるため、迷うときは残す）。後の発言のテーマは、発言から読み取っただけの前のテーマの行を置き換え、利用者が書いた行は残す。後の発言が制約の語を否定して取り下げたとき（「やっぱり成人じゃない」）は、発言から読み取っただけの制約の行からその語を外す。利用者が書いた行・確定した行には触れない）、Producer / Writer / Director へ固定の条件として渡す。なお、自由文から読む題材はヒューリスティックで、語の一覧が閉じているものは抜けが残る。決まっていない・先送りの語の一覧は終わりがなく（「見込み」「模様」「考え」などの名詞化の語の一覧、「撮影後で決める」、「後から」「後に」「後日」「のちほど」「追って」「改めて」「別途」「次回」「今度」「そのうち」「決まり次第」は先送りの語として数えない）、一覧にない曖昧な言い方は題材として取り込まれうる。その取りこぼしを受け止めるのは KEY / CUSTOM CHECK の確認カード（利用者が見て直せる）で、FULL AUTO では確認しないため、一覧にない曖昧な言い方がそのまま題材に採用されうる（既知の限界）。「作りたくない」を含む題材の名詞句を誤って聞き直すことがあり、「服を人物は出さない猫」のように拒否の節が入れ子になった文は題材を取り違えうる。依頼のない安全拒否だけの発言（「未成年は出さない」）は聞き直しになるだけで「制約:」の行に残らず、「わけではない」などの取り消しが付いた安全拒否も「制約:」の行に残る（行は確認カードに見えて消せる）。**決まっている Subject は、既定で保持する**: 答えの言い回しがどうであっても（依頼の言い方の一覧にない「動画を作りたいですけど」「動画をつくりたいです」も）、利用者が示した根拠がない限り上書きしない。根拠は、新しい Subject を名指しした変更（「…に変更して」「猫ではなく犬」）と、食い違いの確認カードで2つの値のどちらかを名指しすること（ボタン、または値だけに助詞・「がいいです」「にします」などの終わりが付いた短い文）の2つだけで、`backend/internal/agent/briefing/overwrite` の表がカードへの返信の規則を一か所に定めている（自由な発言も、変更と判定する条件に同じ形の根拠を要する）。根拠には**形**がある: 変更を言う文が**最後の文**で、Subject の前は役割のラベル（「主人公は」「題材を」）か否定された値（「猫ではなく」）だけ、後ろは助詞・役割のラベル・変更の動詞の言い切り（「に変更して」「にします」「にしたい」）・丁寧な終わりだけ。その後ろに別の文が続くときは根拠にならない（後の文が「やっぱりやめます」「いや待って」「後で相談」と取り消すかもしれないため。前の文が別の項目のこと、「30秒にして。主人公を猫に変更して」、であるのは構わない）。疑問（「？」）も根拠にならない。「犬に変更するかも」「犬にしたいけど迷う」「犬に変更したい、後で」「もし犬にするなら」のように形に当てはまらない言い方は、一覧にない言い方でも根拠にならず、Subject は保たれる（形を知らない言い方は、もう1度聞かれるだけで、Subject は替わらない）。値そのものにも**名詞の形**を求める（語の一覧ではなく文字の並びで判定する）: 漢字・カタカナ・英数字の語を「の」でつないだもの（「架空の30代女性インフルエンサー」「AIキャラクター」）、語の前の「かわいい」「小さな」のような修飾（ひらがなで「い」「な」で終わるもの）、ひらがなだけの2文字以上の語（「ねこ」「うさぎ」）だけを名詞とし、助詞（を・に・で・が・は・と・や・も・へ）、動詞の語尾、ひらがなで終わる語、読点・記号、列挙（「犬or猫」「犬・猫」「犬とロボット」「犬や猫」「犬か猫」）、指示・プレースホルダ（「これ」「前者」「両方」「適当」「TBD」）を含む値は名詞でない。値の前後の形は、値が**最初に現れる文**で判定し、その後ろに別の文が続くときは根拠にならない（前の文が迷い・取り消し・他人の発言で、最後の文が値だけを繰り返す「主人公を犬に変更しようか迷う。犬」も根拠にならない）。値に「多分」「明日」「一応」「暫定」のような前置きの語が付いたもの（「多分犬」）や、保留・確認中・調整中・相談・検討・次第・後日・上記・以下・別の・他の・前回・候補・番目・TBC などの先送り・指示の語を含む値、数字だけの値は名詞として扱わない。「A ではなく B」の B も同じ形の値だけで、「猫ではなく犬にして、30秒にして」「猫ではなく犬を検討中」のように後ろに動詞や節が続く形は根拠にならず、否定される A も名詞で、「多分」「たぶん」「もし」「できれば」「とりあえず」などの前置きで始まらず、助詞（「友達は猫ではなく」）や接続を含まないものだけを受け付ける。名詞の形をした値（「犬」）に変更の根拠があれば、そのまま替える。名詞の形ではないが断片でもない値（「空飛ぶ猫」「お母さん」「ぞう」「猫と犬」のような、修飾の動詞を含む語やひらがなの語）は、すぐには替えず、別の Subject の候補としてその1点だけ聞く（利用者がボタンで選べば替わる）。断片（動詞・否定・推量・列挙・指示語・節を含むもの。記号を含む名前（「ドナルド・ダック」「AT&T」「C++」）も断片ではなく、漢字・ひらがなを「・」「/」「&」で結んだ列挙（「犬・猫」「犬/猫」）だけを断片とする。語の途中に断片の語の一部が含まれるだけの名詞（「泣いている赤ちゃん」「たらこ」「Twitter」「ミケ（猫）」「とかげ」）は断片ではなく、替えるか候補として聞く。「迷う」「そのまま」「犬かな」「犬w」「犬のはず」「犬か猫」「犬にして、30秒」）は候補にもしない。確認カードのボタンは、文字列が一致するほうをそのまま選び（「猫の動画」と「猫」が並んでも）、形の判定はボタン以外の答えにだけ使う。変更の動詞は「変更して」「変えて」「差し替えて」「直して」「にして」「にします」などの言い切りを受け付け、「猫から犬に」「やっぱり犬に」「猫でなく犬に」も受け付ける（自由な発言の「変えます」「切り替えて」は変更と読まれず、候補として聞かれる）。変更と言わずに別の Subject を言い切りの形で名指しした場合は、Subject を保ったまま食い違いとしてその1点だけを聞く。食い違いの確認中に、どちらも名指ししない答えが来たときは、何も決めずに「どちらを主役にするか読み取れませんでした」と添えて同じカードで聞き直す。既知の限界: 食い違いの確認カードが新しい値に移った後は、古いカードのボタンは受け付けない。前置き・先送り・指示の語は閉じた一覧で、一覧にない語が値に融合した形（一覧外の副詞や指示語、「例の動物」のような言い方）は名詞の形として通りうる。名詞の形を満たさない正当な値（漢字で書いた語に「ー」以外の記号が付くもの、ひらがなの1文字の語など）は、変更・候補にならず、同じ Subject のまま残る（食い違いのカードのボタンでは選べる）。名詞化の語（方向・予定・方針など）と先送りの語の一覧は終わりがなく、一覧にない言い方は拒否や先送りとして扱われない。確認カード（KEY / CUSTOM CHECK）がこれを受け止め、FULL AUTO では一覧にない言い方が採用されうる。
2. **出どころを区別する**: Brief の値は項目ごとに出どころを持つ。`user`（利用者が答えた・編集した・確定した値）、`inferred_from_request`（発言から読み取った、まだ確認していない値）、`recommended`（目的から決めた仮の値）。利用者が明示した値（`user`、発言に書かれた値）は、推奨値より常に優先する。Brief の画面（編集フォーム）は、`recommended` の値に「推奨（仮）」、`inferred_from_request` に「依頼文から（未確認）」を項目名の横に出し、MCP の `get_brief` は項目ごとの出どころ（`sources`）を返す。保存（編集フォーム）と確定（チャットの「この内容でよいですか？」への承認、MCP の `update_brief` の `confirm`）で、表示された値は `user` になる。MCP の `update_brief` は、渡した項目だけを `user` にする（値が推奨・読み取りの値と同じでも、渡した項目は `user`）。Subject の食い違い（`proposed_subject`）は、Subject を渡したとき・保存や確定のときに解消し、ほかの項目だけの更新では残る。
3. **決まっていない項目は推奨値を仮に置く**: 目的（Objective）が下表の掲載先と読み取れるとき、Aspect Ratio と Duration を**仮の提案**として入れる（`recommended`）。発言の「YouTube Shorts」「InstagramのReels」「YouTubeのショート動画」「Instagramリール」のように、YouTube に Shorts・ショート、Instagram に Reels・リールが（「の」「で」「に」「向け」「用」をはさんでも）続くときは、YouTube・Instagram だけでなく、その形式（`YouTube Shorts`・`Instagram Reels`）を Objective として読み、下表の行を当てる（TikTok も同じ。「Instagram広告」「YouTube広告」は表に無いまま）。掲載先が長さを一意に決めるとは言わない。Duration は、その掲載先がふだん使う範囲の中の 1 つの値を、秒数で示す。表に無い掲載先（Instagram 広告・YouTube 広告・ブランドサイトなど。リール・ストーリーズ・フィードで形式が違うものを含む）と、掲載先が分からないときは、提案を作らず、二つとも未設定のままにする。発言に Aspect Ratio や Duration があれば（`inferred_from_request`）、その値が推奨値に代わる。掲載先が変わって表に無くなった推奨値は取り下げる。

   | 掲載先（Objective の読み取り） | Aspect Ratio | Duration の提案 | その掲載先がふだん使う範囲 |
   |---|---|---|---|
   | TikTok | 9:16 | 15秒 | 15〜60秒 |
   | YouTube Shorts（YouTube + ショート、Shorts） | 9:16 | 30秒 | 15〜60秒 |
   | Instagram Reels（Instagram + リール、Reels） | 9:16 | 15秒 | 15〜30秒 |

4. **上書きしない**: 後の発言が、決めた値を上書きするのは、**明示した変更**のときだけである。明示した変更とは、発言から具体的な新しい値が読み取れ、かつ**その値を述べている節（句点・読点で区切った範囲）**が変更と分かる言い方（「変更」「変えて」「にして」「にしたい」「に直して」「やっぱり」「ではなく」など）をしているときをいう（決定的な規則。別の項目の節にある変更の語は数えない。「30秒にして。主人公はロボット」の Subject は変更ではなく食い違いで、Duration だけが変わる。拒否の語（上記 1。「ほしくない」「いらない」「やめて」「にはしない」など）を含む節は何も述べない（「不要品回収」のように語の一部の「不要」は拒否ではない）。拒否は答え全体ではなく文・節ごとに読み、「犬を主人公に。猫はいらない」は犬を述べる。「A ではなく B」を Subject の変更と読む条件は上記 1 のとおり）。否定・拒否・一般的な言い方だけの発言（「ただの投稿動画」「商品ではなく」「特にない」）は、題材として何も読み取れないので、Subject を一切変えず、Subject への答えとしても受け付けない（具体的に聞き直す。Subject の質問が開いていれば、それに答えるまで）。Subject 以外の項目は、変更と分かる言い方がなければ、決めた値を変えない。発言から読み取っただけの値（Subject を除く）は、新しい発言の値で置き換わる。**Subject が食い違う**（決めた Subject と違う具体的な題材が、変更と分かる言い方なしに述べられた）ときは、Subject を変えず、その点だけを質問カード（二つの値のボタン）で聞く。どちらかを選ぶか、二つの値の一方を返信するまで、その質問は開いたまま、確認レベルに関わらず PLANNING は始まらない。
5. **質問は最小にし、確認は 1 回にまとめる**: 一つずつ質問を続けることはしない。聞くのは、**Subject を決められないとき**（専用の質問カード。「AIに任せる」は出さない）と、Subject の食い違いだけである。ほかの項目は、読み取れた値・推奨値・未設定の項目を**一覧にした 1 枚の確認カード「この内容でよいですか？」**（`ref_type=brief_confirm`。ボタン「この内容で進める」、各値に出どころの印）に出す。承認（ボタン、または「はい」「OK」など。§23.1 の承認の語）すると、値は `user` になり、未設定の項目は「AIに任せる」になって PLANNING が始まる。確認カードへの返信が承認でなければ、本文から読み取れる値（変更は上記 4 の規則に従う）で Brief を直し、確認カードを出し直す（返信したカードは回答済みになるので、開いている確認カードが常に 1 枚残る。確認カードを新しく出したときは、それより前の確認カードも閉じる（古い Brief の内容を承認できてしまわないように。最新の 1 枚だけが開いている）。何も読み取れない返信でも Brief は変えず、確認カードだけを出し直す）。カードに返信せず Brief の画面で保存しても、表示された値は確定する（未設定の項目が残るときは、確認カードが開いたまま残る）。
6. **確認レベルとの関係**: この確認は**生成の承認（`generation`）でも脚本の承認でもない**。承認カード（`domain.Checkpoint`）ではなく Question カードで、Project を AWAITING_APPROVAL にしない。**FULL AUTO** では確認を求めず、推奨値を採用し、未設定の項目を「AIに任せる」にして PLANNING を始める（止まるのは、Subject を決められないときと、Subject の食い違いだけ）。確認レベルを FULL AUTO に切り替えると、確認カードの返事を待っている DRAFT の Project も始まる。**KEY CHECK・CUSTOM CHECK** では、確認カードに答えるまで PLANNING を始めない（利用者が全項目を自分の値にしてあれば、カードは出ず、そのまま始まる）。
7. **脚本を Brief の主役に合わせる**: Producer / Writer / Director / Storyboard のプロンプトは、Subject（主人公・題材）と Additional Instruction の制約を**固定の条件**として渡し（「別の人物・商品・題材に置き換えない」「性別や年齢などの設定を変えない」）、推奨値は仮の値だと伝える。Producer は `protagonist`（主人公・題材）を答えに含める。**ガード**: Producer の `protagonist` が Brief と**明らかに食い違う**とき（Subject か追加指示が一方の性別を述べているのに、もう一方の性別だけを述べている。追加指示に「成人」とあるのに未成年を述べている。「女性向け」「男性層」のような対象の語、「大人っぽい」「大人向け」「成人式」、「成人ではない」は主人公の性別・成人の指定として数えない）は、その案を保存せず `planning.ErrInvalidPlan`（LLM の出力が使えない）として制作を止め、チャットに知らせる。検査は構造化された `protagonist` 1 項目の語の照合に限る。主人公が空のとき、語が当たらないときの食い違い、脚本の中身が主人公に沿うかどうかは検査できず、プロンプトの固定条件に頼る（既知の限界）。

従来の #1108 の規則は残る: **Subject は「AIに任せる」を選べない**（題材をAIが決めると、ユーザーの指定と無関係な脚本になるため、Subject の質問カードには「AIに任せる」ボタンを出さず、その答えは受け付けずに同じ質問を理由つきで聞き直す）。Subject が空でも、利用者が書いた Additional Instruction に題材があればそれを題材として扱う（どちらも空なら PLANNING で脚本を作らず、Subject の質問カードを 1 度だけ出して止まる。答えると続きから再開する。発言から読み取ったテーマ・制約の行（`テーマ: …`・`制約: …` だけの行）は題材に数えない。Brief の画面で保存しても、`update_brief` で確定しても、その行が利用者のものになるだけで題材にはならず、利用者が自分で書いた行（これらの接頭辞でない行）だけが題材になる。Producer / Writer / Director のプロンプトも、Subject 未指定を「AIに任せる」とは書かず「Brief に無い題材を作らない」と伝える）。チャットでの回答も編集フォームと同じ値域（Aspect Ratio は 9:16 / 16:9 / 1:1、各項目 200 文字以内）で、範囲外の回答は保存せず同じ質問を聞き直す。発言から抜き出す URL も編集フォームと同じ規則（200 文字以内・空白を含まない）で、外れた URL は References に入れない。Assets / References / Additional Instruction は任意（§23.1）。

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

> **実装の範囲**: Orchestrator が自動で進めるのは Brief → Producer → Writer → Director → Storyboard（Scene / Shot Planning、Project は PLANNING → VISUAL_DESIGN）と、その直後の**生成の自動開始**、Art Director 以降（Reference Pack・Keyframe・Image / Video Generation）の本線である。AUTO MODE では、Scene / Shot 計画のあとに、**承認カードを出さずに** Reference Pack → Shot 生成を本線で自動進行する（AUTO を選ぶことが生成を始める同意。#1115 が #1108 の「生成の承認」を置き換えた。#1111）。起動した Shot では AI Video Judge → Repair / Retake → AI Pick が AUTO MODE で連鎖する。AUTO MODE では、トランクの全 Shot にファイル付き Selected Take が揃うと Rough Cut の作成と Editor Judge が自動で始まる。確認ポイントを通過したあと（承認カードの回答後、または確認不要の設定で AUDIO に入ったあと）は Audio と Final QC まで自動で進む。承認カードが未応答のとき、Editor Judge が Re-edit のとき、Final QC が FAIL のときはそこで止まる（Editor Judge が `need_shot` のときは、AUTO MODE では追加 Shot の要求が自動で始まり、Retry Policy の範囲で繰り返し、上限に達したら Escalation で止まる。MANUAL ではユーザーが要求する）。Export は READY の動画に対するユーザーの操作である。AUTO では Shot を順に自動で始めるが、起動のたびに予算上限が効き、**1 回の操作で Project 全体を一括で生成する段は持たない**（Shot と段階（Reference Pack・生成）ごとに起動し、重複しない）。生成テスト（`/generation-test`）は試用の経路で、その結果はプロジェクトの Shot の Take にならず、画面にもそう書く。
>
> **AUTO の生成開始（`generation`。#1115 で #1108 の「生成の承認」を置き換えた）**: **AUTO MODE を選ぶことが、有料の生成を始める同意である**。Scene / Shot 計画が終わった直後から、Orchestrator が Reference Pack → Keyframe → Shot 生成を**承認カードを出さずに**自動で始めて進め続ける。確認レベルは生成の開始に影響しない（FULL AUTO の「Finalまで原則確認しない」と矛盾しない。KEY CHECK・CUSTOM CHECK でも AUTO の意図を優先して、生成の開始は確認しない）。脚本・Rough Cut・Final の確認は各レベルの定義どおりで、この変更で増えも減りもしない。確認ポイント `generation`（生成の承認）は廃止した（`domain.Checkpoint` に旧カードを読むための定義だけを残し、`RequiresApproval` は常に偽）。
>
> | MODE | 確認レベル | 生成の承認カード | 生成の開始 |
> |------|-----------|----------------|-----------|
> | AUTO | FULL AUTO | 出さない | Scene / Shot 計画の直後に Reference Pack → Keyframe → Shot 生成を自動で始めて進め続ける |
> | AUTO | KEY CHECK・CUSTOM CHECK | 出さない（AUTO の意図を優先） | 同上。脚本・Rough Cut・Final の確認は各レベルの定義どおり（脚本の確認が要るレベルでは、脚本の承認のあとに計画へ進むので、その承認のあとで始まる） |
> | DIRECTOR・MANUAL | （どれでも） | 出さない | 自動では始めない。Shot 単位でユーザーが起動する従来どおりで、迂回の手段を足さない（手動の Shot カードの「生成」「Reference Pack」、Continue・Branch・DIRECTOR の再生成・Reshoot はそのまま） |
>
> - **始めるときに一度だけ知らせる（非ブロッキング）**: Orchestrator は、まだ起動していない Shot を初めて起動するときに、チャットへ**お知らせを 1 件**出す（待たない。承認を求めない）。中身は、同意の事実（AUTO のため承認を待たずに始める。費用がかかる）、対象（Shot 数、Reference Pack を作る Shot（Keyframe の無い Shot）、使うモデル（動画モデルと解像度、Reference Pack の画像モデル）、Shot ごとのおおよその本数）、費用の見積、予算上限（Project・Shot。未設定なら「設定なし」）、予算上限が数えるものと止め方（予算上限が数えるのは単価が分かるモデルの呼び出しだけで、単価が不明の呼び出しは数えず止めない。確実に止めるのは MODE を DIRECTOR にすること。起動済みの生成は終わるまで続く。起動済みの生成を取り消す操作は無い）。見積は Reference Pack（Shot ごとに最大 6 枚。Art Director が実際に何枚作るかは実行時に決まるので上限）と動画（Shot ごとの本数 × 1 本の見積。Quality Policy と固定した動画モデルで決める。Multi-model routing が実行時にモデルを入れ替える分は含めない）の合計で、Judge・Prompt Planner・Art Director など LLM の費用と Repair の再生成は含まない。お知らせは「起動の単位」ごとに 1 件で、同じ Shot について何回見直しても、プロセスを再起動しても、Reference Pack のあとの生成の起動でも、重ねて出さない（`generation_launch` に記録のある Shot は、すでにお知らせ済みとして扱う）。あとから Shot を足したときは、その Shot を起動したときに新しいお知らせを 1 件出す。**お知らせを出す順序（正直な制限）**: お知らせは、その見直しの起動を行ったあとに、起動できた Shot の分だけ出す（起動が 1 つも成立しなかった見直しは出さない）。起動の記録（`generation_launch`）は起動の前に書くので、起動の記録を書いてからお知らせを投稿するまでのあいだにプロセスが終わると、起動だけが済んでお知らせが出ないまま残る（その Shot は記録があるため、あとからお知らせし直さない）。お知らせを起動の前に出すことは保証しない。
> - **単価が不明でも止めない**: 単価が分からないモデル（`models.json` に価格が無いクラウドモデル、ChatGPT の定額枠のモデル）が含まれても、**自動の開始は止まらない**。その分を**「不明」**と正直に書き（でっち上げの数字は出さない。確認できる分だけを併記する）、「単価が不明でも止めずに始める」と明記する。単価を設定していないローカルモデルは 0 USD（無料）として数える。**予算上限は、単価が分かるモデルの呼び出しだけを数える**。単価が不明の呼び出しは数えないので、予算上限を決めていても止まらない（まさにこの「不明でも始める」ケースである）。したがって、単価が不明なモデルが計画に含まれるときは、予算上限を決めているかどうかに関わらず、「この分は予算上限に数えられず、費用の上限は保証できない」というリスクをお知らせに書く（容認したリスクである）。確実に止める手段は MODE を DIRECTOR にすることで、お知らせ・同意の通知・待機の案内・MCP のツール説明は、予算上限を「止める手段」として単独では勧めず、この範囲と DIRECTOR を併せて書く。進行中の Job を取り消す操作は無い。
> - **守るもの（変えない）**: ①**予算上限は承認の代わりではなく、AUTO でも効く**: Reference Pack・Shot ごとの起動で `ErrBudgetExceeded` になったら、その起動は始めず、Escalation（Retry Policy の上限と同じ扱い）で人間に判断を求め、答えるまでほかの Shot も新しくは始めない。②**キャンセル**: ジョブのキャンセル（プロセスの停止）で起動の途中だった Shot の記録は返し、残りは起動しない。③**重複しない（冪等）**: 起動した Shot と段階（`reference_pack` / `generate`）を `generation_launch` に記録し、何回見直しても、再起動しても、同じ Shot の同じ段階を再び起動しない。すでに Take がある Shot、Job が走っている Shot（`ErrShotBusy`）は起動しない。Reference Pack の Job が失敗した Shot は自動では生成しない（失敗はチャットに通知済みで、手動で起動できる）。④**使えないモデルの検出**: 起動が使えないモデルで拒否されたときは、ほかの失敗と同じく、理由をチャットに知らせ、その Shot は自動では進めない（起動前に使えるモデルの経路が無いことを分かりやすく返すのは、モデル候補の Preflight の仕事）。
> - **起動を断られたとき**: 予算上限（`ErrBudgetExceeded`）は上の①のとおり Escalation で止める。使えるモデルが無い（`modelroute.ErrNoUsableModel`。#1113 の起動前の検査）のはモデルの状態であって Shot の問題ではないため、**そのラウンドは断りで止めず、別の用途・メディアの Shot を続けて試す**（画像モデルが無くても、Keyframe を持つ Shot の動画は起動できる）: 原因（用途とメディアで決まる文面）ごとに、最初に断られた Shot だけを「起動に失敗」として記録し（自動では再試行しない。手動で起動する）、同じ原因で断られた別の Shot は記録せず、起動の予約を返して残す。チャットには原因ごとに原因と対処を**1 件だけ**書き、同じ原因のお知らせは、断りに当たらずに起動が 1 つ以上できた見直しか、別の原因になるまで繰り返さない（その間の見直しは、残した Shot を試して断られても記録せず何も書かない）。API キーの登録や割り当ての変更を保存しても、Orchestrator はその場では Project を見ない（保存に見直しの合図は付けない。制限）。続けるには、API キーを登録するか割り当てを直したあと、チャットに何かメッセージを送る（`POST /projects/:id/messages` は Orchestrator に見直しの合図を送る）。次の見直しで、残した Shot から自動で続く。それ以外のエラーは、従来どおり Shot ごとに失敗として記録・通知して次の Shot に進む。お知らせ（始めるときの 1 件）は、起動が 1 つ以上できたときだけ出す（Shot が使用中などで 1 つも始まらなかった見直しは出さない）。
> - **DIRECTOR / MANUAL（承認は元からない）**: DIRECTOR・MANUAL の Project には、生成の承認は元からなく、新しく加えることもない。生成の開始は自動で行わず、Shot をユーザーが手で起動する（ゲートは何も求めず、何もしない。承認の迂回を足したものではない）。#1115 の「manual による承認」は、この「手で起動し、ゲートは何も尋ねない」の意味である。旧カードに「承認」と答えても、DIRECTOR・MANUAL の Project は自動では始まらず、チャットの返答もそう書く。
> - **進め方**: Orchestrator が、Reference Pack が要る Shot の Reference Pack を起動し（`StartReferencePack`）、その Job が成功した Shot から Quality Policy に従う生成（`StartByPolicy`）を起動する。Reference Pack が要らない Shot はすぐ生成する。順序と同時実行数は Shot 単位の仕組みとジョブキューに従い、起動した生成は Job と Progress で追える。
> - **同意の見え方**: プロジェクトの MODE の操作（AUTO を選んでいる間）と確認レベルの操作には、「AUTO を選ぶと承認を待たずに生成を始める。費用がかかる。単価が不明なことがあるが、不明でも止めずに始める。予算上限が数えるのは単価が分かるモデルの呼び出しだけで、不明な分は上限を決めていても止まらない。確実に止めるのは MODE を DIRECTOR にすること」「確認レベルは生成の開始に影響しない」を表示する。`set_project_mode`・`set_confirmation_level`・`respond_approval` は、画面の操作と同じサービスの呼び出しで、チャットの通知と MCP のツール説明も同じ内容を言う（画面と MCP で食い違わない。文面は `mainline.AutoStartNotice` を共有する）。**既知の制限**: `create_project` が作る新しい Project の AUTO の既定は、作る操作の時点では同意の文面を示さない（始めるときのお知らせで初めて示す）。
> - **旧方針（v0.1.34）のカード**: v0.1.34 の Project は「生成の承認」カード（`ref_type = generation`）を持つことがある。**未回答（pending）**のカードは、AUTO の Project では Orchestrator が次に Project を見たとき（ユーザーの操作や Job の完了のあと）に**システムが閉じる**: 「旧方針のカードで、AUTO では承認を求めなくなったのでシステムが閉じた（あなたの回答ではありません）」とチャットに書き、カードを閉じ、Project を AWAITING_APPROVAL から VISUAL_DESIGN へ戻して、そのまま自動で始める（ユーザーの返信や承認の記録は作らない）。起動時の一括処理は持たない（ユーザーが居ないところで課金を始めないため）。**ユーザーが差し戻した（`changes_requested`）カード**は「まだ生成しない」の意味を保ち、自動では始めない。チャットの案内と画面は「自動では始めない」と次の行動を示し、ユーザーがカードへの返信ではない発言をチャットに送るか、Shot の追加・削除・モデルや本数が変わる計画の変更をすると、自動の開始に進む（新しいカードは出さない）。DIRECTOR の Project の未回答カードは、ユーザーの回答に任せる。旧カードに `respond_approval`・ボタン・文章の返信で答えることは今までどおりできる（エラーにしない。承認は Project を VISUAL_DESIGN に戻し、差戻しはカードを差戻しのまま残して自動では始めない状態にする）。カードは `generation_approval` の記録とともに読み取り専用で残り、新しく書かれない。**取り込んだ Project の MODE（#1115・同意は取り込まない）**: AUTO を選ぶことは、選んだ環境での有料の生成への同意なので、バンドル（他人から受け取ったものを含む）の `mode = auto` をこの環境の同意として引き継がない。インポートは、AUTO の Project を **DIRECTOR** として作り（ほかの列はバンドルのまま）、その Project のチャットに「別の環境から読み込んだので、AUTO を引き継がず DIRECTOR にした。この環境で AUTO で進めるなら MODE で選び直す（それがこの環境での同意になる）」と書く（DIRECTOR のバンドルには何も書かない）。したがって取り込んだ Project では、旧カード（承認済み・差戻し・未回答、記録の有無）に関わらず、ユーザーが MODE を AUTO にするまで Orchestrator は何も始めない。MODE を AUTO にしたあとは、上の旧カードの規則のとおりに進む。自分のバックアップを取り込み直した Project も DIRECTOR で戻る（AUTO で続けるなら選び直す）。**記録の無い旧カード（#1116）**: `generation_approval`・`generation_launch` はバンドルに含めないので、取り込んだ Project の旧カードには記録が無い（クラッシュで記録だけが残らなかったカードも同じ）。**記録の無いカードは、どの答えでも、有効な承認として扱わない**（`respond_approval`・ボタン・文章のどれで答えても、そのカードが生成を始める根拠になることはない）。生成を始める根拠は、AUTO の Project では AUTO を選んでいること（同意）だけで、カードの有無・記録の有無・答えに依らない。したがって、①AUTO の未回答カードは、記録の有無に関わらずシステムが閉じ、自動の開始は AUTO のものとして進む（カードの承認ではない）。②AUTO で差し戻された記録の無いカードは、記録が無く計画と比べられないので、**計画を直しても解けない**。カードへの返信ではないユーザーの発言（チャットへのメッセージ）だけで解ける（記録のあるカードは、発言か計画の変更で解ける）。画面（Scene / Shot 画面の上部とバッジ）とチャットの案内は、この場合「計画を直すと始める」とは言わず、メッセージを送ることと手動で起動することだけを示す。③DIRECTOR・MANUAL では、記録の有無に関わらず何も自動では始まらない。未回答のカードに「承認」と答えると Project は VISUAL_DESIGN に戻るだけで何も起動せず、差戻しはカードを差戻しのまま残す（あとで AUTO にしても、差戻しのあとにチャットへメッセージが無ければ自動では始まらない。差戻しの返答はこの解け方も正確に書く: 記録のあるカードは「メッセージを送るか、計画を直す」まで、記録の無いカードは「メッセージを送る」まで）。④チャットの返答は、記録の無いカードを承認したとき「記録が無いため承認としては扱わない」と書き、AUTO ではそれでも AUTO 自身の開始が進むこと、AUTO 以外では生成が自動では始まらないことを書く（差戻しの返答は上の②③のとおり）。カードの本文の下にも同じ注記を出し（`data-testid="approval-unrecorded"`。未回答・承認済み・差戻し済みのどれでも）、MCP の `list_chat_messages`・`get_chat_message`・`respond_approval` の `message.unrecorded` も同じ事実を返す。**「最新」の旧カードの決め方（#1116）**: 最新の旧カードは、**この環境が行を作った順（SQLite の `rowid`）**で決める。メッセージ ID と `created_at` は取り込んだバンドルが持ち込むので、基準にしない（巨大な ID のカードが、あとでこの環境が作ったカードより「最新」になり続けることを防ぐ）。取り込みはバンドルの行を書き出された順（元の `rowid` 順）に作るので、正規のバンドルでは元の順が保たれる。「差戻しのあとの発言」の判定と、脚本など他のチェックポイントのカードの「最新」（`LatestApprovalCard`）も同じ順で決める。Brief の要約カード（`brief_confirm`。最新の 1 枚だけが回答できる）、最新の未回答の Brief の質問、カードへの最新のユーザーの返信、`kind` ごとの最新のメッセージも同じ `rowid` で決める（巨大な ID の要約カードが、この環境で作った新しい要約カードを閉じ続けない）。スレッドの `after` の続き（`GET /projects/:id/messages?after=`・`list_chat_messages` の `after`）も、メッセージ ID の大小ではなく `rowid` で「それ以降」を決める。
> - **差戻しのあとの発言の決め方（#1116）**: 差し戻された旧カードを解く「差戻しのあとの発言」は、**差戻し（ユーザーがカードに返信して差し戻した、その返信）より後**に、カードへの返信ではないユーザーの発言が書かれたことで決める。基準は、**カードより後に書かれた**カードへの最初のユーザーの返信（`rowid` が最小。カードへの返信が差戻しの答え。取り込みでカードより前の `rowid` になった返信は差戻しの答えではない）で、カードより後の返信が無ければカード自身。差戻しのあとにカードへ返信が足されても（答え済みのカードへの返信も通常の発言として受け付ける）、基準は動かず、その間に書かれた自由な発言は差戻しを解く。カードが作られてから差戻しまでの間の発言（「ちょっと待って」のような、差戻しに至る会話）は、差戻しより前なので解かない。順序は上の `rowid` で決める。取り込んだ Project で差戻しの返信が無いカードは、カードの `rowid` が基準になり、巨大な ID でも順序は変わらない。
>
> 待機の理由は見える（#1108・#1115）: 計画完了のチャット通知と Scene / Shot 画面の上部は、自動で進めている（「AUTO を選んでいるので承認を待たずに Reference Pack → Shot 生成を自動で進める」。費用がかかる）、旧カードの差戻しで自動では始めない（次の行動。記録の無いカードの差戻しは計画を直しても解けないので、メッセージを送ることと手動で起動することだけを示す）、自動では進められない Shot（Reference Pack の失敗・予算上限・起動の失敗。手動で起動する）を示し、Reference Library・World State の Character が 0 件のときは「生成はできるが顔や見た目の一貫性を保つ参照が無い」も示す。PLANNING のままで Take の無い Shot のカードには「自動開始待ち」（旧カードの差戻しの間は「自動開始を保留」、DIRECTOR・MANUAL は従来どおり「生成待ち」）を出す。

---

## 4.4 Confirmation Levels

AUTO MODEでは、人間が確認する頻度を選択できる。

### FULL AUTO

制作開始後、Finalまで原則確認しない。有料の生成を始める前にも確認は出さない（AUTO を選ぶことが生成を始める同意。始めるときのお知らせと止め方は §4.3）。

### KEY CHECK

主要マイルストーンのみ確認する。

標準ポイント：

1. Concept / Script
2. Rough Cut
3. Final

生成の開始は確認しない（AUTO では Scene / Shot 計画の直後に自動で始まる。§4.3）。

> Main Visual / Character / Product の確認は、該当する成果物の完了を検知して承認を求める段が無いため標準ポイントに含めない。

### CUSTOM CHECK

ユーザーが確認ポイントを自由設定する。選べるのは、パイプラインが承認カードを出す Script・Rough Cut・Final（`script` / `rough_cut` / `final`）。生成の開始（`generation`）は確認ポイントではなく、選択肢には出さない（AUTO の意図を優先して確認しない。§4.3）。

例：

- Scriptのみ確認
- Rough Cutのみ確認
- Finalのみ確認

> キャラクターデザイン・商品Key Visual・各Scene完成時の確認（`character_design` / `product_key_visual` / `scene_complete`）と Main Visual（`main_visual`）は、該当する成果物の完了を検知して承認を求める段が無いため、選択肢に出さない（`domain.Checkpoint` に定義だけがあり、承認を出す段は実装していない。Reference Pack・Keyframe を作る前の確認は持たない。生成の開始は §4.3 のとおり確認せず、始めるときのお知らせと予算上限で扱う）。成果物ごとの確認を足すときは、完了を検知して承認を求める段を実装して選択肢に加える。

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

> **実装の範囲**: 4 つ目の例（「Take B の演技は残して、カメラだけ Take C に近づけて」）のように、別々の Take の要素（演技とカメラなど）を取り出して組み合わせる指示は未対応。Command Interpreter は Take を参照できず、Shot の現行 Spec からの再生成になる（Take を組み合わせる操作は §14 の Partial Take Use による区間の合成だけ。DIRECTOR の指示で構造化できる操作は §24 Example 4）。

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

> **実装の対応範囲（Editing / Music / Dialogue）**: Editing が MANUAL / LOCK の Shot は、Rough Cut を作り直しても（`POST /projects/:id/edit/rough-cut`）ユーザーが編集したクリップ（In / Out・字幕・Transition）をそのまま残し、Editor Agent の Pace / Hook / CTA Timing / Caption の調整もそのクリップには行わない。Music が MANUAL / LOCK の Project は、音声を作り直しても（`POST /projects/:id/audio/generate`）直前の Audio Timeline の BGM を引き継ぎ、BGM の再計画・再生成（と課金）をしない。Story が MANUAL / LOCK の Project は、Shot の完了後も State Extractor が World State の Story（`scene/character_goal/known_information/previous_event`）を更新しない（MANUAL は AI による更新だけを止める）。Dialogue は Writer が PLANNING で書く脚本のセリフであり、Shot が存在した後に AI がセリフを書き換える経路が無いため、常に変更されない（Audio Director は脚本のセリフをそのまま読む）。

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

> **使えるモデルの選択（実装）**: 生成の開始前に、割り当てのモデルが今使えるか（API キー・ログイン・導入）を確認する。既定の割り当てが使えないときは、条件（用途・入力・解像度と長さ・成人向けでないこと・ライセンス・Quality Policy）に合う、今使えるモデルで生成する。利用者が選んだ割り当てと Shot で固定したモデルは、使えなくても別のモデルへ替えず、原因と直し方を添えて断る。使ったモデルとその経路・理由・費用の扱いは Take から辿れる。詳細は `spec/architecture/model-adapters.md` §3.1。

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

> **起動の経路（実装）**: モードは Quality Policy で決まる（MAX QUALITY→Quality、BALANCED→Balanced、FAST→Economy）。Shot カードの「生成」、Continue、Branch、追加撮影（Editor）、Director の再生成は、AI Video Judge が構成済みで Prompt が LOCK されていなければ Candidate 生成（`POST /shots/:id/generate/candidates` と同じ処理）を起動する。Judge が無い、または Spec 済みの Shot の Prompt が LOCK されているときは、Best Take を選べない／Spec を作り直せないため単発の生成（`POST /shots/:id/generate`）になる。`/shots/:id/generate` を直接呼ぶと、常に単発の生成である。 Candidate は撮影戦略ごとに Spec を書き直すが、LOCK の部分と指示で保持する部分に加えて、指示が無いときは MANUAL の部分（ユーザーが指定した値）も変えない（指示があるときの MANUAL は、指示が変えない限り保持するようプランナーに伝える）。戦略の指示は MANUAL の部分には及ばない。

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
- 抽出に失敗しても Take の生成は失敗させない（World State は更新されず、次の Shot は現在の World State から始まる）。 `story.known_information` は 20 件まで（1 項目 200 文字以内）で、State Extractor には現在の項目を保ったまま追加した完全なリストを、20 件を超えるときは古い／重要でない項目を落として 20 件以内で返させる。超えたリストは適用せず（World State は据え置き）、警告ログに残す。

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
- **LOCK 違反の判定**: AI Video Judge は、Shot が使う Character / Product / Location の LOCK を次の評価軸に対応付け、その軸の Issue が severity medium 以上なら LOCK 違反とし、スコア・確信度に関係なく Take を FAIL にする（§13）。ただし顔の数値評価（`face_identity_drift`）は FAIL 閾値を割った連続（severity high）だけを LOCK 違反とし、警告閾値だけを割った連続（severity medium）は違反にしない（§12.4）。`identity` → `character_identity`・`face`、`wardrobe/hairstyle` → `character_consistency`、`geometry` → `product_geometry`、`label` → `logo_text_accuracy`、`orientation/material/condition` → `product_accuracy`、Location の `time/lighting_state` → `lighting`、`weather/layout_reference` → `background_consistency`。Shot 自身に LOCK したパラメータ（`PUT /shots/:id/locks`）も同様に対応付ける: Character Identity → `character_identity`・`face`、Product Geometry → `product_geometry`、Acting → `acting`、Camera → `camera_motion`、Lighting → `lighting`（Dialogue・Prompt・Video Model・Take Selection・Editing は評価軸を持たないため対象外）

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

名前とタグの部分一致（`q`）、種別（`kind`）、タグの完全一致（`tag`）で絞り込む。`q` と `tag` は大文字小文字を区別しない（ASCII 以外の英字・アクセント付き文字・キリル文字も同様）。新しい登録順に並べる。

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

- **First Frame**: Reference Pack の Keyframe（Art Director が用意した生成画像）だけを動画モデルの First Frame として渡す。ユーザーが選んだ画像を First Frame にする経路は無い。Model Adapter がモデルごとに送信する（Kling 3.0 `image`、Seedance 2.0 `first_frame`）。Seedance 2.0 の API は First Frame（と Last Frame）と自由な参照画像（`reference_image`、上限 9 枚）を同じリクエストに混在させられない（排他のシナリオ）ので、Keyframe があるときは First Frame だけを送り、Reference Pack の他画像は送らない。Reference Reinforcement だけは Keyframe を外して他画像（`reference_image`）を送る。Kling 3.0 と Runway Gen-4.5 は自由な参照画像を受け取らないので、First Frame だけを送る（Last Frame は送らない。下の Last Frame の項）。Generation Orchestrator のモデル選択は、Keyframe があるとき First Frame に対応するモデルを適合（fit）で優先する。
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
- **手順**: Take のフレームをサンプリング → 顔検出 → 顔埋め込み → 参照顔とのコサイン類似度を算出。計測対象は Shot Spec の Character（`shot_spec.character`）1 人で、Shot の他の Character の顔は計測しない。1フレームに複数の顔がある場合は、対象の Character の参照顔に最も近い顔を割り当てる
- **出力**: Judge Result の `metrics.face_identity`（対象 Character の `similarity_mean` / `similarity_min` / `sampled_frames` / `undetected_ratio`。配列だが現状の要素は 1 つ）。閾値割れが連続した区間は `face_identity_drift` Issue（`time_range` / `frames` 付き）として出力する
- **欠測**: 顔を検出できないフレーム（後ろ向き・遮蔽・極小）は欠測として扱い、FAIL 判定に含めない。`undetected_ratio` が高く数値評価が成立しない Take は、定性評価（Gemini）のみで判定する
- **閾値**: 警告 / FAIL は、その Character の参照画像同士の類似度分布から決める（ソースコードの固定値では決めない）。顔が取れた参照が 2 枚以上あり、参照同士の平均コサイン類似度がキャリブレーション下限（Judge 設定 `min_calibration_similarity`）以上のとき、警告閾値は平均 × `calibration_warn_scale`、FAIL 閾値は平均 × `calibration_fail_scale` とする（スケールは Judge 設定で、閾値そのものは分布から出る）。参照が 1 枚、または分布が使えない（参照が 2 枚未満、または平均が下限未満）ときは数値の警告 / FAIL を出さない（`calibrated = false`、`evaluated = false`）。類似度は記録し、合否は定性評価に任せる
- **LOCK 連動**: Character Identity が LOCK のとき、FAIL 閾値を割った Take は総合スコアに関係なく FAIL とする（§13）
- **Repair**: `face_identity_drift` は Reference Reinforcement を優先候補とする
- **Series**: Episode をまたいでも Series 正本の `face_reference` を基準に計測する（F-29）
- **実行**: ローカル実行。顔画像は外部へ送信しない。YuNet / SFace と ONNX Runtime（purego、CGO なしの Go 単一バイナリ）は M1 で検証済み（`spec/architecture/model-adapters.md` §10.1）。残る確認は SFace の学習データの権利関係（同 §8）。Windows のインストーラーは ONNX Runtime の DLL を同梱しており数値評価が動く。DLL かモデルが無いとき（ポータブル版・モデル未配置）は定性評価にフォールバックする（`spec/requirements/non-functional.md` §1、`spec/architecture/model-adapters.md` §10.1）
- **生成テストの「画像から似せる」への利用**: 生成した画像の顔と参考の顔のコサイン類似度を、LLM の似てる度に重み付きで混ぜられる（設定 `models.json` の `gentest.match_face`。既定は混ぜない。顔が取れないときは LLM の点だけ。`spec/architecture/gentest/prompt-match.md`）。この用途の閾値は上の Character 別の較正とは別で、固定の写し方（`floor` / `ceil`）を使う

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

- Max retries per Shot は、新しい Take を作った再生成系の Repair の数と、Prompt Candidate Generation が作った Take の数（最初の 1 本を除く）の合計で数える。Max cost per Shot は Shot の予算上限（`shot_budget_ceiling`）で、その Shot のコスト台帳の累計（それまでの Take・Reference Pack・Keyframe の分を含む）に次の再生成の見積りを足した額が上限を超えるなら再生成しない（Project の予算上限も、Project の累計に見積りを足した額で同じように検査する）。上限に達したら Escalation（チャットのカード）で人間に判断を求める
- 予算上限は承認の代わりではなく、AUTO MODE が承認なしに生成を始める（§4.3）ようになっても効く。上限が無い（nil）なら上限による停止は無い（単価が不明でも始める。お知らせに書く）。上限は Reference Pack・Shot ごとの起動で効き、超えるなら `ErrBudgetExceeded`（402）でその起動を止め、Escalation で人間に判断を求める
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

実装では、Continue Agent への入力は Brief・Scene/Shot の目的・Storyboard・Canonical Shot Spec・World State のテキストに加え、ffmpeg が使えるときは起点にする Take（選択済みの Take、なければ AI Pick、なければ最新の却下されていない Take）の**実際の最終フレーム**の画像である。画像と Shot Spec が食い違うときは画像を正とする。ffmpeg が無い、または最終フレームを取り出せないときは、Shot Spec と World State のテキストだけから解析する（Continue は失敗させない）。次の Shot の First Frame は、この最終フレームにはならない。Continue は追加した Shot の Reference Pack を作らないので、First Frame は他の Shot と同じく、その Shot で Reference Pack を作って Art Director の Keyframe ができたときだけ送られる（§11）。

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

> この選択（Continue）は、ユーザーが次の展開を選んで生成を始める操作なので、§4.3 の AUTO の生成開始（自動の開始）とは別の経路で、確認もお知らせも要求しない。生成は Quality Policy と予算上限に従う。

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

> Branch の生成の起動も、ユーザーが Branch を作る・選ぶ操作なので、§4.3 の AUTO の自動の開始とは別の経路で、確認もお知らせも要求しない。予算上限は従来どおり効く。

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

Loudness は全プラットフォーム共通で −14 LUFS / −1.5 dBTP 固定で正規化する。Brief の配信先に応じたプラットフォーム別の目標（Platform Optimization）は未実装（将来の拡張）。

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

Brief が掲載先を TikTok・YouTube Shorts・Instagram Reels と読み取れるときは、Platform Format の既定として Aspect Ratio 9:16 と、その掲載先がふだん使う範囲の中の Duration（TikTok 15秒、YouTube Shorts 30秒、Instagram Reels 15秒）を、仮の推奨値として Brief に入れる。値の表と規則は §4.2。掲載先が長さを一意に決めるわけではなく、利用者が示した値が常に優先する。

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

利用者の好みや運用方針は、「User Preferences」という別の項目としては持たない。確認の好みは Confirmation Policy（§4）が、予算・品質・リトライの好みは Budget / Quality Policy（§25）が保持する。

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
AWAITING_APPROVAL (必須: 生成する内容と費用の承認。確認レベルに関わらない。AUTO MODE のみ)
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

> **Project 状態と Shot 状態（実装の範囲）**: 上の状態遷移のうち、Project の状態として実際に遷移するのは DRAFT → PLANNING → VISUAL_DESIGN（Orchestrator）、確認ポイント（と v0.1.34 が残した旧「生成の承認」カード）での AWAITING_APPROVAL、および EDITING 以降（EDIT_JUDGING・AUDIO・FINAL_QC・READY・EXPORTED）である。EDITING の開始は `POST /projects/:id/edit/rough-cut` だけではない。ユーザーが Rough Cut を作る／編集したときに加え、AUTO MODE ではトランクの全 Shot に Selected Take が揃った時点で Rough Cut と Editor Judge が自動で始まり EDITING に入る。開始時、VISUAL_DESIGN からは GENERATING → JUDGING → EDITING を一括で辿る（全 Shot に Selected Take があることが前提）。確認ポイント通過後の AUDIO と Final QC も AUTO MODE では自動で進み、Export はユーザー操作である。GENERATING / JUDGING / REPAIRING は Shot 単位（`shot.state`、§13）で進み、Shot の生成・Judge・Repair は Shot ごとに並行して起動されるため、Project 全体の状態としては遷移させない（Project は VISUAL_DESIGN のまま。AUTO の生成開始は承認カードを出さずに Project を VISUAL_DESIGN のまま Shot 単位で進める。旧「生成の承認」カードに答えたとき、またはシステムが未回答の旧カードを閉じたときは、Project は AWAITING_APPROVAL から VISUAL_DESIGN に戻る）。例外は Reshoot Loop の追加 Shot（Project を GENERATING にする）と、Escalation への回答後（Project は REPAIRING になり、Editor Judge／編集操作で EDITING へ戻る）である。

図の AWAITING_APPROVAL は確認ポイント（脚本・Rough Cut・Final）の位置を示す（生成の承認（`generation`）は #1115 で廃止した）。AWAITING_APPROVAL に遷移するのは、確認ポイントの Approval カードと Escalation カードのみとする。ただし Escalation カードで AWAITING_APPROVAL に入れるのは、PLANNING・VISUAL_DESIGN・GENERATING・JUDGING・REPAIRING・EDITING・EDIT_JUDGING・FINAL_QC のいずれかにいるときだけである（`domain.stateTransitions` はこれと一致させる。AWAITING_APPROVAL から戻れるのは PLANNING・VISUAL_DESIGN・GENERATING・REPAIRING・EDITING・AUDIO・READY で、確認ポイントの承認 / 修正依頼と Escalation の再開先から決まる）。DRAFT・AUDIO・READY・EXPORTED で出た Shot 単位の Escalation（書き出し後の Repair 上限到達など）はカードを投稿するだけで Project の状態を変えず、そのカードは確認ポイントを待たせず、回答しても REPAIRING にはならない。Notes の昇格提案カード（`ref_type=note_promotion`、§9.4）は非ブロッキングで、Core State Machine の状態を変えない。

---

# 23. UI Concept

## 23.1 Chat Workspace（標準画面）

標準画面は1プロジェクト＝1本のチャットスレッドとする。ユーザーの操作は原則すべて、このスレッドへの発言とスレッド内カードへのボタン操作で完結する。Scene/Shot/Take の階層、Inspector、Take比較、タイムライン、Branch は補助ビュー（§23.3）であり、使わなくても制作を進められる。ただし書き出し（Export）は補助ビュー「編集 / 書き出し」から行う（ヘッダーに Export ボタンはない）。

```text
┌────────────────────────────────────────────────────┐
│ PROJECT: Serum Commercial               [ ⋯ 補助ビュー ] │
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
│     │ ▶ Take 2（動画プレーヤー）    │              │
│     └──────────────────────────────┘              │
│ AI: Take 2 の Judge 結果: PASS（信頼度 0.91）       │
│     AI Pick: Take 2 を採用しました。               │
│                                                    │
│ あなた（上のカードに返信）: 商品に寄って。          │
│   ラベルは固定して、高級感を強めて                  │
├────────────────────────────────────────────────────┤
│ [＋] 指示を入力…                          [ 送信 ] │
│ MODE [ AUTO | DIRECTOR ]   Quality: Balanced        │
└────────────────────────────────────────────────────┘
```

- **Brief は会話で作る**: 最初の発言から Creative Brief を抽出し（すでに述べた項目は聞き直さない）、決まっていない項目には目的から決めた推奨値を仮に置き、全体を 1 枚の確認カード「この内容でよいですか？」で確認する。質問するのは Subject を決められないときと食い違いだけである（Subject に「AIに任せる」は併記しない。§4.2、#1114）。確定した Brief は補助ビューで確認・編集できる。推奨値は「推奨（仮）」と表示され、保存すると確定する
- **確認ポイントはスレッド内カード**: Confirmation Level で設定した確認ポイント（脚本/Rough Cut/Final）は、承認カードとして表示する（Escalation は承認カードではなく、§23.2 の別カードである）。ボタン（承認 / 修正を依頼）か、自然言語の返答で応答できる。返答が承認になるのは、前後の空白と句読点（。 . ! ！）を除き大文字小文字を区別せず、承認の語（「承認」「承認します」「承認です」「OK」「okay」「はい」「いいね」「いいです」「問題ない」「問題なし」「LGTM」「お願いします」）に全文が一致するときだけで、それ以外の返答は本文を添えた修正依頼になる（「いいけどもう少し明るく」も修正依頼）。応答があるまで Core State Machine は AWAITING_APPROVAL に留まる（§22。Notes の昇格提案カードを除く）
- **発言の対象**: 指示の対象 Scene/Shot/Take は、カードへの返信（引用）で明示するか、直前の文脈から AI が解決する。解決できない場合は AI が聞き返す。§24 の Command Interpreter がこの解決を含めて構造化操作へ変換する
- **MODE / Quality**: 入力欄の下に小さく置く。AUTO では AI が主導し、DIRECTOR では指示した箇所だけを直して待機する（DRAFT の間の発言は、メモの依頼を除き Brief への回答として扱う）。切替はいつでも可能で、状態は保持される
- **添付**: 画像・動画をドラッグ&ドロップまたは［＋］で添付でき、Assets/References として Brief に取り込まれる
- **非同期**: 生成・Repair の進捗はスレッド内の進捗カードにリアルタイム表示される。Judge は Take ごとに自動で走るため進捗カードは出さず、実行中は Shot カードの JUDGING 表示（Shot 進捗 WebSocket）で示し、完了時に結果を発言として投稿する。入力は生成中も可能

## 23.2 スレッド内カード

| カード | 内容 | 操作 |
|--------|------|------|
| Question | Brief の Subject の質問・食い違いの確認・Brief 全体の確認（「この内容でよいですか？」。生成の承認ではない）・Continue の次の方向の確認。DIRECTOR MODE の曖昧な指示の聞き返し（対象 Shot が決まらないなど）は Question カードにせず通常のテキストで返し、ユーザーは Take カードへの返信か「Scene 1 の Shot 2」のような次の発言で答える | 選択肢ボタン / 自由入力 /「AIに任せる」（Subject と Brief の確認カードには出ない） |
| Approval | 確認ポイントの成果物（脚本・Rough Cut・Final）。v0.1.34 が出した旧「生成の承認」カードは読めるまま残る（§4.3。#1115 以降は新しく出さない）。M2 では Notes の World State 昇格提案（§9.4）も同じカードで提示する。昇格提案は非ブロッキング | 承認 / 修正を依頼（昇格提案では「修正を依頼」は却下） |
| Keyframe | Keyframe / Reference Pack 画像 | 拡大、返信で修正指示 |
| Take | 動画プレーヤー（Judge の Issue を時間軸のマーカーで表示）と生成の要約（モデル・解像度・秒数）。スレッドは追記のみなので、Judge が結果を保存するとサーバーはその Take のカードの更新（`card.updated`）を通知して、ライブ中のスレッドでもマーカーを描き直す。Judge 要約と Take 状態（AI Pick / 採用 / 除外）はカードには載せず、Judge が結果を保存するたびに別の発言として投稿する（「Shot N · Take X の Judge 結果: PASS/FAIL（信頼度）」と、AI Pick が変わったときの「AI Pick: Take X を採用/おすすめ」）。Take ごとの最新の状態は Take 比較で確認する | 再生、返信で指示、Take比較を開く |
| Progress | 生成・Repair の進捗（Judge の進捗は Shot カード側、結果は発言で通知） | なし（自動更新） |
| Escalation | Retry Policy 上限到達などで AI が判断を求める内容。回答（選択肢ボタン / 自由入力）は記録されるだけで自動実行されない。回答後は（Project がこの回答待ちで AWAITING_APPROVAL にいたとき）REPAIRING に戻り、AI は「この判断は自動では実行されません」と返すので、ユーザーが設定の変更・Take の採用・Shot の指示の修正を画面から行う | 選択肢ボタン / 自由入力 |

## 23.3 補助ビュー

チャットの横に開く/別ページで開くビューで、スレッド内カードや［⋯］メニューから到達する。いずれも操作結果は同じ Project の状態に反映される。スレッドに発言またはカードとして記録されるのは、Scene・Shot の追加、Brief の更新と Confirmation Level の設定、Quality Policy・予算上限・Production Template の変更、編集・音声・書き出しの各工程（Editor / Audio / Export）、Review Response の取り込み、Take の部分採用（つなぎ込み）、DIRECTOR の指示、AUTO ⇄ DIRECTOR のモード切替（通知の発言）、Continue による次の Shot・Scene の作成で、Take の採用/除外、LOCK の変更、Shot Spec の手動編集、World State の編集・復元、Branch の作成・改名、Notes、Brand Constraint の操作はスレッドには記録されない（各ビューの表示と履歴で確認する）。

- **Inspector**（選択中 Shot の詳細）: Shot Objective / Duration / Character / Acting / Camera / Lighting / Product / Continuity / References / Generation Model / Take List / Judge Results / Locks。モデル別プロンプトは Inspector に常設せず、各 Take の「生成の経緯」（Generation Trace、`GET /takes/:id/trace`）で確認する。各項目は AUTO / MANUAL / LOCK を切替可能
- Take 比較、Timeline、Branch ツリー、World State、Notes（M2、§9.4）、Cost、Settings

---

# 24. Natural Language Director Commands

自然言語入力をCommand Interpreterが構造化操作へ変換する。入力はチャット画面（§23.1）の発言で、対象 Scene/Shot/Take はカードへの返信または直前の文脈から解決し、解決できない場合は聞き返す（通常のテキストで返す。§23.2）。チャットの発言を解釈するのは DIRECTOR MODE（企画が始まった後 = Project の状態が DRAFT でないとき。DRAFT の間は対象になる Scene/Shot が無いので解釈せず、Brief が揃っても DIRECTOR のままなら AUTO に切り替えるまで企画は始まらない）と、どのモードでもメモの依頼語を含む発言だけで、AUTO MODE の指示は受けない（指示としては解釈しないが、保存した発言をスレッドで放置せず、処理を始められない理由を AI が一言返す（#1107）。DIRECTOR MODE への切替は画面のモード切替から行う）。

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

> **コストの扱い（モデルの経路別）**: ローカルモデルは無料（0 USD）、ChatGPT サブスクは単価不明（0 とは扱わず、合計にも足さない）、API は単価で見積もる。予算上限は、単価が分かるモデルに従来どおり効く。既定の割り当てが使えず別のモデルへ切り替わったときも、切り替え後のモデルで見積もる（`spec/architecture/model-adapters.md` §3.1）。

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

JSON ファイル 1 つ（`format=mam-review-response`・`version`・`response_id`・`review_id`・`reviewer_name`・`decision`（`approved` / `changes_requested`）・`comment`（全体コメント、2000 文字以内）・`comments`（最大 200 件。`timecode_ms`（省略可、動画内の位置）・`body`（1〜2000 文字））・`created_at`）。判定を書いた後にだけ書き出せる。コメントや判定を変えると `response_id` が変わり、変更前のファイルとは別の応答として取り込まれる。`response_id` は取り込むと `review_response.id`（行の主キー）になるため、プロジェクトバンドルの行 ID と同じく `[A-Za-z0-9_-]{1,64}` で、`reviews` と Windows の予約デバイス名（`CON`・`NUL`・`COM1` など。大文字小文字を問わない）は取り込まない（`bundlepath.ValidID`。書き出したバンドルを取り込めなくならないため）。

### レビューの状態

レビュー依頼の状態は、レビュアー表示名（前後の空白は無視する）ごとの最新の応答から決める。最新とは応答の作成日時（`created_at`、レビュアーが書いた順）が最も新しいもので、同時刻なら後に取り込んだもの（取り込む順序が前後しても、古い版が新しい版を上書きしない）。応答が無ければ `pending`（未回答）、全員の最新が `approved` なら `approved`（承認済み）、1 人でも `changes_requested` なら `changes_requested`（修正依頼あり）。承認はオーナーへの意思表示であり、Project の状態遷移（§22）や Final Export をブロック/解除しない。応答を取り込むと、Chat Workspace のスレッドに AI の発言として「レビュー結果（表示名・判定・コメント件数）」が記録される（§23.3）。

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
| 採用された Shot | `shot.selected_take_id` がある Shot（AUTO では Judge を通過し Minimum acceptance score を満たした AI Pick、DIRECTOR ではユーザーの選択） |
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
- 使ったモデルの経路（API / ChatGPT サブスク / ローカル）と選んだ理由（割り当て、または条件に合うモデルの自動選択）・費用の扱い
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
