"""mam's launcher of the GPT-SoVITS server (spec/architecture/model-manager/local-runtime.md section 5.2).

The vendor's api_v2.py has no token and no Host check, and it can load any file of the PC on request (/set_gpt_weights,
/set_sovits_weights), stop or restart itself (/control) and read any path (/tts ref_audio_path). A web page of the user's browser can
send GETs to 127.0.0.1, and a name that points at 127.0.0.1 (DNS rebinding) passes as well. So mam does not start api_v2.py itself: it
starts this script with the Python of the runtime (working directory: the extracted GPT-SoVITS package, the same arguments as
api_v2.py: -a 127.0.0.1 -p <port> -c <config>), which imports api_v2, puts a guard in front of its FastAPI app and then runs uvicorn
as api_v2.py's own __main__ does. The guard answers only:

    POST /tts            the header X-Mam-Token with the token of this start is required (MAM_RUNTIME_TOKEN)
    GET  /openapi.json   mam's readiness probe sends no token (the server answers it once the models are loaded)

and only for the Host 127.0.0.1:<port> or localhost:<port>. Every other path (/control, /set_gpt_weights, /set_sovits_weights, the
docs pages, ...) is 404 even for the holder of the token, so that no request makes the server load or read what mam did not ask for.

A third request is answered by the guard itself and never reaches api_v2 (Issue #936, the voice a Character was trained to):

    POST /mam/voice      {"voice": "<id>"} loads the GPT and SoVITS weights of that trained voice, {"voice": ""} the base weights again. The
                         id names a directory below the one directory mam names in MAM_VOICE_WEIGHTS_DIR (the id is checked against a
                         strict pattern and the files must really be inside it): no request makes the server load a path of the PC.
                         It needs the token, as /tts does.

The guard needs the standard library only (it is tested without api_v2, torch or fastapi: the vendor's package is imported by main()).
"""

from __future__ import annotations

import argparse
import asyncio
import hmac
import json
import os
import re
import shutil
import signal
import sys
import traceback

TOKEN_ENV = "MAM_RUNTIME_TOKEN"
VOICE_ROOT_ENV = "MAM_VOICE_WEIGHTS_DIR"
VOICE_ID = re.compile(r"^[A-Za-z0-9-]{1,64}$")
VOICE_FILES = ("gpt.ckpt", "sovits.pth")  # the names mam gives the two weights of a trained voice
MAX_VOICE_BODY = 4096
TOKEN_HEADER = b"x-mam-token"  # the name of the header as the ASGI server gives it: lower case
BIND_ADDR = "127.0.0.1"
MIN_TOKEN_LEN = 16

