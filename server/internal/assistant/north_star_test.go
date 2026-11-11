package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// northStarDB is an isolated database with the main conversation "main-chat" and the leader conversation "dot-chat".
func northStarDB(t *testing.T) (*Service, *db.DB, string) {
	t.Helper()
	dsn := newDatabase(t)
	database, err := db.Open(context.Background(), dsn, db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	s, _ := newQuietServiceOn(t, database)
	setSettings(t, s, func(v *Settings) { v.Main, v.Leaders = "main-chat", []string{"dot-chat"} })
	return s, database, dsn
}

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func shanghai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	mustDo(t, err)
	return loc
}

func TestNorthStarInterruptionsAndAvailability(t *testing.T) {
	ctx := context.Background()
	s, database, _ := northStarDB(t)
	loc := shanghai(t)
	midnight := time.Date(2026, 11, 11, 0, 0, 0, 0, loc)
	now := midnight.Add(15 * time.Hour)
	mustDo(t, database.PutSetting(ctx, northStarSinceKey, northStarSince{At: midnight.Add(-time.Hour), Build: "b1"}))

	at := func(h int) time.Time { return midnight.Add(time.Duration(h) * time.Hour) }
	cards := []struct {
		ix                  proto.Interaction
		told, auto          time.Time
		todo                string
		wantCategory, kind  string
		wantCounted, routed bool
	}{
		{ix: proto.Interaction{ID: "ask", MachineID: "pc", SessionID: "main-chat", Kind: proto.IxQuestion, ToolName: "ask_user", CreatedAt: at(9)}, told: at(9)},
		{ix: proto.Interaction{ID: "worker-question", MachineID: "pc", SessionID: "worker", SessionTitle: "Fix the build", Kind: proto.IxQuestion, ToolName: "AskUserQuestion", CreatedAt: at(10)}, told: at(10), todo: "task-1"},
		{ix: proto.Interaction{ID: "tool", MachineID: "pc", SessionID: "loose", Kind: proto.IxPermission, ToolName: "Bash", CreatedAt: at(11)}, told: at(11)},
		{ix: proto.Interaction{ID: "standing", MachineID: "pc", SessionID: "worker", Kind: proto.IxPlan, CreatedAt: at(11)}, auto: at(11), todo: "task-1"},
		{ix: proto.Interaction{ID: "held", MachineID: "pc", SessionID: "worker", Kind: proto.IxQuestion, CreatedAt: at(12)}},
		{ix: proto.Interaction{ID: "escalated", MachineID: "pc", SessionID: "worker", Kind: proto.IxQuestion, CreatedAt: at(12)}, told: at(13), todo: "task-1"},
		{ix: proto.Interaction{ID: "probe", MachineID: "pc", SessionID: "probe", Kind: proto.IxPermission, Cwd: "/tmp/persona-live-1/agent", CreatedAt: at(12)}, told: at(12)},
		{ix: proto.Interaction{ID: "yesterday", MachineID: "pc", SessionID: "loose", Kind: proto.IxScreen, CreatedAt: at(-3)}, told: at(-3)},
	}
	for _, c := range cards {
		mustDo(t, database.InsertInteraction(ctx, c.ix))
		if !c.told.IsZero() {
			mustDo(t, database.MarkConfirmationNotified(ctx, c.ix.ID, c.ix.CreatedAt, c.told, c.todo))
		}
		if !c.auto.IsZero() {
			mustDo(t, database.MarkConfirmationAutoAuthorized(ctx, c.ix.ID, c.ix.CreatedAt, c.auto, c.todo, "grant"))
		}
	}
	events := []db.WorkEvent{
		{At: at(12), MachineID: "pc", SessionID: "worker", Kind: "ask_routed", Detail: map[string]any{"id": "held", "kind": "question"}},
		{At: at(12), MachineID: "pc", SessionID: "worker", Kind: "ask_routed", Detail: map[string]any{"id": "escalated", "kind": "question"}},
		{At: at(13), MachineID: "pc", SessionID: "worker", Kind: "ask_escalated", Detail: map[string]any{"todo": "task-1"}},
		{At: at(14), MachineID: "pc", SessionID: "dot-chat", Kind: notifyEvent, Detail: map[string]any{}},
		{At: at(14), MachineID: "pc", SessionID: "probe", Kind: notifyEvent, Detail: map[string]any{"probe": true}},
		// Availability: the main conversation before the split (build b1), the leader conversation after it (b2).
		{At: at(8), MachineID: "pc", SessionID: "main-chat", Kind: sendEvent, Detail: map[string]any{"op": "resume", "origin": originApp, "role": roleMain, "outcome": outcomeOK, "build": "b1"}},
		{At: at(9), MachineID: "pc", SessionID: "main-chat", Kind: sendEvent, Detail: map[string]any{"op": "resume", "origin": originApp, "role": roleMain, "outcome": outcomeFailed, "reason": "conversation_not_found", "build": "b1"}},
		{At: at(10), MachineID: "pc", SessionID: "main-chat", Kind: sendEvent, Detail: map[string]any{"op": "resume", "origin": originManager, "role": roleMain, "outcome": outcomeFailed, "reason": "conversation_not_found", "build": "b1"}},
		{At: at(13), MachineID: "pc", SessionID: "dot-chat", Kind: sendEvent, Detail: map[string]any{"op": "resume", "origin": originManager, "role": roleDot, "outcome": outcomeOK, "build": "b2"}},
		{At: at(14), MachineID: "pc", SessionID: "", Kind: sendEvent, Detail: map[string]any{"op": "start", "origin": originManager, "role": roleMain, "outcome": outcomeFailed, "reason": "timeout", "build": "b2"}},
		{At: at(14), MachineID: "pc", SessionID: "dot-chat", Kind: sendEvent, Detail: map[string]any{"op": "resume", "origin": originApp, "role": roleDot, "outcome": outcomeUnknown, "build": "b2"}},
		{At: at(-2), MachineID: "pc", SessionID: "dot-chat", Kind: sendEvent, Detail: map[string]any{"op": "resume", "origin": originApp, "role": roleDot, "outcome": outcomeFailed, "reason": "machine_offline", "build": "b1"}},
	}
	for _, e := range events {
		mustDo(t, database.AddWorkEvent(ctx, e))
	}

	ns, err := s.northStarAt(ctx, midnight, now, at(12), loc)
	mustDo(t, err)
	in := ns.Interruptions
	if in.Total != 5 || in.Partial {
		t.Fatalf("interruptions total %d partial %v: %+v", in.Total, in.Partial, in)
	}
	got := map[string]int{}
	for c, v := range in.Categories {
		got[c] = v.Count
	}
	want := map[string]int{nsAskUser: 1, nsActionCard: 2, nsNotifyUser: 1, nsAuthorization: 1}
	for c, n := range want {
		if got[c] != n {
			t.Fatalf("category %s = %d, want %d (%v)", c, got[c], n, got)
		}
	}
	if in.Categories[nsAskUser].BySource[nsFromDot] != 1 || in.Categories[nsActionCard].BySource[nsFromTask] != 2 ||
		in.Categories[nsAuthorization].BySource[nsFromSession] != 1 || in.Categories[nsNotifyUser].BySource[nsFromDot] != 1 {
		t.Fatalf("sources: %+v %+v %+v %+v", in.Categories[nsAskUser], in.Categories[nsActionCard], in.Categories[nsAuthorization], in.Categories[nsNotifyUser])
	}
	if in.Categories[nsAuthorization].ByKind[proto.IxPermission] != 1 || in.Categories[nsActionCard].ByKind[proto.IxQuestion] != 2 {
		t.Fatalf("kinds: %+v %+v", in.Categories[nsAuthorization], in.Categories[nsActionCard])
	}
	wantExcluded := InterruptExcluded{SupervisorRouted: 2, SupervisorHeld: 1, SupervisorEscalated: 1, AutoAuthorized: 1, Probe: 2}
	if in.Excluded != wantExcluded {
		t.Fatalf("excluded %+v, want %+v", in.Excluded, wantExcluded)
	}
	if len(in.Sources) == 0 || in.Sources[0].Count != 2 || in.Sources[0].TodoID != "task-1" || in.Sources[0].Title != "Fix the build" {
		t.Fatalf("source rows: %+v", in.Sources)
	}

	av := ns.Availability
	if av.Attempts != 6 || av.OK != 2 || av.Failed != 3 || av.Unknown != 1 || av.Rate == nil || percent(*av.Rate) != "33.3%" {
		t.Fatalf("availability: %+v", av.Tally)
	}
	if len(av.Failures) != 2 || av.Failures[0] != (FailureReason{"conversation_not_found", 2}) || av.Failures[1] != (FailureReason{"timeout", 1}) {
		t.Fatalf("failure reasons: %+v", av.Failures)
	}
	if av.Split == nil || av.Split.Before.Attempts != 3 || av.Split.Before.OK != 1 || av.Split.After.Attempts != 3 || av.Split.After.OK != 1 {
		t.Fatalf("split: %+v", av.Split)
	}
	if av.ByBuild["b1"].Attempts != 3 || av.ByBuild["b2"].Attempts != 3 || av.ByOp["start"].Failed != 1 || av.ByRole[roleDot].Attempts != 2 ||
		av.ByOrigin[originApp].Attempts != 3 {
		t.Fatalf("groups: build %+v op %+v role %+v origin %+v", av.ByBuild, av.ByOp, av.ByRole, av.ByOrigin)
	}
	if len(av.Gaps) != 0 || len(av.Conversations) != 2 {
		t.Fatalf("gaps %v conversations %v", av.Gaps, av.Conversations)
	}

	// The route answers the same, to a full device only.
	w := serve(s, "GET", "/api/v1/assistant/north-star?date=2026-11-11&tz=Asia/Shanghai&split="+url.QueryEscape(at(12).Format(time.RFC3339)), "")
	if w.Code != http.StatusOK {
		t.Fatalf("route: %d %s", w.Code, w.Body)
	}
	var routed NorthStar
	mustDo(t, json.Unmarshal(w.Body.Bytes(), &routed))
	if routed.Date != "2026-11-11" || routed.Interruptions.Total != 5 || routed.Availability.Attempts != 6 || routed.Availability.Split.After.Attempts != 3 {
		t.Fatalf("route body: %s", w.Body)
	}
	t.Logf("NORTH_STAR_ROUTE %s", w.Body)
	if w := serveAs(s, "answer", "GET", "/api/v1/assistant/north-star", ""); w.Code != http.StatusForbidden {
		t.Fatalf("limited role: %d", w.Code)
	}
	for _, q := range []string{"date=2026-13-01", "split=yesterday", "tz=Nowhere/City", "date=2999-01-01"} {
		if w := serve(s, "GET", "/api/v1/assistant/north-star?"+q, ""); w.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", q, w.Code)
		}
	}
}

