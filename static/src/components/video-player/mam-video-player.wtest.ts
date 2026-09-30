import { aTimeout, expect, fixture, html, oneEvent, waitUntil } from '@open-wc/testing';
import './mam-video-player';
import type { MamVideoPlayer, SeekRequestedDetail } from './mam-video-player';
import { FakeSocket, stubFetch, stubWebSocket } from '../testdata/fakes';
import { initShotProgress } from '../../shot';

// 2 s, 25 fps, 160x90. The dev server does not answer Range requests, so the file is played from a
// blob URL: a video that cannot be ranged cannot be seeked.
const CLIP = URL.createObjectURL(await (await fetch('/src/components/testdata/clip.webm')).blob());
const JUDGE_URL = '/api/v1/takes/t1/judge-result';

async function player(attrs: { src?: string; api?: string; ws?: string; fps?: number } = {}): Promise<MamVideoPlayer> {
  const el = await fixture<MamVideoPlayer>(html`<mam-video-player
    video-src=${attrs.src ?? ''}
    api-url=${attrs.api ?? ''}
    ws-url=${attrs.ws ?? ''}
    fps=${attrs.fps ?? 0}
  ></mam-video-player>`);
  return el;
}

async function loaded(el: MamVideoPlayer): Promise<HTMLVideoElement> {
  const video = el.querySelector('video') as HTMLVideoElement;
  await waitUntil(() => video.readyState >= 1, 'video metadata did not load', { timeout: 5000 });
  await el.updateComplete;
  return video;
}

describe('mam-video-player: seeking', () => {
  it('seeks to an exact frame and tells the others', async () => {
    const el = await player({ src: CLIP, fps: 25 });
    const video = await loaded(el);

    const seek = oneEvent(el, 'seek-requested');
    el.seekFrame(25);
    const { detail } = (await seek) as CustomEvent<SeekRequestedDetail>;

    expect(detail.frame).to.equal(25);
    expect(detail.time).to.be.closeTo(1.02, 1e-6); // the middle of frame 25
    expect(video.currentTime).to.be.closeTo(1.02, 0.01);
    expect(video.paused).to.equal(true);
  });

  it('steps one frame at a time with the buttons and the arrow keys, and stays inside the video', async () => {
    const el = await player({ src: CLIP, fps: 25 });
    await loaded(el);
    const timecode = () => el.querySelector('[data-timecode]')?.textContent;

    (el.querySelector('[data-action="next-frame"]') as HTMLButtonElement).click();
    await waitUntil(() => timecode() === '00:00:01');
    (el.querySelector('[data-action="next-frame"]') as HTMLButtonElement).click();
    await waitUntil(() => timecode() === '00:00:02');
    (el.querySelector('[data-action="prev-frame"]') as HTMLButtonElement).click();
    await waitUntil(() => timecode() === '00:00:01');

    el.querySelector('video')!.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await waitUntil(() => timecode() === '00:00:02');

    el.seekFrame(0);
    (el.querySelector('[data-action="prev-frame"]') as HTMLButtonElement).click();
    await aTimeout(50);
    expect(timecode()).to.equal('00:00:00');
  });

  it('seeks from the time bar', async () => {
    const el = await player({ src: CLIP, fps: 25 });
    await loaded(el);
    const bar = el.querySelector('input[type="range"]') as HTMLInputElement;

    const seek = oneEvent(el, 'seek-requested');
    bar.value = '1.48'; // a multiple of the 1/25 s step of the bar
    bar.dispatchEvent(new Event('input', { bubbles: true }));
    const { detail } = (await seek) as CustomEvent<SeekRequestedDetail>;

    expect(detail.frame).to.equal(37);
  });

  it('measures the frame rate of the file while it plays when the server did not give one', async () => {
    const el = await player({ src: CLIP });
    const video = await loaded(el);
    expect(el.frameRateKnown).to.equal(false);

    video.muted = true;
    await video.play();
    await waitUntil(() => el.frameRateKnown, 'the frame rate was not measured', { timeout: 5000 });
    video.pause();

    expect(el.frameRate).to.equal(25);
  });
});

