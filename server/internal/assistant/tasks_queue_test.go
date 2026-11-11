package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

func planQueueTasks(t *testing.T, s *Service, conversation string, scope *leaderScope, n int, role string) (*TaskPlan, string) {
	t.Helper()
	var tasks []map[string]any
	for i := 0; i < n; i++ {
		tasks = append(tasks, map[string]any{"title": fmt.Sprintf("Task %d", i), "prompt": "Read the code and report findings", "cwd": "/code/app", "machine": "box", "readonly": true, "role": role})
	}
	if scope != nil {
		for _, task := range tasks {
			delete(task, "role")
		}
	}
	raw, _ := json.Marshal(map[string]any{"tasks": tasks})
	res, err := s.taskPlanFrom(context.Background(), "box", conversation, raw, scope)
	if err != nil {
		t.Fatal(err)
	}
	plans, err := s.conversationPlans(context.Background(), conversation)
	if err != nil || len(plans) == 0 {
		t.Fatalf("plans: %v %v", plans, err)
	}
	var p *TaskPlan
	for _, v := range plans {
		if p == nil || v.CreatedAt.After(p.CreatedAt) {
			p = v
		}
	}
	return p, res
}

func linkedPlanTasks(s *Service, p *TaskPlan) int {
	n := 0
	for _, pt := range p.Tasks {
		if td, ok := s.todoNow(pt.TodoID); ok && len(td.Agents) > 0 {
			n++
		}
	}
	return n
}

func TestTaskQueueWorkerLimitAndFIFO(t *testing.T) {
	s, f := newQuietService(t)
	parent := startTaskLeader(t, s, f)
	scope := &leaderScope{task: parent, agent: parent.Agents[0]}
	p, res := planQueueTasks(t, s, "box/s1", scope, 6, "")
	if strings.Count(res, "(queued: waiting for capacity)") != 2 {
		t.Fatalf("receipt: %s", res)
	}
	waitFor(t, "four workers", func() bool { return linkedPlanTasks(s, p) == 4 })
	for i, pt := range p.Tasks {
		td := todoNow(s, pt.TodoID)
		if i >= 4 && (len(td.Agents) != 0 || td.Supervision.state(td) != reviewQueued || taskState(td, nil) != "queued") {
			t.Fatalf("queued worker %d: %+v", i, td)
		}
	}
	if _, err := s.reviewDoneTodo(context.Background(), p.Tasks[0].TodoID, "Verified", true); err != nil {
		t.Fatal(err)
	}
	s.superviseWatch(context.Background(), time.Now())
	waitFor(t, "fifth worker starts after acceptance", func() bool { return len(todoNow(s, p.Tasks[4].TodoID).Agents) == 1 })
	if len(todoNow(s, p.Tasks[5].TodoID).Agents) != 0 {
		t.Fatal("sixth worker jumped FIFO")
	}
	if _, err := s.reviewDoneTodo(context.Background(), p.Tasks[1].TodoID, "Cancelled", false); err != nil {
		t.Fatal(err)
	}
	s.superviseWatch(context.Background(), time.Now())
	waitFor(t, "sixth worker starts after cancellation", func() bool { return len(todoNow(s, p.Tasks[5].TodoID).Agents) == 1 })
}

func TestTaskQueueGlobalLeaderLimitAndLiveSettings(t *testing.T) {
	s, _ := newQuietService(t)
	p, res := planQueueTasks(t, s, "conv1", nil, 8, roleLeader)
	if strings.Count(res, "(queued: waiting for capacity)") != 2 {
		t.Fatalf("receipt: %s", res)
	}
	waitFor(t, "six leaders", func() bool { return linkedPlanTasks(s, p) == 6 })
	if _, err := s.reviewDoneTodo(context.Background(), p.Tasks[0].TodoID, "Verified", true); err != nil {
		t.Fatal(err)
	}
	s.superviseWatch(context.Background(), time.Now())
	waitFor(t, "seventh leader", func() bool { return len(todoNow(s, p.Tasks[6].TodoID).Agents) == 1 })
	if len(todoNow(s, p.Tasks[7].TodoID).Agents) != 0 {
		t.Fatal("last leader jumped FIFO")
	}
	setSettings(t, s, func(v *Settings) { v.TaskConcurrency = &TaskConcurrency{WorkersPerTask: 4, Leaders: 7} })
	s.superviseWatch(context.Background(), time.Now())
	waitFor(t, "eighth leader after live limit change", func() bool { return len(todoNow(s, p.Tasks[7].TodoID).Agents) == 1 })
}

