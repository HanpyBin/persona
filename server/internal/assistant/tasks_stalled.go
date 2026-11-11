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

// closeStalledTask stops only Persona-owned herdr sessions. Runtime-owned sessions remain
// subject to the existing reminders, without close, retire, or archive requests.
func (s *Service) closeStalledTask(ctx context.Context, task Todo, now time.Time) {
	if task.Supervision == nil || task.DoneAt != nil {
		return
	}
	switch task.Supervision.state(task) {
	case reviewPending, reviewNeedsUser, reviewParked, reviewQueued, reviewTimedOut:
		return
	}
	agents := s.taskAgents(ctx, task)
	if len(agents) == 0 {
		return
	}
	for _, c := range agents {
		since := stalledSince(task, c)
		if c.Host != proto.EngineHerdr || c.SourceKind != "" || c.SourceInstanceID != "" || c.Assistant || !c.StartedByPersona || c.Starting ||
			c.PaneID == "" || c.State != proto.StateWorking || c.PendingInteractions > 0 || since.IsZero() || now.Sub(since) < stallCloseAfter(task.Supervision.ExpectMinutes) {
			return
		}
	}
	// Serialize with reviews and ledger updates, then re-read before the destructive node call.
	s.supervise.writeMu.Lock()
	defer s.supervise.writeMu.Unlock()
	current, ok := s.todoNow(task.ID)
	if !ok || current.DoneAt != nil || current.Supervision == nil || !current.Supervision.UpdatedAt.Equal(task.Supervision.UpdatedAt) {
		return
	}
	for _, c := range agents {
		live, ok := s.session(ctx, c.MachineID, c.SessionID)
		if !ok || live.Host != proto.EngineHerdr || live.State != proto.StateWorking || live.PendingInteractions > 0 || now.Sub(stalledSince(current, live)) < stallCloseAfter(current.Supervision.ExpectMinutes) {
			return
		}
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := s.call(cctx, c.MachineID, "session.close", map[string]any{"session_id": c.SessionID}, nil)
		cancel()
		if err != nil {
			s.logf("task %s: stalled close: %v", task.ID, err)
			return
		}
		s.endSessionCards(c.MachineID, c.SessionID)
	}
	if _, _, err := s.updateSupervisionLockedAt(ctx, task.ID, now, func(v *Supervision) bool {
		v.State = reviewTimedOut
		v.Reviews = append(v.Reviews, TaskReview{At: now.UTC(), Verdict: reviewTimedOut, By: "system"})
		return true
	}); err != nil {
		s.logf("task %s: record timeout: %v", task.ID, err)
	}
}
