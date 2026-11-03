"""Tests of mam's driver of the GPT-SoVITS fine-tuning (data/overlays/mam_gptsovits_train.py) against a FAKE package: scripts that write what the
vendor's scripts write (the files, the "====> Epoch: N" lines of the S2 train.log, the weights of the S1 trainer), so that neither torch nor
GPT-SoVITS is needed. Run by the Go test TestGPTSoVITSTrainingDriver (python3 testdata/test_gptsovits_train.py <path of the script>).

What this cannot show: that the real scripts of the package take these settings and write these files. That is read from the vendor's source
(tag 20250606v2pro; spec local-runtime.md section 16.2) and stays unconfirmed until it runs on a Windows PC with an NVIDIA GPU."""

import importlib.util
import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest

try:
    import yaml
except ImportError:  # the package has PyYAML; a machine that runs these tests may not
    yaml = None

SCRIPT = os.path.abspath(sys.argv.pop(1))
spec = importlib.util.spec_from_file_location("mam_gptsovits_train", SCRIPT)
tr = importlib.util.module_from_spec(spec)
spec.loader.exec_module(tr)

FAKE_SCRIPTS = {
    "config.py": "is_half = HALF\n",
    "GPT_SoVITS/prepare_datasets/1-get-text.py": """
import os
e = os.environ
assert e["version"] == "v2" and e["is_half"] in ("True", "False"), e["version"]
assert e["bert_pretrained_dir"].endswith("chinese-roberta-wwm-ext-large")
assert e["_CUDA_VISIBLE_DEVICES"] == "0" and e["i_part"] == "0" and e["all_parts"] == "1"
os.makedirs(e["opt_dir"], exist_ok=True)
with open(e["inp_text"], encoding="utf8") as f:
    lines = f.read().strip("\\n").split("\\n")
with open(e["opt_dir"] + "/2-name2text-0.txt", "w", encoding="utf8") as f:
    f.write("\\n".join("%s\\tphones\\t[1]\\t%s" % (l.split("|")[0], l.split("|")[3]) for l in lines) + "\\n")
""",
    "GPT_SoVITS/prepare_datasets/2-get-hubert-wav32k.py": """
import os
e = os.environ
assert e["cnhubert_base_dir"].endswith("chinese-hubert-base")
d = e["opt_dir"] + "/4-cnhubert"
os.makedirs(d, exist_ok=True)
with open(e["inp_text"], encoding="utf8") as f:
    for l in f.read().strip("\\n").split("\\n"):
        open(d + "/" + l.split("|")[0] + ".pt", "w").close()
""",
    "GPT_SoVITS/prepare_datasets/3-get-semantic.py": """
import os
e = os.environ
assert e["pretrained_s2G"].endswith("s2G2333k.pth") and e["s2config_path"] == "GPT_SoVITS/configs/s2.json"
with open(e["inp_text"], encoding="utf8") as f:
    names = [l.split("|")[0] for l in f.read().strip("\\n").split("\\n")]
with open(e["opt_dir"] + "/6-name2semantic-0.tsv", "w", encoding="utf8") as f:
    f.write("\\n".join(n + "\\t1 2 3" for n in names) + "\\n")
""",
    "GPT_SoVITS/s2_train.py": """
import json, os, sys, time
cfg = json.load(open(sys.argv[sys.argv.index("--config") + 1]))
t = cfg["train"]
assert cfg["version"] == "v2" and cfg["model"]["version"] == "v2" and t["save_every_epoch"] == t["epochs"]
assert t["pretrained_s2D"].endswith("s2D2333k.pth") and t["pretrained_s2G"].endswith("s2G2333k.pth")
if os.environ.get("FAKE_FAIL") == "sovits":
    print("CUDA out of memory", file=sys.stderr)
    sys.exit(1)
json.dump({"batch": t["batch_size"], "fp16": t["fp16_run"]}, open(cfg["data"]["exp_dir"] + "/s2_seen.json", "w"))
log = open(cfg["data"]["exp_dir"] + "/train.log", "w")
for n in range(1, t["epochs"] + 1):
    log.write("2026-11-04 10:00:00,000\\tx\\tINFO\\tTrain Epoch: %d [50%%]\\n" % n)
    log.write("2026-11-04 10:00:00,000\\tx\\tINFO\\t====> Epoch: %d\\n" % n)
    log.flush()
    time.sleep(0.4)
os.makedirs(cfg["save_weight_dir"], exist_ok=True)
if os.environ.get("FAKE_NO_WEIGHTS") != "1":
    open("%s/%s_e%d_s77.pth" % (cfg["save_weight_dir"], cfg["name"], t["epochs"]), "wb").write(b"sovits-weights")
""",
    "GPT_SoVITS/s1_train.py": """
import os, sys, time
import yaml
cfg = yaml.safe_load(open(sys.argv[sys.argv.index("--config_file") + 1]))
t = cfg["train"]
assert cfg["pretrained_s1"].endswith("s1bert25hz-5kh-longer-epoch=12-step=369668.ckpt") and t["save_every_n_epoch"] == 1 and t["if_save_every_weights"]
assert os.environ["hz"] == "25hz" and os.environ["_CUDA_VISIBLE_DEVICES"] == "0"
if os.environ.get("FAKE_FAIL") == "gpt":
    sys.exit(1)
open(os.environ["opt_dir"] + "/s1_seen.json", "w").write('{"batch": %d, "precision": "%s"}' % (t["batch_size"], t["precision"]))
os.makedirs(t["half_weights_save_dir"], exist_ok=True)
for n in range(1, t["epochs"] + 1):
    open("%s/%s-e%d.ckpt" % (t["half_weights_save_dir"], t["exp_name"], n), "wb").write(b"gpt-%d" % n)
    time.sleep(0.4)
""",
}


