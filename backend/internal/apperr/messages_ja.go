package apperr

// messagesJa are the Japanese texts, the base of the catalog. A code has a cause ("<code>.cause": why it failed) and a remedy
// ("<code>.remedy": what to do); each ends with its full stop, so that View.Sentence can join them. {name} is an argument
// (Error.Args); an argument that is not given reads as nothing.
var messagesJa = map[string]string{
	"unknown.cause":  "原因がわかりませんでした。想定していない失敗です。",
	"unknown.remedy": "もう一度試してください。直らないときは、詳細（とエラー ID）を添えて報告してください。mam のログにも同じ ID で残しています。",

	"key_missing.cause":  "API キーが設定されていません。",
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

	"request_rejected.cause":  "相手がリクエストを受け付けませんでした{http}。入力か設定が、そのモデルに合っていません。",
	"request_rejected.remedy": "詳細に、相手の返した理由があります。プロンプトの長さや内容、解像度・秒数・入力画像などの設定を見直してください。",

	"input_missing.cause":  "このモデルが必ず使う入力（{param}）が渡されていません。",
	"input_missing.remedy": "{param} に使う画像を指定してから実行してください。画像から動画を作るモデルは、最初のフレーム（first_frame）の画像が要ります。",

	"input_unsupported.cause":  "このモデルは、渡された入力画像や解像度の組み合わせを受け付けません。",
	"input_unsupported.remedy": "入力画像の使い方（最初のフレーム・最後のフレーム・参照画像）や解像度を、そのモデルが対応するものに変えてください。モデルの対応はモデル一覧で確かめられます。",

	"rate_limited.cause":  "相手のサービスの利用回数の上限（レート制限、HTTP 429）に達しました。",
	"rate_limited.remedy": "しばらく待ってからもう一度実行してください。続くときは、同時に走らせる数を減らすか、相手のサービスのプランの上限を確かめてください。",

	"server_error.cause":  "相手のサービス側で障害が起きています{http}。",
	"server_error.remedy": "しばらく待ってからもう一度試してください。続くときは、相手のサービスの障害情報を確かめてください。ローカルの ComfyUI なら、ComfyUI のログを確かめてください。",

	"connection_failed.cause":  "接続先につながりませんでした。",
	"connection_failed.remedy": "接続先が起動しているか、URL とネットワーク（プロキシ・VPN・ファイアウォール）を確かめてください。同梱の ComfyUI は、モデル管理でエンジンが動いているかを確かめてください。",

	"circuit_open.cause":  "このモデルのベンダーで失敗が続いているため、呼び出しを止めています。",
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

	"llm_invalid_output.cause":  "LLM の答えが、求めた形式になっていませんでした。",
	"llm_invalid_output.remedy": "もう一度実行してください。続くときは、設定画面の「テキスト生成（LLM）」で、その Agent のモデルを変えてみてください。",

	"not_writable.cause":  "保存先のフォルダに書き込めません。",
	"not_writable.remedy": "書き込める場所に移すか、フォルダの権限を確かめてください。外部の ComfyUI の URL を使う方法もあります。",

	"no_gpu.cause":  "対応する GPU が見つかりません。",
	"no_gpu.remedy": "外部の ComfyUI の URL を設定するか、CPU 実行を明示的に選んでください。",

	"image_unavailable.cause":  "ChatGPT のアカウントで、画像生成を載せられるモデルが使えませんでした。",
	"image_unavailable.remedy": "ChatGPT のプランとログインを確かめ、設定の「ChatGPT でログイン」からやり直してください。",

	"save_failed.cause":  "設定やデータを保存できませんでした。",
	"save_failed.remedy": "ディスクの空き容量とフォルダの権限を確かめてから、もう一度保存してください。続くときは mam のログを確認してください。",

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
}
