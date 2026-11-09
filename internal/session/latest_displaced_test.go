package session

import (
	"testing"
	"time"
)

// TestExclusiveAttachDoesNotKeepTheDisplacedSize: under window_size latest, a
// latest client that leaves keeps the session's size for itself while a
// client still there has had no input since. A client that an exclusive
// attach took off the session did not leave, it was replaced: the session
// takes the attaching client's size at once. That is the desktop attaching
// after a phone, which otherwise restored its floating panes into the
// phone's size and clamped them there.
//
// Negative control: drop the latestDisplaced call. kept stays true.
func TestExclusiveAttachDoesNotKeepTheDisplacedSize(t *testing.T) {
	d := &Daemon{}
	now := time.Now()
	phone := sizedClient{id: "phone", w: 60, h: 30, activity: now.Add(-time.Second), seq: 2}
	desk := sizedClient{id: "desk", w: 160, h: 48, seq: 1}

	// The phone is the latest client, then leaves with input behind it.
	if got, _ := d.pickLatest("s", []sizedClient{desk, phone}, now); got != "phone" {
		t.Fatalf("latest = %q, want phone", got)
	}
	d.attachCount.Store(3)
	d.latestLeft("s", "phone", phone.activity)

	// An ordinary leave keeps the phone's size for the idle desktop.
	if _, kept := d.pickLatest("s", []sizedClient{desk}, now); !kept {
		t.Fatal("an ordinary leave did not keep the size (the policy this test leans on changed)")
	}

	// The same leave caused by an exclusive attach does not.
	d.latestDisplaced("s")
	latest, kept := d.pickLatest("s", []sizedClient{desk}, now)
	if kept || latest != "desk" {
		t.Fatalf("after an exclusive attach latest = %q kept = %v, want desk and not kept", latest, kept)
	}
	if w, h := sizeForPolicy("latest", []sizedClient{desk}, latest); w != 160 || h != 48 {
		t.Fatalf("the session size is %dx%d, want the desktop's 160x48", w, h)
	}
}
