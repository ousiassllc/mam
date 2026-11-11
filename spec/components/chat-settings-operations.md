# チャットからの設定操作（#1140）

「mam はチャットだけで全部できる」ための部品。今まで設定画面（GUI）か MCP だけでできた設定の操作を、アプリ内チャット（`POST /projects/:id/messages`）からもできるようにする。実装は `backend/internal/service/chatops/`（契約）と、その下の操作ごとのパッケージ。画面・MCP と**同じサービス層の関数**を呼び、ロジックは複製しない。

## 0. 共通の約束

### 0.1 確認してから実行する

- **変更する操作は、必ず先に確認カードを出す**（`kind=question`・`ref_type=operation`・`ref_id=<操作の種類>`。#1139 の予算と同じカード。ボタンは実行する操作を `CardBody.option_actions` に持つ）。カードに答えるまで何も変わらない。「やめる」「いいえ」は何も変えない。
- **全体の設定は、全プロジェクトに効くことをカードに書く**（API キー・プロバイダー・モデルの導入・モデルと LLM の割り当て・LoRA の取り込み。Project ごとの設定ではなく、アプリ全体の設定）。
- **同じ操作の新しいカードが出ると、古いカードは答えられなくなる**（`latest[ref_id]`。409）。カードは 1 回しか答えられない。
- **何も変わらない操作**（すでに同じ設定、ダウンロード用のリンクを出すだけ）は、カードを出さず、そう答える。
- 実行できない理由（設定ファイルが起動オプションで固定されている、モデルが未導入、ffmpeg が無い、など）は、カードを出さずに理由をそのまま答える。

### 0.2 どのモード・状態でも受ける

操作は `commandinterpreter.Spec` の `Global` を持つ。DIRECTOR MODE だけでなく AUTO MODE でも、Brief を作り終えたあとは、どの状態でも受ける。**Brief を作っている最中（DRAFT）に受けるのは、初めて使うときに最初に要る設定（API キー・クラウド動画モデルのプロバイダー・LLM の割り当て。`Spec.Setup`）と、§0.4 の拒否だけ**で、それ以外の発言は Brief への答えとして Brief の抜き出しに回す（「動画は縦型の15秒にして」「参照画像を使って」「出演者はプロのモデルを入れて」は Brief の指示で、モデルの割り当てやモデルの導入ではない）。発言が求めていないのにモデルが操作を返したときは捨てる（`Spec.Asked`。キーワードの検査）。`Asked` は、操作の**対象を名指ししている**ときだけ真になる（「画像モデル」「動画のモデル」「用途」「LLM」などの語と、行う動作の組み合わせ。「動画」「画像」「入れて」だけでは足りず、「モデルさん」「プロのモデル」のような人のモデルは数えない）。

### 0.3 LLM が無くても受ける操作

LLM の API キーが未設定でも、「OpenAI の API キーを設定したい」は読み取れる（`Spec.Detect`。モデルを使わない決定的な規則）。LLM が未設定のとき、Command Interpreter は `Detect` が読めた操作だけを実行する。それ以外は従来どおり「LLM が設定されていないため…」と返す。

### 0.4 意図的にチャットでは行わないもの

次は**拒否**し、設定画面から行うよう伝える（`commandinterpreter.RegisterRefusal`）:

- ffmpeg・FaceFusion の実行パス（実行されるプログラムを AI や発言に選ばせない）
- MCP のトークン・待ち受けアドレスの管理（エージェントが自分のアクセスを管理しない）

### 0.5 構成

| 場所 | 役割 |
|---|---|
| `commandinterpreter.Spec` + `Register` | 操作の登録（プロンプトの例と説明、モデルの答えの検査 `Clean`、発言が求めているか `Asked`、モデルを使わない読み取り `Detect`、`AnyMode`・`Confirm`・`Global`・`Immediate`）。操作の引数は `Operation.Args`（操作のパッケージが持つ構造体の JSON） |
| `chatops.Handler` | `Question`（確認カードの中身。何も変えない）と `Apply`（承認のあとに、画面・MCP と同じサービス層の関数で実行） |
| `chatops.SecretSubmitter` | 秘密の値（API キー）を受け取る操作だけ。カードの入力欄から専用のエンドポイントで受ける |
| `chatops.FileOffer` | 画像・動画以外のファイルが添付されたとき（LoRA）に、そのファイルを引き取る |
| `chatwire.Connect(services, chatwire.Settings{…})` | アプリの起動時に、操作をサービスにつなぐ（`backend/internal/app/app.go`。実装は `service/chatwire/*.go`）。つながっていない環境では「この環境では、その設定をチャットから変更できません」と答える |

## 1. API キーとプロバイダー

実装: `backend/internal/service/chatops/apikey`（`set_credential`・`set_video_provider`）、キーの検査 `chatops/secretguard`。

### 1.1 API キー（`set_credential`）

- 発言: 「OpenAI の API キーを設定したい」「Claude のキーを入れ替えたい」。サービス名は `service.CredentialProviders` の ID・名前・通称（Claude・Gemini・fal.ai …）から引く。名指しが無い・曖昧なときは「どのサービスの API キーですか？」と聞き返し、カードは出さない。LLM が無くても読み取れる（§0.3）。
- **キーはチャットの本文に出さない。** 確認カードが**マスクされた入力欄**（`type=password`・`autocomplete=off`・値を埋め戻さない）を持ち、「保存」で**専用のエンドポイント** `POST /projects/:id/messages/:messageId/secret`（フィールド `secret`。CSRF あり）に送る。カードの本文・`option_actions` にキーは入らない。カードには「はい」ボタンは無く、入力欄の「保存」が確認である（押すまで何も保存しない）。
- 保存は `CredentialService.Save`（設定画面・MCP `set_credential` と同じ。OS のセキュアストレージ、再起動なしで効く。空・空白を含む・長すぎるキーは 422 で、カードは開いたまま）。成功すると、スレッドには**キーを含まない**発言（「「OpenAI」の API キーを入力しました（値はチャットには残しません）」）と、保存した旨（全プロジェクト共通）が残り、カードは回答済みになる。
- **キーがどこにも残らない**: 値は `ChatService.SubmitSecret` から `CredentialService.Save` に渡るだけで、チャットの履歴・イベント（WebSocket）・トレース・ログ・エラー文・応答の HTML のどれにも書かない。テストは、保存後に DB の全テーブルとログ、スレッドの全メッセージに値が無いことを確かめる（`service/chatops/chattest/credential_test.go`・`secret_test.go`）。
- **発言にキーらしき文字列が含まれていたとき**（`secretguard`）: 次のどれかに当たると、その発言は**伏せた形で記録**し（「［API キーらしき文字列は伏せました］」）、モデル（Brief の抜き出し・Command Interpreter）には渡さず、カードで入力するよう案内する。
  - 既知の形: `sk-…`・`sk_…`・`AIza…`・`hf_…`・`r8_…`・`id:secret`・`Bearer …`・JWT（`eyJ….….…`。MiniMax など）・「キー/token の後ろの長い英数字」。
  - **形が決まっていないキー**（Kling の 32 桁の英数字、Runway の `key_` + 16 進、Luma、Seedance・Flux の UUID、Sync Labs など）: 発言全体が空白を含まない 24 文字以上の 1 語（`A-Za-z0-9_.-`）、または文中にそのような語（英字と数字の両方を含み、パスや URL の一部でないもの）があるとき。**モデルの名前は鍵として扱わない**: カタログのモデル ID と、カタログが固定しているファイルの名前（`.safetensors` など）と**完全に同じ語**だけは、そのまま Command Interpreter に渡す（例 `ltx-2.5-22b-distilled-nvfp4`。発言の「このモデルで作って」が伏せられないように）。語の形（小文字とハイフン・拡張子）での判定はしない（形だけが同じ本物のキーを通してしまうため）。上の既知の形（`sk-…` や UUID など）はこれとは別に先に読む。
  - **キーを聞くカード（`set_credential`）が開いている間**（最新のカードに返信が無い）: 空白を含まない 8 文字以上の ASCII の 1 語は、形が分からなくてもキーとして扱う。そのカードへの返信は、「やめる」などの断り以外はすべて伏せて記録し、読み取らない。
