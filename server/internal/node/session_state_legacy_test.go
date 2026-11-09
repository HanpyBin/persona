package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/rpc"
)

const legacySessionJSON = `{"sessions":{` +
	`"sess-1":{"launcher":"herdr","cwd":"/work/one","started_at":"2026-01-01T00:00:00Z"},` +
	`"sess-2":{"launcher":"orca","cwd":"/work/two","started_at":"2026-01-02T00:00:00Z"}` +
	`}}`

func writeLegacyFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A valid legacy file parses and nothing is quarantined.
func TestLegacyStateReadsValidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	writeLegacyFile(t, path, legacySessionJSON)
	sessions, ok := legacyState(t.Logf, path)
	if !ok || len(sessions) != 2 {
		t.Fatalf("legacy state: ok %v, %d sessions", ok, len(sessions))
	}
	if got := sessions["sess-1"].Launcher; got != "herdr" {
		t.Fatalf("sess-1 launcher %q", got)
	}
	if _, err := os.Stat(path + legacyCorruptSuffix); !os.IsNotExist(err) {
		t.Fatalf("a valid file must not be quarantined: %v", err)
	}
}

// No file means nothing to migrate, with no fault and no quarantine.
func TestLegacyStateMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	if sessions, ok := legacyState(t.Logf, path); ok || sessions != nil {
		t.Fatalf("a missing file: ok %v sessions %v", ok, sessions)
	}
	if _, err := os.Stat(path + legacyCorruptSuffix); !os.IsNotExist(err) {
		t.Fatalf("a missing file must not be quarantined: %v", err)
	}
}

// An unparseable legacy file is quarantined once and logged, instead of returning empty on every connection.
func TestLegacyStateQuarantinesUnparseableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	writeLegacyFile(t, path, "{not json")
	var logs strings.Builder
	sessions, ok := legacyState(func(f string, a ...any) {
		fmt.Fprintf(&logs, f+"\n", a...)
	}, path)
	if ok || sessions != nil {
		t.Fatalf("a corrupt file: ok %v sessions %v", ok, sessions)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the corrupt file was not moved away: %v", err)
	}
	if _, err := os.Stat(path + legacyCorruptSuffix); err != nil {
		t.Fatalf("the corrupt file was not quarantined: %v", err)
	}
	if !strings.Contains(logs.String(), "quarantined") {
		t.Fatalf("the quarantine was not logged:\n%s", logs.String())
	}
	// The second connection no longer sees the corrupt file: it is not re-read or re-quarantined.
	if sessions, ok := legacyState(t.Logf, path); ok || sessions != nil {
		t.Fatalf("a quarantined file must not be read again: ok %v", ok)
	}
}

// An unreadable legacy file is reported as missing and left in place so fixing the permissions lets it migrate.
func TestLegacyStateUnreadableFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission faults do not trigger for root")
	}
	path := filepath.Join(t.TempDir(), "sessions.json")
	writeLegacyFile(t, path, legacySessionJSON)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if sessions, ok := legacyState(t.Logf, path); ok || sessions != nil {
		t.Fatalf("an unreadable file: ok %v sessions %v", ok, sessions)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("an unreadable file must stay in place for a retry: %v", err)
	}
	if _, err := os.Stat(path + legacyCorruptSuffix); !os.IsNotExist(err) {
		t.Fatalf("an unreadable file must not be quarantined: %v", err)
	}
}

// statePutHub wires n.peer to a fake hub: it records state.put events and answers state.load.
type statePutHub struct {
	mu   sync.Mutex
	puts []map[string]any
}

func (h *statePutHub) putsLen() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.puts)
}

func wireStatePutHub(t *testing.T, n *Node) *statePutHub {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	n2hR, n2hW := io.Pipe()
	h2nR, h2nW := io.Pipe()
	n.peer = rpc.New("n", n2hW)
	hub := rpc.New("h", h2nW)
	rec := &statePutHub{}
	hub.OnEvent = func(event string, params json.RawMessage) {
		if event != "state.put" {
			return
		}
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		rec.mu.Lock()
		rec.puts = append(rec.puts, p)
		rec.mu.Unlock()
	}
	hub.OnRequest = func(_ context.Context, method string, _ json.RawMessage) (any, error) {
		if method == "state.load" {
			return map[string]any{"sessions": map[string]sessionState{}}, nil
		}
		return nil, nil
	}
	go func() { _ = hub.Serve(ctx, n2hR) }()
	go func() { _ = n.peer.Serve(ctx, h2nR) }()
	t.Cleanup(func() {
		cancel()
		_ = n2hW.Close()
		_ = h2nW.Close()
	})
	return rec
}

func newLegacyNode(t *testing.T) (*Node, string) {
	t.Helper()
	dir := t.TempDir()
	n := &Node{
		dir:    dir,
		logger: log.New(io.Discard, "node: ", log.LstdFlags),
		state:  newState(),
	}
	return n, filepath.Join(dir, "sessions.json")
}

// The happy path is unchanged: every state.put reaches the hub, the state is merged and the file is renamed once.
func TestLoadStateRenamesLegacyFileOnlyAfterAllPutsSucceed(t *testing.T) {
	n, legacyPath := newLegacyNode(t)
	rec := wireStatePutHub(t, n)
	writeLegacyFile(t, legacyPath, legacySessionJSON)

	if ok := n.loadState(context.Background()); !ok {
		t.Fatal("loadState failed on the happy path")
	}
	waitFor(t, "both state.put events to arrive", func() bool { return rec.putsLen() == 2 })
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("the migrated file was not renamed: %v", err)
	}
	if _, err := os.Stat(legacyPath + ".migrated"); err != nil {
		t.Fatalf("the legacy file was not marked migrated: %v", err)
	}
	ids := map[string]bool{}
	rec.mu.Lock()
	for _, p := range rec.puts {
		id, _ := p["session_id"].(string)
		ids[id] = true
	}
	rec.mu.Unlock()
	if !ids["sess-1"] || !ids["sess-2"] {
		t.Fatalf("the hub got %v, want both sessions", ids)
	}
	if _, ok := n.state.get("sess-1"); !ok {
		t.Fatal("the migrated sessions were not merged into the state")
	}
}

// A Notify failure keeps the original file for the next connection's retry: no rename and no partial merge.
func TestLoadStateKeepsLegacyFileWhenANotifyFails(t *testing.T) {
	n, legacyPath := newLegacyNode(t)
	wireStatePutHub(t, n)
	writeLegacyFile(t, legacyPath, legacySessionJSON)
	n.peer.Close(rpc.ErrClosed) // every state.put now fails

	if ok := n.loadState(context.Background()); ok {
		t.Fatal("loadState must fail when neither the puts nor state.load reach the hub")
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("the original file must stay for a retry: %v", err)
	}
	if _, err := os.Stat(legacyPath + ".migrated"); !os.IsNotExist(err) {
		t.Fatalf("a failed migration must not rename the file: %v", err)
	}
	if got := len(n.state.Sessions); got != 0 {
		t.Fatalf("nothing must be merged after a failed migration, got %d sessions", got)
	}

	// The next connection retries: with a live hub the full migration now goes through.
	n2 := &Node{
		dir:    filepath.Dir(legacyPath),
		logger: log.New(io.Discard, "node: ", log.LstdFlags),
		state:  newState(),
	}
	rec := wireStatePutHub(t, n2)
	if ok := n2.loadState(context.Background()); !ok {
		t.Fatal("the retry migration failed")
	}
	waitFor(t, "the retried state.put events to arrive", func() bool { return rec.putsLen() == 2 })
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatalf("the retried migration did not rename the file: %v", err)
	}
}
