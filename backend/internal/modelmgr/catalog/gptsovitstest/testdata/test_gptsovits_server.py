"""Tests of mam's guarded launcher of GPT-SoVITS (data/overlays/mam_gptsovits_server.py): the Host check, the allow-list of requests and the
token that stand in front of the vendor's api_v2.py. api_v2, uvicorn and the vendor's FastAPI app are fakes, so neither torch nor GPT-SoVITS
is needed; run by the Go test TestGPTSoVITSLauncherGuardsTheVendorServer (python3 testdata/test_gptsovits_server.py <path of the script>)."""

import asyncio
import importlib.util
import os
import sys
import tempfile
import types
import unittest

SERVER = sys.argv.pop(1)
spec = importlib.util.spec_from_file_location("mam_gptsovits_server", SERVER)
gs = importlib.util.module_from_spec(spec)
spec.loader.exec_module(gs)

TOKEN = "0123456789abcdef0123456789abcdef"
PORT = 9880


class FakeApp:
    """The vendor's app: it records the requests it was given and answers 200."""

    def __init__(self):
        self.calls = []

    async def __call__(self, scope, receive, send):
        self.calls.append((scope["method"], scope["path"]))
        await send({"type": "http.response.start", "status": 200, "headers": []})
        await send({"type": "http.response.body", "body": b"vendor"})


def call(guard, method, path, host=f"127.0.0.1:{PORT}", token=None, extra=(), body=b""):
    """Sends one request through the guard and returns (status, body)."""
    headers = [] if host is None else [(b"host", host.encode("latin-1"))]
    if token is not None:
        headers.append((b"x-mam-token", token.encode("utf-8")))
    headers.extend(extra)
    sent = []

    async def receive():
        return {"type": "http.request", "body": body, "more_body": False}

    async def send(message):
        sent.append(message)

    asyncio.run(guard({"type": "http", "method": method, "path": path, "headers": headers}, receive, send))
    return sent[0]["status"], b"".join(m.get("body", b"") for m in sent[1:])


class GuardTest(unittest.TestCase):
    def setUp(self):
        self.app = FakeApp()
        self.guard = gs.Guard(self.app, PORT, TOKEN)

    def test_tts_with_the_token_reaches_the_vendor_app(self):
        for host in (f"127.0.0.1:{PORT}", f"localhost:{PORT}", f"LOCALHOST:{PORT}"):
            self.assertEqual((200, b"vendor"), call(self.guard, "POST", "/tts", host=host, token=TOKEN))
        self.assertEqual([("POST", "/tts")] * 3, self.app.calls)

    def test_tts_without_the_right_token_is_forbidden(self):
        for token in (None, "", "wrong", TOKEN + "x", TOKEN[:-1]):
            status, _ = call(self.guard, "POST", "/tts", token=token)
            self.assertEqual(403, status, f"token {token!r}")
        status, _ = call(self.guard, "POST", "/tts", token=TOKEN, extra=[(b"x-mam-token", TOKEN.encode())])
        self.assertEqual(403, status, "two token headers are not one token")
        self.assertEqual([], self.app.calls)

    def test_another_host_is_forbidden_even_with_the_token(self):
        # DNS rebinding: the page's own name points at 127.0.0.1 and is sent as the Host; so is the right name with another port.
        for host in ("evil.example.com", f"evil.example.com:{PORT}", "127.0.0.1", f"127.0.0.1:{PORT + 1}", f"[::1]:{PORT}", None):
            for method, path in (("POST", "/tts"), ("GET", "/openapi.json")):
                status, _ = call(self.guard, method, path, host=host, token=TOKEN)
                self.assertEqual(403, status, f"{method} {path} Host {host!r}")
        status, _ = call(self.guard, "GET", "/openapi.json", extra=[(b"host", b"evil.example.com")])
        self.assertEqual(403, status, "two Host headers")
        self.assertEqual([], self.app.calls)

    def test_the_readiness_probe_needs_no_token(self):
        self.assertEqual(200, call(self.guard, "GET", "/openapi.json")[0])
        self.assertEqual([("GET", "/openapi.json")], self.app.calls)

    def test_every_other_request_is_not_found_even_for_the_holder_of_the_token(self):
        # /control stops or restarts the server, /set_*_weights load a file of the PC, GET /tts and /set_refer_audio read a path.
        for method, path in (
            ("GET", "/control"),
            ("POST", "/control"),
            ("GET", "/set_gpt_weights"),
            ("GET", "/set_sovits_weights"),
            ("GET", "/set_refer_audio"),
            ("GET", "/tts"),
            ("POST", "/openapi.json"),
            ("GET", "/docs"),
            ("GET", "/redoc"),
            ("GET", "//tts"),
            ("POST", "/tts/"),
            ("DELETE", "/tts"),
        ):
            status, _ = call(self.guard, method, path, token=TOKEN)
            self.assertEqual(404, status, f"{method} {path}")
        self.assertEqual([], self.app.calls)

    def test_the_lifespan_reaches_the_vendor_app(self):
        seen = []

        async def app(scope, receive, send):
            seen.append(scope["type"])

        asyncio.run(gs.Guard(app, PORT, TOKEN)({"type": "lifespan"}, None, None))
        self.assertEqual(["lifespan"], seen)

    def test_a_websocket_is_closed(self):
        sent = []

        async def send(message):
            sent.append(message)

        asyncio.run(self.guard({"type": "websocket", "headers": []}, None, send))
        self.assertEqual([{"type": "websocket.close", "code": 1008}], sent)
        self.assertEqual([], self.app.calls)


