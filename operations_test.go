package flashpi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// contractOperation is an operation of openapi/openapi.json.
type contractOperation struct {
	method   string
	path     string
	returns  string // the schema of its 2xx JSON answer (the items of a list), or ""
	isList   bool
	bodyKind string // "json", "multipart" or ""
}

func loadContract(t *testing.T) map[string]contractOperation {
	t.Helper()
	data, err := os.ReadFile("openapi/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	type schemaRef struct {
		Ref        string                     `json:"$ref"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	var document struct {
		Paths      map[string]map[string]json.RawMessage `json:"paths"`
		Components struct {
			Schemas map[string]schemaRef `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	operations := map[string]contractOperation{}
	for path, item := range document.Paths {
		for method, raw := range item {
			if !slices.Contains([]string{"get", "post", "patch", "put", "delete"}, method) {
				continue
			}
			var operation struct {
				OperationID string `json:"operationId"`
				RequestBody *struct {
					Content map[string]json.RawMessage `json:"content"`
				} `json:"requestBody"`
				Responses map[string]struct {
					Content map[string]struct {
						Schema schemaRef `json:"schema"`
					} `json:"content"`
				} `json:"responses"`
			}
			if err := json.Unmarshal(raw, &operation); err != nil {
				t.Fatal(err)
			}
			op := contractOperation{method: strings.ToUpper(method), path: path}
			if operation.RequestBody != nil {
				if _, ok := operation.RequestBody.Content["multipart/form-data"]; ok {
					op.bodyKind = "multipart"
				} else {
					op.bodyKind = "json"
				}
			}
			for status, response := range operation.Responses {
				content, ok := response.Content["application/json"]
				if !strings.HasPrefix(status, "2") || !ok {
					continue
				}
				name := strings.TrimPrefix(content.Schema.Ref, "#/components/schemas/")
				if wrapper, found := document.Components.Schemas[name]; found && wrapper.Properties["pagination"] != nil {
					var data struct {
						Items schemaRef `json:"items"`
					}
					_ = json.Unmarshal(wrapper.Properties["data"], &data)
					name = strings.TrimPrefix(data.Items.Ref, "#/components/schemas/")
					op.isList = true
				}
				op.returns = name
			}
			operations[operation.OperationID] = op
		}
	}
	return operations
}

// sdkOperation calls the method of one operation with the ids id1, id2, ...
type sdkOperation func(ctx context.Context, c *Client) (any, error)

func p12() CertificateUpload {
	return CertificateUpload{File: bytes.NewReader([]byte("p12")), Password: "secret"}
}

var sdkOperations = map[string]sdkOperation{
	"statusGet": func(ctx context.Context, c *Client) (any, error) { return c.Status.Get(ctx) },

	"issuersCreate": func(ctx context.Context, c *Client) (any, error) { return c.Issuers.Create(ctx, CreateIssuer{}) },
	"issuersList": func(ctx context.Context, c *Client) (any, error) {
		return c.Issuers.ListPage(ctx, nil)
	},
	"issuersGet": func(ctx context.Context, c *Client) (any, error) { return c.Issuers.Get(ctx, "id1") },
	"issuersUpdate": func(ctx context.Context, c *Client) (any, error) {
		return c.Issuers.Update(ctx, "id1", UpdateIssuer{})
	},
	"issuersDelete": func(ctx context.Context, c *Client) (any, error) { return nil, c.Issuers.Delete(ctx, "id1") },

	"establishmentsCreate": func(ctx context.Context, c *Client) (any, error) {
		return c.Issuers.Establishments.Create(ctx, "id1", CreateEstablishment{})
	},
	"establishmentsList": func(ctx context.Context, c *Client) (any, error) {
		return c.Issuers.Establishments.ListPage(ctx, "id1", nil)
	},
	"establishmentsGet": func(ctx context.Context, c *Client) (any, error) {
		return c.Issuers.Establishments.Get(ctx, "id1", "id2")
	},
	"establishmentsUpdate": func(ctx context.Context, c *Client) (any, error) {
		return c.Issuers.Establishments.Update(ctx, "id1", "id2", UpdateEstablishment{})
	},
	"establishmentsDelete": func(ctx context.Context, c *Client) (any, error) {
		return nil, c.Issuers.Establishments.Delete(ctx, "id1", "id2")
	},

	"emissionPointsCreate": func(ctx context.Context, c *Client) (any, error) {
		return c.Issuers.Establishments.EmissionPoints.Create(ctx, "id1", "id2", CreateEmissionPoint{})
	},
	"emissionPointsList": func(ctx context.Context, c *Client) (any, error) {
		return c.Issuers.Establishments.EmissionPoints.ListPage(ctx, "id1", "id2", nil)
	},
	"emissionPointsGet": func(ctx context.Context, c *Client) (any, error) {
		return c.Issuers.Establishments.EmissionPoints.Get(ctx, "id1", "id2", "id3")
	},
	"emissionPointsDelete": func(ctx context.Context, c *Client) (any, error) {
		return nil, c.Issuers.Establishments.EmissionPoints.Delete(ctx, "id1", "id2", "id3")
	},

	"certificatesUpload": func(ctx context.Context, c *Client) (any, error) {
		return c.Issuers.Certificates.Upload(ctx, "id1", p12())
	},
	"certificatesList": func(ctx context.Context, c *Client) (any, error) {
		return c.Issuers.Certificates.ListPage(ctx, "id1", nil)
	},
	"certificatesGet": func(ctx context.Context, c *Client) (any, error) {
		return c.Issuers.Certificates.Get(ctx, "id1", "id2")
	},

	"issuerOnboardingsCreate": func(ctx context.Context, c *Client) (any, error) {
		return c.IssuerOnboardings.Create(ctx, p12())
	},
	"issuerOnboardingsGet": func(ctx context.Context, c *Client) (any, error) {
		return c.IssuerOnboardings.Get(ctx, "id1")
	},
	"issuerOnboardingsComplete": func(ctx context.Context, c *Client) (any, error) {
		return c.IssuerOnboardings.Complete(ctx, "id1", nil)
	},

	"invoicesCreate": func(ctx context.Context, c *Client) (any, error) {
		return c.Invoices.Create(ctx, CreateInvoice{})
	},
	"creditNotesCreate": func(ctx context.Context, c *Client) (any, error) {
		return c.CreditNotes.Create(ctx, CreateCreditNote{})
	},
	"debitNotesCreate": func(ctx context.Context, c *Client) (any, error) {
		return c.DebitNotes.Create(ctx, CreateDebitNote{})
	},
	"purchaseSettlementsCreate": func(ctx context.Context, c *Client) (any, error) {
		return c.PurchaseSettlements.Create(ctx, CreatePurchaseSettlement{})
	},
	"waybillsCreate": func(ctx context.Context, c *Client) (any, error) {
		return c.Waybills.Create(ctx, CreateWaybill{})
	},
	"withholdingsCreate": func(ctx context.Context, c *Client) (any, error) {
		return c.Withholdings.Create(ctx, CreateWithholding{})
	},

	"documentsList": func(ctx context.Context, c *Client) (any, error) { return c.Documents.ListPage(ctx, nil) },
	"documentsGet":  func(ctx context.Context, c *Client) (any, error) { return c.Documents.Get(ctx, "id1") },
	"documentsGetXml": func(ctx context.Context, c *Client) (any, error) {
		return c.Documents.XML(ctx, "id1")
	},
	"documentsGetPdf": func(ctx context.Context, c *Client) (any, error) {
		return c.Documents.PDF(ctx, "id1")
	},
	"documentsSendEmail": func(ctx context.Context, c *Client) (any, error) {
		return c.Documents.SendEmail(ctx, "id1", nil)
	},

	"webhookEndpointsCreate": func(ctx context.Context, c *Client) (any, error) {
		return c.WebhookEndpoints.Create(ctx, CreateWebhookEndpoint{})
	},
	"webhookEndpointsList": func(ctx context.Context, c *Client) (any, error) {
		return c.WebhookEndpoints.ListPage(ctx, nil)
	},
	"webhookEndpointsGet": func(ctx context.Context, c *Client) (any, error) {
		return c.WebhookEndpoints.Get(ctx, "id1")
	},
	"webhookEndpointsUpdate": func(ctx context.Context, c *Client) (any, error) {
		return c.WebhookEndpoints.Update(ctx, "id1", UpdateWebhookEndpoint{})
	},
	"webhookEndpointsDelete": func(ctx context.Context, c *Client) (any, error) {
		return nil, c.WebhookEndpoints.Delete(ctx, "id1")
	},
	"webhookEndpointsSecret": func(ctx context.Context, c *Client) (any, error) {
		return c.WebhookEndpoints.GetSecret(ctx, "id1")
	},
	"webhookEndpointsRotateSecret": func(ctx context.Context, c *Client) (any, error) {
		return c.WebhookEndpoints.RotateSecret(ctx, "id1", &RotateWebhookEndpointSecret{})
	},
	"webhookEndpointsVerify": func(ctx context.Context, c *Client) (any, error) {
		return c.WebhookEndpoints.Verify(ctx, "id1")
	},
	"webhookEndpointsDeliveriesList": func(ctx context.Context, c *Client) (any, error) {
		return c.WebhookEndpoints.Deliveries.ListPage(ctx, "id1", nil)
	},
	"webhookEndpointsDeliveriesGet": func(ctx context.Context, c *Client) (any, error) {
		return c.WebhookEndpoints.Deliveries.Get(ctx, "id1", "id2")
	},
	"webhookEndpointsDeliveriesReplay": func(ctx context.Context, c *Client) (any, error) {
		return c.WebhookEndpoints.Deliveries.Replay(ctx, "id1", "id2")
	},

	"eventsList": func(ctx context.Context, c *Client) (any, error) { return c.Events.ListPage(ctx, nil) },
	"eventsGet":  func(ctx context.Context, c *Client) (any, error) { return c.Events.Get(ctx, "id1") },
}

var pathParameter = regexp.MustCompile(`\{[^}]+\}`)

func TestEveryOperationOfTheContractHasAMethod(t *testing.T) {
	contract := loadContract(t)
	if len(contract) != 45 {
		t.Errorf("the contract has %d operations, not 45", len(contract))
	}
	for id := range contract {
		if sdkOperations[id] == nil {
			t.Errorf("no method for %s", id)
		}
	}
	for id := range sdkOperations {
		if _, ok := contract[id]; !ok {
			t.Errorf("%s is not in the contract", id)
		}
	}
	for id, call := range sdkOperations {
		op, ok := contract[id]
		if !ok {
			continue
		}
		t.Run(id, func(t *testing.T) {
			body := `{}`
			switch {
			case op.isList:
				body = `{"data":[{}],"pagination":{"nextCursor":null,"hasMore":false}}`
			case op.method == "DELETE":
				body = ""
			}
			status := 200
			if body == "" {
				status = 204
			}
			client, transport, _ := testClient(t, respond(status, body))
			result, err := call(context.Background(), client)
			if err != nil {
				t.Fatal(err)
			}
			request := transport.only(t)
			counter := 0
			wantPath := pathParameter.ReplaceAllStringFunc(op.path, func(string) string {
				counter++
				return "id" + string(rune('0'+counter))
			})
			if request.Method != op.method || request.URL != testBaseURL+wantPath {
				t.Errorf("sent %s %s, want %s %s", request.Method, request.URL, op.method, testBaseURL+wantPath)
			}
			contentType := request.Header.Get("Content-Type")
			switch op.bodyKind {
			case "json":
				if contentType != "application/json" || !json.Valid(request.Body) {
					t.Errorf("body %q with %q", request.Body, contentType)
				}
			case "multipart":
				if !strings.HasPrefix(contentType, "multipart/form-data; boundary=") {
					t.Errorf("Content-Type = %q", contentType)
				}
			default:
				if len(request.Body) != 0 || contentType != "" {
					t.Errorf("an operation without a body sent %q with %q", request.Body, contentType)
				}
			}
			if (op.method == http.MethodPost) != (request.Header.Get("Idempotency-Key") != "") {
				t.Errorf("Idempotency-Key = %q", request.Header.Get("Idempotency-Key"))
			}
			if op.returns != "" {
				if got := returnedType(result); got != op.returns {
					t.Errorf("returns %s, the contract says %s", got, op.returns)
				}
			}
		})
	}
}

// returnedType is the Go name of what a method returned: the items of a page.
func returnedType(result any) string {
	value := reflect.ValueOf(result)
	if value.Kind() == reflect.Pointer && strings.HasPrefix(value.Type().Elem().Name(), "Page[") {
		item := value.Elem().FieldByName("Data").Type().Elem()
		if item.Kind() == reflect.Pointer {
			item = item.Elem()
		}
		return item.Name()
	}
	if _, isEvent := result.(Event); isEvent {
		return "Event"
	}
	if value.Kind() == reflect.Pointer {
		return value.Type().Elem().Name()
	}
	return value.Type().Name()
}

func TestTheAnswersOfXMLAndPDF(t *testing.T) {
	client, transport, _ := testClient(t,
		respond(200, "<factura/>", "Content-Type", "application/xml", "Flashpi-Xml-Kind", "authorized"),
		respond(200, "<factura/>", "Content-Type", "application/xml", "Flashpi-Xml-Kind", "other"),
		respond(200, "%PDF-1.7\xff", "Content-Type", "application/pdf"),
		respond(200, "", "Content-Type", "application/pdf"),
	)
	ctx := context.Background()
	xml, err := client.Documents.XML(ctx, "d")
	if err != nil || xml.XML != "<factura/>" || xml.Kind != DocumentXMLKindAuthorized {
		t.Errorf("got %+v %v", xml, err)
	}
	xml, err = client.Documents.XML(ctx, "d")
	if err != nil || xml.Kind != "" {
		t.Errorf("an unknown kind was kept: %+v %v", xml, err)
	}
	pdf, err := client.Documents.PDF(ctx, "d")
	if err != nil || string(pdf) != "%PDF-1.7\xff" {
		t.Errorf("got %q %v", pdf, err)
	}
	pdf, err = client.Documents.PDF(ctx, "d")
	if err != nil || pdf == nil || len(pdf) != 0 {
		t.Errorf("an empty PDF: %v %v", pdf, err)
	}
	requests := transport.all()
	if requests[0].Header.Get("Accept") != "application/xml, application/problem+json" || requests[2].Header.Get("Accept") != "application/pdf, application/problem+json" {
		t.Errorf("Accept = %q, %q", requests[0].Header.Get("Accept"), requests[2].Header.Get("Accept"))
	}
}

func TestTheBodiesOfTheActions(t *testing.T) {
	client, transport, _ := testClient(t, respond(201, `{}`), respond(201, `{}`), respond(200, `{}`), respond(200, `{}`))
	ctx := context.Background()
	if _, err := client.IssuerOnboardings.Complete(ctx, "o", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := client.IssuerOnboardings.Complete(ctx, "o", &CompleteIssuerOnboarding{LegalName: ptr("MI EMPRESA S.A."), TradeName: Null[string]()}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.WebhookEndpoints.RotateSecret(ctx, "w", &RotateWebhookEndpointSecret{PreviousSecretExpiresInHours: ptr(0)}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Documents.SendEmail(ctx, "d", nil); err != nil {
		t.Fatal(err)
	}
	want := []string{`{}`, `{"legalName":"MI EMPRESA S.A.","tradeName":null}`, `{"previousSecretExpiresInHours":0}`, `{}`}
	for index, request := range transport.all() {
		if string(request.Body) != want[index] {
			t.Errorf("request %d: body %s, want %s", index, request.Body, want[index])
		}
	}
}

func TestAnEventFromTheAPI(t *testing.T) {
	client, _, _ := testClient(t,
		respond(200, `{"id":"e1","type":"certificate.verified","environment":"live","createdAt":"2026-11-13T12:00:00Z","data":{"id":"c1","status":"accepted"}}`),
		respond(200, `{"id":"e2","type":"x.new","environment":"live","createdAt":"2026-11-13T12:00:00Z","data":[1]}`),
		respond(200, `{"id":"e3","type":"invoice.authorized","environment":"live","createdAt":"2026-11-13T12:00:00Z","data":{"id":5}}`),
	)
	ctx := context.Background()
	event, err := client.Events.Get(ctx, "e1")
	certificate, ok := event.(*CertificateEvent)
	if err != nil || !ok || certificate.Data.ID != "c1" || certificate.Data.Status != CertificateStatusAccepted {
		t.Errorf("got %#v %v", event, err)
	}
	event, err = client.Events.Get(ctx, "e2")
	if unknown, ok := event.(*UnknownEvent); err != nil || !ok || string(unknown.Data) != "[1]" {
		t.Errorf("got %#v %v", event, err)
	}
	_, err = client.Events.Get(ctx, "e3")
	if sdkErr := mustAs[*Error](t, err); !strings.Contains(sdkErr.Message, "event this version of the SDK cannot read") {
		t.Errorf("got %v", err)
	}
}

func TestOptionalBodiesThatAreNil(t *testing.T) {
	client, transport, _ := testClient(t, respond(200, `{}`))
	if _, err := client.WebhookEndpoints.RotateSecret(context.Background(), "w", nil); err != nil {
		t.Fatal(err)
	}
	request := transport.only(t)
	if len(request.Body) != 0 || request.Header.Get("Content-Type") != "" {
		t.Errorf("RotateSecret(nil) sent %q with %q", request.Body, request.Header.Get("Content-Type"))
	}
}
