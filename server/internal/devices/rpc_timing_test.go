package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
)

func timingMachine(t *testing.T) (*Machine, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	d := &Devices{logf: func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(format, args...))
	}}
	m := &Machine{cfg: MachineConfig{ID: "home-mac-mini"}, d: d}
	return m, func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), lines...) }
}

func TestNodeTimingDisabledIsDirectAndExactMatch(t *testing.T) {
	event := func(string, json.RawMessage) {}
	request := func(context.Context, string, json.RawMessage) (any, error) { return nil, nil }
	for _, env := range []string{"", "home-mac", "home-mac-mini,other", " home-mac-mini"} {
		t.Setenv("PERSONA_NODE_RPC_TIMING", env)
		m, logs := timingMachine(t)
		if m.nodeTimingEnabled() {
			t.Fatalf("enabled for %q", env)
		}
		if reflect.ValueOf(m.nodeTimingEvents(event)).Pointer() != reflect.ValueOf(event).Pointer() {
			t.Fatal("disabled event path wrapped handler")
		}
		if reflect.ValueOf(m.nodeTimingRequests(request)).Pointer() != reflect.ValueOf(request).Pointer() {
			t.Fatal("disabled request path wrapped handler")
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		peer := rpc.New("test", nil)
		if err := m.nodeTimingCall(ctx, peer, "node.configure", map[string]string{"token": "private"}, nil); !errors.Is(err, context.Canceled) {
			t.Fatalf("changed error: %v", err)
		}
		m.nodeTimingInitialized()
		if len(logs()) != 0 {
			t.Fatal("disabled timing emitted logs")
		}
	}
}

func timingPair(t *testing.T, lines []string, kind, method string) {
	t.Helper()
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want paired start/end: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], kind+" start") || !strings.Contains(lines[1], kind+" end") || !strings.Contains(lines[1], "elapsed_ms=") || !strings.Contains(lines[0], method) {
		t.Fatalf("missing timing fields: %v", lines)
	}
	re := regexp.MustCompile(`seq=([0-9a-f]+)`)
	a, b := re.FindStringSubmatch(lines[0]), re.FindStringSubmatch(lines[1])
	if len(a) != 2 || len(b) != 2 || a[1] != b[1] {
		t.Fatalf("not paired: %v", lines)
	}
}

func TestNodeTimingCallPreservesResultAndDoesNotLogSecrets(t *testing.T) {
	t.Setenv("PERSONA_NODE_RPC_TIMING", "home-mac-mini")
	left, right := net.Pipe()
	client, server := rpc.New("h", left), rpc.New("n", right)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); left.Close(); right.Close() })
	const secret = "SENSITIVE_TOKEN_NEVER_LOG"
	server.OnRequest = func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if !strings.Contains(string(params), secret) {
			return nil, errors.New("params were changed")
		}
		if method == "node.configure" {
			return nil, errors.New(secret)
		}
		return map[string]string{"value": secret}, nil
	}
	go client.Serve(ctx, left)
	go server.Serve(ctx, right)
	for _, method := range []string{"sessions.list", "node.configure"} {
		m, logs := timingMachine(t)
		var result map[string]string
		callCtx, done := context.WithTimeout(ctx, time.Second)
		err := m.nodeTimingCall(callCtx, client, method, map[string]string{"token": secret}, &result)
		done()
		if method == "sessions.list" && (err != nil || result["value"] != secret) {
			t.Fatalf("changed successful result: %v %v", result, err)
		}
		if method == "node.configure" && (err == nil || err.Error() != secret) {
			t.Fatalf("changed error result: %v", err)
		}
		timingPair(t, logs(), "call", method)
		if strings.Contains(strings.Join(logs(), "\n"), secret) {
			t.Fatal("secret leaked")
		}
		want := "success"
		if err != nil {
			want = "other"
		}
		if !strings.Contains(logs()[1], "result="+want) {
			t.Fatalf("wrong outcome: %v", logs())
		}
	}
}

func TestNodeTimingEventShowsBlockedEntryWithoutPayload(t *testing.T) {
	t.Setenv("PERSONA_NODE_RPC_TIMING", "home-mac-mini")
	m, logs := timingMachine(t)
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	raw := json.RawMessage(`{"token":"PRIVATE_EVENT_BODY"}`)
	handler := m.nodeTimingEvents(func(event string, params json.RawMessage) {
		if event != "sessions.changed" || string(params) != string(raw) {
			t.Error("handler input changed")
		}
		close(entered)
		<-release
	})
	go func() { handler("sessions.changed", raw); close(done) }()
	<-entered
	if lines := logs(); len(lines) != 1 || !strings.Contains(lines[0], "event start") {
		t.Fatalf("blocked callback not identifiable: %v", lines)
	}
	close(release)
	<-done
	timingPair(t, logs(), "event", "sessions.changed")
	if strings.Contains(strings.Join(logs(), "\n"), "PRIVATE_EVENT_BODY") || !strings.Contains(logs()[0], fmt.Sprintf("bytes=%d", len(raw))) {
		t.Fatalf("unsafe or missing byte count: %v", logs())
	}
}

func TestNodeTimingReverseRequestClassifiesWithoutErrorText(t *testing.T) {
	t.Setenv("PERSONA_NODE_RPC_TIMING", "home-mac-mini")
	m, logs := timingMachine(t)
	want := errors.Join(context.Canceled, errors.New("PRIVATE_REVERSE_ERROR"))
	handler := m.nodeTimingRequests(func(context.Context, string, json.RawMessage) (any, error) { return "PRIVATE_RESULT", want })
	result, err := handler(context.Background(), "state.get", json.RawMessage(`{"token":"PRIVATE_PARAMS"}`))
	if result != "PRIVATE_RESULT" || err != want {
		t.Fatal("reverse result changed")
	}
	timingPair(t, logs(), "request", "state.get")
	if joined := strings.Join(logs(), "\n"); strings.Contains(joined, "PRIVATE") || !strings.Contains(joined, "result=canceled") {
		t.Fatalf("unsafe or wrong outcome: %v", logs())
	}
}

func TestNodeTimingLabelsAndErrorClasses(t *testing.T) {
	for _, label := range []string{"", "session.items\nsecret", strings.Repeat("x", 97), "token=private"} {
		if nodeTimingLabel(label) != "invalid" {
			t.Fatalf("unsafe label %q", label)
		}
	}
	if nodeTimingLabel("state.get") != "state.get" {
		t.Fatal("valid method rejected")
	}
	for _, tt := range []struct {
		err  error
		want string
	}{{nil, "success"}, {context.Canceled, "canceled"}, {fmt.Errorf("hidden: %w", context.DeadlineExceeded), "timeout"}, {errors.New("secret"), "other"}} {
		if got := nodeTimingError(tt.err); got != tt.want {
			t.Fatalf("got %s want %s", got, tt.want)
		}
	}
}
