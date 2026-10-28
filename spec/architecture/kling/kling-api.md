# Kling 3.0 のリクエストと公式ドキュメントの突き合わせ

Issue #838。[model-adapters.md](../model-adapters.md)（§9 の実装）に足す、`klingBackend`（`internal/agent/modeladapter/backends_video.go`）が送る本文を、Kling の公式ドキュメントと突き合わせた記録。`model-adapters.md` は行数が多いため、別ファイルにしている。**実際の API は呼んでいない**（ドキュメントの読み合わせだけ。調査日 2026-10-25）。

## 1. 結論

- **本文と経路は変えない。** 公式の「3.0」のページは新しい本文（`contents` / `settings`）の経路を載せているが、旧経路（`/v1/videos/text2video`・`/v1/videos/image2video`）のページ（各ページの「legacy」）にも `model_name` の値として `kling-v3` が載っており、公式の更新履歴（2026-07-15）に「旧 API は引き続き使え、廃止の予定は今のところない」とある。旧経路が `kling-v3` を受け付けないと読める記述は見つからなかった。ただし旧（legacy）のページは画面を JavaScript で描くため、本文を直接取得できず、検索結果の抜粋で確かめた（§2）。
- **実機では確かめていない。** 最初に確かめるのは**ホスト名**。公式の認証のページは、中国の外のサーバー向けのホストを `api.klingai.com` から `api-singapore.klingai.com` に変えたと書いているが、mam の既定は今も `api.klingai.com`（§3 の 1）。旧経路が今も `kling-v3` を受け付けるか、シード・ネガティブプロンプトの扱い、JWT の認証も**未検証**（§4）。確かめて合わなければ、新しい本文に合わせるかをオーナーが決める（Issue #838 の「やること」。#838 に「決定事項」の節は無い）。

## 2. 公式ドキュメントの形（2026-10-25 時点）

出典: <https://kling.ai/document-api/api/video/3-0-omni/text-to-video>（新）と同 `/legacy`（旧）、同 `image-to-video` と `/legacy`、<https://kling.ai/document-api/api/get-started/authentication>、<https://kling.ai/document-api/updates/api>。新・認証・更新履歴のページは本文を取得して読んだ。旧（`/legacy`）のページは、本文を直接取得できず、検索結果の抜粋で読んだ範囲だけを「確認できた」とし、読めなかった欄はそう書く。

| 項目 | 新（3.0 のページ） | 旧（legacy のページ。mam が使う） |
|------|---------------------|-------------------------------------|
| 作成 | `POST /text-to-video/kling-3.0`、`POST /image-to-video/kling-3.0` | `POST /v1/videos/text2video`、`POST /v1/videos/image2video` |
| モデル | パスの `kling-3.0` | 本文の `model_name`（`kling-v2-5-turbo` / `kling-v2-6` / `kling-v3`） |
| 認証 | API キー 1 つを `Authorization: Bearer <API_KEY>` | AccessKey + SecretKey から作る JWT（HS256）を `Bearer`。認証のページは AK/SK を「legacy 設計向け」と位置づけ、更新履歴（2026-06-17）には API キーだけの認証が既存のモデルと互換とある（**未検証**。実機の確認は §4） |
| 本文（テキスト） | `prompt`（3072 文字まで、推奨 2500）、`settings`（`multi_shot` 既定 true・`audio` `native`/`off`・`resolution` `720p`/`1080p`/`4k`・`aspect_ratio`・`duration` 整数 3〜15）、`options` | `model_name`・`prompt`（2500 文字まで）・`negative_prompt`（2500 文字まで）・`mode`（`std`=720p / `pro`=1080p / `4k`）・`aspect_ratio`・`duration`（文字列。3〜15 は旧ページの本文を再取得できず、検索結果の抜粋では確認できなかった）・`sound`（同じ理由で確認できなかった）・`multi_shot`（既定 false）など |
| 本文（画像） | `contents[]`（`prompt` / `first_frame` / `last_frame` / `element`）。画像は URL か Base64 | `image` / `image_tail`。URL か、`data:` の接頭辞なしの Base64（10MB までは旧ページの本文を再取得できず、検索結果の抜粋では確認できなかった） |
| ネガティブプロンプト | **欄なし**（`prompt` に否定の記述を含められる、とだけある） | `negative_prompt` あり（ドキュメントは「プロンプトの中の否定文で補う」ことも勧める） |
| シード | **欄なし** | **欄なし** |
| 応答 | `data.id`・`data.status`（`submitted` / `processing` / `succeeded` / `failed`） | `data.task_id`・`data.task_status`（`submitted` / `processing` / `succeed` / `failed`） |
| 取得 | `GET /tasks?task_ids=<id>`（結果は `data[].outputs[].url`） | `GET /v1/videos/text2video/{id}`・`/v1/videos/image2video/{id}`（結果は `data.task_result.videos[].url`、失敗の理由は `task_status_msg`） |
| ホスト | `https://api-singapore.klingai.com` | 同左 |

