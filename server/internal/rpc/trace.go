package rpc

import (
	"context"
	"errors"
	"hash/maphash"
	"io"
	"time"
)

// TraceEvent contains only transport metadata. ID is a connection-local keyed hash,
// never a wire request ID. Frame distinguishes writes within a connection. Labels
// are bounded protocol names; payloads and raw errors are deliberately unavailable.
type TraceEvent struct {
	Phase, Kind, Label, Outcome string
	ID, Frame                   uint64
	Bytes, Written              int
	Elapsed                     time.Duration
}

// traceMeta is private, so it cannot change the JSON wire format.
type traceMeta struct {
	id, frame   uint64
	kind, label string
}

// Only fixed protocol names are exposed. Even an otherwise syntactically valid
// unknown label may contain a credential or session ID.
func traceLabel(s string) string {
	switch s {
	case "node.configure", "launchers.catalog", "launchers.managed", "engine.use", "state.load", "state.init",
		"ping", "node.info", "node.doctor", "sessions.list", "session.items", "session.item",
		"session.watch", "session.unwatch", "session.screen", "session.screen.before",
		"terminal.watch", "terminal.unwatch", "herdr.client.open", "herdr.client.close",
		"mcp.call", "rpc.cancel", "state.put", "sessions.changed", "sessions.revs",
		"session.delta", "session.echo", "session.live", "terminal.frame", "terminal.end",
		"terminal.input", "terminal.resize", "terminal.click", "terminal.mouse", "terminal.scroll",
		"herdr.client.data", "herdr.client.end", "herdr.focus.changed", "fs.changed",
		"claude.dirs", "codex.inventory", "codex.interactions", "hook.event", "busy", "metrics",
		"notes.ready", "runtime.hub", "runtime.launchers", "runtime.sessions", "usage",
		"interaction.cancel", "interaction.dismiss":
		return s
	default:
		return "other"
	}
}

func (p *Peer) traceID(id string) uint64 {
	if id == "" {
		return 0
	}
	p.traceOnce.Do(func() { p.traceSeed = maphash.MakeSeed() })
	return maphash.String(p.traceSeed, id)
}

func (p *Peer) traceMeta(m message) traceMeta {
	if p.Trace == nil {
		return traceMeta{}
	}
	t := traceMeta{id: p.traceID(m.ID), kind: "response", label: traceLabel(m.traceMethod)}
	if m.Method != "" {
		t.kind, t.label = "request", traceLabel(m.Method)
	} else if m.Event != "" {
		t.kind, t.label = "event", traceLabel(m.Event)
	}
	return t
}

func traceOutcome(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	default:
		return "error"
	}
}

func (p *Peer) trace(t traceMeta, phase string, bytes, written int, started time.Time, err error) {
	if p.Trace == nil {
		return
	}
	e := TraceEvent{Phase: phase, Kind: t.kind, Label: t.label, ID: t.id, Frame: t.frame, Bytes: bytes, Written: written, Outcome: traceOutcome(err)}
	if !started.IsZero() {
		e.Elapsed = time.Since(started)
	}
	p.Trace(e)
}

func (p *Peer) traceStart() time.Time {
	if p.Trace == nil {
		return time.Time{}
	}
	return time.Now()
}

// TraceWriter adds raw transport boundaries underneath framing or scrubbing
// wrappers. Use only for short opt-in diagnostics. It forwards each Write once,
// unchanged; callbacks must be concurrency safe and must return promptly.
func TraceWriter(w io.Writer, trace func(TraceEvent)) io.Writer {
	if trace == nil {
		return w
	}
	return traceWriter{w: w, trace: trace}
}

type traceWriter struct {
	w     io.Writer
	trace func(TraceEvent)
}

func (w traceWriter) Write(b []byte) (int, error) {
	started := time.Now()
	w.trace(TraceEvent{Phase: "transport.write.start", Bytes: len(b)})
	n, err := w.w.Write(b)
	w.trace(TraceEvent{Phase: "transport.write.end", Bytes: len(b), Written: n, Elapsed: time.Since(started), Outcome: traceOutcome(err)})
	return n, err
}
