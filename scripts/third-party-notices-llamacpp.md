## llama.cpp とローカル LLM の重み（利用者が取得する別プログラム）

MAM のローカルランタイム管理（Issue #1008）は、利用者の操作で llama.cpp の `llama-server`（ローカルの LLM サーバー。ggml-org/llama.cpp）と、LLM の重み（GGUF）を、MAM の実行ファイルと同じ場所の `localai\llama-cpp` ディレクトリに取得して展開します。**MAM のインストーラー・リリースには含まれません**（MAM は llama.cpp のバイトを配布しません）。`llama-server.exe` は MAM が決めた固定の引数だけで起動し、`127.0.0.1` だけで待ち受けます（利用者が引数を指定することはできません）。

- llama.cpp のライセンス（MIT）とソースコード: <https://github.com/ggml-org/llama.cpp/tree/b11960>（ライセンス文は <https://github.com/ggml-org/llama.cpp/blob/b11960/LICENSE>）。取得するのは公式の GitHub リリース `b11960`（<https://github.com/ggml-org/llama.cpp/releases/tag/b11960>）の Windows x64 のビルド済みバイナリ `llama-b11960-bin-win-cuda-12.4-x64.zip`（RTX 50 シリーズは `…cuda-13.4-x64.zip`）と、CUDA ランタイムの DLL の `cudart-llama-bin-win-cuda-12.4-x64.zip`（同 `13.4`）で、SHA-256 を MAM のカタログに固定して検証します（4 つの SHA-256 は spec/architecture/model-manager/local-runtime.md §17.1）。ビルドには OpenMP のランタイムのライセンス（`LICENSE-LLVM-OpenMP`）が含まれます。
- **注意が要るもの**: CUDA ランタイムの DLL（`cudart`・`cublas`・`cublasLt`）は NVIDIA のライセンス（CUDA Toolkit の使用許諾）に従います（**再配布の条件は確認していません（未確認）**）。
- 取得したものを再配布するときは、取得した zip に入っているライセンス文を添えてください。

### ローカル LLM の重み（GGUF）

どの重みも、`mradermacher/<名前>-GGUF`（Hugging Face）のコミット固定の URL から、固定した SHA-256 とサイズを検証して取得します（2026-11-03 に Hugging Face の API で確認。どのリポジトリも gated ではありません）。選んだモデル 1 つだけを取得します。ライセンスは、元の重みと GGUF の変換元のモデルカードの `license` の表記で、**法的な保証ではありません**（商用で使う前にライセンス全文を確認してください）。**商用利用がはっきりしないもの（`unknown`）は、DeepSeek-R1-Distill-Qwen-14B の非検閲版（huihui-ai の abliterated-v2。派生側のカードにライセンスの表示がありません。元の DeepSeek-R1-Distill-Qwen-14B は MIT）です**。非検閲（abliterated）の 4 つは、安全のための拒否を取り除いた派生で、成人向け相当の内容を出力しうるため、画面に注記が出ます。

| 名前 | 出典（変換元） | コミット | サイズ（バイト） | ライセンス | 商用利用 |
|---|---|---|---|---|---|
| Gemma 4 12B-it（純正・Q4_K_M） | <https://huggingface.co/mradermacher/gemma-4-12B-it-GGUF> | `bd28046d` | 7,381,382,592 | [Apache License 2.0（Gemma 4 12B-it）](https://huggingface.co/google/gemma-4-12B-it) | 可 |
| Gemma 4 12B-it 非検閲（Huihui abliterated・Q4_K_M）（非検閲・成人向け相当） | <https://huggingface.co/mradermacher/Huihui-gemma-4-12B-it-abliterated-GGUF> | `f28c4a14` | 7,381,382,720 | [Apache License 2.0（Huihui-gemma-4-12B-it-abliterated）](https://huggingface.co/huihui-ai/Huihui-gemma-4-12B-it-abliterated) | 可 |
| Qwen3.8-27B（純正・IQ4_XS） | <https://huggingface.co/mradermacher/Qwen3.8-27B-GGUF> | `860659a5` | 15,420,450,528 | [Apache License 2.0（Qwen3.8-27B）](https://huggingface.co/Qwen/Qwen3.8-27B) | 可 |
| Qwen3.8-27B（純正・Q3_K_M 軽量版） | <https://huggingface.co/mradermacher/Qwen3.8-27B-GGUF> | `860659a5` | 13,500,737,248 | [Apache License 2.0（Qwen3.8-27B）](https://huggingface.co/Qwen/Qwen3.8-27B) | 可 |
| Qwen3.8-27B 非検閲（Huihui abliterated・IQ4_XS）（非検閲・成人向け相当） | <https://huggingface.co/mradermacher/Huihui-Qwen3.8-27B-abliterated-GGUF> | `c5f7e0aa` | 15,420,450,976 | [Apache License 2.0（Huihui-Qwen3.8-27B-abliterated）](https://huggingface.co/huihui-ai/Huihui-Qwen3.8-27B-abliterated) | 可 |
| Qwen3.8-27B 非検閲（Huihui abliterated・Q3_K_M 軽量版）（非検閲・成人向け相当） | <https://huggingface.co/mradermacher/Huihui-Qwen3.8-27B-abliterated-GGUF> | `c5f7e0aa` | 13,500,737,696 | [Apache License 2.0（Huihui-Qwen3.8-27B-abliterated）](https://huggingface.co/huihui-ai/Huihui-Qwen3.8-27B-abliterated) | 可 |
| DeepSeek-R1-Distill-Qwen-14B（純正・Q4_K_M） | <https://huggingface.co/mradermacher/DeepSeek-R1-Distill-Qwen-14B-GGUF> | `f5a17506` | 8,988,110,432 | [MIT License（DeepSeek-R1-Distill-Qwen-14B）](https://huggingface.co/deepseek-ai/DeepSeek-R1-Distill-Qwen-14B) | 可 |
| DeepSeek-R1-Distill-Qwen-14B 非検閲（Huihui abliterated-v2・Q4_K_M）（非検閲・成人向け相当） | <https://huggingface.co/mradermacher/DeepSeek-R1-Distill-Qwen-14B-abliterated-v2-GGUF> | `39e97fd1` | 8,988,110,912 | [ライセンスの表示なし（DeepSeek-R1-Distill-Qwen-14B-abliterated-v2）](https://huggingface.co/huihui-ai/DeepSeek-R1-Distill-Qwen-14B-abliterated-v2) | **はっきりしない（商用不明）** |

