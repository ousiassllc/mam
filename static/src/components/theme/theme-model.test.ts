import { describe, expect, it } from 'vitest';
import { MAX_AGE_SECONDS, otherTheme, themeCookie } from './theme-model';

describe('themeCookie', () => {
  it('stores the theme for a year, for the whole site, for the same site only', () => {
    expect(themeCookie('mam_theme', 'dark')).toBe(
      `mam_theme=dark; Path=/; Max-Age=${MAX_AGE_SECONDS}; SameSite=Strict`,
    );
    expect(themeCookie('mam_theme', 'light')).toBe(
      `mam_theme=light; Path=/; Max-Age=${MAX_AGE_SECONDS}; SameSite=Strict`,
    );
  });
});

describe('otherTheme', () => {
  it('is the one the page does not show', () => {
    expect(otherTheme('dark')).toBe('light');
    expect(otherTheme('light')).toBe('dark');
  });
});
