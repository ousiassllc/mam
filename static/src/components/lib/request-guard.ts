/**
 * A request whose path has an empty segment ("/projects//messages") was built from a missing ID: the
 * server can only answer 404 for it. It is stopped here and the user is told what to do (Issue 1034).
 */

/** The cause and the way out, in the form of the common errors (#1010): why it failed, then what to do. */
export const MISSING_ID_MESSAGE =
  '対象のプロジェクトを特定できなかったため、送信を中止しました。画面を再読み込みしてから、もう一度お試しください。';

/** Whether the path (a path with or without a query, or a URL) has an empty segment, such as the one left by an empty ID. */
export function hasEmptySegment(path: string): boolean {
  let pathname: string;
  try {
    pathname = new URL(path, 'http://localhost').pathname;
  } catch {
    return false;
  }
  return pathname.includes('//');
}

/** The `htmx:configRequest` handler: cancels a request with a missing ID and reports it through `show`. */
export function refuseMissingId(event: Event, show: (message: string) => void): void {
  const path = (event as CustomEvent<{ path?: string }>).detail?.path;
  if (path === undefined || !hasEmptySegment(path)) return;
  event.preventDefault();
  show(MISSING_ID_MESSAGE);
}
