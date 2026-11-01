# Model Adapter 選定（LLM / 画像生成 / 動画生成 / 音声）

`spec/architecture/overview.md` §2 の「エージェント推論用LLM連携」「外部生成モデル連携」を具体化し、どのプロバイダー・モデルを Model Adapter / Reasoning LLM Adapter の対象とするかを定義する。2026-09時点の最新モデルから選定。

## 1. 目的・原則

- **2層構造**: 自然言語推論を担う **Reasoning LLM Adapter**（Orchestrator/Producer/Writer/Director/Judge 等の思考エンジン）と、画像/動画生成を担う **Model Adapter**（`spec/components/overview.md` 既定義）は別レイヤーとして扱う。前者は Agent の「頭脳」、後者は「撮影機材」に相当する
- **ベンダーロックイン禁止**（`spec/overview.md` 「含まないもの」）: 各カテゴリで複数プロバイダーを採用し、Agent種別/用途/Quality-Costポリシーに応じてルーティングする。特定ベンダーのAPI仕様に Canonical Shot Spec や Agent ロジックを直接依存させない
- **Repair Agent の "Different Model" 戦略**（F-14）と **Quality/Cost Policy**（F-22: FAST/BALANCED/MAX QUALITY）は、本ドキュメントのプライマリ/代替の切り替えとして実装する
- 本書はプロバイダー候補と選定理由（アーキテクチャ決定）を記録するものであり、SDKバージョン・エンドポイント等の実装詳細は `internal/credential`, `internal/agent/*` 側で別途管理する
- 生成AI市場は変化が速いため、本書は半期ごとに見直す（§8）

## 2. Reasoning LLM Adapter（エージェント推論）

| Agent | プライマリ | 代替（Different Model / レート制限時） | 選定理由 |
|-------|-----------|--------------------------------------|---------|
| Orchestrator Agent | Anthropic Claude Opus系 | OpenAI GPT-5系 | 状態機械運用のような長時間・多段の自律タスクでのエージェントベンチマーク最上位。判断の一貫性が Core State Machine の安定運用に直結 |
| Producer Agent | OpenAI GPT-5系 | Claude Opus系 | Creative Brief からのコンセプト展開はクリエイティブライティング適性が高いモデルを優先 |
| Writer Agent | OpenAI GPT-5系 | Claude Opus系 | 脚本生成も同様にクリエイティブライティング適性を優先 |
| Director Agent / Storyboard Agent | Claude Opus系 | Google Gemini 3.1 Pro | Scene/Shot Planningという構造化出力の安定性・再現性を優先 |
| Art Director Agent | Google Gemini 3.1 Pro | OpenAI GPT-5系 | Reference Pack構築は参照画像解析を伴うマルチモーダル入力+ 大コンテキストが有利 |
| Prompt Planner | Claude Opus系 | Google Gemini 3.1 Pro | Canonical Shot Spec（YAML）という構造化出力の安定生成、Prompt Candidate Generation（Stability First/Acting First/Dynamic Camera）でのツール的思考 |
| AI Video Judge | Google Gemini 3.1 Pro | OpenAI GPT-5系 | 生成動画のフレーム/時間単位解析はネイティブマルチモーダル・長尺コンテキストに強いモデルを優先 |
| Repair Agent | Claude Opus系 | OpenAI GPT-5系 | Judge Issueに基づく複数戦略（Prompt修正/部分再生成/別モデル選択等）の選択はツール利用を伴うエージェントタスクの適性を優先 |
| Editor Agent / Editor Judge | Google Gemini 3.1 Pro | Claude Opus系 | Rough Cut全体の長尺タイムライン解析は大コンテキスト窓が有利 |
| Audio Director | OpenAI GPT-5系 | Claude Opus系 | Scene Emotion・編集テンポの解釈にマルチモーダル入力適性を優先 |
| Command Interpreter | Claude Opus系 | OpenAI GPT-5系 | 自然言語 DIRECTOR 指示 → 構造化操作への変換精度・一貫性を優先 |
| State Extractor | Google Gemini 3.1 Pro | Claude Opus系 | World State全体からの差分抽出は大コンテキスト窓が有利 |
| Final QC | Claude Opus系 | Google Gemini 3.1 Pro | Brand/Safety制約適合の厳格判定における一貫性を優先 |

注記: 現在の実装では、Orchestrator Agent と Final QC のモデル割り当て（`orchestrator` / `final_qc`）はどのコードからも参照されない。Orchestrator は Core State Machine の遷移を決定的に進め（Producer 以降の LLM 呼び出しは各 Agent の割り当てに従う）、Final QC は決定的なチェックのみを行う（`spec/components/overview.md`）。この 2 行は将来 LLM 判定を導入するための予約で、設定ファイルでこの 2 つの Agent のモデルを変えても動作は変わらない（全体の設定画面の「LLM の割り当て」では、この 2 つを折りたたみの「現在は使われません」にまとめる。値は編集・保存でき、検証は他の Agent と同じ）。

補足: 上表は各モデルの相対的な強み（Claude=長時間エージェントタスク・構造化出力の一貫性、GPT-5系=クリエイティブライティング・マルチモーダルツール利用、Gemini 3.1 Pro=長尺コンテキスト・マルチモーダル解析・価格対性能）に基づく初期割当であり、実装後のベンチマーク結果に応じて調整可能な設定値として扱う（ハードコード禁止）。

## 3. 画像生成 Model Adapter（Keyframe / Reference Pack）

| 用途 | プライマリ | 代替 | 選定理由 |
|------|-----------|------|---------|
| Character / Product / Location Reference（新規生成、テキスト正確性が必要な素材） | OpenAI GPT Image 2 | Google Nano Banana（Gemini Image） | テキストレンダリング精度・製品写実性が高く、ラベル/ロゴ等 Brand Constraints 適合の素材生成に強い。Google の Imagen 4 は 2026-08-17 に終了し、参照画像を使える Nano Banana が代替になる |
| Style / Lighting Reference、既存素材の参照ベース編集・差分変更 | Google Nano Banana（Gemini Image） | OpenAI GPT Image 2 | 編集・変換・参照ベース変更・キャラクター一貫性維持に強く、World State継承の再生成に向く。代替の GPT Image 2 も参照画像を受け取れるので、同一性の基準が代替でも保たれる |
| 高速反復・低コストプレビュー生成 | Black Forest Labs FLUX.2 | Google Nano Banana Lite（Gemini 3.1 Flash Lite Image、1K のみ） | オープンウェイトでセルフホスト可能（ベンダーロックイン回避の保険）、生成速度重視のCandidate Generation向き。代替は低遅延・低価格の Gemini 画像モデル |
| OpenAI系LLM（GPT-5）と認証・請求を統合したい場合 | OpenAI GPT Image 2 | Google Nano Banana | 品質ベンチマーク最上位、GPT-5系のAgentと同一ベンダーで運用しやすい |
| ChatGPT のサブスクで描く場合（**非公式・既定 OFF**。設定画面で ON にするときにリスクを確認する） | GPT Image 2（`gpt-image-2:chatgpt-subscription`） | 利用者が割り当てる | API キーなしで ChatGPT のプランの枠を使える。ChatGPT の内部 API（`chatgpt.com/backend-api/codex/responses` の `image_generation`）を使うため、予告なく使えなくなることがあり、公式のプレビューでは画像生成はサポート外で、プランの利用上限を消費する（`spec/architecture/chatgpt-signin.md` §5.2） |

利用者が登録した ComfyUI・OpenAI 互換 API の画像モデルも、上の用途（`new_reference` / `edit_reference` / `preview` / `unified_openai`）の主・代替に割り当てられる（OpenAI 互換は画像のみ。設計は [local-models.md](local-models.md)）。

> **注（Imagen 4 の終了と、参照画像を捨てる場合）**: Imagen 4（`imagen-4.0-*-001`）は Google が 2026-08-17 に終了した（後継は `gemini-nano-banana-2.1`。<https://ai.google.dev/gemini-api/docs/deprecations>）ため、識別子 `imagen-4` ごと組み込みから外した。保存済みの `models.json` が `imagen-4` を指していれば、起動時に Nano Banana へ読み替える（§6）。組み込みの 4 モデル（Nano Banana・Nano Banana Lite・GPT Image 2・FLUX.2）はどれも参照画像を受け取れるので、組み込み同士の代替では参照画像は捨てられない。参照画像を受け取れない登録モデル（`accepts_image_inputs` が偽のローカルモデル。[local-models.md](local-models.md)）が `edit_reference` などの主・代替に回ったときは、参照画像は警告ログを残して**黙って捨てられ**、プロンプトだけの新規生成になる（利用者への通知や失敗扱いはなく、画像の結果にも代替に回ったことは残らない）。その画像ではアップロード素材・World State の見た目やキャラクターの同一性は保たれず、以降の Judge の Identity 評価も参照なしで作られた画像が基準になる。同一性を保ちたい用途では、そのモデルを主・代替に割り当てない。

## 4. 動画生成 Model Adapter（Take生成）

| Quality/Cost Policy | プライマリ | 代替（Different Model） | 選定理由 |
|---------------------|-----------|--------------------------|---------|
| FAST / Economy Mode | Kuaishou Kling 3.0 (Standard) | ByteDance Seedance 2.0 (480p) | 業界最安値クラス。大量SNSショート動画・高頻度撮り直しに向く |
| BALANCED | ByteDance Seedance 2.0 (720p) | Kling 3.0 | ネイティブ音声同時生成でAudio Director側の合成コストを削減。テキスト/画像/音声/動画の最大12参照ファイル入力がReference Pack・World State継承と相性が良い |
| MAX QUALITY / クライアント案件 | Runway Gen-4.5 | Kling 3.0 | 生成間の再現性・一貫性が高くNG率（外れ値）が低い。プロンプト制御力が高くDIRECTOR MODEでの細かい演出指示に向く |

- Repair Agent の **Different Model** 戦略（F-14）は上記3モデル間の自動フォールバックとして実装し、同一 Canonical Shot Spec から別ベンダーへの再試行で品質不足・レート制限の双方をヘッジする
- **M1 実装順**: コスト効率と音声同時生成による検証のしやすさを優先し、Kling 3.0 + Seedance 2.0 の2アダプタを先行実装する。Runway Gen-4.5 は M2 で追加した（§9.1、MAX QUALITY のプライマリ）
- OpenAI Sora 2 は候補から除外: API提供が2026-09-24付で終了済みのため
- ローカルの動画モデルは ComfyUI の接続先に登録したワークフローで、Quality Policy の主・代替に割り当てられる。OpenAI 互換の動画は対象外（公式 Videos API が 2026-09-24 に終了し、互換サーバーごとに仕様が違うため。[local-models.md](local-models.md) §1）

**追加の動画モデル（Issue #872・#873・#874・#875・#876・#878）**: 次の動画モデルは組み込みの識別子（§6）で呼べるが、上の Quality/Cost Policy の既定の主・代替にも Multi-model routing の既定の候補にも**入れていない**（実機で品質と単価を見たうえで別途判断する。使うときは設定画面の「画像・動画モデル」か `models.json` の `policies` で割り当てる）。API・制約・単価は §9.4。

| 識別子 | モデル | 選定の理由と注意 |
|--------|--------|------------------|
| `gemini-omni-flash` | Google Gemini Omni Flash（API の `gemini-omni-1.1-flash`） | Gemini API の Veo 3.1 Preview は 2026-10-22 に終了し、公式が後継に案内するモデル（Veo の識別子は追加しない）。画像と同じ `google` の API キーで呼べる。3〜10 秒・ネイティブ音声つき・720p で約 0.10 USD/秒。縦横比は 16:9 / 9:16 だけ |
| `kling-4.0` | Kuaishou Kling 4.0（API の `kling-4.0`） | ネイティブ最大 30 秒（3〜30 秒）・720p / 1080p。新 API（`api-singapore.klingai.com`・Bearer の API キー 1 つ）で、Kling 3.0 の旧 API（ACCESS_KEY:SECRET_KEY の JWT）とは別のベンダー `kling4`（API キーも別に保存する）。公式は Early Access（2026-10-28 23:00 北京時間に全面提供と案内）で、単価も公式の料金ページでは Preview。既存の `kling-3.0` は旧 API のまま変えない（§9.4） |
| `seedance-2.5` | ByteDance Dreamina Seedance 2.5（API の `dreamina-seedance-2-5-260628`） | BytePlus ModelArk。ネイティブ最大 30 秒（4〜30 秒）・480p / 720p / 1080p・参照アセット最大 50（画像 30）・ネイティブ音声つき。Seedance 2.0 と同じ `seedance` の API キー・ベース URL・同時実行・ブレーカー。単価は ModelArk の公式の料金ページのトークン単価から秒あたりに換算（480p 0.103 / 720p 0.231 / 1080p 0.569 USD/秒）。既存の `seedance-2.0` は変えない（§9.4） |
| `minimax-h3` | MiniMax H3（API の `MiniMax-H3`） | 新ベンダー `minimax`（API キーは MiniMax の API キー 1 つ・`https://api.minimax.io`・Bearer）。768p / 2k・4〜15 秒（整数）・T2V / First・Last Frame / 参照画像（mam は先頭 5 枚、1 タスクの無料枠まで）。出力の秒単価は 768p 0.08 / 2k 0.13 USD。MiniMax の旗艦で、公式は Pay-as-you-go の API が要るとし、Video Packages は H3 に未対応。既存のベンダー・モデルは変えない（§9.4） |
| `minimax-h3-max` | MiniMax H3 Max（API の `MiniMax-H3-Max`） | 同じ `minimax`。MiniMax と fal.ai が H3 を後調整した、高速生成向けの版。480p / 768p・5〜15 秒・参照画像は先頭 2 枚（H3 Max の無料枠）。出力の秒単価は 480p 0.05 / 768p 0.08 USD（§9.4） |
| `wan-3.0` | Alibaba Cloud Model Studio の Wan 3.0（API の `wan3.0-video`。公式は preview） | 新ベンダー `alibaba`（キーは「ワークスペース ID:API キー」の 1 つ。Model Studio の非同期 `video-synthesis` API、Singapore リージョンの `{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com`、Bearer）。ネイティブ最大 30 秒（2〜30 秒）・480p / 720p / 1080p・T2V / First・Last Frame / 参照画像（最大 10）・ネイティブ音声つき。単価は Singapore の秒単価（480P 0.05 / 720P 0.10 / 1080P 0.20 USD）で、ほかのリージョンは別単価（§9.4）。ローカルの Wan 2.2（Model Manager のカタログ・ComfyUI のテンプレート）とは別物で、そちらは変えない |
| `luma-ray-3.2` | Luma Ray 3.2（Agents API の `ray-3.2`） | 新ベンダー `luma`（API キーは Luma Agents API の API キー 1 つ・`https://agents.lumalabs.ai`・Bearer）。5 秒 / 10 秒・360p（ドラフト）/ 540p / 720p / 1080p・T2V / First・Last Frame（フレームつきは 5 秒だけ）。参照画像は動画では取れない。単価はクレジット（5 秒で 360p 20 / 540p 50 / 720p 100 / 1080p 400、10 秒はその 3 倍）を、公式の既定のリスト価格 1 クレジット = 0.003 USD（Runway Gen-4.5 の 0.01 USD とは別）で USD に換算する（§9.4） |

## 4.5 評価・判断の補助（Judge / Repair 用）

Reasoning LLM Adapter にも動画/画像生成 Model Adapter にも属さない、判定精度と判断コストを補う2つの部品。いずれも生成物ではないため Take / Cost Ledger の対象外（§6）。

