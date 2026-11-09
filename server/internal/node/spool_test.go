package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/nodeclient"
	"github.com/nwverse/persona/server/internal/rpc"
)

// mustLoad reads the spool, failing the test on a read or scanner error.
func mustLoad(t *testing.T, sp *spool) []spoolEntry {
	t.Helper()
	entries, err := sp.load()
	if err != nil {
		t.Fatalf("load spool %s: %v", sp.path, err)
	}
	return entries
}

func entryIDs(entries []spoolEntry) []string {
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.ID)
	}
	return ids
}

func TestSpoolKeepsOrderBoundsAndPersists(t *testing.T) {
	defer func(n, b int) { spoolMost, spoolBytesMost = n, b }(spoolMost, spoolBytesMost)
	spoolMost = 3
	path := filepath.Join(t.TempDir(), spoolFile)
	sp := openSpool(path)
	for i := range 5 {
		dropped, err := sp.add(spoolEntry{ID: string(rune('a' + i)), At: time.Now(), Kind: "hook"})
		if want := min(1, max(0, i-2)); err != nil || dropped != want {
			t.Fatalf("add %d: dropped %d %v", i, dropped, err)
		}
	}
	var ids []string
	for _, e := range mustLoad(t, openSpool(path)) { // a new process reads what the last one kept
		ids = append(ids, e.ID)
	}
	if strings.Join(ids, "") != "cde" {
		t.Fatalf("kept %v, want the 3 newest in order", ids)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("spool file: %v %v", fi, err)
	}
	spoolBytesMost = 400
	big := sockRequest{Kind: "hook", Input: json.RawMessage(`"` + strings.Repeat("x", 150) + `"`)}
	_, _ = sp.add(spoolEntry{ID: "f", Kind: "hook", Request: big})
	_, _ = sp.add(spoolEntry{ID: "g", Kind: "hook", Request: big})
	if got := mustLoad(t, sp); len(got) == 0 || got[len(got)-1].ID != "g" {
		t.Fatalf("by size: %+v", got)
	}
	for _, e := range mustLoad(t, sp) {
		_ = sp.remove(e.ID)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("an empty spool leaves no file: %v", err)
	}
}

func hookReq(event, tool string, mod func(*sockRequest)) sockRequest {
	b, _ := json.Marshal(map[string]any{"hook_event_name": event, "session_id": reportSession, "tool_name": tool, "cwd": "/work/project"})
	r := sockRequest{Kind: "hook", Input: b, Pane: "w1:p1", Attempt: "att-" + event + tool}
	if mod != nil {
		mod(&r)
	}
	return r
}

func TestStandbyAnswers(t *testing.T) {
	path := filepath.Join(t.TempDir(), spoolFile)
	sb := newStandby(openSpool(path), t.Logf)
	now := time.Now()
	sb.now = func() time.Time { return now }
	sb.healthy = now
	reply := func(req sockRequest) map[string]any {
		b, _ := json.Marshal(sb.answer(req))
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		return m
	}

	if r := reply(hookReq("Stop", "", nil)); r["output"] != nil || r["retry"] != nil {
		t.Fatalf("a Stop: %v", r)
	}
	if r := reply(hookReq("PermissionRequest", "Bash", nil)); r["retry"] != true {
		t.Fatalf("a permission while the manager was just here: %v, want retry", r)
	}
	if r := reply(hookReq("PreToolUse", "Bash", nil)); r["retry"] != nil {
		t.Fatalf("a Bash PreToolUse is not held: %v", r)
	}
	if r := reply(hookReq("PreToolUse", "AskUserQuestion", func(r *sockRequest) { r.Resume = true })); r["retry"] != true {
		t.Fatalf("a resumed question: %v", r)
	}
	if r := reply(hookReq("Stop", "", func(r *sockRequest) { r.ReportOnly = true })); r["output"] != nil {
		t.Fatalf("report-only: %v", r)
	}
	_ = reply(hookReq("PostToolUse", "Read", func(r *sockRequest) { r.ReportOnly = true })) // not a reported event
	if r := reply(sockRequest{Kind: "ask", Attempt: "a1", Input: json.RawMessage(`{"question":"q"}`)}); r["retry"] != true {
		t.Fatalf("ask: %v", r)
	}
	if r := reply(sockRequest{Kind: "notify", Input: json.RawMessage(`{"title":"built"}`)}); r["queued"] != true {
		t.Fatalf("notify: %v", r)
	}
	if r := reply(sockRequest{Kind: "notify", Input: json.RawMessage(`{"title":" "}`)}); r["error"] == nil {
		t.Fatalf("an empty notify: %v", r)
	}
	// MCP gets what a node says when it cannot reach its manager (an older `personad mcp` knows it); the rest no node.
	if r := reply(sockRequest{Kind: "mcp.tools"}); r["error"] == nil || r["tools"] == nil {
		t.Fatalf("mcp.tools: %v", r)
	}
	if r := reply(sockRequest{Kind: "mcp.call", Name: "agent_send"}); r["is_error"] != true {
		t.Fatalf("mcp.call: %v, want an error result", r)
	}
	for _, req := range []sockRequest{{Kind: "mcp.tools", AwayOK: true}, {Kind: "mcp.call", AwayOK: true}} {
		if r := reply(req); r["away"] != true {
			t.Fatalf("%s from a current client: %v, want away", req.Kind, r)
		}
	}
	for _, kind := range []string{"launcher.env", "cli.call"} {
		if r := reply(sockRequest{Kind: kind}); r["away"] != true {
			t.Fatalf("%s: %v, want away", kind, r)
		}
	}

	var kept []string
	for _, e := range mustLoad(t, sb.sp) {
		var in hookInput
		_ = json.Unmarshal(e.Request.Input, &in)
		kept = append(kept, e.Kind+":"+in.HookEventName+in.ToolName)
	}
	// The resumed question was reported by the node it first reached; the report-only PostToolUse is not reported.
	want := "hook:Stop hook:PermissionRequestBash hook:PreToolUseBash hook:Stop notify:"
	if strings.Join(kept, " ") != want {
		t.Fatalf("kept %v\nwant %s", kept, want)
	}

	now = now.Add(standbyHold + time.Second) // the manager is away for long: the terminal asks
	if r := reply(hookReq("PermissionRequest", "Bash", nil)); r["retry"] != nil {
		t.Fatalf("after the hold: %v", r)
	}
	if r := reply(sockRequest{Kind: "ask", Attempt: "a2", Input: json.RawMessage(`{"question":"q"}`)}); r["status"] != nodeclient.AskUnavailable {
		t.Fatalf("ask after the hold: %v", r)
	}
}

