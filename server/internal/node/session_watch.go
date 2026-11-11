package node

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/transcript"
)

// watch streams new transcript items of a session to the hub, and a working Claude's status line (claudestatus.go).
// The shared pump (watchPump) reads it; a watch still waiting for its transcript (n.waiting) has only cancel.
type watch struct {
	cancel context.CancelFunc

	ctx context.Context // ends with session.unwatch or the hub connection (watchFor)
	id  string
	f   *transcript.File
	// armed: the watch's first read and catch-up are done, so the pump may read it (the first read of a file is the
	// subscriber's backlog, not news to push).
	armed atomic.Bool
	ticks int // the pump's ticks since the watch was armed; only the pump touches it
	// reading: new lines are being read and pushed in a goroutine of their own; the pump skips the watch meanwhile.
	reading atomic.Bool
	live    liveWatch
	// codex: a Codex thread's, whose transcript may come from the Codex App's database (codex_apphistory.go).
	codex bool

	// pending is the delivery frontier, not the file read cursor. A canceled page request can read new lines
	// without finishing their push; the pump retries them even when the file no longer looks changed.
	// pending is protected by pushLock; retry is also read by the pump.
	pending transcript.Span
	retry   atomic.Bool
}

// watchEvery is how often the pump reads the watched transcripts; every followEvery ticks it also follows the copy
// the session list reads (followCopy), and every liveTicks it checks the status line.
var watchEvery = 250 * time.Millisecond

const followEvery = 4

// maxSubagentWatches bounds the subagent transcripts followed at once (a page open on one follows it, a stat on every
// tick of the watch pump and a read when it grew): sessions are few, but every subagent a person opens would be one
// more file to follow for as long as its page stays.
const maxSubagentWatches = 16

// subagentWatches counts the followed subagent transcripts. Callers hold n.mu.
func (n *Node) subagentWatches() int {
	count := 0
	for id := range n.watches {
		if _, _, sub := splitSubagent(id); sub {
			count++
		}
	}
	return count
}

// watch keeps a session's transcript streaming to the hub. Outside of a hub connection (tests that drive the method
// directly) there is nothing to end it but session.unwatch; Serve's calls go through watchFor, so the stream stops
// when its connection does.
func (n *Node) watch(id string, since int) error {
	return n.watchFor(context.Background(), id, since)
}

// watchFor is watch for the hub connection that owns parent: the transcript loop ends with that connection, so a
// resident node that opens a new Node per reconnect is not left with a reader per watch from the one before.
func (n *Node) watchFor(parent context.Context, id string, since int) error {
	return n.watchRequest(parent, parent, id, since)
}

// Initialization belongs to the request; a successfully armed stream belongs to the connection.
func (n *Node) watchRequest(request, parent context.Context, id string, since int) (err error) {
	if err := request.Err(); err != nil {
		return err
	}
	f, err := n.file(id)
	if err != nil {
		if !n.startingSession(id) {
			return err
		}
		// A session that just started has no transcript until claude writes its first line: watch once it exists,
		// sending everything written before the watch began (the phone's backlog was empty). The wait is kept in
		// n.waiting, so an unwatch meanwhile ends it and another watch does not start a second one (#5389).
		n.mu.Lock()
		if _, ok := n.watches[id]; ok || n.waiting[id] != nil {
			n.mu.Unlock()
			return nil
		}
		wctx, cancel := context.WithCancel(parent)
		pw := &watch{cancel: cancel}
		if n.waiting == nil {
			n.waiting = map[string]*watch{}
		}
		n.waiting[id] = pw
		n.mu.Unlock()
		go func() {
			defer func() {
				n.mu.Lock()
				if n.waiting[id] == pw {
					delete(n.waiting, id)
				}
				n.mu.Unlock()
			}()
			for i := 0; i < 120; i++ {
				select {
				case <-wctx.Done(): // unwatched, or the connection ended, before the transcript appeared
					return
				case <-time.After(500 * time.Millisecond):
				}
				f, err := n.file(id)
				if err != nil {
					continue
				}
				mu := n.pushLock(id)
				if err := mu.LockContext(wctx); err != nil {
					return
				}
				if _, err := f.Refresh(); err == nil {
					since, err = n.catchUp(wctx, id, f, since)
					if err != nil {
						mu.Unlock()
						cancel()
						return
					}
				}
				mu.Unlock()
				// Under wctx: an unwatch that comes before this goroutine is done still cancels the watch it installs.
				_ = n.watchFor(wctx, id, since)
				return
			}
			cancel()
		}()
		return nil
	}
	_, codex := n.codexSource(id)
	n.mu.Lock()
	if _, ok := n.watches[id]; ok {
		n.mu.Unlock()
		return nil
	}
	if _, _, sub := splitSubagent(id); sub && n.subagentWatches() >= maxSubagentWatches {
		n.mu.Unlock()
		return fmt.Errorf("too many subagent conversations followed (%d): close one first", maxSubagentWatches)
	}
	ctx, cancel := context.WithCancel(parent)
	w := &watch{cancel: cancel, ctx: ctx, id: id, f: f, codex: codex}
	n.watches[id] = w
	if !n.pumping {
		n.pumping = true
		go n.watchPump()
	}
	n.mu.Unlock()
	context.AfterFunc(ctx, func() { n.watchEnded(w) })

	defer func() {
		if err != nil {
			cancel()
			n.watchEnded(w) // do not remove a newer watch with the same id
		}
	}()
	// Unwatch/connection closure must also interrupt initialization, but ending the request after success must
	// not cancel the stream (#3139).
	initCtx, initCancel := context.WithCancel(request)
	stop := context.AfterFunc(ctx, initCancel)
	if ctx.Err() != nil {
		initCancel()
	}
	defer func() { stop(); initCancel() }()
	mu := n.pushLock(id)
	if err = mu.LockContext(initCtx); err != nil {
		return err
	}
	defer mu.Unlock()
	readBefore := f.LastSeq() > 0
	if _, span, refreshErr := f.RefreshSpan(); refreshErr == nil {
		if since > 0 {
			_, err = n.catchUp(initCtx, id, f, since)
		} else if readBefore && span.To > span.From {
			_, err = n.catchUp(initCtx, id, f, span.From)
		}
	}
	if err == nil {
		err = initCtx.Err()
	}
	if err != nil {
		return err
	}
	n.liveRecheckContext(initCtx, id)
	if err = initCtx.Err(); err != nil {
		return err
	}
	w.armed.Store(true)
	return nil
}

