// Claude Code asking on its own terminal to run a shell command (its permission prompt, or its safety check in bypass
// mode), as the hub reads it off the screen (#5265, #5266): what the screen card says of it in plain words. The same
// rules as the iPhone's CommandAsk (apple/Shared/CommandAsk.swift).
import { t } from "../i18n";
import type { DialogOption, ScreenDialog } from "./types";

export interface CommandAsk {
  command: string;
  /** The agent's own words for it. */
  description?: string;
  /** The boxed warning as Claude Code words it, and its countdown sentence: under "Show original". */
  original?: string;
  /** The warning in plain words; none without a warning. */
  riskLine?: string;
  /** When the countdown runs out (ms, the hub's clock), and whether it lets the request through then. */
  deadlineAt?: number;
  deadlineAllows: boolean;
  /** Longer than FOLDED_LINES rows at a phone's width, by a rough count: folded until "Show all". */
  long: boolean;
}

/** The rows a long command shows until "Show all"; about how many characters of code fit a row on a phone. */
export const FOLDED_LINES = 4;
const ROW_CHARS = 38;

/** The command prompt of a screen card's dialog; null for any other dialog, and from a hub before #5265. */
export function commandAsk(d: ScreenDialog | undefined): CommandAsk | null {
  const command = d?.command?.trim();
  if (!d || !command) return null;
  const warning = d.warning?.trim();
  const at = d.deadline_at ? Date.parse(d.deadline_at) : NaN;
  const rows = command.split("\n").reduce((n, row) => n + Math.max(1, Math.ceil(row.length / ROW_CHARS)), 0);
  const original = [warning, d.deadline?.trim()].filter(Boolean).join("\n");
  return {
    command,
    description: d.description?.trim() || undefined,
    original: original || undefined,
    riskLine: warning ? t(d.risk === "delete" ? "chat-interactions.risk_delete" : "chat-interactions.risk_other") : undefined,
    deadlineAt: Number.isFinite(at) ? at : undefined,
    deadlineAllows: d.deadline_action === "allow",
    long: rows > FOLDED_LINES,
  };
}

/** The prompt's Yes and No, for the card's Allow and Deny: only a command prompt with just those two choices. One with
 *  more ("Yes, and don't ask again for …") keeps its own labels. */
export function allowDeny(d: ScreenDialog | undefined): { allow: DialogOption; deny: DialogOption } | null {
  if (!commandAsk(d) || d?.multi) return null;
  const options = d?.options ?? [];
  if (options.length !== 2 || options.some((o) => o.kind)) return null;
  const words = options.map((o) => o.label.trim().toLowerCase());
  return words[0] === "yes" && words[1] === "no" ? { allow: options[0], deny: options[1] } : null;
}

/** The countdown's line at `now` ("Denied automatically in 1:44"), or once it ran out what happens; null without one. */
export function countdownLine(ask: CommandAsk, now: number): string | null {
  if (ask.deadlineAt === undefined) return null;
  const left = Math.ceil((ask.deadlineAt - now) / 1000);
  if (left <= 0) return t(ask.deadlineAllows ? "chat-interactions.countdown_over_allow" : "chat-interactions.countdown_over_deny");
  const time = `${Math.floor(left / 60)}:${String(left % 60).padStart(2, "0")}`;
  return t(ask.deadlineAllows ? "chat-interactions.countdown_allow" : "chat-interactions.countdown_deny", { time });
}
