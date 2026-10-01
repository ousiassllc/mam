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

注記: 現在の実装では、Orchestrator Agent と Final QC のモデル割り当て（`orchestrator` / `final_qc`）はどのコードからも参照されない。Orchestrator は Core State Machine の遷移を決定的に進め（Producer 以降の LLM 呼び出しは各 Agent の割り当てに従う）、Final QC は決定的なチェックのみを行う（`spec/components/overview.md`）。この 2 行は将来 LLM 判定を導入するための予約で、設定ファイルでこの 2 つの Agent のモデルを変えても動作は変わらない。

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
- **M1 実装順**: コスト効率と音声同時生成による検証のしやすさを優先し、Kling 3.0 + Seedance 2.0 の2アダプタを先行実装する。Runway Gen-4.5 は M2 で追加した（§9.1、MAX QUALITY のプライマリ）
- OpenAI Sora 2 は候補から除外: API提供が2026-09-24付で終了済みのため

## 4.5 評価・判断の補助（Judge / Repair 用）

Reasoning LLM Adapter にも動画/画像生成 Model Adapter にも属さない、判定精度と判断コストを補う2つの部品。いずれも生成物ではないため Take / Cost Ledger の対象外（§6）。

| 用途 | 採用 | 実行場所 | 入力 | 選定理由 |
|------|------|---------|------|---------|
| Face Identity Similarity（F-13、`spec/spec.md` §12.4） | 顔検出 + 顔埋め込みモデル（ArcFace 系を想定。具体モデルは実装時に確定） | ローカル（外部送信なし） | Take のサンプルフレーム、`character.face_reference_asset_id` の参照顔 | Gemini の定性評価だけでは微妙な顔のズレを見逃す恐れがあり、シリーズ制作で致命的になる。数値化により閾値判定と時系列追跡ができる |
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
画像: imagen-4, nano-banana, flux-2, gpt-image-2
動画: kling-3.0, seedance-2.0, runway-gen-4.5
音声: elevenlabs-tts, elevenlabs-music, elevenlabs-sfx, sync-labs
```

Reasoning LLM Adapter側の呼び出しモデルは Take/Cost Ledger の対象外（生成物ではなく思考プロセスのため）。将来的にLLM呼び出しコストの可視化が必要になった場合は、別テーブル（例: `AGENT_INVOCATION_LOG`）の追加を別途検討する。

## 7. フォールバック / サーキットブレーカー

`spec/requirements/non-functional.md` §6「モデル呼び出し耐性」（プロバイダ単位のタイムアウト・429指数バックオフ・サーキットブレーカー。画像・動画生成ではタイムアウトは再試行せず代替へ、§9.3）は、本書§2〜§4の「代替」列をフェイルオーバー先として実装する。あるプロバイダーのサーキットブレーカーがOPENになった場合、Model/LLM Adapterは同一用途の代替プロバイダーへ自動的にルーティングする。

## 8. 未確定事項・レビュー方針

- 各プロバイダーのSDKバージョン・具体的なエンドポイント・レート制限値は実装フェーズで確定する
- コスト実測値（$/秒、$/画像等）は市場変動が大きいため、`non-functional.md` §6「コスト可視化」機能で継続的に検証し、本書の数値は参考値として扱う
- 動画/画像/LLM市場は数ヶ月単位でモデル改廃が発生するため、本書は半期ごとに見直しレビューを行う（次回レビュー目安: 2027-03）
- Face Identity Similarity の顔検出・埋め込みモデルと推論ランタイム（ONNX Runtime）は M1 で検証済み（§10.1）。残る確認事項は、SFace の学習データの権利関係（配布元リポジトリは重みのライセンス（Apache-2.0）のみを明記し、学習データセットを明記していない）で、商用配布前に法務確認が必要（本アプリはモデルファイルを同梱しない）。閾値は実測で決定する（仕様では固定しない）
- Jev の外部 API 利用は、シャドー実行の結果（一致率・confidence・遅延・コスト）で採否を判断する。`spec/requirements/non-functional.md` §5 の外部送信方針との整合を導入時に確認する

## 2.1 実装（`internal/agent/llm`）

- 設定: Agent ごとの `routes`（`primary` / `fallback` の `provider`+`model`）、`providers`（`base_url`）、`timeout_seconds`（既定 120）、`retry`（`max_attempts` 3 / `base_delay_ms` 1000 / `max_delay_ms` 16000）、`breaker`（`failure_threshold` 5 / `open_seconds` 30）。既定値は §2 の表と同じ割り当てで、`--llm-config`（`MAM_LLM_CONFIG`）の JSON ファイルで上書きできる（未知のキーはエラー）。モデル名は設定値であり Agent のコードには書かない
- API キーは `credential.Store` のプロバイダー名（`anthropic` / `openai` / `google`）で呼び出しごとに読む。設定画面で保存したキーは再起動なしで有効になる
- 再試行: タイムアウト・ネットワーク断・429・5xx を指数バックオフで再試行する。`Retry-After` があれば、待ち時間は現在のバックオフ以上にし（バックオフより短い指示には従わず、長い指示を優先する）、`max_delay_ms`（既定 16 秒）で頭打ちにする（ベンダーが長い待ちを求めても呼び出しを長時間止めない）。認証エラー等の 4xx は再試行しない
- サーキットブレーカー: プロバイダー単位。連続失敗が `failure_threshold` に達すると OPEN になり、`open_seconds` 経過後に 1 回だけ試行（HALF-OPEN）して成功で CLOSED に戻る。OPEN のプロバイダーは呼ばず、代替へ自動でルーティングする（§7）
- 出力は構造化 JSON を要求し、コードフェンスや前置きを除いて検証する。検証に失敗した回答は保存せず、チャットにエラーを通知して再開できる状態に留める

## 9. 画像・動画生成 Model Adapter の実装（`internal/agent/modeladapter`、`internal/agent/generation`）

M1 Issue #7（F-06/F-07/F-11/F-12）の実装仕様。Reasoning LLM Adapter（§2.1）とは別パッケージで、耐障害部品（サーキットブレーカー・指数バックオフ）は `internal/agent/resilience` を両者が共有する。

### 9.1 M1 で実装するプロバイダー

- **画像**: §3 の 4 種すべて（Imagen 4 / Nano Banana / FLUX.2 / GPT Image 2）を実装する。用途→プライマリ/代替は §3 の表どおり（`new_reference` / `edit_reference` / `preview` / `unified_openai`）。Reference Pack の Character/Product/Location は新規生成として `new_reference`（アップロード素材や World State の参照画像がある場合は参照ベース編集として `edit_reference`）、Style/Lighting/Keyframe は `edit_reference` を使う。`prefer_openai` を有効にすると新規参照を `unified_openai` へ回す
- **動画**: Kling 3.0 / Seedance 2.0（M1）と Runway Gen-4.5（M2）の 3 アダプタ（§4）。Runway Gen-4.5 は `POST /v1/text_to_video`（Shot に先頭フレームがなければ）または `POST /v1/image_to_video`（先頭フレームのみ）を非同期タスクとして起動して `GET /v1/tasks/:id` をポーリングし、`X-Runway-Version` ヘッダーを付ける。出力は 720p のみ、長さは 2〜10 秒、ネガティブプロンプトはなく（Avoid は本文に含める）プロンプトは 1000 文字まで、自由な参照画像は取れない。価格はクレジット（12 クレジット/秒、1 クレジット = 0.01 USD）から USD に換算して見積る
- **`model_name` / `provider`**: `take.model_name`・`cost_ledger_entry.provider` は §6 の正規識別子（`imagen-4` / `nano-banana` / `flux-2` / `gpt-image-2` / `kling-3.0` / `seedance-2.0` / `runway-gen-4.5`）で保存する。API のモデル ID（`imagen-4.0-generate-001` など）は設定値で、保存値には使わない。`gpt-image-2` の既定 API モデル ID は、現時点で公開 API として確認できている `gpt-image-1`（`models.gpt-image-2.model` で差し替え可能。公開されたら設定で `gpt-image-2` に切り替える）

### 9.2 設定

`--model-config`（`MAM_MODEL_CONFIG`）の JSON ファイルで既定値を部分的に上書きできる（未知のキーはエラー）。

| キー | 内容 | 既定 |
|---|---|---|
| `vendors.<google\|openai\|flux\|kling\|seedance\|runway>` | `base_url` / `max_concurrency`（同時実行の上限） / `timeout_seconds` | 4 / 4 / 4 / 3 / 3 / 2、120〜900 秒 |
| `models.<正規識別子>` | `model`（API のモデル ID）、`fast_model`、`cost_per_image_usd`、`cost_per_second_usd`（解像度別）。価格は参考値（§8）で見積コストにのみ使う | §3 / §4 の割り当て |
| `images.<purpose>` | `primary` / `fallback`（`model` と `fast`） | §3 の表 |
| `policies.<fast\|balanced\|max_quality>` | `primary` / `fallback`（`model` と `resolution`）、`takes`（Take 数） | FAST: Kling 3.0 720p → Seedance 2.0 480p、2 Take。BALANCED: Seedance 2.0 720p → Kling 3.0 720p、3 Take。MAX QUALITY: Runway Gen-4.5 720p → Kling 3.0 1080p、4 Take |
| `retry` / `breaker` | §2.1 と同じ形式・同じ既定値 | 3 回 / 1000ms〜16000ms、5 回失敗で 30 秒 OPEN |

API キーは `credential.Store` のベンダー名（`google` / `openai` / `flux` / `kling` / `seedance` / `runway`）で呼び出しごとに読む。Kling のキーは `ACCESS_KEY:SECRET_KEY` の形式で保存する。

### 9.3 動作

- **Canonical Shot Spec → モデル固有プロンプト（F-07）**: `Prepare` が Shot Spec を英語のプロンプトへ変換する。モデルごとに対応する長さ（Kling 5/10 秒、Seedance 4〜12 秒。要求を満たす最短の長さ）、First/Last Frame、参照画像の上限（Seedance 9 枚）に合わせて入力を絞る。Negative Prompt を持たないモデルでは本文へ折り込む。プロンプトは英語のみ（`non-functional.md` §9）
- **フェイルオーバー（§7）**: ベンダー単位のブレーカーが OPEN、または再試行しても失敗したとき、同じ Shot Spec を代替モデル向けに再変換して呼ぶ。429・5xx・ネットワーク断のみ同じモデルで再試行し、認証エラー等の 4xx は再試行しない（待ち時間は §2.1 の再試行と同じく、`Retry-After` をバックオフの下限として加味し `max_delay_ms` で頭打ちにする）。タイムアウトは同じモデルでは再試行せず、直ちに代替モデルへフェイルオーバーする（ベンダー側でジョブが継続・課金されている可能性があり、再提出すると二重に課金されるため。LLM の §2.1 は課金を伴う非同期ジョブを持たないためタイムアウトも再試行する）。タイムアウトもブレーカーの失敗に数える。ベンダーがジョブを受け付けた後（状態確認・結果のダウンロード）の 429・5xx・ネットワーク断は、同じジョブ ID への GET だけを上限回数（3 回）まで再試行し、それでも失敗したときはジョブを再提出せず（二重課金回避）代替モデルへ進む
- **ベンダー応答の URL の扱い**: API キーを付けるリクエストは、ベンダーのベース URL と同じスキーム・ポートで、同じホストまたはそのベースホストの親ドメイン配下（例: `api.bfl.ai` → `api.eu.bfl.ai`）の URL にだけ送る。FLUX の `polling_url` がそれ以外を指すときは、キーを送らずエラーにする。生成結果のダウンロード（Sync Labs の `outputUrl` を含む）は https の URL だけを受け付け、`localhost`・ループバック・リンクローカル（クラウドのメタデータサービス `169.254.169.254` など）・プライベートアドレスへは接続しない（ホスト名の解決後のアドレスも検査する。リダイレクト先も同じ規則。プロキシ設定は使わない）。ローカルの常駐アプリがベンダー応答を経由して内部ネットワークを読み出さないための制限である。ベンダー API を呼ぶ既定の HTTP クライアント（LLM・Model Adapter・Audio Director・Jev 共通、`internal/httpclient`）は、別ホストへのリダイレクトと https から http へのリダイレクトを拒否し（独自ヘッダーの API キーを別ホストへ転送しない）、接続・TLS ハンドシェイク・レスポンスヘッダーの待ちにタイムアウトを持つ。生成動画のダウンロード（上限 300 MB）は本文をメモリに載せずアセットディレクトリ配下の一時ファイル（`<assets>/.staging/`）へストリーム書き込みし、保存時にアセットのパスへリネームする（失敗・サイズ超過時は一時ファイルを削除し、異常終了で残った分は起動時に掃除する）。画像は小さいためメモリ上で扱う
- **同時実行数（`non-functional.md` §4）**: ベンダーごとの上限を Model Adapter が持つ。429 を受けると上限を半分（最小 1）にし、成功が上限回数続くごとに 1 ずつ戻す。上限を超える呼び出しはスロットが空くまで待つ。Prompt Candidate Generation（F-08）が Candidate を並列に生成しても、外部 API への同時呼び出し数はこの上限（Runway は既定 2）で抑えられる
- **Generation Orchestrator（F-12）**: Quality Policy でモデル・解像度・Take 数を決め、(1) Shot が主モデルの最長クリップを超え代替モデルなら収まる場合は入れ替え、(2) Shot/Project の予算上限（残額）に Take 数が収まらなければ Take 数を減らし、主モデルが 1 Take も収まらず代替なら収まるなら代替を主にし、(3) 1 Take も収まらなければ 402 で開始しない。決定理由は `prompt_version.model_adapter_output`（`reasons`）に残る。Take は並列に生成し、1 つ以上成功すれば成功とする
- **Prompt Autopilot（F-06）**: Prompt Planner（Reasoning LLM）が Shot の指示と World State・直前 Shot の Spec・Reference Pack から Canonical Shot Spec（JSON、英語）を作る。検証（目的・フレーミングあり、Acting が Shot 内で時系列順、日本語などを含まない）に失敗した回答は保存しない。既存 Spec は、追加指示がある場合だけ改訂し、無ければ再利用する。生成のたびに `prompt_version` が 1 行増え、`shot_spec_snapshot` に保存時点の Spec、`model_adapter_output` に各モデルへ渡す内容（プロンプト・解像度・参照枚数）を記録する
- **Reference Pack（F-11）**: Art Director（Reasoning LLM）が Character/Product/Location/Style/Lighting/Keyframe の各画像のプロンプトを決め、画像 Model Adapter が生成する。Character/Product/Location/Style/Lighting を先に並列生成し、Keyframe は先行する 3 種を入力にして最後に生成する。全画像が揃ったときだけ `shot.shot_spec.references` を置き換え（途中で失敗したら旧 Pack を残し、作成済みファイルも削除する）。Keyframe は動画生成の First Frame になる
- **ジョブ**: 生成・Reference Pack 作成は `job.kind = generate` の非再開ジョブ（課金を伴うため、異常終了後と、シャットダウン（Ctrl+C など）で実行中に中断された場合は、キューに戻さず失敗として扱い、Shot を PLANNING へ戻す）。開始時に Shot を GENERATING にし、同じ Shot の二重実行は 409 とする。進捗は `/ws/shots/:id`、チャットの進捗カードと Take/Keyframe カードで通知する

## 10. AI Video Judge と Face Identity Similarity の実装（`internal/agent/judge`、`internal/facesim`）

M1 Issue #9（F-13）の実装仕様。

### 10.1 モデルと推論ランタイムの検証結果

| 項目 | 採用 | ライセンス | 検証 |
|---|---|---|---|
| 顔検出 | YuNet（`face_detection_yunet_2023mar.onnx`、OpenCV Zoo）。5 点ランドマーク付き | MIT | 本リポジトリの Go 実装の出力が OpenCV `FaceDetectorYN` の出力と一致（同一画像で検出枠・スコアが一致）することを確認 |
| 顔埋め込み | SFace（`face_recognition_sface_2021dec.onnx`、OpenCV Zoo）。112×112 の整列顔から 128 次元、コサイン類似度で照合 | Apache-2.0（重み。学習データの権利は §8） | 同一人物（縮小・明度変更）0.93、別人物 −0.03（各 1 例。閾値は実測で決める） |
| 推論ランタイム | ONNX Runtime 1.23.x の共有ライブラリ（`libonnxruntime.so` / `.dylib` / `onnxruntime.dll`）を `github.com/shota3506/onnxruntime-purego`（MIT、purego 経由）で実行時にロード | MIT（ONNX Runtime 本体、バインディングとも） | `CGO_ENABLED=0` のバイナリのまま推論できることを確認。cgo バインディング（`yalue/onnxruntime_go`）は単一バイナリ・クロスコンパイル（`make build`）と両立しないため不採用 |

- **同梱しない**: モデルファイル（約 39 MB）と ONNX Runtime ライブラリはバイナリに同梱せず、利用者が配置する（`--face-model-dir`（`MAM_FACE_MODEL_DIR`、既定 `<data-dir>/models`）にモデル 2 ファイル、`--onnxruntime-lib`（`MAM_ONNXRUNTIME_LIB`、空ならシステムの標準ライブラリ検索）にランタイム）。`scripts/fetch-face-models.sh` がモデルを取得し SHA-256 を検証する（取得元は OpenCV Zoo のコミットに固定、`sha256sum` または `shasum` で検証、https のみ・タイムアウトと再試行あり、失敗時に一時ファイルを残さない。配置先は引数、`MAM_FACE_MODEL_DIR`、`MAM_DATA_DIR/models` の順で決まる）
- **未配置でも動く**: モデルまたはランタイムが無いとき、起動時に警告ログを残し Face Identity Similarity を無効にする。Take は定性評価のみで判定し、Judge Result の `metrics.face_identity_status` は `unavailable` になる（チャットにも明記する）
- **ローカル実行**: 顔検出・埋め込みはプロセス内で実行し、顔画像はネットワークに出さない。Reasoning LLM（Gemini）には定性評価用のフレームと Reference Pack を送る（`non-functional.md` §5 の外部送信方針の対象。顔の数値化そのものは送らない）

### 10.2 動作

- **フレームサンプリング**: `ffmpeg` / `ffprobe`（PATH）でフレームを抜き出す。数値評価用は `face_sample_fps`（既定 4 fps）で最大 `max_face_frames`（48）枚（長い Take は間引く）、定性評価用は等間隔に最大 `review_frames`（12）枚（`face_sample_fps` で頭打ちになるため、3 秒未満の Take は 12 枚に満たない）。ffmpeg が無いと Judge 自体が無効になる（起動時に警告）
- **顔照合**: 各フレームで顔検出 → 埋め込み → Character の参照顔とのコサイン類似度。1 フレームに複数の顔があるときは、全ペアの類似度が高い順に「1 顔 = 1 Character」で割り当てる。参照顔は、Episode の Character が Series 正本から作られたもの（`origin_id`）ならその正本の `face_reference_asset_id`（F-29。Episode をまたいで同じ顔を基準にする）、Character 自身の `face_reference_asset_id`、Reference Pack の Character 画像のうち名前が一致するもの（同じファイルは 1 枚に数える）。検出できない参照画像は使わず、どの参照画像にも顔が無い Character は測定対象から外す（`face_identity_status = not_applicable`）
- **欠測**: 顔を検出できない（または割り当てられなかった）フレームは `undetected_ratio` にだけ数え、類似度の平均・最小・drift 判定・FAIL 判定に含めない。`undetected_ratio` が `max_undetected_ratio`（0.5）を超える、または検出フレームが `min_detected_frames`（3）未満なら `evaluated = false` とし、その Take は定性評価だけで判定する（`face_identity_drift` は出さない）
- **閾値**: 警告 `warn` と FAIL `fail` は、その Character の参照画像同士の類似度分布から決める（ソースの固定類似度は持たない）。顔が取れた参照が 2 枚以上あり、参照同士の平均類似度 $m$ が `min_calibration_similarity`（0.4）以上のとき、`warn = calibration_warn_scale × m`（既定 0.85）、`fail = calibration_fail_scale × m`（既定 0.65）とし `calibrated = true` にする。適用した閾値は `metrics.face_identity[].warn_threshold` / `fail_threshold` に記録する。参照が 1 枚、または平均が下限未満のときは数値の警告 / FAIL を出さない（`calibrated = false`、`evaluated = false`、閾値は 0）。類似度は記録し、合否は定性評価に任せる。スケールと下限は `--judge-config`（`MAM_JUDGE_CONFIG`、JSON、未知のキーはエラー）で上書きできる
- **drift Issue**: 連続する `min_run_frames`（2）フレーム以上が `warn` を下回った区間を `face_identity_drift` Issue にする（`time_range`・`frames` 付き、`dimension = character_identity`）。区間内に `min_run_frames` フレーム連続で `fail` を下回る箇所があれば `severity = high`、なければ `medium`（警告）。顔が検出できないフレームは区間を切る
- **定性評価**: Reasoning LLM（`AgentVideoJudge`、既定 Gemini → GPT）に、Canonical Shot Spec・前後 Shot の Spec・LOCK 中の評価軸・Reference Pack（`max_reference_images` 枚まで）・サンプルフレームを渡し、`spec/spec.md` §12.2 の評価軸（識別子は `domain.JudgeDimensions`）で JSON を返させる。数値評価が成立している Character については、モデル自身の `face_identity_drift` は採用しない（数値が正本）。`time_range` は Take の長さに収め、ソースのフレーム番号（`frames`）を付ける。時間範囲を読めない Issue は範囲なしで残す
- **判定（決定的ルール、`judge.Decide`）**: (1) LOCK 違反（LOCK 中の評価軸の medium 以上の Issue、または Character Identity が LOCK のときの FAIL 閾値割れ = high の `face_identity_drift`）があれば、モデルの判定・信頼度に関係なく FAIL（`spec/spec.md` §13）。(2) それ以外は、モデルが fail を返す、または severity `high` の Issue があれば FAIL、なければ PASS。警告（medium）だけでは FAIL にしない。`overall_confidence` はモデルの値をそのまま保存する
- **LOCK の解決**: Shot Spec の `character.identity` / `product.geometry` が `locked`、または `lock` テーブルの `(character, <id>, identity)`・`(product, <id>, geometry|label)` が `mode = lock` のとき、Character Identity + Face / Product Geometry / Logo・Text Accuracy が LOCK 扱い
- **Repair Strategy**: モデルの `repair_strategy`（`action` は `spec/spec.md` §13 の 6 種）を保存する。high の `face_identity_drift` があるとき、`action` が無い・`prompt_repair` なら `reference_reinforcement` に置き換え、`increase_face_reference_strength` を `instructions` に足す（`spec/spec.md` §12.4）。PASS で戦略が無い Take は `repair_strategy` なし
- **ジョブ**: Take を作るたびに `job.kind = judge` のジョブを起動する（`payload = {take_id, shot_id}`）。再開可能ジョブ（低コストで再実行できるため、異常終了後は再実行する）。実行中は Shot を `JUDGING` にし（GENERATING 中の Shot の状態は変えない）、`/ws/shots/:id` に `action = judge` の `queued` / `judging` / `done` / `failed` を流す。結果は `judge_result` に追記し（上書きしない）、チャットに PASS/FAIL・Issue・LOCK 違反・数値評価が使えなかった理由を投稿する。同じ Take の Judge が queued/running なら `POST /takes/:id/judge` は 409
- **エラー**: ffmpeg 未導入は「ffmpeg / ffprobe が見つかりません」、API キー未設定・レート制限・タイムアウト・AI 出力不正は生成と同じ分類（`generation.FailureReason`）で通知する。顔モデルの推論エラーは Judge を失敗させず、その Take を定性評価のみで判定する（`face_identity_status = unavailable`）

## 11. Repair Agent と Jev のシャドー実行の実装（`internal/agent/repair`、`internal/agent/jev`）

M1（F-14、Issue #10）で実装する Repair / Retake Loop の判断部分。§4.5 の導入手順に従い、前段選別は有効にせず Jev はシャドー実行のみ行う。

### 11.1 戦略の選択

- **決定的ルール（Jev を経由しない）**: (1) Retry Policy の上限判定（Max retries per Shot・Max cost per Shot・Project 予算。`domain.EffectivePolicy.CanRetry`）、(2) LOCK 違反・Critical constraints（LOCK 違反のある Take は点数に関係なく不合格。`JudgeResult.Accepted`）、(3) 実行できない戦略の除外（`repair.Candidates`: Prompt が LOCK なら Prompt Repair / Partial Regeneration は除外、Reference が無ければ Reference Reinforcement、別モデルが無ければ Different Model、時間範囲を切れない Issue・high の Issue・LOCK 違反があれば Edit-Level Repair を除外。すでに試した戦略は繰り返さないが Full Retake は毎回新しい生成なので残す）、(4) `face_identity_drift` があり Reference Reinforcement が候補にあれば、それを選ぶ（`repair.Rule`）。候補が 1 つのときもルールで決める
- **Repair Agent（Reasoning LLM、`llm.AgentRepair`）**: ルールで決まらないときだけ、残った候補の中から選ぶ。候補外の答えは採用しない。LLM が答えられないとき（キー無し・レート制限・不正な出力）は、Judge の `repair_strategy` が候補にあればそれを、無ければ Full Retake を使う。誰が決めたかは `repair_action.instructions.decided_by`（`rule` / `llm`）に残す
- **不合格の判定**: Judge の結果が `fail`、LOCK 違反あり、または Judge が点数（`judge_result.scores`）を付けていて Minimum acceptance score（総合。Production Template の重みで軸別点数を平均）か軸別の最低点（Production Template の各軸）に届かないときに Repair の対象になる

### 11.2 Retry Policy と Escalation

- Max retries は「新しい Take を作った再生成系の Repair の数」で数える（失敗して Take を作らなかった Repair と Edit-Level Repair は数えない）。次の再生成の見積りコストは Max cost per Shot（`project.shot_budget_ceiling`）と Project の予算上限に対して検査する。Different Model は別モデルの見積りで検査し直す
- 上限に達したら Take を作らず、チャットに Escalation カードを出して人間に判断を求める（Confirmation Level に関係なく出す）。AUTO MODE の自動ループは同じ Judge 結果について 1 度だけ尋ねる。手動の Repair は毎回尋ねる
- **AUTO MODE**: Take の生成・Judge・Repair のジョブが成功するたびに Shot を見て、Shot のすべての Take が Judge 済みで 1 つも合格していなければ、最良の Take（LOCK 違反なし → 総合点が高い → high の Issue が少ない → 新しい方）から Repair を始める。合格した Take が 1 つでもあれば止まる。DIRECTOR MODE では自動で始めず、ユーザーが `POST /takes/:id/repair` を呼ぶ

### 11.3 戦略ごとの再生成

いずれも新しい `take` 行を作り（既存 Take は変えない）、作った Take を Judge する。Shot は再生成中 `REPAIRING` になり、GENERATING / REPAIRING の Shot は二重に始められない。

| 戦略 | 実装 |
|------|------|
| Prompt Repair | Prompt Planner が Issue と修復指示に沿って Shot Spec を修正して再生成する。LOCK された部分は変えない（Prompt が LOCK なら選べない） |
| Partial Regeneration | Prompt Repair に加え、Judge の `preserve`（良かった部分）を保つ。新しい Take は Shot 全体を作り直す（M1）。**M2**: Judge のすべての Issue が `time_range` を持ち、ffmpeg があるときは、それに加えて修復元の Take の問題区間（前後 0.2 秒を足し、0.3 秒未満は広げる）だけを新しい Take の同じ区間に差し替えた Take（`repair.Splicer` = `TakeService.SpliceRegenerated`、`media.Compose` でクロスフェード）を候補として 1 本足し、Judge する。区間の決め方と足さない条件は `spec/spec.md` §13 の「Partial Regeneration の範囲（M1 と M2）」が正 |
| Full Retake | 同じ Shot Spec で新しく生成し直す |
| Different Model | Quality Policy の primary / fallback の**もう一方**のモデルで生成する（既定では FAST は Kling 3.0 ↔ Seedance 2.0、BALANCED は Seedance 2.0 ↔ Kling 3.0、MAX QUALITY は Runway Gen-4.5 ↔ Kling 3.0 の自動フォールバック、§4。Multi-model routing 有効時は §13.4 のとおり）。ユーザーが Video Model を固定していたとき（AUTO 以外）は選べない |
| Reference Reinforcement | Reference に基づく制約を Shot Spec に足し、失敗した種類の Reference を先頭にする。Keyframe は First Frame のまま残し、自由な Reference 画像を受け取れるモデルには他の画像も同時に渡す |
| Edit-Level Repair | 動画モデルを呼ばない。該当区間をチャットで知らせ、編集で直すことにする（Take は作らず、Retry の回数にも数えない）。AUTO MODE で Shot の Take Selection が AUTO かつ Selected Take が無いときは、直す対象の Take を Selected Take にして Rough Cut に載せる（AUTO の編集チェーンが止まらないようにする）。DIRECTOR MODE ではユーザーが Take を選ぶ |

### 11.4 Jev のシャドー実行

- **対象**: Repair Agent（LLM）が戦略を選んだときだけ、同じ質問（候補から 1 つを選ぶ Choice）を Jev にも投げる。ルールや LOCK・Retry Policy で決めたときは呼ばない。Jev の答えは記録するだけで、Repair には使わない。Jev の失敗・タイムアウトも Repair に影響しない
- **送るもの**: Issue の構造化フィールド（`type` / `severity` / `dimension` / `time_range`）と候補の識別子のみ。日本語の自由記述・Character ID・フレーム番号・Brief・画像は送らない
- **バージョン固定**: 既定は `jev-1.13.0`。`jev-latest` などの別名は起動時に拒否する（`jev-<major>.<minor>.<patch>` のみ）。`--jev-config`（`MAM_JEV_CONFIG`）の JSON ファイル（`base_url` / `model` / `timeout_seconds` / `input_cost_per_million_usd`）で上書きできる。未知のキーはエラー
- **API キー**: `internal/credential` のプロバイダー `typesafe` に保存する。キーが無いときは呼ばず、何も記録しない
- **記録**: `repair_shadow`（`spec/architecture/er.md`）に、Jev の選択・確率・confidence・Repair Agent の選択・一致の有無・遅延・トークン数・コスト（入力トークン × 単価）・失敗時の理由を 1 呼び出し 1 行で残す。集計（`RepairRepository.ShadowReport`）は一致率（失敗を除く）・confidence の平均と 10 区間の分布（各区間での一致数つき）・平均遅延・合計コストを返す。前段選別を有効にするかどうかは、この集計を見て §4.5 の導入手順に従って決める

## 12. 音声（M1 簡易版）の実装（`internal/agent/audiodirector`）

M1 は §5 のうち ElevenLabs の TTS（Dialogue / Narration）とローカル DSP（ffmpeg）のみを実装した。BGM・SE・Foley・Ambience・Lip Sync・音声クローンは M3 で追加した（§14）。M1 の簡易音声から M3 の Audio Director への移行は §14.6。

- **認証**: 設定画面の API キー（`credential.Store` のプロバイダー名 `elevenlabs`）。ヘッダー `xi-api-key`。キーがなければ音声生成は 502（「ElevenLabs の API キーが設定されていません」）
- **TTS**: `POST {base}/v1/text-to-speech/{voice_id}`（既定 base `https://api.elevenlabs.io`、モデル `eleven_multilingual_v2`、日本語対応）。Dialogue と Narration はそれぞれ別の voice ID（既定は ElevenLabs の premade voice）。応答の音声（mp3）をそのままファイルにし、ffmpeg で扱う。429 はレート制限として利用者に伝え、リトライは行わない
- **ミキシング・ラウドネス正規化**: すべてローカルの ffmpeg（外部 API を呼ばない）。TTS の行を `adelay` で Scene の開始時刻に置き、Take 自体の音声と `amix` で 1 本にし、`loudnorm` を 2 パス（測定 → 測定値を渡して線形補正）で −14 LUFS / −1.5 dBTP に揃える。測定値を Audio Timeline に保存する
- **コスト**: M3 以降は Audio Director が TTS を含む音声の呼び出しを見積額で `cost_ledger_entry` に記録する（§14.5）

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

