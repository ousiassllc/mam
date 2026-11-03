# 画面仕様: ジョブ一覧（Issue #980）

> **状態: 実装済み（Issue #980。2026-11-03。メモリ上の作業の一覧入りは Issue #1019）。** 「ジョブ一覧を作るかどうか」はオーナーが「作る」と決めた（#962 から切り出した Issue）。**読み取り専用**: ジョブを止める・やり直す・消すボタンは置かない。**永続化も足さない**（既にある `job` テーブルを読むだけ。メモリ上の作業は §2 の「メモリ上の作業」のとおりメモリだけで、再起動で消える）。**実機（Windows の同梱デスクトップアプリ・macOS）での見た目と、同梱 ComfyUI で動く実際の導入・生成を並べた表示は未確認**（テストはフェイクの導入と生成テストのバッチで確かめた）。

[overview.md](overview.md) §3 の `pages` に `jobs_page`（`GET /jobs`）を足す。モデルの導入・生成テスト・Shot の生成や判定など、**裏で動く作業**を、実行中のものと直近に終わったものを 1 か所で見られる画面。動画の書き出し・音声の作成・Editor の判定・ワークフローの実行・Codex の導入・ffmpeg のダウンロードも、同じ一覧に出る（Issue #1019。`job` テーブルの行ではなく、メモリ上の追跡から）。ジョブの進み具合は導入のカード・生成テストの履歴・チャットの進捗カードも出しているが、それぞれの画面に行かないと分からなかった。この画面は、それらを**一覧にして、元の画面へ行く入口**になる。ルートは [../api/endpoints-jobs.md](../api/endpoints-jobs.md)。

## 1. 決めたこと

| # | 決定 | 理由 |
|---|------|------|
| 1 | データ源は**ジョブキューの `job` テーブル**（`domain.Job`。種類 `generate`・`judge`・`repair`・`plan`・`model_install`・`gentest`）。サービス層の読み取り専用 `service/joblist` が読む | 背景の作業はすべてこのテーブルを通る。MCP が持つ履歴（`mcp_job_history`）は MCP から始めた作業だけで、画面から始めた導入・生成テストを含まない |
| 2 | **`job` テーブルを通らない作業は、メモリ上の追跡を一覧のソースに足して出す**（Issue #1019）: 動画の書き出し・音声の作成（声・BGM・効果音・ミックス）・Editor の判定（メモリ上の実行）、ワークフローの実行、Codex の導入、ffmpeg のダウンロード。`job` テーブルには**書かない**（`domain.JobKind` も足さない） | `job` に行を持たせると「ジョブの永続化」になる（やらないと決めた）。6 種はどれも再開できず（再起動で中断）、行き先も一点で拾える入口（`EditorService` の 3 段・`workflows.Service.StartRun`・`codex.Service.StartInstall`・`ffmpegdl.Service.StartInstall`）が 1 つずつなので、その入口で `worktrack`（メモリ上の記録）に 1 件ずつ記録し、一覧の組み立て時に `Entry` に写す。永続化の要否はオーナー判断として残す（要るなら別の Issue で `job` の行に移す。`Entry` の形は変わらない） |
| 3 | 読む件数は「**新しい 50 件**＋終わっていない（`queued`・`running`）ものはすべて」（`joblist.Newest`）。テーブルは削られないため、全件は読まない | 画面が遅くならない。動いているものは古くても必ず出る |
| 4 | 並びは**開始が新しい順**（まだ始まっていないものは積んだ時刻） | 直近のものが上 |
| 5 | **元の画面へのリンクだけ**を持つ（「開く」）。キャンセル等の操作は無い | Issue の範囲外。操作は元の画面にある（導入の「中断する」・生成テストの「取り消し」） |
| 6 | 更新は `molecules/refresh.Bar`（[model-manager-screen.md](model-manager-screen.md) の「更新ボタン」）。動いているジョブがあるあいだは 2 秒ごとに自分で取り直し、無くなると止まる | 他の画面（生成テストの履歴・モデルマネージャー）と同じ流儀 |
| 7 | 左メニューの「ジョブ」（`sidenav.Jobs`。モデルマネージャーの次）。主な画面は 9 項目（プロジェクト・生成テスト・モデルマネージャー・ジョブ・ワークフロー・ノード・レビュー・制作指標・設定）、トップ画面の「ほかの画面」は 8 画面 | [side-menu.md](side-menu.md) §1・[home-screen.md](home-screen.md) |

