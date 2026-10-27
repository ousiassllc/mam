## macOS 版（Apple Silicon）を初めて開くとき

`mam-VERSION-darwin-arm64.dmg` は Apple の Developer ID による署名・公証をしていないため、初回の起動で macOS が「開発元を検証できません」などと表示してブロックします。次のどちらかで開いてください。

1. dmg を開き、`MAM.app` を `/Applications` にドラッグします（ここに置かないと自動更新が働きません）
2. `/Applications` の `MAM.app` を**右クリック（control + クリック）→「開く」**を選び、確認のダイアログで「開く」を押します（2 回目以降は普通に開けます）。macOS 15 以降で「開く」が出ないときは、いったん開こうとしてから「システム設定 > プライバシーとセキュリティ」の「このまま開く」を押します

ターミナルで済ませるなら、隔離属性を外します。

```sh
xattr -dr com.apple.quarantine /Applications/MAM.app
```

対応は Apple Silicon（arm64）の macOS 12 以降だけです（Intel 版・ユニバーサル版はありません）。
