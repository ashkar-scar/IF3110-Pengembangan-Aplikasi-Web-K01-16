package observability_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/observability"
	"github.com/ashkar-scar/IF3110-Pengembangan-Aplikasi-Web-K01-16/internal/testutil"
)

func TestEmptyQueueExportsZeroForEveryStatus(t *testing.T) {
	metrics := observability.NewMetrics(testutil.Platform(t))
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	if response.Code != 200 {
		t.Fatalf("metrics: %d %s", response.Code, response.Body.String())
	}
	for _, status := range []string{"PENDING", "RUNNING", "SUCCESS", "FAILED", "PARTIAL", "PARTIALLY_REVERTED", "REVERTED", "UNKNOWN"} {
		expected := `ruangdb_jobs{action_type="UP",execution_mode="NORMAL",status="` + status + `"} 0`
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("missing zero-valued status: %s", status)
		}
	}
}
