package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
)

// peerHub is the fake hub with Tell going through an rpc.Peer, as BusHub.Tell's Notify does: what a fleet event
// costs then includes the line the hub gets, not the fake's own copy.
type peerHub struct {
	*fakeHub
	peer *rpc.Peer
}

func (h peerHub) Tell(event string, params any) error { return h.peer.Notify(event, params) }

// lineHub is the fake hub that also writes each fleet event's line as the bus writes it now, and as it wrote the same
// bytes handed over as json.RawMessage.
type lineHub struct {
	*fakeHub
	lines []toldLine
}

type toldLine struct {
	event, now, was string
	marshaled       bool // handed over as rpc.Marshaled
}

func (h *lineHub) Tell(event string, params any) error {
	var raw json.RawMessage
	switch p := params.(type) {
	case rpc.Marshaled:
		raw = json.RawMessage(p)
	case json.RawMessage:
		raw = p
	default:
		return fmt.Errorf("%s told as %T", event, params)
	}
	var now, was bytes.Buffer
	if err := rpc.New("t", &now).Notify(event, params); err != nil {
		return err
	}
	if err := rpc.New("t", &was).Notify(event, raw); err != nil {
		return err
	}
	_, marshaled := params.(rpc.Marshaled)
	h.lines = append(h.lines, toldLine{event, now.String(), was.String(), marshaled})
	return h.fakeHub.Tell(event, params)
}

// The fleet's events are told as json.Marshal's bytes, which the bus puts into its line as they are: the line is the
// one it wrote when it checked and escaped them itself, for HTML characters, U+2028 and U+2029, any script, an empty
// list, and a list that does not encode (null, as before).
func TestFleetEventsAreTheLinesTheyWere(t *testing.T) {
	d := newFleetDevs()
	d.add("mac", "Mac")
	d.add("box", "Box")
	hub := &lineHub{fakeHub: newFakeHub()}
	s := New(Options{Hub: hub, Logf: func(string, ...any) {}, Devices: d})
	busy := time.Now().Add(-time.Minute)
	d.setSessions("mac", []proto.Session{
		{SessionID: "a", Agent: "claude", Status: proto.StatusWorking, Title: `<b>fix</b> & "ship"`, LastText: "a\u2028b\u2029c", UpdatedAt: busy},
		{SessionID: "b", Agent: "codex", Status: proto.StatusDone, Title: "修复登录 · ログイン 🚀", Cwd: "/代码/app", UpdatedAt: busy},
	})
	s.publishList("mac")
	s.publishList("box") // nothing on it
	s.tellLive(s.fleetSnapshot())
	s.tellLive(FleetState{Phase: "working", Items: []FleetItem{{ID: "mac/a", Title: "<&>\u2028\u2029 修复"}}})
	d.setSessions("mac", []proto.Session{{SessionID: "a", Title: "ok", UpdatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}})
	s.publishList("mac")                              // a year json.Marshal cannot write
	s.tellLive(FleetState{WorkingSince: math.Inf(1)}) // nor this

	var events []string
	for i, l := range hub.lines {
		events = append(events, l.event)
		if l.now != l.was {
			t.Errorf("line %d is\n%s\nit was\n%s", i, l.now, l.was)
		}
		if null := strings.HasSuffix(l.now, `"params":null}`+"\n"); l.marshaled == null {
			t.Errorf("line %d (marshaled %v): %s", i, l.marshaled, l.now)
		}
	}
	want := []string{assistantwire.EventFleetSessions, assistantwire.EventFleetSessions, assistantwire.EventFleetLive,
		assistantwire.EventFleetLive, assistantwire.EventFleetSessions, assistantwire.EventFleetLive}
	if fmt.Sprint(events) != fmt.Sprint(want) {
		t.Fatalf("told %q, want %q", events, want)
	}
	var all strings.Builder
	for _, l := range hub.lines {
		all.WriteString(l.now)
	}
	for _, esc := range []string{`\u003cb\u003efix\u003c/b\u003e \u0026`, `a\u2028b\u2029c`, "修复登录 · ログイン 🚀", `"sessions":`} {
		if !strings.Contains(all.String(), esc) {
			t.Errorf("no %s in what was told", esc)
		}
	}
}