func TestNorthStarBeforeRecordingIsUnknown(t *testing.T) {
	ctx := context.Background()
	s, database, _ := northStarDB(t)
	loc := shanghai(t)
	midnight := time.Date(2026, 11, 11, 0, 0, 0, 0, loc)
	now := midnight.Add(15 * time.Hour)
	// Cards without markers (raised before recording began) and the scheduled runs are all there is.
	for _, ix := range []proto.Interaction{
		{ID: "old-ask", MachineID: "pc", SessionID: "main-chat", Kind: proto.IxQuestion, ToolName: "ask_user", CreatedAt: midnight.Add(time.Hour)},
		{ID: "old-tool", MachineID: "pc", SessionID: "worker", Kind: proto.IxPermission, CreatedAt: midnight.Add(2 * time.Hour)},
		{ID: "old-probe", MachineID: "pc", SessionID: "probe", Kind: proto.IxScreen, Cwd: "/tmp/live", CreatedAt: midnight.Add(2 * time.Hour)},
	} {
		mustDo(t, database.InsertInteraction(ctx, ix))
	}
	mustDo(t, database.PutSetting(ctx, runsKey, []map[string]any{
		{"at": midnight.Add(3 * time.Hour), "status": runOK, "session_id": "dot-chat"},
		{"at": midnight.Add(4 * time.Hour), "status": runFailed, "session_id": "dot-chat", "error": "No conversation found with session ID x"},
		{"at": midnight.Add(5 * time.Hour), "status": runSkipped, "session_id": "dot-chat"},
		{"at": midnight.Add(5 * time.Hour), "status": runOK, "session_id": "another-routine"},
	}))
	st := s.store().(northStarStore)
	extra := []nsAttempt{{At: midnight.Add(6 * time.Hour), SessionID: "main-chat", Role: roleMain, Op: "resume", Origin: originAppOps, Outcome: outcomeFailed, Reason: "conversation_not_found"}}
	ns, err := readNorthStar(ctx, st, northStarQuery{Since: midnight, Until: now, Loc: loc, Extra: extra})
	mustDo(t, err)
	if ns.RecordingSince != nil || !ns.Interruptions.Partial || ns.Interruptions.Total != 0 ||
		ns.Interruptions.Categories[nsAskUser].Unknown != 1 || ns.Interruptions.Categories[nsAuthorization].Unknown != 1 || ns.Interruptions.Categories[nsActionCard].Unknown != 0 {
		t.Fatalf("unmarked cards must stay unknown, never zero or guessed: %+v", ns.Interruptions)
	}
	av := ns.Availability
	if av.Attempts != 3 || av.OK != 1 || av.Failed != 2 || av.ByOrigin[originScheduledRun].Attempts != 2 || av.ByOrigin[originAppOps].Attempts != 1 ||
		av.Failures[0] != (FailureReason{"conversation_not_found", 2}) || av.ByBuild["before_recording"].Attempts != 3 {
		t.Fatalf("baseline availability: %+v", av)
	}
	if strings.Join(av.Gaps, ",") != "manager_sends_before_recording" {
		t.Fatalf("gaps: %v", av.Gaps)
	}
	// A full run list that starts after the window's start lost the window's first runs: said, not hidden.
	full := make([]map[string]any, maxRuns)
	for i := range full {
		full[i] = map[string]any{"at": midnight.Add(time.Duration(10+i) * time.Second), "status": runOK, "session_id": "dot-chat"}
	}
	mustDo(t, database.PutSetting(ctx, runsKey, full))
	cut, err := readNorthStar(ctx, st, northStarQuery{Since: midnight, Until: now, Loc: loc})
	mustDo(t, err)
	if strings.Join(cut.Availability.Gaps, ",") != "manager_sends_before_recording,app_sends_before_recording,scheduled_runs_truncated" {
		t.Fatalf("truncated runs: %v", cut.Availability.Gaps)
	}
	lines := briefNorthStar{Today: ns, todayOK: true}.lines(i18n.Lang("en"))
	if lines[0] != "Interruptions today: unknown (not recorded yet); no comparison with yesterday" {
		t.Fatalf("unrecorded interruptions line: %q", lines[0])
	}
}