決定は `take.model_parameters.routing` に Take ごとに保存する: 方式（`adaptive`: 13.4 で挑戦者に切り替えた / `policy_default`: 比べたが Quality Policy のまま / `fixed`: ユーザーが固定 / `disabled`: 設定で無効）、Quality Policy のプライマリ/代替、選んだプライマリ/代替、理由（1 文）、候補ごとの quality・実績数・平均点・合格率・コスト・cost・fit・score・除外理由（`circuit_open` / `no_backend` / `insufficient_evidence` / `below_minimum_score` / `margin` など）、Shot の性質から決めた軸の重み。実際に使われたモデル（`fell_back`）は従来どおり `model_name` / `fell_back` で分かる。`GET /takes/:id/trace` の「Model routing」に表示する。Take を作らずに失敗したときは Take がないので記録しない（決定理由は `prompt_version.model_adapter_output.reasons` に従来どおり残る）。

### 13.7 設定

`--model-config` の JSON に `routing` を追加する（未知のキーはエラー）。

| キー | 内容 | 既定 |
|---|---|---|
| `routing.enabled` | Multi-model routing を有効にする。偽なら M1 のポリシー別ルーティングのみ | 真 |
| `routing.min_samples` | 実績として使うのに必要な Judge 済み Take 数 | 5 |
| `routing.prior_quality` | 実績が足りないモデルの quality（0〜1） | 0.75 |
| `routing.switch_margin` | 切り替えに必要な score の差 | 0.05 |
| `routing.weights.<fast\|balanced\|max_quality>` | `quality` / `cost` / `fit` の重み（負でなく、合計が正） | 13.3 |
| `routing.candidates.<fast\|balanced\|max_quality>` | `model` と `resolution` の配列（各モデルは動画モデルで、`policies` と同じ検証を受ける） | 13.2 |

