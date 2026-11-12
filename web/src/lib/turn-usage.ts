// What a finished exchange used (#5426, cindy's per-turn usage under a reply): every item of the exchange adds the tokens
// it carries (Item.usage, and a fold's for the work it stands for), summed by model. A finished subagent's report comes
// with an `id` and counts once however often it shows. On the first screen through lib/turns.ts: only the sums live
// here; the prices, the card's lines and the bar's text load with the reply bar (lib/turn-cost.ts).

import { bareModel } from "./model-change";
import type { Item, TokenUsage } from "./types";

/** One model's tokens in an exchange: `in` without the cache, `cache_write_1h` the part of `cache_write` kept an hour.
 *  `fast`: Claude Code's fast speed, apart from the same model's normal requests (it has no known price). `model` is
 *  the id without its wrappers (bareModel), "" when the transcript did not say. */
export interface ModelTokens {
  model: string;
  fast?: boolean;
  in: number;
  out: number;
  cache_read: number;
  cache_write: number;
  cache_write_1h: number;
}

export interface TurnUsage {
  /** In the order each model first used tokens in the exchange. */
  models: ModelTokens[];
  /** No earlier exchange of the loaded chat used any tokens: likely its first round, with nothing in the cache to read
   *  yet (a low hit rate says nothing then). */
  first?: boolean;
}

const KINDS = ["in", "out", "cache_read", "cache_write", "cache_write_1h"] as const;

/** An exchange's counts so far: entries without an `id` summed by model and speed; each report (`id`) by model and
 *  speed too, the largest count of each kind it was seen with. */
export interface UsageTally {
  sums: Map<string, ModelTokens>;
  reports: Map<string, ModelTokens>;
}

const num = (v: unknown) => (typeof v === "number" && Number.isFinite(v) && v > 0 ? v : 0);

function add(into: Map<string, ModelTokens>, key: string, model: string, fast: boolean, u: TokenUsage, max: boolean) {
  let m = into.get(key);
  if (!m) into.set(key, (m = { model, ...(fast ? { fast } : {}), in: 0, out: 0, cache_read: 0, cache_write: 0, cache_write_1h: 0 }));
  for (const k of KINDS) m[k] = max ? Math.max(m[k], num(u[k])) : m[k] + num(u[k]);
}

/** `tally` with an item's tokens added: its own (`usage`) and a fold's (`fold.usage`). */
export function tallyItem(tally: UsageTally | undefined, it: Item): UsageTally | undefined {
  for (const list of [it.usage, it.fold?.usage]) {
    if (!list?.length) continue;
    tally ??= { sums: new Map(), reports: new Map() };
    for (const u of list) {
      const model = u.model ? bareModel(u.model) : "";
      const key = `${model}\n${u.fast ? 1 : 0}`;
      if (u.id) add(tally.reports, `${u.id}\n${key}`, model, Boolean(u.fast), u, true);
      else add(tally.sums, key, model, Boolean(u.fast), u, false);
    }
  }
  return tally;
}

/** What an exchange used, by model; undefined when nothing of it carried tokens (an older node's transcript). */
export function turnUsage(tally: UsageTally | undefined): TurnUsage | undefined {
  if (!tally) return undefined;
  const models = new Map<string, ModelTokens>();
  for (const [key, m] of [...tally.sums, ...[...tally.reports].map(([k, r]) => [k.slice(k.indexOf("\n") + 1), r] as const)]) {
    const have = models.get(key);
    if (!have) models.set(key, { ...m });
    else for (const k of KINDS) have[k] += m[k];
  }
  const used = [...models.values()].filter((m) => KINDS.some((k) => m[k] > 0));
  return used.length ? { models: used } : undefined;
}

/** The usage as one string, what the reply's props carry: the same usage is the same string, so a finished reply is
 *  not drawn again when its exchange is worked out again (SegmentView's Answer is memo'd). */
export const usageKey = (u: TurnUsage | undefined): string | undefined => (u ? JSON.stringify(u) : undefined);
