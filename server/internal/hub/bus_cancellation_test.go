package hub

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/devicewire"
	"testing"
	"time"
)

func TestBusCancelsHistoryButNotAgentMutations(t *testing.T) {
	h, srv, _ := newBusHub(t)
	ended := make(chan bool, 1)
	release := make(chan struct{})
	defer close(release)
	service := dialRawBusWith(t, srv, busToken(t, h, "assistant", "assistant"), func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if method != devicewire.MethodCall && method != assistantwire.MethodFleetAsk {
			return map[string]any{}, nil
		}
		select {
		case <-ctx.Done():
			ended <- true
		case <-release:
			ended <- false
		case <-time.After(150 * time.Millisecond):
			ended <- false
		}
		return map[string]any{}, nil
	})
	if err := service.call(busHello, map[string]any{"id": "assistant"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		outer, inner string
		wantCancel   bool
	}{
		{devicewire.MethodCall, "session.items", true},
		{devicewire.MethodCall, "session.start", false},
		{devicewire.MethodCall, "session.send", false},
		{assistantwire.MethodFleetAsk, "", true},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		err := h.callBusAsIs(ctx, "assistant", tc.outer, devicewire.NodeCall{MachineID: "box", Method: tc.inner}, nil)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s/%s: %v", tc.outer, tc.inner, err)
		}
		select {
		case got := <-ended:
			if got != tc.wantCancel {
				t.Fatalf("%s/%s cancellation=%v", tc.outer, tc.inner, got)
			}
		case <-time.After(time.Second):
			t.Fatal("handler did not finish")
		}
	}
}

func TestCancelDeviceReadEnvelope(t *testing.T) {
	for _, tc := range []struct {
		outer, raw string
		cancel     bool
	}{
		{devicewire.MethodCall, `{"machine_id":"box","method":"session.items","params":{"session_id":"s"}}`, true},
		{devicewire.MethodCall, `{"method":"session.start"}`, false},
		{devicewire.MethodCall, `{"method":"session.send"}`, false},
		{devicewire.MethodCall, `{"method":"session.watch"}`, false},
		{devicewire.MethodCall, `{"method":"session.items"`, false},
		{devicewire.MethodCall, `{}`, false},
		{"plugin.call", `{"method":"session.items"}`, false},
	} {
		if got := cancelDeviceRead(tc.outer, json.RawMessage(tc.raw)); got != tc.cancel {
			t.Errorf("%s %s: cancellation=%v, want %v", tc.outer, tc.raw, got, tc.cancel)
		}
	}
}
