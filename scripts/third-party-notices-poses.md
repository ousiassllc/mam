
## Shisei のポーズ集（利用者が取得するもの）

MAM の生成テストの「ポーズを選ぶ」（Issue #986）は、利用者が同意して「導入する」を押したときだけ、ポーズの参照画像（マネキンの線画と、ControlNet 用の深度・法線マップ〔Issue #1020〕。Qwen-Image 2.1 などの参照画像に「ポーズだけ」の役割で渡す）を GitHub の `wquguru/shisei` のリリース `v0.4.1` から取得し、MAM の実行ファイルと同じ場所の `localai\shisei-poses` ディレクトリに展開します。**MAM はこれらを同梱せず再配布しません**。取得する Zip 4 つ（線画 2・深度 1・法線 1）と分類表（`data/catalog.json`。コミット `26522a708c77088eda4631327b15d8d05f6281c0` に固定）は、SHA-256 と大きさを MAM のカタログ（`backend/internal/modelmgr/catalog/data/assets/shisei-poses.json`）に固定して検証します。取り出すのは、全年齢向けと分類されたポーズの線画（96 ポーズ・414 枚）だけです。成人向けのポーズ（`sexy`・`duo`）は取得しません。

- Shisei のコードとドキュメントは Apache License 2.0、画像（Release の Zip を含む）と Shisei 自身のデータは CC0 1.0 Universal（パブリックドメインへの供与。帰属表示は不要）です。出典・ライセンス文: <https://github.com/wquguru/shisei/blob/26522a708c77088eda4631327b15d8d05f6281c0/NOTICE>、<https://github.com/wquguru/shisei/blob/26522a708c77088eda4631327b15d8d05f6281c0/LICENSE-images>。AI が作る画像の権利についての考え方は <https://github.com/wquguru/shisei/blob/26522a708c77088eda4631327b15d8d05f6281c0/docs/LICENSING.md> にあります。
- **ポーズの選び方・番号・名前・分類は、ギア（@giaaibusi）の ai-pose-gallery（<https://ai-pose-gallery.pages.dev/>）に従っています。Shisei の NOTICE は、その部分を「credited, not licensed」（出典を示すが、ライセンスは与えない）と書いており、どちらのライセンスも及びません。** MAM は分類表をポーズ名の表示に使うため、画面ではこの分類表の商用利用の可否を「不明」としています。画像（CC0）の商用利用は可です。
- Shisei の画像は、マネキンまたは AI が作った成人の姿で、実在の人物を写したものではありません。
- 深度・法線マップ（2026-10-31 に作成）は、Shisei が線画から Depth Anything V2 Small（Apache-2.0）と Marigold Normals v1-1（CreativeML OpenRAIL++-M）で作ったものです。docs/LICENSING.md の規則 3 のとおり、その使用制限はモデルの使い方に掛かり、出力の再配布には掛からず、出力は CC0 です。MAM は線画と同じ姿勢の分だけを取り出し、成人向けのシリーズ（`sexy`・`duo`）は Zip に入っていても取り出しません。
- ポーズの参照画像で作った画像の権利と、選んだ生成モデルのライセンス（Qwen-Image 2.1 を含む Qwen RESEARCH LICENSE のモデルは非商用）は別です。画面では、選んだモデルの商用利用の可否をポーズの選択画面にも表示します。

## ComfyUI_VNCCS_Utils（PoseStudio。許可リストに載せているが、今は導入できない）

MAM のカスタムノードの許可リスト（Issue #976・#985）には、AHEKOT の ComfyUI_VNCCS_Utils（コミット `334eee549fba5025b97c4e0a6123b2b68a46f0fc`、MIT License、Copyright (c) 2025 MiuProject）を載せています。**今は導入できません**（PoseStudio はブラウザの画面でポーズを取り込むのを待つ作りで、実行時に Hugging Face からポーズ集・モデルを自動で取得し、必要な Python パッケージの一部が同梱の環境にないためです）。導入できる状態になるまで、MAM がこのコードを取得・同梱・再配布することはありません。このリポジトリには取り込んだ別のコード（ComfyUI-Krea2Edit、Apache License 2.0）の LICENSE と NOTICE が入っており、実行時に取得されるポーズ集・モデルのライセンスは配布元ごとに違い、MAM は確かめていません（商用利用の可否は「不明」）。LoRA `MIUProject/VNCCS_PoseStudio_QI2.1`（Hugging Face）にはライセンスの記載がなく、MAM は取得・同梱しません。
