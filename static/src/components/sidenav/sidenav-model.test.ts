import { describe, expect, it } from 'vitest';
import { MAX_AGE_SECONDS, stateCookie } from './sidenav-model';

describe('stateCookie', () => {
  it('stores the folded value for a year, for the whole site, for the same site only', () => {
    expect(stateCookie('mam_nav', 'folded', true)).toBe(
      `mam_nav=folded; Path=/; Max-Age=${MAX_AGE_SECONDS}; SameSite=Strict`,
    );
  });

  it('stores "open" when the menu is open, so that the server reads it as not folded', () => {
    expect(stateCookie('mam_nav', 'folded', false)).toBe(
      `mam_nav=open; Path=/; Max-Age=${MAX_AGE_SECONDS}; SameSite=Strict`,
    );
  });
});
