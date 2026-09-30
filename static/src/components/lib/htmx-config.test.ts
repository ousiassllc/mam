import { describe, expect, it } from 'vitest';
import { lockDownHtmx, type HtmxSecurityConfig } from './htmx-config';

describe('lockDownHtmx', () => {
  it('turns off script tags and eval and keeps requests on the same origin', () => {
    const config: HtmxSecurityConfig = { selfRequestsOnly: false, allowScriptTags: true, allowEval: true };
    lockDownHtmx(config);
    expect(config).toEqual({ selfRequestsOnly: true, allowScriptTags: false, allowEval: false });
  });
});
