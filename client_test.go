package flashpi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewClientReadsTheKeyFromTheEnvironment(t *testing.T) {
	t.Setenv("FLASHPI_API_KEY", "fp_live_fromenv")
	client, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	if client.Environment() != EnvironmentLive {
		t.Errorf("Environment() = %q, want live", client.Environment())
	}
	if client.engine.apiKey != "fp_live_fromenv" {
		t.Error("the key of the environment was not used")
	}
}

func TestNewClientPrefersWithAPIKey(t *testing.T) {
	t.Setenv("FLASHPI_API_KEY", "fp_live_fromenv")
	client, err := NewClient(WithAPIKey("fp_test_given"))
	if err != nil {
		t.Fatal(err)
	}
	if client.Environment() != EnvironmentTest || client.engine.apiKey != "fp_test_given" {
		t.Errorf("got %q with key %q", client.Environment(), client.engine.apiKey)
	}
}

func TestNewClientRefusesBadKeys(t *testing.T) {
	cases := []struct {
		name, key, message string
	}{
		{"missing", "", "Missing API key"},
		{"only spaces", " \t\n", "Missing API key"},
		{"webhook secret", "whsec_abc", "webhook signing secret"},
		{"no prefix", "sk_live_abc", "must start with fp_test_ or fp_live_"},
		{"inner space", "fp_test_ab c", "spaces, line breaks"},
		{"inner line break", "fp_test_ab\nc", "spaces, line breaks"},
		{"control character", "fp_test_ab\x00c", "spaces, line breaks"},
		{"outside ASCII", "fp_test_abñ", "spaces, line breaks"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FLASHPI_API_KEY", "fp_test_shouldnotbeused")
			_, err := NewClient(WithAPIKey(tc.key))
			sdkErr := mustAs[*Error](t, err)
			if !strings.Contains(sdkErr.Message, tc.message) {
				t.Errorf("message %q does not say %q", sdkErr.Message, tc.message)
			}
			if key := strings.TrimSpace(tc.key); key != "" && strings.Contains(err.Error(), key) {
				t.Errorf("the error shows the key: %q", err)
			}
		})
	}
}

func TestNewClientWithoutAnyKey(t *testing.T) {
	t.Setenv("FLASHPI_API_KEY", "")
	_, err := NewClient()
	if err == nil || !strings.Contains(err.Error(), "FLASHPI_API_KEY") {
		t.Fatalf("got %v", err)
	}
}

func TestNewClientTrimsTheKeyAsJavaScript(t *testing.T) {
	for _, padding := range []string{" ", "\t\n", "\ufeff", "\u00a0", "\u2003", "\u3000"} {
		client, err := NewClient(WithAPIKey(padding + "fp_test_trimmed" + padding))
		if err != nil {
			t.Fatalf("%q: %v", padding, err)
		}
		if client.engine.apiKey != "fp_test_trimmed" {
			t.Errorf("%q: key %q", padding, client.engine.apiKey)
		}
	}
	// U+0085 is a space for Go but not for JavaScript's trim(): it stays, and is refused.
	if _, err := NewClient(WithAPIKey("fp_test_x\u0085")); err == nil {
		t.Error("a key ending in U+0085 was accepted")
	}
}

func TestNewClientChecksItsSettings(t *testing.T) {
	cases := []struct {
		name    string
		option  ClientOption
		message string
	}{
		{"zero timeout", WithTimeout(0), "timeout must be more than 0"},
		{"negative timeout", WithTimeout(-time.Second), "timeout must be more than 0"},
		{"huge timeout", WithTimeout(maxTimeout + time.Millisecond), "at most 2147483.647 seconds"},
		{"negative retries", WithMaxRetries(-1), "0 or more"},
		{"base URL without scheme", WithBaseURL("api.flashpi.com.ec"), "must be an http:// or https:// URL"},
		{"base URL of another scheme", WithBaseURL("ftp://api.flashpi.com.ec"), "must be an http:// or https:// URL"},
		{"base URL without host", WithBaseURL("https://"), "must be an http:// or https:// URL"},
		{"header with a bad name", WithHeader("Bad Name", "x"), "not a valid header name"},
		{"header with a line break", WithHeader("X-Test", "a\r\nInjected: yes"), "line break"},
		{"header with a NUL", WithHeader("X-Test", "a\x00b"), "control character"},
		{"header that is not UTF-8", WithHeader("X-Test", "a\xffb"), "not UTF-8"},
		{"Content-Length", WithHeader("content-length", "3"), "set by the SDK"},
		{"Transfer-Encoding", WithHeader("Transfer-Encoding", "chunked"), "set by the SDK"},
		{"Idempotency-Key", WithHeader("idempotency-key", "abc"), "unique per operation"},
		{"language with a line break", WithLanguage("es\nX: y"), "line break"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewClient(WithAPIKey(testKey), tc.option)
			sdkErr := mustAs[*Error](t, err)
			if !strings.Contains(sdkErr.Message, tc.message) {
				t.Errorf("message %q does not say %q", sdkErr.Message, tc.message)
			}
		})
	}
}

