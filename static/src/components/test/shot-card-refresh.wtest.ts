import { expect } from '@open-wc/testing';
import { planCardRefresh } from '../lib/shot';

// planCardRefresh builds selectors with CSS.escape (a browser API), so it is tested in Chrome, against a DOM.
describe('planCardRefresh', () => {
  const card = { id: 'shot-b', refreshUrl: '/projects/p/scenes/s?shot=b' };

  it('refreshes the whole Shot view when the selected Shot finished', () => {
    expect(planCardRefresh({ ...card, selected: true }, '/projects/p/scenes/s?shot=b')).to.deep.equal({
      url: '/projects/p/scenes/s?shot=b',
      target: '#scene-shot-view',
      swap: 'outerHTML',
    });
  });

  it('refreshes only the finished card, from the selected Shot page, when another Shot is viewed', () => {
    expect(planCardRefresh({ ...card, selected: false }, '/projects/p/scenes/s?shot=a')).to.deep.equal({
      url: '/projects/p/scenes/s?shot=a',
      target: '#shot-b',
      swap: 'outerHTML',
      select: '#shot-b',
    });
  });

  it('asks for the Scene page without ?shot= when no Shot is selected, so the card is not rendered as selected', () => {
    expect(planCardRefresh({ ...card, selected: false }, null).url).to.equal('/projects/p/scenes/s');
  });

  // An imported Shot ID may hold characters that mean something in a selector (#550).
  it('selects exactly the card of a Shot whose ID holds ".", ":", " ", "#" or "["', () => {
    for (const shotId of ['a.b', 'a:b', 'a b', 'a#b', '1', 'a[b]']) {
      const root = document.createElement('ul');
      for (const id of [`shot-${shotId}`, 'shot-a']) {
        const li = document.createElement('li');
        li.id = id;
        root.append(li);
      }
      const plan = planCardRefresh({ id: `shot-${shotId}`, refreshUrl: '/projects/p/scenes/s', selected: false }, null);
      const found = root.querySelectorAll(plan.target);
      expect(found.length, shotId).to.equal(1);
      expect(found[0]?.id, shotId).to.equal(`shot-${shotId}`);
      expect(plan.select, shotId).to.equal(plan.target);
    }
  });
});
