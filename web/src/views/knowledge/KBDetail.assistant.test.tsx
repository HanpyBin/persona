// @vitest-environment jsdom
// A knowledge entry opened while another assistant than the default is current (docs/assistant/managers.md "Clients"):
// its address names no assistant, so it is read from the current one, and everything its page writes afterwards (tags,
// confirmed facts) goes to the same one; another made current opens the entry again from that one.
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { initI18n, t } from "../../i18n";
import { setToken } from "../../lib/api";
import type { KBDetailResponse } from "../../lib/kb";
import { chooseManager, resetManagerChoice, setRosterDefault } from "../../lib/manager-choice";
import { EntryDetail } from "./KBDetail";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
type Request = { url: string; init: RequestInit; resolve: (value: Response) => void };
let requests: Request[];
let host: HTMLDivElement;
let root: Root;
const detail: KBDetailResponse = { detail: { kind: "people", id: "example", path: "", meta: { name: "Example entry", tags: ["existing"], importance: "P1" }, summary: "# Example entry" } };
const render = () => act(() => root.render(<EntryDetail kind="people" id="example" reloadKey={0} onToggleSidebar={() => {}} onBack={() => {}} onOpen={() => {}} onSaved={() => {}} />));
const reply = (r: Request, value: unknown) => act(async () => r.resolve(new Response(JSON.stringify(value), { status: 200, headers: { "Content-Type": "application/json" } })));
const named = (r: Request) => new Headers(r.init.headers).get("X-Persona-Manager");
const button = (label: string) => [...document.body.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === label)!;
const reads = () => requests.filter((r) => !r.init.method && r.url === "/api/v1/kb/people/example");
const writes = () => requests.filter((r) => r.init.method === "PUT");

beforeEach(async () => {
  await initI18n("en");
  localStorage.clear();
  resetManagerChoice();
  setToken("synthetic-owner-a");
  setRosterDefault("assistant");
  requests = [];
  vi.stubGlobal("fetch", vi.fn((url: string, init: RequestInit) => new Promise<Response>((resolve) => requests.push({ url, init, resolve }))));
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
});
afterEach(async () => {
  await act(async () => root.unmount());
  host.remove();
  resetManagerChoice();
  vi.unstubAllGlobals();
});

it("reads the entry from the current assistant and saves its tags and facts to that one", async () => {
  act(() => chooseManager("m-work"));
  render();
  expect(reads().map(named)).toEqual(["m-work"]);
  await reply(reads()[0], detail);
  act(() => button("Tag").click());
  await act(async () => button("Save").click());
  expect(writes().map(named)).toEqual(["m-work"]);
  await reply(writes()[0], { id: "example" });
  act(() => button("Add").click());
  const text = document.body.querySelector<HTMLTextAreaElement>("textarea")!;
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value")!.set!.call(text, "Confirmed.");
    text.dispatchEvent(new Event("input", { bubbles: true }));
  });
  act(() => button("Save").click());
  expect(writes().map(named)).toEqual(["m-work", "m-work"]);
});

it("opens the entry again from the assistant made current, writing nothing to the one before", async () => {
  act(() => chooseManager("m-work"));
  render();
  await reply(reads()[0], detail);
  // the kept pick left the roster (or another was picked): the page is the default's now
  act(() => chooseManager(null));
  expect(reads().map(named)).toEqual(["m-work", "assistant"]);
  await reply(reads()[1], detail);
  act(() => button("Tag").click());
  await act(async () => button("Save").click());
  expect(writes().map(named)).toEqual(["assistant"]);
});

// A coded refusal is terminal; a bare gateway 503 would leave automatic GET retries pending.
const refuseKnowledge = (request: Request) => act(async () => request.resolve(new Response(JSON.stringify({ code: "kb_unavailable", error: "Synthetic knowledge unavailable" }), { status: 503, headers: { "Content-Type": "application/json" } })));

it("shows an initial read failure and recovers with a GET-only retry (#5048)", async () => {
  render();
  await refuseKnowledge(reads()[0]);
  expect(host.textContent).toContain(t("lib-api.knowledge_unavailable"));
  expect(reads()).toHaveLength(1);
  act(() => button("Retry").click());
  expect(reads()).toHaveLength(2);
  await reply(reads()[1], detail);
  expect(host.textContent).toContain("Example entry");
  expect(host.textContent).not.toContain(t("lib-api.knowledge_unavailable"));
  expect(button("Retry")).toBeUndefined();
  expect(writes()).toHaveLength(0);
});

it("retains detail beside a failed tag readback, then retries without saving again (#5048)", async () => {
  act(() => chooseManager("m-work"));
  let reloadKey = 0;
  const draw = () => root.render(<EntryDetail kind="people" id="example" reloadKey={reloadKey} onToggleSidebar={() => {}} onBack={() => {}} onOpen={() => {}} onSaved={() => { reloadKey++; draw(); }} />);
  act(draw);
  await reply(reads()[0], detail);
  act(() => button("Tag").click());
  const tags = document.body.querySelector<HTMLInputElement>('[role=dialog] input[aria-label="Tags"]')!;
  act(() => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!.call(tags, "updated");
    tags.dispatchEvent(new Event("input", { bubbles: true }));
  });
  act(() => button("Save").click());
  expect(JSON.parse(writes()[0].init.body as string).tags).toEqual(["updated"]);
  await reply(writes()[0], { id: "example" });
  expect(reads()).toHaveLength(2);
  await refuseKnowledge(reads()[1]);
  expect(host.textContent).toContain("Example entry");
  expect(host.textContent).toContain("#existing");
  expect(host.textContent).toContain(t("lib-api.knowledge_unavailable"));
  act(() => button("Retry").click());
  expect(reads()).toHaveLength(3);
  expect(writes()).toHaveLength(1);
  expect(reads().map(named)).toEqual(["m-work", "m-work", "m-work"]);
  await reply(reads()[2], { detail: { ...detail.detail, meta: { ...detail.detail.meta, tags: ["updated"] } } });
  expect(host.textContent).toContain("#updated");
  expect(host.textContent).not.toContain("#existing");
  expect(host.textContent).not.toContain(t("lib-api.knowledge_unavailable"));
  expect(button("Retry")).toBeUndefined();
  expect(writes()).toHaveLength(1);
});

it.each(["manager", "credential"])("ignores a retired read failure after changing %s (#5048)", async (change) => {
  act(() => chooseManager("m-work"));
  render();
  const retired = reads()[0];
  act(() => { if (change === "manager") chooseManager(null); else setToken("synthetic-owner-b"); });
  expect(retired.init.signal?.aborted).toBe(true);
  expect(reads()).toHaveLength(2);
  await reply(reads()[1], { detail: { ...detail.detail, meta: { ...detail.detail.meta, name: "Current entry" } } });
  await refuseKnowledge(retired);
  expect(host.textContent).toContain("Current entry");
  expect(host.textContent).not.toContain(t("lib-api.knowledge_unavailable"));
  expect(button("Retry")).toBeUndefined();
  expect(writes()).toHaveLength(0);
});