describe('mam-video-player: Judge Issue markers', () => {
  let restore: () => void;
  afterEach(() => restore());

  it('draws one marker per Issue over the time bar, frames first', async () => {
    ({ restore } = stubFetch({
      [`GET ${JUDGE_URL}`]: {
        body: {
          issues: [
            { type: 'product_logo_deformation', severity: 'high', time_range: '0.4-0.8s', frames: [10, 19] },
            { type: 'face_identity_drift', severity: 'medium', time_range: '1.0-1.5s' },
            { type: 'unreadable', severity: 'low', time_range: 'whenever' },
          ],
        },
      },
    }));
    const el = await player({ src: CLIP, api: JUDGE_URL, fps: 25 });
    await loaded(el);
    await waitUntil(() => el.querySelectorAll('[data-marker]').length === 2, 'markers were not drawn');

    const [first, second] = Array.from(el.querySelectorAll<HTMLElement>('[data-marker]'));
    expect(first!.dataset.severity).to.equal('high');
    expect(first!.getAttribute('aria-label')).to.contain('product_logo_deformation');
    // frames 10..19 at 25 fps = 0.4 s to 0.8 s of a 2 s video
    expect(parseFloat(first!.style.left)).to.be.closeTo(20, 0.5);
    expect(parseFloat(first!.style.width)).to.be.closeTo(20, 0.5);
    expect(parseFloat(second!.style.left)).to.be.closeTo(50, 0.5);
  });

  it('jumps to the start of an Issue when its marker is clicked', async () => {
    ({ restore } = stubFetch({
      [`GET ${JUDGE_URL}`]: { body: { issues: [{ type: 'flicker', severity: 'low', time_range: '1.0-1.4s' }] } },
    }));
    const el = await player({ src: CLIP, api: JUDGE_URL, fps: 25 });
    await loaded(el);
    await waitUntil(() => el.querySelector('[data-marker]') !== null);

    const seek = oneEvent(el, 'seek-requested');
    (el.querySelector('[data-marker]') as HTMLButtonElement).click();
    const { detail } = (await seek) as CustomEvent<SeekRequestedDetail>;

    expect(detail.frame).to.equal(25);
  });

  it('shows no markers for a Take that has not been judged (404) or when the request fails', async () => {
    ({ restore } = stubFetch({}));
    const el = await player({ src: CLIP, api: JUDGE_URL, fps: 25 });
    await loaded(el);
    await aTimeout(50);
    expect(el.querySelectorAll('[data-marker]').length).to.equal(0);
    expect(el.querySelector('video')).to.not.equal(null);
  });
});

describe('mam-video-player: generation placeholder', () => {
  let restoreSocket: () => void;
  beforeEach(() => {
    restoreSocket = stubWebSocket();
  });
  afterEach(() => restoreSocket());

  const placeholder = (el: MamVideoPlayer) => el.querySelector('[data-placeholder]') as HTMLElement | null;

  it('reflects the progress pushed over the WebSocket instead of a video', async () => {
    const el = await player({ ws: '/ws/shots/s1' });
    expect(placeholder(el)!.textContent).to.contain('動画はまだありません');
    const socket = FakeSocket.instances[0]!;
    expect(socket.url).to.match(/^wss?:\/\/.+\/ws\/shots\/s1$/);

    socket.emit({ shot_id: 's1', action: 'generate', stage: 'queued' });
    await waitUntil(() => placeholder(el)!.textContent!.includes('順番を待っています'));

    socket.emit({ shot_id: 's1', action: 'generate', stage: 'generating', takes_done: 1, takes_total: 4 });
    await waitUntil(() => placeholder(el)!.textContent!.includes('1 / 4 Take'));
    expect(parseFloat((el.querySelector('[data-progress-bar]') as HTMLElement).style.width)).to.equal(25);
    expect(el.querySelector('video')).to.equal(null);
  });

  it('announces the end of the work and closes the socket', async () => {
    const el = await player({ ws: '/ws/shots/s1' });
    const socket = FakeSocket.instances[0]!;

    const finished = oneEvent(el, 'progress-finished');
    socket.emit({ shot_id: 's1', action: 'generate', stage: 'done' });
    await finished;

    expect(socket.closed).to.equal(true);
  });

  it('covers a video that is already there while a new generation runs, and closes the socket when removed', async () => {
    const el = await player({ src: CLIP, ws: '/ws/shots/s1', fps: 25 });
    const socket = FakeSocket.instances[0]!;
    expect(el.querySelector('video')).to.not.equal(null);

    socket.emit({ shot_id: 's1', action: 'generate', stage: 'generating', takes_done: 0, takes_total: 2 });
    await waitUntil(() => placeholder(el) !== null);

    el.remove();
    expect(socket.closed).to.equal(true);
  });

  it('tells the user when the progress can no longer be followed and reconnects on request', async () => {
    // The backoff between attempts (500 ms and more) would make this test take minutes: run it at once.
    const setTimeoutOriginal = window.setTimeout;
    window.setTimeout = ((fn: () => void, ms?: number, ...args: unknown[]) =>
      (ms ?? 0) >= 400 ? setTimeoutOriginal(fn, 0) : setTimeoutOriginal(fn, ms, ...args)) as typeof window.setTimeout;
    try {
      const el = await player({ ws: '/ws/shots/s1' });
      const notice = () => el.querySelector('[data-connection-lost]');
      expect(notice()).to.equal(null);

      // Every attempt fails until the retries are used up (waitUntil's own timer is slowed down as well: poll by hand).
      for (let i = 0; i < 20 && notice() === null; i++) {
        FakeSocket.instances.filter((s) => !s.closed).forEach((s) => ((s.closed = true), s.onclose?.()));
        await aTimeout(20);
      }
      await el.updateComplete;
      expect(notice()).to.not.equal(null);
      expect(notice()!.getAttribute('role')).to.equal('alert');
      expect(notice()!.textContent).to.contain('接続が切れました');

      const before = FakeSocket.instances.length;
      (el.querySelector('[data-action="reconnect"]') as HTMLButtonElement).click();
      expect(FakeSocket.instances.length).to.equal(before + 1);
      FakeSocket.instances.at(-1)!.onopen?.();
      await el.updateComplete;
      expect(notice()).to.equal(null);
    } finally {
      window.setTimeout = setTimeoutOriginal;
    }
  });

  it('shows the placeholder when the video cannot be played', async () => {
    const el = await player({ src: '/src/components/testdata/missing.webm' });
    await waitUntil(() => placeholder(el)?.textContent?.includes('再生できません') === true, 'no error shown', { timeout: 5000 });
  });
});

