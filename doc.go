// Package flashpi is the official Go SDK of Flashpi, the electronic invoicing API for
// Ecuador's SRI (https://flashpi.com.ec).
//
// It covers the 45 operations of the API: the six document types, the issued documents (XML,
// RIDE as PDF, email), issuers with their establishments, emission points and .p12
// certificates, webhook endpoints and events. It uses the standard library only.
//
//	client, err := flashpi.NewClient() // reads FLASHPI_API_KEY
//	if err != nil {
//		log.Fatal(err)
//	}
//	invoice, err := client.Invoices.Create(ctx, flashpi.CreateInvoice{...})
//
// Every method takes a context.Context first and options last (WithIdempotencyKey,
// WithTimeout, WithResponseInto, ...). Lists are iterators:
//
//	for document, err := range client.Documents.List(ctx, nil) {
//		...
//	}
//
// Errors are checked with errors.As: *APIError and its status types (*UnprocessableEntityError,
// *RateLimitError, ...), *ConnectionError and *TimeoutError, and *Error for everything else.
//
// Every POST carries an Idempotency-Key that its retries reuse, so a retry never issues a
// document twice. UnwrapWebhook verifies the webhooks Flashpi sends (Standard Webhooks).
package flashpi
