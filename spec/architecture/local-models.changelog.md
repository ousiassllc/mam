# ローカル・代替の画像／動画モデル 改訂履歴

[local-models.md](local-models.md) の改訂履歴。新しい版は末尾に足す。

| 版 | 日付 | 変更内容 | 理由 |
|---|---|---|---|
| 1.0 | 2026-10-24 | 新規。接続先の種類（ComfyUI・OpenAI 互換の画像のみ）、識別子、`models.json` のスキーマ、ComfyUI の割り当て表・実行・取り消し・テンプレート、接続テスト、ダウンロード方針、即時反映、Service API を定めた | Issue #831 |
| 1.1 | 2026-10-25 | 冒頭に、MAM が ComfyUI を取得・起動してモデルを入れる経路（`model-manager/model-manager.md`、接続先 `bundled-comfyui`）への参照を足した | Issue #833 |
| 1.2 | 2026-10-29 | §4 に LTX-2.5（§4.1）を足した: カタログ `ltx-2.5-22b-distilled` とテンプレート `txt2video-ltx-2-5`・`img2video-ltx-2-5`、取得の手順（ライセンスの同意とアクセスの申請 → トークンの発行 → 保存 → 導入）、外部の ComfyUI での使い方。組み込みテンプレートの一覧を更新した | Issue #877 |
| 1.3 | 2026-11-02 | §3 の `workflow` に `reference_mode`・`no_reference`・`lora_model_node`、§4 の割り当てに `reference_image` の `optional` と `reference_denoise` を足し、§4.2 を新設した: 画像モデルの参照画像（img2img と編集）、強さ・役割・LoRA・ポーズを真似させない指定のモデルごとの方式。組み込みテンプレートを `templates/image/`・`templates/video/` に分けた | Issue #933。同梱モデルが参照画像を使えなかった |