// Through the real socket: clients take standby's "away" for no node, and a hook's card retries.
func TestStandbyOnTheSocket(t *testing.T) {
	home, err := os.MkdirTemp("/tmp", "sb") // short: sun_path
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	t.Setenv("HOME", home)
	sb := newStandby(openSpool(SpoolPath()), t.Logf)
	sb.want()
	defer sb.unwant()
	ctx := context.Background()
	if _, err := nodeclient.Dial(ctx, sockRequest{Kind: "launcher.env"}); !errors.Is(err, nodeclient.ErrNoNode) {
		t.Fatalf("launcher.env in standby: %v, want ErrNoNode", err)
	}
	rep, err := nodeclient.NotifyNode(ctx, nodeclient.Dial, nodeclient.NotifyRequest{Title: "built"}, "")
	if err != nil || !rep.Queued || rep.Sent {
		t.Fatalf("notify: %+v %v", rep, err)
	}
	if out := nodeclient.Hook(ctx, hookReq("Stop", "", nil).Input, "w1:p1"); out != nil {
		t.Fatalf("a Stop printed %s", out)
	}
	if n := len(mustLoad(t, openSpool(SpoolPath()))); n != 2 {
		t.Fatalf("spooled %d, want the notification and the Stop", n)
	}
	sb.unwant()
	sb.want() // started again (the next manager went away too): still serves
	if _, err := nodeclient.Dial(ctx, sockRequest{Kind: "ping"}); err != nil {
		t.Fatalf("ping after a restart of standby: %v", err)
	}
}

// replayHub records what the manager gets from a replay.
type replayHub struct {
	mu  sync.Mutex
	got []string
	ids map[string]int
}

func newReplayHub(t *testing.T, n *Node) *replayHub {
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	rec := &replayHub{ids: map[string]int{}}
	note := func(what string, params json.RawMessage) {
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		rec.mu.Lock()
		defer rec.mu.Unlock()
		id, _ := p["spool_id"].(string)
		rec.ids[id]++
		rec.got = append(rec.got, what)
	}
	hub.OnEvent = func(event string, params json.RawMessage) {
		if event == "hook.event" {
			var p struct {
				Event string `json:"event"`
			}
			_ = json.Unmarshal(params, &p)
			note("event:"+p.Event, params)
		}
	}
	hub.OnRequest = func(_ context.Context, method string, params json.RawMessage) (any, error) {
		if method == "notify" {
			var p struct {
				Title string `json:"title"`
			}
			_ = json.Unmarshal(params, &p)
			note("notify:"+p.Title, params)
			return map[string]any{"sent": true}, nil
		}
		return nil, nil
	}
	go func() { _ = hub.Serve(ctx, n2hR) }()
	go func() { _ = n.peer.Serve(ctx, h2nR) }()
	t.Cleanup(func() { cancel(); _ = n2hW.Close(); _ = h2nW.Close() })
	return rec
}