## 14. Audio Director（M3）の実装（`internal/agent/audiodirector`）

M3 Issue #25 の仕様。§5 の全機能（TTS・音声クローン / Music / Sound Effects / Lip Sync / Mixing・Loudness）を実装する。

### 14.1 プロバイダーと識別子

| 機能 | 識別子（`cost_ledger_entry.provider`、`audio_timeline` のクリップの `model`） | 呼び出し | 認証（設定画面のキー名） |
|------|------|------|------|
| Dialogue / Narration（TTS） | `elevenlabs-tts` | `POST /v1/text-to-speech/{voice_id}`（モデル `eleven_multilingual_v2`） | `elevenlabs`（ヘッダー `xi-api-key`） |
| 音声クローン | （生成物ではなくコストなし。音声 ID は `voice_clone` に保存） | `POST /v1/voices/add`（multipart。`name` と `files`）。応答の `voice_id` を保存する | `elevenlabs` |
| BGM（Music v2） | `elevenlabs-music` | `POST /v1/music`（`prompt`・`music_length_ms`・`model_id: music_v2`・`force_instrumental: true`）。長さは 3〜600 秒 | `elevenlabs` |
| SE / Foley / Ambience | `elevenlabs-sfx` | `POST /v1/sound-generation`（`text`・`duration_seconds`・`loop`）。長さは 0.5〜30 秒 | `elevenlabs` |
| Lip Sync | `sync-labs` | `POST https://api.sync.so/v2/generate`（multipart。`video`・`audio`・`model`）→ `GET /v2/generate/{id}` を `COMPLETED` までポーリングし、`outputUrl` をダウンロードする（ダウンロードには API キーを送らない） | `synclabs`（ヘッダー `x-api-key`） |

