// Extracted from README.md by internal/cmd/readme: edit the README, then run
// go run ./internal/cmd/readme generate.

package es

import (
	"fmt"
	"github.com/flashpi1/flashpi-go"
	"os"
	"time"
)

// Block11 is go block 11 of README.md.
func Block11() error {
	client, err := flashpi.NewClient(
		flashpi.WithAPIKey(os.Getenv("FLASHPI_API_KEY")), // por defecto, la variable FLASHPI_API_KEY
		flashpi.WithLanguage(flashpi.LanguageSpanish),    // idioma de los mensajes: inglés (por defecto) o español
		flashpi.WithTimeout(60*time.Second),              // por intento (con waitForAuthorization, al menos la espera + 15 s)
		flashpi.WithMaxRetries(2),                        // 0 desactiva los reintentos
	)
	if err != nil {
		return err
	}
	fmt.Println(client) // muestra el ambiente y la URL, nunca la clave
	return nil
}
