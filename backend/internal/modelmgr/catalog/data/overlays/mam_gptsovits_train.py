"""mam's driver of the GPT-SoVITS fine-tuning (spec/architecture/model-manager/local-runtime.md section 16, Issue #936).

mam runs one step per process: python -I -u -X utf8 mam_gptsovits_train.py <step> <job.json>, in the directory of the extracted GPT-SoVITS
package. The step is one of a fixed list, each of which runs one fixed script of the vendor's package (the same ones its webui.py runs for
"1A / 1B / 1C dataset formatting", "1Aa SoVITS training" and "1Ab GPT training"); nothing in job.json names a script, a command or a path
outside the working directory of the job (mam wrote it, and this script refuses what it does not recognise):

    text      GPT_SoVITS/prepare_datasets/1-get-text.py        phonemes and BERT features        -> 2-name2text.txt
    hubert    GPT_SoVITS/prepare_datasets/2-get-hubert-wav32k.py   32 kHz audio and SSL features
    semantic  GPT_SoVITS/prepare_datasets/3-get-semantic.py    semantic tokens                   -> 6-name2semantic.tsv
    sovits    GPT_SoVITS/s2_train.py                           S2 (SoVITS) training              -> <out>/sovits.pth
    gpt       GPT_SoVITS/s1_train.py                           S1 (GPT) training                 -> <out>/gpt.ckpt

The vendor's webui.py assembles the settings of a step the same way (environment variables for the three preparation scripts, a temporary
config file for the two trainings); this script does it with the values of job.json. The output of the vendor's script goes to stderr
(mam keeps its tail for a failure message); stdout carries only the lines "MAM_TRAIN {json}" that say how far the step is:

    {"step": "hubert", "done": 12, "total": 30}      files of the step that are done
    {"step": "sovits", "done": 3, "total": 8}        epochs that are done
    {"step": "gpt", "done": 5, "total": 15}

It needs the standard library only (it is tested with a fake package, without torch); only the settings of the GPT trainer need PyYAML,
which the package has (its own webui.py reads and writes the same file).
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import threading
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))  # -I does not put the script's directory there

from mam_gptsovits_train_job import *  # noqa: E402,F401,F403  (the names the steps and the tests of the driver use)
from mam_gptsovits_train_job import JobError, load_job  # noqa: E402

POLL_SECONDS = 2.0


def sovits_epochs_done(job: dict) -> int:
    """The last "====> Epoch: N" the S2 trainer wrote to its train.log (utils.get_logger(exp_dir))."""
    best = 0
    try:
        with open(os.path.join(job["exp_dir"], "train.log"), encoding="utf-8", errors="replace") as f:
            for line in f:
                m = re.search(r"====> Epoch: (\d+)", line)
                if m:
                    best = max(best, int(m.group(1)))
    except OSError:
        pass
    return best


GPT_WEIGHT = re.compile(r"^(?P<name>.+)-e(?P<epoch>\d+)\.ckpt$")


def gpt_weights(job: dict) -> dict:
    """epoch -> path of the half weights the S1 trainer wrote after that epoch (<exp_name>-e<N>.ckpt)."""
    found = {}
    try:
        names = os.listdir(job["gpt_weights_dir"])
    except OSError:
        return found
    for name in names:
        m = GPT_WEIGHT.match(name)
        if m and m.group("name") == job["exp_name"]:
            found[int(m.group("epoch"))] = os.path.join(job["gpt_weights_dir"], name)
    return found


def gpt_epochs_done(job: dict) -> int:
    """Epochs that are finished: the newest weights file of the S1 trainer. Weights of the earlier epochs are removed, so that the disk holds
    one of them and not one per epoch."""
    found = gpt_weights(job)
    if not found:
        return 0
    latest = max(found)
    for epoch, path in found.items():
        if epoch < latest:
            try:
                os.remove(path)
            except OSError:
                pass
    return latest


def progress_of(step: str, job: dict):
    """A function that says (done, total) of the running step, or None for a step that has no count to report."""
    if step == "hubert":
        done_dir = os.path.join(job["exp_dir"], "4-cnhubert")

        def hubert():
            try:
                return sum(1 for n in os.listdir(done_dir) if n.endswith(".pt")), job["clips"]
            except OSError:
                return 0, job["clips"]

        return hubert
    if step == "sovits":
        return lambda: (sovits_epochs_done(job), job["epochs_sovits"])
    if step == "gpt":
        return lambda: (gpt_epochs_done(job), job["epochs_gpt"])
    return None


def pump(stream, sink) -> None:
    for line in iter(stream.readline, ""):
        sink.write(line)
        sink.flush()


def run_vendor(step: str, job: dict, args: list, env: dict) -> int:
    script = SCRIPTS[step]
    if not os.path.isfile(script):
        raise JobError("the package has no %s" % script)
    child = subprocess.Popen(  # noqa: S603 - script comes from the fixed SCRIPTS table, args are built here
        [sys.executable, "-s", script] + args,
        env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, encoding="utf-8", errors="replace",
    )
    reader = threading.Thread(target=pump, args=(child.stdout, sys.stderr), daemon=True)
    reader.start()
    progress, last = progress_of(step, job), None
    while True:
        try:
            code = child.wait(timeout=POLL_SECONDS)
        except subprocess.TimeoutExpired:
            code = None
        if progress is not None:
            now = progress()
            if now != last:
                emit(step, *now)
                last = now
        if code is not None:
            break
    reader.join(timeout=5)
    if progress is not None:
        emit(step, *progress())
    return code


def finish_sovits(job: dict) -> None:
    prefix = "%s_e%d_s" % (job["exp_name"], job["epochs_sovits"])
    names = sorted(n for n in os.listdir(job["sovits_weights_dir"]) if n.startswith(prefix) and n.endswith(".pth"))
    if not names:
        raise JobError("the S2 trainer wrote no weights for epoch %d" % job["epochs_sovits"])
    os.makedirs(job["out_dir"], exist_ok=True)
    shutil.move(os.path.join(job["sovits_weights_dir"], names[-1]), os.path.join(job["out_dir"], "sovits.pth"))


def finish_gpt(job: dict) -> None:
    last = gpt_weights(job).get(job["epochs_gpt"])
    if last is None:
        raise JobError("the S1 trainer wrote no weights for epoch %d" % job["epochs_gpt"])
    os.makedirs(job["out_dir"], exist_ok=True)
    shutil.move(last, os.path.join(job["out_dir"], "gpt.ckpt"))


def run_step(step: str, job: dict) -> int:
    root = os.getcwd()
    write_users_pth(root)
    half = detect_half(root)
    env = step_env(step, job, half)
    args = []
    if step == "sovits":
        args = ["--config", sovits_config(job, half)]
    elif step == "gpt":
        args = ["--config_file", gpt_config(job, half)]
    os.makedirs(job["exp_dir"], exist_ok=True)
    code = run_vendor(step, job, args, env)
    if code != 0:
        return code
    exp = job["exp_dir"]
    if step == "text":
        merge_part(os.path.join(exp, "2-name2text-0.txt"), os.path.join(exp, "2-name2text.txt"))
    elif step == "semantic":
        merge_part(os.path.join(exp, "6-name2semantic-0.tsv"), os.path.join(exp, "6-name2semantic.tsv"), "item_name\tsemantic_audio")
    elif step == "sovits":
        finish_sovits(job)
    elif step == "gpt":
        finish_gpt(job)
    return 0


def main(argv: list) -> int:
    if len(argv) != 3 or argv[1] not in SCRIPTS:
        print("usage: mam_gptsovits_train.py <%s> <job.json>" % "|".join(SCRIPTS), file=sys.stderr)
        return 2
    try:
        job = load_job(argv[2])
        return run_step(argv[1], job)
    except (JobError, OSError, ValueError) as e:
        print("mam_gptsovits_train: %s" % e, file=sys.stderr)
        return 3


if __name__ == "__main__":
    sys.exit(main(sys.argv))
