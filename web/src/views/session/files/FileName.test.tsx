// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { initI18n } from "../../../i18n";
import { displayFileName, FileName, spokenFileName } from "./FileName";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

beforeAll(async () => { await initI18n("en"); });
afterAll(async () => { await initI18n("zh-Hans"); });

describe("file name whitespace", () => {
  it("shows and speaks exact boundary kinds and runs, leaving ordinary and internal names alone", () => {
    expect(displayFileName("report")).toBe("report");
    expect(spokenFileName("report")).toBe("report");
    expect(displayFileName("report ")).toBe("report[space]");
    expect(displayFileName("report\u00a0")).toBe("report[non-breaking space]");
    expect(displayFileName("  report\t")).toBe("[space ×2]report[tab]");
    expect(displayFileName(" \u00a0")).toBe("[space, non-breaking space]");
    expect(displayFileName("report\u200b")).toBe("report[U+200B]");
    expect(displayFileName("a b")).toBe("a b");
    expect(spokenFileName("report ")).toBe("report, trailing whitespace [space]");
    expect(spokenFileName(" report ")).toBe("report, leading whitespace [space], trailing whitespace [space]");
    expect(spokenFileName(" ")).toBe("Whitespace-only name [space]");
  });

  it("exposes a distinct spoken label and includes the marker in the visual text", () => {
    const host = document.createElement("div");
    const root = createRoot(host);
    act(() => root.render(<button><FileName name="report\u00a0" className="truncate" /></button>));
    const visual = [...host.querySelectorAll('[aria-hidden="true"]')].map((n) => n.textContent).join("");
    expect(visual).toBe("report[non-breaking space]");
    expect(host.querySelector(".sr-only")?.textContent).toBe("report, trailing whitespace [non-breaking space]");
    expect(host.querySelectorAll('[aria-hidden="true"]')).toHaveLength(3);
    act(() => root.unmount());
  });

  it.each([
    ["en", "space", "trailing whitespace"],
    ["zh-Hans", "空格", "末尾空白"],
    ["ja", "スペース", "末尾の空白"],
  ])("localizes visual and spoken markers in %s", async (lang, marker, direction) => {
    await initI18n(lang);
    expect(displayFileName("report ")).toContain(marker);
    expect(spokenFileName("report ")).toContain(direction);
  });
});
