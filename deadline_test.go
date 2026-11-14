package flashpi

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// slowServer answers after the handler's own delays, on a real connection.
func slowServer(t *testing.T, handle http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handle)
	t.Cleanup(server.Close)
	return server
}

func serverClient(t *testing.T, server *httptest.Server, options ...ClientOption) *Client {
	t.Helper()
	all := append([]ClientOption{WithAPIKey(testKey), WithBaseURL(server.URL), WithMaxRetries(0)}, options...)
	client, err := NewClient(all...)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestTheDeadlineCoversAnAnswerWhoseHeadersAreLate(t *testing.T) {
	release := make(chan struct{})
	server := slowServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	defer close(release)
	client := serverClient(t, server, WithTimeout(150*time.Millisecond))
	started := time.Now()
	_, err := client.Status.Get(context.Background())
	elapsed := time.Since(started)
	timeout := mustAs[*TimeoutError](t, err)
	if !timeout.Timeout() || !strings.Contains(timeout.Message, "timed out after 0.15 seconds") {
		t.Errorf("got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("the attempt took %v", elapsed)
	}
	mustAs[*ConnectionError](t, err)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a timeout does not wrap context.DeadlineExceeded: %v", err)
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Error("a timeout is not a net.Error timeout")
	}
}

func TestTheDeadlineCoversABodyThatTrickles(t *testing.T) {
	server := slowServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`{"status":`))
		flusher.Flush()
		for i := 0; i < 50; i++ {
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
			_, _ = w.Write([]byte(" "))
			flusher.Flush()
		}
		_, _ = w.Write([]byte(`"operational"}`))
	})
	client := serverClient(t, server, WithTimeout(200*time.Millisecond))
	started := time.Now()
	_, err := client.Status.Get(context.Background())
	mustAs[*TimeoutError](t, err)
	if elapsed := time.Since(started); elapsed > 900*time.Millisecond {
		t.Errorf("a trickling body kept the attempt for %v", elapsed)
	}
}

func TestATimeoutIsRetried(t *testing.T) {
	var calls atomic.Int32
	server := slowServer(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"operational"}`))
	})
	client := serverClient(t, server, WithTimeout(100*time.Millisecond), WithMaxRetries(1))
	client.engine.sleep = func(context.Context, time.Duration) error { return nil }
	status, err := client.Status.Get(context.Background())
	if err != nil || status.Status != StatusStatusOperational || calls.Load() != 2 {
		t.Errorf("got %v %v after %d calls", status, err, calls.Load())
	}
}

func TestTheCallersDeadlineEndsTheCall(t *testing.T) {
	server := slowServer(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	client := serverClient(t, server, WithMaxRetries(5))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := client.Status.Get(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	var timeout *TimeoutError
	if errors.As(err, &timeout) {
		t.Error("the caller's deadline is reported as an attempt's timeout")
	}
	mustAs[*Error](t, err)
}

func TestAHTTPClientTimeoutIsATimeout(t *testing.T) {
	server := slowServer(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	client := serverClient(t, server, WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}))
	_, err := client.Status.Get(context.Background())
	timeout := mustAs[*TimeoutError](t, err)
	// It names the timeout that fired, not the SDK's 60 seconds.
	if strings.Contains(timeout.Message, "60 seconds") || !strings.Contains(timeout.Message, "timed out: ") {
		t.Errorf("message = %q", timeout.Message)
	}
}

func TestARealServerAnswer(t *testing.T) {
	server := slowServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testKey || r.URL.Path != "/v1/documents/a%2Fb" && r.URL.RawPath != "/v1/documents/a%2Fb" {
			w.WriteHeader(400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-real")
		_, _ = w.Write([]byte(documentJSON("a/b")))
	})
	client := serverClient(t, server)
	var response Response
	document, err := client.Documents.Get(context.Background(), "a/b", WithResponseInto(&response))
	if err != nil || document.ID != "a/b" || response.RequestID != "req-real" {
		t.Errorf("got %v %v %+v", document, err, response)
	}
}
