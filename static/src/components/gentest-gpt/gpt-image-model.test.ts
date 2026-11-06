import { describe, expect, it } from 'vitest';
import { framesShown, gptOf, otherNote, viewOf } from './gpt-image-model';

const flare = { label: 'GPT Image 2.6 Flare', takes: true, refusal: '' };
const sub = { label: 'GPT Image 2.6 Flare（ChatGPT）', takes: false, refusal: 'ChatGPT サブスク経由では使えません' };
const other = { label: 'SDXL', takes: false, refusal: '' };

describe('the rules of the "GPT Image 2.6 の設定" section (#1096)', () => {
  it('reads the data attributes of a model card', () => {
    expect(gptOf({ label: 'A', gptImage: 'true', gptRefusal: '' })).toEqual({ label: 'A', takes: true, refusal: '' });
    expect(gptOf({ model: 'm', gptImage: 'false', gptRefusal: 'why' })).toEqual({
      label: 'm',
      takes: false,
      refusal: 'why',
    });
  });

  it('is in view, with its fields posted, while a ticked model takes the settings', () => {
    expect(viewOf([flare], false)).toEqual({ shown: true, fields: true, notes: [] });
    expect(viewOf([flare, other], false).fields).toBe(true);
  });

  it('stays out of view while no ticked model takes or is refused them and nothing is set', () => {
    expect(viewOf([], false)).toEqual({ shown: false, fields: false, notes: [] });
    expect(viewOf([other], false)).toEqual({ shown: false, fields: false, notes: [] });
  });

  it('says why the ChatGPT-subscription twin cannot take them, and posts nothing for it', () => {
    expect(viewOf([sub], false)).toEqual({ shown: true, fields: false, notes: [`${sub.label}: ${sub.refusal}`] });
  });

  it('keeps a value that is set in view and posted, and names the models that cannot take it', () => {
    const v = viewOf([other, sub], true);
    expect(v.shown && v.fields).toBe(true);
    expect(v.notes).toEqual([otherNote('SDXL'), `${sub.label}: ${sub.refusal}`]);
  });

  it('asks for the frames only for a GIF', () => {
    expect(framesShown('gif')).toBe(true);
    expect(framesShown('svg')).toBe(false);
    expect(framesShown('')).toBe(false);
  });
});
