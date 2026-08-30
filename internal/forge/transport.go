package forge

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

// The bounds every platform call carries (CLI-005, FR-046). They are forgectl's
// own, applied by one transport both clients are given, so the retry policy
// never comes from a client library (R5).
const (
	// RequestTimeout bounds a single request, retries excluded.
	RequestTimeout = 30 * time.Second
	// MaxAttempts is the total number of attempts, so at most two retries
	// follow the first try.
	MaxAttempts = 3
	// BaseBackoff is the first pause between attempts; each subsequent pause
	// doubles it.
	BaseBackoff = 500 * time.Millisecond
)

// Transport retries a request that the platform could not serve yet — a rate
// limit or a server-side failure — a bounded number of times, backing off
// between attempts, and cancels the moment its context does.
//
// A 4xx other than 429 is the platform's considered answer and is never
// retried: retrying a 404 or a 403 only wastes the maintainer's time.
type Transport struct {
	// Base is the round tripper actually performing the request. A nil Base
	// uses http.DefaultTransport.
	Base http.RoundTripper
	// MaxAttempts overrides the default when set, so a test need not wait.
	MaxAttempts int
	// BaseBackoff overrides the default when set.
	BaseBackoff time.Duration
	// Sleep is the pause function, replaced in tests. A nil Sleep uses time.Sleep.
	Sleep func(time.Duration)
}

// NewClient builds the *http.Client both platform clients are given: one
// explicit request timeout, one bounded and backed-off retry policy, and full
// context propagation (CLI-005, R5).
func NewClient() *http.Client {
	return &http.Client{
		Transport: &Transport{},
		Timeout:   RequestTimeout,
	}
}

// RoundTrip performs the request, retrying while the platform says "not now".
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	attempts := t.MaxAttempts
	if attempts <= 0 {
		attempts = MaxAttempts
	}

	var (
		resp *http.Response
		err  error
	)

	for attempt := 1; attempt <= attempts; attempt++ {
		resp, err = t.base().RoundTrip(req)

		if !t.shouldRetry(resp, err) || attempt == attempts {
			break
		}

		// The body of a response being discarded must still be drained, or the
		// connection cannot be reused.
		drain(resp)

		if waitErr := t.wait(req, attempt); waitErr != nil {
			return nil, waitErr
		}
	}

	if err != nil {
		return nil, fmt.Errorf("request to %s: %w", req.URL.Host, err)
	}

	return resp, nil
}

// base returns the round tripper doing the actual work.
func (t *Transport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}

	return http.DefaultTransport
}

// shouldRetry reports whether the platform's answer is worth asking again.
func (t *Transport) shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		// A transport-level failure is worth one more attempt; a cancelled
		// context is caught by wait, which never sleeps through it.
		return true
	}

	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError
}

// wait pauses before the next attempt, doubling the pause each time, and
// returns immediately if the context is cancelled meanwhile.
func (t *Transport) wait(req *http.Request, attempt int) error {
	base := t.BaseBackoff
	if base <= 0 {
		base = BaseBackoff
	}

	pause := time.Duration(math.Pow(2, float64(attempt-1))) * base

	if t.Sleep != nil {
		t.Sleep(pause)

		return nil
	}

	timer := time.NewTimer(pause)
	defer timer.Stop()

	select {
	case <-req.Context().Done():
		return fmt.Errorf("request to %s: %w", req.URL.Host, req.Context().Err())
	case <-timer.C:
		return nil
	}
}

// drain reads and closes a response body being discarded, so its connection can
// be reused by the next attempt.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}
