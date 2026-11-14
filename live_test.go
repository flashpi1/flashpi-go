//go:build live

// The SDK against the real Flashpi API in test mode (SRI pruebas), port of the Node SDK's
// test/live/live.test.ts:
//
//	go test -tags live -run TestLive -v .
//
// It needs FLASHPI_TEST_API_KEY (an fp_test_ key with read_write access) and
// FLASHPI_DEMO_ISSUER_ID (the demo issuer's id, on the console's Overview); FLASHPI_BASE_URL may
// point to another API. Without them it is skipped, and without the live build tag it is not
// even built. It issues ONE real test invoice on the shared demo issuer, which counts toward its
// daily quota of 50, so CI only runs it while a release is being prepared. It never accepts an
// fp_live_ key.

package flashpi_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/flashpi1/flashpi-go"
)

// polls are the checks of a slow SRI step, 5 seconds apart: up to 2 minutes.
const polls = 24

func sampleInvoice(issuerID string) flashpi.CreateInvoice {
	// The same test invoice as the docs' quickstart: USD 1.00 + 15 % VAT to the SRI's test RUC.
	return flashpi.CreateInvoice{
		IssuerID:      issuerID,
		Establishment: "001",
		EmissionPoint: "001",
		Customer: flashpi.DocumentParty{
			IDType:  flashpi.IDTypeRUC,
			ID:      new("1760013210001"),
			Name:    new("PRUEBAS SERVICIO DE RENTAS INTERNAS"),
			Address: new("AMAZONAS AVENUE AND VILLALENGUA, QUITO"),
		},
		Items: []flashpi.DocumentItem{{
			Code:        new("SDK-LIVE-001"),
			Description: "flashpi-go live test",
			Quantity:    "1",
			UnitPrice:   "1.00",
			Taxes:       []flashpi.DocumentTax{{Type: flashpi.TaxTypeVAT, Rate: "15"}},
		}},
		Payments: []flashpi.DocumentPayment{{Method: flashpi.PaymentMethodCash, Amount: "1.15"}},
		Options:  &flashpi.DocumentOptions{WaitForAuthorization: new(true), WaitTimeoutSeconds: new(30), SendEmail: new(false)},
	}
}

