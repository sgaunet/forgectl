package forge_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sgaunet/forgectl/internal/forge"
)

// countingServer answers with the given statuses in order, and records how many
// requests it received.
func countingServer(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(calls.Add(1))
		status := statuses[len(statuses)-1]
		if n <= len(statuses) {
			status = statuses[n-1]
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)

	return srv, &calls
}

// noWait is a transport that retries without ever really sleeping, so the test
// exercises the policy rather than the clock.
func noWait(t *testing.T) *forge.Transport {
	t.Helper()

	var slept []time.Duration

	return &forge.Transport{
		BaseBackoff: time.Millisecond,
		Sleep:       func(d time.Duration) { slept = append(slept, d) },
	}
}

func TestRetriesRateLimitAndServerErrors(t *testing.T) {
	tests := []struct {
		name         string
		statuses     []int
		wantCalls    int32
		wantStatus   int
		description  string
		expectRetry  bool
		finalSuccess bool
	}{
		{
			name:         "429 then success",
			statuses:     []int{http.StatusTooManyRequests, http.StatusOK},
			wantCalls:    2,
			wantStatus:   http.StatusOK,
			expectRetry:  true,
			finalSuccess: true,
		},
		{
			name:         "500 then success",
			statuses:     []int{http.StatusInternalServerError, http.StatusOK},
			wantCalls:    2,
			wantStatus:   http.StatusOK,
			expectRetry:  true,
			finalSuccess: true,
		},
		{
			name:       "retries are bounded at 3 attempts",
			statuses:   []int{http.StatusServiceUnavailable},
			wantCalls:  forge.MaxAttempts,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "404 is the platform's answer, not a retryable failure",
			statuses:   []int{http.StatusNotFound},
			wantCalls:  1,
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "403 is never retried",
			statuses:   []int{http.StatusForbidden},
			wantCalls:  1,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "401 is never retried",
			statuses:   []int{http.StatusUnauthorized},
			wantCalls:  1,
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls := countingServer(t, tt.statuses...)
			client := &http.Client{Transport: noWait(t)}

			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}

			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			defer resp.Body.Close()

			if got := calls.Load(); got != tt.wantCalls {
				t.Errorf("requests = %d, want %d", got, tt.wantCalls)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestBackoffIsExponential(t *testing.T) {
	srv, _ := countingServer(t, http.StatusTooManyRequests)

	var slept []time.Duration
	client := &http.Client{Transport: &forge.Transport{
		BaseBackoff: 100 * time.Millisecond,
		Sleep:       func(d time.Duration) { slept = append(slept, d) },
	}}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	// Three attempts means two pauses, each double the one before it.
	if len(slept) != forge.MaxAttempts-1 {
		t.Fatalf("pauses = %v, want %d", slept, forge.MaxAttempts-1)
	}
	if slept[0] != 100*time.Millisecond || slept[1] != 200*time.Millisecond {
		t.Errorf("pauses = %v, want exponential backoff from 100ms", slept)
	}
}

func TestCancellationStopsTheRetryLoop(t *testing.T) {
	srv, calls := countingServer(t, http.StatusTooManyRequests)

	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: &forge.Transport{BaseBackoff: time.Hour}}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	// Cancel while the transport is waiting to retry: it must return at once
	// rather than sleep out the hour (CLI-005).
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	//nolint:bodyclose // the request fails, so there is no body to close
	if _, err := client.Do(req); err == nil {
		t.Fatal("Do succeeded despite cancellation")
	}

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("cancellation took %v; the transport slept through it", elapsed)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("requests = %d, want the loop to stop after the first", got)
	}
}

func TestClientCarriesAnExplicitTimeout(t *testing.T) {
	client := forge.NewClient()

	if client.Timeout != forge.RequestTimeout {
		t.Errorf("timeout = %v, want %v (CLI-005)", client.Timeout, forge.RequestTimeout)
	}
	if _, ok := client.Transport.(*forge.Transport); !ok {
		t.Errorf("transport = %T, want the retrying transport", client.Transport)
	}
}

func TestTimeoutIsEnforced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	client := &http.Client{
		Transport: &forge.Transport{MaxAttempts: 1},
		Timeout:   100 * time.Millisecond,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	//nolint:bodyclose // the request times out, so there is no body to close
	if _, err := client.Do(req); err == nil {
		t.Fatal("a request past its timeout succeeded")
	}
}
