# API 仕様

HALT の3ルートパターン（`spec/architecture/overview.md` §7）に従う。ページルート/アクションルートは Gin が HTML（フルページ or フラグメント）を返し、`/api/v1` は Huma が JSON（OpenAPI 3.1 自動生成、`/api/v1/openapi.json`）を返す。JSON API は主に Lit Web Components（`mam-video-player` / `mam-timeline-editor` / `mam-take-comparator`）向け。

## 0. 認証・共通仕様

- 単一ユーザー・ループバック限定（`127.0.0.1`）のため外部向け認証は行わない。すべてのリクエストに CSRF トークン（meta タグ由来）を付与する（`spec/requirements/non-functional.md` §5）
- 外部生成モデル API のキーはサーバー側（`agent/model_adapter`）でのみ保持し、フロントエンドには一切露出しない
- エラーレスポンスは HALT の方針（`spec/architecture/overview.md` 参照元 `skill://halt`）に従う:
  - ページ/アクションルートのバリデーションエラー（422）: フォームをエラー付きで再レンダリング
  - ビジネスエラー（403/409/423等）: OOBスワップでトースト表示
  - APIルートのエラー: Huma による RFC 7807 Problem Details
- 共通ビジネスエラーコード:
  | HTTPステータス | 意味 |
  |---|---|
  | 400 | multipart の本文が読み取れない（multipart ではない・途中で切断された） |
  | 413 | リクエスト本文が上限を超えた（`MaxBody` ミドルウェアが 1MiB。`readMultipart` を呼ぶアップロードルートの `multipart/form-data` の本文だけが各上限で、アップロードルートでも multipart 以外（urlencoded など）の本文は 1MiB のまま。Content-Type を `multipart/form-data` にしてもそれ以外のルートの上限は外れない。宣言された長さが上限を超える本文は読まずに 413） |
  | 402 | Project/Shot budget ceiling 超過（F-22） |
  | 403 | Brand/Safety Constraint 違反、または LOCK 対象への変更試行 |
  | 409 | 対象 Shot が既に生成中（多重実行防止） |
  | 423 | LOCK されたパラメータ/State への更新要求 |
  | 429 | 外部モデル API（Model Adapter・Reasoning LLM・ElevenLabs 等）のレート制限。同期で応答できる経路では `service.ErrRateLimited`（`AIError` が 429 を包んでいれば一致）が 429 になる。生成ジョブ内の 429 はチャットの失敗理由として通知する |

## 1. ページルート（Gin, HX-Request 分岐）

| Method | Path | 概要 | 返却（通常/HTMX） |
|---|---|---|---|
| GET | `/` | `GET /projects` と同じ（プロジェクト一覧。ルート URL の入口） | フルページ / プロジェクト一覧フラグメント |
| GET | `/healthz` | ヘルスチェック。JSON `{"ok": bool, "database": "ok"\|"unavailable"}`。DB への Ping が失敗すると `ok:false`・`database:"unavailable"` で 503（それ以外は 200）。`GET /api/v1/health` と同じ結果を返す | JSON（HX 分岐なし） |
| GET | `/static/*filepath` | ビルド済みフロントエンド資産（`app.js`・`app.css` 等）。`HEAD` も可。埋め込みまたは `--static-dir` の内容を配信する | ファイル（HX 分岐なし） |
| GET | `/projects` | プロジェクト一覧・新規作成導線 | フルページ / プロジェクト一覧フラグメント |
| GET | `/projects/:id` | Chat Workspace（標準画面。`chat_thread` + `chat_composer`。Take カード内に `mam-video-player` を埋め込み、履歴は `GET /projects/:id/messages` と同じ内容を初期表示） | フルページ / ワークスペースフラグメント |
| GET | `/projects/:id/messages` | チャット履歴のフラグメント取得。クエリ `after`（メッセージID、省略時=全履歴）で新着のみ取得。応答済みになったカードは `hx-swap-oob` で差し替え、Project 状態バッジも同梱する | フラグメントのみ |
| GET | `/projects/:id/messages/:messageId` | 1 枚のカードの現在の描画（進捗カードの自動更新・応答済み Approval カードの再描画用） | フラグメントのみ |
| GET | `/projects/:id/assets/:assetId` | アップロード済みアセットの配信（当該 Project のアセットのみ。`X-Content-Type-Options: nosniff`、`Content-Security-Policy: sandbox`） | ファイル |
| GET | `/projects/:id/brief` | Creative Brief の確認・編集ページ（補助ビュー。Brief は通常チャットで作成する、F-01） | フルページ / フォームフラグメント |
| GET | `/projects/:id/scenes` | Scene / Shot / Take 階層（先頭 Scene を選択。Scene がなければ追加フォームのみ）。補助ビュー | フルページ / 階層フラグメント |
| GET | `/projects/:id/scenes/:sceneId` | Scene詳細（Shot一覧、各 Shot の絵コンテと Take）。クエリ `shot` で 1 つの Shot を選択。他 Project の Scene は 404 | フルページ / 階層フラグメント |
| GET | `/shots/:id` | Shot Inspector（Objective/Duration/Character/Acting/Camera/Lighting/Product/Continuity/References/Generation Model/Take List/Judge Results/Locks）。モデル別プロンプトは常設せず、各 Take の「生成の経緯」（`GET /takes/:id/trace`）で確認する | フルページ / Inspectorフラグメント |
| GET | `/takes/:id/trace` | Take の Generation Trace（F-27）: Prompt version・Source Shot Spec（Prompt version のスナップショット）・World State version・Model・Model routing（Multi-model routing の選択理由: 方式・Quality Policy の割り当て・選んだモデル・候補ごとの実績/コスト/適合/score・除外理由。旧 Take や失敗時は「記録なし」）・Model parameters・References・Output（動画へのリンク。アーカイブ済みなら「アーカイブ済み」）・Judge results（新しい順）・Repair history（この Take への Repair と、この Take を作った Repair）。Shot Inspector の Take 一覧の「生成の経緯」から `hx-get` で読み込む。存在しない Take は 404 | フラグメントのみ（`take_trace_panel`） |
| GET | `/branches/:id/versions` | Branch のバージョン履歴（`version_history_panel` を 2 つ: Branch 自体の `branch_version` と、当該 Branch 独自の World State バージョン系列。復元ボタン付き）。存在しない Branch は 404 | フラグメントのみ |
| GET | `/projects/:id/bundle` | プロジェクトバンドルのエクスポート（`spec/requirements/non-functional.md` §8）。ZIP（`Content-Disposition: attachment`）: `manifest.json`（`format=mam-project-bundle`・`version`・`project_id`・`exported_at`）、`data.json`（Project の全テーブルの行。`job` は実行時状態のため含めない。Series 所属の Episode は Series から切り離して出力し、`series_id` などの Series 由来の参照を空にする）、`assets/<asset 相対パス>`（Asset・Take・Export のファイルと、Audio Timeline が指す音声（ミックス・クリップ）・Lip Sync 画像。既にアーカイブされて無いファイルは含めず、行だけ残る）。存在しない Project は 404 | ファイル |
| GET | `/shots/:id/takes` | Take比較ページ（`mam-take-comparator` を埋め込み） | フルページ（Lit埋め込みのため常にフルページ） |
| GET | `/projects/:id/edit` | AI Editor / Rough Cut編集ページ（`mam-timeline-editor` を埋め込み） | フルページ |
| GET | `/projects/:id/branches` | Branch ツリーページ（ツリーは `branch_tree` をサーバー描画） | フルページ |
| GET | `/projects/:id/export` | Final Export ページ | フルページ / フラグメント |
| GET | `/projects/:id/settings` | Production Template・Quality/Cost Policy・予算上限・コスト（`cost_summary_panel`）・ディスク使用量と不要 Take のアーカイブ・Brand Constraints・APIキー設定。Chat Workspace の補助ビュー一覧と入力欄の「設定」から開く | フルページ / フラグメント |
| GET | `/projects/:id/analytics` | Production Analytics（M3、`spec/spec.md` §28）。Project の MVP Success Metrics（North Star・Shot・人間の介入・時間・コスト・品質・完成）を `analytics_panel` で表示する。計算式と記録する人間の介入は §28.2・§28.3。集計はローカル DB のみで外部へ送信しない。存在しない Project は 404。Project のメニューの「制作指標」から開く | フルページ / フラグメント |
| GET | `/analytics` | すべての Project の合計の MVP Success Metrics（`/projects/:id/analytics` と同じ画面、Project 名の代わりに「すべてのプロジェクト」）。Project 一覧の「制作指標」から開く | フルページ / フラグメント |
| GET | `/projects/:id/world-state` | World State 一覧（Character/Product/Location/Camera/Story の現在値表示） | フルページ / フラグメント |
| GET | `/projects/:id/references` | Reference Library（補助ビュー、M2。登録済みの参照画像の検索・アップロード・Shot への追加・削除）。クエリ `q`・`kind`・`tag` で絞り込む | フルページ / フラグメント |
| GET | `/projects/:id/notes` | Notes 一覧・編集（補助ビュー、M2。ピン留め・カテゴリ・アーカイブの表示、`version_history_panel` でバージョン復元） | フルページ / フラグメント |
| GET | `/series/:id` | Series（M2。正本の Style・Character・Product・Location、Episode 一覧と次の Episode 作成、正本の更新履歴。Character の編集と Style の保存フォームを持つ） | フルページ / フラグメント |
| GET | `/series/:id/notes` | Series 正本 Notes の一覧・編集（M2） | フルページ / フラグメント |
| GET | `/projects/:id/reviews` | レビュー（補助ビュー、M3。オーナー側）: 書き出しからレビュー依頼を作るフォーム、依頼ごとの状態（未回答/承認済み/修正依頼あり）・パッケージのダウンロード・取り込んだ応答（判定・コメント、解決済み切替）、応答の取り込みフォーム | フルページ / フラグメント |
| GET | `/projects/:id/reviews/:reviewId/package` | Review Package のダウンロード（ZIP。`Content-Disposition: attachment`、`manifest.json` と `video.mp4|webm` だけ。`spec/spec.md` §27.1）。当該 Project の依頼でなければ 404、書き出しファイルが無ければ 409 | ファイル |
| GET | `/reviews` | レビュアー側の入口（M3）: Review Package の読み込みフォームと、読み込み済みのレビュー一覧（判定済みか）。Project を持たないので Project の画面に依存しない | フルページ / フラグメント |
| GET | `/reviews/:id` | レビュー画面（M3）: 動画プレーヤー、オーナーのメモ、コメント追加フォーム・一覧、判定フォーム、応答の書き出しリンク。存在しなければ 404 | フルページ / フラグメント |
| GET | `/reviews/:id/video` | 読み込んだ動画の配信（`X-Content-Type-Options: nosniff`、`Content-Security-Policy: sandbox`） | ファイル |
| GET | `/reviews/:id/response` | Review Response（JSON、`Content-Disposition: attachment`）のダウンロード。判定を書いていなければ 409 | ファイル |

## 2. アクションルート（Gin, フラグメントのみ）

