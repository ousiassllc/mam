import { describe, expect, it } from 'vitest';
import { adjust, read, WEIGHT_MAX, WEIGHT_MIN, type Change, type Direction } from './weight-model';

/** Selects `sel` in `text` (the first place it is found) and moves its weight. */
function press(text: string, sel: string, dir: Direction): Change | null {
  const at = text.indexOf(sel);
  expect(at, `"${sel}" is in the text`).toBeGreaterThanOrEqual(0);
  return adjust(text, at, at + sel.length, dir);
}

/** The text after pressing, and the part that stays selected. */
function pressed(text: string, sel: string, dir: Direction): [string, string] {
  const c = press(text, sel, dir);
  expect(c, `pressing on "${sel}" of "${text}" changes it`).not.toBeNull();
  return [c!.value, c!.value.slice(c!.start, c!.end)];
}

describe('raising and lowering a selected word', () => {
  it('wraps a plain word in a weight of 1.1 / 0.9', () => {
    expect(pressed('a cat on a sofa', 'cat', 1)).toEqual(['a (cat:1.1) on a sofa', '(cat:1.1)']);
    expect(pressed('a cat on a sofa', 'cat', -1)).toEqual(['a (cat:0.9) on a sofa', '(cat:0.9)']);
  });

  it('works on Japanese and on a selection over several lines', () => {
    expect(pressed('青い猫、赤い犬', '青い猫', 1)).toEqual(['(青い猫:1.1)、赤い犬', '(青い猫:1.1)']);
    expect(pressed('line one\nline two\nthree', 'one\nline two', 1)[0]).toBe('line (one\nline two:1.1)\nthree');
  });

  it('changes only the number of a word that already has one, whether the brackets are selected or only the word', () => {
    expect(pressed('a (cat:1.3) b', '(cat:1.3)', 1)[0]).toBe('a (cat:1.4) b');
    expect(pressed('a (cat:1.3) b', 'cat', 1)[0]).toBe('a (cat:1.4) b');
    expect(pressed('a (cat:1.3) b', 'cat', -1)[0]).toBe('a (cat:1.2) b');
  });

  it('keeps the selection on the whole changed part, so that the next press finds the same word', () => {
    let text = '猫が寝ている';
    let [s, e] = [0, 1];
    for (const want of ['(猫:1.1)', '(猫:1.2)', '(猫:1.3)']) {
      const c = adjust(text, s, e, 1)!;
      [text, s, e] = [c.value, c.start, c.end];
      expect(text.slice(s, e)).toBe(want);
    }
    expect(text).toBe('(猫:1.3)が寝ている');
  });

  it('takes the brackets off when the weight is back to 1.0', () => {
    expect(pressed('a (cat:1.1) b', 'cat', -1)).toEqual(['a cat b', 'cat']);
    expect(pressed('a (cat:0.9) b', '(cat:0.9)', 1)).toEqual(['a cat b', 'cat']);
    let text = 'cat';
    let [s, e] = [0, 3];
    for (const dir of [1, 1, -1, -1] as const) {
      const c = adjust(text, s, e, dir)!;
      [text, s, e] = [c.value, c.start, c.end];
    }
    expect(text).toBe('cat');
    expect([s, e]).toEqual([0, 3]);
  });

  it('leaves out the white space a double click takes along', () => {
    expect(pressed('a cat on', 'cat ', 1)).toEqual(['a (cat:1.1) on', '(cat:1.1)']);
    expect(pressed('a cat on', ' cat', 1)[0]).toBe('a (cat:1.1) on');
  });
});

describe('the brackets without a number, as ComfyUI reads them', () => {
  it('reads "(word)" as 1.1 and "((word))" as 1.2 (1.21)', () => {
    expect(read('(cat)', 0, 5)?.weight).toBe(1.1);
    expect(read('((cat))', 2, 5)?.weight).toBe(1.2);
    expect(read('((cat))', 0, 7)?.weight).toBe(1.2);
  });

  it('writes "(word)" as a number when it is raised, and takes it off when it is lowered', () => {
    expect(pressed('a (cat) b', 'cat', 1)[0]).toBe('a (cat:1.2) b');
    expect(pressed('a (cat) b', '(cat)', -1)[0]).toBe('a cat b');
  });

  it('replaces the nesting by one number', () => {
    expect(pressed('a ((cat)) b', 'cat', 1)).toEqual(['a (cat:1.3) b', '(cat:1.3)']);
    expect(pressed('a ((cat)) b', '((cat))', -1)[0]).toBe('a (cat:1.1) b');
  });

  it('does not read [word] as a weight', () => {
    expect(read('[cat]', 1, 4)?.weight).toBe(1);
    expect(pressed('[cat]', 'cat', 1)[0]).toBe('[(cat:1.1)]');
  });

  it('does not read an escaped bracket as a weight', () => {
    expect(read('\\(cat\\)', 2, 5)?.weight).toBe(1);
    expect(pressed('\\(cat\\)', 'cat', 1)[0]).toBe('\\((cat:1.1)\\)');
  });
});

