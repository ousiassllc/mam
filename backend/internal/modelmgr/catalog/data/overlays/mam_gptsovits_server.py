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

The guard needs the standard library only (it is tested without api_v2, torch or fastapi: the vendor's package is imported by main()).
"""

from __future__ import annotations

import argparse
import hmac
import os
import signal
import sys
import traceback

TOKEN_ENV = "MAM_RUNTIME_TOKEN"
TOKEN_HEADER = b"x-mam-token"  # the name of the header as the ASGI server gives it: lower case
BIND_ADDR = "127.0.0.1"
MIN_TOKEN_LEN = 16

# The requests that are answered: (method, path) -> whether the token is required. The readiness probe sends none.
ROUTES = {
    ("POST", "/tts"): True,
    ("GET", "/openapi.json"): False,
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
    """An ASGI application in front of the app of api_v2: the Host check, the allow-list of requests and the token."""

    def __init__(self, app, port: int, token: str):
        self.app = app
        self.token = token.encode("utf-8")
        self.hosts = (f"{BIND_ADDR}:{port}", f"localhost:{port}")

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
        await self.app(scope, receive, send)


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
    import api_v2  # reads sys.argv (-a -p -c) and loads the models: the port opens after that

    if api_v2.host != BIND_ADDR or api_v2.port != args.port:
        print("api_v2 did not read the address mam gave", file=sys.stderr)
        return 2
    import uvicorn

    try:
        uvicorn.run(app=Guard(api_v2.APP, args.port, token), host=BIND_ADDR, port=args.port, workers=1)
    except Exception:
        traceback.print_exc()
        os.kill(os.getpid(), signal.SIGTERM)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
