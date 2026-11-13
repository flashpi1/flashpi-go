package flashpi

import (
	"encoding/json"
	"time"
)

// WebhookEvent is what a webhook endpoint receives, as UnwrapWebhook returns it: one of
//
//   - *DocumentEvent, *CertificateEvent and *AccountEvent (an Event, in full mode),
//   - *ThinEvent (an event of an endpoint in thin mode, without its data: read it with
//     client.Events.Get),
//   - *WebhookEndpointVerificationEvent (the activation handshake: answer it with
//     {"challenge": e.Data.Challenge}),
//   - *UnknownEvent (a type this version of the SDK does not know).
//
// Use a type switch:
//
//	switch e := event.(type) {
//	case *flashpi.WebhookEndpointVerificationEvent:
//		// answer {"challenge": e.Data.Challenge}
//	case *flashpi.DocumentEvent:
//		fmt.Println(e.Type, e.Data.AccessKey)
//	}
type WebhookEvent interface {
	// Info returns what every event has: its id, type, environment and creation time.
	Info() EventInfo
	isWebhookEvent()
}

// EventInfo is what every event has.
type EventInfo struct {
	// ID is the event's id, also its webhook-id: store it and drop repeated deliveries.
	ID string

	// Type is the event's type, such as EventTypeInvoiceAuthorized, or
	// "webhook_endpoint.verification" for the activation handshake.
	Type EventType

	// Environment is the mode of the resource.
	Environment Environment

	// CreatedAt is when the event happened.
	CreatedAt time.Time
}

// UnknownEvent is an event of a type this version of the SDK does not know. Its data is kept as
// raw JSON.
type UnknownEvent struct {
	ID          string          `json:"id"`
	Type        EventType       `json:"type"`
	Environment Environment     `json:"environment"`
	CreatedAt   time.Time       `json:"createdAt"`
	Data        json.RawMessage `json:"data,omitempty"`
}

// Info returns what every event has: its id, type, environment and creation time.
func (e *UnknownEvent) Info() EventInfo {
	return EventInfo{ID: e.ID, Type: e.Type, Environment: e.Environment, CreatedAt: e.CreatedAt}
}

func (*UnknownEvent) isEvent()        {}
func (*UnknownEvent) isWebhookEvent() {}

// Info returns what every event has: its id, type, environment and creation time.
func (e *ThinEvent) Info() EventInfo {
	return EventInfo{ID: e.ID, Type: e.Type, Environment: e.Environment, CreatedAt: e.CreatedAt}
}

func (*ThinEvent) isWebhookEvent() {}

// Info returns what every event has: its id, type, environment and creation time.
func (e *WebhookEndpointVerificationEvent) Info() EventInfo {
	return EventInfo{ID: e.ID, Type: EventType(e.Type), Environment: e.Environment, CreatedAt: e.CreatedAt}
}

func (*WebhookEndpointVerificationEvent) isWebhookEvent() {}

// verificationType is the type of the activation handshake of an endpoint.
const verificationType = "webhook_endpoint.verification"

// decodeWebhookEvent decodes the body of a webhook: the handshake, a thin event (no data) or an
// event.
func decodeWebhookEvent(body []byte) (WebhookEvent, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(body, &members); err != nil {
		return nil, err
	}
	var kind string
	_ = json.Unmarshal(members["type"], &kind)
	data, hasData := members["data"]
	switch {
	case kind == verificationType:
		event := new(WebhookEndpointVerificationEvent)
		if err := json.Unmarshal(body, event); err != nil {
			return nil, err
		}
		return event, nil
	case !hasData || string(data) == "null":
		event := new(ThinEvent)
		if err := json.Unmarshal(body, event); err != nil {
			return nil, err
		}
		return event, nil
	}
	return decodeEvent(body)
}

func decodeEventItem(raw json.RawMessage) (Event, error) { return decodeEvent(raw) }
