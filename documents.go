package flashpi

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"time"
)

const (
	// defaultSyncWait is how long the API waits for the SRI in sync mode when
	// waitTimeoutSeconds is not sent.
	defaultSyncWait = 15 * time.Second

	// syncMargin is the time on top of the sync wait for Flashpi to build, sign and answer.
	syncMargin = 15 * time.Second
)

// syncTimeoutFloor is the shortest per-attempt timeout for issuing a body. In sync mode
// (options.waitForAuthorization) the API holds the request for up to waitTimeoutSeconds (15 by
// default) while the SRI decides, so each attempt waits at least that plus 15 seconds.
// Otherwise 0: the client's timeout applies.
func syncTimeoutFloor(content []byte) time.Duration {
	var body struct {
		Options *struct {
			WaitForAuthorization json.RawMessage `json:"waitForAuthorization"`
			WaitTimeoutSeconds   json.RawMessage `json:"waitTimeoutSeconds"`
		} `json:"options"`
	}
	if json.Unmarshal(content, &body) != nil || body.Options == nil || string(body.Options.WaitForAuthorization) != "true" {
		return 0
	}
	wait := defaultSyncWait.Seconds()
	var seconds float64
	if raw := body.Options.WaitTimeoutSeconds; string(raw) != "null" && json.Unmarshal(raw, &seconds) == nil {
		wait = seconds
	}
	total := wait + syncMargin.Seconds()
	switch {
	case total <= 0:
		return 0
	case total >= maxTimeout.Seconds():
		return maxTimeout
	}
	return time.Duration(total * float64(time.Second))
}

// issuer issues one document type.
type issuer struct {
	engine *engine
	path   string
}

// create issues a document. Its retries reuse the same Idempotency-Key, and when a retry finds
// the document already created by an earlier attempt (422 idempotency_key_reused with a
// documentId), the call returns that document instead of an error.
func (s issuer) create(ctx context.Context, body any, options []RequestOption) (*Document, error) {
	op, err := jsonOperation(http.MethodPost, s.path, body)
	if err != nil {
		return nil, err
	}
	op.minTimeout = syncTimeoutFloor(op.content)
	op.recoverDocument = true
	return fetch[Document](ctx, s.engine, op, options)
}

// InvoiceService issues invoices (facturas). See client.Invoices.
type InvoiceService struct{ issuer issuer }

// Create issues an invoice. The answer is the document: processing right away (202), or, with
// options.waitForAuthorization, its outcome once the SRI decides (200), unless the wait ends
// first (202 with processing or pending_sri; follow it with webhooks or Documents.Get).
// Retries reuse the same Idempotency-Key, and when a retry finds the document already created,
// its current state is returned instead of an error.
func (s *InvoiceService) Create(ctx context.Context, body CreateInvoice, options ...RequestOption) (*Document, error) {
	return s.issuer.create(ctx, body, options)
}

// CreditNoteService issues credit notes (notas de crédito). See client.CreditNotes.
type CreditNoteService struct{ issuer issuer }

// Create issues a credit note. See InvoiceService.Create for the answer and the retries.
func (s *CreditNoteService) Create(ctx context.Context, body CreateCreditNote, options ...RequestOption) (*Document, error) {
	return s.issuer.create(ctx, body, options)
}

// DebitNoteService issues debit notes (notas de débito). See client.DebitNotes.
type DebitNoteService struct{ issuer issuer }

// Create issues a debit note. See InvoiceService.Create for the answer and the retries.
func (s *DebitNoteService) Create(ctx context.Context, body CreateDebitNote, options ...RequestOption) (*Document, error) {
	return s.issuer.create(ctx, body, options)
}

// PurchaseSettlementService issues purchase settlements (liquidaciones de compra). See
// client.PurchaseSettlements.
type PurchaseSettlementService struct{ issuer issuer }

// Create issues a purchase settlement. See InvoiceService.Create for the answer and the
// retries.
func (s *PurchaseSettlementService) Create(ctx context.Context, body CreatePurchaseSettlement, options ...RequestOption) (*Document, error) {
	return s.issuer.create(ctx, body, options)
}

// WaybillService issues waybills (guías de remisión). See client.Waybills.
type WaybillService struct{ issuer issuer }

// Create issues a waybill. See InvoiceService.Create for the answer and the retries.
func (s *WaybillService) Create(ctx context.Context, body CreateWaybill, options ...RequestOption) (*Document, error) {
	return s.issuer.create(ctx, body, options)
}

// WithholdingService issues withholding receipts (comprobantes de retención). See
// client.Withholdings.
type WithholdingService struct{ issuer issuer }

