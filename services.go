package flashpi

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
)

// IssuerOnboardingService creates an issuer from its .p12: upload the certificate, wait while
// the onboarding is verifying until it is ready (Flashpi reads the holder, RUC and SRI data) or
// rejected, review the prefill, then complete it. It needs an account-scoped API key with
// read_write access. See client.IssuerOnboardings.
type IssuerOnboardingService struct{ engine *engine }

// Create starts an onboarding with a .p12 and its password. It starts verifying.
func (s *IssuerOnboardingService) Create(ctx context.Context, upload CertificateUpload, options ...RequestOption) (*IssuerOnboarding, error) {
	op, err := certificateForm(upload)
	if err != nil {
		return nil, err
	}
	op.path = "/v1/issuer-onboardings"
	return fetch[IssuerOnboarding](ctx, s.engine, op, options)
}

// Get retrieves an onboarding, to follow it while it is verifying.
func (s *IssuerOnboardingService) Get(ctx context.Context, onboardingID string, options ...RequestOption) (*IssuerOnboarding, error) {
	return get[IssuerOnboarding](ctx, s.engine, options, "/v1/issuer-onboardings/{}", onboardingID)
}

// Complete completes a ready onboarding: it creates the issuer from body, or, when the RUC
// already is an issuer of the account (ExistingIssuer), adds the certificate to it. body may be
// nil, which sends {} (a renewal).
func (s *IssuerOnboardingService) Complete(ctx context.Context, onboardingID string, body *CompleteIssuerOnboarding, options ...RequestOption) (*CompletedIssuerOnboarding, error) {
	if body == nil {
		body = &CompleteIssuerOnboarding{}
	}
	return create[CompletedIssuerOnboarding](ctx, s.engine, options, body, "/v1/issuer-onboardings/{}/complete", onboardingID)
}

// WebhookEndpointService manages the URLs Flashpi sends events to, and the deliveries of those
// events. It needs an account-scoped API key with read_write access. To check the signature of
// a webhook you receive, use UnwrapWebhook instead. See client.WebhookEndpoints.
type WebhookEndpointService struct {
	engine *engine

	// Deliveries reads the deliveries of an endpoint.
	Deliveries *WebhookDeliveryService
}

// Create creates an endpoint. Flashpi sends it the activation handshake
// (webhook_endpoint.verification) right away.
func (s *WebhookEndpointService) Create(ctx context.Context, body CreateWebhookEndpoint, options ...RequestOption) (*WebhookEndpoint, error) {
	return create[WebhookEndpoint](ctx, s.engine, options, body, "/v1/webhook-endpoints")
}

func (s *WebhookEndpointService) lister(params *WebhookEndpointListParams, options []RequestOption) *lister[*WebhookEndpoint] {
	return newLister(s.engine, "/v1/webhook-endpoints", params.values(), options, decodeStruct[WebhookEndpoint])
}

// List walks the endpoints, newest first, reading the pages as it goes. params may be nil.
func (s *WebhookEndpointService) List(ctx context.Context, params *WebhookEndpointListParams, options ...RequestOption) iter.Seq2[*WebhookEndpoint, error] {
	return s.lister(params, options).all(ctx)
}

// ListPage reads the first page of endpoints, newest first; NextPage reads the next one.
func (s *WebhookEndpointService) ListPage(ctx context.Context, params *WebhookEndpointListParams, options ...RequestOption) (*Page[*WebhookEndpoint], error) {
	return s.lister(params, options).page(ctx, "")
}

// Get retrieves an endpoint.
func (s *WebhookEndpointService) Get(ctx context.Context, endpointID string, options ...RequestOption) (*WebhookEndpoint, error) {
	return get[WebhookEndpoint](ctx, s.engine, options, "/v1/webhook-endpoints/{}", endpointID)
}

// Update changes an endpoint (only the members set are sent; flashpi.Null clears the
// description).
func (s *WebhookEndpointService) Update(ctx context.Context, endpointID string, body UpdateWebhookEndpoint, options ...RequestOption) (*WebhookEndpoint, error) {
	return update[WebhookEndpoint](ctx, s.engine, options, body, "/v1/webhook-endpoints/{}", endpointID)
}

// Delete deletes an endpoint.
func (s *WebhookEndpointService) Delete(ctx context.Context, endpointID string, options ...RequestOption) error {
	return remove(ctx, s.engine, options, "/v1/webhook-endpoints/{}", endpointID)
}

// GetSecret retrieves the endpoint's signing secret (whsec_...), for UnwrapWebhook.
func (s *WebhookEndpointService) GetSecret(ctx context.Context, endpointID string, options ...RequestOption) (*WebhookEndpointSecret, error) {
	return get[WebhookEndpointSecret](ctx, s.engine, options, "/v1/webhook-endpoints/{}/secret", endpointID)
}

