package kb

import (
	"context"
	"errors"
	"time"
)

// A call that runs out its own time is not an unusable answer: the model may just be slow. Such a recording waits
// before it is tried again (doubling up to minutesBackoffMax), so it doesn't come first in every round ahead of the
// others, and is failed only after minutesTimeoutTries timeouts for one revision, so a stuck one still can't hold the
// queue for ever.
const (
	minutesTimeoutTries = 5
	minutesBackoff      = 5 * time.Minute
	minutesBackoffMax   = 2 * time.Hour
)

// minutesNow is the clock of the timeouts' backoff (tests move it).
var minutesNow = time.Now

// callTimedOut: err is a call (or the job) running out its own time while the round still had time.
func callTimedOut(ctx context.Context, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil
}

// minutesWaiting: r timed out for its current revision and is not to be tried again before MinutesRetryAt.
func (r *Recording) minutesWaiting(now time.Time) bool {
	return r.MinutesFailRev == r.Revision && r.MinutesRetryAt != nil && now.Before(*r.MinutesRetryAt)
}

// resetMinutesTries forgets the unusable answers, timeouts and backoff counted so far.
func (r *Recording) resetMinutesTries() {
	r.MinutesFails, r.MinutesTimeouts, r.MinutesFailRev, r.MinutesRetryAt = 0, 0, 0, nil
}

// minutesTimedOut counts a timeout for revision rev of recording id and sets when it may be tried again.
func (e *Engine) minutesTimedOut(id string, rev int64) {
	st := e.Store
	st.recMu.Lock()
	defer st.recMu.Unlock()
	r, ok := st.Recording(id)
	if !ok || r.Revision != rev {
		return
	}
	if r.MinutesFailRev != rev {
		r.resetMinutesTries()
		r.MinutesFailRev = rev
	}
	r.MinutesTimeouts++
	wait := minutesBackoffMax
	if n := r.MinutesTimeouts - 1; n < 8 {
		wait = min(minutesBackoff<<n, minutesBackoffMax)
	}
	at := minutesNow().UTC().Add(wait)
	r.MinutesRetryAt = &at
	r.planMinutes()
	_ = st.putEntity(KindRecording, r.ID, r, &r.Tagging, false)
}
