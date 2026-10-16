import { expect, fixture, html, waitUntil } from '@open-wc/testing';
import './mam-video-player';
import type { MamVideoPlayer } from './mam-video-player';

const CLIP = URL.createObjectURL(await (await fetch('/src/components/testdata/clip.webm')).blob());

const ACTIONS = ['prev-frame', 'play', 'next-frame'] as const;

const button = (el: HTMLElement, action: string) => el.querySelector(`[data-action="${action}"]`) as HTMLButtonElement;

describe('mam-video-player: transport buttons', () => {
  it('tells the three buttons apart by their visible text and gives each a tooltip equal to its accessible name', async () => {
    const el = await fixture<MamVideoPlayer>(html`<mam-video-player video-src=${CLIP} fps="25"></mam-video-player>`);

    // The play button shows ▶ while stopped; the frame steps must not repeat it.
    const visible = ACTIONS.map((a) => button(el, a).textContent!.trim());
    expect(new Set(visible).size).to.equal(ACTIONS.length);

    for (const action of ACTIONS) {
      const b = button(el, action);
      expect(b.title).to.not.equal('');
      expect(b.title).to.equal(b.getAttribute('aria-label'));
    }
    expect(button(el, 'play').title).to.equal('再生');
    expect(button(el, 'prev-frame').title).to.equal('1 コマ戻る');
    expect(button(el, 'next-frame').title).to.equal('1 コマ進む');
  });

  it('keeps the tooltip of the play button in step with its label while playing', async () => {
    const el = await fixture<MamVideoPlayer>(html`<mam-video-player video-src=${CLIP} fps="25"></mam-video-player>`);
    const video = el.querySelector('video') as HTMLVideoElement;
    await waitUntil(() => video.readyState >= 1, 'video metadata did not load', { timeout: 5000 });
    video.loop = true;
    await video.play();
    await waitUntil(() => button(el, 'play').title === '一時停止', 'the tooltip did not follow the state');

    expect(button(el, 'play').getAttribute('aria-label')).to.equal('一時停止');
    const visible = ACTIONS.map((a) => button(el, a).textContent!.trim());
    expect(new Set(visible).size).to.equal(ACTIONS.length);
    video.pause();
  });
});
