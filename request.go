package flashpi

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// userAgent names the SDK and its version in every request.
const userAgent = "flashpi-go/" + Version

const (
	// maxRetryAfter is the longest Retry-After the SDK waits for by itself; longer waits (daily
	// quotas) are returned.
	maxRetryAfter = 60 * time.Second

	initialRetryDelay = 500 * time.Millisecond
	maxRetryDelay     = 8 * time.Second

	// Waits between checks on an earlier attempt that the API is still running.
	inProgressFirstWait = time.Second
	inProgressMaxWait   = 5 * time.Second

	codeInProgress = "idempotency_key_in_progress"

	jsonAccept = "application/json"
	xmlAccept  = "application/xml, application/problem+json"
	pdfAccept  = "application/pdf, application/problem+json"
)

// engine sends the requests of a client: retries, timeouts, idempotency and typed errors.
type engine struct {
	apiKey      string
	baseURL     string
	timeout     time.Duration
	maxRetries  int
	language    Language
	headers     []header
	http        *http.Client
	environment Environment

	// Replaced by the tests.
	now    func() time.Time
	sleep  func(context.Context, time.Duration) error
	random func() float64
	newKey func() string
}

// operation is one API operation, as the services declare it.
type operation struct {
	method string
	path   string
	query  url.Values

	// content is the body, already serialized: every attempt sends the same bytes. Nil sends
	// no body.
	content     []byte
	contentType string

	accept string

	// minTimeout is a floor for the per-attempt timeout of requests the API holds open
	// (issuing with options.waitForAuthorization).
	minTimeout time.Duration

	// recoverDocument is set on the POSTs that issue a document: when a retry of the call is
	// answered 422 idempotency_key_reused with a documentId, an earlier attempt created the
	// document, so the call returns GET /v1/documents/{documentId} instead of failing.
	recoverDocument bool
}

// call is what every attempt of one call shares, resolved before the first one is sent.
type call struct {
	settings       requestSettings
	maxRetries     int
	timeout        time.Duration
	url            string
	idempotencyKey string
}

// answer is a successful answer, with its body read.
type answer struct {
	status    int
	header    http.Header
	body      []byte
	requestID string
}

// do runs an operation with its retries and recoveries, then reads its answer with read. The
// error of read carries the call's Idempotency-Key, and only an answer read without error fills
// WithResponseInto.
func (e *engine) do(ctx context.Context, op *operation, options []RequestOption, read func(*answer) error) error {
	if ctx == nil {
		return &Error{Message: "flashpi: nil Context: pass context.Background() or the request's context"}
	}
	c, err := e.prepare(op, options)
	if err != nil {
		return err
	}
	result, err := e.run(ctx, op, c)
	if err != nil {
		return withIdempotencyKey(err, c.idempotencyKey)
	}
	if read != nil {
		if err := read(result); err != nil {
			return withIdempotencyKey(err, c.idempotencyKey)
		}
	}
	if c.settings.response != nil {
		*c.settings.response = Response{StatusCode: result.status, Header: result.header, RequestID: result.requestID}
	}
	return nil
}

// prepare checks the options of a call and builds what its attempts share. It fails before
// anything is sent.
func (e *engine) prepare(op *operation, options []RequestOption) (*call, error) {
	var settings requestSettings
	for _, o := range options {
		if o != nil {
			o.applyToRequest(&settings)
		}
	}
	headers, err := checkHeaders("WithHeader", settings.headers)
	if err != nil {
		return nil, err
	}
	settings.headers = headers
	if settings.idempotencyKey != nil {
		key, err := headerValue("The Idempotency-Key", *settings.idempotencyKey)
		if err != nil {
			return nil, err
		}
		settings.idempotencyKey = &key
	}
	if settings.correlationID != nil {
		id, err := headerValue("The correlation id", *settings.correlationID)
		if err != nil {
			return nil, err
		}
		settings.correlationID = &id
	}
	if settings.language != nil {
		language, err := headerValue("The language", string(*settings.language))
		if err != nil {
			return nil, err
		}
		settings.language = (*Language)(&language)
	}
	c := &call{settings: settings, maxRetries: e.maxRetries, timeout: e.timeout}
	if settings.maxRetries != nil {
		if err := validRetries(*settings.maxRetries); err != nil {
			return nil, err
		}
		c.maxRetries = *settings.maxRetries
	}
	if settings.timeout != nil {
		if err := validTimeout(*settings.timeout); err != nil {
			return nil, err
		}
		c.timeout = *settings.timeout
	}
	c.timeout = min(max(c.timeout, op.minTimeout), maxTimeout)
	c.url = e.baseURL + op.path
	if search := op.query.Encode(); search != "" {
		c.url += "?" + search
	}
	if op.method == http.MethodPost {
		c.idempotencyKey = idempotencyKeyOf(settings, e.newKey)
		// Without a key the API runs the POST as it comes, so a retry could issue twice.
		if c.idempotencyKey == "" {
			return nil, &Error{Message: "The Idempotency-Key is empty: pass a key of 1 to 255 visible ASCII characters, or leave it out and the SDK makes one"}
		}
	}
	return c, nil
}

