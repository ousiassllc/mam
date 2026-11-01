# ffmpeg / ffprobe のダウンロード

> **状態: 実装済み**（Issue #900）。実装は `backend/internal/media/ffmpegdl`（カタログ・取得・導入・状態）、共通の安全な展開は `backend/internal/exeinstall`、探す場所への組み込みは `service/ffmpegsettings`（`WithManagedDirs`）と `internal/app/ffmpeg.go`。画面は [../components/settings-screen-categories.md](../components/settings-screen-categories.md) §8 と [../components/onboarding-screen.md](../components/onboarding-screen.md) §4.3、ルートは [../api/endpoints-settings.md](../api/endpoints-settings.md) §5.2。探し方の全体は [ffmpeg-lookup.md](ffmpeg-lookup.md)。

ffmpeg と ffprobe が見つからないとき、利用者が**ボタンを押すと**、mam が固定した版を配布元からダウンロードして入れる。対象は Windows x64 と macOS（Apple Silicon）だけ。それ以外（Linux・Windows arm64・macOS の Intel）は、これまでどおり自分で入れる（案内は変えない）。**mam のインストーラー・ポータブル版・リリースには ffmpeg のバイトを一切含めない**（Codex・ComfyUI と同じ。[chatgpt-signin.md](chatgpt-signin.md) §8）。

## 1. 決定（Issue #900。委ねられた事項）

| 項目 | 決定 | 理由 |
|------|------|------|
| 取得元 | Windows x64: gyan.dev の `essentials_build` 9.0.2（GitHub Releases `GyanD/codexffmpeg` の固定タグ）。macOS arm64: martin-riedl.de の release 9.0.2（ffmpeg と ffprobe の 2 つの zip） | §2 |
| 版の固定 | 配布元の**固定の版の URL**（`latest` は使わない）と、各アーカイブ・各実行ファイルの SHA-256 とサイズを `ffmpegdl/data/catalog.json` に固定する | 取得したものが、レビューしたものと同じだと確かめるため |
| 検証 | アーカイブの SHA-256・サイズ → 展開した 2 つの実行ファイルの SHA-256・サイズ → 実行確認（`-version`）の**すべてが通ったときだけ**入れる。一致しなければ消して、使わない | §4 |
| ライセンス | どちらも **GPL v3 以降**のビルド。mam は ffmpeg を別のプロセスとして起動するだけで、リンクも同梱もしない | §3 |
| 更新 | mam のリリースが固定する版を上げる。利用者がもう一度ボタンを押すと、新しい版に入れ替わる。**自動更新も、バックグラウンドの通信も無い**。通信は利用者がボタンを押したときだけ | §5 |
| 優先順位 | 設定したパス ＞ `PATH` ＞ Homebrew の場所 ＞ **ダウンロードして入れたもの**。利用者が自分で入れた ffmpeg が、mam が入れたものに必ず勝つ | §6 |
| 置き場所 | Windows: `mam.exe` と同じ場所の `ffmpeg` ディレクトリ（アンインストールで消える）。macOS: データディレクトリの `ffmpeg`（`MAM.app` の外。自動更新で `.app` を入れ替えても消えない） | §6 |

## 2. 取得元

