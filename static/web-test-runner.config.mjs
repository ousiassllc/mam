// Tests of the Lit components (*.wtest.ts) run in a real Chrome: `npm run test:components`.
// The pure helpers of components/lib are tested by vitest (*.test.ts).
import { execFile } from 'node:child_process';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { promisify } from 'node:util';
import { esbuildPlugin } from '@web/dev-server-esbuild';
import { chromeLauncher } from '@web/test-runner-chrome';

// The answers of the Review Workspace's forms (test/review-workspace.wtest.ts): the dev server only
// serves files on GET, so these requests get what the handlers answered, from testdata.
const reviewAnswers = {
  'POST /reviews/r1/comments': ['review-comment-added.html', 201],
  'DELETE /reviews/r1/comments/c1': ['review-comment-deleted.html', 200],
  'PUT /reviews/r1/decision': ['review-decision-saved.html', 200],
};

async function reviewAnswerMiddleware(ctx, next) {
  const answer = reviewAnswers[`${ctx.method} ${ctx.path}`];
  if (!answer) return next();
  const [file, status] = answer;
  ctx.status = status;
  ctx.type = 'text/html';
  ctx.body = await readFile(new URL(`./src/components/testdata/${file}`, import.meta.url));
}

// The answer of "restore a version" (test: focus-restore): the handler sends `HX-Trigger: branch-reload`.
async function hxTriggerMiddleware(ctx, next) {
  await next();
  if (ctx.path.endsWith('/focus-trigger-after.html')) ctx.set('HX-Trigger', 'branch-reload');
}

// The answers of the modal dialog (test: modal): a saved form closes it through `HX-Trigger-After-Swap: modal-close`, a
// refused one is a 422 with the form again.
async function modalAnswerMiddleware(ctx, next) {
  // The answer of a POST (the dev server only serves files on GET): a token issued, with a row to swap out of band.
  if (ctx.method === 'POST' && ctx.path.endsWith('/modal-issued.html')) {
    ctx.type = 'text/html';
    ctx.body = await readFile(new URL('./src/components/testdata/modal-issued.html', import.meta.url));
    return;
  }
  await next();
  if (ctx.path.endsWith('/modal-saved.html')) ctx.set('HX-Trigger-After-Swap', 'modal-close');
  if (ctx.path.endsWith('/modal-invalid.html')) ctx.status = 422;
}

// The answers of the model manager (test: scroll): the install of a model is a POST and the list polls with a query,
// which the dev server does not serve; the files are what the handlers render (backend/internal/web/scroll_fixture_test.go).
const modelManagerAnswers = {
  'POST /model-manager/models/model-5/install': ['scroll-mm-install-answer.html', 202],
  'POST /model-manager/models/model-8/install': ['scroll-mm-consent-answer.html', 202],
  'GET /model-manager/models/model-8/consent': ['scroll-mm-consent.html', 200],
  'GET /model-manager/dialog': ['', 200], // what "閉じる" swaps in: nothing (handlers.CloseDialog)
  'GET /model-manager/models': ['scroll-mm-poll-done.html', 200],
};

async function modelManagerAnswerMiddleware(ctx, next) {
  const answer = modelManagerAnswers[`${ctx.method} ${ctx.path}`];
  if (!answer) return next();
  const [file, status] = answer;
  ctx.status = status;
  ctx.type = 'text/html';
  ctx.body = file ? await readFile(new URL(`./src/components/testdata/${file}`, import.meta.url)) : '';
}

// The real Tailwind CSS of the app (src/css/app.css, which scans the Templ templates and the components), for the tests
// that measure a layout (components/layout). It is built once, by the installed @tailwindcss/cli, while the browser
// starts, and served as /__tailwind.css.
async function tailwindCssMiddleware(ctx, next) {
  if (ctx.path !== '/__tailwind.css') return next();
  ctx.type = 'text/css';
  ctx.body = await tailwindCss;
}

async function buildTailwindCss() {
  const root = dirname(fileURLToPath(import.meta.url));
  const pkgPath = createRequire(import.meta.url).resolve('@tailwindcss/cli/package.json');
  const { bin } = JSON.parse(await readFile(pkgPath, 'utf8'));
  const entry = join(dirname(pkgPath), typeof bin === 'string' ? bin : bin.tailwindcss);
  const dir = await mkdtemp(join(tmpdir(), 'mam-tailwind-'));
  try {
    const out = join(dir, 'app.css');
    await promisify(execFile)(
      process.execPath,
      [entry, '--input', join(root, 'src', 'css', 'app.css'), '--output', out],
      {
        cwd: root,
      },
    );
    return await readFile(out, 'utf8');
  } finally {
    await rm(dir, { recursive: true, force: true });
  }
}

const tailwindCss = buildTailwindCss();
tailwindCss.catch(() => {}); // the failure is reported where it is awaited (the request of a test)

export default {
  files: 'src/components/**/*.wtest.ts',
  // One page at a time: when two pages load a clip together (video-player and review-workspace wtests
  // run side by side) the <video> metadata of both never loads and the tests time out.
  concurrency: 1,
  // Mocha's default of 2 s cut every test off before the waits inside it (`waitUntil(..., { timeout: 5000 })` for
  // a video that loads, 2 s for an htmx answer) could end: on a loaded host a first request or a video takes
  // longer than that, and the test failed with a bare "Timeout of 2000ms exceeded" instead of the message of
  // the wait. Mocha now only bounds a test that hangs; each wait bounds itself and says what it waited for.
  testFramework: { config: { timeout: 15000 } },
  nodeResolve: true,
  middleware: [
    reviewAnswerMiddleware,
    hxTriggerMiddleware,
    modalAnswerMiddleware,
    modelManagerAnswerMiddleware,
    tailwindCssMiddleware,
  ],
  plugins: [esbuildPlugin({ ts: true, tsconfig: './tsconfig.json' })],
  browsers: [
    chromeLauncher({
      launchOptions: {
        args: ['--no-sandbox', '--autoplay-policy=no-user-gesture-required'],
      },
    }),
  ],
};
