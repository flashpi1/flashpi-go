package flashpi

import (
	"net/http"
	"time"
)

// ClientOption configures a Client: pass it to NewClient.
type ClientOption interface {
	applyToClient(*clientSettings)
}

// RequestOption configures one call: pass it to any method, after its other arguments.
type RequestOption interface {
	applyToRequest(*requestSettings)
}

// Option configures a Client or one call: WithTimeout, WithMaxRetries, WithLanguage and
// WithHeader. On a call it overrides the client's setting.
type Option interface {
	ClientOption
	RequestOption
}

// Language is the language of the API's messages. Error codes are the same in every language.
type Language string

// The languages of the API's messages.
const (
	LanguageEnglish Language = "en"
	LanguageSpanish Language = "es"
)

// settings both a client and a call may have.
type commonSettings struct {
	timeout    *time.Duration
	maxRetries *int
	language   *Language
	headers    []header
}

type header struct {
	name  string
	value string
}

type clientSettings struct {
	commonSettings
	apiKey     *string
	baseURL    *string
	httpClient *http.Client
}

type requestSettings struct {
	commonSettings
	idempotencyKey *string
	correlationID  *string
	response       *Response
}

type clientOption func(*clientSettings)

func (o clientOption) applyToClient(s *clientSettings) { o(s) }

type requestOption func(*requestSettings)

func (o requestOption) applyToRequest(s *requestSettings) { o(s) }

// option is a setting of both levels.
type option func(*commonSettings)

func (o option) applyToClient(s *clientSettings)   { o(&s.commonSettings) }
func (o option) applyToRequest(s *requestSettings) { o(&s.commonSettings) }

// WithAPIKey sets the API key (fp_test_... or fp_live_...). Without it, NewClient reads the
// FLASHPI_API_KEY environment variable.
func WithAPIKey(key string) ClientOption {
	return clientOption(func(s *clientSettings) { s.apiKey = &key })
}

// WithBaseURL sets the API's address (default https://api.flashpi.com.ec).
func WithBaseURL(url string) ClientOption {
	return clientOption(func(s *clientSettings) { s.baseURL = &url })
}

// WithHTTPClient sends the requests with your own *http.Client (a proxy, a test transport).
// The SDK never follows redirects: it uses a copy of it whose CheckRedirect stops at the first
// answer. Its Timeout still applies on top of the SDK's.
func WithHTTPClient(client *http.Client) ClientOption {
	return clientOption(func(s *clientSettings) { s.httpClient = client })
}

// WithTimeout sets how long each attempt of a request may take, from sending it to reading the
// whole answer (default 60 seconds). Issuing with options.waitForAuthorization raises it to the
// wait plus 15 seconds. The context of the call bounds the whole call, retries included.
func WithTimeout(timeout time.Duration) Option {
	return option(func(s *commonSettings) { s.timeout = &timeout })
}

// WithMaxRetries sets how many times a request is retried (default 2; 0 turns retries off).
// Retried: network errors, timeouts, 408, 429 and 5xx, except a 501, a 503
// demo_issuer_unavailable and answers that ask to wait (Retry-After) more than 60 seconds, and a
// 409 idempotency_key_in_progress. POSTs keep their Idempotency-Key, so a retry never issues
// twice.
func WithMaxRetries(retries int) Option {
	return option(func(s *commonSettings) { s.maxRetries = &retries })
}

// WithLanguage sets the language of the API's messages: LanguageEnglish (the API's default) or
// LanguageSpanish. Error codes do not change. An empty language is not sent.
func WithLanguage(language Language) Option {
	return option(func(s *commonSettings) { s.language = &language })
}

// WithHeader sends a header with every request of the client, or with one call. It never
// replaces the API key. On a client it cannot be Idempotency-Key, which is per operation: use
// WithIdempotencyKey on the call.
func WithHeader(name, value string) Option {
	return option(func(s *commonSettings) { s.headers = append(s.headers, header{name, value}) })
}

// WithIdempotencyKey sets the Idempotency-Key of a POST: 1 to 255 visible ASCII characters
// (no spaces), unique per operation, such as your order id or a UUID. The SDK creates one when
// it is missing and reuses it on its retries, so a retry never issues a document twice. Other
// methods ignore it.
func WithIdempotencyKey(key string) RequestOption {
	return requestOption(func(s *requestSettings) { s.idempotencyKey = &key })
}

// WithCorrelationID sends your own id for the request (X-Correlation-Id, 1 to 128 of
// A-Za-z0-9._:/+=-), which Flashpi echoes back and logs.
func WithCorrelationID(id string) RequestOption {
	return requestOption(func(s *requestSettings) { s.correlationID = &id })
}

// WithResponseInto fills response with the HTTP answer of a successful call: its status, its
// headers and Flashpi's request id. On a list, it holds the answer of the last page read.
func WithResponseInto(response *Response) RequestOption {
	return requestOption(func(s *requestSettings) { s.response = response })
}

// Response is the HTTP answer of a call, for WithResponseInto. Its body was already read into
// what the method returns.
type Response struct {
	// StatusCode is the HTTP status.
	StatusCode int

	// Header holds the headers of the answer.
	Header http.Header

	// RequestID is Flashpi's id of the request (x-request-id): share it with support.
	RequestID string
}
