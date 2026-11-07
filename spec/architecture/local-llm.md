# ローカル LLM（OpenAI 互換サーバー）

Issue #1008（前半）。LLM のプロバイダーに、自分の PC や LAN で動く **OpenAI 互換のサーバー**（Ollama・LM Studio・llama.cpp server）を足す。API キー不要・オフライン・内容の制限を自分で決められる形で、企画・脚本・Shot 計画などを回せるようにする。Reasoning LLM Adapter の全体は [`model-adapters.md`](model-adapters.md) §2。共通のエラーは [`errors.md`](errors.md)。モデルの絞り込み・使えるかどうかは [`model-manager/model-filter.md`](model-manager/model-filter.md)。

**構成**: §1〜§9 は「すでに動いている OpenAI 互換サーバー」につなぐ部分（前半）。**GGUF のカタログ（モデルマネージャーからのダウンロード）と、mam が管理する llama.cpp server（同梱ランタイム）は §10**（後半）。§7 の推奨モデルは、そのカタログに入れた 6 系統の調査結果。

## 1. プロバイダー `local`

- `llm.ProviderLocal = "local"`（`llm.Providers()` の 6 つ目）。`llm.json` の `providers.local.base_url` がサーバーのアドレス（既定 `http://localhost:11434`＝Ollama）。API キーは**任意**で、`credential.Store` のプロバイダー名 `local` に保存する（キーが無ければ `Authorization` を付けない。キーは llm.json に書かない）。
- 呼び出しは `POST {base_url}/v1/chat/completions`、モデル一覧は `GET {base_url}/v1/models`。OpenAI の Chat Completions と同じ形だが、ローカルのサーバー向けの違いを持つ別のバックエンド（`internal/agent/llm/localllm`）にした: 上限は `max_tokens`（`max_completion_tokens` は OpenAI の新しい名前で、古いサーバーは知らない）、キーは任意、アドレスは実行中に変えられる、JSON の欄は拒否されたら外す（§3）、答えの `<think>` を取り除く（§3）。
- **アドレスの検証**（`localllm.NormalizeBaseURL`。`llm.Config.Validate` と設定画面・MCP の保存が使う）: 登録済みの接続先（ローカル・代替モデル）と同じ規則 `localcfg.CheckBaseURL` — `http` / `https` のみ、ホストあり、ユーザー情報・クエリ・フラグメントなし、リンクローカル（169.254.0.0/16・fe80::/10。クラウドのメタデータ）・未指定・マルチキャストのアドレスは拒否。ローカルのサーバーを指すので **http も、localhost・LAN・tailnet も許す**（クラウドのプロバイダーの `base_url` は従来どおり検証しない運営者の設定）。末尾の `/` と `/v1` は外して保存する（LM Studio の表示そのまま `http://localhost:1234/v1` を貼ってよい）。接続の段でも `safefetch.NewEndpointClient()`（接続先のアドレスを解決したあとに検査する。名前がリンクローカルに解決される・DNS リバインディングを止める。プロキシ設定は使わない。別ホストへのリダイレクトは追わない）を使う。`NewBackends` に別のクライアントを渡したとき（テスト）だけそれを使う。
- **アドレスは再起動なしで効く**: `llm.Client.Apply` が `providers.local.base_url` を稼働中のバックエンドに渡す（`llm.LiveBaseURL`）。ほかのプロバイダーの `base_url`・タイムアウトなどは従来どおり再起動後（「再起動して反映」）。モデル一覧のキャッシュは「アドレス＋キーの指紋」を鍵にするので、アドレスやキーを変えると前のサーバーの一覧は返さない。
- **料金は 0**: `llm.PriceOf` はプロバイダー `local` のモデルを、名前に関わらず 0 USD にする（自分の PC で動き、誰も課金しない）。見積りに「単価が分からない」の警告を出さない。
- **タイムアウト**: 1 回の呼び出しは `timeout_seconds`（既定 120 秒）。大きなモデル（27B を VRAM に収まらない量子化で動かす等）で遅いときは `llm.json` の `timeout_seconds` を延ばす（再起動後に効く）。サーバーが応答しない（接続拒否・名前解決失敗）は status 0 の `APIError` で、従来どおり再試行（指数バックオフ）・サーキットブレーカー・代替プロバイダーへの切り替えの対象。

## 2. モデル一覧・使えるか（#1013）