// A machine's list is told the hub when it is not the one told last, by as little as a byte, and again once the
// connection is back; one that does not encode is told once, as null, and the next that does is told.
func TestPublishListTellsWhatChanged(t *testing.T) {
	ft := newFleetTestNoStore(t)
	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	list := func(title string, at time.Time) {
		ft.d.setSessions("mac", []proto.Session{{SessionID: "a", Agent: "claude", Status: proto.StatusDone, Title: title, UpdatedAt: at}})
	}
	want := func(step string, n int, last string) {
		t.Helper()
		ft.f.mu.Lock()
		defer ft.f.mu.Unlock()
		var told []string
		for _, tl := range ft.f.told {
			if tl.event == assistantwire.EventFleetSessions {
				told = append(told, string(tl.params))
			}
		}
		if len(told) != n || !strings.Contains(told[n-1], last) {
			t.Fatalf("%s: told %d lists, want %d ending with %s: %q", step, len(told), n, last, told)
		}
	}
	list("abc", at)
	ft.s.publishList("mac")
	ft.s.publishList("mac")
	want("the same list", 1, `"title":"abc"`)
	list("abd", at)
	ft.s.publishList("mac")
	ft.s.publishList("mac")
	want("a byte changed", 2, `"title":"abd"`)
	ft.s.FleetUp(context.Background())
	ft.s.publishList("mac")
	want("connected again", 3, `"title":"abd"`)
	list("abd", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC))
	ft.s.publishList("mac")
	ft.s.publishList("mac")
	want("no JSON for it", 4, "null")
	list("abd", at)
	ft.s.publishList("mac")
	want("JSON again", 5, `"title":"abd"`)
}

// A list told the hub costs its decorated copy, its JSON in the buffer every list reuses, and the bus's line
// (#3197): about 671 KB for 300 sessions measured, where the JSON copied out to be kept and the line checked and
// copied twice more made it 998 KB.
func TestPublishListAllocations(t *testing.T) {
	if raceEnabled {
		t.Skip("race instrumentation allocates")
	}
	if !isolated(t) {
		return
	}
	s, next := publishOf(300, io.Discard)
	next()
	s.publishList("mac")
	const runs = 200
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		next()
		s.publishList("mac")
	}
	runtime.ReadMemStats(&after)
	if per := (after.TotalAlloc - before.TotalAlloc) / runs; per > 691000 {
		t.Fatalf("a changed list told the hub allocated %d bytes, want at most 691000", per)
	}
}

// publishOf is a manager whose machine "mac" lists n sessions, and next, which hands the devices the other of two
// lists that differ in one row: every publishList after it tells the hub.
func publishOf(n int, w io.Writer) (s *Service, next func()) {
	d := newFleetDevs()
	d.add("mac", "Mac")
	s = New(Options{Hub: peerHub{newFakeHub(), rpc.New("t", w)}, Logf: func(string, ...any) {}, Devices: d})
	var lists [2][]proto.Session
	for k := range lists {
		list := make([]proto.Session, n)
		for i := range list {
			list[i] = proto.Session{SessionID: fmt.Sprintf("s%03d", i), Cwd: "/code/app", Agent: "claude", Status: proto.StatusDone,
				PaneID: fmt.Sprintf("p%d", i), SpaceName: "app", Title: "a session", LastText: strings.Repeat("the last words of the turn ", 8)}
		}
		list[0].LastText = fmt.Sprintf("turn %d", k)
		lists[k] = list
	}
	turn := 0
	return s, func() {
		d.setSessions("mac", lists[turn%2])
		turn++
	}
}

// BenchmarkPublishList: a machine's list of 300 sessions told the hub after one row changed (fleet.sessions), the
// line written as the bus writes it.
func BenchmarkPublishList(b *testing.B) {
	s, next := publishOf(300, io.Discard)
	b.ReportAllocs()
	for b.Loop() {
		next()
		s.publishList("mac")
	}
}
