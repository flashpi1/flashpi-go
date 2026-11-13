package flashpi

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultBaseURL is the address of the Flashpi API.
const DefaultBaseURL = "https://api.flashpi.com.ec"

// DefaultTimeout is how long each attempt of a request may take by default, from sending it to
// reading the whole answer.
const DefaultTimeout = 60 * time.Second

// DefaultMaxRetries is how many times a request is retried by default.
const DefaultMaxRetries = 2

// Client is the Flashpi API client. Create one with NewClient and reuse it: it is safe for
// concurrent use and keeps its connections open.
//
// An fp_test_ key issues in the SRI test environment (pruebas); an fp_live_ key issues real
// documents in production. Use it on servers only: the key must stay secret.
type Client struct {
	// Invoices issues invoices (facturas).
	Invoices *InvoiceService

	// CreditNotes issues credit notes (notas de crédito).
	CreditNotes *CreditNoteService

	// DebitNotes issues debit notes (notas de débito).
	DebitNotes *DebitNoteService

	// PurchaseSettlements issues purchase settlements (liquidaciones de compra).
	PurchaseSettlements *PurchaseSettlementService

	// Waybills issues waybills (guías de remisión).
	Waybills *WaybillService

	// Withholdings issues withholding receipts (comprobantes de retención).
	Withholdings *WithholdingService

	// Documents reads every issued document: search, XML, RIDE (PDF) and email.
	Documents *DocumentService

	// Issuers manages the issuers (RUCs) with their establishments, emission points and
	// certificates.
	Issuers *IssuerService

	// IssuerOnboardings creates an issuer from its .p12 certificate.
	IssuerOnboardings *IssuerOnboardingService

	// WebhookEndpoints manages the URLs Flashpi sends events to.
	WebhookEndpoints *WebhookEndpointService

	// Events reads the events of documents, certificates and email sending.
	Events *EventService

	// Status reads the API's status.
	Status *StatusService

	engine *engine
}

// NewClient creates a client. Without WithAPIKey it reads the FLASHPI_API_KEY environment
// variable. It fails on a missing or malformed key and on settings it cannot use, before
// sending anything; the error never shows the key.
func NewClient(options ...ClientOption) (*Client, error) {
	var settings clientSettings
	for _, o := range options {
		if o != nil {
			o.applyToClient(&settings)
		}
	}
	// The key is checked first, so a missing or bad key is the error reported.
	var key string
	if settings.apiKey != nil {
		key = *settings.apiKey
	} else {
		key = os.Getenv("FLASHPI_API_KEY")
	}
	key = strings.TrimFunc(key, jsSpace)
	if problem := apiKeyProblem(key); problem != "" {
		return nil, &Error{Message: problem}
	}
	e := &engine{
		apiKey:     key,
		baseURL:    DefaultBaseURL,
		timeout:    DefaultTimeout,
		maxRetries: DefaultMaxRetries,
		now:        time.Now,
		sleep:      sleepContext,
		random:     rand.Float64,
		newKey:     newUUID,
	}
	headers, err := checkHeaders("WithHeader", settings.headers)
	if err != nil {
		return nil, err
	}
	for _, h := range headers {
		if strings.EqualFold(h.name, "Idempotency-Key") {
			return nil, &Error{Message: "A client cannot send an Idempotency-Key with every request: it must be unique per operation. Pass flashpi.WithIdempotencyKey to each call instead"}
		}
	}
	e.headers = headers
	if settings.language != nil {
		language, err := headerValue("The language", string(*settings.language))
		if err != nil {
			return nil, err
		}
		e.language = Language(language)
	}
	if settings.baseURL != nil {
		if !isHTTPURL(*settings.baseURL) {
			return nil, &Error{Message: fmt.Sprintf("The base URL must be an http:// or https:// URL, got %q", *settings.baseURL)}
		}
		e.baseURL = *settings.baseURL
	}
	e.baseURL = strings.TrimRight(e.baseURL, "/")
	if settings.timeout != nil {
		if err := validTimeout(*settings.timeout); err != nil {
			return nil, err
		}
		e.timeout = *settings.timeout
	}
	if settings.maxRetries != nil {
		if err := validRetries(*settings.maxRetries); err != nil {
			return nil, err
		}
		e.maxRetries = *settings.maxRetries
	}
	if settings.httpClient != nil {
		// A copy, so the caller's client keeps its own redirect policy.
		client := *settings.httpClient
		client.CheckRedirect = stopAtRedirects
		e.http = &client
	} else {
		e.http = &http.Client{CheckRedirect: stopAtRedirects}
	}
	if strings.HasPrefix(key, "fp_test_") {
		e.environment = EnvironmentTest
	} else {
		e.environment = EnvironmentLive
	}
	return newClient(e), nil
}

func newClient(e *engine) *Client {
	return &Client{
		Invoices:            &InvoiceService{issuer{e, "/v1/invoices"}},
		CreditNotes:         &CreditNoteService{issuer{e, "/v1/credit-notes"}},
		DebitNotes:          &DebitNoteService{issuer{e, "/v1/debit-notes"}},
		PurchaseSettlements: &PurchaseSettlementService{issuer{e, "/v1/purchase-settlements"}},
		Waybills:            &WaybillService{issuer{e, "/v1/waybills"}},
		Withholdings:        &WithholdingService{issuer{e, "/v1/withholdings"}},
		Documents:           &DocumentService{e},
		Issuers: &IssuerService{
			engine: e,
			Establishments: &EstablishmentService{
				engine:         e,
				EmissionPoints: &EmissionPointService{e},
			},
			Certificates: &CertificateService{e},
		},
		IssuerOnboardings: &IssuerOnboardingService{e},
		WebhookEndpoints: &WebhookEndpointService{
			engine:     e,
			Deliveries: &WebhookDeliveryService{e},
		},
		Events: &EventService{e},
		Status: &StatusService{e},
		engine: e,
	}
}

// Environment is EnvironmentTest for an fp_test_ key (SRI pruebas) and EnvironmentLive for an
// fp_live_ key (SRI production).
func (c *Client) Environment() Environment { return c.engine.environment }

// String describes the client without its API key.
func (c *Client) String() string {
	return fmt.Sprintf("flashpi.Client{Environment: %q, BaseURL: %q}", c.engine.environment, c.engine.baseURL)
}

// GoString describes the client without its API key, for %#v.
func (c *Client) GoString() string { return c.String() }

func stopAtRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func isHTTPURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// sleepContext waits for d, or until ctx ends.
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