- `llmmodels.Service.List("local")`: `GET /v1/models` の `data[].id`（埋め込み・rerank・whisper を含む名前は除く）。**キーは要らない**（あれば付ける）。10 分キャッシュ、失敗は 1 分。アドレスは `Options.LocalBaseURL`（設定画面で変えられるので呼び出しのたびに読む）。
- サーバーに接続できない・一覧を読めないときは `Fallback = FallbackLocalDown`（文言「ローカルサーバーに接続できません」）、`Source = default`（mam が持つ候補は無いので `Models` は空）、`Availability` は**使えない**（`Reason = endpoint_unusable`、`settings_route = /settings/llm`、メッセージに「設定の「テキスト生成（LLM）」の「ローカル LLM サーバー」で確かめます」）。接続できてモデルが 0 件のサーバーは、接続できているので使える（`SourceAPI`・空の一覧）。
- `Availability("local")`（ネットワークを使わない）は、`List` が覚えている結果に従う（成功なら使える、失敗の記憶〜1 分は使えない）。まだ一度も読んでいなければ使えるとみなす（キーのあるプロバイダーと同じ）。
- `Candidates` の項目（`Candidate.Item`）は `Kinds=[llm]`、`Origin=local`（このアプリが動く PC か LAN のサーバー）、`Installed=true`（サーバーが一覧に出したもの）、`Provider=local`。ピッカーの初期の絞り込み（使えるものだけ）で、接続できているローカルのモデルが出て、接続できないときは「使えないものも表示」に理由つきで出る。`Warnings` は、保存済みの割り当てが接続できないサーバーを指しているときに警告する（保存は拒まない）。
- 割り当て: 14 の Agent すべての primary / fallback に `local` を選べる（`llmassign` の検証は `llm.Providers()` に従う。モデル名は空・空白・改行・128 バイト超を拒否する従来の規則のまま）。`set_llm_assignment`（MCP）も同じ。一覧にないモデル名は拒否せず警告して保存する（接続できないサーバーのときは「確かめられません」の警告）。

## 3. 構造化出力（JSON）

ローカルのモデルは JSON を守らないことが多い。汎用の処理は `llm` パッケージに入れたが、**既存のクラウドのプロバイダーの挙動は変えない**（テストで担保）。

1. **JSON モード**: `Request.JSON` の呼び出しで `response_format: {"type":"json_object"}` を送る。サーバーが HTTP 400 / 422 で拒否したら、その欄を外して同じ呼び出しをやり直す（呼び出しの内側。ブレーカーに数えない）。外して通ったら、その（サーバー, モデル）では覚えて、次から送らない。
2. **thinking のオフ**: JSON を読む役割（`Request.JSON`）だけ、`chat_template_kwargs: {"enable_thinking": false}`（llama.cpp server・LM Studio が chat template に渡す）を送る。拒否されたら同様に外して覚える。JSON でない呼び出し（本文を書く役割）には送らない（thinking のあるモデルの本来の動作）。拒否の文面が欄名（`response_format` / `json` / `chat_template_kwargs` / `enable_thinking` / `template`）を言っていればその欄を、言っていなければ thinking の欄を先に外す。400 / 422 以外（404 のモデルなし・500 ほか）は欄の問題ではないのでそのまま返す。
3. **`<think>…</think>` の除去**（`localllm.StripThinking`）: 答えを返す前に、`<think>…</think>` を取り除く。DeepSeek R1 の chat template は開きタグを prompt に入れるため、答えに閉じタグだけが現れる — 閉じタグの前を捨てる。トークン切れで閉じていないブロックは、その手前までを答えにする。除去したあとが空なら「答えにテキストがない」エラー。JSON の中の `{` を `extractJSONObject` が拾い間違えないように、除去は JSON を読む前（バックエンドの返り値の段）に行う。
4. **パース失敗のリトライ**（`llm.CompleteJSONResponse`）: **ローカルのモデルの答え**が JSON として読めないとき、理由（空・JSON なし・閉じていない・形が違う）と「JSON のオブジェクトだけを返す」を prompt に足して、**最大 `MaxJSONRetries = 2` 回**やり直す（呼び出しは最大 3 回）。トークンは全呼び出しの合計を `Response.Usage` に入れる。クラウドのプロバイダーは 1 回のまま（従来）。
5. **失敗の文面**: 3 回とも読めなかったら `ErrInvalidOutput`（共通エラー `llm_invalid_output`）に、モデル・呼び出し回数・最後の答えを読めなかった理由（「答えが空」「JSON のオブジェクトがない」「JSON が途中で切れている（トークン切れかもしれない）」「`{` と `}` の間が JSON ではない」）と JSON のデコードエラーを付ける。

## 4. エラー（#1010）

- 新しいコード `local_llm_unreachable`（クラス `upstream`、導線 `/settings/llm`）: 原因「ローカルサーバーに接続できません。ローカルの LLM サーバー（Ollama・LM Studio・llama.cpp server など）が応答しませんでした。」、直し方「サーバーが起動しているか、…ベース URL（Ollama は http://localhost:11434、LM Studio は http://localhost:1234）が合っているかを確かめてください。LM Studio はローカルサーバーを起動（Start Server）する必要があります。」（英語版もカタログにある）。
- `errmap.classify`: `localllm.IsUnreachable(err)`（プロバイダー `local` の `APIError` で status 0・原因あり・タイムアウトでもキャンセルでもない）→ `local_llm_unreachable`。モデルが無い（404）・認証（401 / 403）・その他は、既存の `remote_not_found`・`auth_failed`・`request_rejected` など。企画のジョブが失敗したときのチャット通知・MCP の `failure` は、既存の経路（`errmap.Explain`）でこのコードになる。
- 接続テストの失敗（§5）も同じコード（`llmlocal.TestResult.Error`）。

## 5. 設定画面・MCP・VRAM

### 5.1 設定画面のカード「ローカル LLM サーバー（OpenAI 互換）」

> **#1104 で、このカードはモデルマネージャーの分類 `local-llm` の区画「別のサーバーにつなぐ」（`#mm-llm-connection`）へ移る**（状態: 実装済み。§11）。以下の欄・保存・接続テストの意味は変わらず、置き場所と URL だけが変わる（`PUT /model-manager/local-llm/connection`・`POST /model-manager/local-llm/connection/test`。設定の `PUT /settings/llm/local`・`POST /settings/llm/local/test` は廃止）。設定「テキスト生成（LLM）」に残るのは、モデルマネージャーへのリンクのカード 1 枚だけ。