| 用途 | 採用 | 実行場所 | 入力 | 選定理由 |
|------|------|---------|------|---------|
| Face Identity Similarity（F-13、`spec/spec.md` §12.4） | 顔検出 YuNet + 顔埋め込み SFace（ONNX、ローカル実行。確定の経緯と検証は §10.1） | ローカル（外部送信なし） | Take のサンプルフレーム、`character.face_reference_asset_id` の参照顔 | Gemini の定性評価だけでは微妙な顔のズレを見逃す恐れがあり、シリーズ制作で致命的になる。数値化により閾値判定と時系列追跡ができる |
| Repair Strategy の前段選別（F-14） | TypeSafe AI Jev（System One API。外部 API、`POST https://api.typesafe.ai/v1/systemone`） | 外部 API | Judge Result の issues（`type` / `severity` / `time_range`）を英語の構造化 JSON にしたもの | Choice で Repair 戦略を選び、確率と confidence を返す。confidence を閾値ゲートに使えるため、低 confidence の場合のみ Repair Agent（LLM）に回して LLM 呼び出しを減らせる |

Jev の利用ルール:

- **テキスト専用**: 画像・動画・音声は入力できない。顔や映像の判定には使わず、Judge が出した Issue の構造化データのみを入力とする
- **前段選別**: Choice で Repair 戦略（`spec/spec.md` §13 の6種）から1つを選ぶ。confidence が閾値以上なら採用、閾値未満なら Repair Agent（Claude Opus 系）へ回す
- **決定的ルールを優先**: LOCK 違反・Critical constraints・Retry Policy の上限判定は決定的ルールで行い、Jev を経由しない
- **言語**: Jev の主学習言語は英語で、日本語を含む CJK は精度が低い。`type` / `severity` 等の英語スキーマ値のみを渡し、日本語の自由記述は渡さない
- **送信範囲**: Issue の構造化フィールドのみ。Brief・脚本・画像・動画は送らない。API キーは `internal/credential` で管理する
- **バージョン固定**: `jev-latest` は使わず、具体バージョンを指定する。モデル更新で選別結果が変わることを避けるため
- **導入手順**: M1 では Repair Agent の LLM 判断のみで動かし、Jev を並走（シャドー）実行して LLM の選択との一致率・confidence 分布・遅延・コストを記録する。許容水準に達した場合のみ前段選別を有効化する。閾値と許容水準は実測で決定する（仕様では固定しない）

## 5. 音声（Audio Director）

| 機能（F-20対応） | プロバイダー | 選定理由 |
|------------------|-------------|---------|
| Dialogue / Voice / Narration（TTS・音声クローン） | ElevenLabs | 音声品質最高水準、秒単位の音声クローニングが成熟したAPIで提供される |
| BGM（Music） | ElevenLabs Music (v2) | Voice/SFXと同一ベンダー・同一APIで統合済み。セクション単位のアレンジ制御が可能 |
| SE / Foley / Ambience | ElevenLabs Sound Effects | テキスト→効果音生成に加え、動画を解析して自動でSEを生成するVideo-to-Sound機能を持つ |
| Lip Sync | Sync Labs | 音声ソースを問わないAPI-first設計。ElevenLabsで生成したクローン音声IDをそのままLip Sync入力に使える連携を前提に選定 |
| Mixing / Loudness最適化 | （AI API不使用）ローカルDSP処理 | ラウドネス正規化・ミキシングは決定的信号処理であり生成AIの対象外。`internal/agent/audiodirector` 内でffmpeg等を用いて実装する |

## 6. `model_name` / `provider` 識別子命名規約

`architecture/er.md` の `TAKE.model_name`、`COST_LEDGER_ENTRY.provider` に格納する識別子は以下の文字列を正規値とする（Model Adapter実装時のenum定義元）。

```
画像: nano-banana, nano-banana-lite, flux-2, gpt-image-2
動画: kling-3.0, seedance-2.0, runway-gen-4.5, gemini-omni-flash, kling-4.0, seedance-2.5, minimax-h3, minimax-h3-max, wan-3.0, luma-ray-3.2
音声: elevenlabs-tts, elevenlabs-music, elevenlabs-sfx, sync-labs
```

`imagen-4` は 2026-08-17 の Imagen 4 の終了で、この集合から外した。過去に保存した Take・Cost Ledger・参照画像（Shot Spec の `references[].model`）・生成テストの履歴には、実際に生成して課金したモデルとして `imagen-4` が残る（書き換えない。表示するだけで、動作を決めない。履歴の `imagen-4` の結果を再実行しようとすると、組み込みのモデルではないので断られる）。**動作を決める設定**だけを起動時（設定画面の読み込み・保存・`--model-config` の固定ファイルも同じ）に読み替える: `models.json` の `images` の `imagen-4` は `nano-banana` に、`fast: true` の `imagen-4` は `nano-banana-lite`（高速な代替。`fast` は外す）に読み、`models.imagen-4` の項目は読み込みのたびに捨てる。ファイルは書き換えない: 設定画面で保存すると `images` と `policies` だけを今の割り当てで書き直し、ほかのキー（`models.imagen-4` を含む）は手書きのままファイルに残る（無視されるだけで害はない）。

利用者が登録した接続先のモデルは `comfyui:<接続先名>/<モデル名>`、`openai-compat:<接続先名>/<モデル名>` で保存する（例: `comfyui:gpu-box/wan22-i2v`）。接続先名・モデル名は作成後に変えられない（Take と Cost Ledger の識別子が指すため。変えるときは削除して作り直す）。見積コストが 0 USD でも Cost Ledger の行は書く。組み込みの識別子は列挙のまま、登録したモデルを足した開いた集合として扱う（[local-models.md](local-models.md) §2）。

ChatGPT のサブスク経由の画像は `gpt-image-2:chatgpt-subscription`（API キー経由の `gpt-image-2` と区別できる識別子。`take.model_name`・`cost_ledger_entry.provider` に制約はないのでマイグレーション不要。見積もり 0 USD で、台帳には 0 円の行を残す）。上の表は組み込みの識別子で、設定で増える識別子も同じ列に入る。

Reasoning LLM Adapter側の呼び出しモデルは Take/Cost Ledger の対象外（生成物ではなく思考プロセスのため）。将来的にLLM呼び出しコストの可視化が必要になった場合は、別テーブル（例: `AGENT_INVOCATION_LOG`）の追加を別途検討する。

## 7. フォールバック / サーキットブレーカー

`spec/requirements/non-functional.md` §6「モデル呼び出し耐性」（プロバイダ単位のタイムアウト・429指数バックオフ・サーキットブレーカー。画像・動画生成ではタイムアウトは再試行せず代替へ、§9.3）は、本書§2〜§4の「代替」列をフェイルオーバー先として実装する。あるプロバイダーのサーキットブレーカーがOPENになった場合、Model/LLM Adapterは同一用途の代替プロバイダーへ自動的にルーティングする。 経路の全モデルがブレーカー OPEN でスキップされたときだけ、その呼び出し全体が `ErrCircuitOpen`（「待って再実行」）になる。プライマリがスキップされ、代替が別の理由（拒否・タイムアウトなど）で失敗したときは、代替の失敗理由で通知する。 年齢確認のゲート（Issue #848。[model-manager/model-manager.md](model-manager/model-manager.md) §6.1）は、この代替には**回さない**: 経路（用途・Quality Policy の主と代替、固定した Video Model）のどちらかが、年齢確認がオフの間の成人向けモデルなら、`Adapter` はどのバックエンドも呼ぶ前に経路ごと `agegate.ErrRefused` で断る。主モデルが成人向けで代替が非成人向けでも、代替の結果には変わらない（利用者に気づかれない結果の変化を作らない）。Multi-model routing の候補としては `Adapter.Unavailable` が外す（理由は「年齢確認がオフ」）。

## 8. 未確定事項・レビュー方針

- 各プロバイダーのSDKバージョン・具体的なエンドポイント・レート制限値は実装フェーズで確定する
- コスト実測値（$/秒、$/画像等）は市場変動が大きいため、`non-functional.md` §6「コスト可視化」機能で継続的に検証し、本書の数値は参考値として扱う
- 動画/画像/LLM市場は数ヶ月単位でモデル改廃が発生するため、本書は半期ごとに見直しレビューを行う（次回レビュー目安: 2027-03）
- Face Identity Similarity の顔検出・埋め込みモデルと推論ランタイム（ONNX Runtime）は M1 で検証済み（§10.1）。残る確認事項は、SFace の学習データの権利関係（配布元リポジトリは重みのライセンス（Apache-2.0）のみを明記し、学習データセットを明記していない）で、商用配布前に法務確認が必要（本アプリはモデルファイルを同梱しない）。閾値は実測で決定する（仕様では固定しない）
- Jev の外部 API 利用は、シャドー実行の結果（一致率・confidence・遅延・コスト）で採否を判断する。`spec/requirements/non-functional.md` §5 の外部送信方針との整合を導入時に確認する

## 2.1 実装（`internal/agent/llm`）

- 設定: Agent ごとの `routes`（`primary` / `fallback` の `provider`+`model`）、`providers`（`base_url`）、`timeout_seconds`（既定 120）、`retry`（`max_attempts` 3 / `base_delay_ms` 1000 / `max_delay_ms` 16000）、`breaker`（`failure_threshold` 5 / `open_seconds` 30）。既定値は §2 の表と同じ割り当てで、JSON ファイルで上書きできる（未知のキーはエラー）。読むファイルは `--llm-config`（`MAM_LLM_CONFIG`）で指定したもの、指定が無ければデータディレクトリの `llm.json`（無ければ既定値）。全体の設定画面（`GET /settings`）の「LLM の割り当て」は、14 Agent の `primary` / `fallback` を `llm.json` の `routes` に保存する（`internal/service/llmassign`）: 既定と同じ割り当ては書かず（のちに既定が変わっても追従する）、`providers`・`retry` など `routes` 以外のキーと手書きの他の Agent の経路はそのまま残し、全体を `llm.ParseConfig`（起動時の `llm.LoadConfig` と同じ検証）に通して通らなければ 422 で何も書かない。起動オプションで指定したファイルは書き換えず読み取り専用で表示する。起動時に 1 回だけ読むため、保存・「既定に戻す」の反映にはアプリの再起動が必要（画面は実行中の割り当てではなく保存済みの割り当てを示す）。モデル名は設定値であり Agent のコードには書かない
- API キーは `credential.Store` のプロバイダー名（`anthropic` / `openai` / `google`）で呼び出しごとに読む。設定画面で保存したキーは再起動なしで有効になる
- プロバイダー `chatgpt-subscription`（`llm.Providers()` の 4 つ目）は API キーではなく「ChatGPT でログイン」（Sign in with ChatGPT、公式の ChatGPT plan usage）のアクセストークンで、公式の Responses API（`POST https://api.openai.com/v1/responses`、`store:false`・`stream:true`・`instructions`・`max_output_tokens` 等は送らない）を呼ぶ（`internal/agent/llm/chatgptsub`）。成功は `response.completed` だけ。プランの利用上限（`subscription_sharing_usage_limit_exceeded`、429）など待っても戻らない失敗（`APIError.Permanent`）は再試行せず・ブレーカーに数えず、直ちに代替へ回して理由と `https://chatgpt.com/settings/usage` のリンクをチャットに出す（`llm.WithFallbackNotice`）。5xx・503 の利用量取得失敗は §2.1 の指数バックオフで再試行する。JSON は `text.format` ではなく `instructions` で求め、上の検証で受ける。モデル名は `GET https://api.openai.com/v1/models` で選ぶ（固定しない）。詳細は `spec/architecture/chatgpt-signin.md` §5.1
- プロバイダー `chatgpt-codex`（`llm.Providers()` の 5 つ目。**既定の `routes` には入れない**。設定画面の「LLM の割り当て」で利用者が選ぶ。`internal/agent/llm/chatgptcodex`）は、Business / Enterprise / Edu のワークスペースのアカウントなど、公式の `chatgpt-subscription` が使えないアカウント向けの**テキスト生成だけ**の経路で、「ChatGPT でログイン」の Codex 経由のログインの Codex app-server（OpenAI 公式。標準入出力の JSON-RPC）に `turn/start` で頼む。`base_url` は `stdio://codex-app-server`（接続先は無い）。API キーも mam が読むトークンも使わない。画像・動画・音声には使わない（画像入力を送るのは、対応するモデルのときだけ）。Codex の道具（シェル・ファイル編集・Web 検索など）は設定と読み取り専用の sandbox で切り、道具を使い始めたターンは中断して失敗にする。失敗の分け方は `chatgpt-subscription` と同じ: 利用上限・未ログイン・認可の失敗・エンジンが無い・未対応の機能は `APIError.Permanent`（再試行せず・ブレーカーに数えず代替へ回し、理由と日本時間のリセット時刻、`https://chatgpt.com/settings/usage` をチャットに出す）、通信の一時的な失敗は 503 で再試行する。同時実行数の上限は設けない（`chatgpt-subscription` と同じ）。設計は `chatgpt-codex/codex-login.md`。
- 再試行: タイムアウト・ネットワーク断・429・5xx を指数バックオフで再試行する。`Retry-After` があれば、待ち時間は現在のバックオフ以上にし（バックオフより短い指示には従わず、長い指示を優先する）、`max_delay_ms`（既定 16 秒）で頭打ちにする（ベンダーが長い待ちを求めても呼び出しを長時間止めない）。認証エラー等の 4xx は再試行しない
- サーキットブレーカー: プロバイダー単位。連続失敗が `failure_threshold` に達すると OPEN になり、`open_seconds` 経過後に 1 回だけ試行（HALF-OPEN）して成功で CLOSED に戻る。OPEN のプロバイダーは呼ばず、代替へ自動でルーティングする（§7）
- 出力は構造化 JSON を要求し、コードフェンスや前置きを除いて検証する。検証に失敗した回答は保存せず、チャットにエラーを通知して再開できる状態に留める。出力は 1 回の呼び出しあたり最大 8192 トークン（`Request.MaxTokens` が 0 のときに必ず適用される固定値 `DefaultMaxTokens`。Anthropic の `max_tokens`・OpenAI の `max_completion_tokens`・Gemini の `maxOutputTokens` に渡す。現状 `MaxTokens` を指定する呼び出し元は無く、全 Agent に掛かる。設定（`--llm-config`）では変えられない）で、長い回答が途中で切れると上の検証に失敗する。プロバイダー応答の読み取りは 8 MiB までで、それ以上は読まない

## 9. 画像・動画生成 Model Adapter の実装（`internal/agent/modeladapter`、`internal/agent/generation`）

M1 Issue #7（F-06/F-07/F-11/F-12）の実装仕様。Reasoning LLM Adapter（§2.1）とは別パッケージで、耐障害部品（サーキットブレーカー・指数バックオフ・同時実行数の上限 `Limiter`）は `internal/agent/resilience` を両者が共有する。ローカル・代替モデルは `modeladapter/` の下位パッケージ（`localcfg`・`localhttp`・`comfyui`・`openaicompat`・`localbackends`）に置く。

### 9.1 M1 で実装するプロバイダー

