# 生成テスト: 改訂履歴

[gentest.md](gentest.md)・[er-gentest.md](er-gentest.md)・[gentest-adapter.md](gentest-adapter.md) の改訂履歴。

| 版 | 日付 | 変更内容 | 理由 |
|---|---|---|---|
| 1.0 | 2026-10-26 | 新規。プロジェクトに属さない画像・動画の比較生成（モデル × プロンプト × 本数）、見積りの確認（`priced` / `free` / `unknown`）と上限、バッチのジョブ（`resumable=false`・取り消し・異常終了後の整理）、履歴・プロンプト履歴・プロジェクトへの取り込み、テーブル、モデル名を指定した直接呼び出しとシードを定めた。#833 の試し生成を、この仕組みの 1 件のバッチに統合した | Issue #835 |
| 1.1 | 2026-10-25 | シードの表の「Imagen」を外し、Nano Banana Lite を足した（送らない側） | Imagen 4 の終了と、画像モデルの割り当て・識別子の変更（`model-adapters.md` 1.52。Issue #836・#837） |
| 1.2 | 2026-10-25 | §2 に、成人向けモデル（モデルマネージャー）を年齢確認の前は一覧に出さず、名指しも断ることを足した（`Deps.Hidden`） | Issue #841 |
| 1.3 | 2026-10-25 | §2: 直接呼び出し（`direct.Image`・`direct.Video`）も、年齢確認がオフの間は成人向けのモデルを `agegate.ErrRefused` で断る（一覧と始まる時の確認に加えた最後の砦） | Issue #848 |
| 1.4 | 2026-10-28 | §3 確認の取り決め: `estimate_id` はプロンプトの改行を LF にそろえて数える（送る文は書き換えない）。単価なしのモデルを含む見積りも、確認のあと条件を変えなければ実行できることを明記 | Issue #864。確認した見積りが毎回「違う」と判定されて実行できなかった（textarea の CRLF と hidden の LF の差） |
| 1.5 | 2026-11-01 | §2: ChatGPT サブスクの画像モデルを、識別子ではなく表示名 `GPT Image 2 (ChatGPT サブスク)` で出すことを明記（`chatgptid.Label`） | Issue #870。モデル選択と見積りの内訳に識別子のまま出ていた |
| 1.6 | 2026-11-01 | §8 の画面で、モデルの選び方を「チェックボックス（無効なものは理由つき）」から、モデルのカード（タップで選ぶ。選べないものは「選べません」のバッジで、理由は「?」ヘルプと読み上げにある）に改めた | Issue #906 |
| 1.7 | 2026-11-01 | §3.1 を新設: プロンプトとネガティブプロンプトを LLM（LLM の割り当ての Prompt Planner）で英語に訳して送る切り替え `translate_to_english`（既定 ON。画面と MCP で同じ）。英語の文は訳さない（「訳す文」は、Latin でも Common でもない script の文字を 1 字でも含む文。`µ`・`ʼ` のような Common の文字や数字・記号は数えず、`5µm particles` は英語として訳さない）・1 回の実行で 1 回だけ訳す・訳せなければ何も生成せず `upstream`（502）・見積りに単価なし（`unknown`）の「英訳」の行・履歴に入力した文と送った英語の両方。§1 の「補正しない」を英訳 OFF のときと改め、§8・§9 に画面とテストを足した。[er-gentest.md](er-gentest.md) の `params` に `translate_to_english`・`translation` を足した | Issue #913。生成モデルは英語のほうが良い結果が出やすいが、生成テストだけ日本語のまま届いていた |
| 1.8 | 2026-11-02 | §6: 画像モデルの参照画像に役割（reference / face / style / edit）と強さ（0〜1、既定 0.7）、リクエストにポーズを真似ない設定と LoRA（名前・強さ 既定 0.4）を足した。モデルごとの対応（`reference_mode`・`max_reference_images`・`reference_roles`・`supports_lora`）と、対応しない分の警告（`too_many_refs`・`role_unsupported`・`lora_unsupported`）、`Service.LoRAs` | Issue #933 |
| 1.9 | 2026-11-02 | §8: 入力画像の画面を、ドロップゾーン 1 つ・サムネイルの格子（番号・外す・並べ替え・拡大）・モデルに合わせて出す役割と強さ・ポーズを真似させない設定・LoRA・モデルのカードの入力画像アイコンに改めた | Issue #933 |
| 1.10 | 2026-11-02 | §6: 存在しないシンボルの参照を実在のもの（`localcfg.DefaultReferenceStrength` ほか）に直した。img2img のモデルは参照の先頭 1 枚だけを使い残りは `too_many_refs`、強さの保存値は画面（常に明示。0.7）と MCP（省くと NULL）で違うこと、LoRA の名前の検査（200 文字・制御文字なし）、強さ 0 は LoRA も参照と同じく最小値で送る（エンジンが 0 を既定 0.4 と読むため）こと、LoRA の一覧は失敗も短く覚えて取得を共有し動画では問い合わせないことを足した。§8: 画像／動画の切り替えでブラウザで選んだファイルが外れることを足した | Issue #933 のレビュー |
| 1.11 | 2026-11-02 | §5: ローカル（登録した接続先）のタイムアウトの理由は課金に触れず、待ち時間と次にできること（接続先のタイムアウトを延ばす・解像度や秒数を小さくする）を出す | Issue #917 |
| 1.12 | 2026-11-02 | §3.2 を新設: プロンプトマッチ（参考の画像から英語のプロンプトを作り、似るまで直して生成し直す）。詳細は [prompt-match.md](prompt-match.md) | Issue #916 |
| 1.13 | 2026-11-02 | §7.1 を新設: 結果の ZIP エクスポート（`photos/` と `prompts/prompts.csv`。選び方は `export.Selection` 1 か所を画面と MCP で共有・ストリーム・分割・CSV インジェクション対策・ファイル名の無害化）。`gentestrepo.Filter.IDs` を足した | Issue #932 |
| 1.14 | 2026-11-02 | §6: 動画の速さのモード（`speed_mode`。LightX2V の 4 ステップ / 通常 20 ステップ）、モデルの `video.speed_modes`、警告 `speed_mode_unsupported`、画面の「速さ」の選択、結果への記録を足した | Issue #930 |
| 1.15 | 2026-11-02 | §10 を新設: `service/gentest` を責務ごとに `runner`・`history`・`record` へ分けたパッケージ構成と依存の向き（振る舞いは変えない） | Issue #963 |
| 1.16 | 2026-11-02 | §5: ローカルのタイムアウトの文言を 3 通りに分けた（ComfyUI のジョブが進まなくなった・全体の上限 24 時間・互換の接続先の待ち時間） | Issue #961 |
| 1.17 | 2026-11-02 | §6: 警告 `role_unsupported` に、使わない参照の役割 `roles` を付けた（画面が実行前のモーダルでモデルごとに伝えるための源。警告と `refs.RoleFor` の置き換えは変えない） | Issue #959 |
| 1.18 | 2026-11-02 | §3 の上限に `models.json` の `gentest.match_face` を足した（プロンプトマッチの顔の混ぜ方。既定は混ぜない）。プロンプトマッチは [prompt-match.md](prompt-match.md) の 1.1 で、似てる度への顔の混ぜ込み（#964）と、見積りへの LLM のトークン単価の算入・各回から比較への導線（#965）を足した | Issue #964・#965 |
| 1.19 | 2026-11-03 | §3.3 を新設: 生成の詳細設定（`seed`・`sampling` の `steps`・`cfg`・`sampler`・`scheduler`）。固定シードは n 本目に `seed + n`、受け取らない値は警告 `sampling_ignored`・`seed_ignored`、Speed mode のモデルは steps・cfg を無視（速さのモードが優先）、使った値を `params.sampling` に残す。§3.1: 英訳でウェイト構文（`(語:1.3)`・`(語)`・`[語]`）を残す指示、欠けたウェイトの検出（`lost_weights`）、効かないモデルへの警告 `prompt_weight` | Issue #973・#972 |
| 1.20 | 2026-11-03 | §6: 参照画像の方式 `controlnet`・`ipadapter` と、追加ノードが無いモデルの `unavailable=missing_node` | Issue #976 |
| 1.21 | 2026-11-03 | §3.4 を新設: 結果を 2 倍・4 倍にする「大きくする」（アップスケール）。ワークフロー種別 `upscale`（モデルの一覧には出さない）・方式 `model` / `two_pass`・別の結果として `params.upscale`（元の結果・倍率・使ったモデル）を残す・上限・16GB は未測定 | Issue #975 |
| 1.22 | 2026-11-03 | §6: 参照の強さの既定を役割ごとにした（`localcfg.DefaultStrengthOf`。「顔だけ」は 0.9、他は 0.7）。編集モデルへ渡す「顔だけ」の参照は、バッチの入力を読む時に 1 回だけ顔のまわりに切る（`runner.FaceCropFunc`・`facesim.CropFace`。顔検出モデルがあるときだけ） | Issue #960。詳細は [local-models.md](../local-models.md) §4.5 |
| 1.23 | 2026-11-03 | §6: 参照画像の役割 `pose`（ポーズだけ）、入力の出どころ `pose_library`（Shisei のポーズ集。`service/gentest/poses`）、VNCCS PoseStudio の扱い（ルート ①② は既存テンプレート、③④ は許可リストのみ）。§10 に `poses` | Issue #985・#986 |
