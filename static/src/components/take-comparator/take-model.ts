import type { SegmentLimits } from '../lib/timeline-model';
import type { TakeStatus } from './take-status';

/** A Take of GET /api/v1/shots/:id/takes. The URLs are the server's: the component builds none. An action link is absent while the server does not allow the action. */
export interface TakeScore {
  id: string;
  label: string;
  status: TakeStatus;
  status_label: string;
  adopted: boolean;
  model_name: string;
  resolution?: string;
  fps?: number;
  duration_seconds?: number;
  verdict?: string;
  scores: { product: number; acting: number; camera: number; continuity: number; overall: number } | null;
  proxied: boolean;
  video_url?: string;
  thumbnail_url?: string;
  judge_result_url: string;
  select_url?: string;
  reject_url?: string;
  partial_usable: boolean;
}

export interface TakesBody {
  shot_id: string;
  duration_seconds?: number;
  partial_url?: string;
  limits: SegmentLimits & { min_axis_score: number };
  takes: TakeScore[];
}