- 長さが範囲外の Music / SE はリクエストを送らずに拒否する。429 はレート制限として利用者に伝え、リトライしない。API キーはエラーメッセージに含めない
- 各ベンダーの呼び出しはインターフェース（`Synthesizer` / `Composer` / `SoundMaker` / `VoiceCloner` / `LipSyncer`）の裏にあり、`audiodirector.Providers` で束ねて `service.Deps.Audio` に渡す。未設定のプロバイダーの層は作らず、Audio Timeline の `skipped` に残す（偽の音は作らない）

### 14.2 Audio Timeline の生成（Scene Emotion と編集テンポ）

1. **Scene の読み取り**: Edit Timeline のクリップを Scene ごとにまとめ、編集後の動画上の開始・終了、クリップ数、Scene の場所・目的・脚本の内容・Shot の目的を集める。編集テンポは Scene 内のクリップの平均の長さから決める（2.5 秒未満 `fast`、5 秒超 `slow`、それ以外 `medium`）
2. **プラン（Planner）**: Reasoning LLM（`llm.AgentAudioDirector`）が Scene ごとに Scene Emotion（感情）・BGM のプロンプト・Ambience のプロンプト・SE / Foley の置き場所（Scene 先頭からの秒数・長さ・プロンプト）を JSON で返す。Quality Policy の範囲外の層、Scene の外・長さ 0.5 秒未満・最大 4 個を超える効果音は捨てる。LLM が未設定のときは規則で作る（BGM はテンポとブリーフの mood、Ambience は Scene の場所から。効果音は置かない）。LLM の出力が使えない（Scene を 1 つも指していない等）ときは **プロバイダーへの支払いの前に** エラーにする
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

