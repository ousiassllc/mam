# MCP ツール: World State・参照・Branch・バージョン・Series・メモ

[`mcp-tools.md`](mcp-tools.md) の規約に従う、World State・Reference Library・Branch・バージョン履歴・Series・メモ（Notes）の画面の操作。サービス層を直接呼び、検証・LOCK・状態の衝突は画面と同じ（`validation` / `locked` / `conflict` / `not_found`）。ルートとツールの対応は [`mcp-tools-world-notes-coverage.md`](mcp-tools-world-notes-coverage.md)。

共通の入力: `branch_id` を渡すと、その Branch の World State を扱う（省略すると本線）。ID は文字列。

## World State（`/projects/:id/world-state`）

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_world_state` | World State の表示。画面と同じ | `project_id`, `branch_id?` | `project_id`, `branch_id?`, `branch_name?`, `version`, `characters[]`, `products[]`, `locations[]`, `camera`, `story`, `locks[]`（`target_type`, `target_id?`, `parameter`）, `project_locks[]`（`story` / `music` の `mode`）, `library[]`（Reference Library の画像）, `versions[]`（本線の World State のバージョン）。`characters[]` の各要素は `id`, `name`, 属性, `face_reference` / `body_reference`、そして下書きのときだけ `draft: true`（Brief の添付画像から LLM なしで作った下書き。名前は仮で、`update_character` で属性を保存すると外れる。Issue #1022） | はい |
| `update_character` | 登場人物の属性を、渡した項目だけ更新（空文字で空にする）。LOCK 中の項目は `locked` で何も書かない | `project_id`, `branch_id?`, `character_id`, `hairstyle?`, `wardrobe?`, `voice?`, `personality?`, `emotion?`, `position?`, `current_action?`（各 200 文字以内） | `get_world_state` と同じ | いいえ |
| `update_product` | 商品の属性（`material`, `orientation`, `condition`）を更新 | `project_id`, `branch_id?`, `product_id`, 各属性 | 同上 | いいえ |
| `update_location` | 場所の属性（`time`, `lighting_state`, `weather`）を更新 | `project_id`, `branch_id?`, `location_id`, 各属性 | 同上 | いいえ |
| `set_world_field_lock` | 項目を AUTO / LOCK で切り替える（冪等）。LOCK した項目は State Extractor が更新せず、編集もできない | `project_id`, `branch_id?`, `target_type`（`character` / `product` / `location` / `camera` / `story`）, `target_id?`（camera・story は不要）, `parameter`, `mode`（`auto` / `lock`） | 同上 | いいえ |
| `create_character` | 登場人物を直接作る（LLM の企画を待たない。企画が LLM 未設定で止まった Project や、空の Series の正本に登場人物を足す。#1001）。`project_id` ならその Project の World State（`branch_id` で Branch）に、`series_id` なら Series の正本に作る（どちらか一方。両方・どちらも無しは `validation`）。名前は前後の空白を除いて 1〜100 文字、同じ World State / 正本の中で重複できない（大文字小文字・全半角を区別しない。重複は `validation`）。`hairstyle` / `wardrobe` / `personality` / `voice` は各 200 文字以内。顔・全身の参照画像は作成時に `face_reference_id` / `body_reference_id`（Reference Library の画像の `id`。`kind=character` でなければ `validation`、無い ID は `not_found`）で渡せ、あとから `assign_character_reference` でも付け替えられる。Series には Reference Library が無いので、Series に作るときの参照画像は、その画像を登録した Project を `reference_project_id` で指す（参照画像を渡すのに無ければ `validation`。正本は画像を自分のものとして持つ）。作った登場人物の `character.draft` は `false`（下書きの印は、企画が LLM 未設定で止まったときに Brief の添付画像から作る下書きだけに付く。Issue #1022）。Project に作るとき World State に新しいバージョンが、Series に作るとき正本に新しいバージョンができる（作成済みの Episode は変わらず、次の Episode から使われる）。LOCK の対象にはまだならない（作った直後は AUTO） | `project_id?`, `series_id?`, `branch_id?`（`project_id` のとき）, `name`, `hairstyle?`, `wardrobe?`, `personality?`, `voice?`, `face_reference_id?`, `body_reference_id?`, `reference_project_id?`（`series_id` のとき） | `project_id` / `series_id`, `character_id`（`assign_character_reference` の `entity_id`、`update_series_character`・`update_character_voice_reference` の `character_id`）, `character`（`id`, `name`, `hairstyle?`, `wardrobe?`, `voice?`, `personality?`, `face_reference?`, `body_reference?`）, `version`（作ったあとの World State / 正本の最新バージョン番号） | いいえ |
| `assign_character_reference` | 登場人物の参照枠（`face` / `body`）に Reference Library の画像を割り当てる。`reference_id` を空にすると枠を空にする。枠の LOCK（`identity`）中は `locked`。種別が枠に合わない画像は `validation` | `project_id`, `branch_id?`, `entity_id`, `slot`, `reference_id?` | 同上 | いいえ |
| `assign_product_reference` | 商品の参照枠（`geometry` / `label`）。LOCK は `geometry` / `label` | 同上 | 同上 | いいえ |
| `assign_location_reference` | 場所の参照枠（`layout`）。LOCK は `layout_reference` | 同上 | 同上 | いいえ |

`parameter`（LOCK できる属性）: character は編集できる 7 項目と `identity`、product は 3 項目と `geometry` / `label`、location は 3 項目と `layout_reference`、camera は `lens` / `position` / `angle` / `movement`、story は `scene` / `character_goal` / `known_information` / `previous_event`。それ以外は `validation`。

## Reference Library（`/projects/:id/references`）

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_references` | Library の画像一覧（新しい順）。絞り込みは画面と同じ | `project_id`, `q?`（名前・タグの一部）, `kind?`（character / product / location / style / lighting）, `tag?` | `project_id`, `items[]`: `id`, `asset_id`, `kind`, `name`, `tags[]`, `source`（`uploaded` / `generated`）, `source_shot_id?`, `created_at` | はい |
| `delete_reference` | Library から画像を外す（画像そのものと Shot が持つ参照は残る。冪等ではなく、無い ID は `not_found`） | `project_id`, `reference_id` | `list_references` と同じ（絞り込みなし） | いいえ（破壊的） |
| `pin_reference` | Library の画像を Shot の Reference Pack に加える（同じ種別・名前は置き換わる）。生成中の Shot は `conflict`。他の Project の画像は `not_found` | `shot_id`, `reference_id` | `shot_id`, `references[]`: `kind`, `name?`, `asset_id`, `prompt?`, `model?`, `library_item?` | いいえ |

