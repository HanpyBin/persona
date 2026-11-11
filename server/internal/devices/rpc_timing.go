package devices

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/rpc"
)

// Temporary, opt-in diagnostics for a single machine. Never log RPC params/results or error text: configuration
// calls carry credentials. Disabled paths retain the original call/handler, without extra workers or timers.
var nodeTimingSequence atomic.Uint64

func (m *Machine) nodeTimingEnabled() bool {
	return m.cfg.ID != "" && os.Getenv("PERSONA_NODE_RPC_TIMING") == m.cfg.ID
}

func (m *Machine) nodeTimingCall(ctx context.Context, peer *rpc.Peer, method string, params, out any) error {
	if !m.nodeTimingEnabled() {
		return peer.Call(ctx, method, params, out)
	}
	seq, start := nodeTimingSequence.Add(1), time.Now()
	machine, label := nodeTimingLabel(m.cfg.ID), nodeTimingLabel(method)
	m.d.logf("[%s] node-rpc-timing call start seq=%x method=%s", machine, seq, label)
	err := peer.Call(ctx, method, params, out)
	m.d.logf("[%s] node-rpc-timing call end seq=%x method=%s elapsed_ms=%d result=%s", machine, seq, label, time.Since(start).Milliseconds(), nodeTimingError(err))
	return err
}

func (m *Machine) nodeTimingEvents(next rpc.EventHandler) rpc.EventHandler {
	if !m.nodeTimingEnabled() {
		return next
	}
	machine := nodeTimingLabel(m.cfg.ID)
	return func(event string, params json.RawMessage) {
		seq, start := nodeTimingSequence.Add(1), time.Now()
		label := nodeTimingLabel(event)
		m.d.logf("[%s] node-rpc-timing event start seq=%x event=%s bytes=%d", machine, seq, label, len(params))
		next(event, params)
		m.d.logf("[%s] node-rpc-timing event end seq=%x event=%s bytes=%d elapsed_ms=%d", machine, seq, label, len(params), time.Since(start).Milliseconds())
	}
}

// Reverse calls can occur during node.configure (for example state.get); record only their method and outcome.
func (m *Machine) nodeTimingRequests(next rpc.Handler) rpc.Handler {
	if !m.nodeTimingEnabled() {
		return next
	}
	machine := nodeTimingLabel(m.cfg.ID)
	return func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		seq, start := nodeTimingSequence.Add(1), time.Now()
		label := nodeTimingLabel(method)
		m.d.logf("[%s] node-rpc-timing request start seq=%x method=%s", machine, seq, label)
		result, err := next(ctx, method, params)
		m.d.logf("[%s] node-rpc-timing request end seq=%x method=%s elapsed_ms=%d result=%s", machine, seq, label, time.Since(start).Milliseconds(), nodeTimingError(err))
		return result, err
	}
}

func (m *Machine) nodeTimingInitialized() {
	if m.nodeTimingEnabled() {
		m.d.logf("[%s] node-rpc-timing configure-and-list finished", nodeTimingLabel(m.cfg.ID))
	}
}

func nodeTimingError(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	return "other"
}

// Event names originate on the peer. Keep log lines bounded and reject embedded data/control characters.
func nodeTimingLabel(s string) string {
	if len(s) == 0 || len(s) > 96 {
		return "invalid"
	}
	for _, c := range s {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-' {
			continue
		}
		return "invalid"
	}
	return s
}
