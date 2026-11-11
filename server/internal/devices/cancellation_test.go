package devices

import (
	"context"
	"encoding/json"
	"github.com/nwverse/persona/server/internal/devicewire"
	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"net"
	"testing"
	"time"
)

func TestHistoryCancellationReachesConnectedNode(t *testing.T) {
	d := newTestDevices(t)
	m := newMachine(d, MachineConfig{ID: "box", Name: "box", Kind: "ssh"})
	ended := make(chan bool, 1)
	m.dialNode = func(context.Context) (net.Conn, error) {
		managerSide, nodeSide := net.Pipe()
		peer := rpc.New("n", nodeSide)
		peer.OnRequest = func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
			switch method {
			case "node.info":
				return proto.NodeInfo{Hostname: "box", OS: "linux", Arch: "amd64", Home: "/home/test"}, nil
			case "session.items", "session.send":
				select {
				case <-ctx.Done():
					ended <- true
				case <-time.After(150 * time.Millisecond):
					ended <- false
				}
			}
			return map[string]any{}, nil
		}
		go func() { _ = peer.Serve(context.Background(), nodeSide); nodeSide.Close() }()
		return managerSide, nil
	}
	d.addEntry(m)
	runMachine(t, m)
	eventually(t, 5*time.Second, "node connected", m.reachable)
	for _, method := range []string{"session.items", "session.send"} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		_, err := d.Call(ctx, devicewire.NodeCall{MachineID: "box", Method: method, Params: json.RawMessage(`{"session_id":"s"}`)})
		cancel()
		if err == nil {
			t.Fatal("expected expired call")
		}
		select {
		case cancelled := <-ended:
			if cancelled != (method == "session.items") {
				t.Fatalf("%s cancelled=%v", method, cancelled)
			}
		case <-time.After(time.Second):
			t.Fatal("node handler did not finish")
		}
	}
}
