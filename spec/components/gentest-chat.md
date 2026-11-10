# コンポーネント仕様: 単発生成のチャット

[gentest-screen.md](gentest-screen.md) の「チャットで作る」のタブ（Issue #1138）。しくみ（エージェントループ・確認のカード・安全の規則）は [../architecture/gentest/gentest-chat.md](../architecture/gentest/gentest-chat.md)、ルートは [../api/endpoints-gentest.md](../api/endpoints-gentest.md)。

| 区分 | コンポーネント | 責務 |
|------|---------------|------|
| pages（`gentest_chat_page.templ`） | `GenerationChatPage` | 共通の枠 `generationShell`（見出し「単発生成」・「?」・ページ間のタブ。タブは「生成する」「**チャットで作る**」「画像から似せる」「一部だけ描き直す」「履歴」「プロンプト履歴」）に `Chat` を入れる。モーダルのホスト（`modal.Host()`）も置く（結果のタイルが詳細を開くため） |
| organisms（`organisms/gentest/chat/`） | `Chat`・`Composer`・`Messages`・`ProposalCard`・`ResultBlock`・`Swaps` | スレッド（`#gtc-thread`。`role=log`・`aria-live=polite`・`tabindex=0`。最大高さ 65vh でスクロール）と挨拶のカード・入力欄・スレッドに足す部品 |
| handler（`web/handler/gentest/chatscreen/`） | `Register`・`Page`・`Send`・`Confirm`・`Dismiss`・`Batch` | ルート。サービス（`service/gentest/chat`）の結果を表示用の構造にする（`views.go`）。organism はサービスを import しない |
| static（`static/src/components/gentest-chat/`） | `chat.ts` | Enter で送信（`isSendKey`。Shift+Enter は改行・IME の確定の Enter は送らない・空は送らない）、送信が通ったら、送った言葉だけを入力欄から消して戻す（拒否されたら言葉を残す。送信中に打った次のメッセージは残す）、新しい部品が足されたらスレッドを末尾へ（結果のブロックが 2 秒ごとに自分を取り直したときは、利用者が末尾の近くにいるときだけ）、**答え済みの選択肢（最新以外）を `inert`** にして薄くする |

## 1. スレッドの部品

- **挨拶のカード**: 説明（「見積りを見て『この内容で実行する』を押すまで、生成は始まりません」）と、始め方の選択肢 3 枚（画像を作りたい／動画を作りたい／モデルを比べたい）。
- **ユーザーの発言**: 右寄せ（`bg-accent-soft`）。**アシスタントの発言**: 左寄せの枠（`border-border`）。
- **選択肢のカード**（`gtc-option`）: 1 枚 1 フォーム（会話の id と `reply` を `content` として `POST /generation-test/chat/messages`）。幅が狭いと 1 列・`sm:` 以上は 2 列。見出し（`text-ink`・太字）と 1 行の説明（`text-mute`）。**ボタンそのもの**なので Tab・Enter・Space・スクリーンリーダーが使える。押している間は無効。
- **状態の行**（`gtc-status`）: アプリの言葉（「画像のモデルを調べました（6 件）」＝`text-xs text-mute`）、失敗（`text-danger`・`role=alert`・`data-failed=true`。キー無しのときは「テキスト生成の設定を開く」のリンクつき）。
- **確認のカード**（`gtc-proposal`。`data-state`）: 見出し「実行の確認」・種類・モデル・本数・縦横比（動画は解像度・長さ）・プロンプト（ネガティブつき）・「全部で N 件を生成します。見積り合計は $X」・モデルごとの内訳の表（モデル・件数・単価・小計。単価なしのモデルは「単価なし／合計に入りません」。横にはみ出すときは表の箱の中でスクロール）・単価不明の注記（`text-warning`）・見積りの警告。**開いている間だけ**ボタン「この内容で実行する」（主）と「やめる」。押すフォームが持つのは会話の id だけ。閉じたカードはボタンの代わりに状態（「実行しました」「やめました」「新しい見積りに置き換わりました」「実行しませんでした」＋理由）を出す。状態が変わったカードは、応答の帯域外（`hx-swap-oob`）で元の位置のまま描き直す。
- **結果のブロック**（`gtc-result`）: 状態のバッジ・件数・見積り・進捗バー・結果のタイル（`resulttile.Tile`。2 列〜`md` で 4 列。モデル名つき。タップで結果の詳細のモーダル）。動いているあいだ 2 秒ごとに自分だけを取り直し、終わると止まる。
- **入力欄**（`gtc-composer`）: `textarea`（最大 2000 文字・プレースホルダに例・Enter で送信、Shift+Enter で改行）と「送信」。送っている間はボタンが無効。

## 2. 安全

有料の生成を始めるのは確認のカードの「この内容で実行する」だけ。選択肢のカードや入力欄の発言からは始まらない。サーバーが見積りの再計算・上限の検査をやり直す（[../architecture/gentest/gentest-chat.md](../architecture/gentest/gentest-chat.md) §3）。LLM が決まっていない（Prompt Planner のルートが無い）設定では、タブは案内（`gtc-unavailable`）だけを出し、入力欄を置かない。

## 3. 狭い幅

375 px で、カード・表・結果が横にはみ出さない（長いモデル名は折り返し、表は自分の箱の中でスクロール）。`gentest-chat/chat-layout.wtest.ts` が 375・1280 px で確かめる（フィクスチャは `web/gentest_chat_fixture_test.go`）。タップの対象（実行ボタン）の高さは 36 px 以上。

## 改訂履歴

| 版 | 日付 | 変更内容 | 変更理由 |
|----|------|---------|---------|
| 1.0 | 2026-11-10 | 新規作成（スレッド・選択肢のカード・確認のカード・結果のブロック・入力欄・安全・狭い幅） | Issue #1138 |
| 1.1 | 2026-11-10 | 構成の表の static の行に、入力欄は送った言葉だけを消すこと（送信中に打った次のメッセージを残す）と、スレッドは新しい部品が来たときと利用者が末尾の近くにいるときだけ末尾へ動くこと（結果のブロックの取り直しでは動かさない）を足した | PR #1141 のレビュー指摘 |