### 2.1 Project / Brief / Mode

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/import` | プロジェクトバンドルのインポート（`multipart/form-data`、フィールド `bundle`＝`GET /projects/:id/bundle` の ZIP。`spec/requirements/non-functional.md` §8）。バンドルの Project を ID・ファイルパスのまま新規に作る（1 トランザクション。失敗時は行もファイルも残さない）。成功は 201 と `HX-Redirect: /projects/:id`。ファイルがない・ZIP でない・`manifest.json`/`data.json` が読めない・`format`/`version` が違う・未知のテーブル/カラム・他 Project の行・外部キーが壊れている・Project ディレクトリ外を指すパスは 422（インポートフォームをエラー付きで再描画）。`data.json` が参照していない `assets/` のエントリは展開しない。展開するファイルの合計が 32 GB を超える、または assets ディレクトリのあるディスクの空き容量を超える場合も 422 で、何も書かない。ファイルは assets 直下の一時ディレクトリ（`.import-*`）に展開し、Project のディレクトリへ移して確定するため、同じバンドルを同時にインポートしても先に成功した Project のファイルは消えない。同じ ID の Project が既にあれば 409（既存を変更しない）。取り込んだ Project に実行中のジョブは無いので、生成中だった Shot は PLANNING に戻す | non-functional.md §8 |
| POST | `/projects` | 新規プロジェクト作成。Body（`multipart/form-data`）に最初の発言（`content`）と添付（`attachments`、画像 jpg/png/webp/gif ≤20MB・動画 mp4/mov/webm ≤300MB、最大8件。拡張子と内容の署名を検証し不正は 422）を指定でき、Brief はその発言から抽出してチャットで補完する。成功時は `HX-Redirect: /projects/:id`（201）。既存アセットの再利用は `POST /projects/:id/messages` の `attachment_asset_ids`。Series 作成後の Episode は `POST /series/:id/episodes` を使う。任意のフォーム項目 `brand_template_id` を指定すると、その Brand Template の制約を新しい Project にコピーする（存在しなければ 422 で何も作らない。F-23） | F-01, F-25, F-30 |
| PUT | `/projects/:id/brief` | Brief 更新 | F-01 |
| PUT | `/projects/:id/mode` | AUTO ⇄ DIRECTOR 切替 | F-02 |
| PUT | `/projects/:id/confirmation-level` | FULL AUTO / KEY CHECK / CUSTOM CHECK 設定。Body `level`=`full_auto\|key_check\|custom_check`、`checkpoints`（CUSTOM CHECK の確認ポイント、複数可。選べるのは `script`/`rough_cut`/`final` で、それ以外の値は 422。空なら前回の選択、無ければ script+final）。変更内容はスレッドに AI 発言として記録される | F-03 |
| PUT | `/projects/:id/quality-policy` | FAST / BALANCED / MAX QUALITY 設定。Body `quality_policy`=`fast\|balanced\|max_quality`。設定画面のフォームは併せて `max_retries`（0〜10）・`max_generation_seconds`（10〜3600、Take あたり）・`min_acceptance_score`（0〜100、総合）・`min_axis_score`（0〜100、軸ごと）を送り、空欄は「Policy の既定値」（保存済みの上書きも消える）。`quality_policy` だけの要求（入力欄のセレクト）は上書きを変えず 204。不正値は 422（設定画面はフォームを入力値・エラー付きで再描画）。変更内容はスレッドに AI 発言として記録される | F-22 |
| PUT | `/projects/:id/budget` | Project/Shot budget ceiling 設定。Body `project_budget`・`shot_budget`（USD、0 より大きく 1,000,000 以下。空欄=上限なし）。不正値は 422（フォーム再描画）。Shot の上限は Repair/Retake の Max cost per Shot を兼ねる。上限を超える見込みの生成は §2.3 の `POST /shots/:id/generate` が 402 で拒否する | F-22 |
| PUT | `/projects/:id/production-template` | PRODUCT/CM・DRAMA・ANIME・SNS/SHORT VIDEO 切替。Body `production_template`=`product_cm\|drama\|anime\|sns_short`。以降の Prompt Planner / Art Director / Judge / Editor は選択テンプレートの評価軸と優先順位（`spec/spec.md` §19）に従う。不明な値は 422。変更内容はスレッドに AI 発言として記録される | F-21 |
| PUT | `/projects/:id/locks` | Story/Music という Shot に属さないパラメータの AUTO/MANUAL/LOCK 設定（`target_type`=`story\|music` を受理） | F-05 |

### 2.1a Chat / Approval

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/messages` | チャットへの発言。Body `content`、`reply_to_message_id`（任意、返信先カード）、`attachment_asset_ids`（任意、当該 Project の既存アセット）、`attachments`（任意、`multipart/form-data` の新規ファイル。検証は `POST /projects` と同じ）。応答は追加された発言・AI 発言のフラグメント。AI 発言への回答（Question/Escalation カードの選択肢押下を含む）もこの経路。内容は Command Interpreter が解釈し、対象解決・構造化操作・聞き返しのいずれかを実行して AI 発言をスレッドに追記する | F-30, F-24, F-01 |
| POST | `/projects/:id/approvals/:messageId` | Approval カードへの応答。Body `decision`=`approve\|request_changes`、`comment`（任意）。`approve` で AWAITING_APPROVAL から次工程へ進む（`ref_type=note_promotion` のカードを除く）。`ref_type=note_promotion` のカード（M2）は非ブロッキングで、`approve` は `proposed_diff` を主系列 World State に適用して `applied`（LOCK 項目は再判定して除外し、エラーにはしない）、`request_changes` は `rejected` とする。いずれも Project の状態は変わらない。応答は応答済みのカード（ボタンなし）で、`HX-Trigger: chat:refresh` により後続メッセージを取得させる。応答済みのカードへの再応答は 409 | F-30, F-03, F-31 |

### 2.2 Scene / Shot / Lock

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/scenes` | Scene 作成。Body `title`（必須、100文字以内）、`location`/`time`/`objective`（任意）。成功時は追加した Scene を選択した階層フラグメント（201）、入力不正は入力値を保ったまま再描画（422）。スレッドに AI 発言として記録される | F-04 |
| POST | `/scenes/:id/shots` | Shot 作成。Body `objective`（必須、300文字以内）、`duration_seconds`（必須、0 より大きく 60 以下）。成功時は階層フラグメント（201）、入力不正は 422、存在しない Scene は 404 | F-04 |
| PUT | `/shots/:id/locks` | パラメータ単位の AUTO/MANUAL/LOCK 設定（Character Identity/Camera/Product Geometry 等） | F-05 |
| PUT | `/shots/:id/shot-spec` | Canonical Shot Spec の手動編集（DIRECTOR MODE） | F-06 |

M1 の入出力契約（Issue #12）:

- `PUT /projects/:id/mode`: Body `mode`=`auto|director`。応答は MODE コントロール（`chat_composer` 内）のフラグメントで、`HX-Trigger: chat:refresh` によりスレッドに追記された AI 発言（モード切替の通知）を取得させる。不正値は 422、存在しない Project は 404。DIRECTOR の間はパイプラインが工程を進めない（指示した箇所だけを直して待機する）。AUTO に戻すと、DIRECTOR 中に止まっていた工程から再開する。モードは `project.mode` に保持され、切替でほかの状態は変わらない
- `PUT /shots/:id/locks`: Body `parameter`（`dialogue|character_identity|acting|camera|lighting|product_geometry|prompt|video_model|take_selection|editing`）と `mode`（`auto|manual|lock`）、`video_model` のみ `value`（`kling-3.0|seedance-2.0`）。応答は当該パラメータの `lock_toggle` フラグメント。`video_model` は MANUAL でモデル名が必須、LOCK で省略時は最新 Take のモデルを固定する。すでに LOCK 中の値を別の値で LOCK し直す要求は 423。不正なパラメータ/モードは 422、存在しない Shot は 404。`lock` に行が無いパラメータは AUTO
- `PUT /projects/:id/locks`: Body `target_type`（または `parameter`）=`story|music` と `mode`。Story を LOCK すると State Extractor は Story の全項目を変更しない。Shot のパラメータは 422、存在しない Project は 404
- `PUT /shots/:id/shot-spec`: Body `shot_spec` は Canonical Shot Spec の JSON（未知のキーは 422。`duration` は Shot の長さに固定）。`domain.ShotSpec.Validate` を満たさない入力は 422 でフォームを入力値と理由付きで再描画。LOCK 中のパラメータの部分を変える編集は 423 で何も書かず（Prompt を LOCK した Shot は Spec のどの部分も変えられない）、生成中の Shot は 409。成功時は新しい `prompt_version` を追記して Inspector を返す
- `GET /shots/:id`: `HX-Request` でフラグメント、なければフルページ（`shot_inspector_page`）。Canonical Shot Spec・Take List・全パラメータの AUTO/MANUAL/LOCK 切替・Spec の手動編集フォーム・自然言語の指示フォームを表示する
- `POST /shots/:id/director-command`: Body `content`（必須、`POST /projects/:id/messages` と同じ上限）。Command Interpreter が返した操作を実行する。順序は LOCK 設定 → 再生成 → モード切替。再生成は対象 Shot だけを起動し（202、他は 200）、変更する部分以外（Character/Acting/Environment 等）と LOCK 中の部分は Shot Spec に引き継ぐ。LOCK 中の部分を変える再生成を含む指示は 423 で、発言も記録せず何も実行しない（同じ指示の中で LOCK を外していれば許可）。LLM 未構成は 503。発言と結果は AI 発言としてスレッドに記録される。時間範囲を名指しした指示（`partial_regenerate`、M2）は `job.kind=repair` を起動する（202）: Shot 全体を新しい Take として作り、その区間だけを Shot の選択 Take（なければ AI Pick、なければ最新の除外されていない Take）に差し替えた候補 Take をもう 1 本足す。既存の Take は変わらず、Retry Policy の再試行回数には数えない（予算上限は適用）。ffmpeg が無い・元にできる Take が無い・Shot が生成中のときは何も作らず、その旨を AI 発言に記録して 200
- `POST /projects/:id/messages`: DIRECTOR MODE で Brief 確定後（DRAFT 以外）の発言は Command Interpreter が解釈する（Approval / Question / Escalation カードへの返信は従来どおりその回答）。対象 Shot は「Take カードへの返信 → 発言中の Scene/Shot 番号 → スレッドの直近の Take カード → Shot が 1 つだけの Project」の順で解決し、解決できなければ AI が聞き返して何も実行しない。LOCK 中の部分を変える再生成は実行せず、AI 発言でその旨を伝える。Branch 作成の指示は `create_branch` として実行する（§2.4。`count` 1〜5。指示のない分は Continue Agent の NEXT 候補で埋め、各 Branch の最初の Shot を生成する）
- `POST /shots/:id/generate`: `additional_instruction` を伴う要求は、Prompt が LOCK された Shot では 423。LOCK / 指示で保持する部分は Shot Spec を作り直す際にも変更されず、`video_model` が MANUAL/LOCK の Shot はそのモデルだけで生成する（長さやコストによる別モデルへの切替は行わない）

### 2.3 Prompt / Generation / Judge / Repair

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/shots/:id/reference-pack` | Keyframe / Reference Pack（Character/Product/Location/Style/Lighting Reference）生成・再生成。非同期ジョブを起動し、`/shots/:id/generate` と同じく 202 と GENERATING の Shot カードを返す | F-11 |
| POST | `/shots/:id/generate` | Prompt Autopilot → Model Adapter → Video Generation を実行（非同期ジョブ起動、202+進捗はWSで通知）。Body `additional_instruction`（任意、1000 文字以内）。予算上限に 1 Take も収まらなければ 402、生成中の Shot は 409、存在しない Shot は 404、生成機能が未構成（Model Adapter / LLM / ジョブキューなし）は 503。完了した Take は Take カードとしてチャットに追記され、`take.model_name` と `cost_ledger_entry.provider` は `spec/architecture/model-adapters.md` §6 の正規識別子で保存される | F-06, F-07, F-12 |
| POST | `/shots/:id/generate/candidates` | Prompt Candidate Generation（Quality/Balanced/Economy Mode）。Prompt Planner が Stability First / Acting First / Dynamic Camera の Candidate（Prompt Version）を作り、Take を生成して AI Video Judge が Best Take（`take.status=ai_pick`）を選ぶ。非同期ジョブ（`job.kind` は generation と同じ）を起動し、`/shots/:id/generate` と同じく 202 と GENERATING の Shot カードを返す。進捗 WebSocket の `action` は `candidates` で、`stage` は §4 の列挙のままである。Body `additional_instruction`（任意）。モードは Quality/Cost Policy で決まる: MAX QUALITY→Quality（Stability First / Acting First / Dynamic Camera を並列生成し Judge が選ぶ。予算が 3 Take 分あれば 3 戦略すべて。1〜2 本だけ収まるときは先頭からその本数だけ 202 で並列生成し、1 本も収まらなければ 402 でジョブも Take も作らない）、BALANCED→Balanced（第一候補のみ。Judge が FAIL の間、Retry Policy と予算の範囲で次の Candidate を追加）、FAST→Economy（1 Take。LOCK 違反または high の Issue があるときだけ Retake）。Take の `model_parameters.candidate_strategy` に戦略を保存する。エラーは `/shots/:id/generate` と同じ（402 / 404 / 409 / 503）で、Judge が未構成でも 503 | F-08 |
| POST | `/takes/:id/judge` | AI Video Judge を明示的に再実行。非同期ジョブ（`job.kind=judge`）を起動し、202 と JUDGING の Shot カードを返す。結果は `judge_result` に追記され（過去の結果は残る）、チャットに投稿される。Take を作るたびに自動でも実行される。エラー: 404（Take が存在しない）、409（この Take の Judge がすでに実行待ち/実行中）、503（ffmpeg / LLM 未設定で Judge が無効） | F-13 |
| POST | `/takes/:id/repair` | Judge 結果に基づく Repair 実行。最新の Judge 結果から Repair Agent が戦略（Prompt Repair/Partial Regeneration/Full Retake/Different Model/Reference Reinforcement/Edit-Level Repair）を選び、Retry Policy の範囲内で新しい Take を作る。非同期ジョブ（`job.kind=repair`）を起動し、202 と REPAIRING の Shot カードを返す。新しい Take は `take` の新しい行で、修復元の Take は変わらない（`repair_action.resulting_take_id` で追跡）。作られた Take は自動で Judge される。Partial Regeneration は、Judge のすべての Issue が `time_range` を持ち ffmpeg があるとき、Shot 全体の新しい Take に加えて、その区間だけを新しい Take の映像に差し替えた候補 Take（`model_parameters.kind=partial_take`、`source=partial_regeneration`。Shot の選択 Take は置き換えない）をもう 1 本作り、これも Judge する（`spec/spec.md` §13）。Retry Policy（Max retries / Max cost per Shot / Project 予算）の上限に達していたときはジョブが Take を作らず、チャットに Escalation カードを出す。AUTO MODE では Judge のあと全 Take が不合格なら最良の Take から自動で繰り返す（DIRECTOR MODE では自動で繰り返さない）。エラー: 404（Take が存在しない）、409（まだ Judge されていない／Judge に合格していて Repair 不要／Shot が生成中または Repair 中）、503（ジョブキュー・LLM 未設定で Repair が無効） | F-14 |

