"""Tests of mam's guarded launcher of Irodori-TTS (data/overlays/mam_irodori_server.py): the Host check, the allow-list of requests and the token
that stand in front of the vendor's server, the settings it gives the vendor's server (the weights of the chosen model by path, nothing to
download) and the redirection of SilentCipher to the weights mam fetched. The vendor's FastAPI app, torch and silentcipher are fakes; run by the
Go test TestIrodoriLauncherGuardsTheVendorServer (python3 testdata/test_irodori_server.py <path of the script>)."""

import asyncio
import importlib.util
import os
import sys
import tempfile
import types
import unittest

SERVER = sys.argv.pop(1)
spec = importlib.util.spec_from_file_location("mam_irodori_server", SERVER)
irs = importlib.util.module_from_spec(spec)
spec.loader.exec_module(irs)

TOKEN = "0123456789abcdef0123456789abcdef"
PORT = 8088


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
        self.guard = irs.Guard(self.app, PORT, TOKEN)

    def test_speech_with_the_token_reaches_the_vendor_app(self):
        for host in (f"127.0.0.1:{PORT}", f"localhost:{PORT}", f"LOCALHOST:{PORT}"):
            self.assertEqual((200, b"vendor"), call(self.guard, "POST", "/v1/audio/speech", host=host, token=TOKEN))
        self.assertEqual([("POST", "/v1/audio/speech")] * 3, self.app.calls)

    def test_the_readiness_probe_needs_no_token(self):
        self.assertEqual((200, b"vendor"), call(self.guard, "GET", "/health"))

    def test_speech_without_the_token_or_with_another_one_is_refused(self):
        self.assertEqual(403, call(self.guard, "POST", "/v1/audio/speech")[0])
        self.assertEqual(403, call(self.guard, "POST", "/v1/audio/speech", token="x" * 32)[0])
        self.assertEqual(403, call(self.guard, "POST", "/v1/audio/speech", token=TOKEN + "0")[0])
        two = (b"x-mam-token", TOKEN.encode())
        self.assertEqual(403, call(self.guard, "POST", "/v1/audio/speech", token=TOKEN, extra=[two])[0], "two tokens are not one")
        self.assertEqual([], self.app.calls)

    def test_a_foreign_host_is_refused_whatever_the_token(self):
        for host in ("evil.example", f"evil.example:{PORT}", "127.0.0.1", "127.0.0.1:9999", None):
            self.assertEqual(403, call(self.guard, "POST", "/v1/audio/speech", host=host, token=TOKEN)[0], host)
            self.assertEqual(403, call(self.guard, "GET", "/health", host=host)[0], host)
        self.assertEqual([], self.app.calls)

    def test_every_other_request_is_404_even_with_the_token(self):
        for method, path in [
            ("GET", "/v1/audio/speech"),
            ("POST", "/health"),
            ("GET", "/v1/models"),
            ("GET", "/v1/audio/voices"),
            ("POST", "/v1/audio/voices"),
            ("PUT", "/v1/audio/voices/x"),
            ("DELETE", "/v1/audio/voices/x"),
            ("GET", "/docs"),
            ("GET", "/openapi.json"),
            ("POST", "/v1/audio/speech/"),
        ]:
            self.assertEqual(404, call(self.guard, method, path, token=TOKEN)[0], f"{method} {path}")
        self.assertEqual([], self.app.calls)

    def test_lifespan_reaches_the_vendor_app_and_a_websocket_is_closed(self):
        sent = []

        async def receive():
            return {"type": "websocket.connect"}

        async def send(message):
            sent.append(message)

        asyncio.run(self.guard({"type": "websocket", "path": "/"}, receive, send))
        self.assertEqual([{"type": "websocket.close", "code": 1008}], sent)


class ServerEnvTest(unittest.TestCase):
    def setUp(self):
        self.root = tempfile.mkdtemp()
        os.makedirs(os.path.join(self.root, "models", "v4.1-small"))
        open(os.path.join(self.root, "models", "v4.1-small", "model.safetensors"), "wb").close()

    def test_the_weights_are_the_chosen_models_and_nothing_is_downloaded(self):
        env = irs.server_env(self.root, "v4.1-small", PORT)
        self.assertEqual("127.0.0.1", env["IRODORI_HOST"])
        self.assertEqual(str(PORT), env["IRODORI_PORT"])
        self.assertEqual(os.path.join(self.root, "models", "v4.1-small", "model.safetensors"), env["IRODORI_CHECKPOINT"])
        self.assertEqual(os.path.join(self.root, "models", "codec", "weights.pth"), env["IRODORI_CODEC_REPO"])
        self.assertTrue(os.path.isabs(env["IRODORI_CODEC_REPO"]), "a path, so that no Hugging Face repository is looked up")
        self.assertEqual("true", env["IRODORI_PRELOAD"])
        self.assertNotIn("IRODORI_API_KEY", env)
        self.assertNotIn("IRODORI_HF_CHECKPOINT", env)

    def test_a_model_that_is_not_installed_or_not_a_slug_is_refused_before_anything_is_loaded(self):
        with self.assertRaises(FileNotFoundError):
            irs.server_env(self.root, "v4-large", PORT)
        for bad in ("", "../x", "V4", "a b", "x" * 40):
            with self.assertRaises(ValueError, msg=bad):
                irs.server_env(self.root, bad, PORT)


class SilentCipherTest(unittest.TestCase):
    def test_get_model_reads_the_weights_mam_fetched(self):
        calls = []
        fake = types.ModuleType("silentcipher")
        fake.get_model = lambda **kw: calls.append(kw) or "model"
        sys.modules.pop("silentcipher.server", None)
        irs.patch_silentcipher(fake, "/env")

        # The vendor's call: only the model type and the device, as irodori_tts/watermark.py makes it.
        self.assertEqual("model", fake.get_model(model_type="44.1k", device="cuda"))
        ckpt = os.path.join("/env", "models", "silentcipher", "44_1_khz", "73999_iteration")
        self.assertEqual([{"model_type": "44.1k", "ckpt_path": ckpt, "config_path": os.path.join(ckpt, "hparams.yaml"), "device": "cuda"}], calls)


if __name__ == "__main__":
    unittest.main()