func TestNorthStarRecording(t *testing.T) {
	ctx := context.Background()
	s, database, _ := northStarDB(t)
	s.build = "test-build"
	before := time.Now().Add(-time.Second)
	s.northStarStarted(ctx)
	var since northStarSince
	if ok, err := database.Setting(ctx, northStarSinceKey, &since); err != nil || !ok || since.Build != "test-build" || since.At.Before(before) {
		t.Fatalf("recording start: %v %v %+v", ok, err, since)
	}
	first := since.At
	s.build = "later-build"
	s.northStarStarted(ctx) // a later build keeps when recording first began
	mustDo(t, func() error { _, err := database.Setting(ctx, northStarSinceKey, &since); return err }())
	if !since.At.Equal(first) || since.Build != "test-build" {
		t.Fatalf("recording start moved: %+v", since)
	}

	// The manager's own sends: a new main conversation that failed to start, a resume of the leader conversation, and a
	// conversation that is neither (not kept).
	s.recordManagerSend("pc", map[string]any{"is_main": true, "text": "hi"}, nil, &rpc.Error{Code: "timeout", Message: "context deadline exceeded"})
	s.recordManagerSend("pc", map[string]any{"session_id": "dot-chat"}, []byte(`{}`), nil)
	s.recordManagerSend("pc", map[string]any{"session_id": "routine-chat"}, []byte(`{}`), nil)
	// An app's messages, as the hub tells them.
	s.ConversationSent(ctx, ConversationSent{MachineID: "pc", SessionID: "main-chat", Outcome: assistantwire.ConversationFailed, Code: "internal",
		Error: "claude exited: No conversation found with session ID: main-chat"})
	s.ConversationSent(ctx, ConversationSent{MachineID: "pc", SessionID: "main-chat", Outcome: assistantwire.ConversationUnknown})
	s.ConversationSent(ctx, ConversationSent{MachineID: "pc", SessionID: "worker", Outcome: assistantwire.ConversationDelivered})
	// notify_user from the leader conversation.
	if _, err := s.runTool(ctx, "pc", "dot-chat", "notify_user", json.RawMessage(`{"title":"Done","body":"PR merged"}`)); err != nil {
		t.Fatal(err)
	}
	s.waitWorkEvents()
	ns, err := s.northStarAt(ctx, before, time.Now(), time.Time{}, time.UTC)
	mustDo(t, err)
	av := ns.Availability
	if av.Attempts != 4 || av.OK != 1 || av.Failed != 2 || av.Unknown != 1 || av.ByOp["start"].Failed != 1 || av.ByOrigin[originApp].Attempts != 2 ||
		av.ByBuild["later-build"].Attempts != 4 {
		t.Fatalf("recorded availability: %+v by op %+v origin %+v build %+v", av.Tally, av.ByOp, av.ByOrigin, av.ByBuild)
	}
	if len(av.Failures) != 2 || av.Failures[0] != (FailureReason{"conversation_not_found", 1}) || av.Failures[1] != (FailureReason{"timeout", 1}) {
		t.Fatalf("recorded reasons: %+v", av.Failures)
	}
	if n := ns.Interruptions.Categories[nsNotifyUser]; n.Count != 1 || n.BySource[nsFromDot] != 1 {
		t.Fatalf("notify_user: %+v", n)
	}
}

