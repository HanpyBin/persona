// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { execFileSync } from "node:child_process";
import { appendFileSync, mkdtempSync, readFileSync, renameSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import JSZip from "jszip";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
const fsLive = vi.hoisted(() => ({ notify: undefined as ((change: { git: boolean }) => void) | undefined }));
vi.mock("../../lib/use-fs-live", () => ({ useFsLive: (_mid: string, _cwd: string, _paths: string[], _git: boolean, notify: (change: { git: boolean }) => void) => { fsLive.notify = notify; } }));
const documents = vi.hoisted(() => ({ reads: [] as Promise<unknown>[], gate: undefined as Promise<void> | undefined }));
vi.mock("../../lib/compare", async (original) => {
  const actual = await original<typeof import("../../lib/compare")>();
  return { ...actual, docxUnits: (...args: Parameters<typeof actual.docxUnits>) => {
    const read = actual.docxUnits(...args);
    documents.reads.push(read);
    return documents.gate ? Promise.all([read, documents.gate]).then(([units]) => units) : read;
  } };
});
import { getToken, setToken } from "../../lib/api";
import { chosenManager, chooseManager } from "../../lib/manager-choice";
import { getState } from "../../lib/store";
import { t } from "../../i18n";
import { GitPanel } from "./GitPanel";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

type File = { path: string; orig_path?: string; index: string; worktree: string; added: number; deleted: number; binary: boolean };
type Request = { rev: string; path: string };
let repo: string;
let host: HTMLDivElement;
let root: Root;
let requested: Request[];
let downloads: string[];
let oldToken: string;
let oldManager: string | null;
let features: string[];
let hideOriginal: boolean;
let deny: boolean;
let hold: ((request: Request) => Promise<Response> | undefined) | undefined;
let statusOverride: ((files: File[]) => File[]) | undefined;
const cached = new Map<string, Buffer>();

function git(...args: string[]): string {
  return execFileSync("git", args, { cwd: repo, encoding: "utf8", env: { ...process.env, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null" }, stdio: ["ignore", "pipe", "ignore"] });
}
function gitBytes(...args: string[]): Buffer {
  return execFileSync("git", args, { cwd: repo, env: { ...process.env, GIT_CONFIG_NOSYSTEM: "1", GIT_CONFIG_GLOBAL: "/dev/null" }, stdio: ["ignore", "pipe", "ignore"] });
}
function files(): File[] {
  const records = git("status", "--porcelain=v2", "-z").split("\0");
  const result: File[] = [];
  for (let i = 0; i < records.length; i++) {
    const fields = records[i]!.split(" ");
    if (fields[0] === "2") result.push({ path: fields.slice(9).join(" "), ...(hideOriginal ? {} : { orig_path: records[i + 1] }), index: fields[1]![0]!, worktree: fields[1]![1]!, added: 0, deleted: 0, binary: true }), i++;
    else if (fields[0] === "1") result.push({ path: fields.slice(8).join(" "), index: fields[1]![0]!, worktree: fields[1]![1]!, added: 0, deleted: 0, binary: true });
  }
  return result;
}
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
function blobAnswer({ rev, path }: Request): Response {
  if (deny && rev === "HEAD" && path.startsWith("old")) return json({ error: "Synthetic path refused" }, 403);
  try {
    const bytes = gitBytes("cat-file", "blob", `${rev === "INDEX" ? "" : rev}:${path}`);
    const cachePath = `/synthetic/${rev}/${path}`;
    cached.set(cachePath, bytes);
    return json({ path: cachePath, size: bytes.length });
  } catch {
    return json({ missing: true });
  }
}
function stub() {
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
    const u = new URL(String(input), "http://local.invalid");
    if (u.pathname.endsWith("/git/status")) return json({ is_repo: true, root: repo, branch: "main", files: statusOverride?.(files()) ?? files(), commits: [] });
    if (u.pathname.endsWith("/git/diff")) {
      const path = u.searchParams.get("path")!;
      return json({ path, diff: git("diff", ...(u.searchParams.get("staged") === "1" ? ["--cached"] : []), "--", path), truncated: false });
    }
    if (u.pathname.endsWith("/git/blob")) {
      const request = { rev: u.searchParams.get("rev")!, path: u.searchParams.get("path")! };
      requested.push(request);
      return hold?.(request) ?? blobAnswer(request);
    }
    if (u.pathname.endsWith("/file/download")) {
      const path = u.searchParams.get("path")!;
      downloads.push(path);
      const bytes = cached.get(path) ?? readFileSync(path);
      return new Response(new Uint8Array(bytes));
    }
    throw new Error(`Unexpected synthetic request: ${u.pathname}`);
  }));
}
function writeBase(path: string, bytes: string | Buffer) {
  writeFileSync(join(repo, path), bytes);
  git("add", "--", path);
  git("commit", "-qm", "base");
}
function button(name: string) {
  return [...host.querySelectorAll("button")].find((b) => b.textContent?.trim() === name || b.getAttribute("aria-label") === name);
}
async function click(name: string) {
  const found = button(name);
  expect(found, name).toBeTruthy();
  await act(async () => { found!.click(); });
}
async function show(name: string) {
  await act(async () => { root.render(<GitPanel mid="computer" sid="session" cwd={repo} onClose={() => {}} />); });
  const row = [...host.querySelectorAll("button")].find((b) => b.title === name);
  expect(row, name).toBeTruthy();
  await act(async () => { row!.click(); });
}
async function docx(text: string) {
  const zip = new JSZip();
  zip.file("word/document.xml", `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>${text}</w:t></w:r></w:p></w:body></w:document>`);
  return zip.generateAsync({ type: "nodebuffer", compression: "STORE" });
}
function holdDocuments() {
  let release!: () => void;
  documents.reads = [];
  documents.gate = new Promise<void>((resolve) => { release = resolve; });
  return async () => {
    await vi.waitFor(() => expect(documents.reads).toHaveLength(2));
    await act(async () => { release(); await Promise.all(documents.reads); });
    documents.gate = undefined;
  };
}

beforeEach(() => {
  repo = mkdtempSync(join(tmpdir(), "persona-rename-compare-"));
  git("init", "-q", "--initial-branch=main");
  git("config", "user.name", "Sample Dev");
  git("config", "user.email", "dev@example.com");
  requested = []; downloads = []; cached.clear(); hideOriginal = false; deny = false; hold = undefined; statusOverride = undefined; fsLive.notify = undefined;
  documents.reads = []; documents.gate = undefined;
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  oldToken = getToken(); oldManager = chosenManager(); features = getState().features;
  Object.assign(getState(), { features: [] });
  host = document.createElement("div"); document.body.append(host); root = createRoot(host);
});
afterEach(() => {
  act(() => root.unmount()); host.remove();
  Object.assign(getState(), { features });
  setToken(oldToken); chooseManager(oldManager);
  vi.unstubAllGlobals();
  rmSync(repo, { recursive: true, force: true });
});

describe("Git panel renamed version comparisons", () => {
  it("reads a staged image rename chain from the original HEAD path and new INDEX path, including encoded names", async () => {
    writeBase("old & a.png", "synthetic image bytes");
    git("mv", "old & a.png", "middle.png"); git("mv", "middle.png", "new # b.png"); stub();
    expect(files()).toEqual([expect.objectContaining({ path: "new # b.png", orig_path: "old & a.png", index: "R", worktree: "." })]);
    await show("new # b.png");
    expect(requested).toEqual([{ rev: "HEAD", path: "old & a.png" }, { rev: "INDEX", path: "new # b.png" }]);
    expect(host.querySelectorAll("img")).toHaveLength(2);
    expect(host.textContent).not.toContain(t("session-files-compareview.not_in_version"));
  });

  it("uses the new index path for unstaged edits, then the old HEAD path for the staged rename", async () => {
    writeBase("old.png", "synthetic image bytes"); git("mv", "old.png", "new.png"); appendFileSync(join(repo, "new.png"), " worktree"); stub();
    expect(files()).toEqual([expect.objectContaining({ path: "new.png", orig_path: "old.png", index: "R", worktree: "M" })]);
    await show("new.png");
    expect(requested).toEqual([{ rev: "INDEX", path: "new.png" }]);
    await click(t("session-gitpanel.staged"));
    expect(requested.slice(1)).toEqual([{ rev: "HEAD", path: "old.png" }, { rev: "INDEX", path: "new.png" }]);
    await click(t("session-gitpanel.unstaged"));
    expect(requested.at(-1)).toEqual({ rev: "INDEX", path: "new.png" });
  });

  it("compares both records of an actual staged then unstaged rename chain", async () => {
    writeBase("old.png", "synthetic image bytes");
    git("mv", "old.png", "index.png");
    renameSync(join(repo, "index.png"), join(repo, "worktree.png"));
    git("add", "-N", "worktree.png"); stub();
    expect(files()).toEqual(expect.arrayContaining([
      expect.objectContaining({ path: "index.png", orig_path: "old.png", index: "R", worktree: "." }),
      expect.objectContaining({ path: "worktree.png", orig_path: "index.png", index: ".", worktree: "R" }),
    ]));
    await show("worktree.png");
    expect(requested).toEqual([{ rev: "INDEX", path: "index.png" }]);
    expect(host.querySelectorAll("img")).toHaveLength(2);
    await click(t("session-files-parts.back"));
    const staged = [...host.querySelectorAll("button")].find((b) => b.title === "index.png");
    expect(staged).toBeTruthy();
    await act(async () => { staged!.click(); });
    expect(requested.slice(1)).toEqual([{ rev: "HEAD", path: "old.png" }, { rev: "INDEX", path: "index.png" }]);
    expect(host.querySelectorAll("img")).toHaveLength(2);
  });

  it("does not guess an intermediate path when both status columns report a rename", async () => {
    writeBase("old.png", "synthetic image bytes"); git("mv", "old.png", "new.png");
    statusOverride = (entries) => entries.map((file) => ({ ...file, worktree: "R" })); stub();
    await show("new.png");
    expect(host.textContent).toContain(t("session-files-compareview.rename_path_unavailable"));
    expect(requested).toEqual([]);
    await click(t("session-gitpanel.staged"));
    expect(host.textContent).toContain(t("session-files-compareview.rename_path_unavailable"));
    expect(requested).toEqual([]);
  });

  it("keeps an ordinary staged image on the same path", async () => {
    writeBase("photo.png", "synthetic image bytes"); appendFileSync(join(repo, "photo.png"), " edited"); git("add", "photo.png"); stub();
    await show("photo.png");
    expect(requested).toEqual([{ rev: "HEAD", path: "photo.png" }, { rev: "INDEX", path: "photo.png" }]);
    expect(host.querySelectorAll("img")).toHaveLength(2);
  });

  it("keeps ordinary text diffs on the text renderer", async () => {
    writeBase("notes.txt", "Original line\n"); appendFileSync(join(repo, "notes.txt"), "Added line\n"); git("add", "notes.txt"); stub();
    await show("notes.txt");
    expect(host.querySelector('[data-testid="diff"]')?.textContent).toContain("Added line");
    expect(requested).toEqual([]);
  });

  it("compares actual document text across a rename chain and a later worktree edit", async () => {
    writeBase("old.docx", await docx("Original paragraph"));
    git("mv", "old.docx", "middle.docx"); git("mv", "middle.docx", "new.docx");
    writeFileSync(join(repo, "new.docx"), await docx("Changed paragraph")); stub();
    let releaseDocuments = holdDocuments();
    await show("new.docx");
    await releaseDocuments();
    expect(host.textContent).toContain("Changed paragraph");
    expect(host.textContent).toContain("Original paragraph");
    expect(requested).toEqual([{ rev: "INDEX", path: "new.docx" }]);
    expect(downloads).toEqual(expect.arrayContaining([`/synthetic/INDEX/new.docx`, join(repo, "new.docx")]));
    releaseDocuments = holdDocuments();
    await click(t("session-gitpanel.staged"));
    await releaseDocuments();
    expect(host.textContent).toContain(t("session-files-compareview.no_text_change"));
    expect(requested.slice(1)).toEqual([{ rev: "HEAD", path: "old.docx" }, { rev: "INDEX", path: "new.docx" }]);
  });

  it("does not invent a source when an older node omits rename metadata and refresh can recover", async () => {
    writeBase("old.png", "synthetic image bytes"); git("mv", "old.png", "new.png"); hideOriginal = true; stub();
    await show("new.png");
    expect(requested).toEqual([]);
    expect(host.textContent).toContain(t("session-files-compareview.rename_path_unavailable"));
    expect(host.textContent).not.toContain(t("session-files-compareview.not_in_version"));
    hideOriginal = false;
    await click(t("session-gitpanel.retry"));
    expect(requested).toEqual([{ rev: "HEAD", path: "old.png" }, { rev: "INDEX", path: "new.png" }]);
    expect(host.querySelectorAll("img")).toHaveLength(2);
  });

  it("can still compare the unstaged new name when rename metadata is unavailable", async () => {
    writeBase("old.png", "synthetic image bytes"); git("mv", "old.png", "new.png"); appendFileSync(join(repo, "new.png"), " worktree");
    hideOriginal = true; stub();
    await show("new.png");
    expect(requested).toEqual([{ rev: "INDEX", path: "new.png" }]);
    expect(host.querySelectorAll("img")).toHaveLength(2);
    await click(t("session-gitpanel.staged"));
    expect(host.textContent).toContain(t("session-files-compareview.rename_path_unavailable"));
    expect(requested).toHaveLength(1);
  });

  it("shows a denied original read as an error and retries it", async () => {
    writeBase("old.png", "synthetic image bytes"); git("mv", "old.png", "new.png"); deny = true; stub();
    await show("new.png");
    expect(host.textContent).toContain("Synthetic path refused");
    expect(host.textContent).not.toContain(t("session-files-compareview.not_in_version"));
    expect(host.querySelectorAll("img")).toHaveLength(0);
    deny = false;
    await click(t("session-gitpanel.retry"));
    expect(requested.filter((r) => r.rev === "HEAD")).toEqual([{ rev: "HEAD", path: "old.png" }, { rev: "HEAD", path: "old.png" }]);
    expect(host.querySelectorAll("img")).toHaveLength(2);
  });

  it("ignores a late failed old-path read after switching back to the unstaged view", async () => {
    writeBase("old.png", "synthetic image bytes"); git("mv", "old.png", "new.png"); appendFileSync(join(repo, "new.png"), " worktree");
    let answer!: (response: Response) => void;
    hold = (r) => r.rev === "HEAD" ? new Promise<Response>((resolve) => { answer = resolve; }) : undefined; stub();
    await show("new.png");
    await click(t("session-gitpanel.staged"));
    await click(t("session-gitpanel.unstaged"));
    await act(async () => { answer(json({ error: "Late refusal" }, 403)); });
    expect(host.textContent).not.toContain("Late refusal");
    expect(host.querySelectorAll("img")).toHaveLength(2);
    expect(requested.at(-1)).toEqual({ rev: "INDEX", path: "new.png" });
  });

  it("does not attach a late answer to another selected file", async () => {
    writeBase("old-a.png", "first synthetic image");
    writeFileSync(join(repo, "old-b.png"), "second synthetic image"); git("add", "old-b.png"); git("commit", "-qm", "second");
    git("mv", "old-a.png", "new-a.png"); git("mv", "old-b.png", "new-b.png");
    let answer!: (response: Response) => void;
    hold = (r) => r.rev === "HEAD" && r.path === "old-a.png" ? new Promise<Response>((resolve) => { answer = resolve; }) : undefined; stub();
    await show("new-a.png");
    await click(t("session-files-parts.back"));
    const second = [...host.querySelectorAll("button")].find((b) => b.title === "new-b.png");
    expect(second).toBeTruthy();
    await act(async () => { second!.click(); });
    expect(requested).toEqual(expect.arrayContaining([{ rev: "HEAD", path: "old-b.png" }, { rev: "INDEX", path: "new-b.png" }]));
    await act(async () => { answer(json({ error: "Another file's refusal" }, 403)); });
    expect(host.textContent).not.toContain("Another file's refusal");
    expect(host.querySelectorAll("img")).toHaveLength(2);
    expect(host.textContent).toContain("new-b.png");
  });

  it("retires a comparison when a live status no longer contains the selected change", async () => {
    writeBase("old.png", "synthetic image bytes"); git("mv", "old.png", "new.png");
    let answer!: (response: Response) => void;
    hold = (r) => r.rev === "HEAD" ? new Promise<Response>((resolve) => { answer = resolve; }) : undefined; stub();
    await show("new.png");
    git("mv", "new.png", "later.png");
    await act(async () => { fsLive.notify?.({ git: true }); });
    expect(host.textContent).toContain(t("session-gitpanel.change_gone"));
    await act(async () => { answer(json({ error: "Stale selection" }, 403)); });
    expect(host.textContent).not.toContain("Stale selection");
    expect(host.querySelectorAll("img")).toHaveLength(0);
  });

  it("reloads versions after a new status and ignores the earlier answer", async () => {
    writeBase("old.png", "synthetic image bytes"); git("mv", "old.png", "new.png");
    let answer!: (response: Response) => void;
    let first = true;
    hold = (r) => {
      if (r.rev !== "HEAD" || !first) return undefined;
      first = false;
      return new Promise<Response>((resolve) => { answer = resolve; });
    };
    stub();
    await show("new.png");
    await act(async () => { fsLive.notify?.({ git: true }); });
    expect(requested.filter((r) => r.rev === "HEAD")).toHaveLength(2);
    expect(host.querySelectorAll("img")).toHaveLength(2);
    await act(async () => { answer(json({ error: "Earlier status failed" }, 403)); });
    expect(host.textContent).not.toContain("Earlier status failed");
    expect(host.querySelectorAll("img")).toHaveLength(2);
  });

  it.each(["manager", "credential"])("retires the selected comparison when the %s changes away and back", async (kind) => {
    writeBase("old.png", "synthetic image bytes"); git("mv", "old.png", "new.png");
    let answer!: (response: Response) => void;
    hold = (r) => r.rev === "HEAD" ? new Promise<Response>((resolve) => { answer = resolve; }) : undefined; stub();
    await show("new.png");
    await act(async () => {
      if (kind === "manager") { chooseManager("synthetic-manager"); chooseManager(oldManager); }
      else { setToken("synthetic-token"); setToken(oldToken); }
    });
    await act(async () => { answer(json({ error: "Stale refusal" }, 403)); });
    expect(host.textContent).not.toContain("Stale refusal");
    expect(host.querySelectorAll("img")).toHaveLength(0);
    expect([...host.querySelectorAll("button")].some((b) => b.title === "new.png")).toBe(true);
  });
});
