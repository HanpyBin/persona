package assistant

import (
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// fleetInputsOf is a manager whose machines each list sessions, every tenth one working.
func fleetInputsOf(machines, sessions int) *Service {
	d := newFleetDevs()
	for m := range machines {
		id := fmt.Sprintf("m%02d", m)
		d.add(id, id)
		list := make([]proto.Session, sessions)
		for i := range list {
			st := proto.StatusDone
			if i%10 == 0 {
				st = proto.StatusWorking
			}
			list[i] = proto.Session{SessionID: fmt.Sprintf("s%03d", i), Cwd: "/code/app", Agent: "claude", Status: st,
				PaneID: fmt.Sprintf("p%d", i), SpaceName: "app", Title: "a session", LastText: "done"}
		}
		d.setSessions(id, list)
	}
	return New(Options{Hub: newFakeHub(), Logf: func(string, ...any) {}, Devices: d})
}

// BenchmarkFleetInputs is one of liveLoop's rounds while the fleet changes, ten machines of 200 sessions: the agents
// over the lists the last round left, the live fleet, Persona Link's summary.
func BenchmarkFleetInputs(b *testing.B) {
	s := fleetInputsOf(10, 200)
	lists := map[string][]proto.Session{}
	b.ReportAllocs()
	for b.Loop() {
		agents, ixs, told := s.fleetInputsInto(lists)
		_ = buildFleetIn("en", agents, ixs)
		_ = linkAttention("en", agents, ixs, told)
	}
}

// liveLoop's round over the last round's lists copies the 2,000 sessions into them: 280 allocations and 390 KB
// measured in BenchmarkFleetInputs (290 and 3.49 MB when every round decorated new copies).
func TestFleetInputsIntoAllocations(t *testing.T) {
	if raceEnabled {
		t.Skip("race instrumentation allocates")
	}
	if !isolated(t) {
		return
	}
	s := fleetInputsOf(10, 200)
	lists := map[string][]proto.Session{}
	round := func() {
		agents, ixs, told := s.fleetInputsInto(lists)
		_ = buildFleetIn("en", agents, ixs)
		_ = linkAttention("en", agents, ixs, told)
	}
	round()
	if n := testing.AllocsPerRun(20, round); n > 288 {
		t.Errorf("a round of 2,000 sessions allocated %v times, want at most 288", n)
	}
	const runs = 20
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		round()
	}
	runtime.ReadMemStats(&after)
	if per := (after.TotalAlloc - before.TotalAlloc) / runs; per > 401900 {
		t.Errorf("a round of 2,000 sessions allocated %d bytes, want at most 401900", per)
	}
}

