package devices

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
)

func TestNodeReceiveTimingDisabledAndMachineScoped(t *testing.T) {
	for _, tc := range []struct{ env, machine string }{{"", "home-mac-mini"}, {"other", "home-mac-mini"}, {"other", "other"}} {
		t.Setenv("PERSONA_NODE_RPC_TIMING", tc.env)
		m, logs := timingMachine(t)
		m.cfg.ID = tc.machine
		peer := rpc.New("h", io.Discard)
		reader := strings.NewReader("private")
		if got := m.nodeTimingReceiver(context.Background(), peer, reader); got != reader || peer.Trace != nil {
			t.Fatal("disabled path wrapped reader or enabled trace")
		}
		if len(logs()) != 0 {
			t.Fatal("disabled path logs")
		}
	}
}

type receiveBlockingReader struct {
	entered, release chan struct{}
	err              error
}

func (r *receiveBlockingReader) Read(b []byte) (int, error) {
	close(r.entered)
	<-r.release
	return copy(b, "private payload"), r.err
}

func TestNodeReceiveTimingSnapshotShowsBlockedReadAndPreservesResult(t *testing.T) {
	want := errors.New("private error /path/to/secret")
	raw := &receiveBlockingReader{entered: make(chan struct{}), release: make(chan struct{}), err: want}
	state := &nodeReceiveTiming{reader: raw, phase: "receive.scan.start", phaseAt: time.Now()}
	done := make(chan struct{})
	var n int
	var err error
	buf := make([]byte, 30)
	go func() { n, err = state.Read(buf); close(done) }()
	<-raw.entered
	var lines []string
	logf := func(f string, args ...any) { lines = append(lines, fmt.Sprintf(f, args...)) }
	state.snapshot(logf, 7)
	if !strings.Contains(lines[0], "read_pending=true") || !strings.Contains(lines[0], "reads=1 bytes=0") {
		t.Fatalf("missing blocked read: %v", lines)
	}
	close(raw.release)
	<-done
	if err != want || string(buf[:n]) != "private payload" {
		t.Fatal("reader result changed")
	}
	state.snapshot(logf, 7)
	if !strings.Contains(lines[1], "read_pending=false") || !strings.Contains(lines[1], "bytes=15") || !strings.Contains(lines[1], "last_read_result=other") {
		t.Fatalf("missing result metadata: %v", lines)
	}
	if strings.Contains(strings.Join(lines, "\n"), "private") || strings.Contains(strings.Join(lines, "\n"), "/path") {
		t.Fatal("leaked payload or error")
	}
}

func TestNodeReceiveTimingPeriodicSnapshotWhileReadBlocked(t *testing.T) {
	t.Setenv("PERSONA_NODE_RPC_TIMING", "home-mac-mini")
	m, logs := timingMachine(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	peer := rpc.New("h", io.Discard)
	reader, writer := io.Pipe()
	defer writer.Close()
	defer reader.Close()
	wrapped := m.nodeTimingReceiver(ctx, peer, reader)
	done := make(chan error, 1)
	go func() { done <- peer.Serve(ctx, wrapped) }()
	deadline := time.NewTimer(7 * time.Second)
	defer deadline.Stop()
	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()
	for {
		select {
		case <-poll.C:
			current := logs()
			if len(current) > 1 {
				line := current[len(current)-1]
				if !strings.Contains(line, "phase=receive.scan.start") || !strings.Contains(line, "read_pending=true") {
					t.Fatalf("wrong periodic status: %s", line)
				}
				cancel()
				writer.Close()
				<-done
				return
			}
		case <-deadline.C:
			t.Fatal("no periodic read status")
		}
	}
}
