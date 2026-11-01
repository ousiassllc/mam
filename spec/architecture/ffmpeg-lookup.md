# ffmpeg / ffprobe の探し方

> **状態: 実装済み**（Issue #856）。実装は `backend/internal/media/locate`（探し方）・`media.Look` / `LookWith` / `LookIn`（`Tools` を作る）・`service/ffmpegsettings`（設定ファイル）。画面は [../components/settings-screen-categories.md](../components/settings-screen-categories.md) §8、ルートは [../api/endpoints-settings.md](../api/endpoints-settings.md) §1・§5.2。

Finder や Dock から起動した macOS のアプリは、`launchd` の `PATH`（`/usr/bin:/bin:/usr/sbin:/sbin`）で動く。Homebrew の `brew install ffmpeg` の入れ先（Apple Silicon は `/opt/homebrew/bin`、Intel は `/usr/local/bin`）はそこに無く、`exec.LookPath` だけでは MAM.app が ffmpeg を見つけられなかった（Issue #856）。

## 1. 方針（オーナー決定 2026-11-01）

1. **設定でパスを指定できる**。指定があれば、それを優先し、探さない。
2. 指定が無いとき、次の場所を**この順に**探す（`locate.SearchDirs`）。
   1. 環境変数 `PATH` の各ディレクトリ（`PATH` の並び順）
   2. macOS（`GOOS=darwin`）だけ: Homebrew の既定の場所 `/opt/homebrew/bin`、`/usr/local/bin`
   3. 追加のディレクトリ（`locate.Finder.Extra`。mam が自分で置く場所用。今は空で、ffmpeg の自動取得（#900）がここに取得先を足す）
3. **ログインシェルの `PATH` は読まない**（シェルの設定ファイルの実行を伴い、遅く、環境ごとに挙動が違うため）。

`SearchDirs` は OS に依存しない純関数で、環境変数もディスクも読まない（`goos`・`PATH` の値・追加のディレクトリを引数にとる）。空・相対パスの項目と重複は除く（相対パスは作業ディレクトリに結果が左右されるため）。Windows は `;` 区切りで `"` を外し、ファイル名に `.exe` を付ける。

「使える」とは、通常のファイル（リンクは辿る）で、実行ビットがある（Windows は無条件）こと。実行はしない（`locate.IsExecutable`）。

## 2. 設定したパスが使えないとき

**エラーにする。探し直しに切り替えない**。ユーザーが選んだ ffmpeg とは別のものを黙って動かさないため。

- 設定画面の保存（`PUT /settings/ffmpeg`）: 絶対パスでない・無い・実行できないパスは **422** で、書かない。理由（`locate.PathError`。`ErrNotAbsolute` / `ErrNotExecutable`）と入力を画面に残す。
- 保存したあとにファイルが消えたとき: 起動時の探索が `*locate.PathError` を含む `media.ErrUnavailable` を返し、ログに指定したパスを出す。ffmpeg は無いものとして起動する（Take のプレビュー・Partial Take Use・Judge が使えない。これは ffmpeg が無いときと同じ）。設定画面は「見つかりません」と理由を出す。
- どこにも無いとき: `locate.ErrNotFound` を含む `media.ErrUnavailable`。メッセージに探した場所を並べる。

ffmpeg と ffprobe は別々に指定でき、片方だけ指定したときのもう片方は探索に任せる。

## 3. 設定ファイルと反映

- 保存先: データフォルダの `ffmpeg.json`（`{"ffmpeg": "/abs/ffmpeg", "ffprobe": "/abs/ffprobe"}`。指定の無いキーは書かない。両方空にするとファイルを消す。ファイルにある未知のキーは保存で消さない）。壊れたファイルは、設定画面では分類の問題として出し（ほかの分類は使える）、起動時の探索では ffmpeg が無い扱いにしてログに理由を出す。
- **反映は次の起動から**。mam は起動時に一度だけ探して `media.Tools` を各サービスに渡す（`internal/app` の `buildServices`）ため、保存では動いているサービスのパスを替えない。初回ガイドの ffmpeg の確認（`service/onboarding.Service.FFmpeg`）は、保存したパスと同じ探し方で毎回読むので、保存の直後に「見つかりました」になる。
- 探し方の使い手: `media.Look()`（設定なし。テストと `testenv`）、`media.LookWith(locate.Config)`、`media.LookIn(locate.Finder, locate.Config)`（Finder を差し替えられる）。アプリは `ffmpegsettings.Service.Look()`（保存したパスつき）を使う。Judge（`judge.NewFFmpeg`）・Take のプレビュー・MCP のツール・各サービスは、見つけた `media.Tools` を受け取り、名前で `exec` しない（子プロセスは `Tools` の絶対パスで起動する）。

## 4. テスト

- 探す場所の一覧・優先順・エラー: `media/locate/locate_test.go`（`goos=darwin` と `PATH` に ffmpeg が無い状況で、Homebrew の 2 つを探して見つけること、`PATH` が先、設定が最優先、使えない設定はエラーで探し直さない）。Linux で動く。
- `media.LookIn`: `media/look_test.go`。設定ファイル: `service/ffmpegsettings`。画面と保存: `web/ffmpeg_settings_test.go`。
- Finder から起動した実際の挙動は macOS の実機でしか確かめられない（Issue #856 の「実機で確かめること」）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-01 | 新規作成 | Issue #856。Finder から起動した MAM.app が Homebrew の ffmpeg を見つけられなかった。`PATH` と Homebrew の既定の場所を探し、設定したパスを優先する |
