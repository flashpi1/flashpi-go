// Extracted from README.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package es

import (
	"errors"
	"fmt"
	"github.com/flashpi1/flashpi-go"
)

// Block05 is go block 05 of README.md.
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
		fmt.Println(apiErr.Status, apiErr.Code, apiErr.RequestID) // comparte RequestID con soporte
	case err != nil:
		return err // sin respuesta de la API: *flashpi.ConnectionError, *flashpi.TimeoutError, ...
	}
	return nil
}
