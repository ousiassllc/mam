import { describe, expect, it } from 'vitest';
import { startingRoles } from './starting-roles';

describe('startingRoles', () => {
  it('starts the first picture as the first frame while an I2V model is ticked, and the others as references', () => {
    const two = [
      { touched: false, role: 'reference' },
      { touched: false, role: 'reference' },
    ];
    expect(startingRoles(two, true)).toEqual(['first_frame', 'reference']);
    expect(startingRoles(two, false), 'no model needs one: all are references again').toEqual([
      'reference',
      'reference',
    ]);
    expect(startingRoles([], true)).toEqual([]);
  });

  it('never moves a role the user chose, and gives the first frame only once', () => {
    expect(
      startingRoles(
        [
          { touched: true, role: 'last_frame' },
          { touched: false, role: 'reference' },
          { touched: false, role: 'reference' },
        ],
        true,
      ),
    ).toEqual(['last_frame', 'first_frame', 'reference']);
    expect(
      startingRoles(
        [
          { touched: false, role: 'reference' },
          { touched: true, role: 'first_frame' },
        ],
        true,
      ),
      'the user already made one the first frame',
    ).toEqual(['reference', 'first_frame']);
    expect(startingRoles([{ touched: true, role: 'reference' }], true), 'a reference the user chose stays one').toEqual(
      ['reference'],
    );
  });
});