// idempotencyKeyOf is the caller's Idempotency-Key of a POST (the option, or the last
// Idempotency-Key header of the call), or a new one.
func idempotencyKeyOf(settings requestSettings, newKey func() string) string {
	if settings.idempotencyKey != nil {
		return *settings.idempotencyKey
	}
	key := ""
	found := false
	for _, h := range settings.headers {
		if strings.EqualFold(h.name, "Idempotency-Key") {
			key, found = h.value, true
		}
	}
	if found {
		return key
	}
	return newKey()
}

// run sends the attempts of one call, with its retries and recoveries.
func (e *engine) run(ctx context.Context, op *operation, c *call) (*answer, error) {
	started := e.now()
	attempt, retries, waits := 0, 0, 0
	// Whether an earlier attempt may have reached the API without its answer reaching us (a
	// network error, a timeout, or a 5xx from a proxy). Only then can a 409
	// idempotency_key_in_progress be this call's own attempt still running.
	unanswered := false
	for {
		result, failure := e.attempt(ctx, op, c)
		if failure == nil {
			return result, nil
		}
		var apiErr *APIError
		isAPIError := errors.As(failure, &apiErr)
		var connErr *ConnectionError
		isConnection := errors.As(failure, &connErr)
		if !isAPIError && !isConnection {
			// The context ended, or the request could not be built: never retried.
			return nil, failure
		}
		if attempt > 0 && isAPIError {
			if documentID := recoverableDocument(op, apiErr); documentID != "" {
				return e.recoverDocument(ctx, c, documentID)
			}
			// A DELETE retried after a lost answer finds the resource already gone.
			if op.method == http.MethodDelete && apiErr.Status == http.StatusNotFound {
				return deleted(apiErr), nil
			}
			// This call's earlier attempt is still running on the API (a sync issue whose answer
			// was lost): wait for it with the same key while the call has time left. These
			// checks do not count as retries; once the time is up, the normal rules apply.
			if unanswered && apiErr.Status == http.StatusConflict && apiErr.Code == codeInProgress {
				wait := min(inProgressFirstWait*time.Duration(1<<min(waits, 16)), inProgressMaxWait)
				if e.now().Add(wait).Sub(started) <= c.timeout {
					waits++
					attempt++
					if err := e.sleep(ctx, wait); err != nil {
						return nil, e.cancelled(ctx, op, c)
					}
					continue
				}
			}
		}
		if isConnection || (apiErr.Status >= 500 && apiErr.Problem == nil) {
			unanswered = true
		}
		delay, retry := e.retryDelay(failure, retries)
		if !retry || retries >= c.maxRetries {
			return nil, failure
		}
		retries++
		attempt++
		if err := e.sleep(ctx, delay); err != nil {
			return nil, e.cancelled(ctx, op, c)
		}
	}
}

// recoverDocument reads the document an earlier attempt of the call created. A failure keeps
// the call's key on the error, so trying again later with that key returns the document instead
// of issuing another.
func (e *engine) recoverDocument(ctx context.Context, c *call, documentID string) (*answer, error) {
	path, err := pathOf("/v1/documents/{}", documentID)
	if err != nil {
		return nil, err
	}
	read := &operation{method: http.MethodGet, path: path, accept: jsonAccept}
	settings := c.settings
	settings.idempotencyKey = nil
	// The read has the call's options, without the floor of a sync issue.
	timeout := e.timeout
	if settings.timeout != nil {
		timeout = *settings.timeout
	}
	readCall := &call{settings: settings, maxRetries: c.maxRetries, timeout: timeout, url: e.baseURL + path}
	return e.run(ctx, read, readCall)
}

// recoverableDocument is the id of the document an earlier attempt of the call created, if the
// answer shows one.
func recoverableDocument(op *operation, failure *APIError) string {
	if !op.recoverDocument || failure.Status != http.StatusUnprocessableEntity || failure.Code != "idempotency_key_reused" || failure.Problem == nil {
		return ""
	}
	if id := failure.Problem.DocumentID; id != nil {
		return *id
	}
	return ""
}

// deleted is the answer of a DELETE whose resource is gone: a 204 with the 404's headers.
func deleted(failure *APIError) *answer {
	header := failure.Header.Clone()
	for _, name := range []string{"Content-Length", "Content-Type", "Content-Encoding", "Transfer-Encoding"} {
		header.Del(name)
	}
	return &answer{status: http.StatusNoContent, header: header, requestID: failure.RequestID}
}

