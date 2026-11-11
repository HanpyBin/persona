package node

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/rpc"
	"github.com/nwverse/persona/server/internal/transcript"
)

// Observing Done proves the operation reached a cancellation-aware wait before the test cancels it.
type historyWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *historyWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func historyCancelFixture(t *testing.T) (*Node, *transcript.File, string) {
	t.Helper()
	n, home := testNode(t)
	path := filepath.Join(home, ".claude", "projects", "-work", watchedSession+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	writeCodexToolLines(t, path, 0, 1)
	f, err := n.file(watchedSession)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Refresh(); err != nil {
		t.Fatal(err)
	}
	return n, f, path
}

func TestSessionItemsCancellationWhileWaitingForPushLock(t *testing.T) {
	n, f, path := historyCancelFixture(t)
	writeCodexToolLines(t, path, 1, 1)
	lock := n.pushLock(watchedSession)
	lock.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	params, _ := json.Marshal(map[string]any{"session_id": watchedSession, "lazy_images": true})
	observed := &historyWaitContext{Context: ctx, waiting: make(chan struct{})}
	go func() { _, err := n.handle(observed, "session.items", params); done <- err }()
	<-observed.waiting
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled read remained blocked on push lock")
	}
	if got := f.LastSeq(); got != 1 {
		t.Fatalf("canceled read advanced file to %d", got)
	}
	lock.Unlock()
	if _, err := n.handle(context.Background(), "session.items", params); err != nil {
		t.Fatal(err)
	}
	if f.LastSeq() != 2 {
		t.Fatal("next request could not refresh")
	}
}

func TestWatchInitializationCancellationDoesNotLeaveWatch(t *testing.T) {
	n, _, _ := historyCancelFixture(t)
	lock := n.pushLock(watchedSession)
	lock.Lock()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- n.watchRequest(ctx, context.Background(), watchedSession, 1) }()
	waitFor(t, "watch to register", func() bool { n.mu.Lock(); defer n.mu.Unlock(); return n.watches[watchedSession] != nil })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("watch initialization remained blocked")
	}
	n.mu.Lock()
	w := n.watches[watchedSession]
	n.mu.Unlock()
	if w != nil {
		t.Fatal("canceled initialization left a registered watch")
	}
	lock.Unlock()
	if err := n.watch(watchedSession, 1); err != nil {
		t.Fatal(err)
	}
	n.unwatch(watchedSession)
}

type historyFrame struct {
	Event  string `json:"event"`
	Params struct {
		Items   []proto.Item `json:"items"`
		From    int          `json:"from_seq"`
		To      int          `json:"to_seq"`
		History string       `json:"history"`
	} `json:"params"`
}
type historyCancelWriter struct {
	batches    []historyFrame
	afterWrite func()
}

func (w *historyCancelWriter) Write(b []byte) (int, error) {
	var frame historyFrame
	if err := json.Unmarshal(b, &frame); err != nil {
		return 0, err
	}
	w.batches = append(w.batches, frame)
	if w.afterWrite != nil {
		w.afterWrite()
	}
	return len(b), nil
}

// The file cursor is already at the end when page one is canceled. Retain the delivered frontier and retry the
// remaining pages without any new file growth; a successful page counts even when it triggers cancellation.
func TestCanceledHistoryDeliveryResumesWithoutLosingItems(t *testing.T) {
	n, f, path := historyCancelFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	writer := &historyCancelWriter{afterWrite: cancel}
	n.peer = rpc.New("n", writer)
	w := &watch{ctx: context.Background(), id: watchedSession, f: f}
	n.watches[watchedSession] = w
	writeCodexToolLines(t, path, 1, 700)
	if err := n.refreshContext(ctx, watchedSession, f); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	if len(writer.batches) != 1 || !w.retry.Load() {
		t.Fatalf("batches=%d retry=%v", len(writer.batches), w.retry.Load())
	}
	if w.pending.From != 251 {
		t.Fatalf("delivered frontier=%d, want 251", w.pending.From)
	}
	if f.Changed() {
		t.Fatal("fixture must have no new lines for retry")
	}
	writer.afterWrite = nil
	if err := n.refreshContext(context.Background(), watchedSession, f); err != nil {
		t.Fatal(err)
	}
	if w.retry.Load() {
		t.Fatal("completed delivery left pending work")
	}
	count, covered := 0, 1
	for _, b := range writer.batches {
		if b.Params.From != covered || b.Params.History == "" {
			t.Fatalf("noncontiguous span %+v after %d", b.Params, covered)
		}
		for _, item := range b.Params.Items {
			if want := 2 + count/2; item.Seq != want {
				t.Fatalf("item %d seq=%d want=%d", count, item.Seq, want)
			}
			count++
		}
		covered = b.Params.To
	}
	if count != 1400 || covered != 701 {
		t.Fatalf("delivered %d items through %d", count, covered)
	}
}

