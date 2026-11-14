// Extracted from README.en.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/flashpi1/flashpi-go"
)

func main() {
	http.HandleFunc("POST /webhooks/flashpi", flashpiWebhook)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func flashpiWebhook(w http.ResponseWriter, r *http.Request) {
	// The body arrives before the signature is checked: limit its size.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
	if err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	event, err := flashpi.UnwrapWebhook(body, r.Header, os.Getenv("FLASHPI_WEBHOOK_SECRET"))
	var invalid *flashpi.WebhookVerificationError
	switch {
	case errors.As(err, &invalid) && invalid.Reason == flashpi.WebhookInvalidSecret:
		// An empty or mistyped secret is your configuration, not a forged webhook.
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	case errors.As(err, &invalid):
		w.WriteHeader(http.StatusBadRequest)
		return
	case err != nil:
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	switch e := event.(type) {
	case *flashpi.WebhookEndpointVerificationEvent:
		// The endpoint's activation: answer the challenge.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge": e.Data.Challenge})
		return
	case *flashpi.DocumentEvent: // e.Data is the document
		switch e.Type {
		case flashpi.DocumentEventTypeInvoiceAuthorized:
			log.Println("Authorized", e.Data.AccessKey)
		case flashpi.DocumentEventTypeInvoiceRejected:
			log.Println("Rejected", e.Data.SRIMessages)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