- 伏せて記録した発言は、**カードへの返信としては記録しない**（`reply_to_message_id` を持たない）。キーのカードは開いたままで、あとから入力欄にキーを入れて保存できる。
- **チャットの発言以外の入口も同じ**: 新規プロジェクトの最初の発言（`ProjectService.Create`）、Shot への指示（`DirectorService.DirectorCommand`）、承認カードのコメント（`ChatService.RespondApproval`）、単発生成のチャットの発言（`gentest/chat.Service.Send`。伏せた形をスレッドに出し、LLM にも伏せた形を渡す）も `secretguard.Redact` を通し、見つかれば伏せた形を記録して、Brief の抜き出し・Command Interpreter には渡さない（承認の決定そのものは実行する）。

### 1.2 クラウド動画モデルの API プロバイダー（`set_video_provider`、#1136）

- 発言: 「Kling は OpenRouter 経由で使いたい」「公式に戻して」。公式（`official`）・`openrouter`・`fal`・`replicate`。
- 確認カードは、変更前後の呼び出し先、そのプロバイダーに必要な API キーが設定済みか、全プロジェクトに効くことを書く。承認で `modelsettings.Service.SaveVideoProvider`（設定画面・MCP `set_video_provider` と同じ）。すでにその設定ならカードは出さない。`--model-config` で固定されているときは理由を答えてカードは出さない。
- 選んだ先の API キーが未設定なら、承認の答えのあとに §1.1 のカードを続けて出す。

## 2. モデルの導入

実装: `backend/internal/service/chatops/install`（`install_model`・`install_model_engine`・`install_hf_file`・`install_local_runtime`）、つなぎ込み `service/chatwire/install.go`、テスト `chatops/install/installtest`・`service/chatops/chattest/install_test.go`・`service/chatops/chattest/install_real_test.go`（実物のモデルマネージャーに mmtest の偽ホストをつないだ通し）。いずれも**アプリ全体の設定**（`Global`・`Confirm`・`AnyMode`）で、どのモードでも、Brief を作り終えたあとは受ける。Brief を作っている最中（DRAFT）は受けず（`Setup` ではない。`AsksSetup`）、その発言は Brief への答えとして抜き出しに回る（§0.2）。サービス層は画面（`/model-manager`）・MCP と同じ `modelmanager.Service`・`localruntime.Service` で、導入の可否の判断も実行もそこで行う（ロジックは複製しない）。導入はモデルを読む LLM が要る（`Spec.Detect` は無い）。

### 2.1 共通の流れ

- 発言: 「FLUX.1 schnell を入れて」「ComfyUI のエンジンを導入して」「GPT-SoVITS をインストールしたい」「huggingface.co/owner/name の safetensors を入れて」。**モデルの返事は名前でしかない**: `Asked`（導入・インストール・ダウンロード・入れて・install と、モデル・エンジン・実行環境・Hugging Face の語や ID の語が同時にある発言だけ）に通らない操作は捨てる。`args` に入っている `commit`・`terms_version`・ライセンスは信用しない（カードの `Question` がサービスから読み直し、カードのボタンだけが持つ）。
- **確認カードを必ず先に出す。** カードに答えるまで、何も取得・記録しない。カードには、導入するもの・大きさ（ダウンロード、これから取得する分、導入先の空き容量）・ライセンスと商用利用・導入先で足りないものと、**「導入はアプリ全体の設定です。全プロジェクトで使えるようになり、ディスクの空きとネットワークを使います」**を書く。承認のボタンは、カードが解決した ID（コミット・ビルド）を持つ（`Card.Op`）。「やめる」「いいえ」、無関係な返信では何も導入されない。
- **断れることは、カードの前に答える**（カードは出さない）。文言は MCP のツールと同じ（`mmview.Refuse`・`RefuseRuntime`）: 同梱の ComfyUI が使えない OS・`--model-config` で設定ファイルが固定・導入先に書き込めない・エンジン未導入（「先に ComfyUI エンジンを導入してください」）・空き容量が足りない（「必要 3.4 GB、空き 1.0 GB」）・導入がすでに動いている・追加ノードが足りない・Hugging Face のトークンが要る（`hfguide.NoToken`）・成人向けの年齢確認の前。すでに導入済み（最新）なら「すでにその内容です。変更しませんでした。」（`ErrNoChange`）。
- **承認すると** `Apply` が MCP の `install_*` と同じ関数（`InstallModel`・`InstallEngine`・`InstallHFFile`・`localruntime.Install`）を**1 回**呼ぶ。同じ導入がすでに動いていれば新しいジョブは作らず、そのジョブを返す（「すでに導入中でした」）。サービスが断ったとき（承認までに状態が変わった、など）は「実行できませんでした: 〜」とその理由を答え、進捗カードは出さない。
- **進捗カード**: 承認の答えのあとに、`kind=progress`・`ref_type=job`・`ref_id=<ジョブ ID>`・本文「「〜」を導入しています」のカードが続く。カードの下の行（`ThreadMessage.Detail`）は進捗プロバイダー（`reg.Progress` → `install.Detail`）が出す: 「42% ・ダウンロード中（1.2 / 3.4 GB）」「95% ・展開中（300 / 500 MB）」「順番待ち（まもなく始まります）」「導入が完了しました」「導入に失敗しました: 理由」。ジョブはローカルの実行環境 → モデルマネージャーの順に引き（`localruntime.JobStatus`・`modelmanager.JobStatus`）、どちらも知らない ID には `""` を返して他のプロバイダーに任せる。カードはジョブが終わるまで 2 秒ごとに読み直される（既存の仕組み）。 **読み上げ**: 取り直している間（`Detail` があり、ジョブが終わっていない間。`molecules.readsItself`）、カードの要素（`MessageFrame` の `<article>`）に `aria-live="off"` を付ける。スレッドは `role="log" aria-live="polite"` で、2 秒ごとの `outerHTML` の置き換えが毎回「追加」として読まれるのを避けるため。終わったカードには付けず、スレッドへの追加として 1 度だけ読まれる。
- 名前が複数に当てはまると、候補を 1 つずつのボタンにしたカードを出す（最大 6 件。それ以上は「もう少し具体的な名前で」）。ボタンを押しても導入は始まらず、その 1 件の確認カード（上の流れ）が続く（ボタンの操作は `pick=true`）。当てはまるものが無ければ「カタログに「〜」というモデルはありません」とだけ答える。カタログに無い ID・名前が導入に渡ることはない。名前の照合は、ID の完全一致 → ID・名前の表記ゆれを無視した一致（「FLUX.1 schnell」と「flux1-schnell」）→ 部分一致の順。

