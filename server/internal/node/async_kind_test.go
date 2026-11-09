package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

func TestInlineStartAnswersWithTheCompatibleCLISession(t *testing.T) {
	for _, declared := range []string{"traex", ""} {
		t.Run("agent_type="+declared, func(t *testing.T) {
			n, terminal, home := herdrNode(t)
			withCodexLaunchers(t, map[string][]string{})
			probedLaunchers(t, map[string]string{"example-wrapper": "traex"})
			terminal.reply("workspace.list", map[string]any{"workspaces": []any{map[string]any{"workspace_id": "w1", "label": "example"}}})
			terminal.reply("session.snapshot", snapshot(nil, nil, nil, nil))
			terminal.reply("tab.create", map[string]any{"tab": map[string]any{"tab_id": "w1:t2"}, "root_pane": map[string]any{"pane_id": "w1:p2"}})
			terminal.after(func(c herdrCall) {
				if c.Method == "pane.send_input" && strings.Contains(str(c.Params["text"]), "example-wrapper") {
					writeTraexOwner(t, home, startID, os.Getpid())
					seedTraexPane(os.Getpid(), "w1:p2")
					terminal.reply("session.snapshot", snapshot(nil, nil, nil, []any{map[string]any{"pane_id": "w1:p2", "tab_id": "w1:t2", "workspace_id": "w1", "cwd": home}}))
				}
			})
			const prompt = "Inspect the example project"
			params, _ := json.Marshal(map[string]any{"cwd": home, "workspace_id": "w1", "launcher": "example-wrapper", "agent_type": declared, "prompt": prompt})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := n.handle(ctx, "session.start", params)
			if err != nil {
				t.Fatalf("%v; owners=%v; agents=%v; calls=%v", err, n.traexOwners(), n.agents, terminal.trace())
			}
			session := result.(map[string]any)["session"].(*proto.Session)
			if session.SessionID != startID || session.Agent != "traex" || session.Starting {
				t.Fatalf("inline acknowledgement = %+v", session)
			}
			typed := terminal.trace("pane.send_input")
			if len(typed) != 1 || !strings.Contains(typed[0], prompt) || strings.Contains(typed[0], "--session-id") || strings.Contains(typed[0], "--mcp-config") {
				t.Fatalf("first prompt was not typed exactly once without Claude flags: %v", typed)
			}
		})
	}
}

