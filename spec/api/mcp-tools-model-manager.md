# MCP ツール: モデルマネージャー

[`mcp-tools.md`](mcp-tools.md) の規約に従う、モデルマネージャー画面（[endpoints-model-manager.md](endpoints-model-manager.md)）の操作のツール（Issue #833）。ComfyUI エンジン・ローカルの画像・動画モデルのカタログ・ライセンスの同意。試し生成は「生成テスト」の MCP ツール（[mcp-tools-generation-test.md](mcp-tools-generation-test.md)）に移した（1.1）。意味は [../architecture/model-manager/model-manager.md](../architecture/model-manager/model-manager.md)。

実装は `backend/internal/mcp/tools/modelmgr`（エンジン・カタログ・トークン・外部の ComfyUI）。画面と同じ `modelmanager.Service`（`service.Services.ModelManager`）を呼ぶ。アプリが持たないとき（`ModelManager` が nil）は全ツールが `unavailable`。

## 方針

- **同意はサービスが強制する**: 同意が要るモデル（`consent_required`）の `install_model` は、同意の記録（ライセンスの ID と版）が無ければ `conflict` で拒否し、ダウンロードもジョブも始めない。エージェントは `get_catalog_model` でライセンスの要約・URL・商用利用の可否・地域の制限を取って**ユーザーに見せ**、ユーザーが同意したら `accept_model_license`（または `install_model` の `license_id`・`license_version`）で記録する。ID か版がカタログと違えば `validation`。カタログの版が変わると改めて同意が要る。
- **年齢確認もサービスが強制する**: 成人向けのモデル（`content_rating=adult`）は、年齢確認と注意書きへの同意が `state.json` に記録されるまで、`list_model_catalog` に出ず、`get_catalog_model`・`accept_model_license`・`install_model` は `conflict`（ダウンロードもジョブも始めない）。確認を記録できるのは `set_adult_content` だけで、**ユーザーが `get_adult_content_status` の注意書きを見て自分で答えた後にだけ**呼ぶ。エージェントが年齢や同意を代わりに答えてはならず、確認を自動で済ませる仕組みは無い。モデルごとのライセンスの同意（上）は、年齢確認とは別に取る（成人向けのモデルは `tier=advanced` で、同意が必須）。他の機能（#842 の Hugging Face の取り込みなど）が「確認は済んだか」を知るには `get_adult_content_status` の `enabled`（サービスでは `AdultContentEnabled()`）を見る。
- **長い操作は `job_id` を返す**: `install_model_engine`・`install_model`。`get_job` が状態・進捗・結果・失敗の理由を返す（下の「ジョブ」）。同じ対象の導入がすでに動いていれば新しいジョブは作らず、それを返す（`existing: true`）。導入のジョブは再開できる（`job_id` はジョブキューの行の ID なので、再起動後も `get_job` で読める）。
- **拒否は画面と同じ文面**（`mmview.Refuse`）: 入力の誤り・GPU が無い（CPU は `cpu=true` の明示）は `validation`、同意が無い・エンジン未導入・導入中・設定ファイルの固定（`--model-config`）・Windows 以外・空き容量不足は `conflict`、無いものは `not_found`。空き容量・GPU・同意の検査は、ダウンロードを始める前に行う。
- **同梱エンジンは Windows 版と Apple Silicon の Mac（macOS 14 以降）版だけ**: `get_model_engine_status` の `engine.supported` が `false` のときは、`install_model_engine`・`start_model_engine`・`install_model` は `conflict`（外部の ComfyUI の URL を [`create_local_endpoint`](mcp-tools-local-models.md) で登録して使う）。
- **Hugging Face のトークンは返さない**: 状態（設定済みか・末尾数文字）だけ。