func TestNorthStarBriefingOpensWithIt(t *testing.T) {
	ctx := context.Background()
	s, database, _ := northStarDB(t)
	loc := shanghai(t)
	setSettings(t, s, func(v *Settings) { v.Timezone = "Asia/Shanghai" })
	midnight := time.Date(2026, 11, 11, 0, 0, 0, 0, loc)
	now := midnight.Add(9 * time.Hour)
	mustDo(t, database.PutSetting(ctx, northStarSinceKey, northStarSince{At: midnight.Add(-48 * time.Hour), Build: "b1"}))
	ev := func(at time.Time, kind, session string, detail map[string]any) {
		mustDo(t, database.AddWorkEvent(ctx, db.WorkEvent{At: at, MachineID: "pc", SessionID: session, Kind: kind, Detail: detail}))
	}
	ok := map[string]any{"op": "resume", "origin": originApp, "role": roleMain, "outcome": outcomeOK}
	bad := map[string]any{"op": "resume", "origin": originApp, "role": roleMain, "outcome": outcomeFailed, "reason": "conversation_not_found"}
	// Yesterday until 09:00: three notifications, one of two resumes worked; after 09:00 yesterday does not count.
	for _, h := range []int{-20, -18, -16} {
		ev(midnight.Add(time.Duration(h)*time.Hour), notifyEvent, "dot-chat", nil)
	}
	ev(midnight.Add(-20*time.Hour), sendEvent, "main-chat", ok)
	ev(midnight.Add(-19*time.Hour), sendEvent, "main-chat", bad)
	ev(midnight.Add(-10*time.Hour), notifyEvent, "dot-chat", nil)
	// Today: one notification, three of four resumes worked.
	ev(midnight.Add(2*time.Hour), notifyEvent, "dot-chat", nil)
	for _, h := range []int{1, 2, 3} {
		ev(midnight.Add(time.Duration(h)*time.Hour), sendEvent, "main-chat", ok)
	}
	ev(midnight.Add(4*time.Hour), sendEvent, "main-chat", bad)

	f := s.briefFactsAt(ctx, now, "")
	f.l = i18n.Lang("en")
	f.NorthStarLines = f.NorthStar.lines(f.l)
	want := []string{
		"Interruptions today: 1 (0 questions · 0 cards to act on · 1 notifications · 0 authorization cards); yesterday by this time 3, -2",
		"Availability today (main and leader conversations): 75% (3 of 4 starts and resumes worked); yesterday by this time 50%, +25 pp",
		"Failures by reason: conversation_not_found 1",
		"Source: Persona's own records: cards and notifications that reached you (cards a supervisor took, standing authorizations and test probes excluded), and every start and resume of the main and leader conversations.",
	}
	if strings.Join(f.NorthStarLines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("north star lines:\n%s", strings.Join(f.NorthStarLines, "\n"))
	}
	f.ConfirmationLines = s.confirmationLines(f.l, f.Confirmations)
	card, plain, _ := s.briefCard(f, "Daily report", "")
	head := strings.Index(plain, "North star")
	if head < 0 || head > strings.Index(plain, want[0]) || strings.Index(plain, want[3]) > strings.Index(plain, "Today's confirmation cards") {
		t.Fatalf("the card does not open with the north star:\n%s", plain)
	}
	if !strings.Contains(card.JSON(), "75%") {
		t.Fatalf("card JSON lacks the rate: %s", card.JSON())
	}
	model := f.modelText()
	if !strings.HasPrefix(model[strings.Index(model, "\n")+1:], "- 北极星指标") || !strings.Contains(model, want[1]) {
		t.Fatalf("model facts do not open with the north star:\n%s", model)
	}
	body, err := json.Marshal(f.body(s, "Daily report"))
	mustDo(t, err)
	if !strings.Contains(string(body), `"north_star":{"lines":[`) || !strings.Contains(string(body), `"interruptions":1,"interruptions_yesterday":3`) ||
		!strings.Contains(string(body), `"availability":0.75,"availability_yesterday":0.5,"attempts":4,"ok":3`) {
		t.Fatalf("feed north star: %s", body)
	}
	t.Logf("NORTH_STAR_BRIEFING\n%s", plain)
}

