// Extracted from README.en.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package en

import (
	"fmt"
	"os"
)

// Block02 is go block 02 of README.en.md.
func Block02() error {
	document, err := client.Documents.Get(ctx, invoice.ID)
	if err != nil {
		return err
	}
	fmt.Println(document.Status)

	xml, err := client.Documents.XML(ctx, invoice.ID)
	if err != nil {
		return err
	}
	fmt.Println(xml.Kind) // flashpi.DocumentXMLKindAuthorized once the SRI authorized it

	pdf, err := client.Documents.PDF(ctx, invoice.ID) // the RIDE, as bytes
	if err != nil {
		return err
	}
	if err := os.WriteFile(invoice.AccessKey+".pdf", pdf, 0o644); err != nil {
		return err
	}

	// Emails the RIDE and the XML to the buyer again.
	if _, err := client.Documents.SendEmail(ctx, invoice.ID); err != nil {
		return err
	}
	return nil
}
