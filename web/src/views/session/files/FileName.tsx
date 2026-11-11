import { t } from "../../../i18n";

// Make only boundary whitespace visible. The original name remains the identity used by all file operations.
const invisible = (ch: string) => /[\p{White_Space}\u200b\ufeff]/u.test(ch);
const boundaryWhitespace = /^[\p{White_Space}\u200b\ufeff]|[\p{White_Space}\u200b\ufeff]$/u;

function whitespaceName(ch: string): string {
  switch (ch) {
    case " ": return t("files-name.space");
    case "\u00a0": return t("files-name.nbsp");
    case "\t": return t("files-name.tab");
    case "\n": return t("files-name.newline");
    case "\r": return t("files-name.return");
    default: return `U+${ch.codePointAt(0)!.toString(16).toUpperCase().padStart(4, "0")}`;
  }
}

function markers(chars: string[]): string {
  if (!chars.length) return "";
  const runs: string[] = [];
  for (let i = 0; i < chars.length;) {
    let end = i + 1;
    while (end < chars.length && chars[end] === chars[i]) end++;
    const count = end - i;
    runs.push(whitespaceName(chars[i]) + (count > 1 ? ` ×${count}` : ""));
    i = end;
  }
  return `[${runs.join(", ")}]`;
}

function parts(name: string): { leading: string; body: string; trailing: string } | null {
  if (!boundaryWhitespace.test(name)) return null;
  const chars = Array.from(name);
  let start = 0, end = chars.length;
  while (start < end && invisible(chars[start])) start++;
  while (end > start && invisible(chars[end - 1])) end--;
  return { leading: markers(chars.slice(0, start)), body: chars.slice(start, end).join(""), trailing: markers(chars.slice(end)) };
}

/** Display and speak boundary characters while keeping the caller's original name for actions. */
export function displayFileName(name: string): string {
  const p = parts(name);
  return p ? p.leading + p.body + p.trailing : name;
}

export function spokenFileName(name: string): string {
  const p = parts(name);
  if (!p) return name;
  if (!p.body) return t("files-name.only", { marks: p.leading });
  if (p.leading && p.trailing) return t("files-name.both", { name: p.body, leading: p.leading, trailing: p.trailing });
  return p.leading ? t("files-name.leading", { name: p.body, marks: p.leading }) : t("files-name.trailing", { name: p.body, marks: p.trailing });
}

export function displayFilePath(path: string): string {
  return path.split("/").map(displayFileName).join("/");
}

export function FileName({ name, className }: { name: string; className?: string }) {
  const p = parts(name);
  if (!p) return <span className={className}>{name}</span>;
  return (
    <span className={`${className ?? ""} inline-flex min-w-0`}>
      <span aria-hidden="true" className="min-w-0 truncate rounded bg-field">{p.leading}</span>
      <span aria-hidden="true" className="min-w-0 truncate">{p.body}</span>
      <span aria-hidden="true" className="min-w-0 truncate rounded bg-field">{p.trailing}</span>
      <span className="sr-only">{spokenFileName(name)}</span>
    </span>
  );
}
