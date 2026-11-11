package assistant

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
)

type standingRepositoryFixture struct{ main, tree, other string }

func newStandingRepositoryFixture(t *testing.T) standingRepositoryFixture {
	t.Helper()
	root := t.TempDir()
	f := standingRepositoryFixture{main: filepath.Join(root, "source"), tree: filepath.Join(root, "elsewhere", "worktree"), other: filepath.Join(root, "source-copy")}
	git := func(dir string, args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-C", dir}, args...)...)
		command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Example", "GIT_AUTHOR_EMAIL=dev@example.com", "GIT_COMMITTER_NAME=Example", "GIT_COMMITTER_EMAIL=dev@example.com")
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s %v", args, out, err)
		}
	}
	git(root, "init", "--initial-branch=main", f.main)
	if err := os.MkdirAll(filepath.Join(f.main, "server"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.main, "server", "app.txt"), []byte("example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git(f.main, "add", ".")
	git(f.main, "commit", "-m", "Add example")
	git(f.main, "worktree", "add", "-b", "worker", f.tree)
	git(root, "init", "--initial-branch=main", f.other)
	return f
}
func standingFixtureIdentity(t *testing.T, cwd string) proto.RepositoryIdentity {
	t.Helper()
	read := func(args ...string) string {
		command := exec.Command("git", append([]string{"-C", cwd}, args...)...)
		out, err := command.Output()
		if err != nil {
			t.Fatalf("git metadata: %v", err)
		}
		return strings.TrimSpace(string(out))
	}
	root := read("rev-parse", "--show-toplevel")
	common := read("rev-parse", "--git-common-dir")
	if !filepath.IsAbs(common) {
		common = filepath.Join(cwd, common)
	}
	physical, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	return proto.RepositoryIdentity{Version: 1, IsRepo: true, Cwd: physical, Root: root, CommonDir: filepath.Clean(common)}
}
func standingFixtureNode(t *testing.T, f *fakeHub) {
	t.Helper()
	f.answerNode("git.status", func(_ string, args json.RawMessage) (any, error) {
		var params struct {
			Cwd          string `json:"cwd"`
			IdentityOnly bool   `json:"identity_only"`
		}
		if err := json.Unmarshal(args, &params); err != nil {
			t.Fatal(err)
		}
		if !params.IdentityOnly {
			t.Fatal("authorization matching requested full status")
		}
		return standingFixtureIdentity(t, params.Cwd), nil
	})
}
func TestStandingAuthorizationRepositoryScope(t *testing.T) {
	fixture := newStandingRepositoryFixture(t)
	s, f := newQuietService(t)
	standingFixtureNode(t, f)
	for _, test := range []struct {
		scope, cwd string
		want       bool
	}{
		{fixture.main, fixture.tree, true}, {fixture.main, filepath.Join(fixture.tree, "server"), true},
		{filepath.Join(fixture.main, "server"), filepath.Join(fixture.tree, "server"), true}, {filepath.Join(fixture.main, "server"), fixture.tree, false}, {fixture.main, fixture.other, false},
	} {
		got, err := s.standingScopeMatches(t.Context(), "box", test.scope, test.cwd)
		if err != nil || got != test.want {
			t.Errorf("scope %s cwd %s: %v %v", test.scope, test.cwd, got, err)
		}
	}
}
func TestStandingAuthorizationRepositoryOriginScope(t *testing.T) {
	s, f := newQuietService(t)
	f.answerNode("git.status", func(_ string, args json.RawMessage) (any, error) {
		var params struct {
			Cwd string `json:"cwd"`
		}
		_ = json.Unmarshal(args, &params)
		root, common, origin := "/work/main", "/git/main", "example.com/team/app"
		if strings.HasPrefix(params.Cwd, "/work/clone") {
			root = "/work/clone"
			common = "/git/clone"
		}
		if strings.HasPrefix(params.Cwd, "/work/different") {
			root = "/work/different"
			common = "/git/different"
			origin = "example.com/team/other"
		}
		return proto.RepositoryIdentity{Version: 1, IsRepo: true, Cwd: params.Cwd, Root: root, CommonDir: common, Origin: origin}, nil
	})
	for _, test := range []struct {
		scope, cwd string
		want       bool
	}{{"/work/main", "/work/clone", true}, {"/work/main/server", "/work/clone/server", true}, {"/work/main/server", "/work/clone/server-copy", false}, {"/work/main", "/work/different", false}} {
		got, err := s.standingScopeMatches(t.Context(), "box", test.scope, test.cwd)
		if err != nil || got != test.want {
			t.Errorf("%+v: %v %v", test, got, err)
		}
	}
}
func TestStandingAuthorizationWorktreeDispatch(t *testing.T) {
	fixture := newStandingRepositoryFixture(t)
	for _, method := range []string{"task_plan", "agent_send"} {
		t.Run(method, func(t *testing.T) {
			database := taskRecoveryDatabase(t)
			grant := dispatchAuthorization()
			grant.Directory = fixture.main
			saveDispatchAuthorizations(t, database, grant)
			s, f := newQuietServiceOn(t, database)
			t.Cleanup(func() { settleService(t, s) })
			standingFixtureNode(t, f)
			if method == "task_plan" {
				task := PlanTask{Title: "Fix the app", Prompt: "Fix the app and report the tests.", Machine: "box", Cwd: fixture.tree, Permission: "bypassPermissions", UserRequested: true}
				if _, err := s.toolTaskPlan(t.Context(), "conv1", readonlyTaskArgs(t, task)); err != nil {
					t.Fatal(err)
				}
				waitFor(t, "worktree task dispatch", func() bool { return len(f.started()) == 1 })
				text, _ := f.started()[0]["prompt"].(string)
				assertDispatchAuthorization(t, text, grant, true)
				t.Logf("WORKTREE_TASK_INSTRUCTIONS\n%s", text)
			} else {
				secretary(f)
				f.mu.Lock()
				f.sessions["box"][0].Cwd = fixture.tree
				f.mu.Unlock()
				if _, err := runJSON(t, s, "conv1", "agent_send", map[string]any{"machine": "box", "session_id": "sec00001", "text": "Fix the app.", "task": map[string]any{"title": "Fix the app"}}); err != nil {
					t.Fatal(err)
				}
				sent := sentToAgent(f)
				if len(sent) != 1 {
					t.Fatalf("sent: %v", sent)
				}
				assertDispatchAuthorization(t, sent[0], grant, true)
			}
		})
	}
}
