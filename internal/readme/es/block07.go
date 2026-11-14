// Extracted from README.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package es

import (
	"errors"
	"github.com/flashpi1/flashpi-go"
)

// Block07 is go block 07 of README.md.
func Block07() error {
	_, err := client.Invoices.Create(ctx, body)
	var flashpiErr *flashpi.Error
	if errors.As(err, &flashpiErr) && flashpiErr.IdempotencyKey != "" {
		saveForRetry(body, flashpiErr.IdempotencyKey) // tu cola de reintentos
	}
	return nil
}
