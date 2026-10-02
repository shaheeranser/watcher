package incident

import (
	"testing"
	"time"
)

var base = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func TestFirstOccurrenceNotifiesNew(t *testing.T) {
	n := NewNotifier(15*time.Minute, 2*time.Minute)

	obs := n.Observe("app.log", "fp1", base)
	if !obs.Notify || obs.Kind != KindNew {
		t.Fatalf("first occurrence = %+v, want a new notification", obs)
	}
	if !obs.Fresh {
		t.Error("the first occurrence must be a fresh cycle")
	}
}

func TestRepeatWithinThrottleIsSuppressed(t *testing.T) {
	n := NewNotifier(15*time.Minute, 2*time.Minute)
	n.Observe("app.log", "fp1", base)

	obs := n.Observe("app.log", "fp1", base.Add(time.Minute))
	if obs.Notify {
		t.Errorf("a repeat inside the throttle window must be suppressed, got %+v", obs)
	}
	if obs.Fresh {
		t.Error("a suppressed repeat is not a fresh cycle")
	}
}

func TestThrottledOngoingReportsTheOccurrence(t *testing.T) {
	n := NewNotifier(15*time.Minute, 2*time.Minute)
	n.Observe("app.log", "fp1", base)

	if obs := n.Observe("app.log", "fp1", base.Add(14*time.Minute)); obs.Notify {
		t.Errorf("inside the window = %+v, want suppressed", obs)
	}
	obs := n.Observe("app.log", "fp1", base.Add(16*time.Minute))
	if !obs.Notify || obs.Kind != KindOngoing {
		t.Errorf("after the window = %+v, want an ongoing notification", obs)
	}
}

func TestResolveAfterQuietWindowHappensOnce(t *testing.T) {
	n := NewNotifier(15*time.Minute, 2*time.Minute)
	n.Observe("app.log", "fp1", base)

	if got := n.Resolve(base.Add(90 * time.Second)); len(got) != 0 {
		t.Fatalf("resolved before the window closed: %+v", got)
	}
	got := n.Resolve(base.Add(2*time.Minute + time.Second))
	if len(got) != 1 || got[0].Kind != KindResolved || got[0].Fingerprint != "fp1" {
		t.Fatalf("resolve = %+v, want exactly one resolved for fp1", got)
	}
	if again := n.Resolve(base.Add(3 * time.Minute)); len(again) != 0 {
		t.Errorf("a second resolve must not repeat: %+v", again)
	}
}

func TestRecurrenceAfterResolveStartsAFreshCycle(t *testing.T) {
	n := NewNotifier(15*time.Minute, 2*time.Minute)
	n.Observe("app.log", "fp1", base)
	n.Resolve(base.Add(3 * time.Minute))

	obs := n.Observe("app.log", "fp1", base.Add(4*time.Minute))
	if !obs.Notify || obs.Kind != KindNew {
		t.Errorf("recurrence after resolve = %+v, want a fresh new notification", obs)
	}
	if !obs.Fresh {
		t.Error("a recurrence after resolve must restart the cycle, so the count resets")
	}
}

func TestUnrelatedIncidentsAreIndependent(t *testing.T) {
	n := NewNotifier(15*time.Minute, 2*time.Minute)
	n.Observe("app.log", "fp1", base)

	obs := n.Observe("app.log", "fp2", base.Add(time.Second))
	if !obs.Notify || obs.Kind != KindNew {
		t.Errorf("a different fingerprint = %+v, want its own new notification", obs)
	}
	if obs.Fresh != true {
		t.Error("a different fingerprint is a fresh cycle")
	}
}

