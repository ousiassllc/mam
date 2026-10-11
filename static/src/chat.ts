/**
 * Behaviour of the Chat Workspace (F-30). The server renders everything; this module
 * only listens to /ws/projects/:id and asks for HTML fragments when told something changed
 * (the socket never carries a body), and handles the composer (reply target, attachments, Enter to send).
 */
import { ajaxChecked, ajaxGet, showToast } from './ajax';
import { createConnectionNotice, releaseIfDetached } from './components/lib/connection-notice';
import { AJAX_FAILED_MESSAGE } from './components/lib/ajax';
import { selfAndDescendants } from './components/lib/query';
import { createSyncer } from './components/lib/sync';
import { connect, socketUrl } from './components/lib/ws';
import {
  type ComposerSnapshot,
  catchUpAfterReconnect,
  fieldsToClear,
  fileKey,
  isSendKey,
  isSuccessResponse,
  removeDuplicateIds,
  type ProjectEvent,
} from './components/lib/thread';

const THREAD = '#chat-thread';

function requestXhr(event: Event): { status: number } | undefined {
  return (event as CustomEvent<{ xhr?: { status: number } }>).detail.xhr;
}

export function initChatWorkspaces(root: ParentNode = document): void {
  for (const el of selfAndDescendants<HTMLElement>(root, '#chat-workspace:not([data-chat-ready])')) {
    el.dataset.chatReady = 'true';
    new ChatWorkspace(el);
  }
}

class ChatWorkspace {
  private readonly messagesUrl: string;
  private readonly thread: HTMLElement;
  private readonly composer: HTMLFormElement;
  private readonly content: HTMLTextAreaElement;
  private readonly files: HTMLInputElement;
  private stickToBottom = true; // is the thread scrolled to the bottom (so new messages should be followed)?
  /** Fetches the messages after the last one shown; requests are merged and a failure is retried. */
  private readonly syncer = createSyncer({
    run: () => ajaxChecked('get', this.nextMessagesUrl(), { target: THREAD, swap: 'beforeend', source: THREAD }),
    onGiveUp: () => showToast(AJAX_FAILED_MESSAGE),
  });

  constructor(private readonly root: HTMLElement) {
    this.messagesUrl = root.dataset.messagesUrl ?? '';
    this.thread = root.querySelector<HTMLElement>(THREAD)!;
    this.composer = root.querySelector<HTMLFormElement>('#chat-composer')!;
    this.content = this.composer.querySelector<HTMLTextAreaElement>('#composer-content')!;
    this.files = this.composer.querySelector<HTMLInputElement>('#composer-files')!;

    this.bindThread();
    this.bindComposer();
    this.scrollToBottom();
    const notice = createConnectionNotice('チャット');
    const connection = connect<ProjectEvent>({
      url: socketUrl(root.dataset.wsUrl ?? '', location),
      onMessage: (event) => {
        if (!releaseIfDetached(root, notice, connection)) this.onEvent(event);
      },
      // After a reconnect, events may have been missed: catch up on new messages and on cards that can still change.
      onOpen: (reconnected) => {
        if (releaseIfDetached(root, notice, connection)) return;
        notice.hide();
        if (reconnected) void this.catchUp();
      },
      onGiveUp: () => {
        if (releaseIfDetached(root, notice, connection)) return;
        notice.show(() => {
          if (!releaseIfDetached(root, notice, connection)) connection.retry();
        });
      },
    });
  }

  private onEvent(event: ProjectEvent): void {
    if (event.type === 'card.updated') {
      const card = this.thread.querySelector(`#msg-${CSS.escape(event.message_id)}`);
      if (card) {
        void this.refetchCard(event.message_id);
        return;
      }
    }
    this.syncNew();
  }

  /** Replaces the card with the server's current one, from the URL the server put on it (data-card-url). */
  private refetchCard(messageId: string): Promise<boolean> {
    const target = `#msg-${CSS.escape(messageId)}`;
    const url = this.thread.querySelector<HTMLElement>(target)?.dataset.cardUrl;
    return url ? ajaxGet(url, { target, swap: 'outerHTML' }) : Promise.resolve(false);
  }

  private catchUp(): Promise<unknown> {
    return catchUpAfterReconnect({
      pendingMessageIds: () =>
        Array.from(this.thread.querySelectorAll<HTMLElement>('[data-card-pending]'), (el) => el.dataset.messageId ?? '').filter(Boolean),
      refetchCard: (id) => this.refetchCard(id),
      syncNew: () => this.syncer.request(),
    });
  }

  private syncNew(): void {
    void this.syncer.request();
  }