「テキスト生成（LLM）」（`/settings/llm`）の Agent の表の下に、`settingsui.CardItem` のカード（`organisms/settings/llmlocal`、`service/llmlocal`）。API キーのカード（「API キーとログイン」）は隠さない・変えない（`local` のキーはこのカードで扱い、`CredentialProviders` には足さない）。

- 欄: ベース URL（既定のプレースホルダー `http://localhost:11434`・例の説明）、API キー（任意。`type=password`・保存済みなら「設定済み（末尾 4 文字）。空欄なら変更しません」・画面に値は出さない）、「保存したキーを削除する」（保存済みのときだけ）、スイッチ「画像・動画を作る前に、Ollama のモデルをメモリから降ろす」（§5.3）。ボタン「保存」「接続テスト」。
- `PUT /settings/llm/local`（`base_url`・`api_key`・`clear_key`・`unload`＋`unload_present`）→ カードを返す。200 は「保存しました。次の LLM の呼び出しからすぐ使われます（再起動は要りません）」、422 は理由と、打った内容（キーは返さない）・`aria-invalid`、409 は設定ファイルが `--llm-config` で固定されているとき（アドレスとスイッチは変えられない。キーだけは保存できる）、503 はキーストアの失敗。アドレスは `llm.json` の `providers.local`（既定と同じなら書かない。ほかのキーは残す。保存前に `llm.ParseConfig` で検査）。
- `POST /settings/llm/local/test`（`base_url`・`api_key`。空なら保存したもの。キーは保存しない。`api_key` が空のとき保存済みのキーを付けるのは、`base_url` が空か保存済みのアドレスと同じときだけ。別のアドレスには保存済みのキーを送らない）→ 結果だけを返す。成功は「接続できました（モデル N 件）」と先頭 20 件のモデル名（0 件なら `ollama pull` / LM Studio でダウンロードの案内）。接続できないときは共通のエラー表示（`errorbox.Box`、コード `local_llm_unreachable`）。401 / 403 は `auth_failed`、ほかの HTTP エラーは `upstream_failed`（理由つき）。拒否されるアドレス（リンクローカルなど）は試さずに 422。
- 画面の「LLM の割り当て」のモデル選択（共通のモデル選択モーダル）は #1011 の担当。

### 5.2 MCP

- `set_local_llm_server`（`base_url?`・`api_key?`・`clear_api_key?`・`unload_before_generation?`。指定した項目だけ変わる。キーは返さない）、`test_local_llm_server`（`base_url?`・`api_key?`。接続できないときは `ok: false` と `failure`）。`get_global_settings` の `llm.local_server` が今の設定（アドレス・キーの有無・スイッチ・固定か）。`list_llm_models` の `local`、`set_llm_assignment` の `local` は §2。詳細は [`../api/mcp-tools-settings-policy.md`](../api/mcp-tools-settings-policy.md)。

### 5.3 VRAM の取り合い（決めたこと）

ComfyUI と同じ GPU（16GB 級）でローカル LLM を動かすと、LLM のモデルがメモリに残っている間は画像・動画の生成が VRAM 不足（`vram_oom`）になりうる。決め:

1. **設定でオンにしたときだけ**、ローカルの画像・動画の生成を始める前に、Ollama のモデルをメモリから降ろす。既定はオフ（`providers.local.unload_before_generation`）。理由: 降ろすと次の LLM の呼び出しでモデルを読み込み直すので、数秒〜数十秒遅くなる。CPU メモリや別の GPU に余裕がある人には不要。Ollama 以外のサーバーを黙って操作しない。
2. **実装**（`llmlocal.Service.BeforeGeneration`／`UnloadModels`）: `GET {base_url}/api/ps`（Ollama が今メモリに置いているモデルの一覧。公式の API）で名前を取り、各モデルに `POST {base_url}/api/generate` `{"model": <名前>, "keep_alive": 0}`（公式の FAQ にある「すぐ降ろす」方法）。全体で 10 秒まで。キー（あれば）は付ける。アプリでは、ローカルのモデルの生成の前に呼ばれる `Prepare` のフック（同梱の ComfyUI を起動するのと同じ場所）から呼ぶ。**失敗は警告ログだけで、生成は止めない**（降ろせなくても生成は試せる）。
3. **Ollama 以外**（LM Studio・llama.cpp server）: `/api/ps` が無い（404）ので何もしない。LM Studio は「Idle TTL and Auto-Evict」（モデルを使わない間に自動で降ろす）をサーバー側で設定する。llama.cpp server は、mam が管理するもの（§10）なら生成の前に止まる。自分で立てた llama-server は、必要なときだけ立ち上げる、またはモデルサイズ・`-ngl`（GPU に載せる層の数）で VRAM に収める。mam の画面・API には、これらを降ろす操作は入れていない（OpenAI 互換の API には「降ろす」が無い）。
4. **実機での確認はしていない**: Ollama の `/api/ps`・`keep_alive: 0` は公式ドキュメントの API に合わせて実装し、偽の Ollama サーバー（httptest）でリクエストの形をテストした。実際の Ollama と ComfyUI を同時に動かしての確認は device_only（悠太さんの PC）。
5. mam が管理する llama.cpp は、画像・動画の生成の前に**止めて VRAM を空ける**（§10.3）。LLM の起動の前に ComfyUI が生成中なら待ち、空いていれば ComfyUI のモデルを降ろす（§10.3。逆方向）。Ollama 以外の外部サーバーは従来どおり何もしない。

