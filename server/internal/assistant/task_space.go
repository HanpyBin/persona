package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// Task workspaces (#4786, docs/integrations/engines.md "Task workspaces"). On a terminal host (herdr) a task leader
// gets a workspace of its own, named after its task, and every worker of the task opens as a tab there; a writable
// worker still works in its own Git worktree, made without a workspace of its own. Rework and "go on" go to the same
// session, and one that was retired resumes as a new tab in that workspace. Once the leader's task is done (accepted,
// cancelled or closed) and nothing runs in the workspace any more, it is closed.
//
// The hub keeps which workspace a node opened for which leader task (settings key task_workspaces). Those are the only
// workspaces it ever closes, and the node closes one only while herdr still lists it under the label it was opened
// with: a workspace the user opened, renamed or that belongs to another task is never closed. Paseo and other hosts
// open no task workspace (the node answers none), so nothing is recorded or closed there.

const taskSpacesKey = "task_workspaces"

// deviceWorkspaceClose closes a task's workspace on its node (node taskspace.go).
const deviceWorkspaceClose = "workspace.close" // {workspace_id, label} → {closed, gone?}

// taskSpaceLabelMax bounds the workspace's name taken from the task's title.
const taskSpaceLabelMax = 60

// taskSpacesKeep is how long the record of a done task's closed workspace stays (a resume reopens it under its name).
var taskSpacesKeep = 14 * 24 * time.Hour

// taskSpace is the terminal workspace a node opened for a leader task: on Machine, with ID ("" once closed), named
// Label.
type taskSpace struct {
	Machine string `json:"machine"`
	ID      string `json:"workspace_id,omitempty"`
	Label   string `json:"label"`
}

// taskSpaceReply is the task_workspace a node answers a start or a message with when its new tab opened in a task's
// workspace (node taskspace.go).
type taskSpaceReply struct {
	TaskWorkspace *struct {
		WorkspaceID string `json:"workspace_id"`
		Label       string `json:"label"`
	} `json:"task_workspace"`
}

type taskSpaceBook struct {
	mu     sync.Mutex
	loaded bool
	spaces map[string]taskSpace // by the leader task's todo id
	// tried: when closing a task's workspace last failed, so a node that cannot is not asked every minute.
	tried map[string]time.Time
}

// loadTaskSpacesLocked reads the records once; s.spaces.mu is held. A failed read leaves them unloaded (a write
// then would put a partial map over them).
func (s *Service) loadTaskSpacesLocked(ctx context.Context) bool {
	b := &s.spaces
	if b.loaded {
		return true
	}
	m := map[string]taskSpace{}
	if st := s.store(); st != nil {
		if _, err := st.Setting(ctx, taskSpacesKey, &m); err != nil {
			s.logf("task workspaces: load: %v", err)
			return false
		}
		if m == nil {
			m = map[string]taskSpace{}
		}
	}
	b.spaces, b.loaded = m, true
	return true
}

// taskSpaceOf is the workspace recorded for leader task root.
func (s *Service) taskSpaceOf(ctx context.Context, root string) (taskSpace, bool) {
	s.spaces.mu.Lock()
	defer s.spaces.mu.Unlock()
	if root == "" || !s.loadTaskSpacesLocked(ctx) {
		return taskSpace{}, false
	}
	sp, ok := s.spaces.spaces[root]
	return sp, ok
}

// putTaskSpace records (sp set) or forgets (nil) the workspace of leader task root.
func (s *Service) putTaskSpace(ctx context.Context, root string, sp *taskSpace) {
	s.spaces.mu.Lock()
	defer s.spaces.mu.Unlock()
	if root == "" || !s.loadTaskSpacesLocked(ctx) {
		return
	}
	if cur, ok := s.spaces.spaces[root]; sp != nil && ok && cur == *sp || sp == nil && !ok {
		return
	}
	if sp == nil {
		delete(s.spaces.spaces, root)
	} else {
		s.spaces.spaces[root] = *sp
	}
	if st := s.store(); st != nil {
		if err := st.PutSetting(ctx, taskSpacesKey, s.spaces.spaces); err != nil {
			s.logf("task workspaces: save: %v", err)
		}
	}
}

// taskSpaceLabel is the workspace name of a leader task titled title.
func taskSpaceLabel(title, id string) string {
	return clip(oneLine(firstNonEmpty(strings.TrimSpace(title), id)), taskSpaceLabelMax)
}

