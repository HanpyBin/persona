package node

import (
	"context"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// taskSpaceHerdr is a herdr node whose layout lists workspaces wss; workspace.create opens wN, tab.create a tab in
// the workspace asked for.
func taskSpaceHerdr(t *testing.T, wss ...map[string]any) (*Node, *fakeHerdr, string) {
	t.Helper()
	n, f, home := herdrNode(t)
	list := []any{}
	for _, w := range wss {
		list = append(list, w)
	}
	f.reply("workspace.list", map[string]any{"workspaces": list})
	f.reply("workspace.create", map[string]any{"workspace": map[string]any{"workspace_id": "wN", "label": "x"},
		"tab": map[string]any{"tab_id": "wN:t1"}, "root_pane": map[string]any{"pane_id": "wN:p1"}})
	f.handle("tab.create", func(p map[string]any) any {
		ws := str(p["workspace_id"])
		return map[string]any{"tab": map[string]any{"tab_id": ws + ":t2"}, "root_pane": map[string]any{"pane_id": ws + ":p2"}}
	})
	return n, f, home
}

func herdrSpace(id, label string) map[string]any {
	return map[string]any{"workspace_id": id, "label": label, "tab_count": 1, "pane_count": 1}
}

// A tab for a task's agent opens in the task's workspace while herdr lists it under the task's name; gone (herdr
// removed it with its last tab) or renamed by the user, a new workspace under the task's name is opened instead, never
// the one named after the directory. Without a task workspace the directory's workspace is used, as before.
func TestTaskSpaceTabPlacement(t *testing.T) {
	const label = "Rework the login"
	for _, tc := range []struct {
		name, id, label string
		listed          []map[string]any
		want            []string // workspace.create / tab.create calls
		got             string   // the task_workspace answered
	}{
		{name: "listed", id: "wT1", label: label, listed: []map[string]any{herdrSpace("wT1", label), herdrSpace("w2", "app")},
			want: []string{call("tab.create", map[string]any{"workspace_id": "wT1", "cwd": "/code/app", "label": "worker", "focus": false})}, got: "wT1"},
		{name: "gone", id: "wT1", label: label, listed: []map[string]any{herdrSpace("w2", "app")},
			want: []string{call("workspace.create", map[string]any{"cwd": "/code/app", "label": label, "focus": false})}, got: "wN"},
		{name: "renamed by the user", id: "wT1", label: label, listed: []map[string]any{herdrSpace("wT1", "mine now"), herdrSpace("w2", "app")},
			want: []string{call("workspace.create", map[string]any{"cwd": "/code/app", "label": label, "focus": false})}, got: "wN"},
		{name: "first tab of a leader", label: label, listed: []map[string]any{herdrSpace("w2", "app")},
			want: []string{call("workspace.create", map[string]any{"cwd": "/code/app", "label": label, "focus": false})}, got: "wN"},
		{name: "no task", listed: []map[string]any{herdrSpace("w2", "app")},
			want: []string{call("tab.create", map[string]any{"workspace_id": "w2", "cwd": "/code/app", "label": "worker", "focus": false})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, f, _ := taskSpaceHerdr(t, tc.listed...)
			launch, err := n.launcherFor(n.startKind(context.Background()))
			if err != nil {
				t.Fatal(err)
			}
			ctx, ts := withTaskSpace(context.Background(), tc.id, tc.label)
			ws, err := n.tabSpace(ctx, launch, "/code/app", "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := n.newTab(ctx, launch, "/code/app", ws, "worker", ""); err != nil {
				t.Fatal(err)
			}
			assertTrace(t, f.trace("workspace.create", "tab.create"), tc.want)
			r := ts.reply()
			if tc.got == "" && r != nil || tc.got != "" && (r == nil || r["workspace_id"] != tc.got || r["label"] != label) {
				t.Fatalf("task_workspace: %v, want %q", r, tc.got)
			}
		})
	}
}

// A message that resumes a task's agent opens its tab in the task's workspace (session.send passes it down to the
// start), not in the directory's.
func TestTaskSpaceResumeOpensInTheTaskWorkspace(t *testing.T) {
	const id = "dddddddd-0000-0000-0000-000000000004"
	n, f, home := taskSpaceHerdr(t, herdrSpace("wT1", "Rework the login"), herdrSpace("w2", "app"))
	f.reply("agent.list", map[string]any{"agents": []any{}})
	f.reply("agent.get", map[string]any{"agent": map[string]any{"pane_id": "wT1:p2", "agent": "claude", "agent_status": proto.StatusBlocked}})
	f.setScreen("wT1:p2", screenFixture(t, "claude-2.1.312-model-picker-162.txt"))
	n.state.put(id, sessionState{Cwd: home, Launcher: "claude"})
	_, err := request(t, n, "session.send", map[string]any{"session_id": id, "text": "redo it",
		"workspace_id": "wT1", "workspace_label": "Rework the login"})
	if code, _ := refusalOf(t, err); code != "dialog_open" { // came up with a dialog: nothing typed (resumesend_test.go)
		t.Fatalf("send: %v", err)
	}
	creates := f.trace("workspace.create", "tab.create")
	if len(creates) != 1 || creates[0] != call("tab.create", map[string]any{"workspace_id": "wT1", "cwd": home, "label": "agent-1", "focus": false}) {
		t.Fatalf("the resumed tab: %v", creates)
	}
}

// workspace.close closes a task's workspace while herdr lists it under the task's name: gone already is no error, and
// a workspace listed under another name (the user renamed it) or in a named session is refused and left open.
func TestTaskSpaceClose(t *testing.T) {
	const label = "Rework the login"
	n, f, _ := taskSpaceHerdr(t, herdrSpace("wT1", label), herdrSpace("wU", "mine"))
	res, err := request(t, n, "workspace.close", map[string]any{"workspace_id": "wT1", "label": label})
	if err != nil || res["closed"] != true {
		t.Fatalf("close: %v %v", res, err)
	}
	assertTrace(t, f.trace("workspace.close"), []string{call("workspace.close", map[string]any{"workspace_id": "wT1"})})

	f.reset()
	if _, err := request(t, n, "workspace.close", map[string]any{"workspace_id": "wU", "label": label}); refusalCode(err) != notTaskSpace {
		t.Fatalf("a renamed workspace: %v", err)
	}
	if _, err := request(t, n, "workspace.close", map[string]any{"workspace_id": "@dev:w1", "label": label}); refusalCode(err) != notTaskSpace {
		t.Fatalf("a named session's workspace: %v", err)
	}
	res, err = request(t, n, "workspace.close", map[string]any{"workspace_id": "wGone", "label": label})
	if err != nil || res["closed"] != false || res["gone"] != true {
		t.Fatalf("a workspace already gone: %v %v", res, err)
	}
	if got := f.trace("workspace.close"); len(got) != 0 {
		t.Fatalf("closed what is not the task's: %v", got)
	}

	f.reset()
	f.reply("workspace.close", herdrFail{Code: "workspace_not_found", Message: "workspace wT1 not found"})
	res, err = request(t, n, "workspace.close", map[string]any{"workspace_id": "wT1", "label": label})
	if err != nil || res["gone"] != true {
		t.Fatalf("closed meanwhile: %v %v", res, err)
	}
}

// A worker's start names its leader's workspace: the tab opens there (here a Codex worker, the implement tier's) and
// the answer says which workspace it went in, which is what the hub records.
func TestTaskSpaceStartAnswersItsWorkspace(t *testing.T) {
	withCodexLaunchers(t, map[string][]string{"codex": {"codex"}})
	n, f, _, _ := codexStartHerdr(t, 300*time.Millisecond)
	res, err := request(t, n, "session.start", map[string]any{"cwd": "~/proj", "launcher": "codex", "prompt": "hello",
		"workspace_id": "w1", "workspace_label": "proj"})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := res["task_workspace"].(map[string]any); got["workspace_id"] != "w1" || got["label"] != "proj" {
		t.Fatalf("task_workspace: %v", res["task_workspace"])
	}
	if got := f.trace("workspace.create"); len(got) != 0 {
		t.Fatalf("opened another workspace: %v", got)
	}
}
