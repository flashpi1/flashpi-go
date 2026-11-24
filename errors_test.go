package flashpi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestEachStatusHasItsType(t *testing.T) {
	cases := map[int]func(error) bool{
		400: func(err error) bool { var e *BadRequestError; return errors.As(err, &e) },
		401: func(err error) bool { var e *AuthenticationError; return errors.As(err, &e) },
		403: func(err error) bool { var e *PermissionDeniedError; return errors.As(err, &e) },
		404: func(err error) bool { var e *NotFoundError; return errors.As(err, &e) },
		409: func(err error) bool { var e *ConflictError; return errors.As(err, &e) },
		422: func(err error) bool { var e *UnprocessableEntityError; return errors.As(err, &e) },
		429: func(err error) bool { var e *RateLimitError; return errors.As(err, &e) },
		500: func(err error) bool { var e *InternalServerError; return errors.As(err, &e) },
		502: func(err error) bool { var e *InternalServerError; return errors.As(err, &e) },
		599: func(err error) bool { var e *InternalServerError; return errors.As(err, &e) },
	}
	for status, matches := range cases {
		err := newAPIError(status, http.Header{}, nil)
		if !matches(err) {
			t.Errorf("%d: %T", status, err)
		}
		apiErr := mustAs[*APIError](t, err)
		if apiErr.Status != status {
			t.Errorf("%d: Status = %d", status, apiErr.Status)
		}
	}
	for _, status := range []int{302, 405, 410, 418} {
		if _, ok := newAPIError(status, http.Header{}, nil).(*APIError); !ok {
			t.Errorf("%d is not a plain *APIError", status)
		}
	}
}

func TestAnAPIErrorFromProblemDetails(t *testing.T) {
	body := `{"type":"https://flashpi.com.ec/docs/en/errors/validation_failed","title":"Validation failed","status":422,
		"detail":"One or more fields are invalid.","instance":"/v1/invoices","code":"validation_failed",
		"errors":[{"field":"customer.id","code":"invalid_check_digit","message":"Bad check digit."}],
		"requestId":"req-body","expectedTotal":"1.15","newMember":{"a":1}}`
	header := http.Header{"X-Request-Id": {"req-header"}}
	err := newAPIError(422, header, []byte(body))
	invalid := mustAs[*UnprocessableEntityError](t, err)
	if invalid.Message != "422 validation_failed: One or more fields are invalid." || err.Error() != invalid.Message {
		t.Errorf("Message = %q", invalid.Message)
	}
	if invalid.Code != "validation_failed" || invalid.Title != "Validation failed" || invalid.RequestID != "req-body" {
		t.Errorf("got %q %q %q", invalid.Code, invalid.Title, invalid.RequestID)
	}
	if len(invalid.Errors) != 1 || invalid.Errors[0].Field != "customer.id" || invalid.Errors[0].Code != FieldErrorCodeInvalidCheckDigit {
		t.Errorf("Errors = %+v", invalid.Errors)
	}
	if invalid.Problem == nil || *invalid.Problem.ExpectedTotal != "1.15" || string(invalid.Problem.Extra["newMember"]) != `{"a":1}` {
		t.Errorf("Problem = %+v", invalid.Problem)
	}
	if len(invalid.Problem.Extra) != 1 {
		t.Errorf("Extra = %v", invalid.Problem.Extra)
	}
}

func TestTheCodesOfTheAPIAreRecognized(t *testing.T) {
	denied := mustAs[*PermissionDeniedError](t, newAPIError(403, http.Header{},
		[]byte(`{"status":403,"code":"live_key_required","detail":"The issuer has live documents: use a live key."}`)))
	if denied.Code != ErrorCodeLiveKeyRequired {
		t.Errorf("Code = %q", denied.Code)
	}
	conflict := mustAs[*ConflictError](t, newAPIError(409, http.Header{},
		[]byte(`{"status":409,"code":"webhook_endpoint_limit_reached","detail":"Delete an endpoint first.","limit":20}`)))
	if conflict.Code != ErrorCodeWebhookEndpointLimitReached || conflict.Problem == nil || conflict.Problem.Limit == nil || *conflict.Problem.Limit != 20 {
		t.Errorf("got %q %+v", conflict.Code, conflict.Problem)
	}
	invalid := mustAs[*UnprocessableEntityError](t, newAPIError(422, http.Header{}, []byte(`{"status":422,"code":"validation_failed","errors":[
		{"field":"file","code":"certificate_ruc_missing","message":"m"},
		{"field":"additionalInfo","code":"demo_issuer_free_text_not_allowed","message":"m"},
		{"field":"items[0].description","code":"demo_issuer_description_too_long","message":"m"}]}`)))
	want := []FieldErrorCode{FieldErrorCodeCertificateRUCMissing, FieldErrorCodeDemoIssuerFreeTextNotAllowed, FieldErrorCodeDemoIssuerDescriptionTooLong}
	if len(invalid.Errors) != len(want) {
		t.Fatalf("Errors = %+v", invalid.Errors)
	}
	for index, field := range invalid.Errors {
		if field.Code != want[index] {
			t.Errorf("Errors[%d].Code = %q, want %q", index, field.Code, want[index])
		}
	}
}