画像の登録（アップロード）は基盤の `create_upload_ticket`（`purpose=reference_image`、`mcp-tools.md` の表）、画像そのものの取得は `get_asset_download_url`（`asset_id` は `items[]` の値）。このドメインはアップロードの用途もダウンロードの種別も足さない。

## Branch（`/projects/:id/branches`・`/branches/:id`）

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_branch_tree` | Branch の一覧（作成順。`parent_id` で木になる）と、Branch を作れる Shot | `project_id` | `project_id`, `branches[]`（`id`, `parent_id?`, `name`, `point_type`, `point_text`, `point_scene_id?`, `point_shot_id?`, `point_offset_seconds?`, `world_state_version`, `version`, `created_at`）, `shots[]`（`shot_id`, `label`） | はい |
| `get_branch` | Branch の詳細: 分岐点・親・Branch 自身の Scene と Shot・バージョン・参照できる World State の最大バージョン | `branch_id` | `branch`, `parent?`, `scenes[]`, `shots[]`, `versions[]`, `latest_world_version`, `started` | はい |
| `create_branch` | Shot に分岐点（`scene` / `shot` / `frame`）を作って Branch を作り、その時点の World State を保存する。`direction` があれば AI が最初の Shot を計画して生成を始める（生成の設定がなければ `unavailable`、Brand の禁止表現は `forbidden`、予算超過は `budget_exceeded`） | `shot_id`, `point_type?`（既定 `shot`）, `offset_seconds?`（`frame` のとき 0 以上 Shot の長さ以内）, `name?`（100 文字以内。省略は `direction` か「Branch N」）, `direction?` | `get_branch` の出力。`direction` があるとき `job`（`job_id`, `state`）も返す | いいえ（`direction` があるときモデル API を呼ぶ） |
| `update_branch` | Branch の名前の変更、または参照する World State のバージョンの付け替え（履歴に残る） | `branch_id`, `name?`, `world_state_version?`（1 から `latest_world_version`） | `get_branch` の出力 | いいえ |

### ジョブ `create_branch`

`create_branch` に `direction` を渡したときだけ `job` を返す。`job_id` は `get_job` に渡す（`job_<UUIDv7>`。ジョブの履歴に残るので再起動後も引ける）。対象（`target`）は分岐元の Shot。Branch の作成と最初の Shot の計画・生成の開始は `job` が返るまでに終わっている。状態は永続化された Branch・その最初の Shot・Take から導く: `running`（最初の Shot が GENERATING / JUDGING / REPAIRING）→ `succeeded`（ジョブの開始より後に作られた Take がある。`result`: `branch_id`, `shot_id`, `shot_state`, `take_ids`）。Branch に Shot がない、または Take ができないまま止まったときは `failed`。

## バージョン履歴（F-28）

履歴は追加のみ。戻す操作は選んだバージョンの写しを新しいバージョンとして足す（戻した操作も戻せる）。`version_number` は 1 以上（0 以下は `validation`、無い番号は `not_found`）。

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_shot_versions` | Shot の Spec の保存済みバージョン（新しい順） | `shot_id` | `versions[]`: `version_number`, `summary`（Shot の目的）, `created_at` | はい |
| `list_world_state_versions` | 本線（または `branch_id` の Branch）の World State のバージョン（新しい順） | `project_id`, `branch_id?` | 同上（`summary` は件数） | はい |
| `get_branch_versions` | Branch 自身のバージョンと、Branch 専用の World State のバージョン | `branch_id` | `branch_id`, `versions[]`, `world_state_versions[]` | はい |
| `restore_shot_version` | Shot の Spec を戻す。LOCK 中の部分が変わるなら `locked`、生成中は `conflict`（何も書かない） | `shot_id`, `version_number` | `shot_id`, `versions[]`（戻した結果を含む） | いいえ |
| `restore_world_state_version` | World State（本線、または `branch_id`）を戻す。LOCK 中の項目が変わるなら `locked`（何も書かない） | `project_id`, `branch_id?`, `version_number` | `get_world_state` の出力 | いいえ |
| `restore_branch_version` | Branch 自身（名前と World State の参照）を戻す | `branch_id`, `version_number` | `get_branch_versions` の出力 | いいえ |
| `restore_branch_world_state_version` | Branch 専用の World State の系列を戻す。LOCK 中の項目が変わるなら `locked` | `branch_id`, `version_number` | 同上 | いいえ |

