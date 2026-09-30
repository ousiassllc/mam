import { LitElement, html, nothing, svg } from 'lit';
import { customElement, property, state } from 'lit/decorators.js';
import { get } from '../lib/api';
import { createLogger } from '../lib/logger';
import { errorMessage } from '../take-comparator/mam-take-comparator';
import { branchPointLabel, layoutBranches, type BranchNode, type PlacedBranch } from './branch-layout';

const log = createLogger('branch-tree');

interface BranchesBody {
  project_id: string;
  branches: BranchNode[];
}

const ROW_HEIGHT = 40;
const INDENT = 28;
const PAD = 16;
const RADIUS = 6;
const LABEL_GAP = 12;
const CHAR_WIDTH = 8;

/**
 * `<mam-branch-tree>`: the Branch hierarchy of a Project as an SVG tree (spec/components/overview.md §4.4,
 * UC-5): the trunk on top, every Branch under the Scene / Shot it starts from.
 *
 * `api-url` is GET /api/v1/projects/:id/branches. Choosing a Branch (click, Enter or Space) fires
 * `branch-selected` (bubbles, composed; detail: the Branch as the API returned it, with `detail_url` and
 * `world_state_url`); the page loads the Branch detail from `detail_url`. `selected` is the ID of the
 * Branch the page shows. `load()` reads the tree again. It renders into the light DOM.
 */
@customElement('mam-branch-tree')
export class MamBranchTree extends LitElement {
  @property({ type: String, attribute: 'api-url' }) apiUrl = '';
  @property({ type: String }) selected = '';

  @state() private tree: BranchNode[] = [];
  @state() private loaded = false;
  @state() private error = '';

  private loadToken = 0;

  protected override createRenderRoot(): HTMLElement {
    return this;
  }

  protected override updated(changed: Map<string, unknown>): void {
    if (changed.has('apiUrl')) void this.load();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.loadToken += 1;
  }

  /** Loads the tree again (after a Branch was renamed). */
  async load(): Promise<void> {
    const token = ++this.loadToken;
    if (!this.apiUrl) return;
    try {
      const body = await get<BranchesBody>(this.apiUrl);
      if (token !== this.loadToken) return;
      this.tree = body.branches;
      this.error = '';
    } catch (error) {
      if (token === this.loadToken) this.error = errorMessage(error);
      log.warn('could not load the Branches', error);
    } finally {
      if (token === this.loadToken) this.loaded = true;
    }
  }

  private select(node: BranchNode): void {
    this.selected = node.id;
    this.dispatchEvent(new CustomEvent('branch-selected', { detail: node, bubbles: true, composed: true }));
  }

  private onKey(event: KeyboardEvent, node: BranchNode): void {
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      this.select(node);
    }
  }

  protected override render() {
    if (!this.loaded) return html`<p class="text-sm text-neutral-400" role="status">読み込み中…</p>`;
    const error = this.error
      ? html`<p class="rounded border border-red-500/50 bg-red-950/40 px-3 py-2 text-xs text-red-200" role="alert" data-error>${this.error}</p>`
      : nothing;
    const placed = layoutBranches(this.tree);
    if (placed.length === 0) {
      return html`${error}<p class="text-sm text-neutral-400" data-empty>まだ Branch がありません。Shot から「Branch を作る」か、チャットで「このカットから別展開を作って」と頼めます。</p>`;
    }
    const labelOf = (p: PlacedBranch) => `${p.node.name}（${branchPointLabel(p.node)}）`;
    const width = Math.max(...placed.map((p) => PAD + p.depth * INDENT + RADIUS + LABEL_GAP + labelOf(p).length * CHAR_WIDTH)) + PAD;
    const height = PAD * 2 + (placed.length + 1) * ROW_HEIGHT - ROW_HEIGHT / 2;
    const x = (depth: number) => PAD + depth * INDENT;
    const y = (row: number) => PAD + row * ROW_HEIGHT;
    return html`${error}<svg
        viewBox="0 0 ${width} ${height}"
        width=${width}
        height=${height}
        class="max-w-full"
        role="tree"
        aria-label="Branch ツリー"
        data-tree
      >
        ${svg`<g data-trunk>
          <circle cx=${x(0)} cy=${y(0)} r=${RADIUS} class="fill-neutral-300"></circle>
          <text x=${x(0) + RADIUS + LABEL_GAP} y=${y(0) + 4} class="fill-neutral-300 text-xs">トランク</text>
        </g>`}
        ${placed.map((p) => {
          const parentDepth = p.depth - 1;
          const isSelected = p.node.id === this.selected;
          return svg`<g
            role="treeitem"
            tabindex="0"
            aria-selected=${isSelected ? 'true' : 'false'}
            class="cursor-pointer outline-none focus-visible:opacity-80"
            data-branch=${p.node.id}
            @click=${() => this.select(p.node)}
            @keydown=${(e: KeyboardEvent) => this.onKey(e, p.node)}
          >
            <path d="M ${x(parentDepth)} ${y(p.parentRow)} V ${y(p.row)} H ${x(p.depth)}" class="fill-none stroke-neutral-600"></path>
            <circle cx=${x(p.depth)} cy=${y(p.row)} r=${RADIUS} class=${isSelected ? 'fill-indigo-400' : 'fill-neutral-400'}></circle>
            <text x=${x(p.depth) + RADIUS + LABEL_GAP} y=${y(p.row) + 4} class=${isSelected ? 'fill-indigo-300 text-xs font-semibold' : 'fill-neutral-200 text-xs'}>${labelOf(p)}</text>
          </g>`;
        })}
      </svg>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    'mam-branch-tree': MamBranchTree;
  }
}
