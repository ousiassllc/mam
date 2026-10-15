// Bundles the front end into src/dist (embedded into the Go binary by embed.go).
//   node build.mjs            development build (sourcemaps)
//   node build.mjs --minify   production build
//   node build.mjs --watch    rebuild on change (make dev)
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { mkdirSync, readdirSync, rmSync } from 'node:fs';
import { dirname, join } from 'node:path';
import * as esbuild from 'esbuild';

const root = dirname(fileURLToPath(import.meta.url));
const args = new Set(process.argv.slice(2));
const minify = args.has('--minify');
const watch = args.has('--watch');
const outdir = join(root, 'src', 'dist');

// embed.go embeds the whole of src/dist, so anything a previous build left there (a dev build's
// app.js.map, renamed outputs) would ship in the binary and be served. Start from an empty dir;
// .gitkeep stays so a clean checkout still compiles.
mkdirSync(outdir, { recursive: true });
for (const name of readdirSync(outdir)) {
  if (name !== '.gitkeep') rmSync(join(outdir, name), { recursive: true, force: true });
}

const jsOptions = {
  entryPoints: [join(root, 'src', 'main.ts')],
  outfile: join(outdir, 'app.js'),
  bundle: true,
  format: 'esm',
  target: 'es2022',
  minify,
  sourcemap: minify ? false : 'linked',
  logLevel: 'info',
};

const tailwindArgs = [
  'tailwindcss',
  '--input',
  join(root, 'src', 'css', 'app.css'),
  '--output',
  join(outdir, 'app.css'),
  ...(minify ? ['--minify'] : []),
  ...(watch ? ['--watch'] : []),
];

// The Tailwind process is part of the build: if it dies (crash, killed, failed build) the build
// fails, in watch mode too, where an unnoticed dead Tailwind would leave the CSS stale while esbuild
// keeps rebuilding (make dev runs this under `concurrently -k`, so exiting non-zero stops the rest).
const tailwind = spawn('npx', ['--no-install', ...tailwindArgs], { cwd: root, stdio: 'inherit' });
let stopping = false;
const tailwindDone = new Promise((resolve, reject) => {
  tailwind.on('exit', (code, signal) => {
    if (code === 0 || stopping) resolve();
    else reject(new Error(`tailwind exited with ${signal ?? code}`));
  });
  tailwind.on('error', reject);
});

let ctx;
try {
  if (watch) {
    ctx = await esbuild.context(jsOptions);
    await ctx.watch();
    const stop = async () => {
      stopping = true;
      tailwind.kill();
      await ctx.dispose();
      process.exit(0);
    };
    process.on('SIGINT', stop);
    process.on('SIGTERM', stop);
    await tailwindDone;
    // stop() is shutting down (it calls process.exit); otherwise Tailwind's watch mode, which never
    // ends on its own, exited with 0, and that is a failure too.
    if (stopping) await new Promise(() => {});
    throw new Error('tailwind stopped');
  }
  // Wait for both, so a failing esbuild does not abandon a running Tailwind (and vice versa).
  const results = await Promise.allSettled([esbuild.build(jsOptions), tailwindDone]);
  const failed = results.find((r) => r.status === 'rejected');
  if (failed) throw failed.reason;
} catch (err) {
  console.error(`build failed: ${err.message}`);
  stopping = true;
  tailwind.kill();
  await ctx?.dispose();
  process.exit(1);
}
