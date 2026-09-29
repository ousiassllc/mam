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

補足: 上表は各モデルの相対的な強み（Claude=長時間エージェントタスク・構造化出力の一貫性、GPT-5系=クリエイティブライティング・マルチモーダルツール利用、Gemini 3.1 Pro=長尺コンテキスト・マルチモーダル解析・価格対性能）に基づく初期割当であり、実装後のベンチマーク結果に応じて調整可能な設定値として扱う（ハードコード禁止）。

## 3. 画像生成 Model Adapter（Keyframe / Reference Pack）

| 用途 | プライマリ | 代替 | 選定理由 |
|------|-----------|------|---------|
| Character / Product / Location Reference（新規生成、テキスト正確性が必要な素材） | Google Imagen 4 | OpenAI GPT Image 2 | テキストレンダリング精度・製品写実性が高く、ラベル/ロゴ等 Brand Constraints 適合の素材生成に強い |
| Style / Lighting Reference、既存素材の参照ベース編集・差分変更 | Google Nano Banana（Gemini Image） | Imagen 4 | 編集・変換・参照ベース変更・キャラクター一貫性維持に強く、World State継承の再生成に向く |
| 高速反復・低コストプレビュー生成 | Black Forest Labs FLUX.2 | Imagen 4 Fast | オープンウェイトでセルフホスト可能（ベンダーロックイン回避の保険）、生成速度重視のCandidate Generation向き |
| OpenAI系LLM（GPT-5）と認証・請求を統合したい場合 | OpenAI GPT Image 2 | Imagen 4 | 品質ベンチマーク最上位、GPT-5系のAgentと同一ベンダーで運用しやすい |

## 4. 動画生成 Model Adapter（Take生成）

| Quality/Cost Policy | プライマリ | 代替（Different Model） | 選定理由 |
|---------------------|-----------|--------------------------|---------|
| FAST / Economy Mode | Kuaishou Kling 3.0 (Standard) | ByteDance Seedance 2.0 (480p) | 業界最安値クラス。大量SNSショート動画・高頻度撮り直しに向く |
| BALANCED | ByteDance Seedance 2.0 (720p) | Kling 3.0 | ネイティブ音声同時生成でAudio Director側の合成コストを削減。テキスト/画像/音声/動画の最大12参照ファイル入力がReference Pack・World State継承と相性が良い |
| MAX QUALITY / クライアント案件 | Runway Gen-4.5 | Kling 3.0 | 生成間の再現性・一貫性が高くNG率（外れ値）が低い。プロンプト制御力が高くDIRECTOR MODEでの細かい演出指示に向く |

- Repair Agent の **Different Model** 戦略（F-14）は上記3モデル間の自動フォールバックとして実装し、同一 Canonical Shot Spec から別ベンダーへの再試行で品質不足・レート制限の双方をヘッジする
- **M1 実装順**: コスト効率と音声同時生成による検証のしやすさを優先し、Kling 3.0 + Seedance 2.0 の2アダプタを先行実装する。Runway Gen-4.5 はM2以降で追加する（`spec/overview.md` マイルストーンの「1〜2 Video Model Adapter」に対応）
- OpenAI Sora 2 は候補から除外: API提供が2026-09-24付で終了済みのため

## 4.5 評価・判断の補助（Judge / Repair 用）

Reasoning LLM Adapter にも動画/画像生成 Model Adapter にも属さない、判定精度と判断コストを補う2つの部品。いずれも生成物ではないため Take / Cost Ledger の対象外（§6）。

| 用途 | 採用 | 実行場所 | 入力 | 選定理由 |
|------|------|---------|------|---------|
| Face Identity Similarity（F-13、`spec/spec.md` §12.4） | 顔検出 + 顔埋め込みモデル（ArcFace 系を想定。具体モデルは実装時に確定） | ローカル（外部送信なし） | Take のサンプルフレーム、`character.face_reference_asset_id` の参照顔 | Gemini の定性評価だけでは微妙な顔のズレを見逃す恐れがあり、シリーズ制作で致命的になる。数値化により閾値判定と時系列追跡ができる |
| Repair Strategy の前段選別（F-14） | TypeSafe AI Jev（System One API。外部 API、`POST https://api.typesafe.ai/v1/systemone`） | 外部 API | Judge Result の issues（`type` / `severity` / `time_range`）を英語の構造化 JSON にしたもの | Choice で Repair 戦略を選び、確率と confidence を返す。confidence を閾値ゲートに使えるため、低 confidence の場合のみ Repair Agent（LLM）に回して LLM 呼び出しを減らせる |

Jev の利用ルール:

- **テキスト専用**: 画像・動画・音声は入力できない。顔や映像の判定には使わず、Judge が出した Issue の構造化データのみを入力とする
- **前段選別**: Choice で Repair 戦略（§13 の6種）から1つを選ぶ。confidence が閾値以上なら採用、閾値未満なら Repair Agent（Claude Opus 系）へ回す
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
画像: imagen-4, nano-banana, flux-2, gpt-image-2
動画: kling-3.0, seedance-2.0, runway-gen-4.5
音声: elevenlabs-tts, elevenlabs-music, elevenlabs-sfx, sync-labs
```

Reasoning LLM Adapter側の呼び出しモデルは Take/Cost Ledger の対象外（生成物ではなく思考プロセスのため）。将来的にLLM呼び出しコストの可視化が必要になった場合は、別テーブル（例: `AGENT_INVOCATION_LOG`）の追加を別途検討する。

## 7. フォールバック / サーキットブレーカー

`spec/requirements/non-functional.md` §6「モデル呼び出し耐性」（プロバイダ単位のタイムアウト・429指数バックオフ・サーキットブレーカー）は、本書§2〜§4の「代替」列をフェイルオーバー先として実装する。あるプロバイダーのサーキットブレーカーがOPENになった場合、Model/LLM Adapterは同一用途の代替プロバイダーへ自動的にルーティングする。

## 8. 未確定事項・レビュー方針

- 各プロバイダーのSDKバージョン・具体的なエンドポイント・レート制限値は実装フェーズで確定する
- コスト実測値（$/秒、$/画像等）は市場変動が大きいため、`non-functional.md` §6「コスト可視化」機能で継続的に検証し、本書の数値は参考値として扱う
- 動画/画像/LLM市場は数ヶ月単位でモデル改廃が発生するため、本書は半期ごとに見直しレビューを行う（次回レビュー目安: 2027-03）
- Face Identity Similarity の埋め込みモデルは、商用利用可能なライセンスかを確認したうえで選定する。Go 単一バイナリでの推論ランタイム（ONNX Runtime 等）との両立も実装時に検証する
- Jev の外部 API 利用は、シャドー実行の結果（一致率・confidence・遅延・コスト）で採否を判断する。`spec/requirements/non-functional.md` §5 の外部送信方針との整合を導入時に確認する

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-29 | 新規作成 | LLM/画像生成/動画生成/音声の具体的プロバイダー選定をユーザーと合意し、Model Adapter層の対象を確定するため |
| 1.1 | 2026-09-29 | §4.5（Face Identity Similarity / Jev による Repair 前段選別）と §8 の未確定事項を追加 | シリーズ制作での顔のブレ検知の数値化と、Repair 戦略選択の低コスト化を仕様に加えるため |
