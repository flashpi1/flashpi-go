// Extracted from README.en.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package en

import (
	"fmt"
	"github.com/flashpi1/flashpi-go"
)

// Block04 is go block 04 of README.en.md.
func Block04() error {
	page, err := client.Documents.ListPage(ctx, &flashpi.DocumentListParams{Limit: new(50)})
	for page != nil && err == nil {
		fmt.Println(len(page.Data)) // the documents of this page
		page, err = page.NextPage(ctx)
	}
	if err != nil {
		return err
	}
	return nil
}
