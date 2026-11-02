import { expect } from '@open-wc/testing';
import { installSlider, syncSliders } from './slider';

// The fill of the common slider (#955): written at load and in a swapped body, and as the value moves. The look itself is
// static/src/css/slider.css; the value, the range and the step stay the input's own.
const SLIDER = (name: string, value: string) =>
  `<input type="range" name="${name}" min="0" max="1" step="0.05" value="${value}">`;

describe('the fill of the common slider', () => {
  let host: HTMLElement;
  const fill = (el: Element) => (el as HTMLElement).style.getPropertyValue('--fill');

  before(() => installSlider());

  beforeEach(() => {
    host = document.createElement('div');
    host.innerHTML = SLIDER('a', '0.7') + SLIDER('b', '0.5');
    document.body.append(host);
  });

  afterEach(() => host.remove());

  it('is written for the sliders of a body that was swapped in', () => {
    host.dispatchEvent(new CustomEvent('htmx:load', { bubbles: true }));
    expect(Number(fill(host.children[0]!))).to.be.closeTo(0.7, 1e-9);
    expect(Number(fill(host.children[1]!))).to.be.closeTo(0.5, 1e-9);
  });

  it('follows the value as it is dragged or stepped, and leaves the value itself alone', () => {
    syncSliders(host);
    const slider = host.children[0] as HTMLInputElement;
    slider.value = '0.25';
    slider.dispatchEvent(new Event('input', { bubbles: true }));
    expect(Number(fill(slider))).to.be.closeTo(0.25, 1e-9);
    expect(slider.value).to.equal('0.25');
    expect(Number(fill(host.children[1]!)), 'the other slider is not touched').to.be.closeTo(0.5, 1e-9);
  });

  it('follows the value of a slider that is disabled too (the rail is drawn from it)', () => {
    const slider = host.children[0] as HTMLInputElement;
    slider.disabled = true;
    syncSliders(slider);
    expect(Number(fill(slider))).to.be.closeTo(0.7, 1e-9);
  });
});