  /** Where the messages newer than the last one shown are: the server put it on that message (data-next-url), else the whole thread. */
  private nextMessagesUrl(): string {
    const items = this.thread.querySelectorAll<HTMLElement>('[data-message-id]');
    return items.item(items.length - 1)?.dataset.nextUrl || this.messagesUrl;
  }

  private bindThread(): void {
    // htmx swaps into the thread from actions and from syncNew: keep it duplicate-free and scrolled.
    // Only the user's scrolling (and our own scrollToBottom) changes the stickiness: a layout
    // shift elsewhere on the page must not make the thread stop following new messages.
    this.thread.addEventListener('scroll', () => (this.stickToBottom = this.isNearBottom()));
    this.thread.addEventListener('htmx:afterSwap', () => this.afterThreadChange());
    this.root.addEventListener('chat:refresh', () => this.syncNew());
    this.thread.addEventListener('click', (event) => {
      const button = (event.target as HTMLElement).closest<HTMLElement>('[data-reply-to]');
      if (button) this.setReplyTarget(button.dataset.replyTo ?? '', button.dataset.replyLabel ?? '');
    });
  }

  private afterThreadChange(): void {
    removeDuplicateIds(this.thread.querySelectorAll<HTMLElement>('[data-message-id]'));
    if (this.stickToBottom) this.scrollToBottom();
  }

  private bindComposer(): void {
    this.content.addEventListener('keydown', (event) => {
      if (isSendKey(event)) {
        event.preventDefault();
        this.composer.requestSubmit();
      }
    });
    this.files.addEventListener('change', () => this.showAttachments());
    this.composer.querySelector('#reply-cancel')?.addEventListener('click', () => this.setReplyTarget('', ''));

    this.composer.addEventListener('dragover', (event) => event.preventDefault());
    this.composer.addEventListener('drop', (event) => {
      event.preventDefault();
      const dropped = event.dataTransfer?.files;
      if (!dropped?.length) return;
      const merged = new DataTransfer();
      for (const f of [...this.files.files!, ...dropped]) merged.items.add(f);
      this.files.files = merged.files;
      this.showAttachments();
    });

    // After a successful (2xx) send the composer is cleared; a failed one keeps the user's input. Only what was
    // sent is cleared: the input stays editable while the request runs, and the next message typed meanwhile
    // is kept. detail.successful is not used: main.ts swaps HTML error responses (toasts), so htmx reports
    // those as successful too.
    let sent: ComposerSnapshot | undefined;
    this.composer.addEventListener('htmx:beforeRequest', (event) => {
      if (event.target === this.composer) sent = this.snapshot();
    });
    this.composer.addEventListener('htmx:afterRequest', (event) => {
      if (event.target !== this.composer || !sent || !isSuccessResponse(requestXhr(event))) return;
      const clear = fieldsToClear(sent, this.snapshot());
      sent = undefined;
      if (clear.content) this.content.value = '';
      if (clear.files) {
        this.files.value = '';
        this.showAttachments();
      }
      if (clear.replyTo) this.setReplyTarget('', '');
    });
    // Card buttons post replies on their own (not the refetches of syncNew): clear the pending reply target with them.
    this.thread.addEventListener('htmx:afterRequest', (event) => {
      if ((event.target as HTMLElement).hasAttribute('hx-post') && isSuccessResponse(requestXhr(event))) this.setReplyTarget('', '');
    });
  }

  private snapshot(): ComposerSnapshot {
    return {
      content: this.content.value,
      files: [...this.files.files!].map(fileKey),
      replyTo: this.composer.querySelector<HTMLInputElement>('#reply-to')!.value,
    };
  }

  private setReplyTarget(messageId: string, label: string): void {
    this.composer.querySelector<HTMLInputElement>('#reply-to')!.value = messageId;
    const chip = this.composer.querySelector<HTMLElement>('#reply-chip')!;
    chip.classList.toggle('hidden', messageId === '');
    chip.classList.toggle('flex', messageId !== '');
    this.composer.querySelector<HTMLElement>('#reply-label')!.textContent = label;
    if (messageId) this.content.focus();
  }

  private showAttachments(): void {
    const list = this.composer.querySelector<HTMLElement>('#attachment-names')!;
    list.replaceChildren(
      ...[...this.files.files!].map((file) => {
        const li = document.createElement('li');
        li.textContent = file.name;
        li.className = 'rounded bg-neutral-800 px-2 py-0.5';
        return li;
      }),
    );
  }

  private isNearBottom(): boolean {
    const t = this.thread;
    return t.scrollHeight - t.scrollTop - t.clientHeight < 80;
  }

  private scrollToBottom(): void {
    this.thread.scrollTop = this.thread.scrollHeight;
  }
}