### 2.2 カタログのモデル（`install_model`、MCP `install_model`）

- カードに出すもの: 名前・ダウンロードの大きさ（これから取得する分が違えばそれも）・導入先の空き容量・ライセンスの名前と（`ID 版`）・**商用利用**（`license.commercial`: 商用OK／条件つき（条件）／商用NG／不明）・注意・使えない地域・VRAM の目安・Hugging Face のトークンを使うこと・成人向けの注意書き。
- **成人向けモデル**は、年齢確認（`set_adult_content`。チャットからは確認しない）が済むまで一覧に出ず、名前では見つからない。ID を直接言うと、MCP の `get_catalog_model` と同じ年齢確認の案内で断る。
- **ライセンスに同意が要るモデル**（`consent_required` で未同意）: カードにライセンスの ID・版・要約・原文の URL を出し、ボタンは**「同意して導入する」だけ**（`はい、この内容で進める` のボタンも、「はい」という返信も同意にならない）。このボタンの操作が `license_id`・`license_version` を持ち、`Apply` が MCP と同じ `LicenseRef` つきで `InstallModel` を呼ぶ（サービスが ID・版を照合して同意を記録する）。すでに同意済みの版なら通常の確認カード。同意が要るのに ID・版が分からないときは、同意を記録できないので画面から導入するよう答える。
- `requires_hf_token` のモデルはトークン未設定なら断る（アクセス承認・無効なトークンの検査は `InstallModel` が承認後に行い、断られたら理由を答える）。

### 2.3 ComfyUI エンジン（`install_model_engine`、MCP `install_model_engine`）

- ビルドは MCP と同じく GPU の推奨（`DetectGPU`）で決める。対応する GPU が無ければ、案内をそのまま答えて断る。「CPU で」と言ったときだけ（`args.cpu`）CPU 実行で導入し、カードにそう書く（とても遅く、動画には向かない）。`args.variant` はモデルが書いても、そのマシンのビルドにあるものだけが通る。
- カードに出すもの: ビルド名・ダウンロード約 N GB・展開後約 N GB・必要な空き容量（導入先に書き込める前提）・ライセンス（GPL-3.0）。導入済みで最新ならカードは出さない。導入済みでも固定版と違えば「更新しますね？（導入済み v1 → v2）」、ファイルが無くなっていれば「入れ直しますね？」。ビルドは導入・更新・入れ直しのどれも GPU の推奨（`Service.chooseVariant` と同じ選び方。`chatwire.engineDeps` が導入済みのときも推奨を埋める）で、推奨が無ければ理由つきで断る（CPU 実行を選んだときを除く）。

### 2.4 Hugging Face のファイル（`install_hf_file`、MCP `install_hf_file`）

- `args.repo`（`owner/name` か URL）を `modelmanager.HFRepo`（`get_hf_model_files` と同じ）で読み、**コミットはそのとき Hub が返した値**に固定する。ファイルは `args.path`（または URL が指したファイル）、無ければ導入できる `.safetensors` が 1 つならそれ、複数なら 1 つずつのボタン（`pick=true`）。導入済み・導入中・導入できない形式（理由つき）・導入先のフォルダを決められない・gated でトークンが無い・成人向け（NFAA）で年齢確認の前、は断る。
- カードに出すもの: ファイルの大きさ・導入先（`models/<folder>/<name>`）・コミット（先頭 7 桁）・モデルカードの自己申告のライセンスと商用利用（`modellicense.FromHF`: 申告だけでは商用OKにしない）・ライセンスの場所・ライセンスの記載が無い警告・セキュリティスキャン・成人向け・gated。
- terms（取得はあなたの判断です、という注意書き）が**このリポジトリのこのコミットで未同意**なら、ボタンは「同意して導入する」だけで、操作が `terms_version`（カードに出した版）を持つ。`Apply` は `Accept=true`・その版で `InstallHFFile` を呼ぶ。同意済みなら通常の確認カードで、`terms_version` は持たない（記録済みの同意をサービスが確かめる）。`Apply` はコミットが無い操作を実行しない。

### 2.5 ローカルの実行環境（`install_local_runtime`、MCP `install_local_runtime`）

- `args.runtime`（ID か名前: gpt-sovits・musetalk・irodori-tts・llama-cpp・ollama …）を `localruntime.Service.List` から引く。
- カードに出すもの: ダウンロード約 N GB（GPT-SoVITS は約 8 GB）・展開後約 N GB（約 14 GB）・必要な空き容量・重みと依存ファイルの大きさと、商用利用が「商用OK」と確認できていないものの件数（導入後に実行環境の欄で出典とライセンスを見るよう案内）・ライセンス。この PC で動かせないとき（Windows + NVIDIA の GPU 以外）は、理由とクラウドへの案内をそのまま答えて断る。導入済みで最新なら出さず、更新・入れ直しは「更新しますね？」「入れ直しますね？」。

### 2.6 テスト

- `chatops/install/installtest`（偽のサービス）: 名前の解決（ID・名前・表記ゆれ・複数・無い・成人向けの門）・カードの中身（大きさ・ライセンス・商用利用・同意・全プロジェクト）・承認の前に呼ばれないこと・`Apply` の呼び出し 1 回とライセンスの受け渡し・進捗カードと `Detail` の文面・各断り（エンジン無し・設定ファイル固定・空き容量・Hugging Face のトークン・導入中・導入済み・OS）・モデルの返事のガード。
- `service/chatops/chattest/install_test.go`（チャット全体・偽のサービス）: 発言 → カード → やめる／無関係な返信（何も導入されない）→ 承認（1 回導入され、スレッドに進捗カードと `Detail`）・同意のボタン・曖昧な名前の選択・カタログに無い名前・導入済み・発言が求めていないときは捨てる・サービスが無い環境。`chattest/install_real_test.go`: 実物の `modelmanager.Service`（mmtest）で、エンジン未導入の断り → エンジンのカード → 承認 → ジョブの進捗 → モデルのカード → 導入済み、同意のボタンが同意として記録されること、成人向けの門。

## 3. モデルと LLM の割り当て

実装: `backend/internal/service/chatops/assign`（`set_model_assignment`）、`chatops/llmroute`（`set_llm_assignment`）、つなぎ込み `service/chatwire/assign.go`。どちらも**アプリ全体の設定**（`Global`）で、DIRECTOR MODE でも AUTO MODE でも、Brief を作り終えたあとは受ける。Brief を作っている最中（DRAFT）に受けるのは `set_llm_assignment` だけ（`Setup`）で、`set_model_assignment` は受けずに Brief への答えとして抜き出しに回す（§0.2）。サービス層は設定画面・MCP と同じで、ロジックは複製しない。