// RotateSecret creates a new signing secret. The previous one keeps signing next to it for
// body.PreviousSecretExpiresInHours (default 24). body may be nil, which sends no body.
func (s *WebhookEndpointService) RotateSecret(ctx context.Context, endpointID string, body *RotateWebhookEndpointSecret, options ...RequestOption) (*WebhookEndpoint, error) {
	var content any
	if body != nil {
		content = body
	}
	return create[WebhookEndpoint](ctx, s.engine, options, content, "/v1/webhook-endpoints/{}/rotate-secret", endpointID)
}

// Verify sends the activation handshake again, to activate an endpoint that did not answer it.
func (s *WebhookEndpointService) Verify(ctx context.Context, endpointID string, options ...RequestOption) (*WebhookEndpoint, error) {
	return create[WebhookEndpoint](ctx, s.engine, options, nil, "/v1/webhook-endpoints/{}/verify", endpointID)
}

// WebhookDeliveryService reads the deliveries of events to one endpoint. See
// client.WebhookEndpoints.Deliveries.
type WebhookDeliveryService struct{ engine *engine }

func (s *WebhookDeliveryService) lister(endpointID string, params *WebhookDeliveryListParams, options []RequestOption) (*lister[*WebhookDelivery], error) {
	path, err := pathOf("/v1/webhook-endpoints/{}/deliveries", endpointID)
	if err != nil {
		return nil, err
	}
	return newLister(s.engine, path, params.values(), options, decodeStruct[WebhookDelivery]), nil
}

// List walks the deliveries of an endpoint, newest first, optionally by status, event type or
// event, reading the pages as it goes. params may be nil.
func (s *WebhookDeliveryService) List(ctx context.Context, endpointID string, params *WebhookDeliveryListParams, options ...RequestOption) iter.Seq2[*WebhookDelivery, error] {
	l, err := s.lister(endpointID, params, options)
	if err != nil {
		return failedList[*WebhookDelivery](err)
	}
	return l.all(ctx)
}

// ListPage reads the first page of an endpoint's deliveries, newest first; NextPage reads the
// next one.
func (s *WebhookDeliveryService) ListPage(ctx context.Context, endpointID string, params *WebhookDeliveryListParams, options ...RequestOption) (*Page[*WebhookDelivery], error) {
	l, err := s.lister(endpointID, params, options)
	if err != nil {
		return nil, err
	}
	return l.page(ctx, "")
}

// Get retrieves a delivery with its attempts.
func (s *WebhookDeliveryService) Get(ctx context.Context, endpointID, deliveryID string, options ...RequestOption) (*WebhookDeliveryDetail, error) {
	return get[WebhookDeliveryDetail](ctx, s.engine, options, "/v1/webhook-endpoints/{}/deliveries/{}", endpointID, deliveryID)
}

// Replay sends the delivery's event again, with the same webhook-id.
func (s *WebhookDeliveryService) Replay(ctx context.Context, endpointID, deliveryID string, options ...RequestOption) (*WebhookDeliveryDetail, error) {
	return create[WebhookDeliveryDetail](ctx, s.engine, options, nil, "/v1/webhook-endpoints/{}/deliveries/{}/replay", endpointID, deliveryID)
}

// EventService reads the events of documents, certificates and email sending, newest first.
// See client.Events.
type EventService struct{ engine *engine }

func (s *EventService) lister(params *EventListParams, options []RequestOption) *lister[Event] {
	return newLister(s.engine, "/v1/events", params.values(), options, decodeEventItem)
}

// List walks the events, newest first, optionally by type, issuer, document or certificate,
// reading the pages as it goes. params may be nil. Narrow each event with a type switch (see
// Event).
func (s *EventService) List(ctx context.Context, params *EventListParams, options ...RequestOption) iter.Seq2[Event, error] {
	return s.lister(params, options).all(ctx)
}

// ListPage reads the first page of events, newest first; NextPage reads the next one.
func (s *EventService) ListPage(ctx context.Context, params *EventListParams, options ...RequestOption) (*Page[Event], error) {
	return s.lister(params, options).page(ctx, "")
}

// Get retrieves an event: the same object its webhook delivered.
func (s *EventService) Get(ctx context.Context, eventID string, options ...RequestOption) (Event, error) {
	path, err := pathOf("/v1/events/{}", eventID)
	if err != nil {
		return nil, err
	}
	var event Event
	op := &operation{method: http.MethodGet, path: path, accept: jsonAccept}
	err = s.engine.do(ctx, op, options, func(a *answer) error {
		var raw json.RawMessage
		if err := readJSON(a, &raw); err != nil {
			return err
		}
		decoded, err := decodeEvent(raw)
		if err != nil {
			return &Error{Message: "The API answered an event this version of the SDK cannot read: " + err.Error(), Err: err}
		}
		event = decoded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return event, nil
}

// StatusService reads the API's own status. See client.Status.
type StatusService struct{ engine *engine }

// Get reports whether Flashpi is operational.
func (s *StatusService) Get(ctx context.Context, options ...RequestOption) (*Status, error) {
	return fetch[Status](ctx, s.engine, &operation{method: http.MethodGet, path: "/v1/status", accept: jsonAccept}, options)
}
