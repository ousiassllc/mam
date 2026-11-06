import { describe, expect, it } from 'vitest';
import { RESTART_PATH, shouldRefresh } from './refresh';

describe('shouldRefresh', () => {
  it('reads the panel again after a write of the settings screen', () => {
    expect(shouldRefresh('put', '/settings/llm')).toBe(true);
    expect(shouldRefresh('POST', '/settings/audio')).toBe(true);
    expect(shouldRefresh('delete', '/settings/llm')).toBe(true);
  });

  it('does not after a read, or after the panel own request (its answer is what it says)', () => {
    expect(shouldRefresh('get', '/settings/llm')).toBe(false);
    expect(shouldRefresh('post', RESTART_PATH)).toBe(false);
    expect(shouldRefresh(undefined, '/settings/llm')).toBe(false);
  });
});
