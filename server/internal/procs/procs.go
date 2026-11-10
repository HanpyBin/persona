// Package procs keeps how many Ps (GOMAXPROCS) Persona's long-running processes use: the node, and the hub's edge,
// slot and built-in manager. They are I/O-bound and sleep most of the time, between reads, writes and a few timers; on
// a host with many cores every P Go keeps is one more to wake and search for work, and more threads, stacks and per-P
// caches held (docs/lessons/server.md, "GOMAXPROCS for the hub's processes").
package procs

import (
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
)

// Max is the most Ps these processes keep unless the GOMAXPROCS environment variable says otherwise.
const Max = 4

// Marker goes into a child's environment beside the GOMAXPROCS that ChildEnv puts there, with the same value: the
// child (AtStart) then knows the variable is the cap passed down, not the user's, and takes both out again.
const Marker = "PERSONA_GOMAXPROCS_CAP"

const envProcs = "GOMAXPROCS"

// passed is the cap this process hands the hub's own children (ChildEnv): set when Cap lowered GOMAXPROCS here or
// AtStart took the cap a parent passed; 0 while the user's GOMAXPROCS or the runtime's default applies.
var passed atomic.Int32

// Cap lowers GOMAXPROCS to n in this process, unless the GOMAXPROCS environment variable is set (that one wins, as it
// does for the runtime). A runtime.GOMAXPROCS call stops the runtime's own updates of it (from a container's or
// cgroup's CPU limit changing later), so it is made only when it lowers it: a machine or container with n CPUs or
// fewer keeps the runtime's value and its updates, and has no cap to pass on. Nothing is put in the environment: what
// this process starts keeps its own default, except the hub's own children, which get the cap from ChildEnv.
func Cap(n int) {
	if os.Getenv(envProcs) != "" {
		return
	}
	if runtime.GOMAXPROCS(0) > n {
		runtime.GOMAXPROCS(n)
		passed.Store(int32(n))
	}
}

// AtStart is what the hub's slot, the built-in manager and the node call first thing. When the parent passed the cap
// (ChildEnv: GOMAXPROCS and Marker with the same value), the runtime has already started with that many Ps; AtStart
// takes both variables out of this process's environment, so the node, agents, extensions and tools it starts keep
// their own default, and remembers the cap for ChildEnv (the slot passes it on to its manager). A marker that does not
// match GOMAXPROCS is dropped and the variable kept: that one is the user's. With no cap passed (a slot an older edge
// started, a manager run as a service of its own, the node) it is Cap(Max).
func AtStart() { atStart(Max) }

func atStart(n int) {
	if mark, ok := os.LookupEnv(Marker); ok {
		_ = os.Unsetenv(Marker)
		if v, err := strconv.Atoi(mark); err == nil && v > 0 && os.Getenv(envProcs) == mark {
			_ = os.Unsetenv(envProcs)
			passed.Store(int32(v))
			return
		}
	}
	Cap(n)
}

// ChildEnv is env for one of the hub's own children (the edge's slot, the hub's built-in manager), with GOMAXPROCS and
// Marker set to this process's cap when it has one, so the child starts with that many Ps rather than making one per
// core and lowering them after (the runtime's start-up structures for every P would stay reachable). env comes back
// as it is when there is no cap to pass or it already sets GOMAXPROCS (the user's, which the child keeps).
func ChildEnv(env []string) []string {
	n := passed.Load()
	if n == 0 {
		return env
	}
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, envProcs+"="); ok && v != "" {
			return env
		}
	}
	v := strconv.Itoa(int(n))
	return append(slices.Clip(env), envProcs+"="+v, Marker+"="+v)
}
