package devices

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/nodeproto"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestTailcatRPCManualDeployWithAutoUpdatesDisabled(t *testing.T) {
	isolateNodeBinaries(t)
	h, m, n, _ := linkedDeployFixture(t, "tailcat")
	h.cfg.AutoUpdateNodes = false
	h.opts.Store = newMemStore()
	m.info.Protocol = nodeproto.Version
	m.info.Capabilities = []string{nodeproto.NodeUpdate}
	peer := m.peer
	// Fail before entering deployment if routing regresses: this test must never
	// open the legacy tailcat upload tunnel.
	m.mu.Lock()
	usesRPC := m.rpcDeploymentLocked()
	m.mu.Unlock()
	if !usesRPC {
		t.Fatal("capable tailcat node must deploy through its existing RPC peer")
	}
	if err := os.WriteFile(filepath.Join(nodeBinaryDir(), "personad-linux-amd64"), n.expected, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := m.queueOfflineNodeUpdate(ctx); err != nil {
		t.Fatal(err)
	}
	m.nodeUpdateConnected(ctx, peer, m.info)
	if m.snapshot().Deploying {
		t.Fatal("reconnect resumed a queued tailcat upgrade with automatic updates disabled")
	}
	if err := m.startAutoDeploy(); !errors.Is(err, errAutoNodeUpdatesDisabled) {
		t.Fatalf("automatic deployment gate: %v", err)
	}
	n.mu.Lock()
	before := n.begin + n.commits + n.confirms
	n.mu.Unlock()
	if before != 0 {
		t.Fatal("automatic deployment sent update RPCs")
	}
	var queued nodeDeployRecord
	if found, err := h.opts.Store.Setting(ctx, m.deployRecordKey(), &queued); err != nil || !found || queued.State != "queued" {
		t.Fatalf("queued deployment changed: found=%v err=%v record=%+v", found, err, queued)
	}
	if err := m.startDeploy(); err != nil {
		t.Fatalf("explicit manual deployment: %v", err)
	}
	waitMachine(t, m, 5*time.Second, func(s proto.Machine) bool { return !s.Deploying })
	m.mu.Lock()
	samePeer, version := m.peer == peer, m.info.Version
	m.mu.Unlock()
	if !samePeer || version != "new-build" || m.snapshot().DeployError != "" {
		t.Fatalf("manual deployment: samePeer=%v version=%q error=%q", samePeer, version, m.snapshot().DeployError)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.begin != 1 || n.commits != 1 || n.confirms != 1 || n.aborts != 0 || !bytes.Equal(n.data, n.expected) {
		t.Fatalf("begin=%d commits=%d confirms=%d aborts=%d bytes=%d", n.begin, n.commits, n.confirms, n.aborts, len(n.data))
	}
	var completed nodeDeployRecord
	if found, err := h.opts.Store.Setting(ctx, m.deployRecordKey(), &completed); err != nil || !found || completed.State != "succeeded" || completed.Target != "new-build" {
		t.Fatalf("completed deployment: found=%v err=%v record=%+v", found, err, completed)
	}
}

func TestTailcatRPCDoesNotTransferWhileBusy(t *testing.T) {
	for _, background := range []bool{false, true} {
		name := "working-session"
		if background {
			name = "background-task"
		}
		t.Run(name, func(t *testing.T) {
			_, m, n, bin := linkedDeployFixture(t, "tailcat")
			n.busySession, n.background = !background, background
			record := nodeDeployRecord{Target: "new-build", State: "queued"}
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			if err := m.performRPCDeployment(ctx, bin, &record); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("busy deployment: %v", err)
			}
			n.mu.Lock()
			defer n.mu.Unlock()
			if n.begin != 0 || n.commits != 0 || n.confirms != 0 || len(n.data) != 0 {
				t.Fatalf("busy node received update: begin=%d commits=%d confirms=%d bytes=%d", n.begin, n.commits, n.confirms, len(n.data))
			}
		})
	}
}

func TestTailcatRPCRequiresUpdateCapability(t *testing.T) {
	for _, tc := range []struct {
		name string
		info proto.NodeInfo
		want bool
	}{
		{"legacy-without-update", proto.NodeInfo{}, false},
		{"legacy-update-flag", proto.NodeInfo{NodeUpdate: true}, true},
		{"missing-announced-capability", proto.NodeInfo{Protocol: nodeproto.Version, NodeUpdate: true}, false},
		{"missing-update-flag", proto.NodeInfo{Protocol: nodeproto.Version, Capabilities: []string{nodeproto.NodeUpdate}}, false},
		{"capable", proto.NodeInfo{Protocol: nodeproto.Version, Capabilities: []string{nodeproto.NodeUpdate}, NodeUpdate: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(newTestDevices(t), MachineConfig{ID: "home-mac-mini", Kind: "tailcat"})
			m.mu.Lock()
			m.info = tc.info
			got := m.rpcDeploymentLocked()
			m.mu.Unlock()
			if got != tc.want {
				t.Fatalf("RPC deployment = %v, want %v (false preserves legacy deployment)", got, tc.want)
			}
		})
	}
}
