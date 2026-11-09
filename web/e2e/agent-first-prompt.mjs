// Actual New agent and to-do handoff workflows. All API writes stay in synthetic fixtures.
// Usage: node web/e2e/agent-first-prompt.mjs <mockWebOrigin> <artifactDir>
import assert from "node:assert/strict";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { chromium } from "playwright-core";

const [address, artifacts, ...extra] = process.argv.slice(2);
assert.ok(address && artifacts && !extra.length, "Supply the mock web origin and artifact directory");
const base = new URL(address);
assert.ok(["127.0.0.1", "localhost", "[::1]"].includes(base.hostname), "Use a loopback mock server");
assert.ok(["http:", "https:"].includes(base.protocol) && !base.username && !base.password && base.pathname === "/" && !base.search && !base.hash, "Use an origin without credentials or a route");
assert.equal((await (await fetch(new URL("/api/v1/health", base), { signal: AbortSignal.timeout(5000) })).json()).version, "mock");
const output = resolve(artifacts);
await mkdir(output, { recursive: true });
const machine = { id: "example-computer", name: "Example computer", online: true };
const prompt = "Summarize the example project";
const todo = { id: "example-todo", title: "Review the example project", created_at: "2026-01-01T10:00:00Z", updated_at: "2026-01-01T10:00:00Z" };
const result = { status: "running", runs: [], screenshots: [] };
const browser = await chromium.launch({ channel: "chrome", headless: true });
const deadline = setTimeout(() => void browser.close(), 240_000);