type failingNorthStarStore struct{ Store }

func (failingNorthStarStore) Setting(context.Context, string, any) (bool, error) {
	return false, errors.New("unavailable")
}
func (failingNorthStarStore) NorthStarCards(context.Context, time.Time, time.Time) ([]db.NorthStarCard, error) {
	return nil, errors.New("unavailable")
}
func (failingNorthStarStore) WorkEventsBetween(context.Context, time.Time, time.Time, []string) ([]db.WorkEvent, error) {
	return nil, errors.New("unavailable")
}

func TestNorthStarUnreadableIsUnknown(t *testing.T) {
	s := newFleetTest(t).s
	noDirectionsTimers(s)
	base := s.store()
	s.store = func() Store { return failingNorthStarStore{Store: base} }
	b := s.briefNorthStarAt(context.Background(), time.Now(), time.UTC)
	if !b.Unknown || strings.Join(b.lines(i18n.Lang("en")), "") != "North star: today's numbers could not be read; they are unknown." {
		t.Fatalf("a read failure became numbers: %+v", b)
	}
	if body := b.body(nil); body.Interruptions != nil || body.Availability != nil || len(body.Lines) != 0 {
		t.Fatalf("unknown body: %+v", body)
	}
}

func TestNorthStarHelpers(t *testing.T) {
	for _, c := range []struct{ code, msg, want string }{
		{"internal", "assistant exited: No conversation found with session ID: x", "conversation_not_found"},
		{"timeout", "", "timeout"},
		{"node_unreachable", "", "machine_offline"},
		{"", "context canceled", "cancelled"},
		{"claude_missing", "", "claude_missing"},
		{"internal", "boom", "internal"},
	} {
		if got := sendReason(c.code, c.msg); got != c.want {
			t.Errorf("sendReason(%q, %q) = %q, want %q", c.code, c.msg, got, c.want)
		}
	}
	for dir, want := range map[string]bool{"/tmp": true, "/tmp/x/y": true, "/private/tmp/a": true, "/var/folders/ab/T/x": true, "/tmpfoo": false, "/home/me/code": false, "": false} {
		if probeDir(dir) != want {
			t.Errorf("probeDir(%q) != %v", dir, want)
		}
	}
	if percent(1) != "100%" || percent(2.0/3) != "66.7%" || signedPoints(0.25) != "+25 pp" || signedPoints(-0.0001) != "0 pp" || signed(-2) != "-2" {
		t.Fatal("formatting")
	}
}