- **画像**: §3 の 4 モデル（Nano Banana / Nano Banana Lite / FLUX.2 / GPT Image 2）のアダプターと、用途→プライマリ/代替の設定（§3 の表どおり。`new_reference` / `edit_reference` / `preview` / `unified_openai`）を実装する。Nano Banana と Nano Banana Lite は同じ Gemini の `models/<モデル ID>:generateContent`（`responseModalities` は `IMAGE`、`imageConfig.aspectRatio`、`imageSize` は送らず既定の 1K）を呼ぶ。Reference Pack の Character/Product/Location は新規生成として `new_reference`（アップロード素材や World State の参照画像がある場合は参照ベース編集として `edit_reference`）、Style/Lighting/Keyframe は `edit_reference` を使う。`prefer_openai` を有効にすると新規参照を `unified_openai` へ回す。`preview`（FLUX.2 による高速プレビュー）は、現状どの機能からも呼ばれない予約ルートである（FLUX.2 を使うには `images.new_reference` などの `primary` に FLUX.2 を指定する）
- **動画**: Kling 3.0 / Seedance 2.0（M1）と Runway Gen-4.5（M2）の 3 アダプタ（§4）。Runway Gen-4.5 は `POST /v1/text_to_video`（Shot に先頭フレームがなければ）または `POST /v1/image_to_video`（先頭フレームのみ）を非同期タスクとして起動して `GET /v1/tasks/:id` をポーリングし、`X-Runway-Version` ヘッダーを付ける。出力は 720p のみ、長さは 2〜10 秒、ネガティブプロンプトはなく（Avoid は本文に含める）プロンプトは 1000 文字まで、自由な参照画像は取れない。縦横比は `text_to_video` が 16:9 / 9:16 だけ、`image_to_video` は 1:1 も取れる。そのため Brief の縦横比が 1:1 で Shot に先頭フレーム（Keyframe）がないときは、リクエストを送らず 422 相当の `ProviderError` で失敗し、§9.3 のフェイルオーバーで代替モデルに回る（MAX QUALITY では Kling 3.0）。1:1 の Shot で Runway を主に使うには先頭フレームを用意する。価格はクレジット（12 クレジット/秒、1 クレジット = 0.01 USD）から USD に換算して見積る
- **追加の動画モデル（Issue #872・#873・#874・#875・#876・#878）**: Gemini Omni Flash・Kling 4.0・Seedance 2.5・MiniMax H3 / H3 Max・Wan 3.0・Luma Ray 3.2 は、既定の割り当てには入れずに識別子で呼べる動画モデルとして足した。API・制約・単価・未検証の点は §9.4
- **`model_name` / `provider`**: `take.model_name`・`cost_ledger_entry.provider` は §6 の正規識別子で保存する。API のモデル ID（`gemini-nano-banana-2.1` など。既定は §9.2 の表）は設定値で、保存値には使わない。`gpt-image-2` の既定 API モデル ID は `gpt-image-2`（`gpt-image-1` は 2026-10-23 に終了した）。リクエストは OpenAI の公式仕様（`openai-openapi` の `CreateImageRequest` / `CreateImageEditRequest`）に合わせる: 生成は `POST /v1/images/generations` の JSON（`model`・`prompt`・`size`・`quality`・`n`）、参照画像つきは `POST /v1/images/edits` の multipart（`model`・`prompt`・`size`・`quality`・`image[]`）。`gpt-image-2` では `input_fidelity`（常に高精細で処理され、API が変更を許さない）と `response_format`（GPT Image は常に base64 で返す）を送らない。`quality` は `auto` にせず、**品質プロファイル**（`models.gpt-image-2.quality`: `low` / `medium` / `high`。既定 `medium`。§9.2）を毎回明示して送る（`auto` ではモデルが品質を選び、1 枚の価格を前もって決められないため）。サイズは Brief の縦横比から `1536x1024` / `1024x1536` / `1024x1024`（16 の倍数・比 3:1 以内・画素数の範囲を満たす）に決める
- **ローカル・代替モデル（Issue #831）**: 登録した ComfyUI（画像・動画。`POST /prompt` と `/ws` の進捗、取り消しは自分の `prompt_id` だけ）と OpenAI 互換（画像のみ。`/v1/images/generations`・`/v1/images/edits`）の接続先のモデルを、組み込みと同じ用途・Quality Policy の割り当て、フェイルオーバー、ブレーカー（接続先ごとに `endpoint:<名>`）、同時実行数（既定 1）に載せる。識別子は §6 のとおり。保存した接続先・モデルは再起動なしで反映する。詳細は [local-models.md](local-models.md)
- **ChatGPT のサブスク経由の画像（非公式、`internal/agent/modeladapter/chatgptimage`）**: `gpt-image-2:chatgpt-subscription`。Codex のエンドポイントの `image_generation` ツールを、画像専用のログイン（Codex の公開クライアント）のトークンで呼ぶ。参照画像は `input_image` で渡せる。**既定 OFF**で、OFF のあいだは外部への呼び出しを一切しない。ON にするには設定画面でリスクを確認する。選択肢（設定画面・MCP）に出るのは ON のあいだだけで、すでに割り当ててあって OFF になった場合は呼ばずに代替モデルへ回る。専用のベンダー `chatgpt-subscription`（ブレーカー・同時実行数 1）を持つ。呼び出しの失敗は §9.3 と同じ再試行・代替の対象で、無音が 180 秒続くと中止する。Codex のドライバーのモデルは固定せず、アカウントが使える Codex のモデル一覧から自動で選び、「このモデルは使えない」と答えられたら次のモデルを試す（全部拒否されたら、画面には対処つきの文面を出す。Issue #907）。設計は `spec/architecture/chatgpt-signin.md` §5.2

### 9.2 設定

`--model-config`（`MAM_MODEL_CONFIG`）の JSON ファイルで既定値を部分的に上書きできる（未知のキーはエラー）。指定しないときは、データディレクトリの `models.json` があれば起動時に同じ規則で読む。全体の設定画面（`GET /settings`）の「画像・動画モデル」は、この `models.json` の `images`（用途ごとの主・代替モデルと `fast`）と `policies`（Quality Policy ごとの主・代替モデル・解像度・`takes`）だけに、既定と違う割り当てを保存する（既定と同じものは書かず、ほかのキーは残し、書く前に起動時と同じ検査を通して通らなければ 422 で何も書かない。選べるのは、画像は画像バックエンドのあるモデル、動画は `Capabilities` が対応する解像度との組み合わせ。保存した割り当ては次の起動から効く）。`--model-config` で指定したファイルは読み取り専用で、画面から書かない。`fast` を選べるのは `models.<識別子>.fast_model` を設定したモデルだけで、既定では組み込みのどのモデルにも `fast_model` はない。参照画像を使えない画像モデル（`AcceptsImageInputs` が偽。組み込みにはなく、`accepts_image_inputs` が偽の登録モデルだけ）で生成するときは、主モデルでも代替モデルでも参照画像を渡さず Prompt だけで作る（`Adapter.GenerateImage`。ログに警告が出るだけで、呼び出し側へのエラーにはならない）。

| キー | 内容 | 既定 |
|---|---|---|
| `vendors.<google\|openai\|flux\|kling\|kling4\|seedance\|runway\|minimax\|alibaba\|luma>` | `base_url` / `max_concurrency`（同時実行の上限） / `timeout_seconds` | 4 / 4 / 4 / 3 / 3 / 3 / 2 / 3 / 3 / 3、180〜1800 秒（`google` は動画（Gemini Omni Flash）の生成を待つため 600 秒。画像の呼び出しにも同じ値が掛かる。`alibaba` は Wan 3.0 の生成が公式の例で 12 分かかっているため 1800 秒）。`kling` は `https://api.klingai.com`、`kling4` は `https://api-singapore.klingai.com`、`minimax` は `https://api.minimax.io`、`luma` は `https://agents.lumalabs.ai`、`alibaba` は `https://{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com`（`{WorkspaceId}` は保存したキーの先頭の値に置き換える。別のリージョンを使うときの設定は §9.4 の Wan 3.0） |
| `models.<正規識別子>` | `model`（API のモデル ID）、`fast_model`、`cost_per_image_usd`、`cost_per_second_usd`（解像度別）、`quality`（`gpt-image-2` だけ。`low` / `medium` / `high`。品質プロファイル。下の「GPT Image 2 の品質プロファイルと価格」）。価格は参考値（§8）で見積コストにのみ使う（GPT Image 2 の `actual_cost` は応答の `usage` から計算する） | §3 / §4 の割り当て。`quality` は `medium` |
| `images.<purpose>` | `primary` / `fallback`（`model` と `fast`） | §3 の表 |
| `policies.<fast\|balanced\|max_quality>` | `primary` / `fallback`（`model` と `resolution`）、`takes`（Take 数） | FAST: Kling 3.0 720p → Seedance 2.0 480p、2 Take。BALANCED: Seedance 2.0 720p → Kling 3.0 720p、3 Take。MAX QUALITY: Runway Gen-4.5 720p → Kling 3.0 1080p、4 Take |
| `endpoints.<名>` / `local_models.<接続先>/<名>` | 登録した接続先（`kind`・`base_url`・`max_concurrency`・`timeout_seconds`）とそのモデル（ワークフロー・割り当て表・動画の長さと解像度・価格）。`images`・`policies` の `model` には `<kind>:<接続先>/<名>` を書ける。接続先の API キーは `credential.Store` の `endpoint:<名>`（任意）に置く。スキーマは [local-models.md](local-models.md) §3 | 空（価格 0、同時実行 1、タイムアウト ComfyUI 3600 秒・互換 900 秒） |
| `prefer_openai` | トップレベルの真偽値。`true` で新規参照（`new_reference`）を `unified_openai` へ回す（§3） | `false` |
| `retry` / `breaker` | §2.1 と同じ形式・同じ既定値 | 3 回 / 1000ms〜16000ms、5 回失敗で 30 秒 OPEN |

API キーは `credential.Store` のベンダー名（`google` / `openai` / `flux` / `kling` / `kling4` / `seedance` / `runway` / `minimax` / `alibaba` / `luma`）で呼び出しごとに読む。`kling`（Kling 3.0）のキーは `ACCESS_KEY:SECRET_KEY` の形式、`kling4`（Kling 4.0）のキーは Kling の API キー 1 つで保存する（設定画面の「Kling」と「Kling 4.0」）。`minimax`（MiniMax H3 / H3 Max）のキーは MiniMax の API キー 1 つで、設定画面の「MiniMax（H3）」から保存・削除する（ほかのベンダーと同じ `credential.Store`）。`alibaba`（Wan 3.0）のキーは `ワークスペース ID:API キー` の形式で、設定画面の「Alibaba Cloud（Wan 3.0）」から保存・削除する（理由は §9.4 の Wan 3.0）。`luma`（Luma Ray 3.2）のキーは Luma Agents API の API キー 1 つで、設定画面の「Luma（Ray 3.2）」から保存・削除する。Kling の本文と公式ドキュメントとの突き合わせ、旧経路が `kling-v3` を受け付けるかの未検証は [kling-api.md](kling/kling-api.md)。

既定の API モデル ID と見積価格（`cost_per_image_usd`。2026-10-25 に公式の料金表で確認。価格は参考値で、見積コストにのみ使う）:

