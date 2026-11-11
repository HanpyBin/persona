package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type traceRecorder struct {
	mu     sync.Mutex
	events []TraceEvent
	wake   chan struct{}
}

func newTraceRecorder() *traceRecorder { return &traceRecorder{wake: make(chan struct{}, 1)} }
func (r *traceRecorder) add(e TraceEvent) {
	r.mu.Lock()
	r.events = append(r.events, e)
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}
func (r *traceRecorder) snapshot() []TraceEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]TraceEvent(nil), r.events...)
}
func (r *traceRecorder) wait(t *testing.T, phase string) TraceEvent {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		for _, e := range r.snapshot() {
			if e.Phase == phase {
				return e
			}
		}
		select {
		case <-r.wake:
		case <-timer.C:
			t.Fatalf("no %s in %+v", phase, r.snapshot())
		}
	}
}
func TestTraceDefaultOffAndWireUnchanged(t *testing.T) {
	var plain, traced bytes.Buffer
	p := New("n", &plain)
	if p.Trace != nil {
		t.Fatal("trace enabled by default")
	}
	if TraceWriter(&plain, nil) != &plain {
		t.Fatal("disabled transport tracing wraps writer")
	}
	r := newTraceRecorder()
	q := New("n", TraceWriter(&traced, r.add))
	q.Trace = r.add
	for _, peer := range []*Peer{p, q} {
		if err := peer.Notify("sessions.changed", Marshaled(`{"secret":"payload"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if plain.String() != traced.String() {
		t.Fatal("trace changes wire")
	}
	if p.traceFrame.Load() != 0 {
		t.Fatal("disabled trace creates metadata")
	}
	for _, e := range r.snapshot() {
		if strings.HasPrefix(e.Phase, "frame.") && e.Label != "sessions.changed" {
			t.Fatalf("lost marshaled event: %+v", e)
		}
	}
}
func TestTracePrivacyAndResponseCorrelation(t *testing.T) {
	var wire bytes.Buffer
	r := newTraceRecorder()
	p := New("n", &wire)
	p.Trace = r.add
	p.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
		return nil, errors.New("error-secret-/private/path")
	}
	p.handle(context.Background(), message{ID: "session-secret-123", Method: "secret_token_only_letters", Params: json.RawMessage(`{"token":"param-secret"}`)})
	p.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
		return map[string]string{"value": "result-secret"}, nil
	}
	p.handle(context.Background(), message{ID: "session-secret-123", Method: "session.items"})
	encoded, err := json.Marshal(r.snapshot())
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"session-secret", "secret_token", "error-secret", "private/path", "param-secret", "result-secret"} {
		if bytes.Contains(encoded, []byte(secret)) {
			t.Fatalf("trace leaked %q", secret)
		}
	}
	var id uint64
	for _, e := range r.snapshot() {
		if id == 0 {
			id = e.ID
		}
		if e.ID != id || id == 0 {
			t.Fatalf("lost correlation %+v", e)
		}
	}
	if New("n", io.Discard).traceID("session-secret-123") == id {
		t.Fatal("request hash reused across connections")
	}
	if got := traceLabel("session.items\nsecret"); got != "other" {
		t.Fatal(got)
	}
	r.wait(t, "payload.prepare.start")
	r.wait(t, "payload.prepare.end")
}

type traceBlockedWriter struct {
	entered chan struct{}
	release chan struct{}
}

func (w *traceBlockedWriter) Write(b []byte) (int, error) {
	close(w.entered)
	<-w.release
	return len(b), nil
}
func TestTraceBlockedTransportAndCanceledWriterWait(t *testing.T) {
	r := newTraceRecorder()
	w := &traceBlockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	p := New("n", TraceWriter(w, r.add))
	p.Trace = r.add
	first := make(chan error, 1)
	go func() { first <- p.Notify("session.items", map[string]string{"x": "secret"}) }()
	select {
	case <-w.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("writer not entered")
	}
	defer close(w.release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	second := make(chan error, 1)
	go func() { second <- p.NotifyContext(ctx, "sessions.changed", nil) }()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
waiting:
	for {
		for _, e := range r.snapshot() {
			if e.Phase == "writer.wait.start" && e.Frame == 2 {
				break waiting
			}
		}
		select {
		case <-r.wake:
		case <-deadline.C:
			t.Fatal("second frame did not wait")
		}
	}
	cancel()
	select {
	case err := <-second:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel blocked")
	}
	wait := r.wait(t, "writer.wait.end")
	if wait.Frame != 2 || wait.Outcome != "canceled" {
		t.Fatalf("wrong canceled frame %+v", wait)
	}
	for _, e := range r.snapshot() {
		if e.Phase == "write.end" || e.Phase == "transport.write.end" {
			t.Fatal("blocked write reported complete")
		}
		if e.Phase == "write.start" && e.Frame != 1 {
			t.Fatal("canceled queued frame reached wire")
		}
	}
	r.wait(t, "transport.write.start")
	t.Cleanup(func() {
		select {
		case err := <-first:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("writer failed to finish")
		}
	})
}
func TestTraceCancelArrivalCorrelatesWithHandler(t *testing.T) {
	r := newTraceRecorder()
	p := New("n", io.Discard)
	p.Trace = r.add
	p.OnRequest = func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	reader, writer := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- p.Serve(context.Background(), reader) }()
	t.Cleanup(func() { _ = writer.Close(); _ = reader.Close(); <-done })
	_, err := io.WriteString(writer, "{\"id\":\"private-session-id\",\"method\":\"session.items\"}\n")
	if err != nil {
		t.Fatal(err)
	}
	start := r.wait(t, "handler.start")
	_, err = io.WriteString(writer, "{\"event\":\"rpc.cancel\",\"params\":{\"id\":\"private-session-id\"}}\n")
	if err != nil {
		t.Fatal(err)
	}
	cancel := r.wait(t, "cancel.received")
	end := r.wait(t, "response.canceled")
	if start.ID == 0 || start.ID != cancel.ID || start.ID != end.ID {
		t.Fatal("cancel correlation lost")
	}
	for _, e := range r.snapshot() {
		if e.Phase == "write.start" {
			t.Fatal("canceled response written")
		}
	}
}

type traceFailWriter struct{ err error }

func (w traceFailWriter) Write([]byte) (int, error) { return 2, w.err }
func TestTraceWriterPreservesPartialWriteAndError(t *testing.T) {
	r := newTraceRecorder()
	want := errors.New("secret raw transport error")
	n, err := TraceWriter(traceFailWriter{err: want}, r.add).Write([]byte("private payload"))
	if n != 2 || err != want {
		t.Fatalf("changed writer outcome: %d %v", n, err)
	}
	end := r.wait(t, "transport.write.end")
	if end.Written != 2 || end.Bytes != 15 || end.Outcome != "error" {
		t.Fatalf("wrong trace %+v", end)
	}
	encoded, _ := json.Marshal(r.snapshot())
	if bytes.Contains(encoded, []byte("secret")) || bytes.Contains(encoded, []byte("private")) {
		t.Fatal("transport trace leaked content")
	}
}