// Create issues a withholding receipt. See InvoiceService.Create for the answer and the
// retries.
func (s *WithholdingService) Create(ctx context.Context, body CreateWithholding, options ...RequestOption) (*Document, error) {
	return s.issuer.create(ctx, body, options)
}

// DocumentXML is the XML of a document and the stage it comes from.
type DocumentXML struct {
	// XML is the document's XML.
	XML string

	// Kind is the stage of the XML: DocumentXMLKindAuthorized (the valid tax document, once
	// the SRI authorizes it), DocumentXMLKindSigned or DocumentXMLKindUnsigned; "" when the API
	// did not say.
	Kind DocumentXMLKind
}

// DocumentXMLKind is the stage an XML comes from. Only DocumentXMLKindAuthorized is the valid
// tax document.
type DocumentXMLKind string

// The stages of an XML.
const (
	DocumentXMLKindAuthorized DocumentXMLKind = "authorized"
	DocumentXMLKindSigned     DocumentXMLKind = "signed"
	DocumentXMLKindUnsigned   DocumentXMLKind = "unsigned"
)

// DocumentService reads every issued document, of any type: search, XML, RIDE (PDF) and email.
// See client.Documents.
type DocumentService struct{ engine *engine }

func (s *DocumentService) lister(params *DocumentListParams, options []RequestOption) *lister[*Document] {
	return newLister(s.engine, "/v1/documents", params.values(), options, decodeStruct[Document])
}

// List walks the documents, newest first, with optional filters (params may be nil), reading
// the pages as it goes:
//
//	for document, err := range client.Documents.List(ctx, nil) {
//		if err != nil {
//			return err
//		}
//		fmt.Println(document.AccessKey)
//	}
func (s *DocumentService) List(ctx context.Context, params *DocumentListParams, options ...RequestOption) iter.Seq2[*Document, error] {
	return s.lister(params, options).all(ctx)
}

// ListPage reads the first page of documents, newest first; NextPage reads the next one.
func (s *DocumentService) ListPage(ctx context.Context, params *DocumentListParams, options ...RequestOption) (*Page[*Document], error) {
	return s.lister(params, options).page(ctx, "")
}

// Get retrieves a document with its tax status, SRI messages, RIDE and email state.
func (s *DocumentService) Get(ctx context.Context, documentID string, options ...RequestOption) (*Document, error) {
	path, err := pathOf("/v1/documents/{}", documentID)
	if err != nil {
		return nil, err
	}
	op := &operation{method: http.MethodGet, path: path, accept: jsonAccept}
	return fetch[Document](ctx, s.engine, op, options)
}

// XML retrieves the document's XML: the authorized one once the SRI authorizes it, otherwise
// the signed or unsigned one (see Kind).
func (s *DocumentService) XML(ctx context.Context, documentID string, options ...RequestOption) (*DocumentXML, error) {
	path, err := pathOf("/v1/documents/{}/xml", documentID)
	if err != nil {
		return nil, err
	}
	op := &operation{method: http.MethodGet, path: path, accept: xmlAccept}
	result := new(DocumentXML)
	err = s.engine.do(ctx, op, options, func(a *answer) error {
		result.XML = decodeText(a.body)
		switch kind := DocumentXMLKind(a.header.Get("Flashpi-Xml-Kind")); kind {
		case DocumentXMLKindAuthorized, DocumentXMLKindSigned, DocumentXMLKindUnsigned:
			result.Kind = kind
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// PDF retrieves the RIDE (the printable PDF) of an authorized document. It fails with a
// *ConflictError whose Code is ride_not_available while the RIDE is still being generated.
func (s *DocumentService) PDF(ctx context.Context, documentID string, options ...RequestOption) ([]byte, error) {
	path, err := pathOf("/v1/documents/{}/pdf", documentID)
	if err != nil {
		return nil, err
	}
	op := &operation{method: http.MethodGet, path: path, accept: pdfAccept}
	var pdf []byte
	err = s.engine.do(ctx, op, options, func(a *answer) error {
		pdf = a.body
		if pdf == nil {
			pdf = []byte{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return pdf, nil
}

// SendEmail emails the authorized document (RIDE and XML) again to the address of its request.
// The new delivery shows in Email.Deliveries; daily limits apply (*RateLimitError). body may be
// nil, which sends {}: the API has no options for it yet.
func (s *DocumentService) SendEmail(ctx context.Context, documentID string, body *SendDocumentEmail, options ...RequestOption) (*Document, error) {
	if body == nil {
		body = &SendDocumentEmail{}
	}
	return create[Document](ctx, s.engine, options, body, "/v1/documents/{}/email", documentID)
}
