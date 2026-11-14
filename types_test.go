package flashpi

import (
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
