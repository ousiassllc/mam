"""Tests of mam's guarded launcher of GPT-SoVITS (data/overlays/mam_gptsovits_server.py): the Host check, the allow-list of requests and the
token that stand in front of the vendor's api_v2.py. api_v2, uvicorn and the vendor's FastAPI app are fakes, so neither torch nor GPT-SoVITS
is needed; run by the Go test TestGPTSoVITSLauncherGuardsTheVendorServer (python3 testdata/test_gptsovits_server.py <path of the script>)."""

import asyncio
import importlib.util
import os
import sys
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


def call(guard, method, path, host=f"127.0.0.1:{PORT}", token=None, extra=()):
    """Sends one request through the guard and returns (status, body)."""
    headers = [] if host is None else [(b"host", host.encode("latin-1"))]
    if token is not None:
        headers.append((b"x-mam-token", token.encode("utf-8")))
    headers.extend(extra)
    sent = []

    async def receive():
        return {"type": "http.request", "body": b"", "more_body": False}

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


class MainTest(unittest.TestCase):
    """main() starts the vendor's server behind the guard, or refuses to start."""

    def setUp(self):
        self.runs = []
        self.vendor = types.SimpleNamespace(APP=FakeApp(), host="127.0.0.1", port=PORT)
        uvicorn = types.SimpleNamespace(run=lambda **kw: self.runs.append(kw))
        saved = (sys.argv, dict(os.environ), list(sys.path))
        self.addCleanup(self.restore, saved)
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

    def test_it_runs_the_vendor_app_behind_the_guard_on_the_given_address(self):
        self.assertEqual(0, gs.main())
        (run,) = self.runs
        self.assertEqual(("127.0.0.1", PORT, 1), (run["host"], run["port"], run["workers"]))
        self.assertIsInstance(run["app"], gs.Guard)
        self.assertIs(self.vendor.APP, run["app"].app, "the guard stands in front of the vendor's app, not another one")
        self.assertEqual(403, call(run["app"], "POST", "/tts")[0])

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
