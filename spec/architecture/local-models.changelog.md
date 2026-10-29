# ローカル・代替の画像／動画モデル 改訂履歴

[local-models.md](local-models.md) の改訂履歴。新しい版は末尾に足す。

| 版 | 日付 | 変更内容 | 理由 |
|---|---|---|---|
| 1.0 | 2026-10-24 | 新規。接続先の種類（ComfyUI・OpenAI 互換の画像のみ）、識別子、`models.json` のスキーマ、ComfyUI の割り当て表・実行・取り消し・テンプレート、接続テスト、ダウンロード方針、即時反映、Service API を定めた | Issue #831 |
| 1.1 | 2026-10-25 | 冒頭に、MAM が ComfyUI を取得・起動してモデルを入れる経路（`model-manager/model-manager.md`、接続先 `bundled-comfyui`）への参照を足した | Issue #833 |
| 1.2 | 2026-10-29 | §4 に LTX-2.5（§4.1）を足した: カタログ `ltx-2.5-22b-distilled` とテンプレート `txt2video-ltx-2-5`・`img2video-ltx-2-5`、取得の手順（ライセンスの同意とアクセスの申請 → トークンの発行 → 保存 → 導入）、外部の ComfyUI での使い方。組み込みテンプレートの一覧を更新した | Issue #877 |
