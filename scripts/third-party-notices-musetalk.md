## MuseTalk 1.5（利用者が取得する別プログラム）

MAM のローカルランタイム管理（Issue #921）は、利用者の操作で MuseTalk 1.5（既存の動画と音声から口元を合わせた動画を作る。コードは GitHub の `TMElyralab/MuseTalk` のコミット `0a89dec45a0192b824e3cf4daf96c239440c5ed8`）の実行環境を、MAM の実行ファイルと同じ場所の `localai\musetalk` ディレクトリに組み立て、別のプロセス（MAM が同梱する薄い HTTP ラッパー `mam_musetalk_server.py`、`127.0.0.1` だけで待ち受け）として起動します。MuseTalk のコードは MIT ライセンスで、README に「The code of MuseTalk is released under the MIT License. There is no limitation for both academic and commercial usage.」「The trained model are available for any purpose, even commercially.」とあります。**MAM のインストーラー・ポータブル版・リリースには MuseTalk・Python・下の Python パッケージ・PyTorch のバイトを一切含めません**。MuseTalk は Windows 向けの配布物を出していないため、MAM は公式の手順（Python 3.10・PyTorch 2.0.1 + CUDA 11.8・mmcv 2.0.1・mmdet 3.1.0・mmpose 1.1.0 ほか）と同じ版を、次の公式の配布先から固定の URL と SHA-256 で取得して組み立てます（`pip` も `setup.py` も実行しません）。対応するのは Windows の x64 と NVIDIA の GPU（RTX 40 シリーズまで）だけです。

- MuseTalk のライセンス（MIT）とソースコード: <https://github.com/TMElyralab/MuseTalk/tree/0a89dec45a0192b824e3cf4daf96c239440c5ed8>（ライセンス文は <https://github.com/TMElyralab/MuseTalk/blob/0a89dec45a0192b824e3cf4daf96c239440c5ed8/LICENSE>）。MAM が同梱するラッパー（`backend/internal/modelmgr/catalog/data/overlays/musetalk_server.py`）の口パクの生成の手順は、MuseTalk の `scripts/inference.py`（MIT、Copyright (c) 2024 Tencent Music Entertainment Group）に従っています。
- Python 3.10.11 の Windows 用の組み込み版（<https://www.python.org/ftp/python/3.10.11/python-3.10.11-embed-amd64.zip>。Python Software Foundation License。zip の中の `LICENSE.txt`）。
- Python パッケージの wheel 82 個。PyTorch（`torch`・`torchvision`）は <https://download.pytorch.org/whl/cu118/>、mmcv は <https://download.openmmlab.com/mmcv/dist/cu118/torch2.0.0/>、その他は PyPI（`files.pythonhosted.org`）から取得します。ライセンスは各 wheel のメタデータの表記です（**原文との突き合わせはしていません（未確認）**）。各 wheel の中の `*.dist-info` に、それぞれのライセンス文が入っています。
- **注意が要るもの**: `torch` の CUDA 版の wheel は NVIDIA の CUDA ランタイムと cuDNN の DLL を含み、NVIDIA のライセンスに従います（**条件は確認していません（未確認）**）。`opencv-python` の wheel は LGPL の FFmpeg の DLL、`soundfile` の wheel は LGPL の libsndfile、`soxr` は LGPL-2.1-or-later の libsoxr を含みます（MAM は再配布せず、利用者の PC に取得します）。`certifi`・`tqdm` は MPL-2.0 です。

