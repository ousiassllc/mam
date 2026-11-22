import { expect } from '@open-wc/testing';
import { whenFaded } from './fade';

describe('whenFaded', () => {
  let el: HTMLElement;
  beforeEach(() => {
    el = document.createElement('div');
    el.style.opacity = '1';
    el.style.transition = 'opacity 150ms linear';
    document.body.append(el);
  });
  afterEach(() => el.remove());

  it('resolves at once when nothing is running', async () => {
    await whenFaded(el);
  });

  it('waits for the transition that the change just started, not for a timer', async () => {
    el.style.opacity = '0';
    let done = false;
    const faded = whenFaded(el).then(() => (done = true));
    await Promise.resolve();
    expect(done).to.equal(false);
    await faded;
    expect(done).to.equal(true);
    expect(getComputedStyle(el).opacity).to.equal('0');
  });

  it('does not wait for an animation that never ends', async () => {
    const spinner = document.createElement('span');
    spinner.animate([{ opacity: 1 }, { opacity: 0.5 }], { duration: 1000, iterations: Infinity });
    el.append(spinner);
    await whenFaded(el);
  });

  it('is over when the element is removed in the middle of the fade', async () => {
    el.style.opacity = '0';
    const faded = whenFaded(el);
    el.remove();
    await faded;
  });
});
