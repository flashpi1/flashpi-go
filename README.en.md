# Flashpi for Go

The official Go SDK for [Flashpi](https://flashpi.com.ec), the electronic invoicing API for
Ecuador's SRI.

[Español](https://github.com/flashpi1/flashpi-go/blob/main/README.md)

- The 45 operations of the API: the six document types, documents (XML, RIDE PDF, email),
  issuers with establishments, emission points and .p12 certificates, webhooks and events.
- Structs generated from the API's OpenAPI document, so your editor and the compiler know every
  field.
- A `context.Context` on every call, lists you walk with `range`, and errors you check with
  `errors.As`.
- Safe retries: every issuing request carries an `Idempotency-Key`, so a retry never issues a
  document twice.
- Typed errors (RFC 9457) with their `Code`, field errors and `RequestID`.
- Webhook signature verification (Standard Webhooks) with `net/http` or any framework.
- Go 1.26 or later. No dependencies: the standard library only.

## Install

```bash
go get github.com/flashpi1/flashpi-go
```

## Getting started

Create an API key in the [console](https://flashpi.com.ec/app/) and keep it in the
`FLASHPI_API_KEY` environment variable. An `fp_test_` key issues in the SRI's test environment;
an `fp_live_` key issues real documents in production.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/flashpi1/flashpi-go"
)

func main() {
	// Reads FLASHPI_API_KEY; you can also pass it: flashpi.NewClient(flashpi.WithAPIKey("fp_test_..."))
	client, err := flashpi.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	invoice, err := client.Invoices.Create(context.Background(), flashpi.CreateInvoice{
		IssuerID:      "<the demo issuer id or yours>",
		Establishment: "001",
		EmissionPoint: "001",
		Customer: flashpi.DocumentParty{
			IDType:  flashpi.IDTypeRUC,
			ID:      new("1760013210001"),
			Name:    new("PRUEBAS SERVICIO DE RENTAS INTERNAS"),
			Address: new("Av. Amazonas y Villalengua, Quito"),
		},
		Items: []flashpi.DocumentItem{{
			Code:        new("P-001"),
			Description: "Test service",
			Quantity:    "1",
			UnitPrice:   "1.00",
			Taxes:       []flashpi.DocumentTax{{Type: flashpi.TaxTypeVAT, Rate: "15"}},
		}},
		Payments: []flashpi.DocumentPayment{{Method: flashpi.PaymentMethodCash, Amount: "1.15"}},
		Options:  &flashpi.DocumentOptions{WaitForAuthorization: new(true)},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(invoice.Status, invoice.AccessKey)
}
```

Requests and answers are structs with the API's fields (`invoice.AccessKey` is `accessKey`).
Optional fields are pointers: `new("value")` makes one. Amounts and quantities are **strings**
(`"1.15"`), never numbers, so no decimals are lost.

With the **demo issuer** you can issue test documents without uploading a certificate: its id is
on the console's Overview (up to 50 documents a day). See
[Sandbox and demo issuer](https://flashpi.com.ec/docs/en/sandbox/).

A client keeps its connections open and is safe to use from several goroutines: create one and
reuse it.

## Issuing documents

| Document                                    | Method                                         |
| ------------------------------------------- | ---------------------------------------------- |
| Invoice (factura)                           | `client.Invoices.Create(ctx, body)`            |
| Credit note (nota de crédito)               | `client.CreditNotes.Create(ctx, body)`         |
| Debit note (nota de débito)                 | `client.DebitNotes.Create(ctx, body)`          |
| Purchase settlement (liquidación de compra) | `client.PurchaseSettlements.Create(ctx, body)` |
| Waybill (guía de remisión)                  | `client.Waybills.Create(ctx, body)`            |
| Withholding (comprobante de retención)      | `client.Withholdings.Create(ctx, body)`        |

They all return the document. By default the answer comes right away with the status
`processing`, and the SRI's outcome arrives by webhook. With
`Options: &flashpi.DocumentOptions{WaitForAuthorization: new(true)}` the API waits for the SRI
(15 s by default, up to 30 s with `WaitTimeoutSeconds`) and returns `authorized`, `rejected` or
`failed`; if the SRI takes longer, it returns `processing` or `pending_sri` and the document
carries on.

## Documents

```go
document, err := client.Documents.Get(ctx, invoice.ID)
if err != nil {
	return err
}
fmt.Println(document.Status)

xml, err := client.Documents.XML(ctx, invoice.ID)
if err != nil {
	return err
}
fmt.Println(xml.Kind) // flashpi.DocumentXMLKindAuthorized once the SRI authorized it

pdf, err := client.Documents.PDF(ctx, invoice.ID) // the RIDE, as bytes
if err != nil {
	return err
}
if err := os.WriteFile(invoice.AccessKey+".pdf", pdf, 0o644); err != nil {
	return err
}

// Emails the RIDE and the XML to the buyer again.
if _, err := client.Documents.SendEmail(ctx, invoice.ID, nil); err != nil {
	return err
}
```

## Lists and pagination

`List` walks every page with `range`, fetching them as you go. When a page fails, you get the
error in `err`:

```go
params := &flashpi.DocumentListParams{
	Status:        new(flashpi.DocumentStatusAuthorized),
	IssueDateFrom: new("2026-11-01"),
}
for document, err := range client.Documents.List(ctx, params) {
	if err != nil {
		return err
	}
	fmt.Println(document.AccessKey)
}
```

`ListPage` returns one page at a time, and `NextPage` the next one (`nil` after the last):

```go
page, err := client.Documents.ListPage(ctx, &flashpi.DocumentListParams{Limit: new(50)})
for page != nil && err == nil {
	fmt.Println(len(page.Data)) // the documents of this page
	page, err = page.NextPage(ctx)
}
if err != nil {
	return err
}
```

Filters use the API's names (`IssueDateFrom`, `IssuerID`), and `nil` means no filters.

## Errors

Every error of the API is a `*flashpi.APIError`, wrapped in a type for its HTTP status. Check it
with `errors.As` and branch on `Code`, which is stable and in English; `Message` follows the
requested language.

```go
_, err := client.Invoices.Create(ctx, body)

var invalid *flashpi.UnprocessableEntityError
var apiErr *flashpi.APIError
switch {
case errors.As(err, &invalid):
	fmt.Println(invalid.Code) // "validation_failed", "total_mismatch", ...
	for _, field := range invalid.Errors {
		fmt.Println(field.Field, field.Message)
	}
case errors.As(err, &apiErr):
	fmt.Println(apiErr.Status, apiErr.Code, apiErr.RequestID) // share RequestID with support
case err != nil:
	return err // no answer from the API: *flashpi.ConnectionError, *flashpi.TimeoutError, ...
}
```

| Type                       | Status | Examples of `Code`                                          |
| -------------------------- | ------ | ----------------------------------------------------------- |
| `BadRequestError`          | 400    | `malformed_json`, `invalid_idempotency_key`                 |
| `AuthenticationError`      | 401    | `authentication_required`, `invalid_api_key`                |
| `PermissionDeniedError`    | 403    | `key_read_only`, `key_scope_mismatch`, `ip_not_allowed`     |
| `NotFoundError`            | 404    | `not_found`                                                 |
| `ConflictError`            | 409    | `sequential_already_used`, `ride_not_available`             |
| `UnprocessableEntityError` | 422    | `validation_failed`, `total_mismatch`                       |
| `RateLimitError`           | 429    | `demo_issuer_quota_exceeded`, `account_email_limit_reached` |
| `InternalServerError`      | 5xx    | `internal_error`                                            |
| `ConnectionError`          | —      | no answer (network); `TimeoutError` when time ran out       |

Every one of them can also be read as a `*flashpi.Error` with `errors.As`. Some errors carry
extra data in `apiErr.Problem`, such as `DocumentID`, `Limit`, `ResetsAt` or `RIDEStatus`. For
messages in Spanish: `flashpi.NewClient(flashpi.WithLanguage(flashpi.LanguageSpanish))`.

## Retries and idempotency

The SDK retries network errors, timeouts, and 408, 429 (when asked to wait 60 s or less) and
5xx answers up to 2 times, with growing waits. Every POST carries an `Idempotency-Key` that its
retries repeat, so the API never issues twice:

- When a retry finds that the first attempt did create the document, the SDK returns that
  document.
- When the first attempt is still running on the API (for example, the connection dropped while
  it waited for the SRI), the SDK waits for it with the same key, up to the call's time limit,
  without using up retries.

To use your own key (for example your order id), pass it to the call:

```go
_, err := client.Invoices.Create(ctx, body, flashpi.WithIdempotencyKey("order-"+order.ID))
if err != nil {
	return err
}
```

If issuing still fails, the error carries the key it used in `IdempotencyKey`. Trying again with
`flashpi.WithIdempotencyKey(key)` never issues a second document: within 24 hours the API
returns the first answer again; after that (or if the API lost track of the first attempt), the
call fails with a 422 `idempotency_key_reused` whose `Problem.DocumentID` is the existing
document (read it with `client.Documents.Get`).

```go
_, err := client.Invoices.Create(ctx, body)
var flashpiErr *flashpi.Error
if errors.As(err, &flashpiErr) && flashpiErr.IdempotencyKey != "" {
	saveForRetry(body, flashpiErr.IdempotencyKey) // your retry queue
}
```

The `ctx` bounds the whole call, retries included. When it is cancelled or its deadline passes,
the call stops without retrying and returns a `*flashpi.Error` with its key, for which
`errors.Is(err, context.Canceled)` (or `context.DeadlineExceeded`) is true.

Daily limits (such as the demo issuer's quota) are not retried: they come as a
`*flashpi.RateLimitError`, whose `RetryAfter()` says how long to wait.

## Webhooks

Verify every webhook with the body **exactly as received** (not parsed) and the endpoint's
`whsec_` secret. `flashpi.UnwrapWebhook` needs no API key and does no I/O, and returns the event:
use a type switch.

```go
package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/flashpi1/flashpi-go"
)

func main() {
	http.HandleFunc("POST /webhooks/flashpi", flashpiWebhook)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func flashpiWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	event, err := flashpi.UnwrapWebhook(body, r.Header, os.Getenv("FLASHPI_WEBHOOK_SECRET"))
	var invalid *flashpi.WebhookVerificationError
	if errors.As(err, &invalid) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	switch e := event.(type) {
	case *flashpi.WebhookEndpointVerificationEvent:
		// The endpoint's activation: answer the challenge.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge": e.Data.Challenge})
		return
	case *flashpi.DocumentEvent: // e.Data is the document
		switch e.Type {
		case flashpi.DocumentEventTypeInvoiceAuthorized:
			log.Println("Authorized", e.Data.AccessKey)
		case flashpi.DocumentEventTypeInvoiceRejected:
			log.Println("Rejected", e.Data.SRIMessages)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
```

With Gin, Echo, chi or Fiber, pass the raw body and the request's headers the same way.

Store `event.Info().ID` and skip repeats: an event can arrive more than once. During a secret
rotation Flashpi signs with both the new and the previous secret, so either works. If the
endpoint uses `thin` mode (no `data`), the event is a `*flashpi.ThinEvent`: fetch the whole event
with `client.Events.Get(ctx, e.ID)`. An event type this version does not know comes as a
`*flashpi.UnknownEvent`.

## Issuers and certificates

These operations need an account-wide key with read and write access.

An issuer starts from its electronic signature (.p12): Flashpi checks it, reads the RUC and asks
the SRI, and gives you a `Prefill` with what it found so you can build the issuer.

```go
p12, err := os.ReadFile("firma.p12")
if err != nil {
	return err
}
onboarding, err := client.IssuerOnboardings.Create(ctx, flashpi.CertificateUpload{
	File:     bytes.NewReader(p12),
	Password: os.Getenv("P12_PASSWORD"),
})
for err == nil && onboarding.Status == flashpi.IssuerOnboardingStatusVerifying {
	time.Sleep(2 * time.Second)
	onboarding, err = client.IssuerOnboardings.Get(ctx, onboarding.ID)
}
if err != nil {
	return err
}
if onboarding.Status != flashpi.IssuerOnboardingStatusReady {
	return fmt.Errorf("the signature cannot be used: %v", onboarding.Rejection)
}

var completed *flashpi.CompletedIssuerOnboarding
if onboarding.ExistingIssuer != nil {
	// You already have that RUC: the certificate is added to that issuer (a renewal).
	completed, err = client.IssuerOnboardings.Complete(ctx, onboarding.ID, nil)
} else {
	// A new RUC: you confirm the issuer's data, starting from the prefill.
	issuer := flashpi.CompleteIssuerOnboarding{
		LegalName:         new("MY COMPANY S.A."),
		HeadOfficeAddress: new("Av. Amazonas N24-100, Quito"),
		TaxProfile:        &flashpi.IssuerTaxProfileInput{KeepsAccounting: false},
		// When onboarding.RUCNeedsConfirmation is true, send RUC too.
	}
	if prefill := onboarding.Prefill; prefill != nil {
		issuer.LegalName = cmp.Or(prefill.LegalName, issuer.LegalName)
		issuer.HeadOfficeAddress = cmp.Or(prefill.HeadOfficeAddress, issuer.HeadOfficeAddress)
		issuer.TaxProfile.KeepsAccounting = prefill.TaxProfile.KeepsAccounting != nil && *prefill.TaxProfile.KeepsAccounting
		// For RIMPE, a special taxpayer or a withholding agent, add that data.
	}
	completed, err = client.IssuerOnboardings.Complete(ctx, onboarding.ID, &issuer)
}
if err != nil {
	return err
}

for establishment, err := range client.Issuers.Establishments.List(ctx, completed.Issuer.ID, nil) {
	if err != nil {
		return err
	}
	fmt.Println(establishment.Code)
}
```

The `File` is an `io.Reader` (the bytes of the .p12 with `bytes.NewReader`, or an open
`*os.File`) of up to 1 MiB. The SDK reads it once, before sending anything, and does not close
it.

To **renew an issuer's signature** for test and live mode at once, run the same flow with the
new .p12: Flashpi recognizes the RUC (`ExistingIssuer`) and `Complete` with no data adds the
certificate. `client.Issuers.Certificates.Upload(ctx, issuerID, flashpi.CertificateUpload{...})`,
instead, uploads a certificate for the mode of the key you use only (test or live).

## Optional fields and null

Optional fields are pointers (`nil` is not sent) and `new(value)` makes them. In updates, the
fields you can clear are `flashpi.Nullable`: `flashpi.Null` sends `null` (clears the field),
`flashpi.NullableOf` sends a value, and an unset one is not sent.

```go
_, err := client.Issuers.Update(ctx, issuerID, flashpi.UpdateIssuer{
	TradeName:          flashpi.Null[string](), // clears the trade name
	SendEmailByDefault: new(false),
	Branding:           &flashpi.IssuerBrandingInput{BrandColor: flashpi.NullableOf("#0C160D")},
})
if err != nil {
	return err
}
```

## Options

```go
client, err := flashpi.NewClient(
	flashpi.WithAPIKey(os.Getenv("FLASHPI_API_KEY")), // defaults to the FLASHPI_API_KEY variable
	flashpi.WithLanguage(flashpi.LanguageSpanish),    // language of the messages: English (default) or Spanish
	flashpi.WithTimeout(60*time.Second),              // per attempt (at least the SRI wait + 15 s with waitForAuthorization)
	flashpi.WithMaxRetries(2),                        // 0 turns retries off
)
if err != nil {
	return err
}
fmt.Println(client) // shows the environment and the URL, never the key
```

Every method also takes, last, the options of that call: `flashpi.WithIdempotencyKey`,
`flashpi.WithTimeout`, `flashpi.WithMaxRetries`, `flashpi.WithLanguage`,
`flashpi.WithCorrelationID` (your own id, which Flashpi logs) and `flashpi.WithHeader`.

```go
document, err := client.Documents.Get(ctx, documentID,
	flashpi.WithTimeout(10*time.Second),
	flashpi.WithLanguage(flashpi.LanguageSpanish),
	flashpi.WithCorrelationID("order-42"),
)
if err != nil {
	return err
}
fmt.Println(document.Status)
```

To see the HTTP response and the request id, pass `flashpi.WithResponseInto`:

```go
var response flashpi.Response
document, err := client.Documents.Get(ctx, documentID, flashpi.WithResponseInto(&response))
if err != nil {
	return err
}
fmt.Println(document.Status, response.StatusCode, response.RequestID)
```

To send the requests through your own `*http.Client` (a proxy, a test transport), pass it with
`flashpi.WithHTTPClient`. The SDK never follows redirects.

```go
proxy, err := url.Parse("http://proxy.internal:3128")
if err != nil {
	return err
}
client, err := flashpi.NewClient(flashpi.WithHTTPClient(&http.Client{
	Transport: &http.Transport{Proxy: http.ProxyURL(proxy)},
}))
if err != nil {
	return err
}
fmt.Println(client.Environment())
```

In tests, an `httptest.Server` (with `flashpi.WithBaseURL`) or a `http.RoundTripper` of your own
answers in place of the API.

## Servers only

Your API key is a secret: use the SDK on your servers, never in code that runs on your users'
devices. `fmt.Println(client)` shows the environment and the URL, never the key.

## License

[MIT](https://github.com/flashpi1/flashpi-go/blob/main/LICENSE) © 2026 SIGNIADIGITAL S.A.
