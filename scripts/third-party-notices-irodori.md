## Irodori-TTS（利用者が取得する別プログラム）

MAM のローカルランタイム管理（Issue #1015）は、利用者の操作で Irodori-TTS（日本語専用の音声合成。Aratako 氏）の実行環境を、MAM の実行ファイルと同じ場所の `localai\irodori-tts` ディレクトリに組み立て、別のプロセス（MAM が同梱する起動用スクリプト `mam_irodori_server.py` が、公式の API サーバー Irodori-TTS-Server の FastAPI アプリの前に番人を置いて起動する。起動のたびに作る合言葉を付けた `POST /v1/audio/speech` だけに答え、`127.0.0.1` だけで待ち受け）として起動します。**公式は Windows 向けの配布物を出していないため、MAM が部品をカタログに固定して取得し、組み立てます**（MuseTalk と同じ作り）。MAM はこれらを同梱せず再配布しません。

- Irodori-TTS-Server（API サーバー。MIT）: <https://github.com/Aratako/Irodori-TTS-Server/tree/61012c760f22f7b4a6c21c5c5f8f9e148120b6f9>（ライセンス文は同じコミットの `LICENSE`）。
- Irodori-TTS（推論のコード。MIT）: <https://github.com/Aratako/Irodori-TTS/tree/89f9d8fbd4d51ea019867ee1197725ede1df13c5>。
- DACVAE（音声コーデックのコード。MIT）: <https://github.com/facebookresearch/dacvae/tree/414c20785fc3a28373073ea8ef7a1316eeeaca6e>。
- SilentCipher（見えない透かしのコード。MIT）: <https://github.com/SesameAILabs/silentcipher/tree/d46d7d0893a583d8968ab3a6626e2289faec9152>。Irodori-TTS は生成した音声に**必ずこの透かしを入れます（切れません）**。
- Python 3.10.11 の Windows 用の組み込み版（<https://www.python.org/ftp/python/3.10.11/python-3.10.11-embed-amd64.zip>。Python Software Foundation License。zip の中の `LICENSE.txt`）。
- argbind・julius・randomname（wheel が無い純 Python のパッケージ）は、PyPI の配布物（sdist。`.tar.gz`）をそのまま展開して Python の検索パスに置きます（ビルドも実行もしません）。
- Python パッケージの wheel 155 個の版は、公式サーバーの `uv.lock`（コミット `61012c76…`）の Windows・Python 3.10・CUDA 12.8 の分を、そのまま SHA-256 つきで固定しました。PyTorch（`torch`・`torchaudio`）は <https://download.pytorch.org/whl/cu128/>、その他は PyPI（`files.pythonhosted.org`）です。
- **注意が要るもの**: `torch` の CUDA 版の wheel は NVIDIA の CUDA ランタイムと cuDNN の DLL を含み、NVIDIA のライセンスに従います（**条件は確認していません（未確認）**）。`soxr`（libsoxr を含む）は LGPL-2.1 以降、`certifi` は MPL-2.0 です。`torchcodec` は FFmpeg の DLL を必要とします（**Windows での動作は未確認**）。
- 取得したものを再配布するときは、各パッケージのライセンス文（wheel の `*.dist-info` にあります）を添えてください。**一覧の突き合わせは、メタデータの表記を写したもので、原文との突き合わせはしていません（未確認）**。