| | Windows x64 | macOS arm64（Apple Silicon） |
|--|------|------|
| 配布元 | gyan.dev（Gyan Doshi）。`winget install Gyan.FFmpeg` が使うのと同じビルド | Martin Riedl（martin-riedl.de の ffmpeg ビルドサーバー） |
| ビルド | `essentials_build` 9.0.2（静的・64 ビット）。ソース: <https://github.com/FFmpeg/FFmpeg/commit/946fcce07b> | release 9.0.2（静的・arm64）。ソース: <https://ffmpeg.org/releases/ffmpeg-9.0.2.tar.xz> |
| URL | `https://github.com/GyanD/codexffmpeg/releases/download/9.0.2/ffmpeg-9.0.2-essentials_build.zip` | `https://ffmpeg.martin-riedl.de/download/macos/arm64/1789931890_9.0.2/ffmpeg.zip` と `…/ffprobe.zip`（ディレクトリ名のタイムスタンプ `1789931890` が版ごとに固定） |
| アーカイブの SHA-256 | `60f467265b1e312373dbcd92200c2618a74850f98d3d078e94296bb3fa2047ba`（114,768,076 バイト） | ffmpeg: `c8ed4c4e6978a03c485edbfe4e0a5dc2380f8a30bba5150531b31b094492d924`（28,395,699）／ ffprobe: `fcbe839537485eaee7a7a8bc5cbc0f90d53617e80943e8a5b2e31cb851197ea6`（28,317,701） |
| 中の実行ファイル | `ffmpeg-9.0.2-essentials_build/bin/ffmpeg.exe`（105,423,872 バイト、SHA-256 `3256173f…8edec`）と `…/bin/ffprobe.exe`（105,221,120、`f0d36ecb…65c52`）。ほかのファイル（`ffplay.exe`・README・LICENSE）は展開しない | zip 1 つに 1 ファイル。`ffmpeg`（66,332,880 バイト）・`ffprobe`（66,158,064 バイト）。SHA-256 は下記 |
| 配布元の公開チェックサムとの照合 | gyan.dev の `https://www.gyan.dev/ffmpeg/builds/packages/ffmpeg-9.0.2-essentials_build.zip.sha256` と一致。GitHub のリリース API の `digest`（`sha256:60f46726…`）とも一致 | 配布元の `ffmpeg.zip.sha256`・`ffprobe.zip.sha256` と一致 |
| 署名 | （Authenticode 署名の確認は Windows 実機で。mam の `net/http` の取得はファイルに Mark of the Web を付けないので、SmartScreen の対象にならない見込み） | `LC_CODE_SIGNATURE` があり、署名は **Apple の Developer ID（`Developer ID Application: Martin Riedl (KU3N25YGLU)`）**、Hardened Runtime のフラグ付き（Linux 上で Mach-O を読んで確認）。arm64 で必要な署名は満たす。公証（notarization）は未確認 |
| 大きさ | ダウンロード約 110 MiB、展開後 約 201 MiB | ダウンロード約 54 MiB（2 つ）、展開後 約 126 MiB |

macOS の 2 つの実行ファイルの SHA-256 の全文: ffmpeg `2e11c6f90993cdb79fff84d3f90044d28316b310e75b3e030cfc9a54f2c9d384`、ffprobe `2738aa46a7f9acbc8ab09a6715554c90f7de603171ac403726765685df6a0059`。Windows: ffmpeg.exe `3256173f3f8bffd7df12227c68adf68025edb1832273a9530688a7bb1ed8edec`、ffprobe.exe `f0d36ecbbdd3bcfac3efa078c96c7271c2e68b3810595552ac3b7f17e9a65c52`。

**ハッシュの出し方**: 実装者が上の URL から `curl` で実際にダウンロードし、`sha256sum`（と zip の中身の取り出し）で計算した。配布元が公開しているチェックサムは上の欄のとおり照合した。誰かのコピーや推測は使っていない。

### 2.1 なぜこの 2 つか

要件: ① 固定の版の HTTPS の URL ② 配布元が公開するチェックサムで照合できる ③ ffmpeg と ffprobe の両方が入る ④ mam が呼ぶエンコーダーとフィルターがすべて入っている ⑤ Apple Silicon で動く（署名がある） ⑥ 手入れされている配布元 ⑦ 大きさが妥当。

- **mam が使うもの**（`internal/media`・`testenv` の呼び出しを調べた）: エンコーダー `libx264`（H.264。既定の書き出しとプレビュー）・`libx265`（HEVC）・`libvpx-vp9`（VP9）・`aac`・`libopus`、フィルター `drawtext`（字幕。`libfreetype` が要る）・`loudnorm`・`xfade`・`acrossfade`・`volume`・`aresample`・`aformat`・`crop`・`scale`・`format`・`concat` ほか。
- **確かめ方**: macOS のビルドは配布元が公開する `codecs.txt` に `libx264`・`libx265`・`libvpx-vp9`・`aac`・`libopus` があり、`versions.txt` の configure 行に `--enable-libfreetype --enable-fontconfig --enable-libharfbuzz` がある。Windows のビルドは、zip の中の `ffmpeg.exe` に埋まった configure 行（`--enable-libx264 --enable-libx265 --enable-libvpx --enable-libfreetype --enable-libmp3lame --enable-libopus --enable-libvorbis` ほか）で確かめた（`.exe` は Linux で動かせない）。さらに**同じ配布元・同じ 9.0.2 の Linux 版**（`martin-riedl.de/download/linux/amd64/1789931100_9.0.2`）を実際に動かし、`-encoders` と `-filters` に上のものがすべてあること、mam の `internal/media`・`internal/judge` のテストが通ることを確かめた。
- **GPL になる理由**: `libx264` と `libx265` は GPL なので、それを含むビルドは GPL になる。BtbN の `lgpl` ビルドは GPL 専用のライブラリ（主に libx264・libx265）を外すため `libx264` が無く、mam の既定の書き出し・プレビューが動かない。**LGPL のビルドでは mam が呼ぶエンコーダーを満たせない**ので、GPL ビルドを使う（§3）。
- **検討して外したもの**（確認した事実だけ）: evermeet.cx（macOS は x86_64 のみ）、osxexperts.net（ページに「educational purposes only」とあり、Apple Silicon では利用者が自分で ad-hoc 署名を付ける前提の説明）、BtbN/FFmpeg-Builds（Windows と Linux のみで macOS が無い。`lgpl` は libx264・libx265 を含まない。`latest` は動く別名）、gyan.dev の `full_build`（約 246 MiB。mam が使うコーデックは `essentials` に入っていて、大きいだけ）。
- **macOS の過去の版の URL**: 配布元の履歴ページ（`/info/history/macos/arm64/release`）に過去の版が並び、5.0 の URL まで取得できる。新しい版が出ても、固定した 9.0.2 は取れ続ける見込み（保証はない。取れなくなったら 404 の案内を出し、mam を更新すると新しい版に固定し直す）。

