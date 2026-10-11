import { expect, waitUntil } from '@open-wc/testing';
import './mam-timeline-editor';
import type { EditTimelineBody, MamTimelineEditor } from './mam-timeline-editor';
import { holdFetch, stubFetch } from '../testdata/fakes';

const API = '/api/v1/projects/p1/edit-timeline';

const timeline: EditTimelineBody = {
  id: 'e1',
  version_number: 1,
  duration_seconds: 4,
  clips: [
    {
      shot_id: 'a',
      take_id: 't-a',
      label: 'Scene 1 · a',
      objective: 'objective of a',
      source_seconds: 6,
      in_point: 0,
      out_point: 4,
      start: 0,
      transition: { type: 'cut', seconds: 0 },
      video_url: '/takes/t-a/video',
      thumbnail_url: '/takes/t-a/thumbnail',
    },
  ],
  save_url: '/edit-timelines/e1',
  preview_url: '/edit-timelines/e1/preview',
  transitions: ['cut', 'fade', 'fade_black', 'dissolve'],
  limits: { min_clip_seconds: 0.5, max_transition_seconds: 2, max_caption_runes: 10 },
  readonly: false,
};

describe('mam-timeline-editor: moved in the DOM', () => {
  let stub: ReturnType<typeof stubFetch>;
  afterEach(() => stub.restore());

  it('loads again when it is moved while the timeline is loading', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline } });
    const release = holdFetch();
    const parent = document.createElement('div');
    document.body.append(parent);
    try {
      const el = document.createElement('mam-timeline-editor') as MamTimelineEditor;
      el.setAttribute('api-url', API);
      parent.append(el);
      await el.updateComplete;
      expect(el.querySelector('[role="status"]')?.textContent).to.contain('読み込み中');

      el.remove();
      parent.append(el);
      release();

      await waitUntil(() => el.querySelector('[data-clip="a"]') !== null, 'the timeline was not shown after the move');
    } finally {
      release();
      parent.remove();
    }
  });
});
