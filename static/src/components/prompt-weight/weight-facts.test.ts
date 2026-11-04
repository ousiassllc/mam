import { describe, expect, it } from 'vitest';
import { cautionOf, factsOf, lineOf, modelWeightOf, NO_FACTS, type ModelWeight, notesOf } from './weight-facts';

const m = (label: string, level: ModelWeight['level'], title = `${label} says ${level}`): ModelWeight => ({
  label,
  level,
  title,
});

describe('what the chosen models say of prompt weights', () => {
  it('says nothing, and allows the buttons, while no model is chosen', () => {
    expect(factsOf([])).toEqual(NO_FACTS);
    expect(cautionOf(NO_FACTS)).toBe('');
    expect(notesOf(NO_FACTS)).toEqual([]);
  });

  it('is one group for one chosen model, and nothing is said of a model that reads the weights', () => {
    const facts = factsOf([m('SDXL', 'supported', '効きます')]);
    expect(facts).toEqual({ usable: true, groups: [{ level: 'supported', title: '効きます', labels: ['SDXL'] }] });
    expect(lineOf(facts.groups[0]!)).toBe('SDXL: 効きます');
    expect(cautionOf(facts)).toBe('');
    expect(notesOf(facts)).toEqual([]);
  });

  it('turns the buttons off, with the reason, when every chosen model does not read them', () => {
    const facts = factsOf([m('Qwen-Image', 'unsupported'), m('FLUX.2', 'unsupported')]);
    expect(facts.usable).toBe(false);
    expect(facts.groups).toHaveLength(1);
    expect(facts.groups[0]?.labels).toEqual(['Qwen-Image', 'FLUX.2']);
    expect(notesOf(facts)).toEqual([cautionOf(facts)]);
  });

  it('keeps the buttons on while one chosen model reads the weights, and puts the models in the order of what works first', () => {
    const facts = factsOf([m('Mine', 'unknown'), m('Qwen', 'unsupported'), m('Flux', 'weak'), m('SDXL', 'supported')]);
    expect(facts.usable).toBe(true);
    expect(facts.groups.map((g) => g.level)).toEqual(['supported', 'weak', 'unsupported', 'unknown']);
  });

  it('says the models that read the weights the same way once, and a name once', () => {
    const facts = factsOf([m('A', 'unsupported', 'no'), m('B', 'unsupported', 'no'), m('A', 'unsupported', 'no')]);
    expect(facts.groups).toEqual([{ level: 'unsupported', title: 'no', labels: ['A', 'B'] }]);
    expect(lineOf(facts.groups[0]!)).toBe('A・B: no');
  });

  it('cautions for the models that do not read the weights only, by name, with what to do instead', () => {
    const caution = cautionOf(factsOf([m('SDXL', 'supported'), m('Qwen-Image', 'unsupported'), m('Mine', 'unknown')]));
    expect(caution).toContain('Qwen-Image');
    expect(caution).not.toContain('SDXL');
    expect(caution).not.toContain('Mine');
    expect(caution).toContain('効きません');
    expect(caution).toContain('文で言い換えてください');
  });

  it('puts the line of the models that read poorly in the notes of the bar, and says nothing of a model that is not known', () => {
    expect(notesOf(factsOf([m('Flux', 'weak')]))).toEqual(['Flux: Flux says weak']);
    expect(notesOf(factsOf([m('Mine', 'unknown')]))).toEqual([]);
  });

  it('reads the card: the short words and the name of the card; a card without the words gets the standard ones', () => {
    expect(modelWeightOf({ label: 'A', promptWeight: 'weak', promptWeightTitle: 't' })).toEqual({
      label: 'A',
      level: 'weak',
      title: 't',
    });
    expect(modelWeightOf({ label: 'A', promptWeight: 'supported' }).title).toBe('効きます');
    expect(modelWeightOf({ label: 'A', promptWeight: 'weak' }).title).toBe('効きが弱いかも');
    expect(modelWeightOf({ label: 'A', promptWeight: 'unsupported' }).title).toBe(
      '効きません（代わりに文で強調してください）',
    );
    expect(modelWeightOf({ model: 'id' })).toEqual({ label: 'id', level: 'unknown', title: '未確認' });
    expect(modelWeightOf({ promptWeight: 'strong' }).level).toBe('unknown');
  });
});