### 2.4 Take / Continue / Branch

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/takes/:id/select` | Take を Selected として確定（DIRECTOR MODEでの手動選択。AI Pick や以前の選択より優先され、以前の Selected は Candidate に戻る）。アーカイブ済み Take は 422、存在しなければ 404。応答は Shot カード（200） | F-15 |
| POST | `/shots/:id/takes/partial` | 複数Takeの区間指定による部分合成。Body は JSON `{"segments":[{"take_id","start","end"}]}`（秒）または `{"instruction":"Take A の 0-2秒 + Take B の 2-4秒"}`（フォームの `segments`（JSON 配列）/`instruction` も可、`segments` 優先）。区間は 2〜8 個・各 0.3 秒以上・Take の長さ内・除外/アーカイブ済み Take 不可（違反は 422）。つなぎ目のクロスフェードは 0.25 秒で、最短区間の 1/3 の方が短いときはその長さに縮める（区間が 1 つのときは無い）。音声は全区間に音声があるときだけ同じ長さでクロスフェードし、1 本でも音声が無い Take が混ざると出力は無音になる。映像は先頭区間の解像度・フレームレートに揃える。新 Take を作って Selected にし、チャットに Take カードを投稿。ffmpeg が無いとき 503。応答は Shot カード（200） | F-15 |
| DELETE | `/takes/:id` | Take を Rejected として除外（ファイルと履歴は残す。Shot の選択中 Take だった場合は選択なしに戻る）。応答は Shot カード（200） | F-15 |
| GET | `/takes/:id/video` | Take のプレビュー動画（Range 対応）。高さ 1080 超（4K）のときは 1080p プロキシを初回に生成して配信し、原本は変えない。ffmpeg が無いときは原本を配信。アーカイブ済みは 404 | `mam-video-player` |
| GET | `/takes/:id/thumbnail` | Take の先頭フレームの JPEG（ffmpeg が無いとき 503） | `mam-video-player`（poster） |
| POST | `/shots/:id/continue` | Continue: 完成した Shot の最終フレーム（起点の Take の映像から ffmpeg で取り出した画像。ffmpeg が無いときは Shot Spec と World State のテキストのみ）と Story/Character/Position/Emotion/Object/Camera/Lighting/Motion/Dialogue/Music State を解析し、次展開候補（NEXT A/B/C）を Question カード（`ref_type=continue`、`ref_id`=Shot ID）としてチャットに追記する。選択肢は A〜C、D（自由指示）は入力欄からの返信、E（AIに任せる）は常設ボタン。応答は 201 とカードのフラグメントで、`HX-Redirect` でチャットへ遷移させる。未完成の Shot（非 Rejected の Take が無い）は 409、生成中の Shot は 409、存在しない Shot は 404、生成機能が未構成は 503、AI の出力が使えない場合は 502 | F-16 |
| POST | `/shots/:id/continue/confirm` | 選択した展開候補で次 Shot/Scene を生成。Body `instruction`（選択した候補または自由指示、1000 文字以内）または `delegate=true`（AIに任せる。AI が候補を選ぶ）。Director Agent が World State を引き継いだ次 Shot を決め、場所・時間が変わる場合のみ新しい Scene を作って既存プロジェクト末尾に連結し（同じ Scene の場合は完成 Shot の Scene 末尾に追加）、`POST /shots/:id/generate` と同じ生成を `instruction` を追加指示として開始する。応答は 202 と新 Shot の GENERATING の Shot カード（`HX-Trigger: chat:refresh`）。指示も `delegate` も無ければ 422、その他のエラーは `/shots/:id/continue` と同じ。Continue カードへの返信（`POST /projects/:id/messages` の `reply_to_message_id`）も同じ処理を起動する（選択肢の押下・自由入力・「AIに任せる」）。カードは 1 回だけ回答でき、2 回目は 409 | F-16 |
| POST | `/shots/:id/branch` | Branch Point 保存 + Snapshot 生成（Body: `point_type`=`scene\|shot\|frame`、`frame`時は`offset_seconds`を指定。`scene`指定時は当該Shotの所属Sceneを分岐点とする） | F-17 |
| GET | `/branches/:id` | Branch 詳細（フラグメント） | F-17 |
| PUT | `/branches/:id` | Branch 名称変更・World State参照更新（`branch_version` に新規バージョン行を追記） | F-17, F-28 |
| POST | `/shots/:id/versions/:versionNumber/restore` | Shot/Take（`prompt_version`）の指定バージョンの Canonical Shot Spec を Shot の現在の Spec に戻し、新しい `prompt_version` として追記する（既存バージョンとその Take は変えない。復元自体も戻せる）。応答は 200 と Shot Inspector。バージョン番号が 1 以上の整数でなければ 422、存在しない Shot/バージョンは 404、生成中の Shot は 409、復元で LOCK 中の部分が変わるなら 423（何も書かない） | F-28 |
| POST | `/projects/:id/world-state/versions/:versionNumber/restore` | 主系列（トランク、`branch_id`=NULL）World State の指定バージョンへ復元。そのバージョンに載っている Character/Product/Location の各フィールド値を行へ書き戻し（バージョンに無い、後から追加された行は変えない）、Camera/Story はそのバージョンの値にして、結果を新しい `world_state_version` として追記する。応答は 200 と World State ビュー。422（番号）/404（Project・バージョン）/423（復元で LOCK 中のフィールドが変わる。何も書かない） | F-28 |
| POST | `/branches/:id/world-state/versions/:versionNumber/restore` | 当該Branchが独立して持つ World State バージョン系列の指定バージョンへ復元。Branch はトランクと同じく自身の Character / Product / Location 行に状態を持つので（Issue #18）、指定バージョンの内容をその Branch の行へ書き戻し、結果をその系列の末尾へ新バージョンとして追記する（LOCK 中の項目が変わる復元は 423 で何も書かない）。応答は 200 と Branch のバージョン履歴。422/404（Branch・バージョン）。パスの Branch ID は他の `/branches/:id/...` と同じく `:id`（Gin は同じパス位置のワイルドカード名を 1 つしか許さない） | F-28 |
| POST | `/branches/:id/versions/:versionNumber/restore` | Branch自体（`branch_version`）の指定バージョンへ復元。その名称・World State 参照を Branch に戻し、新しい `branch_version` として追記する。応答は 200 と Branch のバージョン履歴。422/404 | F-28 |


**Take System の動作（M1）**

- AI Pick: Judge が結果を保存するたびに、その Shot の Take のうち Judge 済みで除外されていないものから最良を `ai_pick` にする（PASS が FAIL より先、次に 4 スコアの平均、同点は古い方）。ユーザーが Selected にした Take があれば何もしない。AUTO MODE では PASS した AI Pick を Shot の Take（`selected_take_id`）にもする。DIRECTOR MODE では推奨のみで、確定はユーザー（`POST /takes/:id/select`）。変更があったときは Judge のチャット通知に「AI Pick: …」を追記する。
- Take カード（`ref_type=take`）の本文 JSON は `video_url`（`/takes/:id/video`）・`poster_url`・`shot_id` を持ち、`mam-video-player` を埋め込む。カードには返信と Take 比較（`GET /shots/:id/takes`）への導線がある。
- `GET /shots/:id/takes` は `mam-take-comparator`（`api-url` に `GET /api/v1/shots/:id/takes`）を置くページ。部分採用が確定するとコンポーネントが `partial-take-confirmed` を発火する。

M2 の入出力契約（Issue #18 Branch）:

- `POST /shots/:id/branch`: Body は `application/x-www-form-urlencoded`。`point_type`（`scene|shot|frame`、省略時 `shot`）、`offset_seconds`（`frame` のときのみ。0 以上・Shot の長さ以内、外れれば 422）、`name`（省略時は指示文、なければ「Branch N」。100 文字以内）、`direction`（省略可。指定すると Continue Agent が Branch 最初の Shot を計画して生成する。生成が未設定なら 503）。分岐点の World State を Snapshot として Branch 独自の `world_state_version` v1 に保存し、201 と `HX-Redirect: /branches/:id` を返す。分岐点が Branch 内の Shot なら、その Branch の子 Branch になる
- `GET /branches/:id`: `HX-Request` で詳細フラグメント（`branch_tree` の各項目が `hx-get` で取得して `#branch-detail` へ差し込む。応答には選択中の Branch を示したツリーを `hx-swap-oob` で同梱する。分岐点・親 Branch・Branch 独自の Scene/Shot・バージョン履歴・`/projects/:id/world-state?branch_id=` への導線）、なければ Branch ツリー付きのフルページ。`GET /projects/:id/branches` は選択なしのツリーページ
- `PUT /branches/:id`: `name` と `world_state_version`（その Branch 自身の World State 系列の番号）の少なくとも一方。応答は詳細フラグメントと、新しい名前を反映した Branch ツリー（`hx-swap-oob` で `#branch-tree` を置換）。変更があれば `branch_version` に 1 行追記する（変化なしは追記しない）。系列にない番号・空の名前は 422、不明な ID は 404
- Branch は `scene` / `character` / `product` / `location` / `world_state_version` を独自に持ち（`branch_id`）、Branch の Shot の生成・State Extractor・編集はその Branch の行だけを更新する。トランクや他の Branch には影響しない。チャットスレッドは Project に 1 本で Branch では分けない
- チャット（DIRECTOR MODE）の「このカットから別展開を3つ作って」は Command Interpreter の `create_branch`（`count` 1〜5、`point_type` は `scene|shot`、`directions`）となり、指示のない分は Continue Agent の NEXT 候補で埋め、各 Branch の最初の Shot を生成する