### 3.1 画像・動画モデルの割り当て（`set_model_assignment`）

- 発言: 「生成の画像モデルの 新規参照 を flux にして」「Quality MAX の動画モデルを kling-4.0 に」「BALANCED の代替を seedance-2.0 に」「1080p で 3 Take」。引数（`Operation.Args`）は `target`（画像の用途 `new_reference`・`edit_reference`・`preview`・`unified_openai`、または Quality Policy `fast`・`balanced`・`max_quality`）、`role`（`primary` 既定／`fallback`）、`model`（発言のままの名前）、画像の `fast`（高速版）、動画の `resolution`・`takes`。`Spec.Clean` が `target`（「新規参照」「Quality MAX」なども ID にする）と `role`（代替・フォールバック・バックアップ）を整える。読み取れない `target` は空にし、推測しない。
- `Spec.Asked`: 画像・動画・用途・Quality Policy・モデル名のどれかを含み、かつ「にして」「変えて」「割り当て」などの変更の言い回しがあること（解像度と Take 数だけの「1080p で 3 Take」も可）。質問（「今の画像モデルは何？」）、API キー・呼び出し先プロバイダー（§1）の発言、LLM の発言（§3.2）は含めない。モデルを使わない読み取り（`Detect`）は持たない（LLM が要る）。
- **確認カード（`Question`。何も保存しない）**: `modelsettings.Service.View()` の保存済みの割り当てを読み、名前を解決し、「いま」と「変更後」の経路（画像: `主 … / 代替 …`〔高速版〕、動画: `主 モデル（解像度） / 代替 … / N Take`）を出す。あわせて次を書く。
  - **全プロジェクトに効く**（アプリ全体の設定）こと。
  - **保存するとすぐに反映される**こと（再起動は要らない。設定画面の「すぐに反映されます」と同じ）。
  - 割り当てるモデルが今使えない（API キー未登録など。`View.ImageModels[].Availability` / `VideoModels[].Availability`）ときは、**拒否せず警告行**（「注意: kling-4.0 は今使えません: API キー未設定（割り当ては保存できます。…）」）。MCP と同じく割り当ては保存できる。
  - `preview`（どの機能からも呼ばれない）と `unified_openai`（`prefer_openai` のときだけ使われる）の注。
- **名前の解決**: モデル名は、`View.ImageModels` / `View.VideoModels`（設定画面が選択肢に出す、いま選べるもの。ローカルモデルもそこに出るものは同じ名前で選べる）から、記号と大文字小文字を無視した完全一致、なければ名前に含まれるもので探す。1 つなら決める。**複数**（「kling」は kling-3.0 と kling-4.0）ならカードを出し、**モデルごとのボタン**（各ボタンがそのモデルを入れた操作を持つ）で聞く。**どれにも当たらない**ときは、カードを出さず、選べるモデルの一部を挙げて断る。年齢確認がオフで選べない成人向けモデルは、そう伝えて断る。解像度は、そのモデルが対応するもの（`VideoOption.Resolutions`）から引く。モデルを替えて解像度を言わず、元の解像度が新しいモデルに無いときは、解像度を添えるよう答えて断る。高速版は `ImageOption.Fast` があるモデルだけ。
- **承認（`Apply`）**: MCP `set_model_assignment` と同じく、保存済みの `View()` を読み直し、名指しされた用途・Quality Policy の**主か代替の片側だけ**を変え（もう片側、他の用途、他の Quality Policy、Take 数は保存済みのまま）、`modelsettings.Service.Save(Input)` で割り当て全体を保存する。検証（`modelsettings.InvalidError`・`ErrPinned`・年齢確認）は `Save` のものをそのまま使い、理由は「実行できませんでした: …」として答える。`models.json` には既定と違う用途・ポリシーだけが残る。
- **カードを出さずに答える場合**: すでに同じ割り当て（「すでにその内容です。変更しませんでした。」）／`--model-config` で固定されている／設定ファイルを読めない／用途・Quality Policy が読み取れない（選べるものを挙げる）／変える内容が無い／選べないモデル。

### 3.2 LLM の割り当て（`set_llm_assignment`）

- 発言: 「Director の LLM を Claude Opus にして」「Producer の代替を GPT にして」。引数は `agent`（ID またはラベル。`llmassign` の Agent は `llm.Agents()`、ラベルは設定画面と同じ `failmsg.Label`）、`role`（`primary` 既定／`fallback`）、`provider`（`llm.Providers()`）、`model`（発言のままの名前）。
- `Spec.Asked`: Agent の名前（ID・ラベル・注を除いたラベル。最長一致なので「Art Director」は「Director」ではない）を含み、「LLM」「モデル」「代替」「プライマリ」のいずれかと変更の言い回しがあること。質問、画像・動画・Quality Policy の発言（§3.1）は含めない。
- **`Detect`（LLM が無くても読める）**: 「<Agent> の LLM／代替を <モデル> にして」を、Agent・role・モデル（名前からプロバイダーを引く）に読む。LLM の API キーが無い最初の設定でも、LLM の割り当てを変えられる。
- **名前の解決**: Agent は完全一致、なければ名前に含むもの（複数ならボタンで聞く。どれにも当たらなければ選べる Agent を挙げて断る）。プロバイダーは、`provider` の指定、なければモデル名が表すもの（`claude`→anthropic・`gpt`→openai・`gemini`→google・`codex`・`ollama` など。最長一致）、なければ**その経路の現在のプロバイダー**。モデルは `llmmodels.Service.List` のプロバイダーの一覧（キー未登録のときは mam の既定候補）から、完全一致、なければ全部の語を含むもので探す。1 つなら決める。2〜5 件なら**ボタンで聞く**。6 件以上は絞るよう答えて断る。
- **一覧に無いモデルは、拒否せず警告**（MCP と同じ）: 「「…」は anthropic の一覧にありません。使えないかもしれません（保存はできます）」。空白・全角文字を含む語はモデル名として読めないので、選べるモデルを挙げて断る。プロバイダーが今使えない（API キー未登録・未ログイン。`llmmodels.Service.Availability`）ときも警告行。**API キーはカードにも答えにも出さない。**
- **確認カード**: 「いま」「変更後」の経路（`プライマリ プロバイダー / モデル / 代替 …`）、警告、今のコードでは使われない Agent（Orchestrator・Final QC）の注、**全プロジェクトに効く**こと、**反映の時期**。割り当てを実行中のクライアントに渡している（`View.Live`）ときは「次の LLM 呼び出しからすぐ使われます（再起動は要りません）」、そうでなければ「次にアプリを起動したときから使われます」。
- **承認（`Apply`）**: MCP `set_llm_assignment` と同じく `llmassign.Service.Save`（名指しされた Agent だけの `map[agent]Route`。経路のもう片側は保存済みのまま）。`llmassign.InvalidError`・`ErrPinned` は「実行できませんでした: …」。保存後に再起動が要る設定が残っていれば（`View.Pending`）答えに添える。`llm.json` には既定と違う Agent だけが残る。
- カードを出さずに答える場合: すでに同じ経路／`--llm-config` で固定されている／設定ファイルを読めない／Agent・モデルが読み取れない。