func TestGoldenNorthStarBriefFacts(t *testing.T) {
	golden(t, "routine_facts.north_star", prompts.RenderBriefFacts(prompts.BriefFacts{Hours: 12, NorthStar: []string{
		"Interruptions today: 1 (0 questions · 0 cards to act on · 1 notifications · 0 authorization cards); yesterday by this time 3, -2",
		"Availability today (main and leader conversations): 75% (3 of 4 starts and resumes worked); yesterday by this time 50%, +25 pp",
	}}))
}

func TestNorthStarCommandReadsReadOnly(t *testing.T) {
	ctx := context.Background()
	s, _, dsn := northStarDB(t)
	setSettings(t, s, func(v *Settings) { v.Timezone = "Asia/Shanghai" })
	loc := shanghai(t)
	hubURL := newDatabase(t)
	hubDB, err := db.Open(ctx, hubURL, db.HubSet)
	mustDo(t, err)
	t.Cleanup(hubDB.Close)
	// The hub's records of three app messages: one to the main conversation that failed to resume, one that went
	// through to the leader conversation, and one to an Agent (not read).
	ops := []struct{ id, path, state, body string }{
		{"00000000-0000-0000-0000-000000000001", "/api/v1/sessions/pc/main-chat/messages", "unknown", `{"code":"internal","detail":"claude exited: No conversation found with session ID: main-chat"}`},
		{"00000000-0000-0000-0000-000000000002", "/api/v1/sessions/pc/dot-chat/messages", "done", `{}`},
		{"00000000-0000-0000-0000-000000000003", "/api/v1/sessions/pc/worker/messages", "failed", `{"code":"offline"}`},
	}
	for _, op := range ops {
		target, _ := json.Marshal(map[string]string{"path": op.path, "query": ""})
		_, _, err := hubDB.BeginClientOp(ctx, db.ClientOp{ID: op.id, DeviceID: "phone", Kind: "message", Target: target, Digest: "d"})
		mustDo(t, err)
		result, _ := json.Marshal(map[string]any{"status": 200, "body": []byte(op.body)})
		mustDo(t, hubDB.FinishClientOp(ctx, op.id, op.state, result))
	}
	ro, err := attachReadOnly(ctx, dsn, db.ManagerSet)
	mustDo(t, err)
	t.Cleanup(ro.Close)
	roHub, err := attachReadOnly(ctx, hubURL, db.HubSet)
	mustDo(t, err)
	t.Cleanup(roHub.Close)
	if err := ro.PutSetting(ctx, "north-star-write-probe", 1); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("the command's connection could write: %v", err)
	}
	ns, err := northStarCommand(ctx, ro, roHub, "", "", "", time.Now())
	mustDo(t, err)
	if ns.TimeZone != loc.String() || ns.RecordingSince != nil {
		t.Fatalf("zone %s since %v", ns.TimeZone, ns.RecordingSince)
	}
	av := ns.Availability
	if av.Attempts != 2 || av.OK != 1 || av.Failed != 1 || av.ByOrigin[originAppOps].Attempts != 2 || av.Failures[0] != (FailureReason{"conversation_not_found", 1}) {
		t.Fatalf("hub app messages: %+v %+v", av.Tally, av.Failures)
	}
	if _, err := northStarCommand(ctx, ro, roHub, "tomorrow", "", "", time.Now()); err == nil {
		t.Fatal("a bad date was read")
	}
}

