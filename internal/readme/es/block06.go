// Extracted from README.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package es

import (
	"github.com/flashpi1/flashpi-go"
)

// Block06 is go block 06 of README.md.
func Block06() error {
	_, err := client.Invoices.Create(ctx, body, flashpi.WithIdempotencyKey("pedido-"+order.ID))
	if err != nil {
		return err
	}
	return nil
}