## Series（`/series/:id`）

`series_id` は `list_projects` の `series_id`、または `create_series` の結果。正本を更新しても、作成済みの Episode には反映されない（次の Episode から）。

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `get_series` | Series の正本・履歴・Episode・Episode に使える Brand Template | `series_id` | `series_id`, `name`, `version`, `created_at`, `canon`（`style?`, `style_reference_asset_id?`, `characters[]`, `products[]`, `locations[]`）, `versions[]`（`version_number`, `created_at`。新しい順）, `episodes[]`（`project_id`, `name`, `state`, `episode_number`）, `brand_templates[]`（`id`, `name`） | はい |
| `create_series` | Series を作る。`from_project_id` があればその Project の登場人物・商品・場所を正本にして第 1 話にする（`name` 省略時はその Project の名前）。すでに Series に属する Project は `validation` | `name?`（1〜100 文字）, `from_project_id?` | `series_id`, `name` | いいえ |
| `create_episode` | 正本から次の Episode（新しい Project）を作る。`previous_episode_id` があればその話の最終 World State から始める（同じ Series の話でなければ `validation`）。`brand_template_id` があれば制約をコピーする（無ければ `validation`） | `series_id`, `previous_episode_id?`, `brand_template_id?` | `project_id`, `name`, `episode_number` | いいえ |
| `update_series_character` | 正本の登場人物の `hairstyle` / `wardrobe` / `voice` / `personality` を、渡した項目だけ更新（各 200 文字以内）。正本に新しいバージョンができる | `series_id`, `character_id`, 各属性 | `get_series` と同じ | いいえ |
| `update_series_style` | 正本の Style を更新する（500 文字以内。空にもできる） | `series_id`, `style` | `get_series` と同じ | いいえ |

正本に登場人物を足すのは「World State」の節の `create_character`（`series_id` を渡す。#1001）。LLM の企画を待たずに、空の正本へ直接作れる。

## 参照音声（`/series/:id/characters/:characterId/voice-reference`・`/projects/:id/characters/:characterId/voice-reference`）

