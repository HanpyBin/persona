package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func stalledTaskFixture(t *testing.T, host string, expect int) (*Service, *fakeHub, Todo, proto.Session, time.Time) {
	t.Helper()
	s, f := newQuietService(t)
	shortSupervise(t, s)
	p, _ := planQueueTasks(t, s, "conv1", nil, 1, roleLeader)
	waitFor(t, "leader link", func() bool { return linkedPlanTasks(s, p) == 1 })
	task := todoNow(s, p.Tasks[0].TodoID)
	now := time.Date(2030, 1, 1, 12, 0, 0, 0, time.UTC)
	last := now.Add(-stallCloseAfter(expect))
	_, _, err := s.updateSupervisionAt(context.Background(), task.ID, last, func(v *Supervision) bool { v.ExpectMinutes = expect; return true })
	if err != nil {
		t.Fatal(err)
	}
	task = todoNow(s, task.ID)
	c := proto.Session{MachineID: "box", SessionID: task.Agents[0].SessionID, Host: host, PaneID: "pane-1", WorkspaceID: "workspace-1", State: proto.StateWorking, Status: proto.StatusWorking, UpdatedAt: last, StartedByPersona: true, Agent: "claude"}
	if host == proto.EnginePaseo {
		c.SourceInstanceID = "paseo"
	}
	f.setSessions("box", c)
	f.answerNode("session.close", func(_ string, raw json.RawMessage) (any, error) {
		var v struct {
			SessionID string `json:"session_id"`
		}
		_ = json.Unmarshal(raw, &v)
		for i := range f.sessions["box"] {
			if f.sessions["box"][i].SessionID == v.SessionID {
				f.sessions["box"][i].PaneID = ""
				f.sessions["box"][i].Status = proto.StatusOffline
				f.sessions["box"][i].State = proto.StateIdle
			}
		}
		return map[string]any{}, nil
	})
	return s, f, task, c, now
}

func countCloseCalls(f *fakeHub) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.Method == "session.close" || c.Method == deviceSessionRetire {
			n++
		}
	}
	return n
}

func TestTaskStalledClosesHerdrAtThresholdAndNotifies(t *testing.T) {
	for _, expect := range []int{0, 30, 90} {
		t.Run(time.Duration(expect).String(), func(t *testing.T) {
			s, f, task, _, now := stalledTaskFixture(t, proto.EngineHerdr, expect)
			ctx := context.Background()
			s.superviseWatch(ctx, now.Add(-time.Second))
			if countCloseCalls(f) != 0 {
				t.Fatal("closed before threshold")
			}
			s.superviseWatch(ctx, now)
			got := todoNow(s, task.ID)
			if countCloseCalls(f) != 1 || got.DoneAt != nil || got.Supervision.state(got) != reviewTimedOut || taskState(got, nil) != reviewTimedOut {
				t.Fatalf("closure: calls=%d todo=%+v", countCloseCalls(f), got)
			}
			if closed, err := s.completedTodo(ctx, "box", task.Agents[0].SessionID); err != nil || !closed {
				t.Fatalf("automatic continuation was not held: %v %v", closed, err)
			}
			s.superviseWatch(ctx, now.Add(time.Hour))
			if countCloseCalls(f) != 1 {
				t.Fatal("timeout closed twice")
			}
			waitFor(t, "supervisor timeout notice", func() bool {
				f.mu.Lock()
				defer f.mu.Unlock()
				for _, send := range f.sends {
					if text, _ := send["text"].(string); strings.Contains(text, "超时收口") && strings.Contains(text, task.ID) {
						return true
					}
				}
				return false
			})
		})
	}
}

func TestTaskStalledPaseoOnlyReminds(t *testing.T) {
	s, f, task, _, now := stalledTaskFixture(t, proto.EnginePaseo, 0)
	s.superviseWatch(context.Background(), now.Add(24*time.Hour))
	got := todoNow(s, task.ID)
	if countCloseCalls(f) != 0 || got.Supervision.state(got) == reviewTimedOut {
		t.Fatal("runtime-owned session touched")
	}
	waitFor(t, "existing stalled reminder", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, send := range f.sends {
			if text, _ := send["text"].(string); strings.Contains(text, "分钟没有进展") {
				return true
			}
		}
		return false
	})
}

func TestTaskStalledProgressAndSupervisorActionResetClock(t *testing.T) {
	for _, kind := range []string{"activity", "report", "supervisor", "needs_user", "parked"} {
		t.Run(kind, func(t *testing.T) {
			s, f, task, c, now := stalledTaskFixture(t, proto.EngineHerdr, 0)
			recent := now.Add(-5 * time.Minute)
			switch kind {
			case "activity":
				c.ActivityAt = &recent
				f.setSessions("box", c)
			case "report":
				s.noteProgress(context.Background(), task.ID, TaskProgress{At: recent, Kind: "progress", Text: "Advanced"})
			case "supervisor":
				_, _, _ = s.updateSupervisionAt(context.Background(), task.ID, recent, func(*Supervision) bool { return true })
			case "needs_user":
				_, _, _ = s.updateSupervisionAt(context.Background(), task.ID, task.Supervision.UpdatedAt, func(v *Supervision) bool { v.State = reviewNeedsUser; return true })
			case "parked":
				_, _, _ = s.updateSupervisionAt(context.Background(), task.ID, task.Supervision.UpdatedAt, func(v *Supervision) bool { v.State = reviewPending; return true })
			}
			s.superviseWatch(context.Background(), now)
			if countCloseCalls(f) != 0 {
				t.Fatal("closed despite progress or supervisor action")
			}
		})
	}
}

func TestTaskStalledCloseFailureDoesNotMarkTimedOut(t *testing.T) {
	s, f, task, _, now := stalledTaskFixture(t, proto.EngineHerdr, 0)
	f.answerNode("session.close", func(string, json.RawMessage) (any, error) { return nil, errors.New("node refused close") })
	s.superviseWatch(context.Background(), now)
	if got := todoNow(s, task.ID); got.Supervision.state(got) == reviewTimedOut {
		t.Fatal("failed close reported as closed")
	}
}

func TestTaskStalledContinueResumesOriginalSession(t *testing.T) {
	s, f, task, c, now := stalledTaskFixture(t, proto.EngineHerdr, 0)
	ctx := context.Background()
	s.superviseWatch(ctx, now)
	task = todoNow(s, task.ID)
	f.mu.Lock()
	starts := len(f.starts)
	f.mu.Unlock()
	if _, err := s.continueTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "continue original session", func() bool { return len(sendsTo(f, "box", c.SessionID)) == 1 })
	f.mu.Lock()
	after := len(f.starts)
	f.mu.Unlock()
	if starts != after {
		t.Fatal("continuation started another agent")
	}
	got := todoNow(s, task.ID)
	if len(got.Agents) != 1 || got.Agents[0].SessionID != c.SessionID || got.Agents[0].Cwd != task.Agents[0].Cwd || got.Supervision.state(got) != "" {
		t.Fatalf("continuation changed identity: %+v", got)
	}
}
