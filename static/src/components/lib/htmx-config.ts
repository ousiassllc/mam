/** The part of htmx.config this app sets. */
export interface HtmxSecurityConfig {
  selfRequestsOnly: boolean;
  allowScriptTags: boolean;
  allowEval: boolean;
}

/**
 * The front end is served by the app itself and its fragments carry no <script>, hx-on or
 * `js:` expression: never let htmx call external hosts, run a <script> that turns up in a fragment,
 * or evaluate strings.
 */
export function lockDownHtmx(config: HtmxSecurityConfig): void {
  config.selfRequestsOnly = true;
  config.allowScriptTags = false;
  config.allowEval = false;
}
