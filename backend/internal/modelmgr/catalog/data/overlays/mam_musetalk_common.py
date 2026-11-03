"""What the HTTP layer and the engine of mam's MuseTalk server share: the error codes and the call of ffmpeg."""

from __future__ import annotations

import subprocess

MAX_FRAME_BYTES = 6 << 30  # the frames of the input video are held in memory (as MuseTalk does): refuse a video that would not fit
FFMPEG_TIMEOUT_SEC = 3600  # one ffmpeg call (splitting the video, converting the audio, encoding): a hung ffmpeg would hold the server's busy lock forever
IMAGE_EXTS = (".jpg", ".jpeg", ".png", ".bmp", ".tif", ".tiff")

# code -> HTTP status. The Go client (backend/internal/modelmgr/lipsync) knows the same codes.
ERRORS = {
    "bad_request": 400,
    "forbidden": 403,
    "input_not_found": 404,
    "busy": 409,
    "too_large": 413,
    "no_face": 422,
    "ffmpeg_failed": 422,
    "out_of_memory": 503,
    "internal": 500,
}


class LipSyncError(Exception):
    def __init__(self, code: str, message: str):
        super().__init__(message)
        assert code in ERRORS
        self.code = code
        self.message = message


def ffmpeg_failure(proc: subprocess.CompletedProcess) -> LipSyncError:
    tail = (proc.stderr or b"").decode("utf-8", "replace").strip().splitlines()[-3:]
    return LipSyncError("ffmpeg_failed", "ffmpeg の実行に失敗しました: " + " / ".join(tail))


def run_ffmpeg(ffmpeg: str, *args: str) -> None:
    try:
        proc = subprocess.run(
            [ffmpeg, "-nostdin", "-y", "-v", "error", *args],
            stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, check=False, timeout=FFMPEG_TIMEOUT_SEC,
        )
    except subprocess.TimeoutExpired as err:  # subprocess.run has killed it
        raise LipSyncError("ffmpeg_failed", f"ffmpeg が {FFMPEG_TIMEOUT_SEC} 秒で終わらなかったので止めました") from err
    except OSError as err:
        raise LipSyncError("ffmpeg_failed", f"ffmpeg を起動できません: {err}") from err
    if proc.returncode != 0:
        raise ffmpeg_failure(proc)