// taskSpaceRoot is the leader task whose workspace plan task t's agent opens in: its own for a leader, its leader's
// for a worker, none for any other task.
func taskSpaceRoot(t PlanTask) string {
	switch {
	case t.Role == roleLeader && t.Parent == "":
		return t.TodoID
	case t.Parent != "":
		return t.Parent
	}
	return ""
}

// workerTaskSpace is the leader's workspace a worker (plan task t) on machine mid opens in; false when its leader has
// none there.
func (s *Service) workerTaskSpace(ctx context.Context, t PlanTask, mid string) (taskSpace, bool) {
	if t.Parent == "" || t.Role == roleLeader {
		return taskSpace{}, false
	}
	sp, ok := s.taskSpaceOf(ctx, t.Parent)
	return sp, ok && s.canonical(sp.Machine) == s.canonical(mid)
}

// taskSpaceParams adds to a start of plan task t on machine mid the task's workspace: a leader's is named after its
// task (and is the recorded one when there is one there), a worker's is its leader's. Nothing for any other task, or a
// worker whose leader has no workspace on that machine (another computer, or an engine that opens none).
func (s *Service) taskSpaceParams(ctx context.Context, t PlanTask, mid string, params map[string]any) {
	root := taskSpaceRoot(t)
	if root == "" {
		return
	}
	sp, ok := s.taskSpaceOf(ctx, root)
	if ok && s.canonical(sp.Machine) != s.canonical(mid) {
		if t.Role != roleLeader {
			return
		}
		ok = false // the leader starts again on another computer: a workspace of its own there
	}
	if !ok {
		if t.Role != roleLeader {
			return
		}
		sp = taskSpace{Machine: mid, Label: taskSpaceLabel(t.Title, root)}
	}
	params["workspace_label"] = sp.Label
	if sp.ID != "" {
		params["workspace_id"] = sp.ID
	} else {
		delete(params, "workspace_id")
	}
}

// noteTaskSpace records the workspace a node says the new tab of an agent of leader task root went in.
func (s *Service) noteTaskSpace(ctx context.Context, root, mid string, r taskSpaceReply) {
	if root == "" || r.TaskWorkspace == nil || r.TaskWorkspace.WorkspaceID == "" || r.TaskWorkspace.Label == "" {
		return
	}
	s.putTaskSpace(ctx, root, &taskSpace{Machine: s.canonical(mid), ID: r.TaskWorkspace.WorkspaceID, Label: r.TaskWorkspace.Label})
}

// withSendTaskSpace adds to a message for agent session on machine the workspace of the leader task it works for, where
// a resume opens its tab; it answers the task to record the reply's workspace for ("" none).
func (s *Service) withSendTaskSpace(ctx context.Context, machine string, p map[string]any) string {
	sid, _ := p["session_id"].(string)
	root, sp, ok := s.sessionTaskSpace(ctx, machine, sid)
	if !ok {
		return ""
	}
	p["workspace_label"] = sp.Label
	if sp.ID != "" {
		p["workspace_id"] = sp.ID
	}
	return root
}

// noteSendTaskSpace records the workspace a message's reply raw says a resumed agent of task root opened in.
func (s *Service) noteSendTaskSpace(ctx context.Context, root, machine string, raw json.RawMessage) {
	var r taskSpaceReply
	if root != "" && len(raw) > 0 && json.Unmarshal(raw, &r) == nil {
		s.noteTaskSpace(ctx, root, machine, r)
	}
}

// sessionTaskSpace is the leader task, and its workspace, that agent machine/session works for: the leader's own
// task, or the task a worker's row names as its leader's (via). Open tasks first; a done one still counts, so a
// message that resumes one of its agents opens the tab in the task's workspace again.
func (s *Service) sessionTaskSpace(ctx context.Context, machine, session string) (string, taskSpace, bool) {
	s.spaces.mu.Lock()
	empty := !s.loadTaskSpacesLocked(ctx) || len(s.spaces.spaces) == 0
	s.spaces.mu.Unlock()
	if empty || session == "" || !s.todosLoaded() {
		return "", taskSpace{}, false
	}
	machine = s.canonical(machine)
	root := ""
	s.todos.mu.RLock()
	for _, t := range s.todos.todos {
		for _, a := range t.Agents {
			if a.MachineID != machine || a.SessionID != session || a.DismissedAt != nil {
				continue
			}
			r := ""
			switch a.Source {
			case sourceLeader:
				r = t.ID
			case sourceWorker:
				r = a.Via
			}
			if r != "" && (root == "" || t.DoneAt == nil) {
				root = r
			}
		}
	}
	s.todos.mu.RUnlock()
	sp, ok := s.taskSpaceOf(ctx, root)
	if !ok || s.canonical(sp.Machine) != machine {
		return "", taskSpace{}, false
	}
	return root, sp, true
}

