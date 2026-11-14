// Extracted from README.md by internal/cmd/readme: edit the README, then run
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
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	event, err := flashpi.UnwrapWebhook(body, r.Header, os.Getenv("FLASHPI_WEBHOOK_SECRET"))
	var invalid *flashpi.WebhookVerificationError
	if errors.As(err, &invalid) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	switch e := event.(type) {
	case *flashpi.WebhookEndpointVerificationEvent:
		// La activación del endpoint: responde el desafío.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge": e.Data.Challenge})
		return
	case *flashpi.DocumentEvent: // e.Data es el documento
		switch e.Type {
		case flashpi.DocumentEventTypeInvoiceAuthorized:
			log.Println("Autorizada", e.Data.AccessKey)
		case flashpi.DocumentEventTypeInvoiceRejected:
			log.Println("Rechazada", e.Data.SRIMessages)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
