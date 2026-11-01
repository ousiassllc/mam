"""mam's local HTTP front of MuseTalk 1.5 (spec/architecture/model-manager/local-runtime.md section 13).

MuseTalk has no server of its own. mam starts this script with the Python of the runtime (working directory: the MuseTalk
source tree), it loads the models once and then answers on 127.0.0.1 only:

    GET  /health        200 {"status": "ok", ...} once the models are loaded (the port opens after the load)
    POST /v1/lipsync    {"video_path", "audio_path", "output_path", "ffmpeg_path", "fps"?} -> 200 {"output_path", ...}
                        (the header X-Mam-Token with the token of this start is required: MAM_RUNTIME_TOKEN)

Every error is {"error": {"code", "message"}} with the HTTP status of the code (ERRORS below). One request is worked on at a
time (the GPU is one): another one at the same time is answered 409 "busy".

The generation is in mam_musetalk_engine.py (it follows scripts/inference.py of MuseTalk, MIT License). This file is the HTTP
layer and needs the standard library only, so that it is tested without torch.
"""

from __future__ import annotations

import argparse
import hmac
import json
import os
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# The MuseTalk source tree is the directory of this script. -I (isolated mode) does not put it on the path, so it is done here.
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from mam_musetalk_common import ERRORS, IMAGE_EXTS, LipSyncError  # noqa: E402

TOKEN_ENV = "MAM_RUNTIME_TOKEN"
TOKEN_HEADER = "X-Mam-Token"
MAX_BODY_BYTES = 64 * 1024
DEFAULT_FPS = 25
VIDEO_EXTS = (".mp4", ".mov", ".avi", ".mkv", ".flv")


