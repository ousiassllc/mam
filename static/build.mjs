// Bundles the front end into src/dist (embedded into the Go binary by embed.go).
//   node build.mjs            development build (sourcemaps)
//   node build.mjs --minify   production build
//   node build.mjs --watch    rebuild on change (make dev)
import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import * as esbuild from 'esbuild';

const root = dirname(fileURLToPath(import.meta.url));
const args = new Set(process.argv.slice(2));
const minify = args.has('--minify');
const watch = args.has('--watch');
const outdir = join(root, 'src', 'dist');

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
  '--input', join(root, 'src', 'css', 'app.css'),
  '--output', join(outdir, 'app.css'),
  ...(minify ? ['--minify'] : []),
  ...(watch ? ['--watch'] : []),
];

const tailwind = spawn('npx', ['--no-install', ...tailwindArgs], { cwd: root, stdio: 'inherit' });
const tailwindDone = new Promise((resolve, reject) => {
  tailwind.on('exit', (code) => (code === 0 || watch ? resolve() : reject(new Error(`tailwind exited with ${code}`))));
  tailwind.on('error', reject);
});

if (watch) {
  const ctx = await esbuild.context(jsOptions);
  await ctx.watch();
  const stop = async () => {
    tailwind.kill();
    await ctx.dispose();
    process.exit(0);
  };
  process.on('SIGINT', stop);
  process.on('SIGTERM', stop);
  await tailwindDone;
} else {
  await esbuild.build(jsOptions);
  await tailwindDone;
}
