"""Tests of the HTTP layer of mam's MuseTalk server (data/overlays/musetalk_server.py): the contract the Go client
(modelmgr/lipsync) relies on. The engine is a fake, so neither torch nor MuseTalk is needed; run by the Go test
TestMuseTalkServerHonoursTheClientContract (python3 testdata/test_musetalk_server.py <path of the server script>)."""

import http.client
import importlib.util
import json
import os
import shutil
import sys
import tempfile
import threading
import unittest

SERVER = sys.argv.pop(1)
spec = importlib.util.spec_from_file_location("musetalk_server", SERVER)
ms = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ms)
import mam_musetalk_common as common  # the server put its directory on the path


TOKEN = "0123456789abcdef0123456789abcdef"


class FakeEngine:
    def __init__(self):
        self.calls = []
        self.entered = threading.Event()
        self.release = threading.Event()
        self.block = False
        self.raises = None

    def info(self):
        return {"model": "musetalk-1.5", "device": "fake", "float16": True}

    def lipsync(self, req):
        self.calls.append(req)
        if self.block:
            self.entered.set()
            assert self.release.wait(30), "the test never released the generation"
        if self.raises:
            raise self.raises
        with open(req["output_path"], "wb") as f:
            f.write(b"mp4")
        return {"output_path": req["output_path"], "frames": 25, "fps": req["fps"], "duration_sec": 1.0}


