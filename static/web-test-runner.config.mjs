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
  'GET /model-manager/models/model-8/consent': ['scroll-mm-consent.html', 200],
  'GET /model-manager/dialog': ['', 200], // what "閉じる" swaps in: nothing (handlers.CloseDialog)
  'GET /model-manager/models': ['scroll-mm-poll-done.html', 200],
  // The engine card (test: poll/model-manager-refresh): its poll and its refresh button get the card of the finished install, and the list out of band.
  'GET /model-manager/engine': ['mm-engine-done.html', 200],
};

// A license that is not the one of the catalog is refused (422) into the dialog again, as handlers.InstallModel does
// (the test sends the stale version "0": scroll.wtest.ts).
async function readBody(req) {
  let body = '';
  for await (const chunk of req) body += chunk;
  return body;
}

async function modelManagerAnswerMiddleware(ctx, next) {
  if (ctx.method === 'POST' && ctx.path === '/model-manager/models/model-8/install') {
    const form = new URLSearchParams(await readBody(ctx.req));
    if (form.get('license_version') === '0') {
      ctx.status = 422;
      ctx.type = 'text/html';
      ctx.set('HX-Retarget', '#mm-dialog');
      ctx.set('HX-Reswap', 'innerHTML');
      ctx.body = await readFile(new URL('./src/components/testdata/scroll-mm-consent-refused.html', import.meta.url));
      return;
    }
    ctx.status = 202;
    ctx.type = 'text/html';
    ctx.body = await readFile(new URL('./src/components/testdata/scroll-mm-consent-answer.html', import.meta.url));
    return;
  }
  const answer = modelManagerAnswers[`${ctx.method} ${ctx.path}`];
  if (!answer) return next();
  const [file, status] = answer;
  ctx.status = status;
  ctx.type = 'text/html';
  ctx.body = file ? await readFile(new URL(`./src/components/testdata/${file}`, import.meta.url)) : '';
}

// The answers of the generation test (test: scroll/gentest-live): a running batch asks for itself every 2 s, which the dev
// server does not serve; the files are what the handlers render (backend/internal/web/gentest_live_fixture_test.go).
const gentestAnswers = {
  'GET /generation-test/batches/b1': ['gentest-batch-progress.html', 200],
  'GET /generation-test/history': ['gentest-history-live-done.html', 200], // the part of the history ("?live=1"): the last result finished
};

// The answers of a tile that is opened in the modal (test: gentest/result-modal, #1004): the detail of any result (GET, into the body of the dialog), the
// favorite button (POST: the button and the heart of the tile out of band) and a delete (DELETE: the tile goes out of band and the modal closes).
const gentestResultAnswers = [
  ['GET', /^\/generation-test\/results\/[^/]+$/, 'gentest-detail-modal.html', {}],
  ['POST', /^\/generation-test\/results\/[^/]+\/favorite$/, 'gentest-favorite-reply.html', {}],
  [
    'DELETE',
    /^\/generation-test\/results\/[^/]+$/,
    'gentest-result-gone.html',
    { 'HX-Trigger-After-Swap': 'modal-close' },
  ],
];

async function gentestAnswerMiddleware(ctx, next) {
  const result = gentestResultAnswers.find(([method, path]) => ctx.method === method && path.test(ctx.path));
  const answer = result ? [result[2], 200, result[3]] : gentestAnswers[`${ctx.method} ${ctx.path}`];
  if (!answer) return next();
  const [file, status, headers = {}] = answer;
  ctx.status = status;
  ctx.type = 'text/html';
  for (const [name, value] of Object.entries(headers)) ctx.set(name, value);
  ctx.body = await readFile(new URL(`./src/components/testdata/${file}`, import.meta.url));
}

// The answers of the ZIP export of the history (test: gentest/export-pick): the bar asks through htmx first. A choice with
// "latest=7" cannot be saved (the fragment of exportbar.Problem, 422); any other is answered with HX-Redirect to a download,
// which the browser loads without leaving the page. What was downloaded is told by /__export-downloads.
const exportDownloads = [];

async function exportAnswerMiddleware(ctx, next) {
  if (ctx.path === '/__export-downloads') {
    ctx.type = 'application/json';
    ctx.body = JSON.stringify(exportDownloads);
    return;
  }
  if (ctx.path !== '/generation-test/export') return next();
  if (ctx.get('HX-Request') !== 'true') {
    exportDownloads.push(ctx.url);
    ctx.set('Content-Disposition', 'attachment; filename=results.zip');
    ctx.type = 'application/zip';
    ctx.body = 'PK';
    return;
  }
  if (ctx.query.latest === '7') {
    ctx.status = 422;
    ctx.type = 'text/html';
    ctx.body = await readFile(new URL('./src/components/testdata/gentest-export-refused.html', import.meta.url));
    return;
  }
  ctx.set('HX-Redirect', ctx.url);
  ctx.status = 204;
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
    gentestAnswerMiddleware,
    exportAnswerMiddleware,
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