### 3.3 つながっていないとき

`chatwire.Connect` に渡す `chatwire.Settings` に `Models`（§3.1）または `LLM`（§3.2）が無い環境では、その操作は登録されず、「この環境では、その設定をチャットから変更できません」と答える（§0.5）。`chatwire.Settings.LLMModels` が無いときは、LLM の名前をそのまま受け、一覧の警告を出さない。

## 4. LoRA の取り込み

実装: `backend/internal/service/chatops/lora`（`add_lora`・`discard_lora`・`chatops.FileOffer`）、つなぎ込み `service/chatwire/lora.go`。モデルの管理の「LoRA を追加」（`POST /model-manager/loras/inspect` → `POST /model-manager/loras`）と MCP のアップロード用途 `model_lora_import` と**同じ** `modelmanager.Service.LoRAs`（`loraimport.Service`）の `Stage` / `Commit` / `Discard` を呼ぶ。検査（名前・ヘッダー・大きさ・LoRA のテンソル・空き容量・待ちの数）も、名前の衝突の扱いも、画面と同じ。

### 4.1 添付で引き取る

- **チャットのコンポーザーは `.safetensors` を選べる**（`<input id="composer-files">` の `accept` は `AcceptedUploads + ",.safetensors"`（`web/organisms/chat_composer.templ`）＝ 画像・動画の拡張子 ＋ `.safetensors`。新規プロジェクトの入力欄は従来の `AcceptedUploads` のまま）。ドラッグ＆ドロップも同じ。コンポーザーの JS は拡張子や大きさを検査しないので変更は無い。
- `.safetensors`（大文字小文字は問わない）のファイルが付いたメッセージは、「対応していない添付」として断らず、`chatops.FileOffer`（`lora.Offer.Accepts`）に渡す。画像・動画は従来どおり Brief の添付になる（同じメッセージに両方付けてもよい）。**LoRA は Brief の素材（asset）にならない**。発言の本文は、書いたとおり（空なら「添付: <ファイル名>」）がスレッドに残る。
- ファイルは `LoRAs.Stage` にストリームで渡され、インポート用のフォルダに置かれる（`loras` フォルダにはまだ置かない）。**1 ファイルにつき 1 枚の確認カード**を返す。複数のファイルの一部だけが断られたときは、通る分のカードと、断られたファイルの理由（「取り込めませんでした: <ファイル名>: …」）を返す。

### 4.2 確認カード

カードは `ref_type=operation`、`ref_id=add_lora:<token>`（ファイルごとに別のカードとして答えられる。ほかの操作のように「新しいカードが古いカードを置き換える」ことはない）。ボタンは「はい、この内容で進める」（`add_lora`）と「やめる」（`discard_lora`）。本文に書くこと:

- 添付の名前、**取り込む名前**（`lorafile.CleanName` の結果。使えない文字を `_` に置き換えたときはそう書き、元の名前も書く）、大きさ、SHA-256 の先頭 16 文字。
- ファイルが自分について言っていること: 題名、**対応するベースモデル**（`__metadata__` から読めたもの。読めなければ「未設定」。ファイルに文字列はあるが読み取れないときは、その文字列を引いて「未設定」）、**トリガー語**、**ライセンス名**。**推奨の強さ**はファイルからは読まず「未設定」。商用利用の可否はファイルの言葉から決めず、「不明」として記録すると書く。
- **名前の衝突**: `Staged.Conflict` があるときは、「すでにその名前のファイルがあり、上書きしない」ことと、画面と同じ**空き名前の提案**（`Conflict.Suggest`）で追加することを書く。提案の名前は `add_lora` の `args.name` に入り、承認でその名前で追加する。**同じ内容のファイルがすでにある**とき（`Conflict.Same`）は、カードを出さずに「同じ内容のファイルが、すでに「…」としてモデルの管理にあります。追加は要りません」と答え、置いたファイルは捨てる。
- 「**全プロジェクトに効く（モデルの管理に追加され、単発生成・各 Project の生成で選べる）設定です**」。

### 4.3 答え

- **はい**: `LoRAs.Commit(Input{Token, Name, Meta})`。`Meta` は画面のフォームを**何も直さずに**送ったときと同じ＝ファイルが言っているベースモデル・トリガー語・ライセンス名だけ（推奨の強さと商用利用の答えは入れない。作らない）。ファイルが `loras` フォルダに 1 回の rename で移り、記録（`state.json` の `LocalLoRAs`）が残る。答えは「「<名前>」を LoRA として追加しました。全プロジェクトで、単発生成と各 Project の生成の LoRA として選べます。推奨の強さ・ベースモデル・トリガー語・ライセンスは「モデルの管理」の画面で直せます。」
- **Commit が断られたとき**（値が正しくない、名前が埋まった、など。`mmview.Refuse` の文をそのまま使う）: 「実行できませんでした: <理由>\nファイルは破棄しました。値を直す必要があるときは、「モデルの管理」の画面の「LoRA を追加」から追加してください」。ファイルは捨てる（このカードしか触れないため）。記録の書き込みに失敗するなどの不具合はエラーで、ファイルは待たせたまま（もう一度「はい」で試せる）。
- **やめる**（ボタン・「やめる」「キャンセル」などの言葉）: `Card.Decline` の `discard_lora` で `LoRAs.Discard(token)` を呼び、**置いたファイルをインポート用のフォルダから消す**。答えは「取り込みをやめました。ファイルは破棄し、何も追加していません。」
- カードと関係のない返信は新しいメッセージとして読まれ、何も追加しない（カードは閉じ、置いたファイルは残りの 1 時間で `loraimport` が捨てる。`maxStaged`＝3 個まで同時に待てる）。
- そのトークンのファイルが待っていない（1 時間たった、すでに追加した、アプリを再起動した）ときの `Question`・`Apply` は「確認待ちのファイルが見つかりません。もう一度添付してください」。**同じカードで 2 回は追加できない**（カードは 1 回しか答えられず、`Commit` も 1 回だけ呼ばれる）。

### 4.4 断られたとき（カードを出さない）

`Stage` が断る理由は、画面（`POST /model-manager/loras/inspect`）と同じ文で「取り込めませんでした: …」と答える（`chatops.Refusal` ← `mmview.Refuse`）: LoRA のファイルではない（ヘッダー・テンソル）、大きすぎる（`lorafile.MaxBytes` ＝ 4 GiB まで）、空き容量が足りない、確認待ちのファイルが多い（3 個まで）、同梱の ComfyUI が使えない OS（外部の ComfyUI の `loras` フォルダに直接置く）。モデルの管理がつながっていない環境では、`.safetensors` は従来どおり「対応していない添付」として断る。

### 4.5 リクエストの大きさの上限