class ServerTest(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp(prefix="mt-test-")
        self.addCleanup(shutil.rmtree, self.dir, ignore_errors=True)
        self.engine = FakeEngine()
        self.server = ms.Server("127.0.0.1", 0, self.engine, TOKEN)
        self.thread = threading.Thread(target=lambda: self.server.serve_forever(poll_interval=0.01), daemon=True)
        self.thread.start()
        self.addCleanup(self.stop)
        self.video = self.touch("in.mp4")
        self.audio = self.touch("in.wav")
        self.ffmpeg = self.touch("ffmpeg")
        self.out = os.path.join(self.dir, "out.mp4")

    def stop(self):
        self.engine.release.set()
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(5)

    def touch(self, name):
        path = os.path.join(self.dir, name)
        with open(path, "wb") as f:
            f.write(b"x")
        return path

    def request(self, method, path, body=None, headers=None, host=None):
        conn = http.client.HTTPConnection("127.0.0.1", self.server.port, timeout=30)
        try:
            hdrs = {"Host": host or f"127.0.0.1:{self.server.port}", "X-Mam-Token": TOKEN}
            if body is not None:
                hdrs["Content-Type"] = "application/json"
            hdrs.update(headers or {})
            data = body if isinstance(body, (bytes, type(None))) else json.dumps(body).encode()
            conn.putrequest(method, path, skip_host=True, skip_accept_encoding=True)
            for k, v in hdrs.items():
                if v is not None:
                    conn.putheader(k, v)
            if data is not None:
                conn.putheader("Content-Length", str(len(data)))
            conn.endheaders(data)
            res = conn.getresponse()
            raw = res.read()
            return res.status, json.loads(raw)
        finally:
            conn.close()

    def good(self, **over):
        body = {"video_path": self.video, "audio_path": self.audio, "output_path": self.out, "ffmpeg_path": self.ffmpeg}
        body.update(over)
        return body

    def error(self, status, code, got):
        self.assertEqual(status, got[0], got)
        self.assertEqual(code, got[1]["error"]["code"], got)
        self.assertTrue(got[1]["error"]["message"])

    def test_health_answers_without_a_token_and_names_the_model(self):
        status, body = self.request("GET", "/health", headers={"X-Mam-Token": None})
        self.assertEqual(200, status)
        self.assertEqual({"status": "ok", "model": "musetalk-1.5", "device": "fake", "float16": True}, body)

    def test_lipsync_returns_the_report_and_writes_the_output(self):
        status, body = self.request("POST", "/v1/lipsync", self.good())
        self.assertEqual(200, status, body)
        self.assertEqual(self.out, body["output_path"])
        self.assertEqual(25, body["frames"])
        self.assertEqual(25, body["fps"], "the default is the rate MuseTalk was trained at")
        self.assertIn("elapsed_sec", body)
        self.assertTrue(os.path.isfile(self.out))

    def test_fps_is_passed_on(self):
        self.assertEqual(200, self.request("POST", "/v1/lipsync", self.good(fps=30))[0])
        self.assertEqual(30, self.engine.calls[-1]["fps"])

    def test_a_request_without_the_token_or_with_another_is_forbidden(self):
        self.error(403, "forbidden", self.request("POST", "/v1/lipsync", self.good(), headers={"X-Mam-Token": None}))
        self.error(403, "forbidden", self.request("POST", "/v1/lipsync", self.good(), headers={"X-Mam-Token": "x" * 32}))
        self.assertEqual([], self.engine.calls)

    def test_another_host_header_is_refused_for_every_path(self):
        self.error(403, "forbidden", self.request("GET", "/health", host="evil.example:80"))
        self.error(403, "forbidden", self.request("POST", "/v1/lipsync", self.good(), host="evil.example"))

    def test_bad_requests(self):
        cases = {
            "not an object": [1, 2],
            "no video": {k: v for k, v in self.good().items() if k != "video_path"},
            "relative video": self.good(video_path="in.mp4"),
            "empty output": self.good(output_path=""),
            "a number as a path": self.good(audio_path=3),
            "fps zero": self.good(fps=0),
            "fps too high": self.good(fps=61),
            "fps a string": self.good(fps="25"),
            "fps a bool": self.good(fps=True),
            "output is not mp4": self.good(output_path=os.path.join(self.dir, "out.mov")),
            "output folder missing": self.good(output_path=os.path.join(self.dir, "nope", "out.mp4")),
            "video of another kind": self.good(video_path=self.touch("in.txt")),
            "output is the video": self.good(output_path=self.video.replace(".mp4", ".mp4")),
        }
        for name, body in cases.items():
            with self.subTest(name):
                got = self.request("POST", "/v1/lipsync", body)
                self.assertEqual(400, got[0], got)
                self.assertEqual("bad_request", got[1]["error"]["code"])
        self.assertEqual([], self.engine.calls)

    def test_a_missing_input_is_not_found(self):
        for key in ("video_path", "audio_path", "ffmpeg_path"):
            with self.subTest(key):
                bad = os.path.join(self.dir, "missing.mp4" if key == "video_path" else "missing")
                self.error(404, "input_not_found", self.request("POST", "/v1/lipsync", self.good(**{key: bad})))

    def test_body_that_is_not_json_or_too_large(self):
        self.error(400, "bad_request", self.request("POST", "/v1/lipsync", b"not json"))
        self.error(400, "bad_request", self.request("POST", "/v1/lipsync", b"x" * (ms.MAX_BODY_BYTES + 1)))
        self.error(400, "bad_request", self.request("POST", "/v1/lipsync", json.dumps(self.good()).encode(), headers={"Content-Type": "text/plain"}))

    def test_unknown_paths(self):
        self.error(400, "bad_request", self.request("GET", "/nope"))
        self.error(400, "bad_request", self.request("POST", "/v1/other", self.good()))

    def test_the_engines_refusals_keep_their_code_and_status(self):
        for code, status in common.ERRORS.items():
            if code in ("busy", "forbidden", "input_not_found", "bad_request"):
                continue
            with self.subTest(code):
                self.engine.raises = common.LipSyncError(code, "message of " + code)
                got = self.request("POST", "/v1/lipsync", self.good())
                self.assertEqual(status, got[0])
                self.assertEqual({"error": {"code": code, "message": "message of " + code}}, got[1])

    def test_an_unexpected_failure_is_an_internal_error_and_the_server_stays_up(self):
        self.engine.raises = RuntimeError("boom")
        status, body = self.request("POST", "/v1/lipsync", self.good())
        self.assertEqual(500, status)
        self.assertEqual("internal", body["error"]["code"])
        self.assertIn("boom", body["error"]["message"])
        self.engine.raises = None
        self.assertEqual(200, self.request("POST", "/v1/lipsync", self.good())[0])

    def test_one_generation_at_a_time(self):
        self.engine.block = True
        first = {}
        t = threading.Thread(target=lambda: first.update(r=self.request("POST", "/v1/lipsync", self.good())))
        t.start()
        self.assertTrue(self.engine.entered.wait(30))
        self.error(409, "busy", self.request("POST", "/v1/lipsync", self.good(output_path=os.path.join(self.dir, "second.mp4"))))
        self.assertEqual(200, self.request("GET", "/health")[0], "the health check answers while a generation runs")
        self.engine.release.set()
        t.join(30)
        self.assertEqual(200, first["r"][0])
        self.engine.block = False
        self.assertEqual(200, self.request("POST", "/v1/lipsync", self.good())[0], "the lock is free again")


class FFmpegTest(unittest.TestCase):
    def test_a_missing_ffmpeg_is_ffmpeg_failed(self):
        with self.assertRaises(common.LipSyncError) as cm:
            common.run_ffmpeg("/nonexistent/ffmpeg", "-i", "x")
        self.assertEqual("ffmpeg_failed", cm.exception.code)

    def test_a_failing_ffmpeg_reports_the_end_of_its_message(self):
        script = os.path.join(tempfile.mkdtemp(prefix="mt-ff-"), "ffmpeg.py")
        with open(script, "w") as f:
            f.write("import sys\nprint('first', file=sys.stderr)\nprint('Invalid data found', file=sys.stderr)\nsys.exit(1)\n")
        # run_ffmpeg starts the program itself: use the interpreter's launcher script as the "ffmpeg"
        launcher = script.replace(".py", ".sh")
        with open(launcher, "w") as f:
            f.write(f"#!/bin/sh\nexec '{sys.executable}' '{script}' \"$@\"\n")
        os.chmod(launcher, 0o700)
        if sys.platform == "win32":
            self.skipTest("a shell script is not a program on Windows")
        with self.assertRaises(common.LipSyncError) as cm:
            common.run_ffmpeg(launcher, "-i", "x")
        self.assertEqual("ffmpeg_failed", cm.exception.code)
        self.assertIn("Invalid data found", cm.exception.message)


class MainTest(unittest.TestCase):
    def test_it_refuses_to_listen_anywhere_but_the_loopback(self):
        os.environ[ms.TOKEN_ENV] = TOKEN
        self.assertEqual(2, ms.main(["--host", "0.0.0.0", "--port", "1"]))

    def test_it_refuses_to_start_without_a_token(self):
        os.environ.pop(ms.TOKEN_ENV, None)
        self.assertEqual(2, ms.main(["--port", "1"]))
        os.environ[ms.TOKEN_ENV] = "short"
        self.assertEqual(2, ms.main(["--port", "1"]))


if __name__ == "__main__":
    unittest.main()
