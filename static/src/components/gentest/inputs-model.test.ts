import { describe, expect, it } from 'vitest';
import { dropSide, isUploadRef, moveItem, noRefNotice, takeFiles, unionRoles, uploadRef } from './inputs-model';

const png = (name: string, size = 10) => new File([new Uint8Array(size)], name, { type: 'image/png' });

describe('the rules of the input images of the generation test', () => {
  it('lists the roles that at least one chosen model takes, in the order of the select', () => {
    expect(unionRoles([['style', 'reference'], ['reference']])).toEqual(['reference', 'style']);
    expect(unionRoles([['edit'], ['face', 'reference']])).toEqual(['reference', 'face', 'edit']);
    expect(unionRoles([[], []])).toEqual([]);
    expect(unionRoles([])).toEqual([]);
  });

  it('names an image of the browser by its number among the files', () => {
    expect(uploadRef(2)).toBe('upload::#2');
    expect(isUploadRef(uploadRef(1))).toBe(true);
    expect(isUploadRef('history::abc')).toBe(false);
  });

  it('moves an item and keeps the index inside the list', () => {
    expect(moveItem(['a', 'b', 'c'], 0, 1)).toEqual(['b', 'a', 'c']);
    expect(moveItem(['a', 'b', 'c'], 2, 0)).toEqual(['c', 'a', 'b']);
    expect(moveItem(['a', 'b', 'c'], 0, -1)).toEqual(['a', 'b', 'c']);
    expect(moveItem(['a', 'b', 'c'], 1, 9)).toEqual(['a', 'c', 'b']);
    expect(moveItem(['a'], 3, 0)).toEqual(['a']);
  });

  it('drops before the thumbnail on its left half and after it on its right half', () => {
    const rect = { left: 100, width: 80 };
    expect(dropSide(rect, 120)).toBe('before');
    expect(dropSide(rect, 150)).toBe('after');
  });

  it('says which chosen models do not use the images, and only while there are images', () => {
    const models = [
      { label: 'A', accepts: true },
      { label: 'B', accepts: false },
      { label: 'C', accepts: false },
    ];
    expect(noRefNotice(2, models)).toBe('B、C は入力画像を使いません');
    expect(noRefNotice(0, models)).toBe('');
    expect(noRefNotice(2, [models[0]!])).toBe('');
  });

  it('takes the images that fit and says in one line what it left out', () => {
    const ok = takeFiles([png('a.png'), png('b.png')], 5, 100, 10);
    expect(ok.files).toHaveLength(2);
    expect(ok.error).toBe('');

    const full = takeFiles([png('a.png'), png('b.png'), png('c.png')], 2, 100, 10);
    expect(full.files.map((f) => f.name)).toEqual(['a.png', 'b.png']);
    expect(full.error).toBe('最大 10 枚です');

    const big = takeFiles([png('a.png', 3 << 20), png('b.png')], 5, 2 << 20, 10);
    expect(big.files.map((f) => f.name)).toEqual(['b.png']);
    expect(big.error).toContain('大きすぎる');

    const text = takeFiles([new File(['x'], 'a.txt', { type: 'text/plain' }), png('b.png')], 5, 100, 10);
    expect(text.files.map((f) => f.name)).toEqual(['b.png']);
    expect(text.error).toContain('PNG・JPEG・WebP');

    const empty = takeFiles([png('empty.png', 0), png('b.png')], 5, 100, 10);
    expect(empty.files.map((f) => f.name)).toEqual(['b.png']);
    expect(empty.error).toBe('空の画像ファイルは除きました');
    expect(takeFiles([png('empty.png', 0)], 5, 0, 10).files, 'also when there is no size limit').toEqual([]);

    expect(takeFiles([png('a.png')], 0, 100, 10).files).toEqual([]);
  });
});
