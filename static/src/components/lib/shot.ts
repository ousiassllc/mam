/** Pure helpers of the Shot progress display (DOM wiring is in shot.ts). */

/** Message of WebSocket /ws/shots/:id. */
export interface ShotProgress {
  shot_id: string;
  action?: 'generate' | 'reference_pack' | 'judge';
  stage: 'idle' | 'queued' | 'generating' | 'judging' | 'done' | 'failed';
  message?: string;
  takes_done?: number;
  takes_total?: number;
}

/** Work on the Shot is over (or nothing is running): the card must be refreshed and the socket closed. */
export function isFinished(progress: Pick<ShotProgress, 'stage'>): boolean {
  return progress.stage === 'done' || progress.stage === 'failed' || progress.stage === 'idle';
}

/** What the placeholder of a Shot that has no video (yet) says about its progress. */
export function describeProgress(progress: Pick<ShotProgress, 'stage' | 'action' | 'message' | 'takes_done' | 'takes_total'> | null): string {
  if (!progress || progress.stage === 'idle') return '動画はまだありません';
  const judge = progress.action === 'judge';
  switch (progress.stage) {
    case 'queued':
      return judge ? 'Judge の順番を待っています' : '生成の順番を待っています';
    case 'generating':
      return progress.takes_total ? `生成中 ${progress.takes_done ?? 0} / ${progress.takes_total} Take` : '生成中';
    case 'judging':
      return 'AI Video Judge が評価しています';
    case 'done':
      return judge ? '評価が完了しました' : '生成が完了しました';
    case 'failed':
      return progress.message ? `失敗しました: ${progress.message}` : '失敗しました';
  }
}

/** Fraction (0..1) of the work that is done, or null when the progress has no measure. */
export function progressFraction(progress: Pick<ShotProgress, 'stage' | 'takes_done' | 'takes_total'> | null): number | null {
  if (!progress) return null;
  if (progress.stage === 'done') return 1;
  if (progress.stage === 'generating' && progress.takes_total) {
    return Math.min((progress.takes_done ?? 0) / progress.takes_total, 1);
  }
  return null;
}
