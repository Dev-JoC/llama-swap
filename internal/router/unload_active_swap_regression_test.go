package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mostlygeek/llama-swap/internal/process"
)

// TestBaseRouter_UnloadCancelsActiveSwap verifies the Unload contract when the
// target has a swap in flight but has not started yet. Once Unload returns, an
// older swap goroutine must not be able to start the target afterward.
func TestBaseRouter_UnloadCancelsActiveSwap(t *testing.T) {
	target := newFakeProcess("target")
	target.autoReady = true

	evictee := newFakeProcess("evictee")
	evictee.markReady()
	evictee.stopBlock = make(chan struct{})

	b := newTestBase(t, map[string]process.Process{
		"target":  target,
		"evictee": evictee,
	}, &stubPlanner{evict: map[string][]string{
		"target": {"evictee"},
	}})

	response := httptest.NewRecorder()
	serveDone := make(chan struct{})
	go func() {
		b.ServeHTTP(response, newRequest("target"))
		close(serveDone)
	}()

	// Hold doSwap in its eviction phase. The target is now recorded as an
	// active swap, but EnsureReady(target) has not happened yet.
	waitSignal(t, evictee.stopStarted, "evictee Stop")

	b.Unload(time.Second, "target")
	if got := target.State(); got != process.StateStopped {
		t.Fatalf("target state immediately after Unload = %s, want stopped", got)
	}

	// Let the pre-existing swap continue. A correct Unload must have cancelled
	// or invalidated it, so it must not start target after Unload returned.
	close(evictee.stopBlock)

	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("request was not released by Unload")
	}
	if response.Code == http.StatusOK {
		t.Fatalf("request unexpectedly succeeded after target was unloaded")
	}

	select {
	case <-target.ensureAsked:
		// Current buggy behaviour reaches here: the orphaned doSwap proceeds to
		// EnsureReady after Unload has already returned.
	case <-time.After(250 * time.Millisecond):
	}

	// Give the orphaned swap enough time to start the auto-ready fake if it is
	// still alive. The target must remain stopped throughout.
	time.Sleep(50 * time.Millisecond)
	if got := target.State(); got != process.StateStopped {
		t.Fatalf("target restarted after Unload returned: state=%s", got)
	}
	if got := target.runCalls.Load(); got != 0 {
		t.Fatalf("target started %d time(s) after Unload; want 0", got)
	}
}
