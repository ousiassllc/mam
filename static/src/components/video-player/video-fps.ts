import { createLogger } from '../lib/logger';
import { estimateFps } from '../lib/video';

const log = createLogger('video-player');

/** Number of presentation times read to measure the frame rate of a file. */
const FPS_SAMPLES = 8;
/** How long the muted probe may run to measure the frame rate before it gives up. */
const FPS_PROBE_MS = 1500;

/** Reads the presentation times of the next frames while the video runs and reports the rate they imply. */
export function sampleFrameRate(video: HTMLVideoElement, onRate: (rate: number) => void): void {
  if (typeof video.requestVideoFrameCallback !== 'function') return;
  const times: number[] = [];
  const onFrame = (_now: number, meta: VideoFrameCallbackMetadata) => {
    times.push(meta.mediaTime);
    if (times.length >= FPS_SAMPLES) {
      const rate = estimateFps(times);
      if (rate > 0) onRate(rate);
      return;
    }
    if (!video.paused) video.requestVideoFrameCallback(onFrame);
  };
  video.requestVideoFrameCallback(onFrame);
}

/** Plays the video muted for a moment until `isKnown()` (the rate was measured), then puts it back where it was. */
export async function probeFrameRate(video: HTMLVideoElement, isKnown: () => boolean): Promise<void> {
  if (typeof video.requestVideoFrameCallback !== 'function') return;
  const time = video.currentTime;
  const muted = video.muted;
  const wasPaused = video.paused;
  video.muted = true;
  try {
    await video.play();
    const deadline = Date.now() + FPS_PROBE_MS;
    while (!isKnown() && Date.now() < deadline) {
      await new Promise((resolve) => setTimeout(resolve, 30));
    }
  } catch (error) {
    log.warn('could not measure the frame rate', error);
  } finally {
    if (wasPaused) video.pause();
    video.currentTime = time;
    video.muted = muted;
  }
}
