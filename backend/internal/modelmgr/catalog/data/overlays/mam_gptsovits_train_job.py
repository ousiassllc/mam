"""The job file and the environment of mam's driver of the GPT-SoVITS fine-tuning (mam_gptsovits_train.py; spec/architecture/model-manager/
local-runtime.md section 18, Issue #936): the paths and limits of job.json (what mam wrote and nothing else is accepted), the table of the five
fixed steps, and the environment variables and settings files the vendor's scripts are started with. Split from the driver only for its size."""

from __future__ import annotations

import json
import os
import re
import shutil
import subprocess
import sys

VERSION = "v2"  # the pretrained base of the runtime (the weights of data/assets/gpt-sovits.json); the one this mam fine-tunes
PRETRAINED = "GPT_SoVITS/pretrained_models"
BERT_DIR = PRETRAINED + "/chinese-roberta-wwm-ext-large"
SSL_DIR = PRETRAINED + "/chinese-hubert-base"
PRETRAINED_S2G = PRETRAINED + "/gsv-v2final-pretrained/s2G2333k.pth"
PRETRAINED_S2D = PRETRAINED + "/gsv-v2final-pretrained/s2D2333k.pth"
PRETRAINED_S1 = PRETRAINED + "/gsv-v2final-pretrained/s1bert25hz-5kh-longer-epoch=12-step=369668.ckpt"
S2_CONFIG = "GPT_SoVITS/configs/s2.json"
S1_CONFIG = "GPT_SoVITS/configs/s1longer-v2.yaml"
PREPARE = "GPT_SoVITS/prepare_datasets/"

# step -> the script of the package it runs. This table is the only thing that is ever started.
SCRIPTS = {
    "text": PREPARE + "1-get-text.py",
    "hubert": PREPARE + "2-get-hubert-wav32k.py",
    "semantic": PREPARE + "3-get-semantic.py",
    "sovits": "GPT_SoVITS/s2_train.py",
    "gpt": "GPT_SoVITS/s1_train.py",
}

LIMITS = {"epochs_sovits": (1, 25), "epochs_gpt": (1, 50), "batch": (1, 64)}  # the sliders of the vendor's webui, and its S2 maximum for the batch
EXP_NAME = re.compile(r"^[A-Za-z0-9_-]{1,64}$")
GPU = re.compile(r"^[0-9]{1,2}$")
SITE_PACKAGES = "runtime/Lib/site-packages"


class JobError(Exception):
    """A job.json that is not what mam writes, or a step that did not produce what the next one needs."""


def inside(path: str, root: str) -> bool:
    path, root = os.path.realpath(path), os.path.realpath(root)
    return path == root or path.startswith(root + os.sep)


def load_job(path: str) -> dict:
    with open(path, encoding="utf-8") as f:
        raw = json.load(f)
    if not isinstance(raw, dict):
        raise JobError("job.json is not an object")
    work = raw.get("work_dir")
    if not isinstance(work, str) or not os.path.isabs(work) or not os.path.isdir(work):
        raise JobError("work_dir is not an existing absolute directory")
    job = {"work_dir": work}
    for key in ("list_path", "wav_dir", "exp_dir", "out_dir", "sovits_weights_dir", "gpt_weights_dir"):
        value = raw.get(key)
        if not isinstance(value, str) or not os.path.isabs(value) or not inside(value, work) or "\0" in value or "\n" in value:
            raise JobError("%s is not a path inside work_dir" % key)
        job[key] = value
    if not isinstance(raw.get("exp_name"), str) or not EXP_NAME.match(raw["exp_name"]):
        raise JobError("exp_name is not a plain name")
    if not isinstance(raw.get("gpu"), str) or not GPU.match(raw["gpu"]):
        raise JobError("gpu is not a device number")
    job["exp_name"], job["gpu"] = raw["exp_name"], raw["gpu"]
    for key, limits in (("epochs_sovits", "epochs_sovits"), ("epochs_gpt", "epochs_gpt"), ("batch_sovits", "batch"), ("batch_gpt", "batch")):
        value = raw.get(key)
        low, high = LIMITS[limits]
        if not isinstance(value, int) or isinstance(value, bool) or not low <= value <= high:
            raise JobError("%s is not an integer from %d to %d" % (key, low, high))
        job[key] = value
    total = raw.get("clips")
    if not isinstance(total, int) or isinstance(total, bool) or total < 1:
        raise JobError("clips is not a count")
    job["clips"] = total
    return job


def emit(step: str, done: int, total: int) -> None:
    print("MAM_TRAIN " + json.dumps({"step": step, "done": done, "total": total}), flush=True)


def write_users_pth(root: str) -> None:
    """The vendor's webui.py writes this file into the Python of the package at each start: the scripts it runs find their own modules
    (config, tools, GPT_SoVITS, AR, ...) through it (the embedded Python ignores PYTHONPATH). A step that mam starts without webui.py
    writes the same file."""
    site = os.path.join(root, *SITE_PACKAGES.split("/"))
    if not os.path.isdir(site):
        return  # not the layout of the package (a test): the scripts bring their own paths
    paths = [root, "GPT_SoVITS/BigVGAN", "tools", "tools/asr", "GPT_SoVITS"]
    lines = [root] + [os.path.join(root, *p.split("/")) for p in paths[1:]]
    with open(os.path.join(site, "users.pth"), "w", encoding="utf-8") as f:
        f.write("\n".join(lines))