登場人物の参照音声（F-20・F-29）。ローカルの声（GPT-SoVITS）が同じキャラに同じ声を毎回作るための、3〜10 秒の録音と、その書き起こし・言語。Series の正本の登場人物（`series_id` と `get_series` の `canon.characters[].id`）にも、Project の登場人物（`project_id` と `get_world_state` の `characters[].id`）にも付けられる。`series_id` と `project_id` はどちらか一方（両方・どちらも無しは `validation`）。正本に付けると正本に新しいバージョンができ、作成済みの Episode は変わらない（次の Episode から）。Episode の登場人物の音声は、その Episode だけ差し替えられる。`get_series`・`get_world_state` の登場人物に `voice_reference`（`file_path`（アセットディレクトリ内の保存先。参照しない・ダウンロードは `get_character_voice_reference_url`）, `mime_type`, `size_bytes`, `duration_ms`, `text?`, `language`。無ければ省略）が付く。

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| （アップロード `purpose=character_voice_reference`） | 音声ファイルを置き換える。`create_upload_ticket` で発行した URL に multipart で送る（[mcp-tools.md](mcp-tools.md) の表）。拡張子と中身の署名・10 MB・長さ（ffprobe があるときだけ 3〜10 秒）・言語・書き起こしの長さを画面と同じに検証し、不正は `validation` | `params`: `series_id` か `project_id`, `character_id`, `text?`, `language?` | `{series_id?, project_id?, character_id, voice_reference}` | いいえ |
| `update_character_voice_reference` | 書き起こしと言語だけを変える（渡した項目だけ。録音が未設定なら `validation`、書き起こしは 500 文字以内） | `series_id?` / `project_id?`, `character_id`, `text?`, `language?` | `{series_id?, project_id?, character_id, voice_reference}` | いいえ |
| `delete_character_voice_reference` | 参照音声を削除する（ほかの Episode や正本に複製された音声は残る。未設定なら何もしない） | `series_id?` / `project_id?`, `character_id` | 同上（`voice_reference` は `null`） | いいえ |
| `get_character_voice_reference_url` | 録音のダウンロード URL（15 分有効。試聴用）。未設定なら `not_found` | `series_id?` / `project_id?`, `character_id` | `url`, `expires_at`, `file_name` | はい |

## 声の学習（`/series/:id/characters/:characterId/voice-training`・`/projects/:id/characters/:characterId/voice-training`。Issue #936）

登場人物の声を、手持ちの録音（合計 1 分〜30 分）から GPT-SoVITS で学習（fine-tune）し、できた声を声の合成に使う。Windows ＋ NVIDIA の GPU の PC だけ（macOS は対象外）。意味・段階・決定（再開しない・壊れたときはゼロショットに黙って戻さず止める・バンドルに重みを入れない）は [local-runtime.md §18](../architecture/model-manager/local-runtime.md)。`series_id` と `project_id` はどちらか一方（両方・どちらも無しは `validation`）。**どのツールも、パス・コマンド・スクリプト・重みのファイル名を受けない**。**声のコピーには声の持ち主本人の同意が要る**（実在の人の声を同意なしに学習しない・なりすましに使わない）。mam は同意を確かめず、エージェントが本人の同意を代わりに確認したことにしてはならない（各ツールの説明に同じ注意を入れてある）。

|ツール|説明|入力|出力|readOnly|
|---|---|---|---|---|
| （アップロード `purpose=character_voice_training_clip`） | 学習用の音声を 1 つ足す。`create_upload_ticket` の URL に multipart で送る（[mcp-tools.md](mcp-tools.md) の表）。形式・署名・10 MB は参照音声と同じ、長さは 2〜30 秒（ffprobe が無いと受け付けない）、書き起こしは必須で 500 文字以内、合計 30 分・200 個まで。不正は `validation` | `params`: `series_id` か `project_id`, `character_id`, `text` | `{id, name, text, duration_ms, seconds}` | いいえ |
| `get_character_voice_training` | 状態: 音声の一覧（`clips`）と合計の秒数、いま始められない理由（`problems`。日本語）、限度（`limits`）、エポック数の既定（8 / 15）と上限（25 / 50）、学習済みの声（`trained`）、いまの学習（`current`）、別の登場人物の学習が GPU を使っているか（`gpu_busy`）、同意の注意（`consent`）。**見込み時間は学習の前には出ない**（最初のエポックの後に `get_job` の `progress` に出る） | `series_id?` / `project_id?`, `character_id` | 左のとおり | はい |
| `start_character_voice_training` | 学習のジョブを始める。`job_id` を返す（`get_job` で段階・エポック n/m・経過・残り時間を見る。mam を再起動した後は無い）。参照音声・音声の量・ランタイム（未導入・非対応・重みなし・導入中）・空き容量・同梱の ComfyUI が生成中でないことを**始める前に**確かめ、足りなければ理由つきで断る（`validation` / `conflict` / `disk_full`、ランタイムの断りは共通のエラー）。学習は 1 つずつ。mam を再起動すると途中から再開せずやり直しになる | `series_id?` / `project_id?`, `character_id`, `epochs_sovits?`, `epochs_gpt?` | `{job_id, character}`（`get_job` の `job_id`。学習する登場人物の名前） | いいえ |
| `cancel_character_voice_training` | 動いている学習を中止する（部分的な結果は残さない）。動いていなければ `conflict` | `series_id?` / `project_id?`, `character_id` | `{cancelled}` | いいえ |
| `delete_character_voice_training_clips` | 学習用の音声を 1 つ（`clip_id`）または全部消す（学習済みの声は残る） | `series_id?` / `project_id?`, `character_id`, `clip_id?` | `get_character_voice_training` と同じ形 | いいえ |
| `delete_character_trained_voice` | 学習済みの声を取り除き、参照音声のゼロショットに戻す（重みのファイルも消える。無くても消せる。何も無ければ何もしない） | `series_id?` / `project_id?`, `character_id` | 同上 | いいえ |

