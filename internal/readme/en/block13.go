// Extracted from README.en.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package en

import (
	"fmt"
	"github.com/flashpi1/flashpi-go"
)

// Block13 is go block 13 of README.en.md.
func Block13() error {
	var response flashpi.Response
	document, err := client.Documents.Get(ctx, documentID, flashpi.WithResponseInto(&response))
	if err != nil {
		return err
	}
	fmt.Println(document.Status, response.StatusCode, response.RequestID)
	return nil
}
