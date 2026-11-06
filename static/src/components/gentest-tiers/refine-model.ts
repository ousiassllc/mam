/**
 * The rules of the "こだわり" tier's marks and its way back (Issue #977), with no DOM: what an item of the form was when the tier began, what
 * it is now, and how the list of the changed ones words it. refine.ts reads and writes the controls.
 */

/** One control of the form that the tier lets the user change: the value it began with and the value it holds now. */
export interface Item {
  key: string;
  label: string;
  /** What a control with no value is called ("既定", "ランダム", "なし"). */
  empty: string;
  initial: string;
  current: string;
}

/** Whether the item differs from where the tier began. */
export function changed(item: Item): boolean {
  return item.initial !== item.current;
}

/** The items that were changed, in the order of the form. */
export function changedItems(items: readonly Item[]): Item[] {
  return items.filter(changed);
}

function show(value: string, empty: string): string {
  return value === '' ? empty : value;
}

/** The line of the list: what it is now and what it was ("ステップ数: 30（元は 20）"). */
export function line(item: Item): string {
  return `${item.label}: ${show(item.current, item.empty)}（元は ${show(item.initial, item.empty)}）`;
}

/** The accessible name of the button that puts one item back. */
export function resetLabel(item: Item): string {
  return `${item.label}を元に戻す`;
}

/**
 * The detailed settings that the easy tier does not hold, and that the reader changed in the detail tier: the keys ("seed", "steps", "cfg", "sampler",
 * "scheduler", "exposed" for the values of the exposed inputs, and "gpt" for the settings of GPT Image 2.6) that go with the switch to the easy tier, so that
 * the form says they are gone (Issue #1000, #1096). The negative prompts, the LoRA and the strengths of the input images are kept by the easy tier and are not listed.
 */
export function droppedKeys(changed: readonly Item[], exposedSet: boolean, gptSet = false): string[] {
  const keys = changed.filter((item) => item.key.startsWith('adv:')).map((item) => item.key.slice('adv:'.length));
  if (exposedSet) keys.push('exposed');
  if (gptSet) keys.push('gpt');
  return keys;
}
