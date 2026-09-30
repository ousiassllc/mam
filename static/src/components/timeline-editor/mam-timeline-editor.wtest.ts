import { expect, fixture, html, oneEvent, waitUntil } from '@open-wc/testing';
import './mam-timeline-editor';
import type { EditTimelineBody, MamTimelineEditor } from './mam-timeline-editor';
import type { EditClip } from '../lib/edit-model';
import { stubFetch } from '../testdata/fakes';

const API = '/api/v1/projects/p1/edit-timeline';

function clip(shot: string, inPoint: number, outPoint: number, over: Partial<EditClip> = {}): EditClip {
  return {
    shot_id: shot,
    take_id: `t-${shot}`,
    label: `Scene 1 · ${shot}`,
    objective: `objective of ${shot}`,
    source_seconds: 6,
    in_point: inPoint,
    out_point: outPoint,
    start: 0,
    transition: { type: 'cut', seconds: 0 },
    video_url: `/takes/t-${shot}/video`,
    thumbnail_url: `/takes/t-${shot}/thumbnail`,
    ...over,
  };
}

function timeline(id: string, clips: EditClip[], over: Partial<EditTimelineBody> = {}): EditTimelineBody {
  return {
    id,
    version_number: 1,
    duration_seconds: 0,
    clips,
    save_url: `/edit-timelines/${id}`,
    preview_url: `/edit-timelines/${id}/preview`,
    transitions: ['cut', 'fade', 'fade_black', 'dissolve'],
    limits: { min_clip_seconds: 0.5, max_transition_seconds: 2, max_clips: 200 },
    readonly: false,
    ...over,
  };
}

const three = () => [clip('a', 0, 4), clip('b', 0, 3), clip('c', 0, 2)];

async function editor(): Promise<MamTimelineEditor> {
  const el = await fixture<MamTimelineEditor>(html`<mam-timeline-editor api-url=${API}></mam-timeline-editor>`);
  await waitUntil(() => el.querySelector('[data-clips], [data-empty], [data-error]') !== null, 'the timeline was not shown');
  return el;
}

const order = (el: Element) => [...el.querySelectorAll('[data-clip]')].map((n) => n.getAttribute('data-clip'));
const text = (el: Element, selector: string) => el.querySelector(selector)?.textContent?.trim();

function type(input: HTMLInputElement | HTMLSelectElement, value: string): void {
  input.value = value;
  input.dispatchEvent(new Event('change', { bubbles: true }));
}

