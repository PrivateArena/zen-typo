//go:build integration

package ui

import (
	"os"
	"runtime"
	"testing"

	"github.com/gotk3/gotk3/gtk"
)

// drainEvents flushes all pending GTK events (equivalent to running the main loop
// until idle). Must be called from the OS-locked thread.
func drainEvents() {
	for gtk.EventsPending() {
		gtk.MainIteration()
	}
}

// TestFocusRetainedAfterCandidateRender is the core regression test for the
// focus-theft bug. Previously, widget.Destroy() on old candidate labels would
// move keyboard focus away from the entry. The fix: pre-allocated slot pool
// means Destroy is never called.
//
// Run with: go test -tags integration ./pkg/ui/ -v -run TestFocus
func TestFocusRetainedAfterCandidateRender(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("requires X11 display (set DISPLAY env var)")
	}

	// GTK must run on the same OS thread it was initialized on
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ctrl, err := Start(func(string) {}, func(string) {}, func() {})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Show the overlay and wait for it to appear
	ctrl.Show()
	drainEvents()

	if !ctrl.entry.HasFocus() {
		t.Fatal("FAIL: entry does not have focus after Show()")
	}

	// Render a first batch of candidates — this is the operation that used to
	// call widget.Destroy() and steal focus
	ctrl.UpdateCandidates([]string{"hello", "world", "foo", "bar", "baz"})
	drainEvents()

	if !ctrl.entry.HasFocus() {
		t.Error("FAIL: entry lost focus after first UpdateCandidates (widget pool not working)")
	}

	// Simulate a second keystroke: render a different set of candidates
	ctrl.UpdateCandidates([]string{"second", "render", "cycle"})
	drainEvents()

	if !ctrl.entry.HasFocus() {
		t.Error("FAIL: entry lost focus after second UpdateCandidates")
	}

	// Simulate repeated rapid renders (fast typing)
	for i := 0; i < 10; i++ {
		ctrl.UpdateCandidates([]string{"rapid", "typing", "test"})
		drainEvents()
	}

	if !ctrl.entry.HasFocus() {
		t.Errorf("FAIL: entry lost focus after 10 rapid renders")
	}

	// After clearing candidates, focus should still be on entry
	ctrl.UpdateCandidates(nil)
	drainEvents()

	if !ctrl.entry.HasFocus() {
		t.Error("FAIL: entry lost focus after clearing candidates")
	}
}

// TestSlotPoolNeverDestroys verifies that candidate slots are created once
// and reused (not destroyed/recreated) across multiple UpdateCandidates calls.
func TestSlotPoolNeverDestroys(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("requires X11 display")
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ctrl, err := Start(func(string) {}, func(string) {}, func() {})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctrl.Show()
	drainEvents()

	// Capture initial slot pointers — these must never change
	initialSlots := ctrl.slots

	ctrl.UpdateCandidates([]string{"a", "b", "c"})
	drainEvents()
	ctrl.UpdateCandidates([]string{"x", "y", "z", "w"})
	drainEvents()
	ctrl.UpdateCandidates(nil)
	drainEvents()

	for i, slot := range ctrl.slots {
		if slot != initialSlots[i] {
			t.Errorf("slot[%d] pointer changed — widget was destroyed and recreated", i)
		}
	}
}

// TestMaxSlotsNotExceeded verifies that more than maxSlots candidates are
// silently capped and do not panic.
func TestMaxSlotsNotExceeded(t *testing.T) {
	if os.Getenv("DISPLAY") == "" {
		t.Skip("requires X11 display")
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ctrl, err := Start(func(string) {}, func(string) {}, func() {})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctrl.Show()
	drainEvents()

	// Pass more candidates than slots — must not panic
	overload := make([]string, maxSlots+5)
	for i := range overload {
		overload[i] = "word"
	}
	ctrl.UpdateCandidates(overload)
	drainEvents()
}
