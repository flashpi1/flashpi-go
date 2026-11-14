// Extracted from README.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package es

import (
	"bytes"
	"cmp"
	"fmt"
	"github.com/flashpi1/flashpi-go"
	"os"
	"time"
)

// Block09 is go block 09 of README.md.
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
		return fmt.Errorf("la firma no sirve: %v", onboarding.Rejection)
	}

	var completed *flashpi.CompletedIssuerOnboarding
	if onboarding.ExistingIssuer != nil {
		// Ya tienes ese RUC: el certificado se agrega a ese emisor (una renovación).
		completed, err = client.IssuerOnboardings.Complete(ctx, onboarding.ID, nil)
	} else {
		// Un RUC nuevo: tú confirmas los datos del emisor, partiendo del prefill.
		issuer := flashpi.CompleteIssuerOnboarding{
			LegalName:         new("MI EMPRESA S.A."),
			HeadOfficeAddress: new("Av. Amazonas N24-100, Quito"),
			TaxProfile:        &flashpi.IssuerTaxProfileInput{KeepsAccounting: false},
			// Si onboarding.RUCNeedsConfirmation es true, envía también RUC.
		}
		if prefill := onboarding.Prefill; prefill != nil {
			issuer.LegalName = cmp.Or(prefill.LegalName, issuer.LegalName)
			issuer.HeadOfficeAddress = cmp.Or(prefill.HeadOfficeAddress, issuer.HeadOfficeAddress)
			issuer.TaxProfile.KeepsAccounting = prefill.TaxProfile.KeepsAccounting != nil && *prefill.TaxProfile.KeepsAccounting
			if regime := prefill.TaxProfile.RIMPERegime; regime != nil && *regime != flashpi.IssuerOnboardingPrefillTaxProfileRIMPERegimeGeneral {
				issuer.TaxProfile.RIMPERegime = flashpi.NullableOf(flashpi.RIMPERegime(*regime))
			}
			// Si es contribuyente especial o agente de retención, agrega sus resoluciones.
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