`POST /projects/:id/messages` の multipart の上限は `maxMessageRequestBytes` ＝ `lorafile.MaxBytes`（4 GiB）＋ 従来の上限（8 件 × 動画の 300 MB ＋ 1 MiB）。LoRA を 1 つ付けても、同じ要求に最大の画像・動画を足せる大きさ。新規プロジェクト（`POST /projects`）の上限は従来のまま（`.safetensors` は選べない）。上限の検査は、宣言された `Content-Length` では本文を読む前に 413、それ以外は読みながら（`http.MaxBytesReader`）行う。**結果**: 他のアップロードと同じく本文はいったん一時ファイルに書かれる（`readMultipart` は `ParseMultipartForm`）ので、LoRA 1 つにつき、ファイルの大きさ分の空きがテンポラリフォルダに要り、そこからインポート用のフォルダへもう一度コピーされる（処理が終わると一時ファイルは消える）。ここで足りなくなると 400「送信内容の形式が正しくありません」になる。大きなファイルは、モデルの管理の「LoRA を追加」（本文をそのままストリームする）のほうが軽い。上限を超える個々のファイルは `Stage` が大きさで断る。

### 4.6 モデルに伝えない操作

`add_lora`・`discard_lora` は `Spec` に `Example`・`Doc` を持たない（プロンプトに載らない）。ボタンの `option_actions` にだけ存在し、モデルが返しても `Clean` の前に捨てる（`cleanRegistered`）。どちらも `Confirm`（`add_lora`）・`Global` を持つ。テスト: `chatops/lora/lora_test.go`（Importer の偽物）、`service/chatops/chattest/lora_test.go`、`web/handler/chatcard/message_limit_test.go`。

## 5. 書き出し（Export）

パッケージ `service/chatops/export`（`spec.go` `detect.go` `handler.go` `download.go`）。配線は `service/chatwire/export.go`。この 3 つの操作は **Project の操作**で `Global` ではない（カードに「全プロジェクト」とは書かない）。`EditorService` が無いと登録しない。これまで「書き出し」は `unsupported`（Command Interpreter のプロンプトの `unsupported` の例・説明はタイムラインの編集だけに直してある）だった。

| 操作 | 型 | `Spec` | 同じ関数 |
|---|---|---|---|
| 動画を書き出す | `export_video` | `Confirm`・`AnyMode` | `EditorService.Export`（画面 `POST /projects/:id/export`、MCP `export_video`） |
| 書き出し済みの動画のリンク | `get_export_download_url` | `Immediate`・`AnyMode` | `EditorService.View` の `Exports`（MCP `get_export_download_url` の同じ対象。MCP は 15 分有効のトークン URL を発行するが、アプリ内のチャットは同一オリジンの画面のルートを使う） |
| Project 全体のバンドル（ZIP） | `export_project_bundle` | `Immediate`・`AnyMode` | `GET /projects/:id/bundle`（`web/handler/bundle.go`、`BundleService.Prepare`。MCP `export_project_bundle`） |

### 5.1 動画を書き出す（`export_video`）

`Args` は `aspect_ratio`・`resolution`・`codec`（`domain.ExportAspectRatios` / `ExportResolutions` / `ExportCodecs`。省略は Brief の既定＝Brief の縦横比・1080p・H.264）。`Clean` は「9：16」「4K」「HEVC」などを `9:16`・`2160p`・`h265` に書き直すだけで、選択肢に無い値は残し、カードが「…から選んでください」と断る（言っていない値で書き出さない）。

- **カードを出す前の確認**（`EditorService.View` ＝編集画面の書き出しボタンと同じ見方。いずれもカードを出さず、理由を答える）: ①`Project.State.CanExport()` が偽（READY / EXPORTED でない）→「いまは書き出せません（現在の状態: <STATE>）。書き出せるのは Final QC に合格した後（READY）です。…」 ②`CanRender` が偽（ffmpeg / ffprobe が無い）→ 画面と同じ「ffmpeg / ffprobe が見つからないため、音声と書き出しは使えません。…」 ③この系列に音声が無い／最新の編集に対する音声でない→ 画面の `ExportBlocker` と同じ文 ④選択肢が範囲外。
- **カード**: 「動画を書き出しますね？」＋ 縦横比・解像度・コーデック（言った値は「9:16（既定: 16:9）」、既定のままは「1080p（既定）」）＋「ffmpeg で書き出します（時間がかかります。…）」＋ READY なら「書き出すと Project は EXPORTED になります」、すでに EXPORTED なら「もう一度書き出します（前の書き出しは残ります）」＋「終わると、ダウンロードのリンクを出します」。はいのボタン（`Card.Op`）は**カードに出した選択肢（既定を埋めたもの）**を運ぶ。返事を待つあいだに Brief の既定が変わっても、同意した内容で書き出す。
- **はい**: `EditorService.Export(projectID, ExportInput{AspectRatio, Resolution, Codec})` を 1 回呼ぶ（`BranchID` は主線）。`Export` は自分で「書き出しが完了しました（…）」の `ref_type=export` の通知（ダウンロードボタン付き）を投稿するので、`Result.Text` は短く「書き出しが完了しました（9:16 / 720p / vp9）。Project は EXPORTED です。\nダウンロード: /projects/<id>/exports/<exportId>/video」とし、**通知を重ねて投稿しない**（リンクは文面のパスと、`Export` の通知のボタンの 2 つで、メッセージは 1 通ずつ）。`EditorService` は変更していない。
- **断られたとき**（`exportRefusal`）: `ConflictError`（状態・音声が最新でない・Take のファイルが無い）→ その文、`ErrStageBusy` →「別の編集処理（…）が実行中です」、`ErrUnavailable` → ffmpeg が無い、`ErrNotFound` → 編集・音声が無い、`AIError`（ffmpeg の失敗）→ `Reason`。「実行できませんでした: <理由>」と答える（`director_ops.go` の `applyOp` が `Refusal` をそう答える）。何も書き出されない。

### 5.2 書き出し済みの動画をダウンロードする（`get_export_download_url`）

何も変えないのでカードを出さずに答える。`EditorView.Exports` のうち**最新**（`exported_at`）を選び、「最新の書き出し（<日時>・<縦横比 / 解像度 / コーデック>）です。下のボタンからダウンロードできます。」と、`ref_type=export`・`ref_id=<export id>` の AI テキスト通知（`web/molecules` が「書き出した動画をダウンロード」のボタン `GET /projects/:id/exports/:exportId/video` に描く、`Export` が出すのと同じ通知）を続ける。書き出しが 1 つも無いときは「まだ書き出した動画がありません。「この動画を書き出して」と送ると書き出します（…）」と答え、リンクは出さない。

### 5.3 Project のバンドル（`export_project_bundle`）

何も変えないのでカードを出さず、「プロジェクトのバンドル（ZIP: データと素材ファイル）はこちらからダウンロードできます: /projects/<id>/bundle」と、別の端末での取り込み方（プロジェクト一覧の「プロジェクトバンドルを読み込む」）を答える。ZIP の用意はリンクを開いたときのルートが行う（チャットでは作らない）。

### 5.4 言葉の読み取りと守り（3 つとも `Asked` と `Detect`）

`書き出/書出/エクスポート/export` または `ダウンロード/download` ＋ 対象の語。モデルの返事だけでは足りず（`Spec.Asked`）、モデル無しでも読む（`Spec.Detect`、LLM 未設定でも動く）。

