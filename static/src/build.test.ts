import { execFileSync, spawn, spawnSync } from 'node:child_process';
import { existsSync, mkdtempSync, readdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
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

  const files = readdirSync(dist)
    .filter((n) => n !== '.gitkeep')
    .sort();
  expect(files).toEqual(['app.css', 'app.js']);
}, 60_000);

// A fake `npx` first on PATH stands in for Tailwind: it exits with the given code after a moment.
function withFakeTailwind(code: number, seconds = 0.3) {
  const bin = mkdtempSync(join(tmpdir(), 'fake-npx-'));
  writeFileSync(join(bin, 'npx'), `#!/bin/sh\nsleep ${seconds}\nexit ${code}\n`, { mode: 0o755 });
  return { ...process.env, PATH: `${bin}:${process.env.PATH}` };
}

function run(args: string[], env: NodeJS.ProcessEnv) {
  return spawnSync('node', ['build.mjs', ...args], { cwd: root, env, encoding: 'utf8', timeout: 30_000 });
}

it.skipIf(process.platform === 'win32')(
  'watch mode fails when Tailwind dies instead of serving a stale CSS',
  () => {
    const res = run(['--watch'], withFakeTailwind(3));

    expect(res.status).toBe(1);
    expect(res.stderr).toContain('tailwind exited with 3');
  },
  40_000,
);

it.skipIf(process.platform === 'win32')(
  'a one-off build fails when Tailwind fails, after esbuild finished',
  () => {
    const res = run([], withFakeTailwind(2));

    expect(res.status).toBe(1);
    expect(res.stderr).toContain('tailwind exited with 2');
  },
  40_000,
);

it.skipIf(process.platform === 'win32')(
  'watch mode exits cleanly on SIGTERM',
  async () => {
    const child = spawn('node', ['build.mjs', '--watch'], {
      cwd: root,
      env: withFakeTailwind(0, 30),
      stdio: ['ignore', 'pipe', 'ignore'],
    });
    const exited = new Promise<number | null>((resolve) => child.on('exit', (code) => resolve(code)));
    // SIGTERM is handled only once the first build is done; wait for that instead of a fixed delay.
    const ready = new Promise<void>((resolve) => {
      let out = '';
      child.stdout.on('data', (chunk) => {
        out += chunk;
        if (out.includes('[build] watching for changes')) resolve();
      });
    });
    await Promise.race([ready, exited]);

    child.kill('SIGTERM');

    expect(await exited).toBe(0);
  },
  20_000,
);