## 2. 構成

| 区分 | 部品 | 責務 |
|------|------|------|
| pages | `JobsPage`（`pages/jobs_page.templ`） | `AppLayout` ＋ `pagehead.Main(Medium)` ＋ `pagehead.Header("ジョブ", "", …)`（説明は「?」ヘルプ）＋ `jobs.Section` |
| organisms（`organisms/jobs/`） | `Section`・`List`・`Row`・`Progress` | 一覧。`#jobs-list`（更新ボタンが差し替える区画）。organism はサービスを import しない（ハンドラーが表示用の値に直して渡す） |
| handler（`web/handler/jobs/`） | `Handlers`・`Register` | `GET /jobs`・`GET /jobs/list`。`joblist.Entry` を `organisms/jobs.Row` に写す（状態のバッジの文字・色、行き先の URL） |
| service（`service/joblist/`） | `Service.Recent`・`Deps.Works`・`Deps.Hidden` | 読み取り専用。`job` テーブルの行を、種類ごとに「対象」「進み具合」「行き先」を足して `Entry` にする。サービスのジョブの動きは変えない。`Deps.Works`（`[]worktrack.Source`）が返すメモリ上の作業も同じ `Entry` にして、テーブルの行と**開始が新しい順に 1 本に混ぜる**（`tracked.go`）。`Entry.Kind` は `domain.JobKind` か `worktrack.Kind` の文字列 |
| メモリ上の記録（`internal/worktrack/`） | `Tracker`・`Record`・`Run`・`Source` | 葉のパッケージ（`domain` だけを import）。持ち主が `Tracker.Begin` で記録を始め、`Run.Progress`・`Run.Succeed`・`Run.Fail`・`Run.Cancel`・`Run.End(err)` で進め・終える。終わった記録は新しい 20 件（`worktrack.Keep`）だけ残し、動いているものは必ず残す。**永続化しない**。`nil` の `Run` に呼んでも何も起きない |
| repository（`repository/jobrecent/`） | `Repository.ListRecent` | 「新しい N 件＋終わっていないもの」を 1 つの SELECT で読む（`repository` が行数の基準値を超えているため別のパッケージ） |
| routes（`router/settingsroutes`） | `Services.JobList` | `nil` ならルートが無い（他の画面のサービスと同じ）。`internal/app` が組み立てる |

### 種類ごとの表示

| 種類（`domain.JobKind`） | 画面の種類名 | 対象 | 進み具合（実行中だけ） | 「開く」の行き先 |
|---|---|---|---|---|
| `model_install` | 導入 | モデルの名前（カタログ）・`ComfyUI エンジン`・Hugging Face のリポジトリ名 | 導入の進捗（`modelmanager` の進捗の追跡。%・いま取っているファイル）。再起動をまたいだジョブには無い | `/model-manager` |
| `gentest` | 生成テスト | バッチのモデル名（3 つまで。超えると「ほか n モデル」） | 終わった結果の数 / 全体（`2 / 4 件`）。動いている ComfyUI のステップがあれば % に足し、補足の文に ComfyUI の進み具合（下の「ComfyUI の進み具合の文」）を足す | `/generation-test/history` |
| `generate`・`judge`・`repair` | Shot の生成・Take の判定・Take の修正 | Shot が属する Project の名前 | 動いているローカル ComfyUI の進み具合（`live` の進み具合。#966・#1002）。補足の文は下の「ComfyUI の進み具合の文」。クラウドのモデルや準備中は出さない | Project の Scene / Shot（`/projects/:id/scenes`） |
| `plan` | 企画・脚本の作成 | Project の名前 | なし | Project のチャット（`/projects/:id`） |

