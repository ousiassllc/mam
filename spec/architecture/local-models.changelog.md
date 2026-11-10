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
| 1.9 | 2026-11-03 | §4.2 の表に `edit`（Qwen-Image-Edit。`qwen-image-edit-2511`・`txt2img-qwen-image-edit-2511`）を足した。`TextEncodeQwenImageEditPlus` は `image1`〜`image3` の入力で肯定・否定の 2 ノードなので、`comfyui/stage` が両方を区間ごとに同じ参照で作り直す（`<imageN>` は `Picture N` に直す）。`comfyui/stage/encoders.go` に分けた | Issue #971 |
| 1.10 | 2026-11-03 | §4.3 を新設: 割り当ての `param` に `steps`・`cfg`・`sampler`・`scheduler` を足した（テンプレートの KSampler・KSamplerAdvanced・SamplerCustom・SamplerCustomAdvanced 系に割り当て済み。上書きしなければグラフは不変）。`steps` の割り当てに `ratio`（2 段サンプラーの切り替え）。速さのモードがあるワークフローは steps・cfg を速さのモードが決める。§4.4 を新設: ワークフローの `prompt_weight`（supported / weak / unsupported / unknown）と、その根拠 | Issue #973・#972。生成の詳細設定と、プロンプトのウェイトの効き方の表示 |
| 1.11 | 2026-11-03 | §3: 組み込みテンプレートに `txt2img-sdxl-clipskip2`・`img2img-sdxl-clipskip2`（SDXL に `CLIPSetLastLayer`〔`stop_at_clip_layer = -2`〕を足した。Pony 系のモデル用。コアのノードだけ）を足した | Issue #984 |
| 1.12 | 2026-11-03 | §3: ワークフローの `requires_nodes`（組み込みテンプレートが必要とする追加ノード）。§4.2: 参照画像の方式 `controlnet`（制御画像。コアのノード）と `ipadapter`（追加ノード `ipadapter-plus`）、強さの割り当て `reference_strength`。組み込みテンプレートの「コアノードだけ」を、`requires_nodes` を宣言したものだけが追加ノードを使える、に改めた | Issue #976（[model-manager/custom-nodes.md](model-manager/custom-nodes.md)） |
| 1.13 | 2026-11-03 | §4.5 を新設: 画像を大きくするワークフローの印 `upscale`（`native`・`mode`・`max_output_pixels`）・割り当て `upscale_ratio`・モデルの一覧に出ないこと・コアのノード（`ImageScaleBy`・`ImageUpscaleWithModel`・`UpscaleModelLoader`・`VAEEncodeTiled`） | Issue #975 |
| 1.14 | 2026-11-03 | §4.6 を新設: 「顔だけ」の参照の固定の第 1 段（顔のまわりへの自動の切り出し・指示文の 3 文化・既定の強さ 0.9。効果は未検証）、顔検出器と PuLID・InsightFace・SFace の採否の表、効果の測り方（旧指標との併記） | Issue #960 |
| 1.15 | 2026-11-03 | §4.2: `edit` 方式の 3 モデルの役割に「ポーズだけ」（`pose`）を足した。役割の文（「`<imageN>` はポーズの参照としてだけ使う」）と、`face`・`style` と同じく VAE の潜在を作らない扱い（`stage.seenOnly`） | Issue #986 |
| 1.16 | 2026-11-02 | §4.7 を新設: `reference_mode: inpaint` と割り当て `mask_image`・`denoise`。テンプレート `inpaint-sdxl`・`inpaint-flux-dev`（コアノード。`ImageCompositeMasked` でマスクの外を元画像のままにする。`prompt_weight` と steps・cfg・sampler・scheduler の割り当ても持つ）。`supports_inpaint`。起動時の更新は新しいワークフロー名を足さない | Issue #974 |
| 1.17 | 2026-11-03 | §4.3 に、ワークフローの `quality_presets`（速い・きれいの steps・cfg。標準は書かない。速さのモードのワークフローには書けない）を足した | Issue #977 |
| 1.18 | 2026-11-04 | §4.8 を新設: ワークフローの `exposed_inputs`（画面に出す値。ノード ID＋入力名＋型＋範囲・選択肢＋表示名の加算的な汎用入力）・検査・グラフへの適用・保存先 | Issue #977（ステージ 2） |
| 1.19 | 2026-11-04 | §4.3: 2 段のサンプラー（`ratio` つきの `steps`）は `sampling.Caps.TwoStage` を立て、ステップ数の下限を 2 にした（検査と画面のスライダー） | 1 ステップだと後の段が何もせず、ノイズのままの動画になった（Issue #973 レビュー） |
| 1.20 | 2026-11-03 | §4.2: 導入したあとにカタログが足したワークフロー名を起動時に足す `wfrefresh.AddMissing` を足した（利用者のコピーは上書きしない。[model-manager/model-manager.md](model-manager/model-manager.md) §5.2） | Issue #990 |
| 1.21 | 2026-11-03 | §3: `speed_modes` を画像のワークフローにも広げた（`ValidateModel` が画像を断らない。`ImageRequest.Options.SpeedMode`。先頭のモードでも使わない LoRA のノードを送らない）。`txt2img-qwen-image-edit-2511` が `standard`・`lightning-8`・`lightning-4`（lightx2v の Lightning LoRA）を持ち、`quality_presets` を外した | Issue #991（#971 の任意項目。#973 の決定「速さのモードが steps・cfg を決める」に従う） |
| 1.22 | 2026-11-03 | §4.9 を新設: 生成テストの結果ごとの顔の類似度の自動計測（SFace。`params.face_similarity`・目安の閾値 0.363・モデルの配布は YuNet の先例のまま・ライセンスの一次情報）と、#960 の監査（第 1 段以外の項目の状態・実機が必要な項目） | Issue #998・#960 |
| 1.23 | 2026-11-04 | §4.9: 顔モデル・ONNX Runtime が無いときも、役割 `face` の参照がある結果に `face_similarity`（`skipped`: `models_missing` / `runtime_unavailable`、`reason`・`fix`・`model_dir`・`missing_files`）を残す。オンかオフかを `list_generation_models` の `face_similarity` と、見積りの警告 `face_similarity_off` で読める。モデルマネージャーから入れる導線は無い（ONNX はカタログに載せられない）ことを確かめ、導入方法は直し方として出す（自動取得は作らない） | Issue #1026 |
| 1.24 | 2026-11-04 | §4.9「モデルファイルの配布」を変えた: YuNet・SFace をモデルマネージャーの区画「顔チェック用モデル」からボタン 1 つで導入できる（資産セット `onnx-face-models`・opencv_zoo の固定コミット・SHA-256 検証・ライセンスの表示）。導入後の再起動は要らない（顔エンジンを初めて使うときに読み込む）。`models_missing` の `fix` は導入を指し、ONNX Runtime が無いのは別の理由として区別する。それまでの「カタログに載せない」「モデルマネージャーから入れる導線は無い」をやめた | Issue #1026（悠太さんの決定） |
| 1.25 | 2026-11-04 | §4.10 を新設: 動画を大きくする・滑らかにするワークフローの印 `video_enhance`（`op: upscale` / `interpolate`）、割り当て `source_video`・`video_scale`・`frame_multiplier`・`output_fps`、一覧に出さないこと、`enhancevideo.Run` と ComfyUI での動画のアップロード（拡張子）・実行・取得、テンプレート 3 件（実機未確認） | Issue #1031 |
| 1.26 | 2026-11-06 | §4.9: バッチの結果の顔の類似度をモデルごとにまとめて読めるようにした（`BatchStatus.face_summaries`＝`record.FaceSummary`: 測れた数・測れなかった数・平均・最小・最大・目安以上の数と割合。MCP の `get_generation_test`、画面のグリッドのモデルの行）。「改善幅を数字で確かめる」ために、結果を 1 枚ずつ見ず、設定違いのバッチの平均を並べて比べられる。実機（GPU）での改善幅の測定そのものはこれまでどおり「実機が必要」 | Issue #998 |
| 1.27 | 2026-11-06 | §4.3.1 を新設: `frames` の割り当てに `offset`（整数、−64〜64）を足した。フレーム数に足した値を入れる（`frames-2` は `offset: -2`。1 未満は 1）。MCP の割り当て（`localmodels`・`workflows`）も `offset` を持つ。§4 の割り当て表（`param`）の説明に追記 | Issue #1076。Mega の最初から最後のフレーム（FLF2V）は、灰色のフレームの数が `frames-2` で、割り当てが式を持てなかったため載せられなかった |
| 1.28 | 2026-11-06 | §3.4・§4.9 の「一覧に出ない」の記述から、モデルマネージャーの試し生成を外した（区画の廃止） | Issue #1100 |
| 1.29 | 2026-11-10 | 画面名・機能名の「生成テスト」を「単発生成」に改めた（本文の文言だけ。仕様は変えない。過去の改訂履歴の文は当時のまま） | Issue #1137 |
