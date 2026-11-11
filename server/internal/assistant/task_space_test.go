package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/proto"
)

// taskSpaceService is a service whose node opens task workspaces as herdr does, in a repository (/code/app) where a
// second writer gets a worktree of its own; worktree.add makes it without a workspace, and workspace.close is recorded.
func taskSpaceService(t *testing.T) (*Service, *fakeHub, func() []map[string]any) {
	t.Helper()
	s, f := newQuietService(t)
	f.herdrSpaces = true
	var mu sync.Mutex
	var closed []map[string]any
	f.nodes = map[string]nodeAnswer{
		"worktree.status": func(string, json.RawMessage) (any, error) {
			return proto.WorktreeStatus{Path: "/code/app", Branch: "main"}, nil
		},
		deviceWorktreeAdd: func(_ string, raw json.RawMessage) (any, error) {
			var in struct{ Cwd, Branch string }
			_ = json.Unmarshal(raw, &in)
			return proto.WorktreeAdded{Path: "/wt/app/" + herdr.WorktreeDirName(in.Branch), Branch: in.Branch, Root: "/code/app"}, nil
		},
		deviceWorkspaceClose: func(_ string, raw json.RawMessage) (any, error) {
			var in map[string]any
			_ = json.Unmarshal(raw, &in)
			mu.Lock()
			closed = append(closed, in)
			mu.Unlock()
			return map[string]any{"closed": true}, nil
		},
	}
	return s, f, func() []map[string]any { mu.Lock(); defer mu.Unlock(); return append([]map[string]any(nil), closed...) }
}

// lastSendParams are the params of the latest session.send to machine/session.
func lastSendParams(t *testing.T, f *fakeHub, machine, session string) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var out map[string]any
	for _, c := range f.calls {
		var p map[string]any
		if c.Device == machine && c.Method == DeviceSessionSend && json.Unmarshal(c.Params, &p) == nil && p["session_id"] == session {
			out = p
		}
	}
	if out == nil {
		t.Fatalf("no session.send to %s/%s", machine, session)
	}
	return out
}

