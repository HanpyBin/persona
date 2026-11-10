package procs

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// roleEnv runs the test binary as one process of the hub's tree (TestMain): "start" calls AtStart, "cap-at" and
// "cap-below" call atStart with a cap at and just below the runtime's default, and "grand" is a program that process
// starts with its own environment, as the manager starts a node, an agent or a tool.
const roleEnv = "PERSONA_TEST_PROCS_ROLE"

func TestMain(m *testing.M) {
	if role := os.Getenv(roleEnv); role != "" {
		os.Exit(entry(role))
	}
	os.Exit(m.Run())
}

// entry prints one line: GOMAXPROCS before and after the call, the GOMAXPROCS and Marker left in its environment, the
// ones ChildEnv hands a child of the hub's, and the ones a program it starts sees ("-": not set).
func entry(role string) int {
	if role == "grand" {
		fmt.Printf("\ngrand %s %s\n", env(os.Environ(), envProcs), env(os.Environ(), Marker))
		return 0
	}
	before := runtime.GOMAXPROCS(0)
	switch role {
	case "start":
		AtStart()
	case "cap-at":
		atStart(before)
	case "cap-below":
		atStart(before - 1)
	}
	child := ChildEnv(os.Environ())
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), roleEnv+"=grand")
	out, err := cmd.Output()
	i := strings.LastIndex(string(out), "\ngrand ")
	if err != nil || i < 0 {
		fmt.Fprintf(os.Stderr, "grandchild: %v\n%s", err, out)
		return 1
	}
	fmt.Printf("\nprocs %d %d %s %s %s %s %s\n", before, runtime.GOMAXPROCS(0), env(os.Environ(), envProcs),
		env(os.Environ(), Marker), env(child, envProcs), env(child, Marker), strings.TrimSpace(string(out[i+len("\ngrand "):])))
	return 0
}

// env is the value of key in env as exec passes it (the last one wins), "-" when it is not there.
func env(env []string, key string) string {
	v := "-"
	for _, e := range env {
		if rest, ok := strings.CutPrefix(e, key+"="); ok {
			v = rest
		}
	}
	return v
}

// The cap's rules, each through a process of its own (GOMAXPROCS is read when the runtime starts): the user's
// GOMAXPROCS wins and is passed on as it is; a default at or below the cap is left alone and nothing is passed; above
// it, the process lowers it and hands the hub's children the cap with the marker; a child given both takes them out
// of its environment, keeps the cap and passes it on, and nothing it starts sees them; a marker without its
// GOMAXPROCS is dropped and the user's value kept.
func TestCap(t *testing.T) {
	for _, c := range []struct {
		name, role string
		env        []string
		want       func(before int) string // after, its GOMAXPROCS and marker, ChildEnv's, a program's it starts
	}{
		{"user GOMAXPROCS set", "start", []string{"GOMAXPROCS=3"},
			func(int) string { return "3 3 - 3 - 3 -" }},
		{"default at the cap", "cap-at", nil,
			func(b int) string { return fmt.Sprintf("%d - - - - - -", b) }},
		{"default above the cap", "cap-below", nil,
			func(b int) string { return fmt.Sprintf("%d - - %[1]d %[1]d - -", b-1) }},
		{"marker present", "start", []string{"GOMAXPROCS=2", Marker + "=2"},
			func(int) string { return "2 - - 2 2 - -" }},
		{"marker without its GOMAXPROCS", "start", []string{"GOMAXPROCS=3", Marker + "=2"},
			func(int) string { return "3 3 - 3 - 3 -" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^$")
			cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), roleEnv + "=" + c.role}, c.env...)
			out, err := cmd.Output()
			i := strings.LastIndex(string(out), "\nprocs ")
			if err != nil || i < 0 {
				t.Fatalf("%v\n%s", err, out)
			}
			before, rest, _ := strings.Cut(strings.TrimSpace(string(out[i+len("\nprocs "):])), " ")
			b, err := strconv.Atoi(before)
			if err != nil {
				t.Fatal(err)
			}
			if c.role == "cap-below" && b < 2 {
				t.Skip("one P by default: nothing to lower")
			}
			if want := c.want(b); rest != want {
				t.Fatalf("GOMAXPROCS %d, then (after, env, marker, child's, grandchild's) %q, want %q", b, rest, want)
			}
		})
	}
}