- 動画の書き出し: 書き出しの語 ＋（`動画/ビデオ/映像/完成/mp4/webm/video/最終版` か 「これ/この/それ/その」）。「書き出した動画は…」（過去の書き出しの話）は読まない（「書き出したい」は読む）。縦横比（`16:9` `9:16` `1:1` `4:5`、縦長・横長・正方形）、解像度（`720p`〜`2160p`、`4K`、フル HD）、コーデック（`h264` `h265`/HEVC `vp9`/webm）が書いてあれば `Args` に入れる。
- ダウンロード: `ダウンロード/download` ＋（動画・さっき・最新・前の・ファイル・書き出し…）。
- バンドル: `バンドル/bundle/zip/バックアップ/まるごと/プロジェクトごと/プロジェクト全体` ＋ 書き出し／ダウンロードの語、または「プロジェクト」を書き出す・ダウンロードする（動画の語が無いもの）。
- **書き出し・ダウンロードの語を話の中で触れているだけの文は読まない**（`mentionedOnly`）: 「書き出す前に…」「書き出したら…」「書き出しは最後に…」「書き出すと EXPORTED になる…」（語のあとに 前・直前・手前・より前・後・あと・時・とき・際・場合・まで・ので・ため・ように・たら・れば・なら が続くもの、「書き出すと」、「書き出しは最後に／最終的に／あとで／不要／終わった／完了」、「書き出さない／書き出さず／エクスポートせず」。「書き出しは 1080p で」「書き出しは最終版で」「書き出しなおして」は依頼として読む）、`before/after/until/when export`、`exporting/exported`。この語を除いてから上の読み取りをするので、ほかの指示のついでに言及しただけでは書き出しのカードもバンドルのリンクも出ない。「プロジェクト全体/まるごと/丸ごと/プロジェクトごと」は、動画の語（動画・ビデオ・映像・完成・mp4…）が無いときだけバンドルとして読み（動画の語があれば動画の書き出し。「動画をまるごと書き出して」）、「プロジェクト全体の雰囲気を…」のように「の」で続くときは書き出しの語（書き出し・エクスポート・ダウンロード・バックアップ・バンドル・ZIP）が続くときだけ読む。`バンドル/bundle/zip/ジップ/バックアップ` は動画の語があってもバンドルとして読む。
- **読まない**: `モデル/LoRA/Ollama/ComfyUI/ランタイム/インストール/導入…` の語を含むもの（「動画モデルをダウンロードして」はモデルの導入＝§2 の操作）、対象の無い「ダウンロードしたい」、動画の書き出しを頼まない文。

`Detect` の結果はモデルが同じ型を返さなかったときだけ足される（`withDetected`）。`director_shot_resolution_test.go` の「これを書き出して」は今は `export_video` の読み取りに当たるので、`unsupported` の例には `これを YouTube に投稿して` を使う（テストの文だけ直した）。

テスト: `chatops/export/export_test.go`（偽の `Deps`: 先にカード・承認までは何も呼ばない・はいで 1 回・状態／ffmpeg／音声／選択肢の断り・既に EXPORTED・リンクの有無・バンドル・`Detect`・`Asked`）、`service/chatops/chattest/export_test.go`（本物の `EditorService` の状態確認と ffmpeg 確認を `chatwire.Connect` 経由で通し、レンダリング自体は `ChatOps().Handle` で登録した偽（`export_helpers_test.go`）で置き換える）。

## 6. キャラクターの顔・参照

パッケージ `service/chatops/charref`（`spec.go` `handler.go`）。配線は `service/chatwire/charref.go`、Library への登録は `service/chatwire/assetref.go` の `addFromAsset`（保存済みのアセットの場所は `service/reference_from_asset.go` の `ReferenceService.StoredAsset`。`reference.go` は変えていない）。操作は `assign_character_reference`（`Confirm`・`AnyMode`・**`Global` ではない**＝この Project の Character だけが変わる。World State が要るので DRAFT 中は読まない）。`WorldService` か `ReferenceService` が無いと登録しない。

### 6.1 チャットの添付がどこまで届くか（調べた結果）

- チャットに画像を添付して送ると、`PostMessage` が `storeUploads` で**アセット（`type=image`）として保存**し、`user.AttachmentAssetIDs` に付け、`attachToBrief` で **Brief の `Assets` に加える**（`brief.AttachAssets`）。ここまでは従来どおりで、**Reference Library には入らず、Character にも割り当てられない**。
- 割り当ての既存の経路は 2 つ: ①Reference Library 画面の `ReferenceService.Upload`（ファイルを取り、`type=reference` のアセットと `reference_item` を作る）→ ②`WorldService.AssignReference(projectID, branchID, target, entityID, slot, referenceID)`（画面・MCP `assign_character_reference` 共通。Library の `ReferenceItem.Kind` が枠の種別と同じ `character` でなければ `ValidationError`、枠が LOCK なら `ErrLocked`）。**既に保存されたアセットから Library の項目を作る関数は無かった**ので、`chatwire/assetref.go` の `addFromAsset` を足した（`ReferenceService.StoredAsset` が保存済みの画像ファイルの場所を返す）: そのファイルを開き、`Upload` に渡す（種別・名前・画像かどうか・中身・サイズの確認を `Upload` と共有）。項目は自分の `type=reference` のアセットを持つので、添付とも Library の項目ともお互いに消えても影響しない。
- 添付のアセットは、このメッセージの `ctx` にだけある（`chatops.Attachments(ctx)`）。はいのボタンの返事は後で来て添付を持たないので、`Question` がアセット ID と Character の ID を**はいのボタン（`Card.Op`）に入れて運ぶ**。モデルが返した `asset_id` / `character_id` は `Question` が必ず上書きする（モデルの値は信用しない）。

### 6.2 カードを出すまで

`Args`: `character`（言った名前）・`slot`（`face`/`body`。「顔・フェイス」「体・全身・ボディ」を `Clean` が直す）。

1. 枠が決まらない → 「顔（face）と体（body）のどちらの参照にするか教えてください。…」（カードなし）。
2. 添付の画像が 0 枚（動画だけも含む）→「参照にする画像が添付されていません。画像を添付して、同じメッセージで…」。2 枚以上 →「画像が N 枚添付されています。参照にする画像を 1 枚だけ…」。
3. World State（`WorldService.View`、主線）に Character がいない →「…まだ Character がいません。…」。名前が一致しない →「「<名前>」という Character は見つかりません。この Project の Character: 「A」、「B」」（名前の一覧）。
4. 名前の突き合わせ: 空白・大小を無視して**完全一致**を優先、なければ**部分一致**（2 文字以上）。名前が空か「主人公/主役/メイン/ヒロイン」などの役割の語なら全員が候補になる（1 人ならその人、複数なら聞く）。
5. 候補のうち、その枠が LOCK（`identity`）されていない人だけを残す。全員 LOCK → 「<名前> の顔の参照枠は LOCK されているため変更できません。LOCK を解除してから指示してください」（画面の文）。2 人以上残る → `Card.Choices`（「<名前> の顔にする」ボタン 1 人 1 つ、各ボタンが添付アセットと Character の ID を運ぶ）。1 人 → 下のカード。

