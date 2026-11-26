package flashpi

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"
	"time"
)

func TestNullable(t *testing.T) {
	cases := []struct {
		body UpdateIssuer
		want string
	}{
		{UpdateIssuer{}, `{}`},
		{UpdateIssuer{TradeName: Null[string]()}, `{"tradeName":null}`},
		{UpdateIssuer{TradeName: NullableOf("")}, `{"tradeName":""}`},
		{UpdateIssuer{TradeName: NullableOf("La Pradera"), LegalName: ptr("X")}, `{"legalName":"X","tradeName":"La Pradera"}`},
		{
			UpdateIssuer{TaxProfile: &IssuerTaxProfilePatch{RIMPERegime: Null[RIMPERegime](), KeepsAccounting: ptr(false)}},
			`{"taxProfile":{"keepsAccounting":false,"rimpeRegime":null}}`,
		},
		{
			UpdateIssuer{TaxProfile: &IssuerTaxProfilePatch{RIMPERegime: NullableOf(RIMPERegimeEntrepreneur)}},
			`{"taxProfile":{"rimpeRegime":"entrepreneur"}}`,
		},
	}
	for _, tc := range cases {
		data, err := json.Marshal(tc.body)
		if err != nil || string(data) != tc.want {
			t.Errorf("got %s %v, want %s", data, err, tc.want)
		}
	}
	content, _ := encodeJSON(CreateWebhookEndpoint{URL: "https://a.test/?x=1&y=<2>", Description: NullableOf("A & B <c>")})
	if string(content) != `{"url":"https://a.test/?x=1&y=<2>","description":"A & B <c>","enabledEvents":null}` {
		t.Errorf("body = %s", content)
	}
	var zero Nullable[int]
	if !zero.IsZero() || zero.IsNull() {
		t.Error("the zero value is not unset")
	}
	if value, ok := NullableOf(3).Value(); !ok || value != 3 {
		t.Error("NullableOf lost its value")
	}
	if _, ok := Null[int]().Value(); ok || !Null[int]().IsNull() {
		t.Error("Null has a value")
	}
	if data, _ := json.Marshal([]Nullable[int]{zero, NullableOf(1)}); string(data) != "[null,1]" {
		t.Errorf("outside omitzero: %s", data)
	}
	var read struct {
		A Nullable[string] `json:"a"`
		B Nullable[string] `json:"b"`
		C Nullable[string] `json:"c"`
	}
	if err := json.Unmarshal([]byte(`{"a":null,"b":"x"}`), &read); err != nil {
		t.Fatal(err)
	}
	if !read.A.IsNull() || !read.C.IsZero() {
		t.Errorf("got %+v", read)
	}
	if value, ok := read.B.Value(); !ok || value != "x" {
		t.Errorf("B = %q", value)
	}
	if err := json.Unmarshal([]byte(`{"a":1}`), &read); err == nil {
		t.Error("a number was read as a string")
	}
}

func TestADocumentFromTheAPI(t *testing.T) {
	body := `{"id":"d1","type":"invoice","environment":"test","status":"pending_sri","accessKey":"k",
		"createdAt":"2026-11-13T12:00:00.123Z","counterparty":null,"totals":{"total":"1.15"},"newField":[1,2]}`
	var document Document
	if err := json.Unmarshal([]byte(body), &document); err != nil {
		t.Fatal(err)
	}
	if document.Status != DocumentStatusPendingSRI || document.Type != DocumentTypeInvoice || document.Counterparty != nil {
		t.Errorf("got %+v", document)
	}
	if document.Totals == nil || *document.Totals.Total != "1.15" {
		t.Errorf("Totals = %+v", document.Totals)
	}
	if !document.CreatedAt.Equal(time.Date(2026, 11, 13, 12, 0, 0, 123_000_000, time.UTC)) {
		t.Errorf("CreatedAt = %v", document.CreatedAt)
	}
}

func TestTheFailureCodesOfADocument(t *testing.T) {
	cases := map[string]DocumentFailureCode{
		"certificate_ruc_mismatch":   DocumentFailureCodeCertificateRUCMismatch,
		"sri_authorization_mismatch": DocumentFailureCodeSRIAuthorizationMismatch,
	}
	for code, want := range cases {
		var document Document
		body := `{"id":"d1","status":"failed","failure":{"code":"` + code + `","message":"Failed."}}`
		if err := json.Unmarshal([]byte(body), &document); err != nil {
			t.Fatal(err)
		}
		if document.Status != DocumentStatusFailed || document.Failure == nil || document.Failure.Code != want {
			t.Errorf("%s: got %+v", code, document.Failure)
		}
	}
}

