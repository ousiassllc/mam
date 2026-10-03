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
    request_shot_url: `/edit-timelines/${id}/request-additional-shot`,
    preview_url: `/edit-timelines/${id}/preview`,
    transitions: ['cut', 'fade', 'fade_black', 'dissolve'],
    limits: { min_clip_seconds: 0.5, max_transition_seconds: 2, max_clips: 200, max_caption_runes: 10 },
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

  it('shows the error, not "no Rough Cut yet", when the first load fails with something other than 404', async () => {
    stub = stubFetch({ [`GET ${API}`]: { status: 503, body: { title: 'Service Unavailable' } } });
    const el = await editor();
    expect(el.querySelector('[data-error]')).to.not.equal(null);
    expect(el.querySelector('[data-empty]')).to.equal(null);
  });

  it('keeps the timeline and the unsaved edits and shows an error when a reload fails', async () => {
    let fail = false;
    stub = stubFetch({ [`GET ${API}`]: () => (fail ? { status: 503, body: { title: 'Service Unavailable' } } : { body: timeline('e1', three()) }) });
    const el = await editor();
    (el.querySelector('[data-clip="c"] [data-action="up"]') as HTMLButtonElement).click();
    await waitUntil(() => order(el).join() === 'a,c,b');

    fail = true;
    document.dispatchEvent(new CustomEvent('timeline-reload')); // a Rough Cut was made: the page asks for the timeline again
    await waitUntil(() => el.querySelector('[data-error]') !== null);
    expect(order(el)).to.deep.equal(['a', 'c', 'b']);
    expect(el.querySelector('[data-empty]')).to.equal(null);

    (el.querySelector('[data-action="revert"]') as HTMLButtonElement).click(); // 変更を戻す fails as well
    await waitUntil(() => stub.requests.filter((r) => r.method === 'GET').length === 3);
    await el.updateComplete;
    expect(order(el)).to.deep.equal(['a', 'c', 'b']);
    expect(el.querySelector('[data-error]')).to.not.equal(null);

    fail = false;
    (el.querySelector('[data-action="revert"]') as HTMLButtonElement).click();
    await waitUntil(() => order(el).join() === 'a,b,c' && el.querySelector('[data-error]') === null);
  });

  it('reloads to the empty state when the timeline is gone (404)', async () => {
    let gone = false;
    stub = stubFetch({ [`GET ${API}`]: () => (gone ? { status: 404, body: { title: 'Not Found' } } : { body: timeline('e1', three()) }) });
    const el = await editor();
    gone = true;
    document.dispatchEvent(new CustomEvent('timeline-reload'));
    await waitUntil(() => el.querySelector('[data-empty]') !== null);
    expect(el.querySelector('[data-clips]')).to.equal(null);
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

  it('says the save is done but the reload failed, without "saved" or timeline-updated, when the reload after a save fails', async () => {
    let gets = 0;
    stub = stubFetch({
      [`GET ${API}`]: () => {
        gets += 1;
        return gets === 1 ? { body: timeline('e1', three()) } : { status: 503, body: { title: 'Service Unavailable' } };
      },
      'PUT /edit-timelines/e1': { status: 200 },
    });
    const el = await editor();
    let updated = false;
    el.addEventListener('timeline-updated', () => (updated = true));
    (el.querySelector('[data-clip="c"] [data-action="up"]') as HTMLButtonElement).click();
    await waitUntil(() => order(el).join() === 'a,c,b');

    (el.querySelector('[data-action="save"]') as HTMLButtonElement).click();
    await waitUntil(() => el.querySelector('[data-error]') !== null);
    await el.updateComplete;

    expect(text(el, '[data-error]')).to.contain('再読込に失敗しました');
    expect(el.querySelector('[data-notice]')).to.equal(null);
    expect(updated).to.equal(false);
    expect(text(el, '[data-total]')).to.contain('未保存');
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

  it('edits the caption of a clip and saves it with the clip kind', async () => {
    let gets = 0;
    stub = stubFetch({
      [`GET ${API}`]: () => {
        gets += 1;
        return { body: timeline(`e${gets}`, [clip('a', 0, 4), clip('r', 0, 2, { kind: 'reaction' })]) };
      },
      'PUT /edit-timelines/e1': { status: 200 },
    });
    const el = await editor();
    expect(text(el, '[data-clip="r"] [data-kind]')).to.equal('リアクション');
    expect(el.querySelector('[data-clip="a"] [data-kind]')).to.equal(null);

    type(el.querySelector('[data-clip="a"] [data-field="caption"]') as HTMLTextAreaElement as unknown as HTMLInputElement, '  朝が来た ');
    await waitUntil(() => text(el, '[data-total]')?.includes('未保存') === true);
    (el.querySelector('[data-action="save"]') as HTMLButtonElement).click();
    await waitUntil(() => stub.requests.some((r) => r.method === 'PUT'));

    const sent = (stub.requests.find((r) => r.method === 'PUT')!.body as { clips: Array<Record<string, unknown>> }).clips;
    expect(sent[0]).to.deep.include({ shot_id: 'a', kind: 'main', caption: '朝が来た' });
    expect(sent[1]).to.deep.include({ shot_id: 'r', kind: 'reaction', caption: '' });
  });

  it('refuses a caption over the limit without changing the clip', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();

    type(el.querySelector('[data-clip="a"] [data-field="caption"]') as HTMLTextAreaElement as unknown as HTMLInputElement, 'あ'.repeat(11));
    await waitUntil(() => el.querySelector('[data-error]') !== null);

    expect(text(el, '[data-error]')).to.contain('10 文字以内');
    expect((el.querySelector('[data-action="save"]') as HTMLButtonElement).disabled).to.equal(true);
  });

  it('puts the accepted value back into a caption that was refused, and drops the refused text on a reload', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();
    const box = el.querySelector('[data-clip="a"] [data-field="caption"]') as HTMLTextAreaElement;

    type(box as unknown as HTMLInputElement, 'あ'.repeat(11));
    await waitUntil(() => el.querySelector('[data-error]') !== null);
    await el.updateComplete;
    expect(box.value).to.equal('あ'.repeat(11)); // stays in the box so it can be shortened

    (el.querySelector('[data-action="revert"]') as HTMLButtonElement | null)?.click();
    document.dispatchEvent(new CustomEvent('timeline-reload'));
    await waitUntil(() => el.querySelector('[data-error]') === null);
    await el.updateComplete;
    expect(box.value).to.equal(''); // the model's caption
  });

  it('writes the clamped transition length back into the input, also when the model does not change', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', [clip('a', 0, 4), clip('b', 0, 3, { transition: { type: 'fade', seconds: 1 } }), clip('c', 0, 2)]) } });
    const el = await editor();
    const seconds = el.querySelector('[data-clip="b"] [data-field="transition-seconds"]') as HTMLInputElement;

    type(seconds, '5'); // max_transition_seconds = 2
    await waitUntil(() => seconds.value === '2');
    type(seconds, '5'); // the model is already 2: no change, the input still has to go back
    await waitUntil(() => seconds.value === '2');
    type(seconds, '0');
    await waitUntil(() => seconds.value === '0.1');
    type(seconds, '');
    await waitUntil(() => seconds.value === '0.5');
  });

  it('keeps every transition select on its own clip after the user changed one and the clips were reordered', async () => {
    const fade = { transition: { type: 'fade' as const, seconds: 0.5 } };
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', [clip('a', 0, 4), clip('b', 0, 3), clip('c', 0, 2, fade)]) } });
    const el = await editor();
    const select = (shot: string) => el.querySelector(`[data-clip="${shot}"] [data-field="transition"]`) as HTMLSelectElement;
    const shown = () => [...el.querySelectorAll<HTMLSelectElement>('[data-field="transition"]')].map((s) => s.value);

    type(select('b'), 'fade'); // b: cut → fade (the option is dirty now)
    await waitUntil(() => el.querySelector('[data-clip="b"] [data-field="transition-seconds"]') !== null);
    type(select('b'), 'cut');
    await waitUntil(() => el.querySelector('[data-clip="b"] [data-field="transition-seconds"]') === null);
    expect(shown()).to.deep.equal(['cut', 'fade']);

    (el.querySelector('[data-clip="c"] [data-action="up"]') as HTMLButtonElement).click(); // a c b
    await waitUntil(() => order(el).join() === 'a,c,b');
    await el.updateComplete;
    expect(shown()).to.deep.equal(['fade', 'cut']);

    (el.querySelector('[data-clip="c"] [data-action="remove"]') as HTMLButtonElement).click(); // a b
    await waitUntil(() => order(el).join() === 'a,b');
    await el.updateComplete;
    expect(shown()).to.deep.equal(['cut']);
  });

  it('shows the additional Shot a need_shot verdict asks the Director for', async () => {
    const judge = {
      verdict: 'need_shot' as const,
      findings: [
        {
          clip_index: 2,
          issue: 'リアクションがありません',
          action: 'リアクションショットを足す',
          need_shot: { kind: 'reaction' as const, after_clip: 2, objective: '驚く', duration_seconds: 1.5 },
        },
      ],
    };
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three(), { judge }) } });
    const el = await editor();

    expect(text(el, '[data-judge]')).to.contain('素材不足');
    expect(text(el, '[data-need-shot]')).to.contain('リアクション 1.5 秒をクリップ 2 の後に');
  });
});