func worktreeCalls(f *fakeHub, method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

// A task leader on herdr opens a workspace named after its task, and every worker of the task opens as a tab in it: the
// start carries the leader's workspace_id. A writable worker still gets a worktree of its own, made without a workspace
// (worktree.add, not the engine's worktree.create that opens one).
func TestTaskWorkspaceLeaderAndWorkersShareOne(t *testing.T) {
	s, f, _ := taskSpaceService(t)
	ctx := context.Background()
	leader := startTaskLeader(t, s, f)
	if got := f.starts[0]; got["workspace_label"] != "拆开做登录改造" || got["workspace_id"] != nil {
		t.Fatalf("the leader's start: workspace_label %v workspace_id %v", got["workspace_label"], got["workspace_id"])
	}
	if sp, ok := s.taskSpaceOf(ctx, leader.ID); !ok || sp != (taskSpace{Machine: "box", ID: "wT1", Label: "拆开做登录改造"}) {
		t.Fatalf("recorded workspace: %+v %v", sp, ok)
	}

	planWorker(t, s, f, nil)
	planWorker(t, s, f, map[string]any{"title": "改注册页"})
	if len(f.starts) != 3 {
		t.Fatalf("starts: %d", len(f.starts))
	}
	for i, w := range f.starts[1:] {
		cwd, _ := w["cwd"].(string)
		if w["workspace_id"] != "wT1" || w["workspace_label"] != "拆开做登录改造" || !strings.HasPrefix(cwd, "/wt/app/") {
			t.Fatalf("worker %d start: workspace_id %v label %v cwd %v", i+1, w["workspace_id"], w["workspace_label"], w["cwd"])
		}
	}
	if n := worktreeCalls(f, DeviceWorktreeCreate); n != 0 {
		t.Fatalf("a worker's worktree opened a workspace of its own (%d worktree.create)", n)
	}
	if n := worktreeCalls(f, deviceWorktreeAdd); n != 2 {
		t.Fatalf("worktree.add calls: %d, want 2", n)
	}
	if f.spacesMade != 1 {
		t.Fatalf("workspaces opened for one task: %d", f.spacesMade)
	}
}

// Rework and "go on" go to the same session and name the task's workspace, so a retired agent resumes as a new tab
// there: no workspace is opened by directory or made anew while the task's is still there. When the node had to open it
// again (herdr removed it with its last tab), the new id is what the next start and the close use.
func TestTaskWorkspaceReworkAndContinueStayInIt(t *testing.T) {
	s, f, _ := taskSpaceService(t)
	ctx := context.Background()
	leader := startTaskLeader(t, s, f)
	w := planWorker(t, s, f, nil)
	wsid := w.Agents[0].SessionID

	raw, _ := json.Marshal(map[string]any{"todo_id": w.ID, "verdict": "rework", "note": "补一个深色模式截图"})
	if res := s.mcpCall(ctx, "box", "s1", "task_review", raw, false); res.IsError {
		t.Fatalf("rework: %+v", res)
	}
	if p := lastSendParams(t, f, "box", wsid); p["workspace_id"] != "wT1" || p["workspace_label"] != "拆开做登录改造" ||
		!strings.Contains(p["text"].(string), "补一个深色模式截图") {
		t.Fatalf("the rework message: %v", p)
	}
	if err := s.call(ctx, "box", DeviceSessionSend, map[string]any{"session_id": "s1", "text": continueText}, nil); err != nil {
		t.Fatal(err)
	}
	if p := lastSendParams(t, f, "box", "s1"); p["workspace_id"] != "wT1" || p["workspace_label"] != "拆开做登录改造" {
		t.Fatalf("the leader's go-on: %v", p)
	}
	if f.spacesMade != 1 {
		t.Fatalf("rework or go-on opened another workspace: %d", f.spacesMade)
	}
	// An agent of no task gets no workspace.
	if err := s.call(ctx, "box", DeviceSessionSend, map[string]any{"session_id": "someone-else", "text": "hi"}, nil); err != nil {
		t.Fatal(err)
	}
	if p := lastSendParams(t, f, "box", "someone-else"); p["workspace_id"] != nil || p["workspace_label"] != nil {
		t.Fatalf("a message to an agent of no task: %v", p)
	}

	// The workspace was gone, so the resume opened it again under the task's name: that is the task's workspace now.
	f.nodes[DeviceSessionSend] = func(string, json.RawMessage) (any, error) {
		return map[string]any{"ok": true, "task_workspace": map[string]any{"workspace_id": "wT9", "label": "拆开做登录改造"}}, nil
	}
	if err := s.call(ctx, "box", DeviceSessionSend, map[string]any{"session_id": wsid, "text": "再看一下"}, nil); err != nil {
		t.Fatal(err)
	}
	if sp, _ := s.taskSpaceOf(ctx, leader.ID); sp.ID != "wT9" {
		t.Fatalf("the reopened workspace was not recorded: %+v", sp)
	}
	delete(f.nodes, DeviceSessionSend)
	planWorker(t, s, f, map[string]any{"title": "改注册页"})
	if got := f.starts[len(f.starts)-1]; got["workspace_id"] != "wT9" {
		t.Fatalf("the next worker's start: %v", got["workspace_id"])
	}
}

// A done leader task's workspace is closed once nothing runs in it any more; another task's workspace and one the user
// opened stay open, and so does the task's own while one of its agents still runs there.
func TestTaskWorkspaceClosesOnlyTheDoneTasksOwn(t *testing.T) {
	for _, verdict := range []string{"cancelled", "accepted"} {
		t.Run(verdict, func(t *testing.T) {
			s, f, closed := taskSpaceService(t)
			ctx := context.Background()
			leader := startTaskLeader(t, s, f)
			w := planWorker(t, s, f, nil)
			other, err := s.createTaskTodo(ctx, "另一个任务", "", "conv1", "", "")
			if err != nil {
				t.Fatal(err)
			}
			s.putTaskSpace(ctx, other.ID, &taskSpace{Machine: "box", ID: "wOther", Label: "另一个任务"})
			running := func(sid, ws string) proto.Session {
				return proto.Session{SessionID: sid, MachineID: "box", Cwd: "/code/app", Agent: "claude", Status: proto.StatusIdle,
					PaneID: ws + ":p1", WorkspaceID: ws, UpdatedAt: time.Now()}
			}
			retired := func(sid string) proto.Session {
				return proto.Session{SessionID: sid, MachineID: "box", Cwd: "/code/app", Agent: "claude", Status: proto.StatusOffline}
			}
			f.setSessions("box", running("s1", "wT1"), running(w.Agents[0].SessionID, "wT1"), running("mine", "wUser"), running("o1", "wOther"))

			s.closeDoneTaskSpaces(ctx, time.Now())
			if got := closed(); len(got) != 0 {
				t.Fatalf("closed while the task is open: %v", got)
			}

			args := map[string]any{"todo_id": leader.ID, "verdict": verdict, "note": "结束"}
			if verdict == "accepted" {
				if _, _, err := s.updateSupervision(ctx, leader.ID, func(v *Supervision) bool { v.State = reviewPending; return true }); err != nil {
					t.Fatal(err)
				}
			}
			raw, _ := json.Marshal(args)
			if _, err := s.toolTaskReview(ctx, "conv1", raw); err != nil {
				t.Fatal(err)
			}
			if todoNow(s, leader.ID).DoneAt == nil {
				t.Fatalf("the leader's task is not done after %s", verdict)
			}
			s.closeDoneTaskSpaces(ctx, time.Now())
			if got := closed(); len(got) != 0 {
				t.Fatalf("closed while its agents still run there: %v", got)
			}

			// Its agents retired (tasks_close.go) or closed on cancel; the user's and the other task's still run.
			f.setSessions("box", retired("s1"), retired(w.Agents[0].SessionID), running("mine", "wUser"), running("o1", "wOther"))
			s.closeDoneTaskSpaces(ctx, time.Now())
			s.closeDoneTaskSpaces(ctx, time.Now())
			got := closed()
			if len(got) != 1 || got[0]["workspace_id"] != "wT1" || got[0]["label"] != "拆开做登录改造" {
				t.Fatalf("workspace.close calls: %v", got)
			}
			if sp, _ := s.taskSpaceOf(ctx, leader.ID); sp.ID != "" || sp.Label != "拆开做登录改造" {
				t.Fatalf("the record after closing: %+v", sp)
			}
			if sp, _ := s.taskSpaceOf(ctx, other.ID); sp.ID != "wOther" {
				t.Fatalf("another task's workspace was touched: %+v", sp)
			}
		})
	}
}

// A plain task (no leader) and a node that opens no task workspace (Paseo, an older node) are as before: the start
// names no workspace, nothing is recorded, and workers get no workspace_id from it.
func TestTaskWorkspaceOnlyForLeadersOnHostsThatOpenOne(t *testing.T) {
	s, f, closed := taskSpaceService(t)
	f.herdrSpaces = false // the node answers no task_workspace
	ctx := context.Background()
	leader := startTaskLeader(t, s, f)
	planWorker(t, s, f, nil)
	if _, ok := s.taskSpaceOf(ctx, leader.ID); ok {
		t.Fatal("recorded a workspace the node did not open")
	}
	if w := f.starts[1]; w["workspace_id"] != "ws-new" || w["workspace_label"] != nil { // its worktree's own, as before
		t.Fatalf("the worker's start: %v %v", w["workspace_id"], w["workspace_label"])
	}
	if n := worktreeCalls(f, DeviceWorktreeCreate); n != 1 {
		t.Fatalf("a worker without a task workspace keeps the engine's worktree: %d worktree.create", n)
	}
	s.closeDoneTaskSpaces(ctx, time.Now())
	if len(closed()) != 0 {
		t.Fatal("closed a workspace")
	}

	plain := PlanTask{Title: "普通任务", TodoID: "todo_plain"}
	params := map[string]any{}
	s.taskSpaceParams(ctx, plain, "box", params)
	if len(params) != 0 {
		t.Fatalf("a plain task's start: %v", params)
	}
}
