// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import { getState } from "../lib/store";
import { deliverLaunch, type AgentLaunch } from "../lib/new-agent";
import { StartingAgent } from "./StartingAgent";
import { t } from "../i18n";

vi.mock("../lib/new-agent", async (original) => ({ ...(await original<typeof import("../lib/new-agent")>()), deliverLaunch: vi.fn() }));
(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
const previous = { ...getState() };
afterEach(() => { Object.assign(getState(), previous); vi.clearAllMocks(); });

it.each([{ replace: false }, { replace: true }, { replace: false, repeatOnly: true }])("keeps the captured kind unless the user can replace the launcher: %j", async ({ replace, repeatOnly }) => {
  const launch: AgentLaunch = { id: "local-example", machineId: "example-computer", cwd: "/code/example", launcher: "example-wrapper", agentType: "traex", model: "example-model", effort: "high", text: "Inspect the example", attachments: [], uploaded: {}, phase: "failed", error: "Start refused", repeatOnly,
    startOpId: "00000000-0000-4000-8000-000000000021", startPromptOpId: "00000000-0000-4000-8000-000000000022", messageId: "00000000-0000-4000-8000-000000000023" };
  Object.assign(getState(), { launches: { [launch.id]: launch }, machines: [] });
  const host = document.createElement("div"); document.body.append(host);
  const root = createRoot(host);
  try {
    await act(async () => root.render(<StartingAgent launch={launch} />));
    if (repeatOnly) expect(host.querySelector("input")).toBeNull();
    if (replace) await act(async () => {
      const input = host.querySelector("input")!;
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(input, "claude");
      input.dispatchEvent(new Event("input", { bubbles: true }));
    });
    const retry = [...host.querySelectorAll("button")].find((button) => button.textContent === t("startingagent.retry"))!;
    await act(async () => retry.click());
    const submitted = getState().launches[launch.id];
    expect(submitted.launcher).toBe(replace ? "claude" : launch.launcher);
    expect(submitted.agentType).toBe(replace ? undefined : "traex");
    expect(submitted.model).toBe(replace ? undefined : launch.model);
    expect(submitted.startOpId).toBe(launch.startOpId);
    expect(deliverLaunch).toHaveBeenCalledWith(launch.id, undefined);
  } finally {
    await act(async () => root.unmount()); host.remove();
  }
});