class Fake:
    """A fake package directory and a job in a work directory."""

    def __init__(self, half=True):
        self.root = tempfile.mkdtemp()
        self.work = tempfile.mkdtemp()
        for rel, body in FAKE_SCRIPTS.items():
            path = os.path.join(self.root, *rel.split("/"))
            os.makedirs(os.path.dirname(path), exist_ok=True)
            with open(path, "w", encoding="utf-8") as f:
                f.write(body.replace("HALF", str(half)))
        os.makedirs(os.path.join(self.root, "GPT_SoVITS", "configs"), exist_ok=True)
        with open(os.path.join(self.root, "GPT_SoVITS", "configs", "s2.json"), "w") as f:
            json.dump({"train": {"fp16_run": True, "batch_size": 32}, "data": {}, "model": {}}, f)
        with open(os.path.join(self.root, "GPT_SoVITS", "configs", "s1longer-v2.yaml"), "w") as f:
            f.write("train:\n  precision: 16-mixed\n  batch_size: 8\n  epochs: 20\n")
        os.makedirs(os.path.join(self.work, "data"))
        self.list = os.path.join(self.work, "data", "data.list")
        with open(self.list, "w", encoding="utf8") as f:
            f.write("a.wav|mam|ja|おはよう\nb.wav|mam|ja|こんにちは\nc.wav|mam|ja|さようなら\n")
        self.job = {
            "work_dir": self.work, "list_path": self.list, "wav_dir": os.path.join(self.work, "data"),
            "exp_dir": os.path.join(self.work, "exp"), "out_dir": os.path.join(self.work, "out"),
            "sovits_weights_dir": os.path.join(self.work, "w_sovits"), "gpt_weights_dir": os.path.join(self.work, "w_gpt"),
            "exp_name": "mam-job1", "gpu": "0", "epochs_sovits": 3, "epochs_gpt": 4, "batch_sovits": 6, "batch_gpt": 5, "clips": 3,
        }

    def close(self):
        shutil.rmtree(self.root, ignore_errors=True)
        shutil.rmtree(self.work, ignore_errors=True)

    def job_file(self, **override):
        job = dict(self.job, **override)
        path = os.path.join(self.work, "job.json")
        with open(path, "w") as f:
            json.dump(job, f)
        return path

    def run(self, step, job_path=None, env=None):
        """Runs the driver in the fake package like mam does, and returns (exit code, progress events, stderr)."""
        env = dict(os.environ, PYTHONDONTWRITEBYTECODE="1", **(env or {}))
        if yaml is not None:  # the vendor scripts run with -s (no user site): PyYAML of this machine is found by PYTHONPATH
            env["PYTHONPATH"] = os.path.dirname(os.path.dirname(yaml.__file__))
        # main() like `python mam_gptsovits_train.py <step> <job.json>`, with a poll that is short enough for a test.
        boot = (
            "import importlib.util, sys; s = importlib.util.spec_from_file_location('t', %r); m = importlib.util.module_from_spec(s); "
            "s.loader.exec_module(m); m.POLL_SECONDS = 0.05; sys.exit(m.main(['mam_gptsovits_train.py'] + sys.argv[1:]))" % SCRIPT
        )
        proc = subprocess.run(
            [sys.executable, "-c", boot, step, job_path or self.job_file()], cwd=self.root, capture_output=True, text=True, env=env, timeout=120,
        )
        events = [json.loads(line[len("MAM_TRAIN "):]) for line in proc.stdout.splitlines() if line.startswith("MAM_TRAIN ")]
        for line in proc.stdout.splitlines():
            assert line.startswith("MAM_TRAIN "), "stdout carries only progress lines: %r" % line
        return proc.returncode, events, proc.stderr


