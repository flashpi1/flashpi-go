// Extracted from README.en.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package en

import (
	"github.com/flashpi1/flashpi-go"
)

// Block10 is go block 10 of README.en.md.
func Block10() error {
	_, err := client.Issuers.Update(ctx, issuerID, flashpi.UpdateIssuer{
		TradeName:          flashpi.Null[string](), // clears the trade name
		SendEmailByDefault: new(false),
		Branding:           &flashpi.IssuerBrandingInput{BrandColor: flashpi.NullableOf("#0C160D")},
	})
	if err != nil {
		return err
	}
	return nil
}
