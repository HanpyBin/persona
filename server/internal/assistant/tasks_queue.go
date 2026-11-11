package assistant

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func limitedTask(t PlanTask) bool { return t.Role == roleLeader || t.Parent != "" }

type queuedTask struct {
	plan  *TaskPlan
	index int
	todo  Todo
}

// plannedTasks reads execution bindings from the plans, including reused todos whose PlanID is empty.
// Fail closed if any plan cannot be read: its starting agents may already occupy slots.
func (s *Service) plannedTasks(ctx context.Context) ([]queuedTask, error) {
	plans := map[string]*TaskPlan{}
	conversations := map[string]bool{}
	todos := s.todosOf(ctx, "*")
	for _, t := range todos {
		if t.DoneAt != nil || t.Origin == "" {
			continue
		}
		if t.PlanID == "" {
			conversations[t.Origin] = true
			continue
		}
		if _, ok := plans[t.PlanID]; ok {
			continue
		}
		p, err := s.loadPlan(ctx, t.PlanID)
		if err != nil {
			return nil, err
		}
		plans[p.ID] = p
	}
	for conv := range conversations {
		list, err := s.conversationPlans(ctx, conv)
		if err != nil {
			return nil, err
		}
		for _, p := range list {
			plans[p.ID] = p
		}
	}
	byID := map[string]Todo{}
	for _, t := range todos {
		byID[t.ID] = t
	}
	var out []queuedTask
	for _, p := range plans {
		if p.State != planStarted {
			continue
		}
		for i, pt := range p.Tasks {
			t, ok := byID[pt.TodoID]
			if !ok || t.DoneAt != nil || pt.Error != "" || !limitedTask(pt) {
				continue
			}
			out = append(out, queuedTask{plan: p, index: i, todo: t})
		}
	}
	return out, nil
}

// taskOccupiesSlot counts a start before its agent link exists. A missing machine/session is
// uncertain, so it keeps its slot. Only a confirmed closed terminal session releases it.
func (s *Service) taskOccupiesSlot(ctx context.Context, t Todo) bool {
	if t.DoneAt != nil || t.Supervision.state(t) == reviewTimedOut {
		return false
	}
	if t.Supervision.state(t) == reviewStarting || t.Supervision.state(t) == reviewClosing {
		return true
	}
	linked := false
	for _, a := range t.Agents {
		if !a.active() {
			continue
		}
		linked = true
		c, ok := s.session(ctx, a.MachineID, a.SessionID)
		if !ok {
			return true
		}
		if t.Supervision != nil && t.Supervision.UpdatedAt.After(c.UpdatedAt) && time.Since(t.Supervision.UpdatedAt) < taskStartTimeout {
			return true
		}
		closedTerminal := c.Host == proto.EngineHerdr || c.Host == "" && c.SourceKind == "" && c.SourceInstanceID == "" && c.StartedByPersona
		if !closedTerminal || c.PaneID != "" || c.Status != proto.StatusOffline {
			return true
		}
	}
	return !linked && len(t.Agents) == 0
}

func (s *Service) drainTaskQueue(ctx context.Context) {
	s.tasks.admissionMu.Lock()
	defer s.tasks.admissionMu.Unlock()
	s.drainTaskQueueLocked(ctx)
}

// drainTaskQueueLocked reserves each slot durably before scheduling the start. FIFO applies
// within each capacity pool, so a full leader does not block another leader's workers.
func (s *Service) drainTaskQueueLocked(ctx context.Context) {
	if err := s.ensureTodosLoaded(ctx); err != nil {
		return
	}
	entries, err := s.plannedTasks(ctx)
	if err != nil {
		s.logf("task queue: %v", err)
		return
	}
	limits := *s.settings(ctx).TaskConcurrency
	leaders := 0
	workers := map[string]int{}
	var queue []queuedTask
	for _, entry := range entries {
		pt := entry.plan.Tasks[entry.index]
		if entry.todo.Supervision.state(entry.todo) == reviewQueued {
			queue = append(queue, entry)
			continue
		}
		if !s.taskOccupiesSlot(ctx, entry.todo) {
			continue
		}
		if pt.Role == roleLeader {
			leaders++
		} else {
			workers[pt.Parent]++
		}
	}
	slices.SortFunc(queue, func(a, b queuedTask) int {
		if n := a.todo.Supervision.UpdatedAt.Compare(b.todo.Supervision.UpdatedAt); n != 0 {
			return n
		}
		if n := cmp.Compare(a.plan.ID, b.plan.ID); n != 0 {
			return n
		}
		return cmp.Compare(a.index, b.index)
	})
	for _, entry := range queue {
		pt := entry.plan.Tasks[entry.index]
		if pt.Role == roleLeader {
			if leaders >= limits.Leaders {
				continue
			}
		} else {
			parent, ok := s.todoNow(pt.Parent)
			if !ok || parent.DoneAt != nil || taskAdmissionHeld(parent) {
				continue
			}
			if workers[pt.Parent] >= limits.WorkersPerTask {
				continue
			}
		}
		// Recheck closure after the plan snapshot. Cancellation while queued must not create an agent.
		t, ok := s.todoNow(entry.todo.ID)
		if !ok || t.DoneAt != nil || t.Supervision.state(t) != reviewQueued {
			continue
		}
		if _, changed, err := s.updateSupervision(ctx, t.ID, func(v *Supervision) bool {
			if v.State != reviewQueued {
				return false
			}
			v.State = reviewStarting
			return true
		}); err != nil || !changed {
			s.logf("task queue %s: %v", t.ID, err)
			continue
		}
		if pt.Role == roleLeader {
			leaders++
		} else {
			workers[pt.Parent]++
		}
		if taskHasSession(t) {
			s.taskStarting.Add(1)
			go func(task Todo) {
				defer s.taskStarting.Add(-1)
				cctx, cancel := context.WithTimeout(context.Background(), taskStartTimeout)
				defer cancel()
				if err := s.resumeTimedOutTask(cctx, task); err != nil {
					s.logf("task %s: continue: %v", task.ID, err)
					_, _, _ = s.updateSupervision(cctx, task.ID, func(v *Supervision) bool { v.State = reviewTimedOut; return true })
				}
			}(t)
		} else {
			s.startPlanTaskAsync(entry.plan.ID, entry.plan.Conversation, entry.index, pt, false, func() { s.releaseTakenTodo(pt.ReuseTodo) })
		}
	}
}

