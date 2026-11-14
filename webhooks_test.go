package flashpi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	webhookKey    = []byte("0123456789abcdef0123456789abcdef") // 32 bytes
	webhookSecret = "whsec_" + base64.StdEncoding.EncodeToString(webhookKey)
	webhookNow    = time.Unix(1_700_000_000, 0)
)

func sign(key []byte, id, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(id + "." + timestamp + "."))
	mac.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func signedHeaders(body []byte) http.Header {
	timestamp := strconv.FormatInt(webhookNow.Unix(), 10)
	return http.Header{
		"Webhook-Id":        {"msg_1"},
		"Webhook-Timestamp": {timestamp},
		"Webhook-Signature": {sign(webhookKey, "msg_1", timestamp, body)},
	}
}

const documentEventBody = `{"id":"evt_1","type":"invoice.authorized","environment":"test","createdAt":"2026-11-13T12:00:00.000Z","data":{"id":"doc-1","type":"invoice","environment":"test","status":"authorized","accessKey":"123"}}`

func verify(body []byte, header http.Header, secret string, options ...WebhookOption) error {
	return VerifyWebhookSignature(body, header, secret, append([]WebhookOption{WithNow(webhookNow)}, options...)...)
}

func reasonOf(t *testing.T, err error) WebhookVerificationReason {
	t.Helper()
	return mustAs[*WebhookVerificationError](t, err).Reason
}

func TestUnwrapWebhookReturnsTheEvent(t *testing.T) {
	body := []byte(documentEventBody)
	event, err := UnwrapWebhook(body, signedHeaders(body), webhookSecret, WithNow(webhookNow))
	if err != nil {
		t.Fatal(err)
	}
	document, ok := event.(*DocumentEvent)
	if !ok {
		t.Fatalf("got %T", event)
	}
	if document.Type != DocumentEventTypeInvoiceAuthorized || document.Data.AccessKey != "123" || document.Data.Status != DocumentStatusAuthorized {
		t.Errorf("got %+v", document)
	}
	info := event.Info()
	if info.ID != "evt_1" || info.Type != EventTypeInvoiceAuthorized || info.Environment != EnvironmentTest || !info.CreatedAt.Equal(time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("Info() = %+v", info)
	}
	if _, isEvent := event.(Event); !isEvent {
		t.Error("a document event is not an Event")
	}
}