### 2.5 Director Command

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/shots/:id/director-command` | 対象 Shot が確定している自然言語指示 → Command Interpreter → 構造化操作（LOCK設定/パラメータ更新/再生成/Branch作成等）を実行。チャットからの発言は対象解決を含む `POST /projects/:id/messages`（§2.1a）を使い、内部で本エンドポイントと同じ処理を呼ぶ | F-24 |

### 2.6 Editor / Audio / Final QC / Export

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/edit/rough-cut` | Selected Takes から Rough Cut 生成。Body `branch_id`（省略時=主系列トランク）で対象Branchを指定 | F-18 |
| PUT | `/edit-timelines/:id` | タイムライン編集（Cut Point/Timing/Pace/Transition 等の手動調整） | F-18 |
| POST | `/edit-timelines/:id/judge` | Editor Judge 実行（PASS/Re-edit/素材不足を判定） | F-18, F-19 |
| POST | `/edit-timelines/:id/request-additional-shot` | 素材不足時の Director Agent への差し戻し（Editor→Reshoot Loop）。Editor Judge が `need_shot` と判定した版の追加 Shot を作らせ、できたら Edit へ戻る（M3、下記） | F-19 |
| POST | `/projects/:id/audio/generate` | Audio Production 実行（Dialogue/Narration/BGM/SE/Mixing）。Body `branch_id`（省略時=主系列トランク）で対象Branchを指定 | F-20 |
| POST | `/projects/:id/final-qc` | Final QC 実行 | F-26 |
| POST | `/projects/:id/export` | 最終書き出し実行（Aspect Ratio/解像度/コーデック指定）。Body `branch_id`（省略時=主系列トランク）で対象Branchを指定 | F-26 |

M1（F-18 Simple / F-20 簡易版 / F-26。範囲は `spec/requirements/functional.md` §3.6）の実装は次のとおり。アクションルートは HTML フラグメント（`#editor-actions`）を返し、業務エラーは Toast（§0）で返す。

| Method | Path | M1 の挙動 |
|---|---|---|
| GET | `/projects/:id/edit`（別名 `/projects/:id/export`） | 編集画面（`mam-timeline-editor` と Rough Cut〜書き出しの各ステージのボタン）。補助ビュー（別ページ）。チャットの［⋯］メニューから到達 |
| GET | `/projects/:id/edit/actions` | ステージボタン部分のフラグメント。`timeline-updated` イベントで再取得される |
| POST | `/projects/:id/edit/rough-cut` | 各 Shot の Selected Take（ファイルのあるもの）から Rough Cut を作り、Project を EDITING にして Edit Timeline の第1版を保存する。Selected Take のない Shot がある・Shot がない場合は 422。EDITING へ進めない状態（承認待ち等）は 409。`HX-Trigger: timeline-reload` を返す（`mam-timeline-editor` が読み直す）。フォーム／JSON の `branch_id`（省略時=主系列トランク）でその Branch の Shot 列から Rough Cut を作り、Branch ごとに独立した版として保存する。Production Template が評価軸に持つ Pace / Hook / Retention / CTA Timing / Caption は Editor Agent が作成時に反映し、内容をチャットに投稿する。Reshoot Loop で追加済みの Shot は元の Shot の後に残る |
| PUT | `/edit-timelines/:id` | JSON `{"clips":[{"shot_id","take_id","in_point","out_point","transition":{"type","seconds"}}]}` を次の版として保存し 204。`:id` は編集画面が表示していた版で、それより新しい版があれば 409（古い画面での上書き防止）。クリップ 0 件・最短 0.5 秒未満・Take の長さ超過・別 Shot の Take・不明な Transition は 422。保存すると Project は EDITING に戻る。クリップには `kind`（`main`（省略時）/`insert`/`reaction`。追加 Shot 由来の切り出し）と `caption`（字幕、最大 60 文字、空で字幕なし）も持てる。保存は `:id` の版と同じ Branch の版として追記する |
| GET | `/edit-timelines/:id/preview` | Rough Cut の下書きレンダリング（`video/mp4`、Range 対応）。版ごとに初回アクセスで作成しキャッシュする（ffmpeg が必要。なければ 503） |
| POST | `/edit-timelines/:id/judge` | Editor Judge（PASS / Re-edit / `need_shot`）。最新版のみ・EDITING のみ（Escalation 応答後の REPAIRING からも実行でき、EDITING へ戻す）。判定は Production Template の評価軸に従い、Pace / Hook / Retention / CTA Timing / Caption は編集で直せる指摘（Re-edit）、Product Exposure / Reaction Shot は追加 Shot が要る指摘（`need_shot`、指摘に `need_shot: {kind: insert|reaction, after_clip, objective, duration_seconds}` が付く）にする。1 件でも `need_shot` があれば判定は `need_shot`。結果を `editor_judge_result` に保存する。PASS は AUDIO へ（Confirmation Level が Rough Cut の確認を求める場合は Approval カードで AWAITING_APPROVAL、承認で AUDIO）。Re-edit は EDITING のまま、指摘をチャットに投稿する。`need_shot` は EDIT_JUDGING のまま、AUTO では続けて `request-additional-shot` の処理を自動で行い、DIRECTOR では要求を待つ |
| POST | `/projects/:id/audio/generate` | AUDIO のときのみ。Audio Director（M3、`spec/architecture/model-adapters.md` §14）が Scene Emotion と編集テンポを読んで Audio Timeline（Dialogue / Narration / BGM / SE / Foley / Ambience）を作り、Take の音声とミックスして −14 LUFS に正規化し、Audio Timeline の版として保存する。作る層は Quality Policy で決まる（下記）。フォーム／JSON の `branch_id`（省略時=主系列トランク）でその Branch の Edit Timeline から作り、Branch ごとの系列に版を追記する。API キー未設定は 502（`elevenlabs` のキーを設定画面で登録）、ffmpeg なしは 503 |
| POST | `/projects/:id/final-qc` | AUDIO のときのみ。PASS → READY（Final の確認を求める設定なら Approval カード経由）。FAIL → 該当工程（EDITING / AUDIO）へ差し戻し、指摘をチャットに投稿する。結果は `final_qc_result` に履歴として残る |
| POST | `/projects/:id/export` | READY（または再書き出しの EXPORTED）のときのみ。フォーム／JSON の `aspect_ratio`（`16:9`/`9:16`/`1:1`/`4:5`）・`resolution`（`720p`/`1080p`/`1440p`/`2160p`）・`codec`（`h264`/`h265`/`vp9`）を指定（省略時は Brief のアスペクト比・1080p・h264）。`branch_id`（省略時=主系列トランク）の Edit Timeline と Audio Timeline を書き出し、その Branch が無い・そのタイムラインが無いときは 404。一時ファイルへ書いて完了後にリネームし（アトミック）、`export`（`branch_id` も記録）に記録して EXPORTED にする |
| GET | `/projects/:id/exports/:exportId/video` | 書き出した動画のダウンロード（`video/mp4` / `video/webm`） |
| POST | `/edit-timelines/:id/request-additional-shot` | M3。Editor Judge が `need_shot` と判定した最新版に限る（EDIT_JUDGING のとき。判定前は 409、生成が未設定なら 503）。判定の `need_shot` ごとに追加 Shot（Branch の系列では Branch 側の Scene）を作り、各 Shot の生成を始める。すべての生成を enqueue できたときだけ `reshoot_request`（1 回の呼び出し = 1 ラウンド、status `requested`）を記録して GENERATING へ進む。予算超過などで開始に失敗した Shot があるときはラウンドを作らず（`requested` を残さない）、開始できなかった Shot の行は消し、Project は EDIT_JUDGING のままエラーを返す（始められた Shot の生成は残る）。同じ版への再要求は「まだできていません」では拒否されない。ラウンド数が Retry Policy の Max retries（Repair Loop と共通）に達しているときは要求せず、Escalation カードを投稿して AWAITING_APPROVAL にする（応答後は REPAIRING から Editor Judge をやり直せる）。追加 Shot の Selected Take が揃うと、`need_shot` の `after_clip` の Shot の後にそのクリップ（`kind` は `insert`/`reaction`）を足した新しい版を保存し、EDITING へ戻る（Take の選択・ジョブ完了のたびに確認する）。もう一度 Editor Judge を実行して Loop を続ける |


M3（F-20 Audio Director）の実装は次のとおり。

| Method | Path | M3 の挙動 |
|---|---|---|
| POST | `/projects/:id/audio/generate` | Project が AUDIO でなければ 409。Branch がこの Project のものでなければ 404（Branch に Edit Timeline がなければ 404）。①Edit Timeline の Scene ごとに編集テンポ（`fast`/`medium`/`slow`）と Scene Emotion（Reasoning LLM が判定。LLM 未設定時は規則）を読み、②脚本の `dialogue` 行を ElevenLabs TTS（`elevenlabs-tts`）で話し（話者に音声クローンがあればその声）、③Quality Policy に応じて BGM（`elevenlabs-music`）・Ambience / SE / Foley（`elevenlabs-sfx`）・Lip Sync（`sync-labs`）を作り、④ローカル ffmpeg でミックスして −14 LUFS に正規化する。**FAST** はセリフ + BGM、**BALANCED** は + Ambience・SE、**MAX QUALITY** は + Foley・Lip Sync。プロバイダー呼び出しごとの見積を `cost_ledger_entry` に記録し、Project の予算上限に収まらない層は作らず（`skipped`）、セリフの合成だけで上限を超えるときは 409 で何も呼ばない。プランの LLM 出力が使えないときは 502（何も課金しない）。作れなかった層とその理由はチャットの投稿と画面に出る（個別のプロバイダーの失敗は実行全体を失敗にしない。TTS の失敗は 502）。`HX-Trigger` なし。応答はステージボタンのフラグメント |
| POST | `/projects/:id/audio/voices` | multipart の `speaker`（1〜20 文字）・`sample`（音声ファイル、1 バイト以上 10 MB 以下）・任意の `name`・`branch_id`（応答の画面用）。ElevenLabs で音声クローンを作り（`elevenlabs` のキーが必要。未設定 502、クローン未対応の構成 503）、`voice_clone` に保存する（話者ごとに 1 件。再クローンは置き換え）。入力不備は 422 |

Audio Timeline の版は Branch ごとに 1 から振る追記のみの系列で、M1 の簡易音声の版（`mode` なし）はそのまま読める（`spec/architecture/model-adapters.md` §14.6）。

