"""mam's launcher of the Irodori-TTS server (spec/architecture/model-manager/local-runtime.md section 14).

The vendor's server (Irodori-TTS-Server, an OpenAI-compatible /v1/audio/speech) has no Host check, reads any audio path of the PC on request
(irodori.ref_wav), loads any LoRA adapter directory on request (irodori.lora_adapter), lets whoever reaches it upload and delete voice files
(/v1/audio/voices) and, without IRODORI_API_KEY, has no authentication at all. A web page of the user's browser can send requests to 127.0.0.1,
and a name that points at 127.0.0.1 (DNS rebinding) passes as well. So mam does not start the vendor's `python -m irodori_openai_tts`: it starts
this script with the Python of the runtime (working directory: the environment mam assembled), which imports the vendor's FastAPI app, puts a
guard in front of it and runs uvicorn. The guard answers only:

    POST /v1/audio/speech   the header X-Mam-Token with the token of this start is required (MAM_RUNTIME_TOKEN)
    GET  /health            mam's readiness probe sends no token (the server answers it once the model is loaded; it loads no model itself)

and only for the Host 127.0.0.1:<port> or localhost:<port>. Every other path (the voice upload and delete, /v1/models, the docs pages, ...) is 404
even for the holder of the token. mam sends the reference voice as a path (irodori.ref_wav) it checked before, and never sends lora_adapter.

The model is the one mam's settings chose (MAM_RUNTIME_CHOICE, a folder of models/ that the install filled and verified), the codec and the
watermark weights are the ones mam fetched, so nothing is downloaded while the server runs (the offline switches of the catalog's env). The
guard needs the standard library only (it is tested without torch or fastapi: the vendor's server is imported by main()).
"""

from __future__ import annotations

import argparse
import hmac
import os
import re
import signal
import sys
import traceback

TOKEN_ENV = "MAM_RUNTIME_TOKEN"
CHOICE_ENV = "MAM_RUNTIME_CHOICE"
TOKEN_HEADER = b"x-mam-token"  # the name of the header as the ASGI server gives it: lower case
BIND_ADDR = "127.0.0.1"
MIN_TOKEN_LEN = 16
CHOICE_PATTERN = re.compile(r"^[a-z0-9][a-z0-9.-]{0,30}$")

# Where the install puts the weights (the catalog's assets, relative to the environment).
CODEC_WEIGHTS = "models/codec/weights.pth"
SILENTCIPHER_DIR = "models/silentcipher/44_1_khz/73999_iteration"

# The requests that are answered: (method, path) -> whether the token is required. The readiness probe sends none.
ROUTES = {
    ("POST", "/v1/audio/speech"): True,
    ("GET", "/health"): False,
}


async def _refuse(send, status: int, message: str) -> None:
    body = message.encode("utf-8")
    headers = [
        (b"content-type", b"text/plain; charset=utf-8"),
        (b"content-length", str(len(body)).encode("ascii")),
        (b"connection", b"close"),  # the request body may not have been read
    ]
    await send({"type": "http.response.start", "status": status, "headers": headers})
    await send({"type": "http.response.body", "body": body})


class Guard:
    """An ASGI application in front of the vendor's app: the Host check, the allow-list of requests and the token."""

    def __init__(self, app, port: int, token: str):
        self.app = app
        self.token = token.encode("utf-8")
        self.hosts = (f"{BIND_ADDR}:{port}", f"localhost:{port}")

    async def __call__(self, scope, receive, send) -> None:
        kind = scope["type"]
        if kind == "lifespan":
            await self.app(scope, receive, send)
            return
        if kind != "http":  # the vendor's server has no WebSocket
            if kind == "websocket":
                await send({"type": "websocket.close", "code": 1008})
            return
        headers = scope.get("headers", [])
        # DNS rebinding: a web page that made its own name point at 127.0.0.1 sends that name as the Host.
        hosts = [v for k, v in headers if k == b"host"]
        if len(hosts) != 1 or hosts[0].decode("latin-1").lower() not in self.hosts:
            await _refuse(send, 403, "このアドレスでは受け付けません")
            return
        needs_token = ROUTES.get((scope["method"], scope["path"]))
        if needs_token is None:
            await _refuse(send, 404, "このパスはありません")
            return
        if needs_token:
            given = [v for k, v in headers if k == TOKEN_HEADER]
            if len(given) != 1 or not hmac.compare_digest(given[0], self.token):
                await _refuse(send, 403, "トークンが一致しません")
                return
        await self.app(scope, receive, send)