## エンジン

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_model_engine_status` | 同梱の ComfyUI エンジンの状態と、ComfyUI の接続先の一覧 | なし | `engine`（`supported`, `installed`, `version`, `pinned_version`, `update_available`, `variant`, `cpu`, `broken`, `process`（`state`: stopped / starting / running / failed, `url`, `error`）, `auto_start`, `dir`, `writable`, `free_bytes`, `variants[]`, `install_job`, `license`）, `endpoints[]`, `pinned` | はい |
| `detect_gpu` | この PC の GPU と、推奨する ComfyUI のビルド | なし | `os`, `adapters[]`, `recommendation`（`variant`（空なら対応する GPU なし）, `reason`, `guidance`, `cpu_available`） | はい |
| `get_gpu_status` | GPU の**今の**状態（下の「GPU の今の状態」）。読み取り専用 | なし | `os`, `source`, `gpus[]`, `processes[]`, `unavailable`, `comfyui[]` | はい |
| `detect_comfyui` | この PC で動いている既存の ComfyUI（127.0.0.1 の 8188・8000） | なし | `found[]`（`url`, `version`, `desktop`, `devices`）, `recommended_url` | はい |
| `install_model_engine` | エンジンを取得して導入する（ジョブ） | `variant`（`nvidia` / `nvidia_cu126` / `amd` / `intel`。省略で GPU の推奨）, `cpu` | `job_id`, `state`, `existing`, `target`（`engine`） | いいえ |
| `start_model_engine` | 同梱の ComfyUI を起動し、応答するまで待つ（冪等） | なし | `state`, `port`, `url`, `pid`, `error` | いいえ |
| `stop_model_engine` | 停止する（冪等） | なし | 同上 | いいえ |
| `remove_model_engine` | エンジンを削除する（モデルのファイルは残る。導入中は `conflict`） | なし | `removed` | いいえ（破壊的） |
| `set_model_engine_autostart` | mam の起動時に起動するか（冪等） | `on` | `auto_start` | いいえ |
| `cancel_model_install` | 動いている導入を中断する。ジョブは `failed`（理由「導入を中断しました…」）で終わり、取得済みの分は残って再開できる | `job_id` | `job_id`, `cancelled` | いいえ |

## GPU の今の状態（`get_gpu_status`、Issue #914）

ローカルの生成ジョブ（Wan 2.2・Flux・Qwen-Image など）を投げる前に、GPU がどれくらい空いているか・いま何が動いているかを確かめる入口。`detect_gpu`（どの GPU があり、どのビルドを入れるか）とは別で、こちらは**今の値**を返す。読み取り専用で、コマンドの実行と ComfyUI への `GET` だけを行い、何も変えない。

| 出力 | 内容 |
|------|------|
| `gpus[]` | `index`, `name`, `vendor`, `memory`（`dedicated` / `unified`）, `vram_total_bytes`, `vram_used_bytes`, `vram_free_bytes`, `utilization_percent`, `temperature_celsius`, `note`, `unavailable` |
| `processes[]` | GPU で計算しているプロセス: `pid`, `name`, `gpu_index`, `owner`（`mam` / `mam_engine`（同梱エンジンのフォルダの下のプログラム）/ `other`）, `used_bytes`, `used_bytes_unavailable` |
| `comfyui[]` | mam が知っている ComfyUI（同梱エンジンと外部の接続先）ごと: `endpoint`, `url`, `bundled`, `reachable`, `unreachable_reason`, `version`, `devices[]`（`vram_total_bytes`, `vram_free_bytes`, `torch_vram_*`, `comfyui_used_bytes` = ComfyUI が自分で持っている VRAM）, `running[]`・`pending[]`（ジョブ。`pending` は実行される順で `position` = 1 が次）, `unavailable` |
| `running[].progress` | `node`, `step`・`max_step`（いま動いているノードのステップ。Wan 2.2 のようにサンプラーが複数あるとノードごとに数え直す）, `percent`（ワークフロー全体）, `elapsed_seconds`（ComfyUI に渡してからの経過秒）, `eta_seconds`（そのノードの残りの目安。ジョブ全体の残りではない） |

- **取れない値はエラーにせず、省いて理由を付ける**: 項目名（JSON 名）をキーにした `unavailable`（GPU ごと・全体・ComfyUI ごと）と、`unreachable_reason`・`progress_unavailable`・`eta_unavailable`・`used_bytes_unavailable` に理由を日本語で返す。値を `null` や 0 で埋めない。
- **NVIDIA（Windows・Linux）**: `nvidia-smi --query-gpu=…`（名前・VRAM の合計/使用/空き・使用率・温度）と `--query-compute-apps=…`（プロセス）を CSV で読む。nvidia-smi が無い・失敗したときは、アダプターの一覧（Windows は DXGI）から名前と専用メモリの大きさだけ返し、他は理由付きで省く。AMD・Intel（と nvidia-smi が失敗した NVIDIA）の Windows での使用量は、下の「Windows の AMD・Intel」で取る。Windows の WDDM ドライバはプロセスごとのメモリを `[N/A]` で返すので、その理由を付ける（ComfyUI の使用量は `comfyui[].devices[].comfyui_used_bytes` で見る）。
- **Windows の AMD・Intel（Issue #966）**: nvidia-smi は NVIDIA 専用なので、Windows 標準のパフォーマンスカウンター（WDDM 2.x。Windows 10 1709 以降のドライバーが `GPU Adapter Memory` と `GPU Engine` を出す。タスクマネージャーの GPU の表示と同じ元）を読む。**管理者権限も追加のツールも要らない**。`typeperf` は使わない: カウンターのパスが Windows の表示言語で訳される（日本語環境では英語のパスが通らない）ため。同じカウンターを WMI のクラス `Win32_PerfFormattedData_GPUPerformanceCounters_GPUAdapterMemory`（`DedicatedUsage` = 専用メモリの使用バイト数）と `..._GPUEngine`（`UtilizationPercentage`）として公開する（クラス名・列名は言語によらない）ので、`powershell.exe -NoProfile -NonInteractive -Command "Get-CimInstance …| ConvertTo-Csv"` で読み、CSV を純関数で解析する（`modelmgr/gpu/status_wddm.go`）。アダプターは DXGI が返す LUID（インスタンス名 `luid_0x<上位>_0x<下位>_phys_N`）で対応づける。`vram_used_bytes` = `DedicatedUsage`、`vram_free_bytes` = DXGI の専用メモリ − 使用量（使用量が上回る統合 GPU は共有メモリを使っているので省き理由を付ける）、`utilization_percent` = 同じエンジン（`engtype_3D` など）のプロセスごとの値を足し、**最も忙しいエンジン 1 つの値**（100 を上限。タスクマネージャーと同じ見方）。温度は標準のカウンターに無いので省く。2 つのカウンターは別々のコマンドで、片方が読めなくても他方は返す。`source` は `windows-counters`、`note` が読み方を説明する。nvidia-smi が失敗した NVIDIA も同じ方法で使用量を返す。PowerShell が無い・クラスが無い（WDDM 2.0 未満）ときは、アダプターの名前と専用メモリだけを返し、理由を付ける。
- **Apple Silicon（macOS）**: 管理者権限が要らない `ioreg -r -d 1 -w 0 -c IOAccelerator`（`Device Utilization %`・`In use system memory`）・`sysctl`（チップ名・メモリ全体）・`vm_stat`（空き = free + inactive + speculative のページ）から読む。ユニファイドメモリなので `memory` は `unified`、`vram_total_bytes` はメモリ全体、`vram_free_bytes` はシステム全体の空き。温度とプロセスごとの使用量は権限なしでは取れないため、理由を付けて省く。Intel の Mac は GPU の状態を取らない（`unavailable.gpus`）。
- **ComfyUI**: `GET /system_stats`（VRAM。`comfyui_used_bytes` = `torch_vram_total` − `torch_vram_free`）と `GET /queue`（実行中・順番待ち）を、接続先ごとに 4 秒の期限で読む。応答しない接続先は `reachable=false` と理由で返す。
- **ジョブの進み具合**: ComfyUI はステップの進みを、ジョブを投入したクライアントの WebSocket にしか送らない。そのため `progress` は**mam が投入した**ジョブ（生成テスト・ショットの生成）だけに付く（`backend/internal/agent/modeladapter/comfyui/live` に記録）。ComfyUI の画面など別のクライアントが投入したジョブ・mam の再起動前に始まったジョブは、実行中として列挙するが `progress_unavailable` に理由を付ける。`eta_seconds` はそのノードのここまでの速さでの見積もり。
- コマンドは `gpu.Runner` を介して実行する（5 秒の期限・コンソールを出さない）。Linux のテストは fake で NVIDIA・Apple Silicon・Windows のカウンター（`modelmgr/gpu/testdata` の CSV）・取得不能を検証する。
- 同じ進みの記録（`live`）は `get_job`・`get_generation_test` の `progress` にも使う（[mcp-tools.md](mcp-tools.md)。Issue #966）。

## モデルのカタログ

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_model_catalog` | カタログのモデルと、それぞれの状態（`state` は必須のファイルが揃えば `installed`。無い任意のファイル〔Lightning LoRA など〕は `missing_optional_files[]`・`partial_note`、`files[].optional`。必須が揃うのに導入の記録が無いときは `files_in_place`。任意ファイルの取得中も、必須が揃っていれば `installed`（`install_job` が付く）。`install_blocked` のノードを要るモデルは `available:false` の `message` がその理由。Issue #1025・#1016）。各モデルの `license` に商用利用の可否（`commercial`: `yes` 商用OK / `conditional` 条件つき / `no` 商用NG / `unknown` 不明。ベースと派生で違うときは厳しい方）・条件・出典 URL・確認日（#928）。入力 `commercial`（`all` / `ok` / `ok_or_conditional`。省くと設定に従う）で絞れ、出力 `commercial_filter` が適用した絞り込み。**成人向け（`content_rating=adult`）のモデルは、年齢確認が済むまで出ない** | なし | `models[]`（`id`, `name`, `kind`, `tier`, `content_rating`（`general` / `adult`）, `trigger_words[]`（LoRA のトリガー語）, `total_size`, `need_bytes`, `vram`（`min_gb`, `source`, `estimate`）, `license`（`id`, `version`, `name`, `url`, `summary`, `commercial`（`yes` / `conditional` / `no` / `unknown`）, `commercial_condition`, `checked_at`, `components[]`（`part`, `name`, `url`, `commercial`）, `excluded_regions`, `notice`）, `consent_required`, `accepted`, `state`（`not_installed` / `installing` / `installed` / `incomplete`）, `install_job`, `model_ids[]`）, `blocked`（導入できない理由。空なら導入できる） | はい 出力の `vram_fit`（モデル ID → `fit`・`text`）は、この PC の GPU から見た VRAM の判定（`short` 足りない / `limited` 動くが制約あり（`text` に `vram.limits` の内容）/ `enough`。GPU のメモリが分からないときは省く。導入は止めない）。モデルの `vram` は `min_gb`・`recommended_gb`・`limits`・`source`・`estimate`（Issue #927） | **フィルター（Issue #1011。[model-filter.md](../architecture/model-manager/model-filter.md)）**: 入力はすべて省略可で、画面のフィルターパネルと同じ条件を**すべて満たす**ものだけが返る — `kinds[]`（`image` / `video` / `image_edit` / `upscale`）・`inputs[]`（`t2i`・`i2i`・`inpaint`・`edit`・`t2v`・`i2v`・`first_frame`・`last_frame`・`reference_whole`・`reference_face`・`reference_style`・`reference_pose`・`reference_control`。指定した**すべて**を持つ）・`adult`（`all` / `general_only` / `adult_only`）・`uncensored`（`all` / `censored_only` / `uncensored_only`）・`params[]`（`small` 5B 以下 / `medium` 5B より大きく 14B 未満 / `large` 14B 以上。数が分からないモデルは入らない）・`vram_fit`（VRAM 16GB で動く）・`commercial`（`all` / `ok` / `ok_or_conditional`、または `ok`・`conditional`・`unknown`・`ng` のカンマ区切り）・`installed`（`all` / `installed_only` / `not_installed_only`）・`origins[]`・`providers[]`・`audio`・`speeds[]`・`qualities[]`・`available_only`・`q`（名前・ID・タグ・トリガー語の検索）。言葉が不正なら `validation`（キー名つき）。`models[]` の各モデルに、構造化した `traits`（`params_b`・`params_label`・`uncensored`・`speed`・`quality`・`languages`。`uncensored` は成人向けとは別で、非検閲なら必ず成人向け）・`inputs[]`（ワークフローから導く）・`audio`（音声つき動画）・`available`（導入済みなら真）・`unavailable`・`unavailable_message`・`settings_route`（未導入は `not_installed` と `/model-manager`）が付く（Issue #1011・#1013）
| `get_catalog_model` | 1 件（`traits`・`inputs`・`audio`・`available` も含む。`files[]`・共有ファイル・ミラーの原本の場所 `source_note` を含む）。成人向けモデルは `adult_notice`（ライセンスの要約の後に見せる注意書き）も返し、年齢確認の前は `conflict` | `model_id` | `list_model_catalog` の要素 | はい |
| `accept_model_license` | ライセンスに同意したことを記録する（冪等）。ユーザーが確認した後にだけ呼ぶ。成人向けモデルは年齢確認の前は `conflict`。成人向けモデルの同意は、そのモデルだけに効く（年齢確認とは別） | `model_id`, `license_id`, `license_version` | `model_id`, `accepted` | いいえ |
| `install_model` | モデルを取得して導入する（ジョブ）。同意が要るモデルは記録が無ければ `conflict`。`requires_hf_token` のモデル（LTX-2.5）は、トークン未設定・無効・アクセス未承認なら、ジョブを作らず `validation` で案内（画面と同じ文言。model-manager.md §5.3） | `model_id`, `license_id`・`license_version`（同意をこの呼び出しで渡すとき） | `job_id`, `state`, `existing`, `target`（`model:<id>`） | いいえ |
| `delete_model` | 導入したモデルを削除する（使用中の用途・Policy があれば `validation`。共有ファイルは残る） | `model_id` | `model_id`, `deleted` | いいえ（破壊的） |
| `get_huggingface_token_status` | トークンの有無と末尾の数文字（値は返さない） | なし | `configured`, `last_chars` | はい |
| `set_huggingface_token` | トークンを保存・置き換える（冪等。応答に含まれない） | `token` | 同上 | いいえ |
| `clear_huggingface_token` | トークンを削除する（冪等） | なし | 同上 | いいえ（破壊的） |
| `get_external_inventory` | 外部の ComfyUI（種類 ComfyUI の登録済み接続先）が持つモデルファイル（`checkpoints`・`diffusion_models`・`text_encoders`・`vae`・`loras`）と、カタログのモデルに足りないファイル。読むだけ。成人向けのモデルは年齢確認が済むまで `models[]` に出ない | `endpoint` | `endpoint`, `version`, `folders`, `models[]`（`id`, `present`, `missing`, `complete`） | はい（外部へ接続する） |
| `get_commercial_only` | 設定「商用OKのモデルだけを使う」の状態（Issue #928）。オンの間、`list_generation_models`・`list_model_catalog` は `commercial` を省くと商用OK（`yes`）だけを返し、画面の一覧は「商用OKのみ」で始まる。**生成そのものは止めない**。意味は [model-license.md](../architecture/model-license.md) §5 | なし | `commercial_only` | はい |
| `set_commercial_only` | 同設定を切り替える（冪等）。ユーザーが求めたときだけ呼ぶ | `enabled` | `commercial_only` | いいえ |
| `get_adult_content_status` | 成人向けモデルの表示（年齢確認のゲート）の状態と、いまの注意書き | なし | `enabled`（ゲートが開いている）, `confirmed`（現行の注意書きの版の確認が記録済み）, `confirmed_at`, `notice_version`, `notice` | はい |
| `set_adult_content` | 成人向けモデルの表示を切り替える（冪等）。**オンにするのは、`notice` をユーザーに見せ、ユーザー自身が「18 歳以上（またはお住まいの地域で成年）」と注意書きへの同意を答えた後だけ**。エージェントが自分で確認したことにして呼ばない（`accept_model_license` と同じ扱い）。確認の日時と注意書きの版は `state.json` に記録される | `enabled`, `confirm_adult`（ユーザーが答えたとき true）, `notice_version`（`get_adult_content_status` の値）。一度確認すれば、オフ→オンでは `confirm_adult` を省ける | `get_adult_content_status` と同じ | いいえ |

