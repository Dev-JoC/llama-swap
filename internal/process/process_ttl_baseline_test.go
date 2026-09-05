package process

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/mostlygeek/llama-swap/internal/config"
)

// TestProcessCommand_TTLStartsAtReadyWithoutRequest verifies that a freshly
// ready process gets a full idle TTL even when it has not served a request yet.
// Without a readiness baseline, lastUse is zero (the Unix epoch), so the TTL
// goroutine unloads the process on its first one-second tick.
func TestProcessCommand_TTLStartsAtReadyWithoutRequest(t *testing.T) {
	skipIfNoSimpleResponder(t)

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(mock.Close)

	cmd, _ := simpleResponderCmd(t, "-silent")
	cfg := config.ModelConfig{
		Cmd:                cmd,
		Proxy:              mock.URL,
		CheckEndpoint:      "/health",
		HealthCheckTimeout: 10,
		UnloadAfter:        2,
		UnloadTimeout:      1,
	}
	if runtime.GOOS == "windows" {
		cfg.CmdStop = "taskkill /f /t /pid ${PID}"
	}

	p := newProcessCommand(t, cfg)
	runErr := runAsync(t, p)
	t.Cleanup(func() {
		if p.State() == StateReady {
			_ = p.Stop(testStopTimeout)
		}
	})

	if got := p.State(); got != StateReady {
		t.Fatalf("expected StateReady, got %s", got)
	}

	// Do not call ServeHTTP here. A newly ready process should remain loaded
	// for its configured TTL even before the first inference request arrives.
	time.Sleep(1200 * time.Millisecond)
	if got := p.State(); got != StateReady {
		t.Fatalf("fresh process unloaded before TTL elapsed; state=%s", got)
	}

	deadline := time.Now().Add(3 * time.Second)
	for p.State() != StateStopped && time.Now().Before(deadline) {
		time.Sleep(testPollInterval)
	}
	if got := p.State(); got != StateStopped {
		t.Fatalf("TTL did not stop idle process; state=%s", got)
	}

	select {
	case err := <-runErr:
		if err != nil {
			t.Errorf("Run() after TTL stop: expected nil, got %v", err)
		}
	case <-time.After(testReturnTimeout):
		t.Fatal("Run() did not return after TTL-induced stop")
	}
}
