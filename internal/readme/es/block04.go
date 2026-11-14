// Extracted from README.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package es

import (
	"fmt"
	"github.com/flashpi1/flashpi-go"
)

// Block04 is go block 04 of README.md.
func Block04() error {
	page, err := client.Documents.ListPage(ctx, &flashpi.DocumentListParams{Limit: new(50)})
	for page != nil && err == nil {
		fmt.Println(len(page.Data)) // los documentos de esta página
		page, err = page.NextPage(ctx)
	}
	if err != nil {
		return err
	}
	return nil
}