# The requests that are answered: (method, path) -> whether the token is required. The readiness probe sends none.
ROUTES = {
    ("POST", "/tts"): True,
    ("GET", "/openapi.json"): False,
    ("POST", "/mam/voice"): True,
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


def voice_files(root: str, voice: str):
    """The (gpt, sovits) weight files of the trained voice below root, or None when the id is not a safe name or a file is not really there."""
    if not root or not VOICE_ID.match(voice):
        return None
    base = os.path.realpath(root)
    paths = []
    for name in VOICE_FILES:
        path = os.path.realpath(os.path.join(base, voice, name))
        if os.path.dirname(os.path.dirname(path)) != base or not os.path.isfile(path):
            return None
        paths.append(path)
    return tuple(paths)


async def _json_reply(send, status: int, message: str) -> None:
    body = json.dumps({"message": message}, ensure_ascii=False).encode("utf-8")
    headers = [(b"content-type", b"application/json"), (b"content-length", str(len(body)).encode("ascii"))]
    await send({"type": "http.response.start", "status": status, "headers": headers})
    await send({"type": "http.response.body", "body": body})


class Guard:
    """An ASGI application in front of the app of api_v2: the Host check, the allow-list of requests and the token.

    switch(weights) loads the (gpt, sovits) weights it is given, or the base weights for None; root is the directory of the trained voices.
    Without switch the guard answers /mam/voice with 404."""

    def __init__(self, app, port: int, token: str, switch=None, root: str = ""):
        self.app = app
        self.token = token.encode("utf-8")
        self.hosts = (f"{BIND_ADDR}:{port}", f"localhost:{port}")
        self.switch, self.root = switch, root

    async def _voice(self, receive, send) -> None:
        body = b""
        while True:
            message = await receive()
            body += message.get("body", b"")
            if len(body) > MAX_VOICE_BODY:
                await _json_reply(send, 413, "要求が大きすぎます")
                return
            if not message.get("more_body"):
                break
        try:
            voice = json.loads(body)["voice"]
            if not isinstance(voice, str):
                raise TypeError("voice")
        except (ValueError, KeyError, TypeError):
            await _json_reply(send, 400, "voice を文字列で指定してください")
            return
        weights = None
        if voice != "":
            weights = voice_files(self.root, voice)
            if weights is None:
                await _json_reply(send, 404, "学習済みの声のファイルがありません")
                return
        try:
            await asyncio.get_running_loop().run_in_executor(None, self.switch, weights)
        except Exception as e:  # a broken file: torch.load or load_state_dict raised
            traceback.print_exc()
            await _json_reply(send, 500, f"学習済みの声を読み込めませんでした: {type(e).__name__}: {e}")
            return
        await _json_reply(send, 200, "success")

    async def __call__(self, scope, receive, send) -> None:
        kind = scope["type"]
        if kind == "lifespan":
            await self.app(scope, receive, send)
            return
        if kind != "http":  # api_v2 has no WebSocket
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
        if scope["path"] == "/mam/voice":
            if self.switch is None:
                await _refuse(send, 404, "このパスはありません")
                return
            await self._voice(receive, send)
            return
        await self.app(scope, receive, send)


def use_private_config(argv: list) -> None:
    """Points -c of argv at a copy of the config of api_v2 (TEMP/mam_tts_infer.yaml). api_v2 writes the weights it loads back into its
    config (TTS.init_t2s_weights saves it), so a trained voice loaded through /mam/voice would otherwise become the base weights of every
    later start: the vendor's file stays as it came and the server's copy is made anew at each start."""
    for i, arg in enumerate(argv[:-1]):
        if arg in ("-c", "--tts_config"):
            dst = os.path.join(os.getcwd(), "TEMP", "mam_tts_infer.yaml")
            os.makedirs(os.path.dirname(dst), exist_ok=True)
            shutil.copyfile(argv[i + 1], dst)
            argv[i + 1] = dst
            return


def switcher(api_v2):
    """The function of Guard.switch: it loads the weights into the pipeline of api_v2, the base weights (what the server started with) for None."""
    pipeline = api_v2.tts_pipeline
    base = (pipeline.configs.t2s_weights_path, pipeline.configs.vits_weights_path)

    def switch(weights):
        gpt, sovits = weights or base
        pipeline.init_vits_weights(sovits)
        pipeline.init_t2s_weights(gpt)

    return switch


def main() -> int:
    # The same options as api_v2.py; they are read here first so that a wrong start is refused before the models are loaded.
    parser = argparse.ArgumentParser(add_help=False)
    parser.add_argument("-a", "--bind_addr", default=BIND_ADDR)
    parser.add_argument("-p", "--port", type=int, required=True)
    args, _ = parser.parse_known_args()
    if args.bind_addr != BIND_ADDR:
        print(f"this server listens on {BIND_ADDR} only", file=sys.stderr)
        return 2
    token = os.environ.get(TOKEN_ENV, "")
    if len(token) < MIN_TOKEN_LEN:
        print(f"{TOKEN_ENV} is not set: mam starts this server with a token", file=sys.stderr)
        return 2

    # api_v2.py finds its own modules from the working directory (the package's root); -I (isolated mode) does not put it on the path.
    now_dir = os.getcwd()
    sys.path.append(now_dir)
    sys.path.append("%s/GPT_SoVITS" % now_dir)
    use_private_config(sys.argv)
    import api_v2  # reads sys.argv (-a -p -c) and loads the models: the port opens after that

    if api_v2.host != BIND_ADDR or api_v2.port != args.port:
        print("api_v2 did not read the address mam gave", file=sys.stderr)
        return 2
    import uvicorn

    try:
        guard = Guard(api_v2.APP, args.port, token, switcher(api_v2), os.environ.get(VOICE_ROOT_ENV, ""))
        uvicorn.run(app=guard, host=BIND_ADDR, port=args.port, workers=1)
    except Exception:
        traceback.print_exc()
        os.kill(os.getpid(), signal.SIGTERM)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