### Hugging Face からの導入（Issue #842）

意味は [model-manager-hf.md](../architecture/model-manager/model-manager-hf.md)。**terms の確認・年齢ゲート・コミット固定・SHA-256 の検証は Service が強制する**（`install_hf_file` は `accept_terms` が無く記録も無ければ `conflict`。NFAA のリポジトリは年齢確認の前は `search_hf_models` に出ず、`get_hf_model_files`・`install_hf_file` は `conflict`）。`accept_terms` は、ユーザーが `get_hf_model_files` のライセンス・terms を見て自分で答えた後にだけ渡す（エージェントが代わりに true にしない）。GGUF は一覧に出るが `installable=false`（導入は `validation`）、pickle 形式は導入しない。

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `search_hf_models` | HF のモデルをキーワードで検索（ダウンロード数の多い順）。NFAA は年齢確認が済むまで出ない | `q`, `kind`（`text-to-image` / `text-to-video` / `image-to-video`。省略で全部）, `limit`（既定 20・最大 50） | `hits[]`（`repo`, `downloads`, `likes`, `gated`, `nfaa`, `pipeline_tag`） | はい（外部へ接続する） |
| `get_hf_model_files` | リポジトリ（`owner/repo` か `huggingface.co` の URL）をコミットに固定してファイルを一覧する | `repo` | `repo`（`id`, `commit`, `license`, `gated`, `nfaa`, `tags`）, `page`, `terms`, `terms_version`, `license_warning`, `consented`, `token_configured`, `selected`, `files[]`（`path`, `name`, `size`, `sha256`, `format`, `installable`, `reason`, `security`, `group`, `variant`, `folder`・`folder_source`・`folder_reason`・`family`, `fit`・`need_bytes`, `selected`, `installed`, `install_job`）, `truncated` | はい（外部へ接続する） |
| `install_hf_file` | `.safetensors` を 1 つ導入する（ジョブ）。SHA-256・サイズは Service が HF から読み直す | `repo`, `commit`, `path`（`get_hf_model_files` の値）, `folder`・`name`（省略でパス・ヘッダーから決める）, `accept_terms`・`terms_version` | `job_id`, `state`, `existing`, `target`（`hf:<repo>@<commit>/<path>`） | いいえ |
| `list_user_models` | 利用者が HF から足したファイル（NFAA は年齢確認が済むまで出ない） | なし | `models[]`（`path`, `name`, `folder`, `repo`, `commit`, `repo_path`, `sha256`, `size`, `license`, `gated`, `nfaa`, `family`, `workflows[]`, `model_ids[]`, `shared_with[]`, `present`, `installed_at`） | はい |
| `delete_user_model` | 足したファイルと、作ったモデルを削除する（使用中の用途・Policy があれば `validation`、導入中は `conflict`、記録に無ければ `conflict`）。カタログのモデルと共有しているファイルは、そのモデルが残る間は消えない | `path`（`list_user_models` の `path`） | `path`, `deleted` | いいえ（破壊的） |

