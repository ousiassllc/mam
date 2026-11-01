# 画面仕様: 設定画面の「第三者のライセンス」

> **状態: 実装済み。** Issue #910（Windows 版の顔の同一性の数値評価が使う ONNX Runtime の DLL を同梱するので、そのライセンス表記をアプリの中でも見られるようにする）。オーナー決定（2026-11-02）: `THIRD_PARTY_NOTICES.md` の内容を、設定画面の新しい分類「第三者のライセンス」に出す。GPT-SoVITS・MuseTalk（Issue #912）の出典・ライセンスは、この画面に**あとから、データの追加だけで**足せる作りにする（#912 の中身はこの Issue の範囲外）。

画面の枠・ナビ・概要は [settings-screen.md](settings-screen.md)、ルートの契約は [../api/endpoints-settings.md](../api/endpoints-settings.md) §5.4、見た目の決まりは [shared-ui-parts.md](shared-ui-parts.md)・[color-rules.md](color-rules.md)（#867）、画面の文字を減らす方針は #906（[gentest-screen.md](gentest-screen.md)）に従う。

## 1. 画面

- URL は `GET /settings/licenses`（スラッグ `licenses`。ナビの最後、「バージョンとアップデート」のあと。常に出す）。タイトルは「設定: 第三者のライセンス」、`h2` は「第三者のライセンス」。
- **画面に出す文字は、グループの見出し（件数つき）と、項目の題だけ。** 説明は `h2` の横の「?」ヘルプ（`help.Text(licenses.Description)`。[shared-ui-parts.md](shared-ui-parts.md) §9）に入れる。新しい色のクラス・部品は足さない（面は `bg-neutral-900`・枠は `border-neutral-800`・文字は `text-neutral-100` / `-300` / `-400`、フォーカスの輪は `ring-indigo-400`。いずれも [color-rules.md](color-rules.md) §3 の表にある）。
- 1 項目 = 1 つの `<details>`（閉じた状態で並ぶ）。`<summary>` が題。開くと、その項目の全文を `<pre>`（プレーンテキスト。`whitespace-pre-wrap break-words`。長いときは `max-h-[60vh]` で縦にスクロール）で出す。
- **全文はページに入れない。** ページ（`GET /settings/licenses`）は題だけを持ち、項目を初めて開いたときに（`hx-trigger="toggle once"`）`GET /settings/licenses/:id` が全文の断片を返す。`THIRD_PARTY_NOTICES.md` は約 13000 行・約 650 KB なので、1 つの DOM にしない。読み込み中は「読み込み中…」を出す。
- 全文は**プレーンテキスト**。`{ body }`（templ のエスケープ）でだけ出し、HTML としては読まない（`<script>` を含む文字列も文字のまま出る）。`<pre>` は `tabindex="0"`・`role="region"`・`aria-label="<題>のライセンス文"` を持ち、キーボードでスクロールできる。
- 375px の幅で横にあふれない: 項目・グループ・`<pre>` に `min-w-0`、題と本文に `break-words`、`<pre>` は折り返す。

### 1.1 グループと項目（データの形）

画面は `internal/licenses.Catalog` の `Groups()` を描くだけで、どのグループ・項目があるかを**知らない**。

| 型 | 中身 |
|---|---|
| `licenses.Entry` | `Group`（見出し。空は「その他」）・`Title`（題）・`Body`（プレーンテキストの全文） |
| `licenses.Source` | `func() []Entry`。項目の出どころ |
| `licenses.Catalog` | Source を順に読み、グループ（最初に現れた順）にまとめ、項目に ID（1 から始まる通し番号）を付ける。`Groups()`・`Find(id)`・`Len()` |

出どころは今 1 つ: **`licenses.ThirdPartyNotices`**（`THIRD_PARTY_NOTICES.md` の埋め込みを項目に分ける。§2）。

| グループ | 項目 |
|---|---|
| 概要 | 「このページについて」（ファイルの先頭の、何を載せているかの説明） |
| Go 標準ライブラリ・Go モジュール・npm パッケージ・同梱した素材 | 1 コンポーネント = 1 項目（題は `<名前> <版>`。全文はそのコンポーネントの `LICENSE` などのファイルの写し）。ONNX Runtime（`onnxruntime.dll`）・Lucide は「同梱した素材」 |
| 同梱の補足・利用者が取得するもの | 手で書いた節 1 つ = 1 項目（WebView2 のブートストラッパー、実行時に取得するモデル、ComfyUI、**GPT-SoVITS**、**MuseTalk 1.5**、GPT-SoVITS と MuseTalk の重み・依存ファイル、ffmpeg、Codex） |

## 2. `THIRD_PARTY_NOTICES.md` の埋め込み（単一の出どころ）