func (s *Service) queueRetry(ctx context.Context, t PlanTask) error {
	s.tasks.admissionMu.Lock()
	defer s.tasks.admissionMu.Unlock()
	if _, _, err := s.updateSupervision(ctx, t.TodoID, func(v *Supervision) bool { v.State = reviewQueued; return true }); err != nil {
		return err
	}
	s.releaseTakenTodo(t.ReuseTodo)
	s.drainTaskQueueLocked(ctx)
	return nil
}

// continueTimedOut queues a continuation of the original session, never a new agent.
func (s *Service) continueTimedOut(ctx context.Context, task Todo) (json.RawMessage, error) {
	s.tasks.admissionMu.Lock()
	defer s.tasks.admissionMu.Unlock()
	current, ok := s.todoNow(task.ID)
	if !ok || current.Supervision.state(current) != reviewTimedOut {
		return s.todoView(ctx, task.ID)
	}
	entries, err := s.plannedTasks(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.todo.ID != task.ID {
			continue
		}
		if _, _, err := s.updateSupervision(ctx, task.ID, func(v *Supervision) bool { v.State = reviewQueued; return true }); err != nil {
			return nil, err
		}
		s.drainTaskQueueLocked(ctx)
		return s.todoView(ctx, task.ID)
	}
	// Adopted agents have no plan or admission pool, but still resume their original session.
	if err := s.resumeTimedOutTask(ctx, current); err != nil {
		return nil, err
	}
	return s.todoView(ctx, task.ID)
}

func (s *Service) resumeTimedOutTask(ctx context.Context, task Todo) error {
	for _, a := range task.Agents {
		if !a.active() {
			continue
		}
		if err := s.call(ctx, a.MachineID, DeviceSessionSend, map[string]any{"session_id": a.SessionID, "text": continueText}, nil); err != nil {
			return err
		}
	}
	_, _, err := s.updateSupervision(ctx, task.ID, func(v *Supervision) bool { v.State = ""; return true })
	return err
}

func taskHasSession(t Todo) bool {
	for _, a := range t.Agents {
		if a.active() && a.SessionID != "" && a.StartError == "" {
			return true
		}
	}
	return false
}

func (s *Service) taskQueued(id string) bool {
	t, ok := s.todoNow(id)
	return ok && t.Supervision.state(t) == reviewQueued
}

func taskAdmissionHeld(t Todo) bool {
	switch t.Supervision.state(t) {
	case reviewQueued, reviewClosing, reviewTimedOut:
		return true
	}
	return false
}

// reconcileTaskAdmission resolves a continuation whose response or final ledger write was lost.
// It never sends another message: uncertain sessions keep their reservation until observed.
func (s *Service) reconcileTaskAdmission(ctx context.Context, t Todo, now time.Time) {
	if t.Supervision.state(t) != reviewStarting || !taskHasSession(t) {
		return
	}
	agents := s.taskAgents(ctx, t)
	n := 0
	for _, a := range t.Agents {
		if a.active() {
			n++
		}
	}
	if len(agents) != n {
		return
	}
	state := ""
	for _, c := range agents {
		if c.Status == proto.StatusOffline && c.PaneID == "" {
			if now.Sub(t.Supervision.UpdatedAt) < taskStartTimeout {
				return
			}
			state = reviewTimedOut
		}
	}
	_, _, err := s.updateSupervision(ctx, t.ID, func(v *Supervision) bool {
		if v.State != reviewStarting {
			return false
		}
		v.State = state
		return true
	})
	if err != nil {
		s.logf("task %s: admission recovery: %v", t.ID, err)
	}
}
