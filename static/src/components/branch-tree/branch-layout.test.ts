import { describe, expect, it } from 'vitest';
import { branchPointLabel, layoutBranches, type BranchNode } from './branch-layout';

function node(id: string, children: BranchNode[] = [], over: Partial<BranchNode> = {}): BranchNode {
  return {
    id,
    name: id,
    branch_point_type: 'shot',
    scene_number: 1,
    shot_number: 2,
    snapshot_version: 1,
    version: 1,
    detail_url: `/branches/${id}`,
    world_state_url: `/projects/p/world-state?branch_id=${id}`,
    children,
    ...over,
  };
}

describe('layoutBranches', () => {
  it('places Branches depth-first under the trunk, each row pointing at its parent', () => {
    const placed = layoutBranches([node('a', [node('a1'), node('a2', [node('a2x')])]), node('b')]);
    expect(placed.map((p) => [p.node.id, p.depth, p.row, p.parentRow])).toEqual([
      ['a', 1, 1, 0],
      ['a1', 2, 2, 1],
      ['a2', 2, 3, 1],
      ['a2x', 3, 4, 3],
      ['b', 1, 5, 0],
    ]);
  });

  it('is empty without Branches', () => {
    expect(layoutBranches([])).toEqual([]);
  });
});

describe('branchPointLabel', () => {
  it('names the point in the numbers of the series', () => {
    expect(branchPointLabel(node('a', [], { branch_point_type: 'scene', scene_number: 3 }))).toBe('Scene 3 の頭');
    expect(branchPointLabel(node('a'))).toBe('Scene 1 / Shot 2 の後');
    expect(branchPointLabel(node('a', [], { branch_point_type: 'frame', offset_seconds: 1.25 }))).toBe('Scene 1 / Shot 2 の 1.3 秒');
  });
});
