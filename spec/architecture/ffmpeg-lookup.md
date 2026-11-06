# ffmpeg / ffprobe の探し方

> **状態: 実装済み**（Issue #856・#900）。実装は `backend/internal/media/locate`（探し方）・`media.Look` / `LookWith` / `LookIn`（`Tools` を作る）・`service/ffmpegsettings`（設定ファイル）。画面は [../components/settings-screen-categories.md](../components/settings-screen-categories.md) §8、ルートは [../api/endpoints-settings.md](../api/endpoints-settings.md) §1・§5.2。

Finder や Dock から起動した macOS のアプリは、`launchd` の `PATH`（`/usr/bin:/bin:/usr/sbin:/sbin`）で動く。Homebrew の `brew install ffmpeg` の入れ先（Apple Silicon は `/opt/homebrew/bin`、Intel は `/usr/local/bin`）はそこに無く、`exec.LookPath` だけでは MAM.app が ffmpeg を見つけられなかった（Issue #856）。

## 1. 方針（オーナー決定 2026-11-01）

1. **設定でパスを指定できる**。指定があれば、それを優先し、探さない。
2. 指定が無いとき、次の場所を**この順に**探す（`locate.SearchDirs`）。
   1. 環境変数 `PATH` の各ディレクトリ（`PATH` の並び順）
   2. macOS（`GOOS=darwin`）だけ: Homebrew の既定の場所 `/opt/homebrew/bin`、`/usr/local/bin`
   3. 追加のディレクトリ（`locate.Finder.Extra`。mam が自分で置く場所用）。**mam がダウンロードして入れた ffmpeg の `bin/`**（#900。[ffmpeg-download.md](ffmpeg-download.md)）がここに入る。導入の記録があり、ファイルがそろっているときだけで、**最後**に探すので、利用者が自分で入れた ffmpeg（`PATH`・Homebrew）が必ず勝つ
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
- **反映は、保存・ダウンロード・削除のすぐあと（再起動は要らない。Issue #1047）**。アプリは `media.Tools` を **1 つ**だけ作って各サービス（Judge・Take のプレビューと部分採用・Export・音声・口パク・参照音声と学習の長さの確認・動画の高画質化）に渡し（`internal/app` の `buildServices`）、そこに保存したパス／探して見つけた ffmpeg を入れ直す（`Tools.Reload`。`internal/app` の `reloadFFmpeg`）。`Tools` のコピーは中身を共有するので、渡したあとの入れ替えを全サービスが見る。入れ直すのは次のとき: ① 起動時 ② パスの保存（`ffmpegsettings.WithOnSet`）③ ダウンロードの成功・削除（`ffmpegdl.Service.SetOnChange`。画面に「入れました」を出す**前**に入れ直す）④ 設定画面・概要のカードが表示する探索の結果（`View`）が、サービスの持つものと違うとき（`ffmpegsettings.WithOnView`。PATH に後から入れた ffmpeg など。**画面の「使えます」は、サービスがいま動かせるものと一致する**）。起動時に見つからなくても `Tools` は空のまま渡り、あとで入れた ffmpeg を動かす。見つからない間の各機能は、ffmpeg が無いときと同じ（プレビューは原本・部分採用・Judge・Export・音声は使えない。`Tools.Available()` が偽。Judge は `judge.Usable`、部分採用は `repair.Splicer.CanSplice`、生成の Judge は `generation.Deps.JudgeOn`、口パクの ffmpeg は `musetalk.Config.FFmpeg` で、そのつど見る）。実行中の ffmpeg は入れ替えの影響を受けない（呼び出しは始まるときに 1 回だけ中身を読む）。初回ガイドの ffmpeg の確認（`service/onboarding.Service.FFmpeg`）は、保存したパスと同じ探し方で毎回読むので、保存の直後に「見つかりました」になる。
- **初回ガイドとアプリは同じ答えを返す**: 壊れた `ffmpeg.json` は、`Look`（起動時の探索）も `Find`（初回ガイドの確認）も `media.ErrUnavailable` にする（`Find` が設定を無視して探し直すことはしない）。ガイドは「見つかりません」を出し、アプリは ffmpeg が無い扱いで起動する。
- ローカルの口パク（MuseTalk。[model-manager/local-runtime.md §13](model-manager/local-runtime.md)）は ffmpeg を持たない。呼び出し側（Audio Director の口パクプロバイダー）が、この探索の結果（`media.Tools.FFmpeg`）を `lipsync.Request.FFmpegPath` に入れて渡し、ランタイムのラッパーはそれを `-nostdin` つきで動かす。見つからない・使えないときは、ランタイムを起動する前に呼び出し側が止める。
- 探し方の使い手: `media.Look()`（設定なし。テストと `testenv`）、`media.LookWith(locate.Config)`、`media.LookIn(locate.Finder, locate.Config)`（Finder を差し替えられる）。アプリは `ffmpegsettings.Service.Look()`（保存したパスつき）を使う。Judge（`judge.NewFFmpeg`）・Take のプレビュー・MCP のツール・各サービスは、見つけた `media.Tools` を受け取り、名前で `exec` しない（子プロセスは `Tools` の絶対パスで起動する）。

## 4. テスト

- 探す場所の一覧・優先順・エラー: `media/locate/locate_test.go`（`goos=darwin` と `PATH` に ffmpeg が無い状況で、Homebrew の 2 つを探して見つけること、`PATH` が先、設定が最優先、使えない設定はエラーで探し直さない）。Linux で動く。
- `media.Tools` の差し替え（`Reload`・全コピーが見る・見つからない間は `ErrUnavailable`・競合しない）: `media/look_test.go`。アプリ全体で、保存・画面の表示のあとに全サービスが新しい ffmpeg を動かすこと: `app/ffmpeg_reload_test.go`。ダウンロードの成功・削除の通知: `ffmpegdl/service_test.go`。
- `media.LookIn`: `media/look_test.go`。設定ファイル: `service/ffmpegsettings`。画面と保存: `web/ffmpegscreen/settings_test.go`。
- Finder から起動した実際の挙動は macOS の実機でしか確かめられない（Issue #856 の「実機で確かめること」）。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-01 | 新規作成 | Issue #856。Finder から起動した MAM.app が Homebrew の ffmpeg を見つけられなかった。`PATH` と Homebrew の既定の場所を探し、設定したパスを優先する |
| 1.1 | 2026-11-01 | 追加のディレクトリ（`Finder.Extra`）に、ダウンロードして入れた ffmpeg の `bin/` を最後に足した（`ffmpegsettings.WithManagedDirs`。呼ぶたびに読む）。設定したパス ＞ `PATH` ＞ Homebrew ＞ 入れたもの | Issue #900 |
| 1.2 | 2026-11-01 | ローカルの口パク（MuseTalk）が、この探索の結果を要求で受け取って使うことを §3 に足した（ランタイムは ffmpeg を同梱しない）。探し方は変えていない | Issue #921 |
| 1.3 | 2026-11-06 | 壊れた `ffmpeg.json` のとき、初回ガイドの確認（`ffmpegsettings.Service.Find`）も `Look` と同じく `media.ErrUnavailable` を返す（以前は設定を無視して探していたので、初回ガイドが「見つかりました」でもアプリは ffmpeg 無しで起動した）。§3 | Issue #1055 |
| 1.4 | 2026-11-06 | §3「反映」を、次の起動から → 保存・ダウンロード・削除のすぐあとに改めた（`media.Tools` を差し替え可能にし、各サービスに同じものを渡す。画面の「使えます」はサービスが動かせるものと一致する）。実行中の呼び出しは壊さない | Issue #1047 |
