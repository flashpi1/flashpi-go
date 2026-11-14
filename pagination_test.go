package flashpi

import (
	"context"
	"fmt"
	"iter"
	"net/url"
	"strings"
	"testing"
)

func listJSON(cursor string, hasMore bool, ids ...string) string {
	items := make([]string, 0, len(ids))
	for _, id := range ids {
		items = append(items, documentJSON(id))
	}
	next := "null"
	if cursor != "" {
		next = fmt.Sprintf("%q", cursor)
	}
	return fmt.Sprintf(`{"data":[%s],"pagination":{"nextCursor":%s,"hasMore":%t}}`, strings.Join(items, ","), next, hasMore)
}

func TestListWalksEveryPage(t *testing.T) {
	client, transport, _ := testClient(t,
		respond(200, listJSON("c1", true, "a", "b")),
		respond(200, listJSON("c2", true, "c")),
		respond(200, listJSON("", false, "d")),
	)
	var ids []string
	params := &DocumentListParams{Status: ptr(DocumentStatusAuthorized), Cursor: ptr("start")}
	for document, err := range client.Documents.List(context.Background(), params, WithHeader("X-Page", "1")) {
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, document.ID)
	}
	if strings.Join(ids, ",") != "a,b,c,d" {
		t.Errorf("ids = %v", ids)
	}
	requests := transport.all()
	wantQueries := []string{"cursor=start&status=authorized", "cursor=c1&status=authorized", "cursor=c2&status=authorized"}
	for index, request := range requests {
		parsed, _ := url.Parse(request.URL)
		if parsed.RawQuery != wantQueries[index] || request.Header.Get("X-Page") != "1" {
			t.Errorf("request %d: %s %v", index, request.URL, request.Header)
		}
	}
	if *params.Cursor != "start" {
		t.Error("the params were changed")
	}
}

func TestListStopsWhenTheLoopBreaks(t *testing.T) {
	client, transport, _ := testClient(t, respond(200, listJSON("c1", true, "a", "b")))
	for document, err := range client.Documents.List(context.Background(), nil) {
		if err != nil || document.ID != "a" {
			t.Fatalf("got %v %v", document, err)
		}
		break
	}
	if len(transport.all()) != 1 {
		t.Errorf("%d requests", len(transport.all()))
	}
}

func TestListYieldsTheErrorOfAPageOnce(t *testing.T) {
	client, _, _ := testClient(t, respond(200, listJSON("c1", true, "a")), problem(404, "not_found"))
	var got []string
	for document, err := range client.Documents.List(context.Background(), nil) {
		if err != nil {
			mustAs[*NotFoundError](t, err)
			got = append(got, "error")
			continue
		}
		got = append(got, document.ID)
	}
	if strings.Join(got, ",") != "a,error" {
		t.Errorf("got %v", got)
	}
}

func TestAListCanBeWalkedAgain(t *testing.T) {
	client, transport, _ := testClient(t, respond(200, listJSON("", false, "a")), respond(200, listJSON("", false, "b")))
	list := client.Documents.List(context.Background(), nil)
	var ids []string
	for range 2 {
		for document, err := range list {
			if err != nil {
				t.Fatal(err)
			}
			ids = append(ids, document.ID)
		}
	}
	if strings.Join(ids, ",") != "a,b" || len(transport.all()) != 2 {
		t.Errorf("ids %v after %d requests", ids, len(transport.all()))
	}
}

func TestListPageAndNextPage(t *testing.T) {
	client, transport, _ := testClient(t,
		respond(200, listJSON("c1", true, "a"), "X-Request-Id", "first"),
		respond(200, listJSON("", false, "b"), "X-Request-Id", "second"),
	)
	ctx := context.Background()
	var response Response
	page, err := client.Documents.ListPage(ctx, &DocumentListParams{Limit: ptr(1)}, WithResponseInto(&response))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 1 || page.Data[0].ID != "a" || !page.HasMore || page.NextCursor != "c1" || !page.HasNextPage() || response.RequestID != "first" {
		t.Fatalf("page = %+v, response %+v", page, response)
	}
	next, err := page.NextPage(ctx)
	if err != nil || len(next.Data) != 1 || next.Data[0].ID != "b" || next.HasNextPage() || response.RequestID != "second" {
		t.Fatalf("next = %+v %v", next, err)
	}
	last, err := next.NextPage(ctx)
	if last != nil || err != nil {
		t.Errorf("after the last page: %v %v", last, err)
	}
	if got := transport.all()[1].URL; got != testBaseURL+"/v1/documents?cursor=c1&limit=1" {
		t.Errorf("URL = %s", got)
	}
	var none *Page[*Document]
	if none.HasNextPage() {
		t.Error("a nil page has a next page")
	}
}

