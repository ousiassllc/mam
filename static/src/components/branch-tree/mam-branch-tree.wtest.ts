import { expect, fixture, html, oneEvent, waitUntil } from '@open-wc/testing';
import './mam-branch-tree';
import type { MamBranchTree } from './mam-branch-tree';
import type { BranchNode } from './branch-layout';
import { stubFetch } from '../testdata/fakes';

const API = '/api/v1/projects/p1/branches';

function node(id: string, name: string, children: BranchNode[] = []): BranchNode {
  return {
    id,
    name,
    branch_point_type: 'shot',
    scene_number: 1,
    shot_number: 2,
    snapshot_version: 1,
    version: 1,
    detail_url: `/branches/${id}`,
    world_state_url: `/projects/p1/world-state?branch_id=${id}`,
    children,
  };
}

async function tree(): Promise<MamBranchTree> {
  const el = await fixture<MamBranchTree>(html`<mam-branch-tree api-url=${API}></mam-branch-tree>`);
  await waitUntil(() => el.querySelector('[data-tree], [data-empty], [data-error]') !== null, 'the Branches were not shown');
  return el;
}

describe('mam-branch-tree', () => {
  let stub: ReturnType<typeof stubFetch>;
  afterEach(() => stub.restore());

  it('draws the Branches under the Shot they start from, nested Branches one level deeper', async () => {
    stub = stubFetch({
      [`GET ${API}`]: { body: { project_id: 'p1', branches: [node('a', 'ドアを開ける', [node('a1', '奥へ進む')]), node('b', '逃げる')] } },
    });
    const el = await tree();
    const items = [...el.querySelectorAll('[data-branch]')];
    expect(items.map((i) => i.getAttribute('data-branch'))).to.deep.equal(['a', 'a1', 'b']);
    expect(items[0]?.textContent).to.contain('ドアを開ける');
    expect(items[0]?.textContent).to.contain('Scene 1 / Shot 2 の後');
    const cx = (i?: Element) => Number(i?.querySelector('circle')?.getAttribute('cx'));
    expect(cx(items[1])).to.be.greaterThan(cx(items[0]));
    expect(cx(items[2])).to.equal(cx(items[0]));
  });

  it('fires branch-selected with the Branch (and its URLs) on click and on Enter', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: { project_id: 'p1', branches: [node('a', 'A'), node('b', 'B')] } } });
    const el = await tree();

    setTimeout(() => (el.querySelector('[data-branch="a"]') as SVGElement).dispatchEvent(new MouseEvent('click', { bubbles: true })));
    const first = (await oneEvent(el, 'branch-selected')) as CustomEvent<BranchNode>;
    expect(first.detail.detail_url).to.equal('/branches/a');
    expect(first.detail.world_state_url).to.equal('/projects/p1/world-state?branch_id=a');

    setTimeout(() => (el.querySelector('[data-branch="b"]') as SVGElement).dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })));
    const second = (await oneEvent(el, 'branch-selected')) as CustomEvent<BranchNode>;
    expect(second.detail.id).to.equal('b');
    await el.updateComplete;
    expect(el.querySelector('[data-branch="b"]')?.getAttribute('aria-selected')).to.equal('true');
    expect(el.querySelector('[data-branch="a"]')?.getAttribute('aria-selected')).to.equal('false');
  });

  it('says so when there is no Branch, and reports a failed load', async () => {
    stub = stubFetch({ [`GET ${API}`]: { body: { project_id: 'p1', branches: [] } } });
    let el = await tree();
    expect(el.querySelector('[data-empty]')).to.not.equal(null);
    stub.restore();

    stub = stubFetch({});
    el = await tree();
    expect(el.querySelector('[data-error]')).to.not.equal(null);
  });

  it('draws the tree again on load()', async () => {
    let branches = [node('a', 'A')];
    stub = stubFetch({ [`GET ${API}`]: () => ({ body: { project_id: 'p1', branches } }) });
    const el = await tree();
    branches = [node('a', 'Renamed')];
    await el.load();
    await el.updateComplete;
    expect(el.querySelector('[data-branch="a"]')?.textContent).to.contain('Renamed');
  });
});