func TestTaskQueueConcurrentPlansRespectReservation(t *testing.T) {
	s, _ := newQuietService(t)
	setSettings(t, s, func(v *Settings) { v.TaskConcurrency = &TaskConcurrency{WorkersPerTask: 4, Leaders: 1} })
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		p := &TaskPlan{ID: fmt.Sprintf("plan_race%d", i), Conversation: "conv1", State: planOpen, CreatedAt: time.Now(), Tasks: []PlanTask{{Title: "Read code", Prompt: "Read code", Machine: "box", Cwd: "/code/app", Role: roleLeader, Readonly: true}}}
		if err := s.savePlan(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := s.startPlan(ctx, fmt.Sprintf("plan_race%d", i), nil, false); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	waitFor(t, "one leader link", func() bool {
		n := 0
		for _, td := range s.todosOf(ctx, "*") {
			n += len(td.Agents)
		}
		return n == 1
	})
	queued := 0
	for _, td := range s.todosOf(ctx, "*") {
		if td.Supervision.state(td) == reviewQueued {
			queued++
		}
	}
	if queued != 1 {
		t.Fatalf("queued=%d", queued)
	}
}

func TestTaskQueueCancelledTicketNeverStartsAndRetirementReleases(t *testing.T) {
	s, f := newQuietService(t)
	setSettings(t, s, func(v *Settings) { v.TaskConcurrency = &TaskConcurrency{WorkersPerTask: 4, Leaders: 1} })
	p, _ := planQueueTasks(t, s, "conv1", nil, 3, roleLeader)
	waitFor(t, "first leader", func() bool { return linkedPlanTasks(s, p) == 1 })
	if _, err := s.reviewDoneTodo(context.Background(), p.Tasks[1].TodoID, "Cancelled", false); err != nil {
		t.Fatal(err)
	}
	first := todoNow(s, p.Tasks[0].TodoID).Agents[0]
	f.setSessions("box", proto.Session{MachineID: "box", SessionID: first.SessionID, Host: proto.EngineHerdr, Status: proto.StatusOffline, UpdatedAt: time.Now().Add(time.Minute)})
	s.superviseWatch(context.Background(), time.Now())
	waitFor(t, "third leader after retirement", func() bool { return len(todoNow(s, p.Tasks[2].TodoID).Agents) == 1 })
	if len(todoNow(s, p.Tasks[1].TodoID).Agents) != 0 {
		t.Fatal("cancelled ticket started")
	}
}

func TestTaskConcurrencySettingValidationAndReload(t *testing.T) {
	st := &memStore{}
	s, f := newTaskServiceOn(t, st)
	for _, body := range []string{`{"task_concurrency":{"workers_per_task":0,"leaders":6}}`, `{"task_concurrency":{"workers_per_task":4,"leaders":-1}}`} {
		w, _ := permissionSettingsRequest(t, s, "PUT", "/api/v1/assistant", "assistant", "full", body)
		if w.Code != 400 {
			t.Fatalf("%d: %s", w.Code, w.Body)
		}
	}
	w, view := permissionSettingsRequest(t, s, "PUT", "/api/v1/assistant", "assistant", "full", `{"task_concurrency":{"workers_per_task":2,"leaders":3}}`)
	if w.Code != 200 || *view.TaskConcurrency != (TaskConcurrency{2, 3}) {
		t.Fatalf("%d: %s", w.Code, w.Body)
	}
	f.settings = nil
	reloaded := newServiceOn(t, f, st)
	if got := *reloaded.settings(context.Background()).TaskConcurrency; got != (TaskConcurrency{2, 3}) {
		t.Fatalf("reloaded: %+v", got)
	}
}

func TestTaskQueueSurvivesRestartWithoutInterruptedStartFailure(t *testing.T) {
	database := todoTestDB(t)
	s, f := newQuietServiceOn(t, database)
	setSettings(t, s, func(v *Settings) { v.TaskConcurrency = &TaskConcurrency{WorkersPerTask: 4, Leaders: 1} })
	p, _ := planQueueTasks(t, s, "conv1", nil, 3, roleLeader)
	waitFor(t, "first persisted leader", func() bool { return linkedPlanTasks(s, p) == 1 })
	f.settings = nil
	restarted := newServiceOn(t, f, database)
	if err := restarted.recoverTaskStarts(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, pt := range p.Tasks[1:] {
		td := todoNow(restarted, pt.TodoID)
		if td.DoneAt != nil || td.StartError != "" || td.Supervision.state(td) != reviewQueued {
			t.Fatalf("queued task failed recovery: %+v", td)
		}
	}
	if _, err := restarted.reviewDoneTodo(context.Background(), p.Tasks[0].TodoID, "Verified", true); err != nil {
		t.Fatal(err)
	}
	restarted.superviseWatch(context.Background(), time.Now())
	waitFor(t, "FIFO after restart", func() bool { return len(todoNow(restarted, p.Tasks[1].TodoID).Agents) == 1 })
	if len(todoNow(restarted, p.Tasks[2].TodoID).Agents) != 0 {
		t.Fatal("restart lost FIFO order")
	}
}
