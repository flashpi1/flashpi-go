// Extracted from README.en.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package en

import (
	"errors"
	"github.com/flashpi1/flashpi-go"
)

// Block07 is go block 07 of README.en.md.
func Block07() error {
	_, err := client.Invoices.Create(ctx, body)
	var flashpiErr *flashpi.Error
	if errors.As(err, &flashpiErr) && flashpiErr.IdempotencyKey != "" {
		saveForRetry(body, flashpiErr.IdempotencyKey) // your retry queue
	}
	return nil
}