学習の `get_job` の種類は `train_character_voice`（メモリ上の追跡。再起動をまたがない）。

## メモ（`/projects/:id/notes`・`/series/:id/notes`）

Project のメモと Series の正本メモは同じ形。Series のメモは Episode の開始時に Project へコピーされる（アーカイブ済みは除く）。アーカイブしたメモは一覧に残るが、どの AI にも渡らない。題は 100 文字以内、本文は 1〜4000 文字、`category` は `character` / `product` / `location` / `world` / `tone` / `other`。

| ツール | 説明 | 入力 | 出力 | readOnly |
|--------|------|------|------|----------|
| `list_project_notes` | Project のメモ（アーカイブ済みを含む）。各メモにバージョン履歴つき | `project_id` | `project_id`, `notes[]`: `id`, `project_id?`, `series_id?`, `origin_id?`, `category`, `pinned`, `archived`, `archived_at?`, `title`, `content`, `version_number`, `source`（`manual` / `chat`）, `created_at`, `updated_at`, `versions[]`（`version_number`, `summary`=題, `created_at`） | はい |
| `list_series_notes` | Series の正本メモ | `series_id` | `series_id`, `notes[]` | はい |
| `create_project_note` | Project にメモを作る。`category` を省略すると AI が分類（モデル未設定なら `other`）。作成後に AI が World State への反映を提案することがある（承認カードでの応答は別のドメイン） | `project_id`, `title?`（省略は本文の 1 行目）, `content`, `category?`, `pinned?` | 作ったメモ（`notes[]` の 1 件と同じ） | いいえ |
| `create_series_note` | Series の正本メモを作る | `series_id`, `title?`, `content`, `category?`, `pinned?` | 同上 | いいえ |
| `update_note` | メモを、渡した項目だけ更新する。題・本文が変わると新しいバージョン。`category` / `pinned` / `archived` はメモだけを更新（`archived=false` でアーカイブを戻す） | `note_id`, `title?`, `content?`, `category?`, `pinned?`, `archived?` | 更新したメモ | いいえ |
| `archive_note` | メモをアーカイブする | `note_id` | 同上 | いいえ |
| `restore_note_version` | 以前のバージョンの題・本文を最新バージョンとして追加する | `note_id`, `version_number`（1 以上） | 同上 | いいえ |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-24 | 新規作成。World State（8）・Reference Library（3）・Branch（4）・バージョン履歴（7）・Series（5）・メモ（7）の 34 ツールと、ジョブ `create_branch` | Issue #830。画面の全操作を MCP ツールにする |
| 1.1 | 2026-11-01 | 「参照音声」の節を足した（アップロード `character_voice_reference`、`update_character_voice_reference`・`delete_character_voice_reference`・`get_character_voice_reference_url`。`get_series`・`get_world_state` の登場人物に `voice_reference`） | Issue #919（#912） |
| 1.2 | 2026-11-03 | World State の表に `create_character`（Project の World State か Series の正本に、名前・髪型・衣装・性格・声と顔・全身の参照画像を渡して登場人物を直接作る。`character_id` を返す）を足した | Issue #1001。登場人物は LLM の企画だけが作っていたため、LLM が止まるとシリーズの正本も空のままで、`assign_character_reference` に渡す `entity_id` も無かった |
| 1.3 | 2026-11-03 | `get_world_state` の `characters[]` に、下書き（Brief の添付画像から LLM なしで作ったもの）だけ付く `draft` を足し、`create_character` の説明に作った登場人物は `draft=false` であることを足した。`create_character` の `branch_id` の拒否文を、実際の規則（`project_id` のときだけ使える）に直した | Issue #1022 |
| 1.4 | 2026-11-04 | 「声の学習」の節（`get_character_voice_training`・`start_character_voice_training`・`cancel_character_voice_training`・`delete_character_voice_training_clips`・`delete_character_trained_voice`、アップロード `character_voice_training_clip`）を足した | Issue #936。GPT-SoVITS の fine-tune の MCP の入口 |