## 3. ライセンス

- どちらのビルドも `--enable-gpl --enable-version3` で、**GPL v3 以降**（Windows 版は同梱の README に `License: GPL v3`）。
- **mam のバイトに ffmpeg は入らない**。利用者の操作で配布元から取得し、別のプロセス（`exec`）として起動するだけで、リンクも組み込みもしない。mam は AGPL-3.0 で、GPL v3 と互換（AGPL-3.0 §13 と GPL-3.0 は互いに結合できる）。ComfyUI（GPL-3.0）と同じ整理で、`THIRD_PARTY_NOTICES.md` に「利用者が取得する別プログラム」の節を足す。**配布元が配るビルドのソースは配布元が提供する**（上の表のソース）。mam が再配布するのではないので、mam はソースを添えない。
- 取得した実行ファイルが静的に含む依存（x264・x265・libvpx・libass ほか）のライセンスの一覧は**調べていない（未確認）**。再配布するときは、取得した配布元の README・LICENSE に従う。
- 画面（設定・初回ガイド）には、ビルド名・配布元・ライセンス名と URL・この版のソースの URL・大きさを、ボタンの前に表示する。

## 4. 検証と入れ方（`ffmpegdl.Installer.Install`）

1. 保存先が書き込めること・空き容量（ダウンロードの残りと展開後の合計）を、通信の前に確かめる。足りなければ `*fetch.NoSpaceError`（通信しない）。
2. アーカイブを**1 つずつ**ダウンロードする（`modelmgr/fetch`。Codex のエンジンの取得と同じ）。URL とリダイレクト先は `ffmpegdl.DownloadHosts`（`github.com`・`githubusercontent.com`・`ffmpeg.martin-riedl.de` とそのサブドメイン）の https だけ。`<名前>.part` に書き、Range で再開でき、**サイズと SHA-256 が一致してから**初めて `downloads/<名前>` になる。一致しなければ `.part` を消して `fetch.ErrChecksum` / `ErrSize`（使わない）。
3. 展開（`exeinstall.Extract`）: 取り出すのは、カタログが名指しした 2 つのファイル（zip の中の**完全一致のエントリ名**）だけ。ほかのエントリは読まず・書かない。出力先は `bin.new/` の下で、ファイル名はカタログの**素の名前**（パスの区切り・`..`・ドライブ名は拒否）。リンクやディレクトリは拒否、同じエントリが 2 つあれば拒否。**アーカイブの中の名前や書き方は、書き込み先を決めない**。各ファイルは新規作成（既存は上書きしない）で、サイズと SHA-256 がカタログと一致すること。どれか 1 つでも一致しなければ、書いたものをすべて消し、アーカイブも消す。
4. macOS の隔離属性（`com.apple.quarantine`）を外す（ベストエフォート。mam の取得は付けない）。
5. 各実行ファイルを `bin.new/` で一度実行する（`-version`。出力に ` version ` を含むこと。上限時間つき、`internal/media.Version`）。実行できなければ（署名が通らない・セキュリティ製品が止めた・形式が違う）`ErrNotRunnable` で、何も入れない。
6. **原子的に入れ替える**: 今の `bin/` を `bin.old/` に退避 → `bin.new/` を `bin/` に改名 → `bin.old/` を消す。途中で止まったら起動時と次の導入が `bin.old/` を戻す（`CleanLeftovers`）。
7. **最後に** `installed.json`（版・各ファイルの名前・サイズ・SHA-256・時刻）を書く。アーカイブを消す。