カード: 「この画像を「<名前>」の顔（face）の参照にします。」＋ Reference Library に Character の参照画像「<名前>（顔）」として登録し <名前> の face 枠に割り当てる＋（枠に画像があれば）「いま face 枠にある画像は外れます」＋「この Project の Character だけが変わります（ほかのプロジェクトには影響しません）」＋ 以降の Shot の生成でこの画像が参照として使われる（できあがった Take は変わらない）。

### 6.3 答え

- **はい**: ①Character と枠をもう一度読み、LOCK されていたら断る（何も登録しない）→ ②`addFromAsset(…, character, "<名前>（顔）")`（`chatwire/assetref.go`。`ReferenceService.Upload` 経由）（名前は `MaxReferenceNameRunes` に切る）→ ③`WorldService.AssignReference(…, character, c.ID, slot, referenceID)`。③が断られたら、②で作った Library の項目を `ReferenceService.Remove` で取り除く（何も残さない）。答えは「「<名前>」の顔（face）の参照を設定しました。…この Project の Character だけが変わりました。」
- **やめる／カードと関係の無い返信**: 何も登録せず、何も割り当てない（共通の約束 §0.1）。
- 断り（`charrefRefusal`）: `ErrLocked` →「LOCK されている項目は変更できません。…」、`ErrNotFound` →「Character か画像が見つかりません。…」、`ValidationError`（`refusalOf`）→ その文。「実行できませんでした: …」と答える。

### 6.4 `Asked` と `Detect`

枠の語（顔・フェイス・face・全身・ボディ・身体・体・body。「全体・具体・主体・本体・体験・体力」のように別の語の中の「体」は枠の語にしない。「体型・体格・身体」は体。名前は書かれたままの文から読む）＋ 画像の語（画像・写真・イラスト・参照・リファレンス・添付…）＋ 割り当ての語（にして・として・に設定・に割り当て・を使って…）が全部あり、顔の見た目の指示（笑顔・表情・明るく・修正・補正・ぼかし・髪・アップ…）を含まないこと。「主人公の顔を笑顔にして」「顔を大きく映して」は読まない。`Detect` は「<名前>の顔/全身…」から名前と枠を取り出す（「この画像」「写真」などは名前としない）。モデルの `assign_character_reference` は `Asked` が偽なら捨てる。

### 6.5 チャットからは届かないもの

- **商品（`assign_product_reference`: geometry / label）・場所（`assign_location_reference`: layout）の参照**: 操作として作っていない（要求は Character の顔・参照だけ。同じ `addFromAsset` と `AssignReference` で足せるが、この Issue の範囲外）。
- **Reference Library に既にある項目を名前で指す**（「Library の A を主人公の顔に」）: 作っていない。画像を添付したメッセージだけが入口。
- **枠を空にする**（MCP は `reference_id` 空で空にできる）: 作っていない。
- **声の参照（`voicereference`）**: チャットは画像・動画しか添付に取らない（`extensions` に音声が無い）ので、声のサンプル音声はチャットに添付できない。
- **Series の Character（canon）・Branch の World State**: チャットの操作は主線（`branchID=""`）の Character だけ。Series の canon の顔は Series の画面から。
- **複数枚の画像**: 1 メッセージ 1 枚だけ（何枚目かを聞く手段が無いため断る）。
- 以前のメッセージの添付（「さっきの画像を…」）は使わない。このメッセージに添付した画像だけ（`ctx` の添付）。

テスト: `chatops/charref/charref_test.go`（偽の `Deps`: カードの中身・はいのボタンがアセットを運ぶ・承認まで何も登録しない・はいで 1 回・名前の曖昧さ／未知／Character 無し・添付無し／複数枚／動画・LOCK・カードのあとの LOCK・割り当て失敗で Library の項目を戻す・`Detect`・`Asked`・モデルの値を信用しない）、`service/chatops/chattest/charref_test.go`（本物の `WorldService`・`ReferenceService`・実ファイルのアップロードで、枠への割り当てと Library の項目を確かめる）。

## 7. 変更履歴

| 版 | 日付 | 内容 |
|---|---|---|
| 1.0 | 2026-11-10 | 新規。共通の約束（§0）と API キー（§1）を書いた（#1140） |
| 1.1 | 2026-11-10 | モデルの導入（§2）・割り当て（§3）・LoRA（§4）・書き出し（§5）・キャラクターの参照（§6）を足した。実装は `service/chatwire/` が配線し、サービス層のテストは `service/chatops/chattest/` に置く（#1140） |
| 1.2 | 2026-11-10 | レビューの指摘を直した: DRAFT 中は設定の操作を API キー・プロバイダー・LLM の割り当てと拒否だけに絞り、モデルの割り当て・導入の `Asked` を対象の名指しに絞った（§0.2）。キーらしき文字列の検出を形の無いキー・開いているキーのカードへの返信まで広げ、伏せた発言をカードへの返信にしないようにし、ほかの入口にも適用した（§1.1）。MCP のトークン・ポートの拒否を語の境界で判定する（§0.4）。実装・テストの名前を実際のものに直した（`chatwire`・`chattest`・`chatcard`・`addFromAsset`）（#1140） |
| 1.3 | 2026-11-10 | カタログのモデル ID・ファイル名と完全に同じ語をキーとして伏せないことを足し（§1）、`secretguard.Redact` の入口に単発生成のチャットの発言を足した。DRAFT 中の受付を実装（`AsksSetup`）に合わせて直した（§2・§3。API キー・プロバイダー・LLM の割り当てと拒否だけ） |
| 1.4 | 2026-11-11 | §5.4 の検出を直した: 書き出し・ダウンロードの語を話の中で触れているだけの文（「書き出す前に／直前に／手前で／たら…」「書き出すと…」「書き出しは最後に」「書き出さない／せず」。「書き出しは 1080p で」「書き出しは最終版で」は依頼）を読まない。「プロジェクト全体の〜」は書き出しの語が続くときだけバンドルとして読み、動画の語があれば動画の書き出しにする（PR #1141 のレビュー指摘） |
| 1.5 | 2026-11-11 | 進捗カードの「読み上げ」を足した（取り直している間は `aria-live="off"`。メインのチャットの進捗カードが 2 秒ごとに読み上げ直されていた。PR #1141 のレビュー指摘） |
| 1.6 | 2026-11-11 | §6 の検出で、「全体の雰囲気」「具体的な」「体験版」のように別の語の中の「体」を体の枠と読まないようにした（PR #1141 のレビュー指摘） |
| 1.7 | 2026-11-11 | §2.3 の ComfyUI エンジンのカードに、更新・入れ直しでもビルドを GPU の推奨から選ぶことを足した（実配線で推奨が導入前にしか埋まらず、更新が「対応する GPU が見つかりません」で断られていた。PR #1141 のレビュー指摘） |
| 1.8 | 2026-11-11 | §6 の枠の語の例外を、決め打ちの語（全体・具体・主体・本体・体験・体力ほか）に絞り、「体型」を体として読み、名前を伏せ字にした文から読まないようにした（1.6 の直しが起こした不具合。PR #1141 のレビュー指摘） |
