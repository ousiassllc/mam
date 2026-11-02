import { describe, expect, it } from 'vitest';
import { changed, changedItems, line, resetLabel, type Item } from './refine-model';

const item = (over: Partial<Item>): Item => ({
  key: 'steps',
  label: 'ステップ数',
  empty: '既定',
  initial: '20',
  current: '20',
  ...over,
});

describe('the marks of the こだわり tier', () => {
  it('an item is changed only while it differs from where the tier began', () => {
    expect(changed(item({}))).toBe(false);
    expect(changed(item({ current: '30' }))).toBe(true);
    expect(changed(item({ current: '30' }))).toBe(true);
    expect(changed(item({ initial: '', current: '' }))).toBe(false);
  });

  it('lists the changed ones in the order of the form', () => {
    const items = [item({ key: 'a', current: '1' }), item({ key: 'b' }), item({ key: 'c', current: '2' })];
    expect(changedItems(items).map((i) => i.key)).toEqual(['a', 'c']);
  });

  it('words a value that is none as its own word, and says what it was', () => {
    expect(line(item({ current: '30' }))).toBe('ステップ数: 30（元は 20）');
    expect(line(item({ initial: '', current: '30' }))).toBe('ステップ数: 30（元は 既定）');
    expect(line(item({ initial: '20', current: '' }))).toBe('ステップ数: 既定（元は 20）');
    expect(resetLabel(item({}))).toBe('ステップ数を元に戻す');
  });
});