func TestNewClientChecksTheKeyFirst(t *testing.T) {
	_, err := NewClient(WithAPIKey("bad"), WithTimeout(-1))
	if err == nil || !strings.Contains(err.Error(), "fp_test_ or fp_live_") {
		t.Fatalf("got %v", err)
	}
}

func TestNewClientSkipsNilOptions(t *testing.T) {
	if _, err := NewClient(nil, WithAPIKey(testKey), nil); err != nil {
		t.Fatal(err)
	}
}

func TestClientNeverShowsItsKey(t *testing.T) {
	client, err := NewClient(WithAPIKey("fp_test_secretvalue"))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{
		fmt.Sprint(client), fmt.Sprintf("%v", client), fmt.Sprintf("%+v", client), fmt.Sprintf("%#v", client),
		fmt.Sprintf("%+v", *client), fmt.Sprintf("%#v", *client), fmt.Sprintf("%+v", client.Invoices),
	} {
		if strings.Contains(text, "secretvalue") {
			t.Errorf("the key shows in %q", text)
		}
	}
	if got := client.String(); got != `flashpi.Client{Environment: "test", BaseURL: "https://api.flashpi.com.ec"}` {
		t.Errorf("String() = %s", got)
	}
}

func TestDefaultSettings(t *testing.T) {
	client, err := NewClient(WithAPIKey(testKey))
	if err != nil {
		t.Fatal(err)
	}
	e := client.engine
	if e.baseURL != DefaultBaseURL || e.timeout != DefaultTimeout || e.maxRetries != DefaultMaxRetries || e.language != "" {
		t.Errorf("got %s %v %d %q", e.baseURL, e.timeout, e.maxRetries, e.language)
	}
	if e.http.CheckRedirect == nil {
		t.Error("the default HTTP client follows redirects")
	}
}

func TestTheBaseURLLosesItsTrailingSlash(t *testing.T) {
	client, transport, _ := testClientWith(t, []ClientOption{WithBaseURL("https://api.test/prefix///")}, respond(200, `{"status":"operational"}`))
	if _, err := client.Status.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := transport.only(t).URL; got != "https://api.test/prefix/v1/status" {
		t.Errorf("URL = %s", got)
	}
}

func TestACallersHTTPClientIsCopiedAndNeverFollowsRedirects(t *testing.T) {
	followed := false
	own := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { followed = true; return nil }}
	client, err := NewClient(WithAPIKey(testKey), WithHTTPClient(own))
	if err != nil {
		t.Fatal(err)
	}
	if client.engine.http == own {
		t.Fatal("the caller's client is used as it is")
	}
	if own.CheckRedirect == nil || client.engine.http.CheckRedirect == nil {
		t.Fatal("redirect policies lost")
	}
	_ = own.CheckRedirect(nil, nil)
	if !followed {
		t.Error("the caller's own policy was replaced")
	}
	if err := client.engine.http.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Errorf("the SDK's copy follows redirects: %v", err)
	}
}

func TestARedirectIsAnAPIError(t *testing.T) {
	client, transport, _ := testClient(t, respond(302, "", "Location", "https://elsewhere.test/v1/status"))
	_, err := client.Status.Get(context.Background())
	apiErr := mustAs[*APIError](t, err)
	if apiErr.Status != 302 || len(transport.all()) != 1 {
		t.Errorf("got %v after %d requests", err, len(transport.all()))
	}
}

func TestEnvironmentFollowsTheKey(t *testing.T) {
	for key, want := range map[string]Environment{"fp_test_x": EnvironmentTest, "fp_live_x": EnvironmentLive} {
		client, err := NewClient(WithAPIKey(key))
		if err != nil {
			t.Fatal(err)
		}
		if client.Environment() != want {
			t.Errorf("%s: %s", key, client.Environment())
		}
	}
}