## ローカルの実行環境（声・口パク。Issue #920）

意味は [local-runtime.md](../architecture/model-manager/local-runtime.md)（§12 が重みと依存ファイルの固定カタログ）。GPT-SoVITS・Irodori-TTS（声。Irodori-TTS のモデルの選択は local-runtime.md §14）と MuseTalk 1.5（口パク。環境の組み立て・起動・口パクの入口は local-runtime.md §13）と llama.cpp（ローカル LLM。`llama-cpp`。GGUF の 8 モデルの選択・非検閲は `uncensored`・商用不明の注記は local-runtime.md §17。非対応の PC の理由は macOS を含む。使う側は local-llm.md §10）を、mam が自動で導入・起動・停止する。MuseTalk の `support.fallback` は「代わりに、クラウドの口パク（Sync Labs）を使ってください。」、macOS の理由は「公式の PyTorch 版が使えない（CUDA 前提。mmcv 2.0.1 の Apple Silicon 向けの事前ビルドも無い）。第三者の MLX 版は顔の検出・貼り戻しを含むサーバーが無く、mam の自動導入の対象にしていない」（GPT-SoVITS は別の文: pyopenjtalk・jieba_fast・jieba が Apple Silicon 向けの部品に無い。local-runtime.md §16）、RTX 50 シリーズは `support.code=compute_cap`（PyTorch 2.0.1 が対応していない）。`install_local_runtime` の導入は、MuseTalk では部品 84 個の取得・展開（約 2.9 GB・展開後約 5.7 GB）と重み（約 6 GB）の取得までを 1 つのジョブで行う。画面（[endpoints-model-manager.md](endpoints-model-manager.md) §3「ローカル音声・口パク」）と同じ `localruntime.Service`（`service.Services.LocalRuntimes`）を呼ぶ。サービスが無いアプリでは `unavailable`。拒否は `mmview.RefuseRuntime` で画面と同じ文面になる（重みの不足・取得元が消えた・この PC で使えない（理由とクラウドの案内）・導入中・未導入は `conflict`、未知の ID は `not_found`）。**自動導入は Windows ＋ NVIDIA だけ**（同梱の ComfyUI と同じ）。使えない PC では導入・起動が `conflict` で、`support.reason`・`support.fallback`（ElevenLabs / Sync Labs を選ぶ案内）が分かる。クラウドへの切り替えは利用者が設定で選ぶもので、このツールでは黙って切り替えない。

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_local_runtimes` | 実行環境の一覧。各環境の状態（導入済みの版・起動中か停止か・自動起動）、この PC で使えるか（`support`: `supported`・`reason`・`fallback`）、`weights`（下） | なし | `runtimes[]`（`id`, `name`, `purpose`（`tts` / `lip_sync` / `llm`）, `support`, `pinned_version`, `installed`, `broken`, `update_available`, `process`, `auto_start`, `dir`, `writable`, `free_bytes`, `install_job`, `license`, `vram`, `weights`） | はい |
| `get_local_runtime_status` | 1 つの環境の状態（`list_local_runtimes` の 1 件と同じ） | `runtime_id` | 同上 | はい |
| `install_local_runtime` | 環境（Python・本体・重み）を取得して導入する（ジョブ）。SHA-256 を検証し、中断しても再開する | `runtime_id` | `job_id`, `state`, `existing`, `target`（`runtime:<id>`） | いいえ（外部へ接続） |
| `install_local_runtime_weights` | 導入済みの環境の重み・依存ファイルを固定の SHA-256 と照合し、足りない・壊れたファイルだけを取り直す（ジョブ）。未導入は `conflict` | `runtime_id` | 同上 | いいえ（外部へ接続） |
| `start_local_runtime` | サーバー（127.0.0.1 だけ）を起動し、応答するまで待つ（冪等）。**必須の重みが足りなければ、起動せず `conflict`**（足りない名前と「`install_local_runtime_weights` を実行する」案内） | `runtime_id` | `state`, `port`, `url`, `pid`, `error` | いいえ |
| `stop_local_runtime` | 停止する（冪等） | `runtime_id` | 同上 | いいえ |
| `remove_local_runtime` | 環境と取得した重みを削除する（設定は残る。導入中は `conflict`） | `runtime_id` | `removed` | いいえ（破壊的） |
| `set_local_runtime_autostart` | mam の起動時に起動するか（冪等） | `runtime_id`, `on` | `auto_start` | いいえ |
| `list_local_runtime_models` | 複数のモデルを持つ実行環境（`irodori-tts`）のモデルの一覧。モデルごとの `id`・`name`・`description`・`default`・`active`（使用中）・`kind`・`languages`・`size_bytes`（取得量）・`license`（`license_info`）・`cautions`（使うときの制限）・`vram_min_gb`/`vram_recommended_gb`/`vram_limits`/`vram_note`/`vram_estimate`・`vram_fit`（この PC の GPU との比較）・`consent{version,text}`（同意が要るモデル）・`consent_needed`/`accepted`。モデルの無い環境は空。知らない `runtime_id` は `not_found`。**`runtime_id` を省くと、モデルを持つすべての実行環境のモデルを返す**（各モデルに `runtime_id`）。`Condition` の引数（`kinds`〔`audio`＝声・`llm`〕・`languages`〔`ja`・`en`〕・`uncensored`・`vram_fit`・`commercial`・`speeds`・`qualities`・`q`。[model-filter.md §2](../architecture/model-manager/model-filter.md)）で絞れる（日本語の声は `kinds=["audio"]`・`languages=["ja"]`）。`commercial` を省くと設定「商用OKのモデルだけを使う」に従い、適用した値は `commercial_filter`。不正な語は `validation`（Issue #1015・#1011） | `runtime_id?`, `kinds?`, `languages?`, `uncensored?`, `vram_fit?`, `commercial?`, `q?` ほか | `runtime_id?`, `models[]`, `commercial_filter` | はい |
| `select_local_runtime_model` | 使うモデルを選ぶ。サーバーが動いていれば止める（次の起動から新しいモデル。重みの取得は `install_local_runtime_weights`）。**同意が要るモデル（Irodori-TTS の v4-Large 系。Gemma Terms of Use）は、利用者がテキスト（`consent.text`）を読んで同意したときだけ、その `consent.version` を `accept_license_version` に渡す。エージェントが代わりに渡してはならない**。渡さない・版が違うときは `conflict`（何も記録しない）、知らないモデルは `validation`、導入の最中は `conflict` | `runtime_id`, `model_id`, `accept_license_version?` | `runtime_id`, `models[]`（`list_local_runtime_models` と同じ。選んだあとの状態） | いいえ |
| `list_ollama_models` | 管理している Ollama（`runtime_id=ollama`。[local-runtime.md §19](../architecture/model-manager/local-runtime.md)。#1105。状態: 仕様のみ・未実装）のモデルの一覧。カタログの 3 つ（`gemma4:12b`・`qwen3.8:27b`・`deepseek-r1:14b`）ごとに `name`・`display_name`・`state`（`missing` / `pulling` / `installed` / `changed` / `unavailable`）・`size_bytes`（取得量）・`license`（`license_info`。商用の答え）・`verified`（取得後の照合の結果）・`pull_job`、と `others[]`（カタログに無い取得済みのモデルの名前とサイズ。取得・削除はできない）。Ollama が未導入・停止中でも返す（`state` が `unavailable`、`reason`） | なし | `ollama{installed, running, reason}`, `models[]`, `others[]` | はい |
| `pull_ollama_model` | Ollama のモデルを取得する（ジョブ。`ollama pull` 相当。管理している Ollama の `/api/pull`）。カタログに無い名前・`library` 以外の名前空間・`cloud` を含むタグは `validation`、Ollama が未導入・非対応は `conflict`、空き不足は `conflict`（必要量・空き・場所）。取得中の同じ名前は同じジョブ（`existing`）。止まっている Ollama は起動する | `name` | `job_id`, `state`, `existing`, `target`（`ollama:<name>`） | いいえ（外部へ接続） |
| `delete_ollama_model` | 取得したモデルを削除する。LLM の割り当てが指していれば `validation`、取得中・未取得は `conflict` | `name` | `name`, `deleted` | いいえ（破壊的） |

Ollama 本体の導入・起動・停止・削除は、上の `install_local_runtime`・`start_local_runtime`・`stop_local_runtime`・`remove_local_runtime`（`runtime_id=ollama`）をそのまま使う（`list_local_runtimes` の `purpose=llm` に `ollama` が載る）。取得のジョブは `get_job`（`target` = `ollama:<name>`、`done` / `total` は進捗の割合）で見て、中断は `cancel_model_install`（`ollama:<name>` のジョブも中断する）。**管理している Ollama には認証が無い**（[local-runtime.md §19.4](../architecture/model-manager/local-runtime.md)）。`list_ollama_models` の応答に `noauth: true` と注意の文を入れる。

`weights` は、`state`（`none`: 未導入 / `ready` / `missing`: **導入済みだが重みが不足** / `unverified`: 確認が済んでいない）・`message`・`need_bytes`・`total_bytes`・`unclear`（商用利用が `yes` でない重みの数）と、`assets[]`: `name`, `description`, `state`（`ok` / `missing` / `unverified`）, `required`, `size`, `source`（`name`, `url`, `source_note`）, `license`（`id`, `name`, `url`, `summary`, **`commercial`（`yes` / `conditional` / `no` / `unknown`）**, `notice`）。**`commercial` が `yes` でない重みは、ユーザーに `notice` を見せてから使う**（法的な助言ではなく、確認できた範囲の記載）。`cancel_model_install` は `runtime:<id>` のジョブも中断する。

## 追加ノード（ComfyUI のカスタムノードの許可リスト。Issue #976）

意味・許可リスト・導入の流れは [custom-nodes.md](../architecture/model-manager/custom-nodes.md)。実装は `backend/internal/mcp/tools/modelmgr/nodetools`（画面と同じ `modelnodes.Service`＝`service.Services.ModelNodes`。アプリが持たないときは全ツールが `unavailable`）。**許可リストにないノードは導入できず、読み込まれない**。ノードは ComfyUI の中でそのまま実行されるプログラムなので、同意（ライセンスと注意事項）はユーザーが内容を確認してからにする。MCP のエージェントが同意を記録できる（`accept_custom_node_license`。`accept_model_license` と同じ扱いで、ユーザーが確認した後にだけ呼ぶ）。`install_custom_node` は、`accept_custom_node_license` で同意が記録されていなければ `validation` で拒否し、何も取得しない。拒否の文面は画面と同じ（`mmview.Refuse`）。導入は `get_job`（`target` = `node:<id>`）、中断は `cancel_model_install`。`list_model_catalog`・`get_catalog_model` のモデルには、必要なノードの id が `requires_nodes` で付く（ノードが無い間、`install_model` は `validation`「追加ノード「…」が導入されていません…」で拒否される）。

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_custom_nodes` | 許可リストの全ノードと状態 | なし | `nodes[]`（`id`, `name`, `description`, `repo`, `commit`, `archive_sha256`, `archive_size`, `extracted_size`, `folder`（`custom_nodes` の下のフォルダ名）, `license`（`name`, `url`, `summary`, `commercial`: yes / conditional / no / unknown, `commercial_condition`, `notice`）, `python_requires`, `python_optional`, `install_blocked`, `node_classes`, `limits`, `reviewed_at`, `review`, `state`: not_installed / installed / outdated / modified / blocked, `detail`, `installed_commit`, `accepted`, `security_notice`, `notice_version`, `install_job`, `models[]`, `unavailable`, `need_bytes`） | はい |
| `get_custom_node_status` | 1 つのノードの状態（`list_custom_nodes` の 1 件と同じ） | `node_id` | 同上 | はい |
| `accept_custom_node_license` | ライセンスと注意事項に同意したことを記録する（ユーザーが確認した後にだけ呼ぶ。版が違えば `validation`） | `node_id`, `license_id`, `license_version`, `notice_version`（`get_custom_node_status` の値） | `node_id`, `accepted` | いいえ（冪等） |
| `install_custom_node` | 固定のコミットのアーカイブを取得して導入する（ジョブ。SHA-256 を検証。動いている ComfyUI は再起動し、読み込まれたことを確かめる）。同意が無い・許可リストにない（`not_found`）・`install_blocked`・同梱エンジンが無い（`conflict`）・空き容量不足は始めずに拒否 | `node_id` | `job_id`, `state`, `existing`, `target`（`node:<id>`） | いいえ |
| `remove_custom_node` | 導入したノードを削除する（動いている ComfyUI は再起動）。導入中・未導入は `conflict` | `node_id` | `node_id`, `removed` | いいえ（冪等・破壊的） |

