package flashpi

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEveryMethodRefusesAMissingID(t *testing.T) {
	client, transport, _ := testClient(t)
	ctx := context.Background()
	calls := map[string]func() error{
		"Documents.Get":       func() error { _, err := client.Documents.Get(ctx, ""); return err },
		"Documents.XML":       func() error { _, err := client.Documents.XML(ctx, ""); return err },
		"Documents.PDF":       func() error { _, err := client.Documents.PDF(ctx, ""); return err },
		"Documents.SendEmail": func() error { _, err := client.Documents.SendEmail(ctx, ""); return err },
		"Issuers.Update":      func() error { _, err := client.Issuers.Update(ctx, "", UpdateIssuer{}); return err },
		"Establishments.Create": func() error {
			_, err := client.Issuers.Establishments.Create(ctx, "", CreateEstablishment{})
			return err
		},
		"Certificates.Upload": func() error { _, err := client.Issuers.Certificates.Upload(ctx, " ", p12()); return err },
		"Certificates.Get":    func() error { _, err := client.Issuers.Certificates.Get(ctx, "i", ""); return err },
		"IssuerOnboardings.Complete": func() error {
			_, err := client.IssuerOnboardings.Complete(ctx, "..", nil)
			return err
		},
		"WebhookEndpoints.RotateSecret": func() error { _, err := client.WebhookEndpoints.RotateSecret(ctx, "", nil); return err },
		"Deliveries.Replay":             func() error { _, err := client.WebhookEndpoints.Deliveries.Replay(ctx, "e", ""); return err },
		"Events.Get":                    func() error { _, err := client.Events.Get(ctx, ""); return err },
	}
	for name, call := range calls {
		if err := call(); err == nil || !strings.Contains(err.Error(), "id in /v1/") {
			t.Errorf("%s: %v", name, err)
		}
	}
	if len(transport.all()) != 0 {
		t.Error("a request was sent")
	}
}

func TestEveryEventTypeIsAWebhookEvent(t *testing.T) {
	events := []Event{&DocumentEvent{}, &CertificateEvent{}, &AccountEvent{}, &UnknownEvent{}}
	for _, event := range events {
		event.isEvent()
		event.isWebhookEvent()
	}
	for _, event := range []WebhookEvent{&ThinEvent{}, &WebhookEndpointVerificationEvent{}} {
		event.isWebhookEvent()
	}
}

func TestSleepContext(t *testing.T) {
	if err := sleepContext(context.Background(), 0); err != nil {
		t.Error(err)
	}
	if err := sleepContext(context.Background(), time.Millisecond); err != nil {
		t.Error(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v", err)
	}
	if err := sleepContext(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Errorf("got %v", err)
	}
}

func TestDeflateAndBrokenCompression(t *testing.T) {
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	_, _ = writer.Write([]byte(`{"status":"operational"}`))
	_ = writer.Close()
	client, _, _ := testClient(t, respond(200, compressed.String(), "Content-Encoding", "Deflate"))
	if status, err := client.Status.Get(context.Background()); err != nil || status.Status != StatusStatusOperational {
		t.Errorf("got %v %v", status, err)
	}
	client, _, _ = testClientWith(t, []ClientOption{WithMaxRetries(0)}, respond(200, "not gzip", "Content-Encoding", "gzip"))
	_, err := client.Status.Get(context.Background())
	mustAs[*ConnectionError](t, err)
	if (&unsupportedEncodingError{"br"}).Error() != "unsupported Content-Encoding br" {
		t.Error("the message of an unsupported encoding")
	}
}

func TestARecoveryNeedsADocumentID(t *testing.T) {
	op := &operation{recoverDocument: true}
	cases := []*APIError{
		{Status: 422, Code: "idempotency_key_reused"},
		{Status: 422, Code: "idempotency_key_reused", Problem: &ProblemDetails{}},
		{Status: 409, Code: "idempotency_key_reused", Problem: &ProblemDetails{DocumentID: ptr("d")}},
	}
	for index, failure := range cases {
		if id := recoverableDocument(op, failure); id != "" {
			t.Errorf("case %d: %q", index, id)
		}
	}
	reused := respond(422, `{"code":"idempotency_key_reused","documentId":".."}`)
	client, _, _ := testClient(t, failNetwork("reset"), reused)
	_, err := client.Invoices.Create(context.Background(), invoiceBody, WithIdempotencyKey("k"))
	if sdkErr := mustAs[*Error](t, err); !strings.Contains(sdkErr.Message, "Invalid id") || sdkErr.IdempotencyKey != "k" {
		t.Errorf("got %v", err)
	}
}

func TestHeaderNames(t *testing.T) {
	for name, valid := range map[string]bool{"X-Ok_1.2~!#$%&'*+^`|": true, "": false, "a b": false, "a:b": false, "ñ": false, "a\x00": false} {
		if isToken(name) != valid {
			t.Errorf("isToken(%q) = %v", name, !valid)
		}
	}
}

func TestWebhookBodiesOfTheWrongShape(t *testing.T) {
	for _, body := range []string{
		`{"type":"webhook_endpoint.verification","createdAt":"never","data":{}}`,
		`{"type":"invoice.authorized","createdAt":7}`,
		`{"type":"invoice.authorized","createdAt":"2026-11-13T12:00:00Z","data":"text"}`,
	} {
		payload := []byte(body)
		_, err := UnwrapWebhook(payload, signedHeaders(payload), webhookSecret, WithNow(webhookNow))
		if reason := reasonOf(t, err); reason != WebhookInvalidPayload {
			t.Errorf("%s: %s", body, reason)
		}
	}
	payload := []byte(documentEventBody)
	if _, err := UnwrapWebhook(payload, signedHeaders(payload), webhookSecret, nil, WithNow(webhookNow)); err != nil {
		t.Errorf("a nil option: %v", err)
	}
}

func TestAProblemWithExtraMembersOnly(t *testing.T) {
	var problem ProblemDetails
	if err := problem.UnmarshalJSON([]byte(`{"code":"x"}`)); err != nil || problem.Extra != nil {
		t.Errorf("got %+v %v", problem, err)
	}
	if err := problem.UnmarshalJSON([]byte(`[1]`)); err == nil {
		t.Error("an array was read as a problem")
	}
	data, err := problem.MarshalJSON()
	if err != nil || !strings.Contains(string(data), `"code":"x"`) {
		t.Errorf("got %s %v", data, err)
	}
}

func TestAFailedListPageAfterTheFirst(t *testing.T) {
	client, _, _ := testClient(t, respond(200, listJSON("c1", true, "a")), respond(200, "{"))
	page, err := client.Documents.ListPage(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	next, err := page.NextPage(context.Background())
	if next != nil || err == nil {
		t.Errorf("got %v %v", next, err)
	}
	var unread Page[*Document]
	unread.HasMore, unread.NextCursor = true, "c"
	if next, err := unread.NextPage(context.Background()); next != nil || err != nil {
		t.Error("a page without its reader read another")
	}
}