| パッケージ | 版 | ライセンス（wheel・sdist のメタデータの表記） |
|---|---|---|
| absl-py | 2.4.0 | Apache-2.0 |
| accelerate | 1.13.0 | Apache |
| aiohappyeyeballs | 2.6.1 | PSF-2.0 |
| aiohttp | 3.13.5 | Apache-2.0 AND MIT |
| aiosignal | 1.4.0 | Apache 2.0 |
| annotated-doc | 0.0.4 | MIT |
| annotated-types | 0.7.0 | MIT License |
| anyio | 4.13.0 | MIT |
| asttokens | 3.0.1 | Apache 2.0 |
| async-timeout | 5.0.1 | Apache 2 |
| attrs | 26.1.0 | MIT |
| audioread | 3.1.0 | MIT |
| blinker | 1.9.0 | MIT License |
| brotli | 1.2.0 | MIT |
| certifi | 2026.4.22 | MPL-2.0 |
| cffi | 2.0.0 | MIT |
| charset-normalizer | 3.4.7 | MIT |
| click | 8.4.2 | BSD-3-Clause |
| colorama | 0.4.6 | BSD License |
| contourpy | 1.3.2 | BSD License |
| cycler | 0.12.1 | BSD License |
| datasets | 4.8.5 | Apache 2.0 |
| decorator | 5.2.1 | BSD-2-Clause |
| descript-audiotools | 0.7.2 | MIT |
| dill | 0.4.1 | BSD-3-Clause |
| docstring-parser | 0.18.0 | MIT |
| einops | 0.8.2 | MIT |
| exceptiongroup | 1.3.1 | MIT License |
| executing | 2.2.1 | MIT |
| fastapi | 0.136.1 | MIT |
| ffmpy | 1.0.0 | MIT |
| filelock | 3.29.0 | MIT |
| fire | 0.7.1 | Apache-2.0 |
| flask | 3.1.3 | BSD-3-Clause |
| flatten-dict | 0.5.0 | MIT |
| fonttools | 4.62.1 | MIT |
| frozenlist | 1.8.0 | Apache-2.0 |
| fsspec | 2026.2.0 | BSD-3-Clause |
| gitdb | 4.0.12 | BSD License |
| gitpython | 3.1.50 | BSD-3-Clause |
| gradio | 6.14.0 | Apache-2.0 |
| gradio-client | 2.5.0 | Apache-2.0 |
| groovy | 0.1.2 | MIT License |
| grpcio | 1.80.0 | Apache-2.0 |
| h11 | 0.16.0 | MIT |
| hf-gradio | 0.4.1 | MIT |
| hf-xet | 1.5.2 | Apache-2.0 |
| httpcore | 1.0.9 | BSD-3-Clause |
| httptools | 0.7.1 | MIT |
| httpx | 0.28.1 | BSD-3-Clause |
| huggingface-hub | 1.26.0 | Apache-2.0 |
| idna | 3.15 | BSD-3-Clause |
| importlib-resources | 7.1.0 | Apache-2.0 |
| ipython | 8.39.0 | BSD-3-Clause |
| itsdangerous | 2.2.0 | BSD License |
| jedi | 0.20.0 | MIT |
| jinja2 | 3.1.6 | BSD License |
| joblib | 1.5.3 | BSD-3-Clause |
| kiwisolver | 1.5.0 | BSD License |
| lazy-loader | 0.5 | BSD-3-Clause |
| librosa | 0.11.0 | ISC |
| llvmlite | 0.47.0 | BSD-2-Clause（wheel のメタデータに表記がなく、同梱の LICENSE から読んだ） |
| markdown | 3.10.2 | BSD-3-Clause |
| markdown-it-py | 4.2.0 | MIT License |
| markdown2 | 2.5.5 | MIT |
| markupsafe | 3.0.3 | BSD-3-Clause |
| matplotlib | 3.10.9 | PSF 系の matplotlib License（Classifier: Python Software Foundation License） |
| matplotlib-inline | 0.2.2 | BSD-3-Clause |
| mdurl | 0.1.2 | MIT License |
| mpmath | 1.3.0 | BSD |
| msgpack | 1.1.2 | Apache-2.0 |
| multidict | 6.7.1 | Apache License 2.0 |
| multiprocess | 0.70.19 | BSD-3-Clause |
| networkx | 3.4.2 | BSD License |
| numba | 0.65.1 | BSD |
| numpy | 2.2.6 | BSD License |
| orjson | 3.11.9 | MPL-2.0 AND (Apache-2.0 OR MIT) |
| packaging | 26.2 | Apache-2.0 OR BSD-2-Clause |
| pandas | 2.3.3 | BSD License |
| parso | 0.8.7 | MIT |
| peft | 0.19.1 | Apache |
| pillow | 12.2.0 | MIT-CMU |
| platformdirs | 4.9.6 | MIT |
| pooch | 1.9.0 | BSD-3-Clause |
| prompt-toolkit | 3.0.52 | BSD License |
| propcache | 0.5.2 | Apache-2.0 |
| protobuf | 3.19.6 | 3-Clause BSD License |
| psutil | 7.2.2 | BSD-3-Clause |
| pure-eval | 0.2.3 | MIT |
| pyaml | 26.2.1 | WTFPL |
| pyarrow | 24.0.0 | Apache-2.0 |
| pycparser | 3.0 | BSD-3-Clause |
| pydantic | 2.13.4 | MIT |
| pydantic-core | 2.46.4 | MIT |
| pydantic-settings | 2.14.1 | MIT |
| pydub | 0.25.1 | MIT |
| pygments | 2.20.0 | BSD-2-Clause |
| pyloudnorm | 0.2.0 | MIT |
| pyparsing | 3.3.2 | MIT |
| pystoi | 0.4.1 | MIT |
| python-dateutil | 2.9.0.post0 | Dual License |
| python-dotenv | 1.2.2 | BSD-3-Clause |
| python-multipart | 0.0.28 | Apache-2.0 |
| pytz | 2026.2 | MIT |
| pyyaml | 6.0.3 | MIT |
| regex | 2026.5.9 | Apache-2.0 AND CNRI-Python |
| requests | 2.34.0 | Apache-2.0 |
| rich | 15.0.0 | MIT |
| safehttpx | 0.1.7 | MIT License |
| safetensors | 0.7.0 | Apache Software License |
| scikit-learn | 1.7.2 | BSD-3-Clause |
| scipy | 1.15.3 | BSD License |
| semantic-version | 2.10.0 | BSD |
| sentencepiece | 0.1.99 | Apache |
| sentry-sdk | 2.60.0 | MIT |
| setuptools | 81.0.0 | MIT |
| shellingham | 1.5.4 | ISC License |
| six | 1.17.0 | MIT |
| smmap | 5.0.3 | BSD-3-Clause |
| soundfile | 0.13.1 | BSD 3-Clause License |
| soxr | 1.1.0 | LGPL-2.1-or-later |
| stack-data | 0.6.3 | MIT |
| starlette | 1.0.0 | BSD-3-Clause |
| sympy | 1.14.0 | BSD |
| tensorboard | 2.20.0 | Apache 2.0 |
| tensorboard-data-server | 0.7.2 | Apache 2.0 |
| termcolor | 3.3.0 | MIT |
| threadpoolctl | 3.6.0 | BSD-3-Clause |
| tokenizers | 0.22.2 | Apache Software License |
| tomlkit | 0.14.0 | MIT |
| torch | 2.10.0+cu128 | BSD-3-Clause |
| torch-stoi | 0.2.3 | MIT |
| torchao | 0.16.0 | BSD-3-Clause（メタデータに表記がなく、同梱の LICENSE から読んだ） |
| torchaudio | 2.10.0+cu128 | BSD License |
| torchcodec | 0.10.0 | BSD-3-Clause |
| torchdata | 0.11.0 | BSD |
| tqdm | 4.67.3 | MPL-2.0 AND MIT |
| traitlets | 5.15.0 | BSD License |
| transformers | 5.12.1 | Apache 2.0 License |
| typer | 0.25.1 | MIT |
| typing-extensions | 4.15.0 | PSF-2.0 |
| typing-inspection | 0.4.2 | MIT |
| tzdata | 2026.2 | Apache-2.0 |
| urllib3 | 2.7.0 | MIT |
| uvicorn | 0.46.0 | BSD-3-Clause |
| wandb | 0.25.0 | MIT License |
| watchfiles | 1.1.1 | MIT |
| wcwidth | 0.7.0 | MIT |
| websockets | 16.0 | BSD-3-Clause |
| werkzeug | 3.1.8 | BSD-3-Clause |
| xxhash | 3.7.0 | BSD |
| yarl | 1.23.0 | Apache-2.0 |
| argbind | 0.3.9 | MIT License |
| julius | 0.2.7 | MIT License |
| randomname | 0.2.1 | MIT License |

