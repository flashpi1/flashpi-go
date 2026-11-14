package flashpi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Error is a failure of the SDK itself: a setting or an argument it refuses before sending
// anything, an answer it cannot read, or a call whose context ended. Every error the SDK returns
// is an *Error or can be read as one with errors.As, *APIError and *ConnectionError included, so
// the Idempotency-Key of any failed POST is in reach:
//
//	var flashpiErr *flashpi.Error
//	if errors.As(err, &flashpiErr) && flashpiErr.IdempotencyKey != "" {
//		// Try again later with flashpi.WithIdempotencyKey(flashpiErr.IdempotencyKey).
//	}
type Error struct {
	// Message says what went wrong, in English.
	Message string

	// IdempotencyKey is the Idempotency-Key a failed POST used (yours, or the one the SDK made).
	// To try the same operation again later without issuing twice, pass it back with
	// WithIdempotencyKey. Empty for other methods and for failures before a request.
	IdempotencyKey string

	// Err is the cause, if any: context.Canceled or context.DeadlineExceeded when the call's
	// context ended, or the error of decoding an answer.
	Err error
}

func (e *Error) Error() string { return e.Message }

// Unwrap returns the cause, so errors.Is(err, context.Canceled) works.
func (e *Error) Unwrap() error { return e.Err }

// asError fills target with an *Error that stands for an error of another type.
func asError(target any, message, idempotencyKey string, cause error) bool {
	pointer, ok := target.(**Error)
	if !ok {
		return false
	}
	*pointer = &Error{Message: message, IdempotencyKey: idempotencyKey, Err: cause}
	return true
}

// APIError is an answer of the API with an error status. Its status has a type of its own
// (BadRequestError, UnprocessableEntityError, ...) that wraps it, so either can be asked for:
//
//	var invalid *flashpi.UnprocessableEntityError
//	if errors.As(err, &invalid) { ... }
//	var apiErr *flashpi.APIError
//	if errors.As(err, &apiErr) { ... } // any status
//
// Branch on Code (stable English slugs such as validation_failed), never on Message, which
// follows the requested language.
type APIError struct {
	// Message is the status, the code and the API's explanation: "422 validation_failed: ...".
	Message string

	// Status is the HTTP status of the answer.
	Status int

	// Code is the error code, or "" when the answer had no Problem Details (for example a 502
	// from a proxy). New codes may appear: handle an unknown one by its status.
	Code ErrorCode

	// Title is a short summary of the problem type.
	Title string

	// Detail says what went wrong in this request.
	Detail string

	// Errors lists the field-level problems of a request body or query (may be empty).
	Errors []FieldError

	// RequestID is Flashpi's id of the request (x-request-id): share it with support.
	RequestID string

	// Problem is the whole Problem Details body, with its extension members (DocumentID,
	// Limit, ResetsAt, ...; Extra for the ones this version does not declare), or nil.
	Problem *ProblemDetails

	// Header holds the headers of the answer.
	Header http.Header

	// IdempotencyKey is the Idempotency-Key of a failed POST: see Error.IdempotencyKey.
	IdempotencyKey string
}

func (e *APIError) Error() string { return e.Message }

// As lets errors.As read an APIError as an *Error.
func (e *APIError) As(target any) bool {
	return asError(target, e.Message, e.IdempotencyKey, nil)
}

// RetryAfter is how long the API asked to wait before trying again (the Retry-After header),
// and whether it asked. Daily quota errors point at the next midnight in Ecuador, so it can be
// hours.
func (e *APIError) RetryAfter() (time.Duration, bool) {
	seconds, ok := parseRetryAfter(e.Header.Get("Retry-After"), time.Now())
	if !ok {
		return 0, false
	}
	if seconds > int64(math.MaxInt64/int64(time.Second)) {
		return time.Duration(math.MaxInt64), true
	}
	return time.Duration(seconds) * time.Second, true
}

// BadRequestError is a 400: the request could not be read (malformed JSON, invalid
// Idempotency-Key, ...).
type BadRequestError struct{ *APIError }

// Unwrap returns the APIError.
func (e *BadRequestError) Unwrap() error { return e.APIError }

// AuthenticationError is a 401: the API key is missing, malformed, unknown or revoked.
type AuthenticationError struct{ *APIError }

// Unwrap returns the APIError.
func (e *AuthenticationError) Unwrap() error { return e.APIError }

// PermissionDeniedError is a 403: the key may not do this (read-only, another issuer, IP
// allowlist, suspended account).
type PermissionDeniedError struct{ *APIError }

// Unwrap returns the APIError.
func (e *PermissionDeniedError) Unwrap() error { return e.APIError }

