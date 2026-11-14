// Extracted from README.en.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package en

import (
	"fmt"
	"github.com/flashpi1/flashpi-go"
	"os"
	"time"
)

// Block11 is go block 11 of README.en.md.
func Block11() error {
	client, err := flashpi.NewClient(
		flashpi.WithAPIKey(os.Getenv("FLASHPI_API_KEY")), // defaults to the FLASHPI_API_KEY variable
		flashpi.WithLanguage(flashpi.LanguageSpanish),    // language of the messages: English (default) or Spanish
		flashpi.WithTimeout(60*time.Second),              // per attempt (at least the SRI wait + 15 s with waitForAuthorization)
		flashpi.WithMaxRetries(2),                        // 0 turns retries off
	)
	if err != nil {
		return err
	}
	fmt.Println(client) // shows the environment and the URL, never the key
	return nil
}