// retryDelay is how long to wait before retrying, and whether the failure may be retried.
//
// Retried: network errors and timeouts, 408, 429 and 5xx except 501 (with a Retry-After of
// maxRetryAfter or less), and 409 idempotency_key_in_progress (an attempt with the same key is
// still running). POSTs are safe to retry because they keep their Idempotency-Key.
func (e *engine) retryDelay(failure error, retries int) (time.Duration, bool) {
	var apiErr *APIError
	if !errors.As(failure, &apiErr) {
		return e.backoff(retries), true
	}
	status := apiErr.Status
	if status == http.StatusConflict {
		if apiErr.Code == codeInProgress {
			return max(inProgressFirstWait, e.backoff(retries)), true
		}
		return 0, false
	}
	transient := status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || (status >= 500 && status != http.StatusNotImplemented)
	// The shared demo certificate is missing: no retry within seconds will fix that.
	if !transient || apiErr.Code == "demo_issuer_unavailable" {
		return 0, false
	}
	seconds, asked := parseRetryAfter(apiErr.Header.Get("Retry-After"), e.now())
	if !asked {
		return e.backoff(retries), true
	}
	if seconds > int64(maxRetryAfter/time.Second) {
		return 0, false
	}
	return time.Duration(seconds) * time.Second, true
}

// backoff is the exponential backoff from 0.5 s up to 8 s, minus up to 25 % of jitter, in
// whole milliseconds.
func (e *engine) backoff(retries int) time.Duration {
	delay := min(initialRetryDelay*time.Duration(1<<min(retries, 16)), maxRetryDelay)
	jittered := float64(delay) * (1 - e.random()*0.25)
	return time.Duration(math.Round(jittered/float64(time.Millisecond))) * time.Millisecond
}

// attempt sends one attempt within its deadline, from sending it to reading the whole answer.
// The deadline is a context, which net/http honors while it resolves, connects, writes and
// reads the headers and the body.
func (e *engine) attempt(ctx context.Context, op *operation, c *call) (*answer, error) {
	if ctx.Err() != nil {
		return nil, e.cancelled(ctx, op, c)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	var body io.Reader
	if op.content != nil {
		body = bytes.NewReader(op.content)
	}
	request, err := http.NewRequestWithContext(attemptCtx, op.method, c.url, body)
	if err != nil {
		return nil, &Error{Message: fmt.Sprintf("%s %s was not sent: the request is not valid", op.method, c.url)}
	}
	// Built for each attempt and never kept: it carries the API key.
	request.Header = e.headersOf(op, c)
	response, err := e.http.Do(request)
	if err != nil {
		return nil, e.transportError(ctx, attemptCtx, op, c, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, e.transportError(ctx, attemptCtx, op, c, err)
	}
	success := response.StatusCode >= 200 && response.StatusCode <= 299
	// The answer arrived: a body that cannot be decoded is never retried as a lost answer.
	data, err := decodeBody(response, raw)
	if err != nil {
		if success {
			return nil, &Error{Message: fmt.Sprintf("%s %s: the API's answer could not be decoded: %v", op.method, c.url, err), Err: err}
		}
		data = raw // the error status still counts
	}
	if !success {
		return nil, newAPIError(response.StatusCode, response.Header, data)
	}
	return &answer{
		status:    response.StatusCode,
		header:    response.Header,
		body:      data,
		requestID: response.Header.Get("X-Request-Id"),
	}, nil
}

// headersOf are the headers of an attempt, from the lowest priority to the highest: the
// client's language and headers, the call's language and headers, then the SDK's own.
func (e *engine) headersOf(op *operation, c *call) http.Header {
	header := http.Header{}
	header.Set("Accept", op.accept)
	header.Set("User-Agent", userAgent)
	if e.language != "" {
		header.Set("Accept-Language", string(e.language))
	}
	for _, h := range e.headers {
		header.Set(h.name, h.value)
	}
	if language := c.settings.language; language != nil && *language != "" {
		header.Set("Accept-Language", string(*language))
	}
	for _, h := range c.settings.headers {
		header.Set(h.name, h.value)
	}
	if c.settings.correlationID != nil {
		header.Set("X-Correlation-Id", *c.settings.correlationID)
	}
	if c.idempotencyKey != "" {
		header.Set("Idempotency-Key", c.idempotencyKey)
	}
	if op.content != nil {
		header.Set("Content-Type", op.contentType)
	}
	header.Set("Authorization", "Bearer "+e.apiKey)
	return header
}

// transportError is the error of an attempt that got no answer. When the call's own context
// ended, the call ends; otherwise it is a ConnectionError (a TimeoutError when the attempt ran
// out of time), which is retried.
func (e *engine) transportError(ctx, attemptCtx context.Context, op *operation, c *call, err error) error {
	if ctx.Err() != nil {
		return e.cancelled(ctx, op, c)
	}
	reason := err.Error()
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		reason = urlErr.Err.Error()
	}
	if errors.Is(attemptCtx.Err(), context.DeadlineExceeded) {
		seconds := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(c.timeout.Seconds(), 'f', 3, 64), "0"), ".")
		return &TimeoutError{&ConnectionError{
			Message: fmt.Sprintf("%s %s timed out after %s seconds", op.method, c.url, seconds),
			Err:     err,
		}}
	}
	// Another timeout, such as the Timeout of a caller's http.Client.
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return &TimeoutError{&ConnectionError{Message: fmt.Sprintf("%s %s timed out: %s", op.method, c.url, reason), Err: err}}
	}
	return &ConnectionError{Message: fmt.Sprintf("%s %s failed: %s", op.method, c.url, reason), Err: err}
}

