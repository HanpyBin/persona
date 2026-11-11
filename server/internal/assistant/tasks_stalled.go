package assistant

import (
	"context"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func stallCloseAfter(expect int) time.Duration {
	return max(time.Hour, time.Duration(expect)*3*time.Minute)
}

func stalledSince(t Todo, c proto.Session) time.Time {
	at := c.UpdatedAt
	if c.ActivityAt != nil && c.ActivityAt.After(at) {
		at = *c.ActivityAt
	}
	if v := t.Supervision; v != nil {
		if v.UpdatedAt.After(at) {
			at = v.UpdatedAt
		}
		if v.Progress != nil && v.Progress.At.After(at) {
			at = v.Progress.At
		}
	}
	return at
}

func timeoutHost(c proto.Session) bool {
	return c.Host == proto.EngineHerdr && c.SourceKind == "" && c.SourceInstanceID == "" && !c.Assistant && c.StartedByPersona && !c.Starting
}

// closeStalledTask records a durable intent before closing a Persona-owned herdr session.
// If the final ledger write fails or the manager restarts, timeout_closing completes it on
// the next pass. Runtime sessions receive reminders only, never close or retire requests.
func (s *Service) closeStalledTask(ctx context.Context, task Todo, now time.Time) {
	if task.Supervision == nil || task.DoneAt != nil {
		return
	}
	state := task.Supervision.state(task)
	switch state {
	case reviewPending, reviewNeedsUser, reviewParked, reviewQueued, reviewStarting, reviewTimedOut:
		return
	}
	agents := s.taskAgents(ctx, task)
	count := 0
	for _, a := range task.Agents {
		if a.active() {
			count++
		}
	}
	if count == 0 || len(agents) != count {
		return
	}
	recovering := state == reviewClosing
	for _, c := range agents {
		if recovering && c.Status == proto.StatusOffline && c.PaneID == "" {
			continue
		}
		since := stalledSince(task, c)
		if !timeoutHost(c) || c.PaneID == "" || c.State != proto.StateWorking || c.PendingInteractions > 0 || since.IsZero() || !recovering && now.Sub(since) < stallCloseAfter(task.Supervision.ExpectMinutes) {
			return
		}
	}
	s.supervise.writeMu.Lock()
	defer s.supervise.writeMu.Unlock()
	s.todos.writing.Lock() // do not close an agent while its link is moved to another task
	defer s.todos.writing.Unlock()
	current, ok := s.todoNow(task.ID)
	if !ok || current.DoneAt != nil || current.Supervision == nil || !current.Supervision.UpdatedAt.Equal(task.Supervision.UpdatedAt) {
		return
	}
	s.todos.mu.RLock()
	owned := true
	for _, a := range current.Agents {
		if a.active() && s.todos.activeBySession[[2]string{a.MachineID, a.SessionID}] != task.ID {
			owned = false
		}
	}
	s.todos.mu.RUnlock()
	if !owned {
		return
	}
	if !recovering {
		if _, _, err := s.updateSupervisionLockedAt(ctx, task.ID, now, func(v *Supervision) bool { v.State = reviewClosing; return true }); err != nil {
			s.logf("task %s: record timeout intent: %v", task.ID, err)
			return
		}
	}
	for _, c := range agents {
		live, ok := s.session(ctx, c.MachineID, c.SessionID)
		if !ok {
			return
		}
		if live.Status == proto.StatusOffline && live.PaneID == "" {
			continue
		}
		// A new turn or supervisor action during recovery cancels the old close intent.
		if !timeoutHost(live) || live.State != proto.StateWorking || live.PendingInteractions > 0 || live.UpdatedAt.After(now) ||
			recovering && (live.UpdatedAt.After(task.Supervision.UpdatedAt) || task.Supervision.Progress != nil && task.Supervision.Progress.At.After(task.Supervision.UpdatedAt)) {
			_, _, _ = s.updateSupervisionLockedAt(ctx, task.ID, now, func(v *Supervision) bool { v.State = ""; return true })
			return
		}
		if !recovering && now.Sub(stalledSince(current, live)) < stallCloseAfter(current.Supervision.ExpectMinutes) {
			_, _, _ = s.updateSupervisionLockedAt(ctx, task.ID, now, func(v *Supervision) bool { v.State = ""; return true })
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := s.call(cctx, c.MachineID, "session.close", map[string]any{"session_id": c.SessionID}, nil)
		cancel()
		if err != nil {
			s.logf("task %s: stalled close: %v", task.ID, err)
			// Retain the intent: a timed-out RPC may still have closed the session.
			return
		}
	}
	if _, _, err := s.updateSupervisionLockedAt(ctx, task.ID, now, func(v *Supervision) bool {
		v.State = reviewTimedOut
		v.Reviews = append(v.Reviews, TaskReview{At: now.UTC(), Verdict: reviewTimedOut, By: "system"})
		return true
	}); err != nil {
		s.logf("task %s: record timeout: %v", task.ID, err)
	}
}
