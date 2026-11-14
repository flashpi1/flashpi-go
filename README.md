# Flashpi para Go

SDK oficial de [Flashpi](https://flashpi.com.ec), la API de facturación electrónica del SRI de
Ecuador, para Go.

[English](https://github.com/flashpi1/flashpi-go/blob/main/README.en.md)

- Las 45 operaciones de la API: los seis comprobantes, documentos (XML, RIDE en PDF, correo),
  emisores con establecimientos, puntos de emisión y certificados .p12, webhooks y eventos.
- Structs generados del documento OpenAPI de la API: tu editor y el compilador conocen cada
  campo.
- `context.Context` en cada llamada, listas que se recorren con `range` y errores que se revisan
  con `errors.As`.
- Reintentos seguros: cada emisión lleva un `Idempotency-Key`, así un reintento nunca emite dos
  veces.
- Errores tipados (RFC 9457) con su `Code`, los errores por campo y el `RequestID`.
- Verificación de firmas de webhooks (Standard Webhooks) con `net/http` o cualquier framework.
- Go 1.26 o superior. Sin dependencias: solo la biblioteca estándar.

## Instalación

```bash
go get github.com/flashpi1/flashpi-go
```

## Primeros pasos

Crea una clave de API en la [consola](https://flashpi.com.ec/app/) y guárdala en la variable de
entorno `FLASHPI_API_KEY`. Una clave `fp_test_` emite en el ambiente de pruebas del SRI; una
`fp_live_` emite comprobantes reales en producción.

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/flashpi1/flashpi-go"
)

func main() {
	// Lee FLASHPI_API_KEY; también puedes pasarla: flashpi.NewClient(flashpi.WithAPIKey("fp_test_..."))
	client, err := flashpi.NewClient()
	if err != nil {
		log.Fatal(err)
	}

	invoice, err := client.Invoices.Create(context.Background(), flashpi.CreateInvoice{
		IssuerID:      "<id del emisor demo o del tuyo>",
		Establishment: "001",
		EmissionPoint: "001",
		Customer: flashpi.DocumentParty{
			IDType:  flashpi.IDTypeRUC,
			ID:      new("1760013210001"),
			Name:    new("PRUEBAS SERVICIO DE RENTAS INTERNAS"),
			Address: new("Av. Amazonas y Villalengua, Quito"),
		},
		Items: []flashpi.DocumentItem{{
			Code:        new("P-001"),
			Description: "Servicio de prueba",
			Quantity:    "1",
			UnitPrice:   "1.00",
			Taxes:       []flashpi.DocumentTax{{Type: flashpi.TaxTypeVAT, Rate: "15"}},
		}},
		Payments: []flashpi.DocumentPayment{{Method: flashpi.PaymentMethodCash, Amount: "1.15"}},
		Options:  &flashpi.DocumentOptions{WaitForAuthorization: new(true)},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(invoice.Status, invoice.AccessKey)
}
```

Las peticiones y las respuestas son structs con los campos de la API (`invoice.AccessKey` es
`accessKey`). Los campos opcionales son punteros: `new("valor")` los crea (los que aceptan `null`
son `flashpi.Nullable`, más abajo). Los montos y
cantidades son **textos** (`"1.15"`), nunca números, para no perder decimales.

Con el **emisor demo** puedes emitir en pruebas sin subir un certificado: su id está en el
Resumen de la consola (hasta 50 comprobantes al día). Más en
[Sandbox y emisor demo](https://flashpi.com.ec/docs/sandbox/).

Un cliente mantiene sus conexiones abiertas y se puede usar desde varias goroutines: crea uno y
reutilízalo.

## Emitir comprobantes

| Comprobante           | Método                                           |
| --------------------- | ------------------------------------------------ |
| Factura               | `client.Invoices.Create(ctx, body)`              |
| Nota de crédito       | `client.CreditNotes.Create(ctx, body)`           |
| Nota de débito        | `client.DebitNotes.Create(ctx, body)`            |
| Liquidación de compra | `client.PurchaseSettlements.Create(ctx, body)`   |
| Guía de remisión      | `client.Waybills.Create(ctx, body)`              |
| Retención             | `client.Withholdings.Create(ctx, body)`          |

Todos devuelven el documento. Por defecto la respuesta llega enseguida con el estado
`processing` y el resultado del SRI llega por webhook. Con
`Options: &flashpi.DocumentOptions{WaitForAuthorization: new(true)}` la API espera la respuesta
del SRI (15 s por defecto, hasta 30 s con `WaitTimeoutSeconds`) y devuelve `authorized`,
`rejected` o `failed`; si el SRI tarda más, devuelve `processing` o `pending_sri` y el documento
sigue su curso.

## Documentos

```go
document, err := client.Documents.Get(ctx, invoice.ID)
if err != nil {
	return err
}
fmt.Println(document.Status)

xml, err := client.Documents.XML(ctx, invoice.ID)
if err != nil {
	return err
}
fmt.Println(xml.Kind) // flashpi.DocumentXMLKindAuthorized cuando ya lo autorizó el SRI

pdf, err := client.Documents.PDF(ctx, invoice.ID) // el RIDE, en bytes
if err != nil {
	return err
}
if err := os.WriteFile(invoice.AccessKey+".pdf", pdf, 0o644); err != nil {
	return err
}

// Reenvía RIDE y XML al correo del comprador.
if _, err := client.Documents.SendEmail(ctx, invoice.ID, nil); err != nil {
	return err
}
```

## Listas y paginación

`List` recorre todas las páginas con `range`, pidiéndolas a medida que avanzas. Si una página
falla, recibes el error en `err`:

```go
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
```

`ListPage` devuelve una página a la vez, y `NextPage` la siguiente (`nil` después de la última):

```go
page, err := client.Documents.ListPage(ctx, &flashpi.DocumentListParams{Limit: new(50)})
for page != nil && err == nil {
	fmt.Println(len(page.Data)) // los documentos de esta página
	page, err = page.NextPage(ctx)
}
if err != nil {
	return err
}
```

Los filtros usan los nombres de la API (`IssueDateFrom`, `IssuerID`), y `nil` significa sin
filtros. `Limit` es el tamaño de cada página: `List` recorre todas, así que sal del `for` con
`break` cuando tengas lo que buscabas.

## Errores

Cada error de la API es un `*flashpi.APIError`, envuelto en un tipo propio cuando su estado HTTP lo
tiene (los de la tabla; los demás, como 405 o 413, llegan como `*flashpi.APIError`). Revísalo con
`errors.As` y decide según `Code`, que es estable y en inglés; `Message` sigue el idioma pedido.

```go
_, err := client.Invoices.Create(ctx, body)

var invalid *flashpi.UnprocessableEntityError
var apiErr *flashpi.APIError
switch {
case errors.As(err, &invalid):
	fmt.Println(invalid.Code) // "validation_failed", "total_mismatch", ...
	for _, field := range invalid.Errors {
		fmt.Println(field.Field, field.Message)
	}
case errors.As(err, &apiErr):
	fmt.Println(apiErr.Status, apiErr.Code, apiErr.RequestID) // comparte RequestID con soporte
case err != nil:
	return err // sin respuesta de la API: *flashpi.ConnectionError, *flashpi.TimeoutError, ...
}
```

| Tipo                       | Estado | Ejemplos de `Code`                                          |
| -------------------------- | ------ | ----------------------------------------------------------- |
| `BadRequestError`          | 400    | `malformed_json`, `invalid_idempotency_key`                 |
| `AuthenticationError`      | 401    | `authentication_required`, `invalid_api_key`                |
| `PermissionDeniedError`    | 403    | `key_read_only`, `key_scope_mismatch`, `ip_not_allowed`     |
| `NotFoundError`            | 404    | `not_found`                                                 |
| `ConflictError`            | 409    | `sequential_already_used`, `ride_not_available`             |
| `UnprocessableEntityError` | 422    | `validation_failed`, `total_mismatch`                       |
| `RateLimitError`           | 429    | `demo_issuer_quota_exceeded`, `account_email_limit_reached` |
| `InternalServerError`      | 5xx    | `internal_error`                                            |
| `ConnectionError`          | —      | sin respuesta (red); `TimeoutError` si se agotó el plazo    |

Todos se leen también como `*flashpi.Error` con `errors.As`. Algunos errores traen datos extra en
`apiErr.Problem`, por ejemplo `DocumentID`, `Limit`, `ResetsAt` o `RIDEStatus`. Para recibir los
mensajes en español: `flashpi.NewClient(flashpi.WithLanguage(flashpi.LanguageSpanish))`.

## Reintentos e idempotencia

El SDK reintenta hasta 2 veces, con espera creciente, los errores de red, los tiempos agotados y
las respuestas 408, 429 y 5xx, salvo un 501, un 503 `demo_issuer_unavailable` y las que piden
esperar (`Retry-After`) más de 60 s; también reintenta un 409 `idempotency_key_in_progress`. Cada
POST lleva un
`Idempotency-Key` que se repite en sus reintentos, así que la API nunca emite dos veces:

- Si un reintento descubre que el primer intento sí creó el documento, el SDK devuelve ese
  documento.
- Si el primer intento sigue en curso en la API (por ejemplo, se cortó la conexión mientras
  esperaba al SRI), el SDK espera a que termine con la misma clave, hasta el tiempo límite de la
  llamada, sin gastar reintentos.

Para usar tu propia clave (por ejemplo el id de tu pedido), pásala a la llamada:

```go
_, err := client.Invoices.Create(ctx, body, flashpi.WithIdempotencyKey("pedido-"+order.ID))
if err != nil {
	return err
}
```

Si una emisión falla igual, el error trae la clave que usó en `IdempotencyKey`. Reintentar con
`flashpi.WithIdempotencyKey(key)` nunca emite un segundo documento: dentro de 24 horas la API
devuelve la primera respuesta; después (o si la API perdió el primer intento), la llamada falla
con un 422 `idempotency_key_reused` cuyo `Problem.DocumentID` es el documento que ya existe
(léelo con `client.Documents.Get`).

```go
_, err := client.Invoices.Create(ctx, body)
var flashpiErr *flashpi.Error
if errors.As(err, &flashpiErr) && flashpiErr.IdempotencyKey != "" {
	saveForRetry(body, flashpiErr.IdempotencyKey) // tu cola de reintentos
}
```

El `ctx` limita la llamada entera, reintentos incluidos. Si se cancela o vence, la llamada se
detiene sin reintentar y devuelve un `*flashpi.Error` con su clave, con el que
`errors.Is(err, context.Canceled)` (o `context.DeadlineExceeded`) es verdadero. Un intento que
agota su propio plazo (`flashpi.WithTimeout`) también cumple `context.DeadlineExceeded`, como en
`net/http`: revisa primero `*flashpi.TimeoutError` o `ctx.Err()` para distinguirlos.

Los límites diarios (como la cuota del emisor demo) no se reintentan: llegan como
`*flashpi.RateLimitError`, y su `RetryAfter()` dice cuánto esperar.

## Webhooks

Verifica cada webhook con el cuerpo **tal cual llegó** (sin parsear) y el secreto `whsec_` del
endpoint. `flashpi.UnwrapWebhook` no necesita clave de API ni hace E/S, y devuelve el evento: usa
un `switch` de tipos.

```go
package main

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/flashpi1/flashpi-go"
)

func main() {
	http.HandleFunc("POST /webhooks/flashpi", flashpiWebhook)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func flashpiWebhook(w http.ResponseWriter, r *http.Request) {
	// El cuerpo llega antes de verificar la firma: limita su tamaño.
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
	if err != nil {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		return
	}
	event, err := flashpi.UnwrapWebhook(body, r.Header, os.Getenv("FLASHPI_WEBHOOK_SECRET"))
	var invalid *flashpi.WebhookVerificationError
	switch {
	case errors.As(err, &invalid) && invalid.Reason == flashpi.WebhookInvalidSecret:
		// Un secreto vacío o mal copiado es tu configuración, no un webhook falso.
		log.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	case errors.As(err, &invalid):
		w.WriteHeader(http.StatusBadRequest)
		return
	case err != nil:
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	switch e := event.(type) {
	case *flashpi.WebhookEndpointVerificationEvent:
		// La activación del endpoint: responde el desafío.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"challenge": e.Data.Challenge})
		return
	case *flashpi.DocumentEvent: // e.Data es el documento
		switch e.Type {
		case flashpi.DocumentEventTypeInvoiceAuthorized:
			log.Println("Autorizada", e.Data.AccessKey)
		case flashpi.DocumentEventTypeInvoiceRejected:
			log.Println("Rechazada", e.Data.SRIMessages)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
```

Con Gin, Echo, chi o Fiber pasa igual el cuerpo crudo y las cabeceras de la petición. Responde 400
a un webhook que no pasa la verificación, salvo `WebhookInvalidSecret`: ese es tu secreto, no el
webhook, así que regístralo y responde 500 (Flashpi lo reintentará).

Guarda `event.Info().ID` y descarta los repetidos: un evento puede llegar más de una vez. Durante
una rotación del secreto, Flashpi firma con el nuevo y el anterior, así que cualquiera de los dos
sirve. Si el endpoint usa el modo `thin` (sin `data`), el evento es un `*flashpi.ThinEvent`: pide
el evento completo con `client.Events.Get(ctx, e.ID)`. Un tipo de evento que esta versión no
conoce llega como `*flashpi.UnknownEvent`.

## Emisores y certificados

Estas operaciones necesitan una clave de toda la cuenta con acceso de lectura y escritura.

Un emisor nace de su firma electrónica (.p12): Flashpi la verifica, lee el RUC y consulta al SRI,
y te devuelve un `Prefill` con lo que encontró para que armes el emisor.

```go
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
```

El `File` es un `io.Reader` (los bytes del .p12 con `bytes.NewReader`, o un `*os.File` abierto),
de hasta 1 MiB. El SDK lo lee una vez, antes de enviar nada, y no lo cierra.

Para **renovar la firma** de un emisor, para pruebas y producción a la vez, repite el mismo
flujo con el .p12 nuevo: Flashpi reconoce el RUC (`ExistingIssuer`) y `Complete` sin datos
agrega el certificado. En cambio,
`client.Issuers.Certificates.Upload(ctx, issuerID, flashpi.CertificateUpload{...})` sube un
certificado solo para el modo de la clave que usas (pruebas o producción).

## Campos opcionales y null

Los campos opcionales son punteros (`nil` no se envía) y `new(valor)` los crea. Los campos que una
petición puede enviar como `null` son `flashpi.Nullable`: `flashpi.Null` envía `null` (en una
actualización, borra el campo), `flashpi.NullableOf` envía un valor y sin asignar no se envía.
Están en las actualizaciones y también en algunas creaciones, como `RIMPERegime` del perfil
tributario o `Description` de un endpoint de webhooks.

```go
_, err := client.Issuers.Update(ctx, issuerID, flashpi.UpdateIssuer{
	TradeName:          flashpi.Null[string](), // borra el nombre comercial
	SendEmailByDefault: new(false),
	Branding:           &flashpi.IssuerBrandingInput{BrandColor: flashpi.NullableOf("#0C160D")},
})
if err != nil {
	return err
}
```

## Opciones

```go
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
```

Cada método acepta además, al final, opciones de esa llamada: `flashpi.WithIdempotencyKey`,
`flashpi.WithTimeout`, `flashpi.WithMaxRetries`, `flashpi.WithLanguage`,
`flashpi.WithCorrelationID` (tu propio id, que Flashpi registra) y `flashpi.WithHeader`.

```go
document, err := client.Documents.Get(ctx, documentID,
	flashpi.WithTimeout(10*time.Second),
	flashpi.WithLanguage(flashpi.LanguageSpanish),
	flashpi.WithCorrelationID("pedido-42"),
)
if err != nil {
	return err
}
fmt.Println(document.Status)
```

Para ver la respuesta HTTP y el id de la petición, pasa `flashpi.WithResponseInto`:

```go
var response flashpi.Response
document, err := client.Documents.Get(ctx, documentID, flashpi.WithResponseInto(&response))
if err != nil {
	return err
}
fmt.Println(document.Status, response.StatusCode, response.RequestID)
```

Para enviar las peticiones con tu propio `*http.Client` (un proxy, un transporte de pruebas),
pásalo con `flashpi.WithHTTPClient`. El SDK nunca sigue redirecciones.

```go
proxy, err := url.Parse("http://proxy.interno:3128")
if err != nil {
	return err
}
client, err := flashpi.NewClient(flashpi.WithHTTPClient(&http.Client{
	Transport: &http.Transport{Proxy: http.ProxyURL(proxy)},
}))
if err != nil {
	return err
}
fmt.Println(client.Environment())
```

En las pruebas, un `httptest.Server` (con `flashpi.WithBaseURL`) o un `http.RoundTripper` propio
responde en lugar de la API.

## Solo en el servidor

Tu clave de API es secreta: usa el SDK en tus servidores, nunca en código que corre en los
dispositivos de tus usuarios. `fmt.Println(client)` muestra el ambiente y la URL, nunca la clave.

## Licencia

[MIT](https://github.com/flashpi1/flashpi-go/blob/main/LICENSE) © 2026 SIGNIADIGITAL S.A.