## 試し生成（廃止。Issue #1100）

モデルマネージャーの「試し生成」の区画は #1100 で廃止する（実装済み）。もともと専用のツールは無く（1.1 で削除）、ローカルのモデルを試すには `list_generation_models`・`estimate_generation_test`・`run_generation_test`（参照画像は用途 `gentest_reference`、結果のファイルは `get_generation_result` の URL）を使う。これは今までどおりで、**このファイルのツールは増減しない**。**導入済みの成人向けモデルは、年齢確認のゲートが閉じている間は `list_generation_models` に出ず、名指しても不明なモデルとして断られる**（Service が強制する。[model-manager.md](../architecture/model-manager/model-manager.md) §6.2）。1.0 の `list_trial_models`・`start_model_trial`・`list_model_trials`・`get_model_trial`・`get_model_trial_file_url`・`delete_model_trial`、アップロードの用途 `model_trial_reference`、ダウンロードの種類 `model_trial_file` は 1.1 で削除済み。

## ジョブ（`get_job`）

| 種別（`kind`） | 開始するツール | `target` | `done` / `total` | `result` |
|---------------|---------------|----------|------------------|----------|
| `model_install` | `install_model_engine`・`install_model`・`install_hf_file`・`install_local_runtime`・`install_local_runtime_weights`・`install_custom_node`・`pull_ollama_model`（#1105） | `engine` / `model:<id>` / `hf:<repo>@<commit>/<path>` / `runtime:<id>` / `node:<id>` / `ollama:<name>`（#1105。例 `ollama:gemma4:12b`） | 進捗の割合 / 100 | `target`, `phase`（download / extract / register / done など。Ollama は pulling / verifying / done）, `done_bytes`, `total_bytes` |