### 2.7 Brand / Cost

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/brand-constraints` | Brand Guideline/禁止表現/Disclaimer/Product Claims/Logo Rules/Character Restrictions/Rights/Platform Rules 登録。フォーム `type`（`brand_guideline`/`forbidden_expression`/`required_disclaimer`/`product_claim`/`logo_rule`/`character_restriction`/`asset_rights`/`platform_rule`）・`level`（`lock`/`advisory`、省略時 `lock`）・`text`（1〜1000 文字。禁止表現は 1 行に 1 つ）。成功は 200 で `brand_constraint_list` を返す。入力不備は 422（入力とエラーを載せた `brand_constraint_list`）、Project が無ければ 404。Prompt 生成・AI Video Judge・Final QC の適用は `spec/spec.md` §26.1。`lock` の禁止表現を含む指示での生成・再生成・候補生成は 403（Take を作らない）、生成した Shot Spec が違反する場合は生成ジョブが失敗して理由をチャットに残す | F-23 |
| DELETE | `/brand-constraints/:id` | 制約削除。200 で `brand_constraint_list` を返す。無ければ 404。設定画面（`GET /projects/:id/settings`）の `brand_constraint_list` から登録・削除できる | F-23 |
| POST | `/projects/:id/brand-templates` | Project の制約の現在のセットを Brand Template として保存（M3）。フォーム `name`（空白をまとめて 1〜100 文字、大文字小文字を区別せず一意）。保存はその時点のコピー（以後の Project の制約の変更はテンプレートに影響しない）。成功は 200 で `brand_template_panel`。名前の不備・重複、制約が 1 件もない Project は 422（入力とエラーを載せた `brand_template_panel`）、Project が無ければ 404。設定画面（`GET /projects/:id/settings`）に `brand_template_panel` を表示する | F-23 |
| POST | `/projects/:id/brand-templates/:templateId/apply` | テンプレートの制約を既存 Project にコピー適用（M3）。同じ `type`・`level`・`text` の制約を Project がすでに持つ場合はコピーせず、既存の制約は変更しない。適用後のコピーは Project 自身の制約で、独立して削除・追加できテンプレートには伝播しない。200 で `brand_constraint_list`、Project またはテンプレートが無ければ 404 | F-23 |
| DELETE | `/projects/:id/brand-templates/:templateId` | テンプレートを削除（M3。`:id` は応答を描く設定画面の Project）。適用済みの Project の制約は変わらない。200 で `brand_template_panel`、無ければ 404 | F-23 |
| PUT | `/credentials/:provider` | 外部モデル API キーの保存・置き換え（Form `api_key`）。`provider` は `anthropic` / `openai` / `google` / `flux` / `kling` / `seedance` / `runway` / `elevenlabs` / `synclabs` / `typesafe`。キーは前後の空白を除いて OS のセキュアストレージ（`credential.Store`）へ保存し、再起動なしで次の呼び出しから有効になる。200 で `credential_panel`（設定済み/未設定のみ。キーの値は返さない）。空・空白を含む・長すぎるキーと未対応の `provider` は 422（既存のキーは変えない） | フラグメント |
| DELETE | `/credentials/:provider` | API キーの削除。未設定でもエラーにならず 200 で `credential_panel` を返す。未対応の `provider` は 422 | フラグメント |
| GET | `/projects/:id/cost-summary` | Shot/Scene/Project 別の推定コスト・実コスト集計（`cost_summary_panel`向けフラグメント）。推定コストは生成時の見積り、実コストはベンダーが報告した金額（`cost_ledger_entry.actual_cost`。報告が無い呼び出しがあれば合計に `*` を付ける）。予算上限に対する消化率を併せて表示し、フラグメントは 15 秒ごとに自己更新する | F-22 |
| POST | `/projects/:id/takes/:takeId/archive` | Take の動画ファイルを削除して Rejected にする（Take 行・Judge/Repair 履歴・コスト記録は残す）。Selected Take は 409、当該 Project に無い Take・ファイルを既に手放した Take は 404。ディスク使用量パネルのフラグメントを返す | non-functional.md §4 |
| POST | `/projects/:id/storage/archive-unneeded` | 不要な Take（Rejected、または同じ Shot の別 Take が Selected）をまとめてアーカイブする。ディスク使用量パネルのフラグメントを返す | non-functional.md §4 |

### 2.8 World State

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| PUT | `/projects/:id/characters/:characterId` | Character 属性更新（wardrobe/hairstyle/personality 等、LOCK対象項目は423）。クエリ `branch_id`（省略時=主系列トランク）で更新対象Branchを指定 | F-09 |
| PUT | `/projects/:id/products/:productId` | Product 属性更新（material/orientation/condition 等、LOCK対象項目は423）。クエリ `branch_id`（省略時=主系列トランク）で更新対象Branchを指定 | F-09 |
| PUT | `/projects/:id/locations/:locationId` | Location 属性更新（lighting_state/weather 等、LOCK対象項目は423）。クエリ `branch_id`（省略時=主系列トランク）で更新対象Branchを指定 | F-09 |
| GET | `/projects/:id/world-state` | 現在の World State（Character/Product/Location/Camera/Story）フラグメント取得。クエリ `branch_id`（省略時=主系列トランク）で対象Branchの派生World Stateを取得 | F-09, F-10 |

M1 の入出力契約（Issue #8）:

- `PUT /projects/:id/characters|products|locations/:entityId`: Body は `application/x-www-form-urlencoded`。編集できる項目は Character が `hairstyle/wardrobe/voice/personality/emotion/position/current_action`、Product が `material/orientation/condition`、Location が `time/lighting_state/weather`（各 200 文字以内、前後の空白は除去）。送った項目だけを更新し、空文字は項目のクリア。応答は更新後の World State ビュー（`#world-state-view` フラグメント、200）で、新しい `world_state_version` を追記する。エラーはトースト（`HX-Reswap: none`）: LOCK 対象を1項目でも含むと **423** で全体を拒否（何も書き込まない）、更新項目なし・未知の項目・長すぎる値は 422、Project または当該 Project のエンティティが無ければ 404
- `GET /projects/:id/world-state`: `HX-Request` でフラグメント、なければフルページ（`world_state_page`）。LOCK 中の項目は「LOCK」バッジ付きで編集不可。Camera/Story は Shot 完了ごとに State Extractor が更新するため参照専用
- `branch_id` クエリは、当該 Project の Branch を指すときだけ有効で（Issue #18）、存在しない Branch は 404。省略時は主系列（トランク）

M2 の入出力契約（Issue #15）:

- `PUT /projects/:id/world-state/locks`: World State の項目ごとの AUTO/LOCK 設定。Body `target_type`（`character|product|location|camera|story`）、`target_id`（Character/Product/Location の ID。`camera`/`story` は省略）、`parameter`（`spec/architecture/er.md` §4 の属性名）、`mode`（`auto|lock`）。応答は更新後の World State ビュー（`#world-state-view`、200）。LOCK 中の項目は State Extractor が更新せず、ユーザー編集は 423。未知の `target_type`/`parameter`/`mode` は 422、存在しない Project・エンティティは 404。Story 全体の LOCK は従来どおり `PUT /projects/:id/locks`
- `PUT /projects/:id/characters|products|locations/:entityId/reference`: ライブラリの画像を World State の参照に割り当てる。Body `slot`（Character は `face|body`、Product は `geometry|label`、Location は `layout`）、`reference_id`（`reference_item.id`。空文字で割り当てを外す）。応答は更新後の World State ビュー（200）で、新しい `world_state_version` を追記する。対応する LOCK（`spec/spec.md` §10.1）が掛かっていれば 423、`slot` が種別に合わない・`reference_id` の種別がスロットに合わないは 422、Project/エンティティ/`reference_id` が無ければ 404
- `GET /projects/:id/world-state` の各項目には LOCK の状態（`world_state_field` の LOCK バッジと `lock_toggle`）を表示する

### 2.9 Series / Episode（M2）

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/series` | Series 作成。Body（フォーム）`name`（1〜100 文字。`from_project_id` があれば省略可で、その Project 名になる）、`from_project_id`（任意）で既存 Project の Character/Product/Location を Series 正本として登録し、その Project を Episode 1 にする（Project の Notes は Series 正本にならない。Series の Notes は `POST /series/:id/notes` で登録し、Episode 作成時に Project へコピーされる）。正本はバージョン 1（`series_version`）。成功は 201 と `HX-Redirect: /series/:id`。名前が空・長すぎる／`from_project_id` がすでに Series に属している場合は 422、`from_project_id` が存在しない場合は 404 | F-29, F-31 |
| POST | `/series/:id/episodes` | Series 正本を継承した Episode（Project）を作成（Character/Product/Location/Notes を project 側の行へコピーし `origin_id` で由来を保持。参照画像は Project の `asset`・Reference Library にも複製し、Style は Brief の `style` に入れる）。`episode_number` は最大値+1（`(series_id, episode_number)` UNIQUE）。Body `previous_episode_id`（任意、同じ Series の Episode）で前 Episode の最終 World State を開始状態にし、`inherited_world_state_version_id` に記録する。成功は 201 と `HX-Redirect: /projects/:id`。Series が無ければ 404、`previous_episode_id` が同じ Series の Episode でなければ 422。任意のフォーム項目 `brand_template_id` を指定すると、その Brand Template の制約を Episode にコピーする（存在しなければ 422 で何も作らない。F-23。Series 自体は制約を持たない） | F-29, F-25, F-31 |
| PUT | `/series/:id/characters/:characterId` | Series 正本の Character 更新。Body（フォーム）`hairstyle`/`wardrobe`/`voice`/`personality`（送った項目だけ更新）。応答は更新後の Series ビュー（200）で、新しい `series_version` を追記する。既存 Episode には自動伝播しない（次の Episode から適用）。項目が無い／長すぎる場合は 422、Series・Character が無ければ 404 | F-29 |
| PUT | `/series/:id/style` | Series の Style 正本（テキスト、500 文字以内）の更新。Body `style`。応答は更新後の Series ビュー（200）で、新しい `series_version` を追記する。既存 Episode には自動伝播しない | F-29 |

### 2.10 Notes（M2）

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/notes` | Project メモ作成。Body `title`、`content`、`category`（任意、省略時は AI が分類）、`pinned`（任意）。チャットからは `POST /projects/:id/messages` の Command Interpreter が同じ処理を呼ぶ（`source=chat`）。作成後、State Extractor が昇格提案を非同期で生成する | F-31 |
| POST | `/series/:id/notes` | Series 正本メモ作成（昇格提案は生成しない） | F-31, F-29 |
| PUT | `/notes/:id` | メモ更新。`title`/`content` が変わる場合は新しい `note_version` を追記する（Project スコープは昇格提案を再生成）。`category`/`pinned`/`archived`（`false` でアーカイブ解除）は行を更新し、バージョンは増えない。Series 正本の更新はコピー済みの Episode へ伝播しない | F-31, F-28 |
| DELETE | `/notes/:id` | メモをアーカイブ（`archived_at` を設定。以降の推論文脈には渡さない） | F-31 |
| POST | `/notes/:id/versions/:versionNumber/restore` | 指定バージョンの内容を新しい `note_version` として追記して復元 | F-31, F-28 |

### 2.11 Reference Library（M2）

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/references` | 参照画像をライブラリへ登録。`multipart/form-data`: `file`（必須、jpg/png/webp/gif、20MB まで）、`kind`（必須、`character|product|location|style|lighting`）、`name`（必須、100文字以内）、`tags`（任意、カンマ・空白・読点区切り、最大10個・各30文字以内） | F-11 |
| DELETE | `/projects/:id/references/:referenceId` | ライブラリから外す（画像と、すでに Shot に付いた参照は残す） | F-11 |
| POST | `/shots/:id/references` | ライブラリの画像を Shot の Reference Pack に追加（ピン留め）。Body `reference_id` | F-11 |

入出力契約:

- `GET /projects/:id/references`: `HX-Request` でフラグメント（`#reference-library`）、なければフルページ（`reference_library_page`）。`q`（名前・タグの部分一致）、`kind`、`tag`（完全一致）は AND で絞り込み、新しい登録順。不正な `kind` は 422
- `POST /projects/:id/references`: 成功時は更新後のライブラリ（201）。ファイルなし・拡張子と内容の不一致・サイズ超過・不正な `kind`・名前が空か長すぎる・タグが多すぎる／長すぎるは 422（何も保存しない）、存在しない Project は 404
- `DELETE /projects/:id/references/:referenceId`: 更新後のライブラリ（200）。当該 Project に無ければ 404
- `POST /shots/:id/references`: 応答は更新後の Shot Inspector（`GET /shots/:id` と同じ、Reference Pack にピン留めした参照を含む。200）。同じ種別・同じ名前の参照があれば置き換え、`library_item` に登録 ID を記録する。Reference Pack の再生成ではピン留めした参照を残し、再生成しない。`reference_id` が空は 422、Shot または `reference_id`（同じ Project のもの）が無ければ 404、生成中の Shot は 409

### 2.12 Team Collaboration（M3）