// catchUp returns the last successfully delivered seq. Never advance it for an unsent page: another request or the
// pump must be able to resume after cancellation. After keeps each transcript line whole, including call/result pairs.
func (n *Node) catchUp(ctx context.Context, id string, f *transcript.File, since int) (int, error) {
	for {
		if err := ctx.Err(); err != nil {
			return since, err
		}
		items, more, span := f.After(since, 500)
		if err := ctx.Err(); err != nil {
			return since, err
		}
		items = f.WithImages(items) // legacy subscribers still need inline images
		if err := n.pushItemsContext(ctx, id, items, span); err != nil {
			return since, err
		}
		since = span.To
		if !more {
			return since, nil
		}
	}
}

func (n *Node) pushItems(id string, items []proto.Item, span transcript.Span) {
	_ = n.pushItemsContext(context.Background(), id, items, span)
}

func (n *Node) pushItemsContext(ctx context.Context, id string, items []proto.Item, span transcript.Span) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if n.peer == nil {
		return nil
	}
	n.mu.Lock()
	f := n.files[id]
	n.mu.Unlock()
	items = n.echoed(id, f, items)
	if err := n.peer.NotifyContext(ctx, "session.items", withSpan(map[string]any{"session_id": id, "items": nonNil(items)}, span, true)); err != nil {
		return err
	}
	// Delivery succeeded even if cancellation arrived just afterward; the caller must advance its frontier.
	if ctx.Err() == nil {
		n.liveRecheckContext(ctx, id)
	}
	return nil
}

// sessionPushLock serializes file reads and pushes without trapping canceled requests behind a slow subscriber.
// Existing background writers can still use Lock; requests and the watch pump use LockContext.
type sessionPushLock struct{ held chan struct{} }