needs_yaml = unittest.skipIf(yaml is None, "PyYAML is not installed here")


class DriverTest(unittest.TestCase):
    def setUp(self):
        self.fake = Fake()
        self.addCleanup(self.fake.close)

    def prepare(self, fake=None):
        fake = fake or self.fake
        for step in ("text", "hubert", "semantic"):
            code, _, err = fake.run(step)
            self.assertEqual(0, code, "%s: %s" % (step, err))

    def exp(self, name):
        return os.path.join(self.fake.work, "exp", name)

    @needs_yaml
    def test_the_whole_pipeline_in_order_writes_the_two_weights_the_server_loads(self):
        self.prepare()
        for step in ("sovits", "gpt"):
            code, events, err = self.fake.run(step)
            self.assertEqual(0, code, "%s: %s" % (step, err))
            self.assertEqual(step, events[-1]["step"])
        self.assertTrue(os.path.isfile(self.exp("2-name2text.txt")))
        self.assertFalse(os.path.exists(self.exp("2-name2text-0.txt")), "the part is merged away, as webui.py does")
        with open(self.exp("6-name2semantic.tsv"), encoding="utf8") as f:
            self.assertEqual("item_name\tsemantic_audio", f.readline().rstrip("\n"))
            self.assertEqual(3, len(f.read().strip().split("\n")))
        with open(os.path.join(self.fake.work, "out", "sovits.pth"), "rb") as f:
            self.assertEqual(b"sovits-weights", f.read())
        with open(os.path.join(self.fake.work, "out", "gpt.ckpt"), "rb") as f:
            self.assertEqual(b"gpt-4", f.read(), "the weights of the last epoch")

    def test_hubert_reports_the_files_that_are_done(self):
        self.assertEqual(0, self.fake.run("text")[0])
        code, events, _ = self.fake.run("hubert")
        self.assertEqual(0, code)
        self.assertEqual({"step": "hubert", "done": 3, "total": 3}, events[-1])

    def test_sovits_reports_the_epochs_from_the_train_log(self):
        self.prepare()
        code, events, _ = self.fake.run("sovits")
        self.assertEqual(0, code)
        done = [e["done"] for e in events]
        self.assertEqual(sorted(done), done, "progress only goes forward")
        self.assertEqual({"step": "sovits", "done": 3, "total": 3}, events[-1])
        self.assertTrue(any(0 < d < 3 for d in done), "an epoch is seen while the trainer runs: %s" % done)
        with open(self.exp("s2_seen.json")) as f:
            self.assertEqual({"batch": 6, "fp16": True}, json.load(f))

    @needs_yaml
    def test_gpt_reports_the_epochs_and_keeps_one_weights_file(self):
        self.prepare()
        code, events, _ = self.fake.run("gpt")
        self.assertEqual(0, code)
        self.assertEqual({"step": "gpt", "done": 4, "total": 4}, events[-1])
        self.assertTrue(any(0 < e["done"] < 4 for e in events), "an epoch is seen while the trainer runs")
        self.assertEqual([], os.listdir(self.fake.job["gpt_weights_dir"]), "the last weights moved to out/, the earlier ones were removed")

    @needs_yaml
    def test_a_gpu_without_half_precision_halves_the_batch_as_webui_does(self):
        fake = Fake(half=False)
        self.addCleanup(fake.close)
        self.prepare(fake)
        self.assertEqual(0, fake.run("sovits")[0])
        self.assertEqual(0, fake.run("gpt")[0])
        with open(os.path.join(fake.work, "exp", "s2_seen.json")) as f:
            self.assertEqual({"batch": 3, "fp16": False}, json.load(f))
        with open(os.path.join(fake.work, "exp", "s1_seen.json")) as f:
            self.assertEqual({"batch": 2, "precision": "32"}, json.load(f))

    @needs_yaml
    def test_a_failing_trainer_fails_the_step_and_leaves_no_weights(self):
        self.prepare()
        code, _, err = self.fake.run("sovits", env={"FAKE_FAIL": "sovits"})
        self.assertNotEqual(0, code)
        self.assertIn("CUDA out of memory", err, "the vendor's message reaches the log mam keeps the tail of")
        self.assertFalse(os.path.exists(os.path.join(self.fake.work, "out", "sovits.pth")))
        code, _, _ = self.fake.run("gpt", env={"FAKE_FAIL": "gpt"})
        self.assertNotEqual(0, code)
        self.assertFalse(os.path.exists(os.path.join(self.fake.work, "out", "gpt.ckpt")))

    def test_a_trainer_that_ends_without_weights_is_a_failure(self):
        self.prepare()
        code, _, err = self.fake.run("sovits", env={"FAKE_NO_WEIGHTS": "1"})
        self.assertEqual(3, code)
        self.assertIn("no weights", err)

    def test_an_empty_preparation_is_a_failure_not_an_empty_dataset(self):
        with open(os.path.join(self.fake.root, "GPT_SoVITS", "prepare_datasets", "1-get-text.py"), "w") as f:
            f.write("import os; open(os.environ['opt_dir'] + '/2-name2text-0.txt', 'w').write('\\n')\n")
        code, _, err = self.fake.run("text")
        self.assertEqual(3, code)
        self.assertIn("produced nothing", err)

    def test_only_the_fixed_steps_run(self):
        for step in ("webui", "../s2_train", "text; echo", "", "api_v2"):
            code, _, err = self.fake.run(step)
            self.assertEqual(2, code, step)
            self.assertIn("usage", err)