func TestACertificateWithWarnings(t *testing.T) {
	client, transport, _ := testClient(t,
		respond(200, `{"id":"c1","issuerId":"i1","environment":"test","allModes":false,"status":"accepted","usable":true,
			"ruc":"1790016919001","revocation":null,"rejection":null,
			"warnings":[{"code":"ruc_mismatch","message":"This certificate is not for the issuer's RUC."},{"code":"x_new","message":"New."}],
			"createdAt":"2026-11-24T12:00:00Z","updatedAt":"2026-11-24T12:00:00Z"}`),
		respond(200, `{"id":"c2","issuerId":"i1","environment":"live","allModes":true,"status":"accepted","usable":true,
			"revocation":null,"rejection":null,"warnings":[],"createdAt":"2026-11-24T12:00:00Z","updatedAt":"2026-11-24T12:00:00Z"}`),
	)
	ctx := context.Background()
	certificate, err := client.Issuers.Certificates.Get(ctx, "i1", "c1")
	if err != nil {
		t.Fatal(err)
	}
	if len(certificate.Warnings) != 2 {
		t.Fatalf("Warnings = %+v", certificate.Warnings)
	}
	if warning := certificate.Warnings[0]; warning.Code != CertificateWarningCodeRUCMismatch || warning.Message != "This certificate is not for the issuer's RUC." {
		t.Errorf("Warnings[0] = %+v", warning)
	}
	// A code this version does not know is kept as it came.
	if certificate.Warnings[1].Code != "x_new" {
		t.Errorf("Warnings[1] = %+v", certificate.Warnings[1])
	}
	certificate, err = client.Issuers.Certificates.Get(ctx, "i1", "c2")
	if err != nil || certificate.Warnings == nil || len(certificate.Warnings) != 0 {
		t.Errorf("got %+v %v", certificate, err)
	}
	if got := transport.all()[1].URL; got != testBaseURL+"/v1/issuers/i1/certificates/c2" {
		t.Errorf("URL = %s", got)
	}
}

func TestTheQueryOfEveryParams(t *testing.T) {
	cases := []struct {
		values url.Values
		want   string
	}{
		{(*DocumentListParams)(nil).values(), ""},
		{(&DocumentListParams{}).values(), ""},
		{(&DocumentListParams{Limit: ptr(100), Type: ptr(DocumentTypeCreditNote), IssuerID: ptr("i"), CounterpartyID: ptr("c"), IssueDateTo: ptr("2026-11-30")}).values(), "counterpartyId=c&issueDateTo=2026-11-30&issuerId=i&limit=100&type=credit_note"},
		{(&EventListParams{IssuerID: ptr("i"), DocumentID: ptr("d"), CertificateID: ptr("c"), Cursor: ptr("x")}).values(), "certificateId=c&cursor=x&documentId=d&issuerId=i"},
		{(&WebhookDeliveryListParams{EventID: ptr("e")}).values(), "eventId=e"},
		{(&CertificateListParams{Limit: ptr(1)}).values(), "limit=1"},
		{(&EmissionPointListParams{Cursor: ptr("c")}).values(), "cursor=c"},
		{(&EstablishmentListParams{Limit: ptr(2)}).values(), "limit=2"},
		{(&WebhookEndpointListParams{Limit: ptr(3)}).values(), "limit=3"},
	}
	for index, tc := range cases {
		if got := tc.values.Encode(); got != tc.want {
			t.Errorf("case %d: %s, want %s", index, got, tc.want)
		}
	}
}

func TestSharedEnums(t *testing.T) {
	// One Go type for the values several schemas repeat.
	var document Document
	var certificate Certificate
	var thin ThinEvent
	var delivery WebhookDelivery
	document.Environment = EnvironmentLive
	certificate.Environment = document.Environment
	thin.Type = EventTypeInvoiceAuthorized
	delivery.EventType = thin.Type
	if certificate.Environment != EnvironmentLive || delivery.EventType != EventTypeInvoiceAuthorized {
		t.Fail()
	}
	endpoint := CreateWebhookEndpoint{EnabledEvents: []EnabledEvent{EnabledEventAll, EnabledEventInvoiceRejected}}
	data, _ := json.Marshal(endpoint.EnabledEvents)
	if string(data) != `["*","invoice.rejected"]` {
		t.Errorf("got %s", data)
	}
}
