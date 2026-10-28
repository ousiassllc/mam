# 画面仕様: 初回ガイド（ウォークスルーと初期セットアップ）

> **状態: 実装済み**（Issue #863）。ルートと応答は [../api/endpoints-onboarding.md](../api/endpoints-onboarding.md)。設定の保存先・モーダル・一覧の部品は [settings-screen.md](settings-screen.md)・[shared-ui-parts.md](shared-ui-parts.md) をそのまま使い、新しく持つのは「初回かどうかのフラグ」と、ガイドの枠・紹介の文面・ffmpeg の確認・Mac の案内だけである。

起動直後にプロジェクト一覧が出るだけでは、何を設定すれば使えるのかが初めての人に分かりにくい。API キーも ChatGPT のログインも ffmpeg も無くても mam は起動するが、機能は使えなかったり縮退したりする。**初回だけ**、ウォークスルー（mam で何ができるか）→ 初期セットアップ（順番に入れる）→ プロジェクト一覧の順に案内する。

## 1. 決定事項

Issue #863 の本文で決まっていること（オーナー指示を含む）と、この仕様での扱い。

| # | 決定 | この仕様 |
|---|------|---------|
| 1 | ウォークスルー（数ページ、「次へ」で進む）で、企画〜撮影〜編集〜QC の流れと AUTO / DIRECTOR を紹介する | §3 の 3 ページ |
| 2 | 初期セットアップ（順番に入れる）: API キーか ChatGPT ログインのどちらか（最低 1 つ）→ LLM・画像/動画モデルの割り当て（任意）→ ffmpeg の確認と入れ方 → Mac のみ Codex のパスと ComfyUI の案内 | §4 |
| 3 | 各ステップは「あとで」でスキップでき、最後にプロジェクト一覧へ進む | §5 |
| 4 | 設定画面からいつでも案内をもう一度開ける | §7 |
| 5 | 初回かどうかは**専用のフラグ**で決める（API キーの数などから推測しない）。最後まで進むかスキップしたら、以降は出さない | §2 |
| 6 | 入力した値は既存の設定画面と同じ場所に保存する（API キーは OS キーリング `mam` / `api-key:<provider>`、LLM は `llm.json`、モデルは `models.json`） | §4。ガイドは**設定画面と同じ部品・同じルート**を埋め込む。ガイド専用の保存経路は作らない |
| 7 | Mac の ComfyUI は同梱しない。公式の Comfy Desktop を入れてもらい、その接続先 URL を `/settings/local-models` に入れる流れを案内する | §4.4。案内の文だけを出す（同梱の ComfyUI の取得は #854 の範囲で、この Issue では扱わない） |

## 2. 初回の見分け方（フラグ）

- 保存先は**データフォルダの `onboarding.json`**。`{"completed": true}`。`mcp.json` などと同じく `service/settingsfile` で読み書きする（ほかのキーを消さない・一時ファイルから置き換える・権限 0600）。ファイルが無いのが初回。
- フラグを立てるのは `POST /onboarding/finish` だけ（最後のステップの「完了」か、どのページの「ガイドを閉じる」）。ガイドのページを開いただけでは立てない（途中で閉じたら、次の起動でもう一度出る）。
- 一度立てたら下ろさない。設定画面からの再表示（§7）で最後まで進んでも、スキップしても、フラグは立ったまま。
- `GET /` は、フラグが立っていなければ `/onboarding` へ 302 で送り、立っていればプロジェクト一覧を返す。`GET /projects` とほかの画面は送らない（直接開く道を塞がない）。HTMX の要求も送らない。フラグを読めなかったとき（ファイルが壊れている）は、案内で止めずにプロジェクト一覧を返し、エラーはログに残す。
- **この Issue の帰結**: フラグの無い既存の利用者は、更新後の最初の起動で一度だけガイドが出る。「API キーが入っているか」などから推測して出さない、がオーナーの指示である（Issue #863「初回の見分け方」）。「ガイドを閉じる」で消える（フラグが立つ）。
- ガイドの途中で設定の別ページへ移ってから `/` へ戻ると、フラグが立つまではガイドの先頭に戻る。ガイドは設定の部品を埋め込み、設定の別ページへ出さないことでこれを避ける（Mac の案内の `/settings/local-models` へのリンクだけは例外で、戻ったときは「ガイドを閉じる」で抜けられる）。

