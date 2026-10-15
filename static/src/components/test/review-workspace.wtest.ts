import { expect, waitUntil } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';

// The Review Workspace (organisms/review_workspace.templ): adding or deleting a comment and saving the
// decision answer with their own part of the screen, so the <video> keeps playing where it is and what
// is typed in the other forms stays. The fixtures in testdata are what the handlers answered (rendered
// by the real templates); web-test-runner.config.mjs serves them for the requests of the forms.
const CLIP = URL.createObjectURL(await (await fetch('/src/components/testdata/clip.webm')).blob());
const WORKSPACE = await (await fetch('/src/components/testdata/review-workspace.html')).text();

function $<T extends Element>(selector: string): T {
  const el = document.querySelector<T>(selector);
  if (!el) throw new Error(`no element for ${selector}`);
  return el;
}

function type(selector: string, value: string): void {
  const el = $<HTMLInputElement | HTMLTextAreaElement>(selector);
  el.value = value;
}

function submit(button: Element): void {
  (button as HTMLButtonElement).click();
}

describe('Review Workspace: actions replace their own part only', () => {
  let host: HTMLElement;
  let video: HTMLVideoElement;
  const realConfirm = window.confirm;

  beforeEach(async function () {
    this.timeout(10_000);
    host = document.createElement('div');
    host.innerHTML = WORKSPACE;
    document.body.append(host);
    htmx.process(host);
    window.confirm = () => true;

    video = $<HTMLVideoElement>('#review-workspace video');
    video.src = CLIP;
    await waitUntil(() => video.readyState >= 1, 'video metadata did not load', { timeout: 5000 });
    video.currentTime = 1;
    await waitUntil(() => !video.seeking, 'the video did not seek', { timeout: 5000 });
  });

  afterEach(() => {
    window.confirm = realConfirm;
    host.remove();
  });

  function expectVideoUntouched(): void {
    expect($('#review-workspace video')).to.equal(video);
    expect(video.currentTime).to.be.closeTo(1, 0.05);
  }

  function fillDecisionDraft(): void {
    type('#review-reviewer-name', 'Hana');
    type('#review-decision-comment', '全体に良いです');
    $<HTMLInputElement>('input[name=decision][value=changes_requested]').checked = true;
  }

  function expectDecisionDraft(): void {
    expect($<HTMLInputElement>('#review-reviewer-name').value).to.equal('Hana');
    expect($<HTMLTextAreaElement>('#review-decision-comment').value).to.equal('全体に良いです');
    expect($<HTMLInputElement>('input[name=decision][value=changes_requested]').checked).to.equal(true);
  }

  async function addComment(): Promise<void> {
    type('#review-comment-timecode', '1:23');
    type('#review-comment-body', 'ロゴが小さい');
    submit($('#review-comment-form button[type=submit]'));
    await waitUntil(() => document.querySelector('#review-comments li'), 'the comment was not listed', {
      timeout: 5000,
    });
  }

  it('adds a comment: the list is replaced and the form is emptied; the video and the decision draft stay', async () => {
    fillDecisionDraft();
    await addComment();

    expect($('#review-comments li').textContent).to.contain('ロゴが小さい');
    expect($<HTMLInputElement>('#review-comment-timecode').value).to.equal('');
    expect($<HTMLTextAreaElement>('#review-comment-body').value).to.equal('');
    expect(document.querySelectorAll('#review-comment-form')).to.have.length(1);
    expectVideoUntouched();
    expectDecisionDraft();
  });

  it('deletes a comment: the list is replaced; the video, the comment being written and the decision draft stay', async () => {
    await addComment();
    type('#review-comment-timecode', '2:00');
    type('#review-comment-body', '書きかけのコメント');
    fillDecisionDraft();

    submit($('#review-comments li button'));
    await waitUntil(() => !document.querySelector('#review-comments li'), 'the comment was not removed', {
      timeout: 5000,
    });

    expect($('#review-comments').textContent).to.contain('コメントはまだありません');
    expect($<HTMLInputElement>('#review-comment-timecode').value).to.equal('2:00');
    expect($<HTMLTextAreaElement>('#review-comment-body').value).to.equal('書きかけのコメント');
    expectVideoUntouched();
    expectDecisionDraft();
  });

  it('saves the decision: the section shows the download link; the video and the comment being written stay', async () => {
    fillDecisionDraft();
    type('#review-comment-timecode', '0:30');
    type('#review-comment-body', '書きかけのコメント');

    submit($('#review-decision form button[type=submit]'));
    await waitUntil(() => document.querySelector('#review-decision a[download]'), 'the decision was not saved', {
      timeout: 5000,
    });

    // the section now shows what the server saved (the fixture's answer), not the draft
    expect($<HTMLInputElement>('#review-reviewer-name').value).to.equal('Taro');
    expect($<HTMLInputElement>('input[name=decision][value=approved]').checked).to.equal(true);
    expect($<HTMLInputElement>('#review-comment-timecode').value).to.equal('0:30');
    expect($<HTMLTextAreaElement>('#review-comment-body').value).to.equal('書きかけのコメント');
    expectVideoUntouched();
  });
});