// closeDoneTaskSpaces closes the workspaces of leader tasks that are done once nothing runs in them any more: their
// agents were retired (tasks_close.go) or closed on cancel, and nobody started another agent there meanwhile.
func (s *Service) closeDoneTaskSpaces(ctx context.Context, now time.Time) {
	if !s.todosLoaded() {
		return
	}
	s.spaces.mu.Lock()
	if !s.loadTaskSpacesLocked(ctx) {
		s.spaces.mu.Unlock()
		return
	}
	spaces := make(map[string]taskSpace, len(s.spaces.spaces))
	for k, v := range s.spaces.spaces {
		spaces[k] = v
	}
	s.spaces.mu.Unlock()
	reachable := map[string]bool{}
	for _, d := range s.devices(ctx) {
		reachable[s.canonical(d.ID)] = d.Reachable
	}
	lists := map[string][]proto.Session{}
	for root, sp := range spaces {
		t, known := s.todoNow(root)
		if known && t.DoneAt == nil {
			continue
		}
		if sp.ID == "" { // closed already: kept a while for a resume to reopen it under its name
			if !known || now.Sub(*t.DoneAt) > taskSpacesKeep {
				s.putTaskSpace(ctx, root, nil)
			}
			continue
		}
		m := s.canonical(sp.Machine)
		if !reachable[m] || now.Sub(s.taskSpaceTried(root)) < taskCloseRetry {
			continue
		}
		list, ok := lists[m]
		if !ok {
			got, err := s.sessions(ctx, m)
			if err != nil {
				continue
			}
			list, lists[m] = got, got
		}
		if spaceBusy(list, sp.ID) {
			continue
		}
		s.closeTaskSpace(ctx, root, sp, now)
	}
}

// spaceBusy: an agent still runs in workspace ws (a task's agent not retired yet, or one someone started there).
func spaceBusy(list []proto.Session, ws string) bool {
	for _, c := range list {
		if c.WorkspaceID == ws && c.PaneID != "" && c.Status != proto.StatusOffline {
			return true
		}
	}
	return false
}

func (s *Service) taskSpaceTried(root string) time.Time {
	s.spaces.mu.Lock()
	defer s.spaces.mu.Unlock()
	return s.spaces.tried[root]
}

// closeTaskSpace asks the node to close leader task root's workspace sp. Closed, already gone, or no longer the task's
// (renamed by the user): the record keeps only the name. Any other failure is tried again after taskCloseRetry.
func (s *Service) closeTaskSpace(ctx context.Context, root string, sp taskSpace, now time.Time) {
	var res struct {
		Closed bool `json:"closed"`
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	err := s.call(cctx, sp.Machine, deviceWorkspaceClose, map[string]any{"workspace_id": sp.ID, "label": sp.Label}, &res)
	cancel()
	var re *rpc.Error
	switch {
	case err == nil:
		if res.Closed {
			s.logf("task %s: closed its workspace %s/%s", root, sp.Machine, sp.ID)
		}
	case errors.As(err, &re) && re.Code == "not_task_workspace":
		s.logf("task %s: workspace %s/%s left open: %v", root, sp.Machine, sp.ID, err)
	default:
		s.logf("task %s: close workspace %s/%s: %v", root, sp.Machine, sp.ID, err)
		s.spaces.mu.Lock()
		if s.spaces.tried == nil {
			s.spaces.tried = map[string]time.Time{}
		}
		s.spaces.tried[root] = now
		s.spaces.mu.Unlock()
		return
	}
	s.spaces.mu.Lock()
	delete(s.spaces.tried, root)
	s.spaces.mu.Unlock()
	sp.ID = ""
	s.putTaskSpace(ctx, root, &sp)
}
