package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mostlygeek/llama-swap/internal/config"
	"github.com/mostlygeek/llama-swap/internal/process"
)

type upstreamRejectProcess struct {
	*fakeProcess
}

func (p *upstreamRejectProcess) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_, _ = w.Write([]byte(`{"error":"upstream rejected request"}`))
}

// TestBaseRouter_LoadingStatePreservesUpstreamError verifies that once loading
// state has started, a later upstream rejection is still visible to an SSE
// client. Either the real non-2xx status must be preserved, or (if 200 was
// already committed by the loading prelude) the error must be framed in-band
// as SSE and terminate the stream cleanly.
func TestBaseRouter_LoadingStatePreservesUpstreamError(t *testing.T) {
	sendLoading := true
	base := newFakeProcess("a")
	base.autoReady = true
	target := &upstreamRejectProcess{fakeProcess: base}

	conf := config.Config{
		HealthCheckTimeout: 5,
		Models: map[string]config.ModelConfig{
			"a": {SendLoadingState: &sendLoading},
		},
	}
	b := newTestBaseWithConfig(t, conf, map[string]process.Process{"a": target}, &stubPlanner{})

	response := httptest.NewRecorder()
	b.ServeHTTP(response, newStreamRequest("a"))

	if response.Code == http.StatusUnprocessableEntity {
		return
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 422 or an SSE-framed 200 error", response.Code)
	}

	body := response.Body.String()
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("loading committed HTTP 200 but upstream 422 was not terminated as SSE; body=%q", body)
	}

	// The upstream JSON error must not be appended as a bare, non-SSE line.
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "upstream rejected request") && !strings.HasPrefix(line, "data: ") {
			t.Fatalf("upstream error was appended outside SSE framing: %q", line)
		}
	}
}
