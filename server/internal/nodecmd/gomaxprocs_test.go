//go:build linux

package nodecmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/osproc/osproctest"
	"github.com/nwverse/persona/server/internal/procs"
)

// procsEntryEnv runs the test binary as the command in its value (words split on spaces) with stdin closed, then
// prints GOMAXPROCS before and after it.
const procsEntryEnv = "PERSONA_TEST_PROCS_ENTRY"

func procsEntry(args string) int {
	before := runtime.GOMAXPROCS(0)
	if _, err := Run(context.Background(), func() {}, strings.Fields(args), Build{Version: "test"}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("\nprocs %d %d\n", before, runtime.GOMAXPROCS(0))
	return 0
}

// The node that serves a hub keeps at most procs.Max Ps; other commands keep the runtime's value. (On a machine with
// procs.Max CPUs or fewer the runtime's value is already that.) A GOMAXPROCS variable wins: internal/procs tests that.
func TestNodeCapsProcs(t *testing.T) {
	for _, c := range []struct {
		args   string
		capped bool
	}{
		{"node --stdio", true},
		{"node-update-check", false},
	} {
		t.Run(c.args, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^$")
			cmd.Env = osproctest.HomeEnv(t, procsEntryEnv+"="+c.args)
			out, err := cmd.Output()
			var before, after int
			if i := strings.LastIndex(string(out), "\nprocs "); err != nil || i < 0 {
				t.Fatalf("%v: %v\n%s", c.args, err, out)
			} else if _, err := fmt.Sscanf(string(out[i:]), "\nprocs %d %d", &before, &after); err != nil {
				t.Fatal(err)
			}
			want := before
			if c.capped {
				want = min(before, procs.Max)
			}
			if after != want {
				t.Fatalf("%s: GOMAXPROCS %d -> %d, want %d", c.args, before, after, want)
			}
		})
	}
}
