// Extracted from README.en.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package en

import (
	"bytes"
	"cmp"
	"fmt"
	"github.com/flashpi1/flashpi-go"
	"os"
	"time"
)

// Block09 is go block 09 of README.en.md.
func Block09() error {
	p12, err := os.ReadFile("firma.p12")
	if err != nil {
		return err
	}
	onboarding, err := client.IssuerOnboardings.Create(ctx, flashpi.CertificateUpload{
		File:     bytes.NewReader(p12),
		Password: os.Getenv("P12_PASSWORD"),
	})
	for err == nil && onboarding.Status == flashpi.IssuerOnboardingStatusVerifying {
		time.Sleep(2 * time.Second)
		onboarding, err = client.IssuerOnboardings.Get(ctx, onboarding.ID)
	}
	if err != nil {
		return err
	}
	if onboarding.Status != flashpi.IssuerOnboardingStatusReady {
		return fmt.Errorf("the signature cannot be used: %v", onboarding.Rejection)
	}

	var completed *flashpi.CompletedIssuerOnboarding
	if onboarding.ExistingIssuer != nil {
		// You already have that RUC: the certificate is added to that issuer (a renewal).
		completed, err = client.IssuerOnboardings.Complete(ctx, onboarding.ID, nil)
	} else {
		// A new RUC: you confirm the issuer's data, starting from the prefill.
		issuer := flashpi.CompleteIssuerOnboarding{
			LegalName:         new("MY COMPANY S.A."),
			HeadOfficeAddress: new("Av. Amazonas N24-100, Quito"),
			TaxProfile:        &flashpi.IssuerTaxProfileInput{KeepsAccounting: false},
			// When onboarding.RUCNeedsConfirmation is true, send RUC too.
		}
		if prefill := onboarding.Prefill; prefill != nil {
			issuer.LegalName = cmp.Or(prefill.LegalName, issuer.LegalName)
			issuer.HeadOfficeAddress = cmp.Or(prefill.HeadOfficeAddress, issuer.HeadOfficeAddress)
			issuer.TaxProfile.KeepsAccounting = prefill.TaxProfile.KeepsAccounting != nil && *prefill.TaxProfile.KeepsAccounting
			if regime := prefill.TaxProfile.RIMPERegime; regime != nil && *regime != flashpi.IssuerOnboardingPrefillTaxProfileRIMPERegimeGeneral {
				issuer.TaxProfile.RIMPERegime = flashpi.NullableOf(flashpi.RIMPERegime(*regime))
			}
			// For a special taxpayer or a withholding agent, add their resolution numbers.
		}
		completed, err = client.IssuerOnboardings.Complete(ctx, onboarding.ID, &issuer)
	}
	if err != nil {
		return err
	}

	for establishment, err := range client.Issuers.Establishments.List(ctx, completed.Issuer.ID, nil) {
		if err != nil {
			return err
		}
		fmt.Println(establishment.Code)
	}
	return nil
}