func TestInlineCodexWrapperAfterNodeRestartMatchesWarmStart(t *testing.T) {
	for _, tc := range []struct {
		name             string
		cold, staleProbe bool
	}{
		{name: "cold", cold: true}, {name: "warm"},
		{name: "cold with older Traex probe", cold: true, staleProbe: true},
		{name: "warm with older Traex probe", staleProbe: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, _, cwd, commands := codexStartHerdr(t, 0)
			home := filepath.Dir(cwd)
			t.Setenv("SHELL", "/bin/zsh")
			t.Setenv("ZDOTDIR", home)
			for _, rc := range []string{".zshrc", ".bashrc"} {
				if err := os.WriteFile(filepath.Join(home, rc), []byte("alias example_wrapper='codex'\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			withCodexLaunchers(t, nil)
			launcherCache.Lock()
			list, settings, scripts := launcherCache.list, launcherCache.settings, launcherCache.scripts
			launcherCache.at, launcherCache.list = time.Time{}, nil
			launcherCache.Unlock()
			t.Cleanup(func() {
				launcherCache.Lock()
				launcherCache.list, launcherCache.settings, launcherCache.scripts = list, settings, scripts
				launcherCache.Unlock()
			})
			if !tc.cold {
				if _, err := n.launchers(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else if got := knownLauncherAgentType("example_wrapper"); got != "claude" {
				t.Fatalf("fixture did not start with cold metadata: %s", got)
			}
			if tc.staleProbe {
				probedLaunchers(t, map[string]string{"example_wrapper": "traex"})
				probeCache.Lock()
				probeCache.at["example_wrapper"] = time.Now().Add(-2 * time.Minute) // Older than a refreshed shell list, still valid for a probe.
				probeCache.Unlock()
			}
			params, _ := json.Marshal(map[string]any{"cwd": cwd, "launcher": "example_wrapper", "prompt": "Inspect the example"})
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := n.handle(ctx, "session.start", params)
			if err != nil {
				t.Fatalf("Codex wrapper picked the wrong driver: %v; typed=%v", err, *commands)
			}
			session := result.(map[string]any)["session"].(*proto.Session)
			if session.Agent != "codex" || session.SessionID != startThread || session.Starting || len(*commands) != 1 || knownLauncherAgentType("example_wrapper") != "codex" {
				t.Fatalf("wrapper acknowledgement = %+v, commands = %v", session, *commands)
			}
		})
	}
}

func TestAsyncStartRefusesOtherKindsBeforeSideEffects(t *testing.T) {
	for _, tc := range []struct {
		name, launcher, listed, probed, declared string
	}{
		{name: "compatible CLI", launcher: "traex"},
		{name: "explicit compatible CLI", launcher: "traex", declared: "traex"},
		{name: "listed wrapper", launcher: "example-compatible", listed: "traex"},
		{name: "probed wrapper", launcher: "example-probed", probed: "traex"},
		{name: "stale Claude declaration", launcher: "example-changed", listed: "traex", declared: "claude"},
		{name: "Codex alias", launcher: "example-codex", listed: "codex"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, terminal, home := herdrNode(t)
			withCodexLaunchers(t, map[string][]string{})
			launcherCache.Lock()
			previous := launcherCache.list
			launcherCache.list = []proto.Launcher{{Name: tc.launcher, Agent: tc.listed}}
			launcherCache.Unlock()
			t.Cleanup(func() { launcherCache.Lock(); launcherCache.list = previous; launcherCache.Unlock() })
			if tc.probed != "" {
				probedLaunchers(t, map[string]string{tc.launcher: tc.probed})
			}
			cwd := mkdir(t, filepath.Join(home, "example-project"))
			_, err := request(t, n, "session.start", map[string]any{
				"cwd": cwd, "launcher": tc.launcher, "async": true, "agent_type": tc.declared,
			})
			code, data := refusalOf(t, err)
			if code != "codex_needs_prompt" || data["delivered"] != "none" {
				t.Fatalf("refusal = %s %v", code, data)
			}
			if err.Error() != i18n.T(i18n.Default, "rpc-errors.codex_needs_prompt") {
				t.Fatalf("refusal did not use its locale resource: %v", err)
			}
			n.starts.mu.Lock()
			count := len(n.starts.m)
			n.starts.mu.Unlock()
			if count != 0 || len(n.state.Sessions) != 0 {
				t.Fatal("refused start created a starting or persisted session")
			}
			for _, method := range []string{"tab.create", "workspace.create", "pane.send_input"} {
				if calls := terminal.trace(method); len(calls) != 0 {
					t.Fatalf("refused start dispatched %s: %v", method, calls)
				}
			}
		})
	}
}

func TestClaudeAsyncRPCStillOwnsTheStartingSession(t *testing.T) {
	n, home := testNode(t)
	n.modes = map[string]sessionModes{}
	result, err := request(t, n, "session.start", map[string]any{"cwd": home, "launcher": "claude", "async": true})
	if err != nil {
		t.Fatal(err)
	}
	session := decodeSessions(t, []any{result["session"]})[0]
	if !session.Starting || session.Agent != "claude" {
		t.Fatalf("Claude acknowledgement = %+v", session)
	}
	select {
	case <-n.starts.get(session.SessionID).done: // This fixture has no terminal host; let its background failure finish.
	case <-time.After(5 * time.Second):
		t.Fatal("the fixture start did not terminalize")
	}
}

func TestSyncStartUsesTheKnownWrapperKind(t *testing.T) {
	n, _, home := herdrNode(t)
	withCodexLaunchers(t, map[string][]string{})
	probedLaunchers(t, map[string]string{"example-compatible": "traex"})
	_, err := request(t, n, "session.start", map[string]any{
		"cwd": home, "launcher": "example-compatible", "prompt": "Inspect the example", "permission_mode": "plan",
	})
	// The wrapper is a compatible CLI, whose flags cannot enforce read-only. It must never take Claude's start path.
	code, _ := refusalOf(t, err)
	if code != "readonly_unsupported" {
		t.Fatalf("wrapper start kind was lost: %v", err)
	}
}