| 識別子 | 既定の API モデル ID | `cost_per_image_usd` | 出どころ |
|---|---|---|---|
| `nano-banana` | `gemini-nano-banana-2.1` | 0.0336 | [Gemini API pricing](https://ai.google.dev/gemini-api/docs/pricing)「Nano Banana 2.1」Standard: 1K 画像（1120 トークン × $30 / 100 万トークン）。`imageSize` を送らない既定が 1K。2K は 0.0504、4K は 0.113 |
| `nano-banana-lite` | `gemini-3.1-flash-lite-image` | 0.0336 | 同「Gemini 3.1 Flash Lite Image」Standard: 1K 画像（1K のみ対応） |
| `flux-2` | `flux-2-pro` | 0.03 | [BFL の料金表](https://bfl.ai/pricing)「FLUX.2 [pro]」: 生成画像の最初の 1 メガピクセルが $0.03（参照画像は 1 メガピクセルごとに $0.015 加算。mam の出力サイズ 1344×768 などは 1 メガピクセル未満）。参照画像つきの呼び出しではこの値より高くなる |
| `gpt-image-2` | `gpt-image-2` | 未設定。`quality` とサイズで決まる（既定 `medium` で 1536×1024 / 1024×1536 は 0.041、1024×1024 は 0.053。下の表）。`cost_per_image_usd` を書くとそれが優先される | [OpenAI の料金表](https://developers.openai.com/api/docs/pricing)（画像トークン: 出力 $30 / 100 万トークン、入力は画像 $8・テキスト $5）と、[画像生成ガイド](https://developers.openai.com/api/docs/guides/image-generation)の「Calculating costs」の GPT Image 2 の表（2026-10-25 に取得）。下の「GPT Image 2 の品質プロファイルと価格」 |

#### Kling 3.0・Seedance 2.0 の見積単価（`cost_per_second_usd`。Issue #880）

見積（`estimate`）・Cost Ledger の `estimated_cost` は 秒数 × この単価。公式の料金ページを 2026-11-01 に取得した値で、価格は参考値（見積コストにのみ使う）。

| 識別子 | 解像度 | `cost_per_second_usd` | 出どころと計算式 |
|---|---|---|---|
| `kling-3.0` | 720p / 1080p | 0.084 / 0.112 | [Kling API Pricing](https://kling.ai/dev/pricing)「Kling 3.0」の **No Native Audio**（映像入力なし、秒課金）: 720P 0.6 units/秒、1080P 0.8 units/秒。1 unit = 0.14 USD（同ページの General の 180-Day Package「Standard Package 1・2」の list price「$0.14 per unit」）。0.6 × 0.14 = 0.084、0.8 × 0.14 = 0.112 |
| `seedance-2.0` | 480p / 720p / 1080p | 0.07 / 0.15 / 0.37 | [BytePlus ModelArk の Model pricing](https://docs.byteplus.com/en/docs/modelark/model-pricing)「Video generation models」の「Dreamina Seedance 2.0 series」の **Input without video**（既定のタブ）、Dreamina Seedance 2.0、16:9・出力 5 秒の例の「per second」: 480p 0.07（$0.35 / 動画）、720p 0.15（$0.76 / 動画）、1080p 0.37（$1.87 / 動画）。4K は 0.78 だが mam は 4K を使わない。値は同ページが載せる「per second」の表示を採る |

- **Kling 3.0 の `sound` と音声なしの選択**: `klingBackend` は `sound` を送らない。mam が使う旧 API（`POST /v1/videos/text2video`・`image2video`）の `sound` は、公式（[Text to Video（legacy）](https://kling.ai/document-api/api/video/3-0-omni/text-to-video/legacy)、2026-11-01 に取得）が「Default to off」とするので、送らない呼び出しは音声なしで、料金表の「No Native Audio」の行に当たる。音声ありの行（720P 0.9 / 1080P 1.2 units/秒 = 0.126 / 0.168 USD/秒）は使わない。`sound: on` を送るようにしたら、この単価も音声ありの値へ変える。`mode` は `std`（720p）/ `pro`（1080p）で、料金表の 720P / 1080P に対応する
- **Seedance 2.0 の単価の条件**: 映像入力なし（mam は動画を入力に使わない）。映像入力があると同ページの「Input with video」の別の単価（最低トークン数つき）になる
- **未確認**: 両ページとも実際の請求との照合はしていない（API キーが要る）。Kling の料金表は新 API の見出し（Kling 3.0）の価格で、旧 API（`kling-v3`）に同じ価格が適用されるかは、ページに明記が無いので未確認

#### GPT Image 2 の品質プロファイルと価格（Issue #843）

`gpt-image-2` の 1 枚の価格は画像出力トークン数（`quality` とサイズで変わる）で決まる。`quality: auto` ではモデルが品質を選び、見積を前もって決められないため、mam は**品質プロファイルを設定として選べる**ようにし、選んだ品質を**毎回明示して**送る（`POST /v1/images/generations` の `quality`、`POST /v1/images/edits` の `quality` フィールド。`auto` は設定できない）。プロファイルは `models.json` の `models.gpt-image-2.quality`（`low` / `medium` / `high`。未知の値と `gpt-image-2` 以外のモデルへの指定は起動時の検査で拒否する）。書かない既存の設定ファイルは既定のプロファイルになる。この項目は設定画面にはまだ出していない（設定画面の「画像・動画モデル」は §9.2 冒頭のとおり `images` と `policies` だけを書く）。今は `models.json` を直接書いて選ぶ。

- **既定は `medium`**: 参照画像（Character / Product / Location / Style）の質を落とさず、`high` の約 1/4 の価格（1536×1024 で $0.041 対 $0.165）に収める中間である。`low`（$0.005）は下書き向けで、参照画像の一貫性に響くおそれがあり、`high` は予算の消費が大きい。Issue #843 の例示が `medium` で、ほかに既定を示す記述は無い。送る品質と見積が同じプロファイルから出るため、見積が実際の請求と食い違う主因（`auto` でモデルが品質を選ぶこと）が無くなり、予算の確認が 0 や代替モデルの価格に落ちない（出力トークン分の見積。入力分は下の未検証①）
- **見積（`estimate`）= プロファイルの 1 枚の価格（サイズ別）**: Cost Ledger の `estimated_cost` と生成テストの単価は、生成する縦横比のサイズの価格。縦横比を指定しない呼び出しの `estimated_cost` は最も高いサイズ（1024×1024）の価格で、実際に送るサイズ（既定の 16:9 → 1536×1024）より多めになる。予算の確認（`Adapter.EstimateImage`）は用途の主・代替のうち高いほうで、サイズは最も高いもの（1024×1024）を使う。Brief の縦横比と OpenAI のサイズの対応は `aspectsize.OpenAI`（16:9 → 1536×1024、9:16 → 1024×1536、1:1 → 1024×1024）
- **実コスト（`actual_cost`）**: 応答の `usage`（`input_tokens_details` のテキスト・画像の入力トークンと `output_tokens`）に、下の単価（テキスト入力 $5、画像入力 $8、出力 $30、いずれも 100 万トークンあたり）を掛けて計算し、Cost Ledger の `actual_cost` に記録する。`usage` は画像本体とは別に読み、読めない形（数が文字列・小数・整数に収まらない値など）でも画像は返し、`actual_cost` は空にする（画像の取得に課金済みなので、`usage` の不備で再試行や代替への切り替えをしない）。次のときは `actual_cost` が空（NULL）で、見積が集計に使われる: `usage` が無い・読めない応答、`gpt-image-2`（日付つきのスナップショットを含む）以外の API モデル ID（価格が表に無い）、ありえない数を含む `usage`（どれかのトークン数が負、または上限を超える。上限は出力 100,000、入力（合計・テキスト・画像のそれぞれ）200,000 で、mam が送る最大の画像（`high`・1024×1024）の出力約 7,000 トークンの十数倍。壊れた・悪意ある応答が巨額の実コストを予算の合計に入れるのを防ぐ）
- **API モデル ID を別のものにした場合**: 下の表は `gpt-image-2` の値で、ほかのモデルの価格は推測しない。`gpt-image-2` 以外の API モデル ID に変えたときは、`cost_per_image_usd` を書かない限り見積は 0（価格不明）になる。この組み合わせは `configcheck.Load` が起動時（設定ファイルの読み込み時）に警告ログで知らせる（拒否はしない。価格を自分で決めて使う運用は許す）

| `quality` | 1024×1024 | 1024×1536（9:16） | 1536×1024（16:9） |
|---|---|---|---|
| `low` | $0.006 | $0.005 | $0.005 |
| `medium`（既定） | $0.053 | $0.041 | $0.041 |
| `high` | $0.211 | $0.165 | $0.165 |

出どころ: OpenAI「Image generation」ガイドの Cost and latency「Calculating costs」の GPT Image 2 の表（<https://developers.openai.com/api/docs/guides/image-generation>、2026-10-25 に取得）。この表は画像の**出力トークンの価格のみ**で、入力（プロンプトのテキストトークン、参照画像つきの編集では画像の入力トークン）は含まない。トークン単価は料金表の「Image generation models」Standard の `gpt-image-2`（<https://developers.openai.com/api/docs/pricing>、2026-10-25 に取得）: 画像 入力 $8.00 / 出力 $30.00 / キャッシュ入力 $2.00、テキスト 入力 $5.00 / キャッシュ入力 $1.25（100 万トークンあたり）。モデルのページ（<https://developers.openai.com/api/docs/models/gpt-image-2>）にはサイズ別の価格は無い。mam が送る 3 つのサイズは表にすべてあり、補間や推測した値は無い。

未検証: ① 見積は出力のみで、入力トークンの分（参照画像つきの `/v1/images/edits` では画像入力 $8 / 100 万トークンが加わる）を含まない。実コストは `usage` から入力も含めて計算するので、編集では実コストが見積を超えることがありうる。つまり**編集の予算の確認は実際の支出より緩い**。見積に入力の余裕を足さないのは、公式の資料に `gpt-image-2` の入力画像 1 枚あたりのトークン数の根拠（式や表）が無く、上限を決められないため（2026-10-25 に確認: ビジョンのガイドの画像トークンの計算（パッチ方式）は「GPT Image の生成・編集には使えない」と書き、入力トークンの規則を載せるのは 2026-10-23 に終了した `gpt-image-1` だけ。`gpt-image-2` は入力画像をすべて高忠実度で処理するのでトークンが多くなりうる、とだけ書く）。推測の値を足すとかえって根拠のない上限になる。また、編集の応答に `usage` が無いと、Cost Ledger には出力のみの見積が残り、編集の支出を少なく数える。公式が数式を示すか、実アカウントで入力トークンを測れたら、見積に入力分を足す。② 実際の `gpt-image-2` の Images API 応答に `usage`（`input_tokens` / `output_tokens` / `input_tokens_details`）が付くか、`quality` を明示したときの請求が上の表と一致するかは、実アカウントでの呼び出しで確かめていない（公式の OpenAPI は `usage` を `gpt-image-1` のみと書き、ガイドは「応答の `usage` でトークン消費を測る」と書く。テストは `usage` つきの偽応答で計算だけを確かめる）。③ 料金表は 2026-10-25 時点の値で、半期ごとの見直し（§8）で取り直す。

### 9.3 動作

- **Canonical Shot Spec → モデル固有プロンプト（F-07）**: `Prepare` が Shot Spec を英語のプロンプトへ変換する。モデルごとに対応する長さ（Kling 3.0 は 5/10 秒、Seedance 2.0 は 4〜12 秒、Kling 4.0 は 3〜30 秒、Seedance 2.5 は 4〜30 秒、MiniMax H3 は 4〜15 秒・H3 Max は 5〜15 秒、Wan 3.0 は 2〜30 秒、Luma Ray 3.2 は 5/10 秒（フレームがあるときは 5 秒に固定。10 秒はフレームと一緒に送れない）。要求を満たす最短の長さ）、First/Last Frame、参照画像の上限（Seedance 2.0 は 9 枚、Seedance 2.5 は 30 枚、MiniMax H3 は 5 枚・H3 Max は 2 枚、Wan 3.0 は 10 枚、Luma Ray 3.2 は無し）に合わせて入力を絞る。First Frame と自由な参照画像は同じリクエストに入れない（Seedance は両方を別のシナリオとして拒否する。2.5・MiniMax・Wan 3.0 も同じ。First Frame を優先し、Reference Reinforcement のときだけ参照を優先する）。Negative Prompt を持たないモデルでは本文へ折り込む。プロンプトは英語のみ（`non-functional.md` §9）。プロンプトの長さの上限（文字数は rune）は、Kling 3.0 が本文 2400 文字・ネガティブプロンプト 400 文字（Avoid の項目を `, ` で連結して 400 文字で切る）、Runway Gen-4.5 が 1000 文字、Kling 4.0 が 26000 文字（公式の英語だけの入力の上限。ネガティブプロンプトの欄は無く Avoid は本文へ折り込む）、MiniMax H3 / H3 Max が 7000 文字（公式の `text` 1 つの上限。ネガティブプロンプトの欄は無く Avoid は本文へ折り込む）、Luma Ray 3.2 が 6000 文字（公式の `prompt` の 1〜6,000 文字。ネガティブプロンプトの欄は無く Avoid は本文へ折り込む）、Wan 3.0 が 20000 文字（公式の `prompt` の上限。超えた分は API が黙って切り捨てるので、Constraints と Avoid 文を守るために mam が先に収める。ネガティブプロンプトの欄は無く Avoid は本文へ折り込む）、ネガティブプロンプトを持たないその他のモデル（Seedance 2.0・2.5・Gemini Omni Flash など）が 2400 文字。超えるときは、末尾の Constraints 文（ネガティブプロンプトを持たないモデルでは続く「Avoid:」文も）を丸ごと残し、Shot の描写（本文）の末尾側を削って収める（LOCK 由来の制約・ブランド制約を生成に届けるため）。末尾の文だけで上限を超えるときは、その末尾の文を上限で切る
- **フェイルオーバー（§7）**: ベンダー単位のブレーカーが OPEN、または再試行しても失敗したとき、同じ Shot Spec を代替モデル向けに再変換して呼ぶ。429・5xx・ネットワーク断のみ同じモデルで再試行し、認証エラー等の 4xx は再試行しない（待ち時間は §2.1 の再試行と同じく、`Retry-After` をバックオフの下限として加味し `max_delay_ms` で頭打ちにする）。タイムアウトは同じモデルでは再試行せず、直ちに代替モデルへフェイルオーバーする（ベンダー側でジョブが継続・課金されている可能性があり、再提出すると二重に課金されるため。LLM の §2.1 は課金を伴う非同期ジョブを持たないためタイムアウトも再試行する）。タイムアウトもブレーカーの失敗に数える。ベンダーがジョブを受け付けた後（状態確認・結果のダウンロード）の 429・5xx・ネットワーク断は、同じジョブ ID への GET だけを再試行する。出力は課金済みで、短い障害で失うと取り戻せないため、回数の上限は置かず、呼び出し全体のタイムアウトまで続ける（待ち時間は状態確認の間隔（既定 5 秒）から倍々に増やして 1 分で頭打ちにし、`Retry-After` がそれより長ければ従う。ただし 1 分を超えない）。タイムアウトに達してもなお失敗していたときは、ジョブを再提出せず（二重課金回避）代替モデルへ進む。Sync Labs（`model-adapters-m3.md` §14.1）のポーリング・ダウンロードも同じ規則で、`Retry-After` と呼び出し全体のタイムアウト（Lip Sync の `timeout`）まで再試行する。ただし結果ファイルの保存先（ステージングディレクトリ）の作成・書き込み・同期の失敗（ディスクフル・権限など、ベンダーではなくこのマシンの問題）は再試行せず、ベンダーの一時障害としても数えない（ダウンロードを期限まで繰り返さず、ブレーカーも開かない）
- **ベンダー応答の URL の扱い**: API キーを付けるリクエストは、ベンダーのベース URL と同じスキーム・ポートで、同じホスト（FLUX だけは、公式がリージョンのホストで答えるため、ベースホストの親ドメイン配下の例 `api.bfl.ai` → `api.eu.bfl.ai` も）の URL にだけ送る。親ドメインの規則を使うのは FLUX だけで、`googleapis.com` のように親ドメインを他者と共有するベンダー（Google）には使わない（`storage.googleapis.com` のバケットなどへキーを送らないため）。FLUX の `polling_url` がそれ以外を指すときは、キーを送らずエラーにする。API キーを付けるダウンロード（Gemini Omni Flash）は、ダウンロード先をベース URL から組み立て、リダイレクトされたときはベンダーのホスト以外ではキーのヘッダーを外す（§9.4）。生成結果のダウンロード（Sync Labs の `outputUrl` を含む。登録した接続先の例外は次の項目）は https の URL だけを受け付け、公開インターネットのアドレス以外へは接続しない（許可リスト型。`localhost`、ループバック、リンクローカル（クラウドのメタデータサービス `169.254.169.254` など）、プライベート、未指定、マルチキャストに加え、IANA の特殊用途範囲 — キャリアグレード NAT `100.64.0.0/10`・`0.0.0.0/8`・ドキュメント用・ベンチマーク用・予約済みクラス E・6to4 リレー anycast・Teredo など — は全て拒否する。IPv4 互換・6to4・NAT64 の IPv6 アドレスは埋め込まれた IPv4 アドレスで判定する。ホスト名の解決後のアドレスも検査する。リダイレクト先も同じ規則。プロキシ設定は使わない）。ローカルの常駐アプリがベンダー応答を経由して内部ネットワークを読み出さないための制限である。ベンダー API を呼ぶ既定の HTTP クライアント（LLM・Model Adapter・Audio Director・Jev 共通、`internal/httpclient`）は、別ホストへのリダイレクトと https から http へのリダイレクトを拒否し（独自ヘッダーの API キーを別ホストへ転送しない）、接続・TLS ハンドシェイク・レスポンスヘッダーの待ちにタイムアウトを持つ。ベンダーの JSON 応答は 64 MiB まで読み、超えると失敗扱いにする（画像は base64 で JSON に入って返るため）。生成動画のダウンロード（上限 300 MB）は本文をメモリに載せずアセットディレクトリ配下の一時ファイル（`<assets>/.staging/`）へストリーム書き込みし、保存時にアセットのパスへリネームする（失敗・サイズ超過時は一時ファイルを削除し、異常終了で残った分は起動時に掃除する）。画像は小さいためメモリ上で扱う
- **登録した接続先（ローカルモデル）の URL の扱い**: 組み込みベンダーの規則をゆるめるのは、登録した接続先の `base_url` と**同じスキーム・ホスト・ポート**の URL に対してだけで、http・`localhost`・プライベート・`100.64.0.0/10` へ接続できる。それ以外の URL（第三者ベンダーの応答 URL を含む）は上の規則のまま拒否する。ホスト違いと https から http へのリダイレクトは拒否し、API キーは接続先の `base_url` 配下にだけ付ける。**リンクローカル（`169.254.0.0/16`、`fe80::/10`）は登録した接続先でも常に拒否**し、判断は接続時に解決した IP（ダイヤラの `Control`）で行う（DNS リバインディング対策。`local-models.md` §6）
- **同時実行数（`non-functional.md` §4）**: ベンダーごとの上限を Model Adapter が持つ。429 を受けると上限を半分（最小 1）にし、成功が上限回数続くごとに 1 ずつ戻す。上限を超える呼び出しはスロットが空くまで待つ。Prompt Candidate Generation（F-08）が Candidate を並列に生成しても、外部 API への同時呼び出し数はこの上限（Runway は既定 2）で抑えられる
- **Generation Orchestrator（F-12）**: Quality Policy でモデル・解像度・Take 数を決め、(1) Shot が主モデルの最長クリップを超え代替モデルなら収まる場合は入れ替え、(2) Shot/Project の予算上限（残額）に Take 数が収まらなければ Take 数を減らし、主モデルが 1 Take も収まらず代替なら収まるなら代替を主にし、(3) 1 Take も収まらなければ 402 で開始しない。決定理由は `prompt_version.model_adapter_output`（`reasons`）に残る。Take は並列に生成し、1 つ以上成功すれば成功とする
- **Prompt Autopilot（F-06）**: Prompt Planner（Reasoning LLM）が Shot の指示と World State・直前 Shot の Spec・Reference Pack から Canonical Shot Spec（JSON、英語）を作る。検証（目的・フレーミングあり、Acting が Shot 内で時系列順、日本語などを含まない）に失敗した回答は保存しない。既存 Spec は、追加指示がある場合だけ改訂し、無ければ再利用する。生成のたびに `prompt_version` が 1 行増え、`shot_spec_snapshot` に保存時点の Spec、`model_adapter_output` に各モデルへ渡す内容（プロンプト・解像度・参照枚数）を記録する
- **Reference Pack（F-11）**: Art Director（Reasoning LLM）が Character/Product/Location/Style/Lighting/Keyframe の各画像のプロンプトを決め、画像 Model Adapter が生成する。Character/Product/Location/Style/Lighting を先に並列生成し、Keyframe は先行する 3 種を入力にして最後に生成する。全画像が揃ったときだけ `shot.shot_spec.references` を置き換え（途中で失敗したら旧 Pack を残し、作成済みファイルも削除する）。Keyframe は動画生成の First Frame になる。画像モデルへ入力として渡す参照画像は、Brief に添付したアップロード素材も、World State の参照（Character の顔・体、Product の形・ラベル、Location のレイアウト）も、種類ごとに読み込めた先頭の 4 枚までで、5 枚目以降は通知なしで使われない。1 枚 20 MiB を超える保存済み画像は入力にせず、警告ログを残すだけで除外する（AI Video Judge に渡す参照画像も同じ 20 MiB で、超えるものは黙って渡さない）
- **ジョブ**: 生成・Reference Pack 作成は `job.kind = generate` の非再開ジョブ（課金を伴うため、異常終了後と、シャットダウン（Ctrl+C など）で実行中に中断された場合は、キューに戻さず失敗として扱い、Shot を PLANNING へ戻す）。開始時に Shot を GENERATING にし、同じ Shot の二重実行は 409 とする。進捗は `/ws/shots/:id`、チャットの進捗カードと Take/Keyframe カードで通知する

### 9.4 追加の動画モデル（Gemini Omni Flash・Kling 4.0・Seedance 2.5・MiniMax H3 / H3 Max・Wan 3.0・Luma Ray 3.2）

§4 の「追加の動画モデル」の API・制約・単価。どれも既定の割り当てには入れず、識別子で呼べる。単価は公式の料金ページで確認できたものだけを `cost_per_second_usd` に入れる（価格は参考値で、見積コストにのみ使う。§8）。公式に秒あたりの価格が無い解像度は `Capabilities` の解像度から外して提供しない。

#### Gemini Omni Flash（`gemini-omni-flash`、Issue #872）

- **API**: Gemini の Interactions API。ベンダーは `google`（画像と同じ API キー・ベース URL・同時実行・ブレーカー）。`POST {base_url}/v1beta/interactions`（`x-goog-api-key`）。本文は `model`（既定 `gemini-omni-1.1-flash`。Stable。Preview の `gemini-omni-flash-preview` と Veo 3.1 Preview の識別子は終了済みなので入れない）・`input`（画像 `{type: image, mime_type, data}`（Base64）を先に、最後に `{type: text}`）・`response_format`（`type: video`・`aspect_ratio`・`resolution`・`delivery: uri`）・`generation_config.video_config.task`
- **入力**: テキストだけなら `text_to_video`。First Frame があれば画像を送り、プロンプトの先頭に `<FIRST_FRAME> ` を付けて `image_to_video`。First と Last の両方があれば 2 枚を順に送り `<FIRST_FRAME> <LAST_FRAME> ` を付ける（公式の `task` の値に補間は無いので `task` は送らず、タグで指示する）。自由な参照画像は、画像を送り、プロンプトの先頭に `[# References <IMAGE_REF_0>@Image1 …]`、末尾に公式の案内文（参照画像を最初のフレームとして使わない）を付けて `reference_to_video`。フレームと参照は同時に送らない（`Prepare` が片方を残す。§9.3）。参照画像の上限は公式に記載が無く、公式の例の 6 枚を `MaxReferenceImages` にした。音声・動画の参照は使わない
- **結果**: `steps[]` の `model_output` の `video`。`delivery: uri` では `uri`（Files API のファイル）が返る。ファイルが `ACTIVE` になるまで `GET {base_url}/v1beta/files/<名前>` を状態確認の間隔でポーリングし（`FAILED` は失敗）、`GET {base_url}/v1beta/files/<名前>:download?alt=media` をダウンロードする。`uri` からは**ファイル名だけ**を取り、`uri` のホストは信用しない（状態確認もダウンロードも `base_url` から組み立てる。`uri` が別のホスト・`googleapis.com` 配下の別ホストを指しても、そこへは何も送らない）。API キーはダウンロードでも他の呼び出しと同じ `x-goog-api-key` ヘッダーで送り、URL のクエリには付けない（クエリのキーはリダイレクトのとき Go の HTTP クライアントが付ける `Referer` に入って別ホストへ漏れる）。ダウンロードがリダイレクトされたときは、リダイレクト先が `base_url` と同じスキーム・ホスト・ポートでなければキーのヘッダーを外して続ける（署名つきの URL にキーは要らない）。リダイレクト先は結果 URL の規則（§9.3。https・公開アドレス・回数の上限）も満たす必要がある。動画が `data`（インライン）で返ったときはそのまま読む。応答が動画を含まず `in_progress` などのときは `GET {base_url}/v1beta/interactions/<id>` を同じ間隔でポーリングする。`completed` なのに動画が無い（プロンプトのフィルタ）ときは空の結果の `ProviderError`、`failed` / `cancelled` / `incomplete` は 422 相当の拒否。受付後の失敗は §9.3 のとおり同じジョブへの GET だけを再試行し、再提出しない
- **長さ・解像度**: 長さを渡すリクエストの項目は無く、プロンプトで頼む（`Prepare` のプロンプトに「Duration N seconds」が入る）。対応する長さは 3〜10 秒（整数）。解像度は `720p` だけ。公式は 360p・1080p・4K（アップスケール）も載せるが、秒あたりの価格が公式に載るのは 720p（5,792 トークン/秒）だけなので提供しない
- **縦横比**: `16:9` と `9:16` だけ（公式の `aspect_ratio` の値）。1:1 は送らず、422 相当の `ProviderError` で失敗して §9.3 のフェイルオーバーで代替に回る（Runway と同じ。`direct.AspectProblem` も断る）
- **使えない項目**: ネガティブプロンプト・システム指示・温度などは公式が非対応。Avoid の項目はプロンプトへ折り込み、シードは送らない。プロンプトの上限は公式に記載が無く、折り込みモデルと同じ 2400 文字にする
- **音声**: 生成した動画にモデルのネイティブ音声が付く（既定で、切る項目は無い。`NativeAudio` は真）。mam はそれを Take のファイルにそのまま残し、Audio Director の既存のミキシングが扱う（Seedance 2.0 と同じ扱い）。ElevenLabs の音声（Audio Director）は変えない
- **単価**: 動画出力 $17.50 / 100 万トークン、720p は 5,792 トークン/秒。公式は実効「約 $0.10/秒」と書き、`cost_per_second_usd` は `720p: 0.10`（計算では 0.1014）。入力（$1.50 / 100 万トークン）は含めず、`actual_cost` は取らない（見積のみ）。出どころ: <https://ai.google.dev/gemini-api/docs/pricing>（2026-10-28 に取得）
- **同時実行・タイムアウト**: ベンダー `google` のまま（同時実行 4）。呼び出し全体のタイムアウトは画像と共有で、動画は受付から結果の取得まで待つため 120 秒から 600 秒に延ばした（画像が応答しないときの待ちも延びる）
- **未検証（実際の API キーが要る）**: ① `delivery: uri` と Files API の状態（`ACTIVE` / `FAILED`）・`:download` の形、② `generation_config.video_config.task` と `<FIRST_FRAME>`・`[# References …]` のタグの効き方、③ 参照画像の上限、④ `POST` が生成の完了まで返らないときの待ちと、`in_progress` のときの `GET /v1beta/interactions/<id>`、⑤ 実際の請求が約 0.10 USD/秒に合うか

#### Kling 4.0（`kling-4.0`、Issue #873）

- **新 API と認証**: ベース URL は `https://api-singapore.klingai.com`（公式の認証のページ。`api.klingai.com` から中国の外のサーバー向けに変わった）、認証は `Authorization: Bearer <API キー>`（キーを 1 つ。AccessKey / SecretKey は旧設計向け）。ベンダーは Kling 3.0（`kling`）と別の `kling4` で、API キー（`credential.Store` の `kling4`）・ベース URL・同時実行・ブレーカーを分ける。キー形式が `ACCESS_KEY:SECRET_KEY` だけの利用者は、Kling のコンソールで API キー（Create a new API Key）を作って「Kling 4.0」に入れる（設定画面の説明にもそう書く）
- **作成**: テキストだけなら `POST /text-to-video/kling-4.0`（本文 `prompt`・`settings`: `duration`（整数の秒）・`resolution`（`720p` / `1080p`）・`aspect_ratio`（`16:9` / `9:16` / `1:1`。公式は 21:9 も取るが mam の縦横比にない））。First Frame があれば `POST /image-to-video/kling-4.0`（本文 `contents`: `{type: prompt, text}`・`{type: first_frame, url}`・（Last Frame があれば）`{type: last_frame, url}`、`settings` は `duration`・`resolution`。縦横比は画像から決まるので送らない）。画像は Base64（`data:` の接頭辞なし）で `url` に入れる。パスのモデル版（`kling-4.0`）は `models.kling-4.0.model` で、API モデル ID として設定できる。ネガティブプロンプトの欄・シードは無く、Avoid は本文へ折り込む。中間フレーム・オムニ参照（参照 15 件）・マルチショット・編集は使わない
- **取得**: `GET /tasks?task_ids=<id>`。`data[0].status` が `submitted` / `processing`（待つ）・`succeeded`（`outputs[]` の `type: video` の `url` をダウンロード）・`failed`（`message` が理由で、422 相当の拒否）。応答の `code` は Kling 3.0 と同じ扱い（0 が成功、1302 / 1303 は 429、5000 以上は 502、そのほかは 422）。結果は 30 日で消えるので、すぐ保存する（mam は取得後すぐ保存する）
- **長さ・解像度**: 3〜30 秒の整数。`duration` を渡すと公式はそれを優先する（省かれればプロンプトの意図か 10 秒）。mam は常に渡す。解像度は `720p` / `1080p`（Capability Map は 4K も載せるが、text-to-video・image-to-video の `settings` の値は 720p / 1080p だけ）。参照画像は取らない（`MaxReferenceImages` は 0）
- **音声**: `settings` に音声の項目は無い。Capability Map の表は文字として読めた範囲ではネイティブ音声の対応を確かめられなかったので、`NativeAudio` は偽にした
- **単価**: 公式の料金ページ（<https://kling.ai/dev/pricing>、2026-10-28 に取得）の Kling 4.0（映像入力なし）は 720p が 1.0 unit/秒、1080p が 1.5 units/秒、1 unit = 0.14 USD（list price）で、`cost_per_second_usd` は `720p: 0.14`・`1080p: 0.21`。ただし同ページは Kling 4.0 を「Coming Soon」とし、この価格を「プレビュー用で、公式の提供日と価格は別途告知」と書く。**正式価格が出たら見直す**
- **Kling 3.0（`kling-3.0`）は変えない**: 旧 API（`api.klingai.com`・`/v1/videos/text2video`・`image2video`・JWT）のまま、キーも `kling` のまま。新ホストへ移すか、API キー 1 つで `kling-v3` が通るかは実機で確かめるまで決められないので、移行しない（理由と確認項目は [kling-api.md](kling/kling-api.md) §5。#838 の未検証と同じ）
- **未検証（実際の API キーが要る）**: ① 新 API の create / `GET /tasks` の応答の形（公式の例を読んだだけ）、② `first_frame` / `last_frame` の Base64 の書式（`data:` の有無）、③ Kling 4.0 が Early Access のあいだ API キーで呼べるか、④ 単価が正式な価格に合うか、⑤ ネイティブ音声の有無

#### Seedance 2.5（`seedance-2.5`、Issue #874）

- **API**: Seedance 2.0 と同じ ModelArk の content generation tasks（ベンダー `seedance`、ベース URL `https://ark.ap-southeast.bytepluses.com`、`POST /api/v3/contents/generations/tasks` と `GET …/tasks/<id>`、`Authorization: Bearer`）で、`model` が `dreamina-seedance-2-5-260628`（`models.seedance-2.5.model`）。`seedanceBackend` を流用し、**2.5 では出力の仕様をプロンプト末尾の `--resolution` などのフラグではなく、公式の例のとおり本文のフィールドで送る**（2.0 は従来のフラグのまま）: `resolution`（`480p` / `720p` / `1080p`）・`ratio`・`duration`（整数の秒）・`generate_audio: true`（ネイティブ音声。公式の例はいずれも `true`）。`watermark` は送らず（公式の既定は付けない）、`output_format` も送らない（既定の mp4。mov は編集・延長の用途）
- **入力と排他**: テキストだけなら本文は `content` のテキストのみ。First Frame は `role: first_frame`、Last Frame は `last_frame`、自由な参照画像は `role: reference_image`（`data:` の Base64）で `content` に載せる。First Frame（と Last Frame）があるときは**参照画像を載せない**（#341 の排他。公式は first-frame / omni reference を別のタスク型として扱う）。First Frame があるときは公式の制約で **`ratio` を `adaptive` にする**（出力の縦横比は最初のフレームの画像に合う。それ以外の値は `InvalidParameter.TaskTypeConstraint` の非同期エラー）。参照画像があるときは `omni_reference_task_type: "reference"` を送り、検証を早める（公式の例の値）。編集・延長・動画や音声の参照・Draft（480p だけ）・最後のフレームの返却は使わない
- **長さ・参照**: 4〜30 秒の整数（公式は `-1` でモデルが長さを選ぶとするが、`Prepare` は常に長さを決める）。参照アセットは公式で最大 50（画像 30・動画 10・音声 10）、mam が送るのは画像だけなので `MaxReferenceImages` は 30（Seedance 2.0 は 9）。1080p は公式が 10-bit・H.265 と注記し、再生できない環境がある
- **単価**: ModelArk の料金ページ（<https://docs.byteplus.com/en/docs/modelark/model-pricing>、2026-10-28 に取得）の `dreamina-seedance-2-5-260628`（映像入力なしの表）は、480p・720p が $10.70、1080p が $11.7 / 100 万トークン（4K とオフライン推論は非対応）。トークン数は（出力の秒数 × 幅 × 高さ × フレームレート）/ 1024 で、同ページの 16:9・5 秒の例は 480p $0.514（$0.103/秒）・720p $1.156（$0.231/秒）・1080p $2.843（$0.569/秒）。`cost_per_second_usd` はこの 3 つ（例の金額が単価とトークン数の式に合うことを確かめた）。縦横比が違うとピクセル数が変わり、実際の価格も少し変わる（見積は 16:9 の値）。映像入力ありの表は別の単価・最低トークン数があるが、mam は映像を入力しないので使わない。`actual_cost` は取らない（見積のみ。API 応答の `usage.completion_tokens` から取れる可能性はあるが、このバッチでは扱わない）
- **Seedance 2.0（`seedance-2.0`）は変えない**: API のモデル ID は `seedance-2-0` のまま。公式の ID は `dreamina-seedance-2-0-260128`（ほか fast・mini）で、`seedance-2-0` が今のエンドポイントで通るかは実機で確かめるまで分からない。確かめずに公式 ID へ置き換えると、今通っているなら壊れるので動かさない。通らないときは `models.json` の `models.seedance-2.0.model` を `dreamina-seedance-2-0-260128` にすれば直せる（コードの変更は要らない）。2.0 の見積単価（480p 0.07 / 720p 0.15 / 1080p 0.37）は §9.2「Kling 3.0・Seedance 2.0 の見積単価」に出どころを書いた（Issue #880 で公式の料金ページに合わせた）
- **有効化の条件**: 公式は Seedance 2.5 の利用に、BytePlus のアカウント残高が 30 USD を超えるか、AI Savings Plan の 30 USD 以上、または Seedance 2.5 のリソースパックが必要としている。条件を満たさないアカウントでは API が拒否する（mam は事前に確かめない）
- **未検証（実際の API キーが要る）**: ① `dreamina-seedance-2-5-260628` がこのリージョン（`ap-southeast`）のエンドポイントで通るか、② 本文のフィールド（`resolution`・`ratio`・`duration`・`generate_audio`）と `omni_reference_task_type: "reference"` の受け付け、③ First Frame と `ratio: adaptive`、Base64 の画像（`data:`）が公式の URL / asset の代わりに受け付けられるか、④ 2.0 の `seedance-2-0` と `dreamina-seedance-2-0-260128` のどちらが通るか、⑤ 単価が実際の請求に合うか

#### MiniMax H3 / H3 Max（`minimax-h3`・`minimax-h3-max`、Issue #875）

出典（2026-10-28 に取得）: Video Generation <https://platform.minimax.io/docs/guides/video-generation>、Create Video Generation Task <https://platform.minimax.io/docs/api-reference/video-generation-v2-create>、Query Task <https://platform.minimax.io/docs/api-reference/video-generation-v2-query>、Pricing <https://platform.minimax.io/docs/pricing/overview>（Video）。

- **ベンダーと認証**: 新ベンダー `minimax`（`modeladapter.VendorMiniMax`）。`credential.Store` のキー名も `minimax`。ベース URL は `https://api.minimax.io`、認証は `Authorization: Bearer <API キー>`（公式の `bearerAuth`。キーは MiniMax のコンソールの Account Management > API Keys で作る）。`minimax-h3` と `minimax-h3-max` は同じベンダー（キー・ベース URL・同時実行・ブレーカーを共有する）。公式は H3 / H3 Max に **Pay-as-you-go の API** を選ぶよう求めている（Video Packages は H3 に未対応）ので、設定画面の説明にもそう書く。同時実行 3・タイムアウト 900 秒は mam の既定（公式に上限の記載が見つからない。Kling 4.0・Seedance 2.5 と同じ。動画の非同期ジョブが数分かかるため）
- **作成**: `POST {base_url}/v2/video_generation`、本文は `model`（`MiniMax-H3` / `MiniMax-H3-Max`。`models.minimax-h3.model` などで設定できる API モデル ID）・`content[]`・`duration`（整数の秒）・`resolution`（`768P` / `2K` / `480P`。mam の識別子は小文字の `768p` / `2k` / `480p` で、送るときに大文字にする）・`ratio`。応答は `{task_id}`。`content[]` の要素は `{type: text, text}`（必須。1 つ・7000 文字まで）と `{type: image_url, image_url: {url}, role}`。画像の `url` は Base64 のデータ URI（`data:image/<形式>;base64,…`）で入れる（公式が許す形の 1 つ。リクエスト全体は 64 MB 以下）
- **モード（混ぜない）**: テキストだけなら T2V（`ratio` が必須で `adaptive` 不可。mam の縦横比 `16:9` / `9:16` / `1:1` は公式の値）。First Frame は `role: first_frame`、Last Frame は `role: last_frame`（First と一緒に送る。単独の Last Frame も公式が許す）で、縦横比は画像から決まるので `ratio` は送らない。参照画像は `role: reference_image` で、`first_frame` / `last_frame` と**同じリクエストに入れない**（公式が排他と明記。#341 の Seedance と同じ）。First Frame があれば参照画像は載せず、`Prepare` も First Frame を優先する。Reference Reinforcement のときだけ参照画像を優先し、その参照モードでは `ratio` に Shot の縦横比を送る。参照動画・参照音声（`reference_video` / `reference_audio`）は使わない
- **取得**: `GET {base_url}/v2/query/video_generation/<task_id>`（認証は同じ。直近 7 日のタスクだけ）。`task.status` が `queued` / `running`（待つ。公式は 10 秒間隔を推奨。mam は状態確認の間隔で続ける）・`succeeded`（`task.content.url` をダウンロード）・`failed` / `cancelled`（`task.error.message` を理由にした 422 相当の拒否で、再提出しない）。出力の URL は期限つきなので、取得後すぐ保存する。URL は §9.3 の「ベンダー応答 URL の扱い」の許可リスト型（https・公開アドレスだけ）でダウンロードし、API キーは付けない
- **エラー**: HTTP のステータスがそのまま返る（400 / 401 / 402（残高不足）/ 422（入力の内容が審査に触れた）/ 429 / 500。本文は `{type: error, error: {type, message, http_code}}`）。429・5xx は §9.3 のとおり再試行・代替へ、402・422・401 などの 4xx は拒否として再試行しない
- **長さ・解像度**: H3 は 4〜15 秒・`768p` / `2k`、H3 Max は 5〜15 秒（4 秒は不可）・`480p` / `768p`（どちらも整数の秒。2K は H3 だけ）。`Prepare` は要求を満たす最短の長さに丸める（15 秒を超えるときは 15 秒で、Editor が後で詰める）。2k は Take の解像度の表記のまま保存する。公式は 2K の画素数を書いていないので、`domain.ResolutionHeight` は「2k」を 1440 行と見なし、プレビューはプロキシで再生する（プロキシは 1080 行までに縮めるだけで、実際の画素数が小さくても害はない）。プロンプトは 7000 文字まで（`Prepare` の上限。ネガティブプロンプトの欄は無く Avoid は本文へ折り込み、超えるときは §9.3 のとおり末尾の Constraints・Avoid 文を残す）
- **参照画像の上限と課金**: API は参照画像を最大 9 枚まで取るが、公式の料金では**1 タスクの先頭 5 枚（H3 Max は 2 枚）が無料で、超えた分は 1 枚ごとに課金**される（H3 は $0.04、H3 Max は $0.074。出力の解像度や画像の大きさに依存しない）。mam は**無料枠を超えて送らない**ため、`MaxReferenceImages` は H3 が 5、H3 Max が 2 とし、First / Last Frame（2 枚）も無料枠に収まる。この上限なら秒単価だけが費用になり、追加の画像課金を見積に載せる必要がない（無料枠を超える参照画像を使いたくなったら、その枚数分の単価を見積に足す設計が要るので別 Issue）。参照動画・参照音声は送らない（入力動画は出力解像度の秒単価で別課金、音声は無料）
- **使えない項目**: シード・ネガティブプロンプトは公式の本文に無い（`SupportsSeed` は偽）。H3 Max の `extra.prompt_expansion_mode`（既定 `balanced`）、`callback_url` は使わない。出力の音声の有無は公式が明記していないので `NativeAudio` は偽にした
- **単価**: 出力動画の秒単価（Pay-as-you-go）。H3 が 2K 0.13 / 768P 0.08、H3 Max が 768P 0.08 / 480P 0.05 USD で、`cost_per_second_usd` は `minimax-h3` が `768p: 0.08`・`2k: 0.13`、`minimax-h3-max` が `480p: 0.05`・`768p: 0.08`。同じモデル・解像度ならタスクの種類（T2V・I2V・First/Last・Reference）で単価は変わらない（公式）。見積（`estimate`）・Cost Ledger の `estimated_cost` は秒数 × この単価。`actual_cost` は取らない（応答の `usage` は秒数で金額ではない）。なお公式は H3-Highspeed（768P 0.06）・H3-Lite（768P 0.04 / 480P 0.025）も載せるが、Issue の範囲外なので識別子を足していない
- **ルーティング**: 既定の Quality/Cost Policy・routing の候補には入れない（実機で品質と単価を見てから。§4）
- **未検証（実際の API キーが要る）**: ① 作成・照会の応答の形（公式の例を読んだだけ。`task_id`、`task.status`、`task.content.url`）、② 画像を Base64 のデータ URI で入れたときの受け付け（公式の例は公開 URL。64 MB の上限に画像 Base64 が収まるか）、③ `role` を省いた単独画像と `last_frame` だけの単独指定の効き方、④ 参照画像のモードで `ratio` を明示する指定が通るか（公式は任意で、既定は `adaptive`）、⑤ 出力のネイティブ音声の有無、⑥ 実際の請求が秒単価に合うか、⑦ 同時実行 3・タイムアウト 900 秒が MiniMax のレート制限・生成時間に合うか

#### Wan 3.0（`wan-3.0`、Issue #876）

出典（2026-10-28 に取得）: モデルの紹介と料金 <https://www.alibabacloud.com/help/en/model-studio/wan3-0-video>（Last Updated: Oct 11, 2026）、API Reference <https://www.alibabacloud.com/help/en/model-studio/wan3-video-generation-api-reference>（Last Updated: Oct 15, 2026。**Currently in preview**）、リージョンとエンドポイント <https://www.alibabacloud.com/help/en/model-studio/regions>。

- **ベンダーと識別子**: 新ベンダー `alibaba`（`modeladapter.VendorAlibaba`。Alibaba Cloud Model Studio。DashScope は同社の API の名前で、この API キーを使う公式の環境変数名も `DASHSCOPE_API_KEY`）。`credential.Store` のキー名も `alibaba`。mam の識別子は `wan-3.0`、API のモデル ID は `wan3.0-video`（標準版。`models.wan-3.0.model` で設定できる）。公式にはもう 1 つ `wan3.0-video-prime`（標準版と能力は同じで、エンド・ツー・エンドの速度を大きく上げた高速版）があるが、料金ページに価格が載っていないので識別子を足さない（`models.wan-3.0.model` を `wan3.0-video-prime` にすれば呼べるが、見積は標準版の単価のままになる）。ローカルの Wan 2.2（`wan22-ti2v-5b`・`wan22-i2v-14b`、ComfyUI のテンプレート。[model-manager.md](model-manager/model-manager.md)）とは別系統で、本件は変えない
- **エンドポイントとリージョン**: 公式が推奨する**ワークスペース専用ドメイン**（`{WorkspaceId}.{リージョン}.maas.aliyuncs.com`）を使う。API Reference の Wan 3.0 の URL がこの形だけで、DashScope ドメイン（`dashscope-intl.aliyuncs.com` など）は旧来のもので 2026-09-30 以降は新機能に対応しないと公式が書くため。リージョンはモデル・エンドポイント・API キーを同じにしなければ呼び出しが失敗する（リージョン違いは使えない）。**既定は Singapore（`ap-southeast-1`）** で、既定の `vendors.alibaba.base_url` は `https://{WorkspaceId}.ap-southeast-1.maas.aliyuncs.com`。`{WorkspaceId}` は利用者ごとの値なので、**API キーを `ワークスペース ID:API キー` の形で保存**し（Model Studio の Workspace Management の API Host の先頭。例の `llm-xxx.cn-beijing.maas.aliyuncs.com` なら `llm-xxx`。Kling 3.0 の `ACCESS_KEY:SECRET_KEY` と同じ考え方）、呼び出しのたびに `{WorkspaceId}` をその値に置き換える。ワークスペース ID はホスト名の 1 つのラベルに入れるので、英数字とハイフン（先頭は英数字）だけを受け付け、それ以外が入ったキーは、リクエストを送らずに拒否する（キーを送るホストを利用者の入力で別のホストに変えられないため）。`base_url` に `{WorkspaceId}` を含まない値（利用者が自分の API Host を丸ごと書いたもの）を設定したときは、保存したキーを API キーそのものとして使う。**ほかのリージョン**（China (Beijing)・US (Virginia)・Japan (Tokyo)・Germany (Frankfurt)・China (Hong Kong)）を使うには、そのリージョンのワークスペースと API キーを用意し、`models.json` の `vendors.alibaba.base_url` をそのリージョンのホスト（例 `https://{WorkspaceId}.ap-northeast-1.maas.aliyuncs.com`）にし、単価も下のとおり `models.wan-3.0.cost_per_second_usd` で変える
- **認証と作成**: `Authorization: Bearer <API キー>`、`X-DashScope-Async: enable`（必須。無いと同期呼び出しは非対応のエラーになる）、`Content-Type: application/json`。`POST {base_url}/api/v1/services/aigc/video-generation/video-synthesis`、本文 `model`・`input`（`prompt` と、画像があるときの `media[]`）・`parameters`（`resolution`: `480P` / `720P` / `1080P`（mam の識別子は小文字の `480p` / `720p` / `1080p` で、送るときに大文字にする）・`ratio`・`duration`（整数の秒）・`audio: true`）。応答は `{output: {task_id, task_status: PENDING}}`。作成の失敗は `{code, message, request_id}`（HTTP のステータスも付く。200 で `task_id` が無く `code` があるときも 422 相当の拒否にする）。**同じタスクを二重に作らず、ポーリングで結果を取る**（公式の注意。mam の二重課金回避の規則と同じ）
- **モード（混ぜない）**: テキストだけなら `media` を付けない T2V（`ratio` は Shot の縦横比 `16:9` / `9:16` / `1:1`）。First Frame は `media` の `{type: first_frame, url}`、Last Frame は `{type: last_frame, url}`（First と一緒に送る）で、縦横比は入力の画像から決めるので `ratio: adaptive` にする。参照画像は `{type: reference_image, url}`（最大 10 枚）で、`first_frame` / `last_frame` と**同じリクエストに入れない**（公式が排他と明記し、混ぜると `InvalidParameter` で失敗する。#341 の Seedance と同じ）。First Frame があれば参照画像は載せず、`Prepare` も First Frame を優先する。Reference Reinforcement のときだけ参照画像を優先し、その参照モードでは `ratio` に Shot の縦横比を送る。画像は `data:{MIME};base64,…` で `url` に入れる（公式が許す形の 1 つ。JPEG・PNG・BMP・WEBP、1 枚 20 MB 以下、各辺 240〜8000 px、縦横比 8:1 以内）。参照動画・参照音声・`file`・`link`、動画の編集・延長のモードは使わない
- **取得**: `GET {base_url}/api/v1/tasks/<task_id>`（`Authorization` のみ。`task_id` の有効期間は 24 時間）。`output.task_status` が `PENDING` / `RUNNING`（待つ。公式は 15 秒間隔などを推奨。mam は状態確認の間隔で続ける）・`SUCCEEDED`（`output.video_url` をダウンロード。MP4（H.264）で、24 時間有効なので取得後すぐ保存する）・`FAILED` / `CANCELED` / `UNKNOWN`（UNKNOWN はタスクが無い・状態が不明。`output.code`・`output.message` を理由にした 422 相当の拒否で、再提出しない）。`video_url` は OSS のバケットなど別ホストで、§9.3 の「ベンダー応答 URL の扱い」の許可リスト型（https・公開アドレスだけ）でダウンロードし、API キーは付けない
- **長さ・解像度**: 動画の入力なしで 2〜30 秒の整数（公式は `-1` でモデルが長さを選ぶとするが、`Prepare` は常に長さを決める）。解像度は `480p` / `720p` / `1080p`（公式の既定は 1080P だが、mam は常に送る）。出力は 30fps。**ネイティブ音声つき**（`audio` は公式の既定が `true`。切るか否かは料金に影響しないと公式が明記しているので、mam は `true` を明示し、`NativeAudio` を真にした。Take の音声は Audio Director のミキシングが扱う）
- **使わない項目**: `seed`（公式は `-1` か 0〜2147483647 を取るが、`SupportsSeed` は偽のまま送らない。生成テストの同じ入力の本数ぶんの呼び出しは別 Issue の話）、`prompt_extend`（公式の既定は `true` で、LLM がプロンプトを書き換える。mam は送らず公式の既定のままにする。Constraints を書き換えで失わないかは実機で見てから決める）、`watermark`（既定は付けない）、ネガティブプロンプト（欄が無く Avoid は本文へ折り込む）
- **単価**: モデルの紹介ページ（Model Studio の料金。プロモーションは含まない）の**生成した動画 1 秒あたり**の価格。**Singapore（国際）**は 480P 0.05 / 720P 0.10 / 1080P 0.20 USD で、`cost_per_second_usd` の既定は `480p: 0.05`・`720p: 0.10`・`1080p: 0.20`。**そのほかのリージョン**（China (Beijing)・Japan (Tokyo)・Germany (Frankfurt)・US (Virginia)・China (Hong Kong)）は同じページで 480P 0.041256 / 720P 0.082513 / 1080P 0.165025 USD で、`base_url` を別のリージョンにしたら `models.wan-3.0.cost_per_second_usd` をこの値に合わせる（mam は `base_url` から単価を切り替えない。見積は参考値で、請求ではない）。見積（`estimate`）・Cost Ledger の `estimated_cost` は秒数 × この単価。`actual_cost` は取らない（応答の `usage` は秒数・解像度・fps で金額ではない）。レート制限は公式で RPM 300（アカウント単位）で、同時実行の上限は書かれていないので、同時実行 3 は mam の既定（ほかのクラウド動画と同じ）
- **ルーティング**: 既定の Quality/Cost Policy・routing の候補には入れない（実機で品質と単価を見てから。公式も preview。§4）
- **未検証（実際の API キーが要る）**: ① 作成・照会の応答の形（公式の例を読んだだけ。`output.task_id`、`output.task_status`、`output.video_url`）、② ワークスペース専用ドメインと `ワークスペース ID:API キー` の形で通るか（キーが Singapore のものであること）、③ 画像を Base64 のデータ URI で入れたときの受け付けと、`first_frame` だけ・`last_frame` を伴う場合の効き方、④ 参照画像のモードで `ratio` を Shot の縦横比にする指定が通るか、⑤ 同時実行 3 とタイムアウト 1800 秒が Model Studio のレート制限・生成時間に合うか（公式の例は 5 秒の動画で約 12 分）、⑥ 出力のネイティブ音声の有無（`audio: true` で付くか）、⑦ 単価が実際の請求に合うか（preview の価格）、⑧ `prompt_extend` の既定（`true`）が Constraints を保つか

#### Luma Ray 3.2（`luma-ray-3.2`、Issue #878）

出典（2026-10-28 に取得）: 価格 <https://docs.agents.lumalabs.ai/guides/pricing/>、動画の生成 <https://docs.agents.lumalabs.ai/guides/videos/generation/>、作成 <https://docs.agents.lumalabs.ai/api/resources/generations/methods/create/>、モデル <https://docs.agents.lumalabs.ai/guides/model/>、レート制限 <https://docs.agents.lumalabs.ai/guides/rate-limits/>、エラー <https://docs.agents.lumalabs.ai/guides/error-handling/>、使用量とクレジット <https://docs.agents.lumalabs.ai/guides/usage/>。製品側の USD 目安は <https://lumalabs.ai/api>。

- **ベンダーと識別子**: 新ベンダー `luma`（`modeladapter.VendorLuma`）。`credential.Store` のキー名も `luma`。mam の識別子は `luma-ray-3.2`、API のモデル ID は `ray-3.2`（`models.luma-ray-3.2.model`）。ベース URL は `https://agents.lumalabs.ai`、認証は `Authorization: Bearer <API キー>`（公式の例の環境変数は `LUMA_AGENTS_API_KEY`）。設定画面の「Luma（Ray 3.2）」から保存・削除する。同時実行 3・タイムアウト 900 秒は mam の既定（公式の上限は契約ごと: 1 分あたりの作成数と同時実行ジョブ数で、ダッシュボードに出る。公式の例は 10 分の期限）
- **範囲**: T2V と I2V（First Frame・Last Frame）だけ。編集（`video_edit`）・reframe・extend・マルチキーフレーム（`keyframes`）・`loop`・HDR・EXR は使わない（別の料金表がある。Issue #878 の範囲）
- **作成**: `POST /v1/generations`（HTTP 201）。本文 `model: "ray-3.2"`・`type: "video"`・`prompt`（1〜6,000 文字）・`aspect_ratio`（`16:9` / `9:16` / `1:1`。公式は 3:4・4:3・21:9 も取る）・`video`: `resolution`（`360p` / `540p` / `720p` / `1080p`）・`duration`（文字列の `"5s"` / `"10s"`）。First / Last Frame があるときは `video.start_frame` / `video.end_frame`（画像は Base64 の `data` と `media_type`。公式は URL・`file_id` も取る）で、**`aspect_ratio` は送らない**（公式: 省くとプロンプトとフレームからモデルが決める）。ネガティブプロンプト・シードの欄は無い（Avoid はプロンプトへ折り込む。`SupportsSeed` は偽）
- **取得**: `GET /v1/generations/<id>`。`state` が `queued` / `processing`（待つ。公式は 2〜5 秒間隔を勧める。mam は状態確認の間隔で続ける）・`completed`（`output[]` の `type: video` の `url` をダウンロード。署名つきで **1 時間で失効**するので取得後すぐ保存する）・`failed`。`failed` の `failure_code` が `generation_failed` / `output_not_found` / `rate_limited` のときは Luma 側の失敗で課金は返金され、同じ生成を聞き直しても変わらないので、再提出せずに代替モデルへ回る（502 相当）。`content_moderated` など、それ以外は 422 相当の拒否（再提出しない）。作成の同期エラーは課金されない: 402（残高不足）・400・422 は拒否、429（RPM と同時実行ジョブ数のどちらも `Retry-After` つき）・502・503 は再試行の対象
- **長さ・解像度**: 5 秒か 10 秒（`Prepare` は要求を満たす最短を選ぶ）。**10 秒は `start_frame` / `end_frame` と一緒に送れない**（公式が拒否する）ので、`Prepare` は First / Last Frame があるときは長さを 5 秒にする（要求が 6 秒以上でも）。解像度は `360p`（ドラフト）・`540p`・`720p`・`1080p`。`NativeAudio` は偽（公式の動画のページに音声の記載が見つからない）。自由な参照画像は動画では取れない（`image_ref` は画像モデルの項目）ので `MaxReferenceImages` は 0
- **単価**: Pay-as-you-go の価格表（「`ray-3.2` — per-video pricing」の Video generation の標準ダイナミックレンジ）は、5 秒が 360p 20 / 540p 50 / 720p 100 / 1080p 400 クレジット、10 秒が 60 / 150 / 300 / 1200 クレジット。**クレジットの USD は公式が固定していない**（価格ページ: ドル価値はアカウントのプランや契約による）。そこで使用量のページ（Credits and dollars）に従い、**公式の既定のリスト価格 1 クレジット = 0.003 USD** で換算する（`lumalabs.ai/api` の USD 目安 540p $0.15・720p $0.30・1080p $1.20（5 秒）とも合う。Runway Gen-4.5 の 1 クレジット = 0.01 USD は Runway 固有で、Luma には使わない）。**10 秒は 5 秒の 3 倍**で秒に比例しないので、`cost_per_second_usd` は「5 秒の動画の 1 秒あたり」（360p 0.012 / 540p 0.03 / 720p 0.06 / 1080p 0.24 USD）とし、`Config.VideoCost` が 5 秒以下は 5 秒分、それより長いときは 5 秒分の 3 倍にする（`lumaCost`）。契約で単価が違うときは `cost_per_second_usd` を書き換える。フレームつきは 5 秒に固定されるので、ルーティングと予算の事前の見積も、First Frame が**実際に付くとき**（Shot に Keyframe があり、そのファイルが読めるとき）だけ 5 秒のクリップとして出す（`Config.ClipSeconds`。`Prepare` と同じ規則）。Keyframe の Reference があってもファイル・Asset が読めず First Frame が付かないときは、Shot の長さのクリップ（10 秒の Shot は 10 秒）を生成し、見積りも同じ長さで出す（生成・Prompt Candidates・Different Model 以外の再生成・ルーティング・予算判定のすべてで、Keyframe が読めないものは First Frame として数えない。Issue #902）。生成テスト・モデル一覧の単価は 5 秒のクリップの 1 秒あたり（上の `cost_per_second_usd` そのまま。`VideoPerSecond`）で、1 秒のクリップの料金（5 秒分）ではない。HDR・EXR・延長・編集は見積に入れない。`actual_cost` は取らない（見積のみ）
- **ルーティング**: 既定の Quality/Cost Policy・routing の候補には入れない（実機で品質と単価を見てから。§4）
- **未検証（実際の API キーが要る）**: ① 作成（201）・取得の応答の形（公式の例を読んだだけ。`id`、`state`、`output[].url`）、② `start_frame` / `end_frame` を Base64 の `data` + `media_type` で送って通るか、`end_frame` だけの指定の効き方、③ `aspect_ratio` を送らないフレームつきの出力の縦横比、④ 540p・360p の出力、⑤ 出力のネイティブ音声の有無、⑥ 実際の請求がクレジット表と 1 クレジット = 0.003 USD に合うか（契約・プランで違いうる）、⑦ 同時実行 3 とタイムアウト 900 秒が契約のレート制限・生成時間に合うか

## 10. AI Video Judge と Face Identity Similarity の実装（`internal/agent/judge`、`internal/facesim`）

M1 Issue #9（F-13）の実装仕様。

### 10.1 モデルと推論ランタイムの検証結果

| 項目 | 採用 | ライセンス | 検証 |
|---|---|---|---|
| 顔検出 | YuNet（`face_detection_yunet_2023mar.onnx`、OpenCV Zoo）。5 点ランドマーク付き | MIT | 本リポジトリの Go 実装の出力が OpenCV `FaceDetectorYN` の出力と一致（同一画像で検出枠・スコアが一致）することを確認 |
| 顔埋め込み | SFace（`face_recognition_sface_2021dec.onnx`、OpenCV Zoo）。112×112 の整列顔から 128 次元、コサイン類似度で照合 | Apache-2.0（重み。学習データの権利は §8） | 同一人物（縮小・明度変更）0.93、別人物 −0.03（各 1 例。閾値は実測で決める） |
| 推論ランタイム | ONNX Runtime 1.23.x の共有ライブラリ（`libonnxruntime.so` / `.dylib` / `onnxruntime.dll`）を `github.com/shota3506/onnxruntime-purego`（MIT、purego 経由）で実行時にロード | MIT（ONNX Runtime 本体、バインディングとも） | `CGO_ENABLED=0` のバイナリのまま推論できることを確認。cgo バインディング（`yalue/onnxruntime_go`）は単一バイナリ・クロスコンパイル（`make build`）と両立しないため不採用 |

- **同梱しない**: モデルファイル（約 39 MB）と ONNX Runtime ライブラリはバイナリに同梱せず、利用者が配置する（`--face-model-dir`（`MAM_FACE_MODEL_DIR`、既定 `<data-dir>/models`）にモデル 2 ファイル、`--onnxruntime-lib`（`MAM_ONNXRUNTIME_LIB`、空ならシステムの標準ライブラリ検索）にランタイム）。`scripts/fetch-face-models.sh` がモデルを取得し SHA-256 を検証する（取得元は OpenCV Zoo のコミットに固定、`sha256sum` または `shasum` で検証、https のみ・タイムアウトと再試行あり、失敗時に一時ファイルを残さない。配置先は引数、`MAM_FACE_MODEL_DIR`、`MAM_DATA_DIR/models`、ユーザー設定ディレクトリ配下 `mam/models`（macOS は `~/Library/Application Support/mam/models`、それ以外は `${XDG_CONFIG_HOME:-~/.config}/mam/models`）の順で決まる。最後の既定値はサーバーの `--data-dir` の既定値（`os.UserConfigDir()/mam`）の `models` と一致するが、スクリプトは `--data-dir` / `--face-model-dir` のフラグを読まないため、サーバーをそれらで起動しているときは、同じディレクトリを引数か環境変数で渡す）
- **実モデルのテスト（手動、CI では動かさない）**: 上の「検証」欄の値（OpenCV `FaceDetectorYN` との出力一致、類似度 0.93 / −0.03、`CGO_ENABLED=0` での推論）は開発時に手元で一度だけ測った記録で、自動テストが毎回確かめているわけではない。`backend/internal/facesim/onnx_test.go` の `TestONNX_SamePersonIsCloserThanAnotherPerson`（同一人物の類似度が別人より高い。同一人物 > 0.7、別人物 < 0.3、自己類似度 1.0）と `TestONNX_ImageWithoutFaceHasNoFaces`（顔のない画像は顔 0 件）は、実モデルを使うため、環境変数が未設定だと `t.Skip` する。CI（`.github/workflows/ci.yml`）も `make test` もこれらを設定しないので、これらは CI でも pre-push でも常に Skip され、`align.go` / `detect.go` / `imaging.go` の変更で埋め込みの出力が崩れても検知しない（純 Go の単体テスト `facesim_test.go` は実モデルを使わない）。変更したときは手元で次を実行して Skip が出ないことを確かめる。理由は、写真（同一人物 2 枚・別人 1 枚・顔なし 1 枚）が権利上リポジトリに置けず、約 39 MB のモデルと ONNX Runtime を毎回取得するコストに見合わないため
  1. `scripts/fetch-face-models.sh <DIR>` でモデルを `<DIR>` に取得する（SHA-256 検証付き）。ONNX Runtime 1.23.x の共有ライブラリは https://github.com/microsoft/onnxruntime/releases から別に取得する
  2. 写真ディレクトリに `a.jpg`（人物 A）、`a_small.jpg`（人物 A を別のサイズ・明るさで）、`b.jpg`（人物 B）、`none.jpg`（顔のない画像）を置く
  3. `MAM_FACESIM_MODEL_DIR=<DIR> MAM_FACESIM_RUNTIME_LIB=<libonnxruntime のパス> MAM_FACESIM_PHOTOS=<写真ディレクトリ> go test -v -run TestONNX ./backend/internal/facesim/`（`MAM_FACESIM_RUNTIME_LIB` が空ならシステムの標準ライブラリ検索）

  `MAM_FACESIM_MODEL_DIR` と `MAM_FACESIM_PHOTOS` のどちらかが空だとこの 2 つのテストは Skip する。`TestNewONNX_MissingModelsIsUnavailable`（モデル未配置は `ErrUnavailable`）は環境変数なしで常に動く
- **未配置でも動く**: モデルまたはランタイムが無いとき、起動時に警告ログを残し Face Identity Similarity を無効にする。Take は定性評価のみで判定し、Judge Result の `metrics.face_identity_status` は `unavailable` になる（チャットにも明記する）
- **ローカル実行**: 顔検出・埋め込みはプロセス内で実行し、顔画像はネットワークに出さない。Reasoning LLM（Gemini）には定性評価用のフレームと Reference Pack を送る（`non-functional.md` §5 の外部送信方針の対象。顔の数値化そのものは送らない）

### 10.2 動作

- **フレームサンプリング**: `ffmpeg` / `ffprobe`（探し方は [ffmpeg-lookup.md](ffmpeg-lookup.md)）でフレームを抜き出す。数値評価用は `face_sample_fps`（既定 4 fps）で最大 `max_face_frames`（48）枚（長い Take は間引く）、定性評価用は等間隔に最大 `review_frames`（12）枚（`face_sample_fps` で頭打ちになるため、3 秒未満の Take は 12 枚に満たない）。抽出するフレームは幅を数値評価用が `face_max_width`（既定 1280 px）、定性評価用が `review_max_width`（既定 768 px）に収める（超える分は縮小する）。ffmpeg が無いと Judge 自体が無効になる（起動時に警告）
- **顔照合**: 各フレームで顔検出 → 埋め込み → Character の参照顔とのコサイン類似度。1 フレームに複数の顔があるときは、全ペアの類似度が高い順に「1 顔 = 1 Character」で割り当てる。参照顔は、Episode の Character が Series 正本から作られたもの（`origin_id`）ならその正本の `face_reference_asset_id`（F-29。Episode をまたいで同じ顔を基準にする）、Character 自身の `face_reference_asset_id`、Reference Pack の Character 画像のうち名前が一致するもの（同じファイルは 1 枚に数える）。検出できない参照画像は使わず、どの参照画像にも顔が無い Character は測定対象から外す（`face_identity_status = not_applicable`）
- **欠測**: 顔を検出できない（または割り当てられなかった）フレームは `undetected_ratio` にだけ数え、類似度の平均・最小・drift 判定・FAIL 判定に含めない。`undetected_ratio` が `max_undetected_ratio`（0.5）を超える、または検出フレームが `min_detected_frames`（3）未満なら `evaluated = false` とし、その Take は定性評価だけで判定する（`face_identity_drift` は出さない）
- **閾値**: 警告 `warn` と FAIL `fail` は、その Character の参照画像同士の類似度分布から決める（ソースの固定類似度は持たない）。顔が取れた参照が 2 枚以上あり、参照同士の平均類似度 $m$ が `min_calibration_similarity`（0.4）以上のとき、`warn = calibration_warn_scale × m`（既定 0.85）、`fail = calibration_fail_scale × m`（既定 0.65）とし `calibrated = true` にする。適用した閾値は `metrics.face_identity[].warn_threshold` / `fail_threshold` に記録する。参照が 1 枚、または平均が下限未満のときは数値の警告 / FAIL を出さない（`calibrated = false`、`evaluated = false`、閾値は 0）。類似度は記録し、合否は定性評価に任せる。スケールと下限は `--judge-config`（`MAM_JUDGE_CONFIG`、JSON、未知のキーはエラー）で上書きできる。キーは `frames` と `face` の入れ子で、これらは `face` の下に置く（例: `{"face":{"calibration_warn_scale":0.8,"calibration_fail_scale":0.6,"min_calibration_similarity":0.4}}`）。フラットに書くと未知のキーとして起動に失敗する
- **drift Issue**: 連続する `min_run_frames`（2）フレーム以上が `warn` を下回った区間を `face_identity_drift` Issue にする（`time_range`・`frames` 付き、`dimension = character_identity`）。区間内に `min_run_frames` フレーム連続で `fail` を下回る箇所があれば `severity = high`、なければ `medium`（警告）。顔が検出できないフレームは区間を切る
- **定性評価**: Reasoning LLM（`AgentVideoJudge`、既定 Gemini → GPT）に、Canonical Shot Spec・前後 Shot の Spec・LOCK 中の評価軸・Reference Pack（`max_reference_images`（既定 4）枚まで）・サンプルフレームを渡し、`spec/spec.md` §12.2 の評価軸（識別子は `domain.JudgeDimensions`）で JSON を返させる。数値評価が成立している Character については、モデル自身の `face_identity_drift` は採用しない（数値が正本）。`time_range` は Take の長さに収め、ソースのフレーム番号（`frames`）を付ける。時間範囲を読めない Issue は範囲なしで残す
- **判定（決定的ルール、`judge.Decide`）**: (1) LOCK 違反（LOCK 中の評価軸の medium 以上の Issue、または Character Identity が LOCK のときの FAIL 閾値割れ = high の `face_identity_drift`）があれば、モデルの判定・信頼度に関係なく FAIL（`spec/spec.md` §13）。(2) それ以外は、モデルが fail を返す、または severity `high` の Issue があれば FAIL、なければ PASS。警告（medium）だけでは FAIL にしない。`overall_confidence` はモデルの値をそのまま保存する
- **LOCK の解決**: Shot Spec の `character.identity` / `product.geometry` が `locked`、または `lock` テーブルの `(character, <id>, identity)`・`(product, <id>, geometry|label)` が `mode = lock` のとき、Character Identity + Face / Product Geometry / Logo・Text Accuracy が LOCK 扱い
- **Repair Strategy**: モデルの `repair_strategy`（`action` は `spec/spec.md` §13 の 6 種）を保存する。high の `face_identity_drift` があるとき、`action` が無い・`prompt_repair` なら `reference_reinforcement` に置き換え、`increase_face_reference_strength` を `instructions` に足す（`spec/spec.md` §12.4）。PASS で戦略が無い Take は `repair_strategy` なし
- **ジョブ**: Take を作るたびに `job.kind = judge` のジョブを起動する（`payload = {take_id, shot_id}`）。再開可能ジョブ（低コストで再実行できるため、異常終了後は再実行する。再実行できない場合 —— 試行回数が上限、または Judge が構成されていない —— はジョブを失敗にし、その Shot の `JUDGING` を起動時に `PLANNING` へ戻す）。実行中は Shot を `JUDGING` にし（GENERATING 中の Shot の状態は変えない）、`/ws/shots/:id` に `action = judge` の `queued` / `judging` / `done` / `failed` を流す。結果は `judge_result` に追記し（上書きしない）、チャットに PASS/FAIL・Issue・LOCK 違反・数値評価が使えなかった理由を投稿する。同じ Take の Judge が queued/running なら `POST /takes/:id/judge` は 409
- **エラー**: ffmpeg 未導入は「ffmpeg / ffprobe が見つかりません」、API キー未設定・レート制限・タイムアウト・AI 出力不正は生成と同じ分類（`generation.FailureReason`）で通知する（ffmpeg / ffprobe の上限時間超過 `media.ErrTimeout` も同じ分類で、「動画・音声の処理（ffmpeg）が制限時間を超えました」と通知する。上限は `spec/requirements/non-functional.md` §3）。顔モデルの推論エラーは Judge を失敗させず、その Take を定性評価のみで判定する（`face_identity_status = unavailable`）

## 11. Repair Agent と Jev のシャドー実行の実装（`internal/agent/repair`、`internal/agent/jev`）

M1（F-14、Issue #10）で実装する Repair / Retake Loop の判断部分。§4.5 の導入手順に従い、前段選別は有効にせず Jev はシャドー実行のみ行う。

### 11.1 戦略の選択

- **決定的ルール（Jev を経由しない）**: (1) Retry Policy の上限判定（Max retries per Shot・Max cost per Shot・Project 予算。`domain.EffectivePolicy.CanRetry`）、(2) LOCK 違反・Critical constraints（LOCK 違反のある Take は点数に関係なく不合格。`JudgeResult.Accepted`）、(3) 実行できない戦略の除外（`repair.Candidates`: Prompt が LOCK なら Prompt Repair / Partial Regeneration は除外、Reference が無ければ Reference Reinforcement、別モデルが無ければ Different Model、時間範囲を切れない Issue・high の Issue・LOCK 違反があれば Edit-Level Repair を除外。すでに試した戦略は繰り返さないが Full Retake は毎回新しい生成なので残す）、(4) `face_identity_drift` があり Reference Reinforcement が候補にあれば、それを選ぶ（`repair.Rule`）。候補が 1 つのときもルールで決める
- **Repair Agent（Reasoning LLM、`llm.AgentRepair`）**: ルールで決まらないときだけ、残った候補の中から選ぶ。候補外の答えは採用しない。LLM が答えられないとき（キー無し・レート制限・不正な出力）は、Judge の `repair_strategy` が候補にあればそれを、無ければ Full Retake を使う。誰が決めたかは `repair_action.instructions.decided_by`（`rule` / `llm`）に残す
- **不合格の判定**: Judge の結果が `fail`、LOCK 違反あり、または Judge が点数（`judge_result.scores`）を付けていて Minimum acceptance score（総合。Production Template の重みで軸別点数を平均）か軸別の最低点（Production Template の各軸）に届かないときに Repair の対象になる

### 11.2 Retry Policy と Escalation

- Max retries は「新しい Take を作った再生成系の Repair の数」に、Prompt Candidate Generation が Shot で作った Take の数から最初の 1 本を除いた数を足して数える（失敗して Take を作らなかった Repair と Edit-Level Repair は数えない）。候補生成も Repair も同じ上限を使うので、候補生成が上限まで Take を作った後の自動ループは Take を作らず Escalation に進む。次の再生成の見積りコストは、Shot の累計コスト（台帳）に足した額を Max cost per Shot（`project.shot_budget_ceiling`）に対して、Project の累計に足した額を Project の予算上限に対して検査する。Different Model は別モデルの見積りで検査し直す
- 上限に達したら Take を作らず、チャットに Escalation カードを出して人間に判断を求める（Confirmation Level に関係なく出す）。AUTO MODE の自動ループは同じ Judge 結果について 1 度だけ尋ねる。手動の Repair は毎回尋ねる
- **AUTO MODE**: Take の生成・Judge・Repair のジョブが成功するたびに Shot を見て、Shot のすべての Take が Judge 済みで 1 つも合格していなければ、最良の Take（LOCK 違反なし → 総合点が高い → high の Issue が少ない → 新しい方）から Repair を始める。合格した Take が 1 つでもあれば止まる。DIRECTOR MODE では自動で始めず、ユーザーが `POST /takes/:id/repair` を呼ぶ

### 11.3 戦略ごとの再生成

いずれも新しい `take` 行を作り（既存 Take は変えない）、作った Take を Judge する。Shot は再生成中 `REPAIRING` になり、GENERATING / REPAIRING の Shot は二重に始められない。

| 戦略 | 実装 |
|------|------|
| Prompt Repair | Prompt Planner が Issue と修復指示に沿って Shot Spec を修正して再生成する。LOCK された部分は変えない（Prompt が LOCK なら選べない） |
| Partial Regeneration | Prompt Repair に加え、Judge の `preserve`（良かった部分）を保つ。新しい Take は Shot 全体を作り直す（M1）。**M2**: Judge のすべての Issue が `time_range` を持ち、ffmpeg があるときは、それに加えて修復元の Take の問題区間（前後 0.2 秒を足し、0.3 秒未満は広げる）だけを新しい Take の同じ区間に差し替えた Take（`repair.Splicer` = `TakeService.SpliceRegenerated`、`media.Compose` でクロスフェード）を候補として 1 本足し、Judge する。区間の決め方と足さない条件は `spec/spec.md` §13 の「Partial Regeneration の範囲（M1 と M2）」が正 |
| Full Retake | 同じ Shot Spec で新しく生成し直す |
| Different Model | Quality Policy の primary / fallback の**もう一方**のモデルで生成する（既定では FAST は Kling 3.0 ↔ Seedance 2.0、BALANCED は Seedance 2.0 ↔ Kling 3.0、MAX QUALITY は Runway Gen-4.5 ↔ Kling 3.0 の自動フォールバック、§4。Multi-model routing 有効時は `model-adapters-m3.md` §13.4 のとおり）。ユーザーが Video Model を固定していたとき（AUTO 以外）は選べない。再生成の見積り（長さ・単価）は `Prepare` が要求するクリップと同じ規則（`Config.ClipSeconds`）で出す。Luma Ray 3.2 は First Frame（Keyframe）が**実際に読めて付くとき**だけ 5 秒基準で、Keyframe のファイルが読めず付かないときは Shot の長さのまま（10 秒の Shot は 10 秒基準）で見積る。見積りと送るクリップの長さは常に一致する |
| Reference Reinforcement | Reference に基づく制約を Shot Spec に足し、失敗した種類の Reference を先頭にする。自由な Reference 画像を受け取れるモデルでは、Keyframe の代わりにそれを使う（API が First Frame と参照画像の混在を認めないため、どちらか一方だけを送る） |
| Edit-Level Repair | 動画モデルを呼ばない。該当区間をチャットで知らせ、編集で直すことにする（Take は作らず、Retry の回数にも数えない）。AUTO MODE で Shot の Take Selection が AUTO かつ Selected Take が無いときは、直す対象の Take を Selected Take にして Rough Cut に載せる（AUTO の編集チェーンが止まらないようにする）。DIRECTOR MODE ではユーザーが Take を選ぶ |

### 11.4 Jev のシャドー実行

- **対象**: Repair Agent（LLM）が戦略を選んだときだけ、同じ質問（候補から 1 つを選ぶ Choice）を Jev にも投げる。ルールや LOCK・Retry Policy で決めたときは呼ばない。Jev の答えは記録するだけで、Repair には使わない。Jev の失敗・タイムアウトも Repair に影響しない
- **送るもの**: Issue の構造化フィールド（`type` / `severity` / `dimension` / `time_range`）と候補の識別子のみ。日本語の自由記述・Character ID・フレーム番号・Brief・画像は送らない
- **バージョン固定**: 既定は `jev-1.13.0`。`jev-latest` などの別名は起動時に拒否する（`jev-<major>.<minor>.<patch>` のみ）。`--jev-config`（`MAM_JEV_CONFIG`）の JSON ファイル（`base_url` / `model` / `timeout_seconds` / `input_cost_per_million_usd`）で上書きできる。未知のキーはエラー。既定値は `base_url` が `https://api.typesafe.ai`、`timeout_seconds` が 10（1 回の呼び出しの上限秒数）、`input_cost_per_million_usd` が 0.042（入力 100 万トークンあたりの USD。出力は無料。下の `repair_shadow` のコストはこの単価で計算するので、合計コストの前提になる）。応答の本文は 1 MiB までしか読まない
- **API キー**: `internal/credential` のプロバイダー `typesafe` に保存する。キーが無いときは呼ばず、何も記録しない
- **記録**: `repair_shadow`（`spec/architecture/er.md`）に、Jev の選択・確率・confidence・Repair Agent の選択・一致の有無・遅延・トークン数・コスト（入力トークン × 単価）・失敗時の理由を 1 呼び出し 1 行で残す。集計（`RepairRepository.ShadowReport`）は一致率（失敗を除く）・confidence の平均と 10 区間の分布（各区間での一致数つき）・平均遅延（失敗を除く）・合計コストを返す。前段選別を有効にするかどうかは、この集計を見て §4.5 の導入手順に従って決める

## 12. 音声（M1 簡易版）の実装（`internal/agent/audiodirector`）

M1 は §5 のうち ElevenLabs の TTS（Dialogue / Narration）とローカル DSP（ffmpeg）のみを実装した。BGM・SE・Foley・Ambience・Lip Sync・音声クローンは M3 で追加した（`model-adapters-m3.md` §14）。M1 の簡易音声から M3 の Audio Director への移行は `model-adapters-m3.md` §14.6。

- **認証**: 設定画面の API キー（`credential.Store` のプロバイダー名 `elevenlabs`）。ヘッダー `xi-api-key`。キーがなければ、台詞行があるときの音声生成は 502（「ElevenLabs の API キーが設定されていません」。Planner の前に確認する）。台詞行が無いときは確認せず、キーが要る層を skip（`SkipError`）して Audio Timeline を保存し 200 を返す
- **TTS**: `POST {base}/v1/text-to-speech/{voice_id}`（既定 base `https://api.elevenlabs.io`、モデル `eleven_multilingual_v2`、日本語対応）。Dialogue と Narration はそれぞれ別の voice ID（既定は ElevenLabs の premade voice）。応答の音声（mp3）をそのままファイルにし、ffmpeg で扱う。429 はレート制限として利用者に伝え、リトライは行わない
- **ミキシング・ラウドネス正規化**: すべてローカルの ffmpeg（外部 API を呼ばない）。TTS の行を `adelay` で Scene の開始時刻に置き、Take 自体の音声と `amix` で 1 本にし、`loudnorm` を 2 パス（測定 → 測定値を渡して線形補正）で −14 LUFS / −1.5 dBTP に揃える。測定値を Audio Timeline に保存する
- **コスト**: M3 以降は Audio Director が TTS を含む音声の呼び出しを見積額で `cost_ledger_entry` に記録する（`model-adapters-m3.md` §14.5）

## 13. Multi-model routing / 14. Audio Director（M3）の実装

M3 の Multi-model routing（§13）と Audio Director の実装（§14）は [model-adapters-m3.md](model-adapters-m3.md) に分けた。

## 改訂履歴

[model-adapters.changelog.md](model-adapters.changelog.md) を参照（本文の行数を増やさないよう別ファイルに分けている。環境構築 spec の Linterly 節）。