MAX QUALITY のみ。話者付きの Dialogue の行（Narration は対象外）が入っているクリップごとに、そのクリップの Take の該当範囲を切り出した動画と、そのクリップの行を本来の時刻に置いた音声（ffmpeg で作る）を Sync Labs に渡し、返った動画を `<project_id>/audio/<edit_timeline_id>/<run>/lipsync-NN.mp4`（assets ディレクトリ直下からの相対パス）に保存して Audio Timeline の `lip_sync` に記録する。書き出し（Export）はそのクリップの映像を Take の代わりにこの動画から切り出す。行がクリップをまたぐ・どのクリップにも入らない場合と、失敗した場合は、そのクリップの Lip Sync だけ諦めて `skipped` に残す（音声は作る）。Sync Labs のキーがなければ、残りのクリップも試さない。

### 14.5 コストと予算（F-22）

- 料金はプロバイダー呼び出しごとの **見積** で `cost_ledger_entry`（`provider` は §14.1 の識別子）に記録する。単価の既定は TTS 1,000 文字あたり $0.30、Music 1 秒あたり $0.02、SFX 1 秒あたり $0.01、Lip Sync は Sync Labs の公開価格（25fps）の 1 秒あたり `lipsync-1.9.0-beta` $0.02・`lipsync-2` $0.04・`sync-3` $0.133。ベンダーの課金は plan・クレジットで実額と異なりうるため、予算管理のための見積である（`actual_cost` は空）。失敗した呼び出しは記録しない
- Project の予算上限（`project.project_budget_ceiling`）から台帳の合計（`COALESCE(actual_cost, estimated_cost)` の和）を引いた残りが、各呼び出しの見積に足りなければその呼び出しは行わず `skipped`（`reason: budget`）に残す。セリフの音声合成の見積の合計が残りを超えるときは、何も呼ばずに 409（`BudgetError`）にする
- Audio Timeline の `estimated_cost` にその実行の見積合計を保存する