class JobTest(unittest.TestCase):
    def setUp(self):
        self.fake = Fake()
        self.addCleanup(self.fake.close)

    def refused(self, **override):
        code, events, err = self.fake.run("text", self.fake.job_file(**override))
        self.assertEqual(3, code, override)
        self.assertEqual([], events)
        return err

    def test_a_path_outside_the_work_directory_is_refused(self):
        for key in ("list_path", "wav_dir", "exp_dir", "out_dir", "sovits_weights_dir", "gpt_weights_dir"):
            self.assertIn(key, self.refused(**{key: "/etc/passwd"}))
            self.assertIn(key, self.refused(**{key: os.path.join(self.fake.work, "..", "x")}))
            self.assertIn(key, self.refused(**{key: "relative/path"}))

    def test_a_symlink_that_leaves_the_work_directory_is_refused(self):
        outside = tempfile.mkdtemp()
        self.addCleanup(lambda: shutil.rmtree(outside, ignore_errors=True))
        link = os.path.join(self.fake.work, "link")
        try:
            os.symlink(outside, link)
        except (OSError, NotImplementedError):
            self.skipTest("no symlinks here")
        self.assertIn("exp_dir", self.refused(exp_dir=os.path.join(link, "exp")))

    def test_names_and_numbers_are_checked(self):
        for name in ("a b", "../x", "x;y", "", "x" * 65):
            self.assertIn("exp_name", self.refused(exp_name=name))
        for gpu in ("0,1", "a", "", "100"):
            self.assertIn("gpu", self.refused(gpu=gpu))
        for key, bad in (("epochs_sovits", 0), ("epochs_sovits", 26), ("epochs_gpt", 51), ("batch_sovits", 0), ("batch_gpt", 65), ("batch_gpt", True), ("clips", 0), ("clips", "3")):
            self.assertIn(key, self.refused(**{key: bad}))

    def test_a_job_that_is_not_an_object_is_refused(self):
        path = os.path.join(self.fake.work, "job.json")
        with open(path, "w") as f:
            f.write("[1]")
        code, _, err = self.fake.run("text", path)
        self.assertEqual(3, code)
        self.assertIn("not an object", err)


if __name__ == "__main__":
    unittest.main()