describe('mam-timeline-editor', () => {
  let stub: ReturnType<typeof stubFetch>;
  afterEach(() => stub.restore());

  it('lists the clips in order with their total length and the preview of the saved cut', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();

    expect(order(el)).to.deep.equal(['a', 'b', 'c']);
    expect(text(el, '[data-total]')).to.contain('9.0 秒');
    expect(el.querySelector('mam-video-player')?.getAttribute('video-src')).to.equal('/edit-timelines/e1/preview');
    expect((el.querySelector('[data-action="save"]') as HTMLButtonElement).disabled).to.equal(true); // nothing changed
  });

  it('says there is no Rough Cut yet on 404 without an error', async () => {
    stub = stubFetch({});
    const el = await editor();
    expect(el.querySelector('[data-empty]')).to.not.equal(null);
    expect(el.querySelector('[data-error]')).to.equal(null);
  });

  it('reorders with the arrows, sets a Cut Point and a Transition, and saves the clips to the server URL', async () => {
    let gets = 0;
    stub = stubFetch({
      [`GET ${API}`]: () => {
        gets += 1;
        return { body: gets === 1 ? timeline('e1', three()) : timeline('e2', [clip('b', 0, 3), clip('a', 1, 4, { transition: { type: 'fade', seconds: 0.5 } }), clip('c', 0, 2)], { version_number: 2 }) };
      },
      'PUT /edit-timelines/e1': { status: 200 } // the server answers 204; the fetch stub cannot build that,
    });
    const el = await editor();

    (el.querySelector('[data-clip="b"] [data-action="up"]') as HTMLButtonElement).click(); // b a c
    await waitUntil(() => order(el).join() === 'b,a,c');
    type(el.querySelector('[data-clip="a"] [data-field="in"]') as HTMLInputElement, '1');
    await waitUntil(() => text(el, '[data-total]')?.includes('未保存') === true);
    type(el.querySelector('[data-clip="a"] [data-field="transition"]') as HTMLSelectElement, 'fade');
    await waitUntil(() => el.querySelector('[data-clip="a"] [data-field="transition-seconds"]') !== null);

    const updated = oneEvent(el, 'timeline-updated');
    (el.querySelector('[data-action="save"]') as HTMLButtonElement).click();
    const event = (await updated) as CustomEvent<{ id: string }>;

    const put = stub.requests.find((r) => r.method === 'PUT')!;
    expect(put.url).to.equal('/edit-timelines/e1');
    const sent = (put.body as { clips: Array<Record<string, unknown>> }).clips;
    expect(sent.map((c) => c.shot_id)).to.deep.equal(['b', 'a', 'c']);
    expect(sent[1]).to.deep.include({ in_point: 1, out_point: 4 });
    expect(sent[1]!.transition).to.deep.equal({ type: 'fade', seconds: 0.5 });
    expect(sent[0]!.transition).to.deep.equal({ type: 'cut', seconds: 0 });
    expect(event.detail.id).to.equal('e2'); // the new version
    expect(el.querySelector('mam-video-player')?.getAttribute('video-src')).to.equal('/edit-timelines/e2/preview');
  });

  it('refuses a Cut Point past the end of the Take and keeps the last accepted value', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();

    const input = el.querySelector('[data-clip="a"] [data-field="out"]') as HTMLInputElement;
    type(input, '9');
    await waitUntil(() => el.querySelector('[data-error]') !== null);

    expect(text(el, '[data-error]')).to.contain('Take の長さ');
    expect(input.value).to.equal('4');
    expect((el.querySelector('[data-action="save"]') as HTMLButtonElement).disabled).to.equal(true);
  });

  it('shows the server error of a refused save and keeps the edit', async () => {
    stub = stubFetch({
      [`GET ${API}`]: { body: timeline('e1', three()) },
      'PUT /edit-timelines/e1': { status: 409, body: '<div>別の編集が保存されています</div>' },
    });
    const el = await editor();
    (el.querySelector('[data-clip="c"] [data-action="up"]') as HTMLButtonElement).click();
    await waitUntil(() => order(el).join() === 'a,c,b');

    (el.querySelector('[data-action="save"]') as HTMLButtonElement).click();
    await waitUntil(() => el.querySelector('[data-error]') !== null);

    expect(text(el, '[data-error]')).to.equal('別の編集が保存されています');
    expect(order(el)).to.deep.equal(['a', 'c', 'b']);
  });

  it('cannot be edited once the server says the timeline is read-only', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three(), { readonly: true }) } });
    const el = await editor();

    expect(el.querySelector('[data-readonly]')).to.not.equal(null);
    expect((el.querySelector('[data-clip="b"] [data-action="up"]') as HTMLButtonElement).disabled).to.equal(true);
    expect((el.querySelector('[data-clip="a"] [data-field="in"]') as HTMLInputElement).disabled).to.equal(true);
  });

  it('reloads the timeline when the server replaced it (timeline-reload)', async () => {
    let gets = 0;
    stub = stubFetch({
      [`GET ${API}`]: () => {
        gets += 1;
        return { body: timeline(`e${gets}`, gets === 1 ? three() : [clip('z', 0, 2)]) };
      },
    });
    const el = await editor();

    document.body.dispatchEvent(new CustomEvent('timeline-reload', { bubbles: true }));
    await waitUntil(() => order(el).join() === 'z');
    expect(el.querySelector('mam-video-player')?.getAttribute('video-src')).to.equal('/edit-timelines/e2/preview');
  });
});