func TestLive(t *testing.T) {
	key := os.Getenv("FLASHPI_TEST_API_KEY")
	issuerID := os.Getenv("FLASHPI_DEMO_ISSUER_ID")
	if key == "" || issuerID == "" {
		t.Skip("needs FLASHPI_TEST_API_KEY and FLASHPI_DEMO_ISSUER_ID")
	}
	if len(key) < 8 || key[:8] != "fp_test_" {
		t.Fatal("FLASHPI_TEST_API_KEY must be an fp_test_ key: this suite never runs live")
	}
	options := []flashpi.ClientOption{flashpi.WithAPIKey(key)}
	if baseURL := os.Getenv("FLASHPI_BASE_URL"); baseURL != "" {
		options = append(options, flashpi.WithBaseURL(baseURL))
	}
	client, err := flashpi.NewClient(options...)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	idempotencyKey := fmt.Sprintf("sdk-live-go-%x-%d", time.Now().Unix(), time.Now().UnixNano())
	var invoice *flashpi.Document

	t.Run("reads the API status", func(t *testing.T) {
		status, err := client.Status.Get(ctx)
		if err != nil || status.Status != flashpi.StatusStatusOperational {
			t.Fatalf("got %v %v", status, err)
		}
	})

	t.Run("rejects an invalid invoice with field errors in Spanish", func(t *testing.T) {
		body := sampleInvoice(issuerID)
		body.Items = []flashpi.DocumentItem{}
		_, err := client.Invoices.Create(ctx, body, flashpi.WithLanguage(flashpi.LanguageSpanish), flashpi.WithMaxRetries(0))
		var invalid *flashpi.UnprocessableEntityError
		if !errors.As(err, &invalid) {
			t.Fatalf("got %v", err)
		}
		if invalid.Code != "validation_failed" || len(invalid.Errors) == 0 || !regexp.MustCompile(`^[0-9a-f-]{36}$`).MatchString(invalid.RequestID) {
			t.Errorf("got %+v", invalid.APIError)
		}
		if invalid.Header.Get("Content-Language") != "es" {
			t.Errorf("Content-Language = %q", invalid.Header.Get("Content-Language"))
		}
	})

	t.Run("issues a test invoice and waits for the SRI", func(t *testing.T) {
		var response flashpi.Response
		document, err := client.Invoices.Create(ctx, sampleInvoice(issuerID),
			flashpi.WithIdempotencyKey(idempotencyKey),
			flashpi.WithCorrelationID("flashpi-go-live"),
			flashpi.WithResponseInto(&response),
		)
		if err != nil {
			t.Fatal(err)
		}
		if (response.StatusCode != 200 && response.StatusCode != 202) || response.RequestID == "" {
			t.Errorf("response = %+v", response)
		}
		if document.Type != flashpi.DocumentTypeInvoice || document.Environment != flashpi.EnvironmentTest {
			t.Errorf("got %s %s", document.Type, document.Environment)
		}
		// The SRI's test environment can be slow: follow the document for up to 2 minutes.
		for range polls {
			if document.Status != flashpi.DocumentStatusProcessing && document.Status != flashpi.DocumentStatusPendingSRI {
				break
			}
			time.Sleep(5 * time.Second)
			if document, err = client.Documents.Get(ctx, document.ID); err != nil {
				t.Fatal(err)
			}
		}
		if document.Status != flashpi.DocumentStatusAuthorized || !regexp.MustCompile(`^\d{49}$`).MatchString(document.AccessKey) {
			t.Fatalf("got %s %s", document.Status, document.AccessKey)
		}
		invoice = document
	})
	if invoice == nil {
		t.Fatal("the test invoice was not issued")
	}

	t.Run("replays the same Idempotency-Key instead of issuing again", func(t *testing.T) {
		var response flashpi.Response
		document, err := client.Invoices.Create(ctx, sampleInvoice(issuerID), flashpi.WithIdempotencyKey(idempotencyKey), flashpi.WithResponseInto(&response))
		if err != nil || document.ID != invoice.ID || response.Header.Get("Idempotent-Replayed") != "true" {
			t.Errorf("got %v %v %v", document, err, response.Header)
		}
	})

	t.Run("finds the document by its access key", func(t *testing.T) {
		page, err := client.Documents.ListPage(ctx, &flashpi.DocumentListParams{AccessKey: new(invoice.AccessKey), Limit: new(5)})
		if err != nil || len(page.Data) != 1 || page.Data[0].ID != invoice.ID {
			t.Errorf("got %v %v", page, err)
		}
	})

	t.Run("downloads the authorized XML", func(t *testing.T) {
		xml, err := client.Documents.XML(ctx, invoice.ID)
		if err != nil || xml.Kind != flashpi.DocumentXMLKindAuthorized || !bytes.Contains([]byte(xml.XML), []byte(invoice.AccessKey)) {
			t.Errorf("got %v %v", xml, err)
		}
	})

	t.Run("downloads the RIDE once it is generated", func(t *testing.T) {
		var pdf []byte
		for range polls {
			pdf, err = client.Documents.PDF(ctx, invoice.ID)
			var conflict *flashpi.ConflictError
			if errors.As(err, &conflict) && conflict.Code == "ride_not_available" {
				time.Sleep(5 * time.Second)
				continue
			}
			break
		}
		if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
			t.Errorf("got %d bytes: %v", len(pdf), err)
		}
	})

	t.Run("lists the events of the document", func(t *testing.T) {
		var types []flashpi.EventType
		for poll := range 12 {
			if slices.Contains(types, flashpi.EventTypeInvoiceAuthorized) {
				break
			}
			if poll > 0 {
				time.Sleep(5 * time.Second)
			}
			types = types[:0]
			for event, err := range client.Events.List(ctx, &flashpi.EventListParams{DocumentID: new(invoice.ID)}) {
				if err != nil {
					t.Fatal(err)
				}
				types = append(types, event.Info().Type)
				if document, ok := event.(*flashpi.DocumentEvent); !ok || document.Data.ID != invoice.ID {
					t.Errorf("got %#v", event)
				}
			}
		}
		if !slices.Contains(types, flashpi.EventTypeInvoiceAuthorized) {
			t.Errorf("types = %v", types)
		}
	})

	t.Run("answers 404 for a document that does not exist", func(t *testing.T) {
		_, err := client.Documents.Get(ctx, "01a00000-0000-7000-8000-000000000000", flashpi.WithMaxRetries(0))
		var notFound *flashpi.NotFoundError
		if !errors.As(err, &notFound) {
			t.Errorf("got %v", err)
		}
	})
}
