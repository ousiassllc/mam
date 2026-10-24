# モデルマネージャー: 改訂履歴

[model-manager.md](model-manager.md) の改訂履歴。

| 版 | 日付 | 変更内容 | 理由 |
|---|---|---|---|
| 1.0 | 2026-10-25 | 新規。ComfyUI エンジンの取得（GPU 判定・固定版・SHA-256・`Range` 再開・7z 展開）、`<exe のディレクトリ>\comfyui` への配置、子プロセスの起動と終了、管理される接続先 `bundled-comfyui`、モデルのカタログ（8 モデル）と同意・削除の参照数、ジョブ `model_install`・`gentest`、検出、Service API、未確認の項目を定めた | Issue #833。ComfyUI を同梱せず、利用者の操作でエンジンとモデルを入れられるようにするため |