- 状態は文字のバッジ（待機中・実行中・完了・失敗・中止。色だけで伝えない）。`中止` は生成テストのバッチが取り消されたとき。生成テストの状態は**バッチの状態**を使う（キューの行は取り消しを知らない）。
- 失敗は理由を 1 行（`logging.Redact` で鍵などを伏せる）。企画（`plan`）のジョブが LLM の API キー未設定で失敗したときだけ、理由の下に「続けるには」の案内（`data-testid="jobs-next-steps"`。`planadvice.For`。Issue #1001）を足す: (1) 設定で鍵を登録するか ChatGPT にログインしてチャットに何か送る、(2) World State・Series の「登場人物を追加」で登場人物を直接作る。理由に「登場人物の下書き」の文（Brief の添付画像から下書きを作ったとき。`planadvice.DraftNote`。Issue #1022）があるときは、(2) が「World State の登場人物のうち『下書き』の印がついたものに髪型・衣装などを書き足す（書き足すと印は消える）。足りない登場人物は『登場人物を追加』で作る」になる。理由の文面（`agent/llm/failmsg` のもの）は書き換えない。チャットの進捗カードにも同じ案内が出る。
- 対象の Project・Shot・バッチ・モデルが**もう無い**とき（Project の削除・カタログに無いモデル・年齢確認がオフの成人向けモデル）は、対象と「開く」を出さない。一覧全体は壊さない。
- 年齢確認がオフの間、成人向けモデルの名前は対象に出さない（Issue #992）。カタログのモデルの導入（`model:`）は `Model` が隠し、Hugging Face の導入（`hf:`）は、ジョブのピン（`hf.nfaa`＝NFAA のリポジトリ。導入そのものもこの印で断る）が立っているとき、リポジトリ名を出さない。生成テストのバッチは、`Hidden`（`HidesModel`）に当たるモデルを名前の列から外す（全部が当たれば対象は空）。ゲートを開くと、同じ一覧にまた名前が出る。

### メモリ上の作業（Issue #1019。`job` テーブルの行を持たない 6 種）

| 種類（`worktrack.Kind`） | 画面の種類名 | 記録する入口（1 か所） | 対象 | 進み具合（実行中だけ） | 「開く」の行き先 |
|---|---|---|---|---|---|
| `export` | 動画の書き出し | `EditorService.Export`（条件・設定の確認のあと、`renderExport` の前から、`CreateExport` まで） | Project の名前 | `%` なし。「動画を書き出しています（比率 / 解像度 / コーデック）・経過 3分20秒」 | `/projects/:id/edit` |
| `audio` | 音声の作成 | `EditorService.GenerateAudioWith`（AUDIO の確認のあと、`SaveAudioTimeline` まで。ラフカットの書き出し・声・BGM・効果音・ミックスを含む） | Project の名前 | `%` なし。「音声（声・BGM・効果音・ミックス）を作っています・経過 …」 | `/projects/:id/edit` |
| `editor_judge` | Editor の判定 | `EditorService.JudgeTimeline`（`runJudge` の呼び出しの間。AUTO の連鎖・Reshoot からの呼び出しも同じ入口） | Project の名前 | `%` なし。「Editor が Edit Timeline を判定しています・経過 …」 | `/projects/:id/edit` |
| `workflow_run` | ワークフローの実行 | `workflows.Service.StartRun`（画面・MCP とも同じ）。ID は実行の記録（`run.json`）と同じ | ワークフローの名前（モデルが年齢確認でオフの成人向けなら出さない） | ComfyUI の `progress`（`Percent`・`Message`）。準備中は 0 % | `/workflows` |
| `codex_install` | Codex の導入 | `codex.Service.StartInstall` | `Codex` | ダウンロード 0〜70 %・展開 70〜100 %（設定の導入カードと同じ値）・いまの段階の文 | `/settings/api-keys`（「ChatGPT でログイン」のカード） |
| `ffmpeg_install` | ffmpeg のダウンロード | `ffmpegdl.Service.StartInstall` | `ffmpeg` | ダウンロード 0〜85 %・展開 85〜98 %・確認 98〜99 %（設定の導入カードと同じ値）・いまの段階の文 | `/settings/ffmpeg` |
| `voice_train` | 声の学習 | `voicetrain.Service.Start` | 登場人物の名前 | 段階 0〜98 %（text 0〜5・hubert 5〜15・semantic 15〜20・sovits 20〜60・gpt 60〜98。見た目だけの値）・いまの段階の文（エポック n/m・経過・その段階の残り時間） | Project の登場人物は `/projects/:id/world-state`、Series の正本の登場人物は `/series/:id`（[local-runtime.md §18](../architecture/model-manager/local-runtime.md)） |

