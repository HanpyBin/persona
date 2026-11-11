package devices

import (
	"context"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
)

// nodeReceiveTiming keeps only counts and fixed trace labels. A periodic snapshot
// is necessary: a blocked Read or event handler cannot emit its own completion.
// There is exactly one reader and one connection-bounded diagnostic ticker.
type nodeReceiveTiming struct {
	mu              sync.Mutex
	reader          io.Reader
	phase           string
	label           string
	phaseAt         time.Time
	frame           uint64
	frameBytes      int
	readPending     bool
	readAt          time.Time
	reads, bytes    uint64
	lastReadBytes   int
	lastReadElapsed time.Duration
	lastReadResult  string
}

func (s *nodeReceiveTiming) trace(e rpc.TraceEvent) {
	if !strings.HasPrefix(e.Phase, "receive.") {
		return
	}
	s.mu.Lock()
	s.phase, s.label, s.phaseAt = e.Phase, e.Label, time.Now()
	s.frame, s.frameBytes = e.Frame, e.Bytes
	s.mu.Unlock()
}

func (s *nodeReceiveTiming) Read(b []byte) (int, error) {
	started := time.Now()
	s.mu.Lock()
	s.readPending, s.readAt = true, started
	s.reads++
	s.mu.Unlock()
	n, err := s.reader.Read(b)
	s.mu.Lock()
	s.readPending = false
	if n > 0 {
		s.bytes += uint64(n)
	}
	s.lastReadBytes, s.lastReadElapsed, s.lastReadResult = n, time.Since(started), nodeTimingError(err)
	s.mu.Unlock()
	return n, err
}

func (s *nodeReceiveTiming) snapshot(logf func(string, ...any), connection uint64) {
	s.mu.Lock()
	phase, label, frame, frameBytes := s.phase, s.label, s.frame, s.frameBytes
	phaseElapsed := time.Since(s.phaseAt)
	pending, reads, total, lastN, lastElapsed, result := s.readPending, s.reads, s.bytes, s.lastReadBytes, s.lastReadElapsed, s.lastReadResult
	var readElapsed time.Duration
	if pending {
		readElapsed = time.Since(s.readAt)
	}
	s.mu.Unlock()
	logf("[home-mac-mini] node-rpc-receive connection=%x phase=%s label=%s frame=%d frame_bytes=%d phase_elapsed_ms=%d read_pending=%t read_elapsed_ms=%d reads=%d bytes=%d last_read_bytes=%d last_read_elapsed_ms=%d last_read_result=%s", connection, phase, label, frame, frameBytes, phaseElapsed.Milliseconds(), pending, readElapsed.Milliseconds(), reads, total, lastN, lastElapsed.Milliseconds(), result)
}

func (m *Machine) nodeTimingReceiver(ctx context.Context, peer *rpc.Peer, r io.Reader) io.Reader {
	if m.cfg.ID != "home-mac-mini" || !m.nodeTimingEnabled() {
		return r
	}
	state := &nodeReceiveTiming{reader: r, phase: "starting", phaseAt: time.Now()}
	connection := nodeTimingSequence.Add(1)
	peer.Trace = state.trace
	state.snapshot(m.d.logf, connection)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				state.snapshot(m.d.logf, connection)
			case <-ctx.Done():
				return
			case <-peer.Done():
				return
			}
		}
	}()
	return state
}
