import { describe, expect, it } from 'vitest';
import { factsOf, modelWeightOf, NO_FACTS, type ModelWeight } from './weight-facts';

const m = (label: string, level: ModelWeight['level'], text = `${label} says ${level}`): ModelWeight => ({
  label,
  level,
  text,
});

describe('what the chosen models say of prompt weights', () => {
  it('says nothing, and allows the buttons, while no model is chosen', () => {
    expect(factsOf([])).toEqual(NO_FACTS);
  });

  it('allows the buttons without a note for a model that reads them', () => {
    expect(factsOf([m('SDXL', 'supported')])).toEqual({ usable: true, notes: [] });
  });

  it('turns the buttons off, with the reason, when every chosen model does not read them', () => {
    expect(factsOf([m('Qwen', 'unsupported')])).toEqual({ usable: false, notes: ['Qwen says unsupported'] });
    expect(factsOf([m('Qwen', 'unsupported'), m('FLUX.2', 'unsupported')]).usable).toBe(false);
  });

  it('keeps the buttons on, with a note, for a weak or unchecked model', () => {
    expect(factsOf([m('Flux', 'weak')])).toEqual({ usable: true, notes: ['Flux says weak'] });
    expect(factsOf([m('Mine', 'unknown')])).toEqual({ usable: true, notes: ['Mine says unknown'] });
  });

  it('names the models when there are several, and keeps the buttons on while one reads the weights', () => {
    const facts = factsOf([m('SDXL', 'supported'), m('Qwen', 'unsupported')]);
    expect(facts.usable).toBe(true);
    expect(facts.notes).toEqual(['Qwen: Qwen says unsupported']);
  });

  it('reads the card: an unknown or missing level is unknown', () => {
    expect(modelWeightOf({ label: 'A', promptWeight: 'weak', promptWeightText: 't' })).toEqual({
      label: 'A',
      level: 'weak',
      text: 't',
    });
    expect(modelWeightOf({ model: 'id' })).toEqual({ label: 'id', level: 'unknown', text: '' });
    expect(modelWeightOf({ promptWeight: 'strong' }).level).toBe('unknown');
  });
});