`state` は `queued` / `running` / `succeeded` / `failed`。失敗の `error` はジョブキューの行の理由（SHA-256 の不一致・空き容量・取得元が認証を求めた・**取得元が消えた（404・410。ミラーの削除など。ファイル名と、ミラーなら原本の場所を添える）**・中断・ComfyUI の失敗など）。導入は再開できる（再起動のたびに最大 3 回）。進捗の割合はメモリ上で、ジョブの終了直前や再起動の後は 0 に戻ることがある（`succeeded` では 100）。

## テスト

`mcptest` で、偽の Hugging Face・偽のエンジンのプロセス・偽の ComfyUI（`mmtest`・`comfyuitest`）を使う。エンジンの導入から `get_job`、起動・停止・削除、GPU が無いとき・Windows 以外・固定された設定ファイル・空き容量不足の拒否、同意の強制（記録が無ければダウンロードしない・別のライセンスや版は拒否・同意後に導入）、中断と再導入、共有ファイルを残す削除、トークンが結果に出ないこと、`get_job` が別の入口（画面）で始めたジョブを読めること。年齢確認（`adult_test.go`）: 確認が無い間は一覧に出ず・読めず・同意も導入もできない（ダウンロードしない）こと、`confirm_adult`・`notice_version` が無い・違う呼び出しは拒否されること、確認の後に一覧・導入ができライセンスの同意は別に要ること、オフで再び隠れ確認の記録は残ること。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成。エンジン 9 ツール・カタログ 9 ツール・試し生成 6 ツール、ジョブの種別 `model_install`・`gentest`、アップロードの用途 `model_trial_reference`、ダウンロードの種類 `model_trial_file` | Issue #833（#830 の方針: 画面の全操作を MCP で操作できるようにする） |
| 1.1 | 2026-10-26 | 試し生成の 6 ツール・アップロードの用途 `model_trial_reference`・ダウンロードの種類 `model_trial_file` を削除し、生成テストのツール（[mcp-tools-generation-test.md](mcp-tools-generation-test.md)）に統合した。ジョブの種別 `gentest` の Probe も生成テスト側に移った | Issue #835。生成テストの実装を 1 つにする（試し生成は 1 モデル 1 プロンプトのバッチ） |
| 1.2 | 2026-10-25 | 成人向けモデルの年齢確認のツール `get_adult_content_status`・`set_adult_content` を足した。`list_model_catalog`・`get_catalog_model` に `content_rating`・`trigger_words`（と `files[].source_note`・`adult_notice`）を足し、ゲートが閉じている間は成人向けモデルを一覧に出さず、`get_catalog_model`・`accept_model_license`・`install_model` は `conflict` にした。取得元が消えた導入の失敗の理由を足した | Issue #841。成人向け（非検閲）のモデル・LoRA と年齢確認 |
| 1.3 | 2026-10-25 | Hugging Face の任意の `.safetensors` の導入のツール `search_hf_models`・`get_hf_model_files`・`install_hf_file`・`list_user_models`・`delete_user_model` を足した。ジョブの `target` に `hf:<repo>@<commit>/<path>` を足した | Issue #842。意味は [../architecture/model-manager/model-manager-hf.md](../architecture/model-manager/model-manager-hf.md) |
| 1.4 | 2026-10-25 | `get_external_inventory` が成人向けのモデルを年齢確認が済むまで返さず、`loras` フォルダも読むようにした。`get_hf_model_files`・`install_hf_file` の NFAA 判定を、コミットのタグと今の先頭のタグの OR にした（古いコミットを指名しても年齢確認が要る）。同じファイルを別のフォルダ・名前で導入中に頼むと `validation`。同じ宛先の別ファイルは上書きされない（`validation`）。`list_user_models` の `shared_with` は年齢確認が済むまで成人向けのモデルを挙げない | PR #849 のレビューの指摘（#841・#842）への修正 |
| 1.5 | 2026-10-29 | `install_model` が `requires_hf_token` のモデルでトークンとアクセスを確かめ、足りなければ `validation` で案内することを足した。`set_huggingface_token` の説明を、LTX-2.5 に使うものに改めた | Issue #877 |
| 1.6 | 2026-11-01 | 「ローカルの実行環境」のツール 8 個（`list_local_runtimes`・`get_local_runtime_status`・`install_local_runtime`・`install_local_runtime_weights`・`start_local_runtime`・`stop_local_runtime`・`remove_local_runtime`・`set_local_runtime_autostart`）を足した。重みごとの出典・ライセンス・`commercial_use`（`unclear` を含む）・「導入済み（重みが不足）」を返し、必須の重みが足りない起動は `conflict`。`cancel_model_install` が `runtime:<id>` のジョブも中断し、`get_job` の `model_install` の `target` に `runtime:<id>` を足した | Issue #920。重み・依存ファイルの出典とライセンスを MCP でも見られるように |
| 1.7 | 2026-11-01 | ローカルの実行環境のツールが MuseTalk 1.5 を扱うことを書いた（`support.fallback` が Sync Labs、macOS の理由、RTX 50 の `compute_cap`、導入のジョブの内容。意味は [local-runtime.md §13](../architecture/model-manager/local-runtime.md)）。ツールの名前・引数・応答の形は変えていない | Issue #921（MuseTalk のランタイム） |
| 1.8 | 2026-11-02 | `get_gpu_status`（読み取り専用）を足した。GPU ごとの VRAM・使用率・温度、ComfyUI ごとの VRAM・実行中のジョブの進み具合・順番待ちの順番を返し、取れない値は理由付きで省く | Issue #914 |
| 1.9 | 2026-11-02 | `get_commercial_only`・`set_commercial_only` を足した。`list_model_catalog` に入力 `commercial`・出力 `commercial_filter`、`license` の `commercial_use`（真偽値）を `commercial`（4 値）・`checked_at`・`components[]` に替えた。ローカル実行環境の重みの `commercial_use` / `unclear` も `commercial` / `unknown` に | Issue #928（商用利用の可否の表示と絞り込み） |
| 1.10 | 2026-11-02 | `list_model_catalog` の出力に `vram_fit`、モデルの `vram` に `recommended_gb`・`limits` を足した | Issue #927（LTX-2.5。16 GB は動くが制約あり）|
| 1.11 | 2026-11-02 | `get_gpu_status` が Windows の AMD・Intel（と nvidia-smi が失敗した NVIDIA）の VRAM の使用量・使用率を、Windows のパフォーマンスカウンター（WMI の `Win32_PerfFormattedData_GPUPerformanceCounters_*`。管理者権限不要）から返すことを足した。`source` に `windows-counters`。進みの記録は `get_job`・`get_generation_test` の `progress` にも使う | Issue #966（#914 の続き） |
| 1.12 | 2026-11-03 | 追加ノードのツール 5 つ（`list_custom_nodes`・`get_custom_node_status`・`accept_custom_node_license`・`install_custom_node`・`remove_custom_node`）を足した。`get_job` の `target` に `node:<id>`、モデルに `requires_nodes` | Issue #976 |
| 1.13 | 2026-11-03 | `list_model_catalog` に、画面のフィルターと同じ条件の入力（`kinds`・`inputs`・`adult`・`uncensored`・`params`・`vram_fit`・`commercial`（4 語のカンマ区切りも）・`installed`・`origins`・`providers`・`audio`・`speeds`・`qualities`・`available_only`・`q`）を足した（既存の `commercial` は互換）。`list_model_catalog`・`get_catalog_model` のモデルに `traits`・`inputs`・`audio`・`available`・`unavailable`・`unavailable_message`・`settings_route` を足した | Issue #1011・#1013 |
| 1.14 | 2026-11-03 | ローカルの実行環境のツールが Irodori-TTS（`irodori-tts`）も扱うこと、モデルを選ぶ 2 ツール `list_local_runtime_models`・`select_local_runtime_model`（同意が要るモデルは `accept_license_version`）を足した | Issue #1015。意味は [../architecture/model-manager/local-runtime.md §14](../architecture/model-manager/local-runtime.md) |
| 1.15 | 2026-11-04 | `list_local_runtimes` の `purpose` に `llm`（llama.cpp。[local-runtime.md §17](../architecture/model-manager/local-runtime.md)）を足した。`list_local_runtime_models` のモデルは `kind: llm`・`uncensored`（非検閲）・`values` を持ち、`select_local_runtime_model` で選ぶ。LLM としての使い方は `set_local_llm_server` の `use_managed_runtime`（[mcp-tools-settings-policy.md](mcp-tools-settings-policy.md)） | Issue #1008（後半） |
| 1.16 | 2026-11-04 | `list_model_catalog`・`get_catalog_model` の各モデルに `missing_optional_files[]`・`partial_note`・`files_in_place`、`files[].optional` を足した（必須のファイルが揃えば `installed`。生成テストの `available` と同じ答え） | Issue #1025 |
| 1.17 | 2026-11-04 | `list_model_catalog`・`get_catalog_model`: 任意ファイルの取得中も、必須が揃うモデルは `state: installed`（`install_job` が付く）。`install_blocked` のノードを要るモデルの `available:false` の `message` はその理由（行き止まりの案内にしない）。導入は任意ファイルの取得に失敗しても失敗にしない | Issue #1025・#1016 |
| 1.18 | 2026-11-06 | `list_local_runtime_models` の `runtime_id` を省略可にし（省略するとモデルを持つすべての実行環境のモデル。各モデルに `runtime_id`）、フィルターの条件（`kinds`・`languages`・`uncensored`・`vram_fit`・`commercial`・`q` ほか）で絞れるようにした。日本語の声は `kinds=["audio"]`・`languages=["ja"]` | Issue #1015・#1011 |
| 1.19 | 2026-11-06 | 「試し生成」の節を、区画の廃止（#1100）に合わせて書き直した（ツールの増減なし。状態は仕様のみ） | Issue #1100 |
| 1.20 | 2026-11-06 | `list_ollama_models`・`pull_ollama_model`・`delete_ollama_model` を足し、ジョブの `target` に `ollama:<name>` を足した。Ollama 本体は既存の `*_local_runtime` で扱う（`runtime_id=ollama`）。状態は仕様のみ（未実装） | Issue #1105 |
| 1.21 | 2026-11-07 | 「試し生成」の節の状態を「実装済み」にした（#1100。ツールの増減なし） | Issue #1100 |
