import { describe, expect, it, vi } from 'vitest';
import { hasEmptySegment, MISSING_ID_MESSAGE, refuseMissingId } from './request-guard';

function configRequest(path: string): CustomEvent<{ path: string }> {
  return new CustomEvent('htmx:configRequest', { detail: { path }, cancelable: true });
}

describe('hasEmptySegment', () => {
  it('finds the empty segment an empty project ID leaves', () => {
    expect(hasEmptySegment('/projects//messages')).toBe(true);
    expect(hasEmptySegment('/projects//messages?after=m1')).toBe(true);
    expect(hasEmptySegment('http://localhost:8080/projects//approvals/m1')).toBe(true);
  });

  it('lets a path with every segment through, whatever its query holds', () => {
    expect(hasEmptySegment('/projects/p1/messages')).toBe(false);
    expect(hasEmptySegment('/projects/p1/messages?after=m%2F1')).toBe(false);
    expect(hasEmptySegment('/projects/p1/messages?next=//x')).toBe(false);
    expect(hasEmptySegment('/')).toBe(false);
  });
});

describe('refuseMissingId', () => {
  it('cancels the request and tells the user in Japanese, with the way out', () => {
    const show = vi.fn();
    const event = configRequest('/projects//messages');

    refuseMissingId(event, show);

    expect(event.defaultPrevented).toBe(true);
    expect(show).toHaveBeenCalledWith(MISSING_ID_MESSAGE);
    expect(MISSING_ID_MESSAGE).toContain('再読み込み');
  });

  it('leaves a request with a project ID alone', () => {
    const show = vi.fn();
    const event = configRequest('/projects/p1/messages');

    refuseMissingId(event, show);

    expect(event.defaultPrevented).toBe(false);
    expect(show).not.toHaveBeenCalled();
  });
});
