// Extracted from README.en.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package en

import (
	"fmt"
	"github.com/flashpi1/flashpi-go"
)

// Block03 is go block 03 of README.en.md.
func Block03() error {
	params := &flashpi.DocumentListParams{
		Status:        new(flashpi.DocumentStatusAuthorized),
		IssueDateFrom: new("2026-11-01"),
	}
	for document, err := range client.Documents.List(ctx, params) {
		if err != nil {
			return err
		}
		fmt.Println(document.AccessKey)
	}
	return nil
}
