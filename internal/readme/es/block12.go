// Extracted from README.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package es

import (
	"fmt"
	"github.com/flashpi1/flashpi-go"
	"time"
)

// Block12 is go block 12 of README.md.
func Block12() error {
	document, err := client.Documents.Get(ctx, documentID,
		flashpi.WithTimeout(10*time.Second),
		flashpi.WithLanguage(flashpi.LanguageSpanish),
		flashpi.WithCorrelationID("pedido-42"),
	)
	if err != nil {
		return err
	}
	fmt.Println(document.Status)
	return nil
}