// A round over the last round's lists is the round over new copies, whatever the lists did in between: the same
// agents, sessions decorated the same to the last field, the same fleet and summary. Nothing of an earlier
// decoration stays in a reused row, a shorter list leaves no rows behind and a machine that went is dropped.
func TestFleetInputsIntoIsAFreshRound(t *testing.T) {
	ft := newFleetTest(t)
	s, d := ft.s, ft.d
	d.add("pc", "PC")
	mac, _ := s.fleetMachine("mac")
	main := proto.Session{SessionID: "main", Cwd: mac.assistantCwd(), Agent: "claude", Status: proto.StatusWorking}
	row := func(id, st string) proto.Session {
		return proto.Session{SessionID: id, Cwd: "/code/app", Agent: "claude", Status: st, PaneID: "p-" + id,
			SpaceName: "app", Title: "node's " + id, LastText: "done", UpdatedAt: time.Unix(1700000000, 0)}
	}
	rows := func(n int, st string) []proto.Session {
		var out []proto.Session
		for i := range n {
			out = append(out, row(fmt.Sprintf("s%d", i), st))
		}
		return out
	}
	s.fleet.rep.mu.Lock()
	s.fleet.rep.sessions = map[string]SessionView{"main": {Title: "Persona", Queued: []string{"from the inbox"}, Origin: "app"}}
	s.fleet.rep.mu.Unlock()
	// An archived conversation the node no longer lists: the archives add its row after the node's.
	gone := archiveSnapshot(row("gone", proto.StatusDone))
	gone.MachineID = "mac"
	archived := map[string]agentArchive{runtimeListID("mac", "gone"): {Session: gone, Archived: true, ChangedAt: time.Unix(1700000200, 0)}}
	s.agentArchives.records, s.agentArchives.pending, s.agentArchives.loaded = archived, map[string]bool{}, true
	s.agentArchives.snapshot.Store(&archived)
	s.fleet.mu.Lock()
	s.fleet.interactions = map[string]*pending{"ix1": {ix: proto.Interaction{ID: "ix1", Kind: proto.IxPermission,
		MachineID: "pc", SessionID: "s1", ToolName: "Bash", CreatedAt: time.Unix(1700000100, 0)}}}
	s.fleet.mu.Unlock()

	rounds := []struct {
		name string
		set  func()
	}{
		{"first", func() {
			d.setSessions("mac", append([]proto.Session{main}, rows(5, proto.StatusDone)...))
			d.setSessions("pc", rows(3, proto.StatusWorking))
		}},
		// The assistant's row is a coding session now: its title, space, queue and state are the node's again.
		{"rows change places", func() {
			d.setSessions("mac", append(rows(5, proto.StatusWorking), main))
		}},
		{"shorter and longer", func() {
			d.setSessions("mac", rows(2, proto.StatusIdle))
			d.setSessions("pc", rows(40, proto.StatusDone))
		}},
		{"same again", func() {}},
		{"a machine goes", func() {
			d.mu.Lock()
			delete(d.machines, "pc")
			d.order = slices.DeleteFunc(d.order, func(id string) bool { return id == "pc" })
			d.mu.Unlock()
		}},
		{"empty", func() { d.setSessions("mac", nil) }},
	}
	prev := map[string][]proto.Session{}
	var last map[string][]proto.Session
	for _, r := range rounds {
		r.set()
		last = map[string][]proto.Session{}
		for id, list := range prev {
			last[id] = list[:cap(list)]
		}
		agents, ixs, told := s.fleetInputsInto(prev)
		got := [...]any{agentValues(agents), ixs, told, buildFleetIn("en", agents, ixs), linkAttention("en", agents, ixs, told)}
		agents, ixs, told = s.fleetInputs()
		want := [...]any{agentValues(agents), ixs, told, buildFleetIn("en", agents, ixs), linkAttention("en", agents, ixs, told)}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: over the last lists\n%+v\nnew copies\n%+v", r.name, got, want)
		}
		if l := prev["mac"]; len(l) == 0 || l[len(l)-1].SessionID != "gone" || !l[len(l)-1].Archived {
			t.Fatalf("%s: the archived row is not after the node's", r.name)
		}
		for _, m := range s.fleetMachines() {
			list := prev[m.ID]
			if old := last[m.ID]; len(old) > 0 && cap(old) >= len(list) && &list[:1][0] != &old[0] {
				t.Errorf("%s: %s's list was copied into a new array", r.name, m.ID)
			}
			for _, sess := range list[len(list):cap(list)] {
				if !reflect.DeepEqual(sess, proto.Session{}) {
					t.Errorf("%s: %s keeps a row past its list: %s", r.name, m.ID, sess.SessionID)
				}
			}
		}
		if len(prev) != len(s.fleetMachines()) {
			t.Errorf("%s: lists of %d machines kept for %d", r.name, len(prev), len(s.fleetMachines()))
		}
	}
	if len(ft.toldOf("fleet.live")) != 0 {
		t.Fatal("a round told the hub")
	}
}

// agentValues are the agents with the sessions they point at.
func agentValues(agents []fleetInput) []any {
	out := make([]any, 0, len(agents))
	for _, a := range agents {
		out = append(out, [...]any{a.Machine, *a.Session, a.Muted, a.TurnStart})
	}
	return out
}
