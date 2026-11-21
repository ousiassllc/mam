import { readFileSync } from 'node:fs';
import { describe, expect, it } from 'vitest';
import { PATHS } from './icon';

// The Lit widgets draw the icons of atoms.Icon (Iconoir v7.12.2): every path here is a path of one of its templates.
const ATOMS = ['icon.templ', 'icon_player.templ'].map((f) =>
  readFileSync(new URL(`../../../../backend/internal/web/atoms/${f}`, import.meta.url), 'utf8'),
);

describe('icon paths', () => {
  for (const [name, paths] of Object.entries(PATHS)) {
    it(`${name} is the path of atoms.Icon`, () => {
      expect(paths.length).toBeGreaterThan(0);
      for (const d of paths) {
        expect(
          ATOMS.some((src) => src.includes(`<path d="${d}"`)),
          d,
        ).toBe(true);
      }
    });
  }
});