- 状態は `実行中`・`完了`・`失敗`・`中止`（`待機中` は無い: 記録は動き出してから始まる）。`中止` は Codex・ffmpeg の「中断する」と、書き出し・音声・判定で要求が取り消されたとき（ワークフローの実行は要求から切り離して動くので、時間切れは失敗）。ほかの失敗は持ち主が使う人向けに書いた文（`Explain`・`audioError`・`aiError`・`workflows.reasonOf`）のまま `Entry.Error` に入れ、`logging.Redact` で鍵を伏せる（共通のエラー表示は別の Issue #1010。ここでは変えない）。
- `%` を言えない作業（書き出し・音声・判定は ffmpeg や LLM の 1 回の呼び出しで、数える段が無い）は、`<progress>` の棒を出さず、いま何をしているかの文と**動き出してからの経過時間**（`経過 3分20秒`）を出す。2 秒ごとの取り直しで時間が進むので、長い作業が止まっていないと分かる。値は作らない（`Progress.Percent` が負 = 不明。organism の `Progress.Known()`）。
- **対象の Project が無い・年齢確認でモデルが隠れる**ときの扱いは、上の共通の箇条書きと同じ（対象と、Project の行き先は出さない。ワークフローの「開く」は `/workflows` で、Project を持たないので残す）。
- **永続化しない・再起動で消える**: 記録は持ち主のメモリにあり、mam を再起動すると 6 種とも一覧から消える（作業そのものも再起動で中断され、再開しない）。`job` テーブルの 6 種は再起動後も出る。ワークフローの実行はディスク上の `run.json`（`workflows.Service.Run`）が別にあるが、一覧は読まない（メモリ上の記録だけ）。
- 書き出し・音声・判定は、**開始を断られた**もの（Project の状態が違う・別の段が動いている・ffmpeg が無い）は記録しない（動いていない作業を失敗として並べない）。

## 3. 画面

```
ジョブ（h1）
実行中 2 件                          最終更新 10:42:03 [↻]
┌──────────────────────────────────────────────┐
│ 導入 [実行中]                     2026-11-03 10:41 │
│ Flux.2                                              │
│ ████░░░░░░  40%・flux.safetensors                   │
│ 開く                                                │
└──────────────────────────────────────────────┘
┌──────────────────────────────────────────────┐
│ 生成テスト [失敗]                 2026-11-03 10:20 │
│ Qwen-Image、Kling                                    │
│ （失敗の理由）                                       │
│ 開く                                                │
└──────────────────────────────────────────────┘
```

- 見出しは h1 `ジョブ`（`pagehead`）。「?」のヘルプに、何が対象か（上の §2 の表のとおり裏で動く作業。書き出し・音声・Editor の判定・ワークフローの実行・Codex の導入・ffmpeg のダウンロードを含む。mam を再起動すると、`job` テーブルにない作業は一覧から消える）と、見るだけで止められないこと、「開く」の意味を書く。
- 1 行は `<li data-testid="jobs-item">`。カードの面は `rounded-lg border border-neutral-800 bg-neutral-900 p-3`（新しい色・影は足さない。`color-rules.md` の許可リストの内）。
- 進み具合は `<progress>`（`aria-label` が行の名前）と `%`・補足の文。`%` を言えない作業（§2 の「メモリ上の作業」）は棒と `%` を出さず、補足の文（いまの作業と経過時間）だけを出す。長い名前・長い文は `break-words` で折り返す（既存の行と同じ）。
- **空**: ジョブが 1 件も無いと「ジョブはまだありません。…」（`data-testid="jobs-empty"`）。
- **エラー**: `job` テーブルを読めなかったときは、一覧の中に赤い文（`role="alert"`・`data-testid="jobs-error"`）。応答は 200（エラーページにしない。`main.ts` の失敗のトーストも出さない）。原因は応答に出さず、ルーターのロガーに残す。「空」の文は出さない（読めなかったのに「ジョブは無い」と言わない）。更新ボタンと自動更新（§4）も続ける（読めたら自然に復帰する）。

## 4. 更新ボタンと自動更新

