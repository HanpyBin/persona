// What a finished exchange used (#5426, cindy's per-turn usage under a reply): the tokens every item of the exchange
// carries (Item.usage, and a fold's for the work it stands for), collected as they are. On the first screen through
// lib/turns.ts, so only that: the sums by model, the prices, the card's lines and the bar's text load with the reply bar
// (lib/turn-cost.ts).

import type { Item, TokenUsage } from "./types";

export interface TurnUsage {
  /** What the exchange's items carry, in order (a subagent's report may be there more than once: it counts once). */
  entries: TokenUsage[];
  /** No earlier exchange of the loaded chat used any tokens: likely its first round, with nothing in the cache to read
   *  yet (a low hit rate says nothing then). */
  first?: boolean;
}

/** Adds what `it` carries, its own and its fold's, to an exchange's usage. */
export function countUsage(to: { usage?: TurnUsage }, it: Item) {
  for (const list of [it.usage, it.fold?.usage]) if (list?.length) (to.usage ??= { entries: [] }).entries.push(...list);
}

/** Marks the first of `segs` that used tokens as the loaded chat's first round. */
export function markFirstUsage(segs: { usage?: TurnUsage }[]) {
  const first = segs.find((s) => s.usage)?.usage;
  if (first) first.first = true;
}

/** The usage as one string, what the reply's props carry: the same usage is the same string, so a finished reply is
 *  not drawn again when its exchange is worked out again (SegmentView's Answer is memo'd). */
export const usageKey = (u: TurnUsage | undefined): string | undefined => (u ? JSON.stringify(u) : undefined);