// A dot's daily report (a daily task in a leader conversation, with no facts of its own) opens with the north star;
// a daily task elsewhere does not.
func TestNorthStarOpensADotsDailyReport(t *testing.T) {
	ctx := context.Background()
	s, database, _ := northStarDB(t)
	now := time.Now()
	mustDo(t, database.PutSetting(ctx, northStarSinceKey, northStarSince{At: now.Add(-72 * time.Hour)}))
	mustDo(t, database.AddWorkEvent(ctx, db.WorkEvent{At: now.Add(-time.Minute), MachineID: "pc", SessionID: "dot-chat", Kind: sendEvent,
		Detail: map[string]any{"op": "resume", "origin": originManager, "role": roleDot, "outcome": outcomeOK}}))
	facts := s.northStarRunFacts(ctx, "dot-chat", now)
	if !strings.HasPrefix(facts, "Persona 算好的北极星指标") || !strings.Contains(facts, "\n- ") || !strings.Contains(facts, "100%") {
		t.Fatalf("dot report facts:\n%s", facts)
	}
	if got := s.northStarRunFacts(ctx, "routine-chat", now); got != "" {
		t.Fatalf("another conversation's daily task got the north star: %q", got)
	}
	task := Task{ID: "fu_report", Title: "Dot daily report", Repeat: repeatDaily, SessionID: "dot-chat", Prompt: "Write today's report.", factsText: facts}
	prompt := taskPrompt(task, now, now, time.UTC, false, false, false)
	if i, j := strings.Index(prompt, "北极星指标"), strings.Index(prompt, "Write today's report."); i < 0 || i > j {
		t.Fatalf("the report does not open with the north star:\n%s", prompt)
	}
	t.Logf("DOT_REPORT_PROMPT\n%s", prompt)
}

func TestGoldenNorthStarRunFacts(t *testing.T) {
	golden(t, "north_star_facts", prompts.RenderNorthStarFacts([]string{
		"Interruptions today: 1 (0 questions · 0 cards to act on · 1 notifications · 0 authorization cards); yesterday by this time 3, -2",
		"Availability today (main and leader conversations): 75% (3 of 4 starts and resumes worked); yesterday by this time 50%, +25 pp",
	}))
}