| Method | Path | 概要 | 対応機能 |
|---|---|---|---|
| POST | `/projects/:id/reviews` | レビュー依頼の作成。Body（フォーム）`export_id`（当該 Project の Final Export）、`note`（任意、1000 文字以内） | F-32 |
| POST | `/projects/:id/reviews/feedback` | Review Response の取り込み。`multipart/form-data`、フィールド `response`（JSON ファイル） | F-32 |
| PUT | `/projects/:id/review-comments/:commentId` | 取り込んだコメントの解決済み切替。Body `resolved=true\|false` | F-32 |
| POST | `/reviews/import` | Review Package の読み込み（レビュアー側）。`multipart/form-data`、フィールド `package` | F-32 |
| POST | `/reviews/:id/comments` | コメントの追加。Body `timecode`（動画内の位置、秒。空なら位置なし）、`body`（1〜2000 文字） | F-32 |
| DELETE | `/reviews/:id/comments/:commentId` | コメントの削除 | F-32 |
| PUT | `/reviews/:id/decision` | 判定を書く。Body `reviewer_name`（1〜50 文字）、`decision=approved\|changes_requested`、`comment`（任意、2000 文字以内） | F-32 |

入出力契約（認証は行わない。共有物の中身と除外は `spec/spec.md` §27.1、`spec/requirements/non-functional.md` §5）:

- `POST /projects/:id/reviews`: 成功時は更新後のレビュー一覧（201）。`export_id` が空・当該 Project の書き出しでない（存在しない Project も）は 404、書き出しファイルが無ければ 409、`note` が長すぎるは 422
- `POST /projects/:id/reviews/feedback`: 成功時は更新後のレビュー一覧（201）。ファイルなし・JSON として読めない・`format`/`version` が違う・判定やコメントが不正・1MB 超過は 422（何も保存しない）、`review_id` がこの Project のレビュー依頼でなければ 422、`response_id` が取り込み済みなら 409。取り込むと Chat Workspace のスレッドに AI の発言（表示名・判定・コメント件数）を追加する
- `PUT /projects/:id/review-comments/:commentId`: 更新後のレビュー一覧（200）。当該 Project のコメントでなければ 404
- `POST /reviews/import`: 成功時は `HX-Redirect: /reviews/:id`（201）。ZIP として読めない・`manifest.json` と `video.mp4|webm` 以外のエントリがある・形式やバージョンが違う・動画の署名が合わない・サイズ超過は 422（何も保存しない）、同じ `review_id` を読み込み済みなら 409
- `POST /reviews/:id/comments`・`DELETE /reviews/:id/comments/:commentId`・`PUT /reviews/:id/decision`: 更新後のレビュー画面のフラグメント（201 / 200）。コメントは 1 レビュー 200 件まで（超過は 422）、`timecode` が負・数値でない・24 時間超は 422、`decision` が不正・`reviewer_name` が空か長すぎるは 422。コメントや判定を変えると、書き出す応答の `response_id` が新しくなる

## 3. API ルート（Huma, JSON, `/api/v1`）

Lit Web Components 専用。HTMX リクエストは発行しない（`spec/architecture/overview.md` §7 の境界ルール）。

| Method | Path | 概要 | 利用コンポーネント |
|---|---|---|---|
| GET | `/api/v1/shots/:id` | Shot詳細（`id`/`scene_id`/`project_id`/`order_index`/`objective`/`duration_seconds`/`shot_spec`/`state`/`selected_take_id`（未選択なら省略））。存在しない Shot は 404 | `mam-video-player`, `mam-timeline-editor` |
| GET | `/api/v1/shots/:id/takes` | Take一覧（古い順。`label`（A, B, …）・`status`（candidate/ai_pick/selected/rejected）・`adopted`（Shot が使う Take）・`scores`（product/acting/camera/continuity/overall、各 0-100。最新 Judge の Issue から算出: 各軸 100 から high 35・medium 15・low 5 を Issue ごとに引く。未 Judge は null）・`verdict`・`resolution`/`fps`/`duration_seconds`・`proxied`・`video_url`/`thumbnail_url`/`judge_result_url`/`select_url`/`reject_url`、および Shot の `partial_url`） | `mam-take-comparator` |
| GET | `/api/v1/takes/:id/judge-result` | 最新のJudge Result（`result`/`overall_confidence`/`metrics`/`issues`（`time_range`/`frames`付き）/`preserve`/`repair_strategy`）。同一Takeの過去Judge履歴は含まない（最新1件）。Take が存在しない、または一度も Judge されていないとき 404 | `mam-video-player`（フレームマーカー重畳） |
| GET | `/api/v1/repair/jev-shadow-report` | Jev のシャドー実行の集計（`runs`/`errors`/`agreed`/`agreement_rate`（失敗を除く一致率）/`mean_confidence`/`mean_latency_ms`/`total_cost_usd`/`confidence_histogram`（0.1 刻み 10 区間）/`confidence_agreed`）。Jev による前段選別を有効にするかどうかの判断材料（`spec/architecture/model-adapters.md` §4.5・§11） | Repair Agent（F-14） |
| GET | `/api/v1/projects/:id/edit-timeline` | 現在の Edit Timeline（Shot順序/Cut Point/Transition）と最新の Editor Judge。Rough Cut がなければ 404。クエリ `branch_id`（省略時=主系列トランク）で Branch ごとの版を返す。クリップに `kind`・`caption`、判定の指摘に `need_shot`、応答に `request_shot_url` と `limits.max_caption_runes` を含む | `mam-timeline-editor` |
| — | `PUT /edit-timelines/:id`（アクションルート、§2.6） | Edit Timeline 保存（ドラッグ&ドロップ操作の反映）。`/api/v1` では提供せず、JSON API の GET が返す `save_url` へ PUT する（Lit は API クライアント経由で CSRF トークン付きで送る）。`branch_id` は M2 | `mam-timeline-editor` |
| GET | `/api/v1/health` | ヘルスチェック。`/healthz` と同一の応答（`ok`/`database`、DB 障害時は 503） | 外形監視（Lit は使わない） |
| GET | `/api/v1/openapi.json` | OpenAPI 3.1 スペック（Huma自動生成）。`/api/v1/openapi.yaml`・`/api/v1/openapi-3.0.json`・`/api/v1/openapi-3.0.yaml` は同内容の別形式、`/api/v1/schemas/:schema` は各型の JSON Schema | - |

### リクエスト/レスポンス例

```yaml
# POST /shots/:id/generate （アクションルート、フラグメント返却）
Request:
  additional_instruction: "もう少しカメラを寄せて"   # 任意（DIRECTOR MODE指示があれば反映）
Response: 202 Accepted
  # フラグメント: Shot状態=GENERATINGのInspector表示
  # 完了通知は /ws/shots/:id 経由
```

```yaml
# GET /api/v1/projects/:id/edit-timeline   （mam-timeline-editor。保存は PUT /edit-timelines/:id のアクションルート）
Response 200:
  Body:
    id: "et_01"                  # 表示している版。保存時に PUT のパスへ使う
    version_number: 5
    duration_seconds: 9.5
    readonly: false              # 編集できない段階（承認待ち・READY 等）では true
    save_url: "/edit-timelines/et_01"
    preview_url: "/edit-timelines/et_01/preview"
    transitions: ["cut", "fade", "fade_black", "dissolve"]
    limits: { min_clip_seconds: 0.5, max_transition_seconds: 2, max_clips: 200 }
    judge: { verdict: "re_edit", findings: [{ clip_index: 2, issue: "...", action: "..." }] }   # 省略あり
    clips:
      - shot_id: "shot_003"
        take_id: "take_010"
        label: "Scene 1 · Shot 1"
        in_point: 0.0
        out_point: 4.2
        start: 0.0
        transition: { type: "cut", seconds: 0 }
Errors:
  404: "Rough Cut がまだない"
```


```yaml
# GET /api/v1/takes/:id/judge-result
Response 200:
  Body:
    result: "fail"
    overall_confidence: 0.94
    metrics:
      face_identity:
        - character_id: "Character_A"
          similarity_mean: 0.71
          similarity_min: 0.42
          sampled_frames: 48
          undetected_ratio: 0.08
    issues:
      - type: "product_logo_deformation"
        severity: "high"
        time_range: "2.1-2.8s"
        frames: [63, 84]
      - type: "face_identity_drift"
        severity: "high"
        time_range: "3.0-3.9s"
    preserve: ["character", "acting", "background"]
    repair_strategy:            # PASS で戦略が無いときは省略
      action: "partial_regeneration"
      instructions:
        - "increase_product_reference_strength"
        - "preserve_logo_geometry"
Notes:
  metrics.face_identity_status: "measured" | "not_applicable"（顔参照を持つ Character が Shot にいない）| "unavailable"（顔モデル未配置・推論エラーで定性評価のみ）
  metrics.face_identity[]: 上記に加え evaluated（数値評価が成立したか。false は undetected_ratio が高い等で定性評価のみ）/ warn_threshold / fail_threshold / calibrated（参照画像でキャリブレーションした閾値か）
  issues[]: 上記に加え dimension（評価軸）/ character_id（face_identity_drift）/ description / lock_violation（LOCK 違反。true の Take は score に関係なく FAIL）/ brand_constraint_id（Brand / Safety Constraint の違反。`type=brand_constraint_violation`。`lock` の制約は lock_violation=true、`advisory` は severity low の警告）
  time_range: "開始-終了s"（秒）。frames: [開始フレーム, 終了フレーム]（ソース動画のフレーム番号、両端を含む）
```

## 4. WebSocket

| Path | 概要 |
|---|---|
| `/ws/shots/:id` | 生成中Shotの進捗（キュー待ち/生成中/Judge中/完了/失敗）をプッシュ通知。メッセージは JSON `{"shot_id","action"?:"generate\|candidates\|reference_pack\|judge\|repair","stage":"idle\|queued\|generating\|judging\|repairing\|done\|failed","message"?,"takes_done"?,"takes_total"?}`。候補生成中も `action=candidates` で、`stage` はこの列挙のまま（`queued` / `generating` / `judging` / `done` / `failed`）である。接続直後に生成中でなければ `stage=idle` を 1 通送る。同一オリジンの接続のみ受理し、存在しない Shot は 404。Judge 中は `action=judge` で `stage=queued\|judging\|done\|failed`（`stage` に `judging` を追加）。Repair 中は `action=repair` で `stage=queued\|repairing\|done\|failed`（`stage` に `repairing` を追加。Retry Policy の上限で止めたときは `done` とその理由の `message`） 生成中・Judge 中の Shot カードは `mam-video-player` が接続し、完了（done/failed/idle）の `progress-finished` でカードを再取得する。プレーヤーのないカード（Repair 中）は `shot.ts` が接続する。カード 1 枚につき接続は 1 本 |
| `/ws/projects/:id` | チャットの通知のみを配信する（Core State Machine 遷移・コスト更新・Editor→Reshoot ループの発火のための専用イベントは配信しない。これらの変化はチャットに投稿されたメッセージ・カードとして `message.created` / `card.updated` で届くか、画面が HTTP で取得する。コストは Cost Summary パネルが 15 秒間隔の HTMX ポーリングで更新する）。通知は本文を含まない JSON `{"type":"message.created","message_id":"<最新メッセージID>"}`（新着あり。クライアントは `GET /projects/:id/messages?after=` で取得）と `{"type":"card.updated","message_id":"<カードID>"}`（進捗カード等の更新。`GET /projects/:id/messages/:messageId` で取得）。同一オリジンの接続のみ受理する |

