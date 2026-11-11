package assistant

import (
	"context"
	"path/filepath"

	"github.com/nwverse/persona/server/internal/herdr"
	"github.com/nwverse/persona/server/internal/proto"
)

// deviceWorktreeAdd makes a Git worktree on a node and opens nothing (node agentworktree.go).
const deviceWorktreeAdd = "worktree.add" // {cwd, branch} → {path, branch, root}

// newWorktree makes a worktree on branch of the repository cwd is in, through the machine's terminal engine ("New
// worktree…", docs/integrations/terminal.md): herdr's or Orca's own, so it is listed there and in the apps, never a bare
// `git worktree add` by the agent. The checkout goes where the engine puts them, named for the branch the way the
// dialog names it.
func (s *Service) newWorktree(ctx context.Context, mid, cwd, branch string) (proto.WorktreeCreateResult, error) {
	var prep proto.WorktreePrepare
	if err := s.call(ctx, mid, DeviceWorktreePrepare, map[string]any{"cwd": cwd}, &prep); err != nil {
		return proto.WorktreeCreateResult{}, err
	}
	var res proto.WorktreeCreateResult
	err := s.call(ctx, mid, DeviceWorktreeCreate, map[string]any{"cwd": cwd, "branch": branch,
		"path": filepath.Join(prep.Path, herdr.WorktreeDirName(branch))}, &res)
	return res, err
}

// newTaskWorktree makes plan task t's own worktree. A worker whose leader has a task workspace on that computer
// (task_space.go) gets the checkout alone (worktree.add, which opens nothing): its tab opens in the leader's workspace.
// Any other task gets the engine's worktree with a workspace of its own (newWorktree).
func (s *Service) newTaskWorktree(ctx context.Context, t *PlanTask) (proto.WorktreeCreateResult, error) {
	if m, ok := s.machineByName(ctx, t.Machine); ok {
		if _, shared := s.workerTaskSpace(ctx, *t, m.ID); shared {
			var added proto.WorktreeAdded
			err := s.call(ctx, t.Machine, deviceWorktreeAdd, map[string]any{"cwd": t.Cwd, "branch": t.WorkspaceResolved.Branch}, &added)
			return proto.WorktreeCreateResult{Path: added.Path, Branch: added.Branch}, err
		}
	}
	return s.newWorktree(ctx, t.Machine, t.Cwd, t.WorkspaceResolved.Branch)
}
