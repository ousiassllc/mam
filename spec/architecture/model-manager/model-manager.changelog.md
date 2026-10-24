# モデルマネージャー: 改訂履歴

[model-manager.md](model-manager.md) の改訂履歴。

| 版 | 日付 | 変更内容 | 理由 |
|---|---|---|---|
| 1.0 | 2026-10-25 | 新規。ComfyUI エンジンの取得（GPU 判定・固定版・SHA-256・`Range` 再開・7z 展開）、`<exe のディレクトリ>\comfyui` への配置、子プロセスの起動と終了、管理される接続先 `bundled-comfyui`、モデルのカタログ（8 モデル）と同意・削除の参照数、ジョブ `model_install`・`gentest`、検出、Service API、未確認の項目を定めた | Issue #833。ComfyUI を同梱せず、利用者の操作でエンジンとモデルを入れられるようにするため |
| 1.1 | 2026-10-25 | §10 に `CancelInstall`・`StageReference`（`GentestInput.ReferenceID`）を足し、§12「画面と MCP ツール」を足した。冒頭の「別の作業で載せる」を、画面・MCP ツールへのリンクに改めた | Issue #833。画面（`/model-manager`）と MCP ツールの実装 |
| 1.2 | 2026-10-26 | §8 の試し生成を、生成テスト（[../gentest/gentest.md](../gentest/gentest.md)）の 1 件のバッチとして動くものに改めた。`Service` から `StartGentest`・`Gentests`・`Gentest`・`StageReference`・`GentestFile`・`DeleteGentest`・`GentestInput`・`GentestRecord`・`DataDir` を削除し（実装は `service/gentest` の 1 つ）、MCP の試し生成のツール（`mcp/tools/modeltrial`）を生成テストのツールに統合した | Issue #835。試し生成と生成テストの実装を 1 つにするため |
