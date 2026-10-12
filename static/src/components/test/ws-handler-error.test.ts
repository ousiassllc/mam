import { describe, expect, it, vi } from 'vitest';
import { connect, type SocketLike } from '../lib/ws';

// An error thrown by the caller's onMessage is a bug of the caller, not a malformed server message:
// it must reach window.onerror instead of being logged as "ignored a malformed message".
describe('connect: onMessage errors', () => {
  it('does not report an error thrown by onMessage as a malformed message', () => {
    const socket: SocketLike = { onopen: null, onmessage: null, onclose: null, close: () => {} };
    const boom = new Error('render failed');
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    try {
      connect({
        url: 'ws://x/ws',
        onMessage: () => {
          throw boom;
        },
        createSocket: () => socket,
        subscribeResume: () => () => {},
      });
      expect(() => socket.onmessage?.({ data: '{"type":"message.created"}' })).toThrow(boom);
      expect(warn).not.toHaveBeenCalled();
      socket.onmessage?.({ data: 'not json' });
      expect(warn).toHaveBeenCalledTimes(1);
    } finally {
      warn.mockRestore();
    }
  });
});
