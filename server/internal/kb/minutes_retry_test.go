package kb

import (
	"context"
	"strings"
	"testing"
	"time"
)

// fakeMinutesClock sets the backoff's clock to start until the test ends; advance moves it.
func fakeMinutesClock(t *testing.T, start time.Time) (advance func(time.Duration)) {
	t.Helper()
	now := start
	minutesNow = func() time.Time { return now }
	t.Cleanup(func() { minutesNow = time.Now })
	return func(d time.Duration) { now = now.Add(d) }
}

const minutesOneDecision = `{"title":"t","decisions":[{"text":"定了","line":1}]}`

// A slow model's timeouts wait, longer each time, and don't fail the minutes: the next answer in time makes them.
func TestMinutesTimeoutBacksOff(t *testing.T) {
	model := &funcModel{f: func(n int, _, _ string) (string, error) {
		if n <= 2 {
			return "", context.DeadlineExceeded
		}
		return minutesOneDecision, nil
	}}
	e := goldenEngine(t, model)
	now := time.Now()
	advance := fakeMinutesClock(t, now)
	putRecording(t, e, recID, meetingUpload(1, now.Add(-time.Hour), "第一句"), now)

	e.WriteMinutes(context.Background(), 2)
	r := recordingNow(t, e, recID)
	if r.MinutesState != MinutesPending || r.MinutesTimeouts != 1 || r.MinutesFails != 0 || r.MinutesRetryAt == nil ||
		!r.MinutesRetryAt.Equal(now.Add(minutesBackoff)) {
		t.Fatalf("first timeout: %q timeouts %d fails %d retry %v", r.MinutesState, r.MinutesTimeouts, r.MinutesFails, r.MinutesRetryAt)
	}
	advance(minutesBackoff - time.Second)
	if e.WriteMinutes(context.Background(), 2); len(model.calls) != 1 {
		t.Fatal("tried again before the backoff ran out")
	}
	advance(time.Second)
	e.WriteMinutes(context.Background(), 2)
	r = recordingNow(t, e, recID)
	if len(model.calls) != 2 || r.MinutesTimeouts != 2 || !r.MinutesRetryAt.Equal(now.Add(3*minutesBackoff)) {
		t.Fatalf("second timeout: %d calls, timeouts %d retry %v", len(model.calls), r.MinutesTimeouts, r.MinutesRetryAt)
	}
	advance(2 * minutesBackoff)
	if n := e.WriteMinutes(context.Background(), 2); n != 1 {
		t.Fatalf("wrote %d", n)
	}
	r = recordingNow(t, e, recID)
	if r.MinutesState != MinutesReady || r.MinutesTimeouts != 0 || r.MinutesRetryAt != nil || r.MinutesFailRev != 0 {
		t.Fatalf("written: %q timeouts %d retry %v", r.MinutesState, r.MinutesTimeouts, r.MinutesRetryAt)
	}
}

// A recording that timed out doesn't hold up the others: the round goes on past it, and while it waits it takes no
// turn of the round's max.
func TestMinutesTimeoutDoesNotBlockOthers(t *testing.T) {
	const (
		slow  = "8c1f0e2a-4b7d-4e5f-9a6b-3c2d1e0f9a8b"
		other = "5a6b7c8d-9e0f-4a1b-8c2d-3e4f5a6b7c8d"
		third = "2b3c4d5e-6f70-4812-9a3b-4c5d6e7f8091"
	)
	model := &funcModel{f: func(_ int, _, user string) (string, error) {
		if strings.Contains(user, "慢的会") {
			return "", context.DeadlineExceeded
		}
		return minutesOneDecision, nil
	}}
	e := goldenEngine(t, model)
	now := time.Now()
	fakeMinutesClock(t, now)
	putRecording(t, e, slow, meetingUpload(1, now.Add(-time.Hour), "慢的会"), now) // the newest: tried first
	putRecording(t, e, other, meetingUpload(1, now.Add(-2*time.Hour), "别的会"), now)

	if n := e.WriteMinutes(context.Background(), 2); n != 1 {
		t.Fatalf("wrote %d after a timeout", n)
	}
	if r := recordingNow(t, e, other); r.MinutesState != MinutesReady {
		t.Fatalf("other: %q", r.MinutesState)
	}
	if r := recordingNow(t, e, slow); r.MinutesState != MinutesPending || r.MinutesTimeouts != 1 {
		t.Fatalf("slow: %q timeouts %d", r.MinutesState, r.MinutesTimeouts)
	}

	putRecording(t, e, third, meetingUpload(1, now.Add(-3*time.Hour), "第三个会"), now)
	calls := len(model.calls)
	if n := e.WriteMinutes(context.Background(), 1); n != 1 || len(model.calls) != calls+1 {
		t.Fatalf("during the backoff: wrote %d in %d calls", n, len(model.calls)-calls)
	}
	if r := recordingNow(t, e, third); r.MinutesState != MinutesReady {
		t.Fatalf("third: %q", r.MinutesState)
	}

	// A new revision starts over at once.
	putRecording(t, e, slow, meetingUpload(2, now.Add(-time.Hour), "改过了"), now)
	if n := e.WriteMinutes(context.Background(), 1); n != 1 {
		t.Fatalf("new revision: wrote %d", n)
	}
	if r := recordingNow(t, e, slow); r.MinutesState != MinutesReady || r.MinutesTimeouts != 0 || r.MinutesRetryAt != nil {
		t.Fatalf("new revision: %q timeouts %d retry %v", r.MinutesState, r.MinutesTimeouts, r.MinutesRetryAt)
	}
}