- `refresh.Bar(paths.JobsList(), "jobs-list", 時刻)`。ボタンは `id="jobs-list-refresh"`、`GET /jobs/list` を `#jobs-list` に `outerHTML` で差し替える。時刻は「最終更新 hh:mm:ss」。
- 一覧は、終わっていないジョブが 1 件でもあれば `hx-get`・`hx-trigger="every 2s"` を持ち、すべて終わった応答には持たない（止まる）。`job` テーブルを読めなかった応答も持つ（読めない間も止めず、読めたら一覧に戻る）。ボタンの要求は `poll-guard` に止められず、進行中のポーリングの要求に取って代わる（`refresh.Bar` の契約）。
- **二重送信**: 変更する操作がないので、`hx-sync` の入ったボタン（取り直し）だけ。

## 5. アクセシビリティ

- ランドマークは `<main>`（`pagehead.Main`）の中の `<section aria-label="ジョブ一覧">`。見出し h1 は 1 つ。
- 「開く」のリンクは `aria-label="開く: <種類> <対象>（<開始時刻>）"`、進捗は `<種類> <対象>（<開始時刻>）の進み具合`。同じ「開く」が並ぶので、行の名前をつける（#793。開始時刻を足して、同じ種類・同じ対象の 2 行も区別する）。見える文字が先頭にある。
- 状態は文字。キーボードは Tab でリンクと更新ボタン。フォーカスの輪は共通の `indigo-400`。
- 進捗の `<progress>` に `role="status"` は付けない（2 秒ごとの取り直しで読み上げが続くのを避ける）。

## 6. 試験

| 試験 | 内容 |
|------|------|
| `repository/jobrecent/jobrecent_test.go` | 終わっていないものは古くても入る・新しい N 件・新しい順・空 |
| `service/joblist/joblist_test.go` | 導入（モデル名・待機中→実行中の進捗→完了）・エンジン・未知のモデル・Shot の生成/判定（Project 名と行き先・失敗の理由の秘密を伏せる・無い Shot）・企画・生成テストのバッチ・件数の上限と動いているものの残り・テーブルの失敗 |
| `service/joblist/tracked_test.go` | メモリ上の 6 種の種類名・対象・行き先・隠れるモデルと無い Project（対象を出さない）・実行中（`%` ありは `%`・無しは経過時間）→完了・失敗（秘密を伏せる）・中止・テーブルの行と開始が新しい順に混ざる |
| `web/handler/jobs/jobs_test.go` | 空・更新ボタンの属性・実行中の導入の一覧と取り直し・終わったらポーリングが止まる・失敗の理由と操作が無いこと・テーブルが読めないときのメッセージ（200・原因を出さない・ポーリングが残る）・メモリ上の作業の行（棒は `%` がある作業だけ・長い名前の折り返し・「開く」の名前と行き先・実行中はポーリング、終われば止まる） |
| `internal/worktrack/worktrack_test.go` | 実行中→完了・失敗・中止・終わった後は変わらない・`nil` の `Run`・終わった記録の件数の上限と動いているものは残る・時刻 |
| `media/ffmpegdl/service_test.go`・`chatgpt/codex/integration/manage_test.go`・`service/workflows/workflowstest/run_works_test.go`・`router/editor_works_test.go` | 持ち主ごとの記録: 動いている間は実行中（導入・ワークフローは `%`・文つき）→完了・失敗（画面と同じ文）・中止（ffmpeg）。Editor の判定は LLM の応答を止めて実行中を見てから完了、音声・書き出しまで新しい順、失敗した判定は失敗、開始を断られた段は記録しない |
| `web/a11y/jobs_list_test.go` | リンクと進捗の名前・状態が文字 |
| `web/sidenavtest/sidenav_test.go`・`organisms/sidenav/nav_test.go` | 全項目（ジョブを含む）が 200・自分の画面で `aria-current`・順序 |
| `web/layout_fixture_test.go`・`static/src/components/layout/layout.wtest.ts` | 左メニューの見本（`layout-sidenav.html`。9 項目）で横に溢れない・幅 |

## 7. 未確認・今後