def detect_half(root: str) -> bool:
    """Whether the GPU does half precision, as the vendor's config.py says (webui.py imports is_half from it)."""
    code = "import sys; sys.path.insert(0, %r); import config; print('MAM_HALF', bool(config.is_half))" % root
    out = subprocess.run([sys.executable, "-s", "-c", code], cwd=root, capture_output=True, text=True, timeout=600, check=False)
    for line in out.stdout.splitlines():
        if line.startswith("MAM_HALF "):
            return line.split(" ", 1)[1].strip() == "True"
    raise JobError("config.py of the package did not say whether the GPU does half precision: " + out.stderr[-300:])


def count_lines(path: str) -> int:
    try:
        with open(path, encoding="utf-8") as f:
            return sum(1 for line in f if line.strip())
    except OSError:
        return 0


def merge_part(part: str, final: str, header: str = "") -> int:
    """The vendor's webui.py merges the output of the parts of a step into one file; there is one part here."""
    with open(part, encoding="utf-8") as f:
        lines = f.read().strip("\n").split("\n")
    lines = [line for line in lines if line]
    if not lines:
        raise JobError("%s is empty: the step produced nothing" % os.path.basename(part))
    with open(final, "w", encoding="utf-8") as f:
        f.write("\n".join(([header] if header else []) + lines) + "\n")
    os.remove(part)
    return len(lines)


def base_env(job: dict, half: bool) -> dict:
    env = dict(os.environ)
    env.update(
        {
            "version": VERSION,
            "exp_name": job["exp_name"],
            "opt_dir": job["exp_dir"],
            "inp_text": job["list_path"],
            "inp_wav_dir": job["wav_dir"],
            "i_part": "0",
            "all_parts": "1",
            "_CUDA_VISIBLE_DEVICES": job["gpu"],
            "is_half": str(half),
        }
    )
    return env


def step_env(step: str, job: dict, half: bool) -> dict:
    env = base_env(job, half)
    if step == "text":
        env["bert_pretrained_dir"] = BERT_DIR
    elif step == "hubert":
        env["cnhubert_base_dir"] = SSL_DIR
        env["sv_path"] = PRETRAINED + "/sv/pretrained_eres2netv2w24s4ep4.ckpt"  # read by the script, used by the v2Pro models only
    elif step == "semantic":
        env["pretrained_s2G"] = PRETRAINED_S2G
        env["s2config_path"] = S2_CONFIG
    elif step == "gpt":
        env["_CUDA_VISIBLE_DEVICES"] = job["gpu"]
        env["hz"] = "25hz"
    return env


def sovits_config(job: dict, half: bool) -> str:
    """The temporary config of the S2 trainer: GPT_SoVITS/configs/s2.json with the settings of webui.py's open1Ba."""
    with open(S2_CONFIG, encoding="utf-8") as f:
        data = json.load(f)
    batch = job["batch_sovits"]
    if not half:
        data["train"]["fp16_run"] = False
        batch = max(1, batch // 2)
    epochs = job["epochs_sovits"]
    data["train"].update(
        {
            "batch_size": batch,
            "epochs": epochs,
            "text_low_lr_rate": 0.4,
            "pretrained_s2G": PRETRAINED_S2G,
            "pretrained_s2D": PRETRAINED_S2D,
            "if_save_latest": True,  # keep only the newest full checkpoint (the weights of the end are saved separately)
            "if_save_every_weights": True,
            "save_every_epoch": epochs,  # the weights are written once, after the last epoch
            "gpu_numbers": job["gpu"],
            "grad_ckpt": False,
            "lora_rank": 32,
        }
    )
    data["model"]["version"] = VERSION
    data["data"]["exp_dir"] = data["s2_ckpt_dir"] = job["exp_dir"]
    data["save_weight_dir"] = job["sovits_weights_dir"]
    data["name"] = job["exp_name"]
    data["version"] = VERSION
    path = os.path.join(job["work_dir"], "tmp_s2.json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump(data, f)
    os.makedirs(os.path.join(job["exp_dir"], "logs_s2_" + VERSION), exist_ok=True)
    os.makedirs(job["sovits_weights_dir"], exist_ok=True)
    return path


def gpt_config(job: dict, half: bool) -> str:
    """The temporary config of the S1 trainer: GPT_SoVITS/configs/s1longer-v2.yaml with the settings of webui.py's open1Bb."""
    import yaml

    with open(S1_CONFIG, encoding="utf-8") as f:
        data = yaml.load(f.read(), Loader=yaml.FullLoader)
    batch = job["batch_gpt"]
    if not half:
        data["train"]["precision"] = "32"
        batch = max(1, batch // 2)
    data["train"].update(
        {
            "batch_size": batch,
            "epochs": job["epochs_gpt"],
            "save_every_n_epoch": 1,  # an epoch is seen finished by its file; older weights are removed as the next ones appear
            "if_save_every_weights": True,
            "if_save_latest": True,
            "if_dpo": False,
            "half_weights_save_dir": job["gpt_weights_dir"],
            "exp_name": job["exp_name"],
        }
    )
    data["pretrained_s1"] = PRETRAINED_S1
    data["train_semantic_path"] = os.path.join(job["exp_dir"], "6-name2semantic.tsv")
    data["train_phoneme_path"] = os.path.join(job["exp_dir"], "2-name2text.txt")
    data["output_dir"] = os.path.join(job["exp_dir"], "logs_s1_" + VERSION)
    path = os.path.join(job["work_dir"], "tmp_s1.yaml")
    with open(path, "w", encoding="utf-8") as f:
        f.write(yaml.dump(data, default_flow_style=False))
    os.makedirs(data["output_dir"], exist_ok=True)
    os.makedirs(job["gpt_weights_dir"], exist_ok=True)
    return path
