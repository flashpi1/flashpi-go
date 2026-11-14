package flashpi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

var invoiceBody = CreateInvoice{
	IssuerID:      "issuer-1",
	Establishment: "001",
	EmissionPoint: "001",
	Customer:      DocumentParty{IDType: IDTypeFinalConsumer},
	Items: []DocumentItem{{
		Description: "Service <&>",
		Quantity:    "1",
		UnitPrice:   "1.00",
		Taxes:       []DocumentTax{{Type: TaxTypeVAT, Rate: "15"}},
	}},
	Payments: []DocumentPayment{{Method: PaymentMethodCash, Amount: "1.15"}},
}

func TestTheHeadersOfARequest(t *testing.T) {
	client, transport, _ := testClient(t, respond(200, documentJSON("doc-1")))
	if _, err := client.Documents.Get(context.Background(), "doc-1"); err != nil {
		t.Fatal(err)
	}
	request := transport.only(t)
	want := map[string]string{
		"Accept":        "application/json",
		"User-Agent":    "flashpi-go/" + Version,
		"Authorization": "Bearer " + testKey,
	}
	for name, value := range want {
		if got := request.Header.Get(name); got != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
	for _, name := range []string{"Idempotency-Key", "Content-Type", "Accept-Language", "X-Correlation-Id"} {
		if request.Header.Get(name) != "" {
			t.Errorf("a GET sent %s", name)
		}
	}
	if request.Method != "GET" || request.URL != testBaseURL+"/v1/documents/doc-1" || len(request.Body) != 0 {
		t.Errorf("got %s %s with %q", request.Method, request.URL, request.Body)
	}
}

func TestTheHeadersFromTheLowestPriorityToTheHighest(t *testing.T) {
	client, transport, _ := testClientWith(t,
		[]ClientOption{WithLanguage(LanguageEnglish), WithHeader("X-Client", "client"), WithHeader("Accept-Language", "fr"), WithHeader("User-Agent", "mine")},
		respond(201, documentJSON("doc-1")),
	)
	_, err := client.Invoices.Create(context.Background(), invoiceBody,
		WithLanguage(LanguageSpanish),
		WithHeader("X-Call", "call"),
		WithHeader("Authorization", "Bearer fp_test_other"),
		WithHeader("Content-Type", "text/plain"),
		WithHeader("X-Correlation-Id", "from-header"),
		WithCorrelationID("  order-42  "),
	)
	if err != nil {
		t.Fatal(err)
	}
	h := transport.only(t).Header
	checks := map[string]string{
		"X-Client":         "client",
		"X-Call":           "call",
		"Accept-Language":  "es",
		"User-Agent":       "mine",
		"Authorization":    "Bearer " + testKey, // a header never replaces the key
		"Content-Type":     "application/json",  // the SDK's own
		"X-Correlation-Id": "order-42",
		"Idempotency-Key":  "key-1",
	}
	for name, value := range checks {
		if got := h.Values(name); len(got) != 1 || got[0] != value {
			t.Errorf("%s = %q, want %q", name, got, value)
		}
	}
}

func TestTheClientLanguageAppliesUnlessACallChangesIt(t *testing.T) {
	client, transport, _ := testClientWith(t, []ClientOption{WithLanguage(" es ")},
		respond(200, `{"status":"operational"}`), respond(200, `{"status":"operational"}`), respond(200, `{"status":"operational"}`))
	ctx := context.Background()
	for _, options := range [][]RequestOption{nil, {WithLanguage("en")}, {WithLanguage("  ")}} {
		if _, err := client.Status.Get(ctx, options...); err != nil {
			t.Fatal(err)
		}
	}
	requests := transport.all()
	got := []string{requests[0].Header.Get("Accept-Language"), requests[1].Header.Get("Accept-Language"), requests[2].Header.Get("Accept-Language")}
	if !slices.Equal(got, []string{"es", "en", "es"}) {
		t.Errorf("Accept-Language = %q", got)
	}
}

func TestAnEmptyLanguageIsNotSent(t *testing.T) {
	client, transport, _ := testClientWith(t, []ClientOption{WithLanguage("")}, respond(200, `{"status":"operational"}`))
	if _, err := client.Status.Get(context.Background(), WithLanguage("")); err != nil {
		t.Fatal(err)
	}
	if _, sent := transport.only(t).Header["Accept-Language"]; sent {
		t.Error("an empty language was sent")
	}
}

func TestTheJSONBody(t *testing.T) {
	client, transport, _ := testClient(t, respond(202, documentJSON("doc-1")))
	if _, err := client.Invoices.Create(context.Background(), invoiceBody); err != nil {
		t.Fatal(err)
	}
	request := transport.only(t)
	body := string(request.Body)
	if !strings.HasPrefix(body, `{"issuerId":"issuer-1","establishment":"001","emissionPoint":"001","customer":{"idType":"final_consumer"},"items":[{"description":"Service <&>"`) {
		t.Errorf("body = %s", body)
	}
	if strings.HasSuffix(body, "\n") || strings.Contains(body, `"options"`) || strings.Contains(body, `"code"`) {
		t.Errorf("body = %q", body)
	}
	if request.Method != "POST" || request.URL != testBaseURL+"/v1/invoices" {
		t.Errorf("got %s %s", request.Method, request.URL)
	}
}

func TestIdsArePercentEncoded(t *testing.T) {
	client, transport, _ := testClient(t, respond(200, documentJSON("x")), respond(200, documentJSON("x")))
	ctx := context.Background()
	if _, err := client.Documents.Get(ctx, "a/b?c#d e"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Documents.Get(ctx, "ñ!~*'()-_."); err != nil {
		t.Fatal(err)
	}
	requests := transport.all()
	if got := requests[0].URL; got != testBaseURL+"/v1/documents/a%2Fb%3Fc%23d%20e" {
		t.Errorf("URL = %s", got)
	}
	if got := requests[1].URL; got != testBaseURL+"/v1/documents/%C3%B1!~*'()-_." {
		t.Errorf("URL = %s", got)
	}
}

func TestBadIdsAreRefusedBeforeSending(t *testing.T) {
	client, transport, _ := testClient(t)
	ctx := context.Background()
	for id, message := range map[string]string{
		"":   "Missing id in /v1/documents/{id}",
		"  ": "Missing id in /v1/documents/{id}",
		".":  "Invalid id in /v1/documents/{id}",
		"..": "Invalid id in /v1/documents/{id}",
	} {
		_, err := client.Documents.Get(ctx, id)
		if sdkErr := mustAs[*Error](t, err); !strings.Contains(sdkErr.Message, message) {
			t.Errorf("%q: %v", id, err)
		}
	}
	err := client.Issuers.Establishments.EmissionPoints.Delete(ctx, "issuer", "", "point")
	if err == nil || !strings.Contains(err.Error(), "/v1/issuers/{id}/establishments/{id}/emission-points/{id}") {
		t.Errorf("got %v", err)
	}
	for _, err := range client.Issuers.Establishments.List(ctx, "", nil) {
		if err == nil || !strings.Contains(err.Error(), "Missing id") {
			t.Errorf("got %v", err)
		}
	}
	if len(transport.all()) != 0 {
		t.Error("a request was sent")
	}
}

func TestANewIdempotencyKeyIsReusedByTheRetries(t *testing.T) {
	client, transport, clock := testClient(t,
		failNetwork("connection reset by peer"),
		problem(503, "service_unavailable"),
		respond(201, documentJSON("doc-1")),
	)
	document, err := client.Invoices.Create(context.Background(), invoiceBody)
	if err != nil {
		t.Fatal(err)
	}
	if document.ID != "doc-1" {
		t.Errorf("ID = %s", document.ID)
	}
	requests := transport.all()
	if len(requests) != 3 {
		t.Fatalf("%d requests", len(requests))
	}
	for _, request := range requests {
		if request.Header.Get("Idempotency-Key") != "key-1" || !bytes.Equal(request.Body, requests[0].Body) {
			t.Errorf("an attempt changed: %q %s", request.Header.Get("Idempotency-Key"), request.Body)
		}
	}
	if got := clock.Sleeps(); !slices.Equal(got, []time.Duration{500 * time.Millisecond, time.Second}) {
		t.Errorf("sleeps = %v", got)
	}
}

func TestTheCallersIdempotencyKey(t *testing.T) {
	client, transport, _ := testClient(t, respond(201, documentJSON("a")), respond(201, documentJSON("b")), respond(201, documentJSON("c")))
	ctx := context.Background()
	if _, err := client.Invoices.Create(ctx, invoiceBody, WithIdempotencyKey(" order-1 ")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Invoices.Create(ctx, invoiceBody, WithHeader("idempotency-key", "first"), WithHeader("Idempotency-Key", "last")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Invoices.Create(ctx, invoiceBody, WithHeader("Idempotency-Key", "header"), WithIdempotencyKey("option")); err != nil {
		t.Fatal(err)
	}
	requests := transport.all()
	for index, want := range []string{"order-1", "last", "option"} {
		if got := requests[index].Header.Values("Idempotency-Key"); len(got) != 1 || got[0] != want {
			t.Errorf("request %d: Idempotency-Key = %q, want %q", index, got, want)
		}
	}
}

func TestOnlyPOSTsGetAnIdempotencyKey(t *testing.T) {
	client, transport, _ := testClient(t, respond(200, documentJSON("x")), respond(200, documentJSON("x")), respond(204, ""))
	ctx := context.Background()
	if _, err := client.Documents.Get(ctx, "x", WithIdempotencyKey("ignored")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Issuers.Update(ctx, "x", UpdateIssuer{}); err != nil {
		t.Fatal(err)
	}
	if err := client.Issuers.Delete(ctx, "x"); err != nil {
		t.Fatal(err)
	}
	for _, request := range transport.all() {
		if request.Header.Get("Idempotency-Key") != "" {
			t.Errorf("%s sent an Idempotency-Key", request.Method)
		}
	}
}

func TestTheRealKeyIsAUUID(t *testing.T) {
	key := newUUID()
	if len(key) != 36 || key[14] != '4' || !strings.ContainsRune("89ab", rune(key[19])) || key == newUUID() {
		t.Errorf("newUUID() = %s", key)
	}
}

func TestRequestOptionsAreChecked(t *testing.T) {
	client, transport, _ := testClient(t)
	ctx := context.Background()
	cases := []struct {
		option  RequestOption
		message string
	}{
		{WithTimeout(0), "timeout must be more than 0"},
		{WithMaxRetries(-2), "0 or more"},
		{WithIdempotencyKey("a\nb"), "Idempotency-Key has a line break"},
		{WithCorrelationID("a\x7fb"), "correlation id has a line break"},
		{WithLanguage("e\x01s"), "language has a line break"},
		{WithHeader("X Bad", "v"), "not a valid header name"},
		{WithHeader("Content-Length", "1"), "set by the SDK"},
		{WithHeader("X-Test", "\xff"), "not UTF-8"},
	}
	for _, tc := range cases {
		_, err := client.Invoices.Create(ctx, invoiceBody, tc.option)
		if sdkErr := mustAs[*Error](t, err); !strings.Contains(sdkErr.Message, tc.message) {
			t.Errorf("message %q does not say %q", sdkErr.Message, tc.message)
		}
	}
	if len(transport.all()) != 0 {
		t.Error("a request was sent")
	}
}

func TestATabInsideAHeaderValueIsKept(t *testing.T) {
	client, transport, _ := testClient(t, respond(200, `{"status":"operational"}`))
	if _, err := client.Status.Get(context.Background(), WithHeader("X-Test", " \ta\tb ñ\r\n")); err != nil {
		t.Fatal(err)
	}
	if got := transport.only(t).Header.Get("X-Test"); got != "a\tb ñ" {
		t.Errorf("X-Test = %q", got)
	}
}

func TestNilRequestOptionsAreSkipped(t *testing.T) {
	client, _, _ := testClient(t, respond(200, `{"status":"operational"}`))
	if _, err := client.Status.Get(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

func TestANilContextIsAnError(t *testing.T) {
	client, transport, _ := testClient(t)
	//lint:ignore SA1012 a nil context is what this test checks
	_, err := client.Status.Get(nil)
	if sdkErr := mustAs[*Error](t, err); !strings.Contains(sdkErr.Message, "nil Context") {
		t.Errorf("got %v", err)
	}
	if len(transport.all()) != 0 {
		t.Error("a request was sent")
	}
}

func TestWhatIsRetried(t *testing.T) {
	retried := []handler{
		failNetwork("dial tcp: connection refused"),
		respond(408, ""),
		problem(429, "rate_limited"),
		problem(500, "internal_error"),
		respond(502, "<html>bad gateway</html>", "Content-Type", "text/html"),
		problem(503, "service_unavailable"),
		respond(504, ""),
		problem(409, "idempotency_key_in_progress"),
	}
	for index, first := range retried {
		client, transport, _ := testClient(t, first, respond(200, `{"status":"operational"}`))
		if _, err := client.Status.Get(context.Background()); err != nil {
			t.Errorf("case %d: %v", index, err)
		}
		if len(transport.all()) != 2 {
			t.Errorf("case %d: %d requests", index, len(transport.all()))
		}
	}
	notRetried := []handler{
		problem(400, "bad_request"),
		problem(401, "invalid_api_key"),
		problem(403, "key_read_only"),
		problem(404, "not_found"),
		problem(409, "sequential_already_used"),
		problem(422, "validation_failed"),
		respond(501, ""),
		problem(503, "demo_issuer_unavailable"),
		problem(429, "demo_issuer_quota_exceeded", "Retry-After", "61"),
		respond(302, ""),
	}
	for index, first := range notRetried {
		client, transport, _ := testClient(t, first)
		_, err := client.Status.Get(context.Background())
		mustAs[*APIError](t, err)
		if len(transport.all()) != 1 {
			t.Errorf("case %d: %d requests", index, len(transport.all()))
		}
	}
}

func TestRetriesStopAtMaxRetries(t *testing.T) {
	client, transport, clock := testClient(t, problem(500, "internal_error"), problem(500, "internal_error"), problem(500, "internal_error"))
	_, err := client.Status.Get(context.Background())
	internal := mustAs[*InternalServerError](t, err)
	if internal.Status != 500 || len(transport.all()) != 3 || len(clock.Sleeps()) != 2 {
		t.Errorf("status %d after %d requests and %d sleeps", internal.Status, len(transport.all()), len(clock.Sleeps()))
	}
	client, transport, _ = testClientWith(t, []ClientOption{WithMaxRetries(0)}, problem(500, "internal_error"))
	if _, err := client.Status.Get(context.Background()); err == nil || len(transport.all()) != 1 {
		t.Error("WithMaxRetries(0) retried")
	}
	client, transport, _ = testClientWith(t, []ClientOption{WithMaxRetries(0)}, problem(500, "internal_error"), respond(200, `{"status":"operational"}`))
	if _, err := client.Status.Get(context.Background(), WithMaxRetries(1)); err != nil || len(transport.all()) != 2 {
		t.Errorf("a call's WithMaxRetries was ignored: %v", err)
	}
}

func TestBackoffAndJitter(t *testing.T) {
	e := &engine{random: func() float64 { return 0 }}
	for retries, want := range []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 8 * time.Second} {
		if got := e.backoff(retries); got != want {
			t.Errorf("backoff(%d) = %v, want %v", retries, got, want)
		}
	}
	if got := e.backoff(1000); got != 8*time.Second {
		t.Errorf("backoff(1000) = %v", got)
	}
	e.random = func() float64 { return 0.9999 }
	if got := e.backoff(0); got != 375*time.Millisecond {
		t.Errorf("full jitter = %v", got)
	}
	e.random = func() float64 { return 0.5 }
	if got := e.backoff(2); got != 1750*time.Millisecond {
		t.Errorf("half jitter = %v", got)
	}
}

func TestRetryAfter(t *testing.T) {
	client, transport, clock := testClient(t,
		problem(429, "rate_limited", "Retry-After", "3"),
		problem(503, "service_unavailable", "Retry-After", "Fri, 13 Nov 2026 12:00:42 GMT"),
		respond(200, `{"status":"operational"}`),
	)
	if _, err := client.Status.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	// The clock moved 3 s with the first wait, so the date is 39 s away.
	if got := clock.Sleeps(); !slices.Equal(got, []time.Duration{3 * time.Second, 39 * time.Second}) || len(transport.all()) != 3 {
		t.Errorf("sleeps = %v", got)
	}
	client, transport, _ = testClient(t, problem(429, "rate_limited", "Retry-After", "60"), respond(200, `{"status":"operational"}`))
	if _, err := client.Status.Get(context.Background()); err != nil || len(transport.all()) != 2 {
		t.Errorf("a Retry-After of 60 s was not waited for: %v", err)
	}
	client, transport, _ = testClient(t, problem(429, "rate_limited", "Retry-After", "3600"))
	_, err := client.Status.Get(context.Background())
	rate := mustAs[*RateLimitError](t, err)
	if wait, ok := rate.RetryAfter(); !ok || wait != time.Hour || len(transport.all()) != 1 {
		t.Errorf("RetryAfter() = %v %v after %d requests", wait, ok, len(transport.all()))
	}
	client, _, clock = testClient(t, problem(429, "rate_limited", "Retry-After", "soon"), respond(200, `{"status":"operational"}`))
	if _, err := client.Status.Get(context.Background()); err != nil || !slices.Equal(clock.Sleeps(), []time.Duration{500 * time.Millisecond}) {
		t.Errorf("an unreadable Retry-After did not fall back to the backoff: %v %v", err, clock.Sleeps())
	}
}

func TestAnInProgressAttemptIsWaitedForOnlyWhenItsAnswerWasLost(t *testing.T) {
	// The first attempt timed out: the 409 is this call's own attempt, still running.
	client, transport, clock := testClient(t,
		failNetwork("connection reset"),
		problem(409, "idempotency_key_in_progress"),
		problem(409, "idempotency_key_in_progress"),
		problem(409, "idempotency_key_in_progress"),
		problem(409, "idempotency_key_in_progress"),
		respond(200, documentJSON("doc-1")),
	)
	document, err := client.Invoices.Create(context.Background(), invoiceBody)
	if err != nil {
		t.Fatal(err)
	}
	if document.ID != "doc-1" || len(transport.all()) != 6 {
		t.Errorf("%s after %d requests", document.ID, len(transport.all()))
	}
	want := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second}
	if got := clock.Sleeps(); !slices.Equal(got, want) {
		t.Errorf("sleeps = %v, want %v", got, want)
	}
}

func TestTheInProgressWaitEndsWithTheCallsTime(t *testing.T) {
	client, transport, _ := testClientWith(t, []ClientOption{WithTimeout(4 * time.Second)},
		failNetwork("connection reset"),
		problem(409, "idempotency_key_in_progress"),
		problem(409, "idempotency_key_in_progress"),
		problem(409, "idempotency_key_in_progress"),
		problem(409, "idempotency_key_in_progress"),
	)
	_, err := client.Invoices.Create(context.Background(), invoiceBody)
	conflict := mustAs[*ConflictError](t, err)
	if conflict.Code != codeInProgress || conflict.IdempotencyKey != "key-1" {
		t.Errorf("got %v with key %q", err, conflict.IdempotencyKey)
	}
	// After the 0.5 s backoff, waits of 1 s and 2 s fit in the 4 s; then the 409s spend the
	// last retry.
	if got := len(transport.all()); got != 5 {
		t.Errorf("%d requests", got)
	}
}

func TestAnInProgressConflictWithoutALostAnswerIsRetriedNormally(t *testing.T) {
	client, transport, clock := testClient(t,
		problem(409, "idempotency_key_in_progress"),
		problem(409, "idempotency_key_in_progress"),
		problem(409, "idempotency_key_in_progress"),
	)
	_, err := client.Invoices.Create(context.Background(), invoiceBody)
	mustAs[*ConflictError](t, err)
	if len(transport.all()) != 3 || !slices.Equal(clock.Sleeps(), []time.Duration{time.Second, time.Second}) {
		t.Errorf("%d requests, sleeps %v", len(transport.all()), clock.Sleeps())
	}
}

func TestARetryRecoversTheDocumentAnEarlierAttemptIssued(t *testing.T) {
	reused := respond(422, `{"type":"x","title":"Reused","status":422,"detail":"d","instance":"/v1/invoices","code":"idempotency_key_reused","errors":[],"requestId":"r","documentId":"doc-9"}`, "Content-Type", "application/problem+json")
	client, transport, _ := testClient(t, failNetwork("connection reset"), reused, respond(200, documentJSON("doc-9"), "X-Request-Id", "req-read"))
	var response Response
	document, err := client.Invoices.Create(context.Background(), invoiceBody, WithIdempotencyKey("order-9"), WithResponseInto(&response), WithHeader("X-Call", "1"))
	if err != nil {
		t.Fatal(err)
	}
	requests := transport.all()
	if document.ID != "doc-9" || len(requests) != 3 {
		t.Fatalf("%s after %d requests", document.ID, len(requests))
	}
	read := requests[2]
	if read.Method != "GET" || read.URL != testBaseURL+"/v1/documents/doc-9" || read.Header.Get("Idempotency-Key") != "" || read.Header.Get("X-Call") != "1" || len(read.Body) != 0 {
		t.Errorf("read = %s %s %v", read.Method, read.URL, read.Header)
	}
	if response.RequestID != "req-read" || response.StatusCode != 200 {
		t.Errorf("response = %+v", response)
	}
}

func TestAReusedKeyOnTheFirstAttemptIsAnError(t *testing.T) {
	reused := respond(422, `{"code":"idempotency_key_reused","documentId":"doc-9"}`)
	client, transport, _ := testClient(t, reused)
	_, err := client.Invoices.Create(context.Background(), invoiceBody, WithIdempotencyKey("order-9"))
	invalid := mustAs[*UnprocessableEntityError](t, err)
	if *invalid.Problem.DocumentID != "doc-9" || invalid.IdempotencyKey != "order-9" || len(transport.all()) != 1 {
		t.Errorf("got %v", err)
	}
}

func TestOnlyDocumentPOSTsAreRecovered(t *testing.T) {
	reused := respond(422, `{"code":"idempotency_key_reused","documentId":"doc-9"}`)
	client, transport, _ := testClient(t, failNetwork("reset"), reused)
	_, err := client.Issuers.Create(context.Background(), CreateIssuer{})
	mustAs[*UnprocessableEntityError](t, err)
	if len(transport.all()) != 2 {
		t.Errorf("%d requests", len(transport.all()))
	}
}

func TestAFailedRecoveryKeepsTheCallsKey(t *testing.T) {
	reused := respond(422, `{"code":"idempotency_key_reused","documentId":"doc-9"}`)
	client, _, _ := testClientWith(t, []ClientOption{WithMaxRetries(1)}, failNetwork("reset"), reused, problem(404, "not_found"))
	_, err := client.Invoices.Create(context.Background(), invoiceBody, WithIdempotencyKey("order-9"))
	notFound := mustAs[*NotFoundError](t, err)
	if notFound.IdempotencyKey != "order-9" {
		t.Errorf("key = %q", notFound.IdempotencyKey)
	}
}

func TestADeleteRetriedAfterALostAnswerThatFindsNothingIsDone(t *testing.T) {
	client, transport, _ := testClient(t, failNetwork("reset"), problem(404, "not_found", "X-Request-Id", "req-404", "Content-Length", "200"))
	var response Response
	if err := client.WebhookEndpoints.Delete(context.Background(), "we-1", WithResponseInto(&response)); err != nil {
		t.Fatal(err)
	}
	if len(transport.all()) != 2 || response.StatusCode != 204 || response.RequestID != "req_problem" {
		t.Errorf("response = %+v", response)
	}
	if response.Header.Get("Content-Type") != "" || response.Header.Get("Content-Length") != "" || response.Header.Get("X-Request-Id") != "req-404" {
		t.Errorf("headers = %v", response.Header)
	}
	client, _, _ = testClient(t, problem(404, "not_found"))
	err := client.WebhookEndpoints.Delete(context.Background(), "we-1")
	mustAs[*NotFoundError](t, err)
}

func TestErrorsCarryTheIdempotencyKeyOfAPOST(t *testing.T) {
	client, _, _ := testClientWith(t, []ClientOption{WithMaxRetries(0)}, failNetwork("connection refused"), problem(422, "validation_failed"), respond(200, "not json"))
	ctx := context.Background()
	_, err := client.Invoices.Create(ctx, invoiceBody)
	if connErr := mustAs[*ConnectionError](t, err); connErr.IdempotencyKey != "key-1" {
		t.Errorf("key = %q", connErr.IdempotencyKey)
	}
	_, err = client.Invoices.Create(ctx, invoiceBody)
	if apiErr := mustAs[*APIError](t, err); apiErr.IdempotencyKey != "key-2" {
		t.Errorf("key = %q", apiErr.IdempotencyKey)
	}
	if sdkErr := mustAs[*Error](t, err); sdkErr.IdempotencyKey != "key-2" {
		t.Errorf("as *Error, key = %q", sdkErr.IdempotencyKey)
	}
	_, err = client.Invoices.Create(ctx, invoiceBody)
	if sdkErr := mustAs[*Error](t, err); sdkErr.IdempotencyKey != "key-3" || !strings.Contains(sdkErr.Message, "not JSON") {
		t.Errorf("got %v with key %q", err, sdkErr.IdempotencyKey)
	}
}

func TestACancelledCallStopsAndIsNotRetried(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client, transport, _ := testClient(t, func(*http.Request) (*http.Response, error) {
		cancel()
		return nil, context.Canceled
	})
	_, err := client.Invoices.Create(ctx, invoiceBody)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	sdkErr := mustAs[*Error](t, err)
	if sdkErr.IdempotencyKey != "key-1" || len(transport.all()) != 1 {
		t.Errorf("key %q after %d requests", sdkErr.IdempotencyKey, len(transport.all()))
	}
	var connErr *ConnectionError
	if errors.As(err, &connErr) {
		t.Error("a cancellation is a ConnectionError")
	}
}

func TestACancelDuringTheWaitStopsTheRetries(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client, transport, _ := testClient(t, problem(503, "service_unavailable"), respond(200, "{}"))
	client.engine.sleep = func(ctx context.Context, d time.Duration) error {
		cancel()
		return sleepContext(ctx, time.Hour)
	}
	_, err := client.Status.Get(ctx)
	if !errors.Is(err, context.Canceled) || len(transport.all()) != 1 {
		t.Errorf("got %v after %d requests", err, len(transport.all()))
	}
}

func TestADoneContextSendsNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client, transport, _ := testClient(t)
	if _, err := client.Status.Get(ctx); !errors.Is(err, context.Canceled) || len(transport.all()) != 0 {
		t.Errorf("got %v after %d requests", err, len(transport.all()))
	}
}

func TestAnswersThatCannotBeRead(t *testing.T) {
	cases := []struct {
		handler handler
		message string
	}{
		{respond(200, ""), "answered 200 without a body"},
		{respond(200, "<html>"), "answered 200 with a body that is not JSON"},
		{respond(200, `{"id":42}`), "cannot read"},
		{respond(200, `{"status":"operational"}`, "Content-Encoding", "br"), "could not be decoded: unsupported Content-Encoding br"},
	}
	for _, tc := range cases {
		client, transport, _ := testClient(t, tc.handler)
		_, err := client.Documents.Get(context.Background(), "doc-1")
		if sdkErr := mustAs[*Error](t, err); !strings.Contains(sdkErr.Message, tc.message) {
			t.Errorf("message %q does not say %q", sdkErr.Message, tc.message)
		}
		if len(transport.all()) != 1 {
			t.Error("an unreadable answer was retried")
		}
	}
}

func TestACompressedAnswerIsDecoded(t *testing.T) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, _ = writer.Write([]byte(`{"status":"operational"}`))
	_ = writer.Close()
	client, _, _ := testClient(t, respond(200, compressed.String(), "Content-Encoding", "gzip"))
	status, err := client.Status.Get(context.Background(), WithHeader("Accept-Encoding", "gzip"))
	if err != nil || status.Status != StatusStatusOperational {
		t.Fatalf("got %v %v", status, err)
	}
}

func TestWithResponseInto(t *testing.T) {
	client, _, _ := testClient(t, respond(200, `{"status":"operational"}`, "X-Request-Id", "req-1", "X-Other", "v"))
	var response Response
	if _, err := client.Status.Get(context.Background(), WithResponseInto(&response)); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || response.RequestID != "req-1" || response.Header.Get("X-Other") != "v" {
		t.Errorf("response = %+v", response)
	}
	client, _, _ = testClient(t, problem(404, "not_found"))
	response = Response{StatusCode: -1}
	if _, err := client.Status.Get(context.Background(), WithResponseInto(&response)); err == nil || response.StatusCode != -1 {
		t.Error("a failed call filled the response")
	}
	client, _, _ = testClient(t, respond(200, "nope"))
	if _, err := client.Status.Get(context.Background(), WithResponseInto(&response)); err == nil || response.StatusCode != -1 {
		t.Error("an unreadable answer filled the response")
	}
}

func TestTheConnectionErrorNamesTheRequest(t *testing.T) {
	client, _, _ := testClientWith(t, []ClientOption{WithMaxRetries(0)}, failNetwork("dial tcp 10.0.0.1:443: connect: connection refused"))
	_, err := client.Documents.Get(context.Background(), "doc-1")
	connErr := mustAs[*ConnectionError](t, err)
	if connErr.Message != "GET https://api.test/v1/documents/doc-1 failed: dial tcp 10.0.0.1:443: connect: connection refused" {
		t.Errorf("message = %q", connErr.Message)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Error("the error shows the key")
	}
}

func TestSyncTimeoutFloor(t *testing.T) {
	cases := []struct {
		body string
		want time.Duration
	}{
		{`{}`, 0},
		{`{"options":{"waitForAuthorization":false}}`, 0},
		{`{"options":{"waitForAuthorization":true}}`, 30 * time.Second},
		{`{"options":{"waitForAuthorization":true,"waitTimeoutSeconds":25}}`, 40 * time.Second},
		{`{"options":{"waitForAuthorization":true,"waitTimeoutSeconds":null}}`, 30 * time.Second},
		{`{"options":{"waitForAuthorization":true,"waitTimeoutSeconds":"20"}}`, 30 * time.Second},
		{`{"options":{"waitForAuthorization":true,"waitTimeoutSeconds":-100}}`, 0},
		{`{"options":{"waitForAuthorization":true,"waitTimeoutSeconds":1e300}}`, maxTimeout},
		{`{"options":{"waitForAuthorization":"true"}}`, 0},
	}
	for _, tc := range cases {
		if got := syncTimeoutFloor([]byte(tc.body)); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.body, got, tc.want)
		}
	}
}

func TestASyncIssueRaisesTheTimeoutOfEachAttempt(t *testing.T) {
	client, _, _ := testClientWith(t, []ClientOption{WithTimeout(5 * time.Second)})
	body := invoiceBody
	body.Options = &DocumentOptions{WaitForAuthorization: ptr(true), WaitTimeoutSeconds: ptr(20)}
	content, err := encodeJSON(body)
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.engine.prepare(&operation{method: "POST", path: "/v1/invoices", minTimeout: syncTimeoutFloor(content)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.timeout != 35*time.Second {
		t.Errorf("timeout = %v", c.timeout)
	}
	c, _ = client.engine.prepare(&operation{method: "POST", minTimeout: syncTimeoutFloor(content)}, []RequestOption{WithTimeout(time.Minute)})
	if c.timeout != time.Minute {
		t.Errorf("a longer timeout was lowered: %v", c.timeout)
	}
}

func TestTheQueryOfAList(t *testing.T) {
	client, transport, _ := testClient(t, respond(200, `{"data":[],"pagination":{"nextCursor":null,"hasMore":false}}`))
	params := &DocumentListParams{Limit: ptr(50), Status: ptr(DocumentStatusAuthorized), IssueDateFrom: ptr("2026-11-01"), AccessKey: ptr("a b&c")}
	for _, err := range client.Documents.List(context.Background(), params) {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := transport.only(t).URL; got != testBaseURL+"/v1/documents?accessKey=a+b%26c&issueDateFrom=2026-11-01&limit=50&status=authorized" {
		t.Errorf("URL = %s", got)
	}
}

func TestEncodeJSONLikeJavaScript(t *testing.T) {
	content, err := encodeJSON(map[string]any{"a": "<b>&ñ", "n": 1.5})
	if err != nil || string(content) != `{"a":"<b>&ñ","n":1.5}` {
		t.Errorf("got %s %v", content, err)
	}
	if _, err := encodeJSON(map[string]any{"bad": make(chan int)}); err == nil {
		t.Error("a channel was encoded")
	}
	var raw json.RawMessage = []byte("{bad")
	_, err = encodeJSON(raw)
	if sdkErr := mustAs[*Error](t, err); !strings.Contains(sdkErr.Message, "cannot be sent as JSON") {
		t.Errorf("got %v", err)
	}
}

func TestAnEmptyIdempotencyKeyIsRefused(t *testing.T) {
	client, transport, _ := testClient(t)
	ctx := context.Background()
	for _, option := range []RequestOption{
		WithIdempotencyKey(""),
		WithIdempotencyKey("  \t "),
		WithHeader("Idempotency-Key", ""),
		WithHeader("idempotency-key", " "),
	} {
		_, err := client.Invoices.Create(ctx, invoiceBody, option)
		if sdkErr := mustAs[*Error](t, err); !strings.Contains(sdkErr.Message, "Idempotency-Key is empty") {
			t.Errorf("got %v", err)
		}
	}
	if len(transport.all()) != 0 {
		t.Error("a POST without an Idempotency-Key was sent")
	}
}

func TestBodyTextThatIsNotUTF8IsRefused(t *testing.T) {
	client, transport, _ := testClient(t)
	ctx := context.Background()
	_, err := client.Issuers.Create(ctx, CreateIssuer{LegalName: "JOS\xc9 P\xc9REZ"})
	if sdkErr := mustAs[*Error](t, err); sdkErr.Message != "The request body has text that is not UTF-8" {
		t.Errorf("got %v", err)
	}
	_, err = client.Issuers.Update(ctx, "i", UpdateIssuer{TradeName: NullableOf("\xff")})
	mustAs[*Error](t, err)
	body := invoiceBody
	body.AdditionalInfo = map[string]string{"k\xff": "v"}
	_, err = client.Invoices.Create(ctx, body)
	mustAs[*Error](t, err)
	if len(transport.all()) != 0 {
		t.Error("a body that is not UTF-8 was sent")
	}
	for _, valid := range []any{
		map[string]string{"a": "\ufffd"},    // a real replacement character
		map[string]string{"a": `\ufffd`},    // the letters, after a backslash
		map[string]string{"a": `x\\\ufffd`}, // escaped backslashes, then the letters
	} {
		if _, err := encodeJSON(valid); err != nil {
			t.Errorf("%v was refused: %v", valid, err)
		}
	}
	if _, err := encodeJSON(json.RawMessage("\"\xff\"")); err == nil {
		t.Error("raw JSON that is not UTF-8 was accepted")
	}
	for _, invalid := range []any{
		[]any{map[string]any{"deep": []string{"ok", "\xc3"}}},
		&struct{ Inner *string }{Inner: ptr("\xe2\x82")},
		[2]string{"ok", "\x80"},
	} {
		if _, err := encodeJSON(invalid); err == nil {
			t.Errorf("%#v was accepted", invalid)
		}
	}
	if !validText(reflect.ValueOf(map[string][]byte{"bytes": {0xff}}), 0) {
		t.Error("bytes, sent as base64, were refused")
	}
}

func TestConnectionHeadersAreRefused(t *testing.T) {
	client, transport, _ := testClient(t)
	for _, name := range []string{"Connection", "upgrade", "Keep-Alive", "Proxy-Connection", "te", "Host"} {
		_, err := client.Status.Get(context.Background(), WithHeader(name, "x"))
		if sdkErr := mustAs[*Error](t, err); !strings.Contains(sdkErr.Message, "managed by the HTTP client") {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := NewClient(WithAPIKey(testKey), WithHeader(name, "x")); err == nil {
			t.Errorf("a client sends %s", name)
		}
	}
	if len(transport.all()) != 0 {
		t.Error("a request was sent")
	}
}