- Windows 実機の同梱エンジンの導入・生成を実行中に並べたときの見た目と、macOS。
- 画面の読み上げ（NVDA・VoiceOver）での聞こえ方。
- 6 種の記録を、再起動をまたいで残すかどうか（`job` の行にするかどうか）はオーナー判断として残している（現状は残さない）。残すなら、`worktrack.Source` を `job` の行に置き換えるだけで、`Entry` と画面は変わらない。書き出し・音声・判定の `%`（ffmpeg の進み具合を読む）は未実装で、今は経過時間だけ。
- **ComfyUI の進み具合の文（Issue #1002）**: `<ノード> ステップ <step> / <max_step>・経過 <時間>・このノードの残り約 <時間>`（例: `KSampler ステップ 12 / 20・経過 3分20秒・このノードの残り約 1分10秒`）。ノード名が分からなければ省き、残り時間が見積もれない間（ノードがまだ進んでいない）は残りの句ごと省く（値を作らない）。**経過は ComfyUI に渡してからの時間**で、モデルの読み込み中は行に進み具合が出ない（ステップが報告されるまで `progress` が無い）ので、この文は最初のステップのあとに出る。残りは**そのノードだけ**（Wan 2.2 のようにサンプラーが 2 つあるとノードごとに数え直す）で、ジョブ全体の残りではない。MCP の `get_job`・`get_generation_test` の `progress`（`node`・`step`・`max_step`・`percent`・`elapsed_seconds`・`eta_seconds`）と同じ値から作る（`service/joblist/live.go`）。ポーリング（2 秒）で更新される。

- `job` テーブルは削られない（既存）。画面は 50 件までしか読まないので影響しないが、テーブル自体の整理は別の話。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-03 | 新規作成（実装済み） | Issue #980。オーナーの決定「作る」。`refresh.Bar` を置く画面としてのジョブ一覧 |
| 1.1 | 2026-11-03 | 各行の「開く」に安定した id（`jobs-open-<ジョブ ID>`。`organisms/jobs.Row.ID`）を付けた。2 秒ごとの一覧の差し替えでも、htmx が同じ id の要素へフォーカスを戻す（`focus-restore-request.wtest.ts`） | Issue #980 の統合レビュー指摘。「開く」のフォーカスが 2 秒以内に失われていた |
| 1.2 | 2026-11-03 | 読めなかった応答にもポーリングを残す。`job` の並びを `COALESCE(started_at, created_at) DESC, id DESC`（LIMIT の副問い合わせも同じ）に揃える | 統合レビュー指摘（#980）。読めない 1 回でポーリングが止まり復帰しなかった。並びが積んだ順で、仕様の「開始が新しい順」と食い違っていた |
| 1.3 | 2026-11-03 | 進み具合の行（`%` と説明）を、長いファイル名でも行の幅で折り返す（`break-words`）。測るのは `layout/long-names.wtest.ts`（[model-manager-screen.md](model-manager-screen.md) の「長い名前は折り返す」） | Issue #1003 |
| 1.4 | 2026-11-03 | 年齢確認がオフの間、`hf:` の導入（NFAA のリポジトリ・成人向けの導入記録）と、`Hidden` に当たる生成テストのモデルの名前を対象に出さない | Issue #992。`model:` の導入だけが隠れ、`hf:` のリポジトリ名と生成テストのモデル名はゲートに関係なく出ていた |
| 1.5 | 2026-11-03 | 動いている ComfyUI の進み具合の文に、ノード名・経過時間・そのノードの残り時間を足した（生成テストのバッチの行にも出す） | Issue #1002。ステップの数だけでは、長い生成が進んでいるのか分からなかった |
| 1.6 | 2026-11-03 | 企画のジョブが LLM の API キー未設定で失敗したときだけ、失敗の理由の下に「続けるには」の案内を出す（§2 の「種類ごとの表示」の失敗の行） | Issue #1001 |
| 1.7 | 2026-11-03 | 企画の失敗の理由に登場人物の下書きの文があるときは、「続けるには」の (2) を「下書きを直す」案内にする（§2 の失敗の行） | Issue #1022 |
| 1.8 | 2026-11-03 | 動画の書き出し・音声の作成・Editor の判定・ワークフローの実行・Codex の導入・ffmpeg のダウンロードを一覧に出す（決定 2 の変更）。`job` テーブルには書かず、持ち主のメモリ上の記録（`internal/worktrack`）を `joblist.Deps.Works` が読み、テーブルの行と混ぜる。`%` を言えない作業は棒を出さず、いまの作業と経過時間を出す（`Progress.Percent` が負）。再起動で消えることを明記 | Issue #1019 |
| 1.9 | 2026-11-04 | 声の学習（`voice_train`）を、メモリ上の追跡の種類に足した（Issue #936）。再起動をまたがず、ジョブ一覧から開くと登場人物の画面へ行く | GPT-SoVITS の fine-tune |
