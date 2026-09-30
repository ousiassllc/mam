import { execFileSync } from 'node:child_process';
import { existsSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, it } from 'vitest';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const dist = join(root, 'src', 'dist');

// embed.go embeds all of src/dist, so a build must leave nothing but its own outputs there.
it('a production build removes leftovers of earlier builds from the embedded dist', () => {
  execFileSync('node', ['build.mjs'], { cwd: root }); // dev build: writes app.js.map
  expect(existsSync(join(dist, 'app.js.map'))).toBe(true);
  writeFileSync(join(dist, 'old-output.js'), 'stale');

  execFileSync('node', ['build.mjs', '--minify'], { cwd: root });

  const files = readdirSync(dist).filter((n) => n !== '.gitkeep').sort();
  expect(files).toEqual(['app.css', 'app.js']);
}, 60_000);