func TestCanceledHistoryDeliveryResetsOnRewrite(t *testing.T) {
	n, f, path := historyCancelFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	writer := &historyCancelWriter{afterWrite: cancel}
	n.peer = rpc.New("n", writer)
	w := &watch{ctx: context.Background(), id: watchedSession, f: f}
	n.watches[watchedSession] = w
	writeCodexToolLines(t, path, 1, 700)
	_ = n.refreshContext(ctx, watchedSession, f)
	oldHistory := w.pending.History
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	writeCodexToolLines(t, path, 800, 2)
	writer.afterWrite = nil
	if err := n.refreshContext(context.Background(), watchedSession, f); err != nil {
		t.Fatal(err)
	}
	last := writer.batches[len(writer.batches)-1].Params
	if last.History == oldHistory || last.From != 0 || last.To != 2 || len(last.Items) != 4 {
		t.Fatalf("rewrite skipped new history: %+v", last)
	}
}

// The retry flag must wake a watch whose file cursor already consumed all new lines. Without it a canceled
// request's remaining items stay hidden forever until the agent happens to append another line.
func TestWatchPumpRetriesCanceledDeliveryWithoutFileGrowth(t *testing.T) {
	n, f, path := historyCancelFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	writer := &historyCancelWriter{afterWrite: cancel}
	n.peer = rpc.New("n", writer)
	w := &watch{ctx: context.Background(), id: watchedSession, f: f}
	w.armed.Store(true)
	n.watches[watchedSession] = w
	writeCodexToolLines(t, path, 1, 700)
	_ = n.refreshContext(ctx, watchedSession, f)
	writer.afterWrite = nil
	n.pumpWatch(w, 1000)
	waitFor(t, "pending history to finish without file growth", func() bool { return !w.reading.Load() && !w.retry.Load() })
	count := 0
	for _, b := range writer.batches {
		count += len(b.Params.Items)
	}
	if count != 1400 {
		t.Fatalf("delivered %d items, want 1400", count)
	}
}

type blockedHistoryWriter struct {
	entered chan struct{}
	release chan struct{}
	once    bool
}

func (w *blockedHistoryWriter) Write(b []byte) (int, error) {
	if !w.once {
		w.once = true
		close(w.entered)
		<-w.release
	}
	return len(b), nil
}

func TestCanceledLiveDeltaDoesNotAdvanceShown(t *testing.T) {
	n, _, _ := historyCancelFixture(t)
	writer := &blockedHistoryWriter{entered: make(chan struct{}), release: make(chan struct{})}
	n.peer = rpc.New("n", writer)
	sent := make(chan error, 1)
	go func() { sent <- n.peer.Notify("busy", nil) }()
	<-writer.entered
	n.live.show(watchedSession, "the hub still shows this")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	observed := &historyWaitContext{Context: ctx, waiting: make(chan struct{})}
	go func() { n.showLiveContext(observed, watchedSession); close(done) }()
	<-observed.waiting
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("live delta waited behind blocked history after cancellation")
	}
	n.live.mu.Lock()
	r := n.live.m[watchedSession]
	shown := r.shown
	n.live.mu.Unlock()
	if shown != "the hub still shows this" {
		t.Fatalf("unsent delta changed shown to %q", shown)
	}
	close(writer.release)
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	n.showLive(watchedSession)
	n.live.disarm(watchedSession)
	n.live.mu.Lock()
	r = n.live.m[watchedSession]
	n.live.mu.Unlock()
	if r != nil && r.shown != "" {
		t.Fatal("later live retry failed to clear the hub text")
	}
}
