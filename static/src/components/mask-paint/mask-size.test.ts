import { describe, expect, it } from 'vitest';
import { canvasSize, MAX_EDGE } from './mask-size';

describe('canvasSize (#999: the memory of a huge picture)', () => {
  it('keeps the picture at its own size when its longer side is within the limit', () => {
    expect(canvasSize(1024, 1536)).toEqual({ width: 1024, height: 1536 });
    expect(canvasSize(MAX_EDGE, MAX_EDGE)).toEqual({ width: MAX_EDGE, height: MAX_EDGE });
  });

  it('scales a picture over the limit down to it on the longer side, keeping the shape', () => {
    expect(canvasSize(8192, 8192)).toEqual({ width: MAX_EDGE, height: MAX_EDGE });
    expect(canvasSize(4096, 1024)).toEqual({ width: 2048, height: 512 });
    expect(canvasSize(1000, 5000)).toEqual({ width: 410, height: 2048 });
  });

  it('never gives a side of 0, and rounds an odd size', () => {
    expect(canvasSize(100000, 3)).toEqual({ width: 2048, height: 1 });
    expect(canvasSize(0, 0)).toEqual({ width: 1, height: 1 });
    expect(canvasSize(100.4, 50.6)).toEqual({ width: 100, height: 51 });
  });

  it('holds a 8192 x 8192 picture to 2 canvases of 16 MiB instead of 2 of 256 MiB', () => {
    const { width, height } = canvasSize(8192, 8192);
    const bytes = width * height * 4;
    expect(bytes).toBe(16 * 1024 * 1024);
    expect(8192 * 8192 * 4).toBe(256 * 1024 * 1024);
  });
});