- 上の表の `llvmlite`・`torchao` は wheel のメタデータにライセンスの表記がなく、同梱の `LICENSE` を読んで記録しました。`matplotlib` は matplotlib 自身のライセンス（PSF 系）です。

### Irodori-TTS の重みと依存ファイル

MAM のローカルランタイム管理は、Irodori-TTS のモデルの重み・音声コーデックの重み・SilentCipher の透かしの重みを、固定の URL（Hugging Face のコミット固定）と SHA-256 で検証します。**MAM は再配布せず**、利用者の操作で取得元から取得します。モデルは利用者がモデルマネージャーで選び（既定は v4.1-Small）、選んだモデルの重みだけを取得します。出典・ライセンス・商用利用の可否・使うときの制限は、モデルマネージャーの画面と MCP（`list_local_runtime_models`）にも表示します。

| 名前 | 出典 | ライセンス | 商用利用 |
|---|---|---|---|
| Irodori-TTS v4.1-Small・v4.1-Small-MF の重みとトークナイザー | <https://huggingface.co/Aratako/Irodori-TTS-v4.1-Small>・<https://huggingface.co/Aratako/Irodori-TTS-v4.1-Small-MF> | [MIT License](https://huggingface.co/Aratako/Irodori-TTS-v4.1-Small)（モデルカードの `license: mit`） | 可 |
| Irodori-TTS v4-Large（fp32）・v4-Large-Quantized（int8・int4・fp8）の重みとトークナイザー | <https://huggingface.co/Aratako/Irodori-TTS-v4-Large>・<https://huggingface.co/Aratako/Irodori-TTS-v4-Large-Quantized> | [Gemma Terms of Use](https://ai.google.dev/gemma/terms) と[禁止用途ポリシー](https://ai.google.dev/gemma/prohibited_use_policy)（モデルカードの `license: gemma`） | **条件つき**（規約と禁止用途ポリシーに従う。選ぶときに利用者の同意が要る） |
| Semantic-DACVAE-Japanese-32dim の重み（音声コーデック） | <https://huggingface.co/Aratako/Semantic-DACVAE-Japanese-32dim> | [MIT License](https://huggingface.co/Aratako/Semantic-DACVAE-Japanese-32dim)（元の `facebook/dacvae-watermarked` は Apache-2.0） | 可 |
| SilentCipher（44.1 kHz）の重み 4 ファイル（見えない透かし） | <https://huggingface.co/sony/silentcipher> | **ライセンスの表示を確認できていない**（コードは MIT。配布元に表示がない） | **はっきりしない**（Irodori-TTS は必ずこの透かしを入れるので省けない。商用で使う前に配布元（Sony Research）に確認してください） |

- 取得先のコミットは、v4.1-Small `2b28324d…`、v4.1-Small-MF `ccc78f5d…`、v4-Large `2e0c5542…`、v4-Large-Quantized `bdef9592…`、Semantic-DACVAE `47376ee2…`、sony/silentcipher `a1c4d021…`。
- 各ライセンスは、モデルカードの表記を読んで記録したもので、**原文との突き合わせや学習データのライセンスの確認はしていません（未確認）**。
- **声のコピー（参照音声）の使い方の制限**: モデルカードは、本人の同意なしに実在の人（声優・有名人など）の声をコピーしたりなりすましに使ったりすること、人をだます合成音声・ディープフェイクに使うことを禁じています。MAM は画面・MCP でこの制限を伝えますが、守る責任は利用者にあります。

