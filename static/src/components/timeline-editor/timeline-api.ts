import type { EditClip, EditLimits, TransitionType } from '../lib/edit-model';

/** GET /api/v1/projects/:id/edit-timeline. The URLs are the server's: the component builds none. */
export interface EditTimelineBody {
  id: string;
  version_number: number;
  duration_seconds: number;
  clips: EditClip[];
  branch_id?: string;
  save_url: string;
  preview_url: string;
  transitions: TransitionType[];
  limits: EditLimits;
  readonly: boolean;
}

/** Fired when the server made new work of its own (the Reshoot Loop was started): the page's stage buttons reload. */
export const TIMELINE_UPDATED_EVENT = 'timeline-updated';

/** The event `htmx` fires (HX-Trigger of the Rough Cut / Judge / Final QC actions) when the timeline changed on the server. */
export const TIMELINE_RELOAD_EVENT = 'timeline-reload';