// The refresh is an htmx GET into #scene-shot-view: the test only watches it start.
function refreshes(): { urls: string[]; stop: () => void } {
  const urls: string[] = [];
  const on = (e: Event) => urls.push((e as CustomEvent<{ pathInfo: { requestPath: string } }>).detail.pathInfo.requestPath);
  document.body.addEventListener('htmx:beforeRequest', on);
  return { urls, stop: () => document.body.removeEventListener('htmx:beforeRequest', on) };
}

describe('Shot card: one WebSocket per card (shot.ts)', () => {
  let restoreSocket: () => void;
  beforeEach(() => {
    restoreSocket = stubWebSocket();
  });
  afterEach(() => restoreSocket());

  it('a card with a player is followed by the player alone, and is refreshed when the player says it is over', async () => {
    const watch = refreshes();
    const root = await fixture(html`<div>
      <div id="scene-shot-view"></div>
      <ul>
        <li data-shot-progress data-refresh-url="/refresh/player">
          <mam-video-player ws-url="/ws/shots/s1"></mam-video-player>
        </li>
      </ul>
    </div>`);
    initShotProgress(root);

    expect(FakeSocket.instances.map((s) => s.url)).to.deep.equal([`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/ws/shots/s1`]);

    FakeSocket.instances[0]!.emit({ shot_id: 's1', stage: 'generating', takes_done: 1, takes_total: 3 });
    await aTimeout(20);
    expect(watch.urls).to.deep.equal([]);

    FakeSocket.instances[0]!.emit({ shot_id: 's1', stage: 'done' });
    await aTimeout(50);
    watch.stop();
    expect(watch.urls).to.deep.equal(['/refresh/player']);
    expect(FakeSocket.instances.length).to.equal(1);
  });

  it('a card without a player is followed over data-ws-url, and is refreshed when the work is over', async () => {
    const watch = refreshes();
    const root = await fixture(html`<div>
      <div id="scene-shot-view"></div>
      <ul>
        <li data-shot-progress data-refresh-url="/refresh/socket" data-ws-url="/ws/shots/s2"></li>
      </ul>
    </div>`);
    initShotProgress(root);
    expect(FakeSocket.instances.length).to.equal(1);
    expect(FakeSocket.instances[0]!.url).to.contain('/ws/shots/s2');

    FakeSocket.instances[0]!.emit({ shot_id: 's2', stage: 'failed' });
    await aTimeout(50);
    watch.stop();
    expect(watch.urls).to.deep.equal(['/refresh/socket']);
  });
});