def server_env(root: str, choice: str, port: int) -> dict:
    """The IRODORI_* settings of the vendor's server (pydantic-settings reads them when its app is imported): the weights of the chosen
    model and of the codec by path, no voice directory that anything could fill, no API key (the guard is the authentication)."""
    if not CHOICE_PATTERN.match(choice):
        raise ValueError(f"{CHOICE_ENV} is not a model of mam's catalog")
    model_dir = os.path.join(root, "models", choice)
    env = {
        "IRODORI_HOST": BIND_ADDR,
        "IRODORI_PORT": str(port),
        "IRODORI_CHECKPOINT": os.path.join(model_dir, "model.safetensors"),
        "IRODORI_CODEC_REPO": os.path.join(root, *CODEC_WEIGHTS.split("/")),
        "IRODORI_VOICES_DIR": os.path.join(root, "voices"),
        "IRODORI_PRELOAD": "true",  # the model is loaded before the port opens: mam's start waits for it
        "IRODORI_ALLOW_NO_REF_VOICE": "true",
    }
    if not os.path.isfile(env["IRODORI_CHECKPOINT"]):
        raise FileNotFoundError(f"the weights of the model {choice} are not installed: {env['IRODORI_CHECKPOINT']}")
    return env


def patch_silentcipher(silentcipher, root: str) -> None:
    """Points SilentCipher at the weights mam fetched. Its own get_model looks in ../Models of the working directory and otherwise
    downloads them from Hugging Face; the vendor's inference code calls it without paths."""
    ckpt = os.path.join(root, *SILENTCIPHER_DIR.split("/"))
    config = os.path.join(ckpt, "hparams.yaml")
    original = silentcipher.get_model

    def get_model(model_type="44.1k", ckpt_path=None, config_path=None, device="cpu"):
        return original(model_type=model_type, ckpt_path=ckpt, config_path=config, device=device)

    silentcipher.get_model = get_model
    server = sys.modules.get("silentcipher.server")
    if server is not None:
        server.get_model = get_model


def main() -> int:
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("--host", default=BIND_ADDR)
    parser.add_argument("--port", type=int, required=True)
    args, _ = parser.parse_known_args()
    if args.host != BIND_ADDR:
        print(f"this server listens on {BIND_ADDR} only", file=sys.stderr)
        return 2
    token = os.environ.get(TOKEN_ENV, "")
    if len(token) < MIN_TOKEN_LEN:
        print(f"{TOKEN_ENV} is not set: mam starts this server with a token", file=sys.stderr)
        return 2
    root = os.getcwd()
    try:
        os.environ.update(server_env(root, os.environ.get(CHOICE_ENV, ""), args.port))
    except (ValueError, FileNotFoundError) as exc:
        print(str(exc), file=sys.stderr)
        return 2

    import silentcipher  # the vendor's inference code imports it too; the patch has to be in place before the first synthesis

    patch_silentcipher(silentcipher, root)
    from irodori_openai_tts.app import app  # reads the IRODORI_* settings above and, with preload, loads the model at startup

    import uvicorn

    try:
        uvicorn.run(app=Guard(app, args.port, token), host=BIND_ADDR, port=args.port, workers=1)
    except Exception:
        traceback.print_exc()
        os.kill(os.getpid(), signal.SIGTERM)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