func TestSameFingerprintOnTwoLabelsIsIndependent(t *testing.T) {
	n := NewNotifier(15*time.Minute, 2*time.Minute)
	n.Observe("backend", "fpsame", base)
	n.Observe("frontend", "fpsame", base.Add(10*time.Minute))

	// At one instant the backend's throttle has elapsed since it was last
	// notified, while the frontend's has not. The same fingerprint on two
	// labels must not share a window.
	backend := n.Observe("backend", "fpsame", base.Add(16*time.Minute))
	if !backend.Notify || backend.Kind != KindOngoing {
		t.Errorf("backend = %+v, want its own ongoing notification", backend)
	}
	frontend := n.Observe("frontend", "fpsame", base.Add(16*time.Minute))
	if frontend.Notify {
		t.Errorf("frontend = %+v, want it still throttled by its own window", frontend)
	}
}

func TestExplainedFollowsUpAPendingNotificationOnce(t *testing.T) {
	n := NewNotifier(15*time.Minute, 2*time.Minute)
	if obs := n.Observe("app.log", "fp1", base); !obs.Notify {
		t.Fatal("expected the first occurrence to notify")
	}

	kind, ok := n.Explained("app.log", "fp1", base.Add(time.Second))
	if !ok || kind != KindOngoing {
		t.Fatalf("follow-up = %q/%v, want an ongoing notification", kind, ok)
	}
	if _, again := n.Explained("app.log", "fp1", base.Add(2*time.Second)); again {
		t.Error("the pending explanation is owed only one follow-up")
	}
}

func TestExplainedDoesNothingWithoutAPendingNotification(t *testing.T) {
	n := NewNotifier(15*time.Minute, 2*time.Minute)
	if _, ok := n.Explained("app.log", "fp1", base); ok {
		t.Error("an unknown incident has nothing to follow up")
	}

	// An ongoing notification sent with the explanation already present owes
	// nothing.
	n.Observe("app.log", "fp1", base)
	n.Explained("app.log", "fp1", base.Add(time.Second))
	n.Observe("app.log", "fp1", base.Add(16*time.Minute))
	if _, ok := n.Explained("app.log", "fp1", base.Add(16*time.Minute+time.Second)); ok {
		t.Error("a notification sent with the explanation must not follow up")
	}
}

func TestRestartEmitsNoSpuriousResolve(t *testing.T) {
	first := NewNotifier(15*time.Minute, 2*time.Minute)
	first.Observe("app.log", "fp1", base)
	first.Resolve(base.Add(3 * time.Minute))

	// A fresh process has forgotten the fingerprint entirely, so it never
	// announces a resolution for something it no longer tracks (PROD-STM-7).
	restarted := NewNotifier(15*time.Minute, 2*time.Minute)
	if got := restarted.Resolve(base.Add(time.Hour)); len(got) != 0 {
		t.Errorf("restart resolved %+v, want nothing", got)
	}
}

func TestZeroResolveWindowNeverResolves(t *testing.T) {
	n := NewNotifier(15*time.Minute, 0)
	n.Observe("app.log", "fp1", base)
	if got := n.Resolve(base.Add(time.Hour)); len(got) != 0 {
		t.Errorf("a disabled resolve window resolved %+v", got)
	}
}

func TestTableDrivenLifecycle(t *testing.T) {
	type step struct {
		name     string
		at       time.Duration
		wantKind NotificationKind
		want     bool
		fresh    bool
	}
	steps := []step{
		{"first sight", 0, KindNew, true, true},
		{"repeat inside T", time.Minute, "", false, false},
		{"repeat inside T again", 10 * time.Minute, "", false, false},
		{"repeat after T", 16 * time.Minute, KindOngoing, true, false},
		{"repeat inside T", 20 * time.Minute, "", false, false},
		{"repeat after T", 32 * time.Minute, KindOngoing, true, false},
	}
	n := NewNotifier(15*time.Minute, 2*time.Minute)
	for _, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			obs := n.Observe("app.log", "fp1", base.Add(s.at))
			if obs.Notify != s.want || obs.Kind != s.wantKind || obs.Fresh != s.fresh {
				t.Errorf("at %s = %+v, want notify=%v kind=%q fresh=%v", s.at, obs, s.want, s.wantKind, s.fresh)
			}
		})
	}
}