`lib/ws.ts` 経由で接続し、自動再接続（指数バックオフ、最大10回）を行う。接続してから 1 秒以内に閉じた接続は失敗として数える（開いてすぐ閉じるサーバーを 500ms 間隔で繰り返さず、10回で断念する）。断念した後、および再接続待ちの間は、ブラウザの `online`・`visibilitychange`（表示）または画面の「再接続」で直ちに再試行する。断念するとチャット・生成進捗（Shot カード・`mam-video-player`）は「接続が切れました」の通知（「再接続」「再読み込み」ボタン付き）を表示する。チャット画面は再接続後、新着メッセージに加えて、まだ変わりうるカード（未回答の承認・質問・エスカレーション、未完了ジョブの進行カード。サーバーが `data-card-pending` を付ける）を `GET /projects/:id/messages/:messageId` で取り直す（切断中の `card.updated` の取りこぼしを補う）（`spec/architecture/overview.md` 参照元 HALT 標準パターン）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-09-28 | 新規作成 | 機能要件（F-01〜F-28）と ER 設計を HALT の3ルートパターンへ落とし込み定義 |
| 1.1 | 2026-09-28 | World State（Character/Product/Location）のページルート・アクションルート（`GET /projects/:id/world-state`、`PUT /projects/:id/characters/:characterId` 等）を追加 | F-09/F-10（World State管理・差分反映）に対応するエンドポイントが未定義だったため（整合性レビュー指摘） |
| 1.2 | 2026-09-28 | `GET /projects/:id`（Main Workspace）の概要に `mam-video-player` の埋め込みを明記 | 他3つのLit Web Components（timeline-editor/take-comparator/branch-tree）は埋め込み先ページが明記されているのに `mam-video-player` だけ記載が無かったため（整合性レビュー指摘） |
| 1.3 | 2026-09-28 | `POST /shots/:id/reference-pack`（Keyframe/Reference Pack生成）を追加 | F-11（M1必須機能）に対応するエンドポイントが未定義だったため（整合性レビュー指摘） |
| 1.4 | 2026-09-28 | `POST /shots/:id/versions/:versionNumber/restore`（バージョン復元）を追加 | F-28（Reversible Decisions）の「復元」を実行するエンドポイントが未定義だったため（整合性レビュー指摘） |
| 1.5 | 2026-09-28 | `PUT /api/v1/projects/:id/edit-timeline` レスポンス例の `updated_at` を `created_at` に修正 | er.md の `edit_timeline_version` テーブルには `created_at` のみ定義され `updated_at` は存在しない（追記のみで上書きしない設計）ため、API例が実際のカラムと矛盾していた（整合性レビュー指摘） |
| 1.6 | 2026-09-28 | `POST /shots/:id/branch` のリクエストボディに `branch_point_type`（scene/shot/frame）・`offset_seconds` を追加 | UC-5が要求するScene/Frame単位の分岐点作成にShot専用パスしか対応していなかったため（整合性レビュー指摘） |
| 1.7 | 2026-09-28 | 復元エンドポイントをスコープ別に分割（Shot/Take用に加え `POST /projects/:id/world-state/versions/:versionNumber/restore`・`POST /branches/:id/versions/:versionNumber/restore` を追加） | F-28「Shot/Take/Branch/State」全対象の復元を単一のShotスコープパスでは表現できなかったため（整合性レビュー指摘） |
| 1.8 | 2026-09-28 | `PUT /projects/:id/locks`（Story/Music等ShotスコープでないパラメータのAUTO/MANUAL/LOCK設定）を追加 | F-05がShot外パラメータもLOCK対象とするのに対し `PUT /shots/:id/locks` しか存在しなかったため（整合性レビュー指摘） |
| 1.9 | 2026-09-28 | `GET /projects/:id/cost-summary`（Shot/Scene/Project別コスト集計）を追加 | components/overview.mdの`cost_summary_panel`・non-functional.md §6の「コスト可視化」に対応するエンドポイントが未定義だったため（整合性レビュー指摘） |
| 1.10 | 2026-09-28 | `PUT /projects/:id/locks` の概要から「等」を削除し「Story/Music という」に修正 | 対象を`target_type`=`story\|music`の2値に限定する記述と「等」（他にもあることを示唆）が自己矛盾していたため（品質レビュー指摘） |
| 1.11 | 2026-09-28 | `GET /projects/:id/world-state` に `branch_id` クエリを追加。`POST /branches/:branchId/world-state/versions/:versionNumber/restore`（Branch専用World State復元）を追加し、既存の `/projects/:id/world-state/.../restore` は主系列（トランク）専用と明記 | er.mdの`world_state_version`が`(project_id, branch_id, version_number)`UNIQUEとなったのに対し、既存エンドポイントにBranch指定手段がなくトランクとBranchの`version_number`衝突時に対象を一意特定できなかったため（整合性レビュー指摘） |
| 1.12 | 2026-09-28 | Character/Product/Location更新エンドポイントに `branch_id` クエリを追加 | er.md 1.10 で `character`/`product`/`location` に `branch_id` を追加したのに対し、更新対象BranchをAPIから指定する手段がなかったため（整合性レビュー指摘） |
| 1.13 | 2026-09-28 | `PUT /branches/:id`（Branch名称変更・World State参照更新、branch_version新規行生成）を追加 | er.mdの`branch_version`が「Branch自体の主要変更を追記のみで履歴化」としていたのに対し、2件目以降の生成契機となるエンドポイントが存在しなかったため（整合性レビュー指摘） |
| 1.14 | 2026-09-28 | Editor/Audio/Export/edit-timeline取得APIに `branch_id` パラメータ（Body/Query）を追加 | er.md 1.14で`edit_timeline_version`/`audio_timeline_version`/`export`にbranch_idを追加したのに対し、APIから対象BranchのTimeline/Exportを指定する手段が存在しなかったため（整合性レビュー指摘） |
| 1.15 | 2026-09-28 | `GET /api/v1/takes/:id/judge-result` の概要に「最新1件のみ返す」ことを明記 | er.md 1.16で`judge_result`のUNIQUE制約を削除し複数回のJudge履歴を保持可能にしたため、取得APIがどのレコードを返すか明示する必要があったため（整合性レビュー指摘） |
| 1.16 | 2026-09-29 | Series / Episode 用エンドポイント（§2.9）を追加。`GET /api/v1/takes/:id/judge-result` の応答例に `metrics` と `face_identity_drift` Issue を追加 | F-29（Series / Episode Memory, M2）と F-13 の Face Identity Similarity（`spec/spec.md` §12.4）に対応する API 契約がなかったため |
| 1.17 | 2026-09-29 | チャット中心UIに対応し、`GET /projects/:id` を Chat Workspace に変更、`GET /projects/:id/messages`・`POST /projects/:id/messages`・`POST /projects/:id/approvals/:messageId`（§2.1a）を追加。`POST /projects` の Body に最初の発言を追加。`GET /projects/:id/brief` を補助ビューに位置づけ直し | ユーザー要望「基本的にはただのチャット画面にしたい」（`spec/spec.md` §23）。既存の Shot 単位エンドポイントだけではチャットの発言・承認カード応答を受ける経路がなかったため |
| 1.18 | 2026-09-29 | Notes 用のページルート（`/projects/:id/notes`、`/series/:id/notes`）と §2.10 を追加。`POST /projects/:id/approvals/:messageId` に昇格提案カードの挙動を追記。`POST /series/:id/episodes` に Notes のコピーを追記（`POST /series` は Notes を登録しない） | F-31（Notes, M2）に対応する API 契約がなかったため |
| 1.19 | 2026-09-29 | `GET /projects/:id/messages/:messageId`・`GET /projects/:id/assets/:assetId` を追加。`POST /projects` を `multipart/form-data`（添付検証の上限を明記）にし、`PUT /projects/:id/confirmation-level` に `checkpoints` を追加。`/ws/projects/:id` のチャット通知ペイロードと Approval 応答の挙動（`HX-Trigger`、409）を明記 | M1「Project とチャット画面」（Issue #5）の実装で、カードの部分更新・添付ファイルの配信/検証・CUSTOM CHECK の選択・WebSocket ペイロードの契約が未定義だったため |
| 1.20 | 2026-09-29 | `GET /projects/:id/scenes` を追加。Scene/Shot 作成エンドポイントと `GET /projects/:id/scenes/:sceneId` の入力・エラー契約を明記 | M1「Reasoning LLM Adapter と AUTO パイプライン」（Issue #6）で Scene/Shot/Take 階層の表示・選択・作成 API の契約が未定義だったため |
| 1.21 | 2026-09-29 | `POST /shots/:id/generate` の Body・エラー（402/404/409/503）、`POST /shots/:id/reference-pack` の非同期応答、`/ws/shots/:id` のメッセージ形式を明記 | M1「Prompt Autopilot・Model Adapter・Reference Pack・生成オーケストレーション」（Issue #7）で生成 API と進捗通知の契約が未定義だったため |
| 1.22 | 2026-09-29 | §2.8 に M1 の入出力契約を追記（編集できる項目・応答・423/422/404 の条件、`branch_id` は M2 まで 404） | M1「World State（Basic）と State Extractor」（Issue #8）で `PUT .../characters/:id` 等の Body・応答・LOCK 時の挙動が名称レベルでしか定義されていなかったため |
| 1.23 | 2026-09-29 | `PUT /projects/:id/quality-policy`・`budget`・`production-template` の Body・エラー、`GET /projects/:id/settings`・`cost-summary` の表示内容を明記。`POST /projects/:id/takes/:takeId/archive`・`POST /projects/:id/storage/archive-unneeded`（不要 Take のアーカイブ）を追加 | M1「Production Template・Quality/Cost Policy・コスト可視化」（Issue #13）で設定 API の入力・エラー契約と、non-functional.md §4 の「不要 Take のアーカイブ/削除」に対応するエンドポイントが未定義だったため |
| 1.24 | 2026-09-29 | `POST /takes/:id/judge` の応答・エラー（202/404/409/503）、`GET /api/v1/takes/:id/judge-result` の 404 と応答項目（`preserve`・`metrics.face_identity_status`・`lock_violation` 等）、`/ws/shots/:id` の Judge 通知（`action=judge`・`stage=judging`）を明記 | M1「AI Video Judge（Face Identity Similarity 含む）」（Issue #9）で Judge の API 契約と進捗通知が未定義だったため |
| 1.25 | 2026-09-29 | §2.4 の `POST /shots/:id/continue`・`/continue/confirm` の入出力・エラー・チャットカードとの対応を明記 | M2「Continue」（Issue #17）で Body・応答・Continue カード（`ref_type=continue`）の回答方法・新 Scene を作る条件が名称レベルでしか定義されておらず、保守的に確定したため |
| 1.26 | 2026-09-29 | §2.2 に M1 の入出力契約（mode / locks / shot-spec / Inspector / director-command / チャットの DIRECTOR 解釈）を追記。`POST /shots/:id/generate` の LOCK・Video Model 固定時の挙動を追記 | M1「AUTO/MANUAL/LOCK と DIRECTOR MODE」（Issue #12）で、Body・応答・423/422/409/202 の条件、対象 Shot の解決順、LOCK の判定範囲が名称レベルでしか定義されていなかったため |
| 1.27 | 2026-09-30 | §2.8 に M2 の契約（`PUT /projects/:id/world-state/locks`、`PUT …/:entityId/reference`）を追記。`GET /projects/:id/references` と §2.11 Reference Library（`POST/DELETE /projects/:id/references`、`POST /shots/:id/references`）を追加 | M2「Character/Product Lock・Location State・Reference Library」（Issue #15）で、World State の属性単位 LOCK の切替とライブラリの登録・検索・再利用の API が定義されていなかったため。ライブラリは Project 内の画像を対象とし、Reference Pack への追加は Shot 単位のピン留めにした |
| 1.28 | 2026-09-30 | `POST /takes/:id/repair` の応答・エラー（202/404/409/503）・Retry Policy の適用・AUTO MODE の自動ループ、`/ws/shots/:id` の Repair 通知（`action=repair`・`stage=repairing`）、`GET /api/v1/repair/jev-shadow-report` を明記 | M1「Repair/Retake Loop」（Issue #10）で Repair の API 契約と進捗通知が未定義だったため |
| 1.29 | 2026-09-30 | §2.4 の Take 選択・除外・部分採用の入出力とエラー、`/takes/:id/video`・`/thumbnail`、`GET /api/v1/shots/:id/takes` の項目、AI Pick の動作を追記 | M1「Take システムと補助ビュー」（Issue #11）で Body・エラー・スコア算出・AUTO/DIRECTOR の扱いが名称レベルでしか定義されていなかったため |
| 1.30 | 2026-09-30 | `POST /takes/:id/repair` の Partial Regeneration がすべての Issue に時間範囲があるとき、区間をつなぎ込んだ候補 Take も足すことを追記。`POST /shots/:id/director-command` に時間範囲を名指しした再生成（`partial_regenerate`）の動作・エラーを追記 | M2「Partial Regeneration」（Issue #16）で、新しい Take の数・選択への影響・DIRECTOR MODE からの起動が名称レベルでしか定義されていなかったため |
| 1.31 | 2026-09-30 | §2.9 に `POST /series`・`POST /series/:id/episodes`・`PUT /series/:id/characters/:characterId` の入出力とエラー、`PUT /series/:id/style` を追記。`GET /series/:id`（Series ページ）を追加 | M2「Series / Episode Memory」（Issue #21）で、Series の入出力契約・Style の更新経路・画面が未定義だったため |
| 1.32 | 2026-09-30 | 復元エンドポイント（§2.4）の入出力・エラー・LOCK との関係を定義。`GET /takes/:id/trace`・`GET /branches/:id/versions`・`GET /projects/:id/bundle`・`POST /projects/import` を追加。Branch 系の復元パスの Branch ID を `:id` に統一 | F-27/F-28 とデータポータビリティ（Issue #20）の入出力契約が未定義だった。Gin は同じパス位置のワイルドカード名を 1 つしか許さず、`/branches/:branchId/...` と `/branches/:id/...` を併存できないため |
| 1.33 | 2026-09-30 | §2.4 に M2 の Branch 契約（`POST /shots/:id/branch`・`GET/PUT /branches/:id`・`GET /api/v1/projects/:id/branches`・チャットの `create_branch`）を追記。§2.8 の `branch_id` は Branch が存在すれば有効とした | M2「Branch（別展開の並行生成）」（Issue #18）で Branch を実装したため |
| 1.34 | 2026-09-30 | §2.6 に M1 の Editor / Audio / Final QC / Export の挙動（Rough Cut・タイムライン保存の版管理と 409/422・プレビュー・Editor Judge・音声・Final QC・書き出し・ダウンロード）を追記。Edit Timeline の保存を `PUT /edit-timelines/:id`（アクションルート）に確定し、`GET /api/v1/projects/:id/edit-timeline` の応答例を更新（旧 `PUT /api/v1/projects/:id/edit-timeline` は提供しない） | M1「簡易 Editor・簡易 Audio・Final QC・Export」（Issue #14）の実装に合わせるため |
| 1.35 | 2026-09-30 | §2.7 の `POST /projects/:id/brand-constraints`・`DELETE /brand-constraints/:id` の入出力・エラー（422/404）と、Brand 違反の 403 の扱い、Judge Issue の `brand_constraint_id` を追記 | M2「Safety / Brand Constraints」（Issue #23）の実装に合わせるため。403 の意味は `spec/spec.md` §26.1 |
| 1.36 | 2026-09-30 | ページルートに `GET /projects/:id/analytics`・`GET /analytics`（Production Analytics）を追加。人間の介入の記録は既存のアクションルート（Take の選択・除外・Repair・生成・Shot Spec 編集・LOCK 変更・Timeline 保存・承認の変更依頼・モード切替）が成功時に `human_intervention` へ追記する（新しいアクションルートは無い） | M3「Production Analytics」（Issue #29）の MVP Success Metrics の表示と、既存テーブルにない人間の介入・上書きの記録のため |
| 1.37 | 2026-09-30 | §2.6 に M3 の Editor 契約（Advanced 編集項目の反映、`need_shot`、`POST /edit-timelines/:id/request-additional-shot` と Retry Policy、Branch ごとの Edit Timeline、クリップの `kind`/`caption`）を追記 | Advanced Editor Agent と Editor → Reshoot Loop（Issue #24）の実装に合わせるため |
| 1.38 | 2026-09-30 | §2.7 に Brand Templates の `POST /projects/:id/brand-templates`・`POST /projects/:id/brand-templates/:templateId/apply`・`DELETE /projects/:id/brand-templates/:templateId` を追加し、`POST /projects` と `POST /series/:id/episodes` に任意の `brand_template_id` を追記 | M3「Brand Templates」（Issue #27）の API 契約がなかったため。保存・適用はコピーで更新は非伝播（`spec/requirements/functional.md` UC-9 拡張） |
| 1.39 | 2026-09-30 | §2.6 に M3 の Audio Director 契約（`POST /projects/:id/audio/generate` の Quality Policy ごとの層・`branch_id`・予算・エラー、`POST /projects/:id/audio/voices`）を追記 | M3「Audio Director（フル機能）」（Issue #25）の実装に合わせるため |
| 1.40 | 2026-10-01 | §3 の `GET /api/v1/shots/:id` の応答項目と 404 を明記（実装を追加） | 仕様にあるが未実装だった（#31） |
| 1.41 | 2026-10-01 | §4 の `/ws/projects/:id` を実装の現状（チャット通知のみ。状態遷移・コスト・Reshoot の専用イベントは配信せず、コストは Cost Summary パネルのポーリングで更新）に合わせた | 仕様は状態遷移・コスト更新・Reshoot 発火のプロジェクトイベントを列挙していたが、実装は `message.created` / `card.updated` しか配信していなかったため（Issue #96） |
| 1.42 | 2026-10-01 | `mam-branch-tree` を廃止し Branch ツリーを Templ（`branch_tree`）でサーバー描画に変更。`GET /api/v1/projects/:id/branches` を削除し、`GET /branches/:id`・`PUT /branches/:id` の HTMX 応答にツリーの `hx-swap-oob` を同梱、`HX-Trigger: branches-changed` を廃止 | Branch ツリーは D&D・Canvas・WebSocket を使わず HALT の Lit 使用基準に該当しないため（Issue #40） |
| 1.43 | 2026-10-01 | `PUT /projects/:id/confirmation-level` の `checkpoints` を `script`/`rough_cut`/`final` に限定（それ以外は 422） | 他の確認ポイントは承認カードを出す段が無く、選んでも要求されないため（#87） |
| 1.44 | 2026-10-01 | `POST /projects/import` の展開を、`data.json` が参照するファイルだけ・合計 32 GB と空き容量の範囲内・一時ディレクトリ経由に限定することを追記 | 参照されない大きなエントリでディスクを埋められたこと、並行インポートが先行 Project のファイルを消すことを防ぐため（Issue #51、#52） |
| 1.45 | 2026-10-01 | チャット画面の再接続後の補完に、`data-card-pending` が付いたカードの再取得を追記 | 切断中の `card.updated` を取りこぼすと承認・進行カードが古いままになるため（Issue #75） |
| 1.46 | 2026-10-01 | `PUT`・`DELETE /credentials/:provider` を追加し、設定画面の `credential_panel`（API キーの設定済み/未設定表示・保存・削除）を仕様化 | 仕様（設定画面の API キー設定）に実装が無かった（#90） |
| 1.47 | 2026-10-01 | WebSocket の再接続仕様に、開いてすぐ閉じる接続を失敗として数えること、断念時の通知と再試行手段（再接続ボタン・`online`・`visibilitychange`）を追記 | 断念しても利用者に知らされず更新が止まること、即閉じるサーバーへ永久に再接続することを防ぐため（Issue #76） |
| 1.48 | 2026-10-01 | `POST /projects/:id/export` が `branch_id` を受け付け、その Branch の Edit/Audio Timeline を書き出して `export.branch_id` に記録することを M1 の挙動表に明記（実装を追加） | 表に `branch_id` があるが実装はトランク固定だった（#97） |
| 1.49 | 2026-10-01 | §2.11 の `POST /shots/:id/references` の応答を Shot カードから Shot Inspector に改めた | 実装はピン留め後の Reference Pack を Inspector の References 欄に表示するため Shot Inspector を返しており、仕様と食い違っていたため（Issue #100） |
| 1.50 | 2026-10-01 | `/ws/shots/:id` の接続は Shot カード 1 枚につき 1 本（プレーヤーがあればプレーヤー、なければ `shot.ts`）と明記 | 同じ進捗 WebSocket を 2 本張っていたため（Issue #38） |
| 1.51 | 2026-10-01 | §2.4 の `POST /shots/:id/branch` の Body 名を実装と §2.8 に合わせて `branch_point_type` から `point_type` に修正（検証エラーの項目名も `point_type`）。Issue #40 で削除済みの `GET /api/v1/projects/:id/branches` の応答説明を削除 | 表と M2 契約で項目名が食い違い、表の名前で送ると黙って既定の shot になるため（Issue #101） |
| 1.52 | 2026-10-01 | §1 に `GET /`・`GET /healthz`・`GET /static/*filepath`、§3 に `GET /api/v1/health` と OpenAPI の別形式・`schemas` ルートを追記 | 実装（`router/pages.go`・`router/api.go`）にあり仕様に無かった（#36）。ヘルスは 503 に統一済み（#66） |
| 1.53 | 2026-10-01 | `POST /shots/:id/continue` の解析が、起点の Take の最終フレーム画像を Continue Agent に渡すことを明記（ffmpeg が無いときはテキストのみ）。`spec/spec.md` §15 にも追記 | 実装はテキストだけから State を推測しており、仕様の「最終フレームを解析」と食い違っていたため、最終フレームの抽出を実装した（Issue #110） |
| 1.54 | 2026-10-01 | `POST /edit-timelines/:id/request-additional-shot` は、全 Shot の生成開始に成功したときだけ `reshoot_request` を `requested` にして GENERATING へ進む。開始失敗時はラウンドを残さず EDIT_JUDGING のままエラーを返す | 開始失敗が未完了ラウンドを残し、同じ版への再要求が拒否されていたため（Issue #120） |
| 1.55 | 2026-10-01 | `MaxBody` の 1MiB は Content-Type ではなくルートで外す。アップロード以外（`PUT /edit-timelines/:id`、`POST /shots/:id/branch` を含む）は `multipart/form-data` でも 1MiB を超えれば本文を読まず 413 | Content-Type を multipart と偽ると本文上限が外れていたため（Issue #125） |
| 1.56 | 2026-10-01 | `POST /shots/:id/takes/partial` のつなぎ目を、`media.Compose` に合わせて「0.25 秒。最短区間の 1/3 の方が短いときは短縮。音声は全区間にあるときだけクロスフェードし、1 本でも無音なら出力は無音」と明記 | 仕様は常に 0.25 秒（音声も）だったが、短い区間では縮み、音声の無い Take が混ざると無音にしていたため（Issue #127） |
| 1.57 | 2026-10-01 | `GET /shots/:id` と `spec/spec.md` §23.3 から Prompt を Shot Inspector の常設項目から外し、モデル別プロンプトは `GET /takes/:id/trace`（「生成の経緯」）で確認すると明記 | #104 以降も製品仕様と API 概要が Prompt を Inspector の常設項目のまま残していたため（Issue #135） |
| 1.58 | 2026-10-01 | 冒頭の JSON API の利用者から廃止済みの `mam-branch-tree` を外し、Lit は Video Player / Timeline Editor / Take Comparator の 3 つとした | #40 で Branch ツリーは Templ の `branch_tree` に移したが、冒頭の 1 文だけが残っていたため（Issue #136） |
| 1.59 | 2026-10-01 | §2.2 の `POST /projects/:id/messages` から「Branch 作成は M2 まで未対応」を外し、`create_branch` を実行する §2.4 の契約に揃えた | チャットの Branch 作成は実装済みで、M1 契約の文だけが M2 契約と食い違っていたため（Issue #117） |
| 1.60 | 2026-10-01 | `/ws/shots/:id` の `action` に `candidates` を加えた。候補生成の `stage` は既存の列挙のまま | 進捗は `action=candidates` を送るが、仕様とフロントの型が `generate\|reference_pack\|judge\|repair` だけだったため（Issue #124） |
| 1.61 | 2026-10-01 | MAX QUALITY の候補生成は、予算が 3 Take 分あるとき 3 戦略すべてを並列生成し、1〜2 本だけ収まるときはその本数を 202 で、1 本も収まらなければ 402 と書いた | 仕様は常に全 Candidate だったが、実装は予算に収まる本数だけ作っていたため（Issue #126） |
