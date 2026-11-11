package node

import (
	"context"
	"errors"
	"sync"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/host"
)

// Task workspaces (#4786, docs/integrations/engines.md "Task workspaces"). A task leader and its workers share one
// herdr workspace named after the task. The hub names it on every start, and on a message that may resume one of the
// task's agents: workspace_label, with workspace_id once a node has opened it. The new tab goes there. When the
// workspace is gone (herdr removes a workspace with its last tab) a new one with the same label is opened, and the
// reply carries task_workspace so the hub keeps the id it may close once the task is done (workspace.close). Only
// herdr does this: another host keeps placing tabs by directory and answers no task_workspace, so the hub never
// records, and never closes, anything there.

type taskSpaceKey struct{}

// taskSpace is the task workspace a start or a message asked for (id, label) and the one its new tab went in (got).
type taskSpace struct {
	id, label string
	mu        sync.Mutex
	got       string
}

// withTaskSpace carries the task workspace of a start or a message in ctx; none without a label.
func withTaskSpace(ctx context.Context, id, label string) (context.Context, *taskSpace) {
	if label == "" {
		return ctx, nil
	}
	ts := &taskSpace{id: id, label: label}
	return context.WithValue(ctx, taskSpaceKey{}, ts), ts
}

// taskSpaceIn is the task workspace ctx carries for a tab launch opens; nil when there is none or launch is not herdr.
func taskSpaceIn(ctx context.Context, launch host.Launcher) *taskSpace {
	ts, _ := ctx.Value(taskSpaceKey{}).(*taskSpace)
	if _, herdrTab := launch.(*herdrHost); !herdrTab {
		return nil
	}
	return ts
}

func (ts *taskSpace) opened(ws string) {
	ts.mu.Lock()
	ts.got = ws
	ts.mu.Unlock()
}

// reply is the task_workspace a start or a message answers with: the workspace its new tab went in; nil when it
// opened none.
func (ts *taskSpace) reply() map[string]any {
	if ts == nil {
		return nil
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.got == "" {
		return nil
	}
	return map[string]any{"workspace_id": ts.got, "label": ts.label}
}

// withTaskSpaceReply adds ts's reply to a handler's result map.
func withTaskSpaceReply(res any, ts *taskSpace) any {
	r := ts.reply()
	if m, ok := res.(map[string]any); ok && r != nil {
		m["task_workspace"] = r
	}
	return res
}

// tabSpace is the workspace a new tab for cwd goes in: the task's on herdr (taskSpace: "" when it is gone, so newTab
// opens a new one under its label), else asked, else the one named after the directory (workspaceFor).
func (n *Node) tabSpace(ctx context.Context, launch host.Launcher, cwd, asked string) (string, error) {
	ts := taskSpaceIn(ctx, launch)
	if ts == nil {
		if asked != "" {
			return asked, nil
		}
		return n.workspaceFor(ctx, launch, cwd)
	}
	if ts.id == "" {
		return "", nil
	}
	wss, err := launch.Spaces(ctx)
	if err != nil {
		return n.workspaceFor(ctx, launch, cwd) // herdr is not answering: its refusal, as for any start
	}
	for _, w := range wss {
		if w.ID == ts.id && w.Label == ts.label {
			return w.ID, nil
		}
	}
	return "", nil // closed, or renamed by the user and so no longer the task's
}

// Refusal codes of workspace.close.
const (
	notTaskSpace = "not_task_workspace"
)

// closeTaskSpace closes task workspace id of the own herdr server, with every tab in it, when herdr still lists it
// under label. Gone already is not an error (closed false, gone true): herdr removes a workspace with its last tab.
// A workspace listed under another label was renamed by the user, who has taken it over: refused, never closed.
func (n *Node) closeTaskSpace(ctx context.Context, id, label string) (map[string]any, error) {
	if id == "" || label == "" {
		return nil, errors.New("workspace_id and label are required")
	}
	h, ok := n.hosts.Get(host.Herdr).(*herdrHost)
	if session, _ := herdr.SplitID(id); !ok || session != "" {
		return nil, refusal(notTaskSpace, "only a workspace Persona opened on this computer's herdr is closed", nil)
	}
	wss, err := h.Spaces(ctx)
	if err != nil {
		return nil, err
	}
	for _, w := range wss {
		if w.ID != id {
			continue
		}
		if w.Label != label {
			return nil, refusal(notTaskSpace, "the workspace was renamed: it is no longer the task's", nil)
		}
		if err := h.CloseSpace(ctx, id); err != nil {
			if herdr.IsNotFound(err) {
				break
			}
			return nil, err
		}
		_ = n.refreshHosts(ctx, h)
		return map[string]any{"closed": true}, nil
	}
	return map[string]any{"closed": false, "gone": true}, nil
}
