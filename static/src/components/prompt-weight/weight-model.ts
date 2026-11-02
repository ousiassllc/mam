/**
 * The rules of the word weights of a prompt (Issue #977, #972): what a selected word weighs now, and the text that raises or lowers it by 0.1.
 * It reads the prompt the way ComfyUI does (comfy/sd1_clip.py `token_weights`): "(word:1.3)" weighs 1.3 whatever is around it, "(word)" weighs
 * 1.1 times what is around it, a backslash escapes a bracket, and "[word]" is plain text. The range is MIN..MAX (spec/components/gentest-tiers.md).
 * Everything here is a pure function of the text and the selection (UTF-16 offsets, as in a textarea).
 */

export const WEIGHT_MIN = 0.5;
export const WEIGHT_MAX = 1.5;
export const WEIGHT_STEP = 0.1;
/** What "(word)" multiplies by in ComfyUI. */
const PLAIN_FACTOR = 1.1;

export type Direction = 1 | -1;

/** What a selection weighs, and which way it may still move. */
export interface Reading {
  weight: number;
  canRaise: boolean;
  canLower: boolean;
}

/** The new text, the selection that is put on the changed part, and where it came from: `value.slice(start, end)` replaces [start, replacedEnd) of the old text. */
export interface Change {
  value: string;
  start: number;
  end: number;
  replacedEnd: number;
}

/** A matched pair of brackets: `open` and `close` are the offsets of the "(" and the ")"; `explicit` is the ":1.3" it names (null for a plain one). */
interface Group {
  open: number;
  close: number;
  explicit: number | null;
  /** Where the content ends: before the ":1.3" when there is one. */
  innerEnd: number;
}

const SUFFIX = /:\s*(-?\d+(?:\.\d+)?|-?\.\d+)\s*$/;

function groupsOf(text: string): Group[] {
  const out: Group[] = [];
  const stack: number[] = [];
  for (let i = 0; i < text.length; i++) {
    const c = text[i];
    if (c === '\\') i++;
    else if (c === '(') stack.push(i);
    else if (c === ')' && stack.length > 0) {
      const open = stack.pop()!;
      const inner = text.slice(open + 1, i);
      const m = SUFFIX.exec(inner);
      // ComfyUI cuts at the last ":" and only when it is not the first character of the content
      const named = m && m.index > 0 ? m : null;
      out.push({
        open,
        close: i,
        explicit: named ? Number(named[1]) : null,
        innerEnd: open + 1 + (named ? named.index : inner.length),
      });
    }
  }
  return out.sort((a, b) => a.open - b.open);
}

/** The weight of the text in [a, b): each group around it multiplies by 1.1, or sets the weight when it names one (outermost first). */
function weightAround(groups: readonly Group[], a: number, b: number): number {
  let w = 1;
  for (const g of groups) if (g.open < a && g.close >= b) w = g.explicit ?? w * PLAIN_FACTOR;
  return w;
}

const round1 = (w: number): number => Math.round(w * 10) / 10;

const SPACE = /\s/;

/** The selection without the white space at its ends (a double click on a word often takes the space after it). */
function trimmed(text: string, start: number, end: number): [number, number] {
  let s = Math.min(start, end);
  let e = Math.max(start, end);
  while (s < e && SPACE.test(text[s]!)) s++;
  while (e > s && SPACE.test(text[e - 1]!)) e--;
  return [s, e];
}

/** The content of a group without its brackets and its ":1.3", trimmed. */
function coreOf(text: string, g: Group): [number, number] {
  return trimmed(text, g.open + 1, g.innerEnd);
}

/** The part that changes: `outer` is what is replaced (the outermost group around the selection, or the selection), `core` the words in it. */
interface Target {
  outer: [number, number];
  core: [number, number];
}

function targetOf(text: string, groups: readonly Group[], s: number, e: number): Target | null {
  // a selection that cuts a group (holds one of its brackets and not the other) cannot be wrapped
  const held = (i: number) => i >= s && i < e;
  if (groups.some((g) => held(g.open) !== held(g.close))) return null;
  let [a, b] = [s, e];
  // the words of a group that is selected without its brackets: take the group; and so on outwards ("((word))")
  for (;;) {
    const g = groups.find((x) => {
      const [cs, ce] = coreOf(text, x);
      return x.open < a && x.close >= b && cs === a && ce === b;
    });
    if (!g) break;
    [a, b] = [g.open, g.close + 1];
  }
  const outer: [number, number] = [a, b];
  // the words of a group that is selected with its brackets: the group's own, and so on inwards
  for (;;) {
    const g = groups.find((x) => x.open === a && x.close + 1 === b);
    if (!g) break;
    [a, b] = coreOf(text, g);
  }
  return a < b ? { outer, core: [a, b] } : null;
}

/** What the selection [start, end) weighs and which way it can move; null when nothing can be changed (nothing selected, or the selection cuts a bracket pair). */
export function read(text: string, start: number, end: number): Reading | null {
  const [s, e] = trimmed(text, start, end);
  if (s === e) return null;
  const groups = groupsOf(text);
  const t = targetOf(text, groups, s, e);
  if (!t) return null;
  const weight = round1(weightAround(groups, t.core[0], t.core[1]));
  return { weight, canRaise: weight < WEIGHT_MAX, canLower: weight > WEIGHT_MIN };
}

/**
 * The text with the selected words 0.1 heavier (`dir` 1) or lighter (-1): "word" → "(word:1.1)", "(word:1.1)" → "(word:1.2)", back to what
 * it is around → the brackets are taken off. Null when nothing changes (see `read`, or the end of the range is reached).
 */
export function adjust(text: string, start: number, end: number, dir: Direction): Change | null {
  const [s, e] = trimmed(text, start, end);
  if (s === e) return null;
  const groups = groupsOf(text);
  const t = targetOf(text, groups, s, e);
  if (!t) return null;
  const now = round1(weightAround(groups, t.core[0], t.core[1]));
  if ((dir > 0 && now >= WEIGHT_MAX) || (dir < 0 && now <= WEIGHT_MIN)) return null;
  const next = Math.min(WEIGHT_MAX, Math.max(WEIGHT_MIN, round1(now + dir * WEIGHT_STEP)));
  const core = text.slice(t.core[0], t.core[1]);
  // around the group the words weigh what they would without it: no group is needed
  const bare = round1(weightAround(groups, t.outer[0], t.outer[1])) === next;
  const piece = bare ? core : `(${core}:${next.toFixed(1)})`;
  return {
    value: text.slice(0, t.outer[0]) + piece + text.slice(t.outer[1]),
    start: t.outer[0],
    end: t.outer[0] + piece.length,
    replacedEnd: t.outer[1],
  };
}