func TestUnwrapWebhookOfEveryKind(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"id":"v1","type":"webhook_endpoint.verification","environment":"live","createdAt":"2026-11-13T12:00:00Z","data":{"webhookEndpointId":"we-1","challenge":"abc"}}`, "*flashpi.WebhookEndpointVerificationEvent"},
		{`{"id":"e1","type":"certificate.revoked","environment":"live","createdAt":"2026-11-13T12:00:00Z","data":{"id":"cert-1","status":"accepted"}}`, "*flashpi.CertificateEvent"},
		{`{"id":"e2","type":"account.email_paused","environment":"live","createdAt":"2026-11-13T12:00:00Z","data":{"environment":"live","status":"paused"}}`, "*flashpi.AccountEvent"},
		{`{"id":"e3","type":"invoice.rejected","environment":"test","createdAt":"2026-11-13T12:00:00Z"}`, "*flashpi.ThinEvent"},
		{`{"id":"e4","type":"invoice.rejected","environment":"test","createdAt":"2026-11-13T12:00:00Z","data":null}`, "*flashpi.ThinEvent"},
		{`{"id":"e5","type":"issuer.created","environment":"test","createdAt":"2026-11-13T12:00:00Z","data":{"x":1}}`, "*flashpi.UnknownEvent"},
		{` {"id":"e6","type":"document.email.sent","environment":"test","createdAt":"2026-11-13T12:00:00Z","data":{"id":"d"}} `, "*flashpi.DocumentEvent"},
	}
	for _, tc := range cases {
		body := []byte(tc.body)
		event, err := UnwrapWebhook(body, signedHeaders(body), webhookSecret, WithNow(webhookNow))
		if err != nil {
			t.Fatalf("%s: %v", tc.want, err)
		}
		if got := typeName(event); got != tc.want {
			t.Errorf("got %s, want %s", got, tc.want)
		}
		if event.Info().ID == "" {
			t.Errorf("%s: no id", tc.want)
		}
	}
	body := []byte(cases[0].body)
	event, _ := UnwrapWebhook(body, signedHeaders(body), webhookSecret, WithNow(webhookNow))
	verification := event.(*WebhookEndpointVerificationEvent)
	if verification.Data.Challenge != "abc" || verification.Info().Type != "webhook_endpoint.verification" {
		t.Errorf("got %+v", verification)
	}
	body = []byte(cases[5].body)
	event, _ = UnwrapWebhook(body, signedHeaders(body), webhookSecret, WithNow(webhookNow))
	unknown := event.(*UnknownEvent)
	if string(unknown.Data) != `{"x":1}` || unknown.Info().Type != "issuer.created" {
		t.Errorf("got %+v", unknown)
	}
	if _, isEvent := event.(Event); !isEvent {
		t.Error("an unknown event is not an Event")
	}
}

func typeName(v any) string {
	switch v.(type) {
	case *DocumentEvent:
		return "*flashpi.DocumentEvent"
	case *CertificateEvent:
		return "*flashpi.CertificateEvent"
	case *AccountEvent:
		return "*flashpi.AccountEvent"
	case *ThinEvent:
		return "*flashpi.ThinEvent"
	case *WebhookEndpointVerificationEvent:
		return "*flashpi.WebhookEndpointVerificationEvent"
	case *UnknownEvent:
		return "*flashpi.UnknownEvent"
	}
	return "?"
}

func TestUnwrapWebhookRefusesBodiesThatAreNotEvents(t *testing.T) {
	cases := map[string]string{
		`not json`: "not JSON",
		`[1,2]`:    "not a JSON object",
		`"text"`:   "not a JSON object",
		`null`:     "not a JSON object",
		`{"id":7,"type":"invoice.authorized","environment":"test","createdAt":"x","data":{}}`: "not an event",
	}
	for body, message := range cases {
		payload := []byte(body)
		_, err := UnwrapWebhook(payload, signedHeaders(payload), webhookSecret, WithNow(webhookNow))
		verification := mustAs[*WebhookVerificationError](t, err)
		if verification.Reason != WebhookInvalidPayload || !strings.Contains(verification.Message, message) {
			t.Errorf("%s: got %v", body, err)
		}
	}
}

func TestVerifyWebhookSignature(t *testing.T) {
	body := []byte(documentEventBody)
	if err := verify(body, signedHeaders(body), webhookSecret); err != nil {
		t.Fatal(err)
	}
	// The secret without its prefix, with spaces around it, or without padding.
	for _, secret := range []string{
		base64.StdEncoding.EncodeToString(webhookKey),
		"  " + webhookSecret + "\n",
		"whsec_" + base64.RawStdEncoding.EncodeToString(webhookKey),
	} {
		if err := verify(body, signedHeaders(body), secret); err != nil {
			t.Errorf("%q: %v", secret, err)
		}
	}
}

func TestAWebhookThatWasChanged(t *testing.T) {
	body := []byte(documentEventBody)
	header := signedHeaders(body)
	changed := []byte(strings.Replace(documentEventBody, "123", "124", 1))
	if reason := reasonOf(t, verify(changed, header, webhookSecret)); reason != WebhookNoMatchingSignature {
		t.Errorf("reason = %s", reason)
	}
	other := "whsec_" + base64.StdEncoding.EncodeToString([]byte("another key of thirty-two bytes!"))
	if reason := reasonOf(t, verify(body, header, other)); reason != WebhookNoMatchingSignature {
		t.Errorf("reason = %s", reason)
	}
	header.Set("Webhook-Id", "msg_2")
	if reason := reasonOf(t, verify(body, header, webhookSecret)); reason != WebhookNoMatchingSignature {
		t.Errorf("reason = %s", reason)
	}
}

func TestSignatureLists(t *testing.T) {
	body := []byte(documentEventBody)
	header := signedHeaders(body)
	good := header.Get("Webhook-Signature")
	bad := sign([]byte("another key of thirty-two bytes!"), "msg_1", header.Get("Webhook-Timestamp"), body)
	cases := map[string]bool{
		bad + " " + good:                             true, // a rotation: either secret
		"v2," + strings.TrimPrefix(good, "v1,"):      false,
		strings.TrimPrefix(good, "v1,"):              false,
		"v1," + strings.TrimPrefix(good, "v1,")[:20]: false,
		"v1,!!!! " + good:                            true,
		"  " + good:                                  true,
	}
	for signatures, valid := range cases {
		header.Set("Webhook-Signature", signatures)
		err := verify(body, header, webhookSecret)
		if (err == nil) != valid {
			t.Errorf("%q: %v", signatures, err)
		}
	}
	// Repeated headers are joined.
	header["Webhook-Signature"] = []string{bad, good}
	if err := verify(body, header, webhookSecret); err != nil {
		t.Errorf("repeated signature headers: %v", err)
	}
}

func TestWebhookHeadersInAnyCase(t *testing.T) {
	body := []byte(documentEventBody)
	signed := signedHeaders(body)
	header := http.Header{
		"webhook-id":        signed["Webhook-Id"],
		"WEBHOOK-TIMESTAMP": signed["Webhook-Timestamp"],
		"Webhook-Signature": signed["Webhook-Signature"],
	}
	if err := verify(body, header, webhookSecret); err != nil {
		t.Error(err)
	}
}

func TestMissingWebhookHeaders(t *testing.T) {
	body := []byte(documentEventBody)
	for _, name := range []string{"Webhook-Id", "Webhook-Timestamp", "Webhook-Signature"} {
		header := signedHeaders(body)
		header.Del(name)
		if reason := reasonOf(t, verify(body, header, webhookSecret)); reason != WebhookMissingHeaders {
			t.Errorf("%s: %s", name, reason)
		}
		header = signedHeaders(body)
		header.Set(name, "")
		if reason := reasonOf(t, verify(body, header, webhookSecret)); reason != WebhookMissingHeaders {
			t.Errorf("empty %s: %s", name, reason)
		}
	}
	if reason := reasonOf(t, verify(body, nil, webhookSecret)); reason != WebhookMissingHeaders {
		t.Errorf("nil header: %s", reason)
	}
}

func TestWebhookTimestamps(t *testing.T) {
	body := []byte(documentEventBody)
	at := func(timestamp string) http.Header {
		return http.Header{
			"Webhook-Id":        {"msg_1"},
			"Webhook-Timestamp": {timestamp},
			"Webhook-Signature": {sign(webhookKey, "msg_1", timestamp, body)},
		}
	}
	now := webhookNow.Unix()
	cases := []struct {
		timestamp string
		options   []WebhookOption
		reason    WebhookVerificationReason
	}{
		{strconv.FormatInt(now-300, 10), nil, ""},
		{strconv.FormatInt(now+300, 10), nil, ""},
		{strconv.FormatInt(now-301, 10), nil, WebhookTimestampOutOfRange},
		{strconv.FormatInt(now+301, 10), nil, WebhookTimestampOutOfRange},
		{strconv.FormatInt(now-10, 10), []WebhookOption{WithTolerance(5 * time.Second)}, WebhookTimestampOutOfRange},
		{strconv.FormatInt(now, 10), []WebhookOption{WithTolerance(0)}, ""},
		{strconv.FormatInt(now-1000, 10), []WebhookOption{WithTolerance(time.Hour)}, ""},
		{"1700000000.5", nil, WebhookInvalidTimestamp},
		{"-1700000000", nil, WebhookInvalidTimestamp},
		{"1234567890123", nil, WebhookInvalidTimestamp},
		{"abc", nil, WebhookInvalidTimestamp},
		{" 1700000000", nil, WebhookInvalidTimestamp},
	}
	for _, tc := range cases {
		err := verify(body, at(tc.timestamp), webhookSecret, tc.options...)
		if tc.reason == "" {
			if err != nil {
				t.Errorf("%s: %v", tc.timestamp, err)
			}
			continue
		}
		if reason := reasonOf(t, err); reason != tc.reason {
			t.Errorf("%s: %s, want %s", tc.timestamp, reason, tc.reason)
		}
	}
	err := verify(body, at(strconv.FormatInt(now-400, 10)), webhookSecret)
	if !strings.Contains(err.Error(), "more than 300 seconds away") {
		t.Errorf("message = %q", err)
	}
	// The clock is read when no time is given.
	header := at(strconv.FormatInt(time.Now().Unix(), 10))
	if err := VerifyWebhookSignature(body, header, webhookSecret); err != nil {
		t.Errorf("with the clock: %v", err)
	}
}

func TestANegativeToleranceIsRefused(t *testing.T) {
	body := []byte(documentEventBody)
	err := verify(body, signedHeaders(body), webhookSecret, WithTolerance(-time.Second))
	var verification *WebhookVerificationError
	if errors.As(err, &verification) {
		t.Fatal("a negative tolerance is a verification error")
	}
	if sdkErr := mustAs[*Error](t, err); !strings.Contains(sdkErr.Message, "tolerance") {
		t.Errorf("got %v", err)
	}
}

func TestWebhookSecrets(t *testing.T) {
	body := []byte(documentEventBody)
	header := signedHeaders(body)
	cases := map[string]string{
		"":              "empty",
		"   ":           "empty",
		"whsec_":        "not a whsec_ secret",
		"whsec_!!!!":    "not a whsec_ secret",
		"whsec_abc=d":   "not a whsec_ secret",
		"whsec_a":       "not a whsec_ secret",
		"whsec_YWJj===": "not a whsec_ secret",
		"whsec_" + base64.StdEncoding.EncodeToString(make([]byte, 23)): "not a complete whsec_ secret",
		"whsec_" + base64.StdEncoding.EncodeToString(make([]byte, 65)): "not a complete whsec_ secret",
	}
	for secret, message := range cases {
		err := verify(body, header, secret)
		verification := mustAs[*WebhookVerificationError](t, err)
		if verification.Reason != WebhookInvalidSecret || !strings.Contains(verification.Message, message) {
			t.Errorf("%q: %v", secret, err)
		}
		if len(secret) > 10 && strings.Contains(err.Error(), strings.TrimPrefix(secret, "whsec_")) {
			t.Errorf("the error shows the secret: %v", err)
		}
	}
	// The secret is checked before the headers.
	if reason := reasonOf(t, verify(body, nil, "")); reason != WebhookInvalidSecret {
		t.Errorf("reason = %s", reason)
	}
	for _, size := range []int{24, 64} {
		key := make([]byte, size)
		secret := "whsec_" + base64.StdEncoding.EncodeToString(key)
		timestamp := strconv.FormatInt(webhookNow.Unix(), 10)
		header := http.Header{"Webhook-Id": {"m"}, "Webhook-Timestamp": {timestamp}, "Webhook-Signature": {sign(key, "m", timestamp, body)}}
		if err := verify(body, header, secret); err != nil {
			t.Errorf("a %d-byte key: %v", size, err)
		}
	}
}

func TestDecodeBase64LikeAtob(t *testing.T) {
	cases := map[string]string{
		"YWJj":      "abc",
		"YWI=":      "ab",
		"YWI":       "ab",
		"YQ==":      "a",
		"YQ":        "a",
		"":          "",
		"YQ=":       "!",
		"Y":         "!",
		"YWJjZ":     "!",
		"YW=j":      "!",
		"YW_j":      "!",
		"Y===":      "!",
		"YWJj YQ==": "!",
	}
	for input, want := range cases {
		got, ok := decodeBase64(input)
		if want == "!" {
			if ok {
				t.Errorf("%q was decoded to %q", input, got)
			}
			continue
		}
		if !ok || string(got) != want {
			t.Errorf("%q: %q %v, want %q", input, got, ok, want)
		}
	}
}
