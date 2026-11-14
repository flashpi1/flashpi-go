// Extracted from README.en.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package en

import (
	"errors"
	"fmt"
	"github.com/flashpi1/flashpi-go"
)

// Block05 is go block 05 of README.en.md.
func Block05() error {
	_, err := client.Invoices.Create(ctx, body)

	var invalid *flashpi.UnprocessableEntityError
	var apiErr *flashpi.APIError
	switch {
	case errors.As(err, &invalid):
		fmt.Println(invalid.Code) // "validation_failed", "total_mismatch", ...
		for _, field := range invalid.Errors {
			fmt.Println(field.Field, field.Message)
		}
	case errors.As(err, &apiErr):
		fmt.Println(apiErr.Status, apiErr.Code, apiErr.RequestID) // share RequestID with support
	case err != nil:
		return err // no answer from the API: *flashpi.ConnectionError, *flashpi.TimeoutError, ...
	}
	return nil
}
