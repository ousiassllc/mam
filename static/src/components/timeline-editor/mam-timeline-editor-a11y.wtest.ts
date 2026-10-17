import { expect } from '@open-wc/testing';
import { stubFetch } from '../testdata/fakes';
import { API, editor, three, timeline } from './timeline-fixtures';

describe('mam-timeline-editor: accessible names', () => {
  let stub: ReturnType<typeof stubFetch>;
  afterEach(() => stub.restore());

  it('names the buttons that repeat on every clip after the clip, starting with their visible text', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: timeline('e1', three()) } });
    const el = await editor();

    const remove = [...el.querySelectorAll('[data-action="remove"]')].map((b) => b.getAttribute('aria-label'));
    expect(remove).to.deep.equal(['外す: Scene 1 · a', '外す: Scene 1 · b', '外す: Scene 1 · c']);
    expect(new Set(remove).size).to.equal(remove.length);
    expect(el.querySelector('[data-action="remove"]')?.textContent?.trim()).to.equal('外す');
  });
});