### 14.6 M1 の簡易音声からの移行と Branch ごとの版管理

- M1 の簡易音声（`mode` なし、セリフの行だけのクリップ）の Audio Timeline は **移行せずそのまま読める**。`clips` の `layer` が空のクリップは話者の種別（`kind`）から Dialogue / Narration として扱い、Final QC・Export・画面表示は従来どおり動く。スキーマの変更は `branch_id`（#18 で追加済み）のみで、既存データの書き換えは行わない
- `POST /projects/:id/audio/generate` は、以後は常に Audio Director で作り、`mode: "director"` の版を追記する。M1 の簡易版だけを作る経路は残さない（FAST は「セリフ + BGM」まで）
- Audio Timeline の版は `(project_id, branch_id)` ごとに `version_number` を 1 から振り、追記のみ（Edit Timeline と同じ）。Branch の音声は、その Branch の最新の Edit Timeline を元に、Branch 自身の系列へ保存する。Final QC が検査するのは主系列トランクの最新版で、書き出し（`POST /projects/:id/export`）は `branch_id`（省略時=主系列トランク）で指定した Branch の最新の Edit Timeline と Audio Timeline を使う

### 14.7 音声クローン

`POST /projects/:id/audio/voices`（multipart。`speaker`・`sample`（音声ファイル、最大 10 MB）・任意の `name`）で、サンプル音声から ElevenLabs の声を作り、`voice_clone`（Project × 話者で 1 件。再クローンは置き換え）に保存する。以後の音声生成でその話者の Dialogue の行はこの声で話す。クローンはアカウントの声として ElevenLabs 側に作られ、Project の削除では消さない（ベンダー側の削除は設定画面で行う）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-29 | 新規作成 | LLM/画像生成/動画生成/音声の具体的プロバイダー選定をユーザーと合意し、Model Adapter層の対象を確定するため |
| 1.1 | 2026-09-29 | §4.5（Face Identity Similarity / Jev による Repair 前段選別）と §8 の未確定事項を追加 | シリーズ制作での顔のブレ検知の数値化と、Repair 戦略選択の低コスト化を仕様に加えるため |
| 1.2 | 2026-09-29 | §2.1（Reasoning LLM Adapter の実装仕様: 設定・再試行・サーキットブレーカー・出力検証）を追加 | M1「Reasoning LLM Adapter と AUTO パイプライン」（Issue #6）で設定項目と耐障害動作の具体値が未定義だったため |
| 1.3 | 2026-09-29 | §9（画像・動画生成 Model Adapter と Generation Orchestrator の実装仕様）を追加。M1 で実装する画像プロバイダーは §3 の 4 種すべてと決定 | M1「Prompt Autopilot・Model Adapter・Reference Pack・生成オーケストレーション」（Issue #7）で画像プロバイダーの範囲と設定・耐障害動作の具体値が未定義だったため |
| 1.4 | 2026-09-29 | §10（AI Video Judge と Face Identity Similarity の実装仕様、顔モデル・ONNX Runtime のライセンスと単一バイナリ両立の検証結果、閾値のキャリブレーション、LOCK 連動）を追加。§8 の Face Identity Similarity の未確定事項を検証済みの内容に更新 | M1「AI Video Judge（Face Identity Similarity 含む）」（Issue #9）で、埋め込みモデルの商用ライセンス・推論ランタイム・閾値の決め方・欠測の扱いが名称レベルで未確定だったため |
| 1.5 | 2026-09-29 | §11（Repair Agent の戦略選択・Retry Policy・戦略ごとの再生成・Jev のシャドー実行の実装仕様）を追加。Partial Regeneration は M1 では「Judge の `preserve` を保つ Shot 全体の再生成」とし、時間範囲の再生成とつなぎ込みは MVP 2 と確定 | M1「Repair/Retake Loop」（Issue #10）で、Partial Regeneration の M1 での範囲（`spec/spec.md` §27 は MVP 2 に列挙）、決定的ルールと LLM の分担、Jev のシャドー記録項目が未確定だったため |
| 1.6 | 2026-09-30 | §11.3 の Partial Regeneration に M2 の時間範囲つなぎ込み（Shot 全体の Take + 問題区間だけを差し替えた候補 Take、`repair.Splicer`）を追記 | M2「Partial Regeneration」（Issue #16）で、動画モデルが区間だけを生成できない前提での実現方法と新しい Take の扱いを確定するため |
| 1.7 | 2026-09-30 | §10.2 の参照顔に Series 正本の `face_reference` を追加 | M2「Series / Episode Memory」（Issue #21）で、顔同一性を Episode をまたいで Series 正本の顔を基準に計測する契約を定めるため |
| 1.8 | 2026-09-30 | §12（M1 の音声: ElevenLabs TTS とローカル DSP の実装仕様）を追加 | M1「簡易 Editor・簡易 Audio・Final QC・Export」（Issue #14）の実装に合わせるため |
| 1.9 | 2026-09-30 | §13（Multi-model routing の判断基準・切り替え条件・ブレーカーとの関係・トレース・設定）を追加 | M3「Multi-model routing」（Issue #26）が M1 のポリシー別ルーティングとどう違うか、Shot の性質・Judge 実績・コストをどう使うかが未定義だったため、保守的な選択（アンカー方式・実績が無ければ M1 と同じ・対象は動画モデル）で確定した |
| 1.10 | 2026-09-30 | §14（Audio Director の M3 実装: ElevenLabs TTS・音声クローン・Music・SFX / Sync Labs の識別子・呼び出し、Scene Emotion と編集テンポによる Planner、Quality Policy ごとの層、Lip Sync、コストと予算、M1 からの移行と Branch ごとの版管理）を追加。§12 の冒頭を M3 で追加済みに更新 | M3「Audio Director（フル機能）」（Issue #25）の実装に合わせるため |
| 1.11 | 2026-10-01 | §7・§9.3 に、画像・動画生成ではタイムアウトを同じモデルで再試行せず直ちに代替モデルへフェイルオーバーすることを明記 | 実装（`modeladapter.retryable`）はタイムアウト後の再提出による二重課金を避けて再試行しないが、仕様は再試行対象と記していたため（Issue #32） |
| 1.12 | 2026-10-01 | §9.3 に、ジョブ受付後の状態確認・ダウンロードの失敗は同じジョブへの GET だけを再試行し、ジョブを再提出しないことを明記 | 実装が受付後の一時的な失敗でジョブ全体を再提出し二重課金になっていたため、再提出しない挙動に改めた（Issue #41） |
| 1.13 | 2026-10-01 | §9.3 に、ベンダー応答の URL（FLUX の `polling_url`、結果ファイルのダウンロード URL）の検査規則を追加 | 応答の URL へ API キーを送り、http や内部アドレスの URL も取得していたため、キーの漏えいと SSRF の恐れがあった（Issue #43） |
| 1.14 | 2026-10-01 | §9.3 のベンダー応答 URL の扱いに、既定の HTTP クライアントが別ホストへのリダイレクトを拒否することを追記 | Go 標準のリダイレクトは `x-api-key` 等の独自ヘッダーを別ホストへ転送するため（Issue #45） |
| 1.15 | 2026-10-01 | §9.3 に、動画のダウンロードを一時ファイルへのストリーム書き込みにし、保存をリネームにすることを追記 | Take の同時生成時に最大 300 MB の動画を複数メモリへ載せていたため（Issue #48） |
| 1.16 | 2026-10-01 | §10.1 のモデル取得スクリプトの説明に、取得元コミットの固定・検証コマンドの自動選択・配置先の決定順を追記 | スクリプトが `shasum` のみに依存し、取得元が可変ブランチで、`MAM_FACE_MODEL_DIR` を見ていなかったため（Issue #77） |
| 1.17 | 2026-10-01 | §10.2 の顔同一性の閾値から、未校正時の固定値 `warn_similarity` / `fail_similarity` を外した。参照が 1 枚、または分布が使えないときは数値の警告 / FAIL を出さない | 固定値がソース定数のままだったため（Issue #133）。`spec/spec.md` §12.4 と同じ |
| 1.18 | 2026-10-01 | §9.1 に、`gpt-image-2` の既定 API モデル ID が `gpt-image-1` であること（設定で差し替え可）を明記 | 実装の既定値と仕様の選定名が食い違って見えたため（Issue #165） |
| 1.19 | 2026-10-01 | §9.3 のジョブに、シャットダウン（Ctrl+C など）で実行中に中断された非再開ジョブも失敗として扱う（キューに戻さない）ことを明記 | 実装が中断ジョブを queued に戻し、次回起動で再実行して二重課金になり得たため（Issue #194）。コードも失敗扱いに修正 |
| 1.20 | 2026-10-01 | §4.5・§10 の `§12.2`・`§12.4`・`§13` に `spec/spec.md` を付けた | model-adapters.md 自身の §12（音声）・§13（Multi-model routing）と読み違えられたため（Issue #185） |
| 1.21 | 2026-10-01 | §2 と §9.3 の再試行の待ち時間を、「`Retry-After` があれば優先」から「`Retry-After` をバックオフの下限として加味し `max_delay_ms` で頭打ち」に修正 | 実装（`resilience.Do`）は `min(max(バックオフ, Retry-After), MaxDelay)` で待つ。上限なしだと過大な指示で呼び出しが止まるため仕様を実装に合わせた（Issue #195） |
| 1.22 | 2026-10-01 | §12 のコストの記述を、「TTS は `cost_ledger_entry` に記録しない」から「Audio Director が TTS を含む音声の呼び出しを見積額で記録する（§14.5）」に直した | 実装（`audiodirector/cost.go` の `spending.record`）は TTS・Music・SFX・Lip Sync の呼び出しごとに台帳へ書き、§6・§14.5 の記述と矛盾していたため（Issue #217） |
| 1.23 | 2026-10-01 | §14.4 の Lip Sync 動画の保存先を `audio/<edit_timeline_id>/…` から `<project_id>/audio/<edit_timeline_id>/<run>/lipsync-NN.mp4`（assets 直下からの相対パス）に直した | 実装（`audiodirector/director.go` の `rel: path.Join(in.ProjectID, "audio", …)`）は Project ID を先頭に付けており、`er.md` の `<project_id>/exports/…` と同じ流儀のため（Issue #219） |
| 1.24 | 2026-10-02 | §14.6 の「Final QC と書き出しが使うのは主系列トランクの最新版」を、「Final QC は主系列トランクの最新版を検査し、書き出しは `branch_id`（省略時=主系列トランク）の Edit / Audio Timeline を使う」に直した | 書き出しは `branch_id` で指定した Branch のタイムラインを書き出す実装（`spec/api/endpoints.md` §2.6、変更履歴 1.48）で、Final QC だけがトランク固定のため（Issue #244） |
