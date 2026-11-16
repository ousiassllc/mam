import { expect } from '@open-wc/testing';
import htmx from 'htmx.org';
import '../../main';

// #1153: the two file inputs under the box of the chat (the sound and the picture of a speech-to-video model). The page is what the handlers render
// (backend/internal/web/organisms/gentest/chat/fixture_test.go).
describe('the files attached in the chat of the single generation (#1153)', () => {
  let host: HTMLElement;

  const form = (kind: 'audio' | 'image') => host.querySelector<HTMLFormElement>(`form[data-gtc-attach="${kind}"]`)!;
  const input = (kind: 'audio' | 'image') => form(kind).querySelector<HTMLInputElement>('input[type="file"]')!;

  beforeEach(async () => {
    host = document.createElement('div');
    host.innerHTML = await (await fetch('/src/components/testdata/gentest-chat-thread.html')).text();
    document.body.append(host);
    htmx.process(host);
  });

  afterEach(() => host?.remove());

  it('has an input for the sound and one for the picture, each sent on its own as multipart with the conversation and its kind', () => {
    for (const kind of ['audio', 'image'] as const) {
      const f = form(kind);
      expect(f.getAttribute('hx-post'), kind).to.match(/\/generation-test\/chat\/files$/);
      expect(f.getAttribute('hx-encoding'), kind).to.equal('multipart/form-data');
      expect(f.getAttribute('hx-trigger'), kind + ': a chosen file is sent at once').to.equal('change');
      expect(f.getAttribute('hx-target'), kind + ': the answer is a line of the thread').to.equal('#gtc-thread');
      expect(f.querySelector<HTMLInputElement>('input[name="kind"]')!.value, kind).to.equal(kind);
      expect(f.querySelector<HTMLInputElement>('input[name="session"]')!.value, kind).to.match(/^[0-9a-f]{32}$/);
      expect(input(kind).name, kind).to.equal('file');
    }
  });

  it('accepts the kinds the server takes, by type and by extension', () => {
    const audio = input('audio').accept;
    for (const ext of ['.wav', '.mp3', '.flac', '.ogg', '.m4a']) expect(audio, ext).to.contain(ext);
    const image = input('image').accept;
    for (const ext of ['.png', '.jpg', '.webp']) expect(image, ext).to.contain(ext);
  });

  it('empties the input once the request ended, so that the same file can be chosen again', () => {
    for (const kind of ['audio', 'image'] as const) {
      const transfer = new DataTransfer();
      transfer.items.add(new File(['x'], 'voice.wav', { type: 'audio/wav' }));
      input(kind).files = transfer.files;
      expect(input(kind).files!.length, kind).to.equal(1);

      form(kind).dispatchEvent(
        new CustomEvent('htmx:afterRequest', {
          bubbles: true,
          detail: { elt: form(kind), successful: true, xhr: { status: 422 } },
        }),
      );

      expect(input(kind).files!.length, kind + ': a refused file is not kept either').to.equal(0);
    }
  });
});