class VoiceTest(unittest.TestCase):
    """POST /mam/voice switches the weights of the pipeline to a trained voice below the one directory mam named, and only to that."""

    def setUp(self):
        self.root = tempfile.mkdtemp()
        self.addCleanup(lambda: __import__("shutil").rmtree(self.root, ignore_errors=True))
        self.good = "0190c7f0-0000-7000-8000-000000000001"
        os.makedirs(os.path.join(self.root, self.good))
        for name in gs.VOICE_FILES:
            with open(os.path.join(self.root, self.good, name), "wb") as f:
                f.write(b"weights")
        self.app, self.loaded = FakeApp(), []
        self.guard = gs.Guard(self.app, PORT, TOKEN, self.loaded.append, self.root)

    def voice(self, voice, token=TOKEN):
        return call(self.guard, "POST", "/mam/voice", token=token, body=('{"voice": %s}' % voice).encode())

    def test_a_trained_voice_is_loaded_by_its_id_and_the_base_by_an_empty_one(self):
        self.assertEqual(200, self.voice('"%s"' % self.good)[0])
        self.assertEqual(200, self.voice('""')[0])
        gpt, sovits = self.loaded[0]
        self.assertEqual((os.path.join(os.path.realpath(self.root), self.good, "gpt.ckpt"), os.path.join(os.path.realpath(self.root), self.good, "sovits.pth")), (gpt, sovits))
        self.assertIsNone(self.loaded[1])
        self.assertEqual([], self.app.calls, "the vendor's app never sees this request")

    def test_it_needs_the_token(self):
        for token in (None, "wrong"):
            self.assertEqual(403, self.voice('"%s"' % self.good, token=token)[0])
        self.assertEqual([], self.loaded)

    def test_an_id_that_is_not_a_name_below_the_root_is_not_found(self):
        os.makedirs(os.path.join(self.root, "..", "outside"), exist_ok=True)
        for voice in ("../outside", "/etc", "a/b", "..", ".", "x" * 65, "a b", "unknown-voice", self.good + "/../" + self.good):
            self.assertIn(self.voice(json_string(voice))[0], (404, 400), voice)
        self.assertEqual([], self.loaded)

    def test_a_voice_whose_file_is_missing_is_not_found(self):
        os.remove(os.path.join(self.root, self.good, "sovits.pth"))
        self.assertEqual(404, self.voice('"%s"' % self.good)[0])
        self.assertEqual([], self.loaded)

    def test_a_symlink_out_of_the_root_is_not_followed(self):
        outside = tempfile.mkdtemp()
        self.addCleanup(lambda: __import__("shutil").rmtree(outside, ignore_errors=True))
        for name in gs.VOICE_FILES:
            with open(os.path.join(outside, name), "wb") as f:
                f.write(b"x")
        link = "0190c7f0-0000-7000-8000-000000000002"
        try:
            os.symlink(outside, os.path.join(self.root, link))
        except (OSError, NotImplementedError):
            self.skipTest("no symlinks here")
        self.assertEqual(404, self.voice('"%s"' % link)[0])

    def test_a_body_that_is_not_a_voice_is_refused(self):
        for body in (b"", b"[]", b"{}", b'{"voice": 1}', b'{"voice": null}', b"x" * 5000):
            status, _ = call(self.guard, "POST", "/mam/voice", token=TOKEN, body=body)
            self.assertIn(status, (400, 413), body[:10])
        self.assertEqual([], self.loaded)

    def test_a_weight_file_that_does_not_load_is_a_server_error_not_a_silent_fallback(self):
        def broken(weights):
            raise RuntimeError("invalid load key")

        guard = gs.Guard(self.app, PORT, TOKEN, broken, self.root)
        status, body = call(guard, "POST", "/mam/voice", token=TOKEN, body=('{"voice": "%s"}' % self.good).encode())
        self.assertEqual(500, status)
        self.assertIn("invalid load key", body.decode())

    def test_without_a_switch_the_path_is_not_found(self):
        guard = gs.Guard(self.app, PORT, TOKEN)
        self.assertEqual(404, call(guard, "POST", "/mam/voice", token=TOKEN, body=b'{"voice": ""}')[0])

    def test_other_methods_of_the_path_are_not_found(self):
        self.assertEqual(404, call(self.guard, "GET", "/mam/voice", token=TOKEN)[0])