func TestAnAPIErrorWithoutProblemDetails(t *testing.T) {
	cases := []struct {
		body, message string
	}{
		{"", "502 (no response body)"},
		{"<html>\n  <b>Bad   gateway</b>\n</html>", "502 <html> <b>Bad gateway</b> </html>"},
		{`{"message":"no code"}`, `502 {"message":"no code"}`},
		{`{"code":42}`, `502 {"code":42}`},
		{`{"code":null,"detail":"bad gateway"}`, `502 {"code":null,"detail":"bad gateway"}`},
		{`["code"]`, `502 ["code"]`},
		{strings.Repeat("ñ", 300), "502 " + strings.Repeat("ñ", 200)},
		{"bad \xff byte", "502 bad � byte"},
	}
	for _, tc := range cases {
		err := newAPIError(502, http.Header{"X-Request-Id": {"req-edge"}}, []byte(tc.body))
		apiErr := mustAs[*APIError](t, err)
		if apiErr.Message != tc.message {
			t.Errorf("%q: Message = %q, want %q", tc.body, apiErr.Message, tc.message)
		}
		if apiErr.Problem != nil || apiErr.Code != "" || apiErr.RequestID != "req-edge" || apiErr.Errors == nil {
			t.Errorf("%q: got %+v", tc.body, apiErr)
		}
	}
}

func TestAProblemWithMembersOfAnotherType(t *testing.T) {
	err := newAPIError(409, http.Header{}, []byte(`{"code":"ride_not_available","status":"409","title":"Not ready","detail":7,"documentId":"doc-1","errors":"none"}`))
	conflict := mustAs[*ConflictError](t, err)
	if conflict.Code != "ride_not_available" || conflict.Title != "Not ready" || conflict.Detail != "" {
		t.Errorf("got %+v", conflict.APIError)
	}
	if conflict.Message != "409 ride_not_available: Not ready" || *conflict.Problem.DocumentID != "doc-1" || len(conflict.Errors) != 0 {
		t.Errorf("got %q %+v", conflict.Message, conflict.Problem)
	}
}

func TestAProblemWithoutDetail(t *testing.T) {
	err := newAPIError(404, http.Header{}, []byte(`{"code":"not_found"}`))
	if err.Error() != "404 not_found" {
		t.Errorf("Error() = %q", err)
	}
}

func TestErrorsAsReadsEveryErrorAsAnError(t *testing.T) {
	api := newAPIError(422, http.Header{}, []byte(`{"code":"validation_failed"}`))
	mustAs[*UnprocessableEntityError](t, api).IdempotencyKey = "k-api"
	connection := &ConnectionError{Message: "failed", IdempotencyKey: "k-conn", Err: errors.New("reset")}
	timeout := &TimeoutError{&ConnectionError{Message: "timed out", IdempotencyKey: "k-time", Err: context.DeadlineExceeded}}
	webhook := &WebhookVerificationError{Reason: WebhookNoMatchingSignature, Message: "no match"}
	cases := []struct {
		err     error
		key     string
		message string
	}{
		{api, "k-api", "422 validation_failed"},
		{connection, "k-conn", "failed"},
		{timeout, "k-time", "timed out"},
		{webhook, "", "no match"},
		{&Error{Message: "own", IdempotencyKey: "k-own"}, "k-own", "own"},
	}
	for _, tc := range cases {
		sdkErr := mustAs[*Error](t, tc.err)
		if sdkErr.IdempotencyKey != tc.key || sdkErr.Message != tc.message {
			t.Errorf("%T: got %+v", tc.err, sdkErr)
		}
	}
	if !errors.Is(timeout, context.DeadlineExceeded) {
		t.Error("a timeout does not unwrap to its cause")
	}
	if errors.Is(api, context.Canceled) {
		t.Error("an API error unwraps to a cancellation")
	}
}

