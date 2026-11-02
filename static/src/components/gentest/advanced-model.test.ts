import { describe, expect, it } from 'vitest';
import { minStepsOf, note, shown, startValue, useOf, type ModelUse } from './advanced-model';

const sdxl = useOf({
  label: 'SDXL',
  sampling: 'steps cfg sampler scheduler',
  supportsSeed: 'true',
  defaultSteps: '25',
  defaultCfg: '7',
});
const wan = useOf({
  label: 'Wan 2.2',
  sampling: 'sampler scheduler',
  samplingLocked: 'true',
  supportsSeed: 'true',
  defaultSteps: '4',
  defaultCfg: '1',
});
const wan14 = useOf({
  label: 'Wan 2.2 14B',
  sampling: 'steps cfg sampler scheduler',
  supportsSeed: 'true',
  defaultSteps: '20',
  defaultCfg: '3.5',
  minSteps: '2',
});
const cloud: ModelUse = useOf({ label: 'Cloud', sampling: '', supportsSeed: 'false' });

describe('the detailed settings of the generation test form', () => {
  it('reads what a card says a model takes', () => {
    expect([...sdxl.takes].sort()).toEqual(['cfg', 'sampler', 'scheduler', 'seed', 'steps']);
    expect([...wan.takes].sort()).toEqual(['sampler', 'scheduler', 'seed']);
    expect(wan.locked).toBe(true);
    expect(cloud.takes.size).toBe(0);
  });

  it('shows a setting while a ticked model takes it, and every setting while none is ticked', () => {
    expect(shown([cloud], 'steps')).toBe(false);
    expect(shown([cloud, sdxl], 'steps')).toBe(true);
    expect(shown([wan], 'steps')).toBe(false);
    expect(shown([wan], 'sampler')).toBe(true);
    expect(shown([], 'cfg')).toBe(true);
  });

  it('starts a slider from the default of the first model that takes it', () => {
    expect(startValue([cloud, wan, sdxl], 'steps', 10)).toBe(25);
    expect(startValue([cloud], 'cfg', 5)).toBe(5);
  });

  it('starts the steps slider at the largest minimum of the chosen models that take the steps (a two-stage sampler needs 2)', () => {
    expect(minStepsOf([sdxl], 1)).toBe(1);
    expect(minStepsOf([], 1)).toBe(1);
    expect(minStepsOf([wan14], 1)).toBe(2);
    expect(minStepsOf([sdxl, wan14], 1)).toBe(2);
    expect(minStepsOf([cloud, sdxl], 1)).toBe(1);
  });

  it('names the models that do not take what was given, and says a speed mode owns the steps and the cfg', () => {
    expect(note([sdxl], ['steps', 'seed'])).toBe('');
    expect(note([cloud], ['steps', 'seed'])).toBe('Cloud: ステップ数・シード は受け取らないため、指定は使いません');
    expect(note([wan], ['steps', 'cfg', 'sampler'])).toBe(
      'Wan 2.2: ステップ数・CFG は速さのモードが決めるため、指定は使いません',
    );
    expect(note([sdxl, cloud], [])).toBe('');
  });
});
