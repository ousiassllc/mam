// Tests of the Lit components (*.wtest.ts) run in a real Chrome: `npm run test:components`.
// The pure helpers of components/lib are tested by vitest (*.test.ts).
import { readFile } from 'node:fs/promises';
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
  await next();
  if (ctx.path.endsWith('/modal-saved.html')) ctx.set('HX-Trigger-After-Swap', 'modal-close');
  if (ctx.path.endsWith('/modal-invalid.html')) ctx.status = 422;
}

export default {
  files: 'src/components/**/*.wtest.ts',
  // One page at a time: when two pages load a clip together (video-player and review-workspace wtests
  // run side by side) the <video> metadata of both never loads and the tests time out.
  concurrency: 1,
  nodeResolve: true,
  middleware: [reviewAnswerMiddleware, hxTriggerMiddleware, modalAnswerMiddleware],
  plugins: [esbuildPlugin({ ts: true, tsconfig: './tsconfig.json' })],
  browsers: [
    chromeLauncher({
      launchOptions: {
        args: ['--no-sandbox', '--autoplay-policy=no-user-gesture-required'],
      },
    }),
  ],
};
