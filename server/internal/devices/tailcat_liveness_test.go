package devices

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
)

// These are in-memory connections only; no DERP, real node or network probes.
func livenessPipe(t *testing.T, now func() time.Time) (*Machine, *activityConn, *rpc.Peer, net.Conn, context.Context) {
	t.Helper()
	m := tailcatMachine(newTestDevices(t), time.Hour, time.Hour, 20*time.Millisecond)
	m.cfg.Kind = "tailcat"
	m.dialTailcat = func(context.Context) (net.Conn, error) { return nil, errors.New("not used") }
	a, b := net.Pipe()
	ac := &activityConn{Conn: a, now: now}
	ac.touch()
	peer := rpc.New("h", ac)
	ctx, cancel := context.WithCancel(context.Background())
	go peer.Serve(ctx, ac)
	t.Cleanup(func() { cancel(); peer.Close(io.EOF); a.Close(); b.Close() })
	return m, ac, peer, b, ctx
}

// Freeze the activity clock after the idle interval. A completed ping must not refresh it; the old mixed RX/TX
// timestamp kept the connection alive forever here although every ping was consumed without any answer.
func TestTailcatConsumedUnansweredPingsExpire(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	m, ac, peer, remote, ctx := livenessPipe(t, func() time.Time { return time.Unix(0, clock.Load()) })
	clock.Add(int64(time.Hour))
	var pings atomic.Int32
	go func() {
		dec := json.NewDecoder(remote)
		for {
			var msg struct {
				Method string `json:"method"`
			}
			if dec.Decode(&msg) != nil {
				return
			}
			if msg.Method == "ping" {
				pings.Add(1)
			}
		}
	}()
	done := make(chan struct{})
	go func() { defer close(done); m.watchTailcat(ctx, peer, ac) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("outgoing pings kept an unresponsive node alive")
	}
	if !errors.Is(peer.Err(), errTailcatUnresponsive) {
		t.Fatalf("end reason: %v", peer.Err())
	}
	if pings.Load() != 2 {
		t.Fatalf("pings=%d, want two misses", pings.Load())
	}
}

func TestTailcatPartialIncomingFrameIsAlive(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	m, ac, peer, remote, ctx := livenessPipe(t, func() time.Time { return time.Unix(0, clock.Load()) })
	go m.watchTailcat(ctx, peer, ac)
	// The frame deliberately remains incomplete; liveness must count byte progress, not complete RPC replies.
	if _, err := remote.Write([]byte(`{"event":"large","params":{"text":"`)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		clock.Add(int64(m.timings.checkEvery / 4))
		if _, err := remote.Write([]byte(strings.Repeat("x", 512))); err != nil {
			t.Fatalf("partial frame dropped: %v", err)
		}
		time.Sleep(m.timings.checkEvery / 4)
	}
	select {
	case <-peer.Done():
		t.Fatal("partial incoming history frame was disconnected")
	default:
	}
}

func TestTailcatSlowActiveUploadIsAlive(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	m, ac, peer, remote, ctx := livenessPipe(t, func() time.Time { return time.Unix(0, clock.Load()) })
	clock.Add(int64(time.Hour)) // no recent RX at all
	progress := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, activityChunk)
		for {
			clock.Add(int64(m.timings.checkEvery / 4))
			if _, err := remote.Read(buf); err != nil {
				return
			}
			select {
			case progress <- struct{}{}:
			default:
			}
			time.Sleep(m.timings.checkEvery / 4)
		}
	}()
	go func() { _, _ = ac.Write([]byte(strings.Repeat("x", activityChunk*100))) }()
	<-progress
	go m.watchTailcat(ctx, peer, ac)
	time.Sleep(8 * m.timings.checkEvery)
	if ac.writing.Load() == 0 {
		t.Fatal("test upload finished too soon")
	}
	select {
	case <-peer.Done():
		t.Fatal("active upload making progress was disconnected")
	default:
	}
}

