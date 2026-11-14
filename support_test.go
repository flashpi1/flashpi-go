package flashpi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testKey     = "fp_test_abc123"
	testBaseURL = "https://api.test"
)

// recorded is a request the mock transport received.
type recorded struct {
	Method string
	URL    string
	Header http.Header
	Body   []byte
}

// handler answers one request of the mock transport.
type handler func(*http.Request) (*http.Response, error)

// mockTransport answers the requests in order with its handlers and records them.
type mockTransport struct {
	t        testing.TB
	mu       sync.Mutex
	handlers []handler
	requests []recorded
}

func (m *mockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
		_ = r.Body.Close()
	}
	m.mu.Lock()
	index := len(m.requests)
	m.requests = append(m.requests, recorded{Method: r.Method, URL: r.URL.String(), Header: r.Header.Clone(), Body: body})
	m.mu.Unlock()
	if index >= len(m.handlers) {
		m.t.Errorf("unexpected request %d: %s %s", index+1, r.Method, r.URL)
		return nil, errors.New("no handler for this request")
	}
	response, err := m.handlers[index](r)
	if response != nil {
		response.Request = r
	}
	return response, err
}

func (m *mockTransport) all() []recorded {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]recorded(nil), m.requests...)
}

func (m *mockTransport) only(t testing.TB) recorded {
	t.Helper()
	requests := m.all()
	if len(requests) != 1 {
		t.Fatalf("got %d requests, want 1", len(requests))
	}
	return requests[0]
}

// respond answers with a status, a body and headers given as name, value pairs. A body gets a
// JSON content type unless the headers say otherwise.
func respond(status int, body string, headers ...string) handler {
	return func(r *http.Request) (*http.Response, error) {
		header := http.Header{}
		for i := 0; i+1 < len(headers); i += 2 {
			header.Add(headers[i], headers[i+1])
		}
		if body != "" && header.Get("Content-Type") == "" {
			header.Set("Content-Type", "application/json")
		}
		return &http.Response{
			StatusCode:    status,
			Status:        fmt.Sprintf("%d %s", status, http.StatusText(status)),
			Header:        header,
			Body:          io.NopCloser(strings.NewReader(body)),
			ContentLength: int64(len(body)),
			ProtoMajor:    1,
			ProtoMinor:    1,
		}, nil
	}
}

// problem answers with a Problem Details body.
func problem(status int, code string, headers ...string) handler {
	body := fmt.Sprintf(`{"type":"https://flashpi.com.ec/docs/en/errors/%[1]s","title":"Title","status":%[2]d,"detail":"Detail of %[1]s","instance":"/v1/x","code":%[1]q,"errors":[],"requestId":"req_problem"}`, code, status)
	return respond(status, body, append([]string{"Content-Type", "application/problem+json"}, headers...)...)
}

// failNetwork fails like a connection reset.
func failNetwork(message string) handler {
	return func(*http.Request) (*http.Response, error) { return nil, errors.New(message) }
}

// fakeClock stands for the clock and the sleeps of a client: a sleep moves the clock.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	c.mu.Unlock()
	return ctx.Err()
}

func (c *fakeClock) Sleeps() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.sleeps...)
}

// testClient is a client whose requests the handlers answer, with a fake clock, no jitter and
// Idempotency-Keys key-1, key-2, ...
func testClient(t testing.TB, handlers ...handler) (*Client, *mockTransport, *fakeClock) {
	t.Helper()
	return testClientWith(t, nil, handlers...)
}

func testClientWith(t testing.TB, options []ClientOption, handlers ...handler) (*Client, *mockTransport, *fakeClock) {
	t.Helper()
	transport := &mockTransport{t: t, handlers: handlers}
	all := append([]ClientOption{
		WithAPIKey(testKey),
		WithBaseURL(testBaseURL),
		WithHTTPClient(&http.Client{Transport: transport}),
	}, options...)
	client, err := NewClient(all...)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	clock := &fakeClock{now: time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)}
	client.engine.now = clock.Now
	client.engine.sleep = clock.Sleep
	client.engine.random = func() float64 { return 0 }
	var mu sync.Mutex
	generated := 0
	client.engine.newKey = func() string {
		mu.Lock()
		defer mu.Unlock()
		generated++
		return fmt.Sprintf("key-%d", generated)
	}
	return client, transport, clock
}

// documentJSON is a document as the API answers it.
func documentJSON(id string) string {
	return fmt.Sprintf(`{"id":%q,"type":"invoice","environment":"test","status":"authorized","accessKey":"1311202601179001691900110010010000000011234567813"}`, id)
}

func ptr[T any](v T) *T { return &v }

// mustAs fails the test unless err is or wraps a T, and returns it.
func mustAs[T error](t testing.TB, err error) T {
	t.Helper()
	var target T
	if !errors.As(err, &target) {
		t.Fatalf("got error %T (%v), want a %T", err, err, target)
	}
	return target
}