def validate_request(body: object) -> dict:
    """Checks the JSON of POST /v1/lipsync and returns the fields (raises LipSyncError bad_request / input_not_found)."""
    if not isinstance(body, dict):
        raise LipSyncError("bad_request", "リクエストは JSON のオブジェクトである必要があります")
    out: dict = {}
    for key in ("video_path", "audio_path", "output_path", "ffmpeg_path"):
        value = body.get(key)
        if not isinstance(value, str) or not value or not os.path.isabs(value) or "\x00" in value:
            raise LipSyncError("bad_request", f"{key} は絶対パスの文字列で指定してください")
        out[key] = value
    fps = body.get("fps", DEFAULT_FPS)
    if isinstance(fps, bool) or not isinstance(fps, int) or not 1 <= fps <= 60:
        raise LipSyncError("bad_request", "fps は 1 から 60 の整数で指定してください")
    out["fps"] = fps
    for key in ("video_path", "audio_path", "ffmpeg_path"):
        if not os.path.isfile(out[key]):
            raise LipSyncError("input_not_found", f"{key} のファイルが見つかりません: {out[key]}")
    if not out["video_path"].lower().endswith(VIDEO_EXTS + IMAGE_EXTS):
        raise LipSyncError("bad_request", "video_path は動画（mp4・mov・avi・mkv・flv）か画像（jpg・png・bmp・tif）のファイルにしてください")
    if not out["output_path"].lower().endswith(".mp4"):
        raise LipSyncError("bad_request", "output_path は .mp4 のファイルにしてください")
    out_dir = os.path.dirname(out["output_path"])
    if not os.path.isdir(out_dir):
        raise LipSyncError("bad_request", f"output_path のフォルダがありません: {out_dir}")
    if os.path.abspath(out["output_path"]) in (os.path.abspath(out["video_path"]), os.path.abspath(out["audio_path"])):
        raise LipSyncError("bad_request", "output_path は入力のファイルと別にしてください")
    return out


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    server: "Server"

    def log_message(self, fmt: str, *args) -> None:  # noqa: A003 - the name of the base class
        print("http:", fmt % args, file=sys.stderr, flush=True)

    def _send(self, status: int, payload: dict, close: bool = False) -> None:
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(data)))
        self.send_header("Cache-Control", "no-store")
        if close:
            self.send_header("Connection", "close")
        self.end_headers()
        self.wfile.write(data)

    def _error(self, err: LipSyncError) -> None:
        # A refusal may come before the body was read: the connection ends with it, so that the rest of the body is never taken for
        # the next request.
        self.close_connection = True
        self._send(ERRORS[err.code], {"error": {"code": err.code, "message": err.message}}, close=True)

    def _guard_host(self) -> None:
        """Refuses another Host header (DNS rebinding: a web page that made its own name point at 127.0.0.1)."""
        host = (self.headers.get("Host") or "").lower()
        if host not in (f"127.0.0.1:{self.server.port}", f"localhost:{self.server.port}"):
            raise LipSyncError("forbidden", "このアドレスでは受け付けません")

    def _guard(self) -> None:
        """Refuses a request without the token of this start (a web page cannot send it), after the Host check."""
        self._guard_host()
        given = self.headers.get(TOKEN_HEADER) or ""
        if not hmac.compare_digest(given.encode("utf-8"), self.server.token.encode("utf-8")):
            raise LipSyncError("forbidden", "トークンが一致しません")

    def do_GET(self) -> None:  # noqa: N802
        try:
            self._guard_host()  # /health says nothing but that the models are loaded: mam's readiness probe sends no token
            if self.path != "/health":
                raise LipSyncError("bad_request", "このパスはありません")
            self._send(200, {"status": "ok", **self.server.engine.info()})
        except LipSyncError as err:
            self._error(err)

    def do_POST(self) -> None:  # noqa: N802
        try:
            self._guard()
            if self.path != "/v1/lipsync":
                raise LipSyncError("bad_request", "このパスはありません")
            self._send(200, self._lipsync())
        except LipSyncError as err:
            self._error(err)
        except Exception as err:  # the engine's own failure: say what it was, keep the server up
            print("lipsync failed:", repr(err), file=sys.stderr, flush=True)
            self._error(LipSyncError("internal", f"口パクの生成に失敗しました: {type(err).__name__}: {err}"))

    def _lipsync(self) -> dict:
        if (self.headers.get("Content-Type") or "").split(";")[0].strip().lower() != "application/json":
            raise LipSyncError("bad_request", "Content-Type は application/json にしてください")
        try:
            length = int(self.headers.get("Content-Length") or "")
        except ValueError:
            raise LipSyncError("bad_request", "Content-Length が必要です") from None
        if not 0 < length <= MAX_BODY_BYTES:
            raise LipSyncError("bad_request", "リクエストの大きさが正しくありません")
        try:
            req = validate_request(json.loads(self.rfile.read(length)))
        except (json.JSONDecodeError, UnicodeDecodeError):
            raise LipSyncError("bad_request", "リクエストが JSON ではありません") from None
        if not self.server.busy.acquire(blocking=False):
            raise LipSyncError("busy", "別の口パクを生成中です。終わってからもう一度送ってください")
        try:
            started = time.monotonic()
            result = self.server.engine.lipsync(req)
            result["elapsed_sec"] = round(time.monotonic() - started, 3)
            return result
        finally:
            self.server.busy.release()


class Server(ThreadingHTTPServer):
    daemon_threads = True

    def __init__(self, host: str, port: int, engine, token: str):
        super().__init__((host, port), Handler)
        self.engine = engine
        self.token = token
        self.busy = threading.Lock()

    @property
    def port(self) -> int:
        return self.server_address[1]


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description="mam's local HTTP front of MuseTalk 1.5")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--device", choices=("cuda", "cpu"), default="cuda")
    parser.add_argument("--use-float16", action="store_true")
    args = parser.parse_args(argv)
    if args.host != "127.0.0.1":
        print("this server listens on 127.0.0.1 only", file=sys.stderr)
        return 2
    token = os.environ.get(TOKEN_ENV, "")
    if len(token) < 16:
        print(f"{TOKEN_ENV} is not set: mam starts this server with a token", file=sys.stderr)
        return 2
    from mam_musetalk_engine import MuseTalkEngine

    engine = MuseTalkEngine(args.device, args.use_float16)
    print("loading MuseTalk 1.5 ...", file=sys.stderr, flush=True)
    engine.load()
    server = Server(args.host, args.port, engine, token)
    print(f"MuseTalk 1.5 is ready on {args.host}:{server.port} ({engine.device_name})", file=sys.stderr, flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    return 0


if __name__ == "__main__":
    sys.exit(main())
