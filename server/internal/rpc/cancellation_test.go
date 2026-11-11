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

type eventGateWriter struct {
	io.Writer
	event   string
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (w *eventGateWriter) Write(b []byte) (int, error) {
	if bytes.Contains(b, []byte(`"event":"`+w.event+`"`)) {
		w.once.Do(func() { close(w.entered) })
		<-w.release
	}
	return w.Writer.Write(b)
}

// Both a blocked earlier frame and a stalled cancellation frame must not hold the caller past its deadline.
func TestCallCancellationCannotBlockItsCaller(t *testing.T) {
	for _, event := range []string{"stuck", cancelEvent} {
		t.Run(event, func(t *testing.T) {
			hub, node := pair(t)
			hub.Cancels = map[string]bool{"session.items": true}
			gate := &eventGateWriter{Writer: hub.w, event: event, entered: make(chan struct{}), release: make(chan struct{})}
			hub.w = gate
			defer close(gate.release)
			started := make(chan struct{})
			node.OnRequest = func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- hub.Call(ctx, "session.items", nil, nil) }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("request not received")
			}
			if event == "stuck" {
				go func() { _ = hub.Notify("stuck", nil) }()
				select {
				case <-gate.entered:
				case <-time.After(time.Second):
					t.Fatal("writer not blocked")
				}
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("cancelled caller blocked on cancellation write")
			}
			if event == cancelEvent {
				select {
				case <-gate.entered:
				case <-time.After(time.Second):
					t.Fatal("cancel was not sent")
				}
			}
			select {
			case <-hub.Done():
				t.Fatal("cancellation closed connection")
			default:
			}
		})
	}
}

func TestCancelledResponseDoesNotWaitForWriter(t *testing.T) {
	w := newStuckWriter()
	p := New("n", w)
	defer p.Close(nil)
	defer close(w.release)
	blockWriter(t, p, w)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	marshaled := make(chan struct{})
	p.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
		return callMarshalFunc(func() ([]byte, error) { close(marshaled); return []byte(`{"items":["old history"]}`), nil }), nil
	}
	done := make(chan struct{})
	go func() { defer close(done); p.handle(ctx, message{ID: "expired", Method: "session.items"}) }()
	select {
	case <-marshaled:
	case <-time.After(time.Second):
		t.Fatal("handler did not marshal reply")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled response stuck behind writer")
	}
	if strings.Contains(w.String(), "expired") {
		t.Fatal("expired reply reached wire")
	}
}

func TestCancelledHandlerDoesNotMarshalResult(t *testing.T) {
	p := New("n", io.Discard)
	defer p.Close(nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
		cancel()
		return callMarshalFunc(func() ([]byte, error) { t.Error("cancelled result marshaled"); return []byte(`{}`), nil }), nil
	}
	p.handle(ctx, message{ID: "expired", Method: "session.items"})
}

func TestNotifyContextDropsCancelledQueuedEvent(t *testing.T) {
	w := newStuckWriter()
	p := New("n", w)
	defer p.Close(nil)
	defer close(w.release)
	blockWriter(t, p, w)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.NotifyContext(ctx, "session.items", json.RawMessage(`{"items":[]}`)) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("notification did not cancel")
	}
}

// Legacy peers ignore unknown events; a dropped cancellation must not poison their connection.
func TestCancellationCompatibleWithPeerIgnoringEvents(t *testing.T) {
	toOldR, toOldW := io.Pipe()
	fromOldR, fromOldW := io.Pipe()
	p := New("h", toOldW)
	p.Cancels = map[string]bool{"session.items": true}
	t.Cleanup(func() { p.Close(nil); toOldW.Close(); fromOldW.Close() })
	go p.Serve(context.Background(), fromOldR)
	cancelSeen := make(chan struct{})
	go func() {
		dec := json.NewDecoder(toOldR)
		enc := json.NewEncoder(fromOldW)
		for {
			var m message
			if dec.Decode(&m) != nil {
				return
			}
			if m.Event == cancelEvent {
				close(cancelSeen) // ignored by the legacy implementation
			}
			if m.Method == "ping" {
				_ = enc.Encode(message{ID: m.ID, Result: json.RawMessage(`{}`)})
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := p.Call(ctx, "session.items", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	select {
	case <-cancelSeen:
	case <-time.After(time.Second):
		t.Fatal("legacy peer did not receive cancellation event")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	if err := p.Call(ctx2, "ping", nil, nil); err != nil {
		t.Fatalf("legacy connection failed after cancellation: %v", err)
	}
}

func TestCancelledOversizedResponseDoesNotClosePeer(t *testing.T) {
	p := New("n", io.Discard)
	defer p.Close(nil)
	p.Limits.FrameBytes = 80
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.OnRequest = func(context.Context, string, json.RawMessage) (any, error) {
		return callMarshalFunc(func() ([]byte, error) {
			cancel() // caller leaves while the response is being serialized
			return json.Marshal(strings.Repeat("history", 100))
		}), nil
	}
	p.handle(ctx, message{ID: "expired", Method: "session.items"})
	select {
	case <-p.Done():
		t.Fatal("one cancelled response closed the entire peer")
	default:
	}
}