async function open(lang, theme, mode = "inline", phone = false) {
  const labels = JSON.parse(await readFile(new URL(`../src/i18n/locales/${lang}/newagent.json`, import.meta.url)));
  const todos = JSON.parse(await readFile(new URL(`../src/i18n/locales/${lang}/todos.json`, import.meta.url)));
  const run = { lang, theme, mode, phone, writes: [], errors: [], blocked: [], mockHellos: 0 };
  result.runs.push(run);
  const context = await browser.newContext({ viewport: { width: phone ? 390 : 1280, height: 900 }, colorScheme: theme, serviceWorkers: "block" });
  await context.addInitScript(({ lang, theme }) => {
    localStorage.setItem("persona.token", "fixture-token");
    localStorage.setItem("persona.language", lang);
    localStorage.setItem("persona.theme", theme);
    localStorage.setItem("new.cwd.example-computer", "/code/example");
  }, { lang, theme });
  let started, first = "";
  const kind = mode === "claude" ? "claude" : mode === "legacy" ? "codex" : "traex";
  const launcher = mode === "legacy" ? "legacy-wrapper" : "example-wrapper";
  await context.route("**/*", async (route) => {
    const request = route.request(), url = new URL(request.url());
    if (url.origin !== base.origin) { run.blocked.push(url.pathname); return route.abort(); }
    if (!url.pathname.startsWith("/api/")) return route.continue();
    const path = url.pathname.replace(/^\/api\/v1/, "");
    if (path.endsWith("/worktree/status")) return route.fulfill({ json: { path: "/code/example", dirty: 0, unmerged: 0 } });
    if (!["GET", "HEAD"].includes(request.method())) {
      const body = request.postDataJSON(); run.writes.push({ path, body });
      if (path === "/sessions") {
        if (mode === "refused") return route.fulfill({ status: 409, json: { error: "Example launcher is unavailable", delivered: "none" } });
        if (mode === "unknown") return route.fulfill({ status: 502, json: { error: "Startup outcome unknown", delivered: "unknown" } });
        if (mode === "legacy" && body.async) return route.fulfill({ status: 409, json: { code: "codex_needs_prompt", error: "First message required", delivered: "none" } });
        first = body.prompt ?? "";
        started = { machine_id: machine.id, session_id: "example-started", title: "Example project", agent: body.launcher === "backup-claude" ? "claude" : kind, launcher: body.launcher, cwd: "/code/example", status: "idle" };
        return route.fulfill({ json: { session: started } });
      }
      if (path.endsWith("/messages")) { first = body.text; return route.fulfill({ json: { ok: true } }); }
      assert.fail(`Unexpected write ${path}`);
    }
    if (path.includes("/model-config/purposes/")) return route.fulfill({ status: 404, json: { error: "Not configured" } });
    if (path.endsWith("/launchers")) return route.fulfill({ json: { launchers: [{ name: launcher, ...(mode === "legacy" ? {} : { agent: kind }) }, { name: "backup-claude", agent: "claude" }] } });
    if (path.includes("/launchers/") && path.includes("/models")) return route.fulfill({ json: { agent: mode === "legacy" ? "claude" : kind, models: [] } });
    if (path.includes("/dirs")) return route.fulfill({ json: { path: "/code", entries: [{ name: "example", path: "/code/example" }] } });
    if (path.endsWith("/items")) return route.fulfill({ json: { items: first ? [{ id: "first-prompt", seq: 1, role: "user", kind: "text", text: first, at: "2026-01-01T10:00:00Z" }] : [], has_more: false, to_seq: first ? 1 : 0 } });
    const data = {
      "/sessions": { sessions: started ? [started] : [], total: started ? 1 : 0, has_more: false, revision: "fixture" },
      "/machines": { machines: [machine] }, "/interactions": { interactions: [] },
      "/assistant": { machine_id: machine.id, conversations: [] }, "/todos": { todos: [todo] },
      [`/todos/${todo.id}`]: { todo },
      "/mutes": { spaces: [], sessions: [], allowed: [] }, "/pins": { spaces: [], sessions: [], sections: [] },
      "/agents/sessions": { sessions: [] }, "/agents/prefs": {}, "/managers": { managers: [] },
      "/personas": { personas: [] }, "/assistant/personas": { personas: [] }, "/workspaces": { workspaces: [] }, "/extensions": { extensions: [] },
    };
    return route.fulfill({ json: data[path] ?? {} });
  });
  await context.routeWebSocket(/\/api\/v1\/ws(?:\?|$)/, (socket) => {
    const upstream = socket.connectToServer();
    upstream.onMessage((message) => {
      const event = JSON.parse(String(message));
      if (event.type !== "hello") return;
      assert.equal(event.server_version, "mock"); run.mockHellos++;
      socket.send(JSON.stringify({ type: "hello", server_version: "mock", protocol: 1, features: [], hub_name: "Example hub" }));
      socket.send(JSON.stringify({ type: "machines", machines: [machine] }));
    });
    socket.onMessage((message) => { const event = JSON.parse(String(message)); if (event.type === "app_ping") socket.send(JSON.stringify({ type: "pong", id: event.id })); });
  });
  const page = await context.newPage(); page.setDefaultTimeout(10_000);
  page.on("pageerror", (e) => run.errors.push(e.message));
  page.on("console", (m) => { if (m.type() === "error" && !/Failed to load resource.*(?:404|409|502)/.test(m.text())) run.errors.push(m.text()); });
  const shot = async (state) => {
    await page.evaluate(() => document.fonts.ready); await page.waitForTimeout(350);
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), "No horizontal overflow");
    assert.equal(await page.locator("vite-error-overlay").count(), 0);
    const file = `${lang}-${theme}-${mode}${phone ? "-phone" : ""}-${state}.png`;
    await page.screenshot({ path: join(output, file) }); result.screenshots.push(file);
  };
  await page.goto(new URL("/agents", base).href);
  await page.locator("main").getByRole("button", { name: labels.title, exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button").filter({ hasText: launcher }).click();
  await dialog.locator("textarea").fill(prompt);
  return { context, page, dialog, run, labels, todos, shot };
}

async function finish(x) {
  assert.ok(x.run.mockHellos > 0, "Connected only to the confirmed mock hub");
  assert.deepEqual(x.run.errors, []); assert.deepEqual(x.run.blocked, []);
  await x.context.close();
}

try {
  for (const lang of ["en", "zh-Hans", "ja"]) for (const theme of ["light", "dark"]) {
    const x = await open(lang, theme);
    await x.shot("picker");
    await x.dialog.getByRole("button", { name: x.labels.start, exact: true }).click();
    await x.page.waitForURL("**/s/example-computer/example-started");
    await x.page.locator("main").getByText(prompt, { exact: true }).waitFor();
    assert.deepEqual(x.run.writes, [{ path: "/sessions", body: { machine_id: machine.id, cwd: "/code/example", launcher: "example-wrapper", agent_type: "traex", prompt } }]);
    assert.equal(await x.page.locator("main").getByText(prompt, { exact: true }).count(), 1);
    await x.shot("conversation"); await finish(x);
  }
  for (const mode of ["claude", "legacy", "refused", "unknown"]) {
    const x = await open("en", "light", mode);
    await x.dialog.getByRole("button", { name: x.labels.start, exact: true }).click();
    if (mode === "refused" || mode === "unknown") {
      await x.page.getByText(mode === "refused" ? /Example launcher is unavailable/ : /Startup outcome unknown/).waitFor();
      await x.page.waitForTimeout(500);
      assert.equal(x.run.writes.length, 1); await x.shot("failed");
    } else {
      await x.page.waitForURL("**/s/example-computer/example-started");
      await x.page.locator("main").getByText(prompt, { exact: true }).waitFor();
      assert.equal(x.run.writes.length, 2);
      assert.equal(x.run.writes[0].body.async, true);
      if (mode === "claude") assert.equal(x.run.writes[1].body.text, prompt);
      else { assert.equal(x.run.writes[1].path, "/sessions"); assert.equal(x.run.writes[1].body.prompt, prompt); assert.ok(!x.run.writes[1].body.async); }
    }
    await finish(x);
  }
  {
    const x = await open("en", "light", "attachments");
    await x.dialog.locator('input[type="file"]').setInputFiles({ name: "example.txt", mimeType: "text/plain", buffer: Buffer.from("Example attachment") });
    await x.dialog.getByRole("button", { name: x.labels.start, exact: true }).click();
    await x.page.locator("main").getByText(/This launcher can't take attachments in the first message/).waitFor();
    assert.equal(x.run.writes.length, 0);
    await x.page.locator("main").getByText("example.txt", { exact: true }).waitFor();
    await x.shot("retained");
    await x.page.getByRole("button", { name: "Remove example.txt", exact: true }).click();
    await x.page.locator("main input").fill("backup-claude");
    await x.page.getByRole("button", { name: "Retry", exact: true }).click();
    await x.page.waitForURL("**/s/example-computer/example-started");
    assert.equal(x.run.writes.length, 2);
    assert.equal(x.run.writes[0].body.launcher, "backup-claude");
    assert.equal(x.run.writes[0].body.async, true);
    assert.equal(x.run.writes[0].body.agent_type, undefined);
    assert.equal(x.run.writes[1].body.text, prompt);
    await finish(x);
  }
  {
    const x = await open("en", "dark", "handoff");
    await x.page.keyboard.press("Escape");
    await x.page.goto(new URL(`/todos/${todo.id}`, base).href);
    await x.page.getByRole("button", { name: x.todos.hand_over, exact: true }).click();
    const handoff = x.page.getByRole("dialog");
    await handoff.locator('select option[value="example-wrapper"]').waitFor({ state: "attached" });
    await handoff.locator("textarea").fill(prompt);
    await x.shot("picker");
    await handoff.getByRole("button", { name: x.todos.launch_and_hand, exact: true }).click();
    await x.page.waitForURL("**/s/example-computer/example-started");
    assert.equal(x.run.writes.length, 1);
    assert.equal(x.run.writes[0].body.agent_type, "traex");
    assert.equal(x.run.writes[0].body.todo_id, todo.id);
    assert.ok(x.run.writes[0].body.prompt.includes(todo.title) && x.run.writes[0].body.prompt.includes(prompt));
    assert.ok(!x.run.writes[0].body.async);
    await finish(x);
  }
  const x = await open("en", "dark", "inline", true);
  await x.dialog.getByRole("button", { name: x.labels.start, exact: true }).click();
  await x.page.waitForURL("**/s/example-computer/example-started");
  await x.page.locator("main").getByText(prompt, { exact: true }).waitFor();
  await x.shot("conversation"); await finish(x);
  result.status = "pass";
} catch (error) {
  result.status = "fail"; result.error = String(error); throw error;
} finally {
  clearTimeout(deadline); await browser.close();
  await writeFile(join(output, "result.json"), JSON.stringify(result, null, 2));
}
console.log(`PASS agent first prompt: ${result.runs.length} workflows`);