func (m *sessionPushLock) Lock()   { _ = m.LockContext(context.Background()) }
func (m *sessionPushLock) Unlock() { <-m.held }
func (m *sessionPushLock) LockContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case m.held <- struct{}{}:
		if err := ctx.Err(); err != nil {
			m.Unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (n *Node) pushLock(id string) *sessionPushLock {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.pushLocks == nil {
		n.pushLocks = map[string]*sessionPushLock{}
	}
	mu := n.pushLocks[id]
	if mu == nil {
		mu = &sessionPushLock{held: make(chan struct{}, 1)}
		n.pushLocks[id] = mu
	}
	return mu
}

func (n *Node) unwatch(id string) {
	n.mu.Lock()
	w, pw := n.watches[id], n.waiting[id]
	delete(n.watches, id)
	delete(n.waiting, id)
	n.mu.Unlock()
	if w != nil {
		w.cancel()
	}
	if pw != nil {
		pw.cancel()
	}
	n.liveUnwatched(id)
}

// refresh parses new transcript lines and pushes them to the hub when the session is watched — also when the new
// lines hold no items (titles, modes): the batch still tells clients they have everything up to its to_seq, so the
// next one does not look like it skipped something.
func (n *Node) refresh(id string, f *transcript.File) error {
	return n.refreshContext(context.Background(), id, f)
}

func (n *Node) refreshContext(ctx context.Context, id string, f *transcript.File) error {
	mu := n.pushLock(id)
	if err := mu.LockContext(ctx); err != nil {
		return err
	}
	defer mu.Unlock()
	_, span, err := f.RefreshSpan()
	if err != nil {
		return err
	}
	n.mu.Lock()
	w := n.watches[id]
	n.mu.Unlock()
	if w != nil {
		if span.To != span.From || w.retry.Load() && w.pending.History != span.History {
			if !w.retry.Load() || w.pending.History != span.History {
				w.pending = span
			}
			w.retry.Store(true)
		}
		if w.retry.Load() {
			// Unwatch ends this delivery too; a replacement watch has its own frontier and connection context.
			delivery, cancel := context.WithCancel(ctx)
			var stop func() bool
			if w.ctx != nil { // direct test watches may have no context
				stop = context.AfterFunc(w.ctx, cancel)
				if w.ctx.Err() != nil {
					cancel()
				}
			}
			last, pushErr := n.catchUp(delivery, id, f, w.pending.From)
			cancel()
			if stop != nil {
				stop()
			}
			w.pending.From = last
			if pushErr != nil {
				return pushErr
			}
			w.retry.Store(false)
		}
	}
	return ctx.Err()
}

// watchPump reads every watched session on one ticker while any is watched, and ends when none is left. It used to be
// two tickers per watch, each in a goroutine of its own (250 ms for the transcript, 1 s for the status line): four
// watched sessions at rest woke the node twenty times a second, about 65 ms of CPU a minute each. A tick only stats
// each transcript (transcript.File.Changed); reading new lines and reading a screen run in goroutines of their own,
// one at a time per session, so a slow parse or a slow herdr never holds up another session.
func (n *Node) watchPump() {
	t := time.NewTicker(watchEvery)
	defer t.Stop()
	live := liveTicks()
	var ws []*watch
	for range t.C {
		n.mu.Lock()
		if len(n.watches) == 0 {
			n.pumping = false
			n.mu.Unlock()
			return
		}
		for _, w := range n.watches {
			ws = append(ws, w)
		}
		n.mu.Unlock()
		for _, w := range ws {
			n.pumpWatch(w, live)
		}
		clear(ws)
		ws = ws[:0]
	}
}

// pumpWatch is one tick of a watch: new transcript lines four times a second. (The watch used to read the pane's
// screen every 400 ms too, a herdr call per watched session, for a live preview no view showed; the terminal pane reads
// the screen itself.) About once a second it follows the copy the session list reads (followCopy): a stream that only
// refreshed the file it opened kept pushing an old copy while the list's rev went on with the new one (#1423).
func (n *Node) pumpWatch(w *watch, live int) {
	if !w.armed.Load() || w.ctx.Err() != nil { // not caught up yet, or ended (watchEnded drops it)
		return
	}
	w.ticks++
	if w.ticks%live == 0 {
		n.pumpLive(w) // claudestatus.go
	}
	if w.reading.Load() { // still reading: this tick is skipped, as a busy ticker's was
		return
	}
	// The path is cached: a lookup and a stat, a search at most every 30 s. A file being read (Changed says true without
	// waiting for it) is followed in the goroutine that reads it, so the pump never waits for another's parse.
	follow := w.ticks%followEvery == 0
	// A Codex thread: if it is an App chat with no rollout (or the App deleted its rollout meanwhile), the App's new turns
	// are written into its transcript in the reading goroutine (codex_apphistory.go), then read as any growth or followed
	// to the new file.
	appChat := follow && w.codex
	if !w.retry.Load() && !w.f.Changed() && !appChat {
		if !follow || !n.followCopy(w.id, w.f) {
			return // at rest
		}
		follow = false // moved: what the new path has is read below
	}
	w.reading.Store(true)
	go func() {
		defer w.reading.Store(false)
		if appChat {
			n.refreshCodexAppHistory(w.id)
		}
		if follow {
			n.followCopy(w.id, w.f)
		}
		_ = n.refreshContext(w.ctx, w.id, w.f)
	}()
}

// watchEnded: the watch's context ended (session.unwatch, or its hub connection closed). Drop the entry so the session
// is not left looking watched: a watch that ended with its connection has nobody to send to, and an unwatch for it
// never comes. Only drop our own: after an unwatch, a new watch may already hold the id, and deleting it would silently
// stop that stream. A status line that was out ends with null (claudestatus.go).
func (n *Node) watchEnded(w *watch) {
	n.mu.Lock()
	if n.watches[w.id] == w {
		delete(n.watches, w.id)
	}
	n.mu.Unlock()
	n.endLive(w)
}