## 3. ウォークスルー（紹介 3 ページ）

保存するものは無い。文面の根拠は [../spec.md](../spec.md) §1・§4・§5。

| 順 | slug | 見出し（`h2`） | 中身 |
|----|------|---------------|------|
| 1 | `flow` | mam でできること | 作りたい動画を伝えると、AI が制作チームとして **企画 → 脚本・絵コンテ → 撮影（キーフレームと動画の生成）→ 品質判定（Judge）と撮り直し → 編集・音声 → 最終 QC** までを進める。1 つのプロンプトから動画を出すだけのツールではない |
| 2 | `modes` | 2 つの進め方 | **AUTO MODE**: 作りたい動画を指定すると、AI が完成まで自律的に進める。**DIRECTOR MODE**: 自分が監督になり、自然言語で演出を指示する（「もう少し寄って」など）。同じプロジェクトでいつでも切り替えられる（AUTO で初稿 → 気になる Scene だけ DIRECTOR → AUTO へ戻す） |
| 3 | `prepare` | 使い始める前に | この後のセットアップで入れるものの一覧: ① API キーか ChatGPT ログイン（どちらか 1 つ。無いと生成が動かない）② モデルの割り当て（任意）③ ffmpeg（無くても起動する。Judge・Take のプレビュー・部分採用が縮退する）、Mac のみ ④ Codex と ComfyUI。どれも「あとで」で飛ばせて、設定画面から入れられる |

## 4. 初期セットアップ（4 ステップ。Mac 以外は 3 つ）

各ステップの見出しは `h2`。中身は設定画面の部品をそのまま置く（ガイド専用の入力欄を作らない）。保存・検証・応答の決まりは設定画面と同じ（[settings-screen-routes.md](settings-screen-routes.md) §2）。

### 4.1 `credentials` — API キーまたは ChatGPT ログイン（最低 1 つ）

- 上に**状態の行**（`#onboarding-credentials-status`、`role="status"`）。API キーが 1 件以上、または ChatGPT のログイン（公式・画像生成・Codex 経由のどれか）が 1 つ以上あれば「設定済み」（API キー n 件、ChatGPT ログイン済み）。どちらも無ければ「API キーも ChatGPT ログインもまだありません。どちらか 1 つを入れると生成が使えます。あとで設定することもできます」（警告）。キーストアを読めなければ「状態を確認できません」（警告）。
- 状態の行は、モーダルが閉じたとき（`modal-close`）に `GET /onboarding/credentials/status` で取り直す。ログインの待ちが終わってモーダルが閉じたときも同じ。
- 下に設定画面の「API キーとログイン」の中身をそのまま置く（`organisms/settings/credentials` の API キーのカードとモーダル、`organisms/settings/chatgpt` の 3 つのログインのカード）。キーは `PUT /credentials/:provider` で OS キーリングに保存される。

### 4.2 `models` — モデルの割り当て（任意）

- 「既定の割り当てのままでも進めます」を最初に書く。既定のプロバイダーのキーが無いときは、ここで割り当てを変えると使える（例: ChatGPT ログインだけなら、プロバイダー `chatgpt-subscription` を選ぶ）と書く。
- 「テキスト生成（LLM）」と「画像・動画モデル」は、それぞれ `<details>`（閉じた状態）に、設定画面の中身（`organisms/settings/llm`・`organisms/settings/media`）をそのまま置く。保存は `PUT /settings/llm/agents/:agent` などで `llm.json`・`models.json` に書かれる。

### 4.3 `ffmpeg` — ffmpeg の確認

- サーバーの `PATH` から `ffmpeg` と `ffprobe` を探す（`exec.LookPath`。mam が実際に使う探し方と同じ。`internal/media`）。実行はしない。
- **見つかった**: 「ffmpeg・ffprobe が見つかりました」（緑）と、それぞれのパス。案内は出さない。
- **見つからない**: どちらが無いかを書き（警告）、無いと何が縮退するかを 1 文（Judge・Take のプレビュー・部分採用）、そして **OS ごとの入れ方**を出す。OS は mam が動いている OS（`runtime.GOOS`）。