## 6. 非機能の注意

- 内容の制限をなくしたモデル（abliterated）を使うかどうかは利用者の判断。この版は、LLM の出力への検査を足したり外したりしていない（従来どおり）。年齢確認・成人向けモデルの扱いは生成モデル側の規則のまま。
- ローカルのサーバーに送る内容（Creative Brief・脚本・画像）は、その PC かネットワーク内に留まる。アドレスが LAN・tailnet のときは、そのネットワーク上の通信（http）になる。
- thinking のあるモデルは JSON を読む役割で遅く・長くなりがち。`enable_thinking=false` が効かないモデル（DeepSeek R1 系）は `max_tokens`（既定 8192）の中に考える過程が入るので、足りないときは §3 のリトライと「トークン切れかもしれない」の理由が出る。

## 7. 推奨する LLM（GGUF）の候補 — 調べた結果（2026-11-03 に Hugging Face の API で確認）

16GB VRAM で ComfyUI と同居する前提。GGUF の置き場所（mradermacher）はすべて gated なし（認証・同意なしでダウンロードできる）。**利用規約・ライセンスへの同意は、この調査ではしていない**（Hugging Face のページ・API を読んだだけ。gated のモデルは候補に入れていない）。ライセンスは各リポジトリの `license` タグ（カードのメタデータ）で、法的な保証ではない。商用利用の前に、元のライセンス全文を読むこと。

