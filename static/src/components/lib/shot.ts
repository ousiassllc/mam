/** Pure helpers of the Shot progress display (DOM wiring is in shot.ts). */

/** Message of WebSocket /ws/shots/:id. */
export interface ShotProgress {
  shot_id: string;
  action?: 'generate' | 'reference_pack' | 'judge' | 'repair';
  stage: 'idle' | 'queued' | 'generating' | 'judging' | 'repairing' | 'done' | 'failed';
  message?: string;
  takes_done?: number;
  takes_total?: number;
}

/** Work on the Shot is over (or nothing is running): the card must be refreshed and the socket closed. */
export function isFinished(progress: Pick<ShotProgress, 'stage'>): boolean {
  return progress.stage === 'done' || progress.stage === 'failed' || progress.stage === 'idle';
}