| OS | 案内 |
|----|------|
| Windows | `winget install Gyan.FFmpeg` を PowerShell で実行する |
| macOS | Homebrew で `brew install ffmpeg` を実行する（`ffprobe` も入る）。入れたのに見つからないときは、この mam の `PATH` に Homebrew（`/opt/homebrew/bin`）が入っていない可能性がある、と書く（Finder から起動したときの `PATH` の問題は #856 の範囲で、この Issue では探し方を変えない） |
| ほか（Linux） | お使いのパッケージマネージャーで ffmpeg（ffprobe を含む）を入れる、と書く |

- どの OS でも「入れたあとは mam を一度終了して起動し直してください（ffmpeg は起動時に探します）」と書き、「もう一度確認する」のリンク（同じ URL の GET）を置く。
- コマンドは `<code>` で、コピーしやすい 1 行にする。

### 4.4 `mac` — Mac の準備（Codex・ComfyUI）。macOS のときだけ

macOS 以外ではこのステップはない（ナビにも出ず、URL は 404）。

- **Codex のパス**（ChatGPT のサービスがあるときだけ）: Codex を自分で入れた人が、その実行ファイルの絶対パスを入れる欄（`name="path"`、現在の値を描く）。「ターミナルで `which codex` を実行すると分かります」と書く。保存は設定画面と同じ `chatgpt.Service.Codex().SetCustomPath`（`PUT /onboarding/codex-path`。応答はこの欄の下の結果の 1 文だけ）。空で保存すると指定を解除する。Codex を使わないなら飛ばしてよい。
- **ComfyUI は外部のものを使う**: mam は ComfyUI を同梱しない。公式の **Comfy Desktop**（macOS 13 以降・Apple Silicon。`.dmg` から入れるだけで Python と MPS の設定まで自動）を入れ、その接続先 URL を mam の設定「ローカル・代替モデル」（`/settings/local-models`）に入れる。手順の番号つきの案内:
  1. 公式の手順（https://docs.comfy.org/installation/desktop/macos）で Comfy Desktop を入れて起動する。
  2. Comfy Desktop の接続先 URL を確かめる（たいてい `http://127.0.0.1:8000`）。
  3. mam の「設定 → ローカル・代替モデル」で接続先を追加し、種類は ComfyUI、URL に 2 の値を入れる。
- 3 へのリンク（`/settings/local-models`）は、ガイドを閉じたあとでも設定画面から開けると添える。

## 5. 画面の枠とボタン

```
┌ main（max-w-4xl、p-8）──────────────────────────────────────────────────┐
│ はじめに                                     ← h1（全ページ共通）         │
│ ご紹介 1 2 3 ／ セットアップ 4 5 6 7         ← nav（現在は aria-current） │
│ ステップ 4 / 7                                                          │
│ API キーまたは ChatGPT ログイン              ← h2（ステップの見出し）      │
│ …中身…                                                                  │
│ [戻る]            [あとで] [次へ]   ガイドを閉じてプロジェクト一覧へ        │
└─────────────────────────────────────────────────────────────────────────┘
```

