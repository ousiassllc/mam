package apperr

// messagesJa are the Japanese texts, the base of the catalog. A code has a cause ("<code>.cause": why it failed) and a remedy
// ("<code>.remedy": what to do); each ends with its full stop, so that View.Sentence can join them. {name} is an argument
// (Error.Args); an argument that is not given reads as nothing.
var messagesJa = map[string]string{ //nolint:gosec // texts of errors (they mention "API キー" and credits, but hold no credential)
	"unknown.cause":  "原因がわかりませんでした。想定していない失敗です。",
	"unknown.remedy": "もう一度試してください。直らないときは、詳細（とエラー ID）を添えて報告してください。mam のログにも同じ ID で残しています。",

	"key_missing.cause":  "{service}API キーが設定されていません。",
	"key_missing.remedy": "設定画面の「API キーとログイン」で、使うサービスの API キーを登録してください。",

	"llm_key_missing.cause":  "LLM の API キーが設定されていません。{route}",
	"llm_key_missing.remedy": "設定画面の「API キーとログイン」で {who}API キーを登録してください。{{advice.chatgpt}}{restart}",

	"auth_failed.cause":  "接続先が認証を拒否しました{http}。API キーが違う・期限切れ、またはこの操作の権限がありません。",
	"auth_failed.remedy": "設定画面の「API キーとログイン」でキーを確かめて入れ直してください。自分で登録した接続先（ローカルの ComfyUI など）なら、その接続先の認証の設定も確かめてください。",

	"remote_not_found.cause":  "接続先に、呼び出した URL かモデルがありません（HTTP 404）。",
	"remote_not_found.remedy": "接続先の URL とモデル名が合っているか、モデルが導入済みかを確かめてください。同梱の ComfyUI は、モデル管理で導入の状況を確かめられます。",

	"timeout.cause":  "相手の応答が時間内に返りませんでした。",
	"timeout.remedy": "しばらく待ってからもう一度試してください。クラウドのモデルは、相手側では生成が続き、課金される場合があります。",

	"stalled.cause":  "ローカルの生成が{after}のあいだ進まなかったため打ち切りました。",
	"stalled.remedy": "ComfyUI が止まっていないか確かめてください。重いモデルで進みの出ない処理が続く場合は、設定画面の接続先でタイムアウト（進みが止まってから打ち切るまでの時間）を延ばしてください。",

	"time_limit.cause":  "ローカルの生成が全体の上限（{after}）を超えたため打ち切りました。",
	"time_limit.remedy": "解像度や秒数を小さくして実行し直してください。",

	"endpoint_timeout.cause":  "ローカルの生成が接続先の待ち時間（{after}）を超えたため打ち切りました。",
	"endpoint_timeout.remedy": "設定画面の接続先でタイムアウトを延ばすか、解像度や秒数を小さくして実行し直してください。",

	"request_rejected.cause":  "相手がリクエストを拒否しました{http}。コンテンツポリシーに当たったか、入力や設定がそのモデルに合っていません。",
	"request_rejected.remedy": "詳細に、相手の返した理由があります。プロンプトの長さや内容、解像度・秒数・入力画像などの設定を見直してください。",

	"input_missing.cause":  "このモデルが必ず使う入力（{param}）が渡されていません。",
	"input_missing.remedy": "{param} に使う画像を添えるか、添えた画像の役割を変えてから実行してください。画像から動画を作るモデルは、最初のフレーム（first_frame）の画像が要ります。添えた画像が「参照」になっているときは、「最初のフレーム」に変えてください。",

	"input_unused.cause":  "添えた入力画像は、選んだモデルでは使われません。",
	"input_unused.remedy": "画像から作るなら、I2V（画像から動画）や参照画像に対応したモデルを選んでください。画像を使わずに作るなら、画像を外してから実行してください。",

	"input_unsupported.cause":  "このモデルは、渡された入力画像や解像度の組み合わせを受け付けません。",
	"input_unsupported.remedy": "入力画像の使い方（最初のフレーム・最後のフレーム・参照画像）や解像度を、そのモデルが対応するものに変えてください。モデルの対応はモデル一覧で確かめられます。",

	"rate_limited.cause":  "相手のサービスの利用回数の上限（レート制限、HTTP 429）に達しました。",
	"rate_limited.remedy": "しばらく待ってからもう一度実行してください。続くときは、同時に走らせる数を減らすか、相手のサービスのプランの上限を確かめてください。",

	"server_error.cause":  "相手のサービス側で障害が起きています{http}。",
	"server_error.remedy": "しばらく待ってからもう一度試してください。続くときは、相手のサービスの障害情報を確かめてください。ローカルの ComfyUI なら、ComfyUI のログを確かめてください。",

	"connection_failed.cause":  "接続先につながりませんでした。",
	"connection_failed.remedy": "接続先が起動しているか、URL とネットワーク（プロキシ・VPN・ファイアウォール）を確かめてください。同梱の ComfyUI は、モデル管理でエンジンが動いているかを確かめてください。",

	"circuit_open.cause":  "このモデルのベンダーで失敗が続いているため、呼び出しを止めています（一時的に利用できません）。",
	"circuit_open.remedy": "しばらく待ってから実行し直してください。",

	"vram_oom.cause":  "GPU のメモリ（VRAM）が足りませんでした。",
	"vram_oom.remedy": "解像度や秒数を小さくする、もっと軽いモデルや速度モード（lightning など）に変える、ほかに GPU を使うアプリを閉じる、のどれかをしてから実行し直してください。",

	"disk_full.cause":  "ディスクの空き容量が足りません{where}。",
	"disk_full.remedy": "使わないモデルや古い生成結果を消して空きを作ってから、もう一度実行してください。",

	"model_not_installed.cause":  "必要なモデルのファイルが見つかりません。",
	"model_not_installed.remedy": "モデル管理で、そのモデルを導入してください。導入済みなのに出るときは、モデルを入れ直してください。",

	"license_not_accepted.cause":  "このモデルのライセンスに、まだ同意していません。",
	"license_not_accepted.remedy": "モデル管理でライセンスを読み、同意してから導入してください。",

	"engine_not_installed.cause":  "ComfyUI エンジンが導入されていません。",
	"engine_not_installed.remedy": "モデル管理で、先に ComfyUI エンジンを導入してください。外部の ComfyUI を使うなら、設定の「ローカル・代替モデル」に URL を登録してください。",

	"node_missing.cause":  "ワークフローが使うカスタムノードが、ComfyUI にありません。",
	"node_missing.remedy": "モデル管理で必要なカスタムノードを導入し、ComfyUI を再起動してから実行し直してください。",

	"face_models_missing.cause":  "顔の類似度を測る顔モデルがありません（{files}）。顔の類似度（SFace）はオフです。",
	"face_models_missing.remedy": "モデルマネージャー（/model-manager）の「顔チェック用モデル」から、ボタン 1 つで {dir} に導入できます（YuNet は MIT・SFace は Apache-2.0。取得元は opencv/opencv_zoo の固定したコミットで、SHA-256 を確かめます。導入したあと mam を再起動する必要はなく、次の生成テストから測れます）。手で置くときは {dir} に置いてください（リポジトリの scripts/fetch-face-models.sh {dir} が、同じ固定したコミットから取得して置きます）。置き場所を変えるときは起動オプション --face-model-dir を使います。顔の類似度を測るには、このモデルのほかに ONNX Runtime のライブラリも要ります（Windows のインストーラー版は mam.exe と同じフォルダに入れます）。",

	"face_runtime_missing.cause":  "顔の類似度を測る ONNX Runtime のライブラリを読み込めません。顔の類似度（SFace）はオフです。",
	"face_runtime_missing.remedy": "顔モデル（YuNet・SFace）は置いてあります。モデルマネージャーの「顔チェック用モデル」から導入し直しても直りません（直すのは ONNX Runtime のライブラリです）。ONNX Runtime 1.23.x のライブラリ（Windows は mam.exe と同じフォルダの onnxruntime.dll。インストーラー版は置いてあります。ポータブル版は含まないので、https://github.com/microsoft/onnxruntime/releases の v1.23.2 の onnxruntime-win-x64-1.23.2.zip を開き、lib フォルダの onnxruntime.dll を mam.exe と同じフォルダへコピーしてください。コマンドは要りません。ほかの OS は libonnxruntime）を用意してください。場所は起動オプション --onnxruntime-lib で指定できます。Windows で DLL があるのに読めないときは、Microsoft Visual C++ 2015-2022 Redistributable (x64) が必要です。置いたあとは、次の生成テストから測れます（再起動は要りません）。詳細に顔モデルのファイルの読み込みの失敗が出ているときは、モデルマネージャーの「顔チェック用モデル」で削除してから入れ直してください。",

	"llm_invalid_output.cause":  "LLM の答えが、求めた形式になっていませんでした。",
	"llm_invalid_output.remedy": "もう一度実行してください。続くときは、設定画面の「テキスト生成（LLM）」で、その Agent のモデルを変えてみてください。",

	"local_llm_unreachable.cause":  "ローカルサーバーに接続できません。ローカルの LLM サーバー（Ollama・LM Studio・llama.cpp server など）が応答しませんでした。",
	"local_llm_unreachable.remedy": "サーバーが起動しているか、設定画面の「テキスト生成（LLM）」の「ローカル LLM サーバー」のベース URL（Ollama は http://localhost:11434、LM Studio は http://localhost:1234）が合っているかを確かめてください。LM Studio はローカルサーバーを起動（Start Server）する必要があります。",

	"generation_time_limit.cause":  "生成が制限時間（Max generation time）を超えました。",
	"generation_time_limit.remedy": "設定画面で制限時間を延ばすか、ポリシーを見直してください。",

	"media_timeout.cause":  "動画・音声の処理（ffmpeg）が制限時間を超えました。",
	"media_timeout.remedy": "素材ファイルが壊れていないか、大きすぎないかを確かめて、もう一度実行してください。",

	"download_corrupt.cause":  "ダウンロードしたファイルが、固定した SHA-256 または大きさと一致しませんでした。破棄しました（取得元の改ざんか、転送の破損の可能性があります）。",
	"download_corrupt.remedy": "もう一度導入してください。続くときは、mam を更新してから試してください。",

	"install_failed.cause":  "導入を完了できませんでした。",
	"install_failed.remedy": "原因は詳細にあります。モデル管理の画面から、もう一度導入してください。",

	"training_failed.cause":  "声の学習を完了できませんでした。",
	"training_failed.remedy": "ほかに GPU を使うアプリ（同梱の ComfyUI の生成など）を閉じて、もう一度学習してください。続くときは、詳細（学習のログの末尾）を確かめるか、モデル管理で GPT-SoVITS の状態を確かめてください。途中までの結果は残していません。",

	"not_writable.cause":  "保存先のフォルダに書き込めません。",
	"not_writable.remedy": "書き込める場所に移すか、フォルダの権限を確かめてください。外部の ComfyUI の URL を使う方法もあります。",

	"no_gpu.cause":  "対応する GPU が見つかりません。",
	"no_gpu.remedy": "外部の ComfyUI の URL を設定するか、CPU 実行を明示的に選んでください。",

	"image_unavailable.cause":  "ChatGPT サブスクリプションでの画像生成に使える Codex のモデルが見つかりませんでした（OpenAI が ChatGPT ログインで使えるモデルを入れ替えた可能性があります）。",
	"image_unavailable.remedy": "mam を最新版に更新して、もう一度実行してください。直らない場合は、設定で ChatGPT 画像生成を OFF にして、API キーで使える画像モデルに切り替えてください。",

	"save_failed.cause":  "設定やデータを保存できませんでした{why}。",
	"save_failed.remedy": "ディスクの空き容量とフォルダの権限を確かめてから、もう一度保存してください。続くときは mam のログを確認してください。",

	"ffmpeg_missing.cause":  "ffmpeg と ffprobe が見つかりません。",
	"ffmpeg_missing.remedy": "設定画面の「ffmpeg」で、ffmpeg と ffprobe をダウンロードするか、自分で入れたもののパスを指定してください。",

	"media_failed.cause":  "{what}に失敗しました。動画・音声の処理（ffmpeg）がエラーで終わりました。",
	"media_failed.remedy": "素材の動画が壊れていないか、保存先の空き容量と書き込み権限を確かめてから、もう一度実行してください。続くときは、詳細（ffmpeg の出力の末尾）を添えて報告してください。",

	"facefusion_unavailable.cause":  "FaceFusion を使えません。見つからないか、設定したパスが使えません。",
	"facefusion_unavailable.remedy": "FaceFusion は mam に同梱していないので、自分で入れてください。入れてあるときは、設定画面の「FaceFusion」で、Python と FaceFusion のフォルダのパスを確かめてください。詳細に、見つからなかった理由と探した場所があります。",

	"facefusion_failed.cause":  "FaceFusion が画像を作れませんでした。{why}",
	"facefusion_failed.remedy": "顔の画像と入れ替え先の画像に、顔がはっきり写っているか確かめてください。成人向けの画像は FaceFusion が断ります。GPU のメモリ不足や FaceFusion の導入の不備のこともあります。詳細（FaceFusion の出力の末尾）と、設定画面の「FaceFusion」の状態を確かめてください。",

	"update_network.cause":  "更新の確認で、ネットワークに接続できませんでした。",
	"update_network.remedy": "ネットワークの接続を確かめてください。mam はしばらくしてから、自動でもう一度確認します。",

	"update_rate_limit.cause":  "GitHub の取得回数の上限に達したため、更新を確認できませんでした。",
	"update_rate_limit.remedy": "しばらく待ってください。mam が自動でもう一度確認します。",

	"update_access.cause":  "更新のリリースを GitHub から取得できませんでした（アクセスが拒否されたか、ファイルがありません）。",
	"update_access.remedy": "しばらくしてから、もう一度確認してください。続くときは、プロキシやファイアウォールが GitHub への接続を止めていないか確かめてください。",

	"update_verification.cause":  "ダウンロードした更新のファイルを検証できませんでした（破損または改ざんの可能性があるため、更新しません）。",
	"update_verification.remedy": "しばらくしてから、もう一度確認してください。続くときは、配布元から新しい版を取得して入れ直してください。",

	"update_release.cause":  "公開されているリリースの内容が正しくありません。",
	"update_release.remedy": "しばらくしてから、もう一度確認してください。続くときは、配布元に知らせてください。",

	"update_failed.cause":  "更新の確認に失敗しました。原因はわかりませんでした。",
	"update_failed.remedy": "もう一度確認してください。続くときは、ログファイルを確認してください。",

	"credits_insufficient.cause":  "相手のサービスのクレジット（残高）が足りません（HTTP 402）。この処理に必要な分を確保できませんでした。",
	"credits_insufficient.remedy": "相手のサービスのアカウントでクレジットを追加するか、自動チャージを有効にしてから、もう一度実行してください。残高が増えるまでは、何度実行しても同じように失敗します。",

	"topaz_maintenance.cause":  "Topaz のクラウド動画処理は、いまメンテナンス中で止まっています（HTTP 503）。実行中だった処理の分は返金されます。",
	"topaz_maintenance.remedy": "しばらく待ってから、もう一度見積りを取って実行してください。Topaz の状態は GET /video/status（isAvailable とメッセージ）で確かめられます。",

	"topaz_credit_difference.cause":  "アップロードした動画を Topaz が調べ直したところ、必要なクレジットが見積りと大きく違っていたため、処理を止めました（全額返金されます）。",
	"topaz_credit_difference.remedy": "動画の長さ・フレーム数・フレームレート・解像度が、ファイルの実際の値と合っていない可能性があります。ffmpeg の設定（ffprobe）が動いているか確かめて、もう一度見積りを取って実行してください。",

	"topaz_job_timeout.cause":  "Topaz の処理が時間切れになりました（アップロード待ち・順番待ち・処理時間のどれか）。クレジットは返金されます。",
	"topaz_job_timeout.remedy": "もう一度見積りを取って実行してください。長い動画は短く分けて試してください。",

	"topaz_job_failed.cause":  "Topaz が処理を完了できませんでした（相手側の失敗か、処理が取り消されました）。クレジットは返金されます。",
	"topaz_job_failed.remedy": "もう一度見積りを取って実行してください。同じ動画で続くときは、標準の H.264 の mp4 に変換した動画で試すか、詳細にあるリクエスト ID を添えて Topaz のサポートに連絡してください。同じ処理を mam が自動でやり直すことはありません（二重に課金されないため）。",

	"topaz_source_invalid.cause":  "Topaz が元の動画を処理できませんでした（ファイルが壊れている、拡張子と中身の形式が違う、動画の情報が実際と違う、のどれかです）。課金はされません。",
	"topaz_source_invalid.remedy": "元の動画が再生できるか確かめて、もう一度実行してください。続くときは、動画を標準の H.264 の mp4 に変換してから試してください。",

	"topaz_transfer_failed.cause":  "Topaz との動画のやり取り（元の動画のアップロード、または結果のダウンロード）に失敗しました。",
	"topaz_transfer_failed.remedy": "ネットワーク（プロキシ・VPN・ファイアウォール）を確かめてください。アップロードに失敗したときは、見積りの有効期限が切れていないか確かめて、もう一度見積りを取って実行してください。処理が済んだあとのダウンロードの失敗は、結果が Topaz に 7 日間残るので、時間を置いてから確かめられます。同じ処理を mam が自動でやり直すことはありません（二重に課金されないため）。",

	"topaz_job_interrupted.cause":  "処理の途中で mam が止まったか、ジョブが取り消されたため、Topaz の結果を待つのをやめました。Topaz はこの処理をすでに受け付けているため、Topaz 側では続いている可能性があり、クレジットが引かれることがあります（リクエスト ID: {request_id}）。",
	"topaz_job_interrupted.remedy": "mam からは Topaz の処理を取り消せません（取り消しの呼び出しは未実装です）。Topaz のアカウントで、このリクエスト ID（{request_id}）の処理の状況と使われたクレジットを確かめてください。同じ処理を mam が自動でやり直すことはありません（二重に課金されないため）。",

	"invalid_input.cause":    "入力が正しくありません。",
	"invalid_input.remedy":   "入力内容を直してから、もう一度実行してください。",
	"not_found.cause":        "対象が見つかりません。",
	"not_found.remedy":       "画面を読み込み直すか、指定した ID を確かめてください。",
	"conflict.cause":         "いまの状態では実行できません。",
	"conflict.remedy":        "実行中の処理が終わってから、または状態を確かめてから、やり直してください。",
	"locked.cause":           "LOCK されている項目は変更できません。",
	"locked.remedy":          "LOCK を解除してから、もう一度指示してください。",
	"forbidden.cause":        "Brand / Safety Constraint などの制約に違反しています。",
	"forbidden.remedy":       "制約に合うように内容を変えてください。",
	"budget_exceeded.cause":  "予算の上限を超えるため、実行できません。",
	"budget_exceeded.remedy": "予算の上限を見直すか、本数やモデルを減らしてください。",
	"unavailable.cause":      "この機能はいま使えません（API キーや設定が足りません）。",
	"unavailable.remedy":     "設定画面で、API キーと設定を確かめてください。",
	"upstream_failed.cause":  "外部のモデルやサービスが、使える答えを返しませんでした。",
	"upstream_failed.remedy": "しばらく待ってから、もう一度試してください。",

	"place.settings":      "設定を開く",
	"place.api-keys":      "設定: API キーとログインを開く",
	"place.llm":           "設定: テキスト生成（LLM）を開く",
	"place.local-models":  "設定: ローカル・代替モデルを開く",
	"place.model-manager": "モデル管理を開く",
	"place.chatgpt":       "設定: ChatGPT でログインを開く",

	"ui.title":    "失敗しました",
	"ui.cause":    "原因",
	"ui.remedy":   "直し方",
	"ui.settings": "関係する画面",
	"ui.detail":   "詳細を見る",
	"ui.copy":     "詳細をコピー",
	"ui.copied":   "コピーしました",
	"ui.error_id": "エラー ID",

	"llm_key_missing.route": "失敗した Agent は {agent} です。試したプロバイダーとモデル: {tried}。",
	"llm_key_missing.named": "{providers} の ",
	"llm_key_missing.any":   "使うプロバイダー（LLM は Anthropic / OpenAI / Google）の ",
	"advice.chatgpt":        "ChatGPT のサブスクで動かすなら、先に「API キーとログイン」で ChatGPT にログインし、「テキスト生成（LLM）」でこの Agent のプロバイダーを chatgpt-subscription（組織のアカウントは chatgpt-codex）にしてください。",
	"restart.pending":       "保存した設定のうち、{items} は再起動すると反映されます（設定画面の「再起動して反映」）。",
	"why.no_key":            "（API キー未設定）",
	"why.circuit_open":      "（一時的に利用不可）",
	"why.failed":            "（呼び出しに失敗）",

	"http.status": "（HTTP {status}）",
	"where.path":  "（場所 {path}）",

	"save_failed.disk":       "（ディスクの空き容量が足りません）",
	"save_failed.permission": "（保存先のフォルダに書き込めません）",

	"duration.hours":   "{n}時間",
	"duration.minutes": "{n}分",
	"duration.seconds": "{n}秒",

	"key_missing.elevenlabs": "ElevenLabs の ",
	"key_missing.topaz":      "Topaz Labs の ",

	"what.rough_cut": "Rough Cut の書き出し",
	"what.export":    "動画の書き出し",
	"what.compose":   "Take の合成",
	"what.audio_mix": "音声のミキシング",

	"facefusion.timeout":  "決められた時間内に終わらなかったため、打ち切りました。",
	"facefusion.exit":     "FaceFusion が途中でエラーを出して終わりました。",
	"facefusion.nooutput": "FaceFusion は終わりましたが、出力の画像がありません。",

	"agent.orchestrator":        "Orchestrator（進行管理）",
	"agent.producer":            "Producer（コンセプト）",
	"agent.writer":              "Writer（脚本）",
	"agent.director":            "Director（Scene / Shot の計画）",
	"agent.storyboard":          "Storyboard（絵コンテ）",
	"agent.art_director":        "Art Director（Reference Pack）",
	"agent.prompt_planner":      "Prompt Planner（Shot Spec）",
	"agent.video_judge":         "AI Video Judge（動画の評価）",
	"agent.repair":              "Repair（修復戦略の選択）",
	"agent.editor":              "Editor（編集の判定）",
	"agent.audio_director":      "Audio Director（音声の設計）",
	"agent.command_interpreter": "Command Interpreter（DIRECTOR 指示）",
	"agent.state_extractor":     "State Extractor（World State の抽出）",
	"agent.final_qc":            "Final QC（最終検査）",

	"list.separator": "、",
	"list.join":      "・",
}