// NotFoundError is a 404: the resource does not exist in this mode (test or live) for this
// account.
type NotFoundError struct{ *APIError }

// Unwrap returns the APIError.
func (e *NotFoundError) Unwrap() error { return e.APIError }

// ConflictError is a 409: the request conflicts with the current state (sequential used, RIDE
// not ready, ...).
type ConflictError struct{ *APIError }

// Unwrap returns the APIError.
func (e *ConflictError) Unwrap() error { return e.APIError }

// UnprocessableEntityError is a 422: the request is well formed but invalid
// (validation_failed, total_mismatch, ...).
type UnprocessableEntityError struct{ *APIError }

// Unwrap returns the APIError.
func (e *UnprocessableEntityError) Unwrap() error { return e.APIError }

// RateLimitError is a 429: a limit was reached; see RetryAfter and the Limit and ResetsAt
// members of Problem.
type RateLimitError struct{ *APIError }

// Unwrap returns the APIError.
func (e *RateLimitError) Unwrap() error { return e.APIError }

// InternalServerError is a 5xx: Flashpi failed to answer. Requests are retried automatically
// before it is returned.
type InternalServerError struct{ *APIError }

// Unwrap returns the APIError.
func (e *InternalServerError) Unwrap() error { return e.APIError }

// ConnectionError means the request never got an answer (DNS, TLS, a connection reset, ...).
// Requests are retried automatically before it is returned.
type ConnectionError struct {
	// Message names the request and the reason.
	Message string

	// IdempotencyKey is the Idempotency-Key of a failed POST: see Error.IdempotencyKey.
	IdempotencyKey string

	// Err is the error of the HTTP client.
	Err error
}

func (e *ConnectionError) Error() string { return e.Message }

// Unwrap returns the error of the HTTP client.
func (e *ConnectionError) Unwrap() error { return e.Err }

// As lets errors.As read a ConnectionError as an *Error.
func (e *ConnectionError) As(target any) bool {
	return asError(target, e.Message, e.IdempotencyKey, e.Err)
}

// TimeoutError means an attempt took longer than its timeout (WithTimeout), from sending it to
// reading the whole answer. It wraps a ConnectionError.
type TimeoutError struct{ *ConnectionError }

// Unwrap returns the ConnectionError.
func (e *TimeoutError) Unwrap() error { return e.ConnectionError }

// Timeout reports true, as net.Error does for timeouts.
func (e *TimeoutError) Timeout() bool { return true }

// WebhookVerificationReason says why a webhook was rejected.
type WebhookVerificationReason string

// The reasons of a WebhookVerificationError.
const (
	// The webhook-id, webhook-timestamp or webhook-signature header is missing.
	WebhookMissingHeaders WebhookVerificationReason = "missing_headers"
	// The webhook-timestamp is not Unix seconds.
	WebhookInvalidTimestamp WebhookVerificationReason = "invalid_timestamp"
	// The webhook-timestamp is too far from now (more than the tolerance).
	WebhookTimestampOutOfRange WebhookVerificationReason = "timestamp_out_of_range"
	// No signature matches the body and the secret.
	WebhookNoMatchingSignature WebhookVerificationReason = "no_matching_signature"
	// The secret is empty or not a whsec_ secret.
	WebhookInvalidSecret WebhookVerificationReason = "invalid_secret"
	// The body is not a JSON object.
	WebhookInvalidPayload WebhookVerificationReason = "invalid_payload"
)

// WebhookVerificationError means a webhook did not come from Flashpi, was altered, or is too
// old: answer 400 and ignore it.
type WebhookVerificationError struct {
	// Reason says why it was rejected.
	Reason WebhookVerificationReason

	// Message explains it, in English.
	Message string

	// Err is the cause, if any (the JSON error of a body that is not an event).
	Err error
}

func (e *WebhookVerificationError) Error() string { return e.Message }

// Unwrap returns the cause, if any.
func (e *WebhookVerificationError) Unwrap() error { return e.Err }

// As lets errors.As read a WebhookVerificationError as an *Error.
func (e *WebhookVerificationError) As(target any) bool {
	return asError(target, e.Message, "", e.Err)
}