func TestWithIdempotencyKey(t *testing.T) {
	timeout := &TimeoutError{&ConnectionError{Message: "x"}}
	_ = withIdempotencyKey(timeout, "k1")
	if timeout.IdempotencyKey != "k1" {
		t.Error("the timeout did not get the key")
	}
	sdkErr := &Error{Message: "x"}
	_ = withIdempotencyKey(sdkErr, "k2")
	if sdkErr.IdempotencyKey != "k2" {
		t.Error("the *Error did not get the key")
	}
	other := errors.New("other")
	if withIdempotencyKey(other, "k3") != other || withIdempotencyKey(sdkErr, "") != sdkErr {
		t.Error("an error was replaced")
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		value   string
		seconds int64
		ok      bool
	}{
		{"0", 0, true},
		{" 120 ", 120, true},
		{"999999999999999999", 999999999999999999, true},
		{"9999999999999999999999", 1_000_000_000_000_000_000, true},
		{"Fri, 13 Nov 2026 12:01:00 GMT", 60, true},
		{"Friday, 13-Nov-26 12:00:30 GMT", 30, true},
		{"Fri Nov 13 12:00:10 2026", 10, true},
		{"2026-11-13T12:00:05.5Z", 6, true},
		{"2026-11-13T07:00:20-05:00", 20, true},
		{"Fri, 13 Nov 2026 11:00:00 GMT", 0, true},
		{"", 0, false},
		{"-5", 0, false},
		{"1.5", 0, false},
		{"tomorrow", 0, false},
	}
	for _, tc := range cases {
		seconds, ok := parseRetryAfter(tc.value, now)
		if seconds != tc.seconds || ok != tc.ok {
			t.Errorf("%q: %d %v, want %d %v", tc.value, seconds, ok, tc.seconds, tc.ok)
		}
	}
}

func TestRetryAfterOfAnAPIError(t *testing.T) {
	apiErr := mustAs[*APIError](t, newAPIError(429, http.Header{"Retry-After": {"9999999999999999999999"}}, nil))
	if wait, ok := apiErr.RetryAfter(); !ok || wait <= 0 {
		t.Errorf("RetryAfter() = %v %v", wait, ok)
	}
	apiErr = mustAs[*APIError](t, newAPIError(429, http.Header{}, nil))
	if _, ok := apiErr.RetryAfter(); ok {
		t.Error("a missing Retry-After was read")
	}
}

func TestDecodeText(t *testing.T) {
	if got := decodeText([]byte("a\xff\xfeb")); got != "a��b" {
		t.Errorf("got %q", got)
	}
}

func TestAProblemSurvivesJSON(t *testing.T) {
	var problem ProblemDetails
	if err := json.Unmarshal([]byte(`{"code":"rate_limited","limit":50,"other":true}`), &problem); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(problem)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	_ = json.Unmarshal(data, &back)
	if back["other"] != true || back["limit"] != float64(50) || back["code"] != "rate_limited" {
		t.Errorf("got %s", data)
	}
	problem.Extra = map[string]json.RawMessage{"code": []byte(`"overridden"`)}
	data, _ = json.Marshal(problem)
	if !strings.Contains(string(data), `"code":"rate_limited"`) {
		t.Errorf("Extra replaced a declared member: %s", data)
	}
}

func TestAProxyErrorWithANullCodeLeavesTheAnswerUnanswered(t *testing.T) {
	// The 502 without Problem Details means the POST may have reached the API, so the 409s are
	// its own attempt, waited for without spending retries.
	client, transport, _ := testClient(t,
		respond(502, `{"code":null}`),
		problem(409, "idempotency_key_in_progress"),
		problem(409, "idempotency_key_in_progress"),
		respond(201, documentJSON("doc-1")),
	)
	if _, err := client.Invoices.Create(context.Background(), invoiceBody); err != nil || len(transport.all()) != 4 {
		t.Errorf("got %v after %d requests", err, len(transport.all()))
	}
	if problem := parseProblem([]byte(`{"code":""}`)); problem == nil {
		t.Error("an empty code is still a code")
	}
}
