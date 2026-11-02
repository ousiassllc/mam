# ローカル・代替の画像／動画モデル 改訂履歴

[local-models.md](local-models.md) の改訂履歴。新しい版は末尾に足す。

| 版 | 日付 | 変更内容 | 理由 |
|---|---|---|---|
| 1.0 | 2026-10-24 | 新規。接続先の種類（ComfyUI・OpenAI 互換の画像のみ）、識別子、`models.json` のスキーマ、ComfyUI の割り当て表・実行・取り消し・テンプレート、接続テスト、ダウンロード方針、即時反映、Service API を定めた | Issue #831 |
| 1.1 | 2026-10-25 | 冒頭に、MAM が ComfyUI を取得・起動してモデルを入れる経路（`model-manager/model-manager.md`、接続先 `bundled-comfyui`）への参照を足した | Issue #833 |
| 1.2 | 2026-10-29 | §4 に LTX-2.5（§4.1）を足した: カタログ `ltx-2.5-22b-distilled` とテンプレート `txt2video-ltx-2-5`・`img2video-ltx-2-5`、取得の手順（ライセンスの同意とアクセスの申請 → トークンの発行 → 保存 → 導入）、外部の ComfyUI での使い方。組み込みテンプレートの一覧を更新した | Issue #877 |
| 1.3 | 2026-11-02 | §3 の `workflow` に `reference_mode`・`no_reference`・`lora_model_node`、§4 の割り当てに `reference_image` の `optional` と `reference_denoise` を足し、§4.2 を新設した: 画像モデルの参照画像（img2img と編集）、強さ・役割・LoRA・ポーズを真似させない指定のモデルごとの方式。組み込みテンプレートを `templates/image/`・`templates/video/` に分けた。起動時に、参照に対応する前の写しのままのインストール済みモデルを更新する | Issue #933。同梱モデルが参照画像を使えなかった |
| 1.4 | 2026-11-02 | §4.2: 起動時の更新は**利用者が書き換えていない古い写しだけ**にした（リテラルの値が違えば編集とみなす）。画面・MCP での編集は、フォームに欄の無い `reference_mode` などを保存済みのワークフローのまま残し、参照画像の行を保存済みの枚数まで出す。`reference_mode` があるモデルは `Options.Refs` の無い `Inputs` を参照にしない。参照の区切りが多いときは近い区切りへ寄せる | Issue #933（refine の指摘） |

| 1.5 | 2026-11-02 | §1・§3: ComfyUI のタイムアウトの既定を 3600 秒から 10800 秒（3 時間）にし、接続先ごとに変えられると明記。取り消しは `/api/jobs/<id>/cancel` が失敗（404・405）したら対象を指す割り込み／キュー削除に回る（405 を取りこぼして止まらなかった） | Issue #917 |
| 1.6 | 2026-11-02 | §4.1: 非検閲のテキストエンコーダーのテンプレート 2 つ（`txt2video-ltx-2-5-uncensored`・`img2video-ltx-2-5-uncensored`）、音声つきの保存形式、macOS（外部 ComfyUI）が不安定で未確認であることを足した | Issue #927 |
| 1.7 | 2026-11-02 | §4.2 の表に FLUX.2 の `edit`（`flux2-klein-4b`・`txt2img-flux2-klein`）を足した。参照は `ReferenceLatent` の鎖で、最大 4 枚。強さは Qwen の編集と同じ意味で区間に分ける。`localcfg` のテンプレート（20 個）と `comfyui/stage` パッケージ（Qwen の編集の区間分けを `agent/modeladapter/comfyui` から移した）の `CLIPTextEncode` のとき（`stageReferenceLatent`）、`dropUnused` が使わないスロットの縮小・VAEEncode・`ReferenceLatent` を外す | Issue #931 |
| 1.7 | 2026-11-02 | §3: ワークフローの `speed_modes`（動画。先頭がグラフそのまま、2 つ目以降が `overrides`）と、リクエストの `speed_mode`、出力から辿れなくなったノードを送らないこと、テンプレート `img2video-wan22-14b-lightx2v`・`img2video-wan22-14b-lightx2v-uncensored`・`txt2video-wan22-14b-remix`（コアノードだけ）を足した | Issue #930 |
| 1.8 | 2026-11-02 | §3: ComfyUI の接続先の `timeout_seconds` を「ジョブが進まなくなってから打ち切るまでの時間」にした（進んでいる間は延びる）。呼び出し全体は 24 時間が上限（暴走防止に残す）。キュー待ちは打ち切らない。ソケットが無いときは従来どおり全体で数える。画像と動画で上限を分けない・既定を縮めないを決め、理由を「タイムアウト（進み具合）」に書いた | Issue #961（#917 は既定を 3 時間にしただけだった） |
| 1.9 | 2026-11-03 | §4.3 を新設: 割り当ての `param` に `steps`・`cfg`・`sampler`・`scheduler` を足した（テンプレートの KSampler・KSamplerAdvanced・SamplerCustom・SamplerCustomAdvanced 系に割り当て済み。上書きしなければグラフは不変）。`steps` の割り当てに `ratio`（2 段サンプラーの切り替え）。速さのモードがあるワークフローは steps・cfg を速さのモードが決める。§4.4 を新設: ワークフローの `prompt_weight`（supported / weak / unsupported / unknown）と、その根拠 | Issue #973・#972。生成の詳細設定と、プロンプトのウェイトの効き方の表示 |