// newAPIError is the error of an answer with an error status, of the type of its status.
func newAPIError(status int, header http.Header, body []byte) error {
	text := decodeText(body)
	problem := parseProblem(body)
	base := &APIError{
		Message:   errorMessage(status, problem, text),
		Status:    status,
		Problem:   problem,
		Header:    header,
		RequestID: header.Get("X-Request-Id"),
	}
	if problem != nil {
		base.Code = problem.Code
		base.Title = problem.Title
		base.Detail = problem.Detail
		base.Errors = problem.Errors
		if problem.RequestID != "" {
			base.RequestID = problem.RequestID
		}
	}
	if base.Errors == nil {
		base.Errors = []FieldError{}
	}
	switch {
	case status == http.StatusBadRequest:
		return &BadRequestError{base}
	case status == http.StatusUnauthorized:
		return &AuthenticationError{base}
	case status == http.StatusForbidden:
		return &PermissionDeniedError{base}
	case status == http.StatusNotFound:
		return &NotFoundError{base}
	case status == http.StatusConflict:
		return &ConflictError{base}
	case status == http.StatusUnprocessableEntity:
		return &UnprocessableEntityError{base}
	case status == http.StatusTooManyRequests:
		return &RateLimitError{base}
	case status >= 500:
		return &InternalServerError{base}
	}
	return base
}

// parseProblem reads a Problem Details body: a JSON object whose code is a string. Members of
// an unexpected type are left empty rather than losing the whole problem (they stay in Extra).
// Anything else (an answer from a proxy or a firewall) is nil.
func parseProblem(body []byte) *ProblemDetails {
	var members map[string]json.RawMessage
	if json.Unmarshal(body, &members) != nil || members == nil {
		return nil
	}
	// The code must be a string ("" included): null is not one.
	var code string
	if raw := bytes.TrimSpace(members["code"]); len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &code) != nil {
		return nil
	}
	problem := new(ProblemDetails)
	if json.Unmarshal(body, problem) == nil {
		return problem
	}
	// One member did not fit its type: read the members one by one.
	problem = &ProblemDetails{Code: ErrorCode(code), Extra: members}
	_ = json.Unmarshal(members["type"], &problem.Type)
	_ = json.Unmarshal(members["title"], &problem.Title)
	_ = json.Unmarshal(members["detail"], &problem.Detail)
	_ = json.Unmarshal(members["instance"], &problem.Instance)
	_ = json.Unmarshal(members["requestId"], &problem.RequestID)
	_ = json.Unmarshal(members["documentId"], &problem.DocumentID)
	if json.Unmarshal(members["errors"], &problem.Errors) != nil {
		problem.Errors = nil
	}
	return problem
}

func errorMessage(status int, problem *ProblemDetails, text string) string {
	if problem != nil {
		detail := problem.Detail
		if detail == "" {
			detail = problem.Title
		}
		if detail == "" {
			return fmt.Sprintf("%d %s", status, problem.Code)
		}
		return fmt.Sprintf("%d %s: %s", status, problem.Code, detail)
	}
	excerpt := []rune(strings.Join(strings.Fields(text), " "))
	if len(excerpt) > 200 {
		excerpt = excerpt[:200]
	}
	if len(excerpt) == 0 {
		return fmt.Sprintf("%d (no response body)", status)
	}
	return fmt.Sprintf("%d %s", status, string(excerpt))
}

// decodeText is UTF-8 text with every invalid byte replaced by U+FFFD.
func decodeText(body []byte) string {
	if utf8.Valid(body) {
		return string(body)
	}
	var builder strings.Builder
	for len(body) > 0 {
		r, size := utf8.DecodeRune(body)
		builder.WriteRune(r) // RuneError for an invalid byte
		body = body[size:]
	}
	return builder.String()
}

// maxDeltaDigits bounds a delta-seconds Retry-After: longer ones are past any useful wait.
const maxDeltaDigits = 18

// parseRetryAfter reads a Retry-After value: delta-seconds or an HTTP date (or an RFC 3339
// one), as whole seconds from now.
func parseRetryAfter(value string, now time.Time) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if strings.Trim(value, "0123456789") == "" {
		if len(value) > maxDeltaDigits {
			return 1_000_000_000_000_000_000, true
		}
		seconds, err := strconv.ParseInt(value, 10, 64)
		return seconds, err == nil
	}
	moment, err := http.ParseTime(value)
	if err != nil {
		moment, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return 0, false
		}
	}
	wait := moment.Sub(now)
	if wait <= 0 {
		return 0, true
	}
	return int64(math.Ceil(wait.Seconds())), true
}

// withIdempotencyKey sets the Idempotency-Key on an error of the SDK.
func withIdempotencyKey(err error, key string) error {
	if key == "" {
		return err
	}
	var apiErr *APIError
	var connErr *ConnectionError
	switch {
	case errors.As(err, &apiErr):
		apiErr.IdempotencyKey = key
	case errors.As(err, &connErr):
		connErr.IdempotencyKey = key
	default:
		// Not through errors.As, which also reads the types above as a new *Error.
		if sdkErr, ok := err.(*Error); ok {
			sdkErr.IdempotencyKey = key
		}
	}
	return err
}
