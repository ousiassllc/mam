# Kling 3.0 のリクエストと公式ドキュメントの突き合わせ

Issue #838。[model-adapters.md](../model-adapters.md)（§9 の実装）に足す、`klingBackend`（`internal/agent/modeladapter/backends_video.go`）が送る本文を、Kling の公式ドキュメントと突き合わせた記録。`model-adapters.md` は行数が多いため、別ファイルにしている。**実際の API は呼んでいない**（ドキュメントの読み合わせだけ。調査日 2026-10-25）。

## 1. 結論

- **本文と経路は変えない。** 公式の「3.0」のページは新しい本文（`contents` / `settings`）の経路を載せているが、旧経路（`/v1/videos/text2video`・`/v1/videos/image2video`）のページ（各ページの「legacy」）にも `model_name` の値として `kling-v3` が載っており、公式の更新履歴（2026-07-15）に「旧 API は引き続き使え、廃止の予定は今のところない」とある。旧経路が `kling-v3` を受け付けないと読める記述は見つからなかった。
- ただし**実機では確かめていない**。旧経路が今も `kling-v3` を受け付けるか、ホスト名（§3 の 1）、シード・ネガティブプロンプトの扱いは**未検証**（§4）。確かめて合わなければ、新しい本文に合わせる（Issue #838 の決定）。

## 2. 公式ドキュメントの形（2026-10-25 時点）

出典: <https://kling.ai/document-api/api/video/3-0-omni/text-to-video>（新）と同 `/legacy`（旧）、同 `image-to-video` と `/legacy`、<https://kling.ai/document-api/api/get-started/authentication>、<https://kling.ai/document-api/updates/api>。

| 項目 | 新（3.0 のページ） | 旧（legacy のページ。mam が使う） |
|------|---------------------|-------------------------------------|
| 作成 | `POST /text-to-video/kling-3.0`、`POST /image-to-video/kling-3.0` | `POST /v1/videos/text2video`、`POST /v1/videos/image2video` |
| モデル | パスの `kling-3.0` | 本文の `model_name`（`kling-v2-5-turbo` / `kling-v2-6` / `kling-v3`） |
| 認証 | API キー 1 つを `Authorization: Bearer <API_KEY>` | AccessKey + SecretKey から作る JWT（HS256）を `Bearer`。同じページに「API キーだけの認証は既存のモデルでも使える」との記述がある（更新履歴 2026-06-17） |
| 本文（テキスト） | `prompt`（3072 文字まで、推奨 2500）、`settings`（`multi_shot` 既定 true・`audio` `native`/`off`・`resolution` `720p`/`1080p`/`4k`・`aspect_ratio`・`duration` 整数 3〜15）、`options` | `model_name`・`prompt`（2500 文字まで）・`negative_prompt`（2500 文字まで）・`mode`（`std`=720p / `pro`=1080p / `4k`）・`aspect_ratio`・`duration`（文字列 3〜15）・`sound`・`multi_shot`（既定 false）など |
| 本文（画像） | `contents[]`（`prompt` / `first_frame` / `last_frame` / `element`）。画像は URL か Base64 | `image` / `image_tail`。URL か、`data:` の接頭辞なしの Base64（10MB まで） |
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

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-10-25 | 新規作成。公式ドキュメントの新旧の形と、`klingBackend` との差、未検証の項目を記録 | Issue #838。コードは変えず、実機で確かめるまでの状態を残すため |