func TestAPageThatSaysItHasMoreWithoutACursorIsTheLast(t *testing.T) {
	client, transport, _ := testClient(t, respond(200, `{"data":[],"pagination":{"nextCursor":null,"hasMore":true}}`))
	for _, err := range client.Documents.List(context.Background(), nil) {
		t.Fatalf("got %v", err)
	}
	if len(transport.all()) != 1 {
		t.Error("a page without a cursor was followed")
	}
}

func TestAListBodyWithoutDataAndPagination(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":[]}`, `{"pagination":{}}`, `{"data":null,"pagination":{}}`} {
		client, _, _ := testClient(t, respond(200, body))
		_, err := client.Documents.ListPage(context.Background(), nil)
		if sdkErr := mustAs[*Error](t, err); !strings.Contains(sdkErr.Message, "without data and pagination") {
			t.Errorf("%s: %v", body, err)
		}
	}
	client, _, _ := testClient(t, respond(200, `{"data":[{"id":1}],"pagination":{"hasMore":false}}`))
	_, err := client.Documents.ListPage(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "list item") {
		t.Errorf("got %v", err)
	}
}

func TestEveryListOfTheAPI(t *testing.T) {
	ctx := context.Background()
	empty := `{"data":[],"pagination":{"nextCursor":null,"hasMore":false}}`
	client, transport, _ := testClient(t,
		respond(200, empty), respond(200, empty), respond(200, empty), respond(200, empty),
		respond(200, empty), respond(200, empty), respond(200, empty), respond(200, empty),
	)
	drain(t, client.Issuers.List(ctx, &IssuerListParams{Limit: ptr(5)}))
	drain(t, client.Issuers.Establishments.List(ctx, "i", nil))
	drain(t, client.Issuers.Establishments.EmissionPoints.List(ctx, "i", "e", nil))
	drain(t, client.Issuers.Certificates.List(ctx, "i", nil))
	drain(t, client.WebhookEndpoints.List(ctx, nil))
	drain(t, client.WebhookEndpoints.Deliveries.List(ctx, "w", &WebhookDeliveryListParams{Status: ptr(WebhookDeliveryStatusFailed), EventType: ptr(EventTypeInvoiceAuthorized)}))
	drain(t, client.Events.List(ctx, &EventListParams{Type: ptr(EventTypeCertificateRevoked)}))
	drain(t, client.Documents.List(ctx, nil))
	want := []string{
		"/v1/issuers?limit=5",
		"/v1/issuers/i/establishments",
		"/v1/issuers/i/establishments/e/emission-points",
		"/v1/issuers/i/certificates",
		"/v1/webhook-endpoints",
		"/v1/webhook-endpoints/w/deliveries?eventType=invoice.authorized&status=failed",
		"/v1/events?type=certificate.revoked",
		"/v1/documents",
	}
	for index, request := range transport.all() {
		if request.URL != testBaseURL+want[index] || request.Method != "GET" {
			t.Errorf("request %d: %s %s", index, request.Method, request.URL)
		}
	}
}

func TestEveryListPageRefusesAMissingID(t *testing.T) {
	client, _, _ := testClient(t)
	ctx := context.Background()
	errs := []error{}
	_, err := client.Issuers.Establishments.ListPage(ctx, "", nil)
	errs = append(errs, err)
	_, err = client.Issuers.Establishments.EmissionPoints.ListPage(ctx, "i", " ", nil)
	errs = append(errs, err)
	_, err = client.Issuers.Certificates.ListPage(ctx, ".", nil)
	errs = append(errs, err)
	_, err = client.WebhookEndpoints.Deliveries.ListPage(ctx, "", nil)
	errs = append(errs, err)
	for index, err := range errs {
		if err == nil {
			t.Errorf("case %d: no error", index)
		}
	}
	for _, list := range []func() error{
		func() error {
			for _, err := range client.Issuers.Establishments.EmissionPoints.List(ctx, "", "e", nil) {
				return err
			}
			return nil
		},
		func() error {
			for _, err := range client.Issuers.Certificates.List(ctx, "", nil) {
				return err
			}
			return nil
		},
		func() error {
			for _, err := range client.WebhookEndpoints.Deliveries.List(ctx, "", nil) {
				return err
			}
			return nil
		},
	} {
		if list() == nil {
			t.Error("a list without its id did not fail")
		}
	}
}

// drain walks a list to its end, failing on an error.
func drain[E any](t *testing.T, list iter.Seq2[E, error]) []E {
	t.Helper()
	var items []E
	for item, err := range list {
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	return items
}
