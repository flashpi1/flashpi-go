// Extracted from README.en.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package main

import (
	"context"
	"fmt"
	"log"

	"github.com/flashpi1/flashpi-go"
)

func main() {
	// Reads FLASHPI_API_KEY; you can also pass it: flashpi.NewClient(flashpi.WithAPIKey("fp_test_..."))
	client, err := flashpi.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	invoice, err := client.Invoices.Create(context.Background(), flashpi.CreateInvoice{
		IssuerID:      "<the demo issuer id or yours>",
		Establishment: "001",
		EmissionPoint: "001",
		Customer: flashpi.DocumentParty{
			IDType:  flashpi.IDTypeRUC,
			ID:      new("1760013210001"),
			Name:    new("PRUEBAS SERVICIO DE RENTAS INTERNAS"),
			Address: new("Av. Amazonas y Villalengua, Quito"),
		},
		Items: []flashpi.DocumentItem{{
			Code:        new("P-001"),
			Description: "Test service",
			Quantity:    "1",
			UnitPrice:   "1.00",
			Taxes:       []flashpi.DocumentTax{{Type: flashpi.TaxTypeVAT, Rate: "15"}},
		}},
		Payments: []flashpi.DocumentPayment{{Method: flashpi.PaymentMethodCash, Amount: new("1.15")}},
		Options:  &flashpi.DocumentOptions{WaitForAuthorization: new(true)},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(invoice.Status, invoice.AccessKey)
}