| パッケージ | 版 | ライセンス（wheel のメタデータの表記） |
|---|---|---|
| accelerate | 0.28.0 | Apache-2.0 |
| addict | 2.4.0 | MIT |
| audioread | 3.1.0 | MIT |
| certifi | 2022.12.7 | MPL-2.0 |
| cffi | 2.1.1 | MIT-0 |
| charset-normalizer | 2.1.1 | MIT |
| cloudpickle | 3.1.2 | BSD-3-Clause |
| colorama | 0.4.6 | BSD-3-Clause |
| contourpy | 1.3.2 | BSD-3-Clause |
| cycler | 0.12.1 | BSD-3-Clause |
| decorator | 5.3.1 | BSD-2-Clause |
| diffusers | 0.30.2 | Apache-2.0 |
| einops | 0.8.1 | MIT |
| ffmpeg-python | 0.2.0 | Apache-2.0 |
| filelock | 4.1.0 | MIT |
| fonttools | 4.65.0 | MIT |
| fsspec | 2026.9.0 | BSD-3-Clause |
| future | 1.0.0 | MIT |
| huggingface-hub | 0.30.2 | Apache-2.0 |
| idna | 3.4 | BSD-3-Clause |
| importlib_metadata | 9.0.1 | Apache-2.0 |
| Jinja2 | 3.1.6 | BSD-3-Clause |
| joblib | 1.6.0 | BSD-3-Clause |
| json-tricks | 3.17.3 | BSD-3-Clause |
| kiwisolver | 1.5.1 | BSD-3-Clause |
| lazy-loader | 0.6 | BSD-3-Clause |
| librosa | 0.11.0 | ISC |
| llvmlite | 0.50.0 | BSD-2-Clause |
| markdown-it-py | 4.2.0 | MIT |
| MarkupSafe | 3.0.3 | BSD-3-Clause |
| matplotlib | 3.10.9 | Matplotlib License（PSF ベース） |
| mdurl | 0.1.2 | MIT |
| mmcv | 2.0.1 | Apache-2.0 |
| mmdet | 3.1.0 | Apache-2.0 |
| mmengine | 0.10.7 | Apache-2.0 |
| mmpose | 1.1.0 | Apache-2.0 |
| mpmath | 1.3.0 | BSD-3-Clause |
| msgpack | 1.2.4 | Apache-2.0 |
| munkres | 3.0.0 | Apache-2.0 |
| networkx | 3.4.2 | BSD-3-Clause |
| numba | 0.68.1 | BSD-2-Clause |
| numpy | 1.23.5 | BSD-3-Clause |
| omegaconf | 2.4.0 | BSD-3-Clause |
| opencv-python | 4.9.0.80 | Apache-2.0（wheel は LGPL の FFmpeg の DLL を含む） |
| packaging | 26.3 | Apache-2.0 または BSD-2-Clause |
| pillow | 12.3.0 | MIT-CMU |
| platformdirs | 4.12.4 | MIT |
| pooch | 1.9.0 | BSD-3-Clause |
| psutil | 7.2.2 | BSD-3-Clause |
| pycocotools | 2.0.11 | BSD-2-Clause |
| pycparser | 3.11 | BSD-3-Clause |
| Pygments | 2.21.0 | BSD-2-Clause |
| pyparsing | 3.3.3 | MIT |
| python-dateutil | 3.0.0 | BSD-3-Clause と Apache-2.0 の二重 |
| PyYAML | 6.0.3 | MIT |
| regex | 2026.9.29 | Apache-2.0 と CNRI-Python |
| requests | 2.28.1 | Apache-2.0 |
| rich | 15.0.0 | MIT |
| safetensors | 0.8.0 | Apache-2.0 |
| scikit-learn | 1.7.2 | BSD-3-Clause |
| scipy | 1.15.3 | BSD-3-Clause |
| setuptools | 69.5.1 | MIT |
| shapely | 2.1.2 | BSD-3-Clause |
| six | 1.17.0 | MIT |
| soundfile | 0.12.1 | BSD-3-Clause |
| soxr | 1.1.0 | LGPL-2.1-or-later |
| sympy | 1.14.0 | BSD-3-Clause |
| termcolor | 3.3.0 | MIT |
| terminaltables | 3.1.10 | MIT |
| threadpoolctl | 3.7.0 | BSD-3-Clause |
| tokenizers | 0.15.2 | Apache-2.0 |
| tomli | 2.5.0 | MIT |
| torch | 2.0.1+cu118 | BSD-3-Clause（wheel は NVIDIA の CUDA・cuDNN の DLL を含む） |
| torchvision | 0.15.2+cu118 | BSD-3-Clause |
| tqdm | 4.70.1 | MPL-2.0 と MIT |
| transformers | 4.39.2 | Apache-2.0 |
| typing_extensions | 4.16.0 | PSF-2.0 |
| tzdata | 2026.6 | Apache-2.0 |
| urllib3 | 1.26.13 | MIT |
| xtcocotools | 1.14.3 | MIT |
| yapf | 0.43.0 | Apache-2.0 |
| zipp | 4.1.2 | MIT |