## 3. 現行の実装との差

`klingBackend` は旧経路の形で送り、旧経路の応答の形で読む。旧（legacy）のページと照らして、次の点が違う・確かめられない。

1. **ホスト名**: 既定の `vendors.kling.base_url` は `https://api.klingai.com`。公式の認証のページは「`api.klingai.com` から `api-singapore.klingai.com`（サーバーが中国の外の利用者向け）に変わった」と書いている。旧ホストが今も応答するかはドキュメントからは分からない（**未検証**）。`models.json` の `vendors.kling.base_url` で差し替えられるので、通らなければ設定で直せる。
2. **シード**: どちらの形にも欄がない。`modeladapter.SupportsSeed` は Kling で偽、`klingBackend` もシードを送らない。実装と食い違いはない。
3. **ネガティブプロンプト**: 旧経路では欄がある（mam は常に送る。空文字も送る。公式の例も空文字を送っている）。新経路に移ると欄がなく、`direct.Fold` と同じ「本文へ折り込む」形に変える必要がある。プロンプトの上限は mam が本文 2400 文字・ネガティブ 400 文字（`model-adapters.md` §9.3）で、旧経路の上限（どちらも 2500 文字）の内側にある。
4. **`multi_shot`**: 旧経路の既定は false で、mam は送らない（単一ショット）。新経路の既定は true なので、移るときは `settings.multi_shot` を false にして送る。
5. **長さ・解像度**: 公式は `kling-v3` で 3〜15 秒・`4k` まで受ける。mam は 5 / 10 秒と `std` / `pro`（720p / 1080p）だけを使う。受け付ける範囲の内側で、食い違いはない。
6. **認証**: mam は `ACCESS_KEY:SECRET_KEY` から JWT を作る（旧経路の認証と同じ）。新経路へ移すときは API キー 1 つの `Bearer` に変わり、`credential` の保存形式（`ACCESS_KEY:SECRET_KEY`）の扱いを決める必要がある。

## 4. 未検証（実際の API キーが要る）

| 確かめること | 状態 |
|--------------|------|
| 旧経路 `/v1/videos/text2video`・`/v1/videos/image2video` が `model_name: kling-v3` を受け付け、Take の動画が生成できるか | **未検証** |
| 既定のホスト `https://api.klingai.com` が今も応答するか（`api-singapore.klingai.com` でなければならないか） | **未検証** |
| `negative_prompt` を `kling-v3` が効かせるか（効かないなら本文へ折り込む） | **未検証** |
| AccessKey + SecretKey の JWT で `kling-v3` が通るか（API キー 1 つでなければならないか） | **未検証** |

確かめる手順: Kling の API キーで、`Prepare` 後と同じ本文を最小の長さ（5 秒・720p）で 1 回送る。通らないときの応答の `code` / `message` を控え、新経路の本文（§2 の左の列）へ切り替えるかをオーナーが決める。確かめたら `<日付> <確かめた人>` をこの表に付ける。実機の確認項目は Issue #839 のチェックリストにも載せる。

## 5. Kling 4.0（Issue #873）

`kling-4.0` は新 API で呼ぶ（`kling4Backend`、`internal/agent/modeladapter/backends_kling4.go`）。API の全体は [model-adapters.md](../model-adapters.md) §9.4。ここには公式の形と、Kling 3.0 を移さない理由を残す。**実際の API は呼んでいない**（ドキュメントの読み合わせだけ。調査日 2026-10-28）。

出典: <https://kling.ai/document-api/api/video/4-0/text-to-video>、同 `/image-to-video`、<https://kling.ai/document-api/api/get-started/authentication>、<https://kling.ai/document-api/guides/capability-map/video>、<https://kling.ai/dev/pricing>、<https://kling.ai/document-api/api/get-started/error-codes>。ページは JavaScript で描かれるため、描画したあとの本文を読んだ。