- ファイルは今までどおり**リポジトリ直下**に置き、`scripts/third-party-notices.sh`（と `scripts/third-party-notices-*.md`）が生成する。インストーラー・`.app` が配るのもこのファイル。**コピーは作らない。**
- Go の `//go:embed` は親のディレクトリを指せないので、**リポジトリ直下に Go パッケージ `mam`（`notices.go`。`github.com/ousiassllc/mam`）を置き**、`//go:embed THIRD_PARTY_NOTICES.md` で `mam.ThirdPartyNotices` を公開する。生成物のコピー（`backend/internal/.../THIRD_PARTY_NOTICES.md`）を同期させる案は、同期のずれを防ぐテストと make の手順が増えるので採らない。ファイルが 1 つなら、ずれようがない。
- `licenses.ThirdPartyNotices` が、埋め込んだ文字列を `## ` の見出しごとに分ける（`licenses.ParseNotices`）。`~~~` の柵（`fence_for` が全文より長い列を選ぶ）の中の `## ` は全文の一部として読む。「## 一覧」の表は、画面がグループで一覧するので項目にしない。見出しの次の行が生成スクリプトの種別（`kind_label`: Go 標準ライブラリ・Go モジュール・npm パッケージ・同梱した素材）なら、それがグループ。種別の行が無い節は「同梱の補足・利用者が取得するもの」。**ファイルに節が増えれば、項目は増える（コードは変えない）。**
- ずれを防ぐテスト:
  - `TestEmbeddedNotices_AreTheFileOfTheRepositoryRoot`（`internal/licenses`）: 埋め込んだ文字列がリポジトリ直下のファイルと 1 バイトも違わない（ファイルを置き換えたり、埋め込みをコピーに向けたりすると落ちる）。
  - `TestThirdPartyNotices_EveryHeadingOfTheFileIsAnEntryWithItsText`: ファイルの `## ` の見出しが、順に 1 つずつ項目になり、どの項目にも全文がある（GPT-SoVITS・MuseTalk・ONNX Runtime の項目を含む）。
  - `make check-notices`（`scripts/check-third-party-notices.sh`。CI の `ci` ジョブが `make test` の前に実行する）: 生成スクリプトを今動かした出力が、コミットされた `THIRD_PARTY_NOTICES.md` と 1 バイトも違わない。`go.mod`・`static/package-lock.json`・ONNX Runtime の文書・`scripts/third-party-notices-*.md` を変えて、ファイルを作り直し忘れると落ちる。テストは `scripts/check-third-party-notices_test.sh`（`make test-scripts`）。

## 3. GPT-SoVITS・MuseTalk（#912）の出典・ライセンスをあとから足す方法

この画面に足す**ものは、データだけ**（画面のコード・テンプレートは変えない）。

- 出典・ライセンスが `THIRD_PARTY_NOTICES.md` の節であれば、生成スクリプトが書く節（`scripts/third-party-notices.sh` の here-document、または `scripts/third-party-notices-musetalk.md` のようなファイル）を足してファイルを作り直す。節は自動で項目になる。**GPT-SoVITS・MuseTalk の節は、#918・#921・#920 の分がすでにファイルにあり、この画面に項目として出ている**（#912 の範囲の新しい内容は足していない）。
- 節にならないデータ（モデルマネージャーが持つ、重みごとの出典・ライセンス・商用利用の可否。`backend/internal/modelmgr/catalog/data/assets/`。#920）をこの画面にも出すなら、`licenses.Source` を 1 つ書き（カタログを読んで `[]licenses.Entry{Group: "ローカル音声・口パク", Title: "<重みの名前>", Body: "<出典・ライセンス>"}` を返す）、`internal/licenses/licenses.go` の `sources` に足す。グループ・項目・全文のルートは Catalog が作る。出どころを 1 つ足すだけで、`GET /settings/licenses` の一覧にグループが増え、`GET /settings/licenses/:id` が全文を返す。
- 項目の順は、Source の順（`sources`）と、Source の中の順。グループは最初に現れた順。

## 4. 概要のカード

概要（`/settings`）に「第三者のライセンス」のカードを足す（`atoms.IconPackage`・説明は 1 行「MAM が使っているソフトウェアのライセンス文。」・状態は「<項目の数> 件」。[settings-screen.md](settings-screen.md) §5）。

## 5. 配置

| 場所 | 中身 |
|---|---|
| `notices.go`（リポジトリ直下。パッケージ `mam`） | `//go:embed THIRD_PARTY_NOTICES.md` |
| `backend/internal/licenses/` | `Entry`・`Source`・`Catalog`（`licenses.go`）、`THIRD_PARTY_NOTICES.md` の分け方（`notices.go`） |
| `backend/internal/web/handler/settings/licenses/` | ページ用のデータ（`SectionData`）と `GET /settings/licenses/:id`（`Text`） |
| `backend/internal/web/organisms/settings/licenses/` | `Section`（グループと項目の `<details>`）・`Text`（全文の `<pre>`）・`Description`（「?」の説明） |
| `backend/internal/web/pages/settings_categories.templ` | `SettingsLicensesBody` |
| `backend/internal/web/handler/settingspage/` | 分類の登録（`categories`）・概要のカード・`Services.Licenses`（テストが別のカタログを渡す。nil は `licenses.Default()`） |
| `backend/internal/web/paths/` | `SettingsLicenses`（`"licenses"`）・`LicenseText(id)` |

## 6. テスト

- `internal/licenses`: `ParseNotices`（見出しの分割・柵の中の `## ` ・CRLF・種別の行がグループ・一覧の表を除く）、`Catalog`（グループの順・ID・`Find` の未知の ID）、埋め込みとリポジトリ直下のファイルの一致、実ファイルのすべての見出しが項目になること。
- `handler/settingspage`: `GET /settings/licenses` が 200 でグループと全項目の題を持ち、全文を持たない（ページが 200 KB を超えない）、概要にカード、ナビに載る、HTMX でないときの `GET /settings/licenses/:id` はページ全体、未知の ID は 404。
- `handler/settings/licenses`: 別の Source（`<script>` を含む題・本文）を渡しても、グループと項目が描かれ、題も全文もエスケープされる。
- `a11y`（色の許可リスト・コントラスト）と `help_moved_test`・`headings_test` は、既存のテストがこの分類にも掛かる。

## 改訂履歴

| 版 | 日付 | 変更 | 根拠 |
|----|------|------|------|
| 0.1 | 2026-11-02 | 新規。設定画面の分類「第三者のライセンス」（`THIRD_PARTY_NOTICES.md` を埋め込んで項目ごとに出す。GPT-SoVITS・MuseTalk の項目をあとからデータで足せる作り） | Issue #910（オーナー決定 2026-11-02） |