describe('a word inside another weight', () => {
  it('starts from what the outer weight gives it, and the number it names replaces the outer one', () => {
    expect(read('(red cat:1.3)', 5, 8)?.weight).toBe(1.3);
    expect(pressed('(red cat:1.3)', 'cat', 1)[0]).toBe('(red (cat:1.4):1.3)');
    expect(pressed('(red cat:1.3)', 'cat', -1)[0]).toBe('(red (cat:1.2):1.3)');
  });

  it('drops the inner brackets when its weight comes back to the outer one', () => {
    expect(pressed('(red (cat:1.4):1.3)', '(cat:1.4)', -1)[0]).toBe('(red cat:1.3)');
  });

  it('counts a plain bracket around it as 1.1', () => {
    expect(read('(red cat)', 5, 8)?.weight).toBe(1.1);
    expect(pressed('(red cat)', 'cat', 1)[0]).toBe('(red (cat:1.2))');
  });

  it('does not touch the words beside it', () => {
    expect(pressed('(dog:1.2) cat', 'cat', 1)[0]).toBe('(dog:1.2) (cat:1.1)');
  });
});

describe('the range', () => {
  it(`stops at ${WEIGHT_MAX} and ${WEIGHT_MIN}`, () => {
    expect(press('(cat:1.5)', 'cat', 1)).toBeNull();
    expect(pressed('(cat:1.4)', 'cat', 1)[0]).toBe('(cat:1.5)');
    expect(press('(cat:0.5)', 'cat', -1)).toBeNull();
    expect(pressed('(cat:0.6)', 'cat', -1)[0]).toBe('(cat:0.5)');
  });

  it('says which way a word can still move', () => {
    expect(read('(cat:1.5)', 1, 4)).toEqual({ weight: 1.5, canRaise: false, canLower: true });
    expect(read('(cat:0.5)', 1, 4)).toEqual({ weight: 0.5, canRaise: true, canLower: false });
    expect(read('cat', 0, 3)).toEqual({ weight: 1, canRaise: true, canLower: true });
  });

  it('lets a weight typed outside the range come back to its edge, and no further out', () => {
    expect(press('(cat:2)', 'cat', 1)).toBeNull();
    expect(pressed('(cat:2)', 'cat', -1)[0]).toBe('(cat:1.5)');
    expect(pressed('(cat:0.2)', 'cat', 1)[0]).toBe('(cat:0.5)');
  });

  it('does not add up the floating point error of 0.1 steps', () => {
    let text = 'cat';
    let [s, e] = [0, 3];
    for (let i = 0; i < 5; i++) {
      const c = adjust(text, s, e, 1)!;
      [text, s, e] = [c.value, c.start, c.end];
    }
    expect(text).toBe('(cat:1.5)');
  });
});

describe('a selection that cannot be changed', () => {
  it('is nothing selected, or only white space', () => {
    expect(adjust('cat', 1, 1, 1)).toBeNull();
    expect(adjust('a  b', 1, 3, 1)).toBeNull();
    expect(read('cat', 2, 2)).toBeNull();
  });

  it('is a selection that holds one bracket of a pair and not the other', () => {
    expect(press('(red cat)', 'red cat)', 1)).toBeNull();
    expect(press('(red cat)', '(red', 1)).toBeNull();
    expect(read('(red cat)', 1, 9)).toBeNull();
  });

  it('is an empty pair of brackets', () => {
    expect(press('a () b', '()', 1)).toBeNull();
  });

  it('takes a selection made backwards (end before start)', () => {
    expect(adjust('a cat b', 5, 2, 1)?.value).toBe('a (cat:1.1) b');
  });
});
