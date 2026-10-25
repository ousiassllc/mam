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
動画: kling-3.0, seedance-2.0, runway-gen-4.5
音声: elevenlabs-tts, elevenlabs-music, elevenlabs-sfx, sync-labs
```

`imagen-4` は 2026-08-17 の Imagen 4 の終了で、この集合から外した。過去に保存した Take・Cost Ledger・参照画像（Shot Spec の `references[].model`）・生成テストの履歴には、実際に生成して課金したモデルとして `imagen-4` が残る（書き換えない。表示するだけで、動作を決めない。履歴の `imagen-4` の結果を再実行しようとすると、組み込みのモデルではないので断られる）。**動作を決める設定**だけを起動時（設定画面の読み込み・保存・`--model-config` の固定ファイルも同じ）に読み替える: `models.json` の `images` の `imagen-4` は `nano-banana` に、`fast: true` の `imagen-4` は `nano-banana-lite`（高速な代替。`fast` は外す）に読み、`models.imagen-4` の項目は読み込みのたびに捨てる。ファイルは書き換えない: 設定画面で保存すると `images` と `policies` だけを今の割り当てで書き直し、ほかのキー（`models.imagen-4` を含む）は手書きのままファイルに残る（無視されるだけで害はない）。

利用者が登録した接続先のモデルは `comfyui:<接続先名>/<モデル名>`、`openai-compat:<接続先名>/<モデル名>` で保存する（例: `comfyui:gpu-box/wan22-i2v`）。接続先名・モデル名は作成後に変えられない（Take と Cost Ledger の識別子が指すため。変えるときは削除して作り直す）。見積コストが 0 USD でも Cost Ledger の行は書く。組み込みの識別子は列挙のまま、登録したモデルを足した開いた集合として扱う（[local-models.md](local-models.md) §2）。

ChatGPT のサブスク経由の画像は `gpt-image-2:chatgpt-subscription`（API キー経由の `gpt-image-2` と区別できる識別子。`take.model_name`・`cost_ledger_entry.provider` に制約はないのでマイグレーション不要。見積もり 0 USD で、台帳には 0 円の行を残す）。上の表は組み込みの 7 つで、設定で増える識別子も同じ列に入る。

Reasoning LLM Adapter側の呼び出しモデルは Take/Cost Ledger の対象外（生成物ではなく思考プロセスのため）。将来的にLLM呼び出しコストの可視化が必要になった場合は、別テーブル（例: `AGENT_INVOCATION_LOG`）の追加を別途検討する。

## 7. フォールバック / サーキットブレーカー

`spec/requirements/non-functional.md` §6「モデル呼び出し耐性」（プロバイダ単位のタイムアウト・429指数バックオフ・サーキットブレーカー。画像・動画生成ではタイムアウトは再試行せず代替へ、§9.3）は、本書§2〜§4の「代替」列をフェイルオーバー先として実装する。あるプロバイダーのサーキットブレーカーがOPENになった場合、Model/LLM Adapterは同一用途の代替プロバイダーへ自動的にルーティングする。 経路の全モデルがブレーカー OPEN でスキップされたときだけ、その呼び出し全体が `ErrCircuitOpen`（「待って再実行」）になる。プライマリがスキップされ、代替が別の理由（拒否・タイムアウトなど）で失敗したときは、代替の失敗理由で通知する。

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
- **`model_name` / `provider`**: `take.model_name`・`cost_ledger_entry.provider` は §6 の正規識別子（`nano-banana` / `nano-banana-lite` / `flux-2` / `gpt-image-2` / `kling-3.0` / `seedance-2.0` / `runway-gen-4.5`）で保存する。API のモデル ID（`gemini-nano-banana-2.1` など。既定は §9.2 の表）は設定値で、保存値には使わない。`gpt-image-2` の既定 API モデル ID は `gpt-image-2`（`gpt-image-1` は 2026-10-23 に終了した）。リクエストは OpenAI の公式仕様（`openai-openapi` の `CreateImageRequest` / `CreateImageEditRequest`）に合わせる: 生成は `POST /v1/images/generations` の JSON（`model`・`prompt`・`size`・`quality`・`n`）、参照画像つきは `POST /v1/images/edits` の multipart（`model`・`prompt`・`size`・`quality`・`image[]`）。`gpt-image-2` では `input_fidelity`（常に高精細で処理され、API が変更を許さない）と `response_format`（GPT Image は常に base64 で返す）を送らない。`quality` は `auto` にせず、**品質プロファイル**（`models.gpt-image-2.quality`: `low` / `medium` / `high`。既定 `medium`。§9.2）を毎回明示して送る（`auto` ではモデルが品質を選び、1 枚の価格を前もって決められないため）。サイズは Brief の縦横比から `1536x1024` / `1024x1536` / `1024x1024`（16 の倍数・比 3:1 以内・画素数の範囲を満たす）に決める
- **ローカル・代替モデル（Issue #831）**: 登録した ComfyUI（画像・動画。`POST /prompt` と `/ws` の進捗、取り消しは自分の `prompt_id` だけ）と OpenAI 互換（画像のみ。`/v1/images/generations`・`/v1/images/edits`）の接続先のモデルを、組み込みと同じ用途・Quality Policy の割り当て、フェイルオーバー、ブレーカー（接続先ごとに `endpoint:<名>`）、同時実行数（既定 1）に載せる。識別子は §6 のとおり。保存した接続先・モデルは再起動なしで反映する。詳細は [local-models.md](local-models.md)
- **ChatGPT のサブスク経由の画像（非公式、`internal/agent/modeladapter/chatgptimage`）**: `gpt-image-2:chatgpt-subscription`。Codex のエンドポイントの `image_generation` ツールを、画像専用のログイン（Codex の公開クライアント）のトークンで呼ぶ。参照画像は `input_image` で渡せる。**既定 OFF**で、OFF のあいだは外部への呼び出しを一切しない。ON にするには設定画面でリスクを確認する。選択肢（設定画面・MCP）に出るのは ON のあいだだけで、すでに割り当ててあって OFF になった場合は呼ばずに代替モデルへ回る。専用のベンダー `chatgpt-subscription`（ブレーカー・同時実行数 1）を持つ。呼び出しの失敗は §9.3 と同じ再試行・代替の対象で、無音が 180 秒続くと中止する。設計は `spec/architecture/chatgpt-signin.md` §5.2

### 9.2 設定

`--model-config`（`MAM_MODEL_CONFIG`）の JSON ファイルで既定値を部分的に上書きできる（未知のキーはエラー）。指定しないときは、データディレクトリの `models.json` があれば起動時に同じ規則で読む。全体の設定画面（`GET /settings`）の「画像・動画モデル」は、この `models.json` の `images`（用途ごとの主・代替モデルと `fast`）と `policies`（Quality Policy ごとの主・代替モデル・解像度・`takes`）だけに、既定と違う割り当てを保存する（既定と同じものは書かず、ほかのキーは残し、書く前に起動時と同じ検査を通して通らなければ 422 で何も書かない。選べるのは、画像は画像バックエンドのあるモデル、動画は `Capabilities` が対応する解像度との組み合わせ。保存した割り当ては次の起動から効く）。`--model-config` で指定したファイルは読み取り専用で、画面から書かない。`fast` を選べるのは `models.<識別子>.fast_model` を設定したモデルだけで、既定では組み込みのどのモデルにも `fast_model` はない。参照画像を使えない画像モデル（`AcceptsImageInputs` が偽。組み込みにはなく、`accepts_image_inputs` が偽の登録モデルだけ）で生成するときは、主モデルでも代替モデルでも参照画像を渡さず Prompt だけで作る（`Adapter.GenerateImage`。ログに警告が出るだけで、呼び出し側へのエラーにはならない）。

| キー | 内容 | 既定 |
|---|---|---|
| `vendors.<google\|openai\|flux\|kling\|seedance\|runway>` | `base_url` / `max_concurrency`（同時実行の上限） / `timeout_seconds` | 4 / 4 / 4 / 3 / 3 / 2、120〜900 秒 |
| `models.<正規識別子>` | `model`（API のモデル ID）、`fast_model`、`cost_per_image_usd`、`cost_per_second_usd`（解像度別）、`quality`（`gpt-image-2` だけ。`low` / `medium` / `high`。品質プロファイル。下の「GPT Image 2 の品質プロファイルと価格」）。価格は参考値（§8）で見積コストにのみ使う（GPT Image 2 の `actual_cost` は応答の `usage` から計算する） | §3 / §4 の割り当て。`quality` は `medium` |
| `images.<purpose>` | `primary` / `fallback`（`model` と `fast`） | §3 の表 |
| `policies.<fast\|balanced\|max_quality>` | `primary` / `fallback`（`model` と `resolution`）、`takes`（Take 数） | FAST: Kling 3.0 720p → Seedance 2.0 480p、2 Take。BALANCED: Seedance 2.0 720p → Kling 3.0 720p、3 Take。MAX QUALITY: Runway Gen-4.5 720p → Kling 3.0 1080p、4 Take |
| `endpoints.<名>` / `local_models.<接続先>/<名>` | 登録した接続先（`kind`・`base_url`・`max_concurrency`・`timeout_seconds`）とそのモデル（ワークフロー・割り当て表・動画の長さと解像度・価格）。`images`・`policies` の `model` には `<kind>:<接続先>/<名>` を書ける。接続先の API キーは `credential.Store` の `endpoint:<名>`（任意）に置く。スキーマは [local-models.md](local-models.md) §3 | 空（価格 0、同時実行 1、タイムアウト ComfyUI 3600 秒・互換 900 秒） |
| `prefer_openai` | トップレベルの真偽値。`true` で新規参照（`new_reference`）を `unified_openai` へ回す（§3） | `false` |
| `retry` / `breaker` | §2.1 と同じ形式・同じ既定値 | 3 回 / 1000ms〜16000ms、5 回失敗で 30 秒 OPEN |

API キーは `credential.Store` のベンダー名（`google` / `openai` / `flux` / `kling` / `seedance` / `runway`）で呼び出しごとに読む。Kling のキーは `ACCESS_KEY:SECRET_KEY` の形式で保存する。Kling の本文と公式ドキュメントとの突き合わせ、旧経路が `kling-v3` を受け付けるかの未検証は [kling-api.md](kling/kling-api.md)。

既定の API モデル ID と見積価格（`cost_per_image_usd`。2026-10-25 に公式の料金表で確認。価格は参考値で、見積コストにのみ使う）:

| 識別子 | 既定の API モデル ID | `cost_per_image_usd` | 出どころ |
|---|---|---|---|
| `nano-banana` | `gemini-nano-banana-2.1` | 0.0336 | [Gemini API pricing](https://ai.google.dev/gemini-api/docs/pricing)「Nano Banana 2.1」Standard: 1K 画像（1120 トークン × $30 / 100 万トークン）。`imageSize` を送らない既定が 1K。2K は 0.0504、4K は 0.113 |
| `nano-banana-lite` | `gemini-3.1-flash-lite-image` | 0.0336 | 同「Gemini 3.1 Flash Lite Image」Standard: 1K 画像（1K のみ対応） |
| `flux-2` | `flux-2-pro` | 0.03 | [BFL の料金表](https://bfl.ai/pricing)「FLUX.2 [pro]」: 生成画像の最初の 1 メガピクセルが $0.03（参照画像は 1 メガピクセルごとに $0.015 加算。mam の出力サイズ 1344×768 などは 1 メガピクセル未満）。参照画像つきの呼び出しではこの値より高くなる |
| `gpt-image-2` | `gpt-image-2` | 未設定。`quality` とサイズで決まる（既定 `medium` で 1536×1024 / 1024×1536 は 0.041、1024×1024 は 0.053。下の表）。`cost_per_image_usd` を書くとそれが優先される | [OpenAI の料金表](https://developers.openai.com/api/docs/pricing)（画像トークン: 出力 $30 / 100 万トークン、入力は画像 $8・テキスト $5）と、[画像生成ガイド](https://developers.openai.com/api/docs/guides/image-generation)の「Calculating costs」の GPT Image 2 の表（2026-10-25 に取得）。下の「GPT Image 2 の品質プロファイルと価格」 |

#### GPT Image 2 の品質プロファイルと価格（Issue #843）

`gpt-image-2` の 1 枚の価格は画像出力トークン数（`quality` とサイズで変わる）で決まる。`quality: auto` ではモデルが品質を選び、見積を前もって決められないため、mam は**品質プロファイルを設定として選べる**ようにし、選んだ品質を**毎回明示して**送る（`POST /v1/images/generations` の `quality`、`POST /v1/images/edits` の `quality` フィールド。`auto` は設定できない）。プロファイルは `models.json` の `models.gpt-image-2.quality`（`low` / `medium` / `high`。未知の値と `gpt-image-2` 以外のモデルへの指定は起動時の検査で拒否する）。書かない既存の設定ファイルは既定のプロファイルになる。設定画面にこの項目は出さない（設定画面の「画像・動画モデル」は §9.2 冒頭のとおり `images` と `policies` だけを書く。画面への追加は別の Issue で行う）。

- **既定は `medium`**: 参照画像（Character / Product / Location / Style）の質を落とさず、`high` の約 1/4 の価格（1536×1024 で $0.041 対 $0.165）に収める中間である。`low`（$0.005）は下書き向けで、参照画像の一貫性に響くおそれがあり、`high` は予算の消費が大きい。Issue #843 の例示が `medium` で、ほかに既定を示す記述は無い。送る品質と見積が同じプロファイルから出るため、見積が実際の請求と食い違う主因（`auto` でモデルが品質を選ぶこと）が無くなり、予算の確認が 0 や代替モデルの価格に落ちない（出力トークン分の見積。入力分は下の未検証①）
- **見積（`estimate`）= プロファイルの 1 枚の価格（サイズ別）**: Cost Ledger の `estimated_cost` と生成テストの単価は、生成する縦横比のサイズの価格。予算の確認（`Adapter.EstimateImage`）は用途の主・代替のうち高いほうで、サイズは最も高いもの（1024×1024）を使う。Brief の縦横比と OpenAI のサイズの対応は `aspectsize.OpenAI`（16:9 → 1536×1024、9:16 → 1024×1536、1:1 → 1024×1024）
- **実コスト（`actual_cost`）**: 応答の `usage`（`input_tokens_details` のテキスト・画像の入力トークンと `output_tokens`）に、下の単価（テキスト入力 $5、画像入力 $8、出力 $30、いずれも 100 万トークンあたり）を掛けて計算し、Cost Ledger の `actual_cost` に記録する。`usage` が無い応答と、`gpt-image-2`（日付つきのスナップショットを含む）以外の API モデル ID を設定した場合は、価格が表に無いので `actual_cost` は空（NULL）で、見積が集計に使われる
- **API モデル ID を別のものにした場合**: 下の表は `gpt-image-2` の値で、ほかのモデルの価格は推測しない。`gpt-image-2` 以外の API モデル ID に変えたときは、`cost_per_image_usd` を書かない限り見積は 0（価格不明）になる

| `quality` | 1024×1024 | 1024×1536（9:16） | 1536×1024（16:9） |
|---|---|---|---|
| `low` | $0.006 | $0.005 | $0.005 |
| `medium`（既定） | $0.053 | $0.041 | $0.041 |
| `high` | $0.211 | $0.165 | $0.165 |

出どころ: OpenAI「Image generation」ガイドの Cost and latency「Calculating costs」の GPT Image 2 の表（<https://developers.openai.com/api/docs/guides/image-generation>、2026-10-25 に取得）。この表は画像の**出力トークンの価格のみ**で、入力（プロンプトのテキストトークン、参照画像つきの編集では画像の入力トークン）は含まない。トークン単価は料金表の「Image generation models」Standard の `gpt-image-2`（<https://developers.openai.com/api/docs/pricing>、2026-10-25 に取得）: 画像 入力 $8.00 / 出力 $30.00 / キャッシュ入力 $2.00、テキスト 入力 $5.00 / キャッシュ入力 $1.25（100 万トークンあたり）。モデルのページ（<https://developers.openai.com/api/docs/models/gpt-image-2>）にはサイズ別の価格は無い。mam が送る 3 つのサイズは表にすべてあり、補間や推測した値は無い。

未検証: ① 見積は出力のみで、入力トークンの分（参照画像つきの `/v1/images/edits` では画像入力 $8 / 100 万トークンが加わる）を含まない。実コストは `usage` から入力も含めて計算するので、編集では実コストが見積を超えることがありうる。② 実際の `gpt-image-2` の Images API 応答に `usage`（`input_tokens` / `output_tokens` / `input_tokens_details`）が付くか、`quality` を明示したときの請求が上の表と一致するかは、実アカウントでの呼び出しで確かめていない（公式の OpenAPI は `usage` を `gpt-image-1` のみと書き、ガイドは「応答の `usage` でトークン消費を測る」と書く。テストは `usage` つきの偽応答で計算だけを確かめる）。③ 料金表は 2026-10-25 時点の値で、半期ごとの見直し（§8）で取り直す。

### 9.3 動作

- **Canonical Shot Spec → モデル固有プロンプト（F-07）**: `Prepare` が Shot Spec を英語のプロンプトへ変換する。モデルごとに対応する長さ（Kling 5/10 秒、Seedance 4〜12 秒。要求を満たす最短の長さ）、First/Last Frame、参照画像の上限（Seedance 9 枚）に合わせて入力を絞る。Negative Prompt を持たないモデルでは本文へ折り込む。プロンプトは英語のみ（`non-functional.md` §9）。プロンプトの長さの上限（文字数は rune）は、Kling 3.0 が本文 2400 文字・ネガティブプロンプト 400 文字（Avoid の項目を `, ` で連結して 400 文字で切る）、Runway Gen-4.5 が 1000 文字、ネガティブプロンプトを持たないその他のモデル（Seedance 2.0 など）が 2400 文字。超えるときは、末尾の Constraints 文（ネガティブプロンプトを持たないモデルでは続く「Avoid:」文も）を丸ごと残し、Shot の描写（本文）の末尾側を削って収める（LOCK 由来の制約・ブランド制約を生成に届けるため）。末尾の文だけで上限を超えるときは、その末尾の文を上限で切る
- **フェイルオーバー（§7）**: ベンダー単位のブレーカーが OPEN、または再試行しても失敗したとき、同じ Shot Spec を代替モデル向けに再変換して呼ぶ。429・5xx・ネットワーク断のみ同じモデルで再試行し、認証エラー等の 4xx は再試行しない（待ち時間は §2.1 の再試行と同じく、`Retry-After` をバックオフの下限として加味し `max_delay_ms` で頭打ちにする）。タイムアウトは同じモデルでは再試行せず、直ちに代替モデルへフェイルオーバーする（ベンダー側でジョブが継続・課金されている可能性があり、再提出すると二重に課金されるため。LLM の §2.1 は課金を伴う非同期ジョブを持たないためタイムアウトも再試行する）。タイムアウトもブレーカーの失敗に数える。ベンダーがジョブを受け付けた後（状態確認・結果のダウンロード）の 429・5xx・ネットワーク断は、同じジョブ ID への GET だけを再試行する。出力は課金済みで、短い障害で失うと取り戻せないため、回数の上限は置かず、呼び出し全体のタイムアウトまで続ける（待ち時間は状態確認の間隔（既定 5 秒）から倍々に増やして 1 分で頭打ちにし、`Retry-After` がそれより長ければ従う。ただし 1 分を超えない）。タイムアウトに達してもなお失敗していたときは、ジョブを再提出せず（二重課金回避）代替モデルへ進む。Sync Labs（`model-adapters-m3.md` §14.1）のポーリング・ダウンロードも同じ規則で、`Retry-After` と呼び出し全体のタイムアウト（Lip Sync の `timeout`）まで再試行する。ただし結果ファイルの保存先（ステージングディレクトリ）の作成・書き込み・同期の失敗（ディスクフル・権限など、ベンダーではなくこのマシンの問題）は再試行せず、ベンダーの一時障害としても数えない（ダウンロードを期限まで繰り返さず、ブレーカーも開かない）
- **ベンダー応答の URL の扱い**: API キーを付けるリクエストは、ベンダーのベース URL と同じスキーム・ポートで、同じホストまたはそのベースホストの親ドメイン配下（例: `api.bfl.ai` → `api.eu.bfl.ai`）の URL にだけ送る。FLUX の `polling_url` がそれ以外を指すときは、キーを送らずエラーにする。生成結果のダウンロード（Sync Labs の `outputUrl` を含む。登録した接続先の例外は次の項目）は https の URL だけを受け付け、公開インターネットのアドレス以外へは接続しない（許可リスト型。`localhost`、ループバック、リンクローカル（クラウドのメタデータサービス `169.254.169.254` など）、プライベート、未指定、マルチキャストに加え、IANA の特殊用途範囲 — キャリアグレード NAT `100.64.0.0/10`・`0.0.0.0/8`・ドキュメント用・ベンチマーク用・予約済みクラス E・6to4 リレー anycast・Teredo など — は全て拒否する。IPv4 互換・6to4・NAT64 の IPv6 アドレスは埋め込まれた IPv4 アドレスで判定する。ホスト名の解決後のアドレスも検査する。リダイレクト先も同じ規則。プロキシ設定は使わない）。ローカルの常駐アプリがベンダー応答を経由して内部ネットワークを読み出さないための制限である。ベンダー API を呼ぶ既定の HTTP クライアント（LLM・Model Adapter・Audio Director・Jev 共通、`internal/httpclient`）は、別ホストへのリダイレクトと https から http へのリダイレクトを拒否し（独自ヘッダーの API キーを別ホストへ転送しない）、接続・TLS ハンドシェイク・レスポンスヘッダーの待ちにタイムアウトを持つ。ベンダーの JSON 応答は 64 MiB まで読み、超えると失敗扱いにする（画像は base64 で JSON に入って返るため）。生成動画のダウンロード（上限 300 MB）は本文をメモリに載せずアセットディレクトリ配下の一時ファイル（`<assets>/.staging/`）へストリーム書き込みし、保存時にアセットのパスへリネームする（失敗・サイズ超過時は一時ファイルを削除し、異常終了で残った分は起動時に掃除する）。画像は小さいためメモリ上で扱う
- **登録した接続先（ローカルモデル）の URL の扱い**: 組み込みベンダーの規則をゆるめるのは、登録した接続先の `base_url` と**同じスキーム・ホスト・ポート**の URL に対してだけで、http・`localhost`・プライベート・`100.64.0.0/10` へ接続できる。それ以外の URL（第三者ベンダーの応答 URL を含む）は上の規則のまま拒否する。ホスト違いと https から http へのリダイレクトは拒否し、API キーは接続先の `base_url` 配下にだけ付ける。**リンクローカル（`169.254.0.0/16`、`fe80::/10`）は登録した接続先でも常に拒否**し、判断は接続時に解決した IP（ダイヤラの `Control`）で行う（DNS リバインディング対策。`local-models.md` §6）
- **同時実行数（`non-functional.md` §4）**: ベンダーごとの上限を Model Adapter が持つ。429 を受けると上限を半分（最小 1）にし、成功が上限回数続くごとに 1 ずつ戻す。上限を超える呼び出しはスロットが空くまで待つ。Prompt Candidate Generation（F-08）が Candidate を並列に生成しても、外部 API への同時呼び出し数はこの上限（Runway は既定 2）で抑えられる
- **Generation Orchestrator（F-12）**: Quality Policy でモデル・解像度・Take 数を決め、(1) Shot が主モデルの最長クリップを超え代替モデルなら収まる場合は入れ替え、(2) Shot/Project の予算上限（残額）に Take 数が収まらなければ Take 数を減らし、主モデルが 1 Take も収まらず代替なら収まるなら代替を主にし、(3) 1 Take も収まらなければ 402 で開始しない。決定理由は `prompt_version.model_adapter_output`（`reasons`）に残る。Take は並列に生成し、1 つ以上成功すれば成功とする
- **Prompt Autopilot（F-06）**: Prompt Planner（Reasoning LLM）が Shot の指示と World State・直前 Shot の Spec・Reference Pack から Canonical Shot Spec（JSON、英語）を作る。検証（目的・フレーミングあり、Acting が Shot 内で時系列順、日本語などを含まない）に失敗した回答は保存しない。既存 Spec は、追加指示がある場合だけ改訂し、無ければ再利用する。生成のたびに `prompt_version` が 1 行増え、`shot_spec_snapshot` に保存時点の Spec、`model_adapter_output` に各モデルへ渡す内容（プロンプト・解像度・参照枚数）を記録する
- **Reference Pack（F-11）**: Art Director（Reasoning LLM）が Character/Product/Location/Style/Lighting/Keyframe の各画像のプロンプトを決め、画像 Model Adapter が生成する。Character/Product/Location/Style/Lighting を先に並列生成し、Keyframe は先行する 3 種を入力にして最後に生成する。全画像が揃ったときだけ `shot.shot_spec.references` を置き換え（途中で失敗したら旧 Pack を残し、作成済みファイルも削除する）。Keyframe は動画生成の First Frame になる。画像モデルへ入力として渡す参照画像は、Brief に添付したアップロード素材も、World State の参照（Character の顔・体、Product の形・ラベル、Location のレイアウト）も、種類ごとに読み込めた先頭の 4 枚までで、5 枚目以降は通知なしで使われない。1 枚 20 MiB を超える保存済み画像は入力にせず、警告ログを残すだけで除外する（AI Video Judge に渡す参照画像も同じ 20 MiB で、超えるものは黙って渡さない）
- **ジョブ**: 生成・Reference Pack 作成は `job.kind = generate` の非再開ジョブ（課金を伴うため、異常終了後と、シャットダウン（Ctrl+C など）で実行中に中断された場合は、キューに戻さず失敗として扱い、Shot を PLANNING へ戻す）。開始時に Shot を GENERATING にし、同じ Shot の二重実行は 409 とする。進捗は `/ws/shots/:id`、チャットの進捗カードと Take/Keyframe カードで通知する

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

- **フレームサンプリング**: `ffmpeg` / `ffprobe`（PATH）でフレームを抜き出す。数値評価用は `face_sample_fps`（既定 4 fps）で最大 `max_face_frames`（48）枚（長い Take は間引く）、定性評価用は等間隔に最大 `review_frames`（12）枚（`face_sample_fps` で頭打ちになるため、3 秒未満の Take は 12 枚に満たない）。抽出するフレームは幅を数値評価用が `face_max_width`（既定 1280 px）、定性評価用が `review_max_width`（既定 768 px）に収める（超える分は縮小する）。ffmpeg が無いと Judge 自体が無効になる（起動時に警告）
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
| Different Model | Quality Policy の primary / fallback の**もう一方**のモデルで生成する（既定では FAST は Kling 3.0 ↔ Seedance 2.0、BALANCED は Seedance 2.0 ↔ Kling 3.0、MAX QUALITY は Runway Gen-4.5 ↔ Kling 3.0 の自動フォールバック、§4。Multi-model routing 有効時は `model-adapters-m3.md` §13.4 のとおり）。ユーザーが Video Model を固定していたとき（AUTO 以外）は選べない |
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

