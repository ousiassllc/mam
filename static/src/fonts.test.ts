import { expect, it } from 'vitest';
// @ts-expect-error fonts.mjs is the build script's module, plain JavaScript without types
import { fontFaces } from '../fonts.mjs';

const css = `/* sora-latin-ext-wght-normal */
@font-face {
  font-family: 'Sora Variable';
  src: url(./files/sora-latin-ext-wght-normal.woff2) format('woff2-variations');
  unicode-range: U+0100-02BA;
}

/* sora-cyrillic-wght-normal */
@font-face {
  font-family: 'Sora Variable';
  src: url(./files/sora-cyrillic-wght-normal.woff2) format('woff2-variations');
}

/* sora-[12]-wght-normal */
@font-face {
  font-family: 'Sora Variable';
  src: url(./files/sora-12-wght-normal.woff2) format('woff2-variations');
}
`;

it('keeps the slices that are asked for, under the family of the guide and the folder of dist', () => {
  const faces = fontFaces(css, 'sora', 'Sora', (slice: string) => slice === 'latin-ext');

  expect(faces).toHaveLength(1);
  expect(faces[0].file).toBe('sora-latin-ext-wght-normal.woff2');
  expect(faces[0].rule).toContain("font-family: 'Sora';");
  expect(faces[0].rule).toContain('url(./fonts/sora-latin-ext-wght-normal.woff2)');
  expect(faces[0].rule).toContain('unicode-range: U+0100-02BA;');
  expect(faces[0].rule).not.toContain('Variable');
});

it('names a numbered slice by its number', () => {
  const faces = fontFaces(css, 'sora', 'Sora', (slice: string) => /^\d+$/.test(slice));

  expect(faces.map((f: { file: string }) => f.file)).toEqual(['sora-12-wght-normal.woff2']);
});