// A stream of completed small outbound writes, including the watchdog's ping, is not an inbound health signal.
func TestTailcatCompletedWritesDoNotProveLiveness(t *testing.T) {
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	_, ac, _, remote, _ := livenessPipe(t, func() time.Time { return time.Unix(0, clock.Load()) })
	go io.Copy(io.Discard, remote)
	for i := 0; i < 5; i++ {
		clock.Add(int64(time.Minute))
		if _, err := ac.Write([]byte("ping\n")); err != nil {
			t.Fatal(err)
		}
		if got := ac.idle(); got < time.Minute {
			t.Fatalf("completed write reset inbound liveness: %s", got)
		}
	}
}

func TestTailcatSuccessfulPingResetsMisses(t *testing.T) {
	m, ac, peer, remote, ctx := livenessPipe(t, nil)
	ac.lastRead.Store(time.Now().Add(-time.Hour).UnixNano())
	var pings atomic.Int32
	go func() {
		dec := json.NewDecoder(remote)
		enc := json.NewEncoder(remote)
		for {
			var req struct {
				ID     string `json:"id"`
				Method string `json:"method"`
			}
			if dec.Decode(&req) != nil {
				return
			}
			if req.Method == "ping" && pings.Add(1) == 2 {
				_ = enc.Encode(map[string]any{"id": req.ID, "result": map[string]any{}})
			}
		}
	}()
	done := make(chan struct{})
	go func() { defer close(done); m.watchTailcat(ctx, peer, ac) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("never reached two consecutive misses")
	}
	if got := pings.Load(); got != 4 {
		t.Fatalf("pings=%d, want miss, success, miss, miss", got)
	}
}

func TestTailcatUnresponsiveRetainsReconnectBackoff(t *testing.T) {
	d := newTestDevices(t)
	m := newMachine(d, MachineConfig{ID: "box", Kind: "tailcat"})
	p := cfgPath(m)
	for want := 1; want <= 7; want++ {
		before := time.Now()
		d.pathEnded(p, true, fmt.Errorf("watchdog: %w", errTailcatUnresponsive), 3*time.Minute, false)
		d.fleet.mu.Lock()
		health := *d.fleet.health[p.key]
		d.fleet.mu.Unlock()
		fails := min(want, 6)
		if health.fails != fails {
			t.Fatalf("failures=%d, want %d", health.fails, fails)
		}
		if wait := health.failUntil.Sub(before); wait < min(time.Second<<fails, time.Minute) {
			t.Fatalf("backoff reset: %s", wait)
		}
	}
	d.pathEnded(p, true, io.EOF, 3*time.Minute, false)
	d.fleet.mu.Lock()
	fails := d.fleet.health[p.key].fails
	d.fleet.mu.Unlock()
	if fails != 0 {
		t.Fatal("ordinary healthy long-lived connection no longer clears backoff")
	}
}

// Closing the transport makes both peer.Done and the reader result ready. Whichever wins serve's select, the
// watchdog reason must survive so the path's failure count cannot randomly reset after a long connection.
func TestTailcatWatchdogReasonSurvivesTransportClose(t *testing.T) {
	for i := 0; i < 12; i++ {
		m := tailcatMachine(newTestDevices(t), time.Hour, time.Hour, 10*time.Millisecond)
		m.cfg.Kind = "tailcat"
		m.dialTailcat = func(context.Context) (net.Conn, error) {
			a, b := net.Pipe()
			p := rpc.New("n", b)
			nctx, stop := context.WithCancel(context.Background())
			p.OnRequest = func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
				if method == "ping" {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return map[string]any{}, nil
			}
			go func() { defer stop(); _ = p.Serve(nctx, b); b.Close() }()
			return a, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := m.connect(ctx)
		cancel()
		if !errors.Is(err, errTailcatUnresponsive) {
			t.Fatalf("attempt %d lost watchdog reason: %v", i, err)
		}
	}
}