// cancelled is the error of a call whose context ended: it is never retried.
func (e *engine) cancelled(ctx context.Context, op *operation, c *call) error {
	cause := ctx.Err()
	return &Error{Message: fmt.Sprintf("%s %s: %v", op.method, c.url, cause), Err: cause}
}

// decodeBody decodes a body the transport left compressed (a caller that sets its own
// Accept-Encoding, or an answer without a body that the transport does not decode).
func decodeBody(response *http.Response, raw []byte) ([]byte, error) {
	encoding := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Encoding")))
	if response.Uncompressed || len(raw) == 0 || encoding == "" || encoding == "identity" {
		return raw, nil
	}
	var reader io.ReadCloser
	var err error
	switch encoding {
	case "gzip", "x-gzip":
		reader, err = gzip.NewReader(bytes.NewReader(raw))
	case "deflate":
		reader, err = zlib.NewReader(bytes.NewReader(raw))
	default:
		return nil, fmt.Errorf("unsupported Content-Encoding %s", encoding)
	}
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// encodeJSON is the body of a request: compact UTF-8 JSON, as JSON.stringify writes it.
func encodeJSON(body any) ([]byte, error) {
	content, err := marshalJSON(body)
	if err != nil {
		return nil, &Error{Message: "The request body cannot be sent as JSON: " + err.Error(), Err: err}
	}
	// encoding/json replaces each byte that is not UTF-8 with the escape \ufffd (and writes a
	// real U+FFFD as it is), and passes a json.RawMessage through: either way the text would
	// reach the SRI altered, so it is refused, as in headers.
	if !utf8.Valid(content) || hasReplacementEscape(content) {
		return nil, &Error{Message: "The request body has text that is not UTF-8"}
	}
	return content, nil
}

// hasReplacementEscape reports whether JSON text holds the escape \ufffd itself, not an
// escaped backslash followed by the letters ufffd.
func hasReplacementEscape(content []byte) bool {
	for index := bytes.Index(content, []byte(`\ufffd`)); index >= 0; {
		backslashes := 0
		for i := index; i >= 0 && content[i] == '\\'; i-- {
			backslashes++
		}
		if backslashes%2 == 1 {
			return true
		}
		next := bytes.Index(content[index+1:], []byte(`\ufffd`))
		if next < 0 {
			return false
		}
		index += 1 + next
	}
	return false
}

// marshalJSON is json.Marshal without HTML escaping (<, > and & stay as they are), as
// JSON.stringify writes it.
func marshalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

// readJSON decodes a JSON answer into v.
func readJSON(a *answer, v any) error {
	if len(bytes.TrimSpace(a.body)) == 0 {
		return &Error{Message: fmt.Sprintf("The API answered %d without a body", a.status)}
	}
	if !json.Valid(a.body) {
		return &Error{Message: fmt.Sprintf("The API answered %d with a body that is not JSON", a.status)}
	}
	if err := json.Unmarshal(a.body, v); err != nil {
		return &Error{Message: fmt.Sprintf("The API answered %d with a body this version of the SDK cannot read: %v", a.status, err), Err: err}
	}
	return nil
}

// newUUID is a random (version 4) UUID.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// jsonOperation is a request whose body, if any, is JSON.
func jsonOperation(method, path string, body any) (*operation, error) {
	op := &operation{method: method, path: path, accept: jsonAccept}
	if body != nil {
		content, err := encodeJSON(body)
		if err != nil {
			return nil, err
		}
		op.content = content
		op.contentType = "application/json"
	}
	return op, nil
}

// fetch runs an operation and decodes its JSON answer into a new T.
func fetch[T any](ctx context.Context, e *engine, op *operation, options []RequestOption) (*T, error) {
	result := new(T)
	err := e.do(ctx, op, options, func(a *answer) error { return readJSON(a, result) })
	if err != nil {
		return nil, err
	}
	return result, nil
}