| 項目 | Kling 4.0（公式） | mam の `kling4Backend` |
|------|--------------------|-------------------------|
| ホスト | `https://api-singapore.klingai.com` | 既定の `vendors.kling4.base_url` |
| 認証 | `Authorization: Bearer <API キー>`（キー 1 つ） | `credential.Store` の `kling4` を、そのまま `Bearer` に付ける（JWT は作らない） |
| 作成（テキスト） | `POST /text-to-video/kling-4.0`。`prompt`（英語だけなら 26,000 文字、日本語など英語以外だけなら 11,000 文字、混在は E/26000 + N/11000 ≤ 1）、`settings`（`resolution` `720p`/`1080p` 既定 1080p・`aspect_ratio` `21:9`/`16:9`/`9:16`/`1:1` 既定 16:9・`duration` 3〜30 の整数 既定 10）、`options` | `prompt` と `settings` の 3 項目（常に `duration`・`resolution`・`aspect_ratio` を送る）。`options`（コールバック・外部 ID・透かし）は送らない |
| 作成（画像） | `POST /image-to-video/kling-4.0`。`contents[]`（`prompt` / `first_frame` / `middle_frame` / `last_frame`、画像は URL か Base64、`.jpg` `.jpeg` `.png`・30 MB 以下・300px 以上・縦横比 2:5〜5:2・最大 10 枚）、`settings`（`resolution`・`duration`。縦横比の項目は無い） | `prompt`・`first_frame`・（あれば）`last_frame`。画像は Base64（`data:` の接頭辞なし。**書式は未検証**）。`middle_frame` は使わない |
| 応答（作成） | `code`・`message`・`request_id`・`data.id`・`data.status`（`submitted` / `processing` / `succeeded` / `failed`） | `data.id` を読む |
| 取得 | `GET /tasks?task_ids=<id>`。`data[].status`・`message`（失敗の理由）・`outputs[]`（`type: video` の `url`。30 日保持、ホットリンク防止） | `data[0]` を読み、`outputs[]` の最初の `type: video` の `url` をダウンロードする |
| エラーコード | HTTP とは別の `code`（1302 / 1303 = 429、1301 = 内容の安全性、5000〜5002 = 内部） | Kling 3.0 と同じ表（`klingCodeError`） |
| 単価 | 料金ページの「Coming Soon」の Kling 4.0（映像入力なし）: 720p 1.0 unit/秒、1080p 1.5 units/秒。1 unit = 0.14 USD（list price）。「プレビュー用で、提供前は使えない。公式の提供日と価格は別途告知」 | `720p: 0.14`・`1080p: 0.21`（USD/秒）。**正式価格が出たら見直す** |
| 提供状況 | 公式のドキュメントは「Early Access」。「2026-10-28 23:00（北京時間）に全面提供」と案内 | 実機で通るかは未検証（§5.2） |

### 5.1 Kling 3.0 を新ホストへ移さない理由

Kling 3.0（`kling-3.0`、API の `kling-v3`）は、旧 API の経路（`api.klingai.com`・`/v1/videos/text2video`・JWT）のまま変えない。理由は、**新ホストと API キーで `kling-v3` が通るか**が実機でしか確かめられず（§4 の表の 3 つの項目）、確かめずに移すと既存の Take 生成が壊れるおそれがあるため。Kling 4.0 は別のベンダー `kling4` にして、キー（`kling` は ACCESS_KEY:SECRET_KEY、`kling4` は API キー）・ベース URL・同時実行・ブレーカーを混ぜない。公式の認証のページは API キーを「すべてのモデル」に使えるとし、AccessKey / SecretKey を「旧設計向け」とするので、実機で `kling-v3` が API キーで通ると分かれば、`kling-3.0` を `kling4` のホスト・キーへ移して `kling` を無くせる（そのときこの節と `model-adapters.md` §9.4 を更新する）。

### 5.2 未検証（実際の API キーが要る）

| 確かめること | 状態 |
|--------------|------|
| `kling-4.0` の create / `GET /tasks` が、公式の例どおりの形で返り、Take の動画が生成できるか（テキスト・First Frame） | **未検証** |
| 画像の Base64 に `data:` の接頭辞が要るか | **未検証** |
| Early Access のあいだ、Kling 4.0 が API キーで呼べるか（提供日: 2026-10-28 23:00 北京時間） | **未検証** |
| 単価（0.14 / 0.21 USD/秒）が正式な価格・実際の請求に合うか | **未検証** |
| `kling-3.0`（`kling-v3`）が新ホスト `api-singapore.klingai.com` と API キー 1 つで通るか（通れば §5.1 のとおり移せる） | **未検証**（§4 と同じ） |
| Kling 4.0 のネイティブ音声の有無（`NativeAudio` は偽にしてある） | **未確認** |

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成。公式ドキュメントの新旧の形と、`klingBackend` との差、未検証の項目を記録 | Issue #838。コードは変えず、実機で確かめるまでの状態を残すため |
| 1.1 | 2026-10-25 | 認証の行の出典を直し（認証のページと更新履歴を分けた）、旧ページで確認できなかった項目を明記、ホスト名を最初の確認事項に、「決定」の言い方を直した | 独立レビュー（PR #849）の指摘。過大な言い方の訂正 |
| 1.2 | 2026-10-28 | §5（Kling 4.0 の公式の形・`kling4Backend` との対応・Kling 3.0 を新ホストへ移さない理由・未検証）を足した | Issue #873 |