**使うのは、記録があり、ファイルがそろっているときだけ**（`Installer.Dirs`）。`installed.json` が無い・壊れている・記録に ffmpeg と ffprobe の両方が載っていない・記録のファイルが無い／サイズが違う（手で消した・変えた）ときは「壊れている」で、**探す場所に加えない**。`bin/` に記録の無いファイルが置かれていても使わない。検証に失敗した導入は、前の導入（あれば）をそのまま残す。

## 5. 更新の扱い

- 固定する版は mam のリリースで上げる（Codex のカタログと同じ。`ffmpegdl/data/catalog.json` を書き換えて、SHA-256 を実際にダウンロードして計算し直す。テストが形式（https・`latest` でない・SHA-256 の形・名前）を検査する）。
- 入れてある版と mam の固定した版が違うとき、画面は「この mam が確かめた版は X です。入れてある Y は、そのまま使えます」と出し、ボタンを「新しい版をダウンロード」にする。**入れてある版は使い続ける**（古くても ffmpeg として動く。勝手に消さない・勝手に取らない）。もう一度ボタンを押すと、同じ手順（§4）で入れ替わる。失敗しても、前の版が残る。
- Windows では、動いている ffmpeg（Judge・書き出し・プレビューが使っている最中）のあるフォルダは改名できず、ウイルス対策ソフトが書いたばかりのファイルを調べている間も改名が失敗することがある。そのときは導入が失敗して前の版が残るので、少し待ってからもう一度押す（実機で確かめる項目。§9）。
- **自動の更新確認・バックグラウンドの通信は無い**。ネットワークに出るのは、利用者が「ダウンロード」を押した間だけ。
- 「削除」ボタンで、入れた ffmpeg を消せる（`bin/`・記録・ダウンロードの残りを消す）。起動中の mam は、再起動するまで、起動時に見つけた ffmpeg を使い続ける。

## 6. 置き場所と探す順

```
<ffmpeg のディレクトリ>/
  installed.json     導入の記録（最後に書く）
  bin/               ffmpeg(.exe)・ffprobe(.exe)。ここだけが探す場所に入る
  bin.new/ bin.old/  入れ替えの途中（普通は無い）
  downloads/         ダウンロード中のアーカイブと .part（導入が終わると消える）
```

- ディレクトリ: Windows は `<mam.exe のディレクトリ>\ffmpeg`（アンインストーラーは `RMDir /r $INSTDIR` で、インストールディレクトリごと消す。`comfyui`・`codex` と同じ）。macOS は `<データディレクトリ>/ffmpeg`（`~/Library/Application Support/mam/ffmpeg`）。`MAM.app` の中には置かない（自動更新が `.app` を丸ごと入れ替えるため）。書き込めない場所（読み取り専用の場所のポータブル版）は、押した時点で「書き込めません」と答える。
- 探す順（[ffmpeg-lookup.md](ffmpeg-lookup.md) §1）: ① 設定したパス ② `PATH` ③ macOS の Homebrew の場所 ④ **ダウンロードして入れたもの**（`locate.Finder.Extra` に `bin/` を入れる）。`ffmpegsettings.Service` が `ffmpegdl.Service.Dirs` を呼ぶたびに読む（`WithManagedDirs`）ので、導入が終わったあとの探索（設定画面・初回ガイド）はすぐ見つけ、アプリの `media.Tools` は**次の起動から**使う（起動時に一度だけ探す）。
- 利用者が自分で入れた ffmpeg が `PATH` や Homebrew にあれば、それが使われ、ダウンロードしたものは使われない。画面は、別の場所の ffmpeg が見つかっているとき、その旨を出す。

## 7. 画面とルート

