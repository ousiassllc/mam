// The fonts of the UI guide (T1): Sora for Latin text and numbers, Noto Sans JP for Japanese, JetBrains Mono for code and aligned
// numbers, all SIL OFL. They come from the @fontsource-variable packages (devDependencies), whose own CSS is one @font-face per
// unicode-range slice; this writes dist/fonts.css (the same rules under the family names of the guide, without the "Variable" suffix of the
// packages, and with the files under ./fonts/) and copies the slices it names into dist/fonts/. A page downloads only the slices of the
// characters it shows. The Latin of Noto Sans JP is left out (Sora has it) and so are the scripts mam does not write (Cyrillic, Greek,
// Vietnamese).
import { copyFileSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { dirname, join } from 'node:path';

const require = createRequire(import.meta.url);

// package, the family the guide names, and which of its slices (the part of the comment before -wght-normal) to keep.
const families = [
  { pkg: '@fontsource-variable/sora', family: 'Sora', keep: (slice) => slice === 'latin' || slice === 'latin-ext' },
  {
    pkg: '@fontsource-variable/jetbrains-mono',
    family: 'JetBrains Mono',
    keep: (slice) => slice === 'latin' || slice === 'latin-ext',
  },
  { pkg: '@fontsource-variable/noto-sans-jp', family: 'Noto Sans JP', keep: (slice) => /^\d+$/.test(slice) },
];

/** The @font-face rules of one package's wght.css (its font is `name`, as its files are named), kept by `keep`, with the file of each slice. */
export function fontFaces(css, name, family, keep) {
  const faces = [];
  for (const block of css.split('\n\n')) {
    const file = /url\(\.\/files\/([^)]+\.woff2)\)/.exec(block)?.[1];
    const slice = new RegExp(`/\\* ${name}-\\[?([a-z0-9-]+?)\\]?-wght-normal \\*/`).exec(block)?.[1];
    if (!file || slice === undefined || !keep(slice)) continue;
    const rule = block
      .slice(block.indexOf('@font-face'))
      .replace(/font-family: '[^']+';/, `font-family: '${family}';`)
      .replace(`url(./files/${file})`, `url(./fonts/${file})`);
    faces.push({ file, rule });
  }
  return faces;
}

/** Writes outdir/fonts.css and outdir/fonts/*.woff2. */
export function buildFonts(outdir) {
  mkdirSync(join(outdir, 'fonts'), { recursive: true });
  const rules = [];
  for (const { pkg, family, keep } of families) {
    const dir = dirname(require.resolve(`${pkg}/package.json`));
    const faces = fontFaces(readFileSync(join(dir, 'wght.css'), 'utf8'), pkg.split('/')[1], family, keep);
    if (faces.length === 0) throw new Error(`${pkg}: no font slice to ship`);
    for (const { file, rule } of faces) {
      copyFileSync(join(dir, 'files', file), join(outdir, 'fonts', file));
      rules.push(rule);
    }
  }
  writeFileSync(join(outdir, 'fonts.css'), `${rules.join('\n')}\n`);
}
