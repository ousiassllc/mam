"""MuseTalk 1.5 loaded once, and one lip-sync generation (spec/architecture/model-manager/local-runtime.md section 13.3).

The generation follows scripts/inference.py of MuseTalk (MIT License, Copyright (c) 2024 Tencent Music Entertainment Group), for the
version v15, with the fixed settings of that script's defaults. It imports torch and MuseTalk lazily, so that the HTTP layer
(mam_musetalk_server.py) is tested without them.
"""

from __future__ import annotations

import os
import shutil
import sys
import tempfile

from mam_musetalk_common import IMAGE_EXTS, MAX_FRAME_BYTES, LipSyncError, run_ffmpeg


class MuseTalkEngine:
    """MuseTalk 1.5 loaded once. Imports torch and MuseTalk lazily so that the HTTP layer above does not need them."""

    def __init__(self, device: str, use_float16: bool):
        self.requested_device = device
        self.use_float16 = use_float16
        self.device_name = ""

    def info(self) -> dict:
        return {"model": "musetalk-1.5", "device": self.device_name, "float16": self.use_float16}

    def load(self) -> None:
        import torch

        if self.requested_device == "cuda" and not torch.cuda.is_available():
            raise SystemExit("CUDA が使えません。NVIDIA のドライバーが入った NVIDIA の GPU が必要です")
        self.torch = torch
        self.device = torch.device("cuda:0" if self.requested_device == "cuda" else "cpu")
        self.device_name = torch.cuda.get_device_name(0) if self.device.type == "cuda" else "cpu"

        from omegaconf import OmegaConf  # noqa: F401 - imported here so that a missing package fails the start, not a request
        from transformers import WhisperModel

        from musetalk.utils import blending, preprocessing, utils
        from musetalk.utils.audio_processor import AudioProcessor
        from musetalk.utils.face_parsing import FaceParsing

        self.blending, self.preprocessing, self.datagen = blending, preprocessing, utils.datagen  # the import loads DWPose and S3FD

        vae, unet, pe = utils.load_all_model(
            unet_model_path=os.path.join("models", "musetalkV15", "unet.pth"), vae_type="sd-vae",
            unet_config=os.path.join("models", "musetalkV15", "musetalk.json"), device=self.device,
        )
        if self.use_float16:
            pe = pe.half()
            vae.vae = vae.vae.half()
            unet.model = unet.model.half()
        self.pe = pe.to(self.device)
        vae.vae = vae.vae.to(self.device)
        unet.model = unet.model.to(self.device)
        self.vae, self.unet = vae, unet
        self.weight_dtype = unet.model.dtype
        whisper_dir = os.path.join("models", "whisper")
        self.audio_processor = AudioProcessor(feature_extractor_path=whisper_dir)
        self.whisper = WhisperModel.from_pretrained(whisper_dir).to(device=self.device, dtype=self.weight_dtype).eval()
        self.whisper.requires_grad_(False)
        self.face_parsing = FaceParsing(left_cheek_width=90, right_cheek_width=90)
        self.timesteps = torch.tensor([0], device=self.device)

    def lipsync(self, req: dict) -> dict:
        work = tempfile.mkdtemp(prefix="lipsync-")
        try:
            return self._lipsync(req, work)
        except self.torch.cuda.OutOfMemoryError:
            self.torch.cuda.empty_cache()
            raise LipSyncError(
                "out_of_memory",
                "ビデオメモリが足りません。動画の解像度を下げるか、短くして、もう一度お試しください",
            ) from None
        finally:
            shutil.rmtree(work, ignore_errors=True)
            if self.device.type == "cuda":
                self.torch.cuda.empty_cache()

    def _lipsync(self, req: dict, work: str) -> dict:
        import cv2
        import numpy as np

        torch = self.torch
        ffmpeg, video, audio, output, fps = req["ffmpeg_path"], req["video_path"], req["audio_path"], req["output_path"], req["fps"]

        frames_dir = os.path.join(work, "frames")
        os.makedirs(frames_dir)
        if video.lower().endswith(IMAGE_EXTS):
            img_list = [video]
        else:
            self._check_video_size(video)
            run_ffmpeg(ffmpeg, "-i", video, "-start_number", "0", os.path.join(frames_dir, "%08d.png"))
            img_list = sorted(f for f in (os.path.join(frames_dir, n) for n in os.listdir(frames_dir)) if f.endswith(".png"))
            cap = cv2.VideoCapture(video)
            fps = int(round(cap.get(cv2.CAP_PROP_FPS))) or fps
            cap.release()
        if not img_list:
            raise LipSyncError("bad_request", "動画からフレームを取り出せませんでした。動画のファイルを確かめてください")

        wav = os.path.join(work, "audio.wav")
        run_ffmpeg(ffmpeg, "-i", audio, "-vn", "-ac", "1", "-ar", "16000", wav)

        with torch.no_grad():
            features, librosa_length = self.audio_processor.get_audio_feature(wav)
            chunks = self.audio_processor.get_whisper_chunk(
                features, self.device, self.weight_dtype, self.whisper, librosa_length, fps=fps,
                audio_padding_length_left=2, audio_padding_length_right=2,
            )
            if len(chunks) == 0:
                raise LipSyncError("bad_request", "音声が短すぎて口パクを作れません")

            coord_list, frame_list = self.preprocessing.get_landmark_and_bbox(img_list, 0)
            # A frame where no face was found is dropped (scripts/inference.py keeps its place and then skips its output, which leaves a gap
            # that ends the video there): the video the audio is laid over is the frames that have a face, played back and forth.
            faces = [(b, f) for b, f in zip(coord_list, frame_list) if b != self.preprocessing.coord_placeholder]
            if not faces:
                raise LipSyncError("no_face", "動画から顔を見つけられませんでした。顔がはっきり写っている動画にしてください")
            coord_list = [b for b, _ in faces]
            frame_list = [f for _, f in faces]
            extra_margin = 10
            latents = []
            for bbox, frame in faces:
                x1, y1, x2, y2 = bbox
                y2 = min(y2 + extra_margin, frame.shape[0])
                crop = cv2.resize(frame[y1:y2, x1:x2], (256, 256), interpolation=cv2.INTER_LANCZOS4)
                latents.append(self.vae.get_latents_for_unet(crop))

            frame_cycle = frame_list + frame_list[::-1]
            coord_cycle = coord_list + coord_list[::-1]
            latent_cycle = latents + latents[::-1]
            results = []
            for whisper_batch, latent_batch in self.datagen(whisper_chunks=chunks, vae_encode_latents=latent_cycle, batch_size=8, delay_frame=0, device=self.device):
                audio_feature = self.pe(whisper_batch)
                pred = self.unet.model(latent_batch.to(dtype=self.unet.model.dtype), self.timesteps, encoder_hidden_states=audio_feature).sample
                results.extend(self.vae.decode_latents(pred))

            out_dir = os.path.join(work, "out")
            os.makedirs(out_dir)
            for i, res in enumerate(results):
                x1, y1, x2, y2 = coord_cycle[i % len(coord_cycle)]
                original = frame_cycle[i % len(frame_cycle)].copy()
                y2 = min(y2 + extra_margin, original.shape[0])
                try:
                    res = cv2.resize(res.astype(np.uint8), (x2 - x1, y2 - y1))
                    merged = self.blending.get_image(original, res, [x1, y1, x2, y2], mode="jaw", fp=self.face_parsing)
                except Exception as err:  # the frame stays as it was, the numbering of the frames stays whole
                    print("frame", i, "not blended:", repr(err), file=sys.stderr, flush=True)
                    merged = original
                cv2.imwrite(os.path.join(out_dir, f"{i:08d}.png"), merged)

        run_ffmpeg(
            ffmpeg, "-r", str(fps), "-f", "image2", "-i", os.path.join(out_dir, "%08d.png"), "-i", audio,
            "-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "18", "-c:a", "aac", "-shortest",
            "-movflags", "+faststart", output,
        )
        return {"output_path": output, "frames": len(results), "fps": fps, "duration_sec": round(len(results) / fps, 3)}

    @staticmethod
    def _check_video_size(video: str) -> None:
        import cv2

        cap = cv2.VideoCapture(video)
        try:
            count = int(cap.get(cv2.CAP_PROP_FRAME_COUNT))
            width, height = int(cap.get(cv2.CAP_PROP_FRAME_WIDTH)), int(cap.get(cv2.CAP_PROP_FRAME_HEIGHT))
        finally:
            cap.release()
        if count * width * height * 3 > MAX_FRAME_BYTES:
            raise LipSyncError(
                "too_large",
                f"動画が大きすぎます（{width}x{height}、{count} フレーム）。短く切るか、解像度を下げてください",
            )