- 1 ページに 1 ステップ。ページ全体を返す（HTMX の断片ではない）。`<title>` は「はじめに: <ステップの見出し>」。
- **進み具合**: `<nav aria-label="進み具合"><ol>`。ステップの番号と名前。今のステップは `aria-current="step"`（色だけでなく太字と枠で示す）。
- **ボタン**（`<a href>`。フォームの送信ではない）: 「戻る」（最初のステップには無い）。ウォークスルーのページは「次へ」だけ。セットアップのステップは「あとで」（そのステップで何も入れずに次へ。中立の見た目）と「次へ」（主の見た目）。**最後のステップ**は「次へ」の代わりに「完了してプロジェクト一覧へ」、「あとで」も同じく完了する（どちらも `POST /onboarding/finish`）。
- **最後のステップ以外のどのページにも**（最後は「あとで」「完了」が同じ働きをする）、下に「ガイドを閉じてプロジェクト一覧へ」（`POST /onboarding/finish` のフォームの送信。スキップ。フラグを立てて `/` へ）。`hx-sync="this:drop"` と `hx-disabled-elt` を付ける（二重送信防止。`double_submit_test`）。
- 応答はヘッダー `HX-Redirect: /`（本文なし）。書き込みに失敗したら 500 とトースト「保存できませんでした」で、ページに留まる（フラグは立たない）。
- セットアップのステップの下に「あとで飛ばしても、設定画面からいつでも入れられます」と書く。
- ガイドのページにはモーダルの置き場（`modal.Host`）を持つ（`credentials`・`models` のステップ）。トーストの置き場はレイアウトにある。

## 6. 配置

| 置き場所 | 内容 |
|----------|------|
| `service/onboarding/` | フラグ（`Completed`・`Complete`）、ffmpeg の確認（`FFmpeg`）、OS（`OS`） |
| `web/handler/onboarding/` | `GET /onboarding`・`/onboarding/:step`・`/onboarding/credentials/status`、`POST /onboarding/finish`、`PUT /onboarding/codex-path`、`GET /` の初回の送り（`FirstRun`） |
| `web/organisms/onboarding/` | 枠・ウォークスルー・各ステップ（サービスを import しない。ハンドラーが渡す値で描く） |
| `web/pages/onboarding_page.templ` | ページ（レイアウトと枠） |
| `web/paths/onboarding.go` | パス |
| `router/settingsroutes` | `Services.Onboarding`（nil ならガイドも `GET /` の送りも無い）。ルートの登録 |

## 7. 設定画面からの再表示

- 設定の概要（`GET /settings`）の最後に「初期セットアップの案内」の区画（`h3`）を置き、文と「案内をもう一度開く」のリンク（`<a href="/onboarding">`）を出す。フラグの有無に関わらず出す。ガイドを出さない構成（`Services.Onboarding` が無い）ではこの区画も無い。
- `/onboarding` を直接開いても同じ（フラグが立っていても開ける。フラグは変わらない）。

## 8. テストで確かめること

- フラグ: ファイルが無い → 未完了、`Complete` で `onboarding.json` に `completed:true`、もう一度呼んでも同じ、ほかのキーを残す、壊れたファイルは読めずエラー。
- `GET /`: 未完了なら 302 `/onboarding`、完了なら一覧（200）、`Services.Onboarding` が無ければ常に一覧、HTMX は送らない、壊れたフラグのファイルは一覧。
- `POST /onboarding/finish`: CSRF なしは 403、成功で `HX-Redirect: /` とフラグ、その後の `GET /` が一覧、書けないとき 500。
- ステップ: 全 slug が 200 で `h1` が 1 つ、未知は 404、`mac` は macOS のときだけ。ナビの `aria-current`。「あとで」・「次へ」の行き先、最後のステップの完了。
- ffmpeg: 見つかる / 片方だけ無い / 両方無い。Windows は `winget`、macOS は `brew`、ほかは汎用の文。見つかったときは案内を出さない。
- `credentials`: 状態の行（未設定の警告・API キーのみ・ChatGPT のみ）、キーストアの失敗。設定画面と同じカード・モーダルの経路で、フェイクのキーストアの `api-key:<provider>` に保存されること。
- `models`: LLM・画像動画の表が出て、保存が `llm.json`・`models.json` に書かれること（一時ディレクトリ）。
- `mac`: パスの保存が `SetCustomPath` に届く、空で解除、不正は 422。
- 設定の概要に再表示のリンクがあり、`Services.Onboarding` が無ければ無い。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-28 | 新規作成。初回ガイド（ウォークスルー 3 ページ・セットアップ 4 ステップ）の画面仕様 | Issue #863 |
| 1.1 | 2026-10-28 | 「ガイドを閉じる」は最後のステップには置かないこと、進み具合の番号は 1 から振ることを実装に合わせた | Issue #863 の実装 |