| # | 系統 | 種類 | 元の重み | GGUF（推奨ファイル） | サイズ | ライセンス（確認結果） | 16GB VRAM | thinking |
|---|---|---|---|---|---|---|---|---|
| 1 | Gemma | 純正 | [google/gemma-4-12B-it](https://huggingface.co/google/gemma-4-12B-it)（12.0B） | [mradermacher/gemma-4-12B-it-GGUF](https://huggingface.co/mradermacher/gemma-4-12B-it-GGUF) `gemma-4-12B-it.Q4_K_M.gguf` | 6.87 GiB | Apache-2.0（商用可） | 余裕あり。ComfyUI と同居しやすい | あり。`enable_thinking=false` でオフ |
| 2 | Gemma | 非検閲 | [huihui-ai/Huihui-gemma-4-12B-it-abliterated](https://huggingface.co/huihui-ai/Huihui-gemma-4-12B-it-abliterated) | [mradermacher/Huihui-gemma-4-12B-it-abliterated-GGUF](https://huggingface.co/mradermacher/Huihui-gemma-4-12B-it-abliterated-GGUF) `…abliterated.Q4_K_M.gguf` | 6.87 GiB | Apache-2.0（商用可。元の Gemma が Apache-2.0） | 同上 | 純正と同じ |
| 3 | Qwen | 純正 | [Qwen/Qwen3.8-27B](https://huggingface.co/Qwen/Qwen3.8-27B)（27.8B） | [mradermacher/Qwen3.8-27B-GGUF](https://huggingface.co/mradermacher/Qwen3.8-27B-GGUF) `Qwen3.8-27B.IQ4_XS.gguf`（軽量版 `Q3_K_M` 12.57 GiB） | 14.36 GiB | Apache-2.0（商用可） | 重量のみで 14GiB 超: **ComfyUI とは同居できない**（LLM を降ろす §5.3）。`Q4_K_M` は 15.66 GiB で 16GB に収まらない。画像・動画を作らない間なら動く | 初期オン。`enable_thinking=false` でオフ |
| 4 | Qwen | 非検閲 | [huihui-ai/Huihui-Qwen3.8-27B-abliterated](https://huggingface.co/huihui-ai/Huihui-Qwen3.8-27B-abliterated) | [mradermacher/Huihui-Qwen3.8-27B-abliterated-GGUF](https://huggingface.co/mradermacher/Huihui-Qwen3.8-27B-abliterated-GGUF) `…abliterated.IQ4_XS.gguf`（軽量版 `Q3_K_M` 12.57 GiB） | 14.36 GiB | Apache-2.0（商用可。元の Qwen が Apache-2.0） | 同上 | 純正と同じ |
| 5 | DeepSeek | 純正 | [deepseek-ai/DeepSeek-R1-Distill-Qwen-14B](https://huggingface.co/deepseek-ai/DeepSeek-R1-Distill-Qwen-14B)（14.8B） | [mradermacher/DeepSeek-R1-Distill-Qwen-14B-GGUF](https://huggingface.co/mradermacher/DeepSeek-R1-Distill-Qwen-14B-GGUF) `DeepSeek-R1-Distill-Qwen-14B.Q4_K_M.gguf` | 8.37 GiB | MIT（商用可） | 動く。ComfyUI と同時は小さめの生成のみ | 常にあり。公式のオフ手段なし → `<think>` を取り除く（§3） |
| 6 | DeepSeek | 非検閲 | [huihui-ai/DeepSeek-R1-Distill-Qwen-14B-abliterated-v2](https://huggingface.co/huihui-ai/DeepSeek-R1-Distill-Qwen-14B-abliterated-v2) | [mradermacher/DeepSeek-R1-Distill-Qwen-14B-abliterated-v2-GGUF](https://huggingface.co/mradermacher/DeepSeek-R1-Distill-Qwen-14B-abliterated-v2-GGUF) `…abliterated-v2.Q4_K_M.gguf` | 8.37 GiB | **商用不明**（派生側のカードに `license` の記載がなく、確認できなかった。元は MIT だが、派生の条件は確かめられていない） | 同上 | 純正と同じ |

読み方:

- サイズは GGUF ファイルの大きさ（重みだけ）。実際の VRAM は、これに KV キャッシュ（文脈の長さ × 並列数）と実行環境の分が加わる。`OLLAMA_CONTEXT_LENGTH` を大きくするほど増える。16GB で ComfyUI と同居するなら、1・2（約 7 GiB）が現実的。
- 「商用可」はカードのライセンス表示（Apache-2.0 / MIT）による。表示が無い 6 は「商用不明」とし、悠太さんの OK が出るまで商用可として扱わない。カタログに入れるときは「商用不明」を表示する（別の担当）。
- モデル名（Gemma 4・Qwen3.8 など）は 2026-11-03 の Hugging Face の記載のまま。量子化は mradermacher が公開しているもの（`IQ4_XS`・`Q3_K_M`・`Q4_K_M` ほか `Q2_K`〜`Q8_0`）。
- 非検閲（abliterated）モデルは、拒否の挙動を取り除いた派生で、元のモデルより出力の質や指示への従い方が落ちることがある。JSON を守る力も落ちうるので、§3 のリトライの回数は小さめ（2 回）にしてある。

## 8. 使い方（利用者向けの手順）

### 8.1 Ollama

1. [Ollama](https://ollama.com/download) を入れて起動する（既定で `http://localhost:11434`）。
2. モデルを取る。Hugging Face の GGUF はそのまま取れる: `ollama pull hf.co/mradermacher/gemma-4-12B-it-GGUF:Q4_K_M`（`ollama list` で名前を確かめる）。
3. 文脈の長さを確かめる。Ollama の既定は 4096 トークン（`OLLAMA_CONTEXT_LENGTH` で変える）で、mam の企画・脚本の入力は長く、既定のままだと途中が切れて答えが崩れることがある。`OLLAMA_CONTEXT_LENGTH=16384 ollama serve`（systemd・Windows・macOS の環境変数の設定は Ollama の FAQ）。大きくするほど VRAM を使う。
4. mam の設定「テキスト生成（LLM）」→ カード「ローカル LLM サーバー」で、ベース URL を `http://localhost:11434` にして「接続テスト」→ 「保存」。モデルの一覧に `ollama list` の名前（`hf.co/…:Q4_K_M` など）が出る。
5. 「LLM の割り当て」で Agent のプロバイダーを `local`、モデルを選ぶ。
6. ComfyUI と同居するなら、カードの「画像・動画を作る前に、Ollama のモデルをメモリから降ろす」をオンにする（§5.3）。降ろさない代わりに `OLLAMA_KEEP_ALIVE=0` を Ollama 側に設定する方法もある（使うたびに降ろす）。

### 8.2 LM Studio

1. [LM Studio](https://lmstudio.ai/) でモデルをダウンロードする（検索欄に `mradermacher/gemma-4-12B-it-GGUF` など。量子化は `Q4_K_M`）。
2. 「Developer」タブ（または `lms server start`）で**ローカルサーバーを起動**する（既定 `http://localhost:1234`）。モデルは読み込んでおく（または Just-In-Time 読み込み）。LAN の他の PC から使うときは「Serve on Local Network」。
3. mam のカードのベース URL を `http://localhost:1234`（`/v1` を付けてもよい）にして「接続テスト」→「保存」。
4. 「Idle TTL and Auto-Evict」を設定すると、使っていないモデルが自動で降ろされ、ComfyUI との VRAM の取り合いが減る（mam からは操作しない）。

### 8.3 llama.cpp server

1. `llama-server -m <モデル>.gguf --port 8080 -ngl 99 -c 16384` のように起動する（`-ngl`: GPU に載せる層の数、`-c`: 文脈の長さ）。`--jinja` を付けると chat template を使うモデルで `enable_thinking` などが効く。
2. mam のカードのベース URL を `http://localhost:8080` にする。サーバーを `--api-key` で起動したときだけ、カードの API キーに同じキーを入れる。
3. llama-server は 1 つのモデルを読み込んで動かす。モデルの一覧には読み込んだ 1 つが出る。

## 9. テスト

- `llm/localllm`（偽のサーバー `httptest`）: `/v1/chat/completions` の形・キーの有無・JSON モードと `chat_template_kwargs` の拒否→外して再試行→覚える・欄以外の拒否はそのまま返す・`<think>` の除去・思考だけの答えは空・`/v1/models`・アドレスの検証（`NormalizeBaseURL`、`Config.Validate`）・接続時の保護（リンクローカル）・接続できない（`IsUnreachable`）・`Client.Apply` でアドレスが再起動なしで変わる・JSON のリトライ（ローカルは最大 2 回、クラウドは 1 回のまま、トークンの合計）・料金 0。
- `llmmodels`: 一覧（キーなし）・キーとアドレスの変更で読み直す・接続できない（理由・使えない・`/settings/llm`）・モデル 0 件は接続できている・`Candidates`（Origin=local）・`Warnings`。
- `llmlocal`: 保存（llm.json の他のキーを残す・既定は書かない・固定は 409 相当）・キー（保存・保持・削除・値を出さない）・検証・接続テスト（成功・接続不可の共通エラー・401・404・HTML）・Ollama のアンロード（スイッチ・Ollama 以外・接続不可）。
- 画面（`llmsettingstest`）と MCP（`tools/settings`）: カードの保存・422・409・接続テスト・共通のエラー表示、`set_local_llm_server`・`test_local_llm_server`・`list_llm_models`・`set_llm_assignment`（14 の Agent すべてに `local`）。
- 統合（`orchestrator`）: 偽の OpenAI 互換サーバーで、Producer・Writer・Director・Storyboard のすべてを `local` に割り当て、`response_format` を拒否するサーバー・`<think>` つきの答え・コードフェンスの答え・最初は JSON でない答え（リトライ）を含めて、**企画が Shot 計画・絵コンテまで最後まで通る**（`TestHandle_PlanningRunsToTheEndOnALocalOpenAICompatibleServer`）。
- **device_only（未確認）**: 実際の Ollama・LM Studio・llama.cpp server につないでの企画の通し、実際の Ollama の `/api/ps`・`keep_alive: 0` での VRAM の解放と ComfyUI との同居、各モデルの出力の質・JSON の守り方。

## 10. mam が管理する llama.cpp（GGUF の LLM。Issue #1008 後半）

モデルマネージャーの「ローカル」から llama.cpp の `llama-server` と GGUF の重みを導入し、mam が起動・停止して LLM のプロバイダー `local` から使う。ランタイムの定義・導入・モデルの選択は [model-manager/local-runtime.md §17](model-manager/local-runtime.md)（Irodori-TTS と同じ基盤）。この節は、LLM として使う側。

### 10.1 「この PC の llama.cpp を使う」（管理されたサーバー）

- 設定「テキスト生成（LLM）」のカード「ローカル LLM サーバー」に、チェックボックス「この PC の llama.cpp を使う」（`providers.local.use_managed_runtime`。既定オフ）。**#1104 のあと、これはモデルマネージャーの「使うサーバー」（`#mm-llm-use`）のラジオ `llama-cpp` になる**（§11）。オンの間、プロバイダー `local` の呼び出しは**ベース URL・API キーを使わず**、管理サーバーの `http://127.0.0.1:<ポート>` に向かう。起動中はそのポートを自動で使い（ポートは起動のたびに mam が決める。前回のポートを優先）、ベース URL の欄は別のサーバー（Ollama など）用にそのまま残る。オフにすれば元のアドレスに戻る。
- カードは、サーバーの状態（`unsupported` この PC では使えない／`not_installed` 未導入か重みが不足／`stopped` 導入済み（最初の呼び出しで起動）／`starting`／`running`／`failed`）と、使うモデル名、理由と何をすればよいか、モデルマネージャーへのリンクを出す。管理サーバーが無い PC（macOS など）では、理由とクラウド・Ollama への案内。モデルの導入と選択はモデルマネージャーでする（カードは選ばない）。
- **モデル名**: 管理サーバーが答えるモデル名は、選んだモデルの id（`--alias`。例 `gemma-4-12b-it`・`qwen3.8-27b`）。`list_llm_models` の `local` と LLM の割り当てのモデル選択には、管理サーバーを選んでいる間は**そのモデル 1 つだけ**が出る（止まっていても出る。未導入・非対応は 0 件）。割り当ての `model` にその id を指定する。モデルを切り替える（モデルマネージャー・MCP の `select_local_runtime_model`）とサーバーが止まり、次の呼び出しで新しいモデルが起動する。
- **起動**: 最初の LLM の呼び出し（`localllm` のバックエンドが `Endpoint` を呼ぶ）でサーバーが止まっていれば起動し、`/health` が 200 になるまで待つ（モデルの読み込みで数十秒〜数分。起動は呼び出しのものではなく、ランタイムの `start_timeout_sec`（llama.cpp は 900 秒）だけが区切る。呼び出しは完了を待つだけで、`timeout_seconds` が先に切れても読み込み中のサーバーは止められず、その呼び出しだけが失敗する。次の呼び出しが同じ起動に合流する。同時の呼び出しでも起動は 1 回。大きなモデルでは、最初の呼び出しが失敗しても 2 回目以降で使えるようになる）。自動起動（モデルマネージャーのカードのスイッチ）をオンにすれば mam の起動時に起動する（既定オフ）。起動できないとき（非対応・未導入・重みが不足・ComfyUI が生成中のまま・起動の失敗）は、理由つきのエラーで、**黙って別のアドレスに切り替えない**。エージェントの呼び出しからは「ローカルサーバーが応答しない」と同じ扱い（`local_llm_unreachable`。再試行・代替プロバイダーへの切り替えの対象）。
- **接続テスト**: 管理サーバーを選んでいる間は、フォームのアドレスを試さず、サーバーの状態を言う（起動はしない。モデルの読み込みは接続テストより長いため）。MCP は `set_local_llm_server` の `use_managed_runtime`、`get_global_settings` の `llm.local_server.managed_runtime`（`state`・`reason`・`model`）。

### 10.2 コマンドラインは外から指定できない

起動の引数は**カタログの固定のテンプレート 1 つ**で、利用者・設定・MCP が渡せるのはモデルの選択（カタログの id）だけ（local-runtime.md §17.1）。任意の実行ファイルも任意の引数も受け付けない。サーバーは `127.0.0.1` だけで待ち受け、起動のたびの乱数の合言葉（`LLAMA_API_KEY`）を `/health` 以外のすべての要求に求める。プロバイダーはこの合言葉を `Authorization: Bearer` に付ける。設定の API キーは管理サーバーには送らない（別のサーバーのキーを漏らさない）。

### 10.3 VRAM の取り合い（決めたこと）

16GB VRAM で ComfyUI と同居する前提。LLM はオーケストレーターが常に呼ぶので、画像・動画の生成と LLM が同じ GPU を取り合う。決め:

| 場面 | 動作 |
|---|---|
| LLM を**起動する**とき | 同梱の ComfyUI が**生成中なら待つ**（`gpufree.Guard.MakeRoom` が ComfyUI の `GET /queue` を見る。最長 5 分、3 秒ごと。呼び出しの文脈が先に切れればそれまで）。まだ空かなければ「ComfyUI が画像・動画を生成している最中で、GPU のメモリが空きません」で断る。生成中でなければ ComfyUI に `POST /free`（モデルのアンロード）を送ってから起動する。Irodori-TTS が使う GPU 調停と**同じ部品**（`gpufree.Guard`。声は生成中なら待たずに断り、LLM は待つ） |
| 画像・動画の生成を**始める**とき | 管理サーバーを選んでいれば、サーバーを**止める**（VRAM を返す。27B は ComfyUI と同居できず、12B・14B も生成の余裕を空けるため）。次の LLM の呼び出しで、モデルを読み込み直して起動する（数十秒〜数分）。Ollama のアンロードのスイッチ（§5.3）とは独立で、管理サーバーには常に効く |
| LLM が**使われない**間 | `--sleep-idle-seconds 900`（llama-server 自身が 15 分使われなければモデルをメモリから降ろす。次の呼び出しで読み込み直す） |

Qwen 27B は IQ4_XS（14.36 GiB）でも 16GB の GPU に収まらないぶんは CPU に置いて動き、ComfyUI とは同時に動かせない。Gemma 12B（約 7 GiB）なら同居しやすい。**実機での同居の確認は device_only**。

### 10.4 実装

- `service/llamaserver.Server`（`llmlocal.Managed`）: `Endpoint`（必要なら ComfyUI を待って起動し、アドレスと合言葉を返す）・`Stop`・`Info`（起動せずに状態）。`localruntime.Service` を呼ぶ（導入・重み・非対応の拒否は他のランタイムと同じエラー。文面にしたのが `llamaserver.Explain`）。`llmlocal.Service` が `SetManaged` で持ち、`localllm.WithManaged` でバックエンドへ、`llmmodels.Options.LocalManaged` でモデル一覧へ、`BeforeGeneration`（生成の前の `Prepare` のフック）で停止へ結ぶ（`app/services.go`）。
- `gpufree.Guard.MakeRoom` を、声の `Before` と LLM の起動で共有する（`irodoriwire.Guard`）。

### 10.5 thinking

JSON を返す役割は `enable_thinking=false`、DeepSeek は `<think>` を除去（§3）。管理サーバーでも同じバックエンド（`localllm`）が呼ぶので、そのまま効く。偽の llama-server で、Gemma・Qwen のモデルは thinking が切れ（JSON の役割だけ）、DeepSeek は `<think>` 付きの答えから JSON を読めることを確かめた（`llamaservertest`）。

### 10.6 テスト・未確認

`llamaservertest`（偽の llama-server。local-runtime.md §17.6）・`localllm`（管理サーバーへの結びつき・キーの扱い）・`llmlocal`（`use_managed_runtime` の保存・固定・停止）・`llmmodels`・`gpufree`・設定画面（`llmsettingstest`）・MCP。**device_only**: 実機での導入・起動・推論、ComfyUI との同居、`--sleep-idle-seconds` での VRAM の解放、各モデルの質と JSON の守り方。

## 11. モデルマネージャーとの統合（Issue #1104。状態: 実装済み。管理する Ollama〔`use` が `ollama`〕は #1105 で実装済み）

ローカル LLM を、モデルマネージャーの分類 `local-llm`「ローカル LLM」の 1 つの導線にする。画面の構成・ID・テストは [../components/model-manager-screen.md](../components/model-manager-screen.md) の「ローカル LLM の統合（#1104）」、ルートは [../api/endpoints-model-manager.md](../api/endpoints-model-manager.md)。

- **向き先は 1 つ（`use`）**: プロバイダー `local` の呼び出しは、次の 3 つのどれか 1 つだけに行く。`llama-cpp`（mam が導入・起動する llama.cpp。§10）・`ollama`（mam が導入・起動する Ollama。[model-manager/local-runtime.md](model-manager/local-runtime.md) §19。#1105）・`custom`（別のサーバー。LM Studio・自分で入れた Ollama・llama-server など。`providers.local.base_url`。§1）。保存は `llm.json` の `providers.local` の 2 つのブール値: `use_managed_runtime`（llama.cpp。今のまま）と `use_managed_ollama`（Ollama。新設。既定偽）。2 つは同時に真にならない（`llm.ParseConfig` が検査し、両方真は `InvalidError`。`llmlocal.Service.SetUse` が片方を真にするときもう片方を偽にする）。両方偽が `custom`。
- **管理サーバーでの呼び出し**: `ollama` のときは llama.cpp と同じく**ベース URL・API キーを使わず**、管理している Ollama の `http://127.0.0.1:<ポート>` に向かう。Ollama には API キーの仕組みが無いので、`Authorization` は付けない（llama.cpp の合言葉〔§10.2〕に当たるものが無い。理由と受け入れる危険は local-runtime.md §19.4）。モデルの一覧（§2）は、そのサーバーの `GET /v1/models`（Ollama に取得済みのモデル）。割り当ての `model` は Ollama のモデル名（`gemma4:12b` など）。
- **状態の言い方**: `use` の状態は 1 つの `Info`（`state`・`reason`・`model`）にまとめ、設定のカード（`llm-local-state`）・モデルマネージャーの概要・`#mm-llm-use-state`・MCP の `get_global_settings` の `llm.local_server.managed_runtime`（今は llama.cpp だけ。`ollama` のときもこの名前で同じ形を返し、どちらのサーバーかは新設の `llm.local_server.use`〔`llama-cpp`・`ollama`・`custom`〕で分かる）が同じ文を出す。
- **Ollama のメモリ降ろし（§5.3）**: 降ろす相手は `use` が `custom` のとき `base_url`、`ollama` のとき管理している Ollama（`/api/ps`・`keep_alive: 0`）。`llama-cpp` のときは今のとおり llama.cpp を止める（§10.3）。スイッチ `unload_before_generation` は `ollama`・`custom` のときだけ意味を持つ。
- **VRAM の取り合い**: `ollama`（管理）は llama.cpp と同じ扱い（LLM の起動の前に ComfyUI が生成中なら待つ・画像・動画の生成の前に止める・アイドルで降ろす）。詳細は local-runtime.md §19.5。
- **取得中は止めない**: 管理している Ollama でモデルを取得（`ollama pull`）している間は、サーバーの停止（カードの「停止」）・削除・導入し直しを `ErrBusy`（409。理由に取得中のモデル名）で断る（取得が途中で失敗するため。local-runtime.md §19.1）。LLM の呼び出しの前の起動と、生成の前のメモリ降ろし（止めずに `keep_alive: 0`。§5.3）は影響を受けない。管理する Ollama は、利用者自身の `OLLAMA_*`（`OLLAMA_ORIGINS` など）を継承しない（local-runtime.md §19.1）。

## 改訂履歴

| 版 | 日付 | 内容 | 理由 |
|---|---|---|---|
| 1.1 | 2026-11-04 | §10 を足した: mam が管理する llama.cpp（GGUF の 8 モデル）を「この PC の llama.cpp を使う」でプロバイダー `local` に結びつける。VRAM の調停（ComfyUI が生成中なら待つ・生成の前に止める・アイドルで降ろす） | Issue #1008（後半） |
| 1.2 | 2026-11-06 | §11 を足した（モデルマネージャーとの統合。向き先 `use` を `llama-cpp`・`ollama`・`custom` の 1 つにし、`use_managed_ollama` を足す）。§5.1・§10.1 に、設定のカードがモデルマネージャーの `local-llm` へ移ることを書いた。状態は仕様のみ（未実装） | Issue #1104 |
| 1.0 | 2026-11-03 | 新規作成。プロバイダー `local`、モデル一覧・使えるか、JSON の扱い、エラー、設定画面・MCP、VRAM の取り合い、推奨モデルの調査、Ollama / LM Studio / llama.cpp server の使い方 | Issue #1008（前半） |
| 1.3 | 2026-11-07 | §11 を実装した。状態を「実装済み」にした。`llm.ProviderConfig.UseManagedOllama`（`use_managed_ollama`。`llm.ParseConfig` が `use_managed_runtime` との両方真を拒む）・`llmlocal.Service` の `Use`・`SetUse`・`Status`・`SetManagedOllama`（管理する Ollama を持つ app だけ `ollama` を選べる。#1105 がつなぐ）。使っている管理サーバーは、画像・動画の生成の前に止める（llama.cpp と同じ）。Ollama のモデルの一覧・メモリ降ろしの `/api/ps` 経路の管理 Ollama への適用は #1105 | Issue #1104 |
| 1.4 | 2026-11-07 | 管理する Ollama（`use` が `ollama`。#1105）を実装した。呼び出しは `llamaserver.Server{ID: "ollama", NoKey: true}`（認証ヘッダーなし）。モデルの選択は `GET /api/tags` の取得済みのモデルをすべて出す（`ManagedInfo.Models`）。画像・動画の生成の前は、llama.cpp のように止めず、スイッチ `unload_before_generation` がオンのときだけ `/api/ps` と `keep_alive: 0` で降ろす（`BeforeGeneration`。動いていなければ何もしない）。LLM の割り当てが使うモデルは、モデルマネージャーの削除が 422（`llmassign.UsedBy`）。実機での降ろしの効きは未確認（`device_only`） | Issue #1105 |
| 1.5 | 2026-11-07 | §11 に、管理する Ollama の取得中は停止・削除・導入し直しを 409 で断ること、利用者の `OLLAMA_*` を継承しないことを足した（実装済み） | PR #1110 のレビュー |
