/** A Branch of GET /api/v1/projects/:id/branches. The URLs are the server's: the component builds none. */
export interface BranchNode {
  id: string;
  name: string;
  parent_id?: string;
  branch_point_type: 'scene' | 'shot' | 'frame';
  scene_number: number;
  shot_number?: number;
  offset_seconds?: number;
  snapshot_version: number;
  version: number;
  detail_url: string;
  world_state_url: string;
  children: BranchNode[];
}

/** A Branch placed on the drawing: `row` is its line, `depth` its indentation, `parentRow` the line of the node it hangs from. */
export interface PlacedBranch {
  node: BranchNode;
  depth: number;
  row: number;
  parentRow: number;
}

/** The trunk is row 0 at depth 0; Branches follow depth-first, each under the node its Branch Point lies in. */
export function layoutBranches(roots: BranchNode[]): PlacedBranch[] {
  const placed: PlacedBranch[] = [];
  const walk = (nodes: BranchNode[], depth: number, parentRow: number): void => {
    for (const node of nodes) {
      const row = placed.length + 1;
      placed.push({ node, depth, row, parentRow });
      walk(node.children, depth + 1, row);
    }
  };
  walk(roots, 1, 0);
  return placed;
}

/** Where the Branch starts, in the numbers of the series it hangs from ("Scene 2 / Shot 1 の後"). */
export function branchPointLabel(node: BranchNode): string {
  const scene = `Scene ${node.scene_number}`;
  const shot = `${scene} / Shot ${node.shot_number ?? 0}`;
  switch (node.branch_point_type) {
    case 'scene':
      return `${scene} の頭`;
    case 'frame':
      return `${shot} の ${(node.offset_seconds ?? 0).toFixed(1)} 秒`;
    default:
      return `${shot} の後`;
  }
}