func TestReplayHandsTheManagerWhatStandbyTookInOrderOnce(t *testing.T) {
	n, _ := testNode(t)
	n.logger = log.New(io.Discard, "", 0)
	n.panes = map[string]string{}
	rec := newReplayHub(t, n)
	sp := openSpool(filepath.Join(t.TempDir(), spoolFile))
	at := time.Now().Add(-time.Minute)
	add := func(kind string, req sockRequest, at time.Time) {
		if _, err := sp.add(spoolEntry{ID: newUUID(), At: at, Kind: kind, Request: req}); err != nil {
			t.Fatal(err)
		}
	}
	add("hook", hookReq("UserPromptSubmit", "", nil), at)
	add("notify", sockRequest{Kind: "notify", Input: json.RawMessage(`{"title":"tests passed"}`)}, at)
	add("hook", hookReq("Stop", "", nil), at)
	add("hook", hookReq("Stop", "", nil), time.Now().Add(-spoolMaxAge-time.Hour)) // too old: dropped

	n.replayed, n.stateReady = make(chan struct{}), make(chan struct{})
	close(n.stateReady)
	n.replaySpool(context.Background(), sp)
	select {
	case <-n.replayed:
	default:
		t.Fatal("the gate for new hooks stayed shut")
	}
	waitFor(t, "the replay to arrive", func() bool {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		return len(rec.got) == 3
	})
	rec.mu.Lock()
	got := strings.Join(rec.got, " ")
	ids := rec.ids
	rec.mu.Unlock()
	if got != "event:UserPromptSubmit notify:tests passed event:Stop" {
		t.Fatalf("replayed %q", got)
	}
	for id, c := range ids {
		if id == "" || c != 1 {
			t.Fatalf("spool ids %v: each once, never empty", ids)
		}
	}
	if left := mustLoad(t, sp); len(left) != 0 {
		t.Fatalf("left in the spool: %+v", left)
	}
	n.replayed = make(chan struct{})
	n.replaySpool(context.Background(), sp) // nothing again: nothing sent twice
	time.Sleep(50 * time.Millisecond)
	if rec.mu.Lock(); len(rec.got) != 3 {
		t.Fatalf("a second replay sent %v", rec.got)
	}
	rec.mu.Unlock()
}

// Truncated lines, invalid JSON and id-less entries are skipped but counted and logged; a line past the scanner's
// budget is a scanner error returned with the entries parsed before it, never a silent empty spool.
func TestSpoolSurfacesCorruptionAndReadFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), spoolFile)
	goodLine := func(id string) string {
		b, err := json.Marshal(spoolEntry{ID: id, At: time.Now(), Kind: "hook"})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	lines := []string{
		goodLine("a"),
		`{"id":"`, // truncated line
		`{}`,       // parses, but has no id
		"{not json",
		goodLine("b"),
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	sp := openSpool(path).withLogger(func(f string, a ...any) { fmt.Fprintf(&logs, f+"\n", a...) })
	entries, err := sp.load()
	if err != nil {
		t.Fatalf("bad lines are skipped, not a load failure: %v", err)
	}
	if ids := strings.Join(entryIDs(entries), ""); ids != "ab" {
		t.Fatalf("kept %v, want a and b in order", ids)
	}
	if !strings.Contains(logs.String(), "skipping") || !strings.Contains(logs.String(), "3 unparseable") {
		t.Fatalf("the bad lines were not logged:\n%s", logs.String())
	}

	// A line beyond the scanner budget stops the scan: the error comes back with what parsed before it.
	huge := strings.Repeat("x", spoolBytesMost+2)
	if err := os.WriteFile(path, []byte(goodLine("c")+"\n"+huge+"\n"+goodLine("d")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err = sp.load()
	if err == nil {
		t.Fatal("a line past the scanner budget must be returned as an error, not hidden")
	}
	if len(entries) != 1 || entries[0].ID != "c" {
		t.Fatalf("entries before the scanner failure: %+v", entries)
	}
}

// An unreadable spool is an error every caller sees; a read-only directory makes the rewrite fail.
func TestSpoolPermissionFaultsAreReturned(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission faults do not trigger for root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, spoolFile)
	sp := openSpool(path).withLogger(t.Logf)
	if _, err := sp.add(spoolEntry{ID: "a", Kind: "hook"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if _, err := sp.load(); err == nil {
		t.Fatal("load must surface an unreadable spool")
	}
	if _, err := sp.add(spoolEntry{ID: "b", Kind: "hook"}); err == nil {
		t.Fatal("add must surface a read error instead of losing the kept entries")
	}
	if _, err := sp.failed("a"); err == nil {
		t.Fatal("failed must surface a read error")
	}
	if err := sp.remove("a"); err == nil {
		t.Fatal("remove must surface a read error")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}

	// The file reads again; a read-only directory makes the atomic rewrite fail.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, err := sp.add(spoolEntry{ID: "b", Kind: "hook"}); err == nil {
		t.Fatal("add must surface a write error when the directory is read-only")
	}
}

// A marshal failure is returned, not discarded, and leaves the spool untouched.
func TestSpoolAddReturnsMarshalFailure(t *testing.T) {
	sp := openSpool(filepath.Join(t.TempDir(), spoolFile)).withLogger(t.Logf)
	_, err := sp.add(spoolEntry{ID: "x", Kind: "hook", Request: sockRequest{Input: json.RawMessage("{bad")}})
	if err == nil {
		t.Fatal("a marshal failure must be returned, not discarded")
	}
	if entries := mustLoad(t, sp); len(entries) != 0 {
		t.Fatalf("nothing should have been written: %+v", entries)
	}
}