def json_string(value):
    import json

    return json.dumps(value)


class SwitcherTest(unittest.TestCase):
    def test_it_loads_the_given_weights_or_the_ones_the_server_started_with(self):
        calls = []
        pipeline = types.SimpleNamespace(
            configs=types.SimpleNamespace(t2s_weights_path="base-gpt", vits_weights_path="base-sovits"),
            init_vits_weights=lambda p: calls.append(("vits", p)),
            init_t2s_weights=lambda p: calls.append(("t2s", p)),
        )
        switch = gs.switcher(types.SimpleNamespace(tts_pipeline=pipeline))
        pipeline.configs.t2s_weights_path = "changed-by-init"  # init_t2s_weights writes the loaded path back into the config
        switch(("g.ckpt", "s.pth"))
        switch(None)
        self.assertEqual([("vits", "s.pth"), ("t2s", "g.ckpt"), ("vits", "base-sovits"), ("t2s", "base-gpt")], calls)


class MainTest(unittest.TestCase):
    """main() starts the vendor's server behind the guard, or refuses to start."""

    def setUp(self):
        self.runs = []
        self.vendor = types.SimpleNamespace(
            APP=FakeApp(), host="127.0.0.1", port=PORT,
            tts_pipeline=types.SimpleNamespace(configs=types.SimpleNamespace(t2s_weights_path="g", vits_weights_path="s")),
        )
        self.workdir = tempfile.mkdtemp()
        self.addCleanup(lambda: __import__("shutil").rmtree(self.workdir, ignore_errors=True))
        os.makedirs(os.path.join(self.workdir, "GPT_SoVITS", "configs"))
        with open(os.path.join(self.workdir, "GPT_SoVITS", "configs", "tts_infer.yaml"), "w") as f:
            f.write("custom: {}\n")
        uvicorn = types.SimpleNamespace(run=lambda **kw: self.runs.append(kw))
        saved = (sys.argv, dict(os.environ), list(sys.path), os.getcwd())
        self.addCleanup(self.restore, saved)
        os.chdir(self.workdir)
        sys.modules["api_v2"], sys.modules["uvicorn"] = self.vendor, uvicorn
        self.addCleanup(lambda: [sys.modules.pop(n, None) for n in ("api_v2", "uvicorn")])
        os.environ[gs.TOKEN_ENV] = TOKEN
        sys.argv = ["mam_gptsovits_server.py", "-a", "127.0.0.1", "-p", str(PORT), "-c", "GPT_SoVITS/configs/tts_infer.yaml"]

    @staticmethod
    def restore(saved):
        sys.argv = saved[0]
        os.environ.clear()
        os.environ.update(saved[1])
        sys.path[:] = saved[2]
        os.chdir(saved[3])

    def test_it_runs_the_vendor_app_behind_the_guard_on_the_given_address(self):
        self.assertEqual(0, gs.main())
        (run,) = self.runs
        self.assertEqual(("127.0.0.1", PORT, 1), (run["host"], run["port"], run["workers"]))
        self.assertIsInstance(run["app"], gs.Guard)
        self.assertIs(self.vendor.APP, run["app"].app, "the guard stands in front of the vendor's app, not another one")
        self.assertEqual(403, call(run["app"], "POST", "/tts")[0])

    def test_api_v2_reads_a_copy_of_its_config_so_a_trained_voice_never_becomes_the_base(self):
        original = os.path.join(self.workdir, "GPT_SoVITS", "configs", "tts_infer.yaml")
        self.assertEqual(0, gs.main())
        used = sys.argv[sys.argv.index("-c") + 1]
        self.assertEqual(os.path.join(self.workdir, "TEMP", "mam_tts_infer.yaml"), used)
        self.assertNotEqual(original, used)
        with open(original) as a, open(used) as b:
            self.assertEqual(a.read(), b.read())

    def test_it_gives_the_guard_the_voice_directory_mam_named(self):
        os.environ[gs.VOICE_ROOT_ENV] = self.workdir
        self.assertEqual(0, gs.main())
        guard = self.runs[0]["app"]
        self.assertEqual(self.workdir, guard.root)
        self.assertIsNotNone(guard.switch)

    def test_it_refuses_an_address_that_is_not_the_loopback(self):
        for addr in ("0.0.0.0", "::", "None", "localhost"):
            sys.argv[2] = addr
            self.assertEqual(2, gs.main(), addr)
        self.assertEqual([], self.runs)

    def test_it_refuses_a_missing_or_short_token(self):
        del os.environ[gs.TOKEN_ENV]
        self.assertEqual(2, gs.main())
        os.environ[gs.TOKEN_ENV] = "short"
        self.assertEqual(2, gs.main())
        self.assertEqual([], self.runs)

    def test_it_refuses_a_vendor_server_that_did_not_read_the_address(self):
        self.vendor.port = PORT + 1
        self.assertEqual(2, gs.main())
        self.assertEqual([], self.runs)


if __name__ == "__main__":
    unittest.main()