- 設定画面の「ffmpeg」（`/settings/ffmpeg`）の下と、初回ガイドの「ffmpeg の確認」（ffmpeg か ffprobe が見つからないとき）に、同じ部品（`organisms/settings/ffmpeg.DownloadPanel`）を置く。**ダウンロードできる環境（Windows x64・macOS arm64）だけ**に出す。ほかの環境は、これまでの案内（`winget`・`brew`・汎用）だけ。初回ガイドでは、ボタンの下に「自分で入れるときは」として従来の案内を残す。
- ボタン「ffmpeg をダウンロードして入れる」（新しい版があるときは「新しい版をダウンロード」）、進み具合（バーと %。ダウンロード → 展開 → 動作確認）、「中断」（部分ファイルを残し、もう一度押すと続きから）、「削除」。進行中は 2 秒ごとに自分を取り直す（Codex の取得と同じ）。成功すると「mam を一度終了して、起動し直してください」を出す。
- ルート（[../api/endpoints-settings.md](../api/endpoints-settings.md) §5.2）: `GET` / `POST` / `DELETE /settings/ffmpeg/download`、`POST /settings/ffmpeg/download/cancel`。

## 8. テスト

- `exeinstall/extract_test.go`: zip と tar.gz の展開（名指しのエントリだけ・フラット・パスの拒否・リンク・二重・足りない・サイズ違い・内容違い・途中の失敗で書いたものを消す・上書きしない・取り消し）。Codex のエンジンのテスト（`chatgpt/codex/engine`）は、この共通の展開を通る。
- `ffmpegdl/*_test.go`: カタログの形（実際の固定値・https・`latest` でない・不正なカタログの拒否）、導入（zip 1 つ・2 つ・フェーズ・記録・原子的入れ替え・更新・失敗した更新が前の版を残す）、**アーカイブの SHA-256 が違えば何も入れず何も残さない**、**アーカイブは合っていても中の実行ファイルが違えば入れない**、実行できなければ入れない、記録が無い／壊れている／ファイルが変わった（壊れている）ときは使わない、中断と再開（Range）、容量不足、書き込み不可、途中の入れ替えの復旧、削除、`Service`（バックグラウンド・1 つずつ・取り消し・閉じる）。
- **ffmpeg が無いマシンで、ダウンロードして入れたあと、アプリが使う探索（`ffmpegsettings.Look`）が見つけ、そのツールで `Probe`・`Thumbnail` が動く**こと、**PATH の ffmpeg がダウンロードしたものに勝つ**こと、**SHA-256 が合わないダウンロードは見つからない**こと（`ffmpegdl/lookup_test.go`。マシンの本物の ffmpeg を zip に入れて `httptest` で配る）。
- `service/ffmpegsettings`: 入れたものは最後に探す・PATH と設定が勝つ・呼ぶたびに読む。`web/ffmpegscreen/download_test.go`: 画面（ボタン・進み具合・取り消し・チェックサム不一致の表示・削除・CSRF）。`web/handler/onboarding`: 初回ガイドのボタンの出し分け。
- 実機: §9。

## 9. 実機で確かめること（Linux では確かめられない）

- **Windows 11 x64**: ffmpeg の無い PC で、設定の「ffmpeg をダウンロードして入れる」を押し、約 110 MiB を取得して「ffmpeg を入れました」になること。mam を再起動して、Judge・Take のプレビュー・Export が動くこと。`<mam.exe のディレクトリ>\ffmpeg\bin` に `ffmpeg.exe`・`ffprobe.exe` ができ、**Windows Defender・SmartScreen が止めないこと**（Authenticode 署名は無い）。PATH に別の ffmpeg を入れたら、そちらが使われること。
- **macOS（Apple Silicon）**: 同様に、約 54 MiB を取得して再起動後に Judge・プレビュー・Export が動くこと。`~/Library/Application Support/mam/ffmpeg/bin/ffmpeg` が **Gatekeeper に止められず**、`codesign -dv --verbose=4` で Developer ID の署名が出ること。Finder から起動した MAM.app で動くこと。`brew install ffmpeg` した ffmpeg があれば、そちらが使われること。
- **Windows の入れ替え**: ダウンロードのあとの `bin.new` → `bin` の改名が、ウイルス対策ソフトに止められないこと。止められるなら、改名のやり直しを足す。
- **9.0.2 の ffmpeg での mam の動作**（mam の CI は Ubuntu の ffmpeg で動く）。同じ 9.0.2 の Linux 版ではテストが通ることを確かめたが、Windows・macOS のビルドでの Export（H.264・HEVC・VP9）・字幕（drawtext の既定フォント）は実機で確かめる。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-01 | 新規作成。Windows x64（gyan.dev 9.0.2）と macOS arm64（martin-riedl.de 9.0.2）の ffmpeg / ffprobe を、ボタンでダウンロードして入れる。取得元・SHA-256 の固定・検証・GPL v3 の方針・更新の扱い（自動なし）・探す順（利用者の ffmpeg が先）を決めた | Issue #900 |
