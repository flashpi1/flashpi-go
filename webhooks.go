package flashpi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultWebhookTolerance is the largest gap between a webhook's timestamp and the receiver's
// clock that UnwrapWebhook and VerifyWebhookSignature accept by default.
const DefaultWebhookTolerance = 300 * time.Second

const (
	secretPrefix   = "whsec_"
	minSecretBytes = 24 // key sizes of a Standard Webhooks secret (Flashpi makes 32-byte keys)
	maxSecretBytes = 64
	signatureBytes = 32
)

// WebhookOption configures UnwrapWebhook and VerifyWebhookSignature.
type WebhookOption func(*webhookSettings)

type webhookSettings struct {
	tolerance time.Duration
	now       time.Time
}

// WithTolerance sets the largest gap between a webhook's timestamp and now (default 5 minutes).
func WithTolerance(tolerance time.Duration) WebhookOption {
	return func(s *webhookSettings) { s.tolerance = tolerance }
}

// WithNow sets the current time, for tests (default: the clock).
func WithNow(now time.Time) WebhookOption {
	return func(s *webhookSettings) { s.now = now }
}

// UnwrapWebhook verifies a webhook (see VerifyWebhookSignature) and returns its event. It needs
// no API key and does no I/O.
//
// Pass the raw body exactly as received, before any JSON parsing, and the request's headers.
// When it fails with a *WebhookVerificationError, answer 400 and ignore the request. Narrow the
// event with a type switch (see WebhookEvent):
//
//	event, err := flashpi.UnwrapWebhook(body, r.Header, os.Getenv("FLASHPI_WEBHOOK_SECRET"))
func UnwrapWebhook(payload []byte, header http.Header, secret string, options ...WebhookOption) (WebhookEvent, error) {
	if err := VerifyWebhookSignature(payload, header, secret, options...); err != nil {
		return nil, err
	}
	if !json.Valid(payload) {
		return nil, &WebhookVerificationError{Reason: WebhookInvalidPayload, Message: "The webhook body is not JSON"}
	}
	if trimmed := strings.TrimLeft(string(payload), " \t\r\n"); !strings.HasPrefix(trimmed, "{") {
		return nil, &WebhookVerificationError{Reason: WebhookInvalidPayload, Message: "The webhook body is not a JSON object"}
	}
	event, err := decodeWebhookEvent(payload)
	if err != nil {
		return nil, &WebhookVerificationError{Reason: WebhookInvalidPayload, Message: "The webhook body is not an event this version of the SDK can read: " + err.Error(), Err: err}
	}
	return event, nil
}

// VerifyWebhookSignature checks that a webhook comes from Flashpi and is recent (Standard
// Webhooks). A webhook is signed with HMAC-SHA256 over webhook-id.webhook-timestamp.body using
// the endpoint's whsec_ secret; during a secret rotation Flashpi signs with both secrets, and
// either matches.
//
// It returns a *WebhookVerificationError when the webhook must be refused (answer 400). Pass
// the raw body exactly as received: a body parsed and serialized again does not match. A
// negative tolerance is refused with an *Error, never by skipping the time check.
func VerifyWebhookSignature(payload []byte, header http.Header, secret string, options ...WebhookOption) error {
	settings := webhookSettings{tolerance: DefaultWebhookTolerance}
	for _, o := range options {
		if o != nil {
			o(&settings)
		}
	}
	if settings.tolerance < 0 {
		return &Error{Message: "The webhook tolerance must be 0 or more"}
	}
	if settings.now.IsZero() {
		settings.now = time.Now()
	}
	key, err := signingKey(secret)
	if err != nil {
		return err
	}
	webhookID := headerOf(header, "webhook-id")
	timestamp := headerOf(header, "webhook-timestamp")
	signatures := headerOf(header, "webhook-signature")
	if webhookID == "" || timestamp == "" || signatures == "" {
		return &WebhookVerificationError{Reason: WebhookMissingHeaders, Message: "The webhook-id, webhook-timestamp and webhook-signature headers are required"}
	}
	if len(timestamp) > 12 || strings.Trim(timestamp, "0123456789") != "" {
		return &WebhookVerificationError{Reason: WebhookInvalidTimestamp, Message: "The webhook-timestamp is not valid"}
	}
	sent, _ := strconv.ParseInt(timestamp, 10, 64)
	gap := math.Abs(float64(settings.now.Unix() - sent))
	// Written so that anything unexpected fails closed.
	if !(gap <= settings.tolerance.Seconds()) {
		return &WebhookVerificationError{
			Reason:  WebhookTimestampOutOfRange,
			Message: "The webhook-timestamp is more than " + strconv.FormatFloat(settings.tolerance.Seconds(), 'g', -1, 64) + " seconds away from now",
		}
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(webhookID + "." + timestamp + "."))
	mac.Write(payload)
	expected := mac.Sum(nil)
	for _, entry := range strings.Split(signatures, " ") {
		version, encoded, found := strings.Cut(entry, ",")
		if !found || version != "v1" {
			continue
		}
		signature, ok := decodeBase64(encoded)
		if ok && len(signature) == signatureBytes && hmac.Equal(signature, expected) {
			return nil
		}
	}
	return &WebhookVerificationError{Reason: WebhookNoMatchingSignature, Message: "No webhook-signature matches the body and the secret"}
}

// signingKey is the decoded key of a whsec_ secret.
func signingKey(secret string) ([]byte, error) {
	trimmed := strings.TrimSpace(secret)
	if trimmed == "" {
		return nil, &WebhookVerificationError{Reason: WebhookInvalidSecret, Message: "The webhook secret is empty: copy it from the console or client.WebhookEndpoints.GetSecret"}
	}
	key, ok := decodeBase64(strings.TrimPrefix(trimmed, secretPrefix))
	if ok && minSecretBytes <= len(key) && len(key) <= maxSecretBytes {
		return key, nil
	}
	if !ok || len(key) == 0 {
		return nil, &WebhookVerificationError{Reason: WebhookInvalidSecret, Message: "The webhook secret is not a whsec_ secret (base64 after the prefix)"}
	}
	return nil, &WebhookVerificationError{Reason: WebhookInvalidSecret, Message: "The webhook secret is not a complete whsec_ secret: copy it again from the console or client.WebhookEndpoints.GetSecret"}
}

// headerOf reads a header whatever the case of its name, also in a map whose names are not in
// canonical form. Repeated headers: the signature lists are joined, otherwise the first counts.
func headerOf(header http.Header, name string) string {
	var values []string
	for key, list := range header {
		if strings.EqualFold(key, name) {
			values = append(values, list...)
		}
	}
	if len(values) == 0 {
		return ""
	}
	if name == "webhook-signature" {
		return strings.Join(values, " ")
	}
	return values[0]
}

// decodeBase64 decodes standard base64 with optional padding, as atob does, refusing anything
// else.
func decodeBase64(value string) ([]byte, bool) {
	data := value
	if len(value)%4 == 0 {
		data = strings.TrimRight(value, "=")
		if len(value)-len(data) > 2 {
			return nil, false
		}
	}
	if len(data)%4 == 1 {
		return nil, false
	}
	for i := 0; i < len(data); i++ {
		c := data[i]
		if !('A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '+' || c == '/') {
			return nil, false
		}
	}
	decoded, err := base64.RawStdEncoding.DecodeString(data)
	if err != nil {
		return nil, false
	}
	return decoded, true
}
